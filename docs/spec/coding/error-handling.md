# Error Handling

> How failures are represented, logged, and surfaced.

---

## Overview

Go functions return explicit errors instead of silently continuing. Fallback
paths are narrow and documented: for example, QuickFix falls back to the
webpage API, and a 403 CDN URL is refreshed from the current official list
once. The former PowerShell implementation has been removed after the Go
migration was verified.

---

## Error Types

退出码的权威是 `docs/spec/contracts.md`（门禁步骤 `CLI exit codes match docs contract` 锁定）。本文件不复述那张表，只写类型语义。

- **HTTP 状态**：`download.httpStatusError` 保留状态码，让编排层能做窄范围重试判断。它是未导出的；调用方的公开面是 `download.IsHTTPStatus`，编排层实际用的就是它。
- **接口漂移**：`api.contractDriftError` 表示非空列表解析出零行（必填字段缺失）——“接口结构变了”，与“确实没有驱动”不同。未导出：它在 `internal/api` 内部产生并判定。
- **接口闸门**：`api.interfaceGateError` 表示 HTTP 非 2xx、非 JSON、或所有期望字段为空——“接口被限流/阻断”，与字段漂移不同，且绝不静默降级成“没有驱动”。同样未导出。
- **需要重启的成功**：安装器退出码 `3010` 与 `1641` 视为成功并附重启警告。
- **超时**：`install.ProcessResult.TimedOut` 在进程树因超时被杀时置位。

---

## Error Handling Patterns

- Fail early with an actionable message for machine/API failures.
- Use typed errors only where the caller needs the type to choose a fallback.
- Log failures with `ERROR` before returning or accumulating them.
- Keep download and installer outcomes in `success` / `failed` collections and
  decide the process exit code from those collections.
- Do not silently retry a generic EXE failure. Ask the user whether to run it
  interactively before marking the driver failed.

---

## Common Mistakes

- Returning an empty value without an error when the caller cannot distinguish
  a valid empty result from a failure.
- 改动退出码语义（`0` 成功、`1` 一个及以上失败、`2` 非法 flag 组合、`3` GUI 驱动码不匹配）。
- Adding a generic silent retry for unknown installer families; this hides the
  failure and can leave the user with no actionable path.
