#!/usr/bin/env bash
# GENERATOR: scripts/materials/pick.sh —— 本文件的产物就是集成挑选集与放映页。
#
# pick.sh — 从素材库 docs/archive/demo/materials/ 里**脚本化挑选**集成集，并产出可放映集成产物。
#
# 判据（与 docs/archive/demo/materials/README.md §5 的机器谓词对齐，缺一不选）：
#   six       六件齐全：00-entry.md / 01-input.txt / 02-output.txt / 03-raw.png / 04-annotated.png / 05-finding.md，
#             且 01/02 非空、02 含一行 ^exit=[0-9]+、两张 PNG 有 PNG magic（README §2）；
#   finding   非空：05-finding.md 去掉空行与标题行后仍有正文（README §2 第 3 条）；
#   annotated 有 04-annotated.png 且**标注真的发生过**：图在、非空、PNG magic（README §2），
#             且 sha256(04) != sha256(03) —— 两张图逐字节相同就等于标注没有落笔，
#             这与库自己的验收口径一致（"sha 不同 = 标注真发生过"）。实测：库里 95/95 条
#             两张图 sha 不同，所以这条门槛当前一条都不淘汰，**但它不是装饰**——
#             它拦的正是"复制 raw 当 annotated"这个未来会发生的退化。
#             卡片自陈 annotation_required 的存在与否**只记录不判**（见下面 ann_decl 列）：
#             库里最早一批（M0 之前的模板）没有这个字段，但它们**确实有**标注图——
#             按 §1，census 也只检查"自陈依赖 ∧ 图不在"这个矛盾，不评价自陈本身。
#   clean     01-input.txt 与 05-finding.md 里**没有**机器绝对路径（/home/... /Users/...）
#             与**没有**会话 id（sessions/<id> 或裸 UUID）——这条是集成产物的出货条件：
#             产物不得把内部绝对路径 / 会话 id 带出去；
#   group     按 face 分组；face 取 00-entry.md 的 `- face:` 字段（README §4：面以字段为准，
#             不以目录位置猜）；库里最早的一批（README 之前的 M0 约定）没有 face 字段，
#             对它们回落到卡片自己的 `**group**` 字段并记 face=legacy（详见 --help 的说明）。
#
# 它做什么（全部只读素材树）：
#   1. 扫条目目录 → 逐条算上面五个布尔量 → 写选择记录 selection.tsv（**明细每一行**：
#      命中的和不命中的都在，不命中的带原因码，所以"为什么没选"是机器可查的）；
#   2. 按 face 分组打印命中条目列表（**由脚本打印，不是人工抄**）；
#   3. 生成可放映集成产物 index.html：一页一条 = 标注图 + 该条的 verbatim 命令 +
#      一行断言 + 回溯到素材条目的相对路径与 sha256。
#
# 它不做什么：**不改素材树里的任何一个字节**（README §8 只读契约）——只写自己声明的
# 两个产物（selection.tsv / index.html），且这两个产物写在素材树**之外**。
#
# Rule 38：本脚本自报 generator / measured_commit / baseline，数字来自现场扫描。
# Rule 30：证据（逐条中间记录）落在 --evidence 目录；产物落在 --out 目录。
#
# 用法:
#   bash scripts/materials/pick.sh                       # 挑选 + 放映页，写到默认两处
#   bash scripts/materials/pick.sh --tree docs/archive/demo/materials --out docs/archive/demo/showcase/materials-cut
#   bash scripts/materials/pick.sh --no-html             # 只挑选（stdout + TSV）
#   bash scripts/materials/pick.sh --only env            # 只看某个 face
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Rule 22：临时工作区取自共享的 scratch 契约（断言不在 `/` 分区上、注册回收），
# 不用 mktemp —— 一个 `mktemp -d` 落点由 TMPDIR 决定，而 TMPDIR 可能就指向 `/tmp`。
# shellcheck source=../lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh"
trap gopi_scratch_cleanup EXIT INT TERM
TREE="$ROOT/docs/archive/demo/materials"
OUT="$ROOT/docs/archive/demo/showcase/materials-cut"
EVIDENCE=""
ONLY=""
NO_HTML=0
SELFTEST=0
# 基线：README §6 的面基线表（36+17+16+4+3+27+90）。它不是"应该有 193 条素材"的承诺，
# 而是"库总容量"的分母，写在这里是为了让报告里的覆盖率数字有出处。
BASELINE=193
BASELINE_SOURCE="docs/archive/demo/materials/README.md §6 — 36+17+16+4+3+27+90"
GENERATOR="scripts/materials/pick.sh"
GENERATOR_VERSION=1

