package openapi

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// The Swagger UI assets come from the swagger-ui-dist@5 npm package
// (Apache-2.0, see ui/LICENSE). They are embedded so the page works offline.
//
//go:embed ui/swagger-ui-bundle.js ui/swagger-ui.css ui/LICENSE
var uiFiles embed.FS

var uiAssets = mustSub(uiFiles, "ui")

func mustSub(f embed.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // dir is a constant that exists in the embed directive
	}
	return s
}

var pageTemplate = template.Must(template.New("ui").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="{{.Base}}swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="{{.Base}}swagger-ui-bundle.js"></script>
<script>
window.onload = function () {
  window.ui = SwaggerUIBundle({ url: {{.SpecURL}}, dom_id: "#swagger-ui", deepLinking: true });
};
</script>
</body>
</html>
`))

type pageData struct {
	Title   string
	Base    string // asset URL prefix, relative to the page
	SpecURL string // document URL, relative to the page
}

// servePage writes the Swagger UI HTML. All URLs are relative, so the page
// works behind a global prefix or a reverse proxy that strips a path.
func servePage(w http.ResponseWriter, o Options) {
	title := o.Info.Title
	if title == "" {
		title = "API"
	}
	data := pageData{
		Title:   title + " - API docs",
		Base:    path.Base(o.UIPath) + "/",
		SpecURL: relativePath(path.Dir(o.UIPath), o.JSONPath),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pageTemplate.Execute(w, data)
}

// relativePath returns the URL of absolute path to as seen from directory from.
func relativePath(from, to string) string {
	f := strings.Split(strings.Trim(from, "/"), "/")
	t := strings.Split(strings.Trim(to, "/"), "/")
	if f[0] == "" {
		f = nil
	}
	i := 0
	for i < len(f) && i < len(t)-1 && f[i] == t[i] {
		i++
	}
	parts := make([]string, 0, len(f)-i+len(t)-i)
	for range f[i:] {
		parts = append(parts, "..")
	}
	return strings.Join(append(parts, t[i:]...), "/")
}

var contentTypes = map[string]string{
	".js":  "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8",
}

// serveAsset writes the embedded file name, or redirects "UIPath/" to the page.
func serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		http.Redirect(w, r, strings.TrimRight(r.URL.Path, "/"), http.StatusFound)
		return
	}
	data, err := fs.ReadFile(uiAssets, name)
	if err != nil || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	ct := contentTypes[path.Ext(name)]
	if ct == "" {
		ct = "text/plain; charset=utf-8"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
