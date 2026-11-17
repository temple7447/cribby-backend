package main

import (
	"context"
	"strings"
	"testing"

	"campus-backend/internal/db"
	"campus-backend/internal/geo"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/store"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open("sqlite+aiosqlite:///:memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateGo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SyncSchools(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

// placeholders recreates what the old demo data left on the site: ids and titles as in production.
func placeholders(t *testing.T, d *db.DB) {
	t.Helper()
	ctx := context.Background()
	var temp []int
	for range sampleHomes {
		l, err := store.CreateListing(ctx, d.Executor, &model.Listing{Title: "tmp", Type: "Studio", PricePer: 890, Total: 890, Walk: 8,
			Rating: 4.8, Reviews: 32, Verified: true, Hot: true, Grad: "from-coral to-sunshine", Emoji: "🌇",
			LandlordName: "Sandra", LandlordVerified: true, LandlordResponseRate: 95, Amenities: []string{"🛏️ Furnished"}, Status: model.ListingApproved})
		if err != nil {
			t.Fatal(err)
		}
		temp = append(temp, l.ID)
	}
	for i, h := range sampleHomes { // two steps so ids never collide while moving
		if _, err := d.Exec(ctx, `UPDATE listings SET id = ? WHERE id = ?`, 1000+h.ID, temp[i]); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range sampleHomes {
		if _, err := d.Exec(ctx, `UPDATE listings SET id = ?, title = ? WHERE id = ?`, h.ID, h.OldTitle, 1000+h.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSampleDataIsSane(t *testing.T) {
	allowed := map[string]bool{}
	for _, a := range amenityChoices {
		allowed[a] = true
	}
	types := map[string]bool{"Self-contain": true, "Single room": true, "Shared room": true, "Mini flat": true, "1 bedroom": true, "2 bedroom": true, "3 bedroom+": true, "Hostel": true}
	if len(sampleHomes) != 22 {
		t.Fatalf("expected the 22 placeholder homes, got %d", len(sampleHomes))
	}
	ids := map[int]bool{}
	for _, h := range sampleHomes {
		if ids[h.ID] {
			t.Errorf("id %d twice", h.ID)
		}
		ids[h.ID] = true
		if !types[h.Type] {
			t.Errorf("%d: type %q is not one the app offers", h.ID, h.Type)
		}
		for _, a := range h.Extras {
			if !allowed[a] {
				t.Errorf("%d: amenity %q is not in the shared list", h.ID, a)
			}
		}
		// monthly, per person: a realistic Nigerian student range, and a whole-home rent that is a whole number of shares
		if h.Price < 5000 || h.Price > 150000 || h.Total < h.Price || h.Total%h.Price != 0 {
			t.Errorf("%d: price %d / total %d looks wrong", h.ID, h.Price, h.Total)
		}
		if h.Title == "" || h.Area == "" || h.About == "" || strings.Contains(h.About, "year") {
			t.Errorf("%d: missing text, or talks about a year", h.ID)
		}
		if m := walkMinutes(h.North, h.East); m < 3 || m > 45 {
			t.Errorf("%d: walk %d min", h.ID, m)
		}
	}
	if !ids[7] || !ids[12] || !ids[32] || ids[33] {
		t.Error("the id range must be 7 and 12-32, and never the real listing 33")
	}
}

func count(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDryRunChangesNothing(t *testing.T) {
	d := testDB(t)
	placeholders(t, d)
	if err := seedRealistic(context.Background(), d, nil); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM listings WHERE rating = 4.8 AND verified = ?`, true); n != 22 {
		t.Fatalf("a dry run changed %d homes", 22-n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM listings WHERE title LIKE '%(sample)'`); n != 0 {
		t.Fatalf("a dry run renamed %d homes", n)
	}
}

func TestApplyReplacesHomesInPlaceAndRemovesFakeProof(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	placeholders(t, d)
	// a real listing (someone's own) and a favourite on a placeholder: both must survive untouched
	u, err := store.CreateUser(ctx, d.Executor, "real@gmail.com", "x", "Real", "", "School", nil)
	if err != nil {
		t.Fatal(err)
	}
	real, err := store.CreateListing(ctx, d.Executor, &model.Listing{Title: "Neat Selfcon available", Type: "Self-contain", PricePer: 120000, Total: 120000,
		Grad: "g", Emoji: "🏠", OwnerID: &u.ID, Status: model.ListingApproved, Amenities: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO favorites (user_id, listing_id) VALUES (?, 7)`, u.ID); err != nil {
		t.Fatal(err)
	}

	if err := seedRealistic(ctx, d, []string{"-apply"}); err != nil {
		t.Fatal(err)
	}

	for _, h := range sampleHomes {
		l, err := store.ListingByID(ctx, d.Executor, h.ID)
		if err != nil || l == nil {
			t.Fatalf("listing %d: %v", h.ID, err)
		}
		if l.Title != h.Title+" (sample)" || l.PricePer != h.Price || l.Total != h.Total || l.Deposit != h.Price {
			t.Errorf("%d: %q ₦%d/%d deposit %d", h.ID, l.Title, l.PricePer, l.Total, l.Deposit)
		}
		if l.Verified || l.LandlordVerified || l.Hot || l.Rating != 0 || l.Reviews != 0 || l.LandlordResponseRate != 0 {
			t.Errorf("%d still carries fake proof: verified=%v landlord=%v hot=%v rating=%v reviews=%d response=%d", h.ID, l.Verified, l.LandlordVerified, l.Hot, l.Rating, l.Reviews, l.LandlordResponseRate)
		}
		if !strings.Contains(l.Description, "SAMPLE LISTING") || l.LandlordName != "Sample landlord" || l.Address == "" {
			t.Errorf("%d is not clearly labelled a sample: %q / %q", h.ID, l.LandlordName, l.Description)
		}
		var sLat, sLng float64
		if err := d.QueryRow(ctx, `SELECT latitude, longitude FROM schools WHERE name = ?`, h.School).Scan(&sLat, &sLng); err != nil {
			t.Fatal(err)
		}
		if km := geo.HaversineKM(sLat, sLng, *l.Latitude, *l.Longitude); km > 3 {
			t.Errorf("%d is %.1f km from %s", h.ID, km, h.School)
		}
		if l.Status != model.ListingApproved {
			t.Errorf("%d status changed to %q", h.ID, l.Status)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM favorites WHERE listing_id = 7`); n != 1 {
		t.Error("a favourite on a replaced home must survive")
	}
	after, _ := store.ListingByID(ctx, d.Executor, real.ID)
	if after.Title != "Neat Selfcon available" || after.PricePer != 120000 {
		t.Errorf("a real listing was touched: %+v", after)
	}
	// running it twice is refused, because the titles no longer match (safe against a double run)
	if err := seedRealistic(ctx, d, []string{"-apply"}); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("second run should refuse, got %v", err)
	}
}

func TestRefusesIfAnyHomeIsNotWhatItExpects(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	placeholders(t, d)
	if _, err := d.Exec(ctx, `UPDATE listings SET title = 'Someone renamed this' WHERE id = 20`); err != nil {
		t.Fatal(err)
	}
	err := seedRealistic(ctx, d, []string{"-apply"})
	if err == nil || !strings.Contains(err.Error(), "refusing") || !strings.Contains(err.Error(), "listing 20") {
		t.Fatalf("got %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM listings WHERE title LIKE '%(sample)'`); n != 0 {
		t.Fatalf("it changed %d homes despite refusing", n)
	}

	// and a placeholder that someone has since claimed is a real listing
	placeholders2 := testDB(t)
	placeholders(t, placeholders2)
	u, _ := store.CreateUser(ctx, placeholders2.Executor, "owner@gmail.com", "x", "Owner", "", "S", nil)
	if _, err := placeholders2.Exec(ctx, `UPDATE listings SET owner_id = ? WHERE id = 15`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := seedRealistic(ctx, placeholders2, []string{"-apply"}); err == nil || !strings.Contains(err.Error(), "real listing") {
		t.Fatalf("an owned listing must be refused, got %v", err)
	}
}

func TestDemoAccountsAreOnlyLockedWhenAsked(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	placeholders(t, d)
	hash, _ := security.HashPassword("password123")
	maya, err := store.CreateUser(ctx, d.Executor, "maya@stateuniversity.edu", hash, "Maya", "", "State University", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := seedRealistic(ctx, d, []string{"-apply"}); err != nil { // without the flag: reported, not changed
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM users WHERE id = ? AND is_suspended = ?`, maya.ID, false); n != 1 {
		t.Fatal("the account must not be touched without -lock-demo-accounts")
	}

	// the homes are already replaced, so lock in a fresh database
	d2 := testDB(t)
	placeholders(t, d2)
	maya2, _ := store.CreateUser(ctx, d2.Executor, "maya@stateuniversity.edu", hash, "Maya", "", "State University", nil)
	if err := seedRealistic(ctx, d2, []string{"-apply", "-lock-demo-accounts"}); err != nil {
		t.Fatal(err)
	}
	u, _ := store.UserByID(ctx, d2.Executor, maya2.ID)
	if !u.IsSuspended || security.VerifyPassword("password123", u.HashedPassword) {
		t.Fatalf("the demo account should be suspended with a scrambled password: suspended=%v", u.IsSuspended)
	}
}
