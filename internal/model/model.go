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
	// DisplayName is the public, username-style handle ("" if none). Name is the person's full
	// name and is never shown publicly.
	DisplayName string `json:"display_name"`
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
	VideoURL             *string             `json:"video_url"`   // a playable (MP4) walkthrough the poster uploaded, if any
	VideoPublicID        *string             `json:"-"`
	Checklist            *Checklist          `json:"checklist,omitempty"` // only on a single listing, not in lists
	Boosted              bool                `json:"boosted"`             // a paid featured spot is running
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
	ID              int                 `json:"id"`
	UserID          int                 `json:"user_id"`
	Name            string              `json:"name"`
	Age             int                 `json:"age"`
	School          string              `json:"school"`
	Year            string              `json:"year"`
	BudgetMin       int                 `json:"budget_min"`
	BudgetMax       int                 `json:"budget_max"`
	MoveInDate      string              `json:"move_in_date"`
	Tags            db.JSONList[string] `json:"tags"`
	Bio             string              `json:"bio"`
	LookingFor      string              `json:"looking_for"`
	PreferredAreas  db.JSONList[string] `json:"preferred_areas"`
	Verified        bool                `json:"verified"`         // computed: owner's verification_status == "verified"
	StudentVerified bool                `json:"student_verified"` // computed: verified email at a university domain (.edu.ng, .edu)
	Visibility      string              `json:"visibility"`
	Emoji           string              `json:"emoji"`
	Grad            string              `json:"grad"`
	BannerURL       *string             `json:"banner_url"`
	BannerPublicID  *string             `json:"-"`
	UserAvatarURL   *string             `json:"user_avatar_url"` // computed from the owner
	CreatedAt       db.Time             `json:"created_at"`
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

// ClientError is a crash or error reported by the app or website, grouped by fingerprint.
type ClientError struct {
	ID          int     `json:"id"`
	Source      string  `json:"source"` // app | web
	Message     string  `json:"message"`
	Stack       string  `json:"stack"`
	Route       string  `json:"route"`
	AppVersion  string  `json:"app_version"`
	Platform    string  `json:"platform"`
	UserID      *int    `json:"user_id"`
	Count       int     `json:"count"`
	FirstSeenAt db.Time `json:"first_seen_at"`
	LastSeenAt  db.Time `json:"last_seen_at"`
	Resolved    bool    `json:"resolved"`
}

// StudentSummary is what a landlord sees of someone who contacted them: enough to judge the
// request, never contact details. Email appears only once a booking is approved.
type StudentSummary struct {
	Name     string  `json:"name"`
	School   string  `json:"school"`
	Verified bool    `json:"verified"`
	Email    *string `json:"email,omitempty"`
}

// OwnerConversation is a student's chat about one of the landlord's listings.
type OwnerConversation struct {
	ID           int            `json:"id"`
	ListingID    int            `json:"listing_id"`
	ListingTitle string         `json:"listing_title"`
	Student      StudentSummary `json:"student"`
	StudentID    int            `json:"-"`
	Unread       int            `json:"unread"` // the student's messages the landlord hasn't opened yet
	CreatedAt    db.Time        `json:"created_at"`
	LastMessage  *Message       `json:"last_message"`
	Messages     []Message      `json:"messages,omitempty"` // only on the single-conversation view
}

// OwnerBooking is a booking request on one of the landlord's listings.
type OwnerBooking struct {
	ID                int            `json:"id"`
	ListingID         int            `json:"listing_id"`
	ListingTitle      string         `json:"listing_title"`
	MoveInDate        db.Date        `json:"move_in_date"`
	LeaseMonths       int            `json:"lease_months"`
	Status            string         `json:"status"`
	MessageToLandlord *string        `json:"message_to_landlord"`
	GuarantorName     *string        `json:"guarantor_name"`
	CreatedAt         db.Time        `json:"created_at"`
	Student           StudentSummary `json:"student"`
}

// LandlordVerification is a landlord's identity check. IDPhotoPublicID is internal; admins get a
// signed link instead.
type LandlordVerification struct {
	UserID          int      `json:"user_id"`
	Status          string   `json:"status"` // none | pending | verified | rejected
	Phone           string   `json:"phone"`
	IDType          string   `json:"id_type"`
	IDPhotoPublicID string   `json:"-"`
	Note            string   `json:"note"` // why it was rejected
	SubmittedAt     *db.Time `json:"submitted_at"`
	ReviewedAt      *db.Time `json:"reviewed_at"`
}

// StudentVerification is a student-ID check for people without a university email. The photo is
// private and deleted once an admin decides.
type StudentVerification struct {
	UserID          int      `json:"user_id"`
	Status          string   `json:"status"` // none | pending | verified | rejected
	School          string   `json:"school"`
	IDPhotoPublicID string   `json:"-"`
	Note            string   `json:"note"`
	SubmittedAt     *db.Time `json:"submitted_at"`
	ReviewedAt      *db.Time `json:"reviewed_at"`
}

