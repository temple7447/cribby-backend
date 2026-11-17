package api

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Campus landing pages. Anyone can read them; an admin creates one per school.

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// Names the website already uses as its own pages: a campus can't take them.
var reservedSlugs = map[string]bool{
	"listings": true, "login": true, "signup": true, "dashboard": true, "list-your-property": true,
	"forgot-password": true, "privacy": true, "support": true, "delete-account": true, "admin": true,
	"api": true, "c": true, "app": true, "terms": true, "fresher": true, "freshers": true, "scams": true,
}

const campusHomesRadiusKM = 5.0

// withHomes adds how many live homes are within 5 km of the campus.
func (s *Server) withHomes(r *http.Request, c *model.Campus) error {
	if c.School.Latitude == nil || c.School.Longitude == nil {
		return nil
	}
	rad := campusHomesRadiusKM
	homes, err := store.SearchListings(r.Context(), s.DB.Executor, store.ListingSearch{
		NearLat: c.School.Latitude, NearLng: c.School.Longitude, RadiusKM: &rad})
	if err != nil {
		return err
	}
	c.HomesNear = len(homes)
	return nil
}

func (s *Server) listCampuses(w http.ResponseWriter, r *http.Request) error {
	out, err := store.ListCampuses(r.Context(), s.DB.Executor, true)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getCampus(w http.ResponseWriter, r *http.Request) error {
	c, err := store.CampusBySlug(r.Context(), s.DB.Executor, strings.ToLower(r.PathValue("slug")))
	if err != nil {
		return err
	}
	if c == nil || !c.Active {
		return httpErr(http.StatusNotFound, "Campus not found")
	}
	if err := s.withHomes(r, c); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

func (s *Server) adminListCampuses(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	out, err := store.ListCampuses(r.Context(), s.DB.Executor, false)
	if err != nil {
		return err
	}
	for i := range out {
		if err := s.withHomes(r, &out[i]); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) adminSaveCampus(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	slug := strings.ToLower(r.PathValue("slug"))
	if !slugRe.MatchString(slug) || reservedSlugs[slug] {
		return httpErr(http.StatusUnprocessableEntity, "Use 3-40 letters, numbers or dashes that isn't a page name already in use")
	}
	p := struct {
		SchoolID    int    `json:"school_id" validate:"required"`
		WhatsAppURL string `json:"whatsapp_url"`
		Tagline     string `json:"tagline"`
		Active      *bool  `json:"active"`
	}{}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	wa := strings.TrimSpace(p.WhatsAppURL)
	if wa != "" {
		u, err := url.Parse(wa)
		if err != nil || u.Scheme != "https" || (u.Host != "chat.whatsapp.com" && u.Host != "wa.me" && u.Host != "whatsapp.com" && u.Host != "www.whatsapp.com") {
			return httpErr(http.StatusUnprocessableEntity, "That doesn't look like a WhatsApp link (https://chat.whatsapp.com/…)")
		}
	}
	tag := strings.TrimSpace(p.Tagline)
	if len([]rune(tag)) > 200 {
		return httpErr(http.StatusUnprocessableEntity, "Tagline is too long (200 characters max)")
	}
	ctx, ex := r.Context(), s.DB.Executor
	if sch, err := store.SchoolByID(ctx, ex, p.SchoolID); err != nil {
		return err
	} else if sch == nil {
		return httpErr(http.StatusNotFound, "School not found")
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	if err := store.UpsertCampus(ctx, ex, slug, p.SchoolID, wa, tag, active); err != nil {
		return err
	}
	return s.adminListCampuses(w, r)
}

func (s *Server) adminDeleteCampus(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	if err := store.DeleteCampus(r.Context(), s.DB.Executor, strings.ToLower(r.PathValue("slug"))); err != nil {
		return err
	}
	noContent(w)
	return nil
}

type campusGrowth struct {
	Slug        string `json:"slug"`
	School      string `json:"school"`
	Active      bool   `json:"active"`
	Users       int    `json:"users"`
	Users7d     int    `json:"users_7d"`      // joined in the last 7 days
	UsersPrev7d int    `json:"users_prev_7d"` // joined in the 7 days before that
	Roommates   int    `json:"roommates"`
	Requests    int    `json:"requests"`
	Rentals     int    `json:"rentals"`
	HomesNear   int    `json:"homes_near"`
}

// adminCampusGrowth: how each campus page's school is doing, fastest-growing first, plus the most popular
// schools that don't have a page yet.
func (s *Server) adminCampusGrowth(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	campuses, err := store.ListCampuses(ctx, ex, false)
	if err != nil {
		return err
	}
	rows := make([]campusGrowth, 0, len(campuses))
	for i := range campuses {
		c := &campuses[i]
		if err := s.withHomes(r, c); err != nil {
			return err
		}
		st, err := store.GrowthFor(ctx, ex, c.School.ID, time.Now().UTC())
		if err != nil {
			return err
		}
		rows = append(rows, campusGrowth{Slug: c.Slug, School: c.School.Name, Active: c.Active, Users: st.Users, Users7d: st.Users7d,
			UsersPrev7d: st.UsersPrev7d, Roommates: st.Roommates, Requests: st.Requests, Rentals: st.Rentals, HomesNear: c.HomesNear})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Users7d != rows[j].Users7d {
			return rows[i].Users7d > rows[j].Users7d
		}
		return rows[i].Users > rows[j].Users
	})
	next, err := store.TopSchoolsWithoutCampus(ctx, ex, 10)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"campuses": rows, "open_next": next})
	return nil
}
