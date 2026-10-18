package api_test

import (
	"strings"
	"testing"
)

func mapsFixture(t *testing.T) (e *env, token string, schoolID, listingID int) {
	e = newEnv(t)
	token, _ = e.signup("maps@gmail.com")
	unilag := schoolByName(t, e, "University of Lagos")
	lat, lng := unilag["latitude"].(float64), unilag["longitude"].(float64)
	return e, token, id(unilag), e.createListingAt("Near campus", f(lat+0.01), f(lng+0.01))
}

func TestMapsEndpointsRequireLogin(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/maps/status", "/maps/route?listing_id=1&school_id=1", "/maps/places/nearby?lat=6.5&lng=3.3", "/maps/places/x", "/maps/places/photo?name=x"} {
		e.expect(401, "GET", p, "", nil)
	}
}

func TestMapsStatus(t *testing.T) {
	e, token, _, _ := mapsFixture(t)
	if out := e.expect(200, "GET", "/maps/status", token, nil).(obj); out["enabled"] != true {
		t.Fatalf("got %v", out)
	}
	e.google.disable()
	if out := e.expect(200, "GET", "/maps/status", token, nil).(obj); out["enabled"] != false {
		t.Fatalf("got %v", out)
	}
}

func TestRouteUsesGoogleForRealWalkingDistance(t *testing.T) {
	e, token, school, listing := mapsFixture(t)
	out := e.expect(200, "GET", path("/maps/route?listing_id=%d&school_id=%d&mode=walk", listing, school), token, nil).(obj)
	if out["source"] != "google" || num(out, "distance_m") != 2300 || num(out, "duration_min") != 28 || out["distance_km"].(float64) != 2.3 {
		t.Fatalf("got %v", out)
	}
	if sl := out["straight_line_km"].(float64); sl < 1 || sl > 2 {
		t.Fatalf("straight line %v", sl)
	}
	url := out["directions_url"].(string)
	if !strings.HasPrefix(url, "https://www.google.com/maps/dir/?api=1&origin=") || !strings.HasSuffix(url, "travelmode=walking") {
		t.Fatalf("directions url %q", url)
	}
	if out["from"] != "Near campus" || out["to"] != "University of Lagos" {
		t.Fatalf("labels %v / %v", out["from"], out["to"])
	}
	// repeat call is served from cache: Google is billed once
	e.expect(200, "GET", path("/maps/route?listing_id=%d&school_id=%d&mode=walk", listing, school), token, nil)
	if n := e.google.called("route"); n != 1 {
		t.Fatalf("google routes called %d times, want 1 (cache)", n)
	}
}

func TestRouteFallsBackToLabelledEstimate(t *testing.T) {
	e, token, school, listing := mapsFixture(t)
	check := func(label string) {
		out := e.expect(200, "GET", path("/maps/route?listing_id=%d&school_id=%d&mode=walk", listing, school), token, nil).(obj)
		if out["source"] != "estimate" || out["distance_km"].(float64) <= out["straight_line_km"].(float64) || num(out, "duration_min") < 1 {
			t.Fatalf("%s: got %v", label, out)
		}
	}
	e.google.failRoutes = true
	check("google error")
	e.google.disable()
	check("not configured")
	drive := e.expect(200, "GET", path("/maps/route?listing_id=%d&school_id=%d&mode=drive", listing, school), token, nil).(obj)
	walk := e.expect(200, "GET", path("/maps/route?listing_id=%d&school_id=%d&mode=walk", listing, school), token, nil).(obj)
	if num(drive, "duration_min") >= num(walk, "duration_min") {
		t.Fatalf("driving should be faster than walking: %v vs %v", drive["duration_min"], walk["duration_min"])
	}
}

func TestRouteValidation(t *testing.T) {
	e, token, school, listing := mapsFixture(t)
	e.expect(400, "GET", "/maps/route", token, nil)
	e.expect(400, "GET", path("/maps/route?listing_id=%d&school_id=%d&mode=teleport", listing, school), token, nil)
	e.expect(404, "GET", path("/maps/route?listing_id=99999&school_id=%d", school), token, nil)
	e.expect(404, "GET", path("/maps/route?listing_id=%d&school_id=99999", listing), token, nil)
	noCoords := e.createListingAt("No coords", nil, nil)
	e.expect(422, "GET", path("/maps/route?listing_id=%d&school_id=%d", noCoords, school), token, nil)
	// raw coordinates work too
	out := e.expect(200, "GET", "/maps/route?from_lat=6.5&from_lng=3.3&to_lat=6.52&to_lng=3.39&mode=walk", token, nil).(obj)
	if out["source"] != "google" {
		t.Fatalf("got %v", out)
	}
}

func TestNearbyBusinessesFromGoogleWithBusinessInfo(t *testing.T) {
	e, token, school, _ := mapsFixture(t)
	out := e.expect(200, "GET", path("/maps/places/nearby?school_id=%d&type=pharmacy&radius_m=3000", school), token, nil).(obj)
	if out["source"] != "google" {
		t.Fatalf("got %v", out)
	}
	places := list(out["places"])
	if len(places) != 2 {
		t.Fatalf("places %v", places)
	}
	p := places[1].(obj) // not re-sorted by us: Google already ranks by distance
	near := places[0].(obj)
	if near["name"] != "Far Pharmacy" {
		near, p = p, near
	}
	for _, k := range []string{"rating", "user_rating_count", "open_now", "phone", "website", "maps_url", "photo_names", "address", "distance_km"} {
		if _, ok := p[k]; !ok {
			t.Fatalf("missing %s in %v", k, p)
		}
	}
	if p["name"] != "Near Pharmacy" || p["rating"].(float64) != 4.6 || p["open_now"] != true || p["phone"] != "0803 000 0000" ||
		p["website"] != "https://near.example" || len(list(p["photo_names"])) != 1 || p["distance_km"] == nil {
		t.Fatalf("business info wrong: %v", p)
	}
	e.expect(400, "GET", "/maps/places/nearby", token, nil)
}

