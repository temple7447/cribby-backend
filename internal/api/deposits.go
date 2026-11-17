package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/payments"
	"campus-backend/internal/security"
	"campus-backend/internal/store"
)

// Pre-arrival deposits: once a landlord approves a booking, the student can pay a deposit to hold the home.
// Cribby keeps the money until the student confirms the move-in, or a few days pass after the move-in date
// with no complaint. Same safety rules as marketplace orders: only Paystack is believed, every change is a
// conditional update, refunds claim the status first.
//
//	awaiting_payment -> paid -> completed (queued for the landlord)
//	                      \__ disputed -> completed | refunded (an admin decides)
//	paid -> refunded (the landlord can't give the home, or the student cancels well before moving in)

const (
	depositFreeCancelDays = 7 // a student may cancel for a full refund while move-in is further away than this
	minDepositAmount      = 500
)

type depositOut struct {
	model.Deposit
	Role  string `json:"role"` // tenant | landlord
	Other string `json:"other"`
}

func (s *Server) presentDeposit(ctx context.Context, d model.Deposit, viewer *model.User) depositOut {
	out := depositOut{Deposit: d, Role: "tenant"}
	otherID := d.LandlordID
	if viewer.ID == d.LandlordID {
		out.Role, otherID = "landlord", d.TenantID
	}
	if u, err := store.UserByID(ctx, s.DB.Executor, otherID); err == nil && u != nil {
		out.Other = firstName(u.Name)
	}
	if out.Role == "tenant" {
		out.PayoutReference = ""
	} else {
		out.PaymentURL = ""
	}
	if d.Status != "paid" && d.Status != "disputed" && d.Status != "completed" {
		out.TenantPhone = ""
	}
	if d.Status != "awaiting_payment" {
		out.PaymentURL = ""
	}
	return out
}

func (s *Server) deposit(r *http.Request, user *model.User) (*model.Deposit, error) {
	id, err := pathInt(r, "deposit_id")
	if err != nil {
		return nil, err
	}
	d, err := store.DepositByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if d == nil || (d.TenantID != user.ID && d.LandlordID != user.ID && !user.IsAdmin) {
		return nil, httpErr(http.StatusNotFound, "Deposit not found")
	}
	return d, nil
}

func (s *Server) respondDeposit(w http.ResponseWriter, r *http.Request, id int, user *model.User) error {
	d, err := store.DepositByID(r.Context(), s.DB.Executor, id)
	if err != nil || d == nil {
		return httpErr(http.StatusNotFound, "Deposit not found")
	}
	writeJSON(w, http.StatusOK, s.presentDeposit(r.Context(), *d, user))
	return nil
}

// depositAmount is what the landlord asked for as a deposit, or one month's rent when they didn't say.
func depositAmount(l *model.Listing) int {
	if l.Deposit > 0 {
		return l.Deposit
	}
	return l.PricePer
}

