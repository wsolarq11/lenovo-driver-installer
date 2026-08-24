# Polish Lenovo Driver Installer - Design

## Scope

This is a behavior-preserving refactor of the existing single-file PowerShell
installer plus a documentation refresh. The current file is 1,416 lines and
mixes configuration, API calls, local inventory, comparison logic, console UI,
download handling, installer handling, and the main orchestration flow.

The delivery remains one PowerShell file and one `.bat` wrapper. Splitting into
modules would change deployment shape and add import/path risk without
delivering user value for this small tool.

## Data Flow

```text
CLI/.bat
  -> resolve machine + OS
  -> Lenovo category API
  -> driver list API (current OS, optional cross-OS)
  -> local device/app inventory
  -> applicability + version comparison
  -> plan file + console table
  -> interactive selection
  -> download cache (size + SHA-256 companion)
  -> installer dispatch (msi/inf/zip/cab/exe)
  -> post-install recheck
  -> summary + exit code
```

The refactor must preserve every arrow in this flow.

## File Layout

### `install_lenovo_drivers.ps1`

Keep one file, but make its shape explicit with regions:

1. Parameters and comment-based help
2. Configuration constants
3. Logging
4. Local system inventory
5. Lenovo API
6. Driver comparison
7. Console formatting
8. Selection
9. Download helpers
10. Installer dispatch
11. Main orchestration

This is a structural improvement, not a public API change. Function names that
are already stable remain stable; new helpers use PowerShell-approved verbs.

### `install_lenovo_drivers.bat`

Keep unchanged except for any necessary wording consistency. It is already a
thin elevated entry point.

### `README.md`

Rewrite to be the operational front door:

- What the tool does
- Quick start
- Full option table
- Interactive choices
- Safety and integrity guarantees
- How the installer handles common files and fallbacks
- Logs and plan artifacts
- Troubleshooting notes

### `lenovo_installer_improvement_plan.md`

Keep as the v4 scope/contract document. Refresh wording only if the refactor
changes how a contract is implemented; do not add unrequested features.

## Refactor Rules

- Preserve all existing parameter names and meanings.
- Preserve all exit codes: `0`, `1`, `2`, and propagated installer codes.
- Preserve current OS as the default.
- Preserve SHA-256 companion behavior including `-SkipHashCheck`.
- Preserve timeout, process-tree kill, and extracted-installer fallback order.
- Preserve `Not applicable`, `Unknown`, `Update`, `Up to date`, and
  `Local newer` status semantics.
- Extract repeated logic only when the helper is small and unambiguous:
  - file size tolerance
  - reboot-required exit codes
  - installer result logging where duplication exists
- Add comments for non-obvious Lenovo API or Windows behavior, not for every
  line.

## Explicitly Not Changing

- Lenovo API endpoints and headers.
- OS matching heuristic.
- Driver grouping by `PartId` + `DriverName`.
- Interactive prompts.
- Console table widths/colors.
- Post-install recheck behavior.
- Log file and plan file locations.
