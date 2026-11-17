package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const orderCols = `id, item_id, buyer_id, seller_id, title, amount, fee, seller_amount, status, reference, payment_url,
	buyer_phone, seller_phone, dispute_reason, resolution_note, payout_status, payout_reference,
	created_at, updated_at, paid_at, handed_over_at, auto_release_at, completed_at`

func scanOrder(s rowScanner) (model.Order, error) {
	var o model.Order
	err := s.Scan(&o.ID, &o.ItemID, &o.BuyerID, &o.SellerID, &o.Title, &o.Amount, &o.Fee, &o.SellerAmount, &o.Status,
		&o.Reference, &o.PaymentURL, &o.BuyerPhone, &o.SellerPhone, &o.DisputeReason, &o.ResolutionNote, &o.PayoutStatus,
		&o.PayoutReference, &o.CreatedAt, &o.UpdatedAt, &o.PaidAt, &o.HandedOverAt, &o.AutoReleaseAt, &o.CompletedAt)
	return o, err
}

func CreateOrder(ctx context.Context, ex db.Executor, o model.Order) (int, error) {
	now := db.Now()
	return ex.InsertID(ctx, `INSERT INTO orders (item_id, buyer_id, seller_id, title, amount, fee, seller_amount, status, reference,
		payment_url, buyer_phone, seller_phone, created_at, updated_at) VALUES (?,?,?,?,?,?,?,'awaiting_payment',?,?,?,?,?,?)`,
		o.ItemID, o.BuyerID, o.SellerID, o.Title, o.Amount, o.Fee, o.SellerAmount, o.Reference, o.PaymentURL,
		o.BuyerPhone, o.SellerPhone, now, now)
}

func OrderByID(ctx context.Context, ex db.Executor, id int) (*model.Order, error) {
	return queryOne(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE id = ?`, id)
}

func OrderByReference(ctx context.Context, ex db.Executor, ref string) (*model.Order, error) {
	return queryOne(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE reference = ?`, ref)
}

func SetOrderPaymentURL(ctx context.Context, ex db.Executor, id int, url string) error {
	_, err := ex.Exec(ctx, `UPDATE orders SET payment_url = ?, updated_at = ? WHERE id = ?`, url, db.Now(), id)
	return err
}

// OrdersForUser lists orders where the person is the buyer or the seller, newest first.
func OrdersForUser(ctx context.Context, ex db.Executor, userID int) ([]model.Order, error) {
	return queryList(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE buyer_id = ? OR seller_id = ? ORDER BY created_at DESC, id DESC LIMIT 200`, userID, userID)
}

// OrdersInStatus is the admin view, oldest first.
func OrdersInStatus(ctx context.Context, ex db.Executor, status string, limit int) ([]model.Order, error) {
	return queryList(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE status = ? ORDER BY created_at ASC LIMIT ?`, status, limit)
}

// OrdersWithPayout lists completed orders by payout state.
func OrdersWithPayout(ctx context.Context, ex db.Executor, payout string, limit int) ([]model.Order, error) {
	return queryList(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE payout_status = ? ORDER BY completed_at ASC LIMIT ?`, payout, limit)
}

// OpenOrderForItem returns an order on this item that blocks a new one: money is held or being disputed, or
// someone started paying less than `freshAfter` ago. It is nil when the item is free to buy.
func OpenOrderForItem(ctx context.Context, ex db.Executor, itemID int, freshAfter db.Time) (*model.Order, error) {
	return queryOne(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE item_id = ?
		AND (status IN ('paid','handed_over','disputed') OR (status = 'awaiting_payment' AND created_at > ?)) ORDER BY id DESC LIMIT 1`, itemID, freshAfter)
}

