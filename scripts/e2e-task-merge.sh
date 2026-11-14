#!/bin/bash
# E2E for `gopi task merge` + the one-line post-receive hook (epic 2026-09-01).
# Builds the real binary, then drives the five acceptance items:
#   ① gopi task merge --all on a bare repo with 3 branches (FF / clean
#     diverged / real conflict)
#   ② push triggers the hook → one-line call → whole chain runs
#   ③ idempotent re-push
#   ④ conflict branch's LLM enqueue correct (tags=merge-conflict, no
#     status change — row lands pending, master untouched)
#   ⑤ gopi task merge -h prints help
# Sandbox everywhere: GOPI_HOME is a temp dir; no host state is touched.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
BIN="$ROOT/bin/gopi"
# Scratch root under the shared workspace scratch home (never /tmp — the
# 2026-09-11 exhaustion). See scripts/lib/tmp-ban.sh.
# shellcheck source=lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh"
WORK="$(gopi_scratch_mkdir task-merge)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
if gopi_dry_run_requested "$@"; then
    gopi_dry_run_report e2e-task-merge "$WORK" \
        "build bin/gopi, drive the five task-merge acceptance items (FF/diverged/conflict, hook chain, idempotent re-push, conflict enqueue, -h)" \
        git
    exit $?
fi
pass=0; fail=0
ok()   { echo "  PASS: $1"; pass=$((pass+1)); }
bad()  { echo "  FAIL: $1"; fail=$((fail+1)); }

echo "== build =="
( cd "$ROOT" && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ) || { echo "build failed"; exit 1; }
echo "  build OK"

echo "== ⑤ gopi task merge -h =="
if GOPI_HOME="$WORK/help-home" "$BIN" task merge -h | grep -q 'gopi task merge'; then
  ok "-h prints the merge section"
else
  bad "-h missing the merge section"
fi

GOPI_HOME="$WORK/home"

echo "== fixture: non-bare integration repo (the real zpeer shape: git merge needs a work tree — a bare repo refuses with 'must be run in a work tree') + work clone =="
BARE="$WORK/repo.git"          # name kept; it is the INTEGRATION repo now
REPO="$WORK/repo.wt"           # its work tree — hook + merge run here
GOPI_REPO="$REPO"              # the hook reads this — merge+audit target the fixture repo
GOPI_BIN="$ROOT/bin/gopi"      # the hook runs THE BUILT BINARY
export GOPI_HOME GOPI_REPO GOPI_BIN
git init -q -b master "$REPO"
git -C "$REPO" config user.email t@t
git -C "$REPO" config user.name t
git -C "$REPO" config receive.denyCurrentBranch updateInstead
CLONE="$WORK/clone"
git clone -q "$REPO" "$CLONE"
git -C "$CLONE" config user.email t@t
git -C "$CLONE" config user.name t
git -C "$CLONE" commit --allow-empty -q -m seed
git -C "$CLONE" push -q origin master
mkbr() { # mkbr name file content  (commit on new branch from master)
  git -C "$CLONE" checkout -q -b "$1" master
  printf '%s\n' "$3" > "$CLONE/$2"
  git -C "$CLONE" add "$2"
  git -C "$CLONE" commit -q -m "$1 work"
  git -C "$CLONE" checkout -q master
}
mkbr feat/ff     ff.txt     "ff only"
mkbr feat/div    div.txt    "diverged file"
mkbr feat/conf   shared.txt "branch version"
# the conflict: master edits the SAME file+line on master itself
printf 'master version\n' > "$CLONE/shared.txt"
git -C "$CLONE" add shared.txt
git -C "$CLONE" commit -q -m "master edits shared.txt"
git -C "$CLONE" push -q origin master
git -C "$CLONE" push -q "file://$REPO" feat/ff feat/div feat/conf

echo "== ① gopi task merge --all on the bare repo =="
OUT="$(GOPI_HOME="$GOPI_HOME" "$BIN" task merge --all --repo "$REPO" --db "$GOPI_HOME/memory.db" --submit 2>&1)"
RC=$?
echo "$OUT" | sed 's/^/    /'
echo "  exit=$RC"
echo "$OUT" | grep -q 'feat/ff .*merged'      && ok "feat/ff merged (FF)"      || bad "feat/ff not merged"
echo "$OUT" | grep -q 'feat/div .*merged'     && ok "feat/div merged (clean)"  || bad "feat/div not merged"
echo "$OUT" | grep -qE 'feat/conf .*(conflict|dispatched)' && ok "feat/conf conflict routed" || bad "feat/conf not conflicted"
[ "$RC" -ne 0 ] && ok "exit non-zero on conflict" || bad "exit 0 despite conflict"
BRANCHES="$(git -C "$REPO" for-each-ref --format='%(refname:short)' refs/heads)"
echo "  bare branches: $(echo "$BRANCHES" | tr '\n' ' ')"
echo "$BRANCHES" | grep -q 'feat/ff'  && bad "feat/ff not pruned"  || ok "feat/ff pruned"
echo "$BRANCHES" | grep -q 'feat/div' && bad "feat/div not pruned" || ok "feat/div pruned"
echo "$BRANCHES" | grep -q 'feat/conf' && ok "feat/conf kept (conflict)" || bad "feat/conf wrongly deleted"
git -C "$REPO" show master:div.txt >/dev/null 2>&1 && ok "div.txt landed on master" || bad "div.txt missing on master"
git -C "$REPO" show master:shared.txt | grep -q 'master version' && ok "master untouched by conflict" || bad "master corrupted"

