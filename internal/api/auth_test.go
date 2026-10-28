package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// ---- verification (tests/test_verification.py) ----

func TestSignupCreatesUnverifiedUser(t *testing.T) {
	e := newEnv(t)
	_, user := e.signup("student@stateuniversity.edu")
	if user["verification_status"] != "unverified" {
		t.Fatalf("got %v", user["verification_status"])
	}
}

func TestSignupRejectsDuplicateEmail(t *testing.T) {
	e := newEnv(t)
	e.signup("dup@gmail.com")
	e.expect(400, "POST", "/auth/signup", "", obj{"email": "dup@gmail.com", "password": "pw123456", "name": "X"})
}

func TestEmailDomainIsNormalisedLikeEmailStr(t *testing.T) {
	e := newEnv(t)
	_, user := e.signup("Student@GMAIL.com")
	if user["email"] != "Student@gmail.com" {
		t.Fatalf("email = %v", user["email"])
	}
}

func TestRequestVerificationAcceptsAnyEmailDomain(t *testing.T) {
	// .edu isn't a realistic constraint outside the US, so verification accepts
	// any address — gmail included.
	e := newEnv(t)
	token, _ := e.signup("student@gmail.com")
	e.expect(204, "POST", "/auth/verify/request", token, nil)
	if len(e.mail.verifications) != 1 || e.mail.verifications[0].To != "student@gmail.com" {
		t.Fatalf("sent %+v", e.mail.verifications)
	}
}

func TestVerifyRequestAndConfirmSucceeds(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/verify/request", token, nil)
	code := e.mail.verifications[0].Code
	if len(code) != 6 {
		t.Fatalf("code %q", code)
	}
	out := e.expect(200, "POST", "/auth/verify/confirm", token, obj{"code": " " + code + " "}).(obj)
	if out["verification_status"] != "verified" {
		t.Fatalf("got %v", out)
	}
	// already verified now
	e.expect(400, "POST", "/auth/verify/request", token, nil)
}

func TestConfirmWrongCodeFails(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/verify/request", token, nil)
	wrong := "000000"
	if e.mail.verifications[0].Code == wrong {
		wrong = "000001"
	}
	e.expect(400, "POST", "/auth/verify/confirm", token, obj{"code": wrong})
}

func TestConfirmWithoutRequestFails(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	e.expect(400, "POST", "/auth/verify/confirm", token, obj{"code": "123456"})
}

func TestConfirmExpiredCodeFails(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/verify/request", token, nil)
	e.exec(`UPDATE users SET verification_code_expires_at = ? WHERE id = ?`, past(), id(user))
	e.expect(400, "POST", "/auth/verify/confirm", token, obj{"code": e.mail.verifications[0].Code})
}

func TestVerifyResendCooldownReturns429(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/verify/request", token, nil)
	out := e.expect(429, "POST", "/auth/verify/request", token, nil).(obj)
	if !strings.HasPrefix(out["detail"].(string), "Please wait ") {
		t.Fatalf("detail %v", out["detail"])
	}
}

func TestVerifyEmailFailureIs502AndLeavesNoCooldown(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	e.mail.failVerification = true
	e.expect(502, "POST", "/auth/verify/request", token, nil)
	e.mail.failVerification = false
	e.expect(204, "POST", "/auth/verify/request", token, nil) // no cooldown was recorded
}

func TestSelfVerifyViaPatchUsersMeIsRejected(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	e.expect(200, "PATCH", "/users/me", token, obj{"verification_status": "verified"})
	me := e.expect(200, "GET", "/users/me", token, nil).(obj)
	if me["verification_status"] != "unverified" {
		t.Fatalf("self-verify succeeded: %v", me)
	}
}

func TestUpdateMeChangesOnlyProvidedFields(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("student@stateuniversity.edu")
	out := e.expect(200, "PATCH", "/users/me", token, obj{"name": "Renamed"}).(obj)
	if out["name"] != "Renamed" || out["school"] != "State University" || out["avatar_emoji"] != "🎓" {
		t.Fatalf("got %v", out)
	}
}

// ---- auth basics & error shapes ----

