package api

import (
	"context"
	"net/http"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// listSchools is the school directory behind the "choose your school" picker.
// Search by name/town/state with q, narrow with type (university | polytechnic |
// college_of_education) or state, and page with limit (default 100, max 500) and
// offset.
func (s *Server) listSchools(w http.ResponseWriter, r *http.Request) error {
	q := newQuery(r)
	f := store.SchoolSearch{Q: q.str("q"), Type: q.str("type"), State: q.str("state"), Limit: 100}
	if n := q.optInt("limit"); n != nil {
		f.Limit = *n
	}
	if n := q.optInt("offset"); n != nil {
		f.Offset = *n
	}
	if err := q.err(); err != nil {
		return err
	}
	if f.Limit < 1 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	out, err := store.SearchSchools(r.Context(), s.DB.Executor, f)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getSchool(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt(r, "school_id")
	if err != nil {
		return err
	}
	school, err := store.SchoolByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return err
	}
	if school == nil {
		return httpErr(http.StatusNotFound, "School not found")
	}
	writeJSON(w, http.StatusOK, s.refineSchool(r.Context(), school))
	return nil
}

// schoolOrigin resolves the optional ?school_id= used by the listing endpoints.
// It returns the school's coordinates, or nil when the parameter is absent or
// the school has no coordinates (distances are then simply not computed).
func (s *Server) schoolOrigin(ctx context.Context, id *int) (lat, lng *float64, err error) {
	if id == nil {
		return nil, nil, nil
	}
	school, err := store.SchoolByID(ctx, s.DB.Executor, *id)
	if err != nil {
		return nil, nil, err
	}
	if school == nil {
		return nil, nil, httpErr(http.StatusNotFound, "School not found")
	}
	return school.Latitude, school.Longitude, nil
}

var _ = model.School{}
