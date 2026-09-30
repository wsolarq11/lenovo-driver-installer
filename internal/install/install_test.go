package install

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lenovo-driver/internal/model"
)

func TestRunProcessTimeoutKillsTree(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("kill-tree path uses taskkill.exe and requires Windows")
	}
	start := time.Now()
	result := runProcess(context.Background(), "powershell.exe",
		[]string{"-NoProfile", "-Command", "Start-Sleep -Seconds 30"},
		500*time.Millisecond, "")
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %#v", result)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("kill after timeout took too long: %v", elapsed)
	}
}

// TestInstallDriverFileStartFailureIsTerminal guards the InstallDriverFile
// contract: a sub-process that cannot be started at all has no usable exit
// code, so it must surface as err != nil (not as a concrete "exit code -2").
func TestInstallDriverFileStartFailureIsTerminal(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("start-failure path exercises Windows process-start semantics")
	}
	missing := filepath.Join(t.TempDir(), "launcher.bin")
	code, err := InstallDriverFile(missing, &model.Driver{DriverCode: "d1"}, t.TempDir())
	if err == nil {
		t.Fatalf("start failure must surface a terminal error, got code=%d err=nil", code)
	}
}

// TestRunProcessStartFailureCarriesError checks that runProcess records the
// start error instead of collapsing it into a bare "-2".
func TestRunProcessStartFailureCarriesError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("start-failure path exercises Windows process-start semantics")
	}
	missing := filepath.Join(t.TempDir(), "missing.exe")
	result := runProcess(context.Background(), missing, nil, 0, "")
	if result.StartErr == nil {
		t.Fatal("runProcess should surface a start error for a missing executable")
	}
}

func TestNormalizePnPUtilExitCode(t *testing.T) {
	for code, want := range map[int]int{
		0:    0,
		1:    errorSuccessRebootRequired,
		3010: 3010,
		-1:   -1,
	} {
		if got := normalizePnPUtilExitCode(code); got != want {
			t.Fatalf("normalizePnPUtilExitCode(%d) = %d, want %d", code, got, want)
		}
	}
}

