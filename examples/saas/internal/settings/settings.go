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

// docs:end
