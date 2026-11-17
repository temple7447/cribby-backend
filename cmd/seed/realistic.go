package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/security"
)

// `seed realistic` replaces the 22 placeholder homes the old demo data left on the site (US-style names,
// prices like ₦890, no addresses, fake 4.8-star ratings and "verified" badges) with realistic Nigerian
// SAMPLE homes near UNIBEN (Benin City), FUTA (Akure) and UNILAG (Akoka).
//
//	go run ./cmd/seed realistic                         # dry run: prints what would change, changes nothing
//	go run ./cmd/seed realistic -apply                  # does it (take a backup first: go run ./cmd/backup)
//	go run ./cmd/seed realistic -apply -lock-demo-accounts
//
// Every home is updated in place, so favourites, chats and bookings keep working. A home is only touched if
// its id AND current title match what this tool expects and it has no owner; otherwise nothing changes at
// all. The new homes are clearly labelled "(sample)": they are not real homes, and a student who contacted a
// non-existent landlord would be exactly the kind of scam Cribby exists to prevent. They carry no ratings,
// no reviews, no response rate and no verification badges, because none of that is real.
//
// -lock-demo-accounts also suspends the old demo accounts (maya@, zoe@, marcus@, priya@, tom@stateuniversity.edu),
// scrambles their shared password ("password123", which is in the public source) and removes their roommate profiles.

const sampleBanner = "\n\nSAMPLE LISTING: this is an example of how a home looks on Cribby. It is not a real home, so please don't try to contact anyone about it."

// the shared amenity list (campus-rent/lib/amenities.ts); a test keeps every sample inside it.
var amenityChoices = []string{
	"⚡ 24h light", "🔋 Generator", "💧 Water", "🚰 Borehole", "🛡️ Security", "🏡 Fenced compound", "📶 Wi-Fi",
	"🅿️ Parking", "🍳 Kitchen", "🚿 Own bathroom", "❄️ AC", "🧺 Laundry", "🪑 Furnished",
}

type sampleHome struct {
	ID       int
	OldTitle string // the title the placeholder has now; a mismatch aborts the whole run
	School   string // exact school name; coordinates are placed around it
	North    float64
	East     float64 // metres from the school
	Title    string
	Type     string
	Area     string
	Price    int // naira per person per month
	Total    int // whole-home rent per month (== Price when one person pays it all)
	Utils    int
	Extras   []string
	About    string
}

const (
	uniben = "University of Benin"
	futa   = "Federal University of Technology Akure"
	unilag = "University of Lagos"
)

