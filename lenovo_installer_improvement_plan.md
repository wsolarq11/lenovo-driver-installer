# Lenovo Driver Installer Improvement Plan (v5)

> Status: this is the implemented v5 scope contract for
> `install_lenovo_drivers.ps1`. Future refactors must preserve these decisions;
> feature requests should be written as a new plan, not folded into this one.

## Objective

Make the script the single usable entry point for Lenovo driver work while
keeping the factual source authoritative:

- Official Lenovo data only; QuickFix backend first, webpage API as fallback.
- Current OS is the safe default.
- Cross-OS comparison is an explicit experimental exception.
- Driver identity is auditable through `DriverCode + OSID + Version + MD5`.
- Downloads are integrity-checked with official MD5 when available plus a local
  SHA-256 cache companion.
- Installers cannot hang forever.
- Recheck is one cheap final pass instead of repeated full scans.

## User Workflow

- `y` installs update-only drivers.
- `a` installs all applicable drivers.
- `s` installs manually selected driver numbers.
- `n` cancels.
- `-DryRun` writes a plan and never downloads or installs.
- `-LatestAcrossOS` is explicit and logs that it is experimental.

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
- OS resolution tries the official webpage OS list first and falls back to the
  QuickFix backend OS list.

### C2. Official Data Source

- Preferred driver source is `https://ptstpd.lenovo.com.cn/home/driver/SearchForXbb`
  with `searchKey` and `osid`.
- Fallback driver source is the official webpage
  `/drive/drive_listnew?searchKey=...&sysid=...`.
- QuickFix rows carry `DriverCode`, `Version`, `FileName`, `FilePath`,
  `FileSize`, `Parameter`, `Bootfile`, and official `MD5`.
- Webpage rows carry `InstallCode` but no official MD5; the script handles that
  as a missing optional field.
- Keep per-OS failure isolation.
- No runspace pool and no new cache-key machinery in this iteration.

### C3. Driver History And Source Attribution

- Write `%TEMP%\lenovo_driver_history.csv` after successful downloads and
  after install attempts.
- Each record contains timestamp, `DriverCode`, `OSID`, `OSName`,
  `DriverName`, `Version`, `FileName`, `MD5`, `Source`, `Result`, and message.
- `Local newer` is not treated as an error. The script labels it as:
  - `Local newer (source Windows 11 64-bit)` when the local version matches an
    alternate official OS entry.
  - `Local newer (same current OS source)` when local history proves the
    current OS source installed it.
  - `Local newer (source unknown)` when no official source or history can be
    attributed.
- Alternate OS matching normalizes version strings and extracts component
  versions from multi-vendor packages before comparing.

### C4. Download Integrity

- Keep unique `DriverCode_FileName` output names.
- Keep `FileSize` tolerance checks.
- Validate official MD5 whenever the selected source provides one.
- Keep a local SHA-256 companion file for cache reuse.
- Missing, malformed, or mismatched companion forces a fresh download.
- Cached files are also checked against official MD5 before reuse.
- `-SkipHashCheck` bypasses SHA-256 and official MD5 validation but still
  enforces non-empty files and size checks when available.

### C5. Installer Timeout And EXE Fallback

- EXE, MSI, pnputil, and expand run through `Start-Process -PassThru` with a
  timeout and full process-tree kill on timeout.
- EXE silent install uses the current Inno-style flags plus official
  `Parameter`/`InstallCode` arguments when provided.
- On a nonzero EXE exit, do not silently retry with generic flags. Log the
  installer log path and ask the user whether to run the EXE interactively.
- Unknown family does not get an automatic silent retry.

### C6. Post-Install Recheck

- Remove per-driver full PnP and registry rescans.
- After all installs, run one verification pass with one fresh device snapshot
  and one refreshed app registry snapshot.
- Log `before -> after`, `unchanged`, `not detectable`, or `failed`.

### C7. Manual Selection

- Manual selection with no valid numbers returns an empty selection.
- The caller logs `No drivers selected` and finishes cleanly.

### C8. Version Detail

- Keep full versions, URLs, OSID, data source, MD5, and source notes in the
  plan file.
- Keep the console table compact.

## Explicitly Out Of Scope

- Runspace-based parallel API requests.
- Full deterministic test suite.
- BIOS/EC install unless `-IncludeBios` is passed.
- Third-party driver sources.
- Automatic reboot.
- Replicating every hidden QuickFix installer-specific behavior.
- Guaranteeing that `Local newer (source unknown)` always resolves; unknown is
  an honest result when neither history nor official alternate lists match.

## Verification

- `-DryRun` resolves the expected update/status summary and plan fields.
- `-CurrentOSOnly` and `-LatestAcrossOS` together fail fast.
- Help documents current-OS default, mutual exclusion, QuickFix-first data
  source, MD5 validation, and history file.
- A dry-run and a helper-level syntax check pass without code regressions.
- QuickFix and webpage fallback paths are exercised by dry runs where possible.
