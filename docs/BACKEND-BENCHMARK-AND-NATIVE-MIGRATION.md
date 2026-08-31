# Lenovo Driver Installer — 后端对标与“更原生/更底层/更无缝”迁移调研报告

> 范围：**后端优先**（用户明确“先考虑后端，暂不考虑 UI”）。
> 口径：本报告只讨论驱动发现/比对/下载/安装/审计这条后端链路，不讨论桌面外壳。
> 阅读对象：本项目维护者。产出方向是“决策参考 + 可执行改造路径”，本报告本身不改代码。

---

## 0. 结论速览（TL;DR）

1. **市面/开源没有“现成更强、可直接取代”的完整实现**，但存在若干**可借鉴的更强后端范式**：
   - 开源侧：**Snappy Driver Installer (SDI)** 是最接近的成熟开源驱动管理器，但其强项在“索引 + 离线驱动包 + 量级覆盖”，不在“底层原生”;它的安装引擎反而依赖 `pnputil`/`DPInst` 等命令行，与我们现在同一层。
   - 官方侧：**Lenovo System Update / Commercial Vantage / Update Retriever(Thin Installer)** 才真正拥有“更底层、与 Lenovo 官方 + Windows Update 打通”的后端;这就是我们最值得吸收的“无缝”方向。
   - 底层侧：**Windows SetupAPI / SetupDi / DriverStore / WUA(Windows Update Agent)** 才是“更原生”的正确打开方式;现在本项目把这些能力几乎全部通过 `powershell.exe` / `pnputil` / `msiexec` 子进程间接调用，正是要删的“缺点”。

2. **本项目后端当前最大的结构性缺点**（对应“删缺点”）：
   - **整台 inventory 全部 shell 到 PowerShell**(`Get-CimInstance`/`Get-WmiObject`/`Get-PnpDevice*`+JSON)——每个读取都拉起一个进程，慢、脆、易被策略/语法坑、无法 native 复用驱动状态。
   - **安装通道全是子进程外壳**(`msiexec`/`pnputil`/`expand`/`taskkill` + vendored EXE 的 `is-*.tmp` 启发式嗅探)，没有走 SetupAPI 的原生安装接口，也没有把 DriverStore 作为一等公民管理。
   - **没有与 Windows Update / Microsoft Update Catalog 打通**, `Local newer`/跨 OS 的判定完全依赖“仅官方网页那份列表”, 而非查询系统真实驱动目录与 WU 目录。
   - **运行时语言是 Go**,生态里 SetupAPI 绑定不齐、低频、维护存疑;这正是“更底层更强运行时”要回答的问题。

3. **集大成方向（后端优先）一句话**：保留 Go 引擎的价值（确定性、纯函数、离线可测、SHA/MD5 完整性、审计 WAL），**把“系统交互层”下沉替换为原生 Windows API**,并且选择性地把 Windows 侧耦合拆成可替换接口，声称用更底层语言实现的只是“原生系统桥（Rust/C#）”，Go 引擎继续当业务核心。
   - **挖到底的落点**（详见 §5）：最稳的“原生”不是换语言，而是**用微软 SDK 的原生 API**：枚举用 **CfgMgr32/SetupDi + `DEVPKEY_*`**，安装用 **`SetupCopyOEMInf` → `SetupDiCallClassInstaller(DIF_INSTALLDEVICE)`**（或一步封装 **`DiInstallDriver` / `UpdateDriverForPlugAndPlayDevices`**），前置做 **签名/.cat 校验**，并保留“原生失败 → 降级 `pnputil`”这条兜底链。这是“挖到底 + 最稳定可靠”的那条线。

---

## 1. 市面对标清单（谁强在哪）

以下按“与后端相关度”排序。每项给出：定位、后端强点、与本项目的关系、可借鉴点。

### 1.1 联想官方后端：System Update / Vantage / Update Suite（最值得借鉴的“无缝”）

来源：
- Lenovo System Update 部署指南(DG-SystemUpdateSuite)与 ThinkVantage 系列说明
  <https://download.lenovo.com/cdrt/docs/DG-SystemUpdateSuite.pdf>
  <https://docs.lenovocdrt.com/guides/sus/su_dg/su_dg_ch1/>
