#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-flagF.sh — F 面（**顶层 flag**）原始素材条目生成器。
#
# 面 = `gopi -h` 的 `Options (flag > env var > settings.json > default):` 段
#      ——内部 `internal/config/help.go` 的 runOptions 列表。
# 每一条 = 一条素材（六件套，形状见 docs/archive/demo/materials/README.md）。
#
# 为什么本生成器存在（本面的核心 finding）：
#   旧口径 `gopi -h | sed -n '/Flags:/,$p' | grep -cE '^\s+--'` 实测得 **0**，
#   它数的是**另一个 CLI**（clap/cobra 风格的 `Flags:` 段）的版式。正确口径见
#   docs/archive/demo/materials/flag-01-*.md（`Options` 段 + 两空格缩进），本文件把它
#   作为**生成器**（Rule 38）逐字执行。
#
# 诚实性约束（Rule 31 / Rule 34 / Rule 38 / Rule 27）：
#  * 每条都是真 argv + 真 stdout/stderr + 真 exit code；`-h` 从不单独构成一条素材。
#  * 一律在私有沙盒 profile 里跑（`gopi tool sandbox create`，Rule 34）；写动词的载体
#    只在沙盒；操作者台账从不被读写。
#  * 输出的采集用**重定向**，绝不接管道（管道会把 $? 变成 head 的退出码——
#    A 面同批素材踩过，见 docs/archive/demo/materials/108-code-search/05-finding.md）。
#  * 图：`gopi text2img` 渲染 + `gopi annotate` 叠加（输入图不动），框坐标由
#    scripts/materials/render.py 量出，框内没有墨迹就 exit 1、不产出这张图。
#
# 用法: scripts/materials/gen-flagF.sh [<out_root>]    # 默认 docs/archive/demo/materials
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EV="${GOPI_MATERIALS_EVIDENCE:-/home/nick/.gopi/sessions/01a0ed23-1945-77f7-bad0-962ec576bcb9}"
BIN="${GOPI_MATERIALS_BIN:-$EV/bin/gopi}"
RENDER="$ROOT/scripts/materials/render.py"
# shellcheck source=scripts/lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "missing scripts/lib/tmp-ban.sh" >&2; exit 1; }
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:-$ROOT/docs/archive/demo/materials}"
TASK_ID=01a0ed23194577f7bad0962ec576bcb9
FACE=flags
GROUP="Options (顶层 run flag)"
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"

if [[ ! -x "$BIN" ]]; then
  echo "no binary at $BIN — building" >&2
  mkdir -p "$(dirname "$BIN")"
  ( cd "$ROOT" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/agent/ ) || exit 1
fi

RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
SB="$("$BIN" tool sandbox create --label "materials-flagF-$RUNSTAMP" --task-id "$TASK_ID" | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
export GOPI_HOME="$SB"
echo "sandbox=$SB"
echo "measured_commit=$COMMIT"

MAXLINES="${GOPI_MATERIALS_MAXLINES:-150}"
FAILMARKS=0
W="$(gopi_scratch_mkdir materials-flagF)" || { echo "FATAL: no off-/ scratch dir" >&2; exit 1; }
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$OUTROOT"
: > "$W/index.tsv"
: > "$W/failed.txt"

# 沙盒里先落一个会话与一行台账，好让「成功/失败」「非空/空」两侧都有东西可指。
( cd "$SB" && echo "Reply with exactly: ok" | timeout 90 "$BIN" --timeout 60 >/dev/null 2>&1 )
echo "seed_session=$(ls -1 "$SB/.gopi/sessions" | head -1)"

