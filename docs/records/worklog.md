# Work Log - 2026-08-29

## Session: Native-only Runtime and WPF Fix

**Branch**: `main`

### Summary

Completed the native-only backend migration, removed the PowerShell runtime
path, fixed the WPF UI not updating after a refresh, wrote the quality gate
evidence, and pushed the working tree.

### Main Changes

- `internal/inventory`: all inventory now uses native SetupAPI/CfgMgr32,
  registry, and SMBIOS reads; PowerShell runtime path removed.
- `internal/inventory/native_full_windows.go`: native device snapshot, machine
  and OS info, installed apps, software snapshot, file version, elevation
  check.
- `internal/inventory/smbios_windows.go`: `GetSystemFirmwareTable` SMBIOS
  parsing, including Type 1 serial.
- `internal/install/native_windows.go`: `DiInstallDriverW` native INF install
  with system-native `pnputil` fallback.
- `internal/app/native_elevate_windows.go`: native `ShellExecuteExW` UAC
  elevation, replacing the PowerShell elevation path.
- `wpf/window.xaml`: added UTF-8 BOM so Windows PowerShell 5.1 parses the
  Chinese XAML correctly.
- `wpf/worker.ps1`: wait for the child process to exit and read its exit code
  after `HasExited`; PowerShell 5.1 can leave `ExitCode` null after
  `Start-Process` with redirected output, which froze the UI on
  "正在识别机器...".
- `docs/BACKEND-BENCHMARK-AND-NATIVE-MIGRATION.md`: benchmark and native
  migration report.
- `README.md`, `docs/TECHNICAL.md`: native-only architecture and smoke commands.

### Testing

- [OK] `scripts/verify.ps1` 14/14 PASS.
- [OK] `LENOVO_NATIVE_SMOKE=1 go test ./internal/inventory/ -run TestNativeSmoke` PASS.
- [OK] `LENOVO_NATIVE_EQUIV_SMOKE=1 go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke` PASS.
- [OK] `LENOVO_NATIVE_INSTALL_SMOKE=1 go test ./internal/install/ -run TestNativeInstallSmoke` PASS.
- [OK] WPF `-SelfTest` and `-WorkerSmoke` PASS.

### Status

[OK] **Completed** and pushed to `origin/main`.

---

## Session: Thermo-Nuclear Code Quality Review Delivery

**Branch**: `main`

### Summary

Reviewed the Trellis 0.6.16 working-tree changes, delivered a thermo-nuclear
maintainability review report, cleaned generated artifacts, restored
accidentally touched Trellis files to their template hashes, and made
Trellis-managed directories local-only in Git.

### Main Changes

- Added `docs/thermo-nuclear-code-quality-review.md`.
- Restored accidentally touched `.trellis` scripts to the Trellis 0.6.16
  template hashes.
- Removed Python bytecode caches generated during review.
- Added `/.trellis/`, `/.agents/`, and `/.dsh/` to `.gitignore` and removed
  those Trellis-managed directories from Git tracking while keeping local
  files intact.

### Git Commits

| Hash | Message |
|------|---------|
| `0f33c8d` | docs: add thermo-nuclear code quality review |
| `e14f68a` | chore: keep Trellis-managed files local-only |

### Testing

- [OK] Verified restored `.trellis` files against `.trellis/.template-hashes.json`.
- [OK] AST parsed modified Python files before rollback.
- [OK] `git status` clean after commits.

### Status

[OK] **Completed**

### Next Steps

- Forward the review findings to the Trellis maintainers.
- Keep Trellis-managed files local-only.

---

## Session: Thermo-Nuclear Follow-up (fresh review + #4/#5/#6/#7)

**Branch**: `main`

### Summary

Ran an independent full re-review of the `cmd/` + `internal/` Go engine (not relying on the
first round's conclusions), then implemented all outstanding follow-ups #4 (parallel OS fetch),
#5 (source-audit rule table), #6 (assessment ownership separation) and #7 (delete the fake
process-output capture). No follow-up remains open.

