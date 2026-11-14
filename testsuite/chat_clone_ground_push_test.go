package testsuite

// chat_clone_ground_push_test.go — THE REAL BINARY, THE REAL DECISION.
//
// PROJECT.md Rule 31: the unit tests in internal/tools call ChatOpAdmitIn
// directly, so they prove the PREDICATE. The claim this file measures is about a
// CHAT — a tool round, the directory it runs in, and the refusal (or its absence)
// that reaches the model — so it drives `gopi chat --once --tools` against a
// scripted LLM, twice, with the SAME command line and a different working
// directory:
//
//	① in a CLONE GROUND (a real clone one level below the pinned workspace root)
//	   `git push origin HEAD:master` is the clone channel's delivery and RUNS,
//	② in the repo's MAIN CHECKOUT the identical line RUNS TOO.
//
// WHAT ② ASSERTED AND WHY IT FLIPPED. This arm was written when a push was a REF
// MOVE, and the claim was "the same line from the shared checkout is refused".
// TWO later operator rulings retired that claim, and the second one is the one
// that fired here:
//
//	2026-11-03 (row 01a1a786, "push 都可以") — every `git push` spelling is a
//	  REPOSITORY WRITE by its WORDS; `chatOpClassifyIn`'s own contract says the
//	  directory "is carried for the callers' sakes, not to decide a push".
//	2026-11-05 (commit eb1b36d1, "限制的全部改掉") — even the REF MOVE class is
//	  admitted, so the class that used to refuse ② no longer refuses anything.
//
// So the production answer was measured, not assumed: with the criterion judging
// the WORDS, a push from the main checkout is ADMITTED and RUNS, and an arm
// demanding a refusal was pinning a gate that production deliberately deleted.
// The repair is therefore to the TEST and not to the guard — see
// chatClonePushBothSides.
//
// The observable is still the tool_result the session records: a refused bash call
// comes back carrying the category refusal text, an admitted one comes back with
// the bash tool's own `exit=N` prefix and the command's output. Reading the
// session record rather than the guard's return value is what keeps this an
// END-TO-END assertion.
//
// The defect this exists for (operator's epic, 2026-10-29): the push was refused
// in BOTH places, so a console could not finish a failed or interrupted row's
// delivery and a commit living only in a clone died with that clone.
//
// THE NAME IS HISTORICAL, AND IT IS KEPT ON PURPOSE. Its "…IsRefused" half is the
// claim the defect was FILED with; the assertion below now pins what the rulings
// make true. Renaming the function would make this row's own acceptance command
// match no test at all — the vacuum Rule 38 and Rule 42 both record as worse than
// no check, because `go test` prints "no tests to run" and exits 0.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// chatClonePushCommand is THE OPERATOR'S OWN SPELLING from the bug row
	// (01a19443): `git push origin`, no refspec. It is also what the channel's
	// delivery uses, and what the guard used to refuse outright — its destination
	// is decided by git (push.default), so the fixture below is a real clone whose
	// master tracks origin/master.
	chatClonePushCommand = "git push origin"
	// chatClonePushBranch is the branch both fixture checkouts stand on.
	chatClonePushBranch = "master"
	// chatClonePushRefusalMarker is the phrase the category refusal carries; it
	// is what a REFUSED bash result contains and an admitted one cannot (the
	// command's own output is git's).
	chatClonePushRefusalMarker = "CHAT OPS CATEGORY"
	// chatClonePushAnswer is the mock's final answer — longer than the engine's
	// degenerate-round floor so a completed turn really produces it.
	chatClonePushAnswer = "Ran the delivery push in the ground this session was started in."
	// chatClonePushAheadSubject is the commit the shared main checkout carries and
	// origin does not — it is what makes that side's push have something to send,
	// so git answers with the ref update instead of "Everything up-to-date".
	chatClonePushAheadSubject = "the checkout's own unpushed change"
	// chatClonePushUpToDateMarker is git's answer when a push sends no ref — the
	// string a push cannot produce once the branch it pushes differs from the
	// destination. Measured on git 2.43.0: a bare `git push origin` over an
	// up-to-date branch prints exactly "Everything up-to-date" and exits 0.
	chatClonePushUpToDateMarker = "Everything up-to-date"
	// chatClonePushRefUpdateMarker is what a push that DID send a ref prints — the
	// refspec summary line `… -> master`. It is the positive half of the work proof.
	chatClonePushRefUpdateMarker = "-> " + chatClonePushBranch
)

