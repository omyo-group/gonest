package guide_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/openapi"
)

func Example_openapiGenerate() {
	app, err := gonest.New(&gonest.Module{Name: "app", Controllers: []any{NewArticlesController}}, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	// docs:openapi-generate
	doc, err := openapi.Generate(app.Routes(), openapi.Info{Title: "Articles", Version: "1.0.0"},
		openapi.WithServers(openapi.Server{URL: "https://api.example.com"}))
	if err != nil {
		fmt.Println(err)
		return
	}
	out := filepath.Join(os.TempDir(), "openapi.json")
	if err := openapi.WriteFile(doc, out); err != nil { // e.g. in CI, for client generation
		fmt.Println(err)
		return
	}
	// docs:end
	defer os.Remove(out)

	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		item := doc.Paths[p]
		for _, op := range []struct {
			m  string
			op *openapi.Operation
		}{{"GET", item.Get}, {"POST", item.Post}, {"PUT", item.Put}, {"DELETE", item.Delete}} {
			if op.op == nil {
				continue
			}
			codes := make([]string, 0, len(op.op.Responses))
			for c := range op.op.Responses {
				codes = append(codes, c)
			}
			sort.Strings(codes)
			fmt.Println(op.m, p, op.op.OperationID, codes, op.op.Deprecated)
		}
	}
	search := doc.Paths["/orgs/{org}/articles"].Get
	for _, prm := range search.Parameters {
		fmt.Println(prm.In, prm.Name, prm.Required)
	}
	// Output:
	// GET /orgs/{org}/articles ArticlesController.Search [200 400 500] false
	// POST /orgs/{org}/articles ArticlesController.Create [201 400 500] false
	// GET /orgs/{org}/articles/export/csv ArticlesController.ExportCSV [200 500] true
	// PUT /orgs/{org}/articles/{id} ArticlesController.Replace [202 400 500] false
	// DELETE /orgs/{org}/articles/{id} ArticlesController.Delete [204 400 500] false
	// path org true
	// query tag false
	// query limit false
	// query since false
	// header X-Tenant true
	// query verbose false
}
