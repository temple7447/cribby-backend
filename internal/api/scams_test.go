package api_test

import (
	"strings"
	"testing"
)

func TestScamAlertBoard(t *testing.T) {
	e := newEnv(t)
	student, _ := e.signup("student@gmail.com")
	other, _ := e.signup("other@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	report := obj{"kind": "fake_agent", "suspect_name": "  Mr Chidi  ", "suspect_phone": "0803 123 4567", "area": "Ugbowo",
		"details": "Collected 50k for a room that was already rented, then blocked me."}
	e.expect(401, "POST", "/scam-alerts", "", report)
	for name, bad := range map[string]obj{
		"kind":    {"kind": "prank", "suspect_name": "X", "details": report["details"]},
		"nothing": {"kind": "fake_agent", "details": report["details"]},
		"phone":   {"kind": "fake_agent", "suspect_phone": "abc", "details": report["details"]},
		"short":   {"kind": "fake_agent", "suspect_name": "X", "details": "bad man"},
	} {
		if code, _ := e.do("POST", "/scam-alerts", student, bad); code != 422 {
			t.Fatalf("%s: got %d, want 422", name, code)
		}
	}
	made := e.expect(201, "POST", "/scam-alerts", student, report).(obj)
	if made["status"] != "pending" || made["suspect_phone"] != "0803•••567" || made["suspect_name"] != "Mr Chidi" {
		t.Fatalf("created: %v", made)
	}

	// pending alerts are invisible to the public and to other people, visible to their author
	if got := list(e.expect(200, "GET", "/scam-alerts", "", nil)); len(got) != 0 {
		t.Fatalf("pending alert leaked: %v", got)
	}
	if got := list(e.expect(200, "GET", "/scam-alerts/mine", student, nil)); len(got) != 1 {
		t.Fatalf("author should see their own: %v", got)
	}
	if got := list(e.expect(200, "GET", "/scam-alerts/mine", other, nil)); len(got) != 0 {
		t.Fatalf("someone else's: %v", got)
	}

	// admins see the whole number and who reported it; others can't
	e.expect(403, "GET", "/admin/scam-alerts", student, nil)
	q := list(e.expect(200, "GET", "/admin/scam-alerts", admin, nil))
	if len(q) != 1 || q[0].(obj)["suspect_phone_full"] != "08031234567" || q[0].(obj)["reporter_email"] != "student@gmail.com" {
		t.Fatalf("admin queue: %v", q)
	}
	aid := id(made)
	e.expect(403, "PATCH", path("/admin/scam-alerts/%d", aid), student, obj{"decision": "approve"})
	e.expect(422, "PATCH", path("/admin/scam-alerts/%d", aid), admin, obj{"decision": "reject"}) // needs a reason
	e.expect(404, "PATCH", path("/admin/scam-alerts/%d", 9999), admin, obj{"decision": "approve"})
	e.expect(200, "PATCH", path("/admin/scam-alerts/%d", aid), admin, obj{"decision": "approve"})

	// now it's public, with the number masked, and findable by number
	pub := list(e.expect(200, "GET", "/scam-alerts", "", nil))
	if len(pub) != 1 || strings.Contains(toJSON(pub), "08031234567") || strings.Contains(toJSON(pub), "reporter") {
		t.Fatalf("public board leaks: %v", pub)
	}
	if got := list(e.expect(200, "GET", "/scam-alerts?phone=0803-123-4567", "", nil)); len(got) != 1 {
		t.Fatalf("lookup by number: %v", got)
	}
	if got := list(e.expect(200, "GET", "/scam-alerts?phone=09099999999", "", nil)); len(got) != 0 {
		t.Fatalf("an unreported number must come back empty: %v", got)
	}
	e.expect(422, "GET", "/scam-alerts?phone=abc", "", nil)

	// a rejected report never shows; three a day is the limit
	rej := id(e.expect(201, "POST", "/scam-alerts", other, obj{"kind": "other", "suspect_name": "Someone", "details": "A long enough but unconvincing story."}).(obj))
	e.expect(200, "PATCH", path("/admin/scam-alerts/%d", rej), admin, obj{"decision": "reject", "note": "No evidence"})
	if got := list(e.expect(200, "GET", "/scam-alerts", "", nil)); len(got) != 1 {
		t.Fatalf("rejected alert showed: %v", got)
	}
	e.expect(201, "POST", "/scam-alerts", other, obj{"kind": "other", "suspect_name": "A", "details": "Another long enough report text here."})
	e.expect(201, "POST", "/scam-alerts", other, obj{"kind": "other", "suspect_name": "B", "details": "Yet another long enough report text."})
	e.expect(429, "POST", "/scam-alerts", other, obj{"kind": "other", "suspect_name": "C", "details": "One report too many in a day, sorry."})
}
