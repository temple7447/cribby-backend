package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRouter answers per model: status 200 with a reply, an error status, or empty content.
type fakeRouter struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []string
	rules map[string]string // model -> "ok:text" | "status:429" | "empty" | "hang"
}

func newFakeRouter(rules map[string]string) *fakeRouter {
	f := &fakeRouter{rules: rules}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls = append(f.calls, body.Model)
		rule := f.rules[body.Model]
		f.mu.Unlock()
		switch {
		case rule == "hang":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		case strings.HasPrefix(rule, "status:"):
			var code int
			fmt.Sscanf(rule, "status:%d", &code)
			w.WriteHeader(code)
			fmt.Fprint(w, `{"error":{"message":"upstream said no"}}`)
		case rule == "empty":
			fmt.Fprint(w, `{"choices":[{"message":{"content":"   "}}]}`)
		default:
			fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, strings.TrimPrefix(rule, "ok:"))
		}
	}))
	return f
}

func (f *fakeRouter) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func clientFor(f *fakeRouter, primary string, backups ...string) *Client {
	c := New("key", primary, false).WithFallback(backups...)
	c.URL = f.srv.URL
	c.AttemptTimeout = 300 * time.Millisecond
	return c
}

var hi = []Message{{Role: "user", Content: "hi"}}

func TestTheMainModelAnswersAndTheBackupIsNeverTouched(t *testing.T) {
	f := newFakeRouter(map[string]string{"main": "ok:from main", "backup": "ok:from backup"})
	defer f.srv.Close()
	got, err := clientFor(f, "main", "backup").Chat(context.Background(), hi)
	if err != nil || got != "from main" {
		t.Fatalf("got %q, %v", got, err)
	}
	if c := f.called(); len(c) != 1 || c[0] != "main" {
		t.Fatalf("only the main model should be asked: %v", c)
	}
}

func TestBackupAnswersWhenTheMainModelFails(t *testing.T) {
	for name, rule := range map[string]string{"rate limited": "status:429", "server error": "status:503", "retired": "status:404", "empty answer": "empty", "hangs": "hang"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeRouter(map[string]string{"main": rule, "backup": "ok:from backup"})
			defer f.srv.Close()
			got, err := clientFor(f, "main", "backup").Chat(context.Background(), hi)
			if err != nil || got != "from backup" {
				t.Fatalf("got %q, %v", got, err)
			}
			if c := f.called(); len(c) != 2 || c[0] != "main" || c[1] != "backup" {
				t.Fatalf("calls: %v", c)
			}
		})
	}
}

func TestAFailedModelRestsSoPeopleAreNotKeptWaiting(t *testing.T) {
	f := newFakeRouter(map[string]string{"main": "status:429", "backup": "ok:from backup"})
	defer f.srv.Close()
	c := clientFor(f, "main", "backup")
	for i := 0; i < 3; i++ {
		if got, err := c.Chat(context.Background(), hi); err != nil || got != "from backup" {
			t.Fatalf("call %d: %q, %v", i, got, err)
		}
	}
	// main is tried once (and fails), then skipped while it rests
	if got := f.called(); strings.Join(got, ",") != "main,backup,backup,backup" {
		t.Fatalf("calls: %v", got)
	}
	// a rate limit rests it for minutes; once the rest is over the main model gets another go
	c.mu.Lock()
	c.until["main"] = time.Now().Add(-time.Second)
	c.mu.Unlock()
	f.mu.Lock()
	f.rules["main"] = "ok:main is back"
	f.mu.Unlock()
	if got, err := c.Chat(context.Background(), hi); err != nil || got != "main is back" {
		t.Fatalf("after resting: %q, %v", got, err)
	}
}

func TestWhenEveryModelFailsTheErrorComesBack(t *testing.T) {
	f := newFakeRouter(map[string]string{"main": "status:429", "backup": "status:503"})
	defer f.srv.Close()
	c := clientFor(f, "main", "backup")
	_, err := c.Chat(context.Background(), hi)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 {
		t.Fatalf("want the last error, got %v", err)
	}
	// both are resting now, but a request still tries them rather than giving up without asking
	if _, err := c.Chat(context.Background(), hi); err == nil {
		t.Fatal("still failing upstream")
	}
	if n := len(f.called()); n != 4 {
		t.Fatalf("each request should try both models: %v", f.called())
	}
}

func TestARefusalOfTheTextItselfStaysARefusalAndDoesNotBenchTheModel(t *testing.T) {
	f := newFakeRouter(map[string]string{"main": "status:400", "backup": "status:400"})
	defer f.srv.Close()
	c := clientFor(f, "main", "backup")
	_, err := c.Chat(context.Background(), hi)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("callers rely on a 400 to flag odd text: %v", err)
	}
	f.mu.Lock()
	f.rules["main"] = "ok:fine"
	f.mu.Unlock()
	if got, err := c.Chat(context.Background(), hi); err != nil || got != "fine" {
		t.Fatalf("a bad request must not bench a healthy model: %q, %v", got, err)
	}
}

func TestNoRetryWhenTheCallerHasGivenUp(t *testing.T) {
	f := newFakeRouter(map[string]string{"main": "hang", "backup": "ok:from backup"})
	defer f.srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	c := clientFor(f, "main", "backup")
	c.AttemptTimeout = 0 // only the caller's own deadline applies
	if _, err := c.Chat(ctx, hi); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a deadline error, got %v", err)
	}
	if c := f.called(); len(c) != 1 {
		t.Fatalf("no point asking the backup after the caller is gone: %v", c)
	}
}

func TestWithoutABackupBehaviourIsUnchanged(t *testing.T) {
	f := newFakeRouter(map[string]string{"main": "status:429"})
	defer f.srv.Close()
	c := New("key", "main", false)
	c.URL = f.srv.URL
	if _, err := c.Chat(context.Background(), hi); err == nil {
		t.Fatal("expected the error")
	}
	if n := len(f.called()); n != 1 {
		t.Fatalf("calls: %v", f.called())
	}
	// the same name twice, or an empty one, is not a backup
	if c := New("key", "main", false).WithFallback("", "main"); len(c.Fallbacks) != 0 {
		t.Fatalf("fallbacks: %v", c.Fallbacks)
	}
	if c := New("key", "main", false).WithFallback("b", "b"); len(c.Fallbacks) != 1 {
		t.Fatalf("fallbacks: %v", c.Fallbacks)
	}
	if _, err := New("", "main", false).Chat(context.Background(), hi); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no key: %v", err)
	}
}
