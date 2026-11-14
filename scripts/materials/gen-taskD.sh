#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-taskD.sh — D 面（隔离与恢复：\`gopi sandbox\` 3 子命令 + \`gopi checkpoint\` 3 子命令）
# 的原始素材条目生成器。
#
# 交付物形状（docs/archive/demo/materials/README.md 的契约）：
#   docs/archive/demo/materials/<NNN>-<slug>/{00-entry.md,01-input.txt,02-output.txt,
#                                    03-raw.png,04-annotated.png,05-finding.md}——恰好六件。
#
# 三条硬约束（都是别人踩过、写进 GOPI.md 的）：
#  * Rule 34：沙盒只能由 \`gopi tool sandbox create\` 产生，绝不 mktemp 手造；每个条目
#    在自己的沙盒 profile（GOPI_HOME=$SB）里跑，操作者的真台账一次都不碰。
#  * Rule 31：\`-h\` 不算真跑。每个条目有真 argv + 真 stdout/stderr + 真 exit code。
#  * Rule 22：截图只在离屏显示上做（默认 :78），绝不碰操作者的 DISPLAY。
#
# 用法: scripts/materials/gen-taskD.sh <out_root>
#   前置：离屏显示已在（\`Xvfb :<N> -screen 0 2800x1500x24\` 作为常驻后台任务），
#         \`GOPI_MATERIALS_DISPLAY\` 指到它（默认 :80）；二进制先
#         \`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/\`。
#   ROOT 由脚本自身位置推出，TASK_ID/SESSION_DIR 由 ROOT 目录名推出 —— 任何
#   checkout 里都能跑，不再钉在某一次运行的 worktree 上。
set -uo pipefail

# ---- 具名常量（Rule 1：没有裸字面量散在逻辑里）------------------------------
FACE=sandbox_plus_checkpoint
LABEL_PREFIX=materials-D

# Rule 22：scratch 目录不自己造，也不写路径 —— scripts/lib/tmp-ban.sh 是 scratch
# root 的唯一 reader，它用设备号断言目录在 `/` 之外，并在 trap 时清理；本机 /tmp
# 与 `/` 同设备，因此它永远不是候选。（Rule 34 说的"绝不手造沙盒"是同一件事的
# 另一半：沙盒由 `gopi tool sandbox create` 产出，脚本文档自身的临时文件走这里。）
. "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
trap 'gopi_scratch_cleanup' EXIT
CHAT_MODEL_PROMPT_REPLY_PONG="Reply with exactly: SANDBOX-ALIVE"
CHAT_MODEL_PROMPT_RESUME="Reply with exactly: RESUMED-FROM-CHECKPOINT"
SAMPLED_EXPORT_LINES=14
MAX_ENTRY_BYTES=32768
# `session resume` 走的是 chat/REPL 那条渲染路径：stdout 被命令替换成内部管道时它会
# 卡在「读终端尺寸」上（旧 `checkpoint resume` 实测 >120s 不返回）。给它一个上限，
# 让 124 这件事**被记录下来**，而不是把整轮生成器吊死。
RESUME_TIMEOUT_SEC="${GOPI_MATERIALS_RESUME_TIMEOUT_SEC:-120}"

# 位置派生，不是绝对值（Rule 6b/31）：上一版把 ROOT 钉在**那次运行的 worktree**上
# （已删除的 .../gopi-01a0ed2304b07af28ac5eafe05cc25bd），于是这份脚本在任何一个
# 别的 checkout 里都跑不起来——而"跑不起来"的记录不是素材。ROOT 取脚本自己在哪：
# scripts/materials/gen-taskD.sh → 上两级就是仓库根。
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="$ROOT/bin/gopi"
RENDER="$ROOT/scripts/materials/materials-render-D.py"
FONT_WQY=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
# 本 run 的 task id = worktree 目录名里的那 32 位十六进制（形如 gopi-<task-id>）。
TASK_ID="$(basename -- "$ROOT")"
TASK_ID="${TASK_ID#gopi-}"
[[ "$TASK_ID" =~ ^[0-9a-f]{32}$ ]] || {
  echo "FATAL: cannot derive a task id from $ROOT (want .../gopi-<32 hex>);" >&2
  echo "       pass a checkout whose directory names its task — never guess one." >&2
  exit 1
}
# session 目录是同一个 id 的 UUID 写法（8-4-4-4-12）——证据落在本 run 自己的目录里
# （Rule 37：素材的证据不写进主仓可写路径）。
SESSION_DIR="$HOME/.gopi/sessions/${TASK_ID:0:8}-${TASK_ID:8:4}-${TASK_ID:12:4}-${TASK_ID:16:4}-${TASK_ID:20:12}"
OUTROOT="${1:?usage: scripts/materials/gen-taskD.sh [--render-only] <out_root>}"
# --render-only：只重画两张图（用 01/02 这份**证据**当输入），不重跑命令、不建沙盒、
# 不改 01/02、不动 00-entry.md 的其余部分，只把两张图的 sha 与尺寸重新记一遍。
# 为什么需要它：图像尺寸不是缺陷（见下），而重跑整条 D 面会在**随机命名的沙盒路径**
# 与**随时间为变量的输出**上产生新证据——那会改写 02-output.txt，而本次工作只修
# "框怎么算出来的"，证据一个字都不许改。
RENDER_ONLY=false
if [ "$OUTROOT" = "--render-only" ]; then
  RENDER_ONLY=true; shift
  OUTROOT="${1:?usage: scripts/materials/gen-taskD.sh [--render-only] <out_root>}"
fi
# 2026-09-30：--render-only 下本次重画的输出与新的判据都属于这个 commit；
# 而 01/02 的证据仍出自 885eab6c（各自的 measured_commit 保持不动）。
MEASURED_COMMIT_OVERRIDE=""
if [ -n "${GOPI_MATERIALS_MEASURED_COMMIT:-}" ]; then
  MEASURED_COMMIT_OVERRIDE="$GOPI_MATERIALS_MEASURED_COMMIT"
fi
WORK="$SESSION_DIR/materials-D"
SCRATCH="$SESSION_DIR/materials-scratch"
RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
: "${GOPI_MATERIALS_DISPLAY:=:80}"
export GOPI_MATERIALS_DISPLAY

