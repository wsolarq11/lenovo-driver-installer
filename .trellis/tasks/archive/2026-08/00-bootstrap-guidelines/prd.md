# Bootstrap Guidelines

## Goal

Populate `.trellis/spec/` with project-specific guidance for this repository.
The repository is a single-repo Lenovo driver installer: one PowerShell runtime,
one `.bat` wrapper, and Markdown documentation. It has no web frontend, no
package workspace, and no database.

## Scope

- Spec directory: `.trellis/spec/backend/`
- Source to inspect: `install_lenovo_drivers.ps1`, `install_lenovo_drivers.bat`
- Documents to align with: `README.md`, `lenovo_installer_improvement_plan.md`
- Out of scope: changing installer runtime behavior, adding a frontend, adding
  database tooling

## Architecture Context

The runtime layer is a single PowerShell file with explicit regions:

- Configuration
- Logging
- Environment and system info
- Lenovo API
- Local device and app inventory
- Driver version comparison
- Console and plan output
- Driver selection
- Download integrity
- Process and installer helpers
- Help and interactive selection
- Main

The `.bat` file is a thin elevated entry point. There is no `src/`, no package
manifest, no frontend code, and no database.

## Files To Update

- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/directory-structure.md`
- `.trellis/spec/backend/error-handling.md`
- `.trellis/spec/backend/logging-guidelines.md`
- `.trellis/spec/backend/quality-guidelines.md`
- Remove `.trellis/spec/backend/database-guidelines.md`
- Remove `.trellis/spec/frontend/` because the repository has no frontend
  runtime

## Acceptance Criteria

- [x] Specs describe the repository as it exists now.
- [x] Backend specs reference real files and patterns.
- [x] Non-applicable database/frontend template files are removed.
- [x] Backend index matches the final spec file set.
- [x] No placeholder text remains in `.trellis/spec/`.
