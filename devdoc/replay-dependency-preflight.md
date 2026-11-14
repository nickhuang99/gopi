<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# devdoc: replay 管线的依赖前置体检 +「私有 display」的书面化

任务：`01a0fa48-30fb-7c3e-a1d2-e44600873a4c`
分支：`task/01a0fa48-30fb-7c3e-a1d2-e44600873a4c`
worktree：`/home/user/workspace/gopi-01a0fa4830fb7c3ea1d2e44600873a4c`

## 缺陷（怎么发现的、错在哪）

在**第二台机器**上跑 `scripts/replay/replay.sh`：那台机器没有装 Xvfb。管线把整条命令
跑到 `Xvfb` 探测那一段，撞上 **12 次 Xvfb 启动失败**，然后打印了一句**方向错误**的话：

```
no free X display in :62..:73 — a leaked server is holding them (check `pgrep -ax Xvfb`)
```

三处错，一处比一处贵：

1. **发现得太晚**：一个两秒钟就能查出来的事实（`command -v Xvfb`），要跑完整条命令
   （私有 sandbox、参数校验、目录准备…）才暴露，而且暴露在**最不友好**的位置。
2. **一次只报一个症状**：读者只看到"12 个显示号都起不来"，看不到"你缺的是 xvfb 这个包"。
3. **原因是被猜出来的，而且猜错了**：没有任何东西占着显示号——那台机器上根本没有 Xvfb。
   泄漏的服务**有它自己的证据**（`Server is already active for display N`），而 12 份
   日志**全是空的**；脚本却把一个空日志说成了一个具体的占用者，还把读者引向
   `pgrep -ax Xvfb`（那个列表只会显示"没有任何 X 进程"）。这就是 Rule 15 说的
   "没有观测到的原因，不写成结论"——而且它比沉默更坏：沉默会让人去看日志，
   错误的结论会让人去看错的地方。

4. 附带的一条：**「为什么必须是私有 display」只活在代码注释里**。注释不面向使用者；
   将来有人"顺手"把它改成操作者的 `DISPLAY`（少起一个进程、少一个依赖），没有任何
   一行文字拦得住他。

## 三个修法

### ① 依赖目录 + 体检：**从候选表导出**，不是第二份清单（Rule 14）

`tools/termemu/termemu.py` 新增 `build_deps()` / `DEPS` / `dep_rows()` /
`missing_hard_deps()` / `print_dep_report()` / `preflight_report()`，以及 CLI 动词
`preflight`：

```bash
python3 tools/termemu/termemu.py preflight         # 人读；缺硬依赖 → 非零退出
python3 tools/termemu/termemu.py preflight --json  # 同一份结论的机器可读形态
```

**关键设计：清单是算出来的，不是写下来的。** 只有「不属于候选表」的那几条写死在
`SHARED_DEPS`（python3 / Xvfb / xdotool / xwininfo / xprop——每一条都标了真实的
spawn 点）；终端、WM、dbus、dconf 那些行由 `EMULATORS` / `WM_CANDIDATES` 与它们的
`needs_bus` / `needs_wm` / `needs_config_copy` 标志**推导**出来。因此：

- 往候选表加一台终端 ⇒ 它的依赖行自动出现（测试
  `test_the_catalogue_is_derived_from_the_candidate_table` 钉住这一点）；
- 从候选表删掉 gnome-terminal ⇒ 它独有的 `dbus-daemon` / `dbus-run-session` / `dconf`
  行自动消失（`test_a_candidate_that_leaves_the_table_takes_its_rows_away`）；
- 没有「第二份会漂的清单」。

**分层是刻意的**（`ROLE_HARD` / `ROLE_TERMINAL` / `ROLE_CONDITIONAL`）：只有 `hard`
拦死运行。理由：`--emu auto` 存在的意义就是"这台机器上哪台终端能跑就用哪台"，它逐台
**真起一次**、只接受真起出窗口的那台；把"没装 gnome-terminal"说成"缺依赖"，会把一台
本来能用 xterm 跑完的机器白停一次，也把结论说过头了（Rule 15 的另一个方向）。
`dconf` 更是条件性的：缺席只是退到默认字号，整跑不会失败。

**模块自己也有一道闸**：`probe` / `launch` 两个入口调用 `require_hard_deps()`，缺硬依赖
就返回 1。脚本忘了体检时，模块不会装作没事——与其起 12 次必然失败的 Xvfb，不如在第一次
spawn 前说清楚。

### ② 起 Xvfb 之前先体检（replay.sh）

在 `SB=$(gopi sandbox create …)` **之前**插入：

```bash
python3 "$TERMEMU" preflight >&2 || die "dependency preflight failed (…)"
```

放在这里而不是更早：它需要 `$TERMEMU` 已经解析、`$SCRATCH` 已经建好；放在
`sandbox create` 之前，是因为"机器缺依赖"和"给这台机器建一个私有 profile"是两件事，
前者不合格就没必要做后者。**它绝不安装任何东西**（Rule 35）：只报告，然后脚本停下。

### ③ Xvfb 失败：先读回日志，再说话（Rule 15）

