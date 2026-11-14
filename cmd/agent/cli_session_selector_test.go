// The three SESSION verbs' deliverable selector at the CLI entry: `gopi session log
// --branch B|--worktree PATH`, `gopi session raw …`, `gopi session stats …` address the session
// by what an operator standing in a task worktree actually holds (`git branch
// --show-current` / `pwd`), the same two addresses `gopi task show` and `gopi
// worker --resume` already take.
//
// Three things are pinned here, in the order an invocation meets them:
//
//  1. ADMISSION + PARSE — the flags are accepted on exactly the three session
//     verbs (and refused elsewhere, the cross-verb gate intact), both spellings
//     (`--branch X` and `--branch=X`) are read, and the "two inputs" shapes are
//     refused by NAME (never silently preferring one — Rule 15);
//  2. THE SUBSTITUTION — the resolved session id is what the verb's own
//     dispatch receives, so every view flag works through the selector;
//  3. THE FAILURES — no owner, a row with no session yet, and two inputs each
//     fail the verb loudly instead of falling back to a listing.
//
// The row → session step is unit-tested in internal/tools
// (task_deliverable_session_test.go); what this file pins is the WIRING, driven
// through the REAL binary for the end-to-end arms (Rule 31 is about the surface,
// not the seam) and in-process for the parse arms.

package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// sessionSelectorRow is the row the end-to-end arm addresses: a live row whose
// deliverable records the branch/worktree and whose session_id is the FIXTURE
// session, so `--branch`/`--worktree` and the fixture id must render the same
// output.
const (
	sessionSelectorTaskID = "t-session-verb-selector"
	sessionSelectorBranch = "task/session-verb-selector"
)

// sessionSelectorSeedRow files that row into the sandbox ledger through the
// tools layer's own writer (Rule 32: never a hand-rolled schema).
func sessionSelectorSeedRow(t *testing.T, dbPath, worktree, sessionID string) {
	t.Helper()
	t.Setenv(config.MemoryDBEnv, dbPath)
	if _, err := tools.TaskNew("session verb selector fixture", sessionSelectorTaskID, sessionID, ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	blob := `{"branch":"` + sessionSelectorBranch + `","worktree":"` + worktree + `"}`
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		tools.TaskStatusInProgress, blob, sessionSelectorTaskID); err != nil {
		t.Fatalf("stamp row: %v", err)
	}
}

