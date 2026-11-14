#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-env-E1.sh — E 面（环境变量）批 1 的三组素材条目生成器：
#   Distributed task ledger (sync / worker / role) / Remote dispatch / Agent runtime。
#
# 每一条目 = 一个变量，形状是**一次真实改值的前后对照**：
#   01-input.txt  两条 arm 的 verbatim argv（含 cwd / GOPI_HOME / 二进制绝对路径）
#   02-output.txt arm A（默认值）与 arm B（改了那个变量）的逐字节输出，各带 exit=<n>
#   03/04-*.png   同一份文本渲染 + 标注（gopi tool text2img + gopi annotate，像素自证）
#   05-finding.md 发现（真缝或"查过、没缝"）
#
# 诚实性约束（与本仓既有生成器同一条契约）：
#  * 每个变量都在私有沙盒 profile（Rule 34 的 `gopi tool sandbox create`）里真跑；
#  * `-h` 不算跑（Rule 31）：每条都有真 argv + 真 stdout/stderr + 真 exit code；
#  * 输出不编造：抓不到差异就写"没抓到差异"，并把它本身记成 finding；
#  * 标注图自证：框里没有墨迹 / annotate 改了 0 像素 → 拒绝产出该图（exit 1）。
#
# 用法: bash scripts/materials/gen-env-E1.sh [out_root]
set -uo pipefail

ROOT=/home/user/workspace/gopi-01a0ed23088d7062abf9bd91d33985d4
TASK_ID=01a0ed23-088d-7062-abf9-bd91d33985d4
BIN="$ROOT/bin/gopi"
RENDER="$ROOT/scripts/materials/render.py"
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:-$ROOT/docs/archive/demo/materials}"
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"

# 每个 arm 的外生常量：把 LLM/网络排除掉，让**只有那个变量**是 A/B 之间的差。
DEAD_ENDPOINT=http://127.0.0.1:9

RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
SB="$("$BIN" tool sandbox create --label "materials-env-E1-$RUNSTAMP" --task-id "$TASK_ID" | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
export GOPI_HOME="$SB"
echo "sandbox=$SB"
echo "measured_commit=$COMMIT"

# 本批的工单记账表：随素材一起提交的**交付物**（Rule 35：不是隐藏状态文件 ——
# 它可读、可审、解释自己，并且只被本生成器读来保持幂等）。
QUEUE="$OUTROOT/env-E1-queue.tsv"
[[ -f "$QUEUE" ]] || printf 'slug\tticket_id\tsubmitted\n' > "$QUEUE"

# Rule 22: the scratch dir comes from scripts/lib/tmp-ban.sh — the ONE reader of
# the scratch root, asserted off `/` by DEVICE ID and registered for trap-time
# cleanup. /tmp shares `/`'s device here, so it is never a candidate.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
W="$(gopi_scratch_mkdir materials-E1)" || exit 1
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$OUTROOT"
: > "$W/index.tsv"

# --- fixture：每个 notify 类条目自己造一行（只落在沙盒台账里） ------------------
mkid() { grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1; }
sub() { (cd "$ROOT" && "$BIN" task submit "$@" 2>&1 | mkid); }

# --- 一个条目 -----------------------------------------------------------------
# entry <n> <slug> <group> <var> <default> <reader> <feature> <ann> <solves> \
#       <readw> <caption> <marks> <envA> <envB> <argv...>
#   envA/envB  臂 A / 臂 B 的环境（空格分隔的 K=V；臂 B 必须含臂 A 的全部，只多一个改变量）
#   marks      --mark-match 规格，'|' 分隔成 'needle:label'，';;' 分隔多条
entry() {
  local n="$1" slug="$2" group="$3" var="$4" def="$5" reader="$6" feature="$7" ann="$8" solves="$9" readw="${10}" caption="${11}" marks="${12}" envA="${13}" envB="${14}"; shift 14
  [[ "${1:-}" == "--" ]] && shift
  local argv=("$@")
  local dir="$OUTROOT/$n-$slug"
  mkdir -p "$dir"
  find "$dir" -maxdepth 1 -type f ! -name '0[0-9]-*' -delete 2>/dev/null
  rm -f "$dir"/03-raw*.png "$dir"/04-annotated*.png

  # 01-input.txt —— 两个 arm 的 verbatim 命令
  {
    printf 'cd %s\n\n' "$ROOT"
    printf '# arm A — %s 未设（默认 %s）\n' "$var" "$def"
    printf 'GOPI_HOME=%s %s \\\n' "$SB" "$envA"
    printf '  %s %s\n\n' "$BIN" "${argv[*]}"
    printf '# arm B — %s 改了值\n' "$var"
    printf 'GOPI_HOME=%s %s \\\n' "$SB" "$envB"
    printf '  %s %s\n' "$BIN" "${argv[*]}"
  } > "$dir/01-input.txt"

  # 真跑两个 arm（stdout+stderr 合并；exit code 钉在同一份文件里）
  local outA rcA outB rcB
  outA="$( (cd "$ROOT" && env $envA "${argv[@]}") 2>&1 )"; rcA=$?
  outB="$( (cd "$ROOT" && env $envB "${argv[@]}") 2>&1 )"; rcB=$?
  {
    printf '── arm A（%s 未设，默认 %s）──────────────────\n' "$var" "$def"
    printf '%s\n' "$outA"
    printf 'exit=%d\n\n' "$rcA"
    printf '── arm B（%s 已改值；环境：%s）──────────────────\n' "$var" "$envB"
    printf '%s\n' "$outB"
    printf 'exit=%d\n' "$rcB"
  } > "$dir/02-output.txt"

  {
    printf '# %s-%s — %s\n\n' "$n" "$slug" "$solves"
    printf -- '- face: env\n'
    printf -- '- group: %s\n' "$group"
    printf -- '- feature: `%s`\n' "$feature"
    printf -- '- variable: `%s`\n' "$var"
    printf -- '- default: `%s`\n' "$def"
    printf -- '- reader: `%s`\n' "$reader"
    printf -- '- annotation_required: %s\n' "$ann"
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- how_to_replay: `bash scripts/materials/gen-env-E1.sh` 重跑整批；单条按 `01-input.txt` 的两条 arm 逐字执行\n'
    printf -- '- sandbox_profile: `%s`（私有；操作者台账从未被读写）\n\n' "$SB"
    printf '## 原始输入从哪来\n\n'
    printf '`01-input.txt` 是**两条 arm 的 verbatim argv**：A 是不设 `%s` 的默认运行，\n' "$var"
    printf 'B 只多/改这一个变量，其余环境逐字节相同——所以 `02-output.txt` 里的差异只能来自它。\n'
    printf '`GOPI_HOME` 指向的沙盒 profile 是本次运行读写的那个私有台账。\n\n'
    printf '## 输出怎么读\n\n'
    printf '`02-output.txt` 是 stdout 与 stderr 合并后的**逐字节**记录，两段各以裸形式 `exit=<n>` 收尾\n'
    printf '（census 的 output 谓词认这个拼写）。\n%s\n\n' "$readw"
    printf '## 标注图在指什么\n\n%s\n' "$caption"
  } > "$dir/00-entry.md"

  # render.py 的 --mark-match 是 "一个 needle 一个 label"1:1 的；第二次 --mark-match
  # 会被当成 "cell anchor"（实测报错）。所以这里**只接受一条**规格，且 needle/label
  # 都不含 ':'（partition 在第一个 ':' 处切分）。
  local mkargs=()
  if [[ -n "$marks" ]]; then
    [[ "$marks" == *";;"* ]] && { echo "FATAL: entry $n: marks 只能一条" >&2; return 1; }
    [[ "${marks%%:*}" == *":"* ]] && { echo "FATAL: entry $n: mark needle 含 ':'" >&2; return 1; }
    mkargs+=(--mark-match "$marks")
  fi
  printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$caption" > "$W/$slug.head"
  {
    cat "$W/$slug.head"
    printf '── 01-input.txt（两条 arm 的 verbatim 命令）──\n'
    cat "$dir/01-input.txt"
    printf '\n── 02-output.txt（verbatim 输出，两段各带 exit code）──\n'
    cat "$dir/02-output.txt"
  } | python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --json-out "$W/$slug.json" --max-lines 120 "${mkargs[@]}" \
        > "$W/$slug.render.log" 2>&1
  local rrc=$?

  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$n" "$slug" "$var" "$rcA/$rcB" "$rrc" \
     "$(wc -l < "$dir/02-output.txt")" >> "$W/index.tsv"
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $n-$slug 标注图没产出（render=$rrc）——见 $W/$slug.render.log" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
  fi
  printf 'entry %-34s exitA=%-3s exitB=%-3s render=%-3s marks=%d\n' "$n-$slug" "$rcA" "$rcB" "$rrc" "${#mkargs[@]}"
}

