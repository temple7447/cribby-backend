package main

import (
	"context"
	"testing"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
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
	return d
}

func count(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func addHome(t *testing.T, d *db.DB, title string, owner *int) int {
	t.Helper()
	l, err := store.CreateListing(context.Background(), d.Executor, &model.Listing{Title: title, Type: "Studio", PricePer: 890, Total: 890,
		Grad: "g", Emoji: "🏠", Amenities: []string{}, Status: model.ListingApproved, OwnerID: owner})
	if err != nil {
		t.Fatal(err)
	}
	return l.ID
}

func setup(t *testing.T) (d *db.DB, demoHome, ownedSameTitle, realHome, demoUser, realUser int) {
	t.Helper()
	d = testDB(t)
	ctx := context.Background()
	hash := "x"
	real, err := store.CreateUser(ctx, d.Executor, "real@student.edu.ng", hash, "Real Person", "", "University of Benin", nil)
	if err != nil {
		t.Fatal(err)
	}
	maya, err := store.CreateUser(ctx, d.Executor, "maya@stateuniversity.edu", hash, "Maya Chen", "", "State University", nil)
	if err != nil {
		t.Fatal(err)
	}
	demoHome = addHome(t, d, "Sunny Studio on Maple St", nil)
	ownedSameTitle = addHome(t, d, "Garden Studio", &real.ID) // a real landlord who happens to reuse a title
	realHome = addHome(t, d, "Two-bedroom flat in Ugbowo", nil)
	if _, err := store.CreatePlace(ctx, d.Executor, &model.Place{Name: "Iya Basira Buka", Category: "restaurant", Emoji: "🍲", Latitude: 6.5, Longitude: 3.3}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePlace(ctx, d.Executor, &model.Place{Name: "Mama Put near the gate", Category: "restaurant", Emoji: "🍲", Latitude: 6.5, Longitude: 3.3}); err != nil {
		t.Fatal(err)
	}
	return d, demoHome, ownedSameTitle, realHome, maya.ID, real.ID
}

func TestDryRunChangesNothing(t *testing.T) {
	d, _, _, _, _, _ := setup(t)
	if err := purge(context.Background(), d, false); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM listings WHERE status = 'removed'`); n != 0 {
		t.Fatalf("dry run removed %d homes", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM users`); n != 2 {
		t.Fatalf("dry run changed users: %d", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM places`); n != 2 {
		t.Fatalf("dry run changed places: %d", n)
	}
}

func TestApplyRemovesOnlyDemoData(t *testing.T) {
	d, demoHome, ownedSameTitle, realHome, demoUser, realUser := setup(t)
	if err := purge(context.Background(), d, true); err != nil {
		t.Fatal(err)
	}
	status := func(id int) string {
		var s string
		if err := d.QueryRow(context.Background(), `SELECT status FROM listings WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := status(demoHome); s != "removed" {
		t.Errorf("placeholder home status = %q, want removed", s)
	}
	if s := status(ownedSameTitle); s != "approved" {
		t.Errorf("a landlord's own home with a placeholder title was touched: %q", s)
	}
	if s := status(realHome); s != "approved" {
		t.Errorf("a real ownerless home was touched: %q", s)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM users WHERE id = ?`, demoUser); n != 0 {
		t.Error("demo account still exists")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM users WHERE id = ?`, realUser); n != 1 {
		t.Error("a real account was deleted")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM places WHERE name = 'Iya Basira Buka'`); n != 0 {
		t.Error("demo place still exists")
	}
	if n := count(t, d, `SELECT COUNT(*) FROM places WHERE name = 'Mama Put near the gate'`); n != 1 {
		t.Error("a real place was deleted")
	}
	// running it again is harmless
	if err := purge(context.Background(), d, true); err != nil {
		t.Fatal(err)
	}
}
