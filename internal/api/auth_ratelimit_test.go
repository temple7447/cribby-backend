package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestRepeatedWrongPasswordsAreBlockedEvenForTheRightOne(t *testing.T) {
	e := newEnv(t)
	e.signup("victim@gmail.com")
	for i := 0; i < 5; i++ {
		e.expect(401, "POST", "/auth/login", "", obj{"email": "victim@gmail.com", "password": "wrong-" + strings.Repeat("x", i)})
	}
	// now locked for this account on this connection: even the correct password is refused
	code, body := e.do("POST", "/auth/login", "", obj{"email": "victim@gmail.com", "password": "pw123456"})
	if code != 429 {
		t.Fatalf("after 5 failures: %d %v, want 429", code, body)
	}
	if msg, _ := body.(obj)["detail"].(string); !strings.Contains(msg, "Try again in") {
		t.Fatalf("message should say when to retry: %v", body)
	}
	// the same account can still be reached by someone else (a spread-out attack needs 20 failures)
	other := newEnv(t)
	other.signup("victim@gmail.com")
	other.expect(200, "POST", "/auth/login", "", obj{"email": "victim@gmail.com", "password": "pw123456"})
}

func TestRetryAfterHeaderIsSent(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.do("POST", "/auth/login", "", obj{"email": "nobody@gmail.com", "password": "x"})
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/auth/login", strings.NewReader(`{"email":"nobody@gmail.com","password":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

func TestSuccessfulLoginForgetsEarlierTypos(t *testing.T) {
	e := newEnv(t)
	e.signup("typo@gmail.com")
	for round := 0; round < 3; round++ {
		for i := 0; i < 4; i++ {
			e.expect(401, "POST", "/auth/login", "", obj{"email": "typo@gmail.com", "password": "nope"})
		}
		e.expect(200, "POST", "/auth/login", "", obj{"email": "typo@gmail.com", "password": "pw123456"})
	}
}

func TestOneAccountBeingGuessedDoesNotLockOthers(t *testing.T) {
	e := newEnv(t)
	e.signup("target@gmail.com")
	e.signup("bystander@gmail.com")
	for i := 0; i < 6; i++ {
		e.do("POST", "/auth/login", "", obj{"email": "target@gmail.com", "password": "guess"})
	}
	e.expect(200, "POST", "/auth/login", "", obj{"email": "bystander@gmail.com", "password": "pw123456"})
}

func TestSignupsFromOneConnectionAreCapped(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 30; i++ {
		e.expect(200, "POST", "/auth/signup", "", obj{"email": strings.Repeat("a", i+1) + "@gmail.com", "password": "pw123456", "name": "N"})
	}
	e.expect(429, "POST", "/auth/signup", "", obj{"email": "one-too-many@gmail.com", "password": "pw123456", "name": "N"})
}

func TestWrongVerificationCodesAreLimited(t *testing.T) {
	e := newEnv(t)
	token, _ := e.signup("guesser@gmail.com")
	e.expect(204, "POST", "/auth/verify/request", token, nil)
	good := e.mail.verifications[0].Code
	for i := 0; i < 5; i++ {
		e.expect(400, "POST", "/auth/verify/confirm", token, obj{"code": "000000"})
	}
	// out of guesses: even the real code is refused until the window passes
	e.expect(429, "POST", "/auth/verify/confirm", token, obj{"code": good})
}

func TestWrongResetCodesAreLimitedAndDoNotRevealAccounts(t *testing.T) {
	e := newEnv(t)
	e.signup("reset@gmail.com")
	e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "reset@gmail.com"})
	good := e.mail.resets[0].Code
	for _, email := range []string{"reset@gmail.com", "ghost@gmail.com"} {
		for i := 0; i < 5; i++ {
			e.expect(400, "POST", "/auth/password-reset/confirm", "", obj{"email": email, "code": "000000", "new_password": "newpass123"})
		}
		// real or not, the account gets the same answer once the guesses run out
		e.expect(429, "POST", "/auth/password-reset/confirm", "", obj{"email": email, "code": good, "new_password": "newpass123"})
	}
}

func TestResetRequestsAreThrottledByAddress(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 15; i++ {
		e.expect(204, "POST", "/auth/password-reset/request", "", obj{"email": "nobody-" + strings.Repeat("z", i+1) + "@gmail.com"})
	}
	e.expect(429, "POST", "/auth/password-reset/request", "", obj{"email": "again@gmail.com"})
}
