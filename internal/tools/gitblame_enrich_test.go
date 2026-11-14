package tools

// gitblame_enrich_test.go — pins the ledger half of git_blame (epic
// gitblame-task-enrichment). Every arm builds a REAL temp git repository and a
// REAL (fresh, temp) ledger: the join is between two stores, so a test that
// mocked either side would prove nothing about the three fallback layers that
// are the whole contract.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// gitBlameEnrichRepo is a temp repository plus a commit helper, so each arm
// states its commits in one line instead of five.
type gitBlameEnrichRepo struct {
	t    *testing.T
	root string
}

func newGitBlameEnrichRepo(t *testing.T) *gitBlameEnrichRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — optional tool")
	}
	r := &gitBlameEnrichRepo{t: t, root: t.TempDir()}
	r.git("init", "-q")
	return r
}

func (r *gitBlameEnrichRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// write overwrites a repo file with content.
func (r *gitBlameEnrichRepo) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.root, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// commit stages everything and commits with an optional `task:` trailer —
// `--trailer` is how the bash tool's own commit seam attaches the join key
// (internal/tools/git_commit_task_trailer.go), so the fixture writes it exactly
// the way production does.
func (r *gitBlameEnrichRepo) commit(msg string, taskIDs ...string) string {
	r.t.Helper()
	args := []string{"add", "-A"}
	r.git(args...)
	cargs := []string{"commit", "-q", "-m", msg}
	for _, id := range taskIDs {
		cargs = append(cargs, "--trailer", gitBlameTaskTrailerKey+": "+id)
	}
	r.git(cargs...)
	return strings.TrimSpace(r.git("rev-parse", "HEAD"))
}

// blame runs the real tool core and decodes the JSON both arms need.
func (r *gitBlameEnrichRepo) blame(file string, start, end int) (GitBlameResult, string) {
	r.t.Helper()
	raw := gitBlameAt(r.root, file, start, end)
	if strings.Contains(raw, `"error"`) {
		r.t.Fatalf("git_blame returned an error: %s", raw)
	}
	var res GitBlameResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		r.t.Fatalf("decode blame result: %v\n%s", err, raw)
	}
	return res, raw
}

// newGitBlameEnrichLedger points the process at a FRESH temp ledger and returns
// its path plus an open handle for seeding (Rule 32: the scaffolder owns the
// path, never a hand-rolled one).
func newGitBlameEnrichLedger(t *testing.T) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	t.Setenv(memoryDBEnv, path)
	return path
}

// seedGitBlameTask inserts one row and stamps its status, returning nothing —
// the assertions read the tool's output, not the ledger.
func seedGitBlameTask(t *testing.T, taskID, summary, status string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: summary, Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close() //stylegate:ignore test handle; a close error cannot change the assertions
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET status = ? WHERE task_id = ?`, status, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// dropGitBlameTaskSummary replaces a row's summary with an arbitrary string —
// the ONLY way to build the legacy/dirty-row fixture, because the ledger's own
// WRITE path sanitizes (that is exactly why the render path needs its own
// guard, and why the tool must not trust the column).
func dropGitBlameTaskSummary(t *testing.T, taskID, summary string) {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close() //stylegate:ignore test handle; a close error cannot change the assertions
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET summary = ? WHERE task_id = ?`, summary, taskID); err != nil {
		t.Fatalf("dirty summary %s: %v", taskID, err)
	}
}

// TestGitBlameEnrichResolvesTrailerToRow is acceptance ①: a commit carrying a
// `task:` trailer comes back with all four attribution fields filled from the
// LEDGER, and the summary is the ledger's, not the commit's title.
func TestGitBlameEnrichResolvesTrailerToRow(t *testing.T) {
	newGitBlameEnrichLedger(t)
	const taskID = "01a195bb-4287-7ad5-a764-b6df563895dc"
	seedGitBlameTask(t, taskID, "run 越界写共享 checkout 的剩余守卫缺口", TaskStatusCompleted)

	repo := newGitBlameEnrichRepo(t)
	repo.write("guard.go", "line1\nline2\n")
	repo.commit("fix(tools): clone ground guard", taskID)

	res, raw := repo.blame("guard.go", 1, 2)
	if res.TaskEnrichment != gitBlameEnrichmentFull {
		t.Fatalf("task_enrichment = %q (note %q), want full — output: %s", res.TaskEnrichment, res.TaskEnrichmentNote, raw)
	}
	for i, it := range res.LineItems {
		if it.TaskID != taskID {
			t.Errorf("line %d task_id = %q, want %q", i+1, it.TaskID, taskID)
		}
		if it.TaskSummary != "run 越界写共享 checkout 的剩余守卫缺口" {
			t.Errorf("line %d task_summary = %q", i+1, it.TaskSummary)
		}
		if it.TaskStatus != TaskStatusCompleted {
			t.Errorf("line %d task_status = %q, want %q", i+1, it.TaskStatus, TaskStatusCompleted)
		}
		if it.TaskHost != "" {
			// This fixture's commit carries no `host:` trailer — the two axes
			// are independent (a peer row's host comes from its own trailer).
			t.Errorf("line %d task_host = %q, want empty (no host trailer on this commit)", i+1, it.TaskHost)
		}
	}
	if res.LedgerAccess.LedgerOpens != 1 || res.LedgerAccess.LedgerQueries != 1 {
		t.Errorf("ledger_access = %+v, want 1 open / 1 query", res.LedgerAccess)
	}
}

