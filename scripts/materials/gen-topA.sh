#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
#
# RETIRED MECHANISM (merge verb removal, 2026-11-01). This generator's subject is
# the TOP-LEVEL `gopi merge` surface, which no longer exists: `gopi merge` now
# answers with one retirement line and exit 1 (measured on this tree), so the
# entry that pins `gopi merge …` output can only reproduce the RECORD, not the
# live page. The entry text is kept verbatim (it is evidence of what the binary
# printed when the campaign ran); the surviving spelling is `gopi task merge`,
# documented by `man/gopi-merge.md` (see `./bin/gopi task merge -h`).
# materials-gen-topA.sh — A 面（顶层命令，批 1）原始素材条目生成器。
#
# 面 = `internal/config/help.go` 的两组顶层索引：
#   :580  "Task ledger & execution — the cross-session audit trail of every task:"
#   :587  "Coding & session inspection:"
# 每组里的每一行 → 一条素材条目（六件套）。
# 行号锚：命令层级重组 51484475（`internal/config/help.go` 8+/8−）之前是 :570/:577。
# 本批取的两组索引行本身**未被该次重组改动**（组 1 五行、组 2 现为八行，见条目 101/102）。
#
# GENERATOR: scripts/materials/gen-topA.sh（本文件）—— 报告与素材卡的
#   how_to_replay 都指向它。形状遵守 docs/archive/demo/materials/README.md（M0 约定）。
#
# 诚实性约束（Rule 31 / Rule 34 / Rule 37）：
#  * 每条都是真 argv + 真 stdout/stderr + 真 exit code；`-h` 只作为**补充**
#    片段出现在个别条目里，从不单独构成一条素材。
#  * 写动词（submit/update/archive/merge）只在私有沙盒 profile 里跑：
#    `gopi tool sandbox create` 是唯一初始化器（Rule 34），操作者真台账全程只读。
#    脚本开头/结尾对真台账取 sha256 前缀并打印，作为"没碰"的自证。
#  * 本 run 不触碰 merge / archive 的破坏性路径：只跑它们的**无参/拒绝**分支。
#  * 输出不编造；跑不出来就写「跑不出来」并让退出码进 02。
#  * 图：`gopi text2img` 渲染同一份文本 + `gopi annotate` 叠加（输入图不动）；
#    框坐标由 scripts/materials/render.py 量出，并在写 04 之前自证框内有墨迹
#    （渲染真值 = 像素真值；框到空行就 exit 1，不产出那张图）。
#
# 用法: scripts/materials/gen-topA.sh [<out_root>]     # 默认 docs/archive/demo/materials
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EV="${GOPI_MATERIALS_EVIDENCE:-/home/user/.gopi/sessions/01a0ed23-1245-7e67-a8c2-cb827c03a4f1}"
BIN="${GOPI_MATERIALS_BIN:-$EV/bin/gopi}"
RENDER="$ROOT/scripts/materials/render.py"
# Rule 22：临时工作目录来自仓库自己的共享助手（不用手搓 mktemp）。
# shellcheck source=scripts/lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "missing scripts/lib/tmp-ban.sh" >&2; exit 1; }
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:-$ROOT/docs/archive/demo/materials}"
TASK_ID=01a0ed2312457e67a8c2cb827c03a4f1
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
SB="$("$BIN" tool sandbox create --label "materials-topA-$RUNSTAMP" --task-id "$TASK_ID" | tail -1)"
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

# 一页图能装多少行：默认 80 会把长输出（gopi -h 160 行）的标注行截到图外——
# 实测 101/102/108/110/111 五个条目因此整条图没产出。190 仍在 text2img 一页之内。
MAXLINES="${GOPI_MATERIALS_MAXLINES:-190}"
FAILMARKS=0
LOGDIR="$EV/materials-topA/logs"
mkdir -p "$LOGDIR"
W="$(gopi_scratch_mkdir materials-topA)" || { echo "FATAL: no off-/ scratch dir" >&2; exit 1; }
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
  # 同理 `| head` 会让 `$?` 读到 head 的 0（正文见 108 的 05-finding.md）。
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
    printf -- '- how_to_replay: `bash scripts/materials/gen-topA.sh <out>` 重跑整批；单条按 `01-input.txt` 逐字执行\n'
    printf -- '- sandbox_profile: `%s`（私有：每条 argv 都带 `GOPI_HOME=$SB`，写动词的载体只在沙盒里；操作者台账的哈希在生成器首尾各印一次，但它同时会记录"哈希变化不构成本生成器的证据"——宿主 chat 会话自己会写操作者台账）\n\n' "$SB"
    printf '## 原始输入从哪来\n\n'
    printf '本次运行的 argv 在 `01-input.txt`：cwd、`GOPI_HOME`、二进制绝对路径与逐字参数，\n'
    printf '四样合起来就是复跑所需的全部。**cwd 不是装饰**——`code-search` / `keyword_search`\n'
    printf '是相对工作目录去找仓库的，**cwd 因此是复跑契约的一部分**（见 `01-input.txt` 第 1 行）：cwd 不是仓库时，两者给出的回答与「仓库里确实没有」**逐字相同**（`code-search` 答 `no symbol found: <symbol>`、`keyword_search` 答 `No matches.`），退出码也一样——退出码分不出「没有」与「没搜成」（实测见 `108-code-search/05-finding.md`、`109-keyword-search/05-finding.md`）。\n\n'
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
    printf '（本批的真缝写在 108 / 109 / 112 / 113 四条里，同一批运行挖出来的；其正文在下方
