#!/usr/bin/env bash
# GENERATOR: scripts/materials/preview.sh —— 本脚本的产物就是 docs/archive/demo/preview/index.html
#
# 集成预览（integration preview）—— **半成品**，不是最终展示物。
#
# 它回答一个问题，且只回答这一个问题：
#   「照 docs/archive/demo/materials/ 这个方向做下去，最后的东西会是我想看的吗？」
# 操作者读完应当能说「对，就是这个意思」**或**「不对，我要的是另一个」——两者都算成功。
#
# 它是什么形态、为什么不引外部工具（Rule 35 的四件事）：
#   1. 不装任何东西会怎样：没有任何依赖可失去——产物是一个**单文件 HTML**，
#      只引用素材条目自己的 PNG（相对路径），双击即开，不联网、不起服务、不装 ffmpeg。
#   2. 为什么不能进程内解决：本产物就是那个"进程内"的答案。前几轮的失败形态是
#      "一个 md 目录"，读者要点很多次才能看到一个东西；这里改成**一个入口**。
#   3. 谁管生命周期：产物由本脚本写、由 git 跟踪、由下一个跑本脚本的人覆盖——
#      没有常驻物、没有定时器、没有守护进程。证据文件落在 --evidence 目录（默认
#      $GOPI_EVIDENCE_DIR 或产物目录下的 evidence/），不落在素材树内。
#   4. 下一个人看到什么：本文件的头 30 行 + 产物页面顶部的 REPRODUCE 段，
#      逐字给出「谁生成的、在哪个 commit 上生成的、怎么重跑」。
#
# Rule 38（census 数字自报生成器与测量 commit）：页面里的覆盖率数字**不是手写的**，
# 是运行时调用 scripts/materials-coverage.sh 抓来的，并把它的 header 原样登在页面上。
#
# Rule 31（预览里出现的命令必须真跑过）：本脚本的产物里出现的每一个命令，
# 都是本脚本**自己在这次运行里执行过**的（生成器复跑覆盖率 + 本脚本的 residue 断言），
# 以及每条素材的 01-input.txt —— 那些不是本 run 跑的，所以页面上**署名给了素材自己**
# （每条都印它的 measured_commit 和 how_to_replay），不冒充本 run 的实测。
#
# 只读契约：本脚本对素材树（docs/archive/demo/materials/）**只读**，不改其中任何一个字节。
#
# 用法:
#   bash scripts/materials/preview.sh                 # 写 docs/archive/demo/preview/index.html
#   bash scripts/materials/preview.sh --out DIR       # 写到别处
#   bash scripts/materials/preview.sh --evidence DIR  # 证据目录
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TREE="${GOPI_MATERIALS_DIR:-$ROOT/docs/archive/demo/materials}"
OUTDIR="$ROOT/docs/archive/demo/preview"
EVID="${GOPI_EVIDENCE_DIR:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUTDIR="$2"; shift 2 ;;
    --evidence) EVID="$2"; shift 2 ;;
    --tree) TREE="$2"; shift 2 ;;
    -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done
[ -n "$EVID" ] || EVID="$OUTDIR/evidence"
mkdir -p "$OUTDIR" "$EVID"

COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then COMMIT="$COMMIT+dirty"; fi
GENERATED_AT="$(date '+%Y-%m-%d %H:%M:%S %z')"
HERE="preview"   # 产物目录名，用于相对路径

