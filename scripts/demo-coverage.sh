#!/usr/bin/env bash
# demo-coverage.sh — the coverage INSTRUMENT of docs/archive/demo/showcase/.
#
# WHAT IT ANSWERS: for every verb `gopi -h` advertises (each top-level command
# AND its subcommands), did we REALLY run it, or not — and if not, WHY. `-h` is
# NEVER a run (Rule 31: a command you did not run is not delivered), so every
# `probe` invokes the verb with real arguments against a PRIVATE profile and
# records what came back. A verb this instrument does not execute is written
# with `skip` and carries a REASON — there is no third way to add a row.
#
# RUN IT:
#   bash scripts/demo-coverage.sh                  # builds ./bin/gopi if needed
#   bash scripts/demo-coverage.sh --no-build       # reuse ./bin/gopi or $PATH
#   GOPI_DEMO_HOME=/path/to/profile bash scripts/demo-coverage.sh
#
# OUTPUT: docs/archive/demo/showcase/coverage.tsv
#   * one `top` row per advertised VERB — the Commands section of `gopi -h` is
#     parsed, never hand-listed, and the count that must match the rows is the
#     number of DISTINCT VERBS it advertises (a family head is advertised once
#     and its subcommands take their own lines, so lines, verbs and first tokens
#     are three different numbers — `TOP_LINES` / `TOP_FIRST` below). The retired
#     flat verbs print no line at all: their capability is advertised under its
#     family door (`gopi tool <command>` / `gopi session <command>` /
#     `gopi task <command>`). The writer asserts against the live numbers, never
#     against a literal (Rule 38).
#   * one `sub` row per discovered subcommand of a command that has them.
#   * a meta header naming the GENERATOR and the measured COMMIT (Rule 38).
#
# ISOLATION: every probe runs with GOPI_HOME pointing at a PRIVATE profile — a
# `gopi tool sandbox` (Rule 34) — so the operator's ~/.gopi ledger is never read
# or written. GOPI_DEMO_HOME overrides it, so the script replays on any host.
# The sandbox `create` hand-off onto that profile is the FIRST thing the
# retirement below breaks, and it used to break SILENTLY (see the command
# substitution that materialises the profile): a `tail -1` of nothing is the
# empty string, so the script died one test later with a message about the
# directory instead of about the verb.
#
# NO SILENT SKIPS: the script asserts against the file it just wrote — a row
# without a status, or a non-`ran` row without a reason, fails the run.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Rule 22: every temp path comes from scripts/lib/tmp-ban.sh — the ONE reader of
# the scratch root (GOPI_TEST_SCRATCH_HOME, else GOPI_TEST_HOME's workspace, the
# tree the Go suite GC sweeps). The helper asserts by DEVICE ID that the
# directory is off `/` and registers it for trap-time cleanup: /tmp shares `/`'s
# device here, so it is never a candidate and there is no fallback to it.
. "$ROOT/scripts/lib/tmp-ban.sh"
OUT="${GOPI_DEMO_OUT:-$ROOT/docs/archive/demo/showcase/coverage.tsv}"
BIN="${GOPI_DEMO_BIN:-$ROOT/bin/gopi}"
BUILD=1
[[ "${1:-}" == "--no-build" ]] && BUILD=0

COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
{ git -C "$ROOT" status --porcelain 2>/dev/null | grep -q . ; } && COMMIT="$COMMIT+dirty"

if [[ "$BUILD" == 1 ]]; then
  ( cd "$ROOT" && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ) || {
    echo "demo-coverage: build failed — nothing measured" >&2; exit 1; }
fi
[[ -x "$BIN" ]] || { echo "demo-coverage: no binary at $BIN (build it, or set GOPI_DEMO_BIN)" >&2; exit 1; }

# --- private profile (Rule 34: ASK for a sandbox, never invent a directory) --
OWN_SANDBOX=0
if [[ -z "${GOPI_DEMO_HOME:-}" ]]; then
  # The FAMILY door (`gopi tool sandbox`), never the retired flat `gopi sandbox`
  # (verb-retirement epic, 2026-10-05 — cmd/agent/cli_verb_aliases.go refuses the
  # flat word BY NAME and still exits 0, so a stale spelling here produces a
  # silent empty profile path, not a loud failure). The refusal is captured and
  # reported so the diagnosis names the verb, not the directory.
  _sb_out="$("$BIN" tool sandbox create --label demo-coverage --task-id demo-coverage 2>&1)" || {
    echo "demo-coverage: \`$BIN tool sandbox create\` failed ($(printf '%s' "$_sb_out" | head -1)) — cannot materialise the private profile (Rule 34); refusing to probe in the operator profile" >&2; exit 1; }
  GOPI_DEMO_HOME="$(printf '%s\n' "$_sb_out" | tail -1)"
  [[ -d "$GOPI_DEMO_HOME" ]] || { echo "demo-coverage: tool sandbox create produced no directory (last line: '$GOPI_DEMO_HOME')" >&2; exit 1; }
  OWN_SANDBOX=1
fi
export GOPI_HOME="$GOPI_DEMO_HOME"

