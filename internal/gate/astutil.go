package gate

import (
	"go/ast"
	"go/token"
)

// This file holds the small AST queries the rule checks are written in terms of.
// They exist so a check can state the shape it requires ("the body of this
// function contains no != operator") instead of matching the text a
// particular formatting produced. Text matching reads a comment as a
// violation and misses a declaration whose layout changed; an AST query reads
// the program.

// qualifiedName renders a name or a dotted selector chain as one string, so a
// caller can compare it to "model.StatusUnknown" or "compare.Order". It returns
// "" for anything else, which makes a comparison against a target string safely
// false rather than accidentally equal.
func qualifiedName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		prefix := qualifiedName(e.X)
		if prefix == "" {
			return e.Sel.Name
		}
		return prefix + "." + e.Sel.Name
	}
	return ""
}

// identsIn collects every plain identifier name in a node, including the field
// half of a selector. A check that asks whether a body mentions StatusUpdate
// means either spelling.
func identsIn(n ast.Node) map[string]bool {
	out := map[string]bool{}
	if n == nil {
		return out
	}
	ast.Inspect(n, func(node ast.Node) bool {
		switch e := node.(type) {
		case *ast.Ident:
			out[e.Name] = true
		case *ast.SelectorExpr:
			out[e.Sel.Name] = true
		}
		return true
	})
	return out
}

// hasBinaryOp reports whether a node contains an operator of the given kind.
// This is how "the allow-list must not use !=" is asserted: the operator's
// presence is a property of the program, and a comment explaining the rule
// cannot satisfy or break it.
func hasBinaryOp(n ast.Node, op token.Token) bool {
	found := false
	if n == nil {
		return false
	}
	ast.Inspect(n, func(node ast.Node) bool {
		if bin, ok := node.(*ast.BinaryExpr); ok && bin.Op == op {
			found = true
		}
		return !found
	})
	return found
}

// binaryOpsWithOperand returns every binary expression of the given operator
// that has an operand whose qualified name matches one of the names given. It is
// the shape behind "this value must not be compared with ==": the operand is
// what identifies the comparison, not the line it sits on.
func binaryOpsWithOperand(n ast.Node, op token.Token, names ...string) []*ast.BinaryExpr {
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	var out []*ast.BinaryExpr
	if n == nil {
		return nil
	}
	ast.Inspect(n, func(node ast.Node) bool {
		bin, ok := node.(*ast.BinaryExpr)
		if !ok || bin.Op != op {
			return true
		}
		if want[qualifiedName(bin.X)] || want[qualifiedName(bin.Y)] {
			out = append(out, bin)
		}
		return true
	})
	return out
}

// callsQualified reports whether a node calls the dotted function named by
// target, such as "model.InAutomaticInstallSet". Resolution is by name rather
// than by type object because the target is always a function of another
// package in this module, and the call's spelling is exactly what the rule is
// about.
func callsQualified(n ast.Node, target string) bool {
	return anyCall(n, func(fun ast.Expr) bool { return qualifiedName(fun) == target })
}

// callsBare reports whether a node calls a function spelled with a bare
// identifier, such as a package-local helper.
func callsBare(n ast.Node, name string) bool {
	return anyCall(n, func(fun ast.Expr) bool { return qualifiedName(fun) == name })
}

// anyCall reports whether some call in the node has a callee the predicate
// accepts.
func anyCall(n ast.Node, match func(ast.Expr) bool) bool {
	found := false
	if n == nil {
		return false
	}
	ast.Inspect(n, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && match(call.Fun) {
			found = true
		}
		return !found
	})
	return found
}

// callsOn reports whether a node calls method on the value spelled recv, as in
// localVersion.Compare(...). The receiver spelling is what the rule forbids:
// a raw version comparison reaching past the one comparator.
func callsOn(n ast.Node, recv, method string) bool {
	return anyCall(n, func(fun ast.Expr) bool {
		sel, ok := fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return false
		}
		return qualifiedName(sel.X) == recv
	})
}

// hasTopLevelName reports whether the file declares the name at package level,
// as a func, a type, or a value. Value declarations are found inside grouped
// `var (...)` and `const (...)` blocks too, which a line-anchored pattern reads
// as neither a var nor a const and therefore never sees.
func hasTopLevelName(f *ast.File, name string) bool {
	found := false
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == name {
				found = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == name {
						found = true
					}
				case *ast.ValueSpec:
					for _, id := range s.Names {
						if id.Name == name {
							found = true
						}
					}
				}
			}
		}
	}
	return found
}

// ifStmts returns every if statement in a node.
func ifStmts(n ast.Node) []*ast.IfStmt {
	var out []*ast.IfStmt
	if n == nil {
		return nil
	}
	ast.Inspect(n, func(node ast.Node) bool {
		if stmt, ok := node.(*ast.IfStmt); ok {
			out = append(out, stmt)
		}
		return true
	})
	return out
}

// returnsName reports whether the block returns the given dotted name, as in
// `return model.StatusUnknown`.
func returnsName(block *ast.BlockStmt, name string) bool {
	found := false
	if block == nil {
		return false
	}
	ast.Inspect(block, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		if qualifiedName(ret.Results[0]) == name {
			found = true
		}
		return !found
	})
	return found
}

// caseClause is one `case` of a switch, reduced to what the rules ask about:
// the literal values it matches, the values it returns, and whether one of them
// is the boolean true. A rule that says "these labels count as confirmed and
// those do not" is a statement about these clauses, and reading them as clauses
// is what lets one arm carry several labels without the rule missing all but
// the first.
type caseClause struct {
	values      []string
	returns     []string
	returnsTrue bool
}

// switchCases returns every case clause of every switch in a block.
func switchCases(block *ast.BlockStmt) []caseClause {
	var out []caseClause
	if block == nil {
		return nil
	}
	ast.Inspect(block, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		cc := caseClause{}
		for _, expr := range clause.List {
			// A case list may name its labels with string literals ("bound") or
			// with constants (StatusUpdate); both spellings identify the same
			// arm, so both are collected.
			if name := qualifiedName(expr); name != "" {
				cc.values = append(cc.values, name)
			}
			cc.values = append(cc.values, stringLiteralValues(expr)...)
		}
		for _, stmt := range clause.Body {
			ret, ok := stmt.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			if id, ok := ret.Results[0].(*ast.Ident); ok && id.Name == "true" {
				cc.returnsTrue = true
				continue
			}
			cc.returns = append(cc.returns, stringLiteralValues(ret.Results[0])...)
		}
		out = append(out, cc)
		return true
	})
	return out
}

// assignsIdent reports whether the block assigns to a bare identifier, which is
// how a partition appends to its result slice.
func assignsIdent(block *ast.BlockStmt, name string) bool {
	found := false
	if block == nil {
		return false
	}
	ast.Inspect(block, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

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

// fileDeclaring returns the parsed file in dir that declares the named
// top-level function. A rule pinned to a function rather than to a filename
// keeps holding when the function moves to a sibling file, which is how this
// repository splits a package as it grows.
func (r *Repo) fileDeclaring(dir, funcName string) (*file, error) {
	for _, f := range r.dirProd(dir) {
		if topLevelFunc(f.ast, funcName) != nil {
			return f, nil
		}
	}
	return nil, &missingDeclError{dir: dir, name: funcName}
}

type missingDeclError struct {
	dir  string
	name string
}

func (e *missingDeclError) Error() string {
	return e.name + " is gone from " + e.dir
}
