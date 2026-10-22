package gate

// Type-shape queries: questions about what a declaration *is* (a signature, a
// slice literal) rather than what it calls. Kept apart from the call/expression
// queries so each file answers one kind of question.

import (
	"go/ast"
	"go/token"
)

// hasTypeNamed reports whether a signature mentions the named type in its
// parameters or results. It is how "this function must not take a flag again"
// is stated: the parameter list is the signature, not the line that formats it.
func hasTypeNamed(fn *ast.FuncType, name string) bool {
	if fn == nil {
		return false
	}
	check := func(fields *ast.FieldList) bool {
		if fields == nil {
			return false
		}
		for _, field := range fields.List {
			if qualifiedName(field.Type) == name {
				return true
			}
		}
		return false
	}
	return check(fn.Params) || check(fn.Results)
}

// isStringSliceType reports whether a type expression is []string. It is
// spelled as an ArrayType with an element name, so asking for the qualified name
// of the type node alone would return "" and the rule would never see the table
// it forbids.
func isStringSliceType(expr ast.Expr) bool {
	arr, ok := expr.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	id, ok := arr.Elt.(*ast.Ident)
	return ok && id.Name == "string"
}

// returnsStringSliceLiteral reports whether the file returns a []string built
// from a composite literal, which is what a silent-switch table looks like. The
// rule is that no such table may exist while no switch is verified, so the
// question is whether the literal is there at all.
func returnsStringSliceLiteral(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			lit, ok := result.(*ast.CompositeLit)
			if ok && isStringSliceType(lit.Type) {
				found = true
			}
		}
		return !found
	})
	return found
}

// stringLiteralValues returns every string literal value in a node.
func stringLiteralValues(n ast.Node) []string {
	var out []string
	if n == nil {
		return nil
	}
	ast.Inspect(n, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			out = append(out, unquote(lit))
		}
		return true
	})
	return out
}

// topLevelFunc returns the single top-level function declaration with the given
// name, or nil when there is none.
func topLevelFunc(f *ast.File, name string) *ast.FuncDecl {
	decls := topLevelFuncs(f, name)
	if len(decls) == 0 {
		return nil
	}
	return decls[0]
}

// funcDeclByName returns the declaration of the named function, whether it is
// top-level or a method. topLevelFunc deliberately rejects methods - a method
// is reached through a receiver, so a name-pinning rule that accepted both
// would silently move targets - but some rules genuinely pin the method, and
// they say so by asking here instead.
func funcDeclByName(f *ast.File, name string) *ast.FuncDecl {
	var out *ast.FuncDecl
	ast.Inspect(f, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok && fd.Name.Name == name {
			out = fd
			return false
		}
		return true
	})
	return out
}

// selectorCallName returns the method name of a zero-arg call expression, as in
// `evidence.MeasuredVersion()`. It returns "" for anything else, which is what
// lets a rule ask "is this argument the measured version" without matching text.
func selectorCallName(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

// topLevelType returns the named type declaration in a file, or nil.
func topLevelType(f *ast.File, name string) *ast.TypeSpec {
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == name {
				return ts
			}
		}
	}
	return nil
}

// exportedFields counts a struct type's exported fields. A sealed union keeps
// them at zero so a caller cannot assemble a state the constructors forbid.
func exportedFields(ts *ast.TypeSpec) int {
	st, ok := ts.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return 0
	}
	count := 0
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			if name.IsExported() {
				count++
			}
		}
	}
	return count
}

// topLevelConstNames returns the names declared in a block-form `const (...)`
// whose first value carries the named type. It is how an enumeration's zero
// value is located without depending on a line number or a position.
func topLevelConstNames(f *ast.File, typeName string) []string {
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		var names []string
		matches := false
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := vs.Type.(*ast.Ident); ok && id.Name == typeName {
				matches = true
			}
			for _, name := range vs.Names {
				names = append(names, name.Name)
			}
		}
		if matches && len(names) > 0 {
			return names
		}
	}
	return nil
}

// fileDeclaring returns the parsed file in dir that declares the named
// top-level function. A rule pinned to a function rather than to a filename
// keeps holding when the function moves to a sibling file, which is how this
// repository splits a package as it grows.
func (r *Repo) fileDeclaring(dir, funcName string) (*file, error) {
	for _, f := range r.dirProd(dir) {
		if funcDeclByName(f.ast, funcName) != nil {
			return f, nil
		}
	}
	return nil, &missingDeclError{dir: dir, name: funcName}
}

// fileWithName returns the parsed file in dir that declares the named top-level
// entity, whether it is a function, a type, a constant or a variable. A rule
// about a value (the help text, a struct's shape) has to find its file the same
// way a rule about a function does, so that moving the declaration does not
// break the rule.
func (r *Repo) fileWithName(dir, name string) (*file, error) {
	for _, f := range r.dirProd(dir) {
		if hasTopLevelName(f.ast, name) {
			return f, nil
		}
	}
	return nil, &missingDeclError{dir: dir, name: name}
}

type missingDeclError struct {
	dir  string
	name string
}

func (e *missingDeclError) Error() string {
	return e.name + " is gone from " + e.dir
}
