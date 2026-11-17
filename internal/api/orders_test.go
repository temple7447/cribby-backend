package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type shop struct {
	e                           *env
	seller, buyer, other, admin string
	sellerID, buyerID           int
	item                        int
}

var bank = obj{"bank_name": "GTBank", "account_number": "0123456789", "account_name": "Ada Obi"}

func newShop(t *testing.T) *shop {
	e := newEnv(t)
	sh := &shop{e: e}
	var su, bu, ou, au obj
	sh.seller, su = e.signup("seller@gmail.com")
	sh.buyer, bu = e.signup("buyer@gmail.com")
	sh.other, ou = e.signup("other@gmail.com")
	sh.admin, au = e.signup("boss@gmail.com")
	for _, u := range []obj{su, bu, ou} {
		e.verifyUser(id(u))
	}
	e.makeAdmin(id(au))
	sh.sellerID, sh.buyerID = id(su), id(bu)
	e.expect(200, "PUT", "/payout-account", sh.seller, bank)
	sh.item = id(e.expect(201, "POST", "/market", sh.seller, item("Mattress")).(obj))
	return sh
}

func (s *shop) order() int {
	o := s.e.expect(201, "POST", path("/market/%d/orders", s.item), s.buyer, obj{"buyer_phone": "0809 111 2222"}).(obj)
	return id(o)
}

// webhook sends a Paystack-style charge.success for an order.
func (s *shop) webhook(ref string, kobo int, sig string) int {
	body, _ := json.Marshal(obj{"event": "charge.success", "data": obj{"reference": ref, "amount": kobo, "status": "success"}})
	req, _ := http.NewRequest(http.MethodPost, s.e.srv.URL+"/api/v1/webhooks/paystack", bytes.NewReader(body))
	req.Header.Set("X-Paystack-Signature", sig)
	code, _ := s.e.send(req, "")
	return code
}

func (s *shop) ref(orderID int) string {
	s.e.t.Helper()
	var ref string
	if err := s.e.db.QueryRow(context.Background(), `SELECT reference FROM orders WHERE id = ?`, orderID).Scan(&ref); err != nil {
		s.e.t.Fatal(err)
	}
	return ref
}

func (s *shop) get(token string, orderID int) obj {
	return s.e.expect(200, "GET", path("/orders/%d", orderID), token, nil).(obj)
}

func (s *shop) paid() int {
	oid := s.order()
	if code := s.webhook(s.ref(oid), 1_500_000, "valid-signature"); code != 200 {
		s.e.t.Fatalf("webhook: %d", code)
	}
	return oid
}

func (s *shop) itemStatus() string {
	s.e.t.Helper()
	var st string
	if err := s.e.db.QueryRow(context.Background(), `SELECT status FROM market_items WHERE id = ?`, s.item).Scan(&st); err != nil {
		s.e.t.Fatal(err)
	}
	return st
}