finding() { cat > "$OUTROOT/$1/05-finding.md"; }

# Oracle：把「差异在哪」写成脚本自己刚测出的计数（Rule 10.4：最小样本自证），
# 结论句里的数字不是散文，是刚跑出来的。
#
# 归一化：两臂之间**按定义就会不同**的东西（session id、耗时、时间戳）必须先抹掉，
# 否则「零差异」的条目会被读成十几行差异 —— 那是把噪声当成运行结果。
#  另外四类**已实测确认是 run-to-run 噪声**、与所测变量无关的行也一并抹掉：
#    * session id / raw-log 路径（每次运行都新、已由 UUID 规则覆盖）
#    * 耗时（NS）
#    * `[trajectory stats]` 块 —— 它是否被打印取决于这一轮走得多远，而两条臂的
#      endpoint 是同一个死地址，走得多远由**重试时序**决定，不由被测变量决定
#      （实测：同一个臂连跑两次，这块时有时无）
#    * 末尾那行 `Error: LLM call failed …` —— 同上（本轮在 retry 用尽前退出就不打印）
norm() {
  sed -E -e 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/UUID/g' \
         -e 's/[0-9]+\.[0-9]+s/NS/g' \
         -e 's/iterations [0-9]+/iterations N/g' \
         -e 's/^── arm [AB]（.*$//' \
         -e '/^\[trajectory stats\]$/d' \
         -e '/^rounds=[0-9]/d' \
         -e '/^waits: median/d' \
         -e '/^tool_calls=/d' \
         -e '/^completion_tokens=/d' \
         -e '/^tasklist: ever_established=/d' \
         -e '/^memory: first_tool_round=/d' \
         -e '/^Error: LLM call failed/d' \
         -e '/^memory adoption:/d' \
         -e '/^memory store calls:/d' \
         -e '/^memory consult checkpoint/d' \
         -e '/^elapsed=/d'
}
oc() { # oc <n> <slug> <armA|armB> <needle> <label>
  local dir="$OUTROOT/$1-$2" arm="$3" needle="$4" label="$5" seg c
  if [[ "$arm" == A ]]; then
    seg="$(sed -n '1,/^exit=/p' "$dir/02-output.txt")"
  else
    seg="$(sed -n '/^── arm B/,$p' "$dir/02-output.txt")"
  fi
  c="$(printf '%s\n' "$seg" | grep -c -- "$needle" || true)"
  printf -- '- **arm %s**：`grep -c -- %s` = **%s** %s\n' "$arm" "$needle" "$c" "$label" >> "$dir/00-entry.md"
}
ochead() {
  printf '\n## Oracle（脚本刚测出的数字，不是散文）\n\n' >> "$OUTROOT/$1-$2/00-entry.md"
}
ocdiff() { # ocdiff <n> <slug>  —— 两臂（归一化后、忽略 exit 行）的差异行数
  local dir="$OUTROOT/$1-$2" segA segB d
  segA="$(sed -n '1,/^exit=/p' "$dir/02-output.txt" | norm)"
  segB="$(sed -n '/^── arm B/,$p' "$dir/02-output.txt" | norm)"
  d="$(diff <(printf '%s\n' "$segA" | grep -v '^exit=') <(printf '%s\n' "$segB" | grep -v '^exit=') | grep -c '^[<>]' || true)"
  printf -- '- **两臂差异行数**（归一化：session id / 耗时 / 时间戳已抹平）：**%s**\n- 两臂 exit：A=%s，B=%s\n' "$d" \
    "$(grep -m1 '^exit=' "$dir/02-output.txt" | tr -d 'exit=')" \
    "$(tail -1 "$dir/02-output.txt" | tr -d 'exit=')" >> "$dir/00-entry.md"
}

echo "== entries =="

# ── 组 1：GOPI_NODE_ROLE ──────────────────────────────────────────────────────
entry 030 env-node-role-client "Distributed task ledger" GOPI_NODE_ROLE "unset（无角色）" \
  "internal/config/node_role.go:56（resolve 的唯一读者；role 常量 node_role.go:33）" \
  "GOPI_NODE_ROLE=client 声明本节点是「替中心干活」的客户端" yes \
  "让一台机器说清自己是「把活派出去的中心」还是「替中心干活的节点」——一次运行，一行横幅" \
  "结论是 **arm B 独有的 \`[gopi] node:\` 横幅**：arm A（不设该变量）里同一串出现 0 次——本条目已用真跑标定过这条谓词（见 05-finding.md 的 oracle 三行）。" \
  "框住 B 段那行 \`[gopi] node:\`：角色是**启动横幅**里的一个既定事实，不是要靠翻 settings.json 推断的东西——这一行同时也是 finding 的对象（见 05-finding.md）。" \
  "(client):角色横幅：arm B 独有（arm B）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_HOME=$SB" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_HOME=$SB GOPI_NODE_ROLE=client" \
  -- ./bin/gopi "hi"

# ── 组 2：Remote dispatch ────────────────────────────────────────────────────
entry 031 env-sync-peers "Distributed task ledger" GOPI_SYNC_PEERS "settings.json sync_peers＝[]" \
  "internal/config/config.go:709（唯一的 env 读者）；常量 config.go:697" \
  "GOPI_SYNC_PEERS=<逗号别名表> 声明对端节点" yes \
  "决定这台机器是不是集群里的一员：有没有对端，直接决定一条推送腿是跑还是短接" \
  "结论是两句互斥的横幅：A 段 \`code delivery OFF (no sync peer declared …)\`，B 段 \`code delivery ON — role client …\`，两者 exit 都是 0。" \
  "框住 A 段的 \`code delivery OFF\` 与 B 段的 \`code delivery ON\`：同一个动词、同一份台账，唯一差别是环境里那一个变量——这就是「声明了没有」的可见边界。" \
  "code delivery OFF:无对端：腿短接（arm A）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_SYNC_PEERS=hp" \
  -- ./bin/gopi task sync-pass --once

entry 032 env-sync-interval "Distributed task ledger" GOPI_SYNC_INTERVAL "absent/0 = 关（手动）" \
  "cmd/agent/cli_tasks_sync.go:615（唯一读者 syncIntervalFromEnv；常量 :99）" \
  "GOPI_SYNC_INTERVAL=<秒> 是 serve tick 自动同步的节拍" yes \
  "一个写错的节拍值必须当场喊出来，不能悄悄按 0 跑（静默等于关掉同步）" \
  "结论是 **arm B 独有的 \`[serve] sync: GOPI_SYNC_INTERVAL: \"abc\" is not a non-negative integer (seconds)\`**；两段 exit 都是 0（见 05-finding.md）。" \
  "框住 B 段那行 \`GOPI_SYNC_INTERVAL: \"abc\" is not\`：这条变量的契约是「坏值 = 报错」，这一行就是它在真跑里的样子——**但它没有改退出码**，那正是 finding 要说的。" \
  "not a non-negative integer:坏值被报出来（arm B 独有）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_TASK_SYNC=1" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_TASK_SYNC=1 GOPI_SYNC_INTERVAL=abc" \
  -- ./bin/gopi task sync-pass --once

entry 033 env-sync-git-interval "Distributed task ledger" GOPI_SYNC_GIT_INTERVAL "900（0/负 = 关）" \
  "cmd/agent/cli_tasks_sync_git.go:34（唯一读者 serveGitSyncInterval；常量 :23）" \
  "GOPI_SYNC_GIT_INTERVAL=<秒> 是 serve tick 的 git/ref 腿节拍" yes \
  "让一条**推分支**的腿能被人为关掉（不想让自动流程动 ref 的时候）" \
  "结论是 **arm A 出现 \`[git-client] pushed=\`（推送腿在跑，1 次），arm B 里这一串 0 次**——两段都是 \`pass done\`、exit=0。" \
  "框住 A 段那行 \`[git-client] pushed=\`：把它设成 0 之后这一行整段消失，而同一条命令**其余部分逐字不变**——这就是「这条腿被关掉」的可见形状。" \
  "git-client] pushed=:推送腿在跑（arm A，默认 900s）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_SYNC_PEERS=hp" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_SYNC_PEERS=hp GOPI_SYNC_GIT_INTERVAL=0" \
  -- ./bin/gopi task sync-pass --once

