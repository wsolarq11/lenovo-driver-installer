# Lenovo Driver Installer

A PowerShell installer for Lenovo machines. It detects the current
machine model at runtime, queries the official Lenovo driver API, compares
available drivers with locally installed versions, and installs only the
selected applicable drivers. The code is split into a deterministic core and a
side-effect shell; the `.bat` wrapper remains the only user entry point.

## Fact Standard

The factual standard for Lenovo driver decisions is documented in
[DRIVER_FACT_STANDARD.md](DRIVER_FACT_STANDARD.md). In short:

- The normal source is the official Lenovo driver list for the installed OS.
- `Local newer` is usually a source mismatch, not a bug, and is not a reason to
  downgrade or switch OS lists.
- The official QuickFix tool is good for one-click current-OS matching, but it
  is not a dry-run/audit tool.
- This script remains a current-OS-first dry-run, selection, logging, and
  integrity-checking tool; `-LatestAcrossOS` is an explicit experimental
  exception, not the standard update path.

## Quick Start

Run the batch file from an elevated PowerShell or Command Prompt:

```bat
install_lenovo_drivers.bat
```

The batch wrapper requests administrator rights when they are missing. During a
normal run, the installer prompts you to choose what to install:

- `y` installs update-only drivers.
- `a` installs all applicable drivers.
- `s` selects driver numbers manually, for example `1,3,5`.
- `n` cancels.

A dry run never downloads or installs anything:

```bat
install_lenovo_drivers.bat -DryRun
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
8. For `Local newer`, use the local install history or the alternate OS list to
   label the likely source OS instead of treating it as an error.
9. Write a detailed plan file and show a compact console table.
10. Ask which drivers to download and install.
11. Validate cached or downloaded files with size, official MD5 when provided,
    and a local SHA-256 companion file.
12. Install them, write a CSV history record, and run one post-install
    verification pass.

## Options

| Option | Meaning |
| --- | --- |
| `-DryRun` | Compare versions without downloading or installing. |
| `-CurrentOSOnly` | Use only the current OS driver list. This is the default. |
| `-LatestAcrossOS` | Allow newer drivers from other OS entries. Cannot be used with `-CurrentOSOnly`. |
| `-SkipHashCheck` | Skip local SHA-256 companion-file validation. |
| `-IncludeBios` | Include BIOS/EC packages. They are skipped by default. |
| `-DownloadOnly` | Download applicable files without installing. |
| `-DownloadDir <path>` | Override the download directory. Defaults to `%TEMP%\LenovoDrivers`. |
| `-Model <model>` | Override automatic machine model lookup, for example `82JQ`. |
| `-Elevated` | Skip elevation. Used internally by the batch wrapper. |
| `-Help` | Show usage help. |

`-CurrentOSOnly` and `-LatestAcrossOS` are mutually exclusive; passing both
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
- If an Inno-style wrapper stalls or fails, the script attempts extracted INF
  installation through `pnputil` or an inner installer where available.

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
- Download directory: `%TEMP%\LenovoDrivers` unless `-DownloadDir` is used.

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Completed successfully, or no drivers were selected. |
| `1` | One or more downloads or installs failed. |
| `2` | Invalid flag combination. |

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

The runtime is deliberately split into two PowerShell files:

- `lenovo_driver_core.ps1` contains deterministic decision logic only:
  driver-list parsing, hardware matching, version comparison, source
  attribution, selection, plan/table formatting, and token parsing. It must
  not call network, registry, PnP, file, console, or process APIs.
- `install_lenovo_drivers.ps1` is the side-effect shell. It dot-sources the
  core and owns API calls, system snapshots, history files, downloads,
  installers, logging, prompts, and orchestration.
- `install_lenovo_drivers.bat` stays thin and remains the recommended entry
  point.

The shell gathers all inputs (driver objects, local devices, installed apps,
history, and source maps), passes them into core functions, and performs only
the side effects the core results require.

## Development

The shell is organized into regions for configuration, logging, system info,
Lenovo API access, local inventory, console output, selection, download
integrity, installer dispatch, and main orchestration. The deterministic core
is kept side-effect-free so it can be tested without a real machine or network.

Validate syntax with:

```powershell
$files = @('install_lenovo_drivers.ps1', 'lenovo_driver_core.ps1')
foreach ($file in $files) {
  $tokens = $null; $errors = $null
  [System.Management.Automation.Language.Parser]::ParseFile(
    $file,
    [ref]$tokens,
    [ref]$errors
  ) | Out-Null
  if ($errors) { $errors | Format-List; exit 1 } else { "PARSE OK $file" }
}
```

Run the offline core tests with:

```powershell
.\lenovo_driver_core.tests.ps1
```

The v5 scope and non-goals are documented in
`lenovo_installer_improvement_plan.md`.
