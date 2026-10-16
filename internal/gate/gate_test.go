package gate

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture writes a synthetic module under t.TempDir(), asks the Go
// toolchain for its dependency export data exactly as cmd/gatecheck does, and
// returns the loaded repository and a context ready for any check. This is the
// same seam the real gate uses, so a red-light here is a red-light in the gate.
func loadFixture(t *testing.T, module string, files map[string]string) *Context {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, files)
	exports := listExports(t, root)
	repo, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return &Context{Repo: repo, Exports: exports}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func listExports(t *testing.T, root string) ExportData {
	t.Helper()
	cmd := exec.Command("go", "list", "-export", "-deps", "-test", "-f", "{{.ImportPath}}\t{{.Export}}", "./...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -export in fixture: %v\n%s", err, out)
	}
	exports := ExportData{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		path, file, ok := strings.Cut(line, "\t")
		if !ok || path == "" || file == "" {
			continue
		}
		exports[path] = file
	}
	return exports
}

// checkErr runs one check and returns its error (nil on pass).
func checkErr(t *testing.T, ctx *Context, name string) error {
	t.Helper()
	err := Run(ctx, name)
	if err != nil {
		t.Logf("%s: %v", name, err)
	}
	return err
}

// The positive test runs every check against this repository and must pass.
// This is the regression guard: if a check's judgment drifts from the code it
// guards, either the check or the code is wrong, and this test names which.
func TestChecksPassAgainstThisRepo(t *testing.T) {
	root := repoRoot(t)
	repo, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	exports := listExports(t, root)
	ctx := &Context{Repo: repo, Exports: exports}
	for _, name := range Names() {
		if err := Run(ctx, name); err != nil {
			t.Errorf("%s failed against this repo: %v", name, err)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above test dir")
		}
		dir = parent
	}
}

func TestZeroNetworkImportRejectsNetImport(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod":                "module example.com/m\n\ngo 1.27\n",
		"internal/install/a.go": "package install\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nvar _ = fmt.Sprint\nvar _ = http.Get\n",
	})
	err := checkErr(t, ctx, "engine zero-network import (local side-effect packages)")
	if err == nil {
		t.Fatal("net/http import in internal/install was not rejected")
	}
	if !strings.Contains(err.Error(), "net/http") || !strings.Contains(err.Error(), "internal/install/a.go") {
		t.Fatalf("error should name the import and its file, got: %v", err)
	}
}

func TestPackageCleanupRejectsdeleteDriverArg(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/install/rollback.go": "package install\n\n" +
			"// cleanup is not rollback.\n" +
			"func cleanup() []string {\n\treturn []string{\"/delete-driver\", \"oem0.inf\"}\n}\n",
		// A device-level rollback call must exist for the refusal to guard
		// anything, so this fixture also pins that side.
		"internal/install/rollback_native_windows.go": "//go:build windows\n\npackage install\n\n" +
			"func DiRollbackDriver() error { return nil }\n",
	})
	err := checkErr(t, ctx, "package cleanup is never presented as rollback")
	if err == nil {
		t.Fatal("the /delete-driver argument vector was not rejected")
	}
	if !strings.Contains(err.Error(), "internal/install/rollback.go") {
		t.Fatalf("error should name the offending file, got: %v", err)
	}
}

func TestLedgerDeviceIdentityRejectsFreeText(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/audit/row.go": "package audit\n\nimport \"fmt\"\n\n" +
			"func row(id string) string {\n\treturn fmt.Sprintf(\"device=\" + id)\n}\n",
	})
	err := checkErr(t, ctx, "ledger device identity stays a column (not free text)")
	if err == nil {
		t.Fatal("free-text device identity was not rejected")
	}
}

func TestLedgerHashByNameRejectsPositionalIndex(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/history.go": "package app\n\n" +
			"var historyColumns = []string{}\n\n" +
			"func resolveHashIndex(header []string) (int, int, bool) { return 0, 0, true }\n\n" +
			"func readRow(row []string) string {\n\treturn row[len(historyColumns)]\n}\n",
	})
	err := checkErr(t, ctx, "ledger hash column located by header name")
	if err == nil {
		t.Fatal("positional ledger indexing was not rejected")
	}
}

func TestTestsUseTestConstructorRejectsBareNew(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/app.go": "package app\n\n" +
			"func New() *App { return &App{} }\n\ntype App struct{}\n",
		"internal/app/app_test.go": "package app\n\nimport \"testing\"\n\n" +
			"func TestUsesNew(t *testing.T) {\n\ta := New()\n\t_ = a\n}\n",
	})
	err := checkErr(t, ctx, "tests never write to the real audit directory")
	if err == nil {
		t.Fatal("a test calling New directly was not rejected")
	}
}
