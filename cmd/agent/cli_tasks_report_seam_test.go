package main

// task_report_seam_test.go — the HARNESS SEAM proof (Rule 11: a tool/seam
// change is only finished when it is exercised end-to-end, not merely
// unit-tested). Where task_report_test.go drives the writer/reader functions
// directly, this file drives the real `gopi task run` wrapper in a sandbox:
//
//	submit a row → `task run` a child whose model writes the report through
//	its bash tool to the path the harness exported → the wrapper's terminal
//	writeback seals it → `task show` renders it.
//
// That is the whole epic in one flow, with no hand-written report anywhere:
// the path comes from taskreport.PathEnv, the bytes come from the model's
// tool call, the bound/record comes from the harness. The binary is built
// from THIS tree (a stale ../bin/gopi would prove nothing about this branch).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// reportSeamBinary returns the gopi binary this seam drives.
//
// It used to build its own copy from THIS TREE into a suite-lifetime directory
// and cache it in this file's own sync.Once. The build was the same `go build
// -o <scratch>/… .` every other file in this package runs, so what is kept is
// the part that is specific to this seam — the loud refusal of a non-executable
// artifact — and the binary itself comes from gopibin_test.go's ONE build slot
// (a second `go build` of the same package is pure duplicated cost, and the
// slot already lives in the suite scratch root, off `/`, outliving each test).
//
// A build failure stays FATAL, not skipped: this is an acceptance test, and a
// silent skip reports a green seam that never ran (the sweep-zombies lesson,
// cli_tasks_sweep_zombies_test.go).
func reportSeamBinary(t *testing.T) string {
	t.Helper()
	bin := gopiTestBinPath(t)
	// A `go build` of a NON-main package writes a non-executable archive;
	// stamping that path would make every RunGopi below fail with
	// "permission denied" — a loud, misleading failure deep inside the
	// seam. Refuse it here, where the cause is obvious (Rule 15).
	if fi, err := os.Stat(bin); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("built %s is not executable (mode %v, err %v) — is this test still in package main?", bin, modeOf(fi), err)
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("abs %s: %v", bin, err)
	}
	// Stamp GOPI_BIN so testutil.RunGopi (and anything below it) cannot
	// re-resolve to another tree's binary.
	t.Setenv("GOPI_BIN", abs)
	return abs
}

