package codegen

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File is a file to write. Path is absolute.
type File struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
}

// Plan is the outcome of a generator: files to create, existing files to
// update, and hints for edits that could not be made safely.
type Plan struct {
	// Root is used to print relative paths.
	Root string
	// Create lists new files. Existing ones are an error unless Force is set.
	Create []File
	// Update lists existing files rewritten in place (module registration).
	Update []File
	// Hints are lines the user must add by hand.
	Hints []string
}

// WriteOptions control Plan.Apply.
type WriteOptions struct {
	// Force overwrites existing files in Create.
	Force bool
	// DryRun prints what would happen and writes nothing.
	DryRun bool
	// Out receives progress lines; nil discards them.
	Out io.Writer
}

// ErrExists is wrapped by errors for files that exist and Force is false.
var ErrExists = errors.New("file already exists (use --force to overwrite)")

// Apply writes the plan. All conflicts are detected before the first write.
func (p *Plan) Apply(o WriteOptions) error {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	var conflicts []string
	for _, f := range p.Create {
		if _, err := os.Stat(f.Path); err == nil && !o.Force {
			conflicts = append(conflicts, p.rel(f.Path))
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("%w: %s", ErrExists, strings.Join(conflicts, ", "))
	}
	prefix := ""
	if o.DryRun {
		prefix = "(dry run) "
	}
	for _, f := range p.Create {
		verb := "CREATE"
		if _, err := os.Stat(f.Path); err == nil {
			verb = "OVERWRITE"
		}
		fmt.Fprintf(out, "%s%s %s (%d bytes)\n", prefix, verb, p.rel(f.Path), len(f.Content))
		if !o.DryRun {
			if err := WriteFileAtomic(f.Path, f.Content, f.Mode); err != nil {
				return err
			}
		}
	}
	for _, f := range p.Update {
		fmt.Fprintf(out, "%sUPDATE %s\n", prefix, p.rel(f.Path))
		if !o.DryRun {
			if err := WriteFileAtomic(f.Path, f.Content, f.Mode); err != nil {
				return err
			}
		}
	}
	for _, h := range p.Hints {
		fmt.Fprintln(out, h)
	}
	return nil
}

func (p *Plan) rel(path string) string {
	if p.Root != "" {
		if r, err := filepath.Rel(p.Root, path); err == nil {
			return r
		}
	}
	return path
}

// WriteFileAtomic writes data to a temporary file in the target directory and
// renames it over path, so readers never see a partial file.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".gonest-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Chmod(name, mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}
