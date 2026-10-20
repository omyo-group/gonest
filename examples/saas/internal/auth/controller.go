package auth

import (
	"net/http"

	"github.com/omyo/gonest"
)

// LoginInput is the body of POST /auth/login.
type LoginInput struct {
	Email    string `json:"email" validate:"required,email" example:"alice@acme.test"`
	Password string `json:"password" validate:"required" example:"demo-password"`
}

// TokenResponse is returned by a successful login.
type TokenResponse struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType" example:"Bearer"`
	ExpiresIn   int    `json:"expiresIn" doc:"lifetime in seconds" example:"3600"`
}

// AuthController exposes login and the current user.
type AuthController struct {
	users  *UserStore
	tokens *TokenService
	jwt    *JWTGuard
}

// NewAuthController is the controller constructor; its parameters are injected.
func NewAuthController(users *UserStore, tokens *TokenService, jwt *JWTGuard) *AuthController {
	return &AuthController{users: users, tokens: tokens, jwt: jwt}
}

// Routes implements gonest.Controller.
func (c *AuthController) Routes(r gonest.Router) {
	r.Prefix("/auth")
	r.Use(gonest.Tags("auth"))
	r.POST("/login", gonest.Handle(c.Login),
		gonest.Status(http.StatusOK),
		gonest.Summary("Exchange credentials for an access token"),
		gonest.Responds(http.StatusUnauthorized, "Invalid credentials", gonest.ErrorBody{}))
	r.GET("/me", gonest.Handle(c.Me),
		gonest.UseGuards(c.jwt),
		gonest.Security("bearer"),
		gonest.Summary("The authenticated user"))
}

// Login checks credentials and issues a token.
func (c *AuthController) Login(_ *gonest.Ctx, in LoginInput) (TokenResponse, error) {
	u, ok := c.users.Authenticate(in.Email, in.Password)
	if !ok {
		return TokenResponse{}, gonest.Unauthorized("invalid credentials")
	}
	tok, err := c.tokens.Issue(u)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{AccessToken: tok, TokenType: "Bearer", ExpiresIn: int(c.tokens.TTL().Seconds())}, nil
}

// Me returns the user stored by JWTGuard.
func (c *AuthController) Me(ctx *gonest.Ctx, _ gonest.Empty) (User, error) {
	u, ok := CurrentUser(ctx)
	if !ok {
		return User{}, gonest.Unauthorized("not authenticated")
	}
	return *u, nil
}