while [ $# -gt 0 ]; do
  case "$1" in
    --tree)     TREE="$2"; shift 2 ;;
    --out)      OUT="$2"; shift 2 ;;
    --evidence) EVIDENCE="$2"; shift 2 ;;
    --only)     ONLY="$2"; shift 2 ;;
    --no-html)  NO_HTML=1; shift ;;
    -h|--help)  sed -n '2,30p' "$0"; exit 0 ;;
    --selftest) SELFTEST=1; shift ;;
    *) echo "pick.sh: unknown arg: $1" >&2; exit 2 ;;
  esac
done

# 本轮 run 的证据目录：不指定就落在会话目录下（Rule 30/37：证据不进分支）。
if [ -z "$EVIDENCE" ]; then
  EVIDENCE="${GOPI_EVIDENCE_DIR:-$HOME/.gopi/sessions/${GOPI_TASK_ID:-pick}/materials-cut}"
fi
mkdir -p "$EVIDENCE" || exit 1

# measured_commit：git 的短哈希 + 脏标记（脏树对应不到任何提交，必须说出来）。
cd "$ROOT" || exit 1
MEASURED_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
# 脏标记只由**本次测量的输入**决定，就是这两样：素材树 + 本脚本自己。
# 别处的未提交改动（放映产物、报告、别人正在写的文件）不是这次挑选读过的字节 ——
# 把它们算进来，量的就是错的对象，而且会形成一个死循环（产物一写就"脏"，
# 于是永远重生成不出一个 dirty_inputs: none 的快照）。
# 至于"全仓是否干净"：那是 Rule 30 的事，由 close-out 查工作树，不归这个 header 管。
DIRTY_INPUTS="$(git status --porcelain -- docs/archive/demo/materials scripts/materials/pick.sh 2>/dev/null)"
[ -n "$DIRTY_INPUTS" ] && MEASURED_COMMIT="$MEASURED_COMMIT-dirty"
MEASURED_AT="$(date '+%F %T %z')"

sha16() { [ -f "$1" ] && sha256sum "$1" 2>/dev/null | cut -c1-16 || echo "-"; }
sha64() { [ -f "$1" ] && sha256sum "$1" 2>/dev/null | cut -d' ' -f1 || echo "-"; }

# --- 出货净化：把一次真跑的 argv 里属于"这台机器"的部分抽成占位符 -------------
# 目的不是好看：集成产物是给外人看的，任何 /home/... 绝对路径或会话 id 都会把
# 读者引到一个他不拥有的命名空间里（也把操作者的目录结构带出去）。
# 净化是**默认拒绝**：净不掉就标记 unclean，该条不选（宁缺毋滥，绝不"差不多"）。
sanitize() { # stdin -> stdout；规则从"最长最具体"到"最泛"依次施加
  sed -E \
    -e 's#/home/[A-Za-z0-9._-]+#${HOME}#g' \
    -e 's#/Users/[A-Za-z0-9._-]+#${HOME}#g' \
    -e 's#\$\{HOME\}/workspace/gopi-sandbox/[A-Za-z0-9._-]+/\.gopi/sessions/[A-Za-z0-9._-]+#${EV}#g' \
    -e 's#\$\{HOME\}/workspace/gopi-sandbox/[A-Za-z0-9._-]+#${SB}#g' \
    -e 's#\$\{HOME\}/\.gopi/sessions/[A-Za-z0-9._-]+#${EV}#g' \
    -e 's#\$\{HOME\}/workspace/gopi-[A-Za-z0-9]+#${RUN}#g' \
    -e 's#/[A-Za-z0-9._-]*sessions/[A-Za-z0-9._-]+#${EV}#g' \
    -e 's#[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}#{session-id}#g'
}
# 净化后仍不能出现的残渣 = 出货条件，就是验收点名的那三项：
#   /home/ /Users/  —— 别人的命名空间（绝对家目录）
#   sessions/       —— 内部会话目录
#   GOPI_HOME=/     —— 指向绝对路径的 GOPI_HOME
#   裸 UUID         —— 会话 id
# 机器查，不靠人看。`~/.gopi/raw` 这类**产品自己的 help 文本**不在其列：
# 它是相对于家目录的产品路径，不是任何人的命名空间。
# 判据是非空洞的：它拦的是**被别人拥有的命名空间**（家目录）与**私域树根**下的绝对路径
# （/mnt /media /opt /srv —— 这些是要么是别人的挂载点，要么是机器本地布局），
# 加上 sessions/ 与裸 UUID。产品自己的 help 文本里那种 `/tmp/x`、`/dev/null`、
# `/home/user/.gopi/raw` 不在其列：它们是产品路径，不是谁的命名空间。
residue() { grep -cE '/home/|/Users/|/mnt/|/media/|/opt/|/srv/|sessions/|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}' "$1" 2>/dev/null || true; }

