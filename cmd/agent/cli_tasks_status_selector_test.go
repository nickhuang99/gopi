// The `gopi task status` DELIVERABLE SELECTORS at the CLI entry: `--branch B` /
// `--worktree PATH` address the row an operator standing in a task worktree
// actually holds (`git branch --show-current` or `pwd`), instead of the row id
// that workflow never carries — the SAME two addresses `gopi task show` gained,
// applied to the live-process verdict.
//
// WHY THE ZOMBIE VERB NEEDS THEM, and not just as symmetry: the question this
// verb answers ("is that row's runner really alive?") is asked FROM a worktree,
// where the operator holds the branch or the directory and NOT the row id. The
// id is discovered by leaving the worktree — which is exactly when nobody is
// asking any more.
//
// What this file pins, at the three points the operator actually touches:
//
//  1. ADMISSION — both flags are accepted on `status` (and stay accepted on
//     `show`, whose admission they share) and are refused on a verb that has no
//     use for them, so the cross-verb gate is intact;
//  2. THE THREE ADDRESSES — the positional task_id, --branch and --worktree
//     resolve to the SAME row, proved in process through the ONE seam both
//     verbs call (resolveDeliverableSelector) and END TO END through the real
//     binary: `task status --branch B --json` prints the byte-identical verdict
//     `task status <row-id> --json` prints;
//  3. THE REFUSALS — no address at all is a named usage error, two selectors at
//     once are refused by the resolver, and a selector NO row records is
//     refused BY NAME with the family's "no owner" exit code (never exit 1,
//     which on THIS verb means "running — do NOT resume").

package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/flaketest"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// statusSelectorNoOwner is a branch shape NO row records — the selector the
// refusal probes ask about.
const statusSelectorNoOwner = "feat/status-nobody-owns-this"

// statusSelectorBranch is the branch the seeded row's deliverable records.
const statusSelectorBranch = "feat/status-selector-address"

// statusBranchFlagArg / statusWorktreeFlagArg / worktreeProbe are the argv
// spellings these probes type. They ARE the constants production uses
// (taskBranchFlagName / taskWorktreeFlagName, with the `--` the shell spelling
// needs): a test that typed its own literal could keep passing while the
// production spelling changed under it.
const (
	statusBranchFlagArg   = "--" + taskBranchFlagName
	statusWorktreeFlagArg = "--" + taskWorktreeFlagName
	worktreeProbe         = "/wt/status-selector"
)

// TestTaskStatusAcceptsBranchAndWorktree: admission is verb-scoped — the
// two flags are legal on `status` and `show`, and rejected elsewhere.
func TestTaskStatusAcceptsBranchAndWorktree(t *testing.T) {
	for _, args := range [][]string{
		{statusBranchFlagArg, statusSelectorBranch},
		{statusWorktreeFlagArg, worktreeProbe},
		{g1FixtureID, statusBranchFlagArg, statusSelectorBranch},
	} {
		if _, err := parseTasksArgs(taskverbs.Status, args); err != nil {
			t.Errorf("%s %v must be admitted: %v", taskverbs.Status, args, err)
		}
	}
	// The same flags on a verb that cannot use them stay rejected (the
	// cross-verb gate is what keeps a typo'd command from reading as a silent
	// no-op).
	// `archive` stands in for "a verb that cannot use these flags". It used to
	// be `for-branch`, which is a RETIRED spelling now (task 01a18799) whose
	// parse arm admits the historical flag set so a recorded command reads a
	// removal sentence rather than "unknown flag" — so it can no longer serve as
	// the rejection example here.
	for _, verb := range []string{taskverbs.List, taskverbs.Archive} {
		if _, err := parseTasksArgs(verb, []string{statusBranchFlagArg, statusSelectorBranch}); err == nil {
			t.Errorf("%s --branch must be rejected as an unknown flag for that verb", verb)
		}
		if _, err := parseTasksArgs(verb, []string{statusWorktreeFlagArg, worktreeProbe}); err == nil {
			t.Errorf("%s --worktree must be rejected as an unknown flag for that verb", verb)
		}
	}
	// The parse carries the selectors into the fields the seam reads.
	opts, err := parseTasksArgs(taskverbs.Status, []string{statusBranchFlagArg, statusSelectorBranch})
	if err != nil {
		t.Fatalf("parse status --branch: %v", err)
	}
	if opts.selBranch != statusSelectorBranch || opts.selWorktree != "" || opts.taskID != "" {
		t.Errorf("parsed selectors = (branch %q, worktree %q, taskID %q), want (%q, \"\", \"\")",
			opts.selBranch, opts.selWorktree, opts.taskID, statusSelectorBranch)
	}
}

