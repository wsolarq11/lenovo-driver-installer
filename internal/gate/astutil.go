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

// callsTo returns every call in the node whose callee is the named method,
// such as flag.FlagSet.Var. Unlike callsOn it does not pin the receiver, which
// is what a rule about an API (rather than about one value) needs.
func callsTo(n ast.Node, method string) []*ast.CallExpr {
	var out []*ast.CallExpr
	if n == nil {
		return nil
	}
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
			out = append(out, call)
		}
		return true
	})
	return out
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
	return declaredNames(f)[name]
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

// returnsAnyName reports whether the block returns the named value in any
// result position. returnsName requires a single result, which misses a
// `return evidence, vendor` pair; a rule asking "does this branch hand back the
// device evidence" must read every returned expression.
func returnsAnyName(block *ast.BlockStmt, name string) bool {
	found := false
	if block == nil {
		return false
	}
	ast.Inspect(block, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			if qualifiedName(result) == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// switchCases returns every case clause of every switch in a block.
func switchCases(block *ast.BlockStmt) []caseClause {
	var out []caseClause
	if block == nil {
		return nil
	}
	ast.Inspect(block, func(n ast.Node) bool {
		if stmt, ok := n.(*ast.SwitchStmt); ok && stmt.Body != nil {
			out = append(out, clauseFrom(stmt.Body)...)
			return false
		}
		return true
	})
	return out
}

// clauseFrom reads every case clause of one switch body.
func clauseFrom(body *ast.BlockStmt) []caseClause {
	var out []caseClause
	for _, stmt := range body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
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
			if name := qualifiedName(ret.Results[0]); name != "" {
				cc.returns = append(cc.returns, name)
			}
			cc.returns = append(cc.returns, stringLiteralValues(ret.Results[0])...)
		}
		out = append(out, cc)
	}
	return out
}

// namesValue reports whether the clause's case list names the given constant.
func (c caseClause) namesValue(name string) bool {
	for _, value := range c.values {
		if value == name {
			return true
		}
	}
	return false
}

// returnsName reports whether the clause body returns the named value.
func (c caseClause) returnsName(name string) bool {
	for _, value := range c.returns {
		if value == name {
			return true
		}
	}
	return false
}

// tagMatches compares a switch tag against a dotted name, tolerating the
// accessor form: `local.Kind` and `local.Kind()` name the same discriminant, and
// a union that hides its field behind a method must not silently drop the rule
// that reads it.
func tagMatches(expr ast.Expr, tag string) bool {
	if qualifiedName(expr) == tag {
		return true
	}
	if call, ok := expr.(*ast.CallExpr); ok && len(call.Args) == 0 {
		return qualifiedName(call.Fun) == tag
	}
	return false
}

// switchClausesOn returns the case clauses of every switch whose tag renders to
// the given name, so a rule about one discriminant is not satisfied by a case
// label that happens to appear in a different switch. Without the tag filter a
// check for "the placeholder answers undetermined" would match any switch in the
// function that contained both spellings.
func switchClausesOn(block *ast.BlockStmt, tag string) []caseClause {
	var out []caseClause
	if block == nil {
		return nil
	}
	ast.Inspect(block, func(n ast.Node) bool {
		stmt, ok := n.(*ast.SwitchStmt)
		if !ok || stmt.Tag == nil {
			return true
		}
		if !tagMatches(stmt.Tag, tag) {
			return true
		}
		cc := clauseFrom(stmt.Body)
		out = append(out, cc...)
		return true
	})
	return out
}

// calleeIdent returns the identifier a call names, whether the call is spelled
// bare or through a receiver or package qualifier. The identifier is returned
// rather than its name because a type check resolves it to the object it names.
func calleeIdent(call *ast.CallExpr) *ast.Ident {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun
	case *ast.SelectorExpr:
		return fun.Sel
	}
	return nil
}

// calleeName is the same query for the callers that only need the spelling.
func calleeName(call *ast.CallExpr) string {
	if id := calleeIdent(call); id != nil {
		return id.Name
	}
	return ""
}
