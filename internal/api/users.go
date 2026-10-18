package api

import (
	"net/http"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

func (s *Server) readMe(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, user)
	return nil
}

// updateMe only touches name, school and avatar_emoji — verification_status is
// deliberately not settable here (closes the old self-verify exploit).
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		Name        *string `json:"name"`
		School      *string `json:"school"`
		AvatarEmoji *string `json:"avatar_emoji"`
		SchoolID    *int    `json:"school_id"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	// Choosing a school also sets the display name, so any free-text `school`
	// sent alongside is ignored in favour of the chosen school's name.
	if p.SchoolID != nil {
		picked, err := store.SchoolByID(r.Context(), s.DB.Executor, *p.SchoolID)
		if err != nil {
			return err
		}
		if picked == nil {
			return httpErr(http.StatusBadRequest, "Unknown school_id")
		}
		if err := store.SetUserSchool(r.Context(), s.DB.Executor, user.ID, picked); err != nil {
			return err
		}
		s.refineSchool(r.Context(), picked) // pin it to its exact Google location, if not already
		p.School = nil
	}
	if err := store.UpdateUserProfile(r.Context(), s.DB.Executor, user.ID, p.Name, p.School, p.AvatarEmoji); err != nil {
		return err
	}
	updated, err := store.UserByID(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, updated)
	return nil
}

// deleteMe permanently deletes the account and everything it owns. Irreversible
// — there's no soft-delete/undo, matching what "delete my account" implies.
func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		// Re-confirming the password protects against an unattended, left-open
		// session being used to instantly and irreversibly delete the account.
		Password string `json:"password" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if !security.VerifyPassword(p.Password, user.HashedPassword) {
		return httpErr(http.StatusUnauthorized, "Incorrect password")
	}
	err = s.DB.InTx(r.Context(), func(tx db.Executor) error {
		return store.DeleteUserCascade(r.Context(), tx, user.ID)
	})
	if err != nil {
		return err
	}
	noContent(w)
	return nil
}

// uploadMyAvatar replaces the profile photo. avatar_emoji stays as the fallback
// everywhere this is unset — uploading one just switches which the app renders.
func (s *Server) uploadMyAvatar(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	up, err := parseUpload(w, r)
	if err != nil {
		return err
	}
	defer up.close()

	ctx := r.Context()
	uploaded, err := s.uploadImage(ctx, up, "cribby/avatars/"+itoa(user.ID), storage.TransformAvatar)
	if err != nil {
		return err
	}
	old := user.AvatarPublicID
	if err := store.SetAvatar(ctx, s.DB.Executor, user.ID, &uploaded.URL, &uploaded.PublicID); err != nil {
		return err
	}
	s.destroyQuietly(ctx, old)

	updated, err := store.UserByID(ctx, s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, updated)
	return nil
}

// deleteMyAvatar removes the profile photo, reverting to the emoji fallback.
func (s *Server) deleteMyAvatar(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	old := user.AvatarPublicID
	if err := store.SetAvatar(ctx, s.DB.Executor, user.ID, nil, nil); err != nil {
		return err
	}
	s.destroyQuietly(ctx, old)

	updated, err := store.UserByID(ctx, s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, updated)
	return nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	users, err := store.ListUsers(r.Context(), s.DB.Executor)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, users)
	return nil
}

// getUserDetail returns everything about one account in a single call —
// bookings, roommate profile, and reports both filed by and against them — so
// the dashboard's user page doesn't need four round trips.
func (s *Server) getUserDetail(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	target, err := store.UserByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if target == nil {
		return httpErr(http.StatusNotFound, "User not found")
	}

	detail := model.AdminUserDetail{User: *target}
	if detail.Bookings, err = store.ListBookingsForUser(ctx, ex, id, nil); err != nil {
		return err
	}
	if detail.RoommateProfile, err = store.RoommateByUserID(ctx, ex, id); err != nil {
		return err
	}
	filed, err := store.ListReportsFiledBy(ctx, ex, id)
	if err != nil {
		return err
	}
	against, err := store.ListReportsAgainstUser(ctx, ex, id)
	if err != nil {
		return err
	}
	if detail.ReportsFiled, err = enrichAll(ctx, ex, filed); err != nil {
		return err
	}
	if detail.ReportsAgainst, err = enrichAll(ctx, ex, against); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, detail)
	return nil
}

func (s *Server) setAdmin(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	var p struct {
		IsAdmin bool `json:"is_admin" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if id == admin.ID && !p.IsAdmin {
		return httpErr(http.StatusBadRequest, "You can't remove your own admin access")
	}
	return s.updateUserFlag(w, r, id, func() error { return store.SetAdmin(r.Context(), s.DB.Executor, id, p.IsAdmin) })
}

func (s *Server) setSuspended(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	var p struct {
		IsSuspended bool `json:"is_suspended" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if id == admin.ID && p.IsSuspended {
		return httpErr(http.StatusBadRequest, "You can't suspend your own account")
	}
	return s.updateUserFlag(w, r, id, func() error { return store.SetSuspended(r.Context(), s.DB.Executor, id, p.IsSuspended) })
}

// updateUserFlag 404s on an unknown user, applies the change, and returns the user.
func (s *Server) updateUserFlag(w http.ResponseWriter, r *http.Request, id int, apply func() error) error {
	ctx := r.Context()
	target, err := store.UserByID(ctx, s.DB.Executor, id)
	if err != nil {
		return err
	}
	if target == nil {
		return httpErr(http.StatusNotFound, "User not found")
	}
	if err := apply(); err != nil {
		return err
	}
	updated, err := store.UserByID(ctx, s.DB.Executor, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, updated)
	return nil
}
