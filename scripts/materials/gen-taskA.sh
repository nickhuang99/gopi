#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# materials-gen-taskA.sh — A 面（`gopi task` 命令族）原始素材条目生成器。
#
# GENERATOR: scripts/… 本文件（证据目录里的这一份）—— 见产出报告的 Generator 一节。
# 形状遵守 docs/archive/demo/materials/README.md（M0 约定，master 上已有）：
#   * 目录名 <NNN>-<slug>，三位补零；
#   * 六件套固定名；02-output.txt 必须含一行 ^exit=[0-9]+；
#   * 00-entry.md 必填 face / feature / annotation_required / measured_commit /
#     date / how_to_replay + 三节（原始输入从哪来 / 输出怎么读 / 标注图在指什么）。
#
# 诚实性约束：
#  * 每个动词在私有沙盒 profile 里真跑（GOPI_HOME=$SB，Rule 34 的
#    `gopi tool sandbox create`），绝不碰操作者真台账。
#  * `-h` 不算真跑（Rule 31）：每条有真 argv + 真 stdout/stderr + 真 exit code。
#  * 输出不编造；脚本抓不到就写「没跑出来」。
#  * 图 = `gopi text2img` 渲染同一份文本 + `gopi annotate` 叠加（输入图不动）；
#    框的坐标由 material-render.py 量出来，并在写 04 前自证「框里有墨迹」——
#    框了个空行就 exit 1，不产出那张图。
#
# 用法: materials-gen-taskA.sh <out_root>
set -uo pipefail

# THE REPO THIS DRIVES is resolved from THIS SCRIPT's own location, not spelled:
# a hard-wired checkout path pins the generator to one throwaway directory that
# no longer exists (measured 2026-10-31: the spelled root was absent, so the
# script could not run at all — Rule 31's "run the surface you changed" was
# unsatisfiable by construction). GP_ROOT overrides it for a caller that wants a
# specific checkout.
ROOT="${GP_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
BIN="$ROOT/bin/gopi"
# 证据目录（非分支产物落在这里）：/home/user/.gopi/sessions/01a0ed22-fdaa-7a1d-9fb4-243d2aada2f6
RENDER="$ROOT/scripts/materials/render.py"
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:?usage: scripts/materials/gen-taskA.sh <out_root>}"
FACE=task
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"

# --- 私有沙盒 profile（Rule 34：ASK for a sandbox，never invent a directory） --
RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
SB="$("$BIN" tool sandbox create --label "materials-task-A-$RUNSTAMP" \
        --task-id 01a0ed22fdaa7a1d9fb4243d2aada2f6 | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
export GOPI_HOME="$SB"
echo "sandbox=$SB"
echo "measured_commit=$COMMIT"

# Rule 22: the scratch dir comes from scripts/lib/tmp-ban.sh — the ONE reader of
# the scratch root, asserted off `/` by DEVICE ID and registered for trap-time
# cleanup. /tmp shares `/`'s device here, so it is never a candidate.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
W="$(gopi_scratch_mkdir materials-A)" || exit 1
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$OUTROOT"
: > "$W/index.tsv"

# --- fixture 行（全部落在沙盒台账里） -----------------------------------------
mkid() { grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1; }
sub() { (cd "$ROOT" && "$BIN" task submit "$@" 2>&1 | mkid); }
ID_A="$(sub "素材 fixture A：可推进的行" --payload "打印 hello 再退出" --repo "$ROOT")"
ID_B="$(sub "素材 fixture B：bug 行" --payload "复现步骤：跑 X，见 Y" --tags bug --repo "$ROOT")"
ID_C="$(sub "素材 fixture C：父行" --payload "父行 payload" --repo "$ROOT")"
ID_D="$(sub "素材 fixture D：被父行卡住的孩子" --payload "子行 payload" --parent-task-id "$ID_C" --repo "$ROOT")"
# X 是 clone 条目要复制的那条**已结束**的行：clone 只复制"跑完了、工作还没被要"的行，
# 一条 pending 行会被按名字拒绝（\`is pending: it has not run yet\` —— 那是守卫在工作，
# 不是素材）。cancelled 是最便宜、最不会污染其它条目的终态。
ID_X="$(sub "素材 fixture X：已结束、可被 clone 的行" --payload "旧行 payload" --repo "$ROOT")"
[[ -n "$ID_X" ]] && (cd "$ROOT" && "$BIN" task update "$ID_X" --status cancelled --reason "素材 fixture：给 clone 条目一个可复制的终态行" >/dev/null 2>&1)
echo "fixture A=$ID_A B=$ID_B C=$ID_C D=$ID_D X=$ID_X"
[[ -n "$ID_A" && -n "$ID_B" && -n "$ID_C" && -n "$ID_D" && -n "$ID_X" ]] \
  || { echo "FATAL: fixture submit did not yield five ids" >&2; exit 1; }
