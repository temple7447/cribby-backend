package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"campus-backend/internal/geo"
	"campus-backend/internal/google"
	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// mapsStatus tells the app which Google features are live so it can hide the rest.
func (s *Server) mapsStatus(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentUser(r); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  s.maps.Configured(),
		"features": []string{"route", "nearby_businesses", "place_details", "photos"},
	})
	return nil
}

// point resolves "where" from the query: school_id, listing_id, or lat+lng.
type point struct {
	lat, lng float64
	label    string
}

func (s *Server) schoolPoint(r *http.Request, id int) (*point, error) {
	sc, err := store.SchoolByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, httpErr(http.StatusNotFound, "School not found")
	}
	sc = s.refineSchool(r.Context(), sc) // exact Google location when we only had a rough one
	if sc.Latitude == nil || sc.Longitude == nil {
		return nil, httpErr(http.StatusUnprocessableEntity, "This school has no map location yet")
	}
	return &point{*sc.Latitude, *sc.Longitude, sc.Name}, nil
}

func (s *Server) listingPoint(r *http.Request, id int) (*point, error) {
	l, err := store.ListingByID(r.Context(), s.DB.Executor, id)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, httpErr(http.StatusNotFound, "Listing not found")
	}
	if l.Latitude == nil || l.Longitude == nil {
		return nil, httpErr(http.StatusUnprocessableEntity, "This listing has no map location yet")
	}
	return &point{*l.Latitude, *l.Longitude, l.Title}, nil
}

// resolvePoint finds "where" from the query using <prefix>school_id,
// <prefix>listing_id or <prefix>lat + <prefix>lng; nil when none is given.
func (s *Server) resolvePoint(r *http.Request, q *queryParams, prefix string) (*point, error) {
	if id := q.optInt(prefix + "school_id"); id != nil {
		return s.schoolPoint(r, *id)
	}
	if id := q.optInt(prefix + "listing_id"); id != nil {
		return s.listingPoint(r, *id)
	}
	lat, lng := q.optFloat(prefix+"lat"), q.optFloat(prefix+"lng")
	if lat != nil && lng != nil {
		return &point{*lat, *lng, ""}, nil
	}
	return nil, nil
}

func directionsURL(from, to point, mode string) string {
	tm := map[string]string{"walk": "walking", "drive": "driving", "transit": "transit"}[mode]
	return fmt.Sprintf("https://www.google.com/maps/dir/?api=1&origin=%.6f,%.6f&destination=%.6f,%.6f&travelmode=%s",
		from.lat, from.lng, to.lat, to.lng, tm)
}

