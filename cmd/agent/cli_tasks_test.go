package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/flaketest"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// parseTasksArgs — the pure front-end of the gopi task subcommand family.
// ---------------------------------------------------------------------------

func TestParseTasksListArgs(t *testing.T) {
	opts, err := parseTasksArgs("list", []string{"--status", "completed", "--db=/tmp/x.db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.status != "completed" {
		t.Errorf("status = %q, want completed", opts.status)
	}
	if opts.db != "/tmp/x.db" {
		t.Errorf("db = %q, want /tmp/x.db (--db=value form)", opts.db)
	}
	// Both flag forms work.
	opts, err = parseTasksArgs("list", []string{"--status", "failed"})
	if err != nil || opts.status != "failed" {
		t.Fatalf("--status v form: %+v err=%v", opts, err)
	}
}

func TestParseTasksListUnexpectedPositional(t *testing.T) {
	if _, err := parseTasksArgs("list", []string{"stray"}); err == nil || !strings.Contains(err.Error(), "positional argument") {
		t.Fatalf("want positional error, got %v", err)
	}
}

func TestParseTasksShowArgs(t *testing.T) {
	opts, err := parseTasksArgs("show", []string{"t-abc", "--db", "/tmp/y.db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.taskID != "t-abc" {
		t.Errorf("taskID = %q, want t-abc", opts.taskID)
	}
	if _, err := parseTasksArgs("show", nil); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("missing task_id must error, got %v", err)
	}
}

// TestParseTasksNewArgs: M4.5 ④ removes `task new` from the CLI — the
// parser must refuse the verb outright (removal advice, not usage dump).
func TestParseTasksNewArgs(t *testing.T) {
	if _, err := parseTasksArgs("new", []string{"fix the bug"}); err == nil ||
		!strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("task new must be removed/unknown, got err=%v", err)
	}
	// The removal advice routes to the replacement path.
	if !strings.Contains(taskRemovedAdvice("new"), "task submit") {
		t.Errorf("new removal advice must point to task submit")
	}
	if !strings.Contains(taskRemovedAdvice("next"), "gopi worker") {
		t.Errorf("next removal advice must point to gopi worker (the row runner)")
	}
}

func TestParseTasksUpdateArgs(t *testing.T) {
	opts, err := parseTasksArgs("update", []string{"t-1", "--status", "completed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.taskID != "t-1" || opts.status != "completed" {
		t.Errorf("opts = %+v", opts)
	}
	// M4.5 ⑤: --summary is GONE — status-only updates; the flag is rejected.
	if _, err := parseTasksArgs("update", []string{"t-1", "--summary", "new text"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --summary") {
		t.Fatalf("--summary must be removed/rejected, got err=%v", err)
	}
	// bugfix-task-p1: --result (the verdict evidence digest) IS accepted —
	// it rides task_events, never the summary column.
	opts2, err := parseTasksArgs("update", []string{"t-1", "--status", "verified", "--result", "evidence: go test ./... 12/12"})
	if err != nil {
		t.Fatalf("--result must be accepted: %v", err)
	}
	if opts2.result != "evidence: go test ./... 12/12" || opts2.status != "verified" {
		t.Errorf("opts = %+v", opts2)
	}
	// task-update-reason epic: --reason (the operator-visible WHY) IS
	// accepted alongside --status.
	opts3, err := parseTasksArgs("update", []string{"t-1", "--status", "failed", "--reason", "flaky integration test"})
	if err != nil {
		t.Fatalf("--reason must be accepted: %v", err)
	}
	if opts3.reason != "flaky integration test" || opts3.status != "failed" {
		t.Errorf("opts = %+v", opts3)
	}
	// --reason with both --status and --result (the verify loop's full face).
	opts4, err := parseTasksArgs("update", []string{"t-1", "--status", "verified", "--reason", "green on rerun", "--result", "evidence: 12/12"})
	if err != nil {
		t.Fatalf("--reason + --result must be accepted: %v", err)
	}
	if opts4.reason != "green on rerun" || opts4.result != "evidence: 12/12" {
		t.Errorf("opts = %+v", opts4)
	}
	// A status-less update is refused (content immutable — nothing else to set).
	if _, err := parseTasksArgs("update", []string{"t-1"}); err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("empty update must error, got %v", err)
	}
}

func TestParseTasksErrors(t *testing.T) {
	cases := []struct {
		verb string
		args []string
		want string
	}{
		{"delete", []string{"t-1"}, "unknown subcommand"}, // no delete (append-only)
		{"new", []string{"s"}, "unknown subcommand"},      // M4.5 ④: removed
		{"next", nil, "unknown subcommand"},               // M4.5 ⑥: removed
		{"list", []string{"--bogus", "x"}, "unknown flag --bogus"},
		{"list", []string{"--task-id", "t-x"}, "unknown flag --task-id"},
		{"show", []string{"t-1", "--status", "completed"}, "unknown flag --status"},
		{"update", []string{"t-1", "--task-id", "t-x"}, "unknown flag --task-id"},
		{"update", []string{"t-1", "--summary", "s"}, "unknown flag --summary"},
		{"show", []string{"t-1", "t-2"}, "positional argument"},
	}
	for _, c := range cases {
		if _, err := parseTasksArgs(c.verb, c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %v: err = %v, want containing %q", c.verb, c.args, err, c.want)
		}
	}
}

// TestTasksUsageIsAppendOnly guards nick's iron rules at the help-text
// level: the usage documents the no-delete semantics, must NOT advertise a
// delete subcommand — and since M4.5 neither `task new` nor `task next`
// exist on the CLI surface (content immutable, run next is the queue entry).
func TestTasksUsageIsAppendOnly(t *testing.T) {
	for _, want := range []string{
		"gopi task list", "gopi task show", "gopi task update",
		"NO delete", "append-only", "immutable",
	} {
		if !strings.Contains(tasksUsage, want) {
			t.Errorf("tasksUsage is missing %q", want)
		}
	}
	if strings.Contains(tasksUsage, "task delete") {
		t.Errorf("tasksUsage must not advertise a delete subcommand")
	}
	if strings.Contains(tasksUsage, "gopi task new ") || strings.Contains(tasksUsage, "gopi task next") {
		t.Errorf("tasksUsage must not advertise removed subcommands (new/next)")
	}
	// And the parser rejects the removed verbs outright.
	for _, verb := range []string{"delete", "new", "next"} {
		if _, err := parseTasksArgs(verb, []string{"t-1"}); err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
			t.Fatalf("%s verb must be rejected, got %v", verb, err)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end: the CLI dispatch calls the SAME tools.Task* functions the
// future harness would use. These tests prove the round-trip through those
// shared paths with a --db override, mirroring `gopi task new ... --db X`
// then `gopi task list --db X` (runTasksSubcommand maps --db onto
// GOPI_MEMORY_DB before calling the engine).
// ---------------------------------------------------------------------------

// TestTasksCLIUpdateReason: the task-update-reason CLI face — parse accepts
// --reason, the dispatch routes it through TaskUpdateReason, and show
// renders it. Also proves the lazy default (no --reason) stamps a meaning.
func TestTasksCLIUpdateReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := tools.TaskNew("CLI reason task", "t-cli-reason", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Explicit --reason: parse → dispatch → engine → row.
	opts, err := parseTasksArgs("update", []string{"t-cli-reason", "--status", "failed", "--reason", "probe died mid-run"})
	if err != nil {
		t.Fatalf("parseTasksArgs: %v", err)
	}
	if opts.taskID != "t-cli-reason" || opts.status != "failed" || opts.reason != "probe died mid-run" {
		t.Fatalf("opts = %+v", opts)
	}
	out, err := tools.TaskUpdateReason(opts.taskID, opts.status, opts.reason, opts.result)
	if err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}
	if !strings.Contains(out, "status=failed") || !strings.Contains(out, "reason=probe died mid-run") {
		t.Errorf("update out = %q", out)
	}
	show, err := tools.TaskShow("t-cli-reason")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         probe died mid-run") {
		t.Errorf("show missing reason:\n%s", show)
	}
}

// TestTaskCommandUsageAdvertisesReason: `gopi task -h` (tasksUsage) and the
// /task update slash help must document --reason so operators discover the
// tracking hook.
func TestTaskCommandUsageAdvertisesReason(t *testing.T) {
	if !strings.Contains(tasksUsage, "--reason WHY") {
		t.Errorf("tasksUsage must advertise gopi task update --reason WHY")
	}
	if !strings.Contains(tasksUsage, "status-specific default") {
		t.Errorf("tasksUsage must explain the lazy-caller default reason")
	}
	if !strings.Contains(taskCommandUsage, "--reason WHY") {
		t.Errorf("/task usage must advertise --reason WHY")
	}
}

func TestTasksCLIFullFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// gopi task new "..." [--task-id X] — the engine path runTasksSubcommand
	// dispatches to for the new verb.
	out, err := tools.TaskNew("CLI 全流程测试任务", "t-cli-e2e", "", "")
	if err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if !strings.Contains(out, "Task created: t-cli-e2e") {
		t.Errorf("new out = %q", out)
	}
	// gopi task list — must show the new task (default: all statuses).
	list, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "t-cli-e2e") || !strings.Contains(list, "[in_progress]") {
		t.Errorf("list out missing task:\n%s", list)
	}
	// gopi task update <id> --status completed — stamps ended_at.
	upd, err := tools.TaskUpdate("t-cli-e2e", "completed")
	if err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}
	if !strings.Contains(upd, "Task updated: t-cli-e2e") || !strings.Contains(upd, "status=completed") {
		t.Errorf("update out = %q", upd)
	}
	// gopi task show <id> — full record reflects the update.
	show, err := tools.TaskShow("t-cli-e2e")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, want := range []string{"t-cli-e2e", "status:         completed", "CLI 全流程测试任务", "ended_at:       20"} {
		if !strings.Contains(show, want) {
			t.Errorf("show missing %q:\n%s", want, show)
		}
	}
	// gopi task list --status completed — the value-signal filter finds it.
	filtered, err := tools.TaskList("completed")
	if err != nil {
		t.Fatalf("TaskList completed: %v", err)
	}
	if !strings.Contains(filtered, "t-cli-e2e") {
		t.Errorf("status filter out missing task:\n%s", filtered)
	}
}

