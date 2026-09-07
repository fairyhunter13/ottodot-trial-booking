// Package booking holds the trial booking rules: create, pay, roster.
package booking

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Statuses. The brief names these four.
const (
	StatusPending   = "pending_payment"
	StatusConfirmed = "confirmed"
	StatusFailed    = "payment_failed"
	StatusCancelled = "cancelled"

	ReasonSeatTaken = "seat_taken"

	TrialPriceCents = 2500
)

var (
	ErrNotFound           = errors.New("not found")
	ErrForbidden          = errors.New("child does not belong to this parent")
	ErrDuplicate          = errors.New("this child already has a live booking for this class")
	ErrClassFull          = errors.New("class is full")
	ErrClassStarted       = errors.New("class has already started")
	ErrNotAwaitingPayment = errors.New("booking is not awaiting payment")
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

type Booking struct {
	ID          string
	StudentID   string
	StudentName string
	ClassID     string
	ClassName   string
	Status      string
	Reason      string
}

type Class struct {
	ID        string
	Subject   string
	StartsAt  string
	Capacity  int
	Confirmed int
}

func (c Class) SeatsLeft() int { return c.Capacity - c.Confirmed }
func (c Class) Full() bool     { return c.SeatsLeft() <= 0 }

type Student struct {
	ID, Name, Grade string
}

type Parent struct {
	ID, Name string
	Children []Student
}

type RosterEntry struct {
	BookingID   string `json:"booking_id"`
	StudentID   string `json:"student_id"`
	StudentName string `json:"student_name"`
	ParentName  string `json:"parent_name"`
}

// Create opens a booking in pending_payment. It holds no seat: the seat is counted at confirm time.
func (s *Store) Create(ctx context.Context, parentID, studentID, classID string) (Booking, error) {
	var ownerID string
	err := s.db.QueryRowContext(ctx, `SELECT parent_id FROM students WHERE id = ?`, studentID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, fmt.Errorf("student %q: %w", studentID, ErrNotFound)
	}
	if err != nil {
		return Booking{}, err
	}
	if ownerID != parentID {
		return Booking{}, ErrForbidden
	}

	var capacity, confirmed, started int
	err = s.db.QueryRowContext(ctx, `
		SELECT c.capacity,
		       (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id AND b.status = 'confirmed'),
		       (c.starts_at <= datetime('now'))
		  FROM trial_classes c WHERE c.id = ?`, classID).Scan(&capacity, &confirmed, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, fmt.Errorf("class %q: %w", classID, ErrNotFound)
	}
	if err != nil {
		return Booking{}, err
	}
	if started == 1 {
		return Booking{}, ErrClassStarted
	}
	if confirmed >= capacity {
		return Booking{}, ErrClassFull
	}

	id := newID("bk")
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO bookings (id, student_id, class_id, status, reason, created_at, updated_at)
		VALUES (?, ?, ?, 'pending_payment', '', datetime('now'), datetime('now'))`, id, studentID, classID)
	if isUniqueViolation(err) {
		return Booking{}, ErrDuplicate
	}
	if err != nil {
		return Booking{}, err
	}
	return s.Get(ctx, id)
}

// Pay records a payment attempt and settles the booking.
//
// The seat count and the status change run inside one BEGIN IMMEDIATE transaction, so two payers
// cannot both win the last seat. The loser is cancelled with reason seat_taken, and the attempt is
// flagged for a refund.
func (s *Store) Pay(ctx context.Context, bookingID string, success bool) (Booking, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return Booking{}, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return Booking{}, err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()

	var status, classID string
	err = conn.QueryRowContext(ctx, `SELECT status, class_id FROM bookings WHERE id = ?`, bookingID).Scan(&status, &classID)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, fmt.Errorf("booking %q: %w", bookingID, ErrNotFound)
	}
	if err != nil {
		return Booking{}, err
	}
	// A second submit of the same form changes nothing and charges nothing.
	if status == StatusConfirmed {
		return s.Get(ctx, bookingID)
	}
	if status != StatusPending {
		return Booking{}, ErrNotAwaitingPayment
	}

	if !success {
		if err := settle(ctx, conn, bookingID, StatusFailed, "", "failed", false); err != nil {
			return Booking{}, err
		}
		return commitAndGet(ctx, s, conn, &committed, bookingID)
	}

	var capacity, confirmed int
	err = conn.QueryRowContext(ctx, `
		SELECT c.capacity, (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id AND b.status = 'confirmed')
		  FROM trial_classes c WHERE c.id = ?`, classID).Scan(&capacity, &confirmed)
	if err != nil {
		return Booking{}, err
	}

	if confirmed < capacity {
		err = settle(ctx, conn, bookingID, StatusConfirmed, "", "succeeded", false)
	} else {
		err = settle(ctx, conn, bookingID, StatusCancelled, ReasonSeatTaken, "succeeded", true)
	}
	if err != nil {
		return Booking{}, err
	}
	return commitAndGet(ctx, s, conn, &committed, bookingID)
}

// settle writes the booking status and the payment attempt in one step.
// The status guard in the UPDATE is what makes a repeated submit a no-op.
func settle(ctx context.Context, conn *sql.Conn, bookingID, status, reason, outcome string, refund bool) error {
	res, err := conn.ExecContext(ctx, `
		UPDATE bookings SET status = ?, reason = ?, updated_at = datetime('now')
		 WHERE id = ? AND status = 'pending_payment'`, status, reason, bookingID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotAwaitingPayment
	}
	_, err = conn.ExecContext(ctx, `
		INSERT INTO payment_attempts (id, booking_id, outcome, amount_cents, refund_required, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))`, newID("pa"), bookingID, outcome, TrialPriceCents, boolInt(refund))
	return err
}

func commitAndGet(ctx context.Context, s *Store, conn *sql.Conn, committed *bool, id string) (Booking, error) {
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return Booking{}, err
	}
	*committed = true
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (Booking, error) {
	var b Booking
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id, b.student_id, st.name, b.class_id, c.subject, b.status, b.reason
		  FROM bookings b
		  JOIN students st ON st.id = b.student_id
		  JOIN trial_classes c ON c.id = b.class_id
		 WHERE b.id = ?`, id).
		Scan(&b.ID, &b.StudentID, &b.StudentName, &b.ClassID, &b.ClassName, &b.Status, &b.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, fmt.Errorf("booking %q: %w", id, ErrNotFound)
	}
	return b, err
}

