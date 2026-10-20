package di

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/omyo/gonest/internal/core"
)

var errorType = reflect.TypeFor[error]()

// plan is the precomputed injection recipe of a function type: which token
// feeds every parameter and whether the last result is an error. Plans depend
// only on the function type, so they are cached per type.
type plan struct {
	params []paramPlan
	// errOut reports that the last result is of type error.
	errOut bool
}

// paramPlan describes one function parameter: either a plain token or a
// parameter object (a struct embedding core.In).
type paramPlan struct {
	typ    reflect.Type
	tok    core.Token
	object []fieldPlan
	isObj  bool
}

// fieldPlan describes one injected field of a parameter object.
type fieldPlan struct {
	index    int
	typ      reflect.Type
	tok      core.Token
	optional bool
}

// buildPlan computes the plan of a function type. It rejects variadic functions
// and malformed parameter objects.
func buildPlan(ft reflect.Type) (*plan, error) {
	if ft.IsVariadic() {
		return nil, fmt.Errorf("variadic function %s is not supported", ft)
	}
	p := &plan{params: make([]paramPlan, ft.NumIn())}
	for i := range ft.NumIn() {
		in := ft.In(i)
		if !isParamObject(in) {
			p.params[i] = paramPlan{typ: in, tok: core.Token{Type: in}}
			continue
		}
		fields, err := objectFields(in)
		if err != nil {
			return nil, err
		}
		p.params[i] = paramPlan{typ: in, object: fields, isObj: true}
	}
	if n := ft.NumOut(); n > 0 && ft.Out(n-1) == errorType {
		p.errOut = true
	}
	return p, nil
}

// isParamObject reports whether t is a struct embedding core.In.
func isParamObject(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous && f.Type == core.InType {
			return true
		}
	}
	return false
}

func objectFields(t reflect.Type) ([]fieldPlan, error) {
	var out []fieldPlan
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous && f.Type == core.InType {
			continue
		}
		if !f.IsExported() {
			return nil, fmt.Errorf("parameter object %s: field %s is unexported and cannot be injected", t, f.Name)
		}
		fp := fieldPlan{index: i, typ: f.Type, tok: core.Token{Type: f.Type, Name: f.Tag.Get("name")}}
		if s, ok := f.Tag.Lookup("optional"); ok {
			b, err := strconv.ParseBool(s)
			if err != nil {
				return nil, fmt.Errorf("parameter object %s: field %s: invalid optional tag %q", t, f.Name, s)
			}
			fp.optional = b
		}
		out = append(out, fp)
	}
	return out, nil
}

// planFor returns the cached plan of ft, building it if needed. The caller
// holds c.mu for writing.
func (c *Container) planFor(ft reflect.Type) (*plan, error) {
	if p, ok := c.plans[ft]; ok {
		return p, nil
	}
	p, err := buildPlan(ft)
	if err != nil {
		return nil, err
	}
	c.plans[ft] = p
	return p, nil
}

// call invokes fn, turning a trailing error result into err and a panic into
// an error. The returned results exclude the trailing error.
func call(fn reflect.Value, p *plan, args []reflect.Value) (out []reflect.Value, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out, err = nil, &panicError{value: rec}
		}
	}()
	out = fn.Call(args)
	if p.errOut {
		last := out[len(out)-1]
		out = out[:len(out)-1]
		if !last.IsNil() {
			err = last.Interface().(error)
		}
	}
	return out, err
}

// panicError reports a panic raised by a constructor or an invoked function.
type panicError struct{ value any }

func (p *panicError) Error() string { return fmt.Sprintf("panic: %v", p.value) }

// valueOf converts an instance to a reflect.Value of type t, mapping nil to
// the zero value.
func valueOf(v any, t reflect.Type) reflect.Value {
	if v == nil {
		return reflect.Zero(t)
	}
	return reflect.ValueOf(v)
}

func nilable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return true
	default:
		return false
	}
}
