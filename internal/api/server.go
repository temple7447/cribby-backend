// Package api is the HTTP layer: routing, auth, validation and the handlers
// for every endpoint of the original FastAPI app, under the same /api/v1 paths.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"campus-backend/internal/ai"
	"campus-backend/internal/config"
	"campus-backend/internal/db"
	"campus-backend/internal/email"
	"campus-backend/internal/google"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

type Server struct {
	cfg    *config.Settings
	DB     *db.DB
	mail   email.Sender
	photos storage.Storage
	maps   *google.Client
	// assistant is the OpenRouter-backed Cribby AI; chats rate-limits it per user.
	assistant *ai.Client
	chats     *chatLimiter
	mapsLimit *chatLimiter // per-user cap on Google-backed calls (Google bills per request)
	log       *slog.Logger
	mux       *http.ServeMux
}

func New(cfg *config.Settings, database *db.DB, mail email.Sender, photos storage.Storage, maps *google.Client, assistant *ai.Client, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, DB: database, mail: mail, photos: photos, maps: maps, assistant: assistant, chats: newChatLimiter(), mapsLimit: newLimiter(150, 10*time.Minute), log: log, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler is the fully wrapped http.Handler.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.cors(s.accessLog(s.mux)))
}

func (s *Server) routes() {
	p := s.cfg.APIV1Str
	m := s.mux

	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// auth
	m.Handle("POST "+p+"/auth/signup", s.h(s.signup))
	m.Handle("POST "+p+"/auth/login", s.h(s.login))
	m.Handle("POST "+p+"/auth/verify/request", s.h(s.requestVerification))
	m.Handle("POST "+p+"/auth/verify/confirm", s.h(s.confirmVerification))
	m.Handle("POST "+p+"/auth/password-reset/request", s.h(s.requestPasswordReset))
	m.Handle("POST "+p+"/auth/password-reset/confirm", s.h(s.confirmPasswordReset))

	// users
	m.Handle("GET "+p+"/users/me", s.h(s.readMe))
	m.Handle("PATCH "+p+"/users/me", s.h(s.updateMe))
	m.Handle("DELETE "+p+"/users/me", s.h(s.deleteMe))
	m.Handle("POST "+p+"/users/me/avatar", s.h(s.uploadMyAvatar))
	m.Handle("DELETE "+p+"/users/me/avatar", s.h(s.deleteMyAvatar))
	m.Handle("GET "+p+"/users", s.h(s.listUsers))
	m.Handle("GET "+p+"/users/{user_id}", s.h(s.getUserDetail))
	m.Handle("PATCH "+p+"/users/{user_id}/admin", s.h(s.setAdmin))
	m.Handle("PATCH "+p+"/users/{user_id}/suspend", s.h(s.setSuspended))

	// listings
	m.Handle("GET "+p+"/listings", s.h(s.listListings))
	m.Handle("POST "+p+"/listings", s.h(s.createListing))
	m.Handle("GET "+p+"/listings/mine", s.h(s.listMyListings))
	m.Handle("GET "+p+"/listings/{listing_id}", s.h(s.getListing))
	m.Handle("PATCH "+p+"/listings/{listing_id}", s.h(s.updateListing))
	m.Handle("DELETE "+p+"/listings/{listing_id}", s.h(s.deleteListing))
	m.Handle("GET "+p+"/admin/listings", s.h(s.adminListListings))
	m.Handle("PATCH "+p+"/admin/listings/{listing_id}/review", s.h(s.adminReviewListing))
	m.Handle("POST "+p+"/listings/{listing_id}/photos", s.h(s.uploadListingPhoto))
	m.Handle("DELETE "+p+"/listings/{listing_id}/photos/{photo_id}", s.h(s.deleteListingPhoto))

	// AI concierge (OpenRouter)
	m.Handle("POST "+p+"/concierge/chat", s.h(s.conciergeChat))

	// admin AI (advisory only)
	m.Handle("POST "+p+"/admin/ai/review-report", s.h(s.adminReviewReport))
	m.Handle("POST "+p+"/admin/ai/chat", s.h(s.adminChat))

	// schools
	m.Handle("GET "+p+"/schools", s.h(s.listSchools))
	m.Handle("GET "+p+"/schools/{school_id}", s.h(s.getSchool))

	// google maps (open to guests, rate-limited per user or per IP: Google bills per call)
	m.Handle("GET "+p+"/maps/status", s.h(s.mapsStatus))
	m.Handle("GET "+p+"/maps/route", s.h(s.mapsRoute))
	m.Handle("GET "+p+"/maps/places/nearby", s.h(s.mapsNearby))
	m.Handle("GET "+p+"/maps/places/search", s.h(s.mapsSearch))
	m.Handle("GET "+p+"/maps/places/photo", s.h(s.mapsPhoto))
	m.Handle("GET "+p+"/maps/places/{place_id}", s.h(s.mapsPlaceDetails))

	// places
	m.Handle("GET "+p+"/places", s.h(s.listPlaces))
	m.Handle("POST "+p+"/places", s.h(s.createPlace))
	m.Handle("GET "+p+"/places/{place_id}", s.h(s.getPlace))
	m.Handle("DELETE "+p+"/places/{place_id}", s.h(s.deletePlace))

	// bookings ("/pending" is a literal, so it wins over "/{booking_id}")
	m.Handle("GET "+p+"/bookings", s.h(s.listMyBookings))
	m.Handle("POST "+p+"/bookings", s.h(s.createBooking))
	m.Handle("GET "+p+"/bookings/pending", s.h(s.listPendingBookings))
	m.Handle("GET "+p+"/bookings/{booking_id}", s.h(s.getBooking))
	m.Handle("PATCH "+p+"/bookings/{booking_id}", s.h(s.decideBooking))
	m.Handle("PATCH "+p+"/bookings/{booking_id}/cancel", s.h(s.cancelBooking))

	// roommates
	m.Handle("GET "+p+"/roommates", s.h(s.listRoommates))
	m.Handle("POST "+p+"/roommates", s.h(s.createRoommateProfile))
	m.Handle("GET "+p+"/roommates/me", s.h(s.getMyRoommateProfile))
	m.Handle("PATCH "+p+"/roommates/me", s.h(s.updateMyRoommateProfile))
	m.Handle("POST "+p+"/roommates/me/banner", s.h(s.uploadMyRoommateBanner))
	m.Handle("DELETE "+p+"/roommates/me/banner", s.h(s.deleteMyRoommateBanner))
	m.Handle("GET "+p+"/roommates/{roommate_id}", s.h(s.getRoommate))

	// favorites
	m.Handle("GET "+p+"/favorites", s.h(s.listFavorites))
	m.Handle("PUT "+p+"/favorites/{listing_id}", s.h(s.addFavorite))
	m.Handle("DELETE "+p+"/favorites/{listing_id}", s.h(s.removeFavorite))

	// conversations
	m.Handle("GET "+p+"/conversations", s.h(s.listConversations))
	m.Handle("POST "+p+"/conversations", s.h(s.startConversation))
	m.Handle("GET "+p+"/conversations/{conversation_id}", s.h(s.getConversation))
	m.Handle("POST "+p+"/conversations/{conversation_id}/messages", s.h(s.sendMessage))

	// reports & blocks
	m.Handle("POST "+p+"/reports", s.h(s.createReport))
	m.Handle("GET "+p+"/reports", s.h(s.listReports))
	m.Handle("PATCH "+p+"/reports/{report_id}", s.h(s.updateReport))
	m.Handle("GET "+p+"/blocks", s.h(s.listBlocks))
	m.Handle("POST "+p+"/blocks", s.h(s.createBlock))
	m.Handle("DELETE "+p+"/blocks/{blocked_user_id}", s.h(s.deleteBlock))

	// Catch-all: FastAPI answers 405 when the path exists for another method, else 404.
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if allow := s.allowedMethods(r); len(allow) > 0 {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"detail": "Method Not Allowed"})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "Not Found"})
	})
}

