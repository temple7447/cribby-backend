package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/store"
)

type tokenResponse struct {
	AccessToken string     `json:"access_token"`
	TokenType   string     `json:"token_type"`
	User        model.User `json:"user"`
}

func (s *Server) tokenFor(user *model.User) (tokenResponse, error) {
	tok, err := s.newToken(user.Email)
	return tokenResponse{AccessToken: tok, TokenType: "bearer", User: *user}, err
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) error {
	p := struct {
		Email    string `json:"email" validate:"required,email"`
		Password string `json:"password" validate:"required"`
		Name     string `json:"name" validate:"required"`
		School   string `json:"school"`
	}{School: "State University"}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx := r.Context()
	exists, err := store.UserByEmail(ctx, s.DB.Executor, p.Email)
	if err != nil {
		return err
	}
	if exists != nil {
		return httpErr(http.StatusBadRequest, "Email already registered")
	}
	hash, err := security.HashPassword(p.Password)
	if err != nil {
		return err
	}
	user, err := store.CreateUser(ctx, s.DB.Executor, p.Email, hash, p.Name, p.School)
	if db.IsUniqueViolation(err) { // lost a race with a concurrent signup
		return httpErr(http.StatusBadRequest, "Email already registered")
	}
	if err != nil {
		return err
	}
	resp, err := s.tokenFor(user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		Email    string `json:"email" validate:"required,email"`
		Password string `json:"password" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	user, err := store.UserByEmail(r.Context(), s.DB.Executor, p.Email)
	if err != nil {
		return err
	}
	if user == nil || !security.VerifyPassword(p.Password, user.HashedPassword) {
		return httpErr(http.StatusUnauthorized, "Incorrect email or password")
	}
	if user.IsSuspended {
		// 403, not 401 — the credentials were correct, access is what's denied.
		return httpErr(http.StatusForbidden, "Your account has been suspended")
	}
	resp, err := s.tokenFor(user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) requestVerification(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	if user.VerificationStatus == "verified" {
		return httpErr(http.StatusBadRequest, "Already verified")
	}

	now := db.Now()
	if user.VerificationCodeLastSentAt != nil {
		elapsed := now.Sub(user.VerificationCodeLastSentAt.Time).Seconds()
		remaining := float64(s.cfg.EmailVerifyCooldownSeconds) - elapsed
		if remaining > 0 {
			return httpErr(http.StatusTooManyRequests,
				fmt.Sprintf("Please wait %ds before requesting another code", int(remaining)))
		}
	}

	code := security.GenerateCode()
	if err := s.mail.SendVerification(r.Context(), user.Email, code); err != nil {
		s.log.Error("sending verification email", "err", err)
		return httpErr(http.StatusBadGateway, "Couldn't send verification email — try again")
	}
	expires := db.Time{Time: now.Add(time.Duration(s.cfg.EmailVerifyExpireMinutes) * time.Minute)}
	if err := store.SetVerificationCode(r.Context(), s.DB.Executor, user.ID, security.HashCode(code), expires, now); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (s *Server) confirmVerification(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		Code string `json:"code" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if user.VerificationStatus == "verified" {
		return httpErr(http.StatusBadRequest, "Already verified")
	}
	if user.VerificationCodeHash == nil || user.VerificationCodeExpiresAt == nil {
		return httpErr(http.StatusBadRequest, "No verification code requested — request one first")
	}
	if time.Now().UTC().After(user.VerificationCodeExpiresAt.Time) {
		return httpErr(http.StatusBadRequest, "Verification code expired — request a new one")
	}
	if !security.CodesEqual(security.HashCode(strings.TrimSpace(p.Code)), *user.VerificationCodeHash) {
		return httpErr(http.StatusBadRequest, "Incorrect verification code")
	}
	if err := store.MarkVerified(r.Context(), s.DB.Executor, user.ID); err != nil {
		return err
	}
	updated, err := store.UserByID(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, updated)
	return nil
}

// requestPasswordReset is unauthenticated by design — it's how you get back in
// when you can't log in. It always answers 204 regardless of whether the
// account exists, the cooldown is active, or the email send fails: surfacing
// any of those asymmetrically would let someone enumerate registered emails.
func (s *Server) requestPasswordReset(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		Email string `json:"email" validate:"required,email"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx := r.Context()
	user, err := store.UserByEmail(ctx, s.DB.Executor, p.Email)
	if err != nil {
		return err
	}
	if user == nil {
		noContent(w)
		return nil
	}

	now := db.Now()
	if user.PasswordResetLastSentAt != nil &&
		now.Sub(user.PasswordResetLastSentAt.Time).Seconds() < float64(s.cfg.PasswordResetCooldownSecs) {
		noContent(w)
		return nil
	}

	code := security.GenerateCode()
	if err := s.mail.SendPasswordReset(ctx, user.Email, code); err != nil {
		s.log.Error("sending password reset email", "err", err)
		noContent(w)
		return nil
	}
	expires := db.Time{Time: now.Add(time.Duration(s.cfg.PasswordResetExpireMinutes) * time.Minute)}
	if err := store.SetPasswordResetCode(ctx, s.DB.Executor, user.ID, security.HashCode(code), expires, now); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (s *Server) confirmPasswordReset(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		Email       string `json:"email" validate:"required,email"`
		Code        string `json:"code" validate:"required"`
		NewPassword string `json:"new_password" validate:"required,min=6"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	invalid := httpErr(http.StatusBadRequest, "Invalid or expired code")
	ctx := r.Context()

	user, err := store.UserByEmail(ctx, s.DB.Executor, p.Email)
	if err != nil {
		return err
	}
	if user == nil || user.PasswordResetCodeHash == nil || user.PasswordResetExpiresAt == nil {
		return invalid
	}
	if time.Now().UTC().After(user.PasswordResetExpiresAt.Time) {
		return invalid
	}
	if !security.CodesEqual(security.HashCode(strings.TrimSpace(p.Code)), *user.PasswordResetCodeHash) {
		return invalid
	}

	hash, err := security.HashPassword(p.NewPassword)
	if err != nil {
		return err
	}
	if err := store.ResetPassword(ctx, s.DB.Executor, user.ID, hash); err != nil {
		return err
	}
	updated, err := store.UserByID(ctx, s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	resp, err := s.tokenFor(updated)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}
