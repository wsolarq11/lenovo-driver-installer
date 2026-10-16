package gate

// Reading the fixtures the tests depend on. A fixture is data, not Go source, so
// the paths come from the string literals and filepath.Join arguments the tests
// spell, and the files are measured on disk.

import (
	"go/ast"
	"strings"
)

func allCalls(f *ast.File) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			out = append(out, call)
		}
		return true
	})
	return out
}

// testdataFixtures returns the fixture filenames the test sources reference.
// Both spellings a test can use are read: a literal path containing "testdata/"
// and a filepath.Join whose arguments spell the directory and the file
// separately. Covering one and not the other would reintroduce the same blind
// spot in a new shape.
func (r *Repo) testdataFixtures() []string {
	seen := map[string]bool{}
	var out []string
	add := func(candidate string) {
		name := fixtureName(candidate)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, f := range r.files {
		if !f.isTest {
			continue
		}
		for _, lit := range stringLiteralValues(f.ast) {
			if strings.Contains(lit, "testdata/") || strings.Contains(lit, `testdata\`) {
				add(lit)
			}
		}
		for _, call := range allCalls(f.ast) {
			if qualifiedName(call.Fun) != "filepath.Join" {
				continue
			}
			joined := make([]string, 0, len(call.Args))
			allLiterals := true
			for _, arg := range call.Args {
				values := stringLiteralValues(arg)
				if len(values) == 0 {
					allLiterals = false
					break
				}
				joined = append(joined, values...)
			}
			if allLiterals {
				add(strings.Join(joined, "/"))
			}
		}
	}
	return sorted(out)
}

// fixtureName returns the file name of a path that runs through a testdata
// directory, or "" when the path does not.
func fixtureName(path string) string {
	normalized := strings.ReplaceAll(path, `\`, "/")
	idx := strings.LastIndex(normalized, "testdata/")
	if idx < 0 {
		return ""
	}
	rest := normalized[idx+len("testdata/"):]
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	if !strings.Contains(rest, ".") {
		return ""
	}
	return rest
}

// fixturePath returns the repository-relative path of a fixture, searching the
// testdata directories. It returns "" when no such fixture exists.
func (r *Repo) fixturePath(name string) string {
	for _, f := range r.files {
		if strings.HasSuffix(f.relPath, "testdata/"+name) {
			return f.relPath
		}
	}
	// A fixture no Go file sits beside (a JSON with no package of its own) is
	// still found by walking the testdata directories on disk.
	matches, err := findFixture(r.root, name)
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}
