package booking_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/fairyhunter13/ottodot-trial-booking/internal/booking"
	"github.com/fairyhunter13/ottodot-trial-booking/internal/fixture"
	"github.com/fairyhunter13/ottodot-trial-booking/internal/store"
)

var ctx = context.Background()

func TestCreate(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fixture.Fix) (parent, student, class string)
		want  error
	}{
		{"EC-01_first_booking_is_pending", func(f *fixture.Fix) (string, string, string) {
			p := f.Parent()
			return p, f.Student(p), f.Class(4, 0)
		}, nil},

		{"EC-02_duplicate_against_a_pending_row", func(f *fixture.Fix) (string, string, string) {
			p, c := f.Parent(), f.Class(4, 0)
			s := f.Student(p)
			f.Booking(s, c, booking.StatusPending)
			return p, s, c
		}, booking.ErrDuplicate},

		{"EC-03_duplicate_against_a_confirmed_row", func(f *fixture.Fix) (string, string, string) {
			p, c := f.Parent(), f.Class(4, 0)
			s := f.Student(p)
			f.Booking(s, c, booking.StatusConfirmed)
			return p, s, c
		}, booking.ErrDuplicate},

		{"EC-04_retry_after_a_failed_payment", func(f *fixture.Fix) (string, string, string) {
			p, c := f.Parent(), f.Class(4, 0)
			s := f.Student(p)
			f.Booking(s, c, booking.StatusFailed)
			return p, s, c
		}, nil},

		{"EC-05_retry_after_a_lost_seat", func(f *fixture.Fix) (string, string, string) {
			p, c := f.Parent(), f.Class(4, 2)
			s := f.Student(p)
			f.Booking(s, c, booking.StatusCancelled)
			return p, s, c
		}, nil},

		{"EC-06_class_is_already_full", func(f *fixture.Fix) (string, string, string) {
			p := f.Parent()
			return p, f.Student(p), f.Class(4, 4)
		}, booking.ErrClassFull},

		{"EC-07_unknown_child", func(f *fixture.Fix) (string, string, string) {
			return f.Parent(), "nobody", f.Class(4, 0)
		}, booking.ErrNotFound},

		{"EC-08_unknown_class", func(f *fixture.Fix) (string, string, string) {
			p := f.Parent()
			return p, f.Student(p), "nowhere"
		}, booking.ErrNotFound},

		{"EC-09_child_of_another_parent", func(f *fixture.Fix) (string, string, string) {
			return f.Parent(), f.Student(f.Parent()), f.Class(4, 0)
		}, booking.ErrForbidden},

		{"EC-20_class_has_already_started", func(f *fixture.Fix) (string, string, string) {
			p := f.Parent()
			return p, f.Student(p), f.ClassAt("-1 hour", 4)
		}, booking.ErrClassStarted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.New(t)
			st := booking.New(f.DB)
			parent, student, class := tc.setup(f)

			const pending = `SELECT COUNT(*) FROM bookings WHERE status = 'pending_payment'`
			before := f.Count(pending)

			b, err := st.Create(ctx, parent, student, class)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			want := before
			if tc.want == nil {
				want++
				if b.Status != booking.StatusPending {
					t.Fatalf("status = %q, want %q", b.Status, booking.StatusPending)
				}
			}
			if got := f.Count(pending); got != want {
				t.Fatalf("pending bookings = %d, want %d", got, want)
			}
		})
	}
}

func TestPayStates(t *testing.T) {
	cases := []struct {
		name       string
		seatsUsed  int // confirmed students already in the 4-seat class
		startAs    string
		success    bool
		wantStatus string
		wantReason string
		wantErr    error
		wantRefund int
	}{
		{"EC-11_success_takes_the_last_seat", 3, booking.StatusPending, true, booking.StatusConfirmed, "", nil, 0},
		{"EC-12_failure_keeps_the_roster_clean", 1, booking.StatusPending, false, booking.StatusFailed, "", nil, 0},
		{"EC-14_success_after_the_seat_is_gone", 4, booking.StatusPending, true, booking.StatusCancelled, booking.ReasonSeatTaken, nil, 1},
		{"EC-15_pay_on_a_failed_booking", 1, booking.StatusFailed, true, "", "", booking.ErrNotAwaitingPayment, 0},
		{"EC-16_pay_on_a_cancelled_booking", 1, booking.StatusCancelled, true, "", "", booking.ErrNotAwaitingPayment, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.New(t)
			st := booking.New(f.DB)
			class := f.Class(4, tc.seatsUsed)
			id := f.Booking(f.Student(f.Parent()), class, tc.startAs)

			b, err := st.Pay(ctx, id, tc.success)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if b.Status != tc.wantStatus || b.Reason != tc.wantReason {
				t.Fatalf("status/reason = %q/%q, want %q/%q", b.Status, b.Reason, tc.wantStatus, tc.wantReason)
			}
			if n := f.Count(`SELECT COUNT(*) FROM payment_attempts WHERE booking_id = ?`, id); n != 1 {
				t.Fatalf("payment attempts = %d, want 1", n)
			}
			if n := f.Count(`SELECT COUNT(*) FROM payment_attempts WHERE booking_id = ? AND refund_required = 1`, id); n != tc.wantRefund {
				t.Fatalf("refund_required rows = %d, want %d", n, tc.wantRefund)
			}
		})
	}

	t.Run("EC-17_unknown_booking", func(t *testing.T) {
		f := fixture.New(t)
		if _, err := booking.New(f.DB).Pay(ctx, "nothing", true); !errors.Is(err, booking.ErrNotFound) {
			t.Fatalf("error = %v, want %v", err, booking.ErrNotFound)
		}
	})
}

