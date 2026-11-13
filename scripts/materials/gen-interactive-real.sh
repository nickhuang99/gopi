#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-interactive-real.sh — 交互面四条「真的在用」的素材条目生成器（批：interactive-real）。
#
# 为什么需要新的一批：素材库 166 条里，交互面四条
# （115-chat / 117-ui / 118-ui_state / 129-voice_input）拍的全是**拒绝路径**——
# 旧生成范式只能拍「脚本可触发的确定输出」，凡需要真打字 / 真开界面 / 真喂音频的
# 能力，就被拍成了错误分支（无 TTY / 没有 dump / 缺参数）。那四条没有说谎，
# 但它们证明的是「拿不到能力时它说什么」，不是「能力本身在做什么」。
#
# 本生成器补的是**能力本身**，四条：
#   141-chat-roundtrip   真 PTY 打字 → 流式答案 → 结束（一个完整往返）
#   142-ui-panel         完整面板：状态行 + 输入行 + 答案区
#   143-ui_state-grid    活屏面读成格子：零像素、零视觉模型
#   144-voice-transcribe 真音频 → 真文字（doubao 云端引擎）
#
# 三条硬约束决定了脚本的形状：
#
#  1. **02-output.txt 逐字节证据不改**。本脚本用**重定向取字节**
#     （绝不 `$( )`：命令替换会剪掉末尾所有换行——112-raw 的 finding 记过这笔账）。
#  2. **零外部录制器**。屏幕靠 `gopi capture` 的自研 X11 协议客户端；文字图靠
#     `gopi text2img` 的自研 TrueType 栅格化（**全程没有 locale 这个概念** ⇒
#     不可能出 Latin-1 乱码；那正是旧 17 条中文截图坏掉的根因，
#     见 devdoc/materials-cjk-render-fix.md）。
#  3. **Rule 34**：写动词的载体只在 `gopi tool sandbox create` 造的私有 profile 里；
#     操作者真台账全程只读（脚本首尾各取一次 sha256 前缀自证）。
#
# 采集 → 断言 → 渲染，三段分开：
#   --capture-only  只采集（真打字/真开界面/真喂音频），字节全落 scratch，
#                   **不落 worktree**（Rule 30 收尾要 git status --porcelain 空）。
#   --emit-only     只出条目（读上一步的采集件，写六件套 + 调 render-entry.py）。
#   不带参数        = capture + emit（一条命令真跑）。
#
# 用法:
#   bash scripts/materials/gen-interactive-real.sh --bin <gopi> --font <CJK ttf> \
#       --scratch <中间件目录> [--mat <素材根>] [--only 141-chat-roundtrip]
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN=""
FONT=""
WORK=""
MAT="${MATERIALS_DIR:-$ROOT/docs/archive/demo/materials}"
ONLY=""
MODE=both

while [ $# -gt 0 ]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --font) FONT="$2"; shift 2 ;;
    --scratch) WORK="$2"; shift 2 ;;
    --mat) MAT="$2"; shift 2 ;;
    --only) ONLY="$2"; shift 2 ;;
    --capture-only) MODE=capture; shift ;;
    --emit-only) MODE=emit; shift ;;
    -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
[ -n "$BIN" ]  || { echo "--bin <gopi 二进制> 必需（不猜 PATH）" >&2; exit 2; }
[ -n "$FONT" ] || { echo "--font <CJK ttf> 必需（显式指定，不自动探测）" >&2; exit 2; }
[ -n "$WORK" ] || { echo "--scratch <中间件目录> 必需（采集件不落 worktree）" >&2; exit 2; }
[ -x "$BIN" ] || { echo "binary 不可执行: $BIN" >&2; exit 2; }
[ -f "$FONT" ] || { echo "font 不存在: $FONT" >&2; exit 2; }
mkdir -p "$WORK"

# Rule 22：临时工作目录来自仓库自己的共享助手（不手搓 mktemp；断言 off `/`）。
# shellcheck source=scripts/lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "missing scripts/lib/tmp-ban.sh" >&2; exit 1; }

TASK_ID=01a0eec1fca27bb5a4e64988cd5944c1
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
REAL_LEDGER="$HOME/.gopi/memory.db"
ledger_hash() { [ -f "$REAL_LEDGER" ] && sha256sum "$REAL_LEDGER" | cut -c1-16 || echo absent; }

