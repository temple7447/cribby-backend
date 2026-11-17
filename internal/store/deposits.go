package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const depositCols = `id, booking_id, listing_id, tenant_id, landlord_id, title, amount, fee, landlord_amount, status, reference,
	payment_url, tenant_phone, move_in_date, dispute_reason, resolution_note, payout_status, payout_reference,
	created_at, updated_at, paid_at, auto_release_at, completed_at`

func scanDeposit(s rowScanner) (model.Deposit, error) {
	var d model.Deposit
	err := s.Scan(&d.ID, &d.BookingID, &d.ListingID, &d.TenantID, &d.LandlordID, &d.Title, &d.Amount, &d.Fee, &d.LandlordAmount,
		&d.Status, &d.Reference, &d.PaymentURL, &d.TenantPhone, &d.MoveInDate, &d.DisputeReason, &d.ResolutionNote,
		&d.PayoutStatus, &d.PayoutReference, &d.CreatedAt, &d.UpdatedAt, &d.PaidAt, &d.AutoReleaseAt, &d.CompletedAt)
	return d, err
}

func CreateDeposit(ctx context.Context, ex db.Executor, d model.Deposit) (int, error) {
	now := db.Now()
	return ex.InsertID(ctx, `INSERT INTO deposits (booking_id, listing_id, tenant_id, landlord_id, title, amount, fee, landlord_amount,
		status, reference, tenant_phone, move_in_date, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,'awaiting_payment',?,?,?,?,?)`,
		d.BookingID, d.ListingID, d.TenantID, d.LandlordID, d.Title, d.Amount, d.Fee, d.LandlordAmount, d.Reference,
		d.TenantPhone, d.MoveInDate, now, now)
}

func DepositByID(ctx context.Context, ex db.Executor, id int) (*model.Deposit, error) {
	return queryOne(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE id = ?`, id)
}

func DepositByReference(ctx context.Context, ex db.Executor, ref string) (*model.Deposit, error) {
	return queryOne(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE reference = ?`, ref)
}

func SetDepositPaymentURL(ctx context.Context, ex db.Executor, id int, url string) error {
	_, err := ex.Exec(ctx, `UPDATE deposits SET payment_url = ?, updated_at = ? WHERE id = ?`, url, db.Now(), id)
	return err
}

func DepositsForUser(ctx context.Context, ex db.Executor, userID int) ([]model.Deposit, error) {
	return queryList(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE tenant_id = ? OR landlord_id = ? ORDER BY created_at DESC, id DESC LIMIT 200`, userID, userID)
}

func DepositsInStatus(ctx context.Context, ex db.Executor, status string, limit int) ([]model.Deposit, error) {
	return queryList(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE status = ? ORDER BY created_at ASC LIMIT ?`, status, limit)
}

func DepositsWithPayout(ctx context.Context, ex db.Executor, payout string, limit int) ([]model.Deposit, error) {
	return queryList(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE payout_status = ? ORDER BY completed_at ASC LIMIT ?`, payout, limit)
}

// OpenDepositForBooking is a deposit on this booking that blocks a new one: money is held or disputed, or a
// payment was started less than `freshAfter` ago.
func OpenDepositForBooking(ctx context.Context, ex db.Executor, bookingID int, freshAfter db.Time) (*model.Deposit, error) {
	return queryOne(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE booking_id = ?
		AND (status IN ('paid','disputed') OR (status = 'awaiting_payment' AND created_at > ?)) ORDER BY id DESC LIMIT 1`, bookingID, freshAfter)
}

func MarkDepositPaid(ctx context.Context, ex db.Executor, id int, autoRelease db.Time) (bool, error) {
	return transitionIn(ctx, ex, "deposits", id, []string{"awaiting_payment"}, "paid", "paid_at = ?, auto_release_at = ?", db.Now(), autoRelease)
}

func CompleteDeposit(ctx context.Context, ex db.Executor, id int, from []string, note string) (bool, error) {
	return transitionIn(ctx, ex, "deposits", id, from, "completed", "completed_at = ?, payout_status = 'queued', resolution_note = ?", db.Now(), note)
}

func DisputeDeposit(ctx context.Context, ex db.Executor, id int, reason string) (bool, error) {
	return transitionIn(ctx, ex, "deposits", id, []string{"paid"}, "disputed", "dispute_reason = ?, auto_release_at = NULL", reason)
}

func RefundDeposit(ctx context.Context, ex db.Executor, id int, from []string, note string) (bool, error) {
	return transitionIn(ctx, ex, "deposits", id, from, "refunded", "resolution_note = ?", note)
}

func RevertDepositRefund(ctx context.Context, ex db.Executor, id int, to string) error {
	_, err := transitionIn(ctx, ex, "deposits", id, []string{"refunded"}, to, "")
	return err
}

func CancelDeposit(ctx context.Context, ex db.Executor, id int) (bool, error) {
	return transitionIn(ctx, ex, "deposits", id, []string{"awaiting_payment"}, "cancelled", "")
}

func MarkDepositPayoutPaid(ctx context.Context, ex db.Executor, id int, reference string) (bool, error) {
	res, err := ex.Exec(ctx, `UPDATE deposits SET payout_status = 'paid', payout_reference = ?, updated_at = ? WHERE id = ? AND payout_status = 'queued'`,
		reference, db.Now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func DepositsDueForRelease(ctx context.Context, ex db.Executor, now db.Time) ([]model.Deposit, error) {
	return queryList(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE status = 'paid' AND auto_release_at <= ? LIMIT 100`, now)
}

func DepositsAbandoned(ctx context.Context, ex db.Executor, createdBefore db.Time) ([]model.Deposit, error) {
	return queryList(ctx, ex, scanDeposit, `SELECT `+depositCols+` FROM deposits WHERE status = 'awaiting_payment' AND created_at <= ? LIMIT 100`, createdBefore)
}

// SetBookingDepositPaid keeps the booking's own "deposit paid" tick in step with the money.
func SetBookingDepositPaid(ctx context.Context, ex db.Executor, bookingID int, paid bool) error {
	_, err := ex.Exec(ctx, `UPDATE bookings SET deposit_paid = ? WHERE id = ?`, paid, bookingID)
	return err
}

// BookingByIDPlain loads a booking without the listing join (id, user, listing, status, move-in date).
func BookingDepositFacts(ctx context.Context, ex db.Executor, id int) (userID, listingID int, status string, moveIn db.Date, found bool, err error) {
	rows, err := ex.Query(ctx, `SELECT user_id, listing_id, status, move_in_date FROM bookings WHERE id = ?`, id)
	if err != nil {
		return
	}
	defer rows.Close()
	if rows.Next() {
		found = true
		err = rows.Scan(&userID, &listingID, &status, &moveIn)
	}
	return
}
