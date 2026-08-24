# Polish Lenovo Driver Installer Script and Documentation

## Objective

Restructure the Lenovo driver installer into a clear, maintainable, and
self-documenting package while preserving the current CLI contract and runtime
behavior.

## Requirements

- Keep single-file PowerShell deployment and the existing `.bat` entry point.
- Preserve every existing parameter, interactive choice, exit code, download
  cache rule, and installer fallback path.
- Improve code readability with stable section boundaries, named constants,
  shared helpers, and concise comments.
- Keep user-facing behavior deterministic: no GUI, no new external tooling, no
  silent feature expansion.
- Refresh `README.md` so it accurately explains usage, options, safety,
  architecture, and troubleshooting.
- Refresh `lenovo_installer_improvement_plan.md` only where the delivered
  structure changes how the plan should be read; do not invent new features.

## Acceptance Criteria

- [ ] `install_lenovo_drivers.ps1` passes a PowerShell syntax parse.
- [ ] `-Help` output documents every option and the current-OS default.
- [ ] The script remains a single `.ps1` file invoked by
      `install_lenovo_drivers.bat`.
- [ ] No new external dependencies are added.
- [ ] README is aligned with the delivered code.
- [ ] Code is organized into readable regions/sections with duplication removed
      where it is safe to do so.
- [ ] The existing functional contracts in the v4 improvement plan remain
      intact unless the task explicitly changes them.

## Constraints

- Do not change the Lenovo API request strategy.
- Do not change download hash/size validation semantics.
- Do not change installer timeouts or fallback behavior.
- Do not add automatic reboot or third-party driver sources.

## Out Of Scope

- GUI or web interface.
- Runspace or parallel API requests.
- Full deterministic test suite.
- New features beyond readability and documentation polish.
- Commit or push operations unless separately requested.