// mapsRoute: real travel distance/time from a listing (or any point) to a school.
//
//	/maps/route?listing_id=12&school_id=5&mode=walk
//
// Falls back to a clearly-labelled estimate when Google is unavailable, so the UI
// always has something to show; `source` says which it is.
func (s *Server) mapsRoute(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentUser(r); err != nil {
		return err
	}
	q := newQuery(r)
	mode := q.str("mode")
	if mode == "" {
		mode = "walk"
	}
	from, err := s.resolvePoint(r, q, "from_")
	if err != nil {
		return err
	}
	to, err := s.resolvePoint(r, q, "to_")
	if err != nil {
		return err
	}
	// Convenience: ?listing_id=&school_id= means listing → school.
	if from == nil && to == nil {
		if id := q.optInt("listing_id"); id != nil {
			if from, err = s.listingPoint(r, *id); err != nil {
				return err
			}
		}
		if id := q.optInt("school_id"); id != nil {
			if to, err = s.schoolPoint(r, *id); err != nil {
				return err
			}
		}
	}
	if err := q.err(); err != nil {
		return err
	}
	if from == nil || to == nil {
		return httpErr(http.StatusBadRequest, "Provide listing_id and school_id (or from_/to_ lat+lng)")
	}
	if _, ok := map[string]bool{"walk": true, "drive": true, "transit": true}[mode]; !ok {
		return httpErr(http.StatusBadRequest, "mode must be walk, drive or transit")
	}

	straight := geo.Round2(geo.HaversineKM(from.lat, from.lng, to.lat, to.lng))
	out := map[string]any{
		"mode": mode, "straight_line_km": straight, "directions_url": directionsURL(*from, *to, mode),
		"from": from.label, "to": to.label,
	}
	if s.maps.Configured() {
		rt, err := s.maps.Route(r.Context(), google.LatLng{Latitude: from.lat, Longitude: from.lng},
			google.LatLng{Latitude: to.lat, Longitude: to.lng}, mode)
		if err == nil {
			out["source"], out["distance_km"], out["distance_m"] = "google", geo.Round2(rt.DistanceKM), rt.DistanceM
			out["duration_min"], out["duration_s"] = rt.DurationMin, rt.DurationS
			writeJSON(w, http.StatusOK, out)
			return nil
		}
		s.log.Warn("google route failed, using estimate", "err", err)
	}
	// Estimate: roads are ~30% longer than the straight line; walking ~5 km/h,
	// driving ~25 km/h in Nigerian city traffic.
	road := geo.Round2(straight * 1.3)
	speed := map[string]float64{"walk": 5, "drive": 25, "transit": 18}[mode]
	mins := int(road/speed*60 + 0.5)
	out["source"], out["distance_km"], out["distance_m"] = "estimate", road, int(road*1000)
	out["duration_min"], out["duration_s"] = mins, mins*60
	writeJSON(w, http.StatusOK, out)
	return nil
}

type placeResult struct {
	Source string         `json:"source"` // google | curated
	Places []google.Place `json:"places"`
}

// mapsNearby lists businesses around a school, listing or point — with Google
// Business data (rating, hours, phone, website, photos) when Google is enabled,
// otherwise our curated places table in the same shape.
//
//	/maps/places/nearby?school_id=5&type=pharmacy,hospital&radius_m=2000&limit=15
func (s *Server) mapsNearby(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentUser(r); err != nil {
		return err
	}
	q := newQuery(r)
	origin, err := s.resolvePoint(r, q, "")
	if err != nil {
		return err
	}
	radius := 1500.0
	if v := q.optFloat("radius_m"); v != nil {
		radius = *v
	}
	limit := 15
	if v := q.optInt("limit"); v != nil {
		limit = *v
	}
	types := splitList(q.str("type"))
	if err := q.err(); err != nil {
		return err
	}
	if origin == nil {
		return httpErr(http.StatusBadRequest, "Provide school_id, listing_id, or lat and lng")
	}
	if radius < 100 || radius > 50000 {
		radius = 1500
	}
	if limit < 1 || limit > 20 {
		limit = 15
	}

	if s.maps.Configured() {
		places, err := s.maps.Nearby(r.Context(), origin.lat, origin.lng, radius, types, limit)
		if err == nil {
			places = append([]google.Place(nil), places...) // don't mutate the cached slice
			for i := range places {
				if places[i].Latitude != nil && places[i].Longitude != nil {
					d := geo.Round2(geo.HaversineKM(origin.lat, origin.lng, *places[i].Latitude, *places[i].Longitude))
					places[i].DistanceKM = &d
				}
			}
			writeJSON(w, http.StatusOK, placeResult{Source: "google", Places: places})
			return nil
		}
		s.log.Warn("google nearby failed, using curated places", "err", err)
	}
	return s.curatedNearby(w, r, origin, radius/1000, types, limit)
}

