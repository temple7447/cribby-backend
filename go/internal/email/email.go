// Package email sends transactional mail through Resend's HTTP API. With no
// RESEND_API_KEY it logs instead of sending, so local dev needs no credentials.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Sender is the set of emails the app sends; tests substitute a fake.
type Sender interface {
	SendVerification(ctx context.Context, to, code string) error
	SendPasswordReset(ctx context.Context, to, code string) error
	SendBookingRequest(ctx context.Context, adminEmail, studentName, listingTitle, moveInDate string) error
	SendBookingDecision(ctx context.Context, studentEmail, listingTitle, status string) error
}

type Resend struct {
	APIKey, From                            string
	VerifyExpiryMinutes, ResetExpiryMinutes int
	Endpoint                                string // overridable for tests
	Client                                  *http.Client
}

func NewResend(apiKey, from string, verifyMinutes, resetMinutes int) *Resend {
	return &Resend{
		APIKey: apiKey, From: from, VerifyExpiryMinutes: verifyMinutes, ResetExpiryMinutes: resetMinutes,
		Endpoint: "https://api.resend.com/emails",
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *Resend) send(ctx context.Context, to, subject, htmlBody string) error {
	payload, _ := json.Marshal(map[string]any{"from": r.From, "to": []string{to}, "subject": subject, "html": htmlBody})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("resend: %s: %s", resp.Status, body)
	}
	return nil
}

func (r *Resend) SendVerification(ctx context.Context, to, code string) error {
	if r.APIKey == "" {
		// Warn, not info: this must be visible with zero logging setup for the
		// console fallback to be useful in local dev.
		slog.Warn("RESEND_API_KEY not set — verification code", "to", to, "code", code)
		return nil
	}
	return r.send(ctx, to, "Your Cribby verification code",
		fmt.Sprintf("<p>Your verification code is <strong>%s</strong>.</p><p>It expires in %d minutes.</p>",
			html.EscapeString(code), r.VerifyExpiryMinutes))
}

func (r *Resend) SendPasswordReset(ctx context.Context, to, code string) error {
	if r.APIKey == "" {
		slog.Warn("RESEND_API_KEY not set — password reset code", "to", to, "code", code)
		return nil
	}
	return r.send(ctx, to, "Reset your Cribby password",
		fmt.Sprintf("<p>Your password reset code is <strong>%s</strong>.</p>"+
			"<p>It expires in %d minutes. If you didn't request this, you can safely ignore this email.</p>",
			html.EscapeString(code), r.ResetExpiryMinutes))
}

// SendBookingRequest notifies an admin (standing in for the listing's
// landlord — there's no real landlord-account model yet) of a new booking.
func (r *Resend) SendBookingRequest(ctx context.Context, adminEmail, studentName, listingTitle, moveInDate string) error {
	subject := "New booking request: " + listingTitle
	if r.APIKey == "" {
		slog.Warn("RESEND_API_KEY not set — email not sent", "to", adminEmail, "subject", subject)
		return nil
	}
	return r.send(ctx, adminEmail, subject, fmt.Sprintf(
		"<p><strong>%s</strong> requested to book <strong>%s</strong>, move-in %s.</p>"+
			"<p>Review it in the Cribby app under Profile → Pending bookings.</p>",
		html.EscapeString(studentName), html.EscapeString(listingTitle), html.EscapeString(moveInDate)))
}

func (r *Resend) SendBookingDecision(ctx context.Context, studentEmail, listingTitle, status string) error {
	verb := "rejected"
	if status == "approved" {
		verb = "approved"
	}
	subject := "Your booking request was " + verb
	if r.APIKey == "" {
		slog.Warn("RESEND_API_KEY not set — email not sent", "to", studentEmail, "subject", subject)
		return nil
	}
	return r.send(ctx, studentEmail, subject, fmt.Sprintf(
		"<p>Your request for <strong>%s</strong> was <strong>%s</strong>.</p><p>Check the app for details and next steps.</p>",
		html.EscapeString(listingTitle), verb))
}
