#!/usr/bin/env bash
# Mutation red-proof for the pass-level rebuild invariant (epic
# merger-always-rebuilds). Each mutation is applied to the tree, the pinning
# test is run against it, and the original is restored from the backup. A
# mutation that leaves the test GREEN proves the test is vacuous.
#
# RUN FROM THE REPO ROOT:  bash devdoc/merger-always-rebuilds-mutation-redproof.sh
#
# (bash, not sh: it sources scripts/lib/tmp-ban.sh, which is bash — its
# bounded-loop helper uses the C-style `for ((i=…))` form. `sh` on this host is
# dash and dies on that line, taking the backup setup with it.)
#
# TWO LESSONS ARE BAKED IN, both measured on this epic:
#
#  ① the original revision cd'd to its own directory (devdoc/) and then used
#     repo-root-relative paths, so it died on `cp` before applying a single
#     mutation and every run looked "clean". A red-proof that cannot fail is
#     worse than no red-proof → the root check below refuses to run elsewhere.
#  ② it kept its backups under TMPDIR, which this host's scratch sweeper
#     removed mid-run; restore() then failed SILENTLY (no `set -e`, unchecked
#     cp) while the script still printed "all mutations killed" — leaving the
#     tree mutated. Backups now live beside the repo, every restore is checked,
#     and a failed restore exits non-zero.
set -u

if [ ! -d cmd/agent ]; then
	echo "run me from the repo root (cmd/agent not found in $PWD)" >&2
	exit 2
fi

# Scratch contract: the backups live under the shared, off-`/`, age-GC'd scratch
# queue — never an ambient temp path, never /tmp (scripts/lib/tmp-ban.sh). The
# helper is also what gives us trap-time cleanup of the backup directory.
# shellcheck source=scripts/lib/tmp-ban.sh
. "$(cd "$(dirname "$0")/../scripts/lib" && pwd)/tmp-ban.sh"

# Per-package test budget (Rule 1a/2): a bare `go test` inherits Go's 10m cap,
# which a loaded host trips on cmd/agent — a 600s FAIL that proves nothing about
# the mutation (the same defect scripts/test-budget.sh exists for). Named once.
MUTATION_TEST_TIMEOUT=15m
MUTATION_TEST_PKG=./cmd/agent/

PASS_SRC=cmd/agent/cli_tasks.go
REBUILD_SRC=cmd/agent/cli_tasks_rebuild.go

BACKUP_DIR="$(gopi_scratch_mkdir merger-redproof)" || exit 1
PASS_BAK="$BACKUP_DIR/cli_tasks.go"
REBUILD_BAK="$BACKUP_DIR/cli_tasks_rebuild.go"
cp "$PASS_SRC" "$PASS_BAK" || exit 1
cp "$REBUILD_SRC" "$REBUILD_BAK" || exit 1

restore() {
	cp "$PASS_BAK" "$PASS_SRC" || { echo "RESTORE FAILED for $PASS_SRC" >&2; exit 3; }
	cp "$REBUILD_BAK" "$REBUILD_SRC" || { echo "RESTORE FAILED for $REBUILD_SRC" >&2; exit 3; }
}
cleanup() {
	restore
	# gopi_scratch_cleanup removes the registered backup dir (and any child);
	# BACKUP_DIR is a scratch-queue entry, never an ambient temp path.
	gopi_scratch_cleanup
}
trap cleanup EXIT INT TERM

fail=0
report() {
	# $1 mutation name, $2 the test's exit status (NONZERO = killed = good)
	if [ "$2" -eq 0 ]; then
		echo "MUTATION [$1]: test exit=$2 — SURVIVED (the test is vacuous!)"
		fail=1
	else
		echo "MUTATION [$1]: test exit=$2 — killed"
	fi
}

