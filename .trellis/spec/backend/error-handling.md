# Error Handling

> How failures are represented, logged, and surfaced.

---

## Overview

The installer uses `$ErrorActionPreference = 'Stop'` so unexpected failures
stop execution instead of silently continuing. Fallback paths are explicit
and narrow: for example, CIM calls fall back to WMI, and failed installers use
the documented fallback only when an extracted INF or inner installer exists.

The deterministic core does not perform fallible I/O, so it does not throw
environment errors; the side-effect shell owns API, system, file, download,
and process failures and converts them into logs, result objects, or exit
codes at the shell boundary.

---

## Error Types

- **CLI contract errors**: invalid flag combinations return `2`.
- **Runtime failures**: download or install failures are accumulated in
  `$failed` and produce exit code `1`.
- **Success with reboot required**: installer exit codes `3010` and `1641`
  are treated as success with a reboot warning.
- **Timeout**: process helpers return a result object with `TimedOut = $true`;
  the process tree is killed with `taskkill /T /F`.

---

## Error Handling Patterns

- Throw early with an actionable message for machine/API failures:
  `install_lenovo_drivers.ps1` throws when the Lenovo category or current OS
  entry cannot be resolved.
- Use `try/catch` only for a real fallback or a controlled boundary, not as a
  blanket error suppressor.
- Log failures with `Write-Log -Level ERROR` before returning or accumulating
  them.
- Keep installer and download outcomes in `$success` / `$failed` arrays and
  decide the process exit code from those arrays.
- Do not silently retry a generic EXE failure. The script asks the user whether
  to run it interactively instead.

---

## Common Mistakes

- Catching an error and continuing without logging; the failure disappears
  from both the console and `%TEMP%\lenovo_driver_install.log`.
- Changing exit code semantics: `0` success, `1` one or more failures, `2`
  invalid flag combination.
- Adding a generic silent retry for unknown installer families; this hides the
  failure and can leave the user with no actionable path.