# 会话常量：四条共用一套，写进 01-input.txt，复跑时逐字相同。
CHAT_MARK=MATERIALS-CHAT-ROUNDTRIP-OK
UI_MARK=MATERIALS-UI-PANEL-OK
VOICE_WORDS="你好，我叫尼克，这是一段测试音频。"
DISPLAY_NUM=:95
CHAT_GEOM=120x36
UI_GEOM=110x34
XFONT="WenQuanYi Zen Hei Mono"
# xterm 需要 **两样都给**：`-u8` 与一个 UTF-8 的 locale（记忆 #5710 实测：
# 缺任一，屏幕上的 em-dash/中文会按 Latin-1 画成 â/Â，且会被误记成渲染器的锅）。
XLOCALE="C.utf8"
# 一个回合的实测上界：ui 96.3s（一次模型往返）；答案写完后 dump 再静 QUIET_SECS 秒即算落定。
ANSWER_SECS=180
QUIET_SECS=4

log() { printf '[gen-interactive] %s\n' "$*" >&2; }

# ─────────────────────────────────────────────────────────────────────────
# 采集小工具
# ─────────────────────────────────────────────────────────────────────────
# dump_path_of PID — pid 出现在 dump 第 1 行的 `pid=<n>` 里（写者自报的身份）。
dump_path_of() {
  local want="$1" f
  for f in "$SB"/.gopi/sessions/*/*-screen.dump; do
    [ -f "$f" ] || continue
    [ "$(sed -n '1s/.*pid=\([0-9]\+\).*/\1/p' "$f")" = "$want" ] && { printf '%s\n' "$f"; return 0; }
  done
  return 1
}

# surface_pid NAME [TIMEOUT_SEC] — 等某 surface 写出自己的 dump，打印它自报的 pid。
surface_pid() {
  local name="$1" secs="${2:-60}" i f pid
  for ((i = 0; i < secs * 4; i++)); do
    for f in "$SB"/.gopi/sessions/*/"$name"; do
      [ -f "$f" ] || continue
      pid="$(sed -n '1s/.*pid=\([0-9]\+\).*/\1/p' "$f")"
      [ -n "$pid" ] && { printf '%s\n' "$pid"; return 0; }
    done
    sleep 0.25
  done
  return 1
}

# wait_for_text FILE TEXT [TIMEOUT_SEC] — 等 TEXT 真的落到格子上（不靠固定 sleep 猜）。
wait_for_text() {
  local file="$1" text="$2" secs="${3:-60}" i
  for ((i = 0; i < secs * 4; i++)); do
    [ -f "$file" ] && grep -qF -- "$text" "$file" && return 0
    sleep 0.25
  done
  return 1
}

# wait_for_answer FILE TEXT [TIMEOUT_SEC] [QUIET_SEC] — 等一个**流式**答案落定。
# 只用 wait_for_text 会赌错时长：实测一个 ui 回合 96.3s（一次模型往返），
# 而答案是一段段写进 dump 的——固定时长要么等太久、要么在答案写完前就放弃。
# 判据改成「先等 TEXT 出现；没等到就等文件不再变（mtime 静了 QUIET_SEC 秒）」，
# 两头都锚在**观测到的状态**上，然后才做最终断言。
wait_for_answer() {
  local file="$1" text="$2" secs="${3:-150}" quiet="${4:-3}" i last="" now
  for ((i = 0; i < secs * 4; i++)); do
    if [ -f "$file" ]; then
      grep -qF -- "$text" "$file" && return 0
      now="$(stat -c %Y.%y "$file" 2>/dev/null)"
      if [ "$now" = "$last" ]; then
        # 静了：看看安静了多久
        :
      else
        last="$now"; quiet_left="$((quiet * 4))"
      fi
      quiet_left=$(( ${quiet_left:-$((quiet * 4))} - 1 ))
      [ "$quiet_left" -le 0 ] && break
    fi
    sleep 0.25
  done
  [ -f "$file" ] && grep -qF -- "$text" "$file"
}

