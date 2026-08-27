# 迁移 Lenovo Driver Installer 到 Go

## Goal

把当前 PowerShell 驱动的 Lenovo Driver Installer 逐步迁移到 Go 核心引擎，保留现有 WPF 表层，最终让 PS1/bat 退出长期维护路径。迁移不是“Go 当中间层、PS1 当底层”，而是 Go 拥有业务逻辑，WPF 只消费 Go 的 JSON 输出。

## Requirements

- 新增 Go 工程，核心业务逻辑全部位于 Go：Lenovo API、机器/OS/PnP/应用探测、驱动匹配、版本比较、来源审计、下载校验、安装器调度、CLI、日志、历史记录。
- 保持现有用户可见 CLI 契约：`-DryRun`、`-CurrentOSOnly`、`-LatestAcrossOS`、`-TargetOS`、`-SkipHashCheck`、`-IncludeBios`、`-DownloadOnly`、`-DownloadDir`、`-Model`、`-Elevated`、`-Help`。
- 保持现有交互选项：`y`、`a`、`s`、`t`、`n`。
- 保持现有退出码：`0` 成功或无选择，`1` 下载/安装失败，`2` 非法参数组合，`3` GUI 驱动码不匹配。
- 保持现有数据源：QuickFix 官方 API 优先，官方 webpage API 回退；`-LatestAcrossOS` 是显式实验模式。
- 保持现有下载完整性规则：文件大小、官方 MD5、本地 SHA-256 companion、`-SkipHashCheck`。
- 保持现有安装器处理：EXE Inno 风格、MSI、pnputil、zip/cab 解包、超时与进程树 kill、EXE 失败后的提取 INF/内部安装器回退。
- 保持现有来源审计：install history、setupapi offline/dev/setup 日志解析、当前/替代 OS 官方匹配、DriverStore 证据；不猜测缺失来源。
- 保持 WPF 现有 JSON 协议：Go 输出 `lenovo_gui_export_*.json` 格式，WPF 读取后回传 `-GuiInstallCodes`。
- 为 Go 核心提供单元测试，覆盖现有离线纯逻辑场景，避免依赖真实网络和真实 Lenovo 机器。
- 迁移过程中保留原 PS1 文件作为对照与回滚基线，直到 Go 版本通过验收后进入删除/归档阶段。

## Acceptance Criteria

- [ ] `go build ./...` 通过，产物为 Windows 可执行文件。
- [ ] Go 核心单元测试通过，覆盖版本解析/比较、硬件匹配、适用性、setupapi 日志解析、来源审计、选择解析等核心逻辑。
- [ ] `lenovo-driver.exe -Help` 输出与原 CLI 契约一致；契约以 fixture 固化，包含 `-Elevated`、交互选择、精确驱动集合提示。
- [ ] `lenovo-driver.exe -CurrentOSOnly -LatestAcrossOS` 返回 `2`。
- [ ] `lenovo-driver.exe -CurrentOSOnly -TargetOS 248` 返回 `2`。
- [ ] `lenovo-driver.exe -LatestAcrossOS -TargetOS 248` 返回 `2`。
- [ ] Go 生成 `%TEMP%\lenovo_driver_plan.txt`、`%TEMP%\lenovo_driver_history.csv`、`%TEMP%\lenovo_driver_install.log`，格式与现有文档一致。
- [ ] Go 生成 WPF 可读 JSON：字段与当前 `Export-LenovoDriverViewJson` 输出兼容；`SourceAudit` 以 Go 输出为规范，WPF 按字符串渲染。
- [ ] 现有 WPF 至少通过 `-WorkerSmoke` 或等价的 Go JSON smoke 验证；WPF 默认消费 Go JSON，不再调用 PS1 业务引擎。
- [ ] 原 PS1/bat 未删除，作为回滚基线；README 明确标注 Go 为新的开发/运行路径，PS1 为冻结基线。
- [ ] `scripts/verify.ps1` 单命令通过：Go build/test/vet/gofmt、PS parser、PS core tests、CLI 冒烟、`git diff --check`。
- [ ] `LatestAcrossOS` 在 Go 主流程中实际拉取并合并其他 OS 列表。
- [ ] 下载遇到 403/URL 过期时使用 `GetRefreshedDriverURL` 刷新后重试。
- [ ] EXE 超时进入 fallback；fallback 按 `DriverCode.log` 优先定位，不在无日志证据时扫描任意 `is-*.tmp` 安装 INF；EXE 静默失败后提供 `r/s` 交互。
- [ ] 非管理员提权重启后父进程返回真实子进程退出码，UAC 取消或失败不得返回 `0`。
- [ ] Go 可读取 PS1 生成的带 UTF-8 BOM 历史 CSV；历史/日志写入失败显式记录，不静默吞错。
- [ ] Go 新文件或拆分后文件不超过 500 行；重复的 path 工具函数已合并。
- [ ] `.gitignore` 覆盖 `bin/*.exe`、`sessionlogs/`、`nul`、Go 构建产物；验收时无未跟踪构建产物。
- [ ] 三方独立审计与交叉会审结论已纳入实施计划；审计文件保留在 `research/`。

## Notes

- 本次按“核心/CLI 优先，WPF 随后”拆分，但 WPF 切换属于本任务收口范围，不能作为永久已知边界。
- 迁移属于复杂任务，需要 `design.md` 和 `implement.md`；执行前必须先通过三方独立审计与交叉会审。
- 真实 Lenovo API、真实机器、真实安装器和桌面交互为手动验收边界；自动化覆盖离线 fixture、CLI 冒烟和可注入 fake。
