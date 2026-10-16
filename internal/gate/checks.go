package gate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// checkZeroNetworkImport keeps the determinism invariant: only download and api
// may touch the network. The check reads the import block rather than the file
// text, so a mention in a comment or a string no longer counts as an import.
func checkZeroNetworkImport(ctx *Context) error {
	local := map[string]bool{
		"internal/install":   true,
		"internal/compare":   true,
		"internal/inventory": true,
		"internal/plan":      true,
		"internal/audit":     true,
	}
	var offenders []string
	for _, f := range ctx.Repo.prod() {
		if !local[f.dir] {
			continue
		}
		for _, spec := range f.ast.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if path == "net/http" || path == "net/url" {
				offenders = append(offenders, fmt.Sprintf("%s (%s)", ctx.Repo.position(spec), path))
			}
		}
	}
	if len(offenders) > 0 {
		return failf("local side-effect packages must not import net/http or net/url", offenders)
	}
	return nil
}

// The needles below are built by concatenating the two halves of the literal a
// violation would contain. A check that scans the source cannot itself spell the
// forbidden literal, or it becomes its own first hit. `"/delete-" + "driver"`
// matches the argument vector a caller would hand pnputil without leaving a
// literal `"/delete-driver"` in this file.
const (
	deleteDriverArg = "/delete-" + "driver"
	deviceAssign    = "device" + "="
	devicesAssign   = "devices" + "="
	deviceWord      = "device" + " "
)

// checkPackageCleanupBanned holds invariant 6. Rollback is a device-level
// operation - DiRollbackDriver, or reinstalling the previous INF. Deleting the
// published package by INF does not put a device back on its previous driver
// and can leave it with none until PnP re-enumerates, so no code may carry that
// argument vector. The dead primitive that held it was removed; this check
// keeps it gone rather than trusting that nobody re-adds it. Tests are scanned
// too: a test that pins the vector is exactly how the primitive comes back.
func checkPackageCleanupBanned(ctx *Context) error {
	var offenders []string
	for _, f := range ctx.Repo.files {
		if !under(f.dir) {
			continue
		}
		for _, lit := range stringLiterals(f.ast) {
			if unquote(lit) == deleteDriverArg {
				offenders = append(offenders, ctx.Repo.position(lit))
			}
		}
	}
	if len(offenders) > 0 {
		return failf("engine code carries the driver-package delete vector, but package cleanup is "+
			"not rollback and must not exist as an action", offenders)
	}

	// Refusing package cleanup is only meaningful while a real device-level
	// rollback path exists, so pin that side too.
	native, err := ctx.Repo.fileAt("internal/install/rollback_native_windows.go")
	if err != nil {
		return err
	}
	if !mentions(native.ast, "DiRollbackDriver") {
		return fmt.Errorf("the device-level rollback call is gone; refusing package cleanup guards nothing")
	}
	return nil
}

// checkLedgerDeviceIdentity keeps the ledger's Devices column the single
// machine-readable source for which device a row acted on. Re-embedding a
// device id into the free-text Message would let two sources of truth drift
// apart and force downstream parsing.
func checkLedgerDeviceIdentity(ctx *Context) error {
	var offenders []string
	for _, f := range ctx.Repo.prod() {
		if !under(f.dir) {
			continue
		}
		// A literal that merely starts with "device=" is a device id being
		// written into a sentence. The bare "device " literal only counts when
		// something is concatenated onto it, which is the same spelling.
		for _, lit := range stringLiterals(f.ast) {
			value := unquote(lit)
			switch {
			case strings.HasPrefix(value, deviceAssign), strings.HasPrefix(value, devicesAssign):
				offenders = append(offenders, ctx.Repo.position(lit))
			case value == deviceWord && concatenated(f.ast, lit):
				offenders = append(offenders, ctx.Repo.position(lit))
			}
		}
	}
	if len(offenders) > 0 {
		return failf("device identity must ride in the ledger Devices column, not in free-text messages",
			offenders)
	}
	return nil
}

