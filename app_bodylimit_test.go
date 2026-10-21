package gonest_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omyo-group/gonest"
)

type bodyIn struct {
	Name string `json:"name"`
}

type bodyCtrl struct{}

func newBodyCtrl() *bodyCtrl { return &bodyCtrl{} }

func (c *bodyCtrl) Routes(r gonest.Router) {
	r.POST("/echo", gonest.Handle(c.Echo))
}

func (c *bodyCtrl) Echo(_ *gonest.Ctx, in bodyIn) (bodyIn, error) { return in, nil }

func TestBodyLimit(t *testing.T) {
	big := `{"name":"` + strings.Repeat("x", 2048) + `"}`
	cases := []struct {
		name string
		opts []gonest.Option
		body string
		want int
	}{
		{"small body under default", nil, `{"name":"ok"}`, http.StatusCreated},
		{"over explicit limit", []gonest.Option{gonest.WithBodyLimit(1024)}, big, http.StatusRequestEntityTooLarge},
		{"limit disabled", []gonest.Option{gonest.WithBodyLimit(0)}, big, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]gonest.Option{gonest.WithoutStartupLog()}, tc.opts...)
			app, err := gonest.New(&gonest.Module{Name: "body", Controllers: []any{newBodyCtrl}}, opts...)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			app.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
