package config

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

const redactedMask = "******"

// Redacted renders v (a struct or pointer to struct) for logging, replacing
// fields tagged `secret:"true"` with "******".
func Redacted(v any) string {
	var b strings.Builder
	writeRedacted(&b, reflect.ValueOf(v), false)
	return b.String()
}

func writeRedacted(b *strings.Builder, v reflect.Value, secret bool) {
	if secret {
		b.WriteString(redactedMask)
		return
	}
	if !v.IsValid() {
		b.WriteString("<nil>")
		return
	}
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			b.WriteString("<nil>")
			return
		}
		if v.Kind() == reflect.Ptr && isScalarStruct(v.Type().Elem()) {
			break
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct && !isScalarStruct(v.Type()) {
		b.WriteString(v.Type().Name())
		b.WriteByte('{')
		first := true
		for i := 0; i < v.NumField(); i++ {
			sf := v.Type().Field(i)
			if !sf.IsExported() {
				continue
			}
			if !first {
				b.WriteString(", ")
			}
			first = false
			b.WriteString(sf.Name)
			b.WriteByte(':')
			writeRedacted(b, v.Field(i), sf.Tag.Get("secret") == "true")
		}
		b.WriteByte('}')
		return
	}
	if !v.CanInterface() {
		return
	}
	if v.Kind() == reflect.Ptr && v.Type().Elem() == urlType {
		v = v.Elem()
	}
	if v.Type() == urlType {
		u := v.Interface().(url.URL)
		b.WriteString(u.Redacted()) // masks the password in user:password@
		return
	}
	fmt.Fprintf(b, "%v", v.Interface())
}
