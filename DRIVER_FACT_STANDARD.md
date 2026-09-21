# Lenovo 驱动事实标准

> 版本：v1  
> 适用对象：联想中国大陆销售机型，重点以 `82JQ` / `PF2SBWJA` / Windows 10 19045 为验证样本。  
> 本文是“驱动应不应该更新、要不要跨 OS 安装、以谁为准”的事实标准，不等同于对当前实现细节的承诺。

## 1. 事实结论

1. 驱动来源的事实标准是联想官方数据源，不是第三方驱动工具，也不是“任意版本越新越好”。
2. 官方按“机型/主机编号 + 操作系统”给出驱动列表。本机 `82JQ` 的 `OSID 42` 是 Windows 10 64-bit，`OSID 248` 是 Windows 11 64-bit。
3. 当前机器是 Windows 10，默认应只比较 `OSID 42` 的官方列表。
4. `Local newer` 主要不是版本解析错误，而是“本机装的是另一个 OS 列表里的驱动，当前脚本拿当前 OS 列表来比”。它不是必须消除的状态，也不应该用“整体降级到 Win10 旧包”来强行消除。
5. 官方 QuickFix 驱动安装工具适合普通用户的“当前系统一键匹配”，但它是 GUI 黑盒，不公开可审计的 dry-run 和安装来源记录。
6. 官网驱动页面适合人工查看、按需选择，但不会自动对比本机驱动，也不提供批量安装。
7. 本项目脚本的价值是 dry-run、计划文件、本机版本对比、选择安装、哈希缓存、日志和超时保护；它应该继续以当前 OS 为默认，并把“来自哪个 OSID”作为一等信息记录。
8. 本机 Fn 键、亮度、音量、性能模式键当前可正常使用，因此即使 `LenovoFnAndFunctionKeys` 服务是 `Stopped`，也不应为了“修状态”去盲目重装或降级。

## 2. 已核实证据

### 2.1 官方页面

联想官方驱动页原文：

> 官方正版驱动安装软件，一键精准匹配，操作简单，稳定护航系统流畅运行！  
> 注：“该软件支持范围为联想中国大陆地区销售的机型”。

并写明“需要选择对应操作系统的驱动”。官方页面对应的是按主机编号查询驱动，而不是跨 OS 抢最新版。

来源：<https://newsupport.lenovo.com.cn/driveDownloads_index.html?fromsource=guanwang>

### 2.2 官方 QuickFix 工具

文件：`D:\_B_\pk\_drivers\driverinstall20260722.exe`

- `ProductName`：QuickFix
- `FileVersion`：`2.6.26.721`
- `CompanyName`：LENOVO
- 签名：Valid，签名者为 `Lenovo (Beijing) Limited`
- SHA-256：`8E16C982C30E17627DB06B8FB66AA4076BCB4C6EA792361F74EC6EE5E73DBB79`

包体内可见官方自研组件：

- `QuickFixDriverInstall.exe`
- `LenovoDriverInstall.dll`
- `LenovoPcHardwareDriver.dll`
- `LenovoDownload.dll`
- `DownloadCesTpd.exe`
- `GetHardwareID.ps1`
- `GXCID.csv`，字段包含 `DeviceID`、`HardwareID`、`DriverVersion`、`DriverProviderName`、`Status`、`Description`、`InfName`

即官方工具确实会扫描本机硬件 ID 和已装驱动版本，再做“精准匹配”。

### 2.3 官方工具使用的后端

工具二进制中的官方接口和实测结果：

- `https://ptstpd.lenovo.com.cn/home/ConfigurationQuery/getHardWareInfoBySn?productSn=...`
- `https://ptstpd.lenovo.com.cn/home/ConfigurationQuery/getMachineSequenceInfo`
- `https://ptstpd.lenovo.com.cn/home/Driver/getDriverList`
- `https://ptstpd.lenovo.com.cn/home/driver/QueryForWeb`
- `https://ptstpd.lenovo.com.cn/home/driver/SearchForXbb`
- `https://ptstpd.lenovo.com.cn/home/driver/Search`
- `https://ptstpd.lenovo.com.cn/Home/tool/version`
- `https://ptstpd.lenovo.com.cn/home/tool/urlList`

实测 `POST /home/driver/SearchForXbb`，body 为 `{"searchKey":"3124166","osid":"42"}` 或 `osid:"248"`：

