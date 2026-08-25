# Directory Structure

> How this repository is organized.

---

## Overview

The project deliberately keeps deployment small: one deterministic PowerShell
core, one side-effect PowerShell shell, one batch wrapper, and two user-facing
documents. There are no packages, workspaces, or build artifacts.

---

## Directory Layout

```text
install_lenovo_drivers.ps1         Side-effect shell, CLI, and orchestration
lenovo_driver_core.ps1             Deterministic decision logic
lenovo_driver_core.tests.ps1       Offline pure-core self-tests
install_lenovo_drivers.bat         Thin elevated entry point
README.md                          Operational guide and troubleshooting
lenovo_installer_improvement_plan.md  Implemented v5 scope contract
.trellis/                          Trellis workflow, tasks, specs, journals
```

---

## Script Organization

`install_lenovo_drivers.ps1` is the side-effect shell. It dot-sources
`lenovo_driver_core.ps1` and uses `#region` blocks for navigation:

| Region | Contents |
|--------|----------|
| Configuration | API base URL, log path, plan path |
| Deterministic core | Dot-source of `lenovo_driver_core.ps1` |
| Logging | `Write-Log` |
| Environment and system info | Admin check, machine info, OS info |
| Lenovo API | API calls, category lookup, driver list loading |
| Local device and app inventory | PnP devices and installed applications |
| Driver version comparison | Side-effect snapshot assembly for core resolution |
| Console and plan output | Render deterministic formatting to console/file |
| Driver selection | Core selection usage |
| Download integrity | Size, SHA-256 companion, retry |
| Process and installer helpers | Timeouts, pnputil, MSI/EXE/zip/cab handling |
| Help and interactive selection | `-Help` and user prompts |
| Main | Orchestration from startup through exit |

`lenovo_driver_core.ps1` is the deterministic core. It must not call network,
registry, PnP, file, console, `Read-Host`, or process APIs. Its functions take
plain driver/device/history objects and return plain results.

---

## Module Organization

- Keep deterministic decision logic in `lenovo_driver_core.ps1`.
- Keep side effects in `install_lenovo_drivers.ps1`.
- Keep `install_lenovo_drivers.bat` thin: it only invokes the script with
  `-ExecutionPolicy Bypass` and forwards exit codes.
- Do not create PowerShell modules or add more runtime files unless a future
  task changes the deployment contract.

---

## Naming Conventions

- Use PowerShell approved verbs for functions: `Get-`, `Test-`, `Write-`,
  `Show-`, `Select-`, `ConvertTo-`, `Invoke-`, `Format-`, `Read-`, `Find-`,
  `Install-`, `Resolve-`, `Compare-`, `Parse-`.
- Use PascalCase for functions and parameters.
- Use `$camelCase` for local variables.
- Use `#region <Name>` / `#endregion` for script sections.

---

## Examples

- Region boundaries: `install_lenovo_drivers.ps1` lines starting with
  `#region Configuration` through `#region Main`.
- Thin wrapper: `install_lenovo_drivers.bat` is six lines and delegates
  directly to the PowerShell file.
