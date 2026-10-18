package api_test

import (
	"reflect"
	"testing"
)

func (e *env) createBooking(token string, listingID int) obj {
	e.t.Helper()
	return e.expect(201, "POST", "/bookings", token,
		obj{"listing_id": listingID, "move_in_date": "2026-09-01", "lease_months": 12}).(obj)
}

// bookingFixture is a student, an admin and a listing.
func bookingFixture(t *testing.T) (e *env, student, admin string, listingID int) {
	e = newEnv(t)
	student, _ = e.signup("student@stateuniversity.edu")
	var adminUser obj
	admin, adminUser = e.signup("admin@stateuniversity.edu")
	e.makeAdmin(id(adminUser))
	return e, student, admin, e.createListing()
}

func decide(e *env, admin string, bookingID int, status string) (int, any) {
	return e.do("PATCH", path("/bookings/%d", bookingID), admin, obj{"status": status})
}

func TestBookingStartsPendingNotAutoApproved(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	if b["status"] != "pending" || b["landlord_approved"] != false || b["request_sent"] != true {
		t.Fatalf("got %v", b)
	}
	if b["move_in_date"] != "2026-09-01" || b["guarantor_status"] != "none" {
		t.Fatalf("got %v", b)
	}
	if b["listing"].(obj)["title"] != "Cozy studio" {
		t.Fatalf("listing not embedded: %v", b["listing"])
	}
}

func TestBookingWithGuarantorEmailIsInvited(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	b := e.expect(201, "POST", "/bookings", student, obj{
		"listing_id": listing, "move_in_date": "2026-09-01", "guarantor_email": "Mom@Example.COM", "guarantor_name": "Mom",
	}).(obj)
	if b["guarantor_status"] != "invited" || b["guarantor_email"] != "Mom@example.com" || b["lease_months"].(float64) != 12 {
		t.Fatalf("got %v", b)
	}
	e.expect(422, "POST", "/bookings", student, obj{"listing_id": listing, "move_in_date": "2026-09-01", "guarantor_email": "nope"})
	bad := e.expect(422, "POST", "/bookings", student, obj{"listing_id": listing, "move_in_date": "next tuesday"}).(obj)
	loc := list(list(bad["detail"])[0].(obj)["loc"])
	if len(loc) != 2 || loc[0] != "body" || loc[1] != "move_in_date" {
		t.Fatalf("422 should name the field, got loc=%v", loc)
	}
}

func TestBookingUnknownListingIs404(t *testing.T) {
	e, student, _, _ := bookingFixture(t)
	e.expect(404, "POST", "/bookings", student, obj{"listing_id": 99999, "move_in_date": "2026-09-01"})
}

func TestPendingListRequiresAdmin(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	e.createBooking(student, listing)
	e.expect(403, "GET", "/bookings/pending", student, nil)
}

func TestAdminSeesPendingBookingWithStudentInfo(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	pending := list(e.expect(200, "GET", "/bookings/pending", admin, nil))
	if len(pending) != 1 || id(pending[0].(obj)) != id(b) {
		t.Fatalf("got %v", pending)
	}
	user := pending[0].(obj)["user"].(obj)
	if user["email"] != "student@stateuniversity.edu" {
		t.Fatalf("user %v", user)
	}
	if _, leaked := user["hashed_password"]; leaked {
		t.Fatal("hashed_password leaked in admin view")
	}
}

func TestAdminCanApproveBooking(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	code, out := decide(e, admin, id(b), "approved")
	if code != 200 || out.(obj)["status"] != "approved" || out.(obj)["landlord_approved"] != true {
		t.Fatalf("%d %v", code, out)
	}
	mine := e.expect(200, "GET", path("/bookings/%d", id(b)), student, nil).(obj)
	if mine["status"] != "approved" {
		t.Fatalf("student sees %v", mine["status"])
	}
	if got := list(e.expect(200, "GET", "/bookings/pending", admin, nil)); len(got) != 0 {
		t.Fatalf("still pending: %v", got)
	}
}