// TestGitBlameEnrichNoTrailerRepoStillBlames is acceptance ②/failure layer ①: a
// repository whose commits carry no `task:` trailer — i.e. any other project —
// returns its line data unchanged, leaves task_* empty, says WHY, and does not
// error.
func TestGitBlameEnrichNoTrailerRepoStillBlames(t *testing.T) {
	newGitBlameEnrichLedger(t)
	seedGitBlameTask(t, "01a10000-0000-7000-8000-000000000000", "some unrelated row", TaskStatusCompleted)

	repo := newGitBlameEnrichRepo(t)
	repo.write("plain.go", "a\nb\nc\n")
	repo.commit("chore: no trailer here")

	res, raw := repo.blame("plain.go", 1, 3)
	if len(res.LineItems) != 3 {
		t.Fatalf("expected 3 line_items, got %d — output: %s", len(res.LineItems), raw)
	}
	if res.TaskEnrichment != gitBlameEnrichmentNone {
		t.Errorf("task_enrichment = %q, want none", res.TaskEnrichment)
	}
	if !strings.Contains(res.TaskEnrichmentNote, "no `task:` trailer") {
		t.Errorf("note must name the missing trailer, got %q", res.TaskEnrichmentNote)
	}
	for i, it := range res.LineItems {
		if it.TaskID != "" || it.TaskSummary != "" || it.TaskStatus != "" || it.TaskHost != "" {
			t.Errorf("line %d carries attribution in a trailer-less repo: %+v", i+1, it)
		}
		if it.CommitHash == "" || it.CommitMessage != "chore: no trailer here" {
			t.Errorf("line %d lost its ordinary blame fields: %+v", i+1, it)
		}
	}
	// A window with no trailers must not open the ledger at all.
	if res.LedgerAccess.LedgerOpens != 0 || res.LedgerAccess.LedgerQueries != 0 {
		t.Errorf("ledger_access = %+v, want 0 opens / 0 queries", res.LedgerAccess)
	}
}

// TestGitBlameEnrichTrailerNamesUnknownRow is acceptance ③/failure layer ②: the
// trailer points at a task id this ledger does not hold. task_id is still
// reported (it came from git), everything ledger-derived stays empty, and the
// call is not an error.
func TestGitBlameEnrichTrailerNamesUnknownRow(t *testing.T) {
	newGitBlameEnrichLedger(t)
	// An EXISTING ledger that simply does not hold the ghost row — that is layer
	// ②, and seeding one unrelated row is what distinguishes it from layer ③
	// (no ledger at all) in the very same assertion set.
	seedGitBlameTask(t, "01a10000-0000-7000-8000-0000000000ff", "an unrelated row", TaskStatusCompleted)
	const ghost = "01a10000-0000-7000-8000-00000000dead"

	repo := newGitBlameEnrichRepo(t)
	repo.write("ghost.go", "x\n")
	repo.commit("fix: commit tagged with a row from another host", ghost)

	res, raw := repo.blame("ghost.go", 1, 1)
	if len(res.LineItems) != 1 {
		t.Fatalf("expected 1 line_item — output: %s", raw)
	}
	it := res.LineItems[0]
	if it.TaskID != ghost {
		t.Errorf("task_id = %q, want %q (the trailer value survives an unknown row)", it.TaskID, ghost)
	}
	if it.TaskSummary != "" || it.TaskStatus != "" {
		t.Errorf("ledger-derived fields must stay empty for an unknown row: %+v", it)
	}
	if res.TaskEnrichment != gitBlameEnrichmentNone {
		t.Errorf("task_enrichment = %q, want none", res.TaskEnrichment)
	}
	if !strings.Contains(res.TaskEnrichmentNote, ghost) {
		t.Errorf("note must name the unresolved id, got %q", res.TaskEnrichmentNote)
	}
	if res.LedgerAccess.LedgerQueries != 1 {
		t.Errorf("one distinct id ⇒ exactly one lookup, got %+v", res.LedgerAccess)
	}
}