- 返回 `defaultOS`、`osList`、`partList`、`driverList`
- 每个驱动条目包含 `DriverCode`、`DriverName`、`Version`、`HardwareId`、`Parameter`、`PubTime`、`UpdateTime`、`FileName`、`FilePath`、`FileSize`、`FileType`、`MD5`、`Bootfile` 等
- `OSID 42` 返回 24 条，`OSID 248` 返回 23 条

官网页面接口 `https://newsupport.lenovo.com.cn/api/drive/drive_listnew?searchKey=3124166&sysid=42`：

- `OSID 42` 返回 17 条，`OSID 248` 返回 15 条
- 返回 `InstallCode`、`DriverIssuedDateTime` 等字段，但**没有官方 `MD5`**

结论：官方工具的数据比官网页面接口更完整；本项目脚本目前使用的官网接口缺少官方 MD5 和部分官方安装参数。

### 2.4 本机官方工具日志

`C:\ProgramData\ToolLog\驱动安装工具.log` 中可见官方工具在 2026-08-21 曾以 `{"searchKey":"PF2SBWJA"}` 查询本机，随后“正在获取本机驱动”，最后记录“没有异常设备”。

这进一步说明官方工具的事实逻辑是：按本机主机编号和硬件信息匹配官方列表，而不是跨 OS 找新版本。

### 2.5 社区/博客通行标准

博客和社区一致强调“官方渠道、对应机型、对应系统、按需安装”：