- System Update(ThinkVantage)百科说明 <https://baike.baidu.com/item/system%20update/10637480>
- Lenovo System Update 官方(已并入 Vantage) <https://www.lenovo.com/us/en/software/lenovo-system-update>
- Lenovo Vantage 驱动管理实操 <https://www.dnqdw.com/news/1776269100000>

要点：
- **Update Retriever / Thin Installer(Update Suite 的离线/企业通道)** 是“包目录 + 离线仓库 + 客户端瘦装”的权威后端模型：机器→目录匹配→下载驱动包→静默安装。它天然支持多 OS 列表、依赖链、BIOS/固件、静默参数化。
- 官方也**与 Windows Update 生态打通**,且在联想官方、大陆区 QuickFix/驱动页并提供相同 `SearchForXbb` 接口（本项目已用）。
- **判断**：官方那套是“企业级全链路 + 在线/离线仓库”,超出当前 Go 引擎的范围。我们要的不是“复刻整个 Update Server”，而是借鉴其**“清单 - 下载 - 静默安装 - 状态回写”分离、支持离线驱动目录、可与 WU 对齐”**这几个后端能力。

### 1.2 开源驱动管理器：Snappy Driver Installer(SDI)

来源：
- SDI 架构总览(DeepWiki) <https://deepwiki.com/gtumanyan/SDI/3-architecture-overview>
- SDI 简介(DeepWiki) <https://deepwiki.com/gtumanyan/SDI/1-overview>
- Windows Update vs SDIO(离线驱动包) <https://windowsforum.com/windows-news.4/windows-update-vs-sdio-restore-vendor-features-with-offline-driver-packs.383894/>

要点：
- **最强项 = 自建驱动索引 + 海量离线驱动包(DriverPacks)**,可离线/本地仓库安装——这是本项目完全没有的能力（本项目只能在线拉联想列表）。
- **弱项/相关性**：SDI 的安装动作仍落回 OS 层工具（`pnputil`/`SetupDi` 或内部安装器），与本项目是在“同一层”,并无更底层优势；SDI 的索引口径（硬件 ID 驱动、Vendor/Device/Subsys）与联想“驱动代码列表”并不一致，迁移成本高。
- **可借鉴**：① 维护“本地驱动仓库/离线包”的能力（本项目缺）；② “驱动序号驱动切片”的后端分层。

### 1.3 开源驱动“仓库查看/管理”：DriverStore Explorer (RAPR)、各类 DevCon/DPInst 替代

- DPInst / pnputil / DevCon / DISM 的驱动管理对比（可操作性）：
  <https://www.cnblogs.com/suv789/p/18606185>
- 调用 SetupAPI 官方路线：<https://learn.microsoft.com/windows-hardware/drivers/install/calling-setupapi-functions>

         **要点**：底层“添加/卸载/比对驱动”无非是 SetupAPI（编程）或 pnputil/DISM/DPInst/DevCon（命令）。`pnputil` 在 Win10 已基本取代 DevCon/DPInst。**如果我们要“更原生”，就是把这些命令调用替换为直接的 SetupAPI/DriverStore API 调用**，而不是换一个命令。

### 1.4 现代/开源“原生驱动安装库”（直接相关，属于“集大成”原材料）

这是本次调研最有价值的部分——**别人已经做好了的“更原生绑定”**，可以直接拿来集大成，而不是重新从零封装：

| 库 | 语言 | 定位 | 链接 |
|---|---|---|---|
| `wdi-rs`（Windows Driver Installer for Rust） | Rust | 封装 SetupAPI 的驱动枚举/安装/卸载 | <https://docs.rs/wdi-rs/0.1.1/wdi_rs/> |
| `gentlemanautomaton/windevice/setupapi` | Go | Go 侧 SetupAPI 绑定（`DrvInfoData`、设备枚举、驱动信息）、不需要 CGO | <https://pkg.go.dev/github.com/gentlemanautomaton/windevice@v0.0.0-2025.../setupapi> |
| Go `golang.org/x/sys/windows` | Go | 原生 Windows 系统调用源 |（标准） |

**结论**：如果坚持 Go，`windevice/setupapi` 这类库能消掉“shell 到 PowerShell”这一大块；如果想更底层，`wdi-rs`(Rust) 是比 Go 更贴合 Windows SetupAPI 的选择。

### 1.5 底层 OS 能力盘点（“更原生/更底层”的完整语法）

