package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const referralSelect = `SELECT c.code, c.label, c.campus_slug, c.active, c.created_at, c.owner_id, u.email, u.name,
	(SELECT COUNT(*) FROM referrals r WHERE r.code = c.code),
	(SELECT COUNT(*) FROM bookings b JOIN referrals r ON r.user_id = b.user_id WHERE r.code = c.code AND b.status <> 'canceled'),
	(SELECT COUNT(*) FROM bookings b JOIN referrals r ON r.user_id = b.user_id WHERE r.code = c.code AND b.status = 'approved')
	FROM referral_codes c JOIN users u ON u.id = c.owner_id `

func scanReferral(s rowScanner) (model.ReferralCode, error) {
	var c model.ReferralCode
	err := s.Scan(&c.Code, &c.Label, &c.CampusSlug, &c.Active, &c.CreatedAt, &c.OwnerID, &c.OwnerEmail, &c.OwnerName,
		&c.Signups, &c.Requests, &c.Rentals)
	return c, err
}

func ReferralCodeByCode(ctx context.Context, ex db.Executor, code string) (*model.ReferralCode, error) {
	return queryOne(ctx, ex, scanReferral, referralSelect+`WHERE c.code = ?`, code)
}

// ListReferralCodes returns every code (ownerID 0) or one person's codes, best results first.
func ListReferralCodes(ctx context.Context, ex db.Executor, ownerID int) ([]model.ReferralCode, error) {
	q, args := referralSelect, []any{}
	if ownerID != 0 {
		q, args = q+`WHERE c.owner_id = ? `, append(args, ownerID)
	}
	return queryList(ctx, ex, scanReferral, q+`ORDER BY c.created_at DESC, c.code`, args...)
}

func SaveReferralCode(ctx context.Context, ex db.Executor, code string, ownerID int, label, campus string, active bool) error {
	res, err := ex.Exec(ctx, `UPDATE referral_codes SET owner_id = ?, label = ?, campus_slug = ?, active = ? WHERE code = ?`,
		ownerID, label, campus, active, code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = ex.Exec(ctx, `INSERT INTO referral_codes (code, owner_id, label, campus_slug, active, created_at) VALUES (?,?,?,?,?,?)`,
		code, ownerID, label, campus, active, db.Now())
	return err
}

// RecordReferral remembers which code brought a new account in. Only the first one counts.
func RecordReferral(ctx context.Context, ex db.Executor, userID int, code string) error {
	_, err := ex.Exec(ctx, `INSERT INTO referrals (user_id, code, created_at) VALUES (?,?,?)`, userID, code, db.Now())
	return err
}