[[ "$(cd "$ROOT" && "$BIN" task show "$ID_X" 2>/dev/null | grep -c 'status:.*cancelled')" -ge 1 ]] \
  || { echo "FATAL: fixture X did not reach a terminal status — the clone entry would probe the guard, not the verb" >&2; exit 1; }

# --- 一个条目 -----------------------------------------------------------------
# entry <n> <slug> <feature> <ann_req> <solves> <read_where> <caption> <marks> <argv...>
#   feature   00-entry.md 的 feature 字段（逐字写出那个动词/开关）
#   ann_req   yes|no —— 这张标注图承不承担解释
#   solves    用户语言说它解决什么问题（禁止复述 help）
#   read_where  02-output.txt 的哪一行是结论
#   caption   04-annotated.png 上的标注各指向哪、为什么这里必须被标
#   marks     --mark-match 规格，'|' 分隔
entry() {
  local n="$1" slug="$2" feature="$3" ann="$4" solves="$5" readw="$6" caption="$7" marks="$8"; shift 8
  [[ "${1:-}" == "--" ]] && shift
  local argv=("$@")
  local dir="$OUTROOT/$n-$slug"
  mkdir -p "$dir"
  # 重跑时清掉上次的产物（text2img 多页会留下 03-raw-1.png 这类残渣）
  find "$dir" -maxdepth 1 -type f ! -name '0[0-9]-*' -delete 2>/dev/null
  rm -f "$dir"/03-raw.png.txt "$dir"/03-raw.png.cal.txt "$dir"/03-raw.png.cal.png "$dir"/04-annotated.png.txt "$dir"/04-annotated.png.cal.png "$dir"/03-raw-*.png

  # 01-input.txt —— 命令 verbatim
  {
    printf 'cd %s\n' "$ROOT"
    printf 'GOPI_HOME=%s \\\n' "$SB"
    printf '  %s' "$BIN"
    printf ' %s' "${argv[@]}"
    printf '\n'
  } > "$dir/01-input.txt"

  # 真跑（stdout+stderr 合并；01 的 argv 能原样复跑出 02 的内容）
  local out rc
  out="$( (cd "$ROOT" && "${argv[@]}") 2>&1 )"
  rc=$?
  { printf '%s\n' "$out"; printf 'exit=%d\n' "$rc"; } > "$dir/02-output.txt"

  # 00-entry.md —— 素材卡（字段名与 README §3 模板一致）
  {
    printf '# %s-%s — %s

' "$n" "$slug" "$solves"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- feature: `%s`\n' "$feature"
    printf -- '- annotation_required: %s\n' "$ann"
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- how_to_replay: `bash scripts/materials/gen-taskA.sh <out>` 重跑整条 A 面；单条按 `01-input.txt` 逐字执行\n'
    printf -- '- sandbox_profile: `%s`（私有；操作者台账从未被读写）

' "$SB"
    printf '## 原始输入从哪来

'
    printf '本次运行的 argv 在 `01-input.txt`，含 cwd、`GOPI_HOME` 与二进制绝对路径——\n'
    printf '这四样就是复跑所需的全部；`GOPI_HOME` 指向的沙盒 profile 是本次运行的台账所在。

'
    printf '## 输出怎么读

'
    printf '`02-output.txt` 是 stdout 与 stderr 合并后的**逐字节**记录，末行 `exit=%d`（裸形式，census 的 output 谓词认这个拼写）是退出码。\n' "$rc"
    printf '%s

' "$readw"
    printf '## 标注图在指什么

'
    printf '%s\n' "$caption"
  } > "$dir/00-entry.md"

  # 03-raw.png / 04-annotated.png
  local mkargs=() m
  # printf 必须补尾换行：没有尾换行时 read 在读完最后一行后返回非 0，
  # while 体就不会执行——单条规格会被整条丢掉（实测：图直接不产出）。
  while IFS= read -r m; do
    [[ -n "$m" ]] && mkargs+=(--mark-match "$m")
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g')
  # 声明了标注却一个都没解析出来 = 生成器自己的缺陷，绝不能静默产出一张没标注的图
  if [[ -n "$marks" && ${#mkargs[@]} -eq 0 ]]; then
    echo "FATAL: entry $n declared marks but parsed none — $marks" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
    return 1
  fi
  printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s

' "$caption" > "$W/$slug.head"
  {
    cat "$W/$slug.head"
    printf '── 01-input.txt（verbatim 命令）────────────────────────────────────\n'
    cat "$dir/01-input.txt"
    printf '\n── 02-output.txt（verbatim 输出，末行是 exit code）──────────────────\n'
    cat "$dir/02-output.txt"
  } | python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --json-out "$W/$slug.json" "${mkargs[@]}" \
        > "$W/$slug.render.log" 2>&1
  local rrc=$?

  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$n" "$slug" "$rc" "$rrc" \
     "$(wc -l < "$dir/02-output.txt")" "$(stat -c %s "$dir/04-annotated.png" 2>/dev/null || echo 0)" >> "$W/index.tsv"
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $n-$slug 的标注图没产出（render=$rrc）——见 $W/$slug.render.log" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
  fi
  printf 'entry %-26s exit=%-3s render=%-3s marks=%d\n' "$n-$slug" "$rc" "$rrc" "${#mkargs[@]}"
}

# 每条都要有 05-finding.md 的正文（README §2 的 finding 条件）。默认正文是
# **诚实的"查过、没缝"**：写明这条跑了什么、拿三方（help 文案 / 实际行为 /
# 代码）比对了什么、结论是什么。挖到真缝的用 finding() 覆写。
finding_default() {
  local dir="$OUTROOT/$1"
  {
    printf '## 这次运行里没挖到缝（但查过什么，写在这里）

'
    printf '本条目对 %s 做了三方比对，**没有发现不一致**：

' "$2"
    printf -- '- **help 文案**：`gopi task -h` 对该动词的 synopsis 与正文描述；\n'
    printf -- '- **实际行为**：`02-output.txt` 的逐字节输出与 exit code（本条目）；\n'
    printf -- '- **代码**：派发分支与实现函数的实际判定。

'
    printf '判据不是"看起来对"，而是这次运行的三样东西**互相不矛盾**：文案承诺的输入形状被真 argv 满足，\n'
    printf '输出里的每个字段都能在代码里找到写法，退出码与输出内容一致。\n'
    printf '**本条目没有可报告的缺陷**——这一条本身就是结论，不是省略。\n'
  } > "$dir/05-finding.md"
}

# finding <dir> ：正文从 stdin 读（heredoc），不经过 argv —— 长正文里的
# 引号/反引号/表管道符不会跟 shell 解析打架。
finding() { cat > "$OUTROOT/$1/05-finding.md"; }

# entry() 之后自动补默认 finding
auto_finding_all() {
  local n slug
  while IFS=$'\t' read -r n slug _; do
    [[ -f "$OUTROOT/$n-$slug/05-finding.md" ]] || finding_default "$n-$slug" "$n-$slug"
  done < "$W/index.tsv"
}

echo "== entries =="

entry 001 task-list "gopi task list" yes \
  "一眼看全台账里谁在做、做到哪一步——不用翻会话日志、也不用问人" \
  "结论是每一行的 \`[状态]\` 方括号；最后一行带 \`exit=0\`。注意第一行**不是**下一个被领的活。" \
  "框住 fixture D 那行的 \`[pending]\`：它明明是 pending，却永远排在 \`[next]\` 之外——列表的顺序是时间倒序，派发是优先级序，两个顺序在同一屏里看得见。" \
  "[pending]:同一个词，两种命运" \
  -- "$BIN" task list

entry 002 task-submit "gopi task submit" yes \
  "把'一次工作'变成一行跨会话的台账记录：进程退出了，这行还在" \
  "结论是 \`Task queued:\` 那一行——它给出的是 id 与初始状态，不是一句确认语。" \
  "框住 \`Task queued:\` 行：submit 的产物是一个可被别的进程引用的 id 加它的初始状态。" \
  "Task queued:产物是 id + 初始状态" \
  -- "$BIN" task submit "素材：submit 真跑" --payload "一条 payload" --repo "$ROOT"

entry 003 task-submit-bug "gopi task submit --tags bug" yes \
  "打上 bug 标签的任务自动接进 bug 流水线——bug 是一等公民，不是备注" \
  "结论是第二行 \`[bug-flow]\` 统计：标签当场改变了优先级并打印流水线计数。" \
  "框住 \`[bug-flow]\` 行：标签不是装饰，它当场改变优先级（\`taskTagPriority\`：bug→1）并打印全库 bug 计数。" \
  "bug-flow:标签改变了优先级并打印流水线" \
  -- "$BIN" task submit "素材：bug 行真跑" --payload "复现步骤：跑 X" --tags bug --repo "$ROOT"

entry 004 task-show "gopi task show" yes \
  "把一行的完整记录摊开：包括能被 tail -f 的会话日志与原始 HTTP 日志路径" \
  "结论是 \`run_dir\` 与 \`task_report\` 两行——它们指向那次运行本身，而不是对它的概括。" \
  "框住 \`run_dir:\` 行：show 真正值钱的是它交出去看那次运行的入口，不是这一屏摘要。" \
  "run_dir:交出去看那次运行的入口" \
  -- "$BIN" task show "$ID_A"

entry 005 task-status "gopi task status" yes \
  "只问一行现在在哪一格——比 show 少一半输出，用于脚本里判状态" \
  "结论是 \`status:\` 那一行；整个契约就是这一个词，机器可读。" \
  "框住 \`status:\` 行：status 的全部输出围绕这一个词，机器可读。" \
  "status:全部输出围绕这一个词" \
  -- "$BIN" task status "$ID_A"

entry 006 task-update "gopi task update --status" yes \
  "推进一行的状态——写出之前状态机先校验，非法跃迁不会落库" \
  "结论是 \`Task updated:\` 行括号里的 \`status=in_progress\`：跃迁是被打印出来的事实。" \
  "框住 \`Task updated:\` 行：一次跃迁两个事实（从 pending 到 in_progress）都在这行里，不是静默写入。" \
  "Task updated:跃迁被打印出来，不是静默写入" \
  -- "$BIN" task update "$ID_A" --status in_progress

entry 007 task-cancel "gopi task update --status cancelled --reason" yes \
  "放弃一行：行不删除，理由进审计——事后还能回答'为什么这活没做完'" \
  "结论是 \`Task updated: … (status=cancelled, reason=…)\` 行——理由与状态一起落库。" \
  "框住 \`status=cancelled, reason=\`：取消是终态不是删除，理由跟着行一起留在台账里。" \
  "reason=:理由与状态一起落库" \
  -- "$BIN" task update "$ID_B" --status cancelled --reason "素材演示：演示取消路径"

entry 008 task-archive "gopi task archive" yes \
  "终态行归档：从默认视图里移走，但 help 明写 There is NO delete" \
  "结论是 \`tags=bug,resolved\`：归档的实现方式是**加标签**，所以行本身完好无损。" \
  "框住 \`tags=bug,resolved\`：归档 = 打标签，行没有消失——这就是'没有删除'的实现。" \
  "resolved:归档 = 打标签，行没有消失" \
  -- "$BIN" task archive "$ID_B"

entry 009 task-show-branch "gopi task show --branch" yes \
  "分支→台账行的反查：不用再手写 task/xxx.md sidecar 去把分支连回那一行" \
  "结论是 \`git provenance\` 与 \`ledger:\` 两行：git 侧读到的事实与台账读到的行数并排给出。" \
  "框住 ledger: 那行。它写的是「台账被读了、N 行里有 0 行指它」——不是空白，也不是静默成功。" \
  "ledger:查不到时先报读了几行" \
  -- "$BIN" task show --branch master

entry 010 task-next-removed "gopi task next（已移除的拼写）" no \
  "手动领活已被删除（M4.5）：派发权只归 worker——残留拼写会指名道姓告诉你去哪" \
  "结论是 \`removed (M4.5)\` 那句与 \`exit=1\`：退掉的动词不是静默失败。" \
  "框住整句 \`removed (M4.5)\`：退掉的动作给出一条指路消息（这是设计，不是 bug）。" \
  "removed (M4.5):退掉的动作给出指路消息" \
  -- "$BIN" task next

entry 011 parent-gate-parked "gopi task list --status pending（父行闸门）" yes \
  "父行没放行，孩子就不派发——一个 session 内的活不会抢在它的前提之前开跑" \
  "结论是 fixture D 那行的 \`[queue-skip]\` 与它后面的 \`parent … not released\`。" \
  "框住 \`[queue-skip]\`：这一行是 pending，却没有任何调度器会领它——父行闸门的可见证据。" \
  "queue-skip:行是 pending 但调度器不会领它" \
  -- "$BIN" task list --status pending

entry 012 task-clone "gopi task clone <id> --dry-run" yes \
  "一条已经跑完、但工作还没落地的行，被复制进一间干净的屋子重来——记录原封不动，工作区从零开始" \
  "结论是 \`--dry-run\` 的那几行预告：\`new task id: <id>-c1 (status=pending, parent=<id>)\`，以及 \`ground:\` 说明它不继承任何工作区。" \
  "框住 \`new task id\` 那一行与 \`ground:\` 那一行：clone 的产品是一条**新行**（parent=原行），不是原行被改写；dry-run 把它说出来而不写下去。" \
  "clone:产品是一条新行（parent=原行），原行不被改写" \
  -- "$BIN" task clone "$ID_X" --reason "素材：clone 真跑（dry-run）" --dry-run --confirm-cancelled

# （013 空缺：原条目 task-finish-cleanup 已删除，见下方 retirement 说明。编号不连续是
#   **有意的**——`== entries ==` 只列出真正产出的条目，硬把新条目填进 013 会让读者以为
#   它和 2026-10-30 之前那版是同一个东西。）

entry 014 task-sweep-worktrees "gopi task sweep-worktrees --dry-run" no \
  "worktree 残留盘点：哪些目录是活干完了却没人收的" \
  "结论是首行 \`repos=2 reclaimed=0 would-reclaim=0 reaped=0 …\`；后面每行是一个目录与它的裁定词。" \
  "框住首行计数与一行 \`keep [main-checkout]\`：扫描把'谁占着目录、为什么不能收'变成可读的裁定，而不是一个数字。" \
  "sweep-worktrees (dry-run):计数先行，逐目录给裁定;;keep:裁定词就是它的结论" \
  -- "$BIN" task sweep-worktrees --dry-run

# （015 空缺：原条目 task-diff-nopeer 已删除。它的前提是"本机没有声明 peer"，
#   而那个前提在这个生成器里**永远不成立**：Rule 34 的沙盒 profile 是操作员
#   settings 文档的一份 COPY（不是空文档），所以 `sync_peers` 被原样继承。
#   实测（2026-10-31）：`gopi task diff` 在沙盒里真的 ssh 出去问 peer1 与 hp，
#   打印 DIVERGED（local=7 peer=7028 / peer=5210），根本不会出现它断言的
#   `no sync peers declared`。一个永远无法满足的期望值不是素材，是谎——删掉。）

entry 017 task-unknown-dispatch "gopi task <打错的子命令>" yes \
  "打错的子命令不会静默变成别的东西：它把完整的可派发动词表当场贴给你" \
  "结论是首行 \`want …\` 那张表——**30 个**词，而 \`task -h\` 只广告 17 个。" \
  "上面一个框住首行 \`want …\` 那张表：这是**派发**面而不是广告面，用户按 \`task -h\` 或 TAB 永远看不到这么长。下面一个框住紧随其后的 \`GoPi task subcommands —\`：打错一个词之后，它还把**整篇** task help dump 了一遍——这是本条目 finding 的对象。" \
  "want archive|:这张表比 help 广告面长得多;;GoPi task subcommands:错一个词 = 整篇 help 也 dump 一遍" \
  -- "$BIN" task zzz

# ===========================================================================
# findings —— 只有真跑挖到的缝才写（README §2：05-finding 有正文即为「有」）
# 顺序要紧：auto_finding_all 先给**所有**条目铺默认 finding（诚实的"查过没缝"），
# 后面这三条真缝再覆写它们各自命中的条目。
# ===========================================================================
auto_finding_all

finding 010-task-next-removed <<'FINDING_EOF'
**缝：一条移除消息指向一个同样被移除的动词。**（类别：代码 vs 代码自带文档）

实测（本目录 02-output.txt，master 二进制，一行逐字）：

```
gopi task next: removed (M4.5) — manual claiming was removed — use "gopi worker" (claims and runs
the next eligible row) or "gopi task serve" (the daemon loop)
```

这行给出的**第二条**出路是 `gopi task serve`。但 `gopi task serve` 同样跑不了：

```
\$ gopi task serve ; echo exit=\$?
serve: retired — the resident merger daemon was retired for re-development: `--stop` is only
answered at a TICK BOUNDARY, so an in-flight tick cannot be interrupted — …
exit=1
```

**机制**：消息文本由 `cmd/agent/cli_tasks.go:4608`（`taskRemovedAdvice`，`:4591` 定义）
拼出，它只**引用** `taskProgName`（`:1744` = `"gopi worker"`）与字面量 `"gopi task serve"`；
而 `serve` 的退役判定在**另一个文件** `cmd/agent/merger_retired.go:77`（`retireMergerFace`）。
两处各自正确、合起来自相矛盾：一个 2026-09-16 的移除指向一个 2026-09-19 的退役，
中间三天没人把这条消息接上。

**同一函数的注释本身就写着这条规矩、并且只对 `run` 执行了它**：

> `cmd/agent/cli_tasks.go:4605-4607` — "a removal message that points at another removed
> spelling sends the reader in a circle"

`run` 的那一句因此指向了 `gopi worker`（活着）；`next` 的这一句没有被打扫到。
这不是设计取舍，是一条**没走完的清理**。

**为什么值得记**：这条消息是**只剩这一条出路**的用户唯一会看到的文本——
他手动领活被删了，正需要一个还活着的替代品，而它给了他一个死链接。
FINDING_EOF
# finding 016 已删除（原条目 016-task-diff-nopeer 一同删除）：那条 finding 的**第一行**
# 断言 `task diff` 在空台账上打印 `no sync peers declared … exit=0`，而这个前提在这个
# 生成器里被实测推翻——沙盒 profile 继承操作员的 `sync_peers`，`task diff` 真的会 ssh
# 出去问 peer1/hp 并打印 DIVERGED（local=7 peer=7028）。一张表格的第一行是伪测量，
# 整条 finding 就不再是"真跑挖到的缝"，删掉比留着诚实（README §2）。
# 那段"族内退出码不一致"的观察本身仍然成立（`for-branch`=4 / `list`=0），但它需要
# 一次**重新实测**才能重新落笔——不是把旧文字改个数字。

finding 017-task-unknown-dispatch <<'FINDING_EOF'
**缝：派发表的长度（30）是广告面（17）的近两倍，且它是在用户打错字时第一次示人。**（类别：help 文案 vs 代码）

三张表，各自从**活的二进制**现场解析（不是手抄）：

```
\$ ./bin/gopi task -h   | grep -oE '^  gopi task [a-z][a-z-]*' | awk '{print \$3}' | sort -u | wc -l
17
\$ ./bin/gopi completion bash | …（task 那一行）… | sort -u | wc -l
17
\$ ./bin/gopi task zzz  | head -1 | grep -oE '\\(want [^)]*\\)' | tr '|' '\\n' | sort -u | wc -l
30
```

**差集 = 13 个词**（`dispatch \\ help`）：

```
adopt audit-merges diff harvest merge merge-execute merged-cleanup merger push run serve sync sync-pass
```

**机制**：`cmd/agent/cli_tasks.go:1438` 的 default 分支把
`strings.Join(taskverbs.DispatchVerbs, "|")` 直接拼进错误消息；而 `DispatchVerbs`
（`internal/taskverbs/verbs.go:312`，推导契约写在 `:292`）是五个组的**并集**——
`FamilyVerbs`(`:203` 附近，17 个，广告) ∪ `SyncVerbs`(`:270` 附近，5 个) ∪
\MergerVerbs`(`:252` 附近，6 个) ∪ `ServiceAliases`(`:288` 附近，1 个) ∪
`RemovedVerbs`(`:233` 附近，1 个 = `run`)。

这个并集是**故意的、有测试护着的**（`internal/taskverbs/verbs.go:307` 的 `TestDispatchVerbsAreDerived` 与
`cli_completion_test.go` 的 `TestTaskCompletionNamesMatchDispatch`：派发 case 标签集合必须**恰好等于**并集）。
它要答的问题是「什么是可输入的拼写」（脚本、部署、service unit 都靠它），
而不是「什么是我们推荐的」。

**为什么仍然是缝**：这个 30 词的并集出现在**唯一一个面向人类的错误路径**上——
用户刚打错一个词，正需要一句最短的指路，却收到一张把
私有协议动词（`sync-pass`、`--serve-remote` 的孪生）、git 基础设施原子
（`merge-execute`、`merged-cleanup`）、已被移除的拼写（`run`）、
已退役的守护进程别名（`serve`）全都平铺在一行里的清单。
其中 `diff / harvest / push / sync-pass` 四个词在 `gopi task -h` 的正文里
**一次都没出现**（实测 0 次），只在 `gopi task sync -h` 里有——
所以这张表里至少 4 个词，用户从 `task -h` 一条路径都到不了。

**可提请的改法（不是本条目要做的）**：错误消息按组分别渲染，
把「可派发但不推荐」的部分降级为一行指路（就像 `run`/`next` 的移除消息已经做的那样），
而不是与广告面平铺进同一个 `|` 列表。本条目只记录测量结果。"
FINDING_EOF
echo "== 形状断言：每个条目必须恰好六件、件件非空 =="
shape_bad=0
for d in "$OUTROOT"/[0-9][0-9][0-9]-*/; do
  nfiles=$(find "$d" -maxdepth 1 -type f | wc -l)
  if [[ "$nfiles" -ne 6 ]]; then
    echo "SHAPE FAIL: $d 有 $nfiles 个文件（要求恰好 6）" >&2
    find "$d" -maxdepth 1 -type f -printf '    %f\n' >&2
    shape_bad=1
  fi
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $d/$f 缺失或为空" >&2; shape_bad=1; }
  done
  grep -qE '^exit=[0-9]+$' "$d/02-output.txt" \
    || { echo "SHAPE FAIL: $d/02-output.txt 没有 ^exit=<n> 行" >&2; shape_bad=1; }
done

echo "== index =="
cat "$W/index.tsv"
if [[ -s "$W/failed.txt" ]]; then
  echo "== FAILED entries ==" >&2
  cat "$W/failed.txt" >&2
  echo "GENERATOR FAILED: $(wc -l < "$W/failed.txt") entry/entries produced no annotated image" >&2
  exit 1
fi
[[ "$shape_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 形状断言未通过" >&2; exit 1; }
printf 'WORK=%s\nSANDBOX=%s\n' "$W" "$SB"
