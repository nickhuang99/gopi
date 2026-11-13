#!/bin/bash
# Operator-authored bounded follow-ups (no LLM in the loop). Each leg is one product's own CLI.
#
# ⚠ HISTORICAL RECORD (2026-11-08): follow-up B drove `gopi task sync`'s three legs
# on 2026-10-31. Those verbs — and every other manual cross-node verb — were retired
# on 2026-11-05 (internal/taskverbs.SyncVerbs is empty), so leg B now prints the
# removal sentence. The lines stay verbatim: they are what the recorded output came
# from, and re-running this file is a history replay, never a check.
S=/home/nick/.gopi/sessions/01a0eec1-e2a4-7366-bcf3-d2c2e183fe18/strongest
R=$S/raw
H=/home/nick/work/hermes-agent/.venv/bin/hermes
echo "=== follow-up A: hermes M5 worktree workspace, REAL creation+reclaim (throwaway git repo) ==="
FIX=$S/work/m5repo
mkdir -p "$FIX"
( cd "$FIX" && git init -q 2>/dev/null; git -c user.email=p@p -c user.name=probe commit -q --allow-empty -m root 2>/dev/null || true )
echo "fixture repo: $FIX head=$(git -C "$FIX" rev-parse --short HEAD 2>&1)"
B=probe-m5wt
timeout 60 $H kanban boards create $B 2>&1 | head -3
timeout 60 $H kanban boards set-default-workdir $B "$FIX" 2>&1 | head -3
T=$(timeout 90 $H kanban --board $B create "M5-worktree-probe-hermes" --workspace worktree 2>&1 | grep -oE 't_[0-9a-f]+')
echo "T=$T"
echo "--- claim (product resolves + materialises the isolated workspace) ---"
timeout 120 $H kanban --board $B claim "$T" 2>&1 | tail -5
echo "--- fixture repo worktree list AFTER claim ---"
git -C "$FIX" worktree list 2>&1 | head -5
echo "--- product workspaces dir ---"
find /home/nick/.hermes/kanban/boards/$B/workspaces -maxdepth 3 2>/dev/null | head -8
echo "--- product reclaim surface: kanban gc / archive ---"
timeout 90 $H kanban gc -h 2>&1 | head -8
timeout 60 $H kanban --board $B complete "$T" 2>&1 | tail -2
timeout 90 $H kanban --board $B gc 2>&1 | tail -4
echo "--- fixture repo worktree list AFTER gc ---"
git -C "$FIX" worktree list 2>&1 | head -5
timeout 60 $H kanban boards rm $B 2>&1 | tail -2
echo
echo "=== follow-up B: gopi M6 sync PROTOCOL LEG run locally against its own sandbox ledger ==="
GH=$S/gopihome
echo "--- peer side leg: --serve-remote (stdin = initiator delta patch, stdout = this node's envelope) ---"
printf '{"rows":[]}' | GOPI_HOME=$GH timeout 60 /usr/local/bin/gopi task sync --serve-remote 2>&1 | head -6; echo "rc=$?"  #retired-verb-ok: HISTORICAL RECORD of the M6 protocol-leg probe run on 2026-10-31; `task sync` and all its flags retired 2026-11-05 (internal/taskverbs.SyncVerbs empty), so no live door exists to point at and the spelling must stay verbatim for the recorded output to be accountable
echo "--- claim-broadcast leg: --claim-remote ---"
printf '{"id":"none","status":"in_progress"}' | GOPI_HOME=$GH timeout 60 /usr/local/bin/gopi task sync --claim-remote 2>&1 | head -6; echo "rc=$?"  #retired-verb-ok: HISTORICAL RECORD — the claim-broadcast leg of the same probe; the verb retired 2026-11-05 and there is no live replacement to measure
echo "--- initiator status leg ---"
GOPI_HOME=$GH timeout 60 /usr/local/bin/gopi task sync --status 2>&1 | head -4; echo "rc=$?"  #retired-verb-ok: HISTORICAL RECORD — the initiator status leg of the same probe; `task sync` retired 2026-11-05, no live door to substitute
echo
echo "=== follow-up C: claude M3 retry with a longer bounded timeout (420s) ==="
cd $S/work/claude || exit 9
P="$(cat /home/nick/workspace/gopi-01a0eec1e2a47366bcf3d2c2e183fe18/scripts/materials/probe-m3-introspect.txt)"
timeout --signal=KILL 420 /home/nick/.local/bin/claude -p --dangerously-skip-permissions "$P" > $R/probe-m3-introspect.claude.long.out 2>&1
echo "claude M3 long: exit=$? bytes=$(wc -c <$R/probe-m3-introspect.claude.long.out)"
grep -aoE 'M3_VERDICT=[A-Za-z]+' $R/probe-m3-introspect.claude.long.out | tail -2
echo "DONE followups"
