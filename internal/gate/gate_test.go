package gate

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture writes a synthetic module under t.TempDir() and returns the
// loaded repository with a context ready for any check. Export data is loaded
// on demand, exactly as cmd/gatecheck does it, so this exercises the same seam
// the real gate uses: a red-light here is a red-light in the gate.
func loadFixture(t *testing.T, module string, files map[string]string) *Context {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, files)
	repo, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return &Context{
		Repo:        repo,
		LoadExports: func() (ExportData, error) { return listExports(t, root), nil },
	}
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
	ctx := &Context{
		Repo:        repo,
		LoadExports: func() (ExportData, error) { return listExports(t, root), nil },
	}
	for _, name := range Names() {
		if err := Run(ctx, name); err != nil {
			t.Errorf("%s failed against this repo: %v", name, err)
		}
	}
}

// Only the checks that resolve names may ask for type information. The export
// data behind it costs about a second to produce, and -check runs one check per
// process, so a check that asks for types it does not use makes the gate slower
// for no verdict. Every check but the two in needsTypes is in that category;
// this test fails if one of them starts asking, or if a check that genuinely
// needs types stops being counted here.
func TestOnlyNameResolvingChecksLoadExports(t *testing.T) {
	root := repoRoot(t)
	repo, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	needsTypes := map[string]bool{
		"tests never write to the real audit directory":    true,
		"no package exports what nothing outside it names": true,
	}
	exportsLoaded := false
	ctx := &Context{
		Repo: repo,
		LoadExports: func() (ExportData, error) {
			exportsLoaded = true
			return listExports(t, root), nil
		},
	}
	for _, name := range Names() {
		exportsLoaded = false
		if err := Run(ctx, name); err != nil {
			t.Fatalf("%s failed against this repo: %v", name, err)
		}
		if !needsTypes[name] && exportsLoaded {
			t.Errorf("%s loaded export data it does not use; it is not a name-resolving check, "+
				"so it should read the AST and cost nothing", name)
		}
		// The context caches the first load, so reset it between checks to make
		// the observation per-check rather than cumulative.
		ctx.ti, ctx.tiErr, ctx.tiDone = nil, nil, false
		ctx.Exports = nil
	}
	if len(needsTypes) != 2 {
		t.Fatalf("the needs-types set has %d entries; this test's premise changed", len(needsTypes))
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
		"internal/ledger/ledger.go": "package ledger\n\n" +
			"var Columns = []string{}\n\n" +
			"func resolveHashIndex(header []string) (int, int, bool) { return 0, 0, true }\n\n" +
			"func readRow(row []string) string {\n\treturn row[len(Columns)]\n}\n",
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
			"func partitionViewDrivers() {\n\tif model.InAutomaticInstallSet(0) {\n\t\treturn\n\t}\n}\n",
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
			"type LocalEvidenceKind int\n\n" +
			"const (\n\tLocalEvidenceAbsent LocalEvidenceKind = iota\n\tLocalEvidenceProvisioned\n)\n\n" +
			"type LocalEvidence struct{ Kind LocalEvidenceKind }\n",
		"internal/compare/matching.go": "package compare\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func CompareDriverStatus(local model.LocalEvidence) model.CompareStatus {\n" +
			"\tswitch local.Kind() {\n" +
			"\tcase model.LocalEvidenceProvisioned:\n" +
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

// The architecture doc's network-boundary line is the human half of invariant 1.
// It drifted: api was listed as deterministic while api imports net/http. The
// check compares the doc's name list against the import graph, so a package that
// gains the network without being named is a red light, not a silent widening.
func TestArchitectureNetworkBoundaryRejectsUnnamedImporter(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod":                    "module example.com/m\n\ngo 1.27\n",
		"internal/api/client.go":    "package api\n\nimport \"net/http\"\n\nvar _ = http.Get\n",
		"internal/download/get.go":  "package download\n\nimport \"net/http\"\n\nvar _ = http.Get\n",
		"docs/spec/architecture.md": "- **联网包**（`api`）：`api` 是联想接口客户端。\n",
	})
	err := checkErr(t, ctx, "the architecture doc names the network boundary the imports show")
	if err == nil {
		t.Fatal("a network-importing package missing from the doc's boundary line was accepted")
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("error should name the unnamed importer, got: %v", err)
	}
}

