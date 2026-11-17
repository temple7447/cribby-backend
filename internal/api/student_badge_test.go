package api_test

import (
	"strings"
	"testing"
)

func TestRoommateStudentBadgeNeedsAVerifiedUniversityEmail(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		email          string
		verified       bool
		wantVerified   bool
		wantStudentTag bool
	}{
		{"ada@unilag.edu.ng", true, true, true},
		{"bayo@uniben.EDU.NG", true, true, true}, // case doesn't matter
		{"dee@gmail.com", true, true, false},     // confirmed, but not a university address
		{"eve@edu.ng.example.com", true, true, false},
	}
	// a university address nobody confirmed proves nothing, and can't make a profile at all
	unconfirmed, _ := e.signup("chi@unilag.edu.ng")
	e.expect(403, "POST", "/roommates", unconfirmed, obj{
		"name": "Chi", "age": 20, "school": "State University", "year": "Sophomore",
		"budget_min": 500, "budget_max": 900, "move_in_date": "Aug 1",
	})
	var ids []int
	for _, c := range cases {
		token, user := e.signup(c.email)
		if c.verified {
			e.verifyUser(id(user))
		}
		ids = append(ids, e.createRoommateProfile(token))
	}
	viewer, _ := e.signup("viewer@gmail.com")
	for i, c := range cases {
		got := e.expect(200, "GET", path("/roommates/%d", ids[i]), viewer, nil).(obj)
		if got["verified"] != c.wantVerified || got["student_verified"] != c.wantStudentTag {
			t.Errorf("%s: verified=%v student_verified=%v, want %v/%v", c.email, got["verified"], got["student_verified"], c.wantVerified, c.wantStudentTag)
		}
	}
}

func TestStudentIDCheckGivesTheBadge(t *testing.T) {
	e := newEnv(t)
	e.photos.configured, e.photos.urlFmt, e.photos.idFmt = true, "https://cdn.test/p%d.jpg", "cribby/p%d"
	token, user := e.signup("dee@gmail.com")
	e.verifyUser(id(user))
	adminTok, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))
	rid := e.createRoommateProfile(token)
	badge := func() any {
		return e.expect(200, "GET", path("/roommates/%d", rid), adminTok, nil).(obj)["student_verified"]
	}
	send := func(school string) (int, any) {
		return e.upload("/student/verification", token, "id.jpg", "image/jpeg", []byte("jpegbytes"), map[string]string{"school": school})
	}

	if got := e.expect(200, "GET", "/student/verification", token, nil).(obj); got["status"] != "none" {
		t.Fatalf("got %v", got)
	}
	if code, _ := send("  "); code != 422 {
		t.Fatalf("empty school: %d", code)
	}
	code, out := send("UNIBEN")
	if code != 200 || out.(obj)["status"] != "pending" || strings.Contains(toJSON(out), "private") {
		t.Fatalf("submit: %d %v", code, out)
	}
	if badge() != false {
		t.Fatal("pending must not give the badge")
	}

	e.expect(403, "GET", "/admin/student-verifications", token, nil)
	queue := list(e.expect(200, "GET", "/admin/student-verifications", adminTok, nil))
	if len(queue) != 1 || !strings.HasPrefix(queue[0].(obj)["id_photo_url"].(string), "https://cdn.test/signed/") {
		t.Fatalf("queue %v", queue)
	}
	uid := id(user)
	e.expect(403, "PATCH", path("/admin/student-verifications/%d", uid), token, obj{"decision": "approve"})
	e.expect(422, "PATCH", path("/admin/student-verifications/%d", uid), adminTok, obj{"decision": "reject"})
	e.expect(200, "PATCH", path("/admin/student-verifications/%d", uid), adminTok, obj{"decision": "reject", "note": "Blurry"})
	if badge() != false || e.expect(200, "GET", "/student/verification", token, nil).(obj)["note"] != "Blurry" {
		t.Fatal("rejected must not give the badge and must say why")
	}

	if code, _ := send("UNIBEN"); code != 200 {
		t.Fatalf("resubmit: %d", code)
	}
	e.expect(200, "PATCH", path("/admin/student-verifications/%d", uid), adminTok, obj{"decision": "approve"})
	if badge() != true {
		t.Fatal("approved student should have the badge")
	}
	if len(e.photos.destroyed) == 0 {
		t.Fatal("the ID photo should be deleted after the decision")
	}
	if code, _ := send("UNIBEN"); code != 409 {
		t.Fatalf("already verified: %d", code)
	}
}
