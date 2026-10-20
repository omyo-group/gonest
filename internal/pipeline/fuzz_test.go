package pipeline

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest/internal/core"
)

type fuzzNested struct {
	Street string   `json:"street" validate:"required,max=32"`
	Zip    int      `json:"zip" validate:"gte=0"`
	Tags   []string `json:"tags" validate:"dive,min=1"`
}

type fuzzQueryIn struct {
	ID     int64            `path:"id" validate:"gt=0"`
	Page   int8             `query:"page" default:"1" validate:"min=1"`
	Size   *uint16          `query:"size"`
	Ratio  float32          `query:"ratio"`
	On     bool             `query:"on"`
	When   time.Time        `query:"when"`
	Wait   time.Duration    `query:"wait" default:"1s"`
	IDs    []int            `query:"ids" validate:"max=8"`
	Names  []string         `query:"name" validate:"dive,alphanum"`
	Addr   netip.Addr       `query:"addr"`
	Opt    *time.Time       `query:"opt"`
	Tenant string           `header:"X-Tenant" validate:"omitempty,uuid"`
	Hops   []uint8          `header:"X-Hops"`
	Mode   string           `query:"mode" validate:"omitempty,oneof=a b"`
	Extra  map[string]int64 `validate:"-"`
}

type fuzzBodyIn struct {
	ID      int               `path:"id"`
	Name    string            `json:"name" validate:"required,min=1,max=64"`
	Email   string            `json:"email" validate:"omitempty,email"`
	Age     *int              `json:"age" validate:"omitempty,lte=150"`
	Address fuzzNested        `json:"address"`
	Items   []fuzzNested      `json:"items" validate:"max=4,dive"`
	Meta    map[string]string `json:"meta" validate:"max=4"`
	When    time.Time         `json:"when"`
}

func fuzzEngine(tb testing.TB) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	m := New(Options{Engine: e, Routes: &core.RouteTable{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := m.Mount(ctrlFunc(func(r core.Router) {
		r.GET("/q/:id", echo[fuzzQueryIn]())
		r.POST("/b/:id", echo[fuzzBodyIn]())
		r.PUT("/b/:id", echo[*fuzzBodyIn]())
	}), "fuzz", nil); err != nil {
		tb.Fatal(err)
	}
	m.InstallFallbacks()
	return e
}

// FuzzBind feeds arbitrary path, query, header and body values to the binder.
// Whatever the input, the answer must be a success or a 4xx with an error
// body: never a 500 (which would mean a panic or an unclassified error).
//
//	go test -run '^$' -fuzz FuzzBind -fuzztime 2m ./internal/pipeline
func FuzzBind(f *testing.F) {
	seeds := []struct{ id, query, header, body string }{
		{"1", "page=2&size=3&ratio=1.5&on=true&when=2024-01-02T03:04:05Z&wait=2m&ids=1,2&name=a&addr=127.0.0.1", "550e8400-e29b-41d4-a716-446655440000", `{"name":"x","address":{"street":"s","zip":1},"items":[{"street":"a","tags":["t"]}]}`},
		{"0", "page=-200&size=70000&ratio=abc&on=maybe", "nope", `{"name":`},
		{"9223372036854775808", "ids=,,,&name=%00&addr=::1%25eth0&opt=", "", `{"name":"x","age":"old"} trailing`},
		{"%2F", "when=0001-01-01T00:00:00Z&wait=-1ns&mode=c", "", `[1,2,3]`},
		{"1", "", "", `null`},
		{"1", "", "", `{"name":"x","meta":{"a":"1","b":"2","c":"3","d":"4","e":"5"},"when":"bad"}`},
	}
	for _, s := range seeds {
		f.Add(s.id, s.query, s.header, s.body)
	}
	e := fuzzEngine(f)
	f.Fuzz(func(t *testing.T, id, query, header, body string) {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
			path := "/q/"
			if method != http.MethodGet {
				path = "/b/"
			}
			u := &url.URL{Path: path + id, RawQuery: query}
			req := httptest.NewRequest(method, "/", strings.NewReader(body))
			req.URL = u
			req.RequestURI = u.RequestURI()
			req.Header["X-Tenant"] = []string{header}
			req.Header["X-Hops"] = []string{header, query}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code >= 500 {
				t.Fatalf("%s %s header=%q body=%q: %d %s", method, u, header, body, rec.Code, rec.Body)
			}
			if rec.Code >= 400 && !strings.Contains(rec.Body.String(), `"statusCode"`) {
				t.Fatalf("%s %s: %d without error body: %s", method, u, rec.Code, rec.Body)
			}
		}
	})
}
