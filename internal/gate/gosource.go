package gate

// Reading the Go side of a contract as declarations rather than as text. A
// field tag is a struct tag; a doc comment that spells one is prose about the
// shape, not the shape.

import (
	"go/ast"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

func stringConst(f *ast.File, name string) (string, bool) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, id := range vs.Names {
				if id.Name != name || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				return unquote(lit), true
			}
		}
	}
	return "", false
}

// jsonTagsInFile collects the json names of every struct field tag in the file.
// Field tags are declarations; a tag written in a comment is prose about the
// shape, not the shape.
func jsonTagsInFile(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		out = append(out, jsonTagsOf(st.Fields)...)
		return true
	})
	return out
}

// jsonTagsInType collects the json names of one named struct type's fields.
func jsonTagsInType(f *ast.File, typeName string) ([]string, bool) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			return jsonTagsOf(st.Fields), true
		}
	}
	return nil, false
}

// jsonTagsOf reads the json name out of each field's tag. reflect.StructTag is
// the same parser the encoder uses, so `,omitempty` and any future option are
// handled by the standard library rather than by a hand-written split.
func jsonTagsOf(fields *ast.FieldList) []string {
	var out []string
	if fields == nil {
		return nil
	}
	for _, field := range fields.List {
		if field.Tag == nil {
			continue
		}
		tag, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			continue
		}
		name := reflect.StructTag(tag).Get("json")
		if name == "" || name == "-" {
			continue
		}
		if base, _, found := strings.Cut(name, ","); found {
			name = base
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// checkJSONContractsMatchDocs holds contracts.md's two JSON blocks against the
// engine's own tags. export_test.go's golden locks the encoder, not the human
// copy, so without this the two can drift with every test still green.
