package api_test

import (
	"testing"
)

func report(target string, targetID int, reason string) obj {
	return obj{"target_type": target, "target_id": targetID, "reason": reason}
}

func TestCreateReportAgainstListing(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("reporter@stateuniversity.edu")
	listing := e.createListing()
	out := e.expect(201, "POST", "/reports", token, report("listing", listing, "spam")).(obj)
	if out["status"] != "pending" || out["resolved_at"] != nil {
		t.Fatalf("got %v", out)
	}
	// reporting hides the listing
	for _, l := range list(e.expect(200, "GET", "/listings", "", nil)) {
		if id(l.(obj)) == listing {
			t.Fatal("reported listing should be hidden from search")
		}
	}
}

func TestCreateReportValidation(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("reporter2@stateuniversity.edu")
	e.expect(400, "POST", "/reports", token, report("landlord", 1, "spam"))
	e.expect(400, "POST", "/reports", token, report("listing", 1, "boring"))
	e.expect(404, "POST", "/reports", token, report("listing", 999999, "spam"))
	e.expect(400, "POST", "/reports", token, report("user", id(user), "spam"))
	e.expect(404, "POST", "/reports", token, report("user", 999999, "spam"))
}

func liveRoommateIDs(e *env) map[int]bool {
	out := map[int]bool{}
	for _, p := range list(e.expect(200, "GET", "/roommates", "", nil)) {
		out[id(p.(obj))] = true
	}
	return out
}

func roommateWithReporter(t *testing.T, ownerEmail, reporterEmail string) (e *env, ownerToken string, profile int, reporterToken string, reporter obj) {
	e = newEnv(t)
	var owner obj
	ownerToken, owner = e.signup(ownerEmail)
	e.verifyUser(id(owner))
	profile = e.createRoommateProfile(ownerToken)
	reporterToken, reporter = e.signup(reporterEmail)
	return
}

func TestReportHidesRoommateProfileFromList(t *testing.T) {
	e, _, profile, reporter, _ := roommateWithReporter(t, "owner@stateuniversity.edu", "reporter4@stateuniversity.edu")
	if !liveRoommateIDs(e)[profile] {
		t.Fatal("profile should start live")
	}
	e.expect(201, "POST", "/reports", reporter, report("roommate_profile", profile, "fake"))
	if liveRoommateIDs(e)[profile] {
		t.Fatal("reported profile should be hidden")
	}
}

func TestOwnerCannotUnhideUnderReviewProfileViaPatch(t *testing.T) {
	e, owner, profile, reporter, _ := roommateWithReporter(t, "owner2@stateuniversity.edu", "reporter5@stateuniversity.edu")
	e.expect(201, "POST", "/reports", reporter, report("roommate_profile", profile, "fake"))
	e.expect(403, "PATCH", "/roommates/me", owner, obj{"visibility": "live"})
}

func TestDismissReportRestoresLiveVisibility(t *testing.T) {
	e, _, profile, reporter, reporterUser := roommateWithReporter(t, "owner3@stateuniversity.edu", "reporter6@stateuniversity.edu")
	r := e.expect(201, "POST", "/reports", reporter, report("roommate_profile", profile, "fake")).(obj)
	e.makeAdmin(id(reporterUser))
	out := e.expect(200, "PATCH", path("/reports/%d", id(r)), reporter, obj{"status": "dismissed"}).(obj)
	if out["status"] != "dismissed" || out["resolved_at"] == nil || out["resolved_by_id"] == nil {
		t.Fatalf("got %v", out)
	}
	if !liveRoommateIDs(e)[profile] {
		t.Fatal("dismissed report should restore the profile")
	}
}

