package store

import (
	"context"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// CanReviewListing is true when the person has booked the home or messaged its landlord about it.
func CanReviewListing(ctx context.Context, ex db.Executor, userID, listingID int) (bool, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM bookings WHERE user_id = ? AND listing_id = ? AND status <> 'rejected') +
		(SELECT COUNT(*) FROM conversations WHERE user_id = ? AND listing_id = ?)`,
		userID, listingID, userID, listingID).Scan(&n)
	return n > 0, err
}

func firstName(display, name string) string {
	s := strings.TrimSpace(display)
	if s == "" {
		s = strings.TrimSpace(name)
	}
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return "Someone"
}

// ListReviews returns a home's reviews, newest first. viewerID marks the viewer's own (0 for guests).
func ListReviews(ctx context.Context, ex db.Executor, listingID, viewerID, limit int) ([]model.Review, error) {
	rows, err := ex.Query(ctx, `SELECT r.id, r.listing_id, r.user_id, r.rating, r.water, r.power, r.landlord, r.body, r.created_at, u.display_name, u.name
		FROM reviews r JOIN users u ON u.id = r.user_id WHERE r.listing_id = ? ORDER BY r.created_at DESC LIMIT ?`, listingID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Review{}
	for rows.Next() {
		var r model.Review
		var display, name *string
		if err := rows.Scan(&r.ID, &r.ListingID, &r.UserID, &r.Rating, &r.Water, &r.Power, &r.Landlord, &r.Body, &r.CreatedAt, &display, &name); err != nil {
			return nil, err
		}
		d, n := "", ""
		if display != nil {
			d = *display
		}
		if name != nil {
			n = *name
		}
		r.Reviewer, r.Mine = firstName(d, n), viewerID != 0 && r.UserID == viewerID
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertReview adds or replaces the person's review and refreshes the listing's average.
func UpsertReview(ctx context.Context, ex db.Executor, listingID, userID, rating, water, power, landlord int, body string) error {
	now := db.Now()
	res, err := ex.Exec(ctx, `UPDATE reviews SET rating = ?, water = ?, power = ?, landlord = ?, body = ?, updated_at = ? WHERE listing_id = ? AND user_id = ?`,
		rating, water, power, landlord, body, now, listingID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := ex.Exec(ctx, `INSERT INTO reviews (listing_id, user_id, rating, water, power, landlord, body, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			listingID, userID, rating, water, power, landlord, body, now, now); err != nil {
			return err
		}
	}
	return refreshRating(ctx, ex, listingID)
}

// DeleteReview removes a review (by id) and refreshes the average. It returns false if none matched.
// userID 0 means "any author" (admin).
func DeleteReview(ctx context.Context, ex db.Executor, reviewID, userID int) (bool, error) {
	var listingID int
	q, args := `SELECT listing_id FROM reviews WHERE id = ?`, []any{reviewID}
	if userID != 0 {
		q, args = q+` AND user_id = ?`, append(args, userID)
	}
	if err := ex.QueryRow(ctx, q, args...).Scan(&listingID); err != nil {
		return false, nil //nolint:nilerr // no row = not found
	}
	if _, err := ex.Exec(ctx, `DELETE FROM reviews WHERE id = ?`, reviewID); err != nil {
		return false, err
	}
	return true, refreshRating(ctx, ex, listingID)
}

func refreshRating(ctx context.Context, ex db.Executor, listingID int) error {
	var n int
	var avg float64
	if err := ex.QueryRow(ctx, `SELECT COUNT(*), COALESCE(AVG(rating), 0) FROM reviews WHERE listing_id = ?`, listingID).Scan(&n, &avg); err != nil {
		return err
	}
	avg = float64(int(avg*10+0.5)) / 10
	_, err := ex.Exec(ctx, `UPDATE listings SET rating = ?, reviews = ? WHERE id = ?`, avg, n, listingID)
	return err
}

// ReviewAverages are the average water, power and landlord scores over the reviews that gave one.
type ReviewAverages struct {
	Water, Power, Landlord    float64
	WaterN, PowerN, LandlordN int
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func ReviewAveragesFor(ctx context.Context, ex db.Executor, listingID int) (ReviewAverages, error) {
	var a ReviewAverages
	err := ex.QueryRow(ctx, `SELECT
		COALESCE(AVG(CASE WHEN water > 0 THEN water END), 0), COUNT(CASE WHEN water > 0 THEN 1 END),
		COALESCE(AVG(CASE WHEN power > 0 THEN power END), 0), COUNT(CASE WHEN power > 0 THEN 1 END),
		COALESCE(AVG(CASE WHEN landlord > 0 THEN landlord END), 0), COUNT(CASE WHEN landlord > 0 THEN 1 END)
		FROM reviews WHERE listing_id = ?`, listingID).Scan(&a.Water, &a.WaterN, &a.Power, &a.PowerN, &a.Landlord, &a.LandlordN)
	a.Water, a.Power, a.Landlord = round1(a.Water), round1(a.Power), round1(a.Landlord)
	return a, err
}