// writeStubPackage writes a package whose bytes prove one installer family, so
// the formula can be tested against evidence rather than against a column.
func writeStubPackage(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pkg.exe")
	if err := os.WriteFile(path, append([]byte("MZ stub payload "), marker...), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSilentPlanRefusesUnverifiedInno pins the 82JQ drill defect. The vendor
// column declared "-QuietInstall" and the binary carried the Inno header but no
// such literal; the real Inno switch "/VERYSILENT" has never been verified
// end-to-end. Recognising the family is not verifying its switch, so the plan
// must refuse and must not send any flag.
func TestSilentPlanRefusesUnverifiedInno(t *testing.T) {
	path := writeStubPackage(t, "Inno Setup Setup Data (6.4.3)")
	canRun, evidence := SilentPlanFor(path, &model.Driver{
		DriverCode:       "DRV202102040007",
		InstallParameter: "-QuietInstall",
	})
	if canRun {
		t.Fatal("an Inno-marked package is unverified and must not be driven unattended")
	}
	if !strings.Contains(evidence, "Inno Setup") {
		t.Fatalf("refusal must name the recognised family: %q", evidence)
	}
	if !strings.Contains(evidence, "interactive") {
		t.Fatalf("refusal must say the package goes interactive: %q", evidence)
	}
	if strings.Contains(evidence, "-QuietInstall") {
		t.Fatalf("the falsified vendor column must not enter the evidence: %q", evidence)
	}
}

// TestSilentPlanRefusesEveryUnverifiedFamily checks the formula is total: every
// recognised family is refused, an unrecognised package is refused, and a vendor
// column alone never licenses an unattended run. No .exe silent switch has a
// verified end-to-end run on 82JQ, so no .exe is ever driven unattended.
func TestSilentPlanRefusesEveryUnverifiedFamily(t *testing.T) {
	for _, marker := range []string{
		"Inno Setup Setup Data (6.4.3)",
		"NullsoftInst",
		"!@Install@!UTF-8!",
		"app.wixburn",
		"InstallShield Setup",
		"nothing recognizable here",
	} {
		canRun, evidence := SilentPlanFor(writeStubPackage(t, marker), &model.Driver{DriverCode: "d1"})
		if canRun {
			t.Errorf("marker %q must be refused: no silent switch is verified", marker)
		}
		if evidence == "" {
			t.Errorf("marker %q: every refusal must record evidence", marker)
		}
	}
	// The vendor column alone proves nothing, in either direction.
	if canRun, _ := SilentPlanFor(writeStubPackage(t, "nothing recognizable here"),
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT /NORESTART"}); canRun {
		t.Fatal("the vendor column alone must not license an unattended run")
	}
}

// TestSilentPlanEvidenceNamesTheRefusedFamily pins the audit requirement. A
// refusal must name which family was recognised so the audit can say "we saw
// Inno and refused it", not just "refused"; a package with no marker must say
// the marker was absent.
func TestSilentPlanEvidenceNamesTheRefusedFamily(t *testing.T) {
	for _, tc := range []struct {
		name, marker, want string
	}{
		{"inno", "Inno Setup Setup Data (6.4.3)", "Inno Setup"},
		{"nsis", "NullsoftInst", "NSIS"},
		{"7z sfx", "!@Install@!UTF-8!", "7-Zip"},
		{"wix burn", "app.wixburn", "WiX Burn"},
		{"installshield", "InstallShield Setup", "InstallShield"},
	} {
		canRun, evidence := SilentPlanFor(writeStubPackage(t, tc.marker), &model.Driver{DriverCode: "d1"})
		if canRun {
			t.Errorf("%s: an unverified family must not run unattended", tc.name)
		}
		if !strings.Contains(evidence, tc.want) || !strings.Contains(evidence, "interactive") {
			t.Errorf("%s: evidence = %q, want it to name %q and say interactive", tc.name, evidence, tc.want)
		}
	}
	canRun, evidence := SilentPlanFor(writeStubPackage(t, "no marker here at all"), &model.Driver{DriverCode: "d1"})
	if canRun {
		t.Error("a package with no marker must not be driven unattended")
	}
	if !strings.Contains(evidence, "no installer marker") {
		t.Errorf("refusal evidence = %q, want it to say the marker was absent", evidence)
	}
}

// TestSilentPlanEvidenceAgreesWithTheDecision keeps the evidence honest with
// the decision: a refusal that reads like a success, or a success with no
// stated mechanism, are both unusable in an audit. An INF payload declaration
// in the vendor column does not make an EXE unattended either — the wrapper is
// still an installer with a GUI, and its silent switch is still unverified.
func TestSilentPlanEvidenceAgreesWithTheDecision(t *testing.T) {
	path := writeStubPackage(t, "Inno Setup Setup Data (6.4.3)")
	drv := &model.Driver{
		DriverCode:       "d1",
		InstallParameter: "/add-driver *.inf /install /subdirs",
	}
	canRun, evidence := SilentPlanFor(path, drv)
	if canRun || evidence == "" {
		t.Fatalf("INF-declared EXE wrapper: canRun=%v evidence=%q, want a refusal with a reason", canRun, evidence)
	}
	if !strings.Contains(evidence, "interactive") {
		t.Fatalf("an INF-declared EXE is still an installer and must be refused to interactive: %q", evidence)
	}
}

// TestInstallDriverFileEXEIsTerminal pins the unified formula at the entry
// point: InstallDriverFile must never launch an EXE, because no EXE silent
// switch is verified. The app routes EXEs to the interactive fallback before
// this call; reaching it directly must fail closed rather than guess a switch.
func TestInstallDriverFileEXEIsTerminal(t *testing.T) {
	code, err := InstallDriverFile(
		writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1", FileName: "d1.exe"},
		t.TempDir(),
	)
	if err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Fatalf("an EXE reaching InstallDriverFile must be a terminal interactive error: code=%d err=%v", code, err)
	}
}

func TestTimeoutErrIncludesKillFailure(t *testing.T) {
	err := timeoutErr("pnputil", ProcessResult{TimedOut: true, KillErr: os.ErrPermission})
	if !strings.Contains(err.Error(), "failed to terminate child tree") {
		t.Fatalf("timeoutErr should surface the kill failure: %v", err)
	}
}

func TestInstallINFsEmptyArchiveIsTerminal(t *testing.T) {
	code, err := installINFs(t.TempDir(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no .inf") {
		t.Fatalf("empty archive should be a terminal no-inf error: code=%d err=%v", code, err)
	}
	if code != 2 {
		t.Fatalf("empty archive code = %d, want 2", code)
	}
}

func TestExtractZipExtractsFilesAndDirs(t *testing.T) {
	src := filepath.Join(t.TempDir(), "pkg.zip")
	writeZip(t, src, map[string]string{
		"top.inf":           "[Version]\n",
		"sub/nested.inf":    "[Version]\n",
		"sub/deep/more.cat": "catalog",
	})
	dest := t.TempDir()
	if err := ExtractZip(src, dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"top.inf", filepath.Join("sub", "nested.inf"), filepath.Join("sub", "deep", "more.cat")} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Fatalf("expected extracted file %s: %v", name, err)
		}
	}
}

func TestExtractZipRejectsUnsafePath(t *testing.T) {
	src := filepath.Join(t.TempDir(), "evil.zip")
	writeZip(t, src, map[string]string{"../evil.inf": "[Version]\n"})
	if err := ExtractZip(src, t.TempDir()); err == nil {
		t.Fatal("ExtractZip must reject a zip entry that escapes the destination")
	}
}

func TestProcessExitCode(t *testing.T) {
	if got := processExitCode(nil); got != 0 {
		t.Fatalf("processExitCode(nil) = %d, want 0", got)
	}
	if got := processExitCode(errors.New("boom")); got != -2 {
		t.Fatalf("processExitCode(generic error) = %d, want -2", got)
	}
}

// writeZip writes a zip archive whose entries map names to contents, so the
// ExtractZip contract can be tested offline against an exact byte layout.
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
