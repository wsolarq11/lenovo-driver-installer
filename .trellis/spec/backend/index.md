# Backend / Go Runtime Guidelines

> Best practices for the non-UI runtime code in this repository.

---

## Overview

This is a single-repo Lenovo driver installer. The runtime is a Go CLI under
`cmd/` and `internal/`, a WPF presentation layer, `lenovo_driver_wpf.ps1`, and
thin `.bat` wrappers. The former PowerShell core and shell have been removed
after the Go migration was verified.

---

## Guidelines Index

| Guide | Description |
|-------|-------------|
| [Directory Structure](./directory-structure.md) | Repository layout and package organization |
| [Error Handling](./error-handling.md) | Failure propagation, exit codes, and fallbacks |
| [Logging Guidelines](./logging-guidelines.md) | Log format, levels, and artifacts |
| [Quality Guidelines](./quality-guidelines.md) | Package structure, naming, and verification |

---

## Pre-Development Checklist

- [ ] Read `README.md`, `DRIVER_FACT_STANDARD.md`, and the active Trellis task
      before changing CLI behavior.
- [ ] Keep deterministic decision logic in `internal/compare`, `internal/audit`,
      `internal/plan`, and `internal/api` where practical.
- [ ] Keep system side effects in `internal/app` and `internal/install`.
- [ ] Preserve public parameters, interactive choices, exit codes, cache rules,
      and installer fallbacks unless the task changes that contract.
- [ ] Run `scripts/verify.ps1` before declaring a Go backend change complete.

---

## Quality Check

- [ ] No template placeholders remain.
- [ ] Claims reference real files in this repository.
- [ ] New helpers are backed by tests or a clear local pattern.
- [ ] No new external dependencies were added without review.
- [ ] `git diff --check` is clean.
