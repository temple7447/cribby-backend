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
	landlord_name, landlord_verified, landlord_response_rate, created_at`

func scanListing(s rowScanner) (model.Listing, error) {
	var l model.Listing
	err := s.Scan(&l.ID, &l.Title, &l.Type, &l.PricePer, &l.Total, &l.UtilitiesMonthly, &l.Deposit, &l.Walk,
		&l.Latitude, &l.Longitude, &l.Rating, &l.Reviews, &l.Verified, &l.Hot, &l.Video, &l.IsHidden,
		&l.Grad, &l.Emoji, &l.Amenities, &l.Photos, &l.LandlordName, &l.LandlordVerified,
		&l.LandlordResponseRate, &l.CreatedAt)
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

func SearchListings(ctx context.Context, ex db.Executor, f ListingSearch) ([]*model.Listing, error) {
	where := []string{"is_hidden = ?"}
	args := []any{false}
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
		where, args = append(where, ilike("title")), append(args, contains(f.Q))
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

func CreateListing(ctx context.Context, ex db.Executor, l *model.Listing) (*model.Listing, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO listings (title, type, price_per, total, utilities_monthly, deposit, walk,
		latitude, longitude, rating, reviews, verified, hot, video, is_hidden, grad, emoji, amenities, photos,
		landlord_name, landlord_verified, landlord_response_rate)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.Title, l.Type, l.PricePer, l.Total, l.UtilitiesMonthly, l.Deposit, l.Walk,
		l.Latitude, l.Longitude, l.Rating, l.Reviews, l.Verified, l.Hot, l.Video, false, l.Grad, l.Emoji,
		l.Amenities, l.Photos, l.LandlordName, l.LandlordVerified, l.LandlordResponseRate)
	if err != nil {
		return nil, err
	}
	return ListingByID(ctx, ex, id)
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
