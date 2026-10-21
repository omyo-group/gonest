package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/omyo/gonest"
)

type level int

func (l *level) UnmarshalText(b []byte) error {
	switch string(b) {
	case "low":
		*l = 1
	case "high":
		*l = 2
	default:
		return errors.New("bad level")
	}
	return nil
}

type dbCfg struct {
	URL      string `env:"URL" validate:"required"`
	PoolSize int    `default:"5" validate:"min=1,max=10"`
	Password string `secret:"true"`
}

type appCfg struct {
	Port     int           `env:"PORT" default:"8080"`
	Debug    bool          `default:"false"`
	Name     string        `env:"APP_NAME" validate:"required"`
	Timeout  time.Duration `default:"3s"`
	Start    time.Time
	Endpoint url.URL
	Home     *url.URL
	Tags     []string
	Ports    []int
	Ratio    float64
	Limit    uint16
	Level    level
	APIKey   string `secret:"true"`
	DB       dbCfg  `envPrefix:"DB_"`
	Cache    *dbCfg `envPrefix:"CACHE_"`
	Skipped  string `env:"-"`
	hidden   string
	Auto     dbCfg
}

func lookupMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoadTypes(t *testing.T) {
	env := map[string]string{
		"PORT": "9000", "DEBUG": "true", "APP_NAME": "x", "TIMEOUT": "1m",
		"START": "2024-01-02T03:04:05Z", "ENDPOINT": "http://a.b/c", "HOME": "http://h",
		"TAGS": "a, b,c", "PORTS": "1,2", "RATIO": "1.5", "LIMIT": "7", "LEVEL": "high",
		"API_KEY": "k", "DB_URL": "u", "DB_PASSWORD": "pw", "CACHE_URL": "cu",
		"AUTO_URL": "au",
	}
	c, err := Load[appCfg](Options{Lookup: lookupMap(env), EnvFiles: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 9000 || !c.Debug || c.Timeout != time.Minute || c.Start.Year() != 2024 {
		t.Errorf("scalars: %+v", c)
	}
	if c.Endpoint.Host != "a.b" || c.Home.Host != "h" {
		t.Errorf("urls: %+v %v", c.Endpoint, c.Home)
	}
	if !reflect.DeepEqual(c.Tags, []string{"a", "b", "c"}) || !reflect.DeepEqual(c.Ports, []int{1, 2}) {
		t.Errorf("slices: %v %v", c.Tags, c.Ports)
	}
	if c.hidden != "" || c.Skipped != "" {
		t.Error("unexported/skipped field set")
	}
	if c.Ratio != 1.5 || c.Limit != 7 || c.Level != 2 || c.APIKey != "k" {
		t.Errorf("misc: %+v", c)
	}
	if c.DB.URL != "u" || c.DB.PoolSize != 5 || c.Cache == nil || c.Cache.URL != "cu" || c.Auto.URL != "au" {
		t.Errorf("nested: %+v %+v", c.DB, c.Cache)
	}
}

func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.env")
	f2 := filepath.Join(dir, "b.env")
	write(t, f1, "PORT=1\nDEBUG=true\nAPP_NAME=file1\nTIMEOUT=9s\n")
	write(t, f2, "PORT=2\nDEBUG=false\n")
	env := map[string]string{"DEBUG": "true"}
	c, err := Load[appCfg](Options{
		Lookup: lookupMap(env), EnvFiles: []string{f1, f2, filepath.Join(dir, "missing.env")},
	})
	// DB_URL missing -> validation error, but we only inspect via a smaller type.
	if err == nil {
		t.Fatal("expected validation error for DB_URL")
	}
	if c != nil {
		t.Fatal("config returned with error")
	}
	type small struct {
		Port    int `env:"PORT" default:"0"`
		Debug   bool
		Name    string `env:"APP_NAME"`
		Timeout time.Duration
		Def     string `default:"dflt"`
	}
	s, err := Load[small](Options{
		Lookup: lookupMap(env), EnvFiles: []string{f1, f2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 2 {
		t.Errorf("later file should override: %d", s.Port)
	}
	if !s.Debug {
		t.Error("real env should beat files")
	}
	if s.Name != "file1" || s.Timeout != 9*time.Second || s.Def != "dflt" {
		t.Errorf("%+v", s)
	}
}

func TestPrefixAndDefaultEnv(t *testing.T) {
	type c struct {
		Host string `env:"HOST"`
		Port int
	}
	t.Setenv("APP_HOST", "h")
	t.Setenv("APP_PORT", "3")
	got, err := Load[c](Options{Prefix: "APP_", EnvFiles: []string{}})
	if err != nil || got.Host != "h" || got.Port != 3 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDefaultEnvFileAndRequired(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	type c struct{ A string }
	// no .env -> ignored
	if _, err := Load[c](Options{Lookup: lookupMap(nil)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load[c](Options{Lookup: lookupMap(nil), RequireEnvFiles: true}); err == nil {
		t.Fatal("expected missing env file error")
	}
	write(t, filepath.Join(dir, ".env"), "A=fromfile\n")
	got, err := Load[c](Options{Lookup: lookupMap(nil)})
	if err != nil || got.A != "fromfile" {
		t.Fatalf("%+v %v", got, err)
	}
	write(t, filepath.Join(dir, ".env"), "garbage line\nA=ok\n")
	_, err = Load[c](Options{Lookup: lookupMap(nil)})
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected parse error, got %v", err)
	}
	// unreadable: a directory
	_, err = Load[c](Options{Lookup: lookupMap(nil), EnvFiles: []string{dir}})
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestAggregatedErrors(t *testing.T) {
	env := map[string]string{"PORT": "abc", "DB_POOL_SIZE": "99", "LIMIT": "-1", "LEVEL": "zzz", "API_KEY": "s"}
	_, err := Load[appCfg](Options{Lookup: lookupMap(env), EnvFiles: []string{}})
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("want *Error, got %v", err)
	}
	// conversion problems stop before validation
	for _, want := range []string{"PORT", "LIMIT", "LEVEL"} {
		if !hasProblem(ce, want) {
			t.Errorf("missing %s in %v", want, ce.Problems)
		}
	}
	// secret values never leak
	env = map[string]string{"API_KEY": "s", "DB_POOL_SIZE": "99", "PORTS": "x"}
	_, err = Load[appCfg](Options{Lookup: lookupMap(env), EnvFiles: []string{}})
	if err == nil {
		t.Fatal("want error")
	}

	// validation aggregation
	env = map[string]string{"DB_POOL_SIZE": "99"}
	_, err = Load[appCfg](Options{Lookup: lookupMap(env), EnvFiles: []string{}})
	if !errors.As(err, &ce) {
		t.Fatal(err)
	}
	for _, want := range []string{"APP_NAME", "DB_URL", "DB_POOL_SIZE", "AUTO_URL", "CACHE_URL"} {
		if !hasProblem(ce, want) {
			t.Errorf("missing %s in %v", want, ce.Problems)
		}
	}
	if !strings.Contains(err.Error(), "problem(s)") {
		t.Error(err)
	}
}

func TestSecretConversionNotLeaked(t *testing.T) {
	type c struct {
		N int `secret:"true"`
	}
	_, err := Load[c](Options{Lookup: lookupMap(map[string]string{"N": "topsecret"}), EnvFiles: []string{}})
	if err == nil || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("leak or no error: %v", err)
	}
}

func hasProblem(e *Error, sub string) bool {
	for _, p := range e.Problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

func TestLoadNonStruct(t *testing.T) {
	if _, err := Load[int](Options{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnsupportedType(t *testing.T) {
	type c struct{ M map[string]string }
	_, err := Load[c](Options{Lookup: lookupMap(map[string]string{"M": "x"}), EnvFiles: []string{}})
	if err == nil {
		t.Fatal("expected unsupported type")
	}
}

func TestUpperSnake(t *testing.T) {
	for in, want := range map[string]string{
		"Port": "PORT", "PoolSize": "POOL_SIZE", "APIKey": "API_KEY",
		"DBURL": "DBURL", "HTTP2Enabled": "HTTP2_ENABLED", "A": "A",
	} {
		if got := upperSnake(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestParseEnv(t *testing.T) {
	src := "\xef\xbb\xbf# comment\n\nA=1\nexport B = two \nC=\"line\\nbreak \\\"q\\\" \\\\ \\t\\r\\x\"\nD='lit\\n # x'\nE=val # trailing\nF=val\t# tab\nG=\nH=a=b\nI=\"x\" # c\r\nA=override\n"
	got, err := ParseEnv(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"A": "override", "B": "two", "C": "line\nbreak \"q\" \\ \t\r\\x", "D": "lit\\n # x",
		"E": "val", "F": "val", "G": "", "H": "a=b", "I": "x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
	for _, bad := range []string{"noequals", "=v", "A='x", "A=\"x", "A=\"x\\"} {
		if _, err := ParseEnv(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestRedacted(t *testing.T) {
	u, _ := url.Parse("http://h")
	c := appCfg{
		Name: "n", APIKey: "supersecret", Home: u, Tags: []string{"a"},
		DB: dbCfg{URL: "u", Password: "pw"}, Timeout: time.Second,
	}
	out := Redacted(&c)
	if strings.Contains(out, "supersecret") || strings.Contains(out, "pw}") || strings.Contains(out, "pw,") {
		t.Errorf("leak: %s", out)
	}
	for _, want := range []string{"APIKey:******", "Password:******", "Name:n", "Home:http://h", "Timeout:1s", "Cache:<nil>", "Tags:[a]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if got := Redacted(c); got != out {
		t.Error("value and pointer differ")
	}
	if Redacted(nil) != "<nil>" || Redacted(5) != "5" {
		t.Error("non-struct")
	}
	if !strings.Contains(Redacted(struct{ E url.URL }{*u}), "E:http://h") {
		t.Error("url value")
	}
}

func TestModules(t *testing.T) {
	type g struct {
		Name string `default:"x"`
	}
	m := ForRoot[g](Options{EnvFiles: []string{}})
	if m.Name != "config" || !m.Global || len(m.Providers) != 1 || len(m.Exports) != 1 {
		t.Fatalf("%+v", m)
	}
	if ForRoot[g](Options{NotGlobal: true}).Global {
		t.Error("NotGlobal ignored")
	}
	if m.Exports[0] != gonest.TokenOf[*g]() || m.Providers[0].Token != gonest.TokenOf[*g]() {
		t.Error("token mismatch")
	}
	f1 := ForFeature[g](Options{})
	f2 := ForFeature[dbCfg](Options{})
	if f1.Global || f1.Name == f2.Name || f1.Exports[0] == f2.Exports[0] {
		t.Error("feature modules not isolated")
	}
	// the constructor loads
	ctor := m.Providers[0].Ctor.(func() (*g, error))
	v, err := ctor()
	if err != nil || v.Name != "x" {
		t.Fatalf("%v %v", v, err)
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUntaggedConversionNotLeaked(t *testing.T) {
	type c struct {
		DSN  url.URL
		Port int
	}
	// "%zz" makes url.Parse fail; its error quotes the whole input.
	env := map[string]string{"DSN": "postgres://u:hunter2@db/%zz", "PORT": "hunter3"}
	_, err := Load[c](Options{Lookup: lookupMap(env), EnvFiles: []string{}})
	if err == nil {
		t.Fatal("want error")
	}
	for _, leak := range []string{"hunter2", "hunter3"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("%s leaked: %v", leak, err)
		}
	}
	for _, want := range []string{"DSN: invalid value, want url.URL", "PORT: invalid value, want int"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestRedactedURLPassword(t *testing.T) {
	u, _ := url.Parse("postgres://user:hunter2@db:5432/app")
	cases := []struct {
		name string
		in   any
	}{
		{"value", struct{ DSN url.URL }{*u}},
		{"pointer", struct{ DSN *url.URL }{u}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Redacted(tc.in)
			if strings.Contains(out, "hunter2") || !strings.Contains(out, "DSN:postgres://user:xxxxx@db:5432/app") {
				t.Errorf("got %s", out)
			}
		})
	}
}