func TestProtectedRouteWithoutTokenIs401(t *testing.T) {
	e := newEnv(t)
	out := e.expect(401, "GET", "/users/me", "", nil).(obj)
	if out["detail"] != "Not authenticated" {
		t.Fatalf("got %v", out)
	}
	bad := e.expect(401, "GET", "/users/me", "garbage", nil).(obj)
	if bad["detail"] != "Could not validate credentials" {
		t.Fatalf("got %v", bad)
	}
}

func TestLoginWrongPasswordIs401(t *testing.T) {
	e := newEnv(t)
	e.signup("student@stateuniversity.edu")
	e.expect(401, "POST", "/auth/login", "", obj{"email": "student@stateuniversity.edu", "password": "nope"})
	e.expect(200, "POST", "/auth/login", "", obj{"email": "student@stateuniversity.edu", "password": "pw123456"})
}

func TestValidationErrorsAre422(t *testing.T) {
	e := newEnv(t)
	e.expect(422, "POST", "/auth/signup", "", obj{"email": "not-an-email", "password": "x", "name": "n"})
	e.expect(422, "POST", "/auth/signup", "", obj{"password": "x"})
	e.expect(422, "POST", "/auth/signup", "", nil)
	e.expect(422, "GET", "/listings?max_price=abc", "", nil)
	e.expect(422, "GET", "/listings/notanumber", "", nil)
}

func TestWrongMethodIs405WithAllowHeader(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest("PUT", e.srv.URL+"/api/v1/listings/1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, PATCH, DELETE" {
		t.Fatalf("status=%d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	// a path that exists for several methods lists them all
	req, _ = http.NewRequest("PUT", e.srv.URL+"/api/v1/favorites/1", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 401 { // PUT is a real favorites route → 401 without a token
		t.Fatalf("PUT /favorites/1 status=%d", resp.StatusCode)
	}
	req, _ = http.NewRequest("PATCH", e.srv.URL+"/api/v1/favorites/1", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "PUT, DELETE" {
		t.Fatalf("status=%d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	e := newEnv(t)
	out := e.expect(404, "GET", "/nope", "", nil).(obj)
	if out["detail"] != "Not Found" {
		t.Fatalf("got %v", out)
	}
}

// ---- password reset (tests/test_password_reset.py) ----

func TestResetForUnknownEmailStill204AndSendsNothing(t *testing.T) {
	e := newEnv(t)
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "nobody@stateuniversity.edu"})
	if len(e.mail.resets) != 0 {
		t.Fatal("sent mail for a nonexistent account")
	}
}

func resetBody(email, code, pw string) obj {
	return obj{"email": email, "code": code, "new_password": pw}
}

func TestPasswordResetRequestAndConfirm(t *testing.T) {
	e := newEnv(t)
	e.expect(200, "POST", "/auth/signup", "", obj{"email": "student@stateuniversity.edu", "password": "oldpass123", "name": "T"})
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "student@stateuniversity.edu"})
	if len(e.mail.resets) != 1 || e.mail.resets[0].To != "student@stateuniversity.edu" {
		t.Fatalf("sent %+v", e.mail.resets)
	}
	out := e.expect(200, "POST", "/auth/password-reset/confirm", "",
		resetBody("student@stateuniversity.edu", e.mail.resets[0].Code, "newpass456")).(obj)
	if out["user"].(obj)["email"] != "student@stateuniversity.edu" || out["access_token"] == nil {
		t.Fatalf("got %v", out)
	}
	e.expect(401, "POST", "/auth/login", "", obj{"email": "student@stateuniversity.edu", "password": "oldpass123"})
	e.expect(200, "POST", "/auth/login", "", obj{"email": "student@stateuniversity.edu", "password": "newpass456"})
	// the code is single-use
	e.expect(400, "POST", "/auth/password-reset/confirm", "",
		resetBody("student@stateuniversity.edu", e.mail.resets[0].Code, "another789"))
}

func TestPasswordResetWrongCodeFails(t *testing.T) {
	e := newEnv(t)
	e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "student@stateuniversity.edu"})
	wrong := "000000"
	if e.mail.resets[0].Code == wrong {
		wrong = "000001"
	}
	e.expect(400, "POST", "/auth/password-reset/confirm", "", resetBody("student@stateuniversity.edu", wrong, "whatever1"))
}

func TestPasswordResetWithoutPendingRequestFails(t *testing.T) {
	e := newEnv(t)
	e.signup("student@stateuniversity.edu")
	e.expect(400, "POST", "/auth/password-reset/confirm", "", resetBody("student@stateuniversity.edu", "123456", "whatever1"))
}

func TestPasswordResetExpiredCodeFails(t *testing.T) {
	e := newEnv(t)
	e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "student@stateuniversity.edu"})
	e.exec(`UPDATE users SET password_reset_code_expires_at = ? WHERE email = ?`, past(), "student@stateuniversity.edu")
	e.expect(400, "POST", "/auth/password-reset/confirm", "",
		resetBody("student@stateuniversity.edu", e.mail.resets[0].Code, "whatever1"))
}

