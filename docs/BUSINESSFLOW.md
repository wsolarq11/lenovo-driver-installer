# Business Flow

This document traces the end-to-end business flow of the Lenovo driver installer
through its layered entry points. It is the canonical record of "who does what,
in what order", so engineers can check any future change against the flow it
must preserve.

Scope: the flow described here is the end-to-end path from process start to a
verified install (or a dry-run / export exit). It covers the Go engine
`internal/`, the CLI shell `cmd/lenovo-driver`, and the WPF desktop layer
`lenovo_driver_wpf.ps1`.

## 1. High-level pipeline

```
cmd/lenovo-driver (main)  ->  internal/app.App.Run
                                   |
                                   v
        +---------------- resolveRuntime ----------------+
        |  machine -> OS -> category -> OS list -> local |
        |  device/app/software snapshot -> driver history |
        +-----------------------------------------------+
                                   |
                                   v
        +------------- CompareOSDriverView --------------+
        |  load list -> (optional cross-OS merge) -> row |
        |  ownership -> filter -> latest-select -> assess |
        |  (apply / local-version / status / source audit)|
        |  -> plan file + console table -> partition      |
        +-----------------------------------------------+
                                   |
                                   v
                 runSelection  -> export | dry-run |
                                  select | interactive
                                   |
                                   v
         InstallSelected -> download-verify ->
         install-dispatch (msi/inf/zip/cab/exe + fallbacks)
                              -> post-install verify -> CSV history
```

## 2. Flow by responsibility

### 2.1 Entry and option contract (`internal/app/app.go`)

`cmd/lenovo-driver/main.go` calls `app.New(...).Run(args)` once.

`Run`:
1. `ParseOptions` maps the PowerShell-style flags to an `Options` value.
2. `-Help` prints usage and exits `0`.
3. `Validate` rejects mutually exclusive flag combinations and exits `2`.
4. Starts a single 30-minute `context.WithTimeout` that bounds every network,
   download, and install step in this run.
5. `maybeElevated` relaunches elevated when an install flow needs admin rights.
6. `resolveRuntime` builds a read-only `ViewContext` shared by compare, selection,
   export, and install.
7. Resolves the OS list to compare (`listOsID`) from `-TargetOS` / current OS.
8. `CompareOSDriverView` builds the assessed view; `runSelection` then
   dispatches export / dry-run / select / install.

`ViewContext` deliberately keeps resolved inputs read-only; per-run mutable
scratch (the OS driver cache) lives on `App`, so messaging code cannot mutate
shared inputs.

### 2.2 Resolve phase (`internal/app/app.go`)

`resolveRuntime` gathers, in order:

- `inventory.GetMachineInfo` -> machine model and serial.
- `inventory.GetOSInfo` -> Windows caption and normalized OS name.
- `api.ResolveCategoryID` -> Lenovo machine category (falls back to `-Model`
  when the automatic model lookup fails).
- `api.ResolveOSEntry` -> the current OS entry and its full supported-OS list.
- `inventory.GetLocalDeviceSnapshot`, `GetInstalledApps`,
  `GetSoftwareSnapshot` -> local inventory (failures are soft).
- `ReadHistory` -> previous driver CSV history for source audit.

A hard failure at machine / OS / category / OS-entry resolution stops the run;
snapshot failures degrade to an empty inventory (still safe to proceed).

### 2.3 Compare phase (`internal/app/view.go`, `internal/compare`, `internal/audit`)

`CompareOSDriverView` is the decision core:

1. Load the mandatory official driver list (QuickFix backend, webpage fallback).
   `mustDriverList` hard-errors; the resulting list is treated as owned data.
2. Under `-LatestAcrossOS`, fetch every alternate OS list in parallel and merge
   their rows in deterministic OSID order; empty/failed alternates are soft
   misses (`tolerated`).
3. Deep-clone the fetched rows so assessment writes never touch the shared
   list cache or API transport rows.
