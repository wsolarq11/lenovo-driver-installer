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

---

## Session: 门禁宿主无关化（UTF-8 解码收口）

**Branch**: `fix/gate-utf8-decoding`

### Main Changes

- **门禁结论按宿主翻转（已复现，最严重）**：`verify.ps1` 约 36 处 `Get-Content -Raw` 未声明编码。Windows PowerShell 5.1 用 ANSI 代码页（本机 GB2312）解码无 BOM 的 UTF-8，pwsh 7 默认 UTF-8——同一文件两个宿主读出不同文本。实测翻转：5.1 报 `37 steps, 1 failed`（`the invariants spec carries every rule the code enforces` 匹配不到中文标题），pwsh 报 37/37 VERIFY_OK。CI（`shell: pwsh`）永远看不到 5.1 侧的失败。
- **静默失败不对称（注入实验证明）**：向 `window.xaml` 注入被禁文案「安装全部可安装」后，5.1 的负向断言（`-notmatch` 中文）读作“不存在”→ 门禁**静默放过**；pwsh 正常抓 FAIL。正向断言错解码下会响亮失败，负向断言不会——所以必须加审计读取方的自检步，只修翻转的那一步不够。
- **修复**：(1) `verify.ps1` 新增 `Read-GateText`（显式 UTF-8，BOM/无 BOM 通吃），全部读取点收归它；(2) 新增第 38 步 `every project file read names its encoding (host-independent)`，扫仓库内每个 `.ps1` 的非注释 `Get-Content`，缺 `-Encoding` 即红——门禁审计自己，也审计运行时脚本；(3) 修嵌套缺陷：第 305 步未闭合导致 350–768 行整体被吞进它的 scriptblock（这解释了汇总计数 1 失败但看不到 FAIL 行），305 步补 `}`、删除尾端孤立 `}`；(4) 删死变量 `$body`（自匹配正则截断且无人使用）；(5) 不变量配对步补双向引用检查：spec 点名的门禁步必须在本文件真实存在；(6) 运行时读取修复：`wpf/ui.ps1` 的 window.xaml（BOM 在 67bf150 丢失后 5.1 下 XAML 标签乱码）、`scripts/rollback_behavior_test.ps1` 的 offer JSON 与账本共 3 处补 `-Encoding UTF8`。
- 调查确认 `Select-String` 双宿主结果一致，不在收口范围。

### Testing

- [实测] 红测一：注入被禁文案 → 5.1 下 `FAIL automatic install set excludes downgrade and no-op statuses`，恢复后工作区干净。
- [实测] 红测二：向 `wpf/ui.ps1` 注入裸 `Get-Content -Raw` → 5.1 下自检步 FAIL 并点名 `ui.ps1: $xaml = Get-Content -LiteralPath $xamlPath -Raw`，恢复后干净。
- [实测] 双宿主全量门禁：`powershell.exe` 5.1 与 `pwsh` 7 均 `38 steps, 0 failed VERIFY_OK`。修复前两宿主结论相反。
- [实测] 三个改动文件在两个宿主下 Parser 均零错误。

### Status

[OK] **Completed** — 文档写回，待 commit。

---

## Session: 全仓净熵 P1 死码与死配置

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **删 `RemoveDriverPackage` + `deleteDriverArgs`**（`internal/install/rollback.go`）：全仓零引用，且它正是被不变量 6 禁止当作回退动作的 `pnputil /delete-driver` 原语。删后该文件 import 收窄为 `fmt` + `model`，并留注释说明「包清理不属于回退」的语义、指明由门禁钉住其缺席。
- 删 `TestDeleteDriverArgs`（唯一引用 `deleteDriverArgs` 的测试）——把禁用原语固化成期望行为的测试，是该原语回流的最短路径。
- 删 `ImportRecord.HeaderKind`（只写不读）与 `sectionStartRule.kind`（4 处赋值 + 1 处写入，唯一读者是刚删掉的字段）。
- `.gitignore`：删 `install-state.json` / `install-state.json.*` / `*.sha256`（全仓零生产者，被删的 PS CLI 时代残留）；补 `/.zcode/`，与既有 `/.trellis/`、`/.agents/`、`/.dsh/` 同列。
- 未跟踪产物白名单收敛为 `^(\.github/|scripts/)`；删掉白名单里指向不存在目录的 `.trellis/tasks/08-26-go-migration/`。
- 新增第 39 步 `package cleanup is never presented as rollback`：扫 `internal/**/*.go`（含测试）断言不出现 `/delete-driver`，并断言 `rollback_native_windows.go` 仍在调 `DiRollbackDriver`——否则这条防线守的是一个已空掉的入口。

### Testing

- [实测] 红灯：把 `/delete-driver` 注回 `rollback_test.go` → `FAIL package cleanup is never presented as rollback: … rollback_test.go:60`；复原后绿。
- [实测] 双宿主全量门禁 39/39 VERIFY_OK。
- [实测] `go build ./...`、`go build -tags legacyps ./...`、`go vet ./...`、`go test ./...` 全过。

### Status

[OK] **Completed**

---

