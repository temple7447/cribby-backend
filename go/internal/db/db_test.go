package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSQLiteRoundTrip(t *testing.T) {
	ctx := context.Background()
	d, err := Open("sqlite+aiosqlite:///:memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// idempotent
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var rev string
	if err := d.QueryRow(ctx, `SELECT version_num FROM alembic_version`).Scan(&rev); err != nil || rev != HeadRevision() {
		t.Fatalf("rev=%q err=%v", rev, err)
	}

	id, err := d.InsertID(ctx, `INSERT INTO users (email, hashed_password, name, school, avatar_emoji, verification_status, is_admin, is_suspended)
		VALUES (?,?,?,?,?,?,?,?)`, "a@b.co", "x", "A", "S", "🎓", "unverified", false, false)
	if err != nil {
		t.Fatal(err)
	}
	sent := Time{time.Date(2026, 8, 19, 3, 12, 9, 659945000, time.UTC)}
	if _, err := d.Exec(ctx, `UPDATE users SET verification_code_last_sent_at=? WHERE id=?`, sent, id); err != nil {
		t.Fatal(err)
	}
	var created Time
	var got *Time
	var admin bool
	if err := d.QueryRow(ctx, `SELECT created_at, verification_code_last_sent_at, is_admin FROM users WHERE id=?`, id).Scan(&created, &got, &admin); err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Equal(sent.Time) {
		t.Fatalf("round trip: got %v want %v", got, sent)
	}
	if created.IsZero() || time.Since(created.Time) > time.Minute {
		t.Fatalf("created_at default not parsed sensibly: %v", created)
	}
	b, _ := json.Marshal(got)
	if string(b) != `"2026-08-19T03:12:09.659945Z"` {
		t.Fatalf("json: %s", b)
	}

	// raw stored text, for Alembic/SQLAlchemy compatibility
	var raw string
	d.QueryRow(ctx, `SELECT CAST(verification_code_last_sent_at AS TEXT) FROM users WHERE id=?`, id).Scan(&raw)
	t.Logf("stored as %q", raw)

	// Python-style naive timestamp written by SQLAlchemy
	d.Exec(ctx, `UPDATE users SET verification_code_last_sent_at='2026-08-19 03:12:09.659945' WHERE id=?`, id)
	d.QueryRow(ctx, `SELECT verification_code_last_sent_at FROM users WHERE id=?`, id).Scan(&got)
	if !got.Equal(sent.Time) {
		t.Fatalf("naive parse: %v", got)
	}

	// Date + JSON
	lid, _ := d.InsertID(ctx, `INSERT INTO listings (title,type,price_per,total,utilities_monthly,deposit,walk,rating,reviews,verified,hot,video,grad,emoji,amenities,landlord_name,landlord_verified,landlord_response_rate,is_hidden,photos)
		VALUES ('t','Studio',1,1,1,1,1,0,0,false,false,false,'g','e',?, 'S', true, 95, false, ?)`, JSONList[string]{"a", "b"}, JSONList[string](nil))
	bid, err := d.InsertID(ctx, `INSERT INTO bookings (user_id,listing_id,move_in_date,lease_months,status,guarantor_status,request_sent,landlord_approved,guarantor_signed,lease_signed,deposit_paid,keys_collected)
		VALUES (?,?,?,12,'pending','none',true,false,false,false,false,false)`, id, lid, Date{time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	var date Date
	var am JSONList[string]
	var ph JSONList[string]
	d.QueryRow(ctx, `SELECT move_in_date FROM bookings WHERE id=?`, bid).Scan(&date)
	d.QueryRow(ctx, `SELECT amenities, photos FROM listings WHERE id=?`, lid).Scan(&am, &ph)
	if date.String() != "2026-09-01" || len(am) != 2 || ph == nil || len(ph) != 0 {
		t.Fatalf("date=%v am=%v ph=%#v", date, am, ph)
	}
}

func TestParseURL(t *testing.T) {
	cases := []struct{ in, dsn string }{
		{"sqlite+aiosqlite:///./campus.db", "file:./campus.db?_time_format=sqlite&_pragma=busy_timeout(5000)"},
		{"sqlite:////abs/x.db", "file:/abs/x.db?_time_format=sqlite&_pragma=busy_timeout(5000)"},
		{"postgresql+asyncpg://u:p@h/db?ssl=require", "postgres://u:p@h/db?sslmode=require"},
		{"postgresql://u:p@h/db?sslmode=require&channel_binding=require", "postgres://u:p@h/db?sslmode=require"},
	}
	for _, c := range cases {
		_, _, dsn, err := parseURL(c.in)
		if err != nil || dsn != c.dsn {
			t.Errorf("%s -> %q (%v), want %q", c.in, dsn, err, c.dsn)
		}
	}
}