func (s *Server) curatedNearby(w http.ResponseWriter, r *http.Request, o *point, radiusKM float64, types []string, limit int) error {
	var merged []*model.Place
	cats := types
	if len(cats) == 0 {
		cats = []string{""}
	}
	seen := map[int]bool{}
	for _, c := range cats {
		found, err := store.SearchPlaces(r.Context(), s.DB.Executor, store.PlaceSearch{Category: c, NearLat: &o.lat, NearLng: &o.lng, RadiusKM: &radiusKM})
		if err != nil {
			return err
		}
		for _, p := range found {
			if !seen[p.ID] {
				seen[p.ID] = true
				merged = append(merged, p)
			}
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return *merged[i].DistanceKM < *merged[j].DistanceKM })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	out := make([]google.Place, 0, len(merged))
	for _, p := range merged {
		lat, lng := p.Latitude, p.Longitude
		gp := google.Place{
			PlaceID: "", Name: p.Name, Latitude: &lat, Longitude: &lng, Types: []string{p.Category}, DistanceKM: p.DistanceKM,
			PhotoNames: []string{}, WeeklyHours: []string{}, Reviews: []google.Review{},
			MapsURL: fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%.6f,%.6f", lat, lng),
		}
		if p.Address != nil {
			gp.Address = *p.Address
		}
		out = append(out, gp)
	}
	writeJSON(w, http.StatusOK, placeResult{Source: "curated", Places: out})
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var errMapsOff = httpErr(http.StatusServiceUnavailable, "Google Maps is not configured on this environment")

// mapsPlaceDetails is the full Google Business profile: hours, phone, website,
// rating, reviews, photos, "open now".
func (s *Server) mapsPlaceDetails(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentUser(r); err != nil {
		return err
	}
	if !s.maps.Configured() {
		return errMapsOff
	}
	p, err := s.maps.Details(r.Context(), r.PathValue("place_id"))
	if err != nil {
		s.log.Warn("google place details failed", "err", err)
		return httpErr(http.StatusBadGateway, "Couldn't load place details — try again")
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

// mapsPhoto turns a photo name from a place into a plain image URL, so the app
// never needs (or sees) the API key.
func (s *Server) mapsPhoto(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentUser(r); err != nil {
		return err
	}
	if !s.maps.Configured() {
		return errMapsOff
	}
	q := newQuery(r)
	width := 800
	if v := q.optInt("max_width"); v != nil && *v >= 50 && *v <= 1600 {
		width = *v
	}
	if err := q.err(); err != nil {
		return err
	}
	u, err := s.maps.PhotoURL(r.Context(), q.str("name"), width)
	if err != nil {
		return httpErr(http.StatusBadGateway, "Couldn't load photo")
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": u})
	return nil
}

// refineSchool pins a school to its exact Google location the first time it's
// needed (low-confidence coordinates, or none). Best-effort: any failure leaves
// the existing coordinates, and a short timeout keeps it off the critical path.
func (s *Server) refineSchool(ctx context.Context, sc *model.School) *model.School {
	if !s.maps.Configured() || sc == nil {
		return sc
	}
	needs := sc.Source != "google" && (sc.Latitude == nil || sc.Confidence == "low" || sc.Confidence == "very_low" || sc.Confidence == "")
	if !needs {
		return sc
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	query := strings.Join(nonEmpty(sc.Name, sc.Town, sc.State, "Nigeria"), ", ")
	places, err := s.maps.SearchText(ctx, query, 1)
	if err != nil || len(places) == 0 || places[0].Latitude == nil {
		if err != nil {
			s.log.Warn("refining school failed", "school", sc.Name, "err", err)
		}
		return sc
	}
	lat, lng := *places[0].Latitude, *places[0].Longitude
	if lat < 4 || lat > 14.8 || lng < 2.6 || lng > 14.8 { // must be inside Nigeria
		return sc
	}
	if sc.Latitude != nil && geo.HaversineKM(*sc.Latitude, *sc.Longitude, lat, lng) > 300 {
		return sc // wildly different from what we had: likely the wrong match
	}
	if err := store.RefineSchool(ctx, s.DB.Executor, sc.ID, lat, lng, places[0].PlaceID); err != nil {
		s.log.Warn("saving refined school failed", "err", err)
		return sc
	}
	refined, err := store.SchoolByID(ctx, s.DB.Executor, sc.ID)
	if err != nil || refined == nil {
		return sc
	}
	return refined
}

func nonEmpty(v ...string) []string {
	var out []string
	for _, s := range v {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

var _ = errors.New
var _ = url.PathEscape
var _ = strconv.Itoa