// allowedMethods lists the methods that have a real route for r's path.
func (s *Server) allowedMethods(r *http.Request) []string {
	var allow []string
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := s.mux.Handler(probe); pattern != "" && pattern != "/" {
			allow = append(allow, method)
		}
	}
	return allow
}

// ---- auth dependencies ----

var errNotAuthenticated = &APIError{
	Status: http.StatusUnauthorized, Detail: "Not authenticated",
	Header: http.Header{"Www-Authenticate": {"Bearer"}},
}

var errBadCredentials = &APIError{
	Status: http.StatusUnauthorized, Detail: "Could not validate credentials",
	Header: http.Header{"Www-Authenticate": {"Bearer"}},
}

// currentUser resolves the bearer token to a user (FastAPI's get_current_user).
func (s *Server) currentUser(r *http.Request) (*model.User, error) {
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !strings.EqualFold(scheme, "bearer") || token == "" {
		return nil, errNotAuthenticated
	}
	email, ok := security.DecodeAccessToken(s.cfg.SecretKey, token)
	if !ok {
		return nil, errBadCredentials
	}
	user, err := store.UserByEmail(r.Context(), s.DB.Executor, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errBadCredentials
	}
	if user.IsSuspended {
		// Cuts off an already-issued token immediately — suspending someone
		// mid-session shouldn't wait for their token to expire.
		return nil, httpErr(http.StatusForbidden, "Your account has been suspended")
	}
	if err := store.TouchActive(r.Context(), s.DB.Executor, user); err != nil {
		s.log.Warn("recording activity failed", "err", err) // never fail a request over this
	}
	return user, nil
}