// TestGitBlameEnrichNoLedgerOnThisHost is failure layer ③: the ledger file does
// not exist. blame answers normally, attribution is empty and explained, and —
// the part that a read must never get wrong — the read does NOT create the
// ledger.
func TestGitBlameEnrichNoLedgerOnThisHost(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-ledger-here.db")
	t.Setenv(memoryDBEnv, missing)

	repo := newGitBlameEnrichRepo(t)
	repo.write("noledger.go", "only\n")
	repo.commit("fix: tagged, but this host has no ledger", "01a10000-0000-7000-8000-0000000000aa")

	res, raw := repo.blame("noledger.go", 1, 1)
	if len(res.LineItems) != 1 {
		t.Fatalf("expected 1 line_item — output: %s", raw)
	}
	if res.LineItems[0].TaskID != "01a10000-0000-7000-8000-0000000000aa" {
		t.Errorf("trailer-derived task_id must survive a missing ledger: %+v", res.LineItems[0])
	}
	if res.TaskEnrichment != gitBlameEnrichmentNone {
		t.Errorf("task_enrichment = %q, want none", res.TaskEnrichment)
	}
	if !strings.Contains(res.TaskEnrichmentNote, "no task ledger") {
		t.Errorf("note must name the missing ledger, got %q", res.TaskEnrichmentNote)
	}
	// The tool OPENED no ledger (that is this feature's read contract, and the
	// counter is the assertion). The file may nevertheless exist afterwards:
	// storeBlameMemory's pre-existing best-effort linkage calls MemoryStore,
	// which creates a ledger it is pointed at. That is the old behaviour this
	// change deliberately leaves alone — which is exactly why "did a ledger
	// exist for me to consult" is sampled BEFORE the linkage runs.
	if res.LedgerAccess.LedgerOpens != 0 {
		t.Errorf("ledger opens = %d, want 0 when no ledger existed at read time", res.LedgerAccess.LedgerOpens)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Logf("ledger still absent after the call (%v) — either way the read opened nothing", err)
	}
}

// TestGitBlameEnrichDedupesCommitsAndOpensLedgerOnce is acceptance ④: a
// 200-line window drawn from 3 commits must cost 1 ledger open and 3 lookups —
// NOT 200 of either. The counters are the assertion.
func TestGitBlameEnrichDedupesCommitsAndOpensLedgerOnce(t *testing.T) {
	newGitBlameEnrichLedger(t)
	ids := []string{
		"01a10000-0000-7000-8000-000000000001",
		"01a10000-0000-7000-8000-000000000002",
		"01a10000-0000-7000-8000-000000000003",
	}
	for i, id := range ids {
		seedGitBlameTask(t, id, "row "+string(rune('A'+i)), TaskStatusCompleted)
	}

	repo := newGitBlameEnrichRepo(t)
	var b strings.Builder
	// 3 commits × 66 lines = 198, then a 4th commit adds the last 2: the file is
	// exactly gitBlameMaxSpan (200) lines long, so one max-width call covers
	// lines from FOUR distinct commits that name THREE distinct task ids (the
	// 4th commit reuses ids[2]) — which is the cost shape under test.
	for c, id := range ids {
		for n := 0; n < 66; n++ {
			b.WriteString("line\n")
		}
		repo.write("many.go", b.String())
		repo.commit("change "+string(rune('A'+c)), id)
	}
	b.WriteString("tail1\n")
	b.WriteString("tail2\n")
	repo.write("many.go", b.String())
	repo.commit("tail change", ids[2])

	res, raw := repo.blame("many.go", 1, gitBlameMaxSpan)
	if len(res.LineItems) != gitBlameMaxSpan {
		t.Fatalf("expected %d line_items, got %d — output head: %.200s", gitBlameMaxSpan, len(res.LineItems), raw)
	}
	access := res.LedgerAccess
	if access.Commits != 4 {
		t.Fatalf("distinct commits = %d, want 4 (the window's %d lines come from 4 commits)", access.Commits, gitBlameMaxSpan)
	}
	if access.LedgerOpens != 1 {
		t.Errorf("ledger opens = %d, want exactly 1 for the whole call", access.LedgerOpens)
	}
	if access.LedgerQueries != 3 {
		t.Errorf("ledger queries = %d, want 3 (distinct task ids, not %d lines)", access.LedgerQueries, gitBlameMaxSpan)
	}
	if access.TrailerLookups != access.Commits {
		t.Errorf("trailer lookups = %d, want one per distinct commit (%d)", access.TrailerLookups, access.Commits)
	}
	if res.TaskEnrichment != gitBlameEnrichmentFull {
		t.Errorf("task_enrichment = %q (%s), want full", res.TaskEnrichment, res.TaskEnrichmentNote)
	}
}

