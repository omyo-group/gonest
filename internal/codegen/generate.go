package codegen

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates
var templateFS embed.FS

// Kind is a component type that can be generated.
type Kind string

// Supported component kinds.
const (
	KindModule      Kind = "module"
	KindController  Kind = "controller"
	KindService     Kind = "service"
	KindGuard       Kind = "guard"
	KindInterceptor Kind = "interceptor"
	KindFilter      Kind = "filter"
	KindPipe        Kind = "pipe"
	KindDTO         Kind = "dto"
)

// Kinds lists every supported kind in display order.
func Kinds() []Kind {
	return []Kind{KindModule, KindController, KindService, KindGuard, KindInterceptor, KindFilter, KindPipe, KindDTO}
}

// ParseKind resolves a kind name or one of its aliases (mo, co, s, gu, in, f, pi).
func ParseKind(s string) (Kind, error) {
	aliases := map[string]Kind{
		"mo": KindModule, "co": KindController, "s": KindService, "gu": KindGuard,
		"in": KindInterceptor, "f": KindFilter, "pi": KindPipe,
	}
	if k, ok := aliases[s]; ok {
		return k, nil
	}
	for _, k := range Kinds() {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown component %q (want one of %s)", s, kindList())
}

func kindList() string {
	var s []string
	for _, k := range Kinds() {
		s = append(s, string(k))
	}
	return strings.Join(s, ", ")
}

// GenerateOptions describe one `gonest generate` run.
type GenerateOptions struct {
	// Root is the working directory that relative paths resolve against.
	Root string
	// Kind is the component to generate.
	Kind Kind
	// Name is the raw user supplied name.
	Name string
	// Path is the target directory; empty means internal/<package>.
	Path string
	// Flat places files directly in Path (or Root when Path is empty)
	// instead of internal/<package>.
	Flat bool
}

// templateData is the data every component template receives.
type templateData struct {
	// Pkg is the Go package name of the target directory.
	Pkg string
	// Base is the name without the kind suffix word.
	Base Names
	// Type is the main Go type or variable name, e.g. UsersController.
	Type string
	// WithService is true when a service for the same name exists beside a controller.
	WithService bool
	// ServiceType is e.g. UsersService.
	ServiceType string
}

// Generate plans the files for one component. Nothing is written.
func Generate(o GenerateOptions) (*Plan, error) {
	if _, err := ParseKind(string(o.Kind)); err != nil {
		return nil, err
	}
	names, err := ParseName(o.Name)
	if err != nil {
		return nil, err
	}
	base := names.trimSuffixWord(string(o.Kind))
	root := o.Root
	if root == "" {
		root = "."
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	dir := o.Path
	switch {
	case dir == "" && o.Flat:
		dir = "."
	case dir == "":
		dir = filepath.Join("internal", base.Snake)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	dir = filepath.Clean(dir)

	pkg := existingPackage(dir)
	if pkg == "" {
		pkg = base.Snake
		if o.Path != "" || o.Flat {
			pkg = snakePackage(filepath.Base(dir))
		}
	}
	if err := validPackage(pkg); err != nil {
		return nil, err
	}

	data := templateData{
		Pkg:         pkg,
		Base:        base,
		Type:        base.Pascal + titleCase(string(o.Kind)),
		ServiceType: base.Pascal + "Service",
	}
	if o.Kind == KindDTO {
		data.Type = base.Pascal + "DTO"
	}
	fileBase := base.Snake + "_" + string(o.Kind)
	if o.Kind == KindController {
		if _, err := os.Stat(filepath.Join(dir, base.Snake+"_service.go")); err == nil {
			data.WithService = true
		}
	}

	plan := &Plan{Root: root}
	for _, tmpl := range []struct{ tmpl, file string }{
		{string(o.Kind) + ".go.tmpl", fileBase + ".go"},
		{string(o.Kind) + "_test.go.tmpl", fileBase + "_test.go"},
	} {
		content, err := render("templates/gen/"+tmpl.tmpl, data)
		if err != nil {
			return nil, err
		}
		plan.Create = append(plan.Create, File{Path: filepath.Join(dir, tmpl.file), Content: content, Mode: 0o644})
	}
	registerInModule(plan, dir, o.Kind, data)
	if o.Kind == KindModule {
		plan.Hints = append(plan.Hints, fmt.Sprintf("Add %s.%s to the Imports of your root module.", pkg, data.Type))
	}
	return plan, nil
}

// registerInModule edits the *_module.go of dir, or records a hint.
func registerInModule(plan *Plan, dir string, kind Kind, d templateData) {
	var field, expr string
	switch kind {
	case KindController:
		field, expr = FieldControllers, "New"+d.Type
	case KindService, KindGuard:
		field, expr = FieldProviders, "gonest.Provide(New"+d.Type+")"
	default:
		return
	}
	hint := fmt.Sprintf("Add %s to the %s of your module.", expr, field)
	matches, _ := filepath.Glob(filepath.Join(dir, "*_module.go"))
	var lastErr error
	for _, m := range matches {
		src, err := os.ReadFile(m)
		if err != nil {
			lastErr = err
			continue
		}
		out, err := RegisterInModule(src, field, expr)
		if err != nil {
			lastErr = err
			continue
		}
		if !bytes.Equal(src, out) {
			plan.Update = append(plan.Update, File{Path: m, Content: out, Mode: 0o644})
		}
		return
	}
	if len(matches) > 0 && lastErr != nil && !errors.Is(lastErr, ErrNoModuleLiteral) {
		hint += " (" + lastErr.Error() + ")"
	}
	plan.Hints = append(plan.Hints, hint)
}

func render(name string, data any) ([]byte, error) {
	t, err := template.ParseFS(templateFS, name)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template %s: %w", name, err)
	}
	if strings.HasSuffix(name, ".go.tmpl") {
		out, err := format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", name, err)
		}
		return out, nil
	}
	return buf.Bytes(), nil
}

// existingPackage returns the package name used by non-test Go files in dir.
func existingPackage(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), m, nil, parser.PackageClauseOnly)
		if err == nil {
			return f.Name.Name
		}
	}
	return ""
}

func snakePackage(s string) string {
	return strings.ToLower(strings.Join(splitWords(s), "_"))
}

func titleCase(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
