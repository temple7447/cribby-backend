// Command seed loads demo data. Subcommands:
//
//	seed demo     full demo dataset (the old scripts/seed.py) — one user, 6 listings,
//	              4 roommate profiles, places, 2 bookings, 2 conversations
//	seed places   idempotent: add missing places and backfill listing coordinates
//	              (the old scripts/seed_places.py); safe against production
//	seed photos   attach a stock photo to each demo listing via Cloudinary
//	              (the old scripts/seed_photos.py)
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"campus-backend/internal/config"
	"campus-backend/internal/db"
	"campus-backend/internal/model"
	"campus-backend/internal/security"
	"campus-backend/internal/storage"
	"campus-backend/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: seed demo|places|photos")
		os.Exit(2)
	}
	cfg := config.Load()
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := database.Migrate(ctx); err != nil {
		fatal(err)
	}

	switch os.Args[1] {
	case "demo":
		err = seedDemo(ctx, database)
	case "places":
		err = seedPlaces(ctx, database)
	case "photos":
		err = seedPhotos(ctx, database, cfg)
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

var amenities = []string{
	"🛏️ Furnished", "⚡ Utilities incl.", "🐱 Pet-friendly", "🧺 Laundry",
	"📶 Wi-Fi", "🅿️ Parking", "❄️ A/C", "🚲 Bike storage",
}

func f64(v float64) *float64 { return &v }
func str(v string) *string   { return &v }

// Coordinates are scattered around a campus reference point (~6.5158, 3.3898,
// the University of Lagos, Akoka) so proximity search / map display has real
// data to work with. Offsets roughly track the `walk` minutes.
var listings = []model.Listing{
	{Title: "Sunny Studio on Maple St", Type: "Studio", PricePer: 890, Total: 890, Walk: 8, Latitude: f64(6.5194), Longitude: f64(3.3934),
		Rating: 4.8, Reviews: 32, Verified: true, Hot: true, Grad: "from-coral to-sunshine", Emoji: "🌇"},
	{Title: "The Foundry House", Type: "Shared 4BR", PricePer: 480, Total: 1920, Walk: 12, Latitude: f64(6.5099), Longitude: f64(3.3951),
		Rating: 4.6, Reviews: 58, Verified: true, Video: true, Grad: "from-violet to-coral", Emoji: "🏘️"},
	{Title: "Corner 1BR with City View", Type: "1BR", PricePer: 1150, Total: 1150, Walk: 15, Latitude: f64(6.5241), Longitude: f64(3.3831),
		Rating: 4.4, Reviews: 19, Grad: "from-mint to-violet", Emoji: "🏙️"},
	{Title: "Cozy 2BR + Balcony", Type: "2BR", PricePer: 620, Total: 1240, Walk: 10, Latitude: f64(6.5112), Longitude: f64(3.3849),
		Rating: 4.9, Reviews: 41, Hot: true, Grad: "from-sunshine to-mint", Emoji: "🪴"},
	{Title: "Garden Studio", Type: "Studio", PricePer: 760, Total: 760, Walk: 20, Latitude: f64(6.5261), Longitude: f64(3.4001),
		Rating: 4.2, Reviews: 12, Video: true, Grad: "from-mint to-sunshine", Emoji: "🌿"},
	{Title: "Loft near North Gate", Type: "Shared 3BR", PricePer: 540, Total: 1620, Walk: 6, Latitude: f64(6.5131), Longitude: f64(3.3914),
		Rating: 4.7, Reviews: 27, Verified: true, Grad: "from-coral to-violet", Emoji: "🛋️"},
}

type roommateSeed struct {
	model.RoommateProfile
	UserVerified bool
}

var roommates = []roommateSeed{
	{RoommateProfile: model.RoommateProfile{Name: "Zoe", Age: 20, School: "State University", Year: "Junior", BudgetMin: 450, BudgetMax: 600,
		MoveInDate: "Aug 1", Tags: []string{"🦉 Night owl", "🧼 Tidy", "🐱 Cat ok"}, Grad: "from-coral to-sunshine", Emoji: "🎨",
		Bio: "Design major, plant mom, professional overthinker. Labels their leftovers.", LookingFor: "Shared 2-4BR, chill vibe, close to campus."},
		UserVerified: true},
	{RoommateProfile: model.RoommateProfile{Name: "Marcus", Age: 21, School: "State University", Year: "Senior", BudgetMin: 500, BudgetMax: 700,
		MoveInDate: "Aug 15", Tags: []string{"🐦 Early bird", "🏋️ Gym rat", "🎧 Chill"}, Grad: "from-violet to-mint", Emoji: "🏀",
		Bio: "Engineering senior. Up at 6, quiet by 10. Will fix your Wi-Fi.", LookingFor: "Quiet 1-2BR near the gym."}},
	{RoommateProfile: model.RoommateProfile{Name: "Priya", Age: 19, School: "State University", Year: "Sophomore", BudgetMin: 400, BudgetMax: 550,
		MoveInDate: "Sep 1", Tags: []string{"📚 Study vibe", "🌱 Vegan", "🧼 Tidy"}, Grad: "from-mint to-sunshine", Emoji: "📖",
		Bio: "Pre-med, chai enthusiast. Looking for a calm home base near the library.", LookingFor: "Studio or shared 2BR, near library."},
		UserVerified: true},
	{RoommateProfile: model.RoommateProfile{Name: "Tom", Age: 22, School: "State University", Year: "Grad", BudgetMin: 600, BudgetMax: 800,
		MoveInDate: "Aug 1", Tags: []string{"🦉 Night owl", "🎮 Gamer", "🐕 Dog ok"}, Grad: "from-sunshine to-coral", Emoji: "🎸",
		Bio: "Grad student, part-time barista. I come with a espresso machine and strong opinions.", LookingFor: "Shared house, pet-friendly."}},
}

// Points of interest around the same campus students search for — "where's the
// nearest pharmacy / ATM / bus stop from this listing".
var places = []model.Place{
	{Name: "Akoka General Hospital", Category: "hospital", Emoji: "🏥", Address: str("12 Akoka Rd"), Latitude: 6.5185, Longitude: 3.3902},
	{Name: "Bariga Health Centre", Category: "clinic", Emoji: "🩺", Address: str("Bariga"), Latitude: 6.5350, Longitude: 3.3830},
	{Name: "MedPlus Pharmacy Yaba", Category: "pharmacy", Emoji: "💊", Address: str("Herbert Macaulay Way, Yaba"), Latitude: 6.5145, Longitude: 3.3875},
	{Name: "HealthPlus Pharmacy Akoka", Category: "pharmacy", Emoji: "💊", Address: str("St. Finbarr's Rd, Akoka"), Latitude: 6.5205, Longitude: 3.3948},
	{Name: "GTBank Akoka", Category: "bank", Emoji: "🏦", Address: str("University Rd, Akoka"), Latitude: 6.5169, Longitude: 3.3925},
	{Name: "Access Bank Yaba", Category: "bank", Emoji: "🏦", Address: str("Commercial Ave, Yaba"), Latitude: 6.5098, Longitude: 3.3790},
	{Name: "UBA ATM — Unilag Gate", Category: "atm", Emoji: "🏧", Address: str("University Main Gate"), Latitude: 6.5162, Longitude: 3.3961},
	{Name: "Yaba Market", Category: "market", Emoji: "🛒", Address: str("Yaba"), Latitude: 6.5090, Longitude: 3.3775},
	{Name: "Akoka Foodstuff Market", Category: "market", Emoji: "🛒", Address: str("Akoka"), Latitude: 6.5220, Longitude: 3.3890},
	{Name: "Sweet Sensation Akoka", Category: "restaurant", Emoji: "🍽️", Address: str("University Rd"), Latitude: 6.5178, Longitude: 3.3930},
	{Name: "Iya Basira Buka", Category: "restaurant", Emoji: "🍲", Address: str("Akoka"), Latitude: 6.5195, Longitude: 3.3908},
	{Name: "Unilag Main Gate Bus Stop", Category: "bus_stop", Emoji: "🚌", Address: str("University Rd"), Latitude: 6.5155, Longitude: 3.3965},
	{Name: "Bariga Bus Stop", Category: "bus_stop", Emoji: "🚌", Address: str("Bariga Rd"), Latitude: 6.5360, Longitude: 3.3845},
	{Name: "FitZone Gym Akoka", Category: "gym", Emoji: "🏋️", Address: str("St. Finbarr's Rd"), Latitude: 6.5210, Longitude: 3.3875},
	{Name: "CleanPro Laundry", Category: "laundry", Emoji: "🧺", Address: str("Akoka"), Latitude: 6.5188, Longitude: 3.3945},
	{Name: "Akoka Police Station", Category: "police", Emoji: "🚓", Address: str("Akoka"), Latitude: 6.5240, Longitude: 3.3860},
	{Name: "Chapel of the Healing Cross", Category: "worship", Emoji: "⛪", Address: str("Unilag campus"), Latitude: 6.5170, Longitude: 3.3985},
	{Name: "Unilag Central Mosque", Category: "worship", Emoji: "🕌", Address: str("Unilag campus"), Latitude: 6.5175, Longitude: 3.3975},
}

func seedDemo(ctx context.Context, d *db.DB) error {
	return d.InTx(ctx, func(tx db.Executor) error {
		hash, err := security.HashPassword("password123")
		if err != nil {
			return err
		}
		maya, err := store.CreateUser(ctx, tx, "maya@stateuniversity.edu", hash, "Maya Chen", "State University")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET verification_status = 'verified' WHERE id = ?`, maya.ID); err != nil {
			return err
		}

		created := make([]*model.Listing, len(listings))
		for i := range listings {
			l := listings[i]
			l.Deposit, l.UtilitiesMonthly = l.PricePer, 45
			l.Amenities = amenities
			l.LandlordName, l.LandlordVerified, l.LandlordResponseRate = "Sandra", true, 95
			if created[i], err = store.CreateListing(ctx, tx, &l); err != nil {
				return err
			}
		}
		for i := range places {
			if _, err := store.CreatePlace(ctx, tx, &places[i]); err != nil {
				return err
			}
		}
		for _, rm := range roommates {
			u, err := store.CreateUser(ctx, tx, fmt.Sprintf("%s@stateuniversity.edu", lower(rm.Name)), hash, rm.Name, rm.School)
			if err != nil {
				return err
			}
			if rm.UserVerified {
				if _, err := tx.Exec(ctx, `UPDATE users SET verification_status = 'verified' WHERE id = ?`, u.ID); err != nil {
					return err
				}
			}
			p := rm.RoommateProfile
			if _, err := store.CreateRoommate(ctx, tx, u.ID, &p); err != nil {
				return err
			}
		}

		sunny, foundry := created[0], created[1]
		date := db.Date{Time: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
		if _, err := store.CreateBooking(ctx, tx, store.NewBooking{UserID: maya.ID, ListingID: sunny.ID, LeaseMonths: 12, MoveInDate: date}); err != nil {
			return err
		}
		b2, err := store.CreateBooking(ctx, tx, store.NewBooking{UserID: maya.ID, ListingID: foundry.ID, LeaseMonths: 12, MoveInDate: date})
		if err != nil {
			return err
		}
		if err := store.DecideBooking(ctx, tx, b2.ID, "approved"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE bookings SET guarantor_status = 'signed', guarantor_signed = ? WHERE id = ?`, true, b2.ID); err != nil {
			return err
		}

		landlord, err := tx.InsertID(ctx, `INSERT INTO conversations (user_id, kind, listing_id, title, subtitle, emoji, grad)
			VALUES (?,?,?,?,?,?,?)`, maya.ID, "landlord", sunny.ID, "Sandra", "Sunny Studio on Maple St", "👩‍💼", "from-coral to-sunshine")
		if err != nil {
			return err
		}
		roommate, err := tx.InsertID(ctx, `INSERT INTO conversations (user_id, kind, title, subtitle, emoji, grad)
			VALUES (?,?,?,?,?,?)`, maya.ID, "roommate", "Zoe", "Design major · Junior", "🎨", "from-coral to-sunshine")
		if err != nil {
			return err
		}
		if _, err := store.AddMessage(ctx, tx, landlord, "system", "Booking request sent ✓"); err != nil {
			return err
		}
		if _, err := store.AddMessage(ctx, tx, roommate, "them", "Hey! Are you still looking for a place near North Gate?"); err != nil {
			return err
		}
		fmt.Printf("Seeded: 1 primary user (maya@stateuniversity.edu / password123), %d listings, %d places, "+
			"%d roommate profiles, 2 bookings, 2 conversations.\n", len(listings), len(places), len(roommates))
		return nil
	})
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// seedPlaces is safe to run against any environment, including production: it
// only inserts places that don't exist yet (matched by name) and only sets
// coordinates on listings that don't have them.
func seedPlaces(ctx context.Context, d *db.DB) error {
	existing, err := d.Query(ctx, `SELECT name FROM places`)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for existing.Next() {
		var n string
		if err := existing.Scan(&n); err != nil {
			existing.Close()
			return err
		}
		names[n] = true
	}
	existing.Close()

	added := 0
	for i := range places {
		if names[places[i].Name] {
			continue
		}
		if _, err := store.CreatePlace(ctx, d.Executor, &places[i]); err != nil {
			return err
		}
		added++
	}
	fmt.Printf("places: +%d added, %d already present\n", added, len(names))

	coords := map[string]*model.Listing{}
	for i := range listings {
		coords[listings[i].Title] = &listings[i]
	}
	all, err := store.SearchListings(ctx, d.Executor, store.ListingSearch{})
	if err != nil {
		return err
	}
	// SearchListings hides reported listings; include them for the backfill too.
	hidden, err := d.Query(ctx, `SELECT id, title FROM listings WHERE is_hidden = ? AND latitude IS NULL`, true)
	if err != nil {
		return err
	}
	type idTitle struct {
		id    int
		title string
	}
	var extra []idTitle
	for hidden.Next() {
		var it idTitle
		if err := hidden.Scan(&it.id, &it.title); err != nil {
			hidden.Close()
			return err
		}
		extra = append(extra, it)
	}
	hidden.Close()

	backfilled := 0
	apply := func(id int, title string) error {
		src, ok := coords[title]
		if !ok {
			fmt.Printf("listing: skip (no mapped coords): %s\n", title)
			return nil
		}
		backfilled++
		return store.SetListingCoords(ctx, d.Executor, id, *src.Latitude, *src.Longitude)
	}
	for _, l := range all {
		if l.Latitude == nil || l.Longitude == nil {
			if err := apply(l.ID, l.Title); err != nil {
				return err
			}
		}
	}
	for _, it := range extra {
		if err := apply(it.id, it.title); err != nil {
			return err
		}
	}
	fmt.Printf("listings: %d coordinates backfilled\n", backfilled)
	return nil
}

// Real, license-free (Unsplash) stock photos picked to roughly match each
// listing's vibe — not the actual property, just a real interior/exterior photo
// instead of an emoji.
var photosByTitle = map[string]string{
	"Sunny Studio on Maple St":  "https://images.unsplash.com/photo-1522708323590-d24dbb6b0267?w=1600&q=80",
	"The Foundry House":         "https://images.unsplash.com/photo-1600585154340-be6161a56a0c?w=1600&q=80",
	"Corner 1BR with City View": "https://images.unsplash.com/photo-1560448204-e02f11c3d0e2?w=1600&q=80",
	"Cozy 2BR + Balcony":        "https://images.unsplash.com/photo-1493809842364-78817add7ffb?w=1600&q=80",
	"Garden Studio":             "https://images.unsplash.com/photo-1502672260266-1c1ef2d93688?w=1600&q=80",
	"Loft near North Gate":      "https://images.unsplash.com/photo-1512918728675-ed5a9ecdebfd?w=1600&q=80",
}

func seedPhotos(ctx context.Context, d *db.DB, cfg *config.Settings) error {
	if cfg.CloudinaryCloudName == "" {
		fmt.Println("CLOUDINARY_CLOUD_NAME not set — nothing to do.")
		return nil
	}
	cl := storage.NewCloudinary(cfg.CloudinaryCloudName, cfg.CloudinaryAPIKey, cfg.CloudinaryAPISecret)
	rows, err := d.Query(ctx, `SELECT id FROM listings ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		l, err := store.ListingByID(ctx, d.Executor, id)
		if err != nil || l == nil {
			return err
		}
		src, ok := photosByTitle[l.Title]
		switch {
		case !ok:
			fmt.Printf("skip (no mapped photo): %s\n", l.Title)
			continue
		case len(l.Photos) > 0:
			fmt.Printf("skip (already has photos): %s\n", l.Title)
			continue
		}
		up, err := cl.Upload(ctx, nil, src, fmt.Sprintf("cribby/listings/%d", l.ID), storage.TransformListing)
		if err != nil {
			return err
		}
		photo := model.Photo{ID: security.NewUUID(), URL: up.URL, PublicID: up.PublicID, Alt: l.Title, UploadedAt: db.Now()}
		if err := store.SetListingPhotos(ctx, d.Executor, l.ID, []model.Photo{photo}); err != nil {
			return err
		}
		fmt.Printf("uploaded: %s -> %s\n", l.Title, up.URL)
	}
	return nil
}