echo "== ④ conflict task in the ledger =="
LIST="$(GOPI_HOME="$GOPI_HOME" "$BIN" task list --db "$GOPI_HOME/memory.db" --status pending 2>&1)"
echo "$LIST" | sed -n '1,6p' | sed 's/^/    /'
echo "$LIST" | grep -q 'resolve merge conflict: feat/conf into master' \
  && ok "merge-conflict task pending" || bad "conflict task missing"
# The row identity is the FULL uuid behind TaskListRowPrefix ("#"), not the
# retired `t-<n>` shape: `grep -oE 't-[0-9]+'` matched nothing, so SHOW_ID was
# empty, `task show ""` printed an error instead of a row, and BOTH tag
# assertions failed while the tags were correct (measured 2026-11-01, task
# 01a19051-f5ec — the tags line itself reads merge-conflict,auto-dispatch).
# Reading the id off the very row this block already selects by summary keeps
# the two tag assertions tied to the row they mean.
SHOW_ID="$(echo "$LIST" | grep 'resolve merge conflict: feat/conf into master' \
  | sed 's/^#\([0-9a-f-]*\).*/\1/' | head -1)"
TAGS="$(GOPI_HOME="$GOPI_HOME" "$BIN" task show "$SHOW_ID" --db "$GOPI_HOME/memory.db" 2>/dev/null | grep -i '^tags' )"
echo "    tags line: $TAGS"
echo "$TAGS" | grep -q 'merge-conflict' && ok "tags carry merge-conflict" || bad "tags missing merge-conflict"
echo "$TAGS" | grep -q 'auto-dispatch'  && ok "tags carry auto-dispatch"  || bad "tags missing auto-dispatch"

echo "== ② push triggers the hook (one-line call → whole chain) =="
HOOK="$ROOT/scripts/git-hooks/post-receive"
grep -c 'gopi task merge --all' "$HOOK" >/dev/null && ok "hook is the one-line trigger" || bad "hook lost the one-line trigger"
# The predicate is "no merge DECISION in bash", not "the word `while` appears
# nowhere" (task 01a19051-f5ec). The v1-v4 loops folded into this file were
# re-implementations of the merge ladder (`git merge`, `git branch -d`, conflict
# triage); the hook's bounded WAIT for its child's exit status is not one of
# those, and the old text predicate could not tell them apart — it failed the
# fix that stops the hook reporting success on a failed merge. So assert on the
# git verbs a second implementation could not avoid.
if grep -qE 'git +(merge|reset|checkout) |git +branch +-[dD] ' "$HOOK"; then
  bad "hook re-implements the merge ladder in bash (Option B: the engine owns landing)"
else
  ok "hook decides no merge itself (the gopi engine owns landing)"
fi
# live-fire: install the hook in the integration repo, push a NEW branch
# (new ref → hook fires). NOTE: a plain-path push between repos sharing the
# local FS is short-circuited by git ("Everything up-to-date", no
# receive-pack) — the file:// URL forces the transport so the hook runs.
mkdir -p "$REPO/.git/hooks" && cp "$HOOK" "$REPO/.git/hooks/post-receive" && chmod +x "$REPO/.git/hooks/post-receive"
git -C "$CLONE" checkout -q -b feat/hooked master
printf 'hooked\n' > "$CLONE/hooked.txt"
git -C "$CLONE" add hooked.txt && git -C "$CLONE" commit -q -m "hook work"
git -C "$CLONE" push -q "file://$REPO" feat/hooked
sleep 5   # the hook runs detached (git auto-merge is quick; give the detached job room)
AUDIT="$REPO/.git/post-receive-audit.log"
[ -f "$AUDIT" ] && tail -4 "$AUDIT" | sed 's/^/    /'
grep -q 'feat/hooked' "$AUDIT" 2>/dev/null && ok "hook ran the merge chain (audit log)" || bad "hook did not run (no audit line)"
git -C "$REPO" for-each-ref --format='%(refname:short)' refs/heads | grep -q 'feat/hooked' \
  && bad "feat/hooked not merged by hook" || ok "feat/hooked merged+pruned by hook"

echo "== ③ idempotent re-push =="
# The re-push really re-sends the ref (a no-op push short-circuits in git);
# the hook then re-runs the sweep: already-merged → branch -d, conflict →
# deduped. Exactly one no-op merge cycle, exit 0.
git -C "$CLONE" push -q "file://$REPO" feat/hooked -f 2>/dev/null
sleep 4   # the hook runs detached
OUT2="$(GOPI_HOME="$GOPI_HOME" "$BIN" task merge --all --repo "$REPO" --db "$GOPI_HOME/memory.db" --submit 2>&1)"
RC2=$?
echo "$OUT2" | sed 's/^/    /'
echo "  exit=$RC2"
echo "$OUT2" | grep -q 'merged=0 deleted=0 dispatched=0' && ok "re-run is a no-op" || bad "re-run not idempotent"
[ "$RC2" -eq 0 ] && ok "exit 0 on clean re-run" || bad "re-run exit non-zero"

echo
echo "=== e2e-task-merge: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ] || exit 1