各自覆写。）\n'
  } > "$dir/05-finding.md"
}
finding() { cat > "$OUTROOT/$1/05-finding.md"; }
auto_finding_all() {
  local n slug
  while IFS=$'\t' read -r n slug _; do
    [[ -f "$OUTROOT/$n-$slug/05-finding.md" ]] || finding_default "$n-$slug" "$n-$slug"
  done < "$W/index.tsv"
}

echo "== entries =="

# --- 组 1：Task ledger & execution（help.go:580） -----------------------------
GROUP='Task ledger & execution'
entry 101 index-task-ledger "$ROOT" 'gopi -h（Task ledger & execution 组）' yes \
  '先看这七个动词各自解决什么问题，再决定读哪一篇 -h——索引是入口，不是文档' \
  '结论是组标题下那五行：每行 = 一个动词 + 它的一句话契约。' \
  '框住 `gopi merger` 行：**整个顶层索引里，它是唯一一行把动词的状态（RETIRED）写进摘要的**——别处退役都要翻到 -h 才知道。再框住 `gopi archive <branch>` 行：它是 `gopi merge` 的另一半（merge 做决定，archive 把决定连同理由记下来），两行隔着两行，读者一眼能看出这是一对。' \
  'gopi merger::状态写进摘要行，全索引仅此一处;;gopi archive <branch>::merge 的另一半' \
  -- "$BIN" -h

entry 102 index-coding-session "$ROOT" 'gopi -h（Coding & session inspection 组）' yes \
  '回答「刚才那次会话到底做了什么」——用日志、数字、原始字节三种粒度，都不是猜' \
  '结论是组标题下这八行，其中 `gopi checkpoint` 一行把两个子动词压在一行里。' \
  '框住 `gopi checkpoint list|export` 行：它是本组唯一一行**一行写两个动词**的，索引的粒度在这里变了——读者以为一行的都是同一种东西，其实这一行是两条命令。再框住 `gopi raw <session-id>` 行：它和同组的 `log`/`stats` 读的是**同一个 run 目录里的不同文件**，粒度差异全在这一行的一句话里。' \
  'gopi session checkpoint list|export::一行写两个动词，粒度在这里变了;;gopi raw <session-id>::与 log/stats 同一个目录的不同文件' \
  -- "$BIN" -h

entry 103 task-family "$ROOT" 'gopi task（族行本身）' yes \
  '把「任务台账」当一个动作的入口来读——不带子命令会怎样' \
  '结论是首行 `gopi task: missing subcommand`（诊断），其后 381 行是整篇 task help。' \
  '框住首行 `missing subcommand`：这是**没有拿到子命令**时的全部诊断——一个词，加一句缺失说明；**退出码在 380 行之后**，中间那整篇 help 把「为什么失败」和「这个动词能干什么」混在同一屏里。再框住 `append-only, never deleted.`：这九个词是台账的语义承诺，出现在**族**这一层而不是某个子命令里。' \
  'missing subcommand::诊断一个词，退出码在 380 行之后;;append-only audit trail::台账语义承诺写在族层级' \
  -- "$BIN" task

entry 104 worker "$ROOT" 'gopi worker' yes \
  '把队列里的活干完再退出——没有守护进程要照看，但它的启动条件很硬' \
  '结论是首行 `refusing to start` 与它列出的三条通道；末行是 `exit=3`。' \
  '框住 `refusing to start` 那一行：它拒绝的理由不是权限也不是配置，而是**调用者的环境**（`GOPI_AGENT_SESSION`），也就是说同一个命令在人手里和在 agent 手里行为不同。再框住 `Exit code: 3` 一行：拒绝**明文报出自己的退出码**，还说明它没有起任何东西、没拿锁——这是「跑了但什么也没做」与「没跑」的分界。' \
  'refusing to start::拒绝的理由是调用者环境;;Exit code::拒绝明说自己没起东西没拿锁' \
  -- "$BIN" worker