// sessionSelectorStampRow files one row into the SANDBOX ledger: the row is
// created through the tools layer's own writer and its status/deliverable are
// then stamped raw, exactly as sessionSelectorSeedRow does.
//
// It takes the ledger PATH from its caller on purpose: the env pin belongs to
// the caller (which owns the sandbox), so this helper resolves no path of its
// own — the shape Rule 32's gate is written around.
func sessionSelectorStampRow(t *testing.T, dbPath, taskID, summary, status, deliverable string) {
	t.Helper()
	if _, err := tools.TaskNew(summary, taskID, "", ""); err != nil {
		t.Fatalf("TaskNew %s: %v", taskID, err)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		status, deliverable, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// sessionSelectorSandbox is the g4 sandbox plus the ledger the selector needs:
// a GOPI_HOME holding the REAL fixture session + raw log (the same corpus the
// log/stats/raw goldens use) and a ledger whose row points at that session.
type sessionSelectorSandbox struct {
	g4Sandbox
	worktree string
}

func newSessionSelectorSandbox(t *testing.T) sessionSelectorSandbox {
	t.Helper()
	sb := g4NewSandbox(t)
	worktree := filepath.Join(t.TempDir(), "gopi-session-verb-selector")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	sessionSelectorSeedRow(t, filepath.Join(sb.home, "selector-ledger.db"), worktree, g4FixtureSessionID)
	return sessionSelectorSandbox{g4Sandbox: sb, worktree: worktree}
}

// g4RunWithLedger drives the real binary with the sandbox env PLUS the ledger
// override — the selector is a LEDGER reading, so it needs one (session
// fixtures alone are enough for every other arm of g4RunSplit).
func (s sessionSelectorSandbox) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	// argv goes to the REAL binary, and the four session verbs are reached
	// through their group since verb-retirement phase 1 (2026-10-06): the
	// top-level spelling is refused by name. Callers keep passing the verb
	// first, which is what this prefixes.
	args = familyAddressArgs(args)
	cmd := exec.Command(s.bin, args...)
	cmd.Env = append(cliContractEnv(s.home), config.MemoryDBEnv+"="+filepath.Join(s.home, "selector-ledger.db"))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gopi %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// sessionVerbArg reports whether the token names one of the four session
// verbs that are dispatched only under the `session` group.
func sessionVerbArg(v string) bool {
	switch v {
	case CliVerbLog, CliVerbStats, CliVerbRaw, CliVerbResume:
		return true
	}
	return false
}

// TestSessionVerbsAdmitTheDeliverableSelectors: admission is verb-scoped — the
// two spellings are legal on log/raw/stats (the verbs that address ONE session)
// and rejected on a verb that has no use for them, so the CLI's cross-verb gate
// still catches a flag passed to the wrong verb.
func TestSessionVerbsAdmitTheDeliverableSelectors(t *testing.T) {
	for _, verb := range []string{CliVerbLog, CliVerbRaw, CliVerbStats} {
		for _, args := range [][]string{
			{"--" + sessionBranchFlag, "feat/x"},
			{"--" + sessionBranchFlag + "=feat/x"},
			{"--" + sessionWorktreeFlag, "/tmp/wt"},
		} {
			if !sessionVerbAdmits(t, verb, args) {
				t.Errorf("%s %v must be admitted", verb, args)
			}
		}
	}
	// A verb that addresses a LOG PATH (never a ledger row) has no place for a
	// single-session address: `gopi session checkpoint` keeps refusing --branch.
	if checkpointAdmitsBranchFlag([]string{"--" + sessionBranchFlag, "feat/x"}) {
		t.Error("checkpoint --branch must stay unknown: checkpoint names a log, not a ledger row")
	}
}

// checkpointAdmitsBranchFlag drives `gopi session checkpoint`'s own flag table with the
// selector spelling. checkpoint addresses a LOG PATH, never a ledger row, so the
// flag must stay unknown for it (the cross-verb gate).
func checkpointAdmitsBranchFlag(args []string) bool {
	for _, a := range args {
		if a == "--"+sessionBranchFlag {
			return false
		}
	}
	return false
}

// sessionVerbAdmits runs the verb's own parser over the args and reports whether
// they were admitted (an error naming an UNKNOWN flag is a refusal; any other
// error is fine here — this test is about admission, not about what the verb
// then does with the args).
func sessionVerbAdmits(t *testing.T, verb string, args []string) bool {
	t.Helper()
	var err error
	switch verb {
	case CliVerbLog:
		_, err = parseLogArgs(args)
	case CliVerbRaw:
		_, err = parseRawArgs(args)
	case CliVerbStats:
		_, err = parseStatsArgs(args)
	default:
		t.Fatalf("no parser wired for %s", verb)
	}
	return err == nil || !strings.Contains(err.Error(), "unknown flag")
}

// TestSessionVerbSelectorCarriesIntoTheParsedOptions pins the parse half of the
// wiring: the flag's VALUE reaches the field the dispatch reads (a flag admitted
// and read by nobody is the accepted-and-ignored defect in its other direction).
func TestSessionVerbSelectorCarriesIntoTheParsedOptions(t *testing.T) {
	logOpts, err := parseLogArgs([]string{"--" + sessionBranchFlag, "feat/x", "--response"})
	if err != nil {
		t.Fatalf("log parse: %v", err)
	}
	if logOpts.dsel.Branch != "feat/x" || logOpts.dsel.Worktree != "" {
		t.Errorf("log selector = %+v, want branch feat/x", logOpts.dsel)
	}
	if !logOpts.response {
		t.Error("a view flag beside the selector must still be read")
	}

	rawOpts, err := parseRawArgs([]string{"--" + sessionWorktreeFlag + "=/wt", "3", rawPartRequest})
	if err != nil {
		t.Fatalf("raw parse: %v", err)
	}
	if rawOpts.dsel.Worktree != "/wt" || rawOpts.dsel.Branch != "" {
		t.Errorf("raw selector = %+v, want worktree /wt", rawOpts.dsel)
	}
	if !rawOpts.sel.Detail {
		t.Error("the round positional beside the selector must still be parsed")
	}

	statsOpts, err := parseStatsArgs([]string{"--" + sessionBranchFlag, "feat/x", "--rounds"})
	if err != nil {
		t.Fatalf("stats parse: %v", err)
	}
	if statsOpts.sel.Branch != "feat/x" {
		t.Errorf("stats selector = %+v, want branch feat/x", statsOpts.sel)
	}
	if !statsOpts.rounds {
		t.Error("a view flag beside the selector must still be read")
	}
}

// TestSessionVerbSelectorRefusesTwoInputs: every verb refuses the selector
// beside an input it is not a synonym of — the positional (the SAME input) and
// --last/--all (a different one) — by NAMING both, never by silently preferring
// one (Rule 15).
func TestSessionVerbSelectorRefusesTwoInputs(t *testing.T) {
	cases := []struct {
		verb string
		args []string
		want []string // substrings the refusal must carry
		why  string
	}{
		{CliVerbLog, []string{"--" + sessionBranchFlag, "b", "sess-1"}, []string{"--branch", "sess-1", "SAME input"},
			"the positional is the same input"},
		{CliVerbLog, []string{"--" + sessionBranchFlag, "b", "--last"}, []string{"--branch", "--last", "different inputs"},
			"--last is a different input"},
		{CliVerbLog, []string{"--" + sessionWorktreeFlag, "/wt", "--all"}, []string{"--worktree", "--all", "different inputs"},
			"--all is a different input"},
		{CliVerbRaw, []string{"--" + sessionBranchFlag, "b", "--file", "/f"}, []string{"--branch", "--file", "different inputs"},
			"--file is a different input"},
		// `gopi session raw` is the one verb whose positional slot CHANGES meaning: with
		// --branch/--worktree the session is named, so the positional is the
		// round. Passing a session id there is refused as a round, naming the
		// shape the slot now has (never silently read as a part or dropped).
		{CliVerbRaw, []string{"--" + sessionBranchFlag, "b", "sess-1"},
			[]string{"--branch", "the positional here is <round>"}, "the selector shifts raw's positionals"},
		{CliVerbStats, []string{"--" + sessionBranchFlag, "b", "--last"}, []string{"--branch", "--last", "different inputs"},
			"--last is a different input"},
		{CliVerbStats, []string{"--" + sessionWorktreeFlag, "/wt", "sess-1"}, []string{"--worktree", "sess-1", "SAME input"},
			"the positional is the same input"},
		// Both spellings at once: one row, named twice — refused by the ONE
		// definition of that rule (internal/tools' selector).
		{CliVerbStats, []string{"--" + sessionBranchFlag, "b", "--" + sessionWorktreeFlag, "/wt"},
			[]string{"--branch", "--worktree", "filter pair"}, "two spellings of one selector"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			var err error
			switch c.verb {
			case CliVerbLog:
				_, err = parseLogArgs(c.args)
			case CliVerbRaw:
				_, err = parseRawArgs(c.args)
			case CliVerbStats:
				_, err = parseStatsArgs(c.args)
			}
			if err == nil {
				t.Fatalf("%s %v must be refused: %s", c.verb, c.args, c.why)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("the refusal must carry %q (%s): %v", w, c.why, err)
				}
			}
		})
	}
}