# --- 一个条目 -----------------------------------------------------------------
# entry <n> <slug> <solves> <read_where> <caption> <marks> -- <cmd>;;<cmd>...
#   cmd   完整 shell 行（在 $SB 里执行；二进制用绝对路径，GOPI_HOME 已在环境里）
#   marks ';;' 分隔的 `<needle>::<label>`；needle 里**不许有冒号**（解析用 ':' 分隔）
entry() {
  local n="$1" slug="$2" solves="$3" readw="$4" caption="$5" marks="$6"; shift 6
  [[ "${1:-}" == "--" ]] && shift
  local cmds="${1//;;/$'\n'}"
  local dir="$OUTROOT/$n-$slug"
  if [[ -n "${GOPI_MATERIALS_ONLY:-}" ]]; then
    case " $GOPI_MATERIALS_ONLY " in *" $n-$slug "*) ;; *) return 0 ;; esac
  fi
  mkdir -p "$dir"
  find "$dir" -maxdepth 1 -type f ! -name '0[0-9]-*' -delete 2>/dev/null
  rm -f "$dir"/03-raw-*.png

  # 01-input.txt：逐字可复跑（cwd / GOPI_HOME / 每行命令）
  {
    printf 'cd %s\n' "$SB"
    printf 'GOPI_HOME=%s\n' "$SB"
    printf '%s\n' "$cmds" | while IFS= read -r c; do
      [[ -z "$c" ]] && continue
      printf '$ %s\n' "$c"
    done
  } > "$dir/01-input.txt"

  # 05-finding.md 每个条目都要有：没挖到缝的走自动默认（诚实空，不是省略）。
  if [[ ! -s "$dir/05-finding.md" ]]; then
    {
      printf '## 这次运行里没挖到缝（但查过什么，写在这里）\n\n'
      printf '本条目对 `%s` 做了三方比对，**没有发现不一致**：\n\n' "$slug"
      printf -- '- **`gopi -h` 的 Options 段**：这个 flag 的 synopsis、取值形状与 `(GOPI_…)` 括注；\n'
      printf -- '- **实际行为**：`02-output.txt` 的逐字节输出与 `exit=<n>`（本条目真跑）；\n'
      printf -- '- **代码**：`internal/config/` 里读这个值的通道，与它印出的归属（`from --flag` / `from GOPI_…` / `from built-in default`）。\n\n'
      printf '判据不是「看起来对」，而是三样东西**互相不矛盾**：文案承诺的输入形状被真 argv 满足，\n'
      printf '输出里的归属与代码的判定一致，退出码与输出内容一致。\n**本条目没有可报告的缺陷**——这一条本身是结论，不是省略。\n'
    } > "$dir/05-finding.md"
  fi

  # 02-output.txt：逐条命令的 `$ cmd` + 合并输出 + 裸 `exit=<n>`（重定向取码，绝不接管道）
  # 注意：输出若不以换行结尾（如正文以 `ok` 收尾），先补一个换行再写 `exit=`——
  # 否则末行变成 `okexit=0`，census 的 `^exit=[0-9]+$` 谓词就落空（实测踩过，见 210）。
  : > "$dir/02-output.txt"
  local rc last=0 i=0
  while IFS= read -r c; do
    [[ -z "$c" ]] && continue
    i=$((i + 1))
    printf '$ %s\n\n' "$c" >> "$dir/02-output.txt"
    ( cd "$SB" && eval "timeout 150 $c" ) > "$W/$slug.$i.bin" 2>&1
    rc=$?
    cat "$W/$slug.$i.bin" >> "$dir/02-output.txt"
    [[ -s "$W/$slug.$i.bin" ]] && [[ -n "$(tail -c1 "$W/$slug.$i.bin")" ]] && printf '\n' >> "$dir/02-output.txt"
    printf 'exit=%d\n\n' "$rc" >> "$dir/02-output.txt"
    last=$rc
  done < <(printf '%s\n' "$cmds")

  {
    printf '# %s-%s — %s\n\n' "$n" "$slug" "$solves"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- group: %s\n' "$GROUP"
    printf -- '- feature: `%s`\n' "$slug"
    printf -- '- annotation_required: yes\n'
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- cwd: `%s`\n' "$SB"
    printf -- '- how_to_replay: `01-input.txt` 逐字执行（首行 cwd、次行 `GOPI_HOME`，其余每行一条 argv）；用 `gopi tool sandbox create --label <名字> --task-id %s` 自建一个**私有** profile 作为 `GOPI_HOME`（Rule 34），然后逐行跑\n' "$TASK_ID"
    printf -- '- sandbox_profile: `%s`（私有；操作者台账从未被读写——每条 argv 只经 `GOPI_HOME` 指向沙盒）\n\n' "$SB"
    printf '## 原始输入从哪来\n\n'
    printf '`01-input.txt` 是逐字 argv（cwd / `GOPI_HOME` / 每行一条命令），复跑只需这三样。\n\n'
    printf '## 输出怎么读\n\n'
    printf '`02-output.txt` 是 stdout 与 stderr 合并后的记录：每个命令块以 `$ <argv>` 开头，\n'
    printf '以裸行 `exit=<n>` 收尾（census 的 output 谓词认这个形式）。%s\n\n' "$readw"
    printf '## 标注图在指什么\n\n%s\n' "$caption"
  } > "$dir/00-entry.md"

  local mkargs=() m
  while IFS= read -r m; do
    if [[ -n "$m" ]]; then
      local nd="${m%%::*}" lb="${m#*::}"
      [[ "$nd" == *:* ]] && { echo "FATAL: mark needle contains ':' — $m" >&2; FAILMARKS=1; continue; }
      mkargs+=(--mark-match "$nd:$lb")
    fi
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g')
  if [[ -n "$marks" && ${#mkargs[@]} -eq 0 ]]; then
    echo "FATAL: entry $n declared marks but parsed none — $marks" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"; return 1
  fi

  # text2img 一页 200 行（render.py 的 LINES_PER_PAGE）：长输出 + 解说会翻页，
  # 而翻页意味着图尾的截断说明与部分标注行落到第 2 张图上 ⇒ 「恰好六件」被破坏
  # （实测 228：261 行折行后翻页，03-raw-1.png/03-raw-2.png 一并落下）。
  # 机制：一页装不下就**把页容量调大一档重试**（最多两档），并在 render 日志里留痕。
  # needle 预验（Rule 15：不静默）：render.py 的 --mark-match 找不到行时只打一行 note、
  # rc 仍为 0，条目会静默少一张图。这里在渲染前用同一份 wrap() 逐条验，验不过就 FATAL。
  local nd_check
  while IFS= read -r nd_check; do
    [[ -z "$nd_check" ]] && continue
    if ! python3 "$ROOT/scripts/materials/materials-needle-check.py" "$dir" "$nd_check"; then
      echo "FATAL: needle 在折行后的证据段里找不到 — $n-$slug :: $nd_check" >&2
      printf '%s\n' "$n-$slug" >> "$W/failed.txt"; FAILMARKS=1; return 1
    fi
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g' | sed 's/::.*//')

  local ml="$MAXLINES" rrc=1 try
  for try in 1 2 3; do
    render_one() {
      python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" --max-lines "$ml" \
        --json-out "$W/$slug.json" "${mkargs[@]}"
    }
    { printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$caption"
      printf '── 01-input.txt（verbatim 命令）────────────────────────────────────\n'
      cat "$dir/01-input.txt"
      printf '\n── 02-output.txt（verbatim 输出，每个命令块末行是 exit code）─────────\n'
      cat "$dir/02-output.txt"
    } | render_one > "$W/$slug.render.log" 2>&1
    rrc=$?
    [[ $rrc -eq 0 ]] && break
    find "$dir" -maxdepth 1 -name '03-raw-[0-9]*.png' -delete 2>/dev/null
    find "$dir" -maxdepth 1 -name '04-annotated-[0-9]*.png' -delete 2>/dev/null
    # 只在「页不够」时放页：needle 没命中、框内无墨迹这类失败同样靠放大页重试一次，
    # 但重试上限写死（3 档），绝不无限放大。
    printf 'RETRY %d: render rc=%d with --max-lines=%s\n' "$try" "$rrc" "$ml" >> "$W/$slug.render.log"
    ml=$((ml * 2))
  done
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$n" "$slug" "$last" "$rrc" \
     "$(wc -l < "$dir/02-output.txt")" "$(stat -c %s "$dir/04-annotated.png" 2>/dev/null || echo 0)" >> "$W/index.tsv"
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $n-$slug 标注图没产出（render=$rrc）——见 $W/$slug.render.log" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
  fi
  printf 'entry %-22s last_exit=%-3s render=%-3s marks=%d\n' "$n-$slug" "$last" "$rrc" "${#mkargs[@]}"
}

finding() { cat > "$OUTROOT/$1/05-finding.md"; }

echo "== entries =="
B="$BIN"

# 200 —— 本面的起点：把「顶层 flag 面」的分母量错这件事本身，做成一条真跑素材
entry 200 flag-census-probe \
  '量「顶层 flag 面有多少项」——旧口径给出一个假分母 0，正确口径给出 23' \
  '结论是三行数字：旧口径 `0`（退出码 1）、新口径 `23`、去重后 `23`；逐项名单在同一份输出里。' \
  '框住第一行的 `0`：**它是错的，而且看不出错**——同一个 `grep -c` 在没匹配到任何行时也打印 0 并以 1 退出。再框住第二行的 `23`：正确口径只是把段名从 `Flags:` 换成 `Options`、把缩进写成该段自己的两个空格——分母因此从一个假数字变成可逐项核对的 23。' \
  'exit=1::旧口径的 0 与"没匹配到"共用同一个数;;^  --[a-z]::正确口径只差段名与缩进' \
  -- "\"$B\" -h 2>&1 | sed -n '/Flags:/,\$p' | grep -cE '^\\s+--';;\"$B\" -h 2>&1 | sed -n '/^Options /,\$p' | grep -cE '^  --[a-z]';;\"$B\" -h 2>&1 | sed -n '/^Options /,\$p' | grep -oE '^  --[a-z][a-z-]*' | sort -u | tr '\\n' ' '"

# 201 --tmpdir 不可用：拒绝，并把路径/原因/通道写在错误里（Rule 22 的当面证据）
entry 201 flag-tmpdir-refusal \
  '把一个不可用的目录当临时工作区——它拒绝并说清是哪条通道给的路径，而不是悄悄退回 /tmp' \
  '结论是 `[gopi] tmpdir: tmpdir is not usable as a temp workspace …` 那一行：路径、失败原因、`(value from --tmpdir)` 三件齐；退出码 1。' \
  '框住 `tmpdir is not usable as a temp workspace`：拒绝句自己带路径。再框住 `value from --tmpdir`：它指出**是哪条通道**给了这个值（同一条拒绝在换用环境变量时会写 `value from GOPI_TMPDIR`）——这一行是「没有静默回退」的实物。' \
  'tmpdir is not usable as a temp workspace::拒绝句自带路径与原因;;value from --tmpdir::决策通道被指名' \
  -- "\"$B\" --tmpdir /proc/nope/nope task list"

# 202 接受的 /tmp：接受不是沉默——它把归属与所在分区都摊开
entry 202 flag-tmpdir-on-root \
  '把 /tmp 显式当临时工作区——它照办，但归属（哪条通道）与分区（和 / 同设备）都查得到' \
  '结论第一段是 `temp workspace  /tmp  (from --tmpdir)`；第二段是本机 `stat` 的分区号：`/` 与 `/tmp` 同为 2051。' \
  '框住 `(from --tmpdir)`：接受时同样写明归属（与 201 的拒绝共用同一种点名）。再框住 `2051 /tmp` 那一行 `stat` 输出：它给出「在 / 分区上」这件事的**判据本身**（设备号），而不是「路径不叫 /tmp 就算安全」的拼写判据。' \
  '(from --tmpdir)::接受时也写明归属;;2051 /tmp::判据是设备号不是路径拼写' \
  -- "\"$B\" --tmpdir /tmp config show;;stat -c '%d %n' / /tmp"

# 203 --model：不存在的模型名 → 行落 failed（不是静默换默认）
entry 203 flag-model-unknown \
  '点名一个不存在的模型——它不会悄悄换回默认，那一行任务直接落 failed' \
  '结论是这次运行的台账结果：`gopi task list` 里那一行 `[failed]` 与它记录的模型名 `zz-no-such-model-xyz`。' \
  '框住 `[failed]` 那一行：**模型名逐字进了台账**——一个不存在的模型不会被替换成默认值再假装成功。再框住同一行里的 `zz-no-such-model-xyz`：被点名的值与落账的值是同一个字符串，失败因此可追。' \
  '[failed]::行落 failed 不是静默换默认;;zz-no-such-model-xyz::被点名与落账是同一个字符串' \
  -- "\"$B\" --model zz-no-such-model-xyz --prompt \"Reply with exactly: ok\" --timeout 25;;\"$B\" task list"

# 204 --db 不可用：错误里带动词名与 errno
entry 204 flag-db-unwritable \
  '把一个不可写的路径当知识库——错误同时给出动词名和底层 errno' \
  '结论是唯一一行 `gopi memory search error: mkdir /nonexistent: permission denied`；退出码 1。' \
  '框住 `gopi memory search error`：前缀点名**动词 + 子动作**，不是一句泛泛的 "error"。再框住 `permission denied`：底层 errno 原样带出，读者不用二次调试就知道是哪一类失败。' \
  'gopi memory search error::前缀点名动词+子动作;;permission denied::底层 errno 原样带出' \
  -- "\"$B\" memory search --db /nonexistent/zz/x.db foo"

# 205 --db 指到别处：空结果也是一句解释
entry 205 flag-db-alt-ledger \
  '把知识库指到另一个文件——查询随之换了台账，空结果也有一句话解释' \
  '结论是 `No distilled memory found for this query.` 那一句，退出码 0。' \
  '框住这一句：空结果不是空屏，它解释「为什么空」并给出下一步（store）——与 204 的失败句形成对照：**能查但没查到**与**查不了**是两件事、两个退出码。' \
  'No distilled memory found::空结果也解释为什么空;;exit=0' \
  -- "\"$B\" memory search --db \$GOPI_HOME/.gopi/alt.db foo;;\"$B\" memory search --db /nonexistent/zz/x.db foo"

# 206 --home 指向一个新目录：真的换了 home（空台账）
entry 206 flag-home-fresh \
  '把 gopi 的数据根指到一个新目录——台账当场变空，证明这个开关真的换了 home' \
  '结论是 `No tasks found.`（新 home 里没有台账）与它之后 `--home $GOPI_HOME` 的对照运行。' \
  '框住 `No tasks found.`：同一个二进制、同一条 argv 只差 `--home`，台账从「没有」变「有」——**空**是这个开关生效的证据，不是失败。再框住第二个命令块的 `task(s)` 计数行：同一个进程环境下另一条 home 的台账。' \
  'No tasks found.::同一个二进制只差 --home，台账变空;;task(s)::另一条 home 的台账' \
  -- "\"$B\" --home \$GOPI_HOME/probe-fresh-home-\$\$ task list;;\"$B\" --home \$GOPI_HOME task list"

# 207 --home 与 GOPI_HOME 同值：别名（flag 胜过环境变量但值相同）
entry 207 flag-home-alias \
  '把 --home 指向与环境变量同一个目录——两条通道指向同一份数据，不是第二份' \
  '结论是同一条 argv 的两个块：`--home $GOPI_HOME` 与只靠 `GOPI_HOME` 得到同一份台账。' \
  '框住两个命令块的计数行：**同一份台账**在两条通道下被读到同样的行数——flag 不是「另一份 home」，它和 `GOPI_HOME` 是同一个旋钮的两个入口（`gopi -h` 的 Options 段每一行都写着 `(GOPI_…)`）。' \
  'task(s)::两条通道读到同一份台账;;task list::不带 --home 的对照块' \
  -- "\"$B\" --home \$GOPI_HOME task list;;\"$B\" task list"

# 208/209 --raw-log ON|OFF：OFF 就真的不写，而非法值退回默认 ON（asymmetry）
entry 208 flag-raw-log-off \
  '关掉原始 HTTP 日志——OFF 是真的不写，而一个**非法值**却按默认（ON）走，不报错' \
  '结论是两块输出的对照：`--raw-log OFF` 的块里**没有** `[gopi] raw log: …` 行，而 `--raw-log maybe` 的块里**有**。' \
  '框住有 raw log 那一行并对照另一块的缺失：同一个开关的两种取值给出相反结果；而第三块 `--raw-log maybe`（非法值）**和 ON 一样**——它不拒绝、不报错、按默认执行。这张图要读者看到的就是这个不对称：**OFF 生效、非法值静默当 ON**。' \
  '[gopi] raw log::OFF 无此行 / 非法值却按 ON 写出此行' \
  -- "\"$B\" --raw-log maybe --prompt \"Reply with exactly: ok\" --timeout 40;;\"$B\" --raw-log OFF --prompt \"Reply with exactly: ok\" --timeout 40"

entry 209 flag-raw-log-on \
  '把原始日志开回来——同一个开关的 ON 侧，行与文件都出现' \
  '结论是 `[gopi] raw log: …/sessions/<sid>/<sid>.raw.log` 一行与其后的会话目录。' \
  '框住 `[gopi] raw log:` 一行：ON 的可见证据**不是一句话而是那个路径**——日志真的落在会话目录里（本条目与 208 的 OFF 侧合起来才是这个开关的完整证据）。' \
  '[gopi] raw log::ON 的证据是那条路径' \
  -- "\"$B\" --raw-log ON --prompt \"Reply with exactly: ok\" --timeout 40;;ls -1 \$GOPI_HOME/.gopi/sessions | tail -1"

# 210 --max-tokens 与思考预算的钳制关系（数字可核）
entry 210 flag-max-tokens-clamp \
  '给出输出上限——它当场把这个数与思考预算的关系算给你看' \
  '结论是 `[gopi] ℹ️ thinking budget 16384 clamped to 2000 (max_tokens/2 = 4000/2)` 一行。' \
  '框住 `clamped to 2000` 与 `4000/2`：上限减半得到思考预算，**算式与结果都印在同一行**——读者不必相信我说的关系，数字自己说。' \
  'clamped to::上限减半得到思考预算，算式同现一行' \
  -- "\"$B\" --max-tokens 4000 --prompt \"Reply with exactly: ok\" --timeout 30"

# 211 --output-language 非法值：致命并解释（5 行）
entry 211 flag-output-language-refusal \
  '给一个不是语言标签的值——致命拒绝，并把「为什么这次是致命的」写清楚' \
  '结论是首行 `gopi: refusing to run: --output-language "chinese" is not a language tag` 与其后 4 行说明；退出码 2。' \
  '框住 `is not a language tag` 与最后一行 `usage:`：拒绝把**合法形状**（BCP-47 标签）与**改法**（`gopi --output-language zh-CN <verb>`）都给出来。图里还能看到第二行的判据：命令行给的值必须能用，而 settings 里的同类值只报告、不阻断——**同一个键的两条通道两条命运**。' \
  'is not a language tag::拒绝并给出合法形状;;usage::改法逐字给出' \
  -- "\"$B\" --output-language chinese task list"

# 212 --settings 指到另一个文件：配置来源被指名
entry 212 flag-settings-alt \
  '换一份 settings.json——谁在生效、路径、大小都当场指名' \
  '结论是 `gopi configuration in force` 里的 `settings file  …/alt-settings.json  (N bytes)` 与被指名的内存台账行。' \
  '框住 `settings file` 一行：生效的设置文件被逐字写出并**带字节数**（本条目这一次是一个 3 字节的 `{}`）——「哪份设置在起作用」因此是可核对的，不是猜的。' \
  'settings file::生效的设置文件被指名并带字节数' \
  -- "printf '{}\\n' > \$GOPI_HOME/probe-alt-settings.json; \"$B\" --settings \$GOPI_HOME/probe-alt-settings.json config show"

# ---------------------------------------------------------------------------
# 续做批（本 run：把 Options 段剩下的 flag 逐项补齐）
# 编号 215…：每条仍是「真 argv + 真输出 + 真 exit code」，`-h` 从不单独成条。
# ---------------------------------------------------------------------------

# 215 --api-key：被点名的凭证逐字进了请求，服务端以 401 回答（凭证不是「可能有效」）
entry 215 flag-api-key-bogus \
  '点名一个假 API key——它按你说的发出去，服务端 401，而 key 本身不出现在任何输出里' \
  '结论是 `[GOPI][minimal-core] streaming HTTP 401` 与其后的 `FAIL` 统计行：凭证被真的用上了。' \
  '框住 `streaming HTTP 401`：401 是**服务端对这把 key 的回答**——本进程没有「忽略你的 key、偷偷换一把」这一路。再框住同一次运行的 `⏱ … FAIL` 行：请求发出、被拒、会话以失败收场，三步都在。' \
  'streaming HTTP 401::被点名的 key 真的到了服务端;;FAIL::被拒之后会话以失败收场' \
  -- "\"$B\" --api-key zz-bogus-key-xyz --prompt \"Reply with exactly: ok\" --timeout 15"

# 216 --base-url：换成不可达端点——连接被拒，报错里逐字回显 URL
entry 216 flag-base-url-unreachable \
  '把 API 端点换成一个没人听的地址——会话以 FAIL 收场（带 URL 的那句错误是**间歇**的，故不写成性质）' \
  '结论是 `⏱ 12.0s | 1 iterations | FAIL | prompt=0(hit=0 miss=0) completion=0` 一行与 `exit=1`。' \
  '框住 `FAIL`：端点不可达时会话以失败收场。**注意本条 02 里没有出现那句带 URL 的 `Error: LLM call failed … connection refused`**——同一 argv 在早先的探测里出现过、本 run 落盘的这一次没有；间歇性的观测不写成性质，图里只框可复现的那一半，带 URL 的错误句作为对照运行记在 05-finding.md。' \
  'FAIL::端点不可达时会话以失败收场;;exit=1::失败带可判读的退出码' \
  -- "\"$B\" --base-url http://127.0.0.1:9/v1 --prompt \"Reply with exactly: ok\" --timeout 30"

# 217 位置规则：run flag 必须在动词之后 —— 放前面时动词被当成 prompt 文本（拒绝并给改法）
entry 217 flag-position-not-verb-first \
  '把动词写在 run flag 后面——它拒绝，并指出动词是从第一个参数读的' \
  '结论是首行 `gopi: "config" is a gopi command, not prompt text — it is a POSITIONAL here, following the run flag --api-key …`，退出码 1。' \
  '框住 `not prompt text` 与 `reads the verb from the FIRST argument`：拒绝句解释的是**位置规则**（动词永远第一），而不是「这个 flag 有问题」——同一句还替读者排除误判：`gopi --api-key <VALUE>` 本身正确。' \
  'not prompt text::拒绝解释的是位置规则不是 flag 本身;;reads the verb from the FIRST argument::根因逐字给出' \
  -- "\"$B\" --api-key zz-bogus-key-xyz config show"

# 218 --context-window：给一个远小于真实 prompt 的窗口 —— 它当场裁历史并印出比例
entry 218 flag-context-window-trim \
  '把上下文窗口说成 200 token——它当场量出超了多少、裁掉最老的消息，然后继续跑' \
  '结论是 `[gopi] ⚠️ context window: est 10821 tokens > 150 (5410.5% of 200) — trimmed 3 oldest message(s)` 一行。' \
  '框住 `est 10821 tokens` 与 `5410.5% of 200`：**估计值、预算、超出比例三个数同现一行**——窗口不是一个「软偏好」，它是真的被拿来算的。再框住 `trimmed 3 oldest message(s)`：裁剪是这次运行的后果，可数。' \
  'context window::估计值/预算/比例三个数同行;;trimmed::裁剪的后果可数' \
  -- "\"$B\" --context-window 200 --prompt \"Reply with exactly: ok\" --timeout 12"

# 219 --reasoning-effort：非法档位不致命 —— 报一行 warning 后按最低档跑
entry 219 flag-reasoning-effort-fallback \
  '给一个后端不认的推理档位——它警告后降到最低档继续跑，不中断' \
  '结论是 `[gopi] warning: GOPI_REASONING_EFFORT="bogus" not in the provider'\''s effort_values, using "minimal"` 一行，退出码 0。' \
  '框住 `not in the provider'"'"'s effort_values, using "minimal"`：这行把**被拒的值、判据（不在后端的能力列表里）、替代值**三样一起给出——同一个面上的 `--output-language` 遇非法值却 exit 2 拒绝（见 213），两个 flag 两种命运，都由各自这一行写明。' \
  'effort_values::被拒值与判据同现一行;;warning::这一行是警告不是致命' \
  -- "\"$B\" --reasoning-effort bogus --prompt \"Reply with exactly: ok\" --timeout 12"

# 220 --thinking：OFF 是真的关 —— `[thinking]` 行 0 条，且统计行报 0 chars
entry 220 flag-thinking-off \
  '关掉思考——不只是不打印，而是思考本身不再发生：`[thinking]` 行 0 条、统计报 0 chars' \
  '结论是两块对照：`--thinking OFF` 的块 `rounds=2 thinking=0 chars (…, 0/2 rounds carried thinking)` 且 `[thinking]` 行 0 条；不打这个 flag 的块有 `[thinking]` 行与非零 chars。' \
  '框住 `thinking=0 chars`：这是**统计口径**上的 0（不是「屏上没画」）——它与「有思考但被隐藏」在数字上分得开（见 221 的 `--hide-thinking`：chars 非零而屏上无行）。再框住对照块的 `[thinking]` 行：同一台机器、同一条 argv 只差这个 flag。' \
  'thinking=0 chars::统计口径上的 0 不是没画出来;;[thinking]::对照块里它存在' \
  -- "\"$B\" --thinking OFF --prompt \"Reply with exactly: ok\" --timeout 20;;\"$B\" --prompt \"Reply with exactly: ok\" --timeout 20"

# 221 --hide-thinking：隐藏展示 ≠ 关闭 —— 屏上 0 行，统计仍报非零 chars
entry 221 flag-hide-thinking-hides-display \
  '隐藏思考展示——屏上一行都不画，但思考仍在发生，统计行的 chars 非零' \
  '结论是 `--hide-thinking ON` 的块 `thinking_lines=0` 而 `rounds=3 thinking=994 chars (…, 3/3 rounds carried thinking)`。' \
  '框住 `thinking=0`（屏上）与 `thinking=994 chars`（统计）这对矛盾读数：它们同属一次运行——**展示被隐藏了，思考没有**。把它与 220 的 `--thinking OFF`（chars 真为 0）并排读，两个 flag 的区别就在这两行数字上。' \
  'carried thinking::屏上一条不画，思考仍在发生;;elapsed=::本次运行的用时统计' \
  -- "\"$B\" --hide-thinking ON --prompt \"Reply with exactly: ok\" --timeout 20"

# 222 --vision-base-url：这个开关在动词面上不存在 —— 按名字拒绝；值仍可从环境变量进
entry 222 flag-vision-base-url-scope \
  '在视觉动词后面写 --vision-base-url——它按名字拒绝；同一个端点换成环境变量，请求就真的发出去了' \
  '结论是两块对照：`gopi vision_analyze: unknown flag --vision-base-url`（退出码 1）与 `GOPI_VISION_BASE_URL=http://127.0.0.1:9/v1` 下的 `connect: connection refused`。' \
  '框住 `unknown flag --vision-base-url`：**这个开关没有动词面入口**——写错层级时它按名字拒绝，不静默忽略。再框住第二块的 `connection refused`：同一个端点换环境变量后请求真的发到了那个地址，证明这个 knob 不是「不被读的摆设」，只是入口不在动词后面。' \
  'unknown flag --vision-base-url::动词面按名字拒绝;;connection refused::环境变量通道真的把请求发到这里' \
  -- "printf 'probe\n' > \$GOPI_HOME/probe-attach.txt; \"$B\" vision_analyze --vision-base-url http://127.0.0.1:9/v1 \$GOPI_HOME/probe-attach.txt;;export GOPI_VISION_BASE_URL=http://127.0.0.1:9/v1; \"$B\" vision_analyze \$GOPI_HOME/probe-attach.txt"

# 223 --vision-model：这个 knob 在 CLI 上只有一个入口 —— 视觉动词按名字拒绝它
entry 223 flag-vision-model-scope \
  '在视觉动词后面写 --vision-model——它按名字拒绝，说明这个值只有一条 CLI 入口' \
  '结论是 `gopi vision_analyze: unknown flag --vision-model` 与同一动词 `-h` 的 `Flags:` 段只列了 `--prompt`。' \
  '框住 `unknown flag --vision-model`：**同一个开关在两级入口上的命运不同**——`gopi --vision-model <VALUE>`（run 面）接受它，`gopi vision_analyze --vision-model`（动词面）按名字拒绝；读者由此知道该值只能从 run 面、环境变量或 settings 进。' \
  'unknown flag --vision-model::动词面按名字拒绝这个开关;;Flags::动词面自己只认 --prompt' \
  -- "\"$B\" vision_analyze --vision-model zz-no-such-vision-xyz \$GOPI_HOME/probe-attach.txt;;\"$B\" vision_analyze -h"

# 224 --bash-timeout：声明面/解析面/消费面三层——真跑不被打断，代码里找不到读它的地方
entry 224 flag-bash-timeout-declared-unread \
  '给 --bash-timeout 一个 3 秒的上限，然后真跑一条 25 秒的 bash —— 它没有被打断，而代码里也没有人读这个值' \
  '结论是三块证据同页：真跑的 `elapsed=` 行与回答里的「sleep 25 已执行」、`--bash-timeout abc` 的 `invalid value … want an integer`（解析层是活的）、以及消费层的 grep 结果（`internal/config` 写入的字段在别处无人读）。' \
  '框住 `TOOL25-DONE` 那一行：3 秒的上限下这条命令仍然跑完（哨兵串只会在 25 秒的 sleep 走完之后出现）——声明（`-h` 里写着这个 flag）、解析（非法值被拒）、**消费**三层里，第三层在这条证据里缺席。再框住 grep 出来的 `BashTimeoutSec`：读者可以自己复核「谁读它」。' \
  'TOOL25-DONE::3 秒上限下这条命令仍然跑完;;invalid value::解析层是活的;;BashTimeoutSec::这个字段只出现在 config 包内' \
  -- "\"$B\" --bash-timeout 3 --prompt \"Use the bash tool to run exactly: sleep 25; then reply with exactly TOOL25-DONE\" --timeout 45;;\"$B\" --bash-timeout abc --prompt \"Reply with exactly: ok\" --timeout 8 > \$GOPI_HOME/refuse.txt 2>&1; echo \"exit=$?\"; sed -n '1p' \$GOPI_HOME/refuse.txt;;grep -rn \"BashTimeoutSec\" --include=*.go /home/nick/workspace/gopi-01a0edf349a371b78c6cbb79fb58b772/internal /home/nick/workspace/gopi-01a0edf349a371b78c6cbb79fb58b772/cmd"

# 225 --tool-timeout：同族第三个成员，用一次真读来问「它到了工具层没有」
entry 225 flag-tool-timeout-declared-unread \
  '给 --tool-timeout 一个 1 秒的上限，然后让 read 工具去读一个真的文件 —— 它照读' \
  '结论是两块：`--tool-timeout 1` 下 read 工具正常返回文件内容，以及 grep 出的消费层证据（工具层读的是环境变量 `GOPI_TOOL_TIMEOUT_SEC`，不是配置里的字段）。' \
  '框住 `TOKEN-ZZ-4711`：1 秒的上限下 read 照常返回——与 224 同一形态（声明与解析都在，消费层缺席）。再框住 grep 输出里的 `GOPI_TOOL_TIMEOUT_SEC`：工具层真正读的是**环境变量**，同一族三个 flag 的运行时值因此由环境决定，而不是由你写下的 flag 决定。' \
  'TOKEN-ZZ-4711::1 秒上限下 read 照常返回;;GOPI_TOOL_TIMEOUT_SEC::工具层读的是环境变量' \
  -- "printf 'TOKEN-%s\\n' ZZ-4711 > \$GOPI_HOME/probe-attach.txt; \"$B\" --tool-timeout 1 --prompt \"Use the read tool on \$GOPI_HOME/probe-attach.txt then reply done\" --timeout 30;;grep -rn \"GOPI_TOOL_TIMEOUT_SEC\" --include=*.go /home/nick/workspace/gopi-01a0edf349a371b78c6cbb79fb58b772/internal"

# 226 --tool-timeout 的另一面：read 遇到命名管道 —— 失败在 seek 上，错误逐字带回 errno
entry 226 flag-tool-timeout-read-pipe \
  '让 read 工具去读一个命名管道——它失败在 seek 上，错误把底层 errno 原样带出' \
  '结论是 `Error reading …/probe.fifo: read: seek …/probe.fifo: illegal seek`。' \
  '框住 `illegal seek`：这条错误**不是**超时（本条的 `--tool-timeout 3` 在这里没轮到发声），而是 read 的实现先 seek 再读、而命名管道不支持 seek（ESPIPE）——「能打开但读不了」的路径因此有自己的错误文本，不冒充超时、也不静默返回空。' \
  'illegal seek::失败原因是 seek 而不是超时;;probe.fifo::路径逐字回显' \
  -- "mkfifo \$GOPI_HOME/probe.fifo 2>/dev/null; \"$B\" --tool-timeout 3 --prompt \"Use the read tool on \$GOPI_HOME/probe.fifo then reply done\" --timeout 25"

# 227 --bash-idle：同族的第三把计时刀 —— flag 下了令，真跑没停，代码里也没人读它
entry 227 flag-bash-idle-declared-unread \
  '把静默时限设成 3 秒、绝对上限留 60 秒，跑一条 12 秒不出声的 bash —— 它跑完了' \
  '结论是三块同页：flag 侧与 env 侧两次真跑都正常完成、`invalid value` 说明解析层活着、grep 说明 `cfg.BashIdleSec` 在别处无人读。' \
  '框住两次真跑的计数行：**flag 与 env 两条通道都没能让计时器开火**（本条不写「杀」，因为没杀）。再框住 grep 的 `cfg.BashIdleSec`：与 224/225 同一族同一形态——声明与解析都在，消费层缺席；「太慢」与「太安静」两把刀在本二进制里都读不到这两个 flag 写下的值。' \
  '已执行::两条通道都没能让计时器开火;;BashIdleSec::这个字段在别处无人读' \
  -- "\"$B\" --bash-timeout 60 --bash-idle 3 --prompt \"Use the bash tool to run exactly: sleep 12; then reply done\" --timeout 40;;export GOPI_BASH_IDLE_SEC=3; \"$B\" --bash-timeout 60 --prompt \"Use the bash tool to run exactly: sleep 12; then reply done\" --timeout 40;;grep -rn \"BashIdleSec\" --include=*.go /home/nick/workspace/gopi-01a0edf349a371b78c6cbb79fb58b772/internal /home/nick/workspace/gopi-01a0edf349a371b78c6cbb79fb58b772/cmd"


# 228 --compact：非法值静默当默认（与同面的整数型 flag 拒绝非法值形成对照）
entry 228 flag-compact-invalid-silent \
  '给 --compact 一个非法值——它不拒绝、不警告，按默认（开）继续跑' \
  '结论是首行 `gopi: invalid value "abc" for --bash-timeout: want an integer`（整数型 flag 拒绝）与随后 `--compact abc --prompt …` 正常完成的对照。' \
  '框住 `invalid value`：**同一次运行里**另一个 flag 对非法值给出了拒绝句；再框住 `--compact abc` 那一块的 `ok`：同一个二进制对 ON|OFF 型 flag 的非法值一言不发。同一份 `gopi -h` 把两者都列为 run flag，判读的差别只能从这类对照运行里看出来。' \
  'invalid value::整数型 flag 马上拒绝;;elapsed=::ON|OFF 型 flag 照常跑完' \
  -- "\"$B\" --bash-timeout abc --prompt \"Reply with exactly: ok\" --timeout 8 > \$GOPI_HOME/refuse-compact.txt 2>&1; echo \"exit=$?\"; sed -n '1p' \$GOPI_HOME/refuse-compact.txt;;\"$B\" --compact abc --prompt \"Reply with exactly: ok\" --timeout 20"

# 229 --prompt：位置参数是文件，prompt 是指令 —— 文件内容被附在指令之后
entry 229 flag-prompt-attaches-file \
  '把文件当位置参数：它读文件内容并把它接到 `--prompt` 的指令后面' \
  '结论是运行末尾的回答 `TOKEN-ZZ-4711`（只存在于那个文件里）。' \
  '框住 `TOKEN-ZZ-4711`：这个串**只存在于**位置参数指向的文件里——回答里出现它，证明位置参数的内容确实被附进指令（`gopi -h` 对 `--prompt` 的原话：a text file'"'"'s contents are appended to the instruction）。' \
  'TOKEN-ZZ-4711::这个串只存在于位置参数的文件里' \
  -- "printf 'TOKEN-%s\\n' ZZ-4711 > \$GOPI_HOME/probe-attach.txt; \"$B\" --prompt \"Reply with exactly the token written in the attached file, nothing else\" \$GOPI_HOME/probe-attach.txt --timeout 25"

# 230 --timeout：1 秒上限 —— 会话未完成即被判死，统计行给出用时与迭代数
entry 230 flag-timeout-fires \
  '把总时限设成 1 秒——会话在第一次迭代就撞线，以 FAIL 收场并把用时印出来' \
  '结论是 `⏱ 1.0s | 1 iterations | FAIL | prompt=0(hit=0 miss=0) completion=0` 一行与 `elapsed=1.7s`。' \
  '框住 `1.0s` 与 `FAIL`：**时限是从这一行里读出来的**（1 秒的旗子对应 1.0 秒的实际用时），失败不是「它没说话」而是带数的判决。再框住 `1 iterations`：撞线发生在请求发出之前，因此 prompt/completion 计数全 0——同一面上的 218（`--context-window`）是「算完再裁」，本条是「时间到就停」，两个 flag 各自的收场形态都在统计行里。' \
  '1 iterations::撞线发生在请求发出之前;;elapsed=::本次运行的用时统计' \
  -- "\"$B\" --timeout 1 --prompt \"Reply with exactly: ok\""

echo "== 形状断言：每条恰好六件、件件非空、02 有 exit 行 =="
shape_bad=0
for d in "$OUTROOT"/2[0-9][0-9]-flag-*/; do
  nfiles=$(find "$d" -maxdepth 1 -type f | wc -l)
  [[ "$nfiles" -ne 6 ]] && { echo "SHAPE FAIL: $d 有 $nfiles 个文件（要求 6）" >&2; shape_bad=1; }
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $d/$f 缺失或为空" >&2; shape_bad=1; }
  done
  grep -qE '^exit=[0-9]+$' "$d/02-output.txt" || { echo "SHAPE FAIL: $d/02-output.txt 无 ^exit=<n>" >&2; shape_bad=1; }
  # 03/04 必须不同：标注真的发生过（验收项）
  a=$(sha256sum "$d/03-raw.png" | cut -c1-16); b=$(sha256sum "$d/04-annotated.png" | cut -c1-16)
  [[ "$a" == "$b" ]] && { echo "SHAPE FAIL: $d 的 03/04 sha256 相同（标注没发生）" >&2; shape_bad=1; }
done

echo "== index (n slug last_exit render lines ann_bytes) =="
cat "$W/index.tsv"
cp -a "$W/." "$EV/materials-flagF-logs/" 2>/dev/null || { mkdir -p "$EV/materials-flagF-logs"; cp -a "$W/." "$EV/materials-flagF-logs/"; }
if [[ -s "$W/failed.txt" ]]; then
  echo "GENERATOR FAILED: $(wc -l < "$W/failed.txt") 条没产出标注图" >&2
  cat "$W/failed.txt" >&2; exit 1
fi
[[ "$shape_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 形状断言未通过" >&2; exit 1; }
printf 'WORK=%s\nSANDBOX=%s\n' "$W" "$SB"
