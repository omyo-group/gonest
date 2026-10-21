package auth

import (
	"time"

	"github.com/omyo/gonest"
)

// docs:auth-module

// Module provides token handling and the guards. JWTGuard and RolesGuard are
// exported so feature modules that import auth.Module can inject them into
// their controllers and pass them to gonest.UseGuards.
var Module = &gonest.Module{
	Name: "auth",
	Providers: []gonest.Provider{
		gonest.Value[Clock](time.Now),
		gonest.Provide(NewTokenService),
		gonest.Provide(NewUserStore),
		gonest.Provide(NewLoginLimiter),
		gonest.Provide(NewJWTGuard),
		gonest.Provide(NewRolesGuard),
	},
	Controllers: []any{NewAuthController},
	Exports: []gonest.Token{
		gonest.TokenOf[*JWTGuard](),
		gonest.TokenOf[*RolesGuard](),
		gonest.TokenOf[*TokenService](),
	},
}

// docs:end