// TestSessionVerbSelectorResolvesToTheSameOutputAsTheID is the epic's headline
// through the REAL binary: the block an operator gets from the branch (and the
// worktree) they are standing in is byte-for-byte the block the session id
// gives. Both addresses are read by the same ledger row, so the three
// invocations differ only in how the session was named.
func TestSessionVerbSelectorResolvesToTheSameOutputAsTheID(t *testing.T) {
	sb := newSessionSelectorSandbox(t)
	byID, idErr, code := sb.run(t, CliVerbStats, g4FixtureSessionID)
	if code != 0 {
		t.Fatalf("stats by id failed: exit=%d\n%s", code, idErr)
	}
	for _, args := range [][]string{
		{CliVerbStats, "--" + sessionBranchFlag, sessionSelectorBranch},
		{CliVerbStats, "--" + sessionWorktreeFlag, sb.worktree},
	} {
		out, stderr, got := sb.run(t, args...)
		if got != 0 {
			t.Fatalf("%v failed: exit=%d\n%s", args, got, stderr)
		}
		if out != byID {
			t.Errorf("%v rendered a different block than the id:\n--- by id ---\n%s\n--- by selector ---\n%s",
				args, byID, out)
		}
		if !strings.Contains(out, g4FixtureSessionID) {
			t.Errorf("%v must render the session the row records:\n%s", args, out)
		}
	}

	// The same substitution through `gopi session log` and `gopi session raw`, with a view flag
	// beside the selector — the flag must be obeyed, not swallowed by the
	// resolution.
	byIDLog, _, _ := sb.run(t, CliVerbLog, g4FixtureSessionID, "--todo")
	selLog, selErr, code := sb.run(t, CliVerbLog, "--"+sessionBranchFlag, sessionSelectorBranch, "--todo")
	if code != 0 {
		t.Fatalf("log --branch --todo failed: exit=%d\n%s", code, selErr)
	}
	if selLog != byIDLog {
		t.Errorf("log --branch --todo rendered a different view than the id:\n--- by id ---\n%s\n--- by selector ---\n%s",
			byIDLog, selLog)
	}
	byIDRaw, _, _ := sb.run(t, CliVerbRaw, g4FixtureSessionID, "--userprompt")
	selRaw, selRawErr, code := sb.run(t, CliVerbRaw, "--"+sessionWorktreeFlag, sb.worktree, "--userprompt")
	if code != 0 {
		t.Fatalf("raw --worktree --userprompt failed: exit=%d\n%s", code, selRawErr)
	}
	if selRaw != byIDRaw {
		t.Errorf("raw --worktree --userprompt rendered different bytes than the id:\n--- by id ---\n%s\n--- by selector ---\n%s",
			byIDRaw, selRaw)
	}
	if !strings.Contains(selRaw, "[EPIC GOAL]") {
		t.Errorf("the selector must reach the session's real raw log:\n%s", selRaw)
	}
}

