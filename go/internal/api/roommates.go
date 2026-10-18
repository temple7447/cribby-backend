package api

import (
	"net/http"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

func (s *Server) listRoommates(w http.ResponseWriter, r *http.Request) error {
	profiles, err := store.ListLiveRoommates(r.Context(), s.DB.Executor)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profiles)
	return nil
}

// getMyRoommateProfile answers `null` (200) when the user has no profile yet.
func (s *Server) getMyRoommateProfile(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	profile, err := store.RoommateByUserID(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profile)
	return nil
}

func (s *Server) createRoommateProfile(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	p := struct {
		Name           string   `json:"name" validate:"required"`
		Age            int      `json:"age" validate:"required"`
		School         string   `json:"school" validate:"required"`
		Year           string   `json:"year" validate:"required"`
		BudgetMin      int      `json:"budget_min" validate:"required"`
		BudgetMax      int      `json:"budget_max" validate:"required"`
		MoveInDate     string   `json:"move_in_date" validate:"required"`
		Tags           []string `json:"tags"`
		Bio            string   `json:"bio"`
		LookingFor     string   `json:"looking_for"`
		PreferredAreas []string `json:"preferred_areas"`
		Emoji          string   `json:"emoji"`
		Grad           string   `json:"grad"`
	}{Emoji: "🙂", Grad: "from-violet to-mint"}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if user.VerificationStatus != "verified" {
		return httpErr(http.StatusForbidden, "Verify your student email to create a roommate profile")
	}
	ctx, ex := r.Context(), s.DB.Executor
	existing, err := store.RoommateByUserID(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return httpErr(http.StatusBadRequest, "Profile already exists")
	}
	created, err := store.CreateRoommate(ctx, ex, user.ID, &model.RoommateProfile{
		Name: p.Name, Age: p.Age, School: p.School, Year: p.Year, BudgetMin: p.BudgetMin, BudgetMax: p.BudgetMax,
		MoveInDate: p.MoveInDate, Tags: p.Tags, Bio: p.Bio, LookingFor: p.LookingFor,
		PreferredAreas: p.PreferredAreas, Emoji: p.Emoji, Grad: p.Grad,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, created)
	return nil
}

// A moderated profile's visibility can't be changed by its owner.
var (
	visibleStates   = map[string]bool{"live": true, "paused": true}
	moderatedStates = map[string]bool{"under_review": true, "removed": true}
)

func (s *Server) updateMyRoommateProfile(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		Bio            *string              `json:"bio"`
		Tags           *db.JSONList[string] `json:"tags"`
		LookingFor     *string              `json:"looking_for"`
		PreferredAreas *db.JSONList[string] `json:"preferred_areas"`
		Visibility     *string              `json:"visibility"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	profile, err := store.RoommateByUserID(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	if profile == nil {
		return httpErr(http.StatusNotFound, "No roommate profile yet")
	}
	if p.Visibility != nil {
		if !visibleStates[*p.Visibility] {
			return httpErr(http.StatusBadRequest, "Invalid visibility value")
		}
		if moderatedStates[profile.Visibility] {
			return httpErr(http.StatusForbidden, "This profile is under review and its visibility can't be changed")
		}
	}
	if err := store.UpdateRoommate(ctx, ex, profile.ID, store.RoommateUpdate{
		Bio: p.Bio, LookingFor: p.LookingFor, Visibility: p.Visibility, Tags: p.Tags, PreferredAreas: p.PreferredAreas,
	}); err != nil {
		return err
	}
	return s.respondRoommate(w, r, profile.ID)
}

// uploadMyRoommateBanner replaces the cover photo of this user's own post.
// Self-service, no admin gate — a student manages only their own post's banner.
func (s *Server) uploadMyRoommateBanner(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	up, err := parseUpload(w, r)
	if err != nil {
		return err
	}
	defer up.close()

	ctx, ex := r.Context(), s.DB.Executor
	profile, err := store.RoommateByUserID(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	if profile == nil {
		return httpErr(http.StatusNotFound, "No roommate profile yet")
	}
	uploaded, err := s.uploadImage(ctx, up, "cribby/roommate-banners/"+itoa(profile.ID), storage.TransformBanner)
	if err != nil {
		return err
	}
	old := profile.BannerPublicID
	if err := store.SetRoommateBanner(ctx, ex, profile.ID, &uploaded.URL, &uploaded.PublicID); err != nil {
		return err
	}
	s.destroyQuietly(ctx, old)
	return s.respondRoommate(w, r, profile.ID)
}

// deleteMyRoommateBanner removes the cover photo, reverting to the emoji/gradient.
func (s *Server) deleteMyRoommateBanner(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	profile, err := store.RoommateByUserID(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	if profile == nil {
		return httpErr(http.StatusNotFound, "No roommate profile yet")
	}
	old := profile.BannerPublicID
	if err := store.SetRoommateBanner(ctx, ex, profile.ID, nil, nil); err != nil {
		return err
	}
	s.destroyQuietly(ctx, old)
	return s.respondRoommate(w, r, profile.ID)
}

func (s *Server) getRoommate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt(r, "roommate_id")
	if err != nil {
		return err
	}
	profile, err := store.RoommateByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return err
	}
	if profile == nil {
		return httpErr(http.StatusNotFound, "Roommate profile not found")
	}
	writeJSON(w, http.StatusOK, profile)
	return nil
}

func (s *Server) respondRoommate(w http.ResponseWriter, r *http.Request, id int) error {
	profile, err := store.RoommateByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profile)
	return nil
}
