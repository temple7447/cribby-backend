package api

import (
	"net/http"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Student ID check: for students without a university email. They send their school and a photo of
// their student ID; an admin looks at it and approves or rejects. Approved students get the "Verified
// student" badge on their roommate profile. The photo is private, only admins see it, and it is
// deleted as soon as an admin decides.

func (s *Server) myStudentVerification(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	v, err := store.StudentVerificationFor(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	if v == nil {
		v = &model.StudentVerification{UserID: user.ID, Status: "none"}
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

// submitStudentVerification: multipart with `file` (the student ID photo) and `school`.
func (s *Server) submitStudentVerification(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	up, err := parseUpload(w, r)
	if err != nil {
		return err
	}
	defer up.close()
	school := strings.TrimSpace(r.FormValue("school"))
	if school == "" || len([]rune(school)) > 120 {
		return httpErr(http.StatusUnprocessableEntity, "Enter your school (120 characters max)")
	}
	ctx, ex := r.Context(), s.DB.Executor
	prev, err := store.StudentVerificationFor(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	if prev != nil && prev.Status == "verified" {
		return httpErr(http.StatusConflict, "You are already a verified student")
	}
	data, err := up.readImage()
	if err != nil {
		return err
	}
	if !s.photos.Configured() {
		return httpErr(http.StatusServiceUnavailable, "Photo storage is not configured")
	}
	uploaded, err := s.photos.UploadPrivate(ctx, data, "cribby/student-ids")
	if err != nil {
		return err
	}
	if err := store.SubmitStudentVerification(ctx, ex, user.ID, school, uploaded.PublicID); err != nil {
		_ = s.photos.DestroyPrivate(ctx, uploaded.PublicID)
		return err
	}
	if prev != nil && prev.IDPhotoPublicID != "" {
		if err := s.photos.DestroyPrivate(ctx, prev.IDPhotoPublicID); err != nil {
			s.log.Warn("could not delete old student ID photo", "err", err)
		}
	}
	return s.myStudentVerification(w, r)
}

type adminStudentVerification struct {
	model.StudentVerification
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IDPhotoURL  string `json:"id_photo_url"` // signed; only for admins; empty once decided
}

func (s *Server) adminStudentVerifications(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "verified" && status != "rejected" {
		return httpErr(http.StatusUnprocessableEntity, "status must be pending, verified or rejected")
	}
	ctx, ex := r.Context(), s.DB.Executor
	rows, err := store.ListStudentVerifications(ctx, ex, status, 200)
	if err != nil {
		return err
	}
	out := make([]adminStudentVerification, 0, len(rows))
	for _, v := range rows {
		item := adminStudentVerification{StudentVerification: v}
		if u, err := store.UserByID(ctx, ex, v.UserID); err == nil && u != nil {
			item.Name, item.DisplayName, item.Email = u.Name, u.DisplayName, u.Email
		}
		if v.IDPhotoPublicID != "" {
			item.IDPhotoURL = s.photos.SignedURL(v.IDPhotoPublicID)
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminDecideStudentVerification(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	userID, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	p := struct {
		Decision string `json:"decision" validate:"required,oneof=approve|reject"`
		Note     string `json:"note"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	note := strings.TrimSpace(p.Note)
	if len([]rune(note)) > maxReviewNoteLen {
		return httpErr(http.StatusUnprocessableEntity, "Note is too long (500 characters max)")
	}
	ctx, ex := r.Context(), s.DB.Executor
	v, err := store.StudentVerificationFor(ctx, ex, userID)
	if err != nil {
		return err
	}
	if v == nil {
		return httpErr(http.StatusNotFound, "No student check from this person")
	}
	approve := p.Decision == "approve"
	if !approve && note == "" {
		return httpErr(http.StatusUnprocessableEntity, "Tell them why it was rejected")
	}
	if err := store.DecideStudentVerification(ctx, ex, userID, approve, note); err != nil {
		return err
	}
	if v.IDPhotoPublicID != "" { // the check is done; don't keep an ID photo around
		if err := s.photos.DestroyPrivate(ctx, v.IDPhotoPublicID); err != nil {
			s.log.Warn("could not delete student ID photo", "err", err)
		}
	}
	v, err = store.StudentVerificationFor(ctx, ex, userID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}
