package api_test

import (
	"fmt"
	"testing"
)

func TestSchoolsDirectoryIsLoadedAndCleaned(t *testing.T) {
	e := newEnv(t)
	all := list(e.expect(200, "GET", "/schools?limit=500", "", nil))
	if len(all) != 452 {
		t.Fatalf("got %d schools, want 452", len(all))
	}
	for _, x := range all {
		n := x.(obj)["name"]
		if n == "Name" || n == "Institution" {
			t.Fatalf("junk header row leaked in: %v", n)
		}
		if f := x.(obj)["funding"]; f == "federa" || f == "type" || f == "funding" {
			t.Fatalf("bad funding %v on %v", f, n)
		}
	}
	// loading twice is idempotent
	if got := e.count(`SELECT COUNT(*) FROM schools`); got != 452 {
		t.Fatalf("db has %d", got)
	}
}

func TestSchoolSearchAndFilters(t *testing.T) {
	e := newEnv(t)
	names := func(q string) []string { return names(e.expect(200, "GET", "/schools"+q, "", nil)) }
	if got := names("?q=unilag"); len(got) > 0 {
		t.Logf("unilag alias matched %v", got)
	}
	lagos := names("?q=university%20of%20lagos")
	if len(lagos) == 0 || lagos[0] != "University of Lagos" {
		t.Fatalf("got %v", lagos)
	}
	for _, typ := range []string{"university", "polytechnic", "college_of_education"} {
		for _, x := range list(e.expect(200, "GET", "/schools?limit=500&type="+typ, "", nil)) {
			if x.(obj)["type"] != typ {
				t.Fatalf("type filter leaked %v", x)
			}
		}
	}
	if len(list(e.expect(200, "GET", "/schools?limit=500&state=lagos", "", nil))) < 3 {
		t.Fatal("expected several Lagos schools")
	}
	if got := len(list(e.expect(200, "GET", "/schools?limit=5", "", nil))); got != 5 {
		t.Fatalf("limit: %d", got)
	}
	page1 := names("?limit=3")
	page2 := names("?limit=3&offset=3")
	if page1[0] == page2[0] {
		t.Fatal("offset ignored")
	}
	e.expect(422, "GET", "/schools?limit=abc", "", nil)
}

func schoolByName(t *testing.T, e *env, name string) obj {
	t.Helper()
	for _, x := range list(e.expect(200, "GET", "/schools?limit=500", "", nil)) {
		if x.(obj)["name"] == name {
			return x.(obj)
		}
	}
	t.Fatalf("school %q not found", name)
	return nil
}

func TestGetSchool(t *testing.T) {
	e := newEnv(t)
	unilag := schoolByName(t, e, "University of Lagos")
	got := e.expect(200, "GET", path("/schools/%d", id(unilag)), "", nil).(obj)
	if got["latitude"] == nil || got["confidence"] == "" || got["state"] != "Lagos" {
		t.Fatalf("got %v", got)
	}
	e.expect(404, "GET", "/schools/99999", "", nil)
}

func TestUserCanChooseASchool(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("pick@gmail.com")
	if user["school_id"] != nil {
		t.Fatalf("school_id should start null: %v", user["school_id"])
	}
	unilag := schoolByName(t, e, "University of Lagos")
	out := e.expect(200, "PATCH", "/users/me", token, obj{"school_id": id(unilag), "school": "ignored text"}).(obj)
	if num(out, "school_id") != id(unilag) || out["school"] != "University of Lagos" {
		t.Fatalf("got %v", out)
	}
	if me := e.expect(200, "GET", "/users/me", token, nil).(obj); num(me, "school_id") != id(unilag) {
		t.Fatalf("not persisted: %v", me)
	}
	e.expect(400, "PATCH", "/users/me", token, obj{"school_id": 99999})
}

func TestSignupWithSchool(t *testing.T) {
	e := newEnv(t)
	unilag := schoolByName(t, e, "University of Lagos")
	out := e.expect(200, "POST", "/auth/signup", "", obj{"email": "s@gmail.com", "password": "pw123456", "name": "S", "school_id": id(unilag)}).(obj)
	u := out["user"].(obj)
	if num(u, "school_id") != id(unilag) || u["school"] != "University of Lagos" {
		t.Fatalf("got %v", u)
	}
	e.expect(400, "POST", "/auth/signup", "", obj{"email": "t@gmail.com", "password": "pw123456", "name": "T", "school_id": 99999})
}

func TestListingsMeasuredFromChosenSchool(t *testing.T) {
	e := newEnv(t)
	unilag := schoolByName(t, e, "University of Lagos")
	lat, lng := unilag["latitude"].(float64), unilag["longitude"].(float64)
	e.createListingAt("Far", f(lat+0.05), f(lng+0.05))
	e.createListingAt("Near", f(lat+0.002), f(lng))
	e.createListingAt("No coords", nil, nil)

	out := e.expect(200, "GET", path("/listings?school_id=%d", id(unilag)), "", nil)
	if got := titlesOf(out); !equal(got, []string{"Near", "Far"}) {
		t.Fatalf("got %v", got)
	}
	near := list(out)[0].(obj)
	if d := near["distance_km"].(float64); d < 0.1 || d > 0.4 {
		t.Fatalf("near distance %v", d)
	}
	// radius works with a school too
	r := e.expect(200, "GET", path("/listings?school_id=%d&radius_km=2", id(unilag)), "", nil)
	if got := titlesOf(r); !equal(got, []string{"Near"}) {
		t.Fatalf("radius: %v", got)
	}
	// single listing
	nearID := id(near)
	one := e.expect(200, "GET", path("/listings/%d?school_id=%d", nearID, id(unilag)), "", nil).(obj)
	if one["distance_km"] == nil {
		t.Fatalf("detail lacks distance: %v", one)
	}
	if plain := e.expect(200, "GET", path("/listings/%d", nearID), "", nil).(obj); plain["distance_km"] != nil {
		t.Fatalf("distance without school: %v", plain["distance_km"])
	}
	e.expect(404, "GET", "/listings?school_id=99999", "", nil)
	e.expect(404, "GET", fmt.Sprintf("/listings/%d?school_id=99999", nearID), "", nil)
}

func TestGoMigrationsDoNotTouchAlembicVersion(t *testing.T) {
	// The Python service may share this database; Alembic must still see its own head.
	e := newEnv(t)
	var rev string
	if err := e.db.QueryRow(t.Context(), `SELECT version_num FROM alembic_version`).Scan(&rev); err != nil || rev != "6c35864e1b28" {
		t.Fatalf("alembic_version=%q err=%v", rev, err)
	}
}