// TestTaskStatusRefusesNoAddressAndNamesTheAlternatives: the shape an operator
// types before reading the description — `gopi task status` with nothing —
// names BOTH deliverable selectors, so the synopsis is discoverable from the
// refusal itself.
func TestTaskStatusRefusesNoAddressAndNamesTheAlternatives(t *testing.T) {
	if _, err := parseTasksArgs(taskverbs.Status, nil); err == nil {
		t.Fatal("status with no address must be refused")
	} else if !strings.Contains(err.Error(), statusBranchFlagArg) || !strings.Contains(err.Error(), statusWorktreeFlagArg) {
		t.Errorf("the empty-address error must name both alternate addresses: %v", err)
	}
	// The refusal is SHARED with show (one sentence, two verbs — Rule 14): the
	// two must not drift into two different instructions for the same mistake.
	_, showErr := parseTasksArgs(taskverbs.Show, nil)
	_, statusErr := parseTasksArgs(taskverbs.Status, nil)
	if showErr == nil || statusErr == nil || showErr.Error() != statusErr.Error() {
		t.Errorf("show and status must give the SAME refusal for a missing address:\n show: %v\n status: %v", showErr, statusErr)
	}
}

// TestTaskStatusRefusesTwoSelectors: `--branch` and `--worktree` are two ways to
// name ONE row, not a filter pair. Parse admits both (it refuses only shapes
// that cannot name a row); the RESOLVER refuses the pair, and that refusal is
// one definition — asserted here through the same seam the verb calls.
func TestTaskStatusRefusesTwoSelectors(t *testing.T) {
	if _, _, err := resolveDeliverableSelector(context.Background(), &tasksOptions{
		selBranch: "feat/x", selWorktree: worktreeProbe,
	}); err == nil {
		t.Fatal("--branch together with --worktree must be refused by the resolver")
	} else if !strings.Contains(err.Error(), statusBranchFlagArg) || !strings.Contains(err.Error(), statusWorktreeFlagArg) {
		t.Errorf("the pair refusal must name both flags: %v", err)
	}
}

// TestTaskStatusResolvesTheSelectorsToTheRowID: the row is addressed three
// ways and all three land on ONE task id — resolved through the shared seam, so
// the two verbs cannot disagree about which row a selector names.
//
// The canonical address pays nothing for the machinery: a bare task_id comes
// back unchanged, with no ledger read to fail and no git command to run.
func TestTaskStatusResolvesTheSelectorsToTheRowID(t *testing.T) {
	ctx := context.Background()
	if got, prov, err := resolveDeliverableSelector(ctx, &tasksOptions{taskID: "t-canonical"}); err != nil ||
		got != "t-canonical" || prov != "" {
		t.Fatalf("a bare task_id must come back unchanged (got %q, prov %q, err %v)", got, prov, err)
	}

	dbPath := testutil.NewTestDBPath(t)
	repo := t.TempDir()
	worktree := t.TempDir()
	showSelectorSeed(t, dbPath, g1FixtureID, tools.TaskStatusInProgress, repo, statusSelectorBranch, worktree)

	for name, opts := range map[string]*tasksOptions{
		"task_id":             {taskID: g1FixtureID, db: dbPath},
		statusBranchFlagArg:   {selBranch: statusSelectorBranch, db: dbPath},
		statusWorktreeFlagArg: {selWorktree: worktree, db: dbPath},
	} {
		got, _, err := resolveDeliverableSelector(ctx, opts)
		if err != nil {
			t.Errorf("%s: resolve: %v", name, err)
			continue
		}
		if got != g1FixtureID {
			t.Errorf("%s resolved to %q, want the seeded row %q", name, got, g1FixtureID)
		}
	}
}

