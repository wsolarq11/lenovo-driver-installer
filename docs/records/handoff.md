# 会话移交

> 本文件写给接手的新会话。只写**现在需要知道的事**：已经落地了什么、还有哪些未闭合、以及哪些坑不要再踩。
> 规范权威不在本文件：分层看 `docs/spec/architecture.md`，编码约定看 `docs/spec/coding/`，不变量看 `docs/spec/invariants.md`。

## 1. 本会话落地了什么

**主题：桌面壳从 PowerShell/WPF 迁移到 mygo 原生 UI，并把 mygo 的完整形态做完。**

九个提交，全部已推送到 `origin/main`，CI 双宿主全绿。

| 提交 | 内容 |
| --- | --- |
| `7baaa0f` | 主体迁移：删 `wpf/` + 入口 ps1 + wpf bat；新建 `internal/gui`（mygo 原生 UI，引擎嵌入同进程）；`-Gui` 单一 UAC 入口；`internal/app/gui.go` 引擎出口门面 |
| `3027d4e` | 清掉引入 mygo 后已不成立的 "stdlib-only" 声明 |
| `8a70fa7` | `install_lenovo_drivers_gui.bat`：GUI 启动器接上共享的陈旧引擎重建规则 |
| `afd5414` | 性能：`resolveRuntime` 网络腿与本地盘点腿并行（5.6s→3.6s）；GUI 两段渲染（首帧 ~1s） |
| `f2bf970` | 阶段 1：托盘（关闭到托盘）、行右键菜单、剪贴板复制 + toast、计划文件只读页（code editor） |
| `0c6e79b` | 阶段 2：`mygo.json`；`locales/zh-Hans.json` 71 条消息全部接管 GUI 字符串；`lenovo-driver://audit?osid=N` 深链 |
| `dc6f27f` | 阶段 3：`mygo build` 产 exe + NSIS 安装器；签名更新档 + manifest；`release.yml`；两份决策记录 |
| `dc1e485` | 全仓整洁：删转发层/死字段/死函数，合并重复的选择策略，catalog 无死 key |
| `82426e8` | `mygo.json` 的更新源仓库名修正为真实的 `wsolarq11/...` |
| `6212ffe` | 第一版交接文档 |
| 本轮 | 真机对拍修复六处缺陷（见 §1.1），已用 `mygo build` 产物在真窗口上逐条验证 |

### 1.1 本轮修掉的缺陷（都是真机可见的，不是风格问题）

| 缺陷 | 后果 | 修法 |
| --- | --- | --- |
| 托盘"检查更新"调用未接线的 nil 闭包 | 点菜单即 panic | `newTray` 改成位置参数，漏一个就是编译错误 |
| 打包 exe 无参启动走进 CLI | `mygo build` 产物是 `-H=windowsgui`（无控制台），快捷方式/安装完成页都以无参启动 → 等一个没人能输入的标准输入，窗口永不出现 | `wantsGUI`：打包构建的无参启动即窗口；带显式参数仍是 CLI；深链参数任何构建都判窗口 |
| 深链在 Windows 以 CLI 启动 | 注册命令是 `"exe" "%1"`，不带 `-Gui` | 同上（深链参数判窗口）+ 参数交给已运行实例 |
| 没有单实例锁 | 重复启动开第二个窗口、第二个托盘、第二个引擎 | `RequestSingleInstanceLock` + `OnSecondInstance` 把已有窗口调到前台 |
| `App.Quit()` 被关闭到托盘拦截 | 托盘"退出"只隐藏窗口，进程留着且没有任何入口回来 | `OnBeforeQuit` 置 `quitting`，关闭处理器据此放行 |
| 日志面板背景透明 | 170 DIP 的日志带在浅色主题下与窗口同色，看不出是面板 | 改 `theme.Surface`，加内边距与 `Clip` |

同轮补的还有：标签早就写着的 `(y)`/`(a)` 快捷键（都先弹确认框）、窗口几何记忆（`StateKey`）、
`OpenPlan` 从 `exec.Command("rundll32")` 改为 `mygo.Shell.OpenPath`（少一条自造的进程边界）。

新增测试锁住这些：`TestInstallShortcutKeysOpenTheConfirmDialog`、`TestWantsGUIDispatch`、
`TestHeaderFitsTheNarrowestWindow`、`TestLogPaneKeepsItsBand`。前两个断言的是"按键→确认框"与
"启动分发判据"，不是渲染细节。

**CI 状态**：run `39986622133`（`82426e8`）与 `39986495524`（`dc1e485`）两个宿主（pwsh 7 /
Windows PowerShell 5.1）均 `success`。
本地门禁 `scripts/verify.ps1`：**58 步全绿**（原 57 步 + 新增的更新检查规则）。