| 能力 | 官方入口 | 本项目现状 | 更原生路径 |
|---|---|---|---|
| 设备状态枚举 | `SetupDiGetClassDevs` + `SetupDiEnumDeviceInfo` / CIM `Win32_PnPEntity` | shell 到 PowerShell WMI | SetupAPI 或 `CM_*`(CfgMgr32) |
| 驱动版本/日期/provider/inf | `DEVPKEY_Device_DriverVersion` 等（`SetupDiGetDeviceProperty`） | `Get-PnpDeviceProperty`(PS) | SetupAPI `SetupDiGetDeviceProperty` |
| 安装 INF | `pnputil /add-driver`；原生=`SetupCopyOEMInf` 或 `SetupDiCallClassInstaller DIF_INSTALLDEVICE` | `pnputil` 子进程 | 原生 API/库 |
| 卸载/回滚 | `pnputil /delete-driver` / `SetupDiCallClassInstaller DIF_REMOVE` | 无 | 原生 |
| 驱动目录查找 | DriverStore(`C:\Windows\System32\DriverStore\FileRepository`) | 读历史/不管理 | `SetupDiGetRegisteredDeviceInfo`+目录枚举/DISM |
| 系统更新驱动 | Windows Update Agent (WUA) API `IUpdateSearcher`+`UpdateCategory=Driver` | ❌ 无 | WUA(COM) |
| 离线/在线驱动库 | DISM `/Add-Driver`、WU 驱动目录 | ❌ 无离线 | DISM/WUA |
| 设备状态(install 前) | CfgMgr32 `CM_*` | 无 | 原生 |

## 2. 本项目后端现状的 “优点” 与 “缺点”（分类）

> 只讲后端，不聊 UI。

### 2.1 优点（应集中/保留）

1. **确定性引擎 + 纯逻辑分层**：`internal/api|compare|audit|plan|model` 无副作用、可离线单测;这与 Trellis/AGENTS 的“可复现验证”一致。
2. **完整性与审计**：`SHA-256` companion + 官方 `MD5` + 文件大小 + `setupapi.*.log` 来源审计 + `history.csv` 前后版本回写——这是绝大多数同类（含 SDI/普通官网工具）**没有**的审计强度。
3. **平台兼容宽度**：`-TargetOS`（看别的 OS 列表）、`-LatestAcrossOS`（跨 OS 合并非截取新版）——官方只给“当前系统一键”,而我们把“另一条 OS 官方列表”变成可检查/可审计的面。
4. **安装器回退链**：EXE 静默 → 提取 INF/内部安装器 → 交互运行;MSI/pnputil/zip/cab 各走一套。这是“对付 Lenovo 专有包”(Inno 风格)的务实经验。
5. **与官方 API 双向主备**(QuickFix `SearchForXbb` + 官网 `drive_listnew`)+ 403 URL 刷新。

### 2.2 缺点（应删/应改）

> **按“对后端的伤害程度”排序。**

- **D1. “系统交互 = 每次 shell 到 PowerShell”**（`internal/inventory/windows.go` 整页）。
  - 每个 `GetMachineInfo/GetOSInfo/GetLocalDeviceSnapshot/GetInstalledApps/GetDeviceDriverVersions/GetDeviceEvidence/IsAdministrator` 都 `exec` 一个 `powershell.exe -Command ... | ConvertTo-Json`。
  - 后果：慢（起进程序列化）、脆（PowerShell 版本/执行策略/引号转义/策略拦截）、不可测（单测只能靠注入 fake）、且把“系统事实”从原生 API 变成“PS 快照快照”。
  - **这是“不原生”的最大单一来源**。

- **D2. 安装通道全是“子进程外壳 + 启发式”，没有原生 DriverStore/SetupAPI。**
  - `.inf`→`pnputil`;`.msi`→`msiexec`;`.cab`→`expand`;`.zip`→自解压;EXE→跑厂商静默安装器，且 `ExtractedDriverFallback` 用 `is-*.tmp` 目录 + “仅 NVIDIA + 含 `Display.Driver`” 启发式来猜 INF。
  - 后果：结果依赖外部工具版本和启发式；没有“重构为 DriverStore 原生操作”的路径；`pnputil` 退出码 1 被硬写成“成功需重启”（容易误判）。

