package store

import (
	"context"
	"sort"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
)

const vendorCols = `id, owner_id, name, category, description, phone, address, latitude, longitude, student_run, status, review_note, featured_until, created_at`

func scanVendor(s rowScanner) (model.Vendor, error) {
	var v model.Vendor
	err := s.Scan(&v.ID, &v.OwnerID, &v.Name, &v.Category, &v.Description, &v.Phone, &v.Address, &v.Latitude, &v.Longitude,
		&v.StudentRun, &v.Status, &v.ReviewNote, &v.FeaturedUntil, &v.CreatedAt)
	v.Featured = v.FeaturedUntil != nil && v.FeaturedUntil.After(time.Now())
	return v, err
}

func CreateVendor(ctx context.Context, ex db.Executor, v model.Vendor) (int, error) {
	status := v.Status
	if status == "" {
		status = "pending"
	}
	return ex.InsertID(ctx, `INSERT INTO vendors (owner_id, name, category, description, phone, address, latitude, longitude, student_run,
		status, review_note, featured_until, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,'',?,?)`,
		v.OwnerID, v.Name, v.Category, v.Description, v.Phone, v.Address, v.Latitude, v.Longitude, v.StudentRun, status, v.FeaturedUntil, db.Now())
}

func VendorByID(ctx context.Context, ex db.Executor, id int) (*model.Vendor, error) {
	return queryOne(ctx, ex, scanVendor, `SELECT `+vendorCols+` FROM vendors WHERE id = ?`, id)
}

func CountRecentVendorsByOwner(ctx context.Context, ex db.Executor, ownerID int, since db.Time) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM vendors WHERE owner_id = ? AND created_at >= ?`, ownerID, since).Scan(&n)
	return n, err
}

func ListVendorsByOwner(ctx context.Context, ex db.Executor, ownerID int) ([]model.Vendor, error) {
	return queryList(ctx, ex, scanVendor, `SELECT `+vendorCols+` FROM vendors WHERE owner_id = ? AND status <> 'removed' ORDER BY created_at DESC, id DESC`, ownerID)
}

func ListVendorsByStatus(ctx context.Context, ex db.Executor, status string) ([]model.Vendor, error) {
	return queryList(ctx, ex, scanVendor, `SELECT `+vendorCols+` FROM vendors WHERE status = ? ORDER BY created_at DESC, id DESC LIMIT 300`, status)
}

func DecideVendor(ctx context.Context, ex db.Executor, id int, status, note string) error {
	_, err := ex.Exec(ctx, `UPDATE vendors SET status = ?, review_note = ? WHERE id = ?`, status, note, id)
	return err
}

// SetVendorFeaturedUntil puts the vendor at the top of nearby results until then; nil removes the spot.
func SetVendorFeaturedUntil(ctx context.Context, ex db.Executor, id int, until *db.Time) error {
	_, err := ex.Exec(ctx, `UPDATE vendors SET featured_until = ? WHERE id = ?`, until, id)
	return err
}

type VendorSearch struct {
	Category, Q      string
	NearLat, NearLng *float64
	RadiusKM         *float64
}

// SearchVendors returns approved vendors: featured first, then nearest (or newest when there is no point).
func SearchVendors(ctx context.Context, ex db.Executor, f VendorSearch, limit int) ([]*model.Vendor, error) {
	where, args := []string{"status = ?"}, []any{"approved"}
	if f.Category != "" {
		where, args = append(where, "category = ?"), append(args, f.Category)
	}
	if f.Q != "" {
		where, args = append(where, "("+ilike("name")+" OR "+ilike("description")+")"), append(args, contains(f.Q), contains(f.Q))
	}
	rows, err := queryList(ctx, ex, scanVendor, `SELECT `+vendorCols+` FROM vendors WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := make([]*model.Vendor, len(rows))
	for i := range rows {
		items[i] = &rows[i]
	}
	if f.NearLat != nil && f.NearLng != nil {
		items = geo.RankByDistance(items, *f.NearLat, *f.NearLng, f.RadiusKM)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Featured && !items[j].Featured })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// ---- search demand ----

func RecordSearch(ctx context.Context, ex db.Executor, kind, label string, schoolID *int, lat, lng float64) error {
	_, err := ex.Exec(ctx, `INSERT INTO search_events (kind, label, school_id, lat, lng, created_at) VALUES (?,?,?,?,?,?)`,
		kind, label, schoolID, lat, lng, db.Now())
	return err
}

// SearchEvent is one logged search.
type SearchEvent struct {
	Kind, Label string
	SchoolID    *int
}

func SearchEventsSince(ctx context.Context, ex db.Executor, since db.Time) ([]SearchEvent, error) {
	return queryList(ctx, ex, func(s rowScanner) (SearchEvent, error) {
		var e SearchEvent
		err := s.Scan(&e.Kind, &e.Label, &e.SchoolID)
		return e, err
	}, `SELECT kind, label, school_id FROM search_events WHERE created_at >= ? ORDER BY id DESC LIMIT 50000`, since)
}

// NearestSchoolID finds the school closest to the point within maxKM, or nil.
func NearestSchoolID(ctx context.Context, ex db.Executor, lat, lng, maxKM float64) (*int, string, error) {
	type pt struct {
		id   int
		name string
		lat  float64
		lng  float64
	}
	rows, err := queryList(ctx, ex, func(s rowScanner) (pt, error) {
		var p pt
		err := s.Scan(&p.id, &p.name, &p.lat, &p.lng)
		return p, err
	}, `SELECT id, name, latitude, longitude FROM schools WHERE latitude IS NOT NULL AND longitude IS NOT NULL`)
	if err != nil {
		return nil, "", err
	}
	best, bestKM := -1, maxKM
	for i, p := range rows {
		if km := geo.HaversineKM(lat, lng, p.lat, p.lng); km <= bestKM {
			best, bestKM = i, km
		}
	}
	if best < 0 {
		return nil, "", nil
	}
	id := rows[best].id
	return &id, rows[best].name, nil
}