// The same check must catch the other direction: a name the doc grants the
// network that no longer imports it. That is what the real drift looked like.
func TestArchitectureNetworkBoundaryRejectsStaleGrant(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod":                    "module example.com/m\n\ngo 1.27\n",
		"internal/api/client.go":    "package api\n\nimport \"net/http\"\n\nvar _ = http.Get\n",
		"docs/spec/architecture.md": "- **联网包**（`api`、`model`）：只有 `api` 真的联网。\n",
	})
	err := checkErr(t, ctx, "the architecture doc names the network boundary the imports show")
	if err == nil {
		t.Fatal("a package the doc grants the network but which does not import it was accepted")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Fatalf("error should name the stale grant, got: %v", err)
	}
}

// The webpage contract fixture is the only thing standing between a
// type-mismatched struct and a green suite, because the struct's own tests use
// whatever the fixture says. A quoted statusCode in the fixture re-creates the
// exact bug: the test agrees with the struct, the server disagrees with both.
func TestAPIFixtureRejectsQuotedStatus(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/api/testdata/web_real_82jq_os42.json": `{"statusCode": "200", "localOSID": 42,` +
			`"osList": [], "partList": [{"PartName": "x", "drivelist": []}]}` + strings.Repeat(" ", 4096),
		"internal/api/testdata/quickfix_contract_golden.json": `{"Parameter": "x"}`,
		"docs/spec/contracts.md":                              "drive_listnew\n",
	})
	err := checkErr(t, ctx, "the API contract fixtures are recorded responses")
	if err == nil {
		t.Fatal("a quoted statusCode was accepted; it hides the type mismatch it exists to catch")
	}
	if !strings.Contains(err.Error(), "statusCode") {
		t.Fatalf("error should name the field, got: %v", err)
	}
}

// A fixture without the real envelope is a hand-written sample wearing a
// recording's name. The check refuses it rather than certifying it.
func TestAPIFixtureRejectsMissingEnvelope(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/api/testdata/web_real_82jq_os42.json": `{"statusCode": 200, "localOSID": 42,` +
			`"osList": [], "partList": []}` + strings.Repeat(" ", 4096),
		"internal/api/testdata/quickfix_contract_golden.json": `{"Parameter": "x"}`,
		"docs/spec/contracts.md":                              "drive_listnew\n",
	})
	err := checkErr(t, ctx, "the API contract fixtures are recorded responses")
	if err == nil {
		t.Fatal("a response without the drivelist envelope was accepted as a recording")
	}
	if !strings.Contains(err.Error(), "drivelist") {
		t.Fatalf("error should name the missing key, got: %v", err)
	}
}

func TestPlanBlockReasonRejectsSilentPlan(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/plan/plan.go": "package plan\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func BuildPlanText(drivers []*model.AssessedDriver, generatedAt string) []string {\n" +
			"\tvar lines []string\n\tfor _, d := range drivers {\n" +
			"\t\tif d.CompareStatus == model.StatusUnknown {\n\t\t\tlines = append(lines, \"Reason: unknown\")\n\t\t}\n\t}\n" +
			"\treturn lines\n}\n",
	})
	if err := checkErr(t, ctx, "plan blocks carry the reason the console states"); err == nil {
		t.Fatal("a plan builder that drops the non-match reason was not rejected")
	}
}

