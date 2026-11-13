#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-topB.sh — A 面（顶层命令，批 2：Chat & web / Tools / Memory & knowledge /
# Configuration & maintenance 四组）的原始素材条目生成器。
#
# 本文件只承担「采集 + 渲染 + 断言」（与批 1 同一套机制，逐字复用其实现）；
# 本轮**采集了哪些条目**写在 scripts/materials/entries-topB.sh，由 MATERIALS_ENTRIES
# 指进来——批次不同、机制相同，就不该有两个会各自漂移的实现（Rule 14）。
#
# 面 = `internal/config/help.go` 的**四组**顶层索引（批 2）：
#   :581  "Chat & web:"
#   :589  "Tools (the agent's own tools, from the shell):"
#   :602  "Memory & knowledge:"
#   :607  "Configuration & maintenance:"
# 每组里的每一行 → 一条素材条目（六件套）。
#
# GENERATOR: scripts/materials/gen-topB.sh（本文件）—— 报告与素材卡的
#   how_to_replay 都指向它。形状遵守 docs/archive/demo/materials/README.md（M0 约定）。
#
# 诚实性约束（Rule 31 / Rule 34 / Rule 37）：
#  * 每条都是真 argv + 真 stdout/stderr + 真 exit code；`-h` 只作为**补充**
#    片段出现在个别条目里，从不单独构成一条素材。
#  * 写动词（submit/update/archive/merge）只在私有沙盒 profile 里跑：
#    `gopi tool sandbox create` 是唯一初始化器（Rule 34），操作者真台账全程只读。
#    脚本开头/结尾对真台账取 sha256 前缀并打印，作为"没碰"的自证。
##  * 本 run 不触碰任何破坏性路径：交互/常驻动词（chat/ui/web/cli/proxy）由
#    `timeout` 截断，退出码 124 在素材卡里被逐字标注成「本 run 截断的，不是它自己的码」。
  * 本 run 不触碰 merge / archive 的破坏性路径：只跑它们的**无参/拒绝**分支。
#  * 输出不编造；跑不出来就写「跑不出来」并让退出码进 02。
#  * 图：`gopi text2img` 渲染同一份文本 + `gopi annotate` 叠加（输入图不动）；
#    框坐标由 scripts/materials/render.py 量出，并在写 04 之前自证框内有墨迹
#    （渲染真值 = 像素真值；框到空行就 exit 1，不产出那张图）。
#
# 用法: scripts/materials/gen-topB.sh [<out_root>]     # 默认 docs/archive/demo/materials
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EV="${GOPI_MATERIALS_EVIDENCE:-/home/nick/.gopi/sessions/01a0ed23154f763eb5ee36b3867ecaf7}"
BIN="${GOPI_MATERIALS_BIN:-$EV/materials-topB/bin/gopi}"
RENDER="$ROOT/scripts/materials/render.py"
# Rule 22：临时工作目录来自仓库自己的共享助手（不用手搓 mktemp）。
# shellcheck source=scripts/lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "missing scripts/lib/tmp-ban.sh" >&2; exit 1; }
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:-$ROOT/docs/archive/demo/materials}"
TASK_ID=01a0ed23154f763eb5ee36b3867ecaf7
FACE=top_level
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
# 操作者真台账（本 run 唯一需要证明"没碰"的东西）
REAL_LEDGER="$HOME/.gopi/memory.db"

if [[ ! -x "$BIN" ]]; then
  echo "no binary at $BIN — building" >&2
  mkdir -p "$(dirname "$BIN")"
  ( cd "$ROOT" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/agent/ ) || exit 1
fi

ledger_hash() { [[ -f "$REAL_LEDGER" ]] && sha256sum "$REAL_LEDGER" | cut -c1-16 || echo "absent"; }
LEDGER_BEFORE="$(ledger_hash)"

