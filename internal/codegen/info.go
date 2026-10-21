package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

// InfoReport describes the toolchain and the project in the working directory.
type InfoReport struct {
	GoVersion     string
	GonestVersion string
	GinVersion    string
	ProjectGonest string
	ProjectGin    string
	ProjectModule string
}

var requireRe = regexp.MustCompile(`(?m)^\s*(?:require\s+)?(github\.com/(?:omyo/gonest|gin-gonic/gin))\s+(v\S+)`)
var moduleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// Info gathers version information. dir is searched for a go.mod.
func Info(dir string) InfoReport {
	r := InfoReport{GoVersion: runtime.Version(), GonestVersion: Version, GinVersion: "unknown (not linked into the CLI)"}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/gin-gonic/gin" {
				r.GinVersion = d.Version
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return r
	}
	if m := moduleRe.FindSubmatch(data); m != nil {
		r.ProjectModule = string(m[1])
	}
	for _, m := range requireRe.FindAllSubmatch(data, -1) {
		switch string(m[1]) {
		case "github.com/omyo-group/gonest":
			r.ProjectGonest = string(m[2])
		case "github.com/gin-gonic/gin":
			r.ProjectGin = string(m[2])
		}
	}
	return r
}

// String renders the report for the terminal.
func (r InfoReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Go:      %s\n", r.GoVersion)
	fmt.Fprintf(&b, "Gonest:  %s (CLI)\n", r.GonestVersion)
	fmt.Fprintf(&b, "Gin:     %s\n", r.GinVersion)
	if r.ProjectModule != "" {
		fmt.Fprintf(&b, "Project: %s\n", r.ProjectModule)
		if r.ProjectGonest != "" {
			fmt.Fprintf(&b, "  requires gonest %s\n", r.ProjectGonest)
		}
		if r.ProjectGin != "" {
			fmt.Fprintf(&b, "  requires gin    %s\n", r.ProjectGin)
		}
	}
	return b.String()
}
