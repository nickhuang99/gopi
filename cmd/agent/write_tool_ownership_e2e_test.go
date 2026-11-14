package main

// THE REAL-WRITER DEMONSTRATION (the epic's acceptance #1): a document written
// through gopi's OWN write tool — the real binary, driven by the scripted mock
// LLM, in a real run — that teaches a worker-launch shape comes out annotated
// with the canonical pointer and NO hand editing.
//
// WHY THIS TEST EXISTS AND WHY IT IS NOT IN internal/testutil: the unit tests
// there prove the injection's predicate and the gate's decision agree, and the
// tool-level tests prove the tool emits the line. Neither proves that the line
// reaches the DISK from a run — a tool the loop refuses to call, or a seam that
// is simply not wired, would leave both green while the merge-round red stayed
// exactly as it was (Rule 11: a tool change is accepted only when the LLM
// really calls it, with the session record as evidence).
//
// The shape is the established cmd/agent harness: a raw-prompt run of the REAL
// binary, a scripted mock LLM (zero API calls), a throwaway git fixture repo as
// the run's cwd, and a sandboxed GOPI_HOME/GOPI_MEMORY_DB.
//
// THE MOCKED PATH IS ABSOLUTE AND INSIDE THE RUN'S OWN TREE. A mocked relative
// path is resolved against the executing process's cwd — the PACKAGE DIRECTORY
// for a test binary — which once dropped an untracked file into every checkout
// (cli_tasks_waitpid_test.go's incident). Here the destination is
// <fixture repo>/devdoc/report.md, so the assertion can only pass if the write
// really landed where the gate's walk would see it, and nothing can leak.
//
// The repository-context sandbox is the same one the sibling E2E tests use: the
// wrapper child must never inherit THIS process's cwd and enumerate the
// developer's live worktrees.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/workerownership"
)

// The binary these tests drive cannot come from testutil.GopiBin's
// $GOPI_BIN-or-bin/gopi discovery: PinSuiteForTestMain moves the suite's cwd
// into a scratch root, moduleRoot() then walks up from there and finds no
// go.mod, so that discovery only works when a caller exports GOPI_BIN. The
// WRITE-TIME annotation has to be demonstrated on EVERY run (`go test
// ./cmd/agent/`), so the binary is built from THIS tree on every run — which is
// exactly what the package's ONE build slot (gopibin_test.go) does.
var writerE2EBuildErr error

// writerE2EBin returns the gopi binary this E2E drives, plus whether it is
// usable. The binary is the package's ONE build slot (gopibin_test.go): it is
// built from THIS tree on every run, so the freshness claim these cases make
// still holds (assertGopiBinIsFresh compares it against the newest production
// source), and it lives in the suite scratch root rather than the tree — so the
// test no longer writes bin/gopi into whatever checkout it runs from.
//
// The requirement that made this file build its own copy was the freshness of
// the artifact, not the LIFETIME of the path (this E2E runs gopi in the
// foreground); the shared slot is built seconds before it is used, which is
// strictly fresher than a cached bin/gopi from an unrelated `make build`.
func writerE2EBin(t *testing.T) (string, bool) {
	t.Helper()
	bin := gopiTestBinPath(t)
	if _, err := os.Stat(bin); err != nil {
		writerE2EBuildErr = err
		return "", false
	}
	return bin, true
}

// writerE2EReportRel is the document the mocked executor writes: INSIDE the
// gate's audited tree, a `.md`, and teaching a launch shape — i.e. every
// condition the write-time annotation answers to.
const writerE2EReportRel = workerownership.DirDevdoc + "/writer-e2e-report.md"

// writerE2EToolArgPath / writerE2EToolArgContent are the write tool's wire
// argument names (toolnames-adjacent literals the sibling tests spell the same
// way).
const (
	writerE2EToolArgPath    = "path"
	writerE2EToolArgContent = "content"
)