4. `filterDriverRows` drops disabled / BIOS / un-installable rows.
5. `SelectLatest` picks the newest applicable driver row per code.
6. `assessSelectedDrivers` runs a single device-version pass, then per driver:
   - tests applicability against local devices,
   - resolves local version and vendor (`compare.ResolveLocalDriverVersion`),
   - computes `CompareStatus` (`Update` / `Up to date` / `Not installed` /
     `Local newer` / `Unknown` / `Not applicable`),
   - for a local version, runs the source-evidence audit
     (`audit.ResolveDriverSourceEvidence`) using setupapi logs and the alternate
     source map; `Local newer` is labeled from that audit rather than treated
     as an error.
7. `present` writes the plan file and prints the console table, then
   `partitionViewDrivers` splits the drivers into applicable / update-only.

> Data-flow note: the device-version index is fetched once for all applicable
> drivers; the alternate-source map is built lazily and reused, so the compare
> phase never re-scans the tree per driver.

### 2.4 Selection (`internal/app/app.go` `runSelection`, `interactive.go`)

After the view is built, selection dispatches on flags:
- `-GuiExportPath` -> `ExportGUIView` writes the WPF JSON payload and `exit 0`.
- `-DryRun` -> logs that nothing was downloaded/installed and `exit 0`.
- `-GuiInstallCodes` -> `selectByCodes` maps the comma-separated codes; missing
  or not-applicable codes fail fast with `exit 3`.
- otherwise -> `SelectInteractive` prompts and, on `t`, reloads another OS list
  and rebuilds the view.

The interactive flow prints the exact `y` / `a` driver set before any download,
so the choice is visible ahead of the side effects.

### 2.5 Download-and-install (`internal/app/install.go`, `internal/install`,
 `internal/download`)

`InstallSelected`:
1. Creates the download directory (CMD default `%TEMP%\LenovoDrivers`).
2. Per selected driver:
   - `downloadVerified` inspects the cached file (size / MD5 / SHA-256). If the
     cached file is usable it is reused; otherwise it is redownloaded.
   - On a `403` it refreshes the official URL once from the current/merged OS
     lists, then retries at most once (bounded 2-pass loop).
   - Under `-DownloadOnly` a history row `Downloaded` is written and the driver
     is counted as success.
   - Otherwise `install.InstallDriverFile` dispatches by file type
     (`.exe` / `.msi` / `.inf` / `.zip` / `.cab`), each with a native path and
     a bounded, well-defined timeout, plus an extracted-INF fallback where
     sensible. The `.exe` branch may additionally offer an interactive rerun
     after a silent non-zero exit.
   - Each outcome writes a CSV history record (`Installed` / `Failed` /
     `Downloaded` / `Verified`).
3. If any installs succeeded and we are not `-DownloadOnly`, runs the
   post-install verification pass (`verifyInstalled`): re-snapshots the system,
   re-reads versions for the just-installed drivers, and writes a `Verified`
   history row with actual before/after versions.
4. Returns a non-nil error when any driver failed (drives `exit 1`).

## 3. Hard guarantees the flow never breaks

- A dry run, `-GuiExportPath`, and `-DownloadOnly` never perform installs;
  side effects are confined to the install/verify phase.
- Every network/download/install step is within the single per-run timeout
  context; sub-process installs and PnPutil have their own per-type timeouts
  and kill the full process tree on a stall.
- Downloads are integrity-checked (size, official MD5 when provided, local
  SHA-256 companion); a cached file is only reused after the same checks.
- Install feedback (`3010`/`1`) is normalized through one
  `InstallSucceeded` gate so reboot-required counts as success everywhere.
- Compare phase never mutates shared/fetched rows (deep-clone before assessment).

## 4. Reading this flow against a new change

To assess whether a change preserves the flow, walk the change through:

1. Which layer (resolve / compare / select / install / WPF) does it touch?
2. Does it add a new network, download, or install side effect outside the
   bounded, time-limited, retried path?
3. Does it mutate a driver row before `CloneDrivers` took ownership, or rely on
   a snapshot outside its rerun scope?
4. Are the failure/compensation semantics still "surfaced + history-recorded +
   bounded retry" rather than "silent drop or unbounded retry"?

If any answer is unclear, this document is the intended navigator back into the
single owner file for that stage.