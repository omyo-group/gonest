package core

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// HTTPError is an error that carries an HTTP status. Handlers, guards, pipes
// and interceptors return it to control the response; the default exception
// filter renders it as:
//
//	{"statusCode": 404, "message": "user 7 not found", "error": "Not Found", "details": ...}
type HTTPError struct {
	// Status is the HTTP status code.
	Status int
	// Message is safe to show to clients.
	Message string
	// Details is optional structured data (for example field errors).
	Details any
	// Err is the wrapped cause, never sent to clients.
	Err error
}

// Error implements error.
func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%d %s: %v", e.Status, e.Message, e.Err)
	}
	return fmt.Sprintf("%d %s", e.Status, e.Message)
}

// Unwrap returns the cause.
func (e *HTTPError) Unwrap() error { return e.Err }

// Wrap sets the cause and returns e, for chaining:
//
//	return nil, gonest.Internal("could not save").Wrap(err)
func (e *HTTPError) Wrap(err error) *HTTPError { e.Err = err; return e }

// ErrorBody is the JSON shape written by the default exception filter.
type ErrorBody struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Error      string `json:"error"`
	Details    any    `json:"details,omitempty"`
}

// Body returns the JSON body for this error.
func (e *HTTPError) Body() ErrorBody {
	return ErrorBody{StatusCode: e.Status, Message: e.Message, Error: http.StatusText(e.Status), Details: e.Details}
}

// NewHTTPError builds an HTTPError. An optional first details value is attached.
func NewHTTPError(status int, message string, details ...any) *HTTPError {
	e := &HTTPError{Status: status, Message: message}
	if len(details) > 0 {
		e.Details = details[0]
	}
	return e
}

// BadRequest returns a 400 error.
func BadRequest(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusBadRequest, message, details...)
}

// Unauthorized returns a 401 error.
func Unauthorized(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusUnauthorized, message, details...)
}

// Forbidden returns a 403 error.
func Forbidden(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusForbidden, message, details...)
}

// NotFound returns a 404 error.
func NotFound(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusNotFound, message, details...)
}

// Conflict returns a 409 error.
func Conflict(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusConflict, message, details...)
}

// UnprocessableEntity returns a 422 error.
func UnprocessableEntity(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusUnprocessableEntity, message, details...)
}

// TooManyRequests returns a 429 error.
func TooManyRequests(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusTooManyRequests, message, details...)
}

// Internal returns a 500 error. Its message is sent to clients, so keep it generic.
func Internal(message string, details ...any) *HTTPError {
	return NewHTTPError(http.StatusInternalServerError, message, details...)
}

// FieldError describes one failed validation rule; ValidationError carries a list.
type FieldError struct {
	// Field is the client-facing name (json/query/path/header name).
	Field string `json:"field"`
	// Source is where the field came from: "body", "query", "path", "header" or "form".
	Source string `json:"source"`
	// Rule is the failed validator tag, e.g. "required" or "min".
	Rule string `json:"rule"`
	// Param is the rule parameter, e.g. "3" for min=3.
	Param string `json:"param,omitempty"`
	// Message is a human readable explanation.
	Message string `json:"message"`
}

// ValidationErrors is returned as Details of a 400 HTTPError when input fails validation.
type ValidationErrors []FieldError

// Error implements error.
func (v ValidationErrors) Error() string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = f.Field + ": " + f.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// ResolveError reports a dependency that could not be built. Path lists the
// chain of tokens from the requested one down to the failing one, e.g.
// UsersController -> *UsersService -> *UsersRepo -> *sql.DB.
type ResolveError struct {
	// Path is the dependency chain, outermost first.
	Path []Token
	// Module is the module from which resolution was attempted, if known.
	Module string
	// Reason is a short explanation: "no provider", "not exported by module x", "cycle", ...
	Reason string
	// Hint is an optional suggestion, e.g. which module provides the type.
	Hint string
	// Err is the underlying error, e.g. one returned by a constructor.
	Err error
}

// Error implements error.
func (e *ResolveError) Error() string {
	var b strings.Builder
	b.WriteString("gonest: cannot resolve ")
	for i, t := range e.Path {
		if i > 0 {
			b.WriteString(" -> ")
		}
		b.WriteString(t.String())
	}
	if e.Module != "" {
		b.WriteString(" in module ")
		b.WriteString(e.Module)
	}
	if e.Reason != "" {
		b.WriteString(": ")
		b.WriteString(e.Reason)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	if e.Hint != "" {
		b.WriteString(" (hint: ")
		b.WriteString(e.Hint)
		b.WriteString(")")
	}
	return b.String()
}

// Unwrap returns the underlying error.
func (e *ResolveError) Unwrap() error { return e.Err }

// ErrNotImplemented is returned by Wave 0 stubs.
var ErrNotImplemented = errors.New("gonest: not implemented")
