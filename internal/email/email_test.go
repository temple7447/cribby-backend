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

// ---- sending ----

func TestVerificationEmailIsSentWithHTMLAndPlainText(t *testing.T) {
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
	html, text := b["html"].(string), b["text"].(string)
	if !strings.Contains(html, "<!DOCTYPE html>") || !strings.Contains(html, "30 minutes") {
		t.Fatalf("html lacks the layout/expiry")
	}
	if !strings.Contains(text, "1 2 3 4 5 6") || !strings.Contains(text, "30 minutes") || strings.Contains(text, "<") {
		t.Fatalf("plain text version is wrong:\n%s", text)
	}
}

func TestPasswordResetEmailUsesResetExpiry(t *testing.T) {
	r, bodies, _ := fakeResend(t, 200)
	_ = r.SendPasswordReset(context.Background(), "a@b.co", "654321")
	b := (*bodies)[0]
	if b["subject"] != "Reset your Cribby password" || !strings.Contains(b["html"].(string), "45 minutes") {
		t.Fatalf("got %v", b["subject"])
	}
}

func TestBookingDecisionSubjects(t *testing.T) {
	r, bodies, _ := fakeResend(t, 200)
	_ = r.SendBookingDecision(context.Background(), "s@x.co", "Loft", "approved")
	_ = r.SendBookingDecision(context.Background(), "s@x.co", "Loft", "rejected")
	if (*bodies)[0]["subject"] != "Your booking request was approved 🎉" || (*bodies)[1]["subject"] != "An update on your booking request" {
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

// ---- the designs ----

func allMessages() map[string]Message {
	return map[string]Message{
		"verification": VerificationEmail("482913", 30),
		"reset":        PasswordResetEmail("076241", 45),
		"request":      BookingRequestEmail("Maya Chen", "Sunny Studio", "2026-12-01"),
		"approved":     BookingDecisionEmail("Sunny Studio", "approved"),
		"rejected":     BookingDecisionEmail("Sunny Studio", "rejected"),
	}
}

func TestEveryEmailIsAWellFormedBrandedDocument(t *testing.T) {
	for name, m := range allMessages() {
		h := m.HTML
		for _, want := range []string{"<!DOCTYPE html>", `lang="en"`, "viewport", "https://www.cribby.space/logo-mark.png", "Cribby",
			"#1A1A2E", "Help &amp; support", "https://www.cribby.space/support", "https://www.cribby.space/privacy"} {
			if !strings.Contains(h, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
		if strings.Contains(h, "{{") || strings.Contains(h, "ZgotmplZ") || strings.Contains(h, "<no value>") {
			t.Errorf("%s: unrendered template output", name)
		}
		if strings.Contains(h, "<script") || strings.Contains(h, "javascript:") {
			t.Errorf("%s: scripts have no place in an email", name)
		}
		if m.Subject == "" || m.Text == "" || !strings.Contains(m.Text, "Cribby") || strings.Contains(m.Text, "<") {
			t.Errorf("%s: subject/plain-text version missing or contains HTML", name)
		}
		// the hidden inbox-preview line is present
		if !strings.Contains(h, "display:none") {
			t.Errorf("%s: no preheader", name)
		}
	}
}

func TestCodeEmailsShowEachDigitInItsOwnBox(t *testing.T) {
	for name, want := range map[string][]string{"verification": {"4", "8", "2", "9", "1", "3"}, "reset": {"0", "7", "6", "2", "4", "1"}} {
		h := allMessages()[name].HTML
		if got := strings.Count(h, `width="38"`); got != 6 {
			t.Fatalf("%s: %d digit boxes, want 6", name, got)
		}
		pos := 0
		for _, d := range want {
			i := strings.Index(h[pos:], ">"+d+"</td>")
			if i < 0 {
				t.Fatalf("%s: digit %q not found in order", name, d)
			}
			pos += i + 1
		}
		if !strings.Contains(h, "never ask you") {
			t.Errorf("%s: missing the anti-phishing warning", name)
		}
	}
}

func TestBookingRequestEmailDetails(t *testing.T) {
	m := allMessages()["request"]
	for _, want := range []string{"Maya Chen", "Sunny Studio", "Tue, Dec 1, 2026", "Review request", "https://admin.cribby.space/bookings"} {
		if !strings.Contains(m.HTML, want) {
			t.Errorf("missing %q", want)
		}
	}
	if m.Subject != "New booking request: Sunny Studio" {
		t.Fatalf("subject %q", m.Subject)
	}
	if !strings.Contains(m.Text, "Student: Maya Chen") || !strings.Contains(m.Text, "Review request: https://admin.cribby.space/bookings") {
		t.Fatalf("plain text:\n%s", m.Text)
	}
	// a date that isn't YYYY-MM-DD is shown as given rather than dropped
	if !strings.Contains(BookingRequestEmail("A", "B", "next spring").HTML, "next spring") {
		t.Fatal("unparseable move-in date should be shown as-is")
	}
}

func TestApprovedAndRejectedLookAndSayDifferentThings(t *testing.T) {
	a, r := allMessages()["approved"], allMessages()["rejected"]
	if !strings.Contains(a.HTML, "You&#39;re approved!") || !strings.Contains(a.HTML, "#3ECF8E") || !strings.Contains(a.HTML, "Never pay a deposit outside the Cribby app") {
		t.Error("approved email is missing its celebration/safety copy")
	}
	if !strings.Contains(r.HTML, "Not this time") || !strings.Contains(r.HTML, "#7C5CFF") || !strings.Contains(r.HTML, "Find another home") {
		t.Error("rejected email is missing its gentle copy")
	}
	if strings.Contains(r.HTML, "You&#39;re approved") || strings.Contains(a.HTML, "Not this time") {
		t.Error("approved and rejected must not share copy")
	}
	// anything that isn't exactly "approved" is treated as a rejection, never as an approval
	if odd := BookingDecisionEmail("X", "weird"); !strings.Contains(odd.HTML, "Not this time") {
		t.Error("an unknown status must never produce an approval email")
	}
}

func TestUserSuppliedTextIsEscaped(t *testing.T) {
	evil := `<script>alert(1)</script> & "quotes" <img src=x onerror=alert(2)>`
	for name, m := range map[string]Message{
		"request":  BookingRequestEmail(evil, evil, evil),
		"approved": BookingDecisionEmail(evil, "approved"),
		"rejected": BookingDecisionEmail(evil, "rejected"),
	} {
		if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, "<img src=x") {
			t.Errorf("%s: unescaped HTML from user input", name)
		}
		if !strings.Contains(m.HTML, "&lt;script&gt;") {
			t.Errorf("%s: the text should still appear, escaped", name)
		}
		// the plain-text version has no HTML context, so the text is kept verbatim there
		if !strings.Contains(m.Text, "<script>") {
			t.Errorf("%s: plain text should carry the original text", name)
		}
	}
}

func TestExpiryWording(t *testing.T) {
	if !strings.Contains(VerificationEmail("123456", 1).HTML, "1 minute.") {
		t.Error("singular minute")
	}
	if !strings.Contains(VerificationEmail("123456", 10).HTML, "10 minutes.") {
		t.Error("plural minutes")
	}
}
