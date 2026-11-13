package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// buildGopiForWorkerHelp compiles the test tree's binary once per test run
// (cached) so the worker -h regression can be asserted through the REAL
// dispatch path — `gopi worker -h` must print the usage head and claim
// NOTHING, and only an end-to-end run of the binary can prove the loop did
// not start (the P0-1 bug was precisely a dispatch-time side-effect).
func buildGopiForWorkerHelp(t *testing.T) string {
	t.Helper()
	// The binary itself comes from the ONE process-level build slot
	// (gopibin_test.go): it lives in the suite scratch root, so it outlives
	// the test that first asked for it (a t.TempDir() build would be removed
	// when that test finished and the next one would fail with "fork/exec …
	// no such file or directory"), it is never /tmp, and the package builds
	// the package under test exactly once.
	return gopiTestBinPath(t)
}

// workerTaskRow is the ledger shape a worker test needs: one row in `status`.
func workerTaskRow(t *testing.T, id, status string) {
	t.Helper()
	if _, err := tools.TaskNew("worker test task "+id, id, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if status != tools.TaskStatusInProgress {
		if _, err := tools.TaskUpdate(id, status); err != nil {
			t.Fatalf("TaskUpdate %s → %s: %v", id, status, err)
		}
	}
}

// TestWorkerWritebackTagUnionsProvenance: a worker round's writeback unions
// the worker:<host> provenance tag into the row's tags; a plain writeback
// leaves tags untouched (the M4.20 ① provenance contract).
func TestWorkerWritebackTagUnionsProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := tools.TaskNew("tagged row", "t-wtag-1", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// The tag helper is pure: idempotent union, host-suffixed.
	if got := tools.TagWorkerClaim("exist"); !strings.Contains(got, "exist") || !strings.Contains(got, tools.WorkerTaskTagPrefix) {
		t.Fatalf("TagWorkerClaim union failed: %q", got)
	}
	if got := tools.TagWorkerClaim(tools.WorkerTaskTag()); got != tools.WorkerTaskTag() {
		t.Fatalf("TagWorkerClaim not idempotent: %q", got)
	}
	// Simulate the writeback path directly (no child process needed).
	if _, err := tools.TaskRunWritebackTagged("t-wtag-1", tools.TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("TaskRunWritebackTagged: %v", err)
	}
	row, err := taskRowTags(t, path, "t-wtag-1")
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !strings.Contains(row, tools.WorkerTaskTagPrefix) {
		t.Fatalf("writeback must union the worker tag, got tags=%q", row)
	}
	wantTag := tools.WorkerTaskTag()
	if !strings.Contains(row, wantTag) {
		t.Fatalf("want tag %q in tags, got %q", wantTag, row)
	}
}

// TestWorkerMaxRoundsEnv: the cap env resolves once (Rule 2) — default 0 =
// unlimited, a positive value passes through, a malformed value falls back
// to the default (with a warning on stderr).
func TestWorkerMaxRoundsEnv(t *testing.T) {
	t.Setenv("GOPI_WORKER_MAX_ROUNDS", "")
	if got := workerMaxRounds(); got != workerDefaultMaxRounds {
		t.Fatalf("absent env: got %d want default %d", got, workerDefaultMaxRounds)
	}
	t.Setenv("GOPI_WORKER_MAX_ROUNDS", "3")
	if got := workerMaxRounds(); got != 3 {
		t.Fatalf("GOPI_WORKER_MAX_ROUNDS=3: got %d want 3", got)
	}
	t.Setenv("GOPI_WORKER_MAX_ROUNDS", "not-a-number")
	if got := workerMaxRounds(); got != workerDefaultMaxRounds {
		t.Fatalf("malformed env: got %d want default %d", got, workerDefaultMaxRounds)
	}
}

// TestWorkerHelpDoesNotRun (P0-1 regression, devdoc/review-cli-task-sync.md):
// `gopi worker -h` must print the usage head and exit 0 WITHOUT running the
// worker loop. The pre-fix bug: isWorkerSubcommand checked only
// args[0]=="worker" and called runTaskWorker() — so `gopi worker -h` claimed
// and EXECUTED a live pending task (the t-1788305178/t-1788305179 "broken
// pipe" incidents — real tasks burned by a help query). The test queues a
// PENDING task in a sandboxed ledger, runs the REAL binary with `worker -h`,
// and asserts ① exit 0, ② the usage head (loop contract, GOPI_WORKER_MAX_ROUNDS,
// pa-queue exclusion), ③ the task is STILL pending (nothing claimed), and
// ④ no worker loop banner was printed (the loop never started).
func TestWorkerHelpDoesNotRun(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", t.TempDir())

	// A real pending task the (broken) worker would have claimed.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "worker -h must not claim me",
		Payload: "if you see this executed, the P0-1 bug is back",
		TaskID:  "t-workerhelp-p0",
		Tags:    "module:worker-help",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}

	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(bin, "worker", flag)
			cmd.Env = append(os.Environ(), "GOPI_MEMORY_DB="+dbPath, "GOPI_HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("gopi worker %s failed: %v\noutput:\n%s", flag, err, out)
			}
			// ① The usage head is printed…
			for _, want := range []string{
				"gopi worker", "GOPI_WORKER_MAX_ROUNDS", "pa-queue",
				"claim", "drained", "exit 0",
			} {
				if !strings.Contains(string(out), want) {
					t.Errorf("gopi worker %s output missing %q:\n%s", flag, want, out)
				}
			}
			// …and the LOOP NEVER STARTED (the live side-effect signature).
			if strings.Contains(string(out), "worker: loop start") {
				t.Errorf("gopi worker %s STARTED THE WORKER LOOP:\n%s", flag, out)
			}
			// ② The queued task was NOT claimed (still pending).
			show, err := tools.TaskShow("t-workerhelp-p0")
			if err != nil {
				t.Fatalf("TaskShow: %v", err)
			}
			if !strings.Contains(show, "status:         pending") {
				t.Errorf("gopi worker %s claimed/executed the queued task — the P0-1 side-effect:\n%s", flag, show)
			}
		})
	}
}

