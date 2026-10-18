package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	neturl "net/url"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"campus-backend/internal/api"
	"campus-backend/internal/config"
	"campus-backend/internal/db"
	"campus-backend/internal/google"
	"campus-backend/internal/security"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

type sentCode struct{ To, Code string }

// fakeMail records what would have been emailed.
type fakeMail struct {
	mu               sync.Mutex
	verifications    []sentCode
	resets           []sentCode
	bookingRequests  []string    // admin emails
	decisions        [][2]string // {student email, status}
	failVerification bool
}

func (f *fakeMail) SendVerification(_ context.Context, to, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failVerification {
		return fmt.Errorf("boom")
	}
	f.verifications = append(f.verifications, sentCode{to, code})
	return nil
}

func (f *fakeMail) SendPasswordReset(_ context.Context, to, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets = append(f.resets, sentCode{to, code})
	return nil
}

func (f *fakeMail) SendBookingRequest(_ context.Context, admin, _, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bookingRequests = append(f.bookingRequests, admin)
	return nil
}

func (f *fakeMail) SendBookingDecision(_ context.Context, student, _, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions = append(f.decisions, [2]string{student, status})
	return nil
}

// fakeStorage stands in for Cloudinary so tests never touch the network.
type fakeStorage struct {
	configured    bool
	urlFmt, idFmt string // each take the upload counter
	uploads       int
	destroyed     []string
}

func (f *fakeStorage) Configured() bool { return f.configured }

func (f *fakeStorage) Upload(_ context.Context, _ []byte, _, _, _ string) (storage.Uploaded, error) {
	f.uploads++
	return storage.Uploaded{URL: fmt.Sprintf(f.urlFmt, f.uploads), PublicID: fmt.Sprintf(f.idFmt, f.uploads)}, nil
}

func (f *fakeStorage) Destroy(_ context.Context, id string) error {
	f.destroyed = append(f.destroyed, id)
	return nil
}

// fakeGoogle is a local stand-in for Places + Routes so tests never spend quota.
type fakeGoogle struct {
	srv    *httptest.Server
	client *google.Client
	mu     sync.Mutex
	calls  []string
	// where text search "finds" things
	textLat, textLng float64
	failRoutes       bool
}