// TestWriteToolAnnotatesTeachingDocsInAuditedTrees is acceptance #1: the REAL
// binary writes a teaching doc through the write tool and the file on disk
// carries the canonical pointer.
func TestWriteToolAnnotatesTeachingDocsInAuditedTrees(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin, built := writerE2EBin(t)
	if !built {
		t.Skipf("could not build bin/gopi: %v", writerE2EBuildErr)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)
	t.Setenv("GOPI_BIN", bin)
	SandboxRepoContext(t)

	repo := RepoContextFixtureRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, workerownership.DirDevdoc), 0o755); err != nil {
		t.Fatalf("create the audited tree in the fixture repo: %v", err)
	}
	target := filepath.Join(repo, filepath.FromSlash(writerE2EReportRel))

	// The document the executor writes: a report that TEACHES the launch move,
	// the exact shape the gate exists to catch (a needle inside prose).
	teaching := "# Task Report: drain the queue\n\n" +
		"The operator's move is `" + workerownership.Needles[0] + "`; an agent submits the work.\n"
	if workerownership.Annotated(teaching) {
		t.Fatalf("the fixture content already carries a pointer — the test would pass vacuously")
	}

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_write_teaching_doc",
			Name: toolnames.ToolWrite,
			Args: map[string]interface{}{writerE2EToolArgPath: target, writerE2EToolArgContent: teaching},
		}},
		testutil.Resp{Text: "Report written."},
	)
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, t.TempDir(), mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SKIP_CLAIM_BROADCAST=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
	stdout, stderr, code := runGopiMapped(t, []string{"write the report " + target}, "", env, repo)
	if code != 0 {
		t.Fatalf("the run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	onDisk, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the write tool never landed %s — the demonstration proves nothing: %v\nstdout:\n%s\nstderr:\n%s",
			target, err, stdout, stderr)
	}
	// PREMISE ASSERTED BEFORE THE VERDICT (Rule 10.4): the file on disk really
	// is a teaching doc, so a green below cannot be an artifact of the mock
	// having written something else.
	if !workerownership.MentionsLaunchShape(string(onDisk)) {
		t.Fatalf("the file on disk does not mention a launch shape — vacuous:\n%s", onDisk)
	}
	if !workerownership.Annotated(string(onDisk)) {
		t.Errorf("a document written through gopi's OWN write tool landed with NO canonical pointer — this is the merge-round red, still live:\n%s\n\nstdout:\n%s", onDisk, stdout)
	}
	want := workerownership.CommentLine(workerownership.SuffixDoc)
	if !strings.Contains(string(onDisk), want) {
		t.Errorf("the file carries a pointer, but not the markdown comment form the gate's own recipe prints (%q):\n%s", want, onDisk)
	}
}

// TestEditToolAnnotatesTeachingDocsInAuditedTrees is the same demonstration for
// the other door: an EDIT that introduces a launch shape into an already
// existing document must also come out annotated (the edit path reaches the
// atomic write directly, so it is a separate seam from write).
func TestEditToolAnnotatesTeachingDocsInAuditedTrees(t *testing.T) {
	bin, built := writerE2EBin(t)
	if !built {
		t.Skipf("could not build bin/gopi: %v", writerE2EBuildErr)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)
	t.Setenv("GOPI_BIN", bin)
	SandboxRepoContext(t)

	repo := RepoContextFixtureRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, workerownership.DirDevdoc), 0o755); err != nil {
		t.Fatalf("create the audited tree in the fixture repo: %v", err)
	}
	target := filepath.Join(repo, workerownership.DirDevdoc, "writer-e2e-edit.md")
	seed := "# Notes\n\nThe queue is the handoff.\n"
	if err := os.WriteFile(target, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed the document: %v", err)
	}

	const oldText = "The queue is the handoff."
	newText := "Run `" + workerownership.Needles[0] + "` to drain it."
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_edit_teaching_doc",
			Name: toolnames.ToolEdit,
			Args: map[string]interface{}{"path": target, "oldStr": oldText, "newStr": newText},
		}},
		testutil.Resp{Text: "Edited."},
	)
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, t.TempDir(), mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SKIP_CLAIM_BROADCAST=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
	stdout, stderr, code := runGopiMapped(t, []string{"edit " + target}, "", env, repo)
	if code != 0 {
		t.Fatalf("the run must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	onDisk, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if !strings.Contains(string(onDisk), workerownership.Needles[0]) {
		t.Fatalf("the edit never landed the launch shape — the demonstration proves nothing:\n%s\nstdout:\n%s", onDisk, stdout)
	}
	if !workerownership.Annotated(string(onDisk)) {
		t.Errorf("an EDIT that introduced a launch shape landed NO canonical pointer:\n%s\n\nstdout:\n%s", onDisk, stdout)
	}
}
