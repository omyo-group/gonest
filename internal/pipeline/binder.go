package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/omyo/gonest/internal/core"
)

// source is where an input field comes from.
type source uint8

const (
	srcNone source = iota
	srcPath
	srcQuery
	srcHeader
	srcForm
	srcJSON
)

// sourceTags lists the binding tags in priority order. Read-only.
var sourceTags = [...]struct {
	tag string
	src source
}{
	{"path", srcPath},
	{"query", srcQuery},
	{"header", srcHeader},
	{"form", srcForm},
	{"json", srcJSON},
}

// String returns the FieldError.Source value of s.
func (s source) String() string {
	switch s {
	case srcPath:
		return "path"
	case srcQuery:
		return "query"
	case srcHeader:
		return "header"
	case srcForm:
		return "form"
	case srcJSON:
		return "body"
	default:
		return ""
	}
}

var (
	fileHeaderType      = reflect.TypeFor[*multipart.FileHeader]()
	fileHeaderSliceType = reflect.TypeFor[[]*multipart.FileHeader]()
)

// bfield is one compiled non-body input field.
type bfield struct {
	index  []int
	goPath string
	name   string
	key    string // canonical header key
	src    source
	typ    reflect.Type

	conv      conv // scalar converter (element converter for slices)
	desc      string
	slice     bool
	elem      reflect.Type // slice element type
	dropEmpty bool         // "" counts as missing (non-string scalars)
	file      int          // 1: *multipart.FileHeader, 2: []*multipart.FileHeader

	hasDef bool
	def    []string
}

// fieldName maps a Go field path to its client name, for validation errors.
type fieldName struct {
	segs []string
	name string
	src  source
}

// binder binds and validates one input type. It is immutable after compileBinder.
type binder struct {
	typ      reflect.Type // struct type
	ptr      bool         // the handler input is *typ
	trivial  bool         // no fields, no validation: use zero
	zero     any
	fields   []bfield
	hasJSON  bool
	reset    [][]int // exported non-body fields zeroed after JSON decoding
	validate *validator.Validate
	names    []fieldName

	// scratch recycles the *typ values that value inputs are bound into.
	// The handler receives a copy (boxing a struct into an interface
	// copies it), and the scratch value is zeroed before it is reused, so
	// nothing the handler sees is ever shared or mutated later.
	scratch sync.Pool
}

// compileBinder analyses t once. t is a struct type or a pointer to one.
func compileBinder(t reflect.Type, v *validator.Validate) (*binder, error) {
	b := &binder{typ: t}
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		b.typ, b.ptr = t.Elem(), true
	} else if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input type %s must be a struct or a pointer to a struct", t)
	}
	var errs []error
	b.walk(b.typ, nil, nil, &errs)
	if len(errs) > 0 {
		return nil, fmt.Errorf("input %s: %w", t, errors.Join(errs...))
	}
	hasForm := false
	for _, f := range b.fields {
		hasForm = hasForm || f.src == srcForm
	}
	if hasForm && b.hasJSON {
		return nil, fmt.Errorf("input %s: cannot mix json and form fields", t)
	}
	if hasValidateTags(b.typ, map[reflect.Type]bool{}) {
		b.validate = v
	}
	if !b.ptr && len(b.fields) == 0 && !b.hasJSON && b.validate == nil {
		b.trivial = true
		b.zero = reflect.Zero(b.typ).Interface()
	}
	return b, nil
}