// checkLedgerHashByName keeps chain verification honest. The hash column must
// be found by name in the on-disk header: a positional lookup silently verifies
// the wrong cells once a column is added, so tampering stops being detectable
// without any error surfacing.
func checkLedgerHashByName(ctx *Context) error {
	found := false
	for _, f := range ctx.Repo.dirProd("internal/app") {
		if len(topLevelFuncs(f.ast, "resolveHashIndex")) > 0 {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("internal/app must expose resolveHashIndex (header-name lookup)")
	}

	var offenders []string
	for _, f := range ctx.Repo.prod() {
		if f.dir != "internal" && !strings.HasPrefix(f.dir, "internal/") {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			index, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			if call, ok := index.Index.(*ast.CallExpr); ok && isLenOf(call, "historyColumns") {
				offenders = append(offenders, ctx.Repo.position(index))
			}
			return true
		})
	}
	if len(offenders) > 0 {
		return failf("the ledger is indexed positionally by column count, but the hash column must be "+
			"located by header name", offenders)
	}
	return nil
}

// checkTestsUseTestConstructor keeps tests out of the real audit directory.
// New() points LogPath, PlanPath and HistoryPath at the real per-user audit
// directory, which is the authoritative evidence store on a live machine; a
// test that calls it appends fixture lines there, where they become
// indistinguishable from a real operation. newTestApp redirects every artifact
// into t.TempDir() and is the only sanctioned constructor. The call is resolved
// through the type checker, so a test helper that happens to be named New in
// another package is not mistaken for it.
func checkTestsUseTestConstructor(ctx *Context) error {
	ti, err := ctx.types()
	if err != nil {
		return err
	}
	constructor := ctx.Repo.module + "/internal/app"
	var offenders []string
	for _, pkg := range ti.packages {
		for _, f := range pkg.files {
			if !f.isTest || f.relPath == "internal/app/testapp_test.go" {
				continue
			}
			ast.Inspect(f.ast, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident := calleeIdent(call)
				if ident == nil {
					return true
				}
				callee, _ := pkg.info.Uses[ident].(*types.Func)
				if callee == nil || callee.Pkg() == nil {
					return true
				}
				if callee.Pkg().Path() == constructor && callee.Name() == "New" {
					offenders = append(offenders, ctx.Repo.position(call))
				}
				return true
			})
		}
	}
	if len(offenders) > 0 {
		return failf("tests must construct via newTestApp(t) so audit artifacts stay in t.TempDir()",
			offenders)
	}

	helper, err := ctx.Repo.fileAt("internal/app/testapp_test.go")
	if err != nil {
		return err
	}
	assigned := selectorNamesAssigned(helper.ast)
	for _, artifact := range []string{"LogPath", "PlanPath", "HistoryPath", "driverListCacheDir"} {
		if !slices.Contains(assigned, artifact) {
			return fmt.Errorf("newTestApp must redirect %s into t.TempDir()", artifact)
		}
	}
	if !callsTempDir(helper.ast) {
		return fmt.Errorf("newTestApp must use t.TempDir(); the redirects could point anywhere")
	}
	return nil
}

// mentions reports whether an identifier with the given name appears as a plain
// identifier, as a selector's field, or as the contents of a string literal. The
// string-literal case is how the Win32 rollback entry point is reached:
// newdevRollback.NewProc("DiRollbackDriver") names it in a string, not as an
// identifier.
func mentions(f *ast.File, name string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if node.Name == name {
				found = true
			}
		case *ast.SelectorExpr:
			if node.Sel.Name == name {
				found = true
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING && strings.Contains(unquote(node), name) {
				found = true
			}
		}
		return !found
	})
	return found
}

// concatenated reports whether a literal is an operand of a + expression.
func concatenated(f *ast.File, lit *ast.BasicLit) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if bin.Op != token.ADD {
			return true
		}
		if bin.X == ast.Expr(lit) || bin.Y == ast.Expr(lit) {
			found = true
		}
		return !found
	})
	return found
}

// isLenOf reports whether a call is len(name).
func isLenOf(call *ast.CallExpr, name string) bool {
	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "len" || len(call.Args) != 1 {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	return ok && arg.Name == name
}

// selectorNamesAssigned collects the selector names that appear on the left of
// an assignment, which is how a constructor redirects an artifact path.
func selectorNamesAssigned(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	slices.Sort(out)
	return out
}

// callsTempDir reports whether the file calls t.TempDir().
func callsTempDir(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "TempDir" {
			found = true
		}
		return !found
	})
	return found
}
