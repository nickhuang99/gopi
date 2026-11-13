// worker-run-entry: `gopi worker` is the ONLY command that runs a task row
// (epic worker-run-entry, 2026-09-16).
//
// The rework moved the retired `gopi task run [<id>|next]` selection onto the
// runner: `gopi worker <task-id>` runs exactly one named row in the
// foreground and exits with ITS outcome, `gopi worker [--once]` claims and
// drains the queue as before, and the `task` group keeps only ledger
// management. Three properties have to hold at the CLI seam, and each is a way
// the rework can silently come apart:
//
//	① the retired spelling still DISPATCHES and explains itself — a script or a
//	   recorded argv that types `gopi task run <id>` must hear where the
//	   function went (a pointed removal message, exit 1), never the generic
//	   usage dump and never a silently-different runner;
//	② the ledger surface no longer advertises running — `gopi task -h` teaches
//	   management, and `gopi task <TAB>` offers no `run`;
//	③ a CONTRADICTORY worker argv is refused with a sentence that names the
//	   conflict (--once with a named row, --daemon with a named row, a
//	   named-run flag without one), because every word is known and
//	   "unexpected argument" would send the reader hunting a typo or a stale
//	   binary;
//	④ the ROW IS POSITIONAL (epic worker-positional-taskid, 2026-09-18): the
//	   selector needs no keyword, and the RETIRED `--id <task-id>` spelling is
//	   refused WITH the replacement named — never answered with the stale-binary
//	   hint, which would send the operator to install the version they run.
//
// WHAT THIS FILE DOES NOT ASSERT: that a named row runs a row. That is a real child
// spawn with a model call; it is covered by the e2e rigs that drive the binary
// (cli_tasks_test.go and the singleton guard) and was verified by hand against
// the built binary (devdoc/worker-run-entry.md, the run transcript).
//
// WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md. The argv this
// file builds is driven by a TEST, in a sandbox profile, through the shared
// pinned runners (`runGopiIn`, repo-context sandboxed) — never by an agent in
// the operator's environment. The commands are quoted in assertions, which is
// not a launch instruction.
package main

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

// workerRunEntryRemovedExit is the exit code the retired `gopi task run`
// spelling answers with: 1 — the ordinary "this did not run" code, deliberately
// the same class as a failed ledger verb. It is NOT the usage code (2): the
// argv is well-formed, the function is simply gone, and pretending it is a
// usage error would bury the sentence under a usage dump.
const workerRunEntryRemovedExit = 1

// TestTaskRunIsRetiredAndPointsAtTheWorker pins ①: the retired spelling prints
// the replacement and exits non-zero, and it does so for EVERY argv the retired
// verb accepted — including the flags the dispatch path recorded
// (`--report-to` / `--repo`), because a deployment's recorded command must
// reach the sentence rather than an unknown-flag error.
func TestTaskRunIsRetiredAndPointsAtTheWorker(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	env := guardTestEnv(t, home)()

	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"bare", []string{CliVerbTask, taskverbs.Run}},
		{"with row", []string{CliVerbTask, taskverbs.Run, "t-anything"}},
		{"next (the old claim spelling)", []string{CliVerbTask, taskverbs.Run, taskverbs.Next}},
		{ // The exact shape the dispatch path records as run_cmd.
			"remote report-back argv", []string{CliVerbTask, taskverbs.Run, "t-dispatch", flagReportTo, "zbook", flagRepo, "zbook:~/work/GoPi"}},
		{"takeover", []string{CliVerbTask, taskverbs.Run, "t-held", workerFlagTakeover, workerFlagAs, "taker:1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runGopiIn(t, bin, env, "", tc.argv...)
			if code != workerRunEntryRemovedExit {
				t.Fatalf("`gopi %s` exited %d, want %d (removed)\noutput:\n%s",
					strings.Join(tc.argv, " "), code, workerRunEntryRemovedExit, out)
			}
			// The sentence must name BOTH faces of the replacement: the retired
			// verb could run one row AND claim the next, so a reader has to be
			// able to see which one they meant. Both halves come from their
			// single sources (Rule 14): the sentence is taskRemovedAdvice's,
			// and the row-carrying spelling is the verb constant plus the
			// POSITIONAL placeholder.
			for _, want := range []string{
				"removed",
				taskRemovedAdvice(taskverbs.Run),
				CliVerbWorker + " <task-id>", // "gopi worker <task-id>" — one row
				CliVerbWorker,                // "gopi worker" — the drain
			} {
				if !strings.Contains(out, want) {
					t.Errorf("the removal message does not mention %q:\n%s", want, out)
				}
			}
			// It must NOT be a usage dump: the whole point of the pointed
			// message is that the reader does not have to diff a page.
			if strings.Contains(out, "GoPi task subcommands") {
				t.Errorf("the retired spelling dumped the family usage instead of a pointed message:\n%s", out)
			}
			// And it must not have run anything: no child, no claim.
			if strings.Contains(out, "claimed:") {
				t.Errorf("the retired spelling ran something:\n%s", out)
			}
		})
	}
}