func TestResolveReportMarksProfileRemoved(t *testing.T) {
	e, _, profile, reporter, reporterUser := roommateWithReporter(t, "owner4@stateuniversity.edu", "reporter7@stateuniversity.edu")
	r := e.expect(201, "POST", "/reports", reporter, report("roommate_profile", profile, "fake")).(obj)
	e.makeAdmin(id(reporterUser))
	e.expect(200, "PATCH", path("/reports/%d", id(r)), reporter, obj{"status": "resolved"})
	if liveRoommateIDs(e)[profile] {
		t.Fatal("resolved report should remove the profile")
	}
	if e.count(`SELECT COUNT(*) FROM roommate_profiles WHERE id = ? AND visibility = 'removed'`, profile) != 1 {
		t.Fatal("visibility should be 'removed'")
	}
}

func TestDismissListingReportUnhidesAndResolveHides(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("mod@stateuniversity.edu")
	e.makeAdmin(id(user))
	listing := e.createListing()
	r := e.expect(201, "POST", "/reports", token, report("listing", listing, "scam")).(obj)
	hidden := func() int {
		return e.count(`SELECT COUNT(*) FROM listings WHERE id = ? AND is_hidden = ?`, listing, true)
	}
	if hidden() != 1 {
		t.Fatal("listing should be hidden after a report")
	}
	e.expect(200, "PATCH", path("/reports/%d", id(r)), token, obj{"status": "dismissed"})
	if hidden() != 0 {
		t.Fatal("dismissing should unhide")
	}
	e.expect(200, "PATCH", path("/reports/%d", id(r)), token, obj{"status": "resolved"})
	if hidden() != 1 {
		t.Fatal("resolving should hide")
	}
	e.expect(400, "PATCH", path("/reports/%d", id(r)), token, obj{"status": "bogus"})
	e.expect(404, "PATCH", "/reports/99999", token, obj{"status": "resolved"})
}

func TestListReportsRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("nonadmin@stateuniversity.edu")
	e.expect(403, "GET", "/reports", token, nil)
	e.makeAdmin(id(user))
	e.expect(200, "GET", "/reports", token, nil)
	e.expect(400, "GET", "/reports?status=bogus", token, nil)
}

func TestListReportsStatusFilter(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("mod2@stateuniversity.edu")
	e.makeAdmin(id(user))
	l1, l2 := e.createListing(), e.createListing()
	r1 := e.expect(201, "POST", "/reports", token, report("listing", l1, "spam")).(obj)
	e.expect(201, "POST", "/reports", token, report("listing", l2, "spam"))
	e.expect(200, "PATCH", path("/reports/%d", id(r1)), token, obj{"status": "dismissed"})
	if got := list(e.expect(200, "GET", "/reports?status=pending", token, nil)); len(got) != 1 {
		t.Fatalf("pending: %v", got)
	}
	if got := list(e.expect(200, "GET", "/reports", token, nil)); len(got) != 2 {
		t.Fatalf("all: %v", got)
	}
}

