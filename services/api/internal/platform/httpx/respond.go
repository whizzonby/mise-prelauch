// Package httpx holds the HTTP conventions shared by every module: the response
// envelope, structured errors, request decoding and middleware.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is an error that is safe to show to API clients.
type Error struct {
	Status  int          `json:"-"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func ValidationError(fields []FieldError) *Error {
	return &Error{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Some fields need attention.",
		Fields:  fields,
	}
}

var (
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "Not found.")
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
	ErrForbidden    = NewError(http.StatusForbidden, "forbidden", "You do not have access to this.")
	ErrRateLimited  = NewError(http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a few minutes.")
)

type meta struct {
	RequestID string `json:"request_id"`
}

type envelope struct {
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
	Meta  meta   `json:"meta"`
}

// JSON writes a success envelope.
func JSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	write(w, status, envelope{Data: data, Meta: meta{RequestID: RequestID(r.Context())}})
}

// Fail writes an error envelope. Anything that is not an *Error is logged and
// reported as a generic internal error, so internals never reach the client.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		slog.ErrorContext(r.Context(), "unhandled error", "error", err.Error())
		apiErr = NewError(http.StatusInternalServerError, "internal_error", "Something went wrong on our side.")
	}
	write(w, apiErr.Status, envelope{Error: apiErr, Meta: meta{RequestID: RequestID(r.Context())}})
}

func write(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Decode reads a JSON request body into dst, rejecting unknown fields, trailing
// data and oversized bodies.
func Decode(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return NewError(http.StatusUnsupportedMediaType, "unsupported_media_type", "Send application/json.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return NewError(http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		}
		return NewError(http.StatusBadRequest, "invalid_json", fmt.Sprintf("Request body is not valid JSON: %s.", jsonProblem(err)))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return NewError(http.StatusBadRequest, "invalid_json", "Request body must contain a single JSON object.")
	}
	return nil
}

func jsonProblem(err error) string {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		return "syntax error"
	case errors.As(err, &typeErr):
		return "wrong type for " + typeErr.Field
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return strings.TrimPrefix(err.Error(), "json: ")
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "body is empty or truncated"
	default:
		return "malformed body"
	}
}
