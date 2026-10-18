package api_test

import (
	"io"
	"net/http"
	"testing"
)

func httpGet(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s, nil
}

func corsReq(t *testing.T, e *env, method, origin string, preflight bool) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+"/api/v1/listings", nil)
	req.Header.Set("Origin", origin)
	if preflight {
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestCORS(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		origin  string
		allowed bool
	}{
		{"http://localhost:8081", true},
		{"https://admin.cribby.space", true},
		{"http://192.168.1.50:8081", true}, // LAN dev origin (phone testing the Expo web build)
		{"http://10.0.0.7:19006", true},
		{"http://172.20.3.4:3000", true},
		{"https://evil.example.com", false},
		{"http://172.40.3.4:3000", false},
		{"https://192.168.1.50:8081", false}, // https is not covered by the private-network rule
	}
	for _, c := range cases {
		simple := corsReq(t, e, "GET", c.origin, false)
		got := simple.Header.Get("Access-Control-Allow-Origin")
		if c.allowed && (got != c.origin || simple.Header.Get("Access-Control-Allow-Credentials") != "true") {
			t.Errorf("%s: simple request ACAO=%q", c.origin, got)
		}
		if !c.allowed && got != "" {
			t.Errorf("%s: should not be allowed, got ACAO=%q", c.origin, got)
		}
		if simple.Header.Get("Vary") != "Origin" {
			t.Errorf("%s: Vary=%q, want Origin", c.origin, simple.Header.Get("Vary"))
		}

		pre := corsReq(t, e, "OPTIONS", c.origin, true)
		switch {
		case c.allowed && (pre.StatusCode != 200 || pre.Header.Get("Access-Control-Allow-Headers") != "authorization,content-type"):
			t.Errorf("%s: preflight status=%d headers=%v", c.origin, pre.StatusCode, pre.Header)
		case !c.allowed && pre.StatusCode != 400:
			t.Errorf("%s: disallowed preflight status=%d, want 400", c.origin, pre.StatusCode)
		}
	}
}
