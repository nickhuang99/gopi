package agent

// chat_write_boundary_test.go — the chat write boundary, both halves, asserted
// against the surfaces a chat turn actually reaches:
//
//  1. the PROMPT each chat path sends (chatSystemPromptServed /
//     chatOpsSystemPrompt — the composition points the request builders call),
//     including the two limbs and the task prompt's freedom from both;
//  2. the MECHANISM at the ONE dispatch point a tool call passes through
//     (Agent.executeToolDefault) — with the RED half: the same call, outside a
//     chat turn, still runs (a worker's deliverable IS `git add`/`commit`), and
//     the fixture's HEAD is really rewritten in that direction.
//
// The repo under test is a FIXTURE repository (a fresh `git init` in t.TempDir()
// made the process cwd) rather than this checkout: a guard that refused nothing
// would be invisible in the repo under test, where the paths it protects are
// also the paths under the test's own feet.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatBoundaryFixtureRepo makes a real repository with one committed file and
// makes it the process cwd, so chatRepoRoot (the resolver the write arm uses)
// answers with IT. It returns the repo root.
func chatBoundaryFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Skipf("git init unavailable in this environment: %v: %s", err, out)
	}
	run("config", "user.email", "chat-boundary@test.invalid")
	run("config", "user.name", "chat boundary test")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "devdoc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devdoc", "seed.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed.txt", "devdoc/seed.md")
	run("commit", "-m", "seed")
	t.Chdir(dir)
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	return resolved
}

// gitPorcelain / gitHead read the fixture's own state — the observation the
// refusal claims ("nothing happened") must be a measurement, not an assumption.
func gitPorcelain(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status --porcelain: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestChatTurnRefusesGitMutationAndTheRepoIsUntouched is the acceptance case
// behind the rule: a chat turn that reaches for `git commit` gets the refusal
// INSTEAD of a commit, and the repository's observed state (HEAD + porcelain) is
// byte-identical across the call.
func TestChatTurnStopsAGitMutationAndTheRepoIsUntouched(t *testing.T) {
	repo := chatBoundaryFixtureRepo(t)
	before, dirtyBefore := gitHead(t, repo), gitPorcelain(t, repo)
	if dirtyBefore != "" {
		t.Fatalf("precondition: the fixture must start clean, porcelain=%q", dirtyBefore)
	}

	// THE DEFAULT BOUNDARY ASKS, IT DOES NOT ASSERT A VERB. This run resolved no
	// rule table, so the frozen posture (on-request) answers — and the answer is
	// the APPROVAL text: the command did not run, the model is told to ask its
	// operator. The proof that it did not run is the fixture, not the prose.
	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "git commit --allow-empty -m chat-was-here", "workdir": repo},
	})
	if !strings.Contains(out, "NEEDS OPERATOR APPROVAL") {
		t.Fatalf("the chat turn's `git commit` was not stopped for approval, got:\n%s", out)
	}
	// And it is NOT a refusal: the model must be able to tell the two apart.
	if strings.Contains(out, "Refused by the CHAT WRITE BOUNDARY") {
		t.Errorf("with the shipped on-request posture the answer must be an APPROVAL request, not a refusal:\n%s", out)
	}

	if got := gitHead(t, repo); got != before {
		t.Fatalf("the stopped command still moved HEAD: before=%s after=%s", before, got)
	}
	if got := gitPorcelain(t, repo); got != "" {
		t.Fatalf("the stopped command left the fixture dirty: %q", got)
	}
}

// TestWorkerTurnStillRunsGitMutations is the RED half, and the reason the guard
// is gated on a.ChatMode rather than on the command alone: a task run's
// deliverable IS commits in its own worktree, so the SAME call must still run.
// The fixture HEAD is really rewritten — the direction is proven, not asserted.
func TestWorkerTurnStillRunsGitMutations(t *testing.T) {
	repo := chatBoundaryFixtureRepo(t)
	before := gitHead(t, repo)

	a := &Agent{ChatMode: false}
	out := a.executeToolDefault(deepseek.ToolCall{
		Name: toolnames.ToolBash,
		Arguments: map[string]interface{}{
			"command": "git add -A && git commit --allow-empty -m worker-was-here",
			"workdir": repo,
		},
	})
	if strings.Contains(out, "CHAT WRITE BOUNDARY") {
		t.Fatalf("the WORKER path was refused by the chat boundary — a task run must be able to commit:\n%s", out)
	}
	if got := gitHead(t, repo); got == before {
		t.Fatalf("the worker-shaped call did not commit (HEAD still %s) — this test is not proving the red direction:\n%s", got, out)
	}
}