func TestPasswordResetCooldownSendsNoSecondEmail(t *testing.T) {
	e := newEnv(t)
	e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "student@stateuniversity.edu"})
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "student@stateuniversity.edu"})
	if len(e.mail.resets) != 1 {
		t.Fatalf("sent %d emails, want 1", len(e.mail.resets))
	}
}

func TestPasswordResetRejectsShortPassword(t *testing.T) {
	e := newEnv(t)
	e.signup("student@stateuniversity.edu")
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "student@stateuniversity.edu"})
	e.expect(422, "POST", "/auth/password-reset/confirm", "",
		resetBody("student@stateuniversity.edu", e.mail.resets[0].Code, "abc"))
}

// ---- suspension (tests/test_user_suspension.py) ----

func TestSetSuspendedRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("notadmin4@gmail.com")
	_, other := e.signup("victim2@gmail.com")
	e.expect(403, "PATCH", path("/users/%d/suspend", id(other)), token, obj{"is_suspended": true})
}

func TestAdminCannotSuspendOwnAccount(t *testing.T) {
	e := newEnv(t)
	token, admin := e.signup("selfsuspend@gmail.com")
	e.makeAdmin(id(admin))
	e.expect(400, "PATCH", path("/users/%d/suspend", id(admin)), token, obj{"is_suspended": true})
}

func TestSuspendBlocksLogin(t *testing.T) {
	e := newEnv(t)
	adminToken, admin := e.signup("modadmin2@gmail.com")
	e.makeAdmin(id(admin))
	_, target := e.signup("suspendme@gmail.com")
	out := e.expect(200, "PATCH", path("/users/%d/suspend", id(target)), adminToken, obj{"is_suspended": true}).(obj)
	if out["is_suspended"] != true {
		t.Fatalf("got %v", out)
	}
	login := e.expect(403, "POST", "/auth/login", "", obj{"email": "suspendme@gmail.com", "password": "pw123456"}).(obj)
	if !strings.Contains(strings.ToLower(login["detail"].(string)), "suspended") {
		t.Fatalf("detail %v", login["detail"])
	}
}

func TestSuspendCutsOffAnExistingSession(t *testing.T) {
	e := newEnv(t)
	adminToken, admin := e.signup("modadmin3@gmail.com")
	e.makeAdmin(id(admin))
	targetToken, target := e.signup("midsession@gmail.com")
	e.expect(200, "GET", "/users/me", targetToken, nil)
	e.expect(200, "PATCH", path("/users/%d/suspend", id(target)), adminToken, obj{"is_suspended": true})
	out := e.expect(403, "GET", "/users/me", targetToken, nil).(obj)
	if !strings.Contains(strings.ToLower(out["detail"].(string)), "suspended") {
		t.Fatalf("detail %v", out["detail"])
	}
}

func TestUnsuspendRestoresAccess(t *testing.T) {
	e := newEnv(t)
	adminToken, admin := e.signup("modadmin4@gmail.com")
	e.makeAdmin(id(admin))
	_, target := e.signup("restoreme@gmail.com")
	e.expect(200, "PATCH", path("/users/%d/suspend", id(target)), adminToken, obj{"is_suspended": true})
	out := e.expect(200, "PATCH", path("/users/%d/suspend", id(target)), adminToken, obj{"is_suspended": false}).(obj)
	if out["is_suspended"] != false {
		t.Fatalf("got %v", out)
	}
	e.expect(200, "POST", "/auth/login", "", obj{"email": "restoreme@gmail.com", "password": "pw123456"})
}

