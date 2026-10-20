package projects_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/omyo/gonest"
	"github.com/omyo/gonest/examples/saas/internal/projects"
	"github.com/omyo/gonest/gonesttest"
)

// docs:testing-filter

func TestDomainErrorFilter(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		handled bool
		message string
	}{
		{"domain error", &projects.DomainError{Code: "quota_exceeded", Message: "too many"}, true, "too many"},
		{"wrapped domain error", fmt.Errorf("create: %w", &projects.DomainError{Code: "archived", Message: "read only"}), true, "read only"},
		{"http error passes through", gonest.NotFound("nope"), false, ""},
		{"plain error passes through", errors.New("boom"), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := gonesttest.NewCtx(t)
			if got := projects.DomainErrorFilter.Catch(ctx, tc.err); got != tc.handled {
				t.Fatalf("Catch = %v, want %v", got, tc.handled)
			}
			if tc.handled {
				gonesttest.ResponseOf(t, ctx).
					ExpectStatus(422).
					ExpectErrorMessage(tc.message)
			}
		})
	}
}

// docs:end

func TestDomainErrorMessage(t *testing.T) {
	e := &projects.DomainError{Code: "quota_exceeded", Message: "too many"}
	if e.Error() != "quota_exceeded: too many" {
		t.Fatal(e.Error())
	}
}

func TestTrimPipe(t *testing.T) {
	meta := gonest.ArgMeta{Type: reflect.TypeFor[projects.CreateInput]()}
	cases := []struct {
		name    string
		in      any
		want    any
		wantErr bool
	}{
		{"trims", projects.CreateInput{Name: "  Alpha ", Description: " d "}, projects.CreateInput{Name: "Alpha", Description: "d"}, false},
		{"blank", projects.CreateInput{Name: "  a  "}, nil, true},
		{"other types untouched", projects.IDInput{ID: 1}, projects.IDInput{ID: 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := projects.TrimPipe.Transform(gonesttest.NewCtx(t), tc.in, meta)
			if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestHandlersWithoutTenant(t *testing.T) {
	// The middleware normally guarantees a tenant; handlers still fail
	// cleanly when it is missing (for example a misconfigured route).
	c := projects.NewProjectsController(projects.ControllerDeps{})
	ctx := gonesttest.NewCtx(t)
	checks := []error{
		second(c.List(ctx, projects.ListInput{})),
		second(c.FindOne(ctx, projects.IDInput{})),
		second(c.Create(ctx, projects.CreateInput{})),
		second(c.Update(ctx, projects.UpdateInput{})),
		second(c.Delete(ctx, projects.IDInput{})),
	}
	for i, err := range checks {
		var he *gonest.HTTPError
		if !errors.As(err, &he) || he.Status != 400 {
			t.Errorf("handler %d: err = %v", i, err)
		}
	}
}

func second[T any](_ T, err error) error { return err }

func TestMemoryRepository(t *testing.T) {
	r := projects.NewMemoryRepository()
	p := r.Insert(projects.Project{TenantID: "a", Name: "x"})
	if r.Update(projects.Project{ID: p.ID, TenantID: "b"}) {
		t.Error("cross-tenant update succeeded")
	}
	if r.Delete("b", p.ID) {
		t.Error("cross-tenant delete succeeded")
	}
	if _, ok := r.Get("b", p.ID); ok {
		t.Error("cross-tenant get succeeded")
	}
}