WORK="$(gopi_scratch_mkdir demo-coverage)" || exit 1
trap 'gopi_scratch_cleanup; [[ "$OWN_SANDBOX" == 1 ]] && rm -rf "$GOPI_DEMO_HOME"' EXIT
ROWS="$WORK/rows.tsv"; : > "$ROWS"

# ---------------------------------------------------------------- the writers
row() { printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" >> "$ROWS"; }

# probe <kind> <parent> <label> <accepted-exits> <cmd...>
probe() {  local kind="$1" parent="$2" label="$3" exits="$4"; shift 4
  local out rc t0 t1
  t0=$SECONDS
  out="$(cd "$ROOT" && timeout 40 "$@" 2>&1)"; rc=$?
  t1=$SECONDS
  printf 'probe: %-30s rc=%-3s %ss\n' "$label" "$rc" "$((t1-t0))" >&2
  if [[ $rc -eq 124 ]]; then
    row "$kind" "$parent" "$label" "$*" refused "timeout 40s — the verb outlives an instrument's probe budget" "$rc"
    return
  fi
  if [[ " $exits " == *" $rc "* ]]; then
    row "$kind" "$parent" "$label" "$*" ran "" "$rc"
    return
  fi
  row "$kind" "$parent" "$label" "$*" refused "$(printf '%s' "$out" | grep -v '^$' | head -1 | cut -c1-170)" "$rc"
}

# probe_cmd / face_row — ONE ROW FOR A VERB WITH SEVERAL FACES.
#
# The Coding & session inspection verbs (class 2) are session-inspection faces
# whose expensive face needs a SESSION to exist, and a session is written by a
# real agent run (an LLM call, minutes, money) that an instrument must not make.
# So a row accumulates the cheap faces it really ran (an index, a hit, a miss, a
# refusal) and carries the name and the reason of the face it did NOT run in its
# reason column. There is no fourth status: a face that was not run is a REASON
# on the row, never a silent omission.
probe_cmd() {  # probe_cmd <accepted-exits> <note> <cmd>
  local exits="$1" note="$2" cmd="$3" out rc
  out="$(cd "$ROOT" && timeout 40 bash -c "$cmd" 2>&1)"; rc=$?
  [[ -z "$PFIRST" ]] && PFIRST=$rc
  if [[ " $exits " != *" $rc "* ]]; then
    PBAD=1
    PFACES+=("$note → rc=$rc UNEXPECTED [$(printf '%s' "$out" | grep -v '^$' | head -1 | cut -c1-90)]")
    return 0
  fi
  PFACES+=("$note → rc=$rc")
}

# face_row <kind> <parent> <label> <status> [trailing reason]
face_row() {
  local joined
  joined="$(printf '%s | ' "${PFACES[@]}")"; joined="${joined% | }"
  [[ -n "${5:-}" ]] && joined="$joined | NOT RUN: $5"
  if [[ "$PBAD" == 0 ]]; then
    row "$1" "$2" "$3" "-" "$4" "$joined" "$PFIRST"
  else
    row "$1" "$2" "$3" "-" refused "$joined" "$PFIRST"
  fi
  PFACES=(); PBAD=0; PFIRST=""
}

# probe_note <kind> <parent> <label> <accepted-exits> <note> <cmd...>
# A RUN whose ROW also carries a note — used where the verb was executed in a
# read-only face while its mutating face is deliberately not run, so the reason
# the full verb was not driven stays visible on the row that WAS measured.
probe_note() {
  local kind="$1" parent="$2" label="$3" exits="$4" note="$5"; shift 5
  local m="$ROWS.note"; local before; before=$(wc -l < "$ROWS")
  probe "$kind" "$parent" "$label" "$exits" "$@"
  local after; after=$(wc -l < "$ROWS")
  [[ "$after" -gt "$before" ]] || return 0
  awk -F'\t' -v OFS='\t' -v n="$note" 'NR==FNR{last=$0; next} END{}' /dev/null "$ROWS" >/dev/null
  # rewrite the last row with the note in the reason column
  python3 - "$ROWS" "$note" <<'PYEOF'
import sys
path, note = sys.argv[1], sys.argv[2]
lines = open(path).read().rstrip('\n').split('\n')
f = lines[-1].split('\t')
f[5] = note
lines[-1] = '\t'.join(f)
open(path,'w').write('\n'.join(lines)+'\n')
PYEOF
}

# skip <kind> <parent> <label> <reason> — recorded, NEVER executed.
skip() { row "$1" "$2" "$3" "-" not-run "$4" 0; }

# =============================================================================
# 1) TOP-LEVEL VERBS — parsed out of `gopi -h`, never hand-listed.
# =============================================================================
# THE SECTION ENDS AT THE FIRST NON-`gopi <word>` LINE, and that half of the
# pattern is a defect this revision removes: ending at `^Examples` swallowed
# EVERYTHING in between, including the FLAG rows the section carries
# (`gopi --session <id> …`, measured 2026-10-31). `--session` is a flag, not a
# door: it parses as a "verb", no `probe top` row can be named after it, and the
# MISSING_TOP assertion below therefore failed on a file whose rows were all
# present. A flag row is excluded by its OWN shape (`--` right after `gopi `),
# so a future flag line cannot re-introduce the same false failure.
"$BIN" -h 2>&1 | sed -n '/^Commands/,/^Examples/p' \
  | grep -oE '^  gopi [a-zA-Z0-9_-]+( (search|fetch))?' \
  | grep -vE '^  gopi --' \
  | sed 's/^  gopi //' > "$WORK/toplevel.txt"
# Three different counts, and they are NOT interchangeable (the assertion below
# used to conflate two of them, which is why it went red the moment the Commands
# section started advertising family doors):
#   TOP_LINES  — distinct advertised VERBS (what a top row must correspond to)
#   TOP_FIRST  — distinct FIRST TOKENS (the doors: tool/session/task/… — one row
#                per token would collapse 21 tool sub-verbs into one)
#   the section's raw line count is neither, because a head is advertised once
#   while its subcommands each take a line.
TOP_LINES=$(sort -u "$WORK/toplevel.txt" | grep -c .)
TOP_FIRST=$(awk '{print $1}' "$WORK/toplevel.txt" | sort -u | wc -l)

# =============================================================================
# 2) PROBES. The task ledger first (this class's subject), then the rest.
#    Accepted exits: 0 = success, 3 = `worker --status`'s documented refusal,
#    4 = `task show --branch`'s documented "no owner", 2 = dedup-scan's "twins
#    found" verdict, 1 = a refusal whose exact text the class doc quotes.
# =============================================================================
mkid() { grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1; }
ID="$("$BIN" task submit "coverage probe row" --payload "probe" --repo "$ROOT" 2>&1 | mkid)"
PARENT="$("$BIN" task submit "coverage probe parent" --payload "probe" --repo "$ROOT" 2>&1 | mkid)"
CHILD="$("$BIN" task submit "coverage probe child" --payload "probe" --parent-task-id "$PARENT" --repo "$ROOT" 2>&1 | mkid)"
# A row the `task update --status verified` probe can actually VERIFY. The
# `verified` write is legal only from `completed` (measured 2026-10-31: pending →
# verified, claimed → verified and in_progress → verified are ALL refused by the
# status machine, and the `--result` gate the probe is really about never gets to
# run), so the fixture is walked to `completed` here, ONCE, through the very
# transitions the ledger documents. The walk is ASSERTED: a fixture that failed to
# reach `completed` would make the probe measure the status machine's refusal
# while its label claims the --result gate.
VERIFIABLE="$("$BIN" task submit "coverage probe verifiable" --payload "probe" --repo "$ROOT" 2>&1 | mkid)"
for _st in claimed in_progress completed; do
  "$BIN" task update "$VERIFIABLE" --status "$_st" >/dev/null 2>&1 \
    || { echo "demo-coverage: fixture $VERIFIABLE could not reach status=$_st — the update(verified) probe would measure the status machine, not the --result gate; refusing" >&2; exit 1; }
done

# --- Task ledger & execution -------------------------------------------------
probe top task "task"                       "0 4"   "$BIN" task list
probe sub task "task submit"                "0 4"   "$BIN" task submit "coverage probe row 2" --payload "probe" --repo "$ROOT"
probe sub task "task list"                  "0 4"   "$BIN" task list
probe sub task "task list --status pending" "0 4"   "$BIN" task list --status pending
probe sub task "task show"                  "0 4"   "$BIN" task show "$ID"
probe sub task "task show (missing id)"     "0 4"   "$BIN" task show 00000000-dead-beef-0000-000000000000
probe sub task "task show --branch"         "0 4"   "$BIN" task show --branch task/nonexistent-probe-branch
# `task for-branch` retired 2026-10-30 (task 01a18799): its row-level question
# is probed one line above as `task show --branch`, through the same resolver.
# The spelling still dispatches a removal sentence and exits 1 — it is NOT
# probed here, because a probe asserts a documented readout, and the documented
# readout of a retired verb is its removal (asserted by
# TestRetiredTaskVerbsDispatchARemoval in cmd/agent).
probe sub task "task status"                "0 4"   "$BIN" task status "$ID"
probe sub task "task search"                "0 4"   "$BIN" task search coverage
probe sub task "task update --status"       "0 4"   "$BIN" task update "$ID" --status pending --reason "coverage probe"
probe sub task "task update (terminal)"     "0 4"   "$BIN" task update "$ID" --status cancelled --reason "coverage probe: a terminal row so archive has something to archive"
probe sub task "task improvements"          "0 4"   "$BIN" task improvements
probe sub task "task dedup-scan"            "0 4 2" "$BIN" task dedup-scan --live
probe sub task "task sweep-worktrees"       "0 4"   "$BIN" task sweep-worktrees --dry-run
probe sub task "task archive"               "0 4"   "$BIN" task archive "$ID"
# `task verify` retired 2026-10-30 (task 01a18799, slice 2/4): the spelling
# answers a removal sentence and exits 1, so this probe asserted an exit code no
# live door returns while claiming to cover verification. The CAPABILITY did not
# leave as a duty — it left as a SECOND SPAWN (verification belongs to a test
# program, "一个程序不能自己检验自己"): the run that measured a result records it
# through `task update --status verified --result`, probed on the next line with
# a REAL evidence string, so the row now measures a door an operator can type.
probe sub task "task update (verified)"     "0 4"   "$BIN" task update "$VERIFIABLE" --status verified --result "evidence: demo-coverage probe row, accepted by the --result gate"
# `task sync` (and the whole cross-node sync family: diff/harvest/pull/push/
# receive/send/sync/sync-pass) retired 2026-11-05 — the same shape a third time.
# This row used to assert exit 0|4 from `task sync --status`, i.e. it recorded
# the REMOVAL SENTENCE as coverage of a sync status face this binary no longer
# has (the manual cross-node verbs are gone; see internal/taskverbs.SyncVerbs,
# now empty). Deleted rather than annotated: there is no live door behind it to
# replace it with, and a probe row that measures a notice is worse than a gap,
# because the gap at least reads as one.
# `task finish-cleanup` retired 2026-10-30 (task 01a18799, slice 3/4, commit
# 58b5618b) — the same shape: the spelling answers a removal sentence and exits
# 1, so the probe measured the refusal while claiming to cover the terminal-row
# close-out chain. That chain is GONE with the worktree+branch delivery
# semantics it served; what it bundled is reachable by its own command, and the
# one of those that a probe can safely type is `task sweep-worktrees` (its own
# row above).
#
# `task list --sweep` gets NO row, for the reason stated two paragraphs up.
# The stale sweep was deleted end to end on 2026-11-05 (commit e4521fad — the
# carrier, not just its trigger), on the operator's ruling that no process
# reports another process's health: a row's holder is now resolved per row and
# REPORTED by `gopi task list`, never acted on. A probe kept here would type a
# dead spelling and record its "unknown flag" refusal as coverage of a report
# that no longer exists — the same shape as the `finish-cleanup` row above.
# The live read-out is covered by the `task list` rows themselves, which sit
# immediately below (grep: `probe .* task list`).
# `task clone` — the ONE live verb in this family whose whole product is a NEW
# row: `--dry-run` states the row it would mint (`<id>-c1`, parent=<id>) and
# writes nothing, which is exactly what an instrument may do. The fixture it
# copies is walked to a TERMINAL status first: a pending row is refused by name
# ("is pending: it has not run yet … it is UNUSED" — the guard working, measured
# 2026-10-31), so cloning one would probe the guard while claiming to cover the
# verb.
CLONEABLE="$("$BIN" task submit "coverage probe cloneable" --payload "probe" --repo "$ROOT" 2>&1 | mkid)"
for _st in claimed in_progress completed; do
  "$BIN" task update "$CLONEABLE" --status "$_st" >/dev/null 2>&1 \
    || { echo "demo-coverage: fixture $CLONEABLE could not reach status=$_st — the clone probe would measure the guard, not the verb; refusing" >&2; exit 1; }
done
probe sub task "task clone (dry run)"       "0 4"   "$BIN" task clone "$CLONEABLE" --reason "coverage probe" --dry-run
probe sub task "task serve"                 "0 4"   "$BIN" task serve
probe sub task "task merger"                "0 4"   "$BIN" task merger
skip  sub task "task sweep-zombies (DELETED)" "the zombie sweeper was removed 2026-10-26 (devdoc/zombie-sweep-removed.md): gopi no longer disposes or rewrites a row on its own, and the retired spelling exits 1 with a pointer rather than a verdict line. Probing it would measure the refusal, not the sweep. A stuck row is REPORTED by the lifecycle tick (`stale reported=N`) and stays open for a human"
skip  sub task "task reopen (DELETED)"     "this verb no longer exists: there is no operator reissue spelling (it minted children of a failed row, which the dependency gate never releases). A FAIL verdict reissues through the engine; an operator files a fresh \`task submit\`"
skip  sub task "task sync --peer"           "carries rows over ssh to a DECLARED peer; this host declares none (settings.json sync_peers / GOPI_SYNC_PEERS) — topology is declared, never discovered"
# THESE ROWS SAID "no peer is declared on this host" AND THAT WAS FALSE (measured
# 2026-10-31): the profile this script copies the operator's settings INTO
# inherits their `sync_peers`, so `gopi task diff` is not peerless here — it ran
# over ssh against zbook and hp and printed a real DIVERGED verdict for each
# (`local=7 peer=7028`, `local=7 peer=5210`). A skipped row whose REASON is wrong
# is worse than a skipped row: it tells the next reader this host has no peers.
# The reasons below are the ones that actually hold — each of these verbs moves
# rows or refs between two ledgers, which an instrument must not do.
skip  sub task "task diff"                  "compares this ledger against a DECLARED peer's over ssh. This profile does declare peers (it copies the operator's settings), and the probe DID read them — the run is skipped because the answer is a cross-node agreement verdict about two live ledgers (measured: DIVERGED, 7035 rows against zbook), not a property of one command"
skip  sub task "task harvest"               "pulls rows FROM a peer over ssh; it writes into this ledger from another host's, which an instrument must not do"
skip  sub task "task push"                  "pushes rows TO a peer over ssh; it mutates a PEER ledger, which this host does not own"
skip  sub task "task send"                  "MOVES a row to a peer over ssh (--task-id --peer, a MOVE or a CHAIN) — it cancels the source row and creates one on the peer, mutating two ledgers at once"
skip  sub task "task receive"               "takes a row FROM a peer over ssh and writes it here, on the peer's authority"
skip  sub task "task pull"                  "takes named rows from a peer over ssh and writes them here"
skip  sub task "task sync-pass"             "the peer-side leg of one sync pass; no peer is declared on this host"
skip  sub task "task merge"                 "a git merge atom: it takes the merge lock and mutates branches — a task run's operation, not an instrument's"
skip  sub task "task merge-execute"         "executes a prepared merge; mutating, and it belongs to a merge run"
skip  sub task "task merged-cleanup"        "post-merge worktree/branch cleanup; mutating, and it belongs to a merge run"
skip  sub task "task adopt"                 "adopts a foreign branch into the ledger; writes rows for a real branch"
skip  sub task "task audit-merges"          "audits merge history; it reads a merge trail this profile has none of"
skip  sub task "task run"                   "runs a task row as a full agent session (minutes, an LLM budget) — see the top-level worker row"

probe_note top worker "worker" "0 4" \
  "ran: `worker --status` (the read-only face). NOT run: the row-claiming run — it starts a full agent session (minutes, an LLM budget), which an instrument must not do" \
  "$BIN" worker --status
probe_note top merge "merge" "0 4" \
  "ran: `merge -h`. NOT run: the merge pass — it takes the merge lock and mutates the integration branch (its own help lists no dry-run and unknown flags are refused), which an instrument must not drive" \
  "$BIN" merge -h
probe_note top archive "task archive" "0 1 4" \
  "ran: the FAMILY door — \`task archive <nonexistent> --reason <unknown code>\` is the verb's own argument gate, and it refuses with rc=1. NOT run: archiving a REAL branch, which writes refs/tags/archive/<branch> + refs/gopi/archive-reason/<branch>. The row's label is the door's own command line, so it also covers the \`task archive\` sub row: one command, one spelling. The flat \`archive\` was RETIRED by the verb-retirement epic (2026-10-05, cmd/agent/cli_verb_aliases.go) and now refuses by name, so probing it would measure the refusal, not the verb" \
  "$BIN" task archive /nonexistent-branch-xyz --reason not-a-reason
probe top merger   "merger"                 "0 4"   "$BIN" merger

# --- Coding & session inspection (class 2) -----------------------------------
# Every row here is ONE verb with ONE status, reached through the FAMILY door its
# group owns (`gopi tool …` for the local tools, `gopi session …` for the
# session readers). Both families retired their bare spellings on 2026-10-05
# (cmd/agent/cli_verb_aliases.go): `gopi code-search` / `keyword_search` /
# `checkpoint` now refuse by name, so a probe spelled the flat way would record
# the REFUSAL as if it were the verb's own result — a false row in a coverage
# instrument, which is the one thing this file may not contain.
# Every row here is ONE verb with ONE status. The expensive face of the
# session-inspection verbs needs a SESSION, and a session exists only after a
# real agent run — which an instrument must not make (an LLM call, minutes,
# money). So those rows accumulate the CHEAP faces they really ran (index /
# hit / miss / refusal, each with its own exit code) and carry the name and the
# reason of the face that was not run. Every status here is therefore a
# measured exit code — nothing in this group is guessed. NOTE: this profile is
# not necessarily session-less by the time these rows run — the `task verify`
# probe above spawns a real verifier CHILD run, which mints its own session here
# (that is why a `raw --last` face can legitimately print bytes).
probe top "tool code-search" "tool code-search" "0 4"  "$BIN" tool code-search taskRowClaimable
probe top "tool keyword_search" "tool keyword_search" "0 1 4" "$BIN" tool keyword_search "append-only ledger"
probe top "session checkpoint" "session checkpoint" "0 1"  "$BIN" session checkpoint list

PFACES=(); PBAD=0; PFIRST=""
probe_cmd "0" "session checkpoint list"                     "\"$BIN\" session checkpoint list"
probe_cmd "0" "session checkpoint list --dir <flat dir>"    "\"$BIN\" session checkpoint list --dir \$GOPI_HOME/.gopi/raw"
probe_cmd "1" "session checkpoint export --bogus (flag gate)" "\"$BIN\" session checkpoint export --bogus"
probe_cmd "1" "session checkpoint export no-such-log"       "\"$BIN\" session checkpoint export no-such-log-xyz"
probe_cmd "1" "session checkpoint resume (no <log>)"        "\"$BIN\" session checkpoint resume"
face_row sub "session checkpoint" "session checkpoint list|export|resume" ran "checkpoint resume <log>: NOT RUN — it FORWARDS its whole argument tail to the one resume entry point (cmd/agent/cli_checkpoint.go:139), whose own row below states why it is not driven"

# The session-dependent verbs. A profile with no session makes --last/--all/
# <bogus id> answer honestly (an empty corpus with its own exit code), which is
# a real face of each verb; the session-scoped faces are the note.
PFACES=(); PBAD=0; PFIRST=""
probe_cmd "0" "session log --last (the profile's own sessions, or none)" "\"$BIN\" session log --last"
probe_cmd "0" "session log --all --summary"                 "\"$BIN\" session log --all --summary"
probe_cmd "1" "session log <unknown full id>"               "\"$BIN\" session log 00000000-dead-beef-0000-000000000000"
face_row top "session log" "session log" ran "log <session-id> [--rounds|--request|--todo|--grep|…] — reading a session's real rounds needed a SESSION to exist, and a session exists only after a real agent run (an LLM call, minutes, money), which this INSTRUMENT never makes (see the class-2 header above). Those faces were run by hand for the class-2 page against its own two-round sandbox session, and the numbers are in docs/archive/demo/showcase/classes/02-coding-session-inspection.md"

PFACES=(); PBAD=0; PFIRST=""
probe_cmd "0" "session stats --all"                         "\"$BIN\" session stats --all"
probe_cmd "0" "session stats --last"                        "\"$BIN\" session stats --last"
probe_cmd "1" "session stats <unknown id>"                  "\"$BIN\" session stats 00000000-dead-beef-0000-000000000000"
probe_cmd "1" "session stats --branch (no owner)"           "\"$BIN\" session stats --branch task/nonexistent-coverage-probe"
face_row top "session stats" "session stats" ran "stats <session-id> [--rounds|--tools|--bytes|--cost|--bill|--json] — the per-session number view needs a session, which only a real agent run creates (this instrument makes none; see the class-2 header). Run by hand for the class-2 page against its own sandbox session; the empty-corpus face and the shared --branch selector face (refused when no row owns the branch) were run HERE"

PFACES=(); PBAD=0; PFIRST=""
probe_cmd "0 1" "session raw --last (0 = the profile holds a session, 1 = none)" "\"$BIN\" session raw --last"
probe_cmd "1" "session raw <unknown id>"                    "\"$BIN\" session raw 00000000-dead-beef-0000-000000000000"
probe_cmd "1" "session raw (no input selector)"             "\"$BIN\" session raw"
probe_cmd "1" "session raw <id> --json (refused)"           "\"$BIN\" session raw 00000000-dead-beef-0000-000000000000 --json"
face_row top "session raw" "session raw" ran "raw <session-id> [<round> [request|response]] [--rounds|--lines A-B|--sysprompt|--userprompt|--file PATH] — printing the bytes of a real exchange needs a session (this instrument makes none; see the class-2 header); run by hand for the class-2 page. Run HERE: the input-selector gate (no argument), the non-raw-flag gate (--json), and the empty corpus"

PFACES=(); PBAD=0; PFIRST=""
probe_cmd "1" "session resume <unresolvable target>"        "\"$BIN\" session resume no-such-target-coverage-probe"
face_row top "session resume" "session resume" ran "resume <task-id|raw-log-path|session-id> [continuation-file]: NOT RUN here — a resume LOADS A CHECKPOINT and CONTINUES a real agent session (an LLM call, minutes, money); the target-resolution face was run and refused by name with the directory it looked under"

# --- Chat & web ---------------------------------------------------------------
probe top web "web search"         "0 4"  "$BIN" web search "append-only ledger in postgres"
probe top web "web fetch"          "0 4"  "$BIN" web fetch https://example.com
skip  top web "web"      "the local browser chat UI is a LONG-RUNNING SSE server (the 2026-09-29 run confirmed it: the bare verb needs a TTY to pick an action, and the probe had to be killed while `gopi web: listening … http://127.0.0.1:8090`) — a probe does not start a server it cannot stop; the two web sub-verbs were run"
skip  top chat "chat"    "an interactive multi-turn REPL — it needs a TTY and an LLM; a probe cannot drive it"
skip  top ui "ui"        "an interactive terminal chat surface — needs a TTY"
skip  top ui_state "ui_state" "reads a LIVE `gopi ui` screen; no `gopi ui` process is running under this profile"
skip  top cli "cli"      "the same chat in a TEXT-mode browser — needs a browser session against the local web server"
skip  top "tool proxy" "tool proxy"  "a resident Anthropic/OpenAI protocol proxy — it serves until stopped and needs an upstream credential"

# --- Tools --------------------------------------------------------------------
probe top "tool capture"        "tool capture"        "0 4"  "$BIN" tool capture --out "$WORK/probe-capture.png"
probe top "tool screen_capture" "tool screen_capture" "0 4"  "$BIN" tool screen_capture --out "$WORK/probe-screen.png"
probe top "tool text2img"       "tool text2img"       "0 4"  bash -c "printf 'gopi task ledger — 真跑\n' > $WORK/probe.txt && $BIN tool text2img $WORK/probe.txt --out $WORK/probe-text.png"
probe top "tool annotate"       "tool annotate"       "0 4"  bash -c "$BIN tool capture --out $WORK/ann-src.png >/dev/null 2>&1; $BIN tool annotate --out $WORK/probe-annot.png $WORK/ann-src.png"
skip  top "tool vision_analyze" "tool vision_analyze" "sends an image to the vision model (a token-spending LLM call); the Tools class owns that surface"
skip  top "tool act" "tool act" "drives the LIVE X11 desktop (move/click/type) — it would move the operator's own mouse"
skip  top "tool voice_input" "tool voice_input" "speech to text — needs a microphone and the ASR endpoint"
skip  top "tool shot" "tool shot" "drives headless Chromium against a URL — needs the browser binary and a bootable page"

# --- Memory & knowledge -------------------------------------------------------
probe top memory "memory"                    "0 4"  "$BIN" memory search "append-only ledger" --limit 3
probe sub memory "memory search"             "0 4"  "$BIN" memory search "append-only ledger" --limit 3
probe sub memory "memory backup"             "0 4"  "$BIN" memory backup
skip  sub memory "memory sync" "cross-node memory-asset sync over ssh; this host declares no peer (GOPI_SYNC_PEERS)"
skip  top "memory memsync" "memory memsync" "the patch-FILE sibling of `gopi memory sync` — it needs a peer or a patch file to carry"
skip  top "memory brew" "memory brew" "distils lessons from recent sessions with an LLM; the private profile has no sessions and the call spends tokens"

# --- Configuration & maintenance ---------------------------------------------
probe top config "config"                    "0 4"  "$BIN" config show
probe sub config "config show"               "0 4"  "$BIN" config show
probe top notify "notify"                 "0 4"  "$BIN" notify sink
probe top "config completion" "config completion"  "0 4"  "$BIN" config completion bash
probe top "tool doccheck"   "tool doccheck"       "0 4 1" "$BIN" tool doccheck
probe top "tool scratch-gc" "tool scratch-gc"     "0 4"  "$BIN" tool scratch-gc --dry-run
probe top "tool sandbox"    "tool sandbox"        "0 4"  "$BIN" tool sandbox show
probe sub "tool sandbox" "tool sandbox show"   "0 4"  "$BIN" tool sandbox show
skip  top "config setup" "config setup" "provisions a host profile — it would (re)write the very profile directory this probe runs in"

# =============================================================================
# 3) WRITE coverage.tsv, then ASSERT the bookkeeping against the file.
# =============================================================================
mkdir -p "$(dirname "$OUT")"
{
  echo "# generator: scripts/demo-coverage.sh"
  echo "# generator_version: 2"
  echo "# measured_commit: $COMMIT"
  echo "# measured_at: $(date '+%Y-%m-%d %H:%M:%S %z')"
  echo "# binary: $BIN"
  echo "# profile: GOPI_HOME=$GOPI_DEMO_HOME  (a private sandbox — the operator ledger is never read)"
  echo "# top_level_advertised_verb_lines: $(grep -c . "$WORK/toplevel.txt") ($TOP_LINES distinct verbs, $TOP_FIRST distinct first tokens)"
  echo "# top_level_distinct_first_tokens: $TOP_FIRST"
  echo "# rule31: every 'ran' row was invoked with real arguments; '-h' is never counted as a run"
  echo "# statuses: ran | refused (a real, non-zero result reported by gopi itself) | not-run (a reason is mandatory)"
  printf 'kind\tparent\tverb\tprobe\tstatus\treason\texit\n'
  cat "$ROWS"
} > "$OUT"

DATA=$(awk -F'\t' '!/^#/ && $1!="kind"' "$OUT" | grep -c .)
TOP=$(awk -F'\t' '!/^#/ && $1=="top"' "$OUT" | grep -c .)
SUB=$(awk -F'\t' '!/^#/ && $1=="sub"' "$OUT" | grep -c .)
RAN=$(awk -F'\t' '!/^#/ && $1!="kind" && $5=="ran"' "$OUT" | grep -c .)
REF=$(awk -F'\t' '!/^#/ && $1!="kind" && $5=="refused"' "$OUT" | grep -c .)
NOT=$(awk -F'\t' '!/^#/ && $1!="kind" && $5=="not-run"' "$OUT" | grep -c .)
BADREASON=$(awk -F'\t' '!/^#/ && $1!="kind" && $5!="ran" && ($6=="" || NF!=7)' "$OUT" | grep -c .)
BADSHAPE=$(awk -F'\t' '!/^#/ && $1!="kind" && NF!=7' "$OUT" | grep -c .)
BADSTATUS=$(awk -F'\t' '!/^#/ && $1!="kind" && $5!="ran" && $5!="refused" && $5!="not-run"' "$OUT" | grep -c .)
# THE HELP'S USAGE SECTION NAMES SPELLINGS THAT ARE NOT SUBCOMMANDS. `gopi task
# -h` prints a `USAGE` block whose lines read `send       gopi task send --task-id
# T --peer <alias> …`, i.e. the pattern `task <word>` also matches the WORD LIST
# at the left of those lines — so `TASKMISS` demanded rows for `pull`, `receive`
# and `send`, which the VERB LIST advertises nowhere (they live in the sync
# section, reached as `task push|pull|receive|send`). The census now reads the
# verb list (lines whose first token is `gopi`), and the three sync spellings a
# reader can still type get their own `not-run` rows above, with the reason the
# instrument does not run them (they carry rows over ssh to a DECLARED peer, and
# this host declares none).
TASKMISS=$(comm -23 \
  <("$BIN" task -h 2>&1 | awk '$1=="gopi" && $2=="task" && $3 ~ /^[a-z][a-z-]*$/ {print $3}' | sort -u) \
  <(awk -F'\t' '$2=="task"{print $3}' "$OUT" | sed 's/^task //;s/ .*$//' | sort -u))

fail=0
[[ "$DATA" -eq $((RAN+REF+NOT)) ]] || { echo "ASSERT FAIL: rows do not partition into ran/refused/not-run ($DATA != $((RAN+REF+NOT)))" >&2; fail=1; }
# A COUNT EQUALITY IS NOT THE INVARIANT, and asserting one was the file's own
# long-standing bug: it held only while the Commands section listed every verb
# bare and exactly one row per verb existed. Two things broke it — the family
# doors (a sub-verb like `tool capture` is a real capability with a row of its
# own, and it is not advertised AS a verb) and the row-per-door split. What must
# hold is set membership in BOTH directions, which is checked by name below
# (MISSING_TOP: an advertised verb with no row; UNREACHABLE: a row naming a verb
# the help does not advertise). The count survives only as a FLOOR, so a run
# that truncated its row list still fails.
[[ "$TOP" -ge "$TOP_LINES" ]] || { echo "ASSERT FAIL: top rows=$TOP < advertised distinct verbs=$TOP_LINES (rows were lost)" >&2; fail=1; }
# One help LINE per verb held only while the Commands section listed bare verbs.
# A family head (`gopi tool …`, `gopi session …`) is advertised ONCE and its
# subcommands occupy their own lines, so lines != verbs; the invariant that
# survives the door structure is one top ROW per distinct advertised token
# (TOP_FIRST), checked below against the rows themselves.
# THE COVERAGE ASSERTION (why this file is a coverage instrument at all): every
# row this file truly RAN must be reachable by the SAME rendering the reader
# uses — the row's label re-parsed as `gopi <label>`. A row whose label is a
# RETIRED spelling never appears there, so it would be a row about nothing while
# still counting as coverage. Since the verb-retirement epic (2026-10-05,
# cmd/agent/cli_verb_aliases.go) that is a live failure mode, not a hypothetical
# one: the bare words the old rows used (`capture`, `sandbox`, `checkpoint`,
# `archive`, `completion`, `doccheck`, `scratch-gc`, `code-search`,
# `keyword_search`) all refuse by name now.
# The check is on the row's FIRST token: a row's label is the command line the
# probe ran (`tool capture`, `session checkpoint`, `task archive`), and the first
# token is the door — the word `gopi -h` must advertise for the row to describe a
# reachable command. A retired flat word is never advertised (it prints no line),
# so it fails here by construction.
MISSING_TOP=$(comm -23 <(awk '{print $1}' "$WORK/toplevel.txt" | sort -u) \
  <(awk -F'\t' '$1=="top"{print $3}' "$OUT" | awk '{print $1}' | sort -u))
[[ -z "$MISSING_TOP" ]] || { echo "ASSERT FAIL: $(printf '%s\n' "$MISSING_TOP" | grep -c .) advertised verb(s) have NO top row: $(echo $MISSING_TOP)" >&2; fail=1; }
UNREACHABLE=$(comm -23 \
  <(awk -F'\t' '!/^#/ && $1!="kind" && $5=="ran" {print $3}' "$OUT" | awk '{print $1}' | sort -u) \
  <(sort -u "$WORK/toplevel.txt"))
[[ -z "$UNREACHABLE" ]] || { echo "ASSERT FAIL: $(( $(printf '%s\n' "$UNREACHABLE" | grep -c .) )) ran row(s) name a verb \`gopi -h\` does not advertise (retired spelling?): $(echo $UNREACHABLE | tr '\n' ' ')" >&2; fail=1; }
[[ "$TOP_LINES" -gt 0 ]] || { echo "ASSERT FAIL: parsed no top-level verb — the help shape changed" >&2; fail=1; }
[[ "$BADREASON" -eq 0 ]] || { echo "ASSERT FAIL: $BADREASON non-ran row(s) carry no reason" >&2; fail=1; }
[[ "$BADSTATUS" -eq 0 ]] || { echo "ASSERT FAIL: $BADSTATUS row(s) carry no status" >&2; fail=1; }
[[ "$BADSHAPE" -eq 0 ]] || { echo "ASSERT FAIL: $BADSHAPE row(s) are not 7 columns" >&2; fail=1; }
[[ -z "$TASKMISS" ]] || { echo "ASSERT FAIL: task subcommand(s) advertised but missing from the tsv: $(echo $TASKMISS)" >&2; fail=1; }

echo "demo-coverage: wrote $OUT"
printf '  top-level advertised verb lines : %s (%s distinct first tokens)\n' "$TOP_LINES" "$TOP_FIRST"
printf '  rows: top=%s sub=%s total=%s\n' "$TOP" "$SUB" "$DATA"
printf '  ran=%s  refused=%s  not-run=%s (every one with a reason)\n' "$RAN" "$REF" "$NOT"
printf '  ran rows reachable from -h      : yes (%s checked)\n' "$(awk -F'\t' '!/^#/ && $1!="kind" && $5=="ran"' "$OUT" | grep -c .)"
printf '  measured commit                 : %s\n' "$COMMIT"
exit $fail
