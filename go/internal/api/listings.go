package api

import (
	"net/http"
	"strconv"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

func itoa(n int) string { return strconv.Itoa(n) }

// listListings: pass near_lat + near_lng together to sort nearest-first and get
// a distance_km on each; add radius_km to also drop anything further away.
func (s *Server) listListings(w http.ResponseWriter, r *http.Request) error {
	q := newQuery(r)
	f := store.ListingSearch{
		MaxPrice: q.optInt("max_price"),
		MaxWalk:  q.optInt("max_walk"),
		RoomType: q.str("room_type"),
		Q:        q.str("q"),
		NearLat:  q.optFloat("near_lat"),
		NearLng:  q.optFloat("near_lng"),
		RadiusKM: q.optFloat("radius_km"),
	}
	if err := q.err(); err != nil {
		return err
	}
	listings, err := store.SearchListings(r.Context(), s.DB.Executor, f)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, listings)
	return nil
}

func (s *Server) getListing(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt(r, "listing_id")
	if err != nil {
		return err
	}
	l, err := store.ListingByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return err
	}
	if l == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}

// Creating and photo management are admin-only for now — listings aren't owned
// by a User account yet (landlord_name is free text), so there's no "is this my
// listing" check to make. Revisit once real landlord accounts exist.

func (s *Server) createListing(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	p := struct {
		Title                string   `json:"title" validate:"required"`
		Type                 string   `json:"type" validate:"required"`
		PricePer             int      `json:"price_per" validate:"required"`
		Total                int      `json:"total" validate:"required"`
		UtilitiesMonthly     int      `json:"utilities_monthly"`
		Deposit              int      `json:"deposit" validate:"required"`
		Walk                 int      `json:"walk" validate:"required"`
		Latitude             *float64 `json:"latitude"`
		Longitude            *float64 `json:"longitude"`
		Rating               float64  `json:"rating"`
		Reviews              int      `json:"reviews"`
		Verified             bool     `json:"verified"`
		Hot                  bool     `json:"hot"`
		Video                bool     `json:"video"`
		Grad                 string   `json:"grad"`
		Emoji                string   `json:"emoji"`
		Amenities            []string `json:"amenities"`
		LandlordName         string   `json:"landlord_name"`
		LandlordVerified     bool     `json:"landlord_verified"`
		LandlordResponseRate int      `json:"landlord_response_rate"`
	}{
		UtilitiesMonthly: 45, Grad: "from-coral to-sunshine", Emoji: "🏠",
		LandlordName: "Sandra", LandlordVerified: true, LandlordResponseRate: 95,
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	created, err := store.CreateListing(r.Context(), s.DB.Executor, &model.Listing{
		Title: p.Title, Type: p.Type, PricePer: p.PricePer, Total: p.Total, UtilitiesMonthly: p.UtilitiesMonthly,
		Deposit: p.Deposit, Walk: p.Walk, Latitude: p.Latitude, Longitude: p.Longitude, Rating: p.Rating,
		Reviews: p.Reviews, Verified: p.Verified, Hot: p.Hot, Video: p.Video, Grad: p.Grad, Emoji: p.Emoji,
		Amenities: p.Amenities, LandlordName: p.LandlordName, LandlordVerified: p.LandlordVerified,
		LandlordResponseRate: p.LandlordResponseRate,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *Server) uploadListingPhoto(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "listing_id")
	if err != nil {
		return err
	}
	up, err := parseUpload(w, r)
	if err != nil {
		return err
	}
	defer up.close()

	ctx, ex := r.Context(), s.DB.Executor
	listing, err := store.ListingByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if listing == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	uploaded, err := s.uploadImage(ctx, up, "cribby/listings/"+itoa(id), storage.TransformListing)
	if err != nil {
		return err
	}
	photos := append(db.JSONList[model.Photo]{}, listing.Photos...)
	photos = append(photos, model.Photo{
		ID: security.NewUUID(), URL: uploaded.URL, PublicID: uploaded.PublicID,
		Alt: r.FormValue("alt"), UploadedAt: db.Time{Time: time.Now().UTC()},
	})
	if err := store.SetListingPhotos(ctx, ex, id, photos); err != nil {
		return err
	}
	return s.respondListing(w, r, id)
}

func (s *Server) deleteListingPhoto(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "listing_id")
	if err != nil {
		return err
	}
	photoID := r.PathValue("photo_id")
	ctx, ex := r.Context(), s.DB.Executor
	listing, err := store.ListingByID(ctx, ex, id)
	if err != nil {
		return err
	}
	if listing == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	var match *model.Photo
	kept := db.JSONList[model.Photo]{}
	for i := range listing.Photos {
		if listing.Photos[i].ID == photoID {
			match = &listing.Photos[i]
		} else {
			kept = append(kept, listing.Photos[i])
		}
	}
	if match == nil {
		return httpErr(http.StatusNotFound, "Photo not found")
	}
	if s.photos.Configured() {
		if err := s.photos.Destroy(ctx, match.PublicID); err != nil {
			s.log.Error("deleting listing photo", "err", err)
			return httpErr(http.StatusBadGateway, "Couldn't delete photo from storage — try again")
		}
	}
	if err := store.SetListingPhotos(ctx, ex, id, kept); err != nil {
		return err
	}
	return s.respondListing(w, r, id)
}

func (s *Server) respondListing(w http.ResponseWriter, r *http.Request, id int) error {
	l, err := store.ListingByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}
