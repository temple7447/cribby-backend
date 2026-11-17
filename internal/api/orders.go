package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/payments"
	"campus-backend/internal/security"
	"campus-backend/internal/store"
)

// Marketplace orders with the money held (escrow-style).
//
//	awaiting_payment -> paid -> handed_over -> completed (money released to the seller)
//	                      \________\__ disputed -> completed | refunded   (an admin decides)
//	paid | handed_over -> refunded (the seller backs out, or never hands it over for 7 days)
//
// The buyer pays on Paystack's page. The server only believes Paystack: a signed webhook, or its own call to
// Paystack's verify endpoint, never the app. The money stays with Cribby until the buyer confirms (or 3 days
// pass after the seller says it was handed over with no objection). Every change goes through a conditional
// update, so a double tap or a webhook racing a timer can never release or refund twice.

const (
	minOrderPrice     = 500       // naira
	payWindow         = time.Hour // how long a started payment blocks other buyers
	handoverDeadline  = 7 * 24 * time.Hour
	maxDisputeReason  = 500
	orderEventPayment = "paid"
)

func (s *Server) canTakePayments() bool { return s.pay != nil && s.pay.Configured() }

// feeFor is Cribby's cut: a percentage of the price, rounded to the nearest naira.
func feeFor(price, percent int) int { return (price*percent + 50) / 100 }

type orderOut struct {
	model.Order
	Role      string             `json:"role"` // buyer | seller
	Other     string             `json:"other"`
	ItemPhoto string             `json:"item_photo,omitempty"`
	Events    []model.OrderEvent `json:"events,omitempty"`
}

var phoneVisible = map[string]bool{"paid": true, "handed_over": true, "disputed": true, "completed": true}

// presentOrder hides what the person shouldn't see: the other side's phone until the money is held, the
// payment link once it is paid, and the payout reference from buyers.
func (s *Server) presentOrder(ctx context.Context, o model.Order, viewer *model.User, withEvents bool) orderOut {
	out := orderOut{Order: o}
	otherID := o.SellerID
	out.Role = "buyer"
	if viewer.ID == o.SellerID {
		out.Role, otherID = "seller", o.BuyerID
	}
	if u, err := store.UserByID(ctx, s.DB.Executor, otherID); err == nil && u != nil {
		out.Other = firstName(u.Name)
	}
	if !viewer.IsAdmin || (viewer.ID == o.BuyerID || viewer.ID == o.SellerID) {
		if out.Role == "buyer" {
			out.BuyerPhone, out.PayoutReference = "", ""
		} else {
			out.SellerPhone, out.PaymentURL = "", ""
		}
	}
	if !phoneVisible[o.Status] {
		out.BuyerPhone, out.SellerPhone = "", ""
	}
	if o.Status != "awaiting_payment" {
		out.PaymentURL = ""
	}
	if it, err := store.MarketItemByID(ctx, s.DB.Executor, o.ItemID); err == nil && it != nil && len(it.Photos) > 0 {
		out.ItemPhoto = it.Photos[0].URL
	}
	if withEvents {
		out.Events, _ = store.OrderEvents(ctx, s.DB.Executor, o.ID)
	}
	return out
}

// order loads {order_id} for someone who is part of it (or an admin).
func (s *Server) order(r *http.Request, user *model.User) (*model.Order, error) {
	id, err := pathInt(r, "order_id")
	if err != nil {
		return nil, err
	}
	o, err := store.OrderByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if o == nil || (o.BuyerID != user.ID && o.SellerID != user.ID && !user.IsAdmin) {
		return nil, httpErr(http.StatusNotFound, "Order not found")
	}
	return o, nil
}

// tell emails someone about their order. A failed email never undoes anything.
func (s *Server) tell(ctx context.Context, userID int, title, intro, item, callout string) {
	u, err := store.UserByID(ctx, s.DB.Executor, userID)
	if err != nil || u == nil {
		return
	}
	if err := s.mail.SendOrderUpdate(ctx, u.Email, title, intro, item, callout); err != nil {
		s.log.Warn("order email failed", "to", u.Email, "err", err)
	}
}

