package main

// COMPOSITION acceptance for the worker's worktree-cwd release (epic
// cwd-release-composition, 2026-09-16).
//
// THE GAP THIS FILE CLOSES. Two guards already exist around the same defect and
// NEITHER can see it end to end:
//
//   - internal/standards/cwd_release_gate_test.go is a STATIC go/ast gate: it
//     proves the release CALL is present in a worktree-removing producer's
//     source, never that the process it runs in ends up outside the directory.
//   - cmd/agent/cwd_release_rig_test.go is a UNIT rig: a re-exec'd holder calls
//     worktreerelease.Release() and lets the parent remove the tree. It proves
//     the ACTION works, not the COMPOSITION — it says nothing about whether the
//     real `gopi worker` reaches that call at the moment its own round has
//     removed the directory it is standing in.
//
// So this test drives the REAL binary. It is the only shape that can: the
// release is a process's own chdir (Linux has no syscall to move another
// process's cwd), so a fixture must be an actual `gopi worker` process.
//
// THE COMPOSITION IT DRIVES (all pre-existing production code, none of it
// re-implemented here):
//
//	submit a row for the fixture repo → `gopi worker` claims it → the round
//	enters the task's session dir, runs the executor child to completion
//	(taskregister's close-out chain), and then its round-end pass
//	(workerRoundOwnWorktrees) frees the finished row's OWN linked worktree and
//	releases the process's cwd (worktreerelease.ReleaseReporting).
//
// THE ORDERING THE ROUND ACTUALLY TAKES (measured, not assumed): the worker
// moves INTO the task's session container for the run (taskWorkdirEnter) and
// restores its ORIGIN at the tail — and for this fixture the origin IS inside
// the worktree the close-out removes, so the restore cannot complete and reports
// "workdir origin … is gone — staying in the current directory" on stderr. The
// process is therefore left standing in the session container, a directory OF
// the removed worktree, and the release that follows in the same close-out is
// the ONLY thing that unpins it. That is the composition, and the readings show
// it end to end: "wt → wt/.gopi/sessions/<id> → wt/.gopi/sessions/<id> (deleted) → /".
//
// WHY THE FIXTURE ALSO STAMPS THE ROW'S GROUND (added 2026-09-26, the red run
// this file spent a day in). The shape above — a worker launched INSIDE the very
// linked worktree whose row it releases — is exactly the actor position the
// guest-row guard refuses (internal/tools/guest_row_guard.go, epic 01a0c2b5,
// 2026-09-21: a process standing in a LINKED worktree of the repository whose
// row it reconciles IS a guest of it). With the fixture's hand-made worktree
// owned by NO row, the round's own release was refused (arm
// guestRowOwnReleaseRefusal) and the worktree was never freed, so the
// composition never ran and the test was RED for a reason that had nothing to
// do with the release it exists to prove.
//
// The guard is not weakened for this. The producer close-out carries its OWN
// mark (internal/tools/reclaim_own_ground.go, SelfGroundReleaseArm*): it arms
// the exact set of grounds the pass is closing out, and every rung that would
// refuse a process standing in its own closing ground — the guest receiver arm
// AND the own-ground rung AND the 5m0s age floor — is skipped per MEMBERSHIP in
// that set. The guest arm's skip is keyed on the ROW'S RECORDED GROUND
// (`r.Worktree`), which is why the fixture must do what it now does: stamp the
// fixture worktree onto the row through the PRODUCTION backfill seam
// (tools.TaskDeliverableWorktreeBashForRow — the very call the executor's bash
// round makes), so the directory under test IS the row's recorded ground and
// the closing pass is entitled to free it. Two consequences, both deliberate:
//
//   - the round's `git worktree remove` and its cwd release are REACHED (that
//     is the composition), and both are performed by the process standing
//     inside the directory being removed;
//   - the 5m0s age floor is skipped for THIS ground by the same membership, so
//     a fresh fixture registration is not mistaken for a concurrent round's
//     (before the stamp the floor also kept the directory, verbatim: "own
//     registered worktree kept: …/001/wt — younger than the 5m0s floor (3s)").
//
// Measured, both before and after the stamp: the row's OTHER ground — the
// linked worktree the WORKER creates for the executor child
// (`gopi worker: child ground → …/004/gopi-<stem>`) — is freed by
// finish-cleanup independently of this fixture, so this file's assertions are
// about the fixture's own directory, never about that one.
//
// WHY THE FIXTURE'S ASSERTION IS A /proc CWD READING AND NOT os.Stat. Measured:
// `git worktree remove` unlinks the path unconditionally — it never consults
// /proc — so `git worktree list` is clean, the path is gone from the directory
// tree and os.Stat(wt) returns ENOENT in BOTH the pinned and the released case.
// The deleted inode survives only in the HOLDER, and /proc/<pid>/cwd renders it
// as "<path> (deleted)". A fixture asserting os.Stat or the git list alone would
// pass with the release call deleted — it would test nothing (Rule 10.4: the
// smallest sample that separates the two shapes is the holder's cwd).
//
// THE SHAPE THAT GIVES THIS FIXTURE TEETH (measured, and the first green run
// got it WRONG): the round's cwd must be inside a directory OF the tree the
// round removes. With the profile root outside the worktree, the round was
// measurably standing in the worktree's SESSION dir while the WORKTREE was
// removed — the removal was real but the cwd was outside it, so a DELETED
// release call still passed (measured: the first red attempt PASSED). So the
// fixture places GOPI_HOME INSIDE the worktree: <wt>/.gopi/sessions/<taskID> is
// then a directory of the removed tree, and the readings are
// "wt → wt/.gopi/sessions/<id> → wt/.gopi/sessions/<id> (deleted) → /". The
// profile dir is committed to the worktree's .gitignore in the same fixture —
// otherwise "?? .gopi/" makes the tree dirty and finish-cleanup refuses the
// removal (also measured).
//
// THE RED PROOF (quoted in devdoc/cwd-release-composition.md): neuter BOTH
// release calls of the round's close-out — cmd/agent/cli_tasks_worker.go (the
// round's own) and cmd/agent/cli_tasks.go's taskReclaimOwnDirectories (the
// wrapper tail the round reaches through runTaskRunOnce) — keeping each file's
// import referenced; rebuild bin/gopi; re-run this test. It must FAIL: the
// worker never reaches the release destination and sits in the deleted
// directory (measured: 234 pinned readings). Neutering the worker-side call
// ALONE passes, because the wrapper tail's call also releases — recorded in the
// devdoc's Not Done.
//
// RULE 22: every directory this test creates is a t.TempDir() under the suite's
// pinned scratch root, whose device is MEASURED off / (crcTempRootOffRootFS)
// rather than assumed, and the child gopi's own temp root is pinned to the same
// measured-clean place instead of inheriting the ambient default.
//
// RULE 27: this fixture spawns one worker for one round and polls procfs. It
// generates no load and is not classified heavy.

