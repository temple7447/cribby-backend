package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const landlordCols = `user_id, status, phone, id_type, id_photo_public_id, note, submitted_at, reviewed_at`

func scanLandlordVerification(s rowScanner) (model.LandlordVerification, error) {
	var v model.LandlordVerification
	err := s.Scan(&v.UserID, &v.Status, &v.Phone, &v.IDType, &v.IDPhotoPublicID, &v.Note, &v.SubmittedAt, &v.ReviewedAt)
	return v, err
}

// LandlordVerificationFor returns the person's check, or nil if they never sent one.
func LandlordVerificationFor(ctx context.Context, ex db.Executor, userID int) (*model.LandlordVerification, error) {
	return queryOne(ctx, ex, scanLandlordVerification,
		`SELECT `+landlordCols+` FROM landlord_verifications WHERE user_id = ?`, userID)
}

// SubmitLandlordVerification records (or replaces) a pending check.
func SubmitLandlordVerification(ctx context.Context, ex db.Executor, userID int, phone, idType, photoID string) error {
	now := db.Now()
	res, err := ex.Exec(ctx, `UPDATE landlord_verifications SET status = 'pending', phone = ?, id_type = ?,
		id_photo_public_id = ?, note = '', submitted_at = ?, reviewed_at = NULL WHERE user_id = ?`,
		phone, idType, photoID, now, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO landlord_verifications (user_id, status, phone, id_type, id_photo_public_id, note, submitted_at)
		VALUES (?, 'pending', ?, ?, ?, '', ?)`, userID, phone, idType, photoID, now)
	return err
}

// ListLandlordVerifications returns checks in one status, oldest first (the queue order).
func ListLandlordVerifications(ctx context.Context, ex db.Executor, status string, limit int) ([]model.LandlordVerification, error) {
	return queryList(ctx, ex, scanLandlordVerification,
		`SELECT `+landlordCols+` FROM landlord_verifications WHERE status = ? ORDER BY submitted_at ASC LIMIT ?`, status, limit)
}

// DecideLandlordVerification approves or rejects a check and keeps the badge on all of the person's
// listings in step with it.
func DecideLandlordVerification(ctx context.Context, ex db.Executor, userID int, approve bool, note string) error {
	status := "rejected"
	if approve {
		status = "verified"
	}
	if _, err := ex.Exec(ctx, `UPDATE landlord_verifications SET status = ?, note = ?, reviewed_at = ? WHERE user_id = ?`,
		status, note, db.Now(), userID); err != nil {
		return err
	}
	return SetOwnerListingsVerified(ctx, ex, userID, approve)
}

// SetOwnerListingsVerified shows or hides the "verified landlord" badge on everything a person posted.
func SetOwnerListingsVerified(ctx context.Context, ex db.Executor, ownerID int, verified bool) error {
	_, err := ex.Exec(ctx, `UPDATE listings SET landlord_verified = ? WHERE owner_id = ?`, verified, ownerID)
	return err
}

// IsLandlordVerified reports whether a person has passed the check.
func IsLandlordVerified(ctx context.Context, ex db.Executor, userID int) (bool, error) {
	v, err := LandlordVerificationFor(ctx, ex, userID)
	return v != nil && v.Status == "verified", err
}