// TestTaskRunSeamWritesReportAndShowRendersIt is the E2E acceptance for the
// epic: a REAL `gopi task run` short task leaves a bounded report at the
// harness-exported path, and `gopi task show` renders it.
//
// The model is a mock LLM; its ONE tool call is a `write` to the path taken
// from the child's OWN environment (GOPI_TASK_REPORT_PATH) — so the test
// proves the hand-off (the env really reaches the child) AND the transport
// (the file really lands in the task's session dir) AND the read face,
// without a single hardcoded session path in the test.
func TestTaskRunSeamWritesReportAndShowRendersIt(t *testing.T) {
	reportSeamBinary(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	// THE FIXTURE'S OWN GROUND — never the tree under test (see
	// reportSeamFixtureRepo for the hermeticity defect this closes).
	repo := reportSeamFixtureRepo(t)

	const taskID = "01a11500-0000-7000-8000-000000000023"

	reportBody := "# Task Report: seam e2e\n\n## Completed\n- wrote the report from the model's tool call\n\n" +
		"## Files\n- none (seam proof)\n\n## Verification\n- task show rendered this body\n\n" +
		"## Not Done\n- (nothing)\n"
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_report",
			Name: "bash",
			Args: map[string]interface{}{
				"command": "cat > \"$" + taskreport.PathEnv + "\" <<'REPORT_EOF'\n" + reportBody + "REPORT_EOF\n",
			},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\n" +
			"Task complete. The bounded report was written to the harness-supplied path and the run is finished."},
	)
	defer mock.Close()

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"write the task report", "--payload", "write the report to the path the harness exports",
		"--task-id", taskID, "--repo", repo}, "", env, work); code != 0 {
		t.Fatalf("task submit: code=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := runGopiMapped(t, []string{"worker", taskID}, "", env, work); code != 0 {
		t.Fatalf("task run: code=%d stderr=%s", code, stderr)
	}

	// 1. The report exists at the HARNESS-EXPORTED location: the task's own
	//    session dir under the sandboxed GOPI_HOME (never a public flat area).
	wantPath := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID, taskreport.FileName)
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("report not at the harness-exported path %s: %v", wantPath, err)
	}
	if !strings.Contains(string(body), "wrote the report from the model's tool call") {
		t.Errorf("report does not carry the model's authored body:\n%s", body)
	}
	if missing := taskreport.MissingSections(string(body)); len(missing) > 0 {
		t.Errorf("report missing required sections %v:\n%s", missing, body)
	}
	if len(body) > taskreport.MaxBytes {
		t.Errorf("report is %d bytes > cap %d", len(body), taskreport.MaxBytes)
	}

	// 2. `gopi task show` surfaces it, in the operator's normal output.
	show, stderr, code := runGopiMapped(t, []string{"task", "show", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("task show: code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{tools.TaskShowReportLabel, wantPath, "## Completed",
		"wrote the report from the model's tool call", "## Not Done"} {
		if !strings.Contains(show, want) {
			t.Errorf("task show missing %q:\n%s", want, show)
		}
	}
	// 2a. THE HERMETICITY GUARD (the fixture stands on its OWN ground): the
	//     run's resolved deliverable must be the fixture's temp repo, never the
	//     tree under test or a sibling session's worktree. Revert the fixture to
	//     a shared path and this line fails red — before the digest assertion
	//     below turns the same defect into a confusing report-line mismatch.
	assertReportSeamDeliverableIsTheFixturesOwnGround(t, show, repo)
	// 3. The ledger RECORDED the path (the index that outlives the directory):
	//    the run's result digest carries the report line.
	if !strings.Contains(show, taskreport.DigestLinePrefix) {
		t.Errorf("task show result digest does not record the report line:\n%s", show)
	}
}

// TestTaskRunSeamFallsBackToSkeleton: the model does NOT write the report. The
// harness must still leave a bounded report with the required sections — the
// epic's "handles no-compliance" half, and the reason the mechanism is
// described as harness-enforced rather than model-dependent.
func TestTaskRunSeamFallsBackToSkeleton(t *testing.T) {
	reportSeamBinary(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	repo := reportSeamFixtureRepo(t)

	const taskID = "01a11500-0000-7000-8000-000000000024"
	// A model that answers with text only — no tool call, so no report write.
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\n" +
		"Task complete. I finished the work but wrote no report file, deliberately."})
	defer mock.Close()

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"task without a report", "--payload", "do the work and finish without writing any file",
		"--task-id", taskID, "--repo", repo}, "", env, work); code != 0 {
		t.Fatalf("task submit: code=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := runGopiMapped(t, []string{"worker", taskID}, "", env, work); code != 0 {
		t.Fatalf("task run: code=%d stderr=%s", code, stderr)
	}

	wantPath := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID, taskreport.FileName)
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("harness did not leave a report at %s: %v (the seal must not depend on the model)", wantPath, err)
	}
	if missing := taskreport.MissingSections(string(body)); len(missing) > 0 {
		t.Errorf("skeleton missing required sections %v:\n%s", missing, body)
	}
	if !strings.Contains(string(body), taskreport.SkeletonPlaceholder) {
		t.Errorf("skeleton must mark the unrecorded parts explicitly:\n%s", body)
	}
	if !strings.Contains(string(body), taskID) {
		t.Errorf("skeleton must carry the harness-owned task id:\n%s", body)
	}

	show, _, code := runGopiMapped(t, []string{"task", "show", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("task show: code=%d", code)
	}
	if !strings.Contains(show, tools.TaskShowReportLabel) || !strings.Contains(show, taskreport.SkeletonPlaceholder) {
		t.Errorf("task show must render the skeleton report:\n%s", show)
	}
}

// The fixture ground's identity (Rule 1: named constants, never inline
// literals).
const (
	// reportSeamFixtureRepoFile is the single tracked file the fixture's own
	// commit carries: enough for git to call the directory a work tree, and
	// nothing that describes the tree under test.
	reportSeamFixtureRepoFile = "fixture-ground.txt"
	// reportSeamFixtureRepoBody is that file's body — it names the fixture's
	// purpose, so a human who finds the directory knows where it came from.
	reportSeamFixtureRepoBody = "task-report seam fixture ground — never the tree under test (Rule 6)\n"
	// reportSeamFixtureGitName / reportSeamFixtureGitEmail are the identity
	// the fixture's commit uses: a fresh temp repo has none, and an inherited
	// global one is not guaranteed — without them `git commit` would fail on
	// an unrelated cause and the failure would read as a seam defect (Rule 15:
	// the failure names its own cause).
	reportSeamFixtureGitName  = "gopi-seam-fixture"
	reportSeamFixtureGitEmail = "seam-fixture@gopi.invalid"
)

