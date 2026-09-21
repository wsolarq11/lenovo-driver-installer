# Lenovo Driver Installer Technical Documentation

This document describes the current Go-based repository and is the reference for
building, running, extending, and reproducing the project from a clean checkout.

## 1. Purpose

Lenovo Driver Installer is a Windows desktop tool for Lenovo machines sold in
mainland China. It resolves the machine model and serial number, queries the
official Lenovo driver APIs, compares the official driver list with the local
driver state, and lets the user download or install selected applicable
drivers.

The runtime engine is a Go CLI. A PowerShell WPF front end provides a desktop
table UI and communicates with the Go CLI through JSON export files and
`DriverCode` selections. There is no other runtime engine in the repository.

## 2. Architecture

```text
User
  |
  | install_lenovo_drivers.bat / install_lenovo_drivers_wpf.bat
  v
WPF layer (lenovo_driver_wpf.ps1)
  |
  | Start-Process bin\lenovo-driver.exe
  | -GuiExportPath <json> / -GuiInstallCodes <codes>
  v
Go CLI (cmd/lenovo-driver -> internal/app)
  |
  | system calls
  v
Lenovo API + Windows inventory + installers + files
```

The Go CLI owns all driver business logic:

- API access and driver normalization
- local machine, OS, PnP, installed application, and software snapshots
- driver applicability and version comparison
- source audit from history, PnP properties, DriverStore, and setupapi logs
- download retry, URL refresh, MD5 and SHA-256 integrity checks
- MSI, INF, ZIP, CAB, and EXE installation dispatch
- plan, log, history, and JSON export artifacts

`lenovo_driver_wpf.ps1` is only the presentation shell. It renders the JSON
export, starts background Go processes, and forwards selected `DriverCode`
values. It does not implement driver matching or installation logic.

## 3. Repository Layout

```text
cmd/lenovo-driver            CLI entry point
internal/api                 Lenovo API clients and response normalization
internal/app                 orchestration, CLI, GUI export, history, prompts
internal/audit               setupapi parsing and source evidence audit
internal/compare             version parsing, matching, selection, formatting
internal/download            HTTP download, retry, SHA-256 companion
internal/install             installer dispatch and EXE fallback; native DiInstallDriverW INF path with pnputil fallback
internal/inventory           Windows machine/OS/PnP/app snapshots via native SetupAPI/CfgMgr32/registry; no PowerShell in runtime
internal/model               shared plain data types
internal/pathutil            Windows path helpers
internal/plan                plan text, tables, history row building
internal/trust               Authenticode signature verification via WinVerifyTrust
wpf/window.xaml              WPF window layout
wpf/ui.ps1                   WPF window construction and UI helpers
wpf/worker.ps1               WPF background worker state machine
wpf/actions.ps1              WPF install action confirmation and dispatch
scripts/verify.ps1           offline acceptance gate
lenovo_driver_wpf.ps1        WPF presentation entry point
install_lenovo_drivers.bat   thin CLI launcher
install_lenovo_drivers_wpf.bat thin WPF launcher
docs/TECHNICAL.md            this document
```

The deterministic packages (`api`, `compare`, `audit`, `plan`, `model`) do not
touch network, registry, PnP, console, process, or file APIs. Side effects live
in `app`, `download`, `install`, and `inventory`.

## 4. Prerequisites

- Windows 10 or Windows 11
- Go 1.27 or newer from the official Go toolchain
- Windows PowerShell 5.1 for the WPF layer
- Administrator privileges for driver installation
- Internet access to the Lenovo official API hosts

## 5. Build

From the repository root:

```powershell
go build ./...
go test ./...
go vet ./...
gofmt -w cmd internal
go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver
```

The batch launcher builds `bin\lenovo-driver.exe` automatically when it is
missing.

## 6. Run

### 6.1 CLI

```bat
install_lenovo_drivers.bat -DryRun
install_lenovo_drivers.bat
```

Useful modes:

```text
-DryRun                          build and print the plan only
-TargetOS 248                    inspect one supported OS list
-TargetOS "Windows 11 64-bit"    same by OS name
-LatestAcrossOS                  experimental merge across all supported OS lists
-DownloadOnly                    download verified files without installing
-DownloadDir <path>              choose download directory
-IncludeBios                     include firmware packages (BIOS/EC/ME/TPM/Thunderbolt/UEFI)
-SkipHashCheck                   disable hash verification
-SkipSignatureCheck              disable Authenticode verification
-Model <model>                   override machine model lookup
```

The CLI automatically relaunches through UAC when installation is requested and
the process is not elevated.

### 6.2 WPF

```bat
install_lenovo_drivers_wpf.bat
```

