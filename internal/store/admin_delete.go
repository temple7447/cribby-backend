package store

import (
	"context"
	"errors"

	"campus-backend/internal/db"
)

// ErrHasPayments means a payment record points at the thing, and payment records are never erased.
var ErrHasPayments = errors.New("payment records point at this")

// DeleteListingHard erases a home and everything that hangs off it. Chats about it stay, without the link.
// Refused if any deposit was ever taken for it. Run inside a transaction. The photos are the caller's job.
func DeleteListingHard(ctx context.Context, tx db.Executor, id int) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE listing_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrHasPayments
	}
	for _, q := range []string{
		`UPDATE conversations SET listing_id = NULL WHERE listing_id = ?`,
		`DELETE FROM favorites WHERE listing_id = ?`,
		`DELETE FROM reviews WHERE listing_id = ?`,
		`DELETE FROM listing_flags WHERE listing_id = ?`,
		`DELETE FROM listing_checklists WHERE listing_id = ?`,
		`DELETE FROM listing_boosts WHERE listing_id = ?`,
		`DELETE FROM verifier_credits WHERE listing_id = ?`,
		`DELETE FROM listing_views WHERE listing_id = ?`,
		`DELETE FROM saved_search_seen WHERE listing_id = ?`,
		`DELETE FROM bookings WHERE listing_id = ?`,
		`DELETE FROM listings WHERE id = ?`,
	} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteBookingHard erases a booking request, unless a deposit was taken for it.
func DeleteBookingHard(ctx context.Context, tx db.Executor, id int) (found bool, err error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE booking_id = ?`, id).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, ErrHasPayments
	}
	res, err := tx.Exec(ctx, `DELETE FROM bookings WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// deleteWhere runs a DELETE and reports whether it removed anything.
func deleteWhere(ctx context.Context, ex db.Executor, q string, arg any) (bool, error) {
	res, err := ex.Exec(ctx, q, arg)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func DeleteVendor(ctx context.Context, ex db.Executor, id int) (bool, error) {
	return deleteWhere(ctx, ex, `DELETE FROM vendors WHERE id = ?`, id)
}

func DeleteScamAlert(ctx context.Context, ex db.Executor, id int) (bool, error) {
	return deleteWhere(ctx, ex, `DELETE FROM scam_alerts WHERE id = ?`, id)
}

// DeleteReferralCode removes the code. People who joined through it keep their record of having joined.
func DeleteReferralCode(ctx context.Context, ex db.Executor, code string) (bool, error) {
	return deleteWhere(ctx, ex, `DELETE FROM referral_codes WHERE code = ?`, code)
}

func DeleteClientError(ctx context.Context, ex db.Executor, id int) (bool, error) {
	return deleteWhere(ctx, ex, `DELETE FROM client_errors WHERE id = ?`, id)
}
