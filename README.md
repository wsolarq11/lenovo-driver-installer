# Lenovo Driver Installer

A Go installer for Lenovo machines. It detects the current machine model at
runtime, queries the official Lenovo driver API, compares available drivers
with locally installed versions, and installs only the selected applicable
drivers. The Go CLI is the deterministic engine; WPF remains the desktop
presentation layer.

Full reproduction and development details are in
[docs/TECHNICAL.md](docs/TECHNICAL.md).

## Fact Standard

The factual standard for Lenovo driver decisions is documented in
[DRIVER_FACT_STANDARD.md](DRIVER_FACT_STANDARD.md). In short:

- The normal source is the official Lenovo driver list for the installed OS.
- `Local newer` is usually a source mismatch, not a bug, and is not a reason to
  downgrade or switch OS lists.
- The official QuickFix tool is good for one-click current-OS matching, but it
  is not a dry-run/audit tool.
- The installer remains a current-OS-first dry-run, selection, logging, and
  integrity-checking tool; `-TargetOS` is the explicit way to inspect another
  supported OS list, while `-LatestAcrossOS` is an experimental merge mode that
  is not the standard update path.

## Quick Start

The first run builds `bin\lenovo-driver.exe` when Go is available, then invokes
the Go CLI from an elevated PowerShell or Command Prompt:

```bat
install_lenovo_drivers.bat
```

The batch wrapper requests administrator rights when they are missing. During a
normal run, the installer prompts you to choose what to install:

- `y` installs update-only drivers.
- `a` installs all applicable drivers.
- `s` selects driver numbers manually, for example `1,3,5`.
- `t` switches to another supported OS list when the machine has multiple OS entries.
- `n` cancels.

Before the input prompt, the installer prints the exact driver set for `y`
and for `a`, including driver code, name, remote version, local version, and
status, so the choice is visible before anything is downloaded.

## Desktop UI (WPF)

A WPF front end is available for the same engine:

```bat
install_lenovo_drivers_wpf.bat
```

The WPF window loads the official driver list through the installer engine,
shows the same `Update` / `Up to date` / `Not installed` / `Local newer`
statuses, preserves the source audit labels, and lets you choose which OS list
to display. The buttons mirror the CLI choices:

- `刷新驱动列表` loads the current machine list, or the selected supported OS list.
- `安装更新项 (y)` installs only drivers marked `Update`.
- `安装全部可安装 (a)` installs every applicable driver.
- `安装选中项` installs only the rows checked in the table.
- `仅下载选中项` downloads the checked rows without installing them.

The WPF script does not duplicate driver decision logic. It starts the Go CLI
in a child process with JSON export or install arguments, then renders the same
deterministic comparison results in the desktop UI. Build the engine once with
`go build -o bin\lenovo-driver.exe ./cmd/lenovo-driver` before launching the
WPF wrapper.


A dry run never downloads or installs anything:

```bat
install_lenovo_drivers.bat -DryRun
```

To inspect the official driver list for another supported OS without merging
lists, pass its OSID or OS name:

```bat
install_lenovo_drivers.bat -DryRun -TargetOS 248
install_lenovo_drivers.bat -DryRun -TargetOS "Windows 11"
```

To compare newer driver versions from other Lenovo OS entries:

```bat
install_lenovo_drivers.bat -LatestAcrossOS
```

## How It Works

1. Resolve the machine model and serial number.
2. Detect the current Windows edition and architecture.
3. Resolve the Lenovo machine category ID.
4. Load the current-OS driver list from the official QuickFix backend
   (`SearchForXbb`), falling back to the official webpage API.
5. Filter BIOS/EC packages unless explicitly enabled.
6. Snapshot local devices and installed applications.
7. Compare remote and local versions and mark each driver as:
   - `Update`
   - `Up to date`
   - `Not installed`
   - `Local newer`
   - `Unknown`
   - `Not applicable`
8. For every applicable driver, audit local source evidence: install history,
   DriverStore import records from `setupapi.*.log`, current OS match, and
   alternate OS match. `Local newer` is labeled from this audit plus the
   alternate OS list instead of treating it as an error. The import parser
   preserves the setupapi `cmd:` from `Driver Install`/`Device Install`
   sections, so real `pnputil.exe` or installer command lines are printed in
   the plan instead of hidden.
9. Software-only packages compare against their installed application version;
   a PnP device that merely shares a vendor name is not treated as evidence.
10. Cross-OS mode selects the actual newest parsed version, not simply the
    newest published row.
11. Write a detailed plan file and show a compact console table.
12. Ask which drivers to download and install.
13. Validate cached or downloaded files with size, official MD5 when provided,
    and a local SHA-256 companion file.
14. Install them, write a CSV history record, and run one post-install
    verification pass that records the actual before/after local versions.

## Options

| Option | Meaning |
| --- | --- |
| `-DryRun` | Compare versions without downloading or installing. |
| `-CurrentOSOnly` | Use only the current OS driver list. This is the default. |
| `-LatestAcrossOS` | Allow newer drivers from other OS entries. Cannot be used with `-CurrentOSOnly`. |
| `-TargetOS <OSID\|OSName>` | Show and compare against one supported OS list, for example `248` or `Windows 11`. Cannot be used with `-CurrentOSOnly` or `-LatestAcrossOS`. |
| `-SkipHashCheck` | Skip local SHA-256 companion-file validation. |
| `-IncludeBios` | Include BIOS/EC packages. They are skipped by default. |
| `-DownloadOnly` | Download applicable files without installing. |
| `-DownloadDir <path>` | Override the download directory. Defaults to `%TEMP%\LenovoDrivers`. |
| `-Model <model>` | Override automatic machine model lookup, for example `82JQ`. |
| `-Elevated` | Skip elevation. Used internally by the WPF wrapper. |
| `-GuiExportPath <path>` | Write the WPF-compatible JSON driver view and exit. |
| `-GuiInstallCodes <codes>` | Install only the comma-separated driver codes from a GUI export. |
| `-Help` | Show usage help. |

