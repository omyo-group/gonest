package projects

import (
	"errors"
	"net/http"

	"github.com/omyo/gonest"
)

// docs:domain-filter

// DomainError is a business rule violation. Services return it without
// knowing about HTTP; DomainErrorFilter turns it into a 422 response.
type DomainError struct {
	Code    string
	Message string
}

// Error implements error.
func (e *DomainError) Error() string { return e.Code + ": " + e.Message }

// DomainErrorFilter is an exception filter (NestJS @Catch(DomainError)).
// It handles *DomainError anywhere in the error chain and lets every other
// error fall through to the next filter (and finally the default one).
var DomainErrorFilter = gonest.ExceptionFilterFunc(func(ctx *gonest.Ctx, err error) bool {
	var de *DomainError
	if !errors.As(err, &de) {
		return false
	}
	body := gonest.UnprocessableEntity(de.Message, map[string]string{"code": de.Code}).Body()
	ctx.Gin().JSON(http.StatusUnprocessableEntity, body)
	return true
})

// docs:end
