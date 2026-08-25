# Backend / PowerShell Runtime Guidelines

> Best practices for the non-UI runtime code in this repository.

---

## Overview

This is a single-repo Lenovo driver installer. The runtime is split into a
side-effect shell, `install_lenovo_drivers.ps1`, a deterministic core,
`lenovo_driver_core.ps1`, and a WPF presentation layer,
`lenovo_driver_wpf.ps1`, with thin `.bat` wrappers.

---

## Guidelines Index

| Guide | Description |
|-------|-------------|
| [Directory Structure](./directory-structure.md) | Repository layout and script organization |
| [Error Handling](./error-handling.md) | Failure propagation, exit codes, and fallbacks |
| [Logging Guidelines](./logging-guidelines.md) | Log format, levels, and artifacts |
| [Quality Guidelines](./quality-guidelines.md) | Script structure, naming, and verification |

---

## Pre-Development Checklist

- [ ] Read `README.md` and `lenovo_installer_improvement_plan.md` before
      changing CLI behavior.
- [ ] Preserve the core/shell PowerShell deployment shape unless a task
      explicitly changes it.
- [ ] Keep deterministic logic in `lenovo_driver_core.ps1` and side effects in
      `install_lenovo_drivers.ps1`.
- [ ] Keep new code inside the matching `#region` in
      `install_lenovo_drivers.ps1`.
- [ ] Keep every public parameter, interactive choice, exit code, cache rule,
      and installer fallback intact unless the task changes that contract.
- [ ] Run the PowerShell parser validation from
      [Quality Guidelines](./quality-guidelines.md).

---

## Quality Check

- [ ] No template placeholders remain.
- [ ] Claims reference real files in this repository.
- [ ] New helpers are backed by repeated code or a clear local pattern.
- [ ] No new external dependencies were added.
- [ ] `git diff --check` is clean.
