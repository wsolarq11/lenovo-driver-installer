// Package gate implements the offline gate's source checks over this
// repository's Go code.
//
// These checks used to be regular expressions run from scripts/verify.ps1. A
// regular expression cannot tell a call from a comment, from a string, or from
// a local variable that happens to share a package's name, and two real defects
// came from exactly that: a type was reported as having no consumer because two
// methods in its package were both named Error, and a test-local variable named
// app made a package qualifier look like a use. Parsing the source - and, where
// the question is which name means what, type-checking it - removes that class
// of error.
//
// The checks then live in a package that is compiled, vetted and tested like
// the rest of the module, instead of being text nobody dares touch. This
// package exports only what cmd/gatecheck names, and it applies the same rule
// to itself.
//
// The package performs no I/O beyond reading the repository and, when a check
// asks for types, the export data the caller supplies. It never runs a process.
package gate

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// file is one parsed Go source file.
type file struct {
	relPath string // slash-separated, relative to the repository root
	dir     string // slash-separated package directory, relative to the root
	pkgPath string // full import path, module path included
	pkgName string // package clause
	isTest  bool   // file name ends in _test.go
	inBuild bool   // false when this host's build constraints exclude the file
	ast     *ast.File
}

// Repo is a parsed repository.
type Repo struct {
	root      string
	module    string
	goVersion string // the go.mod go directive, as a go/types version string
	fset      *token.FileSet
	files     []*file
}

