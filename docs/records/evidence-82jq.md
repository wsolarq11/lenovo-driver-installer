# 82JQ 实测证据与取证时间线

> 本文件是 `docs/spec/fact-standard.md` 的验证样本证据，非规范。规范以 `docs/spec/` 为准；这里记录“当时实测到了什么、结论从哪来”，供追溯，不做行为承诺。

## 1. 已核实证据

### 1.1 官方页面

联想官方驱动页原文：“官方正版驱动安装软件，一键精准匹配，操作简单，稳定护航系统流畅运行！”并注明“该软件支持范围为联想中国大陆地区销售的机型”，写明“需要选择对应操作系统的驱动”。官方按主机编号查询驱动，而不是跨 OS 抢最新版。

来源：<https://newsupport.lenovo.com.cn/driveDownloads_index.html?fromsource=guanwang>

### 1.2 官方 QuickFix 工具

文件 `D:\_B_\pk\_drivers\driverinstall20260722.exe`：`ProductName` QuickFix、`FileVersion` 2.6.26.721、`CompanyName` LENOVO、签名 Valid（`Lenovo (Beijing) Limited`）、SHA-256 `8E16C982C30E17627DB06B8FB66AA4076BCB4C6EA792361F74EC6EE5E73DBB79`。

包体内可见官方组件：`QuickFixDriverInstall.exe`、`LenovoDriverInstall.dll`、`LenovoPcHardwareDriver.dll`、`LenovoDownload.dll`、`DownloadCesTpd.exe`、`GetHardwareID.ps1`、`GXCID.csv`（字段含 `DeviceID`/`HardwareID`/`DriverVersion`/`DriverProviderName`/`Status`/`Description`/`InfName`）。即官方工具确实扫描本机硬件 ID 与已装驱动版本后做“精准匹配”。

### 1.3 官方工具使用的后端

工具二进制内嵌（同域）接口：`ConfigurationQuery/getHardWareInfoBySn`、`ConfigurationQuery/getMachineSequenceInfo`、`Driver/getDriverList`、`driver/QueryForWeb`、`driver/SearchForXbb`、`driver/Search`、`tool/version`、`tool/urlList`。

实测 `POST /home/driver/SearchForXbb`，body `{"searchKey":"3124166","osid":"42"}` 或 `osid:"248"`：返回 `defaultOS`/`osList`/`partList`/`driverList`；每个条目含 `DriverCode`/`DriverName`/`Version`/`HardwareId`/`Parameter`/`PubTime`/`UpdateTime`/`FileName`/`FilePath`/`FileSize`/`FileType`/`MD5`/`Bootfile` 等。`OSID 42` 返回 24 条，`OSID 248` 返回 23 条。

官网接口 `newsupport.lenovo.com.cn/api/drive/drive_listnew?searchKey=3124166&sysid=42`：`OSID 42` 返回 17 条、`OSID 248` 返回 15 条，含 `InstallCode`/`DriverIssuedDateTime`，但**没有官方 `MD5`**。结论：官方工具数据比官网接口更完整；脚本现用 QuickFix 主源 + 网页回退。

### 1.4 本机官方工具日志

`C:\ProgramData\ToolLog\驱动安装工具.log`：官方工具 2026-08-21 曾以 `{"searchKey":"PF2SBWJA"}` 查询本机，随后“正在获取本机驱动”，最后“没有异常设备”。印证官方逻辑是“按主机编号与硬件信息匹配官方列表”，不是跨 OS 找新版本。

### 1.5 社区/博客通行标准

通行标准一致：官方渠道、对应机型、对应系统、按需安装；未找到“跨系统装 OEM 驱动是正统做法”的标准，跨 OS 只是问题驱动下的临时/例外手段。

## 2. 本机 82JQ 判定

最近一次 `-DryRun -CurrentOSOnly`（2026-08-25）：`Update 0 / Up to date 2 / Not installed 0 / Local newer 9 / Unknown 0 / Not applicable 3`。

逐驱动实测结论（全部来自本机 DriverStore/安装器/系统日志）：

