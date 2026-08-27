# 三方独立审计交叉会审报告

## 审计来源

| 审计 | 文件 | 结论 | 数量 |
|---|---|---|---|
| 可维护性与架构 | `research/audit-maintainability.md` | D（较差，急需收口） | Critical 4、High 6、Medium 6、Low 3 |
| 行为等价与迁移风险 | `research/audit-behavior-equivalence.md` | Go 与 PS1 不等价，迁移阻塞 | Critical 3、High 4、Medium 6、Low 3 |
| 交付卫生与可验证性 | `research/audit-delivery-hygiene.md` | 当前仓库不可验收 | Critical 2、High 7、Medium 5、Low 1 |

原始发现合计 41 条。交叉会审按证据复核、跨报告去重后，收敛为 5 个阻塞项和 8 个高优先级项；部分 Medium 作为同一批收口工作的子项处理。

## 会审结论

**现状不可验收，不能按当前规划直接进入执行验收。** PS1 基线和少量 Go CLI 冒烟是绿的，但 Go 不是实际运行路径，核心迁移功能未实现，验证入口和工具链缺失，文档仍描述旧架构。必须先完成“验证基础设施 + 行为等价 + WPF 切换 + 结构收口”，再进入最终验收。

## 交叉验证后的共识发现

### Critical / 阻塞项

| # | 发现 | 证据复核 | 来源 |
|---|---|---|---|
| B1 | `-LatestAcrossOS` 只解析参数，主流程没有拉取并合并其他 OS 列表 | 已核对 `internal/app/view.go:42-50`；PS1 基线 `install_lenovo_drivers.ps1:1429-1470` 有合并逻辑 | 三份报告一致 |
| B2 | `GetRefreshedDriverURL` 无调用者，403/URL 过期刷新未接入下载 | 已核对 `internal/api/client.go:245-273` 与 `internal/app/install.go:137`；PS1 基线 `install_lenovo_drivers.ps1:1695-1714` 有刷新重试 | 三份报告一致 |
| B3 | Go 工具链缺失、`scripts/verify.ps1` 不存在、无 CI，Go 源码无法被 build/test/vet 验证 | `go` 不在 PATH；`scripts/` 不存在；`.github/` 不存在 | 可维护性、交付一致 |
| B4 | WPF 仍调用 PS1，README/Trellis spec 仍把 PowerShell 描述为唯一架构 | 已核对 `lenovo_driver_wpf.ps1:38`；`README.md` 无 Go 路径 | 三份报告一致 |
| B5 | EXE 超时不走 fallback；Go fallback 扫描所有近期 `is-*.tmp`，与 PS1 日志优先算法不等价且可能安装无关 INF；EXE 静默失败后缺少 `r` 交互 | 已核对 `internal/install/install.go:147-161,244-311` 与 PS1 `install_lenovo_drivers.ps1:945-1153` | 行为等价审计独有，已复核 |

### High / 必须在验收前处理

| # | 发现 | 证据复核 | 来源 |
|---|---|---|---|
| H1 | 非管理员提权重启后子进程退出码丢失，失败可能被报告为 0 | `internal/app/app.go:250-265` 忽略 `cmd.Run` 错误；PS1 `install_lenovo_drivers.ps1:1384-1385` 传递 `$p.ExitCode` | 行为、交付一致 |
| H2 | Go 读取 PS1 历史 CSV 时可能遇到 UTF-8 BOM，导致 header 不匹配、历史证据丢失 | PS1 `Set-Content -Encoding UTF8` 写 BOM；Go `encoding/csv` 不剥 BOM；`internal/app/history.go:34-45` | 行为审计独有，风险成立 |
| H3 | `-Help` 契约不一致：Go help 缺 `-Elevated`、交互选择、精确驱动集合提示；PS1 help 未列出 GUI 参数与退出码 | 已核对 `internal/app/help.go:3-37` 与 PS1 `install_lenovo_drivers.ps1:1171-1213` | 交付审计独有，已复核 |
| H4 | `.gitignore`/`.gitattributes` 未覆盖 Go、构建产物、`bin/`、`sessionlogs/`、`nul`；存在 9.95MB 未跟踪 exe 和误产物 | `git check-ignore` 无匹配；`git status` 列出 `bin/`、`nul`、`sessionlogs/` | 可维护性、交付一致 |
| H5 | `internal/compare/compare.go` 695 行；`App.Run` 等函数超长；`pathBase`/`pathParent` 三处重复 | 行数与代码复核一致 | 可维护性、交付一致 |
| H6 | 历史/日志写入错误被 `_ =` 静默吞掉，审计链不可信 | `internal/app/install.go` 多处忽略 `WriteHistoryRecord`；`internal/app/helpers.go:25` 忽略日志写失败 | 可维护性审计独有，成立 |
| H7 | Go 测试覆盖不足：现有 26 个 `Test` 只覆盖少量纯逻辑，86 条 PS 断言未映射；install/inventory/client/JSON/history 无足够测试 | 已核对各 `_test.go` | 三份报告一致 |
| H8 | README 与 `.trellis/spec/backend` 仍称“无包、无构建产物、无自动测试”，与实际 Go 工程冲突 | 已核对 `directory-structure.md:9-12`、`quality-guidelines.md:9-11` | 可维护性、交付一致 |

