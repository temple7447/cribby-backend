package api

import (
	"errors"
	"net/http"

	"campus-backend/internal/db"
	"campus-backend/internal/store"
)

// The admin dashboard can erase what it shows. Anything a payment record points at is refused (409): those
// records are Cribby's accounts, so the home or request is taken down instead.

const hasPaymentsMsg = "A payment was taken for this, and payment records are kept. Take it down instead"

// adminDeleteUser erases an account like "delete my account" does, but without the person's password.
func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) error {
	admin, err := s.currentAdmin(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "user_id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	target, err := store.UserByID(ctx, s.DB.Executor, id)
	if err != nil {
		return err
	}
	if target == nil {
		return httpErr(http.StatusNotFound, "User not found")
	}
	if target.ID == admin.ID {
		return httpErr(http.StatusBadRequest, "You can't delete your own account here")
	}
	if target.IsAdmin {
		return httpErr(http.StatusConflict, "Remove their admin rights first")
	}
	if busy, err := store.MoneyInFlight(ctx, s.DB.Executor, id); err != nil {
		return err
	} else if busy {
		return httpErr(http.StatusConflict, "Money is being held or paid out for this person. Finish that first")
	}
	s.dropIDPhotos(ctx, id)
	if err := s.DB.InTx(ctx, func(tx db.Executor) error { return store.DeleteUserCascade(ctx, tx, id) }); err != nil {
		return err
	}
	noContent(w)
	return nil
}

// adminEraseListing removes a home for good (the existing "take down" keeps it for the records).
func (s *Server) adminEraseListing(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "listing_id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	l, err := store.ListingByID(ctx, s.DB.Executor, id)
	if err != nil {
		return err
	}
	if l == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	err = s.DB.InTx(ctx, func(tx db.Executor) error { return store.DeleteListingHard(ctx, tx, id) })
	if errors.Is(err, store.ErrHasPayments) {
		return httpErr(http.StatusConflict, hasPaymentsMsg)
	}
	if err != nil {
		return err
	}
	// Files go last and never block the deletion.
	if s.photos.Configured() {
		for _, p := range l.Photos {
			if err := s.photos.Destroy(ctx, p.PublicID); err != nil {
				s.log.Error("erasing listing photo", "err", err)
			}
		}
		if l.VideoPublicID != nil {
			if err := s.photos.DestroyVideo(ctx, *l.VideoPublicID); err != nil {
				s.log.Error("erasing listing video", "err", err)
			}
		}
	}
	noContent(w)
	return nil
}

func (s *Server) adminDeleteBooking(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "booking_id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	var found bool
	err = s.DB.InTx(ctx, func(tx db.Executor) (e error) {
		found, e = store.DeleteBookingHard(ctx, tx, id)
		return e
	})
	if errors.Is(err, store.ErrHasPayments) {
		return httpErr(http.StatusConflict, hasPaymentsMsg)
	}
	if err != nil {
		return err
	}
	if !found {
		return httpErr(http.StatusNotFound, "Booking not found")
	}
	noContent(w)
	return nil
}

// simpleErase wraps the one-row deletes: admin only, 404 if it isn't there.
func (s *Server) simpleErase(w http.ResponseWriter, r *http.Request, what string, del func(id int) (bool, error)) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, what)
	if err != nil {
		return err
	}
	found, err := del(id)
	if err != nil {
		return err
	}
	if !found {
		return httpErr(http.StatusNotFound, "Not found")
	}
	noContent(w)
	return nil
}

func (s *Server) adminDeleteVendor(w http.ResponseWriter, r *http.Request) error {
	return s.simpleErase(w, r, "vendor_id", func(id int) (bool, error) { return store.DeleteVendor(r.Context(), s.DB.Executor, id) })
}

func (s *Server) adminDeleteScamAlert(w http.ResponseWriter, r *http.Request) error {
	return s.simpleErase(w, r, "alert_id", func(id int) (bool, error) { return store.DeleteScamAlert(r.Context(), s.DB.Executor, id) })
}

func (s *Server) adminDeleteClientError(w http.ResponseWriter, r *http.Request) error {
	return s.simpleErase(w, r, "error_id", func(id int) (bool, error) { return store.DeleteClientError(r.Context(), s.DB.Executor, id) })
}

func (s *Server) adminDeleteReferral(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	found, err := store.DeleteReferralCode(r.Context(), s.DB.Executor, r.PathValue("code"))
	if err != nil {
		return err
	}
	if !found {
		return httpErr(http.StatusNotFound, "Code not found")
	}
	noContent(w)
	return nil
}
