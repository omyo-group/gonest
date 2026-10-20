package openapi

import (
	"encoding"
	"encoding/json"
	"mime/multipart"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/omyo/gonest"
)

var (
	timeType       = reflect.TypeFor[time.Time]()
	durationType   = reflect.TypeFor[time.Duration]()
	rawMessageType = reflect.TypeFor[json.RawMessage]()
	fileHeaderType = reflect.TypeFor[*multipart.FileHeader]()
	errorBodyType  = reflect.TypeFor[gonest.ErrorBody]()
	textMarshaler  = reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshal  = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// schemaKey identifies a component: the same Go type may produce two schemas,
// the full type and the JSON-body-only view of a handler input struct.
type schemaKey struct {
	t    reflect.Type
	body bool
}

// schemaGen converts Go types to schemas and collects named struct schemas as
// components. It is not safe for concurrent use; Generate creates one per call.
type schemaGen struct {
	components map[string]*Schema
	names      map[schemaKey]string
	taken      map[string]bool
}

func newSchemaGen() *schemaGen {
	return &schemaGen{
		components: map[string]*Schema{},
		names:      map[schemaKey]string{},
		taken:      map[string]bool{},
	}
}

// reserve fixes the component name of a type before its schema is built, so
// recursive types resolve to a $ref.
func (g *schemaGen) reserve(name string) { g.taken[name] = true }

var (
	pathPrefixRe = regexp.MustCompile(`[A-Za-z0-9_.\-]*/`)
	badNameRe    = regexp.MustCompile(`[^A-Za-z0-9._\-]+`)
)

// componentName builds "pkg.Type" from a Go type, sanitised to the characters
// OpenAPI allows in component names. Generic arguments are folded in:
// "pkg.Page[example.com/m/users.User]" becomes "pkg.Page_users.User".
func componentName(t reflect.Type) string {
	s := t.String()
	s = pathPrefixRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("[", "_", "]", "", ",", "_", "*", "").Replace(s)
	s = badNameRe.ReplaceAllString(s, "_")
	if s == "" {
		s = "Object"
	}
	return s
}

func (g *schemaGen) nameFor(k schemaKey) (name string, existed bool) {
	if n, ok := g.names[k]; ok {
		return n, true
	}
	base := componentName(k.t)
	if k.t == errorBodyType && !k.body {
		base = "ErrorBody"
	}
	if k.body {
		base += "Body"
	}
	name = base
	for i := 2; g.taken[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	g.names[k] = name
	g.reserve(name)
	return name, false
}

func ref(name string) *Schema { return &Schema{Ref: "#/components/schemas/" + name} }

// schemaOf returns the schema of t. Struct types become components and are
// returned as $ref; pointers add "null" to the type.
func (g *schemaGen) schemaOf(t reflect.Type) *Schema {
	return g.build(t, true)
}

// build is schemaOf with control over nullability of pointers.
func (g *schemaGen) build(t reflect.Type, nullable bool) *Schema {
	if t.Kind() == reflect.Pointer {
		inner := g.build(t.Elem(), false)
		if !nullable {
			return inner
		}
		return makeNullable(inner)
	}
	return g.base(t)
}

func makeNullable(s *Schema) *Schema {
	if s.Ref != "" || len(s.OneOf) > 0 {
		return &Schema{OneOf: []*Schema{s, {Type: SchemaType{"null"}}}}
	}
	if len(s.Type) == 0 {
		return s // an untyped schema already admits null
	}
	c := *s
	c.Type = append(append(SchemaType{}, s.Type...), "null")
	return &c
}

// base builds the schema of a non-pointer type.
func (g *schemaGen) base(t reflect.Type) *Schema {
	switch {
	case t == timeType:
		return &Schema{Type: SchemaType{"string"}, Format: "date-time"}
	case t == durationType:
		return &Schema{Type: SchemaType{"string"}}
	case t == rawMessageType:
		return &Schema{}
	case t == fileHeaderType.Elem():
		return &Schema{Type: SchemaType{"string"}, Format: "binary"}
	case t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textUnmarshal) ||
		reflect.PointerTo(t).Implements(textMarshaler):
		return &Schema{Type: SchemaType{"string"}}
	}
	switch t.Kind() {
	case reflect.Bool:
		return &Schema{Type: SchemaType{"boolean"}}
	case reflect.Int, reflect.Int64:
		return &Schema{Type: SchemaType{"integer"}, Format: "int64"}
	case reflect.Int8, reflect.Int16, reflect.Int32:
		s := &Schema{Type: SchemaType{"integer"}}
		if t.Kind() == reflect.Int32 {
			s.Format = "int32"
		}
		return s
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		zero := 0.0
		return &Schema{Type: SchemaType{"integer"}, Minimum: &zero}
	case reflect.Float32:
		return &Schema{Type: SchemaType{"number"}, Format: "float"}
	case reflect.Float64:
		return &Schema{Type: SchemaType{"number"}, Format: "double"}
	case reflect.String:
		return &Schema{Type: SchemaType{"string"}}
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: SchemaType{"string"}, Format: "byte"}
		}
		return &Schema{Type: SchemaType{"array"}, Items: g.schemaOf(t.Elem())}
	case reflect.Map:
		return &Schema{Type: SchemaType{"object"}, AdditionalProperties: g.schemaOf(t.Elem())}
	case reflect.Struct:
		return g.structRef(t, false)
	case reflect.Interface:
		return &Schema{}
	default:
		return &Schema{}
	}
}

