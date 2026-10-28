package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
)

const listingCols = `id, title, type, price_per, total, utilities_monthly, deposit, walk, latitude, longitude,
	rating, reviews, verified, hot, video, is_hidden, grad, emoji, amenities, photos,
	landlord_name, landlord_verified, landlord_response_rate, created_at,
	owner_id, status, description, address, review_note`

func scanListing(s rowScanner) (model.Listing, error) {
	var l model.Listing
	err := s.Scan(listingTargets(&l)...)
	return l, err
}

func ListingByID(ctx context.Context, ex db.Executor, id int) (*model.Listing, error) {
	return queryOne(ctx, ex, scanListing, `SELECT `+listingCols+` FROM listings WHERE id = ?`, id)
}

type ListingSearch struct {
	MaxPrice, MaxWalk *int
	RoomType, Q       string
	NearLat, NearLng  *float64
	RadiusKM          *float64
}

// SearchListings is the public search: only approved, unhidden listings.
func SearchListings(ctx context.Context, ex db.Executor, f ListingSearch) ([]*model.Listing, error) {
	where := []string{"is_hidden = ?", "status = ?"}
	args := []any{false, model.ListingApproved}
	if f.MaxPrice != nil {
		where, args = append(where, "price_per <= ?"), append(args, *f.MaxPrice)
	}
	if f.MaxWalk != nil {
		where, args = append(where, "walk <= ?"), append(args, *f.MaxWalk)
	}
	if f.RoomType != "" && !strings.EqualFold(f.RoomType, "any") {
		where, args = append(where, ilike("type")), append(args, contains(f.RoomType))
	}
	if f.Q != "" {
		where, args = append(where, "("+ilike("title")+" OR "+ilike("address")+")"), append(args, contains(f.Q), contains(f.Q))
	}
	rows, err := queryList(ctx, ex, scanListing,
		`SELECT `+listingCols+` FROM listings WHERE `+strings.Join(where, " AND ")+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	listings := make([]*model.Listing, len(rows))
	for i := range rows {
		listings[i] = &rows[i]
	}
	if f.NearLat == nil || f.NearLng == nil {
		return listings, nil
	}
	// Proximity mode: nearest-first, distance_km stamped, coordinate-less
	// listings dropped, and anything past radius_km if given.
	return geo.RankByDistance(listings, *f.NearLat, *f.NearLng, f.RadiusKM), nil
}

// ListingsByOwner returns everything a user has posted, newest first, whatever its status.
func ListingsByOwner(ctx context.Context, ex db.Executor, ownerID int) ([]model.Listing, error) {
	return queryList(ctx, ex, scanListing,
		`SELECT `+listingCols+` FROM listings WHERE owner_id = ? ORDER BY id DESC`, ownerID)
}

// ListingsByStatus is the admin review queue (oldest first, so nothing waits forever).
func ListingsByStatus(ctx context.Context, ex db.Executor, status string) ([]model.Listing, error) {
	return queryList(ctx, ex, scanListing,
		`SELECT `+listingCols+` FROM listings WHERE status = ? ORDER BY id`, status)
}

// CountRecentByOwner counts a user's posts since the given time (spam guard).
func CountRecentByOwner(ctx context.Context, ex db.Executor, ownerID int, since db.Time) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM listings WHERE owner_id = ? AND created_at >= ?`, ownerID, since).Scan(&n)
	return n, err
}

func CreateListing(ctx context.Context, ex db.Executor, l *model.Listing) (*model.Listing, error) {
	status := l.Status
	if status == "" {
		status = model.ListingApproved
	}
	id, err := ex.InsertID(ctx, `INSERT INTO listings (title, type, price_per, total, utilities_monthly, deposit, walk,
		latitude, longitude, rating, reviews, verified, hot, video, is_hidden, grad, emoji, amenities, photos,
		landlord_name, landlord_verified, landlord_response_rate, owner_id, status, description, address)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.Title, l.Type, l.PricePer, l.Total, l.UtilitiesMonthly, l.Deposit, l.Walk,
		l.Latitude, l.Longitude, l.Rating, l.Reviews, l.Verified, l.Hot, l.Video, false, l.Grad, l.Emoji,
		l.Amenities, l.Photos, l.LandlordName, l.LandlordVerified, l.LandlordResponseRate,
		l.OwnerID, status, l.Description, l.Address)
	if err != nil {
		return nil, err
	}
	return ListingByID(ctx, ex, id)
}

// UpdateListingDetails saves an owner's edit (everything they can change) and its new status.
func UpdateListingDetails(ctx context.Context, ex db.Executor, l *model.Listing) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET title = ?, type = ?, price_per = ?, total = ?, utilities_monthly = ?,
		deposit = ?, walk = ?, latitude = ?, longitude = ?, amenities = ?, description = ?, address = ?,
		status = ?, review_note = ? WHERE id = ?`,
		l.Title, l.Type, l.PricePer, l.Total, l.UtilitiesMonthly, l.Deposit, l.Walk, l.Latitude, l.Longitude,
		l.Amenities, l.Description, l.Address, l.Status, l.ReviewNote, l.ID)
	return err
}

func SetListingStatus(ctx context.Context, ex db.Executor, id int, status string, note *string) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET status = ?, review_note = ? WHERE id = ?`, status, note, id)
	return err
}

func SetListingPhotos(ctx context.Context, ex db.Executor, id int, photos db.JSONList[model.Photo]) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET photos = ? WHERE id = ?`, photos, id)
	return err
}

func SetListingHidden(ctx context.Context, ex db.Executor, id int, hidden bool) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET is_hidden = ? WHERE id = ?`, hidden, id)
	return err
}

func SetListingCoords(ctx context.Context, ex db.Executor, id int, lat, lng float64) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET latitude = ?, longitude = ? WHERE id = ?`, lat, lng, id)
	return err
}
