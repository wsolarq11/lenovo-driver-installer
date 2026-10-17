# 六条不变量

部署形态已定为**个人单机审计脚本**。产品尾（自签名、支持矩阵、安装器/卸载器、升级、遥测、品牌合规）不在本规格内，保持冻结。

六条不变量是审计脚本规格的不可回退约束。产品能力（签名、升级、遥测）只能作为核心之外的层加入，不得违反任何一条。

三处出现，但权威关系固定：`AGENTS.md` 是清单权威（硬门禁，CI 阻断），本文件是语义展开的唯一权威（每条“禁止什么、在哪里强制”）；`docs/decisions/distribution-frozen.md` 里的六条是决策时的引述（冻结，不随语义演进）。改语义只改本文件，改清单只改 `AGENTS.md`，决策记录不参与演进。

## 1. 确定性

- 约束：CLI 引擎零遥测、无隐藏联网、无后台服务。
- 强制点：门禁步骤 `engine zero-network import (local side-effect packages)`——`internal/install`、`internal/compare`、`internal/inventory`、`internal/plan`、`internal/audit` 不得 import `net/http` / `net/url`，只有 `download` / `api` 能碰网络。运行时只派生到原生 Windows API 与联想官方接口，从不派生 PowerShell。该步骤的检查体在 `internal/gate`（由 `cmd/gatecheck` 驱动、`scripts/verify.ps1` 逐条调用），用 AST 判定导入而不是匹配源码文本。
- 强制点：门禁步骤 `the architecture doc names the network boundary the imports show`——`docs/spec/architecture.md` §2.1 声明的联网包集合必须**等于**实际 import 网络库的包集合（双向比对，多一个少一个都红）。规格是给人读的那一半，代码是给编译器读的那一半，两边曾漂移：`architecture.md` 把 `api` 列进“确定性包”，而 `api` import `net/http`。
- 强制点：门禁步骤 `the running Go toolchain is the one the module pins`——跑门禁的编译器必须**等于** `go.mod` 的 `toolchain` 指令所钉的版本，否则直接 FAIL 并点名。理由不是惯例而是实测：门禁的覆盖率下限读自 `go test -cover`，而 Go 的语句插桩在补丁版本之间会变——同一份源码、同一台机器，`go1.27.0` 把 `internal/trust` 量成 45.8%（33/72 条语句），`go1.27.2` 量成 34.5%（19/55 条），一字未改。于是「下限」不再是阈值而是随编译器漂移的数：以修测试前的测试集为例，完好值在两工具链上是 45.8% / 34.5%，删掉注入测试后的腐化信号是 38.9% / 25.5%，于是**不存在**一个同时高于腐化信号（最大的 38.9%）又低于完好值（最小的 34.5%）的数——任何下限都可能在一台机器上假绿、在另一台上假红。确定性要求同一个提交给出同一个判定，所以先钉编译器再谈下限。注意 `toolchain` 指令语义是下限（本地更新的工具链仍胜出），精确钉靠这条断言而非指令本身；CI 侧由 `setup-go` 的 `go-version-file: go.mod` 从同一处读取，避免出现第二份版本真相。

## 2. 证据优先

- 约束：计划文件保留原始证据行；WPF 只做薄渲染，不折叠“待定”与来源审计。
- 强制点：`Source audit` 输出类别 + 结论 + 证据行，`setupapi.*.log` 的 `cmd:` 原始命令行原样进入计划。WPF 不实现比较/来源逻辑，只渲染 Go 引擎的 JSON。
- 强制点：门禁步骤 `the API contract fixtures are recorded responses`——`internal/api/testdata/web_real_82jq_os42.json` 必须是真实抓包：保留 `statusCode`/`localOSID`/`osList`/`partList`/`drivelist` 信封、两个 OS 字段为未加引号的 JSON 数字、带 `PartName`，且不得小于 4 KB。手写样例无法冒充。这条防的是一个已经发生过的具体失效：网页源的 `statusCode`/`localOSID` 被声明为 `string` 而线上是数字，整条 web 链路从未解析成功，而手写夹具把数字写成带引号字符串，让测试与结构体一致、与服务器不一致。

