#!/usr/bin/env bash
# Manual acceptance run for `gopi worker --resume` (Rule 31 evidence; the
# `Ran:` line of devdoc/worker-resume-ground.md).
#
# It builds a throwaway world twice — once per selector spelling — plus the two
# refusals, and prints only what an operator would read: the command, the exit
# code, `git worktree list` and the branch tip before and after.
#
# Everything is under ONE scratch root (`$ROOT`); no operator profile is
# touched (GOPI_HOME / GOPI_MEMORY_DB / GOPI_SETTINGS all point inside it).
set -uo pipefail

ROOT="${1:?usage: run_acceptance.sh <scratch-root> <gopi-binary>}"
GOPI="${2:?usage: run_acceptance.sh <scratch-root> <gopi-binary>}"
STUB_PORT="${STUB_PORT:-18792}"

say() { printf '\n=== %s\n' "$*"; }

run_case() { # run_case <rig> <spelling-flag> <operand-kind>
  local rig="$1" flag="$2" kind="$3"
  local R="$ROOT/$rig" DB="$ROOT/$rig-ledger.db" HOME_DIR="$ROOT/$rig/gopi-home"
  local ROW="01a0d0cc-0000-7000-8000-000000000001" BRANCH="feat/rig-resume"

  run_rig "$R"

  # The ledger row: whose ground this is (name / branch / worktree), the shape a
  # real dispatch writes. The submit creates the ledger; the blob is written
  # with sqlite3 because the submit verb has no --branch/--worktree.
  GOPI_MEMORY_DB="$DB" "$GOPI" task submit "rig row for gopi worker --resume" \
    --task-id "$ROW" --repo "$R/repo" --source human >/dev/null
  sqlite3 "$DB" "UPDATE tasks SET deliverable='{\"repo\":\"$R/repo\",\"branch\":\"$BRANCH\",\"worktree\":\"$R/ground\"}' WHERE task_id='$ROW';"

  # The row's run directory: the checkpoint a resume needs, written by hand
  # because a live first run would need the very worktree this run is about.
  local dir="$HOME_DIR/.gopi/sessions/$ROW"
  mkdir -p "$dir"
  printf '→ REQUEST\n{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[{"role":"user","content":[{"type":"text","text":"[EPIC GOAL]\\nfinish the rig deliverable"}]},{"role":"assistant","content":[{"type":"text","text":"working"}]}]}\n' > "$dir/raw.log"
  printf '{"type":"session_start","sessionId":"sess-rig-%s","rawLogPath":"%s","model":"deepseek-v4-flash","timestamp":"2026-09-24T00:00:00Z"}\n' "$rig" "$dir/raw.log" > "$dir/run.jsonl"
  sqlite3 "$DB" "UPDATE tasks SET status='in_progress' WHERE task_id='$ROW';"

  local operand
  if [ "$kind" = path ]; then operand="$R/ground"; else operand="$BRANCH"; fi

  say "BEFORE ($rig): worktree list"
  git -C "$R/repo" worktree list
  say "BEFORE ($rig): branch tip"
  git -C "$R/repo" rev-parse "$BRANCH"

  say "RUN: gopi worker --resume $flag $operand   (cwd: the repo, NOT the ground)"
  ( cd "$R/repo" && env -i PATH=/usr/bin:/bin HOME=/home/nick \
      GOPI_HOME="$HOME_DIR" GOPI_MEMORY_DB="$DB" GOPI_SETTINGS="$R/nonexistent.json" \
      GOPI_TASK_SYNC=0 GOPI_COMPUTE_NODES= GOPI_SYNC_PEERS= GOPI_HUMAN_TERMINAL=1 \
      ANTHROPIC_AUTH_TOKEN=test-key ANTHROPIC_BASE_URL="http://127.0.0.1:$STUB_PORT" \
      DEEPSEEK_API_KEY=test-key DEEPSEEK_BASE_URL="http://127.0.0.1:$STUB_PORT" \
      "$GOPI" worker --resume "$flag" "$operand" >"$R/out.log" 2>"$R/err.log" )
  echo "EXIT=$?"
  echo "--- stdout (tail)"; tail -3 "$R/out.log"
  echo "--- stderr (the mode's own lines)"; grep -E "resume ground|cleanup|finish-cleanup|EXIT|refus" "$R/err.log" | head -8

  say "AFTER ($rig): git worktree list"
  git -C "$R/repo" worktree list
  say "AFTER ($rig): branch tip (must be UNCHANGED)"
  git -C "$R/repo" rev-parse "$BRANCH"
  say "AFTER ($rig): the ground directory"
  if [ -e "$R/ground" ]; then echo "STILL THERE"; else echo "REMOVED (as required)"; fi
}

run_refusal() { # run_refusal <label> <argv...>
  local label="$1"; shift
  say "REFUSAL: $label — gopi $*"
  ( cd "$ROOT/rig-branch/repo" && env -i PATH=/usr/bin:/bin HOME=/home/nick \
      GOPI_HOME="$ROOT/rig-branch/gopi-home" GOPI_MEMORY_DB="$ROOT/rig-branch-ledger.db" \
      GOPI_SETTINGS="$ROOT/rig-branch/nonexistent.json" GOPI_TASK_SYNC=0 GOPI_HUMAN_TERMINAL=1 \
      "$GOPI" "$@" 2>&1 | tail -3 )
  echo "EXIT=${PIPESTATUS[0]}"
}

run_rig() { # run_rig <rig-root>
  local R="$1"
  git init -q --bare "$R/origin.git" 2>/dev/null
  mkdir -p "$R/repo" && cd "$R/repo"
  git init -q .
  git config user.email gopi@localhost; git config user.name gopi
  echo hello > README.md; git add README.md; git commit -qm initial
  git remote add origin "$R/origin.git" 2>/dev/null; git push -q origin HEAD:master
  git checkout -q -b feat/rig-resume
  echo "the rig deliverable" > deliverable.txt; git add deliverable.txt
  git commit -qm "rig: the deliverable work"
  git push -q origin feat/rig-resume
  git checkout -q master
  git worktree add -q "$R/ground" feat/rig-resume
  cd - >/dev/null
}

say "gopi binary under test"
"$GOPI" --version 2>&1 | head -2 || true

run_case rig-branch --branch branch
run_case rig-path   --worktree path
run_refusal "unknown branch"      worker --resume --branch feat/never-recorded
run_refusal "nonexistent path"    worker --resume --worktree "$ROOT/definitely-absent-dir"
# The operand is a path under the ONE scratch root this script was given, and
# that root is asserted off the `/` partition before anything runs (Rule 22);
# a spelled absolute path proved nothing about where it pointed.
run_refusal "the MAIN checkout"   worker --resume --worktree "$ROOT/rig-branch/repo"
run_refusal "no selector"         worker --resume
run_refusal "both selectors"      worker --resume --branch b --worktree /p

say "FINAL: every rig's registrations and branches (nothing left behind)"
for rig in rig-branch rig-path; do
  echo "-- $rig"; git -C "$ROOT/$rig/repo" worktree list; git -C "$ROOT/$rig/repo" branch --format='%(refname:short)'
done
