package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type lease struct {
	e                               *env
	tenant, landlord, admin         string
	tenantID, landlordID, listingID int
}

func newLease(t *testing.T) *lease {
	e := newEnv(t)
	l := &lease{e: e}
	var tu, lu, au obj
	l.tenant, tu = e.signup("tenant@gmail.com")
	l.landlord, lu = e.signup("landlord@gmail.com")
	l.admin, au = e.signup("boss@gmail.com")
	e.verifyUser(id(tu))
	e.verifyUser(id(lu))
	e.makeAdmin(id(au))
	l.tenantID, l.landlordID = id(tu), id(lu)
	l.listingID = e.createListing()
	e.exec(`UPDATE listings SET owner_id = ? WHERE id = ?`, l.landlordID, l.listingID)
	return l
}

func (l *lease) book(daysAway int) int {
	date := time.Now().UTC().Add(time.Duration(daysAway) * 24 * time.Hour).Format("2006-01-02")
	return id(l.e.expect(201, "POST", "/bookings", l.tenant, obj{"listing_id": l.listingID, "move_in_date": date}).(obj))
}

func (l *lease) approve(b int) {
	l.e.expect(200, "PATCH", path("/bookings/%d", b), l.landlord, obj{"status": "approved"})
}

func (l *lease) deposit(b int) obj {
	return l.e.expect(201, "POST", path("/bookings/%d/deposit", b), l.tenant, obj{"tenant_phone": "0809 111 2222"}).(obj)
}

func (l *lease) ref(dep int) string {
	var ref string
	if err := l.e.db.QueryRow(context.Background(), `SELECT reference FROM deposits WHERE id = ?`, dep).Scan(&ref); err != nil {
		l.e.t.Fatal(err)
	}
	return ref
}

func (l *lease) webhook(ref string, kobo int) {
	body, _ := json.Marshal(obj{"event": "charge.success", "data": obj{"reference": ref, "amount": kobo, "status": "success"}})
	req, _ := http.NewRequest(http.MethodPost, l.e.srv.URL+"/api/v1/webhooks/paystack", bytes.NewReader(body))
	req.Header.Set("X-Paystack-Signature", "valid-signature")
	if code, _ := l.e.send(req, ""); code != 200 {
		l.e.t.Fatalf("webhook: %d", code)
	}
}

func (l *lease) paidDeposit(daysAway int) (booking, dep int) {
	booking = l.book(daysAway)
	l.approve(booking)
	dep = id(l.deposit(booking))
	l.webhook(l.ref(dep), 80_000)
	return
}

func (l *lease) get(token string, dep int) obj {
	return l.e.expect(200, "GET", path("/deposits/%d", dep), token, nil).(obj)
}

func (l *lease) bookingPaid(b int) bool {
	return l.e.count(`SELECT COUNT(*) FROM bookings WHERE id = ? AND deposit_paid = ?`, b, true) == 1
}

