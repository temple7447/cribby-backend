package store

import (
	"context"
	"sort"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// ---- bookings on a landlord's listings ----

// ListBookingsForOwner returns booking requests on the listings this user posted, newest first.
func ListBookingsForOwner(ctx context.Context, ex db.Executor, ownerID int, status string) ([]model.BookingAdmin, error) {
	q := `SELECT ` + bookingAdminCols + bookingAdminFrom + `WHERE l.owner_id = ?`
	args := []any{ownerID}
	if status != "" {
		q += ` AND b.status = ?`
		args = append(args, status)
	}
	return queryList(ctx, ex, scanBookingAdmin, q+` ORDER BY b.created_at DESC, b.id DESC`, args...)
}

func CountPendingBookingsForOwner(ctx context.Context, ex db.Executor, ownerID int) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM bookings b JOIN listings l ON l.id = b.listing_id
		WHERE l.owner_id = ? AND b.status = 'pending'`, ownerID).Scan(&n)
	return n, err
}

// ---- conversations about a landlord's listings ----

const ownerConversationSQL = `SELECT c.id, c.listing_id, l.title, c.user_id, u.name, u.school, u.verification_status, c.created_at
	FROM conversations c JOIN listings l ON l.id = c.listing_id JOIN users u ON u.id = c.user_id `

func scanOwnerConversation(s rowScanner) (model.OwnerConversation, error) {
	var c model.OwnerConversation
	var verification string
	err := s.Scan(&c.ID, &c.ListingID, &c.ListingTitle, &c.StudentID, &c.Student.Name, &c.Student.School, &verification, &c.CreatedAt)
	c.Student.Verified = verification == "verified"
	return c, err
}

func messagesOf(ctx context.Context, ex db.Executor, conversationID int) ([]model.Message, error) {
	return queryList(ctx, ex, scanMessage,
		`SELECT id, conversation_id, sender, text, created_at FROM messages
		 WHERE conversation_id = ? ORDER BY created_at ASC, id ASC`, conversationID)
}

func unreadFromStudent(ctx context.Context, ex db.Executor, conversationID int) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE conversation_id = ? AND sender = 'me' AND read = ?`,
		conversationID, false).Scan(&n)
	return n, err
}

// ListConversationsForOwner is the landlord's inbox: one entry per student chat about one of their
// listings, most recently active first, each with its last message and unread count.
func ListConversationsForOwner(ctx context.Context, ex db.Executor, ownerID int) ([]model.OwnerConversation, error) {
	convos, err := queryList(ctx, ex, scanOwnerConversation,
		ownerConversationSQL+`WHERE c.kind = 'landlord' AND l.owner_id = ? ORDER BY c.id DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	for i := range convos {
		msgs, err := messagesOf(ctx, ex, convos[i].ID)
		if err != nil {
			return nil, err
		}
		if n := len(msgs); n > 0 {
			convos[i].LastMessage = &msgs[n-1]
		}
		if convos[i].Unread, err = unreadFromStudent(ctx, ex, convos[i].ID); err != nil {
			return nil, err
		}
	}
	activity := func(c model.OwnerConversation) int64 {
		if c.LastMessage != nil {
			return c.LastMessage.CreatedAt.Unix()
		}
		return c.CreatedAt.Unix()
	}
	sort.SliceStable(convos, func(i, j int) bool { return activity(convos[i]) > activity(convos[j]) })
	return convos, nil
}

// ConversationForOwner returns one chat with all its messages, only if it is about a listing
// this user posted. Unread is the count before the caller marks the chat read.
func ConversationForOwner(ctx context.Context, ex db.Executor, id, ownerID int) (*model.OwnerConversation, error) {
	c, err := queryOne(ctx, ex, scanOwnerConversation,
		ownerConversationSQL+`WHERE c.id = ? AND c.kind = 'landlord' AND l.owner_id = ?`, id, ownerID)
	if err != nil || c == nil {
		return c, err
	}
	if c.Messages, err = messagesOf(ctx, ex, c.ID); err != nil {
		return nil, err
	}
	if n := len(c.Messages); n > 0 {
		c.LastMessage = &c.Messages[n-1]
	}
	if c.Unread, err = unreadFromStudent(ctx, ex, c.ID); err != nil {
		return nil, err
	}
	return c, nil
}

// MarkStudentMessagesRead is called when the landlord opens a chat.
func MarkStudentMessagesRead(ctx context.Context, ex db.Executor, conversationID int) error {
	_, err := ex.Exec(ctx, `UPDATE messages SET read = ? WHERE conversation_id = ? AND sender = 'me' AND read = ?`,
		true, conversationID, false)
	return err
}

func CountUnreadForOwner(ctx context.Context, ex db.Executor, ownerID int) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM messages m JOIN conversations c ON c.id = m.conversation_id
		JOIN listings l ON l.id = c.listing_id
		WHERE c.kind = 'landlord' AND l.owner_id = ? AND m.sender = 'me' AND m.read = ?`, ownerID, false).Scan(&n)
	return n, err
}

// LastMessage is the newest message in a conversation, or nil if it is empty.
func LastMessage(ctx context.Context, ex db.Executor, conversationID int) (*model.Message, error) {
	return queryOne(ctx, ex, scanMessage,
		`SELECT id, conversation_id, sender, text, created_at FROM messages
		 WHERE conversation_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, conversationID)
}