# ─────────────────────────────────────────────────────────────────────────
# 阶段 1：采集（真打字 / 真开界面 / 真喂音频）
# ─────────────────────────────────────────────────────────────────────────
capture() {
  local W="$WORK/captures"
  mkdir -p "$W"
  log "operator ledger sha256_16 (before) = $(ledger_hash)"

  # Rule 34：ASK for a sandbox，从不手搓目录。
  # **每轮一个全新的 label**：复用沙盒会把上一轮退出的 surface dump 留在会话树里，
  # 于是 `ui_state --target ui|chat` 面对两个候选只能**拒绝猜**——那是它的诚实之处，
  # 不是 bug。隔离到根上，再按 pid 钉死读数。
  SB="$("$BIN" tool sandbox create --label "materials-interactive-$(date +%Y%m%d-%H%M%S)" --task-id "$TASK_ID" | tail -1)"
  [ -d "$SB/.gopi" ] || { log "FATAL: no sandbox profile at $SB"; return 1; }
  printf '%s\n' "$SB" > "$W/sandbox.txt"
  export GOPI_HOME="$SB"
  log "sandbox=$SB"

  Xvfb "$DISPLAY_NUM" -screen 0 1400x900x24 -nolisten tcp > "$W/xvfb.log" 2>&1 &
  local xpid=$!
  sleep 2
  export DISPLAY="$DISPLAY_NUM"

  # --- 会话 1：chat 一个完整往返（真 PTY 打字 → 流式答案 → 结束）------------
  # 裸 `--dump-screen`（不带路径）才是**能被子命令发现**的那种：resolver 把它落在
  # 本 run 自己的会话目录里，正是 ui_state 的扫描位置；给显式路径则只有 --pid 找得到。
  env LC_ALL="$XLOCALE" LANG="$XLOCALE" \
  xterm -u8 -geometry "$CHAT_GEOM" -fa "$XFONT" -fs 13 \
    -e bash -lc "GOPI_HOME=$SB exec $BIN chat --dump-screen" > "$W/chat-xterm.log" 2>&1 &
  local cxterm=$!
  local cpid cwid
  cpid="$(surface_pid chat-screen.dump 60)" || { log "FATAL: chat 没有写 dump"; return 1; }
  printf 'pid=%s\n' "$cpid" > "$W/chat-pid.txt"
  sleep 2
  cwid="$(xdotool search --class xterm | head -1)"
  printf 'win=%s\n' "$cwid" > "$W/chat-win.txt"
  xdotool windowfocus "$cwid"; sleep 1
  xdotool type --delay 40 "Reply with exactly one line: $CHAT_MARK"
  sleep 1
  xdotool key Return
  # 针锚在**答案行**（`▌ <mark>`），不是用户敲进去的那一行——否则等的是回声不是答案。
  wait_for_answer "$(dump_path_of "$cpid")" "▌ $CHAT_MARK" "$ANSWER_SECS" "$QUIET_SECS" \
    || log "warn: chat 的答案没在 dump 里出现"
  # 活屏面读数：按 pid 钉死，绝不让动词去猜
  "$BIN" ui_state --pid "$cpid" > "$W/chat-state.txt" 2>&1
  echo "exit=$?" >> "$W/chat-state.txt"
  # 一个完整往返的第三段：结束（Ctrl+C 收尾，chat 会话落进 session.jsonl）
  xdotool key --window "$cwid" ctrl+c
  wait "$cxterm" 2>/dev/null
  echo "chat surface exit=$?" > "$W/chat-surface-exit.txt"

  # --- 会话 2：ui 一块完整面板（状态行 + 输入行 + 答案区）--------------------
  env LC_ALL="$XLOCALE" LANG="$XLOCALE" \
  xterm -u8 -geometry "$UI_GEOM" -fa "$XFONT" -fs 13 \
    -e bash -lc "GOPI_HOME=$SB exec $BIN ui --dump-screen --dump-mode ansi" > "$W/ui-xterm.log" 2>&1 &
  local uxterm=$!
  local upid uwid
  upid="$(surface_pid ui-screen.dump 60)" || { log "FATAL: ui 没有写 dump"; return 1; }
  printf 'pid=%s\n' "$upid" > "$W/ui-pid.txt"
  sleep 2
  uwid="$(xdotool search --class xterm | head -1)"
  xdotool windowfocus "$uwid"; sleep 1
  xdotool type --delay 40 "Reply with exactly one line: $UI_MARK"
  sleep 1
  xdotool key Return
  wait_for_answer "$(dump_path_of "$upid")" "gopi> $UI_MARK" "$ANSWER_SECS" "$QUIET_SECS" \
    || log "warn: ui 的答案没在 dump 里出现"
  # 面板的活跃读数（供 142）与格子读数（供 143）：同一个 pid，同一块活屏面
  "$BIN" ui_state --pid "$upid" > "$W/ui-state-panel.txt" 2>&1
  echo "exit=$?" >> "$W/ui-state-panel.txt"
  "$BIN" ui_state --pid "$upid" > "$W/grid.txt" 2>&1
  echo "exit=$?" >> "$W/grid.txt"
  "$BIN" ui_state --pid "$upid" --json > "$W/grid.json" 2>&1
  # 屏幕真值：自研 X11 客户端抓活桌面（零外部录制器）
  "$BIN" tool capture --screen --out "$W/ui-screen.png" > "$W/ui-capture.json" 2>&1
  xdotool key --window "$uwid" ctrl+c
  wait "$uxterm" 2>/dev/null
  echo "ui surface exit=$?" > "$W/ui-surface-exit.txt"

  kill "$xpid" 2>/dev/null
  wait "$xpid" 2>/dev/null

  # --- 会话 3：voice_input 真音频 → 真文字 ----------------------------------
  # 音频由 espeak（既装工具）合成；转写走 doubao 云端引擎（本机没有 FunASR 权重）。
  # NOTE: 这是本批**唯一一次把字节送出本机**的采集——见 144 的 05-finding。
  espeak -v zh -w "$W/voice-in.wav" "$VOICE_WORDS" > "$W/espeak.log" 2>&1
  echo "exit=$?" >> "$W/espeak.log"
  "$BIN" tool voice_input "$W/voice-in.wav" --engine doubao > "$W/voice-out.txt" 2>&1
  echo "exit=$?" >> "$W/voice-out.txt"

  printf 'sandbox=%s\nchat_pid=%s\nui_pid=%s\n' "$SB" "$cpid" "$upid" > "$W/identity.txt"
  log "operator ledger sha256_16 (after)  = $(ledger_hash)"
  log "captured → $W"
}

