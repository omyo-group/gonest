package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"
	"github.com/omyo-group/gonest"
)

// Options configures Load, ForRoot and ForFeature.
type Options struct {
	// Prefix is prepended to every environment variable name (e.g. "APP_").
	Prefix string
	// EnvFiles lists .env files, lowest precedence first (later files override
	// earlier ones). Default: [".env"].
	EnvFiles []string
	// RequireEnvFiles makes a missing .env file an error.
	RequireEnvFiles bool
	// Lookup resolves real environment variables. Default: os.LookupEnv.
	Lookup func(key string) (string, bool)
	// NotGlobal makes ForRoot's module non-global.
	NotGlobal bool
}

// Error aggregates every problem found while loading a configuration.
type Error struct {
	// Problems holds one human readable message per problem, naming the
	// environment variable involved.
	Problems []string
}

// Error implements error.
func (e *Error) Error() string {
	return "config: " + fmt.Sprint(len(e.Problems)) + " problem(s): " + strings.Join(e.Problems, "; ")
}

// ForRoot returns the global "config" module providing and exporting *T.
// Loading happens in the provider constructor, so failures surface at bootstrap.
func ForRoot[T any](opts Options) *gonest.Module {
	return &gonest.Module{
		Name:      "config",
		Global:    !opts.NotGlobal,
		Providers: []gonest.Provider{gonest.Provide(func() (*T, error) { return Load[T](opts) })},
		Exports:   []gonest.Token{gonest.TokenOf[*T]()},
	}
}

// ForFeature returns a non-global module providing and exporting a module-local
// *T section. Import it where needed.
func ForFeature[T any](opts Options) *gonest.Module {
	var zero T
	return &gonest.Module{
		Name:      "config:" + reflect.TypeOf(zero).String(),
		Providers: []gonest.Provider{gonest.Provide(func() (*T, error) { return Load[T](opts) })},
		Exports:   []gonest.Token{gonest.TokenOf[*T]()},
	}
}

// Load builds a *T from default tags, .env files and environment variables
// (lowest to highest precedence), then validates it with `validate` tags. T
// must be a struct type. All problems are reported in a single *Error.
func Load[T any](opts Options) (*T, error) {
	out := new(T)
	rv := reflect.ValueOf(out).Elem()
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("config: type %s is not a struct", rv.Type())
	}
	lookup := opts.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	files := opts.EnvFiles
	if files == nil {
		files = []string{".env"}
	}
	fileVars := map[string]string{}
	var problems []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && !opts.RequireEnvFiles {
				continue
			}
			problems = append(problems, fmt.Sprintf("env file %q: %v", f, err))
			continue
		}
		vars, err := ParseEnv(string(data))
		if err != nil {
			problems = append(problems, fmt.Sprintf("env file %q: %v", f, err))
		}
		for k, v := range vars {
			fileVars[k] = v
		}
	}
	l := &loader{
		lookup:   lookup,
		fileVars: fileVars,
		names:    map[string]string{},
		problems: problems,
	}
	l.walk(rv, opts.Prefix, "")
	if len(l.problems) == 0 {
		l.validate(out, rv.Type().Name())
	}
	if len(l.problems) > 0 {
		return nil, &Error{Problems: l.problems}
	}
	return out, nil
}

type loader struct {
	lookup   func(string) (string, bool)
	fileVars map[string]string
	// names maps a Go field path (without the root type) to its env variable.
	names    map[string]string
	problems []string
}

func (l *loader) resolve(key string) (string, bool) {
	if v, ok := l.lookup(key); ok {
		return v, true
	}
	v, ok := l.fileVars[key]
	return v, ok
}

func (l *loader) walk(sv reflect.Value, prefix, path string) {
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		sf := st.Field(i)
		if !sf.IsExported() || sf.Tag.Get("env") == "-" {
			continue
		}
		fv := sv.Field(i)
		fpath := sf.Name
		if path != "" {
			fpath = path + "." + sf.Name
		}
		if isSection(sf.Type) {
			sub := prefix
			if sf.Anonymous {
				// embedded structs share the parent prefix unless tagged
				if p, ok := sf.Tag.Lookup("envPrefix"); ok {
					sub += p
				}
			} else if p, ok := sf.Tag.Lookup("envPrefix"); ok {
				sub += p
			} else {
				sub += upperSnake(sf.Name) + "_"
			}
			target := fv
			if sf.Type.Kind() == reflect.Ptr {
				target = reflect.New(sf.Type.Elem()).Elem()
			}
			l.walk(target, sub, fpath)
			if sf.Type.Kind() == reflect.Ptr {
				fv.Set(target.Addr())
			}
			continue
		}
		name := sf.Tag.Get("env")
		if name == "" {
			name = upperSnake(sf.Name)
		}
		key := prefix + name
		l.names[fpath] = key
		raw, ok := l.resolve(key)
		if !ok {
			raw, ok = sf.Tag.Lookup("default")
		}
		if !ok {
			continue
		}
		// The raw value and the parse error (which quotes it) are never
		// reported: an untagged field can still hold a credential.
		if err := setValue(fv, raw); err != nil {
			l.problems = append(l.problems, fmt.Sprintf("%s: invalid value, want %s", key, fv.Type()))
		}
	}
}

func (l *loader) validate(out any, root string) {
	err := validator.New(validator.WithRequiredStructEnabled()).Struct(out)
	if err == nil {
		return
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		l.problems = append(l.problems, fmt.Sprintf("validation: %v", err))
		return
	}
	for _, fe := range verrs {
		path := strings.TrimPrefix(fe.StructNamespace(), root+".")
		key, ok := l.names[path]
		if !ok {
			key = path
		}
		msg := fe.Tag()
		if fe.Param() != "" {
			msg += "=" + fe.Param()
		}
		l.problems = append(l.problems, fmt.Sprintf("%s: failed validation %q", key, msg))
	}
}

// isSection reports whether t is a nested configuration struct (not a value
// type parsed from a single string).
func isSection(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	return !isScalarStruct(t)
}

// upperSnake converts GoFieldName to GO_FIELD_NAME (acronyms stay together:
// DBURL -> DBURL, APIKey -> API_KEY).
func upperSnake(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToUpper(c))
	}
	return b.String()
}
