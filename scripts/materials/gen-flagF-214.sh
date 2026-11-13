#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-flagF-214.sh — 为「214-flag-evidence-only」（证据纪律 / 作者自我修正）产生
# 六件套里的 01-input.txt 与 02-output.txt（真跑，逐字节重定向采集，Rule 31）。
#
# 它复跑两条被修正的说法所依赖的 argv：
#   (a) `--model <不存在的名字>` 真跑 → 是不是「静默换默认」；
#   (b) `--settings <空 {} / 可辨认的一份>` → 是不是「叠加」。
# 外加一段竞品对照（cargo / git 的非法值与路径错误），供 05-finding 引用。
#
# 用法: bash scripts/materials/gen-flagF-214.sh <binary> <sandbox-profile-dir> <out-dir>
set -uo pipefail
BIN="${1:?usage: gen-flagF-214.sh <binary> <sandbox> <out-dir>}"
SB="${2:?sandbox profile dir (GOPI_HOME)}"
DIR="${3:?out dir}"
mkdir -p "$DIR"
U="env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER"
# 必须 export：命令块里可能有 `cmd1; cmd2`，行首的 `VAR=x` 前缀只作用于**第一条**命令
# （实测：加 --settings 的那两块因此读到了操作者的真 home），所以把 GOPI_HOME 放进环境。
export GOPI_HOME="$SB"

BLOCKS=(
  "\"$BIN\" --model zz-no-such-model-xyz --prompt \"Reply with exactly: ok\" --timeout 25"
  "\"$BIN\" task list"
  "printf '{}\\n' > $SB/probe-alt-settings.json; \"$BIN\" --settings $SB/probe-alt-settings.json config show"
  "printf '{\"model\":\"probe-model-xyz\",\"tmpdir\":\"$SB/probe-tmp\"}\\n' > $SB/probe-s2.json; \"$BIN\" --settings $SB/probe-s2.json config show"
  "cargo --color bogus --version"
  "git --git-dir=/nonexistent/x log"
)

{
  printf 'cd %s\n' "$SB"
  printf 'GOPI_HOME=%s\n' "$SB"
  for b in "${BLOCKS[@]}"; do printf '$ %s\n' "$b"; done
} > "$DIR/01-input.txt"

: > "$DIR/02-output.txt"
i=0
for b in "${BLOCKS[@]}"; do
  i=$((i + 1))
  printf '$ %s\n\n' "$b" >> "$DIR/02-output.txt"
  ( cd "$SB" && eval "timeout 90 $U $b" ) > "$DIR/.b$i" 2>&1
  rc=$?
  cat "$DIR/.b$i" >> "$DIR/02-output.txt"
  [[ -s "$DIR/.b$i" && -n "$(tail -c1 "$DIR/.b$i")" ]] && printf '\n' >> "$DIR/02-output.txt"
  printf 'exit=%d\n\n' "$rc" >> "$DIR/02-output.txt"
  rm -f "$DIR/.b$i"
done
grep -c '^exit=' "$DIR/02-output.txt"
