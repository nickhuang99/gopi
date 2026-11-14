package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestCLIPositionalSubmitIsDispatchable is the regression pin for the
// 2026-09-10 positional-submit defect (peer1 memory.db rows pay=0):
// `gopi task submit "<text>"` — the obvious first command anyone types, and
// the form --help advertises with --payload optional — wrote the text into
// the summary column and left payload EMPTY. TaskNextFiltered deliberately
// skips payload-less rows (bug028: those are ledger/registration entries, and
// serving one killed the daemon), so the row sat in the queue VISIBLE to
// `task list --status pending` but un-claimable: `worker --once` printed
// "queue drained — exiting". Only an explicit `task run <id>` (which bypasses
// the claim selection) could ever run it, which is why the defect stayed
// hidden behind manual PA rescues.
//
// The pin asserts BOTH ends of the contract — the CLI row carries a payload,
// and the dispatcher's OWN selection path (tools.TaskNextFiltered, the same
// call worker/serve/dispatch use) hands the row back. Reverting the cli-side
// mirror makes the claim assertion fail (verified by mutation, see the
// committed devdoc).
func TestCLIPositionalSubmitIsDispatchable(t *testing.T) {
	const (
		briefing = "positional submit must stay claimable by the worker"
		taskID   = "b75f19d4-f20e-58bb-96b3-b712acd36b75"
	)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	env := testutil.MockEnv(t, t.TempDir(), "http://127.0.0.1:1") // no LLM traffic
	env = append(env, "GOPI_MEMORY_DB="+dbPath)
	// The claim half runs in-process: the engine reads GOPI_MEMORY_DB.
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// 1. the natural form: positional text, NO --payload.
	stdout, stderr, code := runGopiMapped(t, []string{
		"task", "submit", briefing, "--task-id", taskID,
	}, "", env, "")
	if code != 0 {
		t.Fatalf("positional submit: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, taskID) {
		t.Errorf("submit confirmation must name the task id, got %q", stdout)
	}
	// The mirroring is announced on stderr — never a silent semantic change.
	if !strings.Contains(stderr, taskSubmitPositionalPayloadNote) {
		t.Errorf("positional submit must announce the payload mirror on stderr\nstderr: %q", stderr)
	}

	// 2. the row really carries the briefing in the payload column.
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var summary, payload string
	if err := db.QueryRow(`SELECT summary, payload FROM tasks WHERE task_id = ?`, taskID).
		Scan(&summary, &payload); err != nil {
		t.Fatalf("read submitted row: %v", err)
	}
	if strings.TrimSpace(payload) != briefing {
		t.Errorf("payload = %q, want the positional briefing %q (an empty payload is invisible to every dispatcher)", payload, briefing)
	}
	if strings.TrimSpace(summary) != briefing {
		t.Errorf("summary = %q, want %q (the ledger headline stays the positional text)", summary, briefing)
	}

	// 3. the dispatcher's own selection path claims it (the red line: an
	//    empty payload made TaskNextFiltered skip the row forever).
	res, err := tools.TaskNextFiltered("regression-pin", "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res.Empty || res.Task == nil {
		t.Fatalf("queue empty — the positionally submitted row is un-claimable (payload filter skipped it)")
	}
	if res.Task.TaskID != taskID {
		t.Errorf("claimed %s, want %s", res.Task.TaskID, taskID)
	}
	if strings.TrimSpace(res.Task.Payload) != briefing {
		t.Errorf("claimed row payload = %q, want %q", res.Task.Payload, briefing)
	}
}

// TestCLIPositionalSubmitRunsThroughDispatcherE2E closes the loop reported in
// the peer1 incident: a positionally submitted row must be picked up and RUN
// by the dispatcher's own claim path. Before the fix `gopi task run next`
// (and `worker`, which claims through workerRound→TaskNextFiltered) reported
// an empty queue while `task list --status pending` showed the row — the work
// intent was queued but unreachable. Here the real binary submits
// positionally, then `task run next` must CLAIM it (not report "no pending
// task") and the child session must receive the briefing text.
func TestCLIPositionalSubmitRunsThroughDispatcherE2E(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	//
	// The FIXTURE-REPO variant: the positional submit in step 1 runs the shipped
	// `gopi task submit`, whose CLI face refuses a non-git cwd — the very
	// "is not a git work tree" refusal this test would then report as a red.
	SandboxRepoContextInFixtureRepo(t)
	const briefing = "BRIEFING-MARKER: run the dispatcher pickup of a positional submit"
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "positional pickup done"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath)

	// 1. positional submit (no --payload) — the natural form.
	if _, stderr, code := runGopiMapped(t, []string{
		"task", "submit", briefing, "--task-id", "01a11500-0000-7000-8000-000000000025",
	}, "", env, ""); code != 0 {
		t.Fatalf("positional submit: code=%d stderr=%s", code, stderr)
	}

	// 2. `worker --once` — the drain path, i.e. the dispatcher's claim.
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, "")
	if code != 0 {
		t.Fatalf("worker --once: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "no pending task") {
		t.Fatalf("dispatcher saw an EMPTY queue while a pending row existed — the positional row is un-claimable:\nstdout=%s", stdout)
	}
	if !strings.Contains(stdout, "claimed: 01a11500-0000-7000-8000-000000000025") {
		t.Errorf("dispatcher did not claim the positional row:\nstdout=%s", stdout)
	}

	// 3. the row reaches a terminal state and the child really got the text.
	showOut, _, _ := runGopiMapped(t, []string{"task", "show", "01a11500-0000-7000-8000-000000000025"}, "", env, "")
	if !strings.Contains(showOut, "status:         completed") {
		t.Errorf("positional row must be completed after the dispatched run:\n%s", showOut)
	}
	sess := testutil.LoadSession(t, gopiHome)
	sess.AssertSingleSession(t)
	// The briefing wears the harness marker since row 01a188da, so it lives on
	// the HARNESS side of the record (UserContents returns only the rows the
	// person opened — that split is exactly what the marker buys). The claim
	// here is "the dispatched child really received the positional text".
	found := false
	for _, u := range sess.AllUserContents() {
		if strings.Contains(u, briefing) {
			found = true
		}
	}
	if !found {
		t.Fatalf("child session never received the positional briefing %q — rows=%v", briefing, sess.AllUserContents())
	}
	// ... and it is filed as harness, not as something the person typed.
	onHarnessSide := false
	for _, u := range sess.HarnessUserContents() {
		if strings.Contains(u, briefing) {
			onHarnessSide = true
		}
	}
	if !onHarnessSide {
		t.Errorf("the dispatched child's briefing is NOT on the harness side of the record — the framework's instructions will be counted as the operator's opening turn\nharness rows: %v",
			sess.HarnessUserContents())
	}
}

