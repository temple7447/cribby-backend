package api_test

import (
	"context"
	"strings"
	"testing"
)

func item(title string) obj {
	return obj{"title": title, "category": "mattress", "condition": "good", "price": 15000, "contact_phone": "0803 123 4567", "area": "Ugbowo"}
}

func TestStudentMarketplace(t *testing.T) {
	e := newEnv(t)
	e.photos.configured, e.photos.urlFmt, e.photos.idFmt = true, "https://cdn.test/p%d.jpg", "cribby/p%d"
	seller, _ := e.signup("seller@gmail.com")
	buyer, _ := e.signup("buyer@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	// posting needs an account and valid fields
	e.expect(401, "POST", "/market", "", item("Mattress"))
	for name, bad := range map[string]obj{
		"category": {"title": "Fan", "category": "car", "condition": "good", "price": 1, "contact_phone": "08031234567"},
		"price":    {"title": "Fan", "category": "fan", "condition": "good", "price": -5, "contact_phone": "08031234567"},
		"phone":    {"title": "Fan", "category": "fan", "condition": "good", "price": 5, "contact_phone": "abc"},
		"pin":      {"title": "Fan", "category": "fan", "condition": "good", "price": 5, "contact_phone": "08031234567", "latitude": 6.5},
	} {
		if code, _ := e.do("POST", "/market", seller, bad); code != 422 {
			t.Fatalf("%s: got %d, want 422", name, code)
		}
	}
	near := item("Single mattress")
	near["latitude"], near["longitude"], near["leaving_soon"] = 6.4000, 5.6000, true
	made := e.expect(201, "POST", "/market", seller, near).(obj)
	if made["status"] != "active" || made["contact_phone"] != "08031234567" || made["mine"] != true || made["leaving_soon"] != true {
		t.Fatalf("created: %v", made)
	}
	mid := id(made)
	far := item("Standing fan")
	far["category"], far["price"], far["latitude"], far["longitude"] = "fan", 0, 9.0, 7.5
	e.expect(201, "POST", "/market", seller, far)
	e.expect(201, "POST", "/market", seller, func() obj { o := item("Pot set"); o["category"] = "kitchen"; return o }()) // no pin

	// anyone can browse, but the phone number never appears in a list
	all := e.expect(200, "GET", "/market", "", nil)
	if len(list(all)) != 3 || strings.Contains(toJSON(all), "08031234567") {
		t.Fatalf("browse: %v", all)
	}
	if got := list(e.expect(200, "GET", "/market?category=fan", "", nil)); len(got) != 1 || got[0].(obj)["title"] != "Standing fan" {
		t.Fatalf("category filter: %v", got)
	}
	if got := list(e.expect(200, "GET", "/market?free=1", "", nil)); len(got) != 1 {
		t.Fatalf("free filter: %v", got)
	}
	if got := list(e.expect(200, "GET", "/market?q=pot", "", nil)); len(got) != 1 {
		t.Fatalf("search: %v", got)
	}
	e.expect(422, "GET", "/market?category=car", "", nil)

	// near a point: nearest first, with distance; a radius drops the far ones and the unpinned
	nearby := list(e.expect(200, "GET", "/market?near_lat=6.40&near_lng=5.60", "", nil))
	if len(nearby) != 3 || nearby[0].(obj)["title"] != "Single mattress" || nearby[0].(obj)["distance_km"] == nil {
		t.Fatalf("nearest first: %v", nearby)
	}
	if got := list(e.expect(200, "GET", "/market?near_lat=6.40&near_lng=5.60&radius_km=5", "", nil)); len(got) != 1 {
		t.Fatalf("radius: %v", got)
	}

	// a school works as the starting point too
	var sid int
	if err := e.db.QueryRow(context.Background(), `SELECT id FROM schools WHERE latitude IS NOT NULL ORDER BY id LIMIT 1`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if got := list(e.expect(200, "GET", path("/market?school_id=%d", sid), "", nil)); len(got) != 3 {
		t.Fatalf("school origin: %d", len(got))
	}

	// the number shows on an item's own page only to signed-in people
	if g := e.expect(200, "GET", path("/market/%d", mid), "", nil).(obj); g["contact_phone"] != nil {
		t.Fatalf("guests must not see the number: %v", g)
	}
	if g := e.expect(200, "GET", path("/market/%d", mid), buyer, nil).(obj); g["contact_phone"] != "08031234567" || g["mine"] != false {
		t.Fatalf("signed-in buyer: %v", g)
	}

	// only the seller (or an admin) changes it; photos upload and delete
	e.expect(403, "PATCH", path("/market/%d", mid), buyer, item("Hijacked"))
	if code, _ := e.upload(path("/market/%d/photos", mid), buyer, "a.jpg", "image/jpeg", jpeg, nil); code != 403 {
		t.Fatalf("stranger upload: %d", code)
	}
	code, out := e.upload(path("/market/%d/photos", mid), seller, "a.jpg", "image/jpeg", jpeg, nil)
	if code != 200 || len(list(out.(obj)["photos"])) != 1 {
		t.Fatalf("seller upload: %d %v", code, out)
	}
	pid := list(out.(obj)["photos"])[0].(obj)["id"].(string)
	e.expect(200, "DELETE", path("/market/%d/photos/%s", mid, pid), seller, nil)
	e.expect(404, "DELETE", path("/market/%d/photos/%s", mid, pid), seller, nil)

	// marking sold takes it off the board; it can come back
	sold := item("Single mattress")
	sold["status"] = "sold"
	e.expect(200, "PATCH", path("/market/%d", mid), seller, sold)
	if n := len(list(e.expect(200, "GET", "/market", "", nil))); n != 2 {
		t.Fatalf("sold item still listed: %d", n)
	}
	if mine := list(e.expect(200, "GET", "/market/mine", seller, nil)); len(mine) != 3 {
		t.Fatalf("seller still sees their own: %d", len(mine))
	}

	// reporting hides an item until an admin decides
	other := id(e.expect(201, "POST", "/market", seller, item("Suspicious fridge")).(obj))
	e.expect(400, "POST", "/reports", seller, obj{"target_type": "market_item", "target_id": other, "reason": "scam"}) // not your own
	rep := e.expect(201, "POST", "/reports", buyer, obj{"target_type": "market_item", "target_id": other, "reason": "scam"}).(obj)
	if rep["target_label"] != "Suspicious fridge" {
		t.Fatalf("report label: %v", rep)
	}
	e.expect(404, "GET", path("/market/%d", other), buyer, nil)  // hidden from others
	e.expect(200, "GET", path("/market/%d", other), seller, nil) // not from the seller
	e.expect(200, "PATCH", path("/reports/%d", id(rep)), admin, obj{"status": "dismissed"})
	e.expect(200, "GET", path("/market/%d", other), buyer, nil) // dismissed: back on the board
	rep2 := e.expect(201, "POST", "/reports", buyer, obj{"target_type": "market_item", "target_id": other, "reason": "fake"}).(obj)
	e.expect(200, "PATCH", path("/reports/%d", id(rep2)), admin, obj{"status": "resolved"})
	e.expect(404, "GET", path("/market/%d", other), seller, nil) // resolved: removed

	// deleting removes it for good
	e.expect(204, "DELETE", path("/market/%d", mid), seller, nil)
	e.expect(404, "GET", path("/market/%d", mid), seller, nil)

	// five a day is the limit
	for i := 0; i < 5; i++ {
		e.do("POST", "/market", buyer, item(path("Item %d", i)))
	}
	e.expect(429, "POST", "/market", buyer, item("One too many"))
}
