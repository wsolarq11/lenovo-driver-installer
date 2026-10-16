package gate

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ExportData maps an import path to the export data file that
// `go list -export -deps -test` produced for it. It is only consulted for
// packages outside this module: the module's own packages are checked from
// source, so that every reference resolves to the declaration it actually
// names.
type ExportData map[string]string

// checkedPackage is one set of files type-checked as a single package.
type checkedPackage struct {
	dir     string // slash-separated directory, relative to the repository root
	pkgPath string // import path the checker used
	pkgName string
	files   []*file
	pkg     *types.Package
	info    *types.Info
}

// typeInfo is the outcome of checking the module from source.
type typeInfo struct {
	packages []*checkedPackage
	byDir    map[string]*checkedPackage
}

// types type-checks the repository once and returns the cached result, so the
// checks that need resolved names share one pass.
func (c *Context) types() (*typeInfo, error) {
	if c.ti == nil && c.tiErr == nil {
		c.ti, c.tiErr = c.Repo.typeCheck(c.Exports)
	}
	return c.ti, c.tiErr
}

// typeCheck type-checks the module's own packages from source.
//
// Test files are checked together with the production files beside them, which
// is what `go test` does and what makes a test's references resolvable; a test
// in another directory is therefore still an outside consumer.
//
// Files the current build excludes (an os or tag constraint) are not checked:
// `go build` already proves the selected set compiles, and a file behind
// `legacyps` uses declarations in the configuration it belongs to. Checks that
// must see every file, excluded or not, read the AST instead.
func (r *Repo) typeCheck(exp ExportData) (*typeInfo, error) {
	tc := &typeChecker{
		repo:    r,
		exports: exp,
		groups:  map[string]*group{},
		checked: map[string]*checkedPackage{},
		byPath:  map[string]string{},
		prodDir: map[string]string{},
	}
	tc.build()
	tc.fallback = importer.ForCompiler(r.fset, "gc", tc.lookup)

	keys := make([]string, 0, len(tc.groups))
	for k := range tc.groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := &typeInfo{byDir: map[string]*checkedPackage{}}
	for _, k := range keys {
		cp, err := tc.checkGroup(tc.groups[k])
		if err != nil {
			return nil, err
		}
		out.packages = append(out.packages, cp)
		if cp.pkgName == tc.prodDir[cp.dir] {
			out.byDir[cp.dir] = cp
		}
	}
	return out, nil
}

// group is a set of files checked as one package.
type group struct {
	key     string
	dir     string
	pkgPath string
	pkgName string
	files   []*file
}

type typeChecker struct {
	repo     *Repo
	exports  ExportData
	groups   map[string]*group
	checked  map[string]*checkedPackage
	byPath   map[string]string // import path -> group key
	prodDir  map[string]string // directory -> production package name
	fallback types.Importer
}

func (tc *typeChecker) lookup(path string) (io.ReadCloser, error) {
	filePath, ok := tc.exports[path]
	if !ok || filePath == "" {
		return nil, fmt.Errorf("no export data recorded for %s", path)
	}
	return os.Open(filepath.FromSlash(filePath))
}

// build groups every in-build file by (directory, package clause).
func (tc *typeChecker) build() {
	byDir := map[string][]*file{}
	for _, f := range tc.repo.buildAll() {
		byDir[f.dir] = append(byDir[f.dir], f)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		files := byDir[dir]
		byName := map[string][]*file{}
		var prodName, pkgPath string
		for _, f := range files {
			byName[f.pkgName] = append(byName[f.pkgName], f)
			if !f.isTest {
				prodName = f.pkgName
				pkgPath = f.pkgPath
			}
		}
		tc.prodDir[dir] = prodName
		names := make([]string, 0, len(byName))
		for n := range byName {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			g := &group{dir: dir, pkgPath: pkgPath, pkgName: name}
			if name == prodName {
				// The production files plus the tests that live in the same
				// package, which is the set `go test` compiles.
				g.files = files
				g.key = dir
				tc.byPath[pkgPath] = g.key
			} else {
				// An external test package (foo_test) is its own package; it can
				// import the one it tests, so it is checked separately.
				g.files = byName[name]
				g.key = dir + "#" + name
				if pkgPath == "" {
					g.pkgPath = tc.repo.module + "/" + dir
				} else {
					g.pkgPath = pkgPath + "_test"
				}
			}
			tc.groups[g.key] = g
		}
	}
}

// checkGroup checks one group, reusing the result on a second request.
func (tc *typeChecker) checkGroup(g *group) (*checkedPackage, error) {
	if cp, ok := tc.checked[g.key]; ok {
		if cp == nil {
			return nil, fmt.Errorf("import cycle through %s", g.pkgPath)
		}
		return cp, nil
	}
	tc.checked[g.key] = nil // a placeholder, so a cycle is reported not hung

	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Scopes:     map[ast.Node]*types.Scope{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var errs []error
	conf := types.Config{
		Importer:  tc,
		Sizes:     types.SizesFor("gc", runtime.GOARCH),
		GoVersion: tc.repo.goVersion,
		Error:     func(err error) { errs = append(errs, err) },
	}
	astFiles := make([]*ast.File, 0, len(g.files))
	for _, f := range g.files {
		astFiles = append(astFiles, f.ast)
	}
	pkg, err := conf.Check(g.pkgPath, tc.repo.fset, astFiles, info)
	if err != nil {
		detail := err.Error()
		if len(errs) > 1 {
			detail = strings.Join(errorStrings(errs), "; ")
		}
		return nil, fmt.Errorf("type-check %s: %s", g.pkgPath, detail)
	}
	cp := &checkedPackage{
		dir:     g.dir,
		pkgPath: g.pkgPath,
		pkgName: g.pkgName,
		files:   g.files,
		pkg:     pkg,
		info:    info,
	}
	tc.checked[g.key] = cp
	return cp, nil
}

func errorStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

// Import resolves an import path. A path inside this module is checked from
// source, so the imported objects are the declarations in this repository
// rather than whatever a compiler left in an archive.
func (tc *typeChecker) Import(path string) (*types.Package, error) {
	if key, ok := tc.byPath[path]; ok {
		if cp, ok := tc.checked[key]; ok {
			if cp == nil {
				return nil, fmt.Errorf("import cycle through %s", path)
			}
			return cp.pkg, nil
		}
		cp, err := tc.checkGroup(tc.groups[key])
		if err != nil {
			return nil, err
		}
		return cp.pkg, nil
	}
	if path == tc.repo.module || strings.HasPrefix(path, tc.repo.module+"/") {
		return nil, fmt.Errorf("no source for in-module import %s", path)
	}
	return tc.fallback.Import(path)
}

// exportedObjects returns the package's exported top-level declarations, sorted
// by name.
func exportedObjects(pkg *types.Package) []types.Object {
	scope := pkg.Scope()
	names := scope.Names()
	out := make([]types.Object, 0, len(names))
	for _, n := range names {
		if obj := scope.Lookup(n); obj != nil && obj.Exported() {
			out = append(out, obj)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// kind names what a declaration is, for the failure message.
func kind(obj types.Object) string {
	switch obj.(type) {
	case *types.Func:
		return "func"
	case *types.TypeName:
		return "type"
	case *types.Var:
		return "var"
	case *types.Const:
		return "const"
	}
	return "decl"
}
