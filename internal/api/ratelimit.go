package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// attemptLimiter is an in-memory sliding-window counter keyed by string, used to slow down
// guessing: failed sign-ins, wrong one-time codes, bursts of sign-ups. It is per-instance, which
// is fine for a single Render instance, and it forgets everything on restart.
type attemptLimiter struct {
	mu        sync.Mutex
	hits      map[string][]time.Time
	window    time.Duration
	lastSweep time.Time
}

func newAttemptLimiter(window time.Duration) *attemptLimiter {
	return &attemptLimiter{hits: map[string][]time.Time{}, window: window}
}

// recent drops expired hits for the key and returns what is left. The caller holds the lock.
func (l *attemptLimiter) recent(key string, now time.Time) []time.Time {
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = kept
	return kept
}

// status reports how many attempts the key has in the window and, if any, how long until the
// oldest one expires (the soonest a blocked caller could try again).
func (l *attemptLimiter) status(key string, now time.Time) (n int, retry time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.recent(key, now)
	if len(r) == 0 {
		return 0, 0
	}
	return len(r), l.window - now.Sub(r[0])
}

func (l *attemptLimiter) record(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.recent(key, now), now)
	// Don't let keys from one-off callers pile up forever.
	if now.Sub(l.lastSweep) > l.window {
		l.lastSweep = now
		for k := range l.hits {
			l.recent(k, now)
		}
	}
}

func (l *attemptLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// take is a plain rate limit: it records the attempt and reports whether the key is still
// within `max` per window.
func (l *attemptLimiter) take(key string, max int, now time.Time) (ok bool, retry time.Duration) {
	if n, wait := l.status(key, now); n >= max {
		return false, wait
	}
	l.record(key, now)
	return true, 0
}

// tooManyRequests is a 429 that tells the client when to try again.
func tooManyRequests(msg string, retry time.Duration) *APIError {
	secs := int(retry.Seconds()) + 1
	if secs < 1 {
		secs = 1
	}
	return &APIError{
		Status: http.StatusTooManyRequests,
		Detail: fmt.Sprintf("%s Try again in %s.", msg, waitText(secs)),
		Header: http.Header{"Retry-After": {fmt.Sprint(secs)}},
	}
}

func waitText(secs int) string {
	if secs >= 90 {
		return fmt.Sprintf("%d minutes", (secs+59)/60)
	}
	return fmt.Sprintf("%d seconds", secs)
}

// ---- limits ----

const (
	failWindow = 15 * time.Minute
	// Failed sign-ins before we stop answering: per account+address (the usual forgotten-password
	// case), per account from anywhere (a spread-out attack), and per address (one machine trying
	// many accounts). Campus Wi-Fi puts many students behind one address, so that last one is high.
	maxFailsAccountAtIP = 5
	maxFailsAccount     = 20
	maxFailsIP          = 40
	// One-time codes are 6 digits, so only a handful of wrong guesses are allowed per window.
	maxCodeGuesses = 5

	maxSignupsPerIP = 30 // per hour
	maxResetsPerIP  = 15 // password-reset emails per hour
)

type authLimits struct {
	loginFails *attemptLimiter // keyed ip:/acct:/pair:
	codeFails  *attemptLimiter // wrong verification / reset codes
	signups    *attemptLimiter
	resets     *attemptLimiter
}

func newAuthLimits() *authLimits {
	return &authLimits{
		loginFails: newAttemptLimiter(failWindow),
		codeFails:  newAttemptLimiter(failWindow),
		signups:    newAttemptLimiter(time.Hour),
		resets:     newAttemptLimiter(time.Hour),
	}
}

func loginKeys(ident, ip string) (acct, pair, byIP string) {
	ident = strings.ToLower(strings.TrimSpace(ident))
	return "acct:" + ident, "pair:" + ident + "|" + ip, "ip:" + ip
}

// loginBlocked reports whether this sign-in attempt should be refused outright.
func (a *authLimits) loginBlocked(ident, ip string, now time.Time) (bool, time.Duration) {
	acct, pair, byIP := loginKeys(ident, ip)
	var longest time.Duration
	blocked := false
	for _, c := range []struct {
		key string
		max int
	}{{pair, maxFailsAccountAtIP}, {acct, maxFailsAccount}, {byIP, maxFailsIP}} {
		if n, wait := a.loginFails.status(c.key, now); n >= c.max {
			blocked = true
			if wait > longest {
				longest = wait
			}
		}
	}
	return blocked, longest
}

func (a *authLimits) loginFailed(ident, ip string, now time.Time) {
	acct, pair, byIP := loginKeys(ident, ip)
	for _, k := range []string{acct, pair, byIP} {
		a.loginFails.record(k, now)
	}
}

// loginSucceeded forgets the earlier typos for this person (but not the address-wide count).
func (a *authLimits) loginSucceeded(ident, ip string) {
	acct, pair, _ := loginKeys(ident, ip)
	a.loginFails.clear(acct)
	a.loginFails.clear(pair)
}
