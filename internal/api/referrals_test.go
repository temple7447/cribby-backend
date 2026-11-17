package api_test

import "testing"

func signupWithRef(e *env, email, ref string) (string, obj) {
	e.t.Helper()
	body := e.expect(200, "POST", "/auth/signup", "", obj{"email": email, "password": "pw123456", "name": "Test", "ref": ref}).(obj)
	return body["access_token"].(string), body["user"].(obj)
}

func TestReferralCodesCountSignupsRequestsAndRentals(t *testing.T) {
	f := newLandlordFixture(t)
	e := f.e
	leader, leaderUser := e.signup("leader@gmail.com")

	// only admins create codes, for an account that exists
	body := obj{"owner_email": "leader@gmail.com", "label": " UNIBEN 300L group ", "campus_slug": "UNIBEN"}
	e.expect(401, "PUT", "/admin/referrals/uniben-300l", "", body)
	e.expect(403, "PUT", "/admin/referrals/uniben-300l", leader, body)
	e.expect(422, "PUT", "/admin/referrals/x", f.admin, body)
	e.expect(404, "PUT", "/admin/referrals/uniben-300l", f.admin, obj{"owner_email": "nobody@gmail.com"})
	all := list(e.expect(200, "PUT", "/admin/referrals/UNIBEN-300L", f.admin, body))
	if len(all) != 1 || all[0].(obj)["code"] != "uniben-300l" || all[0].(obj)["label"] != "UNIBEN 300L group" || all[0].(obj)["campus_slug"] != "uniben" {
		t.Fatalf("saved: %v", all)
	}

	// people who sign up with it are counted; bad or unknown codes never block a signup
	a, _ := signupWithRef(e, "a@gmail.com", "UNIBEN-300L")
	b, _ := signupWithRef(e, "b@gmail.com", "uniben-300l")
	signupWithRef(e, "c@gmail.com", "no-such-code")
	signupWithRef(e, "d@gmail.com", "!!!")
	mine := list(e.expect(200, "GET", "/referrals/mine", leader, nil))
	if len(mine) != 1 || num(mine[0].(obj), "signups") != 2 || num(mine[0].(obj), "rentals") != 0 {
		t.Fatalf("after signups: %v", mine)
	}
	if got := list(e.expect(200, "GET", "/referrals/mine", f.student, nil)); len(got) != 0 {
		t.Fatalf("someone else's codes must not show: %v", got)
	}

	// a booking request counts as a request; the landlord approving it makes a rental
	ba := e.createBooking(a, f.listing)
	e.createBooking(b, f.listing)
	mine = list(e.expect(200, "GET", "/referrals/mine", leader, nil))
	if num(mine[0].(obj), "requests") != 2 || num(mine[0].(obj), "rentals") != 0 {
		t.Fatalf("after requests: %v", mine)
	}
	e.expect(200, "PATCH", path("/bookings/%d", id(ba)), f.landlord, obj{"status": "approved"})
	mine = list(e.expect(200, "GET", "/referrals/mine", leader, nil))
	if num(mine[0].(obj), "rentals") != 1 {
		t.Fatalf("after approval: %v", mine)
	}
	// a request someone cancels stops counting
	e.expect(200, "PATCH", path("/bookings/%d/cancel", id(e.createBooking(a, f.listing))), a, nil)

	// a switched-off code stops attributing new signups
	off := false
	e.expect(200, "PUT", "/admin/referrals/uniben-300l", f.admin, obj{"owner_email": "leader@gmail.com", "active": off})
	signupWithRef(e, "e@gmail.com", "uniben-300l")
	if g := list(e.expect(200, "GET", "/admin/referrals", f.admin, nil)); num(g[0].(obj), "signups") != 2 {
		t.Fatalf("inactive code took a signup: %v", g)
	}
	_ = leaderUser
}
