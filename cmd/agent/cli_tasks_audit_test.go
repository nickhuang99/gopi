package main

// F4 of devdoc/merge-resolve-effort-audit.md — the retirement of the dead
// merge-backlog AUDIT + EXECUTE serve pipeline.
//
// WHAT WAS DEAD. `serveMergeBacklogAudit` (lane A's only automatic trigger) and
// `serveMergeExecSweep` (the server-side in-queue merge executor) had no
// production caller: d11af9a6 (2026-09-04, "merger pass is config-driven
// per-repo sweep") deleted the call site and replaced the mechanism with
// MergerRepoSweep. They stayed compiled for 14 days, kept green by their own
// tests — dead automatic dispatch that READS as live is precisely the defect
// class this epic is about (the same finding, one level up, is why the audit
// exists).
//
// WHAT SURVIVES — and is pinned here, because the retirement must not take the
// live faces with it:
//   - `gopi task merge [--submit]` — the manual merge face, whose conflict
//     dispatch is lane A of the resolve-lane audit;
//   - `gopi task audit-merges` — the post-receive hook's inventory/submission
//     path (AuditMergeBacklog, a different summary verb);
//   - `gopi task merge-execute <task-id>` — the operator's seam for executing
//     one merge-backlog row.
//
// The tests below are written against the CONTRACT, not the names: they read
// the production source of the package and assert the retired symbols are gone
// and the live ones are still dispatched, so a future re-wiring has to update
// this file deliberately instead of silently resurrecting a dead sweep.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// cliTasksSource reads a package source file for the absence/presence checks
// below (the subject is the shipped command surface).
func cliTasksSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestDeadServeMergePipelineIsRetired is the retirement's own gate: the dead
// sweep, its two cadence constants and its env knob must not come back. A
// re-wire is a deliberate act — it needs a caller, and this test then has to be
// changed on purpose.
func TestDeadServeMergePipelineIsRetired(t *testing.T) {
	// cli_tasks_sync.go was DELETED (2026-11-05, the cross-node sync family) and
	// its retirement prose went with it, so the scan reads the family page's one
	// remaining source. The gate's subject is unchanged: these symbols must not
	// come back into the production command surface.
	src := cliTasksSource(t, "cli_tasks.go")
	for _, gone := range []string{
		"serveMergeBacklogAudit",   // lane A's dead automatic trigger
		"serveMergeExecSweep",      // the dead in-queue merge executor
		"mergeExecIntervalFromEnv", // its env machinery
		"serveBacklogThrottleMin",
		"serveBacklogLastTick",
		"serveMergeExecLastTick",
		"GOPI_MERGE_AUDIT_INTERVAL", // the audit cadence env (declaration)
	} {
		if strings.Contains(src, gone) {
			// A retirement note may name one of these on purpose (the reader
			// must find the history where the code was); a note lives in a
			// comment, so the check is on CODE occurrences only.
			if strings.Contains(stripCommentsForRetirement(src), gone) {
				t.Errorf("%s is back in the production command surface — F4 retired it "+
					"(no production caller since d11af9a6); if this is a deliberate re-wire, "+
					"give it a real caller, a cadence test, and update this gate", gone)
			}
		}
	}
}

