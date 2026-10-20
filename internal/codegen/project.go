package codegen

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// ProjectOptions describe one `gonest new` run.
type ProjectOptions struct {
	// Name is the raw project name.
	Name string
	// Module is the Go module path; empty means the kebab-case name.
	Module string
	// Dir is the target directory; empty means ./<name>, relative to Root.
	Dir string
	// Root is the working directory that relative paths resolve against.
	Root string
	// GonestReplace, when set, adds `replace github.com/omyo/gonest => PATH`
	// to go.mod, for local development of the framework.
	GonestReplace string
	// Minimal omits the config and logger module wiring, so the project only
	// depends on the root gonest package.
	Minimal bool
}

type projectData struct {
	Name          Names
	Module        string
	GonestVersion string
	GonestReplace string
	Minimal       bool
}

type projectFile struct {
	tmpl, target string
	mode         uint32
	full         bool // only when not Minimal
}

var projectFiles = []projectFile{
	{"go.mod.tmpl", "go.mod", 0o644, false},
	{"main.go.tmpl", "main.go", 0o644, false},
	{"app_module.go.tmpl", "internal/app/app_module.go", 0o644, false},
	{"config.go.tmpl", "internal/app/config.go", 0o644, true},
	{"health_controller.go.tmpl", "internal/app/health_controller.go", 0o644, false},
	{"health_controller_test.go.tmpl", "internal/app/health_controller_test.go", 0o644, false},
	{"Makefile.tmpl", "Makefile", 0o644, false},
	{"Dockerfile.tmpl", "Dockerfile", 0o644, false},
	{"golangci.yml.tmpl", ".golangci.yml", 0o644, false},
	{"gitignore.tmpl", ".gitignore", 0o644, false},
	{"env.example.tmpl", ".env.example", 0o644, true},
	{"README.md.tmpl", "README.md", 0o644, false},
}

// NewProject plans a new application skeleton. Nothing is written.
func NewProject(o ProjectOptions) (*Plan, error) {
	names, err := ParseName(o.Name)
	if err != nil {
		return nil, err
	}
	root := o.Root
	if root == "" {
		root = "."
	}
	if root, err = filepath.Abs(root); err != nil {
		return nil, err
	}
	dir := o.Dir
	if dir == "" {
		dir = names.Kebab
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	replace := o.GonestReplace
	if replace != "" && !filepath.IsAbs(replace) {
		replace = filepath.Join(root, replace)
	}
	module := o.Module
	if module == "" {
		module = names.Kebab
	}
	data := projectData{Name: names, Module: module, GonestVersion: Version, GonestReplace: replace, Minimal: o.Minimal}

	plan := &Plan{Root: root}
	for _, pf := range projectFiles {
		if pf.full && o.Minimal {
			continue
		}
		content, err := render("templates/new/"+pf.tmpl, data)
		if err != nil {
			return nil, err
		}
		plan.Create = append(plan.Create, File{Path: filepath.Join(dir, filepath.FromSlash(pf.target)), Content: content, Mode: fs.FileMode(pf.mode)})
	}
	plan.Hints = append(plan.Hints, fmt.Sprintf("Next: cd %s && go mod tidy && go run .", names.Kebab))
	return plan, nil
}
