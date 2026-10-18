// Command emailpreview renders every Cribby email to an HTML file so the designs can
// be opened in a browser (or screenshotted) without sending anything:
//
//	go run ./cmd/emailpreview ./email-previews
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"campus-backend/internal/email"
)

func main() {
	dir := "email-previews"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	previews := []struct {
		file string
		m    email.Message
	}{
		{"1-verification", email.VerificationEmail("482913", 30)},
		{"2-password-reset", email.PasswordResetEmail("076241", 30)},
		{"3-booking-request", email.BookingRequestEmail("Maya Chen", "Sunny Studio on Maple St", "2026-12-01")},
		{"4-booking-approved", email.BookingDecisionEmail("Sunny Studio on Maple St", "approved")},
		{"5-booking-rejected", email.BookingDecisionEmail("Sunny Studio on Maple St", "rejected")},
	}
	for _, p := range previews {
		must(os.WriteFile(filepath.Join(dir, p.file+".html"), []byte(p.m.HTML), 0o644))
		must(os.WriteFile(filepath.Join(dir, p.file+".txt"), []byte(p.m.Text), 0o644))
		fmt.Printf("%-22s subject: %s\n", p.file, p.m.Subject)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
