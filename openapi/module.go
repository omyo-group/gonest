package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
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
	// RouteOptions are applied to the document and UI routes, for example
	// gonest.SetMetadata so that a global guard lets them through, or
	// gonest.UseGuards to protect the documentation. Documentation options
	// (Summary, Tags, ...) are irrelevant: the routes are always Hidden.
	RouteOptions []gonest.RouteOption
	// Public declares that serving the documentation without a guard in
	// RouteOptions is intended: the API is public, or a global guard
	// protects these routes. It silences the warning logged at bootstrap in
	// release mode.
	Public bool
}

// Module returns a module named "openapi" that serves the document of the
// whole application and, unless DisableUI is set, Swagger UI.
//
// The document is built once, in OnApplicationBootstrap, when every route is
// mounted. The module's own routes are Hidden, so they do not appear in the
// document. Like any route they are prefixed by gonest.WithGlobalPrefix.
// Bootstrap fails if the options are inconsistent, for example when a route
// requires a security scheme that is not defined.
//
// The document describes every route of the application. In gin release mode,
// bootstrap logs a warning when RouteOptions carry no guard and Public is not
// set, since anyone can then read the document.
func Module(opts Options) *gonest.Module {
	o := opts.normalised()
	return &gonest.Module{
		Name: "openapi",
		Controllers: []any{
			func(table *gonest.RouteTable, log *slog.Logger) *docsController {
				return &docsController{opts: o, table: table, log: log}
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
	log   *slog.Logger

	once sync.Once
	spec []byte
	err  error
}

// Routes implements gonest.Controller.
func (c *docsController) Routes(r gonest.Router) {
	if len(c.opts.RouteOptions) > 0 {
		r.Use(c.opts.RouteOptions...)
	}
	r.GET(c.opts.JSONPath, gonest.HandleRaw(c.serveJSON), gonest.Hidden())
	if c.opts.DisableUI {
		return
	}
	r.GET(c.opts.UIPath, gonest.HandleRaw(c.serveUI), gonest.Hidden())
	r.GET(c.opts.UIPath+"/*file", gonest.HandleRaw(c.serveAsset), gonest.Hidden())
}

// OnApplicationBootstrap builds the document once all routes are mounted and
// warns when it is served unguarded in release mode.
func (c *docsController) OnApplicationBootstrap(context.Context) error {
	if !c.opts.Public && gin.Mode() == gin.ReleaseMode && !hasGuard(c.opts.RouteOptions) {
		paths := c.opts.JSONPath
		if !c.opts.DisableUI {
			paths += " and " + c.opts.UIPath
		}
		log := c.log
		if log == nil {
			log = slog.Default()
		}
		log.Warn("openapi: documentation is served without a guard; protect it with Options.RouteOptions "+
			"(gonest.UseGuards) or set Options.Public if a global guard covers it or it is meant to be public",
			"paths", paths)
	}
	return c.build()
}

// hasGuard reports whether opts add at least one guard.
func hasGuard(opts []gonest.RouteOption) bool {
	var cfg gonest.RouteConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return len(cfg.Guards) > 0
}

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