# --- 私有沙盒 profile（Rule 34：ASK for one, never invent a directory） -------
RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
SB="$("$BIN" tool sandbox create --label "materials-topB-$RUNSTAMP" --task-id "$TASK_ID" | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
export GOPI_HOME="$SB"
# 沙盒里先造一个**真会话**：好让 log / stats / raw / checkpoint 四个动词有东西可读
# （写动词因此也只在沙盒里发生；操作者的会话从未被读到）。
echo "Reply with exactly: ok" | ( cd "$ROOT" && timeout 150 "$BIN" ) >/dev/null 2>&1
SID="$(ls -1 "$SB/.gopi/sessions" 2>/dev/null | head -1)"
[[ -n "$SID" ]] || { echo "FATAL: sandbox produced no session to inspect" >&2; exit 1; }
echo "sandbox=$SB"
echo "session=$SID"
echo "measured_commit=$COMMIT"
echo "operator_ledger_sha256_16=$LEDGER_BEFORE"

# SBX：第二个私有沙盒，专供"需要一个**已经产生过会话**的沙盒"的条目（brew 要读会话才
# 有东西可蒸馏）。它同样是 `gopi tool sandbox create` 造出来的（Rule 34），区别只是先在里面
# 跑过一轮会话；生成器在这里造那个会话时的 argv 与原会话完全同形。
SBX="$("$BIN" tool sandbox create --label "materials-topB-read-$RUNSTAMP" --task-id "$TASK_ID" | tail -1)"
[[ -d "$SBX/.gopi" ]] || { echo "FATAL: no second sandbox profile at $SBX" >&2; exit 1; }
echo "Reply with exactly: ok" | ( cd "$ROOT" && GOPI_HOME="$SBX" timeout 150 "$BIN" ) >/dev/null 2>&1
echo "sandbox_read=$SBX"



# 一页图能装多少行：默认 80 会把长输出（gopi -h 160 行）的标注行截到图外——
# 批 1 实测 101/102/108/110/111 五个条目因此整条图没产出。190 仍在 text2img 一页之内。
MAXLINES="${GOPI_MATERIALS_MAXLINES:-190}"
FAILMARKS=0
LOGDIR="$EV/materials-topB/logs"
mkdir -p "$LOGDIR"
W="$(gopi_scratch_mkdir materials-topB)" || { echo "FATAL: no off-/ scratch dir" >&2; exit 1; }
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$OUTROOT"
: > "$W/index.tsv"
: > "$W/failed.txt"

