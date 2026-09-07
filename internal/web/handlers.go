// Package web serves the parent booking pages and the roster.
package web

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"

	"github.com/fairyhunter13/ottodot-trial-booking/internal/booking"
)

//go:embed templates/*.html
var files embed.FS

type Server struct {
	st  *booking.Store
	tpl *template.Template
}

func NewServer(db *sql.DB) *Server {
	return &Server{st: booking.New(db), tpl: template.Must(template.ParseFS(files, "templates/*.html"))}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /bookings", s.create)
	mux.HandleFunc("GET /bookings/{id}", s.show)
	mux.HandleFunc("POST /bookings/{id}/pay", s.pay)
	mux.HandleFunc("GET /classes/{id}/roster", s.roster)
	mux.HandleFunc("GET /api/classes/{id}/roster", s.rosterJSON)
	return mux
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	parents, err := s.st.Parents(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	id := r.URL.Query().Get("parent")
	if id == "" && len(parents) > 0 {
		id = parents[0].ID
	}
	parent, err := s.st.Parent(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	classes, err := s.st.Classes(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "index.html", http.StatusOK, map[string]any{
		"Parents": parents, "Parent": parent, "Classes": classes,
	})
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	b, err := s.st.Create(r.Context(), r.FormValue("parent_id"), r.FormValue("student_id"), r.FormValue("class_id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/bookings/"+b.ID, http.StatusSeeOther)
}

func (s *Server) show(w http.ResponseWriter, r *http.Request) {
	b, err := s.st.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "booking.html", http.StatusOK, map[string]any{"Booking": b})
}

func (s *Server) pay(w http.ResponseWriter, r *http.Request) {
	_, err := s.st.Pay(r.Context(), r.PathValue("id"), r.FormValue("outcome") == "success")
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/bookings/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) roster(w http.ResponseWriter, r *http.Request) {
	class, entries, err := s.st.Roster(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "roster.html", http.StatusOK, map[string]any{"Class": class, "Entries": entries})
}

func (s *Server) rosterJSON(w http.ResponseWriter, r *http.Request) {
	class, entries, err := s.st.Roster(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"class_id": class.ID, "subject": class.Subject, "capacity": class.Capacity,
		"confirmed": class.Confirmed, "seats_left": class.SeatsLeft(), "roster": entries,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, code int, data any) {
	w.WriteHeader(code)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// fail turns a rule error into the status code for that rule.
func (s *Server) fail(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, booking.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, booking.ErrForbidden):
		code = http.StatusForbidden
	case errors.Is(err, booking.ErrDuplicate), errors.Is(err, booking.ErrClassFull),
		errors.Is(err, booking.ErrClassStarted), errors.Is(err, booking.ErrNotAwaitingPayment):
		code = http.StatusConflict
	}
	s.render(w, "error.html", code, map[string]any{"Message": err.Error()})
}
