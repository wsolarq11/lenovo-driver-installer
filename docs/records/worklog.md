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

---

## Session: 清洁全仓、push、远端 CI 跑通

**Branch**: `main`

### Summary

本地 commit push 到 `origin/main`，远端 verify CI 21/21 VERIFY_OK。过程中消除一条误导性 `setup-go` 缓存警告。

### Main Changes

- `.github/workflows/verify.yml`：`setup-go` 显式 `cache: false`。根因：本模块纯标准库（`go.mod` 无 `require`、无 `go.sum`），而 `setup-go` 的 `cache` 默认是 `true`，会尝试恢复不存在的依赖缓存并输出误导性「go.sum not found」警告。先误删 `cache: true`（无效，因默认即 true），后改显式 `cache: false` 才消除。
- push 至 `origin/main`（`f008ae0..5c3ca14`）。

### Testing

- [实测] 远端 CI run `35869205275` success，`Verify summary: 21 steps, 0 failed` + `VERIFY_OK`，`go.sum` 缓存警告已消除。
- [待取证] 真机双击验证（WPF 启动自动识别）仍未做。

### Status

[OK] **Completed** — `5c3ca14`。残余注释：`checkout@v4`/`setup-go@v5` 的 Node 20 deprecation 为 GitHub 平台级告警，功能正常，待官方迁移。

---

## Session: 待取证项真机闭环取证

**Branch**: `main`

### Summary

把三处 `[待取证]` / `[推断]` 项在真机 82JQ 上取证：回退 INF 路径、setupapi 日志易失性、WPF Loaded 自动识别。无代码改动，仅文档写回与标注降级。

### Main Changes

- `docs/spec/behavior.md`：`FullInfPath` 待取证 → 实测（发布副本路径通过 API 检查、与 FileRepository 原件逐字节一致）；来源审计 provenance 推断 → 实测证实其上限（日志易失）。
- `docs/spec/invariants.md`：同步 provenance 推断的实测依据。
- `docs/records/evidence-82jq.md`：追加三条取证结论。

### Testing

- [实测] 基线 `scripts/verify.ps1` 21/21 VERIFY_OK。
- [实测] `UpdateDriverForPlugAndPlayDevicesW` 合成 hardwareID 探针返回 `0xe000020b`（`ERROR_NO_SUCH_DEVINST`），发布副本路径通过 INF 路径检查。
- [实测] `oem47.inf` 与 FileRepository 原件 SHA256 一致（`C3237F2C...`）。
- [实测] `setupapi.offline.log` / `setupapi.setup.log` 已轮转删除，`setupapi.dev.log` 仅 57 行。
- [实测] WPF Loaded 自动识别，MachineText 经 UIAutomation 读为 `82JQ / PF2SBWJA | Windows 10 64-bit (OSID 42)`；`-WorkerSmoke` `WORKER_SMOKE_OK rows=23`。

### Status

[OK] **Completed** — 文档写回，待 commit。

---

## Session: 联调到底 — 账本设备身份列（安装端 ↔ 审计端 join）

**Branch**: `main`

### Summary

把设备级对账缺的 join 键补齐：设备身份从自由文本升格为账本的 `devices` 列，并把链哈希从位置式定位改为按表头列名定位，堵住“加列即静默失效”的漂移。

### Main Changes

- `internal/model/model.go`：`HistoryRecord` 补 `Devices`；`DriverAssessment` 补 `MatchedDeviceIDs`。
- `internal/app/view.go`：评估期一次性捕获匹配设备身份（该遍历本已解析出结果），此后该驱动的所有账本行复用同一集合。
- `internal/plan/plan.go`：`BuildDriverHistoryRecord` 作为唯一出口把设备身份写进行。
- `internal/app/history.go`：补 `Devices` 列；`joinDevices`/`splitDevices` 单一分隔符常量；`resolveHashIndex` 按在盘表头**列名**定位链哈希（原 `row[len(historyColumns)]` 位置式索引在加列后会指向数据列，使篡改检测静默失效）；`ledgerHeaderDriftError` 让表头缺列时**拒绝写入**并给出处置指令，不追加比表头更宽的行；`ReadHistory` 对过期表头记一次 WARN。
- `internal/app/rollback.go`：回退意图行与逐设备结果行的设备身份改走结构列；`deviceScopedDriver` 把逐设备结果行收窄为单个 id；`message` 不再复述设备 id。
- `internal/app/snapshot.go`：`-Audit` 设备差分按 `devices` 列逐条归因（`[ledger: DRV1,DRV2]`）；无匹配行保持可见的未归因，不写成“正常”。
- `scripts/verify.ps1`：新增两道门禁——设备身份只许走结构列（禁止回退成自由文本）、链哈希必须按表头名定位（禁止位置式索引）。
- `docs/spec/contracts.md` §5 / `behavior.md` / `invariants.md` 第 5 条：同步账本 schema、设备身份单一出口、过期表头拒绝语义。

