package openapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/internal/core"
	"github.com/omyo-group/gonest/openapi"
)

type itemsController struct{}

func (c *itemsController) Routes(r gonest.Router) {
	r.Prefix("/items")
	r.GET("/:id", gonest.Handle(c.Get), gonest.Summary("Get an item"))
	r.POST("", gonest.Handle(c.Create), gonest.Hidden())
}

type itemInput struct {
	ID int `path:"id" validate:"required,min=1"`
}

type createItem struct {
	Name string `json:"name" validate:"required"`
}

type itemDTO struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (c *itemsController) Get(_ *gonest.Ctx, in itemInput) (itemDTO, error) {
	return itemDTO{ID: in.ID, Name: "x"}, nil
}
func (c *itemsController) Create(_ *gonest.Ctx, in createItem) (itemDTO, error) {
	return itemDTO{Name: in.Name}, nil
}

func TestIntegration(t *testing.T) {
	items := &gonest.Module{Name: "items", Controllers: []any{func() *itemsController { return &itemsController{} }}}
	root := &gonest.Module{
		Name: "app",
		Imports: []*gonest.Module{items, openapi.Module(openapi.Options{
			Info:            openapi.Info{Title: "Items", Version: "1.2.3"},
			SecuritySchemes: map[string]openapi.SecurityScheme{"bearer": openapi.BearerJWT()},
		})},
	}
	app, err := gonest.New(root, gonest.WithoutStartupLog())
	if errors.Is(err, core.ErrNotImplemented) {
		t.Skip("gonest.New is not implemented yet")
	}
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/openapi.json")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("/openapi.json: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var doc openapi.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	checkStructure(t, &doc)
	if doc.Info.Title != "Items" {
		t.Errorf("info: %+v", doc.Info)
	}
	if doc.Paths["/items/{id}"] == nil || doc.Paths["/items/{id}"].Get.Summary != "Get an item" {
		t.Errorf("paths: %v", keys(doc.Paths))
	}
	if doc.Paths["/items"] != nil || doc.Paths["/docs"] != nil || doc.Paths["/openapi.json"] != nil {
		t.Errorf("hidden routes leaked: %v", keys(doc.Paths))
	}

	ui := get("/docs")
	if ui.Code != 200 || !strings.Contains(ui.Body.String(), "swagger-ui-bundle.js") {
		t.Fatalf("/docs: %d", ui.Code)
	}
	for path, ctype := range map[string]string{
		"/docs/swagger-ui-bundle.js": "javascript", "/docs/swagger-ui.css": "css",
	} {
		rec := get(path)
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), ctype) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := get("/docs/missing.js"); rec.Code != 404 {
		t.Errorf("missing asset: %d", rec.Code)
	}
}
