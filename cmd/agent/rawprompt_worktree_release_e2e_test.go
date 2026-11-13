package main

// rawprompt_worktree_release_e2e_test.go — the RED repro for the epic
// "gopi running with raw prompt" leaves its worktree behind.
//
// THE DEFECT (measured, not inferred): a run launched from a RAW PROMPT is the
// face with NO wrapper row — task-single-row-exec gives `task run`/`worker` a
// human row carrying deliverable{repo,branch}, and every release arm in the
// tree keys on that deliverable. A bare prompt instead auto-registers a row
// from internal/agent/taskregister.go, and that row is born with an EMPTY
// deliverable. Consequences, in order:
//
//	① the executor's deliverable.worktree backfill seam
//	   (internal/tools/task_deliverable_worktree.go) refuses a row with no
//	   deliverable blob AT ALL ("no deliverable blob (auto mirror rows) — nothing
//	   to merge"), so the Rule-6 worktree the executor created by running
//	   `git worktree add` is recorded NOWHERE;
//	② every reclaim arm selects rows by `deliverable LIKE '%worktree%'`
//	   (ReclaimOwnFinishedWorktrees) or by a parsed deliverable.worktree
//	   (finish-cleanup server arm) — an empty blob matches neither;
//	③ so the directory stays registered, `repoSweepProtectedBranches` keeps
//	   reading the branch as worktree-protected ACTIVE BOND WORK, and the
//	   merger never merges the finished deliverable — with nothing anywhere
//	   saying why.
//
// WHAT THIS FILE PINS (the acceptance): a raw-prompt run that creates a Rule-6
// worktree and commits in it must, at exit, have (a) recorded the worktree and
// branch on its auto row and (b) released the directory — while the BRANCH
// survives as the merger's input. The unreleased-directory assertion is the
// RED half: on the unfixed tree the directory is still registered after the
// run exits.
//
// The walk drives the REAL binary with a scripted mock LLM (zero API calls),
// a throwaway git repo, and a sandboxed GOPI_HOME/GOPI_MEMORY_DB — the shape
// cmd/agent/task_session_cwd_e2e_test.go established.

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tasksource"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// rawPromptWorktreeBranch is the branch the scripted executor creates its
// Rule-6 worktree on — the merger's input that must survive the release.
const rawPromptWorktreeBranch = "feat/rawprompt-release-e2e"

// rawPromptPreReleaseSHAFile is the file the run's OWN executor writes its
// branch tip into, from inside its tool round — i.e. BEFORE the release leg
// (a close-out tail of the same process) can touch the repository. It lives
// OUTSIDE the fixture repo (its parent directory), so no release arm can
// withdraw the reading along with the branch it describes.
const rawPromptPreReleaseSHAFile = "pre-release-branch-sha.txt"

// rawPromptSHALen is the length of a full object name as git prints it, and
// the shape rawPromptLooksLikeSHA insists on.
const rawPromptSHALen = 40

// rawPromptGit runs one git command in dir with a hermetic identity/config
// (the host's global config must not leak into the fixture repo).
func rawPromptGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// rawPromptLooksLikeSHA reports whether s names a commit object (40 lowercase
// hex characters). It is the predicate that keeps two verdicts DISJOINT: a
// pre-release record holding something that is not a sha means the executor's
// tip round never RESOLVED a branch (so the fixture never got to the behaviour
// under test), NOT that a deliverable was destroyed.
//
// The trap it closes is git's own: `git rev-parse refs/heads/<missing>` does
// not fail — it ECHOES a non-resolving argument back on stdout (measured
// 2026-10-31, the fixture variant with no `git worktree add`: the record held
// "refs/heads/feat/rawprompt-release-e2e" and assertion ② reported a product
// defect for a fixture that never made a branch). `--verify --quiet` is what
// makes the question answerable, and this predicate is the belt beside it.
func rawPromptLooksLikeSHA(s string) bool {
	if len(s) != rawPromptSHALen {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) })
}

