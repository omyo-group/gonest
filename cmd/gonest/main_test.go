package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut, dir)
	return out.String() + errOut.String(), err
}

func TestCLIErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
	}{
		{"unknown component", []string{"g", "widget", "x"}},
		{"missing name", []string{"g", "module"}},
		{"bad name", []string{"g", "module", "9x"}},
		{"new without name", []string{"new"}},
		{"unknown command", []string{"frobnicate"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runCLI(t, dir, tc.args...); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCLIHelpAndInfo(t *testing.T) {
	dir := t.TempDir()
	out, err := runCLI(t, dir)
	if err != nil || !strings.Contains(out, "generate") || !strings.Contains(out, "new") {
		t.Fatalf("help: %v\n%s", err, out)
	}
	out, err = runCLI(t, dir, "info")
	if err != nil || !strings.Contains(out, "Gonest:") || !strings.Contains(out, "Go:") {
		t.Fatalf("info: %v\n%s", err, out)
	}
}

func TestCLIDryRunAndForce(t *testing.T) {
	dir := t.TempDir()
	out, err := runCLI(t, dir, "g", "service", "users", "--dry-run")
	if err != nil || !strings.Contains(out, "(dry run) CREATE internal/users/users_service.go") {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal")); err == nil {
		t.Fatal("dry run created files")
	}
	if _, err := runCLI(t, dir, "g", "service", "users"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, dir, "generate", "service", "users"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second run err = %v", err)
	}
	if _, err := runCLI(t, dir, "g", "s", "users", "--force"); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, dir, "new", "demo", "--dry-run", "--minimal")
	if err != nil || !strings.Contains(out, "demo/main.go") {
		t.Fatalf("new dry-run: %v\n%s", err, out)
	}
}

// TestEndToEnd scaffolds a project, generates components and compiles it.
// The project uses --minimal because modules/config and modules/logger are
// built by another agent; the full template is covered by golden tests.
func TestEndToEnd(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	repo := repoRoot(t)
	work := t.TempDir()

	steps := [][]string{
		{"new", "shop", "--module", "example.com/shop", "--minimal", "--gonest-replace", repo},
	}
	for _, s := range steps {
		if out, err := runCLI(t, work, s...); err != nil {
			t.Fatalf("%v: %v\n%s", s, err, out)
		}
	}
	proj := filepath.Join(work, "shop")
	for _, s := range [][]string{
		{"g", "module", "users"},
		{"g", "service", "users"},
		{"g", "controller", "users"},
		{"g", "guard", "auth"},
		{"g", "interceptor", "logging"},
		{"g", "filter", "all-exceptions"},
		{"g", "pipe", "trim"},
		{"g", "dto", "create-user"},
		{"g", "controller", "orders", "--path", "internal/app"},
	} {
		if out, err := runCLI(t, proj, s...); err != nil {
			t.Fatalf("%v: %v\n%s", s, err, out)
		}
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}

	mod, _ := os.ReadFile(filepath.Join(proj, "internal/users/users_module.go"))
	for _, want := range []string{"NewUsersController", "gonest.Provide(NewUsersService)"} {
		if !strings.Contains(string(mod), want) {
			t.Errorf("users module lacks %s:\n%s", want, mod)
		}
	}
	app, _ := os.ReadFile(filepath.Join(proj, "internal/app/app_module.go"))
	if !strings.Contains(string(app), "NewHealthController, NewOrdersController") {
		t.Errorf("app module not updated:\n%s", app)
	}

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}, {"test", "./..."}} {
		cmd := exec.Command(goBin, args...)
		cmd.Dir = proj
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && strings.Contains(string(b), "module github.com/omyo/gonest\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
