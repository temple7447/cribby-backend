package api

import (
	"net/http"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

const maxReviewBody = 1000

// Reviews: anyone can read a home's reviews; people who booked or messaged about the home can leave
// one (one each, editable). The listing's rating and review count follow along.

func (s *Server) reviewableListing(r *http.Request) (*model.Listing, error) {
	id, err := pathInt(r, "listing_id")
	if err != nil {
		return nil, err
	}
	l, err := store.ListingByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if l == nil || l.Status != model.ListingApproved {
		return nil, httpErr(http.StatusNotFound, "Listing not found")
	}
	return l, nil
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) error {
	l, err := s.reviewableListing(r)
	if err != nil {
		return err
	}
	viewer, canReview := 0, false
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		if u, err := s.currentUser(r); err == nil {
			viewer = u.ID
			if !(l.OwnerID != nil && *l.OwnerID == u.ID) {
				canReview, _ = store.CanReviewListing(r.Context(), s.DB.Executor, u.ID, l.ID)
			}
		}
	}
	items, err := store.ListReviews(r.Context(), s.DB.Executor, l.ID, viewer, 100)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rating": l.Rating, "count": l.Reviews, "can_review": canReview, "items": items,
	})
	return nil
}

func (s *Server) saveReview(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	l, err := s.reviewableListing(r)
	if err != nil {
		return err
	}
	p := struct {
		Rating int    `json:"rating"`
		Body   string `json:"body"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	if p.Rating < 1 || p.Rating > 5 {
		return httpErr(http.StatusUnprocessableEntity, "Rating must be 1 to 5 stars")
	}
	body := strings.TrimSpace(p.Body)
	if len([]rune(body)) > maxReviewBody {
		return httpErr(http.StatusUnprocessableEntity, "Review is too long (1000 characters max)")
	}
	if l.OwnerID != nil && *l.OwnerID == user.ID {
		return httpErr(http.StatusForbidden, "You can't review your own listing")
	}
	ctx, ex := r.Context(), s.DB.Executor
	ok, err := store.CanReviewListing(ctx, ex, user.ID, l.ID)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusForbidden, "Message the landlord or request this home before reviewing it")
	}
	if err := store.UpsertReview(ctx, ex, l.ID, user.ID, p.Rating, body); err != nil {
		return err
	}
	return s.listReviews(w, r)
}

func (s *Server) deleteReview(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	rid, err := pathInt(r, "review_id")
	if err != nil {
		return err
	}
	author := user.ID
	if user.IsAdmin {
		author = 0
	}
	ok, err := store.DeleteReview(r.Context(), s.DB.Executor, rid, author)
	if err != nil {
		return err
	}
	if !ok {
		return httpErr(http.StatusNotFound, "Review not found")
	}
	noContent(w)
	return nil
}
