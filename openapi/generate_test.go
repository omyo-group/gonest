package openapi_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/openapi"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sampleDoc(t *testing.T) *openapi.Document {
	t.Helper()
	doc, err := openapi.Generate(sampleRoutes(), openapi.Info{Title: "Sample", Version: "2.0.0", Description: "Demo"},
		openapi.WithServers(openapi.Server{URL: "https://api.example.com", Description: "prod"}),
		openapi.WithSecurityScheme("bearer", openapi.BearerJWT()),
		openapi.WithSecurityScheme("apiKey", openapi.APIKeyHeader("X-API-Key")),
	)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return doc
}

func TestGoldenSample(t *testing.T) {
	data, err := json.MarshalIndent(sampleDoc(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	golden := filepath.Join("testdata", "sample.json")
	if *update {
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden (run with -update): %v", err)
	}
	if !bytes.Equal(data, want) {
		t.Errorf("document differs from %s; run go test ./openapi -update and review the diff\n%s", golden, data)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	a, _ := json.Marshal(sampleDoc(t))
	for i := 0; i < 5; i++ {
		b, _ := json.Marshal(sampleDoc(t))
		if !bytes.Equal(a, b) {
			t.Fatal("two runs produced different documents")
		}
	}
}

// walk visits every value in a decoded JSON tree.
func walk(v any, fn func(path string, v any)) {
	var rec func(p string, v any)
	rec = func(p string, v any) {
		fn(p, v)
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				rec(p+"/"+k, c)
			}
		case []any:
			for i, c := range x {
				rec(p+"/"+string(rune('0'+i%10)), c)
			}
		}
	}
	rec("", v)
}

// checkStructure verifies that refs resolve, operationIds are unique, path
// parameters are declared and success responses exist.
func checkStructure(t *testing.T, doc *openapi.Document) {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	if tree["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v", tree["openapi"])
	}
	walk(tree, func(p string, v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		r, ok := m["$ref"].(string)
		if !ok {
			return
		}
		name, found := strings.CutPrefix(r, "#/components/schemas/")
		if !found {
			t.Errorf("%s: unexpected ref %q", p, r)
			return
		}
		if doc.Components == nil || doc.Components.Schemas[name] == nil {
			t.Errorf("%s: unresolved ref %q", p, r)
		}
	})
	ids := map[string]bool{}
	for path, item := range doc.Paths {
		ops := map[string]*openapi.Operation{
			"get": item.Get, "put": item.Put, "post": item.Post, "delete": item.Delete,
			"options": item.Options, "head": item.Head, "patch": item.Patch,
		}
		for method, op := range ops {
			if op == nil {
				continue
			}
			if ids[op.OperationID] {
				t.Errorf("duplicate operationId %q", op.OperationID)
			}
			ids[op.OperationID] = true
			if len(op.Responses) == 0 {
				t.Errorf("%s %s has no responses", method, path)
			}
			declared := map[string]bool{}
			for _, p := range op.Parameters {
				if p.In == "path" {
					declared[p.Name] = true
					if !p.Required {
						t.Errorf("%s %s: path param %s not required", method, path, p.Name)
					}
				}
			}
			for _, seg := range strings.Split(path, "/") {
				if strings.HasPrefix(seg, "{") {
					if n := strings.Trim(seg, "{}"); !declared[n] {
						t.Errorf("%s %s: path param %s undeclared", method, path, n)
					}
				}
			}
			if strings.Contains(path, ":") || strings.Contains(path, "*") {
				t.Errorf("path %q still in gin syntax", path)
			}
		}
	}
}

func TestStructure(t *testing.T) { checkStructure(t, sampleDoc(t)) }