func TestOrderHappyPathHoldsThenReleasesTheMoney(t *testing.T) {
	s := newShop(t)
	e := s.e

	// Nobody can pay until payments are on, the buyer's email is confirmed and the seller can be paid.
	e.pay.on = false
	e.expect(503, "POST", path("/market/%d/orders", s.item), s.buyer, obj{"buyer_phone": "08091112222"})
	e.pay.on = true
	stranger, _ := e.signup("unconfirmed@gmail.com")
	e.expect(403, "POST", path("/market/%d/orders", s.item), stranger, obj{"buyer_phone": "08091112222"})
	e.expect(409, "POST", path("/market/%d/orders", s.item), s.seller, obj{"buyer_phone": "08091112222"}) // own item
	e.expect(422, "POST", path("/market/%d/orders", s.item), s.buyer, obj{"buyer_phone": "abc"})
	noBank, _ := e.signup("nobank@gmail.com")
	noBankItem := id(e.expect(201, "POST", "/market", noBank, item("Fan")).(obj))
	e.expect(409, "POST", path("/market/%d/orders", noBankItem), s.buyer, obj{"buyer_phone": "08091112222"})
	if got := e.expect(200, "GET", path("/market/%d", noBankItem), s.buyer, nil).(obj); got["can_buy"] != false {
		t.Fatalf("no payout account, so no buy button: %v", got)
	}
	if got := e.expect(200, "GET", path("/market/%d", s.item), s.buyer, nil).(obj); got["can_buy"] != true {
		t.Fatalf("can_buy: %v", got)
	}
	if got := e.expect(200, "GET", path("/market/%d", s.item), s.seller, nil).(obj); got["can_buy"] != false {
		t.Fatalf("a seller can't buy their own item: %v", got)
	}

	// Start an order: 5% fee comes out of the seller's side, the buyer pays the listed price.
	oid := s.order()
	o := s.get(s.buyer, oid)
	if o["status"] != "awaiting_payment" || o["amount"] != 15000.0 || o["fee"] != 750.0 || o["seller_amount"] != 14250.0 ||
		!strings.HasPrefix(o["payment_url"].(string), "https://checkout.paystack.test/") {
		t.Fatalf("order: %v", o)
	}
	if got := e.pay.initiated[0].AmountKobo; got != 1_500_000 {
		t.Fatalf("Paystack was asked for %d kobo", got)
	}
	e.expect(409, "POST", path("/market/%d/orders", s.item), s.other, obj{"buyer_phone": "08091112222"}) // someone is mid-payment
	if again := e.expect(200, "POST", path("/market/%d/orders", s.item), s.buyer, obj{"buyer_phone": "08091112222"}).(obj); id(again) != oid {
		t.Fatalf("a second tap should pick up the same order: %v", again)
	}
	e.expect(404, "GET", path("/orders/%d", oid), s.other, nil) // not theirs

	// The webhook must be signed and must match the amount.
	ref := s.ref(oid)
	if s.webhook(ref, 1_500_000, "forged") != 401 {
		t.Fatal("an unsigned webhook must be refused")
	}
	s.webhook(ref, 100, "valid-signature") // wrong amount: ignored
	if s.get(s.buyer, oid)["status"] != "awaiting_payment" {
		t.Fatal("a payment for the wrong amount must not count")
	}
	s.webhook(ref, 1_500_000, "valid-signature")
	s.webhook(ref, 1_500_000, "valid-signature") // Paystack retries: harmless
	o = s.get(s.buyer, oid)
	if o["status"] != "paid" || s.itemStatus() != "reserved" || o["payment_url"] != nil && o["payment_url"] != "" {
		t.Fatalf("after payment: %v item=%s", o, s.itemStatus())
	}
	if strings.Contains(toJSON(e.expect(200, "GET", "/market", "", nil)), "Mattress") {
		t.Fatal("a reserved item must leave the marketplace")
	}
	e.expect(409, "PATCH", path("/market/%d", s.item), s.seller, item("Mattress")) // can't edit mid-order
	e.expect(409, "DELETE", path("/market/%d", s.item), s.seller, nil)

	// Each side sees the other's phone once the money is held, and nothing they shouldn't.
	if sv := s.get(s.seller, oid); sv["buyer_phone"] != "08091112222" || sv["seller_phone"] != nil && sv["seller_phone"] != "" {
		t.Fatalf("seller view: %v", sv)
	}
	if bv := s.get(s.buyer, oid); bv["seller_phone"] != "08031234567" {
		t.Fatalf("buyer view: %v", bv)
	}
	if len(e.mail.orderUpdates) < 2 {
		t.Fatalf("both sides should be emailed: %v", e.mail.orderUpdates)
	}

	// The seller hands it over; only the seller can say so.
	e.expect(403, "POST", path("/orders/%d/handed-over", oid), s.buyer, nil)
	if s.get(s.seller, oid)["status"] != "paid" {
		t.Fatal("still paid")
	}
	e.expect(200, "POST", path("/orders/%d/handed-over", oid), s.seller, nil)
	e.expect(409, "POST", path("/orders/%d/handed-over", oid), s.seller, nil)
	if o := s.get(s.buyer, oid); o["status"] != "handed_over" || o["auto_release_at"] == nil {
		t.Fatalf("handed over: %v", o)
	}

	// Only the buyer can release the money, and only once.
	e.expect(403, "POST", path("/orders/%d/confirm", oid), s.seller, nil)
	done := e.expect(200, "POST", path("/orders/%d/confirm", oid), s.buyer, nil).(obj)
	if done["status"] != "completed" || done["payout_status"] != "queued" || s.itemStatus() != "sold" {
		t.Fatalf("completed: %v item=%s", done, s.itemStatus())
	}
	e.expect(409, "POST", path("/orders/%d/confirm", oid), s.buyer, nil)
	e.expect(409, "POST", path("/orders/%d/dispute", oid), s.buyer, obj{"reason": "too late now"})
	if len(e.pay.refunded) != 0 {
		t.Fatal("nothing should have been refunded")
	}

	// Cribby's team sends the payout and marks it paid.
	e.expect(403, "GET", "/admin/payouts", s.buyer, nil)
	queue := list(e.expect(200, "GET", "/admin/payouts", s.admin, nil))
	if len(queue) != 1 || queue[0].(obj)["payout_account"].(obj)["account_number"] != "0123456789" || queue[0].(obj)["seller_amount"] != 14250.0 {
		t.Fatalf("payout queue: %v", queue)
	}
	e.expect(200, "POST", path("/admin/orders/%d/payout-paid", oid), s.admin, obj{"reference": "TRF-1"})
	e.expect(409, "POST", path("/admin/orders/%d/payout-paid", oid), s.admin, obj{"reference": "TRF-1"})
	if len(list(e.expect(200, "GET", "/admin/payouts", s.admin, nil))) != 0 || len(list(e.expect(200, "GET", "/admin/payouts?status=paid", s.admin, nil))) != 1 {
		t.Fatal("the payout should move to paid")
	}
	var events int
	events = e.count(`SELECT COUNT(*) FROM order_events WHERE order_id = ?`, oid)
	if events < 5 {
		t.Fatalf("every step should be logged, got %d events", events)
	}
}

