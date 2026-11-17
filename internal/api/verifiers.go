package api

import (
	"net/http"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Verifiers: trusted people (cousins, ambassadors) who visit homes and mark them "Verified".
// An admin adds them; they can't verify their own listings.

// verifierTier turns the number of homes someone has checked into a tier and what the next one needs.
func verifierTier(n int) (tier string, next int) {
	switch {
	case n >= 40:
		return "gold", 0
	case n >= 15:
		return "silver", 40
	case n >= 5:
		return "bronze", 15
	default:
		return "starter", 5
	}
}

func (s *Server) canVerify(r *http.Request, user *model.User) (bool, error) {
	if user.IsAdmin {
		return true, nil
	}
	return store.IsVerifier(r.Context(), s.DB.Executor, user.ID)
}

// verifierMe tells the app whether to show verifier tools.
func (s *Server) verifierMe(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	ok, err := s.canVerify(r, user)
	if err != nil {
		return err
	}
	out := map[string]any{"is_verifier": ok, "verified_count": 0, "tier": "", "next_tier_at": 0}
	if ok {
		if v, err := store.VerifierFor(r.Context(), s.DB.Executor, user.ID); err != nil {
			return err
		} else if v != nil {
			out["verified_count"] = v.VerifiedCount
			out["tier"], out["next_tier_at"] = verifierTier(v.VerifiedCount)
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// verifierQueue: live homes nobody has checked yet.
func (s *Server) verifierQueue(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	ok, err := s.canVerify(r, user)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusForbidden, "Only verifiers can see this")
	}
	out, err := store.UnverifiedLiveListings(r.Context(), s.DB.Executor, user.ID, 100)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminListVerifiers(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	out, err := store.ListVerifiers(r.Context(), s.DB.Executor)
	if err != nil {
		return err
	}
	for i := range out {
		out[i].Tier, out[i].NextTierAt = verifierTier(out[i].VerifiedCount)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminSetVerifier(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	userID, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	p := struct {
		Kind   string `json:"kind" validate:"required,oneof=cousin|ambassador"`
		Campus string `json:"campus"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	if u, err := store.UserByID(ctx, ex, userID); err != nil {
		return err
	} else if u == nil {
		return httpErr(http.StatusNotFound, "User not found")
	}
	if err := store.SetVerifier(ctx, ex, userID, p.Kind, strings.TrimSpace(p.Campus)); err != nil {
		return err
	}
	return s.adminListVerifiers(w, r)
}

func (s *Server) adminRemoveVerifier(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	userID, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	if err := store.RemoveVerifier(r.Context(), s.DB.Executor, userID); err != nil {
		return err
	}
	noContent(w)
	return nil
}
