package api_test

import (
	"testing"
)

var jpeg = []byte("fake-bytes")

func f(v float64) *float64 { return &v }

func adminEnv(t *testing.T) (*env, string) {
	e := newEnv(t)
	token, user := e.signup("admin5@gmail.com")
	e.makeAdmin(id(user))
	return e, token
}

// ---- listings (tests/test_listing_photos.py) ----

func TestCreateListingRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("plain5@gmail.com")
	e.expect(403, "POST", "/listings", token, obj{"title": "Test place", "type": "Studio", "price_per": 500, "total": 500, "deposit": 500, "walk": 10})
	e.expect(401, "POST", "/listings", "", obj{})
}

func TestCreateListing(t *testing.T) {
	e, token := adminEnv(t)
	out := e.expect(201, "POST", "/listings", token, obj{
		"title": "Riverside Studio", "type": "Studio", "price_per": 700, "total": 700, "deposit": 700, "walk": 12, "emoji": "🏙️",
	}).(obj)
	if out["title"] != "Riverside Studio" || out["is_hidden"] != false || len(list(out["photos"])) != 0 {
		t.Fatalf("got %v", out)
	}
	// defaults from ListingCreate
	if num(out, "utilities_monthly") != 45 || out["landlord_name"] != "Sandra" || num(out, "landlord_response_rate") != 95 ||
		out["grad"] != "from-coral to-sunshine" || out["landlord_verified"] != true {
		t.Fatalf("defaults wrong: %v", out)
	}
	if out["distance_km"] != nil || out["latitude"] != nil {
		t.Fatalf("got %v", out)
	}
	found := false
	for _, l := range list(e.expect(200, "GET", "/listings", "", nil)) {
		found = found || l.(obj)["title"] == "Riverside Studio"
	}
	if !found {
		t.Fatal("created listing not listed")
	}
	e.expect(422, "POST", "/listings", token, obj{"title": "no price"})
}

func TestListingSearchFilters(t *testing.T) {
	e := newEnv(t)
	e.exec(`INSERT INTO listings (title, type, price_per, total, utilities_monthly, deposit, walk, rating, reviews, verified, hot, video, is_hidden, grad, emoji, amenities, photos, landlord_name, landlord_verified, landlord_response_rate)
		VALUES ('Cheap Studio', 'Studio', 300, 300, 45, 300, 5, 0, 0, false, false, false, false, 'g', 'e', '[]', '[]', 'S', true, 95),
		       ('Pricey 2BR', '2BR', 900, 1800, 45, 900, 25, 0, 0, false, false, false, false, 'g', 'e', '[]', '[]', 'S', true, 95)`)
	titles := func(q string) []string {
		var out []string
		for _, l := range list(e.expect(200, "GET", "/listings"+q, "", nil)) {
			out = append(out, l.(obj)["title"].(string))
		}
		return out
	}
	if got := titles("?max_price=500"); len(got) != 1 || got[0] != "Cheap Studio" {
		t.Errorf("max_price: %v", got)
	}
	if got := titles("?max_walk=10"); len(got) != 1 || got[0] != "Cheap Studio" {
		t.Errorf("max_walk: %v", got)
	}
	if got := titles("?room_type=2br"); len(got) != 1 || got[0] != "Pricey 2BR" {
		t.Errorf("room_type: %v", got)
	}
	if got := titles("?room_type=any"); len(got) != 2 {
		t.Errorf("room_type=any: %v", got)
	}
	if got := titles("?q=PRICEY"); len(got) != 1 || got[0] != "Pricey 2BR" {
		t.Errorf("q: %v", got)
	}
	if got := titles(""); len(got) != 2 {
		t.Errorf("all: %v", got)
	}
}

func TestGetListing(t *testing.T) {
	e := newEnv(t)
	listing := e.createListing()
	out := e.expect(200, "GET", path("/listings/%d", listing), "", nil).(obj)
	if out["title"] != "Cozy studio" || out["amenities"] == nil || out["photos"] == nil {
		t.Fatalf("got %v", out)
	}
	e.expect(404, "GET", "/listings/99999", "", nil)
}

func TestUploadPhotoRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	listing := e.createListing()
	code, _ := e.upload(path("/listings/%d/photos", listing), token, "photo.jpg", "image/jpeg", jpeg, nil)
	if code != 403 {
		t.Fatalf("got %d", code)
	}
}

func TestUploadAndDeletePhoto(t *testing.T) {
	e, token := adminEnv(t)
	e.photos.urlFmt, e.photos.idFmt = "https://res.cloudinary.com/test/fake%d.jpg", "fake%d"
	listing := e.createListing()

	code, out := e.upload(path("/listings/%d/photos", listing), token, "photo.jpg", "image/jpeg", jpeg, map[string]string{"alt": "Living room"})
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	photos := list(out.(obj)["photos"])
	if len(photos) != 1 {
		t.Fatalf("photos %v", photos)
	}
	photo := photos[0].(obj)
	if photo["url"] != "https://res.cloudinary.com/test/fake1.jpg" || photo["alt"] != "Living room" ||
		photo["public_id"] != "fake1" || len(photo["id"].(string)) != 36 || photo["uploaded_at"] == "" {
		t.Fatalf("photo %v", photo)
	}

	// a second photo is appended, not replaced
	code, out = e.upload(path("/listings/%d/photos", listing), token, "b.png", "image/png", jpeg, nil)
	if code != 200 || len(list(out.(obj)["photos"])) != 2 {
		t.Fatalf("%d %v", code, out)
	}

	out = e.expect(200, "DELETE", path("/listings/%d/photos/%s", listing, photo["id"]), token, nil)
	if remaining := list(out.(obj)["photos"]); len(remaining) != 1 || remaining[0].(obj)["public_id"] != "fake2" {
		t.Fatalf("remaining %v", remaining)
	}
	if len(e.photos.destroyed) != 1 || e.photos.destroyed[0] != "fake1" {
		t.Fatalf("destroyed %v", e.photos.destroyed)
	}
	e.expect(404, "DELETE", path("/listings/%d/photos/nope", listing), token, nil)
	e.expect(404, "DELETE", "/listings/99999/photos/x", token, nil)
}

func TestUploadRejectsBadContentType(t *testing.T) {
	e, token := adminEnv(t)
	listing := e.createListing()
	code, _ := e.upload(path("/listings/%d/photos", listing), token, "virus.exe", "application/octet-stream", []byte("not-an-image"), nil)
	if code != 400 {
		t.Fatalf("got %d", code)
	}
}

func TestUploadRejectsOversizeImage(t *testing.T) {
	e, token := adminEnv(t)
	listing := e.createListing()
	code, _ := e.upload(path("/listings/%d/photos", listing), token, "big.jpg", "image/jpeg", make([]byte, 8*1024*1024+1), nil)
	if code != 400 {
		t.Fatalf("got %d", code)
	}
}

func TestUploadWithoutStorageConfiguredIs503(t *testing.T) {
	e, token := adminEnv(t)
	e.photos.configured = false
	listing := e.createListing()
	code, _ := e.upload(path("/listings/%d/photos", listing), token, "photo.jpg", "image/jpeg", jpeg, nil)
	if code != 503 {
		t.Fatalf("got %d", code)
	}
}

func TestUploadMissingFileIs422AndUnknownListingIs404(t *testing.T) {
	e, token := adminEnv(t)
	listing := e.createListing()
	e.expect(422, "POST", path("/listings/%d/photos", listing), token, obj{})
	code, _ := e.upload("/listings/99999/photos", token, "photo.jpg", "image/jpeg", jpeg, nil)
	if code != 404 {
		t.Fatalf("got %d", code)
	}
}

// ---- avatars (tests/test_avatar_photo.py) ----

func avatarEnv(t *testing.T) *env {
	e := newEnv(t)
	e.photos.urlFmt, e.photos.idFmt = "https://res.cloudinary.com/test/avatar%d.jpg", "avatar%d"
	return e
}

