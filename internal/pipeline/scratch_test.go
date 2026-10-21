package pipeline

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/omyo-group/gonest/internal/core"
)

type scratchIn struct {
	ID    int               `path:"id"`
	Tags  []string          `json:"tags"`
	Attrs map[string]string `json:"attrs"`
	Note  *string           `json:"note"`
}

// TestScratchValuesAreNotShared checks that recycling the binding scratch of
// value inputs never lets one request observe or mutate another's input:
// inputs retained by the handler keep their own slices, maps and pointers,
// and fields absent from a later body are zero, not left over.
func TestScratchValuesAreNotShared(t *testing.T) {
	h := newHarness(t, "")
	var (
		mu   sync.Mutex
		kept = map[int]scratchIn{}
	)
	h.mount(func(r core.Router) {
		r.POST("/s/:id", core.Handle(func(_ *core.Ctx, in scratchIn) (core.NoContent, error) {
			mu.Lock()
			kept[in.ID] = in
			mu.Unlock()
			return core.NoContent{}, nil
		}))
	})
	if rec := h.do(http.MethodPost, "/s/1", `{"tags":["a","b"],"attrs":{"k":"v"},"note":"n"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("first request: %d %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodPost, "/s/2", `{"tags":["x"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("second request: %d %s", rec.Code, rec.Body)
	}
	first, second := kept[1], kept[2]
	if fmt.Sprint(first.Tags) != "[a b]" || first.Attrs["k"] != "v" || first.Note == nil || *first.Note != "n" {
		t.Fatalf("first input changed after a later request: %+v", first)
	}
	if fmt.Sprint(second.Tags) != "[x]" || second.Attrs != nil || second.Note != nil {
		t.Fatalf("second input carries data from the first: %+v", second)
	}

	// Concurrent requests each see exactly their own values.
	var wg sync.WaitGroup
	for i := 10; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"tags":["t%d"],"attrs":{"i":"%d"}}`, i, i)
			if rec := h.do(http.MethodPost, fmt.Sprintf("/s/%d", i), body); rec.Code != http.StatusNoContent {
				t.Errorf("request %d: %d", i, rec.Code)
			}
		}(i)
	}
	wg.Wait()
	for i := 10; i < 60; i++ {
		in := kept[i]
		if fmt.Sprint(in.Tags) != fmt.Sprintf("[t%d]", i) || in.Attrs["i"] != fmt.Sprint(i) || in.Note != nil {
			t.Fatalf("request %d bound %+v", i, in)
		}
	}
}
