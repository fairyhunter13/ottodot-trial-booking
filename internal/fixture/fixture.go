// Package fixture builds a throwaway database per test. Tests state the state they need and
// nothing else, so no test depends on the demo seed.
package fixture

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/fairyhunter13/ottodot-trial-booking/internal/store"
)

type Fix struct {
	T  *testing.T
	DB *sql.DB
	n  int
}

// New opens an empty schema and registers the invariant check as cleanup.
func New(t *testing.T) *Fix {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	f := &Fix{T: t, DB: db}
	t.Cleanup(func() {
		AssertInvariants(t, db)
		db.Close()
	})
	return f
}

func (f *Fix) id(prefix string) string {
	f.n++
	return fmt.Sprintf("%s%d", prefix, f.n)
}

func (f *Fix) exec(query string, args ...any) {
	f.T.Helper()
	if _, err := f.DB.Exec(query, args...); err != nil {
		f.T.Fatal(err)
	}
}

func (f *Fix) Parent() string {
	id := f.id("p")
	f.exec(`INSERT INTO parents (id, name, email) VALUES (?, ?, ?)`, id, "Parent "+id, id+"@example.test")
	return id
}

func (f *Fix) Student(parentID string) string {
	id := f.id("s")
	f.exec(`INSERT INTO students (id, parent_id, name, grade) VALUES (?, ?, ?, 'Grade 4')`, id, parentID, "Child "+id)
	return id
}

// Class creates a class of the given capacity that already holds confirmed students.
func (f *Fix) Class(capacity, confirmed int) string {
	id := f.ClassAt("+2 days", capacity)
	for i := 0; i < confirmed; i++ {
		f.Booking(f.Student(f.Parent()), id, "confirmed")
	}
	return id
}

func (f *Fix) ClassAt(offset string, capacity int) string {
	id := f.id("c")
	f.exec(`INSERT INTO trial_classes (id, subject, starts_at, capacity) VALUES (?, 'Science Trial', datetime('now', ?), ?)`,
		id, offset, capacity)
	return id
}

func (f *Fix) Booking(studentID, classID, status string) string {
	id := f.id("b")
	f.exec(`INSERT INTO bookings (id, student_id, class_id, status, reason, created_at, updated_at)
	        VALUES (?, ?, ?, ?, '', datetime('now'), datetime('now'))`, id, studentID, classID, status)
	return id
}

func (f *Fix) Count(query string, args ...any) int {
	f.T.Helper()
	var n int
	if err := f.DB.QueryRow(query, args...).Scan(&n); err != nil {
		f.T.Fatal(err)
	}
	return n
}

// AssertInvariants fails the test when either database rule is broken, whatever the test was
// actually looking at.
func AssertInvariants(t *testing.T, db *sql.DB) {
	t.Helper()
	var over, dup int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT c.id FROM trial_classes c
			  JOIN bookings b ON b.class_id = c.id AND b.status = 'confirmed'
			 GROUP BY c.id HAVING COUNT(*) > c.capacity)`).Scan(&over)
	if err != nil {
		t.Fatal(err)
	}
	err = db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT student_id, class_id FROM bookings
			 WHERE status IN ('pending_payment','confirmed')
			 GROUP BY student_id, class_id HAVING COUNT(*) > 1)`).Scan(&dup)
	if err != nil {
		t.Fatal(err)
	}
	if over != 0 {
		t.Errorf("invariant broken: %d class(es) hold more confirmed students than capacity", over)
	}
	if dup != 0 {
		t.Errorf("invariant broken: %d child/class pair(s) hold two live bookings", dup)
	}
}
