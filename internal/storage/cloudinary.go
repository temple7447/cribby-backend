// Package storage uploads photos to Cloudinary. Cloudinary does the resizing,
// optimisation and CDN delivery itself — we upload the raw bytes with a
// transformation string and it does the rest, so no local image processing.
package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured is returned when no Cloudinary credentials are set; the API
// maps it to 503 so local dev works with zero setup.
var ErrNotConfigured = errors.New("photo storage is not configured")

// Transformations applied on upload (Cloudinary's chained URL syntax).
const (
	// Listing photos: scaled down to fit 1600×1600.
	TransformListing = "c_limit,h_1600,w_1600/f_auto,q_auto"
	// Avatars: square, face-aware (they render as circles everywhere).
	TransformAvatar = "c_fill,g_face,h_512,w_512/f_auto,q_auto"
	// Roommate banners: wide crop (full-width banner, not a circle).
	TransformBanner = "c_fill,g_auto,h_400,w_1200/f_auto,q_auto"
)

type Uploaded struct{ URL, PublicID string }

// Storage is the photo store; tests substitute a fake.
type Storage interface {
	Configured() bool
	// Upload stores an image. `file` is raw bytes, or (for seeding) a remote URL
	// that Cloudinary fetches itself.
	Upload(ctx context.Context, file []byte, remoteURL, folder, transformation string) (Uploaded, error)
	Destroy(ctx context.Context, publicID string) error
}

type Cloudinary struct {
	CloudName, APIKey, APISecret string
	BaseURL                      string // overridable for tests
	Client                       *http.Client
}

func NewCloudinary(cloudName, apiKey, apiSecret string) *Cloudinary {
	return &Cloudinary{
		CloudName: cloudName, APIKey: apiKey, APISecret: apiSecret,
		BaseURL: "https://api.cloudinary.com",
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Cloudinary) Configured() bool { return c.CloudName != "" }

// sign implements Cloudinary's request signature: sha1 of the sorted
// "k=v&k=v" params followed by the API secret.
func (c *Cloudinary) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + params[k]
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "&") + c.APISecret))
	return hex.EncodeToString(sum[:])
}

// call performs one signed Cloudinary API request. `params` are the signed
// fields; `file` (bytes) or `fileURL` (a remote URL Cloudinary fetches itself)
// is attached unsigned, as Cloudinary requires.
func (c *Cloudinary) call(ctx context.Context, action string, params map[string]string, file []byte, fileURL string) (map[string]any, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	params["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range params {
		_ = mw.WriteField(k, v)
	}
	_ = mw.WriteField("api_key", c.APIKey)
	_ = mw.WriteField("signature", c.sign(params))
	switch {
	case file != nil:
		fw, _ := mw.CreateFormFile("file", "upload")
		_, _ = fw.Write(file)
	case fileURL != "":
		_ = mw.WriteField("file", fileURL)
	}
	_ = mw.Close()

	url := fmt.Sprintf("%s/v1_1/%s/image/%s", c.BaseURL, c.CloudName, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if e, ok := out["error"].(map[string]any); ok {
			if m, ok := e["message"].(string); ok {
				msg = m
			}
		}
		return nil, fmt.Errorf("cloudinary %s: %s: %s", action, resp.Status, msg)
	}
	return out, nil
}

func (c *Cloudinary) Upload(ctx context.Context, file []byte, remoteURL, folder, transformation string) (Uploaded, error) {
	out, err := c.call(ctx, "upload", map[string]string{"folder": folder, "transformation": transformation}, file, remoteURL)
	if err != nil {
		return Uploaded{}, err
	}
	url, _ := out["secure_url"].(string)
	publicID, _ := out["public_id"].(string)
	if url == "" || publicID == "" {
		return Uploaded{}, errors.New("cloudinary upload: response missing secure_url/public_id")
	}
	return Uploaded{URL: url, PublicID: publicID}, nil
}

func (c *Cloudinary) Destroy(ctx context.Context, publicID string) error {
	_, err := c.call(ctx, "destroy", map[string]string{"public_id": publicID}, nil, "")
	return err
}