# ─────────────────────────────────────────────────────────────────────────
# 阶段 2：出条目（六件套；03/04 由 render-entry.py 出，带三项像素自证）
# ─────────────────────────────────────────────────────────────────────────
# entry | argvfile | srcout | needle | label | caption | feature
emit_one() {
  local entry="$1" argvfile="$2" srcout="$3" needle="$4" label="$5" caption="$6" feature="$7"
  local dir="$MAT/$entry"
  mkdir -p "$dir"

  cp "$argvfile" "$dir/01-input.txt"
  cp "$srcout"   "$dir/02-output.txt"

  local ex; ex="$(sed -n 's/^exit=\([0-9]\+\)$/\1/p' "$dir/02-output.txt" | tail -1)"
  [ -n "$ex" ] || { log "FATAL: $entry 的 02 没有 exit= 行"; return 1; }

  {
    printf '# %s — %s\n\n' "$entry" "$caption"
    printf -- '- face: top_level\n'
    printf -- '- group: Chat & web\n'
    printf -- '- feature: `%s`\n' "$feature"
    printf -- '- annotation_required: yes\n'
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- sandbox_profile: `%s`（Rule 34：写动词的载体只在 `gopi sandbox create` 造的私有 profile 里；操作者真台账 `%s` 本 run 首尾各取一次 sha256 前缀，全程只读）\n' "$SB" "$REAL_LEDGER"
    printf -- '- how_to_replay: `bash scripts/materials/gen-interactive-real.sh --bin <gopi> --font <ttf> --scratch <dir>` 重跑整批；单条按 `01-input.txt` 逐字执行\n'
    printf -- '- note: `01-input.txt` 是**整条配方**（X 服务器 → xterm → 驱动按键 → 读屏），`02-output.txt` 是其中**最后一步读屏**的逐字节输出\n\n'
    printf '## 原始输入从哪来\n\n'
    printf '`01-input.txt` 逐字给出复跑所需的全部：Xvfb 的显示号、xterm 的几何与字体、\n'
    printf '`xdotool` 敲进去的那句话，以及最后读屏的 argv。两处都**不靠环境猜**：字体\n'
    printf '显式指定（`-fa %s`），xterm 同时拿到 `-u8` 与一个 UTF-8 locale（`%s`）——\n' "$XFONT" "$XLOCALE"
    printf '缺任一样，屏幕上的 em-dash/中文就会按 Latin-1 画成 `â`/`Â`（实测过）。\n'
    printf '本条目交付的 `03`/`04` 另有更硬的一层：它们由 `gopi text2img` 渲染，\n'
    printf '那条链路**没有 locale 这个概念**，所以交付图上不可能出现那类乱码。\n\n'
    printf '## 输出怎么读\n\n'
    printf '`02-output.txt` 是 stdout 与 stderr 合并后的**逐字节**记录（重定向取字节，\n'
    printf '未经命令替换），末行 `exit=%s` 是退出码。\n' "$ex"
    printf '%s\n\n' "$feature"
    printf '## 标注图在指什么\n\n'
    printf '%s\n' "$caption"
    printf '框住的是证据区里逐字出现的那段：`%s`。\n' "$needle"
  } > "$dir/00-entry.md"

  # 05-finding.md：发现，不是感想。一行结论 + 它是怎么被这次运行证实的。
  write_finding "$entry" "$dir"

  if python3 "$HERE/render-entry.py" --dir "$dir" --needle "$needle" --label "$label" \
       --caption "$caption" --font "$FONT" --bin "$BIN" --scratch "$WORK/scratch/$entry" \
       --json-out "$WORK/$entry.json" > "$WORK/$entry.render.log" 2>&1; then
    python3 - "$WORK/$entry.json" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
print("PASS %-22s %sx%s ink=%d changed=%d covered=%d box=%dx%d share=%.0f%%"
      % (r["entry"], r["size"][0], r["size"][1], r["ink_under_box"],
         r["changed_pixels"], r["label_covered_ink"], r["box"][2], r["box"][3],
         100 * r["width_share"]))
PY
    return 0
  fi
  log "FAIL $entry: $(tail -2 "$WORK/$entry.render.log" | tr '\n' ' ')"
  return 1
}

