package main

// task_session_cwd_e2e_test.go — the END-TO-END half of the task-session-cwd
// epic: it runs the REAL binary (worker loop → real claim → real executor
// child) and reads the run's own records to prove where the process stood.
//
// What only an e2e run can prove (a unit test of the chdir seam cannot):
//
//   - the WORKER's round really entered the claimed task's session directory
//     (observed from the child, which is the only party that can see the
//     wrapper it was spawned by);
//   - the CHILD did NOT inherit that move — it stands in the working context
//     the run handed it (the row's own ground, which the spawn seam records in
//     the run's log), which is what keeps GOPI.md loading, the search roots and
//     the repo stamp honest.
//
// The child reports through a bash tool call whose OUTPUT the mock LLM
// round-trips back to the agent; the transcript is the child's session JSONL,
// which is an artifact OF THE TASK (so this test also demonstrates the epic's
// premise: the task id leads to its own directory).

import (
	"encoding/json"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cwdProbeE2ERepo builds the minimal git work tree a task row needs as its
// repo target (the submit path verifies it before the row is written).
//
// IT ENDS WITH A COMMIT, and that is load-bearing, not tidiness: the run's
// ground creation branches FROM HEAD (`git worktree add <dir> -b task/<id>`),
// so a repository whose HEAD is unborn cannot be branched in at all — the
// product then refuses the run (correctly: "fatal: Not a valid object name:
// 'HEAD'"), and a walk over such a fixture measures its own FIXTURE while
// looking like a product failure. The shape pin below makes that happen here.
func cwdProbeE2ERepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"checkout", "-b", "master"},
	} {
		cwdProbeGit(t, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// The commit that makes HEAD resolvable. The row's run branches its ground
	// off HEAD, so this is the fixture's contract with the product, not an
	// optional tidy-up.
	for _, args := range [][]string{{"add", "a.txt"}, {"commit", "-q", "-m", "fixture"}} {
		cwdProbeGit(t, repo, args...)
	}
	cwdProbeAssertResolvableHEAD(t, repo)
	return repo
}

// cwdProbeGit runs one git command in dir and fails the test on error.
func cwdProbeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// cwdProbeAssertResolvableHEAD is the SHAPE PIN: it fails HERE, at the
// fixture, when the repo it just built has no resolvable HEAD.
//
// WHY IT EXISTS (Rule 5's regression guard for this walk): the fixture's
// original form ran `git init` with NO commit, so HEAD was unborn. The product
// did the only correct thing — `git worktree add <ground> -b task/<id>` cannot
// branch off an unborn HEAD ("fatal: Not a valid object name: 'HEAD'"), so the
// run refused to start — and the walk then failed three assertions LATER, at
// the output-log and transcript reads, where the message names the symptom
// (`no session JSONL`) and nothing points at the fixture. Measured on master
// 38ea30db: the walk measured its own scaffolding. The pin moves that verdict
// back to the line that built the repository, and names both halves of the fix.
//
// The probe asks the SAME question the product asks — a plain `rev-parse
// --verify HEAD`, whose nonzero status IS "unborn" (a repository with a
// commit, any commit, resolves) — rather than a `git status`/`git log`
// spelling whose output would have to be parsed (Rule 13: the product's
// question is the fixture's question).
//
// The refusal is deliberately NOT an assert on locale-dependent text: the
// child (and git) inherit LANG from the host, so the "unborn HEAD" wording is
// localized, and internal/tools/bash.go intentionally does not override it
// (Rule 1's spirit — a message is not a contract; the exit status is).
func cwdProbeAssertResolvableHEAD(t *testing.T, repo string) {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture repo %s has NO resolvable HEAD (git rev-parse --verify HEAD: %v; output: %s). "+
			"The run branches its ground off HEAD (`git worktree add <dir> -b task/<id>`), so an unborn HEAD makes the "+
			"product refuse the run and this walk measure its own FIXTURE. Cause: cwdProbeE2ERepo committed nothing. "+
			"Remedy: keep the `git add` + `git commit` pair in the fixture (and this pin) — never delete them to make a red go away.",
			repo, err, out)
	}
}

// TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild is the epic's
// acceptance walk on the real binary.
func TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	// The child's first round runs `pwd`; every later round is a plain text
	// answer (the mock repeats its last script entry), so the run ends.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_cwd", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "pwd"}}},
		testutil.Resp{Text: "cwd probe done"},
	)
	t.Cleanup(mock.Close)

	repo := cwdProbeE2ERepo(t)
	const taskID = "7e0da3d3-939d-42f8-a192-bce48feb4fc9"
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		"GOPI_AUTO_TASK_REGISTER=1",
		config.SkipClaimBroadcastEnv+"=1",
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"session cwd e2e", "--payload", "run pwd", "--task-id", taskID, "--repo", repo},
		"", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// The worker is launched from the REPO — the working context the executor
	// child must keep.
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	if code != 0 {
		t.Fatalf("worker --once must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// The round's own record: the worker announced the move into the claimed
	// task's session dir, and — because this walk drives the deploy seam
	// (`worker --once`, the drain-once face) — the LAUNCHER's stream carries
	// that line, so the whole output of a one-shot worker leads to the run's
	// directory (the epic's "the id is the way in").
	//
	// WHERE THE LINE LIVES NOW (epic child-stream-task-output): the worker
	// holds the stream it was launched with, and only the EXECUTOR CHILD's
	// stdout/stderr are pointed at the row's output.log by the spawn seam. So
	// the round's own record — the move line, the claim — is on the LAUNCHER's
	// stream (asserted here), while the task's own text is in output.log
	// (asserted below).
	sessionDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID)
	moveLine := taskWorkdirMovePrefix + sessionDir
	if !strings.Contains(stdout+stderr, moveLine) {
		t.Fatalf("the worker round must report %q (the move into its task's session dir) on the launcher's stream; got:\nstdout:\n%s\nstderr:\n%s",
			strings.TrimSpace(moveLine), stdout, stderr)
	}
	// The TASK's output log lives in the SAME directory — the epic's "one
	// task, one directory" on the real path, and now the destination of the
	// EXECUTOR CHILD's own stream (the spawn seam), whatever spawned the run.
	logPath := filepath.Join(sessionDir, tools.WorkerLogFileName)
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the run's output log must exist at %s: %v", logPath, err)
	}
	for _, want := range []string{
		// The spawn seam's own record of the run: proof the file is the
		// run's account (written by the seam), not the launcher's stream
		// copied around. It names the task, so a reader knows whose it is.
		"(task " + taskID + ":",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the task output log must contain %q; got:\n%s", want, body)
		}
	}

	// The CHILD's session JSONL — an artifact of the task, in the task's own
	// directory — carries the bash tool's `pwd` RESULT: this is the executed
	// proof that the executor ran in the directory the run resolved for it, not
	// in the session dir.
	//
	// The assertion reads the tool_result PAIRED WITH the pwd tool_use (by
	// tool_use_id), never "does the transcript mention the repo anywhere":
	// the briefing itself carries the repo path (the task was submitted with
	// --repo), so a whole-transcript substring check passes even when the
	// child stands in the session dir — a green-blind assertion. Measured:
	// disabling the child's Cmd.Dir left a substring check PASSING.
	transcript := readFirstSessionJSONL(t, sessionDir)
	childCwd := cwdProbeResult(t, transcript)
	if childCwd == "" {
		t.Fatalf("the child's session JSONL must record the pwd tool_result; got:\n%s", transcript)
	}
	got := strings.TrimSpace(childCwd)
	// THE CONTRACT, and it is these two assertions because they fail for
	// different reasons — the epic's claim is unchanged (the worker's round
	// moved into the task's session dir; the CHILD did not inherit it, and its
	// pwd result is the working context the run handed it):
	//
	//  ① NOT the wrapper's container. A child standing in the SESSION DIR (or
	//     anywhere under it) inherited the wrapper's move — the defect this
	//     test exists for, and the one assertion that must never be relaxed.
	//  ② THE DIRECTORY THE RUN RESOLVED FOR IT. Since epic
	//     worker-creates-child-ground (2026-09-22) a repo-aware row is executed
	//     in its GROUND — a linked worktree of the row's repo, minted by the
	//     spawn seam before the child starts — so a truthful pwd result is the
	//     ground, NOT the row's main checkout. Pinning the main checkout here
	//     asserted the pre-ground behaviour and made this walk red against a
	//     CORRECT product (measured on master 595bd8bd: the pwd result was
	//     <worktree-root>/gopi-tsessioncwde2e). The reference comes from the
	//     RUN'S OWN RECORD of the decision, never from a path this test
	//     re-derives: the seam writes `executor cwd → <dir> (task <id>:
	//     <source>)` into the run's log (epic child-stream-task-output) for
	//     every run, and the marker is taken from the product's own format
	//     (taskCwdResolvedFmt — the shape lives in the const, tests included,
	//     Rule 13). An absent record is a FAILURE, never a fallback to a
	//     guessed path (Rule 15): a run that no longer says where it put its
	//     child is one this walk cannot judge, and quietly substituting the
	//     repo would restore exactly the stale expectation this replaced.
	//
	// WHY THE LEDGER/REPO STATE IS NOT THE INSTRUMENT: the run's own close-out
	// may already have RECLAIMED the ground (measured on master 38ea30db:
	// `git worktree list` then named only the main checkout while the child's
	// pwd result was a worktree path) — the claim is about where the child
	// STOOD, and the seam's record is what survives it.
	resolved := cwdProbeResolvedChildDir(t, string(body))
	if cwdProbeWithin(got, sessionDir) {
		t.Fatalf("the executor child ran `pwd` in the SESSION DIR (%s) — it inherited the wrapper's own move; the run resolved %s for it\n%s",
			got, resolved, transcript)
	}
	if got != resolved && !cwdProbeWithin(got, resolved) {
		t.Fatalf("the executor child must have run `pwd` inside the directory the run resolved for it (%s); the pwd result was %q\n%s",
			resolved, got, transcript)
	}
}