entry 034 env-adoption-interval "Distributed task ledger" GOPI_ADOPTION_INTERVAL "900（0/负 = 关）" \
  "cmd/agent/cli_tasks_adopt.go:43（唯一读者 serveAdoptionInterval；常量 :24）" \
  "GOPI_ADOPTION_INTERVAL=<秒> 是 serve tick 收养（adoption）腿的节拍" yes \
  "控制「对端推回来的分支什么时候被并进 master」这条腿多久跑一次，或直接关掉" \
  "结论是 **arm A 出现一行 \`[serve] [adopt] …\`，而 arm B 连 \`[adopt]\` 五个字符都没有**——arm B 也**没有**像 MIN_AGE_H/MAX 那样打警告（见 05-finding.md）。" \
  "框住 A 段那行 \`[adopt]\`：同一条命令、同一份环境，只把收养节拍设成 \`abc\`，整条腿（连同它的自我报告）就静默消失——这就是 finding 指的那一像素。" \
  "adopt]:收养腿在跑（arm A，默认）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_SYNC_PEERS=hp GOPI_SYNC_GIT_INTERVAL=0" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_SYNC_PEERS=hp GOPI_SYNC_GIT_INTERVAL=0 GOPI_ADOPTION_INTERVAL=abc" \
  -- ./bin/gopi task sync-pass --once

entry 035 env-compute-nodes "Remote dispatch" GOPI_COMPUTE_NODES "unset（无白名单）" \
  "internal/config/config.go:891（唯一 env 读者，常量 :679）；消费面 config.go:887 ResolveComputeNodes" \
  "GOPI_COMPUTE_NODES=<JSON 数组> 是远程计算节点白名单" yes \
  "一句话声明「哪些机器可以替我跑活」，不用改 settings.json" \
  "结论是**两臂零可观测差异**：把这一节唯一的变量设成一个**不合法的** JSON（\`{oops\`），任务列表的逐字输出与不设时完全相同——没有报错、没有警告、没有一行提到它（见 05-finding.md）。" \
  "框住两臂共有的 \`No tasks found.\`：两臂在这一行上**逐字相同**，这是「设了等于没设」的可视形状（本条目里没有别的像素可选）。" \
  "No tasks found:两臂逐字相同（零可观测差异）" \
  "GOPI_HOME=$SB" \
  "GOPI_HOME=$SB GOPI_COMPUTE_NODES={oops" \
  -- ./bin/gopi task list --status pending

entry 036 env-timeout-sec "Agent runtime" GOPI_TIMEOUT_SEC "0 = 不限" \
  "internal/agent/loop.go:3132（唯一 env 读者）" \
  "GOPI_TIMEOUT_SEC=<秒> 是 agent 一轮 run 的墙钟上限" yes \
  "给一次 agent 运行装一个确定的截止时间——跑到时间就停，而不是无限等" \
  "结论是两段末尾的两行耗时数字：**arm A 的 \`elapsed=15.1s\` 与 arm B 的 \`⏱ 3.0s\`**（见 05-finding.md 的逐字行）。" \
  "框住两段的计时行（\`elapsed=15.1s\` / \`⏱ 3.0s\`）：把同一个只读提示交给同一个死掉的 endpoint，唯一变量是截止时间——3 秒到达就停，15 秒到 retries 用尽才停。" \
  "elapsed=:两臂各自耗时（数字见 05-finding.md）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_TIMEOUT_SEC=3" \
  -- ./bin/gopi "hi"

entry 037 env-heartbeat-sec "Agent runtime" GOPI_HEARTBEAT_SEC "30（秒）" \
  "internal/agent/loop.go:3142（唯一 env 读者）；默认值常量 heartbeat.go:11" \
  "GOPI_HEARTBEAT_SEC=<秒> 是「进度停滞」心跳的检查间隔" yes \
  "在 agent 明显卡住时按固定节拍检查一次，并提醒它继续（而不是等完整轮次预算烧完）" \
  "结论是**两段没有差异**：同一份 argv、同时只有一个死 endpoint，只把心跳间隔改成 1 秒，输出逐字节相同（\`diff\` 为 0 行）——这条变量在本条目里**没有任何可观测面**（见 05-finding.md）。" \
  "框住 A 段第一行的 \`[gopi] loaded GOPI.md\`：它是两段共有的第一行——「没有差异」这条结论只能用「框同一行、内容也相同」来表达（本条目里没有别的像素可选）。" \
  "loaded GOPI.md:两臂逐字节相同（无差异=结论）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_HEARTBEAT_SEC=1" \
  -- ./bin/gopi "hi"

entry 038 env-debug "Agent runtime" GOPI_DEBUG "unset = 关（truthy-only）" \
  "internal/scratch/tmproot.go:906（EnvDebugEnabled，唯一的 knob reader；常量 :918）" \
  "GOPI_DEBUG=1 打开调试输出（off/0/false/2 都不算开）」" yes \
  "排障时加一行环境变量就能拿到额外的诊断输出，不用改配置文件" \
  "结论是 arm A 独有的一行 \`[gopi] tmpdir: TMPDIR=…\`，arm B（GOPI_DEBUG=off）一个字都没有——exit 都是 0。" \
  "框住 A 段的 \`[gopi] tmpdir:\` 行：这是「开关打开」的唯一可见痕迹；B 段（\`GOPI_DEBUG=off\`）里同一串 0 次——truthy-only 的语义就长在这一对里。" \
  "tmpdir:开关打开时独有的一行（arm A）" \
  "GOPI_DEBUG=1" \
  "GOPI_DEBUG=off" \
  -- ./bin/gopi task list

entry 039 env-worker-max-rounds "Distributed task ledger" GOPI_WORKER_MAX_ROUNDS "0 = 不限（抽干即退）" \
  "cmd/agent/cli_tasks_worker.go:679（唯一 env 读者；常量 :82）" \
  "GOPI_WORKER_MAX_ROUNDS=<N> 是 worker 循环的轮次上限" yes \
  "给「一直守着队列」的 worker 一个明确的上限：跑够 N 轮就退出，交给下一次调度" \
  "结论是 A 段的 \`queue drained (3/3 idle rounds)\`（等第三轮才自退）与 B 段的 \`max rounds (1) reached — exiting\`，两段 exit 都是 0。" \
  "框住 A 段的 \`queue drained\` 与 B 段的 \`max rounds (1) reached\`：同一份空队列，唯一变量是轮次上限——一个把退出交给「闲置轮数」，一个交给「轮次计数」。" \
  "queue drained:arm A：靠闲置轮数退出" \
  "GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKER_IDLE_SEC=1" \
  "GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKER_IDLE_SEC=1 GOPI_WORKER_MAX_ROUNDS=1" \
  -- ./bin/gopi worker

entry 040 env-worker-idle-exits "Distributed task ledger" GOPI_WORKER_IDLE_EXITS "3（0 = 永不退出）" \
  "cmd/agent/cli_tasks_worker.go:647（唯一 env 读者；常量 :127）" \
  "GOPI_WORKER_IDLE_EXITS=<N> 是 worker 连续 N 个空队列轮后自退" yes \
  "空队列时不要让 worker 无限守着：连续几轮没活就自己退出" \
  "结论是 arm A 的 banner \`self-exits after 3 idle round(s)\` 与 arm B 的 \`self-exits after 1 idle round(s)\`，随后 B 打出 \`1 idle round(s) with no new task — exiting 0\`。" \
  "框住 B 段的 \`exiting 0\`：同一条空队列、同一个节拍，只有自退门限从 3 改成 1——退出决策的**那一轮**就是这两臂的全部差别。" \
  "exiting 0:arm B：门限 1 轮立刻自退" \
  "GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKER_IDLE_SEC=1" \
  "GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKER_IDLE_SEC=1 GOPI_WORKER_IDLE_EXITS=1" \
  -- ./bin/gopi worker

