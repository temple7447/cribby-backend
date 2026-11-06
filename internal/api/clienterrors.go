package api

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"campus-backend/internal/store"
)

const maxErrorReportsPerHour = 60 // per address: enough for a bad session, too few to flood

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// reportClientError receives a crash or error from the app or website. It needs no sign-in (a crash
// can happen before one), is throttled by address, and answers 204 either way so a failing
// reporter never causes a second failure on the device.
func (s *Server) reportClientError(w http.ResponseWriter, r *http.Request) error {
	var p struct {
		Source     string `json:"source" validate:"required,oneof=app|web"`
		Message    string `json:"message" validate:"required"`
		Stack      string `json:"stack"`
		Route      string `json:"route"`
		AppVersion string `json:"app_version"`
		Platform   string `json:"platform"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if ok, wait := s.errReports.take(clientIP(r), maxErrorReportsPerHour, time.Now()); !ok {
		return tooManyRequests("Too many error reports.", wait)
	}
	rep := store.ClientErrorReport{
		Source: p.Source, Message: clipRunes(p.Message, 500), Stack: clipRunes(p.Stack, 4000),
		Route: clipRunes(p.Route, 200), AppVersion: clipRunes(p.AppVersion, 40), Platform: clipRunes(p.Platform, 40),
	}
	if rep.Message == "" {
		return httpErr(http.StatusUnprocessableEntity, "message is empty")
	}
	// Attach the signed-in user, if there is one and the token is good; never fail on it.
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		if u, err := s.currentUser(r); err == nil {
			rep.UserID = &u.ID
		}
	}
	if err := store.RecordClientError(r.Context(), s.DB.Executor, rep); err != nil {
		s.log.Warn("recording client error failed", "err", err)
	}
	noContent(w)
	return nil
}

func (s *Server) adminClientErrors(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	resolved := newQuery(r).str("status") == "resolved"
	out, err := store.ListClientErrors(r.Context(), s.DB.Executor, resolved, 200)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminResolveClientError(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "error_id")
	if err != nil {
		return err
	}
	var p struct {
		Resolved bool `json:"resolved"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	found, err := store.SetClientErrorResolved(r.Context(), s.DB.Executor, id, p.Resolved)
	if err != nil {
		return err
	}
	if !found {
		return httpErr(http.StatusNotFound, "Error report not found")
	}
	noContent(w)
	return nil
}

// ready answers the uptime monitor: unlike /health (which only proves the process is up) it also
// proves the database answers.
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var one int
	if err := s.DB.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
		s.log.Error("readiness check: database unavailable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "db": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": "ok"})
}
