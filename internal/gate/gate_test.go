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

// The nine checks below were PowerShell regexes. Each red-light here pins the
// defect the move fixed, so the check cannot quietly regress to reading text.

// A comment that explains the rule must not be read as a breach of it. The
// PowerShell step matched `!=` anywhere in the function body, so documenting the
// allow-list tripped the gate.
func TestAllowListIgnoresCommentMentioningNotEqual(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/model/model.go": "package model\n\ntype CompareStatus int\n\n" +
			"const (\n\tStatusUpdate CompareStatus = iota\n\tStatusNotInstalled\n)\n\n" +
			"func InAutomaticInstallSet(status CompareStatus) bool {\n" +
			"\t// never widen this with a != comparison\n" +
			"\treturn status == StatusUpdate || status == StatusNotInstalled\n}\n",
		"internal/app/view.go": "package app\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func partition() {\n\tif model.InAutomaticInstallSet(0) {\n\t\treturn\n\t}\n}\n",
		"internal/app/export.go": "package app\n\nimport \"example.com/m/internal/model\"\n\n" +
			"type payload struct {\n\tIsApplicable bool\n}\n\n" +
			"func export() payload {\n\treturn payload{IsApplicable: model.InAutomaticInstallSet(0)}\n}\n",
		"internal/app/interactive.go": "package app\n\nfunc prompt() {}\n",
		"wpf/window.xaml":             "<Window />\n",
	})
	if err := checkErr(t, ctx, "automatic install set excludes downgrade and no-op statuses"); err != nil {
		t.Fatalf("a comment explaining the allow-list was read as a breach: %v", err)
	}
}

// A commented-out declaration must not satisfy an existence guard. The
// PowerShell step matched `func\s+TestX` anywhere in the file text, so a comment
// naming a deleted test kept the rule looking guarded.
func TestTestGuardIgnoresCommentNamingTheTest(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		// The branch itself is correct; only the test that guards it is gone,
		// which is exactly the drift the comment used to hide.
		"internal/model/model.go": "package model\n\ntype CompareStatus int\n\n" +
			"const (\n\tStatusUnknown CompareStatus = iota\n\tStatusNotInstalled\n)\n\n" +
			"const LocalVersionProvisioned = \"Provisioned\"\n",
		"internal/compare/matching.go": "package compare\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func CompareDriverStatus(local string) model.CompareStatus {\n" +
			"\tif local == model.LocalVersionProvisioned {\n" +
			"\t\treturn model.StatusUnknown\n\t}\n" +
			"\treturn model.StatusNotInstalled\n}\n",
		"internal/app/evidence_chain_test.go": "package app\n\nimport \"testing\"\n\n" +
			"// TestProvisionedIsNotAVersion lives here.\n" +
			"func TestSomethingElse(t *testing.T) {}\n",
	})
	err := checkErr(t, ctx, "the provisioning placeholder never reaches a decision")
	if err == nil {
		t.Fatal("a comment naming the test satisfied the guard")
	}
	if !strings.Contains(err.Error(), "TestProvisionedIsNotAVersion") {
		t.Fatalf("error should name the missing test, got: %v", err)
	}
}

func TestSelectionOrderRejectsStableSort(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/compare/selection.go": "package compare\n\nimport \"sort\"\n\n" +
			"type row struct{ PartID, DriverName, DriverCode string }\n\n" +
			"func SelectLatestDrivers(rows []row) []row {\n" +
			"\tselected := rows\n" +
			"\tsort.SliceStable(selected, func(i, j int) bool { return selected[i].PartID < selected[j].PartID })\n" +
			"\treturn selected\n}\n",
		"internal/app/evidence_firmware_determinism_test.go": "package app\n\nimport \"testing\"\n\n" +
			"func TestSelectionOrderIsDeterministic(t *testing.T) {}\n\n" +
			"func TestFirmwareRowsAreDroppedUnlessRequested(t *testing.T) {}\n",
	})
	err := checkErr(t, ctx, "driver selection order is a total order")
	if err == nil {
		t.Fatal("a stable sort on a non-total key was not rejected")
	}
}