// reportSeamFixtureRepo creates the fixture's OWN git work tree: a throwaway
// repository under this test's temp dir, carrying one commit.
//
// WHY IT EXISTS (the hermeticity defect this test was fixed for): the fixture
// used to submit its row with `--repo <filepath.Abs("../..")>` — the tree
// under test. `task submit` records that as the row's deliverable.repo, and
// the executor's ground is resolved FROM THE ROW, so the run's close-out gate
// measured whatever tree the executing session happened to stand in (a sibling
// session's linked worktree) and refused `completed` whenever that tree was
// dirty — losing taskreport.DigestLinePrefix from the digest and failing the
// seam test for a reason unrelated to the branch under test.
//
// A fixture that OWNS its ground cannot observe another run's dirt: nobody
// else writes this directory, and the session's real worktree is never named.
// The seam coverage is untouched — the row is still submitted into a REAL git
// work tree, a real `gopi worker` still executes it, and the report still
// travels the harness path end to end.
//
// It must be a real work tree (a bare t.TempDir() is refused by `task submit`
// before the seam is reached) and it deliberately holds no Go: the executor
// child stands in it and loads the rules from there, which a mock-backed run
// does not exercise.
func reportSeamFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture ground: git %s in %s: %v\n%s", strings.Join(args, " "), repo, err, out)
		}
	}
	git("init", "-q")
	ground := filepath.Join(repo, reportSeamFixtureRepoFile)
	if err := os.WriteFile(ground, []byte(reportSeamFixtureRepoBody), 0o644); err != nil {
		t.Fatalf("fixture ground: write %s: %v", ground, err)
	}
	git("add", reportSeamFixtureRepoFile)
	git("-c", "user.name="+reportSeamFixtureGitName, "-c", "user.email="+reportSeamFixtureGitEmail,
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture ground for the task-report seam")
	// The fixture's whole value is that NOBODY ELSE writes here: assert the
	// state the hermeticity rests on instead of assuming it.
	if out := reportSeamGitOutput(t, repo, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("fixture ground %s is not clean at creation: %q", repo, out)
	}
	return repo
}

// reportSeamGitOutput runs one git command in dir and returns its stdout,
// failing the test when git itself fails (a missing tool must never read as a
// clean tree).
func reportSeamGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

// reportSeamGroundVerdict decides whether the deliverable block `task show`
// rendered off the run's own row names the FIXTURE's temp repo as the resolved
// ground — and that this ground lies inside the suite's own scratch root, i.e.
// a directory THIS test created. "" = the fixture owns its ground (green);
// anything else = the sentence to fail with.
//
// It is a PURE function (no *testing.T) so the guard's own red/green behaviour
// is unit-testable without driving a whole seam run — see
// TestReportSeamGroundGuardRejectsForeignGround.
//
// THE GUARD THE EPIC ASKS FOR, in two arms, because one alone is not enough:
//
//	① the scratch-root arm is the one that catches the REVERSION. The fixed
//	   fixture stands in a t.TempDir() (under the suite scratch root), so a
//	   row resolving to the tree under test — this worktree, any sibling
//	   session's worktree, /home/user/work/GoPi — names a path outside that
//	   root. Comparing the resolved ground against the `repo` argument alone
//	   would NOT catch it: a reverted fixture passes the tree under test AS the
//	   repo argument, so the two agree by construction.
//	② the repo arm pins the ground to THIS fixture's directory, so one
//	   session's scratch cannot be read as another's ground.
func reportSeamGroundVerdict(show, repo, scratchRoot string) string {
	gotRepo, ok := reportSeamShowField(show, tools.TaskShowDeliverableRepoLabel)
	if !ok {
		return fmt.Sprintf("task show carries no %q line — the row's deliverable is unreadable, so the fixture's ground cannot be proven (an unprovable ground is never a passing one, Rule 15):\n%s",
			tools.TaskShowDeliverableRepoLabel, show)
	}
	if !reportSeamPathWithin(scratchRoot, gotRepo) {
		return fmt.Sprintf("the row's deliverable.repo is %q, which lies OUTSIDE the suite's own scratch root %q — the fixture is standing on a SHARED tree (the tree under test, or a sibling session's worktree), not on a directory it created. That is the non-hermetic seam (2026-09-19): the run's close-out gate then measures somebody else's dirt and refuses `%s` for reasons unrelated to the branch under test:\n%s",
			gotRepo, scratchRoot, tools.TaskStatusCompleted, show)
	}
	if !reportSeamSamePath(gotRepo, repo) {
		return fmt.Sprintf("the row's deliverable.repo is %q, not the fixture's own temp repo %q:\n%s", gotRepo, repo, show)
	}
	if wt, ok := reportSeamShowField(show, tools.TaskShowDeliverableWorktreeLabel); ok {
		// The GROUND is a linked worktree the WORKER created for the row (epic
		// worker-creates-child-ground, 2026-09-22), so it is a SIBLING of the
		// fixture's repo, not a path inside it — the pre-epic expectation. The
		// guard's actual subject is HERMETICITY: the ground must be a directory
		// THIS test created. Since MockEnv pins the worktree root to a
		// t.TempDir() (also under the suite scratch root), containing the
		// ground in the scratch root is both the correct and the STRICTER
		// reading — a run bound to the operator's real ~/workspace ground, or
		// to a sibling session's worktree, still fails here.
		if !reportSeamPathWithin(scratchRoot, wt) {
			return fmt.Sprintf("the row's deliverable.worktree is %q, which lies OUTSIDE the suite's own scratch root %q — the run was bound to a FOREIGN worktree (the operator's real workspace, or another session's), not the ground this test created:\n%s",
				wt, scratchRoot, show)
		}
	}
	return ""
}

