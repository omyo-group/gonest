package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/omyo/gonest"
)

// Default paths served by Module.
const (
	DefaultJSONPath = "/openapi.json"
	DefaultUIPath   = "/docs"
)

// Options configures Module.
type Options struct {
	// Info is the API metadata. Title defaults to "API", Version to "1.0.0".
	Info Info
	// Servers lists the servers the API is available on.
	Servers []Server
	// SecuritySchemes defines the schemes routes can require with
	// gonest.Security(name). See BearerJWT and APIKeyHeader.
	SecuritySchemes map[string]SecurityScheme
	// JSONPath is where the document is served (default "/openapi.json").
	JSONPath string
	// UIPath is where Swagger UI is served (default "/docs"). Its assets are
	// served below UIPath + "/".
	UIPath string
	// DisableUI serves only the JSON document.
	DisableUI bool
}

// Module returns a module named "openapi" that serves the document of the
// whole application and, unless DisableUI is set, Swagger UI.
//
// The document is built once, in OnApplicationBootstrap, when every route is
// mounted. The module's own routes are Hidden, so they do not appear in the
// document. Like any route they are prefixed by gonest.WithGlobalPrefix.
// Bootstrap fails if the options are inconsistent, for example when a route
// requires a security scheme that is not defined.
func Module(opts Options) *gonest.Module {
	o := opts.normalised()
	return &gonest.Module{
		Name: "openapi",
		Controllers: []any{
			func(table *gonest.RouteTable) *docsController {
				return &docsController{opts: o, table: table}
			},
		},
	}
}

func (o Options) normalised() Options {
	o.JSONPath = cleanPath(o.JSONPath, DefaultJSONPath)
	o.UIPath = cleanPath(o.UIPath, DefaultUIPath)
	return o
}

func cleanPath(p, def string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return def
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p = strings.TrimRight(p, "/"); p == "" {
		return def
	}
	return p
}

// docsController serves the document and the UI.
type docsController struct {
	opts  Options
	table *gonest.RouteTable

	once sync.Once
	spec []byte
	err  error
}

// Routes implements gonest.Controller.
func (c *docsController) Routes(r gonest.Router) {
	r.GET(c.opts.JSONPath, gonest.HandleRaw(c.serveJSON), gonest.Hidden())
	if c.opts.DisableUI {
		return
	}
	r.GET(c.opts.UIPath, gonest.HandleRaw(c.serveUI), gonest.Hidden())
	r.GET(c.opts.UIPath+"/*file", gonest.HandleRaw(c.serveAsset), gonest.Hidden())
}

// OnApplicationBootstrap builds the document once all routes are mounted.
func (c *docsController) OnApplicationBootstrap(context.Context) error { return c.build() }

// build generates and encodes the document at most once. It also runs lazily
// on the first request, in case lifecycle hooks are not invoked on controllers.
func (c *docsController) build() error {
	c.once.Do(func() {
		var gen []Option
		if len(c.opts.Servers) > 0 {
			gen = append(gen, WithServers(c.opts.Servers...))
		}
		for name, s := range c.opts.SecuritySchemes {
			gen = append(gen, WithSecurityScheme(name, s))
		}
		doc, err := Generate(c.table.All(), c.opts.Info, gen...)
		if err != nil {
			c.err = err
			return
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			c.err = fmt.Errorf("openapi: encoding document: %w", err)
			return
		}
		c.spec = buf.Bytes()
	})
	return c.err
}

// writeJSON writes the OpenAPI document.
func (c *docsController) writeJSON(w http.ResponseWriter) error {
	if err := c.build(); err != nil {
		return gonest.Internal("OpenAPI document unavailable").Wrap(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(c.spec)
	return nil
}

func (c *docsController) serveJSON(ctx *gonest.Ctx) error { return c.writeJSON(ctx.Writer()) }

func (c *docsController) serveUI(ctx *gonest.Ctx) error {
	servePage(ctx.Writer(), c.opts)
	return nil
}

func (c *docsController) serveAsset(ctx *gonest.Ctx) error {
	serveAsset(ctx.Writer(), ctx.Request(), strings.TrimPrefix(ctx.Param("file"), "/"))
	return nil
}
