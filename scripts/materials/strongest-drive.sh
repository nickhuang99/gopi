#!/usr/bin/env bash
# Four-way strongest-diff probe driver.
# ANTI-CHEAT CONTRACT: this script only cd's, runs the product's own native CLI with the
# probe file expanded verbatim into argv, applies a bounded timeout, redirects output, and
# records $?. It never rewrites the prompt, never wraps the product in another program.
set -u
S=$1; PROD=$2; M=$3
RAW=$S/raw; PROG=$S/progress.log
mkdir -p "$RAW"
for f in "$M"/probe-m*.txt; do
  p=$(basename "$f" .txt)
  out=$RAW/$p.$PROD.out; rc=$RAW/$p.$PROD.rc
  if [ -f "$rc" ]; then echo "$p $PROD SKIP(rc exists)" >> "$PROG"; continue; fi
  PROMPT="$(cat "$f")"            # same file, same shell expansion, for every product
  cd "$S/work/$PROD" || exit 9
  t0=$(date +%s)
  case "$PROD" in
    codex)  timeout --signal=KILL 180 /home/nick/bin/codex exec --skip-git-repo-check -C "$S/work/$PROD" --dangerously-bypass-approvals-and-sandbox "$PROMPT" >"$out" 2>&1 ;;
    claude) timeout --signal=KILL 180 /home/nick/.local/bin/claude -p --dangerously-skip-permissions "$PROMPT" >"$out" 2>&1 ;;
    dsh)    timeout --signal=KILL 180 /home/nick/.local/bin/dsh --profile headless "$PROMPT" >"$out" 2>&1 ;;
    hermes) timeout --signal=KILL 180 /home/nick/work/hermes-agent/.venv/bin/hermes chat -q "$PROMPT" --yolo >"$out" 2>&1 ;;
    gopi)   timeout --signal=KILL 180 /usr/local/bin/gopi --prompt "$PROMPT" >"$out" 2>&1 ;;
  esac
  e=$?; echo "$e" > "$rc"
  echo "$p $PROD exit=$e elapsed=$(( $(date +%s) - t0 ))s bytes=$(wc -c <"$out")" >> "$PROG"
done
echo "DONE $PROD" >> "$PROG"