// cwdProbeResolvedChildDir returns the directory the spawn seam decided the
// executor child must stand in, as the seam recorded it in the run's own log.
// An absent record is a FAILURE, never a fallback to a guessed path (Rule 15).
//
// The marker is TAKEN FROM THE PRODUCT'S OWN FORMAT — `taskCwdResolvedFmt`, the
// one rendering of that record (Rule 13: the shape lives in the const, never
// re-typed at a call site, tests included).
func cwdProbeResolvedChildDir(t *testing.T, runLog string) string {
	t.Helper()
	marker := strings.SplitN(taskCwdResolvedFmt, "%s", 2)[0]
	idx := strings.Index(runLog, marker)
	if idx < 0 {
		t.Fatalf("the run must record the child's resolved cwd (%q) in its own log; got:\n%s", marker, runLog)
	}
	rest := runLog[idx+len(marker):]
	if end := strings.Index(rest, " (task "); end >= 0 {
		rest = rest[:end]
	}
	dir := strings.TrimSpace(rest)
	if dir == "" {
		t.Fatalf("the run's cwd record names no directory; got:\n%s", runLog)
	}
	return dir
}

// cwdProbeWithin reports whether dir IS ancestor or lies UNDER it (path-boundary
// safe: /a/bc is not under /a/b).
func cwdProbeWithin(dir, ancestor string) bool {
	dir, ancestor = filepath.Clean(dir), filepath.Clean(ancestor)
	return dir == ancestor || strings.HasPrefix(dir, ancestor+string(filepath.Separator))
}

// TestFrontRunConsoleOutputLandsInItsSessionDir is the epic's FRONT-RUN
// acceptance on the real binary: `gopi "prompt"` runs in the operator's
// working context (its cwd cannot move — the run MEANS that directory), so the
// epic requires its console output to be COPIED into the run's own session
// directory instead. The test drives a real front run with a mock LLM and
// reads the run's directory, which is the whole claim: the id leads to
// everything the run produced.
func TestFrontRunConsoleOutputLandsInItsSessionDir(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "front run answer marker"})
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, "GOPI_CRASH_DUMP_SWEEP=off")
	// stdout is captured by RunGopi (a pipe, never a TTY) — exactly the shape
	// the seam attaches for, and exactly how the launcher runs a front agent.
	stdout, stderr, code := runGopiMapped(t, []string{"front run probe"}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("a front run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "front run answer marker") {
		t.Fatalf("the operator's stdout must still carry the run's answer (the console log is a COPY, never a redirect); got:\n%s", stdout)
	}

	// The run's own directory — found from the sessions root alone, the way an
	// operator finds it — must hold both the session JSONL and console.log.
	sessionsRoot := filepath.Join(gopiHome, ".gopi", config.SessionsDirName)
	dirs, err := os.ReadDir(sessionsRoot)
	if err != nil {
		t.Fatalf("read sessions root %s: %v", sessionsRoot, err)
	}
	var consolePath string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(sessionsRoot, d.Name(), consoleLogFileName)); statErr == nil {
			consolePath = filepath.Join(sessionsRoot, d.Name(), consoleLogFileName)
			break
		}
	}
	if consolePath == "" {
		t.Fatalf("no %s in any session dir under %s — a front run's console output must be recorded beside its session (stderr was:\n%s)", consoleLogFileName, sessionsRoot, stderr)
	}
	body, err := os.ReadFile(consolePath)
	if err != nil {
		t.Fatalf("read %s: %v", consolePath, err)
	}
	if !strings.Contains(string(body), "front run answer marker") {
		t.Errorf("the run's console log must hold what the run printed; %s held:\n%s", consolePath, body)
	}
	// The log is the run's, and self-locating: it names its own path on stderr
	// (the operator can find it without guessing).
	if !strings.Contains(stderr, consolePath) {
		t.Errorf("the attach line must name %s so the operator can find it; stderr was:\n%s", consolePath, stderr)
	}
}

