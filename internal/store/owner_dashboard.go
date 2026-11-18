package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// OwnerListingStats is every home the person posted, with how much interest it has had.
func OwnerListingStats(ctx context.Context, ex db.Executor, ownerID int, since string) ([]model.OwnerListingStat, error) {
	return queryList(ctx, ex, func(s rowScanner) (model.OwnerListingStat, error) {
		var o model.OwnerListingStat
		err := s.Scan(&o.ID, &o.Title, &o.Status, &o.PricePer, &o.Rating, &o.Reviews, &o.Views7d, &o.ViewsTotal, &o.Saves, &o.Chats, &o.PendingRequests, &o.ApprovedBooking, &o.HeldDeposits)
		return o, err
	}, `SELECT l.id, l.title, l.status, l.price_per, l.rating, l.reviews,
		(SELECT COUNT(*) FROM listing_views v WHERE v.listing_id = l.id AND v.day >= ?),
		(SELECT COUNT(*) FROM listing_views v WHERE v.listing_id = l.id),
		(SELECT COUNT(*) FROM favorites f WHERE f.listing_id = l.id),
		(SELECT COUNT(*) FROM conversations c WHERE c.listing_id = l.id AND c.kind = 'landlord'),
		(SELECT COUNT(*) FROM bookings b WHERE b.listing_id = l.id AND b.status = 'pending'),
		(SELECT COUNT(*) FROM bookings b WHERE b.listing_id = l.id AND b.status = 'approved'),
		(SELECT COUNT(*) FROM deposits d WHERE d.listing_id = l.id AND d.status IN ('paid','disputed'))
		FROM listings l WHERE l.owner_id = ? ORDER BY l.id DESC`, since, ownerID)
}

// OwnerMoneyFor adds up what the person is owed as a landlord (deposits) and as a seller (marketplace orders).
func OwnerMoneyFor(ctx context.Context, ex db.Executor, userID int) (model.OwnerMoney, error) {
	var m model.OwnerMoney
	for _, q := range []string{
		`SELECT COALESCE(SUM(CASE WHEN status IN ('paid','disputed') THEN landlord_amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'completed' AND payout_status = 'queued' THEN landlord_amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'completed' AND payout_status = 'paid' THEN landlord_amount ELSE 0 END), 0)
			FROM deposits WHERE landlord_id = ?`,
		`SELECT COALESCE(SUM(CASE WHEN status IN ('paid','handed_over','disputed') THEN seller_amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'completed' AND payout_status = 'queued' THEN seller_amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'completed' AND payout_status = 'paid' THEN seller_amount ELSE 0 END), 0)
			FROM orders WHERE seller_id = ?`,
	} {
		var h, t, r int
		if err := ex.QueryRow(ctx, q, userID).Scan(&h, &t, &r); err != nil {
			return m, err
		}
		m.Held, m.ToReceive, m.Received = m.Held+h, m.ToReceive+t, m.Received+r
	}
	return m, nil
}

// OwnerDisputes counts deposits and orders of this person that are waiting on a Cribby decision.
func OwnerDisputes(ctx context.Context, ex db.Executor, userID int) (int, error) {
	var a, b int
	if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE landlord_id = ? AND status = 'disputed'`, userID).Scan(&a); err != nil {
		return 0, err
	}
	if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE seller_id = ? AND status = 'disputed'`, userID).Scan(&b); err != nil {
		return 0, err
	}
	return a + b, nil
}

// RecordListingView notes that `viewer` (an opaque, per-day token) opened the listing today. Once per viewer per day.
func RecordListingView(ctx context.Context, ex db.Executor, listingID int, viewer, day string) error {
	var n int
	if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM listing_views WHERE listing_id = ? AND viewer = ? AND day = ?`, listingID, viewer, day).Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := ex.Exec(ctx, `INSERT INTO listing_views (listing_id, viewer, day) VALUES (?,?,?)`, listingID, viewer, day)
	return err
}