// chatClonePushGit runs one git command in dir.
func chatClonePushGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv("GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// chatClonePushFixture builds the two directories the two cases need:
//
//	<workspaceRoot>/01a194436e69-fixture-ground  a real CLONE (of origin.git), one
//	                                             level below the pinned root —
//	                                             the clone channel's shape;
//	<base>/main-checkout                         a repository OUTSIDE that root —
//	                                             the shared main checkout's shape.
//
// THE TWO CHECKOUTS CARRY OPPOSITE TREES ON PURPOSE, and that is what makes "which
// side ran?" readable from the WORK, not only from a refusal string. A push whose
// branch matches its destination is answered by git with `Everything up-to-date`
// and exit 0 (measured: git 2.43.0), a verdict indistinguishable from a push the
// guard silently swallowed. So the shared checkout is WOUND BACK one commit: its
// push has something to send and reports `... -> master` with git's own
// `|`-prefixed object count, while the ground — which was cloned at origin's tip —
// can only ever answer `Everything up-to-date`. The main checkout is rewound AFTER
// the ground is cloned, so both checkouts really do start at the same commit.
//
// It returns (workspaceRoot, ground, mainCheckout).
func chatClonePushFixture(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	origin := filepath.Join(base, "origin.git")
	mainCheckout := filepath.Join(base, "main-checkout")
	ground := filepath.Join(root, "01a194436e69-fixture-ground")

	// The origin is a bare repo, so both checkouts are CLONES of it and a push is
	// a real push (it succeeds), which is what makes "did the command run?"
	// observable in the tool_result.
	chatClonePushGit(t, base, "init", "-q", "--bare", "--initial-branch="+chatClonePushBranch, origin)
	chatClonePushGit(t, base, "clone", "-q", origin, mainCheckout)
	chatClonePushGit(t, mainCheckout, "config", "user.email", "t@t")
	chatClonePushGit(t, mainCheckout, "config", "user.name", "t")
	chatClonePushGit(t, mainCheckout, "commit", "-q", "--allow-empty", "-m", "seed")
	chatClonePushGit(t, mainCheckout, "push", "-q", "origin", chatClonePushBranch)
	// WOUND BACK BY ONE — see this function's doc. Origin stays at "seed", so the
	// shared checkout has exactly one commit its push can send and the clone below
	// has none.
	chatClonePushGit(t, mainCheckout, "commit", "-q", "--allow-empty", "-m", chatClonePushAheadSubject)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// `git clone` (not init) — a clone's `.git` is a DIRECTORY, which is the ONE
	// discriminator the criterion and the publish guard both read.
	chatClonePushGit(t, base, "clone", "-q", origin, ground)
	chatClonePushGit(t, ground, "config", "user.email", "t@t")
	chatClonePushGit(t, ground, "config", "user.name", "t")
	return root, ground, mainCheckout
}

// chatClonePushRun drives ONE chat turn whose only tool call is `command`, started
// in dir, with the workspace root pinned, and returns the run's stdout, stderr and
// the session record.
//
// THE COMMAND IS A PARAMETER, not the package constant. It used to be the constant
// alone, so the `explicit-delivery` subtest below drove the OPERATOR spelling twice
// and its `HEAD:master` half was never executed at all — the fixture's own mock
// was the only thing deciding what ran. Rule 31's whole claim is that a surface is
// measured by executing it, so the spelling a subtest names is the spelling sent.
func chatClonePushRun(t *testing.T, command, dir, workspaceRoot string) (string, string, *testutil.Session) {
	t.Helper()
	home := t.TempDir()
	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_01_push",
			Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": command},
		}},
		testutil.Resp{Text: chatClonePushAnswer},
	)
	defer llm.Close()

	// MockEnv is the ONE script that decides a mocked run's environment (it strips
	// every GOPI_* var), so the workspace root is appended HERE and nowhere else.
	env := chatClonePushPinRoot(testutil.MockEnv(t, home, llm.URL()), workspaceRoot)

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--tools", chatGroundPolicyArg, "-p", "publish this run's commit: " + command},
		"", env, dir)
	if code != 0 {
		t.Fatalf("`gopi chat --once --tools` exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	return stdout, stderr, testutil.LoadSession(t, home)
}

// chatClonePushBashResults returns the tool_result text of every bash tool_use the
// session recorded, in order.
func chatClonePushBashResults(t *testing.T, sess *testutil.Session) []string {
	t.Helper()
	ids := map[string]bool{}
	for _, tu := range sess.ToolUses() {
		if tu.Name == toolnames.ToolBash {
			ids[tu.ID] = true
		}
	}
	if len(ids) == 0 {
		t.Fatalf("the session recorded no bash tool_use — the turn never asked for the push")
	}
	var out []string
	for _, ev := range sess.Events {
		if ev.Type != "tool_result" {
			continue
		}
		for _, b := range chatClonePushContentBlocks(ev) {
			id, _ := b["tool_use_id"].(string)
			if !ids[id] {
				continue
			}
			out = append(out, toolResultText(b))
		}
	}
	if len(out) == 0 {
		t.Fatalf("the session recorded a bash tool_use with no tool_result")
	}
	return out
}

// chatClonePushContentBlocks extracts message.content[] blocks from a session
// event — the SAME shape internal/testutil reads internally (its contentBlocks is
// unexported, and Rule 13/14 says a shared reader has one home; this is a
// two-line structural read of the record the fixture already holds, not a second
// definition of the session format).
func chatClonePushContentBlocks(ev testutil.Event) []map[string]interface{} {
	m, ok := ev.Raw["message"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := m["content"].([]interface{})
	if !ok {
		return nil
	}
	var out []map[string]interface{}
	for _, item := range raw {
		if b, ok := item.(map[string]interface{}); ok {
			out = append(out, b)
		}
	}
	return out
}

// chatClonePushPinRoot pins the CHANNEL's workspace root for the run.
//
// BOTH channels are written, and that is not belt-and-braces: the workspace root
// resolves through worktreeroots.Resolve, whose FIRST branch is
// GOPI_WORKTREE_ROOT — MockEnv always sets it (per test, so a fixture never
// writes into the operator's /home/user/workspace), and a lone GOPI_WORKSPACE_ROOT would
// therefore be overruled by the environment the fixture itself handed the child.
// Measured: with only GOPI_WORKSPACE_ROOT set the run read the MockEnv worktree
// root as the workspace root and the clone ground fell outside it
// (shape=false). The earlier entries are dropped so the child sees one value per
// name — a duplicate key in exec.Cmd.Env leaves the winner to the kernel's
// getenv order, which is not something a fixture should depend on.
func chatClonePushPinRoot(env []string, workspaceRoot string) []string {
	pinned := []string{
		config.WorkspaceRootEnvName + "=" + workspaceRoot,
		worktreeroots.WorktreeRootEnvName + "=" + workspaceRoot,
	}
	keys := []string{config.WorkspaceRootEnvName, worktreeroots.WorktreeRootEnvName}
	out := make([]string, 0, len(env)+len(pinned))
	for _, kv := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return append(out, pinned...)
}

// TestChatCloneGroundPushIsAdmittedAndTheSameLineInTheMainCheckoutIsRefused is
// the Rule 31 run: one command line, two directories, two opposite decisions,
// both read off the REAL chat run's own session record. It runs the operator's
// exact spelling AND the explicit one, because the defect had two halves: the
// explicit `HEAD:master` delivery and the bare `git push origin` were both
// refused.
//
// THE FIXTURE IS BUILT PER SUBTEST, and that is a correctness requirement rather
// than hygiene: ② really pushes, so it MOVES origin's master to the shared
// checkout's HEAD, and a second subtest reusing that origin would run its ground
// push against a server already ahead of it — git would answer `! [rejected]
// (non-fast-forward)` and the arm would fail for a reason that has nothing to do
// with the chat ops category.
func TestChatCloneGroundPushIsAdmittedAndTheSameLineInTheMainCheckoutIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, command string }{
		{"operator-spelling", chatClonePushCommand},
		{"explicit-delivery", "git push origin HEAD:master"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, ground, mainCheckout := chatClonePushFixture(t)
			chatClonePushBothSides(t, tc.command, root, ground, mainCheckout)
		})
	}
}

