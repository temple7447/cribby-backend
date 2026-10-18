package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

// APIError is an HTTP error rendered as FastAPI does: {"detail": ...}.
type APIError struct {
	Status int
	Detail any
	Header http.Header
}

func (e *APIError) Error() string { return http.StatusText(e.Status) }

func httpErr(status int, detail string) *APIError { return &APIError{Status: status, Detail: detail} }

// fieldError mirrors one entry of FastAPI's 422 {"detail": [...]} payload.
type fieldError struct {
	Type string `json:"type"`
	Loc  []any  `json:"loc"`
	Msg  string `json:"msg"`
}

func validationFailed(errs ...fieldError) *APIError {
	return &APIError{Status: http.StatusUnprocessableEntity, Detail: errs}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, `{"detail":"Internal Server Error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(bytes.TrimRight(buf.Bytes(), "\n")) // compact body, like FastAPI's
}

func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// h adapts an error-returning handler: APIErrors become {"detail": …} responses
// and anything else is logged and returned as a generic 500.
func (s *Server) h(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if errors.As(err, &ae) {
		for k, v := range ae.Header {
			w.Header()[k] = v
		}
		writeJSON(w, ae.Status, map[string]any{"detail": ae.Detail})
		return
	}
	if r.Context().Err() != nil { // client went away; nothing useful to send
		return
	}
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "Internal Server Error"})
}