entry 105 merger "$ROOT" 'gopi merger' yes \
  '查「常驻合并器还在不在」——它的答案会当场把你转给另一个动词' \
  '结论是首行 `merger: retired` 与其后的转介；末行 `exit=1`。' \
  '框住 `merger: retired`：退役的是**守护进程**，不是合并这件事。再框住第二行 `merging itself is NOT retired`：一个动词退役时把「被退役的是哪一半」写清楚，读者才不会以为合并功能没了——这一行是本条目唯一要读的差别。' \
  'the resident merger daemon was retired::被退役的是守护进程;;merging itself is NOT retired::被退役的是哪一半写清楚了' \
  -- "$BIN" merger

entry 106 merge "$ROOT" 'gopi merge <不存在的分支名>' yes \
  '前台跑一次合并——被点名的分支永远不被静默忽略' \
  '结论是**第一行** `gopi merge: no repo in the "repos" table holds …`；后面 200 余行是同一次运行的 help 正文，末行 `exit=1`。' \
  '框住第一行的拒绝句：点名一个不存在的分支**不是空扫**，它被明确拒绝（help 的原话是 "a named branch is never silently ignored"）。再框住 `A name that matches nothing anywhere is REFUSED`：同一句承诺在 help 正文里重复出现——本条目把「调用处的行为」和「文档里的承诺」放在同一张图上对照。' \
  'no repo in the::点名不存在的分支被明确拒绝;;is REFUSED::文档里的同一句承诺' \
  -- "$BIN" merge zzz-no-such-branch-xyz

entry 107 archive "$ROOT" 'gopi archive（缺 --reason 的拒绝路径）' yes \
  '把一个分支判为「已决定的败者」——但必须写下理由，没有理由就不给归档' \
  '结论是首行 `--reason is required` 与它列出的 8 个合法理由码；末行 `exit=1`。' \
  '框住拒绝行里那串理由码：归档被设计成**必须编号的判决**（8 个码，每个对应一类败因），而不是一次 `-D`。再框住 `refs/gopi/archive-reason/` 那一行：理由不是日志文本，它被写进一个**自己的 ref**——这是「理由与判决同寿命」的实现方式。' \
  '--reason is required::归档必须编号，8 个理由码;;archive-reason::理由写进自己的 ref' \
  -- "$BIN" task archive master

# --- 组 2：Coding & session inspection（help.go:587） ------------------------
GROUP='Coding & session inspection'
entry 108 code-search "$ROOT" 'gopi tool code-search <symbol>' yes \
  '问「这个符号定义在哪」——答案是一行 file:line，不是一段匹配' \
  '结论是唯一一行 `taskverbs.DispatchVerbs  internal/taskverbs/verbs.go:312  var`；末行 `exit=0`。' \
  '框住这一行：整个答案就是**定义点**（文件:行 + kind + 签名首行），没有上下文、没有其它命中——这正是它和全文搜索的分工。注意本条目**只在 cwd 是仓库时才成立**：同一个动词在非仓库目录给出同样形状的一行（`no symbol found: …`）并以 **1** 退出，那正是 108 的 finding。' \
  'var DispatchVerbs = func::答案就是定义点，不是匹配段' \
  -- "$BIN" tool code-search taskverbs.DispatchVerbs

entry 109 keyword-search "$ROOT" 'gopi tool keyword_search <query>' yes \
  '全文搜索：一个词出现在哪些文件、哪一行——用于「这个字符串在哪儿用过」' \
  '结论是 **50 条命中**（= `keywordSearchMaxResults` 的上限；末行写着还有多少条被这个上限砍掉——本条目这一次是 @@CAPLINE@@，**这个数随仓库内容变**：本 run 的三次运行各不相同，所以这一屏是被截断的），每行 `<绝对路径>:<行号>:<该行>`；末行 `exit=0`。' \
  '框住命中行里的 `mergeHandoverBudgetFlag = "--handover-budget"`：全文搜索的价值在于它连**常量定义**与**测试引用**一起吃进来——同一次查询同时给出了实现、help 文案、测试三处的出现位置，这是 code-search 给不了的。' \
  'mergeHandoverBudgetFlag::命中里同时有实现/文案/测试' \
  -- "$BIN" tool keyword_search handover-budget