## 3. 默认不动

- 约束：`Local newer`、未观察到设备问题、证据不足，一律不进自动安装集。无问题码只表示“未观察到问题”，不写成“正常”。
- 自动安装集的成员资格是**显式白名单**，由 `model.InAutomaticInstallSet` 单点声明：只收 `Update` 与 `Not installed`。`Local newer`（装机即降级）与 `Up to date`（装了不起变化）被排除，`Not applicable`（硬件未检出）本就无关，`Unknown` 证据不足也不收。任何新增状态必须在这里表态，不许用宽松比较默认放行。
- 排除只约束**无人值守的自动集**。处于这两类状态的驱动仍可被显式选中（交互 `s`、`-GuiInstallCodes`）——那是操作者的判断，不是工具的判断。
- 强制点：`CM_Get_DevNode_Status` 问题码为 0 只表示未观察到问题，不证明功能正常；计划不把“无问题码”写成“正常”。来源审计的 provenance 归因是**推断**级（本机实测 `setupapi.*` 日志会轮转截断、历史导入行不可复现，无法事后对照 DriverStore ground truth），不得当事实展示。
- 强制点（门禁步骤 `automatic install set excludes downgrade and no-op statuses`，检查体在 `internal/gate`）：`InAutomaticInstallSet` 必须是 `==` 白名单，函数体出现 `!=` 即门禁失败——按语法树判定，因此解释规则的注释不会误触；`view.go` 不得用宽松比较划分自动集；交互提示不得再宣称“all applicable”。真机证据：82JQ 修复前 `Applicable candidates: 11`，其中 9 个 `Local newer`（含 AMD VGA `30.0.14052.9003` → `27.20.15026.8004`、NVIDIA `31.0.15.4630` → `31.0.15.2799`），按 `a` 即降级；修复后为 `1`。

## 4. 事实分级

- 约束：计划、控制台、交互提示、GUI 导出统一输出 fact / inference / undetermined。
- 分级是**三档**：实测 fact / 推断 inference / 待定 undetermined。当前状态集上 `inference` 无成员——`StatusEvidenceBasis` 只返回 `fact` 与 `undetermined`（`NotInstalled`/`LocalNewer` 的依据是硬件 ID 命中与实测版本算术比较，两者都是测量；见 `docs/records/evidence-82jq.md` 2026-09-30 条）。任何通道不得把推断或待定渲染成事实；也不得向读者解释一个代码已不产出的等级。
- 强制点（门禁步骤 `summary notes only describe reachable statuses`）：计划汇总不得再出现 `multi-vendor`（PS 时代的复合版本解析失败路径，`ResolveComparableVersion` 加固后已不产生）一揽子归因，也不得再打印 `inference = ...` 一览（`StatusEvidenceBasis` 无该级）；`Unknown` 逐行注真实原因（Provisioned 占位符 / 官方行无版本 / 版本不可比）。
- 强制点（门禁步骤 `plan blocks carry the reason the console states`）：持久化的 `lenovo_driver_plan.txt` 必须与瞬态通道（控制台汇总、GUI 导出）承载同等的逐行理由——`BuildPlanText` 输出 `NonMatchReason`/`Unknown` 原因。最耐久的产物证据最少的旧形态违反证据优先。

### 2.1 静默安装：开关派发只由端到端验证决定

