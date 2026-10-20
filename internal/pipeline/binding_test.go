package pipeline

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/omyo/gonest/internal/core"
)

type Paging struct {
	Sort string `query:"sort" default:"asc"`
}

type upperText string

func (u *upperText) UnmarshalText(b []byte) error {
	if len(b) > 0 && b[0] == '!' {
		return errors.New("bad")
	}
	*u = upperText(strings.ToUpper(string(b)))
	return nil
}

type allIn struct {
	core.In                // ignored marker
	ID       int           `path:"id"`
	Slug     string        `path:"slug"`
	Page     int           `query:"page" default:"1"`
	Limit    *int          `query:"limit"`
	Tags     []string      `query:"tag"`
	IDs      []int64       `query:"ids"`
	Active   bool          `query:"active"`
	Ratio    float64       `query:"ratio"`
	Small    uint8         `query:"small"`
	Since    time.Time     `query:"since"`
	Timeout  time.Duration `query:"timeout" default:"5s"`
	Opt      *string       `query:"opt"`
	PtrTimes []*int        `query:"pt"`
	Code     upperText     `query:"code"`
	Tenant   string        `header:"X-Tenant"`
	Langs    []string      `header:"accept-language"`
	F32      float32       `query:"f32"`
	I8       int8          `query:"i8"`
	Paging
	hidden   int //nolint:unused // proves unexported fields are skipped
	Untagged string
}