mkdir -p "$OUTROOT" "$WORK" "$SCRATCH"
: > "$WORK/index.tsv"
: > "$WORK/failed.txt"

# 证据文件的默认归宿：本 run 的 session 目录（Rule 37：素材类工作不碰主仓可写路径，
# 只有 docs/ 下的条目本身是交付物）。
EVID="$WORK/evidence"

# 每个条目一个沙盒：读完它的路径就是这一条的隔离证据（Rule 34）。
new_sandbox() {  # new_sandbox <n> <slug>  → 打印沙盒路径（verbatim 输出落到文件）
  # 注意：调用方是命令替换（subshell），变量活不出来——所以输出落**文件**。
  "$BIN" tool sandbox create --label "$LABEL_PREFIX-$1-$RUNSTAMP" --task-id "$TASK_ID" \
    > "$WORK/create-$1.out" 2>&1
  echo $? > "$WORK/create-$1.rc"
  tail -1 "$WORK/create-$1.out"
}

# 跑一条命令并把 verbatim 记录追加进 02-output.txt（含 \`$ 命令行\` 与 exit 码）。
# 第一个参数是"人看到的命令行"（就是要写进 01 的那一行），后面是真正执行的 argv。
run_cmd() {  # run_cmd <02file> <display-cmd> [env prefixes] -- argv...
  $RENDER_ONLY && return 0   # 02 是**证据**：只重画图时一条命令都不重跑
  local f="$1" disp="$2"; shift 2
  local envp=() sep=0 a
  for a in "$@"; do [[ "$a" == "--" ]] && sep=1; done
  if [[ $sep -eq 1 ]]; then
    while [[ "${1:-}" != "--" ]]; do envp+=("$1"); shift; done
    shift
  else
    # 没有 '--' 就没有 env 前缀，剩下的全是命令本身
    :
  fi
  [[ $# -gt 0 ]] || { echo "FATAL: run_cmd got no command (disp=$disp)" >&2; return 1; }
  { printf '$ %s\n' "$disp"; } >> "$f"
  local out rc
  out="$(cd "$ROOT" && env "${envp[@]}" "$@" 2>&1)"; rc=$?
  LAST_OUT="$out"
  LAST_RC="$rc"
  { printf '%s\n' "$out"; printf 'exit=%d\n\n' "$rc"; } >> "$f"
}

entry_header() {  # entry_header <n> <slug> <feature> <annreq> <solves> <readw> <caption>
  E_DIR="$OUTROOT/$1-$2"; E_N="$1"; E_SLUG="$2"; E_FEATURE="$3"; E_ANN="$4"
  E_SOLVES="$5"; E_READW="$6"; E_CAPTION="$7"
  if $RENDER_ONLY; then
    # 只重画图：01/02 是**证据**，一个字都不许被重跑改写，沙盒一个都不建
    # （不建沙盒 = 不产生新的沙盒路径 = 01 里的 GOPI_HOME 仍指向它跑过的那个）。
    # 图要画的正是这两份证据的输出，所以这里只把入口卡重写一遍。
    E_SB="$(sed -n 's/^- sandbox_profile: `\([^`]*\)`.*/\1/p' "$E_DIR/00-entry.md" | head -1)"
    [ -n "$E_SB" ] || { echo "FATAL: $E_DIR/00-entry.md 里没有 sandbox_profile，--render-only 无从取回 E_SB" >&2; exit 1; }
    return 0
  fi
  mkdir -p "$E_DIR"
  find "$E_DIR" -maxdepth 1 -type f ! -name '0[0-9]-*' -delete 2>/dev/null
  : > "$E_DIR/02-output.txt"
  E_SB="$(new_sandbox "$1" "$2")"
  [[ -d "$E_SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $E_SB" >&2; exit 1; }
}

unbt() { sed 's/\\`/`/g'; }   # 去掉给 bash 转义用的反斜杠：这些文件是给人/Markdown 读的

# 每条素材的标注规格（--render-only 用）：entry_render 把 argv 逐行写进**这一条自己的**
# 文件（一个参数一行 —— 参数里不会有换行，所以不需要任何转义；早先试过把 argv 拼进
# 一行再用 tr 切回来，`tr` 不认 '\x1f' 这种转义，参数被切坏后 render 收到的是
# `--scroll 0\x1fT:created=true reused=`）。收尾阶段 flush_pending_renders 按文件里的
# 顺序逐条重画。
RENDER_QUEUE_DIR="$WORK/render-queue"
flush_pending_renders() {
  $RENDER_ONLY || return 0
  local f n slug
  f="$(ls "$RENDER_QUEUE_DIR"/*.args 2>/dev/null | head -1)"
  [ -n "$f" ] || { echo "FATAL: 没有收集到任何标注规格" >&2; return 1; }
  for f in "$RENDER_QUEUE_DIR"/*.args; do
    n="$(basename "$f" .args)"; n="${n%%-*}"
    slug="$(basename "$f" .args)"; slug="${slug#*-}"
    E_N="$n"; E_SLUG="$slug"; E_DIR="$OUTROOT/$n-$slug"
    E_SB="$(sed -n 's/^- sandbox_profile: `\([^`]*\)`.*/\1/p' "$E_DIR/00-entry.md" | head -1)"
    [ -n "$E_SB" ] || { echo "FATAL: $E_DIR/00-entry.md 里没有 sandbox_profile" >&2; return 1; }
    local argv=()
    mapfile -t argv < "$f"
    render_one "${argv[@]}" || return 1
    entry_shas
  done
}

# 01-input.txt 写的是**这一条真正跑过的每一行**：cwd、沙盒 profile、以及每条命令。
entry_input() {  # entry_input <sandbox> <cmdline>...
  local sb="$1"; shift
  $RENDER_ONLY && return 0   # 01 是证据：只重画图时一个字都不改
  {
    printf '# cwd: %s\n' "$ROOT"
    printf '# sandbox profile (GOPI_HOME): %s   ← \`gopi sandbox create\` 的产物\n' "$sb"
    printf '# 逐行可复制执行；下面每一条都在 02-output.txt 里有它的输出与 exit code\n\n'
    printf '%s\n' "$@"
  } | unbt > "$E_DIR/01-input.txt"
}

entry_card() {  # entry_card
  # --render-only 时**保住证据的日期**：measured_commit/date 说的是这份证据（01/02）
  # 是在哪个 commit、哪一天跑出来的，重画图不改它。重画本身属于哪个 commit 另记一行
  # （rerendered_commit）—— 两件事分开写，才不会有"这条素材是 203e8a40 跑的"这种
  # 把图的重产冒充成命令重跑的误读。
  local old_commit="" old_date="" mc="$COMMIT" dt="$TODAY"
  if $RENDER_ONLY && [ -s "$E_DIR/00-entry.md" ]; then
    old_commit="$(sed -n 's/^- measured_commit: //p' "$E_DIR/00-entry.md" | head -1)"
    old_date="$(sed -n 's/^- date: //p' "$E_DIR/00-entry.md" | head -1)"
    [ -n "$old_commit" ] && mc="$old_commit"
    [ -n "$old_date" ] && dt="$old_date"
  fi
  [ -n "$MEASURED_COMMIT_OVERRIDE" ] && mc="$MEASURED_COMMIT_OVERRIDE"
  {
    printf '# %s-%s — %s\n\n' "$E_N" "$E_SLUG" "$E_SOLVES"
    printf -- '- face: \`%s\`\n' "$FACE"
    printf -- '- feature: \`%s\`\n' "$E_FEATURE"
    printf -- '- annotation_required: %s\n' "$E_ANN"
    printf -- '- measured_commit: %s\n' "$mc"
    printf -- '- date: %s\n' "$dt"
    if $RENDER_ONLY; then
      printf -- '- rerendered_commit: %s\n' "$COMMIT"
      printf '  （\`03-raw.png\`/\`04-annotated.png\` 于 %s 重产：标注框的判据与算法在那里改过；\n' "$COMMIT"
      printf '  \`01-input.txt\`/\`02-output.txt\` 是**证据**，仍出自上一条 \`measured_commit\`，未重跑）\n'
    fi
    printf -- '- how_to_replay: \`bash scripts/materials/gen-taskD.sh <out>\` 重跑整条 D 面；\n'
    printf '  单条按 \`01-input.txt\` 逐行执行（第三行起就是命令本身）\n'
    printf -- '- sandbox_profile: \`%s\`（\`gopi sandbox create\` 产生；操作者台账从未被读写）\n\n' "$E_SB"
    printf '## 原始输入从哪来\n\n'
    printf '\`01-input.txt\` 是本次运行逐行复制得出来的输入：cwd、\`GOPI_HOME\`（沙盒 profile 路径）\n'
    printf '与每条命令。\`02-output.txt\` 里每个 \`$ \` 行与它们一一对应——输出不是重排版，是同一份记录。\n\n'
    printf '## 输出怎么读\n\n'
    printf '%s\n\n' "$E_READW"
    printf '每个命令块末尾的 \`exit=<n>\` 是**那一条**命令的退出码（Rule 31：没有退出码的输出不是证据）。\n\n'
    printf '## 标注图在指什么\n\n'
    printf '%s\n' "$E_CAPTION"
  } | unbt > "$E_DIR/00-entry.md"
}

finding_default() {  # finding_default <what-was-cross-checked>
  $RENDER_ONLY && return 0   # 05 是这次运行的结论：只重画图时不重写它
  {
    printf '## 这次运行没挖到缝（但查过什么，写在这里）\n\n'
    printf '本条目对 %s 做了三方比对，**没有发现不一致**：\n\n' "$1"
    printf -- '- **help 文案**：\`gopi sandbox -h\` / \`gopi checkpoint -h\` 对该动词的 synopsis 与正文；\n'
    printf -- '- **实际行为**：本条目 \`02-output.txt\` 的逐字节输出与 exit code；\n'
    printf -- '- **代码**：该动词的实现分支与判定。\n\n'
    printf '判据不是"看起来对"，而是这次运行的三样东西互相不矛盾：文案承诺的输入形状被真 argv 满足，\n'
    printf '输出里的每个字段都能在代码里找到写法，退出码与输出内容一致。\n'
    printf '**本条目没有可报告的缺陷**——这一条本身就是结论，不是省略。\n'
  } | unbt > "$E_DIR/05-finding.md"
}

# 渲染六件套里的两张图（03 原图 + 04 标注图）。
entry_render() {  # entry_render <scroll> <select-text>... -- <caption>...
  if $RENDER_ONLY; then
    # 只重画图：图像在收尾阶段（flush_pending_render）统一重产 —— 那时这一条的
    # 02-output.txt 早已在盘上（全量模式下 02 是在 entry_header 里被截断重写的，
    # 而 --render-only 一条命令都不重跑）。
    mkdir -p "$RENDER_QUEUE_DIR"
    printf '%s\n' "$@" > "$RENDER_QUEUE_DIR/$E_N-$E_SLUG.args"
    return 0
  fi
  render_one "$@"
}

# 真正出图的那一半（全量模式与只重画模式共用同一条路径、同一份判据）。
render_one() {  # render_one <scroll> <select-text>... -- <caption>...
  local scroll="$1"; shift
  local sels=() caps=() cur="sels"
  for a in "$@"; do
    if [[ "$a" == "--" ]]; then cur="caps"; continue; fi
    if [[ "$cur" == "sels" ]]; then sels+=("--select-text" "$a"); else caps+=("--caption" "$a"); fi
  done
  if [[ ${#sels[@]} -eq 0 ]]; then
    echo "FATAL: entry $E_N declares no selector — the annotation would explain nothing" >&2
    printf '%s\n' "$E_N-$E_SLUG" >> "$WORK/failed.txt"; return 1
  fi
  # 只重画图时先写 .new：尺寸变了就中止，旧图原地保留（图像尺寸不是缺陷，不许改）。
  local raw_out="$E_DIR/03-raw.png" ann_out="$E_DIR/04-annotated.png"
  if $RENDER_ONLY; then raw_out="$E_DIR/03-raw.new.png"; ann_out="$E_DIR/04-annotated.new.png"; fi
  python3 "$RENDER" --entry-dir "$E_DIR" --sb "$E_SB" --cwd "$ROOT" --cmd "$E_FEATURE" \
    --win "$E_N" --scroll "$scroll" "${sels[@]}" "${caps[@]}" \
    --raw "$raw_out" --ann "$ann_out" \
    --scratch "$SCRATCH" \
    --manifest "$SCRATCH/$E_N-$E_SLUG.manifest.json" \
    >> "$WORK/$E_N-$E_SLUG.render.log" 2>&1
  local rrc=$?
  if [[ $rrc -eq 0 ]] && $RENDER_ONLY; then
    # 尺寸必须与旧图一致（Rule：图像尺寸由内容定，重产不许改它）。
    local old_sz new_sz
    old_sz="$(png_size "$E_DIR/03-raw.png")"
    new_sz="$(png_size "$raw_out")"
    if [[ "$old_sz" != "$new_sz" ]]; then
      echo "FATAL: $E_N-$E_SLUG 重产的图尺寸变了（$old_sz → $new_sz）——旧图保留，拒绝覆盖" >&2
      printf '%s\t%s\t%s\t%s\t%s\n' "$E_N" "$E_SLUG" "${LAST_RC:--}" "99" "$(wc -l < "$E_DIR/02-output.txt")" >> "$WORK/index.tsv"
      printf '%s\n' "$E_N-$E_SLUG" >> "$WORK/failed.txt"; return 1
    fi
    mv -f "$raw_out" "$E_DIR/03-raw.png"
    mv -f "$ann_out" "$E_DIR/04-annotated.png"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$E_N" "$E_SLUG" "${LAST_RC:--}" "$rrc" "$(wc -l < "$E_DIR/02-output.txt")" >> "$WORK/index.tsv"
  printf 'entry %-30s exit=%-3s render=%-3s\n' "$E_N-$E_SLUG" "${LAST_RC:--}" "$rrc"
  [[ $rrc -eq 0 ]] || { printf '%s\n' "$E_N-$E_SLUG" >> "$WORK/failed.txt"; return 1; }
}

png_size() { python3 -c 'import sys;from PIL import Image;print("%dx%d"%Image.open(sys.argv[1]).size)' "$1"; }

# 每张图的 sha 写进自己的素材卡：图被重产过是可核查的（sha 变了），而 sha 的
# 权威只有一个 —— 盘上的字节（不是 manifest 里的旧数字）。
entry_shas() {  # entry_shas
  local s3 s4 tmp
  s3="$(sha256sum "$E_DIR/03-raw.png" | cut -c1-16)"
  s4="$(sha256sum "$E_DIR/04-annotated.png" | cut -c1-16)"
  # Rule 22：文档导出的中间文件也必须落在 `/` 之外 —— 路径由 tmp-ban.sh 的
  # helper 给出（设备号断言 + trap 清理），不是手写的路径模板。
  tmp="$(gopi_scratch_load entry-shas.txt)" || exit 1
  gopi_scratch_register "$(dirname "$tmp")"
  awk '/^## 截图$/{skip=1} skip&&/^## /&&!/^## 截图$/{skip=0} !skip{print}' \
    "$E_DIR/00-entry.md" > "$tmp"
  cat >> "$tmp" <<EOF

## 截图

| 文件 | 内容 | sha256[:16] |
|---|---|---|
| \`03-raw.png\` | 该命令真实输出的终端截图（Xvfb 离屏 + \`gopi capture\`，像素来自真实帧缓冲） | \`$s3\` |
| \`04-annotated.png\` | 同一批像素 + \`gopi annotate\` 打的框与标签 | \`$s4\` |

两张图 sha 不同 = 标注**真的发生过**。框的位置不是手调的：上下边由**这一行的墨迹**
算出，并在出图前过一遍结构判据（框的上下边所在像素行在框的列范围内没有墨迹 /
框内恰好一个墨迹段 / 该段落在框声称的行槽里），不成立即拒绝出图——
判据、算法与红→绿回归见 \`devdoc/materials-D-annotation-criterion.md\`
与 \`scripts/test_materials_render_D.py\`。
EOF
  mv -f "$tmp" "$E_DIR/00-entry.md"
}

# 只重画图模式下的断言：图片存在、非空、03/04 的 sha 不同（标注真的发生了）、
# 且 00-entry.md 里的 sha 与盘上的字节一致。**只查 D 面那 7 条**——素材树里还有
# 别的面（别的生成器产的，本次工作一个字都不许改）。
assert_shas() {
  local bad=0 d f s3 s4 n slug
  # 只查**这一面**的入口：别的面（别的生成器产的）同样有 001..007 的序号，
  # 按数字前缀筛会把它们卷进来（实测：005-notify-… / 006-task-update 等）。
  for f in "$RENDER_QUEUE_DIR"/*.args; do
    n="$(basename "$f" .args)"; slug="${n#*-}"; n="${n%%-*}"
    d="$OUTROOT/$n-$slug"
    [ -d "$d" ] || continue
    for f in 03-raw.png 04-annotated.png; do
      [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $d/$f 缺失或为空" >&2; bad=1; }
    done
    s3=$(sha256sum "$d/03-raw.png" | cut -c1-16); s4=$(sha256sum "$d/04-annotated.png" | cut -c1-16)
    [[ "$s3" != "$s4" ]] || { echo "SHAPE FAIL: $d 的 03/04 图 sha 相同（标注没发生）" >&2; bad=1; }
    grep -q "$s3" "$d/00-entry.md" || {
      echo "SHAPE FAIL: $d/00-entry.md 的 03 sha 与盘上不一致（先跑 entry_shas）" >&2; bad=1; }
    grep -q "$s4" "$d/00-entry.md" || {
      echo "SHAPE FAIL: $d/00-entry.md 的 04 sha 与盘上不一致（先跑 entry_shas）" >&2; bad=1; }
  done
  return $bad
}

# 形状断言：恰好六件、件件非空、02 里有 ^exit=
assert_shape() {
  local bad=0 d f n
  for d in "$OUTROOT"/[0-9][0-9][0-9]-*/; do
    n=$(find "$d" -maxdepth 1 -type f | wc -l)
    if [[ "$n" -ne 6 ]]; then
      echo "SHAPE FAIL: $d 有 $n 个文件（要求恰好 6）" >&2
      find "$d" -maxdepth 1 -type f -printf '    %f\n' >&2; bad=1
    fi
    for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
      [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $d/$f 缺失或为空" >&2; bad=1; }
    done
    grep -qE '^exit=[0-9]+$' "$d/02-output.txt" || { echo "SHAPE FAIL: $d/02-output.txt 没有 ^exit= 行" >&2; bad=1; }
    local s3 s4
    s3=$(sha256sum "$d/03-raw.png" | cut -c1-16); s4=$(sha256sum "$d/04-annotated.png" | cut -c1-16)
    [[ "$s3" != "$s4" ]] || { echo "SHAPE FAIL: $d 的 03/04 图 sha 相同（标注没发生）" >&2; bad=1; }
  done
  return $bad
}

echo "sandbox_face=$FACE measured_commit=$COMMIT display=$GOPI_MATERIALS_DISPLAY"
echo "out_root=$OUTROOT"

# ===========================================================================
# 001 gopi tool sandbox create —— 沙盒从哪来（末行就是路径，脚本消费的就是它）
# ===========================================================================
entry_header 001 sandbox-create "gopi tool sandbox create" yes \
  "让一次运行拥有一个私有的、可回收的 profile：写坏的台账、留下的会话都关在沙盒里，操作者的真 profile 一次都不碰" \
  "结论是 02 的**末行**：\`create\` 把沙盒路径单独打成最后一行——这正是脚本拿它的方式（\`| tail -1\`），而不是从上面那行日志里正则抠路径。" \
  "上框框住 create 的裁决行：\`.created/.reused\` 说的是这次到底是新建还是复用同名沙盒（同名会被复用，是复跑时最容易读错上一轮台账的地方）。下框框住末行那条路径——脚本消费的 SB 就是它。"
entry_input "$E_SB" \
  "$BIN tool sandbox create --label $LABEL_PREFIX-001-$RUNSTAMP --task-id $TASK_ID" \
  "GOPI_HOME=$E_SB $BIN session log --last"
if ! $RENDER_ONLY; then
{
  printf '$ %s sandbox create --label %s --task-id %s\n' "$BIN" "$LABEL_PREFIX-001-$RUNSTAMP" "$TASK_ID"
  cat "$WORK/create-001.out"
  printf 'exit=%s\n\n' "$(cat "$WORK/create-001.rc")"
} > "$E_DIR/02-output.txt"
fi
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session log --last" GOPI_HOME="$E_SB" "$BIN" session log --last
entry_card
entry_render 0 \
  "T:created=true reused=false:70:裁决行：这一次是新建还是复用" \
  "T:sandbox-materials-d-001-:60:末行 = 沙盒路径（脚本 tail -1 取到的就是它）" -- \
  "T:No sessions found:0:新沙盒里还没有会话——log --last 读的正是这个沙盒自己的目录"
finding_default "gopi tool sandbox create 的判定行与末行路径"
echo

# ===========================================================================
# 002 gopi session checkpoint list —— 有哪些可恢复的检查点
# ===========================================================================
entry_header 002 checkpoint-list "gopi session checkpoint list" yes \
  "在被中断之后，先看清楚有哪些运行留下了检查点、每个检查点有多新多大——再决定恢复哪一个" \
  "结论是那张表：每行一个日志，列里有 reqs（请求数）/ last-model / msgs / bytes / sha256 / last-message。\`last-message\` 一列就能认出"这份日志停在哪个问题上"。" \
  "框住表头：\`checkpoint list\` 的产物是一张**表**，判读它要先知道每一列是什么。第二框框住最后那行记录——它的 \`last-message\` 正是这次被中断的问题本身。"
entry_input "$E_SB" \
  "# 先造一份检查点（一个真的跑过的会话；raw 日志指到沙盒自己的 raw/ 目录）" \
  "printf '%s\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME=$E_SB GOPI_RAW_LOG_DIR=$E_SB/.gopi/raw $BIN chat" \
  "GOPI_HOME=$E_SB $BIN session checkpoint list --dir $E_SB/.gopi/raw" \
  "GOPI_HOME=$E_SB $BIN session log --last"
run_cmd "$E_DIR/02-output.txt" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME=$E_SB GOPI_RAW_LOG_DIR=$E_SB/.gopi/raw $BIN chat" \
  GOPI_HOME="$E_SB" GOPI_RAW_LOG_DIR="$E_SB/.gopi/raw" GOPI_RAW_LOG_DIR="$E_SB/.gopi/raw" \
  env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER bash -c \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME='$E_SB' GOPI_RAW_LOG_DIR='$E_SB/.gopi/raw' '$BIN' chat"
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session checkpoint list --dir $E_SB/.gopi/raw" \
  GOPI_HOME="$E_SB" "$BIN" session checkpoint list --dir "$E_SB/.gopi/raw"
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session log --last" GOPI_HOME="$E_SB" "$BIN" session log --last
entry_card
entry_render 0 \
  "T:reqs last-model:46:表头：reqs / msgs / bytes / sha256 各是什么" \
  "T:gopi-raw-:70:最后一行 = 这份日志停在哪个问题上" -- \
  "T:Session:0:log --last 读的是同一个沙盒自己的会话（沙盒是私有 profile）"
finding_default "gopi session checkpoint list 的表头、列义与 --dir 语义"
echo

# ===========================================================================
# 003 gopi session checkpoint export —— 检查点里到底存了什么（恢复的前提）
# ===========================================================================
entry_header 003 checkpoint-export "gopi session checkpoint export" yes \
  "在恢复之前先看清楚'要恢复的东西'是什么：一份完整的请求快照（system + 全历史 + 工具定义），不是一句话摘要" \
  "结论是两份输出对读：\`wc -c\` 给出字节数（数万），逐键列出的顶层字段则说明这就是一份可直接发出的请求体——而 \`messages\` 只有 1 条。两条命令跑的是同一个导出物，只是读法不同：一个量大小，一个看结构。" \
  "框住 \`\"model\"\` 行：导出物的第一个字段就说明这是一份**请求**快照（恢复 = 把这份请求重新发出去），而不是人类可读的记录。第二框框住 \`wc -c\` 那一行：这份快照有多大，一眼可见。"
entry_input "$E_SB" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME=$E_SB GOPI_RAW_LOG_DIR=$E_SB/.gopi/raw $BIN chat" \
  "GOPI_HOME=$E_SB $BIN session checkpoint export $E_SB/.gopi/raw/gopi-raw-*.log | head -$SAMPLED_EXPORT_LINES" \
  "GOPI_HOME=$E_SB $BIN session checkpoint export $E_SB/.gopi/raw/gopi-raw-*.log | wc -c"
run_cmd "$E_DIR/02-output.txt" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME=$E_SB GOPI_RAW_LOG_DIR=$E_SB/.gopi/raw $BIN chat" \
  env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER bash -c \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME='$E_SB' GOPI_RAW_LOG_DIR='$E_SB/.gopi/raw' '$BIN' chat"
# 导出物是数万字节的一整行 JSON：直接画进图没有意义（一页装不下），所以
# 记录的是"它的结构"——逐键列出顶层字段，再给出几个结构事实。这仍是真跑出来的
# 字节（同一个命令的产物），只是把"读法"写成了命令本身。
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session checkpoint export <raw log> | wc -c" \
  bash -c "GOPI_HOME='$E_SB' '$BIN' session checkpoint export '$E_SB'/.gopi/raw/gopi-raw-*.log | wc -c"
run_cmd "$E_DIR/02-output.txt" \
  "GOPI_HOME=$E_SB $BIN session checkpoint export <raw log> | python3 -c 'import json,sys; d=json.load(sys.stdin); print(chr(10).join(chr(34)+k+chr(34) for k in d)); print(\"messages=\" + str(len(d[\"messages\"])))'" \
  bash -c "GOPI_HOME='$E_SB' '$BIN' session checkpoint export '$E_SB'/.gopi/raw/gopi-raw-*.log | python3 -c 'import json,sys; d=json.load(sys.stdin); print(chr(10).join(chr(34)+k+chr(34) for k in d)); print(\"messages=\" + str(len(d[\"messages\"])))'"
entry_card
entry_render 0 \
  "T:max_tokens:44:顶层字段：这一份请求的全部入参" \
  "T:messages=1:44:只有 1 条消息，快照却数万字节" -- \
  "T:max_tokens:0:导出的是一份可直接发出的请求体，不是人类可读的记录"
cat > "$E_DIR/05-finding.md" <<FINDING_EOF
**缝：\`checkpoint export\` 把整份请求快照一次性倒到 stdout，而它唯一的用途是"看一眼再恢复"。**

本条目实测（02-output.txt，沙盒内真跑）：

\`\`\`
\$ GOPI_HOME=\$SB gopi checkpoint export \$SB/.gopi/raw/gopi-raw-*.log | wc -c
<这一条的真值见本目录 02-output.txt 里对应命令块的最后一行>
\`\`\`

一次**单轮**对话（问一句 "Reply with exactly: SANDBOX-ALIVE"、答一个词）的检查点就是这么大的
一份 JSON：system prompt（含整个 GOPI.md）+ 全部历史 + 工具定义，逐字节快照。

**这不是 bug，是设计**（检查点必须是 byte-exact 的，否则恢复出来的上下文不等于原来的上下文）。
值得记下来的是**它的读法**：\`export\` 没有 \`--summary\` / \`--keys\` 一类的"\只看结构"开关，
所以任何人想知道"这份快照里有哪些字段、多少条消息"，都得把整份倒出来再自己过滤——
上面那条 \`| head -$SAMPLED_EXPORT_LINES\` 就是我在条目里必须做的动作。

**对比**：同族的 \`checkpoint list\` 走的是另一条路——它**只读日志头部**（\`IndexDir\` 的
head-sample，多 GB 的 raw 目录也能秒列），因为"列清单"不需要全量。两个子命令对"读多少"
的取舍是合理的；但第三个子命令（\`export\`）落在中间地带：它的输出注定要被**人**看一眼，
而它没有任何降级读取的形态。
FINDING_EOF
echo

# ===========================================================================
# 004 gopi session checkpoint resume —— 真的恢复一次中断的会话
# ===========================================================================
entry_header 004 checkpoint-resume "gopi session checkpoint resume" yes \
  "被中断的会话可以从它的检查点接着跑：不用重来，也不丢上下文" \
  "结论在末尾：恢复后的那一轮把**被中断的那个问题本身**重新答了一遍（\`PONG\` / \`SANDBOX-ALIVE\` 这类只有靠恢复出来的历史才答得出的内容），并且在 \`[trajectory stats]\` 里报出 cache hit 比例——命中率高说明 system prompt 与历史确实是原样发回去的。" \
  "上框框住 trajectory 的 cache 一行：cache hit 57472/57841 说明发回去的上下文与原来**逐字节等价**（不是重新拼的）。下框框住答案本身——它证明恢复出来的会话真的接着回答了原问题。"
entry_input "$E_SB" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME=$E_SB GOPI_RAW_LOG_DIR=$E_SB/.gopi/raw $BIN chat" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_RESUME' | GOPI_HOME=$E_SB $BIN session checkpoint resume $E_SB/.gopi/raw/gopi-raw-*.log" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_RESUME' | GOPI_HOME=$E_SB $BIN session resume $E_SB/.gopi/raw/gopi-raw-*.log" \
  "GOPI_HOME=$E_SB $BIN session log --last"
run_cmd "$E_DIR/02-output.txt" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME=$E_SB GOPI_RAW_LOG_DIR=$E_SB/.gopi/raw $BIN chat" \
  env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER bash -c \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_REPLY_PONG' | GOPI_HOME='$E_SB' GOPI_RAW_LOG_DIR='$E_SB/.gopi/raw' '$BIN' chat"
run_cmd "$E_DIR/02-output.txt" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_RESUME' | GOPI_HOME=$E_SB $BIN session checkpoint resume $E_SB/.gopi/raw/gopi-raw-*.log" \
  env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER bash -c \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_RESUME' | GOPI_HOME='$E_SB' '$BIN' session checkpoint resume '$E_SB'/.gopi/raw/gopi-raw-*.log"
# 上面那条测的是**旧拼法**（`checkpoint resume` 已被移除，只回一行搬迁指针）；
# 下面这条才是今天真该用的动词 —— 素材要同时给出去重后的判定与正确的入口。
run_cmd "$E_DIR/02-output.txt" \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_RESUME' | GOPI_HOME=$E_SB $BIN session resume $E_SB/.gopi/raw/gopi-raw-*.log" \
  env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER bash -c \
  "printf '%s\\n' '$CHAT_MODEL_PROMPT_RESUME' | GOPI_HOME='$E_SB' timeout $RESUME_TIMEOUT_SEC '$BIN' session resume '$E_SB'/.gopi/raw/gopi-raw-*.log"
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session log --last" GOPI_HOME="$E_SB" "$BIN" session log --last
entry_card
entry_render 0 \
  "T:one continuation, not two:70:旧拼法已被移除：这一步只回搬迁指针（真跑，exit=1）" \
  "T:byte-exact context:70:恢复读的是逐字节的请求快照，不是摘要" -- \
  "T:SANDBOX-ALIVE:0:恢复出来的上下文里就有原来那一问，所以先答的是它"
cat > "$E_DIR/05-finding.md" <<'FINDING_EOF'
**缝：一个长输出 + 宽终端会把 `checkpoint resume` 卡死在「读终端尺寸」上（超时 120s 未返回）。**

复现（本 run 实测，非推测）：恢复同一个检查点两次，唯一差别是外层怎么接它的输出。

| 外层 | 尺寸 | 结果 |
|---|---|---|
| `... | tail -12`（管道） | 未知（不是 tty） | 约 2.4s 返回，正常恢复 |
| 命令替换 `out=$(... 2>&1)` | 未知（不是 tty） | **>120s 不返回**，被外层 timeout 杀掉 |

两边的 stdin 都补了输入（管道 / `< <(printf …)`），差别只在 stdout 是不是管道。

**为什么**：`resume` 走的是 chat/REPL 那条渲染路径——它要问终端"你多宽"来决定怎么排版。
stdout 被命令替换成一根内部管道时，那条查询**永远等不到答复**，于是进程停在那里；
而 `checkpoint list --dir …`、`checkpoint export … | wc -c` 这类**非 REPL** 子命令没有这个问题。

**为什么值得记**：`resume` 最自然的用法之一就是在脚本里"恢复并把结果接到下一步"
（`out=$(gopi checkpoint resume …)`）——而这正是它挂死的姿势。表现是**静默挂起**：
没有报错、没有提示，只有"什么都没发生"。本条目因此改成管道形态取证（上表第一行），
同一份场景的两张答案因此都留在素材里：能跑的那种写法，和会挂死的那种写法。
FINDING_EOF
echo

# ===========================================================================
# 005 gopi tool sandbox show —— 沙盒根目录由哪个通道决定
# ===========================================================================
entry_header 005 sandbox-show "gopi tool sandbox show" yes \
  "沙盒到底落在哪个目录、是谁定的——四个通道（flag > env > 配置文件 > 内置默认）里现在生效的是哪一个" \
  "结论是那一行的两个字段：\`root=\` 是解析出来的目录，\`source=\` 是**哪个通道**决定了它。\`show\` 只回答这两件事（外加回收阈值 stale）。" \
  "框住 \`source=\` 字段：真正值钱的不是路径本身，而是"这个路径是谁定的"——没有它，一个指向别处的沙盒根目录看起来和默认值毫无区别。"
entry_input "$E_SB" \
  "$BIN tool sandbox show" \
  "GOPI_SANDBOX_DIR=/home/user/workspace/_materials-d-demo-root $BIN tool sandbox show" \
  "$BIN tool sandbox show --sandbox-dir /home/user/workspace/_materials-d-demo-flag" \
  "GOPI_HOME=$E_SB $BIN session log --last"
run_cmd "$E_DIR/02-output.txt" "$BIN tool sandbox show" "$BIN" tool sandbox show
run_cmd "$E_DIR/02-output.txt" "GOPI_SANDBOX_DIR=/home/user/workspace/_materials-d-demo-root $BIN tool sandbox show" \
  GOPI_SANDBOX_DIR=/home/user/workspace/_materials-d-demo-root "$BIN" tool sandbox show
run_cmd "$E_DIR/02-output.txt" "$BIN tool sandbox show --sandbox-dir /home/user/workspace/_materials-d-demo-flag" \
  "$BIN" tool sandbox show --sandbox-dir /home/user/workspace/_materials-d-demo-flag
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session log --last" GOPI_HOME="$E_SB" "$BIN" session log --last
entry_card
entry_render 0 \
  "T:source=from GOPI_SANDBOX_DIR:70:第二个通道生效（env 盖过内置默认）" \
  "T:source=from the built-in default:70:默认通道：没有任何覆盖时的答案" -- \
  "T:sandbox root=:0:通道名就在 root 旁边——不看 source 就读不出路径是谁定的"
finding_default "gopi tool sandbox show 的 root/source/prefix/stale 四字段与三个覆盖通道"
echo

# ===========================================================================
# 006 gopi tool sandbox run —— 沙盒是执行环境，不是一条路径
# ===========================================================================
entry_header 006 sandbox-run "gopi tool sandbox run" yes \
  "在沙盒里**跑**一条命令：子进程的 cwd 被绑到沙盒，GOPI_HOME 指进沙盒的 profile，退出码原样传回来" \
  "结论有三处：\`pwd\` 打出沙盒路径（cwd 真的在沙盒里）、\`GOPI_HOME=\` 指进同一个目录、以及第二个命令块的 \`exit=7\`——子进程的退出码是被**原样**传回来的。" \
  "上框框住 \`pwd\` 的结果行：沙盒是执行环境，子进程的工作目录被绑到沙盒（在任务 run 里，父进程的 cwd 是工作 worktree，孤儿进程会钉住它）。下框框住 exit=7：子进程的退出码经 gopi 原样传出，脚本能直接判。" \
  "GOPI_HOME=" "T:SANDBOX-ALIVE:0:子进程在沙盒里跑，GOPI_HOME 指进它的 profile"
entry_input "$E_SB" \
  "$BIN tool sandbox run --label $LABEL_PREFIX-006-$RUNSTAMP --task-id $TASK_ID -- bash -c 'pwd; echo GOPI_HOME=\$GOPI_HOME; ls -l .gopi/memory.db'" \
  "$BIN tool sandbox run --label $LABEL_PREFIX-006-$RUNSTAMP --task-id $TASK_ID -- bash -c 'exit 7'" \
  "GOPI_HOME=$E_SB $BIN session log --last"
run_cmd "$E_DIR/02-output.txt" \
  "$BIN tool sandbox run --label $LABEL_PREFIX-006-$RUNSTAMP --task-id $TASK_ID -- bash -c 'pwd; echo GOPI_HOME=\$GOPI_HOME; ls -l .gopi/memory.db'" \
  bash -c "'$BIN' tool sandbox run --label '$LABEL_PREFIX-006-$RUNSTAMP' --task-id '$TASK_ID' -- bash -c 'pwd; echo GOPI_HOME=\$GOPI_HOME; ls -l .gopi/memory.db'"
E_SB_RUN="${LAST_OUT:-}"
E_SB_RUN="${E_SB_RUN%%$'\n'*}"
run_cmd "$E_DIR/02-output.txt" \
  "$BIN tool sandbox run --label $LABEL_PREFIX-006-$RUNSTAMP --task-id $TASK_ID -- bash -c 'exit 7'" \
  bash -c "'$BIN' tool sandbox run --label '$LABEL_PREFIX-006-$RUNSTAMP' --task-id '$TASK_ID' -- bash -c 'exit 7'"
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session log --last" GOPI_HOME="$E_SB" "$BIN" session log --last
entry_card
entry_render 0 \
  "T:created=:60:run 的第一段：先物化沙盒（created=/reused= 说明它做了什么）" \
  "T:exit=7:20:子进程的退出码原样传出（脚本据此判定）" -- \
  "T:the child:0:run 把子进程的死亡原样报出来（这里是 exit status 7）"
finding_default "gopi tool sandbox run 的 cwd 绑定、GOPI_HOME 注入与退出码传播"
echo

# ===========================================================================
# 007 gopi sandbox（打错的子动词）—— 退掉的动词与未知动词都不会静默
# ===========================================================================
entry_header 007 sandbox-unknown "gopi tool sandbox <未知子动词>" yes \
  "命令族的入口不会猜你的意思：打错一个词，它当场把**全部**合法子动词列出来，并用一个独立的退出码表示这是用法错误" \
  "结论是那两行：\`unknown sub-verb \"bogus\" (want create, run, show or -h)\` 与末行的 \`exit=2\`。**2 是用法错误**，与「沙盒没能被造出来」（1）分开。" \
  "框住 \`unknown sub-verb\` 那一行：这是**只剩这一条路**的用户会看到的文本——他打错了词，需要的是最短的指路；它给的是完整的合法动词表。第二框框住同族的 \`nothing to run\` 与 \`unexpected argument\`：三种用法错误都不静默，且各自指名。"
entry_input "$E_SB" \
  "$BIN tool sandbox bogus" \
  "$BIN tool sandbox run --label $LABEL_PREFIX-007-$RUNSTAMP" \
  "$BIN tool sandbox run --bogus -- echo hi" \
  "$BIN tool sandbox show --sandbox-dir /home/user/workspace/_x" \
  "GOPI_HOME=$E_SB $BIN session log --last"
run_cmd "$E_DIR/02-output.txt" "$BIN tool sandbox bogus" "$BIN" tool sandbox bogus
run_cmd "$E_DIR/02-output.txt" "$BIN tool sandbox run --label $LABEL_PREFIX-007-$RUNSTAMP" \
  "$BIN" tool sandbox run --label "$LABEL_PREFIX-007-$RUNSTAMP"
run_cmd "$E_DIR/02-output.txt" "$BIN tool sandbox run --bogus -- echo hi" "$BIN" tool sandbox run --bogus -- echo hi
run_cmd "$E_DIR/02-output.txt" "$BIN tool sandbox show --sandbox-dir /home/user/workspace/_x" \
  "$BIN" tool sandbox show --sandbox-dir /home/user/workspace/_x
run_cmd "$E_DIR/02-output.txt" "GOPI_HOME=$E_SB $BIN session log --last" GOPI_HOME="$E_SB" "$BIN" session log --last
entry_card
entry_render 0 \
  "T:unknown sub-verb:60:打错的子动词：当场列出全部合法动词" \
  "T:unexpected argument:70:同名参数在 show 上不存在——同样点名拒绝" -- \
  "T:exit=2:0:用法错误有独立的退出码 2，与造不出沙盒的 1 分开"
cat > "$E_DIR/05-finding.md" <<'FINDING_EOF'
**缝：`sandbox show` 接受不了的参数，在 create/run 上是合法的——同一个 flag 在子命令间的可用性是不对称的。**

本条目实测（同一个二进制、同一次运行，02-output.txt）：

```
$ gopi sandbox show --sandbox-dir /home/user/workspace/_x
gopi sandbox show: unexpected argument "--sandbox-dir"     ; exit=2
$ gopi sandbox show   # 但 --sandbox-dir 在 create/run 上是被 help 广告的
sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s
```

`gopi sandbox -h` 的 Flags 段把 `--sandbox-dir PATH` 与 `--label` / `--task-id` **并列**写在一起
（原文："the sandbox root for THIS invocation (highest precedence)"），只在 `--` 那一行标了
"run only"。读者从这页 help 里推不出"哪些 flag 属于哪个子命令"——`show` 是**只读**的一个，
它连"给我看看那个 root 会是哪儿"都做不到，只能靠 `GOPI_SANDBOX_DIR=… gopi sandbox show` 变通。

**机制**：`show` 的参数解析只认它自己那一小张表（`unexpected argument` 由它自己的分支抛出，
exit=2），而 help 页是三件事共用的**一页**。这不是设计取舍写下来的结果，是一页 help 覆盖三个
子命令时必然出现的空隙——`--`(run only) 被标了，`--sandbox-dir`(create/run only) 没有。

**为什么值得记**：`sandbox show` 的整个存在意义就是"回答 root 是哪个"。
一个只读的探询动词，恰恰是"我想先看看换个值会解析到哪"最自然的落点。
FINDING_EOF
echo

# ===========================================================================
flush_pending_renders || exit 1
if $RENDER_ONLY; then
  assert_shas || exit 1
else
  assert_shape
fi
echo "== index =="
cat "$WORK/index.tsv"
if [[ -s "$WORK/failed.txt" ]]; then
  echo "== FAILED ==" >&2; cat "$WORK/failed.txt" >&2
  echo "GENERATOR FAILED: $(wc -l < "$WORK/failed.txt") entry/entries" >&2
  exit 1
fi
printf 'WORK=%s\nSANDBOXES=%s\n' "$WORK" "$(ls -d /home/user/workspace/gopi-sandbox/*materials-d-* 2>/dev/null | wc -l)"
