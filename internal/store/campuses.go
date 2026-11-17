package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const campusCols = `c.slug, c.whatsapp_url, c.tagline, c.active, c.created_at, ` +
	`s.id, s.name, s.type, s.state, s.town, s.funding, s.latitude, s.longitude, s.confidence, s.source, s.google_place_id`

func scanCampus(s rowScanner) (model.Campus, error) {
	var c model.Campus
	err := s.Scan(&c.Slug, &c.WhatsAppURL, &c.Tagline, &c.Active, &c.CreatedAt,
		&c.School.ID, &c.School.Name, &c.School.Type, &c.School.State, &c.School.Town, &c.School.Funding,
		&c.School.Latitude, &c.School.Longitude, &c.School.Confidence, &c.School.Source, &c.School.GooglePlaceID)
	return c, err
}

const campusFrom = ` FROM campuses c JOIN schools s ON s.id = c.school_id `

func CampusBySlug(ctx context.Context, ex db.Executor, slug string) (*model.Campus, error) {
	return queryOne(ctx, ex, scanCampus, `SELECT `+campusCols+campusFrom+`WHERE c.slug = ?`, slug)
}

// ListCampuses returns campus pages, newest last. activeOnly hides switched-off ones (the public list).
func ListCampuses(ctx context.Context, ex db.Executor, activeOnly bool) ([]model.Campus, error) {
	q := `SELECT ` + campusCols + campusFrom
	args := []any{}
	if activeOnly {
		q, args = q+`WHERE c.active = ? `, append(args, true)
	}
	return queryList(ctx, ex, scanCampus, q+`ORDER BY c.created_at, c.slug`, args...)
}

func UpsertCampus(ctx context.Context, ex db.Executor, slug string, schoolID int, whatsapp, tagline string, active bool) error {
	res, err := ex.Exec(ctx, `UPDATE campuses SET school_id = ?, whatsapp_url = ?, tagline = ?, active = ? WHERE slug = ?`,
		schoolID, whatsapp, tagline, active, slug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO campuses (slug, school_id, whatsapp_url, tagline, active, created_at) VALUES (?,?,?,?,?,?)`,
		slug, schoolID, whatsapp, tagline, active, db.Now())
	return err
}

func DeleteCampus(ctx context.Context, ex db.Executor, slug string) error {
	_, err := ex.Exec(ctx, `DELETE FROM campuses WHERE slug = ?`, slug)
	return err
}
