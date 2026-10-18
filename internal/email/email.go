// Package email sends transactional mail through Resend's HTTP API. With no
// RESEND_API_KEY it logs instead of sending, so local dev needs no credentials.
// The message designs live in templates.go.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// send delivers a message with both HTML and plain-text bodies. With no API key it logs
// (the verification and reset codes too, so local dev works with zero setup).
func (r *Resend) send(ctx context.Context, to string, m Message, logCode string) error {
	if r.APIKey == "" {
		// Warn, not info: this must be visible with zero logging setup for the
		// console fallback to be useful in local dev.
		if logCode != "" {
			slog.Warn("RESEND_API_KEY not set — email not sent", "to", to, "subject", m.Subject, "code", logCode)
		} else {
			slog.Warn("RESEND_API_KEY not set — email not sent", "to", to, "subject", m.Subject)
		}
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"from": r.From, "to": []string{to}, "subject": m.Subject, "html": m.HTML, "text": m.Text,
	})
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
	return r.send(ctx, to, VerificationEmail(code, r.VerifyExpiryMinutes), code)
}

func (r *Resend) SendPasswordReset(ctx context.Context, to, code string) error {
	return r.send(ctx, to, PasswordResetEmail(code, r.ResetExpiryMinutes), code)
}

// SendBookingRequest notifies an admin (standing in for the listing's
// landlord — there's no real landlord-account model yet) of a new booking.
func (r *Resend) SendBookingRequest(ctx context.Context, adminEmail, studentName, listingTitle, moveInDate string) error {
	return r.send(ctx, adminEmail, BookingRequestEmail(studentName, listingTitle, moveInDate), "")
}

func (r *Resend) SendBookingDecision(ctx context.Context, studentEmail, listingTitle, status string) error {
	return r.send(ctx, studentEmail, BookingDecisionEmail(listingTitle, status), "")
}