// transition moves an order from one of the `from` statuses to `to` in a single conditional update, so two
// taps (or a tap and a timer) can never both succeed. It reports whether this call made the change.
func transition(ctx context.Context, ex db.Executor, id int, from []string, to, extraSet string, extraArgs ...any) (bool, error) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	set := "status = ?, updated_at = ?"
	if extraSet != "" {
		set += ", " + extraSet
	}
	args := append([]any{to, db.Now()}, extraArgs...)
	args = append(args, id)
	for _, f := range from {
		args = append(args, f)
	}
	res, err := ex.Exec(ctx, `UPDATE orders SET `+set+` WHERE id = ? AND status IN (`+marks+`)`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func MarkOrderPaid(ctx context.Context, ex db.Executor, id int) (bool, error) {
	return transition(ctx, ex, id, []string{"awaiting_payment"}, "paid", "paid_at = ?", db.Now())
}

func MarkOrderHandedOver(ctx context.Context, ex db.Executor, id int, autoRelease db.Time) (bool, error) {
	return transition(ctx, ex, id, []string{"paid"}, "handed_over", "handed_over_at = ?, auto_release_at = ?", db.Now(), autoRelease)
}

// CompleteOrder releases the money to the seller: the payout is queued for Cribby to send.
func CompleteOrder(ctx context.Context, ex db.Executor, id int, from []string, note string) (bool, error) {
	return transition(ctx, ex, id, from, "completed", "completed_at = ?, payout_status = 'queued', resolution_note = ?", db.Now(), note)
}

func DisputeOrder(ctx context.Context, ex db.Executor, id int, reason string) (bool, error) {
	return transition(ctx, ex, id, []string{"paid", "handed_over"}, "disputed", "dispute_reason = ?, auto_release_at = NULL", reason)
}

// RefundOrder claims an order for a refund; the caller then asks the processor and calls RevertRefund if that fails.
func RefundOrder(ctx context.Context, ex db.Executor, id int, from []string, note string) (bool, error) {
	return transition(ctx, ex, id, from, "refunded", "resolution_note = ?", note)
}

// RevertRefund puts an order back after a refund the processor refused.
func RevertRefund(ctx context.Context, ex db.Executor, id int, to string) error {
	_, err := transition(ctx, ex, id, []string{"refunded"}, to, "")
	return err
}

func CancelOrder(ctx context.Context, ex db.Executor, id int) (bool, error) {
	return transition(ctx, ex, id, []string{"awaiting_payment"}, "cancelled", "")
}

func MarkPayoutPaid(ctx context.Context, ex db.Executor, id int, reference string) (bool, error) {
	res, err := ex.Exec(ctx, `UPDATE orders SET payout_status = 'paid', payout_reference = ?, updated_at = ? WHERE id = ? AND payout_status = 'queued'`,
		reference, db.Now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Orders the timers should act on.
func OrdersDueForRelease(ctx context.Context, ex db.Executor, now db.Time) ([]model.Order, error) {
	return queryList(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE status = 'handed_over' AND auto_release_at <= ? LIMIT 100`, now)
}

func OrdersNeverHandedOver(ctx context.Context, ex db.Executor, paidBefore db.Time) ([]model.Order, error) {
	return queryList(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE status = 'paid' AND paid_at <= ? LIMIT 100`, paidBefore)
}

func OrdersAbandoned(ctx context.Context, ex db.Executor, createdBefore db.Time) ([]model.Order, error) {
	return queryList(ctx, ex, scanOrder, `SELECT `+orderCols+` FROM orders WHERE status = 'awaiting_payment' AND created_at <= ? LIMIT 100`, createdBefore)
}

func AddOrderEvent(ctx context.Context, ex db.Executor, orderID int, actorID *int, event, note string) error {
	_, err := ex.Exec(ctx, `INSERT INTO order_events (order_id, actor_id, event, note, created_at) VALUES (?,?,?,?,?)`,
		orderID, actorID, event, note, db.Now())
	return err
}

func OrderEvents(ctx context.Context, ex db.Executor, orderID int) ([]model.OrderEvent, error) {
	return queryList(ctx, ex, func(s rowScanner) (model.OrderEvent, error) {
		var e model.OrderEvent
		err := s.Scan(&e.ID, &e.OrderID, &e.ActorID, &e.Event, &e.Note, &e.CreatedAt)
		return e, err
	}, `SELECT id, order_id, actor_id, event, note, created_at FROM order_events WHERE order_id = ? ORDER BY id ASC`, orderID)
}

func PayoutAccountFor(ctx context.Context, ex db.Executor, userID int) (*model.PayoutAccount, error) {
	return queryOne(ctx, ex, func(s rowScanner) (model.PayoutAccount, error) {
		var a model.PayoutAccount
		err := s.Scan(&a.UserID, &a.BankName, &a.AccountNumber, &a.AccountName, &a.UpdatedAt)
		return a, err
	}, `SELECT user_id, bank_name, account_number, account_name, updated_at FROM payout_accounts WHERE user_id = ?`, userID)
}

func SavePayoutAccount(ctx context.Context, ex db.Executor, a model.PayoutAccount) error {
	now := db.Now()
	res, err := ex.Exec(ctx, `UPDATE payout_accounts SET bank_name = ?, account_number = ?, account_name = ?, updated_at = ? WHERE user_id = ?`,
		a.BankName, a.AccountNumber, a.AccountName, now, a.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO payout_accounts (user_id, bank_name, account_number, account_name, updated_at) VALUES (?,?,?,?,?)`,
		a.UserID, a.BankName, a.AccountNumber, a.AccountName, now)
	return err
}