entry 041 env-worktree-sweep-min-age-h "Distributed task ledger" GOPI_WORKTREE_SWEEP_MIN_AGE_H "24（小时）" \
  "internal/tools/worktree_sweep.go:430（唯一 env 读者；常量名 config/settings.go:37）" \
  "GOPI_WORKTREE_SWEEP_MIN_AGE_H=<小时> 是回收 worktree 注册的年龄门槛" yes \
  "给「自动清理别人工作目录」这件事一个安全边界：太年轻的登记绝不动" \
  "结论是 arm B 的 stderr 行 \`sweep-worktrees: GOPI_WORKTREE_SWEEP_MIN_AGE_H=\"0\" is not a positive hour count — using the default 24h0m0s\`——非正值被**明确报出来**（对比 020 的静默）。" \
  "框住 B 段那行 \`is not a positive hour count\`：坏值没有被接受、也没有被静默吃掉，它被点名并把默认值留在原地。" \
  "not a positive hour count:坏值被点名并回退默认（arm B）" \
  "GOPI_ALLOW_AGENT_SPAWN=1" \
  "GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKTREE_SWEEP_MIN_AGE_H=0" \
  -- ./bin/gopi task sweep-worktrees --dry-run

entry 042 env-worktree-sweep-max "Distributed task ledger" GOPI_WORKTREE_SWEEP_MAX "50（条）" \
  "internal/tools/worktree_sweep.go:440（唯一 env 读者；常量名 config/settings.go:40）" \
  "GOPI_WORKTREE_SWEEP_MAX=<N> 是一次 sweep 最多回收几条登记（爆炸半径节流）" yes \
  "让一次自动清扫最多动 N 条：突然发现一百条陈旧登记时，它只咬有限的一口" \
  "结论是 arm B 的 stderr 行 \`sweep-worktrees: GOPI_WORKTREE_SWEEP_MAX=\"0\" is not a positive count — using the default 50\`。" \
  "框住 B 段那行 \`is not a positive count\`：与 MIN_AGE_H 同族的坏值回退——两个节流旋钮的坏值处理方式一致。" \
  "not a positive count:坏值被点名并回退默认 50（arm B）" \
  "GOPI_ALLOW_AGENT_SPAWN=1" \
  "GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKTREE_SWEEP_MAX=0" \
  -- ./bin/gopi task sweep-worktrees --dry-run

entry 043 env-heartbeat-stale "Agent runtime" GOPI_HEARTBEAT_STALE "3（轮）" \
  "internal/agent/loop.go:3148（唯一 env 读者）；默认值常量 heartbeat.go:12" \
  "GOPI_HEARTBEAT_STALE=<N> 是「连续 N 个未变轮次」后心跳触发提醒的门限" yes \
  "决定 agent 停顿时多快被提醒一次（与间隔配对的另一半）" \
  "结论是**两段没有差异**：同一个变量的两侧（1 轮 vs 999 轮）摆在一起，输出逐字节相同（\`diff\` 为 0 行）——它连同 023 一起构成「心跳面在本批里没有任何可观测出口」（见 05-finding.md）。" \
  "框住 A 段第一行的 \`[gopi] loaded GOPI.md\`：两臂共有的第一行，内容也相同——「没有差异」这条结论只能用这个形状表达。" \
  "loaded GOPI.md:两臂逐字节相同（无差异=结论）" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_HEARTBEAT_SEC=1 GOPI_HEARTBEAT_STALE=1" \
  "ANTHROPIC_BASE_URL=$DEAD_ENDPOINT GOPI_HEARTBEAT_SEC=1 GOPI_HEARTBEAT_STALE=999" \
  -- ./bin/gopi "hi"

# ── 05-finding.md（每条一段；真缝写在前面，诚实空缝写在后面） ────────────────

finding 030-env-node-role-client <<'FINDING_EOF'
## 真缝：角色横幅是「每行都是结论、却没有一行是总览」

**实测。** 本条目两臂的唯一差别是 `GOPI_NODE_ROLE`。arm B 打出：

```
[gopi] node: zpeer (client)
```

arm A（不设）里 `grep -c -- 'node: zpeer'` = 0 —— 也就是说**不声明角色的节点在启动横幅里一个字都不说**。
一个既不是 server 也不是 client 的节点（未声明）与一个「声明了但推导结果相同」的节点，在这个面上
输出完全一致；`config/node_role.go:46` 的注释说 resolve 结果被缓存，但**没有任何一行**说明
「这个角色是哪条通道决定的（flag / env / settings / 默认）」——而同一个仓库里
`config.TaskSyncEnabled`（`internal/config/task_sync_switch.go:194-204`）已经把这件事做对了：
它的横幅显式写出 `off (default — …)` / `off (declared in task_sync: false — …)` /
`off (GOPI_TASK_SYNC: false — …)`，让操作者能区分「我的声明被读到了」与「根本没人声明过」。

**为什么算缝。** 同一份 help（`gopi --help-env` 的 Distributed task ledger 段）把 `GOPI_NODE_ROLE`
作为最高优先级通道广告出来，但它的生效与否在运行时**不可分辨**（不设 = 恒等）。这不是缺失的报告，
是一条已经把成熟做法写在隔壁的横幅，自己没用上。

**可提请的改法（不是本条目要做的）**：`[gopi] node:` 行按 `task-sync` 横幅的形状补上 decided-by
（`zpeer (client, from GOPI_NODE_ROLE)` / `zpeer (server, default)`）。本条目只记录测量结果。
FINDING_EOF

finding 031-env-sync-peers <<'FINDING_EOF'
## 真缝：声明对端就能改变行为，但「声明被谁读到」不问不说

**实测（oracle 行）。** 两臂只差 `GOPI_SYNC_PEERS`：arm A `code delivery OFF (no sync peer declared …)`，
arm B `code delivery ON — role client (pushes finished local branches to the peer)`，两臂 exit 都是 0。

缝在于**同一个变量的第二个消费者完全没有回声**。`GOPI_SYNC_PEERS` 声明的是「谁是我的对端」，
它在两处被读：
1. `cmd/agent/cli_tasks_sync_pass.go:152` 的 `codeDeliveryWord()`（本条目看得见的那行）；
2. `internal/tools/task_sync_git.go` 的**角色推导**（「对端列表里没有我 ⇒ 我是 server」）。

第 2 条在本条目的输出里**一个字都没有**：arm B 说 `role client`，却说不出这个 client 是
**推导出来的**（我的名字不在 `sync_peers` 里）还是**别的通道指定的**。同一个仓库里，
`config.ResolveNodeRole` 的兄弟开关（`TaskSyncEnabled`）已经把 decided-by 写进横幅了——
这里没有。

**为什么算缝。** 一个决定「是否把分支推到对端仓库」的推导，操作者只能靠反向推理才知道结果对不对：
写错一个 alias（比如把服务器自己的名字也写进自己的 `sync_peers`），角色会**静默翻面**而横幅照样
只印两个字。定性的证据是下面这两条命令（本条目的同一次会话里实跑）：

```
$ grep -n "IsServerNode\|roleResolved\|role client" internal/tools/*.go | wc -l   # 角色推导散在多处
$ ./bin/gopi task sync-pass --once | grep -c 'role '                              # 输出里只有一行
```

**建议方向**：把角色那行补成 `role client (derived: this node's name is not in sync_peers)`。
本条目只记录测量结果。
FINDING_EOF

finding 032-env-sync-interval <<'FINDING_EOF'
## 真缝：坏值被点名，退出码却说「没事」

**实测（本目录 02-output.txt，逐字）。**

arm B（`GOPI_SYNC_INTERVAL=abc`）打出：

```
[serve] sync: GOPI_SYNC_INTERVAL: "abc" is not a non-negative integer (seconds)
```

同一臂的末行是 `exit=0`；arm A（默认，interval 不设）的末行也是 `exit=0`。也就是说：
**配置错误被报了出来，但进程的结论与没报错时完全相同。**

这条变量的代码注释把契约写得很硬 —— `cmd/agent/cli_tasks_sync.go:611-613`：
「A malformed value is an ERROR (never a silent fallback — a typo'd interval must not silently
disable or race syncs)」。它确实**没有静默**：它打了一行 stderr。但它也没有**报错**到
调用者能感知的程度：`gopi task sync-pass --once` 的退出码是 0，脚本/CI/单元文件
用 `$?` 判断这次 pass 是否健康时，看到的仍是「一切正常」。

