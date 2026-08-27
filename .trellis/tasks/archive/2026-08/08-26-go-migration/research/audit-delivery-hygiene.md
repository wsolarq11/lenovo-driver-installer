# Lenovo Driver Installer 交付卫生与可验证性审计报告

> 审计范围：`D:\AI\projects\lenovo-driver-installer`，仅做只读审计与报告。
> 审计视角：交付卫生、验证能力、可复现性、可接续性、验收可执行性。
> 本次未发起真实 Lenovo API、真实下载/安装、GUI 交互；未修改除本报告外的任何文件。
> 审计日期：2026-08-26。

---

## 摘要（交付可验收性结论）

当前仓库**不可验收**，但 PS1 基线和部分 Go CLI 行为已有可验证证据。

已确认可用的验证面：

- 4 个 PowerShell 文件语法解析全部 `PARSE OK`。
- `lenovo_driver_core.tests.ps1` 通过：`passed=86, failed=0`。
- 现有未跟踪二进制 `bin\lenovo-driver.exe -Help` 返回 `0`，三种非法参数组合均返回 `2`。
- PowerShell 三种非法参数组合也返回 `2`。
- `git diff --check HEAD` 对已跟踪文件无输出，但它不覆盖 untracked 文件。

阻断验收的问题：

- Go 工具链不在 PATH，`go version / go build / go test / go vet` 全部不可执行；当前无法证明 Go 源码可编译、可测试、可通过 vet。
- 规划要求的单命令验证入口 `scripts/verify.ps1` 不存在，也没有任何 CI workflow。
- `LatestAcrossOS` 只被解析和校验，没有真正拉取并合并其他 OS 列表。
- 403/URL 过期刷新函数 `GetRefreshedDriverURL` 已定义但没有调用者。
- WPF 仍然启动 `install_lenovo_drivers.ps1`，没有切到 Go。
- `.gitignore` 未覆盖 `bin/`、`sessionlogs/`、`nul`、Go 构建产物；当前工作区有大量 untracked WIP 和两类明显误产物。
- README、Trellis backend specs 仍描述“纯 PowerShell、无包管理、无构建产物”的状态，与 Go WIP 不一致。
- `internal/compare/compare.go` 为 695 行，超过 PRD 500 行上限；`pathBase`/`pathParent` 仍在三个包重复实现。

结论：该仓库目前更适合被认定为“Go 迁移半成品”，需要在 Phase 0/Phase 1 基础设施完成后再进入验收；`prd.md` 的验收标准本身写清楚了验证命令，但 `implement.md` 与实际实现状态差距明显。

---

## 现状清单

### Git 工作区

`git status --short --branch` 输出：

```text
## main...origin/main
?? .trellis/tasks/08-26-go-migration/
?? bin/
?? cmd/
?? go.mod
?? internal/
?? nul
?? sessionlogs/
```

`git status --short --ignored --untracked-files=all` 显示：

- untracked 源码：`cmd/`、`internal/`、`go.mod`，共 23 个 Go 文件、约 4054 行。
- untracked 任务文档：`.trellis/tasks/08-26-go-migration/{prd,design,implement,task.json}`。
- untracked 产物：`bin/lenovo-driver.exe`（9,950,720 字节，SHA-256 `49D645131EB4CBC8C2932F298ED0F8F96C6DEC17C2DA84F584FB1F0C6194B70A`）。
- untracked 误产物：根目录 `nul`（67 字节），内容是 `ls: cannot access ':USERPROFILE\go\bin': No such file or directory`。
- untracked 会话日志：`sessionlogs/2026-08-26.md`，包含 Git 远程、SSH/代理配置、mihomo 规则等个人环境信息。
- ignored：`.trellis/.developer` 和 `.trellis/scripts/common/__pycache__/`。

`git check-ignore -v` 对 `bin/lenovo-driver.exe`、`sessionlogs/2026-08-26.md`、`nul`、`go.mod`、`cmd/`、`internal/` 全部无匹配，退出码 1，说明这些路径当前都不被忽略。

最近提交：

