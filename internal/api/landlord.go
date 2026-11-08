package api

import (
	"net/http"
	"regexp"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Landlord identity check: a landlord sends a phone number and a photo of an ID; an admin looks at
// it and approves or rejects. Approved landlords get a "verified" badge on every listing they post.
// The ID photo is stored as a private image and is only ever shown to signed-in admins.

var landlordIDTypes = map[string]bool{"nin": true, "drivers_license": true, "voters_card": true, "passport": true, "other": true}

var phoneRe = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

// normalizePhone strips spaces and dashes; it returns "" when the result doesn't look like a phone number.
func normalizePhone(s string) string {
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(s))
	if !phoneRe.MatchString(s) {
		return ""
	}
	return s
}

// myLandlordVerification: where the signed-in person stands. Never includes the photo.
func (s *Server) myLandlordVerification(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	v, err := store.LandlordVerificationFor(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	if v == nil {
		v = &model.LandlordVerification{UserID: user.ID, Status: "none"}
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

// submitLandlordVerification: multipart with `file` (the ID photo), `phone` and `id_type`.
func (s *Server) submitLandlordVerification(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	up, err := parseUpload(w, r)
	if err != nil {
		return err
	}
	defer up.close()
	phone := normalizePhone(r.FormValue("phone"))
	if phone == "" {
		return httpErr(http.StatusUnprocessableEntity, "Enter a valid phone number")
	}
	idType := strings.TrimSpace(r.FormValue("id_type"))
	if !landlordIDTypes[idType] {
		return httpErr(http.StatusUnprocessableEntity, "Choose which ID you are sending")
	}
	ctx, ex := r.Context(), s.DB.Executor
	prev, err := store.LandlordVerificationFor(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	if prev != nil && prev.Status == "verified" {
		return httpErr(http.StatusConflict, "You are already verified")
	}
	data, err := up.readImage()
	if err != nil {
		return err
	}
	if !s.photos.Configured() {
		return httpErr(http.StatusServiceUnavailable, "Photo storage is not configured")
	}
	uploaded, err := s.photos.UploadPrivate(r.Context(), data, "cribby/landlord-ids")
	if err != nil {
		return err
	}
	if err := store.SubmitLandlordVerification(ctx, ex, user.ID, phone, idType, uploaded.PublicID); err != nil {
		_ = s.photos.DestroyPrivate(ctx, uploaded.PublicID)
		return err
	}
	if prev != nil && prev.IDPhotoPublicID != "" { // the old photo is no longer needed
		if err := s.photos.DestroyPrivate(ctx, prev.IDPhotoPublicID); err != nil {
			s.log.Warn("could not delete old ID photo", "err", err)
		}
	}
	return s.myLandlordVerification(w, r)
}

type adminLandlordVerification struct {
	model.LandlordVerification
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IDPhotoURL  string `json:"id_photo_url"` // signed; only for admins
}

func (s *Server) adminLandlordVerifications(w http.ResponseWriter, r *http.Request) error {
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
	rows, err := store.ListLandlordVerifications(ctx, ex, status, 200)
	if err != nil {
		return err
	}
	out := make([]adminLandlordVerification, 0, len(rows))
	for _, v := range rows {
		item := adminLandlordVerification{LandlordVerification: v}
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

func (s *Server) adminDecideLandlordVerification(w http.ResponseWriter, r *http.Request) error {
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
	v, err := store.LandlordVerificationFor(ctx, ex, userID)
	if err != nil {
		return err
	}
	if v == nil {
		return httpErr(http.StatusNotFound, "No verification request from this person")
	}
	approve := p.Decision == "approve"
	if !approve && note == "" {
		return httpErr(http.StatusUnprocessableEntity, "Tell them why it was rejected")
	}
	if err := store.DecideLandlordVerification(ctx, ex, userID, approve, note); err != nil {
		return err
	}
	v, err = store.LandlordVerificationFor(ctx, ex, userID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}
