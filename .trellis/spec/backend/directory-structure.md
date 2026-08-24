# Directory Structure

> How this repository is organized.

---

## Overview

The project deliberately keeps deployment small: one PowerShell installer, one
batch wrapper, and two user-facing documents. There are no packages, workspaces,
or build artifacts.

---

## Directory Layout

```text
install_lenovo_drivers.ps1         Main installer runtime and CLI
install_lenovo_drivers.bat         Thin elevated entry point
README.md                          Operational guide and troubleshooting
lenovo_installer_improvement_plan.md  Implemented v4 scope contract
.trellis/                          Trellis workflow, tasks, specs, journals
```

---

## Script Organization

`install_lenovo_drivers.ps1` is intentionally a single file. Its sections are
declared with `#region` blocks so maintainers can navigate it without a module
split:

| Region | Contents |
|--------|----------|
| Configuration | API base URL, log path, plan path |
| Logging | `Write-Log` |
| Environment and system info | Admin check, machine info, OS info |
| Lenovo API | API calls, category lookup, driver list parsing |
| Local device and app inventory | PnP devices and installed applications |
| Driver version comparison | Applicability and version matching |
| Console and plan output | Table, summary, plan file |
| Driver selection | Latest-driver grouping and interactive selection |
| Download integrity | Size, SHA-256 companion, retry |
| Process and installer helpers | Timeouts, pnputil, MSI/EXE/zip/cab handling |
| Help and interactive selection | `-Help` and user prompts |
| Main | Orchestration from startup through exit |

---

## Module Organization

- Keep all runtime logic in `install_lenovo_drivers.ps1`.
- Keep `install_lenovo_drivers.bat` thin: it only invokes the script with
  `-ExecutionPolicy Bypass` and forwards exit codes.
- Do not create PowerShell modules unless a future task changes the deployment
  contract.

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