## Session: 全仓净熵 P2 重复真相上锁

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **产物路径（真实性最高的一处漂移）**：产物路径的机器源在 Go（`internal/app/app.go`、`internal/app/rollback.go`），PowerShell 侧另有 4 份字面量副本（`wpf/ui.ps1`、`wpf/actions.ps1`、`scripts/rollback_behavior_test.ps1`）。原先只有 `help.go ↔ contracts.md` 被钉，PS 副本在门禁之外——Go 改路径后「打开计划」按钮与回退 offer 查找会静默指向旧路径。做法：扩展现有步 `CLI artifact paths match docs contract`，读 `help.go` 声明的全部产物名，再扫仓库内每个 `.ps1` 里出现的 `lenovo_driver_*.{log,csv,txt,json}` 字面量，任何未被声明的名字即失败。**副本保留、被锁住**（沿用 `help.go ↔ contracts.md` 的既有写法），理由见计划「不做」第 3 条。
- `internal/app/help.go` 的 Artifacts 块补 `lenovo_driver_rollback.json`（GUI 打开它但从未被声明）。
- **新增步 `the documented JSON contracts match the engine`**：要求 `contracts.md` 恰有 2 个 ```json 块，抽取文档字段名与引擎 json tag（`export.go` + `model.OSListEntry` + `rollback.go`）后集合相等比对，并断言引擎侧字段数 ≥20（防止抽取出空集而「空集等于空集」假绿）。
- `docs/spec/contracts.md` §4 补完整 rollback offer JSON 契约块与 State 机说明（此前 offer schema 被两个 PS 消费者各读一遍，字段名无任何东西钉住）；§2 说明字段集合现由门禁比对。

### Testing

- [实测] 红灯一：改 PS 里的路径字面量 → `FAIL … PowerShell spells artifact names the engine does not declare: lenovo_driver_plan2.txt`。
- [实测] 红灯二：改文档字段名 → `FAIL … JSON contract drift (engine vs contracts.md): =>DriverCodeX`。
- [实测] 红灯三：改 offer 文件名常量 → `FAIL … help.go's Artifacts block does not declare lenovo_driver_offer.json although the GUI opens it`。
- [实测] 三次复原后双宿主 40/40 VERIFY_OK。

### Status

[OK] **Completed**

---

## Session: 全仓净熵 P3 脆弱耦合显式化

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **动态作用域消除**：`wpf/worker.ps1` 的 `Start-LenovoDriverJob` 原先直接读入口脚本作用域里的 `$Model` / `$DownloadDir`。门禁会单独 dot-source 该文件（参数引用 golden），那时这两个变量不存在——函数会静默地不带它们启动引擎。改为显式参数 `[string]$Model` / `[string]$DownloadDir`，更新 5 个调用点（`lenovo_driver_wpf.ps1:75,111`、`wpf/ui.ps1:170`、`wpf/actions.ps1:30,65`）。
- **模式兜底 throw**：模式选择原为 `$Export → Export`、`$Rollback → Rollback`、`else → Install`，即「忘记传模式」会落到唯一改变机器状态的那条动作上。改为显式三分支，无模式即 `throw`。已核实 5 个调用点全部显式传模式，故行为不变。
- **工具链解析成为命名步骤**：`Resolve-GoExe` 的 `throw` 原先发生在 dot-source 期（先于首个 `Assert-Step`），工具链缺失时门禁以裸错误终止、不计入失败数、无步名。改为首个命名步骤 `Go toolchain is discoverable`，并在其后显式打印汇总与退出码。

### Testing

- [实测] 双宿主 Parser 零错误（含改动后的 5 个文件）。
- [实测] 无模式调用在两宿主下抛出 `Start-LenovoDriverJob needs a mode: -Export, -Install or -Rollback`。
- [实测] `-GoExe C:\nonexistent\go.exe` → `FAIL Go toolchain is discoverable: Go executable not found: …` + `Verify summary: 1 steps, 1 failed`，退出码 1。
- [实测] 两个 WPF smoke 在 `powershell.exe`（5.1，即 `install_lenovo_drivers_wpf.bat` 的真实启动宿主）与 `pwsh` 7 下均通过：`WORKER_SMOKE_OK rows=23 list=Windows 11 64-bit (248)`。
- [实测] 双宿主全量门禁 41/41 VERIFY_OK。

### Status

[OK] **Completed**

---

## Session: 全仓净熵 P4 规格与文档漂移

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **C1 互斥的检查项**：`quality-guidelines.md` 的评审项要求「CLI 参数与帮助文本匹配 `README.md`」，与单源规则直接冲突（机器源是 `internal/app/help.go`，README 只是指针）。改写为指向机器源，并说明人读副本由门禁锁死、不做手工同步。
- **C2 命令清单与源映射表各只剩一份**：`quality-guidelines.md` 的 Verification 一节原先把门禁的检查内容与两个 WPF smoke 命令复述了一遍（`docs/howto/develop.md` 才是权威），改为指针。源映射表原先在 `.trellis/spec/backend/single-source.md` 与 `docs/spec/index.md` 各有一份。
  - **与计划的偏离（有证据）**：计划要求把权威映射表放在 `single-source.md`、把 `docs/spec/index.md` 改为指针。执行前核实发现 `.trellis/` **整个目录被 gitignore**（`git ls-files .trellis` 为空、`.gitignore:19` 命中），那样做会让一份已跟踪文档指向克隆里不存在的路径——指针本身就是断的。故按计划的**目标**（收敛成一份权威）反向执行：已跟踪的 `docs/spec/index.md` 承载映射表（合并 Lock 列，补入只在 single-source.md 里有的两行：Go 工具链发现、WPF/引擎参数引用），`.trellis/spec/backend/single-source.md` 只保留 Rule / 变更规程 / 反模式并指向 `docs/spec/index.md`。