import (
	"bytes"
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreerelease"
)

// The CLI spellings this fixture hands the REAL binary. They are argv words of
// a cross-process command (the flag parse table reads them as literals), so the
// only source of truth is the binary itself — named here once so this file has
// one spelling, and asserted by USE (a renamed flag fails this test loudly, it
// cannot drift silently).
const (
	crcFlagPayload = "--payload"
	crcFlagTaskID  = "--task-id"
	crcFlagRepo    = "--repo"
)

const (
	// crcBranch is the fixture worktree's branch — the deliverable's identity,
	// asserted to survive the release (the release frees the DIRECTORY; the
	// branch is the merger's input and is never deleted).
	crcBranch = "feat/cwd-release-composition"
	// crcTaskID is the row the worker claims. Spelled here and passed to
	// `task submit` so the fixture knows the id it submitted (TaskSubmit prints
	// a message rather than returning the id).
	crcTaskID = "4767ee08-699f-5a2c-8125-081e1ff46c80"
	// crcDeliverableFile / crcBaseFile are the fixture's two commits: the
	// base on master, the deliverable on the branch checked out in the worktree.
	crcBaseFile        = "base.txt"
	crcDeliverableFile = "deliverable.txt"
	// crcIgnoreFile / crcIgnoredDirs keep the fixture's profile root out of
	// git's view of the worktree. The profile (GOPI_HOME) must live INSIDE the
	// worktree for the composition to exist at all, and the tree must still be
	// CLEAN for the round to remove it — an ignored profile is both (measured:
	// without this, finish-cleanup refused with "worktree dirty … ?? .gopi/").
	// The paths are rendered from config's own constants, never spelled here.
	crcIgnoreFile = ".gitignore"
	// crcPrompt is the mock LLM's whole script: a plain text answer, so the
	// round runs to completion with no real endpoint and no tool round.
	crcPrompt = "do the deliverable work — done"

	// crcWorkerHandle is the argv word that selects the ROOT worker face (the
	// verb), and crcIdleExits/crcIdleSec are the loop's own knobs. The exits
	// knob is "0" = NEVER self-exit on an idle queue: the daemon face is what
	// makes the post-round cwd observable AT REST. With `--once` the process
	// exits <100ms after the round, so a fixture reading its /proc entry races
	// the exit; a worker that stays alive holds the state under test still.
	crcIdleExits = "0"
	crcIdleSec   = "1"

	// crcScratchGCThrottleMin disables the resident scratch sweep for the life
	// of this fixture (the daemon E2E's own convention): an E2E must never
	// sweep real state.
	crcScratchGCThrottleMin = "100000"

	// crcPollInterval / crcPollMax bound the observation loop (crcPollMax ×
	// crcPollInterval = the window). The window has to cover a real round: the
	// sibling recycle E2E finishes one in ~4s on this host.
	crcPollInterval = 5 * time.Millisecond
	crcPollMax      = 4000

	// crcDeletedSuffix is the /proc rendering this test reads: an unlinked but
	// held cwd is reported as "<path> (deleted)". It carries the same spelling
	// the unit rig matches and the production procfs readers own
	// (internal/tools' procDeletedSuffix) — it is the kernel's marker, not a
	// value we choose.
	crcDeletedSuffix = " (deleted)"
)

