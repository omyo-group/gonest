package docscheck

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// root is the repository root, relative to this package.
const root = "../.."

var markerRE = regexp.MustCompile(`^<!-- snippet: (\S+)#(\S+) -->$`)

// Region returns the lines between "// docs:<name>" and the next
// "// docs:end" in file, with the common leading tabs removed.
func region(t *testing.T, file, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatalf("snippet source: %v", err)
	}
	var out []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !in && trimmed == "// docs:"+name:
			in = true
		case in && trimmed == "// docs:end":
			return dedent(out)
		case in:
			out = append(out, line)
		}
	}
	t.Fatalf("%s: region %q not found or not closed", file, name)
	return ""
}

func dedent(lines []string) string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	minTabs := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, "\t"))
		if minTabs < 0 || n < minTabs {
			minTabs = n
		}
	}
	for i, l := range lines {
		if len(l) >= minTabs && minTabs > 0 {
			lines[i] = l[minTabs:]
		}
	}
	return strings.Join(lines, "\n")
}

type block struct {
	file, src, name string
	line            int
	body            string
}

// blocks parses the ```go blocks of a markdown file. Each must be preceded by
// a snippet marker.
func blocks(t *testing.T, md string) []block {
	t.Helper()
	f, err := os.Open(md)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var (
		out     []block
		pending *block
		cur     *block
		buf     []string
		n       int
	)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
		line := sc.Text()
		switch {
		case cur != nil && line == "```":
			cur.body = strings.Join(buf, "\n")
			out = append(out, *cur)
			cur, buf = nil, nil
		case cur != nil:
			buf = append(buf, line)
		case markerRE.MatchString(line):
			m := markerRE.FindStringSubmatch(line)
			pending = &block{file: md, src: m[1], name: m[2], line: n}
		case line == "```go":
			if pending == nil {
				t.Errorf("%s:%d: go block without a snippet marker", md, n)
				pending = &block{file: md, line: n}
			}
			cur, pending = pending, nil
		case strings.TrimSpace(line) != "" && pending != nil:
			t.Errorf("%s:%d: snippet marker not followed by a go block", md, pending.line)
			pending = nil
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDocSnippets checks that every Go snippet of the guide and the README is
// a verbatim copy of a region of compiled code under examples/.
func TestDocSnippets(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(root, "docs", "guide", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no guide pages found (%v)", err)
	}
	files = append(files, filepath.Join(root, "README.md"))
	total := 0
	for _, md := range files {
		for _, b := range blocks(t, md) {
			if b.src == "" {
				continue
			}
			total++
			if want := region(t, b.src, b.name); b.body != want {
				t.Errorf("%s:%d: snippet %s#%s is out of date; regenerate it from the source", b.file, b.line, b.src, b.name)
			}
		}
	}
	if total == 0 {
		t.Fatal("no snippets checked")
	}
}
