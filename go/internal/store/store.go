// Package store holds the SQL for every resource. Functions take a
// db.Executor so they run unchanged on the pool or inside a transaction, and
// "get" functions return (nil, nil) when nothing matches — the Go analogue of
// the Python crud layer returning None.
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"campus-backend/internal/db"
)

type rowScanner interface{ Scan(dest ...any) error }

func queryOne[T any](ctx context.Context, ex db.Executor, scan func(rowScanner) (T, error), q string, args ...any) (*T, error) {
	v, err := scan(ex.QueryRow(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// queryList always returns a non-nil slice so it serialises as [] not null.
func queryList[T any](ctx context.Context, ex db.Executor, scan func(rowScanner) (T, error), q string, args ...any) ([]T, error) {
	rows, err := ex.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// prefix turns "id, title" into "l.id, l.title".
func prefix(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// ilike is a portable case-insensitive substring match (SQLite has no ILIKE).
func ilike(col string) string { return "LOWER(" + col + ") LIKE LOWER(?)" }

func contains(s string) string { return "%" + s + "%" }