**对比证据（同一批条目里就在隔壁）。** `027/028` 的两个 sweep 旋钮对坏值的处理是
**告警 + 回退默认**并明确说 `using the default …`；`032` 是**告警 + 把这个腿关掉**
（`interval <= 0 → return`，`:657-663`）而不说关掉了。三者对「坏值」给出三种不同的运行时后果，
而只有 `027/028` 那一种把后果说全了。

**建议方向**：坏值要么按 027/028 的形状回退到默认并说明回退到了什么，要么按「配置错误」让
pass 以非 0 退出；现在的形状是「说了，但没算数」。本条目只记录测量结果。
FINDING_EOF

finding 033-env-sync-git-interval <<'FINDING_EOF'
## 真缝：「关掉 git 腿」看起来关掉的是 ref 自动化，实际只关掉了推送腿

**实测。** arm A（默认 900s）出现 `[serve] [git-client] pushed=[…]`；arm B（`=0`）这一行整段消失。
两条臂的 `pass starting` / `pass done` 与 exit（都是 0）完全相同。

缝在**读者对 `GIT` 这个词的合理期待**与**实际语义**之间：`GOPI_SYNC_GIT_INTERVAL=0`
在 `gopi task sync-pass -h` 的正文里被描述为「the git pass interval … `<= 0` off」。但同一次 pass 里
还有**第二条**会动 git 状态的腿 —— `serveAdoptionSweep`（把对端推回的分支并进 master），
它由**另一个**变量 `GOPI_ADOPTION_INTERVAL` 单独决定（`cmd/agent/cli_tasks_adopt.go:43`）。
本批的 `034` 条目就是用同一个 pass 实跑出来的：`GOPI_SYNC_GIT_INTERVAL=0` 之后
`[adopt]` 仍然照跑，并打印自己的 `scanned=… adopted=…`。

代码**知道**这件事，而且写得理直气壮 —— `cli_tasks_adopt.go:73-88` 的注释说收养腿
「BELONGS to the code-delivery axis」，所以**故意**不看 task-row 开关。但「故意不看 task-row 开关」
是一件事，「不看 git pass 的开关」是另一件事，而**两件都没有写进任何面向操作者的文本**：
`gopi task sync-pass -h` 通篇没有提 `GOPI_ADOPTION_INTERVAL` 与 git 腿的关系。

**为什么算缝。** 一个操作者想「让自动流程今晚别碰 git」，会设 `GOPI_SYNC_GIT_INTERVAL=0`，
而收养腿仍然会在 master 上落分支 —— 这正是「一个开关的名字承诺了它管不到的范围」。

**建议方向**：把两条腿的开关关系写进 `sync-pass -h`（一行即可），或者改名让名字只说它管得到的部分。
本条目只记录测量结果。
FINDING_EOF

finding 034-env-adoption-interval <<'FINDING_EOF'
## 真缝：坏值让整条腿静默消失——而且这一条连「回退」的告警都没有

**实测（本目录 02-output.txt）。** arm A（默认节拍）出现一条 `[serve] [adopt] …`；
arm B（`GOPI_ADOPTION_INTERVAL=abc`）里 `grep -c -- 'adopt'` = **0** —— 收养腿连同它自己的
自我报告一行都没有。两臂 exit 都是 0。

这与同一批的 `027/028`（`GOPI_WORKTREE_SWEEP_MIN_AGE_H=0` / `GOPI_WORKTREE_SWEEP_MAX=0`）
形成了明确对照：那两个旋钮对坏值打

```
sweep-worktrees: GOPI_WORKTREE_SWEEP_MIN_AGE_H="0" is not a positive hour count — using the default 24h0m0s
```

也就是**点名 + 说明回退到了什么**。`serveAdoptionInterval`（`cmd/agent/cli_tasks_adopt.go:43-51`）
的坏值路径是：

```go
n, err := strconv.Atoi(raw)
if err != nil || n <= 0 {
    return 0        // ← 0 = 这条腿关掉；没有任何一行输出
}
```

于是 `abc` 与 `0`（故意的关闭）在运行时**完全等价且都无声**。这条变量的注释
（`:40-41`）只说「parse-fail or <=0 = disabled」，把它当作已声明的语义；但同一批素材里
另外两个同类旋钮的作者显然认为这类坏值**值得喊一声**。两个判据，一个仓库。

**为什么算缝。** 收益不对称：一次打字的代价是**一条跨节点代码交付腿整晚不跑**，而这在
输出里是不可见的（没有告警、退出码 0、pass 仍然打印 `pass done`）。这是 Rule 15 的
「静默失败」在配置面的形状。

**建议方向**：与 027/028 对齐 —— 坏值一律打一行 stderr 并说明回退到的值；`0`（故意关闭）
保持无声。本条目只记录测量结果。
FINDING_EOF

finding 035-env-compute-nodes <<'FINDING_EOF'
## 真缝（最重的一条）：`GOPI_COMPUTE_NODES` 的消费面没有活着的调用者

**实测。** 本条目的两臂只差 `GOPI_COMPUTE_NODES`（JSON 白名单）。arm B 多出一行
`[gopi] node: zpeer`（白名单把 role 推成了 client）；arm A 里同一串 0 次。也就是说：
**这个变量在本条目里唯一可观测的效果，是一个与「远程派发」无关的副作用**（角色推导）。

白名单本身一个字节都不打印。于是「这次运行到底有没有把活派到 hp 上」在本条目的输出里
无法回答。这不是本条目跑得不够仔细 —— 是那个消费者**在生产代码里没有调用者**：

```
$ grep -rn "runDispatchOnce(" --include=*.go . | grep -v _test
./cmd/agent/dispatch.go:446:func runDispatchOnce(opts *tasksOptions) (bool, error) {
            ← 只有定义，没有调用点

$ grep -rn "dispatchToNode(\|dispatchPickNode(\|dispatchNodeLoad(\|dispatchPushToNode(" --include=*.go . | grep -v "_test\|^./cmd/agent/dispatch.go"
（空输出 —— 这五个派发原子的调用者数 = 0）

$ ./bin/gopi task run <id> --nodes '[{"name":"hp"}]'
gopi task run: removed (epic worker-run-entry) — running a task is the worker's job now
$ ./bin/gopi task serve
serve: retired — the resident merger daemon was retired for re-development
```

`runDispatchOnce`（`cmd/agent/dispatch.go:442-446`）的注释说自己是
「one serve-tick dispatch pass (the unit/E2E seam)」——而 serve tick 已经退役、`task run` 已移除、
`worker` 只调 `runTaskRunOnce`（`cmd/agent/cli_tasks_worker.go:965`），那条腿唯一的入口就是它自己。

**为什么算缝（而且是本批最重的一条）。** `GOPI_COMPUTE_NODES` 被三个面向操作者的面同时广告：
`gopi --help-env` 的 **Remote dispatch** 段（整段只有它一个变量）、`settings.json` 的
`compute_nodes` 键、`--nodes` CLI 旗标（`cmd/agent/cli_tasks.go:1149-1154`）。它读得进来、
解析得动、合法性校验也写着（`config.ParseComputeNodesJSON`）——**只有最后一段路没有车**。
操作者按目录设好白名单，得到的结果是「什么都没发生」，并且没有任何一行告诉他不幸在哪。

**已按 Rule 21 入队 bug 单**（见本目录的 `## 入队` 段）。
**建议方向**：要么把白名单接到一条活着的腿（`worker` 的 remote 分支），要么把三个广告面
（`--help-env` 段、settings 键、`--nodes` 旗标）与该变量一起标为未接线。本条目只记录测量结果。
FINDING_EOF

finding 036-env-timeout-sec <<'FINDING_EOF'
## 这次的运行里没挖到缝（但查过什么，写在这里）

本条目对 `GOPI_TIMEOUT_SEC` 做了三方比对，**没有发现不一致**：

- **help 文案**：`gopi --help-env` 的 Agent runtime 段写「Overall agent timeout in seconds (0 = unlimited)」；
- **实际行为**：两臂的唯一差别是耗时 —— arm A（不设）`elapsed=16.2s`，arm B（`=3`）`elapsed=3.6s`，
  两臂的 `⏱` 行分别是 `15.6s` 与 `3.0s`；两臂 exit 都是 1（同一个死 endpoint，失败理由是连接被拒）；
