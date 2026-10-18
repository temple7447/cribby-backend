package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Time is a UTC timestamp that scans from every shape SQLite and Postgres
// hand back (time.Time, "2006-01-02 15:04:05", RFC 3339 strings…) and always
// serialises like pydantic does: ISO 8601 with a trailing "Z".
//
// This is the Go counterpart of the Python UTCDateTime TypeDecorator: SQLite
// has no timezone support, so naive values are stamped as UTC on read.
type Time struct{ time.Time }

// Now returns the current time at microsecond precision (what the Python app
// stored), so values round-trip identically.
func Now() Time { return Time{time.Now().UTC().Truncate(time.Microsecond)} }

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700 -0700",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02",
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " m="); i > 0 { // monotonic clock suffix from time.Time.String()
		s = s[:i]
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("db: cannot parse %q as a timestamp", s)
}

func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
	case time.Time:
		t.Time = v.UTC()
	case string:
		p, err := parseTime(v)
		if err != nil {
			return err
		}
		t.Time = p
	case []byte:
		p, err := parseTime(string(v))
		if err != nil {
			return err
		}
		t.Time = p
	default:
		return fmt.Errorf("db: cannot scan %T into Time", src)
	}
	return nil
}

func (t Time) Value() (driver.Value, error) { return t.Time.UTC(), nil }

func formatISO(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	return t.Format("2006-01-02T15:04:05.000000Z")
}

func (t Time) MarshalJSON() ([]byte, error) { return json.Marshal(formatISO(t.Time)) }

func (t *Time) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p, err := parseTime(s)
	if err != nil {
		return err
	}
	t.Time = p
	return nil
}

// Date is a calendar date (no time of day), stored as YYYY-MM-DD.
type Date struct{ time.Time }

func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		d.Time = time.Time{}
		return nil
	case time.Time:
		d.Time = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
		return nil
	case string:
		return d.parse(v)
	case []byte:
		return d.parse(string(v))
	}
	return fmt.Errorf("db: cannot scan %T into Date", src)
}

func (d *Date) parse(s string) error {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		s = s[:10]
	}
	p, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	d.Time = p
	return nil
}

func (d Date) String() string { return d.Time.Format("2006-01-02") }

func (d Date) Value() (driver.Value, error) { return d.String(), nil }

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON reports a bad value as a *json.UnmarshalTypeError so that
// encoding/json attaches the offending field's name, which the API turns into
// FastAPI's {"loc": ["body", "<field>"]}.
func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return &json.UnmarshalTypeError{Value: string(b), Type: reflect.TypeOf(*d)}
	}
	if err := d.parse(s); err != nil {
		return &json.UnmarshalTypeError{Value: "string", Type: reflect.TypeOf(*d)}
	}
	return nil
}

// JSONList is a JSON array stored in a JSON column. A nil list is written as
// "[]" and serialised as [] (never null), like the Python default=list columns.
type JSONList[T any] []T

func (l *JSONList[T]) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*l = JSONList[T]{}
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("db: cannot scan %T into JSONList", src)
	}
	if len(raw) == 0 {
		*l = JSONList[T]{}
		return nil
	}
	var out []T
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out == nil {
		out = []T{}
	}
	*l = out
	return nil
}

func (l JSONList[T]) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]T(l))
	return string(b), err
}

func (l JSONList[T]) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]T(l))
}