// assertReportSeamDeliverableIsTheFixturesOwnGround applies the guard above to
// a live seam run, so a regression to a shared path is reported where its
// cause is visible rather than as a confusing digest mismatch downstream. The
// scratch root is the suite's own (testutil.SuiteTempRoot) — the tree every
// t.TempDir() in this binary lands in.
func assertReportSeamDeliverableIsTheFixturesOwnGround(t *testing.T, show, repo string) {
	t.Helper()
	if verdict := reportSeamGroundVerdict(show, repo, testutil.SuiteTempRoot()); verdict != "" {
		t.Fatal(verdict)
	}
}

// reportSeamShowField reads one `label value` line out of `task show`'s
// output. The label is taken from internal/tools (its renderer), never
// re-spelled here (Rule 14).
func reportSeamShowField(show, label string) (string, bool) {
	for _, line := range strings.Split(show, "\n") {
		if after, found := strings.CutPrefix(strings.TrimSpace(line), strings.TrimSpace(label)+" "); found {
			return strings.TrimSpace(after), true
		}
	}
	return "", false
}

// reportSeamSamePath reports whether two directories are the same ground.
// Symlinks are resolved first (the scratch root may be reached through one),
// and an unresolvable path falls back to its cleaned absolute spelling — the
// comparison never claims a match it cannot support.
func reportSeamSamePath(a, b string) bool {
	ra, rb := reportSeamResolvePath(a), reportSeamResolvePath(b)
	return ra != "" && ra == rb
}

// reportSeamPathWithin reports whether p lies at or under root, on resolved
// absolute paths.
func reportSeamPathWithin(root, p string) bool {
	rr, rp := reportSeamResolvePath(root), reportSeamResolvePath(p)
	if rr == "" || rp == "" {
		return false
	}
	return rp == rr || strings.HasPrefix(rp, rr+string(filepath.Separator))
}

// reportSeamResolvePath canonicalises a path for comparison ("" when it cannot
// be resolved at all).
func reportSeamResolvePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// modeOf reports a FileInfo's mode for the build-guard message ("" when the
// stat failed — the error beside it carries the cause).

func modeOf(fi os.FileInfo) string {
	if fi == nil {
		return "n/a"
	}
	return fi.Mode().String()
}