func TestNearbyFallsBackToCuratedPlaces(t *testing.T) {
	e, token, school, _ := mapsFixture(t)
	e.google.disable()
	e.createPlace("Curated Pharmacy", "pharmacy", 6.5165, 3.3905)
	e.createPlace("Curated Bank", "bank", 6.5166, 3.3906)
	out := e.expect(200, "GET", path("/maps/places/nearby?school_id=%d&type=pharmacy", school), token, nil).(obj)
	places := list(out["places"])
	if out["source"] != "curated" || len(places) != 1 || places[0].(obj)["name"] != "Curated Pharmacy" || places[0].(obj)["distance_km"] == nil {
		t.Fatalf("got %v", out)
	}
	if !strings.Contains(places[0].(obj)["maps_url"].(string), "google.com/maps") {
		t.Fatalf("maps_url %v", places[0].(obj)["maps_url"])
	}
}

func TestPlaceDetailsAndPhoto(t *testing.T) {
	e, token, _, _ := mapsFixture(t)
	d := e.expect(200, "GET", "/maps/places/ChIJabc", token, nil).(obj)
	if d["place_id"] != "ChIJabc" || len(list(d["weekly_hours"])) != 2 || d["summary"] != "Friendly neighbourhood pharmacy." {
		t.Fatalf("got %v", d)
	}
	rev := list(d["reviews"])
	if len(rev) != 1 || rev[0].(obj)["author"] != "Ada" || rev[0].(obj)["rating"].(float64) != 5 {
		t.Fatalf("reviews %v", rev)
	}
	ph := e.expect(200, "GET", "/maps/places/photo?name=places/p-near/photos/abc&max_width=400", token, nil).(obj)
	if ph["url"] != "https://lh3.googleusercontent.com/photo123" || strings.Contains(ph["url"].(string), "key=") {
		t.Fatalf("photo url %v", ph)
	}
	e.expect(502, "GET", "/maps/places/photo?name=not-a-photo", token, nil)
	e.google.disable()
	e.expect(503, "GET", "/maps/places/ChIJabc", token, nil)
	e.expect(503, "GET", "/maps/places/photo?name=places/p/photos/a", token, nil)
}

// ---- schools pinned to exact Google locations ----

func TestPickingALowConfidenceSchoolRefinesItViaGoogle(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("refine@gmail.com")
	var target obj
	for _, x := range list(e.expect(200, "GET", "/schools?limit=500&state=Abia", "", nil)) {
		if x.(obj)["confidence"] == "very_low" {
			target = x.(obj)
			break
		}
	}
	if target == nil {
		t.Skip("no very_low school in Abia")
	}
	before := target["latitude"].(float64)
	e.google.textLat, e.google.textLng = 5.5, 7.5 // inside Nigeria, near Abia
	e.expect(200, "PATCH", "/users/me", token, obj{"school_id": id(target)})
	after := e.expect(200, "GET", path("/schools/%d", id(target)), "", nil).(obj)
	if after["source"] != "google" || after["confidence"] != "high" || after["google_place_id"] != "ChIJ-school" || after["latitude"].(float64) == before {
		t.Fatalf("not refined: %v", after)
	}
	// a second selection doesn't call Google again
	calls := e.google.called("searchText")
	e.expect(200, "PATCH", "/users/me", token, obj{"school_id": id(target)})
	if e.google.called("searchText") != calls {
		t.Fatal("refined school was re-searched")
	}
	// and a dataset reload must not undo the refinement
	e.exec(`DELETE FROM go_migrations WHERE name LIKE 'schools_data_%'`)
	if _, err := syncSchools(e); err != nil {
		t.Fatal(err)
	}
	still := e.expect(200, "GET", path("/schools/%d", id(target)), "", nil).(obj)
	if still["source"] != "google" || still["latitude"] != after["latitude"] {
		t.Fatalf("reload clobbered refinement: %v", still)
	}
}

func TestRefinementRejectsResultsOutsideNigeria(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("abroad@gmail.com")
	var target obj
	for _, x := range list(e.expect(200, "GET", "/schools?limit=500", "", nil)) {
		if x.(obj)["confidence"] == "low" {
			target = x.(obj)
			break
		}
	}
	e.google.textLat, e.google.textLng = 51.5, -0.12 // London
	e.expect(200, "PATCH", "/users/me", token, obj{"school_id": id(target)})
	after := e.expect(200, "GET", path("/schools/%d", id(target)), "", nil).(obj)
	if after["source"] == "google" {
		t.Fatalf("accepted a location outside Nigeria: %v", after)
	}
}

func TestSchoolsNotRefinedWhenGoogleIsOff(t *testing.T) {
	e := newEnv(t)
	e.google.disable()
	token, _ := e.signup("off@gmail.com")
	s := schoolByName(t, e, "University of Lagos")
	e.expect(200, "PATCH", "/users/me", token, obj{"school_id": id(s)})
	if e.google.called("searchText") != 0 {
		t.Fatal("called Google while disabled")
	}
}
