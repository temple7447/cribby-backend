package api_test

import (
	"testing"
	"time"
)

func TestAdminDeleteUser(t *testing.T) {
	l := newLease(t)
	e := l.e
	other, ou := e.signup("someone@gmail.com")
	oid := id(ou)
	e.expect(403, "DELETE", path("/admin/users/%d", oid), l.tenant, nil) // not an admin
	e.expect(400, "DELETE", path("/admin/users/%d", id(e.expect(200, "GET", "/users/me", l.admin, nil).(obj))), l.admin, nil)
	e.expect(404, "DELETE", "/admin/users/99999", l.admin, nil)
	e.expect(204, "DELETE", path("/admin/users/%d", oid), l.admin, nil)
	if e.count(`SELECT COUNT(*) FROM users WHERE id = ?`, oid) != 0 {
		t.Fatal("the account should be gone")
	}
	e.expect(401, "POST", "/auth/login", "", obj{"email": "someone@gmail.com", "password": "pw123456"})
	_ = other

	// Another admin must lose their rights first.
	_, au2 := e.signup("second@gmail.com")
	e.makeAdmin(id(au2))
	e.expect(409, "DELETE", path("/admin/users/%d", id(au2)), l.admin, nil)

	// Money being held blocks it.
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	l.paidDeposit(30)
	e.expect(409, "DELETE", path("/admin/users/%d", l.tenantID), l.admin, nil)
}

func TestAdminEraseListing(t *testing.T) {
	l := newLease(t)
	e := l.e
	now := time.Now().UTC()
	listing := l.listingID
	e.expect(403, "DELETE", path("/admin/listings/%d", listing), l.landlord, nil)

	b := l.book(30)
	e.exec(`INSERT INTO reviews (listing_id, user_id, rating, body, created_at, updated_at) VALUES (?,?,5,'nice',?,?)`, listing, l.tenantID, now, now)
	e.exec(`INSERT INTO listing_boosts (listing_id, boosted_until, created_at) VALUES (?,?,?)`, listing, now, now)
	e.exec(`INSERT INTO listing_views (listing_id, viewer, day) VALUES (?, 'abc', '2026-11-01')`, listing)
	e.expect(204, "PUT", path("/favorites/%d", listing), l.tenant, nil)

	e.expect(204, "DELETE", path("/admin/listings/%d", listing), l.admin, nil)
	for _, q := range []string{`listings WHERE id`, `bookings WHERE listing_id`, `reviews WHERE listing_id`, `listing_boosts WHERE listing_id`, `listing_views WHERE listing_id`, `favorites WHERE listing_id`} {
		if n := e.count(`SELECT COUNT(*) FROM `+q+` = ?`, listing); n != 0 {
			t.Fatalf("%s still has rows", q)
		}
	}
	e.expect(404, "DELETE", path("/admin/listings/%d", listing), l.admin, nil)
	_ = b
}

func TestAdminEraseListingWithDepositIsRefused(t *testing.T) {
	l := newLease(t)
	e := l.e
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	b, _ := l.paidDeposit(30)
	e.expect(409, "DELETE", path("/admin/listings/%d", l.listingID), l.admin, nil)
	e.expect(409, "DELETE", path("/admin/bookings/%d", b), l.admin, nil)
	if e.count(`SELECT COUNT(*) FROM listings WHERE id = ?`, l.listingID) != 1 {
		t.Fatal("the home must still be there")
	}
}

func TestAdminDeleteSmallThings(t *testing.T) {
	l := newLease(t)
	e := l.e
	now := time.Now().UTC()
	b := l.book(30)
	e.expect(403, "DELETE", path("/admin/bookings/%d", b), l.tenant, nil)
	e.expect(204, "DELETE", path("/admin/bookings/%d", b), l.admin, nil)
	e.expect(404, "DELETE", path("/admin/bookings/%d", b), l.admin, nil)

	e.exec(`INSERT INTO vendors (owner_id, name, category, latitude, longitude, created_at) VALUES (?, 'Laundry', 'laundry', 6.3, 5.6, ?)`, l.landlordID, now)
	e.exec(`INSERT INTO scam_alerts (user_id, kind, details, created_at) VALUES (?, 'agent', 'took my money', ?)`, l.tenantID, now)
	e.exec(`INSERT INTO referral_codes (code, owner_id, label, campus_slug, active, created_at) VALUES ('ADA1', ?, '', '', ?, ?)`, l.landlordID, true, now)
	e.exec(`INSERT INTO client_errors (fingerprint, source, message, first_seen_at, last_seen_at) VALUES ('f', 'app', 'boom', ?, ?)`, now, now)

	var vid, sid, eid int
	for _, p := range []struct {
		q string
		d *int
	}{{`SELECT id FROM vendors`, &vid}, {`SELECT id FROM scam_alerts`, &sid}, {`SELECT id FROM client_errors`, &eid}} {
		if err := e.db.QueryRow(t.Context(), p.q).Scan(p.d); err != nil {
			t.Fatal(err)
		}
	}
	e.expect(204, "DELETE", path("/admin/vendors/%d", vid), l.admin, nil)
	e.expect(204, "DELETE", path("/admin/scam-alerts/%d", sid), l.admin, nil)
	e.expect(204, "DELETE", path("/admin/client-errors/%d", eid), l.admin, nil)
	e.expect(204, "DELETE", "/admin/referrals/ADA1", l.admin, nil)
	e.expect(404, "DELETE", "/admin/referrals/ADA1", l.admin, nil)
	for _, q := range []string{"vendors", "scam_alerts", "client_errors", "referral_codes"} {
		if n := e.count(`SELECT COUNT(*) FROM ` + q); n != 0 {
			t.Fatalf("%s still has rows", q)
		}
	}
}