- **D3. 没有 Windows Update / MS Catalog / DISM 的驱动通道。**
  - 一切“最新版”只来自 Lenovo 那张 OS 表;`LocalNewer` 只能“审计标注”却不能溯源到 WU/目录/WU 的版本。
  - 无法利用“预打包/离线驱动仓库”的能力（SDI 那里唯一比本项目强的点）。

- **D4. 运行时语言（Go）在“深度 Windows 原生”上生态薄弱 / 低频。**
  - 上面 `wdi-rs`(Rust) 比 Go 的 SetupAPI 绑定成熟；`windevice/setupapi` 虽能用，但版本低频、覆盖面有限。
  - 这会成为“要更底层”时的真实约束：**系统原生层放 Rust/C#，业务核心放 Go**,才能既保住 Go 的确定性又拿到原生能力。

- **D5. 版本比较以字符串/语义为主，缺“硬件枚举 ↔ 驱动匹配”的通用模型。**
  - 本项目按“联想驱动代码”列表做——这与 SDI 的“硬件 ID/PnP 驱动”模型不兼容，也难平移去接第三方/WU。

### 2.3 中立/边界项（不一定是缺点）

- `Task` 超时+`taskkill`：对“厂商静默安装器”合理；但真原生(SetupDiCallClassInstaller)不会有“杀进程树”这一层。
- `read` WMI/registry：部分能直接改 registry（如卸载注册表、`HKLM\SOFTWARE\...\Uninstall`），省去 PS。
- 500 行/包约束、审计日志等：全是合规优点，保留。

---

## 3. “集大成”建议（后端优先、按优先级排序）

> 原则：**保留 Go 确定性引擎**，把“系统交互层”下沉为原生；不推翻架构，做“外科手术式”替换。每一步都可独立回滚（对住持 `verify.ps1`）。

### P0(最高价值/最贴合“更原生”)
**把 `internal/inventory` 里全部 PowerShell 调用替换为原生 Windows 调用。**

- 方案 A（最小改动、最快见效）：用 `golang.org/x/sys/windows` 直接读 **注册表/Registry**（Uninstall 路径、`Software\Microsoft\Windows NT\CurrentVersion` 等）+ **SetupAPI 绑定**（`windevice/setupapi`,`SetupDiGetClassDevs`→枚举→`SetupDiGetDeviceProperty` 拿 `DEVPKEY_Device_DriverVersion/DriverDate/DriverInfPath/DriverProvider`）。
  - 预期收益：消灭 `Get-CimInstance/Get-WmiObject/Get-PnpDevice*` 的 PS 进程；批量枚举在进程内完成，速度快一个量级，失败更可控；可直接复用 `DEVPKEY_*`，与官方一致。
- 方案 B（更底层、把“原生层”独立成组件）：把“Windows 事实查询层/Driver API 层”抽成一个小而密闭的 C/Rust/或 C# DLL（`native/`），Go 侧只调用它（薄接口）。可选 `wdi-rs`(Rust) 或 COM/WIN32(C#)。**这条路为后续“无缝”铺路**，但一次性成本更高。

### P1(对 D2，让“安装/卸载”原生化)
**把装/卸改为原生驱动安装语义**：
  - INF → 首选 `SetupDiCopyOEMInf` / `SetupDiCallClassInstaller(DIF_INSTALLDEVICE)`，而不是 shell 到 `pnputil`；
  - 卸载/回滚 → `pnputil /delete-driver` 或对应 `SetupDiCallClassInstaller(DIF_REMOVE)`；
  - 若坚持命令行，则**收敛为单一 `pnputil`(Win10 唯一权威)** 并显式处理 `/add-driver` 的退出码而不是把 1 一律当“需重启”。
  - EXE 包的 `ExtractedDriverFallback` 启发式改为“读取安装日志确定 INF,再走原生安装”，不再用 “NVIDIA + is-*.tmp” 猜测。

### P2(对应 D3，集大成“无缝”)
- **接入 Windows Update / 驱动目录**：(后续)用 WUA API(`IUpdateSearcher`，`UpdateCategory=Driver`)做“当前系统推荐/最新驱动”的交叉比对与安装回退。
- **离线驱动仓库**（吸收 SDI 唯一能补的强项）：做“驱动目录 → 可选离线仓库（manifest+包）”模型，让 `-TargetOS`/`-LatestAcrossOS` 可与离线清单对齐。

