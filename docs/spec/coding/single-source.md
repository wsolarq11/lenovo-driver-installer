# 单一源

> 每个可枚举事实存在哪，机器源是什么，哪条门禁锁住人读副本。

## 规则

一个可枚举事实（退出码、产物路径、列顺序、JSON 字段、flag 名）只有一个机器源。其它每一份副本要么被门禁锁死，要么删除。“记得同步”不是控制手段。

## 机器源映射

映射表本身——每个可枚举事实的机器源、人读副本与锁定门禁——是 `docs/spec/index.md` 的「主题地图」。它放在那里，因为 `.trellis/` 整个目录被 gitignore：克隆里不存在的目录不能承载唯一权威，指向它的已跟踪文档会立刻是断链。

本文件只保留规则、变更规程与反模式，它们是写给实现者的指令，不是映射表的第二份副本。

同理，编码约定的单一权威是 `docs/spec/coding/`；`.trellis/spec/` 只保留指针。原因相同：不受版本控制、门禁读不到、克隆里不存在的副本一定会漂，而且漂了没人知道。

## 权威必须可被克隆与门禁读到

任何被当作权威的文档，必须在一个克隆里存在，并且门禁能读到它。`docs/spec/coding/` 满足这一点；`.trellis/` 不满足，因为它整个被 `.gitignore` 排除。

这不是理论风险，是已经发生过的漂移：放在 `.trellis/spec/backend/` 的那份编码规范把 `internal/api` 列进了确定性包，而 `api` import `net/http`。那份副本不受版本控制、门禁读不到、克隆里不存在，所以没有任何机制会报告它。指针指向克隆里没有的路径，本身就是断链。

`.trellis/spec/` 因此只保留指针桩：命名权威，不复述内容。

强制点：门禁步骤 `the coding conventions have one tracked authority`（检查体 `internal/gate/checks_conventions.go`）。它检查三件事：七个规范文件必须存在于 `docs/spec/coding/`；`docs/spec/index.md` 必须指向该目录；被忽略的树（`.trellis`/`.agents`/`.dsh`/`.zcode`）不得再出现规范文档的完整副本——指针桩允许，副本不允许，用文件大小区分。反例测试 `TestConventionsMustBeTracked` 与 `TestConventionsRejectSecondCopyInIgnoredTree` 各钉一个方向。

## 变更规程

1. 先改机器源。
2. 更新门禁检查的每一份副本。
3. 跑 `scripts/verify.ps1`——由门禁而不是记忆确认它们一致。
4. 如果某个事实有副本却没有门禁，要么加门禁，要么删副本。

## 反模式

- 在机器源之外第二次声明退出码、路径或列顺序。
- 只改 `docs/spec/contracts.md` 而留下过时的 `help.go`（或反之）——门禁会抓住，但那已经说明改动做错了。
- 没有门禁、也没有“机器源”指针的文档副本。
- 把编码规范写进不受版本控制、门禁读不到的目录。
