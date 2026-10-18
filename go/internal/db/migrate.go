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
		`CREATE INDEX ix_bookings_listing_id ON bookings (listing_id)`,
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
	return nil
}
