package gonest_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/omyo-group/gonest"
)

type formIn struct {
	Name string                `form:"name"`
	File *multipart.FileHeader `form:"file"`
}

type formOut struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type formCtrl struct{}

func newFormCtrl() *formCtrl { return &formCtrl{} }

func (c *formCtrl) Routes(r gonest.Router) {
	r.POST("/form", gonest.Handle(c.Submit))
	r.GET("/ip", gonest.Handle(c.IP))
}

func (c *formCtrl) Submit(_ *gonest.Ctx, in formIn) (formOut, error) {
	out := formOut{Name: in.Name}
	if in.File != nil {
		out.Size = in.File.Size
	}
	return out, nil
}

func (c *formCtrl) IP(ctx *gonest.Ctx, _ struct{}) (string, error) { return ctx.Gin().ClientIP(), nil }

func multipartBody(t *testing.T, fileSize int) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("name", "n"); err != nil {
		t.Fatal(err)
	}
	fw, err := w.CreateFormFile("file", "f.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte("x"), fileSize)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestFormBodyLimits(t *testing.T) {
	urlencoded := "application/x-www-form-urlencoded"
	small := url.Values{"name": {"n"}}.Encode()
	big := url.Values{"name": {strings.Repeat("x", 2048)}}.Encode()
	mpSmall, mpCT := multipartBody(t, 100)
	mpBig, mpBigCT := multipartBody(t, 4096)
	cases := []struct {
		name string
		opts []gonest.Option
		body string
		ct   string
		want int
	}{
		{"urlencoded under default", nil, small, urlencoded, http.StatusCreated},
		{"urlencoded over body limit", []gonest.Option{gonest.WithBodyLimit(1024)}, big, urlencoded, http.StatusRequestEntityTooLarge},
		{"urlencoded limit disabled", []gonest.Option{gonest.WithBodyLimit(0)}, big, urlencoded, http.StatusCreated},
		{"multipart under default", nil, mpSmall.String(), mpCT, http.StatusCreated},
		{"multipart ignores body limit", []gonest.Option{gonest.WithBodyLimit(1024)}, mpBig.String(), mpBigCT, http.StatusCreated},
		{"multipart over multipart limit", []gonest.Option{gonest.WithMultipartLimit(1024)}, mpBig.String(), mpBigCT, http.StatusRequestEntityTooLarge},
		{"multipart limit disabled", []gonest.Option{gonest.WithMultipartLimit(0)}, mpBig.String(), mpBigCT, http.StatusCreated},
		{"malformed multipart", nil, "--nope\r\n", "multipart/form-data; boundary=b", http.StatusBadRequest},
		{"multipart without boundary", nil, "x", "multipart/form-data", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]gonest.Option{gonest.WithoutStartupLog()}, tc.opts...)
			app, err := gonest.New(&gonest.Module{Name: "form", Controllers: []any{newFormCtrl}}, opts...)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/form?bad=%zz", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.ct)
			app.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestTrustedProxies(t *testing.T) {
	cases := []struct {
		name   string
		opts   []gonest.Option
		remote string
		want   string
	}{
		{"default ignores forwarding headers", nil, "10.0.0.1:1234", "10.0.0.1"},
		{"trusted proxy forwards", []gonest.Option{gonest.WithTrustedProxies("10.0.0.0/8")}, "10.0.0.1:1234", "203.0.113.7"},
		{"untrusted peer ignored", []gonest.Option{gonest.WithTrustedProxies("10.0.0.0/8")}, "192.0.2.1:1234", "192.0.2.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]gonest.Option{gonest.WithoutStartupLog()}, tc.opts...)
			app, err := gonest.New(&gonest.Module{Name: "ip", Controllers: []any{newFormCtrl}}, opts...)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			app.Handler().ServeHTTP(rec, req)
			if got := strings.Trim(rec.Body.String(), "\"\n"); got != tc.want {
				t.Fatalf("client IP %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("invalid proxy", func(t *testing.T) {
		_, err := gonest.New(&gonest.Module{Name: "ip"}, gonest.WithoutStartupLog(), gonest.WithTrustedProxies("not-an-ip"))
		if err == nil || !strings.Contains(err.Error(), "trusted proxies") {
			t.Fatalf("want trusted proxies error, got %v", err)
		}
	})
}
