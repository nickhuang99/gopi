package main

// cli_tasks_bare_scope_test.go — the TWO `gopi task` verbs whose BARE spelling
// used to leave the operator staring at a silent terminal.
//
// MEASURED (this host, self-built HEAD binary 6c48f7ed1, 2026-11-06):
//
//	$ timeout 5 ./bin/gopi task dedup-scan </dev/null ; echo $?
//	124                      # zero output; the pairwise ladder is quadratic
//	$ timeout 5 ./bin/gopi task merge </dev/null ; echo $?
//	124                      # took the merge lock, then a 300s build gate on
//	                         # the operator's LIVE /home/nick/work/gopi
//
// Neither hang is a "slow machine" story: `dedup-scan` with an explicit
// `--live` finishes in 0.46s (19 rows), and `merge` names its work only after
// the lock and the repos-table resolution. Their cost is decided by SCOPE,
// and scope was the one thing the bare spelling never asked for — so the CLI
// face now demands it, through the SAME refusal shape the rest of the family
// already prints ("missing required parameter: …", exit 1).
//
// The reverse arms below are not decoration: making `list` or `improvements`
// refuse would be the same defect mirrored, so a bare `task list` must still
// exit 0 and a scoped `dedup-scan` must still run.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

// TestTaskBareScopeVerbsRefuseWithoutOne pins the PARSER half: the two verbs
// answer a bare argv with the family's missing-parameter refusal and answer a
// SCOPED argv with no refusal at all.
//
// The positive arms are parse-level on purpose where the verb would ACT:
// driving `task merge <branch>` through the binary lands a branch and runs the
// repo's declared build (demo-coverage.sh skips the same verb for the same
// reason), so the "the capability is still there" half is proven on the parser
// for merge and on the binary for dedup-scan.
func TestTaskBareScopeVerbsRefuseWithoutOne(t *testing.T) {
	for _, verb := range []string{taskverbs.DedupScan, taskverbs.Merge} {
		t.Run(verb+"/bare", func(t *testing.T) {
			_, err := parseTasksArgs(verb, nil)
			if err == nil {
				t.Fatalf("`gopi task %s` with no argument must be REFUSED — the bare spelling is "+
					"the unbounded one (see this file's header for the measurement)", verb)
			}
			if !strings.Contains(err.Error(), "missing required parameter") {
				t.Errorf("the refusal must keep the family's lead (scripts and the other verbs' "+
					"tests match on it), got: %v", err)
			}
		})
	}

	// SCOPED spellings still parse — the refusal must be about the ABSENT
	// scope, never a closed door.
	for _, probe := range []struct {
		verb string
		args []string
	}{
		{taskverbs.DedupScan, []string{"--live"}},
		{taskverbs.DedupScan, []string{"--db", "/nonexistent/probe.db"}},
		{taskverbs.Merge, []string{"--all"}},
		{taskverbs.Merge, []string{"--repo", "/nonexistent/probe-repo"}},
		{taskverbs.Merge, []string{"feat/probe-branch"}},
	} {
		if _, err := parseTasksArgs(probe.verb, probe.args); err != nil {
			t.Errorf("`gopi task %s %s` must still parse (the scope is explicit): %v",
				probe.verb, strings.Join(probe.args, " "), err)
		}
	}
}

// TestTaskBareScopeVerbsRefuseOnTheRealBinary is the Rule 31 half: the argv an
// operator types, against the built binary, with the exit code and the first
// line of what they see.
//
// A timeout here is a FAILURE with its own sentence: the defect this test
// exists for was a process that produced nothing for minutes, and a probe that
// silently waited would report the same green either way.
func TestTaskBareScopeVerbsRefuseOnTheRealBinary(t *testing.T) {
	sb := g1NewSandbox(t)

	for _, verb := range []string{taskverbs.DedupScan, taskverbs.Merge} {
		t.Run(verb, func(t *testing.T) {
			out, code := sb.g1Run(t, "task", verb)
			if code == 0 {
				t.Fatalf("`gopi task %s` with no argument exited 0 — it ran the unbounded arm:\n%s", verb, out)
			}
			if code != 1 {
				t.Errorf("`gopi task %s` exit=%d, want 1 (the family's usage-refusal status):\n%s", verb, code, out)
			}
			first, _, _ := strings.Cut(out, "\n")
			if !strings.Contains(first, "missing required parameter") {
				t.Errorf("`gopi task %s` first line = %q, want the missing-parameter refusal", verb, first)
			}
		})
	}

	// REVERSE ARM ①: a scoped audit still RUNS (exit 0 = no twins, 2 = twins
	// found; both are the audit's documented answers and neither is a refusal).
	out, code := sb.g1Run(t, "task", taskverbs.DedupScan, "--"+taskDedupScanLiveFlag)
	if code != 0 && code != 2 {
		t.Fatalf("`gopi task dedup-scan --live` (sandbox ledger) exit=%d, want 0 or 2 — the audit "+
			"must still run when its scope is named:\n%s", code, out)
	}
	if !strings.Contains(out, "rows scanned") {
		t.Errorf("the scoped audit printed no summary line:\n%s", out)
	}

	// REVERSE ARM ②: the verbs whose bare spelling IS a complete instruction
	// keep working. This is the guard against "fix the hang by refusing
	// everything": `list` means "list everything" and `improvements` means
	// "mine the ledger", so a refusal there would be the same defect mirrored.
	lout, lcode := sb.g1Run(t, "task", taskverbs.List, "--db", sb.db)
	if lcode != 0 {
		t.Errorf("`gopi task list` exit=%d, want 0 — its bare spelling is a complete instruction:\n%s", lcode, lout)
	}
}