- **C3 帮助块补齐**：`lenovo_driver_wpf.ps1` 的注释帮助补 `.PARAMETER Model` / `.PARAMETER DownloadDir`，语义对齐 `help.go`（型号覆盖、下载目录默认 `%TEMP%\LenovoDrivers`），并写明两者随 UAC 提权重启传递。
- **C4 输出方式统一**：`scripts/dev.ps1` 的失败汇总行由 `Write-Output` 改为 `Write-Host`，与同块及 `verify.ps1` 的 `Failed: …` 行一致。
- 附带：`docs/howto/run.md` 开头声明构建/门禁命令的权威清单在 `develop.md`，把自己定位为「刚克隆完的最短路」，把该处的重复从静默副本变为已声明的捷径。

### Testing

- [实测] 双宿主全量门禁 41/41 VERIFY_OK（`powershell.exe` 5.1 与 `pwsh` 7）。
- [实测] 改动的两个 `.ps1` 在双宿主 Parser 零错误；`git diff --check` 干净。
- [实测] `grep` 复核：门禁/内循环命令表只剩 `docs/howto/develop.md` 一份；`quality-guidelines.md` 不再把 `README.md` 当参数源。

### Status

[OK] **Completed**

---

## Session: 全仓净熵 P5a 覆盖面缺口（夹具不再写死名字）

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **被漏掉的第 4 个夹具**：门禁的凭据/序列号清洗步与 `evidence chain runs on real fixtures` 都写死了 3 个夹具名，而 `internal/app/testdata/quickfix_real_82jq_os248.json`（38 KB，被 `evidence_cross_edition_test.go` 读取）就在名单外——它是提交进仓库的真实 API 记录，内容当前干净，但没有任何护栏。这正是「写死名字」这种做法的固有缺陷：它守的是写名单那一刻存在的文件。
- **清洗步改为按目录枚举全量**：`internal/**/testdata/*` 全部文件（当前 7 个）逐个查下载 token、机器序列号、真实设备实例 id，并打印扫描数量。设备实例 id 的检查由原来只作用于 `local_devices_82jq.json` 改为**无条件**——实测 7 个夹具对该模式命中数均为 0，故无假阳性，而真实实例 id 落在哪个夹具里都是泄漏。新增「找到的夹具少于 3 个即失败」，防止枚举失效导致整步空转假绿。
- **所需夹具改为从测试源码推导**：不再列名字，而是扫 `internal/**/*_test.go` 里对 `testdata` 的引用，要求每个被引用的夹具都存在。两种写法都匹配——`"testdata/x.json"` 与 `filepath.Join("testdata", "x.json")`——只覆盖一种等于换个形状重犯同一个盲区。大小下限（≥2048 字节）只施加于名字带 `_82jq` 的记录型夹具（即录制这批证据的那台机器）：手写 golden 天然很小（993/1012/1210 字节），用机器标记把它们与记录数据区分开，规则可推导而非再写一张名单。另加一道守卫：若没有任何 `_82jq` 夹具被引用，说明标记约定变了、下限已作用于空集，直接失败。
- 实测推导结果：7 个夹具全部被覆盖（含此前逃逸的 `_os248`），大小下限恰好作用于 4 个记录型夹具。

### Testing

- [实测] 红灯（真实逃逸场景）：向第 4 个夹具 `quickfix_real_82jq_os248.json` 的 `Memo` 字段注入 `?token=…` → `FAIL recorded fixtures carry no live credentials or device serials: quickfix_real_82jq_os248.json contains an unredacted download token`；`go test` 不受影响（注入字段无语义），失败确实来自清洗步而非测试。复原后夹具字节零漂移。
- [实测] 分支可达性：把大小下限临时改为作用于全部夹具 → `FAIL evidence chain runs on real fixtures: recorded fixture quickfix_contract_golden.json is too small to be real recorded data`，证明该分支不是死码且消息正确；复原。
- [实测] 双宿主全量门禁 41/41 VERIFY_OK，清洗步打印 `7 fixtures scanned for tokens, serials and instance ids`。
- [实测] 过程中踩到一次自身误差：临时改门禁脚本时用无 BOM 写回，丢了 `verify.ps1` 的 BOM——该文件含非 ASCII，BOM 步会因此假红。改为保留 BOM 写回并只做这一次分支测试。这是「改动工具本身时要先把工具自身的不变量当约束」的一例。

### Status

[OK] **Completed**

---

