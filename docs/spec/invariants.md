# 六条不变量

部署形态已定为**个人单机审计脚本**。产品尾（自签名、支持矩阵、安装器/卸载器、升级、遥测、品牌合规）不在本规格内，保持冻结。

六条不变量是审计脚本规格的不可回退约束。产品能力（签名、升级、遥测）只能作为核心之外的层加入，不得违反任何一条。

三处出现，但权威关系固定：`AGENTS.md` 是清单权威（硬门禁，CI 阻断），本文件是语义展开的唯一权威（每条“禁止什么、在哪里强制”）；`docs/decisions/distribution-frozen.md` 里的六条是决策时的引述（冻结，不随语义演进）。改语义只改本文件，改清单只改 `AGENTS.md`，决策记录不参与演进。

## 1. 确定性

- 约束：CLI 引擎零遥测、无隐藏联网、无后台服务。
- 强制点：`scripts/verify.ps1` 的“零网络导入”检查——`internal/install`、`internal/compare`、`internal/inventory`、`internal/plan`、`internal/audit` 不得 import `net/http` / `net/url`，只有 `download` / `api` 能碰网络。运行时只派生到原生 Windows API 与联想官方接口，从不派生 PowerShell。

## 2. 证据优先

- 约束：计划文件保留原始证据行；WPF 只做薄渲染，不折叠“待定”与来源审计。
- 强制点：`Source audit` 输出类别 + 结论 + 证据行，`setupapi.*.log` 的 `cmd:` 原始命令行原样进入计划。WPF 不实现比较/来源逻辑，只渲染 Go 引擎的 JSON。

## 3. 默认不动

- 约束：`Local newer`、未观察到设备问题、证据不足，一律不进自动安装集。无问题码只表示“未观察到问题”，不写成“正常”。
- 自动安装集的成员资格是**显式白名单**，由 `model.InAutomaticInstallSet` 单点声明：只收 `Update` 与 `Not installed`。`Local newer`（装机即降级）与 `Up to date`（装了不起变化）被排除，`Not applicable`（硬件未检出）本就无关，`Unknown` 证据不足也不收。任何新增状态必须在这里表态，不许用宽松比较默认放行。
- 排除只约束**无人值守的自动集**。处于这两类状态的驱动仍可被显式选中（交互 `s`、`-GuiInstallCodes`）——那是操作者的判断，不是工具的判断。
- 强制点：`CM_Get_DevNode_Status` 问题码为 0 只表示未观察到问题，不证明功能正常；计划不把“无问题码”写成“正常”。来源审计的 provenance 归因是**推断**级（本机实测 `setupapi.*` 日志会轮转截断、历史导入行不可复现，无法事后对照 DriverStore ground truth），不得当事实展示。
- 强制点（`scripts/verify.ps1`）：`InAutomaticInstallSet` 必须是 `==` 白名单，函数体出现 `!=` 即门禁失败；`view.go` 不得用宽松比较划分自动集；交互提示不得再宣称“all applicable”。真机证据：82JQ 修复前 `Applicable candidates: 11`，其中 9 个 `Local newer`（含 AMD VGA `30.0.14052.9003` → `27.20.15026.8004`、NVIDIA `31.0.15.4630` → `31.0.15.2799`），按 `a` 即降级；修复后为 `1`。

## 4. 事实分级

- 约束：计划、控制台、交互提示、GUI 导出统一输出 fact / inference / undetermined。
- 强制点：四级（实测 fact / 推断 inference / 待定 undetermined）贯穿所有输出通道，任何通道不得把推断或待定渲染成事实。

### 2.1 静默安装按包自身证明的家族派发