1. Intel WLAN `DRV202102040013`，本地 `23.100.0.4`：`setupapi.offline.log:32215` 证明 2026-08-18 NTLite/DISM 离线镜像集成，源 `G:\Win2025\...\Intel_WiFi_23.90.0\Netwtw08.INF`，非本脚本来源。
2. WLAN 多厂商包 `DRV202102040012`：同一离线镜像集成记录，`Netwtw08.INF`，非本脚本来源。
3. 蓝牙 `DRV202102040008`，本地 `22.160.0.4`：本脚本 22:26:46 启动 `DRV202109090062_bluetooth-2GY50FAF43J571C0.exe`；Inno 日志记录原包名，`setupapi.dev.log:9932` 记录 `pnputil.exe /add-driver ...\is-2CS42.tmp\Source\BT_Intel\CCP\ibtusb.inf /install`。
4. AMD Serial-IO `DRV202102040015`，本地 `1.2.0.118`：本脚本 22:27:03 启动 `DRV202109090047_amdIO-2GY505AFFCY401C0.exe`；`setupapi.dev.log:11272` 记录 `pnputil.exe /add-driver ...\is-1F7GH.tmp\Source\SBDrv\I2C\W11x64\amdi2c.inf /install`。
5. Realtek 声卡 `DRV202102040011`，本地 `6.0.9363.1`：本脚本 22:40:31 启动 `DRV202109090046_audio-2GY507AFZLL741C0.exe`；`setupapi.dev.log:6633` 记录 `pnputil.exe /add-driver ...\is-E4G4I.tmp\source\2.Realtek\Codec_9363.1\HDXACPLV.inf /install`。
6. Realtek LAN `DRV202102040000`，本地 `10.50.511.2021`：本脚本 22:26:30 启动 `DRV202109090050_lan-2GY503AFHF9J51C0.exe`；`setupapi.dev.log:9758` 记录 `pnputil.exe /add-driver ...\is-14AGH.tmp\Source\rt640x64.inf /install`。
7. Lenovo Fn `DRV202009030023`，本地 `2.0.0.25`：`setupapi.dev.log:6233` 记录 `pnputil.exe /add-driver C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf /install`，安装于 2026-08-24 19:00:07；Inno 临时包 `Fn-01LF0AAFAR2W6JB0.tmp` 对应 OSID 248 官方包 `Fn-01LF0AAFAR2W6JB0.exe`。脚本日志 22:26 起、未记录 Fn，父级安装器“来源未知”，不写成 QuickFix 或本脚本。
8. AMD VGA `DRV202102040019`，本地 `30.0.14052.9003`：本脚本 21:13:09、22:40:36 启动 `DRV202109090052_AMDVGA-2GY50DAF8XCDZ0C0.exe`；`setupapi.dev.log:8589` 记录 `pnputil.exe /add-driver ...\is-HDQ3M.tmp\source\Packages\Drivers\Display\WT6A_INF\U0381698.inf /install`。
9. NVIDIA VGA `DRV202102040021`，本地 `31.0.15.4630`：有效导入 `setupapi.dev.log:11831` 在 22:37:41，源 `C:\Windows\TempInst\is-6UIF7.tmp\display.driver\nvlt.inf`；该临时目录由 Lenovo 包 `DRV202109090053_NVVGA-TVLC18AF407GA0.exe` 于 21:18:50 解压。脚本日志记录该包 21:18:17、22:17:26 两次启动，22:41:01 那次 22:41:33 中止；无进程创建审计，无法把 22:37 的导入精确归属到某一次启动，但安装器来源是脚本下载并启动过的 Lenovo NVIDIA 包。

`Local newer` 是“本机装的来自其它源的驱动比当前 OS 官方列表新”，不是版本解析错误；这些来源均不是当前 Win10 官方列表。收紧审计边界后，离线镜像集成/在线包安装/预存 DriverStore 包在计划里统一显示 `External`，机制与原始证据行仍保留；只有本工具安装历史才标确定来源。

官方工具接口一致性（2026-08-25 实测）：`SearchForXbb searchKey=3124166 osid=42` 返回 24 行；本机 9 个 `Local newer` 均不在这些版本里；`osid=248` 返回 23 行，Fn/LAN/AMD Serial-IO/Realtek Audio/AMD VGA/Bluetooth/NVIDIA 的官方版本与本机逐项一致，且缓存包名与 Fn Inno 临时包名都对应 OSID 248 包。因此“信息不一致”不是脚本漏掉 QuickFix 接口，而是本机装的是 OSID 248 的包（`-LatestAcrossOS` 模式安装）和离线镜像驱动。

当前状态与结论：Fn 键/亮度/音量/性能模式键正常 → 不重装不降级；`LenovoFnAndFunctionKeys` 服务 `Stopped` 但功能正常 → 继续观察；`AMD Crash Defender Service` 运行中、显示无异常 → 不因 `Degraded` 字样盲目重装；WLAN 已定位为离线镜像集成。动作结论：**不动这些驱动，默认继续用 Win10 官方列表做以后审计；只有出现明确功能故障时才用当前 OS 官方包定向修复。**

