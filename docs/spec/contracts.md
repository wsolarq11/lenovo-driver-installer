# 外部契约

API、GUI JSON 协议、退出码、产物与账本列的**单一权威**。改动任一处必须同步本文件与对应测试 golden。

## 单一权威映射

可枚举契约的权威是代码，本文件是**被门禁锁死的人读副本**；锁不住的，本文件只留指针不留副本。

| 契约项 | 机器源（代码） | 门禁 | 本文件的角色 |
| --- | --- | --- | --- |
| CLI 参数、退出码、产物路径 | `internal/app/help.go` 的 `HelpText` | `scripts/verify.ps1` 比对退出码，漂移即失败 | 人读语义 |
| 账本列序 | `internal/app/history.go` 的 `historyColumns` | `history_test.go` golden 锁列序 | 人读语义 |
| GUI JSON 协议 | `internal/app/export.go` | WPF 层解析（参数引用共享 golden） | 人读协议 |
| API 端点与字段 | `internal/api/` | API 层单测 | 人读契约 |
| 回退 offer 字段 | `internal/app/rollback.go` | 回退测试 golden | 人读语义 |

**规则**：改退出码/参数/产物/账本列时，先改代码（机器源），再改本文件副本，跑 `verify.ps1` 让门禁确认两者一致。不允许“只改文档不改代码”或“只改代码不改文档”留下静默漂移。

## 1. 联想 API 集成

主数据源：

```text
POST https://ptstpd.lenovo.com.cn/home/driver/SearchForXbb
Content-Type: application/json
Body: {"searchKey":"<category-id>","osid":"<osid>"}
```

返回 `defaultOS`、`osList`、`partList`、`driverList`；每个驱动条目含 `DriverCode`、`DriverName`、`Version`、`HardwareId`、`Parameter`、`PubTime`、`UpdateTime`、`FileName`、`FilePath`、`FileSize`、`FileType`、`MD5`、`Bootfile` 等。

备用数据源：

```text
GET https://newsupport.lenovo.com.cn/api/drive/drive_listnew?searchKey=<category-id>&sysid=<osid>
```

返回 `InstallCode`、`DriverIssuedDateTime` 等，但**没有官方 `MD5`**。OS 列表从同一 API 家族解析；网页 API 可通过 source hint `Web` 优先。

其它官方接口（工具二进制内嵌，同域）：`ConfigurationQuery/getHardWareInfoBySn`、`ConfigurationQuery/getMachineSequenceInfo`、`Driver/getDriverList`、`driver/QueryForWeb`、`driver/Search`、`tool/version`、`tool/urlList`。

## 2. GUI JSON 协议

Go CLI 写出的 JSON：

```json
{
  "GeneratedAt": "...",
  "MachineModel": "...",
  "SerialNumber": "...",
  "SystemCaption": "...",
  "CurrentOsId": "...",
  "CurrentOsName": "...",
  "ListOsId": "...",
  "ListOsName": "...",
  "DataSource": "...",
  "OsList": [ {"OSID": "...", "OSName": "..."} ],
  "Drivers": [
    {
      "Selected": false,
      "DriverCode": "...",
      "DriverName": "...",
      "Version": "...",
      "LocalVersion": "...",
      "CompareStatus": "...",
      "SourceAudit": "...",
      "CompareSource": "...",
      "DeviceProblem": "...",
      "EvidenceBasis": "...",
      "NonMatchReason": "...",
      "FileName": "...",
      "FilePath": "...",
      "FileSize": "...",
      "MD5": "...",
      "IsApplicable": true,
      "IsUpdate": false
    }
  ]
}
```

字段机器源是 `internal/app/export.go` 的 `guiExportPayload` / `guiDriverRow` 结构体 tag，由 `export_test.go` 的 golden 锁死；上面的示例是示意，字段增删必须同步两处，否则测试失败。`SourceAudit` 是规范字符串形式 `Category: Summary`。WPF 直接渲染此 JSON，并用 `-GuiInstallCodes` 回传选择。`NonMatchReason` 压缩为 `hardware ids X, Y, ... (N ids)`。

## 3. 退出码

| Code | 含义 |
| --- | --- |
| `0` | 成功，或未选择任何驱动 |
| `1` | 一个或多个下载/安装/回退失败，或运行时失败 |
| `2` | 非法参数组合 |
| `3` | `-GuiInstallCodes` 中的 code 在导出中不存在 |

安装器退出码 `3010` / `1641` 统一归一为“成功但需重启”（单一 `InstallSucceeded` 门）。

## 4. 产物

```text
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_install.log   操作日志
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_plan.txt      人类可读计划（单次运行视图）
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_history.csv   CSV 账本（前后版本）
%LOCALAPPDATA%\Lenovo\DriverInstaller\lenovo_driver_rollback.json 回退 offer（GUI 展示缓存；账本是权威）
%LOCALAPPDATA%\Lenovo\DriverInstaller\driver_list_<osid>.json      上次成功驱动列表缓存（逃生舱）
%TEMP%\LenovoDrivers\                                             默认下载目录
<download-dir>\<DriverCode>_<file>                               下载包
<download-dir>\<DriverCode>_<file>.sha256                         本地 SHA-256 伴生
```

来源审计证据源：`C:\Windows\INF\setupapi.offline.log`、`setupapi.dev.log`、`setupapi.setup.log`。

前三个审计产物（log / plan / history）的机器源是 `internal/app/help.go` 的 `Artifacts` 块，由 `scripts/verify.ps1` 比对锁死；改路径必须同步 `help.go` 与本文件，否则门禁失败。

## 5. 账本（WORM 历史）

`lenovo_driver_history.csv` 每行 13 个数据列 + `Hash` 结构性尾列：`timestamp`、`driver code`、`OSID/OSName`、`driver name`、`remote version`、`verified version`、`before version`、`file name`、`MD5`、`source`、`result`、`message`、（序列）。列序由 `historyColumns` 单一源驱动，`history_test.go` 用 golden 字面量锁列序；`Hash` 不进入数据列。

`Hash` = SHA-256 链：前一行 hash + 本行数据列，以 `0x1F` 分隔。写入时读最后一行 hash 起链；`verifyHistoryChain` 重算校验，任何改写/乱序/删除都在某行断链。旧（无 hash）行不被链覆盖，跳过并从首条新行重新起链。读取器剥离 UTF-8 BOM，兼容旧账本。

账本行类型（`result`）：`Install`（安装意图，动作前）、`Installed` / `Failed` / `Deferred`（安装结果）、`Downloaded` / `Verified`、`RollbackOffered`（offer 派生，`Message` 含 `devices=`、`previous_inf=`、`previous_inf_path=`）、`Rollback`（回退意图）、`RolledBack` / `RollbackFailed`（回退结果，`partial` 表示部分设备成功）。
