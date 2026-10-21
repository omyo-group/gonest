package openapi_test

import (
	"testing"
	"time"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/openapi"
)

type durationIn struct {
	Wait   time.Duration   `query:"wait" default:"1s" example:"2m"`
	Steps  []time.Duration `query:"steps"`
	Header *time.Duration  `header:"X-Budget"`
	Body   time.Duration   `json:"body" example:"5s"`
	Opt    *time.Duration  `json:"opt"`
	List   []time.Duration `json:"list"`
	Docced time.Duration   `json:"docced" doc:"Retry delay."`
	Nanos  time.Duration   `json:"nanos" example:"1500"`
	Odd    time.Duration   `json:"odd" example:"soon"`
}

type durationQuery struct {
	Ptrs []*time.Duration `query:"ptrs"`
}

type durationOut struct {
	Elapsed time.Duration `json:"elapsed"`
}

type durationCtrl struct{}

func (durationCtrl) Routes(gonest.Router) {}

func (durationCtrl) Run(*gonest.Ctx, durationIn) (durationOut, error) { return durationOut{}, nil }

func (durationCtrl) Query(*gonest.Ctx, durationQuery) (durationOut, error) { return durationOut{}, nil }

// encoding/json writes time.Duration as integer nanoseconds, while the binder
// parses path/query/header/form durations with time.ParseDuration.
func TestDurationSchemas(t *testing.T) {
	c := durationCtrl{}
	doc, err := openapi.Generate([]*gonest.RouteInfo{
		route("POST", "/d", c, gonest.Handle(c.Run), gonest.RouteConfig{}, gonest.RouteConfig{}),
		route("GET", "/q", c, gonest.Handle(c.Query), gonest.RouteConfig{}, gonest.RouteConfig{}),
	}, openapi.Info{})
	if err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/d"].Post
	params := map[string]openapi.Parameter{}
	for _, p := range op.Parameters {
		params[p.Name] = p
	}
	if s := params["wait"].Schema; s.Type[0] != "string" || s.Default != "1s" || params["wait"].Example != "2m" {
		t.Errorf("query duration: %+v example=%v", s, params["wait"].Example)
	}
	if s := params["steps"].Schema; s.Type[0] != "array" || s.Items.Type[0] != "string" {
		t.Errorf("query duration slice: %+v", s.Items)
	}
	if s := params["X-Budget"].Schema; s.Type[0] != "string" || s.Format != "" {
		t.Errorf("header duration: %+v", s)
	}
	comps := doc.Components.Schemas
	body := comps["openapi_test.durationInBody"]
	if body == nil {
		t.Fatalf("components: %v", keys(comps))
	}
	if s := body.Properties["body"]; s.Type[0] != "integer" || s.Format != "int64" || s.Example != int64(5*time.Second) {
		t.Errorf("json duration: %+v", s)
	}
	if s := body.Properties["opt"]; len(s.Type) != 2 || s.Type[0] != "integer" || s.Type[1] != "null" {
		t.Errorf("json *duration: %+v", s)
	}
	if s := body.Properties["list"]; s.Items.Type[0] != "integer" {
		t.Errorf("json duration slice: %+v", s.Items)
	}
	if s := body.Properties["docced"]; s.Description != "Retry delay." || s.Type[0] != "integer" {
		t.Errorf("documented duration: %+v", s)
	}
	if s := body.Properties["nanos"]; s.Example != int64(1500) {
		t.Errorf("integer example: %+v", s)
	}
	if s := body.Properties["odd"]; s.Example != "soon" {
		t.Errorf("unparsable example is kept verbatim: %+v", s)
	}
	if s := doc.Paths["/q"].Get.Parameters[0].Schema; s.Items.Type[0] != "string" {
		t.Errorf("query []*duration: %+v", s.Items)
	}
	if s := comps["openapi_test.durationOut"].Properties["elapsed"]; s.Type[0] != "integer" {
		t.Errorf("output duration: %+v", s)
	}
}
