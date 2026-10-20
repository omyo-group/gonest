package logger

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omyo/gonest"
)

const (
	// ContextKey is the gin key under which RequestLogger stores the
	// per-request *slog.Logger.
	ContextKey = "gonest.logger"
	// RequestIDKey is the gin key holding the request id string.
	RequestIDKey = "gonest.request_id"
	// RequestIDHeader is the header read and echoed for request ids.
	RequestIDHeader = "X-Request-ID"

	maxRequestIDLen = 128
)

// Options configures ForRoot.
type Options struct {
	// Level is the minimum level (default slog.LevelInfo).
	Level slog.Level
	// Format is "json" or "text". Default: json when GIN_MODE=release, else text.
	Format string
	// Output defaults to os.Stdout.
	Output io.Writer
	// AddSource adds the caller's file and line to each record.
	AddSource bool
	// Attrs are static attributes added to every record.
	Attrs []slog.Attr
}

// New builds a *slog.Logger from opts.
func New(opts Options) *slog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	ho := &slog.HandlerOptions{Level: opts.Level, AddSource: opts.AddSource}
	format := opts.Format
	if format == "" {
		if os.Getenv(gin.EnvGinMode) == gin.ReleaseMode {
			format = "json"
		} else {
			format = "text"
		}
	}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(out, ho)
	} else {
		h = slog.NewTextHandler(out, ho)
	}
	if len(opts.Attrs) > 0 {
		h = h.WithAttrs(opts.Attrs)
	}
	return slog.New(h)
}

// ForRoot returns the global "logger" module providing and exporting
// *slog.Logger. It does not touch slog.Default.
func ForRoot(opts Options) *gonest.Module {
	return &gonest.Module{
		Name:      "logger",
		Global:    true,
		Providers: []gonest.Provider{gonest.Provide(func() *slog.Logger { return New(opts) })},
		Exports:   []gonest.Token{gonest.TokenOf[*slog.Logger]()},
	}
}

// RequestLogger returns gin middleware that assigns or propagates a request id
// (X-Request-ID, generated when missing and echoed in the response), stores a
// child logger carrying request_id under ContextKey, and logs one record per
// request: method, path, status, latency, bytes and client_ip. The level is
// error for 5xx, warn for 4xx, info otherwise.
func RequestLogger(l *slog.Logger) gin.HandlerFunc {
	if l == nil {
		l = slog.Default()
	}
	return func(c *gin.Context) {
		start := time.Now()
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Header(RequestIDHeader, id)
		child := l.With("request_id", id)
		c.Set(RequestIDKey, id)
		c.Set(ContextKey, child)

		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		child.LogAttrs(c.Request.Context(), level, "request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.Int("bytes", max(c.Writer.Size(), 0)),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}

// FromCtx returns the request-scoped logger stored by RequestLogger, or
// ctx.Logger() when the middleware did not run.
func FromCtx(ctx *gonest.Ctx) *slog.Logger {
	if l, ok := gonest.CtxValue[*slog.Logger](ctx, ContextKey); ok && l != nil {
		return l
	}
	return ctx.Logger()
}

// validRequestID accepts printable ASCII ids of bounded length, so untrusted
// headers cannot inject log lines or bloat records.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] <= ' ' || id[i] >= 0x7f {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
