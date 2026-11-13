#!/usr/bin/env bash
# gen-task17-CJK.sh — 一条命令重产 demo 素材 17 条的中文截图。
#
# 背景（根因，不是猜测）：
#   17 条素材的 03-raw.png / 04-annotated.png 里中文全是 Latin-1 乱码。
#   02-output.txt 本身是完好 UTF-8（已用 file/iconv 验过），字体也在，
#   坏掉的只有**渲染环节**：旧生成器用 `xterm(:77)` 起屏 + `gopi capture` 截图，
#   而脚本环境里没有 LANG/LC_ALL —— xterm 于是按 Latin-1 逐字节画，
#   一个 UTF-8 汉字（3 字节）被当成 3 个 Latin-1 字符。
#   同一张图的标注中文却是好的：`gopi annotate` 走 internal/text2img 的字体链。
#   == 一图两制 ==
#
# 本脚本把截图侧也改走 `gopi text2img`（internal/text2img 的 CLI 脸）：
#   自研 SFNT/TrueType 解析 + 4x 超采样栅格化，零 CGO、零外部依赖，
#   **全程没有 locale 这个概念** —— 没有 locale 就不能误读字节。
#   取舍见 devdoc/materials-cjk-render-fix.md（A/B/C 三路的理由）。
#
# 标注：框字段/框词，不框整行（框整行没有焦点）。定位用 PIL 读出的 TrueType
#   advance 算 needle 的 x 起止，标注后做三项像素自证（见 render-entry.py）。
#
# 用法:
#   bash scripts/materials/gen-task17-CJK.sh --bin <gopi> --font <ttf> --scratch <目录>
#   bash scripts/materials/gen-task17-CJK.sh ... --only 01-list     # 单条真跑（最小样本）
set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MAT="${MATERIALS_DIR:-$HERE/../../docs/archive/demo/materials}"
BIN=""
FONT=""
SCRATCH=""
ONLY=""

while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --font) FONT="$2"; shift 2 ;;
    --scratch) SCRATCH="$2"; shift 2 ;;
    --mat) MAT="$2"; shift 2 ;;
    --only) ONLY="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$BIN" ] || { echo "--bin <gopi 二进制> 必需（不猜，不查 PATH）" >&2; exit 2; }
[ -n "$FONT" ] || { echo "--font <CJK ttf> 必需（显式指定，不依赖自动探测）" >&2; exit 2; }
[ -n "$SCRATCH" ] || { echo "--scratch <中间件目录> 必需（.txt 中间件不落进 worktree）" >&2; exit 2; }
[ -x "$BIN" ] || { echo "binary 不可执行: $BIN" >&2; exit 2; }
[ -f "$FONT" ] || { echo "font 不存在: $FONT" >&2; exit 2; }
mkdir -p "$SCRATCH"

# entry | needle（必须在 02-output.txt 证据区里逐字出现）| label（简短，无半角冒号）| caption（图顶一句）
CFG=$(cat <<'TSV'
01-list	[next]	下一个被派发的行	列表按时间倒序，派发按优先级序：同一个 [next] 标记，两个顺序在一屏里看得见。
02-show	run_dir:	run_dir 这个字段	show 给的不只是一行摘要，还有能 tail -f 的会话与 raw 日志路径。
03-status	evidence:	evidence 这个字段	本例明确写着没有认领戳记——非零判词也是证据。
04-submit	tags=docs,demo	提交时给的标签	提交回显把 source 与 tags 都摆出来，台账里这一行从哪来一眼可查。
05-update	reason=	reason 逐字回显	reason 逐字进回显，并落进 append-only 的 task_events。
06-verify	[task-verify]	verify 的输出头	先说把这行放在哪块私有地面，再报子进程结局。
07-archive	Task already archived:	幂等回显	状态不变，只有 tags 多了 resolved。
08-search	2 task(s) matching	命中计数	2 行同摘要的孪生行被一起列出来——正是要 dedup-scan 的情形。
09-show-branch	ledger:	拒绝的理由	没有行认领这条分支时，git 侧读到的事实照样并排打出，并且明说台账读了几行。
10-sweep-worktrees	would-reclaim=0	回收计数	计数行说明这一轮没有任何工作树会被回收，紧接着逐项给理由。
11-sweep-zombies	no zombie rows	空态	心跳早就停了的行才算僵尸：默认只报告，不动手。
12-finish-cleanup	would-run=0	本轮会跑几条	逐行计划 + 每条被豁免的理由（本例是 branch master is exempt）。
13-improvements	0 candidate pattern(s)	诚实的零	三个数字与一句"没有模式过线"：写 0，而不是空白。
14-dedup-scan	twin pair(s)	孪生计数	12 行扫出 17 对孪生，每对后面跟着判它的闸（gate=similar / exact）。
15-verdicts	no reframe records yet	空态措辞	空态写的是"还没有记录"，不是 0，也不是空白。
16-tag-resumed	resumed:00000000-0000-0000-0000-000000000000	全零会话 id	resumed 的值被原样接受，连全零 id 也不校验（见 05-finding.md）。
17-reopen	parent=	parent 指回原行	重开不是把原行改回 pending，而是长出一行 -r2 指回它，原行记录不动。
TSV
)

total=0; ok=0; failed=""
while IFS=$'\t' read -r entry needle label caption; do
  [ -n "$entry" ] || continue
  if [ -n "$ONLY" ] && [ "$ONLY" != "$entry" ]; then continue; fi
  d="$MAT/$entry"
  if [ ! -d "$d" ]; then echo "SKIP $entry: 目录不存在 $d" >&2; failed="$failed $entry"; continue; fi
  total=$((total+1))
  if python3 "$HERE/render-entry.py" --dir "$d" --needle "$needle" --label "$label" \
       --caption "$caption" --font "$FONT" --bin "$BIN" --scratch "$SCRATCH" \
       --json-out "$SCRATCH/$entry.json" > "$SCRATCH/$entry.log" 2>&1; then
    ok=$((ok+1))
    python3 - "$SCRATCH/$entry.json" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
print("PASS %-20s %sx%s lines=%d box=%dx%d(%d%%宽) ink=%d changed=%d covered=%d"
      % (r["entry"], r["size"][0], r["size"][1], r["lines_rendered"],
         r["box"][2], r["box"][3], round(r["width_share"]*100),
         r["ink_under_box"], r["changed_pixels"], r["label_covered_ink"]))
PY
  else
    echo "FAIL $entry: $(tail -2 "$SCRATCH/$entry.log" | tr '\n' ' ')" >&2
    failed="$failed $entry"
  fi
done <<< "$CFG"

echo "── gen-task17-CJK: $ok/$total 产出，failed:${failed:- none}"
[ "$ok" = "$total" ] && [ "$total" -gt 0 ]
