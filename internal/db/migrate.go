package db

import (
	"context"
	"fmt"
	"strings"
)

// The migration chain below mirrors alembic/versions/*.py one-for-one and
// records progress in the same alembic_version table. A database already
// managed by Alembic (SQLite file or Neon) is therefore recognised as up to
// date, and a fresh one ends up identical — so Alembic and this binary stay
// interchangeable during the cutover.

type migration struct {
	rev   string
	stmts []string
}

// Placeholders: {PK} auto-increment primary key, {TS} timestamp with tz.
var migrations = []migration{
	{"f412bf25ed50", []string{ // initial schema
		`CREATE TABLE listings (
			id {PK}, title VARCHAR(200) NOT NULL, type VARCHAR(50) NOT NULL,
			price_per INTEGER NOT NULL, total INTEGER NOT NULL, utilities_monthly INTEGER NOT NULL,
			deposit INTEGER NOT NULL, walk INTEGER NOT NULL, rating FLOAT NOT NULL, reviews INTEGER NOT NULL,
			verified BOOLEAN NOT NULL, hot BOOLEAN NOT NULL, video BOOLEAN NOT NULL,
			grad VARCHAR(50) NOT NULL, emoji VARCHAR(8) NOT NULL, amenities JSON NOT NULL,
			landlord_name VARCHAR(100) NOT NULL, landlord_verified BOOLEAN NOT NULL,
			landlord_response_rate INTEGER NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE TABLE users (
			id {PK}, email VARCHAR(255) NOT NULL, hashed_password VARCHAR(255) NOT NULL,
			name VARCHAR(100) NOT NULL, school VARCHAR(150) NOT NULL, avatar_emoji VARCHAR(8) NOT NULL,
			verification_status VARCHAR(20) NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE UNIQUE INDEX ix_users_email ON users (email)`,
		`CREATE TABLE bookings (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id), listing_id INTEGER NOT NULL REFERENCES listings (id),
			move_in_date DATE NOT NULL, lease_months INTEGER NOT NULL, status VARCHAR(20) NOT NULL,
			guarantor_name VARCHAR(100), guarantor_email VARCHAR(255), guarantor_status VARCHAR(20) NOT NULL,
			message_to_landlord TEXT, request_sent BOOLEAN NOT NULL, landlord_approved BOOLEAN NOT NULL,
			guarantor_signed BOOLEAN NOT NULL, lease_signed BOOLEAN NOT NULL, deposit_paid BOOLEAN NOT NULL,
			keys_collected BOOLEAN NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ix_bookings_listing_id ON bookings (listing_id)`,
		`CREATE INDEX ix_bookings_user_id ON bookings (user_id)`,
		`CREATE TABLE favorites (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id), listing_id INTEGER NOT NULL REFERENCES listings (id),
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL,
			CONSTRAINT uq_favorite_user_listing UNIQUE (user_id, listing_id))`,
		`CREATE INDEX ix_favorites_listing_id ON favorites (listing_id)`,
		`CREATE INDEX ix_favorites_user_id ON favorites (user_id)`,
		`CREATE TABLE roommate_profiles (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id), name VARCHAR(100) NOT NULL,
			age INTEGER NOT NULL, school VARCHAR(150) NOT NULL, year VARCHAR(30) NOT NULL,
			budget_min INTEGER NOT NULL, budget_max INTEGER NOT NULL, move_in_date VARCHAR(30) NOT NULL,
			tags JSON NOT NULL, bio TEXT NOT NULL, looking_for TEXT NOT NULL, preferred_areas JSON NOT NULL,
			verified BOOLEAN NOT NULL, visibility VARCHAR(20) NOT NULL, emoji VARCHAR(8) NOT NULL,
			grad VARCHAR(50) NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE UNIQUE INDEX ix_roommate_profiles_user_id ON roommate_profiles (user_id)`,
		`CREATE TABLE conversations (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id), kind VARCHAR(20) NOT NULL,
			listing_id INTEGER REFERENCES listings (id), roommate_profile_id INTEGER REFERENCES roommate_profiles (id),
			title VARCHAR(150) NOT NULL, subtitle VARCHAR(200) NOT NULL, emoji VARCHAR(8) NOT NULL,
			grad VARCHAR(50) NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE INDEX ix_conversations_user_id ON conversations (user_id)`,
		`CREATE TABLE messages (
			id {PK}, conversation_id INTEGER NOT NULL REFERENCES conversations (id), sender VARCHAR(10) NOT NULL,
			text TEXT NOT NULL, read BOOLEAN NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE INDEX ix_messages_conversation_id ON messages (conversation_id)`,
	}},
	{"d383b8895691", []string{ // verification codes, admin flag, reports, blocks
		`CREATE TABLE blocks (
			id {PK}, blocker_id INTEGER NOT NULL REFERENCES users (id), blocked_id INTEGER NOT NULL REFERENCES users (id),
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL,
			CONSTRAINT uq_block_blocker_blocked UNIQUE (blocker_id, blocked_id))`,
		`CREATE INDEX ix_blocks_blocked_id ON blocks (blocked_id)`,
		`CREATE INDEX ix_blocks_blocker_id ON blocks (blocker_id)`,
		`CREATE TABLE reports (
			id {PK}, reporter_id INTEGER NOT NULL REFERENCES users (id), target_type VARCHAR(20) NOT NULL,
			target_id INTEGER NOT NULL, reason VARCHAR(20) NOT NULL, details TEXT, status VARCHAR(20) NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL, resolved_at {TS},
			resolved_by_id INTEGER REFERENCES users (id))`,
		`CREATE INDEX ix_reports_reporter_id ON reports (reporter_id)`,
		`CREATE INDEX ix_reports_target ON reports (target_type, target_id)`,
		`ALTER TABLE listings ADD COLUMN is_hidden BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE roommate_profiles DROP COLUMN verified`,
		`ALTER TABLE users ADD COLUMN verification_code_hash VARCHAR(64)`,
		`ALTER TABLE users ADD COLUMN verification_code_expires_at {TS}`,
		`ALTER TABLE users ADD COLUMN verification_code_last_sent_at {TS}`,
		`ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false`,
	}},
	{"a1f2c3d4e5b6", []string{ // listing photos
		`ALTER TABLE listings ADD COLUMN photos JSON NOT NULL DEFAULT '[]'`,
	}},
	{"f3c8a1b2d4e6", []string{ // password reset
		`ALTER TABLE users ADD COLUMN password_reset_code_hash VARCHAR(64)`,
		`ALTER TABLE users ADD COLUMN password_reset_code_expires_at {TS}`,
		`ALTER TABLE users ADD COLUMN password_reset_code_last_sent_at {TS}`,
	}},
	{"cdf07a93edb2", []string{ // user avatar photo
		`ALTER TABLE users ADD COLUMN avatar_url VARCHAR(500)`,
		`ALTER TABLE users ADD COLUMN avatar_public_id VARCHAR(255)`,
	}},
	{"a03f39ddc2a7", []string{ // roommate profile banner photo
		`ALTER TABLE roommate_profiles ADD COLUMN banner_url VARCHAR(500)`,
		`ALTER TABLE roommate_profiles ADD COLUMN banner_public_id VARCHAR(255)`,
	}},
	{"9a860857a8d3", []string{ // user suspension
		`ALTER TABLE users ADD COLUMN is_suspended BOOLEAN NOT NULL DEFAULT false`,
	}},
	{"b2c3d4e5f6a7", []string{ // listing coordinates
		`ALTER TABLE listings ADD COLUMN latitude FLOAT`,
		`ALTER TABLE listings ADD COLUMN longitude FLOAT`,
	}},
	{"6c35864e1b28", []string{ // places table
		`CREATE TABLE places (
			id {PK}, name VARCHAR(200) NOT NULL, category VARCHAR(50) NOT NULL, address VARCHAR(300),
			latitude FLOAT NOT NULL, longitude FLOAT NOT NULL, emoji VARCHAR(8) NOT NULL,
			created_at {TS} DEFAULT (CURRENT_TIMESTAMP) NOT NULL)`,
		`CREATE INDEX ix_places_category ON places (category)`,
	}},
}

