package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const savedCols = `id, user_id, name, max_price, max_walk, room_type, amenities, verified_only, landlord_verified_only, water, power, created_at, last_sent_at`

func scanSaved(s rowScanner) (model.SavedSearch, error) {
	var x model.SavedSearch
	var amen string
	err := s.Scan(&x.ID, &x.UserID, &x.Name, &x.MaxPrice, &x.MaxWalk, &x.RoomType, &amen, &x.VerifiedOnly, &x.LandlordVerifiedOnly,
		&x.Water, &x.Power, &x.CreatedAt, &x.LastSentAt)
	x.Amenities = []string{}
	if amen != "" {
		x.Amenities = strings.Split(amen, "|")
	}
	return x, err
}

// Filter turns a saved search into the same filter the search screen uses.
func SavedSearchFilter(x model.SavedSearch) ListingSearch {
	return ListingSearch{MaxPrice: x.MaxPrice, MaxWalk: x.MaxWalk, RoomType: x.RoomType, Amenities: x.Amenities,
		VerifiedOnly: x.VerifiedOnly, VerifiedLandlordOnly: x.LandlordVerifiedOnly, Water: x.Water, Power: x.Power}
}

func CreateSavedSearch(ctx context.Context, ex db.Executor, x model.SavedSearch) (int, error) {
	return ex.InsertID(ctx, `INSERT INTO saved_searches (user_id, name, max_price, max_walk, room_type, amenities, verified_only,
		landlord_verified_only, water, power, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		x.UserID, x.Name, x.MaxPrice, x.MaxWalk, x.RoomType, strings.Join(x.Amenities, "|"), x.VerifiedOnly, x.LandlordVerifiedOnly,
		x.Water, x.Power, db.Now())
}

func SavedSearchesFor(ctx context.Context, ex db.Executor, userID int) ([]model.SavedSearch, error) {
	return queryList(ctx, ex, scanSaved, `SELECT `+savedCols+` FROM saved_searches WHERE user_id = ? ORDER BY id DESC`, userID)
}

func AllSavedSearches(ctx context.Context, ex db.Executor) ([]model.SavedSearch, error) {
	return queryList(ctx, ex, scanSaved, `SELECT `+savedCols+` FROM saved_searches ORDER BY id`)
}

func CountSavedSearches(ctx context.Context, ex db.Executor, userID int) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM saved_searches WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// DeleteSavedSearch removes one of the person's own searches; false when it isn't theirs or doesn't exist.
func DeleteSavedSearch(ctx context.Context, ex db.Executor, id, userID int) (bool, error) {
	if _, err := ex.Exec(ctx, `DELETE FROM saved_search_seen WHERE search_id IN (SELECT id FROM saved_searches WHERE id = ? AND user_id = ?)`, id, userID); err != nil {
		return false, err
	}
	res, err := ex.Exec(ctx, `DELETE FROM saved_searches WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SeenListings are the homes a saved search has already told (or deliberately not told) the person about.
func SeenListings(ctx context.Context, ex db.Executor, searchID int) (map[int]bool, error) {
	ids, err := queryList(ctx, ex, func(s rowScanner) (int, error) {
		var id int
		return id, s.Scan(&id)
	}, `SELECT listing_id FROM saved_search_seen WHERE search_id = ?`, searchID)
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		seen[id] = true
	}
	return seen, err
}

func MarkSeen(ctx context.Context, ex db.Executor, searchID int, listingIDs []int) error {
	for _, id := range listingIDs {
		if _, err := ex.Exec(ctx, `INSERT INTO saved_search_seen (search_id, listing_id) VALUES (?,?)`, searchID, id); err != nil {
			return err
		}
	}
	return nil
}

func SetSavedSearchSent(ctx context.Context, ex db.Executor, searchID int) error {
	_, err := ex.Exec(ctx, `UPDATE saved_searches SET last_sent_at = ? WHERE id = ?`, db.Now(), searchID)
	return err
}
