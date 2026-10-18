package store

import (
	"context"
	"fmt"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// counterpart_user_id is only meaningful for roommate-kind conversations: the
// other participant's user id, when there is a real User on the other end
// (landlords aren't User-backed accounts yet).
const conversationCols = `c.id, c.user_id, c.kind, c.listing_id, c.roommate_profile_id,
	CASE WHEN c.kind = 'roommate' THEN rp.user_id END, c.title, c.subtitle, c.emoji, c.grad, c.created_at`

const conversationFrom = ` FROM conversations c LEFT JOIN roommate_profiles rp ON rp.id = c.roommate_profile_id `

func scanConversation(s rowScanner) (model.Conversation, error) {
	var c model.Conversation
	err := s.Scan(&c.ID, &c.UserID, &c.Kind, &c.ListingID, &c.RoommateProfileID, &c.CounterpartUserID,
		&c.Title, &c.Subtitle, &c.Emoji, &c.Grad, &c.CreatedAt)
	c.Messages = []model.Message{}
	return c, err
}

func scanMessage(s rowScanner) (model.Message, error) {
	var m model.Message
	err := s.Scan(&m.ID, &m.ConversationID, &m.Sender, &m.Text, &m.CreatedAt)
	return m, err
}

func loadMessages(ctx context.Context, ex db.Executor, convos []model.Conversation) error {
	for i := range convos {
		msgs, err := queryList(ctx, ex, scanMessage,
			`SELECT id, conversation_id, sender, text, created_at FROM messages
			 WHERE conversation_id = ? ORDER BY created_at ASC, id ASC`, convos[i].ID)
		if err != nil {
			return err
		}
		convos[i].Messages = msgs
	}
	return nil
}

func ListConversationsForUser(ctx context.Context, ex db.Executor, userID int) ([]model.Conversation, error) {
	convos, err := queryList(ctx, ex, scanConversation,
		`SELECT `+conversationCols+conversationFrom+`WHERE c.user_id = ? ORDER BY c.created_at DESC, c.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return convos, loadMessages(ctx, ex, convos)
}

func ConversationForUser(ctx context.Context, ex db.Executor, id, userID int) (*model.Conversation, error) {
	c, err := queryOne(ctx, ex, scanConversation,
		`SELECT `+conversationCols+conversationFrom+`WHERE c.id = ? AND c.user_id = ?`, id, userID)
	if err != nil || c == nil {
		return c, err
	}
	one := []model.Conversation{*c}
	if err := loadMessages(ctx, ex, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

func AddMessage(ctx context.Context, ex db.Executor, conversationID int, sender, text string) (*model.Message, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO messages (conversation_id, sender, text, read) VALUES (?,?,?,?)`,
		conversationID, sender, text, true)
	if err != nil {
		return nil, err
	}
	return queryOne(ctx, ex, scanMessage,
		`SELECT id, conversation_id, sender, text, created_at FROM messages WHERE id = ?`, id)
}

// FindOrCreateConversation returns the user's existing conversation for this
// (kind, listing, roommate profile) tuple, or creates one titled from the
// thing being discussed.
func FindOrCreateConversation(ctx context.Context, ex db.Executor, userID int, kind string, listingID, roommateProfileID *int) (*model.Conversation, error) {
	// A NULL id must be matched with IS NULL, not "= NULL".
	cond := func(col string, v *int) (string, []any) {
		if v == nil {
			return col + " IS NULL", nil
		}
		return col + " = ?", []any{*v}
	}
	lc, la := cond("c.listing_id", listingID)
	rc, ra := cond("c.roommate_profile_id", roommateProfileID)
	args := append(append([]any{userID, kind}, la...), ra...)
	existing, err := queryOne(ctx, ex, scanConversation,
		`SELECT `+conversationCols+conversationFrom+
			fmt.Sprintf(`WHERE c.user_id = ? AND c.kind = ? AND %s AND %s ORDER BY c.id LIMIT 1`, lc, rc), args...)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		one := []model.Conversation{*existing}
		if err := loadMessages(ctx, ex, one); err != nil {
			return nil, err
		}
		return &one[0], nil
	}

	title, subtitle, emoji, grad := "New chat", "", "💬", "from-violet to-mint"
	switch {
	case kind == "landlord" && listingID != nil:
		if l, err := ListingByID(ctx, ex, *listingID); err != nil {
			return nil, err
		} else if l != nil {
			title, subtitle, emoji, grad = l.LandlordName, l.Title, "👩‍💼", l.Grad
		}
	case kind == "roommate" && roommateProfileID != nil:
		if r, err := RoommateByID(ctx, ex, *roommateProfileID); err != nil {
			return nil, err
		} else if r != nil {
			title, subtitle, emoji, grad = r.Name, r.School+" · "+r.Year, r.Emoji, r.Grad
		}
	}

	id, err := ex.InsertID(ctx, `INSERT INTO conversations (user_id, kind, listing_id, roommate_profile_id, title, subtitle, emoji, grad)
		VALUES (?,?,?,?,?,?,?,?)`, userID, kind, listingID, roommateProfileID, title, subtitle, emoji, grad)
	if err != nil {
		return nil, err
	}
	return ConversationForUser(ctx, ex, id, userID)
}
