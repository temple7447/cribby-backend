package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Saved searches: a student saves their filters and gets an email when a new matching home goes live (at most
// one email a day per search). The homes that already match when the search is saved are counted as seen, so the
// first email is about something new.

const (
	maxSavedSearches = 5
	alertGap         = 24 * time.Hour
	alertTitles      = 5
)

func (s *Server) listSavedSearches(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	rows, err := store.SavedSearchesFor(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rows)
	return nil
}

func (s *Server) createSavedSearch(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	if user.VerificationStatus != "verified" {
		return httpErr(http.StatusForbidden, "Confirm your email first, so we know where to send the alerts")
	}
	var in struct {
		Name             string   `json:"name"`
		MaxPrice         *int     `json:"max_price"`
		MaxWalk          *int     `json:"max_walk"`
		RoomType         string   `json:"room_type"`
		Amenities        []string `json:"amenities"`
		Verified         bool     `json:"verified"`
		LandlordVerified bool     `json:"landlord_verified"`
		Water            string   `json:"water"`
		Power            string   `json:"power"`
	}
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "My search"
	}
	if len([]rune(name)) > 80 {
		return httpErr(http.StatusUnprocessableEntity, "That name is too long (80 characters max)")
	}
	switch in.Water {
	case "", "tap", "borehole", "well", "running":
	default:
		return httpErr(http.StatusUnprocessableEntity, "water must be tap, borehole, well or running")
	}
	switch in.Power {
	case "", "always", "most":
	default:
		return httpErr(http.StatusUnprocessableEntity, "power must be always or most")
	}
	room := strings.TrimSpace(in.RoomType)
	if strings.EqualFold(room, "any") {
		room = ""
	}
	if len([]rune(room)) > 40 || len(in.Amenities) > 20 {
		return httpErr(http.StatusUnprocessableEntity, "Too many filters")
	}
	var amenities []string
	for _, a := range in.Amenities {
		if a = strings.TrimSpace(strings.ReplaceAll(a, "|", " ")); a != "" && len([]rune(a)) <= 40 {
			amenities = append(amenities, a)
		}
	}
	x := model.SavedSearch{UserID: user.ID, Name: name, MaxPrice: in.MaxPrice, MaxWalk: in.MaxWalk, RoomType: room, Amenities: amenities,
		VerifiedOnly: in.Verified, LandlordVerifiedOnly: in.LandlordVerified, Water: in.Water, Power: in.Power}
	ctx, ex := r.Context(), s.DB.Executor
	if n, err := store.CountSavedSearches(ctx, ex, user.ID); err != nil {
		return err
	} else if n >= maxSavedSearches {
		return httpErr(http.StatusConflict, "You can keep up to 5 alerts. Delete one to add another")
	}
	id, err := store.CreateSavedSearch(ctx, ex, x)
	if err != nil {
		return err
	}
	// What matches now is old news.
	now, err := store.SearchListings(ctx, ex, store.SavedSearchFilter(x))
	if err != nil {
		return err
	}
	ids := make([]int, len(now))
	for i, l := range now {
		ids[i] = l.ID
	}
	if err := store.MarkSeen(ctx, ex, id, ids); err != nil {
		return err
	}
	all, err := store.SavedSearchesFor(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	for _, sv := range all {
		if sv.ID == id {
			writeJSON(w, http.StatusCreated, sv)
			return nil
		}
	}
	return httpErr(http.StatusInternalServerError, "Couldn't save the search")
}

func (s *Server) deleteSavedSearch(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "search_id")
	if err != nil {
		return err
	}
	ok, err := store.DeleteSavedSearch(r.Context(), s.DB.Executor, id, user.ID)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusNotFound, "Search not found")
	}
	noContent(w)
	return nil
}

// SweepSearchAlerts emails people about homes that went live since their last alert. A search that was emailed in
// the last day waits, and the homes it missed are included in its next email.
func (s *Server) SweepSearchAlerts(ctx context.Context, now time.Time) {
	ex := s.DB.Executor
	searches, err := store.AllSavedSearches(ctx, ex)
	if err != nil {
		s.log.Error("search alerts failed", "err", err)
		return
	}
	for _, x := range searches {
		if x.LastSentAt != nil && now.Sub(x.LastSentAt.Time) < alertGap {
			continue
		}
		matches, err := store.SearchListings(ctx, ex, store.SavedSearchFilter(x))
		if err != nil {
			s.log.Warn("search alert lookup failed", "search", x.ID, "err", err)
			continue
		}
		seen, err := store.SeenListings(ctx, ex, x.ID)
		if err != nil {
			continue
		}
		var fresh []int
		var titles []string
		for _, l := range matches {
			if seen[l.ID] {
				continue
			}
			fresh = append(fresh, l.ID)
			if len(titles) < alertTitles {
				titles = append(titles, l.Title)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		u, err := store.UserByID(ctx, ex, x.UserID)
		if err != nil || u == nil || u.IsSuspended || u.VerificationStatus != "verified" {
			continue
		}
		if err := s.mail.SendSearchAlert(ctx, u.Email, x.Name, titles, len(fresh)); err != nil {
			s.log.Warn("search alert email failed", "to", u.Email, "err", err) // tried again next hour
			continue
		}
		if err := store.MarkSeen(ctx, ex, x.ID, fresh); err != nil {
			s.log.Error("search alert not recorded", "search", x.ID, "err", err)
			continue
		}
		_ = store.SetSavedSearchSent(ctx, ex, x.ID)
	}
}

// RunSearchAlerts checks every hour until ctx ends.
func (s *Server) RunSearchAlerts(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SweepSearchAlerts(ctx, time.Now().UTC())
		}
	}
}