## 1.2 真机验证记录（本轮）

用 `go run .../mygo@v0.6.2 build` 的产物（13.4 MB exe + 4.4 MB NSIS）逐条对拍：

- 无参启动 → 进程存活、`MainWindowHandle != 0`、窗口 1240×760、DPI 144（150% 缩放）、真实驱动数据。
- 第二次启动 → 新进程 exit 0，原 pid 继续运行，进程数仍为 1。
- `lenovo-driver://audit?osid=248` → 打到已运行实例，引擎日志出现 `Driver source : QuickFix (OSID 248)`。
- 截图确认日志面板有独立底色、表头四个控件（下拉框、两个复选框、刷新）都在窗口内。

## 2. 现在的形状

```text
cmd/lenovo-driver          单一入口：-Gui 分发到 internal/gui，否则 CLI
internal/gui               mygo 原生 UI 壳
  app.go                   -Gui/-GuiElevated 分发、UAC 提权、窗口装配、托盘接线
  runner.go                引擎调用（后台 goroutine）+ 两段渲染 + 托盘/更新/回滚弹窗
  state.go                 状态、行投影、安装集目标解析、确认对话框
  view.go                  状态→视图纯函数（表格/页签/日志/对话框/右键菜单）
  tray.go                  托盘图标与菜单
  update.go                更新检查（只能从托盘菜单进）
  deeplink.go              lenovo-driver:// URL 解析
internal/app/gui.go        GUI 用的引擎出口门面（ResolveRuntime/SelectByCodes/RunRollback/...）
locales/                   mygo generate 产出的 catalog 访问器（生成代码，随仓库提交）
mygo.json                  mygo 项目配置（名字/标识/版本/URL scheme/更新源/i18n）
resources/icon.png         mygo build 的应用图标源
```

依赖方向：`cmd → gui → app`。引擎 12 个包**零 mygo 依赖**；mygo 只在 `internal/gui` 和 `locales` 里。

## 3. 立即可用的入口

```bat
install_lenovo_drivers_gui.bat                        :: 开发期 GUI（自动重建 bin\ 引擎）
install_lenovo_drivers.bat                            :: CLI
build\windows-amd64\联想驱动安装器.exe -Gui            :: mygo build 产物（带图标）
go run github.com/egoist/mygo/cmd/mygo@v0.6.2 build -platform windows/amd64 -o build
```

发布：`git tag v0.8.0 && git push --tags` → `release.yml` 构建签名并建 draft release。
**前提**：仓库 secret `MYGO_UPDATER_PRIVATE_KEY` 必须存在，否则只产安装器（不产更新档）。

## 4. 未闭合的事（按优先级）

### 4.1 更新通道还没有真实 release

`mygo.json` 的 `updates.github` 已指向真实仓库，但**一个 release 都还没发**。因此托盘菜单的
"检查更新"在真机上会弹 "检查更新失败"（`Updater.Check` 拉不到 manifest）。这是**有意的诚实
行为**（不假装检查过），不是 bug。要闭合：配置 secret → 打第一个 tag → 等 draft release 出现。

### 4.2 没有 Authenticode 代码签名证书

更新档是 Ed25519 签名的（mygo 自己的信任链），但 exe/安装器本身没有 Authenticode。SmartScreen
会对安装器报警。**对外分发前必须解决**，见 `docs/decisions/distribution-unfrozen.md` §3。

### 4.3 D0 仍未回答

`distribution-frozen.md` 提的问题——"分发后它做哪一件 Vantage/Windows Update 做不到、而目标
用户真正需要的事？"——**至今没有答案**。解冻的理由是"要把壳做全"，不是"问题被解决了"。
新会话不要假装这个问题已经闭合；`distribution-unfrozen.md` §1 明确写了这一点。

### 4.4 gui 包覆盖率（下限 35）

本轮又加了一批 `ui.NewTester` 视图测试，余量比上一轮宽。新增 GUI 逻辑时要同步加视图测试，
否则容易跌破下限。

### 4.5 托盘"退出"与关闭的语义（本轮已修，别退回）

现在的行为：点窗口关闭 → 隐藏到托盘；托盘菜单"退出" → 真退出。两件事必须同时成立：

1. 首次启动窗口**显式 `Show()`**（漏掉它 + 关闭时隐藏，会让 `mygo build` 产物首次启动不可见）。
2. **`OnBeforeQuit` 置 `quitting`**，关闭处理器据此放行。只做第 1 条不够：`App.Quit()` 也会走
   `OnClose`，被"关闭即隐藏"拦下，结果是托盘退出只隐藏窗口、进程留着且没有任何入口回来。

## 5. 不要再踩的坑

### 5.1 `mygo build` 的 `main` 必须是模块路径

