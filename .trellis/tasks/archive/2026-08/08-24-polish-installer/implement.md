# Polish Lenovo Driver Installer - Implementation Plan

## Steps

1. Add comment-based help and configuration constants to the PowerShell script.
2. Add explicit `#region` sections and move functions into the intended
   section without changing behavior.
3. Extract safe helpers for:
   - expected size tolerance and file size validation
   - reboot-required installer exit codes
   - repeated timeout/failure result handling only where it is unambiguous
4. Polish `README.md` into an accurate operational guide.
5. Align `lenovo_installer_improvement_plan.md` wording with the delivered
   code, without adding new scope.
6. Run syntax validation:
   - `[System.Management.Automation.Language.Parser]::ParseFile(...)`
7. Run `-Help` smoke check.
8. Run a `-DryRun` smoke check if the environment permits, otherwise verify by
   parser/help checks and state the limitation.
9. Review git diff for accidental behavior changes.

## Validation Commands

```powershell
$tokens = $null; $errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
  'D:\AI\projects\lenovo-driver-installer\install_lenovo_drivers.ps1',
  [ref]$tokens,
  [ref]$errors
) | Out-Null
if ($errors) { $errors | Format-List; exit 1 } else { 'PARSE OK' }
```

```powershell
& 'D:\AI\projects\lenovo-driver-installer\install_lenovo_drivers.ps1' -Help
```

A full dry run requires Lenovo API access and a real Windows machine; it is not
a required gate for this structural refactor, but it is run when available.

## Rollback

- The original script is preserved by git; use `git diff` to review every
  change before finishing.
- If a refactor changes behavior, restore the affected block and re-check.