- **代码**：`internal/agent/loop.go:3132` 的唯一读者；注释（`:3130`）逐字写着
  「Default 0 = unlimited; e.g. GOPI_TIMEOUT_SEC=300 for old 5min behavior」。

判据不是「看起来对」，而是这三样**互相不矛盾**：`0` 的语义（不限）与 arm A 的 16 秒（跑满 retries）
一致，`3` 的语义（截止）与 arm B 的 3 秒一致，而两条臂的**失败理由完全相同**（同一次 LLM 调用失败），
所以耗时差只能来自这个变量。

**附带观察到的一件事（不是缝，未入队）**：坏值 `GOPI_TIMEOUT_SEC=abc` 打
`[gopi] warning: invalid GOPI_TIMEOUT_SEC="abc", using default 0s` **并继续跑**（不中断）——
与 `032`/`034` 不同，它把「回退到了什么」说全了（`using default 0s`），
这正是本批里另外两条应该对齐的形状。
FINDING_EOF

finding 037-env-heartbeat-sec <<'FINDING_EOF'
## 真缝：`GOPI_HEARTBEAT_SEC` 被读进去了，但读进的地方没人再看

**实测。** 两臂同一次 argv、同一个死 endpoint，唯一差别是 `GOPI_HEARTBEAT_SEC`。
`02-output.txt` 的 oracle 行给出的**两臂差异行数 = 0**：逐字节相同。

这不是「这条变量在死 endpoint 下看不出来」那么简单 —— 它的读者**存在**，但值没有出口：

```
$ sed -n '3141,3150p' internal/agent/loop.go
        heartbeatInterval := defaultHeartbeatInterval
        if v := os.Getenv("GOPI_HEARTBEAT_SEC"); v != "" {
                if n, err := strconv.Atoi(v); err == nil && n > 0 {
                        heartbeatInterval = time.Duration(n) * time.Second
                }
        }
$ sed -n '3223,3224p' internal/agent/loop.go
                HeartbeatInterval:       heartbeatInterval,
                HeartbeatStaleRounds:    staleRounds,

$ grep -rn "a\.HeartbeatInterval\|a\.HeartbeatStaleRounds" --include=*.go . | grep -v _test
（空输出 —— 全树没有一处再读这两个字段）

$ grep -rn "NewHeartbeatState(" --include=*.go . | grep -v "_test\|func NewHeartbeatState"
（空输出 —— 这个类型只在测试里被构造）
```

也就是说：变量的读取链是**完整而正确**的（env → 常量默认 → 结构体字段），
链的末端是一个**没有任何生产读者的结构体字段**。`internal/agent/heartbeat.go:9` 的注释
「NewAgent() reads GOPI_HEARTBEAT_SEC and GOPI_HEARTBEAT_STALE to override」描述的是这条链的
**前半段**；后半段（谁用这个值）在今天的树上不存在。

**补充观察（同一族的第二个面）**：真正会被触发的那条「stale 提醒」用的是**另一个常量** ——
`maybeNudgeStaleTaskList(roundsSinceTaskList)`（`loop.go:4039`）的比较对象是
`maxRoundsWithoutTaskList = 3`（`loop.go:731`，硬编码常量），**不是** `GOPI_HEARTBEAT_STALE`。
所以 `GOPI_HEARTBEAT_STALE` 连「间接影响这条提醒」都做不到。

**已按 Rule 21 入队 bug 单**（见本目录的 `## 入队` 段）。
**建议方向**：要么把 `NewHeartbeatState(interval, staleRounds)` 接到 Agent 的循环体上
（让这两个字段真的生效），要么把这两个变量从 `--help-env` 的 Agent runtime 段摘掉 ——
广告一个到不了任何地方的旋钮，与文档没写它是同一类缺陷。本条目只记录测量结果。
FINDING_EOF

finding 038-env-debug <<'FINDING_EOF'
## 这次的运行里没挖到缝（但查过什么，写在这里）

本条目对 `GOPI_DEBUG` 做了三方比对，**没有发现不一致**：

- **help 文案**：`gopi --help-env` 写「Set to 1 to enable debug logging」；
- **实际行为**：arm A（`=1`）第一行 `[gopi] tmpdir: TMPDIR="/home/user/workspace/_tmp" (from the caller's TMPDIR)`；
  arm B（`=off`）里 `grep -c -- 'tmpdir: TMPDIR'` = 0，两臂 exit 都是 0。本次会话另外实跑了
  `GOPI_DEBUG=on`（有那一行）、`=no` / `=false` / `=2`（都没有），与 `01a` 的
  truthy-only 语义（`1, true, on, yes, enabled`）逐项吻合；
- **代码**：`internal/scratch/tmproot.go:906` 的 `EnvDebugEnabled` 只认
  `EnvDebugOn`("1") / "true" / "yes" / "on" / "enabled"，其余（含 `2`）一律关 —— 与实测一致。

**为什么这算「查过了」而不是「漏了」**：`2` 是一个**在别的语言里当真的**值（数字非零），
本条目的实测给出了「在这里它不算开」的确证；而 catalog 的文案（"Set to 1"）说的是充分值，
不是值域 —— 两者不冲突。

**一个观察（不是缝，未入队）**：`GOPI_DEBUG` 在树里有两个读者
（`internal/scratch.EnvDebugEnabled` 与 `internal/deepseek.DebugLogging`），
`tmproot.go:899-903` 的注释显式说明了这一点与「值相同所以两个面按构造一致」的理由 ——
这是 Rule 2 要求的那种「说清楚谁读」的写法。
FINDING_EOF

finding 039-env-worker-max-rounds <<'FINDING_EOF'
## 这次的运行里没挖到缝（但查过什么，写在这里）

本条目对 `GOPI_WORKER_MAX_ROUNDS` 做了三方比对，**没有发现不一致**：

- **help 文案**：`gopi worker -h`（与 `cmd/agent/main.go:342`）写
  「GOPI_WORKER_MAX_ROUNDS=N    rounds before exit (default 0 = unlimited)」；
- **实际行为**：arm A（默认）在空队列上打出 `queue drained (3/3 idle rounds)` 并把退出交给
  闲置轮数门限；arm B（`=1`）打出 `worker: max rounds (1) reached — exiting`，两臂 exit 都是 0；
- **代码**：`cmd/agent/cli_tasks_worker.go:679` 的唯一读者，`:82` 的常量 `workerMaxRoundsEnv`；
  退出行是 `:679` 之后的循环体自己打的。

判据是三样东西**互相不矛盾**：`0`（不限）与 arm A 「靠闲置轮数退出」一致，`1`（一轮就退）
与 arm B 立刻退出一致，而两臂的**环境只差这一个变量**（`GOPI_ALLOW_AGENT_SPAWN=1` 与
`GOPI_WORKER_IDLE_SEC=1` 两臂都有，缺了前者会被 agent-spawn guard 以 exit 3 拒绝 ——
本批探测时实测过：不设它时 `gopi worker` 的退出码是 3，输出里点名两个逃生口）。
FINDING_EOF

finding 040-env-worker-idle-exits <<'FINDING_EOF'
## 这次的运行里没挖到缝（但查过什么，写在这里）

本条目对 `GOPI_WORKER_IDLE_EXITS` 做了三方比对，**没有发现不一致**：

- **help 文案**：`gopi worker -h` / `cmd/agent/main.go:345` 写
  「GOPI_WORKER_IDLE_EXITS=N    consecutive empty rounds before the worker …（default 3；0 = wait forever）」；
- **实际行为**：两臂的 loop banner 逐字不同 —— arm A `self-exits after 3 idle round(s)`，
  arm B `self-exits after 1 idle round(s)`，并且 arm B 随即打出
  `worker: 1 idle round(s) with no new task — exiting 0`；两臂 exit 都是 0；
- **代码**：`cmd/agent/cli_tasks_worker.go:647` 的唯一读者、`:127` 的常量；`:615` 的注释
  写明了 `=0` 的显式 opt-out（「pre-fix 行为」）。

判据是三样东西**互相不矛盾**：banner 里那个数字**就是**这条变量解析出来的值（3 vs 1），
退出时机与 banner 承诺的轮数一致，退出码 0 与 help 的「空队列是 SUCCESS」一致。
**这条变量是本批里最健康的一条**：值→banner→行为→退出码四点闭合，且坏值/`0` 的语义在
`-h` 里有专门段落（同一个 `worker` 页面里，`032`/`034` 那种「说了但没落到输出」的形状没有出现）。
FINDING_EOF