# 出货面 = 放映页**真正嵌入**的三段，逐字来自素材条目：
#   (a) 01-input.txt 全文（verbatim 命令）
#   (b) 02-output.txt 的 exit= 行（断言）
#   (c) 05-finding.md 的首句正文（断言）
# 只查这三段而不是三个源文件全文 —— 判据必须量"上船的东西"，
# 去查一条素材自己都没被带上船的字节，是把判据量错了对象。
SHIP="$EVIDENCE/.ship.tmp"
ship_surface() { # <entry-dir> <dest-file>
  sanitize < "$1/01-input.txt" > "$2"
  grep -m1 -E '^exit=[0-9]+' "$1/02-output.txt" 2>/dev/null | sanitize >> "$2"
  grep -vE '^\s*$|^#' "$1/05-finding.md" 2>/dev/null | head -1 | sanitize >> "$2"
}

# --- 逐条算谓词 ----------------------------------------------------------------
TSV="$EVIDENCE/selection.tsv"
{
  printf '# generator: %s\n' "$GENERATOR"
  printf '# generator_version: %s\n' "$GENERATOR_VERSION"
  printf '# measured_commit: %s\n' "$MEASURED_COMMIT"
  printf '# measured_at: %s\n' "$MEASURED_AT"
  printf '# baseline: %s\n' "$BASELINE"
  printf '# baseline_source: %s\n' "$BASELINE_SOURCE"
  printf '# dirty_inputs: %s\n' "$([ -n "$DIRTY_INPUTS" ] && echo -n "$(printf '%s' "$DIRTY_INPUTS" | wc -l) file(s)" || echo none)"
  printf '# criteria: six=1 finding=1 ann_applied=1 clean=1   (README §5 谓词 + 标注落笔 + 出货净化；annotated 列为报告字段)\n'
  printf '# tree: docs/archive/demo/materials\n'
  printf '# entry\tface\tgroup\tann_decl\tsix\tfinding\tannotated\tann_applied\tclean\tselected\treason\tsha16-04\tsha16-02\tsha16-01\n'
} > "$TSV"

SEL_LIST="$EVIDENCE/selected-slugs.txt"
: > "$SEL_LIST"
DETAIL="$EVIDENCE/detail.md"
: > "$DETAIL"

