# Code Reuse Thinking Guide

> **Purpose**: Stop and ask "does this fact already have a single source?" before
> writing new code or changing a value. The concrete machine-source map lives in
> [../single-source.md](../single-source.md) — this guide is only
> the trigger list.

---

## The Problem

The most expensive drift in this repo is one fact authored in several places: a
later edit updates one copy and misses the others, silently. The recorded
instances (CSV column order, Go↔WPF argument quoting) were exactly this shape.

---

## Before Writing New Code — Ask

1. **Is this value already declared?** Exit code, artifact path, flag name, CSV
   column order, JSON field — if it exists, reference it; don't redeclare it.
   → See [../single-source.md](../single-source.md).
2. **Does a helper already do this?** Search before writing: HTTP-status checks,
   reboot-code checks, path helpers all exist.
3. **Am I copying logic into a second language?** If the same behavior must live
   in Go and PowerShell, it needs a shared golden — before adding a third copy.

---

## When to Abstract

- **Abstract** when the same non-trivial logic appears 3+ times.
- **Do not abstract** a one-line literal that a golden already locks — the lock
  is the cheaper control than an indirection.

---

## Checklist Before Commit

- [ ] Searched for an existing helper before writing new logic.
- [ ] No value redeclared that already has a machine source.
- [ ] Any Go↔PowerShell duplication is covered by a shared golden.
- [ ] Doc copies updated to match the machine source when a path / exit code /
      column changed.
