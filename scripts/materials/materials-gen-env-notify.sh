#!/usr/bin/env bash
# materials-gen-env-notify.sh — E 面（环境变量面）GOPI_TASK_NOTIFY_* 子探测的素材生成器。
#
# shellcheck disable=SC2016
# GENERATOR: 本文件（证据目录里的这一份）。
# 形状遵守 docs/archive/demo/materials/README.md（M0 约定）：
#   * 目录名 <NNN>-<slug>，三位补零；面内唯一、不复用（本脚本**硬断言不撞车**）；
#   * 六件套固定名；02-output.txt 必须含一行 ^exit=[0-9]+；
#   * 00-entry.md 必填 face / feature / annotation_required / measured_commit /
#     date / how_to_replay + 三节。
#
# 本面（env）的条目多了两条本面自己的纪律，因为**环境变量的证据就是"两次运行的差"**：
#   1. 每条必须给出**真实 reader 的 file:line**（发出去的路径 = 真 argv），
#      grep 可核对；找不到 reader 的旋钮按 Rule 21 记 finding 并入 bug 队列。
#   2. 至少一个变量给出**改前/改后两次真实运行的逐字输出**，差异必须**在证据文件里
#      看得见**（不是旁白说"变了"）。
#
# 隔离（Rule 34 + 本次实测的坑）：所有探测都在私有沙盒 profile 里跑，
#   并且把 GOPI_SETTINGS 也**钉在沙盒内**——GOPI_HOME 只管台账与 home，
#   settings.json 由 GOPI_SETTINGS 单独决定（internal/config/config.go:862）。
#   只钉 GOPI_HOME 的话，settings 层的 notify.cmd 会把"沙盒实验"投递进
#   操作者的真台账（本次实测发生过一次，见 007 条目）。本脚本因此在最前面
#   自证：沙盒 settings 里不许有 notify 段。
#
# 用法: materials-gen-env-notify.sh [out_root]
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="$ROOT/bin/gopi"
RENDER="$ROOT/scripts/materials/render.py"
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:-$ROOT/docs/archive/demo/materials}"
FACE=env
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"

# 非分支证据（日志 / webhook 抓包 / census 落盘）只写 GOPI_HOME 的会话目录。
EVID="${GOPI_SESSION_DIR:-/home/user/workspace/gopi-sandbox/sandbox-enve1-7fe4b684/.gopi/sessions/01a0ed89-b98c-73b8-b15d-ac0d32329510}/materials-env-notify"
mkdir -p "$EVID" || exit 1

# webhook 接收端（零依赖：python3 标准库；一次 POST 追加一行 body）
cat > "$EVID/wh.py" <<'PY'
#!/usr/bin/env python3
"""notify 探测的 webhook sink：收 POST、把 body 追加进文件、回 200。"""
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


class H(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(n)
        with open(sys.argv[2], "ab") as f:
            f.write(body + b"\n")
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *a):
        pass


HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY

# --- 私有沙盒 profile（Rule 34：ASK for a sandbox，never invent a directory） --
SB="${GOPI_ENV_NOTIFY_SANDBOX:-/home/user/workspace/gopi-sandbox/sandbox-env-notify-probe-3f7837c4}"
if [[ ! -d "$SB/.gopi" ]]; then
  SB="$("$BIN" tool sandbox create --label "env-notify-probe" \
        --task-id 01a0ed89-b98c-73b8-b15d-ac0d32329510 2>/dev/null | tail -1)"
fi
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }

# --- 沙盒自己的 settings：去掉 notify 段（两条纪律） ---------------------------
# 纪律一：探测必须以"通知默认关闭"为基线，否则改值前后的差是 settings 的差，不是 env 的差。
# 纪律二：沙盒 profile 的 settings.json 是操作者 settings 的副本，其中的 notify.cmd
#         指向操作者真台账——留着它就等于把沙盒实验投递进操作者台账。
OFF="$EVID/settings-off.json"
python3 - "$SB/.gopi/settings.json" "$OFF" <<'PY' || exit 1
import json, sys
src, dst = sys.argv[1], sys.argv[2]
with open(src) as f:
    cfg = json.load(f)
cfg.pop("notify", None)                      # 基线：通知关闭 + 不碰操作者台账
with open(dst, "w") as f:
    json.dump(cfg, f, ensure_ascii=False, indent=2)
PY
export GOPI_HOME="$SB" GOPI_SETTINGS="$OFF"
# 自证（Rule 34 的隔离要么成立要么响亮地不成立）：setting 层不许有 notify 段。
if "$BIN" config show 2>/dev/null | grep -qE '^\s+notify\s+\{'; then
  echo "FATAL: the probe settings still carry a notify section — the isolation is broken" >&2
  exit 1
fi
echo "sandbox=$SB"
echo "settings=$OFF"
echo "measured_commit=$COMMIT"
echo "evidence=$EVID"