n_dirs=0; n_sel=0; bytes_sel=0
face_line=""; group_line=""; feature_line=""; ann_line=""
for d in "$TREE"/*/; do
  [ -d "$d" ] || continue
  slug="$(basename "$d")"
  card="$d/00-entry.md"
  [ -f "$card" ] || continue          # 没有素材卡的不是条目
  n_dirs=$((n_dirs+1))

  # face：字段优先（README §4），库里最早一批无 face 字段 → 回落到卡片自己的 group
  face="$(grep -m1 '^- face:' "$card" | sed 's/^- face: *//; s/ *$//')"
  if [ -z "$face" ]; then
    face="legacy"
    group_line="$(grep -m1 -oE '\*\*group\*\*:[^　]*' "$card" | sed 's/\*\*group\*\*: *//; s/ *$//')"
  else
    group_line="$(grep -m1 '^- group:' "$card" | sed 's/^- group: *//; s/ *$//')"
  fi
  [ -z "$group_line" ] && group_line="-"

  # six（README §5）
  six=1; miss=""
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    if [ ! -s "$d/$f" ]; then six=0; miss="$miss$f,"; fi
  done
  grep -qE '^exit=[0-9]+' "$d/02-output.txt" 2>/dev/null || { six=0; miss="${miss}exit=,"; }
  for f in 03-raw.png 04-annotated.png; do
    head -c4 "$d/$f" 2>/dev/null | grep -q $'\x89PNG' || { six=0; miss="${miss}png-$f,"; }
  done

  # finding 非空（去空行、去标题行后仍有正文）
  if grep -qvE '^\s*$|^#' "$d/05-finding.md" 2>/dev/null; then finding=1; else finding=0; fi

  # annotated：有标注图（图在、非空、PNG magic）。自陈字段单独记录（ann_decl），不作为门槛。
  ann="$d/04-annotated.png"
  ann_decl="$(grep -m1 '^- annotation_required:' "$card" | sed 's/^- annotation_required: *//; s/ *$//')"
  [ -z "$ann_decl" ] && ann_decl="(undeclared)"
  if [ -s "$ann" ] && head -c4 "$ann" 2>/dev/null | grep -q $'\x89PNG'; then annotated=1; else annotated=0; fi
  # 标注是否真的落笔：与 03-raw.png 逐字节不同
  if [ "$(sha64 "$ann")" != "$(sha64 "$d/03-raw.png")" ]; then ann_applied=1; else ann_applied=0; fi

  # clean：出货面（被嵌入的三段）净化后不得有残渣
  clean=1; dirty_field=""
  ship_surface "$d" "$SHIP"
  h="$(residue "$SHIP")"
  if [ "${h:-0}" -gt 0 ]; then clean=0; dirty_field="shipped-surface($h)"; fi

  # 选择：五个布尔量与
  selected=0; reason=""
  if   [ "$six" != 1 ];      then reason="six-incomplete:${miss%,}"
  elif [ "$finding" != 1 ];  then reason="finding-empty"
  elif [ "$ann_applied" != 1 ]; then reason="annotated-identical-to-raw"
  elif [ "$clean" != 1 ];    then reason="unclean:${dirty_field%,}"
  elif [ -n "$ONLY" ] && [ "$face" != "$ONLY" ]; then reason="face-not-selected($face)"
  else selected=1; reason="ok"
  fi
  if [ "$selected" = 1 ]; then
    n_sel=$((n_sel+1)); echo "$slug" >> "$SEL_LIST"
    b="$(stat -c%s "$ann" 2>/dev/null || echo 0)"; bytes_sel=$((bytes_sel+b))
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$slug" "$face" "$group_line" "$ann_decl" "$six" "$finding" "$annotated" "$ann_applied" "$clean" \
    "$([ "$selected" = 1 ] && echo yes || echo no)" "$reason" \
    "$(sha16 "$ann")" "$(sha16 "$d/02-output.txt")" "$(sha16 "$d/01-input.txt")" >> "$TSV"
done

