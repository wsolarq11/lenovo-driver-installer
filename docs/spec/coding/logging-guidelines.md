# Logging Guidelines

> How the installer logs operations and results.

---

## Overview

`App.Log` 把每行写入稳定审计目录并镜像到控制台。目录由 `internal/app/app.go` 的
`artifactBaseDir` 决定：优先用户缓存根，不可用时退回 `%TEMP%`。WPF 子进程把
stdout/stderr 重定向到临时文件，由 WPF 轮询显示。

---

## Log Format

```text
[2026-08-24 15:00:00] [INFO] Lenovo category ID : 123
```

The format is `[timestamp] [LEVEL] message`.

---

## Log Levels

| Level | Use |
|-------|-----|
| `INFO` | Normal lifecycle events: machine, OS, selected drivers, downloads, installs |
| `WARN` | Recoverable or noteworthy events: cache mismatch, reboot required, fallback used |
| `ERROR` | Failures that affect the result: download failure, install failure |
| `INSTALL` | Captured installer output from pnputil or inner installers |

---

## Artifacts

产物路径的权威是 `docs/spec/contracts.md`（由门禁步骤 `CLI artifact paths match docs contract` 锁定）。本文件不重复那张表。

只记一条语义：日志、计划、账本都落在稳定审计目录下，因此不会随 TEMP 清理消失；计划仍是每次运行覆写的视图，不是持久账本。

---

## What To Log

- Machine model, serial number, OS match key.
- Lenovo category ID and matched OS entry.
- Driver counts and comparison status.
- Plan file path, download directory, download URL refresh events.
- Installer exit codes, fallback paths, and post-install recheck results.

---

## What NOT To Log

- User-entered interactive responses.
- Full installer binaries or sensitive local file contents.
- Secrets or credentials if any are ever introduced.
