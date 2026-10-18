package api

import (
	"context"
	"net/http"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// ---- favorites ----

func (s *Server) listFavorites(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	listings, err := store.ListFavoriteListings(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, listings)
	return nil
}

func (s *Server) addFavorite(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	listingID, err := pathInt(r, "listing_id")
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	if l, err := store.ListingByID(ctx, ex, listingID); err != nil {
		return err
	} else if l == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	if err := store.AddFavorite(ctx, ex, user.ID, listingID); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (s *Server) removeFavorite(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	listingID, err := pathInt(r, "listing_id")
	if err != nil {
		return err
	}
	if err := store.RemoveFavorite(r.Context(), s.DB.Executor, user.ID, listingID); err != nil {
		return err
	}
	noContent(w)
	return nil
}

// ---- conversations & messages ----

var (
	errVerifyToMessage = httpErr(http.StatusForbidden, "Verify your student email to message roommates")
	errCannotMessage   = httpErr(http.StatusForbidden, "You can't message this user")
)

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	convos, err := store.ListConversationsForUser(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, convos)
	return nil
}

func (s *Server) startConversation(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		Kind              string `json:"kind" validate:"required"` // landlord | roommate
		ListingID         *int   `json:"listing_id"`
		RoommateProfileID *int   `json:"roommate_profile_id"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor

	if p.Kind == "roommate" {
		if user.VerificationStatus != "verified" {
			return errVerifyToMessage
		}
		var counterpart *int
		if p.RoommateProfileID != nil {
			profile, err := store.RoommateByID(ctx, ex, *p.RoommateProfileID)
			if err != nil {
				return err
			}
			if profile != nil {
				counterpart = &profile.UserID
			}
		}
		if counterpart != nil && *counterpart == user.ID {
			return httpErr(http.StatusBadRequest, "You can't start a conversation with yourself")
		}
		if counterpart != nil {
			if err := s.rejectIfBlocked(ctx, user.ID, *counterpart); err != nil {
				return err
			}
		}
	}
	convo, err := store.FindOrCreateConversation(ctx, ex, user.ID, p.Kind, p.ListingID, p.RoommateProfileID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, convo)
	return nil
}

func (s *Server) rejectIfBlocked(ctx context.Context, a, b int) error {
	blocked, err := store.BlockExistsEitherDirection(ctx, s.DB.Executor, a, b)
	if err != nil {
		return err
	}
	if blocked {
		return errCannotMessage
	}
	return nil
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "conversation_id")
	if err != nil {
		return err
	}
	convo, err := store.ConversationForUser(r.Context(), s.DB.Executor, id, user.ID)
	if err != nil {
		return err
	}
	if convo == nil {
		return httpErr(http.StatusNotFound, "Conversation not found")
	}
	writeJSON(w, http.StatusOK, convo)
	return nil
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "conversation_id")
	if err != nil {
		return err
	}
	var p struct {
		Text string `json:"text" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	convo, err := store.ConversationForUser(ctx, ex, id, user.ID)
	if err != nil {
		return err
	}
	if convo == nil {
		return httpErr(http.StatusNotFound, "Conversation not found")
	}
	if convo.Kind == "roommate" {
		if user.VerificationStatus != "verified" {
			return errVerifyToMessage
		}
		if convo.CounterpartUserID != nil {
			if err := s.rejectIfBlocked(ctx, user.ID, *convo.CounterpartUserID); err != nil {
				return err
			}
		}
	}
	msg, err := store.AddMessage(ctx, ex, convo.ID, "me", p.Text)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, msg)
	return nil
}

// ---- blocks ----

func (s *Server) listBlocks(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	blocks, err := store.ListBlocksFor(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, blocks)
	return nil
}

