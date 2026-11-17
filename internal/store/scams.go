package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const scamCols = `id, user_id, kind, suspect_name, suspect_phone, area, details, status, review_note, created_at, reviewed_at`

func scanScam(s rowScanner) (model.ScamAlert, error) {
	var a model.ScamAlert
	err := s.Scan(&a.ID, &a.UserID, &a.Kind, &a.SuspectName, &a.SuspectPhone, &a.Area, &a.Details, &a.Status, &a.ReviewNote, &a.CreatedAt, &a.ReviewedAt)
	return a, err
}

func CreateScamAlert(ctx context.Context, ex db.Executor, a model.ScamAlert) (int, error) {
	return ex.InsertID(ctx, `INSERT INTO scam_alerts (user_id, kind, suspect_name, suspect_phone, area, details, status, review_note, created_at)
		VALUES (?,?,?,?,?,?,'pending','',?)`, a.UserID, a.Kind, a.SuspectName, a.SuspectPhone, a.Area, a.Details, db.Now())
}

func ScamAlertByID(ctx context.Context, ex db.Executor, id int) (*model.ScamAlert, error) {
	return queryOne(ctx, ex, scanScam, `SELECT `+scamCols+` FROM scam_alerts WHERE id = ?`, id)
}

// ListScamAlerts returns alerts in one status, newest first. phone narrows to one exact number ("" = all).
func ListScamAlerts(ctx context.Context, ex db.Executor, status, phone string, limit int) ([]model.ScamAlert, error) {
	q, args := `SELECT `+scamCols+` FROM scam_alerts WHERE status = ?`, []any{status}
	if phone != "" {
		q, args = q+` AND suspect_phone = ?`, append(args, phone)
	}
	return queryList(ctx, ex, scanScam, q+` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit)...)
}

func ListScamAlertsByUser(ctx context.Context, ex db.Executor, userID, limit int) ([]model.ScamAlert, error) {
	return queryList(ctx, ex, scanScam, `SELECT `+scamCols+` FROM scam_alerts WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
}

func CountRecentScamAlerts(ctx context.Context, ex db.Executor, userID int, since db.Time) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM scam_alerts WHERE user_id = ? AND created_at >= ?`, userID, since).Scan(&n)
	return n, err
}

func DecideScamAlert(ctx context.Context, ex db.Executor, id int, approve bool, note string) error {
	status := "rejected"
	if approve {
		status = "approved"
	}
	_, err := ex.Exec(ctx, `UPDATE scam_alerts SET status = ?, review_note = ?, reviewed_at = ? WHERE id = ?`, status, note, db.Now(), id)
	return err
}
