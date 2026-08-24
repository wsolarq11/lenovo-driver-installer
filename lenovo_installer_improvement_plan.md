# Lenovo Driver Installer Improvement Plan (v4)

> Status: this is the implemented v4 scope contract for
> `install_lenovo_drivers.ps1`. Future refactors must preserve these decisions;
> feature requests should be written as a new plan, not folded into this one.

## Objective

Implement the balanced scope after four weighted cross-audits. The plan favors
Fast and Simple without removing the pragmatic safety fixes:

- Current OS is the safe default.
- Cross-OS comparison is explicit and optional.
- Downloads are integrity-checked without new external tooling.
- Installers cannot hang forever.
- Recheck is one cheap final pass instead of repeated full scans.

## User Workflow

- `y` installs update-only drivers.
- `a` installs all applicable drivers.
- `s` installs manually selected driver numbers.
- `n` cancels.

## Implementation Scope

### C0. Deliverable Shape

- Keep the installer as one `.ps1` file plus the thin `.bat` wrapper.
- Keep the public CLI contract defined by this plan.
- Organize the script into clear regions; do not introduce a module split
  unless a later task explicitly changes the deployment shape.

### C1. OS Mode And CLI

- Default to current OS only.
- `-LatestAcrossOS` is the explicit cross-OS opt-in.
- `-CurrentOSOnly` and `-LatestAcrossOS` are mutually exclusive.
- A missing current OS entry fails with a clear diagnostic.

### C2. API

- Keep the current matched-OS request.
- Keep cross-OS scanning sequential because it is opt-in and small.
- Keep per-OS failure isolation.
- No runspace pool and no new cache-key machinery in this iteration.

### C3. Download Integrity

- Keep unique `DriverCode_FileName` output names.
- Keep `FileSize` tolerance checks.
- Add a local SHA-256 companion file for cache reuse.
- Missing, malformed, or mismatched companion forces a fresh download.
- `-SkipHashCheck` bypasses the SHA-256 companion but still enforces
  non-empty files and size checks when available.

### C4. Installer Timeout And EXE Fallback

- EXE, MSI, pnputil, and expand run through `Start-Process -PassThru` with a
  timeout and full process-tree kill on timeout.
- EXE silent install uses the current Inno-style flags.
- On a nonzero EXE exit, do not silently retry with generic flags. Log the
  installer log path and ask the user whether to run the EXE interactively.
- Unknown family does not get an automatic silent retry.

### C5. Post-Install Recheck

- Remove per-driver full PnP and registry rescans.
- After all installs, run one verification pass with one fresh device snapshot
  and one refreshed app registry snapshot.
- Log `before -> after`, `unchanged`, `not detectable`, or `failed`.

### C6. Manual Selection

- Manual selection with no valid numbers returns an empty selection.
- The caller logs `No drivers selected` and finishes cleanly.

### C7. Version Detail

- Keep full versions and URLs in the plan file.
- Keep the console table compact.
- No new vendor display complexity in this iteration.

## Explicitly Out Of Scope

- Runspace-based parallel API requests.
- Official API hash parsing.
- Full deterministic test suite.
- BIOS/EC install unless `-IncludeBios` is passed.
- Third-party driver sources.
- Automatic reboot.

## Verification

- `-DryRun` still resolves the expected update/status summary.
- `-CurrentOSOnly` and `-LatestAcrossOS` together fail fast.
- Help documents current-OS default and mutual exclusion.
- A dry-run and a helper-level syntax check pass without code regressions.
