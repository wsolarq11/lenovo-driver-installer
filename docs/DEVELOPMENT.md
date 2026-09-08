# Development Pipeline

> Single authoritative guide for the Lenovo driver installer development and
> build flow. Development commands live here only; the README links to this
> file instead of repeating them.

---

## Quick Reference

| What | Command |
| --- | --- |
| Fast inner loop (build → vet → fmt → test → build bin) | `.\scripts\dev.ps1` |
| Full offline acceptance gate (self-counted steps) | `.\scripts\verify.ps1` |
| Inner loop + full gate in one run | `.\scripts\dev.ps1 -Verify` |
| Fast loop without rebuilding `bin\lenovo-driver.exe` | `.\scripts\dev.ps1 -SkipBin` |
| Run every offline step without a shell | push/PR: `scripts/verify.ps1` in CI |

---

## Two-Loop Model

The pipeline is split into a **fast inner loop** and a **full acceptance gate**:

- **Inner loop** (`scripts/dev.ps1`) — for day-to-day iteration. It runs the
  quick checks that catch almost all breakage in seconds and rebuilds the
  engine binary so the WPF wrapper can run right after:
  1. `go build ./...`
  2. `go vet ./...`
  3. `go fmt -l internal cmd` (fails on unformatted files)
  4. `go test ./...`
  5. `go build -o bin\lenovo-driver.exe ./cmd\lenovo-driver` (omit with `-SkipBin`)

  Exits `DEV_OK` when every step passes, otherwise the failing step name and
  exit code `1`.

- **Acceptance gate** (`scripts/verify.ps1`) — the authoritative offline gate
  that runs in CI. Beyond the inner loop it also checks coverage floors,
  PowerShell 5.1 parsing/BOM of the WPF layer, CLI smoke behavior, git-diff
  hygiene, and untracked-artifact hygiene, then prints `VERIFY_OK`.

`go` toolchain discovery is shared: `scripts/lib/go-toolchain.ps1` provides
`Resolve-GoExe`, used by both `scripts/dev.ps1` and `scripts/verify.ps1`, so the
lookup rules never drift. Both accept an explicit `-GoExe <path>` override.

---

## Real-Machine Smokes

These are opt-in and only run on a Windows machine with Lenovo hardware.
They are **not** part of the offline `verify.ps1` gate.

```powershell
# Native inventory quality (device snapshot reads real SMBIOS/SetupAPI)
$env:LENOVO_NATIVE_SMOKE=1; go test ./internal/inventory/ -run TestNativeSmoke -count=10 -v

# Native-vs-PowerShell equivalence (needs the legacyps test oracle tag)
$env:LENOVO_NATIVE_EQUIV_SMOKE=1; go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke -v

# Native install API smoke (does not install a real driver)
$env:LENOVO_NATIVE_INSTALL_SMOKE=1; go test ./internal/install/ -run TestNativeInstallSmoke -count=3 -v
```

After building the engine (`scripts/dev.ps1`), smoke-check the WPF surface:

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

---

## Continuous Integration

`.github/workflows/verify.yml` runs `scripts/verify.ps1` on `windows-latest`
(Go 1.27.x, locked to `GOTOOLCHAIN=local` so the CI version always matches
`go.mod`) for every push and pull request. The tracked CI runs only the offline
gate — the hardware-dependent native and WPF smokes above are locally driven by
design.

---

## Guidelines

- Both scripts are offline and side-effect-free: they never call the Lenovo
  API, download drivers, or install anything.
- `scripts/dev.ps1` and `scripts/verify.ps1` print `DEV_OK` / `VERIFY_OK` and a
  numeric fail count, so automation can assert on a stable sentinel.
- Keep WPF `.ps1` files encoded as UTF-8 with BOM for Windows PowerShell 5.1;
  `verify.ps1` enforces this.
- Keep the root README "Development" section as a short pointer to this page to
  avoid command drift.