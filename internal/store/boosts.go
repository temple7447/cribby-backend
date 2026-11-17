package store

import (
	"context"
	"sort"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// SetListingBoost makes a listing a featured spot until the given time; nil ends it.
func SetListingBoost(ctx context.Context, ex db.Executor, listingID int, until *db.Time) error {
	if until == nil {
		_, err := ex.Exec(ctx, `DELETE FROM listing_boosts WHERE listing_id = ?`, listingID)
		return err
	}
	res, err := ex.Exec(ctx, `UPDATE listing_boosts SET boosted_until = ? WHERE listing_id = ?`, *until, listingID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO listing_boosts (listing_id, boosted_until, created_at) VALUES (?,?,?)`, listingID, *until, db.Now())
	return err
}

// ActiveBoosts returns the ids (among the given) whose boost hasn't run out.
func ActiveBoosts(ctx context.Context, ex db.Executor, ids []int) (map[int]bool, error) {
	out := map[int]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{db.Time{Time: time.Now().UTC()}}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := ex.Query(ctx, `SELECT listing_id FROM listing_boosts WHERE boosted_until > ? AND listing_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// StampBoosts marks boosted listings and moves them to the front, keeping the existing order otherwise.
func StampBoosts(ctx context.Context, ex db.Executor, listings []*model.Listing) error {
	ids := make([]int, len(listings))
	for i, l := range listings {
		ids[i] = l.ID
	}
	boosted, err := ActiveBoosts(ctx, ex, ids)
	if err != nil {
		return err
	}
	for _, l := range listings {
		l.Boosted = boosted[l.ID]
	}
	sort.SliceStable(listings, func(i, j int) bool { return listings[i].Boosted && !listings[j].Boosted })
	return nil
}
