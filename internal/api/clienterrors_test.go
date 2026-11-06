package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// expectRoot calls a path outside /api/v1 (like /health/ready) and returns its JSON body.
func (e *env) expectRoot(status int, method, p string) obj {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+p, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		e.t.Fatalf("%s %s: status %d, want %d", method, p, resp.StatusCode, status)
	}
	var out obj
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func errReport(source, message, stack string) obj {
	return obj{"source": source, "message": message, "stack": stack, "route": "/listing/3", "app_version": "1.0.4", "platform": "android 14"}
}

func TestReadinessChecksTheDatabase(t *testing.T) {
	e := newEnv(t)
	out := e.expectRoot(200, "GET", "/health/ready")
	if out["status"] != "ok" || out["db"] != "ok" {
		t.Fatalf("got %v", out)
	}
}

func TestErrorReportsAreGroupedAndAdminsSeeThem(t *testing.T) {
	e := newEnv(t)
	adminTok, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))
	userTok, user := e.signup("crashy@gmail.com")

	// a guest report, then the same error from a signed-in user, then a different one
	e.expect(204, "POST", "/client-errors", "", errReport("app", "TypeError: x is undefined", "at Foo (a.js:1)\nat Bar (b.js:2)"))
	e.expect(204, "POST", "/client-errors", userTok, errReport("app", "TypeError: x is undefined", "at Foo (a.js:99)\nat Bar (b.js:2)"))
	e.expect(204, "POST", "/client-errors", "", errReport("web", "ChunkLoadError", ""))

	rows := list(e.expect(200, "GET", "/admin/client-errors", adminTok, nil))
	if len(rows) != 2 {
		t.Fatalf("want 2 grouped errors, got %v", rows)
	}
	var grouped obj
	for _, r := range rows {
		if r.(obj)["message"] == "TypeError: x is undefined" {
			grouped = r.(obj)
		}
	}
	if num(grouped, "count") != 2 || grouped["source"] != "app" || grouped["platform"] != "android 14" {
		t.Fatalf("grouped %v", grouped)
	}
	if grouped["user_id"] == nil || num(grouped, "user_id") != id(user) {
		t.Fatalf("the signed-in reporter should be attached: %v", grouped)
	}

	// regular users can't read them
	e.expect(403, "GET", "/admin/client-errors", userTok, nil)

	// resolving hides it; it comes back if the error recurs
	eid := id(grouped)
	e.expect(204, "PATCH", path("/admin/client-errors/%d", eid), adminTok, obj{"resolved": true})
	if open := list(e.expect(200, "GET", "/admin/client-errors", adminTok, nil)); len(open) != 1 {
		t.Fatalf("open after resolving: %v", open)
	}
	if done := list(e.expect(200, "GET", "/admin/client-errors?status=resolved", adminTok, nil)); len(done) != 1 {
		t.Fatalf("resolved list: %v", done)
	}
	e.expect(204, "POST", "/client-errors", "", errReport("app", "TypeError: x is undefined", "at Foo (a.js:1)\nat Bar (b.js:2)"))
	if open := list(e.expect(200, "GET", "/admin/client-errors", adminTok, nil)); len(open) != 2 {
		t.Fatalf("a recurring error should re-open: %v", open)
	}
	e.expect(404, "PATCH", "/admin/client-errors/99999", adminTok, obj{"resolved": true})
}

func TestErrorReportsAreBoundedAndValidated(t *testing.T) {
	e := newEnv(t)
	adminTok, admin := e.signup("boss@gmail.com")
	e.makeAdmin(id(admin))
	e.expect(422, "POST", "/client-errors", "", obj{"source": "toaster", "message": "x"})
	e.expect(422, "POST", "/client-errors", "", obj{"source": "app"})
	e.expect(422, "POST", "/client-errors", "", obj{"source": "app", "message": "   "})
	e.expect(204, "POST", "/client-errors", "not-a-real-token", errReport("app", strings.Repeat("m", 5000), strings.Repeat("s", 50000)))
	row := list(e.expect(200, "GET", "/admin/client-errors", adminTok, nil))[0].(obj)
	if len(row["message"].(string)) != 500 || len(row["stack"].(string)) != 4000 {
		t.Fatalf("not clipped: message %d stack %d", len(row["message"].(string)), len(row["stack"].(string)))
	}
}

func TestErrorReportsAreThrottledPerAddress(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 60; i++ {
		e.expect(204, "POST", "/client-errors", "", errReport("app", "boom "+strings.Repeat("x", i), ""))
	}
	e.expect(429, "POST", "/client-errors", "", errReport("app", "one too many", ""))
}
