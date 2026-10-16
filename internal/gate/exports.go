package gate

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
)

// checkUnusedExports fails on any exported name that nothing outside its own
// package names. Such a name is a false API promise: it invites a consumer the
// package never meant to serve, and it rots into a second surface nobody
// maintains. The rule is that each package's export set equals what its
// consumers use.
//
// The names are resolved through the type checker, so a comment, a string, or a
// local variable that happens to share a package's name cannot make an unused
// export look consumed. That happened: a test-local `app := New(...)` shadowed
// the package qualifier, and a text scan could not tell the difference.
//
// Two things stay exported on purpose:
//   - Methods. A method is never spelled pkg.Name, so no name scan can judge
//     one; a method counts only as evidence that its type's name is needed.
//   - Types another exported declaration names in its signature or struct body.
//     Unexporting one would leave a caller holding a value it cannot name.
//
// Consumers include tests: a test in another directory is a real consumer.
func checkUnusedExports(ctx *Context) error {
	ti, err := ctx.types()
	if err != nil {
		return err
	}

	// The directory an object's package lives in decides whether a use of it
	// comes from outside. Import paths are the link: an object knows its
	// package path, and a file knows its directory.
	dirOfPath := map[string]string{}
	for _, cp := range ti.packages {
		dirOfPath[cp.pkgPath] = cp.dir
	}

	usedOutside := map[types.Object]bool{}
	for _, cp := range ti.packages {
		for ident, obj := range cp.info.Uses {
			if obj == nil || obj.Pkg() == nil {
				continue
			}
			declDir, ok := dirOfPath[obj.Pkg().Path()]
			if !ok {
				continue // a package outside this module
			}
			usingFile := cp.fileAt(ident.Pos())
			if usingFile == nil || usingFile.dir == declDir {
				continue
			}
			usedOutside[obj] = true
		}
	}

	dirs := make([]string, 0, len(ti.byDir))
	for dir := range ti.byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var offenders []string
	for _, dir := range dirs {
		if !under(dir) {
			continue
		}
		cp := ti.byDir[dir]
		for _, obj := range exportedObjects(cp.pkg) {
			if !declaredInProduction(cp, obj) {
				continue // an exported helper that only a test declares
			}
			if usedOutside[obj] {
				continue
			}
			if _, isType := obj.(*types.TypeName); isType && neededByAnotherDecl(cp, obj) {
				continue
			}
			offenders = append(offenders, fmt.Sprintf("%s: %s (%s)",
				ctx.Repo.positionAt(obj.Pos()), obj.Name(), kind(obj)))
		}
	}
	if len(offenders) > 0 {
		return failf("nothing outside its package names these exported identifiers, so they are an "+
			"API that is not an API - unexport them", offenders)
	}
	return nil
}

// fileAt returns the checked file containing a position.
func (cp *checkedPackage) fileAt(pos token.Pos) *file {
	if !pos.IsValid() {
		return nil
	}
	for _, f := range cp.files {
		if pos >= f.ast.Pos() && pos <= f.ast.End() {
			return f
		}
	}
	return nil
}

// declaredInProduction reports whether an object is declared in a non-test file.
func declaredInProduction(cp *checkedPackage, obj types.Object) bool {
	if !obj.Pos().IsValid() {
		return false
	}
	f := cp.fileAt(obj.Pos())
	return f != nil && !f.isTest
}

// neededByAnotherDecl reports whether some other exported declaration in the
// package names this type, in a signature or a struct body. Unexporting it
// would leave that declaration returning something a caller cannot name.
//
// A method's receiver is not counted: to call x.M() nobody spells the type's
// name, so a receiver alone is not a consumer. The text scan this replaced
// could not tell a receiver from a parameter, which is why every type with an
// exported method escaped it.
func neededByAnotherDecl(cp *checkedPackage, target types.Object) bool {
	found := false
	visit := func(t types.Type) {
		if found {
			return
		}
		walkTypes(t, func(named *types.Named) {
			if obj := named.Obj(); obj != nil && obj == target {
				found = true
			}
		})
	}
	for _, obj := range exportedObjects(cp.pkg) {
		if obj == target {
			continue
		}
		visit(obj.Type())
		if tn, ok := obj.(*types.TypeName); ok {
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			methods := types.NewMethodSet(types.NewPointer(named))
			for i := 0; i < methods.Len(); i++ {
				method, ok := methods.At(i).Obj().(*types.Func)
				if !ok || !method.Exported() {
					continue
				}
				sig, ok := method.Type().(*types.Signature)
				if !ok {
					continue
				}
				// Parameters and results only; a receiver is not an API mention.
				visit(sig.Params())
				visit(sig.Results())
			}
		}
	}
	return found
}

// walkTypes visits every named type reachable from t, without looping on a
// recursive type.
func walkTypes(t types.Type, visit func(*types.Named)) {
	seen := map[types.Type]bool{}
	var walk func(types.Type)
	walk = func(t types.Type) {
		if t == nil {
			return
		}
		t = types.Unalias(t)
		if seen[t] {
			return
		}
		seen[t] = true
		switch tt := t.(type) {
		case *types.Named:
			if tt.Obj() != nil {
				visit(tt)
			}
			walk(tt.Underlying())
		case *types.Pointer:
			walk(tt.Elem())
		case *types.Slice:
			walk(tt.Elem())
		case *types.Array:
			walk(tt.Elem())
		case *types.Chan:
			walk(tt.Elem())
		case *types.Map:
			walk(tt.Key())
			walk(tt.Elem())
		case *types.Tuple:
			for i := 0; i < tt.Len(); i++ {
				walk(tt.At(i).Type())
			}
		case *types.Signature:
			walk(tt.Params())
			walk(tt.Results())
		case *types.Struct:
			for i := 0; i < tt.NumFields(); i++ {
				walk(tt.Field(i).Type())
			}
		case *types.Interface:
			for i := 0; i < tt.NumExplicitMethods(); i++ {
				walk(tt.ExplicitMethod(i).Type())
			}
			for i := 0; i < tt.NumEmbeddeds(); i++ {
				walk(tt.EmbeddedType(i))
			}
		case *types.TypeParam:
			walk(tt.Constraint())
		}
	}
	walk(t)
}