### Testing

- [实测] `scripts/verify.ps1` 23/23 VERIFY_OK（原 21 步 + 新增 2 步门禁）。
- [实测] `go build` / `go vet` / `gofmt -l` 干净；`go test ./...` 全包通过。
- [实测] 接缝测试 `TestLedgerRowJoinsToDeviceSnapshot`：写带设备身份的安装行 → 设备快照差分 → 断言变化设备可归因到同一 id。先反证为红灯（不捕获身份时无物可匹配），确认非空跑。
- [实测] `TestHashChainIndexSurvivesSchemaGrowth`：旧 13 列格式账本仍按其自身布局校验通过，且篡改其后仍能断链。
- [实测] `TestWriteHistoryRecordRefusesStaleSchema`：过期表头被拒绝，且未追加半行。
- [实测] 门禁红灯注入验证：注入自由文本后门禁精确报出 `rollback.go:383,391`；注入位置式索引后 `ledger hash column located by header name` FAIL。
- [待取证] 真机 82JQ 上的旧格式账本需移开归档后新格式才会建立（拒绝写入属设计行为，非缺陷）；真机端到端重跑未做。

### Status

[OK] **Completed** — 待 commit。

---

## Session: 缺口调研 + 自动安装集纳入降级驱动（真机取证后修复）

**Branch**: `main`

### Summary

真机 82JQ 上做缺口调研（交换/比较/反复/品味到底），发现规范与代码的直接冲突：不变量 3 写明 `Local newer` 不进自动安装集，而 `partitionViewDrivers` 用宽松比较把它放了进去——选 `a` 会降级显卡。已修复并加门禁锁死。

### Main Changes

- `internal/model/model.go`：新增 `InAutomaticInstallSet`，作为自动安装集成员资格的唯一权威（显式 `==` 白名单：仅 `Update` + `Not installed`）。
- `internal/app/view.go`：`partitionViewDrivers` 改为消费该谓词，不再用 `!= StatusNotApplicable`。
- `internal/app/interactive.go` / `help.go`：`a` 的文案从“all applicable”改为“install set”，并说明按 `a` 不会降级设备、`s` 是安装本机已更新驱动的唯一途径。
- `scripts/verify.ps1`：新增门禁 `automatic install set excludes downgrade and no-op statuses`（函数体出现 `!=` 即失败；`view.go` 不得宽松划分；提示语不得回退）。
- `docs/spec/invariants.md` 第 3 条：把“约束”补成可执行白名单 + 门禁强制点。

### Testing

- [实测] 修复前真机 dry-run：`Applicable candidates: 11 / update-only: 0`，9 个 `Local newer`（AMD VGA 官方 `27.20.15026.8004` vs 本机 `30.0.14052.9003`；NVIDIA `31.0.15.2799` vs `31.0.15.4630`；Realtek Lan / Intel WLAN / Fn 键等）。
- [实测] 修复后真机 dry-run：`Applicable candidates: 1`，9 个 `Local newer` 全部退出自动集。
- [实测] `scripts/verify.ps1` 25/25 VERIFY_OK（原 24 步 + 1 步新门禁）。
- [实测] 三条新测试反证为红：把谓词退回 `!= StatusNotApplicable` 后 `TestAutomaticInstallSetExcludesDowngradeAndNoop`（5≠2）、`TestLocalNewerNeverEntersAutomaticSet`（10≠1）、`TestInAutomaticInstallSetIsExplicit` 同时失败。
- [实测] 门禁红灯注入：首次正则过窄未抓住等价的宽松写法，已修正为“函数体含 `!=` 即失败”，二次注入后门禁精确报错。
- [实测] 真机确认无账本：`lenovo_driver_history.csv` 全盘不存在，此前登记的“旧格式账本需移开归档”为空操作。
- [实测] 82JQ 无 `history.csv` 以外的审计产物被测试写入（已修，见上一提交）。

