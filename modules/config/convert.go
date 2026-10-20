package config

import (
	"encoding"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	timeType     = reflect.TypeOf(time.Time{})
	durationType = reflect.TypeOf(time.Duration(0))
	urlType      = reflect.TypeOf(url.URL{})
	textType     = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// isScalarStruct reports struct types that are parsed from one string.
func isScalarStruct(t reflect.Type) bool {
	return t == timeType || t == urlType || reflect.PointerTo(t).Implements(textType)
}

// setValue parses raw into v according to v's type.
func setValue(v reflect.Value, raw string) error {
	t := v.Type()
	if t.Kind() == reflect.Ptr {
		p := reflect.New(t.Elem())
		if err := setValue(p.Elem(), raw); err != nil {
			return err
		}
		v.Set(p)
		return nil
	}
	switch t {
	case durationType:
		d, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		v.SetInt(int64(d))
		return nil
	case timeType:
		tm, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(tm))
		return nil
	case urlType:
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(*u))
		return nil
	}
	if v.CanAddr() && v.Addr().Type().Implements(textType) {
		return v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(raw))
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 0, t.Bits())
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strings.TrimSpace(raw), 0, t.Bits())
		if err != nil {
			return err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), t.Bits())
		if err != nil {
			return err
		}
		v.SetFloat(f)
	case reflect.Slice:
		return setSlice(v, raw)
	default:
		return fmt.Errorf("unsupported type %s", t)
	}
	return nil
}

func setSlice(v reflect.Value, raw string) error {
	if strings.TrimSpace(raw) == "" {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return nil
	}
	parts := strings.Split(raw, ",")
	s := reflect.MakeSlice(v.Type(), len(parts), len(parts))
	for i, p := range parts {
		if err := setValue(s.Index(i), strings.TrimSpace(p)); err != nil {
			return fmt.Errorf("element %d: %w", i, err)
		}
	}
	v.Set(s)
	return nil
}
