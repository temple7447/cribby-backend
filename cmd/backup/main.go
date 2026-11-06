// Command backup takes a verified snapshot of the database.
//
//	go run ./cmd/backup                       # into ./backups
//	go run ./cmd/backup -out /secure/dir -keep 14
//
// It reads DATABASE_URL like the server does. For Postgres it needs pg_dump and pg_restore.
// Backups contain every user's data: keep them private, and encrypt them if they leave the machine.
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
	out := flag.String("out", "backups", "directory to write the backup into")
	keep := flag.Int("keep", 0, "keep only the newest N backups in the directory (0 = keep all)")
	flag.Parse()

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := backup.Run(ctx, cfg.DatabaseURL, *out, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "backup failed:", err)
		os.Exit(1)
	}
	fmt.Printf("backup ok: %s (%d bytes, %s: %s)\n", res.Path, res.Bytes, res.Engine, res.Detail)
	if removed, err := backup.Prune(*out, *keep); err != nil {
		fmt.Fprintln(os.Stderr, "pruning old backups failed:", err)
		os.Exit(1)
	} else if len(removed) > 0 {
		fmt.Printf("removed %d old backup(s)\n", len(removed))
	}
}
