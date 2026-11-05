package api

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"campus-backend/internal/model"
	"campus-backend/internal/store"
)

// Landlord tools: anyone who has posted a listing can see the booking requests and chats that
// students started about it, and answer them. Everything here is scoped to the signed-in user's
// own listings, so a user with no listings simply gets empty lists.

const (
	maxMessageLen = 2000
	// A burst of messages is one email: we only email again after a quiet spell, or when the
	// other person has spoken in between.
	notifyGap = 15 * time.Minute
)

// shouldNotify decides whether a new message from `sender` deserves an email, given the message
// that came before it in the same conversation.
func shouldNotify(prev *model.Message, sender string, now time.Time) bool {
	return prev == nil || prev.Sender != sender || now.Sub(prev.CreatedAt.Time) > notifyGap
}

func (s *Server) ownerSummary(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	unread, err := store.CountUnreadForOwner(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	pending, err := store.CountPendingBookingsForOwner(ctx, ex, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread_messages": unread, "pending_bookings": pending})
	return nil
}

func (s *Server) ownerConversations(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	out, err := store.ListConversationsForOwner(r.Context(), s.DB.Executor, user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ownerConversation opens a chat (and marks the student's messages as read).
func (s *Server) ownerConversation(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "conversation_id")
	if err != nil {
		return err
	}
	ctx, ex := r.Context(), s.DB.Executor
	c, err := store.ConversationForOwner(ctx, ex, id, user.ID)
	if err != nil {
		return err
	}
	if c == nil {
		return httpErr(http.StatusNotFound, "Conversation not found")
	}
	if err := store.MarkStudentMessagesRead(ctx, ex, c.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

// ownerReply posts the landlord's answer. It shows up in the student's chat as a "them" message.
func (s *Server) ownerReply(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	id, err := pathInt(r, "conversation_id")
	if err != nil {
		return err
	}
	var p struct {
		Text string `json:"text" validate:"required"`
	}
	if err := bindJSON(r, &p); err != nil {
		return err
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return httpErr(http.StatusUnprocessableEntity, "Write a message first")
	}
	if utf8.RuneCountInString(text) > maxMessageLen {
		return httpErr(http.StatusUnprocessableEntity, "Message is too long (2000 characters max)")
	}
	ctx, ex := r.Context(), s.DB.Executor
	c, err := store.ConversationForOwner(ctx, ex, id, user.ID)
	if err != nil {
		return err
	}
	if c == nil {
		return httpErr(http.StatusNotFound, "Conversation not found")
	}
	if err := s.rejectIfBlocked(ctx, user.ID, c.StudentID); err != nil {
		return err
	}
	prev, err := store.LastMessage(ctx, ex, c.ID)
	if err != nil {
		return err
	}
	msg, err := store.AddMessageRead(ctx, ex, c.ID, "them", text, true)
	if err != nil {
		return err
	}
	// Students don't have push notifications; an email is how they learn they were answered.
	if shouldNotify(prev, "them", time.Now()) {
		if student, err := store.UserByID(ctx, ex, c.StudentID); err == nil && student != nil {
			if err := s.mail.SendNewMessage(ctx, student.Email, publicName(user), c.ListingTitle, text, false); err != nil {
				s.log.Warn("new-message email failed", "to", student.Email, "err", err)
			}
		}
	}
	writeJSON(w, http.StatusCreated, msg)
	return nil
}

func (s *Server) ownerBookings(w http.ResponseWriter, r *http.Request) error {
	user, err := s.currentUser(r)
	if err != nil {
		return err
	}
	q := newQuery(r)
	status := q.str("status")
	if err := q.err(); err != nil {
		return err
	}
	rows, err := store.ListBookingsForOwner(r.Context(), s.DB.Executor, user.ID, status)
	if err != nil {
		return err
	}
	out := make([]model.OwnerBooking, 0, len(rows))
	for _, b := range rows {
		ob := model.OwnerBooking{
			ID: b.ID, ListingID: b.ListingID, ListingTitle: b.Listing.Title, MoveInDate: b.MoveInDate,
			LeaseMonths: b.LeaseMonths, Status: b.Status, MessageToLandlord: b.MessageToLandlord,
			GuarantorName: b.GuarantorName, CreatedAt: b.CreatedAt,
			Student: model.StudentSummary{Name: b.User.Name, School: b.User.School, Verified: b.User.VerificationStatus == "verified"},
		}
		if b.Status == "approved" { // contact details are shared only once the landlord says yes
			email := b.User.Email
			ob.Student.Email = &email
		}
		out = append(out, ob)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}
