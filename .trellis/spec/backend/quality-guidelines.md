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
- Resolve software-only driver versions from installed application/service
  evidence; do not treat a same-vendor PnP device as proof for a software
  package.
- Lenovo packages can contain multiple vendor/INF driver sets. An installer
  exit code of 0 means the package ran, not that the package's listed version
  replaced the active local driver. Record the real before/after local version
  and never attribute an unchanged local version to that package.
- Source attribution must use only real evidence: install history,
  `setupapi.*.log` import records, current/alternate official maps, and active
  device/DriverStore properties. Keep `setupapi` parsing as a pure core
  function; the shell only reads logs and supplies device evidence. Do not
  guess a source OS when evidence is missing.
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

Run the same parse for `lenovo_driver_core.ps1` and `lenovo_driver_wpf.ps1`.

Offline core tests:

```powershell
.\lenovo_driver_core.tests.ps1
```

WPF smoke checks:

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
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
& '.\install_lenovo_drivers.ps1' -CurrentOSOnly -TargetOS 248      # expect EXIT=2
& '.\install_lenovo_drivers.ps1' -LatestAcrossOS -TargetOS 248     # expect EXIT=2
& '.\install_lenovo_drivers.ps1' -Elevated              # interactive t toggle, then cancel
& '.\install_lenovo_drivers.ps1' -Elevated -TargetOS 248 # verify y/a preview lists, then cancel
```

---

## Code Review Checklist

- [ ] CLI parameters and help text match `README.md`.
- [ ] New code follows the existing region layout.
- [ ] No repeated size/timeout/reboot logic was copied.
- [ ] No generic silent retry or behavior expansion was added.
- [ ] PowerShell parser validation passes.
- [ ] `git diff --check` is clean.