// Review is one person's rating of a home. Reviewer is a first name only.
type Review struct {
	ID        int     `json:"id"`
	ListingID int     `json:"listing_id"`
	UserID    int     `json:"-"`
	Rating    int     `json:"rating"`
	Water     int     `json:"water"`    // 0 = not rated
	Power     int     `json:"power"`    // 0 = not rated
	Landlord  int     `json:"landlord"` // 0 = not rated
	Body      string  `json:"body"`
	Reviewer  string  `json:"reviewer"`
	Mine      bool    `json:"mine"`
	CreatedAt db.Time `json:"created_at"`
}

// Verifier is someone trusted to check homes in person.
type Verifier struct {
	UserID        int     `json:"user_id"`
	Kind          string  `json:"kind"` // cousin | ambassador
	Campus        string  `json:"campus"`
	VerifiedCount int     `json:"verified_count"`
	Tier          string  `json:"tier"`         // starter | bronze | silver | gold, from verified_count
	NextTierAt    int     `json:"next_tier_at"` // homes needed for the next tier; 0 at the top
	CreatedAt     db.Time `json:"created_at"`
	Email         string  `json:"email"`
	Name          string  `json:"name"`
}

// Checklist is what the person who visited or filmed a home found. ContactPhone is private: only the
// owner, admins and verifiers get it back.
type Checklist struct {
	Condition    string `json:"condition"` // good | fair | needs_work
	Water        string `json:"water"`     // tap | borehole | well | none
	Power        string `json:"power"`     // always | most_of_day | few_hours | none
	Permission   bool   `json:"permission"`
	ContactPhone string `json:"contact_phone,omitempty"`
}

// Campus is a landing page for one school (cribby.space/<slug>).
type Campus struct {
	Slug        string  `json:"slug"`
	School      School  `json:"school"`
	WhatsAppURL string  `json:"whatsapp_url"`
	Tagline     string  `json:"tagline"`
	Active      bool    `json:"active"`
	CreatedAt   db.Time `json:"created_at"`
	HomesNear   int     `json:"homes_near"` // approved homes within 5 km of the school
}

// ReferralCode is a group leader's link code with its results. A rental counts once the landlord approves.
type ReferralCode struct {
	Code       string  `json:"code"`
	Label      string  `json:"label"`
	CampusSlug string  `json:"campus_slug"`
	Active     bool    `json:"active"`
	CreatedAt  db.Time `json:"created_at"`
	OwnerID    int     `json:"owner_id"`
	OwnerEmail string  `json:"owner_email"`
	OwnerName  string  `json:"owner_name"`
	Signups    int     `json:"signups"`
	Requests   int     `json:"requests"` // booking requests by people who came through the code
	Rentals    int     `json:"rentals"`  // of those, approved by the landlord
}

// ScamAlert is a report of a fake agent, landlord or listing. The phone is masked unless an admin asks.
type ScamAlert struct {
	ID           int      `json:"id"`
	UserID       int      `json:"-"`
	Kind         string   `json:"kind"` // fake_agent | fake_landlord | fake_listing | advance_fee | other
	SuspectName  string   `json:"suspect_name"`
	SuspectPhone string   `json:"suspect_phone"`
	Area         string   `json:"area"`
	Details      string   `json:"details"`
	Status       string   `json:"status"` // pending | approved | rejected
	ReviewNote   string   `json:"review_note"`
	CreatedAt    db.Time  `json:"created_at"`
	ReviewedAt   *db.Time `json:"reviewed_at"`
}

// MarketItem is something a student is selling or giving away. ContactPhone is only returned to signed-in people.
type MarketItem struct {
	ID           int                `json:"id"`
	UserID       int                `json:"-"`
	Seller       string             `json:"seller"` // first name only
	Title        string             `json:"title"`
	Description  string             `json:"description"`
	Category     string             `json:"category"`
	Condition    string             `json:"condition"` // new | good | fair
	Price        int                `json:"price"`     // naira; 0 = free
	Latitude     *float64           `json:"latitude"`
	Longitude    *float64           `json:"longitude"`
	DistanceKM   *float64           `json:"distance_km"` // only set on proximity searches
	Area         string             `json:"area"`
	ContactPhone string             `json:"contact_phone,omitempty"`
	LeavingSoon  bool               `json:"leaving_soon"` // sold by someone leaving: a room may be about to open up
	Photos       db.JSONList[Photo] `json:"photos"`
	Status       string             `json:"status"` // active | reserved | sold | reported | removed
	Mine         bool               `json:"mine"`
	// CanBuy: the buyer can pay through Cribby (payments are on, it has a price and the seller can be paid).
	CanBuy    bool    `json:"can_buy"`
	CreatedAt db.Time `json:"created_at"`
	UpdatedAt db.Time `json:"updated_at"`
}