func (b *binder) walk(st reflect.Type, index []int, path []string, errs *[]error) {
	for i := 0; i < st.NumField(); i++ {
		sf := st.Field(i)
		idx := make([]int, len(index)+1)
		copy(idx, index)
		idx[len(index)] = i
		segs := make([]string, len(path)+1)
		copy(segs, path)
		segs[len(path)] = sf.Name
		goPath := strings.Join(segs, ".")
		src, name, n := sourceOf(sf)
		if n > 1 {
			*errs = append(*errs, fmt.Errorf("field %s has more than one source tag", goPath))
			continue
		}
		if sf.Anonymous && src == srcNone {
			ft := sf.Type
			switch {
			case ft == core.InType:
				continue
			case ft.Kind() == reflect.Struct && ft != timeType:
				b.walk(ft, idx, segs, errs)
				continue
			case ft.Kind() == reflect.Pointer && ft.Elem().Kind() == reflect.Struct:
				*errs = append(*errs, fmt.Errorf("embedded pointer field %s is not supported; embed the struct value", goPath))
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		switch src {
		case srcNone:
			b.reset = append(b.reset, idx)
		case srcJSON:
			b.hasJSON = true
			b.names = append(b.names, fieldName{segs: segs, name: name, src: src})
		default:
			f, err := compileField(sf, idx, goPath, name, src)
			if err != nil {
				*errs = append(*errs, err)
				continue
			}
			b.fields = append(b.fields, f)
			b.reset = append(b.reset, idx)
			b.names = append(b.names, fieldName{segs: segs, name: name, src: src})
		}
	}
}

// sourceOf returns the source and client name of a field and how many source
// tags it carries. json:"-" counts as no tag.
func sourceOf(sf reflect.StructField) (source, string, int) {
	var (
		src  source
		name string
		n    int
	)
	for _, st := range sourceTags {
		v, ok := sf.Tag.Lookup(st.tag)
		if !ok {
			continue
		}
		v, _, _ = strings.Cut(v, ",")
		if st.src == srcJSON {
			if v == "-" {
				continue
			}
			if v == "" {
				v = sf.Name
			}
		}
		if v == "" {
			v = sf.Name
		}
		n++
		if src == srcNone {
			src, name = st.src, v
		}
	}
	return src, name, n
}

func compileField(sf reflect.StructField, idx []int, goPath, name string, src source) (bfield, error) {
	f := bfield{index: idx, goPath: goPath, name: name, src: src, typ: sf.Type}
	if src == srcHeader {
		f.key = textproto.CanonicalMIMEHeaderKey(name)
	}
	t := sf.Type
	switch {
	case src == srcForm && t == fileHeaderType:
		f.file = 1
	case src == srcForm && t == fileHeaderSliceType:
		f.file = 2
	case t.Kind() == reflect.Slice && !reflect.PointerTo(t).Implements(textUnmarshalType):
		if src == srcPath {
			return f, fmt.Errorf("field %s: path parameters cannot be slices", goPath)
		}
		c, desc, err := scalarConv(t.Elem())
		if err != nil {
			return f, fmt.Errorf("field %s: %w", goPath, err)
		}
		f.conv, f.desc, f.slice, f.elem = c, desc, true, t.Elem()
	default:
		c, desc, err := scalarConv(t)
		if err != nil {
			return f, fmt.Errorf("field %s: %w", goPath, err)
		}
		f.conv, f.desc = c, desc
		base := t
		if base.Kind() == reflect.Pointer {
			base = base.Elem()
		}
		f.dropEmpty = base.Kind() != reflect.String
	}
	if def, ok := sf.Tag.Lookup("default"); ok {
		if src == srcPath || f.file != 0 {
			return f, fmt.Errorf("field %s: default is only supported for query, header and form values", goPath)
		}
		f.hasDef = true
		f.def = []string{def}
		if err := f.set(reflect.New(t).Elem(), def, f.def); err != nil {
			return f, fmt.Errorf("field %s: invalid default %q: %w", goPath, def, err)
		}
	}
	return f, nil
}

// set converts one value (scalars) or a value list (slices) into dst.
func (f *bfield) set(dst reflect.Value, one string, vals []string) error {
	if !f.slice {
		return f.conv(dst, one)
	}
	n := countItems(vals)
	s := reflect.MakeSlice(f.typ, n, n)
	i := 0
	err := forEachItem(vals, func(item string) error {
		err := f.conv(s.Index(i), item)
		i++
		return err
	})
	if err != nil {
		return err
	}
	dst.Set(s)
	return nil
}

func (f *bfield) typeError() core.FieldError {
	return core.FieldError{Field: f.name, Source: f.src.String(), Rule: "type", Param: f.typ.String(), Message: f.desc}
}

// bind builds the handler input from the request.
func (b *binder) bind(gc *gin.Context) (any, error) {
	if b.trivial {
		return b.zero, nil
	}
	var pv reflect.Value
	if b.ptr {
		pv = reflect.New(b.typ) // handed to the handler: never recycled
	} else {
		pv = b.getScratch()
		defer b.putScratch(pv)
	}
	v := pv.Elem()
	if b.hasJSON {
		decoded, err := decodeJSON(gc.Request, pv.Interface())
		if err != nil {
			return nil, err
		}
		if decoded {
			for _, idx := range b.reset {
				v.FieldByIndex(idx).SetZero()
			}
		}
	}
	var ferrs core.ValidationErrors
	for i := range b.fields {
		f := &b.fields[i]
		if f.file != 0 {
			bindFile(gc, f, v)
			continue
		}
		var (
			one  string
			vals []string
			ok   bool
		)
		switch f.src {
		case srcPath:
			one, ok = gc.Params.Get(f.name)
		case srcQuery:
			vals, ok = gc.GetQueryArray(f.name)
		case srcHeader:
			vals = gc.Request.Header[f.key]
		case srcForm:
			vals, ok = gc.GetPostFormArray(f.name)
		}
		if f.src != srcPath {
			ok = len(vals) > 0
			if ok {
				one = vals[0]
			}
		}
		if f.slice {
			if ok && countItems(vals) == 0 {
				ok = false
			}
		} else if ok && f.dropEmpty && strings.TrimSpace(one) == "" {
			ok = false
		}
		if !ok {
			if !f.hasDef {
				continue
			}
			one, vals = f.def[0], f.def
		}
		if err := f.set(fieldOf(v, f.index), one, vals); err != nil {
			ferrs = append(ferrs, f.typeError())
		}
	}
	if len(ferrs) > 0 {
		return nil, core.BadRequest("validation failed", ferrs)
	}
	if b.validate != nil {
		if err := b.validate.Struct(pv.Interface()); err != nil {
			return nil, b.validationError(err)
		}
	}
	if b.ptr {
		return pv.Interface(), nil
	}
	return v.Interface(), nil
}

// getScratch returns a zero *typ value for binding a value input.
func (b *binder) getScratch() reflect.Value {
	if p := b.scratch.Get(); p != nil {
		return reflect.ValueOf(p)
	}
	return reflect.New(b.typ)
}

// putScratch zeroes pv (dropping every reference it holds, so a slice or
// map decoded into it is never reused by a later request) and recycles it.
func (b *binder) putScratch(pv reflect.Value) {
	pv.Elem().SetZero()
	b.scratch.Put(pv.Interface())
}

func bindFile(gc *gin.Context, f *bfield, v reflect.Value) {
	form, err := gc.MultipartForm()
	if err != nil || form == nil {
		return
	}
	files := form.File[f.name]
	if len(files) == 0 {
		return
	}
	dst := fieldOf(v, f.index)
	if f.file == 1 {
		dst.Set(reflect.ValueOf(files[0]))
		return
	}
	dst.Set(reflect.ValueOf(files))
}

// fieldOf returns the field at index, skipping FieldByIndex's loop for
// top-level fields.
func fieldOf(v reflect.Value, index []int) reflect.Value {
	if len(index) == 1 {
		return v.Field(index[0])
	}
	return v.FieldByIndex(index)
}

// decodeJSON decodes the request body into dst. It reports false when the
// body is empty.
func decodeJSON(r *http.Request, dst any) (bool, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return false, nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			field := te.Field
			if field == "" {
				field = "body"
			}
			return false, core.BadRequest("validation failed", core.ValidationErrors{{
				Field: field, Source: "body", Rule: "type", Param: te.Type.String(),
				Message: "must be of type " + te.Type.String(),
			}}).Wrap(err)
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return false, core.NewHTTPError(http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body exceeds %d bytes", tooLarge.Limit)).Wrap(err)
		}
		return false, core.BadRequest("invalid JSON body").Wrap(err)
	}
	if dec.More() {
		return false, core.BadRequest("invalid JSON body").Wrap(errors.New("unexpected data after the JSON value"))
	}
	return true, nil
}

// hasValidateTags reports whether t or any struct reachable from it carries a
// validate tag.
func hasValidateTags(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == timeType || seen[t] {
		return false
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if _, ok := sf.Tag.Lookup("validate"); ok {
			return true
		}
		if hasValidateTags(sf.Type, seen) {
			return true
		}
	}
	return false
}
