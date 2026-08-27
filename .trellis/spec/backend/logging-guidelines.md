# Logging Guidelines

> How the installer logs operations and results.

---

## Overview

`App.Log` writes each line to `%TEMP%\lenovo_driver_install.log` and mirrors it
to the console. GUI child processes redirect stdout/stderr into temporary files
that the WPF layer polls and displays.

---

## Log Format

```text
[2026-08-24 15:00:00] [INFO] Lenovo category ID : 123
```

The format is `[timestamp] [LEVEL] message`.

---

## Log Levels

| Level | Use |
|-------|-----|
| `INFO` | Normal lifecycle events: machine, OS, selected drivers, downloads, installs |
| `WARN` | Recoverable or noteworthy events: cache mismatch, reboot required, fallback used |
| `ERROR` | Failures that affect the result: download failure, install failure |
| `INSTALL` | Captured installer output from pnputil or inner installers |

---

## Artifacts

- Log file: `%TEMP%\lenovo_driver_install.log`
- Plan file: `%TEMP%\lenovo_driver_plan.txt`
- Download directory: `%TEMP%\LenovoDrivers` unless `-DownloadDir` is used

---

## What To Log

- Machine model, serial number, OS match key.
- Lenovo category ID and matched OS entry.
- Driver counts and comparison status.
- Plan file path, download directory, download URL refresh events.
- Installer exit codes, fallback paths, and post-install recheck results.

---

## What NOT To Log

- User-entered interactive responses.
- Full installer binaries or sensitive local file contents.
- Secrets or credentials if any are ever introduced.
