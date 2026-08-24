# Backend / PowerShell Runtime Guidelines

> Best practices for the non-UI runtime code in this repository.

---

## Overview

This is a single-repo Lenovo driver installer. The runtime layer is one
PowerShell script, `install_lenovo_drivers.ps1`, with a thin `.bat` wrapper.
There is no web backend, no frontend application, and no database.

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
- [ ] Preserve the single-file PowerShell deployment shape unless a task
      explicitly changes it.
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