// stripCommentsForRetirement removes // and /* */ comment text so the gate
// judges the shipped code, not the note that documents why the code is gone.
func stripCommentsForRetirement(src string) string {
	var out strings.Builder
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock:
			if strings.Contains(trimmed, "*/") {
				inBlock = false
			}
			continue
		case strings.HasPrefix(trimmed, "/*"):
			if !strings.Contains(trimmed, "*/") {
				inBlock = true
			}
			continue
		case strings.HasPrefix(trimmed, "//"):
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// TestLiveMergeFacesSurviveTheRetirement pins the other half: the three faces
// an operator actually uses are still dispatched. Removing a dead sweep must
// never remove the manual seam that replaced it.
func TestLiveMergeFacesSurviveTheRetirement(t *testing.T) {
	src := cliTasksSource(t, "cli_tasks.go")
	for _, live := range []string{
		"runTaskMergeExecute", // `gopi task merge-execute <task-id>`
		"runTaskAuditMerges",  // `gopi task audit-merges` (hook face)
	} {
		if !strings.Contains(src, live) {
			t.Errorf("%s is gone — the retirement removed a LIVE face, not just the dead sweep", live)
		}
	}
	// The manual merge face lives in its own file (`gopi task merge`).
	if !strings.Contains(cliTasksSource(t, "cli_tasks_merge.go"), "tools.TaskMerge(") {
		t.Error("`gopi task merge` no longer calls the TaskMerge engine — lane A's live trigger is gone")
	}
}

// retiredFlagOffenses returns the CODE lines of src that mention flag — the
// predicate behind TestRetiredFlagIsNotAdvertised. Comments are stripped first
// (stripCommentsForRetirement), because a note explaining that the flag is gone
// must not read as an advertisement of it.
func retiredFlagOffenses(src, flag string) []string {
	var out []string
	for _, line := range strings.Split(stripCommentsForRetirement(src), "\n") {
		if strings.Contains(line, flag) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// TestRetiredFlagIsNotAdvertised is the Rule 5/15 guard for the knob the F4
// retirement left behind, and the red-proof of its own scanner.
//
// THE DEFECT IT CLOSES (measured this branch, 01a0b1b3): the only READER of
// `--no-branch-scan` was the unmerged-branch inventory that F4 deleted, so
// after the retirement the flag was still admitted by taskFlagMember, still
// advertised on `gopi merger -h` (main.go) and in the shipped help testdata,
// and still carried through mergerRestartArgv — while `opts.noBranchScan` was
// read by nothing. That is Rule 15's exact shape ("an option is either accepted
// and obeyed or refused by name, never accepted and ignored") and Rule 21's
// ("a declared knob no code reads"), one level up from the dead pipeline it was
// a sibling of. The flag, the field, the argv token and the field's reader are
// removed together; this test keeps them removed.
func TestRetiredFlagIsNotAdvertised(t *testing.T) {
	// TWO spellings, on purpose: the CLI form an operator types carries the
	// dashes, the CODE form (`taskFlagMember`'s key, `fs.has(...)`) does not —
	// and the defect was exactly the gap between them.
	const flag = "--no-branch-scan"
	const needle = "no-branch-scan"

	// ① The scanner is not blind (Rule 10.4): the pre-retirement shape must be
	// flagged, and a comment about it must not be.
	if got := retiredFlagOffenses("opts.noBranchScan = fs.has(\"no-branch-scan\")\n", needle); len(got) != 1 {
		t.Errorf("the scanner missed the live pre-fix line (got %v) — a green run would be blind", got)
	}
	if got := retiredFlagOffenses("// "+flag+" was retired with its inventory\n", needle); len(got) != 0 {
		t.Errorf("the scanner flags a comment that merely documents the retirement: %v", got)
	}

	// ② It must not be ADMITTED — an accepted-and-ignored flag is the defect.
	if _, err := parseTasksArgs(taskverbs.Merger, []string{flag}); err == nil {
		t.Errorf("`gopi merger %s` still parses — its only reader is gone, so it must be refused by name", flag)
	}
	if taskFlagMember(taskverbs.Merger, needle) {
		t.Errorf("taskFlagMember still admits serve/merger %s", flag)
	}

	// ③ It must not be ADVERTISED on any page or carried in the restart argv.
	// THE THIRD FILE IS GONE (epic merger-delete-dead-impl, 2026-09-19):
	// merger_restart.go carried the restart argv that escaped a retired flag —
	// that argv and the daemon it restarted were deleted together, so the risk
	// this half guards (a flag surviving in the restart argv) has no carrier.
	// The two files that DO carry the parse table are still scanned, and reading
	// a file that does not exist is a t.Fatalf in cliTasksSource, which is why
	// the name had to leave the list rather than fail loudly.
	for _, f := range []string{"main.go", "cli_tasks.go"} {
		if got := retiredFlagOffenses(cliTasksSource(t, f), needle); len(got) != 0 {
			t.Errorf("%s still carries the retired flag in code — remove it or re-wire it with a reader:\n  %s",
				f, strings.Join(got, "\n  "))
		}
	}
	// The OPTIONS FIELD is the other half of "it looked implemented": a struct
	// field nothing sets and nothing reads is the same defect in a quieter form.
	if got := retiredFlagOffenses(cliTasksSource(t, "cli_tasks.go"), "noBranchScan"); len(got) != 0 {
		t.Errorf("cli_tasks.go still declares/assigns noBranchScan: %v", got)
	}
}

// TestStripCommentsForRetirementKeepsCodeHonest is the red-proof for the
// stripper above: it must drop comment text (so the retirement note does not
// self-trip the gate) and keep live code (so a real re-inline is caught).
func TestStripCommentsForRetirementKeepsCodeHonest(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"live code is kept", "func serveMergeBacklogAudit() int { return 0 }\n", true},
		{"line comment is dropped", "// serveMergeBacklogAudit was retired\n", false},
		{"block comment is dropped", "/* serveMergeBacklogAudit */\n", false},
		{"code after a comment line", "// note\nserveMergeExecSweep()\n", true},
	}
	for _, tc := range cases {
		got := strings.Contains(stripCommentsForRetirement(tc.src), "serveMerge")
		if got != tc.want {
			t.Errorf("%s: needle survived=%v, want %v", tc.name, got, tc.want)
		}
	}
}
