package gate

// docs/spec/behavior.md is the single source for runtime semantics: comparison
// and selection, download integrity, install dispatch, rollback, and the API
// drift escape hatch. Its header requires a change in any of those to be
// mirrored here and in the test golden, and until this check existed nothing
// enforced it - architecture.md and contracts.md were both read by the gate,
// behavior.md was not. The anchors below pin the machine-checkable part of that
// promise without freezing the prose.

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// checkBehaviorDocMatchesEngine holds docs/spec/behavior.md to the runtime
// contract it claims to be the single source for. Unlike architecture.md and
// contracts.md, nothing read it: its own header says a change to comparison,
// integrity, dispatch or rollback must be mirrored here and in the tests, and
// that promise had no enforcement at all. Two anchors keep it honest without
// pinning prose: the CLI flags it names must exist, and it must name every
// CompareStatus value the engine defines (a new status is exactly the kind of
// runtime behavior a reader would come here to learn).
func checkBehaviorDocMatchesEngine(ctx *Context) error {
	behavior, err := ctx.Repo.readFile("docs/spec/behavior.md")
	if err != nil {
		return err
	}

	// The flags the engine registers, read as declarations rather than text.
	app, err := ctx.Repo.fileAt("internal/app/app.go")
	if err != nil {
		return err
	}
	registered := toSet(flagNames(app.ast))
	if len(registered) < 10 {
		return fmt.Errorf("parsed only %d registered CLI flags from app.go; the parse is wrong", len(registered))
	}
	documented := flagsInDoc(behavior)
	if len(documented) < 5 {
		return fmt.Errorf("behavior.md names only %d CLI flags; the parse is wrong", len(documented))
	}
	// One direction only: behavior.md documents runtime semantics, so it names
	// the flags a rule depends on rather than the whole CLI surface (that list
	// belongs to help.go, and the contracts check already holds it). What must
	// never happen is the doc naming a flag the binary no longer accepts - a
	// renamed or deleted switch would leave the spec describing a run shape that
	// cannot be requested.
	var stale []string
	for _, name := range documented {
		if !registered[name] {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("behavior.md names CLI flags the engine does not accept: %s",
			strings.Join(dedupe(stale), ", "))
	}

	// Every status the engine can report must be named here. The engine's set is
	// the single source; the doc must not carry a status that no longer exists,
	// nor omit one that does.
	model, err := ctx.Repo.fileWithName("internal/model", "StatusUpdate")
	if err != nil {
		return err
	}
	statuses := stringConsts(model.ast)
	var known []string
	for name, value := range statuses {
		if strings.HasPrefix(name, "Status") {
			known = append(known, value)
		}
	}
	if len(known) < 6 {
		return fmt.Errorf("parsed only %d CompareStatus values from model.go; the parse is wrong", len(known))
	}
	var missing []string
	for _, status := range known {
		if !strings.Contains(behavior, "`"+status+"`") {
			missing = append(missing, status)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("behavior.md does not name these CompareStatus values: %s", strings.Join(missing, ", "))
	}
	return nil
}

// flagNames reads the names the engine registers through its flag set. A flag
// the binary accepts but the spec omits is a run shape nobody documented.
// flag.FlagSet spells the methods BoolVar, StringVar and so on, so the reader
// matches on the suffix rather than on one method name.
func flagNames(f *ast.File) []string {
	seen := map[string]bool{}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasSuffix(sel.Sel.Name, "Var") || len(call.Args) < 3 {
			return true
		}
		// flag.FlagSet methods take (value, name, default, usage): the name is
		// the second argument, not the third.
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name := strings.Trim(lit.Value, `"`)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return true
	})
	return out
}

// stringConsts returns every name->value pair in the file's const blocks. The
// status set is declared as a block of typed string constants, so the block is
// the reader; a single const would need a different one, which is why an empty
// result is reported as a parse failure rather than a pass.
func stringConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out[name.Name] = strings.Trim(lit.Value, `"`)
			}
		}
	}
	return out
}

// flagsInDoc returns the -Flag spellings behavior.md uses in backticks.
func flagsInDoc(doc string) []string {
	var out []string
	for _, m := range reDocFlag.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1])
	}
	return out
}
