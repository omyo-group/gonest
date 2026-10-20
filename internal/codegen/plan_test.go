package codegen

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanApply(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "a", "x.go")
	plan := &Plan{
		Root:   root,
		Create: []File{{Path: existing, Content: []byte("one")}, {Path: filepath.Join(root, "a", "y.go"), Content: []byte("two"), Mode: 0o755}},
		Hints:  []string{"hint line"},
	}
	var out bytes.Buffer

	if err := plan.Apply(WriteOptions{DryRun: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(existing); err == nil {
		t.Fatal("dry run wrote a file")
	}
	if !strings.Contains(out.String(), "(dry run) CREATE a/x.go") || !strings.Contains(out.String(), "hint line") {
		t.Fatalf("output: %s", out.String())
	}

	if err := plan.Apply(WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "one" {
		t.Fatalf("content %q", b)
	}
	if st, _ := os.Stat(filepath.Join(root, "a", "y.go")); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}

	plan.Create[0].Content = []byte("changed")
	err := plan.Apply(WriteOptions{})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "one" {
		t.Fatal("file overwritten without --force")
	}

	out.Reset()
	if err := plan.Apply(WriteOptions{Force: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "changed" {
		t.Fatalf("content %q", b)
	}
	if !strings.Contains(out.String(), "OVERWRITE") {
		t.Fatalf("output: %s", out.String())
	}
	entries, _ := os.ReadDir(filepath.Join(root, "a"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestPlanConflictWritesNothing(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "old.go")
	_ = os.WriteFile(existing, []byte("old"), 0o644)
	fresh := filepath.Join(root, "new.go")
	plan := &Plan{Root: root, Create: []File{{Path: fresh, Content: []byte("n")}, {Path: existing, Content: []byte("x")}}}
	if err := plan.Apply(WriteOptions{}); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(fresh); err == nil {
		t.Fatal("partial write before conflict")
	}
}

func TestPlanUpdate(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "m.go")
	_ = os.WriteFile(p, []byte("old"), 0o600)
	plan := &Plan{Root: root, Update: []File{{Path: p, Content: []byte("new")}}}
	if err := plan.Apply(WriteOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Fatal("dry run updated file")
	}
	if err := plan.Apply(WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatal("not updated")
	}
}

func TestWriteFileAtomicErrors(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	_ = os.WriteFile(blocker, nil, 0o644)
	if err := WriteFileAtomic(filepath.Join(blocker, "sub", "x"), []byte("x"), 0); err == nil {
		t.Fatal("expected mkdir error")
	}
	// Destination is a directory: rename fails and the temp file is removed.
	dir := filepath.Join(root, "d")
	_ = os.MkdirAll(filepath.Join(dir, "inner"), 0o755)
	if err := WriteFileAtomic(dir, []byte("x"), 0); err == nil {
		t.Fatal("expected rename error")
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}
