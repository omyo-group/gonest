package gonest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultShutdownTimeout   = 10 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 60 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// newEngine creates the default gin engine without gin's Logger and Recovery
// middleware (the pipeline recovers panics itself). It trusts no proxy, so
// forwarding headers cannot spoof the client IP (see WithTrustedProxies).
// Gin's global mode is only touched when it is still the implicit debug
// default: release mode is set unless GIN_MODE is present in the environment.
func newEngine() *gin.Engine {
	if _, ok := os.LookupEnv(gin.EnvGinMode); !ok && gin.Mode() == gin.DebugMode {
		gin.SetMode(gin.ReleaseMode)
	}
	e := gin.New()
	_ = e.SetTrustedProxies(nil) // cannot fail: nil parses no address
	return e
}

// Listen serves HTTP on addr until SIGINT or SIGTERM, then shuts down gracefully
// and runs shutdown hooks. It returns nil after a clean shutdown. A second
// signal during the graceful shutdown terminates the process immediately.
//
// The server allows 10s to read the request headers, 60s to read the whole
// request and keeps idle connections for 120s. It sets no write timeout, so
// streaming responses work. For other timeouts, build an *http.Server and
// call Serve.
func (a *App) Listen(addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Restore default signal handling once the first signal arrived, so a
	// second Ctrl+C is not swallowed while hooks or slow requests drain.
	go func() { <-ctx.Done(); stop() }()
	srv := &http.Server{
		Addr:              addr,
		Handler:           a.cfg.Engine,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
	return a.Serve(ctx, srv)
}

// Serve serves on an existing listener-backed server; useful for tests and
// custom TLS. It blocks until ctx is cancelled, then shuts down gracefully.
//
// A nil srv.Handler is set to the application. When srv.TLSConfig carries
// certificates, the server uses TLS. If the server fails (for example the
// address is in use), the shutdown hooks still run and the error is returned.
func (a *App) Serve(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return errors.New("gonest: Serve: nil *http.Server")
	}
	if srv.Handler == nil {
		srv.Handler = a.cfg.Engine
	}
	errCh := make(chan error, 1)
	go func() {
		var err error
		if srv.TLSConfig != nil && (len(srv.TLSConfig.Certificates) > 0 || srv.TLSConfig.GetCertificate != nil) {
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		errCh <- err
	}()

	var serveErr error
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("gonest: server: %w", err)
		}
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.ShutdownTimeout)
		if err := srv.Shutdown(sctx); err != nil {
			serveErr = fmt.Errorf("gonest: graceful shutdown: %w", err)
			_ = srv.Close()
		}
		cancel()
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr = errors.Join(serveErr, fmt.Errorf("gonest: server: %w", err))
		}
	}
	return errors.Join(serveErr, a.Shutdown(context.WithoutCancel(ctx)))
}

// Shutdown runs OnModuleDestroy, BeforeApplicationShutdown and
// OnApplicationShutdown hooks in reverse dependency order. Safe to call once;
// later calls return nil.
//
// Every hook runs even when an earlier one fails; the errors are joined. Each
// hook receives a context derived from ctx and bounded to 30 seconds.
func (a *App) Shutdown(ctx context.Context) error {
	var err error
	a.shutdownOnce.Do(func() {
		err = a.rt.Shutdown(ctx)
	})
	return err
}

// resolve looks tok up as seen from the root module and converts it to T.
func resolve[T any](a *App, tok Token) (T, error) {
	var zero T
	if a == nil || a.rt == nil {
		return zero, errors.New("gonest: Get on an application that was not created by gonest.New")
	}
	v, err := a.rt.Container.Resolve(tok, a.rt.Graph.Root())
	if err != nil {
		return zero, err
	}
	if v == nil {
		return zero, nil
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("gonest: provider %s holds %T, not %s", tok, v, tok.Type)
	}
	return t, nil
}