// rawPromptBranchSHA resolves the commit a branch tip points at, and whether
// that branch exists at all — the ONE reading assertion ② carries across the
// release.
//
// It is deliberately NOT rawPromptGit: a MISSING branch is an ANSWER this
// helper must RETURN (that is the product defect ② names), not an error that
// kills the test with git's own wording and no product sentence. `rev-parse
// --verify --quiet` exits 1 for a ref that does not resolve, and any other
// failure is reported as the unanswerable question it is (Rule 15).
func rawPromptBranchSHA(t *testing.T, repo, branch string) (sha string, exists bool) {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repo
	cmd.Env = testutil.GitEnv("GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", false
		}
		t.Fatalf("git rev-parse --verify refs/heads/%s in %s: %v", branch, repo, err)
	}
	return strings.TrimSpace(string(out)), true
}

// rawPromptRepo builds the fixture: a real git repo on master, and the path of
// the linked worktree the scripted executor will create (NOT created here —
// creating it is the run's own job, which is the behaviour under test).
func rawPromptRepo(t *testing.T) (repo, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	rawPromptGit(t, root, "init", repo)
	rawPromptGit(t, repo, "config", "user.email", "t@t")
	rawPromptGit(t, repo, "config", "user.name", "t")
	rawPromptGit(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	rawPromptGit(t, repo, "add", "a.txt")
	rawPromptGit(t, repo, "commit", "-m", "init")
	return repo, filepath.Join(root, "wt")
}

// TestRawPromptRunReleasesItsWorktree is the epic's acceptance walk on the real
// binary: the raw-prompt face must record AND release the Rule-6 worktree its
// executor created, keeping the branch as the merger's input.
func TestRawPromptRunReleasesItsWorktree(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	repo, wt := rawPromptRepo(t)

	// The scripted executor does exactly what a real one does under GOPI.md
	// Rule 6a: create the linked worktree from the main checkout, commit the
	// work in it, then answer. Requests past the script repeat the last entry,
	// so the run terminates on its own.
	//
	// THE LAST ROUND RECORDS THE TIP BEFORE THE RELEASE, and it is the RUN'S
	// process that does it: the release leg is a close-out tail of the very
	// process serving these rounds, so a reading TAKEN HERE is provably
	// PRE-RELEASE (a post-mortem read of the repository cannot say what was
	// there before the release ran — measured: the branch's tip and its
	// eventual destruction are indistinguishable in the survivor's view). The
	// file is written OUTSIDE the fixture repo so no release arm can withdraw
	// the reading together with the branch; assertion ② is what compares it.
	preReleaseSHA := filepath.Join(filepath.Dir(repo), rawPromptPreReleaseSHAFile)
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_wt_add", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "git worktree add " + wt + " -b " + rawPromptWorktreeBranch + " master", "workdir": repo}}},
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_wt_commit", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "git -C " + wt + " commit --allow-empty -m 'raw prompt work'", "workdir": repo}}},
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_wt_tip", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "git rev-parse refs/heads/" + rawPromptWorktreeBranch + " > " + preReleaseSHA, "workdir": repo}}},
		testutil.Resp{Text: "worktree work committed"},
	)
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		"GOPI_AUTO_TASK_REGISTER=1",
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
	// The raw prompt is a positional argument — the face with NO ledger row of
	// its own, which is the whole point (a `task run` here would test the other
	// face). cwd = the repo: the launch context whose git toplevel the auto row
	// must record as deliverable.repo.
	stdout, stderr, code := runGopiMapped(t, []string{"create a worktree and commit the work"},
		"", env, repo)
	if code != 0 {
		t.Fatalf("the raw-prompt run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// FIXTURE SANITY — it answers ONE question: did the fixture ever get to the
	// behaviour under test. It is NOT the voice for a destroyed deliverable.
	//
	// WHY THE SPLIT (measured): a branch missing after the run used to be
	// reported HERE, and the message read "the scripted executor did not create
	// branch … — the fixture never got to the behaviour under test", i.e. a
	// broken test scaffold. A release leg that deletes the deliverable produces
	// exactly the same observation, so the product defect was never named by
	// the assertion that exists for it (②). The run's own executor now records
	// the branch tip BEFORE the release can run, and that reading decides which
	// of the two happened:
	//
	//	· no record at all, or an EMPTY one (the executor's tip round did not
	//	  resolve any branch) → the fixture never made a branch: ABORT here, the
	//	  way it always did (that is the vacuous pass this guard exists for).
	//	· a RECORDED TIP but no branch after the run → the fixture did get to the
	//	  behaviour, so this is NOT a scaffold fault: say both possibilities and
	//	  fall through, so assertion ② names the product defect with the tip it
	//	  holds. Aborting here is what buried ②.
	postRunSHA, postRunExists := rawPromptBranchSHA(t, repo, rawPromptWorktreeBranch)
	if !postRunExists {
		recorded, _ := os.ReadFile(preReleaseSHA) //stylegate:ignore the read's error is classified right below: a missing record, a non-sha record and a recorded tip are three different verdicts
		tip := strings.TrimSpace(string(recorded))
		// ONLY the absent-reading shapes are this check's verdict, and it stays
		// SILENT on the third one: a tip WAS recorded before the release, so the
		// fixture did get to the behaviour and the disappearance is not a
		// scaffold fault. Assertion ② below holds both readings and names which
		// one it is — one voice per question, so the red arm names the defect.
		if !rawPromptLooksLikeSHA(tip) {
			if _, statErr := os.Stat(preReleaseSHA); statErr == nil {
				t.Fatalf("the scripted executor's PRE-release tip record at %s holds %q, which is not a commit sha — its tip round did not RESOLVE branch %s (`git rev-parse` echoes a non-resolving argument instead of failing), so the fixture never got to the behaviour under test and no tip was there to destroy\nstdout:\n%s\nstderr:\n%s",
					preReleaseSHA, tip, rawPromptWorktreeBranch, stdout, stderr)
			}
			t.Fatalf("the scripted executor did not create branch %s — the fixture never got to the behaviour under test (its own PRE-release tip record at %s is absent too, so nothing was destroyed; this run never made a branch)\nstdout:\n%s\nstderr:\n%s",
				rawPromptWorktreeBranch, preReleaseSHA, stdout, stderr)
		}
	}

	// ASSERTION ① — the merger's view. `git worktree list` must no longer name
	// the directory: the registration is what scripts/safe-merge.sh gate [c]
	// greps, and what makes the merger read the branch as ACTIVE BOND WORK.
	if listed := rawPromptGit(t, repo, "worktree", "list"); strings.Contains(listed, wt) {
		t.Errorf("the raw-prompt run must RELEASE the worktree it created — the directory is still registered:\n%s\n\n"+
			"An unregistered-but-present directory is equally wrong: git worktree list is the merger's input.", listed)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("the worktree directory %s still exists after the run exited", wt)
	}

	// ASSERTION ② — the branch SURVIVES the release, and it is checked against
	// a state only the release leg can destroy: the TIP the run's own executor
	// recorded BEFORE the release could run. Reading the sha and comparing it
	// to the post-run sha is what keeps this assertion independent of the
	// fixture sanity above — "a branch with this name exists" was satisfiable
	// by any branch, and the sanity check spoke first whenever none did, so a
	// release leg that deletes the deliverable used to be reported as a broken
	// scaffold. Both readings are needed: the name (something must still be
	// there) AND the tip (it must be THE deliverable, not a re-created branch).
	// The release removes the DIRECTORY; the branch is the merger's input.
	record, readErr := os.ReadFile(preReleaseSHA)
	preReleaseTip := strings.TrimSpace(string(record))
	switch {
	case readErr != nil || !rawPromptLooksLikeSHA(preReleaseTip):
		// The sanity check above already decides this case (a run that left no
		// usable reading never got to the behaviour under test). Not a pass: the
		// assertion says so in its own words rather than staying silent — and a
		// non-sha record (git echoing an unresolved ref) must never reach the
		// comparison below, or the fixture fault would be reported as a defect.
		t.Errorf("assertion ② has no PRE-release tip to carry across the release: %s holds %q (%v) — not a commit sha, so this assertion was never exercised", preReleaseSHA, preReleaseTip, readErr)
	case !postRunExists:
		t.Errorf("THE RELEASE DESTROYED THE DELIVERABLE: branch %s (tip %s, read from the run's own executor BEFORE its release leg) no longer resolves after the run exited — the release must remove the WORKTREE DIRECTORY only; the branch is the merger's input and its absence is a product defect, not a fixture fault", rawPromptWorktreeBranch, preReleaseTip)
	case preReleaseTip != postRunSHA:
		t.Errorf("the branch %s no longer points at the deliverable: tip was %s BEFORE the release and is %s after the run exited — the release leg must leave the commit it releases alone (a rewritten tip is a destroyed deliverable, even though a branch of that name still exists)", rawPromptWorktreeBranch, preReleaseTip, postRunSHA)
	}

	// ASSERTION ③ — the ledger's account. The auto row must name the worktree
	// and branch, which is what makes the row auditable and lets any later arm
	// (reclaim/sweep) attribute the directory. Read through the production
	// resolver, not by string-matching the blob (Rule 13: one parser).
	sessionID := rawPromptRunSessionID(t, gopiHome)
	rowID, source := rawPromptRunRow(t, dbPath, sessionID)
	if rowID == "" {
		t.Fatalf("the raw-prompt run registered no ledger row for session %s — nothing to attribute the worktree to\nstdout:\n%s\nstderr:\n%s", sessionID, stdout, stderr)
	}
	// THE PREMISE, asserted rather than assumed (provenance is what a reader
	// narrates the row through, and a stale spelling is what made this test
	// red — see rawPromptRunRow's note).
	if source != tasksource.Server {
		t.Fatalf("fixture premise: the run row %s carries source=%q, want %q — the value internal/agent/taskregister.go stamps today; a test that looks the row up by a STALE provenance would read an empty ledger\nrows:\n%s",
			rowID, source, tasksource.Server, rawPromptRows(t, dbPath))
	}
	// The production resolver reads the ledger through memoryDBPath(), so point
	// THIS process's ledger at the run's own DB (t.Setenv: restored at test
	// end, never leaked into a sibling test).
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	gotWT, gotBranch := tools.TaskDeliverableWorktreeFor(rowID)
	if gotWT != wt || gotBranch != rawPromptWorktreeBranch {
		t.Errorf("auto row %s deliverable = (worktree %q, branch %q), want (%q, %q) — the executor's creation moment must be recorded on the row the run itself registered\nrows:\n%s\nstdout:\n%s\nstderr:\n%s",
			rowID, gotWT, gotBranch, wt, rawPromptWorktreeBranch, rawPromptRows(t, dbPath), stdout, stderr)
	}
}

// rawPromptRows renders every ledger row (id/status/blob) — the diagnostic
// half of assertion ③, so a failure shows the ledger truth rather than only
// the row the test happened to pick.
func rawPromptRows(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := rawPromptLedger(t, dbPath)
	if err != nil {
		return "(row dump failed: " + err.Error() + ")"
	}
	rows, err := db.Query(`select task_id, status, deliverable from tasks;`)
	if err != nil {
		return "(row dump failed: " + err.Error() + ")"
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, status, deliverable string
		if err := rows.Scan(&id, &status, &deliverable); err != nil {
			return "(row dump failed: " + err.Error() + ")"
		}
		b.WriteString(id + " | " + status + " | " + deliverable + "\n")
	}
	if err := rows.Err(); err != nil {
		return "(row dump failed: " + err.Error() + ")"
	}
	return b.String()
}

// rawPromptLedger opens the run's sandbox ledger READ-ONLY through production's
// own DSN, for the assertions in this file.
//
// WHY NOT THE `sqlite3` CLI (measured on this host, 2026-10-30): the helper used
// to shell out to `exec.Command("sqlite3", …)`, and sqlite3 is NOT installed here
// (GOPI.md §3) — so the query failed with ENOENT and the helper called
// `t.Skipf`. Two things were wrong with that, and the second is the reason this
// was a defect rather than an environment gap:
//
//	① the failure was reported through the SQLITE3 CLI's absence, which has
//	   nothing to do with the product under test — an environment gap wearing the
//	   same "FAIL" as a release defect;
//	② `t.Skipf` cannot skip a test that has ALREADY failed (assertions ①/② run
//	   above it), so it printed `rawprompt_worktree_release_e2e_test.go:174:
//	   sqlite3 unavailable …` as the LAST line before FAIL — a diagnostic that
//	   READS like the cause of the failure. It never was: the verdict was already
//	   decided by ①. What it really cost was assertion ③'s COVERAGE, silently, on
//	   every host without the CLI.
//
// The in-process read removes both: the driver is already compiled into every
// gopi binary and every cmd/agent test binary (modernc.org/sqlite), production's
// read-only DSN is the one shape this suite opens ledgers with
// (tools.LedgerReadOnlyDSN — the DSN contract's busy timeout included), and a
// failure now means the LEDGER could not be read, which is a real finding.
func rawPromptLedger(t *testing.T, dbPath string) (*sql.DB, error) {
	t.Helper()
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("the run left no ledger at %s: %w", dbPath, err)
	}
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open ledger %s: %w", dbPath, err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore a read-only assertion handle's teardown; the assertions already ran and a Close error here can change no verdict
	return db, nil
}

// rawPromptRunSessionID returns the session id of the run just executed — the
// key that identifies THE RUN'S OWN ROW, read from the run directory the
// process itself created (<gopi-home>/<profile>/sessions/<session_id>/). It is
// deliberately NOT a ledger column: the session id is what the run's context
// carries, which is the ONE fact this test owns without any assumption about
// how a row describes itself.
func rawPromptRunSessionID(t *testing.T, gopiHome string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName))
	if err != nil {
		t.Fatalf("the raw-prompt run left no session directory under %s: %v", gopiHome, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// A resumed run adds a <session_id>.jsonl symlink BESIDE the dir;
		// the directory entry is the session id we want, and a fresh run
		// writes exactly one.
		return e.Name()
	}
	t.Fatalf("the raw-prompt run left no session directory under %s", gopiHome)
	return ""
}