func TestBlockUnblockRoundtrip(t *testing.T) {
	e := newEnv(t)
	a, _ := e.signup("blocker@stateuniversity.edu")
	_, b := e.signup("blocked@stateuniversity.edu")
	out := e.expect(200, "POST", "/blocks", a, obj{"blocked_user_id": id(b)}).(obj)
	if out["blocked_user"].(obj)["name"] != "Test" || num(out, "blocked_id") != id(b) {
		t.Fatalf("got %v", out)
	}
	// blocking twice returns the same row
	again := e.expect(200, "POST", "/blocks", a, obj{"blocked_user_id": id(b)}).(obj)
	if id(again) != id(out) {
		t.Fatal("double block created a second row")
	}
	if got := list(e.expect(200, "GET", "/blocks", a, nil)); len(got) != 1 || num(got[0].(obj), "blocked_id") != id(b) {
		t.Fatalf("got %v", got)
	}
	e.expect(204, "DELETE", path("/blocks/%d", id(b)), a, nil)
	if got := list(e.expect(200, "GET", "/blocks", a, nil)); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestCannotBlockSelfOrUnknownUser(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("self@stateuniversity.edu")
	e.expect(400, "POST", "/blocks", token, obj{"blocked_user_id": id(user)})
	e.expect(404, "POST", "/blocks", token, obj{"blocked_user_id": 999999})
}

func TestDoubleUnblockIsIdempotent(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("unblocker@stateuniversity.edu")
	e.expect(204, "DELETE", "/blocks/999999", token, nil)
}

func TestBlockedUserCannotMessageRoommateProfile(t *testing.T) {
	e := newEnv(t)
	ownerToken, owner := e.signup("owner5@stateuniversity.edu")
	e.verifyUser(id(owner))
	profile := e.createRoommateProfile(ownerToken)
	otherToken, other := e.signup("other@stateuniversity.edu")
	e.verifyUser(id(other))
	e.expect(200, "POST", "/blocks", otherToken, obj{"blocked_user_id": id(owner)})
	e.expect(403, "POST", "/conversations", otherToken, obj{"kind": "roommate", "roommate_profile_id": profile})
}

func TestBlockAppliesInBothDirectionsAndToExistingThreads(t *testing.T) {
	e := newEnv(t)
	ownerToken, owner := e.signup("owner7@stateuniversity.edu")
	e.verifyUser(id(owner))
	profile := e.createRoommateProfile(ownerToken)
	otherToken, other := e.signup("other7@stateuniversity.edu")
	e.verifyUser(id(other))

	convo := e.expect(201, "POST", "/conversations", otherToken, obj{"kind": "roommate", "roommate_profile_id": profile}).(obj)
	if num(convo, "counterpart_user_id") != id(owner) {
		t.Fatalf("counterpart %v", convo["counterpart_user_id"])
	}
	e.expect(201, "POST", path("/conversations/%d/messages", id(convo)), otherToken, obj{"text": "hi"})

	// the OWNER blocks the other user: the other user's next message is refused
	e.expect(200, "POST", "/blocks", ownerToken, obj{"blocked_user_id": id(other)})
	e.expect(403, "POST", path("/conversations/%d/messages", id(convo)), otherToken, obj{"text": "hello?"})
}

func TestUnverifiedUserCannotCreateRoommateProfile(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("unverified@stateuniversity.edu")
	e.expect(403, "POST", "/roommates", token, obj{
		"name": "Sam", "age": 21, "school": "State University", "year": "Junior",
		"budget_min": 500, "budget_max": 900, "move_in_date": "Aug 1",
	})
}

func TestUnverifiedUserCannotStartRoommateConversation(t *testing.T) {
	e := newEnv(t)
	ownerToken, owner := e.signup("owner6@stateuniversity.edu")
	e.verifyUser(id(owner))
	profile := e.createRoommateProfile(ownerToken)
	unverified, _ := e.signup("unverified2@stateuniversity.edu")
	e.expect(403, "POST", "/conversations", unverified, obj{"kind": "roommate", "roommate_profile_id": profile})
}

func TestCannotConversationWithYourself(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("solo@stateuniversity.edu")
	e.verifyUser(id(user))
	profile := e.createRoommateProfile(token)
	e.expect(400, "POST", "/conversations", token, obj{"kind": "roommate", "roommate_profile_id": profile})
}

func TestRoommateProfileCrud(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("crud@stateuniversity.edu")
	if out := e.expect(200, "GET", "/roommates/me", token, nil); out != nil {
		t.Fatalf("expected null before a profile exists, got %v", out)
	}
	e.verifyUser(id(user))
	profile := e.createRoommateProfile(token)
	e.expect(400, "POST", "/roommates", token, obj{
		"name": "Dup", "age": 20, "school": "S", "year": "Junior", "budget_min": 1, "budget_max": 2, "move_in_date": "Aug 1"})

	out := e.expect(200, "PATCH", "/roommates/me", token, obj{"bio": "new bio", "tags": []string{"a", "b"}, "visibility": "paused"}).(obj)
	if out["bio"] != "new bio" || out["visibility"] != "paused" || len(list(out["tags"])) != 2 || out["verified"] != true {
		t.Fatalf("got %v", out)
	}
	// untouched fields survive a partial update
	if out["looking_for"] != "" || out["emoji"] != "🙂" || out["grad"] != "from-violet to-mint" {
		t.Fatalf("defaults lost: %v", out)
	}
	e.expect(400, "PATCH", "/roommates/me", token, obj{"visibility": "removed"})
	if ids := liveRoommateIDs(e); ids[profile] {
		t.Fatal("paused profile should not be listed")
	}
	e.expect(200, "GET", path("/roommates/%d", profile), "", nil)
	e.expect(404, "GET", "/roommates/99999", "", nil)
}

func TestConversationsAndMessages(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("chat@stateuniversity.edu")
	listing := e.createListing()
	convo := e.expect(201, "POST", "/conversations", token, obj{"kind": "landlord", "listing_id": listing}).(obj)
	if convo["title"] != "Sandra" || convo["subtitle"] != "Cozy studio" || convo["counterpart_user_id"] != nil {
		t.Fatalf("got %v", convo)
	}
	// find-or-create: same conversation the second time
	again := e.expect(201, "POST", "/conversations", token, obj{"kind": "landlord", "listing_id": listing}).(obj)
	if id(again) != id(convo) {
		t.Fatal("expected the existing conversation")
	}
	msg := e.expect(201, "POST", path("/conversations/%d/messages", id(convo)), token, obj{"text": "Is it available?"}).(obj)
	if msg["sender"] != "me" || msg["text"] != "Is it available?" {
		t.Fatalf("got %v", msg)
	}
	got := e.expect(200, "GET", path("/conversations/%d", id(convo)), token, nil).(obj)
	if len(list(got["messages"])) != 1 {
		t.Fatalf("got %v", got)
	}
	if len(list(e.expect(200, "GET", "/conversations", token, nil))) != 1 {
		t.Fatal("expected one conversation")
	}
	other, _ := e.signup("nosy@stateuniversity.edu")
	e.expect(404, "GET", path("/conversations/%d", id(convo)), other, nil)
	e.expect(404, "POST", path("/conversations/%d/messages", id(convo)), other, obj{"text": "x"})
}

func TestFavorites(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("fav@stateuniversity.edu")
	listing := e.createListing()
	e.expect(204, "PUT", path("/favorites/%d", listing), token, nil)
	e.expect(204, "PUT", path("/favorites/%d", listing), token, nil) // idempotent
	if got := list(e.expect(200, "GET", "/favorites", token, nil)); len(got) != 1 || id(got[0].(obj)) != listing {
		t.Fatalf("got %v", got)
	}
	e.expect(404, "PUT", "/favorites/99999", token, nil)
	e.expect(204, "DELETE", path("/favorites/%d", listing), token, nil)
	if got := list(e.expect(200, "GET", "/favorites", token, nil)); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// ---- admin user management (tests/test_admin_users.py) ----

func TestUserDetailRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, other := e.signup("plain2@gmail.com")
	e.expect(403, "GET", path("/users/%d", id(other)), token, nil)
}

func TestUserDetail404ForUnknownUser(t *testing.T) {
	e := newEnv(t)
	token, admin := e.signup("admin2@gmail.com")
	e.makeAdmin(id(admin))
	e.expect(404, "GET", "/users/999999", token, nil)
}

func TestUserDetailIncludesBookingsProfileAndReports(t *testing.T) {
	e := newEnv(t)
	adminToken, admin := e.signup("admin3@gmail.com")
	e.makeAdmin(id(admin))
	studentToken, student := e.signup("student3@gmail.com")
	e.verifyUser(id(student))
	listing := e.createListing()
	e.createBooking(studentToken, listing)
	e.createRoommateProfile(studentToken)
	e.expect(201, "POST", "/reports", adminToken, report("user", id(student), "spam"))

	body := e.expect(200, "GET", path("/users/%d", id(student)), adminToken, nil).(obj)
	if body["user"].(obj)["email"] != "student3@gmail.com" {
		t.Fatalf("user %v", body["user"])
	}
	bookings := list(body["bookings"])
	if len(bookings) != 1 || id(bookings[0].(obj)["listing"].(obj)) != listing {
		t.Fatalf("bookings %v", bookings)
	}
	if body["roommate_profile"].(obj)["name"] != "Zoe" {
		t.Fatalf("profile %v", body["roommate_profile"])
	}
	if len(list(body["reports_filed"])) != 0 {
		t.Fatalf("filed %v", body["reports_filed"])
	}
	against := list(body["reports_against"])
	if len(against) != 1 || against[0].(obj)["reporter_email"] != "admin3@gmail.com" {
		t.Fatalf("against %v", against)
	}
}

func TestUserDetailWithNoActivityHasEmptyListsNotNull(t *testing.T) {
	e := newEnv(t)
	adminToken, admin := e.signup("admin9@gmail.com")
	e.makeAdmin(id(admin))
	_, u := e.signup("quiet@gmail.com")
	body := e.expect(200, "GET", path("/users/%d", id(u)), adminToken, nil).(obj)
	for _, k := range []string{"bookings", "reports_filed", "reports_against"} {
		if _, ok := body[k].([]any); !ok {
			t.Errorf("%s = %#v, want []", k, body[k])
		}
	}
	if body["roommate_profile"] != nil {
		t.Errorf("roommate_profile = %v, want null", body["roommate_profile"])
	}
}

func TestListUsersRequiresAdminAndReturnsEveryone(t *testing.T) {
	e := newEnv(t)
	plain, _ := e.signup("plain@gmail.com")
	e.expect(403, "GET", "/users", plain, nil)
	adminToken, admin := e.signup("admin@gmail.com")
	e.makeAdmin(id(admin))
	e.signup("someone@gmail.com")
	emails := map[string]bool{}
	for _, u := range list(e.expect(200, "GET", "/users", adminToken, nil)) {
		emails[u.(obj)["email"].(string)] = true
	}
	if !emails["admin@gmail.com"] || !emails["someone@gmail.com"] {
		t.Fatalf("got %v", emails)
	}
}

func TestSetAdminPromotesAndDemotes(t *testing.T) {
	e := newEnv(t)
	adminToken, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))
	_, target := e.signup("newmod@gmail.com")
	up := e.expect(200, "PATCH", path("/users/%d/admin", id(target)), adminToken, obj{"is_admin": true}).(obj)
	if up["is_admin"] != true {
		t.Fatalf("got %v", up)
	}
	down := e.expect(200, "PATCH", path("/users/%d/admin", id(target)), adminToken, obj{"is_admin": false}).(obj)
	if down["is_admin"] != false {
		t.Fatalf("got %v", down)
	}
	e.expect(404, "PATCH", "/users/99999/admin", adminToken, obj{"is_admin": true})
}