// TestChatTurnRefusesWriteIntoATrackedTree covers the second arm at the same
// dispatch point: a SOURCE path is NOT written, and the same write to a path
// outside every checkout IS performed — so the arm stops the deposit, not writing.
//
// WHAT THE ANSWER IS NOW DEPENDS ON THE RULE TABLE, WHICH IS THE POINT. This arm
// used to read a compiled tree list plus the git index; it now consumes the same
// frozen (chatpolicy.Policy, posture) pair the bash arm does, so with NO rule
// table resolved the answer is the shipped posture's: `prompt *` in the built-in
// defaults → the APPROVAL request, not a refusal. Both shapes are asserted here —
// a project rule that FORBIDS the path is a refusal, and the default posture is a
// question — because either one alone would leave a hole: an arm that refused
// everything would pass the forbidden case, and one that asked everything would
// pass the default one. The filesystem is the proof in both directions.
func TestChatTurnRefusesWriteIntoATrackedTree(t *testing.T) {
	repo := chatBoundaryFixtureRepo(t)
	srcPath := filepath.Join(repo, "seed.txt")
	defer tools.ResetChatPolicyToDefaults()

	// (1) A project rule that FORBIDS the path: refused, unchanged on disk.
	pol, err := chatpolicy.Parse("test chat-policy", "```chat-policy\nforbidden write seed.txt\nprompt write *\n```\n")
	if err != nil {
		t.Fatalf("the test's own rule table does not parse: %v", err)
	}
	tools.SetChatPolicy(pol, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, "test"))

	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolWrite,
		Arguments: map[string]interface{}{"path": srcPath, "content": "a chat's source edit\n"},
	})
	if !strings.Contains(out, "CHAT WRITE BOUNDARY") {
		t.Fatalf("a `forbidden write seed.txt` rule did not refuse the write, got:\n%s", out)
	}
	if got, err := os.ReadFile(srcPath); err != nil || string(got) != "seed\n" {
		t.Fatalf("the refusal still rewrote the tracked file: content=%q err=%v", got, err)
	}
	if got := gitPorcelain(t, repo); got != "" {
		t.Fatalf("the refused write left the fixture dirty: %q", got)
	}

	// (2) NO rule mentions the path and the posture is on-request: the SAME call
	// is a question for the operator — not run, not refused, and the file is
	// still untouched, which is what makes it a stop rather than a failure to
	// write.
	// An EMPTY table (an operator's fence with no rules): nothing matches, so the
	// posture answers — exactly the shipped `prompt *` default's behaviour.
	empty, err := chatpolicy.Parse("test chat-policy", "```chat-policy\n# only a comment\n```\n")
	if err != nil {
		t.Fatalf("the empty rule table does not parse: %v", err)
	}
	tools.SetChatPolicy(empty, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, "test"))
	out = a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolWrite,
		Arguments: map[string]interface{}{"path": srcPath, "content": "a chat's source edit\n"},
	})
	if !strings.Contains(out, "[NEEDS OPERATOR APPROVAL") {
		t.Fatalf("with no matching rule the on-request posture must ask, got:\n%s", out)
	}
	if strings.Contains(out, "Refused by the CHAT WRITE BOUNDARY") {
		t.Fatalf("prompt collapsed into a refusal:\n%s", out)
	}
	if got, err := os.ReadFile(srcPath); err != nil || string(got) != "seed\n" {
		t.Fatalf("the stopped write still rewrote the file: content=%q err=%v", got, err)
	}

	// (3) The model was told YES: a rule that ALLOWS the session-directory
	// destination makes the very same call run and the file appear. The location
	// is no longer a special case in code — it is a target like any other, which
	// is the whole point of the arm becoming rule-driven — and the rule (not the
	// arm) is what turns the question into a write.
	sessionPath := filepath.Join(t.TempDir(), tools.ChatSessionArtifactsDirName, "notes.md")
	allowAll, err := chatpolicy.Parse("test chat-policy", "```chat-policy\nallow write *\n```\n")
	if err != nil {
		t.Fatalf("the allow rule table does not parse: %v", err)
	}
	tools.SetChatPolicy(allowAll, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, "test"))
	legal := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolWrite,
		Arguments: map[string]interface{}{"path": sessionPath, "content": "a chat artifact\n"},
	})
	if strings.Contains(legal, "CHAT WRITE BOUNDARY") {
		t.Fatalf("an `allow write *` rule did not let the write through, got:\n%s", legal)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("the allowed write created nothing at %s: %v", sessionPath, err)
	}
}

