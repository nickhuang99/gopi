#!/usr/bin/env bash
# demo-memory-loop.sh — 「记忆是有回路的」九步闭环真跑驱动器。
#
# 形状：在 Rule 34 的私有沙盒 profile 里逐条真跑 `gopi memory` / `gopi brew`，
# 每一步把 argv（verbatim）、stdout+stderr（逐字节合并）、exit code 落盘。
#   * 绝不碰操作者真台账（~/.gopi/memory.db 只读不写，Rule 32）。
#   * `-h` 不算真跑（Rule 31）：每一步都是真 argv + 真输出 + 真退出码。
#   * 输出不编造：跑不出来就写「没跑出来」，退出码照抄。
#
# 用法: bash scripts/demo-memory-loop.sh <evidence_dir>
set -uo pipefail

ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
BIN="$ROOT/bin/gopi"
EVID="${1:?usage: bash scripts/demo-memory-loop.sh <evidence_dir>}"
TASK_ID=01a0eec4-7fd6-7715-9cbb-dbb35323befa
[[ -d "$EVID" ]] || { echo "FATAL: evidence dir $EVID does not exist" >&2; exit 1; }
[[ -x "$BIN" ]] || { echo "FATAL: no binary at $BIN (run: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)" >&2; exit 1; }

STAMP="$(date +%Y%m%d-%H%M%S)"
TOK="LOOP9-$STAMP"          # 正确那条的正文记号
WRONGTOK="WRONG9-$STAMP"    # 错误那条的正文记号（纠错后它仍在 topic 里，诚实保留）
CORRTOK="FIXED9-$STAMP"     # 纠错后正文里的新记号（证明改后的文本可被搜到）
RUNSTAMP="$STAMP"
EV="$EVID/loop-$RUNSTAMP"
mkdir -p "$EV"

# --- 私有沙盒 profile（Rule 34：ASK，不发明目录） ------------------------------
SB="$("$BIN" tool sandbox create --label "memory-loop-$RUNSTAMP" --task-id "$TASK_ID" 2>/dev/null | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
export GOPI_HOME="$SB"
DB="$SB/.gopi/memory.db"

# --- 台账隔离的读数（操作者真台账：只读） ---------------------------------------
OPDB="$HOME/.gopi/memory.db"
op_probe_len() { sqlite3 "$OPDB" "select count(*) from distilled_qa where instr(topic||question||answer,'$1')>0;" 2>/dev/null; }

{
  echo "generator=scripts/demo-memory-loop.sh"
  echo "root=$ROOT"
  echo "measured_commit=$(git -C "$ROOT" rev-parse --short HEAD)"
  echo "date=$(date +%F)"
  echo "sandbox=$SB"
  echo "sandbox_db=$DB"
  echo "gopi_home_export=GOPI_HOME=$SB"
  echo "probe_token=$TOK wrong_token=$WRONGTOK fixed_token=$CORRTOK"
  echo "operator_db=$OPDB"
  echo "operator_db_rows_before=$(sqlite3 "$OPDB" 'select count(*) from distilled_qa;' 2>/dev/null)"
  echo "operator_probe_hits_before=$(op_probe_len "$TOK")"
  echo "operator_wrong_probe_hits_before=$(op_probe_len "$WRONGTOK")"
} > "$EV/run.env"

TRANSCRIPT="$EV/transcript.txt"
: > "$TRANSCRIPT"

STEP_N=0
LAST_OUT=""
# step <slug> <argv...> —— 真跑一条命令，落 01-input / 02-output / 进总 transcript
step() {
  local slug="$1"; shift
  STEP_N=$((STEP_N + 1))
  local n; n="$(printf '%02d' "$STEP_N")"
  local dir="$EV/$n-$slug"
  mkdir -p "$dir"
  {
    echo "cd $ROOT"
    echo "GOPI_HOME=$SB"
    printf '%s' "$BIN"
    for a in "$@"; do printf ' %q' "$a"; done
    printf '\n'
  } > "$dir/01-input.txt"
  {
    printf '$ '
    printf '%s' "$BIN"
    for a in "$@"; do printf ' %q' "$a"; done
    printf '\n'
    "$BIN" "$@" 2>&1
    echo "exit=$?"
  } > "$dir/02-output.txt"
  LAST_OUT="$dir/02-output.txt"
  {
    printf '── 步骤 %s · %s ─────────────────────────────────\n' "$n" "$slug"
    cat "$dir/01-input.txt"
    printf '\n'
    cat "$dir/02-output.txt"
    printf '\n'
  } >> "$TRANSCRIPT"
  echo "step $n-$slug exit=$(tail -1 "$dir/02-output.txt" | sed 's/exit=//')"
}

