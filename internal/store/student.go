package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const studentCols = `user_id, status, school, id_photo_public_id, note, submitted_at, reviewed_at`

func scanStudentVerification(s rowScanner) (model.StudentVerification, error) {
	var v model.StudentVerification
	err := s.Scan(&v.UserID, &v.Status, &v.School, &v.IDPhotoPublicID, &v.Note, &v.SubmittedAt, &v.ReviewedAt)
	return v, err
}

// StudentVerificationFor returns the person's check, or nil if they never sent one.
func StudentVerificationFor(ctx context.Context, ex db.Executor, userID int) (*model.StudentVerification, error) {
	return queryOne(ctx, ex, scanStudentVerification,
		`SELECT `+studentCols+` FROM student_verifications WHERE user_id = ?`, userID)
}

// SubmitStudentVerification records (or replaces) a pending check.
func SubmitStudentVerification(ctx context.Context, ex db.Executor, userID int, school, photoID string) error {
	now := db.Now()
	res, err := ex.Exec(ctx, `UPDATE student_verifications SET status = 'pending', school = ?, id_photo_public_id = ?,
		note = '', submitted_at = ?, reviewed_at = NULL WHERE user_id = ?`, school, photoID, now, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO student_verifications (user_id, status, school, id_photo_public_id, note, submitted_at)
		VALUES (?, 'pending', ?, ?, '', ?)`, userID, school, photoID, now)
	return err
}

// ListStudentVerifications returns checks in one status, oldest first.
func ListStudentVerifications(ctx context.Context, ex db.Executor, status string, limit int) ([]model.StudentVerification, error) {
	return queryList(ctx, ex, scanStudentVerification,
		`SELECT `+studentCols+` FROM student_verifications WHERE status = ? ORDER BY submitted_at ASC LIMIT ?`, status, limit)
}

// DecideStudentVerification approves or rejects a check and forgets the photo reference.
func DecideStudentVerification(ctx context.Context, ex db.Executor, userID int, approve bool, note string) error {
	status := "rejected"
	if approve {
		status = "verified"
	}
	_, err := ex.Exec(ctx, `UPDATE student_verifications SET status = ?, note = ?, reviewed_at = ?, id_photo_public_id = '' WHERE user_id = ?`,
		status, note, db.Now(), userID)
	return err
}