func TestSilentDispatchRejectsSwitchTable(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/install/installer_family.go": "package install\n\n" +
			"var familyMarkers = []string{\"Inno\"}\n\n" +
			"func planSilentInstall() {}\n\n" +
			"func switches() []string {\n\treturn []string{\"/S\"}\n}\n",
	})
	err := checkErr(t, ctx, "silent install dispatches on the package, not on the vendor column")
	if err == nil {
		t.Fatal("a silent switch table was not rejected")
	}
}

func TestSilentMechanismRejectsBarePositionalEvidence(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod":                               "module example.com/m\n\ngo 1.27\n",
		"internal/install/installer_family.go": "package install\n\nfunc SilentPlanFor() (bool, string) { return false, \"\" }\n",
		"internal/app/install_silent.go": "package app\n\nimport \"example.com/m/internal/install\"\n\n" +
			"func evidence() string {\n\t_, e := install.SilentPlanFor()\n\treturn e\n}\n",
		"internal/app/install.go": "package app\n\n" +
			"func record(a *App, silence string) {\n" +
			"\ta.writeHistoryRecordChecked(\"Installed\", \"silent via \"+silence, silence, \"\")\n}\n\n" +
			"func (a *App) writeHistoryRecordChecked(result, message, verifiedVersion, beforeVersion string) {}\n\n" +
			"type App struct{}\n",
	})
	err := checkErr(t, ctx, "the silent mechanism reaches the ledger as prose, never as a version")
	if err == nil {
		t.Fatal("the evidence variable as a bare positional argument was not rejected")
	}
}

func TestAutomaticSetEvidenceRejectsInference(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/model/model.go": "package model\n\ntype CompareStatus int\n\n" +
			"const (\n\tStatusUpdate CompareStatus = iota\n\tStatusUpToDate\n\tStatusNotInstalled\n)\n\n" +
			"func StatusEvidenceBasis(status CompareStatus) string {\n" +
			"\tswitch status {\n\tcase StatusUpdate, StatusUpToDate, StatusNotInstalled:\n" +
			"\t\treturn \"inference\"\n\tdefault:\n\t\treturn \"undetermined\"\n\t}\n}\n",
		"internal/model/evidence_basis_test.go": "package model\n\nimport \"testing\"\n\n" +
			"func TestAutomaticSetStatusesAreMeasured(t *testing.T) {}\n\n" +
			"func TestNonFactStatusesStayOutOfTheAutomaticSet(t *testing.T) {}\n",
	})
	err := checkErr(t, ctx, "the automatic install set is backed by measurements only")
	if err == nil {
		t.Fatal("an inference basis for an automatic-set member was not rejected")
	}
}

func TestSoftwareVersionedFallbackRejectsMissingDeviceVersion(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/compare/matching.go": "package compare\n\n" +
			"func isSoftwareVersionedDriver(name string) bool { return true }\n\n" +
			"func deviceVersionFrom(versions []string) string { return \"\" }\n\n" +
			"func local(name string, versions []string) string {\n" +
			"\tif isSoftwareVersionedDriver(name) {\n\t\treturn \"\"\n\t}\n" +
			"\treturn deviceVersionFrom(versions)\n}\n",
		"internal/compare/software_version_fallback_test.go": "package compare\n\nimport \"testing\"\n\n" +
			"func TestSoftwareVersionedDriverFallsBackToMeasuredDeviceVersion(t *testing.T) {}\n",
	})
	err := checkErr(t, ctx, "software-versioned compare falls back to the measured device version")
	if err == nil {
		t.Fatal("a software-versioned branch with no device fallback was not rejected")
	}
}

// The four checks below pin a Go artifact against a document or against the
// tests that use it. Each red-light pins a defect the move fixed: a comment must
// no longer be able to enter the artifact's field set, or stand in for a call.

// A doc comment that spells a json tag is prose about the shape, not a field.
// The PowerShell scan read `json:"GhostField"` out of the comment and compared
// it against contracts.md as though the engine had declared it.
func TestJSONContractIgnoresTagInComment(t *testing.T) {
	fields := ""
	for i := 0; i < 25; i++ {
		fields += "\tField" + string(rune('A'+i)) + " string `json:\"Field" + string(rune('A'+i)) + "\"`\n"
	}
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/export.go": "package app\n\n" +
			"// The shape is `json:\"GhostField\"` when set.\n" +
			"type payload struct {\n" + fields + "}\n",
		"internal/model/model.go":  "package model\n\ntype OSListEntry struct {\n\tOSID string `json:\"OSID\"`\n}\n",
		"internal/app/rollback.go": "package app\n\ntype offer struct {\n\tState string `json:\"State\"`\n}\n",
		// The documented block carries exactly the real fields, no GhostField.
		"docs/spec/contracts.md": "```json\n" + docBlock(fields) + "```\n\n```json\n{\n  \"State\": \"...\"\n}\n```\n",
	})
	err := checkErr(t, ctx, "the documented JSON contracts match the engine")
	if err != nil && strings.Contains(err.Error(), "GhostField") {
		t.Fatalf("a json tag written in a comment entered the engine's field set: %v", err)
	}
}