- [联想官网驱动下载页](https://newsupport.lenovo.com.cn/driveDownloads_index.html?fromsource=guanwang)
- [联想笔记本：安装适配自己电脑的驱动](https://blog.csdn.net/x18094/article/details/124383411)
- [win11可以支持win10驱动吗](https://blog.csdn.net/qq_29508575/article/details/122565595)
- [安装英伟达驱动，提示驱动和windows版本不兼容](https://blog.csdn.net/qq_53254720/article/details/127298063)

没有找到“跨系统装 OEM 驱动是正统做法”的事实标准；相反，社区通行标准是：正常更新使用当前系统官方驱动，跨 OS 只是问题驱动下的一种临时/例外手段。

## 3. 工具能力对比

| 能力 | 官方 QuickFix | 官网驱动页面 | 本项目脚本 |
| --- | --- | --- | --- |
| 官方来源 | 是 | 是 | 是 |
| 签名/来源可信 | 是 | 是 | 是 |
| 按主机编号匹配 | 是 | 是 | 是 |
| 当前 OS 默认 | 是 | 页面按 OS 选择 | 是 |
| 一键安装 | 是 | 否 | 交互式，可自动 |
| Dry-run 计划 | 否，黑盒 | 否 | 是 |
| 本机版本对比 | 是 | 否 | 是 |
| 选择安装 | 有界面选择 | 人工下载 | 数字选择 |
| 官方 MD5 | 后端返回 | 否 | 使用 |
| Authenticode 签名 | 官方包自带 | 是 | 使用（WinVerifyTrust） |
| 官方安装参数 | 后端返回 | 有 `InstallCode` | 使用 `InstallCode` |
| 安装历史/来源记录 | 有限日志 | 否 | 是（CSV 记录 OSID） |
| 超时/回退保护 | 内部实现，不透明 | 否 | 是 |
| 第三方万能驱动 | 否 | 否 | 否 |

## 4. 事实标准规则

1. **数据源规则**：只接受联想官方当前 OS 驱动列表作为正常更新依据。本机 Win10 的默认源是 `OSID 42`。
2. **OS 规则**：不把 `OSID 248` 的更高版本视为“更新”。`Local newer` 必须显示为“当前 OS 列表旧、本机来自其它源”，不能简单当成异常。
3. **跨 OS 例外规则**：仅当当前 OS 列表缺失该驱动、设备出现明确问题、且用户确认该例外时，才允许使用其它 OS 列表中的同一硬件驱动；安装后必须记录来源 OSID。
4. **完整性规则**：官方文件必须校验非空、大小、官方 MD5 与 Authenticode 签名；没有官方 MD5 时使用本地首次下载哈希缓存；`-SkipSignatureCheck` 仅限诊断性未签名样本。
5. **签名规则**：官方工具和下载包必须来自联想官方域，下载包经 WinVerifyTrust 校验 Authenticode；工具自身校验签名和版本。
6. **计划规则**：安装前先 dry-run，输出计划文件；不静默批量安装，不自动重启。
7. **安装规则**：优先使用官方返回的安装参数；失败时只对已知安装器类型做受控回退；不猜测安装器家族。
8. **验证规则**：安装后复核版本、设备状态、服务和关键功能；如果功能正常，不因为状态显示为 `Stopped` 或 `Local newer` 而强行改动。
9. **历史与来源规则**：每次实际安装记录 `DriverCode`、`OSID`、`Version`、`FileName`、`MD5`、来源、时间和结果；每个适用驱动在计划中输出来源审计，优先使用安装历史、DriverStore 导入记录（`setupapi.*.log`）、当前/其它 OS 官方列表等真凭实据；没有可复核证据时如实显示“来源未知”。
10. **第三方工具规则**：生产环境不使用驱动精灵/万能驱动等第三方工具；它们只能算临时应急，不能成为事实标准。

## 5. 推荐工作流

1. 日常更新：先运行官方 QuickFix，由联想官方工具按当前系统匹配；再运行本项目脚本 `-DryRun -CurrentOSOnly` 做审计。
2. 需要人工控制：打开官网驱动页，按主机编号和当前 OS 选择；或使用 `-TargetOS 248` 只查看/比较指定 OS 的官方列表；默认交互菜单里也可以按 `t` 在支持的 OS 列表间切换，不用 `-LatestAcrossOS`。
3. 需要批量/自动化：使用本项目脚本默认的 `-CurrentOSOnly`；先 `-DryRun`，再 `-DownloadOnly`，确认后再安装。
4. 跨 OS 例外：必须写清“哪个驱动、哪个 OSID、为什么、装后如何验证”，并记录到安装历史；不允许为了消除 `Local newer` 而整体切换到 Win11 包或整体回滚 Win10 包。

## 6. 本机 82JQ 判定

最近一次 `-DryRun -CurrentOSOnly`（2026-08-25）结果：

- `Update`：0
- `Up to date`：2
- `Not installed`：0
- `Local newer`：9
- `Unknown`：0
- `Not applicable`：3

逐驱动实测结论（全部来自本机 DriverStore/安装器/系统日志，不用猜测）：

1. Intel WLAN `DRV202102040013`，本地 `23.100.0.4`：`setupapi.offline.log:32215` 证明 2026-08-18 NTLite/DISM 离线镜像集成，源 `G:\Win2025\...\Intel_WiFi_23.90.0\Netwtw08.INF`，非本脚本来源。
2. WLAN 多厂商包 `DRV202102040012`：同一离线镜像集成记录，`Netwtw08.INF`，非本脚本来源。
3. 蓝牙 `DRV202102040008`，本地 `22.160.0.4`：本脚本于 22:26:46 启动 `DRV202109090062_bluetooth-2GY50FAF43J571C0.exe`；Inno 日志记录原包名，`setupapi.dev.log:9932` 记录 `pnputil.exe /add-driver D:\Users\Administrator\TEMP\is-2CS42.tmp\Source\BT_Intel\CCP\ibtusb.inf /install`。
4. AMD Serial-IO `DRV202102040015`，本地 `1.2.0.118`：本脚本于 22:27:03 启动 `DRV202109090047_amdIO-2GY505AFFCY401C0.exe`；`setupapi.dev.log:11272` 记录 `pnputil.exe /add-driver ...\is-1F7GH.tmp\Source\SBDrv\I2C\W11x64\amdi2c.inf /install`。
5. Realtek 声卡 `DRV202102040011`，本地 `6.0.9363.1`：本脚本于 22:40:31 启动 `DRV202109090046_audio-2GY507AFZLL741C0.exe`；`setupapi.dev.log:6633` 记录 `pnputil.exe /add-driver ...\is-E4G4I.tmp\source\2.Realtek\Codec_9363.1\HDXACPLV.inf /install`。
6. Realtek LAN `DRV202102040000`，本地 `10.50.511.2021`：本脚本于 22:26:30 启动 `DRV202109090050_lan-2GY503AFHF9J51C0.exe`；`setupapi.dev.log:9758` 记录 `pnputil.exe /add-driver ...\is-14AGH.tmp\Source\rt640x64.inf /install`。
7. Lenovo Fn `DRV202009030023`，本地 `2.0.0.25`：`setupapi.dev.log:6233` 记录 `pnputil.exe /add-driver C:\Windows\TempInst\is-EFU44.tmp\source\LenovoFnAndFunctionKeys.inf /install`，安装于 2026-08-24 19:00:07；`C:\Windows\TempInst\is-EFU44.tmp\dpinst.exe` 由 Inno 临时包 `Fn-01LF0AAFAR2W6JB0.tmp` 展开，该文件名与 OSID 248 官方包 `Fn-01LF0AAFAR2W6JB0.exe` 对应。脚本日志开始于 22:26，未记录 Fn，因此父级安装器为“来源未知”，不能写成 QuickFix 或本脚本。
8. AMD VGA `DRV202102040019`，本地 `30.0.14052.9003`：本脚本于 21:13:09、22:40:36 启动 `DRV202109090052_AMDVGA-2GY50DAF8XCDZ0C0.exe`；`setupapi.dev.log:8589` 记录 `pnputil.exe /add-driver ...\is-HDQ3M.tmp\source\Packages\Drivers\Display\WT6A_INF\U0381698.inf /install`。
9. NVIDIA VGA `DRV202102040021`，本地 `31.0.15.4630`：有效导入 `setupapi.dev.log:11831` 发生在 22:37:41，源 `C:\Windows\TempInst\is-6UIF7.tmp\display.driver\nvlt.inf`；该临时目录由 Lenovo 包 `DRV202109090053_NVVGA-TVLC18AF407GA0.exe` 于 21:18:50 解压。脚本日志记录该包 21:18:17、22:17:26 两次安装启动，22:41:01 那次在 22:41:33 中止；没有进程创建审计，无法把 22:37 的导入精确归属到某一次启动，但安装器来源是脚本下载并启动过的 Lenovo NVIDIA 包。

`Local newer` 是“本机装的来自其它源的驱动比当前 OS 官方列表新”，不是版本解析错误；这些来源均不是当前 Win10 官方列表。

注：实现收紧审计边界后，上述“离线镜像集成 / 在线包安装 / 预存 DriverStore 包”在计划里的类别统一显示为 `External`，机制与原始证据行仍保留；只有本工具安装历史才被标为确定来源。

官方工具接口一致性（2026-08-25 实测）：

- `D:\_B_\pk\_drivers\driverinstall20260722.exe` 是 Lenovo QuickFix `2.6.26.721`，Lenovo 官方签名有效。
- 该工具二进制内嵌 `https://ptstpd.lenovo.com.cn/home/driver/SearchForXbb`，POST body 为 `{"searchKey":...,"osid":...}`，与脚本当前 QuickFix 接口相同。
- 实测 `SearchForXbb` `searchKey=3124166`、`osid=42` 返回 24 行当前 OS 列表；本机 9 个 `Local newer` 均不在这些版本里。
- 实测 `osid=248` 返回 23 行 Windows 11 列表；Fn、LAN、AMD Serial-IO、Realtek Audio、AMD VGA、Bluetooth、NVIDIA 的官方版本与本机逐项一致，且 `%TEMP%\LenovoDrivers` 缓存包名和 Fn Inno 临时包名都对应 OSID 248 包。
- 因此“信息不一致”不是脚本漏掉 QuickFix 接口，而是本机装的是 OSID 248 的包（脚本 `-LatestAcrossOS` 模式安装）和离线镜像驱动，不是 QuickFix 当前 OS 列表；默认 `-CurrentOSOnly` 本来就该显示为 `Local newer`。

当前状态：

- Fn 键、亮度、音量、性能模式键可正常使用：不重装、不降级。
- `LenovoFnAndFunctionKeys` 服务 `Stopped`，但功能正常：继续观察，不作为必须修复项。
- `AMD Crash Defender Service` 运行中；AMD 显示没有异常，不因 `Degraded` 字样盲目重装。
- WLAN 本机 `23.100.0.4` 已通过 `setupapi.offline.log` 定位为 2026-08-18 NTLite 离线镜像集成（`G:\Win2025\...\Intel_WiFi_23.90.0\Netwtw08.INF`），不是本脚本来源。
- Lenovo Fn 的导入命令和 Inno 临时包可证实，但启动该包的父级程序没有可复核记录；结论保持“来源未知”，不写 QuickFix、不写本脚本。
- 显示、网络、蓝牙、音频近期没有实际故障：保持现状。

因此本机事实标准动作是：**不动这些驱动，默认继续用 Win10 官方列表做以后审计；只有出现明确功能故障时，才用当前 OS 官方包定向修复。**

## 7. 后续落地项

以下项目已由当前 Go 实现落地：

1. 优先使用官方 QuickFix 数据源 `SearchForXbb`（含 `MD5`、`Parameter`、`Bootfile`），失败时回退官网 `drive_listnew`；OS 列表也支持 QuickFix 回退。
2. 下载后校验官方 `MD5`，缓存文件复用前同样校验；本地 SHA-256 缓存继续保留。
3. 安装/下载后写 `%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_history.csv`，记录 `DriverCode`、`OSID`、`Version`、`VerifiedVersion`、`BeforeVersion`、`FileName`、`MD5`、来源、时间和结果；`VerifiedVersion` 保存安装后实际检测到的本机版本，`BeforeVersion` 保存安装前本机版本；操作日志同目录，计划文件为 `%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_plan.txt` 的单次运行视图。
4. `Local newer` 优先从本工具安装历史识别来源；共享 `setupapi` 日志与 DriverStore 的机制证据统一标为 `External`（离线镜像集成/在线包安装/预存包只保留在摘要与证据行），另一 OS 官方列表只给出“版本匹配某 OS 列表”的弱结论，绝不强行猜成某一 OS。
5. `-LatestAcrossOS` 保留但标记为“实验例外”，默认仍为 `-CurrentOSOnly`；新增 `-TargetOS <OSID|OSName>` 显式查看单个支持 OS 的官方列表，不与 `-LatestAcrossOS` 合并。
6. 单文件部署、dry-run、交互选择、退出码、超时保护和 Inno/pnputil 回退保持不变。
7. 每个适用驱动在 dry-run 计划中输出 `Source audit`：类别、结论和证据行；`setupapi.offline.log`、`setupapi.dev.log`、`setupapi.setup.log` 中的 DriverStore 导入记录用于区分离线镜像集成、在线安装包和历史安装。

仍不承诺：替代联想所有隐藏安装器逻辑、自动判断 Fn 等实际功能是否正常、把“来源未知”强行猜成某一 OS。

8. 固件分类取证（2026-08）：QuickFix `driverList` 与官网 `drivelist` 均未提供固件类别字段，只有 `FileType`（exe/inf/zip/cab）与 `Bootfile`；`Bootfile` 语义不足以作为固件证据。当前 `reFirmware` 名字匹配仍是启发式，待官方接口出现类别字段后改为数据字段过滤，不伪造类别。

9. 真机 smoke 清单（个人单机收尾，需 82JQ 主机配合）：以下五项在实机跑一次，逐项记录实测到本节。设备健康：dry-run 计划中 `Device problem` 对正常设备为空、对已知异常设备有 `Code N`。Authenticode：对官方 EXE 缓存包执行签名校验通过，坏文件或 `-SkipSignatureCheck` 行为符合预期。绑定验证：安装后历史记录出现 `binding=bound|staged|unchanged|undetected` 之一，且 `VerifiedVersion/BeforeVersion` 不为空。重启推迟：让任一 INF/EXE 包返回 `3010/1641`，确认后续驱动进入 `Deferred`、不继续安装、日志与账本有记录。回滚提示：构造安装后设备问题，确认计划/日志给出设备级回退指引（回退驱动程序，旧 INF 名），不执行 `pnputil /delete-driver`，并写账本。

9.1 实测结果（2026-09-20，82JQ / PF2SBWJA，Windows 10 19045）：

- dry-run：识别 `82JQ` 与 `PF2SBWJA`，OSID 42 列表 23 行；`Update 0 / Up to date 1 / Not installed 1 / Local newer 9 / Not applicable 12`，`fact=1 inference=10 undetermined=12`。计划文件稳定写入 `C:\Users\Administrator\AppData\Local\Lenovo\DriverInstaller\lenovo_driver_plan.txt`。
- 导出：GUI JSON 的 `EvidenceBasis`、`DeviceProblem`、`NonMatchReason` 均正确填充；未命中原因已压缩为 `hardware ids X, Y, ... (N ids)`，不再打印整条硬件 ID 列表。
- Authenticode：`DRV202009030023`（Lenovo Fn，`FN-01LF02AFAR2W6JB0.exe`，1616448 字节）下载后通过哈希与签名校验，官方 MD5 `067456565a7f261fb00961e70edbc109`，账本写入 `Downloaded` 记录；缓存复用同样走完整校验。
- 绑定/重启/回滚：绑定标签与重启判定用纯函数夹具覆盖，回滚提示改为设备级人工回退指引，不动真机；夹具发现并修复了历史账本首次写入只写表头、丢第一条记录的问题。

10. 回滚语义修正（2026-09-20）：`pnputil /delete-driver oemN.inf /uninstall /force` 是从 Driver Store 删除包，不会把设备切回旧驱动，删除活动包可能让设备失去当前驱动。设备级回退只能走设备管理器“回退驱动程序”或重装旧包 INF。引擎不再自动删除包，只输出人工回退指引并写账本；`RemoveDriverPackage` 保留为包清理原语，注释明确不是回退。

11. 健康判定边界（2026-09-20）：`CM_Get_DevNode_Status` 问题码为 0 只表示未观察到问题，不证明功能正常；间歇性崩溃、功耗、睡眠、性能回退不在其覆盖范围。计划不把“无问题码”写成“正常”，已固化为不变量 3。

12. 签名者与撤销（2026-09）：Authenticode 校验从“仅验证签名有效”收紧为“签名链有效 + 撤销检查开启 + 签名者组织必须为 Lenovo”。实测官方包 `DRV202009030023_FN-01LF02AFAR2W6JB0.exe` 的签名者 Subject 为 `CN=Lenovo, OU=G09, O=Lenovo, L=Morrisville, S=North Carolina, C=US`（Issuer 为 `Symantec Class 3 SHA256 Code Signing CA - G2`），通过 WinVerifyTrust（含撤销检查）与 Lenovo 组织白名单；组织级匹配兼容 `Lenovo (Beijing) Limited` 等多个主体，不用单一精确主体。撤销检查离线（`CRYPT_E_REVOCATION_OFFLINE`，Symantec CRL 已随 CA 迁移腐烂）时降级为“链验证 + 签发者白名单”并记 WARN，不拒死；被吊销证书（`CRYPT_E_REVOKED`）仍拒绝。拦截面用 `node.exe`（OpenJS Foundation 有效签名）实测被拒。

13. 下载域名白名单（2026-09）：下载前校验 `FilePath` 的 host 必须属于 `lenovo.com` 或 `lenovo.com.cn`（含子域），否则在请求第一个字节前拒绝并记日志。实测 82JQ 全部 23 个驱动的真实 `FilePath` 指向 `newdriverdl.lenovo.com.cn`，落白名单内，不误拒官方 CDN。任何指向第三方域的被劫持 URL 失败关闭。扩白名单须按 AGENTS.md 走差异/理由/影响/测试/回滚/过期记录，缺一即缺陷。

14. 接口契约漂移监测（2026-09）：QuickFix/Web 返回非空列表但解析后 0 行（`FileName`/`FilePath` 必需字段缺失）时，报 `ContractDriftError`，区分“接口结构变了”与“真的无驱动”，不再静默降级；首选源漂移降级到备用源时，日志输出 WARN 漂移警告。空列表仍按“无驱动”处理，不误报漂移。HTTP 非 2xx、响应非预期 JSON、或顶层字段全空（`{"code":401}` 类认证改写）时，报 `InterfaceGateError`，区分“接口被认证/封禁/限流”与“字段漂移”，不静默降级为“无驱动”。

15. 接口死亡逃生舱 + 端到端验证（2026-09）：上次成功获取的驱动列表持久化到本地缓存（`driver_list_<osid>.json`，UTC 时间戳），两个联想接口都失效时降级用缓存并标注过期，工具不直接归零。真机 dry-run（82JQ / serial PF2SBWJA / 分类 3124166 / OSID 42）端到端跑通：机器→分类→OS→23 驱动→比较→计划全链路成功，9 Local newer / 1 Not installed / 1 Up to date / 12 Not applicable，fact=1 inference=10 undetermined=12，符合事实分级。真实 CDN 域名另见 `driverdl.lenovo.com.cn`（与 `newdriverdl.lenovo.com.cn` 同为 `lenovo.com.cn` 子域，均落白名单）。