func newFakeGoogle() *fakeGoogle {
	f := &fakeGoogle{textLat: 6.5180, textLng: 3.3980}
	mux := http.NewServeMux()
	rec := func(name string) { f.mu.Lock(); f.calls = append(f.calls, name); f.mu.Unlock() }
	mux.HandleFunc("POST /places:searchText", func(w http.ResponseWriter, r *http.Request) {
		rec("searchText")
		fmt.Fprintf(w, `{"places":[{"id":"ChIJ-school","displayName":{"text":"Found School"},"formattedAddress":"Akoka, Lagos","location":{"latitude":%v,"longitude":%v}}]}`, f.textLat, f.textLng)
	})
	mux.HandleFunc("POST /places:searchNearby", func(w http.ResponseWriter, r *http.Request) {
		rec("searchNearby")
		if r.Header.Get("X-Goog-Api-Key") != "test-key" || r.Header.Get("X-Goog-FieldMask") == "" {
			http.Error(w, `{"error":{"message":"bad auth","status":"PERMISSION_DENIED"}}`, 403)
			return
		}
		fmt.Fprint(w, `{"places":[
		 {"id":"p-far","displayName":{"text":"Far Pharmacy"},"formattedAddress":"1 Far Rd","location":{"latitude":6.54,"longitude":3.41},"rating":3.9,"userRatingCount":12,"types":["pharmacy"],"currentOpeningHours":{"openNow":false}},
		 {"id":"p-near","displayName":{"text":"Near Pharmacy"},"formattedAddress":"2 Near Rd","location":{"latitude":6.5165,"longitude":3.3905},"rating":4.6,"userRatingCount":88,"nationalPhoneNumber":"0803 000 0000","websiteUri":"https://near.example","googleMapsUri":"https://maps.google.com/?cid=1","types":["pharmacy","health"],"currentOpeningHours":{"openNow":true},"photos":[{"name":"places/p-near/photos/abc"}]}]}`)
	})
	mux.HandleFunc("GET /places/{id}", func(w http.ResponseWriter, r *http.Request) {
		rec("details")
		fmt.Fprintf(w, `{"id":%q,"displayName":{"text":"Near Pharmacy"},"formattedAddress":"2 Near Rd","rating":4.6,"userRatingCount":88,
		 "regularOpeningHours":{"weekdayDescriptions":["Monday: 8:00 AM – 9:00 PM","Tuesday: 8:00 AM – 9:00 PM"]},
		 "editorialSummary":{"text":"Friendly neighbourhood pharmacy."},
		 "reviews":[{"rating":5,"text":{"text":"Great service"},"relativePublishTimeDescription":"a week ago","authorAttribution":{"displayName":"Ada"}}]}`, r.PathValue("id"))
	})
	mux.HandleFunc("GET /places/{id}/photos/{photo}/media", func(w http.ResponseWriter, r *http.Request) {
		rec("photo")
		fmt.Fprint(w, `{"name":"x","photoUri":"https://lh3.googleusercontent.com/photo123"}`)
	})
	mux.HandleFunc("POST /routes", func(w http.ResponseWriter, r *http.Request) {
		rec("route")
		if f.failRoutes {
			http.Error(w, `{"error":{"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, 429)
			return
		}
		fmt.Fprint(w, `{"routes":[{"distanceMeters":2300,"duration":"1680s"}]}`)
	})
	f.srv = httptest.NewServer(mux)
	f.client = google.New("test-key")
	f.client.PlacesURL = f.srv.URL
	f.client.RoutesURL = f.srv.URL + "/routes"
	return f
}

func (f *fakeGoogle) disable() { f.client.APIKey = "" }

func (f *fakeGoogle) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

type env struct {
	t      *testing.T
	srv    *httptest.Server
	db     *db.DB
	mail   *fakeMail
	photos *fakeStorage
	google *fakeGoogle
}

// openTestDB returns an empty migrated database: in-memory SQLite by default, or
// — when TEST_DATABASE_URL points at a Postgres server — a throwaway database
// created on it for this test alone, so the whole suite also runs on Postgres.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "sqlite+aiosqlite:///:memory:"
	} else {
		admin, err := db.Open(url)
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("cribby_test_%d", time.Now().UnixNano())
		if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			admin.Close()
		})
		u, err := neturl.Parse(url)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name
		url = u.String()
	}
	d, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SyncSchools(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d
}

func newEnv(t *testing.T) *env {
	t.Helper()
	security.BcryptCost = bcrypt.MinCost // keep the suite fast
	d := openTestDB(t)
	e := &env{t: t, db: d, mail: &fakeMail{}, google: newFakeGoogle(), photos: &fakeStorage{
		configured: true, urlFmt: "https://res.cloudinary.com/test/photo%d.jpg", idFmt: "photo%d",
	}}
	cfg := config.Load()
	cfg.SecretKey = "test-secret"
	e.srv = httptest.NewServer(api.New(cfg, d, e.mail, e.photos, e.google.client, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(func() { e.srv.Close(); e.google.srv.Close(); d.Close() })
	return e
}

// obj is a decoded JSON object.
type obj = map[string]any

func (e *env) do(method, path, token string, body any) (int, any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+"/api/v1"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return e.send(req, token)
}

func (e *env) send(req *http.Request, token string) (int, any) {
	e.t.Helper()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out any
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("%s %s: non-JSON response %q", req.Method, req.URL.Path, raw)
		}
	}
	return resp.StatusCode, out
}

func (e *env) upload(path, token, filename, contentType string, data []byte, fields map[string]string) (int, any) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1"+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return e.send(req, token)
}

func (e *env) expect(status int, method, path, token string, body any) any {
	e.t.Helper()
	got, out := e.do(method, path, token, body)
	if got != status {
		e.t.Fatalf("%s %s: status %d, want %d (body %v)", method, path, got, status, out)
	}
	return out
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(), q, args...); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// signup registers a user and returns (token, user object).
func (e *env) signup(email string) (string, obj) {
	e.t.Helper()
	body := e.expect(200, "POST", "/auth/signup", "", obj{"email": email, "password": "pw123456", "name": "Test"}).(obj)
	return body["access_token"].(string), body["user"].(obj)
}

func (e *env) makeAdmin(id int) { e.exec(`UPDATE users SET is_admin = ? WHERE id = ?`, true, id) }
func (e *env) verifyUser(id int) {
	e.exec(`UPDATE users SET verification_status = 'verified' WHERE id = ?`, id)
}

func (e *env) createListing() int { return e.createListingAt("Cozy studio", nil, nil) }

func (e *env) createListingAt(title string, lat, lng *float64) int {
	e.t.Helper()
	id, err := e.db.InsertID(context.Background(), `INSERT INTO listings (title, type, price_per, total, utilities_monthly, deposit, walk,
		latitude, longitude, rating, reviews, verified, hot, video, is_hidden, grad, emoji, amenities, photos,
		landlord_name, landlord_verified, landlord_response_rate)
		VALUES (?, 'Studio', 800, 800, 45, 800, 5, ?, ?, 0, 0, false, false, false, false, 'from-coral to-sunshine', '🏠', '[]', '[]', 'Sandra', true, 95)`,
		title, lat, lng)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) createPlace(name, category string, lat, lng float64) {
	e.exec(`INSERT INTO places (name, category, latitude, longitude, emoji) VALUES (?,?,?,?,'📍')`, name, category, lat, lng)
}

func (e *env) createRoommateProfile(token string) int {
	e.t.Helper()
	out := e.expect(201, "POST", "/roommates", token, obj{
		"name": "Zoe", "age": 20, "school": "State University", "year": "Sophomore",
		"budget_min": 500, "budget_max": 900, "move_in_date": "Aug 1",
	}).(obj)
	return id(out)
}

func past() db.Time { return db.Time{Time: time.Now().UTC().Add(-time.Minute)} }

// id reads a JSON number field named "id".
func id(o obj) int { return int(o["id"].(float64)) }

func num(o obj, key string) int { return int(o[key].(float64)) }

func list(v any) []any { return v.([]any) }

func path(format string, a ...any) string { return fmt.Sprintf(format, a...) }

func syncSchools(e *env) (bool, error) { return store.SyncSchools(context.Background(), e.db) }
