package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/fairyhunter13/ottodot-trial-booking/internal/booking"
	"github.com/fairyhunter13/ottodot-trial-booking/internal/fixture"
	"github.com/fairyhunter13/ottodot-trial-booking/internal/web"
)

type app struct {
	*fixture.Fix
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
}

func newApp(t *testing.T) *app {
	t.Helper()
	f := fixture.New(t)
	srv := httptest.NewServer(web.NewServer(f.DB).Routes())
	t.Cleanup(srv.Close)
	return &app{Fix: f, t: t, srv: srv, client: &http.Client{
		// The test must see the 303, because the route table promises it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (a *app) get(path string) (int, string) {
	a.t.Helper()
	resp, err := a.client.Get(a.srv.URL + path)
	if err != nil {
		a.t.Fatal(err)
	}
	return read(a.t, resp)
}

func (a *app) post(path string, form url.Values) (int, string, string) {
	a.t.Helper()
	resp, err := a.client.PostForm(a.srv.URL+path, form)
	if err != nil {
		a.t.Fatal(err)
	}
	code, body := read(a.t, resp)
	return code, body, resp.Header.Get("Location")
}

func read(t *testing.T, resp *http.Response) (int, string) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func bookingID(t *testing.T, location string) string {
	t.Helper()
	id := strings.TrimPrefix(location, "/bookings/")
	if id == "" || id == location {
		t.Fatalf("no booking id in Location %q", location)
	}
	return id
}

// Every row of the route table in docs/development-plan.md.
func TestRoutesStatusCodes(t *testing.T) {
	a := newApp(t)
	parent := a.Parent()
	student := a.Student(parent)
	free := a.Class(4, 0)
	full := a.Class(4, 4)
	other := a.Student(a.Parent())
	confirmed := a.Booking(a.Student(a.Parent()), free, booking.StatusConfirmed)
	failed := a.Booking(a.Student(a.Parent()), free, booking.StatusFailed)

	book := func(s, c string) url.Values {
		return url.Values{"parent_id": {parent}, "student_id": {s}, "class_id": {c}}
	}

	cases := []struct {
		name string
		path string
		form url.Values // nil means GET
		code int
		body string
	}{
		{"index", "/", nil, 200, "Book a trial class"},
		{"index_unknown_parent", "/?parent=nobody", nil, 404, "not found"},
		{"create_ok", "/bookings", book(student, free), 303, ""},
		{"create_duplicate", "/bookings", book(student, free), 409, "already"},
		{"create_full_class", "/bookings", book(a.Student(parent), full), 409, "full"},
		{"create_unknown_child", "/bookings", book("nobody", free), 404, "not found"},
		{"create_unknown_class", "/bookings", book(a.Student(parent), "nowhere"), 404, "not found"},
		{"create_other_parents_child", "/bookings", book(other, free), 403, "not"},
		{"show_unknown_booking", "/bookings/nothing", nil, 404, "not found"},
		{"pay_already_confirmed_is_idempotent", "/bookings/" + confirmed + "/pay", url.Values{"outcome": {"success"}}, 303, ""},
		{"pay_failed_booking", "/bookings/" + failed + "/pay", url.Values{"outcome": {"success"}}, 409, "payment"},
		{"pay_unknown_booking", "/bookings/nothing/pay", url.Values{"outcome": {"success"}}, 404, "not found"},
		{"roster_unknown_class", "/classes/nowhere/roster", nil, 404, "not found"},
		{"roster_json_unknown_class", "/api/classes/nowhere/roster", nil, 404, "not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			var body string
			if tc.form == nil {
				code, body = a.get(tc.path)
			} else {
				code, body, _ = a.post(tc.path, tc.form)
			}
			if code != tc.code {
				t.Fatalf("status = %d, want %d\n%s", code, tc.code, body)
			}
			if tc.body != "" && !strings.Contains(strings.ToLower(body), strings.ToLower(tc.body)) {
				t.Fatalf("body does not mention %q:\n%s", tc.body, body)
			}
		})
	}
}

// A template that panics returns a 200 with a half-written page, so this test opens each page once.
func TestEveryTemplateRenders(t *testing.T) {
	a := newApp(t)
	parent := a.Parent()
	class := a.Class(4, 1)
	id := a.Booking(a.Student(parent), class, booking.StatusPending)

	pages := []struct{ name, path, want string }{
		{"index.html", "/?parent=" + parent, "Trial class"},
		{"booking.html", "/bookings/" + id, "Mock payment"},
		{"roster.html", "/classes/" + class + "/roster", "Teacher roster"},
		{"error.html", "/bookings/nothing", "cannot make"},
	}
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			_, body := a.get(p.path)
			if !strings.Contains(strings.ToLower(body), strings.ToLower(p.want)) {
				t.Fatalf("page does not hold %q:\n%s", p.want, body)
			}
		})
	}
}

