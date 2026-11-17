package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
)

const marketCols = `m.id, m.user_id, m.title, m.description, m.category, m.condition, m.price, m.latitude, m.longitude,
	m.area, m.contact_phone, m.leaving_soon, m.photos, m.status, m.created_at, m.updated_at, u.display_name, u.name`

const marketFrom = ` FROM market_items m JOIN users u ON u.id = m.user_id `

func scanMarket(s rowScanner) (model.MarketItem, error) {
	var m model.MarketItem
	var display, name *string
	err := s.Scan(&m.ID, &m.UserID, &m.Title, &m.Description, &m.Category, &m.Condition, &m.Price, &m.Latitude, &m.Longitude,
		&m.Area, &m.ContactPhone, &m.LeavingSoon, &m.Photos, &m.Status, &m.CreatedAt, &m.UpdatedAt, &display, &name)
	d, n := "", ""
	if display != nil {
		d = *display
	}
	if name != nil {
		n = *name
	}
	m.Seller = firstName(d, n)
	return m, err
}

func CreateMarketItem(ctx context.Context, ex db.Executor, m model.MarketItem) (int, error) {
	now := db.Now()
	return ex.InsertID(ctx, `INSERT INTO market_items (user_id, title, description, category, condition, price, latitude, longitude,
		area, contact_phone, leaving_soon, photos, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,'active',?,?)`,
		m.UserID, m.Title, m.Description, m.Category, m.Condition, m.Price, m.Latitude, m.Longitude,
		m.Area, m.ContactPhone, m.LeavingSoon, db.JSONList[model.Photo]{}, now, now)
}

func MarketItemByID(ctx context.Context, ex db.Executor, id int) (*model.MarketItem, error) {
	return queryOne(ctx, ex, scanMarket, `SELECT `+marketCols+marketFrom+`WHERE m.id = ?`, id)
}

func UpdateMarketItem(ctx context.Context, ex db.Executor, m *model.MarketItem) error {
	_, err := ex.Exec(ctx, `UPDATE market_items SET title = ?, description = ?, category = ?, condition = ?, price = ?, latitude = ?,
		longitude = ?, area = ?, contact_phone = ?, leaving_soon = ?, status = ?, updated_at = ? WHERE id = ?`,
		m.Title, m.Description, m.Category, m.Condition, m.Price, m.Latitude, m.Longitude, m.Area, m.ContactPhone,
		m.LeavingSoon, m.Status, db.Now(), m.ID)
	return err
}

func SetMarketStatus(ctx context.Context, ex db.Executor, id int, status string) error {
	_, err := ex.Exec(ctx, `UPDATE market_items SET status = ?, updated_at = ? WHERE id = ?`, status, db.Now(), id)
	return err
}

func SetMarketPhotos(ctx context.Context, ex db.Executor, id int, photos db.JSONList[model.Photo]) error {
	_, err := ex.Exec(ctx, `UPDATE market_items SET photos = ?, updated_at = ? WHERE id = ?`, photos, db.Now(), id)
	return err
}

func CountRecentMarketByUser(ctx context.Context, ex db.Executor, userID int, since db.Time) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM market_items WHERE user_id = ? AND created_at >= ?`, userID, since).Scan(&n)
	return n, err
}

func ListMarketByUser(ctx context.Context, ex db.Executor, userID int) ([]model.MarketItem, error) {
	return queryList(ctx, ex, scanMarket, `SELECT `+marketCols+marketFrom+`WHERE m.user_id = ? AND m.status <> 'removed' ORDER BY m.created_at DESC, m.id DESC`, userID)
}

type MarketSearch struct {
	Category, Q      string
	MaxPrice         *int
	FreeOnly         bool
	NearLat, NearLng *float64
	RadiusKM         *float64
}

// SearchMarket returns active items: nearest-first when a point is given (items with no pin follow, unless a
// radius limits the search), otherwise newest first.
func SearchMarket(ctx context.Context, ex db.Executor, f MarketSearch, limit int) ([]*model.MarketItem, error) {
	where, args := []string{"m.status = ?"}, []any{"active"}
	if f.Category != "" {
		where, args = append(where, "m.category = ?"), append(args, f.Category)
	}
	if f.FreeOnly {
		where = append(where, "m.price = 0")
	} else if f.MaxPrice != nil {
		where, args = append(where, "m.price <= ?"), append(args, *f.MaxPrice)
	}
	if f.Q != "" {
		where, args = append(where, "("+ilike("m.title")+" OR "+ilike("m.description")+")"), append(args, contains(f.Q), contains(f.Q))
	}
	rows, err := queryList(ctx, ex, scanMarket, `SELECT `+marketCols+marketFrom+`WHERE `+strings.Join(where, " AND ")+` ORDER BY m.created_at DESC, m.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := make([]*model.MarketItem, len(rows))
	for i := range rows {
		items[i] = &rows[i]
	}
	if f.NearLat != nil && f.NearLng != nil {
		ranked := geo.RankByDistance(items, *f.NearLat, *f.NearLng, f.RadiusKM)
		if f.RadiusKM == nil {
			for _, it := range items {
				if lat, lng := it.Coords(); lat == nil || lng == nil {
					ranked = append(ranked, it)
				}
			}
		}
		items = ranked
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