- 规则：**一个 `.exe` 包能否无人值守，取决于「静默开关是否端到端验证过」，不是「是否认出了安装器家族」。** 家族识别与开关验证是两个正交证据，只有后者能授权无人值守运行。
- 理由：82JQ 实测证伪了两件事。其一，厂商 `Parameter` 列是伪造的：`DRV202102040007` 声明 `-QuietInstall`，而 `AMD-2GY501AFHN99VBC0.exe` 字节里**没有** `-QuietInstall` 的任何拼写（ASCII 与 UTF-16LE 双扫均不命中），却**有** `Inno Setup Setup Data` 标记。其二，认出 Inno 不等于验证了 `/VERYSILENT`：该开关从未在真机端到端跑通过（真机只证明过「包装了、驱动绑上了」，没证明过「无人在场时开关生效」）。信任未验证开关的后果：安装器弹窗、操作者手工点完、机器零变化，退出码 0。
- 公式（`internal/install/installer_family.go`，单一出口）：
  - `.inf`/`.zip`/`.cab` → **无人值守**：工具直接把 INF 交给 pnputil / `DiInstallDriverW`，该路径无 GUI，**无需开关即无开关可错**。
  - `.exe` → **交互**：带 GUI 的安装器需要静默开关，而 82JQ 上**没有任何 `.exe` 静默开关端到端验证过**（`⊥`），`⊥` 一律落到不动作分支。家族仍从包自身字节识别并写进证据，但识别≠验证。
- 顺序是规格的一部分：这些包都是自解压壳，一个二进制会同时命中多个标识，而跑起来的是包装器，所以包装器标识排在载荷标识之前；但命中只决定「证据里写哪个家族名」，不决定「是否派发开关」。
- 禁止项：**不得给任何 `.exe` 家族下发静默开关**，直到某家族开关在真机端到端验证为 `fact`。安装器不认识的开关是静默 no-op——进程退出 0 而什么也没变，这正是「报假成功」的成因。
- 强制点：门禁 `silent install dispatches on the package, not on the vendor column` 禁止重新出现任何返回 `[]string` 字面量的开关数组（按语法树判定，检查体在 `internal/gate`），并要求 `familyMarkers` 仍在（拒绝证据必须能命名家族）；测试 `TestSilentPlanRefusesUnverifiedInno`、`TestSilentPlanRefusesEveryUnverifiedFamily`、`TestSilentPlanEvidenceNamesTheRefusedFamily`、`TestSilentPlanEvidenceAgreesWithTheDecision`、`TestInstallDriverFileEXEIsTerminal`。
- 证据出口：拒绝决定必须落到账本 `Message` 列（散文，命名家族与「interactive」），**绝不**写进 `VerifiedVersion`/`BeforeVersion` 列——那两列是版本，机制写进去就把「Inno Setup 标记」伪装成版本声明。强制点：门禁 `the silent mechanism reaches the ledger as prose, never as a version`（检查体在 `internal/gate`；`install_silent.go` 必须调 `install.SilentPlanFor`，`Installed` 行含 `silent via`，且证据变量不得作账本写入的裸位置参数——按调用实参判定，不靠行文本）。
- 已知弱点：**当前没有任何 `.exe` 静默开关端到端验证过，所以全部 `.exe` 走交互。** 真机只证明过「INF 载荷包（Inno 自解压，`[Run]` 是 pnputil）装了且驱动绑上」，但那是操作者点完向导的路径，不构成 `/VERYSILENT` 的无人值守证据。将来要恢复某个家族，前提是一次可复现的真机钻探：无人值守运行 + 装后复核 `bound` + 记录退出码与字节证据。


### 3.1 自动安装集必须由实测证据支撑

- 规则：**凡可进入自动安装集的状态，其 `StatusEvidenceBasis` 必须是 `fact`。** 自动安装集是唯一无人值守地改变机器状态的路径；集内出现 inference 或 undetermined，等于让工具依据没测到的东西动手。
- 含义：`Not installed` 必须是「硬件 ID 命中的设备没有绑定驱动版本」这一**实测**，而不是「在软件列表里没查到」这一证据缺失。软件侧查不到时必须回退到设备实测版本。
- 强制点：`TestAutomaticSetStatusesAreMeasured`（正）+ `TestNonFactStatusesStayOutOfTheAutomaticSet`（反）；门禁 `the automatic install set is backed by measurements only`（检查体在 `internal/gate`）防止测试被删除以放行改动——它按语法树确认测试**声明**存在，注释里提到测试名不再能顶替测试本身。

### 4.1 比较只有一个原语，且「未决」是独立值

