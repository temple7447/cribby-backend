package store

import (
	"context"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// Roommate rows are always read joined to their owner: `verified` and
// `user_avatar_url` are computed from the user, not stored.
const roommateCols = `r.id, r.user_id, r.name, r.age, r.school, r.year, r.budget_min, r.budget_max, r.move_in_date,
	r.tags, r.bio, r.looking_for, r.preferred_areas, r.visibility, r.emoji, r.grad, r.banner_url, r.banner_public_id,
	r.created_at, (u.verification_status = 'verified'), u.avatar_url`

const roommateFrom = ` FROM roommate_profiles r JOIN users u ON u.id = r.user_id `

func scanRoommate(s rowScanner) (model.RoommateProfile, error) {
	var r model.RoommateProfile
	err := s.Scan(&r.ID, &r.UserID, &r.Name, &r.Age, &r.School, &r.Year, &r.BudgetMin, &r.BudgetMax, &r.MoveInDate,
		&r.Tags, &r.Bio, &r.LookingFor, &r.PreferredAreas, &r.Visibility, &r.Emoji, &r.Grad, &r.BannerURL,
		&r.BannerPublicID, &r.CreatedAt, &r.Verified, &r.UserAvatarURL)
	return r, err
}

func RoommateByID(ctx context.Context, ex db.Executor, id int) (*model.RoommateProfile, error) {
	return queryOne(ctx, ex, scanRoommate, `SELECT `+roommateCols+roommateFrom+`WHERE r.id = ?`, id)
}

func RoommateByUserID(ctx context.Context, ex db.Executor, userID int) (*model.RoommateProfile, error) {
	return queryOne(ctx, ex, scanRoommate, `SELECT `+roommateCols+roommateFrom+`WHERE r.user_id = ?`, userID)
}

func ListLiveRoommates(ctx context.Context, ex db.Executor) ([]model.RoommateProfile, error) {
	return queryList(ctx, ex, scanRoommate,
		`SELECT `+roommateCols+roommateFrom+`WHERE r.visibility = 'live' ORDER BY r.id`)
}

func CreateRoommate(ctx context.Context, ex db.Executor, userID int, p *model.RoommateProfile) (*model.RoommateProfile, error) {
	id, err := ex.InsertID(ctx, `INSERT INTO roommate_profiles (user_id, name, age, school, year, budget_min, budget_max,
		move_in_date, tags, bio, looking_for, preferred_areas, visibility, emoji, grad)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		userID, p.Name, p.Age, p.School, p.Year, p.BudgetMin, p.BudgetMax, p.MoveInDate, p.Tags, p.Bio,
		p.LookingFor, p.PreferredAreas, "live", p.Emoji, p.Grad)
	if err != nil {
		return nil, err
	}
	return RoommateByID(ctx, ex, id)
}

type RoommateUpdate struct {
	Bio, LookingFor, Visibility *string
	Tags, PreferredAreas        *db.JSONList[string]
}

// UpdateRoommate applies only the fields that are non-nil.
func UpdateRoommate(ctx context.Context, ex db.Executor, id int, u RoommateUpdate) error {
	_, err := ex.Exec(ctx, `UPDATE roommate_profiles SET
		bio = COALESCE(?, bio), looking_for = COALESCE(?, looking_for), visibility = COALESCE(?, visibility),
		tags = COALESCE(?, tags), preferred_areas = COALESCE(?, preferred_areas) WHERE id = ?`,
		u.Bio, u.LookingFor, u.Visibility, jsonArg(u.Tags), jsonArg(u.PreferredAreas), id)
	return err
}

// jsonArg turns an optional list into a driver argument, keeping NULL (not "[]")
// for "unset" so COALESCE leaves the column alone.
func jsonArg(l *db.JSONList[string]) any {
	if l == nil {
		return nil
	}
	v, _ := l.Value()
	return v
}

func SetRoommateVisibility(ctx context.Context, ex db.Executor, id int, visibility string) error {
	_, err := ex.Exec(ctx, `UPDATE roommate_profiles SET visibility = ? WHERE id = ?`, visibility, id)
	return err
}

func SetRoommateBanner(ctx context.Context, ex db.Executor, id int, url, publicID *string) error {
	_, err := ex.Exec(ctx, `UPDATE roommate_profiles SET banner_url = ?, banner_public_id = ? WHERE id = ?`, url, publicID, id)
	return err
}
