#!/bin/bash
# THE LOOP PROOF (epic fix/merge-rebuild-loop): bin/gopi must be brought current
# by the REAL merger pass (the loop), never by a hand build.
#
# The harness runs the SAME scenario twice — the only difference is which binary
# drives the loop — so the output localizes the defect by DIFF:
#
#   PART A (control) — a pass that COMPLETES. Master advances, the artifact is
#     left behind, `gopi merger --once` runs to completion. Both the pre-fix and
#     the fixed binary heal this (master already rebuilds every configured repo
#     at the pass END), so PART A is the control: it shows the loop is not
#     unconditionally broken, and it pins what the fix must NOT regress.
#
#   PART B (the measured defect) — passes that DIE. The branch is landed by a
#     pass that then hangs and is SIGKILLed (a SIGKILL runs no defer, so the
#     pass-END close loop — the only rebuild site on master — is exactly the step
#     it never reaches). The landing's durable mark is present. A SECOND pass is
#     also SIGKILLed mid-sweep. This is the measured 2026-09-16 shape: the
#     08:50-era daemon left three state files (2436009, 2456472, 2470012) all
#     with passes=0 — every pass killed inside its own sweep — while bin/gopi
#     stayed at 07:17:53, older than the source the 07:29:34 merge introduced.
#
#     On master PART B stays stale (nothing drains the mark; the killed passes
#     never reach their close loop). With the fix the SECOND pass heals it in its
#     first act, because the drain sits at the pass START.
#
# Usage: devdoc/loop-proof-merge-rebuild.sh <path-to-bin/gopi-under-test> [evidence-file]
#
# Everything runs under the SHARED scratch queue — scripts/lib/tmp-ban.sh's
# gopi_scratch_mkdir asserts the path is off `/` (Rule 22) and the trap removes
# it — and under an isolated GOPI_HOME/GOPI_SETTINGS, so the developer's real
# profile is never touched (Rule 6).
set -u
# The scratch contract (Rule 22). Sourced before anything creates a directory:
# the helper is what measures the path against `/` and registers the cleanup.
# shellcheck source=../scripts/lib/tmp-ban.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib" && pwd)/tmp-ban.sh" || {
    echo "loop-proof: scripts/lib/tmp-ban.sh missing — refusing to choose a scratch path" >&2
    exit 2
}
BIN="$(realpath "$1")"
OUT="${2:-/dev/stdout}"
ROOT="$(gopi_scratch_mkdir loop-proof)" || exit 1
trap gopi_scratch_cleanup EXIT INT TERM
REPO="$ROOT/repo"
GHOME="$ROOT/gopi-home"
BINS="$ROOT/bins"          # the wrapper bin dir (PATH) — see the git shim below
say() { printf '%s\n' "$*" | tee -a "$OUT"; }
FAIL=0
note_fail() { say "FAIL: $*"; FAIL=1; }

mkdir -p "$REPO/cmd/x" "$GHOME" "$BINS" || exit 1
cd "$REPO" || exit 1
git init -q -b master || exit 1
git config user.email t@t
git config user.name t
printf 'bin/\n' > .gitignore

cat > cmd/x/main.go <<'GO'
package main

import "fmt"

// rev is injected at build time (-ldflags -X main.rev=...), so the artifact
// answers `--version` with the revision it was built from — the contract
// bin/gopi itself holds.
var rev = "unset"

func main() { fmt.Println(rev) }
GO
cat > go.mod <<'GO'
module example.com/fixture

go 1.24
GO
cat > "$ROOT/build.sh" <<SH
#!/bin/bash
set -eu
cd "$REPO"
mkdir -p bin
go build -ldflags "-X main.rev=\$(git rev-parse HEAD)" -o bin/gopi ./cmd/x
SH
chmod +x "$ROOT/build.sh"
git add .gitignore cmd/x/main.go go.mod && git commit -q -m "c1: fixture source"

