package openapi_test

import (
	"encoding/json"
	"encoding/xml"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/openapi"
)

type rules struct {
	S      string            `json:"s" validate:"required,min=2,max=9"`
	Exact  string            `json:"exact" validate:"len=4"`
	Gt     string            `json:"gt" validate:"gt=1,lt=5"`
	N      int               `json:"n" validate:"gte=1,lte=10"`
	F      float64           `json:"f" validate:"gt=0.5,lt=9.5"`
	U      uint8             `json:"u"`
	I32    int32             `json:"i32"`
	I8     int8              `json:"i8"`
	F32    float32           `json:"f32"`
	B      bool              `json:"b" default:"true"`
	L      []int             `json:"l" validate:"min=1,max=3,dive,min=1"`
	Arr    [2]string         `json:"arr"`
	Bytes  []byte            `json:"bytes"`
	M      map[string]int    `json:"m" validate:"dive,max=5"`
	Enum   int               `json:"enum" validate:"oneof=1 2 3"`
	Names  string            `json:"names" validate:"oneof='a b' c"`
	Mail   string            `json:"mail" validate:"email"`
	URI    string            `json:"uri" validate:"uri"`
	UUID   string            `json:"uuid" validate:"uuid4"`
	Raw    json.RawMessage   `json:"raw"`
	Any    any               `json:"any"`
	IP     net.IP            `json:"ip"`
	When   time.Time         `json:"when"`
	Ptr    *time.Time        `json:"ptr"`
	Ex     []int             `json:"ex" example:"1,2,3"`
	ExJ    map[string]string `json:"exj" example:"{\"a\":\"b\"}"`
	ExB    bool              `json:"exb" example:"yes"`
	ExArr  []string          `json:"exarr" example:"[\"x\"]"`
	ExU    uint              `json:"exu" example:"3"`
	ExF    float64           `json:"exf" example:"1.5"`
	ExT    time.Duration     `json:"ext" example:"5s"`
	ExP    *int              `json:"exp" example:"4" default:"5"`
	Bad    int               `json:"bad" validate:"min=x" example:"nan"`
	Opt    string            `json:"opt,omitempty" validate:"required"`
	Rename string            `json:"renamed_field"`
	Plain  string
	_      xml.Name
}

type holder struct {
	Rules rules `json:"rules"`
}

func outDoc(t *testing.T, out reflect.Type) *openapi.Document {
	t.Helper()
	h := &gonest.Handler{InType: reflect.TypeFor[gonest.Empty](), OutType: out, Name: "X.Get"}
	r := &gonest.RouteInfo{Method: "GET", Path: "/x", Handler: h}
	doc, err := openapi.Generate([]*gonest.RouteInfo{r}, openapi.Info{})
	if err != nil {
		t.Fatal(err)
	}
	checkStructure(t, doc)
	return doc
}

