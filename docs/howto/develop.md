# 开发流水线

开发命令的单一权威。README 与其它文档不再重复命令，只链到这里。

## 快速参考

| 做什么 | 命令 |
| --- | --- |
| 快速内循环（build → vet → fmt → test → build bin） | `.\scripts\dev.ps1` |
| 完整离线验收门禁（自带步数计数） | `.\scripts\verify.ps1` |
| 内循环 + 完整门禁一次跑 | `.\scripts\dev.ps1 -Verify` |
| 内循环但不重建 `bin\lenovo-driver.exe` | `.\scripts\dev.ps1 -SkipBin` |
| CI 离线门禁 | push/PR 触发 `.github/workflows/verify.yml` |
| 修复 .ps1 的 BOM 漂移（幂等） | `.\scripts\fix-bom.ps1` 或 `.\scripts\verify.ps1 -FixBom` |

## 双环模型

- **内循环**（`scripts/dev.ps1`）：日常迭代。按序跑 `go build ./...` → `go vet ./...` → `go fmt -l internal cmd`（有未格式化即失败）→ `go test ./...` → `go build -o bin\lenovo-driver.exe ./cmd\lenovo-driver`（`-SkipBin` 跳过）。全过打印 `DEV_OK`，否则打印失败步骤名退出 `1`。
- **验收门禁**（`scripts/verify.ps1`）：CI 权威离线门禁。除内循环外还检查覆盖率下限、WPF 层 PowerShell 5.1 解析/BOM、CLI smoke、git-diff 卫生、未跟踪产物卫生，最后打印 `VERIFY_OK`。

`go` 工具链发现共享：`scripts/lib/go-toolchain.ps1` 提供 `Resolve-GoExe`，`dev.ps1` 与 `verify.ps1` 都用它，查找规则不漂移；两者都接受 `-GoExe <path>` 覆盖。

门禁包含这些强制项（细节在脚本里，与不变量挂钩）：

- Go build/test/vet/gofmt；`legacyps` tag 的 build/vet。
- 生产 Go 文件 ≤ 500 行。
- 覆盖率下限（`inventory` 8 / `app` 22 / `install` 45 / `compare` 50 / `audit` 50 / `download` 35 / `plan` 65 / `api` 55）。
- 含非 ASCII 的 `.ps1` 必须 UTF-8 BOM（Windows PowerShell 5.1 要求）；`verify.ps1` 检测即失败（CI 阻断）、`-FixBom` 显式修复，语法解析覆盖全部 `.ps1`。
- WPF 参数引用共享 golden（`internal/app/testdata/windows_argument_quoting.json`）。
- 旧 PowerShell 引擎文件已删除。
- CLI 互斥参数组合退出码 `2` 且错误不进 stdout。
- 零网络导入：`install/compare/inventory/plan/audit` 不得 import `net/http`/`net/url`。
- `git diff --check` + 未跟踪产物卫生。

## 真机 smoke（可选，不在离线门禁内）

```powershell
# 原生盘点质量（读真实 SMBIOS/SetupAPI）
$env:LENOVO_NATIVE_SMOKE=1; go test ./internal/inventory/ -run TestNativeSmoke -count=10 -v

# 原生 vs PowerShell 等价（需 legacyps 测试 oracle tag）
$env:LENOVO_NATIVE_EQUIV_SMOKE=1; go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke -v

# 原生安装 API smoke（不装真实驱动）
$env:LENOVO_NATIVE_INSTALL_SMOKE=1; go test ./internal/install/ -run TestNativeInstallSmoke -count=3 -v

# 信任边界 smoke（真实缓存联想包）
$env:LENOVO_TRUST_SMOKE=1; go test ./internal/trust/ -run TestVerifyLenovoPackageSmoke -v
```

构建引擎后 smoke WPF 表面：

```powershell
.\lenovo_driver_wpf.ps1 -SelfTest -NoElevation
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

## CI

`.github/workflows/verify.yml` 在 `windows-latest`（Go 1.27.x，`GOTOOLCHAIN=local` 锁定与 `go.mod` 一致）对每次 push/PR 跑 `scripts/verify.ps1`。CI 只跑离线门禁——依赖硬件的 native/WPF smoke 按设计本地驱动。

## 约定

- 两个脚本都离线、无副作用：绝不调联想 API、下载驱动或安装。
- 稳定哨兵 `DEV_OK` / `VERIFY_OK` + 数字失败数，供自动化断言。
- 含非 ASCII 的 `.ps1` 必须 UTF-8 带 BOM；`verify.ps1` 检测即失败（CI 阻断）、`verify.ps1 -FixBom` 或 `scripts/fix-bom.ps1` 显式修复，`.editorconfig` 让 IDE 保存自动带 BOM（纯 ASCII 脚本也会因此带 BOM，无害但会产生 diff）。
- 文档只在本文件维护开发命令，README “开发”节只是指针，避免命令漂移。
- 六条不变量是硬门禁，违反即阻断合并；语义见 `docs/spec/invariants.md`。