### P3(对应 D4，语言/运行时的策略)
- **短期不要整体换语言**；Go 引擎是资产。需要更底层时，先做 P0-B 的“原生层抽离”，把 Windows 专属调用隔离到 Rust/C# 组件；将引擎保持 go 可测。若未来决定迁到 Rust，也只需重写已隔离的“系统层”，业务逻辑保留或重放。

### 里程碑建议（每条都写`verify.ps1`自检）
1. 加 `go` 的 Windows 常量测试(fake registry/CfgMgr 不可行，则做“输入→JSON”半自动测试)。
2. **原生枚举化**后跑一次真机 `-CurrentOS` 冒烟做逐条唯一/最新模式的等价性校验。
3. **原生安装**后用降级 `pnputil` 对照（先 Fail 回退到旧实现）验证退出码与时延。
4. 备选：把 WU/离线仓库做成独立 feature 开关，蓝绿切换。

---

## 4. 结论与后续动作

- **不要** 去寻找“某一个更强的实现”来整体替换——**没有**对手；真正强的是**把原生能力收进后端**。
- **立即做（P0）**：inventory 的 PS 打字换成 `golang.org/x/sys/windows` + SetupAPI 绑定，收益最直接、风险最小、完全配合现有 Go 测试框架。
- **第二步**：装的原生化（设 SetupDi 语义），**第三步**：WU/离线仓库的“无缝通道”。
- 语言层面：**短期保持 Go**；只有在需要真正“更底层”时才把已隔离的「原生系统层」迁到 Rust（可先读 `wdi-rs`/`windevice` 判断够不够用），业务核心仍留在 Go 保证可测性。

> 本报告为“后端对标/改造”研究交付；下一步若定要动手，应新建一个 Trellis 复杂任务（prd+design+implement），按 P0→P3 分阶段实施，并逐阶段保留可回滚基线。

---

## 5. 挖到最底层：原生线路全链（深度篇）

> 这一节回答“更原生/更底层/更稳定可靠”的 **具体是谁**——把微软官方文档里真正的最底层 API 一条条挖出来、标清层次与调用前置条件，并给出在本项目里怎么走才能“最稳定可靠”。

### 5.1 微软官方“简化驱动安装”的原生三原语（这是最底层）

微软自己的《Functions that Simplify Driver Installation》明确定义了三组可以直接编程调用的底层函数，正是 `pnputil` / `DPInst` 底层所做的事：

来源《SetupAPI Functions that Simplify Driver Installation / Functions that Simplify Driver Installation》:
<https://learn.microsoft.com/windows-hardware/drivers/install/functions-that-simplify-driver-installation>

| 原生函数 | 头文件/模块 | 做什么 | 对应命令 | 最合适场景 |
|---|---|---|---|---|
| **`DiInstallDriver`** | `newdev.h` / `newdev.dll` | “预安装(拷入 DriverStore)+ 为所有匹配且需要更新的设备安装”，一步 | 类似 `pnputil /add-driver /install` + 自动设备安装 | 拿着一个驱动包文件、想“装它” |
| **`UpdateDriverForPlugAndPlayDevices`** | `newdev.h` / `newdev.dll` | 只针对**指定 HardwareID** 的设备更新驱动（可指定强制 vs 仅当更优） | 类似 `devcon update` / `pnputil` 按设备更新 | 想只对**某台设备**(按设备实例/硬件 ID)升级 |
| **`SetupCopyOEMInf`** | `setupapi.h` / `setupapi.dll` | 最底层：把这一个 INF（及配套 `*.sys`/DPInst 声明的文件）**拷入 DriverStore 的 OEM 目录**并登记 | `pnputil /add-driver`(预装态) | 蓝队/科研/在不触发设备重排时“只进 DriverStore” |

“真正装上设备”那一步（在 INF 已进 DriverStore 后）是：
**`SetupDiCallClassInstaller(DIF_INSTALLDEVICE)`**（`setupapi.h`）。
它驱动 class installer 完成把驱动密钥绑定到设备的完整 PnP 流程——这是 `SetupCopyOEMInf` 之后、设备真正“激活”的那一环：
<https://learn.microsoft.com/windows-hardware/drivers/install/dif-installdevice>