# ============================== 九步闭环 ======================================
# ① 写入端：一条对照正确的经验
step store-correct memory store \
  --topic "记忆回路探针 $TOK：store 写进去的能不能被搜到" \
  --question "怎么证明一条蒸馏经验真的进了可检索的知识库？" \
  --answer "在私有沙盒里 store 一条正文含 $TOK 的行，然后 search 这个记号：能逐字读回正文，写→读这条边就闭合了。判据是「搜回来的正文里有我写进去的记号」，不是「命令退出码是 0」。" \
  --keywords "memory-loop,闭环,probe" --source "task $TASK_ID"
ID_A="$(grep -oE 'id=[0-9]+' "$LAST_OUT" | head -1 | cut -d= -f2)"

# ② 读回端：同一行，get 给你 search 不给的那一段（eff_conf / fb）
step get-correct-baseline memory get "$ID_A"

# ③ 搜索端：投票之前，这行在搜索里的样子
step search-correct-before memory search "$TOK"

# ④ 投票：一条 useful
step feedback-correct-useful memory feedback "$ID_A" --useful \
  --reason "这一轮真的用上了：闭环取证就是照它做的" --task "$TASK_ID"

# ⑤ 同一行，投票之后再 get 一次 —— conf 与 eff_conf 各自怎么走
step get-correct-after-vote memory get "$ID_A"

# ⑥ 同一查询再 search 一次 —— 搜索行里多出了什么
step search-correct-after-vote memory search "$TOK"

# ⑦ 写入一条【错的】：一句听起来合理、但本轮真跑把它推翻的机制断言。
#    这句错话不是凭空编的——它是知识库里 #6893 那条真记录的断言
#    （记忆原文：「conf 保持 0.85 不变，变的是 eff_conf 0.85→0.86」）。
#    本 run 的真跑（步骤 ④⑤）把它推翻：conf 确实被投票改了（0.85→0.90）。
step store-wrong memory store \
  --topic "记忆回路探针 $WRONGTOK：投票动的到底是哪个字段" \
  --question "gopi memory feedback 票投下去，记录里的 conf 列会不会变？" \
  --answer "不会变。投票只改 eff_conf 这个派生显示字段；conf 是人工通道（memory update --confidence）的专属列，投 useful/useless 都不碰它。所以投票之后 conf 应当保持 0.85 不动，判断投票有没有生效只能看 eff_conf。" \
  --keywords "memory-loop,feedback,投票,conf" --source "task $TASK_ID"
ID_B="$(grep -oE 'id=[0-9]+' "$LAST_OUT" | head -1 | cut -d= -f2)"

# ⑧ 这条错误记忆是「可被搜到、会被当成知识在用」的
step search-wrong memory search "$WRONGTOK"

# ⑨ 投票之前，它的账目基线
step get-wrong-baseline memory get "$ID_B"

# ⑩ 发现它是错的 —— 投一张 useless
step feedback-wrong-useless memory feedback "$ID_B" --useless \
  --reason "实测不符：本轮真跑里 conf 被投票改动了（useful 0.85→0.90、useless 0.85→0.75），eff_conf 是它的派生值；「conf 不变」这句是错的" \
  --task "$TASK_ID"

# ⑪ 投票之后 —— 同一组字段，数值朝反方向走
step get-wrong-after-vote memory get "$ID_B"

# ⑫ 搜索行里的反馈状态也跟着变
step search-wrong-after-vote memory search "$WRONGTOK"

