package api

import (
	"net/http"
	"strings"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

// The student marketplace: used mattresses, fans, fridges and the like. Posting is open and items go live
// at once; the report button hides an item until an admin looks. Sellers' phone numbers are only shown to
// signed-in people. Items from people who are leaving carry a flag, since that often means a room is opening.

const (
	maxMarketPerDay   = 5
	maxMarketPhotos   = 6
	maxMarketPrice    = 5_000_000
	marketPhotoFolder = "cribby/market/"
)

var (
	marketCategories = map[string]bool{"mattress": true, "bed": true, "fan": true, "fridge": true, "cooker": true, "kitchen": true,
		"furniture": true, "electronics": true, "books": true, "clothing": true, "other": true}
	marketConditions = map[string]bool{"new": true, "good": true, "fair": true}
)

type marketInput struct {
	Title        string   `json:"title" validate:"required"`
	Description  string   `json:"description"`
	Category     string   `json:"category" validate:"required"`
	Condition    string   `json:"condition" validate:"required"`
	Price        int      `json:"price"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	Area         string   `json:"area"`
	ContactPhone string   `json:"contact_phone" validate:"required"`
	LeavingSoon  bool     `json:"leaving_soon"`
	Status       string   `json:"status"` // edit only: active | sold
}

func (in *marketInput) clean() error {
	in.Title, in.Description, in.Area = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description), strings.TrimSpace(in.Area)
	if n := len([]rune(in.Title)); n < 3 || n > 120 {
		return httpErr(http.StatusUnprocessableEntity, "Give it a title (3 to 120 characters)")
	}
	if len([]rune(in.Description)) > 1500 || len([]rune(in.Area)) > 120 {
		return httpErr(http.StatusUnprocessableEntity, "The description or area is too long")
	}
	if !marketCategories[in.Category] {
		return httpErr(http.StatusUnprocessableEntity, "Choose a category")
	}
	if !marketConditions[in.Condition] {
		return httpErr(http.StatusUnprocessableEntity, "Choose a condition")
	}
	if in.Price < 0 || in.Price > maxMarketPrice {
		return httpErr(http.StatusUnprocessableEntity, "Enter a price in naira (0 for free)")
	}
	if in.ContactPhone = normalizePhone(in.ContactPhone); in.ContactPhone == "" {
		return httpErr(http.StatusUnprocessableEntity, "Enter a phone number buyers can reach you on")
	}
	if (in.Latitude == nil) != (in.Longitude == nil) {
		return httpErr(http.StatusUnprocessableEntity, "Send both latitude and longitude, or neither")
	}
	if in.Latitude != nil && (*in.Latitude < -90 || *in.Latitude > 90 || *in.Longitude < -180 || *in.Longitude > 180) {
		return httpErr(http.StatusUnprocessableEntity, "That location isn't valid")
	}
	if in.Status != "" && in.Status != "active" && in.Status != "sold" {
		return httpErr(http.StatusUnprocessableEntity, "Status must be active or sold")
	}
	return nil
}

func (s *Server) marketItem(r *http.Request) (*model.MarketItem, error) {
	id, err := pathInt(r, "item_id")
	if err != nil {
		return nil, err
	}
	m, err := store.MarketItemByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if m == nil || m.Status == "removed" {
		return nil, httpErr(http.StatusNotFound, "Item not found")
	}
	return m, nil
}

// signedInUser returns the person if a valid token came with the request, else nil.
func (s *Server) signedInUser(r *http.Request) *model.User {
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		return nil
	}
	u, err := s.currentUser(r)
	if err != nil {
		return nil
	}
	return u
}

// present shapes an item for the person asking: phone only for signed-in people, never in lists.
func present(m *model.MarketItem, viewer *model.User, withPhone bool) {
	m.Mine = viewer != nil && m.UserID == viewer.ID
	if !withPhone || viewer == nil {
		m.ContactPhone = ""
	}
}

func (s *Server) listMarket(w http.ResponseWriter, r *http.Request) error {
	q := newQuery(r)
	f := store.MarketSearch{Category: q.str("category"), Q: strings.TrimSpace(q.str("q")), MaxPrice: q.optInt("max_price"),
		FreeOnly: q.str("free") == "1", NearLat: q.optFloat("near_lat"), NearLng: q.optFloat("near_lng"), RadiusKM: q.optFloat("radius_km")}
	limit := 60
	if n := q.optInt("limit"); n != nil && *n > 0 && *n <= 200 {
		limit = *n
	}
	schoolID := q.optInt("school_id")
	if err := q.err(); err != nil {
		return err
	}
	// ?school_id=N ranks from that school, as it does for homes (an explicit near_lat/near_lng wins).
	if f.NearLat == nil && f.NearLng == nil && schoolID != nil {
		lat, lng, err := s.schoolOrigin(r.Context(), schoolID)
		if err != nil {
			return err
		}
		f.NearLat, f.NearLng = lat, lng
	}
	if f.Category != "" && !marketCategories[f.Category] {
		return httpErr(http.StatusUnprocessableEntity, "Unknown category")
	}
	items, err := store.SearchMarket(r.Context(), s.DB.Executor, f, limit)
	if err != nil {
		return err
	}
	viewer := s.signedInUser(r)
	out := make([]*model.MarketItem, len(items))
	for i, m := range items {
		present(m, viewer, false)
		out[i] = m
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getMarketItem(w http.ResponseWriter, r *http.Request) error {
	m, err := s.marketItem(r)
	if err != nil {
		return err
	}
	viewer := s.signedInUser(r)
	if m.Status == "reported" && !(viewer != nil && (m.UserID == viewer.ID || viewer.IsAdmin)) {
		return httpErr(http.StatusNotFound, "Item not found")
	}
	present(m, viewer, true)
	writeJSON(w, http.StatusOK, m)
	return nil
}

func (s *Server) myMarketItems(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	items, err := store.ListMarketByUser(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	for i := range items {
		present(&items[i], user, true)
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}

func (s *Server) createMarketItem(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	var in marketInput
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	if err := in.clean(); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	n, err := store.CountRecentMarketByUser(ctx, ex, user.ID, db.Time{Time: time.Now().UTC().Add(-24 * time.Hour)})
	if err != nil {
		return err
	}
	if n >= maxMarketPerDay {
		return httpErr(http.StatusTooManyRequests, "You've posted a lot today. Please try again tomorrow")
	}
	id, err := store.CreateMarketItem(ctx, ex, model.MarketItem{UserID: user.ID, Title: in.Title, Description: in.Description, Category: in.Category,
		Condition: in.Condition, Price: in.Price, Latitude: in.Latitude, Longitude: in.Longitude, Area: in.Area,
		ContactPhone: in.ContactPhone, LeavingSoon: in.LeavingSoon})
	if err != nil {
		return err
	}
	m, err := store.MarketItemByID(ctx, ex, id)
	if err != nil {
		return err
	}
	present(m, user, true)
	writeJSON(w, http.StatusCreated, m)
	return nil
}

// ownsItem: the seller, or an admin.
func ownsItem(user *model.User, m *model.MarketItem) error {
	if user.IsAdmin || m.UserID == user.ID {
		return nil
	}
	return httpErr(http.StatusForbidden, "You can only change your own items")
}

func (s *Server) updateMarketItem(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	m, err := s.marketItem(r)
	if err != nil {
		return err
	}
	if err := ownsItem(user, m); err != nil {
		return err
	}
	var in marketInput
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	if err := in.clean(); err != nil {
		return err
	}
	m.Title, m.Description, m.Category, m.Condition, m.Price = in.Title, in.Description, in.Category, in.Condition, in.Price
	m.Latitude, m.Longitude, m.Area, m.ContactPhone, m.LeavingSoon = in.Latitude, in.Longitude, in.Area, in.ContactPhone, in.LeavingSoon
	// Sellers can mark sold or put it back; a reported item stays hidden until an admin decides.
	if in.Status != "" && m.Status != "reported" {
		m.Status = in.Status
	}
	if err := store.UpdateMarketItem(r.Context(), s.DB.Executor, m); err != nil {
		return err
	}
	m, err = store.MarketItemByID(r.Context(), s.DB.Executor, m.ID)
	if err != nil {
		return err
	}
	present(m, user, true)
	writeJSON(w, http.StatusOK, m)
	return nil
}

func (s *Server) deleteMarketItem(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	m, err := s.marketItem(r)
	if err != nil {
		return err
	}
	if err := ownsItem(user, m); err != nil {
		return err
	}
	if err := store.SetMarketStatus(r.Context(), s.DB.Executor, m.ID, "removed"); err != nil {
		return err
	}
	noContent(w)
	return nil
}

func (s *Server) uploadMarketPhoto(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	up, err := parseUpload(w, r)
	if err != nil {
		return err
	}
	defer up.close()
	m, err := s.marketItem(r)
	if err != nil {
		return err
	}
	if err := ownsItem(user, m); err != nil {
		return err
	}
	if len(m.Photos) >= maxMarketPhotos {
		return httpErr(http.StatusUnprocessableEntity, "An item can have at most "+itoa(maxMarketPhotos)+" photos")
	}
	uploaded, err := s.uploadImage(r.Context(), up, marketPhotoFolder+itoa(m.ID), storage.TransformListing)
	if err != nil {
		return err
	}
	photos := append(db.JSONList[model.Photo]{}, m.Photos...)
	photos = append(photos, model.Photo{ID: security.NewUUID(), URL: uploaded.URL, PublicID: uploaded.PublicID,
		Alt: r.FormValue("alt"), UploadedAt: db.Time{Time: time.Now().UTC()}})
	if err := store.SetMarketPhotos(r.Context(), s.DB.Executor, m.ID, photos); err != nil {
		return err
	}
	m.Photos = photos
	present(m, user, true)
	writeJSON(w, http.StatusOK, m)
	return nil
}

func (s *Server) deleteMarketPhoto(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	m, err := s.marketItem(r)
	if err != nil {
		return err
	}
	if err := ownsItem(user, m); err != nil {
		return err
	}
	photoID := r.PathValue("photo_id")
	var match *model.Photo
	kept := db.JSONList[model.Photo]{}
	for i := range m.Photos {
		if m.Photos[i].ID == photoID {
			match = &m.Photos[i]
		} else {
			kept = append(kept, m.Photos[i])
		}
	}
	if match == nil {
		return httpErr(http.StatusNotFound, "Photo not found")
	}
	if s.photos.Configured() {
		if err := s.photos.Destroy(r.Context(), match.PublicID); err != nil {
			s.log.Error("deleting market photo", "err", err)
			return httpErr(http.StatusBadGateway, "Couldn't delete photo from storage. Try again")
		}
	}
	if err := store.SetMarketPhotos(r.Context(), s.DB.Executor, m.ID, kept); err != nil {
		return err
	}
	m.Photos = kept
	present(m, user, true)
	writeJSON(w, http.StatusOK, m)
	return nil
}