// EC-13: a parent who submits the pay form twice must not be charged twice.
func TestDoublePayIsNoop(t *testing.T) {
	t.Run("EC-13 the pay form submitted twice", func(t *testing.T) {
		f := fixture.New(t)
		st := booking.New(f.DB)
		class := f.Class(4, 3)
		id := f.Booking(f.Student(f.Parent()), class, booking.StatusPending)

		for i := 0; i < 2; i++ {
			b, err := st.Pay(ctx, id, true)
			if err != nil || b.Status != booking.StatusConfirmed {
				t.Fatalf("submit %d: status %q err %v", i+1, b.Status, err)
			}
		}
		if n := f.Count(`SELECT COUNT(*) FROM payment_attempts WHERE booking_id = ?`, id); n != 1 {
			t.Fatalf("payment attempts = %d, want 1", n)
		}
		if n := f.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
			t.Fatalf("confirmed = %d, want 4", n)
		}
	})
}

// The brief's four steps, in order.
func TestScriptedLastSeat(t *testing.T) {
	f := fixture.New(t)
	st := booking.New(f.DB)
	class := f.Class(4, 3)

	parentA := f.Parent()
	a, err := st.Create(ctx, parentA, f.Student(parentA), class)
	if err != nil {
		t.Fatal(err)
	}
	parentB := f.Parent()
	b, err := st.Create(ctx, parentB, f.Student(parentB), class)
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := st.Pay(ctx, b.ID, true); got.Status != booking.StatusConfirmed {
		t.Fatalf("B status = %q, want confirmed", got.Status)
	}
	got, err := st.Pay(ctx, a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != booking.StatusCancelled || got.Reason != booking.ReasonSeatTaken {
		t.Fatalf("A status/reason = %q/%q, want cancelled/seat_taken", got.Status, got.Reason)
	}
	if n := f.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
		t.Fatalf("confirmed = %d, want 4", n)
	}
}

// EC-18: many payers, one seat.
func TestConcurrentLastSeat(t *testing.T) {
	t.Run("EC-18 many payers, one seat", func(t *testing.T) {
		f := fixture.New(t)
		st := booking.New(f.DB)
		class := f.Class(4, 3)

		ids := make([]string, 16)
		for i := range ids {
			p := f.Parent()
			ids[i] = f.Booking(f.Student(p), class, booking.StatusPending)
		}

		results := payTogether(t, st, ids)
		if n := countStatus(results, booking.StatusConfirmed); n != 1 {
			t.Fatalf("winners = %d, want 1", n)
		}
		if n := f.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
			t.Fatalf("confirmed = %d, want 4", n)
		}
	})
}

// EC-19: the same booking paid by two browser tabs.
func TestConcurrentSameBooking(t *testing.T) {
	t.Run("EC-19 the same booking paid twice at once", func(t *testing.T) {
		f := fixture.New(t)
		st := booking.New(f.DB)
		class := f.Class(4, 3)
		id := f.Booking(f.Student(f.Parent()), class, booking.StatusPending)

		ids := make([]string, 8)
		for i := range ids {
			ids[i] = id
		}
		payTogether(t, st, ids)

		if n := f.Count(`SELECT COUNT(*) FROM payment_attempts WHERE booking_id = ?`, id); n != 1 {
			t.Fatalf("payment attempts = %d, want 1", n)
		}
		if n := f.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
			t.Fatalf("confirmed = %d, want 4", n)
		}
	})
}

// EC-10: two identical creates at the same instant.
func TestConcurrentDuplicateCreate(t *testing.T) {
	t.Run("EC-10 two identical creates at the same instant", func(t *testing.T) {
		f := fixture.New(t)
		st := booking.New(f.DB)
		class := f.Class(4, 0)
		parent := f.Parent()
		student := f.Student(parent)

		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 8)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, errs[i] = st.Create(ctx, parent, student, class)
			}(i)
		}
		close(start)
		wg.Wait()

		var created int
		for _, err := range errs {
			switch {
			case err == nil:
				created++
			case errors.Is(err, booking.ErrDuplicate):
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if created != 1 {
			t.Fatalf("created = %d, want 1", created)
		}
	})
}