## Session: 全仓净熵 P5b `.gitattributes` 行尾卫生

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- `.gitattributes` 补 `*.xaml text eol=lf`。这是唯一一类没被钉行尾的文本类型，而它恰好最可能被 Windows GUI 编辑器（XAML 设计器）以 CRLF 写回。
- **实测与审计断言不符，据实修正**：审计称 `wpf/window.xaml` 处于「index=LF、worktree=CRLF」。实测 `git ls-files --eol` 为 `i/lf w/lf attr/(none)`，两侧都是 LF，字节 3712、无 BOM。因此这次改动**不产生归一化 diff**——它是声明性的：把行尾从「取决于本机 git 配置与编辑器」变成显式约束。
- 新增第 42 步 `every text type the repo carries has an explicit line ending`：逐一断言 `*.md/*.yml/*.yaml/*.json/*.bat/*.ps1/*.go/*.xaml` 的规则行仍以 `text eol=` 存在（`*.bat` 故意是 `crlf`）。不加这步的话，本次改动就是一条没有护栏的规则——正是本轮要清的那类熵。

### Testing

- [实测] 属性生效验证（非文字断言）：把 `window.xaml` 工作区文件故意改写成 CRLF → `git ls-files --eol` 变 `i/lf w/crlf`，`git add` 后索引仍是 LF、`git diff --cached --stat` 只有 `.gitattributes` 一行——即 CRLF 写入被规范化吸收，不产生整文件 diff。随后按 LF 写回原状（3712 字节、无 BOM），`i/lf w/lf`，工作区零内容差异（porcelain/index/HEAD 三处 hash 同为 `ed4dfbbd`）。
- [实测] 红灯：删掉 `*.xaml text eol=lf` → `FAIL every text type the repo carries has an explicit line ending: .gitattributes no longer pins '*.xaml text eol=lf'`；复原后绿。
- [实测] 双宿主全量门禁 42/42 VERIFY_OK（`powershell.exe` 5.1 与 `pwsh` 7）。

### Status

[OK] **Completed**

---

## Session: 全仓净熵 P6 导出面收敛

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **纳入标准的来历（先量后改）**：判定「可收为未导出」的规则是「包外没有任何文件以 `pkg.Name` 形式拼出该标识符」。但**方法不能用这条规则判**——方法经由接收者变量调用，永远不会写成 `pkg.Name`，用名字扫描去判它必然误判且是静默的。故本阶段只处理包级标识符（函数/类型/变量/常量），方法一律不动，并把这条限制写进门禁步的注释。
- **类型要额外一条**：若某个**导出**声明（含导出方法、导出结构体字段）提到该类型，则必须保持导出——否则那个导出声明会返回调用方无法命名的类型。按此规则 27 个标识符收为未导出：`internal/app` 3（`helpText`/`parseOptions`/`selectionAnswer`）、`compare` 8、`download` 1（`trustedHost`）、`install` 4、`inventory` 6（PS 变体与原生投影）、`api` 2（`parseWeb`、`webResponse`）、`audit` 2。
- **两个名字顺手改对**：`internal/compare/matching.go` 里 `TestHardwareMatch` / `TestSoftwareVersionedDriver` 是**生产文件里的判断谓词**，不是测试（无 `*testing.T`），却带着 `Test` 前缀——下一个人一定会当成测试。改为 `hardwareMatches` / `isSoftwareVersionedDriver`。后者改名后，`internal/app` **再也无法**按驱动族决定可比性（谓词已不可导出），这条规则从文字约束变成了编译器约束。
- **新增第 43 步 `no package exports what nothing outside it names`**：把 P6 的规则本身变成门禁——遍历 `internal/` 每个包，凡是包外无人拼写、又不是被其它导出声明提到的导出标识符，一律失败并点名。规则是推导出来的，没有名单（对照 P5 的教训）。消费者包含测试：同包测试用裸名，只有**另一个包**的测试才算消费者。实测该步在收敛后归零。
- 同步更新两处因改名而失真的引用：`docs/spec/contracts.md` 的机器源格（`HelpText` → `helpText`）、`scripts/verify.ps1` 里 `installBindingLabel` 那条负向断言与定位 `matching.go` 分支的正则。
  - 据实说明：那条负向断言（不得把驱动族谓词传进 `installBindingLabel`）在谓词收为未导出后，**只能**在有人重新导出该谓词时才会触发；而第 43 步恰好禁止重新导出。两条合起来仍然守得住这条规则，但单看那条断言的强度已经变了，不该再当成主守卫。
- 未改写 `docs/records/` 里的历史提及（如 `GetNativeDeviceEvidence`、`TestSoftwareVersionedDriver` 在 review/worklog 记录中的出现）：那是 append-only 历史证据，描述的是当时的代码，改名不会让它变假。

### Testing

- [实测] 编译器与测试：`go build ./...`、`go build -tags legacyps ./...`、`go vet ./...`、`go test ./...`、`go test -tags legacyps ./...` 全绿（31 个文件、27 个标识符跨包零引用，由编译器确认）。
- [实测] 改名后重跑测量脚本：`app/compare/download/install/inventory/api/model/audit/plan/trust/pathutil` 全部 0 候选——导出面恰好等于消费者实际使用面。
- [实测] 新步红灯：向 `internal/pathutil` 加一个包外无人使用的导出函数 → `FAIL no package exports what nothing outside it names: internal/pathutil: ExportedNothingUsesThis (func)`；复原后绿。
- [实测] 新步的允许项：加一个包外无人命名、但被同包导出方法提到的类型 → PASS（证明「导出声明提到即保留」这条豁免真的生效，不是靠运气）。
- [实测] 修掉新步自身一个静默缺陷：导出声明原先以**声明名**为键收集，而 `api` 里 `ContractDriftError` 与 `InterfaceGateError` 各有一个名为 `Error` 的方法——后者覆盖前者，导致前一个类型被误判为「无人提到」而要求收掉它。改为按声明逐条保存（name/body 对）后归零。这类「键冲突导致漏判」正是本步要防的错误形式，出现在本步自身，已修并复测。
- [实测] 双宿主全量门禁 43/43 VERIFY_OK。

