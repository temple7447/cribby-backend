package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
)

// NormalizeAddress lower-cases and collapses spaces so "12  Akoka Rd" matches "12 akoka rd".
func NormalizeAddress(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// FindDuplicateListing looks for a live or waiting listing that is probably the same home: the same
// address, or the same price within about 30 metres. It returns the first match's id and owner (0 if none).
func FindDuplicateListing(ctx context.Context, ex db.Executor, address string, lat, lng float64, price int) (id, ownerID int, err error) {
	const near = 0.0003 // ~33 m of latitude
	addr := NormalizeAddress(address)
	var owner *int
	row := ex.QueryRow(ctx, `SELECT id, owner_id FROM listings
		WHERE status IN ('pending','approved') AND (
			(? <> '' AND LOWER(TRIM(address)) = ?) OR
			(price_per = ? AND latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ?))
		ORDER BY id LIMIT 1`, addr, addr, price, lat-near, lat+near, lng-near, lng+near)
	if err := row.Scan(&id, &owner); err != nil {
		return 0, 0, nil //nolint:nilerr // no row = no duplicate
	}
	if owner != nil {
		ownerID = *owner
	}
	return id, ownerID, nil
}

// FlagDuplicate records that a listing looks like a copy of another.
func FlagDuplicate(ctx context.Context, ex db.Executor, listingID, duplicateOf int) error {
	_, err := ex.Exec(ctx, `INSERT INTO listing_flags (listing_id, duplicate_of, created_at) VALUES (?,?,?)`,
		listingID, duplicateOf, db.Now())
	return err
}

// DuplicateFlags maps listing id -> the id it looks like a copy of, for the given listings.
func DuplicateFlags(ctx context.Context, ex db.Executor, ids []int) (map[int]int, error) {
	out := map[int]int{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, v := range ids {
		args[i] = v
	}
	rows, err := ex.Query(ctx, `SELECT listing_id, duplicate_of FROM listing_flags WHERE listing_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a, b int
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out[a] = b
	}
	return out, rows.Err()
}

// CountApprovedByOwner is how many of the person's listings an admin has approved (their track record).
func CountApprovedByOwner(ctx context.Context, ex db.Executor, ownerID int) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM listings WHERE owner_id = ? AND status = 'approved'`, ownerID).Scan(&n)
	return n, err
}