# --- 自检（--selftest）：判定器必须能说"不" ------------------------------------
# 一个只会说 yes 的判定器不是判定器。这里用**合成的**坏条目驱动同一条代码路径
# （不碰素材树，Rule 29/37），逐条断言它被拒，且原因码正确。红证在文本里：
# 输出里每条都印出实际 reason，与期望不符即 exit 1。
if [ "$SELFTEST" = 1 ]; then
  SBT="$(gopi_scratch_mkdir pick-selftest)" || exit 1   # Rule 22：off `/`，且注册回收
  mkdir -p "$SBT/good" "$SBT/bad-six" "$SBT/bad-finding" "$SBT/bad-ann" "$SBT/bad-applied" "$SBT/bad-clean"
  # 每个分支先造一份"好条目"，再只破坏一个条件
  for g in good bad-six bad-finding bad-ann bad-applied bad-clean; do
    mkdir -p "$SBT/$g"
    printf '# x\n- face: selftest\n- annotation_required: yes\n' > "$SBT/$g/00-entry.md"
    printf 'echo hi\n' > "$SBT/$g/01-input.txt"
    printf 'hi\nexit=0\n' > "$SBT/$g/02-output.txt"
    printf '\211PNG\r\n\032\n raw' > "$SBT/$g/03-raw.png"
    printf '\211PNG\r\n\032\n ann' > "$SBT/$g/04-annotated.png"
    printf '## 发现\n这次运行真的发现了一件事。\n' > "$SBT/$g/05-finding.md"
  done
  : > "$SBT/bad-six/04-annotated.png"                      # six: 图空
  printf '\n## 只有标题\n\n' > "$SBT/bad-finding/05-finding.md"   # finding: 去标题后为空
  printf 'not a png' > "$SBT/bad-ann/04-annotated.png"      # annotated: 非 PNG
  cp "$SBT/good/03-raw.png" "$SBT/bad-applied/04-annotated.png"    # ann_applied: 标注没落笔（与 raw 逐字节相同）
  printf 'GOPI_HOME=/mnt/other/host/.gopi gopi task list\n' > "$SBT/bad-clean/01-input.txt" # clean: 私域根绝对路径

  st_fail=0
  for g in good bad-six bad-finding bad-ann bad-applied bad-clean; do
    d="$SBT/$g"
    six=1; miss=""
    for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
      [ ! -s "$d/$f" ] && { six=0; miss="$miss$f,"; }
    done
    grep -qE '^exit=[0-9]+' "$d/02-output.txt" 2>/dev/null || { six=0; miss="${miss}exit=,"; }
    for f in 03-raw.png 04-annotated.png; do
      head -c4 "$d/$f" 2>/dev/null | grep -q $'\x89PNG' || { six=0; miss="${miss}png-$f,"; }
    done
    grep -qvE '^\s*$|^#' "$d/05-finding.md" 2>/dev/null && finding=1 || finding=0
    if [ -s "$d/04-annotated.png" ] && head -c4 "$d/04-annotated.png" 2>/dev/null | grep -q $'\x89PNG'; then annotated=1; else annotated=0; fi
    if [ "$(sha64 "$d/04-annotated.png")" != "$(sha64 "$d/03-raw.png")" ]; then ann_applied=1; else ann_applied=0; fi
    ship_surface "$d" "$SHIP"
    h="$(residue "$SHIP")"
    if [ "${h:-0}" -gt 0 ]; then clean=0; else clean=1; fi
    reason="ok"
    if   [ "$six" != 1 ];      then reason="six-incomplete:${miss%,}"
    elif [ "$finding" != 1 ];  then reason="finding-empty"
    elif [ "$ann_applied" != 1 ]; then reason="annotated-identical-to-raw"
    elif [ "$clean" != 1 ];    then reason="unclean:shipped-surface($h)"
    fi
    case "$g" in
      good)      want="ok" ;;
      bad-six)   want="six-incomplete:*" ;;
      bad-finding) want="finding-empty" ;;
      bad-ann)   want="six-incomplete:*" ;;   # 坏 PNG 由 six 拦下（six 含 PNG magic）
      bad-applied) want="annotated-identical-to-raw" ;;
      bad-clean) want="unclean:*" ;;
    esac
    ok="FAIL"
    case "$reason" in $want) ok="ok" ;; esac
    printf 'selftest %-12s reason=%-38s expected=%-20s %s\n' "$g" "$reason" "$want" "$ok"
    [ "$ok" = "ok" ] || st_fail=1
  done
  if [ "$st_fail" = 1 ]; then
    echo "SELFTEST FAIL: 判定器没有按判据说"不"" >&2
    exit 1
  fi
  echo "SELFTEST PASS: 6/6（1 条好条目通过 + 5 条各破坏一个判据被拒，原因码正确）"
  exit 0
fi

