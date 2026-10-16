package gate

// The queries the rule checks above are written in terms of. They exist so a
// check can state the shape it requires ("the body of this function contains no
// != operator") instead of the text a particular formatting produced.

import (
	"go/ast"
	"go/token"
)

// positions maps nodes to the gate's path:line spelling.
func positions[T ast.Node](ctx *Context, nodes []T) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, ctx.Repo.position(n))
	}
	return out
}

// declaredNames collects every package-level name a file declares, including
// the specs inside grouped var and const blocks.
func declaredNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					out[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, id := range s.Names {
						out[id.Name] = true
					}
				}
			}
		}
	}
	return out
}

// enumeratesByLabel reports whether a switch body names its cases with string
// literals rather than routing everything through default.
func enumeratesByLabel(block *ast.BlockStmt) bool {
	for _, clause := range switchCases(block) {
		if len(clause.values) > 0 {
			return true
		}
	}
	return false
}

// classifyBindingLabels splits the switch's labels into those whose arm returns
// true and those whose arm returns something else. A label absent from the
// switch is in neither set.
func classifyBindingLabels(block *ast.BlockStmt) (confirmed, denied map[string]bool) {
	confirmed, denied = map[string]bool{}, map[string]bool{}
	for _, clause := range switchCases(block) {
		for _, value := range clause.values {
			if clause.returnsTrue {
				confirmed[value] = true
			} else {
				denied[value] = true
			}
		}
	}
	return confirmed, denied
}

// keyedOnFamily reports whether installBindingLabel is called with the driver
// family predicate among its arguments.
func keyedOnFamily(f *ast.File) bool {
	for _, call := range callsNamed(f, "installBindingLabel") {
		for _, arg := range call.Args {
			if callsBare(arg, "isSoftwareVersionedDriver") {
				return true
			}
		}
	}
	return false
}

// returnsUndecidedOnNil reports whether the function returns OrderUndecided
// from a guard that tests one of its parameters for nil.
func returnsUndecidedOnNil(body *ast.BlockStmt) bool {
	for _, stmt := range ifStmts(body) {
		if !hasBinaryOp(stmt.Cond, token.EQL) {
			continue
		}
		if !mentionsNil(stmt.Cond) {
			continue
		}
		if returnsName(stmt.Body, "OrderUndecided") {
			return true
		}
	}
	return false
}

// mentionsNil reports whether a node compares something with the nil identifier.
func mentionsNil(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Name == "nil" {
			found = true
		}
		return !found
	})
	return found
}

// comparesOperand reports whether a condition compares something to the named
// dotted identifier.
func comparesOperand(cond ast.Expr, name string) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if qualifiedName(bin.X) == name || qualifiedName(bin.Y) == name {
			found = true
		}
		return !found
	})
	return found
}

// callsNamed returns every call whose callee is spelled with the given name,
// whether bare or through a receiver or package qualifier.
func callsNamed(f *ast.File, name string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if fun.Name == name {
				out = append(out, call)
			}
		case *ast.SelectorExpr:
			if fun.Sel.Name == name {
				out = append(out, call)
			}
		}
		return true
	})
	return out
}

// callsWithArgs reports whether a call to the named function passes the given
// arguments in order. It is how "the comparator is called with these two
// values" is stated without depending on the line it is written on.
func callsWithArgs(n ast.Node, callee string, args ...string) bool {
	return firstCallWithArgs(n, callee, args...) != nil
}

// firstCallWithArgs returns the first call to callee whose leading arguments
// match, or nil.
func firstCallWithArgs(n ast.Node, callee string, args ...string) *ast.CallExpr {
	var found *ast.CallExpr
	if n == nil {
		return nil
	}
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || qualifiedName(call.Fun) != callee || len(call.Args) < len(args) {
			return true
		}
		for i, want := range args {
			if qualifiedName(call.Args[i]) != want {
				return true
			}
		}
		found = call
		return false
	})
	return found
}

// assignsCallToKey reports whether a composite literal assigns the named key
// the result of calling the named function, as in
// `IsApplicable: model.InAutomaticInstallSet(ad.CompareStatus)`.
func assignsCallToKey(f *ast.File, key, callee string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if qualifiedName(kv.Key) != key {
			return true
		}
		call, ok := kv.Value.(*ast.CallExpr)
		if ok && qualifiedName(call.Fun) == callee {
			found = true
		}
		return !found
	})
	return found
}