```text
d320af5 feat: add WPF driver installer UI and source audit bridge
cadcb42 chore: record journal
f3c40b1 refactor: split deterministic core from side-effect shell
e989729 feat: implement v5 official QuickFix driver source
```

Go 迁移相关文件全部是未提交 WIP；`sessionlogs/2026-08-26.md` 声称“工作树干净，main...origin/main 同步”，与当前状态矛盾。

### 忽略规则

`.gitignore` 当前内容（9 行）：

```text
# Runtime artifacts
*.log
lenovo_driver_plan.txt
install-state.json
install-state.json.*

# Editor/OS noise
.DS_Store
Thumbs.db
```

缺失项：`bin/`、`*.exe`、Go 构建产物、`sessionlogs/`、`nul`、编辑器目录等。

`.gitattributes` 当前内容（15 行）：

- `*.ps1 text eol=lf`
- `*.bat text eol=crlf`
- `.trellis/workspace/*/journal-*.md merge=union`
- 引用了不存在的 `.githooks/*` 和 `menu.txt`
- 没有 `*.go`、`go.mod`、`go.sum` 规则

`git config core.autocrlf` 为 `true`；Go 文件没有显式 eol 规则，混合仓库下容易出现 checkout 后 CRLF 与 `gofmt`/Go 工具链的噪音差异。

### 验证命令与结果

| 检查 | 命令 | 结果 |
|---|---|---|
| Go 工具链 | `go version` | 失败：`go: The term 'go' is not recognized...` |
| Go 构建 | `go build ./...` | 失败：`go` 不在 PATH |
| Go 测试 | `go test ./...` | 失败：`go` 不在 PATH |
| Go vet | `go vet ./...` | 失败：`go` 不在 PATH |
| PS 语法 | Parser::ParseFile x4 | 全部 `PARSE OK` |
| PS 核心测试 | `.\lenovo_driver_core.tests.ps1` | `passed=86, failed=0` |
| 现有 Go 二进制 Help | `.\bin\lenovo-driver.exe -Help` | `EXIT=0` |
| 现有 Go 二进制非法组合 x3 | `-CurrentOSOnly -LatestAcrossOS` 等 | 均 `EXIT=2` |
| PS 非法组合 x3 | `install_lenovo_drivers.ps1` 同组参数 | 均 `EXIT=2` |
| 已跟踪 diff 检查 | `git diff --check HEAD` | 无输出（不检查 untracked） |
| 单命令验证 | `scripts/verify.ps1` | 不存在 |
| CI workflow | `.github/`、根目录 yml/yaml | 不存在 |

### 文档状态

- `README.md` 仍以 PowerShell 为唯一开发/运行路径；未出现 `Go`、`go build`、`lenovo-driver.exe`、`scripts/verify.ps1`。
- `DRIVER_FACT_STANDARD.md` 仍是 PS1/事实标准文档，没有 Go 迁移说明；不矛盾但未接续。
- `lenovo_installer_improvement_plan.md` 仍标记为“已实现的 v5 范围契约”，未说明 PS1 冻结或 Go 新路径。
- `.trellis/spec/backend/index.md` 仍以 `install_lenovo_drivers.ps1` / `lenovo_driver_core.ps1` / `lenovo_driver_wpf.ps1` 为运行时结构。
- `.trellis/spec/backend/directory-structure.md` 第 9-12 行称“There are no packages, workspaces, or build artifacts”。
- `.trellis/spec/backend/quality-guidelines.md` 第 9-11 行称“This repository has no package manager or automated test suite”。
- 这些陈述在 Go 源码加入后已失真。

### Trellis 任务状态

`.trellis/tasks/08-26-go-migration/task.json`：

- `status: in_progress`
- `branch: null`
- `commit: null`
- `pr_url: null`
- `relatedFiles: []`

任务目录当前只有 `prd.md`、`design.md`、`implement.md`、`task.json`；没有 `research/`、没有分支、没有提交。工作流约定 `.trellis/tasks/` 是应进入版本控制的 task artifacts，而当前任务目录整体 untracked。

---

## 发现列表

### F1 [Critical] 缺少可执行的验证入口，Go 工具链缺失且没有 CI

