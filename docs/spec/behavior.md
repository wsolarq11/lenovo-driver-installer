# 运行时行为

单一权威：比较与选择、下载完整性、安装分发、回退语义、接口漂移与逃生舱。改动任一处必须同步本文件与对应测试 golden。

## 1. 比较与选择

- 默认当前 OS only；`-TargetOS` 选一个显式 OS 列表；`-LatestAcrossOS` 合并所有支持 OS 列表后按解析版本取最新（发布日期/版本号为平局裁决）；`-CurrentOSOnly` 禁用交互 `t` 切换。
- `Local newer` 不是失败。来源证据把它标为：离线镜像集成 / 在线包安装 / 预存 DriverStore 包 / 其它官方 OS 来源 / 同当前 OS 历史 / 未知。
- 软件包（software-only）的本地版本来自已装应用/服务，不来自无关的同厂商 PnP 设备。
- 固件包（BIOS/EC/ME/TPM/Thunderbolt/UEFI）默认跳过，除非传 `-IncludeBios`。
- 匹配设备经 `CM_Get_DevNode_Status` 读 Windows 问题码；匹配设备报问题的驱动在评估、计划、GUI 导出中带 `DeviceProblem` 摘要。
- `CompareStatus` 取值：`Update` / `Up to date` / `Not installed` / `Local newer` / `Unknown` / `Not applicable`。

### 来源审计的证据等级（[推断]）

`Source audit` 的 provenance 归因（`setupapi.dev.log` / `setupapi.offline.log` / `setupapi.setup.log` 导入记录）结论等级是**推断**，且其上限已实测：本机 `setupapi.offline.log` / `setupapi.setup.log` 已被 Windows 轮转删除，`setupapi.dev.log` 仅存最近三次 Boot Session（57 行），历史导入行号（如 `setupapi.dev.log:32215`）不可复现。故 provenance 无法事后对照 DriverStore ground truth——不是“未对照”，而是“对照对象已易失”，语义上不可事后复现，不得当事实展示。

## 2. 下载与完整性

下载产物命名 `DriverCode_FileName`。每个文件：

1. 缓存文件大小对照官方大小。
2. 校验本地 SHA-256 伴生文件。
3. 数据源提供时校验官方 `MD5`（缓存复用前同样校验）。
4. 校验失败即删缓存重下。
5. 下载后计算本地 SHA-256 写 `<file>.sha256`。
6. Authenticode 校验（WinVerifyTrust）：签名链有效 + 撤销检查开启（`WTD_REVOKE_WHOLECHAIN`）+ 签名者组织必须为 Lenovo（`CertEnumCertificatesInStore` + `CertGetNameStringW`，组织级匹配兼容 `Lenovo (Beijing) Limited` 等多个主体）。有效但来自无关发布者的签名被拒。撤销离线（`CRYPT_E_REVOCATION_OFFLINE`）降级为“链验证 + 签发者白名单”并记 WARN，不拒死；被吊销证书（`CRYPT_E_REVOKED`）仍拒绝。

- 下载前校验 `FilePath` host 必须属于 `lenovo.com` / `lenovo.com.cn`（含子域），否则在请求第一个字节前拒绝；扩白名单按 `AGENTS.md` 走差异/理由/影响/测试/回滚/过期记录，缺一即缺陷。
- `-SkipHashCheck` 绕过伴生与官方 MD5，但仍强制非空文件与（可用时的）大小检查。
- `-SkipSignatureCheck` 仅用于诊断性未签名夹具。
- 下载遇 `403`：从当前/合并列表刷新 URL 后重试最多一次（有界两趟循环）。
- 过期的联想 CDN URL 在重试前从当前驱动列表刷新。

## 3. 安装器分发

| 扩展名 | 策略 |
| --- | --- |
| `.msi` | `msiexec.exe /i <file> /qn /norestart`；`3010/1641` 视为成功需重启 |
| `.inf` | 先 `DiInstallDriverW`（newdev.dll）；回退 `pnputil.exe /add-driver <file> /install`；退出码 `1` 视为需重启成功 |
| `.zip` | 展开、遍历提取的 INF，每个走原生优先 INF 安装 |
| `.cab` | `expand.exe <file> -F:* <dir>`，再原生优先 INF 安装 |
| `.exe` | 先静默安装器，超时/失败后提取回退 |

安装通道不使用 Windows Update。运行时原生-only：官方列表 → 校验下载 → 原生 `DiInstallDriverW`（INF），API 不可用时用系统原生 `pnputil` 回退；从不派生 PowerShell。

EXE 回退行为：

- 若 `<working-dir>\<DriverCode>.log` 钉住 `is-*.tmp` 目录，优先用该目录。
- 无日志证据时，只对 NVIDIA 驱动名扫描近期 `is-*.tmp` 根，且只取含 `Display.Driver` 的目录。
- 回退跑 `setup.exe` 或 `nvsetup.exe`，INF 为最后选项。
- 静默 EXE 失败且无可用提取回退时，CLI 提供 `r`（交互重跑）或 `s`（跳过）。