# THE git SHIM: a pass-local git that can be made to hang on ONE subcommand, so a
# pass can be put into the window the fix is about and killed there. for-each-ref
# is the trigger because it is the SWEEP's own first branch-listing call and the
# rebuild/drain path never makes it: a pass that hangs there has already been
# through (or not through) the drain, which is exactly the ordering under test.
cat > "$BINS/git" <<'SH'
#!/bin/bash
REAL=/usr/bin/git
"$REAL" "$@"
rc=$?
case "${GOPI_LOOP_PROOF_HANG:-}" in
  always)      sleep 600 ;;   # die on the pass's first git call (pre-fix: the sweep)
  after-drain)                # die only once the drain discharged its mark
    if [ -n "${GOPI_LOOP_PROOF_MARK:-}" ] && [ ! -e "$GOPI_LOOP_PROOF_MARK" ]; then sleep 600; fi ;;
esac
exit $rc
SH
chmod +x "$BINS/git"
export PATH="$BINS:$PATH"

cat > "$GHOME/settings.json" <<JSON
{"repos":{"$REPO":{"build":"$ROOT/build.sh","build_timeout_sec":120}}}
JSON
export GOPI_HOME="$GHOME" GOPI_SETTINGS="$GHOME/settings.json"

ver_of() { "$REPO/bin/gopi" --version | tr -d ' \n'; }

# ─────────────────────────── PART A: a pass that completes ──────────────────
say "############ PART A — control: a pass that COMPLETES ############"
"$ROOT/build.sh"                                   # artifact at the current tip
TIP_A0=$(git rev-parse HEAD)
git checkout -q -b feat/fixture-branch
cat >> cmd/x/main.go <<'GO'

// mergedByTheLoop marks the source change the merged branch introduced: after
// the loop rebuilds, the artifact must be NEWER than this file.
const mergedByTheLoop = true
GO
git add cmd/x/main.go && git commit -q -m "c2: the branch's own source change"
TIP_A1=$(git rev-parse HEAD)                       # the branch tip = merged tip
git checkout -q master
sleep 1
say "branch tip (merged by the loop) = ${TIP_A1:0:12}"
say "artifact before the pass       = $(ver_of | cut -c1-12)  ($(stat -c '%y' bin/gopi))"
"$BIN" merger --once 2>&1 | grep -E '\[rebuild\]|merged=|\[repos\]' | tail -5 | tee -a "$OUT"
say "master after the pass          = $(git rev-parse --short HEAD)"
if [ "$(git rev-parse HEAD)" != "$TIP_A1" ]; then note_fail "the loop did not merge the branch (master $(git rev-parse --short HEAD))"; fi
if [ "$(ver_of)" != "$TIP_A1" ]; then note_fail "PART A: artifact reports $(ver_of | cut -c1-12), want the merged tip ${TIP_A1:0:12}"; else say "OK: PART A — the completing pass rebuilt the artifact to the merged tip"; fi

# ────────────────────── PART B: passes that die (measured) ──────────────────
say ""
say "############ PART B — the measured defect: passes that DIE ############"
# WHICH HANG (GOPI_LOOP_PROOF_MODE): the two binaries need different walls.
#   always      — hang on the pass's FIRST git call. Correct for the pre-fix
#                 binary: it has no start-of-pass drain, so that call IS its
#                 sweep, and the pass dies before anything could rebuild
#                 (measured reality: passes=0).
#   after-drain — hang only once the durable mark is GONE, i.e. after the drain
#                 has rebuilt and discharged it. Correct for the fixed binary: a
#                 pass killed there has already done the work this epic exists
#                 for, which is the whole point of moving the drain to the start.
HANG_MODE="${GOPI_LOOP_PROOF_MODE:-always}"
# The landing is made the way the MEASURED one was: master advanced by a landing
# whose own pass did not complete its close loop. The harness lands it directly
# (a hand merge), which is the most conservative form of the scenario — it gives
# the loop no help at all, so anything that heals it is the mark being drained.
# What makes it the measured shape is the PASSES below: each is SIGKILLed early
# (a SIGKILL runs no defer), leaving the artifact exactly as stale as the
# operator's — and the pass cap means real sessions are killable too.
git checkout -q -b feat/fixture-dies master
cat >> cmd/x/main.go <<'GO'