func TestOrderDisputeAndAdminDecision(t *testing.T) {
	s := newShop(t)
	e := s.e
	oid := s.paid()
	e.expect(403, "POST", path("/orders/%d/dispute", oid), s.seller, obj{"reason": "I am the seller"})
	e.expect(422, "POST", path("/orders/%d/dispute", oid), s.buyer, obj{"reason": "bad"})
	e.expect(200, "POST", path("/orders/%d/dispute", oid), s.buyer, obj{"reason": "It was not what the photos showed"})
	o := s.get(s.buyer, oid)
	if o["status"] != "disputed" || o["dispute_reason"] != "It was not what the photos showed" {
		t.Fatalf("dispute: %v", o)
	}
	// A disputed order never auto-releases.
	e.exec(`UPDATE orders SET auto_release_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).UTC(), oid)
	e.api.SweepOrders(context.Background(), time.Now().UTC())
	if s.get(s.buyer, oid)["status"] != "disputed" {
		t.Fatal("a dispute must stop the timer")
	}

	e.expect(403, "GET", "/admin/orders", s.buyer, nil)
	queue := list(e.expect(200, "GET", "/admin/orders", s.admin, nil))
	if len(queue) != 1 || queue[0].(obj)["buyer_email"] != "buyer@gmail.com" || queue[0].(obj)["buyer_phone"] != "08091112222" {
		t.Fatalf("admin queue: %v", queue)
	}
	e.expect(422, "POST", path("/admin/orders/%d/resolve", oid), s.admin, obj{"decision": "refund"}) // needs a note
	e.pay.failRefund = true
	e.expect(502, "POST", path("/admin/orders/%d/resolve", oid), s.admin, obj{"decision": "refund", "note": "Seller agreed"})
	if s.get(s.buyer, oid)["status"] != "disputed" {
		t.Fatal("a failed refund must leave the order as it was")
	}
	e.pay.failRefund = false
	e.expect(200, "POST", path("/admin/orders/%d/resolve", oid), s.admin, obj{"decision": "refund", "note": "Seller agreed"})
	if s.get(s.buyer, oid)["status"] != "refunded" || len(e.pay.refunded) != 1 || e.pay.refunded[0] != s.ref(oid) || s.itemStatus() != "active" {
		t.Fatalf("refund: %v item=%s", e.pay.refunded, s.itemStatus())
	}
	e.expect(409, "POST", path("/admin/orders/%d/resolve", oid), s.admin, obj{"decision": "release", "note": "oops"}) // already settled
	if len(list(e.expect(200, "GET", "/market", "", nil))) != 1 {
		t.Fatal("a refunded item goes back on sale")
	}

	// The other way: the admin decides for the seller.
	oid2 := s.paid()
	e.expect(200, "POST", path("/orders/%d/dispute", oid2), s.buyer, obj{"reason": "Changed my mind about it"})
	e.expect(200, "POST", path("/admin/orders/%d/resolve", oid2), s.admin, obj{"decision": "release", "note": "Item was as described"})
	if o := s.get(s.seller, oid2); o["status"] != "completed" || o["payout_status"] != "queued" {
		t.Fatalf("release: %v", o)
	}
}

func TestOrderTimers(t *testing.T) {
	s := newShop(t)
	e := s.e
	now := time.Now().UTC()

	// 3 days after "handed over" with no objection, the money is released.
	oid := s.paid()
	e.expect(200, "POST", path("/orders/%d/handed-over", oid), s.seller, nil)
	e.api.SweepOrders(context.Background(), now)
	if s.get(s.buyer, oid)["status"] != "handed_over" {
		t.Fatal("too early to release")
	}
	e.api.SweepOrders(context.Background(), now.Add(4*24*time.Hour))
	if o := s.get(s.buyer, oid); o["status"] != "completed" || o["payout_status"] != "queued" || s.itemStatus() != "sold" {
		t.Fatalf("auto release: %v", o)
	}
	var auto int
	auto = e.count(`SELECT COUNT(*) FROM order_events WHERE order_id = ? AND event = 'auto_released'`, oid)
	if auto != 1 {
		t.Fatal("the auto release should be logged")
	}
	e.api.SweepOrders(context.Background(), now.Add(10*24*time.Hour)) // running again changes nothing
	if len(e.pay.refunded) != 0 {
		t.Fatal("no refunds expected")
	}

	// A seller who never hands it over for 7 days: the buyer is refunded.
	e.exec(`UPDATE market_items SET status = 'active' WHERE id = ?`, s.item)
	oid2 := s.paid()
	e.api.SweepOrders(context.Background(), now.Add(2*24*time.Hour))
	if s.get(s.buyer, oid2)["status"] != "paid" {
		t.Fatal("still inside the 7 days")
	}
	e.api.SweepOrders(context.Background(), now.Add(8*24*time.Hour))
	if s.get(s.buyer, oid2)["status"] != "refunded" || len(e.pay.refunded) != 1 || s.itemStatus() != "active" {
		t.Fatalf("stale order: %v", s.get(s.buyer, oid2))
	}

	// An unfinished payment expires after an hour; paying after that is refunded on the spot.
	oid3 := s.order()
	e.api.SweepOrders(context.Background(), now.Add(9*24*time.Hour+2*time.Hour))
	if s.get(s.buyer, oid3)["status"] != "cancelled" {
		t.Fatalf("abandoned: %v", s.get(s.buyer, oid3))
	}
	before := len(e.pay.refunded)
	s.webhook(s.ref(oid3), 1_500_000, "valid-signature")
	if s.get(s.buyer, oid3)["status"] != "refunded" || len(e.pay.refunded) != before+1 {
		t.Fatalf("late payment: %v", s.get(s.buyer, oid3))
	}
}

func TestOrderPaymentSeenWithoutWebhookAndSellerCancel(t *testing.T) {
	s := newShop(t)
	e := s.e
	oid := s.order()
	// Back from Paystack before the webhook: the app opens the order and the server asks Paystack.
	if s.get(s.buyer, oid)["status"] != "awaiting_payment" {
		t.Fatal("not paid yet")
	}
	e.pay.pay(s.ref(oid), 1_500_000)
	if s.get(s.buyer, oid)["status"] != "paid" {
		t.Fatal("the server should check with Paystack")
	}
	// A buyer can't just cancel paid money away; the seller can back out and the buyer is refunded.
	e.expect(409, "POST", path("/orders/%d/cancel", oid), s.buyer, nil)
	e.expect(200, "POST", path("/orders/%d/cancel", oid), s.seller, nil)
	if s.get(s.buyer, oid)["status"] != "refunded" || len(e.pay.refunded) != 1 || s.itemStatus() != "active" {
		t.Fatalf("seller cancel: %v", s.get(s.buyer, oid))
	}
	// The buyer can drop an unpaid order.
	oid2 := s.order()
	e.expect(200, "POST", path("/orders/%d/cancel", oid2), s.buyer, nil)
	if s.get(s.buyer, oid2)["status"] != "cancelled" {
		t.Fatal("unpaid cancel")
	}
	// Both can list their orders; a stranger sees none.
	if n := len(list(e.expect(200, "GET", "/orders", s.buyer, nil))); n != 2 {
		t.Fatalf("buyer's orders: %d", n)
	}
	if n := len(list(e.expect(200, "GET", "/orders", s.other, nil))); n != 0 {
		t.Fatalf("stranger's orders: %d", n)
	}
}

func TestPayoutAccountRules(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.signup("seller@gmail.com")
	if got := e.expect(200, "GET", "/payout-account", tok, nil).(obj); got["set"] != false || got["fee_percent"] != 5.0 {
		t.Fatalf("empty: %v", got)
	}
	for name, bad := range map[string]obj{
		"digits": {"bank_name": "GTBank", "account_number": "12345", "account_name": "Ada"},
		"bank":   {"bank_name": "G", "account_number": "0123456789", "account_name": "Ada"},
		"name":   {"bank_name": "GTBank", "account_number": "0123456789", "account_name": ""},
	} {
		if code, _ := e.do("PUT", "/payout-account", tok, bad); code != 422 {
			t.Fatalf("%s: %d", name, code)
		}
	}
	e.expect(200, "PUT", "/payout-account", tok, bank)
	if got := e.expect(200, "GET", "/payout-account", tok, nil).(obj); got["set"] != true || got["account_number"] != "0123456789" {
		t.Fatalf("saved: %v", got)
	}
	e.expect(401, "GET", "/payout-account", "", nil)
}
