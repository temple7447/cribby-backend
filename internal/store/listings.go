package store

import (
	"context"
	"strings"
	"unicode"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
)

const listingCols = `id, title, type, price_per, total, utilities_monthly, deposit, walk, latitude, longitude,
	rating, reviews, verified, hot, video, is_hidden, grad, emoji, amenities, photos,
	landlord_name, landlord_verified, landlord_response_rate, created_at,
	owner_id, status, description, address, review_note, video_url, video_public_id`

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

	// Advanced filters. Water and Power need the visit checklist, so homes without one don't match them.
	Amenities            []string // every one must be among the listing's amenities (matched by words, ignoring emoji)
	VerifiedOnly         bool     // Cribby has checked the home
	VerifiedLandlordOnly bool     // the landlord's ID was approved
	Water                string   // tap | borehole | well | running (tap or borehole)
	Power                string   // always | most (always, or most of the day)
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
	if f.VerifiedOnly {
		where, args = append(where, "verified = ?"), append(args, true)
	}
	if f.VerifiedLandlordOnly {
		where, args = append(where, "landlord_verified = ?"), append(args, true)
	}
	if vals := waterValues(f.Water); len(vals) > 0 {
		where = append(where, "id IN (SELECT listing_id FROM listing_checklists WHERE water IN ("+placeholders(len(vals))+"))")
		for _, v := range vals {
			args = append(args, v)
		}
	}
	if vals := powerValues(f.Power); len(vals) > 0 {
		where = append(where, "id IN (SELECT listing_id FROM listing_checklists WHERE power IN ("+placeholders(len(vals))+"))")
		for _, v := range vals {
			args = append(args, v)
		}
	}
	rows, err := queryList(ctx, ex, scanListing,
		`SELECT `+listingCols+` FROM listings WHERE `+strings.Join(where, " AND ")+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	if needles := amenityNeedles(f.Amenities); len(needles) > 0 {
		kept := rows[:0]
		for _, l := range rows {
			if hasAllAmenities(l.Amenities, needles) {
				kept = append(kept, l)
			}
		}
		rows = kept
	}
	listings := make([]*model.Listing, len(rows))
	for i := range rows {
		listings[i] = &rows[i]
	}
	if f.NearLat == nil || f.NearLng == nil {
		return listings, StampBoosts(ctx, ex, listings)
	}
	// Proximity mode: nearest-first with distance_km stamped, and anything past radius_km dropped
	// if one is given. Listings that have no pin can't be ranked, but they're still homes a
	// student may want — so unless a radius says "only within X km", they follow the nearby
	// ones instead of vanishing.
	ranked := geo.RankByDistance(listings, *f.NearLat, *f.NearLng, f.RadiusKM)
	if f.RadiusKM == nil {
		for _, l := range listings {
			if lat, lng := l.Coords(); lat == nil || lng == nil {
				ranked = append(ranked, l)
			}
		}
	}
	return ranked, StampBoosts(ctx, ex, ranked)
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

// SetListingVideo attaches (or, with nils, removes) the listing's video and keeps the `video`
// flag in step with it.
func SetListingVideo(ctx context.Context, ex db.Executor, id int, url, publicID *string) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET video_url = ?, video_public_id = ?, video = ? WHERE id = ?`,
		url, publicID, url != nil, id)
	return err
}

// SetListingVerified marks whether Cribby has checked the home itself (not just the landlord's ID).
func SetListingVerified(ctx context.Context, ex db.Executor, id int, verified bool) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET verified = ? WHERE id = ?`, verified, id)
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

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func waterValues(w string) []string {
	switch strings.ToLower(strings.TrimSpace(w)) {
	case "tap", "borehole", "well":
		return []string{strings.ToLower(strings.TrimSpace(w))}
	case "running":
		return []string{"tap", "borehole"}
	}
	return nil
}

func powerValues(p string) []string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "always":
		return []string{"always"}
	case "most":
		return []string{"always", "most_of_day"}
	}
	return nil
}

// amenityNeedles turns "🛡️ Security" into "security": the words, lower-cased, without a leading emoji.
func amenityNeedles(in []string) []string {
	var out []string
	for _, a := range in {
		if n := cleanAmenity(a); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// cleanAmenity lower-cases an amenity and drops emoji and punctuation, so "🛡️ Security", "security",
// "A/C" and "AC", and "Wi-Fi" and "WiFi", each match one another.
func cleanAmenity(a string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(a)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func hasAllAmenities(have []string, needles []string) bool {
	for _, n := range needles {
		found := false
		for _, h := range have {
			if strings.Contains(cleanAmenity(h), n) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
