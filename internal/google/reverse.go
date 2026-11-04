package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Address is a human-readable address for a point on the map.
type Address struct {
	Text   string `json:"address"`
	Source string `json:"source"` // "google" | "openstreetmap"
}

// ReverseGeocode turns a map pin into an address. It asks Google's Geocoding API when a key is
// set, and falls back to OpenStreetMap's Nominatim if Google is off, errors (for example the
// Geocoding API isn't enabled on the project) or knows nothing there. It returns nil, nil when
// neither can name the spot. Results are cached, since both providers rate-limit.
func (c *Client) ReverseGeocode(ctx context.Context, lat, lng float64) (*Address, error) {
	key := fmt.Sprintf("rev:%.5f,%.5f", lat, lng)
	v, err := c.cached(key, func() (any, error) {
		if c.Configured() {
			if text, err := c.googleReverse(ctx, lat, lng); err == nil && text != "" {
				return &Address{Text: text, Source: "google"}, nil
			}
		}
		text, err := c.nominatimReverse(ctx, lat, lng)
		if err != nil || text == "" {
			return (*Address)(nil), err
		}
		return &Address{Text: text, Source: "openstreetmap"}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Address), nil
}

// scrub drops the request URL from transport errors: Google's geocoder takes the API key as a
// query parameter, and net/http includes the URL in its errors, which we log.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func (c *Client) getJSON(ctx context.Context, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return scrub(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return scrub(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("geocoder returned %s", resp.Status)
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) googleReverse(ctx context.Context, lat, lng float64) (string, error) {
	q := url.Values{}
	q.Set("latlng", fmt.Sprintf("%f,%f", lat, lng))
	// street-level results only: the default first result is often a Plus Code like "XJ4M+9F".
	q.Set("result_type", "street_address|premise|route|neighborhood|sublocality|locality")
	q.Set("key", c.APIKey)
	var out struct {
		Status  string `json:"status"`
		Results []struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, c.GeocodeURL+"?"+q.Encode(), nil, &out); err != nil {
		return "", err
	}
	switch out.Status {
	case "OK":
		if len(out.Results) > 0 {
			return strings.TrimSpace(out.Results[0].FormattedAddress), nil
		}
		return "", nil
	case "ZERO_RESULTS":
		return "", nil
	default:
		return "", fmt.Errorf("google geocode: %s", out.Status)
	}
}

func (c *Client) nominatimReverse(ctx context.Context, lat, lng float64) (string, error) {
	q := url.Values{}
	q.Set("format", "jsonv2")
	q.Set("lat", fmt.Sprintf("%f", lat))
	q.Set("lon", fmt.Sprintf("%f", lng))
	q.Set("zoom", "18")
	q.Set("addressdetails", "1")
	q.Set("accept-language", "en")
	var out struct {
		DisplayName string            `json:"display_name"`
		Address     map[string]string `json:"address"`
		Error       string            `json:"error"`
	}
	// Nominatim's usage policy requires an identifying User-Agent.
	if err := c.getJSON(ctx, c.NominatimURL+"?"+q.Encode(), map[string]string{"User-Agent": "Cribby/1.0 (https://www.cribby.space)"}, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", nil
	}
	a := out.Address
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(a[k]); v != "" {
				return v
			}
		}
		return ""
	}
	street := strings.TrimSpace(first("house_number") + " " + first("road"))
	var parts []string
	seen := map[string]bool{}
	for _, p := range []string{
		strings.TrimSpace(street),
		first("neighbourhood", "suburb", "quarter"),
		first("city_district", "town", "city", "village", "municipality"),
		first("state"),
	} {
		if p != "" && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(out.DisplayName), nil
	}
	return strings.Join(parts, ", "), nil
}
