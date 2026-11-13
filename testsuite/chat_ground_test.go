package testsuite

// chat_ground_test.go — the END-TO-END surface of "a chat session has no ground
// of its own" (epic chat-session-ground-retired-when-not-a-row, 2026-09-29).
// Every case drives the REAL binary, so what is asserted is what an operator's
// shell sees: `gopi chat` started in a repository's main checkout, one tool
// round that writes a file, and then the readings that decide the retirement —
// what git gained (nothing), what the worktree root gained (nothing), where the
// write landed (the caller's directory), and what the session SAID about it.
//
// This file's assertions are the REVERSE of the ones it carried until now, when
// the epic gave each chat a linked worktree on `chat/<session-id>`: the old code
// made those, the operator retired it (a chat is not a ledger row, so nothing
// could ever collect the ground), and a test kept "for the old shape" would be a
// green husk over deleted code.
//
// PROJECT.md Rule 31 is why this file exists at all: the producer's unit tests call
// ChatGroundFor directly, while the claim is about a CHAT — the tool round, the
// working directory, and the line on stderr. A unit test cannot see those.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// chatGroundWrittenFile is the file the mocked turn asks the engine to write,
// by RELATIVE path — which is the whole point: a relative path in a chat round
// resolves against the process cwd, so where it lands is exactly where the
// session was standing.
const (
	chatGroundWrittenFile    = "chat-ground-written.txt"
	chatGroundWrittenContent = "written by the chat session, in the caller's directory\n"
)

// chatGroundPolicyArg is the posture this case runs under, spelled as the
// operator would: `--policy=allow` (the word read from its ONE definition in
// internal/chatpolicy rather than typed here).
//
// IT IS NOT DECORATION AND IT IS NOT A WAY AROUND A FAILING ASSERTION. The chat
// surface's write/edit arm is judged by the operator's ```chat-policy fence plus
// this run's frozen posture (internal/tools/chat_write_guard.go,
// ChatWritePathRefusalForCarrier), and the BUILT-IN table is `prompt *`: with no
// fence in the fixture and no declared posture, every write target is a QUESTION.
// On `--once` there is no operator to answer it (the run declares itself
// unattended), so the write comes back `[NEEDS OPERATOR APPROVAL — command NOT
// run]` and the file is never created — which is what assertion ③ measured
// before this constant existed. The failure was NEVER the working directory or
// the path resolution: the tool_use carried `"path": "chat-ground-written.txt"`
// verbatim and the session's `workdir` was the fixture repo, so the refusal — not
// a misresolved path — is what left the file absent.
//
// So the case declares the posture its own subject needs. What ③ is FOR — "the
// session did not MOVE, so a relative path lands in the caller's directory" — is
// measurable only when the write is permitted to happen at all; a refused write
// says nothing about where a permitted one would have landed. Pinning `allow`
// also keeps the reading independent of the operator's own settings document
// (MockEnv strips every GOPI_* variable, but settings.json is read from the
// fixture's GOPI_HOME, so an ambient file would otherwise decide this test).
const chatGroundPolicyArg = cliverbs.PolicyFlag + "=" + chatpolicy.PostureYoloWord

// chatGroundAnswer is the mock's final answer. Comfortably longer than the
// engine's degenerate-round floor (minFinalAnswerChars), so a completed turn
// really produces it.
const chatGroundAnswer = "Done — the file was written in the directory this session was started in."