func TestSummaryNotesRejectLegacyCauseLabels(t *testing.T) {
	for name, lit := range map[string]string{
		"multi-vendor": "\"  Note: Unknown = multi-vendor package.\"",
		"inference":    "\"  Note: inference = source audit or absence-based.\"",
	} {
		ctx := loadFixture(t, "example.com/m", map[string]string{
			"go.mod": "module example.com/m\n\ngo 1.27\n",
			"internal/plan/plan.go": "package plan\n\nfunc Notes() []string {\n" +
				"\tvar lines []string\n\tlines = append(lines, " + lit + ")\n\treturn lines\n}\n",
		})
		err := checkErr(t, ctx, "summary notes only describe reachable statuses")
		if err != nil && strings.Contains(err.Error(), "no such check") {
			t.Fatalf("%s case ran without its fixture check: %v", name, err)
		}
		if err == nil {
			t.Fatalf("the %s note literal was not rejected", name)
		}
	}
}

func TestSummaryNotesAllowCountLineAndCase(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/plan/plan.go": "package plan\n\nfunc Counts(v string) string {\n" +
			"\tswitch v {\n\tcase \"inference\":\n\t\treturn \"inference=%d\"\n\t}\n" +
			"\treturn \"fact=%d inference=%d undetermined=%d\"\n}\n",
	})
	if err := checkErr(t, ctx, "summary notes only describe reachable statuses"); err != nil {
		t.Fatalf("the schema-complete count line and switch case must stay legal: %v", err)
	}
}

func TestInteractiveHintRejectsUnguardedCall(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/app.go": "package app\n\ntype Options struct{ LatestAcrossOS bool }\n" +
			"func resolveListOS(o *Options) {\n" +
			"\tif !o.LatestAcrossOS {\n\t\tprintln(\"In the interactive menu, press t to switch supported OS lists.\")\n\t}\n}\n",
	})
	if err := checkErr(t, ctx, "the OS-switch hint only speaks where a menu exists"); err == nil {
		t.Fatal("an unguarded OS-switch hint was not rejected")
	}
}

func TestSkippedRowsRejectSilentFilter(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/view.go": "package app\n\ntype view struct{}\n" +
			"func (a *appFor) CompareOSDriverView() {\n\tv := filterDriverRows(v, false)\n}\n" +
			"func filterDriverRows(rows []int, b bool) []int { return rows }\n" +
			"func filterDriverRowsWithSkips(rows []int, b bool) ([]int, []int) { return rows, nil }\n" +
			"type appFor struct{}\n",
	})
	if err := checkErr(t, ctx, "rows dropped before selection are reported"); err == nil {
		t.Fatal("a silently filtering view was not rejected")
	}
}

func TestLocalNewerRejectsWarnLevel(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/view.go": "package app\n\ntype App struct{ S sink }\ntype sink interface{ Log(msg, level string) }\n" +
			"func (a *App) assessSelectedDrivers(source string) {\n\ta.S.Log(source, \"WARN\")\n}\n" +
			"func (a *App) other(source string) {\n\ta.S.Log(source, \"WARN\")\n}\n",
	})
	if err := checkErr(t, ctx, "local newer is not logged as an anomaly"); err == nil {
		t.Fatal("a WARN-level source label was not rejected")
	}
}

