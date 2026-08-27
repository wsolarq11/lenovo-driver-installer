# Quality Guidelines

> Code quality standards for this repository.

---

## Overview

This repository uses Go as the runtime language. Quality is maintained through
small packages, explicit error propagation, offline tests, `go vet`, `gofmt`,
and a repeatable offline verification script.

---

## Required Patterns

- Keep deterministic logic in `internal/compare`, `internal/audit`,
  `internal/plan`, and `internal/api`; keep side effects in `internal/app` and
  `internal/install`.
- Preserve all public parameters, interactive choices, exit codes, download
  cache rules, and installer fallbacks.
- Log through `App.Log` instead of printing directly from orchestration code.
- Keep core packages free of network, registry, PnP, file, console, and process
  APIs.
- Extract small helpers when the same non-trivial logic appears more than once.
  Examples: `download.IsHTTPStatus`, `install.TestRebootExitCode`, and
  `pathutil.Base`.
- Resolve software-only driver versions from installed application/service
  evidence; do not treat a same-vendor PnP device as proof for a software
  package.
- Lenovo packages can contain multiple vendor/INF driver sets. An installer
  exit code of 0 means the package ran, not that the package's listed version
  replaced the active local driver. Record the real before/after local version
  and never attribute an unchanged local version to that package.
- Source attribution must use only real evidence: install history,
  `setupapi.*.log` import records, current/alternate official maps, and active
  device/DriverStore properties. Keep `setupapi` parsing pure; orchestration
  only reads logs and supplies evidence. Do not guess a source OS when evidence
  is missing.
- Keep comments for non-obvious Lenovo API or Windows behavior, not for every
  line.

---

## Forbidden Patterns

- Adding network, registry, PnP, file, console, or process access to
  deterministic packages.
- Adding another runtime language entry point without updating the deployment
  contract.
- Adding new external dependencies without review.
- Silently retrying unknown installer families with generic flags.
- Swallowing errors without a log message.
- Changing exit code semantics or the default current-OS-only behavior.

---

## Verification

```powershell
.\scripts\verify.ps1
```

The script runs Go build/test/vet/gofmt, PowerShell parser checks for the WPF
layer, CLI contract checks, `git diff --check`, and untracked artifact hygiene.

WPF smoke checks after building `bin\lenovo-driver.exe`:

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

---

## Code Review Checklist

- [ ] CLI parameters and help text match `README.md`.
- [ ] New code follows the existing package responsibilities.
- [ ] No repeated size/timeout/reboot/path logic was copied.
- [ ] No generic silent retry or behavior expansion was added.
- [ ] New public functions have tests when they change behavior.
- [ ] `go vet ./...` and `gofmt` are clean.
- [ ] `git diff --check` is clean.
