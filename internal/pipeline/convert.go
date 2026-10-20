package pipeline

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// conv parses s into dst. dst is addressable and settable.
type conv func(dst reflect.Value, s string) error

var (
	timeType          = reflect.TypeFor[time.Time]()
	durationType      = reflect.TypeFor[time.Duration]()
	textUnmarshalType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// scalarConv returns the converter for a scalar type (or pointer to one) and
// a client-facing description used in type errors ("must be an integer").
func scalarConv(t reflect.Type) (conv, string, error) {
	if t.Kind() == reflect.Pointer {
		et := t.Elem()
		if et.Kind() == reflect.Pointer {
			return nil, "", fmt.Errorf("unsupported type %s", t)
		}
		c, desc, err := scalarConv(et)
		if err != nil {
			return nil, "", err
		}
		return func(dst reflect.Value, s string) error {
			p := reflect.New(et)
			if err := c(p.Elem(), s); err != nil {
				return err
			}
			dst.Set(p)
			return nil
		}, desc, nil
	}
	switch {
	case t == timeType:
		return convTime, "must be an RFC 3339 date-time", nil
	case t == durationType:
		return convDuration, "must be a duration such as 1h30m", nil
	case reflect.PointerTo(t).Implements(textUnmarshalType):
		return convText, "has an invalid format", nil
	}
	switch t.Kind() {
	case reflect.String:
		return convString, "must be a string", nil
	case reflect.Bool:
		return convBool, "must be a boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := t.Bits()
		return func(dst reflect.Value, s string) error {
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, bits)
			if err != nil {
				return err
			}
			dst.SetInt(n)
			return nil
		}, "must be an integer", nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bits := t.Bits()
		return func(dst reflect.Value, s string) error {
			n, err := strconv.ParseUint(strings.TrimSpace(s), 10, bits)
			if err != nil {
				return err
			}
			dst.SetUint(n)
			return nil
		}, "must be a non-negative integer", nil
	case reflect.Float32, reflect.Float64:
		bits := t.Bits()
		return func(dst reflect.Value, s string) error {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), bits)
			if err != nil {
				return err
			}
			dst.SetFloat(f)
			return nil
		}, "must be a number", nil
	default:
		return nil, "", fmt.Errorf("unsupported type %s", t)
	}
}

func convString(dst reflect.Value, s string) error {
	dst.SetString(s)
	return nil
}

func convBool(dst reflect.Value, s string) error {
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	dst.SetBool(b)
	return nil
}

func convTime(dst reflect.Value, s string) error {
	tm, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return err
	}
	p, _ := dst.Addr().Interface().(*time.Time)
	*p = tm
	return nil
}

func convDuration(dst reflect.Value, s string) error {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	dst.SetInt(int64(d))
	return nil
}

func convText(dst reflect.Value, s string) error {
	u, _ := dst.Addr().Interface().(encoding.TextUnmarshaler)
	return u.UnmarshalText([]byte(s))
}

// forEachItem calls fn for every non-empty, trimmed, comma separated item of vals.
func forEachItem(vals []string, fn func(item string) error) error {
	for _, v := range vals {
		for v != "" {
			var item string
			item, v, _ = strings.Cut(v, ",")
			if item = strings.TrimSpace(item); item == "" {
				continue
			}
			if err := fn(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// countItems returns how many items forEachItem would visit.
func countItems(vals []string) int {
	n := 0
	_ = forEachItem(vals, func(string) error { n++; return nil })
	return n
}