- 位置：`go.mod`、`.trellis/tasks/08-26-go-migration/prd.md:33`、`implement.md:19-28`
- 问题：PRD 要求 `scripts/verify.ps1` 单命令通过 Go build/test/vet、PS parser、PS core tests、`git diff --check`；该文件不存在，且本机 `go` 不在 PATH。
- 证据：
  - `go version`、`go build ./...`、`go test ./...`、`go vet ./...` 均报 `go: The term 'go' is not recognized`。
  - `glob scripts/**/*` 和 `glob **/verify.ps1` 均无结果。
  - 常见 Go 安装路径（Program Files、LocalAppData、scoop、choco）也未找到 `go.exe`。
- 影响：无法证明当前 Go WIP 可编译、可测试、可 vet；任何“Go 已完成”的声明都不可验收。
- 建议：将安装/提供官方 Go 工具链设为 Phase 0 硬门槛；先创建 `scripts/verify.ps1` 并让它在 Windows 上覆盖 Go build/test/vet、PS parser、PS core tests、staged `git diff --check`。若远端是 GitHub，增加最小 Windows CI workflow。

### F2 [Critical] `LatestAcrossOS` 没有真正拉取并合并其他 OS 列表

- 位置：`internal/app/app.go:184-190`、`internal/app/view.go:42-50`、`internal/app/help.go:16`；PRD `prd.md:34`、`implement.md:33`
- 问题：`LatestAcrossOS` 只被解析、校验、在 help 中声称“Select the newest driver across all supported OS lists”，主流程没有遍历 `osList` 并合并其他 OS 驱动。
- 证据：
  - `app.go:184-190` 中只有 `listOsID := sysID`，`opts.LatestAcrossOS` 只影响提示文本，没有分支加载其他 OS。
  - `view.go:42` 只调用 `GetDriverObjects(ctx, categoryID, listOsID, "QuickFix")`，传入的是单一 `listOsID`。
  - `grep "LatestAcrossOS" internal/**/*.go` 只出现参数定义、校验、help 和日志提示，没有合并逻辑。
- 影响：PRD 的核心验收项无法通过；当前 `-LatestAcrossOS` 行为等价于普通 current-OS 模式，用户会得到错误承诺。
- 建议：在 `CompareOSDriverView` 中当 `opts.LatestAcrossOS` 为真时，遍历 `osList` 加载所有列表并交给 `compare.SelectLatestDrivers`；用 fixture 覆盖“当前 OS 有行、其他 OS 版本更新、最终选择跨 OS 行”。

### F3 [High] 403/URL 过期刷新没有接入下载流程

- 位置：`internal/api/client.go:245-273`、`internal/download/download.go:50-62`、`internal/app/install.go:137`
- 问题：`GetRefreshedDriverURL` 已实现但无调用者；下载遇到 403 或 URL 过期时只返回 HTTP 错误并结束。
- 证据：
  - `grep "GetRefreshedDriverURL" internal/**/*.go` 只有 `client.go:245-246` 的定义。
  - `download.go:61-62` 对非 2xx 状态返回 `fmt.Errorf("download returned HTTP %d", ...)`。
  - `app/install.go:137` 只调用 `Downloader.DownloadWithRetry(ctx, driver.FilePath, outFile, 3)`，没有刷新 URL。
- 影响：PRD `prd.md:35` 与 README 的 403 行为无法验收；真实下载场景会失败。
- 建议：在 `downloadVerified` 中捕获 403/URL 过期，调用 `GetRefreshedDriverURL` 后重试一次；用 fake HTTP server 做回归测试。

### F4 [High] WPF 仍启动 PS1 引擎，README/spec 也未标记 Go 为当前路径

- 位置：`lenovo_driver_wpf.ps1:38,333-341`、`README.md:3-7,209-269`、`implement.md:38-43`
- 问题：WPF 的 `$script:InstallerPath` 仍指向 `install_lenovo_drivers.ps1`，后台进程仍通过 PowerShell 启动 PS1；README 完全没有 Go 路径说明。
- 证据：
  - `lenovo_driver_wpf.ps1:38`：`$script:InstallerPath = Join-Path $PSScriptRoot 'install_lenovo_drivers.ps1'`
  - `lenovo_driver_wpf.ps1:335`：`$command = "& '$escapedScript' $($tokens -join ' ') *> '$escapedOut'"`
  - `README.md` 全文未出现 `Go`、`lenovo-driver.exe`、`scripts/verify.ps1`。
