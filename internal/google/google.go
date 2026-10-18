// Package google is a small server-side client for Google Maps Platform:
//
//   - Places API (New): business search/details — name, address, rating, opening
//     hours, phone, website, reviews, photos, "open now"
//   - Routes API: real walking / driving distance and time
//
// The API key lives only on the server (GOOGLE_MAPS_API_KEY); the mobile app never
// sees it. Every call is cached briefly because Google bills per request.
package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotConfigured is returned when no API key is set.
var ErrNotConfigured = errors.New("google maps is not configured")

type Client struct {
	APIKey    string
	PlacesURL string // overridable for tests
	RoutesURL string
	HTTP      *http.Client
	cache     sync.Map // key -> cacheEntry
	CacheTTL  time.Duration
}

func New(apiKey string) *Client {
	return &Client{
		APIKey:    apiKey,
		PlacesURL: "https://places.googleapis.com/v1",
		RoutesURL: "https://routes.googleapis.com/directions/v2:computeRoutes",
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		CacheTTL:  10 * time.Minute,
	}
}

func (c *Client) Configured() bool { return c != nil && c.APIKey != "" }

type cacheEntry struct {
	at  time.Time
	val any
}

func (c *Client) cached(key string, fetch func() (any, error)) (any, error) {
	if e, ok := c.cache.Load(key); ok {
		if ce := e.(cacheEntry); time.Since(ce.at) < c.CacheTTL {
			return ce.val, nil
		}
	}
	v, err := fetch()
	if err == nil {
		c.cache.Store(key, cacheEntry{time.Now(), v})
	}
	return v, err
}