`mygo.json` 的 `"main": "cmd/lenovo-driver"` 会被 Go 当成**标准库候选**（`cmd/` 与 GOROOT 的
`cmd/` 冲突），报 `package cmd/lenovo-driver is not in std`，即使 `go list ./cmd/lenovo-driver`
正常。必须写 `"main": "lenovo-driver/cmd/lenovo-driver"`。

### 5.2 `locales/` 必须进 gate 的扫描根

`internal/gate` 的 `Load` 原本只走 `internal/` 和 `cmd/`。`internal/gui` import
`lenovo-driver/locales` 后，type-checker 报 `no source for in-module import`。已加入 `locales`
（见 `gate.go` 的 `for _, top := range []string{...}`）。新增顶层 Go 包时同样要加。

### 5.3 PowerShell 脚本不能带非 ASCII

Windows PowerShell 5.1 按 ANSI 码页解码 BOM-less UTF-8。截图/诊断用的临时 ps1 带中文，会
报"字符串缺少终止符"。临时脚本一律用纯 ASCII（或加 BOM）。

### 5.4 `ui.NewTester` 的 ListState 不能跨 Tester 复用

一个 `ui.ListState` 被两个 List 显示会 panic（`a ListState shown by two Lists`）。写视图测试
时，切换页签要新建 state，不能复用已渲染过的那个。

### 5.5 GUI 里的中文必须进 catalog

门禁没强制这条，但 `dc1e485` 已把最后一批硬编码中文字符串（OS 下拉标签、机器标题、回滚
对话框详情）搬进 catalog。新增字符串请加到 `locales/zh-Hans.json` 并重新
`go run github.com/egoist/mygo/cmd/mygo@v0.6.2 generate`（生成的 `catalog_gen.go` 要一起提交）。

### 5.6 更新检查不许变成定时轮询

门禁步骤 `the update check is operator-initiated, never timed` 会挡：`Updater.Check` 只能从
`checkForUpdates` 调用，且 `internal/gui` 生产文件不得出现 `NewTicker`/`NewTimer`/`Tick`/`AfterFunc`。
这是"零隐藏联网"不变量在分发解冻后的唯一保底。

### 5.7 git 二进制有坑

`/ucrt64/bin/git`（PATH 里的默认）连不上 github.com（`Could not connect ... after 33 ms`），
而 `/c/Program Files/Git/cmd/git.exe` 正常。push 失败时先换这个二进制再怀疑网络。

### 5.8 删掉的东西别再复活

`wpf/`、`lenovo_driver_wpf.ps1`、`install_lenovo_drivers_wpf.bat`、`scripts/fix-bom.ps1` 已被
门禁步骤 `Legacy PowerShell files removed` 钉死为"不得存在"。理由：第二个运行时壳会让门禁
看不见它。

### 5.9 打包构建的 exe 没有控制台

`mygo build` 给 Windows 产物加 `-H=windowsgui`。任何"无参运行它"的入口——NSIS 快捷方式、安装
完成页、深链——都不会有控制台可读可写，所以判据必须落在 `wantsGUI` 里，不能靠"让它跑 CLI 再
抱怨"。反过来，`go build` 出来的 `bin\lenovo-driver.exe` **不带**版本资源，`App.IsPackaged()`
为 false，因此本地开发和门禁走的仍是 CLI 路径，两条路互不干扰。

### 5.10 托盘动作是结构体字面量时，漏字段不会报错

本轮第一个缺陷就是这么来的：`trayActions{...}` 少写 `CheckForUp`，编译通过、点菜单 nil panic。
现在 `newTray` 收位置参数，漏一个就是编译错误。以后加托盘项请继续用位置参数，不要退回具名
字面量。

### 5.11 看 GUI 要看真窗口，不要只看 `ui.NewTester`

`ui.NewTester` 不会应用系统 DPI 缩放。150% 缩放下 1240 物理像素只有 ~827 DIP；布局问题
（表头被挤出窗口、面板背景看不出）只有真窗口截图能发现。截图用 `PrintWindow`，尺寸要乘以
`GetDpiForWindow / 96`，否则截出来是被裁的左上角。

## 6. 需要读的文档

| 想知道 | 读 |
| --- | --- |
| 为什么解冻分发、代价是什么 | `docs/decisions/distribution-unfrozen.md` |
| 为什么不用 frontend/audio/plugins/launchers | `docs/decisions/mygo-modules-declined.md` |
| 原来的冻结结论（历史） | `docs/decisions/distribution-frozen.md` |
| 分层与数据流 | `docs/spec/architecture.md` |
| 六条不变量及其门禁强制点 | `docs/spec/invariants.md` |
| 编码约定 | `docs/spec/coding/` |
| 怎么开发/跑门禁 | `docs/howto/develop.md` |
| 怎么用 | `docs/howto/run.md` |