// landedByADyingPass: this landing happens outside a completing pass, which is
// the measured 2026-09-16 shape.
const landedByADyingPass = true
GO
git add cmd/x/main.go && git commit -q -m "c3: a change landed outside a completing pass"
TIP_B=$(git rev-parse HEAD)
git checkout -q master
"$ROOT/build.sh"                # artifact built at the PRE-merge tip
BIN_BEFORE=$(ver_of)
sleep 1
/usr/bin/git merge --no-edit feat/fixture-dies >/dev/null 2>&1
say "master after the landing       = $(git rev-parse --short HEAD)  (the merged tip ${TIP_B:0:12})"
say "artifact after the landing     = ${BIN_BEFORE:0:12}  (the pre-merge build — the measured staleness)"
if [ "$(ver_of)" = "$TIP_B" ]; then note_fail "PART B is not exercising the defect: the artifact already reports the merged tip"; fi
if [ "$(git rev-parse HEAD)" != "$TIP_B" ]; then note_fail "the landing did not happen"; fi

# The landing's durable mark — what a successor process inherits. Written with
# the primitive's own naming contract (sha256 of the cleaned repo path, 16 hex
# chars; body "<repo>\t<reason>"); internal/tools/rebuild_pending_test.go pins
# that the reader agrees with the primitive on this shape.
KEY=$(printf '%s' "$(realpath "$REPO")" | sha256sum | cut -c1-16)
MARK="$GHOME/.gopi/merger/rebuild-pending-$KEY"   # GopiDir() = $GOPI_HOME/.gopi
export GOPI_LOOP_PROOF_MARK="$MARK"
mkdir -p "$GHOME/.gopi/merger"
printf '%s\t%s\n' "$(realpath "$REPO")" "loop-proof: the landing of ${TIP_B:0:12} while the rebuild waited" > "$MARK"
say "landing's durable mark         = $MARK"

say ""
say "--- pass 1: the first pass is SIGKILLed early (runs no defer); hang mode $HANG_MODE ---"
GOPI_LOOP_PROOF_HANG="$HANG_MODE" timeout -s KILL 25 "$BIN" merger --once >"$ROOT/pass1.out" 2>&1
say "  (pass 1 output, ${#} lines)"; sed -e 's/^/  | /' "$ROOT/pass1.out" | head -12 | tee -a "$OUT"
say "artifact after the killed pass = $(ver_of | cut -c1-12)"
say "--- pass 2: a second killed pass, so no single lucky pass explains the result ---"
GOPI_LOOP_PROOF_HANG="$HANG_MODE" timeout -s KILL 25 "$BIN" merger --once >"$ROOT/pass2.out" 2>&1
say "artifact after killed pass 2   = $(ver_of | cut -c1-12)"
say "bin/gopi mtime                 = $(stat -c '%y' bin/gopi)"
say "cmd/x/main.go mtime            = $(stat -c '%y' cmd/x/main.go)"
say "mark still present?            = $(test -e "$MARK" && echo yes || echo no)"

if [ "$(ver_of)" != "$TIP_B" ]; then
  note_fail "PART B: the artifact still reports $(ver_of | cut -c1-12) after two killed passes — master is at ${TIP_B:0:12}, so every ./bin/gopi run judges pre-merge code (THE OPERATOR'S SYMPTOM)"
else
  say "OK: PART B — a KILLED pass healed the artifact in its first act (the durable mark drained), no hand build involved"
fi
if [ ! bin/gopi -nt cmd/x/main.go ]; then note_fail "PART B: bin/gopi is OLDER than the source the merge introduced"; else say "OK: bin/gopi is NEWER than the source the merge introduced"; fi

say ""
say "===== RESULT: exit=$FAIL (0 = the LOOP rebuilt the artifact in every scenario) ====="
say "scratch root: $ROOT"
exit $FAIL