func TestBehaviorDocRejectsStaleFlag(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/app.go": "package app\n\nimport \"flag\"\n\n" +
			"func parseOptions(args []string) {\n" +
			"\tfs := flag.NewFlagSet(\"x\", flag.ContinueOnError)\n" +
			"\tfs.BoolVar(nil, \"DryRun\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"IncludeBios\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"TargetOS\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"CurrentOSOnly\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"LatestAcrossOS\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"RemovedFlag\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"SkipHashCheck\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"SkipSignatureCheck\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"GuiExportPath\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"GuiInstallCodes\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"Rollback\", false, \"\")\n" +
			"\t_ = args\n}\n",
		"internal/model/model.go": "package model\n\ntype CompareStatus string\n\nconst (\n" +
			"\tStatusUpdate CompareStatus = \"Update\"\n\tStatusUpToDate CompareStatus = \"Up to date\"\n" +
			"\tStatusNotInstalled CompareStatus = \"Not installed\"\n\tStatusLocalNewer CompareStatus = \"Local newer\"\n" +
			"\tStatusUnknown CompareStatus = \"Unknown\"\n\tStatusNotApplicable CompareStatus = \"Not applicable\"\n)\n",
		"docs/spec/behavior.md": "`Update` `Up to date` `Not installed` `Local newer` `Unknown` `Not applicable`\n" +
			"`-DryRun` `-IncludeBios` `-TargetOS` `-CurrentOSOnly` `-LatestAcrossOS` `-Gone`\n",
	})
	err := checkErr(t, ctx, "the behavior doc names the flags and statuses the engine has")
	if err == nil {
		t.Fatal("a doc naming a flag the engine does not accept was not rejected")
	}
	if !strings.Contains(err.Error(), "Gone") {
		t.Fatalf("the failure did not name the stale flag: %v", err)
	}
}

func TestBehaviorDocRejectsOmittedStatus(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", exampleBehaviorFixture(map[string]string{
		"docs/spec/behavior.md": "`Update` `Up to date` `Not installed` `Local newer` `Unknown`\n" +
			"`-DryRun` `-IncludeBios` `-TargetOS` `-CurrentOSOnly` `-LatestAcrossOS`\n",
	}))
	err := checkErr(t, ctx, "the behavior doc names the flags and statuses the engine has")
	if err == nil {
		t.Fatal("a doc omitting a CompareStatus the engine defines was not rejected")
	}
	if !strings.Contains(err.Error(), "Not applicable") {
		t.Fatalf("the failure did not name the omitted status: %v", err)
	}
}

// TestBehaviorDocAllowsPartialFlagCoverage pins the direction of the flag
// assertion: the doc documents runtime semantics, so naming a subset of flags is
// correct. Only a flag the binary does not accept is a defect.
func TestBehaviorDocAllowsPartialFlagCoverage(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", exampleBehaviorFixture(map[string]string{
		"docs/spec/behavior.md": "`Update` `Up to date` `Not installed` `Local newer` `Unknown` `Not applicable`\n" +
			"`-DryRun` `-IncludeBios` `-TargetOS` `-CurrentOSOnly` `-LatestAcrossOS`\n",
	}))
	if err := checkErr(t, ctx, "the behavior doc names the flags and statuses the engine has"); err != nil {
		t.Fatalf("a doc naming a subset of the real flags must pass: %v", err)
	}
}

func exampleBehaviorFixture(extra map[string]string) map[string]string {
	files := map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/app.go": "package app\n\nimport \"flag\"\n\n" +
			"func parseOptions(args []string) {\n" +
			"\tfs := flag.NewFlagSet(\"x\", flag.ContinueOnError)\n" +
			"\tfs.BoolVar(nil, \"DryRun\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"IncludeBios\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"TargetOS\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"CurrentOSOnly\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"LatestAcrossOS\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"SkipHashCheck\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"SkipSignatureCheck\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"GuiExportPath\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"GuiInstallCodes\", false, \"\")\n" +
			"\tfs.BoolVar(nil, \"Rollback\", false, \"\")\n" +
			"\t_ = args\n}\n",
		"internal/model/model.go": "package model\n\ntype CompareStatus string\n\nconst (\n" +
			"\tStatusUpdate CompareStatus = \"Update\"\n\tStatusUpToDate CompareStatus = \"Up to date\"\n" +
			"\tStatusNotInstalled CompareStatus = \"Not installed\"\n\tStatusLocalNewer CompareStatus = \"Local newer\"\n" +
			"\tStatusUnknown CompareStatus = \"Unknown\"\n\tStatusNotApplicable CompareStatus = \"Not applicable\"\n)\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

// TestConventionsMustBeTracked rejects the shape this rule was written for: the
// authority sitting in a gitignored tree, where a clone has nothing and the gate
// reads nothing. Two directions are pinned here — an unreadable authority, and a
// readable authority with no entrance pointing at it.
func TestConventionsMustBeTracked(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod":             "module example.com/m\n\ngo 1.27\n",
		"docs/spec/index.md": "# index\n",
	})
	err := checkErr(t, ctx, "the coding conventions have one tracked authority")
	if err == nil {
		t.Fatal("missing convention documents were not rejected")
	}
}

