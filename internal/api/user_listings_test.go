package api_test

import (
	"testing"
)

func apartment(title string) obj {
	return obj{
		"title": title, "type": "Self-contain", "price_per": 350000, "deposit": 50000, "utilities_monthly": 5000,
		"latitude": 6.5180, "longitude": 3.3910, "address": "12 Akoka Rd, Yaba", "description": "Quiet, 24h light",
		"amenities": []string{"Wi-Fi", " ", "Water"},
	}
}

func TestUserPostsApartmentThatWaitsForReview(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("poster@gmail.com")
	out := e.expect(201, "POST", "/listings", token, apartment("Akoka self-contain")).(obj)
	lid := id(out)
	if out["status"] != "pending" || out["landlord_name"] == "" || out["owner_id"] == nil || out["total"].(float64) != 350000 {
		t.Fatalf("got %v", out)
	}
	if am := list(out["amenities"]); len(am) != 2 {
		t.Fatalf("blank amenity should be dropped: %v", am)
	}
	// not public yet: hidden from search and from other people, visible to the owner
	for _, l := range list(e.expect(200, "GET", "/listings", "", nil)) {
		if id(l.(obj)) == lid {
			t.Fatal("pending listing leaked into public search")
		}
	}
	e.expect(404, "GET", path("/listings/%d", lid), "", nil)
	other, _ := e.signup("nosy@gmail.com")
	e.expect(404, "GET", path("/listings/%d", lid), other, nil)
	e.expect(200, "GET", path("/listings/%d", lid), token, nil)
	if mine := list(e.expect(200, "GET", "/listings/mine", token, nil)); len(mine) != 1 {
		t.Fatalf("mine=%v", mine)
	}

	// admin approves → public
	adminTok, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))
	queue := list(e.expect(200, "GET", "/admin/listings", adminTok, nil))
	if len(queue) != 1 || id(queue[0].(obj)) != lid {
		t.Fatalf("queue=%v", queue)
	}
	e.expect(403, "GET", "/admin/listings", token, nil)
	e.expect(422, "PATCH", path("/admin/listings/%d/review", lid), adminTok, obj{"decision": "reject"}) // needs a reason
	e.expect(200, "PATCH", path("/admin/listings/%d/review", lid), adminTok, obj{"decision": "approve"})
	e.expect(200, "GET", path("/listings/%d", lid), "", nil)

	// an owner edit goes back for review; someone else can't edit it
	e.expect(403, "PATCH", path("/listings/%d", lid), other, apartment("Hijacked"))
	edited := e.expect(200, "PATCH", path("/listings/%d", lid), token, apartment("Akoka self-contain (renovated)")).(obj)
	if edited["status"] != "pending" {
		t.Fatalf("edit should re-queue: %v", edited["status"])
	}
	e.expect(404, "GET", path("/listings/%d", lid), "", nil)

	// rejected with a note the owner can read
	e.expect(200, "PATCH", path("/admin/listings/%d/review", lid), adminTok, obj{"decision": "reject", "note": "Photos missing"})
	got := e.expect(200, "GET", path("/listings/%d", lid), token, nil).(obj)
	if got["status"] != "rejected" || got["review_note"] != "Photos missing" {
		t.Fatalf("got %v", got)
	}

	// owner takes it down
	e.expect(403, "DELETE", path("/listings/%d", lid), other, nil)
	e.expect(204, "DELETE", path("/listings/%d", lid), token, nil)
	if got := e.expect(200, "GET", path("/listings/%d", lid), token, nil).(obj); got["status"] != "removed" {
		t.Fatalf("got %v", got["status"])
	}
}

func TestPostValidationRequiresPinAndSaneValues(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("poster2@gmail.com")
	noPin := apartment("No pin here")
	delete(noPin, "latitude")
	e.expect(422, "POST", "/listings", token, noPin)
	bad := apartment("x")
	e.expect(422, "POST", "/listings", token, bad)
	zero := apartment("Free room")
	zero["price_per"] = 0
	e.expect(422, "POST", "/listings", token, zero)
	lowTotal := apartment("Shared flat")
	lowTotal["total"] = 100
	e.expect(422, "POST", "/listings", token, lowTotal)
}

func TestPostingIsCappedPerDay(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("spammer@gmail.com")
	for i := 0; i < 5; i++ {
		e.expect(201, "POST", "/listings", token, apartment(path("Flat number %d", i)))
	}
	e.expect(429, "POST", "/listings", token, apartment("One too many"))
}

func TestOwnerCanAddPhotosButNotToOthers(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("photos@gmail.com")
	lid := id(e.expect(201, "POST", "/listings", token, apartment("Photo flat")).(obj))
	if code, _ := e.upload(path("/listings/%d/photos", lid), token, "a.jpg", "image/jpeg", jpeg, nil); code != 200 {
		t.Fatalf("owner upload: %d", code)
	}
	other, _ := e.signup("photo-thief@gmail.com")
	if code, _ := e.upload(path("/listings/%d/photos", lid), other, "a.jpg", "image/jpeg", jpeg, nil); code != 403 {
		t.Fatalf("other upload: %d", code)
	}
}

func TestWalkIsEstimatedFromPostersSchool(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("walker@gmail.com")
	unilag := schoolByName(t, e, "University of Lagos")
	e.expect(200, "PATCH", "/users/me", token, obj{"school_id": id(unilag)})
	body := apartment("Walkable flat")
	body["latitude"], body["longitude"] = unilag["latitude"].(float64)+0.01, unilag["longitude"]
	out := e.expect(201, "POST", "/listings", token, body).(obj)
	if w := out["walk"].(float64); w < 10 || w > 25 {
		t.Fatalf("walk=%v, want ~17 min for ~1.1 km", w)
	}
}