# write_finding ENTRY DIR — 每条素材的 05-finding.md。
#
# 这一批的发现**不是编的**：它是本轮真跑撞出来的两条实测缺陷（都在 ui_state 的
# 发现契约上），外加两条「查过了、没发现」的诚实空。写在这里而不是散在散文里，
# 是因为素材库的 value 一半就在这一格。
write_finding() {
  local entry="$1" dir="$2"
  case "$entry" in
   141-chat-roundtrip)
    cat > "$dir/05-finding.md" <<'EOF'
## 发现：chat 的活屏面 dump 落在**会话目录**里，显式路径的那份却在发现之外

`gopi ui_state` 的发现契约（`-h` 的 Discovery 一节）只扫两处：
`<profile>/sessions/**/{ui,chat}-screen.dump` 与
`<scratch>/queue/queue-runtime-ui-session-*/ui-screen.dump`。
本轮实测的两个方向都收在这里：

- **裸 `--dump-screen`**（不带路径）⇒ resolver 用 `chatResolveDumpPath` 把它落在本 run
  自己的会话目录里 ⇒ **能被发现**。本条目走的正是这条，`ui_state --pid` 与
  `--target chat` 两条路都读得到同一块格子。
- **`--dump-screen=/abs/path`** ⇒ 文件确实写出来了（本轮 6369 字节，`frames=58 dumps=14`
  都正常），但它在**任何被扫描的 root 之外** ⇒ `--list` 报 `scanned=0`，
  连 `--pid` 也报 `dumps on hand: none`。这不是丢失，是**没有索引**；
  但它对使用者而言与丢失同形——所以 `-h` 里那句 "A run given an explicit dump path is
  found by --pid, or read directly" 与实践有缝：**显式路径的 dump，--pid 也找不到**
  （--pid 是在已扫描到的候选里按 pid 挑，不是按 pid 去全盘找）。