// TestTaskRunHelpIsARemovalPointer pins the usage query on the retired
// spelling: `gopi task run -h` answers with the same sentence and exits 0. A
// non-zero exit would refuse a usage query (the P0-1 contract: -h answers,
// whatever the state of the world), and the family blob would document a verb
// the family no longer has.
func TestTaskRunHelpIsARemovalPointer(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	env := guardTestEnv(t, home)()

	out, code := runGopiIn(t, bin, env, "", CliVerbTask, taskverbs.Run, workerFlagHelp)
	if code != 0 {
		t.Fatalf("`gopi task run -h` exited %d, want 0 (a usage query must answer)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "removed") || !strings.Contains(out, CliVerbWorker+" <task-id>") {
		t.Errorf("`gopi task run -h` did not print the removal pointer:\n%s", out)
	}
	if strings.Contains(out, "GoPi task subcommands") {
		t.Errorf("`gopi task run -h` printed the whole family blob:\n%s", out)
	}
}

// TestTaskFamilyHelpDoesNotTeachRunning pins ②: the ledger group's own help
// must not advertise a way to run a task. It is the doc half of the
// single-entry invariant — a page that teaches a second runner re-creates the
// feature the epic removed, whatever the dispatcher does.
func TestTaskFamilyHelpDoesNotTeachRunning(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	env := guardTestEnv(t, home)()

	out, code := runGopiIn(t, bin, env, "", CliVerbTask, workerFlagHelp)
	if code != 0 {
		t.Fatalf("`gopi task -h` exited %d, want 0\noutput:\n%s", code, out)
	}
	// The SYNOPSIS shape is the forbidden one — a copyable line. A sentence that
	// points elsewhere ("running a task is NOT here: the worker runs rows") is
	// the replacement working, and must stay.
	for _, forbidden := range []string{
		"gopi " + CliVerbTask + " " + taskverbs.Run + " [",
		"gopi " + CliVerbTask + " " + taskverbs.Run + " <",
		"gopi " + CliVerbTask + " " + taskverbs.Run + " next",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("`gopi task -h` still teaches the retired runner (%q) — running a row has ONE entry, `gopi worker <task-id>`:\n%s",
				forbidden, out)
		}
	}
	if !strings.Contains(out, CliVerbWorker) {
		t.Errorf("`gopi task -h` does not say where running a task went:\n%s", out)
	}
}

// TestWorkerRejectsContradictorySelection pins ③: an argv whose words are all
// known but whose MEANING conflicts is refused with a sentence naming the
// conflict — never accepted (where the outcome would depend on an ordering
// nobody can see) and never answered with the stale-binary hint (which would
// send the operator to upgrade a binary that is not the problem).
func TestWorkerRejectsContradictorySelection(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	SandboxRepoContext(t)
	env := guardTestEnv(t, t.TempDir())()

	for _, tc := range []struct {
		name string
		argv []string
		// conflict names the word pair the message must make visible.
		mustMention []string
	}{
		{
			name:        "once and a named row",
			argv:        []string{CliVerbWorker, workerFlagOnce, "t-1"},
			mustMention: []string{workerFlagOnce, "t-1"},
		},
		{
			name:        "daemon and a named row",
			argv:        []string{CliVerbWorker, "t-1", workerDaemonFlag},
			mustMention: []string{workerDaemonFlag, "t-1"},
		},
		{
			name:        "named-run flag without a row",
			argv:        []string{CliVerbWorker, workerFlagAs, "holder"},
			mustMention: []string{workerFlagAs, "<task-id>"},
		},
		{
			name:        "report-to without a row",
			argv:        []string{CliVerbWorker, flagReportTo, "zbook"},
			mustMention: []string{flagReportTo, "<task-id>"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runGopiIn(t, bin, env, "", tc.argv...)
			if code != workerUnexpectedArgExitCode {
				t.Fatalf("`gopi %s` exited %d, want %d (usage-class refusal)\noutput:\n%s",
					strings.Join(tc.argv, " "), code, workerUnexpectedArgExitCode, out)
			}
			for _, want := range tc.mustMention {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal does not name %q — a conflict the reader cannot see is a bug report:\n%s", want, out)
				}
			}
			// The stale-binary hint belongs to an UNKNOWN word, not to a
			// contradiction: every word here is known.
			if strings.Contains(out, "STALE") {
				t.Errorf("a contradictory argv was reported as a stale binary:\n%s", out)
			}
			if strings.Contains(out, "unexpected argument") {
				t.Errorf("a contradictory argv was reported as an unexpected word:\n%s", out)
			}
			if strings.Contains(out, "worker: loop start") {
				t.Errorf("the worker started despite the contradiction:\n%s", out)
			}
		})
	}
}
