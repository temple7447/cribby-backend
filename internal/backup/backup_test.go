package backup

import (
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"campus-backend/internal/db"
)

func sqliteURL(t *testing.T, path string) string {
	t.Helper()
	abs, _ := filepath.Abs(path)
	return "sqlite:///" + abs
}

func seededDB(t *testing.T, dir string) string {
	t.Helper()
	url := sqliteURL(t, filepath.Join(dir, "live.db"))
	d, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateGo(ctx); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"a@gmail.com", "b@gmail.com", "c@gmail.com"} {
		if _, err := d.Exec(ctx, `INSERT INTO users (email, hashed_password, name, school, avatar_emoji, verification_status, is_admin, is_suspended)
			VALUES (?, 'x', 'N', 'S', 'e', 'unverified', ?, ?)`, email, false, false); err != nil {
			t.Fatal(err)
		}
	}
	return url
}

func countUsers(t *testing.T, url string) int {
	t.Helper()
	d, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err := d.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSQLiteBackupIsVerifiedAndRestorable(t *testing.T) {
	dir := t.TempDir()
	live := seededDB(t, dir)
	out := filepath.Join(dir, "backups")

	res, err := Run(context.Background(), live, out, time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Path) != "cribby-backup-20261106-093000.sqlite" || res.Engine != "sqlite" || res.Bytes == 0 || !strings.Contains(res.Detail, "3 users") {
		t.Fatalf("result %+v", res)
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o700 {
		t.Fatalf("backup directory should be private (0700), is %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(res.Path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup file should be owner-only (0600), is %v", fi.Mode().Perm())
	}

	// lose the data, then restore it
	d, _ := db.Open(live)
	if _, err := d.Exec(context.Background(), `DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if n := countUsers(t, live); n != 0 {
		t.Fatalf("setup: %d users left", n)
	}
	if err := Restore(context.Background(), live, res.Path); err != nil {
		t.Fatal(err)
	}
	if n := countUsers(t, live); n != 3 {
		t.Fatalf("after restore: %d users, want 3", n)
	}
}

func TestRestoreRefusesTheWrongKindOfFile(t *testing.T) {
	dir := t.TempDir()
	live := seededDB(t, dir)
	bad := filepath.Join(dir, "notes.txt")
	os.WriteFile(bad, []byte("hi"), 0o600)
	if err := Restore(context.Background(), live, bad); err == nil {
		t.Fatal("restoring a random file must fail")
	}
	if err := Restore(context.Background(), live, filepath.Join(dir, "missing.sqlite")); err == nil {
		t.Fatal("a missing file must fail")
	}
}

func TestPruneKeepsTheNewestAndOnlyTouchesBackups(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"cribby-backup-20261101-000000.dump", "cribby-backup-20261102-000000.dump",
		"cribby-backup-20261103-000000.sqlite", "cribby-backup-20261104-000000.dump",
		"notes.txt", "cribby-backup-keep.me", // not backups: must survive
	} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	removed, err := Prune(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || removed[0] != "cribby-backup-20261101-000000.dump" || removed[1] != "cribby-backup-20261102-000000.dump" {
		t.Fatalf("removed %v", removed)
	}
	for _, keep := range []string{"cribby-backup-20261103-000000.sqlite", "cribby-backup-20261104-000000.dump", "notes.txt", "cribby-backup-keep.me"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s should have been kept", keep)
		}
	}
	if r, _ := Prune(dir, 0); r != nil {
		t.Fatal("keep=0 means keep everything")
	}
}

func TestPostgresConnectionIsPassedThroughTheEnvironmentNotArguments(t *testing.T) {
	env, target, err := pgEnv("postgres://neon_user:s3cret@ep-cool.neon.tech:5432/cribby?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	has := func(want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"PGHOST=ep-cool.neon.tech", "PGPORT=5432", "PGUSER=neon_user", "PGPASSWORD=s3cret", "PGDATABASE=cribby", "PGSSLMODE=require"} {
		if !has(want) {
			t.Errorf("missing %s", want)
		}
	}
	if target != "ep-cool.neon.tech/cribby" || strings.Contains(target, "s3cret") {
		t.Fatalf("target %q must name the server and database but never the password", target)
	}
}

// tempPostgres creates a throwaway database on the server TEST_DATABASE_URL points at and
// returns its URL; it is dropped afterwards, and nothing else on that server is touched.
func tempPostgres(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	admin, err := db.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("backup_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := neturl.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// With a real Postgres (TEST_DATABASE_URL, and pg_dump/pg_restore on PATH) the whole round trip runs.
func TestPostgresBackupAndRestore(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("set TEST_DATABASE_URL to run against Postgres")
	}
	if _, err := tool("pg_dump"); err != nil {
		t.Skip(err.Error())
	}
	ctx := context.Background()
	url := tempPostgres(t)
	d, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateGo(ctx); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"a@gmail.com", "b@gmail.com"} {
		if _, err := d.Exec(ctx, `INSERT INTO users (email, hashed_password, name, school, avatar_emoji, verification_status, is_admin, is_suspended)
			VALUES (?, 'x', 'N', 'S', 'e', 'unverified', ?, ?)`, email, false, false); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Run(ctx, url, t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Engine != "postgres" || !strings.Contains(res.Detail, "tables") {
		t.Fatalf("result %+v", res)
	}
	if _, err := d.Exec(ctx, `DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, url, res.Path); err != nil {
		t.Fatal(err)
	}
	d.Close() // the restore replaced the tables under the old connections
	d2, err := db.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var n int
	if err := d2.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("after restore: %d users (%v), want 2", n, err)
	}
}
