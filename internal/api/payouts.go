package api

import (
	"net/http"
	"regexp"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Where a seller's money goes, plus the admin's view of orders and payouts. Cribby's team sends payouts from
// the Cribby bank account and then marks them paid here; nothing pays out automatically.

var accountNumberRe = regexp.MustCompile(`^[0-9]{10}$`)

func (s *Server) myPayoutAccount(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	a, err := store.PayoutAccountFor(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	if a == nil {
		a = &model.PayoutAccount{UserID: user.ID}
	}
	writeJSON(w, http.StatusOK, map[string]any{"set": a.AccountNumber != "", "bank_name": a.BankName,
		"account_number": a.AccountNumber, "account_name": a.AccountName, "payments_on": s.canTakePayments(),
		"fee_percent": s.cfg.PlatformFeePercent})
	return nil
}

func (s *Server) savePayoutAccount(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var in struct {
		BankName      string `json:"bank_name" validate:"required"`
		AccountNumber string `json:"account_number" validate:"required"`
		AccountName   string `json:"account_name" validate:"required"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	a := model.PayoutAccount{UserID: user.ID, BankName: strings.TrimSpace(in.BankName),
		AccountNumber: strings.TrimSpace(in.AccountNumber), AccountName: strings.TrimSpace(in.AccountName)}
	if n := len([]rune(a.BankName)); n < 2 || n > 60 {
		return httpErr(http.StatusUnprocessableEntity, "Enter your bank's name")
	}
	if !accountNumberRe.MatchString(a.AccountNumber) {
		return httpErr(http.StatusUnprocessableEntity, "An account number has 10 digits")
	}
	if n := len([]rune(a.AccountName)); n < 2 || n > 100 {
		return httpErr(http.StatusUnprocessableEntity, "Enter the name on the account")
	}
	if err := store.SavePayoutAccount(r.Context(), s.DB.Executor, a); err != nil {
		return err
	}
	return s.myPayoutAccount(w, r)
}

type adminOrder struct {
	orderOut
	BuyerName   string               `json:"buyer_name"`
	BuyerEmail  string               `json:"buyer_email"`
	SellerName  string               `json:"seller_name"`
	SellerEmail string               `json:"seller_email"`
	Payout      *model.PayoutAccount `json:"payout_account"`
}

func (s *Server) adminView(r *http.Request, admin *model.User, o model.Order) adminOrder {
	ctx, ex := r.Context(), s.DB.Executor
	out := adminOrder{orderOut: s.presentOrder(ctx, o, admin, true)}
	// admins see both phones and the payout reference
	out.BuyerPhone, out.SellerPhone, out.PayoutReference = o.BuyerPhone, o.SellerPhone, o.PayoutReference
	out.PaymentURL = ""
	if u, err := store.UserByID(ctx, ex, o.BuyerID); err == nil && u != nil {
		out.BuyerName, out.BuyerEmail = u.Name, u.Email
	}
	if u, err := store.UserByID(ctx, ex, o.SellerID); err == nil && u != nil {
		out.SellerName, out.SellerEmail = u.Name, u.Email
	}
	out.Payout, _ = store.PayoutAccountFor(ctx, ex, o.SellerID)
	return out
}

func (s *Server) adminOrders(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = "disputed"
	}
	switch status {
	case "awaiting_payment", "paid", "handed_over", "completed", "disputed", "refunded", "cancelled":
	default:
		return httpErr(http.StatusUnprocessableEntity, "Unknown status")
	}
	rows, err := store.OrdersInStatus(r.Context(), s.DB.Executor, status, 200)
	if err != nil {
		return err
	}
	out := make([]adminOrder, len(rows))
	for i, o := range rows {
		out[i] = s.adminView(r, admin, o)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// adminResolveOrder: release the money to the seller, or refund the buyer. For disputes, and for orders that got stuck.
func (s *Server) adminResolveOrder(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "order_id")
	if err != nil {
		return err
	}
	var in struct {
		Decision string `json:"decision" validate:"required,oneof=release|refund"`
		Note     string `json:"note"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	note := strings.TrimSpace(in.Note)
	if note == "" || len([]rune(note)) > maxReviewNoteLen {
		return httpErr(http.StatusUnprocessableEntity, "Write a short note on why (500 characters max)")
	}
	ctx := r.Context()
	o, err := store.OrderByID(ctx, s.DB.Executor, id)
	if err != nil {
		return err
	}
	if o == nil {
		return httpErr(http.StatusNotFound, "Order not found")
	}
	from := []string{"paid", "handed_over", "disputed"}
	if in.Decision == "release" {
		ok, err := s.release(ctx, o, from, admin, "admin_released", note)
		if err != nil {
			return err
		}
		if !ok {
			return httpErr(http.StatusConflict, "This order has already moved on")
		}
		s.tell(ctx, o.BuyerID, "We looked into your order", "After checking, the money was released to the seller.", o.Title, "Note from the Cribby team: "+note)
	} else {
		if err := s.refund(ctx, o, from, o.Status, admin, note); err != nil {
			return err
		}
		s.tell(ctx, o.BuyerID, "You've been refunded", "After checking, your money is being returned in full.", o.Title, "Note from the Cribby team: "+note)
		s.tell(ctx, o.SellerID, "The order was refunded to the buyer", "After checking, the Cribby team refunded this order.", o.Title, "Note from the Cribby team: "+note)
	}
	cur, err := store.OrderByID(ctx, s.DB.Executor, id)
	if err != nil || cur == nil {
		return httpErr(http.StatusNotFound, "Order not found")
	}
	writeJSON(w, http.StatusOK, s.adminView(r, admin, *cur))
	return nil
}

func (s *Server) adminPayouts(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = "queued"
	}
	if status != "queued" && status != "paid" {
		return httpErr(http.StatusUnprocessableEntity, "status must be queued or paid")
	}
	rows, err := store.OrdersWithPayout(r.Context(), s.DB.Executor, status, 200)
	if err != nil {
		return err
	}
	out := make([]adminOrder, len(rows))
	for i, o := range rows {
		out[i] = s.adminView(r, admin, o)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminPayoutPaid(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "order_id")
	if err != nil {
		return err
	}
	var in struct {
		Reference string `json:"reference"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	ref := strings.TrimSpace(in.Reference)
	if len([]rune(ref)) > 120 {
		return httpErr(http.StatusUnprocessableEntity, "That reference is too long")
	}
	ok, err := store.MarkPayoutPaid(r.Context(), s.DB.Executor, id, ref)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "There is no queued payout for this order")
	}
	s.event(r.Context(), id, admin, "payout_paid", ref)
	o, err := store.OrderByID(r.Context(), s.DB.Executor, id)
	if err != nil || o == nil {
		return httpErr(http.StatusNotFound, "Order not found")
	}
	writeJSON(w, http.StatusOK, s.adminView(r, admin, *o))
	return nil
}
