package main

import (
	"testing"

	"github.com/omyo-group/gonest/gonesttest"
)

func TestGreet(t *testing.T) {
	app := gonesttest.New(t, AppModule).Compile()
	app.Request("GET", "/hello?name=Ada").Do().
		ExpectStatus(200).
		ExpectJSON(map[string]string{"message": "Hello, Ada!"})
	app.Request("GET", "/hello").Do().
		ExpectStatus(400).
		ExpectErrorMessage("name")
}
