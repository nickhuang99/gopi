package main

// devworkspace_delivery_e2e_test.go — the DELIVERY LOOP on the real binary.
//
// The unit tests prove the loop's arithmetic against a scripted attempt. This
// one proves the seam: a REAL `gopi worker` run whose executor child leaves its
// work uncommitted. The deterministic attempt comes back blocked (dirty), the
// wrapper hands that blocker to a real agent round in the workspace, and after
// MaxDeliveryRounds blocked attempts it records the ledger reason and keeps the
// workspace.
//
// It is a regression gate on the OPERATOR-VISIBLE account: the three lines this
// test demands by name (the attempt counter, the blocker, the agent round) are
// the only way an operator can tell this loop ran instead of the old
// fail-at-the-first-gate pipeline.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
)

// TestCloneChannelDeliveryLoopHandsABlockerToAnAgentRound is the end-to-end
// acceptance: dirty workspace → attempt 1 blocked with git's own words → an
// agent round is started in the workspace → the loop is bounded → the row
// carries the reason and the workspace is KEPT.
func TestCloneChannelDeliveryLoopHandsABlockerToAnAgentRound(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := cloneGateRepo(t)
	root := filepath.Join(t.TempDir(), "workspace")
	const taskID = "01a150eb-0000-7000-8000-00000000e2e1"

	// THE CHILD'S ONE DEED: modify a TRACKED file without committing it. That
	// is the shape the old pipeline died on — and the shape the loop must hand
	// to an agent instead. (An UNTRACKED file would not do: the clean gate
	// tolerates those by design, so the fixture would silently pass.)
	env := cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+root+`"`,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_dirty",
			Name: "bash",
			Args: map[string]interface{}{
				"command": "printf 'left uncommitted\\n' >> a.txt",
			},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nTask complete. I left the change uncommitted, on purpose."},
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"delivery loop e2e", "--payload", "do the work", "--task-id", taskID,
		"--repo", repo}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	out, errOut, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	both := out + errOut
	t.Logf("worker --once exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)

	// THE RUN IS JUDGED A FAILURE — the deliverable never reached origin, and
	// calling that a success is the false success the channel exists to prevent.
	//
	// The ROW is where that is read, not this exit code: `worker --once` drains
	// the queue and reports on each round ("worker: run … failed (continuing)"),
	// so its own exit stays 0 — measured here, and the reason this assertion is
	// on the ledger (assertion ④ below) rather than on `code`.
	if strings.Contains(both, "completed") && !strings.Contains(both, "failed") {
		t.Fatalf("a run whose every delivery attempt was blocked must be recorded as a failure, exit=%d:\n%s", code, both)
	}
	if !strings.Contains(both, "worker: run "+taskID+" failed") {
		t.Fatalf("the worker must report the run as failed by name:\n%s", both)
	}

	// ① THE ATTEMPT COUNTER, in the operator's own words, bounded by the ONE
	//    definition of the limit. Counted over whole LINES: the same phrase also
	//    PREFIXES the "blocked (…)" line, so a substring count sees two
	//    (measured on this test's first run).
	first := "delivery attempt 1/" + strconv.Itoa(devworkspace.MaxDeliveryRounds)
	if got := linesEndingWith(both, first); got != 1 {
		t.Fatalf("the log must announce %q exactly once as a line, found %d:\n%s", first, got, both)
	}
	if !strings.Contains(both, "delivery attempt "+strconv.Itoa(devworkspace.MaxDeliveryRounds)+"/"+strconv.Itoa(devworkspace.MaxDeliveryRounds)) {
		t.Fatalf("the loop must reach its last attempt:\n%s", both)
	}

	// ② THE BLOCKER, WITH GIT'S OWN WORDS: the modified path in git's two-column
	//    spelling (a leading space means "unstaged"), not a paraphrase.
	if !strings.Contains(both, "blocked (dirty)") {
		t.Fatalf("the blocked attempt must name its kind:\n%s", both)
	}
	if !strings.Contains(both, `" M a.txt"`) {
		t.Fatalf("the blocked attempt must quote git's own listing for a.txt:\n%s", both)
	}

	// ③ THE AGENT ROUND, IN THE WORKSPACE: the loop did not just stop, it handed
	//    the facts on. The directory is read OFF THE LINE and only its LEAF is
	//    asserted (`<root>/<task-id>`): which workspace_root won is a separate
	//    question (this profile pins one in the environment, and env outranks
	//    the settings file), while "the round stood in THIS task's workspace" is
	//    the fact this test exists for.
	roundDir := lineAfter(both, "starting an agent round in ")
	if roundDir == "" {
		t.Fatalf("the loop must start an agent round in the workspace:\n%s", both)
	}
	if filepath.Base(roundDir) != taskID {
		t.Fatalf("the agent round must stand in the task's own workspace, got %q (leaf %q, want %q)",
			roundDir, filepath.Base(roundDir), taskID)
	}

	// ④ THE LEDGER CARRIES THE REASON (Rule 15): a refusal that only reached
	//    stderr is invisible to the next reader of the row.
	show, showErr, showCode := runGopiMapped(t, []string{"task", "show", taskID}, "", env, repo)
	if showCode != 0 {
		t.Fatalf("task show: code=%d stderr=%s", showCode, showErr)
	}
	for _, want := range []string{
		"failed",
		"delivery did not land in " + strconv.Itoa(devworkspace.MaxDeliveryRounds) + " attempt(s)",
		"a.txt",
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("the ledger row must carry %q:\n%s", want, show)
		}
	}

	// ⑤ THE WORKSPACE IS KEPT: the agent's work is still there for the next
	//    reader (nothing is stashed, committed or cleaned on its behalf).
	if _, statErr := os.Stat(roundDir); statErr != nil {
		t.Fatalf("the workspace %s must be KEPT after an exhausted delivery: %v", roundDir, statErr)
	}
	// AND ITS UNCOMMITTED EDIT SURVIVED: the channel never commits, stashes or
	// cleans the work it could not deliver.
	if body, readErr := os.ReadFile(filepath.Join(roundDir, "a.txt")); readErr != nil ||
		!strings.Contains(string(body), "left uncommitted") {
		t.Fatalf("the uncommitted edit must survive verbatim, got %q (%v)", body, readErr)
	}
}

// linesEndingWith counts the lines whose trimmed text ENDS with suffix — the
// whole-line measure the attempt announcement needs (the same phrase prefixes a
// second, more detailed line).
func linesEndingWith(haystack, suffix string) int {
	n := 0
	for _, line := range strings.Split(haystack, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), suffix) {
			n++
		}
	}
	return n
}

// lineAfter returns what follows marker on the FIRST line carrying it, trimmed
// — "" when no line does. The marker is searched anywhere in the line, not just
// at its start: the run's narration prefixes these lines with the row id
// ("task <id>: starting an agent round in …"), so an anchored match finds
// nothing (measured). It is how this file reads a path off the run's own
// account instead of re-deriving it — and dividing it wrong: which
// workspace_root wins is the profile's business, not this test's.
func lineAfter(haystack, marker string) string {
	for _, line := range strings.Split(haystack, "\n") {
		if _, rest, ok := strings.Cut(line, marker); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