func TestUploadAvatarSetsURLAndKeepsEmojiFallback(t *testing.T) {
	e := avatarEnv(t)
	token, user := e.signup("student@gmail.com")
	if user["avatar_url"] != nil {
		t.Fatalf("avatar_url = %v", user["avatar_url"])
	}
	code, out := e.upload("/users/me/avatar", token, "photo.jpg", "image/jpeg", jpeg, nil)
	body := out.(obj)
	if code != 200 || body["avatar_url"] != "https://res.cloudinary.com/test/avatar1.jpg" || body["avatar_emoji"] == "" {
		t.Fatalf("%d %v", code, out)
	}
	if _, leaked := body["avatar_public_id"]; leaked {
		t.Fatal("avatar_public_id should not be exposed")
	}
}

func TestReuploadReplacesAndDeletesOldAsset(t *testing.T) {
	e := avatarEnv(t)
	token, _ := e.signup("student2@gmail.com")
	e.upload("/users/me/avatar", token, "photo.jpg", "image/jpeg", jpeg, nil)
	code, out := e.upload("/users/me/avatar", token, "photo.jpg", "image/jpeg", []byte("fake-bytes-2"), nil)
	if code != 200 || out.(obj)["avatar_url"] != "https://res.cloudinary.com/test/avatar2.jpg" {
		t.Fatalf("%d %v", code, out)
	}
	if len(e.photos.destroyed) != 1 || e.photos.destroyed[0] != "avatar1" {
		t.Fatalf("destroyed %v", e.photos.destroyed)
	}
}

func TestDeleteAvatarRevertsToEmojiFallback(t *testing.T) {
	e := avatarEnv(t)
	token, _ := e.signup("student3@gmail.com")
	e.upload("/users/me/avatar", token, "photo.jpg", "image/jpeg", jpeg, nil)
	out := e.expect(200, "DELETE", "/users/me/avatar", token, nil).(obj)
	if out["avatar_url"] != nil {
		t.Fatalf("got %v", out)
	}
	if len(e.photos.destroyed) != 1 || e.photos.destroyed[0] != "avatar1" {
		t.Fatalf("destroyed %v", e.photos.destroyed)
	}
}

func TestUploadAvatarRejectsBadContentType(t *testing.T) {
	e := avatarEnv(t)
	token, _ := e.signup("student4@gmail.com")
	code, _ := e.upload("/users/me/avatar", token, "virus.exe", "application/octet-stream", []byte("not-an-image"), nil)
	if code != 400 {
		t.Fatalf("got %d", code)
	}
}

func TestUploadAvatarRequiresAuth(t *testing.T) {
	e := avatarEnv(t)
	code, _ := e.upload("/users/me/avatar", "", "photo.jpg", "image/jpeg", jpeg, nil)
	if code != 401 {
		t.Fatalf("got %d", code)
	}
}

// ---- roommate banners (tests/test_roommate_banner.py) ----

func bannerEnv(t *testing.T) *env {
	e := newEnv(t)
	e.photos.urlFmt, e.photos.idFmt = "https://res.cloudinary.com/test/banner%d.jpg", "banner%d"
	return e
}

func TestUploadBannerRequiresAProfile(t *testing.T) {
	e := bannerEnv(t)
	token, user := e.signup("nopro@gmail.com")
	e.verifyUser(id(user))
	code, _ := e.upload("/roommates/me/banner", token, "banner.jpg", "image/jpeg", jpeg, nil)
	if code != 404 {
		t.Fatalf("got %d", code)
	}
}

func TestUploadAndDeleteBanner(t *testing.T) {
	e := bannerEnv(t)
	token, user := e.signup("banner1@gmail.com")
	e.verifyUser(id(user))
	e.createRoommateProfile(token)

	code, out := e.upload("/roommates/me/banner", token, "banner.jpg", "image/jpeg", jpeg, nil)
	if code != 200 || out.(obj)["banner_url"] != "https://res.cloudinary.com/test/banner1.jpg" {
		t.Fatalf("%d %v", code, out)
	}
	del := e.expect(200, "DELETE", "/roommates/me/banner", token, nil).(obj)
	if del["banner_url"] != nil {
		t.Fatalf("got %v", del)
	}
	if len(e.photos.destroyed) != 1 || e.photos.destroyed[0] != "banner1" {
		t.Fatalf("destroyed %v", e.photos.destroyed)
	}
	e.expect(404, "DELETE", "/roommates/me/banner", func() string { tk, _ := e.signup("noprofile@gmail.com"); return tk }(), nil)
}

