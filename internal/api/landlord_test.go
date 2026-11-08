package api_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func (e *env) submitID(token, phone, idType string) (int, any) {
	return e.upload("/landlord/verification", token, "id.jpg", "image/jpeg", []byte("jpegbytes"),
		map[string]string{"phone": phone, "id_type": idType})
}

func TestLandlordVerificationFlow(t *testing.T) {
	e := newEnv(t)
	e.photos.configured, e.photos.urlFmt, e.photos.idFmt = true, "https://cdn.test/p%d.jpg", "cribby/p%d"
	token, user := e.signup("landlord@gmail.com")
	adminTok, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))

	if got := e.expect(200, "GET", "/landlord/verification", token, nil).(obj); got["status"] != "none" {
		t.Fatalf("got %v", got)
	}
	// a listing posted before verification has no badge
	before := id(e.expect(201, "POST", "/listings", token, apartment("Before")).(obj))

	if code, _ := e.submitID(token, "abc", "nin"); code != 422 {
		t.Fatalf("bad phone: %d", code)
	}
	if code, _ := e.submitID(token, "0803 123 4567", "selfie"); code != 422 {
		t.Fatalf("bad id type: %d", code)
	}
	code, out := e.submitID(token, "0803 123 4567", "nin")
	if code != 200 || out.(obj)["status"] != "pending" || out.(obj)["phone"] != "08031234567" {
		t.Fatalf("submit: %d %v", code, out)
	}
	if _, leaked := out.(obj)["id_photo_url"]; leaked || strings.Contains(toJSON(out), "private") {
		t.Fatalf("the ID photo must never be returned to the landlord: %v", out)
	}

	// only admins see the queue and the (signed) photo
	e.expect(403, "GET", "/admin/landlord-verifications", token, nil)
	queue := list(e.expect(200, "GET", "/admin/landlord-verifications", adminTok, nil))
	if len(queue) != 1 || queue[0].(obj)["email"] != "landlord@gmail.com" || !strings.HasPrefix(queue[0].(obj)["id_photo_url"].(string), "https://cdn.test/signed/") {
		t.Fatalf("queue %v", queue)
	}
	uid := id(user)
	e.expect(403, "PATCH", path("/admin/landlord-verifications/%d", uid), token, obj{"decision": "approve"})
	e.expect(422, "PATCH", path("/admin/landlord-verifications/%d", uid), adminTok, obj{"decision": "reject"}) // needs a reason
	e.expect(200, "PATCH", path("/admin/landlord-verifications/%d", uid), adminTok, obj{"decision": "reject", "note": "Photo is blurry"})
	got := e.expect(200, "GET", "/landlord/verification", token, nil).(obj)
	if got["status"] != "rejected" || got["note"] != "Photo is blurry" {
		t.Fatalf("got %v", got)
	}

	// resubmit, get approved: the badge appears on old and new listings
	if code, _ := e.submitID(token, "08031234567", "passport"); code != 200 {
		t.Fatalf("resubmit: %d", code)
	}
	if len(e.photos.destroyed) != 1 {
		t.Fatalf("old ID photo should be deleted: %v", e.photos.destroyed)
	}
	e.expect(200, "PATCH", path("/admin/landlord-verifications/%d", uid), adminTok, obj{"decision": "approve"})
	if n := e.count(`SELECT COUNT(*) FROM listings WHERE id = ? AND landlord_verified = ?`, before, true); n != 1 {
		t.Fatal("existing listing should get the badge")
	}
	after := e.expect(201, "POST", "/listings", token, apartment("After")).(obj)
	if after["landlord_verified"] != true {
		t.Fatalf("new listing should be badged: %v", after["landlord_verified"])
	}
	if code, _ := e.submitID(token, "08031234567", "nin"); code != 409 {
		t.Fatalf("already verified: %d", code)
	}

	// revoking removes the badge again
	e.expect(200, "PATCH", path("/admin/landlord-verifications/%d", uid), adminTok, obj{"decision": "reject", "note": "Fake ID"})
	if n := e.count(`SELECT COUNT(*) FROM listings WHERE owner_id = ? AND landlord_verified = ?`, uid, true); n != 0 {
		t.Fatal("badge should be removed")
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
