// cli_tasks_sweep_worktrees_gate_test.go — the dry-run CONTRACT of
// `gopi task sweep-worktrees`.
//
// The defect this file pins: `--apply --dry-run` printed "(APPLY)" and REMOVED
// worktree registrations, while the sibling verb `task finish-cleanup` refused
// the same argv with rc=1. The cause was a gate wired to the wrong half of the
// verb — `dryRunGate()` decided only whether to take the merger work lock,
// while the engine's destructive field was fed the raw `flags.sweepApply`
// (and the engine derives its own posture from Apply alone). So the tests
// below assert the argv→behaviour contract at BOTH ends: the flag-level gate,
// the parse-time refusal, and the source shape that carries the gate to the
// engine (the place the raw field leaked in).
package main

import (
	"os"
	"strings"
	"testing"
)

// TestSweepWorktreesVerbPostureGate: the three postures, at the flag level.
// `--apply --dry-run` is a PLAN — a contradictory argv may never be
// destructive (the sibling verb's contract, one verb at a time).
func TestSweepWorktreesVerbPostureGate(t *testing.T) {
	cases := []struct {
		name  string
		apply bool
		dry   bool
		want  bool // plan only?
	}{
		{"bare run is a plan", false, false, true},
		{"--dry-run is a plan", false, true, true},
		{"--apply RUNS", true, false, false},
		{"--apply --dry-run stays a plan", true, true, true},
	}
	for _, c := range cases {
		o := &tasksOptions{sweepApply: c.apply, dryRun: c.dry}
		if got := o.dryRunGate(); got != c.want {
			t.Errorf("%s: dryRunGate = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSweepWorktreesVerbRefusesContradictoryArgv: the operator is TOLD that
// --apply --dry-run is contradictory instead of being handed an apply they did
// not ask for — the refusal finish-cleanup already had, on the same argv.
func TestSweepWorktreesVerbRefusesContradictoryArgv(t *testing.T) {
	_, err := parseTasksArgs("sweep-worktrees", []string{"--apply", "--dry-run"})
	if err == nil {
		t.Fatal("--apply --dry-run must be refused at parse time, like finish-cleanup")
	}
	if !strings.Contains(err.Error(), "--dry-run wins") {
		t.Errorf("the refusal must say what actually happens, got %q", err.Error())
	}
	if o := (&tasksOptions{sweepApply: true, dryRun: true}); o.sweepWorktreeRunConflicts() == nil {
		t.Error("the contradiction check must refuse the same pair directly")
	}
	if o := (&tasksOptions{sweepApply: true}); o.sweepWorktreeRunConflicts() != nil {
		t.Error("a plain --apply is not contradictory")
	}
}

// TestSweepWorktreesVerbEachSpellingAloneKeepsItsPosture is the no-regression
// half: the two single spellings must parse and mean exactly what they meant
// before the fix (the default stays a plan; --apply stays the destructive
// opt-in).
func TestSweepWorktreesVerbEachSpellingAloneKeepsItsPosture(t *testing.T) {
	cases := []struct {
		args []string
		plan bool
	}{
		{nil, true},
		{[]string{"--dry-run"}, true},
		{[]string{"--apply"}, false},
	}
	for _, c := range cases {
		opts, err := parseTasksArgs("sweep-worktrees", c.args)
		if err != nil {
			t.Fatalf("sweep-worktrees %v: %v", c.args, err)
		}
		if got := opts.dryRunGate(); got != c.plan {
			t.Errorf("sweep-worktrees %v: planOnly = %v, want %v", c.args, got, c.plan)
		}
	}
}

// TestSweepWorktreesEngineOptionsCarryTheOneGate pins the SHAPE that broke:
// every place this verb builds the engine's options must go through the gate.
// The defect was not a wrong expression — it was a SECOND reader of the raw
// flag, so a future edit that reintroduces `Apply: flags.sweepApply` (or any
// other direct read of the parsed field) fails here instead of silently
// removing registrations again.
func TestSweepWorktreesEngineOptionsCarryTheOneGate(t *testing.T) {
	raw, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	src := string(raw)
	// Both removal faces of the one verb: the registration plan and the
	// run-ground axis.
	for _, fn := range []string{"func runTaskSweepWorktrees(", "func runTaskSweepRunGrounds("} {
		body := sourceFuncBody(t, src, fn)
		if !strings.Contains(body, "!flags.dryRunGate()") {
			t.Errorf("%s must feed the engine's Apply through the ONE gate", fn)
		}
		if strings.Contains(body, "sweepApply") {
			t.Errorf("%s reads the raw --apply field directly again — that is the defect", fn)
		}
	}
}

// sourceFuncBody returns the text of one top-level function, from its
// declaration line to the line that closes it at column 0. It is enough for a
// source-shape assertion (the gate is about WHICH call a function makes), and
// it deliberately fails loudly when the function is absent — a silent empty
// body would make the assertion above pass vacuously.
//
// It lives here rather than beside the close-out face it was first written for:
// that face's test file retired with the `finish-cleanup` verb, and this helper
// is a general source-shape instrument several gates in this package read.
func sourceFuncBody(t *testing.T, src, decl string) string {
	t.Helper()
	i := strings.Index(src, decl)
	if i < 0 {
		t.Fatalf("%s not found in the source under test", decl)
	}
	rest := src[i:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("%s has no recognizable end", decl)
	}
	return rest[:end]
}
