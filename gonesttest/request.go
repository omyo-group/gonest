package gonesttest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Request is an HTTP request under construction. Build it with App.Request.
type Request struct {
	app         *App
	method      string
	path        string
	header      http.Header
	query       url.Values
	body        io.Reader
	contentType string
}

// Request starts a request against the application. path may contain a query
// string.
func (a *App) Request(method, path string) *Request {
	return &Request{app: a, method: method, path: path, header: http.Header{}, query: url.Values{}}
}

// WithHeader adds a request header.
func (r *Request) WithHeader(key, value string) *Request {
	r.header.Add(key, value)
	return r
}

// WithQuery adds a query parameter.
func (r *Request) WithQuery(key, value string) *Request {
	r.query.Add(key, value)
	return r
}

// WithBearer sets the Authorization header to "Bearer token".
func (r *Request) WithBearer(token string) *Request {
	r.header.Set("Authorization", "Bearer "+token)
	return r
}

// WithJSON sets the body to the JSON encoding of v and the content type to
// application/json. It fails the test if v cannot be encoded.
func (r *Request) WithJSON(v any) *Request {
	r.app.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		r.app.t.Fatalf("gonesttest: WithJSON: %v", err)
	}
	return r.WithBody(bytes.NewReader(b), "application/json")
}

// WithBody sets the raw body and its content type (empty means unset).
func (r *Request) WithBody(body io.Reader, contentType string) *Request {
	r.body = body
	r.contentType = contentType
	return r
}

// Do serves the request through the application's handler.
func (r *Request) Do() *Response {
	t := r.app.t
	t.Helper()
	target := r.path
	if len(r.query) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + r.query.Encode()
	}
	req, err := http.NewRequest(r.method, target, r.body)
	if err != nil {
		t.Fatalf("gonesttest: bad request %s %s: %v", r.method, target, err)
	}
	for k, vs := range r.header {
		req.Header[k] = append([]string(nil), vs...)
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	rec := httptest.NewRecorder()
	r.app.Handler().ServeHTTP(rec, req)
	return newResponse(t, rec)
}