# ⑬ 纠错：把正文改对（不是删掉、也不是留着）
step update-wrong-corrected memory update "$ID_B" \
  --answer "实测纠正：投票确实会改 conf（feedback 是同一条记录上唯一能动 conf 的自动通道：useful +0.05、useless −0.10，各带 floor/cap）。eff_conf 是以 conf 为基数再算出来的：clamp(conf × e^(−Δt/τ) + 检索复验奖励, 0.05, 1)，Δt 从最后一次复验（max(distilled_at, last_retrieved)）起算，所以它随检索次数漂移，也总在 conf 附近。判断投票是否生效要看这两个数各自的走向——只看 conf 会漏掉派生层，只看 eff_conf 会把漂移误当成票的效果。复核记号 $CORRTOK。" \
  --keywords "memory-loop,feedback,投票,conf,eff_conf"

# ⑭ 纠错之后同一条记录（与 ⑨ / ⑪ 逐字对比正文段）
step get-wrong-after-fix memory get "$ID_B"

# ⑮ 修正后的文本可被搜到（新记号）
step search-fixed memory search "$CORRTOK"

# ⑯ 复查：旧记号还在不在？（诚实检查——topic 里的 WRONG 记号仍会被命中，正文已改）
step search-stale-wrong memory search "$WRONGTOK"

# ---------------- 蒸馏端：先在沙盒里放一份真实会话记录 ------------------------
# brew 扫的是 <GOPI_HOME>/.gopi/sessions/*.jsonl；沙盒的 sessions/ 是空的，
# 所以先把一份真会话放进沙盒（这是 brew 的**真输入**，不是造出来的负载）。
SEED_DIR="$SB/.gopi/sessions/demo"
mkdir -p "$SEED_DIR"
SEED_SRC=/home/user/.gopi/sessions/01a0eec1-fca2-7bb5-a4e6-4988cd5944c1/739f49d7-4963-420f-8f72-3757073e44f0.jsonl
cp "$SEED_SRC" "$SEED_DIR/$(basename "$SEED_SRC")"
{ echo "seed_src=$SEED_SRC"; echo "seed_dst=$SEED_DIR/$(basename "$SEED_SRC")";
  echo "seed_bytes=$(stat -c %s "$SEED_DIR/"*.jsonl)"; } >> "$EV/run.env"

# ⑰ brew --dry-run：判定 + 蒸馏，但不落库（stored=0）
step brew-dry-run brew --dry-run --dir "$SEED_DIR" --limit 1
ROWS_BEFORE="$(sqlite3 "$DB" 'select count(*) from distilled_qa;')"

# ⑱ brew 真跑（无 --dry-run）：同一份输入，这次真写库
step brew-live brew --dir "$SEED_DIR" --limit 1
ROWS_AFTER="$(sqlite3 "$DB" 'select count(*) from distilled_qa;')"

# ⑲ 蒸馏之后沙盒台账的全貌（哪几行是 brew 蒸馏出来的）
step ledger-list memory list --limit 20

echo "ID_A=$ID_A ID_B=$ID_B" | tee -a "$EV/run.env"
{
  echo "operator_db_rows_after=$(sqlite3 "$OPDB" 'select count(*) from distilled_qa;' 2>/dev/null)"
  echo "operator_probe_hits_after=$(op_probe_len "$TOK")"
  echo "operator_wrong_probe_hits_after=$(op_probe_len "$WRONGTOK")"
  echo "operator_corrected_probe_hits_after=$(op_probe_len "$CORRTOK")"
  echo "sandbox_ledger_probe_hits=$(sqlite3 "$DB" "select count(*) from distilled_qa where instr(topic||question||answer,'$TOK')>0;" 2>/dev/null)"
  echo "sandbox_ledger_wrong_hits=$(sqlite3 "$DB" "select count(*) from distilled_qa where instr(topic||question||answer,'$WRONGTOK')>0;" 2>/dev/null)"
  echo "sandbox_ledger_fixed_hits=$(sqlite3 "$DB" "select count(*) from distilled_qa where instr(topic||question||answer,'$CORRTOK')>0;" 2>/dev/null)"
  echo "rows_before_live_brew=$ROWS_BEFORE"
  echo "rows_after_live_brew=$ROWS_AFTER"
  echo "live_brew_wrote=$((ROWS_AFTER - ROWS_BEFORE))"
} >> "$EV/run.env"

echo "EVIDENCE=$EV"
echo "TRANSCRIPT=$TRANSCRIPT"