// chatClonePushBothSides drives the same command line twice: in the clone ground
// and in the shared main checkout.
//
// WHAT EACH SIDE NOW ASSERTS, AND WHY IT IS STILL WORTH ASSERTING.
//
// Both sides must RUN — and "ran" is proven by the WORK, not by the absence of a
// refusal string. An admitted-and-swallowed push and a real one both leave the
// tool_result free of `CHAT OPS CATEGORY`, so the earlier arm's negative test could
// not tell them apart; the trees do. The shared checkout carries one commit origin
// lacks, so git answers its push with the ref update `… -> master`; the ground was
// cloned at origin's tip, so its push has nothing to send and can only answer
// `Everything up-to-date`. Each marker is therefore EXCLUSIVE to its side, and an
// arm that swapped the two directories' expectations would fail.
//
// The ground half additionally checks the SERVER: after the turn, origin's master
// is the commit the ground stands on. That is the end-to-end claim the epic exists
// for — a console finishing a row's delivery is observable at the remote, not in a
// log line.
//
// Nothing here was weakened to buy green: the semantic this file guards ("a chat's
// push really executes in its own directory") is measured MORE strictly than
// before, because a refusal string is no longer standing in for the effect.
func chatClonePushBothSides(t *testing.T, command, root, ground, mainCheckout string) {
	t.Helper()

	// ① the CLONE GROUND: the delivery push runs.
	groundHead := chatClonePushGit(t, ground, "rev-parse", "HEAD")
	_, groundStderr, groundSess := chatClonePushRun(t, command, ground, root)
	groundResults := chatClonePushBashResults(t, groundSess)
	if refused, result := chatClonePushRefused(groundResults); refused {
		t.Fatalf("%q in the clone ground %s was REFUSED by the chat ops category — this is the "+
			"defect the epic exists for: a console cannot finish a failed row's delivery, and its commit "+
			"dies with the clone.\n--- tool_result ---\n%s\n--- stderr ---\n%s", command, ground, result, groundStderr)
	}
	upToDate, _ := chatClonePushResultWith(groundResults, chatClonePushUpToDateMarker)
	if !upToDate {
		t.Fatalf("%q in the clone ground %s was admitted but produced none of git's own answers "+
			"(%q for a branch already at origin's tip): the command did not run.\n--- tool_results ---\n%s\n"+
			"--- stderr ---\n%s", command, ground, chatClonePushUpToDateMarker,
			strings.Join(groundResults, "\n---\n"), groundStderr)
	}
	// And the ground's branch is on origin AT THE COMMIT IT STANDS ON: the push
	// reached the server, which a swallowed or no-op call cannot fake.
	remote := chatClonePushGit(t, ground, "ls-remote", "origin", "refs/heads/"+chatClonePushBranch)
	if !strings.HasPrefix(remote, groundHead) {
		t.Fatalf("origin's %s = %q, want the ground's HEAD %s — the ground's push did not reach the server",
			chatClonePushBranch, remote, groundHead)
	}

	// ② the SAME line in the repo's MAIN CHECKOUT: admitted, and it really ran.
	mainHead := chatClonePushGit(t, mainCheckout, "rev-parse", "HEAD")
	_, mainStderr, mainSess := chatClonePushRun(t, command, mainCheckout, root)
	mainResults := chatClonePushBashResults(t, mainSess)
	if refused, result := chatClonePushRefused(mainResults); refused {
		t.Fatalf("%q in the shared main checkout %s was REFUSED by the chat ops category — a push is "+
			"REPOSITORY WORK by the operator's 2026-11-03 ruling (\"push 都可以\", row 01a1a786) and the "+
			"2026-11-05 ruling (eb1b36d1) admits the REF MOVE class as well, so no chat ops class refuses "+
			"a push any more. A refusal here means a gate came back.\n--- tool_result ---\n%s\n--- stderr ---\n%s",
			command, mainCheckout, result, mainStderr)
	}
	sentRef, result := chatClonePushResultWith(mainResults, chatClonePushRefUpdateMarker)
	if !sentRef {
		t.Fatalf("%q in the shared main checkout %s was admitted but never sent a ref (no %q in any "+
			"tool_result): the command was swallowed rather than run.\n--- tool_results ---\n%s\n--- stderr ---\n%s",
			command, mainCheckout, chatClonePushRefUpdateMarker, strings.Join(mainResults, "\n---\n"), mainStderr)
	}
	remote = chatClonePushGit(t, mainCheckout, "ls-remote", "origin", "refs/heads/"+chatClonePushBranch)
	if !strings.HasPrefix(remote, mainHead) {
		t.Fatalf("origin's %s = %q, want the shared checkout's HEAD %s (%q was admitted and reported "+
			"a ref update, so the server should hold it):\n%s", chatClonePushBranch, remote, mainHead,
			chatClonePushRefUpdateMarker, result)
	}
}

// chatClonePushRefused reports whether any recorded bash tool_result carries the
// category refusal, and the first one that does.
func chatClonePushRefused(results []string) (bool, string) {
	for _, result := range results {
		if strings.Contains(result, chatClonePushRefusalMarker) {
			return true, result
		}
	}
	return false, ""
}

// chatClonePushResultWith reports whether any recorded bash tool_result carries
// want, and the first one that does.
func chatClonePushResultWith(results []string, want string) (bool, string) {
	for _, result := range results {
		if strings.Contains(result, want) {
			return true, result
		}
	}
	return false, ""
}
