package gonest_test

import (
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omyo/gonest"
)

// loadCtrl counts requests in flight and records whether any handler was
// still running when the shutdown hooks started.
type loadCtrl struct {
	inFlight atomic.Int64
	started  atomic.Int64
}

func (c *loadCtrl) Routes(r gonest.Router) {
	r.GET("/work", gonest.Handle(func(ctx *gonest.Ctx, _ gonest.Empty) (string, error) {
		c.started.Add(1)
		c.inFlight.Add(1)
		defer c.inFlight.Add(-1)
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "ok", nil
	}))
}

type hookProbe struct {
	ctrl            *loadCtrl
	inFlightAtHooks atomic.Int64
	ran             atomic.Bool
}

func (h *hookProbe) OnModuleDestroy(context.Context) error {
	h.inFlightAtHooks.Store(h.ctrl.inFlight.Load())
	h.ran.Store(true)
	return nil
}

// Graceful shutdown under concurrent load: every request accepted before the
// shutdown completes with 200, no handler is still running when the
// shutdown hooks start, and Serve returns nil.
func TestServeGracefulShutdownUnderLoad(t *testing.T) {
	ctrl := &loadCtrl{}
	probe := &hookProbe{ctrl: ctrl}
	mod := &gonest.Module{
		Name:        "load",
		Providers:   []gonest.Provider{gonest.Value(probe)},
		Controllers: []any{func() *loadCtrl { return ctrl }},
	}
	app, err := gonest.New(mod, gonest.WithoutStartupLog(), gonest.WithLogger(quiet()), gonest.WithShutdownTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- app.Serve(ctx, &http.Server{Addr: addr, ReadHeaderTimeout: time.Second}) }()
	waitUp(t, "http://"+addr+"/work")

	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64}, Timeout: 10 * time.Second}
	var (
		wg       sync.WaitGroup
		ok, fail atomic.Int64
		stop     = make(chan struct{})
	)
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := client.Get("http://" + addr + "/work")
				if err != nil {
					// Refused after the listener closed: expected, the
					// request was never accepted.
					fail.Add(1)
					time.Sleep(time.Millisecond)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("accepted request answered %d during shutdown", resp.StatusCode)
				}
				ok.Add(1)
			}
		}()
	}
	// Let load build up, then shut down while requests are in flight.
	deadline := time.Now().Add(2 * time.Second)
	for ctrl.inFlight.Load() < 8 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ctrl.inFlight.Load() == 0 {
		t.Fatal("no request in flight before shutdown")
	}
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return")
	}
	close(stop)
	wg.Wait()
	if !probe.ran.Load() {
		t.Fatal("OnModuleDestroy did not run")
	}
	if n := probe.inFlightAtHooks.Load(); n != 0 {
		t.Fatalf("%d handlers still running when shutdown hooks started", n)
	}
	if ok.Load() == 0 {
		t.Fatal("no request completed")
	}
	t.Logf("completed=%d refused=%d started=%d", ok.Load(), fail.Load(), ctrl.started.Load())
}