// assertGopiBinIsFresh — HOW the driven binary is shown to belong to the tree
// under test — lives in cmd/agent/gopibin_identity_guard_test.go. Moved out of this
// file because it is not about the child-cwd contract: it decides a fact about
// a BUILT ARTIFACT by three criteria in order (embedded build identity, source
// content digest, and mtime as the last resort), it is called by every e2e
// guard in this package, and it owns its own unit tests
// (gopibin_identity_test.go). What stays HERE is the walk it consults last: the
// newest production source under the module's cmd/ and internal/ trees.
//
// The failure mode the guard exists for was measured while writing this test:
// the source edit that disables the child's Cmd.Dir was made, the test ran
// green, and only after rebuilding bin/gopi did the same test fail as
// designed. A verification whose subject is a prebuilt artifact must prove the
// artifact reflects the tree, or every green is unfalsifiable (the red-proof of
// the child-cwd contract walked straight into it).
// It compares the binary's mtime against the newest .go file under the
// module's cmd/ and internal/ trees: the e2e drives the WHOLE binary, so any
// newer source file means the binary may not contain the behaviour under
// test. A missing timestamp is reported, never assumed fresh (Rule 15).

// cwdProbeNewestProductionSource returns the newest PRODUCTION .go file under
// root's cmd/ and internal/ trees, with its path. "Production" excludes two
// classes, for the one reason that the guard's red verdict must mean "this
// binary predates a production change":
//
//   - every _test.go file (the binary is not built from tests), and
//   - the whole of the test-only trees in cwdProbeBinaryScopeSkipped — a
//     directory skip, because a test-only helper is a plain .go file the
//     _test.go filter cannot see.
//
// A walk error is returned, never swallowed (Rule 15b) — but an unreadable
// ENTRY cannot make a stale binary look fresh, so a per-entry error is skipped
// and the comparison still runs against every readable file.
func cwdProbeNewestProductionSource(root string) (time.Time, string, error) {
	sources, err := cwdProbeProductionSources(root)
	if err != nil {
		return time.Time{}, "", err
	}
	if len(sources) == 0 {
		return time.Time{}, "", nil
	}
	return sources[0].modTime, sources[0].path, nil
}

// cwdProbeProductionSources returns every production .go file the freshness
// comparison reads, newest FIRST: the same two trees and the same two filters
// the newest-source answer has always used, returned as a list so a refusal can
// show how wide the writer's footprint is instead of naming one path. The
// stamped modTime is the scan's reading, and each entry carries a reader for what
// its mtime is NOW — the pair that lets the refusal (below) show a file that was
// written WHILE the run was comparing.
func cwdProbeProductionSources(root string) ([]cwdProbeSource, error) {
	var sources []cwdProbeSource
	for _, sub := range cwdProbeProductionSourceDirs {
		dir := filepath.Join(root, sub)
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && cwdProbeBinaryScopeSkipped(root, path) {
				return filepath.SkipDir
			}
			if err != nil || d.IsDir() || !strings.HasSuffix(path, cwdProbeSourceSuffix) || strings.HasSuffix(path, cwdProbeTestSourceSuffix) {
				return nil //stylegate:ignore the same reading as the newest-source walk: an unreadable entry cannot make a stale binary look fresh, and every READABLE file is reported
			}
			fi, statErr := d.Info()
			if statErr != nil {
				return nil //stylegate:ignore an unreadable entry cannot make a stale binary look fresh; the comparison below still runs against every readable file
			}
			read := path
			sources = append(sources, cwdProbeSource{
				path:    path,
				modTime: fi.ModTime(),
				// The stamp is the pair (mtime, size) the evidence arm identifies a
				// source by between its two readings — taken HERE, from the same
				// stat every other term of the comparison comes from, so the reading
				// and the verdict describe one state of the tree.
				stamp: cwdProbeFreshnessStampOf(fi),
				now: func() (time.Time, bool) {
					nowInfo, nowErr := os.Stat(read)
					if nowErr != nil {
						return time.Time{}, false
					}
					return nowInfo.ModTime(), true
				},
			})
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, walkErr)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].modTime.After(sources[j].modTime) })
	return sources, nil
}