func TestBindingSources(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/items/:id/:slug", echo[allIn]())
	})
	seven := 7
	optV := ""
	since := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	one, two := 1, 2
	tests := []struct {
		name    string
		target  string
		headers [][2]string
		want    func(*allIn)
	}{
		{
			name:   "defaults only",
			target: "/items/3/abc",
			want:   func(w *allIn) {},
		},
		{
			name:   "every query type",
			target: "/items/3/abc?page=4&limit=7&tag=a&tag=b,c&ids=1,2&active=true&ratio=1.5&small=255&since=2024-05-06T07:08:09Z&timeout=1m&opt=&pt=1,2&code=xy&f32=2.5&i8=-3&sort=desc",
			want: func(w *allIn) {
				w.Page, w.Limit, w.Tags, w.IDs = 4, &seven, []string{"a", "b", "c"}, []int64{1, 2}
				w.Active, w.Ratio, w.Small, w.Since = true, 1.5, 255, since
				w.Timeout, w.Opt, w.PtrTimes, w.Code = time.Minute, &optV, []*int{&one, &two}, "XY"
				w.F32, w.I8, w.Sort = 2.5, -3, "desc"
			},
		},
		{
			name:   "empty non-string value counts as missing",
			target: "/items/3/abc?page=&limit=&tag=&timeout=",
			want:   func(w *allIn) {},
		},
		{
			name:    "headers",
			target:  "/items/3/abc",
			headers: [][2]string{{"x-tenant", "acme"}, {"Accept-Language", "en, it"}, {"Accept-Language", "fr"}},
			want:    func(w *allIn) { w.Tenant, w.Langs = "acme", []string{"en", "it", "fr"} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []reqOpt
			for _, kv := range tt.headers {
				opts = append(opts, withHeader(kv[0], kv[1]))
			}
			rec := h.do(http.MethodGet, tt.target, "", opts...)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			got := decodeJSONBody[allIn](t, rec)
			want := allIn{ID: 3, Slug: "abc", Page: 1, Timeout: 5 * time.Second, Paging: Paging{Sort: "asc"}}
			tt.want(&want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestBindingTypeErrors(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/items/:id/:slug", echo[allIn]())
	})
	tests := []struct {
		target string
		fields []string
	}{
		{"/items/x/abc", []string{"path:id"}},
		{"/items/1/abc?page=abc&active=maybe", []string{"query:page", "query:active"}},
		{"/items/1/abc?small=256&ratio=x&since=yesterday&timeout=5", []string{"query:ratio", "query:small", "query:since", "query:timeout"}},
		{"/items/1/abc?ids=1,x&pt=1,y&code=!&limit=z", []string{"query:limit", "query:ids", "query:pt", "query:code"}},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := h.do(http.MethodGet, tt.target, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			eb := decodeError(t, rec)
			if eb.Message != "validation failed" || eb.Error != "Bad Request" || eb.StatusCode != 400 {
				t.Fatalf("body %+v", eb)
			}
			var got []string
			for _, f := range eb.Details {
				if f.Rule != "type" || f.Message == "" || f.Param == "" {
					t.Errorf("field error %+v", f)
				}
				got = append(got, f.Source+":"+f.Field)
			}
			if !reflect.DeepEqual(got, tt.fields) {
				t.Fatalf("fields %v, want %v", got, tt.fields)
			}
		})
	}
}

type address struct {
	Street string `json:"street" validate:"required"`
	Zip    int    `json:"zip"`
}

type item struct {
	Name string `json:"name" validate:"required"`
}

type createIn struct {
	OrgID    int       `path:"org"`
	DryRun   bool      `query:"dryRun"`
	Name     string    `json:"name" validate:"required,min=3"`
	Email    string    `json:"email,omitempty" validate:"omitempty,email"`
	Age      int       `json:"age" validate:"gte=0,lte=150"`
	Address  *address  `json:"address"`
	Items    []item    `json:"items" validate:"dive"`
	Role     string    `json:"role" validate:"omitempty,oneof=admin user"`
	Untagged string    // never bound from the body
	Skipped  string    `json:"-"`
	Labels   []string  `json:"labels" validate:"max=2"`
	When     time.Time `json:"when"`
}

func TestJSONBody(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.POST("/orgs/:org/users", echo[createIn]())
	})
	tests := []struct {
		name    string
		body    string
		status  int
		check   func(t *testing.T, rec *httptest.ResponseRecorder)
		message string
		fields  []string // source:field:rule
	}{
		{
			name:   "valid body, path wins over body keys",
			body:   `{"name":"alice","OrgID":99,"Untagged":"x","Skipped":"y","address":{"street":"main","zip":1},"items":[{"name":"a"}]}`,
			status: 201,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				got := decodeJSONBody[map[string]any](t, rec)
				if got["OrgID"] != float64(5) || got["Untagged"] != "" || got["name"] != "alice" || got["DryRun"] != true {
					t.Fatalf("got %v", got)
				}
			},
		},
		{
			name:    "validation messages use client names",
			body:    `{"name":"al","email":"nope","age":200,"address":{},"items":[{"name":"ok"},{}],"role":"root","labels":["a","b","c"]}`,
			status:  400,
			message: "validation failed",
			fields: []string{
				"body:name:min", "body:email:email", "body:age:lte", "body:address.street:required",
				"body:items[1].name:required", "body:role:oneof", "body:labels:max",
			},
		},
		{
			name:    "empty body is not decoded, then required fails",
			body:    "",
			status:  400,
			message: "validation failed",
			fields:  []string{"body:name:required"},
		},
		{name: "malformed JSON", body: `{"name":`, status: 400, message: "invalid JSON body"},
		{name: "trailing data", body: `{"name":"alice"} {}`, status: 400, message: "invalid JSON body"},
		{
			name: "type mismatch", body: `{"name":"alice","age":"old"}`, status: 400, message: "validation failed",
			fields: []string{"body:age:type"},
		},
		{
			name: "nested type mismatch", body: `{"name":"alice","address":{"street":"s","zip":"x"}}`, status: 400, message: "validation failed",
			fields: []string{"body:address.zip:type"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/orgs/5/users?dryRun=1", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if tt.check != nil {
				tt.check(t, rec)
			}
			if tt.message == "" {
				return
			}
			eb := decodeError(t, rec)
			if eb.Message != tt.message {
				t.Fatalf("message %q, want %q", eb.Message, tt.message)
			}
			var got []string
			for _, f := range eb.Details {
				if f.Message == "" {
					t.Errorf("empty message in %+v", f)
				}
				got = append(got, f.Source+":"+f.Field+":"+f.Rule)
			}
			if len(tt.fields) > 0 && !reflect.DeepEqual(got, tt.fields) {
				t.Fatalf("fields\n got %v\nwant %v", got, tt.fields)
			}
		})
	}
}

type validatedQuery struct {
	Page   int    `query:"page" validate:"min=1"`
	Q      string `query:"q" validate:"required,min=2"`
	Tenant string `header:"X-Tenant" validate:"required"`
	ID     string `path:"id" validate:"uuid"`
	Free   int    `validate:"max=0"`
	Paging `validate:"-"`
}