func (s *Server) createDeposit(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	if !s.canTakePayments() {
		return httpErr(http.StatusServiceUnavailable, "Paying a deposit through Cribby isn't switched on yet")
	}
	if user.VerificationStatus != "verified" {
		return httpErr(http.StatusForbidden, "Confirm your email before you pay")
	}
	bookingID, err := pathInt(r, "booking_id")
	if err != nil {
		return err
	}
	var in struct {
		TenantPhone string `json:"tenant_phone" validate:"required"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	phone := normalizePhone(in.TenantPhone)
	if phone == "" {
		return httpErr(http.StatusUnprocessableEntity, "Enter a phone number the landlord can reach you on")
	}
	ctx, ex := r.Context(), s.DB.Executor
	ownerID, listingID, status, moveIn, found, err := store.BookingDepositFacts(ctx, ex, bookingID)
	if err != nil {
		return err
	}
	if !found || ownerID != user.ID {
		return httpErr(http.StatusNotFound, "Booking not found")
	}
	if status != "approved" {
		return httpErr(http.StatusConflict, "You can pay the deposit once the landlord approves your booking")
	}
	l, err := store.ListingByID(ctx, ex, listingID)
	if err != nil {
		return err
	}
	if l == nil || l.OwnerID == nil {
		return httpErr(http.StatusConflict, "This home isn't managed by a landlord on Cribby, so the deposit can't be paid here")
	}
	amount := depositAmount(l)
	if amount < minDepositAmount {
		return httpErr(http.StatusUnprocessableEntity, "This home has no deposit set. Ask the landlord")
	}
	if acct, err := store.PayoutAccountFor(ctx, ex, *l.OwnerID); err != nil {
		return err
	} else if acct == nil {
		return httpErr(http.StatusConflict, "The landlord hasn't set up how to get paid yet. Ask them to add a bank account in the app")
	}
	if open, err := store.OpenDepositForBooking(ctx, ex, bookingID, db.Time{Time: time.Now().UTC().Add(-payWindow)}); err != nil {
		return err
	} else if open != nil {
		if open.Status == "awaiting_payment" && open.PaymentURL != "" {
			writeJSON(w, http.StatusOK, s.presentDeposit(ctx, *open, user))
			return nil
		}
		return httpErr(http.StatusConflict, "A deposit for this booking is already being held")
	}
	fee := feeFor(amount, s.cfg.PlatformFeePercent)
	d := model.Deposit{BookingID: bookingID, ListingID: listingID, TenantID: user.ID, LandlordID: *l.OwnerID, Title: l.Title,
		Amount: amount, Fee: fee, LandlordAmount: amount - fee, Reference: "cribbydep_" + strings.ReplaceAll(security.NewUUID(), "-", ""),
		TenantPhone: phone, MoveInDate: moveIn}
	id, err := store.CreateDeposit(ctx, ex, d)
	if err != nil {
		return err
	}
	url, err := s.pay.Initialize(ctx, payments.InitArgs{Email: user.Email, AmountKobo: amount * 100, Reference: d.Reference,
		CallbackURL: s.cfg.PaymentReturnURL + "?deposit=" + itoa(id)})
	if err != nil {
		_, _ = store.CancelDeposit(ctx, ex, id)
		s.log.Error("deposit payment could not be started", "deposit", id, "err", err)
		return httpErr(http.StatusBadGateway, "We couldn't start the payment. Please try again")
	}
	if err := store.SetDepositPaymentURL(ctx, ex, id, url); err != nil {
		return err
	}
	saved, err := store.DepositByID(ctx, ex, id)
	if err != nil || saved == nil {
		return httpErr(http.StatusNotFound, "Deposit not found")
	}
	writeJSON(w, http.StatusCreated, s.presentDeposit(ctx, *saved, user))
	return nil
}

func (s *Server) myDeposits(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	rows, err := store.DepositsForUser(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	out := make([]depositOut, len(rows))
	for i, d := range rows {
		out[i] = s.presentDeposit(r.Context(), d, user)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getDeposit(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	d, err := s.deposit(r, user)
	if err != nil {
		return err
	}
	if d.Status == "awaiting_payment" && s.canTakePayments() {
		if c, err := s.pay.Verify(r.Context(), d.Reference); err == nil && c.Paid && c.AmountKobo == d.Amount*100 {
			s.applyDepositPayment(r.Context(), d)
		}
	}
	return s.respondDeposit(w, r, d.ID, user)
}

// applyDepositPayment records a confirmed payment. Safe to call twice.
func (s *Server) applyDepositPayment(ctx context.Context, d *model.Deposit) {
	ex := s.DB.Executor
	base := time.Now().UTC()
	if m := d.MoveInDate.Time; m.After(base) {
		base = m
	}
	release := db.Time{Time: base.Add(time.Duration(s.cfg.OrderAutoReleaseDays) * 24 * time.Hour)}
	ok, err := store.MarkDepositPaid(ctx, ex, d.ID, release)
	if err != nil {
		s.log.Error("could not record deposit payment", "deposit", d.ID, "err", err)
		return
	}
	if !ok {
		if cur, err := store.DepositByID(ctx, ex, d.ID); err == nil && cur != nil && cur.Status == "cancelled" {
			_ = s.refundDeposit(ctx, cur, []string{"cancelled"}, "cancelled", "Paid after the deposit was cancelled")
		}
		return
	}
	if err := store.SetBookingDepositPaid(ctx, ex, d.BookingID, true); err != nil {
		s.log.Warn("booking deposit tick not saved", "booking", d.BookingID, "err", err)
	}
	s.tell(ctx, d.LandlordID, "A student paid the deposit", "A deposit for your home is being held by Cribby. The home is theirs from the move-in date.", d.Title,
		"You get ₦"+itoa(d.LandlordAmount)+" (after Cribby's "+itoa(s.cfg.PlatformFeePercent)+"% fee) once the student confirms the move-in.")
	s.tell(ctx, d.TenantID, "Deposit received", "Your deposit is safe with Cribby until you've moved in.", d.Title,
		"After you move in and the home is as described, open the app and tap “I moved in”. If something is wrong, tap “There's a problem”.")
}

func (s *Server) refundDeposit(ctx context.Context, d *model.Deposit, from []string, revertTo, note string) error {
	ex := s.DB.Executor
	ok, err := store.RefundDeposit(ctx, ex, d.ID, from, note)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "This deposit has already moved on")
	}
	if err := s.pay.Refund(ctx, d.Reference, d.Amount*100); err != nil {
		s.log.Error("deposit refund failed", "deposit", d.ID, "err", err)
		_ = store.RevertDepositRefund(ctx, ex, d.ID, revertTo)
		return httpErr(http.StatusBadGateway, "The refund didn't go through. Please try again")
	}
	_ = store.SetBookingDepositPaid(ctx, ex, d.BookingID, false)
	return nil
}

func (s *Server) releaseDeposit(ctx context.Context, d *model.Deposit, from []string, note string) (bool, error) {
	ok, err := store.CompleteDeposit(ctx, s.DB.Executor, d.ID, from, note)
	if err != nil || !ok {
		return false, err
	}
	s.tell(ctx, d.LandlordID, "Your deposit is on its way", "The move-in is confirmed. We'll send ₦"+itoa(d.LandlordAmount)+" to your bank account.", d.Title, "Payouts are sent by the Cribby team, usually within a day.")
	return true, nil
}

func (s *Server) depositConfirm(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	d, err := s.deposit(r, user)
	if err != nil {
		return err
	}
	if d.TenantID != user.ID {
		return httpErr(http.StatusForbidden, "Only the student can do this")
	}
	ok, err := s.releaseDeposit(r.Context(), d, []string{"paid"}, "Student confirmed the move-in")
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "This deposit can't be confirmed right now")
	}
	return s.respondDeposit(w, r, d.ID, user)
}

func (s *Server) depositDispute(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	d, err := s.deposit(r, user)
	if err != nil {
		return err
	}
	if d.TenantID != user.ID {
		return httpErr(http.StatusForbidden, "Only the student can do this")
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
	ok, err := store.DisputeDeposit(r.Context(), s.DB.Executor, d.ID, reason)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "You can only report a problem while the deposit is being held")
	}
	s.tell(r.Context(), d.LandlordID, "The student reported a problem", "The deposit stays held while the Cribby team looks into it. They may contact you.", d.Title, "Reason given: "+reason)
	return s.respondDeposit(w, r, d.ID, user)
}

// depositCancel: the student drops an unpaid deposit, or (well before move-in) a paid one; the landlord can
// always back out of a paid one. Paid deposits are refunded in full.
func (s *Server) depositCancel(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	d, err := s.deposit(r, user)
	if err != nil {
		return err
	}
	ctx := r.Context()
	early := time.Until(d.MoveInDate.Time) > depositFreeCancelDays*24*time.Hour
	switch {
	case d.TenantID == user.ID && d.Status == "awaiting_payment":
		ok, err := store.CancelDeposit(ctx, s.DB.Executor, d.ID)
		if err != nil {
			return err
		}
		if !ok {
			return httpErr(http.StatusConflict, "This deposit has already moved on")
		}
	case d.Status == "paid" && (d.LandlordID == user.ID || (d.TenantID == user.ID && early)):
		note := "The landlord cancelled"
		if d.TenantID == user.ID {
			note = "The student cancelled before moving in"
		}
		if err := s.refundDeposit(ctx, d, []string{"paid"}, "paid", note); err != nil {
			return err
		}
		other, msg := d.TenantID, "The landlord can't give you the home, so your deposit is being returned in full."
		if d.TenantID == user.ID {
			other, msg = d.LandlordID, "The student cancelled, so their deposit was returned."
		}
		s.tell(ctx, other, "A deposit was cancelled and refunded", msg, d.Title, "Refunds usually reach the card within a few days.")
	default:
		return httpErr(http.StatusConflict, "This deposit can't be cancelled now. If something is wrong, report a problem")
	}
	return s.respondDeposit(w, r, d.ID, user)
}

// ---- admin ----

type adminDeposit struct {
	depositOut
	TenantName    string               `json:"tenant_name"`
	TenantEmail   string               `json:"tenant_email"`
	LandlordName  string               `json:"landlord_name"`
	LandlordEmail string               `json:"landlord_email"`
	Payout        *model.PayoutAccount `json:"payout_account"`
}

func (s *Server) adminDepositView(r *http.Request, admin *model.User, d model.Deposit) adminDeposit {
	ctx, ex := r.Context(), s.DB.Executor
	out := adminDeposit{depositOut: s.presentDeposit(ctx, d, admin)}
	out.TenantPhone, out.PayoutReference, out.PaymentURL = d.TenantPhone, d.PayoutReference, ""
	if u, err := store.UserByID(ctx, ex, d.TenantID); err == nil && u != nil {
		out.TenantName, out.TenantEmail = u.Name, u.Email
	}
	if u, err := store.UserByID(ctx, ex, d.LandlordID); err == nil && u != nil {
		out.LandlordName, out.LandlordEmail = u.Name, u.Email
	}
	out.Payout, _ = store.PayoutAccountFor(ctx, ex, d.LandlordID)
	return out
}

func (s *Server) adminDeposits(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = "disputed"
	}
	switch status {
	case "awaiting_payment", "paid", "completed", "disputed", "refunded", "cancelled":
	default:
		return httpErr(http.StatusUnprocessableEntity, "Unknown status")
	}
	rows, err := store.DepositsInStatus(r.Context(), s.DB.Executor, status, 200)
	if err != nil {
		return err
	}
	out := make([]adminDeposit, len(rows))
	for i, d := range rows {
		out[i] = s.adminDepositView(r, admin, d)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminResolveDeposit(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "deposit_id")
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
	d, err := store.DepositByID(ctx, s.DB.Executor, id)
	if err != nil {
		return err
	}
	if d == nil {
		return httpErr(http.StatusNotFound, "Deposit not found")
	}
	from := []string{"paid", "disputed"}
	if in.Decision == "release" {
		ok, err := s.releaseDeposit(ctx, d, from, note)
		if err != nil {
			return err
		}
		if !ok {
			return httpErr(http.StatusConflict, "This deposit has already moved on")
		}
		s.tell(ctx, d.TenantID, "We looked into your deposit", "After checking, the deposit was released to the landlord.", d.Title, "Note from the Cribby team: "+note)
	} else {
		if err := s.refundDeposit(ctx, d, from, d.Status, note); err != nil {
			return err
		}
		s.tell(ctx, d.TenantID, "You've been refunded", "After checking, your deposit is being returned in full.", d.Title, "Note from the Cribby team: "+note)
		s.tell(ctx, d.LandlordID, "The deposit was refunded to the student", "After checking, the Cribby team refunded this deposit.", d.Title, "Note from the Cribby team: "+note)
	}
	cur, err := store.DepositByID(ctx, s.DB.Executor, id)
	if err != nil || cur == nil {
		return httpErr(http.StatusNotFound, "Deposit not found")
	}
	writeJSON(w, http.StatusOK, s.adminDepositView(r, admin, *cur))
	return nil
}

func (s *Server) adminDepositPayouts(w http.ResponseWriter, r *http.Request) error {
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
	rows, err := store.DepositsWithPayout(r.Context(), s.DB.Executor, status, 200)
	if err != nil {
		return err
	}
	out := make([]adminDeposit, len(rows))
	for i, d := range rows {
		out[i] = s.adminDepositView(r, admin, d)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminDepositPayoutPaid(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "deposit_id")
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
	ok, err := store.MarkDepositPayoutPaid(r.Context(), s.DB.Executor, id, ref)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusConflict, "There is no queued payout for this deposit")
	}
	d, err := store.DepositByID(r.Context(), s.DB.Executor, id)
	if err != nil || d == nil {
		return httpErr(http.StatusNotFound, "Deposit not found")
	}
	writeJSON(w, http.StatusOK, s.adminDepositView(r, admin, *d))
	return nil
}

// ---- timers ----

// SweepDeposits releases deposits nobody complained about and drops payments nobody finished.
func (s *Server) SweepDeposits(ctx context.Context, now time.Time) {
	ex := s.DB.Executor
	due, err := store.DepositsDueForRelease(ctx, ex, db.Time{Time: now})
	if err != nil {
		s.log.Error("deposit sweep failed", "err", err)
		return
	}
	for i := range due {
		d := due[i]
		if ok, err := s.releaseDeposit(ctx, &d, []string{"paid"}, "No complaint within "+itoa(s.cfg.OrderAutoReleaseDays)+" days of the move-in date"); err != nil {
			s.log.Error("deposit auto release failed", "deposit", d.ID, "err", err)
		} else if ok {
			s.tell(ctx, d.TenantID, "Your deposit was released to the landlord", "You didn't report a problem within "+itoa(s.cfg.OrderAutoReleaseDays)+" days of your move-in date, so the deposit went to the landlord.", d.Title, "")
		}
	}
	if abandoned, err := store.DepositsAbandoned(ctx, ex, db.Time{Time: now.Add(-payWindow)}); err == nil {
		for _, d := range abandoned {
			_, _ = store.CancelDeposit(ctx, ex, d.ID)
		}
	}
}