**微软推荐的最稳定组合（纯原生、一条直线）**：
```text
  1. SetupCopyOEMInf(inf, ...)            → 拷入 DriverStore、登记 OEM INF
  2. SetupDiCallClassInstaller(DIF_INSTALLDEVICE [, DIF_REGISTERDEVICE...])  → 绑定并激活设备
  ── 或直接在拿到 INF 时用更高层封装：DiInstallDriver(...)（内部就是上述两步）
```
官方还提供“最省事”的一套：`SetupDiInstallDriver`…不过 `DiInstallDriver` 就是它的“自动设备安装”上层，已能覆盖大多数纯 INF 场景。

> **微软同样专门澄清了 CfgMgr32 vs SetupAPI**：设备/驱动枚举的最新底层面是 **CfgMgr32 (`CM_*`)/PnP 配置管理器**，并给出官方《Porting from SetupAPI to CfgMgr32》迁移指南。换句话说，最“原生”的枚举不是 SetupDi 也不是 WMI，更不是 PowerShell——而是 `CfgMgr32` 的 `CM_*` API 返回的 DEVINST/设备信息。
> - 枚举已装设备：`SetupDiGetClassDevs`（SetupAPI）或 `CM_Get_Device_Interface_List` / `CM_Locate_DevNode` 系列（CfgMgr32）。
> - 读驱动版本/日期/Provider/INF 路径：`SetupDiGetDeviceProperty` + `DEVPKEY_Device_DriverVersion`/`_DriverDate`/`_DriverInfPath`/`_DriverProvider`（这些正是你现在 `Get-PnpDeviceProperty` 在 PowerShell 里间接读的同一组值）。

来源：
- 设备枚举(MS 官方) <https://learn.microsoft.com/windows-hardware/drivers/install/enumerating-installed-devices>
- SetupAPI → CfgMgr32 迁移 <https://learn.microsoft.com/windows-hardware/drivers/install/porting-from-setupapi-to-cfgmgr32>
- DEVPKEY 用法示例 <https://stackoverflow.com/questions/3438366/setupdigetdeviceproperty-usage-example>

### 5.2 签名/证书链：能不能装上，是先决的硬门槛

最底层链里，**「敢不敢装」不是 API 能决定，而是签名**。微软这层文档《PnP Device Installation Signing Requirements(Win10/11)&Signature Categories》：
- 驱动必须带有效 **签名目录 (.cat) + 制造商标记（微软 WHQL / 交叉签署）**，64 位 Win10/11 默认拒绝未签名驱动的安装。
- 若驱动未签名，`SetupCopyOEMInf`/`DiInstallDriver` 会报 **0x800F0216** 或 **0x00000002 / 拒绝**；这才是一张 Win11 25H2 “DRIVERS hive 为空、注册失败 0x00000002”的坑。

**对本项目的意义**：现在 `pnputil` 已经把“签名失败”翻译成了“退出码”；改成原生 API 后，**必须自己处理这条链**（先 verify `.cat`，再 install）。这正是“更原生 ≠ 更简单”的地方——你要补签名/证书的前置判断，`pnputil` 帮你做了，直接原生就要自己接。

来源：
- PnP 签名要求 <https://learn.microsoft.com/windows-hardware/drivers/install/pnp-device-installation-signing-requirements--windows-vista-and-later->
- 签名分类 <https://learn.microsoft.com/windows-hardware/drivers/install/signature-categories-and-driver-installation>

### 5.3 Windows Update 驱动的无缝通道（最“无缝”的那条）

微软的驱动分发是和你项目平行的另一条**完全免费**通道：企业/开发者把驱动发布到 **Windows Update 驱动目录**，系统靠 WU(Pnp) 自动安装。接入有两条原生路：
- **WUA (Windows Update Agent) API**【COM，`IUpdateSearcher` → `ISearchResult` → `IUpdateDownloader` → `IUpdateInstaller`】，`UpdateCategory=Driver`，可“我要不在 25.x 推送”交叉比对/安装；
- 或**只要模型/驱动已进 WU**，你甚至可以只读微软驱动目录（Driver catalog）。
- 微软给出 **“Driver distribution rule”**（哪些进自动/可选、型号 rank）详见《Understanding Windows Update rules for driver distribution》.
来源：
- WU 驱动分发规则 <https://learn.microsoft.com/windows-hardware/drivers/dashboard/understanding-windows-update-automatic-and-optional-rules-for-driver-distribution>
- WUA 使用示例(搜索/下载/安装) <https://jpdscore.github.io/blog/windowssdk/wuaapi-install-updates/>
- 排除 WU 驱动 CSP(证明该通道真实存在) <https://learn.microsoft.com/windows/client-management/mdm/policy-csp-update>