`-CurrentOSOnly`, `-LatestAcrossOS`, and `-TargetOS` are mutually exclusive:
`-TargetOS` selects one explicit supported OS list, while `-LatestAcrossOS`
merges newer versions from other OS entries. Passing incompatible options
fails fast with exit code `2`.

## Safety And Integrity

- Current-OS-only comparison is the safe default; cross-OS comparison is an
  explicit experimental opt-in.
- Driver lists are loaded from the official QuickFix backend first and fall
  back to the official webpage API if that backend is unavailable.
- Downloads are stored with unique `DriverCode_FileName` names.
- File size is checked against the Lenovo driver list when available.
- Official `MD5` is validated when the data source provides it; cached files
  are also checked against it before reuse.
- Fresh downloads receive a local `.sha256` companion file, and cached files
  are validated before reuse.
- `-SkipHashCheck` bypasses SHA-256 and official MD5 validation but still
  enforces non-empty files and size checks when available.
- Expired Lenovo CDN URLs are refreshed from the current driver list before
  retrying.
- EXE, MSI, pnputil, and expand runs use a timeout and kill the full process
  tree when a run stalls.
- Installer exit codes `3010` and `1641` are treated as success with reboot
  required.
- If an Inno-style wrapper stalls or fails, the installer attempts extracted
  INF installation through `pnputil` or an inner installer where available.
- If that fallback also fails for an EXE, the console asks whether to run the
  downloaded EXE interactively before marking the driver failed.

## Installer Handling

| File type | Strategy |
| --- | --- |
| `.exe` | Silent Inno-style flags, Lenovo `InstallCode` arguments when provided, timeout, log capture, and extracted-installer fallback. |
| `.msi` | `msiexec /i <file> /qn /norestart` with timeout. |
| `.inf` | `pnputil /add-driver <file> /install` with timeout. |
| `.zip` | Expand and install every contained INF. |
| `.cab` | Expand with `expand.exe` and install every contained INF. |

## Logs And Plans

- Log file: `%TEMP%\lenovo_driver_install.log`
- Plan file: `%TEMP%\lenovo_driver_plan.txt`
- Driver history: `%TEMP%\lenovo_driver_history.csv`
- Source audit evidence: `C:\Windows\INF\setupapi.offline.log`,
  `C:\Windows\INF\setupapi.dev.log`, and `C:\Windows\INF\setupapi.setup.log`
- Download directory: `%TEMP%\LenovoDrivers` unless `-DownloadDir` is used.

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Completed successfully, or no drivers were selected. |
| `1` | One or more downloads or installs failed. |
| `2` | Invalid flag combination. |
| `3` | A `-GuiInstallCodes` driver code was not found in the export. |

## Troubleshooting

- **Machine lookup fails**: run with `-Model "<model>"`, for example
  `install_lenovo_drivers.bat -Model "82JQ"`.
- **Current OS entry is not found**: confirm the machine model is correct and
  the Lenovo API returns an OS list. Do not use `-LatestAcrossOS` as a blind
  workaround.
- **A silent installer fails**: the installer log path is printed. Use `s` to
  skip, or run the downloaded file interactively from
  `%TEMP%\LenovoDrivers`.
- **A driver still reports an old version after install**: the post-install
  recheck logs `unchanged`; a reboot may be required.
- **Download returns `403`**: the script refreshes the URL from the current
  driver list and retries once.

## Architecture

The runtime is a functional core with an imperative shell:

- `internal/` contains the Go deterministic core: API parsing, inventory,
  comparison, audit, plan formatting, download integrity, installer dispatch,
  and path utilities.
- `cmd/lenovo-driver` is the CLI orchestration shell. It owns flag parsing,
  elevation, API calls, system snapshots, history files, logging, prompts, and
  download/install side effects.
- `lenovo_driver_wpf.ps1` is the desktop presentation layer. It launches the Go
  CLI in a child process, renders the JSON driver view in a WPF table, and
  forwards user-selected driver codes back to the same CLI for
  download/install.
- `install_lenovo_drivers.bat` and `install_lenovo_drivers_wpf.bat` stay thin
  and are the recommended entry points. The CLI wrapper builds the Go
  engine when needed; the WPF wrapper expects `bin\lenovo-driver.exe`.

The Go packages are small and focused. Files stay under 500 lines, public
functions use explicit error returns, and offline tests cover parsing,
matching, comparison, history, download integrity, and path handling.

## Development

The Go engine is organized by package: API, inventory, comparison, audit,
plan, download, install, and app orchestration. The deterministic core is
side-effect-free where practical so it can be tested without a real machine or
network.

Build and verify the Go engine with:

```powershell
go build ./...
go test ./...
go vet ./...
gofmt -w internal cmd
go build -o bin\lenovo-driver.exe .\cmd\lenovo-driver
```

Run the full offline acceptance gate with:

```powershell
.\scripts\verify.ps1
```

Smoke-check the WPF render and the real API bridge after building the Go
engine:

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

The architecture, API contract, installer behavior, and clean-checkout
reproduction steps are documented in [docs/TECHNICAL.md](docs/TECHNICAL.md).
