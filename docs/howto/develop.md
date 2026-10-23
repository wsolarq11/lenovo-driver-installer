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

## 双环模型

- **内循环**（`scripts/dev.ps1`）：日常迭代。按序跑 `go build ./...` → `go vet ./...` → `gofmt -l internal cmd`（有未格式化即失败）→ `go test ./...` → `go build -o bin\lenovo-driver.exe ./cmd\lenovo-driver`（`-SkipBin` 跳过）。全过打印 `DEV_OK`，否则打印失败步骤名退出 `1`。
- **验收门禁**（`scripts/verify.ps1`）：CI 权威离线门禁。除内循环外还检查覆盖率下限（含 `internal/gui`）、GUI 死文件检查、CLI smoke、git-diff 卫生、未跟踪产物卫生，最后打印 `VERIFY_OK`。

`go` 工具链发现共享：`scripts/lib/go-toolchain.ps1` 提供 `Resolve-GoExe`，`dev.ps1` 与 `verify.ps1` 都用它，查找规则不漂移；两者都接受 `-GoExe <path>` 覆盖。

门禁包含这些强制项（细节在脚本里，与不变量挂钩）：

- Go build/test/vet/gofmt；`legacyps` tag 的 build/vet。
- 生产 Go 文件 ≤ 400 行（上限值只在 `scripts/verify.ps1` 声明一次）。
- 覆盖率下限（`inventory` 8 / `app` 22 / `gui` 35 / `install` 45 / `compare` 50 / `audit` 50 / `download` 35 / `plan` 65 / `api` 55 / `trust` 40 / `gate` 70）。`gui` 的视图测试经 mygo 的 `ui.NewTester` 无窗口驱动真实视图函数（表格投影、确认对话框、动作分发），下限不依赖桌面会话。`trust` 是签名边界（判定下载包是否为 Lenovo 签名且未被吊销），其联网 smoke 按设计 opt-in，故离线下限是它唯一的防退化闸门（该钉的版本上实测 45.5%，删掉白名单测试后掉到 25.5%，下限落在这个缝里）。**下限只在钉死的编译器上有意义**：数字读自 `go test -cover`，插桩随补丁版本变，故门禁有专步断言运行的工具链等于 `go.mod` 所钉的那个版本；否则同一份代码在两台机器上会给出不同的「通过」。
- 旧 PowerShell 引擎文件与 WPF 表现层（`wpf/`、`lenovo_driver_wpf.ps1`、`install_lenovo_drivers_wpf.bat`）已删除，门禁有专步保证它们不再出现。
- CLI 互斥参数组合退出码 `2` 且错误不进 stdout。
- 零网络导入：`install/compare/inventory/plan/audit` 不得 import `net/http`/`net/url`；`architecture.md` §2.1 声明的联网包集合必须等于实际 import 网络库的包集合。
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

构建引擎后 smoke GUI 表面（连真实 API，会请求 UAC 提权）：

```powershell
.\install_lenovo_drivers_gui.bat
```

## CI

`.github/workflows/verify.yml` 在 `windows-latest` 跑 `scripts/verify.ps1`，**工具链版本从 `go.mod` 读**（`setup-go` 的 `go-version-file: go.mod` 取 `toolchain` 指令）。为什么必须是钉死的精确补丁版本而不是 `1.27.x` 一类的浮动范围：门禁的覆盖率下限读自 `go test -cover`，而 Go 的语句插桩**在补丁版本之间会变**——同一份源码在 `go1.27.0` 上把 `internal/trust` 量成 45.8%（33/72 条语句），在 `go1.27.2` 上量成 34.5%（19/55 条），一字未改。于是同一个下限在一台机器上过、在另一台上不过。门禁有专步 `the running Go toolchain is the one the module pins` 钉住这条：跑在别的编译器上直接 FAIL 并点名，而不是让「在下限附近漂」悄悄改变结论。注意 `toolchain` 指令语义是**下限**（本地更新的工具链仍会胜出），所以精确钉靠的是这一步的断言，不是指令本身。注意 `shell:` 关键字不接受任何上下文表达式：写成 `shell: ${{ matrix.host }}` 会让整个工作流文件失效，GitHub 的表现为「0 个 job、耗时 0 秒、无日志、结论 failure」——看起来像基础设施故障，实际是文件没通过校验。门禁有专步钉住这条（连同 `matrix.*` 必须有 `matrix:` 块），**两个 PowerShell 宿主各一个 job**（`pwsh` 7 与 `powershell.exe` 5.1）：这个门禁从设计上就是双宿主的——`Read-GateText` 与「读取点必须声明编码」步存在的唯一理由就是 5.1 按 ANSI 码页解码 BOM-less UTF-8 而 7 默认 UTF-8，只跑单宿主会让这类结论在 CI 里无人验证。`push` 只在 `main` 上触发（PR 已覆盖其余分支，否则同一提交跑两遍）。CI 只跑离线门禁——依赖硬件的 native smoke 按设计本地驱动；GUI 的视图测试经 `ui.NewTester` 无窗口渲染，已纳入离线门禁的覆盖率下限。门禁结论与宿主无关。

