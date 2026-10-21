package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/omyo-group/gonest"
)

// Option customises Generate.
type Option func(*genConfig)

type genConfig struct {
	servers []Server
	schemes map[string]SecurityScheme
}

// WithServers sets the servers listed in the document.
func WithServers(s ...Server) Option {
	return func(c *genConfig) { c.servers = append(c.servers, s...) }
}

// WithSecurityScheme defines a security scheme that routes can require with
// gonest.Security(name).
func WithSecurityScheme(name string, s SecurityScheme) Option {
	return func(c *genConfig) {
		if c.schemes == nil {
			c.schemes = map[string]SecurityScheme{}
		}
		c.schemes[name] = s
	}
}

// Generate builds an OpenAPI 3.1 document from routes. It is a pure function:
// the same routes always give the same document. Hidden routes are skipped.
//
// It fails when a route has no handler, uses an HTTP method OpenAPI cannot
// describe, appears twice, or requires a security scheme that was not defined
// with WithSecurityScheme.
func Generate(routes []*gonest.RouteInfo, info Info, opts ...Option) (*Document, error) {
	var cfg genConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if info.Title == "" {
		info.Title = "API"
	}
	if info.Version == "" {
		info.Version = "1.0.0"
	}
	b := &builder{
		cfg:     cfg,
		g:       newSchemaGen(),
		opIDs:   map[string]bool{},
		tagsSet: map[string]bool{},
		doc: &Document{
			OpenAPI: Version,
			Info:    info,
			Servers: cfg.servers,
			Paths:   map[string]*PathItem{},
		},
	}
	var errs []error
	for _, r := range routes {
		if err := b.addRoute(r); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	comps := &Components{}
	if len(b.g.components) > 0 {
		comps.Schemas = b.g.components
	}
	if len(cfg.schemes) > 0 {
		comps.SecuritySchemes = cfg.schemes
	}
	if comps.Schemas != nil || comps.SecuritySchemes != nil {
		b.doc.Components = comps
	}
	for name := range b.tagsSet {
		b.doc.Tags = append(b.doc.Tags, Tag{Name: name})
	}
	sort.Slice(b.doc.Tags, func(i, j int) bool { return b.doc.Tags[i].Name < b.doc.Tags[j].Name })
	return b.doc, nil
}

// WriteFile writes the document as indented JSON, for CI checks and client
// code generation.
func WriteFile(doc *Document, path string) error {
	if doc == nil {
		return errors.New("openapi: nil document")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("openapi: encoding document: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("openapi: writing %s: %w", path, err)
	}
	return nil
}

type builder struct {
	cfg     genConfig
	g       *schemaGen
	doc     *Document
	opIDs   map[string]bool
	tagsSet map[string]bool
}

func (b *builder) addRoute(r *gonest.RouteInfo) error {
	if r == nil {
		return errors.New("openapi: nil route")
	}
	if r.Config.Hidden || r.ControllerConfig.Hidden {
		return nil
	}
	label := r.Method + " " + r.Path
	if r.Handler == nil {
		return fmt.Errorf("openapi: route %s has no handler", label)
	}
	path, pathParams := convertPath(r.Path)
	item := b.doc.Paths[path]
	if item == nil {
		item = &PathItem{}
		b.doc.Paths[path] = item
	}
	slot := operationSlot(item, r.Method)
	if slot == nil {
		return fmt.Errorf("openapi: route %s: unsupported method %q", label, r.Method)
	}
	if *slot != nil {
		return fmt.Errorf("openapi: duplicate route %s", label)
	}
	op, err := b.operation(r, pathParams)
	if err != nil {
		return fmt.Errorf("openapi: route %s: %w", label, err)
	}
	*slot = op
	return nil
}

func operationSlot(p *PathItem, method string) **Operation {
	switch method {
	case http.MethodGet:
		return &p.Get
	case http.MethodPut:
		return &p.Put
	case http.MethodPost:
		return &p.Post
	case http.MethodDelete:
		return &p.Delete
	case http.MethodOptions:
		return &p.Options
	case http.MethodHead:
		return &p.Head
	case http.MethodPatch:
		return &p.Patch
	}
	return nil
}

// convertPath turns a gin path into an OpenAPI path and lists its parameters.
// ":id" becomes "{id}" and "*any" becomes "{any}".
func convertPath(p string) (string, []string) {
	segs := strings.Split(p, "/")
	var params []string
	for i, s := range segs {
		switch {
		case strings.HasPrefix(s, ":") && len(s) > 1:
			params = append(params, s[1:])
			segs[i] = "{" + s[1:] + "}"
		case strings.HasPrefix(s, "*"):
			name := s[1:]
			if name == "" {
				name = "wildcard"
			}
			params = append(params, name)
			segs[i] = "{" + name + "}"
		}
	}
	out := strings.Join(segs, "/")
	if out == "" {
		out = "/"
	}
	return out, params
}

func firstSegment(p string) string {
	for _, s := range strings.Split(p, "/") {
		if s != "" && s[0] != ':' && s[0] != '*' {
			return s
		}
	}
	return "default"
}

func (b *builder) operation(r *gonest.RouteInfo, pathParams []string) (*Operation, error) {
	cc, rc := r.ControllerConfig, r.Config
	op := &Operation{
		Summary:     firstNonEmpty(rc.Summary, cc.Summary),
		Description: firstNonEmpty(rc.Description, cc.Description),
		Deprecated:  rc.Deprecated || cc.Deprecated,
		OperationID: b.operationID(r),
		Responses:   map[string]*Response{},
	}
	switch {
	case len(rc.Tags) > 0:
		op.Tags = unique(rc.Tags)
	case len(cc.Tags) > 0:
		op.Tags = unique(cc.Tags)
	default:
		op.Tags = []string{firstSegment(r.Path)}
	}
	for _, t := range op.Tags {
		b.tagsSet[t] = true
	}

	for _, name := range unique(append(append([]string{}, cc.Security...), rc.Security...)) {
		if _, ok := b.cfg.schemes[name]; !ok {
			return nil, fmt.Errorf("security scheme %q is not defined", name)
		}
		op.Security = append(op.Security, SecurityRequirement{name: {}})
	}

	in := r.Handler.InType
	for in != nil && in.Kind() == reflect.Pointer {
		in = in.Elem()
	}
	var inFields []field
	if in != nil && in.Kind() == reflect.Struct {
		inFields = fields(in, true)
	}
	op.Parameters = b.parameters(inFields, pathParams)
	if hasBody(r.Method) {
		op.RequestBody = b.requestBody(in, inFields)
	}
	b.responses(op, r, len(inFields) > 0)
	return op, nil
}

func hasBody(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
}

func (b *builder) operationID(r *gonest.RouteInfo) string {
	id := r.Handler.Name
	if id == "" && r.Controller != nil {
		id = strings.TrimPrefix(r.Controller.String(), "*")
		id = id[strings.LastIndex(id, ".")+1:] + "." + strings.ToLower(r.Method)
	}
	if id == "" {
		id = strings.ToLower(r.Method) + "_" + strings.Trim(strings.NewReplacer("/", "_", ":", "", "*", "").Replace(r.Path), "_")
	}
	base := id
	if b.opIDs[id] {
		id = base + "_" + strings.ToLower(r.Method)
	}
	for i := 2; b.opIDs[id]; i++ {
		id = fmt.Sprintf("%s_%d", base, i)
	}
	b.opIDs[id] = true
	return id
}

func (b *builder) parameters(fs []field, pathParams []string) []Parameter {
	declared := map[string]bool{}
	var params []Parameter
	isPath := map[string]bool{}
	for _, p := range pathParams {
		isPath[p] = true
	}
	for _, f := range fs {
		var in string
		switch f.source {
		case "path":
			if !isPath[f.name] {
				continue // the pipeline rejects this at Mount
			}
			in = "path"
		case "query", "header":
			in = f.source
		default:
			continue
		}
		key := in + ":" + f.name
		if declared[key] {
			continue
		}
		declared[key] = true
		s := b.g.paramSchema(f.sf)
		p := Parameter{
			Name:        f.name,
			In:          in,
			Description: s.Description,
			Example:     s.Example,
			Required:    in == "path" || hasRule(f.sf.Tag.Get("validate"), "required"),
			Schema:      s,
		}
		s.Description, s.Example = "", nil
		params = append(params, p)
	}
	// Every path parameter must be declared, even for raw handlers.
	var missing []Parameter
	for _, name := range pathParams {
		if !declared["path:"+name] {
			declared["path:"+name] = true
			missing = append(missing, Parameter{
				Name: name, In: "path", Required: true,
				Schema: &Schema{Type: SchemaType{"string"}},
			})
		}
	}
	return append(missing, params...)
}

func (b *builder) requestBody(in reflect.Type, fs []field) *RequestBody {
	var jsonFields, formFields []field
	for _, f := range fs {
		switch f.source {
		case "json":
			jsonFields = append(jsonFields, f)
		case "form":
			formFields = append(formFields, f)
		}
	}
	switch {
	case len(jsonFields) > 0:
		s := b.g.structRef(in, true)
		return &RequestBody{
			Required: len(b.g.components[strings.TrimPrefix(s.Ref, "#/components/schemas/")].Required) > 0,
			Content:  map[string]*MediaType{"application/json": {Schema: s}},
		}
	case len(formFields) > 0:
		obj := &Schema{Type: SchemaType{"object"}, Properties: map[string]*Schema{}}
		media := "application/x-www-form-urlencoded"
		for _, f := range formFields {
			var s *Schema
			if isFileField(f.sf.Type) {
				media = "multipart/form-data"
				s = fileSchema(f.sf.Type)
				if d := f.sf.Tag.Get("doc"); d != "" {
					s.Description = d
				}
			} else {
				s = b.g.paramSchema(f.sf)
			}
			obj.Properties[f.name] = s
			if hasRule(f.sf.Tag.Get("validate"), "required") {
				obj.Required = append(obj.Required, f.name)
			}
		}
		return &RequestBody{
			Required: len(obj.Required) > 0,
			Content:  map[string]*MediaType{media: {Schema: obj}},
		}
	}
	return nil
}

func fileSchema(t reflect.Type) *Schema {
	file := &Schema{Type: SchemaType{"string"}, Format: "binary"}
	if t.Kind() == reflect.Slice {
		return &Schema{Type: SchemaType{"array"}, Items: file}
	}
	return file
}

func (b *builder) responses(op *Operation, r *gonest.RouteInfo, hasInput bool) {
	status := r.SuccessStatus()
	ok := &Response{Description: http.StatusText(status)}
	if !r.Handler.Raw && r.Handler.OutType != nil && !noBody(status) && !isNoContent(r.Handler.OutType) {
		ok.Content = jsonContent(b.g.build(r.Handler.OutType, false))
	}
	op.Responses[fmt.Sprint(status)] = ok

	docs := append(append([]gonest.ResponseDoc{}, r.ControllerConfig.Responses...), r.Config.Responses...)
	for _, d := range docs {
		key := fmt.Sprint(d.Status)
		resp := &Response{Description: firstNonEmpty(d.Description, http.StatusText(d.Status))}
		switch {
		case d.Type != nil:
			resp.Content = jsonContent(b.g.build(d.Type, false))
		case d.Status == status && ok.Content != nil && d.Description != "":
			resp.Content = ok.Content // description-only override keeps the body
		}
		op.Responses[key] = resp
	}

	guards := len(r.ControllerConfig.Guards) + len(r.Config.Guards)
	add := func(code int, cond bool) {
		key := fmt.Sprint(code)
		if !cond || op.Responses[key] != nil {
			return
		}
		op.Responses[key] = &Response{
			Description: http.StatusText(code),
			Content:     jsonContent(b.g.schemaOf(errorBodyType)),
		}
	}
	add(http.StatusBadRequest, hasInput)
	add(http.StatusUnauthorized, guards > 0)
	add(http.StatusForbidden, guards > 0)
	add(http.StatusInternalServerError, true)
}

func noBody(status int) bool {
	return status == http.StatusNoContent || status == http.StatusResetContent ||
		status == http.StatusNotModified || status < 200
}

func isNoContent(t reflect.Type) bool { return t == reflect.TypeFor[gonest.NoContent]() }

func jsonContent(s *Schema) map[string]*MediaType {
	return map[string]*MediaType{"application/json": {Schema: s}}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
