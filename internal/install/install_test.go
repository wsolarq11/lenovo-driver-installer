package install

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lenovo-driver/internal/model"
)

func TestExtractedDriverFallbackPrefersLogDir(t *testing.T) {
	work := t.TempDir()
	tempRoot := t.TempDir()
	t.Setenv("TEMP", tempRoot)
	extractDir := filepath.Join(tempRoot, "is-ABC123.tmp")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setupPath := filepath.Join(extractDir, "setup.exe")
	if err := os.WriteFile(setupPath, []byte("MZ Inno Setup Setup Data (6.4.3)"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(work, "d1.log")
	if err := os.WriteFile(logPath, []byte("Destination: "+extractDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := &model.Driver{DriverCode: "d1", DriverName: "Test Driver", InstallParameter: "/VERYSILENT"}
	_, used := ExtractedDriverFallback(driver, work)
	if !used {
		t.Fatal("fallback should use the log-pinned extraction directory")
	}
}

func TestTempDirFromLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "d1.log")
	content := "Destination: C:\\Windows\\TempInst\\is-ABC123.tmp\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := tempDirFromLog(logPath); got != `C:\Windows\TempInst\is-ABC123.tmp` {
		t.Fatalf("tempDirFromLog = %q", got)
	}
}

func TestInstallEXETimeoutAttemptsFallback(t *testing.T) {
	fallbackCalled := false
	code, err := installEXE(
		writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: -1, TimedOut: true}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			fallbackCalled = true
			return 0, true
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("installEXE timeout code = %d, want 0", code)
	}
	if !fallbackCalled {
		t.Fatal("EXE timeout did not attempt extracted fallback")
	}
}

func TestInstallEXESurfacesCleanExitWhenFallbackUnused(t *testing.T) {
	code, err := installEXE(
		writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: 1603}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err != nil {
		t.Fatalf("unexpected error for clean non-zero exit: %v", err)
	}
	if code != 1603 {
		t.Fatalf("installEXE failure code = %d, want 1603", code)
	}
}

func TestInstallEXETimeoutWithoutFallbackIsHardError(t *testing.T) {
	code, err := installEXE(
		writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: -1, TimedOut: true}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout with no usable fallback should be a terminal error: %v", err)
	}
	if code != -1 {
		t.Fatalf("installEXE timeout code = %d, want -1", code)
	}
}