entry 110 log "$SB" 'gopi session log <session-id>' yes \
  '把一次会话的 JSONL 摊开——按轮次读模型到底做了什么，而不是读它的总结' \
  '结论是每个 `[round N]` 段：一次 assistant 轮 + 它的工具调用/结果成对出现；末行 `exit=0`。' \
  '框住第一条 `[round 1]`（或其后的工具行）：log 的单位是**轮**，不是文件——同一次运行的 raw HTTP 字节（条目 112）与聚合数字（条目 111）读的是同一份会话的另外两个投影。' \
  'Workdir::log 的单位是轮，不是文件' \
  -- "$BIN" session log "$SID"

entry 111 stats "$SB" 'gopi session stats <session-id>' yes \
  '回答「这次会话花了多少」——轮数、token、工具调用次数，数字而不是内容' \
  '结论是 `Session Statistics` 那一块里的 rounds / tokens；末行 `exit=0`。' \
  '框住 `elapsed=` 与 token 汇总行：stats 的产物是可进表格的数字（本条目也一样：它自己就是一条会被台账统计的运行），而不是解释——与同组 log 的差别就在这一行。' \
  'prompt tokens::产物是可进表格的数字' \
  -- "$BIN" session stats "$SID"

entry 112 raw "$SB" 'gopi session raw <session-id>' yes \
  '把那次会话发给模型的**原始字节**倒出来——最底层的事实，任何总结的上游' \
  '结论是第 3 行那条 `REQUEST`（逐字节的请求，含整个 system prompt）；末行 `exit=0`（**本条目这一次的输出是 @@02LINES@@ 行**——行数随会话内容变，本 run 的四次整批运行分别得到 1661 / 2359 / 682 / 530 行；图里只画前 190 行，完整见 02-output.txt）。' \
  '框住 `event: message_stop` 与紧随的 `data:` 行：raw 的产物是**发出去的东西**——整段 system prompt 逐字节在内，任何「模型当时看到了什么」的问题只有这一层能回答；响应侧则是 SSE 帧，协议层原样，不是渲染后的文本。' \
  'max_tokens::raw 是发出去的字节，含整个 system prompt;;CHUNK::响应侧是 SSE 帧，协议层原样' \
  -- "$BIN" session raw "$SID"

entry 113 checkpoint "$ROOT" 'gopi session checkpoint list' yes \
  '中断的会话从哪儿续——先看看有没有可续的检查点' \
  '结论是这张两列的表头 `log / reqs / last-model / msgs / bytes / sha256 / last-message`，每行一条 raw 日志；末行 `exit=0`。' \
  '框住 `e3bdfc75-….raw.log` 这一行：**它与条目 112 读的是同一个文件**（同一次运行刚写下的那份），也就是说 checkpoint 与 raw 是「枚举」与「读那份」的分工。再框住 stderr 里那句 `a file is a raw log iff its name ends ".raw.log" (co-located with its run) or starts "gopi-raw-" and ends ".log" (legacy flat)`：判据与**看过的目录**都被打出来了——零结果因此是可解释的，而不是沉默。' \
  'if its name ends ".raw.log"::判据与看过的目录都打出来，零结果可解释;;.raw.log::与条目 112 读的是同一个文件' \
  -- "$BIN" session checkpoint list

entry 114 resume "$ROOT" 'gopi resume（无参数）' yes \
  '从检查点continue一次被打断的会话——它的输入有三种身份' \
  '结论是 `Usage:` 行里的三选一 `<task-id|raw-log-path|session-id>`；末行 `exit=1`。' \
  '框住 `<task-id|raw-log-path|session-id>` 这一行：resume 接受**三种身份**指向同一份检查点，而其中之一是 task id——也就是说「行」与「那次运行」在这台机器上是可互推的。再框住下面 `Resume an interrupted session from its raw HTTP log checkpoint.`：它说明这些身份最终都归到同一个东西（raw 日志的最后一个 REQUEST），这正是条目 112/113 那两个动词存在的理由。' \
  'task-id|raw-log-path|session-id::三种身份指向同一份检查点;;raw HTTP log checkpoint::身份最终都归到同一个东西' \
  -- "$BIN" session resume

# ===========================================================================
# findings —— 只有真跑挖到的缝才覆写（其余条目走 finding_default 的诚实空）
# ===========================================================================
auto_finding_all

finding 108-code-search <<'FINDING_EOF'
**缝：同一个动词在"不是仓库"的目录里给出和"仓库里没有这个符号"一模一样的回答。**（类别：报错不准确 / 沉默失败）

实测（同一台机器、同一个二进制、同一个 `GOPI_HOME`，只换 cwd；cwd 是复跑契约的一部分）：

