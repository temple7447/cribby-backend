package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"campus-backend/internal/db"
	"campus-backend/internal/model"
)

// ClientErrorReport is one crash or error as sent by a client.
type ClientErrorReport struct {
	Source, Message, Stack, Route, AppVersion, Platform string
	UserID                                              *int
}

// volatile matches the parts of a stack line that change between builds and devices: line and
// column numbers ("a.js:12:5") and memory addresses.
var volatile = regexp.MustCompile(`:\d+(:\d+)?|0x[0-9a-fA-F]+`)

// errorFingerprint groups the same error from different people and builds: the message plus the
// first few stack lines, with line numbers and addresses removed.
func errorFingerprint(r ClientErrorReport) string {
	lines := strings.Split(r.Stack, "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	for i, l := range lines {
		lines[i] = volatile.ReplaceAllString(strings.TrimSpace(l), "")
	}
	sum := sha256.Sum256([]byte(r.Source + "\x00" + r.Message + "\x00" + strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// RecordClientError stores a report, or bumps the counter if the same error was seen before
// (and re-opens it, since it has come back).
func RecordClientError(ctx context.Context, ex db.Executor, r ClientErrorReport) error {
	fp := errorFingerprint(r)
	bump := func() (int64, error) {
		res, err := ex.Exec(ctx, `UPDATE client_errors SET count = count + 1, last_seen_at = ?, resolved = ?,
			app_version = ?, platform = ?, user_id = COALESCE(?, user_id) WHERE fingerprint = ?`,
			db.Now(), false, r.AppVersion, r.Platform, r.UserID, fp)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	if n, err := bump(); err != nil || n > 0 {
		return err
	}
	now := db.Now()
	_, err := ex.Exec(ctx, `INSERT INTO client_errors (fingerprint, source, message, stack, route, app_version, platform,
		user_id, count, first_seen_at, last_seen_at, resolved) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		fp, r.Source, r.Message, r.Stack, r.Route, r.AppVersion, r.Platform, r.UserID, 1, now, now, false)
	if db.IsUniqueViolation(err) { // two first reports at once: the other one won, so just count ours
		_, err = bump()
	}
	return err
}

func scanClientError(s rowScanner) (model.ClientError, error) {
	var e model.ClientError
	err := s.Scan(&e.ID, &e.Source, &e.Message, &e.Stack, &e.Route, &e.AppVersion, &e.Platform, &e.UserID,
		&e.Count, &e.FirstSeenAt, &e.LastSeenAt, &e.Resolved)
	return e, err
}

const clientErrorCols = `id, source, message, stack, route, app_version, platform, user_id, count, first_seen_at, last_seen_at, resolved`

// ListClientErrors returns open (or resolved) errors, most recently seen first.
func ListClientErrors(ctx context.Context, ex db.Executor, resolved bool, limit int) ([]model.ClientError, error) {
	return queryList(ctx, ex, scanClientError,
		`SELECT `+clientErrorCols+` FROM client_errors WHERE resolved = ? ORDER BY last_seen_at DESC, id DESC LIMIT ?`,
		resolved, limit)
}

func SetClientErrorResolved(ctx context.Context, ex db.Executor, id int, resolved bool) (bool, error) {
	res, err := ex.Exec(ctx, `UPDATE client_errors SET resolved = ? WHERE id = ?`, resolved, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func CountOpenClientErrors(ctx context.Context, ex db.Executor) (int, error) {
	var n int
	err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM client_errors WHERE resolved = ?`, false).Scan(&n)
	return n, err
}