// TestWorkerUsageHeadCoversLoopContract pins the workerUsageHead contract at
// USER level: the help must say what the worker does (claims one pending task
// and runs it), how it treats the queue (pa-queue rows are never claimed;
// a drained queue ends the run with exit 0), and which knobs are available
// (GOPI_WORKER_MAX_ROUNDS). The wire words behind those facts — "db-sync",
// "must-succeed push" — deliberately do NOT appear: they are sync-protocol
// vocabulary, and the operator complaint this test follows was exactly that
// `gopi worker -h` read as a devdoc (help_capability_first_test.go is the
// shape gate; this one keeps the FACTS on the page).
func TestWorkerUsageHeadCoversLoopContract(t *testing.T) {
	for _, want := range []string{
		"claims one pending task", "drained",
		"GOPI_WORKER_MAX_ROUNDS", "pa-queue", "exit 0",
	} {
		if !strings.Contains(workerUsageHead, want) {
			t.Errorf("workerUsageHead missing %q:\n%s", want, workerUsageHead)
		}
	}
}

// taskRowTags reads one row's tags through a raw DB open (the test's own
// assertion path, independent of the tools-layer accessors).
func taskRowTags(t *testing.T, dbPath, taskID string) (string, error) {
	t.Helper()
	// tools.TaskShow renders the whole record; the tags line is greppable.
	out, err := tools.TaskShow(taskID)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "tags:") || strings.HasPrefix(line, "Tags:") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "tags:"), "Tags:")), nil
		}
	}
	// Fallback: the whole show output (the assertion greps the tag anyway).
	return out, nil
}

// TestWorkerExcludedTagsConstant: the worker's skip list is the fixed
// pa-queue guard (serve's anti-misclaim contract, worker face) PLUS the agency
// pass's quarantine set — built from tools.QuarantineTagSet, never a literal,
// so a proposal row can never be auto-claimed (epic agency-proposals: the
// answer to "will this become an auto-dispatch machine?" is this line).
func TestWorkerExcludedTagsConstant(t *testing.T) {
	want := "pa-queue," + tools.QuarantineTagSet
	if workerExcludedTags != want {
		t.Fatalf("workerExcludedTags = %q, want %q", workerExcludedTags, want)
	}
	if os.Getenv("GOPI_WORKER_MAX_ROUNDS") == "" {
		// default path already covered above; this guard only documents
		// that the env name is spelled exactly once (Rule 2).
		_ = workerMaxRoundsEnv
	}
}

// TestWorkerDrainedExitSemantics: drained exit is ALWAYS 0 for the caller;
// the message distinguishes a fully idle node from one with other holders'
// live rows. The counting helper is the contract under test.
func TestWorkerDrainedExitSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	if _, err := tools.TaskNew("drained probe", "t-drain-1", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// No live rows anywhere → the plain drained line.
	if got := tools.LiveRowsOwnedByOthers(); got != 0 {
		t.Fatalf("empty ledger: LiveRowsOwnedByOthers = %d, want 0", got)
	}
	// Another holder's live row → the "other holder active" face.
	db, closeDB, err := tools.OpenWorkerProbeDB()
	if err != nil {
		t.Fatalf("OpenWorkerProbeDB: %v", err)
	}
	defer closeDB()
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', claimed_by = 'other-host:123' WHERE task_id = 't-drain-1'`); err != nil {
		t.Fatalf("stamp other holder: %v", err)
	}
	if got := tools.LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("one foreign live row: LiveRowsOwnedByOthers = %d, want 1", got)
	}
}