WALL_B=""; WALL_A=""   # 占位符的顶层安全网（由 entry() 内部从 02-output.txt 回填）
W="$EVID/work"; mkdir -p "$W"
rm -f "$W"/*.txt "$W"/*.jsonl "$W"/*.json "$W"/*.log 2>/dev/null
: > "$W/index.tsv"

# --- fixture 行（全部落在沙盒台账里） -----------------------------------------
mkid() { grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1; }
sub() { (cd "$ROOT" && "$BIN" task submit "$@" 2>&1 | mkid); }

# entry <n> <slug> <feature> <ann_req> <solves> <readw> <caption> <marks> <code_ref> <diff_note> <runs_file>
#   runs_file: 每行 = 一次真跑，格式 `<BEFORE|AFTER>|<label>|<shell 片段>`（片段在 $ROOT 下、带完整 env 跑）
entry() {
  local n="$1" slug="$2" feature="$3" ann="$4" solves="$5" readw="$6" caption="$7" marks="$8" coderef="$9" diffnote="${10}" runsfile="${11}" viewgrep="${12:-}"
  local dir="$OUTROOT/$n-$slug"
  # 序号复用 = 把"第 N 条"的含义弄漂移（README §4）。本面新建目录前先拦一次。
  if [[ -e "$dir" && -n "${GOPI_MATERIALS_ALLOW_OVERWRITE:-}" ]]; then
    :
  elif [[ -e "$dir" ]]; then
    echo "FATAL: $dir already exists — 序号复用会把这一条的含义弄漂移（README §4：序号只增不减）" >&2
    return 1
  fi
  mkdir -p "$dir"
  # 六件套以外的残渣一律清掉（text2img 多页会留下 03-raw-1.png 这类）
  find "$dir" -maxdepth 1 -type f ! -name '0[0-9]-*' -delete 2>/dev/null

  # 01-input.txt —— 每条 = 两次真跑的逐字 argv（改前/改后）
  {
    printf 'cd %s\n' "$ROOT"
    printf 'SB=%s\n' "$SB"
    printf '# 两次运行的 env 完全相同，只有被测变量的值不同 —— 差异才归因得清\n'
    printf '# settings-off=%s （沙盒 settings 副本，已去掉 notify 段）\n' "$OFF"
    local ln kind label frag
    while IFS='|' read -r kind label frag; do
      [[ -n "${kind:-}" ]] || continue
      printf '\n### %s — %s\n' "$kind" "$label"
      printf 'GOPI_HOME=%s \\\nGOPI_SETTINGS=%s \\\n  %s\n' "$SB" "$OFF" "$frag"
    done < "$runsfile"
  } > "$dir/01-input.txt"

  # 02-output.txt —— stream A 全部跑完再跑 stream B，所以 AFTER 里能看见 BEFORE 没有的行。
  # 每一次的退出码逐次记下来（run-exit=），末尾那行裸 exit= 是**最后一次真跑**的退出码
  # —— 不是写死的 0：census 认的是这一行，写死就等于把"跑过"与"跑成功"重新混成一件事。
  local out="" rc_last=0 kind label frag
  while IFS='|' read -r kind label frag; do
    [[ -n "${kind:-}" ]] || continue
    local r rc
    r="$( (cd "$ROOT" && eval "GOPI_HOME=$SB GOPI_SETTINGS=$OFF $frag") 2>&1 )"
    rc=$?
    rc_last=$rc
    out="$out$r
($kind) run-exit=$rc
"
  done < "$runsfile"
  { printf '%s' "$out"; printf 'exit=%d\n' "$rc_last"; } > "$dir/02-output.txt"

  # 回填 \$WALL_B / \$WALL_A：从刚生成的 02-output.txt 里读回来，而不是凭记忆写数字。
  # 一次真跑的 wall 秒数每次都不同（本次 2.22s / 0.64s），正文里写死的数字迟早会和
  # 它自己引用的那份证据打架 —— 那正是本面 001 finding 的病（引用与真值漂移）。
  local WALL_B WALL_A
  WALL_B="$(grep -B3 '^(BEFORE)' "$dir/02-output.txt" | grep -oE 'wall=[0-9.]+ s' | head -1 | sed 's/wall=//')"
  WALL_A="$(grep -B3 '^(AFTER)'  "$dir/02-output.txt" | grep -oE 'wall=[0-9.]+ s' | head -1 | sed 's/wall=//')"
  if [[ -z "$WALL_B$WALL_A" ]]; then
    # 本条目不声明 wall 占位符时允许为空；声明了就必须回填成功
    case "$readw$caption$diffnote" in
      *'$WALL_'*) echo "FATAL: entry $n 声明了 wall 占位符但 02-output.txt 里读不到 wall=" >&2; return 1 ;;
    esac
  fi
  caption="${caption//\$WALL_B/$WALL_B}"; caption="${caption//\$WALL_A/$WALL_A}"
  readw="${readw//\$WALL_B/$WALL_B}";     readw="${readw//\$WALL_A/$WALL_A}"
  diffnote="${diffnote//\$WALL_B/$WALL_B}"; diffnote="${diffnote//\$WALL_A/$WALL_A}"

  # 00-entry.md —— 素材卡
  {
    printf '# %s-%s — %s\n\n' "$n" "$slug" "$solves"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- feature: `%s`\n' "$feature"
    printf -- '- annotation_required: %s\n' "$ann"
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- how_to_replay: `bash scripts/materials/materials-gen-env-notify.sh <out>` 重跑整条 notify 子探测；单条按 `01-input.txt` 逐字执行\n'
    printf -- '- sandbox_profile: `%s`（私有；操作者台账从未被读写）\n' "$SB"
    printf -- '- code_reader: %s\n\n' "$coderef"
    printf '## 原始输入从哪来\n\n'
    printf '`01-input.txt` 是**两次真跑**的逐字 argv（改前 / 改后），两次的 env 完全相同、\n'
    printf '只有被测变量的值不同。沙盒 profile 与其中被去掉 notify 段的 settings 副本都在里面。\n\n'
    printf '## 输出怎么读\n\n'
    printf '`02-output.txt` 是两次运行的 stdout+stderr 合并后的**逐字节**记录；末行 `exit=0`\n'
    printf '（裸形式，census 的 output 谓词认这个拼写）。%s\n\n' "$readw"
    printf '## 标注图在指什么\n\n'
    printf '%s\n\n' "$caption"
    printf '## 改前 / 改后差在哪\n\n'
    printf '%s\n' "$diffnote"
  } > "$dir/00-entry.md"

  # 03-raw.png / 04-annotated.png（复用 A 面的渲染器：它是像素真值的，不是猜坐标的）
  local mkargs=() m
  while IFS= read -r m; do
    [[ -n "$m" ]] && mkargs+=(--mark-match "$m")
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g')
  if [[ -n "$marks" && ${#mkargs[@]} -eq 0 ]]; then
    echo "FATAL: entry $n declared marks but parsed none — $marks" >&2
    return 1
  fi
  {
    printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$caption"
    printf '── 01-input.txt（verbatim 命令，两次真跑）──────────────────────────\n'
    cat "$dir/01-input.txt"
    if [[ -n "$viewgrep" ]]; then
      # 输出太长（002 是 166 行、003 是 731 行的 help dump）会淹没掉唯一有信息量的那几行，
      # 而 text2img 一页也画不下——两者都指向同一个动作：**摘录**。
      # 摘录是逐字节的原文，每行前面只加一个它在全文里的行号（Lnnn:），
      # 读者可以拿这个号去同目录 02-output.txt 里对；完整输出永远在同目录。
      printf '\n── 02-output.txt（verbatim 摘录：本次改前/改后的差异落在下面这些行上）──\n'
      grep -nE "$viewgrep" "$dir/02-output.txt" | sed -E 's/^([0-9]+):/L\1: /'
      printf '──────\n（图是全文的一扇窗，不是替代品：02-output.txt 共 %s 行，逐字节完整。）\n' \
        "$(wc -l < "$dir/02-output.txt")"
    else
      printf '\n── 02-output.txt（verbatim 输出，末行是 exit code）──────────────────\n'
      cat "$dir/02-output.txt"
    fi
  } | python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --json-out "$W/$slug.json" "${mkargs[@]}" \
        > "$W/$slug.render.log" 2>&1
  local rrc=$?

  printf '%s\t%s\t%s\t%s\t%s\n' "$n" "$slug" "$rc" "$rrc" \
     "$(stat -c %s "$dir/04-annotated.png" 2>/dev/null || echo 0)" >> "$W/index.tsv"
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $n-$slug 的标注图没产出（render=$rrc）—— 见 $W/$slug.render.log" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
  fi
  printf 'entry %-28s render=%-3s marks=%d\n' "$n-$slug" "$rrc" "${#mkargs[@]}"
}

# finding <dir> ：正文从 stdin 读（heredoc）——长正文里的引号/反引号不跟 shell 打架。
finding() { cat > "$OUTROOT/$1/05-finding.md"; }

echo "== entries =="

# 每个 fixture 行在**两个 stream**里各出现一次：stream A（改前）与 stream B（改后）。
# 这样 AFTER 那半段里出现的 [task-notify] 行，在 BEFORE 那半段里必然缺席——
# 差异是"在同一个文件里看得见"的，不是靠旁白声称的。
mk_runs() {  # mk_runs <file> <before-frag> <after-frag>；片段里的 $BIN 在这里展开成绝对路径
  cat > "$1" <<EOF
BEFORE|通知关闭（无 notify env，沙盒 settings 已去掉 notify 段）|$2
AFTER|打开被测旋钮|$3
EOF
}

# --- 001: GOPI_TASK_NOTIFY_CMD（默认值 = 空 = 不投递） ------------------------
ID_C1="$(sub "env-notify A: cmd 默认关闭" --payload p --repo "$ROOT")"
ID_C2="$(sub "env-notify A: cmd 打开" --payload p --repo "$ROOT")"
CAP1="$W/cmd-out.jsonl"
mk_runs "$W/runs-cmd" \
  "$BIN task update $ID_C1 --status in_progress" \
  "GOPI_TASK_NOTIFY_CMD='cat >> $CAP1' $BIN task update $ID_C2 --status in_progress"
entry 001 notify-cmd-default-off \
  "GOPI_TASK_NOTIFY_CMD（默认 = 空 = 不投递）" yes \
  "这条旋钮回答一句话：任务行状态一变，要不要顺手跑一条你自己的命令——默认是不跑" \
  "结论是改后那半段里的 \`[task-notify] cmd ok\` 行，以及改前那半段里**没有**它；两次的 \`Task updated:\` 行都有，说明写台账本身与通知无关。" \
  "框住 AFTER 段的 \`[task-notify] cmd ok\`：这一行只出现在改后的一半，它是"通知真的发出去过"的唯一机器可读痕迹；上面 BEFORE 段同样的 status 跃迁里没有这一行。" \
  "task-notify] cmd ok:只有改后那半段有这一行" \
  "internal/config/config.go:630（读 env）/ internal/tools/tasknotify.go:512（taskNotifyCmd：sh -c 执行，payload 走 stdin）" \
  "改前：\`in_progress\` 跃迁只打印 \`Task updated:\` 一行，没有任何 \`[task-notify]\` 输出。
改后：多出一行 \`[task-notify] cmd ok (stdout: )\`，并且抓包文件 \`cmd-out.jsonl\` 里出现了那次跃迁的 JSON payload（task_id 就是改后那一行）。" \
  "$W/runs-cmd"

# --- 002: GOPI_TASK_NOTIFY_WEBHOOK（默认值 = 空 = 不投递） --------------------
ID_W1="$(sub "env-notify B: webhook 关闭" --payload p --repo "$ROOT")"
ID_W2="$(sub "env-notify B: webhook 打开" --payload p --repo "$ROOT")"
PORT=18099
CAPW="$W/webhook-captured.jsonl"; : > "$CAPW"
python3 "$EVID/wh.py" "$PORT" "$CAPW" > "$W/wh.log" 2>&1 &
WHPID=$!
sleep 1
mk_runs "$W/runs-webhook" \
  "$BIN task update $ID_W1 --status in_progress" \
  "GOPI_TASK_NOTIFY_WEBHOOK=http://127.0.0.1:$PORT/hook $BIN task update $ID_W2 --status in_progress"
entry 002 notify-webhook-default-off \
  "GOPI_TASK_NOTIFY_WEBHOOK（默认 = 空 = 不投递）" yes \
  "任务流转时把一行 JSON POST 到你自己的 HTTP 端点——不配就不发，配了就把整条的正文送过去" \
  "结论是改后那半段里的 \`webhook …: ok (HTTP 200)\`，以及抓包文件里的 payload 正文。" \
  "框住 AFTER 段的 \`webhook http://127.0.0.1:18099/hook: ok (HTTP 200)\`：HTTP 状态码是这条旋钮的**真值**——\`ok\` 三个字谁都会打印，200 是接收端给的。" \
  "HTTP 200:接收端给的 HTTP 状态码才是一条 POST 的真值" \
  "internal/config/config.go:627（读 env）/ internal/tools/tasknotify.go:495（http.NewRequestWithContext 的 POST）+ :59（taskNotifyWebhookTimeout = 10s）" \
  "改前：没有任何 \`[task-notify]\` 行，抓包文件为空。
改后：\`[task-notify] webhook http://127.0.0.1:18099/hook: ok (HTTP 200)\`，且 \`webhook-captured.jsonl\` 里有一行真 JSON，字段含 \`\"event\":\"in_progress\"\` 与改后那一行的 task_id。" \
  "$W/runs-webhook" 'task-notify|^\(BEFORE\)|^\(AFTER\)|run-exit='
kill "$WHPID" 2>/dev/null

# --- 003: GOPI_TASK_NOTIFY_EVENTS（默认值 = claimed,in_progress,completed,failed）
ID_E1="$(sub "env-notify C: events 默认集" --payload p --repo "$ROOT")"
ID_E2="$(sub "env-notify C: events=all" --payload p --repo "$ROOT")"
CAP3="$W/events-allowlist.jsonl"
mk_runs "$W/runs-events" \
  "GOPI_TASK_NOTIFY_CMD='cat >> $CAP3' $BIN task update $ID_E1 --status cancelled --reason '素材演示：默认集里没有 cancelled'" \
  "GOPI_TASK_NOTIFY_EVENTS=all GOPI_TASK_NOTIFY_CMD='cat >> $CAP3' $BIN task update $ID_E2 --status cancelled --reason '素材演示：events=all 放行 cancelled'"
entry 003 notify-events-allowlist \
  "GOPI_TASK_NOTIFY_EVENTS（默认 claimed,in_progress,completed,failed）" yes \
  "哪些流转值得吵你：默认只报四个常用状态，\`cancelled\`/\`pending\` 这类静默，除非你说 all" \
  "结论是改前那半段的 \`event \"cancelled\" … not in the events allow-list\` 与改后那半段的 \`cmd ok\`：同一状态、同一目标，只有 allow-list 变了。" \
  "框住 BEFORE 段的 \`not in the events allow-list\`：这一行是"滤波器把事件挡住了"的自白，它还逐字引用了操作者自己写的那个列表（\`claimed,in_progress,completed,failed\`）——把默认值印在了证据上。" \
  "not in the events:滤波器挡住了事件，并逐字引用操作者自己的列表" \
  "internal/tools/tasknotify.go:66（默认常量）/ internal/tools/tasknotify.go:259（events 为空则回落到 taskNotifyEventsDefault）/ :420（不在集内则 drop，并打印这条诊断）" \
  "改前：\`[task-notify] event \"cancelled\" … not in the events allow-list …\`，抓包文件**没有**新行。
改后：\`[task-notify] cmd ok (stdout: )\`，抓包文件多一行 \`\"event\":\"cancelled\"\`。" \
  "$W/runs-events" 'allow-list|^\(BEFORE\)|^\(AFTER\)|run-exit='

# --- 004: GOPI_TASK_NOTIFY_PATTERN（默认值 = 空 = 广播） ----------------------
ID_P1="$(sub "env-notify D: pattern 广播" --payload p --repo "$ROOT")"
ID_P2="$(sub "env-notify D: pattern 过滤" --payload p --repo "$ROOT")"
mk_runs "$W/runs-pattern" \
  "GOPI_TASK_NOTIFY_CMD='cat >> $W/pattern.jsonl' $BIN task update $ID_P1 --status in_progress" \
  "GOPI_TASK_NOTIFY_PATTERN='zzz*' GOPI_TASK_NOTIFY_CMD='cat >> $W/pattern.jsonl' $BIN task update $ID_P2 --status in_progress"
entry 004 notify-pattern-glob \
  "GOPI_TASK_NOTIFY_PATTERN（默认 = 空 = 广播）" yes \
  "按任务 id / 标签挑人：空着就是全都报，写个 glob 就只报匹配的行" \
  "结论是改前那半段的 \`cmd ok\` 与改后那半段的 \`filtered by pattern\`：同一个状态、同一个目标，只有 glob 变了。" \
  "框住 AFTER 段的 \`filtered by pattern \"zzz*\"\`：它把**操作者自己写的那个 glob 逐字引回来**——被过滤这件事因此可复核，而不是一句"没发"。" \
  "filtered by pattern:被挡住时把操作者自己的 glob 逐字引回来" \
  "internal/tools/tasknotify.go:322（taskNotifyPatternMatch：空 = 广播）/ :332（filepath.Match，非法 glob 一律不匹配）/ :425（命中不了则 drop）" \
  "改前：\`[task-notify] cmd ok (stdout: )\`（广播命中）。
改后：\`[task-notify] event \"in_progress\" … filtered by pattern \"zzz*\" — not delivered\`，抓包文件**没有**新行。" \
  "$W/runs-pattern"

# --- 005: GOPI_TASK_NOTIFY_ASYNC（默认值 = false = 阻塞投递） -----------------
ID_A1="$(sub "env-notify E: async=false" --payload p --repo "$ROOT")"
ID_A2="$(sub "env-notify E: async=1" --payload p --repo "$ROOT")"
LAND="$W/land-by-async-child.txt"; rm -f "$LAND"
mk_runs "$W/runs-async" \
  "GOPI_TASK_NOTIFY_CMD='sleep 2' /usr/bin/time -f 'wall=%e s' $BIN task update $ID_A1 --status in_progress" \
  "GOPI_TASK_NOTIFY_ASYNC=1 GOPI_TASK_NOTIFY_CMD='sleep 2; echo 投递完成了 > $LAND' /usr/bin/time -f 'wall=%e s' $BIN task update $ID_A2 --status in_progress; sleep 3; echo '3 秒后 detached 子进程写下的文件:' $LAND; cat $LAND 2>/dev/null"
entry 005 notify-async-fire-and-forget \
  "GOPI_TASK_NOTIFY_ASYNC（默认 = false = 阻塞）" yes \
  "你的通知目标慢的时候，要不要拖着写台账的那一行一起等：默认等，设 1 就不等" \
  "结论是两半段各自的 \`wall=\` 秒数：同一个 \`sleep 2\` 命令，阻塞那一半把 CLI 拖到 \$WALL_B，async 那一半 \$WALL_A —— 这两个数字是从本条目自己的 02-output.txt 回填的，不是凭记忆写的。" \
  "框住 BEFORE 段的 \`wall=$WALL_B\` 与 AFTER 段末尾那个文件名：**同一个 sleep 2**，一个把 CLI 拖住、一个不拖，而"不拖"不等于"没发"——AFTER 段末尾那个文件（名字里带 land-by-async-child）就是 detached 子进程后来自己写下的。" \
  "wall=2:阻塞的一半把 CLI 拖住在那条 sleep 上;;投递完成了:async 的投递在 CLI 退出之后自己落地" \
  "internal/config/config.go:639（读 env）/ internal/tools/tasknotify.go:457（async 则 go deliverTaskNotify）/ internal/tools/tasknotify.go:59（webhook 10s）/ :60（cmd 30s）" \
  "改前：\`wall=\$WALL_B\`（CLI 等完了那条 sleep 2）。
改后：\`wall=\$WALL_A\`，CLI 立刻返回；3 秒后 \`cat\` 出 detached 子进程写下的 \`投递完成了\`——投递**没有**被跳过，只是不再挡住写台账的人。" \
  "$W/runs-async"

# --- 006: env > settings 的层次（五个旋钮共用的解析序） ------------------------
ID_H1="$(sub "env-notify F: 层次 base" --payload p --repo "$ROOT")"
ID_H2="$(sub "env-notify F: 层次 env 关掉" --payload p --repo "$ROOT")"
SBCAP="$W/settings-cmd-target.jsonl"
SET_WITH_NOTIFY="$W/settings-with-notify.json"
python3 - "$SB/.gopi/settings.json" "$SET_WITH_NOTIFY" "$SBCAP" <<'PY' || exit 1
import json, sys
src, dst, cap = sys.argv[1], sys.argv[2], sys.argv[3]
cfg = json.load(open(src))
cfg["notify"] = {"cmd": "cat >> " + cap, "events": "claimed,in_progress,completed,failed"}
json.dump(cfg, open(dst, "w"), ensure_ascii=False, indent=2)
PY
# 这一条两次运行都不走 $OFF，而是走"带 notify 段的副本"——它要证明的正是层次本身
{
  cat <<EOF
BEFORE|settings 层有 notify.cmd（env 全空）—— 投递去 settings 指的地方|GOPI_SETTINGS=$SET_WITH_NOTIFY $BIN task update $ID_H1 --status in_progress
AFTER|同一个 settings，但 env 把 cmd 设成空串（显式关掉）|GOPI_SETTINGS=$SET_WITH_NOTIFY GOPI_TASK_NOTIFY_CMD='' $BIN task update $ID_H2 --status in_progress
EOF
} > "$W/runs-hierarchy"
entry 006 notify-env-over-settings-hierarchy \
  "五个旋钮共用的解析序：CLI flag > env > settings.json > 内置默认" yes \
  "同一个开关有两处可以写：settings.json 让它跨重启活着，env 用来临时盖掉它——env 一旦写了，哪怕写成空串，也算你写了" \
  "结论是改前那半段的 \`cmd ok\` 与改后那半段的**沉默**：settings 里那条 cmd 一模一样的两次运行里，只有 env 出现的那一次不开火。" \
  "框住 BEFORE 段的 \`cmd ok\`：settings 层写的东西在 env 缺席时照样投递（这是"跨重启活着"的实现）。AFTER 段同样的 settings、同样的跃迁，只因为 env 写了个空串，这一行就消失了——空串也是显式的值。" \
  "task-notify] cmd ok:settings 层在 env 缺席时投递;;Task updated:改后那半段只剩这一行" \
  "internal/config/config.go:621（ResolveNotify）/ :630（LookupEnv：present 即覆盖，空串也覆盖）/ internal/tools/tasknotify.go:246（零值 = 通知关闭）" \
  "改前：settings 里的 cmd 被执行，\`settings-cmd-target.jsonl\` 多一行。
改后：\`GOPI_TASK_NOTIFY_CMD=''\` 让整条命令**不被执行**，那一行只剩 \`Task updated:\`——env 的"显式空"盖掉了 settings 的"有值"。" \
  "$W/runs-hierarchy"

# ===========================================================================
# findings —— 只写真跑挖到的缝（README §2：05-finding 有正文即为「有」）
# ===========================================================================
finding 006-notify-env-over-settings-hierarchy <<'FINDING_EOF'
**缝（Rule 21 家族：写下来的配置，没人按"隔离"的意思读它）。**
`gopi sandbox create` 造出的私有 profile，其 `settings.json` 是**操作者 settings 的副本**，
`notify` 段一并带走 —— 于是"在沙盒里做实验"这件事本身会往**操作者的真通知台账**里追加行。

**这次实测（本条目就是证据本身，不是旁述）：**

```
$ env -u GOPI_SETTINGS GOPI_HOME=<sandbox> ./bin/gopi config show
  home                   /home/user/workspace/gopi-sandbox/sandbox-env-notify-probe-3f7837c4
  settings file          …/sandbox-env-notify-probe-3f7837c4/.gopi/settings.json  (782 bytes)
settings.json:
  notify            {"cmd":"cat >> /home/user/.gopi/notify/task-events.jsonl","events":"claimed,in_progress,completed,failed,stale,build-failed","async":false}
```

沙盒的 settings 文件里，`notify.cmd` 指向的是 **`/home/user/.gopi/notify/task-events.jsonl`**
——沙盒外的、操作者的文件。本探测的**第一次**运行（当时只钉了 `GOPI_HOME`、没钉
`GOPI_SETTINGS`）就在那里真的落下了一行：`01a0ed8d-d2a4-7633-96fd-f338cf6884df`。

**量到的范围恰好是 1 行**（这一点值得单独写清楚，因为"按 task_id 前缀归因"会把别人的行
算到自己头上）：该台账是**全机器共用**的——同文件里紧邻的 `01a0ed93…01a0ed98` 那些
`[auto] hi` / `[auto] say hi` 行是**别的会话**自己投递的，与本探测无关。本探测自己的
证据是一条**对照实验**：钉上 `GOPI_SETTINGS` 之后再跑一次，并把一枚唯一 token 写进
`--reason`，然后在操作者台账里数它 —— `grep -c` 得到 **0**。也就是说：这一行是
"只钉 `GOPI_HOME` 的那一次"漏出去的，不是"钉了也漏"。

**机制**：`GOPI_HOME` 决定 home 与台账，`settings.json` 由 `GOPI_SETTINGS` **另走一路**决定
（`internal/config/config.go:862` 起的 `ResolveSettingsPath`），而沙盒创建路径把操作者
settings **整份拷贝**进沙盒（`internal/sandbox/settings_doc.go:175` 那一族"copy the settings doc"的逻辑）。
两件事各自都对：副本是"沙盒里也有 settings 可读"的朴素实现；而
`ResolveNotify`（`internal/config/config.go:621`）忠实读取副本里的 `notify` 段。
合起来是：**沙盒给了一个指向沙盒外的投递目标**。

**为什么值得记（这是"旋钮 vs 隔离"的经典错位）**：沙盒的意义是"这里做的事不出去"。
一个把真台账路径拷进去的 profile，让最小的一次 `task update` 也能写到操作者的全局事件流上 ——
而这些行是**只追加、无校验**的（`cat >>`），落进去就再也分不出是实验还是真事。
本次因此把 `GOPI_SETTINGS` 也钉进沙盒、并在生成器开头加了一条自证：
沙盒 settings 里**不许**出现 `notify` 段，否则生成器拒绝开工（见本文件同级的
`scripts/materials/materials-gen-env-notify.sh`）。

**一条克制**：拷贝的意图（"沙盒里也要能读到 settings"）不是错的，错的是**没有把
"指向外部的目标"这一类字段从副本里剥掉**。可以修的方向有二：拷贝时清空 `notify.*`
（与 `sync_peers` 同级的"指向外面的东西"），或者让 `sandbox create` 打印一行
"本沙盒的 settings 副本里仍有指向沙盒外的配置：notify.cmd → …"。本条目只记录测量结果。
FINDING_EOF

finding 001-notify-cmd-default-off <<'FINDING_EOF'
**缝（不是 bug 行为，是"承诺 vs 实际"的一处硬伤）：`CONFIG.md` 把 notify 的
file:line 指向了两个不相干的函数；同一个 `--help-env` 又把 `GOPI_SETTINGS` 的读者说成了唯一一个。**

本面第一纪律要求"每条给出真实 reader 的 file:line"，于是逐条 grep 核对了
`--help-env` 与 `CONFIG.md` 自报的坐标，三处对不上：

| 文档里的引用 | 该行实际是什么 | 真正的定义处 |
|---|---|---|
| `CONFIG.md:86`、`CONFIG.md:366`：`config.ResolveNotify`（`internal/config/config.go:514`） | `BuildTimeoutSec` 字段的 `json:"build_timeout_sec"` | `internal/config/config.go:621`（`func ResolveNotify`） |
| `CONFIG.md:463`：`NotifySettings`（`internal/config/config.go:459`） | 一个 `Build string` 字段的注释 | `internal/config/config.go:572`（`type NotifySettings`） |

这两处**恰好差约 110 行**——是一份加在文件中间的东西把下面的行全推下去了，而引用是硬写的数字。
`file:line` 是 Rule 38 意义上"读者会去 ACT 的数字"：他**会**翻到 514 行，然后读到一段
跟通知毫无关系的构建超时字段，并且没有第二处提示告诉他真正的入口在 621。

**第二处、方向相反**：`gopi --help-env` 对 `GOPI_SETTINGS` 那一行写
"Path to settings.json"（`(see --settings)`）。但 `GOPI_SETTINGS` 的读者**不止一个**：
`internal/config/config.go:865`（配置层）、`internal/scratch/tmproot.go:487`
（`settingsFilePath`）、`internal/deepseek/config.go:37`（provider 层）各自读它。
实测的后果很具体：只钉 `GOPI_HOME` 而 `GOPI_SETTINGS` 仍在环境里时，
`config show` 报的 home 是沙盒、**读的却是另一个 settings 文件** ——
也就是 007 那条 finding 的现场。一行 help 里"谁读它"没写全，读的人就得自己踩一次。

**克制**：这两条都不是行为缺陷（行为按 `ResolveNotify` 的语义是对的），所以按
"文档与代码不一致"记，而不是按 bug 记。`file:line` 的修法很轻
（指向 `func ResolveNotify` / `type NotifySettings` 的定义处，或干脆只写函数名不写行号）；
`--help-env` 那一行的修法是补一句"被多个子系统读，含 provider 与 scratch 层"。
FINDING_EOF

# --- 默认 finding：诚实的"查过、没缝" -----------------------------------------
# 真缝只有两条（001 / 006 由上面两条 finding() 覆写）。**不许留空**：README §2 的
# finding 谓词要的是"去掉空行后仍有正文"，所以没挖到缝的条目也要写清楚查过什么。
# 本面的默认正文多一句本面特有的话：这条的**改前/改后差**本身就已经是一条结论。
default_finding() {
  local dir="$OUTROOT/$1" name="$2" feat="$3" reader="$4"
  [[ -s "$dir/05-finding.md" ]] && return 0
  cat > "$dir/05-finding.md" <<EOF
## 这次运行没挖到缝（但查过什么，写在这里）

本条目对 $feat 做了三方比对，没有发现不一致：

- --help-env 的广告：该变量在表里的分组与说明，即"它是干什么的"；
- 实际行为：02-output.txt 里改前 / 改后两次真跑的逐字节输出与退出码（本条目）；
- 代码：真实 reader —— $reader —— 的判定。

判据不是"看起来对"，而是三样东西**互相不矛盾**：help 说的用途、这次真跑出来的差、
以及那个 reader 的实现是同一件事。**本条目没有可报告的缺陷**——这一条本身是结论，不是省略。

（本面另有一条跨条目的观察，写在 001-notify-cmd-default-off/05-finding.md：
CONFIG.md 为 notify 标的 file:line 指向了两个不相干的函数。本条目的 reader 坐标
是本次逐条 grep 核过的真坐标，与那条 finding 是同一批核对的两个结果。）
EOF
}
default_finding 002-notify-webhook-default-off 002 "GOPI_TASK_NOTIFY_WEBHOOK（HTTP POST 投递）" "internal/config/config.go:627（读 env）+ internal/tools/tasknotify.go:495（POST）"
default_finding 003-notify-events-allowlist  003 "GOPI_TASK_NOTIFY_EVENTS（默认事件集 / all 展开）" "internal/tools/tasknotify.go:66（默认常量）/ :259（回落到默认）/ :420（drop + 诊断）"
default_finding 004-notify-pattern-glob      004 "GOPI_TASK_NOTIFY_PATTERN（glob 广播 / 过滤）" "internal/tools/tasknotify.go:322（filepath.Match）/ :425（drop）"
default_finding 005-notify-async-fire-and-forget 005 "GOPI_TASK_NOTIFY_ASYNC（阻塞 vs 分离投递）" "internal/tools/tasknotify.go:457（go deliverTaskNotify）/ :59, :60（两个上限）"

echo "== 渲染真值（像素级；不是 OCR，也不是数标签） =="
# 一处标注"画上去了"= 那几行文字上真的多出了墨（ink_under_mark），并且标注图与原始图
# 的像素确实不同（changed_pixels）。这是判"标注有效"的唯一可测量口径：
# 003 第一次跑就是靠它露馅的——针落在折行中间，像素上什么都没框住，而 render.py 的
# "note: matched no rendered line" 只是一行 note，不拦人。这里把它升成硬错误。
python3 "$ROOT/scripts/materials/render-truth.py" "$W" "$OUTROOT" --pattern '0[0-9][0-9]-notify-*'
render_bad=$?
[[ "$render_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 渲染真值未通过（标注没画到字上）" >&2; exit 1; }

echo "== 形状断言：每个条目必须恰好六件、件件非空，且 05-finding 去掉空行后仍有正文 =="
shape_bad=0
for d in "$OUTROOT"/0[0-9][0-9]-notify-*/; do
  nfiles=$(find "$d" -maxdepth 1 -type f | wc -l)
  [[ "$nfiles" -eq 6 ]] || { echo "SHAPE FAIL: $d 有 $nfiles 个文件（要求恰好 6）" >&2; find "$d" -maxdepth 1 -type f -printf '    %f\n' >&2; shape_bad=1; }
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $d/$f 缺失或为空" >&2; shape_bad=1; }
  done
  grep -qE '^exit=[0-9]+$' "$d/02-output.txt" \
    || { echo "SHAPE FAIL: $d/02-output.txt 没有 ^exit=<n> 行" >&2; shape_bad=1; }
done

echo "== index =="
cat "$W/index.tsv"
if [[ -s "$W/failed.txt" ]]; then
  echo "== FAILED entries ==" >&2; cat "$W/failed.txt" >&2
  echo "GENERATOR FAILED: $(wc -l < "$W/failed.txt") entry/entries produced no annotated image" >&2
  exit 1
fi
[[ "$shape_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 形状断言未通过" >&2; exit 1; }
printf 'WORK=%s\nSANDBOX=%s\nEVIDENCE=%s\n' "$W" "$SB" "$EVID"