var sampleHomes = []sampleHome{
	// ---- Benin City, around UNIBEN (Ugbowo campus)
	{7, "Sunny Studio on Maple St", uniben, 420, 310, "Self-contain near the Ugbowo campus gate", "Self-contain", "Ugbowo", 25000, 25000, 3000,
		[]string{"⚡ 24h light", "💧 Water", "🛡️ Security", "🏡 Fenced compound", "🚿 Own bathroom", "🍳 Kitchen"},
		"Tiled self-contain with its own kitchen area and bathroom, in a fenced compound with a gateman. Prepaid meter. Quiet street, a short walk to lectures."},
	{12, "Loft near North Gate", uniben, -780, 650, "3-bedroom flat in Ekosodin, 3 students sharing", "3 bedroom+", "Ekosodin", 18000, 54000, 4000,
		[]string{"💧 Water", "🚰 Borehole", "🛡️ Security", "🍳 Kitchen", "📶 Wi-Fi"},
		"Three bedrooms, a shared living room and kitchen. Borehole water in the compound. The rent shown is one student's share; the whole flat is ₦54,000 a month."},
	{13, "Maplewood Studio Flat", uniben, -520, 900, "Neat single room in Ekosodin", "Single room", "Ekosodin", 12000, 12000, 2000,
		[]string{"💧 Water", "🛡️ Security", "🏡 Fenced compound"},
		"One room with a shared bathroom and kitchen in a student compound. Water supply is steady. Good for one person on a tight budget."},
	{14, "The Ridgeway Duplex", uniben, 1450, -380, "2-bedroom flat in Aduwawa, 2 sharing", "2 bedroom", "Aduwawa", 22000, 44000, 3000,
		[]string{"⚡ 24h light", "💧 Water", "🅿️ Parking", "🍳 Kitchen", "🛡️ Security"},
		"Two bedrooms with a living room and kitchen, ground floor. Space to park a car. A short bike or bus ride to campus. The rent shown is one student's share."},
	{15, "North Court Loft", uniben, 260, -540, "Clean tiled self-contain in Ugbowo", "Self-contain", "Ugbowo", 30000, 30000, 3000,
		[]string{"⚡ 24h light", "🔋 Generator", "💧 Water", "🛡️ Security", "🚿 Own bathroom", "❄️ AC"},
		"Newly painted self-contain with tiles, a standing wardrobe, and a space for an AC. Generator backup when there is no mains light."},
	{16, "Sunview 1BR", uniben, -1150, -260, "One-bedroom flat close to the back gate", "1 bedroom", "Ugbowo", 38000, 38000, 4000,
		[]string{"💧 Water", "🚰 Borehole", "🛡️ Security", "🍳 Kitchen", "🚿 Own bathroom", "🪑 Furnished"},
		"A separate bedroom and living room with a kitchen, lightly furnished. Suits one student or a couple of friends."},
	{17, "Garden Terrace Studio", uniben, 700, 1150, "Shared room in a student hostel, Ekosodin", "Hostel", "Ekosodin", 9000, 9000, 1000,
		[]string{"💧 Water", "🛡️ Security", "🏡 Fenced compound", "⚡ 24h light"},
		"A bed in a shared room (four to a room) in a managed hostel with a gateman. Shared bathrooms and a common kitchen."},
	{18, "Kingsley 4BR Shared House", uniben, 1900, 900, "Student house for 4 in Aduwawa", "3 bedroom+", "Aduwawa", 15000, 60000, 4000,
		[]string{"💧 Water", "🚰 Borehole", "🛡️ Security", "🍳 Kitchen", "🏡 Fenced compound"},
		"Four bedrooms, one per student, with a shared living room, kitchen and two bathrooms. The rent shown is one student's share."},

	// ---- Akure, around FUTA
	{19, "Willow Creek 2BR", futa, 520, -400, "2-bedroom flat in Aule, 2 sharing", "2 bedroom", "Aule", 20000, 40000, 3000,
		[]string{"⚡ 24h light", "💧 Water", "🚰 Borehole", "🍳 Kitchen", "🛡️ Security"},
		"Two bedrooms with a living room and kitchen in a gated compound. Borehole water. The rent shown is one student's share."},
	{20, "Parkside Studio", futa, -300, 380, "Self-contain close to FUTA", "Self-contain", "Aule", 20000, 20000, 2000,
		[]string{"💧 Water", "🛡️ Security", "🚿 Own bathroom", "🍳 Kitchen"},
		"A simple self-contain with its own bathroom, a short walk from the school. Prepaid meter."},
	{21, "Elmwood 1BR Deluxe", futa, 1750, 1100, "Spacious one-bedroom flat, Ijapo Estate", "1 bedroom", "Ijapo Estate", 32000, 32000, 4000,
		[]string{"⚡ 24h light", "🔋 Generator", "💧 Water", "🛡️ Security", "🅿️ Parking", "🪑 Furnished"},
		"A quiet estate with good roads and security. Bedroom, living room and kitchen, with parking in the compound."},
	{22, "Bellview Shared 3BR", futa, -1250, -900, "3-bedroom flat in Oba-Ile, 3 sharing", "3 bedroom+", "Oba-Ile", 14000, 42000, 3000,
		[]string{"💧 Water", "🚰 Borehole", "🍳 Kitchen", "🛡️ Security"},
		"Three bedrooms with a shared living room and kitchen. A bike or bus ride from campus. The rent shown is one student's share."},
	{23, "Cedar Grove Studio", futa, 380, 560, "Single room with shared bathroom, Aule", "Single room", "Aule", 10000, 10000, 1500,
		[]string{"💧 Water", "🛡️ Security", "🏡 Fenced compound"},
		"A clean room in a student compound with shared bathrooms. Good for one person on a small budget."},
	{24, "Highfield 2BR + Balcony", futa, 2100, -300, "2-bedroom with balcony and parking, Ijapo Estate", "2 bedroom", "Ijapo Estate", 25000, 50000, 4000,
		[]string{"⚡ 24h light", "🔋 Generator", "💧 Water", "🅿️ Parking", "🍳 Kitchen", "🛡️ Security"},
		"Two bedrooms, a balcony, and parking space, on the first floor. The rent shown is one student's share."},
	{25, "Southgate Shared 4BR", futa, -1700, 300, "Student house for 4, Oba-Ile", "3 bedroom+", "Oba-Ile", 12000, 48000, 3000,
		[]string{"💧 Water", "🚰 Borehole", "🍳 Kitchen", "🏡 Fenced compound"},
		"Four bedrooms in one house with shared kitchen and bathrooms. The rent shown is one student's share."},

	// ---- Lagos, around UNILAG (Akoka)
	{26, "Brookview 1BR", unilag, 640, 480, "Mini flat near UNILAG, Akoka", "Mini flat", "Akoka", 85000, 85000, 5000,
		[]string{"⚡ 24h light", "💧 Water", "🛡️ Security", "🍳 Kitchen", "🚿 Own bathroom", "❄️ AC"},
		"A mini flat with a bedroom, small living room and kitchen on a quiet street, a few minutes from the main gate."},
	{27, "Ivy Lane Studio", unilag, -450, 700, "Self-contain in Akoka", "Self-contain", "Akoka", 55000, 55000, 4000,
		[]string{"⚡ 24h light", "💧 Water", "🛡️ Security", "🚿 Own bathroom", "🍳 Kitchen"},
		"A self-contain with its own bathroom and kitchen area in a secured compound."},
	{28, "The Fairmont Shared 3BR", unilag, 1900, -250, "3-bedroom flat in Bariga, 3 sharing", "3 bedroom+", "Bariga", 35000, 105000, 5000,
		[]string{"💧 Water", "🛡️ Security", "🍳 Kitchen", "🔋 Generator"},
		"Three bedrooms with a shared living room and kitchen. The rent shown is one student's share."},
	{29, "Cambridge 2BR", unilag, -1300, -900, "2-bedroom flat in Yaba, 2 sharing", "2 bedroom", "Yaba", 55000, 110000, 5000,
		[]string{"⚡ 24h light", "💧 Water", "🛡️ Security", "🍳 Kitchen", "🅿️ Parking"},
		"Two bedrooms, a living room and kitchen, close to Yaba's shops and buses. The rent shown is one student's share."},
	{30, "Oakridge Studio", unilag, 1450, 650, "Single room in Bariga", "Single room", "Bariga", 25000, 25000, 3000,
		[]string{"💧 Water", "🛡️ Security", "🏡 Fenced compound"},
		"A room with a shared bathroom in a compound. A short bike ride from the school."},
	{31, "Lakeside Shared 4BR", unilag, 280, -1000, "Student apartment for 4, Akoka", "3 bedroom+", "Akoka", 30000, 120000, 5000,
		[]string{"⚡ 24h light", "💧 Water", "🛡️ Security", "🍳 Kitchen", "📶 Wi-Fi"},
		"Four bedrooms, one per student, with shared living room, kitchen and bathrooms. The rent shown is one student's share."},
	{32, "Regency 1BR Deluxe", unilag, -1050, 1200, "Furnished one-bedroom flat, Yaba", "1 bedroom", "Yaba", 95000, 95000, 6000,
		[]string{"⚡ 24h light", "🔋 Generator", "💧 Water", "🛡️ Security", "❄️ AC", "🪑 Furnished"},
		"A furnished one-bedroom flat with AC and generator backup, in a secured building."},
}