// --- the clock-derived fields of a verdict ------------------------------------
//
// WHAT THIS TEST COMPARES, AND THE ONE THING IT MAY NOT COMPARE
// ------------------------------------------------------------
// The assertion below is about the VERDICT: two ADDRESSES for one row must make
// the verb say the same thing about it. It is deliberately a BYTE comparison —
// "the same verdict" is not "a verdict with the same fields", and a looser
// comparison would let the two address paths drift into two different renderings
// without anyone noticing.
//
// It is NOT a comparison of the CLOCK, and the render carries exactly one class
// of field that is a render of `now` rather than a statement about the row: the
// recorded-runner START INSTANT. `gopi task status` prints it twice, from the
// same tick count, through `procStartTimeDisplay` (internal/tools/taskproc.go):
//
//	recorded:    pid=… start=2026-10-09T08:10:57+08:00 cmd="…"   ← RFC3339, JSON too
//	started_at:  2026-10-09 08:11:00                             ← the human column
//
// THE MECHANISM, as MEASURED (the briefing's own premise — "two calls that cross
// a second boundary" — does not reproduce it; see devdoc/status-verdict-clock-flake.md):
// both fields are computed at RENDER time as
//
//	boot = now - /proc/uptime ;  start = boot + ticks/HZ
//
// and /proc/uptime is read with CENTISECOND precision, so the computed `boot`
// carries a sawtooth of up to ~10 ms decided only by WHEN the read lands. A start
// instant lying inside that sawtooth of a wall-clock second boundary therefore
// renders :NN for one invocation and :NN+1 for the next, SECOND-granular, from
// the same row. Measured on this host by driving the real renderer over the tick
// phase: at the risky phase ONE row rendered both 08:19:27 and 08:19:28 across 24
// renders; 400 real CLI invocations of one seeded row inside one process (200
// pairs, no sleep) ALL read a single value. The exposure is therefore that ~10 ms
// window per invocation, not the ~1 s the two sequential calls take.
//
// That is why the two clock-derived shapes are normalised away BEFORE the bytes
// are compared, and why the rest of the text — the `verdict:` word, the row
// address, the evidence chain, the resumable flag — is compared RAW. Normalising
// the clock is not a weakening of the assertion: it removes the one field that
// does NOT describe the row, and a verdict that actually differed would still be
// caught byte-for-byte (the red proof in
// TestTaskStatusVerdictComparisonCatchesAGenuinelyDifferentVerdict drives that,
// both on synthetic pairs and end to end between two seeded ledgers).
//
// HONEST BOUNDARY of the normalisation: it is TEXT-shaped, not field-aware — a
// clock-shaped string printed from ROW DATA (a task summary or a recorded
// cmdline carrying an RFC3339 stamp) would be normalised too. The fixture row
// carries neither, so the comparison as asserted is exact for this fixture; a
// future fixture with a timestamp in its summary would have to compare the
// structured `--json` fields instead of the rendered text.
const (
	// statusClockStampPlaceholder replaces every clock-derived stamp, so a
	// difference between two renders is reported as a difference (never as an
	// empty string that could compare equal to another empty string).
	statusClockStampPlaceholder = "<CLOCK-DERIVED>"

	// statusVerdictDiffMaxLines bounds a failure message: the handful of lines
	// that differ is the diagnosis; the whole report is already adjacent.
	statusVerdictDiffMaxLines = 8
)