## 排障

### 两个环境的同一指标对不上时：先问「尺变了」还是「活少了」

同一个数字在本机与 CI 不一致（覆盖率、步数、通过数），有两种成因，判定成本差一个数量级：

- **活少了**——某步没跑、测试被跳过、文件漏检。要读源码、查 build tag、查环境变量，贵。
- **尺变了**——测量工具本身变了（编译器插桩、依赖版本、宿主差异）。**用算术一秒判定**，不用读任何源文件。

**先做算术**。以 2026-10 那次为例（数字是**当时的**，现在已因补上测试而变）：本机 `internal/trust` 覆盖率 45.8%，CI 报 34.5%，两边都是 `go test -cover`。本地 profile 是 `33/72` 条语句 = 45.8%；而 34.5% 在 72 这个分母下**取不到**（`25/72`=34.7%、`24/72`=33.3%）。数取不到，就说明被量的语句集不是同一个——**「测试少了」这个假设当场排除**，不必去查测试跳过。

**然后换一个变量复现**，而不是读源码：

```powershell
gh run view <run-id> --log | Select-String 'go version go'   # CI 实际用的版本
$env:GOTOOLCHAIN='go1.27.2'; go test -cover ./internal/trust/   # 一条命令复现 CI 的数
```

复现成功即根因到手。那次的结论是：Go 的语句插桩在补丁版本之间会变（两个结构体字面量在 `go1.27.0` 上各记 7 条、`go1.27.2` 上各记 1 条），于是「覆盖率下限」不是阈值而是随编译器漂移的数。**先把口味问题变成可判定问题**，再动手。同一手法现在可复跑验证：`internal/trust` 在 `go1.27.0` 上量得 54.2%、在 `go1.27.2` 上量得 45.5%（同源码），两把尺仍差 8.7 个点。

**判断「能不能靠调数字解决」也用不等式**：量出腐化信号（删掉对应的测试后覆盖率掉到多少），若需要的下限是「高于腐化信号、低于完好值」，而两个环境给出的区间不相交（那次是 `38.9 < x < 34.5`），就**无解**——调数字只会让一台机器假绿、另一台假红。这是钉子而不是口味。

### 红灯用最小夹具，全量门禁只跑最后一遍

门禁 59 步一趟 2–3 分钟（双宿主翻倍）。验证单条规则时不要跑全量：规则体通常只有几行，`pwsh -NoProfile -Command` 直接构造最小输入即可确认它红且**点得出名字**。全量双宿主只在收尾跑一次。上例中三条红灯（工具链不匹配 / 缺指令 / 引用悬空）各跑一趟全量，是纯浪费。

### 引用规则的设计：先读抽取规则，再起名

`invariants.md` 里的反引号片段会被抓成「引用的门禁步骤名」，但抽取**刻意跳过含 `\ / . : %` 与短于 25 字符的片段**（避免把版本号、路径当步骤名）。所以步骤名里带 `.`（如 `...the one go.mod pins`）会让引用被**静默忽略**——规格里写了强制点，却没有步骤真在查它。**动笔前先对照抽取规则**，否则就是自己制造一条「有副本没上锁」。

### 文件手术别让三个运行时互相猜路径

Windows 的 `python` / `py` 看不见 Git Bash 的 `/tmp`（反之亦然）。用 heredoc 或 `sed -i` 改 `/tmp/...` 会以 `FileNotFoundError` 收场，且**容易在以为已经备份的情况下继续往下跑**。改仓库文件一律用仓库相对路径，或整个动作放到同一个工具里做完；备份后先 `ls -l` 确认存在再动原文件。

## 约定

- 两个脚本都离线、无副作用：绝不调联想 API、下载驱动或安装。
- 稳定哨兵 `DEV_OK` / `VERIFY_OK` + 数字失败数，供自动化断言。
- 门禁读取带非 ASCII 内容的文件统一走 `Read-GateText`（显式 UTF-8 解码），结论与 PowerShell 宿主的默认代码页无关。
- 文档只在本文件维护开发命令，README “开发”节只是指针，避免命令漂移。
- 六条不变量是硬门禁，违反即阻断合并；语义见 `docs/spec/invariants.md`。