### 过程里修掉的三处测量误差（都是我自己引入的）

- PowerShell `-match` 默认大小写不敏感，`[A-Z]` 把未导出的名字也收了进来；必须用 `-cmatch`。
- 本环境的 `grep`（ugrep）静默忽略 `--include=*_test.go`，据此得出的「仓库没有外部测试包」是错的；改用 PowerShell 复核。
- `internal/app/testapp_test.go` 里有个名为 `app` 的**局部变量**（`app := New(...)`），于是 `app.WriteHistoryRecord(...)` 这类写法会被名字扫描误认为跨包引用。这只影响方法（方法本就不在本阶段范围），但它是「按文本推断跨包使用」的固有盲区，已记在此备用。

### Status

[OK] **Completed**

---

## Session: 门禁规则搬进 Go（能交给编译器和类型系统的全交）

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **动因**：P6 那一步（`no package exports what nothing outside it names`）是 87 行 PowerShell，靠正则逐行匹配 Go 源码。它自己有已知盲区：把 `app := New(...)` 这种**局部变量**当成包限定符；块式 `const (...)` 枚举值（`Family*`、`OrderLess`）对行首锚定的正则完全不可见；按声明名做键收集，`ContractDriftError` 与 `InterfaceGateError` 各有一个 `Error` 方法就互相覆盖。更根本的问题是：源码文本能表达的判定，编译器本来就会替我们做。
- **新建 `internal/gate`**（只用标准库）：`go/parser` + `go/ast` + `go/token` + `go/types`。本仓自己的包**从源码**做类型检查，外部依赖的导出数据由一次 `go list -export -deps -test` 取回。名字经 `types.Info.Uses` 解析到它真正指代的对象，于是「跨包引用」不再靠拼字符串猜。
- **新建 `cmd/gatecheck`**：唯一进程边界（要工具链的导出数据）。`-list` 从注册表直接回答，**不读仓库**——门禁的步骤清单来自这里，若清单在源码解析失败时消失，六条规则就会被一个无关错误一起吞掉；`-check NAME` 跑一条。
- **搬走六条**（步骤名逐字节保留，仍是各自命名、各自计数的门禁步）：`engine zero-network import`、`package cleanup is never presented as rollback`、`ledger device identity stays a column`、`ledger hash column located by header name`、`tests never write to the real audit directory`、`no package exports what nothing outside it names`。`scripts/verify.ps1` 里对应六段 PS 正则与 `Get-ExportedDecls` 助手共约 200 行删除；新增一步 `Go source checks build` 负责 `go build ./cmd/gatecheck` 并 `-list`，再按注册表逐条生成 `Assert-Step`。
- **类型检查立刻还债**：新检查找出 11 个 P6 的正则**结构性看不到**的死导出——块式 `const (...)` 里的枚举值（`Family*`、`OrderLess`/`OrderGreater`）与只经同包 `errors.As` 以 `error` 面消费的错误类型（`ContractDriftError`/`InterfaceGateError`/`HTTPStatusError`）。旧扫描把方法接收者算作「另一个声明提到了 T」，于是每个带导出方法的类型都被豁免；新检查刻意不把接收者算进去（"to call x.M() nobody spells the type's name"）。11 个 + 类型 `installerFamily` 一并收为未导出，从此跨包误用是**编译错误**。
- **保留旧步的刻意语义**，不因为「新检查更聪明」而放宽：包清理只匹配**恰好等于**参数向量的字符串字面量（旧的 `-SimpleMatch '"/delete-driver"'` 只匹配带引号的实参，不匹配「不要用 pnputil /delete-driver」这类指导散文）；账本哈希步只扫生产文件（`history_test.go` 有一处 `rows[1][len(historyColumns)]` 是断言磁盘列序的正当测试）；零网络步保留五个包的允许名单。
- **针串自匹配**：检查自己扫源码，就不能在检查文件里写出被禁的字面量。`checks.go` 里 `deleteDriverArg = "/delete-" + "driver"`、`deviceAssign = "device" + "="` 等都靠拼接构造。
- **耦合点同步**：`one comparison primitive decides every version question` 步的标识符名单改为实际的 `OrderUndecided`/`orderLess`/`OrderEqual`/`orderGreater`；`the invariants spec carries every rule the code enforces` 步原本只认「本文件里字面存在 `Assert-Step '<名字>'`」，现在同时认 `gatecheck -list` 的注册表，否则搬进 Go 的规则会静默掉出审计；`docs/spec/invariants.md` §1 顺势点名步骤并注明检查体在 `internal/gate`，§6 那句「`RemoveDriverPackage` 仅作为包清理原语保留」在 P1 删除该原语后已失真，改为描述现状 + 点名钉住它的门禁步。
- `internal/gate` 加入覆盖率地板 70%（当前 85.4%）：一条规则的红灯测试被删掉后，它会开始静默放行一切——这是本门禁里唯一不留任何痕迹的失效形式。

