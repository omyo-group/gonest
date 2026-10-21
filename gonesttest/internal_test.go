package gonesttest

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/internal/core"
)

// fakeT records helper failures. Fatalf stops the helper by panicking with
// itself, like runtime.Goexit would.
type fakeT struct {
	testing.TB
	failed bool
}

func (f *fakeT) Helper() {}
func (f *fakeT) Fatalf(string, ...any) {
	f.failed = true
	panic(f)
}
func (f *fakeT) Fatal(...any) { f.Fatalf("") }

// mustFail runs fn and requires that it fails the (fake) test.
func mustFail(t *testing.T, fn func(ft *fakeT)) {
	t.Helper()
	ft := &fakeT{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil && r != ft {
				panic(r)
			}
		}()
		fn(ft)
	}()
	if !ft.failed {
		t.Fatal("expected the helper to fail the test")
	}
}

func okModule() *gonest.Module {
	return &gonest.Module{Name: "ok", Providers: []gonest.Provider{gonest.Value[int](1)}}
}

func TestCompileFailures(t *testing.T) {
	cases := []struct {
		name string
		run  func(ft *fakeT)
	}{
		{"nil root", func(ft *fakeT) { New(ft, nil).Compile() }},
		{"unused provider override", func(ft *fakeT) {
			New(ft, okModule()).OverrideProvider(gonest.Value[float64](1)).Compile()
		}},
		{"unused module override", func(ft *fakeT) {
			New(ft, okModule()).OverrideModule(&gonest.Module{Name: "x"}, &gonest.Module{Name: "y"}).Compile()
		}},
		{"gonest.New error", func(ft *fakeT) { New(ft, &gonest.Module{}).Compile() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { mustFail(t, tc.run) })
	}
}

func TestGetFailures(t *testing.T) {
	app := New(t, okModule()).Compile()
	mustFail(t, func(ft *fakeT) { Get[string](&App{App: app.App, t: ft}) })
	mustFail(t, func(ft *fakeT) { GetNamed[int](&App{App: app.App, t: ft}, "nope") })
}

func TestRequestFailures(t *testing.T) {
	app := New(t, okModule()).Compile()
	mustFail(t, func(ft *fakeT) { (&App{App: app.App, t: ft}).Request("GET", "/").WithJSON(make(chan int)) })
	mustFail(t, func(ft *fakeT) { (&App{App: app.App, t: ft}).Request("BAD METHOD", "/").Do() })
}

func recorded(code int, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(code)
	_, _ = rec.WriteString(body)
	return rec
}

func TestResponseFailures(t *testing.T) {
	cases := []struct {
		name string
		rec  *httptest.ResponseRecorder
		fn   func(r *Response)
	}{
		{"status", recorded(200, `{}`), func(r *Response) { r.ExpectStatus(201) }},
		{"json mismatch", recorded(200, `{"a":1}`), func(r *Response) { r.ExpectJSON(map[string]int{"a": 2}) }},
		{"json unencodable", recorded(200, `{}`), func(r *Response) { r.ExpectJSON(make(chan int)) }},
		{"body not json", recorded(200, `nope`), func(r *Response) { r.ExpectJSON(1) }},
		{"header", recorded(200, `{}`), func(r *Response) { r.ExpectHeader("X-Nope", "1") }},
		{"message", recorded(400, `{"message":"bad"}`), func(r *Response) { r.ExpectErrorMessage("zzz") }},
		{"message with details", recorded(400, `{"message":"bad","details":[{"field":"a"}]}`), func(r *Response) { r.ExpectErrorMessage("zzz") }},
		{"message not an error body", recorded(200, `nope`), func(r *Response) { r.ExpectErrorMessage("x") }},
		{"json decode", recorded(200, `{"a":"s"}`), func(r *Response) { var v struct{ A int }; r.JSON(&v) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustFail(t, func(ft *fakeT) { tc.fn(newResponse(ft, tc.rec)) })
		})
	}
	// Details are searched when the message does not match.
	newResponse(t, recorded(400, `{"message":"bad","details":[{"field":"name"}]}`)).ExpectErrorMessage("name")
}

func TestNewCtxFailures(t *testing.T) {
	mustFail(t, func(ft *fakeT) { NewCtx(ft, CtxMethod("BAD METHOD")) })
	mustFail(t, func(ft *fakeT) { NewCtx(ft, CtxJSON(make(chan int))) })
}

func TestRecorderForeignCtx(t *testing.T) {
	g, _ := gin.CreateTestContext(httptest.NewRecorder())
	foreign := core.NewCtx(g, nil, nil)
	if Recorder(foreign) != nil {
		t.Fatal("expected nil for a foreign ctx")
	}
	mustFail(t, func(ft *fakeT) { ResponseOf(ft, foreign) })

	ctx := NewCtx(t)
	ctx.Set(recorderKey, "not a recorder")
	if Recorder(ctx) != nil {
		t.Fatal("expected nil for a wrong value")
	}
}