func TestDepositHeldThenReleasedOnMoveIn(t *testing.T) {
	l := newLease(t)
	e := l.e
	b := l.book(30)

	e.expect(409, "POST", path("/bookings/%d/deposit", b), l.tenant, obj{"tenant_phone": "08091112222"}) // not approved yet
	l.approve(b)
	e.expect(409, "POST", path("/bookings/%d/deposit", b), l.tenant, obj{"tenant_phone": "08091112222"}) // landlord can't be paid yet
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	e.pay.on = false
	e.expect(503, "POST", path("/bookings/%d/deposit", b), l.tenant, obj{"tenant_phone": "08091112222"})
	e.pay.on = true
	e.expect(404, "POST", path("/bookings/%d/deposit", b), l.landlord, obj{"tenant_phone": "08091112222"}) // not their booking
	e.expect(422, "POST", path("/bookings/%d/deposit", b), l.tenant, obj{"tenant_phone": "abc"})

	d := l.deposit(b)
	dep := id(d)
	if d["status"] != "awaiting_payment" || d["amount"] != 800.0 || d["fee"] != 40.0 || d["landlord_amount"] != 760.0 {
		t.Fatalf("deposit: %v", d)
	}
	if again := e.expect(200, "POST", path("/bookings/%d/deposit", b), l.tenant, obj{"tenant_phone": "08091112222"}).(obj); id(again) != dep {
		t.Fatal("a second tap should pick up the same deposit")
	}
	e.expect(404, "GET", path("/deposits/%d", dep), e.signupToken("nosy@gmail.com"), nil)

	l.webhook(l.ref(dep), 100) // wrong amount
	if l.get(l.tenant, dep)["status"] != "awaiting_payment" {
		t.Fatal("wrong amount must not count")
	}
	l.webhook(l.ref(dep), 80_000)
	l.webhook(l.ref(dep), 80_000)
	if got := l.get(l.tenant, dep); got["status"] != "paid" || !l.bookingPaid(b) {
		t.Fatalf("after payment: %v", got)
	}
	if got := l.get(l.landlord, dep); got["tenant_phone"] != "08091112222" || got["role"] != "landlord" {
		t.Fatalf("the landlord sees the phone once it is held: %v", got)
	}
	e.expect(409, "PATCH", path("/bookings/%d/cancel", b), l.tenant, nil) // can't cancel the booking with money held
	e.expect(403, "POST", path("/deposits/%d/confirm", dep), l.landlord, nil)
	if got := e.expect(200, "POST", path("/deposits/%d/confirm", dep), l.tenant, nil).(obj); got["status"] != "completed" || got["payout_status"] != "queued" {
		t.Fatalf("confirm: %v", got)
	}
	e.expect(409, "POST", path("/deposits/%d/confirm", dep), l.tenant, nil)
	if q := list(e.expect(200, "GET", "/admin/deposit-payouts", l.admin, nil)); len(q) != 1 {
		t.Fatalf("one queued payout expected: %v", q)
	}
	e.expect(403, "GET", "/admin/deposit-payouts", l.tenant, nil)
	e.expect(200, "POST", path("/admin/deposits/%d/payout-paid", dep), l.admin, obj{"reference": "TRF-1"})
	e.expect(409, "POST", path("/admin/deposits/%d/payout-paid", dep), l.admin, obj{"reference": "TRF-1"})
}

func TestDepositDisputeAndCancellations(t *testing.T) {
	l := newLease(t)
	e := l.e
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)

	// A problem: money stays held until an admin decides; a refund also clears the booking's tick.
	b, dep := l.paidDeposit(30)
	e.expect(422, "POST", path("/deposits/%d/dispute", dep), l.tenant, obj{"reason": "no"})
	e.expect(200, "POST", path("/deposits/%d/dispute", dep), l.tenant, obj{"reason": "The room is not what the photos showed"})
	e.expect(409, "POST", path("/deposits/%d/confirm", dep), l.tenant, nil)
	e.expect(422, "POST", path("/admin/deposits/%d/resolve", dep), l.admin, obj{"decision": "refund"}) // a note is required
	if got := e.expect(200, "POST", path("/admin/deposits/%d/resolve", dep), l.admin, obj{"decision": "refund", "note": "Photos did not match"}).(obj); got["status"] != "refunded" {
		t.Fatalf("resolve: %v", got)
	}
	if len(e.pay.refunded) != 1 || l.bookingPaid(b) {
		t.Fatal("the money goes back and the booking is no longer paid")
	}

	// The landlord backs out: refunded in full.
	_, dep2 := l.paidDeposit(30)
	e.expect(200, "POST", path("/deposits/%d/cancel", dep2), l.landlord, nil)
	if l.get(l.tenant, dep2)["status"] != "refunded" || len(e.pay.refunded) != 2 {
		t.Fatal("landlord cancel should refund")
	}

	// The student cancels: fine with weeks to go, not when move-in is days away.
	_, dep3 := l.paidDeposit(30)
	e.expect(200, "POST", path("/deposits/%d/cancel", dep3), l.tenant, nil)
	if l.get(l.tenant, dep3)["status"] != "refunded" {
		t.Fatal("early cancel should refund")
	}
	_, dep4 := l.paidDeposit(2)
	e.expect(409, "POST", path("/deposits/%d/cancel", dep4), l.tenant, nil)

	// A refund Paystack refuses leaves the deposit as it was.
	e.pay.failRefund = true
	e.expect(502, "POST", path("/deposits/%d/cancel", dep4), l.landlord, nil)
	if l.get(l.tenant, dep4)["status"] != "paid" {
		t.Fatal("a failed refund must put the deposit back")
	}
}