// Load parses every .go file under internal/ and cmd/.
//
// Every file is kept, including the ones the current build excludes: the text
// checks looked at all of them, and a file carved out by a build tag is exactly
// where a rule can hide. Checks that need a coherent package take the in-build
// subset instead.
func Load(root string) (*Repo, error) {
	module, goVersion, err := readGoMod(root)
	if err != nil {
		return nil, err
	}
	ctx := build.Default
	ctx.BuildTags = nil

	r := &Repo{root: root, module: module, goVersion: goVersion, fset: token.NewFileSet()}
	for _, top := range []string{"internal", "cmd"} {
		base := filepath.Join(root, top)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			inBuild, err := ctx.MatchFile(filepath.Dir(path), d.Name())
			if err != nil {
				// An unreadable constraint is not a reason to skip the file:
				// treat it as in-build so its contents are still checked.
				inBuild = true
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			parsed, err := parser.ParseFile(r.fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("parse %s: %w", rel, err)
			}
			dir := filepath.ToSlash(filepath.Dir(rel))
			r.files = append(r.files, &file{
				relPath: rel,
				dir:     dir,
				pkgPath: module + "/" + dir,
				pkgName: parsed.Name.Name,
				isTest:  strings.HasSuffix(d.Name(), "_test.go"),
				inBuild: inBuild,
				ast:     parsed,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(r.files, func(i, j int) bool { return r.files[i].relPath < r.files[j].relPath })
	return r, nil
}

// readGoMod returns the module path and the go directive. The go directive
// matters to the type checker: without it, syntax newer than the checker's
// default is rejected even though `go build` accepts it.
func readGoMod(root string) (module, goVersion string, err error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			module = strings.TrimSpace(rest)
		}
		if rest, ok := strings.CutPrefix(line, "go "); ok {
			goVersion = "go" + strings.TrimSpace(rest)
		}
	}
	if module == "" {
		return "", "", fmt.Errorf("go.mod in %s declares no module path", root)
	}
	return module, goVersion, nil
}

// prod returns the non-test files, whether or not the build excludes them.
func (r *Repo) prod() []*file { return r.filter(func(f *file) bool { return !f.isTest }) }

// dirProd returns the non-test files of one package directory.
func (r *Repo) dirProd(dir string) []*file {
	return r.filter(func(f *file) bool { return !f.isTest && f.dir == dir })
}

// dirFiles returns every file of one package directory, tests included. A rule
// that pins a test reads this: the test is a declaration in the tree, not a line
// of text.
func (r *Repo) dirFiles(dir string) []*file {
	return r.filter(func(f *file) bool { return f.dir == dir })
}

// buildAll returns the files the current build selects, which is the set a type
// check can treat as one coherent package.
func (r *Repo) buildAll() []*file { return r.filter(func(f *file) bool { return f.inBuild }) }

func (r *Repo) filter(keep func(*file) bool) []*file {
	var out []*file
	for _, f := range r.files {
		if keep(f) {
			out = append(out, f)
		}
	}
	return out
}

// fileAt returns one parsed file by its repository-relative path.
func (r *Repo) fileAt(relPath string) (*file, error) {
	for _, f := range r.files {
		if f.relPath == relPath {
			return f, nil
		}
	}
	return nil, fmt.Errorf("no such file: %s", relPath)
}

// under reports whether a directory is inside the internal tree.
func under(dir string) bool { return dir == "internal" || strings.HasPrefix(dir, "internal/") }

// position renders a node's location as path:line for a failure message.
func (r *Repo) position(n ast.Node) string {
	if n == nil {
		return "?"
	}
	return r.positionAt(n.Pos())
}

// positionAt renders a token position as path:line.
func (r *Repo) positionAt(pos token.Pos) string {
	if !pos.IsValid() {
		return "?"
	}
	p := r.fset.Position(pos)
	name := filepath.ToSlash(p.Filename)
	if rel, err := filepath.Rel(r.root, p.Filename); err == nil {
		name = filepath.ToSlash(rel)
	}
	return fmt.Sprintf("%s:%d", name, p.Line)
}

// Context is everything a check may look at.
type Context struct {
	Repo *Repo
	// Exports maps an import path outside this module to the export data file
	// `go list -export` produced for it. Set it directly when the caller already
	// has the data; leave it nil and set LoadExports to have it fetched on
	// demand. Only the checks that type-check read either field.
	Exports ExportData
	// LoadExports produces the export data the first time a check needs it.
	//
	// It is a function rather than a value because obtaining it means running
	// `go list -export -deps -test`, which costs about as much as everything
	// else the gate does put together. Eighteen of the twenty checks never
	// touch types, so paying for it in every one of them made the gate ~16s
	// slower for no verdict. A check that does need it pays once.
	LoadExports func() (ExportData, error)

	ti     *typeInfo
	tiErr  error
	tiDone bool
}

// check is one named assertion. The name is the gate step name: the failure a
// user sees is the name plus the error.
type check struct {
	name string
	run  func(*Context) error
}

// checks returns the assertions in the order the gate should run them.
func checks() []check {
	return []check{
		{"engine zero-network import (local side-effect packages)", checkZeroNetworkImport},
		{"package cleanup is never presented as rollback", checkPackageCleanupBanned},
		{"ledger device identity stays a column (not free text)", checkLedgerDeviceIdentity},
		{"ledger hash column located by header name", checkLedgerHashByName},
		{"tests never write to the real audit directory", checkTestsUseTestConstructor},
		{"no package exports what nothing outside it names", checkUnusedExports},
		{"automatic install set excludes downgrade and no-op statuses", checkAutomaticSetExcludesDowngrade},
		{"a run reports only what the recheck confirmed", checkRunReportsOnlyConfirmed},
		{"one comparison primitive decides every version question", checkOneComparisonPrimitive},
		{"silent install dispatches on the package, not on the vendor column", checkSilentDispatchOnPackage},
		{"the silent mechanism reaches the ledger as prose, never as a version", checkSilentMechanismIsProse},
		{"the provisioning placeholder never reaches a decision", checkProvisioningPlaceholderUndecided},
		{"driver selection order is a total order", checkSelectionOrderTotal},
		{"the automatic install set is backed by measurements only", checkAutomaticSetBackedByMeasurements},
		{"software-versioned compare falls back to the measured device version", checkSoftwareVersionedFallback},
		{"CLI exit codes match docs contract", checkCLIExitCodesMatchDocs},
		{"CLI artifact paths match docs contract", checkCLIArtifactPathsMatchDocs},
		{"the documented JSON contracts match the engine", checkJSONContractsMatchDocs},
		{"the architecture doc names the network boundary the imports show", checkArchitectureNetworkBoundary},
		{"evidence chain runs on real fixtures", checkEvidenceChainRunsOnFixtures},
		{"the API contract fixtures are recorded responses", checkAPIFixturesAreRecorded},
	}
}

// Run executes one named check.
func Run(ctx *Context, name string) error {
	for _, c := range checks() {
		if c.name == name {
			return c.run(ctx)
		}
	}
	return fmt.Errorf("no such check: %s", name)
}

// Names lists the check names in the order Run expects them. cmd/gatecheck
// hands these to scripts/verify.ps1, which turns each one into a named, counted
// step.
func Names() []string {
	all := checks()
	names := make([]string, 0, len(all))
	for _, c := range all {
		names = append(names, c.name)
	}
	return names
}

// failf builds the message a failed check reports: the rule it broke, then
// every place the violation was found. Naming path:line is what makes a gate
// failure actionable without opening the source.
func failf(summary string, positions []string) error {
	if len(positions) == 0 {
		return fmt.Errorf("%s", summary)
	}
	return fmt.Errorf("%s: %s", summary, strings.Join(positions, ", "))
}

// stringLiterals returns every string literal in the file, so a caller can both
// test the value and report where it is.
func stringLiterals(f *ast.File) []*ast.BasicLit {
	var out []*ast.BasicLit
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			out = append(out, lit)
		}
		return true
	})
	return out
}

// unquote returns the literal's value, falling back to the raw text when it is
// not a plain interpreted string (a raw or malformed literal still has to be
// reported rather than silently dropped).
func unquote(lit *ast.BasicLit) string {
	if len(lit.Value) < 2 {
		return lit.Value
	}
	return lit.Value[1 : len(lit.Value)-1]
}

// topLevelFuncs returns the declarations of the named top-level functions.
func topLevelFuncs(f *ast.File, name string) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
			out = append(out, fd)
		}
	}
	return out
}