func TestRosterJSON(t *testing.T) {
	a := newApp(t)
	class := a.Class(4, 2)
	// One row per non-confirmed status, none of which may reach the roster.
	for _, status := range []string{booking.StatusPending, booking.StatusFailed, booking.StatusCancelled} {
		a.Booking(a.Student(a.Parent()), class, status)
	}

	code, body := a.get("/api/classes/" + class + "/roster")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	var got struct {
		ClassID   string `json:"class_id"`
		Subject   string `json:"subject"`
		Capacity  int    `json:"capacity"`
		Confirmed int    `json:"confirmed"`
		SeatsLeft int    `json:"seats_left"`
		Roster    []struct {
			StudentName string `json:"student_name"`
			ParentName  string `json:"parent_name"`
			BookingID   string `json:"booking_id"`
		} `json:"roster"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if got.ClassID != class || got.Capacity != 4 || got.Confirmed != 2 || got.SeatsLeft != 2 {
		t.Fatalf("header = %+v", got)
	}
	if len(got.Roster) != 2 {
		t.Fatalf("roster holds %d rows, want 2 (confirmed only)", len(got.Roster))
	}
	for _, r := range got.Roster {
		if r.StudentName == "" || r.ParentName == "" || r.BookingID == "" {
			t.Fatalf("empty field in roster row %+v", r)
		}
	}
}

// The brief's scenario, through HTTP and not through the package.
func TestHTTPLastSeatRace(t *testing.T) {
	a := newApp(t)
	class := a.Class(4, 3)

	pA := a.Parent()
	_, _, locA := a.post("/bookings", url.Values{"parent_id": {pA}, "student_id": {a.Student(pA)}, "class_id": {class}})
	pB := a.Parent()
	_, _, locB := a.post("/bookings", url.Values{"parent_id": {pB}, "student_id": {a.Student(pB)}, "class_id": {class}})

	pay := url.Values{"outcome": {"success"}}
	if code, _, _ := a.post("/bookings/"+bookingID(t, locB)+"/pay", pay); code != http.StatusSeeOther {
		t.Fatalf("B pay status = %d, want 303", code)
	}
	if code, _, _ := a.post("/bookings/"+bookingID(t, locA)+"/pay", pay); code != http.StatusSeeOther {
		t.Fatalf("A pay status = %d, want 303", code)
	}

	_, body := a.get(locA)
	if !strings.Contains(body, booking.ReasonSeatTaken) {
		t.Fatalf("A page does not say seat_taken:\n%s", body)
	}
	if n := a.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
		t.Fatalf("confirmed = %d, want 4", n)
	}
}

// The same race with no scripted order, over 8 HTTP clients.
func TestHTTPConcurrentLastSeat(t *testing.T) {
	a := newApp(t)
	class := a.Class(4, 3)

	ids := make([]string, 8)
	for i := range ids {
		ids[i] = a.Booking(a.Student(a.Parent()), class, booking.StatusPending)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			resp, err := a.client.PostForm(a.srv.URL+"/bookings/"+id+"/pay", url.Values{"outcome": {"success"}})
			if err == nil {
				resp.Body.Close()
			}
		}(id)
	}
	close(start)
	wg.Wait()

	if n := a.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 4 {
		t.Fatalf("confirmed = %d, want 4", n)
	}
	if n := a.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND reason = ?`, class, booking.ReasonSeatTaken); n != 7 {
		t.Fatalf("seat_taken = %d, want 7", n)
	}
}

// Layer 3: the demo path end to end, and the same path as the video.
func TestParentJourney(t *testing.T) {
	a := newApp(t)
	parent := a.Parent()
	student := a.Student(parent)
	class := a.Class(4, 1)
	form := url.Values{"parent_id": {parent}, "student_id": {student}, "class_id": {class}}

	if _, body := a.get("/?parent=" + parent); !strings.Contains(body, class) {
		t.Fatalf("the class is not offered on the parent page:\n%s", body)
	}

	_, _, loc := a.post("/bookings", form)
	first := bookingID(t, loc)
	if _, _, code := a.post("/bookings/"+first+"/pay", url.Values{"outcome": {"fail"}}); code == "" {
		t.Fatal("no redirect after the failed payment")
	}
	if _, body := a.get(loc); !strings.Contains(body, booking.StatusFailed) {
		t.Fatalf("status is not payment_failed:\n%s", body)
	}
	if n := a.Count(`SELECT COUNT(*) FROM bookings WHERE class_id = ? AND status = 'confirmed'`, class); n != 1 {
		t.Fatalf("a failed payment reached the roster: confirmed = %d, want 1", n)
	}

	// The failed row leaves the partial index, so the same child books the same class again.
	_, _, loc = a.post("/bookings", form)
	second := bookingID(t, loc)
	a.post("/bookings/"+second+"/pay", url.Values{"outcome": {"success"}})

	_, body := a.get("/api/classes/" + class + "/roster")
	if !strings.Contains(body, second) {
		t.Fatalf("the confirmed booking is not on the roster:\n%s", body)
	}
	if strings.Contains(body, first) {
		t.Fatalf("the failed booking is on the roster:\n%s", body)
	}
}