func TestSampleDetails(t *testing.T) {
	doc := sampleDoc(t)
	if _, ok := doc.Paths["/api/users/secret"]; ok {
		t.Error("hidden route is documented")
	}
	if got := doc.Paths["/api/users/{id}"].Get.OperationID; got != "UsersController.FindOne" {
		t.Errorf("operationId = %q", got)
	}
	del := doc.Paths["/api/users/{id}"].Delete
	if !del.Deprecated || del.Responses["204"] == nil || del.Responses["204"].Content != nil {
		t.Errorf("delete: %+v", del.Responses["204"])
	}
	create := doc.Paths["/api/users"].Post
	for _, code := range []string{"201", "400", "401", "403", "500"} {
		if create.Responses[code] == nil {
			t.Errorf("create misses %s", code)
		}
	}
	if len(create.Security) != 1 || create.Security[0]["bearer"] == nil {
		t.Errorf("security = %v", create.Security)
	}
	if !create.RequestBody.Required {
		t.Error("request body should be required")
	}
	list := doc.Paths["/api/users"].Get
	if list.Responses["401"] != nil || list.Responses["400"] == nil || list.Responses["500"] == nil {
		t.Errorf("list responses: %v", list.Responses)
	}
	if list.RequestBody != nil {
		t.Error("GET must not have a body")
	}
	// Tags: controller tags, route tags override, first segment fallback.
	if tags := doc.Paths["/api/users/legacy"].Get.Tags; !reflect.DeepEqual(tags, []string{"legacy"}) {
		t.Errorf("legacy tags %v", tags)
	}
	if tags := doc.Paths["/files"].Post.Tags; !reflect.DeepEqual(tags, []string{"files"}) {
		t.Errorf("files tags %v", tags)
	}
	// Raw handler: wildcard parameter declared, no body.
	dl := doc.Paths["/files/{path}"].Get
	if len(dl.Parameters) != 1 || dl.Parameters[0].Name != "path" || dl.Responses["200"].Content != nil {
		t.Errorf("download: %+v", dl)
	}
	// Documented 200 on login overrides the generated description.
	if d := doc.Paths["/login"].Post.Responses["200"].Description; d != "Logged in" {
		t.Errorf("login 200 description %q", d)
	}
	if doc.Paths["/login"].Post.Responses["302"] == nil {
		t.Error("302 missing")
	}
	// Multipart upload.
	up := doc.Paths["/files"].Post.RequestBody
	if up.Content["multipart/form-data"] == nil || !up.Required {
		t.Errorf("upload body %+v", up)
	}
	if doc.Paths["/login"].Post.RequestBody.Content["application/x-www-form-urlencoded"] == nil {
		t.Error("login should be urlencoded")
	}
	// Components.
	for _, n := range []string{"ErrorBody", "openapi_test.UserDTO", "openapi_test.Address", "openapi_test.Node"} {
		if doc.Components.Schemas[n] == nil {
			t.Errorf("component %s missing; have %v", n, keys(doc.Components.Schemas))
		}
	}
	if len(doc.Tags) == 0 || doc.Tags[0].Name != "api" {
		t.Errorf("tags %v", doc.Tags)
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestGenerateErrors(t *testing.T) {
	u := &UsersController{}
	ok := route("GET", "/a", u, gonest.Handle(u.Tree), gonest.RouteConfig{}, gonest.RouteConfig{})
	tests := []struct {
		name   string
		routes []*gonest.RouteInfo
		want   string
	}{
		{"nil route", []*gonest.RouteInfo{nil}, "nil route"},
		{"no handler", []*gonest.RouteInfo{{Method: "GET", Path: "/x"}}, "no handler"},
		{"bad method", []*gonest.RouteInfo{route("TRACE", "/a", u, gonest.Handle(u.Tree), gonest.RouteConfig{}, gonest.RouteConfig{})}, "unsupported method"},
		{"duplicate", []*gonest.RouteInfo{ok, ok}, "duplicate route"},
		{"unknown scheme", []*gonest.RouteInfo{route("GET", "/a", u, gonest.Handle(u.Tree), gonest.RouteConfig{}, gonest.RouteConfig{Security: []string{"nope"}})}, `"nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openapi.Generate(tt.routes, openapi.Info{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestGenerateDefaultsAndEmpty(t *testing.T) {
	doc, err := openapi.Generate(nil, openapi.Info{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Info.Title != "API" || doc.Info.Version != "1.0.0" || doc.Components != nil || len(doc.Paths) != 0 {
		t.Errorf("%+v", doc)
	}
}

func TestOperationIDUniqueness(t *testing.T) {
	u := &UsersController{}
	h := gonest.Handle(u.Tree)
	routes := []*gonest.RouteInfo{
		route("GET", "/a", u, h, gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("POST", "/a", u, h, gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("PATCH", "/b", u, h, gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("OPTIONS", "/b", u, h, gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("HEAD", "/b", u, h, gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("GET", "/:x/*", u, &gonest.Handler{}, gonest.RouteConfig{}, gonest.RouteConfig{}),
	}
	doc, err := openapi.Generate(routes, openapi.Info{})
	if err != nil {
		t.Fatal(err)
	}
	checkStructure(t, doc)
	if _, ok := doc.Paths["/{x}/{wildcard}"]; !ok {
		t.Errorf("paths: %v", keys(doc.Paths))
	}
}

func TestWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := openapi.WriteFile(sampleDoc(t), path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back openapi.Document
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if back.Info.Title != "Sample" || !bytes.HasSuffix(data, []byte("}\n")) || !bytes.Contains(data, []byte("\n  \"")) {
		t.Errorf("unexpected file content")
	}
	if err := openapi.WriteFile(nil, path); err == nil {
		t.Error("nil document must fail")
	}
	if err := openapi.WriteFile(sampleDoc(t), filepath.Join(t.TempDir(), "missing", "x.json")); err == nil {
		t.Error("unwritable path must fail")
	}
}

func TestSecurityHelpers(t *testing.T) {
	b, _ := json.Marshal(openapi.BearerJWT())
	if string(b) != `{"type":"http","scheme":"bearer","bearerFormat":"JWT"}` {
		t.Errorf("bearer: %s", b)
	}
	k, _ := json.Marshal(openapi.APIKeyHeader("X-Key"))
	if string(k) != `{"type":"apiKey","name":"X-Key","in":"header"}` {
		t.Errorf("apiKey: %s", k)
	}
}

func TestSchemaTypeJSON(t *testing.T) {
	for _, tt := range []struct {
		in   openapi.SchemaType
		want string
	}{
		{openapi.SchemaType{"string"}, `"string"`},
		{openapi.SchemaType{"string", "null"}, `["string","null"]`},
	} {
		b, err := json.Marshal(tt.in)
		if err != nil || string(b) != tt.want {
			t.Errorf("marshal %v: %s %v", tt.in, b, err)
		}
		var back openapi.SchemaType
		if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, tt.in) {
			t.Errorf("unmarshal %s: %v %v", b, back, err)
		}
	}
	var s openapi.SchemaType
	if err := json.Unmarshal([]byte(`5`), &s); err == nil {
		t.Error("number must fail")
	}
	if err := json.Unmarshal([]byte(`[5]`), &s); err == nil {
		t.Error("number array must fail")
	}
}