### 已复核的其他风险

- 交互预览只打印数量，不打印精确驱动集合：`internal/app/interactive.go:127-129` 与 PS1 `Show-DriverActionPreview` 不等价。
- 无候选分支、toggle 失败状态、下一 OS 标签不一致。
- OS 名称匹配大小写敏感、web `statusCode` 未校验：`internal/api/client.go:226-243`。
- alternate source map 加载时机与 PS1 不同，会额外请求其他 OS 列表。
- JSON `SourceAudit` 字段值不等价：PS1 输出对象字符串，Go 输出 `Category: Summary`。WPF 仅按字符串展示，可切换为 Go 规范并补契约测试。
- `inventory` 内嵌 PowerShell 脚本存在大量 `catch {}` 吞错和空输出返回 nil 的问题。
- `nul` 在 PowerShell 中作为 Windows 保留名无法直接 `Get-Item`，但 git 明确列出，按真实误产物处理。
- 行为审计曾因探测 PS1 未知参数意外触发真实 API，已终止；后续验证命令必须避免进入真实主流程，verify 脚本应只跑 Help/非法组合/离线测试。

## 最终优先级

1. 建立硬性验证门槛：安装/提供官方 Go 工具链，创建 `scripts/verify.ps1`，补最小 CI。
2. 实现 `LatestAcrossOS` 合并并用多 OS fixture 测试。
3. 接通 403/URL 刷新并用 fake HTTP server 测试。
4. 对齐 EXE 安装路径：超时 fallback、日志优先定位、NVIDIA 限制、`InstallCode`、`r/s` 交互，并补测试。
5. 修复退出码传递、历史 CSV BOM、help 契约、交互预览与 OS 解析。
6. 切换 WPF 到 Go JSON，并同步 README/Trellis spec。
7. 补 Go 测试：把 PS 86 条断言映射到行为场景，覆盖 JSON、history、download、install、CLI。
8. 结构收口：拆分 `compare.go` 和大函数、合并 path 工具、显式错误处理。
9. 仓库卫生：更新 `.gitignore`/`.gitattributes`，确认 `nul`、`sessionlogs/`、`bin/` 归属。
10. 最终验收：单命令验证、CLI 冒烟、WPF smoke、无未跟踪构建产物、任务元数据落盘。

## 对规划的修订

详见更新后的 `implement.md`。核心变化：

- Phase 0 把 Go 工具链从“确认或安装”升级为硬性前置条件。
- Phase 1 加入 `gofmt -l`、`git diff --check --cached`、untracked 产物检查，并把最小 CI 列为推荐验收项。
- Phase 2 从“修 LatestAcrossOS/403”改为“实现并测试”，新增 EXE fallback、提权退出码、CSV BOM、help 契约、交互行为、OS 解析等审计发现。
- Phase 3 明确 WPF 切 Go 前先做 JSON 契约 fixture，并定义 `SourceAudit` 以 Go 输出为规范。
- Phase 6 增加 WIP 归属确认和 `task.json` 的 `branch/commit` 更新。