- 规则：**下发给安装器的静默参数，必须由该包自身字节里的标识决定，不得由厂商列表的 `Parameter` 列决定。** 厂商列只在一种含义上可信——它声明载荷是什么（41/47 行声明 `/add-driver *.inf`，那是 INF 包装包，本就无需任何静默开关）；它不声明哪个开关能静默那个安装器。
- 理由：82JQ 实测证伪。`DRV202102040007` 声明 `-QuietInstall`，而 `AMD-2GY501AFHN99VBC0.exe` 的字节里**没有** `-QuietInstall` 的任何拼写（ASCII 与 UTF-16LE 双扫均不命中），却**有** `Inno Setup Setup Data`（Inno Setup 头标记，ASCII 命中）——证明该包是 Inno，而不是厂商列宣称的那个开关。信任该列的后果是：安装器弹窗、操作者手工点完、机器零变化，而退出码为 0。
- 公式（`internal/install/installer_family.go`，按优先级首次命中，无遗漏分支）：INF 载荷声明 → 无需开关；`!@Install@!UTF-8!` → 7-Zip SFX `-s`；`NullsoftInst` → NSIS `/S`；`Inno Setup Setup Data` → Inno `/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP-`；`.wixburn` → WiX Burn `/quiet /norestart`；`InstallShield` → **交互（开关未验证）**；**无命中 → 交互**。
- 顺序是规格的一部分：这些包都是自解压壳，一个二进制会同时命中多个标识，而**跑起来的是包装器**，所以包装器标识排在载荷标识之前。
- 禁止项：识别不出的包**必须**走交互，不得“猜一组参数发过去”。安装器不认识的开关是静默 no-op——进程退出 0 而什么也没变，这正是“报假成功”的成因。
- 强制点：门禁 `silent install dispatches on the package, not on the vendor column` 禁止 `strings.Fields(vendorParameter)` 重新出现，并逐族核对其仍下发该族自己的开关；测试 `TestSilentInstallerArgsUsesProvenFamilyNotVendorColumn`、`TestSilentInstallerArgsCoversEveryProvenFamily`、`TestSilentInstallerArgsSkipsFlagForINFWrapper`、`TestInstallEXEUnprovenFamilyIsTerminal`、`TestInstallEXERunsProvenPackageWithoutVendorColumn`。
- 证据出口：无人值守安装的决定依据必须落到账本 `Message` 列（散文，命名家族），**绝不**写进 `VerifiedVersion`/`BeforeVersion` 列——那两列是版本，机制写进去就把“Inno Setup 标记”伪装成版本声明。强制点：门禁 `the silent mechanism reaches the ledger as prose, never as a version`（`install.go` 必须 `install.SilentPlanFor(` 且 `Installed` 行含 `silent via`，证据变量不得作裸位置参数）；测试 `TestSilentPlanEvidenceNamesWhatAuthorisedIt`、`TestSilentPlanEvidenceAgreesWithTheDecision`。
- 已知弱点：`InstallShield` 一档目前**没有任何实测真身**，故按禁止项降级为交互——识别出 `InstallShield` 标记但不派发开关，账本记录“识别但未验证”。82JQ 唯一的 NVIDIA 包——驱动条目 `DRV202102040021`（本地 `31.0.15.4630`）由其安装器文件 `DRV202109090053_NVVGA-TVLC18AF407GA0.exe` 提供——按 `setupapi.dev.log` 解压到 `is-6UIF7.tmp`，而 `is-*.tmp` 是本项目 `extraction.go` 模型认定的 Inno Setup 临时目录；同批 7 个脚本启动过的包（蓝牙/AMD IO/Realtek 声卡与 LAN/Fn/AMD VGA/NVIDIA）全部走 `is-*.tmp`。故该包命中 Inno 分支，不命中 InstallShield。`/s /v"/qn /norestart"` 虽是文档值，**在真机验证该族之前不得派发**，也不得宣称某包是它的真身。

### 3.1 自动安装集必须由实测证据支撑

- 规则：**凡可进入自动安装集的状态，其 `StatusEvidenceBasis` 必须是 `fact`。** 自动安装集是唯一无人值守地改变机器状态的路径；集内出现 inference 或 undetermined，等于让工具依据没测到的东西动手。
- 含义：`Not installed` 必须是「硬件 ID 命中的设备没有绑定驱动版本」这一**实测**，而不是「在软件列表里没查到」这一证据缺失。软件侧查不到时必须回退到设备实测版本。
- 强制点：`TestAutomaticSetStatusesAreMeasured`（正）+ `TestNonFactStatusesStayOutOfTheAutomaticSet`（反）；门禁 `the automatic install set is backed by measurements only` 防止测试被删除以放行改动。