// TestReportSeamGroundGuardRejectsForeignGround is the guard's own red-proof,
// run without driving a seam: it hands reportSeamGroundVerdict the shapes a
// regression would produce and requires the refusal.
//
// Why a pure unit test rather than a second live run: the live seam is the
// expensive half (a built binary, a mocked LLM, a submitted row) and the
// property being guarded is a pure comparison over `task show`'s output. The
// live half is exercised by the assertion inside
// TestTaskRunSeamWritesReportAndShowRendersIt, on the real output of the real
// run — this test pins the predicate that assertion delegates to, including
// the two shapes that must NOT be refused.
func TestReportSeamGroundGuardRejectsForeignGround(t *testing.T) {
	// The suite's scratch root, in the shape a real run sees it.
	const scratchRoot = "/home/user/workspace/gopi-test-scratch/suite-cmd-agent-run-1"
	// The fixture's own ground: a t.TempDir() INSIDE that root.
	repo := filepath.Join(scratchRoot, "TestTaskRunSeamWritesReportAndShowRendersIt123", "001")
	// The shapes the defect took: the tree under test (this session's worktree
	// and a sibling session's — both OUTSIDE the suite scratch root).
	const treeUnderTest = "/home/user/workspace/gopi-seam-hermetic"
	const siblingWorktree = "/home/user/workspace/gopi-cmdagent-reds"
	render := func(repoLine, worktreeLine string) string {
		show := "task_id:        01a11500-0000-7000-8000-000000000023\nstatus:         completed\n"
		if repoLine != "" {
			show += tools.TaskShowDeliverableRepoLabel + " " + repoLine + "\n"
		}
		if worktreeLine != "" {
			show += tools.TaskShowDeliverableWorktreeLabel + " " + worktreeLine + "\n"
		}
		return show + "result:         " + taskreport.DigestLinePrefix + "\n"
	}
	verdict := func(t *testing.T, show, repo string) string {
		t.Helper()
		return reportSeamGroundVerdict(show, repo, scratchRoot)
	}

	t.Run("the REVERTED fixture (tree under test as --repo) is refused", func(t *testing.T) {
		// The pre-fix shape, verbatim: the fixture submits the tree under test,
		// so its own `repo` argument and the row's resolved deliverable.repo
		// both name that shared tree — only the scratch-root arm sees it.
		if v := verdict(t, render(treeUnderTest, treeUnderTest), treeUnderTest); v == "" {
			t.Fatal("guard accepted the tree under test as the fixture's ground — the regression the epic names would pass silently")
		}
	})
	t.Run("a sibling session's worktree is refused", func(t *testing.T) {
		if v := verdict(t, render(siblingWorktree, siblingWorktree), siblingWorktree); v == "" {
			t.Fatal("guard accepted a sibling session's worktree as the fixture's ground")
		}
	})
	t.Run("a foreign worktree in another session's scratch is refused", func(t *testing.T) {
		foreign := "/home/user/workspace/gopi-test-scratch/suite-cmd-agent-run-2/Other123/001"
		if v := verdict(t, render(foreign, foreign), repo); v == "" {
			t.Fatal("guard accepted a sibling suite's scratch directory as this fixture's ground")
		}
	})
	t.Run("a worktree outside the suite scratch root is refused", func(t *testing.T) {
		// The GROUND is a linked worktree SIBLING of the fixture repo (epic
		// worker-creates-child-ground), so "outside the fixture repo" is the
		// normal shape now — what must never pass is a ground outside the
		// SUITE's own scratch root, i.e. one this test did not create.
		if v := verdict(t, render(repo, "/home/user/workspace/gopi-otherworld"), repo); v == "" {
			t.Fatal("guard accepted a worktree outside the suite's scratch root — the operator's real workspace would pass silently")
		}
	})
	t.Run("an unreadable deliverable is refused, not assumed clean", func(t *testing.T) {
		if v := verdict(t, render("", ""), repo); v == "" {
			t.Fatal("guard passed a show body with no deliverable line — an unprovable ground is never a passing one (Rule 15)")
		}
	})
	t.Run("the fixture's own ground passes", func(t *testing.T) {
		if v := verdict(t, render(repo, repo), repo); v != "" {
			t.Fatalf("guard refused the fixture's own ground: %s", v)
		}
		// Branch-only deliverable (no worktree stamped yet) is the ordinary
		// mid-run shape and must stay green.
		if v := verdict(t, render(repo, ""), repo); v != "" {
			t.Fatalf("guard refused a repo-only deliverable on the fixture's ground: %s", v)
		}
		// A registered worktree OF the fixture's repo is still the fixture's.
		if v := verdict(t, render(repo, filepath.Join(repo, ".worktrees", "wt")), repo); v != "" {
			t.Fatalf("guard refused the fixture repo's own linked worktree: %s", v)
		}
	})
}
