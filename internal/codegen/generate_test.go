package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func apply(t *testing.T, p *Plan) {
	t.Helper()
	if err := p.Apply(WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratePaths(t *testing.T) {
	tests := []struct {
		name string
		opts GenerateOptions
		file string
		pkg  string
	}{
		{"default dir", GenerateOptions{Kind: KindService, Name: "user-profile"}, "internal/user_profile/user_profile_service.go", "package user_profile"},
		{"suffix word stripped", GenerateOptions{Kind: KindGuard, Name: "AuthGuard"}, "internal/auth/auth_guard.go", "package auth"},
		{"explicit path", GenerateOptions{Kind: KindPipe, Name: "trim", Path: "internal/common"}, "internal/common/trim_pipe.go", "package common"},
		{"flat with path", GenerateOptions{Kind: KindFilter, Name: "all", Path: "pkg/errs", Flat: true}, "pkg/errs/all_filter.go", "package errs"},
		{"dto", GenerateOptions{Kind: KindDTO, Name: "create-user"}, "internal/create_user/create_user_dto.go", "package create_user"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Root = t.TempDir()
			plan, err := Generate(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			apply(t, plan)
			b, err := os.ReadFile(filepath.Join(tc.opts.Root, filepath.FromSlash(tc.file)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tc.pkg+"\n") {
				t.Fatalf("missing %q in\n%s", tc.pkg, b)
			}
			if _, err := os.Stat(strings.TrimSuffix(filepath.Join(tc.opts.Root, tc.file), ".go") + "_test.go"); err != nil {
				t.Fatalf("no test file: %v", err)
			}
		})
	}
}

func TestGenerateFlatUsesRootDirName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my-feature")
	_ = os.MkdirAll(root, 0o755)
	plan, err := Generate(GenerateOptions{Root: root, Kind: KindService, Name: "x", Flat: true})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, plan)
	b, _ := os.ReadFile(filepath.Join(root, "x_service.go"))
	if !strings.HasPrefix(string(b), "package my_feature\n") {
		t.Fatalf("got:\n%s", b)
	}
}

func TestGenerateUsesExistingPackage(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "users")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "other.go"), []byte("package people\n"), 0o644)
	plan, err := Generate(GenerateOptions{Root: root, Kind: KindService, Name: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(plan.Create[0].Content), "package people\n") {
		t.Fatalf("got:\n%s", plan.Create[0].Content)
	}
}

func TestGenerateErrors(t *testing.T) {
	root := t.TempDir()
	for _, o := range []GenerateOptions{
		{Root: root, Kind: "nope", Name: "x"},
		{Root: root, Kind: KindService, Name: ""},
		{Root: root, Kind: KindService, Name: "func"},
	} {
		if _, err := Generate(o); err == nil {
			t.Errorf("%+v: expected error", o)
		}
	}
}

func TestGenerateRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	o := GenerateOptions{Root: root, Kind: KindService, Name: "users"}
	p, _ := Generate(o)
	apply(t, p)
	p, _ = Generate(o)
	if err := p.Apply(WriteOptions{}); err == nil {
		t.Fatal("expected conflict")
	}
	if err := p.Apply(WriteOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateRegistersInModule(t *testing.T) {
	root := t.TempDir()
	step := func(kind Kind, name string) *Plan {
		t.Helper()
		p, err := Generate(GenerateOptions{Root: root, Kind: kind, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		apply(t, p)
		return p
	}
	step(KindModule, "users")
	step(KindService, "users")
	step(KindGuard, "users")
	p := step(KindController, "users")
	if len(p.Update) != 1 {
		t.Fatalf("controller should update module, plan: %+v", p)
	}
	b, _ := os.ReadFile(filepath.Join(root, "internal/users/users_module.go"))
	for _, want := range []string{"NewUsersController", "gonest.Provide(NewUsersService)", "gonest.Provide(NewUsersGuard)"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("module lacks %s:\n%s", want, b)
		}
	}
	// Controller picked up the existing service.
	c, _ := os.ReadFile(filepath.Join(root, "internal/users/users_controller.go"))
	if !strings.Contains(string(c), "service *UsersService") {
		t.Errorf("controller does not inject service:\n%s", c)
	}
	// Idempotent: regenerating with --force does not duplicate entries.
	p, _ = Generate(GenerateOptions{Root: root, Kind: KindController, Name: "users"})
	if len(p.Update) != 0 {
		t.Fatalf("second run should not update module")
	}
}

func TestGenerateHintWhenNoModule(t *testing.T) {
	root := t.TempDir()
	p, err := Generate(GenerateOptions{Root: root, Kind: KindController, Name: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Update) != 0 || len(p.Hints) != 1 || !strings.Contains(p.Hints[0], "NewUsersController") {
		t.Fatalf("hints: %v", p.Hints)
	}

	// A module file without an editable literal produces a hint with the reason.
	dir := filepath.Join(root, "internal", "orders")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "orders_module.go"), []byte("package orders\n\nfunc broken( {\n"), 0o644)
	p, err = Generate(GenerateOptions{Root: root, Kind: KindService, Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Update) != 0 || len(p.Hints) != 1 || !strings.Contains(p.Hints[0], "gonest.Provide(NewOrdersService)") {
		t.Fatalf("hints: %v", p.Hints)
	}
}

func TestGenerateModuleHint(t *testing.T) {
	p, err := Generate(GenerateOptions{Root: t.TempDir(), Kind: KindModule, Name: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hints) != 1 || !strings.Contains(p.Hints[0], "users.UsersModule") {
		t.Fatalf("hints: %v", p.Hints)
	}
}

func TestNewProject(t *testing.T) {
	root := t.TempDir()
	plan, err := NewProject(ProjectOptions{Name: "demo", Root: root, Dir: "out/demo", GonestReplace: "rel/gonest"})
	if err != nil {
		t.Fatal(err)
	}
	var mod, main string
	for _, f := range plan.Create {
		switch filepath.Base(f.Path) {
		case "go.mod":
			mod = string(f.Content)
		case "main.go":
			main = string(f.Content)
		}
		if !strings.HasPrefix(f.Path, filepath.Join(root, "out", "demo")) {
			t.Errorf("file outside target: %s", f.Path)
		}
	}
	if !strings.Contains(mod, "module demo\n") || !strings.Contains(mod, "replace github.com/omyo/gonest => "+filepath.Join(root, "rel", "gonest")) {
		t.Errorf("go.mod:\n%s", mod)
	}
	if !strings.Contains(main, "gonest.Get[*app.AppConfig]") {
		t.Errorf("main.go does not use config:\n%s", main)
	}
	if _, err := NewProject(ProjectOptions{Name: ""}); err == nil {
		t.Error("empty name accepted")
	}

	min, err := NewProject(ProjectOptions{Name: "demo", Root: root, Minimal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(min.Create) != len(plan.Create)-2 {
		t.Errorf("minimal should omit config.go and .env.example: %d vs %d", len(min.Create), len(plan.Create))
	}
}

func TestInfo(t *testing.T) {
	dir := t.TempDir()
	r := Info(dir)
	if r.GonestVersion != Version || !strings.HasPrefix(r.GoVersion, "go") || r.ProjectModule != "" {
		t.Fatalf("%+v", r)
	}
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.24.0\n\nrequire github.com/omyo/gonest v0.1.0\n\nrequire (\n\tgithub.com/gin-gonic/gin v1.11.0 // indirect\n)\n"), 0o644)
	r = Info(dir)
	s := r.String()
	for _, want := range []string{"example.com/x", "gonest v0.1.0", "gin    v1.11.0", "Go:"} {
		if !strings.Contains(s, want) {
			t.Errorf("info lacks %q:\n%s", want, s)
		}
	}
}