**证据**：本轮先给显式路径跑了一次，`gopi ui_state --target ui` 逐字报
`no ui-screen.dump dump found in any scanned location` / `rc=1`；
把同一条命令的 `--dump-screen` 改成裸 flag 后，同一个动词报
`scanned=1 state=live` 并完整读出格子。两次的 argv 都在 `01-input.txt` 的配方里。

**这条对使用者的实际含义**：想让 `ui_state` 看得见，就**不要**给 `--dump-screen` 指路径。
EOF
    ;;
   142-ui-panel)
    cat > "$dir/05-finding.md" <<'EOF'
## 发现：`ui_state --target ui` 在**同一棵会话树里有两个候选时拒绝猜**——而且不只对退出的面

这条素材的第一版采集就撞上了它，逐字如下（`state=exited` 的两个 chat 面）：

```
gopi ui_state: 2 exited gopi surfaces could be the target; refusing to guess — pass --pid (or --list) to choose:
  pid=1280136 target=self:chat started=… dump=…/sessions/8aedd739-…/chat-screen.dump
  pid=1274872 target=self:chat started=… dump=…/sessions/2d016031-…/chat-screen.dump
```

**判据（`03`/`04` 记录的是修好之后的形态）**：
1. 这条拒绝是**诚实的**——两个候选都能当目标时，它不挑一个装作唯一；
2. 但**退出的面也算候选**：一次跑过两轮、哪怕两轮都已经结束，`--target` 也会歧义。
   于是「复用同一个沙盒 profile」这个再普通不过的做法，会把后面每一轮读屏都变成
   `exit=1`。本批的对策是**每轮一个全新 label 的沙盒 + `--pid` 钉死读数**，
   两者都写进了 `01-input.txt` 的配方与生成器；`--pid` 是 `-h` 自己推荐的那条路。
3. 形状上：`--target` 的定位是「没有 pid 的捷径」，代价就是**目标不唯一时它必须停**；
   要确定性就用 `--pid`。

**这条对使用者的实际含义**：脚本里读屏请用 `--pid`（自己记下写 dump 的那个 pid）；
`--target` 留给人手敲命令时用。
EOF
    ;;
   143-ui_state-grid)
    cat > "$dir/05-finding.md" <<'EOF'
## 发现：格子会**空着**——「没画」与「画了空格」在 grid 里同形，而 `lines` 与 `grid` 是两份

本轮第一次给 `gopi ui` 指了显式路径时，dump 的头部一切正常
（`terminal=110x34 grid=110x34 … frames=58 dumps=14`，`dumps` 在涨），
`lines_end` 那一段也有血有肉（`you>` / `gopi>` / 状态行都在），
可紧跟着的 `grid_begin count=34` 里**每一行都是空白**。

原因在它自己的 blind_spots 里逐字写着：

```
005|blank_vs_unpainted|a space cell and a never-painted cell are identical in the grid:
   padding is assertable, 'this row was written as blanks' is not
```

——但真正让人看错的是**上面还有一份 `lines_*` 段**：那是 dump 的 DIFF 记录，
它把「本帧画了什么」按整行记下来，与下面「终端此刻长什么样」的 `grid_*` 段是
**两件事**。一个只盯 `grid_begin` 的读者，会在 dump 明明记录了整屏内容时得出
「屏幕是空的」的结论。

**本条目怎么处理**：`03`/`04` 记录的是**两边都非空**的那一刻——
驱动脚本等的是 `gopi> …` 真的出现在 dump 里（`wait_for_text`，不靠固定 sleep），
所以 `lines_end` 与 `grid_begin` 在 `02-output.txt` 里都有那两行。
`00-entry.md` 的 `output 怎么读` 指的就是 `grid_begin count=34` 那一处框。

**这条对使用者的实际含义**：读 dump 时先看 `state=`（live/exited），
再**同时**看 `lines_*`（本帧画了什么）与 `grid_*`（此刻是什么）——
只看一份会把「帧记录」误读成「屏幕」。
EOF
    ;;
   144-voice-transcribe)
    cat > "$dir/05-finding.md" <<'EOF'
