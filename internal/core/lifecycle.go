package core

import "context"

// Lifecycle hooks. Any provider or controller instance implementing one of
// these interfaces is called by the application at the matching moment,
// in module dependency order (imported modules first) on startup and in
// reverse order on shutdown.

// OnModuleInit runs once the instance's module has been resolved.
type OnModuleInit interface {
	OnModuleInit(ctx context.Context) error
}

// OnApplicationBootstrap runs after all modules are initialised and all
// routes are mounted, before the server accepts connections.
type OnApplicationBootstrap interface {
	OnApplicationBootstrap(ctx context.Context) error
}

// OnModuleDestroy runs first during shutdown, after the server stopped accepting requests.
type OnModuleDestroy interface {
	OnModuleDestroy(ctx context.Context) error
}

// BeforeApplicationShutdown runs after every OnModuleDestroy hook.
type BeforeApplicationShutdown interface {
	BeforeApplicationShutdown(ctx context.Context) error
}

// OnApplicationShutdown runs last; close connections and flush buffers here.
type OnApplicationShutdown interface {
	OnApplicationShutdown(ctx context.Context) error
}