func TestReuploadBannerDeletesOldAsset(t *testing.T) {
	e := bannerEnv(t)
	token, user := e.signup("banner2@gmail.com")
	e.verifyUser(id(user))
	e.createRoommateProfile(token)
	e.upload("/roommates/me/banner", token, "banner.jpg", "image/jpeg", jpeg, nil)
	code, out := e.upload("/roommates/me/banner", token, "banner.jpg", "image/jpeg", []byte("fake-bytes-2"), nil)
	if code != 200 || out.(obj)["banner_url"] != "https://res.cloudinary.com/test/banner2.jpg" {
		t.Fatalf("%d %v", code, out)
	}
	if len(e.photos.destroyed) != 1 || e.photos.destroyed[0] != "banner1" {
		t.Fatalf("destroyed %v", e.photos.destroyed)
	}
}

func TestRoommateReadIncludesUserAvatarURL(t *testing.T) {
	e := avatarEnv(t)
	token, user := e.signup("avatarcheck@gmail.com")
	e.verifyUser(id(user))
	profile := e.createRoommateProfile(token)

	out := e.expect(200, "GET", path("/roommates/%d", profile), "", nil).(obj)
	if v, ok := out["user_avatar_url"]; !ok || v != nil {
		t.Fatalf("user_avatar_url = %v (present=%v), want null", v, ok)
	}
	e.upload("/users/me/avatar", token, "photo.jpg", "image/jpeg", jpeg, nil)
	out = e.expect(200, "GET", path("/roommates/%d", profile), "", nil).(obj)
	if out["user_avatar_url"] != "https://res.cloudinary.com/test/avatar1.jpg" {
		t.Fatalf("got %v", out["user_avatar_url"])
	}
}

// ---- places (tests/test_places.py) ----

var campus = [2]float64{6.5158, 3.3898} // University of Lagos, Akoka