**注意**：联想大陆区驱动不是都发 WHQL/WU 目录（很多是自定义 Inno EXE），所以 WU 通道只能作为“可选交叉源/补充”，取代不了目前官方列表。

### 5.4 现存“最底层”绑定库（到底封到哪一层 + 稳定度）

| 库 | 底层深度 | 稳定性/成熟度 | 结论对本项目 |
|---|---|---|---|
| **`libwdi`（pbatard/Zadig 同作者）** | 直接 `SetupAPI`/`newdev` 的 C 库，被 Zadig 大量使用，发布远超十年 | 高：商用/USB 驱动安装长期用，签名/枚举/安装一条龙 | **最稳定、最该集大成的候选**（C/Rust 层用它） |
| **`wdi-rs`(Rust)** | 封装 `libwdi`(SetupAPI) 的 Rust 绑定 | 中：功能全、维护起来一般 | 想“更底层+Rust”时用它 |
| **`gentlemanautomaton/windevice/setupapi`(Go)** | 包装 `setupapi.dll`/`newdev.dll` 绑定 | 中低：低频 | Go 里先用的折中 |
| **微软 SDK/WDK 原生 API**（setupapi.newdev.CfgMgr32 直接调用） | 最底、稳定、无第三方依赖 | 最高（官方） | “最稳最可靠”路线 = 直接用官方这些原生 API，别用包 |

来源：
- libwdi/Zadig 主工程 <https://github.com/pbatard/libwdi>
- wdi-rs <https://docs.rs/wdi-rs/0.1.1/wdi_rs/>

> 稳定性第一原则：**用微软原生 SDK 的 setupapi/newdev/CfgMgr32 API（P/Invoke 或用 C/Rust 薄壳）\> 用成熟库 \> 用 shell 到命令行工具 \> 用 PowerShell shell**。你现在全卡在最下面的 PowerShell 层，往上走每一步都是“更原生”，而最顶的都是别人写好、动得多的。

### 5.5 在本项目后端“最稳定可靠”的运行线路

结合上面，给本项目后端给出**一条“最原生、最可靠”落地链路**（后半段才触发安装，前半段全是确定性的比对）：

```
比对/发现（只读，纯原生，每次进程零开销）
  CfgMgr32/SetupDi 枚举已装设备 + 读 DEVPKEY_{DriverVersion,DriverDate,DriverInfPath}
  + 读注册表 Uninstall 路径（软件型驱动）
  + 官方 Lenovo 列表比对（现有 api 包不变）
        ↓ 用户选定
下载（现有 Go download 不变；保留 SHA-256/MD5）
        ↓
装（切到原生，唯一一次触碰 DriverStore）
  a. INF/zip/cab 内 INF  → SetupCopyOEMInf + setupDiCallClassInstaller(DIF_INSTALLDEVICE)
     （或一步：DiInstallDriver / UpdateDriverForPlugAndPlayDevices 按目标取）
  b. 签名校验前置：fail 时如实报“签名/证书/0x00000002”，不做静默
  c. MSI/.EXE(Inno) 仍是厂商安装器 → 无法原生替换（见 5.6 边界）
        ↓
后端结果写入 history.csv（前后版本），复用现有 WAL 审计
```

- **UAC/权限**：原生 API 需要 elevate 进程一次。建议保持“首次 elevation 后，后续装/下都在这一个 elevated 进程里原生化”，不要再像现在 每 query 都在 elevation 权重里拉 PowerShell。
- **稳定性兜底(降级链)**：原生失败 → 主动回落到“命令行 `pnputil`” → 都失败再报错；这样既保底线又能对主控签名判断。可灰度（环境变量/flag）切换。

### 5.6 硬边界（诚实说明，避免“过度原生”掉陷阱）