### Status

[OK] **Completed** — 待 commit。

### 剩余（按优先级）

1. `Bootfile` 已采集未消费：官方 `Parameter=/add-driver *.inf /install /subdirs` + `Bootfile=//Pnputil.exe` 表达“解包后 pnputil 装 INF”，包为标准 Inno Setup。`Update: 0` 时不触发，低优先级。
2. Web 源 `Field1` 未解析（官网用 `InstallCode`+`Field1`，QuickFix 用 `Parameter`+`Bootfile`）。
3. 应用列表 native 侧重复采集 77 条（见下一条）。

---

## Session: 真机等价性 smoke + 审计日志归档

**Branch**: `main`

### Summary

补上长期挂着的真机等价性 smoke，并把被测试污染过的审计日志归档重建。smoke 顺带暴露一个此前被 PASS 掩盖的采集瑕疵。

### Testing

- [实测] `LENOVO_NATIVE_EQUIV_SMOKE=1 go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke` **PASS**（11.0s）：设备 `native=169 / ps=169 / nativeOnly=0 / psOnly=0`；机器 `82JQ`/`PF2SBWJA` 两侧一致；OS `Kind`/`OSName`/`Arch` 一致（`Caption` 因本地化不同不参与判定）；应用 `nativeOnly=0 / versionMismatch=0`；`ProvisionedAmdPower=Provisioned`、`LenovoFnServiceVersion=2.0.0.25` 一致。
- [实测] **应用列表重复采集（smoke 副产物）**：`apps native=265 ps=188 nativeOnly=0`。native 多 77 条但去重后无独有项，说明同一批应用被采集两轮（`7-Zip 26.02 (x64)`、`Microsoft Visual C++ 2010 x86 Redistributable` 等确出现两次）。等价性按去重比较故判 PASS，掩盖了该瑕疵。影响输出重复、采集耗时翻倍、"已安装 N 个"统计虚高；不影响驱动判定。待定位 native 注册表枚举路径。
- [实测] **审计日志归档**：污染日志 190 行 / 16400 字节已归档为 `lenovo_driver_install.log.polluted-20260930-005804.bak`，SHA256 `803DB4BC...D78B17`。构成分析：夹具噪声 46 行、真实操作 144 行——两类都是证据，故**归档而非删除**。归档后重跑真实 dry-run，新日志 24 行、夹具噪声 0 行、`Applicable candidates: 1`（修复后值）。

### Status

[OK] **Completed** — 文档写回，待 commit。

---

## Session: 端到端证据链闭环

**Branch**: `main`

### Summary

把散点证据接成一条可验证的端到端链。链上每个输入都是真机实录，结论与真机 `-DryRun` 逐项对齐；离线与实战一旦分歧，对应的计数就会不匹配。

### Main Changes

- 新增 `internal/inventory/evidence_fixture_dump_test.go`：`LENOVO_EVIDENCE_FIXTURE_DUMP=1` 时用**被测代码自己的采集器**导出真机夹具（169 台设备含驱动版本 + 软件快照去重）。不用第三方脚本——那等于拿外部数据验被测系统。
- 新增 `internal/app/evidence_chain_test.go`：链上 9 个环节 `ParseQuickFix → filterDriverRows → SelectLatestDrivers → TestDriverApplicable → ResolveLocalDriverVersion → CompareDriverStatus → partitionViewDrivers → WriteHistoryRecord → diffDeviceSnapshots → 按列归因`，逐状态断言真机数字。
- 新增夹具三件（`internal/app/testdata/`）：真实 QuickFix 响应脱敏（24 条，token 全部 `REDACTED`）、169 台设备（167 台带版本，实例尾替为 `\<instance>`）、182 条去重应用。
- 门禁 25 → 27 步：新增 `evidence chain runs on real fixtures`（链与 9 个环节必须在、夹具必须在且不小于 2KB）与 `recorded fixtures carry no live credentials or device serials`（token / 主板序列号 / 真实实例 ID 任一出现即失败）。