- 影响：PRD 的 WPF 默认消费 Go JSON、README 标注 Go 为开发/运行路径均未满足；双轨状态也没有文档化。
- 建议：若 WPF 切换不在本任务完成，必须按 PRD 允许的方式记录为已知边界并列出切换步骤；若完成，则把 `InstallerPath` 改为 `bin/lenovo-driver.exe` 或构建产物路径。

### F5 [High] `.gitignore` 未覆盖运行时/构建产物，当前所有 WIP 都是 untracked 且无归属记录

- 位置：`.gitignore:1-9`、`git status --short --ignored --untracked-files=all`
- 问题：`bin/lenovo-driver.exe`、`sessionlogs/`、`nul`、Go 构建产物均未被忽略；`git add -A` 会把 9.95 MB 二进制和误产物一起提交。
- 证据：
  - `.gitignore` 只有 `*.log`、`lenovo_driver_plan.txt`、`install-state.json` 等规则。
  - `git check-ignore` 对上述路径全部无匹配。
- 影响：验收项 `prd.md:38` 明确失败；存在误提交二进制、泄露本地环境日志、误删除用户 WIP 的风险。
- 建议：按下方“建议的 .gitignore 增量”补齐；提交前用 `git status --porcelain --untracked-files=all` 逐类确认。

### F6 [High] `nul` 与根目录 `sessionlogs/` 是明显误产物/个人日志，存在 WIP 归属风险

- 位置：根目录 `nul`、`sessionlogs/2026-08-26.md`
- 问题：`nul` 是 shell 重定向事故，内容为一条 `ls` 错误；`sessionlogs/` 是 Trellis workspace 之外的根级日志，包含 SSH、代理、mihomo、远程仓库配置。
- 证据：
  - `nul` 全文：`ls: cannot access ':USERPROFILE\go\bin': No such file or directory`
  - `sessionlogs/2026-08-26.md:22-43` 记录 `~/.ssh/config`、`mihomo 127.0.0.1:7890`、代理规则等。
- 影响：直接提交会污染仓库并可能泄露本地网络/SSH 配置；直接 `git clean` 可能删掉用户有意保留的日志。
- 建议：`nul` 应删除/忽略；`sessionlogs/` 应忽略，或将内容迁移到 `.trellis/workspace/` 中的正式 journal；任何清理前先让用户确认归属。

### F7 [High] `compare.go` 超 500 行，且 path 工具函数三处重复

- 位置：`internal/compare/compare.go:1-695`、`internal/api/parse.go:182-188`、`internal/audit/audit.go:301-316`、`internal/app/evidence.go:137-153`
- 问题：PRD `prd.md:37` 要求新文件不超过 500 行、重复 path 工具函数已合并；当前 `compare.go` 695 行，`pathBase`/`pathParent` 在三个包重复。
- 证据：
  - 文件行数统计：`internal/compare/compare.go` 695 行。
  - 三个文件分别定义了功能相同的 path 工具。
- 影响：结构验收项失败；后续维护和交叉审计更容易产生行为漂移。
- 建议：先按 `implement.md:46-49` 拆分 `compare.go`，把 path 工具集中到一个 `internal/pathutil` 或现有合适包，再跑 `go test`。

### F8 [High] `-Help` 输出与 PS1 CLI 契约不一致

- 位置：`internal/app/help.go:4-37`、`install_lenovo_drivers.ps1` 的 `-Help` 输出
- 问题：Go help 缺少 PS1 help 中的 `-Elevated`、交互选择 `y/a/s/t/n`、数据源说明；PS1 help 也未列出 `-GuiExportPath`/`-GuiInstallCodes`。两边不是同一契约文本。
- 证据：
  - Go help 只列出 `-DryRun` 到 `-Help`，没有 `-Elevated`。
  - PS1 `-Help` 输出包含 `-Elevated`、`Interactive choices`、`Data sources`、`Plan file`、`Driver history`、`Log file`。
  - `install_lenovo_drivers.ps1:55-69` 参数块实际包含 `-GuiExportPath`/`-GuiInstallCodes`，但 PS1 help 未展示。
