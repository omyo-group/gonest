package settings

import "time"

// EnvPrefix is prepended to every environment variable read by Config.
const EnvPrefix = "SAAS_"

// docs:config-struct

// Config is loaded by config.ForRoot from default tags, .env files and the
// environment (SAAS_PORT, SAAS_JWT_SECRET, ...), then validated at bootstrap.
type Config struct {
	// Port is the HTTP listen port.
	Port int `env:"PORT" default:"8080" validate:"min=1,max=65535"`
	// JWTSecret signs access tokens (HS256). Never logged: see config.Redacted.
	JWTSecret string `env:"JWT_SECRET" default:"dev-only-secret-change-me" secret:"true" validate:"min=16"`
	// TokenTTL is the lifetime of issued access tokens.
	TokenTTL time.Duration `env:"TOKEN_TTL" default:"1h" validate:"gt=0"`
}

// docs:end
