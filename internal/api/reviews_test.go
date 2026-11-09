package api_test

import "testing"

func TestReviewsFlow(t *testing.T) {
	e := newEnv(t)
	listing := e.createListing()
	a, aUser := e.signup("a@gmail.com")
	b, _ := e.signup("b@gmail.com")
	stranger, _ := e.signup("c@gmail.com")
	adminTok, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))
	url := path("/listings/%d/reviews", listing)

	// anyone can read; nothing yet
	got := e.expect(200, "GET", url, "", nil).(obj)
	if num(got, "count") != 0 || got["can_review"] != false || len(list(got["items"])) != 0 {
		t.Fatalf("empty: %v", got)
	}
	// strangers (no booking or chat) cannot review; guests get 401
	e.expect(401, "PUT", url, "", obj{"rating": 5})
	e.expect(403, "PUT", url, stranger, obj{"rating": 5})

	// a booking unlocks reviewing for A, a chat for B
	e.expect(201, "POST", "/bookings", a, obj{"listing_id": listing, "move_in_date": "2026-12-01"})
	e.exec(`INSERT INTO conversations (user_id, kind, listing_id, title, subtitle, emoji, grad) VALUES (?, 'landlord', ?, 't', 's', 'x', 'g')`, id(e.expect(200, "GET", "/users/me", b, nil).(obj)), listing)
	if g := e.expect(200, "GET", url, a, nil).(obj); g["can_review"] != true {
		t.Fatalf("a should be able to review: %v", g)
	}
	e.expect(422, "PUT", url, a, obj{"rating": 0})
	e.expect(422, "PUT", url, a, obj{"rating": 6})
	e.expect(200, "PUT", url, a, obj{"rating": 5, "body": "  Great landlord  "})
	got = e.expect(200, "PUT", url, b, obj{"rating": 2, "body": "Noisy"}).(obj)
	if num(got, "count") != 2 || got["rating"].(float64) != 3.5 {
		t.Fatalf("avg: %v", got)
	}

	// editing replaces, not duplicates
	got = e.expect(200, "PUT", url, a, obj{"rating": 4}).(obj)
	if num(got, "count") != 2 || got["rating"].(float64) != 3 {
		t.Fatalf("after edit: %v", got)
	}
	items := list(e.expect(200, "GET", url, a, nil).(obj)["items"])
	mine := 0
	for _, it := range items {
		if it.(obj)["mine"] == true {
			mine++
		}
		if _, leaked := it.(obj)["user_id"]; leaked {
			t.Fatal("user id must not leak")
		}
	}
	if mine != 1 {
		t.Fatalf("mine: %d", mine)
	}

	// the owner can't review their own home
	e.exec(`UPDATE listings SET owner_id = ? WHERE id = ?`, id(aUser), listing)
	e.expect(403, "PUT", url, a, obj{"rating": 5})

	// only the author (or an admin) can delete
	var rid int
	for _, it := range items {
		if it.(obj)["mine"] == true {
			rid = id(it.(obj))
		}
	}
	e.expect(404, "DELETE", path("/reviews/%d", rid), b, nil)
	e.expect(204, "DELETE", path("/reviews/%d", rid), a, nil)
	got = e.expect(200, "GET", url, "", nil).(obj)
	if num(got, "count") != 1 || got["rating"].(float64) != 2 {
		t.Fatalf("after delete: %v", got)
	}
	left := id(list(got["items"])[0].(obj))
	e.expect(204, "DELETE", path("/reviews/%d", left), adminTok, nil)
	if g := e.expect(200, "GET", url, "", nil).(obj); num(g, "count") != 0 || g["rating"].(float64) != 0 {
		t.Fatalf("all gone: %v", g)
	}
}