func (c *Client) do(ctx context.Context, method, endpoint, fieldMask string, body any, out any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.APIKey)
	if fieldMask != "" {
		req.Header.Set("X-Goog-FieldMask", fieldMask)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct{ Message, Status string } `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("google %s: %s: %s", resp.Status, e.Error.Status, e.Error.Message)
	}
	return json.Unmarshal(raw, out)
}

// ---- places ----

type LatLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Place is a Google business / point of interest in the shape the app uses.
type Place struct {
	PlaceID         string   `json:"place_id"`
	Name            string   `json:"name"`
	Address         string   `json:"address"`
	Latitude        *float64 `json:"latitude"`
	Longitude       *float64 `json:"longitude"`
	Rating          *float64 `json:"rating"`
	UserRatingCount *int     `json:"user_rating_count"`
	OpenNow         *bool    `json:"open_now"`
	Phone           string   `json:"phone"`
	Website         string   `json:"website"`
	MapsURL         string   `json:"maps_url"` // opens the business in Google Maps
	Types           []string `json:"types"`
	PrimaryType     string   `json:"primary_type"` // Google's best single type, e.g. "bank"
	TypeLabel       string   `json:"type_label"`   // …and its readable name, e.g. "Bank"
	PriceLevel      string   `json:"price_level"`
	BusinessStatus  string   `json:"business_status"` // OPERATIONAL | CLOSED_TEMPORARILY | CLOSED_PERMANENTLY
	Summary         string   `json:"summary"`
	PhotoNames      []string `json:"photo_names"` // resolve with Photo()
	WeeklyHours     []string `json:"weekly_hours"`
	Reviews         []Review `json:"reviews"`
	DistanceKM      *float64 `json:"distance_km"` // from the search origin, when known
}

type Review struct {
	Author       string  `json:"author"`
	Rating       float64 `json:"rating"`
	Text         string  `json:"text"`
	RelativeTime string  `json:"relative_time"`
}

type gPlace struct {
	ID          string `json:"id"`
	DisplayName struct {
		Text string `json:"text"`
	} `json:"displayName"`
	Formatted     string   `json:"formattedAddress"`
	Location      *LatLng  `json:"location"`
	Rating        *float64 `json:"rating"`
	RatingCount   *int     `json:"userRatingCount"`
	National      string   `json:"nationalPhoneNumber"`
	International string   `json:"internationalPhoneNumber"`
	Website       string   `json:"websiteUri"`
	MapsURI       string   `json:"googleMapsUri"`
	Types         []string `json:"types"`
	PrimaryType   string   `json:"primaryType"`
	PrimaryLabel  struct {
		Text string `json:"text"`
	} `json:"primaryTypeDisplayName"`
	PriceLevel string `json:"priceLevel"`
	Status     string `json:"businessStatus"`
	Editorial  struct {
		Text string `json:"text"`
	} `json:"editorialSummary"`
	CurrentHours struct {
		OpenNow *bool `json:"openNow"`
	} `json:"currentOpeningHours"`
	RegularHours struct {
		Descriptions []string `json:"weekdayDescriptions"`
	} `json:"regularOpeningHours"`
	Photos []struct {
		Name string `json:"name"`
	} `json:"photos"`
	Reviews []struct {
		Rating float64 `json:"rating"`
		Text   struct {
			Text string `json:"text"`
		} `json:"text"`
		Relative string `json:"relativePublishTimeDescription"`
		Author   struct {
			DisplayName string `json:"displayName"`
		} `json:"authorAttribution"`
	} `json:"reviews"`
}

func (g gPlace) toPlace() Place {
	p := Place{
		PlaceID: g.ID, Name: g.DisplayName.Text, Address: g.Formatted, Rating: g.Rating, UserRatingCount: g.RatingCount,
		OpenNow: g.CurrentHours.OpenNow, Phone: firstNonEmpty(g.National, g.International), Website: g.Website,
		MapsURL: g.MapsURI, Types: g.Types, PrimaryType: g.PrimaryType, TypeLabel: g.PrimaryLabel.Text, PriceLevel: g.PriceLevel, BusinessStatus: g.Status, Summary: g.Editorial.Text,
		WeeklyHours: g.RegularHours.Descriptions,
	}
	if g.Location != nil {
		lat, lng := g.Location.Latitude, g.Location.Longitude
		p.Latitude, p.Longitude = &lat, &lng
	}
	for _, ph := range g.Photos {
		p.PhotoNames = append(p.PhotoNames, ph.Name)
	}
	for _, r := range g.Reviews {
		p.Reviews = append(p.Reviews, Review{Author: r.Author.DisplayName, Rating: r.Rating, Text: r.Text.Text, RelativeTime: r.Relative})
	}
	if p.Types == nil {
		p.Types = []string{}
	}
	if p.PhotoNames == nil {
		p.PhotoNames = []string{}
	}
	if p.WeeklyHours == nil {
		p.WeeklyHours = []string{}
	}
	if p.Reviews == nil {
		p.Reviews = []Review{}
	}
	return p
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

const summaryMask = "places.id,places.displayName,places.formattedAddress,places.location,places.rating,places.userRatingCount," +
	"places.currentOpeningHours.openNow,places.nationalPhoneNumber,places.websiteUri,places.googleMapsUri,places.types," +
	"places.priceLevel,places.businessStatus,places.photos.name,places.primaryType,places.primaryTypeDisplayName"

// basicMask asks only for identity and location — Google bills by the fields
// requested, and pinning a school needs nothing more.
const basicMask = "places.id,places.displayName,places.formattedAddress,places.location"

// SearchText finds places by free text ("University of Lagos, Akoka, Lagos, Nigeria"),
// biased to Nigeria. Used to pin schools to exact map locations, so it requests
// only the cheap basic fields.
func (c *Client) SearchText(ctx context.Context, query string, max int) ([]Place, error) {
	v, err := c.cached("text|"+query+"|"+strconv.Itoa(max), func() (any, error) {
		var out struct {
			Places []gPlace `json:"places"`
		}
		err := c.do(ctx, http.MethodPost, c.PlacesURL+"/places:searchText", basicMask,
			map[string]any{"textQuery": query, "regionCode": "NG", "maxResultCount": max}, &out)
		places := make([]Place, 0, len(out.Places))
		for _, g := range out.Places {
			places = append(places, g.toPlace())
		}
		return places, err
	})
	if err != nil {
		return nil, err
	}
	return v.([]Place), nil
}

// SearchNear finds businesses by name or kind ("GTBank", "pharmacy open late") with results
// biased towards a point — what the app's search box needs, since a name filter over the
// nearest places can't find anything that isn't already among them.
func (c *Client) SearchNear(ctx context.Context, query string, lat, lng, radiusM float64, max int) ([]Place, error) {
	key := fmt.Sprintf("near|%s|%.4f|%.4f|%.0f|%d", strings.ToLower(query), lat, lng, radiusM, max)
	v, err := c.cached(key, func() (any, error) {
		var out struct {
			Places []gPlace `json:"places"`
		}
		err := c.do(ctx, http.MethodPost, c.PlacesURL+"/places:searchText", summaryMask, map[string]any{
			"textQuery": query, "regionCode": "NG", "maxResultCount": max,
			"locationBias": map[string]any{"circle": map[string]any{
				"center": map[string]float64{"latitude": lat, "longitude": lng}, "radius": radiusM}},
		}, &out)
		places := make([]Place, 0, len(out.Places))
		for _, g := range out.Places {
			places = append(places, g.toPlace())
		}
		return places, err
	})
	if err != nil {
		return nil, err
	}
	return v.([]Place), nil
}

// Nearby lists businesses around a point (types are Google place types such as
// "pharmacy", "hospital", "bank", "restaurant"), nearest first.
func (c *Client) Nearby(ctx context.Context, lat, lng, radiusM float64, types []string, max int) ([]Place, error) {
	key := fmt.Sprintf("nearby|%.4f|%.4f|%.0f|%s|%d", lat, lng, radiusM, strings.Join(types, ","), max)
	v, err := c.cached(key, func() (any, error) {
		body := map[string]any{
			"maxResultCount": max, "rankPreference": "DISTANCE",
			"locationRestriction": map[string]any{"circle": map[string]any{
				"center": map[string]float64{"latitude": lat, "longitude": lng}, "radius": radiusM}},
		}
		if len(types) > 0 {
			body["includedTypes"] = types
		}
		var out struct {
			Places []gPlace `json:"places"`
		}
		err := c.do(ctx, http.MethodPost, c.PlacesURL+"/places:searchNearby", summaryMask, body, &out)
		places := make([]Place, 0, len(out.Places))
		for _, g := range out.Places {
			places = append(places, g.toPlace())
		}
		return places, err
	})
	if err != nil {
		return nil, err
	}
	return v.([]Place), nil
}

const detailMask = "id,displayName,formattedAddress,location,rating,userRatingCount,nationalPhoneNumber,internationalPhoneNumber," +
	"websiteUri,googleMapsUri,regularOpeningHours,currentOpeningHours,businessStatus,types,primaryType,primaryTypeDisplayName,priceLevel,photos,reviews,editorialSummary"

// Details returns everything Google Business has on one place: hours, phone,
// website, rating, reviews, photos.
func (c *Client) Details(ctx context.Context, placeID string) (*Place, error) {
	v, err := c.cached("detail|"+placeID, func() (any, error) {
		var g gPlace
		if err := c.do(ctx, http.MethodGet, c.PlacesURL+"/places/"+url.PathEscape(placeID), detailMask, nil, &g); err != nil {
			return nil, err
		}
		p := g.toPlace()
		return &p, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Place), nil
}

// PhotoURL resolves a photo name from a Place into a direct, key-free image URL.
func (c *Client) PhotoURL(ctx context.Context, name string, maxWidth int) (string, error) {
	if !strings.HasPrefix(name, "places/") || !strings.Contains(name, "/photos/") {
		return "", errors.New("invalid photo name")
	}
	v, err := c.cached("photo|"+name+"|"+strconv.Itoa(maxWidth), func() (any, error) {
		var out struct {
			PhotoURI string `json:"photoUri"`
		}
		err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/%s/media?maxWidthPx=%d&skipHttpRedirect=true", c.PlacesURL, name, maxWidth), "", nil, &out)
		return out.PhotoURI, err
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// ---- routes ----

// Route is real travel distance and time between two points, with the path to draw on
// an in-app map and the turn-by-turn steps.
type Route struct {
	Mode        string  `json:"mode"` // walk | drive | transit
	DistanceM   int     `json:"distance_m"`
	DurationS   int     `json:"duration_s"`
	DistanceKM  float64 `json:"distance_km"`
	DurationMin int     `json:"duration_min"`
	// Polyline is the route in Google's encoded-polyline format; the app decodes and draws it.
	Polyline string `json:"polyline"`
	Steps    []Step `json:"steps"`
}

// Step is one turn-by-turn instruction.
type Step struct {
	Instruction string `json:"instruction"`
	DistanceM   int    `json:"distance_m"`
	DurationS   int    `json:"duration_s"`
}

var travelModes = map[string]string{"walk": "WALK", "drive": "DRIVE", "transit": "TRANSIT"}

const routeMask = "routes.duration,routes.distanceMeters,routes.polyline.encodedPolyline," +
	"routes.legs.steps.navigationInstruction.instructions,routes.legs.steps.distanceMeters,routes.legs.steps.staticDuration"

func parseSeconds(d string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(d, "s"))
	return n
}

func (c *Client) Route(ctx context.Context, from, to LatLng, mode string) (*Route, error) {
	gm, ok := travelModes[mode]
	if !ok {
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
	key := fmt.Sprintf("route|%.5f,%.5f|%.5f,%.5f|%s", from.Latitude, from.Longitude, to.Latitude, to.Longitude, mode)
	v, err := c.cached(key, func() (any, error) {
		pt := func(p LatLng) map[string]any {
			return map[string]any{"location": map[string]any{"latLng": p}}
		}
		var out struct {
			Routes []struct {
				DistanceMeters int    `json:"distanceMeters"`
				Duration       string `json:"duration"`
				Polyline       struct {
					Encoded string `json:"encodedPolyline"`
				} `json:"polyline"`
				Legs []struct {
					Steps []struct {
						Nav struct {
							Instructions string `json:"instructions"`
						} `json:"navigationInstruction"`
						DistanceMeters int    `json:"distanceMeters"`
						StaticDuration string `json:"staticDuration"`
					} `json:"steps"`
				} `json:"legs"`
			} `json:"routes"`
		}
		err := c.do(ctx, http.MethodPost, c.RoutesURL, routeMask,
			map[string]any{"origin": pt(from), "destination": pt(to), "travelMode": gm, "units": "METRIC"}, &out)
		if err != nil {
			return nil, err
		}
		if len(out.Routes) == 0 {
			return nil, errors.New("google: no route found")
		}
		r0 := out.Routes[0]
		secs := parseSeconds(r0.Duration)
		route := &Route{Mode: mode, DistanceM: r0.DistanceMeters, DurationS: secs,
			DistanceKM: float64(r0.DistanceMeters) / 1000, DurationMin: (secs + 30) / 60,
			Polyline: r0.Polyline.Encoded, Steps: []Step{}}
		for _, leg := range r0.Legs {
			for _, st := range leg.Steps {
				if st.Nav.Instructions == "" {
					continue
				}
				route.Steps = append(route.Steps, Step{Instruction: st.Nav.Instructions, DistanceM: st.DistanceMeters, DurationS: parseSeconds(st.StaticDuration)})
			}
		}
		return route, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Route), nil
}
