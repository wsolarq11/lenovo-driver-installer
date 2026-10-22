# Cross-Layer Thinking Guide

> **Purpose**: Trace a change across every layer boundary it touches before
> implementing. Most silent drift happens at boundaries, not inside a layer.
> The concrete contracts live in `docs/spec/` (architecture / contracts /
> behavior) — this guide is only the trigger list.

---

## The Layers

```text
GUI 表现层 (internal/gui, mygo 原生 UI, 薄渲染)
   |  引擎出口门面 (internal/app/gui.go)  /  DriverCode 意图回传
Go 引擎 (确定性包 ↔ 副作用包)
   |
   +-- 联想 API (唯一归一化点)
   +-- Windows 系统 (SetupAPI / DriverStore / 注册表 / SMBIOS)
   +-- 文件产物 (WORM 账本 / 计划 / JSON / 缓存)
```

---

## Boundary Triggers — Ask Before Editing

1. **GUI ↔ 引擎**: does this change the engine's exported face
   (internal/app/gui.go) or the row projection? The GUI must stay a thin
   renderer — no matching, attribution, or install logic. →
   `docs/spec/contracts.md`, `docs/spec/architecture.md`.
2. **deterministic ↔ side-effect**: does it move logic across the boundary?
   Deterministic packages never touch network / registry / PnP / console /
   process / file APIs. → `docs/spec/architecture.md` §2.1（分类唯一表）与 `docs/spec/coding/quality-guidelines.md`.
3. **Go ↔ Lenovo API**: does it change how raw JSON is parsed or classified?
   Drift (structure changed) and gate (blocked / rate-limited) are distinct from
   "no drivers". → `docs/spec/behavior.md`.
4. **Go ↔ files / WORM ledger**: does it touch the ledger column order or the
   intent-row-before-state-change ordering? → `docs/spec/contracts.md`,
   `docs/spec/coding/single-source.md`.

---

## Checklist for Any Cross-Layer Change

- [ ] Mapped the full data flow before editing.
- [ ] Identified every boundary the change crosses.
- [ ] Confirmed the shared golden / gate still locks both sides (quoting, exit
      codes, artifact paths, ledger columns, JSON fields).
- [ ] Ran `scripts/verify.ps1` — the only place that catches a cross-layer
      drift mechanically.
