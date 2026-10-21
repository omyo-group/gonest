# Configuration

`github.com/omyo/gonest/modules/config` is the ConfigModule: a typed struct
loaded from default tags, `.env` files and environment variables, validated
with `validate` tags, and provided as `*T`. Loading happens while the
application starts, so bad configuration stops `gonest.New` with every
problem listed.

## Declaring the configuration

<!-- snippet: examples/guide/config_test.go#config-types -->
```go
// AppConfig is the root configuration. Field names become UPPER_SNAKE
// variables unless `env` names them; nested structs get a prefix.
type AppConfig struct {
	Port     int           `default:"8080" validate:"min=1,max=65535"` // PORT
	APIKey   string        `secret:"true" validate:"required"`         // API_KEY
	Timeout  time.Duration `env:"HTTP_TIMEOUT" default:"5s"`
	Origins  []string      `default:"http://localhost:3000"` // comma separated
	Database DBConfig      `envPrefix:"DB_"`
}

// DBConfig is a nested section: DB_HOST, DB_POOL_SIZE.
type DBConfig struct {
	Host     string `default:"localhost"`
	PoolSize int    `default:"10"`
}
```

Tags:

| Tag | Meaning |
|---|---|
| `env:"NAME"` | variable name; default is the field name in UPPER_SNAKE (`APIKey` -> `API_KEY`, `PoolSize` -> `POOL_SIZE`) |
| `env:"-"` | skip the field |
| `default:"..."` | value when the variable is not set (an empty but set variable counts as set) |
| `validate:"..."` | validator rules, checked after every value converted successfully |
| `secret:"true"` | never printed: hidden by `config.Redacted` and in error messages |
| `envPrefix:"DB_"` | prefix for a nested struct; without it the prefix is the field name (`Database` -> `DATABASE_`); embedded structs add none |

Supported types: strings, bools, integers, floats, `time.Duration`,
`time.Time` (RFC 3339), `url.URL`, `encoding.TextUnmarshaler`, pointers to
these, and comma separated slices of them.

Precedence, lowest to highest: `default` tag, `.env` files (later files win),
real environment variables. `Options.Prefix` is prepended to every name,
including explicit `env` names.

## Loading directly

<!-- snippet: examples/guide/config_test.go#config-load -->
```go
opts := config.Options{
	Prefix:   "APP_",                                     // APP_PORT, APP_DB_HOST, ...
	EnvFiles: []string{envFile},                          // default: .env (optional)
	Lookup:   env(map[string]string{"APP_PORT": "9000"}), // default: os.LookupEnv
}
cfg, err := config.Load[AppConfig](opts)
```

Here `env` is a small helper turning a map into a `Lookup` function, as tests
do. With `APP_API_KEY` and `APP_DB_HOST` in the `.env` file this gives:

```text
9000 5s [http://localhost:3000] db.local 10
AppConfig{Port:9000, APIKey:******, Timeout:5s, Origins:[http://localhost:3000], Database:DBConfig{Host:db.local, PoolSize:10}}
```

Errors are a `*config.Error` listing every problem by variable name.
Conversion errors are reported first; validation only runs when every value
converted (so `APP_PORT=0` is not reported while `APP_DB_POOL_SIZE=many` is
broken):

```text
config: 1 problem(s): APP_DB_POOL_SIZE: invalid value "many": strconv.ParseInt: parsing "many": invalid syntax
```

The `.env` parser (also exported as `config.ParseEnv`) supports
`KEY=VALUE`, `export KEY=...`, `#` comments, single quotes (literal) and
double quotes (`\n \t \r \" \\` escapes). A missing `.env` file is ignored
unless `RequireEnvFiles` is set. There is no variable expansion and no YAML.

## As a module

<!-- snippet: examples/guide/config_test.go#config-module -->
```go
root := &gonest.Module{
	Name: "app",
	Imports: []*gonest.Module{
		// Global: *AppConfig is injectable in every module.
		config.ForRoot[AppConfig](config.Options{Prefix: "APP_", Lookup: env(map[string]string{"APP_API_KEY": "k"})}),
		// Not global: import it where *DBConfig is needed.
		config.ForFeature[DBConfig](config.Options{Prefix: "APP_DB_", Lookup: env(nil)}),
	},
}
```

- `config.ForRoot[T](opts)` returns the global module `"config"` providing
  and exporting `*T` (set `NotGlobal` to require explicit imports).
- `config.ForFeature[T](opts)` returns a non-global module (`config:<type>`)
  for a section that only some modules import.

A failing configuration stops `gonest.New`:

```text
gonest: cannot resolve *guide_test.AppConfig in module config: constructor failed: config: 1 problem(s): API_KEY: failed validation "required"
```

## In the SaaS example

<!-- snippet: examples/saas/internal/settings/settings.go#config-struct -->
```go
// Config is loaded by config.ForRoot from default tags, .env files and the
// environment (SAAS_PORT, SAAS_JWT_SECRET, ...), then validated at bootstrap.
type Config struct {
	// Port is the HTTP listen port.
	Port int `env:"PORT" default:"8080" validate:"min=1,max=65535"`
	// JWTSecret signs access tokens (HS256). It has no default: a secret
	// shipped in source code lets anyone forge tokens. Generate one with
	// `openssl rand -hex 32`. Never logged: see config.Redacted.
	JWTSecret string `env:"JWT_SECRET" secret:"true" validate:"required,min=32"`
	// BcryptCost is the work factor of password hashes (bcrypt.DefaultCost
	// is 10). Tests lower it to bcrypt.MinCost, 4, to run fast.
	BcryptCost int `env:"BCRYPT_COST" default:"10" validate:"min=4,max=31"`
	// TokenTTL is the lifetime of issued access tokens.
	TokenTTL time.Duration `env:"TOKEN_TTL" default:"1h" validate:"gt=0"`
}
```

The root module loads it with `Prefix: "SAAS_"`, so the variables are
`SAAS_PORT`, `SAAS_JWT_SECRET`, `SAAS_BCRYPT_COST` and `SAAS_TOKEN_TTL`.
`SAAS_JWT_SECRET` has no default on purpose: a secret committed to source
code lets anyone forge tokens, so the application refuses to start until one
is set (`examples/saas/.env.example` lists every variable). `main` reads the
port with `gonest.Get[*settings.Config](app)` and logs `config.Redacted(cfg)`:

```text
level=INFO msg=configuration config="Config{Port:8080, JWTSecret:******, BcryptCost:10, TokenTTL:1h0m0s}"
```

Tests pass `Lookup` and an empty `EnvFiles` so they never read the real
environment (see [Testing](testing.md)).

Options of other dynamic modules (`logger.ForRoot`, `WithShutdownTimeout`)
are fixed when the module or application is built, before the configuration
is loaded. To derive them from the configuration, provide the value with a
factory that injects `*T` (for example
`gonest.Factory[*slog.Logger](func(c *Config) *slog.Logger { ... })`), or call
`config.Load` in `main` first.
