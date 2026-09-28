package app

// HelpText mirrors the PS1 help output and public CLI contract.
const HelpText = `Lenovo driver installer (Go migration)

Usage:
  lenovo-driver [-DryRun] [-CurrentOSOnly] [-LatestAcrossOS] [-TargetOS <OSID|OSName>]
                [-SkipHashCheck] [-SkipSignatureCheck] [-IncludeBios] [-DownloadOnly]
                [-DownloadDir <path>] [-Model <model>] [-Help]
  lenovo-driver -GuiExportPath <path> [-Model <model>]
  lenovo-driver -GuiInstallCodes <code1,code2,...>
  lenovo-driver -Rollback <code1,code2,...>
  lenovo-driver -Audit <snapshotPath>

Options:
  -DryRun          Build and print the plan only; do not download or install.
  -CurrentOSOnly   Disable the interactive t action that switches supported OS lists.
  -LatestAcrossOS  Select the newest driver across all supported OS lists (explicit and experimental).
  -TargetOS        Select drivers for a specific Lenovo OS ID or OS name.
  -SkipHashCheck       Skip local SHA-256 companion and official MD5 verification.
  -SkipSignatureCheck  Skip Authenticode signature verification of downloaded files.
  -IncludeBios         Include firmware packages (BIOS/EC/ME/TPM/Thunderbolt/UEFI). Default skips them.
  -DownloadOnly    Download verified files without installing.
  -DownloadDir     Download directory. Default is %TEMP%\LenovoDrivers.
  -Model           Lenovo machine model override when automatic lookup fails.
  -Elevated        Skip the automatic UAC relaunch when the process is already elevated.
  -GuiExportPath   Export the WPF-compatible JSON view and exit.
  -GuiInstallCodes Install only the comma-separated DriverCode values from a GUI export.
  -Rollback        Roll back the device drivers for the comma-separated DriverCode values
                   that have a pending rollback offer (rolls back the driver, or reinstalls
                   the previous INF when no backup exists; never package cleanup).
  -Audit           Snapshot the current device drivers. On first run it writes a baseline;
                   on later runs it reports the device-level diff since the last snapshot
                   (added/removed devices, driver version/INF/date/problem-code changes)
                   and advances the baseline. Each change is attributed to the driver
                   codes this tool recorded against that device, when any exist.
                   Read-only against device state.
  -Help            Show this help.

Interactive choices:
  y  Install the update-only driver set.
  a  Install all applicable drivers.
  s  Select driver numbers manually, for example 1,3,5.
  t  Switch to another supported OS list when multiple OS entries are available.
  n  Cancel.

Before each prompt, the exact driver set is printed with driver code, name,
remote version, local version, and status.

Data source:
  QuickFix official API is tried first, then the official webpage API.
  -LatestAcrossOS merges all supported OS lists and is not the default path.

Exit codes:
  0  Success or no driver selected.
  1  Download, install, or rollback failure.
  2  Invalid flag combination.
  3  GUI driver code mismatch.

Artifacts:
  %LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_install.log
  %LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_history.csv
  %LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_plan.txt (per-run plan view)
`