## 3. 取证与决策时间线

落地项 1–7 的当前行为已并入 `docs/spec/`（QuickFix 主源、官方 MD5、账本列、`Local newer` 来源审计、`-LatestAcrossOS` 实验例外、单文件部署、来源审计输出）。以下为 2026-08 起的关键取证与修正：

- **固件分类取证（2026-08）**：QuickFix `driverList` 与官网 `drivelist` 均无固件类别字段，只有 `FileType` 与 `Bootfile`，不足以作为固件证据；当前 `reFirmware` 名字匹配是启发式，待官方出现类别字段后改数据字段过滤，不伪造类别。
- **真机 smoke（2026-09-20，82JQ / PF2SBWJA / Win10 19045）**：dry-run 识别 `82JQ` 与 `PF2SBWJA`，OSID 42 列表 23 行，`Update 0 / Up to date 1 / Not installed 1 / Local newer 9 / Not applicable 12`，`fact=1 inference=10 undetermined=12`，计划文件稳定写入。GUI JSON 的 `EvidenceBasis`/`DeviceProblem`/`NonMatchReason` 正确填充，未命中原因压缩为 `hardware ids X, Y, ... (N ids)`。Authenticode：`DRV202009030023`（`FN-01LF02AFAR2W6JB0.exe`，1616448 字节）下载后过哈希与签名校验，官方 MD5 `067456565a7f261fb00961e70edbc109`，账本写 `Downloaded`；缓存复用同样完整校验。绑定/重启/回滚用纯函数夹具覆盖；夹具发现并修复了历史账本首次写入只写表头、丢第一条记录的问题。
- **回退语义修正（2026-09-20）**：`pnputil /delete-driver ... /uninstall /force` 是从 Driver Store 删包，不把设备切回旧驱动，删除活动包可能让设备失去当前驱动。设备级回退只能走“回退驱动程序”或重装旧包 INF；`RemoveDriverPackage` 保留为包清理原语。
- **健康判定边界（2026-09-20）**：`CM_Get_DevNode_Status` 问题码 0 只表示未观察到问题，不证明功能正常；间歇性崩溃/功耗/睡眠/性能回退不在覆盖范围。
- **签名者与撤销（2026-09）**：Authenticode 收紧为“链有效 + 撤销检查开启 + 签名者组织为 Lenovo”。实测 `DRV202009030023_FN-01LF02AFAR2W6JB0.exe` 签名者 Subject `CN=Lenovo, OU=G09, O=Lenovo...`（Issuer `Symantec Class 3 SHA256 Code Signing CA - G2`），过 WinVerifyTrust 与组织白名单；撤销离线（`CRYPT_E_REVOCATION_OFFLINE`，Symantec CRL 腐烂）降级为链验证 + 白名单并记 WARN；被吊销证书仍拒绝。拦截面用 `node.exe`（OpenJS 有效签名）实测被拒。
- **下载域名白名单（2026-09）**：下载前校验 `FilePath` host 必须属于 `lenovo.com`/`lenovo.com.cn`（含子域）。实测 82JQ 全部 23 个驱动 `FilePath` 指向 `newdriverdl.lenovo.com.cn`，落白名单；后续还观察到 `driverdl.lenovo.com.cn`，同为 `lenovo.com.cn` 子域。
- **接口契约漂移（2026-09）**：非空列表但解析 0 行 → `ContractDriftError`；首选源漂移降级备用源时记 WARN；HTTP 非 2xx/非 JSON/顶层全空 → `InterfaceGateError`。空列表仍按“无驱动”，不误报漂移。
- **接口死亡逃生舱（2026-09）**：上次成功列表持久化 `driver_list_<osid>.json`（UTC 时间戳），双接口失效时降级用缓存并标过期。真机 dry-run 全链路成功：`fact=1 inference=10 undetermined=12`。
- **设备级回退落地（2026-09）**：`DiRollbackDriver` 逐实例 ID 回退；装后复核产生 offer 先写 `RollbackOffered` 行；`lenovo_driver_rollback.json` 降级为 GUI 缓存。
- **装错回退的可靠主干（2026-09，纠正上一条）**：`DiRollbackDriver` 备份只在“驱动装成功且设备被判定正常”后才建立，“装错”最常命中 `ERROR_NO_MORE_ITEMS`；且 `DiInstallDriverW`/`pnputil /add-driver /install` **不降级**（对已绑定更新驱动的设备保留新驱动甚至假成功）。fallback 改用 `UpdateDriverForPlugAndPlayDevicesW` + `INSTALLFLAG_FORCE`。待取证：`FullInfPath` 需指向非系统目录，当前旧 INF 仍是 `%SystemRoot%\INF\oemN.inf`，是否改 FileRepository 路径待真状态机确认。
- **审计启发式证据等级（2026-09）**：`Source audit` 的 provenance 是**夹具级**验证，未与真实 DriverStore 对照 ground truth；Windows 会轮转/截断 `setupapi` 日志，离线集成与在线安装常不可区分，结论等级是**推断**，未对照前不得当事实展示。
- **账本哈希链（2026-09）**：账本升级为可证伪 WORM——每行 13 数据列后追加 `Hash`（SHA-256 链，`0x1F` 分隔前一行 hash + 本行数据列）；断链记 WARN 不硬阻断（旧账本无 hash、个人场景自决）。
- **安装意图审计（2026-09）**：安装动作前先写 `Install` 意图行，写失败跳过该驱动安装。
- **设备漂移审计取证（2026-09）**：实测 RAC 数据库不存在（`RacWmiDatabase.sdf` 缺失）、Windows Update 历史不可靠（`ReportingEvents.log` 缺失）、DriverStore 可用（758 个包带导入时间戳）、事件日志可用（`Kernel-PnP/Configuration` 400/410/420、`DeviceSetupManager/Admin` 100/101/112/122）。原“机制 vs 意图对账”在设备级不可行：安装账本只记 `DriverCode`，不记 `PnpDeviceID`/`InfName`，与 Windows 记录之间缺 join 键。本轮落地设备状态快照 + 差分（`-Audit`，只读、按 `PnpDeviceID` 键）作为未来对账基础。
- **账本契约变更落地（安装行补设备身份列，2026-09）**：账本补 `devices` 列（本行动作触及的 PnP 设备实例 id 集合，去重、`;` 分隔），设备身份在评估期一次性捕获后由该驱动所有账本行复用，回退逐设备行收窄为单个 id。`-Audit` 设备差分据此逐条归因到账本行（`[ledger: DRV1,DRV2]`），无匹配行保持可见的未归因。设备身份只有一个机器可读出口：`message` 不再承载设备 id，下游也不解析 `message`（`TestAuditAttributionIgnoresMessageText`）。同时把链哈希从“按当前列数定位”改为“按在盘表头列名定位”（`resolveHashIndex`），并让写入在表头缺列时拒绝而非追加更宽的行——旧语义下一加列就会让篡改检测静默失效。见 `docs/spec/contracts.md` §5、`docs/spec/invariants.md` 第 5 条。
- **回退 INF 路径取证（2026-09-24，真机 82JQ）**：`UpdateDriverForPlugAndPlayDevicesW` 用 `%SystemRoot%\INF\oem47.inf`（发布副本）+ 合成 hardwareID `ROOT\PROBE_NONEXISTENT_82JQ_20260923` 返回 `0xe000020b`（`ERROR_NO_SUCH_DEVINST`，找不到设备），未报“找不到 INF”；对照 missing-INF smoke 测试返回 `Unable to find INF path`，证明发布副本路径通过 API 路径检查。`oem47.inf` 与 FileRepository 原件 `acpivpc.inf_amd64_162ec7a9318c8e40\acpivpc.inf` SHA256 同为 `C3237F2C...`，逐字节一致。完整重装成功判据仍属破坏性演练，留待设备出问题时授权执行。
- **setupapi 日志易失性实测（2026-09-24）**：`setupapi.offline.log` / `setupapi.setup.log` 已被轮转删除，`setupapi.dev.log` 仅存最近三次 Boot Session（57 行、2.9KB）；evidence 历史行号（`setupapi.dev.log:32215` 等）不可复现。provenance 归因无法事后对照 DriverStore ground truth，结论等级维持**推断**，现为实测证实其上限。
- **WPF Loaded 自动识别真机闭环（2026-09-24）**：真实桌面（console）下 `pwsh -STA -File lenovo_driver_wpf.ps1` 启动，窗口 `Loaded` 自动触发 Export（启动后 4 秒生成 `lenovo_gui_export_*.json`），`MachineText` 经 UIAutomation 实测为 `82JQ / PF2SBWJA | 当前系统 Windows 10 64-bit (OSID 42) | 当前列表 Windows 10 64-bit (OSID 42)`；`-WorkerSmoke` 亦 `WORKER_SMOKE_OK rows=23 list=Windows 11 64-bit (248)`。worklog 两处 `[待取证]` 闭环。