// TestChatPromptsCarryTheWriteBoundary is the prompt half: BOTH chat paths send
// the boundary, each with the limb that is TRUE of it — and the task prompt
// carries NEITHER (a worker is told nothing about a console's boundary).
func TestChatPromptsCarryTheWriteBoundary(t *testing.T) {
	chatBoundaryFixtureRepo(t)

	plain := chatSystemPromptServed(nil)
	if !strings.Contains(plain, chatWriteBoundaryMarker) {
		t.Fatalf("the plain chat prompt carries no write boundary:\n%s", plain)
	}
	if !strings.Contains(plain, chatWriteBoundaryNoToolsLimb) {
		t.Errorf("the plain chat prompt must carry the NO-TOOLS limb (it has no tools on the wire)")
	}
	if strings.Contains(plain, chatWriteBoundaryToolsLimb) {
		t.Errorf("the plain chat prompt carries the TOOLS limb — a false instruction: nothing on that path can write")
	}

	toolsPath := chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()})
	if !strings.Contains(toolsPath, chatWriteBoundaryMarker) {
		t.Fatalf("the --tools chat prompt carries no write boundary:\n%s", toolsPath)
	}
	if !strings.Contains(toolsPath, chatWriteBoundaryToolsLimb) {
		t.Errorf("the --tools chat prompt must carry the TOOLS limb (write/edit/bash are on its wire)")
	}
	if strings.Contains(toolsPath, chatWriteBoundaryNoToolsLimb) {
		t.Errorf("the --tools chat prompt carries the NO-TOOLS limb — a false instruction")
	}

	if task := assembleSystemPrompt(); strings.Contains(task, chatWriteBoundaryMarker) {
		t.Fatalf("the TASK prompt carries the chat write boundary — a worker's deliverable IS commits in its own worktree:\n%s", task)
	}
}

