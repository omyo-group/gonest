package di

import (
	"reflect"

	"github.com/omyo/gonest/internal/core"
)

// Container implements core.Container. Wave 0 stub.
type Container struct{}

// New returns an empty container.
func New() *Container { return &Container{} }

// Register implements core.Container.
func (c *Container) Register(p core.Provider, owner *core.Module) error {
	return core.ErrNotImplemented
}

// SetVisibility implements core.Container.
func (c *Container) SetVisibility(v core.Visibility) {}

// Validate implements core.Container.
func (c *Container) Validate() error { return core.ErrNotImplemented }

// Resolve implements core.Container.
func (c *Container) Resolve(tok core.Token, from *core.Module) (any, error) {
	return nil, core.ErrNotImplemented
}

// Invoke implements core.Container.
func (c *Container) Invoke(fn any, from *core.Module) ([]reflect.Value, error) {
	return nil, core.ErrNotImplemented
}

// Instances implements core.Container.
func (c *Container) Instances() []core.Instance { return nil }

// Enhancers implements core.Container.
func (c *Container) Enhancers() []core.Instance { return nil }

var _ core.Container = (*Container)(nil)