// ---- account deletion (tests/test_account_deletion.py) ----

func TestWrongPasswordRejectedAccountSurvives(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("keepme@gmail.com")
	e.expect(401, "DELETE", "/users/me", token, obj{"password": "wrong"})
	if e.count(`SELECT COUNT(*) FROM users WHERE id = ?`, id(user)) != 1 {
		t.Fatal("account was deleted")
	}
}

func TestDeleteRemovesAccountAndBlocksFurtherLogin(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("bye@gmail.com")
	e.expect(204, "DELETE", "/users/me", token, obj{"password": "pw123456"})
	e.expect(401, "POST", "/auth/login", "", obj{"email": "bye@gmail.com", "password": "pw123456"})
}

func TestDeleteCascadesOwnedData(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("cascade@gmail.com")
	listingID := e.createListing()
	e.expect(201, "POST", "/bookings", token, obj{"listing_id": listingID, "move_in_date": "2026-09-01", "lease_months": 12})
	e.expect(204, "PUT", path("/favorites/%d", listingID), token, nil)

	e.exec(`INSERT INTO roommate_profiles (user_id, name, age, school, year, budget_min, budget_max, move_in_date, tags, bio,
		looking_for, preferred_areas, visibility, emoji, grad)
		VALUES (?, 'Test', 20, 'State University', 'Junior', 400, 600, 'Aug 1', '[]', '', '', '[]', 'live', '🎨', 'from-coral to-sunshine')`, id(user))
	profileID := e.count(`SELECT id FROM roommate_profiles WHERE user_id = ?`, id(user))

	e.exec(`INSERT INTO conversations (user_id, kind, listing_id, title, subtitle, emoji, grad)
		VALUES (?, 'landlord', ?, 'Sandra', '', '👩‍💼', 'from-coral to-sunshine')`, id(user), listingID)
	convoID := e.count(`SELECT id FROM conversations WHERE user_id = ?`, id(user))
	e.exec(`INSERT INTO messages (conversation_id, sender, text, read) VALUES (?, 'me', 'hi', true)`, convoID)

	// another user who was blocked by this one, and who messaged this user's
	// roommate profile — their conversation should survive, unlinked.
	_, other := e.signup("other@gmail.com")
	e.expect(200, "POST", "/blocks", token, obj{"blocked_user_id": id(other)})
	e.exec(`INSERT INTO conversations (user_id, kind, roommate_profile_id, title, subtitle, emoji, grad)
		VALUES (?, 'roommate', ?, 'Test', '', '🎨', 'from-coral to-sunshine')`, id(other), profileID)
	otherConvo := e.count(`SELECT id FROM conversations WHERE user_id = ?`, id(other))

	e.expect(204, "DELETE", "/users/me", token, obj{"password": "pw123456"})

	for table, q := range map[string]string{
		"bookings":  `SELECT COUNT(*) FROM bookings`,
		"favorites": `SELECT COUNT(*) FROM favorites`,
		"profile":   `SELECT COUNT(*) FROM roommate_profiles WHERE id = ` + itoa(profileID),
		"convo":     `SELECT COUNT(*) FROM conversations WHERE id = ` + itoa(convoID),
		"messages":  `SELECT COUNT(*) FROM messages WHERE conversation_id = ` + itoa(convoID),
		"blocks":    `SELECT COUNT(*) FROM blocks`,
		"user":      `SELECT COUNT(*) FROM users WHERE email = 'cascade@gmail.com'`,
	} {
		if n := e.count(q); n != 0 {
			t.Errorf("%s: %d rows left", table, n)
		}
	}
	if e.count(`SELECT COUNT(*) FROM conversations WHERE id = ? AND roommate_profile_id IS NULL`, otherConvo) != 1 {
		t.Error("other user's conversation should survive, unlinked from the deleted profile")
	}
}

func itoa(n int) string { return path("%d", n) }
