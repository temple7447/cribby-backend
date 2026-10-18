package api

import (
	"net/http"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// listPlaces searches points of interest (hospitals, pharmacies, banks,
// markets, …). Pass q and/or category to search by name/type. Pass near_lat +
// near_lng to sort nearest-first with a distance_km on each; add radius_km to
// also drop anything further away.
func (s *Server) listPlaces(w http.ResponseWriter, r *http.Request) error {
	q := newQuery(r)
	f := store.PlaceSearch{
		Category: q.str("category"),
		Q:        q.str("q"),
		NearLat:  q.optFloat("near_lat"),
		NearLng:  q.optFloat("near_lng"),
		RadiusKM: q.optFloat("radius_km"),
	}
	if err := q.err(); err != nil {
		return err
	}
	places, err := store.SearchPlaces(r.Context(), s.DB.Executor, f)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, places)
	return nil
}

func (s *Server) getPlace(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt(r, "place_id")
	if err != nil {
		return err
	}
	p, err := store.PlaceByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return err
	}
	if p == nil {
		return httpErr(http.StatusNotFound, "Place not found")
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

// Curating the places table is admin-only — same posture as listings.
func (s *Server) createPlace(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	p := struct {
		Name      string   `json:"name" validate:"required"`
		Category  string   `json:"category" validate:"required"`
		Address   *string  `json:"address"`
		Latitude  *float64 `json:"latitude" validate:"required"`
		Longitude *float64 `json:"longitude" validate:"required"`
		Emoji     string   `json:"emoji"`
	}{Emoji: "📍"}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	created, err := store.CreatePlace(r.Context(), s.DB.Executor, &model.Place{
		Name: p.Name, Category: strings.ToLower(p.Category), Address: p.Address,
		Latitude: *p.Latitude, Longitude: *p.Longitude, Emoji: p.Emoji,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *Server) deletePlace(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "place_id")
	if err != nil {
		return err
	}
	if err := store.DeletePlace(r.Context(), s.DB.Executor, id); err != nil {
		return err
	}
	noContent(w)
	return nil
}