// crcStream is a mutex-guarded sink for a LONG-LIVED child's stdout/stderr.
// exec.Cmd copies from the pipe on its own goroutine, so the test's reader and
// the copier race by construction without the lock.
type crcStream struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *crcStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *crcStream) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// crcWorker is the running worker under test plus the state the fixture reads
// back from it.
type crcWorker struct {
	mp     *testutil.MockProcess
	pid    int
	stdout *crcStream
	stderr *crcStream
}

// crcStartWorker spawns the REAL binary as a `gopi worker` whose cwd is dir —
// the linked worktree — and returns it running. The process is reaped by the
// shared mock-process reaper (its whole group), so a failing test never leaves
// a worker behind.
func crcStartWorker(t *testing.T, bin string, env []string, dir string) *crcWorker {
	t.Helper()
	cmd := exec.Command(bin, CliVerbWorker)
	cmd.Dir = dir
	cmd.Env = env
	out, errb := &crcStream{}, &crcStream{}
	cmd.Stdout, cmd.Stderr = out, errb
	mp := testutil.StartMockProcessCmd(t, cmd)
	w := &crcWorker{mp: mp, pid: mp.Pid(), stdout: out, stderr: errb}
	if w.pid <= 0 {
		t.Fatalf("worker did not start (pid %d)", w.pid)
	}
	return w
}

// crcObserve is the measurement: while the worker lives, read its cwd from
// procfs every crcPollInterval and record three facts — whether a reading sat
// inside the session container under the worktree (the composition's premise),
// whether a reading sat inside a directory that NO LONGER EXISTS (the pinned
// shape: procfs renders an unlinked cwd with crcDeletedSuffix), and whether the
// process was seen AT the release destination. No production output is
// consulted: the process's own cwd is the ground truth, so the fixture never
// depends on a diagnostic string (Rules 1b/15).
type crcObservation struct {
	// readings are the distinct cwds seen while the process was alive, in
	// order — the fixture's raw evidence, printed on failure.
	readings []string
	// pinned holds the readings taken inside a worktree that no longer exists.
	pinned []string
	// sessionInside records that the process was observed standing inside the
	// session container under the worktree — the composition's premise (the
	// round runs there) captured as a positive fact, never assumed.
	sessionInside bool
	// released records that the process was observed standing at the release
	// destination (worktreerelease.ProcessRootPath) — the positive half of the
	// composition.
	released bool
	// exited is true when the process was gone before the window closed.
	exited bool
}