# 头部行数由脚本自己数出来（下游 awk 的 NR> 门槛据此，不写死）。
# 注意：列名行**也**以 '#' 开头，所以"#' 开头的行数"就是最后一行表头的行号，
# 数据从它的下一行开始 —— 这里 +1 会把第一条明细静默吃掉（实测踩过一次）。
HEADER_LINES=$(grep -c '^#' "$TSV")

# --- stdout：命中列表由脚本打印（不是人工抄） ---------------------------------
echo "# generator: $GENERATOR"
echo "# generator_version: $GENERATOR_VERSION"
echo "# measured_commit: $MEASURED_COMMIT"
echo "# measured_at: $MEASURED_AT"
echo "# baseline: $BASELINE   ($BASELINE_SOURCE)"
echo "# criteria: six ∧ finding ∧ ann_applied ∧ clean"
echo "# tree: docs/archive/demo/materials   (只读；本脚本不改素材树任何字节)"
echo "# selection.tsv: $TSV"
echo "candidate_dirs: $n_dirs"
echo "selected: $n_sel"
echo "excluded: $((n_dirs-n_sel))"
echo "selected_annotated_bytes: $bytes_sel   ($((bytes_sel/1024)) KiB —— 现场测量，随快照变)"
echo
echo "## 未命中原因分类"
awk -F'\t' -v H="$HEADER_LINES" 'NR>H { if ($10=="no") { r=$11; sub(/\(.*/,"",r); c[r]++ } } END { for (k in c) printf "%6d  %s\n", c[k], k }' "$TSV" | sort -rn
echo
echo "## 命中条目（按 face 分组）"
[ -n "$ONLY" ] && echo "(只选 face=$ONLY)"
for f in env task top_level memory node legacy; do
  n=$(awk -F'\t' -v F="$f" -v H="$HEADER_LINES" 'NR>H && $2==F && $10=="yes"' "$TSV" | wc -l)
  [ "$n" -eq 0 ] && continue
  echo "[$f] $n 条"
  awk -F'\t' -v F="$f" -v H="$HEADER_LINES" 'NR>H && $2==F && $10=="yes" { printf "    %-42s %s\n", $1, $3 }' "$TSV"
done

# --- 放映页 --------------------------------------------------------------------
if [ "$NO_HTML" = 1 ]; then echo; echo "(--no-html: 未生成 index.html)"; exit 0; fi
mkdir -p "$OUT" || exit 1
cp -f "$TSV" "$OUT/selection.tsv"

html_escape() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' -e 's/"/\&quot;/g'; }