// TestConventionsRejectSecondCopyInIgnoredTree rejects a full copy reappearing
// under a gitignored tree, which is how the copy that drifted was kept. A
// pointer stub stays legal; the rule keys on size because that is what separates
// a pointer from a copy.
func TestConventionsRejectSecondCopyInIgnoredTree(t *testing.T) {
	files := map[string]string{
		"go.mod":             "module example.com/m\n\ngo 1.27\n",
		"docs/spec/index.md": "编码规范在 `docs/spec/coding/`。\n",
	}
	for _, rel := range conventionDocs {
		files[rel] = "# conventions\n"
	}
	// A copy large enough to be content rather than a pointer.
	files[".trellis/spec/backend/quality-guidelines.md"] = strings.Repeat("duplicated convention text\n", 40)

	ctx := loadFixture(t, "example.com/m", files)
	err := checkErr(t, ctx, "the coding conventions have one tracked authority")
	if err == nil {
		t.Fatal("a second copy of a convention document in a gitignored tree was not rejected")
	}
	if !strings.Contains(err.Error(), ".trellis") {
		t.Fatalf("the refusal must name the offending copy: %v", err)
	}
}

// TestAuditMustUseCandidateVersionSet rejects an audit that compares one display
// string. The red-light is the shape that lost evidence: a sourceEvidenceRules
// that reads driver.LocalVersion directly and filters devices by raw equality.
func TestAuditMustUseCandidateVersionSet(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/audit/audit.go": "package audit\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func sourceEvidenceRules(d *model.AssessedDriver) bool {\n" +
			"\tlocalVersion := d.LocalVersion\n" +
			"\treturn localVersion == \"x\"\n}\n",
	})
	err := checkErr(t, ctx, "the source audit matches every measured version")
	if err == nil {
		t.Fatal("an audit comparing one display string was not rejected")
	}
	if !strings.Contains(err.Error(), "candidate") {
		t.Fatalf("the refusal must name what is missing: %v", err)
	}
}

// The positive direction: an audit that asks for the candidate set passes.
func TestAuditUsingCandidateVersionSetPasses(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/audit/audit.go": "package audit\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func candidateVersions(d *model.AssessedDriver) []string { return nil }\n\n" +
			"func versionInCandidates(v string, c []string) bool { return false }\n\n" +
			"func sourceEvidenceRules(d *model.AssessedDriver) bool {\n" +
			"\tfor _, v := range candidateVersions(d) {\n" +
			"\t\tif versionInCandidates(v, nil) {\n\t\t\treturn true\n\t\t}\n\t}\n" +
			"\treturn false\n}\n",
	})
	if err := checkErr(t, ctx, "the source audit matches every measured version"); err != nil {
		t.Fatalf("an audit using the candidate set was rejected: %v", err)
	}
}

// TestEvidenceUnionRejectsExportedFields rejects the regression that would make
// the union suggestion-only: exported payload fields, which let any caller build
// a Conflicted state carrying one version.
func TestEvidenceUnionRejectsExportedFields(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/model/evidence.go": "package model\n\n" +
			"type LocalEvidenceKind int\n\n" +
			"const (\n\tLocalEvidenceUnobserved LocalEvidenceKind = iota\n\tLocalEvidenceVersion\n)\n\n" +
			"type LocalEvidence struct {\n\tKind    LocalEvidenceKind\n\tVersion string\n}\n\n" +
			"func NotObserved() LocalEvidence                     { return LocalEvidence{} }\n" +
			"func ObservedVersion(v string) LocalEvidence          { return LocalEvidence{} }\n" +
			"func ObservedAbsence() LocalEvidence                  { return LocalEvidence{} }\n" +
			"func ObservedConflict(v []string) LocalEvidence       { return LocalEvidence{} }\n" +
			"func ObservedProvisioning() LocalEvidence             { return LocalEvidence{} }\n",
	})
	err := checkErr(t, ctx, "local evidence is a closed union")
	if err == nil {
		t.Fatal("exported payload fields were not rejected")
	}
	if !strings.Contains(err.Error(), "exported field") {
		t.Fatalf("the refusal must name the defect: %v", err)
	}
}