- **EXE(Inno/DPInst)–型驱动**：那不 INF/纯驱动，是“安装器”。世界上不存在“原生 API 跑一个 Inno 安装 EXE”这件事——`SetupDi*` 只能装 INF 驱动的设备，不能替你跑厂商的 EXE。所以 **对 EXE 包，“最原生”= 务实处理：先跑 silent installer；失败再从它解包的临时目录(如 `is-*.tmp`)里提取 INF 交给原生 INF 流程**，这已是能做到的最稳。
- **不要为了“原生”而丢弃** 你现在已有的确定性规范（纯函数、SHA-256、审计 WAL）——它们和“原生”不冲突，且恰是你优于那批官方工具的“爽”点。
- 原生 INF 安装会**真正触碰 DriverStore / 驱动注册表**，是不可逆的系统级动作：必须保持“默认不干、用户确认才干”的当前半自动约定（这点欣赏你的现状）。

> **最终最稳可靠线路一句话**：**PoC 层面改造内部到“CfgMgr/SetupDi 枚举 + (SetupCopyOEMInf → SetupDiCallClassInstaller, 或 DiInstallDriver) 安装 + newdev 签名前置校验 + 降级 pnputil + 保留审计/完整当量”**,Go 当外壳、原生 API 当动作——这就是“挖到底”且“最稳定可靠”的那条,不是换语言。

---

## 6. 实现状态（全部原生化，只留原生线路）

按用户决策完成：**Windows Update 不碰**；**运行时代码只留原生线路**，不再 spawn PowerShell；设备/机器/OS/应用/软件快照全部走 SetupAPI/CfgMgr32/注册表/固件表；安装走 `DiInstallDriverW`，失败时使用系统原生 `pnputil` 兜底。

**改动文件**
- `internal/inventory/native_windows.go`：SetupAPI 枚举 + CfgMgr32 `CM_Get_Device_IDW`（主路径）+ 驱动类注册表读取。
- `internal/inventory/native_full_windows.go`：原生设备快照、机器/OS 信息、已装应用、软件快照、文件版本、提权检查。
- `internal/inventory/smbios_windows.go`：`GetSystemFirmwareTable` 解析（本机固件表未含标准 Type1，serial 保留注册表/空值边界）。
- `internal/inventory/windows.go`：仅保留类型、`normalizeOSInfo` 与兼容常量；运行时无 PowerShell。
- `internal/install/native_windows.go`：`DiInstallDriverW`；`install.go` 原生 INF 优先、`pnputil` 兜底。
- `internal/app/native_elevate_windows.go`：原生 `ShellExecuteExW` UAC 提权，删除 PowerShell 提权路径。
- `internal/inventory/windows_legacy_ps.go`：**仅 `-tags legacyps` 编译**的 PowerShell oracle，用于真机等价性验证，不进入运行产物。
- 测试：`native_windows_test.go`、`native_full_test.go`、`native_smoke_test.go`、`native_equivalence_smoke_test.go`、install 原生测试。
- `README.md`、`docs/TECHNICAL.md`：同步“只留原生”架构与 smoke 命令。

**验证（全部实测）**
- 离线：`scripts/verify.ps1` 14/14 PASS（build/vet/gofmt/test/coverage/hygiene）。
- 真机原生稳定性：`LENOVO_NATIVE_SMOKE=1 go test ./internal/inventory/ -run TestNativeSmoke -count=5` 5/5 PASS；设备 159 台、Lenovo Fn `2.0.0.25` 等真实属性正确。
- 真机等价性：`LENOVO_NATIVE_EQUIV_SMOKE=1 go test -tags legacyps ./internal/inventory/ -run TestNativePSEquivalenceSmoke -v` PASS：设备 159/159、model 一致、应用版本 0 mismatch、Fn 服务版本 `2.0.0.25` 一致。
- 原生安装 API 通路：`LENOVO_NATIVE_INSTALL_SMOKE=1 go test ./internal/install/ -run TestNativeInstallSmoke -count=3` 3/3 PASS。

**稳定性/边界说明**
- 初版 `SetupDiGetDeviceInstanceIdW` 在本机不稳定，切换 CfgMgr32 后稳定。
- SMBIOS serial 已通过 `GetSystemFirmwareTable` 原生解析并实测等于 `PF2SBWJA`；此前解析器走错导致 serial 为空，修正后与 PowerShell 一致。
- 原生安装只覆盖 INF 包；EXE/MSI 仍是厂商安装器边界。
- 真机未执行真实驱动安装，只验证 API 通路与等价性。