# --- mutation 1: DELETE the armed invariant ------------------------------
python3 - "$PASS_SRC" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
anchor = "\tdefer func() { mergerPassCloseLoop(repos) }()\n"
assert anchor in s, "anchor for mutation 1 not found"
open(p, "w").write(s.replace(anchor, "", 1))
PY
go test "$MUTATION_TEST_PKG" -run 'TestMergerPassCloseLoopWiredAfterSweep' -count=1 -timeout "$MUTATION_TEST_TIMEOUT" >/dev/null 2>&1
report "delete-armed-invariant" "$?"
restore

# --- mutation 2: replace the DEFER with a statement after the sweep loop --
# (the shipped shape the operator's report exposed: legal Go, green tests, and
# the rebuild is skipped by every early return of the pass)
python3 - "$PASS_SRC" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
arm = "\tdefer func() { mergerPassCloseLoop(repos) }()\n"
loop = "\tfor _, repo := range repos {\n\t\tmergerPassStatsAdd(mergerSweepRepo(repo).Merged)\n\t}\n"
assert arm in s and loop in s, "anchor for mutation 2 not found"
s = s.replace(arm, "", 1).replace(loop, loop + "\tmergerPassCloseLoop(repos)\n", 1)
open(p, "w").write(s)
PY
go test "$MUTATION_TEST_PKG" -run 'TestMergerPassCloseLoopWiredAfterSweep' -count=1 -timeout "$MUTATION_TEST_TIMEOUT" >/dev/null 2>&1
report "defer-demoted-to-tail-statement" "$?"
restore

# --- mutation 3: an EARLY RETURN above the arm ---------------------------
python3 - "$PASS_SRC" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
decl = "func mergerPassOnce(opts *tasksOptions) {\n"
assert decl in s, "anchor for mutation 3 not found"
s = s.replace(decl, decl + "\tif opts == nil {\n\t\treturn\n\t}\n", 1)
open(p, "w").write(s)
PY
go test "$MUTATION_TEST_PKG" -run 'TestMergerPassCloseLoopWiredAfterSweep' -count=1 -timeout "$MUTATION_TEST_TIMEOUT" >/dev/null 2>&1
report "early-return-above-arm" "$?"
restore

# --- mutation 4: BREAK the mechanism (loop body drops the rebuild) -------
python3 - "$REBUILD_SRC" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
old = "\t\tserveRebuildAfterMerge(repo, nil)\n"
assert old in s, "anchor for mutation 4 not found"
open(p, "w").write(s.replace(old, "\t\t_ = repo\n", 1))
PY
go test "$MUTATION_TEST_PKG" -run 'TestMergerPassCloseLoopRebuildsStaleBin' -count=1 -timeout "$MUTATION_TEST_TIMEOUT" >/dev/null 2>&1
report "no-rebuild-in-invariant-body" "$?"
restore

# --- mutation 5: the merge-execute face loses its close loop -------------
python3 - "$PASS_SRC" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
old = """	if tools.MergeExecLanded(rep) {
		mergerCloseLoopAfterLanding(rep.Repo)
	}
"""
assert old in s, "anchor for mutation 5 not found"
open(p, "w").write(s.replace(old, "", 1))
PY
go test "$MUTATION_TEST_PKG" -run 'TestMergeExecFaceRebuildsAfterLanding' -count=1 -timeout "$MUTATION_TEST_TIMEOUT" >/dev/null 2>&1
report "merge-execute-face-unwired" "$?"
restore

echo "--- restored; the source must show no modification vs the backup"
if diff -q "$PASS_BAK" "$PASS_SRC" >/dev/null && diff -q "$REBUILD_BAK" "$REBUILD_SRC" >/dev/null; then
	echo "restore verified byte-identical"
else
	echo "RESTORE MISMATCH — the tree is NOT what it was" >&2
	exit 3
fi
if [ "$fail" -ne 0 ]; then
	echo "RED-PROOF FAILED: at least one mutation survived" >&2
	exit 1
fi
echo "all mutations killed"