func TestSchemaRules(t *testing.T) {
	doc := outDoc(t, reflect.TypeFor[holder]())
	s := doc.Components.Schemas["openapi_test.rules"]
	if s == nil {
		t.Fatalf("components: %v", keys(doc.Components.Schemas))
	}
	p := s.Properties
	f := func(n float64) *float64 { return &n }

	if p["s"].MinLength == nil || *p["s"].MinLength != 2 || *p["s"].MaxLength != 9 {
		t.Errorf("s: %+v", p["s"])
	}
	if *p["exact"].MinLength != 4 || *p["exact"].MaxLength != 4 {
		t.Errorf("exact: %+v", p["exact"])
	}
	if *p["gt"].MinLength != 2 || *p["gt"].MaxLength != 4 {
		t.Errorf("gt: %+v", p["gt"])
	}
	if *p["n"].Minimum != 1 || *p["n"].Maximum != 10 {
		t.Errorf("n: %+v", p["n"])
	}
	if *p["f"].ExclusiveMinimum != *f(0.5) || *p["f"].ExclusiveMaximum != 9.5 {
		t.Errorf("f: %+v", p["f"])
	}
	if p["u"].Minimum == nil || *p["u"].Minimum != 0 || p["i32"].Format != "int32" || p["i8"].Format != "" || p["f32"].Format != "float" || p["f"].Format != "double" {
		t.Errorf("number kinds wrong")
	}
	if *p["l"].MinItems != 1 || *p["l"].MaxItems != 3 || *p["l"].Items.Minimum != 1 {
		t.Errorf("l: %+v items %+v", p["l"], p["l"].Items)
	}
	if p["arr"].Items == nil || p["bytes"].Format != "byte" {
		t.Errorf("arr/bytes: %+v %+v", p["arr"], p["bytes"])
	}
	if p["m"].AdditionalProperties == nil || *p["m"].AdditionalProperties.Maximum != 5 {
		t.Errorf("m: %+v", p["m"].AdditionalProperties)
	}
	if !reflect.DeepEqual(p["enum"].Enum, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("enum: %v", p["enum"].Enum)
	}
	if !reflect.DeepEqual(p["names"].Enum, []any{"a b", "c"}) {
		t.Errorf("names: %v", p["names"].Enum)
	}
	if p["mail"].Format != "email" || p["uri"].Format != "uri" || p["uuid"].Format != "uuid" {
		t.Errorf("formats wrong")
	}
	if len(p["raw"].Type) != 0 || len(p["any"].Type) != 0 {
		t.Errorf("raw/any should be untyped")
	}
	if !reflect.DeepEqual(p["ip"].Type, openapi.SchemaType{"string"}) {
		t.Errorf("ip: %+v", p["ip"])
	}
	if p["when"].Format != "date-time" || !reflect.DeepEqual(p["ptr"].Type, openapi.SchemaType{"string", "null"}) {
		t.Errorf("time: %+v %+v", p["when"], p["ptr"])
	}
	if !reflect.DeepEqual(p["ex"].Example, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("ex: %v", p["ex"].Example)
	}
	if !reflect.DeepEqual(p["exj"].Example, map[string]any{"a": "b"}) || p["exb"].Example != "yes" ||
		!reflect.DeepEqual(p["exarr"].Example, []any{"x"}) || p["exu"].Example != uint64(3) ||
		p["exf"].Example != 1.5 || p["ext"].Example != "5s" || p["b"].Default != true {
		t.Errorf("examples wrong: %+v", p)
	}
	if p["exp"].Example != int64(4) || p["exp"].Default != int64(5) {
		t.Errorf("exp: %+v", p["exp"])
	}
	if p["bad"].Minimum != nil || p["bad"].Example != "nan" {
		t.Errorf("bad: %+v", p["bad"])
	}
	if _, ok := p["renamed_field"]; !ok {
		t.Error("renamed_field missing")
	}
	if _, ok := p["Plain"]; !ok {
		t.Error("untagged fields use the Go name, as encoding/json does")
	}
	// required: validate required unless omitempty.
	if !reflect.DeepEqual(s.Required, []string{"s"}) {
		t.Errorf("required: %v", s.Required)
	}
}

type recA struct {
	B *recB `json:"b"`
}
type recB struct {
	A []recA `json:"a"`
}

type both struct {
	ID int `path:"id" json:"-"`
	V  int `json:"v"`
}

func TestRecursionAndNaming(t *testing.T) {
	doc := outDoc(t, reflect.TypeFor[recA]())
	a, b := doc.Components.Schemas["openapi_test.recA"], doc.Components.Schemas["openapi_test.recB"]
	if a == nil || b == nil {
		t.Fatalf("components %v", keys(doc.Components.Schemas))
	}
	if a.Properties["b"].OneOf[0].Ref != "#/components/schemas/openapi_test.recB" {
		t.Errorf("a.b: %+v", a.Properties["b"])
	}
	if b.Properties["a"].Items.Ref != "#/components/schemas/openapi_test.recA" {
		t.Errorf("b.a: %+v", b.Properties["a"])
	}

	doc = outDoc(t, reflect.TypeFor[Page[UserDTO]]())
	if doc.Components.Schemas["openapi_test.Page_openapi_test.UserDTO"] == nil {
		t.Errorf("generic name: %v", keys(doc.Components.Schemas))
	}
	doc = outDoc(t, reflect.TypeFor[Page[*Address]]())
	if doc.Components.Schemas["openapi_test.Page_openapi_test.Address"] == nil {
		t.Errorf("generic ptr name: %v", keys(doc.Components.Schemas))
	}
}

