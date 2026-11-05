package api_test

import (
	"strings"
	"testing"
)

// landlordFixture is a landlord with an approved, user-posted listing, a student, and an admin.
type landlordFixture struct {
	e                        *env
	landlord, student, admin string
	landlordID, studentID    int
	listing                  int
}

func newLandlordFixture(t *testing.T) landlordFixture {
	t.Helper()
	e := newEnv(t)
	f := landlordFixture{e: e}
	var lu, su, au obj
	f.landlord, lu = e.signup("landlord@gmail.com")
	f.student, su = e.signup("student@stateuniversity.edu")
	f.admin, au = e.signup("boss@gmail.com")
	e.makeAdmin(id(au))
	f.landlordID, f.studentID = id(lu), id(su)
	f.listing = id(e.expect(201, "POST", "/listings", f.landlord, apartment("Landlord flat")).(obj))
	e.expect(200, "PATCH", path("/admin/listings/%d/review", f.listing), f.admin, obj{"decision": "approve"})
	return f
}

func TestBookingRequestReachesTheLandlordWhoCanDecideIt(t *testing.T) {
	f := newLandlordFixture(t)
	e := f.e
	b := e.createBooking(f.student, f.listing)

	// the landlord is emailed — not the admins
	if len(e.mail.ownerRequests) != 1 || e.mail.ownerRequests[0] != "landlord@gmail.com" || len(e.mail.bookingRequests) != 0 {
		t.Fatalf("owner=%v admins=%v", e.mail.ownerRequests, e.mail.bookingRequests)
	}
	if s := e.expect(200, "GET", "/owner/summary", f.landlord, nil).(obj); num(s, "pending_bookings") != 1 {
		t.Fatalf("summary %v", s)
	}

	// the landlord sees it, without the student's email yet
	rows := list(e.expect(200, "GET", "/owner/bookings", f.landlord, nil))
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	row := rows[0].(obj)
	st := row["student"].(obj)
	if row["status"] != "pending" || st["name"] == "" || st["email"] != nil || row["listing_title"] != "Landlord flat" {
		t.Fatalf("row %v", row)
	}
	// nobody else sees it
	if other, _ := e.signup("nosy@gmail.com"); len(list(e.expect(200, "GET", "/owner/bookings", other, nil))) != 0 {
		t.Fatal("another user can see the landlord's bookings")
	}

	// a stranger can't decide it; the landlord can, and the student is told
	stranger, _ := e.signup("stranger@gmail.com")
	if code, _ := decide(e, stranger, id(b), "approved"); code != 403 {
		t.Fatalf("stranger decide: %d", code)
	}
	if code, _ := decide(e, f.landlord, id(b), "approved"); code != 200 {
		t.Fatalf("landlord decide: %d", code)
	}
	if len(e.mail.decisions) != 1 || e.mail.decisions[0] != [2]string{"student@stateuniversity.edu", "approved"} {
		t.Fatalf("decisions %v", e.mail.decisions)
	}
	// the student's contact details are shared only after approval
	row = list(e.expect(200, "GET", "/owner/bookings?status=approved", f.landlord, nil))[0].(obj)
	if row["student"].(obj)["email"] != "student@stateuniversity.edu" {
		t.Fatalf("approved row %v", row)
	}
	if s := e.expect(200, "GET", "/owner/summary", f.landlord, nil).(obj); num(s, "pending_bookings") != 0 {
		t.Fatalf("summary after %v", s)
	}
}

func TestAdminsStillHandleListingsTheTeamAdded(t *testing.T) {
	e, student, admin, listing := bookingFixture(t) // an owner-less, team-made listing
	b := e.createBooking(student, listing)
	if len(e.mail.bookingRequests) != 1 || len(e.mail.ownerRequests) != 0 {
		t.Fatalf("admins=%v owner=%v", e.mail.bookingRequests, e.mail.ownerRequests)
	}
	if code, _ := decide(e, admin, id(b), "rejected"); code != 200 {
		t.Fatalf("admin decide: %d", code)
	}
}

func TestBookingGuards(t *testing.T) {
	f := newLandlordFixture(t)
	e := f.e
	// can't book your own listing
	e.expect(400, "POST", "/bookings", f.landlord, obj{"listing_id": f.listing, "move_in_date": "2026-09-01", "lease_months": 12})
	// can't book something students can't see yet
	pending := id(e.expect(201, "POST", "/listings", f.landlord, apartment("Still pending")).(obj))
	e.expect(404, "POST", "/bookings", f.student, obj{"listing_id": pending, "move_in_date": "2026-09-01", "lease_months": 12})
}

