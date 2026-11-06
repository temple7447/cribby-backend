// Package backup takes and restores full-database snapshots.
//
//   - SQLite (local dev): VACUUM INTO writes a consistent copy of the file.
//   - Postgres (production): pg_dump in its compressed "custom" format, which pg_restore reads.
//
// Backups hold everything — password hashes, emails, messages — so they must be stored somewhere
// private and ideally encrypted. They are never written inside the repository (see .gitignore).
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"campus-backend/internal/db"
)

const (
	prefix    = "cribby-backup-"
	extPG     = ".dump"
	extSQLite = ".sqlite"
)

type Result struct {
	Path   string
	Bytes  int64
	Engine string // "sqlite" | "postgres"
	Detail string // what the verification step found
}

// Run writes a verified snapshot into dir and returns where it went.
func Run(ctx context.Context, databaseURL, dir string, now time.Time) (Result, error) {
	dialect, dsn, err := db.ParseURL(databaseURL)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	stamp := now.UTC().Format("20060102-150405")
	var res Result
	if dialect == db.SQLite {
		res, err = snapshotSQLite(ctx, databaseURL, filepath.Join(dir, prefix+stamp+extSQLite))
	} else {
		res, err = dumpPostgres(ctx, dsn, filepath.Join(dir, prefix+stamp+extPG))
	}
	if err == nil {
		err = os.Chmod(res.Path, 0o600) // readable by the owner only: it holds everyone's data
	}
	return res, err
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func sqliteURLFor(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "sqlite:///" + abs // "sqlite:////abs/path" once the leading slash of abs is added
}

func snapshotSQLite(ctx context.Context, databaseURL, out string) (Result, error) {
	src, err := db.Open(databaseURL)
	if err != nil {
		return Result{}, err
	}
	defer src.Close()
	if _, err := src.Exec(ctx, "VACUUM INTO "+quote(out)); err != nil {
		return Result{}, fmt.Errorf("snapshot: %w", err)
	}
	// Prove the copy opens and is intact before calling it a backup.
	cp, err := db.Open(sqliteURLFor(out))
	if err != nil {
		return Result{}, fmt.Errorf("the snapshot doesn't open: %w", err)
	}
	defer cp.Close()
	var check string
	if err := cp.QueryRow(ctx, "PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		return Result{}, fmt.Errorf("the snapshot failed its integrity check (%v %s)", err, check)
	}
	var users sql.NullInt64
	_ = cp.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&users)
	return Result{Path: out, Bytes: fileSize(out), Engine: "sqlite", Detail: fmt.Sprintf("integrity ok, %d users", users.Int64)}, nil
}

// pgEnv hands the connection to pg_dump/pg_restore through the environment, so the password never
// appears in the process list.
func pgEnv(dsn string) ([]string, string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, "", fmt.Errorf("invalid Postgres URL: %w", err)
	}
	env := append(os.Environ(), "PGHOST="+u.Hostname(), "PGDATABASE="+strings.TrimPrefix(u.Path, "/"))
	if p := u.Port(); p != "" {
		env = append(env, "PGPORT="+p)
	}
	if u.User != nil {
		env = append(env, "PGUSER="+u.User.Username())
		if pw, ok := u.User.Password(); ok {
			env = append(env, "PGPASSWORD="+pw)
		}
	}
	if m := u.Query().Get("sslmode"); m != "" {
		env = append(env, "PGSSLMODE="+m)
	}
	return env, u.Hostname() + "/" + strings.TrimPrefix(u.Path, "/"), nil
}

func tool(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found: install the PostgreSQL client tools (e.g. `brew install libpq` or `apt install postgresql-client`), "+
			"using a version at least as new as your database server", name)
	}
	return p, nil
}

func run(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func dumpPostgres(ctx context.Context, dsn, out string) (Result, error) {
	pgDump, err := tool("pg_dump")
	if err != nil {
		return Result{}, err
	}
	pgRestore, err := tool("pg_restore")
	if err != nil {
		return Result{}, err
	}
	env, _, err := pgEnv(dsn)
	if err != nil {
		return Result{}, err
	}
	if msg, err := run(ctx, env, pgDump, "--format=custom", "--no-owner", "--no-privileges", "--file="+out); err != nil {
		_ = os.Remove(out)
		return Result{}, fmt.Errorf("pg_dump failed: %w\n%s", err, strings.TrimSpace(msg))
	}
	// Prove the file is a readable archive that actually contains table data.
	list, err := run(ctx, os.Environ(), pgRestore, "--list", out)
	if err != nil {
		_ = os.Remove(out)
		return Result{}, fmt.Errorf("the dump isn't readable by pg_restore: %w\n%s", err, strings.TrimSpace(list))
	}
	tables := strings.Count(list, " TABLE DATA ")
	if tables == 0 {
		_ = os.Remove(out)
		return Result{}, errors.New("the dump contains no table data: refusing to keep it")
	}
	return Result{Path: out, Bytes: fileSize(out), Engine: "postgres", Detail: fmt.Sprintf("readable, %d tables", tables)}, nil
}

// Prune deletes the oldest backups in dir so only the newest `keep` remain. It only ever touches
// files this package wrote (cribby-backup-*.dump / .sqlite).
func Prune(dir string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, prefix) && (strings.HasSuffix(n, extPG) || strings.HasSuffix(n, extSQLite)) {
			names = append(names, n)
		}
	}
	sort.Strings(names) // the timestamp in the name sorts oldest-first
	var removed []string
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return removed, err
		}
		removed = append(removed, names[0])
		names = names[1:]
	}
	return removed, nil
}

// Target describes where a restore would write, for the confirmation message.
func Target(databaseURL string) (string, error) {
	dialect, dsn, err := db.ParseURL(databaseURL)
	if err != nil {
		return "", err
	}
	if dialect == db.SQLite {
		return "SQLite file " + strings.SplitN(strings.TrimPrefix(dsn, "file:"), "?", 2)[0], nil
	}
	_, target, err := pgEnv(dsn)
	return "Postgres " + target, err
}

// Restore loads a backup into the target database, REPLACING what is there.
func Restore(ctx context.Context, databaseURL, file string) error {
	if _, err := os.Stat(file); err != nil {
		return err
	}
	dialect, dsn, err := db.ParseURL(databaseURL)
	if err != nil {
		return err
	}
	if dialect == db.SQLite {
		if !strings.HasSuffix(file, extSQLite) {
			return fmt.Errorf("%s is not a SQLite backup (%s)", file, extSQLite)
		}
		dest := strings.SplitN(strings.TrimPrefix(dsn, "file:"), "?", 2)[0]
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o600)
	}
	if !strings.HasSuffix(file, extPG) {
		return fmt.Errorf("%s is not a Postgres backup (%s)", file, extPG)
	}
	pgRestore, err := tool("pg_restore")
	if err != nil {
		return err
	}
	env, _, err := pgEnv(dsn)
	if err != nil {
		return err
	}
	u, _ := url.Parse(dsn)
	msg, err := run(ctx, env, pgRestore, "--clean", "--if-exists", "--no-owner", "--no-privileges", "--exit-on-error",
		"--dbname="+strings.TrimPrefix(u.Path, "/"), file)
	if err != nil {
		return fmt.Errorf("pg_restore failed: %w\n%s", err, strings.TrimSpace(msg))
	}
	return nil
}
