# Lenovo Driver Installer

PowerShell installer that detects the current Lenovo machine model at runtime, queries the official Lenovo driver API, compares available drivers with locally installed versions, and installs only the selected applicable drivers.

## Files

- `install_lenovo_drivers.bat` - thin entry point for elevated PowerShell execution.
- `install_lenovo_drivers.ps1` - the installer implementation.
- `lenovo_installer_improvement_plan.md` - balanced v4 scope and contracts.

## Usage

```bat
install_lenovo_drivers.bat -LatestAcrossOS
```

Interactive choices:

- `y` - install update-only drivers.
- `a` - install all applicable drivers.
- `s` - select driver numbers manually.
- `n` - cancel.

## Key Options

| Option | Meaning |
| --- | --- |
| `-DryRun` | Compare versions without downloading or installing. |
| `-CurrentOSOnly` | Use only the current OS driver list (default). |
| `-LatestAcrossOS` | Allow newer drivers from other OS entries. |
| `-SkipHashCheck` | Skip local SHA-256 cache validation. |
| `-IncludeBios` | Include BIOS/EC packages. |
| `-DownloadOnly` | Download only; do not install. |
| `-DownloadDir` | Download directory. |
| `-Help` | Show help. |

## Safety Notes

- Downloads are cached with SHA-256 companion files and validated before reuse.
- Installer exit codes `3010` and `1641` are treated as success with reboot required.
- Stale Lenovo CDN URLs are refreshed from the driver list before retrying.
- If an Inno wrapper stalls, the script attempts extracted INF installation through `pnputil` or the inner installer as appropriate.
