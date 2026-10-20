package core

import (
	"runtime"
	"strings"
)

// runtimeFuncName returns a short "Type.Method" name for a function pointer.
func runtimeFuncName(pc uintptr) string {
	f := runtime.FuncForPC(pc)
	if f == nil {
		return "handler"
	}
	name := f.Name() // e.g. github.com/x/app/users.(*UsersController).FindOne-fm
	name = strings.TrimSuffix(name, "-fm")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "."); i >= 0 {
		name = name[i+1:]
	}
	name = strings.NewReplacer("(*", "", ")", "").Replace(name)
	return name
}
