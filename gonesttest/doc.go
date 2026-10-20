// Package gonesttest helps testing Gonest applications, like NestJS
// Test.createTestingModule: build a testing module with overridden providers,
// issue HTTP requests against it, and exercise guards, pipes, interceptors and
// filters in isolation.
//
// Application tests:
//
//	app := gonesttest.New(t, AppModule).
//		OverrideProvider(gonest.Bind[UsersRepo](NewFakeRepo)).
//		Compile()
//	app.Request("GET", "/users/1").WithBearer("tok").Do().
//		ExpectStatus(200).
//		ExpectJSON(map[string]any{"id": 1})
//
// Overrides never mutate the caller's modules: the reachable module graph is
// copied and providers are replaced by token in the copies, so package-level
// module variables can be shared between tests.
//
// Unit tests of pipeline components use NewCtx, which returns a *gonest.Ctx
// backed by a gin test context, and Recorder or ResponseOf to inspect what a
// component wrote.
package gonesttest
