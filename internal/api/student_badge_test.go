package api_test

import "testing"

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