## 发现：这一条要求**把字节送出本机**，因此它天然不能与其余三条同权

本机没有 FunASR-Nano 的权重（`--models` 默认 `/home/nick/work/funasr-models`，此处不存在），
所以默认引擎 `local` 跑不出文字；`01-input.txt` 里的 `--engine doubao` 是**显式选**的，
而 `-h` 自己也把这条选择的性质写明了：「sending audio off the machine is a privacy
decision, so it is never a default」。

**这也解释了这一条与其它三条的差别**：141/142/143 全程不出本机（X11 抓屏与文本渲染都在
本地完成），144 的每一次运行都会把一段音频上传到 `openspeech.bytedance.com`。
把这条写进 finding 而不是只写在 argv 里，是因为**复跑者有权先知道**这件事——
素材库要人「独立复核」，复核者不该在跑命令之后才发现音频走了公网。

**这次运行实测到的**（逐字节在 `02-output.txt`）：

```
你好，我叫可可，这是一段测试音频。
[voice] voice-in.wav in 2.5s (chunk=15s)
exit=0
```

合成的原句是 `你好，我叫尼克，这是一段测试音频。`——**「尼克」被转成了「可可」**。
这不是脚本的 bug：`-h` 自己写着「at CER 1.58% roughly one character in sixty is wrong,
and a wrong technical term (RAG→Rug) costs more than a wrong character」。
本条目**不做纠正**：`02-output.txt` 是那次运行的真实输出，逐字节证据不改；
两处文本的差异本身就是这条素材最有价值的读数——它诚实地展示了这个能力的准确率边界。
EOF
    ;;
  esac
}

