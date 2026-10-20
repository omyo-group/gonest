# CLI

`cmd/gonest` is the equivalent of the Nest CLI. It only generates code; the
generated code is ordinary Go that you own.

```sh
go install github.com/omyo/gonest/cmd/gonest@latest
# or, from a checkout of the repository:
go run ./cmd/gonest --help
```

## gonest new

```text
gonest new <name> [--module path] [--dir path] [--minimal] [--gonest-replace PATH] [--force] [--dry-run]
```

```text
$ gonest new demo --module example.com/demo
CREATE demo/go.mod (74 bytes)
CREATE demo/main.go (469 bytes)
CREATE demo/internal/app/app_module.go (506 bytes)
CREATE demo/internal/app/config.go (365 bytes)
CREATE demo/internal/app/health_controller.go (718 bytes)
CREATE demo/internal/app/health_controller_test.go (365 bytes)
CREATE demo/Makefile (294 bytes)
CREATE demo/Dockerfile (325 bytes)
CREATE demo/.golangci.yml (180 bytes)
CREATE demo/.gitignore (40 bytes)
CREATE demo/.env.example (30 bytes)
CREATE demo/README.md (663 bytes)
Next: cd demo && go mod tidy && go run .
```

The project wires `config.ForRoot[AppConfig]` and `logger.ForRoot` in its
root module, has a health controller with a test, and listens on the
configured `PORT`. `--minimal` leaves out the config and logger modules.
`--gonest-replace` adds a `replace` directive to a local Gonest checkout.
No `go.sum` is written: run `go mod tidy` first.

## gonest generate (g)

```text
gonest generate <module|controller|service|guard|interceptor|filter|pipe|dto> <name> [--path dir] [--flat] [--force] [--dry-run]
```

Aliases: `mo`, `co`, `s`, `gu`, `in`, `f`, `pi`. Files go to
`internal/<name>/` unless `--path` (the target directory) or `--flat` (the
current directory) is given; each component comes with a test file.

```text
$ gonest g module billing
CREATE internal/billing/billing_module.go (337 bytes)
CREATE internal/billing/billing_module_test.go (177 bytes)
Add billing.BillingModule to the Imports of your root module.
$ gonest g service billing
CREATE internal/billing/billing_service.go (467 bytes)
CREATE internal/billing/billing_service_test.go (226 bytes)
UPDATE internal/billing/billing_module.go
$ gonest g controller billing
CREATE internal/billing/billing_controller.go (741 bytes)
CREATE internal/billing/billing_controller_test.go (390 bytes)
UPDATE internal/billing/billing_module.go
```

Services and guards are added to the module's `Providers`, controllers to its
`Controllers` (the file is edited through the Go AST and re-formatted). A
controller generated next to an existing service injects it. New modules are
not added to the root module's `Imports` automatically (the CLI prints a
reminder), and interceptors, filters and pipes are not registered anywhere:
attach them with `gonest.UseInterceptors(...)` or `gonest.AppInterceptor(...)`
and friends. Existing files are never overwritten without `--force`.

The generated module after those three commands:

```text
var BillingModule = &gonest.Module{
	Name:        "billing",
	Providers:   []gonest.Provider{gonest.Provide(NewBillingService)},
	Controllers: []any{NewBillingController},
	Exports:     []gonest.Token{},
}
```

## gonest info

Prints the Go, Gonest and Gin versions and, inside a project, its module path
and required Gonest version.