func TestChatReachesTheLandlordAndTheyCanAnswer(t *testing.T) {
	f := newLandlordFixture(t)
	e := f.e
	convo := e.expect(201, "POST", "/conversations", f.student, obj{"kind": "landlord", "listing_id": f.listing}).(obj)
	cid := id(convo)
	e.expect(201, "POST", path("/conversations/%d/messages", cid), f.student, obj{"text": "Is it still available?"})
	e.expect(201, "POST", path("/conversations/%d/messages", cid), f.student, obj{"text": "And can I view it on Saturday?"})

	// one email for the burst, addressed to the landlord, quoting the first message
	if len(e.mail.messages) != 1 {
		t.Fatalf("emails %v", e.mail.messages)
	}
	if m := e.mail.messages[0]; m.To != "landlord@gmail.com" || !m.ToLandlord || m.Listing != "Landlord flat" || !strings.Contains(m.Preview, "still available") {
		t.Fatalf("email %+v", m)
	}

	// it's in the landlord's inbox, unread
	if s := e.expect(200, "GET", "/owner/summary", f.landlord, nil).(obj); num(s, "unread_messages") != 2 {
		t.Fatalf("summary %v", s)
	}
	inbox := list(e.expect(200, "GET", "/owner/conversations", f.landlord, nil))
	if len(inbox) != 1 {
		t.Fatalf("inbox %v", inbox)
	}
	row := inbox[0].(obj)
	if num(row, "unread") != 2 || row["last_message"].(obj)["text"] != "And can I view it on Saturday?" || row["student"].(obj)["email"] != nil {
		t.Fatalf("inbox row %v", row)
	}

	// opening it shows the whole thread and marks it read
	thread := e.expect(200, "GET", path("/owner/conversations/%d", cid), f.landlord, nil).(obj)
	if len(list(thread["messages"])) != 2 || num(thread, "unread") != 2 {
		t.Fatalf("thread %v", thread)
	}
	if s := e.expect(200, "GET", "/owner/summary", f.landlord, nil).(obj); num(s, "unread_messages") != 0 {
		t.Fatalf("still unread: %v", s)
	}

	// the landlord answers; the student sees it in their own chat and is emailed
	e.expect(201, "POST", path("/owner/conversations/%d/messages", cid), f.landlord, obj{"text": "Yes! Saturday 11am works."})
	mine := e.expect(200, "GET", path("/conversations/%d", cid), f.student, nil).(obj)
	msgs := list(mine["messages"])
	if last := msgs[len(msgs)-1].(obj); last["sender"] != "them" || last["text"] != "Yes! Saturday 11am works." {
		t.Fatalf("student's view %v", msgs)
	}
	if len(e.mail.messages) != 2 || e.mail.messages[1].To != "student@stateuniversity.edu" || e.mail.messages[1].ToLandlord {
		t.Fatalf("reply email %v", e.mail.messages)
	}
	// a quick follow-up from the same person doesn't send another email
	e.expect(201, "POST", path("/owner/conversations/%d/messages", cid), f.landlord, obj{"text": "Bring your ID."})
	if len(e.mail.messages) != 2 {
		t.Fatalf("emails %v", e.mail.messages)
	}
	// but the student speaking again after an answer does notify the landlord
	e.expect(201, "POST", path("/conversations/%d/messages", cid), f.student, obj{"text": "Will do, thanks!"})
	if len(e.mail.messages) != 3 || !e.mail.messages[2].ToLandlord {
		t.Fatalf("emails %v", e.mail.messages)
	}
}

func TestChatPrivacyAndRules(t *testing.T) {
	f := newLandlordFixture(t)
	e := f.e
	cid := id(e.expect(201, "POST", "/conversations", f.student, obj{"kind": "landlord", "listing_id": f.listing}).(obj))
	e.expect(201, "POST", path("/conversations/%d/messages", cid), f.student, obj{"text": "Hi"})

	// only the landlord of that listing can open or answer the chat
	other, _ := e.signup("other@gmail.com")
	e.expect(404, "GET", path("/owner/conversations/%d", cid), other, nil)
	e.expect(404, "POST", path("/owner/conversations/%d/messages", cid), other, obj{"text": "hijack"})
	// ...and nobody but the student can read it as theirs
	e.expect(404, "GET", path("/conversations/%d", cid), f.landlord, nil)

	// empty and over-long replies are refused
	e.expect(422, "POST", path("/owner/conversations/%d/messages", cid), f.landlord, obj{"text": "   "})
	e.expect(422, "POST", path("/owner/conversations/%d/messages", cid), f.landlord, obj{"text": strings.Repeat("x", 2001)})

	// you can't start a chat about your own listing
	e.expect(400, "POST", "/conversations", f.landlord, obj{"kind": "landlord", "listing_id": f.listing})

	// a block stops the chat in both directions
	e.expect(200, "POST", "/blocks", f.landlord, obj{"blocked_user_id": f.studentID})
	e.expect(403, "POST", path("/owner/conversations/%d/messages", cid), f.landlord, obj{"text": "hello?"})
	e.expect(403, "POST", path("/conversations/%d/messages", cid), f.student, obj{"text": "still there?"})
}

func TestMessagesToTeamListingsStillWork(t *testing.T) {
	// listings the Cribby team added have no landlord account: the message is just stored
	e := newEnv(t)
	student, _ := e.signup("s@stateuniversity.edu")
	listing := e.createListing()
	cid := id(e.expect(201, "POST", "/conversations", student, obj{"kind": "landlord", "listing_id": listing}).(obj))
	e.expect(201, "POST", path("/conversations/%d/messages", cid), student, obj{"text": "Hello"})
	if len(e.mail.messages) != 0 {
		t.Fatalf("unexpected email %v", e.mail.messages)
	}
}