The WPF window loads the same official driver view, displays
`Update` / `Up to date` / `Not installed` / `Local newer` / `Not applicable`,
and supports refresh, install update-only, install all applicable, install
selected, and download selected.

Automated smoke modes:

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

## 7. End-to-End Data Flow

1. `internal/app` reads machine model and serial through CIM/WMI via PowerShell.
2. It reads Windows caption, architecture, and the Lenovo OS match key.
3. `internal/api` resolves the Lenovo machine category ID.
4. It resolves the current OS entry and the supported OS list.
5. It loads the driver list from QuickFix `SearchForXbb` first, then falls back
   to the official webpage API.
6. `internal/inventory` snapshots PnP devices, installed applications, and
   software-only driver evidence.
7. `internal/compare` filters to applicable drivers and compares remote/local
   versions.
8. `internal/audit` builds source evidence from history, DriverStore, and
   `setupapi.*.log`.
9. The plan file and optional WPF JSON export are written.
10. The user chooses drivers from the CLI or WPF table.
11. Downloads are verified by size, official MD5 when present, and a local
    SHA-256 companion.
12. Installers are dispatched by file type with timeouts and fallbacks.
13. A post-install verification pass records before/after local versions in
    `lenovo_driver_history.csv`.

## 8. Lenovo API Integration

Primary driver source:

```text
POST https://ptstpd.lenovo.com.cn/home/driver/SearchForXbb
Content-Type: application/json
Body: {"searchKey":"<category-id>","osid":"<osid>"}
```

Fallback driver source:

```text
GET https://newsupport.lenovo.com.cn/api/drive/drive_listnew?searchKey=<category-id>&sysid=<osid>
```

The webpage API can be preferred by passing the source hint `Web`. The OS list
is resolved from the same API family. When a downloaded URL returns HTTP 403,
the engine queries the refreshed URL from the official driver list and retries
once with the new `FilePath`.

## 9. Comparison and Selection

- The default mode is current OS only.
- `-TargetOS` selects one explicit OS list.
- `-LatestAcrossOS` merges every supported OS list and then selects the newest
  driver by parsed version, with issue date/edition as tie-breakers.
- `-CurrentOSOnly` disables the interactive `t` OS switch.
- `Local newer` is not treated as a failure. Source evidence labels it as
  offline image integration, online package installation, pre-existing
  DriverStore package, another official OS source, same-current-OS history, or
  unknown.
- Software-only packages resolve their local version from installed
  applications/services, not from unrelated same-vendor PnP devices.
- Firmware packages (BIOS/EC/ME/TPM/Thunderbolt/UEFI) are skipped unless
  `-IncludeBios` is passed.
- Matched devices are read with their Windows problem code via
  `CM_Get_DevNode_Status`. A driver whose matched devices report a problem
  carries a `DeviceProblem` summary in the assessment, plan, and GUI export.

## 10. Download and Integrity

Download output names use `DriverCode_FileName`.

For every file:

1. Check cached file size against the official size.
2. Check the local SHA-256 companion.
3. Check official MD5 when the source provides one.
4. If verification fails, remove the cached file and download again.
5. After download, compute the local SHA-256 and write `<file>.sha256`.
6. Authenticode-verify the file through WinVerifyTrust (`internal/trust`); a
   failed signature rejects the file. `-SkipSignatureCheck` disables this check
   for diagnostic unsigned fixtures only.

`-SkipHashCheck` bypasses companion and MD5 checks but is not the default path.

## 11. Installer Dispatch

| Extension | Strategy |
|---|---|
| `.msi` | `msiexec.exe /i <file> /qn /norestart`; 3010/1641 treated as success |
| `.inf` | `DiInstallDriverW` (newdev.dll) first; falls back to `pnputil.exe /add-driver <file> /install`; exit 1 treated as reboot-required success |
| `.zip` | expand, walk extracted INFs, native-first INF install for each INF |
| `.cab` | `expand.exe <file> -F:* <dir>`, then native-first INF install |
| `.exe` | silent installer first, then extracted fallback on timeout/failure |

The install channel does not use Windows Update. The runtime is native-only:
official Lenovo API list -> verified download -> native `DiInstallDriverW` for
INF packages, with system-native `pnputil` as a fallback when the API is
unavailable.

EXE fallback behavior:

- If `<working-dir>\<DriverCode>.log` pins an `is-*.tmp` directory, that
  directory is used first.
- Without log evidence, the engine scans recent `is-*.tmp` roots only for
  NVIDIA driver names and only directories containing `Display.Driver`.
- The fallback runs `setup.exe` or `nvsetup.exe`, then INFs as a final option.
- When the silent EXE fails and no extracted fallback is usable, the CLI offers
  `r` to run the EXE interactively or `s` to skip.

