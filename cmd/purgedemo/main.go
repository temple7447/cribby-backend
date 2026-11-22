// Command purgedemo removes the demo data the early versions of Cribby left in a database: the US-style
// placeholder homes (and the "(sample)" homes an interim tool made), the demo accounts that share a public
// password, and the made-up places. It never adds anything.
//
//	go run ./cmd/purgedemo           # dry run: prints what it would remove, changes nothing
//	go run ./cmd/purgedemo -apply    # does it (take a backup first: go run ./cmd/backup)
//
// A home is only touched if it has no owner AND its title is one of the known placeholder titles, so a real
// listing is never caught. Homes are taken down (status "removed", the same way an owner takes one down)
// rather than erased, because bookings and favourites may still point at them. Demo accounts are deleted
// with the same routine as "delete my account".
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"campus-backend/internal/config"
	"campus-backend/internal/db"
	"campus-backend/internal/store"
)

var placeholderTitles = []string{
	// the original demo seed
	"Sunny Studio on Maple St", "The Foundry House", "Corner 1BR with City View", "Cozy 2BR + Balcony", "Garden Studio", "Loft near North Gate",
	// added later by the old scripts
	"Maplewood Studio Flat", "The Ridgeway Duplex", "North Court Loft", "Sunview 1BR", "Garden Terrace Studio", "Kingsley 4BR Shared House",
	"Willow Creek 2BR", "Parkside Studio", "Elmwood 1BR Deluxe", "Bellview Shared 3BR", "Cedar Grove Studio", "Highfield 2BR + Balcony",
	"Southgate Shared 4BR", "Brookview 1BR", "Ivy Lane Studio", "The Fairmont Shared 3BR", "Cambridge 2BR", "Oakridge Studio",
	"Lakeside Shared 4BR", "Regency 1BR Deluxe",
}

var demoEmails = []string{
	"maya@stateuniversity.edu", "zoe@stateuniversity.edu", "marcus@stateuniversity.edu", "priya@stateuniversity.edu", "tom@stateuniversity.edu",
}

var demoPlaces = []string{
	"Akoka General Hospital", "Bariga Health Centre", "MedPlus Pharmacy Yaba", "HealthPlus Pharmacy Akoka", "GTBank Akoka", "Access Bank Yaba",
	"UBA ATM — Unilag Gate", "Yaba Market", "Akoka Foodstuff Market", "Sweet Sensation Akoka", "Iya Basira Buka", "Unilag Main Gate Bus Stop",
	"Bariga Bus Stop", "FitZone Gym Akoka", "CleanPro Laundry", "Akoka Police Station", "Chapel of the Healing Cross", "Unilag Central Mosque",
}

var errDryRun = errors.New("dry run")

func main() {
	apply := flag.Bool("apply", false, "make the changes (without it, only print what would change)")
	flag.Parse()
	cfg := config.Load()
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	if err := purge(context.Background(), database, *apply); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func purge(ctx context.Context, d *db.DB, apply bool) error {
	err := d.InTx(ctx, func(tx db.Executor) error {
		// 1. Homes: ownerless, live (not already removed), and a placeholder title or a "(sample)" one.
		args := make([]any, len(placeholderTitles))
		for i, t := range placeholderTitles {
			args[i] = t
		}
		rows, err := tx.Query(ctx, `SELECT id, title FROM listings WHERE owner_id IS NULL AND status <> 'removed'
			AND (title IN (`+marks(len(args))+`) OR title LIKE '% (sample)')`, args...)
		if err != nil {
			return err
		}
		type home struct {
			id    int
			title string
		}
		var homes []home
		for rows.Next() {
			var h home
			if err := rows.Scan(&h.id, &h.title); err != nil {
				rows.Close()
				return err
			}
			homes = append(homes, h)
		}
		rows.Close()
		for _, h := range homes {
			fmt.Printf("  home #%-3d %s\n", h.id, h.title)
			if apply {
				if _, err := tx.Exec(ctx, `UPDATE listings SET status = 'removed' WHERE id = ? AND owner_id IS NULL`, h.id); err != nil {
					return err
				}
			}
		}

		// 2. Demo accounts.
		accounts := 0
		for _, email := range demoEmails {
			var id int
			if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE LOWER(email) = ?`, email).Scan(&id); err != nil {
				continue
			}
			accounts++
			fmt.Printf("  demo account %s (user %d)\n", email, id)
			if apply {
				if err := store.DeleteUserCascade(ctx, tx, id); err != nil {
					return fmt.Errorf("deleting %s: %w", email, err)
				}
			}
		}

		// 3. Made-up places.
		places := 0
		for _, name := range demoPlaces {
			var id int
			if err := tx.QueryRow(ctx, `SELECT id FROM places WHERE name = ?`, name).Scan(&id); err != nil {
				continue
			}
			places++
			fmt.Printf("  place #%-3d %s\n", id, name)
			if apply {
				if _, err := tx.Exec(ctx, `DELETE FROM places WHERE id = ?`, id); err != nil {
					return err
				}
			}
		}

		fmt.Printf("\n%d homes, %d demo accounts, %d demo places", len(homes), accounts, places)
		if !apply {
			fmt.Println(" would be removed.")
			return errDryRun // roll back: a dry run must change nothing
		}
		fmt.Println(" removed.")
		return nil
	})
	if errors.Is(err, errDryRun) {
		fmt.Println("Dry run: nothing was changed. Take a backup (go run ./cmd/backup), then run again with -apply.")
		return nil
	}
	return err
}