// TestTaskRunBriefingComposesSummaryAndPayload: `task run` feeds the child
// BOTH the summary and the payload — a non-empty payload must no longer
// suppress the summary (nick, 2026-09-02: a submitter often puts the
// essential one-line intent in the summary and the detail in the payload,
// omitting the intent from the payload to avoid repetition; the old
// single-source briefing dropped the summary entirely). The composed form
// is section-marked ([Task summary] / [Briefing payload]) so the LLM does
// not confuse the two sources.
func TestTaskRunBriefingComposesSummaryAndPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// Normal submit convention: distinct summary AND payload.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "FIX: report_to propagation drops the branch",
		Payload: "root-cause the writeback repo push; verify with the E2E recipe",
		TaskID:  "t-both",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed both row: %v", err)
	}
	got, err := taskRunBriefing("t-both")
	if err != nil {
		t.Fatalf("both fields: %v", err)
	}
	for _, want := range []string{"[Task summary]", "FIX: report_to propagation drops the branch",
		"[Briefing payload]", "root-cause the writeback repo push; verify with the E2E recipe"} {
		if !strings.Contains(got, want) {
			t.Errorf("composed briefing missing %q:\n%s", want, got)
		}
	}
	// Order: summary first (intent line), then payload (detail).
	if !strings.Contains(got, "[Task summary]\nFIX: report_to") ||
		!strings.Contains(got, "[Briefing payload]\nroot-cause") {
		t.Errorf("briefing section order wrong (summary must precede payload):\n%s", got)
	}
	if strings.Index(got, "[Task summary]") > strings.Index(got, "[Briefing payload]") {
		t.Errorf("summary header must come before payload header:\n%s", got)
	}
}

