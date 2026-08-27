# Lenovo Driver Installer 可维护性收口：Go 迁移完成计划

> 本计划已通过三方独立审计与交叉会审。审计原文与综合结论见
> `.trellis/tasks/08-26-go-migration/research/`。

## 目标

把当前“PS1 可运行 + Go 半成品”的仓库收敛成单一 Go 维护路径。PS1/bat 冻结为回滚基线，Go 拥有全部业务逻辑，仓库用一条验证命令证明行为等价和结构健康。

## 边界

- WPF 仍保留为桌面表层，但只消费 Go 的 JSON 导出与 `-GuiInstallCodes`，不再作为 PS1 业务逻辑的入口。
- PS1/bat 在验收前保留，不继续加功能；验收后是否删除或归档另开任务决定。
- 不重写 WPF 为原生 C#/Go UI。
- 不新增安装器族、不扩展跨平台支持。
- 不改动冻结 PS1 的风格；只把它作为行为基准和回滚基线。
- 真实 Lenovo API、真实机器、真实安装器和桌面交互为手动验收边界；自动化只覆盖离线 fixture、CLI 冒烟和可注入 fake。

## 交叉会审确认的阻塞项

| 编号 | 问题 | 必须完成的行为 |
|---|---|---|
| B1 | `-LatestAcrossOS` 只解析参数，没有合并其他 OS 列表 | 主流程按 PS1 `install_lenovo_drivers.ps1:1429-1470` 拉取并合并全部 OS 列表 |
| B2 | `GetRefreshedDriverURL` 无调用者 | 403/URL 过期时刷新 URL 并重试一次 |
| B3 | Go 工具链缺失、无 `scripts/verify.ps1`、无 CI | 先建立可执行验证门槛 |
| B4 | WPF 仍启动 PS1，README/spec 仍是 PowerShell-only | WPF 切换 Go JSON，文档同步 |
| B5 | EXE 超时不 fallback；fallback 扫描所有近期临时目录；缺 `r/s` 交互 | 按 PS1 日志优先算法修复并测试 |

## 执行顺序

### Phase 0：基线与环境（硬门槛）

1. 提供或安装官方 Go 工具链；`go version` 可执行。若无法安装，停在 Phase 0 并报告，不进入 Go 验收。
2. 记录 PS1 基线：`lenovo_driver_core.tests.ps1` 通过，PS1 文件 parse OK。
3. 跑当前 Go：`go build ./...`、`go test ./...`、`go vet ./...`、`gofmt -l .`，记录现有失败，不掩盖。
4. 检查 untracked WIP 归属：`bin/`、`sessionlogs/`、`nul`、`.trellis/tasks/08-26-go-migration/`；不擅自删除用户文件。

### Phase 1：验证基础设施

1. 新增 `scripts/verify.ps1`，单命令覆盖：
   - `go build ./...`、`go test ./...`、`go vet ./...`、`gofmt -l .`
   - PS1 parser、`lenovo_driver_core.tests.ps1`
   - PS1/Go 的 `-Help` 与三种非法组合退出码
   - `git diff --check --cached`、untracked 构建产物检查
2. 更新 `.gitignore`：`/bin/`、`*.exe`、Go 构建产物、`/sessionlogs/`、`/nul`、编辑器目录。
3. 更新 `.gitattributes`：`*.go`、`go.mod`、`go.sum` 显式 `eol=lf`，移除失效的 `.githooks/*`、`menu.txt` 引用。
4. 推荐新增最小 GitHub Actions Windows workflow，只跑 `scripts/verify.ps1`，不跑真实 API/安装/GUI。

### Phase 2：行为等价补齐

1. 以 PS core 86 条断言为清单，为 Go 补离线行为测试；报告按场景而非断言数记录覆盖。
2. 实现 `LatestAcrossOS`：`view.go` 遍历 `osList` 拉取并合并驱动，再统一过滤和 `SelectLatestDrivers`；补多 OS fixture。
3. 接通 403 URL 刷新：`downloadVerified` 捕获 403/Forbidden，调用 `GetRefreshedDriverURL`，更新 `driver.FilePath` 后重试一次；用 `httptest` 覆盖。
4. 对齐 EXE 安装路径：
   - 超时后调用 `ExtractedDriverFallback`。
   - fallback 先按 `DriverCode.log` 定位 `is-*.tmp` 目录；只有 NVIDIA 且无日志目录时才按 PS1 限制扫描全局 `Display.Driver`。
   - 内部安装器使用 `InstallCode`/`InstallParameter` 参数和 PS1 的 `Display.Driver` 路径处理。
   - 静默失败且 fallback 不可用时提供 `r/s` 交互。
   - 补 timeout、fallback、`3010/1641`、启动失败测试。
5. 修复提权退出码：`RelaunchElevated` 必须捕获 PowerShell 子进程退出码并把结果传回 `Run`；UAC 取消/失败不得返回 0。
6. 修复历史 CSV：`ReadHistory` 剥离 UTF-8 BOM，兼容 PS1 生成的旧 CSV；补 BOM fixture。
7. 对齐 CLI 契约：
   - Go help 补 `-Elevated`、交互选择、精确驱动集合提示、数据源说明。
   - 三种非法组合均 exit 2；`-GuiInstallCodes` 无匹配 exit 3；未知 flag 行为记录为有意差异。
   - 用 fixture 固化 help 与 README 的契约，不再凭人工对照。