func TestExtractedTempDirsScansRecentRootsOnlyForNvidia(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TEMP", tempRoot)
	t.Setenv("SystemRoot", t.TempDir())
	recentDir := filepath.Join(tempRoot, "is-RECENT.tmp")
	if err := os.MkdirAll(recentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	if got := extractedTempDirs(&model.Driver{DriverCode: "d1", DriverName: "Audio"}, work); len(got) != 0 {
		t.Fatalf("non-NVIDIA fallback scanned recent temp roots: %#v", got)
	}

	nvidiaDir := filepath.Join(recentDir, "Display.Driver")
	if err := os.MkdirAll(nvidiaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := extractedTempDirs(&model.Driver{DriverCode: "d2", DriverName: "NVIDIA Graphics"}, work)
	if len(got) != 1 || filepath.Clean(got[0]) != filepath.Clean(recentDir) {
		t.Fatalf("NVIDIA fallback candidates = %#v, want %q", got, recentDir)
	}
}

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

// TestSilentInstallerArgsUsesProvenFamilyNotVendorColumn pins the defect the
// 82JQ drill exposed. DRV202102040007 declared "-QuietInstall" and the binary
// contained no such literal while carrying "/VERYSILENT"; trusting the column
// sent the installer a flag it does not recognize, so it showed a window, the
// operator clicked through, and the machine gained nothing.
func TestSilentInstallerArgsUsesProvenFamilyNotVendorColumn(t *testing.T) {
	path := writeStubPackage(t, "Inno Setup Setup Data (6.4.3)")
	args, ok := silentInstallerArgs(path, &model.Driver{
		DriverCode:       "DRV202102040007",
		InstallParameter: "-QuietInstall",
	}, `C:\tmp\d1.log`)
	if !ok {
		t.Fatal("an Inno-marked package is provable and must be installable unattended")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/VERYSILENT") {
		t.Fatalf("the proven family's own switch is missing: %#v", args)
	}
	if strings.Contains(joined, "-QuietInstall") {
		t.Fatalf("the falsified vendor column leaked back into the command line: %#v", args)
	}
	if !strings.Contains(joined, "/LOG=") {
		t.Fatalf("/LOG is Inno-only and justified by the marker: %#v", args)
	}
	if !strings.Contains(joined, "/NORESTART") {
		t.Fatalf("Inno needs /NORESTART to stay unattended: %#v", args)
	}
}

// TestSilentInstallerArgsCoversEveryProvenFamily checks the table is total: a
// marker in the formula always yields that family's own documented flags, and a
// family the table does not know never receives a guess.
func TestSilentInstallerArgsCoversEveryProvenFamily(t *testing.T) {
	for _, tc := range []struct {
		marker string
		want   string
	}{
		{"Inno Setup Setup Data (6.4.3)", "/VERYSILENT"},
		{"\x00N\x00u\x00l\x00l\x00s\x00o\x00f\x00t\x00I\x00n\x00s\x00t\x00", "/S"},
		{"!@Install@!UTF-8!", "-s"},
		{"InstallShield Setup", "/s"},
		{".wixburn", "/quiet"},
	} {
		path := writeStubPackage(t, tc.marker)
		args, ok := silentInstallerArgs(path, &model.Driver{DriverCode: "d1"}, "")
		if !ok {
			t.Errorf("marker %q produced no silent plan", tc.marker)
			continue
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, tc.want) {
			t.Errorf("marker %q produced %#v, want it to contain %q", tc.marker, args, tc.want)
		}
	}
	// No evidence means no flags. A package the formula cannot place must go
	// interactive rather than be launched with something invented.
	args, ok := silentInstallerArgs(writeStubPackage(t, "nothing recognizable here"),
		&model.Driver{DriverCode: "d1"}, "")
	if ok || len(args) != 0 {
		t.Fatalf("an unproven package must not be silenced: args=%#v ok=%v", args, ok)
	}
	// A vendor column alone proves nothing, in either direction.
	if _, ok := silentInstallerArgs(writeStubPackage(t, "nothing recognizable here"),
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT /NORESTART"}, ""); ok {
		t.Fatal("the vendor column alone must not license a silent install")
	}
}

// TestSilentInstallerArgsSkipsFlagForINFWrapper pins the largest group on the
// 82JQ lists: 41 of 47 rows declare an INF payload, which needs no silent
// switch because handing the INF to the driver store is already unattended.
func TestSilentInstallerArgsSkipsFlagForINFWrapper(t *testing.T) {
	path := writeStubPackage(t, "Inno Setup Setup Data (6.4.3)")
	plan := planSilentInstall(path, &model.Driver{
		DriverCode:       "d1",
		InstallParameter: "/add-driver *.inf /install /subdirs",
	}, "")
	if plan.ok || len(plan.args) != 0 {
		t.Fatalf("an INF wrapper needs no silent switch: args=%#v ok=%v", plan.args, plan.ok)
	}
	if plan.evidence == "" {
		t.Fatal("every formula decision must record the evidence it rested on")
	}
}

func TestInstallEXESurfacesRebootRequired(t *testing.T) {
	code, err := installEXE(
		writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1", InstallParameter: "/VERYSILENT"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			return ProcessResult{ExitCode: 3010}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 3010 {
		t.Fatalf("installEXE must preserve reboot-required code: got %d, want 3010", code)
	}
}

// TestInstallEXEUnprovenFamilyIsTerminal pins the new terminal condition. A
// package that proves nothing must not be launched: there is no flag to send it
// and no way to know what it would open. The condition used to be "the driver
// record carries no Parameter column", which is a statement about the API and
// not about the machine.
func TestInstallEXEUnprovenFamilyIsTerminal(t *testing.T) {
	called := false
	code, err := installEXE(
		writeStubPackage(t, "nothing recognizable here"),
		&model.Driver{DriverCode: "d1", FileName: "d1.exe"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			called = true
			return ProcessResult{ExitCode: 0}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err == nil || !strings.Contains(err.Error(), "no official silent install parameters") {
		t.Fatalf("an unproven package should be a terminal error: code=%d err=%v", code, err)
	}
	if called {
		t.Fatal("installEXE must not launch an EXE whose family it cannot prove")
	}
}

// TestInstallEXERunsProvenPackageWithoutVendorColumn is the counterpart: a
// package that proves itself runs unattended even when the driver record is
// silent about it, which is the case the 82JQ list is full of.
func TestInstallEXERunsProvenPackageWithoutVendorColumn(t *testing.T) {
	var gotArgs []string
	code, err := installEXE(
		writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1", FileName: "d1.exe"},
		t.TempDir(),
		func(filePath string, args []string, timeoutSeconds int, workingDirectory string) ProcessResult {
			gotArgs = args
			return ProcessResult{ExitCode: 0}
		},
		func(driver *model.Driver, workingDir string) (int, bool) {
			return 0, false
		},
	)
	if err != nil || code != 0 {
		t.Fatalf("a proven package should install unattended: code=%d err=%v", code, err)
	}
	if !strings.Contains(strings.Join(gotArgs, " "), "/VERYSILENT") {
		t.Fatalf("the proven family's switch was not used: %#v", gotArgs)
	}
}

func TestTimeoutErrIncludesKillFailure(t *testing.T) {
	err := timeoutErr("pnputil", ProcessResult{TimedOut: true, KillErr: os.ErrPermission})
	if !strings.Contains(err.Error(), "failed to terminate child tree") {
		t.Fatalf("timeoutErr should surface the kill failure: %v", err)
	}
}

func TestHasSilentParameters(t *testing.T) {
	// The answer now comes from the package, not from the driver record, which
	// is why this test needs a real file on disk.
	if !HasSilentParameters(writeStubPackage(t, "Inno Setup Setup Data (6.4.3)"),
		&model.Driver{DriverCode: "d1"}) {
		t.Fatal("a package that proves itself is Inno must be installable unattended")
	}
	if HasSilentParameters(writeStubPackage(t, "nothing recognizable here"),
		&model.Driver{DriverCode: "d1"}) {
		t.Fatal("a package that proves nothing must fall back to interactive")
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