### Testing

- [实测] 链上结论与真机逐项吻合：`24 → 23 installable → 23 latest`；`Update 0 / Up to date 1 / Not installed 1 / Local newer 9 / Not applicable 12`；自动集 1 个 = `DRV201907160015`，目标 `ACPI\VPC2004\0`，账本按设备列归因。
- [实测] **建链过程抓到三处此前不可见的偏差**：(1) 漏 `filterDriverRows` → 多评估 `DRV200011235378`，实为 62 字节 `TouchPadReadme.txt`（`.txt` 不在 `installableExts`）；(2) 漏软件快照 → 3 条退回 undetermined（Fn 键 / Energy Management / AMD Power 的本机版本来自 InstalledApps 而非 PnP 属性）；(3) `filterDriverRows` 与 `SelectLatestDrivers` 顺序写反（真机 view.go:247→248），两种顺序本机结果相同故测试照样绿。
- [实测] 红灯注入 6 个接缝，5 个被精确捕获；第 6 个（时序）无区分力——`SelectLatestDrivers` 在默认路径（本机 OSID 42 单列表）是恒等变换，23 行落在 23 个 `{PartID, DriverName}` 组内。已用 `TestEvidenceChainLatestSelectionIsIdentityOnThisFixture` 把"为什么恒等"钉死，不冒充通过。
- [实测] 探测脚本一度把 `view.go` 与 `evidence_chain_test.go` 写成彼此的内容（`Copy-Item $files "$file.probe"` 数组拷向同一路径），已从 git 恢复 `view.go`（474 行校验通过）并重写测试文件。此后放弃在生产源文件上做变异探测——静态规则由门禁守，破坏被测文件换来的结论不值。
- [OK] `scripts/verify.ps1` 27/27 VERIFY_OK。

### Status

[OK] **Completed** — 文档写回，待 commit。

---

## Session: 决策由实测证据占有

**Branch**: `main`

### Summary

上一轮把证据接成了链。这一轮把要求提到“严格占有决策”：链上每一个进入决策的量都必须有 fact 级证据，证据缺失不得被当成关于机器的事实。结果推翻了自己上一轮的结论。

### Main Changes

- **根因**：`ResolveLocalDriverVersion` 对 5 类 software-versioned 驱动（`Lenovo Fn|Energy Management|X-Rite|AMD Power|Intel.*Connectivity`）只取 InstalledApps 版本，不看设备侧。联想 Energy Management 是服务而非 InstalledApps 条目 → 查不到 → `""` → `CompareDriverStatus` 首行 `if local == ""` → `Not installed` → **自动安装集唯一据以行动的状态**。实测 `ACPI\VPC2004\0` 装着联想 `oem90.inf` / `15.11.29.65` / Problem 0，官方只给 `15.11.29.13`。
- 修复：抽 `deviceVersionFrom` 单一提取点，两条路径对称化——软件版本优先，为空时回退设备实测版本。影响面 5 类驱动。
- 链断言更新到新真相，账本接缝改用 `DRV201907160015`（现 `Local newer`，经 `s` 手动路径可达）。
- 门禁 27 → 28 步：新增 `software-versioned compare falls back to the measured device version`。

### Testing

- [实测] **结论被推翻**：真机 `Applicable candidates: 1 → 0`；`Not installed 1 → 0`，`Local newer 9 → 10`；`DRV201907160015` 现为 `Local newer`（`15.11.29.65` vs `15.11.29.13`）。**这台机器没有任何驱动值得装**——上一轮“装一个”的结论是缺陷产物。
- [实测] 三个新测试：回退生效 / 软件侧有值时仍优先软件侧 / `local == ""` 必对应“设备在但无驱动”（`TestDriverApplicable` 已在上游排除无匹配设备，该保证被钉死）。
- [实测] 红灯双杀：移除回退后 Go 测试与静态门禁**同时** FAIL，错误信息为 `a failed InstalledApps lookup would again be read as evidence of absence`；还原后 28/28 VERIFY_OK。
- [实测] 写第三个测试时我断言“无证据时应报 undetermined”，实测 FAIL 后确认**是测试写错**：该路径在架构里不存在（`TestDriverApplicable` 先过滤），改写为断言架构真实提供的保证。

### Status