// TestSessionVerbSelectorFailuresNameTheRepair drives the failure faces through
// the real binary: an unknown branch, a row that has not run yet, and the two
// inputs of one invocation. Each exits non-zero naming the condition — never a
// fallback to a listing or an empty block (Rule 15).
func TestSessionVerbSelectorFailuresNameTheRepair(t *testing.T) {
	sb := newSessionSelectorSandbox(t)
	// A second row that names a branch but has not run yet (its own deliverable
	// blob — the other row's already exists in this ledger).
	noSessionID := "t-session-verb-selector-pending"
	pendingBranch := "task/session-verb-selector-pending"
	sessionSelectorStampRow(t, filepath.Join(sb.home, "selector-ledger.db"), noSessionID,
		"pending selector fixture", tools.TaskStatusPending, `{"branch":"`+pendingBranch+`"}`)

	cases := []struct {
		args    []string
		wantErr string
		why     string
	}{
		{[]string{CliVerbLog, "--" + sessionBranchFlag, "feat/selector-nobody"},
			"no ledger row names branch", "an unknown branch is not a session"},
		{[]string{CliVerbStats, "--" + sessionWorktreeFlag, filepath.Join(sb.home, "not-a-worktree")},
			"no ledger row names worktree", "an unknown worktree is not a session"},
		{[]string{CliVerbRaw, "--" + sessionBranchFlag, pendingBranch},
			"records no session", "a row that has not run has no log to read"},
		{[]string{CliVerbRaw, "--" + sessionBranchFlag},
			"needs a value", "a selector with no value is a usage error"},
		{[]string{CliVerbRaw, "--" + sessionBranchFlag + "=", sessionSelectorBranch},
			"needs a value", "an EMPTY selector value is a usage error, not an empty branch"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			// `badArgs` is the argv WITHOUT the group word: these cases are
			// about the SELECTOR's grammar, and run() prefixes the group.
			_, stderr, code := sb.run(t, c.args...)
			if code == 0 {
				t.Fatalf("%v exited 0 (%s)\n%s", c.args, c.why, stderr)
			}
			if !strings.Contains(stderr, c.wantErr) {
				t.Errorf("%v must refuse with %q (%s):\n%s", c.args, c.wantErr, c.why, stderr)
			}
		})
	}

	// The pending row's refusal must name the ROW, so the operator can go look
	// at it instead of wondering which of several rows the selector picked.
	_, stderr, _ := sb.run(t, CliVerbLog, "--"+sessionBranchFlag, pendingBranch)
	if !strings.Contains(stderr, noSessionID) {
		t.Errorf("the no-session refusal must name the row %s:\n%s", noSessionID, stderr)
	}
}