func (s *Server) currentAdmin(r *http.Request) (*model.User, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, err
	}
	if !user.IsAdmin {
		return nil, httpErr(http.StatusForbidden, "Admin access required")
	}
	return user, nil
}

func (s *Server) newToken(subject string) (string, error) {
	return security.CreateAccessToken(s.cfg.SecretKey, subject,
		time.Duration(s.cfg.AccessTokenExpireMinutes)*time.Minute)
}

// ---- middleware ----

// Testing the Expo web build from a physical phone means the frontend is loaded
// from the dev machine's LAN IP, which changes across networks — so any
// private-network origin is allowed. Only affects local dev origins.
var privateNetOrigin = regexp.MustCompile(`^http://(192\.168|10\.\d{1,3}|172\.(1[6-9]|2\d|3[01]))\.\d{1,3}\.\d{1,3}:\d+$`)

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.cfg.BackendCORSOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		origin := r.Header.Get("Origin")
		isPreflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

		// Responses vary by Origin, so shared caches must be told — even for
		// requests without one.
		if isPreflight && origin != "" {
			h.Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers, Access-Control-Request-Private-Network")
		} else {
			h.Add("Vary", "Origin")
		}
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		ok := allowed[origin] || privateNetOrigin.MatchString(origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		if ok {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if isPreflight {
			h.Set("Access-Control-Allow-Methods", "DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT")
			h.Set("Access-Control-Max-Age", "600")
			if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
				h.Set("Access-Control-Allow-Headers", reqHeaders)
			}
			h.Set("Content-Type", "text/plain; charset=utf-8")
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("Disallowed CORS origin"))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
				s.log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(rec))
				writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Internal Server Error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- small helpers shared by handlers ----

// uploadImage enforces the storage rules in the original order — configured
// (503), then type/size (400) — and uploads the photo.
func (s *Server) uploadImage(ctx context.Context, up *upload, folder, transformation string) (storage.Uploaded, error) {
	if !s.photos.Configured() {
		return storage.Uploaded{}, httpErr(http.StatusServiceUnavailable, "Photo storage is not configured on this environment")
	}
	data, err := up.readImage()
	if err != nil {
		return storage.Uploaded{}, err
	}
	out, err := s.photos.Upload(ctx, data, "", folder, transformation)
	if err != nil {
		s.log.Error("photo upload failed", "err", err)
		return storage.Uploaded{}, httpErr(http.StatusBadGateway, "Couldn't upload photo — try again")
	}
	return out, nil
}

// destroyQuietly removes a replaced asset after the DB change is committed;
// a failure leaves an orphaned image, which is not worth failing the request.
func (s *Server) destroyQuietly(ctx context.Context, publicID *string) {
	if publicID == nil || *publicID == "" || !s.photos.Configured() {
		return
	}
	if err := s.photos.Destroy(ctx, *publicID); err != nil {
		s.log.Warn("failed to delete old asset", "public_id", *publicID, "err", err)
	}
}