func TestAdminCannotRemoveOwnAccess(t *testing.T) {
	e := newEnv(t)
	token, admin := e.signup("onlyadmin@gmail.com")
	e.makeAdmin(id(admin))
	e.expect(400, "PATCH", path("/users/%d/admin", id(admin)), token, obj{"is_admin": false})
}

func TestSetAdminRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("notadmin@gmail.com")
	_, other := e.signup("victim@gmail.com")
	e.expect(403, "PATCH", path("/users/%d/admin", id(other)), token, obj{"is_admin": true})
}

func TestReportResponseIncludesReporterEmailAndTargetLabel(t *testing.T) {
	e := newEnv(t)
	reporter, _ := e.signup("reporter2@gmail.com")
	adminToken, admin := e.signup("modadmin@gmail.com")
	e.makeAdmin(id(admin))
	listing := e.createListing()

	created := e.expect(201, "POST", "/reports", reporter, report("listing", listing, "spam")).(obj)
	if created["reporter_email"] != "reporter2@gmail.com" || created["target_label"] != "Cozy studio" {
		t.Fatalf("got %v", created)
	}
	for _, r := range list(e.expect(200, "GET", "/reports", adminToken, nil)) {
		row := r.(obj)
		if num(row, "target_id") == listing {
			if row["reporter_email"] != "reporter2@gmail.com" || row["target_label"] != "Cozy studio" {
				t.Fatalf("got %v", row)
			}
			return
		}
	}
	t.Fatal("report not listed")
}
