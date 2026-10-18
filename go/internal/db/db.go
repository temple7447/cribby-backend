// Package db opens SQLite (local dev) or Postgres (production) behind one
// small interface, so the store layer can write a single set of queries.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	_ "modernc.org/sqlite"             // registers the "sqlite" driver (pure Go, no cgo)
)

type Dialect int

const (
	SQLite Dialect = iota
	Postgres
)

type runner interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// Executor runs queries on either the pool or an open transaction. Queries are
// written with "?" placeholders and rewritten to $1, $2… for Postgres.
type Executor struct {
	r       runner
	Dialect Dialect
}

func (e Executor) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return e.r.ExecContext(ctx, e.rebind(q), args...)
}

func (e Executor) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return e.r.QueryContext(ctx, e.rebind(q), args...)
}

func (e Executor) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return e.r.QueryRowContext(ctx, e.rebind(q), args...)
}

// InsertID runs an INSERT and returns the new row's id (both SQLite ≥3.35 and
// Postgres support RETURNING).
func (e Executor) InsertID(ctx context.Context, q string, args ...any) (int, error) {
	var id int
	err := e.QueryRow(ctx, q+" RETURNING id", args...).Scan(&id)
	return id, err
}

func (e Executor) rebind(q string) string {
	if e.Dialect != Postgres {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

// DB is the connection pool plus its dialect.
type DB struct {
	Executor
	SQL *sql.DB
}

// InTx runs fn in a transaction, committing if it returns nil.
func (d *DB) InTx(ctx context.Context, fn func(Executor) error) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(Executor{r: tx, Dialect: d.Dialect}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *DB) Close() error { return d.SQL.Close() }

// Open connects using a SQLAlchemy-style URL, so the existing DATABASE_URL
// (e.g. "sqlite+aiosqlite:///./campus.db" or
// "postgresql+asyncpg://user:pass@host/db") works unchanged.
func Open(rawURL string) (*DB, error) {
	dialect, driverName, dsn, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if dialect == SQLite {
		// One connection: SQLite serialises writers anyway, and an in-memory
		// database only exists per-connection.
		sqlDB.SetMaxOpenConns(1)
	} else {
		sqlDB.SetMaxOpenConns(10)
		// Neon (and similar serverless Postgres) suspends compute after a few idle
		// minutes, silently killing pooled connections — the Go equivalent of
		// SQLAlchemy's pool_pre_ping. Recycle idle connections well before that.
		sqlDB.SetConnMaxIdleTime(2 * time.Minute)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // allow a cold start
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	return &DB{Executor: Executor{r: sqlDB, Dialect: dialect}, SQL: sqlDB}, nil
}

func parseURL(raw string) (Dialect, string, string, error) {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return 0, "", "", fmt.Errorf("invalid DATABASE_URL %q", raw)
	}
	base, _, _ := strings.Cut(scheme, "+") // drop "+asyncpg" / "+aiosqlite"

	switch base {
	case "sqlite":
		path := strings.TrimPrefix(rest, "/") // sqlite:///rel.db -> rel.db, sqlite:////abs -> /abs
		if path == "" {
			path = ":memory:"
		}
		return SQLite, "sqlite", "file:" + path + "?_time_format=sqlite&_pragma=busy_timeout(5000)", nil
	case "postgres", "postgresql":
		u, err := url.Parse("postgres://" + rest)
		if err != nil {
			return 0, "", "", fmt.Errorf("invalid DATABASE_URL: %w", err)
		}
		q := u.Query()
		// asyncpg spells TLS as ssl=…; libpq/pgx spell it sslmode=…
		if v := q.Get("ssl"); v != "" {
			q.Del("ssl")
			if q.Get("sslmode") == "" {
				switch strings.ToLower(v) {
				case "true", "1", "yes", "on":
					v = "require"
				case "false", "0", "no", "off":
					v = "disable"
				}
				q.Set("sslmode", v)
			}
		}
		q.Del("channel_binding")
		u.RawQuery = q.Encode()
		return Postgres, "pgx", u.String(), nil
	}
	return 0, "", "", fmt.Errorf("unsupported DATABASE_URL scheme %q", scheme)
}

// IsUniqueViolation reports whether err is a unique-constraint failure on
// either database.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