func TestOutputShapes(t *testing.T) {
	for name, tt := range map[string]reflect.Type{
		"slice": reflect.TypeFor[[]UserDTO](), "map": reflect.TypeFor[map[string]*Address](),
		"string": reflect.TypeFor[string](), "bool": reflect.TypeFor[bool](), "iface": reflect.TypeFor[any](),
		"chan": reflect.TypeFor[chan int](), "ptr": reflect.TypeFor[*int](),
	} {
		t.Run(name, func(t *testing.T) {
			doc := outDoc(t, tt)
			if doc.Paths["/x"].Get.Responses["200"].Content["application/json"] == nil {
				t.Error("no json content")
			}
		})
	}
}

func TestNoBodyStatuses(t *testing.T) {
	h := &gonest.Handler{InType: reflect.TypeFor[gonest.Empty](), OutType: reflect.TypeFor[string](), Name: "X.Get"}
	r := &gonest.RouteInfo{Method: "GET", Path: "/x", Handler: h, Config: gonest.RouteConfig{Status: 304}}
	doc, err := openapi.Generate([]*gonest.RouteInfo{r}, openapi.Info{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Paths["/x"].Get.Responses["304"].Content != nil {
		t.Error("304 must have no content")
	}
}

func TestInputSplit(t *testing.T) {
	h := gonest.Handle(func(*gonest.Ctx, *both) (gonest.Empty, error) { return gonest.Empty{}, nil })
	routes := []*gonest.RouteInfo{
		{Method: "PUT", Path: "/m/:id", Handler: h},
		{Method: "PUT", Path: "/n", Handler: h}, // path field not in route: skipped
	}
	doc, err := openapi.Generate(routes, openapi.Info{})
	if err != nil {
		t.Fatal(err)
	}
	checkStructure(t, doc)
	op := doc.Paths["/m/{id}"].Put
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" {
		t.Errorf("params: %+v", op.Parameters)
	}
	if len(doc.Paths["/n"].Put.Parameters) != 0 {
		t.Errorf("params: %+v", doc.Paths["/n"].Put.Parameters)
	}
	body := doc.Components.Schemas["openapi_test.bothBody"]
	if body == nil || len(body.Properties) != 1 {
		t.Errorf("body schema: %+v (%v)", body, keys(doc.Components.Schemas))
	}
	if op.RequestBody.Required {
		t.Error("optional body must not be required")
	}
}

func TestParameterDetails(t *testing.T) {
	doc := sampleDoc(t)
	var byName = map[string]openapi.Parameter{}
	for _, p := range doc.Paths["/api/users"].Get.Parameters {
		byName[p.Name] = p
	}
	page := byName["page"]
	if page.In != "query" || page.Required || page.Description != "Page number" || page.Schema.Default != int64(1) || *page.Schema.Minimum != 1 {
		t.Errorf("page: %+v schema %+v", page, page.Schema)
	}
	if page.Schema.Description != "" {
		t.Error("description must be lifted to the parameter")
	}
	tenant := byName["X-Tenant"]
	if tenant.In != "header" || !tenant.Required {
		t.Errorf("tenant: %+v", tenant)
	}
	roles := byName["role"]
	if roles.Schema.Items == nil || len(roles.Schema.Items.Enum) != 2 || *roles.Schema.MaxItems != 3 {
		t.Errorf("roles: %+v", roles.Schema)
	}
}

func TestSchemaOfNullableAndRefFields(t *testing.T) {
	doc := sampleDoc(t)
	u := doc.Components.Schemas["openapi_test.UserDTO"]
	if u.Properties["address"].OneOf[0].Ref != "#/components/schemas/openapi_test.Address" ||
		!reflect.DeepEqual(u.Properties["address"].OneOf[1].Type, openapi.SchemaType{"null"}) {
		t.Errorf("address: %+v", u.Properties["address"])
	}
	if !reflect.DeepEqual(u.Properties["nickname"].Type, openapi.SchemaType{"string", "null"}) {
		t.Errorf("nickname: %+v", u.Properties["nickname"])
	}
	if u.Properties["id"].Description != "Unique identifier" || u.Properties["id"].Example != int64(7) {
		t.Errorf("id: %+v", u.Properties["id"])
	}
	if _, ok := u.Properties["createdAt"]; !ok {
		t.Error("embedded struct fields must be flattened")
	}
	if _, ok := u.Properties["Secret"]; ok {
		t.Error("json:\"-\" field present")
	}
	if u.Properties["timeout"].Type[0] != "string" {
		t.Errorf("duration: %+v", u.Properties["timeout"])
	}
	if !reflect.DeepEqual(u.Required, []string{"email"}) {
		t.Errorf("required: %v", u.Required)
	}
}
