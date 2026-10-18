package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// bindJSON decodes the request body into dst (a pointer to a struct, optionally
// pre-filled with defaults) and enforces `validate` struct tags — the Go
// stand-in for pydantic. Supported rules: required, email, min=N (string
// length), oneof=a|b. Failures are 422s shaped like FastAPI's.
func bindJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return httpErr(http.StatusBadRequest, "Could not read request body")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return validationFailed(fieldError{"missing", []any{"body"}, "Field required"})
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			return validationFailed(fieldError{"json_invalid", []any{"body", int(syn.Offset)}, "JSON decode error"})
		}
		return validationFailed(fieldError{"model_attributes_type", []any{"body"},
			"Input should be a valid dictionary or object to extract fields from"})
	}
	if err := json.Unmarshal(body, dst); err != nil {
		var ute *json.UnmarshalTypeError
		msg, kind := err.Error(), "value_error"
		field := ""
		if errors.As(err, &ute) {
			msg, kind, field = "Input should be a valid "+goKindName(ute.Type), "type_error", ute.Field
		}
		// encoding/json doesn't name the field when a custom UnmarshalJSON (e.g.
		// db.Date) fails, so find it by decoding each present key on its own.
		if field == "" {
			field = badField(dst, raw)
		}
		loc := []any{"body"}
		if field != "" {
			for _, part := range strings.Split(field, ".") {
				loc = append(loc, part)
			}
		}
		return validationFailed(fieldError{kind, loc, msg})
	}
	return validateStruct(dst, raw)
}

// badField returns the JSON name of the first struct field whose raw value
// fails to decode, or "" if none can be singled out.
func badField(dst any, raw map[string]json.RawMessage) string {
	t := reflect.TypeOf(dst).Elem()
	for i := 0; i < t.NumField(); i++ {
		name := jsonName(t.Field(i))
		if rv, ok := raw[name]; ok {
			if json.Unmarshal(rv, reflect.New(t.Field(i).Type).Interface()) != nil {
				return name
			}
		}
	}
	return ""
}

func goKindName(t reflect.Type) string {
	if t.Name() == "Date" {
		return "date in YYYY-MM-DD format"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int64, reflect.Int32:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice:
		return "list"
	}
	return "value"
}

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "" {
		return f.Name
	}
	return name
}

func validateStruct(dst any, raw map[string]json.RawMessage) error {
	v := reflect.ValueOf(dst).Elem()
	t := v.Type()
	var errs []fieldError
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("validate")
		if tag == "" {
			continue
		}
		name := jsonName(t.Field(i))
		loc := []any{"body", name}
		rawVal, present := raw[name]
		present = present && string(bytes.TrimSpace(rawVal)) != "null"

		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer && !fv.IsNil() {
			fv = fv.Elem()
		}
		for _, rule := range strings.Split(tag, ",") {
			key, arg, _ := strings.Cut(rule, "=")
			if key == "required" {
				if !present {
					errs = append(errs, fieldError{"missing", loc, "Field required"})
					break
				}
				continue
			}
			if !present || fv.Kind() != reflect.String {
				continue
			}
			s := fv.String()
			switch key {
			case "email":
				normalized, ok := normalizeEmail(s)
				if !ok {
					errs = append(errs, fieldError{"value_error", loc, "value is not a valid email address"})
				} else {
					fv.SetString(normalized)
				}
			case "min":
				n, _ := strconv.Atoi(arg)
				if utf8.RuneCountInString(s) < n {
					errs = append(errs, fieldError{"string_too_short", loc,
						fmt.Sprintf("String should have at least %d characters", n)})
				}
			case "oneof":
				opts := strings.Split(arg, "|")
				found := false
				for _, o := range opts {
					found = found || o == s
				}
				if !found {
					quoted := make([]string, len(opts))
					for i, o := range opts {
						quoted[i] = "'" + o + "'"
					}
					errs = append(errs, fieldError{"literal_error", loc, "Input should be " + strings.Join(quoted, " or ")})
				}
			}
		}
	}
	if len(errs) > 0 {
		return validationFailed(errs...)
	}
	return nil
}

