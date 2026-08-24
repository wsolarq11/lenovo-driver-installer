# Quality Guidelines

> Code quality standards for this repository.

---

## Overview

This repository has no package manager or automated test suite. Quality is
maintained through a small, readable single-file PowerShell script, explicit
CLI contracts, and repeatable smoke checks.

---

## Required Patterns

- Keep runtime code in `install_lenovo_drivers.ps1` and keep the `.bat` wrapper
  thin.
- Use PowerShell approved verbs and the existing `#region` structure.
- Preserve all public parameters, interactive choices, exit codes, download
  cache rules, and installer fallbacks.
- Log through `Write-Log` instead of printing directly.
- Extract small helpers when the same non-trivial logic appears more than once.
  Examples: `Test-RebootExitCode`, `Test-FileSizeMatch`, and
  `Get-SizeTolerance` in `install_lenovo_drivers.ps1`.
- Keep comments for non-obvious Lenovo API or Windows behavior, not for every
  line.

---

## Forbidden Patterns

- Splitting the runtime into modules without a task that changes the deployment
  shape.
- Adding new external tooling or dependencies.
- Silently retrying unknown installer families with generic flags.
- Swallowing exceptions without a log message.
- Changing exit code semantics or the default current-OS-only behavior.

---

## Verification

PowerShell syntax parse:

```powershell
$tokens = $null; $errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
  'install_lenovo_drivers.ps1',
  [ref]$tokens,
  [ref]$errors
) | Out-Null
if ($errors) { $errors | Format-List; exit 1 } else { 'PARSE OK' }
```

Smoke checks:

```powershell
& '.\install_lenovo_drivers.ps1' -Help
& '.\install_lenovo_drivers.ps1' -CurrentOSOnly -LatestAcrossOS  # expect EXIT=2
```

---

## Code Review Checklist

- [ ] CLI parameters and help text match `README.md`.
- [ ] New code follows the existing region layout.
- [ ] No repeated size/timeout/reboot logic was copied.
- [ ] No generic silent retry or behavior expansion was added.
- [ ] PowerShell parser validation passes.
- [ ] `git diff --check` is clean.