### Testing

- [实测] 六条搬移检查逐条端到端红灯（改动真实仓库 → 门禁 `FAIL <步骤名>` 且步骤名前就是 gatecheck 报出的 `文件:行 (原因)`）：`internal/compare` 加 `net/url` → 零网络步 FAIL；`internal/install` 加 `"/delete-driver"` 字面量 → 包清理步 FAIL；`internal/plan` 加 `"device=..."` → 设备身份步 FAIL；`internal/app` 加 `historyColumns[len(historyColumns)]` → 哈希按名步 FAIL；新增 `_test.go` 里直接调 `New(...)` → 测试构造器步 FAIL；`internal/compare` 加无人使用的导出函数 → 导出面步 FAIL。六次均复原后转绿。
- [实测] 解析容错：注入一个不可解析的 `.go`（未闭合字符串）→ 门禁仍是 **44 步**（此前会是 38 步、六条规则整批消失），六条各自 FAIL 并报出解析错误位置，没有一条静默通过。
- [实测] `go build ./...`、`go build -tags legacyps ./...`、`go vet ./...`、`go test ./...` 全绿；`go test -cover ./internal/gate/...` = 85.4%，六条检查的单测（含五条合成模块红灯）全过。
- [实测] 双宿主全量门禁 **44/44 VERIFY_OK**（`powershell.exe` 5.1 与 `pwsh` 7）。
- [实测] 门禁耗时仍在秒级：pwsh 7 约 17s，5.1 约 24s（六次 `gatecheck -check` 共约 5s）。

### Status

[OK] **Completed**（合并 PR #2 仍是你的决定；CI 只在仓库公开后触发）

---

## Session: 门禁规则搬进 Go（批 2：剩余九条 Go 源码正则步）

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **先证明缺陷，再动手**：批 1 搬走六条后，`scripts/verify.ps1` 里仍有九条步骤用正则读 Go 源码。动手前先探针，证实了两类真实缺陷，不是"看起来会更干净"：
  - **假红**：`automatic install set excludes downgrade and no-op statuses` 断言 `InAutomaticInstallSet` 函数体内不得出现 `!=`。但它是按**文本**匹配，于是"解释这条规则"的注释（`// never widen this with a != comparison`）会让门禁失败。实测：注入该注释 → FAIL。
  - **假绿**：多处用 `-notmatch 'func\s+TestX'` 断言测试仍在。这是文本匹配，**注释里提到测试名就能顶替测试本身**。实测：把 `TestProvisionedIsNotAVersion` 改名、只留一行提到它的注释 → 旧步绿，新检查 FAIL 点名。
- **新建 `internal/gate/astutil.go` + `checks_rules.go` + `checks_install.go` + `checks_helpers.go`**：九条规则改写为语法树查询。规则现在问的是"这个函数体里有没有 `!=` 运算符"、"这个包里有没有名为 X 的**声明**"、"这个调用传了哪些实参"，而不是"这一行文本像不像"。`const (...)` 块式声明对行首锚定的正则不可见，对 `declaredNames` 是普通事实。
- **九条步骤名逐字节保留**，`scripts/verify.ps1` 里对应九段正则删除（约 350 行），仍由 `Go source checks build` 一步建 `gatecheck`、按 `-list` 逐条生成命名步骤。
- **两个新检查自己先出 bug，靠红灯发现**（记录在此，因为正是这类错误最难自查）：
  - `returnsStringSliceLiteral` 原用 `qualifiedName(lit.Type)` 判 `[]string`——而 `[]string` 是 `ArrayType`，`qualifiedName` 对它返回空串，于是"禁止开关表"这条**永远为绿**。改为 `isStringSliceType` 检查 `ArrayType` + 元素名。
  - `classifyBindingLabels` 原来判"标签是否在返回 false 的臂里"，但注入 `"unchanged"` 进确认臂时它同时**也**出现在默认臂，判定落空。改为直接判"是否在确认臂内"。
  两条都是"检查看起来在跑、实际永远放行"，与批 1 找到的 11 个死导出同一失效形式。
- **不变量审计改为推导**：`the invariants spec carries every rule the code enforces` 原先硬编码三个步骤名去比对。硬编码只守住"有人记得加进去的名字"，改个步骤名就会让规格指向一个无人强制、审计也不报的标题。改为从 `invariants.md` 反引号 span 里推导门禁步骤标题（小写开头、含空格、长度 ≥25、不含路径/版本标点），再对"本文件声明 ∪ Go 注册表"求成员。实测 7 条全部推导出来；红灯：改掉注册表里一个步骤名 → FAIL 点名。
- `docs/spec/invariants.md` 五处强制点补注检查体在 `internal/gate`，并说明"按语法树判定，注释不会误触/顶替"。§4.1 补一句：直接调 `Version.Compare` 现在**编译器也会挡**（返回值类型不匹配），门禁保留以覆盖参数写反等能编译的变体。