// normalizeEmail validates an address and lowercases its domain, which is what
// pydantic's EmailStr (email-validator) hands back.
func normalizeEmail(s string) (string, bool) {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || len(s) > 254 || len(local) > 64 || strings.Contains(domain, "@") {
		return "", false
	}
	if strings.ContainsAny(s, " \t\r\n,;<>()[]\\\"") || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") ||
		strings.Contains(local, "..") {
		return "", false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, l := range labels {
		if l == "" || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return "", false
		}
	}
	return local + "@" + strings.ToLower(domain), true
}

// queryParams collects typed query parameters, accumulating 422 errors the way
// FastAPI does instead of failing on the first bad one.
type queryParams struct {
	q    url.Values
	errs []fieldError
}

func newQuery(r *http.Request) *queryParams { return &queryParams{q: r.URL.Query()} }

func (p *queryParams) has(name string) bool { _, ok := p.q[name]; return ok }

func (p *queryParams) str(name string) string { return p.q.Get(name) }

func (p *queryParams) optStr(name string) *string {
	if !p.has(name) {
		return nil
	}
	s := p.q.Get(name)
	return &s
}

func (p *queryParams) optInt(name string) *int {
	if !p.has(name) {
		return nil
	}
	n, err := strconv.Atoi(p.q.Get(name))
	if err != nil {
		p.errs = append(p.errs, fieldError{"int_parsing", []any{"query", name},
			"Input should be a valid integer, unable to parse string as an integer"})
		return nil
	}
	return &n
}

func (p *queryParams) optFloat(name string) *float64 {
	if !p.has(name) {
		return nil
	}
	f, err := strconv.ParseFloat(p.q.Get(name), 64)
	if err != nil {
		p.errs = append(p.errs, fieldError{"float_parsing", []any{"query", name},
			"Input should be a valid number, unable to parse string as a number"})
		return nil
	}
	return &f
}

func (p *queryParams) err() error {
	if len(p.errs) > 0 {
		return validationFailed(p.errs...)
	}
	return nil
}

func pathInt(r *http.Request, name string) (int, error) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil {
		return 0, validationFailed(fieldError{"int_parsing", []any{"path", name},
			"Input should be a valid integer, unable to parse string as an integer"})
	}
	return n, nil
}

// ---- multipart image uploads ----

const (
	maxUploadBytes = 8 * 1024 * 1024 // 8MB
	// Allow a little headroom for multipart framing; real size is checked after.
	maxRequestBytes = maxUploadBytes + 1<<20
)

var allowedImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/heic": true}

type upload struct {
	file        multipart.File
	contentType string
	form        *multipart.Form
}

func (u *upload) close() {
	if u != nil && u.form != nil {
		_ = u.form.RemoveAll()
	}
}

// parseUpload reads the multipart body and requires a "file" part. A missing
// part is a 422, like FastAPI's File(...).
func parseUpload(w http.ResponseWriter, r *http.Request) (*upload, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := r.ParseMultipartForm(maxRequestBytes); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, httpErr(http.StatusBadRequest, "Image must be under 8MB")
		}
		return nil, validationFailed(fieldError{"missing", []any{"body", "file"}, "Field required"})
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		return nil, validationFailed(fieldError{"missing", []any{"body", "file"}, "Field required"})
	}
	ct := hdr.Header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		ct = mt
	}
	return &upload{file: f, contentType: ct, form: r.MultipartForm}, nil
}

// readImage validates the declared type and size and returns the bytes.
func (u *upload) readImage() ([]byte, error) {
	if !allowedImageTypes[u.contentType] {
		return nil, httpErr(http.StatusBadRequest, "Unsupported image type: "+u.contentType)
	}
	data, err := io.ReadAll(io.LimitReader(u.file, maxUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxUploadBytes {
		return nil, httpErr(http.StatusBadRequest, "Image must be under 8MB")
	}
	return data, nil
}