- 影响：`prd.md:25` “输出与原 CLI 契约一致”没有可执行判据；验收人无法判断通过。
- 建议：先定义“原 CLI 契约”是 PS1 help、README 还是参数行为；再为 Go help 补 `-Elevated`/交互说明，并为 PS1/Go help 做 fixture 对比测试。

### F9 [High] 提权重启动丢失子进程退出码

- 位置：`internal/app/app.go:250-264`
- 问题：`RelaunchElevated` 用 PowerShell 启动提权子进程并 `exit $p.ExitCode`，但 Go 侧 `_ = cmd.Run()` 忽略错误后固定返回 `true`；`Run` 随后直接 `return 0`。
- 证据：`app.go:263` `_ = cmd.Run()`；`app.go:133-135` 中 `if relaunched := ...; relaunched { return 0 }`。
- 影响：提权安装实际失败时，外层进程可能返回 `0`，违反退出码契约。
- 建议：捕获 `cmd.ExitCode()` 或把提权结果传回 `Run`；为提权失败路径加测试或明确标记手动边界。

### F10 [Medium] Go 测试覆盖不足，无法支撑“行为等价”验收

- 位置：`internal/app/app_test.go`、`internal/api/parse_test.go`、`internal/audit/audit_test.go`、`internal/compare/compare_test.go`、`internal/download/download_test.go`、`internal/plan/plan_test.go`
- 问题：共 26 个 Go `func Test`，但没有覆盖三个非法组合的完整矩阵、WPF JSON 协议、`-GuiInstallCodes` 不匹配退出码 3、URL 刷新、历史 CSV、installer 分派、inventory 调用边界。
- 证据：
  - `app_test.go` 只测试 `-Help`、`-CurrentOSOnly -LatestAcrossOS`、`selectByCodes`。
  - 没有 `internal/inventory/*_test.go`、`internal/install/*_test.go`、`internal/api/client_test.go`。
  - `grep "func Test" internal/**/*_test.go` 共 26 个函数。
- 影响：即使 `go test ./...` 未来通过，也不能证明 Go 与 PS 的 86 个离线断言和行为契约等价。
- 建议：按 PS core 86 个测试逐条映射到 Go；补三个非法组合、JSON 字段、URL refresh、history/install 的纯逻辑测试；对外部 I/O 用 fake/httptest 隔离。

### F11 [Medium] `.gitattributes` 缺少 Go/Windows 混合规则并含陈旧引用

- 位置：`.gitattributes:1-15`
- 问题：没有 `*.go text eol=lf`、`go.mod text eol=lf`、`go.sum text eol=lf`；`core.autocrlf=true` 会让未显式标记的 Go 文件 checkout 成 CRLF。同时引用了不存在的 `.githooks/*` 和 `menu.txt`。
- 证据：`git config --get core.autocrlf` 返回 `true`；`git ls-files` 中没有 `.githooks/` 或 `menu.txt`。
- 影响：Go 源码换行随用户本机配置漂移，`gofmt`/diff 噪音风险增加；陈旧规则降低可维护性。
- 建议：为 Go 相关文本文件增加显式 `eol=lf`，删除或更新失效条目；保留 `*.ps1`/`*.bat` 规则。

### F12 [Medium] Trellis backend specs 仍描述旧 PowerShell-only 架构

- 位置：`.trellis/spec/backend/index.md:9-12`、`directory-structure.md:9-12`、`quality-guidelines.md:9-11`
- 问题：spec 声称“没有包管理器、没有自动测试套件、没有 packages/workspaces/build artifacts”，与 untracked Go 工程明显冲突。
- 证据：上述文件原文均把运行时写成 `install_lenovo_drivers.ps1` / `lenovo_driver_core.ps1` / `lenovo_driver_wpf.ps1` 三层 PS 架构。
- 影响：后续实现/检查 agent 会按错误 spec 决策；迁移到 Go 后接续者需要自己重建约定。
- 建议：在 Phase 5 按 `implement.md:54` 更新 backend spec 或新增 Go spec，明确冻结 PS1、Go 目录结构、验证命令、外部依赖策略。

