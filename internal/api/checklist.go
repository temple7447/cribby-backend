package api

import (
	"net/http"
	"strings"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// The visit checklist: what the person who filmed or visited a home found. The condition, water,
// power and permission are public; the landlord's number is only for the owner, admins and verifiers.

// attachChecklist adds the checklist to a single listing for the response, hiding the phone from
// people who shouldn't see it.
func (s *Server) attachChecklist(r *http.Request, l *model.Listing) error {
	if l == nil {
		return nil
	}
	if b, err := store.ActiveBoosts(r.Context(), s.DB.Executor, []int{l.ID}); err != nil {
		return err
	} else {
		l.Boosted = b[l.ID]
	}
	c, err := store.ChecklistFor(r.Context(), s.DB.Executor, l.ID)
	if err != nil || c == nil {
		return err
	}
	if !s.mayReadContact(r, l) {
		c.ContactPhone = ""
	}
	l.Checklist = c
	return nil
}

func (s *Server) mayReadContact(r *http.Request, l *model.Listing) bool {
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		return false
	}
	user, err := s.currentUser(r)
	if err != nil {
		return false
	}
	if ownsListing(user, l) == nil {
		return true
	}
	ok, _ := s.canVerify(r, user)
	return ok
}

var (
	checklistConditions = map[string]bool{"": true, "good": true, "fair": true, "needs_work": true}
	checklistWater      = map[string]bool{"": true, "tap": true, "borehole": true, "well": true, "none": true}
	checklistPower      = map[string]bool{"": true, "always": true, "most_of_day": true, "few_hours": true, "none": true}
)

// putChecklist: PUT /listings/{id}/checklist, by the owner (or an admin).
func (s *Server) putChecklist(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	l, err := s.listingForOwner(r, user)
	if err != nil {
		return err
	}
	var in model.Checklist
	if err := bindJSON(r, &in); err != nil {
		return err
	}
	if !checklistConditions[in.Condition] || !checklistWater[in.Water] || !checklistPower[in.Power] {
		return httpErr(http.StatusUnprocessableEntity, "Choose one of the listed options")
	}
	if in.ContactPhone != "" {
		if in.ContactPhone = normalizePhone(in.ContactPhone); in.ContactPhone == "" {
			return httpErr(http.StatusUnprocessableEntity, "Enter a valid phone number for the landlord")
		}
	}
	ctx, ex := r.Context(), s.DB.Executor
	if err := store.UpsertChecklist(ctx, ex, l.ID, in); err != nil {
		return err
	}
	// The same landlord number on a home someone else posted: hold this one for an admin to compare.
	if owner := user.ID; l.OwnerID != nil {
		owner = *l.OwnerID
		if dup, err := store.FindListingByPhoneOfOtherOwner(ctx, ex, in.ContactPhone, owner, l.ID); err != nil {
			return err
		} else if dup != 0 {
			if flags, _ := store.DuplicateFlags(ctx, ex, []int{l.ID}); len(flags) == 0 {
				if err := store.FlagDuplicate(ctx, ex, l.ID, dup); err != nil {
					return err
				}
			}
			if l.Status == model.ListingApproved && !user.IsAdmin {
				if err := store.SetListingStatus(ctx, ex, l.ID, model.ListingPending, nil); err != nil {
					return err
				}
			}
		}
	}
	return s.respondListing(w, r, l.ID)
}
