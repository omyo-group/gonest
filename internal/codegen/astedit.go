package codegen

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
)

// ErrNoModuleLiteral means no unambiguous &gonest.Module{...} literal exists.
var ErrNoModuleLiteral = errors.New("no unambiguous &gonest.Module{...} literal found")

// Module fields that RegisterInModule can extend.
const (
	FieldProviders   = "Providers"
	FieldControllers = "Controllers"
)

var fieldTypes = map[string]string{
	FieldProviders:   "[]gonest.Provider",
	FieldControllers: "[]any",
}

// RegisterInModule adds expr (for example "NewUsersService" or
// "gonest.Provide(NewUsersService)") to the Providers or Controllers list of
// the single &gonest.Module literal in src. The literal and list are located
// with go/parser; the new element is spliced at the AST-reported offset and the
// result is re-parsed and formatted with go/format. When expr is already
// present, src is returned unchanged.
func RegisterInModule(src []byte, field, expr string) ([]byte, error) {
	fieldType, ok := fieldTypes[field]
	if !ok {
		return nil, fmt.Errorf("unsupported module field %q", field)
	}
	if _, err := parser.ParseExpr(expr); err != nil {
		return nil, fmt.Errorf("invalid expression %q: %w", expr, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "module.go", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse module file: %w", err)
	}
	lit, err := findModuleLiteral(file)
	if err != nil {
		return nil, err
	}

	var list *ast.CompositeLit
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field {
			list, _ = kv.Value.(*ast.CompositeLit)
			if list == nil {
				return nil, fmt.Errorf("%w: %s is not a list literal", ErrNoModuleLiteral, field)
			}
		}
	}

	var at int
	var text string
	switch {
	case list != nil:
		for _, e := range list.Elts {
			if exprString(fset, src, e) == expr {
				return src, nil
			}
		}
		at, text = spliceInList(fset, src, list, expr)
	default:
		text = field + ": " + fieldType + "{" + expr + "}"
		at, text = spliceInList(fset, src, lit, text)
	}
	out := append(append(append([]byte{}, src[:at]...), text...), src[at:]...)
	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("edit would produce invalid Go: %w", err)
	}
	return formatted, nil
}

func exprString(fset *token.FileSet, src []byte, e ast.Node) string {
	return string(bytes.TrimSpace(src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset]))
}

// spliceInList returns the offset and text that append elem to the composite
// literal lit, keeping single-line literals single-line.
func spliceInList(fset *token.FileSet, src []byte, lit *ast.CompositeLit, elem string) (int, string) {
	lbrace := fset.Position(lit.Lbrace)
	rbrace := fset.Position(lit.Rbrace)
	if len(lit.Elts) == 0 {
		if lbrace.Line == rbrace.Line {
			return lbrace.Offset + 1, elem
		}
		return lbrace.Offset + 1, "\n" + elem + ","
	}
	last := lit.Elts[len(lit.Elts)-1]
	end := fset.Position(last.End())
	if end.Line == rbrace.Line {
		return end.Offset, ", " + elem
	}
	// Multi-line: an existing trailing comma terminates our element.
	i := end.Offset
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	if i < len(src) && src[i] == ',' {
		return end.Offset, ",\n" + elem
	}
	return end.Offset, ",\n" + elem + ","
}

func findModuleLiteral(file *ast.File) (*ast.CompositeLit, error) {
	var found []*ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		cl, ok := u.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Module" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "gonest" {
			found = append(found, cl)
		}
		return true
	})
	if len(found) != 1 {
		return nil, fmt.Errorf("%w (found %d)", ErrNoModuleLiteral, len(found))
	}
	return found[0], nil
}
