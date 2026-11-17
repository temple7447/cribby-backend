package api_test

import (
	"context"
	"testing"
)

func vendor(name, category string, lat, lng float64) obj {
	return obj{"name": name, "category": category, "phone": "0803 123 4567", "address": "Ugbowo", "latitude": lat, "longitude": lng, "description": "Same-day service"}
}

func TestVendorsFeaturedSpotsAndDemand(t *testing.T) {
	e := newEnv(t)
	student, _ := e.signup("student@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	// a campus to measure from
	var sid int
	var lat, lng float64
	if err := e.db.QueryRow(context.Background(), `SELECT id, latitude, longitude FROM schools WHERE latitude IS NOT NULL AND name LIKE '%Benin%' ORDER BY id LIMIT 1`).Scan(&sid, &lat, &lng); err != nil {
		t.Fatal(err)
	}

	// anyone signed in can list a business, but it waits for an admin
	e.expect(401, "POST", "/vendors", "", vendor("Quick Wash", "laundry", lat, lng))
	e.expect(422, "POST", "/vendors", student, vendor("Quick Wash", "spaceships", lat, lng))
	e.expect(422, "POST", "/vendors", student, func() obj { v := vendor("Quick Wash", "laundry", lat, lng); v["phone"] = "abc"; return v }())
	mine := e.expect(201, "POST", "/vendors", student, func() obj { v := vendor("Quick Wash", "laundry", lat+0.002, lng); v["student_run"] = true; return v }()).(obj)
	if mine["status"] != "pending" || mine["student_run"] != true || mine["featured"] != false {
		t.Fatalf("submitted: %v", mine)
	}
	vid := id(mine)
	if got := list(e.expect(200, "GET", "/vendors", "", nil)); len(got) != 0 {
		t.Fatalf("pending vendors leaked: %v", got)
	}
	if got := list(e.expect(200, "GET", "/vendors/mine", student, nil)); len(got) != 1 {
		t.Fatalf("my vendors: %v", got)
	}
	e.expect(403, "PATCH", path("/admin/vendors/%d/review", vid), student, obj{"decision": "approve"})
	e.expect(422, "PATCH", path("/admin/vendors/%d/review", vid), admin, obj{"decision": "reject"}) // needs a reason
	e.expect(200, "PATCH", path("/admin/vendors/%d/review", vid), admin, obj{"decision": "approve"})

	// the admin adds a paid, featured vendor directly: it ranks first even though it's farther away
	paid := vendor("Campus Eats", "food", lat+0.02, lng)
	paid["featured_days"] = 30
	paidV := e.expect(201, "POST", "/vendors", admin, paid).(obj)
	if paidV["status"] != "approved" || paidV["featured"] != true {
		t.Fatalf("admin-created: %v", paidV)
	}
	e.expect(201, "POST", "/vendors", admin, vendor("Near Print", "printing", lat+0.001, lng))
	near := list(e.expect(200, "GET", path("/vendors?school_id=%d", sid), "", nil))
	if len(near) != 3 || near[0].(obj)["name"] != "Campus Eats" || near[0].(obj)["featured"] != true {
		t.Fatalf("featured first: %v", near)
	}
	if near[1].(obj)["name"] != "Near Print" || near[1].(obj)["distance_km"] == nil {
		t.Fatalf("then nearest: %v", near)
	}
	if got := list(e.expect(200, "GET", path("/vendors?school_id=%d&category=laundry", sid), "", nil)); len(got) != 1 || got[0].(obj)["student_run"] != true {
		t.Fatalf("category: %v", got)
	}
	e.expect(422, "GET", "/vendors?category=spaceships", "", nil)

	// featuring can be changed or removed, and expires on its own
	e.expect(403, "PUT", path("/admin/vendors/%d/featured", vid), student, obj{"days": 7})
	e.expect(422, "PUT", path("/admin/vendors/%d/featured", vid), admin, obj{"days": 999})
	if got := e.expect(200, "PUT", path("/admin/vendors/%d/featured", vid), admin, obj{"days": 7}).(obj); got["featured"] != true {
		t.Fatalf("feature: %v", got)
	}
	if got := e.expect(200, "PUT", path("/admin/vendors/%d/featured", vid), admin, obj{"days": 0}).(obj); got["featured"] != false {
		t.Fatalf("unfeature: %v", got)
	}
	e.exec(`UPDATE vendors SET featured_until = ? WHERE id = ?`, past(), id(paidV))
	if first := list(e.expect(200, "GET", path("/vendors?school_id=%d", sid), "", nil))[0].(obj); first["name"] == "Campus Eats" {
		t.Fatalf("an expired feature must stop ranking first: %v", first["name"])
	}

	// limit: two submissions a day for non-admins
	e.expect(201, "POST", "/vendors", student, vendor("Second", "snacks", lat, lng))
	e.expect(429, "POST", "/vendors", student, vendor("Third", "snacks", lat, lng))

	// searches near the campus are logged and show up as demand
	e.expect(200, "GET", path("/maps/places/nearby?lat=%f&lng=%f&type=laundry", lat, lng), "", nil)
	e.expect(200, "GET", path("/maps/places/nearby?lat=%f&lng=%f&type=laundry", lat, lng), "", nil)
	e.expect(200, "GET", path("/maps/places/search?q=Printing&lat=%f&lng=%f", lat, lng), "", nil)
	e.expect(403, "GET", "/admin/demand", student, nil)
	d := e.expect(200, "GET", "/admin/demand?days=30", admin, nil).(obj)
	top := list(d["top"])
	if len(top) == 0 || top[0].(obj)["label"] != "laundry" || num(top[0].(obj), "count") < 2 {
		t.Fatalf("top searches: %v", d)
	}
	camps := list(d["campuses"])
	if len(camps) == 0 || num(camps[0].(obj), "school_id") != sid || len(list(camps[0].(obj)["top"])) == 0 {
		t.Fatalf("demand by campus: %v", d)
	}
}