finding 041-env-worktree-sweep-min-age-h <<'FINDING_EOF'
## 这次的运行里没挖到缝（但查过什么，写在这里）

本条目对 `GOPI_WORKTREE_SWEEP_MIN_AGE_H` 做了三方比对，**没有发现不一致**：

- **help 文案**：`gopi task -h` 的 `sweep-worktrees` 段落写
  「--min-age-h N (default 24, env GOPI_WORKTREE_SWEEP_MIN_AGE_H)」，并把阈值测在
  **git 自己的登记记录**上（不是目录 mtime）；
- **实际行为**：arm B（`=0`）在 stderr 打出
  `sweep-worktrees: GOPI_WORKTREE_SWEEP_MIN_AGE_H="0" is not a positive hour count — using the default 24h0m0s`，
  随后仍然按默认 24h 跑完（两臂的 `keep`/`report` 判定行相同）；两臂 exit 都是 0；
- **代码**：`internal/tools/worktree_sweep.go:430` 的 `os.Getenv(worktreeSweepMinAgeEnvName)`
  —— 变量名常量取自 `config.WorktreeSweepMinAgeEnvName`（`config/settings.go:37`），
  CLI 的 `--min-age-h` 优先于 env，坏值走 `else` 分支打警告并保留默认。

**为什么这条是本批的正面样板**（写进 finding 是因为它是判据的另一半）：坏值**被点名**，
**说明了回退到哪个值**，而且**行为与告警一致**（真的按 24h 跑了，输出里 `age=0h … keep` 的判定就是按
默认门槛给的）。本批的 `032`/`034` 是这条的两个反例。
FINDING_EOF

finding 042-env-worktree-sweep-max <<'FINDING_EOF'
## 这次的运行里没挖到缝（但查过什么，写在这里）

本条目对 `GOPI_WORKTREE_SWEEP_MAX` 做了三方比对，**没有发现不一致**：

- **help 文案**：`gopi task -h` 写「--max（default 50, env GOPI_WORKTREE_SWEEP_MAX）」，
  并把这条旋钮的定位说成**爆炸半径节流**（一次 sweep 最多回收几条）；
- **实际行为**：arm B（`=0`）打出
  `sweep-worktrees: GOPI_WORKTREE_SWEEP_MAX="0" is not a positive count — using the default 50`（本次运行出现两次，
  与 sweep 的两处判定各打一次一致）；两臂 exit 都是 0；
- **代码**：`internal/tools/worktree_sweep.go:440` 的唯一读者；变量名常量
  `config.WorktreeSweepMaxEnvName`（`config/settings.go:40`）。

**形状与 027 一致**：坏值 → 点名 → 说明回退值 → 按回退值行事。两条同族旋钮用同一个形状，
这是好消息，也是 `034`（收养节拍）应该对齐的形状。
FINDING_EOF

finding 043-env-heartbeat-stale <<'FINDING_EOF'
## 真缝：与 023 同族——`GOPI_HEARTBEAT_STALE` 的另一侧也一样到不了任何地方

**实测。** 本条目把**同一个变量**的两侧摆在一起（`=1` 轮 vs `=999` 轮）：
`02-output.txt` 的 oracle 行给出的**两臂差异行数 = 0**，逐字节相同。

这是刻意的选臂：023 用「默认 vs 1 秒」已经证明过心跳间隔无出口，本条目换一个方向
（同一个变量的极端两端）复查 —— 结果相同，说明**不是取值落在无差异区间**，
而是**这个值根本没有被消费**。代码侧的确认与 023 共用同一组命令：

```
$ grep -rn "a\.HeartbeatInterval\|a\.HeartbeatStaleRounds" --include=*.go . | grep -v _test
（空）
$ grep -rn "NewHeartbeatState(" --include=*.go . | grep -v "_test\|func NewHeartbeatState"
（空）
```

而真正被触发的那条 stale 提醒（`maybeNudgeStaleTaskList`，`internal/agent/loop.go:4039`）
拿的是**常量** `maxRoundsWithoutTaskList = 3`（`loop.go:731`），不是这个变量。

**为什么这一条要单独成立**：一个变量的「无出口」有两个可能的原因 —— 读者写错了、
或者读者写对了但下游没人接。023 只能说明「两臂输出相同」；本条目把同一变量的另一对端摆出来，
排除了「恰好那一对值等价」的解释。两条合起来才是完整证据。

**已与 023 一起按 Rule 21 入队同一张 bug 单**（见本目录的 `## 入队` 段）。
FINDING_EOF


# ── Oracle 行（每条 3 行，全部是刚跑出来的计数） ──────────────────────────────
ochead 030 env-node-role-client
oc 030 env-node-role-client A "node: zpeer" "（A 不设该变量：无角色横幅）"
oc 030 env-node-role-client B "node: zpeer" "（B 设 client：横幅出现）"
ocdiff 030 env-node-role-client

ochead 031 env-sync-peers
oc 031 env-sync-peers A "code delivery OFF" "（无对端：腿短接）"
oc 031 env-sync-peers B "code delivery ON" "（声明了 hp：腿开了）"
ocdiff 031 env-sync-peers

ochead 032 env-sync-interval
oc 032 env-sync-interval A 'not a non-negative integer' "（默认：无报错）"
oc 032 env-sync-interval B 'not a non-negative integer' "（abc：被报出来）"
ocdiff 032 env-sync-interval

ochead 033 env-sync-git-interval
oc 033 env-sync-git-interval A "git-client] pushed=" "（默认 900s：推送腿在跑）"
oc 033 env-sync-git-interval B "git-client] pushed=" "（设 0：整行消失）"
ocdiff 033 env-sync-git-interval

ochead 034 env-adoption-interval
oc 034 env-adoption-interval A "adopt]" "（默认：收养腿在跑）"
oc 034 env-adoption-interval B "adopt]" "（abc：一行都没有）"
ocdiff 034 env-adoption-interval

ochead 035 env-compute-nodes
oc 035 env-compute-nodes A "No tasks found" "（不设：正常列表）"
oc 035 env-compute-nodes B "No tasks found" "（不合法 JSON：同一行，逐字相同）"
ocdiff 035 env-compute-nodes

ochead 036 env-timeout-sec
oc 036 env-timeout-sec A "elapsed=" "（默认 0=不限）"
oc 036 env-timeout-sec B "elapsed=" "（3 秒截止）"
ocdiff 036 env-timeout-sec

ochead 037 env-heartbeat-sec
oc 037 env-heartbeat-sec A "loaded GOPI.md" "（两臂共有行）"
oc 037 env-heartbeat-sec B "loaded GOPI.md" "（同上）"
ocdiff 037 env-heartbeat-sec

ochead 038 env-debug
oc 038 env-debug A "tmpdir: TMPDIR" "（=1：可见痕迹）"
oc 038 env-debug B "tmpdir: TMPDIR" "（off：0 次）"
ocdiff 038 env-debug

ochead 039 env-worker-max-rounds
oc 039 env-worker-max-rounds A "queue drained" "（默认：靠闲置轮数退出）"
oc 039 env-worker-max-rounds B "max rounds (1) reached" "（=1：轮到就退）"
ocdiff 039 env-worker-max-rounds

ochead 040 env-worker-idle-exits
oc 040 env-worker-idle-exits A "self-exits after 3 idle" "（默认门限 3）"
oc 040 env-worker-idle-exits B "exiting 0" "（门限 1：立刻自退）"
ocdiff 040 env-worker-idle-exits

ochead 041 env-worktree-sweep-min-age-h
oc 041 env-worktree-sweep-min-age-h A "not a positive hour count" "（默认：无告警）"
oc 041 env-worktree-sweep-min-age-h B "not a positive hour count" "（=0：点名回退）"
ocdiff 041 env-worktree-sweep-min-age-h

ochead 042 env-worktree-sweep-max
oc 042 env-worktree-sweep-max A "not a positive count" "（默认：无告警）"
oc 042 env-worktree-sweep-max B "not a positive count" "（=0：点名回退）"
ocdiff 042 env-worktree-sweep-max