// EC-21: the seat count reads the capacity column, and 4 is not hardcoded.
func TestCapacityNeverExceeded(t *testing.T) {
	t.Run("EC-21 a class of capacity 2 and of 4", func(t *testing.T) {
		for _, capacity := range []int{2, 4} {
			t.Run(fmt.Sprintf("capacity_%d", capacity), func(t *testing.T) {
				f := fixture.New(t)
				st := booking.New(f.DB)
				class := f.Class(capacity, 0)

				ids := make([]string, 20)
				for i := range ids {
					ids[i] = f.Booking(f.Student(f.Parent()), class, booking.StatusPending)
				}
				results := payTogether(t, st, ids)

				if n := countStatus(results, booking.StatusConfirmed); n != capacity {
					t.Fatalf("confirmed = %d, want %d", n, capacity)
				}
				if n := countStatus(results, booking.StatusCancelled); n != 20-capacity {
					t.Fatalf("seat_taken = %d, want %d", n, 20-capacity)
				}
			})
		}
	})
}

// EC-23: a guard that refuses everything passes every capacity test above, so this one checks that
// two payers for two free seats both win.
func TestConcurrentDistinctSeats(t *testing.T) {
	t.Run("EC-23 two payers, two free seats", func(t *testing.T) {
		f := fixture.New(t)
		st := booking.New(f.DB)
		class := f.Class(2, 0)

		ids := []string{
			f.Booking(f.Student(f.Parent()), class, booking.StatusPending),
			f.Booking(f.Student(f.Parent()), class, booking.StatusPending),
		}
		results := payTogether(t, st, ids)
		if n := countStatus(results, booking.StatusConfirmed); n != 2 {
			t.Fatalf("confirmed = %d, want 2", n)
		}
	})
}

// EC-22: one goroutine books while another takes the last seat. Either order is fine.
func TestConcurrentCreateAndConfirm(t *testing.T) {
	t.Run("EC-22 create while another goroutine confirms", func(t *testing.T) {
		f := fixture.New(t)
		st := booking.New(f.DB)
		class := f.Class(4, 3)
		holder := f.Booking(f.Student(f.Parent()), class, booking.StatusPending)
		parent := f.Parent()
		student := f.Student(parent)

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; st.Pay(ctx, holder, true) }()
		go func() { defer wg.Done(); <-start; st.Create(ctx, parent, student, class) }()
		close(start)
		wg.Wait()

		if n := f.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
			t.Fatalf("confirmed = %d, want 4", n)
		}
	})
}

// The demo seed is data, so it is tested like data.
// Each subtest is named for its row in the brief checklist, so scripts/brief-check.sh reads the
// result from the test log and writes no SQL of its own.
func TestSeedMeetsBrief(t *testing.T) {
	f := fixture.New(t)
	if _, err := f.DB.Exec(store.Seed); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name  string
		query string
		want  int
	}{
		{"C1 a science class and a math class", `SELECT (SELECT EXISTS(SELECT 1 FROM trial_classes WHERE subject LIKE '%Math%')) + (SELECT EXISTS(SELECT 1 FROM trial_classes WHERE subject LIKE '%Science%'))`, 2},
		{"C3 every class is capped at 4", `SELECT COUNT(*) FROM trial_classes WHERE capacity != 4`, 0},
		{"S2 a class with seats available", `SELECT COUNT(*) FROM trial_classes c WHERE (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id AND b.status = 'confirmed') < c.capacity`, 2},
		{"S3 a class with exactly 3 confirmed", `SELECT COUNT(*) FROM trial_classes c WHERE (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id AND b.status = 'confirmed') = 3`, 1},
		{"S4 a child already booked, so a duplicate is reachable", `SELECT COUNT(*) FROM bookings WHERE status = 'confirmed' AND class_id = 'c1'`, 1},
		{"S5 a payment failure case", `SELECT COUNT(*) FROM bookings WHERE status = 'payment_failed'`, 1},
		{"B1 a parent with 3 children, so choosing a child is a real choice", `SELECT COUNT(*) >= 1 FROM (SELECT parent_id FROM students GROUP BY parent_id HAVING COUNT(*) >= 3)`, 1},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if got := f.Count(c.query); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// A typo in the DSN would turn a guard off without failing anything else.
func TestSchemaPragmas(t *testing.T) {
	f := fixture.New(t)
	var fk, journal string
	if err := f.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if fk != "1" {
		t.Errorf("foreign_keys = %q, want 1", fk)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
}

func payTogether(t *testing.T, st *booking.Store, ids []string) []booking.Booking {
	t.Helper()
	out := make([]booking.Booking, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			<-start
			out[i], errs[i] = st.Pay(ctx, id, true)
		}(i, id)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, booking.ErrNotAwaitingPayment) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	return out
}

func countStatus(bs []booking.Booking, status string) int {
	n := 0
	for _, b := range bs {
		if b.Status == status {
			n++
		}
	}
	return n
}