`xvfb_failure_report()` 逐块读回 `$SCRATCH/xvfb-<num>.log` 的**实际内容**（每块最多 6 行）：

- **日志非空** ⇒ 原样转述。一台真的被占用的显示号会在这里留下
  `Server is already active for display N`——那种情况下读者看到的**就是**占用证据。
- **日志为空** ⇒ 明说"它自己没留下任何诊断"，然后列出与空日志**相容的几种可能**
  （二进制不在 / 写不了 `/tmp/.X11-unix` / scratch 不可写），并指出**如果**是占用，
  这里本该出现哪一行。挑一个猜，正是这次要修掉的东西。

die 的文案也改了：不再是"有个泄漏的服务占着它们"，而是"日志在上面，本脚本不声明超出
它们的任何原因"。

## 文档化（Rule 7 / 可读性）

- `scripts/replay/replay.sh` 用法头新增两节：`DEPENDENCIES — WHAT THIS PIPELINE NEEDS
  BEFORE IT STARTS`（硬依赖 / 终端相关 / 条件性的分层 + 体检命令 + "绝不自动安装"）与
  `WHY THE DISPLAY IS PRIVATE`（三条理由）。`-h` 的打印范围从写死的行号改成
  `sed -n '2,/^set -uo pipefail/p'`——用法头将来还会长，写死的行号会先烂掉。
- `docs/archive/demo/replay/replay-film/README.md` 的「复现」一节后新增两节：「依赖：跑之前需要
  什么（体检在前，失败在两秒内）」与「为什么必须是私有 display（三条理由，不是偏好）」，
  含依赖表与 `preflight` 命令。

三条私有 display 的理由（写进两处的原文）：

1. **绝不碰操作者的桌面**——他们的 DISPLAY 上是活着的会话，在那里开窗拍屏会把他们的
   邮件聊天录进帧里；而且那里的"活动窗口"是他们最后点的那一个（实测：按窗口类搜，
   搜到的正是操作者跑了两周的终端）。
2. **没有窗口管理器，才有确定的像素来源**——裸 Xvfb 上没有别人映射/移动/重绘窗口，
   所以"第 N 拍的屏幕"只有一个作者：重放进程自己，帧与台账的一致性是**构造**出来的。
3. **屏幕尺寸必须钉死，装不下就拒绝、绝不裁剪**——`2600x1700` 是每帧被量算的尺子；
   在任意显示上几何就成了"那块显示恰好是什么"，而被裁剪的帧是一张屏幕上从未存在过的画面。

## 红证与绿证

见 `tasks/01a0fa48-report.md`（本 run 的报告）：红证 stderr 逐字、绿证 `replay.json`
对照 `docs/archive/demo/replay/arm-sess-4ca8cd29-20261002/replay.json`、最终依赖清单。

## 不做什么（诚实边界）

- 不自动安装任何东西，不加 `--skip-preflight` / `--allow-missing` 这类旁路开关
  （Rule 35：给规则加逃生门就是把规则反过来）。
- 体检**不**检查 PIL：`tools/human-ui/assemble.py` 用 PIL，但它是仓库内既有依赖、
  且缺了会在装配那一步以它自己的方式说话；本次不动它（把清单扩大 = 改别人的边界）。
- `docs/archive/demo/replay/shoot-frames.py`（另一条管线）没有加体检：它要求调用者**自己**先
  提供一块 display（`--display` 是必填参数），Xvfb 由调用者起。它的依赖表在
  `termemu.py preflight` 里已经覆盖（同一个模块），需要时用一条命令就能读到。

## 交付点对齐时学到的一条（2026-10-02，可复用）

合并 master 时 `devdoc/DOC-INDEX.tsv` 冲突（它是我这一支声明的生成物，`gopi merge`
有机械 union 通道 `cmd/agent/merge_union.go`）。手工合并踩到一个陷阱，值得写下来：

- **`git checkout --theirs` 不是 union**：master 侧的文件留下后，与 master 有 128 行
  class 冲突——我这侧那份索引是**旧生成器**的产物，给 128 份文档贴了 `normative`，
  而那些文档**自己第一行写着 `<!-- doc-class: historical -->`**（union 通道的
  `mergeUnionChooseClass` 正是按文档自己的 banner 裁决，`mergeUnionBannerVerdict`）。
  证据：`go test ./internal/standards/ -run TestDocClassBannerMatchesManifest`
  在 union 版本上 FAIL（`948 markdown docs of a bannered class carry their banner
  (115 mislabelled)`），在 master 版本上 ok；逐行核对 master 侧 128/128 与 banner 一致、
  我这侧 0/128。
- **正确做法**：取 master 侧文件，再用**增量写入器**只插自己那一行 ——
  `python3 tools/docclass/classify.py . --only devdoc/<new>.md`
  → `git diff --numstat master -- devdoc/DOC-INDEX.tsv` = **1 增 0 删**，
  而不是 510 增 / 509 删。
- 顺带一条机制：写路径会按内容重判**自己**的 doc（本次 devdoc 从 `historical` 变成
  `normative`，并因此被移除了 `<!-- doc-class: historical -->` banner——只有带 banner
  的类才带）。那是生成器按设计工作，不是手改；随合并提交一起落地。