// chatGroundGit runs one git command in dir and returns its combined output.
func chatGroundGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// chatGroundRepo builds a REAL repo with one commit — the "main checkout" the
// chat is started in, which is the shape this epic is about.
func chatGroundRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "chat-ground-main")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"checkout", "-b", "master"},
	} {
		chatGroundGit(t, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chatGroundGit(t, repo, "add", "-A")
	chatGroundGit(t, repo, "commit", "-m", "base")
	return repo
}

// TestChatGround_NoBranchNoWorktreeAndTheWriteLandsWhereTheSessionStands is the
// headline contract of the retirement, in one run:
//
//	① git gained NO branch — in particular no `chat/*` ref (the residue this
//	   epic exists to stop producing) — and NO worktree registration,
//	② the pinned worktree root gained nothing: the retired producer created its
//	   grounds there, so an EMPTY root is the measurement that the producer is
//	   gone,
//	③ the turn's relative-path write landed in the CALLER'S directory — i.e. the
//	   session did not move,
//	④ start-up SAID so on stderr, naming the directory and calling it the main
//	   checkout (a silent run in someone's checkout is the defect).
//
// The old assertions here were the exact opposites (a `chat/*` branch existed, a
// worktree was registered, the write was NOT in the main checkout): they are
// replaced by these, not kept beside them.
func TestChatGround_NoBranchNoWorktreeAndTheWriteLandsWhereTheSessionStands(t *testing.T) {
	home := t.TempDir()
	repo := chatGroundRepo(t)

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_01_write",
			Name: toolnames.ToolWrite,
			Args: map[string]interface{}{
				"path":    chatGroundWrittenFile,
				"content": chatGroundWrittenContent,
			},
		}},
		testutil.Resp{Text: chatGroundAnswer},
	)
	defer llm.Close()

	// MockEnv pins GOPI_WORKTREE_ROOT to a per-test temp dir, so a producer that
	// came back would create its ground under the fixture and be SEEN here
	// rather than in the operator's real workspace.
	env := testutil.MockEnv(t, home, llm.URL())
	root := chatGroundEnvRoot(env)
	if root == "" {
		t.Fatalf("the mock environment did not pin a worktree root — the 'no ground was minted' claim would have nowhere to look")
	}
	branchesBefore := chatGroundGit(t, repo, "branch", "-a")
	worktreesBefore := chatGroundGit(t, repo, "worktree", "list")

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--tools", chatGroundPolicyArg, "-p", "write the file " + chatGroundWrittenFile},
		"", env, repo)

	if code != 0 {
		t.Fatalf("gopi chat --once exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, chatGroundAnswer) {
		t.Errorf("the turn did not answer: stdout = %q", stdout)
	}
	// ① git gained nothing: no branch at all, and certainly not in the retired
	// namespace; no new worktree registration.
	if got := chatGroundGit(t, repo, "branch", "-a"); got != branchesBefore {
		t.Fatalf("the chat changed the repository's branches:\nbefore:\n%s\nafter:\n%s", branchesBefore, got)
	}
	if refs := chatGroundGit(t, repo, "branch", "--list", tools.ChatGroundBranchPrefix+"*"); refs != "" {
		t.Fatalf("a %s* branch exists after the chat — the retired creation path is back:\n%s", tools.ChatGroundBranchPrefix, refs)
	}
	if got := chatGroundGit(t, repo, "worktree", "list"); got != worktreesBefore {
		t.Fatalf("the chat created a worktree:\nbefore:\n%s\nafter:\n%s", worktreesBefore, got)
	}
	// ② the place grounds used to be minted.
	if entries, err := os.ReadDir(root); err != nil {
		t.Fatalf("read the pinned worktree root %s: %v", root, err)
	} else if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("the worktree root %s gained %d entries (%v) — a chat must mint no ground", root, len(entries), names)
	}
	// ③ the write landed where the session stood — the caller's directory. It is
	// therefore UNTRACKED work in that checkout, which is exactly the fact ④
	// exists to announce.
	written := filepath.Join(repo, chatGroundWrittenFile)
	body, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("the turn's write is not in the caller's directory %s: %v", repo, err)
	}
	if string(body) != chatGroundWrittenContent {
		t.Errorf("the written file's content = %q, want %q", string(body), chatGroundWrittenContent)
	}
	if dirty := chatGroundGit(t, repo, "status", "--porcelain"); !strings.Contains(dirty, chatGroundWrittenFile) {
		t.Errorf("the main checkout's status does not list the write, so the walk above measured the wrong tree:\n%s", dirty)
	}
	// ④ the session SAID it, naming the directory, before any turn ran.
	if !strings.Contains(stderr, repo) {
		t.Fatalf("start-up did not name the directory this session writes in (%s):\n%s", repo, stderr)
	}
	if !strings.Contains(stderr, "MAIN CHECKOUT") {
		t.Errorf("start-up did not say the writes land in a repository's main checkout:\n%s", stderr)
	}
	if strings.Contains(stderr, "this session's worktree") {
		t.Errorf("start-up still announced a worktree of the session's own — the retired promise:\n%s", stderr)
	}
	if strings.Contains(stderr, "LEFT IN PLACE") {
		t.Errorf("the close-out still named a ground left in place, but this session has none:\n%s", stderr)
	}
}

// TestChatGround_NonRepoDirectoryStillStarts is the directory shape that must
// never refuse: outside a git work tree `gopi chat` STILL RUNS, and says what it
// has — a directory with no checkout to name, never a silent session and never a
// refusal.
func TestChatGround_NonRepoDirectoryStillStarts(t *testing.T) {
	home := t.TempDir()
	plain := t.TempDir() // deliberately NOT a repo

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatGroundAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "-p", "say something"},
		"", testutil.MockEnv(t, home, llm.URL()), plain)

	if code != 0 {
		t.Fatalf("gopi chat --once in a non-repo directory exit = %d, want 0 (a chat must not be refused for lacking a repository)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, chatGroundAnswer) {
		t.Errorf("the turn did not answer: stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "no git checkout here") {
		t.Fatalf("the run did not say what this directory is (a silent session in someone's directory is the defect):\n%s", stderr)
	}
	if !strings.Contains(stderr, plain) {
		t.Errorf("the report must name the directory it is about (%s):\n%s", plain, stderr)
	}
	// The report is not a failure: the session is REPORTED, not degraded.
	if strings.Contains(stderr, tools.ChatGroundDetailDegradedLabel) {
		t.Errorf("a non-repo directory is an answer, not a degradation:\n%s", stderr)
	}
	if entries, err := os.ReadDir(plain); err == nil && len(entries) != 0 {
		t.Errorf("the non-repo directory gained %d entries — a chat mints nothing anywhere", len(entries))
	}
}

// chatGroundEnvRoot reads the pinned worktree root back out of the mock
// environment (MockEnv is the ONE script that decides a mocked run's
// environment, so the test reads it rather than re-deriving it).
func chatGroundEnvRoot(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GOPI_WORKTREE_ROOT="); ok {
			return v
		}
	}
	return ""
}
