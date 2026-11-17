package api_test

import (
	"strings"
	"testing"
)

// apartment returns a valid listing body. Each title gets its own address and spot, so two
// different titles are never mistaken for the same home by duplicate detection.
func apartment(title string) obj {
	sum := 0
	for _, c := range title {
		sum += int(c)
	}
	off := float64(sum%500) * 0.001
	return obj{
		"title": title, "type": "Self-contain", "price_per": 350000, "deposit": 50000, "utilities_monthly": 5000,
		"latitude": 6.5180 + off, "longitude": 3.3910 + off, "address": "12 Akoka Rd, Yaba (" + title + ")", "description": "Quiet, 24h light",
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
	first := id(e.expect(201, "POST", "/listings", token, apartment("Flat number 0")).(obj))
	e.exec(`UPDATE listings SET status = 'approved' WHERE id = ?`, first) // lifts the new-poster limit; the daily cap remains
	for i := 1; i < 5; i++ {
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

func TestVideoLongerThanOneMinuteIsRejectedAndDeleted(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("longfilm@gmail.com")
	lid := id(e.expect(201, "POST", "/listings", token, apartment("Long video")).(obj))
	up := func() (int, any) {
		return e.upload(path("/listings/%d/video", lid), token, "tour.mp4", "video/mp4", mp4, nil)
	}
	e.photos.videoSeconds = 75
	if code, _ := up(); code != 400 {
		t.Fatalf("75s video: %d, want 400", code)
	}
	if len(e.photos.videosGone) != 1 { // not left behind in storage
		t.Fatalf("over-length video not deleted: %v", e.photos.videosGone)
	}
	if l := e.expect(200, "GET", path("/listings/%d", lid), token, nil).(obj); l["video_url"] != nil {
		t.Fatalf("over-length video was attached: %v", l["video_url"])
	}
	e.photos.videoSeconds = 60.04 // "1:00" with rounding noise is fine
	if code, _ := up(); code != 200 {
		t.Fatalf("60s video: %d, want 200", code)
	}
	e.photos.videoSeconds = 0 // unknown length is allowed (the size cap still applies)
	if code, _ := up(); code != 200 {
		t.Fatalf("unknown-length video: %d, want 200", code)
	}
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

func TestNewPostersAreLimitedAndVerifiedLandlordsGoLive(t *testing.T) {
	e := newEnv(t)
	e.photos.configured, e.photos.urlFmt, e.photos.idFmt = true, "https://cdn.test/p%d.jpg", "cribby/p%d"
	token, user := e.signup("newbie@gmail.com")

	// a brand-new poster waits for review and can add 2 a day
	first := e.expect(201, "POST", "/listings", token, apartment("Flat One")).(obj)
	if first["status"] != "pending" {
		t.Fatalf("new poster should wait for review: %v", first["status"])
	}
	e.expect(201, "POST", "/listings", token, apartment("Flat Two"))
	e.expect(429, "POST", "/listings", token, apartment("Flat Three"))

	// once an admin has approved one, the limit lifts
	e.exec(`UPDATE listings SET status = 'approved' WHERE id = ?`, id(first))
	e.expect(201, "POST", "/listings", token, apartment("Flat Three"))

	// a verified landlord's listing is live at once, and editing it keeps it live
	e.exec(`INSERT INTO landlord_verifications (user_id, status, phone, id_type, submitted_at) VALUES (?, 'verified', '08031234567', 'nin', ?)`, id(user), past())
	live := e.expect(201, "POST", "/listings", token, apartment("Flat Four")).(obj)
	if live["status"] != "approved" {
		t.Fatalf("verified landlord should go live: %v", live["status"])
	}
	edited := e.expect(200, "PATCH", path("/listings/%d", id(live)), token, obj{"title": "Four, renamed", "type": "Self-contain", "price_per": 350000, "deposit": 50000, "utilities_monthly": 5000, "latitude": 6.5, "longitude": 3.4, "address": "somewhere else", "description": "x"}).(obj)
	if edited["status"] != "approved" {
		t.Fatalf("a verified landlord's edit stays live: %v", edited["status"])
	}
}

func TestDuplicateListingsAreBlockedOrFlagged(t *testing.T) {
	e := newEnv(t)
	a, _ := e.signup("a@gmail.com")
	b, _ := e.signup("b@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	orig := id(e.expect(201, "POST", "/listings", a, apartment("Original")).(obj))
	// the same person posting the same home again is stopped
	e.expect(409, "POST", "/listings", a, apartment("Original"))
	// someone else posting it (same address, different spacing and case) is held and flagged
	copyOf := apartment("Original")
	copyOf["address"] = "  12 AKOKA rd,   Yaba (Original) "
	dup := id(e.expect(201, "POST", "/listings", b, copyOf).(obj))
	var flagged any
	for _, it := range list(e.expect(200, "GET", "/admin/listings?status=pending", admin, nil)) {
		if id(it.(obj)) == dup {
			flagged = it.(obj)["duplicate_of"]
		}
	}
	if flagged == nil || int(flagged.(float64)) != orig {
		t.Fatalf("duplicate should point at #%d, got %v", orig, flagged)
	}
}

func TestOnlyVerifiersMarkHomesVerifiedAndMovingClearsIt(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.signup("owner@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	body := apartment("Checked flat")
	l := e.expect(201, "POST", "/listings", owner, body).(obj)
	lid := id(l)
	if l["verified"] != false {
		t.Fatalf("new listings start unverified: %v", l["verified"])
	}
	url := path("/listings/%d/verified", lid)
	e.expect(401, "PATCH", url, "", obj{"verified": true})
	e.expect(403, "PATCH", url, owner, obj{"verified": true}) // you can't verify your own home
	if got := e.expect(200, "PATCH", url, admin, obj{"verified": true}).(obj); got["verified"] != true {
		t.Fatalf("admin verify: %v", got["verified"])
	}
	e.expect(404, "PATCH", path("/listings/%d/verified", 99999), admin, obj{"verified": true})

	// fixing a typo in the description keeps the mark; moving the home removes it
	same := apartment("Checked flat")
	same["description"] = "Quiet, 24h light, new gate"
	if got := e.expect(200, "PATCH", path("/listings/%d", lid), owner, same).(obj); got["verified"] != true {
		t.Fatalf("same place keeps the mark: %v", got["verified"])
	}
	moved := apartment("Checked flat")
	moved["address"] = "99 Somewhere Else"
	if got := e.expect(200, "PATCH", path("/listings/%d", lid), owner, moved).(obj); got["verified"] != false {
		t.Fatalf("a new address must be checked again: %v", got["verified"])
	}
}

func TestVerifiersCanCheckHomesButNotTheirOwn(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.signup("owner@gmail.com")
	cousin, cousinUser := e.signup("cousin@gmail.com")
	other, _ := e.signup("other@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))
	cid := id(cousinUser)

	// nobody is a verifier until an admin says so
	if got := e.expect(200, "GET", "/verifier/me", cousin, nil).(obj); got["is_verifier"] != false {
		t.Fatalf("not yet: %v", got)
	}
	e.expect(403, "GET", "/verifier/queue", cousin, nil)
	e.expect(403, "PUT", path("/admin/verifiers/%d", cid), cousin, obj{"kind": "cousin"}) // can't promote yourself
	e.expect(422, "PUT", path("/admin/verifiers/%d", cid), admin, obj{"kind": "king"})
	e.expect(404, "PUT", path("/admin/verifiers/%d", 99999), admin, obj{"kind": "cousin"})
	vs := list(e.expect(200, "PUT", path("/admin/verifiers/%d", cid), admin, obj{"kind": "cousin", "campus": " UNIBEN "}))
	if len(vs) != 1 || vs[0].(obj)["campus"] != "UNIBEN" {
		t.Fatalf("verifiers: %v", vs)
	}
	if got := e.expect(200, "GET", "/verifier/me", cousin, nil).(obj); got["is_verifier"] != true {
		t.Fatalf("now a verifier: %v", got)
	}

	// a live listing from someone else is in the queue; the cousin's own is not
	e.exec(`UPDATE listings SET status = 'approved' WHERE 1 = 1`)
	theirs := id(e.expect(201, "POST", "/listings", owner, apartment("Owner flat")).(obj))
	mine := id(e.expect(201, "POST", "/listings", cousin, apartment("Cousin flat")).(obj))
	e.exec(`UPDATE listings SET status = 'approved' WHERE id IN (?, ?)`, theirs, mine)
	queue := list(e.expect(200, "GET", "/verifier/queue", cousin, nil))
	if len(queue) != 1 || id(queue[0].(obj)) != theirs {
		t.Fatalf("queue: %v", queue)
	}
	e.expect(403, "PATCH", path("/listings/%d/verified", mine), cousin, obj{"verified": true}) // not your own
	e.expect(403, "PATCH", path("/listings/%d/verified", theirs), other, obj{"verified": true})
	e.expect(200, "PATCH", path("/listings/%d/verified", theirs), cousin, obj{"verified": true})
	e.expect(200, "PATCH", path("/listings/%d/verified", theirs), cousin, obj{"verified": true}) // twice counts once
	// un-verifying and re-verifying the same home must not pad the count either
	e.expect(200, "PATCH", path("/listings/%d/verified", theirs), cousin, obj{"verified": false})
	e.expect(200, "PATCH", path("/listings/%d/verified", theirs), cousin, obj{"verified": true})
	vs = list(e.expect(200, "GET", "/admin/verifiers", admin, nil))
	if num(vs[0].(obj), "verified_count") != 1 {
		t.Fatalf("count: %v", vs[0])
	}
	if q := list(e.expect(200, "GET", "/verifier/queue", cousin, nil)); len(q) != 0 {
		t.Fatalf("queue should be empty: %v", q)
	}

	// removing the role takes the powers away
	e.expect(204, "DELETE", path("/admin/verifiers/%d", cid), admin, nil)
	e.expect(403, "PATCH", path("/listings/%d/verified", theirs), cousin, obj{"verified": false})
}

func TestChecklistKeepsThePhonePrivateAndCatchesRepostedNumbers(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.signup("cousin@gmail.com")
	agent, _ := e.signup("agent@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))
	a := id(e.expect(201, "POST", "/listings", owner, apartment("Cousin find")).(obj))
	b := id(e.expect(201, "POST", "/listings", agent, apartment("Agent repost")).(obj))
	e.exec(`UPDATE listings SET status = 'approved' WHERE id IN (?, ?)`, a, b)

	check := obj{"condition": "good", "water": "borehole", "power": "most_of_day", "permission": true, "contact_phone": "0803 123 4567"}
	e.expect(401, "PUT", path("/listings/%d/checklist", a), "", check)
	e.expect(403, "PUT", path("/listings/%d/checklist", a), agent, check) // not your listing
	e.expect(422, "PUT", path("/listings/%d/checklist", a), owner, obj{"water": "ocean"})
	e.expect(422, "PUT", path("/listings/%d/checklist", a), owner, obj{"contact_phone": "abc"})
	got := e.expect(200, "PUT", path("/listings/%d/checklist", a), owner, check).(obj)
	if c := got["checklist"].(obj); c["water"] != "borehole" || c["contact_phone"] != "08031234567" {
		t.Fatalf("owner sees it all: %v", c)
	}

	// everyone can read the findings; only the owner and admins get the number
	pub := e.expect(200, "GET", path("/listings/%d", a), "", nil).(obj)["checklist"].(obj)
	if pub["condition"] != "good" || pub["permission"] != true {
		t.Fatalf("public findings: %v", pub)
	}
	if _, leaked := pub["contact_phone"]; leaked {
		t.Fatalf("the landlord's number must stay private: %v", pub)
	}
	if c := e.expect(200, "GET", path("/listings/%d", a), admin, nil).(obj)["checklist"].(obj); c["contact_phone"] != "08031234567" {
		t.Fatalf("admin sees the number: %v", c)
	}
	if strings.Contains(toJSON(e.expect(200, "GET", "/listings", "", nil)), "contact_phone") {
		t.Fatal("lists must not carry checklists")
	}

	// the same number on someone else's home is held for an admin
	e.expect(200, "PUT", path("/listings/%d/checklist", b), agent, check)
	if n := e.count(`SELECT COUNT(*) FROM listings WHERE id = ? AND status = 'pending'`, b); n != 1 {
		t.Fatal("the repost should go back to review")
	}
	if n := e.count(`SELECT COUNT(*) FROM listing_flags WHERE listing_id = ? AND duplicate_of = ?`, b, a); n != 1 {
		t.Fatal("the repost should be flagged against the original")
	}
	// ...but one landlord with several flats is fine
	c2 := id(e.expect(201, "POST", "/listings", owner, apartment("Second flat")).(obj))
	e.exec(`UPDATE listings SET status = 'approved' WHERE id = ?`, c2)
	e.expect(200, "PUT", path("/listings/%d/checklist", c2), owner, check)
	if n := e.count(`SELECT COUNT(*) FROM listings WHERE id = ? AND status = 'approved'`, c2); n != 1 {
		t.Fatal("same owner, same number is normal")
	}
}

func TestVerifierTiersFollowHomesChecked(t *testing.T) {
	e := newEnv(t)
	cousin, cu := e.signup("cousin@gmail.com")
	admin, au := e.signup("boss@gmail.com")
	e.makeAdmin(id(au))
	e.expect(200, "PUT", path("/admin/verifiers/%d", id(cu)), admin, obj{"kind": "cousin"})
	for _, c := range []struct {
		count int
		tier  string
		next  int
	}{{0, "starter", 5}, {4, "starter", 5}, {5, "bronze", 15}, {14, "bronze", 15}, {15, "silver", 40}, {39, "silver", 40}, {40, "gold", 0}} {
		e.exec(`UPDATE verifiers SET verified_count = ? WHERE user_id = ?`, c.count, id(cu))
		me := e.expect(200, "GET", "/verifier/me", cousin, nil).(obj)
		if me["tier"] != c.tier || num(me, "next_tier_at") != c.next || num(me, "verified_count") != c.count {
			t.Fatalf("count %d: %v", c.count, me)
		}
		if got := list(e.expect(200, "GET", "/admin/verifiers", admin, nil))[0].(obj); got["tier"] != c.tier {
			t.Fatalf("admin list count %d: %v", c.count, got)
		}
	}
	// people who aren't verifiers get no tier
	plain, _ := e.signup("plain@gmail.com")
	if me := e.expect(200, "GET", "/verifier/me", plain, nil).(obj); me["is_verifier"] != false || me["tier"] != "" {
		t.Fatalf("plain user: %v", me)
	}
}

func TestBoostedListingsRankFirstUntilTheyExpire(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.signup("owner@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))
	first := id(e.expect(201, "POST", "/listings", owner, apartment("Plain flat")).(obj))
	second := id(e.expect(201, "POST", "/listings", owner, apartment("Paid flat")).(obj))
	e.exec(`UPDATE listings SET status = 'approved'`)

	names := func(q string) []string {
		var out []string
		for _, x := range list(e.expect(200, "GET", "/listings"+q, "", nil)) {
			out = append(out, x.(obj)["title"].(string))
		}
		return out
	}
	if got := names(""); got[0] != "Plain flat" {
		t.Fatalf("by default the older listing is first: %v", got)
	}

	e.expect(401, "PUT", path("/admin/listings/%d/boost", second), "", obj{"days": 30})
	e.expect(403, "PUT", path("/admin/listings/%d/boost", second), owner, obj{"days": 30}) // you can't give yourself the spot
	e.expect(422, "PUT", path("/admin/listings/%d/boost", second), admin, obj{"days": 999})
	e.expect(404, "PUT", path("/admin/listings/%d/boost", 99999), admin, obj{"days": 30})
	got := e.expect(200, "PUT", path("/admin/listings/%d/boost", second), admin, obj{"days": 30}).(obj)
	if got["boosted"] != true {
		t.Fatalf("boosted: %v", got)
	}
	if n := names(""); n[0] != "Paid flat" {
		t.Fatalf("boosted listing should rank first: %v", n)
	}
	if n := names("?near_lat=6.5&near_lng=3.4"); n[0] != "Paid flat" {
		t.Fatalf("boosted listing should rank first near a point too: %v", n)
	}
	if one := e.expect(200, "GET", path("/listings/%d", second), "", nil).(obj); one["boosted"] != true {
		t.Fatalf("detail: %v", one)
	}
	if one := e.expect(200, "GET", path("/listings/%d", first), "", nil).(obj); one["boosted"] != false {
		t.Fatalf("unboosted: %v", one)
	}

	// it runs out on its own
	e.exec(`UPDATE listing_boosts SET boosted_until = ? WHERE listing_id = ?`, past(), second)
	if n := names(""); n[0] != "Plain flat" {
		t.Fatalf("an expired boost must stop ranking first: %v", n)
	}
	// and can be ended early
	e.expect(200, "PUT", path("/admin/listings/%d/boost", second), admin, obj{"days": 7})
	e.expect(200, "PUT", path("/admin/listings/%d/boost", second), admin, obj{"days": 0})
	if one := e.expect(200, "GET", path("/listings/%d", second), "", nil).(obj); one["boosted"] != false {
		t.Fatalf("ended: %v", one)
	}
}

func TestAdvancedListingFilters(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.signup("owner@gmail.com")
	admin, adminUser := e.signup("boss@gmail.com")
	e.makeAdmin(id(adminUser))

	post := func(title string, amenities []string) int {
		b := apartment(title)
		b["amenities"] = amenities
		return id(e.expect(201, "POST", "/listings", owner, b).(obj))
	}
	a := post("Borehole flat", []string{"🛡️ Security", "🪑 Furnished", "💧 Water", "❄️ A/C"})
	b := post("Tap flat", []string{"Security"})
	e.exec(`UPDATE listings SET status = 'approved'`) // a first approved home lifts the new-poster limit
	c := post("Bare flat", []string{"📶 Wi-Fi"})
	_ = post("Unchecked flat", []string{"🛡️ Security", "🪑 Furnished"})
	e.exec(`UPDATE listings SET status = 'approved'`)
	e.expect(200, "PUT", path("/listings/%d/checklist", a), owner, obj{"water": "borehole", "power": "always"})
	e.expect(200, "PUT", path("/listings/%d/checklist", b), owner, obj{"water": "tap", "power": "most_of_day"})
	e.expect(200, "PUT", path("/listings/%d/checklist", c), owner, obj{"water": "none", "power": "few_hours"})
	e.expect(200, "PATCH", path("/listings/%d/verified", a), admin, obj{"verified": true})
	e.exec(`UPDATE listings SET landlord_verified = ? WHERE id = ?`, true, b)

	titles := func(q string) []string {
		var out []string
		for _, x := range list(e.expect(200, "GET", "/listings"+q, "", nil)) {
			out = append(out, x.(obj)["title"].(string))
		}
		return out
	}
	same := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		seen := map[string]bool{}
		for _, g := range got {
			seen[g] = true
		}
		for _, w := range want {
			if !seen[w] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}

	same(titles(""), "Borehole flat", "Tap flat", "Bare flat", "Unchecked flat")
	// amenities match by words, ignore emoji and case, and every one asked for must be there
	same(titles("?amenity=security"), "Borehole flat", "Tap flat", "Unchecked flat")
	same(titles("?amenity=%F0%9F%9B%A1%EF%B8%8F%20Security&amenity=furnished"), "Borehole flat", "Unchecked flat")
	same(titles("?amenity=wi-fi"), "Bare flat")
	same(titles("?amenity=wifi"), "Bare flat")                        // punctuation doesn't matter
	same(titles("?amenity=%E2%9D%84%EF%B8%8F%20AC"), "Borehole flat") // "AC" finds a home that says "A/C"
	same(titles("?amenity=generator"))
	// verified homes and verified landlords
	same(titles("?verified=1"), "Borehole flat")
	same(titles("?landlord_verified=1"), "Tap flat")
	// water and light come from the visit checklist, so a home nobody visited can't match
	same(titles("?water=borehole"), "Borehole flat")
	same(titles("?water=running"), "Borehole flat", "Tap flat")
	same(titles("?power=always"), "Borehole flat")
	same(titles("?power=most"), "Borehole flat", "Tap flat")
	same(titles("?water=any&power=any"), "Borehole flat", "Tap flat", "Bare flat", "Unchecked flat")
	// filters combine
	same(titles("?water=running&power=most&amenity=furnished"), "Borehole flat")
	e.expect(422, "GET", "/listings?water=ocean", "", nil)
	e.expect(422, "GET", "/listings?power=sometimes", "", nil)
}