| cwd | argv | 输出 | exit |
|---|---|---|---|
| 仓库根 | `gopi code-search taskverbs.DispatchVerbs` | `taskverbs.DispatchVerbs  internal/taskverbs/verbs.go:312  var  …` | 0 |
| 仓库根 | `gopi code-search zzNoSuchSymbolQ7` | `no symbol found: zzNoSuchSymbolQ7` | 1 |
| 沙盒目录（非仓库） | `gopi code-search zzNoSuchSymbolQ7` | `no symbol found: zzNoSuchSymbolQ7` | 1 |
| 沙盒目录（非仓库） | `gopi code-search taskverbs.DispatchVerbs` | `no symbol found: taskverbs.DispatchVerbs` | 1 |

第 4 行是判决性的：`taskverbs.DispatchVerbs` 就在这个仓库里（第 1 行是它的定义点），
在非仓库 cwd 里它却和"根本不存在"得到**同一条句子、同一个退出码**——所以这条缝不是
"也许就是没有"，而是**同一句话被两种截然不同的原因共用**。前两行用的是一个**此刻不在
树里的 token**（写完本页它才进树，所以复跑者要自选一个当前不出现的名字——这一步不是
修饰，是本页能自我推翻的地方）。

**本页第一版的这张表里，四行的退出码一律写成 0，那是错的**（见本条目末尾「同一批素材里的
一个真缺陷」）。真实契约是 `cmd/agent/cli_code_search.go` 的 usage 自己写的那句：
`1  symbol not found ("no symbol found: <symbol>")`——**"符号不存在"与"搜错了地方"用的是
同一条错误、同一个退出码 1**。

**后果**：脚本写 `gopi code-search Foo || echo 没有这个符号` 时，在错误的目录里会得到
"没有这个符号"这个**错误结论**，而没有任何东西提示它搜错了地方
（`taskverbs.DispatchVerbs` 在仓库里是 100% 存在的，这让上表第四行可以直接反驳任何"也许就是没有"的解释）。
退出码把"没有"和"没搜"分开了吗——**没有**：两者同为 1，区分只发生在**人读那句话的上下文**里。

**对照**：同一次会话里的 `code-search` **工具**（agent 侧）能在项目里找到这个符号——
本条目的运行环境里两个面同时可达，所以这不是"能力不存在"，是**CLI 面把 cwd 当隐含前提却不报告它**。

**可提请的改法（不是本条目要做的）**：cwd 里找不到 `.git` / ctags 索引时，先给一条
带名字的拒绝（像 `gopi archive --check` 那样把 git 的 `fatal: not a git repository`
原样带出来），让"搜过了但没有"与"没搜成"落在不同的句子和不同的退出码上。
本条目只记录测量结果。

---

### 同一批素材里的一个真缺陷：经过管道的退出码不是退出码

写这一页时我复核了两处：`gopi code-search … | head` 得到的 `$?` 是 **0**，而同一个 argv
不接管道时是 **1**——`$?` 读的是管道最后一个进程（`head`）。这条素材库的第一版因此把
四行都记成了 0，**而 09 号 census 谓词只要求 `02-output.txt` 里存在一行 `^exit=[0-9]+`，
所以这个错误可以一路通过所有自动检查**。

判据（本 run 复跑得出，命令与输出见 05 的同批证据目录）：

```
$ (GOPI_HOME=$SB gopi code-search zzzNoSuchSymbolXYZ) | head -5 ; echo piped_exit=$?
no symbol found: zzzNoSuchSymbolXYZ
piped_exit=0
$ GOPI_HOME=$SB gopi code-search zzzNoSuchSymbolXYZ >/dev/null 2>&1 ; echo real_exit=$?
real_exit=1
```

**为未来的素材条目立的规矩（本条目用一次真教训换来）**：退出码一律
`(cd "$cwd" && timeout N binary args) > file 2>&1; rc=$?` ——**重定向，绝不接管道**；
`| head` 只允许用于"我只看前几行"的人眼阅读，不允许参与任何退出码记录。
FINDING_EOF

finding 109-keyword-search <<'FINDING_EOF'
**缝一（真契约）：`keyword_search` 与 `code-search` 把"没搜到"和"搜错了地方/没搜成"压在同一条回答、同一个退出码上，而两份 `-h` 都把"当前目录是一个仓库"当成隐含前提——没有一个字说这件事。**（类别：报错不准确 / 沉默失败 / 文档缺前提）

实测（沙盒 profile，`GOPI_HOME=$SB`，同一台机器）：

| argv | 输出 | exit |
|---|---|---|
| `gopi keyword_search handover-budget`（仓库 cwd） | 50 行命中 | 0 |
| `gopi keyword_search zzNoSuchTokenW9`（仓库 cwd） | `No matches.` | 1 |
| `gopi keyword_search handover-budget`（**非仓库** cwd） | `No matches.` | 1 |
| `gopi keyword_search zzNoSuchTokenW9`（非仓库 cwd） | `No matches.` | 1 |