func (s *Server) event(ctx context.Context, orderID int, actor *model.User, event, note string) {
	var actorID *int
	if actor != nil {
		actorID = &actor.ID
	}
	if err := store.AddOrderEvent(ctx, s.DB.Executor, orderID, actorID, event, note); err != nil {
		s.log.Warn("order event not saved", "order", orderID, "err", err)
	}
}

func (s *Server) setItemStatus(ctx context.Context, itemID int, from []string, to string) {
	if err := store.MoveMarketItem(ctx, s.DB.Executor, itemID, from, to); err != nil {
		s.log.Warn("item status not updated", "item", itemID, "err", err)
	}
}

// ---- buying ----

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	if !s.canTakePayments() {
		return httpErr(http.StatusServiceUnavailable, "Paying through Cribby isn't switched on yet. Contact the seller directly")
	}
	if user.VerificationStatus != "verified" {
		return httpErr(http.StatusForbidden, "Confirm your email before you buy")
	}
	var in struct {
		BuyerPhone string `json:"buyer_phone" validate:"required"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	phone := normalizePhone(in.BuyerPhone)
	if phone == "" {
		return httpErr(http.StatusUnprocessableEntity, "Enter a phone number the seller can reach you on")
	}
	m, err := s.marketItem(r)
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	if m.UserID == user.ID {
		return httpErr(http.StatusConflict, "You can't buy your own item")
	}
	if m.Status != "active" {
		return httpErr(http.StatusConflict, "This item isn't available any more")
	}
	if m.Price < minOrderPrice {
		return httpErr(http.StatusUnprocessableEntity, "Only items of ₦"+itoa(minOrderPrice)+" or more can be paid for through Cribby")
	}
	if acct, err := store.PayoutAccountFor(ctx, ex, m.UserID); err != nil {
		return err
	} else if acct == nil {
		return httpErr(http.StatusConflict, "The seller hasn't set up how to get paid yet. Contact them directly")
	}
	if open, err := store.OpenOrderForItem(ctx, ex, m.ID, db.Time{Time: time.Now().UTC().Add(-payWindow)}); err != nil {
		return err
	} else if open != nil {
		if open.BuyerID == user.ID && open.Status == "awaiting_payment" && open.PaymentURL != "" {
			writeJSON(w, http.StatusOK, s.presentOrder(ctx, *open, user, false)) // pick up where you left off
			return nil
		}
		return httpErr(http.StatusConflict, "Someone else is already buying this")
	}
	fee := feeFor(m.Price, s.cfg.PlatformFeePercent)
	o := model.Order{ItemID: m.ID, BuyerID: user.ID, SellerID: m.UserID, Title: m.Title, Amount: m.Price, Fee: fee,
		SellerAmount: m.Price - fee, Reference: "cribby_" + strings.ReplaceAll(security.NewUUID(), "-", ""),
		BuyerPhone: phone, SellerPhone: m.ContactPhone}
	id, err := store.CreateOrder(ctx, ex, o)
	if err != nil {
		return err
	}
	url, err := s.pay.Initialize(ctx, payments.InitArgs{Email: user.Email, AmountKobo: o.Amount * 100, Reference: o.Reference,
		CallbackURL: s.cfg.PaymentReturnURL + "?order=" + itoa(id)})
	if err != nil {
		_, _ = store.CancelOrder(ctx, ex, id)
		s.log.Error("payment could not be started", "order", id, "err", err)
		return httpErr(http.StatusBadGateway, "We couldn't start the payment. Please try again")
	}
	if err := store.SetOrderPaymentURL(ctx, ex, id, url); err != nil {
		return err
	}
	s.event(ctx, id, user, "created", "")
	saved, err := store.OrderByID(ctx, ex, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, s.presentOrder(ctx, *saved, user, false))
	return nil
}

func (s *Server) myOrders(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	rows, err := store.OrdersForUser(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	out := make([]orderOut, len(rows))
	for i, o := range rows {
		out[i] = s.presentOrder(r.Context(), o, user, false)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	o, err := s.order(r, user)
	if err != nil {
		return err
	}
	// Coming back from Paystack before the webhook arrived: ask Paystack ourselves.
	if o.Status == "awaiting_payment" && s.canTakePayments() {
		if c, err := s.pay.Verify(r.Context(), o.Reference); err == nil && c.Paid && c.AmountKobo == o.Amount*100 {
			s.applyPayment(r.Context(), o)
			if o, err = store.OrderByID(r.Context(), s.DB.Executor, o.ID); err != nil || o == nil {
				return httpErr(http.StatusNotFound, "Order not found")
			}
		}
	}
	writeJSON(w, http.StatusOK, s.presentOrder(r.Context(), *o, user, true))
	return nil
}

// applyPayment records a confirmed payment. Safe to call twice. If the item can no longer be bought (or the
// order was already cancelled), the buyer's money goes straight back.
func (s *Server) applyPayment(ctx context.Context, o *model.Order) {
	ex := s.DB.Executor
	ok, err := store.MarkOrderPaid(ctx, ex, o.ID)
	if err != nil {
		s.log.Error("could not record payment", "order", o.ID, "err", err)
		return
	}
	if !ok {
		if cur, err := store.OrderByID(ctx, ex, o.ID); err == nil && cur != nil && cur.Status == "cancelled" {
			// Paid after the order was cancelled or expired.
			_ = s.refund(ctx, cur, []string{"cancelled"}, "cancelled", nil, "Paid after the order was cancelled")
		}
		return
	}
	s.event(ctx, o.ID, nil, orderEventPayment, "")
	item, err := store.MarketItemByID(ctx, ex, o.ItemID)
	if err != nil || item == nil || item.Status != "active" {
		_ = s.refund(ctx, o, []string{"paid"}, "paid", nil, "The item was no longer available")
		s.tell(ctx, o.BuyerID, "Your payment was refunded", "The item was sold or taken down just before your payment arrived, so we've sent your money back.", o.Title, "Refunds usually reach your card within a few days.")
		return
	}
	s.setItemStatus(ctx, o.ItemID, []string{"active"}, "reserved")
	s.tell(ctx, o.SellerID, "Someone bought your item", "A buyer has paid. Cribby is holding the money. Hand over the item, then tap “I handed it over” in the app.", o.Title,
		"You get ₦"+itoa(o.SellerAmount)+" (after Cribby's "+itoa(s.cfg.PlatformFeePercent)+"% fee) once the buyer confirms they have it.")
	s.tell(ctx, o.BuyerID, "Payment received", "Your money is safe with Cribby until you have the item. The seller will contact you to hand it over.", o.Title,
		"When you have it and it's fine, open the order and tap “I got it”. If something is wrong, tap “There's a problem” instead.")
}

// refund claims the order for a refund, asks Paystack, and puts the order back if Paystack refuses.
func (s *Server) refund(ctx context.Context, o *model.Order, from []string, revertTo string, actor *model.User, note string) error {
	ex := s.DB.Executor
	ok, err := store.RefundOrder(ctx, ex, o.ID, from, note)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "This order has already moved on")
	}
	if err := s.pay.Refund(ctx, o.Reference, o.Amount*100); err != nil {
		s.log.Error("refund failed", "order", o.ID, "err", err)
		_ = store.RevertRefund(ctx, ex, o.ID, revertTo)
		s.event(ctx, o.ID, actor, "refund_failed", note)
		return httpErr(http.StatusBadGateway, "The refund didn't go through. Please try again")
	}
	s.event(ctx, o.ID, actor, "refunded", note)
	s.setItemStatus(ctx, o.ItemID, []string{"reserved"}, "active")
	return nil
}

func (s *Server) release(ctx context.Context, o *model.Order, from []string, actor *model.User, event, note string) (bool, error) {
	ok, err := store.CompleteOrder(ctx, s.DB.Executor, o.ID, from, note)
	if err != nil || !ok {
		return false, err
	}
	s.event(ctx, o.ID, actor, event, note)
	s.setItemStatus(ctx, o.ItemID, []string{"reserved", "active"}, "sold")
	s.tell(ctx, o.SellerID, "Your money is on its way", "The sale is complete. We'll send ₦"+itoa(o.SellerAmount)+" to your bank account.", o.Title, "Payouts are sent by the Cribby team, usually within a day.")
	return true, nil
}

// ---- the steps ----

func (s *Server) orderHandedOver(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	o, err := s.order(r, user)
	if err != nil {
		return err
	}
	if o.SellerID != user.ID {
		return httpErr(http.StatusForbidden, "Only the seller can do this")
	}
	days := s.cfg.OrderAutoReleaseDays
	ok, err := store.MarkOrderHandedOver(r.Context(), s.DB.Executor, o.ID, db.Time{Time: time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)})
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "This order isn't waiting to be handed over")
	}
	s.event(r.Context(), o.ID, user, "handed_over", "")
	s.tell(r.Context(), o.BuyerID, "The seller says they handed it over", "If you have the item and it's fine, tap “I got it” in the app. If not, tap “There's a problem”.", o.Title,
		"If you do nothing, the money is released to the seller in "+itoa(days)+" days.")
	return s.respondOrder(w, r, o.ID, user)
}

func (s *Server) orderConfirm(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	o, err := s.order(r, user)
	if err != nil {
		return err
	}
	if o.BuyerID != user.ID {
		return httpErr(http.StatusForbidden, "Only the buyer can do this")
	}
	ok, err := s.release(r.Context(), o, []string{"paid", "handed_over"}, user, "confirmed", "")
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "This order can't be confirmed right now")
	}
	return s.respondOrder(w, r, o.ID, user)
}

func (s *Server) orderDispute(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	o, err := s.order(r, user)
	if err != nil {
		return err
	}
	if o.BuyerID != user.ID {
		return httpErr(http.StatusForbidden, "Only the buyer can do this")
	}
	var in struct {
		Reason string `json:"reason" validate:"required"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	reason := strings.TrimSpace(in.Reason)
	if n := len([]rune(reason)); n < 5 || n > maxDisputeReason {
		return httpErr(http.StatusUnprocessableEntity, "Tell us what's wrong (5 to 500 characters)")
	}
	ok, err := store.DisputeOrder(r.Context(), s.DB.Executor, o.ID, reason)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "You can only report a problem while the money is being held")
	}
	s.event(r.Context(), o.ID, user, "disputed", reason)
	s.tell(r.Context(), o.SellerID, "The buyer reported a problem", "The money stays held while the Cribby team looks into it. They may contact you.", o.Title, "Reason given: "+reason)
	return s.respondOrder(w, r, o.ID, user)
}