func TestDepositTimers(t *testing.T) {
	l := newLease(t)
	e := l.e
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	now := time.Now().UTC()

	// Released 3 days after the move-in date, not before.
	_, dep := l.paidDeposit(10)
	e.api.SweepOrders(context.Background(), now.Add(11*24*time.Hour))
	if l.get(l.tenant, dep)["status"] != "paid" {
		t.Fatal("only 1 day after move-in: too early")
	}
	e.api.SweepOrders(context.Background(), now.Add(14*24*time.Hour))
	if got := l.get(l.tenant, dep); got["status"] != "completed" || got["payout_status"] != "queued" {
		t.Fatalf("auto release: %v", got)
	}

	// A disputed deposit never auto-releases.
	_, dep2 := l.paidDeposit(1)
	e.expect(200, "POST", path("/deposits/%d/dispute", dep2), l.tenant, obj{"reason": "Landlord gave the room away"})
	e.api.SweepOrders(context.Background(), now.Add(30*24*time.Hour))
	if l.get(l.tenant, dep2)["status"] != "disputed" {
		t.Fatal("disputes wait for an admin")
	}

	// An unfinished payment expires; paying afterwards is refunded at once.
	b := l.book(20)
	l.approve(b)
	dep3 := id(l.deposit(b))
	e.api.SweepOrders(context.Background(), now.Add(31*24*time.Hour))
	if l.get(l.tenant, dep3)["status"] != "cancelled" {
		t.Fatal("unfinished payment should expire")
	}
	l.webhook(l.ref(dep3), 80_000)
	if l.get(l.tenant, dep3)["status"] != "refunded" {
		t.Fatal("a late payment must be refunded")
	}
}

func (e *env) signupToken(email string) string { tok, _ := e.signup(email); return tok }

func TestLandlordDashboardShowsHomesAndMoney(t *testing.T) {
	l := newLease(t)
	e := l.e
	e.expect(401, "GET", "/owner/dashboard", "", nil)

	// Nothing yet: the tenant has no homes, and the landlord has one with no money.
	if d := e.expect(200, "GET", "/owner/dashboard", l.tenant, nil).(obj); len(d["homes"].([]any)) != 0 {
		t.Fatalf("a tenant has no homes: %v", d)
	}
	d := e.expect(200, "GET", "/owner/dashboard", l.landlord, nil).(obj)
	if len(d["homes"].([]any)) != 1 || d["payout_account_set"] != false {
		t.Fatalf("dashboard: %v", d)
	}

	// A pending request, then an approved one with a held deposit.
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	b := l.book(30)
	d = e.expect(200, "GET", "/owner/dashboard", l.landlord, nil).(obj)
	if d["attention"].(obj)["pending_requests"] != 1.0 || d["payout_account_set"] != true {
		t.Fatalf("pending request: %v", d)
	}
	l.approve(b)
	dep := id(l.deposit(b))
	l.webhook(l.ref(dep), 80_000)
	d = e.expect(200, "GET", "/owner/dashboard", l.landlord, nil).(obj)
	home := d["homes"].([]any)[0].(obj)
	if d["money"].(obj)["held"] != 760.0 || home["held_deposits"] != 1.0 || home["approved_bookings"] != 1.0 {
		t.Fatalf("held: %v", d)
	}

	// Released, then paid out: the money moves from "held" to "to receive" to "received".
	e.expect(200, "POST", path("/deposits/%d/confirm", dep), l.tenant, nil)
	d = e.expect(200, "GET", "/owner/dashboard", l.landlord, nil).(obj)
	if m := d["money"].(obj); m["held"] != 0.0 || m["to_receive"] != 760.0 || m["received"] != 0.0 {
		t.Fatalf("to receive: %v", m)
	}
	e.expect(200, "POST", path("/admin/deposits/%d/payout-paid", dep), l.admin, obj{"reference": "TRF-9"})
	d = e.expect(200, "GET", "/owner/dashboard", l.landlord, nil).(obj)
	if m := d["money"].(obj); m["to_receive"] != 0.0 || m["received"] != 760.0 {
		t.Fatalf("received: %v", m)
	}
	// The student's dashboard never shows the landlord's money.
	if m := e.expect(200, "GET", "/owner/dashboard", l.tenant, nil).(obj)["money"].(obj); m["received"] != 0.0 {
		t.Fatalf("not the tenant's money: %v", m)
	}
}
