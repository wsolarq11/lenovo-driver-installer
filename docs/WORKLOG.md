# Work Log - 2026-08-29

## Session: Native-only Runtime and WPF Fix

**Branch**: `main`

### Summary

Completed the native-only backend migration, removed the PowerShell runtime
path, fixed the WPF UI not updating after a refresh, wrote the quality gate
evidence, and pushed the working tree.

### Main Changes

- `internal/inventory`: all inventory now uses native SetupAPI/CfgMgr32,
  registry, and SMBIOS reads; PowerShell runtime path removed.
- `internal/inventory/native_full_windows.go`: native device snapshot, machine
  and OS info, installed apps, software snapshot, file version, elevation
  check.
- `internal/inventory/smbios_windows.go`: `GetSystemFirmwareTable` SMBIOS
  parsing, including Type 1 serial.
- `internal/install/native_windows.go`: `DiInstallDriverW` native INF install
  with system-native `pnputil` fallback.
- `internal/app/native_elevate_windows.go`: native `ShellExecuteExW` UAC
  elevation, replacing the PowerShell elevation path.
- `wpf/window.xaml`: added UTF-8 BOM so Windows PowerShell 5.1 parses the
  Chinese XAML correctly.
- `wpf/worker.ps1`: wait for the child process to exit and read its exit code
  after `HasExited`; PowerShell 5.1 can leave `ExitCode` null after
  `Start-Process` with redirected output, which froze the UI on
  "正在识别机器...".
- `docs/BACKEND-BENCHMARK-AND-NATIVE-MIGRATION.md`: benchmark and native
  migration report.
- `README.md`, `docs/TECHNICAL.md`: native-only architecture and smoke commands.

### Testing

- [OK] `scripts/verify.ps1` 14/14 PASS.
- [OK] `LENOVO_NATIVE_SMOKE=1 go test ./internal/inventory/ -run TestNativeSmoke` PASS.
- [OK] `LENOVO_NATIVE_EQUIV_SMOKE=1 go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke` PASS.
- [OK] `LENOVO_NATIVE_INSTALL_SMOKE=1 go test ./internal/install/ -run TestNativeInstallSmoke` PASS.
- [OK] WPF `-SelfTest` and `-WorkerSmoke` PASS.

### Status

[OK] **Completed** and pushed to `origin/main`.

---

## Session: Thermo-Nuclear Code Quality Review Delivery

**Branch**: `main`

### Summary

Reviewed the Trellis 0.6.16 working-tree changes, delivered a thermo-nuclear
maintainability review report, cleaned generated artifacts, restored
accidentally touched Trellis files to their template hashes, and made
Trellis-managed directories local-only in Git.

### Main Changes

- Added `docs/thermo-nuclear-code-quality-review.md`.
- Restored accidentally touched `.trellis` scripts to the Trellis 0.6.16
  template hashes.
- Removed Python bytecode caches generated during review.
- Added `/.trellis/`, `/.agents/`, and `/.dsh/` to `.gitignore` and removed
  those Trellis-managed directories from Git tracking while keeping local
  files intact.

### Git Commits

| Hash | Message |
|------|---------|
| `0f33c8d` | docs: add thermo-nuclear code quality review |
| `e14f68a` | chore: keep Trellis-managed files local-only |

### Testing

- [OK] Verified restored `.trellis` files against `.trellis/.template-hashes.json`.
- [OK] AST parsed modified Python files before rollback.
- [OK] `git status` clean after commits.

### Status

[OK] **Completed**

### Next Steps

- Forward the review findings to the Trellis maintainers.
- Keep Trellis-managed files local-only.

---

## Session: Thermo-Nuclear Follow-up (fresh review + #4/#5/#6/#7)

**Branch**: `main`

### Summary

Ran an independent full re-review of the `cmd/` + `internal/` Go engine (not relying on the
first round's conclusions), then implemented all outstanding follow-ups #4 (parallel OS fetch),
#5 (source-audit rule table), #6 (assessment ownership separation) and #7 (delete the fake
process-output capture). No follow-up remains open.

### Main Changes

- `internal/app/view.go`, `internal/app/install.go`, `internal/app/app.go`:
  parallel cross-OS fetch via `loadDriverListsForOSIDs` + mutex-guarded `osDriverCache`.
