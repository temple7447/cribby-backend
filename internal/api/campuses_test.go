package api_test

import (
	"context"
	"testing"
)

func TestCampusLandingPages(t *testing.T) {
	e := newEnv(t)
	user, _ := e.signup("student@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	// pick a school that has coordinates, and put a live home right next to it
	var sid int
	var lat, lng float64
	row := e.db.QueryRow(context.Background(), `SELECT id, latitude, longitude FROM schools WHERE latitude IS NOT NULL AND name LIKE '%Benin%' ORDER BY id LIMIT 1`)
	if err := row.Scan(&sid, &lat, &lng); err != nil {
		t.Fatal(err)
	}
	home := e.createListingAt("Near uniben", &lat, &lng)
	far := 0.5 // ~55 km away: must not be counted
	e.createListingAt("Far away", &far, &far)

	body := obj{"school_id": sid, "whatsapp_url": "https://chat.whatsapp.com/abc123", "tagline": " Find a home near campus "}
	e.expect(401, "PUT", "/admin/campuses/uniben", "", body)
	e.expect(403, "PUT", "/admin/campuses/uniben", user, body)
	e.expect(422, "PUT", "/admin/campuses/Login", admin, body) // a page name the website already uses
	e.expect(422, "PUT", "/admin/campuses/market", admin, body)
	e.expect(422, "PUT", "/admin/campuses/x", admin, body) // too short
	bad := obj{"school_id": sid, "whatsapp_url": "https://evil.example.com/x"}
	e.expect(422, "PUT", "/admin/campuses/uniben", admin, bad) // only WhatsApp links
	e.expect(404, "PUT", "/admin/campuses/uniben", admin, obj{"school_id": 999999})
	got := list(e.expect(200, "PUT", "/admin/campuses/UNIBEN", admin, body)) // slugs are lower-cased
	if len(got) != 1 || got[0].(obj)["slug"] != "uniben" || got[0].(obj)["tagline"] != "Find a home near campus" {
		t.Fatalf("saved: %v", got)
	}

	// anyone can read it, with how many live homes are close by
	c := e.expect(200, "GET", "/campuses/Uniben", "", nil).(obj)
	if num(c, "homes_near") != 1 || c["whatsapp_url"] != "https://chat.whatsapp.com/abc123" || c["school"].(obj)["id"] == nil {
		t.Fatalf("campus: %v", c)
	}
	_ = home
	if n := len(list(e.expect(200, "GET", "/campuses", "", nil))); n != 1 {
		t.Fatalf("public list: %d", n)
	}
	e.expect(404, "GET", "/campuses/nowhere", "", nil)

	// switching it off hides it from the public but not from admins
	off := false
	e.expect(200, "PUT", "/admin/campuses/uniben", admin, obj{"school_id": sid, "active": off})
	e.expect(404, "GET", "/campuses/uniben", "", nil)
	if n := len(list(e.expect(200, "GET", "/campuses", "", nil))); n != 0 {
		t.Fatalf("hidden campus listed: %d", n)
	}
	if n := len(list(e.expect(200, "GET", "/admin/campuses", admin, nil))); n != 1 {
		t.Fatalf("admin list: %d", n)
	}
	e.expect(204, "DELETE", "/admin/campuses/uniben", admin, nil)
	if n := len(list(e.expect(200, "GET", "/admin/campuses", admin, nil))); n != 0 {
		t.Fatalf("after delete: %d", n)
	}
}

func TestCampusGrowthRanksFastestAndSuggestsWhereToOpenNext(t *testing.T) {
	e := newEnv(t)
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))
	user, _ := e.signup("student@gmail.com")

	var benin, lagos, other int
	for _, r := range []struct {
		dst  *int
		like string
	}{{&benin, "%University of Benin%"}, {&lagos, "%University of Lagos%"}, {&other, "%Ibadan%"}} {
		if err := e.db.QueryRow(context.Background(), `SELECT id FROM schools WHERE name LIKE ? ORDER BY id LIMIT 1`, r.like).Scan(r.dst); err != nil {
			t.Fatal(err)
		}
	}
	e.expect(200, "PUT", "/admin/campuses/uniben", admin, obj{"school_id": benin})
	e.expect(200, "PUT", "/admin/campuses/unilag", admin, obj{"school_id": lagos})

	join := func(email string, school int) int {
		body := e.expect(200, "POST", "/auth/signup", "", obj{"email": email, "password": "pw123456", "name": "T", "school_id": school}).(obj)
		return id(body["user"].(obj))
	}
	join("b1@gmail.com", benin)
	join("b2@gmail.com", benin)
	old := join("b3@gmail.com", benin)
	e.exec(`UPDATE users SET created_at = ? WHERE id = ?`, past10Days(), old) // joined 10 days ago: last week's, not this week's
	join("l1@gmail.com", lagos)
	join("o1@gmail.com", other)
	join("o2@gmail.com", other)
	join("o3@gmail.com", other)

	e.expect(403, "GET", "/admin/campus-growth", user, nil)
	got := e.expect(200, "GET", "/admin/campus-growth", admin, nil).(obj)
	rows := list(got["campuses"])
	if len(rows) != 2 || rows[0].(obj)["slug"] != "uniben" {
		t.Fatalf("fastest first: %v", rows)
	}
	b := rows[0].(obj)
	if num(b, "users") != 3 || num(b, "users_7d") != 2 || num(b, "users_prev_7d") != 1 {
		t.Fatalf("uniben numbers: %v", b)
	}
	next := list(got["open_next"])
	if len(next) == 0 || num(next[0].(obj), "school_id") != other || num(next[0].(obj), "users") != 3 {
		t.Fatalf("open next should lead with the school people pick most that has no page: %v", next)
	}
}
