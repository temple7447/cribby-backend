package api

import (
	"testing"
	"time"
)

// Unit tests for the limiter itself, with an injected clock (package api, not api_test).

func TestAttemptLimiterSlidesAndCleansUp(t *testing.T) {
	l := newAttemptLimiter(10 * time.Minute)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		l.record("k", t0.Add(time.Duration(i)*time.Minute))
	}
	if n, wait := l.status("k", t0.Add(3*time.Minute)); n != 3 || wait != 7*time.Minute {
		t.Fatalf("status = %d, %v; want 3, 7m (until the oldest hit expires)", n, wait)
	}
	// the oldest falls out of the window first
	if n, _ := l.status("k", t0.Add(10*time.Minute+time.Second)); n != 2 {
		t.Fatalf("after the first expires: %d, want 2", n)
	}
	if n, _ := l.status("k", t0.Add(time.Hour)); n != 0 {
		t.Fatalf("after the window: %d, want 0", n)
	}
	l.mu.Lock()
	left := len(l.hits)
	l.mu.Unlock()
	if left != 0 {
		t.Fatalf("expired keys should be dropped, %d left", left)
	}
}

func TestAttemptLimiterTakeAndClear(t *testing.T) {
	l := newAttemptLimiter(time.Hour)
	now := time.Now()
	for i := 0; i < 3; i++ {
		if ok, _ := l.take("ip", 3, now); !ok {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if ok, wait := l.take("ip", 3, now); ok || wait <= 0 {
		t.Fatalf("4th attempt: ok=%v wait=%v, want refused with a wait", ok, wait)
	}
	if ok, _ := l.take("other", 3, now); !ok {
		t.Fatal("a different key must not be affected")
	}
	l.clear("ip")
	if ok, _ := l.take("ip", 3, now); !ok {
		t.Fatal("clear should reset the key")
	}
}

func TestLoginKeysAreCaseInsensitive(t *testing.T) {
	a1, p1, _ := loginKeys(" Ada@Gmail.com ", "1.2.3.4")
	a2, p2, _ := loginKeys("ada@gmail.com", "1.2.3.4")
	if a1 != a2 || p1 != p2 {
		t.Fatalf("keys differ by case/whitespace: %q %q", a1, a2)
	}
}
