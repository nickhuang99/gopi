package tools

// The reclaim refusal convergence contract — the /tmp/other-repo incident as a
// regression guard (devdoc/reclaim-fossil-20260915.md).
//
// THE DEFECT: the sweep's doc promised "a row whose ground is unreadable is
// reported ONCE, not as a fresh failure burst every pass", but only the
// EMPTY-reason case honoured it. The one refusal that can NEVER converge — a
// row whose REPO DIRECTORY IS GONE, where the ledger is append-only and no verb
// deletes the row — was re-printed by every pass. Measured live 2026-09-15: a
// single leaked fixture row made the merger's 5-minute loop emit
//
//	[task-notify] dead-holder reclaim  (t-stale-sibling) skipped: repo
//	directory /tmp/other-repo is gone — this row cannot be reconciled here
//
// forever, so an operator reading the log could not tell a permanent fossil
// from a real, actionable refusal.
//
// THE CONTRACT these tests pin (three halves, all needed):
//
//	① the FIRST occurrence is REPORTED (Rule 15 — the refusal is disclosed),
//	② the SECOND pass is SILENT (the convergence the doc promises),
//	③ a row whose REASON CHANGES is reported AGAIN (the memo is keyed by
//	  row+reason, so a fresh diagnosis is never hidden behind the verdict it
//	  replaced).

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureStderrDuring runs fn with os.Stderr redirected into a pipe and
// returns what was written. taskNotifyLog writes there unconditionally, so
// this is the seam the refusal report is measured through.
func captureStderrDuring(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	os.Stderr = prev
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}
	return string(out)
}

// freshReclaimRefusalMemo empties the process-wide refusal memo so a test
// measures ITS OWN first occurrence rather than another test's earlier one.
// The memo is a plain var (not a const) for exactly this reason.
func freshReclaimRefusalMemo(t *testing.T) {
	t.Helper()
	prev := reclaimRefusalSeen
	reclaimRefusalSeen = &sync.Map{}
	t.Cleanup(func() { reclaimRefusalSeen = prev })
}

// fossilLedgerEnv points the process at ONE throwaway ledger for the whole
// test and returns a root for the never-created decoy repos. It is separated
// from the row seeding so a test can seed SEVERAL fossil rows into the SAME
// ledger — t.Setenv must not be re-called per row, since a second call with a
// fresh temp path would put the rows in different databases and the test would
// measure the fixture instead of the memo.
func fossilLedgerEnv(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc is unavailable — the dead-holder shape cannot be modelled")
	}
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	return t.TempDir()
}

// seedFossilRow seeds the incident's shape at the SMALLEST sample: one terminal
// row whose recorded REPO does not exist on this host (the decoy /tmp/other-repo
// of the real row). Its holder pid is provably dead, so the row is a genuine
// candidate — the repo probe is what refuses it. Returns the decoy path, which
// is deliberately NEVER created.
func seedFossilRow(t *testing.T, root, taskID string) (repo, wt string) {
	t.Helper()
	repo = filepath.Join(root, "other-repo")
	wt = filepath.Join(root, "wt")
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "fossil", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// THE ROW MUST BE LIVE. The memo/marker machinery below is about DISCLOSING
	// an anomaly, and a terminal row whose ground is gone is not one: it leaves
	// the report set before any wording is chosen (reclaimTerminalRowUnreachable
	// — its own tests are task_reclaim_candidate_set_test.go). A LIVE-status row
	// whose ground vanished IS the anomaly that must be named, so it is the
	// shape these tests drive.
	seedDeadHolderRow(t, taskID, taskStatusInProgress, repo, "feat/fossil", wt, hostPid(t, deadPID(t)))
	return repo, wt
}