func TestValidationNamesAndMessages(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) { r.GET("/v/:id", echo[validatedQuery]()) })
	rec := h.do(http.MethodGet, "/v/zzz?page=0&q=a", "")
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
	eb := decodeError(t, rec)
	want := []core.FieldError{
		{Field: "page", Source: "query", Rule: "min", Param: "1", Message: "must be at least 1"},
		{Field: "q", Source: "query", Rule: "min", Param: "2", Message: "must be at least 2 characters long"},
		{Field: "X-Tenant", Source: "header", Rule: "required", Message: "is required"},
		{Field: "id", Source: "path", Rule: "uuid", Message: "must be a valid UUID"},
	}
	if !reflect.DeepEqual(eb.Details, want) {
		t.Fatalf("details\n got %+v\nwant %+v", eb.Details, want)
	}
	rec = h.do(http.MethodGet, "/v/6ba7b810-9dad-11d1-80b4-00c04fd430c8?page=2&q=ab", "", withHeader("X-Tenant", "t"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

type formIn struct {
	Name  string                  `form:"name" validate:"required"`
	Tags  []string                `form:"tag"`
	Count int                     `form:"count" default:"2"`
	File  *multipart.FileHeader   `form:"file"`
	Files []*multipart.FileHeader `form:"files"`
}

type formOut struct {
	Name      string
	Tags      []string
	Count     int
	FileName  string
	FileCount int
}

func TestFormBinding(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.POST("/upload", core.Handle(func(_ *core.Ctx, in formIn) (formOut, error) {
			o := formOut{Name: in.Name, Tags: in.Tags, Count: in.Count, FileCount: len(in.Files)}
			if in.File != nil {
				o.FileName = in.File.Filename
			}
			return o, nil
		}))
	})

	t.Run("urlencoded", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(url.Values{"name": {"n"}, "tag": {"a,b", "c"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.engine.ServeHTTP(rec, req)
		got := decodeJSONBody[formOut](t, rec)
		want := formOut{Name: "n", Tags: []string{"a", "b", "c"}, Count: 2}
		if rec.Code != 201 || !reflect.DeepEqual(got, want) {
			t.Fatalf("%d %+v", rec.Code, got)
		}
	})

	t.Run("multipart with files", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("name", "n")
		_ = w.WriteField("count", "9")
		for _, f := range []struct{ field, name string }{{"file", "a.txt"}, {"files", "b.txt"}, {"files", "c.txt"}} {
			fw, _ := w.CreateFormFile(f.field, f.name)
			_, _ = fw.Write([]byte("data"))
		}
		_ = w.Close()
		req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		h.engine.ServeHTTP(rec, req)
		got := decodeJSONBody[formOut](t, rec)
		want := formOut{Name: "n", Count: 9, FileName: "a.txt", FileCount: 2}
		if rec.Code != 201 || !reflect.DeepEqual(got, want) {
			t.Fatalf("%d %+v", rec.Code, got)
		}
	})

	t.Run("missing required", func(t *testing.T) {
		rec := h.do(http.MethodPost, "/upload", "")
		eb := decodeError(t, rec)
		if rec.Code != 400 || len(eb.Details) != 1 || eb.Details[0].Field != "name" || eb.Details[0].Source != "form" {
			t.Fatalf("%d %+v", rec.Code, eb)
		}
	})
}

type ptrIn struct {
	ID int `path:"id"`
}

func TestPointerAndEmptyInputs(t *testing.T) {
	h := newHarness(t, "")
	var seen []*ptrIn
	h.mount(func(r core.Router) {
		r.GET("/p/:id", core.Handle(func(_ *core.Ctx, in *ptrIn) (int, error) {
			seen = append(seen, in)
			return in.ID, nil
		}))
		r.GET("/empty", core.Handle(func(_ *core.Ctx, _ core.Empty) (string, error) { return "ok", nil }))
		r.GET("/anon", core.Handle(func(_ *core.Ctx, _ struct{}) (string, error) { return "ok", nil }))
		r.GET("/ptrempty", core.Handle(func(_ *core.Ctx, in *core.Empty) (bool, error) { return in != nil, nil }))
	})
	for i, target := range []string{"/p/4", "/p/5"} {
		rec := h.do(http.MethodGet, target, "")
		if rec.Code != 200 || rec.Body.String() != []string{"4", "5"}[i] {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
	if seen[0] == seen[1] {
		t.Fatal("pointer inputs must be fresh per request")
	}
	for _, target := range []string{"/empty", "/anon"} {
		if rec := h.do(http.MethodGet, target, ""); rec.Code != 200 || rec.Body.String() != `"ok"` {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
	if rec := h.do(http.MethodGet, "/ptrempty", ""); rec.Body.String() != "true" {
		t.Fatalf("ptrempty: %s", rec.Body)
	}
}

func TestCompileBinderErrors(t *testing.T) {
	type twoTags struct {
		A int `query:"a" header:"a"`
	}
	type badType struct {
		M map[string]int `query:"m"`
	}
	type badSliceElem struct {
		M [][]int `query:"m"`
	}
	type pathSlice struct {
		IDs []int `path:"ids"`
	}
	type badDefault struct {
		N int `query:"n" default:"x"`
	}
	type pathDefault struct {
		N int `path:"n" default:"1"`
	}
	type embPtr struct {
		*Paging
	}
	type mixed struct {
		A string `json:"a"`
		B string `form:"b"`
	}
	type ptrPtr struct {
		P **int `query:"p"`
	}
	type nested struct {
		Inner struct {
			X int `query:"x"`
		} `query:"inner"`
	}
	tests := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"non struct", reflect.TypeFor[int](), "must be a struct"},
		{"pointer to non struct", reflect.TypeFor[*int](), "must be a struct"},
		{"two source tags", reflect.TypeFor[twoTags](), "more than one source tag"},
		{"map field", reflect.TypeFor[badType](), "unsupported type"},
		{"slice of slices", reflect.TypeFor[badSliceElem](), "unsupported type"},
		{"path slice", reflect.TypeFor[pathSlice](), "cannot be slices"},
		{"bad default", reflect.TypeFor[badDefault](), "invalid default"},
		{"path default", reflect.TypeFor[pathDefault](), "default is only supported"},
		{"embedded pointer", reflect.TypeFor[embPtr](), "embedded pointer"},
		{"json and form", reflect.TypeFor[mixed](), "cannot mix json and form"},
		{"pointer to pointer", reflect.TypeFor[ptrPtr](), "unsupported type"},
		{"struct query", reflect.TypeFor[nested](), "unsupported type"},
	}
	v := newValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileBinder(tt.typ, v)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBinderCachedByType(t *testing.T) {
	h := newHarness(t, "")
	h.mount(func(r core.Router) {
		r.GET("/a/:id", echo[ptrIn]())
		r.GET("/b/:id", echo[ptrIn]())
	})
	if len(h.m.binders) != 1 {
		t.Fatalf("binders = %d, want 1", len(h.m.binders))
	}
}

func TestRuleMessages(t *testing.T) {
	type in struct {
		S    string   `json:"s" validate:"len=2"`
		L    []int    `json:"l" validate:"min=2"`
		N    int      `json:"n" validate:"gt=5"`
		N2   int      `json:"n2" validate:"lt=-1"`
		N3   int      `json:"n3" validate:"eq=1"`
		N4   int      `json:"n4" validate:"ne=0"`
		N5   int      `json:"n5" validate:"gte=1"`
		U    string   `json:"u" validate:"url"`
		U2   string   `json:"u2" validate:"uri"`
		A    string   `json:"a" validate:"alpha"`
		AN   string   `json:"an" validate:"alphanum"`
		Num  string   `json:"num" validate:"numeric"`
		B    string   `json:"b" validate:"boolean"`
		D    string   `json:"d" validate:"datetime=2006-01-02"`
		C    string   `json:"c" validate:"contains=x"`
		Low  string   `json:"low" validate:"lowercase"`
		Many []string `json:"many" validate:"max=1"`
		Ln   []int    `json:"ln" validate:"len=1"`
	}
	h := newHarness(t, "")
	h.mount(func(r core.Router) { r.POST("/", echo[in]()) })
	rec := h.do(http.MethodPost, "/", `{"s":"abc","n4":0,"u":"x","u2":"x","a":"1","an":"-","num":"x","b":"x","d":"x","c":"y","low":"A","many":["a","b"],"l":[1]}`)
	eb := decodeError(t, rec)
	got := map[string]string{}
	for _, f := range eb.Details {
		got[f.Field] = f.Message
	}
	want := map[string]string{
		"s":    "must be exactly 2 characters long",
		"l":    "must contain at least 2 items",
		"n":    "must be greater than 5",
		"n2":   "must be less than -1",
		"n3":   "must be equal to 1",
		"n4":   "must not be equal to 0",
		"n5":   "must be greater than or equal to 1",
		"u":    "must be a valid URL",
		"u2":   "must be a valid URI",
		"a":    "must contain only letters",
		"an":   "must contain only letters and digits",
		"num":  "must be numeric",
		"b":    "must be a boolean",
		"d":    "must be a date-time in the format 2006-01-02",
		"c":    `failed the "contains" rule (x)`,
		"low":  `failed the "lowercase" rule`,
		"many": "must contain at most 1 items",
		"ln":   "must contain exactly 1 items",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages\n got %v\nwant %v", got, want)
	}
}