// TestTaskRunBriefingFallbacks: the two single-source shapes.
//   - Payload-only (empty summary): the payload is the whole briefing —
//     unchanged from the old behavior (the DB's NOT NULL summary makes a
//     truly empty summary impossible via TaskSubmit, but the compose path
//     tolerates one: a payload-bearing row never depends on the summary).
//   - Summary-only (empty payload): the summary IS the briefing (the
//     TaskSubmit contract when --payload is absent — the composed form
//     falls back to summary only).
//   - Neither: refuse (unchanged from the e3ca35ca defense — a row with no
//     text at all must never dispatch a bare task_id as the briefing).
func TestTaskRunBriefingFallbacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// Payload-only: payload passthrough (summary "" tolerated). The DB's
	// NOT NULL summary blocks a truly-empty insert via TaskSubmit, so blank
	// the summary with raw SQL (openRawTasksDB lives in
	// cli_serve_stale_test.go, same package).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "t-payload-only",
		Payload: "real briefing text",
		TaskID:  "t-payload-only",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed payload-only row: %v", err)
	}
	db, err := openRawTasksDB(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET summary='' WHERE task_id='t-payload-only'`); err != nil {
		t.Fatalf("blank summary: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	got, err := taskRunBriefing("t-payload-only")
	if err != nil {
		t.Fatalf("payload-only: %v", err)
	}
	if strings.Contains(got, "[Task summary]") {
		t.Errorf("payload-only must not carry a summary section:\n%s", got)
	}
	if !strings.Contains(got, "[Briefing payload]\nreal briefing text") {
		t.Errorf("payload-only briefing broken: got %q", got)
	}

	// Summary-only: the summary IS the briefing (fallback preserved).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "[PA-QUEUE][P3] chat/web 内嵌 poll 循环",
		TaskID:  "pa-q-summary-only",
		Source:  "auto",
	}); err != nil {
		t.Fatalf("seed summary-only row: %v", err)
	}
	got, err = taskRunBriefing("pa-q-summary-only")
	if err != nil {
		t.Fatalf("summary-only must fall back to the summary, got err=%v", err)
	}
	if !strings.Contains(got, "[Task summary]\n[PA-QUEUE][P3] chat/web 内嵌 poll 循环") {
		t.Errorf("summary-only fallback broken: got %q", got)
	}
	if strings.Contains(got, "[Briefing payload]") {
		t.Errorf("summary-only must not carry an empty payload section:\n%s", got)
	}
}

// TestTaskRunBriefingRefusesEmpty: a row with NEITHER a summary NOR a
// payload must refuse to dispatch — the e3ca35ca defense (a bare task_id /
// codename fed as the whole briefing burned a context budget guessing what
// the task even was). Only the truly-empty row refuses; any one field
// suffices to dispatch.
func TestTaskRunBriefingRefusesEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// Summary NOT NULL blocks a truly-empty summary at insert; seed via the
	// public API then blank BOTH columns with raw SQL (the sqlite driver is
	// registered by the tools package import — openRawTasksDB lives in
	// cli_serve_stale_test.go, same package).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "placeholder to create the row",
		TaskID:  "t-empty",
		Source:  "auto",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	db, err := openRawTasksDB(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET summary='', payload='' WHERE task_id='t-empty'`); err != nil {
		t.Fatalf("blank both columns: %v", err)
	}
	if _, err := taskRunBriefing("t-empty"); err == nil ||
		!strings.Contains(err.Error(), "neither a summary nor a payload") {
		t.Fatalf("both-empty must refuse, got err=%v", err)
	}

	// Either field alone suffices — blank only the payload.
	if _, err := db.Exec(`UPDATE tasks SET summary='codename-only row', payload='' WHERE task_id='t-empty'`); err != nil {
		t.Fatalf("restore summary: %v", err)
	}
	if got, err := taskRunBriefing("t-empty"); err != nil ||
		!strings.Contains(got, "[Task summary]\ncodename-only row") {
		t.Fatalf("summary-only after blank must dispatch, got %q err=%v", got, err)
	}
}

// TestTaskRunChildReceivesSummaryAndPayloadE2E is the machine-checkable
// acceptance (Epic: task briefing = summary + payload): a task with a
// DISTINCT summary and payload is `task run` — the CHILD's session first
// user message must contain BOTH the summary text AND the payload text
// (before the fix the child received only the payload; the submitter's
// essential one-line intent in the summary was dropped from the execution
// context). The child is a real subprocess gopi against the mock LLM; its
// session JSONL under the sandboxed GOPI_HOME is the ground truth.
func TestTaskRunChildReceivesSummaryAndPayloadE2E(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	//
	// The FIXTURE-REPO variant: the child runs the shipped `gopi task submit`,
	// whose CLI face refuses a non-git cwd (see SandboxRepoContextInFixtureRepo).
	SandboxRepoContextInFixtureRepo(t)
	const (
		wantSummary = "INTENT: fix the report_to propagation (the one-line why)"
		wantPayload = "DETAIL: root-cause the writeback repo push; verify with the E2E recipe; commit a devdoc"
	)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "briefing e2e done"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, "GOPI_AUTO_TASK_REGISTER=1")

	// 1. submit a task whose summary and payload are BOTH set and distinct.
	_, stderr, code := runGopiMapped(t, []string{"task", "submit",
		wantSummary, "--payload", wantPayload, "--task-id", "01a11500-0000-7000-8000-000000000022"}, "", env, "")
	if code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	// 2. run it — the child (subprocess gopi) gets the composed briefing.
	_, stderr, code = runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000022"}, "", env, "")
	if code != 0 {
		t.Fatalf("task run: code=%d stderr=%s", code, stderr)
	}

	// 3. the child's session first user message must carry BOTH fields.
	sess := testutil.LoadSession(t, gopiHome)
	sess.AssertSingleSession(t)
	// The task-start checkpoint injection logs its own user event first
	// (621a60e, M2 enforcement), so scan ALL user messages — the stdin
	// prompt (the composed briefing) must be among them.
	//
	// ALL, not the operator half. Since row 01a188da the stdin briefing wears
	// sessionevent.HarnessBriefingOnStdinMarker, so UserContents — which exists
	// precisely to return the rows the PERSON opened — excludes it by design.
	// What this arm claims is "the child received the text", a fact about the
	// RECORD rather than about who typed it, and the classification is asserted
	// on its own two arms right below, so widening the scan cannot hide a
	// mis-stamp: a briefing filed on the wrong side fails the second arm.
	carriesBoth := func(rows []string) bool {
		for _, u := range rows {
			if strings.Contains(u, wantSummary) && strings.Contains(u, wantPayload) {
				return true
			}
		}
		return false
	}
	if all := sess.AllUserContents(); !carriesBoth(all) {
		t.Fatalf("child session never received the briefing — no user row carries BOTH summary %q and payload %q\nrows: %v",
			wantSummary, wantPayload, all)
	}
	// THE CONTRACT OF THIS ROW, asserted rather than assumed: the briefing is
	// filed as HARNESS, so `session log --turns` does not charge the framework's
	// instructions to the person and the row's title is not the framework's text.
	if harness := sess.HarnessUserContents(); !carriesBoth(harness) {
		t.Errorf("the stdin briefing is not on the harness side of the record — the framework's instructions still read as an operator turn\nharness rows: %v",
			harness)
	}
}

