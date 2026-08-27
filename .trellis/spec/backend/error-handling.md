# Error Handling

> How failures are represented, logged, and surfaced.

---

## Overview

Go functions return explicit errors instead of silently continuing. Fallback
paths are narrow and documented: for example, QuickFix falls back to the
webpage API, and a 403 CDN URL is refreshed from the current official list
once. The former PowerShell implementation has been removed after the Go
migration was verified.

---

## Error Types

- **CLI contract errors**: invalid flag combinations return `2`.
- **Runtime failures**: download or install failures are accumulated and
  produce exit code `1`.
- **HTTP status**: `download.HTTPStatusError` preserves the status code so the
  orchestration layer can make a narrow retry decision.
- **Success with reboot required**: installer exit codes `3010` and `1641` are
  treated as success with a reboot warning.
- **Timeout**: `install.ProcessResult.TimedOut` is set when a process tree is
  killed after the configured timeout.
- **GUI code mismatch**: `-GuiInstallCodes` values that are not present in the
  export return exit code `3`.

---

## Error Handling Patterns

- Fail early with an actionable message for machine/API failures.
- Use typed errors only where the caller needs the type to choose a fallback.
- Log failures with `ERROR` before returning or accumulating them.
- Keep download and installer outcomes in `success` / `failed` collections and
  decide the process exit code from those collections.
- Do not silently retry a generic EXE failure. Ask the user whether to run it
  interactively before marking the driver failed.

---

## Common Mistakes

- Returning an empty value without an error when the caller cannot distinguish
  a valid empty result from a failure.
- Changing exit code semantics: `0` success, `1` one or more failures, `2`
  invalid flag combination, `3` GUI driver code mismatch.
- Adding a generic silent retry for unknown installer families; this hides the
  failure and can leave the user with no actionable path.
