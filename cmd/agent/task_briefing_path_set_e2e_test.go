package main

// task_briefing_path_set_e2e_test.go — ACCEPTANCE ON THE REAL SURFACE (Rule 31)
// for the dispatch seam's FOURTH clause: the pinned path set.
//
// The clause was measured from row 01a1447c's own briefing (2026-10-16), and the
// half that matters here is the one a unit test cannot reach: the clause is
// APPENDED to the briefing the executor child actually receives, and it survives
// into the child's FIRST model request. That is the acceptance the seam's own
// devdoc names as the missing one for the previous attempt at this seam
// (devdoc/claim-truncation-at-dispatch.md: "a clause truncated away is a clause
// that does not exist"), so it is asserted end to end rather than argued.
//
// WHAT THIS WALK MEASURES: a row whose briefing pins a path set is dispatched
// with a scripted child held inside its first model round trip, and the child's
// first request body must carry the clause's label and its instruction. The
// positive control is the branch that would make the assertion meaningless — an
// ordinary payload (one a child's process would carry anyway) — and it is
// exercised by the ground-announcement walk beside this file, which dispatches a
// row WITHOUT a pinned set and never sees this label.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// pathSetE2ERow is this walk's row id — a FULL uuid, because the ground's path
// and branch stem are derived from it (internal/tools/task_run_ground.go).
const pathSetE2ERow = "3f1d6a90-2b47-4c8e-9d15-7a6e0cbb4f21"

// pathSetE2EPayload is the briefing shape under test: a count in prose, then the
// shell list block the count quantifies. Its paths exist in the fixture repo, so
// the clause's ground comparison has something real to answer for.
const pathSetE2EPayload = "Fix the wording of one error message.\n" +
	"\n" +
	"用 manifest 的**唯一写入者**逐行重生成这 12 行：\n" +
	"\n" +
	"```bash\n" +
	"for p in devdoc/a.md \\\n" +
	"         devdoc/b.md; do\n" +
	"  echo \"$p\"\n" +
	"done\n" +
	"```\n"

// TestBriefingPathSetClauseReachesTheChildsFirstRequest is the explicit
// acceptance walk: the pinned-set clause is appended at the spawn seam AND lands
// in the child's first model request.
func TestBriefingPathSetClauseReachesTheChildsFirstRequest(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := t.TempDir()
	repo := executorCwdProbeRepo(t, "pathset-repo")
	if err := os.MkdirAll(filepath.Join(repo, "devdoc"), 0o755); err != nil {
		t.Fatalf("mkdir devdoc: %v", err)
	}
	for _, p := range []string{"devdoc/a.md", "devdoc/b.md"} {
		if err := os.WriteFile(filepath.Join(repo, p), []byte("# "+p+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "fixture: the two pinned paths"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Fixture precondition, asserted rather than assumed: the payload's shape
	// really does carry a pinned set, so a green walk below is a statement about
	// the clause and not about a payload the leaf refused.
	if claims := taskreport.PathSetClaimsIn(pathSetE2EPayload); len(claims) != 1 || claims[0].Count != 12 {
		t.Fatalf("fixture precondition: the payload must carry ONE pinned set of 12; got %+v", claims)
	}

	mock := testutil.NewMockLLM(t, testutil.Resp{Hold: true}, testutil.Resp{Text: "done"})
	t.Cleanup(mock.Close)
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())
	env = append(env, worktreeroots.WorktreeRootEnvName+"="+root)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"pinned path set", "--payload", pathSetE2EPayload,
		"--task-id", pathSetE2ERow, "--repo", repo}, "", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	wrapper := exec.Command(bin, "worker", pathSetE2ERow)
	wrapper.Dir = repo
	wrapper.Env = env
	var wout, werr strings.Builder
	wrapper.Stdout = &wout
	wrapper.Stderr = &werr
	if err := wrapper.Start(); err != nil {
		t.Fatalf("start `gopi worker %s`: %v", pathSetE2ERow, err)
	}
	t.Cleanup(func() {
		if wrapper.ProcessState == nil {
			_ = wrapper.Process.Kill()    //stylegate:ignore kills the fixture process the test spawned
			_, _ = wrapper.Process.Wait() //stylegate:ignore reaps the fixture process the test spawned
		}
	})
	childPid, _ := groundWaitForChildCwd(t, pathSetE2ERow, wrapper.Process.Pid)
	t.Cleanup(func() { groundKill(t, childPid) })

	groundWaitForFirstModelRequest(t, mock)
	body := mock.RequestBody(1)
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the mock served no request body — the child never reached its first model round, so the clause cannot be measured")
	}
	// THE CLAIM: the clause the seam appended is IN the child's first request —
	// label, instruction and the author's own line, not merely in the seam's record.
	for _, want := range []string{
		tools.TaskBriefingPathSetLabel,
		tools.TaskBriefingPathSetVerifyOnly,
		"12 行",
		"devdoc/a.md",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the child's FIRST model request must carry %q — a clause that never reaches the child is a clause that does not exist:\n%s",
				want, groundAnnounceBodyExcerpt(body))
		}
	}
}
