package api_test

import (
	"testing"
	"time"
)

// A person who has used most of the app can still delete their account (Postgres enforces foreign keys,
// so every table that points at the user has to be cleaned up).
func TestDeleteAccountOfAHeavyUser(t *testing.T) {
	l := newLease(t)
	e := l.e
	now := time.Now().UTC()
	uid, listing := l.landlordID, l.listingID

	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	e.expect(201, "POST", "/market", l.landlord, item("Fan"))
	e.exec(`INSERT INTO reviews (listing_id, user_id, rating, body, created_at, updated_at) VALUES (?,?,5,'nice',?,?)`, listing, uid, now, now)
	e.exec(`INSERT INTO verifiers (user_id, kind, campus, verified_count, created_at) VALUES (?, 'student', 'UNIBEN', 1, ?)`, uid, now)
	e.exec(`INSERT INTO verifier_credits (listing_id, user_id, created_at) VALUES (?,?,?)`, listing, uid, now)
	e.exec(`INSERT INTO referral_codes (code, owner_id, label, campus_slug, active, created_at) VALUES ('ADA1', ?, '', '', ?, ?)`, uid, true, now)
	e.exec(`INSERT INTO referrals (user_id, code, created_at) VALUES (?, 'ADA1', ?)`, uid, now)
	e.exec(`INSERT INTO scam_alerts (user_id, kind, details, created_at) VALUES (?, 'agent', 'took my money', ?)`, uid, now)
	e.exec(`INSERT INTO vendors (owner_id, name, category, latitude, longitude, created_at) VALUES (?, 'Laundry', 'laundry', 6.3, 5.6, ?)`, uid, now)
	e.exec(`INSERT INTO student_verifications (user_id, status, school, submitted_at) VALUES (?, 'pending', 'UNIBEN', ?)`, uid, now)
	e.exec(`INSERT INTO landlord_verifications (user_id, status, phone, id_type, submitted_at) VALUES (?, 'pending', '08031234567', 'nin', ?)`, uid, now)
	e.exec(`INSERT INTO listing_boosts (listing_id, boosted_until, created_at) VALUES (?,?,?)`, listing, now, now)

	e.expect(204, "DELETE", "/users/me", l.landlord, obj{"password": "pw123456"})
	if n := e.count(`SELECT COUNT(*) FROM users WHERE id = ?`, uid); n != 0 {
		t.Fatal("the account should be gone")
	}
	// Their home is taken down rather than left pointing at nobody.
	if e.count(`SELECT COUNT(*) FROM listings WHERE id = ? AND status = 'removed' AND owner_id IS NULL`, listing) != 1 {
		t.Fatal("the home should be taken down")
	}
	for _, q := range []string{`reviews WHERE user_id`, `verifiers WHERE user_id`, `scam_alerts WHERE user_id`, `payout_accounts WHERE user_id`, `market_items WHERE user_id`, `student_verifications WHERE user_id`} {
		if n := e.count(`SELECT COUNT(*) FROM `+q+` = ?`, uid); n != 0 {
			t.Fatalf("%s still has rows", q)
		}
	}
}

// Money records must survive (they are Cribby's accounts), so a person with finished payments is anonymised
// instead of erased, and nobody can leave while money is still being held for them.
func TestDeleteAccountWithPayments(t *testing.T) {
	l := newLease(t)
	e := l.e
	e.expect(200, "PUT", "/payout-account", l.landlord, bank)
	b, dep := l.paidDeposit(30)

	e.expect(409, "DELETE", "/users/me", l.tenant, obj{"password": "pw123456"})   // their deposit is held
	e.expect(409, "DELETE", "/users/me", l.landlord, obj{"password": "pw123456"}) // so is the landlord's money

	e.expect(200, "POST", path("/deposits/%d/confirm", dep), l.tenant, nil)
	e.expect(409, "DELETE", "/users/me", l.landlord, obj{"password": "pw123456"}) // payout still to be sent
	e.expect(200, "POST", path("/admin/deposits/%d/payout-paid", dep), l.admin, obj{"reference": "TRF-1"})

	e.expect(204, "DELETE", "/users/me", l.tenant, obj{"password": "pw123456"})
	if e.count(`SELECT COUNT(*) FROM deposits WHERE id = ?`, dep) != 1 {
		t.Fatal("the payment record must be kept")
	}
	if e.count(`SELECT COUNT(*) FROM users WHERE id = ? AND email = 'tenant@gmail.com'`, l.tenantID) != 0 {
		t.Fatal("the tenant's email must be wiped")
	}
	if e.count(`SELECT COUNT(*) FROM users WHERE id = ? AND name = 'Deleted user'`, l.tenantID) != 1 {
		t.Fatal("the account should stay only as an anonymous row")
	}
	e.expect(401, "POST", "/auth/login", "", obj{"email": "tenant@gmail.com", "password": "pw123456"})
	_ = b
}
