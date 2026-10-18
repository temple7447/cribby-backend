package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeResend(t *testing.T, status int) (*Resend, *[]map[string]any, *string) {
	t.Helper()
	var bodies []map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		bodies = append(bodies, m)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	t.Cleanup(srv.Close)
	r := NewResend("re_key", "Cribby <noreply@cribby.space>", 30, 45)
	r.Endpoint = srv.URL
	return r, &bodies, &auth
}

func TestVerificationEmailPayload(t *testing.T) {
	r, bodies, auth := fakeResend(t, 200)
	if err := r.SendVerification(context.Background(), "a@b.co", "123456"); err != nil {
		t.Fatal(err)
	}
	b := (*bodies)[0]
	if *auth != "Bearer re_key" || b["from"] != "Cribby <noreply@cribby.space>" || b["subject"] != "Your Cribby verification code" {
		t.Fatalf("auth=%q body=%v", *auth, b)
	}
	if to := b["to"].([]any); len(to) != 1 || to[0] != "a@b.co" {
		t.Fatalf("to=%v", b["to"])
	}
	html := b["html"].(string)
	if !strings.Contains(html, "<strong>123456</strong>") || !strings.Contains(html, "30 minutes") {
		t.Fatalf("html=%s", html)
	}
}

func TestPasswordResetEmailUsesResetExpiry(t *testing.T) {
	r, bodies, _ := fakeResend(t, 200)
	_ = r.SendPasswordReset(context.Background(), "a@b.co", "654321")
	html := (*bodies)[0]["html"].(string)
	if !strings.Contains(html, "654321") || !strings.Contains(html, "45 minutes") {
		t.Fatalf("html=%s", html)
	}
}

func TestBookingEmailsEscapeUserControlledText(t *testing.T) {
	r, bodies, _ := fakeResend(t, 200)
	_ = r.SendBookingRequest(context.Background(), "admin@x.co", `<script>alert(1)</script>`, `Studio & "Loft"`, "2026-09-01")
	html := (*bodies)[0]["html"].(string)
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("not escaped: %s", html)
	}
	if (*bodies)[0]["subject"] != `New booking request: Studio & "Loft"` {
		t.Fatalf("subject=%v", (*bodies)[0]["subject"])
	}
}

func TestBookingDecisionEmailWording(t *testing.T) {
	r, bodies, _ := fakeResend(t, 200)
	_ = r.SendBookingDecision(context.Background(), "s@x.co", "Loft", "approved")
	_ = r.SendBookingDecision(context.Background(), "s@x.co", "Loft", "rejected")
	if (*bodies)[0]["subject"] != "Your booking request was approved" || (*bodies)[1]["subject"] != "Your booking request was rejected" {
		t.Fatalf("got %v / %v", (*bodies)[0]["subject"], (*bodies)[1]["subject"])
	}
}

func TestResendErrorStatusIsReturned(t *testing.T) {
	r, _, _ := fakeResend(t, 403)
	if err := r.SendVerification(context.Background(), "a@b.co", "123456"); err == nil {
		t.Fatal("expected an error for a 403 from Resend")
	}
}

func TestNoAPIKeyLogsInsteadOfSending(t *testing.T) {
	r, bodies, _ := fakeResend(t, 200)
	r.APIKey = ""
	for _, err := range []error{
		r.SendVerification(context.Background(), "a@b.co", "123456"),
		r.SendPasswordReset(context.Background(), "a@b.co", "123456"),
		r.SendBookingRequest(context.Background(), "a@b.co", "n", "t", "d"),
		r.SendBookingDecision(context.Background(), "a@b.co", "t", "approved"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(*bodies) != 0 {
		t.Fatalf("sent %d requests with no API key", len(*bodies))
	}
}