### Main Changes

- `internal/app/view.go`, `internal/app/install.go`, `internal/app/app.go`:
  parallel cross-OS fetch via `loadDriverListsForOSIDs` + mutex-guarded `osDriverCache`.
- `internal/app/view.go`: `cloneDriver`/`cloneDrivers` — the comparison pass owns its rows, so
  assessment never mutates the shared driver-list cache / API transport DTOs (#6).
- `internal/audit/audit.go`: `sourceCategoryRule`/`sourceEvidenceRules` declarative priority table.
- `internal/install/install.go`: removed `ProcessResult.Stdout/Stderr`, `captureOutput`,
  and the output buffer; `runProcess` discards sub-process output.
- Added unit tests (audit priority, parallel order, tolerance policy, clone ownership);
  updated install tests.
- `docs/thermo-nuclear-code-quality-review-final.md`: refreshed with the second-round findings
  and the #4/#5/#6/#7 implementations.

### Testing

- [OK] `go build ./...`, `go vet ./...`, `gofmt -l` clean.
- [OK] `go test ./...` all packages pass, including new tests.
- [OK] `scripts/verify.ps1` — 14 steps, 0 failed, `VERIFY_OK`.

### Status

[OK] **Completed** (#4/#5/#6/#7). All prior follow-ups closed.

---

## Session: Thermo-Nuclear Code Quality Review — Round 7 (native hardening)

**Branch**: `main`

### Summary

Ran an independent, code-only, whole-repo Round 7 thermo-nuclear review (no
baseline, no reliance on prior report or code comments), then landed the
behavior-preserving fixes it surfaced and completed the native-install /
native-inventory hardening. All work verified green and the working tree is
now clean.

### Main Changes

- `internal/app/view.go`: dropped from 501 → 423 lines by extracting the
  WPF-compatible JSON export concern into a new `internal/app/export.go`
  (`ExportGUIView` / `guiExportPayload` / `guiDriverRow`), removing the now
  unused `encoding/json` dependency. All Go files are back under the 500-line
  ceiling.
- `internal/inventory/native_windows.go`: extracted `evidenceRows()` shared
  "enumerate + load driver evidence + index" prefix, so `GetDeviceEvidence`
  and `GetDeviceDriverVersions` no longer each enumerate and re-index.
- `internal/inventory/native_full_windows.go`: `expandWindowsEnv` changed from
  an unbounded `for {}` to an explicitly bounded loop (64 passes + consuming
  invariant); extracted the Windows file-version reader into
  `internal/inventory/fileversion_windows.go`; made driver-evidence column
  values `RFC3339/UTC` for the setupapi import timestamps.
- `internal/install`: normalised the reboot-required install result across the
  native `DiInstallDriverW` path and the `pnputil` fallback to a single shared
  code (`errorSuccessRebootRequired = 3010`); dropped the now-unused
  `NativeInstallEnabled`/`NativeInstallAvailable` indirection.
- `internal/app/native_elevate_windows.go`: hoisted the Win32 DLL/proc handles
  to package-level vars (loaded lazily) instead of re-binding them on every
  relaunch.
- `internal/app/evidence.go`: normalized the DriverStore package-creation
  timestamp to `RFC3339/UTC`.
- `internal/inventory/windows_legacy_ps.go`: pinned the legacy PowerShell
  oracle to the stable `C:\Windows\System32\WindowsPowerShell\...\powershell.exe`
  path.
- `docs/thermo-nuclear-code-quality-review.md`: appended Round 7 (only the
  10th section) with findings `R7-1..R7-5`, verified file sizes, and the
  run verification matrix.

### Recorded (non-blocking round-7 findings)

- `R7-3`: `HistoryRecord` CSV column order is authored in three places
  (header slice, write literal, read field list) — kept, to preserve the CSV
  line format.
- `R7-5`: the Windows command-line quoting algorithm exists in both Go
  (`quoteWindowsArgument`) and WPF (`ConvertTo-CommandLineArgument`) — cannot
  be reused across layers, both tested, noted as a drift risk.

### Testing

- [OK] `go build ./...`, `go vet ./...`, `go test ./...` all packages pass.
- [OK] `gofmt -l` clean.
- [OK] `go build`/`go vet -tags legacyps ./...` OK.
- [OK] `scripts/verify.ps1` — 14 steps, 0 failed, `VERIFY_OK`.

### Status

[OK] **Completed** and pushed to `origin/main`.

---

## Session: Harden trust boundary — signer, download origin, interface drift

**Branch**: `main`

### Summary

Closed three silent-drift gaps surfaced by the fact-standard review: the
Authenticode check now enforces a Lenovo signer plus revocation checking,
downloads refuse non-Lenovo origins, and the API layer reports interface drift
instead of silently degrading to "no drivers".

### Main Changes

- `internal/trust`: `VerifyFileSignature` runs WinVerifyTrust with
  `WTD_REVOKE_WHOLECHAIN` and requires the signing certificate subject to name
  Lenovo (organization-level match via `CertEnumCertificatesInStore` +
  `CertGetNameStringW`). Added cross-platform `isLenovoSubject` and a
  Windows-only `LENOVO_TRUST_SMOKE=1` smoke over the real cached Lenovo package.
- `internal/download`: added `TrustedHost` allowlist (`lenovo.com` /
  `lenovo.com.cn` suffixes); `downloadOnce` refuses any other host before
  requesting a byte.
- `internal/api`: added `ContractDriftError` (non-empty list but zero parsed
  rows) and threaded `SourceDrivers.DriftWarning` so a preferred source's drift
  is logged when the caller falls back.
- `internal/app`: log the drift warning in `cachedDriverObjects`.

### Testing

- [OK] `scripts/verify.ps1` 17/17 VERIFY_OK.
- [OK] `LENOVO_TRUST_SMOKE=1 go test ./internal/trust/ -run TestVerifyLenovoPackageSmoke` PASS on the real cached Lenovo EXE (`DRV202009030023_FN-01LF02AFAR2W6JB0.exe`).
- [OK] `go build/vet/test/gofmt` clean; new unit tests for `isLenovoSubject`, `TrustedHost`, `ContractDriftError`, and drift-on-fallback.

### Status

[OK] **Completed**

---

## Session: Give the hardening a fallback path — revocation degrade, gate detection, live CDN proof

**Branch**: `main`

### Summary

Closed the "tightening without a fallback" gap from the prior session: the
revocation check now degrades to chain + signer verification instead of
rejecting on offline CRL, the API layer now reports interface gating (auth /
rate-limit / blocking) distinctly from field drift, and a live query proved the
real CDN host is inside the download whitelist.

### Main Changes

- `internal/trust`: split `WinVerifyTrust` into an injectable `winVerifyTrustFn`
  seam and added two-stage verification — `CRYPT_E_REVOCATION_OFFLINE` degrades
  to chain-only plus the Lenovo signer check (still rejecting third-party
  signers) and fires `RevocationFallback`; non-revocation failures still
  hard-fail. `internal/app` records the degradation as a WARN.
- `internal/api`: added `InterfaceGateError` for HTTP non-2xx, non-JSON bodies,
  and "expected fields all empty" responses (e.g. `{"code":401}`) so a gated or
  blocked private interface surfaces as such instead of degrading to "no
  drivers".
- `internal/trust` smoke: added a reproducible interception-face test
  (`TestVerifyRejectsNonLenovoSignerSmoke`) that rejects a valid non-Lenovo
  signature (verified live against `node.exe`).

### Evidence

- [实测] Live query of 82JQ returned 23 drivers, every `FilePath` on host
  `newdriverdl.lenovo.com.cn` — inside the `lenovo.com.cn` whitelist, so the
  download whitelist does not false-reject the official CDN.
- [实测] The cached Lenovo package used by the old smoke no longer exists in
  %TEMP%, which is exactly the reproducibility gap the interception-face smoke
  now closes.

### Testing

- [OK] `scripts/verify.ps1` 17/17 VERIFY_OK.
- [OK] `go build/vet/test/gofmt` clean; new tests for revocation fallback,
  interface gating, and the interception-face smoke.
- [OK] `LENOVO_TRUST_SMOKE=1 LENOVO_TRUST_SMOKE_FILE=...node.exe go test
  ./internal/trust/ -run TestVerifyRejectsNonLenovoSignerSmoke` PASS.

### Status

[OK] **Completed**

---

## Session: Escape hatch for interface death + end-to-end dry-run proof

**Branch**: `main`

### Summary

Added the missing survival layer: when both Lenovo endpoints die or are gated,
the tool now falls back to a persisted last-good driver list (clearly marked
stale) instead of failing outright. Ran the first real end-to-end dry-run on
the 82JQ machine and proved the full pipeline works.

### Main Changes

- `internal/app/drivercache.go`: last-good driver list escape hatch —
  `saveDriverListCache` / `loadDriverListCache` persist the previous successful
  `SourceDrivers` with a UTC timestamp under the stable audit artifact dir
  (`driver_list_<osid>.json`); a stale load carries a `DriftWarning`.
- `internal/app/view.go`: `cachedDriverObjects` now writes the cache on success
  and reads it (with a WARN) when `GetDriverObjects` fails on both sources.
- `internal/app/app.go`: `driverListCacheDir` field so tests isolate cache
  writes to `t.TempDir()` instead of polluting the real directory.
- `internal/api/client.go`: `InterfaceGateError` now also sets the fallback
  `DriftWarning`, closing the gap where a gated preferred source was silent when
  the backup source succeeded.

### Evidence

- [实测] End-to-end `-DryRun` on 82JQ (serial PF2SBWJA, category 3124166, OSID
  42) completed: 23 drivers, 9 Local newer / 1 Not installed / 1 Up to date /
  12 Not applicable, fact=1 inference=10 undetermined=12. No files downloaded
  or installed.
- [实测] Escape-hatch cache written at
  `...\Lenovo\DriverInstaller\driver_list_42.json` (29 KB) during the dry-run,
  and `driver_list_248.json` from the cross-OS comparison.
- [实测] A second real CDN host `driverdl.lenovo.com.cn` observed alongside
  `newdriverdl.lenovo.com.cn`; both are `lenovo.com.cn` subdomains inside the
  whitelist.

### Testing

- [OK] `scripts/verify.ps1` 17/17 VERIFY_OK.
- [OK] `go build/vet/test/gofmt` clean; new tests for cache round-trip / miss /
  OSID mismatch / empty, and gate-on-fallback drift warning.

### Status

[OK] **Completed**

---

## Session: Download chain proven end-to-end + first commit of the hardening work

**Branch**: `main`

### Summary

Ran the first real `-DownloadOnly` on 82JQ and closed the download chain
(host allowlist → download → MD5 → SHA-256 → Authenticode signer) with zero
installation side effects. Committed the four rounds of hardening work.

### Evidence

- [实测] `-DownloadOnly -GuiInstallCodes DRV201907160015` downloaded
  `ME-WWE00GAE40.exe` (1,230,984 bytes) from `newdriverdl.lenovo.com.cn`.
- [实测] Computed MD5 `67b31666572dc1657e09c44dea284734` matches the official
  plan value; SHA-256 companion matches `Get-FileHash`.
- [实测] `Get-AuthenticodeSignature` reports `Valid`, subject
  `CN=Lenovo, OU=G09, O=Lenovo...` — the signer whitelist accepted the real
  Lenovo signer through the tool's own verification path.

### Status

[OK] **Completed** — committed as `188a255` (18 files, +897/-18).

---

## Session: WPF 启动修复 + BOM 治理自动化

**Branch**: `main`

### Summary

修复 WPF 双击启动后一直停在「正在识别机器...」：正常启动路径只构建窗口、进入消息循环，从未触发一次机器识别（Export）后台任务，标题栏停留在 XAML 静态默认文案。同时把 `.ps1` 的 UTF-8 BOM 规则从「手写清单 + 人工记忆」升级为「自动判定 + 自动修复 + 门禁强制 + IDE 无感」。

### Main Changes

- `lenovo_driver_wpf.ps1`：窗口 `Loaded` 后自动执行一次 `Start-LenovoDriverJob -Export`，标题栏从「正在识别机器...」更新为「机型 / 序列号 | 当前系统 | 当前列表」。
- `wpf/worker.ps1`：引擎退出码非 0 时把标题栏设为明确的失败提示，不再停在「正在识别机器...」。
- `scripts/fix-bom.ps1`（新增）：BOM 规则单一源——字节级判定「含非 ASCII 且无 UTF-8 BOM」+ 幂等 strict-UTF-8 修复；纯 ASCII，自身不依赖该规则。
- `scripts/verify.ps1`：BOM 门禁从手写 4 文件清单改为「扫描全部 `.ps1` 自动判定 + 自动恢复 + 残留校验」；语法解析泛化到全部 `.ps1`。
- `.editorconfig`（新增）：`[*.ps1] charset = utf-8-bom`，IDE 保存自动带 BOM。
- `docs/howto/develop.md`、`.gitattributes`：登记新规则，`.editorconfig` 固定 LF。

### Testing

- [OK] `scripts/verify.ps1` 21/21 VERIFY_OK（含新门禁 `PowerShell UTF-8 BOM (auto-restore + gate)` 与泛化 `PowerShell parse`）。
- [实测] 端到端 BOM 漂移：去掉 `wpf/worker.ps1` 的 BOM → `fix-bom.ps1` 直接运行恢复 → `git diff` 无内容残留。
- [待取证] 真实桌面 GUI 闭环（Loaded 自动识别 → 标题栏更新）未在无桌面环境验证，需真机双击确认。

### Status

[OK] **Completed** — `71e06fd`（WPF 启动修复）+ `efd1bf1`（BOM 治理）。

---

## Session: BOM 门禁修正 — fail-closed 而非 auto-repair

**Branch**: `main`

### Summary

上一轮把 BOM 门禁写成了「自动修复 + 残留校验」：`Restore-Ps1Bom` 先修复再查残留，合法 UTF-8 前提下残留恒为空、永不失败。后果是 CI 会静默修复并放行无 BOM 的中文 `.ps1`，坏状态照样进 commit，门禁失去阻断能力，违反「违反即阻断合并」。修正为「检测即失败 + `-FixBom` 显式修复」，恢复 CI 阻断语义。

### Main Changes

- `scripts/verify.ps1`：新增 `-FixBom` 开关（本地显式修复，CI 不传）；BOM 门禁改为纯检测，发现无 BOM 的非 ASCII `.ps1` 即 `throw`，错误信息内嵌修复命令。
- `docs/howto/develop.md`：同步「检测即失败、`-FixBom`/`fix-bom.ps1` 显式修复」的表述。

### Testing

- [实测] 去掉 `wpf/worker.ps1` 的 BOM 后，默认 `verify.ps1` 在 BOM 门禁 FAIL（exit 1），错误信息给出修复命令；连带 parse/quoting 步骤因 5.1 误解码同样 FAIL，证明 CI 已阻断。
- [实测] `verify.ps1 -FixBom` 恢复 BOM 后 21/21 VERIFY_OK，`worker.ps1` 无 diff 残留。

### Status

[OK] **Completed** — `e937286`。
