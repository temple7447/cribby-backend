package store

import (
	"context"
	"strconv"
	"time"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const userCols = `id, email, hashed_password, name, school, avatar_emoji, avatar_url, avatar_public_id,
	verification_status, verification_code_hash, verification_code_expires_at, verification_code_last_sent_at,
	password_reset_code_hash, password_reset_code_expires_at, password_reset_code_last_sent_at,
	is_admin, is_suspended, created_at, school_id, last_login_at, last_active_at, display_name`

// scanUser reads a row of userCols; the targets live in userTargets so the two can't drift apart.
func scanUser(s rowScanner) (model.User, error) {
	var u model.User
	err := s.Scan(userTargets(&u)...)
	return u, err
}

func UserByID(ctx context.Context, ex db.Executor, id int) (*model.User, error) {
	return queryOne(ctx, ex, scanUser, `SELECT `+userCols+` FROM users WHERE id = ?`, id)
}

// UserByEmail matches ignoring case, so accounts stored as "Ada@gmail.com" still work when a client
// sends the lowercased address. An exact match always wins, so two addresses that differ only by
// case (possible in old data) can never be confused with each other.
func UserByEmail(ctx context.Context, ex db.Executor, email string) (*model.User, error) {
	return queryOne(ctx, ex, scanUser, `SELECT `+userCols+` FROM users WHERE LOWER(email) = LOWER(?)
		ORDER BY CASE WHEN email = ? THEN 0 ELSE 1 END, id LIMIT 1`, email, email)
}

// UserByDisplayName finds who holds a display name (case-insensitive), or nil.
func UserByDisplayName(ctx context.Context, ex db.Executor, displayName string) (*model.User, error) {
	return queryOne(ctx, ex, scanUser, `SELECT `+userCols+` FROM users WHERE display_name <> '' AND LOWER(display_name) = LOWER(?)`, displayName)
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

func CreateUser(ctx context.Context, ex db.Executor, email, hashedPassword, name, displayName, school string, schoolID *int) (*model.User, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO users (email, hashed_password, name, display_name, school, school_id, avatar_emoji, verification_status, is_admin, is_suspended)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, email, hashedPassword, name, displayName, school, schoolID, "🎓", "unverified", false, false)
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

// MoneyInFlight reports whether the person has a payment that is not settled: money being held for them, or a
// payout Cribby still has to send. They can't delete their account until it is.
func MoneyInFlight(ctx context.Context, ex db.Executor, userID int) (bool, error) {
	var n int
	for _, q := range []string{
		`SELECT COUNT(*) FROM orders WHERE (buyer_id = ? OR seller_id = ?) AND (status IN ('awaiting_payment','paid','handed_over','disputed')
			OR (status = 'completed' AND payout_status = 'queued'))`,
		`SELECT COUNT(*) FROM deposits WHERE (tenant_id = ? OR landlord_id = ?) AND (status IN ('awaiting_payment','paid','disputed')
			OR (status = 'completed' AND payout_status = 'queued'))`,
	} {
		var c int
		if err := ex.QueryRow(ctx, q, userID, userID).Scan(&c); err != nil {
			return false, err
		}
		n += c
	}
	return n > 0, nil
}

// HasPaymentRecords: orders and deposits are Cribby's accounts, so people who have any are anonymised, not erased.
func hasPaymentRecords(ctx context.Context, ex db.Executor, userID int) (bool, error) {
	var a, b int
	if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE buyer_id = ? OR seller_id = ?`, userID, userID).Scan(&a); err != nil {
		return false, err
	}
	if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE tenant_id = ? OR landlord_id = ?`, userID, userID).Scan(&b); err != nil {
		return false, err
	}
	return a+b > 0, nil
}

// DeleteUserCascade removes the account and everything it owns. Their homes are taken down, their vendor pages
// are kept without an owner. A person with payment records stays behind only as an anonymous row (no email, name
// or password), because those records have to be kept. Run it inside a transaction.
func DeleteUserCascade(ctx context.Context, tx db.Executor, userID int) error {
	records, err := hasPaymentRecords(ctx, tx, userID)
	if err != nil {
		return err
	}
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
		// Bookings, except the ones a payment record points at.
		{`DELETE FROM bookings WHERE user_id = ? AND id NOT IN (SELECT booking_id FROM deposits)`, []any{userID}},
		{`DELETE FROM favorites WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM blocks WHERE blocker_id = ? OR blocked_id = ?`, []any{userID, userID}},
		// Preserve moderation history: keep reports this user resolved (as an
		// admin) but detach the reference; delete reports they filed.
		{`UPDATE reports SET resolved_by_id = NULL WHERE resolved_by_id = ?`, []any{userID}},
		{`DELETE FROM reports WHERE reporter_id = ?`, []any{userID}},
		// Their homes come down; vendor pages stay without an owner.
		{`UPDATE listings SET status = 'removed', owner_id = NULL WHERE owner_id = ?`, []any{userID}},
		{`UPDATE vendors SET owner_id = NULL WHERE owner_id = ?`, []any{userID}},
		{`DELETE FROM reviews WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM verifiers WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM verifier_credits WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM referrals WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM referral_codes WHERE owner_id = ?`, []any{userID}},
		{`DELETE FROM scam_alerts WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM student_verifications WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM landlord_verifications WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM payout_accounts WHERE user_id = ?`, []any{userID}},
	}
	if records {
		// Items that orders point at stay (taken down, phone number wiped); the rest go.
		steps = append(steps,
			struct {
				q    string
				args []any
			}{`UPDATE market_items SET status = 'removed', contact_phone = '' WHERE user_id = ?`, []any{userID}},
			struct {
				q    string
				args []any
			}{`UPDATE users SET email = ?, name = 'Deleted user', display_name = '', school = '', school_id = NULL, hashed_password = '!deleted',
				avatar_url = NULL, avatar_public_id = NULL, is_suspended = true, is_admin = false, verification_code_hash = NULL,
				password_reset_code_hash = NULL WHERE id = ?`, []any{"deleted-" + strconv.Itoa(userID) + "@deleted.invalid", userID}})
	} else {
		steps = append(steps,
			struct {
				q    string
				args []any
			}{`DELETE FROM market_items WHERE user_id = ?`, []any{userID}},
			struct {
				q    string
				args []any
			}{`DELETE FROM users WHERE id = ?`, []any{userID}})
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.q, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// TouchLogin records a sign-in (login, signup, or completing a password reset).
func TouchLogin(ctx context.Context, ex db.Executor, id int) error {
	now := db.Now()
	_, err := ex.Exec(ctx, `UPDATE users SET last_login_at = ?, last_active_at = ? WHERE id = ?`, now, now, id)
	return err
}

// ActiveRefreshInterval is how often "last active" is rewritten for a busy user — frequent
// enough to be useful, rare enough that it adds almost no database load.
const ActiveRefreshInterval = 10 * time.Minute

// TouchActive notes that the user just used the app, if we haven't recorded that recently.
func TouchActive(ctx context.Context, ex db.Executor, u *model.User) error {
	if u.LastActiveAt != nil && time.Since(u.LastActiveAt.Time) < ActiveRefreshInterval {
		return nil
	}
	_, err := ex.Exec(ctx, `UPDATE users SET last_active_at = ? WHERE id = ?`, db.Now(), u.ID)
	return err
}