func names(v any) []string {
	out := []string{}
	for _, p := range list(v) {
		out = append(out, p.(obj)["name"].(string))
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPlacesSearchByCategoryAndProximity(t *testing.T) {
	e := newEnv(t)
	e.createPlace("Far Pharmacy", "pharmacy", 6.5500, 3.4200)
	e.createPlace("Near Pharmacy", "pharmacy", 6.5165, 3.3905)
	e.createPlace("Akoka Hospital", "hospital", 6.5160, 3.3900)

	out := e.expect(200, "GET", path("/places?category=pharmacy&near_lat=%v&near_lng=%v", campus[0], campus[1]), "", nil)
	if got := names(out); !equal(got, []string{"Near Pharmacy", "Far Pharmacy"}) {
		t.Fatalf("got %v", got)
	}
	rows := list(out)
	if rows[0].(obj)["distance_km"].(float64) >= rows[1].(obj)["distance_km"].(float64) {
		t.Fatal("distances not ascending")
	}
}

func TestPlacesSearchByName(t *testing.T) {
	e := newEnv(t)
	e.createPlace("GTBank Akoka", "bank", campus[0], campus[1])
	e.createPlace("Access Bank Yaba", "bank", 6.5098, 3.3790)
	if got := names(e.expect(200, "GET", "/places?q=gtbank", "", nil)); !equal(got, []string{"GTBank Akoka"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPlacesRadiusFilter(t *testing.T) {
	e := newEnv(t)
	e.createPlace("Inside", "market", 6.5165, 3.3905)
	e.createPlace("Outside", "market", 6.7000, 3.5000)
	out := e.expect(200, "GET", path("/places?near_lat=%v&near_lng=%v&radius_km=3", campus[0], campus[1]), "", nil)
	if got := names(out); !equal(got, []string{"Inside"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPlacesWithoutProximityReturnsAllUnsorted(t *testing.T) {
	e := newEnv(t)
	e.createPlace("A", "gym", campus[0], campus[1])
	e.createPlace("B", "gym", 6.6, 3.5)
	out := e.expect(200, "GET", "/places", "", nil)
	if len(list(out)) != 2 {
		t.Fatalf("got %v", out)
	}
	for _, p := range list(out) {
		if p.(obj)["distance_km"] != nil {
			t.Fatalf("distance_km should be null: %v", p)
		}
	}
	// "any" category and radius without a point are ignored
	if len(list(e.expect(200, "GET", "/places?category=any&radius_km=1", "", nil))) != 2 {
		t.Fatal("category=any should match everything")
	}
}

func TestCreatePlaceRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("plain-place@gmail.com")
	e.expect(403, "POST", "/places", token, obj{"name": "X", "category": "Pharmacy", "latitude": 6.5, "longitude": 3.3})
}

func TestCreatePlaceLowercasesCategory(t *testing.T) {
	e, token := adminEnv(t)
	out := e.expect(201, "POST", "/places", token, obj{"name": "MedPlus", "category": "Pharmacy", "latitude": 6.5, "longitude": 3.3}).(obj)
	if out["category"] != "pharmacy" || out["emoji"] != "📍" || out["address"] != nil {
		t.Fatalf("got %v", out)
	}
	e.expect(422, "POST", "/places", token, obj{"name": "No coords", "category": "x"})
	e.expect(200, "GET", path("/places/%d", id(out)), "", nil)
	e.expect(404, "GET", "/places/99999", "", nil)
	e.expect(204, "DELETE", path("/places/%d", id(out)), token, nil)
	e.expect(204, "DELETE", path("/places/%d", id(out)), token, nil) // already gone: still 204
	e.expect(404, "GET", path("/places/%d", id(out)), "", nil)
}

// ---- proximity search on listings (tests/test_listing_distance.py) ----

func TestListingsSortedNearestFirst(t *testing.T) {
	e := newEnv(t)
	e.createListingAt("Far", f(6.5400), f(3.4100))
	e.createListingAt("Near", f(6.5170), f(3.3910))
	e.createListingAt("Middle", f(6.5250), f(3.3990))
	out := e.expect(200, "GET", path("/listings?near_lat=%v&near_lng=%v", campus[0], campus[1]), "", nil)
	if got := titlesOf(out); !equal(got, []string{"Near", "Middle", "Far"}) {
		t.Fatalf("got %v", got)
	}
	prev := -1.0
	for _, l := range list(out) {
		d, ok := l.(obj)["distance_km"].(float64)
		if !ok || d < prev {
			t.Fatalf("distances not populated/ascending: %v", out)
		}
		prev = d
	}
}

func titlesOf(v any) []string {
	out := []string{}
	for _, l := range list(v) {
		out = append(out, l.(obj)["title"].(string))
	}
	return out
}

func TestRadiusFiltersOutDistantListings(t *testing.T) {
	e := newEnv(t)
	e.createListingAt("Inside", f(6.5170), f(3.3910))
	e.createListingAt("Outside", f(6.6500), f(3.5000))
	out := e.expect(200, "GET", path("/listings?near_lat=%v&near_lng=%v&radius_km=5", campus[0], campus[1]), "", nil)
	if got := titlesOf(out); !equal(got, []string{"Inside"}) {
		t.Fatalf("got %v", got)
	}
}

func TestListingsWithoutCoordinatesExcludedFromProximitySearch(t *testing.T) {
	e := newEnv(t)
	e.createListingAt("Has coords", f(6.5170), f(3.3910))
	e.createListingAt("No coords", nil, nil)

	prox := e.expect(200, "GET", path("/listings?near_lat=%v&near_lng=%v", campus[0], campus[1]), "", nil)
	if got := titlesOf(prox); !equal(got, []string{"Has coords"}) {
		t.Fatalf("proximity: %v", got)
	}
	plain := e.expect(200, "GET", "/listings", "", nil)
	if len(list(plain)) != 2 {
		t.Fatalf("plain: %v", plain)
	}
	for _, l := range list(plain) {
		if l.(obj)["distance_km"] != nil {
			t.Fatalf("distance_km should be null on a plain search: %v", l)
		}
	}
}

// ---- CORS & health ----

func TestHealth(t *testing.T) {
	e := newEnv(t)
	resp, err := httpGet(e.srv.URL + "/health")
	if err != nil || resp != `{"status":"ok"}` {
		t.Fatalf("%q %v", resp, err)
	}
}
