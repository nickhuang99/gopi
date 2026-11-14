package main

// RESUME-MODE GRAMMAR tests (cli_worker_resume.go, cmd/agent/main.go).
//
// WHAT IS PINNED, and why at the ARGV level rather than through a full run:
// every assertion here is about the SHAPE the operator typed. A run that
// reached a model would prove "the happy path works" and say nothing about the
// five shapes that must be REFUSED — and those five are where the degradation
// lives: the worker's grammar has always refused an option it does not know
// (devdoc/worker-unknown-flag.md), and a resume selector accepted-and-ignored
// would be exactly the same defect reintroduced through the new vocabulary.
//
// The last test drives the REAL binary for the one property argv parsing cannot
// show: that `--resume` refuses on the resolver's own words and exits NON-ZERO
// without starting the loop (Rule 31 — the surface is executed, not asserted).

import (
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"

	_ "modernc.org/sqlite"
)

// TestWorkerResumeGrammarParses is the ACCEPT half: the mode switch and its two
// selectors are consumed with their operands, in both the `--flag VALUE` and
// the `--flag=VALUE` spell, and they land in the fields the face reads.
func TestWorkerResumeGrammarParses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		argv       []string
		branch     string
		worktree   string
		explicit   string
		wantResume bool
	}{
		{
			name:       "branch selector, separate operand",
			argv:       []string{workerFlagResume, tools.TaskResumeGroundBranchFlag, "feat/x"},
			wantResume: true, branch: "feat/x",
		},
		{
			name:       "branch selector, inline operand",
			argv:       []string{workerFlagResume, tools.TaskResumeGroundBranchFlag + "=feat/x"},
			wantResume: true, branch: "feat/x",
		},
		{
			name:       "worktree selector, separate operand",
			argv:       []string{workerFlagResume, tools.TaskResumeGroundWorktreeFlag, "/w/ground"},
			wantResume: true, worktree: "/w/ground",
		},
		{
			name:       "worktree selector, inline operand",
			argv:       []string{workerFlagResume, tools.TaskResumeGroundWorktreeFlag + "=/w/ground"},
			wantResume: true, worktree: "/w/ground",
		},
		{
			name:       "selector before the mode switch",
			argv:       []string{tools.TaskResumeGroundBranchFlag, "feat/x", workerFlagResume},
			wantResume: true, branch: "feat/x",
		},
		{
			// THE DEFAULT SPELLING: the row rides the verb's ordinary
			// positional, exactly as `gopi worker <task-id>` runs one row —
			// the mode switch is the only thing that changes what it means.
			name:       "the row positional, the default spelling",
			argv:       []string{workerFlagResume, "t-abc123"},
			wantResume: true, explicit: "t-abc123",
		},
		{
			name:       "the row positional before the mode switch",
			argv:       []string{"t-abc123", workerFlagResume},
			wantResume: true, explicit: "t-abc123",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wa, bad := scanWorkerArgs(tc.argv)
			if bad != "" {
				t.Fatalf("argv %v must parse, refused %q", tc.argv, bad)
			}
			if wa.refuse != "" {
				t.Fatalf("argv %v must be an accepted shape, got refusal:\n%s", tc.argv, wa.refuse)
			}
			if wa.resume != tc.wantResume {
				t.Errorf("resume = %v, want %v", wa.resume, tc.wantResume)
			}
			if wa.resumeBranch != tc.branch {
				t.Errorf("resumeBranch = %q, want %q", wa.resumeBranch, tc.branch)
			}
			if wa.resumeWorktree != tc.worktree {
				t.Errorf("resumeWorktree = %q, want %q", wa.resumeWorktree, tc.worktree)
			}
			if wa.explicit != tc.explicit {
				t.Errorf("explicit = %q, want %q (the DEFAULT spelling is the row positional)", wa.explicit, tc.explicit)
			}
		})
	}
}

// TestWorkerResumeGrammarRefusesTheContradictions is the REFUSE half. Each
// shape is its own case because each has its own REPAIR, and the assertion
// checks the sentence names it — a refusal that merely said "invalid arguments"
// would leave the operator to guess which of the two selections to drop.
func TestWorkerResumeGrammarRefusesTheContradictions(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "resume with no row and no ground",
			argv: []string{workerFlagResume},
			want: "needs the row to resume",
		},
		{
			name: "both selectors",
			argv: []string{workerFlagResume, tools.TaskResumeGroundBranchFlag, "feat/x", tools.TaskResumeGroundWorktreeFlag, "/w/g"},
			want: "not a filter pair",
		},
		{
			name: "resume with the daemon",
			argv: []string{workerFlagResume, tools.TaskResumeGroundBranchFlag, "feat/x", workerDaemonFlagLong},
			want: "not a thing",
		},
		{
			name: "resume with a run flag",
			argv: []string{workerFlagResume, tools.TaskResumeGroundBranchFlag, "feat/x", workerFlagAs, "hp:1"},
			want: "ORDINARY named-run path's option",
		},
		{
			// The row AND a ground flag: the row records its own ground, so the
			// two are two answers to one question — refused, never merged.
			name: "the row beside a ground flag",
			argv: []string{workerFlagResume, "t-abc123", tools.TaskResumeGroundBranchFlag, "feat/x"},
			want: "not a filter pair",
		},
		{
			name: "a selector without the mode",
			argv: []string{tools.TaskResumeGroundBranchFlag, "feat/x"},
			want: "means nothing without " + workerFlagResume,
		},
		{
			name: "the worktree selector without the mode",
			argv: []string{tools.TaskResumeGroundWorktreeFlag, "/w/g"},
			want: "means nothing without " + workerFlagResume,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wa, bad := scanWorkerArgs(tc.argv)
			if bad != "" {
				t.Fatalf("argv %v: the FAMILY must accept its own vocabulary and refuse it as a contradiction, got the unknown-word refusal %q", tc.argv, bad)
			}
			if wa.refuse == "" {
				t.Fatalf("argv %v must be refused", tc.argv)
			}
			if !strings.Contains(wa.refuse, tc.want) {
				t.Errorf("refusal must name the repair %q, got:\n%s", tc.want, wa.refuse)
			}
		})
	}
}

