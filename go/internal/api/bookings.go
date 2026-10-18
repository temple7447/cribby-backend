package api

import (
	"fmt"
	"net/http"

	"campus-backend/internal/db"
	"campus-backend/internal/store"
)

func (s *Server) listMyBookings(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	q := newQuery(r)
	status := q.optStr("status_filter")
	if err := q.err(); err != nil {
		return err
	}
	bookings, err := store.ListBookingsForUser(r.Context(), s.DB.Executor, user.ID, status)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, bookings)
	return nil
}

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	p := struct {
		ListingID         int     `json:"listing_id" validate:"required"`
		MoveInDate        db.Date `json:"move_in_date" validate:"required"`
		LeaseMonths       int     `json:"lease_months"`
		GuarantorName     *string `json:"guarantor_name"`
		GuarantorEmail    *string `json:"guarantor_email" validate:"email"`
		MessageToLandlord *string `json:"message_to_landlord"`
	}{LeaseMonths: 12}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	listing, err := store.ListingByID(ctx, ex, p.ListingID)
	if err != nil {
		return err
	}
	if listing == nil {
		return httpErr(http.StatusNotFound, "Listing not found")
	}
	booking, err := store.CreateBooking(ctx, ex, store.NewBooking{
		UserID: user.ID, ListingID: p.ListingID, LeaseMonths: p.LeaseMonths, MoveInDate: p.MoveInDate,
		GuarantorName: p.GuarantorName, GuarantorEmail: p.GuarantorEmail, MessageToLandlord: p.MessageToLandlord,
	})
	if err != nil {
		return err
	}

	// Best-effort notification — a landlord (in practice: admin, since there's no
	// real landlord-account model yet) should hear about new requests, but a
	// flaky email send must never fail the booking itself.
	if admins, err := store.ListAdminEmails(ctx, ex); err == nil {
		for _, to := range admins {
			if err := s.mail.SendBookingRequest(ctx, to, user.Name, listing.Title, p.MoveInDate.String()); err != nil {
				s.log.Warn("booking request email failed", "to", to, "err", err)
			}
		}
	}
	writeJSON(w, http.StatusCreated, booking)
	return nil
}

func (s *Server) listPendingBookings(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	pending, err := store.ListPendingBookings(r.Context(), s.DB.Executor)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pending)
	return nil
}

func (s *Server) getBooking(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "booking_id")
	if err != nil {
		return err
	}
	b, err := store.BookingForUser(r.Context(), s.DB.Executor, id, user.ID)
	if err != nil {
		return err
	}
	if b == nil {
		return httpErr(http.StatusNotFound, "Booking not found")
	}
	writeJSON(w, http.StatusOK, b)
	return nil
}

func (s *Server) decideBooking(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.currentAdmin(r); err != nil {
		return err
	}
	id, err := pathInt(r, "booking_id")
	if err != nil {
		return err
	}
	var p struct {
		Status string `json:"status" validate:"required,oneof=approved|rejected"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	booking, err := store.BookingWithRelations(ctx, ex, id)
	if err != nil {
		return err
	}
	if booking == nil {
		return httpErr(http.StatusNotFound, "Booking not found")
	}
	if booking.Status != "pending" {
		return httpErr(http.StatusBadRequest, fmt.Sprintf("Booking is already %s, not pending", booking.Status))
	}
	if err := store.DecideBooking(ctx, ex, id, p.Status); err != nil {
		return err
	}
	decided, err := store.BookingWithRelations(ctx, ex, id)
	if err != nil {
		return err
	}

	if err := s.mail.SendBookingDecision(ctx, decided.User.Email, decided.Listing.Title, decided.Status); err != nil {
		s.log.Warn("booking decision email failed", "to", decided.User.Email, "err", err)
	}
	writeJSON(w, http.StatusOK, decided.Booking)
	return nil
}

func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "booking_id")
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	booking, err := store.BookingForUser(ctx, ex, id, user.ID)
	if err != nil {
		return err
	}
	if booking == nil {
		return httpErr(http.StatusNotFound, "Booking not found")
	}
	if booking.Status == "rejected" || booking.Status == "canceled" {
		return httpErr(http.StatusBadRequest, fmt.Sprintf("Booking is already %s", booking.Status))
	}
	if err := store.CancelBooking(ctx, ex, id); err != nil {
		return err
	}
	canceled, err := store.BookingForUser(ctx, ex, id, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, canceled)
	return nil
}