### Testing

- [实测] 九条新检查逐条红灯（真实仓库注入 → 点名 FAIL → 复原转绿）：allow-list `!=`、确认臂混入 `unchanged`、`VersionOrder` 少一值、开关表回归、证据变量作裸位置参数、占位符被判成 `StatusUpToDate`、`SliceStable`、`StatusEvidenceBasis` 返回 `inference`、软件版本分支丢掉设备回退。
- [实测] **假红假绿双向回归**：注入"含 `!=` 的解释性注释" → 新检查 PASS（旧步 FAIL）；把被守护的测试改名、只留提到它的注释 → 新检查 FAIL 点名（旧步绿）。这两条已写成 `gate_test.go` 的单测，防止检查退回读文本。
- [实测] 编译器归属边界：把 `switch Order(a,b)` 改成 `switch localVersion.Compare(b)` → **编译不过**（`VersionOrder` 与 `int` 不匹配），门禁根本不需要出手；改成 `switch Order(b,a)`（参数写反，能编译）→ 门禁 FAIL `CompareDriverStatus no longer routes through Order`。这正是"能交给编译器的全交、剩下的才门禁"。
- [实测] `go build ./...`、`go build -tags legacyps ./...`、`go vet ./...`、`go test ./...` 全绿；`internal/gate` 覆盖率 82.0%（地板 70%）；`checks_rules.go` 一度 690 行触到本仓 500 行生产文件门禁，按域拆成 `checks_rules.go`(252) / `checks_install.go`(252) / `checks_helpers.go`(206)。
- [实测] 双宿主全量门禁 **44/44 VERIFY_OK**（`powershell.exe` 5.1 与 `pwsh` 7）。

### Status

[OK] **Completed**

---

## Session: 门禁规则搬进 Go（批 3：最后四条读 Go 源码/文档对照的步骤）

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **又先证明缺陷**。批 2 后仍剩四条步骤读 Go 源码，动手前逐条探针，三条有真实缺陷：
  - `the documented JSON contracts match the engine` 用 `json:"..."` 正则扫 `export.go`。在类型上方写一行**注释** `// shape: json:"GhostField"` → 扫描数 28→29，`GhostField` 进入"引擎字段集"，于是门禁会要求 `contracts.md` 也声明一个不存在的字段。实测确认。
  - `evidence chain runs on real fixtures` 用 `-notmatch 'diffDeviceSnapshots'` 断言链上每个环节都被调用。把真实调用改名、只在注释里留下原名 → 旧步**绿**（假绿）。实测确认。
  - `CLI exit codes match docs contract` 用 `(?ms)Exit codes:\r?\n(.*?)` 从 `help.go` 里抓块。该正则从**文件开头**找第一个匹配，所以在常量上方写一段注释形式的 `Exit codes:` 块会把解析锚点整个挪走。
- **新建 `internal/gate/checks_docs.go` + `gosource.go` + `docparse.go` + `fixtures.go` + `fileutil.go`**：四条规则改写。关键取舍写进注释：**文档侧是文本，正则是对的读法；Go 侧必须按声明读**。所以 json tag 用 `reflect.StructTag`（编码器自己的解析器，`,omitempty` 等选项由标准库处理）从 struct 字段读出，注释里的 tag 不再是字段；链的环节用"这个文件里有没有对名为 X 的调用"判定；`helpText` 按常量声明取值，注释里的锚点不再能挪动它。
- 四条步骤名逐字节保留，`verify.ps1` 对应四段正则删除（约 150 行）。`checks_docs.go` 一度 640 行触线，按域拆成 `checks_docs.go`(240) / `gosource.go`(111) / `docparse.go`(206) / `fixtures.go`(115)。
- 同步两处失真的规格引用：`contracts.md` §2 与 `docs/spec/index.md` 的机器源表原本说这些步骤在 `scripts/verify.ps1`，改为点名步骤并注明检查体在 `internal/gate`（并补一句"写在注释里的 tag 不算字段"）。

### Testing

- [实测] 四条新检查红灯（真实仓库注入 → 点名 FAIL → 复原转绿）：`help.go` 里塞一个假退出码 → `exit-code drift: engine only: 9`；改名一个产物路径 → `artifact-path drift: engine only: …blueprint.txt; docs only: …plan.txt`；链上环节改名 → `the evidence chain no longer calls diffDeviceSnapshots`。
- [实测] **两条改进方向的回归**（已写成单测）：注释里的 `json:"GhostField"` → 新检查 PASS（旧扫描会把它当字段）；改名链环节 + 留下提到原名的注释 → 新检查 FAIL 点名（旧步绿）。
- [实测] `go build ./...`、`go build -tags legacyps ./...`、`go vet ./...`、`go test ./...` 全绿；`internal/gate` 覆盖率 80.1%（地板 70%）。
- [实测] 双宿主全量门禁 **44/44 VERIFY_OK**（`powershell.exe` 5.1 与 `pwsh` 7）。

### Status

[OK] **Completed**

---

## Session: 全仓根本性整洁（死码、重复真相、规格漂移，44 → 45 步）

**Branch**: `chore/repo-entropy-sweep`

### Main Changes