// structRef registers the component for a struct type and returns a $ref.
func (g *schemaGen) structRef(t reflect.Type, body bool) *Schema {
	name, existed := g.nameFor(schemaKey{t: t, body: body})
	if !existed {
		g.components[name] = nil // placeholder: recursion resolves to the $ref
		g.components[name] = g.structSchema(t, body)
	}
	return ref(name)
}

// field is one exported struct field with its parsed source tag.
type field struct {
	sf     reflect.StructField
	source string // "path", "query", "header", "form", "json" or "" (plain)
	name   string // client-facing name
	opts   string // comma options of the tag (omitempty...)
}

var sourceTags = []string{"path", "query", "header", "form", "json"}

// fields lists the exported fields of struct type t, flattening embedded
// structs. When tagged is true only fields with a source tag are returned.
func fields(t reflect.Type, tagged bool) []field {
	var out []field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous {
			et := sf.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct && !hasSourceTag(sf) {
				out = append(out, fields(et, tagged)...)
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		f := field{sf: sf}
		for _, src := range sourceTags {
			if v, ok := sf.Tag.Lookup(src); ok {
				f.source = src
				f.name, f.opts, _ = strings.Cut(v, ",")
				break
			}
		}
		if f.source == "" {
			if tagged {
				continue
			}
			// encoding/json semantics for untagged fields.
			f.source = "json"
			f.name = sf.Name
		}
		if f.name == "-" && f.opts == "" {
			continue
		}
		if f.name == "" {
			f.name = sf.Name
		}
		out = append(out, f)
	}
	return out
}

func hasSourceTag(sf reflect.StructField) bool {
	for _, src := range sourceTags {
		if _, ok := sf.Tag.Lookup(src); ok {
			return true
		}
	}
	return false
}

// structSchema builds an object schema for t. With body set, only fields with
// a json tag are included (the JSON body of a handler input).
func (g *schemaGen) structSchema(t reflect.Type, body bool) *Schema {
	s := &Schema{Type: SchemaType{"object"}, Properties: map[string]*Schema{}}
	var fs []field
	if body {
		for _, f := range fields(t, true) {
			if f.source == "json" {
				fs = append(fs, f)
			}
		}
	} else {
		for _, f := range fields(t, false) {
			if f.source == "json" {
				fs = append(fs, f)
			}
		}
	}
	for _, f := range fs {
		fs := g.fieldSchema(f.sf, true)
		s.Properties[f.name] = fs
		if hasRule(f.sf.Tag.Get("validate"), "required") && !strings.Contains(","+f.opts+",", ",omitempty,") {
			s.Required = append(s.Required, f.name)
		}
	}
	if len(s.Properties) == 0 {
		s.Properties = nil
	}
	return s
}

// fieldSchema builds the schema of a struct field: its type, validate rules,
// doc, example and default tags. nullable controls whether pointers add null.
func (g *schemaGen) fieldSchema(sf reflect.StructField, nullable bool) *Schema {
	t := sf.Type
	isPtr := t.Kind() == reflect.Pointer
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	s := g.base(t)
	// Rules are applied to a copy so cached shared schemas stay untouched.
	if s.Ref == "" {
		c := *s
		s = &c
		applyValidate(s, t, sf.Tag.Get("validate"))
	}
	if d := sf.Tag.Get("doc"); d != "" {
		s = withDescription(s, d)
	}
	if ex, ok := sf.Tag.Lookup("example"); ok {
		s.Example = convertValue(t, ex)
	}
	if df, ok := sf.Tag.Lookup("default"); ok {
		s.Default = convertValue(t, df)
	}
	if isPtr && nullable {
		s = makeNullable(s)
	}
	return s
}

// withDescription sets the description; for a bare $ref it wraps the
// reference (3.1 allows siblings of $ref).
func withDescription(s *Schema, d string) *Schema {
	c := *s
	c.Description = d
	return &c
}

// convertValue converts a tag string to a JSON value matching t when possible.
func convertValue(t reflect.Type, v string) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType || t == durationType {
		return v
	}
	switch t.Kind() {
	case reflect.Bool:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	case reflect.Float32, reflect.Float64:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case reflect.Slice, reflect.Array:
		trim := strings.TrimSpace(v)
		if strings.HasPrefix(trim, "[") {
			var out any
			if json.Unmarshal([]byte(trim), &out) == nil {
				return out
			}
		}
		parts := strings.Split(v, ",")
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			out = append(out, convertValue(t.Elem(), strings.TrimSpace(p)))
		}
		return out
	case reflect.Map, reflect.Struct, reflect.Interface:
		var out any
		if json.Unmarshal([]byte(v), &out) == nil {
			return out
		}
	}
	return v
}

