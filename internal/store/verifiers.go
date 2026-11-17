package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

func IsVerifier(ctx context.Context, ex db.Executor, userID int) (bool, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM verifiers WHERE user_id = ?`, userID).Scan(&n)
	return n > 0, err
}

func SetVerifier(ctx context.Context, ex db.Executor, userID int, kind, campus string) error {
	res, err := ex.Exec(ctx, `UPDATE verifiers SET kind = ?, campus = ? WHERE user_id = ?`, kind, campus, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO verifiers (user_id, kind, campus, verified_count, created_at) VALUES (?,?,?,0,?)`,
		userID, kind, campus, db.Now())
	return err
}

func RemoveVerifier(ctx context.Context, ex db.Executor, userID int) error {
	_, err := ex.Exec(ctx, `DELETE FROM verifiers WHERE user_id = ?`, userID)
	return err
}

// CreditVerifier adds one to the person's count of homes they have checked.
func CreditVerifier(ctx context.Context, ex db.Executor, userID int) error {
	_, err := ex.Exec(ctx, `UPDATE verifiers SET verified_count = verified_count + 1 WHERE user_id = ?`, userID)
	return err
}

func ListVerifiers(ctx context.Context, ex db.Executor) ([]model.Verifier, error) {
	return queryList(ctx, ex, func(s rowScanner) (model.Verifier, error) {
		var v model.Verifier
		err := s.Scan(&v.UserID, &v.Kind, &v.Campus, &v.VerifiedCount, &v.CreatedAt, &v.Email, &v.Name)
		return v, err
	}, `SELECT v.user_id, v.kind, v.campus, v.verified_count, v.created_at, u.email, u.name
		FROM verifiers v JOIN users u ON u.id = v.user_id ORDER BY v.verified_count DESC, v.user_id`)
}

// UnverifiedLiveListings is the verifiers' to-do list: live homes nobody has checked yet, oldest first,
// not counting the verifier's own.
func UnverifiedLiveListings(ctx context.Context, ex db.Executor, excludeOwner, limit int) ([]model.Listing, error) {
	return queryList(ctx, ex, scanListing, `SELECT `+listingCols+` FROM listings
		WHERE status = 'approved' AND verified = ? AND is_hidden = ? AND (owner_id IS NULL OR owner_id <> ?)
		ORDER BY created_at ASC LIMIT ?`, false, false, excludeOwner, limit)
}

// VerifierFor returns the person's verifier record, or nil if they aren't one.
func VerifierFor(ctx context.Context, ex db.Executor, userID int) (*model.Verifier, error) {
	return queryOne(ctx, ex, func(s rowScanner) (model.Verifier, error) {
		var v model.Verifier
		err := s.Scan(&v.UserID, &v.Kind, &v.Campus, &v.VerifiedCount, &v.CreatedAt, &v.Email, &v.Name)
		return v, err
	}, `SELECT v.user_id, v.kind, v.campus, v.verified_count, v.created_at, u.email, u.name
		FROM verifiers v JOIN users u ON u.id = v.user_id WHERE v.user_id = ?`, userID)
}
