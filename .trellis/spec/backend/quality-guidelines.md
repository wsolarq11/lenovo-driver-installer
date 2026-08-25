# Quality Guidelines

> Code quality standards for this repository.

---

## Overview

This repository has no package manager or automated test suite. Quality is
maintained through a small core/shell PowerShell runtime, explicit CLI
contracts, offline pure-core tests, and repeatable smoke checks.

---

## Required Patterns

- Keep deterministic logic in `lenovo_driver_core.ps1`; keep side effects in
  `install_lenovo_drivers.ps1` and keep the `.bat` wrapper thin.
- Use PowerShell approved verbs and the existing `#region` structure.
- Preserve all public parameters, interactive choices, exit codes, download
  cache rules, and installer fallbacks.
- Log through `Write-Log` instead of printing directly from the shell.
- Keep core functions free of network, registry, PnP, file, console,
  `Read-Host`, and process APIs.
- Extract small helpers when the same non-trivial logic appears more than once.
  Examples: `Test-RebootExitCode`, `Test-FileSizeMatch`, and
  `Get-SizeTolerance` in `lenovo_driver_core.ps1`.
- Keep comments for non-obvious Lenovo API or Windows behavior, not for every
  line.

---

## Forbidden Patterns

- Adding network, registry, PnP, file, console, `Read-Host`, or process access
  to `lenovo_driver_core.ps1`.
- Adding a PowerShell module or another runtime file without updating the
  deployment contract.
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

Run the same parse for `lenovo_driver_core.ps1`.

Offline core tests:

```powershell
.\lenovo_driver_core.tests.ps1
```

Static analysis when PSScriptAnalyzer is installed:

```powershell
$issues = @()
foreach ($file in @('lenovo_driver_core.ps1', 'install_lenovo_drivers.ps1')) {
  $issues += Invoke-ScriptAnalyzer -Path $file -Severity Warning, Error
}
$issues | Format-Table RuleName, Line, Message -AutoSize
```

Expected remaining warnings are limited to the intentional legacy surface:
existing public function names, interactive `Write-Host`, WMI fallback, official
MD5 hashing, intentionally silent fallback catches, the `Write-Log` name, and
the nested-scope `OsInfo` false positive in `Resolve-LenovoOsEntry`. Fix new
warnings such as automatic-variable shadowing.

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