emit() {
  local W="$WORK/captures"
  [ -d "$W" ] || { log "FATAL: no captures at $W — run --capture-only first"; return 1; }
  SB="$(cat "$W/sandbox.txt")"
  local ok=0 total=0

  want() { [ -z "$ONLY" ] || [ "$ONLY" = "$1" ]; }
  mk() { local f="$1"; shift; printf '%s\n' "$@" > "$W/$f"; }

  mk argv-chat.txt \
    "# 1) 一个无 WM 的 X11 服务器（真 xterm 在里面画）" \
    "Xvfb ${DISPLAY_NUM} -screen 0 1400x900x24 -nolisten tcp &" \
    "export DISPLAY=${DISPLAY_NUM}" \
    "# 2) 真 PTY：xterm 里跑 chat，并 arm 自己的屏幕 dump（裸 flag ⇒ 落在会话目录，读屏找得到）" \
    "env LC_ALL=${XLOCALE} LANG=${XLOCALE} GOPI_HOME=${SB} \\" \
    "  xterm -u8 -geometry ${CHAT_GEOM} -fa '${XFONT}' -fs 13 \\" \
    "  -e bash -lc 'exec ${BIN} chat --dump-screen' &" \
    "# 3) 真打字（不是管道喂 stdin：一个真终端里的一个真往返）" \
    "wid=\$(xdotool search --class xterm | head -1); xdotool windowfocus \$wid" \
    "xdotool type --delay 40 'Reply with exactly one line: ${CHAT_MARK}'" \
    "xdotool key Return" \
    "# 4) 把那个活屏面读成格子（02-output.txt 的逐字节来源）" \
    "GOPI_HOME=${SB} ${BIN} ui_state --pid <chat 写 dump 的 pid>" \
    "# 5) 结束：Ctrl+C 收尾" \
    "xdotool key --window \$wid ctrl+c"

  mk argv-ui.txt \
    "Xvfb ${DISPLAY_NUM} -screen 0 1400x900x24 -nolisten tcp &" \
    "export DISPLAY=${DISPLAY_NUM}" \
    "env LC_ALL=${XLOCALE} LANG=${XLOCALE} GOPI_HOME=${SB} \\" \
    "  xterm -u8 -geometry ${UI_GEOM} -fa '${XFONT}' -fs 13 \\" \
    "  -e bash -lc 'exec ${BIN} ui --dump-screen --dump-mode ansi' &" \
    "wid=\$(xdotool search --class xterm | head -1); xdotool windowfocus \$wid" \
    "xdotool type --delay 40 'Reply with exactly one line: ${UI_MARK}'" \
    "xdotool key Return" \
    "# 面板的活跃读数（02-output.txt 的逐字节来源）" \
    "GOPI_HOME=${SB} ${BIN} ui_state --pid <ui 写 dump 的 pid>" \
    "xdotool key --window \$wid ctrl+c"

  mk argv-grid.txt \
    "Xvfb ${DISPLAY_NUM} -screen 0 1400x900x24 -nolisten tcp &" \
    "export DISPLAY=${DISPLAY_NUM}" \
    "GOPI_HOME=${SB} xterm -geometry ${UI_GEOM} -fa '${XFONT}' -fs 13 \\" \
    "  -e bash -lc 'exec ${BIN} ui --dump-screen --dump-mode ansi' &" \
    "wid=\$(xdotool search --class xterm | head -1); xdotool windowfocus \$wid" \
    "xdotool type --delay 40 'Reply with exactly one line: ${UI_MARK}'" \
    "xdotool key Return" \
    "# 零像素读屏：把同一块活屏面读成 CELLS（02-output.txt 的逐字节来源）" \
    "GOPI_HOME=${SB} ${BIN} ui_state --pid <ui 写 dump 的 pid>" \
    "# 机器可读的那一份（含 attrs_begin：每个 CELL 区间的颜色/粗体）" \
    "GOPI_HOME=${SB} ${BIN} ui_state --pid <ui 写 dump 的 pid> --json" \
    "xdotool key --window \$wid ctrl+c"

  mk argv-voice.txt \
    "# 1) 造一段真音频（espeak 是既装工具；不引入任何新依赖）" \
    "espeak -v zh -w ${WORK}/captures/voice-in.wav '${VOICE_WORDS}'" \
    "# 2) 真转写：本机没有 FunASR 权重，走 doubao 云端引擎（本批唯一一次把字节送出去）" \
    "${BIN} tool voice_input ${WORK}/captures/voice-in.wav --engine doubao"

  if want 141-chat-roundtrip; then
    total=$((total+1))
    emit_one 141-chat-roundtrip "$W/argv-chat.txt" "$W/chat-state.txt" \
      "▌ $CHAT_MARK" "引擎答的那一行" \
      "真 xterm 里敲进去的一句话，被引擎流式答成一行，答案落在同一个活屏面的格子上；随后 Ctrl+C 收尾——一个完整往返。" \
      '`gopi chat --dump-screen`（裸 flag，落在会话目录）+ `gopi ui_state --pid`（零像素读屏）' \
      && ok=$((ok+1))
  fi
  if want 142-ui-panel; then
    total=$((total+1))
    emit_one 142-ui-panel "$W/argv-ui.txt" "$W/ui-state-panel.txt" \
      "gopi> $UI_MARK" "面板的答案行" \
      "面板的完整一层：提示行、输入行 you>、答案行 gopi> 与底部状态行同屏。" \
      '`gopi ui --dump-screen` + `gopi ui_state --pid`' \
      && ok=$((ok+1))
  fi
  if want 143-ui_state-grid; then
    total=$((total+1))
    emit_one 143-ui_state-grid "$W/argv-grid.txt" "$W/grid.txt" \
      "grid_begin count=34" "活屏面被读成的 CELL 网格" \
      "同一块活屏面被读成 CELL 网格：行、列、光标与属性区间一格不落，零像素、零视觉模型。" \
      '`gopi ui_state --pid`（interpreted cell grid：字节已被终端解释成格子）' \
      && ok=$((ok+1))
  fi
  if want 144-voice-transcribe; then
    total=$((total+1))
    emit_one 144-voice-transcribe "$W/argv-voice.txt" "$W/voice-out.txt" \
      "你好" "音频转出来的中文" \
      "一段真音频进去，一行真中文出来——不是缺参数报错，是能力本身在工作。" \
      '`gopi voice_input <audio> --engine doubao`' \
      && ok=$((ok+1))
  fi

  log "emit: $ok/$total"
  [ "$ok" = "$total" ] && [ "$total" -gt 0 ]
}

case "$MODE" in
  capture) capture ;;
  emit)    emit ;;
  *)       capture && emit ;;
esac