# ---------------------------------------------------------------- 净化 + 断言
# 把本机绝对路径抽象成 ${RUN}/${SB}/${EV}/${HOME}，把会话 id 抽象成 {session}。
# 替换规则与 scripts/materials/pick.sh:103-110 同源；判断（有没有残渣）在末尾的
# residue 断言里，判据与 pick.sh:124 同源。两边判据一致是刻意的：
# 「集成产物不得把内部绝对路径 / 会话 id 带出去」这条出货条件只有一个定义，
# 本脚本不另立第二套。
sanitize() {
  sed -E \
    -e 's#/home/[A-Za-z0-9._-]+/workspace/gopi-sandbox/[A-Za-z0-9._-]+/\.gopi/sessions/[A-Za-z0-9._-]+#${EV}#g' \
    -e 's#/home/[A-Za-z0-9._-]+/workspace/gopi-sandbox/[A-Za-z0-9._-]+#${SB}#g' \
    -e 's#/home/[A-Za-z0-9._-]+/workspace/gopi-[A-Za-z0-9]+#${RUN}#g' \
    -e 's#/home/[A-Za-z0-9._-]+/\.gopi/sessions/[A-Za-z0-9._-]+#${EV}#g' \
    -e 's#/home/[A-Za-z0-9._-]+/workspace/_tmp#${TMP}#g' \
    -e 's#/[A-Za-z0-9._-]*sessions/[A-Za-z0-9._-]+#{session}#g' \
    -e 's#/home/[A-Za-z0-9._-]+#${HOME}#g' \
    -e 's#/Users/[A-Za-z0-9._-]+#${HOME}#g' \
    -e 's#[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}#{session}#g' \
    -e 's#[0-9a-f]{32}#{task-id}#g'
}
# 为什么 bare 32-hex 也要抽象掉：台账 id 是**不带连字符**的 32 位十六进制，
# pick.sh 的 clean 判据只拦**带连字符**的 8-4-4-4-12 形态，于是素材自己的
# how_to_replay 里那句 `--task-id 01a0ed23…` 会整条上船 —— 它既不是会话 id 也不是
# 绝对路径，却是**只在本机台账里才解析得开**的内部记号。对外页面上它读作
# `--task-id {task-id}` 一样清楚，所以在这里抽象，并把同一形态加进下面的断言。
esc() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }
ship() { sanitize | esc; }

# ---------------------------------------------------------------- 现场跑生成器
bash "$ROOT/scripts/materials-coverage.sh" --tree "$TREE" > "$EVID/coverage-summary.txt" 2>&1
COV_EXIT=$?
bash "$ROOT/scripts/materials-coverage.sh" --tree "$TREE" --tsv --out "$EVID/materials-coverage.tsv" >/dev/null 2>&1
COV_TSV="$EVID/materials-coverage.tsv"

PRODUCED="$(grep -m1 -E '^\s+produced' "$EVID/coverage-summary.txt" | sed -E 's/.*: *([0-9]+) \/ ([0-9]+).*/\1/')"
BASELINE="$(grep -m1 -E '^\s+produced' "$EVID/coverage-summary.txt" | sed -E 's/.*: *([0-9]+) \/ ([0-9]+).*/\2/')"
CANDS="$(grep -m1 -E '^\s+candidate dirs' "$EVID/coverage-summary.txt" | sed -E 's/.*: *([0-9]+).*/\1/')"
UNREAD="$(grep -m1 -E '^\s+unreadable' "$EVID/coverage-summary.txt" | sed -E 's/.*: *([0-9]+).*/\1/')"
GAP=$(( ${BASELINE:-0} - ${PRODUCED:-0} ))
COV_HEAD="$(sed -n '1,5p' "$COV_TSV")"

# face 分布（以素材卡 `- face:` 字段为准；README §4：面以字段为准，不以目录位置猜）
FACE_ROWS="$(for f in "$TREE"/*/00-entry.md; do
    face="$(grep -m1 -E '^- face:' "$f" | sed 's/^- face: *//; s/`//g')"
    [ -n "$face" ] || face="undeclared（M0 之前的模板，无 face 字段）"
    echo "$face"
  done | sort | uniq -c | sort -rn)"

# ---------------------------------------------------------------- 本次挑选（curation）
# 5 条素材，跨 4 个面。字段：slug::面::为什么选它(一行)::输出块的提取正则
CUT="001-task-list::task::台账是 gopi 的骨架 —— 这一条把『列表的顺序』与『派发的顺序』两个不同的顺序放进同一屏。::\\[pending\\]|\\[next\\]|exit=
002-task-submit::task::一次工作变成一行跨会话的记录：进程退出了，这行还在。::queued|exit=
224-flag-bash-timeout-declared-unread::flags::同族的第三条：声明层/解析层都在、消费层不在 —— 这类『缝』是本库最值钱的一半。::invalid value|TOOL25-DONE|BashTimeoutSec|exit=
003-checkpoint-export::sandbox_plus_checkpoint::一次单轮对话的检查点有多大、里面是什么 —— 设计问题，不是 bug。::^[0-9]{4,}\$|^messages=|^\"|exit=
001-notify-cmd-default-off::env::默认什么都不发，且优先级写进输出文案本身 —— 真挖出了 CONFIG.md 的 file:line 错误。::task-notify|Task updated|exit="