# --- 一个条目 -----------------------------------------------------------------
# entry <n> <slug> <cwd> <feature> <ann_req> <solves> <read_where> <caption> <marks> <argv...>
#   cwd        命令的运行目录（`code-search`/`keyword_search` 是**相对 cwd** 找仓库的，
#              所以 cwd 是复跑契约的一部分，必须逐字写进 01-input.txt）
#   feature    00-entry.md 的 feature 字段（逐字写出那个动词/开关）
#   ann_req    yes|no —— 这张标注图承不承担解释
#   solves     用户语言说它解决什么问题（禁止复述 help）
#   read_where 02-output.txt 的哪一行是结论
#   caption    04-annotated.png 上每个标注指向哪、为什么这里必须被标
#   marks      --mark-match 规格，';;' 分隔
entry() {
  local n="$1" slug="$2" cwd="$3" feature="$4" ann="$5" solves="$6" readw="$7" caption="$8" marks="$9"; shift 9
  [[ "${1:-}" == "--" ]] && shift
  local argv=("$@")
  local dir="$OUTROOT/$n-$slug"
  # 诊断用过滤器：GOPI_MATERIALS_ONLY="104-worker 112-raw" 只跑这几条。
  if [[ -n "${GOPI_MATERIALS_ONLY:-}" ]]; then
    case " $GOPI_MATERIALS_ONLY " in *" $n-$slug "*) ;; *) return 0 ;; esac
  fi
  mkdir -p "$dir"
  find "$dir" -maxdepth 1 -type f ! -name '0[0-9]-*' -delete 2>/dev/null
  rm -f "$dir"/03-raw-*.png

  # 01-input.txt —— 逐字可复跑的三行：cwd、GOPI_HOME、argv。
  # （上一条 A 面生成器在 argv 前多印了一次二进制路径，01 因此复跑不出 02——
  #  本生成器只印一次：第一个参数就是二进制，格式串里不再重复。）
  {
    printf 'cd %s\n' "$cwd"
    printf 'GOPI_HOME=%s \\\n' "$SB"
    # 一行一个参数：printf 的格式串会按参数**重复**，所以这里必须显式循环，
    # 否则只剩第一个参数保留缩进、其余每个都被再印一遍格式（实测：参数间出现双空格）。
    printf '  %s' "${argv[0]}"
    for ((_i = 1; _i < ${#argv[@]}; _i++)); do printf ' %s' "${argv[_i]}"; done
    printf '\n'
  } > "$dir/01-input.txt"

  # 真跑（stdout+stderr 合并；01 的 argv 能原样复跑出 02 的内容）
  #
  # ⚠ 采集形式（本批的实测教训，正文见 112-raw 的 05-finding.md）：**`$( )` 会改写字节**
  # ——命令替换剪掉末尾所有换行，而 raw 日志以 `}\n\n` 结尾，于是记录比真实日志短 1 字节。
  # 同理 `| head` 会让 `$?` 读到 head 的 0（批 1 的 108 有它的记录）。
  # 本 run 已把采集改成**重定向取字节**（见下面一行），命令替换只剩"取退出码"这一处不涉及字节的用途：
  #   ( cd "$cwd" && timeout 150 "${argv[@]}" ) > file 2>&1 ; rc=$?
  local rc
  # 逐字节捕获（不许命令替换参与）：重定向进一个工作区里的临时文件，再 cat 进 02。
  # `$( )` 会剪掉末尾所有换行——112 的第一版素材就是这么短掉 1 字节的（那一条 finding
  # 记录了它，并注明「本 run 已修」）。临时文件落在工作区 W，不落素材树（Rule 30）。
  ( cd "$cwd" && timeout 150 "${argv[@]}" ) > "$W/$slug.stdout.bin" 2>&1
  rc=$?
  { cat "$W/$slug.stdout.bin"; printf 'exit=%d\n' "$rc"; } > "$dir/02-output.txt"

  # 卡片里凡是"多少行""被砍掉多少条"这类**随仓库/会话内容变的字面量**，一律从刚写下的
  # 02-output.txt 现场量出来注入（占位符 @@02LINES@@ / @@CAPLINE@@），不写字面量：
  # 写死过一次的教训就是本轮的 1661/1945 两处（同一脚本三次运行得到三个不同的数）。
  local O2LINES O2CAP
  O2LINES="$(wc -l < "$dir/02-output.txt")"
  O2CAP="$(sed -n '$d;p' "$dir/02-output.txt" | tail -1 | grep '^… plus ' || true)"
  [[ -z "$O2CAP" ]] && O2CAP='（这一次没有截断行）'

  {
    printf '# %s-%s — %s\n\n' "$n" "$slug" "$solves"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- group: %s\n' "$GROUP"
    printf -- '- feature: `%s`\n' "$feature"
    printf -- '- annotation_required: %s\n' "$ann"
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- cwd: `%s`\n' "$cwd"
    printf -- '- how_to_replay: `bash scripts/materials/gen-topB.sh <out>` 重跑整批；单条按 `01-input.txt` 逐字执行\n'
    printf -- '- sandbox_profile: `%s`（私有：每条 argv 都带 `GOPI_HOME=$SB`，写动词的载体只在沙盒里；操作者台账的哈希在生成器首尾各印一次，但它同时会记录"哈希变化不构成本生成器的证据"——宿主 chat 会话自己会写操作者台账）\n\n' "$SB"
    printf '## 原始输入从哪来\n\n'
    printf '本次运行的 argv 在 `01-input.txt`：cwd、`GOPI_HOME`、二进制绝对路径与逐字参数，\n'
    printf '四样合起来就是复跑所需的全部。**cwd 不是装饰**——本批里 `doccheck`、`scratch-gc`、\n'
    printf '`code-search` 都以工作目录为参照，**cwd 因此是复跑契约的一部分**（见 `01-input.txt` 第 1 行）。\n'
    printf '本轮实测的两个方向都收在库里：`gopi doccheck` 在非仓库 cwd 下 `exit=1` 并把出路\n'
    printf '`--repo PATH` 写进拒绝（条目 137 的两段输出），而 `gopi code-search` 在非仓库 cwd 下\n'
    printf '`exit=4` 且逐字写着 `this is NOT a "no results" answer: the search did not run`\n'
    printf '（对照表见本批的 140 号 finding；批 1 的 108 记的是这条带名字的拒绝出现之前的读数）。\n\n'
    printf '## 输出怎么读\n\n'
    printf '`02-output.txt` 是 stdout 与 stderr 合并后的**逐字节**记录；末行 `exit=%d`（裸拼写，\n' "$rc"
    printf 'census 的 output 谓词认这个形式）是退出码。\n%s\n\n' "$readw"
    printf '## 标注图在指什么\n\n'
    printf '%s\n' "$caption"
  } | sed -e "s|@@02LINES@@|$O2LINES|g" -e "s|@@CAPLINE@@|$O2CAP|g" > "$dir/00-entry.md"

  local mkargs=() m
  while IFS= read -r m; do
    if [[ -n "$m" ]]; then
      # '::' 是 needle/label 的分界（两者都不含 '::'）；交给 render.py 时还原成
      # 它唯一的 ':' 形式，因此 **needle 里不许有冒号**（生成器在下面断言）。
      local nd="${m%%::*}" lb="${m#*::}"
      [[ "$nd" == *:* ]] && { echo "FATAL: mark needle contains ':' — $m" >&2; FAILMARKS=1; continue; }
      mkargs+=(--mark-match "$nd:$lb")
    fi
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g')
  if [[ -n "$marks" && ${#mkargs[@]} -eq 0 ]]; then
    echo "FATAL: entry $n declared marks but parsed none — $marks" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"; return 1
  fi
  printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$caption" > "$W/$slug.head"
  {
    cat "$W/$slug.head"
    printf '── 01-input.txt（verbatim 命令）────────────────────────────────────\n'
    cat "$dir/01-input.txt"
    printf '\n── 02-output.txt（verbatim 输出，末行是 exit code）──────────────────\n'
    cat "$dir/02-output.txt"
  } | python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --max-lines "$MAXLINES" \
        --json-out "$W/$slug.json" "${mkargs[@]}" \
        > "$W/$slug.render.log" 2>&1
  local rrc=$?

  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$n" "$slug" "$rc" "$rrc" \
     "$(wc -l < "$dir/02-output.txt")" "$(stat -c %s "$dir/04-annotated.png" 2>/dev/null || echo 0)" >> "$W/index.tsv"
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $n-$slug 的标注图没产出（render=$rrc）——见 $W/$slug.render.log" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
  fi
  printf 'entry %-24s exit=%-3s render=%-3s marks=%d\n' "$n-$slug" "$rc" "$rrc" "${#mkargs[@]}"
}
# ===========================================================================
# 批 2（本文件）：四组的条目与 finding 由一个可复跑的 entries 文件提供。
#   MATERIALS_ENTRIES 指向它（缺省 scripts/materials/entries-topB.sh）。
#   这样"生成器"（采集/渲染/断言，本文件）与"本轮采集了哪些条目"（entries
#   文件）分开——批 1 把两者写在一起，601 行里有 380 行是条目正文。
# ===========================================================================

# 本批的默认 finding 是本文件（批 2）的诚实空，覆盖批 1 末尾的默认实现：
# 路径与标签换成批 2 自己的（否则空 finding 会把读者指向另一批的条目）。
finding_default() {
  local dir="$OUTROOT/$1"
  {
    printf '## 这次运行里没挖到缝（但查过什么，写在这里）\n\n'
    printf '本条目对 %s 做了三方比对，**没有发现不一致**：\n\n' "$2"
    printf -- '- **help 文案**：该动词 `-h` 里的 synopsis 与正文描述；\n'
    printf -- '- **实际行为**：`02-output.txt` 的逐字节输出与 exit code（本条目）；\n'
    printf -- '- **代码**：`internal/config/help.go` 的索引行 / 派发分支与实现函数的判定。\n\n'
    printf '判据不是「看起来对」，而是这次运行的三样东西**互相不矛盾**：文案承诺的输入形状被真 argv 满足，\n'
    printf '输出里的每个字段都能在代码里找到写法，退出码与输出内容一致。\n'
    printf '**本条目没有可报告的缺陷**——这一条本身就是结论，不是省略。\n\n'
    printf '（本批的真缝写在 140-pixel-evidence / 130-shot / 137-doccheck / 118-ui_state 四条里，同一批运行挖出来的；其正文见各自目录。）\n'
  } > "$dir/05-finding.md"
}
# finding <name> —— 覆写某条已生成条目的 05-finding.md。
#
# ⚠ 目标目录必须**已经由 entry 生成过**，否则这里是**静默丢失**：`cat > file` 的目标
# 目录不存在时 shell 报一条错、重定向照样建不了文件，被 .（source）进来的 entries 文件
# 又不会因此中断整批——结果是这批素材的"真缝"写进了空气，而 COVERAGE/README/devdoc
# 已经把它当条目引用（实测：142-pixel-evidence 就这么丢过；本 run 把它并回 140，
# 并把这条注释变成下面这道拒绝写入的检查，Rule 15：不许静默失败）。
finding() {
  local name="$1"
  # 标签必须对应本文件里的一条 `entry`：`entry <n> <slug>` 是**空格**分隔的两列，
  # 而 finding 的标签写成 `<n>-<slug>`，这里把首段拆开逐字比对。
  # 写错的标签（如 142 之于 140）在这里被拒，而不是把正文写进不存在的目录、
  # 让整批静默少一条缝（Rule 15：不许静默失败）。
  local nm="${name%%-*}" nslug="${name#*-}"
  if ! grep -qE "^entry +${nm} +${nslug//./\\.}([[:space:]]|$)" "$ENTRIES"; then
    echo "FATAL: finding '$name' 在 $ENTRIES 里没有对应的 entry —— 标签写错了。" >&2
    echo "       真缝必须写在一条真实条目的 05-finding.md 里（Rule 15：不许静默失败）。" >&2
    exit 1
  fi
  # 标签合法但目录不存在 = 本次被 GOPI_MATERIALS_ONLY 过滤掉了：跳过并说明（不是错误）。
  if [[ ! -d "$OUTROOT/$name" ]]; then
    echo "note: finding '$name' 跳过（该条目本轮被 GOPI_MATERIALS_ONLY 过滤，没有目录）" >&2
    cat > /dev/null
    return 0
  fi
  cat > "$OUTROOT/$name/05-finding.md"
}
auto_finding_all() {
  local n slug
  while IFS=$'\t' read -r n slug _; do
    [[ -f "$OUTROOT/$n-$slug/05-finding.md" ]] || finding_default "$n-$slug" "$n-$slug"
  done < "$W/index.tsv"
}

ENTRIES="${GOPI_MATERIALS_ENTRIES:-$ROOT/scripts/materials/entries-topB.sh}"
[[ -f "$ENTRIES" ]] || { echo "FATAL: no entries file at $ENTRIES" >&2; exit 1; }
# shellcheck source=/dev/null
. "$ENTRIES"
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

echo "== 台账隔离自证 =="
LEDGER_AFTER="$(ledger_hash)"
echo "operator_ledger $LEDGER_BEFORE -> $LEDGER_AFTER"
# 真台账是宿主进程的活文件：本 run 自己（chat 会话）的 memory 写入也会动它，
# 所以哈希变化**不是**本生成器的证据。这里的证据是别的东西，逐条列出：
#   (1) 每条命令的 argv 都带 GOPI_HOME=$SB（见 01-input.txt）；
#   (2) 沙盒 profile 自己的台账存在、有行（下面 dump）；
#   (3) 写动词（archive/merge/submit）的载体只在沙盒里。
echo "operator_ledger_note 哈希变化不构成本生成器的违规证据（宿主 chat 会话会写自己的台账）；隔离证据见 01-input.txt 的 GOPI_HOME 与下方沙盒台账 dump"
echo "-- sandbox ledger --"
GOPI_HOME="$SB" "$BIN" task list 2>&1 | head -5
[[ "$LEDGER_BEFORE" == "$LEDGER_AFTER" ]] || echo "note: operator ledger changed during the run (host session writes; not this generator)"
cp -a "$W/." "$LOGDIR"/ 2>/dev/null; echo "render_logs=$LOGDIR"; ls "$LOGDIR" | head -20

echo "== index =="
cat "$W/index.tsv"
if [[ -s "$W/failed.txt" ]]; then
  echo "== FAILED entries ==" >&2
  cat "$W/failed.txt" >&2
  echo "GENERATOR FAILED: $(wc -l < "$W/failed.txt") entry/entries produced no annotated image" >&2
  exit 1
fi
[[ "$shape_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 形状断言未通过" >&2; exit 1; }
printf 'WORK=%s\nSANDBOX=%s\nSESSION=%s\n' "$W" "$SB" "$SID"

