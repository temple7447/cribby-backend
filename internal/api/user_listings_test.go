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

// ---- walkthrough video ----

// mp4 is the smallest thing that passes the "ftyp" signature check.
var mp4 = append([]byte{0, 0, 0, 0x18}, []byte("ftypmp42\x00\x00\x00\x00mp42isom")...)

func TestOwnerUploadsReplacesAndRemovesVideo(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("filmer@gmail.com")
	lid := id(e.expect(201, "POST", "/listings", token, apartment("Video flat")).(obj))
	adminTok, admin := e.signup("vidboss@gmail.com")
	e.makeAdmin(id(admin))
	e.expect(200, "PATCH", path("/admin/listings/%d/review", lid), adminTok, obj{"decision": "approve"})

	code, out := e.upload(path("/listings/%d/video", lid), token, "tour.mp4", "video/mp4", mp4, nil)
	if code != 200 {
		t.Fatalf("upload: %d %v", code, out)
	}
	l := out.(obj)
	if l["video_url"] != "https://cdn.test/video/upload/v1/clip.mp4" || l["video"] != true {
		t.Fatalf("got video_url=%v video=%v", l["video_url"], l["video"])
	}
	if l["status"] != "pending" { // a change to an approved listing goes back for review
		t.Fatalf("status = %v", l["status"])
	}

	// replacing it deletes the old file from storage
	_, out = e.upload(path("/listings/%d/video", lid), token, "again.mov", "video/quicktime", mp4, nil)
	if out.(obj)["video_url"] != "https://cdn.test/video/upload/v2/clip.mp4" {
		t.Fatalf("got %v", out.(obj)["video_url"])
	}
	if len(e.photos.videosGone) != 1 || e.photos.videosGone[0] != "cribby/vid1" {
		t.Fatalf("old video not destroyed: %v", e.photos.videosGone)
	}

	// someone else can neither add nor remove it
	other, _ := e.signup("vid-thief@gmail.com")
	if code, _ := e.upload(path("/listings/%d/video", lid), other, "x.mp4", "video/mp4", mp4, nil); code != 403 {
		t.Fatalf("other upload: %d", code)
	}
	e.expect(403, "DELETE", path("/listings/%d/video", lid), other, nil)

	// the owner removes it
	l = e.expect(200, "DELETE", path("/listings/%d/video", lid), token, nil).(obj)
	if l["video_url"] != nil || l["video"] != false {
		t.Fatalf("got video_url=%v video=%v", l["video_url"], l["video"])
	}
	if len(e.photos.videosGone) != 2 || e.photos.videosGone[1] != "cribby/vid2" {
		t.Fatalf("destroyed: %v", e.photos.videosGone)
	}
	e.expect(404, "DELETE", path("/listings/%d/video", lid), token, nil) // nothing left to remove
}

func TestVideoValidation(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("badvid@gmail.com")
	lid := id(e.expect(201, "POST", "/listings", token, apartment("Bad video")).(obj))
	up := func(name, ctype string, data []byte) int {
		code, _ := e.upload(path("/listings/%d/video", lid), token, name, ctype, data, nil)
		return code
	}
	if c := up("pic.jpg", "image/jpeg", jpeg); c != 400 { // an image is not a video
		t.Fatalf("image as video: %d", c)
	}
	if c := up("fake.mp4", "video/mp4", []byte("this is just text, not a video at all")); c != 400 { // header lies
		t.Fatalf("fake mp4: %d", c)
	}
	big := make([]byte, 50*1024*1024+1)
	copy(big, mp4)
	if c := up("huge.mp4", "video/mp4", big); c != 400 {
		t.Fatalf("oversize video: %d", c)
	}
	if c := up("ok.mp4", "video/mp4", mp4); c != 200 {
		t.Fatalf("valid video: %d", c)
	}
}