func (s *Server) createBlock(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		BlockedUserID int `json:"blocked_user_id" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if p.BlockedUserID == user.ID {
		return httpErr(http.StatusBadRequest, "You can't block yourself")
	}
	ctx, ex := r.Context(), s.DB.Executor
	target, err := store.UserByID(ctx, ex, p.BlockedUserID)
	if err != nil {
		return err
	}
	if target == nil {
		return httpErr(http.StatusNotFound, "User not found")
	}
	existing, err := store.BlockPair(ctx, ex, user.ID, p.BlockedUserID)
	if err != nil {
		return err
	}
	if existing == nil {
		if existing, err = store.CreateBlock(ctx, ex, user.ID, p.BlockedUserID); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, existing)
	return nil
}

func (s *Server) deleteBlock(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "blocked_user_id")
	if err != nil {
		return err
	}
	if err := store.DeleteBlock(r.Context(), s.DB.Executor, user.ID, id); err != nil {
		return err
	}
	noContent(w)
	return nil
}

// ---- reports ----

var (
	reportTargetTypes = map[string]bool{"listing": true, "roommate_profile": true, "user": true}
	reportReasons     = map[string]bool{"spam": true, "scam": true, "inappropriate": true, "fake": true, "other": true}
	reportStatuses    = map[string]bool{"pending": true, "reviewing": true, "resolved": true, "dismissed": true}
)

func enrichAll(ctx context.Context, ex db.Executor, reports []model.Report) ([]model.Report, error) {
	out := make([]model.Report, len(reports))
	for i, r := range reports {
		e, err := store.EnrichReport(ctx, ex, r)
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

func (s *Server) createReport(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var p struct {
		TargetType string  `json:"target_type" validate:"required"`
		TargetID   int     `json:"target_id" validate:"required"`
		Reason     string  `json:"reason" validate:"required"`
		Details    *string `json:"details"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if !reportTargetTypes[p.TargetType] {
		return httpErr(http.StatusBadRequest, "Invalid target_type")
	}
	if !reportReasons[p.Reason] {
		return httpErr(http.StatusBadRequest, "Invalid reason")
	}

	ctx, ex := r.Context(), s.DB.Executor
	var found bool
	switch p.TargetType {
	case "listing":
		t, err := store.ListingByID(ctx, ex, p.TargetID)
		if err != nil {
			return err
		}
		found = t != nil
	case "roommate_profile":
		t, err := store.RoommateByID(ctx, ex, p.TargetID)
		if err != nil {
			return err
		}
		found = t != nil
	default: // "user"
		if p.TargetID == user.ID {
			return httpErr(http.StatusBadRequest, "You can't report yourself")
		}
		t, err := store.UserByID(ctx, ex, p.TargetID)
		if err != nil {
			return err
		}
		found = t != nil
	}
	if !found {
		return httpErr(http.StatusNotFound, "Report target not found")
	}

	report, err := store.CreateReport(ctx, ex, user.ID, p.TargetType, p.TargetID, p.Reason, p.Details)
	if err != nil {
		return err
	}

	// Reporting auto-hides the content pending review.
	switch p.TargetType {
	case "roommate_profile":
		if t, err := store.RoommateByID(ctx, ex, p.TargetID); err != nil {
			return err
		} else if t != nil && t.Visibility == "live" {
			if err := store.SetRoommateVisibility(ctx, ex, t.ID, "under_review"); err != nil {
				return err
			}
		}
	case "listing":
		if t, err := store.ListingByID(ctx, ex, p.TargetID); err != nil {
			return err
		} else if t != nil && !t.IsHidden {
			if err := store.SetListingHidden(ctx, ex, t.ID, true); err != nil {
				return err
			}
		}
	}

	enriched, err := store.EnrichReport(ctx, ex, *report)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, enriched)
	return nil
}

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	q := newQuery(r)
	status := q.optStr("status")
	if err := q.err(); err != nil {
		return err
	}
	if status != nil && !reportStatuses[*status] {
		return httpErr(http.StatusBadRequest, "Invalid status")
	}
	ctx, ex := r.Context(), s.DB.Executor
	reports, err := store.ListReports(ctx, ex, status)
	if err != nil {
		return err
	}
	out, err := enrichAll(ctx, ex, reports)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) updateReport(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "report_id")
	if err != nil {
		return err
	}
	var p struct {
		Status string `json:"status" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if !reportStatuses[p.Status] {
		return httpErr(http.StatusBadRequest, "Invalid status")
	}
	ctx, ex := r.Context(), s.DB.Executor
	report, err := store.ReportByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if report == nil {
		return httpErr(http.StatusNotFound, "Report not found")
	}

	var resolvedAt *db.Time
	var resolvedBy *int
	if p.Status == "resolved" || p.Status == "dismissed" {
		now := db.Time{Time: time.Now().UTC().Truncate(time.Microsecond)}
		resolvedAt, resolvedBy = &now, &admin.ID
	}

	// Known limitation (carried over from the original): if multiple reports
	// target the same content, dismissing one un-hides it even if another report
	// on the same target is still open. Acceptable at current report volume;
	// revisit with a count-of-open-reports guard if needed.
	err = s.DB.InTx(ctx, func(tx db.Executor) error {
		if err := s.applyReportOutcome(ctx, tx, report, p.Status); err != nil {
			return err
		}
		return store.UpdateReportStatus(ctx, tx, id, p.Status, resolvedAt, resolvedBy)
	})
	if err != nil {
		return err
	}

	updated, err := store.ReportByID(ctx, ex, id)
	if err != nil {
		return err
	}
	enriched, err := store.EnrichReport(ctx, ex, *updated)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, enriched)
	return nil
}

// applyReportOutcome restores or removes the reported content when a moderator
// dismisses or resolves a report.
func (s *Server) applyReportOutcome(ctx context.Context, tx db.Executor, report *model.Report, status string) error {
	switch report.TargetType {
	case "roommate_profile":
		profile, err := store.RoommateByID(ctx, tx, report.TargetID)
		if err != nil || profile == nil {
			return err
		}
		if status == "dismissed" && profile.Visibility == "under_review" {
			return store.SetRoommateVisibility(ctx, tx, profile.ID, "live")
		}
		if status == "resolved" {
			return store.SetRoommateVisibility(ctx, tx, profile.ID, "removed")
		}
	case "listing":
		listing, err := store.ListingByID(ctx, tx, report.TargetID)
		if err != nil || listing == nil {
			return err
		}
		if status == "dismissed" {
			return store.SetListingHidden(ctx, tx, listing.ID, false)
		}
		if status == "resolved" {
			return store.SetListingHidden(ctx, tx, listing.ID, true)
		}
	}
	return nil
}