// orderCancel: the buyer drops an unpaid order, or the seller backs out of a paid one (the buyer is refunded in full).
func (s *Server) orderCancel(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	o, err := s.order(r, user)
	if err != nil {
		return err
	}
	ctx := r.Context()
	switch {
	case o.BuyerID == user.ID && o.Status == "awaiting_payment":
		ok, err := store.CancelOrder(ctx, s.DB.Executor, o.ID)
		if err != nil {
			return err
		}
		if !ok {
			return httpErr(http.StatusConflict, "This order has already moved on")
		}
		s.event(ctx, o.ID, user, "cancelled", "")
	case o.SellerID == user.ID && (o.Status == "paid" || o.Status == "handed_over"):
		if err := s.refund(ctx, o, []string{"paid", "handed_over"}, o.Status, user, "The seller cancelled"); err != nil {
			return err
		}
		s.tell(ctx, o.BuyerID, "Your order was cancelled and refunded", "The seller cancelled, so your money is being returned in full.", o.Title, "Refunds usually reach your card within a few days.")
	default:
		return httpErr(http.StatusConflict, "This order can't be cancelled now. If something is wrong, report a problem")
	}
	return s.respondOrder(w, r, o.ID, user)
}

func (s *Server) respondOrder(w http.ResponseWriter, r *http.Request, id int, user *model.User) error {
	o, err := store.OrderByID(r.Context(), s.DB.Executor, id)
	if err != nil || o == nil {
		return httpErr(http.StatusNotFound, "Order not found")
	}
	writeJSON(w, http.StatusOK, s.presentOrder(r.Context(), *o, user, true))
	return nil
}