Processes are bounded by timeouts. Timed-out process trees are terminated with
`taskkill.exe /PID <pid> /T /F`.

## 12. Artifacts

```text
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_install.log   operation log
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_plan.txt      human-readable plan (per-run view)
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_history.csv  CSV history with before/after versions
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_rollback.json  rollback offers (GUI display cache; ledger is the authority)
%TEMP%\LenovoDrivers\                                            default download directory
<download-dir>\<DriverCode>_<file>                               downloaded package
<download-dir>\<DriverCode>_<file>.sha256                        local SHA-256 companion
```

`lenovo_driver_history.csv` contains timestamp, driver code, OSID/OSName,
driver name, remote version, verified version, before version, file name, MD5,
source, result, and message. The Go reader strips a UTF-8 BOM so legacy history
files remain readable.

## 13. GUI JSON Protocol

The Go CLI writes a JSON object with:

```json
{
  "GeneratedAt": "...",
  "MachineModel": "...",
  "SerialNumber": "...",
  "SystemCaption": "...",
  "CurrentOsId": "...",
  "CurrentOsName": "...",
  "ListOsId": "...",
  "ListOsName": "...",
  "DataSource": "...",
  "OsList": [
    {"OSID": "...", "OSName": "..."}
  ],
  "Drivers": [
    {
      "Selected": false,
      "DriverCode": "...",
      "DriverName": "...",
      "Version": "...",
      "LocalVersion": "...",
      "CompareStatus": "...",
      "SourceAudit": "...",
      "CompareSource": "...",
      "FileName": "...",
      "FilePath": "...",
      "FileSize": "...",
      "MD5": "...",
      "IsApplicable": true,
      "IsUpdate": false
    }
  ]
}
```

`SourceAudit` is the canonical string form `Category: Summary`. WPF renders
drivers directly from this JSON and sends `-GuiInstallCodes` back to the CLI.

## 14. Exit Codes

```text
0  success or no driver selected
1  download/install/runtime failure
2  invalid flag combination
3  GUI driver code mismatch
```

## 15. Verification

Run the full offline gate from the repository root:

```powershell
.\scripts\verify.ps1
```

The gate runs:

- Go build/test/vet/gofmt
- PowerShell parser checks for the WPF file
- CLI help and invalid flag-combination checks
- `git diff --check`
- workspace hygiene checks

It never calls the real Lenovo API, downloads drivers, or installs anything.

After building the engine, run the WPF smoke checks:

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

Real-machine native inventory checks (opt-in; skipped by the offline gate):

```powershell
$env:LENOVO_NATIVE_SMOKE=1; go test ./internal/inventory/ -run TestNativeSmoke -count=10 -v
# native-vs-PowerShell equivalence requires the legacyps test oracle tag
$env:LENOVO_NATIVE_EQUIV_SMOKE=1; go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke -v
```

Native install API smoke (does not install a real driver):

```powershell
$env:LENOVO_NATIVE_INSTALL_SMOKE=1; go test ./internal/install/ -run TestNativeInstallSmoke -count=3 -v
```

The native path uses SetupAPI enumeration plus CfgMgr32 `CM_Get_Device_IDW`
for stable device instance IDs, then reads driver properties from the class
registry keys. The runtime does not spawn PowerShell; the legacy PowerShell
oracle is compiled only with `-tags legacyps` for equivalence verification.

Real API, real hardware, and real driver installation require an interactive
Windows machine and are outside the offline gate.

## 16. Reproduce From a Clean Checkout

1. Install the official Go 1.27 (or newer) toolchain.
2. Clone or copy the repository to a Windows machine.
3. Open PowerShell in the repository root.
4. Run `.\scripts\verify.ps1`.
5. Build the engine with `go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver`.
6. Run `.\install_lenovo_drivers.bat -DryRun` to verify machine/API resolution
   without downloading or installing.
7. Run `.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation` to verify the WPF
   JSON bridge against the real API.
8. For an actual installation, run `.\install_lenovo_drivers.bat` or
   `.\install_lenovo_drivers_wpf.bat` from an administrator shell.

## 17. Development Rules

- Keep deterministic logic in `internal/compare`, `internal/audit`,
  `internal/plan`, and `internal/api`.
- Keep side effects in `internal/app`, `internal/install`, `internal/download`,
  and `internal/inventory`.
- Preserve public CLI parameters, interactive choices, exit codes, cache rules,
  and installer fallbacks.
- Do not add another runtime language entry point.
- Every behavior change must update tests and must pass `scripts/verify.ps1`.
- Do not resurrect the deleted legacy PowerShell engine files.