// splitRules parses a validate tag into its rules.
func splitRules(tag string) []string {
	if tag == "" {
		return nil
	}
	var out []string
	for _, r := range strings.Split(tag, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// hasRule reports whether a validate tag has the bare rule name, ignoring
// rules after "dive" (they apply to elements).
func hasRule(tag, name string) bool {
	for _, r := range splitRules(tag) {
		if r == "dive" {
			return false
		}
		if r == name {
			return true
		}
	}
	return false
}

// applyValidate maps validator rules onto s, the schema of type t. Rules after
// "dive" apply to the element schema of slices and maps.
func applyValidate(s *Schema, t reflect.Type, tag string) {
	rules := splitRules(tag)
	for i, r := range rules {
		if r != "dive" {
			continue
		}
		rest := strings.Join(rules[i+1:], ",")
		rules = rules[:i]
		if s.Items != nil && s.Items.Ref == "" {
			c := *s.Items
			s.Items = &c
			applyValidate(s.Items, t.Elem(), rest)
		} else if s.AdditionalProperties != nil && s.AdditionalProperties.Ref == "" {
			c := *s.AdditionalProperties
			s.AdditionalProperties = &c
			applyValidate(s.AdditionalProperties, t.Elem(), rest)
		}
		break
	}
	kind := t.Kind()
	isString := kind == reflect.String
	isNum := kind >= reflect.Int && kind <= reflect.Float64 && t != durationType
	isList := kind == reflect.Slice || kind == reflect.Array
	for _, r := range rules {
		name, param, _ := strings.Cut(r, "=")
		switch name {
		case "min", "gte":
			setBound(s, isString, isNum, isList, param, true, false)
		case "max", "lte":
			setBound(s, isString, isNum, isList, param, false, false)
		case "gt":
			setBound(s, isString, isNum, isList, param, true, true)
		case "lt":
			setBound(s, isString, isNum, isList, param, false, true)
		case "len":
			setBound(s, isString, isNum, isList, param, true, false)
			setBound(s, isString, isNum, isList, param, false, false)
		case "oneof":
			if isString || isNum {
				s.Enum = oneOf(t, param)
			}
		case "email":
			if isString {
				s.Format = "email"
			}
		case "url", "uri", "http_url":
			if isString {
				s.Format = "uri"
			}
		case "uuid", "uuid3", "uuid4", "uuid5":
			if isString {
				s.Format = "uuid"
			}
		}
	}
}

func setBound(s *Schema, isString, isNum, isList bool, param string, lower, exclusive bool) {
	switch {
	case isString || isList:
		n, err := strconv.Atoi(param)
		if err != nil {
			return
		}
		if exclusive {
			if lower {
				n++
			} else {
				n--
			}
		}
		switch {
		case isString && lower:
			s.MinLength = &n
		case isString:
			s.MaxLength = &n
		case lower:
			s.MinItems = &n
		default:
			s.MaxItems = &n
		}
	case isNum:
		f, err := strconv.ParseFloat(param, 64)
		if err != nil {
			return
		}
		switch {
		case lower && exclusive:
			s.ExclusiveMinimum = &f
		case lower:
			s.Minimum = &f
		case exclusive:
			s.ExclusiveMaximum = &f
		default:
			s.Maximum = &f
		}
	}
}

// oneOf parses the parameter of a oneof rule ("a b 'c d'") into enum values.
func oneOf(t reflect.Type, param string) []any {
	var out []any
	for _, p := range splitQuoted(param) {
		out = append(out, convertValue(t, p))
	}
	return out
}

func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range s {
		switch {
		case r == '\'':
			inQuote = !inQuote
			has = true
		case r == ' ' && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// isFileField reports whether t is *multipart.FileHeader or a slice of it.
func isFileField(t reflect.Type) bool {
	if t == fileHeaderType {
		return true
	}
	return t.Kind() == reflect.Slice && t.Elem() == fileHeaderType
}