// TestDeadHolderReclaimReportsAnIrreconcilableRowOnce is the incident's own
// regression: the first pass names the refusal, the second says nothing.
//
// The row is LIVE-status on purpose: a terminal row is out of the report set
// entirely (reclaimTerminalRowUnreachable), so using one here would measure the
// wrong contract — silence from the FIRST pass rather than the convergence of a
// disclosure a human must read.
func TestDeadHolderReclaimReportsAnIrreconcilableRowOnce(t *testing.T) {
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)
	repo, _ := seedFossilRow(t, root, "t-fossil-once")
	if _, err := os.Stat(repo); !os.IsNotExist(err) {
		t.Fatalf("precondition: the decoy repo %s must not exist (err=%v)", repo, err)
	}

	first := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if !strings.Contains(first, "t-fossil-once") {
		t.Fatalf("the first pass must REPORT the irreconcilable row (Rule 15 — a refusal is disclosed, "+
			"never swallowed):\n%s", first)
	}
	if !strings.Contains(first, "cannot be reconciled") || !strings.Contains(first, repo) {
		t.Fatalf("the report must name the reason AND the repo that is gone:\n%s", first)
	}

	second := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if strings.Contains(second, "t-fossil-once") {
		t.Fatalf("a refusal that CANNOT converge (the ledger is append-only — no verb deletes the row) must "+
			"not be re-printed every pass; the merger's 5-minute loop made the operator's log a standing "+
			"puzzle this way:\nsecond pass stderr:\n%s", second)
	}

	// A THIRD pass is silent too — and the sweep's returned repair list stays
	// honest (a refusal is not a repair).
	third := captureStderrDuring(t, func() {
		if got := ReclaimDeadHolderWorktrees(true); len(got) != 0 {
			t.Errorf("a refused row is not a repair — the returned list must stay empty, got %v", got)
		}
	})
	if strings.Contains(third, "t-fossil-once") {
		t.Fatalf("third pass must stay silent:\n%s", third)
	}
}

// TestDeadHolderReclaimReportsEachRowSeparately is the memo's keying half: the
// memo is keyed by ROW, so a second fossil row is NOT swallowed by the first
// one's earlier report (an over-broad "we already warned" gate would hide every
// later victim).
func TestDeadHolderReclaimReportsEachRowSeparately(t *testing.T) {
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)
	seedFossilRow(t, root, "t-fossil-a")
	seedFossilRow(t, root, "t-fossil-b")

	out := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if !strings.Contains(out, "t-fossil-a") || !strings.Contains(out, "t-fossil-b") {
		t.Fatalf("each irreconcilable row must be named in its own right (the memo is keyed by row+reason, "+
			"never a single global 'warned' flag):\n%s", out)
	}
}

// TestReclaimRefusalReportedAgainWhenTheReasonChanges is the reason-keying
// half: the SAME row with a DIFFERENT refusal reason is a fresh diagnosis and
// must be reported, not hidden behind the verdict it replaced.
//
// THE ROW ID IS MINTED PER EXECUTION (measured 2026-10-30, `-count=3`, exit 1 on
// iterations 2 and 3 — task_reclaim_converge_test.go:177 "the first refusal of a
// row must be reported"). The suspect in the report that raised this was the
// process-wide memo not being reset, and that is NOT what was measured: the memo
// is freshly installed by freshReclaimRefusalMemo below AND the report is
// suppressed BEFORE it in reclaimRefuse, by the DURABLE half of the same gate —
//
//	if permanent && reclaimUnreachableRecorded(r.TaskID) { … return false }
//
// which reads `reclaim-unreachable` from the append-only task_events table. The
// hard-coded id `t-reason` therefore carried iteration 1's marker into iterations
// 2 and 3: a process-wide reset cannot clear a fact that lives in the LEDGER,
// which is exactly why the re-entrancy fix is a per-execution id.
func TestReclaimRefusalReportedAgainWhenTheReasonChanges(t *testing.T) {
	freshReclaimRefusalMemo(t)
	row := reclaimDeadHolderRow{TaskID: uniqueFixtureTaskID(t, "reason"), Repo: "/gone", Branch: "feat/x", Worktree: "/gone/wt"}

	if !reclaimRefuse(row, reclaimUnreachableRepoGoneReason("/gone"), nil) {
		t.Fatalf("the first refusal of a row must be reported")
	}
	if reclaimRefuse(row, reclaimUnreachableRepoGoneReason("/gone"), nil) {
		t.Fatalf("the same row with the SAME reason must not be reported twice (the convergence contract)")
	}
	if !reclaimRefuse(row, "worktree status unreadable: chdir /gone/wt: no such file or directory", nil) {
		t.Fatalf("the same row with a NEW reason is a fresh diagnosis — it must be reported (otherwise a " +
			"changed failure hides behind the verdict it replaced)")
	}
}

// TestReclaimRefusalSilentForTheNothingToDoCase is the negative control the old
// code already honoured, kept so a future rewrite of the memo does not start
// emitting a line for the "registration already clean" case — which has nothing
// to do and nothing to say.
func TestReclaimRefusalSilentForTheNothingToDoCase(t *testing.T) {
	freshReclaimRefusalMemo(t)
	row := reclaimDeadHolderRow{TaskID: "t-quiet", Repo: "/repo", Branch: "feat/q", Worktree: "/repo/wt"}
	if reclaimRefuse(row, "", nil) {
		t.Fatalf("an EMPTY reason means 'registration already clean' — there is nothing to do and nothing to say")
	}
}
