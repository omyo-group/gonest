package codegen

import (
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRegisterInModule(t *testing.T) {
	const head = "package users\n\nimport \"github.com/omyo/gonest\"\n\n"
	tests := []struct {
		name, body, field, expr string
		contains                []string
		unchanged               bool
		wantErr                 error
	}{
		{
			name:     "append multiline with trailing comma",
			body:     "var M = &gonest.Module{\n\tName: \"users\",\n\tControllers: []any{\n\t\tNewA,\n\t},\n}\n",
			field:    FieldControllers,
			expr:     "NewB",
			contains: []string{"NewA,", "NewB,"},
		},
		{
			name:     "append single line",
			body:     "var M = &gonest.Module{Name: \"users\", Controllers: []any{NewA}}\n",
			field:    FieldControllers,
			expr:     "NewB",
			contains: []string{"NewA, NewB"},
		},
		{
			name:     "empty list",
			body:     "var M = &gonest.Module{Name: \"users\", Providers: []gonest.Provider{}}\n",
			field:    FieldProviders,
			expr:     "gonest.Provide(NewS)",
			contains: []string{"[]gonest.Provider{gonest.Provide(NewS)}"},
		},
		{
			name:     "empty multiline list",
			body:     "var M = &gonest.Module{\n\tName: \"users\",\n\tProviders: []gonest.Provider{\n\t},\n}\n",
			field:    FieldProviders,
			expr:     "gonest.Provide(NewS)",
			contains: []string{"gonest.Provide(NewS),"},
		},
		{
			name:     "missing field added multiline",
			body:     "var M = &gonest.Module{\n\tName: \"users\",\n}\n",
			field:    FieldControllers,
			expr:     "NewC",
			contains: []string{"Controllers: []any{NewC}"},
		},
		{
			name:     "missing field added single line",
			body:     "var M = &gonest.Module{Name: \"users\"}\n",
			field:    FieldProviders,
			expr:     "NewC",
			contains: []string{"Providers: []gonest.Provider{NewC}"},
		},
		{
			name:     "comments survive",
			body:     "// M doc.\nvar M = &gonest.Module{\n\tName: \"users\", // the name\n\tControllers: []any{NewA}, // list\n}\n",
			field:    FieldControllers,
			expr:     "NewB",
			contains: []string{"// M doc.", "// the name", "NewA, NewB"},
		},
		{
			name:      "already present",
			body:      "var M = &gonest.Module{Name: \"users\", Controllers: []any{NewA}}\n",
			field:     FieldControllers,
			expr:      "NewA",
			unchanged: true,
		},
		{
			name:    "no literal",
			body:    "var M = 1\n",
			field:   FieldControllers,
			expr:    "NewA",
			wantErr: ErrNoModuleLiteral,
		},
		{
			name:    "two literals",
			body:    "var M = &gonest.Module{Name: \"a\"}\nvar N = &gonest.Module{Name: \"b\"}\n",
			field:   FieldControllers,
			expr:    "NewA",
			wantErr: ErrNoModuleLiteral,
		},
		{
			name:    "field not a literal",
			body:    "var M = &gonest.Module{Name: \"a\", Controllers: list()}\nfunc list() []any { return nil }\n",
			field:   FieldControllers,
			expr:    "NewA",
			wantErr: ErrNoModuleLiteral,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(head + tc.body)
			out, err := RegisterInModule(src, tc.field, tc.expr)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.unchanged && string(out) != string(src) {
				t.Fatalf("source changed:\n%s", out)
			}
			for _, c := range tc.contains {
				if !strings.Contains(string(out), c) {
					t.Errorf("output lacks %q:\n%s", c, out)
				}
			}
			if _, err := parser.ParseFile(token.NewFileSet(), "x.go", out, 0); err != nil {
				t.Fatalf("output does not parse: %v\n%s", err, out)
			}
		})
	}
}

func TestRegisterInModuleErrors(t *testing.T) {
	src := []byte("package a\n\nvar M = &gonest.Module{}\n")
	if _, err := RegisterInModule(src, "Imports", "X"); err == nil {
		t.Error("unsupported field accepted")
	}
	if _, err := RegisterInModule(src, FieldProviders, "(("); err == nil {
		t.Error("invalid expression accepted")
	}
	if _, err := RegisterInModule([]byte("not go"), FieldProviders, "X"); err == nil {
		t.Error("invalid source accepted")
	}
}
