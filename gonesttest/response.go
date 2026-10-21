package gonesttest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/omyo-group/gonest"
)

// Response is a recorded HTTP response with assertion helpers. Expect methods
// fail the test immediately and return the receiver for chaining.
type Response struct {
	t   testing.TB
	rec *httptest.ResponseRecorder
}

func newResponse(t testing.TB, rec *httptest.ResponseRecorder) *Response {
	return &Response{t: t, rec: rec}
}

// Status returns the HTTP status code.
func (r *Response) Status() int { return r.rec.Code }

// Body returns the response body.
func (r *Response) Body() []byte { return r.rec.Body.Bytes() }

// Header returns the response headers.
func (r *Response) Header() http.Header { return r.rec.Header() }

// JSON decodes the body into v and fails the test on error.
func (r *Response) JSON(v any) {
	r.t.Helper()
	if err := json.Unmarshal(r.Body(), v); err != nil {
		r.t.Fatalf("gonesttest: response body is not the expected JSON: %v\nbody: %s", err, r.Body())
	}
}

// ExpectStatus fails the test, showing the body, unless the status is code.
func (r *Response) ExpectStatus(code int) *Response {
	r.t.Helper()
	if r.Status() != code {
		r.t.Fatalf("gonesttest: expected status %d, got %d\nbody: %s", code, r.Status(), r.Body())
	}
	return r
}

// ExpectJSON fails the test unless the JSON body equals want. Both sides are
// normalised through encoding/json, so want may be a struct, a map or a slice
// and numbers compare by value.
func (r *Response) ExpectJSON(want any) *Response {
	r.t.Helper()
	wb, err := json.Marshal(want)
	if err != nil {
		r.t.Fatalf("gonesttest: ExpectJSON: cannot encode expected value: %v", err)
	}
	var w, g any
	if err := json.Unmarshal(wb, &w); err != nil {
		r.t.Fatalf("gonesttest: ExpectJSON: %v", err)
	}
	if err := json.Unmarshal(r.Body(), &g); err != nil {
		r.t.Fatalf("gonesttest: response body is not JSON: %v\nbody: %s", err, r.Body())
	}
	if !reflect.DeepEqual(w, g) {
		r.t.Fatalf("gonesttest: JSON mismatch\nwant: %s\ngot:  %s", wb, bytes.TrimSpace(r.Body()))
	}
	return r
}

// ExpectHeader fails the test unless the response header key equals value.
func (r *Response) ExpectHeader(key, value string) *Response {
	r.t.Helper()
	if got := r.Header().Get(key); got != value {
		r.t.Fatalf("gonesttest: header %s: want %q, got %q", key, value, got)
	}
	return r
}

// ExpectErrorMessage fails the test unless the body is a gonest.ErrorBody
// whose message, or details (for example validation failures), contain substr.
func (r *Response) ExpectErrorMessage(substr string) *Response {
	r.t.Helper()
	var eb gonest.ErrorBody
	if err := json.Unmarshal(r.Body(), &eb); err != nil {
		r.t.Fatalf("gonesttest: body is not an error body: %v\nbody: %s", err, r.Body())
	}
	if strings.Contains(eb.Message, substr) {
		return r
	}
	if d, err := json.Marshal(eb.Details); err == nil && eb.Details != nil && strings.Contains(string(d), substr) {
		return r
	}
	r.t.Fatalf("gonesttest: error message %q (details %v) does not contain %q", eb.Message, eb.Details, substr)
	return r
}