// Order is a marketplace purchase whose money Cribby holds until the buyer has the item.
// status: awaiting_payment | paid | handed_over | completed | disputed | refunded | cancelled.
type Order struct {
	ID              int      `json:"id"`
	ItemID          int      `json:"item_id"`
	BuyerID         int      `json:"-"`
	SellerID        int      `json:"-"`
	Title           string   `json:"title"`
	Amount          int      `json:"amount"` // naira the buyer pays
	Fee             int      `json:"fee"`    // Cribby's cut, taken from the seller's side
	SellerAmount    int      `json:"seller_amount"`
	Status          string   `json:"status"`
	Reference       string   `json:"-"`
	PaymentURL      string   `json:"payment_url,omitempty"`
	BuyerPhone      string   `json:"buyer_phone,omitempty"`
	SellerPhone     string   `json:"seller_phone,omitempty"`
	DisputeReason   string   `json:"dispute_reason"`
	ResolutionNote  string   `json:"resolution_note"`
	PayoutStatus    string   `json:"payout_status"` // none | queued | paid
	PayoutReference string   `json:"payout_reference,omitempty"`
	CreatedAt       db.Time  `json:"created_at"`
	UpdatedAt       db.Time  `json:"updated_at"`
	PaidAt          *db.Time `json:"paid_at"`
	HandedOverAt    *db.Time `json:"handed_over_at"`
	AutoReleaseAt   *db.Time `json:"auto_release_at"`
	CompletedAt     *db.Time `json:"completed_at"`
}

// OrderEvent is one step in an order's history.
type OrderEvent struct {
	ID        int     `json:"id"`
	OrderID   int     `json:"-"`
	ActorID   *int    `json:"-"`
	Event     string  `json:"event"`
	Note      string  `json:"note"`
	CreatedAt db.Time `json:"created_at"`
}

// PayoutAccount is where a seller's money is sent.
type PayoutAccount struct {
	UserID        int     `json:"-"`
	BankName      string  `json:"bank_name"`
	AccountNumber string  `json:"account_number"`
	AccountName   string  `json:"account_name"`
	UpdatedAt     db.Time `json:"updated_at"`
}

func (m *MarketItem) Coords() (*float64, *float64) { return m.Latitude, m.Longitude }
func (m *MarketItem) SetDistance(km float64)       { m.DistanceKM = &km }
func (m *MarketItem) Distance() float64            { return *m.DistanceKM }

// Vendor is a business near campus. Featured vendors (featured_until in the future) rank first.
type Vendor struct {
	ID            int      `json:"id"`
	OwnerID       *int     `json:"-"`
	Name          string   `json:"name"`
	Category      string   `json:"category"`
	Description   string   `json:"description"`
	Phone         string   `json:"phone"`
	Address       string   `json:"address"`
	Latitude      float64  `json:"latitude"`
	Longitude     float64  `json:"longitude"`
	DistanceKM    *float64 `json:"distance_km"`
	StudentRun    bool     `json:"student_run"`
	Status        string   `json:"status"` // pending | approved | rejected | removed
	ReviewNote    string   `json:"review_note"`
	Featured      bool     `json:"featured"` // computed: featured_until is in the future
	FeaturedUntil *db.Time `json:"featured_until"`
	CreatedAt     db.Time  `json:"created_at"`
}

func (v *Vendor) Coords() (*float64, *float64) { return &v.Latitude, &v.Longitude }
func (v *Vendor) SetDistance(km float64)       { v.DistanceKM = &km }
func (v *Vendor) Distance() float64            { return *v.DistanceKM }

// Deposit is money a student paid to hold a home they were approved for. Cribby keeps it until the student
// confirms the move-in (or a few days pass after the move-in date with no complaint), then queues it for the landlord.
type Deposit struct {
	ID              int      `json:"id"`
	BookingID       int      `json:"booking_id"`
	ListingID       int      `json:"listing_id"`
	TenantID        int      `json:"-"`
	LandlordID      int      `json:"-"`
	Title           string   `json:"title"`
	Amount          int      `json:"amount"`
	Fee             int      `json:"fee"`
	LandlordAmount  int      `json:"landlord_amount"`
	Status          string   `json:"status"` // awaiting_payment | paid | completed | disputed | refunded | cancelled
	Reference       string   `json:"-"`
	PaymentURL      string   `json:"payment_url,omitempty"`
	TenantPhone     string   `json:"tenant_phone,omitempty"`
	MoveInDate      db.Date  `json:"move_in_date"`
	DisputeReason   string   `json:"dispute_reason"`
	ResolutionNote  string   `json:"resolution_note"`
	PayoutStatus    string   `json:"payout_status"`
	PayoutReference string   `json:"payout_reference,omitempty"`
	CreatedAt       db.Time  `json:"created_at"`
	UpdatedAt       db.Time  `json:"updated_at"`
	PaidAt          *db.Time `json:"paid_at"`
	AutoReleaseAt   *db.Time `json:"auto_release_at"`
	CompletedAt     *db.Time `json:"completed_at"`
}