进程全部受超时约束，超时用 `taskkill.exe /PID <pid> /T /F` 杀整棵进程树。

## 4. 回退语义

回退是**设备级**操作。`pnputil /delete-driver oemN.inf /uninstall /force` 是包清理，不把设备切回旧驱动（删除活动包可能让设备失去当前驱动），绝不进入自动回退路径。

- 首选 `DiRollbackDriver`（newdev.dll，设备管理器“回退驱动程序”同一原语），逐实例 ID 回退；无备份（`ERROR_NO_MORE_ITEMS`）时不动设备记 WARN。
- 无备份回退 fallback 用 `UpdateDriverForPlugAndPlayDevicesW` + `INSTALLFLAG_FORCE`（`install.ForceReinstallINF`），是唯一无需先删新包即可强制绑回旧驱动的原语。**不用** `DiInstallDriverW` / `pnputil /add-driver /install` 降级——它们对已绑定更新驱动的设备会保留新驱动不动，甚至返回成功却不换回旧驱动，造成假 `RolledBack`。
  - [实测] `UpdateDriverForPlugAndPlayDevicesW` 用 `%SystemRoot%\INF\oemN.inf`（发布副本）作为 `FullInfPath` 通过 API 的 INF 路径检查：合成 hardwareID `ROOT\PROBE_NONEXISTENT_*` 下返回 `ERROR_NO_SUCH_DEVINST`（找不到设备）而非“找不到 INF”，对照 missing-INF smoke 测试返回 `Unable to find INF path`，证明路径解析关已过。发布副本与 FileRepository 原件逐字节一致（`oem47.inf` vs `acpivpc.inf_amd64_162ec7a9318c8e40`，SHA256 相同），内容等价。剩余未闭合：真实设备 + 真实 hardwareID 的完整重装属破坏性演练，默认不动，留待设备出现明确问题时授权执行。
- 装前快照 = `LocalVersion` + `BeforeInfName` + `BeforeInfPath`（完整旧 INF 路径）+ 每设备 `HardwareID`。重装旧 INF 前先 `os.Stat` 确认旧 INF 仍在（审计先于状态变更），缺失/未捕获则不动作记 `RollbackFailed`，不制造半回退。

### offer 与因果门

- 装后复核仅在“装前无问题码、装后有”时生成回退 offer（`rollbackCausalBasis` 返回 `new`）；装前就有问题码的设备只报 `attribution=pre-existing`，不 offer 回退。
- offer 以 WORM 账本为唯一权威：先写 `RollbackOffered` 行；`-Rollback` 仅对“有 `RollbackOffered` 且无后续 `RolledBack`/`RollbackFailed`”的驱动动作；`lenovo_driver_rollback.json` 是 GUI 展示缓存，不是权威。
- `-Rollback` 在派生 pending 前先 `verifyHistoryChain`；断链记 WARN（不硬阻断，个人场景需操作者自决）。回退动作前先追加 `Rollback` 意图行，写失败则不动作。

### 回退结果诚实

无重启时，动作成功后重读问题码与驱动版本：

- 问题码归 0 **且** 版本已偏离 `AfterVersion` → `RolledBack (recovered)`。
- 仍报问题码 → `RollbackFailed (still reports problem N)`。
- 问题码归 0 但版本未变 → `RollbackFailed (driver unchanged)`（不把自愈/巧合误归功于回退）。
- 重启未决 → `RolledBack (reboot required; recovery unverified)`。

部分设备成功记 `partial`，`Devices[]` 逐设备记 `State`，终态冻结不重投。回退成功判据 = 问题码恢复 **且** 驱动已变，不是版本回退字符串、不是 API 返回。

### 安装意图审计

安装路径在每次安装动作前先写 `Install`（`install requested`）意图行，写入失败则跳过该驱动安装（不提交）。`Installed`/`Failed`/`Deferred` 是动作结果，`Install` 是动作前的决定。

## 5. 接口漂移与逃生舱

- `ContractDriftError`：返回非空列表但解析后 0 行（`FileName`/`FilePath` 必需字段缺失）→ 报“接口结构变了”，区别于“真的无驱动”；首选源漂移降级到备用源时记 WARN。空列表仍按“无驱动”处理，不误报漂移。
- `InterfaceGateError`：HTTP 非 2xx、响应非预期 JSON、或顶层字段全空（`{"code":401}` 类认证改写）→ 报“接口被认证/封禁/限流”，区别于字段漂移，不静默降级为“无驱动”。
- 逃生舱：上次成功获取的驱动列表持久化到 `driver_list_<osid>.json`（UTC 时间戳），两个接口都失效时降级用缓存并标注过期，不直接归零。
