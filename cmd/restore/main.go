// Command restore loads a backup into a database, REPLACING its contents.
//
//	go run ./cmd/restore -in backups/cribby-backup-20261106-120000.dump -yes
//
// By default the target is DATABASE_URL. Point it at a fresh database first (-url) to check a
// backup, before ever restoring over production. Nothing happens without -yes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"campus-backend/internal/backup"
	"campus-backend/internal/config"
)

func main() {
	in := flag.String("in", "", "the backup file to restore (required)")
	target := flag.String("url", "", "database URL to restore into (default: DATABASE_URL)")
	yes := flag.Bool("yes", false, "confirm that the target database will be overwritten")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: restore -in <backup file> [-url <database url>] -yes")
		os.Exit(2)
	}
	url := *target
	if url == "" {
		url = config.Load().DatabaseURL
	}
	where, err := backup.Target(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Restoring %s into %s — this REPLACES its data.\n", *in, where)
	if !*yes {
		fmt.Fprintln(os.Stderr, "Not doing anything: pass -yes to confirm.")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := backup.Restore(ctx, url, *in); err != nil {
		fmt.Fprintln(os.Stderr, "restore failed:", err)
		os.Exit(1)
	}
	fmt.Println("restore ok. Start the server once (it re-applies any newer migrations).")
}
