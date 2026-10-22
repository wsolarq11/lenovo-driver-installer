# 质量规范

> 什么时候算写对了。

## 分层的唯一分类表

包属于确定性 / 联网 / 副作用哪一类，只由 `docs/spec/architecture.md` §2.1 定义，并由门禁步骤
`the architecture doc names the network boundary the imports show` 与
`engine zero-network import (local side-effect packages)` 双向锁定。本文件不复述分类，避免第二份清单漂移。

## 必须遵守

- 保留公开参数、交互选项、退出码、下载缓存规则、安装器回退。
- 编排代码通过 `App.Log` 记日志，不直接打印。
- 同一段非平凡逻辑出现两次就抽助手：`download.IsHTTPStatus`、`install.TestRebootExitCode`、`pathutil.Base`。
- 软件类驱动版本只从已安装应用/服务证据解析；同厂商 PnP 设备不能当软件包的证明。
- 联想包可含多个厂商/INF 驱动集。安装器退出码 `0` 只说明包跑完了，不说明包声明的版本替换了活动驱动。记录真实的安装前后本地版本，不得把未变化的本地版本归因给该包。
- 来源归因只用真实证据：安装历史、`setupapi.*.log` 导入记录、当前/备用官方映射、活动设备与 DriverStore 属性。`setupapi` 解析保持纯函数；编排层只读日志并投喂证据。证据缺失时不猜来源 OS。
- 注释只解释不明显的联想 API 或 Windows 行为，不逐行注释。

## 禁止

- 给确定性包加网络、注册表、PnP、文件、控制台、进程访问。
- 不更新部署契约就新增第二个运行时语言入口。
- 未经评审新增外部依赖。
- 对未知安装器家族用通用参数静默重试。
- 吞掉错误且不记日志。
- 改动退出码语义或默认的“只查当前 OS”行为。
- 在两个位置各写一份同一个可枚举事实。

## 验证

```powershell
.\scripts\verify.ps1
```

`docs/howto/develop.md` 是开发命令、门禁强制项与 smoke 调用方式的唯一权威；本文件不复述那张清单。

## 评审清单

- [ ] CLI 参数与帮助文本来自 `internal/app/help.go`；`docs/spec/contracts.md` 是门禁锁定的指针，不手工同步；`README.md` 不是源。
- [ ] 新代码落在既有包的职责内。
- [ ] 没有复制尺寸/超时/重启码/路径逻辑。
- [ ] 没有新增静默重试或行为扩张。
- [ ] 改变行为的新公开函数有对应测试。
- [ ] `go vet ./...` 与 `gofmt` 干净。
- [ ] `git diff --check` 干净。
