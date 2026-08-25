# Lenovo Driver Installer Improvement Plan (v5)

> Status: this is the implemented v5 scope contract for
> `install_lenovo_drivers.ps1`. Future refactors must preserve these decisions;
> feature requests should be written as a new plan, not folded into this one.

## Objective

Keep the CLI as the single usable entry point for Lenovo driver work while
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
- `t` switches to another supported OS list in the interactive menu.
- `n` cancels.
- Before the input prompt, the exact `y` and `a` driver sets are printed with
  driver code, name, remote version, local version, and status.
- `-DryRun` writes a plan and never downloads or installs.
- `-TargetOS <OSID|OSName>` shows the official driver list for one supported OS.
- `-LatestAcrossOS` is explicit and logs that it is experimental.

## Implementation Scope

### C0. Deliverable Shape

- Keep the user entry point as `install_lenovo_drivers.bat` plus the shell
  `install_lenovo_drivers.ps1`.
- Keep deterministic decision logic in `lenovo_driver_core.ps1`; keep API,
  system inventory, file, console, process, and orchestration side effects in
  the shell.
- Keep the public CLI contract defined by this plan.
- Do not add PowerShell modules beyond the existing core/shell split; if the
  deployment shape changes again, update this plan first.

### C1. OS Mode And CLI

- Default to current OS only.
- `-LatestAcrossOS` is the explicit cross-OS opt-in.
- `-TargetOS <OSID|OSName>` is the explicit single-OS list switch; it resolves
  from the same Lenovo OS list and does not merge versions.
- `-CurrentOSOnly`, `-LatestAcrossOS`, and `-TargetOS` are mutually exclusive.
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
  `DriverName`, `Version`, `VerifiedVersion`, `BeforeVersion`, `FileName`,
  `MD5`, `Source`, `Result`, and message.
- Post-install verification records the real detected local version in
  `VerifiedVersion` and the pre-install version in `BeforeVersion`, so a later
  run can attribute the version that actually ended up on the machine without
  guessing.
- If a Lenovo package exits successfully but the local driver version remains
  unchanged, history keeps both versions. A later run must not treat that
  package as the source of an unchanged local version.
- `Local newer` is not treated as an error. The script labels it as:
  - `Local newer (source offline image integration)` when `setupapi.offline.log`
    proves an offline DISM/NTLite image import.
  - `Local newer (source online package installation)` when
    `setupapi.dev.log` proves an online driver package import.
  - `Local newer (source pre-existing DriverStore package)` when the active
    package exists but no import log can be attributed.
  - `Local newer (source Windows 11 64-bit)` when the local version matches an
    alternate official OS entry.
  - `Local newer (same current OS source)` when local history proves the
    current OS source installed it.
  - `Local newer (source unknown)` when no official source, history, or import
    evidence can be attributed.
- `setupapi.dev.log` parser keeps the `cmd:` from `>>> [Driver Install ...]`
  and `>>> [Device Install ...]` section headers, so Fn's `pnputil.exe` and
  NVIDIA's `RunDll32.exe` command lines appear in plan evidence rather than
  being dropped as unlabeled sections.
- Every applicable driver gets a `SourceAudit` result: install history,
  current/alternate official maps, and DriverStore import evidence parsed from
  `setupapi.*.log`. The plan file records category, summary, and evidence lines.
- Alternate OS matching normalizes version strings and extracts component
  versions from multi-vendor packages before comparing.
- Software-only packages such as Intel Connectivity Performance Suite resolve
  their local version from the installed application, not from an unrelated
  PnP device that happens to share a vendor name.
- Cross-OS driver selection compares actual parsed versions first and only
  falls back to issue date/edition as tie-breakers.

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

- Keep full versions, URLs, OSID, data source, MD5, source notes, and source
  audit evidence in the plan file.
- Keep the console table compact.

## Explicitly Out Of Scope

- Runspace-based parallel API requests.
- A full deterministic test suite for the side-effect shell; core offline tests
  are included, but installer/API integration remains covered by dry runs and
  manual verification.
- BIOS/EC install unless `-IncludeBios` is passed.
- Third-party driver sources.
- Automatic reboot.
- Replicating every hidden QuickFix installer-specific behavior.
- Guaranteeing that `Local newer (source unknown)` always resolves; unknown is
  an honest result when neither history nor official alternate lists match.

## Verification

- `-DryRun` resolves the expected update/status summary, source audit fields,
  and plan fields.
- `-CurrentOSOnly` and `-LatestAcrossOS` together fail fast.
- Help documents current-OS default, mutual exclusion, QuickFix-first data
  source, MD5 validation, and history file.
- `lenovo_driver_core.tests.ps1` passes without network, registry, PnP, file,
  console, or process access.
- A dry-run and a helper-level syntax check pass without code regressions.
- QuickFix and webpage fallback paths are exercised by dry runs where possible.