[OK] **Completed** — 文档写回，待 commit。

---

## Session: 证据等级与决策配对

**Branch**: `main`

### Main Changes

- `StatusEvidenceBasis` 把 `NotInstalled`/`LocalNewer` 归入 `fact`，不再返回 `inference`。旧注释写的依据“the absence of a local match”描述的是**回退修复前**的路径。
- 新增配对不变量（`internal/model/evidence_basis_test.go`）：自动集每个成员必须是 fact；`Unknown`/`NotApplicable` 必须是 undetermined 且不进自动集。两个决策分处 `InAutomaticInstallSet` 与 `StatusEvidenceBasis`，不配对会各自漂移。
- 规范：`docs/spec/invariants.md` 新增 §3.1，把该配对写成不变量。
- 门禁 28 → 29 步：`the automatic install set is backed by measurements only`。

### Testing

- [实测] 真机 `Applicable candidates: 0` 不变；GUI 导出 `Local newer,fact 10 / Not applicable,undetermined 12 / Up to date,fact 1`；**计划文件 `inference` 行与 `attention` 行均降为 0**（修复前 10 条 `Local newer` 被误报为需关注）。
- [实测] 红灯双杀：把 `StatusNotInstalled` 降回 inference 后，Go 测试与新门禁同时 FAIL，报 `automatic-set member StatusNotInstalled is not routed to fact`；还原后 29/29 VERIFY_OK。
- [实测] 一次探测无效：`LocalNewer` 不在自动集内，把它降级不会被不变量捕获——这是正确的，不变量只约束自动集成员；据此确认探测范围而非修改不变量。
- [实测] 操作失误一次：对**未提交**的 `internal/model/model.go` 执行了 `git checkout --`，把自己的修复撤销；已用可审查的 `edit` 重新应用并复验。

### Status

[OK] **Completed** — 文档写回，待 commit。

---

## Session: 补齐未闭合路径

**Branch**: `main`

### Main Changes

- **选择顺序非确定性（真机可见，最严重）**：`SelectLatestDrivers` 用 `sort.SliceStable` 仅按 `PartID` 排序，而 PartID 非全序键（82JQ：23 组只对应 15 个唯一 PartID，PartID 249 覆盖 5 个 WLAN 厂商）。同 PartID 的组落在 map 随机迭代序 → 真机连跑 3 次 `-DryRun` 导出行序不同。修复：排序键扩为 `PartID → DriverName → DriverCode`。
- **应用列表 83 条重复**：`regAppPathUser` 与 `regAppPath64` 路径文本相同，`nativeOpenKey` 硬编码 `hklm` 使 HKCU 分支重读整机列表。修复：root 改为显式参数，7 个调用点逐一声明。
- 采 OSID 248 夹具（23 条，token 脱敏），跨 edition 合并接缝闭合。
- 夹具跳过 `SWD\` 软件枚举设备（25 台，多数带每机唯一 GUID），并用测试证明排除前提成立。
- 门禁 29 → 30 步：`driver selection order is a total order`。

### Testing

- [实测] 真机 dry-run 修复后连跑 **5 次导出顺序完全一致**；红灯验证（改回 `SliceStable` + 仅 PartID）测试 FAIL 报 `OSID 42: run 0 produced a different order`。
- [实测] 真机等价性 smoke（`-tags legacyps`）PASS 11.75s：`apps native=188 ps=188 nativeOnly=0 versionMismatch=0`（修复前 `native=265`）、`devices native=169 ps=169`。
- [实测] hive 红灯：`GetInstalledApps returned 265 rows, want 188`。
- [实测] 两次探测自身有缺陷并已修正：(1) 固件探测用我自己敲的 PowerShell 正则得出「无固件包」，改用 `reFirmware` 原样才可信；(2) 首个 hive 测试直接调 `readUninstallEntries`，只证明 helper 能区分 hive、不证明 `GetInstalledApps` 传对了 hive，改成断言总行数后才真正抓到。
- [实测] 一次过度过滤：排除 `SWD\` 时丢 25 台而非预期的 1 台，据实改为「排除 + 测试证明排除前提成立 + 导出日志打印跳过数」。
- [OK] `scripts/verify.ps1` 30/30 VERIFY_OK。

### Status

[OK] **Completed** — 文档写回，待 commit。