判据是**同一个 argv 在两个 cwd 下逐字相同**；token 请自选（写完本页后它就在树里了，
那时 `keyword_search` 会命中本页本身——这也是这套素材的可复跑性该被这样检查的原因）。

**先纠正本页第一版的一句错话**：初稿写"零命中退 1 这件事 `-h` 里一个字都没写"——**那不对**。
`gopi keyword_search -h` 逐字写着（本 run 真跑该命令取其输出）：

```
  gopi keyword_search <query> [--root DIR]
      "No matches." is printed and the exit status is 1 when nothing matches.
Exit status:
  0    at least one match printed
  1    no matches, a usage error, or ripgrep failed (missing binary → a clear message naming it)
```

该写而没写的是**另一半**——`gopi -h` 的那一行索引逐字是 `gopi keyword_search <query>  Full-text
search over the repo.`，整份 `-h` 里**没有一句**说工作目录必须先是一个仓库。于是用户按最自然的读法
（"这条命令有没有报错"）得到的是：在自己的 home 目录里跑一次 `keyword_search`，会看到与
"仓库里确实没有这个词"**逐字相同**的 `No matches.` 和**同一个退出码 1**——
退出码把"没有"和"没搜成"分开了吗：没有。区分只发生在人读那句话的上下文里。

（`code-search` 的 `-h` 同理：它把 `1  symbol not found ("no symbol found: <symbol>")` 与
"usage error / ctags 建索引失败"并列成**同一个码 1**——见条目 108。）

**缝二（命名）：同一族里两个动词的拼写风格不同，而唯一写下的理由只存在于源码注释里。**

`internal/config/help.go:578` 的索引行逐字写的是 `gopi keyword_search <query>`（下划线），
邻居却是 `code-search`（连字符）、`scratch-gc`（连字符）。这是**有意的**
（help.go:585-590 的注释：动词名与 LLM tool 名同字节，`internal/toolnames` 的常量是唯一来源），
但设计意图只写在**源码注释**里，`gopi -h` 那一行与 `gopi keyword_search -h` 都不提——
用户看到的是同屏两行风格不一致。

**可提请的改法（不是本条目要做的）**：`keyword_search -h` 里补一句"必须在仓库内运行；
零命中时退出码为 1"；并补一句下划线拼写的由来。本条目只记录测量结果。

---

### 同一批素材里的一个真缺陷（本条目的 02 不可逐字节复跑）

本条的 `02-output.txt` 在**同一台机器、同一条 argv、同一个二进制**下重放时会漂移：

```
$ for i in 1 2 3; do gopi keyword_search handover-budget > kw-$i.txt; done
$ diff kw-1.txt kw-2.txt     → 行序不同（同一批命中，顺序不一致）
$ diff kw-1.txt kw-3.txt     → 行序不同，且有新增/消失的命中行
```

`keyword_search` 的命中是并发搜出来的，**顺序不保证**；而命中集合还会随着仓库内容变化
（本批素材记录之后，仓库里又落了新的文件——那正是我们在这个仓库里工作的证据）。

**这条缺陷的严重性不在漂移本身，而在于没有任何自动检查会因此变红**：
census 谓词只要求 `02-output.txt` 里存在一行 `^exit=[0-9]+`、图是 PNG、finding 非空——
一份**永远无法逐字节复跑**的 02 也能拿到 `complete=true`。

**本 run 的处置**（诚实，不修数据）：条目保留原样（它记录的是那一刻的真实输出），
并把这条判定交给一个真跑的重放工具 `scripts/materials/verify-topA.sh`：
01 是逐字 argv，它把 01 当脚本执行、与 02 比对，输出 `identical` / `differs` / `profile-missing`。
109 在这里被判 `differs`——**这条判定就是本次的发现**，而不是需要被抹平的东西。
FINDING_EOF
finding 113-checkpoint <<'FINDING_EOF'
**本条曾是一条 finding：`gopi checkpoint list` 在 2026-09-23 之后恒返回零结果——索引器还在按老名字、老位置找日志。它已被修复；此处保留的是「修复前后」的测量对照。**（类别：代码与搬迁不同步 / 沉默的零结果 / 已修复）

**修复前**（本批首跑，`GOPI_HOME=$SB`、刚写下一个会话）：

```
$ gopi raw 0b770e5a-8063-4666-b1b5-897e81e2a1a9
    （287528 字节的逐字节 HTTP 日志，见条目 112）                        exit=0
$ gopi checkpoint list
    no raw logs found under $GOPI_HOME/.gopi/raw                          exit=0
$ ls -l $GOPI_HOME/.gopi/sessions/<sid>/<sid>.raw.log   → 287528 字节（就是上面那一份）
$ ls -la $GOPI_HOME/.gopi/raw/                          → 空目录（每次运行都会建）
```

