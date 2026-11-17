package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

func ChecklistFor(ctx context.Context, ex db.Executor, listingID int) (*model.Checklist, error) {
	return queryOne(ctx, ex, func(s rowScanner) (model.Checklist, error) {
		var c model.Checklist
		err := s.Scan(&c.Condition, &c.Water, &c.Power, &c.Permission, &c.ContactPhone)
		return c, err
	}, `SELECT condition, water, power, permission, contact_phone FROM listing_checklists WHERE listing_id = ?`, listingID)
}

func UpsertChecklist(ctx context.Context, ex db.Executor, listingID int, c model.Checklist) error {
	now := db.Now()
	res, err := ex.Exec(ctx, `UPDATE listing_checklists SET condition = ?, water = ?, power = ?, permission = ?,
		contact_phone = ?, updated_at = ? WHERE listing_id = ?`, c.Condition, c.Water, c.Power, c.Permission, c.ContactPhone, now, listingID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO listing_checklists (listing_id, condition, water, power, permission, contact_phone, updated_at)
		VALUES (?,?,?,?,?,?,?)`, listingID, c.Condition, c.Water, c.Power, c.Permission, c.ContactPhone, now)
	return err
}

// FindListingByPhoneOfOtherOwner finds a live or waiting listing, owned by someone else, whose landlord
// number is the same. One landlord with several flats is normal; two different posters sharing a number
// is the sign of an agent reposting someone's home. Only earlier listings that are not themselves flagged
// as copies count, so the original is never marked as the copy.
func FindListingByPhoneOfOtherOwner(ctx context.Context, ex db.Executor, phone string, ownerID, listingID int) (int, error) {
	if phone == "" {
		return 0, nil
	}
	var id int
	err := ex.QueryRow(ctx, `SELECT l.id FROM listing_checklists c JOIN listings l ON l.id = c.listing_id
		WHERE c.contact_phone = ? AND l.status IN ('pending','approved') AND (l.owner_id IS NULL OR l.owner_id <> ?)
		AND l.id < ? AND NOT EXISTS (SELECT 1 FROM listing_flags f WHERE f.listing_id = l.id)
		ORDER BY l.id LIMIT 1`, phone, ownerID, listingID).Scan(&id)
	if err != nil {
		return 0, nil //nolint:nilerr // no row = no match
	}
	return id, nil
}