// docBlock renders the same fields as a JSON object body, so the fixture's two
// sides agree on everything but the commented ghost.
func docBlock(fields string) string {
	var names []string
	for _, line := range strings.Split(fields, "\n") {
		idx := strings.Index(line, `json:"`)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(`json:"`):]
		names = append(names, rest[:strings.Index(rest, `"`)])
	}
	var b strings.Builder
	b.WriteString("{\n")
	for _, name := range names {
		b.WriteString("  \"" + name + "\": \"...\",\n")
	}
	b.WriteString("  \"OSID\": \"...\"\n}\n")
	return b.String()
}

// A renamed chain call must not be satisfied by a comment that still names the
// old function. The PowerShell scan matched the name anywhere in the file text.
func TestEvidenceChainRejectsRenamedCallWithComment(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/chain.go": "package app\n\n" +
			"func ParseQuickFix() {}\nfunc filterDriverRows() {}\nfunc SelectLatestDrivers() {}\n" +
			"func TestDriverApplicable() {}\nfunc ResolveLocalDriverVersion() {}\n" +
			"func CompareDriverStatus() {}\nfunc partitionViewDrivers() {}\nfunc WriteHistoryRecord() {}\n" +
			"func diffDeviceSnapshots() {}\n",
		"internal/app/evidence_chain_test.go": "package app\n\nimport \"testing\"\n\n" +
			"// diffDeviceSnapshots is exercised via renamed.\n" +
			"func renamed() {}\n\n" +
			"func TestChain(t *testing.T) {\n\tParseQuickFix()\n\tfilterDriverRows()\n" +
			"\tSelectLatestDrivers()\n\tTestDriverApplicable()\n\tResolveLocalDriverVersion()\n" +
			"\tCompareDriverStatus()\n\tpartitionViewDrivers()\n\tWriteHistoryRecord()\n}\n",
		"internal/app/fixtures_test.go": "package app\n\nimport \"testing\"\n\n" +
			"func TestFixtures(t *testing.T) {\n\t_ = \"testdata/quickfix_real_82jq.json\"\n" +
			"\t_ = \"testdata/local_devices_82jq.json\"\n\t_ = \"testdata/local_software_82jq.json\"\n}\n",
		"internal/app/testdata/quickfix_real_82jq.json":  strings.Repeat("x", 4096),
		"internal/app/testdata/local_devices_82jq.json":  strings.Repeat("x", 4096),
		"internal/app/testdata/local_software_82jq.json": strings.Repeat("x", 4096),
	})
	err := checkErr(t, ctx, "evidence chain runs on real fixtures")
	if err == nil {
		t.Fatal("a comment naming the removed chain call satisfied the guard")
	}
	if !strings.Contains(err.Error(), "diffDeviceSnapshots") {
		t.Fatalf("error should name the dropped link, got: %v", err)
	}
}

// The exit-code block is a constant in the help text. A comment above it that
// quotes the anchor must not be read as the block, which is how the old scan
// could be poisoned into comparing the wrong numbers.
func TestExitCodesIgnoreCommentQuotingTheAnchor(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/help.go": "package app\n\n" +
			"// Exit codes:\n//   9  not a real code\n\n" +
			"const helpText = `Usage:\n  lenovo-driver\n\nExit codes:\n  0  ok\n  2  bad args\n`\n",
		"docs/spec/contracts.md": "| `0` | ok |\n| `2` | bad args |\n",
	})
	err := checkErr(t, ctx, "CLI exit codes match docs contract")
	if err != nil && strings.Contains(err.Error(), "9") {
		t.Fatalf("a comment quoting the anchor was read as the exit-code block: %v", err)
	}
}