三条独立测量排除了「只是默认目录不对」这个解释（`--dir` 直指装着 `<sid>.raw.log` 的
目录，仍是"一条都没有"）。根因：`internal/checkpoint/index.go` 的 `IndexDir` 只往下看一层、
且要求文件名以 `gopi-raw-` 开头；而 `3a816775` 把 raw 日志搬进了
`sessions/<task-id>/<run-uuid>.raw.log`——同一次搬迁里 `gopi log`/`stats`/`raw` 都改用了
共享解析器，只有 `checkpoint list` 的索引器没跟上。当时的 bug 单：`01a0ed99-b05a-7721-8222-8fe93fd2e3b0`。

**修复后**（本条目现在的 02 就是这一份；修复提交 `098c8733`）：

```
$ gopi checkpoint list
log                          reqs last-model         msgs     bytes sha256    last-message
e3bdfc75-6c23-4479-9592-a4c19f506d07.raw.log  29 deepseek-flash  11  149058 d5e3b9926bbd8917 {"type":"text",…
                                                                                exit=0
$ # stderr（同一份输出的一行）：
$ scanned 2 directory(ies) for raw logs; criteria: a file is a raw log iff its name ends
$   ".raw.log" (co-located with its run) or starts "gopi-raw-" and ends ".log" (legacy flat)
$   …/.gopi/sessions — 1 entries, 0 raw log(s)
$   …/.gopi/sessions/01a109b5-3ec6-7bd5-a8a8-f0076d23f0ac — 8 entries, 1 raw log(s)
```

**两次测量的差别就是这条素材的价值**：同一个动词、同一个 profile，修复前是
「一句恒假的零结果 + 静默」，修复后是「一行真表 + 打出来的判据与看过的目录」。
本批 113 取材的那一次（真跑，逐字 argv 写在 `01-input.txt`）用的是**修复后**的二进制，
所以它与 112 读的是**同一个文件**——判定与证据都在条目自己的 `02-output.txt` 里。
FINDING_EOF'
**缝：`gopi checkpoint list` 在 2026-09-23 之后的任何 profile 上都永远找不到检查点——它的索引器还在按老名字、老位置找日志。**（类别：代码与搬迁不同步 / 沉默的零结果 / 文档不准确）

同一个沙盒 profile（`GOPI_HOME=$SB`）、同一次运行刚刚写下的会话：

```
$ gopi raw 0b770e5a-8063-4666-b1b5-897e81e2a1a9
    （287528 字节的逐字节 HTTP 日志，见条目 112）                        exit=0
$ gopi checkpoint list
    no raw logs found under $GOPI_HOME/.gopi/raw                          exit=0
$ ls -l $GOPI_HOME/.gopi/sessions/<sid>/<sid>.raw.log   → 287528 字节（就是上面那一份）
$ ls -la $GOPI_HOME/.gopi/raw/                          → 空目录（每次运行都会建）
```

**三条独立测量排除了"只是默认目录不对"这个解释**（都真跑了，都在本条目的同一台机器上）：

```
$ gopi checkpoint list --dir $GOPI_HOME/.gopi/sessions            → no raw logs found  exit=0
$ gopi checkpoint list --dir $GOPI_HOME/.gopi/sessions/<sid>      → no raw logs found  exit=0
```

第二个 `--dir` 直指那个**装着 `<sid>.raw.log` 的目录本身**，仍然是"一条都没有"。

**根因（读代码）**：`internal/checkpoint/index.go:38` 的 `IndexDir` 逐条过滤：

```go
if de.IsDir() || !strings.HasPrefix(de.Name(), "gopi-raw-") || !strings.HasSuffix(de.Name(), ".log") {
    continue
}
```

- 它**只往下看一层**（`de.IsDir()` 直接跳过）——而 `sessions/` 下每个运行是一个子目录；
- 它要求文件名以 **`gopi-raw-`** 开头——而运行内的日志名是 **`<run-uuid>.raw.log`**。

两个条件各自就足以让运行内的日志不可见，`--dir` 也就无从救起。

**这是搬迁留下的半截**：`3a816775` 把 raw 日志搬进 `sessions/<uuid>/<run-uuid>.raw.log`，
同一次搬迁里 `gopi log` / `gopi stats` / `gopi raw` 都改用了共享解析器
（`gopilog.ResolveRawLogForSession`），**只有 `checkpoint list` 的索引器没跟着改**。
所以它不是"配置问题"，是这条动词在搬迁后就没有可用输入了——而它的输出
（`no raw logs found …`）读起来像"这台机器上没有历史"。