// rawPromptRunRow returns the run's ledger row id AND its provenance, for the
// session that ran it.
//
// THE LOOKUP KEY IS THE SESSION, NOT THE PROVENANCE — and that is the fix, not
// a preference. The query this helper replaces read
// `select task_id from tasks where source='auto' …`, keying on the RETIRED
// spelling of the row's provenance: be17a9bd (feat(tasksource), 2026-09-23)
// made the ledger derive provenance from the process signature and made every
// writer name its ROLE, and internal/agent/taskregister.go has stamped
// tasksource.Server ever since. No writer mints 'auto' any more, so the
// predicate matched NOTHING while the stderr still said "task ledger:
// registered …" — the run registered its row, the test could not see it, and
// the failure read as "registered no auto ledger row" (measured 2026-09-26 at
// master, red at every revision back to the pre-merge base). Same regression
// class as TaskReconcileMirrorCloseout's `and source = 'auto'` second key
// (4f2255ac): identity must not be keyed on a spelling the write side retired.
// The session id IS the run's identity (taskregister.go writes the session UUID
// into the session_id column beside the row), so the row is found whatever its
// provenance is — and the caller separately ASSERTS the provenance, so a future
// change of writer value is reported as the premise it breaks instead of as a
// phantom "nothing was registered".
//
// The session id reaches the query as a BOUND PARAMETER, never spliced into
// the query text — the shape production's own query uses (`WHERE session_id = ?`,
// taskledger.go). The value is a session UUID this run minted inside the
// test's own temporary sandbox, read back READ-ONLY.
func rawPromptRunRow(t *testing.T, dbPath, sessionID string) (id, source string) {
	t.Helper()
	db, err := rawPromptLedger(t, dbPath)
	if err != nil {
		t.Fatalf("assertion ③ cannot read the ledger: %v", err)
	}
	// The parameter binding is production's own shape (`WHERE session_id = ?`,
	// taskledger.go) — the value is a session UUID this run minted inside the
	// test's own temporary sandbox, read back READ-ONLY.
	err = db.QueryRow(`select task_id, source from tasks where session_id = ? order by created_at desc limit 1`, sessionID).
		Scan(&id, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ""
	}
	if err != nil {
		t.Fatalf("assertion ③: the ledger query for session %s failed: %v", sessionID, err)
	}
	return strings.TrimSpace(id), strings.TrimSpace(source)
}