func (o crcObservation) String() string {
	return "readings=" + strings.Join(o.readings, " → ") +
		"; session-inside=" + strconv.FormatBool(o.sessionInside) +
		"; pinned=" + strconv.Itoa(len(o.pinned)) +
		"; released=" + strconv.FormatBool(o.released) +
		"; exited=" + strconv.FormatBool(o.exited)
}

// crcOriginGoneMarker is the phrase the round prints when its workdir restore
// finds the round's origin gone — the OTHER route that can leave a long-lived
// process inside a removed directory (cmd/agent/task_session_cwd.go). It is the
// fragment common to that message, so the fixture tracks the behaviour without
// copying a whole sentence.
const crcOriginGoneMarker = "is gone"

// crcInsideWorktree reports whether a cwd reading names the worktree (or a
// directory under it). The "(deleted)" rendering of an unlinked cwd has the
// worktree path as its prefix, so the prefix test catches the pinned shape too.
func crcInsideWorktree(cwd, wt string) bool {
	return cwd == wt ||
		strings.HasPrefix(cwd, wt+string(os.PathSeparator)) ||
		strings.HasPrefix(cwd, wt+crcDeletedSuffix)
}

func crcObserve(w *crcWorker, wt, sessDir string) crcObservation {
	var o crcObservation
	seen := map[string]bool{}
	for i := 0; i < crcPollMax; i++ {
		cwd, ok := cwdReleaseHolderCwd(w.pid) // the package's ONE procfs cwd reader (cwd_release_rig_test.go)
		if !ok {
			o.exited = true
			return o
		}
		if !seen[cwd] {
			seen[cwd] = true
			o.readings = append(o.readings, cwd)
		}
		if cwd == worktreerelease.ProcessRootPath {
			o.released = true
		}
		if cwd == sessDir {
			o.sessionInside = true
		}
		// THE PIN: a reading inside a worktree that no longer exists. Only
		// observable while the release has NOT rescued the process; the window
		// between the removal and the release is short by design, so a fixture
		// racing it reports "not observed", never a false pass.
		if crcInsideWorktree(cwd, wt) {
			if _, err := os.Stat(wt); err != nil {
				o.pinned = append(o.pinned, cwd)
			}
		}
		if o.sessionInside && o.released {
			return o // both halves of the composition are witnessed
		}
		time.Sleep(crcPollInterval) //stylegate:allow-sleep this IS the measurement loop: procfs has no readiness notification, so the holder's cwd is re-read on a bounded cadence (never an unbounded wait)
	}
	return o
}

// crcWorktreeGone reports whether the worktree is gone in the two senses the
// release contract cares about: git no longer registers it, and the path no
// longer exists.
func crcWorktreeGone(t *testing.T, repo, branch, wt string) (bool, string) {
	t.Helper()
	listed := branchListedInWorktrees(t, repo, branch)
	_, err := os.Stat(wt)
	gone := !listed && os.IsNotExist(err)
	return gone, "git-listed=" + strconv.FormatBool(listed) + " stat-err=" + crcErrString(err)
}

func crcErrString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