// statusClockStampRe matches the RFC3339 instant the render carries (the JSON
// `recorded_start_at` / `live_start_at`, and the `start=` field of the human
// `recorded:` line) — the wall-clock form, never the raw tick count, which IS a
// fact about the row and stays compared.
var statusClockStampRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})`)

// statusClockColumnRe matches the human render's `started_at:` column
// (`2026-10-09 08:11:00`), the second-granular shape the same instant takes
// when formatTaskTime renders it.
var statusClockColumnRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

// statusVerdictSansClock returns a rendered verdict with every clock-derived
// field replaced by statusClockStampPlaceholder. Everything else is untouched:
// the caller compares these two strings byte-for-byte.
func statusVerdictSansClock(verdict string) string {
	verdict = statusClockStampRe.ReplaceAllString(verdict, statusClockStampPlaceholder)
	return statusClockColumnRe.ReplaceAllString(verdict, statusClockStampPlaceholder)
}

// statusVerdictDiff reports how two rendered verdicts differ once the
// clock-derived fields are excluded: nil means "the same verdict", and
// otherwise it names the lines that disagree. It is the ONE comparison — the
// headline test requires it empty, and the red proof requires it non-empty for
// a verdict that genuinely differs — so a comparison that reported "equal"
// unconditionally could not pass both.
func statusVerdictDiff(byID, byBranch string) []string {
	normID, normBranch := statusVerdictSansClock(byID), statusVerdictSansClock(byBranch)
	if normID == normBranch {
		return nil
	}
	idLines, branchLines := strings.Split(normID, "\n"), strings.Split(normBranch, "\n")
	var diffs []string
	for i := 0; i < len(idLines) || i < len(branchLines); i++ {
		var a, b string
		if i < len(idLines) {
			a = idLines[i]
		}
		if i < len(branchLines) {
			b = branchLines[i]
		}
		if a != b {
			diffs = append(diffs, fmt.Sprintf("line %d: by id %q ≠ by branch %q", i+1, a, b))
			if len(diffs) == statusVerdictDiffMaxLines {
				break
			}
		}
	}
	return diffs
}

// TestTaskStatusSelectorAndRowIDPrintTheSameVerdict is the epic's headline at
// the REAL surface (Rule 31): the verdict an operator gets from the branch he
// is standing in is the verdict the row id gives — same bytes, same exit code —
// driven through the built binary, not through a seam this test chose.
//
// The comparison is over the VERDICT, not over the clock (see the block above
// the helpers): the second-granular start instant is normalised away on both
// sides, and everything else is compared raw.
func TestTaskStatusSelectorAndRowIDPrintTheSameVerdict(t *testing.T) {
	// Registered in testdata/flakes.tsv: this test's fixture renders a
	// clock-derived start instant, and the two addresses are two separate
	// processes, each computing that instant at RENDER time.
	flaketest.Declare("deterministic: the fixture's row records a runner identity, so both renders carry a clock-derived start instant computed at render time as boot = now - /proc/uptime (procStartTimeDisplay, internal/tools/taskproc.go:900); /proc/uptime is read with CENTISECOND precision, so that boot carries a sawtooth of up to 10 ms decided by WHEN the read lands, and an instant inside it renders :NN for one invocation and :NN+1 for the next. Registered: testdata/flakes.tsv, owner 01a11df4-153d-787d-aa19-cfb97d256e1f.")

	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusInProgress, statusSelectorBranch)

	byID := sb.g1RunOK(t, "task", taskverbs.Status, g1FixtureID, "--json", "--db", sb.db)
	byBranch := sb.g1RunOK(t, "task", taskverbs.Status, statusBranchFlagArg, statusSelectorBranch, "--json", "--db", sb.db)

	// Non-vacuity of the normalisation itself: the two renders must BOTH carry a
	// clock-derived field (otherwise the fixture stopped exercising the one field
	// this comparison excludes, and the exclusion would be untested). The
	// comparison is kept honest from the other side too — the assertion below
	// still requires the rendered verdict to NAME the resolved row, so a
	// comparison that had scrubbed everything could not pass here either.
	for name, got := range map[string]string{"by id": byID, "by branch": byBranch} {
		if !strings.Contains(statusVerdictSansClock(got), statusClockStampPlaceholder) {
			t.Errorf("the %s render carries NO clock-derived field — the fixture no longer records a runner identity, "+
				"so this test's normalisation is exercising nothing:\n%s", name, got)
		}
	}

	if diffs := statusVerdictDiff(byID, byBranch); len(diffs) != 0 {
		t.Errorf("the verdict reached by --branch differs from the one reached by task_id OUTSIDE the clock-derived fields:\n%s\n--- by id ---\n%s\n--- by branch ---\n%s",
			strings.Join(diffs, "\n"), byID, byBranch)
	}
	if !strings.Contains(byBranch, g1FixtureID) {
		t.Errorf("the verdict reached by --branch must name the resolved row %s:\n%s", g1FixtureID, byBranch)
	}

	// The human render is the same too — the selector changes WHICH row is
	// asked about, never what the verb says about it.
	humanID := sb.g1RunOK(t, "task", taskverbs.Status, g1FixtureID, "--db", sb.db)
	humanBranch := sb.g1RunOK(t, "task", taskverbs.Status, statusBranchFlagArg, statusSelectorBranch, "--db", sb.db)
	if diffs := statusVerdictDiff(humanID, humanBranch); len(diffs) != 0 {
		t.Errorf("the rendered report differs between the two addresses OUTSIDE the clock-derived fields:\n%s\n--- by id ---\n%s\n--- by branch ---\n%s",
			strings.Join(diffs, "\n"), humanID, humanBranch)
	}
}

// TestTaskStatusVerdictComparisonCatchesAGenuinelyDifferentVerdict is the red
// proof standing beside the headline assertion: the comparison above tolerates
// the clock and NOTHING ELSE. It drives the SAME statusVerdictDiff the headline
// test requires to be empty, in three directions:
//
//  1. a clock-only difference — the flake's own shape — is NOT reported (the
//     normalisation works, and the test is not simply asserting "any two
//     strings are equal");
//  2. a difference in the verdict word, the row address or the resumable flag
//     IS reported, line by line (the assertion still measures the verb);
//  3. end to end, two SEEDED LEDGERS whose rows really do answer differently
//     (in_progress vs pending) produce renders the SAME comparison reports as
//     different — so the proof is not confined to synthetic strings.
func TestTaskStatusVerdictComparisonCatchesAGenuinelyDifferentVerdict(t *testing.T) {
	// The flake's own shape: two renders of ONE row that differ ONLY in the
	// second-granular instants. Nothing else may be reported.
	clockOnly := []struct{ id, branch string }{
		{
			"status:      in_progress\nverdict:     running\nstarted_at:  2026-10-09 08:11:00\n" +
				"recorded:    pid=42 start=2026-10-09T08:10:57+08:00 cmd=\"gopi\"\n",
			"status:      in_progress\nverdict:     running\nstarted_at:  2026-10-09 08:11:02\n" +
				"recorded:    pid=42 start=2026-10-09T08:10:59+08:00 cmd=\"gopi\"\n",
		},
		{
			`{"verdict":"running","recorded_start_at":"2026-10-09T08:10:57+08:00","live_start_at":"2026-10-09T08:10:58+08:00"}`,
			`{"verdict":"running","recorded_start_at":"2026-10-09T08:10:59+08:00","live_start_at":"2026-10-09T08:11:00+08:00"}`,
		},
	}
	for i, pair := range clockOnly {
		if diffs := statusVerdictDiff(pair.id, pair.branch); len(diffs) != 0 {
			t.Errorf("case %d: two renders differing ONLY in their clock-derived fields were reported as a verdict "+
				"difference — that is the false failure this comparison exists to remove:\n%s",
				i+1, strings.Join(diffs, "\n"))
		}
	}

	// The mutation half: each of these renders is identical to its partner except
	// for ONE fact about the row, and each must be reported.
	base := "task:        t-1\nstatus:      in_progress\nverdict:     running\nresumable:   no\n"
	for _, c := range []struct{ name, mutated string }{
		{"the verdict word", "task:        t-1\nstatus:      in_progress\nverdict:     dead\nresumable:   no\n"},
		{"the row address", "task:        t-2\nstatus:      in_progress\nverdict:     running\nresumable:   no\n"},
		{"the resumable flag", "task:        t-1\nstatus:      in_progress\nverdict:     running\nresumable:   yes\n"},
	} {
		if diffs := statusVerdictDiff(base, c.mutated); len(diffs) == 0 {
			t.Errorf("%s changed and the comparison reported NO difference — the headline assertion would pass "+
				"for a verb whose two addresses disagree:\n%s", c.name, c.mutated)
		}
	}

	// End to end, on the REAL surface: two ledgers, the same row id and the same
	// branch, but the rows are in different states, so the verb's answer about
	// them genuinely differs. The comparison must report it.
	sbRunning := g1NewSandbox(t)
	g1Seed(t, sbRunning, tools.TaskStatusInProgress, statusSelectorBranch)
	sbPending := g1NewSandbox(t)
	g1Seed(t, sbPending, tools.TaskStatusPending, statusSelectorBranch)

	runningByID := sbRunning.g1RunOK(t, "task", taskverbs.Status, g1FixtureID, "--db", sbRunning.db)
	pendingByBranch := sbPending.g1RunOK(t, "task", taskverbs.Status, statusBranchFlagArg, statusSelectorBranch, "--db", sbPending.db)
	if diffs := statusVerdictDiff(runningByID, pendingByBranch); len(diffs) == 0 {
		t.Errorf("two rows in different states (in_progress vs pending) produced renders this comparison calls "+
			"IDENTICAL — it is comparing nothing:\n--- in_progress by id ---\n%s\n--- pending by branch ---\n%s",
			runningByID, pendingByBranch)
	}
}

// TestTaskStatusSelectorWithNoOwnerIsRefusedWithItsOwnCode: a selector no row
// records is an ANSWER about the ledger, and it must NOT report a verdict.
//
// The exit code is the contract: 4 is the family's "the ledger WAS read and no
// row names it" code (`gopi task show --branch` answers the same way). Exit 1 is
// FORBIDDEN here — on this verb 1 means "running; do NOT resume", so a selector
// naming nothing would tell every script the row is alive. That silent
// inversion is precisely the defect a definite-answer verb exists to remove.
func TestTaskStatusSelectorWithNoOwnerIsRefusedWithItsOwnCode(t *testing.T) {
	sb := g1NewSandbox(t)
	g1Seed(t, sb, tools.TaskStatusPending, "")

	out, code := sb.g1Run(t, "task", taskverbs.Status, statusBranchFlagArg, statusSelectorNoOwner, "--db", sb.db)
	if code != tools.TaskForBranchExitNoOwner {
		t.Errorf("a selector no row records: exit = %d, want %d (the ledger was read; nothing names it)\n%s",
			code, tools.TaskForBranchExitNoOwner, out)
	}
	if code == taskStatusExitRunning {
		t.Fatalf("the refusal exited %d — the code that means RUNNING: a script would read 'alive' for a row that was never found", code)
	}
	if !strings.Contains(out, "no ledger row names") {
		t.Errorf("the refusal must say the ledger was read and no row names the selector:\n%s", out)
	}
	if !strings.Contains(out, statusSelectorNoOwner) {
		t.Errorf("the refusal must name the selector:\n%s", out)
	}
	// The git half rides the SAME stdout channel and is labelled as read from
	// git — the two readings must never be mistakable for one another (the
	// operator asked about a ground git may well hold).
	if !strings.Contains(out, tools.TaskGitProvenanceLabel) {
		t.Errorf("the refusal must carry the labelled git provenance block:\n%s", out)
	}
}

// TestTaskStatusNoRowExitMapping: the mapping itself, for the outcomes a live
// probe cannot be made to produce on demand (an unreadable ledger).
func TestTaskStatusNoRowExitMapping(t *testing.T) {
	noOwner := fmt.Errorf("wrapped: %w", tools.ErrDeliverableNoOwner)
	if got := taskStatusExitNoRow(noOwner); got != tools.TaskForBranchExitNoOwner {
		t.Errorf("a no-owner refusal mapped to %d, want %d", got, tools.TaskForBranchExitNoOwner)
	}
	if taskStatusExitNoRow(noOwner) == taskStatusExitRunning {
		t.Fatal("a no-owner refusal must never map to the RUNNING code")
	}
	if got := taskStatusExitNoRow(fmt.Errorf("the ledger could not be read")); got != 1 {
		t.Errorf("an unreadable-ledger failure mapped to %d, want the generic 1", got)
	}
}