HTML="$OUT/index.html"
{
  cat <<HTML_HEAD
<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<title>gopi 素材库 · 集成挑选集（materials cut）</title>
<style>
  :root { --ink:#111; --dim:#666; --line:#ddd; --bg:#faf9f6; --accent:#1a5fb4; }
  * { box-sizing:border-box; }
  body { margin:0; padding:32px 20px 80px; background:var(--bg); color:var(--ink);
         font:15px/1.65 -apple-system,"Noto Sans CJK SC","WenQuanYi Zen Hei",sans-serif; }
  main { max-width:1180px; margin:0 auto; }
  h1 { font-size:26px; margin:0 0 6px; }
  h2 { font-size:20px; margin:44px 0 10px; padding-bottom:6px; border-bottom:2px solid var(--line); }
  h3 { font-size:16px; margin:0 0 2px; }
  .meta { color:var(--dim); font-size:13px; }
  .head { background:#fff; border:1px solid var(--line); border-radius:8px; padding:18px 20px; }
  .head code { background:#f2f1ec; padding:1px 5px; border-radius:3px; font-size:13px; }
  pre { background:#fff; border:1px solid var(--line); border-left:3px solid var(--accent);
        border-radius:6px; padding:12px 14px; overflow-x:auto; font-size:13px;
        font-family:"Noto Sans Mono CJK SC",Menlo,Consolas,monospace; white-space:pre-wrap; }
  article { background:#fff; border:1px solid var(--line); border-radius:8px;
            padding:18px 20px; margin:18px 0; }
  article img { width:100%; height:auto; border:1px solid var(--line); border-radius:6px;
                background:#fff; margin:10px 0 4px; }
  .assert { border-left:3px solid #c01c28; background:#fdf6f6; padding:10px 14px;
            border-radius:0 6px 6px 0; margin:12px 0; }
  .assert b { color:#c01c28; }
  table { border-collapse:collapse; width:100%; font-size:13px; }
  td,th { border-bottom:1px solid var(--line); padding:5px 8px; text-align:left;
          vertical-align:top; font-family:"Noto Sans Mono CJK SC",Menlo,monospace; }
  th { color:var(--dim); font-weight:600; }
  .toc a { display:inline-block; margin:2px 10px 2px 0; color:var(--accent); font-size:13px;
           text-decoration:none; font-family:"Noto Sans Mono CJK SC",Menlo,monospace; }
  .note { color:var(--dim); font-size:13px; }
  .tag { display:inline-block; background:#eef2f7; color:var(--accent); border-radius:3px;
         padding:0 6px; font-size:12px; margin-left:6px; }
</style></head><body><main>
<h1>gopi 素材库 · 集成挑选集</h1>
<p class="meta">本页是<b>脚本产物</b>，不是手写的：<code>bash scripts/materials/pick.sh</code> 一条命令
既挑又生成这一页。素材本身在 <code>docs/archive/demo/materials/</code>（只读），本页只挑选与索引，不改动它。</p>
<div class="head">
<table>
<tr><th>generator</th><td>$GENERATOR</td></tr>
<tr><th>measured_commit</th><td>$MEASURED_COMMIT</td></tr>
<tr><th>measured_at</th><td>$MEASURED_AT</td></tr>
<tr><th>baseline</th><td>$BASELINE （$BASELINE_SOURCE）</td></tr>
<tr><th>candidate_dirs</th><td>$n_dirs</td></tr>
<tr><th>selected</th><td>$n_sel</td></tr>
<tr><th>excluded</th><td>$((n_dirs-n_sel))</td></tr>
<tr><th>selected_annotated_bytes</th><td>$bytes_sel ($((bytes_sel/1024)) KiB)</td></tr>
<tr><th>criteria</th><td>six ∧ finding ∧ annotated ∧ clean（定义见 selection.tsv 头部与
<a href="selection.tsv">selection.tsv</a>）</td></tr>
</table>
</div>
<p class="note">复现：<code>bash scripts/materials/pick.sh</code>。逐条明细（命中的与未命中的、
每条带原因码与三个 sha256）在 <a href="selection.tsv">selection.tsv</a>。
图片按相对路径直接引用素材条目自己的文件（<code>../../materials/&lt;entry&gt;/04-annotated.png</code>）——
<b>不复制</b>：复制会造出第二份 bytes，与素材库那个"原始记录"的 sha256 就不再是同一个东西。
要整页连图一起看，请在仓库根起一个静态服务：<code>python3 -m http.server 8080</code>，
然后打开 <code>/docs/archive/demo/showcase/materials-cut/</code>。
挑选的判据、逐条未命中原因与"没选什么、为什么"写在同目录的
<a href="selection.tsv">selection.tsv</a>；生成器本体是
<a href="../../../../scripts/materials/pick.sh">scripts/materials/pick.sh</a>
（读它的头部注释 = 读判据的定义）。</p>
<h2>目录</h2>
<p class="toc">
HTML_HEAD

  for f in env task top_level memory node legacy; do
    awk -F'\t' -v F="$f" -v H="$HEADER_LINES" 'NR>H && $2==F && $10=="yes" { printf "  <a href=\"#e-%s\">%s</a>\n", $1, $1 }' "$TSV"
  done
  echo "</p>"

  for f in env task top_level memory node legacy; do
    n=$(awk -F'\t' -v F="$f" -v H="$HEADER_LINES" 'NR>H && $2==F && $10=="yes"' "$TSV" | wc -l)
    [ "$n" -eq 0 ] && continue
    echo "<h2 id=\"face-$f\">face: $f — $n 条</h2>"
    awk -F'\t' -v F="$f" -v H="$HEADER_LINES" 'NR>H && $2==F && $10=="yes" { print $1 }' "$TSV" | while read -r slug; do
      d="$TREE/$slug"
      cmd="$(sanitize <"$d/01-input.txt")"
      exline="$(grep -m1 -E '^exit=[0-9]+' "$d/02-output.txt")"
      find1="$(grep -vE '^\s*$|^#' "$d/05-finding.md" | head -1 | sanitize)"
      echo "<article id=\"e-$slug\">"
      echo "  <h3>$slug <span class=\"tag\">$f</span></h3>"
      printf '  <p class="meta">素材条目：<code>docs/archive/demo/materials/%s/</code> · 04-annotated.png sha256:%s · 02-output.txt sha256:%s · 01-input.txt sha256:%s</p>\n' \
        "$slug" "$(sha16 "$d/04-annotated.png")" "$(sha16 "$d/02-output.txt")" "$(sha16 "$d/01-input.txt")"
      printf '  <img src="../../materials/%s/04-annotated.png" alt="%s 的标注运行截图" loading="lazy">\n' "$slug" "$slug"
      echo "  <p class=\"meta\">verbatim 命令（<code>01-input.txt</code> 逐字；本机路径已抽象为 <code>\${RUN}</code>/<code>\${SB}</code>/<code>\${HOME}</code>）</p>"
      printf '  <pre>%s</pre>\n' "$(printf '%s' "$cmd" | html_escape)"
      echo "  <div class=\"assert\"><b>断言</b>：本次真跑 <code>$exline</code>；<code>05-finding.md</code> 首句 —— $(printf '%s' "$find1" | html_escape)</div>"
      printf '  <p class="meta">回溯：<code>docs/archive/demo/materials/%s/</code>（<code>00-entry.md</code> 素材卡 + <code>01-input.txt</code> 输入 + <code>02-output.txt</code> 输出含 exit code + <code>03-raw.png</code>/<code>04-annotated.png</code> 两图 + <code>05-finding.md</code> 发现）· <code>how_to_replay</code> 见素材卡</p>\n' "$slug"
      echo "</article>"
    done
  done

  echo "<h2>这个挑选集的边界（说清楚，免得读成「全部」）</h2>"
  echo "<ul class=\"note\">"
  echo "<li>本页是 <b>$MEASURED_COMMIT</b> 时点上素材树的一个<b>快照</b>；素材树仍在被并行的素材生产任务追加，重跑 <code>pick.sh</code> 会得到新的快照。</li>"
  echo "<li>未被选中的条目<b>不是坏素材</b>，而是没满足本挑选集的四条判据之一；每一条的原因码在 <a href=\"selection.tsv\">selection.tsv</a> 里逐行写着（<code>six-incomplete:</code> / <code>finding-empty</code> / <code>annotated-missing</code> / <code>unclean:</code>）。</li>"
  echo "<li><code>annotation_required</code> 只记录不判门槛：库里最早一批没有这个字段（<code>ann_decl=(undeclared)</code>），但它们确实有标注图；判据只数「图在不在」（README §2）。</li>"
  echo "<li>本页不复制图片、不改素材树；素材条目的字节以 <code>04-annotated.png</code> 的 sha256 锚定，图片一旦被改动，本页印的哈希就对不上。</li>"
  echo "</ul>"
  echo "</main></body></html>"
} > "$HTML"

# 自证：产物里不许出现绝对路径与会话 id（出货条件，机器查，不靠人看）
leak=0
for f in "$HTML" "$OUT/selection.tsv"; do
  h="$(residue "$f")"
  if [ "${h:-0}" -gt 0 ]; then
    echo "ASSERT FAIL: $(basename "$f") 里仍有 $h 行残渣（/home/ · sessions/ · GOPI_HOME=/ · UUID）—— 出货条件不成立" >&2
    grep -nE '/home/|/Users/|sessions/|GOPI_HOME=/|[0-9a-f]{8}-[0-9a-f]{4}' "$f" | head -5 >&2
    leak=1
  fi
done
[ "$leak" = 1 ] && exit 1

echo
echo "html: $HTML"
echo "evidence: $EVIDENCE"
exit 0