// HeadRevision is the newest revision this binary knows about.
func HeadRevision() string { return migrations[len(migrations)-1].rev }

func (d Dialect) render(stmt string) string {
	pk, ts := "INTEGER PRIMARY KEY", "DATETIME"
	if d == Postgres {
		pk, ts = "SERIAL PRIMARY KEY", "TIMESTAMP WITH TIME ZONE"
	}
	return strings.NewReplacer("{PK}", pk, "{TS}", ts).Replace(stmt)
}

// Migrate brings the database up to HeadRevision, applying only the revisions
// after whatever alembic_version currently records.
func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.Exec(ctx, `CREATE TABLE IF NOT EXISTS alembic_version (
		version_num VARCHAR(32) NOT NULL, CONSTRAINT alembic_version_pkc PRIMARY KEY (version_num))`); err != nil {
		return fmt.Errorf("creating alembic_version: %w", err)
	}

	var current string
	rows, err := d.Query(ctx, `SELECT version_num FROM alembic_version`)
	if err != nil {
		return err
	}
	for rows.Next() {
		if err := rows.Scan(&current); err != nil {
			rows.Close()
			return err
		}
	}
	rows.Close()

	start := 0
	if current != "" {
		start = -1
		for i, m := range migrations {
			if m.rev == current {
				start = i + 1
			}
		}
		if start < 0 {
			return fmt.Errorf("database is at unknown revision %q (this binary knows up to %s)", current, HeadRevision())
		}
	}

	for _, m := range migrations[start:] {
		err := d.InTx(ctx, func(tx Executor) error {
			for _, s := range m.stmts {
				if _, err := tx.Exec(ctx, d.Dialect.render(s)); err != nil {
					return fmt.Errorf("revision %s: %w\n%s", m.rev, err, s)
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM alembic_version`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO alembic_version (version_num) VALUES (?)`, m.rev)
			return err
		})
		if err != nil {
			return err
		}
	}
	return d.MigrateGo(ctx)
}