### 4.1 比较只有一个原语，且「未决」是独立值

- 规则：**版本比较的结果有四个值，不是三个：`<`、`=`、`>`、**`⊥`（未决）**。`⊥` 必须独立于 `<`，不得折叠进任何方向。** 系统内所有版本判断——状态判定、装后复核、跨 edition 选择——都必须经由同一个原语 `compare.Order`，禁止调用方各自实现（尤其禁止原始字符串相等）。
- 理由：`⊥` 折叠成 `<` 就是"不可测量的值冒充事实"。`Version.Compare` 历史上把 `nil` 当作"比任何东西都小"，于是解析不出版本的驱动会被报成"落后于列表"并进入自动安装集——与 `Provisioned` 占位符冒充 `Up to date` 是同一族缺陷。`Provisioned` 正是靠这条修掉的：它既不能证明已装，也不能证明未装（系统确实为它持有一个 `.ppkg`），所以判 `StatusUnknown`，由 §3.1 的事实级白名单挡在自动集之外。改成 `Not installed` 是错的，那等于在系统持有包的情况下断言它不存在。
- 前置：两侧必须先**归一到同一个可比分量**。厂商 `Version` 字段在 82JQ 两个 edition 的 47 行里有 49 种形态：裸版本（`31.0.15.4630`）、带前缀（`Intel_22.10.0.7`）、带后缀（`15.11.29.13 MS signed`）、**组合串**（`Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314`，一个字段三个包）、以及非版本（`Inbox`）。`compare.ResolveComparableVersion` 负责按 vendor 选出对应分量。**不做这一步的直接后果**：装后复核原本用原始字符串相等，而 WLAN 那些行的 `Version` 是组合串，任何设备的本机版本都不可能等于它——**这些驱动永远无法被判定为 `bound`**，装没装成都报"没变化"。
- 强制点：门禁 `one comparison primitive decides every version question` 要求 `VersionOrder` 四值齐全、`Order` 对缺失版本先返回 `OrderUndecided` 再谈方向、`CompareDriverStatus` 不得直接调 `Version.Compare`、`installBindingLabel` 不得出现 `== packageVersion` 一类原始字符串比较。

## 5. 审计先于状态变更

- 约束：历史账本追加写入，写入失败不提交成功结果。
- 强制点：安装前先写 `Install` 意图行、回退前先写 `Rollback` 意图行，写入失败则跳过该动作（不提交）。账本每行带 SHA-256 链哈希（`Hash` 结构性尾列），`verifyHistoryChain` 重算可证伪篡改/乱序/删除；回退动作派生 pending 前先校验链。
- 强制点（设备身份与 schema 诚实）：账本每行在 `devices` 列记录本行动作触及的 PnP 设备实例 id，使“本工具对哪个驱动动手”与设备实际变化可按身份 join，下游不从自由文本解析。账本表头即 schema：在盘表头缺当前列时**拒绝写入**并给出处置指令，不追加更宽的行（旧行不可重写，宽行会让读取端把值绑到错误列名、并让链哈希位置静默错位）。链哈希按表头列名定位，不按当前列数定位。

## 6. 回退语义

- 约束：设备回退是设备级操作（回退驱动程序或重装旧 INF）；`pnputil /delete-driver` 是包清理不是回退，不得作为自动回退动作。
- 强制点：回退优先 `DiRollbackDriver`（设备管理器“回退驱动程序”同一原语），无备份时用 `UpdateDriverForPlugAndPlayDevicesW` + `INSTALLFLAG_FORCE` 强制绑回旧 INF；`RemoveDriverPackage` 仅作为包清理原语保留，注释明确“不是回退”。回退成功判据 = 问题码恢复 **且** 驱动版本已变，不是 API 返回值。