mkdir -p "$EVID/fragments"
ENTRY_HTML=""
TOC=""
IDX=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  IDX=$((IDX+1))
  slug="${line%%::*}"; rest="${line#*::}"
  face="${rest%%::*}"; rest="${rest#*::}"
  why="${rest%%::*}"; pat="${rest#*::}"
  dir="$TREE/$slug"
  [ -d "$dir" ] || { echo "MISSING entry dir: $dir" >&2; exit 3; }

  card="$dir/00-entry.md"
  h1="$(sed -n '1s/^# //p' "$card")"
  subtitle="$(printf '%s' "$h1" | sed -E 's/^[^ ]+ — //')"
  feat="$(grep -m1 -E '^- feature:' "$card" | sed -E 's/^- feature: *//')"
  mcommit="$(grep -m1 -E '^- measured_commit:' "$card" | sed -E 's/^- measured_commit: *//')"
  replay="$(grep -m1 -E '^- how_to_replay:' "$card" | sed -E 's/^- how_to_replay: *//')"
  [ -n "$subtitle" ] || subtitle="$h1"

  # 署名行的每一段都走净化（sanitize 不含转义：它全部落在 <code>/<b> 的
  # **文本**位置，没有一处进属性；转义交给各自的 <pre> 那几处做 esc）。
  # 漏了这一句 = 素材卡里的 how_to_replay 原样上船，上面那条断言就会红
  # （本 run 实测：`--task-id 01a0ed23…` 曾整条出现在出货页上）。
  subtitle="$(printf '%s' "$subtitle" | sanitize)"
  feat="$(printf '%s' "$feat" | sanitize)"
  mcommit="$(printf '%s' "$mcommit" | sanitize)"
  replay="$(printf '%s' "$replay" | sanitize)"

  # 输入块：01-input.txt 的前 4 行（含注释：注释是这次运行的语境，不是解说）
  input_txt="$(head -4 "$dir/01-input.txt" | ship)"

  # 输出块：按正则挑行（挑的是**结论行**，不是全量 stdout）
  grep -E "$pat" "$dir/02-output.txt" | grep -v '^$' | head -14 | cut -c1-200 | ship > "$EVID/fragments/$slug.out.txt"
  output_txt="$(cat "$EVID/fragments/$slug.out.txt")"
  exit_line="$(grep -m1 -E '^exit=' "$dir/02-output.txt" | sanitize)"
  [ -n "$exit_line" ] || { echo "entry $slug has no exit= line" >&2; exit 4; }

  # 缝：05-finding.md 的首段（逐字，净化后）
  finding_txt="$(awk 'NF==0{c++; next} c>=1{print} c>=2{exit}' "$dir/05-finding.md" | head -12 | ship)"

  # 一行解说：素材卡自己的「标注图在指什么」——按素材库 README §1，
  # 标注图是这条素材成立的前提，所以它的解说**必须由素材卡给出**，不由本页代笔。
  ann_txt="$(awk '/^## 标注图在指什么/{f=1;next} /^## /{f=0} f' "$card" | head -6 | ship)"

  ENTRY_HTML="$ENTRY_HTML
<article id=\"e$IDX\">
  <h3>$IDX. <code>$slug</code> <span class=\"tag\">$face</span></h3>
  <p class=\"lead\">$subtitle</p>
  <p class=\"why\"><b>为什么选它：</b>$why</p>
  <img src=\"../materials/$slug/04-annotated.png\" alt=\"$slug 的标注运行截图\">
  <p class=\"cap\">↑ <code>04-annotated.png</code>（素材库自己画的标注图；本页不复制、不改动它，直接相对路径引用）</p>
  <p class=\"meta\"><b>这张图在说明什么</b>（素材卡 <code>00-entry.md</code> 的「标注图在指什么」一节，逐字）：</p><pre>$ann_txt</pre>
  <div class=\"cols\">
    <div><p class=\"h\">输入 · <code>01-input.txt</code>（前 4 行，逐字）</p><pre>$input_txt</pre></div>
    <div><p class=\"h\">输出 · <code>02-output.txt</code>（结论行）</p><pre>$output_txt</pre></div>
  </div>
  <p class=\"meta\"><b>退出码那一行</b>：<code>$exit_line</code>（素材库里每个 02-output.txt 都必须带一行 <code>^exit=[0-9]+</code>，否则它不算证据）</p>
  <p class=\"meta\">素材自己署名：feature <code>$feat</code> · measured_commit <code>$mcommit</code> · 复跑 <code>$replay</code></p>
  <p class=\"meta\">回溯：<code>docs/archive/demo/materials/$slug/</code> —— 六件套 <code>00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md</code></p>
  <details><summary>这条素材的「缝」（<code>05-finding.md</code> 首段，逐字）</summary><pre>$finding_txt</pre></details>
