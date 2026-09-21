# 回退行为验收（VM 快照化测试）

> 状态：本机决定**不做 VM**。本文保留为将来真机/一次性环境验证的现成入口，暂不执行。
> 不执行期间的诚实底线由代码内的"回退后复核"保证：降级若静默 no-op，账本记 `RollbackFailed`，不记假 `RolledBack`。

离线门禁（`scripts/verify.ps1`）只能证明"实现正确"，不能证明"装坏之后救得回来"。
本页描述的行为验收在**一次性 VM** 里真正跑一遍"装驱动 → 出问题 → 回退 → 回到旧版本"，
验证的是**结果**，不是实现。

## 为什么必须用 VM

- 真实回退会改动真实设备的驱动状态，违反"默认不动"，且不可逆。
- `DiRollbackDriver` 的备份前提、重启语义、Windows Update 竞态，都只能在真状态机上暴露。
- 快照让"故意装坏"变成零成本的、可重复的、可丢弃的操作。

## 前置

- 一台 Win10/11 VM（Hyper-V / VMware / VirtualBox），已装好快照。
- VM 内已 build 出 `bin\lenovo-driver.exe`，且以管理员运行 harness。
- 选一个"装坏之后能轻易回滚、且你不在乎"的驱动（读卡器、摄像头、旧版声卡等）。
  先跑 `-SkipRollback` 确认它真的能制造问题码并产生 offer。

## 运行

```powershell
.\scripts\rollback_behavior_test.ps1 `
  -DriverCode <code> `
  -SnapshotCommand { Hyper-V\Checkpoint-VM -Name w10test -SnapshotName before } `
  -RestoreCommand { Hyper-V\Restore-VMCheckpoint -Name w10test -SnapshotName before }
```

先只确认装坏 + offer，不做回退：

```powershell
.\scripts\rollback_behavior_test.ps1 -DriverCode <code> -SkipRollback `
  -SnapshotCommand { ... } -RestoreCommand { ... }
```

## 断言什么

1. 安装后 `lenovo_driver_rollback.json` 存在且含 `pending` offer。
2. 回退后 offer 状态 ∈ `rolled_back / partial / failed`。
3. 账本出现 `RollbackOffered`、`Rollback`、以及 `RolledBack`/`RollbackFailed` 行。
4. 回退前后 dry-run 的 LocalVersion：**人工确认**本地版本回到了安装前的值。
   （第 4 条是唯一无法自动判定的一步，因为"回退是否真正生效"需要人工读版本号或重启后复核。）

## 仍未覆盖的残余风险（诚实声明）

- **重启后才提交**：回退返回 `reboot required` 时，dry-run 读到的仍是坏驱动版本，
  要到重启后才会回到旧值。harness 在重启前读到的 after 版本可能不反映真实结果。
- **Windows Update 竞态**：VM 若联网，WU 可能中途替换驱动，污染 before/after 读数。
  建议 VM 断网或禁用驱动自动更新后运行。
- **实例 ID 漂移**：装坏后设备重新枚举可能产生新实例 ID，导致 offer 里的旧 ID 定位失败。
  这正是 harness 要暴露的场景之一。

## 结果判读

| 现象 | 结论 |
| --- | --- |
| offer 未产生 | 该驱动未制造问题码，换一个真能装坏的驱动 |
| `RolledBack` 但 after 版本未变 | 命中"重启才提交"或"回退未真正绑定"，需重启后复核 |
| `RollbackFailed` + `previous INF missing` | 旧 INF 已被清理，诚实失败（符合设计） |
| `partial` | 部分设备回退成功、部分失败，账本按设备记录 |