// TestGitBlameEnrichSanitizesSummary is acceptance ⑥: a row whose summary
// carries control/binary bytes (a legacy row, written before the ledger's write
// guard existed) must come back through sanitizeTaskSummary — the SAME
// function, not a second implementation — so the tool result can never carry a
// NUL or a raw escape the ledger itself would have escaped.
func TestGitBlameEnrichSanitizesSummary(t *testing.T) {
	newGitBlameEnrichLedger(t)
	const taskID = "01a10000-0000-7000-8000-0000000000c1"
	seedGitBlameTask(t, taskID, "placeholder", TaskStatusCompleted)
	dirty := "resolve conflict\x00 for fix/x — steps\x01two\nnext line"
	dropGitBlameTaskSummary(t, taskID, dirty)

	repo := newGitBlameEnrichRepo(t)
	repo.write("dirty.go", "z\n")
	repo.commit("fix: row with a dirty summary", taskID)

	res, raw := repo.blame("dirty.go", 1, 1)
	if len(res.LineItems) != 1 {
		t.Fatalf("expected 1 line_item — output: %s", raw)
	}
	got := res.LineItems[0].TaskSummary
	if want := sanitizeTaskSummary(dirty); got != want {
		t.Fatalf("task_summary = %q, want the sanitizer's own output %q", got, want)
	}
	if strings.ContainsRune(got, 0x00) || strings.ContainsRune(got, 0x01) || strings.Contains(got, "\n") {
		t.Errorf("sanitized summary still carries a control byte or newline: %q", got)
	}
	// The guard's evidence-preserving behaviour: the binary run is replaced by
	// a marker rather than silently dropped (Rule 15).
	if !strings.Contains(got, taskSummaryBinaryMarker) {
		t.Errorf("sanitized summary lost its binary marker: %q", got)
	}
	if strings.ContainsRune(raw, 0x00) {
		t.Errorf("the tool's JSON output carries a raw NUL byte")
	}
}

// TestGitBlameEnrichPartialWhenSomeCommitsResolve pins the `partial` verdict:
// one commit names a real row, the other names nothing, so the window is
// half-attributed and the enum must say so rather than claim `full` or `none`.
func TestGitBlameEnrichPartialWhenSomeCommitsResolve(t *testing.T) {
	newGitBlameEnrichLedger(t)
	const taskID = "01a10000-0000-7000-8000-0000000000d1"
	seedGitBlameTask(t, taskID, "the tagged commit's purpose", TaskStatusVerified)

	repo := newGitBlameEnrichRepo(t)
	repo.write("mixed.go", "first\n")
	repo.commit("chore: untagged", taskID) // placeholder content, replaced below
	repo.write("mixed.go", "first\nsecond\n")
	repo.commit("fix: untagged (overwrites line 2 only)")

	res, raw := repo.blame("mixed.go", 1, 2)
	if len(res.LineItems) != 2 {
		t.Fatalf("expected 2 line_items — output: %s", raw)
	}
	if res.TaskEnrichment != gitBlameEnrichmentPartial {
		t.Fatalf("task_enrichment = %q (%s), want partial — output: %s", res.TaskEnrichment, res.TaskEnrichmentNote, raw)
	}
	if res.LineItems[0].TaskID != taskID || res.LineItems[0].TaskStatus != TaskStatusVerified {
		t.Errorf("line 1 should carry the tagged row: %+v", res.LineItems[0])
	}
	if res.LineItems[1].TaskID != "" {
		t.Errorf("line 2 belongs to the untagged commit, got %+v", res.LineItems[1])
	}
}

// TestGitBlameUniqueCommitsDedupes is the cheap unit guard under the cost
// assertions: the dedup must preserve FIRST-APPEARANCE order and drop blanks
// (a boundary header of all zeros is a real hash and must be kept).
func TestGitBlameUniqueCommitsDedupes(t *testing.T) {
	items := []GitBlameLineItem{
		{CommitHash: "aaa"},
		{CommitHash: "aaa"},
		{CommitHash: "bbb"},
		{CommitHash: ""},
		{CommitHash: "aaa"},
		{CommitHash: "0000000000000000000000000000000000000000"},
	}
	got := gitBlameUniqueCommits(items)
	want := []string{"aaa", "bbb", "0000000000000000000000000000000000000000"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unique commits = %v, want %v", got, want)
	}
}