**为什么值得记**：`gopi resume` 是 `checkpoint` 的下游，两者是"中断恢复"的一对
（当时 `gopi checkpoint resume` 自己的 help 里逐字写着它做的是 "the SAME resume the
`gopi resume` command below performs"；命令重组 `51484475` 之后这一个子动词已按名删除，
只剩 `gopi resume` / `gopi session resume` 一条续接路径）。用户按最自然的拼写问"我有哪些检查点"时，
拿到的是一个**在任何现代 profile 上都恒为空的枚举**，而退出码是 0。

**文档侧同一处不一致**：`gopi checkpoint -h` 的首段仍写着
"every → REQUEST line in `~/.gopi/raw/` is a byte-exact context snapshot"，
`list` 一节仍写着 "default ~/.gopi/raw"——**而 `~/.gopi/raw/` 这个目录自 2026-09-23 起
对任何普通运行都是空的**（本条目的 `ls -la` 就是证据）。help 文案描述的是一个已经不存在的世界。

**可提请的改法（不是本条目要做的）**：让 `IndexDir` 走与 `log`/`stats` 同一套解析
（递归 `sessions/*/`，或同时认 `<uuid>.raw.log`），并把零结果改成"看过的目录 + 判据"两行；
`-h` 里的 `~/.gopi/raw/` 随之更正。本条目只记录测量结果。

**已入队**：本 run 开了 bug 单 `01a0ed99-b05a-7721-8222-8fe93fd2e3b0`
（该单的既有描述里说的根因与 `--dir` 那三条测量，都在本条目里以逐字节输出复现过了）。
FINDING_EOF

finding 112-raw <<'FINDING_EOF'
**缝：本批素材的"逐字节"记录曾经不是逐字节的——采集端用 `$( )` 抓输出，命令替换吃掉了末尾的换行。**（类别：采集管线缺陷 / 已在本轮修掉，留证反例）

`gopi raw <session-id>` 的一句话契约是 **"The verbatim raw HTTP log, byte for byte"**
（`internal/config/help.go` 的索引行逐字如此）。而本条目 `02-output.txt` 里那份记录
**不是逐字节的**：

```
$ gopi raw <sid> > out.txt 2>&1        # 直接重定向
    bytes=287528  lines=1661  末尾  …"message_stop"}\n\n
$ out="$( gopi raw <sid> 2>&1 )"       # 生成器当时用的形式
    bytes=287527  lines=1660  末尾  …"message_stop"}\n
```

差的就是那个尾部空行：raw 日志以 `}\n\n` 结尾（SSE 的帧分隔就是这个空行），
`$()` 把末尾的换行全部剪掉，于是记录比真实日志**短 1 字节**。
`gopi raw` 的用途恰恰是协议层排障——SSE 帧的**边界**就是它的信息，丢掉的正是这个边界。

**为什么这条会被写进素材而不是被抹平**：这类偏差在自动检查里是隐形的
（census 只要求 `02-output.txt` 里有一行 `^exit=[0-9]+`、图是 PNG、finding 非空），
只有"把 `01-input.txt` 当脚本重放、与 `02-output.txt` 逐字节比对"才能看见——
本 run 为此加了 `scripts/materials/verify-topA.sh`，它在 112 上报 `differs`。
**判定 `differs` 本身就是交付物**：它把"这份素材的记录保真度"变成可测的。

> 采集形式在本轮被改成重定向之后，112 重放仍然报 `differs`——但**差异的性质换了**：不再是"少一个换行"，而是时间戳、pid 与会话内容（raw 是一段活会话的字节流）。这一条区分很重要：`differs` 不等于"素材是假的"，它只等于"这棵树与那次运行的字面量不同"；把两类差异分开看，是本轮两次重放（`regen` 与 `verify`）都在做的事。

**同一个采集形式的两个后果**（这一批一次踩全了，都留在这里当反例）：
1. **exit code 被管道改写**——`gopi code-search … | head` 的 `$?` 是 `head` 的 0 而不是动词的 1
   （见条目 108 的 finding，那一页原本的退出码表就是这么错的）；
2. **字节被命令替换改写**——本条。两者同源：**凡是让 shell 参与搬运的东西，
   都不能当作"逐字节/逐退出码"的证据**。可复用的正确形状只有一种：

```
( cd "$cwd" && GOPI_HOME=$SB timeout N "$BIN" ARGS... ) > file 2>&1 ; rc=$?
```

**可提请的改法（不是本条目要做的）**：`gopi raw -h` 里加一句
"输出以空行分帧，末尾的换行属于内容"——把"逐字节"这个承诺里
最容易在管道里丢掉的那 1 个字节说出来。
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