// TestChatWriteBoundaryTeachesTheThreeAnswersAndTheRoute is the prompt's own
// contract now that the mechanism decides from the OPERATOR's rule table rather
// than from a compiled verb list: the paragraph must teach the THREE answers of a
// policy (allow / prompt / forbidden) as DIFFERENT moves, because a model that
// cannot tell "ask" from "refused" either stops asking or asks about banned work.
//
// The old assertion — "the prompt names every verb the enforcement refuses" — is
// gone with the list it read: there is no compiled list any more, so a prompt that
// still rendered one would be teaching a boundary the guard does not hold. What
// replaces it is the requirement that the prompt state where the judgement COMES
// FROM (the fence + the frozen posture) and that the allowance's own shape is
// still spelled out.
func TestChatWriteBoundaryTeachesTheThreeAnswersAndTheRoute(t *testing.T) {
	chatBoundaryFixtureRepo(t)
	block := chatContextRulesFor(true)

	// The three answers, by the words a rule line uses (one vocabulary: the
	// decisions' own spellings, not a paraphrase typed here).
	for _, decision := range []chatpolicy.Decision{
		chatpolicy.DecisionAllow, chatpolicy.DecisionPrompt, chatpolicy.DecisionForbidden,
	} {
		if !strings.Contains(block, "`"+decision.String()+"`") {
			t.Errorf("the prompt does not teach the %q answer — the model cannot obey a three-valued boundary it reads as two:\n%s",
				decision.String(), block)
		}
	}
	// The prompt must tell the model what PROMPT means — and the ROUTE depends on
	// whether anyone can answer (chatPromptAnswerRoute). On an ATTENDED console
	// that is "ask the operator"; a run that declared no operator must not be told
	// to ask, because the same prompt's unattended block calls asking its failure
	// shape (both arms are pinned by
	// TestChatWriteBoundaryPromptDecisionRouteFollowsWhoIsReading).
	if !strings.Contains(block, chatPromptAnswerRoute()) {
		t.Errorf("the prompt does not carry the `%s` answer's own route:\n%s",
			chatpolicy.DecisionPrompt.String(), block)
	}
	// And where the judgement comes from: the fence and the frozen posture.
	if !strings.Contains(block, "chat-policy") {
		t.Errorf("the prompt does not name the operator's rule fence:\n%s", block)
	}
	pol, posture := tools.ChatPolicyFrozen()
	if !strings.Contains(block, posture.String()) {
		t.Errorf("the prompt does not state this run's posture %q — the sentence and the mechanism would disagree:\n%s",
			posture.String(), block)
	}
	if !strings.Contains(block, fmt.Sprint(pol.Len())) {
		t.Errorf("the prompt does not state the rule count (%d):\n%s", pol.Len(), block)
	}

	// The allowance's shape is still taught, because the write/edit arm and the
	// staging recipe do not come from the rule table.
	if !strings.Contains(block, "`"+chatGitCommandWord+" "+chatGitStageVerbWord+" ") {
		t.Errorf("the prompt does not teach the staging shape `%s %s <file>`:\n%s",
			chatGitCommandWord, chatGitStageVerbWord, block)
	}
	if !strings.Contains(block, tools.ChatGitMessageFlag) {
		t.Errorf("the prompt does not teach the commit shape (the %s flag):\n%s", tools.ChatGitMessageFlag, block)
	}
	if !strings.Contains(block, chatAllowedWriteRoot()) {
		t.Errorf("the prompt does not name the tree the allowance permits (%s):\n%s", chatAllowedWriteRoot(), block)
	}
	if strings.Contains(block, "is a task and not an edit") && !strings.Contains(block, "CODE change") {
		t.Errorf("the prompt must still route a code change to a task:\n%s", block)
	}
	if !strings.Contains(block, "gopi "+chatTaskSubmitRoute()) {
		t.Errorf("the prompt does not teach the route `gopi %s`:\n%s", chatTaskSubmitRoute(), block)
	}
	if !strings.Contains(block, tools.ChatSessionArtifactsDirName) {
		t.Errorf("the prompt does not name the artifact destination (%s):\n%s", tools.ChatSessionArtifactsDirName, block)
	}
	// The destination is named RELATIVE to the run's own session directory, never
	// as a hard-coded home path (the session dir is per-run and relocatable).
	if strings.Contains(block, "/home/") || strings.Contains(block, "/home/user/.gopi") {
		t.Errorf("the boundary hard-codes a session path instead of pointing at [CURRENT SESSION]:\n%s", block)
	}
}

// TestChatWriteBoundaryPromptDecisionRouteFollowsWhoIsReading pins the ONE fact
// the prompt and the guard must read the same way: whether anybody can answer a
// `prompt` decision.
//
// THE CONTRADICTION THIS CASE EXISTS AGAINST (measured 2026-10-22): the boundary
// said "you STOP and ask the operator in the conversation … retry only after they
// say yes" while the SAME system prompt carried chatUnattendedFailureShape for a
// run with no reader — "ending by ASKING is the failure shape". A model cannot
// obey both paragraphs, and the tool result it actually receives was already the
// non-asking one (internal/tools/chat_operator_presence.go). One fact, two
// readings, so the readings are asserted together.
func TestChatWriteBoundaryPromptDecisionRouteFollowsWhoIsReading(t *testing.T) {
	chatBoundaryFixtureRepo(t)

	tools.SetChatOperatorAbsent(true)
	t.Cleanup(func() { tools.SetChatOperatorAbsent(false) })
	unattended := chatContextRulesFor(true)
	if strings.Contains(unattended, "ask the operator in the conversation") {
		t.Fatalf("an UNATTENDED run's prompt still tells it to ask the operator and retry after they say yes — "+
			"nobody can answer, and the same prompt calls asking the failure shape:\n%s", unattended)
	}
	for _, want := range []string{"NO OPERATOR IS READING THIS RUN", cliverbs.PolicyFlag} {
		if !strings.Contains(unattended, want) {
			t.Errorf("the unattended boundary does not name %q — the run is left with no route at all:\n%s", want, unattended)
		}
	}

	tools.SetChatOperatorAbsent(false)
	attended := chatContextRulesFor(true)
	if !strings.Contains(attended, "ask the operator in the conversation") {
		t.Fatalf("the ATTENDED boundary lost its asking sentence — there a human really is there to answer, "+
			"and the operator's reply is the approval channel:\n%s", attended)
	}
}
