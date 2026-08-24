# Lenovo 驱动事实标准

> 版本：v1  
> 适用对象：联想中国大陆销售机型，重点以 `82JQ` / `PF2SBWJA` / Windows 10 19045 为验证样本。  
> 本文是“驱动应不应该更新、要不要跨 OS 安装、以谁为准”的事实标准，不等同于对 `install_lenovo_drivers.ps1` 当前实现的承诺。

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
| 官方 MD5 | 后端返回 | 否 | 当前未使用 |
| 官方安装参数 | 后端返回 | 有 `InstallCode` | 使用 `InstallCode` |
| 安装历史/来源记录 | 有限日志 | 否 | 当前未记录 OSID 来源 |
| 超时/回退保护 | 内部实现，不透明 | 否 | 是 |
| 第三方万能驱动 | 否 | 否 | 否 |

## 4. 事实标准规则

1. **数据源规则**：只接受联想官方当前 OS 驱动列表作为正常更新依据。本机 Win10 的默认源是 `OSID 42`。
2. **OS 规则**：不把 `OSID 248` 的更高版本视为“更新”。`Local newer` 必须显示为“当前 OS 列表旧、本机来自其它源”，不能简单当成异常。
3. **跨 OS 例外规则**：仅当当前 OS 列表缺失该驱动、设备出现明确问题、且用户确认该例外时，才允许使用其它 OS 列表中的同一硬件驱动；安装后必须记录来源 OSID。
4. **完整性规则**：官方文件必须校验非空、大小和官方 MD5；没有官方 MD5 时使用本地首次下载哈希缓存。
5. **签名规则**：官方工具和下载包来自联想官方域，工具应校验签名和版本。
6. **计划规则**：安装前先 dry-run，输出计划文件；不静默批量安装，不自动重启。
7. **安装规则**：优先使用官方返回的安装参数；失败时只对已知安装器类型做受控回退；不猜测安装器家族。
8. **验证规则**：安装后复核版本、设备状态、服务和关键功能；如果功能正常，不因为状态显示为 `Stopped` 或 `Local newer` 而强行改动。
9. **历史规则**：每次实际安装记录 `DriverCode`、`OSID`、`Version`、`FileName`、`MD5`、来源、时间和结果，避免下一次比较失去来源。
10. **第三方工具规则**：生产环境不使用驱动精灵/万能驱动等第三方工具；它们只能算临时应急，不能成为事实标准。

## 5. 推荐工作流

1. 日常更新：先运行官方 QuickFix，由联想官方工具按当前系统匹配；再运行本项目脚本 `-DryRun -CurrentOSOnly` 做审计。
2. 需要人工控制：打开官网驱动页，按主机编号和当前 OS 选择，不用 `-LatestAcrossOS`。
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

`Local newer` 主要来自之前安装的 Win11 列表版本：

- Lenovo Fn：本地 `2.0.0.25`，Win10 官方 `1.0.2.0`
- AMD VGA：本地 `30.0.14052.9003`，Win10 官方 `27.20.15026.8004`
- NVIDIA VGA：本地 `31.0.15.4630`，Win10 官方 `31.0.15.2799`
- 声卡、网卡、蓝牙、WLAN、电源管理等同样如此

当前状态：

- Fn 键、亮度、音量、性能模式键可正常使用：不重装、不降级。
- `LenovoFnAndFunctionKeys` 服务 `Stopped`，但功能正常：继续观察，不作为必须修复项。
- `AMD Crash Defender Service` 运行中；AMD 显示没有异常，不因 `Degraded` 字样盲目重装。
- 显示、网络、蓝牙、音频近期没有实际故障：保持现状。

因此本机事实标准动作是：**不动这些驱动，默认继续用 Win10 官方列表做以后审计；只有出现明确功能故障时，才用当前 OS 官方包定向修复。**

## 7. 后续落地项

以下项目已在 `install_lenovo_drivers.ps1` v5 落地：

1. 优先使用官方 QuickFix 数据源 `SearchForXbb`（含 `MD5`、`Parameter`、`Bootfile`），失败时回退官网 `drive_listnew`；OS 列表也支持 QuickFix 回退。
2. 下载后校验官方 `MD5`，缓存文件复用前同样校验；本地 SHA-256 缓存继续保留。
3. 安装/下载后写 `%TEMP%\lenovo_driver_history.csv`，记录 `DriverCode`、`OSID`、`Version`、`FileName`、`MD5`、来源、时间和结果。
4. `Local newer` 会优先从历史记录或另一 OS 官方列表识别来源；无法识别时如实显示“来源未知”。
5. `-LatestAcrossOS` 保留但标记为“实验例外”，默认仍为 `-CurrentOSOnly`。
6. 单文件部署、dry-run、交互选择、退出码、超时保护和 Inno/pnputil 回退保持不变。

仍不承诺：替代联想所有隐藏安装器逻辑、自动判断 Fn 等实际功能是否正常、把“来源未知”强行猜成某一 OS。
