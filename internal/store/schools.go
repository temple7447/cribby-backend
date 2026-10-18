package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/schools"
)

const schoolCols = `id, name, type, state, town, funding, latitude, longitude, confidence, source, google_place_id`

func scanSchool(s rowScanner) (model.School, error) {
	var x model.School
	err := s.Scan(&x.ID, &x.Name, &x.Type, &x.State, &x.Town, &x.Funding, &x.Latitude, &x.Longitude, &x.Confidence, &x.Source, &x.GooglePlaceID)
	return x, err
}

func SchoolByID(ctx context.Context, ex db.Executor, id int) (*model.School, error) {
	return queryOne(ctx, ex, scanSchool, `SELECT `+schoolCols+` FROM schools WHERE id = ?`, id)
}

type SchoolSearch struct {
	Q, Type, State string
	Limit, Offset  int
}

// SearchSchools matches Q against name, town and state. Schools with real
// coordinates are listed first only when searching is not needed; otherwise
// plain alphabetical order.
func SearchSchools(ctx context.Context, ex db.Executor, f SchoolSearch) ([]model.School, error) {
	var where []string
	var args []any
	if f.Q != "" {
		where = append(where, "("+ilike("name")+" OR "+ilike("town")+" OR "+ilike("state")+")")
		args = append(args, contains(f.Q), contains(f.Q), contains(f.Q))
	}
	if f.Type != "" {
		where, args = append(where, "type = ?"), append(args, f.Type)
	}
	if f.State != "" {
		where, args = append(where, ilike("state")), append(args, f.State)
	}
	q := `SELECT ` + schoolCols + ` FROM schools`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY name LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	return queryList(ctx, ex, scanSchool, q, args...)
}

// SyncSchools loads the embedded dataset: inserts schools that are missing and
// refreshes the rest (matched by name, so ids stay stable). It runs once per
// dataset version, recorded in go_migrations, so booting is cheap.
func SyncSchools(ctx context.Context, d *db.DB) (loaded bool, err error) {
	name := "schools_data_" + schools.Version()
	err = d.InTx(ctx, func(tx db.Executor) error {
		done, err := d.MarkApplied(ctx, tx, name)
		if err != nil || done {
			return err
		}
		rows, err := schools.Rows()
		if err != nil {
			return err
		}
		for _, r := range rows {
			var id int
			err := tx.QueryRow(ctx, `SELECT id FROM schools WHERE name = ?`, r.Name).Scan(&id)
			switch {
			case err == nil:
				// Existing school: refresh it, but never overwrite one a Google lookup pinned.
				if _, err := tx.Exec(ctx, `UPDATE schools SET type=?, state=?, town=?, funding=?, latitude=?, longitude=?, confidence=?, source=?
					WHERE id=? AND source <> 'google'`, r.Type, r.State, r.Town, r.Funding, r.Latitude, r.Longitude, r.Confidence, r.Source, id); err != nil {
					return err
				}
			case errors.Is(err, sql.ErrNoRows):
				if _, err := tx.Exec(ctx, `INSERT INTO schools (name, type, state, town, funding, latitude, longitude, confidence, source)
					VALUES (?,?,?,?,?,?,?,?,?)`, r.Name, r.Type, r.State, r.Town, r.Funding, r.Latitude, r.Longitude, r.Confidence, r.Source); err != nil {
					return err
				}
			default:
				return err
			}
		}
		loaded = true
		return nil
	})
	return loaded, err
}

// RefineSchool pins a school to an exact Google location and links its Google
// place. Later dataset reloads leave google-sourced rows alone.
func RefineSchool(ctx context.Context, ex db.Executor, id int, lat, lng float64, placeID string) error {
	_, err := ex.Exec(ctx, `UPDATE schools SET latitude=?, longitude=?, confidence='high', source='google', google_place_id=? WHERE id=?`,
		lat, lng, placeID, id)
	return err
}