// TestTaskRunBriefingDedupesMirroredPayload: a row whose payload is the
// positional-submit MIRROR (payload == summary) must produce a single-section
// briefing — the child should not read the same one-line text twice under two
// headers. A genuinely distinct payload must still get its own section (the
// dedupe must not over-collapse).
func TestTaskRunBriefingDedupesMirroredPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	const mirrored = "one-line intent mirrored by positional submit"
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: mirrored, Payload: mirrored, TaskID: "t-mirrored", Source: "human",
	}); err != nil {
		t.Fatalf("seed mirrored row: %v", err)
	}
	got, err := taskRunBriefing("t-mirrored")
	if err != nil {
		t.Fatalf("mirrored briefing: %v", err)
	}
	if !strings.Contains(got, taskBriefingHeaderSummary+"\n"+mirrored) {
		t.Errorf("mirrored briefing lost the summary section: %q", got)
	}
	if strings.Contains(got, taskBriefingHeaderPayload) {
		t.Errorf("mirrored payload must not repeat the summary under a second header: %q", got)
	}

	// A distinct payload keeps its own section.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "distinct headline", Payload: "distinct briefing body",
		TaskID: "t-distinct", Source: "human",
	}); err != nil {
		t.Fatalf("seed distinct row: %v", err)
	}
	got, err = taskRunBriefing("t-distinct")
	if err != nil {
		t.Fatalf("distinct briefing: %v", err)
	}
	if !strings.Contains(got, taskBriefingHeaderPayload+"\ndistinct briefing body") {
		t.Errorf("distinct payload section lost: %q", got)
	}
}

// TestCLISubmitExplicitPayloadWins: --payload keeps its meaning — the
// positional summary stays the ONE-LINE headline and the payload the full
// briefing (no mirroring, no note). Guards the fix from collapsing the
// distinct-fields model into "summary always overwrites payload".
func TestCLISubmitExplicitPayloadWins(t *testing.T) {
	const (
		summary     = "one-line intent"
		wantPayload = "the full self-contained briefing"
		taskID      = "8ea8de69-0ac0-554e-b5f1-8050a5500841"
	)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	env := testutil.MockEnv(t, t.TempDir(), "http://127.0.0.1:1")
	env = append(env, "GOPI_MEMORY_DB="+dbPath)

	_, stderr, code := runGopiMapped(t, []string{
		"task", "submit", summary, "--payload", wantPayload, "--task-id", taskID,
	}, "", env, "")
	if code != 0 {
		t.Fatalf("submit with --payload: code=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, taskSubmitPositionalPayloadNote) {
		t.Errorf("an explicit --payload must not announce a mirror\nstderr: %q", stderr)
	}

	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var payload string
	if err := db.QueryRow(`SELECT payload FROM tasks WHERE task_id = ?`, taskID).Scan(&payload); err != nil {
		t.Fatalf("read submitted row: %v", err)
	}
	if payload != wantPayload {
		t.Errorf("payload = %q, want %q", payload, wantPayload)
	}
}