// ---- Paystack tells us ----

func (s *Server) paystackWebhook(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return httpErr(http.StatusBadRequest, "Unreadable body")
	}
	if s.pay == nil || !s.pay.ValidWebhook(body, r.Header.Get("X-Paystack-Signature")) {
		return httpErr(http.StatusUnauthorized, "Bad signature")
	}
	var ev struct {
		Event string `json:"event"`
		Data  struct {
			Reference string `json:"reference"`
			Amount    int    `json:"amount"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return httpErr(http.StatusBadRequest, "Bad body")
	}
	if ev.Event == "charge.success" && ev.Data.Status == "success" {
		o, err := store.OrderByReference(r.Context(), s.DB.Executor, ev.Data.Reference)
		if err != nil {
			return err
		}
		switch {
		case o == nil:
		case ev.Data.Amount != o.Amount*100:
			s.log.Error("payment amount does not match the order", "order", o.ID, "paid", ev.Data.Amount, "expected", o.Amount*100)
			s.event(r.Context(), o.ID, nil, "amount_mismatch", "")
		default:
			s.applyPayment(r.Context(), o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// ---- timers ----

// SweepOrders does what time requires: release money nobody objected to, refund buyers whose seller never
// handed the item over, and drop payments nobody finished.
func (s *Server) SweepOrders(ctx context.Context, now time.Time) {
	ex := s.DB.Executor
	due, err := store.OrdersDueForRelease(ctx, ex, db.Time{Time: now})
	if err != nil {
		s.log.Error("order sweep failed", "err", err)
		return
	}
	for i := range due {
		o := due[i]
		if ok, err := s.release(ctx, &o, []string{"handed_over"}, nil, "auto_released", "No objection within "+itoa(s.cfg.OrderAutoReleaseDays)+" days"); err != nil {
			s.log.Error("auto release failed", "order", o.ID, "err", err)
		} else if ok {
			s.tell(ctx, o.BuyerID, "The money was released to the seller", "You didn't report a problem within "+itoa(s.cfg.OrderAutoReleaseDays)+" days of the handover, so the sale is complete.", o.Title, "")
		}
	}
	stale, err := store.OrdersNeverHandedOver(ctx, ex, db.Time{Time: now.Add(-handoverDeadline)})
	if err == nil {
		for i := range stale {
			o := stale[i]
			if err := s.refund(ctx, &o, []string{"paid"}, "paid", nil, "The seller never handed it over"); err == nil {
				s.tell(ctx, o.BuyerID, "Your order was refunded", "The seller didn't hand the item over within 7 days, so your money is being returned in full.", o.Title, "")
			}
		}
	}
	abandoned, err := store.OrdersAbandoned(ctx, ex, db.Time{Time: now.Add(-payWindow)})
	if err == nil {
		for _, o := range abandoned {
			if ok, _ := store.CancelOrder(ctx, ex, o.ID); ok {
				s.event(ctx, o.ID, nil, "expired", "")
			}
		}
	}
}

// RunOrderSweeper runs SweepOrders every few minutes until ctx ends.
func (s *Server) RunOrderSweeper(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SweepOrders(ctx, time.Now().UTC())
		}
	}
}