- 规则：**版本比较的结果有四个值，不是三个：`<`、`=`、`>`、**`⊥`（未决）**。`⊥` 必须独立于 `<`，不得折叠进任何方向。** 系统内所有版本判断——状态判定、装后复核、跨 edition 选择——都必须经由同一个原语 `compare.Order`，禁止调用方各自实现（尤其禁止原始字符串相等）。
- 理由：`⊥` 折叠成 `<` 就是"不可测量的值冒充事实"。`Version.Compare` 历史上把 `nil` 当作"比任何东西都小"，于是解析不出版本的驱动会被报成"落后于列表"并进入自动安装集——与 `Provisioned` 占位符冒充 `Up to date` 是同一族缺陷。`Provisioned` 正是靠这条修掉的：它既不能证明已装，也不能证明未装（系统确实为它持有一个 `.ppkg`），所以判 `StatusUnknown`，由 §3.1 的事实级白名单挡在自动集之外。改成 `Not installed` 是错的，那等于在系统持有包的情况下断言它不存在。
- 前置：两侧必须先**归一到同一个可比分量**。厂商 `Version` 字段在 82JQ 两个 edition 的 47 行里有 49 种形态：裸版本（`31.0.15.4630`）、带前缀（`Intel_22.10.0.7`）、带后缀（`15.11.29.13 MS signed`）、**组合串**（`Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314`，一个字段三个包）、以及非版本（`Inbox`）。`compare.ResolveComparableVersion` 负责按 vendor 选出对应分量。**不做这一步的直接后果**：装后复核原本用原始字符串相等，而 WLAN 那些行的 `Version` 是组合串，任何设备的本机版本都不可能等于它——**这些驱动永远无法被判定为 `bound`**，装没装成都报"没变化"。
- 强制点：门禁 `one comparison primitive decides every version question`（检查体在 `internal/gate`）要求 `VersionOrder` 四值齐全（含块式 `const (...)` 声明）、`Order` 对缺失版本先返回 `OrderUndecided` 再谈方向、`CompareDriverStatus` 不得直接调 `Version.Compare`、`installBindingLabel` 不得出现 `== packageVersion` 一类原始字符串比较。注：直接调 `Version.Compare` 这一条现在**编译器也会挡**（返回值类型不匹配），门禁仍保留以覆盖参数顺序写反等能编译的变体。

## 5. 审计先于状态变更

- 约束：历史账本追加写入，写入失败不提交成功结果。
- 强制点：安装前先写 `Install` 意图行、回退前先写 `Rollback` 意图行，写入失败则跳过该动作（不提交）。账本每行带 SHA-256 链哈希（`Hash` 结构性尾列），`verifyHistoryChain` 重算可证伪篡改/乱序/删除；回退动作派生 pending 前先校验链。
- 强制点（设备身份与 schema 诚实）：账本每行在 `devices` 列记录本行动作触及的 PnP 设备实例 id，使“本工具对哪个驱动动手”与设备实际变化可按身份 join，下游不从自由文本解析。账本表头即 schema：在盘表头缺当前列时**拒绝写入**并给出处置指令，不追加更宽的行（旧行不可重写，宽行会让读取端把值绑到错误列名、并让链哈希位置静默错位）。链哈希按表头列名定位，不按当前列数定位。

## 6. 回退语义

- 约束：设备回退是设备级操作（回退驱动程序或重装旧 INF）；`pnputil /delete-driver` 是包清理不是回退，不得作为自动回退动作。
- 强制点：回退优先 `DiRollbackDriver`（设备管理器“回退驱动程序”同一原语），无备份时用 `UpdateDriverForPlugAndPlayDevicesW` + `INSTALLFLAG_FORCE` 强制绑回旧 INF；回退成功判据 = 问题码恢复 **且** 驱动版本已变，不是 API 返回值。包清理原语（携带 `/delete-driver` 参数向量的 `RemoveDriverPackage`）已从代码中删除，并由门禁步骤 `package cleanup is never presented as rollback` 钉住其缺席：`internal/` 全仓（含测试）不得出现该参数向量，同时断言设备级回退入口 `DiRollbackDriver` 仍在——拒绝包清理只有在真回退路径存在时才有意义。