- `internal/app/view.go`: `cloneDriver`/`cloneDrivers` — the comparison pass owns its rows, so
  assessment never mutates the shared driver-list cache / API transport DTOs (#6).
- `internal/audit/audit.go`: `sourceCategoryRule`/`sourceEvidenceRules` declarative priority table.
- `internal/install/install.go`: removed `ProcessResult.Stdout/Stderr`, `captureOutput`,
  and the output buffer; `runProcess` discards sub-process output.
- Added unit tests (audit priority, parallel order, tolerance policy, clone ownership);
  updated install tests.
- `docs/thermo-nuclear-code-quality-review-final.md`: refreshed with the second-round findings
  and the #4/#5/#6/#7 implementations.

### Testing

- [OK] `go build ./...`, `go vet ./...`, `gofmt -l` clean.
- [OK] `go test ./...` all packages pass, including new tests.
- [OK] `scripts/verify.ps1` — 14 steps, 0 failed, `VERIFY_OK`.

### Status

[OK] **Completed** (#4/#5/#6/#7). All prior follow-ups closed.

---

## Session: Thermo-Nuclear Code Quality Review — Round 7 (native hardening)

**Branch**: `main`

### Summary

Ran an independent, code-only, whole-repo Round 7 thermo-nuclear review (no
baseline, no reliance on prior report or code comments), then landed the
behavior-preserving fixes it surfaced and completed the native-install /
native-inventory hardening. All work verified green and the working tree is
now clean.

### Main Changes

- `internal/app/view.go`: dropped from 501 → 423 lines by extracting the
  WPF-compatible JSON export concern into a new `internal/app/export.go`
  (`ExportGUIView` / `guiExportPayload` / `guiDriverRow`), removing the now
  unused `encoding/json` dependency. All Go files are back under the 500-line
  ceiling.
- `internal/inventory/native_windows.go`: extracted `evidenceRows()` shared
  "enumerate + load driver evidence + index" prefix, so `GetDeviceEvidence`
  and `GetDeviceDriverVersions` no longer each enumerate and re-index.
- `internal/inventory/native_full_windows.go`: `expandWindowsEnv` changed from
  an unbounded `for {}` to an explicitly bounded loop (64 passes + consuming
  invariant); extracted the Windows file-version reader into
  `internal/inventory/fileversion_windows.go`; made driver-evidence column
  values `RFC3339/UTC` for the setupapi import timestamps.
- `internal/install`: normalised the reboot-required install result across the
  native `DiInstallDriverW` path and the `pnputil` fallback to a single shared
  code (`errorSuccessRebootRequired = 3010`); dropped the now-unused
  `NativeInstallEnabled`/`NativeInstallAvailable` indirection.
- `internal/app/native_elevate_windows.go`: hoisted the Win32 DLL/proc handles
  to package-level vars (loaded lazily) instead of re-binding them on every
  relaunch.
- `internal/app/evidence.go`: normalized the DriverStore package-creation
  timestamp to `RFC3339/UTC`.
- `internal/inventory/windows_legacy_ps.go`: pinned the legacy PowerShell
  oracle to the stable `C:\Windows\System32\WindowsPowerShell\...\powershell.exe`
  path.
- `docs/thermo-nuclear-code-quality-review.md`: appended Round 7 (only the
  10th section) with findings `R7-1..R7-5`, verified file sizes, and the
  run verification matrix.

### Recorded (non-blocking round-7 findings)

- `R7-3`: `HistoryRecord` CSV column order is authored in three places
  (header slice, write literal, read field list) — kept, to preserve the CSV
  line format.
- `R7-5`: the Windows command-line quoting algorithm exists in both Go
  (`quoteWindowsArgument`) and WPF (`ConvertTo-CommandLineArgument`) — cannot
  be reused across layers, both tested, noted as a drift risk.

### Testing

- [OK] `go build ./...`, `go vet ./...`, `go test ./...` all packages pass.
- [OK] `gofmt -l` clean.
- [OK] `go build`/`go vet -tags legacyps ./...` OK.
- [OK] `scripts/verify.ps1` — 14 steps, 0 failed, `VERIFY_OK`.

### Status

[OK] **Completed** and pushed to `origin/main`.
