package guide_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/omyo/gonest"
)

// quiet silences the startup log and the framework logger in examples.
func quiet() []gonest.Option {
	return []gonest.Option{gonest.WithoutStartupLog(), gonest.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}
}

// call serves one request and returns "status body".
func call(app *gonest.App, method, path, body string, headers ...string) string {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)
	return strings.TrimSpace(strconv.Itoa(w.Code) + " " + w.Body.String())
}