// TestEvidenceUnionRejectsAbsentZeroValue rejects the ordering that made a
// forgotten assignment mean "measured absence".
func TestEvidenceUnionRejectsAbsentZeroValue(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/model/evidence.go": "package model\n\n" +
			"type LocalEvidenceKind int\n\n" +
			"const (\n\tLocalEvidenceAbsent LocalEvidenceKind = iota\n\tLocalEvidenceUnobserved\n)\n\n" +
			"type LocalEvidence struct {\n\tkind LocalEvidenceKind\n}\n\n" +
			"func NotObserved() LocalEvidence                { return LocalEvidence{} }\n" +
			"func ObservedVersion(v string) LocalEvidence     { return LocalEvidence{} }\n" +
			"func ObservedAbsence() LocalEvidence             { return LocalEvidence{} }\n" +
			"func ObservedConflict(v []string) LocalEvidence  { return LocalEvidence{} }\n" +
			"func ObservedProvisioning() LocalEvidence        { return LocalEvidence{} }\n",
	})
	err := checkErr(t, ctx, "local evidence is a closed union")
	if err == nil {
		t.Fatal("an Absent zero value was not rejected")
	}
	if !strings.Contains(err.Error(), "zero value") {
		t.Fatalf("the refusal must explain the zero-value requirement: %v", err)
	}
}

// TestVersionColumnRuleRejectsDisplay rejects handing the display prose to a
// ledger append. That value is a sentence; the columns are versions.
func TestVersionColumnRuleRejectsDisplay(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/install.go": "package app\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func (a *App) writeHistoryRecordChecked(d *model.AssessedDriver, r, m, v, b string) {}\n\n" +
			"func (a *App) verifyInstalled(d []*model.AssessedDriver) {\n" +
			"\tev := model.NotObserved()\n" +
			"\ta.writeHistoryRecordChecked(d[0], \"Verified\", \"m\", ev.Display(), ev.Display())\n}\n",
	})
	err := checkErr(t, ctx, "ledger version columns hold measured versions")
	if err == nil {
		t.Fatal("Display() passed into a ledger append was not rejected")
	}
	if !strings.Contains(err.Error(), "display prose") && !strings.Contains(err.Error(), "Message") {
		t.Fatalf("the refusal must name the prose/version confusion: %v", err)
	}
}

// TestVersionColumnRuleAcceptsMeasured is the positive direction.
func TestVersionColumnRuleAcceptsMeasured(t *testing.T) {
	ctx := loadFixture(t, "example.com/m", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.27\n",
		"internal/app/install.go": "package app\n\nimport \"example.com/m/internal/model\"\n\n" +
			"func (a *App) writeHistoryRecordChecked(d *model.AssessedDriver, r, m, v, b string) {}\n\n" +
			"func (a *App) verifyInstalled(d []*model.AssessedDriver) {\n" +
			"\tev := model.NotObserved()\n" +
			"\tmsg := \"local=\" + ev.Display()\n" +
			"\ta.writeHistoryRecordChecked(d[0], \"Verified\", msg, ev.MeasuredVersion(), ev.MeasuredVersion())\n}\n",
	})
	if err := checkErr(t, ctx, "ledger version columns hold measured versions"); err != nil {
		t.Fatalf("measured versions were rejected: %v", err)
	}
}