// crcFixture builds the git world: a repo whose master holds a base commit, and
// a LINKED worktree of that repo checked out on crcBranch with the deliverable
// committed — the shape a task run's Rule-6 worktree has, and the directory the
// worker under test is launched inside.
func crcFixture(t *testing.T, repo, wt string) {
	t.Helper()
	root := filepath.Dir(repo)
	releaseGit(t, root, "init", "-b", "master", repo)
	releaseGit(t, repo, "config", "user.email", "rig@rig")
	releaseGit(t, repo, "config", "user.name", "rig")
	if err := os.WriteFile(filepath.Join(repo, crcBaseFile), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, repo, "add", crcBaseFile)
	releaseGit(t, repo, "commit", "-m", "base")
	releaseGit(t, repo, "worktree", "add", "-b", crcBranch, wt, "master")
	// The profile root this fixture puts inside the worktree must be INVISIBLE
	// to git, or the worktree is dirty and the round's finish-cleanup refuses to
	// remove it (fail-soft, by design): "worktree dirty (live bond protected) —
	// carries ONLY UNTRACKED files …: ?? .gopi/". The ignore file is committed
	// BEFORE the profile is created (the fixture's env sets GOPI_HOME to the
	// worktree), so the tree is clean at removal time.
	ignore := config.ProfileDirName + string(os.PathSeparator) + "\n"
	if err := os.WriteFile(filepath.Join(wt, crcIgnoreFile), []byte(ignore), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, wt, "add", crcIgnoreFile)
	releaseGit(t, wt, "commit", "-m", "ignore the fixture profile root")
	if !branchListedInWorktrees(t, repo, crcBranch) {
		t.Fatalf("fixture: %s must be checked out in the linked worktree %s", crcBranch, wt)
	}
	if err := os.WriteFile(filepath.Join(wt, crcDeliverableFile), []byte("the delivery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, wt, "add", crcDeliverableFile)
	releaseGit(t, wt, "commit", "-m", "the deliverable")
}

// crcStampGround records the fixture worktree as the ROW's ground through the
// PRODUCTION backfill seam (tools.TaskDeliverableWorktreeBashForRow — the call
// the executor's bash round makes when a round creates a directory), then reads
// the row back through the production reader. Writing the row's deliverable here
// would let this fixture drift from the real writer's schema; the read-back is
// the guard that the stamp landed (Rule 15).
//
// The row id is passed EXPLICITLY (the ForRow entry point) rather than through
// GOPI_TASK_WRAPPER_ID: this process is not the row's executor child. Because
// the seam writes through the PROCESS's ledger, the ledger the run is sandboxed
// to must be this process's too for the duration of the call (t.Setenv — the
// same discipline the sibling recycle E2E documents for its own stamping).
func crcStampGround(t *testing.T, env []string, wt string) {
	t.Helper()
	dbPath := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, memoryDBEnv+"="); ok {
			dbPath = v
			break
		}
	}
	if dbPath == "" {
		t.Fatalf("the fixture env carries no %s — the stamp has no ledger to write to", memoryDBEnv)
	}
	t.Setenv(memoryDBEnv, dbPath)
	tools.TaskDeliverableWorktreeBashForRow(crcTaskID, wt, "git worktree add "+wt+" -b "+crcBranch+" master")
	if gotWT, gotBranch := tools.TaskDeliverableWorktreeFor(crcTaskID); gotWT != wt || gotBranch != crcBranch {
		t.Fatalf("the production backfill seam must stamp the row's ground: worktree=%q branch=%q (want %q/%q)", gotWT, gotBranch, wt, crcBranch)
	}
}

// NOTE ON THE MEASUREMENT (why there is no os.Stat or "inode still busy" probe
// beside the /proc reading): measured on both tmpfs and xfs, `git worktree
// remove` unlinks the path unconditionally, so the path is gone, `git worktree
// list` is clean and os.Stat returns ENOENT in BOTH the pinned and the released
// case. The deleted inode survives in exactly one observable place — the
// holder's /proc/<pid>/cwd. A second probe that reads the same filesystem state
// would add no discrimination while making the fixture look stronger than it
// is, so the /proc reading is the measurement and this file says so (Rule 10.4).