### F13 [Medium] Trellis 任务状态未随仓库状态落盘

- 位置：`.trellis/tasks/08-26-go-migration/task.json:6,15-19`
- 问题：任务 `status=in_progress`，但任务目录整体 untracked，`branch/commit/pr_url/relatedFiles` 全空，也没有 `research/`；根级 `sessionlogs/` 与 Trellis workspace journal 分离。
- 证据：`task.json` 字段为空；`git status` 显示 `.trellis/tasks/08-26-go-migration/` 为 `??`。
- 影响：换人接续时看不到任务边界、提交归属和已验证基线；远程分支也没有承载 WIP。
- 建议：把 task artifacts 作为 WIP 提交的一部分纳入版本控制；在 commit 时填 `branch/commit`；把需要保留的会话记录写入 `.trellis/workspace/`。

### F14 [Medium] 没有 CI，规划只承诺“无远端 CI 则本地脚本”

- 位置：`.github/` 不存在；`implement.md:28`
- 问题：仓库没有 workflow；`implement.md` 允许无远端 CI 时只做本地脚本。对交付卫生来说，最小 CI 可以低成本捕获 gofmt、build、test、vet、PS parser 回归。
- 证据：`glob .github/**/*`、根目录 `*.yml`、`*.yaml` 均无结果。
- 影响：换机器/换环境后验证能力不可复现；真实 API 和真实机器仍需要手动边界，但静态门禁可以自动化。
- 建议：PRD/implement 增加“最小 CI 为推荐项”：Windows runner 上执行 `scripts/verify.ps1`，不跑真实 API/安装/GUI；CI 通过不等于真实机器验收。

### F15 [Low] `git diff --check` 不覆盖 untracked WIP

- 位置：验证命令清单与当前执行结果
- 问题：本次 `git diff --check HEAD` 无输出，但 Go 源码和任务文档全部 untracked，该命令对它们无效。
- 证据：`git status` 显示 `cmd/`、`internal/`、`go.mod` 均为 `??`；`git diff --check` 只检查已跟踪差异。
- 影响：验收时“diff check 干净”可能产生假阳性。
- 建议：在 verify 脚本中先暂存待验文件再跑 `git diff --check --cached`，或逐文件用 `git diff --no-index --check`。

---

## 建议的 .gitignore 增量

```gitignore
# Go build outputs
/bin/
*.exe
*.exe.sha256
*.test
coverage.out

# Accidental/runtime session artifacts
/sessionlogs/
/nul

# Editor/OS noise
.vscode/
.idea/
```

说明：

- `/bin/` 覆盖当前 `bin/lenovo-driver.exe`；若未来需要发布预编译 exe，应走 GitHub Release 或 CI artifact，而不是提交进 git。
- `/nul` 覆盖重定向事故产生的根目录文件。
- `/sessionlogs/` 覆盖根级个人/运行时日志；正式会话记录应放入 `.trellis/workspace/`。
- `*.exe` 是保守策略；若仓库后续出现必须跟踪的测试 fixture 二进制，再单独加 `!` 例外。

---

## 建议的验证命令清单

