// Package model holds the row types. JSON tags double as the response schema
// (the Python *Read pydantic models); secrets and internal columns are `json:"-"`.
package model

import "campus-backend/internal/db"

type User struct {
	ID                         int      `json:"id"`
	Email                      string   `json:"email"`
	HashedPassword             string   `json:"-"`
	Name                       string   `json:"name"`
	School                     string   `json:"school"`
	AvatarEmoji                string   `json:"avatar_emoji"`
	AvatarURL                  *string  `json:"avatar_url"`
	AvatarPublicID             *string  `json:"-"`
	VerificationStatus         string   `json:"verification_status"`
	VerificationCodeHash       *string  `json:"-"`
	VerificationCodeExpiresAt  *db.Time `json:"-"`
	VerificationCodeLastSentAt *db.Time `json:"-"`
	PasswordResetCodeHash      *string  `json:"-"`
	PasswordResetExpiresAt     *db.Time `json:"-"`
	PasswordResetLastSentAt    *db.Time `json:"-"`
	IsAdmin                    bool     `json:"is_admin"`
	IsSuspended                bool     `json:"is_suspended"`
	CreatedAt                  db.Time  `json:"created_at"`
	SchoolID                   *int     `json:"school_id"` // the school chosen from /schools, if any
	// LastLoginAt is when they last signed in; LastActiveAt is the last time they used the
	// app at all (refreshed at most every 10 minutes). Both are null for people who haven't
	// been seen since tracking began.
	LastLoginAt  *db.Time `json:"last_login_at"`
	LastActiveAt *db.Time `json:"last_active_at"`
}

type Photo struct {
	ID         string  `json:"id"`
	URL        string  `json:"url"`
	PublicID   string  `json:"public_id"`
	Alt        string  `json:"alt"`
	UploadedAt db.Time `json:"uploaded_at"`
}

type Listing struct {
	ID                   int                 `json:"id"`
	Title                string              `json:"title"`
	Type                 string              `json:"type"`
	PricePer             int                 `json:"price_per"`
	Total                int                 `json:"total"`
	UtilitiesMonthly     int                 `json:"utilities_monthly"`
	Deposit              int                 `json:"deposit"`
	Walk                 int                 `json:"walk"`
	Latitude             *float64            `json:"latitude"`
	Longitude            *float64            `json:"longitude"`
	DistanceKM           *float64            `json:"distance_km"` // only set on proximity searches
	Rating               float64             `json:"rating"`
	Reviews              int                 `json:"reviews"`
	Verified             bool                `json:"verified"`
	Hot                  bool                `json:"hot"`
	Video                bool                `json:"video"`
	IsHidden             bool                `json:"is_hidden"`
	Grad                 string              `json:"grad"`
	Emoji                string              `json:"emoji"`
	Amenities            db.JSONList[string] `json:"amenities"`
	Photos               db.JSONList[Photo]  `json:"photos"`
	LandlordName         string              `json:"landlord_name"`
	LandlordVerified     bool                `json:"landlord_verified"`
	LandlordResponseRate int                 `json:"landlord_response_rate"`
	CreatedAt            db.Time             `json:"created_at"`
	OwnerID              *int                `json:"owner_id"`    // the user who posted it; nil for admin-made listings
	Status               string              `json:"status"`      // pending | approved | rejected | removed
	Description          string              `json:"description"` // free text from the poster
	Address              string              `json:"address"`
	ReviewNote           *string             `json:"review_note"` // why an admin rejected it, shown to the owner
}

// Listing statuses. Only approved listings are public.
const (
	ListingPending  = "pending"
	ListingApproved = "approved"
	ListingRejected = "rejected"
	ListingRemoved  = "removed" // taken down by its owner (e.g. rented out)
)

func (l *Listing) Coords() (*float64, *float64) { return l.Latitude, l.Longitude }
func (l *Listing) SetDistance(km float64)       { l.DistanceKM = &km }
func (l *Listing) Distance() float64            { return *l.DistanceKM }

type Booking struct {
	ID                int     `json:"id"`
	UserID            int     `json:"-"`
	ListingID         int     `json:"listing_id"`
	MoveInDate        db.Date `json:"move_in_date"`
	LeaseMonths       int     `json:"lease_months"`
	Status            string  `json:"status"`
	GuarantorName     *string `json:"guarantor_name"`
	GuarantorEmail    *string `json:"guarantor_email"`
	GuarantorStatus   string  `json:"guarantor_status"`
	MessageToLandlord *string `json:"message_to_landlord"`
	RequestSent       bool    `json:"request_sent"`
	LandlordApproved  bool    `json:"landlord_approved"`
	GuarantorSigned   bool    `json:"guarantor_signed"`
	LeaseSigned       bool    `json:"lease_signed"`
	DepositPaid       bool    `json:"deposit_paid"`
	KeysCollected     bool    `json:"keys_collected"`
	CreatedAt         db.Time `json:"created_at"`
	Listing           Listing `json:"listing"`
}