// crcTempRootOffRootFS states Rule 22's criterion as a measurement, not a
// spelling: the fixture's temp root must live on a different device than /.
// The child gopi's own temp root is then pinned to that same measured place.
func crcTempRootOffRootFS(t *testing.T, root string) {
	t.Helper()
	rootDev := crcDeviceOf(t, root)
	slashDev := crcDeviceOf(t, string(os.PathSeparator))
	if rootDev == slashDev {
		t.Fatalf("Rule 22: the fixture's temp root %s is on the / partition (device %d) — %s must resolve off it",
			root, rootDev, testutil.TempDirEnvName)
	}
	t.Logf("Rule 22 measured: fixture temp root %s device=%d, / device=%d", root, rootDev, slashDev)
}

func crcDeviceOf(t *testing.T, path string) uint64 {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return uint64(st.Dev)
}

// crcEnv is the worker's environment, composed for THIS fixture rather than
// inherited from the sibling recycle rig — because one value decides whether
// this test has teeth: the profile root (GOPI_HOME) is placed INSIDE the linked
// worktree.
//
// WHY THAT ONE VALUE IS THE WHOLE FIXTURE (measured, first green run): the
// round's session container is <profile>/sessions/<taskID>, and the round
// STANDS in that container while it runs. With a profile outside the worktree
// the process was measurably NOT in the tree when the tree was removed — the
// composition degenerated into the session-dir release, a shape that would pass
// with the release call DELETED (nothing removed the directory it stood in).
// With the profile inside the worktree, the directory the round stands in at
// removal time IS a directory of the removed tree — the exact production
// composition, and the shape a neutered release cannot survive.
//
// The repo/role table is the sibling E2E's (settings.json: role omitted = the
// real zbook shape, an empty repos table for the sweep, no peers), the LLM is
// the shared scripted mock, and the ledger is a throwaway file. Returns the env
// and the session container the fixture must be able to see (Rule 15: the
// fixture states its premise instead of assuming it).
func crcEnv(t *testing.T, repo, wt, tmpRoot string) ([]string, string) {
	t.Helper()
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: crcPrompt})
	t.Cleanup(mock.Close)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"sync_peers":[],"repos":{"`+repo+`":{}}}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	env := testutil.MockEnv(t, wt, mock.URL())
	env = append(env,
		memoryDBEnv+"="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SETTINGS="+settingsPath,
		"GOPI_SKIP_CLAIM_BROADCAST=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		workerIdleExitsEnv+"="+crcIdleExits,
		workerIdleIntervalEnv+"="+crcIdleSec,
		scratch.GCIntervalEnvName+"="+crcScratchGCThrottleMin,
		// The child's temp root, pinned explicitly rather than inherited: the
		// ambient default on this host is a directory on /, and Rule 22 says the
		// explicit layer wins over the ambient one.
		testutil.TempDirEnvName+"="+tmpRoot,
		scratch.TmpDirEnvName+"="+tmpRoot,
	)
	// The session container the round must stand in — the worktree's own child,
	// because the profile root above is the worktree. Derived through the SAME
	// shape the round uses (the sessions dir name is config's constant, never a
	// literal here), so a layout change fails this fixture loudly.
	sessDir := filepath.Join(wt, config.ProfileDirName, config.SessionsDirName, crcTaskID)
	return env, sessDir
}

// TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves is the composition proof:
// the real worker binary, launched INSIDE a fixture's linked worktree, runs one
// round that frees that very directory — and the process must not be left
// holding it.
func TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bin := testutil.GopiBin(t)
	root := t.TempDir()
	crcTempRootOffRootFS(t, root)
	tmpRoot := t.TempDir()
	crcTempRootOffRootFS(t, tmpRoot)

	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	crcFixture(t, repo, wt)

	env, sessDir := crcEnv(t, repo, wt, tmpRoot)
	if _, stderr, code := runGopiMapped(t, []string{
		CliVerbTask, taskverbs.Submit, "cwd release composition",
		crcFlagPayload, crcPrompt, crcFlagTaskID, crcTaskID, crcFlagRepo, repo,
	}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// THE ROW'S RECORDED GROUND IS THIS FIXTURE'S WORKTREE (see the file header).
	// Without it the round's close-out is refused by the guest arm — a process
	// standing in a linked worktree of the repository whose row it reconciles —
	// and the composition is never exercised. Stamped through the production
	// seam, never by writing the row's field here.
	crcStampGround(t, env, wt)
	// THE FIXTURE'S PREMISE (Rule 15: stated, never assumed): the session
	// container the round stands in while it works must be a child of the very
	// worktree the round removes. Without that, the composition degenerates
	// into "the round releases the session dir" and a DELETED release call
	// would still pass (measured on the first green run of this fixture).
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Fatalf("fixture premise: the session container %s must NOT exist before the round (stat err=%v)", sessDir, err)
	}

	w := crcStartWorker(t, bin, env, wt)
	obs := crcObserve(w, wt, sessDir)
	out, errOut := w.stdout.String(), w.stderr.String()

	// ① THE FIXTURE EXERCISED THE COMPOSITION (evidence, not a verdict): the
	// worktree contains the session container the round runs in, so removing the
	// worktree puts the ROUND'S OWN cwd inside a directory that no longer
	// exists — the epic's own shape, observed here as the pinned reading. While
	// the round is unwinding, standing there is exactly the state the release
	// exists to end; the defect is staying there (②).
	if len(obs.pinned) > 0 {
		t.Logf("pin observed: %d reading(s) inside a REMOVED directory, e.g. %q — the composition is live (the release is what ends this state)",
			len(obs.pinned), obs.pinned[0])
	}
	// ② THE PROCESS MUST LEAVE — the verdict, and the reading a neutered release
	// cannot survive: the worker was seen AT the release destination, and its
	// cwd read back after the round is no longer inside the removed tree. (Read
	// while the process is still alive; if it has exited, the whole tree is
	// released with it and there is nothing left to pin.)
	if !obs.released {
		t.Errorf("worker pid %d was never observed at %s (composition=%s) — the round's cwd release did not run, so the process is left standing in the directory its own close-out removed\nworker stdout:\n%s\nworker stderr:\n%s",
			w.pid, worktreerelease.ProcessRootPath, obs, out, errOut)
	}
	if last, alive := cwdReleaseHolderCwd(w.pid); alive && crcInsideWorktree(last, wt) {
		t.Errorf("worker pid %d still stands inside the REMOVED worktree (%q) after its round — the deleted inode (and every file under it) stays pinned, which is the defect this composition exists to stop (composition=%s)\nworker stdout:\n%s\nworker stderr:\n%s",
			w.pid, last, obs, out, errOut)
	}
	// ③ THE ROUND FREED THIS DIRECTORY — the REMOVAL half of the composition,
	// and the half the guest arm used to refuse instead of reaching. A refusal is
	// not silent: the line naming it (`own worktree release refused` + the guard's
	// own sentence) is in the captured output this failure prints, so a surviving
	// directory is always diagnosable from the failure itself (Rule 15).
	gone, why := crcWorktreeGone(t, repo, crcBranch, wt)
	if !gone {
		t.Errorf("the round did NOT free the worktree (%s) — the removal half of the composition did not run\nworker stdout:\n%s\nworker stderr:\n%s", why, out, errOut)
	}
	// ④ the branch is the deliverable: never deleted by the release.
	if releaseGit(t, repo, "rev-parse", crcBranch) == "" {
		t.Errorf("the fixture branch %s did not survive the round", crcBranch)
	}
	// ⑤ the round ran at all (the claim line is the worker's own record).
	if !strings.Contains(out, crcTaskID) {
		t.Errorf("the worker never reported the row it claimed (%s) — no round ran:\n%s", crcTaskID, out)
	}
	// The round's own workdir restore reports the gone origin on stderr here,
	// BY CONSTRUCTION of this shape (the fixture put the round's origin inside
	// the very worktree the round removes). It is logged, not failed: on that
	// path the release is the ONLY thing that unpins the process, which is
	// precisely the composition under test.
	if strings.Contains(errOut, crcOriginGoneMarker) {
		t.Logf("round origin reported gone (expected for this shape — the release is then the only unpin):\n%s", errOut)
	}
	t.Logf("composition observed: %s", obs)
	t.Logf("worker stdout:\n%s\nworker stderr:\n%s", out, errOut)
}