// TestWorkerResumeGrammarOperandIsNeverAFlag: a selector whose operand is
// missing must NOT eat the next flag as its value. This is the bug the run-flag
// scanner already carries a scar for (workerRunFlagValue's doc: `--takeover`
// ate the word after it and left the advertised flag unpassable) — the resume
// selectors inherit the same rule rather than repeating the mistake.
func TestWorkerResumeGrammarOperandIsNeverAFlag(t *testing.T) {
	wa, bad := scanWorkerArgs([]string{workerFlagResume, tools.TaskResumeGroundBranchFlag, workerDaemonFlagLong})
	if bad != "" {
		t.Fatalf("unexpected unknown-word refusal: %q", bad)
	}
	if wa.resumeBranch != "" {
		t.Errorf("the selector must not swallow %s as its operand, got branch %q", workerDaemonFlagLong, wa.resumeBranch)
	}
	if wa.daemon != true {
		t.Errorf("%s must still be seen as the daemon switch", workerDaemonFlagLong)
	}
	if wa.refuse == "" {
		t.Error("a selector with no operand must be refused (the mode has no ground)")
	}
	// And the value-carrying spelling is unaffected: `--branch=feat/x` needs no
	// next word at all.
	wa2, bad2 := scanWorkerArgs([]string{workerFlagResume, tools.TaskResumeGroundBranchFlag + "=feat/x"})
	if bad2 != "" || wa2.refuse != "" || wa2.resumeBranch != "feat/x" {
		t.Errorf("inline spelling must be accepted: bad=%q refuse=%q branch=%q", bad2, wa2.refuse, wa2.resumeBranch)
	}
}

// TestWorkerResumeGrammarUnknownWordStillNamesTheAcceptedVocabulary: a word the
// binary does not know is STILL the unknown-argument refusal (the stale-binary
// case), and the message now also teaches the resume spellings — otherwise an
// operator whose launcher is current but whose typo is in a selector would be
// told to upgrade a binary that is not the problem.
func TestWorkerResumeGrammarUnknownWordStillNamesTheAcceptedVocabulary(t *testing.T) {
	msg := workerUnexpectedArgMessage("--resume-ground-typo")
	for _, want := range []string{
		strconv.Quote("--resume-ground-typo"),
		workerFlagResume,
		tools.TaskResumeGroundBranchFlag,
		tools.TaskResumeGroundWorktreeFlag,
		"STALE",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("unexpected-argument message missing %q:\n%s", want, msg)
		}
	}
}

// TestWorkerResumeRefusesThroughTheRealBinary executes the surface (Rule 31):
// `gopi worker --resume …` is run for real — a real binary, a real (fresh,
// empty) ledger — and the properties the acceptance bar names are OBSERVED:
// a NON-ZERO exit, a refusal that names the RESOLUTION failure (not a grammar
// error), and no side effect on the repository or the ledger (nothing was
// created, nothing was claimed).
func TestWorkerResumeRefusesThroughTheRealBinary(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	home, dbPath := workerArgSandbox(t)
	env := workerArgEnv(t, home, dbPath)

	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "unknown branch with no row",
			argv: []string{workerFlagResume, tools.TaskResumeGroundBranchFlag, "feat/never-recorded"},
			want: "no ledger row names",
		},
		{
			name: "a path that is not a work tree",
			argv: []string{workerFlagResume, tools.TaskResumeGroundWorktreeFlag, t.TempDir()},
			want: "is not a work tree",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runGopiIn(t, bin, env, "", append([]string{CliVerbWorker}, tc.argv...)...)
			if code == 0 {
				t.Fatalf("a refusal must exit non-zero, got 0:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("refusal must name %q, got:\n%s", tc.want, out)
			}
			// The refusal is the RESOLVER's, not a usage error: the argv itself
			// was accepted (otherwise a real ground could never be resumed at
			// all), so the operator is being sent to the ground, not to the
			// help page.
			if strings.Contains(out, "unexpected argument") {
				t.Errorf("the argv is legal — the refusal must come from resolution, not the grammar:\n%s", out)
			}
			// NOTHING was created anywhere: the mode never mints ground (the
			// property the whole face rests on), observed at the ledger rather
			// than trusted from the message.
			if rows := ledgerRowCount(t, dbPath); rows != 0 {
				t.Errorf("a refused resume must write nothing to the ledger, found %d row(s)", rows)
			}
		})
	}
}

// ledgerRowCount reads the row count straight from the ledger file under test.
func ledgerRowCount(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger %s: %v", dbPath, err)
	}
	defer db.Close() //stylegate:ignore the probe's own handle, closed immediately
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}
