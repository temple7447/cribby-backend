package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
)

const placeCols = `id, name, category, address, latitude, longitude, emoji, created_at`

func scanPlace(s rowScanner) (model.Place, error) {
	var p model.Place
	err := s.Scan(&p.ID, &p.Name, &p.Category, &p.Address, &p.Latitude, &p.Longitude, &p.Emoji, &p.CreatedAt)
	return p, err
}

func PlaceByID(ctx context.Context, ex db.Executor, id int) (*model.Place, error) {
	return queryOne(ctx, ex, scanPlace, `SELECT `+placeCols+` FROM places WHERE id = ?`, id)
}

type PlaceSearch struct {
	Category, Q      string
	NearLat, NearLng *float64
	RadiusKM         *float64
}

func SearchPlaces(ctx context.Context, ex db.Executor, f PlaceSearch) ([]*model.Place, error) {
	var where []string
	var args []any
	if f.Category != "" && !strings.EqualFold(f.Category, "any") {
		where, args = append(where, "category = ?"), append(args, strings.ToLower(f.Category))
	}
	if f.Q != "" {
		where, args = append(where, ilike("name")), append(args, contains(f.Q))
	}
	q := `SELECT ` + placeCols + ` FROM places`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := queryList(ctx, ex, scanPlace, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	places := make([]*model.Place, len(rows))
	for i := range rows {
		places[i] = &rows[i]
	}
	if f.NearLat == nil || f.NearLng == nil {
		return places, nil
	}
	return geo.RankByDistance(places, *f.NearLat, *f.NearLng, f.RadiusKM), nil
}

func CreatePlace(ctx context.Context, ex db.Executor, p *model.Place) (*model.Place, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO places (name, category, address, latitude, longitude, emoji) VALUES (?,?,?,?,?,?)`,
		p.Name, p.Category, p.Address, p.Latitude, p.Longitude, p.Emoji)
	if err != nil {
		return nil, err
	}
	return PlaceByID(ctx, ex, id)
}

// DeletePlace silently ignores a missing row, like the Python CRUDBase.delete.
func DeletePlace(ctx context.Context, ex db.Executor, id int) error {
	_, err := ex.Exec(ctx, `DELETE FROM places WHERE id = ?`, id)
	return err
}
