package store

import (
	"context"
	"time"

	"campus-backend/internal/db"
)

// CampusStats is how one school is doing: people who picked it as their school, how fast that's growing,
// and what they have done.
type CampusStats struct {
	Users, Users7d, UsersPrev7d, Roommates, Requests, Rentals int
}

// GrowthFor counts activity for the people whose school is schoolID. now is passed in so tests are exact.
func GrowthFor(ctx context.Context, ex db.Executor, schoolID int, now time.Time) (CampusStats, error) {
	var st CampusStats
	d7 := db.Time{Time: now.Add(-7 * 24 * time.Hour)}
	d14 := db.Time{Time: now.Add(-14 * 24 * time.Hour)}
	err := ex.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM users WHERE school_id = ?),
		(SELECT COUNT(*) FROM users WHERE school_id = ? AND created_at >= ?),
		(SELECT COUNT(*) FROM users WHERE school_id = ? AND created_at >= ? AND created_at < ?),
		(SELECT COUNT(*) FROM roommate_profiles r JOIN users u ON u.id = r.user_id WHERE u.school_id = ?),
		(SELECT COUNT(*) FROM bookings b JOIN users u ON u.id = b.user_id WHERE u.school_id = ? AND b.status <> 'canceled'),
		(SELECT COUNT(*) FROM bookings b JOIN users u ON u.id = b.user_id WHERE u.school_id = ? AND b.status = 'approved')`,
		schoolID, schoolID, d7, schoolID, d14, d7, schoolID, schoolID, schoolID).
		Scan(&st.Users, &st.Users7d, &st.UsersPrev7d, &st.Roommates, &st.Requests, &st.Rentals)
	return st, err
}

// SchoolUsers is a school and how many people picked it.
type SchoolUsers struct {
	SchoolID int    `json:"school_id"`
	Name     string `json:"name"`
	Users    int    `json:"users"`
}

// TopSchoolsWithoutCampus are the schools people pick most that have no campus page yet: where to open next.
func TopSchoolsWithoutCampus(ctx context.Context, ex db.Executor, limit int) ([]SchoolUsers, error) {
	return queryList(ctx, ex, func(s rowScanner) (SchoolUsers, error) {
		var x SchoolUsers
		err := s.Scan(&x.SchoolID, &x.Name, &x.Users)
		return x, err
	}, `SELECT s.id, s.name, COUNT(*) AS n FROM users u JOIN schools s ON s.id = u.school_id
		WHERE u.school_id NOT IN (SELECT school_id FROM campuses)
		GROUP BY s.id, s.name ORDER BY n DESC, s.name LIMIT ?`, limit)
}