8. 补 JSON 协议契约测试：字段与 `Export-LenovoDriverViewJson` 对齐；`SourceAudit` 以 Go 输出 `Category: Summary` 为新规范，WPF 按字符串渲染并兼容旧字段。
9. 对齐交互行为：`showActionPreview` 打印每项驱动的 code/name/remote/local/status；零候选分支、`t` 失败状态回滚、下一 OS 标签补 OSID。
10. 修复 OS 解析：`findOSEntry` 大小写不敏感；web `statusCode` 非 200 时失败。
11. 对齐 alternate source map：按 PS1 惰性加载时机实现，并补可解释 fixture。
12. 显式错误处理：历史、日志、清理、inventory 脚本失败不得静默吞掉；`GetRefreshedDriverURL` 返回 `(string, error)`。

### Phase 3：WPF 切换

1. 先完成 JSON 契约 fixture，确保 WPF 可解析 Go 输出。
2. 把 `lenovo_driver_wpf.ps1` 的子进程从 `install_lenovo_drivers.ps1` 改为构建后的 `lenovo-driver.exe`，使用 `-GuiExportPath` / `-GuiInstallCodes`。
3. PS1 文件保留但不再作为默认路径；README 写明切换状态和回滚方式。
4. 跑 `lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation` 或等价 Go JSON smoke；真实 WPF 桌面交互标为手动边界。

### Phase 4：结构收口

1. 拆 `internal/compare/compare.go`（695 行）为 version/hardware/software/format/select 等小文件。
2. 拆分 `App.Run`、`ConvertFromImportLogText`、`TestDriverApplicable`、`InstallDriverFile`、`ExtractedDriverFallback` 等超长函数。
3. 合并 `pathBase`/`pathParent` 重复实现到一个 `internal/pathutil` 或等价小包；消除 `powershellExe` 重复。
4. 删除无调用符号（`normalizeSourceString`、`EarliestTime` 或统一使用、`Timeout` 或统一使用）。
5. 提升静态正则和厂商规则为包级数据表，减少循环内 `regexp.MustCompile`。
6. 新文件或拆分后文件保持不超过 500 行；先测试后拆分，不顺手重写行为。

### Phase 5：文档与规范

1. README 标注 Go 为主路径、PS1 为冻结基线、`scripts/verify.ps1` 为验证入口，并写明 WPF 已切 Go。
2. 更新 `.trellis/spec/backend` 或新增 Go spec，覆盖目录结构、验证命令、冻结策略、真实/手动边界。
3. 更新 `prd.md` / `design.md` 到最终一致状态，保留审计文件链接。

### Phase 6：验收

1. `scripts/verify.ps1` 全绿；Go build/test/vet/gofmt 全部通过。
2. PS1 baseline 通过；Go 测试覆盖等价场景和全部交叉会审阻塞项。
3. 三个 CLI 非法组合均 exit 2；help、GUI mismatch、提权退出码测试通过。
4. WPF smoke 通过 Go JSON。
5. `git status` 无未跟踪构建产物；`git diff --check` 干净。
6. 确认 `sessionlogs`、`nul`、`bin` 产物归属；任务 `task.json` 更新 `branch/commit/relatedFiles`。
7. 按 Trellis 3.4 提交前分类 AI 编辑文件与用户 WIP，不擅自纳入未确认文件。

## 验证命令

```powershell
.\scripts\verify.ps1
go build ./...
go test ./...
go vet ./...
gofmt -l .
.\lenovo_driver_core.tests.ps1
.\lenovo_driver_wpf.ps1 -WorkerSmoke -NoElevation
```

CLI smoke：

```powershell
go run ./cmd/lenovo-driver -Help
go run ./cmd/lenovo-driver -CurrentOSOnly -LatestAcrossOS   # exit 2
go run ./cmd/lenovo-driver -CurrentOSOnly -TargetOS 248      # exit 2
go run ./cmd/lenovo-driver -LatestAcrossOS -TargetOS 248     # exit 2
go run ./cmd/lenovo-driver -DryRun -GuiExportPath "$env:TEMP\lenovo_gui_export.json"
```

## 风险与回滚

- Go 工具链缺失：安装官方工具链后再继续；若不允许安装，停在 Phase 0 并报告。
- 真实 API、硬件、桌面不可测：用 fixture、单测和 WorkerSmoke 覆盖；真实 Lenovo API/机器验收标记为手动边界。
- 验证命令可能触发真实主流程：`scripts/verify.ps1` 只跑 Help、非法组合和离线测试，不允许未知参数探测进入真实 API。
- EXE fallback 有安装错误 INF 风险：按 PS1 日志优先算法实现，不允许无日志证据时扫描任意 `is-*.tmp` 安装 INF。
- 现有 untracked WIP：不擅自删除；提交前按 Trellis 3.4 分类确认。
- 行为差异：任何 Go 修改都以 PS1 测试和 JSON fixture 作为回归基准；必要时回退对应文件。
