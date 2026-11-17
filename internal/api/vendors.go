package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Vendors: businesses near campus. Anyone signed in can list one (laundry, food, printing, braiding, snacks...);
// an admin approves it. Featured vendors, set up by the Cribby team, appear at the top of nearby results.
// Searches are logged (rounded to about 1 km, no user ids) so vendors can be shown real demand.

const (
	maxVendorsPerDay = 2
	maxFeaturedDays  = 365
)

var vendorCategories = map[string]bool{"laundry": true, "food": true, "internet": true, "printing": true, "braiding": true,
	"snacks": true, "tailoring": true, "barber": true, "gadgets": true, "other": true}

type vendorInput struct {
	Name         string   `json:"name" validate:"required"`
	Category     string   `json:"category" validate:"required"`
	Description  string   `json:"description"`
	Phone        string   `json:"phone" validate:"required"`
	Address      string   `json:"address"`
	Latitude     *float64 `json:"latitude" validate:"required"`
	Longitude    *float64 `json:"longitude" validate:"required"`
	StudentRun   bool     `json:"student_run"`
	FeaturedDays int      `json:"featured_days"` // admins only
}

// logSearch records what was searched near which campus. Best effort: it must never fail a search.
func (s *Server) logSearch(r *http.Request, kind, label string, lat, lng float64) {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return
	}
	if len([]rune(label)) > 80 {
		label = string([]rune(label)[:80])
	}
	ctx, ex := r.Context(), s.DB.Executor
	schoolID, _, err := store.NearestSchoolID(ctx, ex, lat, lng, 10)
	if err != nil {
		return
	}
	round := func(v float64) float64 { return float64(int(v*100+sign(v)*0.5)) / 100 }
	_ = store.RecordSearch(ctx, ex, kind, label, schoolID, round(lat), round(lng))
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func (s *Server) listVendors(w http.ResponseWriter, r *http.Request) error {
	q := newQuery(r)
	f := store.VendorSearch{Category: q.str("category"), Q: strings.TrimSpace(q.str("q")),
		NearLat: q.optFloat("near_lat"), NearLng: q.optFloat("near_lng"), RadiusKM: q.optFloat("radius_km")}
	schoolID := q.optInt("school_id")
	limit := 40
	if n := q.optInt("limit"); n != nil && *n > 0 && *n <= 100 {
		limit = *n
	}
	if err := q.err(); err != nil {
		return err
	}
	if f.NearLat == nil && f.NearLng == nil && schoolID != nil {
		lat, lng, err := s.schoolOrigin(r.Context(), schoolID)
		if err != nil {
			return err
		}
		f.NearLat, f.NearLng = lat, lng
	}
	if f.Category != "" && !vendorCategories[f.Category] {
		return httpErr(http.StatusUnprocessableEntity, "Unknown category")
	}
	items, err := store.SearchVendors(r.Context(), s.DB.Executor, f, limit)
	if err != nil {
		return err
	}
	if f.NearLat != nil && f.NearLng != nil {
		label := f.Category
		if f.Q != "" {
			label = f.Q
		}
		s.logSearch(r, "vendor", label, *f.NearLat, *f.NearLng)
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}

func (s *Server) myVendors(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	out, err := store.ListVendorsByOwner(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) createVendor(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var in vendorInput
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	in.Name, in.Description, in.Address = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.Address)
	if n := len([]rune(in.Name)); n < 2 || n > 120 {
		return httpErr(http.StatusUnprocessableEntity, "Give the business a name")
	}
	if !vendorCategories[in.Category] {
		return httpErr(http.StatusUnprocessableEntity, "Choose a category")
	}
	if len([]rune(in.Description)) > 500 || len([]rune(in.Address)) > 200 {
		return httpErr(http.StatusUnprocessableEntity, "The description or address is too long")
	}
	if in.Phone = normalizePhone(in.Phone); in.Phone == "" {
		return httpErr(http.StatusUnprocessableEntity, "Enter a phone number customers can reach")
	}
	if *in.Latitude < -90 || *in.Latitude > 90 || *in.Longitude < -180 || *in.Longitude > 180 {
		return httpErr(http.StatusUnprocessableEntity, "That location isn't valid")
	}
	ctx, ex := r.Context(), s.DB.Executor
	v := model.Vendor{Name: in.Name, Category: in.Category, Description: in.Description, Phone: in.Phone, Address: in.Address,
		Latitude: *in.Latitude, Longitude: *in.Longitude, StudentRun: in.StudentRun}
	owner := user.ID
	v.OwnerID = &owner
	if user.IsAdmin {
		// The Cribby team adds businesses directly, and can feature them.
		v.Status = "approved"
		if in.FeaturedDays < 0 || in.FeaturedDays > maxFeaturedDays {
			return httpErr(http.StatusUnprocessableEntity, "Featured days must be 0 to 365")
		}
		if in.FeaturedDays > 0 {
			until := db.Time{Time: time.Now().UTC().Add(time.Duration(in.FeaturedDays) * 24 * time.Hour)}
			v.FeaturedUntil = &until
		}
	} else {
		n, err := store.CountRecentVendorsByOwner(ctx, ex, user.ID, db.Time{Time: time.Now().UTC().Add(-24 * time.Hour)})
		if err != nil {
			return err
		}
		if n >= maxVendorsPerDay {
			return httpErr(http.StatusTooManyRequests, "You've sent a few businesses today. Please try again tomorrow")
		}
	}
	id, err := store.CreateVendor(ctx, ex, v)
	if err != nil {
		return err
	}
	created, err := store.VendorByID(ctx, ex, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *Server) adminListVendors(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" && status != "removed" {
		return httpErr(http.StatusUnprocessableEntity, "Unknown status")
	}
	out, err := store.ListVendorsByStatus(r.Context(), s.DB.Executor, status)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminReviewVendor(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "vendor_id")
	if err != nil {
		return err
	}
	p := struct {
		Decision string `json:"decision" validate:"required,oneof=approve|reject|remove"`
		Note     string `json:"note"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	note := strings.TrimSpace(p.Note)
	if len([]rune(note)) > maxReviewNoteLen {
		return httpErr(http.StatusUnprocessableEntity, "Note is too long (500 characters max)")
	}
	if p.Decision != "approve" && note == "" {
		return httpErr(http.StatusUnprocessableEntity, "Say why")
	}
	ctx, ex := r.Context(), s.DB.Executor
	v, err := store.VendorByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if v == nil {
		return httpErr(http.StatusNotFound, "Business not found")
	}
	status := map[string]string{"approve": "approved", "reject": "rejected", "remove": "removed"}[p.Decision]
	if err := store.DecideVendor(ctx, ex, id, status, note); err != nil {
		return err
	}
	v, _ = store.VendorByID(ctx, ex, id)
	writeJSON(w, http.StatusOK, v)
	return nil
}

// adminFeatureVendor: PUT /admin/vendors/{id}/featured {"days": 30}. 0 removes the featured spot.
func (s *Server) adminFeatureVendor(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "vendor_id")
	if err != nil {
		return err
	}
	p := struct {
		Days int `json:"days"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if p.Days < 0 || p.Days > maxFeaturedDays {
		return httpErr(http.StatusUnprocessableEntity, "Days must be 0 to 365")
	}
	ctx, ex := r.Context(), s.DB.Executor
	v, err := store.VendorByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if v == nil {
		return httpErr(http.StatusNotFound, "Business not found")
	}
	var until *db.Time
	if p.Days > 0 {
		t := db.Time{Time: time.Now().UTC().Add(time.Duration(p.Days) * 24 * time.Hour)}
		until = &t
	}
	if err := store.SetVendorFeaturedUntil(ctx, ex, id, until); err != nil {
		return err
	}
	v, _ = store.VendorByID(ctx, ex, id)
	writeJSON(w, http.StatusOK, v)
	return nil
}

type demandRow struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type demandCampus struct {
	SchoolID int         `json:"school_id"`
	School   string      `json:"school"`
	Searches int         `json:"searches"`
	Top      []demandRow `json:"top"`
}

func topLabels(counts map[string]int, n int) []demandRow {
	out := make([]demandRow, 0, len(counts))
	for l, c := range counts {
		out = append(out, demandRow{l, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// adminDemand: what people searched for in the last N days, overall and near each campus.
func (s *Server) adminDemand(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	q := newQuery(r)
	days := 30
	if n := q.optInt("days"); n != nil && *n >= 1 && *n <= 365 {
		days = *n
	}
	if err := q.err(); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	events, err := store.SearchEventsSince(ctx, ex, db.Time{Time: time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)})
	if err != nil {
		return err
	}
	overall := map[string]int{}
	per := map[int]map[string]int{}
	totals := map[int]int{}
	for _, e := range events {
		overall[e.Label]++
		if e.SchoolID != nil {
			if per[*e.SchoolID] == nil {
				per[*e.SchoolID] = map[string]int{}
			}
			per[*e.SchoolID][e.Label]++
			totals[*e.SchoolID]++
		}
	}
	campuses := make([]demandCampus, 0, len(per))
	for id, counts := range per {
		name := ""
		if sch, err := store.SchoolByID(ctx, ex, id); err == nil && sch != nil {
			name = sch.Name
		}
		campuses = append(campuses, demandCampus{SchoolID: id, School: name, Searches: totals[id], Top: topLabels(counts, 5)})
	}
	sort.Slice(campuses, func(i, j int) bool { return campuses[i].Searches > campuses[j].Searches })
	if len(campuses) > 20 {
		campuses = campuses[:20]
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "total": len(events), "top": topLabels(overall, 15), "campuses": campuses})
	return nil
}
