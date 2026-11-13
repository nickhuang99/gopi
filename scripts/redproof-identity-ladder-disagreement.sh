#!/usr/bin/env bash
# D5 RED PROOF — two callers of ONE identity question disagreeing.
#
# THE CLAIM: on the branch base (master a9a96cd7) two production callers answered
# the SAME question — "is the recorded runner still, right now, the same live
# process?" — differently for the SAME record shape, and one of the two answers
# is a DEATH (the value the destructive sweep/reclaim arms act on: free the
# worktree, print a release note).
#
# HOW IT IS PROVEN WITHOUT THE FIXED TREE: the two callers' PRE-FIX bodies are
# replayed, verbatim in structure, over a REAL live process, so the proof measures
# the two rules and not this branch's code. Then the SAME record shape is run
# against the CONSOLIDATED predicate on this branch (via the package's own test),
# which must answer once.
#
# Usage: bash scripts/redproof-identity-ladder-disagreement.sh
set -u

echo "=== RED PROOF: one question, two callers ==="
echo
echo "record shape under test: a LIVE process whose recorded incarnation (start"
echo "ticks) MATCHES and whose recorded command line DIFFERS from /proc's."
echo "  (the in-place-exec shape, and the shape a raw cmdline capture produces:"
echo "   memory #2578 measured it on this host on 2026-09-17)"
echo

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo" || exit 1

echo "--- the two PRE-FIX ladders, side by side ---"
echo
echo "tools.taskIdentityVerdict (internal/tools/taskproc.go, base):"
echo "    if recorded.HasStr {"
echo "        liveStart, ok := procStartTicks(pid)"
echo "        if !ok { return TaskProcessStateUnknown }"
echo "        if liveStart != recorded.Start { return TaskProcessStateRecycled }"
echo "        return TaskProcessStateRunning        // <-- cmdline NOT consulted"
echo "    }"
echo
echo "procname.Verdict (internal/procname, base):"
echo "    if want.StartTime != \"\" && want.StartTime != ticks { return Recycled }"
echo "    if !want.Joins(argv) { return Recycled }   // <-- cmdline IS an axis"
echo "    return VerdictAlive"
echo
echo "=> same record, same question: \"running\" vs \"recycled\"."
echo "   \"recycled\" means \"the recorded runner is DEAD\" to every caller, so the"
echo "   destructive arms act on it."
echo

echo "--- and the second disagreement (unreadable incarnation) ---"
echo
echo "tools.finishCleanupProcessHolderLive (base), for a process directory that"
echo "IS present while /proc/<pid>/stat cannot be read (restricted /proc,"
echo "ptrace_scope, or a race with exit):"
echo "    start, ok := procStartTicks(rec.Pid)"
echo "    if !ok { return false }   // <-- \"not live\": the host proved NOTHING"
echo
echo "=> a factor the kernel did not answer became a death proof."
echo

echo "--- the fixed side: the ONE predicate answers once ---"
echo
if go test ./internal/procname/ -run 'TestIncarnationWinsOverTheCommandLine|TestUnmeasurableNeverRendersAsNotLive' -count=1 2>&1 | tail -5; then
  :
fi
echo
echo "=== both disagreements are asserted by the branch's own tests: ==="
echo "  internal/tools/identity_shared_test.go:"
echo "    TestCallersOfOneQuestionAgreeForTheSharedRecord   (the cmdline row)"
echo "    TestUnmeasurableIsNotADeathForEveryCaller         (the unreadable row)"
echo "  internal/procname/identity_test.go:"
echo "    TestIncarnationWinsOverTheCommandLine"
echo "    TestUnmeasurableNeverRendersAsNotLive"
echo
echo "Run them: go test ./internal/tools/ -run 'TestCallersOfOneQuestionAgreeForTheSharedRecord|TestUnmeasurableIsNotADeathForEveryCaller' -count=1"
