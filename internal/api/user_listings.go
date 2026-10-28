package api

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Students and landlords can post their own apartments. A post starts "pending" and only
// becomes public once an admin approves it — so fake or scam listings never go live
// unreviewed. Editing it (text, price, location or photos) sends it back for review.

const (
	maxListingPhotos   = 10
	maxPostsPerDay     = 5
	walkSpeedKMH       = 5.0
	walkDetourFactor   = 1.3 // streets aren't straight lines
	maxDescriptionLen  = 2000
	maxReviewNoteLen   = 500
	minTitleLen        = 4
	maxTitleLen        = 120
	maxAddressLen      = 300
	maxListingTypeLen  = 40
	maxListingAmenity  = 40
	maxListingAmenitys = 20
)

// listingInput is what a user can set on their own listing.
type listingInput struct {
	Title            string   `json:"title"`
	Type             string   `json:"type"`
	PricePer         int      `json:"price_per"`
	Total            *int     `json:"total"`
	UtilitiesMonthly int      `json:"utilities_monthly"`
	Deposit          int      `json:"deposit"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	Address          string   `json:"address"`
	Description      string   `json:"description"`
	Amenities        []string `json:"amenities"`
}

func (in *listingInput) clean() error {
	in.Title = strings.TrimSpace(in.Title)
	in.Type = strings.TrimSpace(in.Type)
	in.Address = strings.TrimSpace(in.Address)
	in.Description = strings.TrimSpace(in.Description)
	switch n := utf8.RuneCountInString(in.Title); {
	case n < minTitleLen:
		return httpErr(http.StatusUnprocessableEntity, "Give your listing a title (at least 4 characters)")
	case n > maxTitleLen:
		return httpErr(http.StatusUnprocessableEntity, "Title is too long (120 characters max)")
	}
	if in.Type == "" || utf8.RuneCountInString(in.Type) > maxListingTypeLen {
		return httpErr(http.StatusUnprocessableEntity, "Choose the type of place")
	}
	if in.PricePer <= 0 || in.PricePer > 100_000_000 {
		return httpErr(http.StatusUnprocessableEntity, "Enter the rent per person")
	}
	if in.Total != nil && (*in.Total < in.PricePer || *in.Total > 1_000_000_000) {
		return httpErr(http.StatusUnprocessableEntity, "Total rent can't be less than the rent per person")
	}
	if in.Deposit < 0 || in.UtilitiesMonthly < 0 {
		return httpErr(http.StatusUnprocessableEntity, "Deposit and utilities can't be negative")
	}
	if in.Latitude == nil || in.Longitude == nil || math.IsNaN(*in.Latitude) || math.IsNaN(*in.Longitude) ||
		*in.Latitude < -90 || *in.Latitude > 90 || *in.Longitude < -180 || *in.Longitude > 180 {
		return httpErr(http.StatusUnprocessableEntity, "Pin the apartment's location on the map")
	}
	if utf8.RuneCountInString(in.Address) > maxAddressLen {
		return httpErr(http.StatusUnprocessableEntity, "Address is too long")
	}
	if utf8.RuneCountInString(in.Description) > maxDescriptionLen {
		return httpErr(http.StatusUnprocessableEntity, "Description is too long (2000 characters max)")
	}
	amenities := make([]string, 0, len(in.Amenities))
	for _, a := range in.Amenities {
		if a = strings.TrimSpace(a); a != "" && utf8.RuneCountInString(a) <= maxListingAmenity {
			amenities = append(amenities, a)
		}
	}
	if len(amenities) > maxListingAmenitys {
		amenities = amenities[:maxListingAmenitys]
	}
	in.Amenities = amenities
	return nil
}

// walkMinutes estimates the walk from the poster's school, or 0 when we don't know it.
func (s *Server) walkMinutes(ctx context.Context, user *model.User, lat, lng float64) int {
	slat, slng, err := s.schoolOrigin(ctx, user.SchoolID)
	if err != nil || slat == nil || slng == nil {
		return 0
	}
	km := geo.HaversineKM(*slat, *slng, lat, lng) * walkDetourFactor
	return int(math.Min(999, math.Ceil(km/walkSpeedKMH*60)))
}

func (s *Server) postUserListing(w http.ResponseWriter, r *http.Request, user *model.User) error {
	var in listingInput
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	if err := in.clean(); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	n, err := store.CountRecentByOwner(ctx, ex, user.ID, db.Time{Time: time.Now().UTC().Add(-24 * time.Hour)})
	if err != nil {
		return err
	}
	if n >= maxPostsPerDay {
		return httpErr(http.StatusTooManyRequests, "You've posted a lot today — please try again tomorrow")
	}
	total := in.PricePer
	if in.Total != nil {
		total = *in.Total
	}
	owner := user.ID
	created, err := store.CreateListing(ctx, ex, &model.Listing{
		Title: in.Title, Type: in.Type, PricePer: in.PricePer, Total: total, UtilitiesMonthly: in.UtilitiesMonthly,
		Deposit: in.Deposit, Walk: s.walkMinutes(ctx, user, *in.Latitude, *in.Longitude),
		Latitude: in.Latitude, Longitude: in.Longitude, Grad: "from-coral to-sunshine", Emoji: "🏠",
		Amenities: in.Amenities, LandlordName: user.Name, OwnerID: &owner, Status: model.ListingPending,
		Description: in.Description, Address: in.Address,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, created)
	return nil
}

// listMyListings: everything the signed-in user has posted, with its review status.
func (s *Server) listMyListings(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	out, err := store.ListingsByOwner(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// updateListing: the owner (or an admin) edits a listing. A non-admin edit goes back
// into the review queue.
func (s *Server) updateListing(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	l, err := s.listingForOwner(r, user)
	if err != nil {
		return err
	}
	if l.Status == model.ListingRemoved {
		return httpErr(http.StatusConflict, "This listing was taken down — post it again instead")
	}
	var in listingInput
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	if err := in.clean(); err != nil {
		return err
	}
	l.Title, l.Type, l.PricePer, l.UtilitiesMonthly, l.Deposit = in.Title, in.Type, in.PricePer, in.UtilitiesMonthly, in.Deposit
	l.Total = in.PricePer
	if in.Total != nil {
		l.Total = *in.Total
	}
	l.Latitude, l.Longitude, l.Address, l.Description, l.Amenities = in.Latitude, in.Longitude, in.Address, in.Description, in.Amenities
	l.Walk = s.walkMinutes(r.Context(), user, *in.Latitude, *in.Longitude)
	if !user.IsAdmin {
		l.Status, l.ReviewNote = model.ListingPending, nil
	}
	if err := store.UpdateListingDetails(r.Context(), s.DB.Executor, l); err != nil {
		return err
	}
	return s.respondListing(w, r, l.ID)
}

// deleteListing: the owner takes their listing down (e.g. it's been rented). It's kept
// (bookings and chats may point at it) but never shown publicly again.
func (s *Server) deleteListing(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	l, err := s.listingForOwner(r, user)
	if err != nil {
		return err
	}
	if err := store.SetListingStatus(r.Context(), s.DB.Executor, l.ID, model.ListingRemoved, nil); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- admin review ----

func (s *Server) adminListListings(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	status := newQuery(r).str("status")
	if status == "" {
		status = model.ListingPending
	}
	switch status {
	case model.ListingPending, model.ListingApproved, model.ListingRejected, model.ListingRemoved:
	default:
		return httpErr(http.StatusUnprocessableEntity, "Unknown status")
	}
	out, err := store.ListingsByStatus(r.Context(), s.DB.Executor, status)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminReviewListing(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "listing_id")
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
	ctx, ex := r.Context(), s.DB.Executor
	l, err := store.ListingByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if l == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	note := strings.TrimSpace(p.Note)
	if utf8.RuneCountInString(note) > maxReviewNoteLen {
		return httpErr(http.StatusUnprocessableEntity, "Note is too long (500 characters max)")
	}
	status, notePtr := model.ListingApproved, (*string)(nil)
	if p.Decision == "reject" {
		if note == "" {
			return httpErr(http.StatusUnprocessableEntity, "Tell the poster why it was rejected")
		}
		status, notePtr = model.ListingRejected, &note
	}
	if err := store.SetListingStatus(ctx, ex, id, status, notePtr); err != nil {
		return err
	}
	return s.respondListing(w, r, id)
}

// ---- ownership helpers ----

// ownsListing: admins may manage any listing; anyone else only their own.
func ownsListing(user *model.User, l *model.Listing) error {
	if user.IsAdmin || (l.OwnerID != nil && *l.OwnerID == user.ID) {
		return nil
	}
	return httpErr(http.StatusForbidden, "You can only change your own listings")
}

func (s *Server) listingForOwner(r *http.Request, user *model.User) (*model.Listing, error) {
	id, err := pathInt(r, "listing_id")
	if err != nil {
		return nil, err
	}
	l, err := store.ListingByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, httpErr(http.StatusNotFound, "Listing not found")
	}
	return l, ownsListing(user, l)
}

// canManageListing reports whether the (optional) caller is the owner or an admin —
// they may see a listing that isn't public yet.
func (s *Server) canManageListing(r *http.Request, l *model.Listing) bool {
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		return false
	}
	user, err := s.currentUser(r)
	return err == nil && ownsListing(user, l) == nil
}

// repend sends an owner-changed listing back for review (admins' changes stand).
func (s *Server) repend(ctx context.Context, user *model.User, l *model.Listing) error {
	if user.IsAdmin || l.Status == model.ListingPending || l.Status == model.ListingRemoved {
		return nil
	}
	return store.SetListingStatus(ctx, s.DB.Executor, l.ID, model.ListingPending, nil)
}
