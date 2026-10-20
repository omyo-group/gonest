package core

// Module groups providers and controllers, like a NestJS @Module.
// Modules are identified by pointer: importing the same *Module twice yields
// one instance. A dynamic module (NestJS forRoot/forFeature) is simply a
// function that returns a *Module.
//
//	var UsersModule = &gonest.Module{
//		Name:        "users",
//		Imports:     []*gonest.Module{DatabaseModule},
//		Providers:   []gonest.Provider{gonest.Provide(NewUsersService)},
//		Controllers: []any{NewUsersController},
//		Exports:     []gonest.Token{gonest.TokenOf[*UsersService]()},
//	}
type Module struct {
	// Name identifies the module in logs and errors. Required.
	Name string

	// Imports are the modules whose exported providers this module can inject.
	Imports []*Module

	// Providers are registered in this module.
	Providers []Provider

	// Controllers are constructors returning a value that implements Controller.
	// Their parameters are injected like any provider's.
	Controllers []any

	// Exports lists the tokens of this module's providers that importers may inject.
	Exports []Token

	// ReExports lists imported modules whose exports are passed on to importers
	// of this module (NestJS: putting a module in exports).
	ReExports []*Module

	// Global makes every exported provider visible to all modules without importing.
	Global bool

	// Configure, when set, is a function func(*MiddlewareConsumer, deps...) whose
	// further parameters are injected. It binds gin middleware to routes,
	// like NestJS NestModule.configure.
	Configure any
}

// String returns the module name.
func (m *Module) String() string {
	if m == nil {
		return "<nil module>"
	}
	return m.Name
}
