package api

import (
	"net/http"
	"regexp"
	"strings"

	"campus-backend/internal/store"
)

// Referral links: an admin gives a group leader a code; people who sign up with it are counted, and so
// are their booking requests and the rentals a landlord approves. Commission is paid by hand, after a rental.

var referralCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$`)

func cleanCode(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// attachReferral records the code a new person signed up with. Unknown or switched-off codes are ignored:
// a bad link must never stop someone from creating an account.
func (s *Server) attachReferral(r *http.Request, userID int, raw string) {
	code := cleanCode(raw)
	if code == "" || !referralCodeRe.MatchString(code) {
		return
	}
	ctx, ex := r.Context(), s.DB.Executor
	c, err := store.ReferralCodeByCode(ctx, ex, code)
	if err != nil || c == nil || !c.Active || c.OwnerID == userID {
		return
	}
	if err := store.RecordReferral(ctx, ex, userID, code); err != nil {
		s.log.Warn("could not record referral", "err", err)
	}
}

// myReferrals: the signed-in person's codes with their results (for group leaders).
func (s *Server) myReferrals(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	out, err := store.ListReferralCodes(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminListReferrals(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	out, err := store.ListReferralCodes(r.Context(), s.DB.Executor, 0)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminSaveReferral(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	code := cleanCode(r.PathValue("code"))
	if !referralCodeRe.MatchString(code) {
		return httpErr(http.StatusUnprocessableEntity, "A code is 3-30 letters, numbers or dashes")
	}
	p := struct {
		OwnerEmail string `json:"owner_email" validate:"required"`
		Label      string `json:"label"`
		CampusSlug string `json:"campus_slug"`
		Active     *bool  `json:"active"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	email, _ := normalizeEmail(p.OwnerEmail)
	owner, err := store.UserByEmail(ctx, ex, email)
	if err != nil {
		return err
	}
	if owner == nil {
		return httpErr(http.StatusNotFound, "No account with that email. They need to sign up to Cribby first.")
	}
	label := strings.TrimSpace(p.Label)
	if len([]rune(label)) > 120 {
		return httpErr(http.StatusUnprocessableEntity, "Label is too long (120 characters max)")
	}
	campus := strings.ToLower(strings.TrimSpace(p.CampusSlug))
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	if err := store.SaveReferralCode(ctx, ex, code, owner.ID, label, campus, active); err != nil {
		return err
	}
	return s.adminListReferrals(w, r)
}
