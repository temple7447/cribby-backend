package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// ---- favorites ----

func ListFavoriteListings(ctx context.Context, ex db.Executor, userID int) ([]model.Listing, error) {
	return queryList(ctx, ex, scanListing,
		`SELECT `+prefix("l", listingCols)+` FROM favorites f JOIN listings l ON l.id = f.listing_id
		 WHERE f.user_id = ? AND l.status = 'approved' ORDER BY f.id`, userID)
}

// AddFavorite is idempotent: favouriting twice is a no-op.
func AddFavorite(ctx context.Context, ex db.Executor, userID, listingID int) error {
	var n int
	if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM favorites WHERE user_id = ? AND listing_id = ?`, userID, listingID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := ex.Exec(ctx, `INSERT INTO favorites (user_id, listing_id) VALUES (?, ?)`, userID, listingID)
	if db.IsUniqueViolation(err) { // lost a race with a concurrent identical request
		return nil
	}
	return err
}

func RemoveFavorite(ctx context.Context, ex db.Executor, userID, listingID int) error {
	_, err := ex.Exec(ctx, `DELETE FROM favorites WHERE user_id = ? AND listing_id = ?`, userID, listingID)
	return err
}

// ---- blocks ----

const blockCols = `b.id, b.blocker_id, b.blocked_id, b.created_at, u.id, u.name, u.avatar_emoji, u.avatar_url`
const blockFrom = ` FROM blocks b JOIN users u ON u.id = b.blocked_id `

func scanBlock(s rowScanner) (model.Block, error) {
	var b model.Block
	err := s.Scan(&b.ID, &b.BlockerID, &b.BlockedID, &b.CreatedAt,
		&b.BlockedUser.ID, &b.BlockedUser.Name, &b.BlockedUser.AvatarEmoji, &b.BlockedUser.AvatarURL)
	return b, err
}

func BlockPair(ctx context.Context, ex db.Executor, blockerID, blockedID int) (*model.Block, error) {
	return queryOne(ctx, ex, scanBlock,
		`SELECT `+blockCols+blockFrom+`WHERE b.blocker_id = ? AND b.blocked_id = ?`, blockerID, blockedID)
}

func ListBlocksFor(ctx context.Context, ex db.Executor, blockerID int) ([]model.Block, error) {
	return queryList(ctx, ex, scanBlock,
		`SELECT `+blockCols+blockFrom+`WHERE b.blocker_id = ? ORDER BY b.created_at DESC, b.id DESC`, blockerID)
}

// BlockExistsEitherDirection is true if either user has blocked the other.
func BlockExistsEitherDirection(ctx context.Context, ex db.Executor, a, b int) (bool, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM blocks
		WHERE (blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)`, a, b, b, a).Scan(&n)
	return n > 0, err
}

func CreateBlock(ctx context.Context, ex db.Executor, blockerID, blockedID int) (*model.Block, error) {
	if _, err := ex.InsertID(ctx, `INSERT INTO blocks (blocker_id, blocked_id) VALUES (?, ?)`, blockerID, blockedID); err != nil {
		if db.IsUniqueViolation(err) {
			return BlockPair(ctx, ex, blockerID, blockedID)
		}
		return nil, err
	}
	return BlockPair(ctx, ex, blockerID, blockedID)
}

func DeleteBlock(ctx context.Context, ex db.Executor, blockerID, blockedID int) error {
	_, err := ex.Exec(ctx, `DELETE FROM blocks WHERE blocker_id = ? AND blocked_id = ?`, blockerID, blockedID)
	return err
}

// ---- reports ----

const reportCols = `id, reporter_id, target_type, target_id, reason, details, status, created_at, resolved_at, resolved_by_id`

func scanReport(s rowScanner) (model.Report, error) {
	var r model.Report
	err := s.Scan(&r.ID, &r.ReporterID, &r.TargetType, &r.TargetID, &r.Reason, &r.Details, &r.Status,
		&r.CreatedAt, &r.ResolvedAt, &r.ResolvedByID)
	return r, err
}

func ReportByID(ctx context.Context, ex db.Executor, id int) (*model.Report, error) {
	return queryOne(ctx, ex, scanReport, `SELECT `+reportCols+` FROM reports WHERE id = ?`, id)
}

func CreateReport(ctx context.Context, ex db.Executor, reporterID int, targetType string, targetID int, reason string, details *string) (*model.Report, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO reports (reporter_id, target_type, target_id, reason, details, status)
		VALUES (?,?,?,?,?,?)`, reporterID, targetType, targetID, reason, details, "pending")
	if err != nil {
		return nil, err
	}
	return ReportByID(ctx, ex, id)
}

func ListReports(ctx context.Context, ex db.Executor, status *string) ([]model.Report, error) {
	q := `SELECT ` + reportCols + ` FROM reports`
	var args []any
	if status != nil {
		q += ` WHERE status = ?`
		args = append(args, *status)
	}
	return queryList(ctx, ex, scanReport, q+` ORDER BY created_at DESC, id DESC`, args...)
}

func ListReportsFiledBy(ctx context.Context, ex db.Executor, reporterID int) ([]model.Report, error) {
	return queryList(ctx, ex, scanReport,
		`SELECT `+reportCols+` FROM reports WHERE reporter_id = ? ORDER BY created_at DESC, id DESC`, reporterID)
}

func ListReportsAgainstUser(ctx context.Context, ex db.Executor, userID int) ([]model.Report, error) {
	return queryList(ctx, ex, scanReport,
		`SELECT `+reportCols+` FROM reports WHERE target_type = 'user' AND target_id = ? ORDER BY created_at DESC, id DESC`, userID)
}

func UpdateReportStatus(ctx context.Context, ex db.Executor, id int, status string, resolvedAt *db.Time, resolvedByID *int) error {
	if resolvedAt == nil {
		_, err := ex.Exec(ctx, `UPDATE reports SET status = ? WHERE id = ?`, status, id)
		return err
	}
	_, err := ex.Exec(ctx, `UPDATE reports SET status = ?, resolved_at = ?, resolved_by_id = ? WHERE id = ?`,
		status, *resolvedAt, resolvedByID, id)
	return err
}

// EnrichReport fills reporter_email and a human-readable target_label so a
// moderator UI can render a report without N follow-up lookups per row.
// Best-effort — a target/reporter that no longer exists just leaves the label
// blank rather than failing.
func EnrichReport(ctx context.Context, ex db.Executor, r model.Report) (model.Report, error) {
	if u, err := UserByID(ctx, ex, r.ReporterID); err != nil {
		return r, err
	} else if u != nil {
		r.ReporterEmail = &u.Email
	}
	switch r.TargetType {
	case "listing":
		if t, err := ListingByID(ctx, ex, r.TargetID); err != nil {
			return r, err
		} else if t != nil {
			r.TargetLabel = &t.Title
		}
	case "roommate_profile":
		if t, err := RoommateByID(ctx, ex, r.TargetID); err != nil {
			return r, err
		} else if t != nil {
			r.TargetLabel = &t.Name
		}
	default: // "user"
		if t, err := UserByID(ctx, ex, r.TargetID); err != nil {
			return r, err
		} else if t != nil {
			r.TargetLabel = &t.Email
		}
	}
	return r, nil
}
