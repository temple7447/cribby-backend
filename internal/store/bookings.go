package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

const bookingCols = `id, user_id, listing_id, move_in_date, lease_months, status, guarantor_name, guarantor_email,
	guarantor_status, message_to_landlord, request_sent, landlord_approved, guarantor_signed, lease_signed,
	deposit_paid, keys_collected, created_at`

func bookingTargets(b *model.Booking) []any {
	return []any{&b.ID, &b.UserID, &b.ListingID, &b.MoveInDate, &b.LeaseMonths, &b.Status, &b.GuarantorName,
		&b.GuarantorEmail, &b.GuarantorStatus, &b.MessageToLandlord, &b.RequestSent, &b.LandlordApproved,
		&b.GuarantorSigned, &b.LeaseSigned, &b.DepositPaid, &b.KeysCollected, &b.CreatedAt}
}

func listingTargets(l *model.Listing) []any {
	return []any{&l.ID, &l.Title, &l.Type, &l.PricePer, &l.Total, &l.UtilitiesMonthly, &l.Deposit, &l.Walk,
		&l.Latitude, &l.Longitude, &l.Rating, &l.Reviews, &l.Verified, &l.Hot, &l.Video, &l.IsHidden,
		&l.Grad, &l.Emoji, &l.Amenities, &l.Photos, &l.LandlordName, &l.LandlordVerified,
		&l.LandlordResponseRate, &l.CreatedAt}
}

func userTargets(u *model.User) []any {
	return []any{&u.ID, &u.Email, &u.HashedPassword, &u.Name, &u.School, &u.AvatarEmoji, &u.AvatarURL, &u.AvatarPublicID,
		&u.VerificationStatus, &u.VerificationCodeHash, &u.VerificationCodeExpiresAt, &u.VerificationCodeLastSentAt,
		&u.PasswordResetCodeHash, &u.PasswordResetExpiresAt, &u.PasswordResetLastSentAt,
		&u.IsAdmin, &u.IsSuspended, &u.CreatedAt, &u.SchoolID, &u.LastLoginAt, &u.LastActiveAt}
}

var bookingJoinCols = prefix("b", bookingCols) + ", " + prefix("l", listingCols)
var bookingJoinFrom = ` FROM bookings b JOIN listings l ON l.id = b.listing_id `

func scanBooking(s rowScanner) (model.Booking, error) {
	var b model.Booking
	err := s.Scan(append(bookingTargets(&b), listingTargets(&b.Listing)...)...)
	return b, err
}

func scanBookingAdmin(s rowScanner) (model.BookingAdmin, error) {
	var b model.BookingAdmin
	targets := append(bookingTargets(&b.Booking), listingTargets(&b.Listing)...)
	err := s.Scan(append(targets, userTargets(&b.User)...)...)
	return b, err
}

type NewBooking struct {
	UserID, ListingID, LeaseMonths int
	MoveInDate                     db.Date
	GuarantorName, GuarantorEmail  *string
	MessageToLandlord              *string
}

func CreateBooking(ctx context.Context, ex db.Executor, n NewBooking) (*model.Booking, error) {
	guarantorStatus := "none"
	if n.GuarantorEmail != nil && *n.GuarantorEmail != "" {
		guarantorStatus = "invited"
	}
	id, err := ex.InsertID(ctx, `INSERT INTO bookings (user_id, listing_id, move_in_date, lease_months, status,
		guarantor_name, guarantor_email, guarantor_status, message_to_landlord,
		request_sent, landlord_approved, guarantor_signed, lease_signed, deposit_paid, keys_collected)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.UserID, n.ListingID, n.MoveInDate, n.LeaseMonths, "pending",
		n.GuarantorName, n.GuarantorEmail, guarantorStatus, n.MessageToLandlord,
		true, false, false, false, false, false)
	if err != nil {
		return nil, err
	}
	return queryOne(ctx, ex, scanBooking, `SELECT `+bookingJoinCols+bookingJoinFrom+`WHERE b.id = ?`, id)
}

func BookingForUser(ctx context.Context, ex db.Executor, id, userID int) (*model.Booking, error) {
	return queryOne(ctx, ex, scanBooking,
		`SELECT `+bookingJoinCols+bookingJoinFrom+`WHERE b.id = ? AND b.user_id = ?`, id, userID)
}

func ListBookingsForUser(ctx context.Context, ex db.Executor, userID int, status *string) ([]model.Booking, error) {
	q := `SELECT ` + bookingJoinCols + bookingJoinFrom + `WHERE b.user_id = ?`
	args := []any{userID}
	if status != nil {
		q += ` AND b.status = ?`
		args = append(args, *status)
	}
	return queryList(ctx, ex, scanBooking, q+` ORDER BY b.created_at DESC, b.id DESC`, args...)
}

var bookingAdminCols = bookingJoinCols + ", " + prefix("u", userCols)
var bookingAdminFrom = bookingJoinFrom + `JOIN users u ON u.id = b.user_id `

// BookingWithRelations is the admin lookup — not scoped to a particular user.
func BookingWithRelations(ctx context.Context, ex db.Executor, id int) (*model.BookingAdmin, error) {
	return queryOne(ctx, ex, scanBookingAdmin,
		`SELECT `+bookingAdminCols+bookingAdminFrom+`WHERE b.id = ?`, id)
}

func ListPendingBookings(ctx context.Context, ex db.Executor) ([]model.BookingAdmin, error) {
	return queryList(ctx, ex, scanBookingAdmin,
		`SELECT `+bookingAdminCols+bookingAdminFrom+`WHERE b.status = 'pending' ORDER BY b.created_at ASC, b.id ASC`)
}

func DecideBooking(ctx context.Context, ex db.Executor, id int, status string) error {
	_, err := ex.Exec(ctx, `UPDATE bookings SET status = ?, landlord_approved = ? WHERE id = ?`,
		status, status == "approved", id)
	return err
}

func CancelBooking(ctx context.Context, ex db.Executor, id int) error {
	_, err := ex.Exec(ctx, `UPDATE bookings SET status = 'canceled' WHERE id = ?`, id)
	return err
}