func TestAdminCanRejectBooking(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	code, out := decide(e, admin, id(b), "rejected")
	if code != 200 || out.(obj)["status"] != "rejected" || out.(obj)["landlord_approved"] != false {
		t.Fatalf("%d %v", code, out)
	}
}

func TestDecideRequiresAdmin(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	if code, _ := decide(e, student, id(b), "approved"); code != 403 {
		t.Fatalf("got %d", code)
	}
}

func TestCannotDecideAnAlreadyDecidedBooking(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	decide(e, admin, id(b), "approved")
	if code, _ := decide(e, admin, id(b), "rejected"); code != 400 {
		t.Fatalf("got %d", code)
	}
}

func TestDecideRejectsInvalidStatusValue(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	if code, _ := decide(e, admin, id(b), "not-a-real-status"); code != 422 {
		t.Fatalf("got %d", code)
	}
}

func TestDecideUnknownBookingIs404(t *testing.T) {
	e, _, admin, _ := bookingFixture(t)
	if code, _ := decide(e, admin, 99999, "approved"); code != 404 {
		t.Fatalf("got %d", code)
	}
}

func TestStudentCanCancelPendingBooking(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	out := e.expect(200, "PATCH", path("/bookings/%d/cancel", id(b)), student, nil).(obj)
	if out["status"] != "canceled" {
		t.Fatalf("got %v", out)
	}
	e.expect(400, "PATCH", path("/bookings/%d/cancel", id(b)), student, nil) // already canceled
}

func TestStudentCanCancelApprovedBooking(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	decide(e, admin, id(b), "approved")
	out := e.expect(200, "PATCH", path("/bookings/%d/cancel", id(b)), student, nil).(obj)
	if out["status"] != "canceled" {
		t.Fatalf("got %v", out)
	}
}

func TestCannotCancelARejectedBooking(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	decide(e, admin, id(b), "rejected")
	e.expect(400, "PATCH", path("/bookings/%d/cancel", id(b)), student, nil)
}

func TestCannotCancelSomeoneElsesBooking(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	other, _ := e.signup("other@stateuniversity.edu")
	b := e.createBooking(student, listing)
	e.expect(404, "PATCH", path("/bookings/%d/cancel", id(b)), other, nil)
	e.expect(404, "GET", path("/bookings/%d", id(b)), other, nil)
}

func ids(v any) []int {
	out := []int{}
	for _, x := range list(v) {
		out = append(out, id(x.(obj)))
	}
	return out
}

func TestStatusFilterOnListBookings(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b1 := e.createBooking(student, listing)
	b2 := e.createBooking(student, listing)
	decide(e, admin, id(b1), "approved")

	if got := ids(e.expect(200, "GET", "/bookings?status_filter=approved", student, nil)); !reflect.DeepEqual(got, []int{id(b1)}) {
		t.Fatalf("approved: %v", got)
	}
	if got := ids(e.expect(200, "GET", "/bookings?status_filter=pending", student, nil)); !reflect.DeepEqual(got, []int{id(b2)}) {
		t.Fatalf("pending: %v", got)
	}
	// newest first
	if got := ids(e.expect(200, "GET", "/bookings", student, nil)); !reflect.DeepEqual(got, []int{id(b2), id(b1)}) {
		t.Fatalf("all: %v", got)
	}
}

func TestAdminNotifiedByEmailOnNewBooking(t *testing.T) {
	e, student, _, listing := bookingFixture(t)
	e.createBooking(student, listing)
	if !reflect.DeepEqual(e.mail.bookingRequests, []string{"admin@stateuniversity.edu"}) {
		t.Fatalf("got %v", e.mail.bookingRequests)
	}
}

func TestStudentNotifiedByEmailOnDecision(t *testing.T) {
	e, student, admin, listing := bookingFixture(t)
	b := e.createBooking(student, listing)
	decide(e, admin, id(b), "approved")
	want := [][2]string{{"student@stateuniversity.edu", "approved"}}
	if !reflect.DeepEqual(e.mail.decisions, want) {
		t.Fatalf("got %v", e.mail.decisions)
	}
}