ochead 043 env-heartbeat-stale
oc 043 env-heartbeat-stale A "loaded GOPI.md" "（1 轮：两侧共有行）"
oc 043 env-heartbeat-stale B "loaded GOPI.md" "（999 轮：同上）"
ocdiff 043 env-heartbeat-stale

# ── 入队：真缝按 Rule 21 交工单（落**操作者真实台账**：这是派发通道，不是沙盒产物） ──
enqueue() { # enqueue <slug> <summary> <payloadfile> <text>
  # 幂等：生成器可重跑，重复重跑不得重复开单（一次发现 = 一张工单）。
  local slug="$1" sum="$2" pay="$3" text="$4" id
  # 幂等：一次发现只开一张工单。记账落在批次的**交付物**里（QUEUE.tsv，与素材
  # 一起提交、可读、可审），不是隐藏状态文件（Rule 35）：重跑读它，已在册就跳过。
  if [[ -f "$QUEUE" ]] && grep -q "^$slug	" "$QUEUE"; then
    printf 'skip    %-34s (already queued: %s)\n' "$slug" \
      "$(awk -F'\t' -v s="$slug" '$1==s{print $2}' "$QUEUE")"
    return 0
  fi
  id="$(cd "$ROOT" && env -u GOPI_HOME -u GOPI_SETTINGS "$BIN" task submit "$sum" \
        --payload "@$pay" --tags "bug,env-face" 2>&1 | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)"
  if [[ -z "$id" ]]; then echo "WARN: enqueue failed for $slug" >&2; return 0; fi
  printf '\n## 入队\n\n- 工单 **%s** —— `gopi task show %s`\n- %s\n- 证据：本目录 01/02/03/04 + 同一批的 `%s` 条目\n' \
    "$id" "$id" "$text" "${slug%%-*}" >> "$OUTROOT/$slug/05-finding.md"
  printf '%s\t%s\t%s\n' "$slug" "$id" "$TODAY" >> "$QUEUE"
  printf 'queued %-34s %s\n' "$slug" "$id"
}

cat > "$W/bug-compute-nodes.md" <<'BUG_EOF'
[Rule 21 finding — 广告了但到不了任何地方的旋钮]

`GOPI_COMPUTE_NODES` 被三个面向操作者的面同时广告：
  * `gopi --help-env` 的 **Remote dispatch** 段（整段只有它一个变量）
  * settings.json 的 `compute_nodes` 键
  * `--nodes` CLI 旗标（cmd/agent/cli_tasks.go:1149-1154，经 config.ParseComputeNodesJSON → ResolveComputeNodes）

但它解析出来的白名单在生产代码里**没有消费者**：
  $ grep -rn "runDispatchOnce(" --include=*.go . | grep -v _test
    cmd/agent/dispatch.go:446:func runDispatchOnce(opts *tasksOptions) (bool, error) {   ← 只有定义，调用点数 = 0
  $ grep -rn "dispatchToNode(\|dispatchPickNode(\|dispatchNodeLoad(\|dispatchPushToNode(" --include=*.go . | grep -v _test
    （空）
  入口那侧的三个面也都已经不接它：
    `gopi task run <id> --nodes …` → "gopi task run: removed (epic worker-run-entry)"
    `gopi task serve`            → "serve: retired — the resident merger daemon was retired for re-development"
  `runDispatchOnce` 的注释自称是 "one serve-tick dispatch pass (the unit/E2E seam)"，而 serve tick 已退役、
  worker 只调 runTaskRunOnce（cmd/agent/cli_tasks_worker.go:965）——那条腿唯一的入口就是它自己。

实测（本批 035-env-compute-nodes 条目）：两臂只差这一个变量，唯一可观测的差异是一个**副作用**
（启动横幅 `[gopi] node: zpeer` —— 白名单把 role 推成了 client），白名单本身一个字节都不打印；
"这次到底派没派活" 在输出里无法回答。

DONE 的定义（三选一，由执行者判断）：
  (a) 把白名单接到一条活着的腿（worker 的 remote 分支 / 重建 serve tick）；
  (b) 若远程派发确已退役，则把三个广告面一起标为未接线（--help-env 段落、settings 键、--nodes 旗标），
      并让 `--nodes` 给出与 `task run` 同形的 "removed" 提示；
  (c) 若保留解析但明确不接线，至少让读到非空白名单时打一行 "declared, no consumer（见 <此处>）"，不要沉默。
复现：bash scripts/materials/gen-env-E1.sh（035-env-compute-nodes/ 六件套，含 01/02 的 verbatim 前后对照）。
BUG_EOF

cat > "$W/bug-heartbeat.md" <<'BUG_EOF'
[Rule 21 finding — 读取链完整、末端没有任何生产读者]

`GOPI_HEARTBEAT_SEC` 与 `GOPI_HEARTBEAT_STALE` 在 `gopi --help-env` 的 **Agent runtime** 段被广告，
读取链也写得完整正确（internal/agent/loop.go:3141-3149）：

        heartbeatInterval := defaultHeartbeatInterval
        if v := os.Getenv("GOPI_HEARTBEAT_SEC"); v != "" { … }
        …  HeartbeatInterval: heartbeatInterval, HeartbeatStaleRounds: staleRounds,   (loop.go:3223-3224)

但链的末端**没有任何生产读者**：
  $ grep -rn "a\.HeartbeatInterval\|a\.HeartbeatStaleRounds" --include=*.go . | grep -v _test
    （空输出）
  $ grep -rn "NewHeartbeatState(" --include=*.go . | grep -v "_test\|func NewHeartbeatState"
    （空输出）—— internal/agent/heartbeat.go 的类型只在测试里被构造

而真正会被触发的 "stale" 提醒走的是**另一个常量**：maybeNudgeStaleTaskList(roundsSinceTaskList)
（loop.go:4039）的比较对象是 maxRoundsWithoutTaskList = 3（loop.go:731，硬编码）。
所以 GOPI_HEARTBEAT_STALE 连间接影响这条提醒都做不到；GOPI_HEARTBEAT_SEC 同理。

实测（本批 037-env-heartbeat-sec 与 043-env-heartbeat-stale 两个条目）：
  * 023：只把间隔改成 1 秒 → 与默认臂逐字节相同（差异行数 0）
  * 029：把**同一个变量**的两端（1 轮 vs 999 轮）摆在一起 → 同样逐字节相同（差异行数 0）
两条合起来排除了 "恰好那一对值等价"，指向 "这个值没有被消费"。

DONE 的定义（二选一）：
  (a) 把 NewHeartbeatState(interval, staleRounds) 接到 Agent 的轮循环上（让两个字段真的生效，
      并按本仓既有形状把 "第几轮触发/门限多少" 打进调试输出，让规则可观测）；
  (b) 若心跳机制已被 maybeNudgeStaleTaskList 取代，则从 --help-env 的 Agent runtime 段摘掉这两个变量，
      并把 loop.go:3141-3149 的死接线一并删除（不留 "读了但没人用" 的中间态）。
复现：bash scripts/materials/gen-env-E1.sh（023/ 与 029/ 六件套）。
BUG_EOF

enqueue "035-env-compute-nodes" \
  "BUG(env-face:035-env-compute-nodes): GOPI_COMPUTE_NODES 被三个广告面声明，但白名单在生产代码里没有消费者（runDispatchOnce 调用点数=0）" \
  "$W/bug-compute-nodes.md" \
  "三个广告面（--help-env Remote dispatch 段 / settings compute_nodes / --nodes 旗标）都指向一个没有调用者的消费者。"

enqueue "037-env-heartbeat-sec" \
  "BUG(env-face:037-env-heartbeat-sec): GOPI_HEARTBEAT_SEC / GOPI_HEARTBEAT_STALE 读取链完整但末端无生产读者（NewHeartbeatState 无人构造）" \
  "$W/bug-heartbeat.md" \
  "读取链 env→字段 正确，字段无人读；真正触发的 stale 提醒用的是硬编码常量 maxRoundsWithoutTaskList=3。"

echo "== index =="
cat "$W/index.tsv"
if [[ -s "$W/failed.txt" ]]; then
  echo "== FAILED ==" >&2; cat "$W/failed.txt" >&2
  echo "GENERATOR FAILED" >&2; exit 1
fi
printf 'WORK=%s\nSANDBOX=%s\n' "$W" "$SB"