// cwdProbeBinaryScopeSkippedDir are the source trees the freshness walk must
// NOT read: the binary is built from production sources only, and these hold
// none — the whole tree is test scaffolding plus test-only helpers (proven
// mechanically: `go list -deps ./cmd/agent/` links ZERO package under
// internal/testutil; nothing under internal/standards is imported by it either).
//
// The guard's whole value is that a red verdict means "this binary predates a
// PRODUCTION change". Counting a test-only tree makes the binary look stale the
// instant an unrelated TEST helper is touched — measured on master 2026-09-16:
// internal/testutil/legacy_residue.go was edited while the operator's bin/gopi
// was being built, so TestBareRunKeepsAllItsArtifactsInOneSessionDir refused to
// run ("/home/user/work/GoPi/bin/gopi (mtime …) is OLDER than …/legacy_residue.go")
// even though nothing the binary compiles had changed. That is the false alarm
// the comment above already warns about, arriving through a directory the
// _test.go skip cannot reach.
var cwdProbeBinaryScopeSkippedDir = []string{
	filepath.Join("internal", "testutil"),
	filepath.Join("internal", "standards"),
}

// cwdProbeBinaryScopeSkipped reports whether path IS one of the test-only trees
// above (the root-relative comparison keeps it independent of where the
// worktree happens to sit).
func cwdProbeBinaryScopeSkipped(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	for _, skipped := range cwdProbeBinaryScopeSkippedDir {
		if rel == skipped {
			return true
		}
	}
	return false
}

// cwdProbeModuleRoot walks up from the test's cwd to the module root (the
// directory holding go.mod) — the tree the freshness comparison scans.
func cwdProbeModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s — cannot prove the built binary is fresh", dir)
		}
		dir = parent
	}
}

// cwdProbeResult returns the content of the tool_result that answers the
// `pwd` tool_use in transcript ("" when either half is absent). It walks the
// JSONL events in order and pairs by tool_use_id — the same pairing the
// provider API requires, so a result can never be read out of another call's
// answer (the wrappers in the transcript also emit tool events).
func cwdProbeResult(t *testing.T, transcript string) string {
	t.Helper()
	type block struct {
		Type      string                 `json:"type"`
		ID        string                 `json:"id"`
		ToolUseID string                 `json:"tool_use_id"`
		Input     map[string]interface{} `json:"input"`   // tool_use arguments
		Content   string                 `json:"content"` // tool_result payload
	}
	type msg struct {
		Content []block `json:"content"`
	}
	type event struct {
		Type    string `json:"type"`
		Message msg    `json:"message"`
	}
	probeID := ""
	for _, line := range strings.Split(transcript, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		for _, b := range ev.Message.Content {
			// The block type lives on the BLOCK, not on the event: an
			// assistant event carries a tool_use block, a tool_result event
			// carries the answering result block (session JSONL shape).
			if b.Type == "tool_use" && b.Input["command"] == "pwd" {
				probeID = b.ID
			}
		}
		if probeID == "" {
			continue
		}
		for _, b := range ev.Message.Content {
			if b.ToolUseID == probeID {
				// The bash tool wraps its result (a timing/tool_use header
				// line, the exit code, then the command's stdout): the pwd
				// answer is the LAST non-empty line, and reading it there —
				// rather than substring-matching the whole payload — is what
				// makes this assertion measure the child's cwd and nothing
				// else (the briefing and the header both name other paths).
				lines := strings.Split(strings.TrimRight(b.Content, "\n"), "\n")
				for i := len(lines) - 1; i >= 0; i-- {
					if got := strings.TrimSpace(lines[i]); got != "" {
						return got
					}
				}
				return ""
			}
		}
	}
	return ""
}

// readFirstSessionJSONL returns the content of the first run JSONL found in
// dir (the child's session transcript for the run) — the task id alone is
// enough to find it, which is the epic's premise in action.
func readFirstSessionJSONL(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+".jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no session JSONL under %s: %v", dir, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read %s: %v", matches[0], err)
	}
	return string(data)
}
