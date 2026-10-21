package pipeline

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/omyo-group/gonest/internal/core"
)

// newValidator returns the default validator. Field names in its errors are
// the client-facing names taken from the binding tags.
func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(clientName)
	return v
}

// clientName returns the name a client uses for a field: the value of its
// path, query, header, form or json tag. An empty result makes the validator
// fall back to the Go field name.
func clientName(sf reflect.StructField) string {
	for _, st := range sourceTags {
		v, ok := sf.Tag.Lookup(st.tag)
		if !ok {
			continue
		}
		v, _, _ = strings.Cut(v, ",")
		if v == "-" {
			continue
		}
		return v
	}
	return ""
}

// validationError converts a validator error into a 400 HTTPError carrying
// ValidationErrors.
func (b *binder) validationError(err error) error {
	var ves validator.ValidationErrors
	if !errors.As(err, &ves) {
		return fmt.Errorf("gonest: validating %s: %w", b.typ, err)
	}
	out := make(core.ValidationErrors, 0, len(ves))
	for _, fe := range ves {
		out = append(out, b.fieldError(fe))
	}
	return core.BadRequest("validation failed", out).Wrap(err)
}

func (b *binder) fieldError(fe validator.FieldError) core.FieldError {
	structSegs := strings.Split(fe.StructNamespace(), ".")[1:]
	nsSegs := strings.Split(fe.Namespace(), ".")[1:]
	name, src := strings.Join(nsSegs, "."), ""
	if len(structSegs) == len(nsSegs) {
		if n, s, ok := b.lookupName(structSegs, nsSegs); ok {
			name, src = n, s
		}
	}
	if name == "" {
		name = fe.Field()
	}
	return core.FieldError{
		Field:   name,
		Source:  src,
		Rule:    fe.Tag(),
		Param:   fe.Param(),
		Message: ruleMessage(fe),
	}
}

// lookupName finds the bound field matching the Go path structSegs and
// returns its client path, e.g. "address.street" or "items[0].name".
func (b *binder) lookupName(structSegs, nsSegs []string) (string, string, bool) {
	for _, fn := range b.names {
		k := len(fn.segs)
		if k > len(structSegs) {
			continue
		}
		match := true
		for i := 0; i < k-1; i++ {
			if structSegs[i] != fn.segs[i] {
				match = false
				break
			}
		}
		last, got := fn.segs[k-1], structSegs[k-1]
		if !match || (got != last && !strings.HasPrefix(got, last+"[")) {
			continue
		}
		var sb strings.Builder
		sb.WriteString(fn.name)
		sb.WriteString(got[len(last):])
		for _, s := range nsSegs[k:] {
			sb.WriteByte('.')
			sb.WriteString(s)
		}
		return sb.String(), fn.src.String(), true
	}
	return "", "", false
}

// ruleMessage returns a human readable explanation of a failed rule.
func ruleMessage(fe validator.FieldError) string {
	p := fe.Param()
	sized := func(what string) string {
		switch fe.Kind() {
		case reflect.String:
			return fmt.Sprintf("must be %s %s characters long", what, p)
		case reflect.Slice, reflect.Map, reflect.Array:
			return fmt.Sprintf("must contain %s %s items", what, p)
		default:
			return fmt.Sprintf("must be %s %s", what, p)
		}
	}
	switch fe.Tag() {
	case "required", "required_if", "required_unless", "required_with", "required_without",
		"required_with_all", "required_without_all":
		return "is required"
	case "min":
		return sized("at least")
	case "max":
		return sized("at most")
	case "len":
		return sized("exactly")
	case "gt":
		return "must be greater than " + p
	case "gte":
		return "must be greater than or equal to " + p
	case "lt":
		return "must be less than " + p
	case "lte":
		return "must be less than or equal to " + p
	case "eq":
		return "must be equal to " + p
	case "ne":
		return "must not be equal to " + p
	case "oneof":
		return "must be one of: " + strings.Join(strings.Fields(p), ", ")
	case "email":
		return "must be a valid email address"
	case "url", "http_url":
		return "must be a valid URL"
	case "uri":
		return "must be a valid URI"
	case "uuid", "uuid3", "uuid4", "uuid5":
		return "must be a valid UUID"
	case "alpha":
		return "must contain only letters"
	case "alphanum":
		return "must contain only letters and digits"
	case "numeric", "number":
		return "must be numeric"
	case "boolean":
		return "must be a boolean"
	case "datetime":
		return "must be a date-time in the format " + p
	default:
		if p != "" {
			return fmt.Sprintf("failed the %q rule (%s)", fe.Tag(), p)
		}
		return fmt.Sprintf("failed the %q rule", fe.Tag())
	}
}
