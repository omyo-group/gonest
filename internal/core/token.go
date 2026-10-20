package core

import (
	"fmt"
	"reflect"
)

// Token identifies a provider in the container: a Go type plus an optional name.
// Two providers may share a type when their names differ.
type Token struct {
	// Type is the type under which the provider is registered and injected.
	Type reflect.Type
	// Name distinguishes several providers of the same Type. Empty for the default.
	Name string
}

// TokenOf returns the unnamed token for type T.
func TokenOf[T any]() Token {
	return Token{Type: reflect.TypeFor[T]()}
}

// NamedTokenOf returns the token for type T registered under name.
func NamedTokenOf[T any](name string) Token {
	return Token{Type: reflect.TypeFor[T](), Name: name}
}

// String renders the token as "*pkg.Type" or "*pkg.Type[name]".
func (t Token) String() string {
	if t.Type == nil {
		return "<nil>"
	}
	if t.Name == "" {
		return t.Type.String()
	}
	return fmt.Sprintf("%s[%s]", t.Type.String(), t.Name)
}

// IsZero reports whether the token has no type.
func (t Token) IsZero() bool { return t.Type == nil }