- **死码与死字段**（`internal/gate` 内的整洁 + 一个模型字段）：
  - 删 `astutil.go` 的 `assignsIdent`——全仓零引用，只剩自己的定义。
  - `calleeIdent`（返回 `*ast.Ident`，给类型检查解析用）与 `calleeName`（只要名字）曾是两份各自 `switch call.Fun` 的重复实现，合并为 `calleeName` 委托 `calleeIdent`，两个函数移进 `astutil.go` 与其余 AST 查询同处。
  - `contains`（`checks.go`）与 `toSet`（`docparse.go`）语义重复，`contains` 删掉，调用点改用 `slices.Contains`。
  - `sorted` / `dedupe` / `selectorNamesAssigned` / 目录遍历的排序改用标准库 `slices.Sort` / `slices.Clone` / `slices.Compact`（go 1.27 可用），删掉 `sort` 导入。`dedupe` 现在是 `slices.Compact(sorted(v))`，从 12 行变 3 行。
  - **`model.Device.StatusFlags` 删除**。它是只写不读字段：`internal/inventory/native_windows.go` 的 `nativeDevNodeStatus` 从 `CM_Get_DevNode_Status` 填它，全仓无任何读取点，也不在任何产物/测试里。与 P1 删掉的 `HeaderKind` 同类。`nativeDevNodeStatus` 相应改为只返回 problem code，注释写明"同一个调用填出的状态位掩码被丢弃：无人消费它，留着只会诱使读者去信任一个没有规则检查的值"。
  - **反例（保留）**：`model.Driver.InstallCode` / `InstallParameter` 一度疑似死字段，核查后**不是**——`model.Driver` 经 `driverListCache` 的逃生缓存 JSON 往返，这两个字段保住原始 API 记录，必须留。
- **`declaredNames` 语义收紧**：它原先把**每个** `*ast.FuncDecl` 都记为包级名字，包括带接收者的方法。方法名不是包级名字（只能经接收者到达，从不裸写），`hasTopLevelName` 委托它后会把方法名当成"该文件声明了这个名字"。已改为跳过 `d.Recv != nil`，并在注释里写明这条区分。
- **8 处规格/文档漂移修正**（每条都独立复核过）：
  1. `docs/spec/architecture.md` §2.1 把 `api` 从"确定性包"移出，单列"联网包"（`api`、`download`）——`api` import `net/http` + `net/url`，与"不碰网络"直接矛盾。
  2. 同文件 §6 的确定性逻辑清单同步去掉 `internal/api`。
  3. `docs/howto/run.md` 的 `a` 由"安装全部适用驱动"改为"自动安装集（`Update` + `Not installed`）"，并点名 `Local newer`/`Up to date`/`Not applicable` 不在其中。
  4. 同文件 WPF 按钮文案对齐真实 XAML：`安装需更新项 (a)`，并补一句 (a) 装的不是"全部可安装项"。
  5. `docs/spec/fact-standard.md` 的"官方安装参数"一行原称本项目"使用 `InstallCode`"——生产路径无任何消费者（`.exe` 一律交互），改为"仅记录，不据以静默安装"，并补一段说明厂商 Parameter 列已被证伪。
  6. `docs/howto/develop.md` 覆盖率下限清单补 `gate` 70。
  7. 同文件内循环命令 `go fmt -l` 改为真实的 `gofmt -l`（`dev.ps1` 调的是 `gofmt.exe`）。
  8. `README.md` 维护模式条目：`RolledBack` 并非只在"问题码恢复且版本已变"时记——需重启与核验不可用两种情形也记 `RolledBack`，只是带 caveat；改为写明这条区分。
- **新增第 45 条门禁步 `the architecture doc names the network boundary the imports show`**。上面第 1 条漂移之所以能存在，是因为**没有任何东西**把"规格说哪些包联网"和"导入图显示哪些包联网"对上：代码侧有零网络导入步，文档侧没人管。新步做**集合相等**双向比对——多一个少一个都红。文档侧读 `architecture.md` 里 `**联网包**` 那一行 `（...）` 内的反引号名字（文档是文本，正则是对的读法）；代码侧从 AST 导入块收集真正 import `net/http`/`net/url` 的 `internal/*` 包（按声明读，注释不算）。

### Testing

- [实测] 新步红灯：把 `architecture.md` 的联网包名从 `api` 改成 `model` → `network-boundary drift: engine only: model; docs only: api`，两侧都点名；复原转绿。
- [实测] 新步双向单测（`gate_test.go`）：漏报一个真的联网包 → FAIL 点名 `download`；文档多授予一个不联网的包 → FAIL 点名 `model`。这正是漂移的实际形状（`api` 被列进确定性包）对应的两个方向。
- [实测] `go build ./...`、`go build -tags legacyps ./...`、`go vet ./...`、`gofmt -l`（无输出）、`go test ./...` 全绿；`internal/gate` 覆盖率 81.7%（地板 70%）。
- [实测] 全仓死码复扫：`internal/` + `cmd/` 内已无零引用函数（剩下的"零引用"全是测试入口，由测试运行器调用）。
- [实测] 双宿主全量门禁 **45/45 VERIFY_OK**（`powershell.exe` 5.1 与 `pwsh` 7）。

### Status

[OK] **Completed**