var demoEmails = []string{"maya@stateuniversity.edu", "zoe@stateuniversity.edu", "marcus@stateuniversity.edu", "priya@stateuniversity.edu", "tom@stateuniversity.edu"}

// walkMinutes is a rough on-foot time for a straight-line distance (a 1.3x road detour at 5 km/h).
func walkMinutes(northM, eastM float64) int {
	m := int(math.Ceil(math.Hypot(northM, eastM) / 1000 * 1.3 / 5 * 60))
	if m < 3 {
		m = 3
	}
	return m
}

// around moves a point by the given metres north and east.
func around(lat, lng, northM, eastM float64) (float64, float64) {
	return lat + northM/111320, lng + eastM/(111320*math.Cos(lat*math.Pi/180))
}

var errDryRun = errors.New("dry run")

func seedRealistic(ctx context.Context, d *db.DB, args []string) error {
	fs := flag.NewFlagSet("realistic", flag.ExitOnError)
	apply := fs.Bool("apply", false, "make the changes (without it, only print what would change)")
	lock := fs.Bool("lock-demo-accounts", false, "with -apply: suspend the old demo accounts and scramble their password")
	_ = fs.Parse(args)

	err := d.InTx(ctx, func(tx db.Executor) error {
		// 1. Refuse to touch anything that isn't exactly the placeholder we expect.
		var problems []string
		for _, h := range sampleHomes {
			var title string
			var owner *int
			err := tx.QueryRow(ctx, `SELECT title, owner_id FROM listings WHERE id = ?`, h.ID).Scan(&title, &owner)
			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("listing %d (%q) is not in this database", h.ID, h.OldTitle))
			case title != h.OldTitle:
				problems = append(problems, fmt.Sprintf("listing %d is now %q, expected %q", h.ID, title, h.OldTitle))
			case owner != nil:
				problems = append(problems, fmt.Sprintf("listing %d (%q) belongs to user %d, so it is a real listing", h.ID, title, *owner))
			}
		}
		if len(problems) > 0 {
			return fmt.Errorf("refusing to change anything:\n  - %s", strings.Join(problems, "\n  - "))
		}

		// 2. Campus reference points.
		ref := map[string][2]float64{}
		for _, h := range sampleHomes {
			if _, ok := ref[h.School]; ok {
				continue
			}
			var lat, lng float64
			if err := tx.QueryRow(ctx, `SELECT latitude, longitude FROM schools WHERE name = ?`, h.School).Scan(&lat, &lng); err != nil {
				return fmt.Errorf("school %q has no coordinates in this database: %w", h.School, err)
			}
			ref[h.School] = [2]float64{lat, lng}
		}

		// 3. Replace each placeholder in place.
		for _, h := range sampleHomes {
			lat, lng := around(ref[h.School][0], ref[h.School][1], h.North, h.East)
			fmt.Printf("  #%-2d %-30s -> %-52s %-13s ₦%-6d /person/month  %s\n", h.ID, h.OldTitle, h.Title+" (sample)", h.Type, h.Price, h.Area)
			if !*apply {
				continue
			}
			_, err := tx.Exec(ctx, `UPDATE listings SET title = ?, type = ?, price_per = ?, total = ?, utilities_monthly = ?, deposit = ?, walk = ?,
				latitude = ?, longitude = ?, amenities = ?, description = ?, address = ?, landlord_name = ?, landlord_verified = ?,
				landlord_response_rate = 0, verified = ?, hot = ?, rating = 0, reviews = 0 WHERE id = ? AND owner_id IS NULL`,
				h.Title+" (sample)", h.Type, h.Price, h.Total, h.Utils, h.Price, walkMinutes(h.North, h.East), lat, lng,
				db.JSONList[string](h.Extras), h.About+sampleBanner, h.Area+" (sample address)", "Sample landlord", false, false, false, h.ID)
			if err != nil {
				return fmt.Errorf("updating listing %d: %w", h.ID, err)
			}
		}

		// 4. The old demo accounts.
		found := 0
		for _, email := range demoEmails {
			var id int
			var suspended bool
			if err := tx.QueryRow(ctx, `SELECT id, is_suspended FROM users WHERE LOWER(email) = ?`, email).Scan(&id, &suspended); err != nil {
				continue
			}
			found++
			fmt.Printf("  ! demo account %s exists (user %d, suspended: %v) and shares the public password \"password123\"\n", email, id, suspended)
			if *apply && *lock {
				junk, err := security.HashPassword(security.NewUUID() + security.NewUUID())
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE users SET is_suspended = ?, hashed_password = ? WHERE id = ?`, true, junk, id); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE roommate_profiles SET visibility = 'removed' WHERE user_id = ?`, id); err != nil {
					return err
				}
				fmt.Printf("    locked: suspended, password scrambled, roommate profile removed\n")
			}
		}
		if found > 0 && !(*apply && *lock) {
			fmt.Println("  Run again with -apply -lock-demo-accounts to suspend them, or suspend them yourself in the admin dashboard.")
		}
		if found == 0 {
			fmt.Println("  no old demo accounts found")
		}

		if !*apply {
			return errDryRun // roll back: a dry run must change nothing
		}
		return nil
	})
	switch {
	case errors.Is(err, errDryRun):
		fmt.Println("\nDry run: nothing was changed. Take a backup (go run ./cmd/backup), then run again with -apply.")
		return nil
	case err != nil:
		return err
	}
	fmt.Printf("\nDone: %d homes replaced. They are labelled (sample) and carry no ratings, reviews or verification.\n", len(sampleHomes))
	return nil
}
