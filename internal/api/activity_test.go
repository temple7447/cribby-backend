package api_test

import (
	"strings"
	"testing"
	"time"

	"campus-backend/internal/db"
)

func userRow(t *testing.T, e *env, userID int) (login, active *db.Time) {
	t.Helper()
	if err := e.db.QueryRow(t.Context(), `SELECT last_login_at, last_active_at FROM users WHERE id = ?`, userID).Scan(&login, &active); err != nil {
		t.Fatal(err)
	}
	return login, active
}

func TestSignupAndLoginRecordLastLogin(t *testing.T) {
	e := newEnv(t)
	body := e.expect(200, "POST", "/auth/signup", "", obj{"email": "act@gmail.com", "password": "pw123456", "name": "Act"}).(obj)
	user := body["user"].(obj)
	if user["last_login_at"] == nil || user["last_active_at"] == nil {
		t.Fatalf("signup response lacks activity: %v", user)
	}
	login, _ := userRow(t, e, id(user))
	if login == nil || time.Since(login.Time) > time.Minute {
		t.Fatalf("signup did not record a login: %v", login)
	}

	// an old login, then a new one moves it forward
	e.exec(`UPDATE users SET last_login_at = ?, last_active_at = ? WHERE id = ?`, db.Time{Time: time.Now().Add(-48 * time.Hour)}, db.Time{Time: time.Now().Add(-48 * time.Hour)}, id(user))
	e.expect(200, "POST", "/auth/login", "", obj{"email": "act@gmail.com", "password": "pw123456"})
	login, active := userRow(t, e, id(user))
	if time.Since(login.Time) > time.Minute || time.Since(active.Time) > time.Minute {
		t.Fatalf("login did not refresh the timestamps: %v %v", login, active)
	}
}

func TestFailedAndSuspendedLoginsAreNotRecorded(t *testing.T) {
	e := newEnv(t)
	_, user := e.signup("nolog@gmail.com")
	e.exec(`UPDATE users SET last_login_at = NULL, last_active_at = NULL WHERE id = ?`, id(user))
	e.expect(401, "POST", "/auth/login", "", obj{"email": "nolog@gmail.com", "password": "wrong"})
	e.exec(`UPDATE users SET is_suspended = ? WHERE id = ?`, true, id(user))
	e.expect(403, "POST", "/auth/login", "", obj{"email": "nolog@gmail.com", "password": "pw123456"})
	if login, active := userRow(t, e, id(user)); login != nil || active != nil {
		t.Fatalf("a refused login was recorded: %v %v", login, active)
	}
}

func TestPasswordResetCountsAsALogin(t *testing.T) {
	e := newEnv(t)
	_, user := e.signup("reset-act@gmail.com")
	e.exec(`UPDATE users SET last_login_at = NULL WHERE id = ?`, id(user))
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "reset-act@gmail.com"})
	e.expect(200, "POST", "/auth/password-reset/confirm", "", resetBody("reset-act@gmail.com", e.mail.resets[0].Code, "newpass456"))
	if login, _ := userRow(t, e, id(user)); login == nil {
		t.Fatal("completing a reset signs the user in and should be recorded")
	}
}

func TestLastActiveRefreshesAtMostEveryTenMinutes(t *testing.T) {
	e := newEnv(t)
	token, user := e.signup("busy@gmail.com")

	// stale activity: using the app refreshes it
	stale := db.Time{Time: time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Microsecond)}
	e.exec(`UPDATE users SET last_active_at = ? WHERE id = ?`, stale, id(user))
	e.expect(200, "GET", "/users/me", token, nil)
	_, active := userRow(t, e, id(user))
	if !active.After(stale.Time.Add(time.Hour)) {
		t.Fatalf("activity not refreshed: %v", active)
	}

	// recent activity: left alone (no write per request)
	recent := db.Time{Time: time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Microsecond)}
	e.exec(`UPDATE users SET last_active_at = ? WHERE id = ?`, recent, id(user))
	e.expect(200, "GET", "/users/me", token, nil)
	if _, active := userRow(t, e, id(user)); !active.Equal(recent.Time) {
		t.Fatalf("activity rewritten within the 10-minute window: %v vs %v", active, recent)
	}
	// and it never changed last_login_at
	if login, _ := userRow(t, e, id(user)); login == nil || time.Since(login.Time) > time.Hour {
		t.Fatalf("last_login_at disturbed: %v", login)
	}
}

func TestAdminSeesActivityOnUsers(t *testing.T) {
	e := newEnv(t)
	adminTok, admin := e.signup("boss-act@gmail.com")
	e.makeAdmin(id(admin))
	e.signup("someone-act@gmail.com")
	for _, u := range list(e.expect(200, "GET", "/users", adminTok, nil)) {
		if u.(obj)["last_login_at"] == nil {
			t.Fatalf("admin list lacks last_login_at: %v", u)
		}
	}
	if detail := e.expect(200, "GET", path("/users/%d", id(admin)), adminTok, nil).(obj); detail["user"].(obj)["last_active_at"] == nil {
		t.Fatalf("user detail lacks activity: %v", detail["user"])
	}
}

func TestAdminChatKnowsRecentLoginsWithoutEmails(t *testing.T) {
	e := newEnv(t)
	adminTok, admin := e.signup("boss-act2@gmail.com")
	e.makeAdmin(id(admin))
	_, ada := e.signup("ada-person@gmail.com")
	e.exec(`UPDATE users SET name = 'Ada Obi', last_login_at = ?, last_active_at = ? WHERE id = ?`,
		db.Time{Time: time.Now().Add(-3 * time.Hour)}, db.Time{Time: time.Now().Add(-3 * time.Hour)}, id(ada))
	e.expect(200, "POST", "/admin/ai/chat", adminTok, obj{"messages": []obj{{"role": "user", "content": "who logged in last?"}}})
	sys := e.ai.messages()[0]["content"].(string)
	for _, want := range []string{"Most recent logins, newest first", "Ada", "login tracking began recently", "active in the last 24 hours"} {
		if !strings.Contains(sys, want) {
			t.Errorf("snapshot lacks %q\n%s", want, sys)
		}
	}
	if strings.Contains(sys, "ada-person@gmail.com") || strings.Contains(sys, "Obi") {
		t.Error("only first names (no emails or surnames) should reach the model")
	}
}

func TestAdminChatPromptListsOnlyRealDashboardPages(t *testing.T) {
	e, admin, _, _ := adminAIFixture(t)
	e.do("POST", "/admin/ai/chat", admin, obj{"messages": []obj{{"role": "user", "content": "hi"}}})
	sys := e.ai.messages()[0]["content"].(string)
	for _, want := range []string{"EXACTLY these pages", "Dashboard", "Bookings", "Reports", "Listings", "Users", "AI Assistant",
		"NO activity log, audit trail", "never suggest or mention any feature that is not in this list"} {
		if !strings.Contains(sys, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}