// TestParseTasksArgsSubmitRepoFlag (M4.8, t-1788089584): --repo is a submit
// flag; absent → the DURABLE default is resolved at parse time (the CLI's
// submitter-side semantics); --repo=X overrides it. Other verbs reject the
// flag (cross-verb whitelist holds).
//
// The absent-flag case asserts the documented intent, which is NOT the bare
// cwd: since epic 01a0d55b a submit issued from inside a run's ground (any
// LINKED WORKTREE) must name the MAIN checkout — the directory that outlives
// the run — so the parse-time default is tools.DurableRepoForSubmit(cwd), the
// ONE resolution (cli_tasks.go's --repo default; Rule 13/14). The expectation
// is therefore derived by calling that very resolver, never spelled: comparing
// against the raw cwd made this test RED IN EVERY LINKED WORKTREE on pristine
// master while it passed in the main checkout (the durable repo and the cwd
// coincide there, which is why the red was invisible to a main-checkout run).
func TestParseTasksArgsSubmitRepoFlag(t *testing.T) {
	// Explicit repo wins over cwd.
	opts, err := parseTasksArgs("submit", []string{"work summary", "--repo", "/some/repo"})
	if err != nil {
		t.Fatalf("submit --repo: %v", err)
	}
	if opts.repo != "/some/repo" {
		t.Errorf("opts.repo = %q, want /some/repo", opts.repo)
	}

	// Absent → the durable (main-checkout) default resolved at parse time,
	// the CLI face.
	cwd, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	want, err := tools.DurableRepoForSubmit(cwd)
	if err != nil {
		t.Fatalf("submit default: durable repo of cwd %q: %v", cwd, err)
	}
	opts, err = parseTasksArgs("submit", []string{"work summary"})
	if err != nil {
		t.Fatalf("submit plain: %v", err)
	}
	if opts.repo != want {
		t.Errorf("opts.repo = %q, want the durable repo %q (cwd %q)", opts.repo, want, cwd)
	}

	// The flag stays submit-only (cross-verb rejection).
	if _, err := parseTasksArgs("list", []string{"--repo", "/x"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("list --repo must be rejected, got err=%v", err)
	}

	// =-syntax works too (memoryFlagSet convention).
	opts, err = parseTasksArgs("submit", []string{"s", "--repo=/eq/repo"})
	if err != nil || opts.repo != "/eq/repo" {
		t.Errorf("submit --repo=: opts=%+v err=%v", opts, err)
	}
}

// TestParseTasksArgsSubmitAttachmentRepeatable (Phase-1 session-dir): the
// submit --attachment flag is REPEATABLE — every occurrence is collected in
// argument order (memoryFlagSet multi-value). A task submitted with two
// --attachment paths must carry both through to the runTaskSubmit options.
func TestParseTasksArgsSubmitAttachmentRepeatable(t *testing.T) {
	opts, err := parseTasksArgs("submit", []string{
		"work summary", "--attachment", "/tmp/a.log", "--attachment=/home/nick/b.log",
	})
	if err != nil {
		t.Fatalf("submit --attachment x2: %v", err)
	}
	if len(opts.attachments) != 2 {
		t.Fatalf("opts.attachments = %v, want 2 entries", opts.attachments)
	}
	if opts.attachments[0] != "/tmp/a.log" || opts.attachments[1] != "/home/nick/b.log" {
		t.Errorf("opts.attachments = %v, want [/tmp/a.log /home/nick/b.log] (arg order preserved)", opts.attachments)
	}

	// Zero occurrences → nil (no-attachment submit unchanged).
	opts, err = parseTasksArgs("submit", []string{"work summary"})
	if err != nil {
		t.Fatalf("submit plain: %v", err)
	}
	if len(opts.attachments) != 0 {
		t.Errorf("plain submit opts.attachments = %v, want none", opts.attachments)
	}

	// The flag stays submit-only (cross-verb rejection).
	if _, err := parseTasksArgs("list", []string{"--attachment", "/x"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("list --attachment must be rejected, got err=%v", err)
	}
}

// TestParseTasksArgsSubmitModelFlag (task-submit-model epic ①): --model is a
// submit flag; absent → opts.model stays "" (the deferred-value default —
// the writeback backfills the actual executor later). Other verbs reject it.
func TestParseTasksArgsSubmitModelFlag(t *testing.T) {
	opts, err := parseTasksArgs("submit", []string{"work summary", "--model", "deepseek-v4-flash"})
	if err != nil {
		t.Fatalf("submit --model: %v", err)
	}
	if opts.model != "deepseek-v4-flash" {
		t.Errorf("opts.model = %q, want deepseek-v4-flash", opts.model)
	}

	opts, err = parseTasksArgs("submit", []string{"work summary"})
	if err != nil {
		t.Fatalf("submit plain: %v", err)
	}
	if opts.model != "" {
		t.Errorf("absent --model must leave opts.model empty, got %q", opts.model)
	}

	opts, err = parseTasksArgs("submit", []string{"s", "--model=glm-5"})
	if err != nil || opts.model != "glm-5" {
		t.Errorf("submit --model=: opts=%+v err=%v", opts, err)
	}

	if _, err := parseTasksArgs("list", []string{"--model", "x"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("list --model must be rejected, got err=%v", err)
	}
}

// ---------------------------------------------------------------------------
// task-single-row-exec E2E: `task submit` + `task run` produces ONE ledger
// row for the execution — the human wrapper row. The child (task-run child)
// no longer mints an auto mirror row; all execution info (session_id stamp,
// writeback) rides the human row. This test drives the real binary against a
// mock LLM and asserts the single-row shape end to end.
// ---------------------------------------------------------------------------

// TestTaskRunSingleRowE2E: full submit → run → show round-trip in a
// sandboxed GOPI_HOME/GOPI_MEMORY_DB. The child (spawned by `task run`) runs
// against a scripted mock LLM that answers one benign text round, completes,
// and the writeback closes the single human row. Then:
//   - task list → exactly ONE in_progress/completed row for this task, NO
//     [auto] duplicate (the two-row shape is gone)
//   - human show → carries session_id + run_method + terminal status
func TestTaskRunSingleRowE2E(t *testing.T) {
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	//
	// the FIXTURE-REPO variant, not the plain pin: this test's child runs the
	// shipped `gopi task submit`, whose CLI face refuses a non-git cwd
	// ("--repo default: repo <cwd> is not a git work tree"), so a non-git
	// sandbox cwd makes the submit leg fail before the run this test is about.
	// The fixture repo keeps the hazard closed (exactly one registration, the
	// main checkout, never removable) while giving the child a real repository.
	SandboxRepoContextInFixtureRepo(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "single-row linkage done"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, "GOPI_AUTO_TASK_REGISTER=1")

	// 1. submit the human wrapper row (CLI, source=human).
	stdout, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"single-row e2e wrapper", "--payload", "do the linkage work", "--task-id", "01a11500-0000-7000-8000-000000000021"}, "", env, "")
	if code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	_ = stdout

	// 2. run it — the child completes against the mock (bounded rounds).
	stdout, stderr, code = runGopiMapped(t, []string{"worker", "01a11500-0000-7000-8000-000000000021"}, "", env, "")
	if code != 0 {
		t.Fatalf("task run: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}

	// 3. list: EXACTLY ONE row (01a11500-0000-7000-8000-000000000021), no auto duplicate, no [dup-of].
	listOut, _, _ := runGopiMapped(t, []string{"task", "list"}, "", env, "")
	if !strings.Contains(listOut, "01a11500-0000-7000-8000-000000000021") {
		t.Errorf("human row missing from list:\n%s", listOut)
	}
	if strings.Contains(listOut, "[auto]") {
		t.Errorf("task run must NOT mint an auto mirror row (single-row exec):\n%s", listOut)
	}
	if strings.Contains(listOut, "dup-of") {
		t.Errorf("list must not render [dup-of] (single-row exec):\n%s", listOut)
	}

	// 4. human show: carries session_id (bug054 stamp) + run_method + the
	//    terminal status — the single row fully describes the execution.
	showOut, _, _ := runGopiMapped(t, []string{"task", "show", "01a11500-0000-7000-8000-000000000021"}, "", env, "")
	if !strings.Contains(showOut, "session_id:") {
		t.Errorf("human row show missing session_id:\n%s", showOut)
	}
	if !strings.Contains(showOut, "run_method:") {
		t.Errorf("human row show missing run_method:\n%s", showOut)
	}
	if !strings.Contains(showOut, "status:         completed") {
		t.Errorf("human row must be completed after the run:\n%s", showOut)
	}
	if strings.Contains(showOut, "executor:") {
		t.Errorf("human row must not render executor (no mirror row):\n%s", showOut)
	}
	if strings.Contains(showOut, "linked_to:") {
		t.Errorf("human row must not render linked_to:\n%s", showOut)
	}
}

// TestTaskRunSurvivesClosedStdoutPipe is the broken-pipe regression guard
// (t-1788305178 incident, 2026-09-02): when `gopi task run` is spawned by an
// external launcher (hermes terminal tool, CI harness), the child's stdout/
// stderr ARE the wrapper's — a pipe whose read-end the launcher holds. If
// the launcher closes the pipe mid-run (turn end, SIGTERM, timeout kill),
// the child's next stderr write gets EPIPE → SIGPIPE → default kill (exit
// 141, "signal: broken pipe"), and the writeback records a spurious FAILED.
// runTaskRun ignores SIGPIPE before spawn so the closed-pipe write returns
// EPIPE (harmless) and the run + writeback survive.
//
// This test simulates the launcher vanishing: it spawns `gopi task run`
// with stdout/stderr on an os.Pipe, closes the READ end once the child is
// in_progress (a few tool rounds in), and asserts the task still reaches
// completed with exit 0 — without the fix the child dies SIGPIPE and the
// row lands failed.
func TestTaskRunSurvivesClosedStdoutPipe(t *testing.T) {
	flaketest.Declare("deterministic: the wrapper's signal.Ignore(syscall.SIGPIPE) sat BELOW TaskStart — the very call the test waits for before slamming the stdout pipe's read end — so the wrapper's next claim/briefing write to stdout raced the ignore and the default disposition killed it. Registered: testdata/flakes.tsv, owner t-1788387314.")
	// repo-context sandbox: the wrapper child must not inherit this process's cwd
	// — the live repository — or its own-worktree-release pass would enumerate
	// (and free) the developer's worktrees. See repo_context_sandbox_test.go.
	//
	// The FIXTURE-REPO variant: the child runs the shipped `gopi task submit`,
	// whose CLI face refuses a non-git cwd (see SandboxRepoContextInFixtureRepo).
	SandboxRepoContextInFixtureRepo(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "broken-pipe regression done"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, "GOPI_AUTO_TASK_REGISTER=1")
	// In-process status reads (tools.TaskShow below) must hit the SAME DB the
	// child writes — GOPI_MEMORY_DB is the single source of truth for the
	// ledger path (Rule 2: one env read, both sides honour it).
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// 1. submit the wrapper row.
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"broken-pipe e2e", "--payload", "do the pipe work", "--task-id", "01a11500-0000-7000-8000-000000000029"}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	// 2. spawn `gopi task run` with stdout/stderr on a REAL pipe (not a
	//    capture buffer) — the launcher-reader shape that breaks. We keep
	//    the read end so the child can start, then close it mid-run.
	bin := testutil.GopiBin(t)
	cmd := exec.Command(bin, "worker", "01a11500-0000-7000-8000-000000000029")
	cmd.Env = env
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	cmd.Stdin = strings.NewReader("")
	// Drain the pipe read-end into a buffer so the child never blocks on a
	// full pipe, and we can print what the wrapper wrote on failure. The
	// drain goroutine exits when pr is closed (read returns EOF/err).
	var drained strings.Builder
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		buf := make([]byte, 4096)
		for {
			n, rerr := pr.Read(buf)
			if n > 0 {
				drained.Write(buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()
	// The wrapper is a real daemon with a `gopi` child under it: the
	// pgid-scoped reaper takes the whole group down on every exit path.
	mp := testutil.StartMockProcessCmd(t, cmd)

	// 3. wait for the child to be in_progress (its session registered), then
	//    slam the read end shut — the launcher-vanished moment.
	deadline := time.Now().Add(60 * time.Second)
	status := ""
	for time.Now().Before(deadline) {
		show, _ := tools.TaskShow("01a11500-0000-7000-8000-000000000029") //stylegate:ignore poll iteration: an unreadable row simply fails the containment assertion
		for _, ln := range strings.Split(show, "\n") {
			// THE STATUS WORD, not the rendered field: `task show` chains the
			// MEASURED holder facts after the word (` + holder dead (pid N
			// gone)`, internal/tools/taskholder_gone.go), so an equality on the
			// raw field would report "never reached in_progress" for a row that
			// reached it and then lost its holder. The split lives once, in
			// statusWord (cli_tasks_statusword_split_test.go).
			if strings.HasPrefix(ln, "status:") {
				status = statusWord(strings.TrimSpace(strings.TrimPrefix(ln, "status:")))
			}
		}
		if status == "in_progress" {
			break
		}
		time.Sleep(100 * time.Millisecond) //stylegate:allow-sleep poll interval while the child reaches in_progress
	}
	if status != "in_progress" {
		t.Fatalf("task never reached in_progress (status=%q) — child may have died before start", status)
	}
	if err := pr.Close(); err != nil {
		t.Fatalf("closing pipe read end: %v", err)
	}
	_ = pw.Close() //stylegate:ignore the parent's own write end; the child holds an independent copy, and its write is the assertion

	// 4. the run must COMPLETE despite the vanished reader (exit 0, status
	//    completed). Without the fix the child dies SIGPIPE → failed. The
	//    wait is the handle's shared one — a second cmd.Wait on the same
	//    *exec.Cmd would be an error and a race with the reaper.
	err = mp.Wait()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("task run wait: %v", err)
		}
	}
	if exit != 0 {
		t.Fatalf("task run exited %d (want 0 — SIGPIPE death leaves 141); err=%v\n--- wrapper output ---\n%s", exit, err, drained.String())
	}
	show, _ := tools.TaskShow("01a11500-0000-7000-8000-000000000029") //stylegate:ignore the row was already asserted above; here only the rendered status line is read
	// status line is column-aligned ("status:         completed"); match the
	// value loosely rather than the exact padding.
	if !regexp.MustCompile(`status:\s+completed`).MatchString(show) {
		t.Fatalf("task did not complete after pipe close:\n%s", show)
	}
}

// TestRetiredSweepZombiesVerbRefusesEveryShape is the post-removal guard of the
// zombie-sweeper retirement (devdoc/zombie-sweep-removed.md, 2026-10-26).
//
// WHAT REPLACED TestParseTasksSweepZombiesArgs, and WHY. That test pinned the
// flag PARSER of a verb that no longer exists — every assertion in it was about
// `--dry-run` / `--kill` / `--stall-sec` / `--max-run` landing in fields the
// mechanism owned. Keeping it would mean keeping those fields, and keeping the
// fields is how a deleted capability comes back: the parser is the half that
// makes a verb look alive. So the fields went with the mechanism, and what a
// deployment on disk still needs is pinned HERE instead.
//
// THE CONTRACT, and it is deliberately NOT "reject the old flags". The
// `gopi task run` retirement settled this shape (cli_tasks.go "case
// taskverbs.Run:" — "must render as a REMOVAL, never as an unknown-flag
// error"): `gopi task sweep-zombies --dry-run` is a command recorded in old task
// rows, shell history and scripts, so parsing must still ADMIT it and let it
// reach the dispatcher, which exits 1 with the pointed removal message. A parse
// failure would answer "unknown flag --dry-run" and send its reader hunting a
// typo that does not exist. An accepted-and-ignored flag is not a hazard here
// because the verb cannot run at all — the dispatcher refuses before any flag
// could be read.
func TestRetiredSweepZombiesVerbRefusesEveryShape(t *testing.T) {
	for _, argv := range [][]string{
		{},
		{"--dry-run"},
		{"--kill"},
		{"--stall-sec", "900"},
		{"--max-run", "5400"},
		{"--db", "/tmp/whatever.db"},
	} {
		// PARSE must succeed: the shape is admitted so the removal sentence is
		// reachable (a recorded command lands on an explanation, not a typo
		// hunt). If this ever fails, the flag left taskFlagMember and the verb
		// still exists — the half-removed state where an old script gets
		// "unknown flag" from a verb that is otherwise gone.
		if _, err := parseTasksArgs(taskverbs.SweepZombies, argv); err != nil {
			t.Errorf("parseTasksArgs(%q, %v) = %v, want success: a RETIRED verb must parse its historical "+
				"flags so the removal message is reachable (the `run` precedent)", taskverbs.SweepZombies, argv, err)
		}
	}
	// A POSITIONAL is still refused — the verb never took one, and accepting a
	// stray token would silently swallow an argument a script meant for
	// something else.
	if _, err := parseTasksArgs(taskverbs.SweepZombies, []string{"some-id"}); err == nil {
		t.Errorf("parseTasksArgs(%q, [\"some-id\"]) succeeded — the retired verb never took a positional",
			taskverbs.SweepZombies)
	}
	// The removal sentence a caller actually sees must be non-empty: an empty
	// advice would leave the dispatcher printing a dangling "removed — ".
	if strings.TrimSpace(taskRemovedAdvice(taskverbs.SweepZombies)) == "" {
		t.Fatalf("taskRemovedAdvice(%q) is empty — a retired verb must carry a replacement sentence", taskverbs.SweepZombies)
	}
}

// TestRunMethodFor: the run-provenance method/cmdline resolver
// (devdoc/run-method-provenance.md). Worker latch → worker method;
// otherwise manual with the target + the caller's --report-to/--repo
// (a dispatched remote's own node stamp carries its return path).
func TestRunMethodFor(t *testing.T) {
	workerWriteback = "" // clean latch
	defer func() { workerWriteback = "" }()

	// Manual, bare target — the DEFAULT settings context. It is named
	// explicitly because the suite pins GOPI_SETTINGS at a sandbox path
	// (internal/testutil.PinSuiteNotifyEnv — without the pin every lifecycle
	// test delivers into the operator's live notify sink), and
	// LaunchSettingsArg correctly echoes any NON-default path onto the
	// run_cmd (see TestRunMethodFor_SettingsContext below). The bare form is
	// exactly the default-context case, so the context must be set to the
	// default here rather than left to the ambient suite value.
	t.Setenv("GOPI_SETTINGS", config.DefaultSettingsPath())
	// The launcher word is pinned to the production binary name: the renderer
	// reports the argv0 BASENAME, and this test runs as the package's own
	// test binary (runcmd.EnvBinary is the launcher-identity seam).
	pinLauncherName(t)
	// The recorded recipe must RE-RUN: since the rework a named row is run by
	// `gopi worker <task-id>`, so that is the command a crashed row stores.
	m, c := runMethodFor("t-abc", &tasksOptions{taskID: "t-abc"})
	if m != runcmd.MethodManual || c != "gopi worker t-abc" {
		t.Fatalf("single-row = %q / %q, want manual / gopi worker t-abc", m, c)
	}
	// Manual with the remote return path (the dispatched node's own row).
	m, c = runMethodFor("t-abc", &tasksOptions{taskID: "t-abc", reportTo: "zbook", repo: "zbook:/home/nick/work/GoPi"})
	if m != runcmd.MethodManual || c != "gopi worker t-abc --report-to zbook --repo zbook:/home/nick/work/GoPi" {
		t.Fatalf("single-row+return = %q / %q, want the full re-runnable command", m, c)
	}
	// Worker round: the latch holds the target. The recorded command is the
	// loop that re-runs this work — `gopi worker` — NOT a hand-written
	// description of the round (the defect this epic removes: a parenthesised
	// pseudo-command could not be pasted into a shell).
	workerWriteback = "t-abc"
	m, c = runMethodFor("t-abc", &tasksOptions{})
	if m != runcmd.MethodWorker || c != runcmd.Command(os.Args) {
		t.Fatalf("worker = %q / %q, want worker / the round's own argv %q", m, c, runcmd.Command(os.Args))
	}
	// The pseudo-command this epic removed: a parenthesis is not a command.
	if strings.ContainsAny(c, "()") {
		t.Fatalf("worker run_cmd must be pasteable, got %q", c)
	}
	workerWriteback = "t-other"
	if m, _ := runMethodFor("t-abc", &tasksOptions{taskID: "t-abc"}); m != runcmd.MethodManual {
		t.Fatalf("latch mismatch must fall back to manual (historical row), got %q", m)
	}
}

// pinLauncherName pins the launcher identity the run_cmd renderer reads, so a
// test asserting a literal command does not depend on the test binary's name
// (runcmd.Launcher falls back to this process's executable).
func pinLauncherName(t *testing.T) {
	t.Helper()
	t.Setenv(runcmd.EnvBinary, "gopi")
	t.Setenv(runcmd.EnvSelf, "")
}

// TestRunMethodFor_SettingsContext: a manual run under a non-default settings
// context must record run_cmd as `gopi --settings <PATH> worker T` (the
// settings token-pair slots after the binary, before the subcommand) so the
// recorded command re-runs under the SAME provider/config the execution
// actually used (run-cmd-settings epic). The reported bug: `gopi --settings
// ~/.gopi/.do_settings.json worker T` was stamped bare `gopi worker T`,
// hiding the do-ai-run provider / 0731 model.
func TestRunMethodFor_SettingsContext(t *testing.T) {
	workerWriteback = "" // clean latch
	defer func() { workerWriteback = "" }()

	// The env path of LaunchSettingsArg: GOPI_SETTINGS = a NON-default file.
	nonDefault := filepath.Join(t.TempDir(), "do_settings.json")
	t.Setenv("GOPI_SETTINGS", nonDefault)
	pinLauncherName(t)

	m, c := runMethodFor("t-settings", &tasksOptions{taskID: "t-settings"})
	if m != runcmd.MethodManual {
		t.Fatalf("method = %q, want manual", m)
	}
	want := "gopi --settings " + nonDefault + " worker t-settings"
	if c != want {
		t.Fatalf("run_cmd = %q, want %q", c, want)
	}

	// The return-path options still ride behind the settings token-pair.
	m, c = runMethodFor("t-settings", &tasksOptions{
		taskID: "t-settings", reportTo: "node1", repo: "node1:/repos/gopi",
	})
	want = "gopi --settings " + nonDefault + " worker t-settings --report-to node1 --repo node1:/repos/gopi"
	if c != want {
		t.Fatalf("run_cmd+return = %q, want %q", c, want)
	}
}

// TestTaskRunBriefingInjectsAttachmentIndex — acceptance (Phase-1, epic-task-
// reframe): a task submitted WITH --attachment files gets an [Attachments]
// EPIC block in its run briefing — one [attachmentN: <abs replica path>] per
// manifest entry, the replica path pointing under the sandboxed GOPI_HOME
// session dir. The child reads those paths with its existing file-read tool,
// so the composed briefing must carry resolvable absolute paths, and the
// replica files must exist there with the original content.
func TestTaskRunBriefingInjectsAttachmentIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	// Two same-name files in different dirs — the collision case (both must
	// survive under distinct replica subdirs).
	dirA := filepath.Join(home, "srcA")
	dirB := filepath.Join(home, "srcB")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fileA := filepath.Join(dirA, "data.txt")
	fileB := filepath.Join(dirB, "data.txt")
	if err := os.WriteFile(fileA, []byte("content-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("content-B"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:     "attach-index task",
		Payload:     "read both replicas",
		Attachments: []string{fileA, fileB},
		TaskID:      "t-attach-brief",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	got, err := taskRunBriefing("t-attach-brief")
	if err != nil {
		t.Fatalf("taskRunBriefing: %v", err)
	}
	if !strings.Contains(got, "[Attachments]") {
		t.Fatalf("briefing lacks [Attachments] header:\n%s", got)
	}
	// The header must come AFTER summary+payload (index injected last).
	if strings.Index(got, "[Attachments]") < strings.Index(got, "[Briefing payload]") {
		t.Errorf("[Attachments] must follow [Briefing payload]:\n%s", got)
	}

	// The replica abs paths derive from the manifest: attachments/<abs
	// path minus leading /> joined onto <home>/.gopi/sessions/<id>.
	sessRoot := filepath.Join(home, ".gopi", "sessions", "t-attach-brief")
	for i, src := range []string{fileA, fileB} {
		n := i + 1
		rel := filepath.Join("attachments", strings.TrimPrefix(filepath.ToSlash(src), "/"))
		rep := filepath.Join(sessRoot, rel)
		wantLine := "[attachment" + itoa(n) + ": " + rep + "]"
		if !strings.Contains(got, wantLine) {
			t.Errorf("briefing missing %q:\n%s", wantLine, got)
		}
		// The replica must be a real, readable file with the original
		// content — the child's file-read tool hits this path.
		data, err := os.ReadFile(rep)
		if err != nil {
			t.Errorf("replica %s unreadable: %v", rep, err)
			continue
		}
		wantContent := "content-A"
		if src == fileB {
			wantContent = "content-B"
		}
		if string(data) != wantContent {
			t.Errorf("replica %s content = %q, want %q", rep, data, wantContent)
		}
	}
}

// itoa is a minimal int→string for the test (strconv is imported by other
// files in this package; keep the dependency local to avoid import churn).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// gopi task list — header count is POST-archive-filter (real binary).
// ---------------------------------------------------------------------------

// TestTasksCLIListHeaderCountMatchesRows is the end-to-end pin for the
// 68-vs-7 defect: `gopi task list --status failed` printed the SELECT's row
// count in its header while the rendered body had the archived rows filtered
// out. The probe seeds the ledger through the SAME engine functions the CLI
// uses (against one --db file), then runs the REAL binary and asserts the
// header integer equals the number of "#"-prefixed rows rendered, and that
// --all keeps counting the unfiltered set.
func TestTasksCLIListHeaderCountMatchesRows(t *testing.T) {
	bin := gopiTestBinPath(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// One unresolved failure + three archived ones.
	if _, err := tools.TaskNew("unresolved failure", "t-hdr-live", "", ""); err != nil {
		t.Fatalf("TaskNew live: %v", err)
	}
	if _, err := tools.TaskUpdate("t-hdr-live", tools.TaskStatusFailed); err != nil {
		t.Fatalf("fail live: %v", err)
	}
	for _, id := range []string{"t-hdr-a", "t-hdr-b", "t-hdr-c"} {
		if _, err := tools.TaskNew("handled failure", id, "", ""); err != nil {
			t.Fatalf("TaskNew %s: %v", id, err)
		}
		if _, err := tools.TaskUpdate(id, tools.TaskStatusFailed); err != nil {
			t.Fatalf("fail %s: %v", id, err)
		}
		if _, err := tools.TaskArchive(id); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}

	// `gopi task list --status failed` — the default (filtered) view.
	out, err := runCLIContractProbeEnv(t, bin, t.TempDir(),
		CliVerbTask, "list", "--status", tools.TaskStatusFailed, "--db", dbPath)
	if err != nil {
		t.Fatalf("task list: %v\n%s", err, out)
	}
	if got, rows := cliLedgerHeaderCount(t, out), cliLedgerRowCount(out); got != rows || rows != 1 {
		t.Errorf("default view header=%d rows=%d, want 1/1 (archived rows must leave the body AND the count):\n%s", got, rows, out)
	}
	if strings.Contains(out, taskResolvedMarkerProbe) {
		t.Errorf("default view must not render archived rows:\n%s", out)
	}

	// `--all` — the unfiltered view keeps all four (no regression).
	all, err := runCLIContractProbeEnv(t, bin, t.TempDir(),
		CliVerbTask, "list", "--status", tools.TaskStatusFailed, "--all", "--db", dbPath)
	if err != nil {
		t.Fatalf("task list --all: %v\n%s", err, all)
	}
	if got, rows := cliLedgerHeaderCount(t, all), cliLedgerRowCount(all); got != rows || rows != 4 {
		t.Errorf("--all header=%d rows=%d, want 4/4 (unfiltered, accurate):\n%s", got, rows, all)
	}
}

// taskResolvedMarkerProbe is the rendered archived-row marker the default
// view must not show (mirrors internal/tools' taskResolvedRowMarker).
const taskResolvedMarkerProbe = " [resolved]"

// cliLedgerHeaderCount parses the leading integer of a rendered ledger
// header — the count the CLI claims.
func cliLedgerHeaderCount(t *testing.T, rendered string) int {
	t.Helper()
	header, _, _ := strings.Cut(rendered, "\n")
	i := 0
	for i < len(header) && header[i] >= '0' && header[i] <= '9' {
		i++
	}
	if i == 0 {
		t.Fatalf("output does not open with a count header:\n%s", rendered)
	}
	n, err := strconv.Atoi(header[:i])
	if err != nil {
		t.Fatalf("header count %q: %v", header[:i], err)
	}
	return n
}

// cliLedgerRowCount counts "#"-prefixed row lines in a rendered ledger.
func cliLedgerRowCount(rendered string) int {
	n := 0
	for _, ln := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(ln, "#") {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The archived-row disclosure, END TO END on the REAL binary (2026-09-29).
// ---------------------------------------------------------------------------

// TestTasksCLIListDisclosesHiddenArchivedRows is the acceptance run for the
// second half of the archive-visibility defect: `task list --status completed`
// over an archived-completed ledger used to print exactly "No tasks found." —
// the same bytes an empty ledger prints (docs/archive/demo/materials/01-list/
// 05-finding.md F1.2). The fix is a disclosure, so the assertion is that the
// HIDDEN COUNT reaches the terminal, with the flag that undoes it.
//
// It is also the doc↔behaviour pin on the CLI side: the two spellings the
// render uses (the "[hidden]" marker and the revealing flag) are read from
// internal/tools' exported constants, and the help text is required to name
// the same marker — a help that describes the disclosure while the render
// has none (or a render whose line the help never mentions) fails here.
func TestTasksCLIListDisclosesHiddenArchivedRows(t *testing.T) {
	bin := gopiTestBinPath(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	archived := []string{"t-disc-a", "t-disc-b"}
	for _, id := range archived {
		if _, err := tools.TaskNew("delivered row", id, "", ""); err != nil {
			t.Fatalf("TaskNew %s: %v", id, err)
		}
		if _, err := tools.TaskUpdate(id, tools.TaskStatusInProgress); err != nil {
			t.Fatalf("in_progress %s: %v", id, err)
		}
		if _, err := tools.TaskUpdate(id, tools.TaskStatusCompleted); err != nil {
			t.Fatalf("completed %s: %v", id, err)
		}
		if _, err := tools.TaskArchive(id); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}

	// The exact invocation the operator report ran, on the exact shape it ran
	// it against: a completed view whose completed rows are all archived.
	out, err := runCLIContractProbeEnv(t, bin, t.TempDir(),
		CliVerbTask, taskverbs.List, "--status", tools.TaskStatusCompleted, "--db", dbPath)
	if err != nil {
		t.Fatalf("task list --status completed: %v\n%s", err, out)
	}
	want := tools.TaskListArchiveHiddenNote(len(archived))
	if !strings.Contains(out, want) {
		t.Errorf("the view must disclose the rows it hid — want %q in:\n%s", want, out)
	}
	if !strings.Contains(out, tools.TaskListAllFlag) {
		t.Errorf("the disclosure must name the flag that reveals them (%q):\n%s", tools.TaskListAllFlag, out)
	}

	// THE DEFECT'S OWN BYTES: this output must NOT be what a ledger that never
	// held a completed row prints. The control is measured on the SAME binary
	// against a fresh (empty) ledger, so no ledger text is mirrored into this
	// test — the comparison is between two real runs.
	emptyDB := filepath.Join(t.TempDir(), "empty.db")
	emptyOut, err := runCLIContractProbeEnv(t, bin, t.TempDir(),
		CliVerbTask, taskverbs.List, "--status", tools.TaskStatusCompleted, "--db", emptyDB)
	if err != nil {
		t.Fatalf("task list --status completed (empty ledger): %v\n%s", err, emptyOut)
	}
	if emptyOut == out {
		t.Errorf("an all-archived view is byte-identical to an empty ledger — the disclosure did not reach the terminal:\n%s", out)
	}
	if strings.Contains(emptyOut, tools.TaskListArchiveHiddenPrefix) {
		t.Errorf("an EMPTY ledger hid nothing — its view must carry no disclosure line:\n%s", emptyOut)
	}

	// The revealing invocation really reveals them: the flag the disclosure
	// names is the flag that works (a disclosure naming a dead end would be
	// worse than none).
	all, err := runCLIContractProbeEnv(t, bin, t.TempDir(),
		CliVerbTask, taskverbs.List, "--status", tools.TaskStatusCompleted, tools.TaskListAllFlag, "--db", dbPath)
	if err != nil {
		t.Fatalf("task list --status completed %s: %v\n%s", tools.TaskListAllFlag, err, all)
	}
	for _, id := range archived {
		if !strings.Contains(all, id) {
			t.Errorf("%s must show the archived row %s:\n%s", tools.TaskListAllFlag, id, all)
		}
	}
	if strings.Contains(all, tools.TaskListArchiveHiddenPrefix) {
		t.Errorf("a view that hid nothing must not carry a disclosure line:\n%s", all)
	}

	// The help is the PROMISE; these tests are the render. The family help
	// must name the marker its render actually prints.
	if !strings.Contains(tasksUsage, tools.TaskListArchiveHiddenPrefix) {
		t.Errorf("tasksUsage must name the disclosure marker %q its own render prints", tools.TaskListArchiveHiddenPrefix)
	}
}