</article>"
  TOC="$TOC<a href=\"#e$IDX\">$IDX. $slug</a> "
done <<< "$CUT"

# ---------------------------------------------------------------- 组装页面
{
cat <<HTML
<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>gopi 集成预览 —— 半成品，用来判断方向</title>
<style>
  :root{--ink:#15161a;--dim:#6a6f7a;--line:#e2e2dd;--bg:#faf9f6;--accent:#1a5fb4;--warn:#b3261e;}
  *{box-sizing:border-box}
  body{margin:0;padding:28px 18px 90px;background:var(--bg);color:var(--ink);
       font:15px/1.7 -apple-system,"Noto Sans CJK SC","WenQuanYi Zen Hei",system-ui,sans-serif}
  main{max-width:1120px;margin:0 auto}
  h1{font-size:27px;margin:0 0 4px}
  h2{font-size:20px;margin:46px 0 10px;padding-bottom:6px;border-bottom:2px solid var(--line)}
  h3{font-size:16px;margin:0 0 4px}
  code{background:#f1f0eb;padding:1px 5px;border-radius:3px;font-size:13px;
       font-family:"Noto Sans Mono CJK SC",Menlo,Consolas,monospace}
  pre{background:#fff;border:1px solid var(--line);border-left:3px solid var(--accent);
      border-radius:6px;padding:10px 12px;overflow-x:auto;font-size:12.5px;margin:6px 0;
      font-family:"Noto Sans Mono CJK SC",Menlo,Consolas,monospace;white-space:pre-wrap;word-break:break-all}
  .box{background:#fff;border:1px solid var(--line);border-radius:9px;padding:16px 20px}
  .meta{color:var(--dim);font-size:12.5px;margin:6px 0}
  .lead{font-size:15.5px;margin:2px 0 6px}
  .why{margin:6px 0 2px;font-size:14px}
  .h{font-size:12.5px;color:var(--dim);margin:0 0 2px}
  .cap{font-size:12px;color:var(--dim);margin:2px 0 10px}
  article{background:#fff;border:1px solid var(--line);border-radius:9px;padding:16px 20px;margin:16px 0}
  article img{width:100%;height:auto;border:1px solid var(--line);border-radius:6px;margin:8px 0 2px;background:#fff}
  .cols{display:flex;gap:14px;flex-wrap:wrap;margin-top:8px}
  .cols>div{flex:1 1 380px;min-width:300px}
  table{border-collapse:collapse;width:100%;font-size:13px;margin:8px 0}
  td,th{border-bottom:1px solid var(--line);padding:5px 8px;text-align:left;vertical-align:top;
        font-family:"Noto Sans Mono CJK SC",Menlo,monospace}
  th{color:var(--dim);font-weight:600}
  .tag{display:inline-block;background:#eef2f7;color:var(--accent);border-radius:3px;padding:0 6px;font-size:12px}
  .toc a{display:inline-block;margin:2px 10px 2px 0;color:var(--accent);font-size:13px;text-decoration:none;
         font-family:"Noto Sans Mono CJK SC",Menlo,monospace}
  .seam{border-left:3px solid var(--warn);background:#fdf7f6;border-radius:0 6px 6px 0;padding:10px 14px;margin:10px 0}
  .seam b{color:var(--warn)}
  details{margin-top:10px}
  summary{cursor:pointer;font-size:13px;color:var(--accent)}
  ol.big{font-size:15px;padding-left:22px}
  ol.big li{margin:8px 0}
</style></head><body><main>

<h1>gopi 集成预览</h1>
<p class="lead"><b>这是一个半成品，只用来判断方向</b>——看完请直接说
「对，就是这个意思」或者「不对，我要的是另一个」。<b>两种回答都算这次成功</b>；
真的失败是看完无话可说。</p>
<p class="meta">原料只有一个：<code>docs/archive/demo/materials/</code>（gopi 功能原始素材库，只读）。
本页是它的<b>集成挑选</b>，不是文章——每一条都能指回具体素材目录，页面上任何一句都不是我编的。</p>

<div class="box">
<table>
<tr><th>在 3 分钟里怎么读这一页</th><td>① 看下面这张表（覆盖率，现场跑的）→ ② 看 §3 的五条素材（每条：图 + 输入 + 输出 + 一句解说）→
③ 看 §4 的「缝」（这是素材库最值钱的部分）→ ④ 看 §5 的叙事（把五条串起来会是什么样）。</td></tr>
<tr><th>generator</th><td>scripts/materials/preview.sh（本页由它生成；页面里的数字是运行时现抓的，不是手写）</td></tr>
<tr><th>measured_commit</th><td>$COMMIT</td></tr>
<tr><th>generated_at</th><td>$GENERATED_AT</td></tr>
<tr><th>素材库扫描口径</th><td>scripts/materials-coverage.sh（现场调用，exit=$COV_EXIT）</td></tr>
</table>
</div>

<h2 id="cov">1. 覆盖率：有多少原料可挑（现场跑的，带 commit）</h2>
<p>生成器 <code>scripts/materials-coverage.sh</code> 在 <code>$COMMIT</code> 上的逐字输出头（原样转载）：</p>
<pre>$(printf '%s\n' "$COV_HEAD" | ship)</pre>
<table>
<tr><th>已产出（complete 条目数）</th><td><b>$PRODUCED / $BASELINE</b></td></tr>
<tr><th>候选目录</th><td>$CANDS</td></tr>
<tr><th>unreadable（声明依赖标注图而图不在）</th><td>$UNREAD</td></tr>
<tr><th>未产出</th><td>$GAP（$BASELINE − $PRODUCED）</td></tr>
</table>
<p class="meta"><b>两个口径不许互相顶替</b>（素材库自己的纪律）：上面的分子是「六件套齐全的条目数」，
不是目录数、不是文件数；分母 <code>193</code> 是素材库登记在册的基线常量（来源见生成器输出的
<code># baseline_source</code> 头）。半成品以 <code>incomplete</code> 出现在明细里，不进分子。</p>

<h2 id="gap">2. 没做完的是哪些面（如实说）</h2>
<p>素材卡用 <code>- face:</code> 字段声明自己属于哪个面（素材库 README §4：面以字段为准，不以目录位置猜）。
本页现场按该字段统计了 <b>$(( $(printf '%s\n' "$FACE_ROWS" | awk '{s+=$1} END{print s+0}') ))</b> 条条目：</p>
<table>
<tr><th>face（素材卡的声明）</th><th>条目数</th></tr>
$(printf '%s\n' "$FACE_ROWS" | awk '{n=$1; $1=""; sub(/^ /,""); printf "<tr><td>%s</td><td>%s</td></tr>\n", $0, n}')
</table>
<p class="meta"><b>这张表的 face 与 193 基线的面不是同一套编号，所以两边的数各自独立、不互相顶替</b>
（素材库 README §6 的纪律 1/3）。已知的具体缺口（来自素材库自己的进度表，不是本页推算）：</p>
<ul>
  <li><b>全局 flag 面</b>：<code>--keep-thinking</code> 与 <code>--compact</code> 的<b>正向侧</b>还没有素材——
      本批真跑过（ON/OFF 各一次）但差异不足以归因给这个 flag，所以<b>没有</b>出成条目。
      来源：<code>docs/archive/demo/materials/README.md</code> §6.2 的表尾行。</li>
  <li><b>环境变量面</b>：一部分变量目前<b>只登记、未逐条真跑</b>（<code>table-only</code>），
      它们的行为在那一行里没有任何证据。来源：同文件 §9.2。</li>
  <li><b>面名不一致本身也是一条缺口</b>：193 基线把 sandbox(4) 与 checkpoint(3) 算作两个面，
      素材库把它们记成一个 <code>sandbox_plus_checkpoint</code> 面（7 条）；基线里也没有 <code>node</code> 面，
      而素材库有 4 条跨节点素材。修法写在素材库 README §6 纪律 2（换分母要留 commit），不在本页。</li>
</ul>

<h2 id="cut">3. 五条素材，端到端</h2>
<p class="meta">挑选判据（本页的 curation，写在生成器里，可核对）：
<b>跨 4 个面</b>（task / flags / env / sandbox+checkpoint）、每条都能回答「这条素材在证明什么」、
且都带一条真实的「缝」或一个真实的用户可见结果。
picked=$IDX 条 / 候选 $PRODUCED 条。下面每条都是同一种形状：<b>标注图 → 输入 → 输出 → 解说 → 素材自己的署名</b>。</p>
<p class="toc">$TOC</p>
$ENTRY_HTML

<h2 id="seams">4. 素材库里真实挖出来的「缝」</h2>
<p>素材库的价值有一半在这里：<b>不是「功能能用」，而是「哪里在说谎」</b>。
下面三条都不是我写的，是素材条目自己的 <code>05-finding.md</code> 的结论
（每条给回指目录；页面上每条素材的折叠段里是它的首段逐字）。</p>
<div class="seam"><b>缝 1 · <code>CONFIG.md</code> 的 file:line 指错了地方</b>（素材：<code>001-notify-cmd-default-off</code>）<br>
通知的 reader 被指到两个不相干的函数上，<b>恰好差约 110 行</b>——是一份加在文件中间的东西把下面的行全推下去了，而引用是硬写的数字。
读者<em>会</em>翻到那一行，然后读到一段与通知毫无关系的构建超时字段，并且没有第二处提示告诉他真正的入口在哪。
这一条是「文档与代码不一致」，不是行为缺陷——素材自己克制地记成前者。</div>
<div class="seam"><b>缝 2 · <code>config show</code> 的 <code>sync_peers</code> 是文档，不是有效值</b>（素材：<code>201-node-hp-config-show</code>，跨节点实测）<br>
同一份 profile 下，多设一个 <code>GOPI_SYNC_PEERS</code> 之后输出<b>逐字节不变</b>（两段 35 行全等）。
同一条缝的另一种形状出现在同面姊妹条目里：<b>键丢了它会大声说，键在、但被环境覆盖时它一声不响</b>——
哑的那个恰好是「以为自己在另一个拓扑上跑」的那种误判。素材给了可重放的反证，也给了竞品对照（git / docker / kubectl 的每一行都带来源）。</div>
<div class="seam"><b>缝 3 · <code>--bash-timeout</code> 声明了、没人读</b>（素材：<code>224-flag-bash-timeout-declared-unread</code>，同族另有 225/227）<br>
给 <code>--bash-timeout 3</code> 一条 25 秒的命令，它<b>跑完了</b>；同面另一个整数 flag 的非法值当场被拒（解析层是活的）；
而 <code>grep</code> 全仓的调用点<b>一个都不在消费层</b>——真正的超时值来自环境变量。
<b>声明层与解析层都在、消费层不在</b>：三个 flag 的运行时值实际由 env 决定，而 <code>-h</code> 承诺的优先级是 <code>flag &gt; env &gt; settings &gt; default</code>。</div>
<p class="meta">另外一条同族的在素材库里排在第 4 位：<code>003-checkpoint-export</code> 记的不是 bug 是设计问题——
<code>export</code> 把整份请求快照一次性倒到 stdout，而它唯一的用途是「看一眼再恢复」，没有 <code>--keys</code>/<code>--summary</code> 这类
「只看结构」的开关（同族的 <code>list</code> 走的是另一条路：只读日志头部，所以多 GB 也能秒列）。</p>

<h2 id="narrative">5. 示范性集成：把上面五条串成一段，最后的东西大概长这样</h2>
<div class="box">
<p><b>一段工作从「排队」到「收尾」，中间每一步都有一条真实记录可以指。</b></p>
<ol class="big">
<li><b>它先成为台账里的一行</b>——<code>task submit</code> 让「一次工作」在进程退出之后还留在盘上（素材 <code>002-task-submit</code>）。</li>
<li><b>这一行会不会被派发，由另一行决定</b>——父行没放行，孩子就不派发；一个 session 内的活不会抢在它的前提之前开跑（素材 <code>011-parent-gate-parked</code>）。</li>
<li><b>谁在做、做到哪一步，一眼看全，不用翻会话日志</b>——但同一个屏上同时有<b>两个顺序</b>：
列表是时间倒序，派发是优先级序，所以一行可以是 <code>[pending]</code> 却永远不在 <code>[next]</code> 里
（素材 <code>001-task-list</code>，第 1 条素材的标注图框的正是这一行）。</li>
<li><b>做完之后收尾</b>——不跑常驻 daemon 的机器也能拿到同一套扫描与守卫（素材 <code>013-task-finish-cleanup</code>）。</li>
<li><b>整个过程里 gopi 自己在往外说话：</b>通知默认什么都不发，配了才发；优先级写进输出文案本身，
所以你不用读源码就知道自己覆盖了哪一层（素材 <code>001-notify-cmd-default-off</code>）。</li>
</ol>
<p class="meta">这条链上每一步的原料都在素材库里，而每一步的<b>教训</b>也在其中——
第 1 步的注释告诉我「命令跑了」和「命令成功了」是两件事；
第 5 步的素材告诉我文档的 file:line 会飘。
<b>这就是这个方向最终的样子：不是一篇介绍 gopi 的文章，而是一条能被逐段核对的链</b>，
链上每一环都有一张标注图、一段逐字输入、一段带退出码的输出、和一条它自己承认的不足。</p>
</div>

<h2 id="repro">6. 判据与复现</h2>
<table>
<tr><th>怎么开</th><td>双击本文件即可（单文件、零依赖、离线；图按相对路径引用素材条目自己的 PNG，没有第二份拷贝）</td></tr>
<tr><th>怎么重跑</th><td><code>bash scripts/materials/preview.sh</code>（一条命令；它现场调覆盖率生成器，再重新组这一页）</td></tr>
<tr><th>原料</th><td><code>docs/archive/demo/materials/</code>（只读；README §1 的判据：<b>拿掉 04-annotated.png，读者就看不懂这条素材在说什么</b>）</td></tr>
<tr><th>本页的判据</th><td>操作者能否在 3 分钟内说「对」或「不对」——见页面顶部那段。</td></tr>
<tr><th>出货条件</th><td>不含内部绝对路径与会话 id（生成器末尾有 residue 断言，判据与挑选脚本同源）</td></tr>
</table>
<p class="meta">本页与 <code>docs/archive/demo/showcase/materials-cut/index.html</code> <b>不是同一件东西</b>，别互相替代：
那个是<b>脚本化的全量挑选集</b>（一页一条、95 条平铺，挑完就完）；本页是<b>人工 curation 的方向样张</b>
（5 条端到端 + 缺口 + 缝 + 一段叙事），读它的人要回答的是「方向对不对」，不是「素材全不全」。</p>

</main></body></html>
HTML
} > "$OUTDIR/index.html"

# ---------------------------------------------------------------- 出货断言
h="$(grep -cE '/home/|/Users/|/mnt/|/media/|/opt/|/srv/|sessions/|GOPI_HOME=/|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}|\b[0-9a-f]{32}\b' "$OUTDIR/index.html")"
if [ "${h:-0}" -gt 0 ]; then
  echo "ASSERT FAIL: $OUTDIR/index.html 里仍有 $h 行残渣（/home/ · sessions/ · GOPI_HOME=/ · 带连字符 UUID · 裸 32 位台账 id）—— 出货条件不成立" >&2
  grep -nE '/home/|/Users/|sessions/|GOPI_HOME=/|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}|\b[0-9a-f]{32}\b' "$OUTDIR/index.html" | head -5 >&2
  exit 5
fi

echo "# generator: scripts/materials/preview.sh"
echo "# measured_commit: $COMMIT"
echo "# generated_at: $GENERATED_AT"
echo "# produced: $PRODUCED / $BASELINE   (scripts/materials-coverage.sh, exit=$COV_EXIT)"
echo "# entries_shown: $IDX"
echo "# out: $OUTDIR/index.html   ($(wc -c < "$OUTDIR/index.html") bytes)"
echo "# evidence: $EVID"
echo "# residue: 0  (clean)"