func (s *Store) Classes(ctx context.Context) ([]Class, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.subject, c.starts_at, c.capacity,
		       (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id AND b.status = 'confirmed')
		  FROM trial_classes c ORDER BY c.starts_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Class
	for rows.Next() {
		var c Class
		if err := rows.Scan(&c.ID, &c.Subject, &c.StartsAt, &c.Capacity, &c.Confirmed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Parent(ctx context.Context, id string) (Parent, error) {
	var p Parent
	err := s.db.QueryRowContext(ctx, `SELECT id, name FROM parents WHERE id = ?`, id).Scan(&p.ID, &p.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Parent{}, fmt.Errorf("parent %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return Parent{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, grade FROM students WHERE parent_id = ? ORDER BY name`, id)
	if err != nil {
		return Parent{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Student
		if err := rows.Scan(&c.ID, &c.Name, &c.Grade); err != nil {
			return Parent{}, err
		}
		p.Children = append(p.Children, c)
	}
	return p, rows.Err()
}

func (s *Store) Parents(ctx context.Context) ([]Parent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM parents ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Parent
	for rows.Next() {
		var p Parent
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Roster lists the confirmed children only. A pending, failed or cancelled booking never appears.
func (s *Store) Roster(ctx context.Context, classID string) (Class, []RosterEntry, error) {
	var c Class
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.subject, c.starts_at, c.capacity,
		       (SELECT COUNT(*) FROM bookings b WHERE b.class_id = c.id AND b.status = 'confirmed')
		  FROM trial_classes c WHERE c.id = ?`, classID).
		Scan(&c.ID, &c.Subject, &c.StartsAt, &c.Capacity, &c.Confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return Class{}, nil, fmt.Errorf("class %q: %w", classID, ErrNotFound)
	}
	if err != nil {
		return Class{}, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, st.id, st.name, p.name
		  FROM bookings b
		  JOIN students st ON st.id = b.student_id
		  JOIN parents p ON p.id = st.parent_id
		 WHERE b.class_id = ? AND b.status = 'confirmed'
		 ORDER BY st.name`, classID)
	if err != nil {
		return Class{}, nil, err
	}
	defer rows.Close()
	entries := []RosterEntry{}
	for rows.Next() {
		var e RosterEntry
		if err := rows.Scan(&e.BookingID, &e.StudentID, &e.StudentName, &e.ParentName); err != nil {
			return Class{}, nil, err
		}
		entries = append(entries, e)
	}
	return c, entries, rows.Err()
}

func isUniqueViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && e.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

func newID(prefix string) string {
	var b [8]byte
	rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
