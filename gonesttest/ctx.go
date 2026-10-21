package gonesttest

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/internal/core"
)

const recorderKey = "gonesttest.recorder"

type ctxSpec struct {
	method      string
	path        string
	header      http.Header
	query       url.Values
	params      gin.Params
	body        io.Reader
	contentType string
	route       *gonest.RouteInfo
	metadata    map[string]any
	logger      *slog.Logger
	values      map[string]any
}

// CtxOption configures NewCtx.
type CtxOption func(*ctxSpec)

// CtxMethod sets the request method (default GET).
func CtxMethod(m string) CtxOption { return func(s *ctxSpec) { s.method = m } }

// CtxPath sets the request path (default "/"); it may include a query string.
func CtxPath(p string) CtxOption { return func(s *ctxSpec) { s.path = p } }

// CtxHeader adds a request header.
func CtxHeader(k, v string) CtxOption { return func(s *ctxSpec) { s.header.Add(k, v) } }

// CtxQuery adds a query parameter.
func CtxQuery(k, v string) CtxOption { return func(s *ctxSpec) { s.query.Add(k, v) } }

// CtxParam sets a path parameter, as read by Ctx.Param.
func CtxParam(k, v string) CtxOption {
	return func(s *ctxSpec) { s.params = append(s.params, gin.Param{Key: k, Value: v}) }
}

// CtxBody sets the raw request body and its content type.
func CtxBody(body io.Reader, contentType string) CtxOption {
	return func(s *ctxSpec) { s.body, s.contentType = body, contentType }
}

// CtxJSON sets a JSON request body. NewCtx fails the test if v cannot be encoded.
func CtxJSON(v any) CtxOption {
	return func(s *ctxSpec) {
		b, err := json.Marshal(v)
		if err != nil {
			panic("gonesttest: CtxJSON: " + err.Error())
		}
		s.body, s.contentType = bytes.NewReader(b), "application/json"
	}
}

// CtxRoute sets the route info returned by Ctx.Route (copied, not modified).
func CtxRoute(r *gonest.RouteInfo) CtxOption { return func(s *ctxSpec) { s.route = r } }

// CtxMetadata sets route metadata, as if by gonest.SetMetadata on the route.
func CtxMetadata(key string, value any) CtxOption {
	return func(s *ctxSpec) { s.metadata[key] = value }
}

// CtxLogger sets the request logger (default: discards output).
func CtxLogger(l *slog.Logger) CtxOption { return func(s *ctxSpec) { s.logger = l } }

// CtxSet stores a request-scoped value, as Ctx.Set would.
func CtxSet(key string, value any) CtxOption { return func(s *ctxSpec) { s.values[key] = value } }

// NewCtx returns a *gonest.Ctx backed by a gin test context and a response
// recorder, for testing guards, pipes, interceptors and exception filters in
// isolation. Ctx.Route is never nil: it carries the method, the path and the
// metadata given by the options (merged into CtxRoute's, if any).
func NewCtx(t testing.TB, opts ...CtxOption) *gonest.Ctx {
	t.Helper()
	s := &ctxSpec{
		method:   http.MethodGet,
		path:     "/",
		header:   http.Header{},
		query:    url.Values{},
		metadata: map[string]any{},
		values:   map[string]any{},
	}
	for _, o := range opts {
		if o != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%v", r)
					}
				}()
				o(s)
			}()
		}
	}
	target := s.path
	if len(s.query) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + s.query.Encode()
	}
	req, err := http.NewRequest(s.method, target, s.body)
	if err != nil {
		t.Fatalf("gonesttest: NewCtx: bad request %s %s: %v", s.method, target, err)
	}
	for k, vs := range s.header {
		req.Header[k] = append([]string(nil), vs...)
	}
	if s.contentType != "" {
		req.Header.Set("Content-Type", s.contentType)
	}

	rec := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(rec)
	g.Request = req
	g.Params = s.params
	g.Set(recorderKey, rec)
	for k, v := range s.values {
		g.Set(k, v)
	}

	route := &gonest.RouteInfo{Method: s.method, Path: s.path}
	if s.route != nil {
		cp := *s.route
		route = &cp
	}
	if len(s.metadata) > 0 {
		md := make(map[string]any, len(route.Config.Metadata)+len(s.metadata))
		for k, v := range route.Config.Metadata {
			md[k] = v
		}
		for k, v := range s.metadata {
			md[k] = v
		}
		route.Config.Metadata = md
	}
	logger := s.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return core.NewCtx(g, route, logger)
}

// Recorder returns the response recorder behind a Ctx created by NewCtx, to
// inspect what a filter or handler wrote. A status set with the gin writer but
// not yet flushed is flushed first. It returns nil for any other Ctx.
func Recorder(ctx *gonest.Ctx) *httptest.ResponseRecorder {
	v, ok := ctx.Get(recorderKey)
	if !ok {
		return nil
	}
	rec, _ := v.(*httptest.ResponseRecorder)
	if w := ctx.Writer(); rec != nil && !w.Written() && w.Status() != http.StatusOK {
		w.WriteHeaderNow()
	}
	return rec
}

// ResponseOf wraps Recorder(ctx) in a Response with the Expect helpers. It
// fails the test if ctx was not created by NewCtx.
func ResponseOf(t testing.TB, ctx *gonest.Ctx) *Response {
	t.Helper()
	rec := Recorder(ctx)
	if rec == nil {
		t.Fatal("gonesttest: ResponseOf: ctx was not created by NewCtx")
	}
	return newResponse(t, rec)
}