```powershell
# 1. 工具链与格式
go version
go build -o "$env:TEMP\lenovo-driver-audit.exe" ./cmd/lenovo-driver
go test ./...
go vet ./...
gofmt -l .

# 2. PowerShell 基线
$files = @('install_lenovo_drivers.ps1','lenovo_driver_core.ps1','lenovo_driver_wpf.ps1','lenovo_driver_core.tests.ps1')
foreach ($file in $files) {
  $tokens = $null; $errors = $null
  [System.Management.Automation.Language.Parser]::ParseFile($file, [ref]$tokens, [ref]$errors) | Out-Null
  if ($errors) { $errors | Format-List; exit 1 } else { "PARSE OK $file" }
}
.\lenovo_driver_core.tests.ps1

# 3. CLI 契约（PS1 与 Go 各跑一次）
& .\install_lenovo_drivers.ps1 -Help
& .\install_lenovo_drivers.ps1 -CurrentOSOnly -LatestAcrossOS   # EXIT=2
& .\install_lenovo_drivers.ps1 -CurrentOSOnly -TargetOS 248      # EXIT=2
& .\install_lenovo_drivers.ps1 -LatestAcrossOS -TargetOS 248     # EXIT=2
& .\bin\lenovo-driver.exe -Help
& .\bin\lenovo-driver.exe -CurrentOSOnly -LatestAcrossOS         # EXIT=2
& .\bin\lenovo-driver.exe -CurrentOSOnly -TargetOS 248           # EXIT=2
& .\bin\lenovo-driver.exe -LatestAcrossOS -TargetOS 248          # EXIT=2

# 4. Git 卫生（先暂存待验文件）
git diff --check --cached
git status --porcelain --untracked-files=all
```

`scripts/verify.ps1` 应把这些步骤收敛为一条命令；建议包含 `go fmt` 检查，但不包含真实网络、真实安装或 WPF GUI。

---

## 手动验收边界清单

以下项当前环境无法自动化验证，验收时必须明确标记为手动/真实环境边界：

1. 真实 Lenovo API：`-DryRun`、QuickFix/Web fallback、OS 列表解析、`LatestAcrossOS` 真实多 OS 合并。
2. 真实 Lenovo 机器：机器型号、序列号、PnP 设备、已安装应用、驱动版本探测。
3. 真实下载与完整性：官方 CDN、403/URL 过期刷新、文件大小、MD5、SHA-256 companion。
4. 真实安装器：EXE/MSI/INF/ZIP/CAB、Inno 参数、超时、进程树 kill、reboot 3010/1641、EXE 失败后 fallback。
5. 提权路径：`install_lenovo_drivers.bat`、UAC、`-Elevated` 的退出码传递。
6. WPF：`-SelfTest` 截图、真实窗口交互、`-WorkerSmoke`（该 smoke 会调用真实 API，且依赖 GUI/desktop session）。
7. Go 工具链：当前审计未运行 `go build/test/vet`，必须由有 Go 的环境执行后记录结果。
8. `-Help` 契约：需要先定义“与原 CLI 契约一致”是逐字文本还是参数/语义等价。
9. PS1 与 Go 行为等价：以 86 个 PS core 测试为基线，但仅依赖人工对照不足，应映射成 Go fixture 测试。

---

## 与现有 implement.md 的差异/补充

- `implement.md:19-21` 的 Phase 0 把 Go 工具链写成“确认或安装”；本审计结论是必须升级为硬性前置条件，否则无法进入任何 Go 验收。
- `implement.md:26-28` 的 Phase 1 只计划本地 `verify.ps1`；建议补充最小 GitHub Actions Windows workflow，并明确 `git diff --check` 对 untracked 的局限。
- `implement.md:33` 写“修 `LatestAcrossOS`”，实际代码没有合并逻辑；应描述为“实现并测试 `LatestAcrossOS`”，并给出 fixture 验收。
- `implement.md:34` 写“接 403 URL 刷新”；实际 `GetRefreshedDriverURL` 无调用者，应先补下载流程接线和 fake HTTP 测试。
- `implement.md:35` 写“校验 CLI 契约”；现有 Go 测试只覆盖一个非法组合，应补三个组合、Help 契约和退出码 3。
- `implement.md:38-42` 的 Phase 3 WPF 切换尚未开始；若本期只交付 CLI，应在 README/PRD 中显式记录“WPF 未切 Go”的边界和切换步骤。
- `implement.md:46-48` 的 Phase 4 拆分 `compare.go` 和合并 path 工具是 PRD 硬验收项，不是可选重构。
- `implement.md:53-55` 的 Phase 5 文档更新尚未开始；backend spec、README、improvement plan 都应改为“PS1 冻结基线 + Go 主路径”的表述。
- `implement.md:63-64` 的 Phase 6 应新增：提交前 WIP 分类清单、`nul`/`sessionlogs`/`bin` 归属确认、任务 task.json 的 `branch/commit` 更新。
