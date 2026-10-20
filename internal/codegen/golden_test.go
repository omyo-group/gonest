package codegen

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from golden file %s\n--- got ---\n%s", name, path, got)
	}
}

func TestGenerateGolden(t *testing.T) {
	for _, kind := range Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			plan, err := Generate(GenerateOptions{Root: t.TempDir(), Kind: kind, Name: "user-profile"})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range plan.Create {
				checkGolden(t, "gen/"+filepath.Base(f.Path), f.Content)
			}
		})
	}
}

func TestGenerateControllerWithServiceGolden(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "user_profile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "user_profile_service.go"), []byte("package user_profile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := Generate(GenerateOptions{Root: root, Kind: KindController, Name: "user-profile"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan.Create {
		checkGolden(t, "gen_with_service/"+filepath.Base(f.Path), f.Content)
	}
}

func TestNewProjectGolden(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		name := "full"
		if minimal {
			name = "minimal"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			plan, err := NewProject(ProjectOptions{Name: "My App", Module: "github.com/acme/my-app", Root: root, GonestReplace: "/src/gonest", Minimal: minimal})
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "my-app")
			for _, f := range plan.Create {
				rel, _ := filepath.Rel(dir, f.Path)
				checkGolden(t, "new_"+name+"/"+strings.ReplaceAll(filepath.ToSlash(rel), "/", "__"), f.Content)
			}
		})
	}
}
