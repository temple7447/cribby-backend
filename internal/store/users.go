package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const userCols = `id, email, hashed_password, name, school, avatar_emoji, avatar_url, avatar_public_id,
	verification_status, verification_code_hash, verification_code_expires_at, verification_code_last_sent_at,
	password_reset_code_hash, password_reset_code_expires_at, password_reset_code_last_sent_at,
	is_admin, is_suspended, created_at, school_id`

func scanUser(s rowScanner) (model.User, error) {
	var u model.User
	err := s.Scan(&u.ID, &u.Email, &u.HashedPassword, &u.Name, &u.School, &u.AvatarEmoji, &u.AvatarURL, &u.AvatarPublicID,
		&u.VerificationStatus, &u.VerificationCodeHash, &u.VerificationCodeExpiresAt, &u.VerificationCodeLastSentAt,
		&u.PasswordResetCodeHash, &u.PasswordResetExpiresAt, &u.PasswordResetLastSentAt,
		&u.IsAdmin, &u.IsSuspended, &u.CreatedAt, &u.SchoolID)
	return u, err
}

func UserByID(ctx context.Context, ex db.Executor, id int) (*model.User, error) {
	return queryOne(ctx, ex, scanUser, `SELECT `+userCols+` FROM users WHERE id = ?`, id)
}

func UserByEmail(ctx context.Context, ex db.Executor, email string) (*model.User, error) {
	return queryOne(ctx, ex, scanUser, `SELECT `+userCols+` FROM users WHERE email = ?`, email)
}

func ListUsers(ctx context.Context, ex db.Executor) ([]model.User, error) {
	return queryList(ctx, ex, scanUser, `SELECT `+userCols+` FROM users ORDER BY created_at DESC, id DESC`)
}

func ListAdminEmails(ctx context.Context, ex db.Executor) ([]string, error) {
	return queryList(ctx, ex, func(s rowScanner) (string, error) {
		var e string
		return e, s.Scan(&e)
	}, `SELECT email FROM users WHERE is_admin = ? ORDER BY id`, true)
}

func CreateUser(ctx context.Context, ex db.Executor, email, hashedPassword, name, school string, schoolID *int) (*model.User, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO users (email, hashed_password, name, school, school_id, avatar_emoji, verification_status, is_admin, is_suspended)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, email, hashedPassword, name, school, schoolID, "🎓", "unverified", false, false)
	if err != nil {
		return nil, err
	}
	return UserByID(ctx, ex, id)
}

// UpdateUserProfile applies only the fields that are non-nil.
func UpdateUserProfile(ctx context.Context, ex db.Executor, id int, name, school, avatarEmoji *string) error {
	_, err := ex.Exec(ctx, `UPDATE users SET
		name = COALESCE(?, name), school = COALESCE(?, school), avatar_emoji = COALESCE(?, avatar_emoji)
		WHERE id = ?`, name, school, avatarEmoji, id)
	return err
}

// SetUserSchool records the chosen school and keeps the free-text `school`
// name (shown across the app) in step with it.
func SetUserSchool(ctx context.Context, ex db.Executor, id int, s *model.School) error {
	_, err := ex.Exec(ctx, `UPDATE users SET school_id = ?, school = ? WHERE id = ?`, s.ID, s.Name, id)
	return err
}

func SetVerificationCode(ctx context.Context, ex db.Executor, id int, hash string, expires, sent db.Time) error {
	_, err := ex.Exec(ctx, `UPDATE users SET verification_code_hash = ?, verification_code_expires_at = ?,
		verification_code_last_sent_at = ? WHERE id = ?`, hash, expires, sent, id)
	return err
}

func MarkVerified(ctx context.Context, ex db.Executor, id int) error {
	_, err := ex.Exec(ctx, `UPDATE users SET verification_status = 'verified', verification_code_hash = NULL,
		verification_code_expires_at = NULL, verification_code_last_sent_at = NULL WHERE id = ?`, id)
	return err
}

func SetPasswordResetCode(ctx context.Context, ex db.Executor, id int, hash string, expires, sent db.Time) error {
	_, err := ex.Exec(ctx, `UPDATE users SET password_reset_code_hash = ?, password_reset_code_expires_at = ?,
		password_reset_code_last_sent_at = ? WHERE id = ?`, hash, expires, sent, id)
	return err
}

func ResetPassword(ctx context.Context, ex db.Executor, id int, newHash string) error {
	_, err := ex.Exec(ctx, `UPDATE users SET hashed_password = ?, password_reset_code_hash = NULL,
		password_reset_code_expires_at = NULL, password_reset_code_last_sent_at = NULL WHERE id = ?`, newHash, id)
	return err
}

func SetAvatar(ctx context.Context, ex db.Executor, id int, url, publicID *string) error {
	_, err := ex.Exec(ctx, `UPDATE users SET avatar_url = ?, avatar_public_id = ? WHERE id = ?`, url, publicID, id)
	return err
}

func SetAdmin(ctx context.Context, ex db.Executor, id int, v bool) error {
	_, err := ex.Exec(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, v, id)
	return err
}

func SetSuspended(ctx context.Context, ex db.Executor, id int, v bool) error {
	_, err := ex.Exec(ctx, `UPDATE users SET is_suspended = ? WHERE id = ?`, v, id)
	return err
}

// DeleteUserCascade permanently deletes the account and everything it owns.
// No ON DELETE CASCADE exists on these foreign keys (SQLite doesn't enforce
// them locally; Postgres does in production), so cleanup is explicit and
// ordered. Run it inside a transaction.
func DeleteUserCascade(ctx context.Context, tx db.Executor, userID int) error {
	steps := []struct {
		q    string
		args []any
	}{
		// Conversations this user owns, plus their messages.
		{`DELETE FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE user_id = ?)`, []any{userID}},
		{`DELETE FROM conversations WHERE user_id = ?`, []any{userID}},
		// Other users' conversations may point at this user's roommate profile —
		// unlink rather than delete, so their thread stays.
		{`UPDATE conversations SET roommate_profile_id = NULL
			WHERE roommate_profile_id IN (SELECT id FROM roommate_profiles WHERE user_id = ?)`, []any{userID}},
		{`DELETE FROM roommate_profiles WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM bookings WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM favorites WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM blocks WHERE blocker_id = ? OR blocked_id = ?`, []any{userID, userID}},
		// Preserve moderation history: keep reports this user resolved (as an
		// admin) but detach the reference; delete reports they filed.
		{`UPDATE reports SET resolved_by_id = NULL WHERE resolved_by_id = ?`, []any{userID}},
		{`DELETE FROM reports WHERE reporter_id = ?`, []any{userID}},
		{`DELETE FROM users WHERE id = ?`, []any{userID}},
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.q, s.args...); err != nil {
			return err
		}
	}
	return nil
}
