# Directory Structure

> How this repository is organized.

---

## Overview

The project keeps a small Go CLI as the runtime engine, a WPF presentation
layer, two thin batch wrappers, and user-facing documents. Build artifacts are
limited to `bin\lenovo-driver.exe` and are ignored by Git.

---

## Directory Layout

```text
cmd/lenovo-driver                 CLI entry point and flag wiring
internal/api                      Lenovo API clients and JSON parsing
internal/app                      Orchestration, GUI export, history, prompts
internal/audit                    Local source evidence audit
internal/compare                  Version comparison, matching, selection, table
internal/download                 Download retry and integrity checks
internal/install                  MSI/EXE/INF/zip/cab installer dispatch
internal/inventory                Machine, OS, device, and software inventory
internal/model                    Shared plain data types
internal/pathutil                 Windows path helpers
internal/plan                     Plan and interactive selection formatting
scripts/verify.ps1                Offline acceptance gate
lenovo_driver_wpf.ps1             WPF presentation layer
install_lenovo_drivers.bat        Thin elevated CLI entry point
install_lenovo_drivers_wpf.bat    Thin elevated WPF entry point
install_lenovo_drivers.ps1        Frozen legacy PowerShell shell
lenovo_driver_core.ps1            Frozen legacy deterministic core
README.md                         Operational guide and troubleshooting
.trellis/                         Trellis workflow, tasks, specs, journals
```

---

## Package Organization

`internal/app` is the imperative shell. It owns flag parsing, elevation, API
calls, system snapshots, history files, logging, prompts, and download/install
side effects.

The deterministic packages must not touch network, registry, PnP, console, or
process APIs. They receive plain model objects and return plain results.

`lenovo_driver_wpf.ps1` is the desktop presentation layer. It must not contain
driver matching, source attribution, or install logic. It starts the Go CLI in
a child process, reads the JSON driver view, renders the table, and forwards
checked driver codes back with `-GuiInstallCodes`.

---

## Module Organization

- Keep small focused Go packages under `internal/`; split files when a package
  grows near 500 lines.
- Keep `install_lenovo_drivers.bat` thin: it builds the Go engine when needed,
  invokes it, and forwards exit codes.
- Do not create more PowerShell runtime entry points. The legacy files stay
  frozen as a behavioral reference only.

---

## Naming Conventions

- Use Go package conventions: exported names document public contracts,
  unexported helpers stay package-private.
- Use explicit error returns instead of silent fallback values.
- Prefer specific package names such as `pathutil`, `inventory`, and `plan`
  over generic names like `util`, `manager`, or `data`.

---

## Examples

- CLI contract: `cmd/lenovo-driver/main.go` parses flags and delegates to
  `internal/app`.
- Thin wrapper: `install_lenovo_drivers.bat` builds `bin\lenovo-driver.exe`
  only when missing, then forwards all arguments.
