package api

import (
	"net/http"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// The scam alert board: students report fake agents, landlords and listings. Each report is checked by an
// admin before it is public (so nobody can post a false accusation), and the suspect's phone number is only
// ever shown masked. Anyone can check a number to see whether it has been reported.

const maxScamAlertsPerDay = 3

var scamKinds = map[string]bool{"fake_agent": true, "fake_landlord": true, "fake_listing": true, "advance_fee": true, "other": true}

// maskPhone keeps the first four and last three digits: 0803•••4567 becomes 0803•••567.
func maskPhone(p string) string {
	r := []rune(p)
	if len(r) < 8 {
		return ""
	}
	return string(r[:4]) + "•••" + string(r[len(r)-3:])
}

func publicScam(a model.ScamAlert) model.ScamAlert {
	a.SuspectPhone = maskPhone(a.SuspectPhone)
	a.ReviewNote = ""
	return a
}

// listScamAlerts is public. ?phone= shows the alerts about one number.
func (s *Server) listScamAlerts(w http.ResponseWriter, r *http.Request) error {
	phone := ""
	if raw := newQuery(r).str("phone"); raw != "" {
		if phone = normalizePhone(raw); phone == "" {
			return httpErr(http.StatusUnprocessableEntity, "Enter a valid phone number")
		}
	}
	rows, err := store.ListScamAlerts(r.Context(), s.DB.Executor, "approved", phone, 100)
	if err != nil {
		return err
	}
	out := make([]model.ScamAlert, len(rows))
	for i, a := range rows {
		out[i] = publicScam(a)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) myScamAlerts(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	rows, err := store.ListScamAlertsByUser(r.Context(), s.DB.Executor, user.ID, 50)
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].SuspectPhone = maskPhone(rows[i].SuspectPhone)
	}
	writeJSON(w, http.StatusOK, rows)
	return nil
}

func (s *Server) createScamAlert(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		Kind         string `json:"kind" validate:"required"`
		SuspectName  string `json:"suspect_name"`
		SuspectPhone string `json:"suspect_phone"`
		Area         string `json:"area"`
		Details      string `json:"details" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if !scamKinds[p.Kind] {
		return httpErr(http.StatusUnprocessableEntity, "Choose what kind of scam it was")
	}
	name, area, details := strings.TrimSpace(p.SuspectName), strings.TrimSpace(p.Area), strings.TrimSpace(p.Details)
	phone := ""
	if strings.TrimSpace(p.SuspectPhone) != "" {
		if phone = normalizePhone(p.SuspectPhone); phone == "" {
			return httpErr(http.StatusUnprocessableEntity, "That phone number doesn't look right")
		}
	}
	if name == "" && phone == "" {
		return httpErr(http.StatusUnprocessableEntity, "Give a name or a phone number so others can recognise them")
	}
	if len([]rune(details)) < 20 {
		return httpErr(http.StatusUnprocessableEntity, "Tell us what happened (at least 20 characters)")
	}
	if len([]rune(details)) > 1500 || len([]rune(name)) > 120 || len([]rune(area)) > 120 {
		return httpErr(http.StatusUnprocessableEntity, "One of the fields is too long")
	}
	ctx, ex := r.Context(), s.DB.Executor
	n, err := store.CountRecentScamAlerts(ctx, ex, user.ID, db.Time{Time: time.Now().UTC().Add(-24 * time.Hour)})
	if err != nil {
		return err
	}
	if n >= maxScamAlertsPerDay {
		return httpErr(http.StatusTooManyRequests, "You've sent a few reports today — please try again tomorrow")
	}
	id, err := store.CreateScamAlert(ctx, ex, model.ScamAlert{UserID: user.ID, Kind: p.Kind, SuspectName: name, SuspectPhone: phone, Area: area, Details: details})
	if err != nil {
		return err
	}
	a, err := store.ScamAlertByID(ctx, ex, id)
	if err != nil {
		return err
	}
	a.SuspectPhone = maskPhone(a.SuspectPhone)
	writeJSON(w, http.StatusCreated, a)
	return nil
}

type adminScamAlert struct {
	model.ScamAlert
	SuspectPhoneFull string `json:"suspect_phone_full"`
	ReporterEmail    string `json:"reporter_email"`
	OtherReports     int    `json:"other_reports"` // approved alerts about the same number
}

func (s *Server) adminScamAlerts(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		return httpErr(http.StatusUnprocessableEntity, "status must be pending, approved or rejected")
	}
	ctx, ex := r.Context(), s.DB.Executor
	rows, err := store.ListScamAlerts(ctx, ex, status, "", 200)
	if err != nil {
		return err
	}
	out := make([]adminScamAlert, 0, len(rows))
	for _, a := range rows {
		item := adminScamAlert{ScamAlert: a, SuspectPhoneFull: a.SuspectPhone}
		item.SuspectPhone = maskPhone(a.SuspectPhone)
		if u, err := store.UserByID(ctx, ex, a.UserID); err == nil && u != nil {
			item.ReporterEmail = u.Email
		}
		if a.SuspectPhone != "" {
			if same, err := store.ListScamAlerts(ctx, ex, "approved", a.SuspectPhone, 50); err == nil {
				item.OtherReports = len(same)
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminDecideScamAlert(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "alert_id")
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
	approve := p.Decision == "approve"
	if !approve && note == "" {
		return httpErr(http.StatusUnprocessableEntity, "Say why it was rejected")
	}
	ctx, ex := r.Context(), s.DB.Executor
	a, err := store.ScamAlertByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if a == nil {
		return httpErr(http.StatusNotFound, "Alert not found")
	}
	if err := store.DecideScamAlert(ctx, ex, id, approve, note); err != nil {
		return err
	}
	a, _ = store.ScamAlertByID(ctx, ex, id)
	a.SuspectPhone = maskPhone(a.SuspectPhone)
	writeJSON(w, http.StatusOK, a)
	return nil
}