// goMigrations are changes made after the Python backend was retired. They are
// tracked in their own table (go_migrations) and deliberately do NOT touch
// alembic_version: the old Python service can still share this database during
// the cutover, and Alembic must keep seeing its own head revision or its
// `alembic upgrade head` start command would fail. Everything here is purely
// additive (new tables, nullable columns) so the Python models ignore it.
var goMigrations = []struct {
	name  string
	stmts []string
}{
	{"001_schools", []string{
		`CREATE TABLE schools (
			id {PK}, name VARCHAR(200) NOT NULL, type VARCHAR(30) NOT NULL, state VARCHAR(60) NOT NULL,
			town VARCHAR(120) NOT NULL, funding VARCHAR(30) NOT NULL, latitude FLOAT, longitude FLOAT,
			confidence VARCHAR(20) NOT NULL, source VARCHAR(30) NOT NULL,
			CONSTRAINT uq_schools_name UNIQUE (name))`,
		`CREATE INDEX ix_schools_state ON schools (state)`,
		`ALTER TABLE users ADD COLUMN school_id INTEGER`,
	}},
	{"002_school_google_place_id", []string{
		`ALTER TABLE schools ADD COLUMN google_place_id VARCHAR(255)`,
	}},
	{"003_user_activity", []string{
		`ALTER TABLE users ADD COLUMN last_login_at {TS}`,
		`ALTER TABLE users ADD COLUMN last_active_at {TS}`,
	}},
	// Users can post their own apartments. Existing (admin-made) listings are owner-less and
	// stay "approved"; a user's post starts "pending" until an admin reviews it.
	{"004_user_listings", []string{
		`ALTER TABLE listings ADD COLUMN owner_id INTEGER`,
		`ALTER TABLE listings ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'approved'`,
		`ALTER TABLE listings ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE listings ADD COLUMN address VARCHAR(300) NOT NULL DEFAULT ''`,
		`ALTER TABLE listings ADD COLUMN review_note VARCHAR(500)`,
		`CREATE INDEX ix_listings_owner_id ON listings (owner_id)`,
		`CREATE INDEX ix_listings_status ON listings (status)`,
	}},
	// A public, username-style handle chosen at sign-up (the full name stays private). Empty for
	// accounts made before this existed or in the app; unique ignoring case when set.
	{"005_display_name", []string{
		`ALTER TABLE users ADD COLUMN display_name VARCHAR(40) NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX uq_users_display_name ON users (LOWER(display_name)) WHERE display_name <> ''`,
	}},
	// One walkthrough video per listing, hosted on Cloudinary. The older boolean `video` column
	// ("has a video tour") is kept in step with it.
	{"006_listing_video", []string{
		`ALTER TABLE listings ADD COLUMN video_url VARCHAR(600)`,
		`ALTER TABLE listings ADD COLUMN video_public_id VARCHAR(255)`,
	}},
	// Crash and error reports sent by the app and website. Identical errors are grouped on a
	// fingerprint so a crash loop can't flood the table.
	{"008_client_errors", []string{
		`CREATE TABLE client_errors (
			id {PK}, fingerprint VARCHAR(64) NOT NULL, source VARCHAR(10) NOT NULL,
			message VARCHAR(600) NOT NULL, stack TEXT NOT NULL DEFAULT '', route VARCHAR(200) NOT NULL DEFAULT '',
			app_version VARCHAR(40) NOT NULL DEFAULT '', platform VARCHAR(40) NOT NULL DEFAULT '',
			user_id INTEGER, count INTEGER NOT NULL DEFAULT 1,
			first_seen_at {TS} NOT NULL, last_seen_at {TS} NOT NULL, resolved BOOLEAN NOT NULL DEFAULT false,
			CONSTRAINT uq_client_errors_fingerprint UNIQUE (fingerprint))`,
		`CREATE INDEX ix_client_errors_last_seen ON client_errors (last_seen_at)`,
	}},
	// Landlord identity checks. One row per person; an admin looks at the ID photo and approves or
	// rejects. The photo is stored as a private image and only ever shown to admins.
	{"009_landlord_verification", []string{
		`CREATE TABLE landlord_verifications (
			user_id INTEGER NOT NULL PRIMARY KEY, status VARCHAR(12) NOT NULL,
			phone VARCHAR(30) NOT NULL, id_type VARCHAR(20) NOT NULL,
			id_photo_public_id VARCHAR(255) NOT NULL DEFAULT '', note VARCHAR(500) NOT NULL DEFAULT '',
			submitted_at {TS} NOT NULL, reviewed_at {TS})`,
		`CREATE INDEX ix_landlord_verifications_status ON landlord_verifications (status, submitted_at)`,
	}},
	// Reviews of a home (and, through it, its landlord). One per person per listing; only people who
	// booked or messaged about the home may write one. listings.rating/reviews are kept in step.
	{"010_reviews", []string{
		`CREATE TABLE reviews (
			id {PK}, listing_id INTEGER NOT NULL REFERENCES listings (id), user_id INTEGER NOT NULL REFERENCES users (id),
			rating INTEGER NOT NULL, body VARCHAR(1000) NOT NULL DEFAULT '',
			created_at {TS} NOT NULL, updated_at {TS} NOT NULL,
			CONSTRAINT uq_reviews_listing_user UNIQUE (listing_id, user_id))`,
		`CREATE INDEX ix_reviews_listing ON reviews (listing_id, created_at)`,
	}},
	// A listing that looks like a copy of someone else's is held for an admin, with a pointer to the original.
	{"011_listing_flags", []string{
		`CREATE TABLE listing_flags (
			listing_id INTEGER NOT NULL PRIMARY KEY REFERENCES listings (id),
			duplicate_of INTEGER NOT NULL, created_at {TS} NOT NULL)`,
	}},
	// People trusted to check homes in person (cousins, campus ambassadors). An admin adds them.
	{"012_verifiers", []string{
		`CREATE TABLE verifiers (
			user_id INTEGER NOT NULL PRIMARY KEY REFERENCES users (id),
			kind VARCHAR(20) NOT NULL, campus VARCHAR(120) NOT NULL DEFAULT '',
			verified_count INTEGER NOT NULL DEFAULT 0, created_at {TS} NOT NULL)`,
	}},
	// What the person who filmed or visited a home found: its condition, water and power, whether the
	// landlord agreed to it being posted, and the landlord's number (private; used to catch reposts).
	{"013_listing_checklists", []string{
		`CREATE TABLE listing_checklists (
			listing_id INTEGER NOT NULL PRIMARY KEY REFERENCES listings (id),
			condition VARCHAR(20) NOT NULL DEFAULT '', water VARCHAR(20) NOT NULL DEFAULT '',
			power VARCHAR(20) NOT NULL DEFAULT '', permission BOOLEAN NOT NULL DEFAULT false,
			contact_phone VARCHAR(30) NOT NULL DEFAULT '', updated_at {TS} NOT NULL)`,
		`CREATE INDEX ix_listing_checklists_phone ON listing_checklists (contact_phone)`,
	}},
	// Campus landing pages: cribby.space/<slug> for one school, with its own WhatsApp group link.
	{"014_campuses", []string{
		`CREATE TABLE campuses (
			slug VARCHAR(40) NOT NULL PRIMARY KEY, school_id INTEGER NOT NULL REFERENCES schools (id),
			whatsapp_url VARCHAR(300) NOT NULL DEFAULT '', tagline VARCHAR(200) NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT true, created_at {TS} NOT NULL)`,
	}},
	// Referral links for group leaders. A code belongs to one person; people who sign up with it are recorded
	// so we can count signups, requests and approved rentals through it.
	{"015_referrals", []string{
		`CREATE TABLE referral_codes (
			code VARCHAR(30) NOT NULL PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users (id),
			label VARCHAR(120) NOT NULL DEFAULT '', campus_slug VARCHAR(40) NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT true, created_at {TS} NOT NULL)`,
		`CREATE INDEX ix_referral_codes_owner ON referral_codes (owner_id)`,
		`CREATE TABLE referrals (
			user_id INTEGER NOT NULL PRIMARY KEY REFERENCES users (id),
			code VARCHAR(30) NOT NULL, created_at {TS} NOT NULL)`,
		`CREATE INDEX ix_referrals_code ON referrals (code)`,
	}},
	// Public scam alerts: students report fake agents and landlords. An admin approves each one before it is
	// public; the suspect's phone is kept in full but only shown masked.
	{"016_scam_alerts", []string{
		`CREATE TABLE scam_alerts (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id), kind VARCHAR(20) NOT NULL,
			suspect_name VARCHAR(120) NOT NULL DEFAULT '', suspect_phone VARCHAR(30) NOT NULL DEFAULT '',
			area VARCHAR(120) NOT NULL DEFAULT '', details TEXT NOT NULL,
			status VARCHAR(10) NOT NULL DEFAULT 'pending', review_note VARCHAR(500) NOT NULL DEFAULT '',
			created_at {TS} NOT NULL, reviewed_at {TS})`,
		`CREATE INDEX ix_scam_alerts_status ON scam_alerts (status, created_at)`,
		`CREATE INDEX ix_scam_alerts_phone ON scam_alerts (suspect_phone)`,
		`CREATE INDEX ix_scam_alerts_user ON scam_alerts (user_id, created_at)`,
	}},
	// Reviews can also score the water, the power and the landlord separately (0 = not rated).
	{"017_review_scores", []string{
		`ALTER TABLE reviews ADD COLUMN water INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE reviews ADD COLUMN power INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE reviews ADD COLUMN landlord INTEGER NOT NULL DEFAULT 0`,
	}},
	// Student marketplace: used mattresses, fans, fridges and so on. Items go live at once; reports hide them.
	{"018_market_items", []string{
		`CREATE TABLE market_items (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id),
			title VARCHAR(120) NOT NULL, description TEXT NOT NULL DEFAULT '', category VARCHAR(20) NOT NULL,
			condition VARCHAR(10) NOT NULL, price INTEGER NOT NULL DEFAULT 0,
			latitude FLOAT, longitude FLOAT, area VARCHAR(120) NOT NULL DEFAULT '',
			contact_phone VARCHAR(30) NOT NULL, leaving_soon BOOLEAN NOT NULL DEFAULT false,
			photos JSON NOT NULL, status VARCHAR(10) NOT NULL DEFAULT 'active',
			created_at {TS} NOT NULL, updated_at {TS} NOT NULL)`,
		`CREATE INDEX ix_market_items_status ON market_items (status, category, created_at)`,
		`CREATE INDEX ix_market_items_user ON market_items (user_id, created_at)`,
	}},
	// Vendors: businesses near campus (laundry, food, internet, printing, braiding, snacks...). Anyone can submit
	// one; an admin approves it. featured_until puts a paid vendor at the top of nearby results.
	{"019_vendors", []string{
		`CREATE TABLE vendors (
			id {PK}, owner_id INTEGER REFERENCES users (id), name VARCHAR(120) NOT NULL, category VARCHAR(20) NOT NULL,
			description VARCHAR(500) NOT NULL DEFAULT '', phone VARCHAR(30) NOT NULL DEFAULT '',
			address VARCHAR(200) NOT NULL DEFAULT '', latitude FLOAT NOT NULL, longitude FLOAT NOT NULL,
			student_run BOOLEAN NOT NULL DEFAULT false, status VARCHAR(10) NOT NULL DEFAULT 'pending',
			review_note VARCHAR(500) NOT NULL DEFAULT '', featured_until {TS},
			created_at {TS} NOT NULL)`,
		`CREATE INDEX ix_vendors_status ON vendors (status, category)`,
		`CREATE INDEX ix_vendors_owner ON vendors (owner_id, created_at)`,
	}},
	// What people search for around campus, so vendors can be shown real demand. Locations are rounded to ~1 km.
	{"020_search_events", []string{
		`CREATE TABLE search_events (
			id {PK}, kind VARCHAR(10) NOT NULL, label VARCHAR(80) NOT NULL, school_id INTEGER,
			lat FLOAT NOT NULL, lng FLOAT NOT NULL, created_at {TS} NOT NULL)`,
		`CREATE INDEX ix_search_events_time ON search_events (created_at)`,
	}},
	// Boosted listings: a landlord pays (by transfer, for now) and an admin sets how many days the listing ranks first.
	{"021_listing_boosts", []string{
		`CREATE TABLE listing_boosts (
			listing_id INTEGER NOT NULL PRIMARY KEY REFERENCES listings (id),
			boosted_until {TS} NOT NULL, created_at {TS} NOT NULL)`,
	}},
	// A verifier earns credit once per home, so marking it verified, un-verified and verified again can't pad their count.
	{"022_verifier_credits", []string{
		`CREATE TABLE verifier_credits (
			listing_id INTEGER NOT NULL REFERENCES listings (id), user_id INTEGER NOT NULL REFERENCES users (id),
			created_at {TS} NOT NULL, PRIMARY KEY (listing_id, user_id))`,
	}},
	// Student ID checks for people without a university email: a photo of a student ID, looked at by an admin.
	{"023_student_verifications", []string{
		`CREATE TABLE student_verifications (
			user_id INTEGER NOT NULL PRIMARY KEY REFERENCES users (id), status VARCHAR(12) NOT NULL,
			school VARCHAR(120) NOT NULL, id_photo_public_id VARCHAR(255) NOT NULL DEFAULT '',
			note VARCHAR(500) NOT NULL DEFAULT '', submitted_at {TS} NOT NULL, reviewed_at {TS})`,
		`CREATE INDEX ix_student_verifications_status ON student_verifications (status, submitted_at)`,
	}},
	// Marketplace orders with the money held by Cribby until the buyer has the item. Every change is also written to order_events.
	{"024_orders", []string{
		`CREATE TABLE orders (
			id {PK}, item_id INTEGER NOT NULL REFERENCES market_items (id),
			buyer_id INTEGER NOT NULL REFERENCES users (id), seller_id INTEGER NOT NULL REFERENCES users (id),
			title VARCHAR(120) NOT NULL, amount INTEGER NOT NULL, fee INTEGER NOT NULL, seller_amount INTEGER NOT NULL,
			status VARCHAR(20) NOT NULL, reference VARCHAR(60) NOT NULL, payment_url TEXT NOT NULL DEFAULT '',
			buyer_phone VARCHAR(30) NOT NULL DEFAULT '', seller_phone VARCHAR(30) NOT NULL DEFAULT '',
			dispute_reason VARCHAR(500) NOT NULL DEFAULT '', resolution_note VARCHAR(500) NOT NULL DEFAULT '',
			payout_status VARCHAR(10) NOT NULL DEFAULT 'none', payout_reference VARCHAR(120) NOT NULL DEFAULT '',
			created_at {TS} NOT NULL, updated_at {TS} NOT NULL, paid_at {TS}, handed_over_at {TS},
			auto_release_at {TS}, completed_at {TS},
			CONSTRAINT uq_orders_reference UNIQUE (reference))`,
		`CREATE INDEX ix_orders_buyer ON orders (buyer_id, created_at)`,
		`CREATE INDEX ix_orders_seller ON orders (seller_id, created_at)`,
		`CREATE INDEX ix_orders_item ON orders (item_id, status)`,
		`CREATE INDEX ix_orders_status ON orders (status, auto_release_at)`,
		`CREATE TABLE order_events (
			id {PK}, order_id INTEGER NOT NULL REFERENCES orders (id), actor_id INTEGER,
			event VARCHAR(30) NOT NULL, note VARCHAR(500) NOT NULL DEFAULT '', created_at {TS} NOT NULL)`,
		`CREATE INDEX ix_order_events_order ON order_events (order_id, created_at)`,
		`CREATE TABLE payout_accounts (
			user_id INTEGER NOT NULL PRIMARY KEY REFERENCES users (id),
			bank_name VARCHAR(60) NOT NULL, account_number VARCHAR(10) NOT NULL, account_name VARCHAR(100) NOT NULL,
			updated_at {TS} NOT NULL)`,
	}},
	{"025_deposits", []string{
		`CREATE TABLE deposits (
			id {PK}, booking_id INTEGER NOT NULL REFERENCES bookings (id), listing_id INTEGER NOT NULL REFERENCES listings (id),
			tenant_id INTEGER NOT NULL REFERENCES users (id), landlord_id INTEGER NOT NULL REFERENCES users (id),
			title VARCHAR(200) NOT NULL, amount INTEGER NOT NULL, fee INTEGER NOT NULL, landlord_amount INTEGER NOT NULL,
			status VARCHAR(20) NOT NULL, reference VARCHAR(60) NOT NULL, payment_url TEXT NOT NULL DEFAULT '',
			tenant_phone VARCHAR(30) NOT NULL DEFAULT '', move_in_date DATE NOT NULL,
			dispute_reason VARCHAR(500) NOT NULL DEFAULT '', resolution_note VARCHAR(500) NOT NULL DEFAULT '',
			payout_status VARCHAR(10) NOT NULL DEFAULT 'none', payout_reference VARCHAR(120) NOT NULL DEFAULT '',
			created_at {TS} NOT NULL, updated_at {TS} NOT NULL, paid_at {TS}, auto_release_at {TS}, completed_at {TS},
			CONSTRAINT uq_deposits_reference UNIQUE (reference))`,
		`CREATE INDEX ix_deposits_tenant ON deposits (tenant_id, created_at)`,
		`CREATE INDEX ix_deposits_landlord ON deposits (landlord_id, created_at)`,
		`CREATE INDEX ix_deposits_booking ON deposits (booking_id, status)`,
		`CREATE INDEX ix_deposits_status ON deposits (status, auto_release_at)`,
	}},
	{"026_listing_views", []string{
		`CREATE TABLE listing_views (
			id {PK}, listing_id INTEGER NOT NULL REFERENCES listings (id), viewer VARCHAR(64) NOT NULL, day VARCHAR(10) NOT NULL,
			CONSTRAINT uq_listing_views UNIQUE (listing_id, viewer, day))`,
		`CREATE INDEX ix_listing_views_day ON listing_views (listing_id, day)`,
	}},
	{"027_saved_searches", []string{
		`CREATE TABLE saved_searches (
			id {PK}, user_id INTEGER NOT NULL REFERENCES users (id), name VARCHAR(80) NOT NULL,
			max_price INTEGER, max_walk INTEGER, room_type VARCHAR(40) NOT NULL DEFAULT '', amenities VARCHAR(500) NOT NULL DEFAULT '',
			verified_only BOOLEAN NOT NULL DEFAULT false, landlord_verified_only BOOLEAN NOT NULL DEFAULT false,
			water VARCHAR(10) NOT NULL DEFAULT '', power VARCHAR(10) NOT NULL DEFAULT '',
			created_at {TS} NOT NULL, last_sent_at {TS})`,
		`CREATE INDEX ix_saved_searches_user ON saved_searches (user_id)`,
		`CREATE TABLE saved_search_seen (
			search_id INTEGER NOT NULL REFERENCES saved_searches (id), listing_id INTEGER NOT NULL,
			PRIMARY KEY (search_id, listing_id))`,
	}},
	// Landlords look up conversations and bookings by listing, so index those.
	{"007_owner_inbox_indexes", []string{
		`CREATE INDEX IF NOT EXISTS ix_conversations_listing_id ON conversations (listing_id)`,
		`CREATE INDEX IF NOT EXISTS ix_bookings_listing_id ON bookings (listing_id)`,
	}},
}

// MigrateGo applies pending Go-only migrations; call it after Migrate.
func (d *DB) MigrateGo(ctx context.Context) error {
	if _, err := d.Exec(ctx, `CREATE TABLE IF NOT EXISTS go_migrations (name VARCHAR(100) NOT NULL PRIMARY KEY)`); err != nil {
		return fmt.Errorf("creating go_migrations: %w", err)
	}
	for _, m := range goMigrations {
		var n int
		if err := d.QueryRow(ctx, `SELECT COUNT(*) FROM go_migrations WHERE name = ?`, m.name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		err := d.InTx(ctx, func(tx Executor) error {
			for _, s := range m.stmts {
				if _, err := tx.Exec(ctx, d.Dialect.render(s)); err != nil {
					return fmt.Errorf("go migration %s: %w\n%s", m.name, err, s)
				}
			}
			_, err := tx.Exec(ctx, `INSERT INTO go_migrations (name) VALUES (?)`, m.name)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// MarkApplied records a named one-off job (e.g. a data load) as done and
// reports whether it had already run.
func (d *DB) MarkApplied(ctx context.Context, tx Executor, name string) (alreadyDone bool, err error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM go_migrations WHERE name = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO go_migrations (name) VALUES (?)`, name)
	return false, err
}