// BookingAdmin is BookingRead plus the requesting student, for the admin
// pending-bookings review screen.
type BookingAdmin struct {
	Booking
	User User `json:"user"`
}

type RoommateProfile struct {
	ID             int                 `json:"id"`
	UserID         int                 `json:"user_id"`
	Name           string              `json:"name"`
	Age            int                 `json:"age"`
	School         string              `json:"school"`
	Year           string              `json:"year"`
	BudgetMin      int                 `json:"budget_min"`
	BudgetMax      int                 `json:"budget_max"`
	MoveInDate     string              `json:"move_in_date"`
	Tags           db.JSONList[string] `json:"tags"`
	Bio            string              `json:"bio"`
	LookingFor     string              `json:"looking_for"`
	PreferredAreas db.JSONList[string] `json:"preferred_areas"`
	Verified       bool                `json:"verified"` // computed: owner's verification_status == "verified"
	Visibility     string              `json:"visibility"`
	Emoji          string              `json:"emoji"`
	Grad           string              `json:"grad"`
	BannerURL      *string             `json:"banner_url"`
	BannerPublicID *string             `json:"-"`
	UserAvatarURL  *string             `json:"user_avatar_url"` // computed from the owner
	CreatedAt      db.Time             `json:"created_at"`
}

type Message struct {
	ID             int     `json:"id"`
	ConversationID int     `json:"-"`
	Sender         string  `json:"sender"`
	Text           string  `json:"text"`
	CreatedAt      db.Time `json:"created_at"`
}

type Conversation struct {
	ID                int       `json:"id"`
	UserID            int       `json:"-"`
	Kind              string    `json:"kind"`
	ListingID         *int      `json:"listing_id"`
	RoommateProfileID *int      `json:"roommate_profile_id"`
	CounterpartUserID *int      `json:"counterpart_user_id"`
	Title             string    `json:"title"`
	Subtitle          string    `json:"subtitle"`
	Emoji             string    `json:"emoji"`
	Grad              string    `json:"grad"`
	CreatedAt         db.Time   `json:"created_at"`
	Messages          []Message `json:"messages"`
}

type Favorite struct {
	ID        int
	UserID    int
	ListingID int
}

type Report struct {
	ID           int      `json:"id"`
	ReporterID   int      `json:"reporter_id"`
	TargetType   string   `json:"target_type"`
	TargetID     int      `json:"target_id"`
	Reason       string   `json:"reason"`
	Details      *string  `json:"details"`
	Status       string   `json:"status"`
	CreatedAt    db.Time  `json:"created_at"`
	ResolvedAt   *db.Time `json:"resolved_at"`
	ResolvedByID *int     `json:"resolved_by_id"`
	// Resolved server-side so a moderator UI doesn't need N follow-up calls.
	ReporterEmail *string `json:"reporter_email"`
	TargetLabel   *string `json:"target_label"`
}

type BlockedUserSummary struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	AvatarEmoji string  `json:"avatar_emoji"`
	AvatarURL   *string `json:"avatar_url"`
}

type Block struct {
	ID          int                `json:"id"`
	BlockerID   int                `json:"-"`
	BlockedID   int                `json:"blocked_id"`
	CreatedAt   db.Time            `json:"created_at"`
	BlockedUser BlockedUserSummary `json:"blocked_user"`
}

type Place struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	Address    *string  `json:"address"`
	Latitude   float64  `json:"latitude"`
	Longitude  float64  `json:"longitude"`
	Emoji      string   `json:"emoji"`
	DistanceKM *float64 `json:"distance_km"`
	CreatedAt  db.Time  `json:"created_at"`
}

func (p *Place) Coords() (*float64, *float64) { return &p.Latitude, &p.Longitude }
func (p *Place) SetDistance(km float64)       { p.DistanceKM = &km }
func (p *Place) Distance() float64            { return *p.DistanceKM }

// AdminUserDetail stitches one account's data together for the dashboard.
type AdminUserDetail struct {
	User            User             `json:"user"`
	Bookings        []Booking        `json:"bookings"`
	RoommateProfile *RoommateProfile `json:"roommate_profile"`
	ReportsFiled    []Report         `json:"reports_filed"`
	ReportsAgainst  []Report         `json:"reports_against"`
}

// School is an institution users can pick; distances in the app are measured to
// its coordinates. Confidence says how exact those coordinates are.
type School struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"` // university | polytechnic | college_of_education
	State      string   `json:"state"`
	Town       string   `json:"town"`
	Funding    string   `json:"funding"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
	Confidence string   `json:"confidence"` // high | medium | low (town centre) | very_low (state centre)
	Source     string   `json:"source"`     // wikidata | nominatim | town-centroid | state-centroid | google
	// GooglePlaceID links the school to its Google Business listing (see /maps/places/{id}).
	GooglePlaceID *string `json:"google_place_id"`
}
