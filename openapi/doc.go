// Package openapi generates an OpenAPI 3.1 document from mounted Gonest routes
// and serves it together with an embedded Swagger UI.
//
// Generate is a pure function over []*gonest.RouteInfo, so it can be used in
// tests and in CI to write the specification to a file (see WriteFile).
// Module wires the same generator into an application: it builds the document
// once, when the application has bootstrapped and every route is known, and
// serves it as JSON plus a Swagger UI page. No OpenAPI library is used; see
// docs/adr/0002-openapi.md.
//
//	app, err := gonest.New(&gonest.Module{
//		Name: "app",
//		Imports: []*gonest.Module{
//			UsersModule,
//			openapi.Module(openapi.Options{
//				Info:            openapi.Info{Title: "Users API", Version: "1.0.0"},
//				SecuritySchemes: map[string]openapi.SecurityScheme{"bearer": openapi.BearerJWT()},
//			}),
//		},
//	})
//
// Owner: agent B2.
package openapi
