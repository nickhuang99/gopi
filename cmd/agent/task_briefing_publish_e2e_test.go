package main

// task_briefing_publish_e2e_test.go — DOES THE PUBLISH CLAUSE REACH THE CHILD,
// AND ONLY ON THE CLONE CHANNEL? (task 01a1604a; the machine half of the same
// defect is internal/tools/git_publish_target_guard.go.)
//
// WHY A REAL RUN AND NOT A UNIT TEST. The clause is prose whose only reader is
// the MODEL, and the model reads it through exactly one channel: the briefing
// bound to the child's stdin (cmd.Stdin) at the spawn seam. A unit test proves
// the STRING; it cannot prove the STRING IS DELIVERED, and a clause appended to
// a variable that never reaches the child is precisely this seam's measured
// failure mode (`briefing, x := …` shadowing, task 01a14242 — the seam printed
// "NAMED at dispatch" for a clause the child never received).
//
// So this walk asserts on the CHILD'S OWN FIRST MODEL REQUEST (mock body 1),
// which is the only surface that can distinguish "appended" from "delivered".
//
// TWO ARMS, AND THE SECOND IS THE CONSTRAINT ON THE FIRST:
//
//	① CLONE CHANNEL ON: body 1 carries the clause's LABEL and the ground's
//	   branch — the run is told the delivery loop publishes, before its first
//	   tool round.
//	② CLONE CHANNEL OFF (linked worktree): body 1 does NOT carry the label.
//	   Rule 6c's worktree half is a different instruction ("the commit is
//	   already on the branch; you do not push"), and a run told the clone rule
//	   would be told about a channel it is not in. The scope decision lives at
//	   the seam (cli_tasks.go: `if cloneRun != nil`), so this arm is what keeps
//	   the clause from leaking to the worktree path.
//
// WHAT THIS WALK DOES NOT CLAIM. It does not measure the model's BEHAVIOUR
// (whether it then refrains from pushing) — a scripted child does what its
// script says. That half is the guard's, and it is proven where it lives
// (internal/tools/git_publish_target_guard_test.go, plus the real-binary run in
// that task's report). What is claimed here is the precondition the prose
// section rests on: the run that is told, is actually told.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// TestPublishClauseReachesTheCloneChannelChildAndOnlyThere is the delivery walk
// described in the file header.
func TestPublishClauseReachesTheCloneChannelChildAndOnlyThere(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := cloneGateRepo(t)

	// ── ① CLONE CHANNEL ON ──────────────────────────────────────────────────
	root := filepath.Join(t.TempDir(), "workspace")
	onTask := "01a1604a-0000-7000-8000-0000000000a1"
	onMock := testutil.NewMockLLM(t, testutil.Resp{Text: "did the work — done"})
	t.Cleanup(onMock.Close)
	onEnv := cloneGateEnv(t, repo, `"dev_repo":"`+repo+`","workspace_root":"`+root+`"`)
	onEnv = replaceMockURL(t, onEnv, onMock.URL())

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"publish clause on", "--payload", "do the work", "--task-id", onTask,
		"--repo", repo}, "", onEnv, ""); code != 0 {
		t.Fatalf("submit (on): code=%d stderr=%s", code, stderr)
	}
	if out, errOut, code := runGopiMapped(t, []string{"worker", "--once"}, "", onEnv, repo); code != 0 {
		t.Fatalf("worker --once (clone channel): code=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	body := onMock.RequestBody(1)
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the mock served no request body — the child never reached its first model round, so delivery cannot be measured")
	}
	if !strings.Contains(body, tools.TaskBriefingPublishLabel) {
		t.Fatalf("the publish clause must reach the CLONE-CHANNEL child's FIRST model request — %q is absent.\n"+
			"The seam's own log cannot distinguish appended from delivered (task 01a14242), so this asserts the bytes the model actually received:\n%s",
			tools.TaskBriefingPublishLabel, briefingPublishExcerpt(body))
	}
	// The SUBJECT: the ground's branch. Without it the clause says "do not push
	// to some other ref" and never says which ref is the one the loop pushes.
	if !strings.Contains(body, "This ground is a CLONE standing on branch ") {
		t.Fatalf("the clause must name the clone ground's SHAPE in the delivered briefing:\n%s", briefingPublishExcerpt(body))
	}
	if !strings.Contains(body, "task/01a1604a") && !strings.Contains(body, "master") {
		t.Fatalf("the clause must name the ground's BRANCH in the delivered briefing (the clone channel stands on the branch it cloned):\n%s",
			briefingPublishExcerpt(body))
	}
	// THE TIMING SENTENCE, measured on the BYTES THE MODEL RECEIVED (task
	// 01a1792e). The clause's first spelling said the loop pushes "after your
	// round, every time"; the leg is latched to ONE attempt per run and fires
	// from the END hook. A run that believes the retired sentence watches a push
	// that cannot come and reports a working channel as broken — measured on row
	// 01a17921-fa90, whose own log ends `push accepted`. Asserting on the
	// delivered body (not on the constant) is what makes this a check of the
	// RENDERED briefing, i.e. of what actually reached the child.
	if !strings.Contains(body, "ONCE PER RUN") || !strings.Contains(body, "END hook") {
		t.Fatalf("the delivered briefing must state the delivery loop's REAL trigger (once per run, from the END hook) — a run told otherwise reports a working channel as broken:\n%s",
			briefingPublishExcerpt(body))
	}
	if strings.Contains(body, "after "+"your round") {
		t.Fatalf("the RETIRED per-round wording reached the model — it is precisely what produced the false defect report on row 01a17921-fa90:\n%s",
			briefingPublishExcerpt(body))
	}

	// ── ② CLONE CHANNEL OFF (the linked-worktree path) ──────────────────────
	//
	// The SAME walk with dev_repo absent: the run is a linked worktree of the
	// repo, where Rule 6c's FIRST half applies ("the commit is already on the
	// branch in the shared repo — you do not push") and the clone-channel
	// instruction would be about a channel this run is not in. The scope
	// decision is the seam's (`if cloneRun != nil`, cmd/agent/cli_tasks.go);
	// this arm is what keeps a later refactor from rendering the clause on
	// every path.
	offTask := "01a1604a-0000-7000-8000-0000000000b2"
	offMock := testutil.NewMockLLM(t, testutil.Resp{Text: "did the work — done"})
	t.Cleanup(offMock.Close)
	offEnv := replaceMockURL(t, cloneGateEnv(t, repo, ""), offMock.URL())
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"publish clause off", "--payload", "do the work", "--task-id", offTask,
		"--repo", repo}, "", offEnv, ""); code != 0 {
		t.Fatalf("submit (off): code=%d stderr=%s", code, stderr)
	}
	if out, errOut, code := runGopiMapped(t, []string{"worker", "--once"}, "", offEnv, repo); code != 0 {
		t.Fatalf("worker --once (worktree channel): code=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	offBody := offMock.RequestBody(1)
	if strings.TrimSpace(offBody) == "" {
		t.Fatalf("the OFF arm's mock served no request body — nothing was measured, so the absence assertion below would be vacuous")
	}
	if strings.Contains(offBody, tools.TaskBriefingPublishLabel) {
		t.Fatalf("the publish clause is the CLONE CHANNEL's, yet it reached a LINKED-WORKTREE run's first request.\n"+
			"Rule 6c's worktree half is a different instruction, so this clause would be telling the run about a channel it is not in:\n%s",
			briefingPublishExcerpt(offBody))
	}
	// The OFF arm is not a vacuous walk: the ground announcement — which IS
	// rendered on both channels — must be present, so "the clause is absent"
	// cannot be satisfied by a briefing that never arrived at all.
	if !strings.Contains(offBody, tools.TaskGroundAnnouncementLabel) {
		t.Fatalf("the OFF arm's briefing must still carry the ground announcement (%q) — without it this walk proves nothing about the publish clause's scope:\n%s",
			tools.TaskGroundAnnouncementLabel, briefingPublishExcerpt(offBody))
	}

	// ── ③ THE REAL BINARY, THE REAL GUARD (Rule 31) ─────────────────────────
	//
	// The two arms above prove the PROSE arrives. This one proves the two halves
	// agree when the run does the measured thing anyway: the child is scripted to
	// execute the incident's own command through the REAL bash tool
	// (`git push origin HEAD:refs/heads/task/x`), and the assertions are on the
	// TOOL RESULT the model received (its bytes are in the next request body) and
	// on the SERVER's ref list — not on a log line. A guard that refused after
	// the write would pass the first and fail the second, which is the difference
	// this arm exists to measure.
	pushTask := "01a1604a-0000-7000-8000-0000000000c3"
	pushRoot := filepath.Join(t.TempDir(), "workspace")
	pushMock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_publish",
			Name: "bash",
			Args: map[string]interface{}{"command": "git push origin HEAD:refs/heads/task/x"},
		}},
		testutil.Resp{Text: "the push was refused — reporting it, not retrying it"},
	)
	t.Cleanup(pushMock.Close)
	pushEnv := replaceMockURL(t, cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+pushRoot+`"`), pushMock.URL())
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"publish guard e2e", "--payload", "attempt the publish", "--task-id", pushTask,
		"--repo", repo}, "", pushEnv, ""); code != 0 {
		t.Fatalf("submit (guard): code=%d stderr=%s", code, stderr)
	}
	pushOut, pushErr, pushCode := runGopiMapped(t, []string{"worker", "--once"}, "", pushEnv, repo)
	t.Logf("worker --once (guard arm) exit=%d\nstdout:\n%s\nstderr:\n%s", pushCode, pushOut, pushErr)

	// ③a THE TOOL RESULT THE MODEL RECEIVED carries the guard's refusal, in the
	// guard's own words. Body 2 is the request that follows the tool round, so
	// its tool_result block is the bash tool's answer verbatim.
	toolRound := pushMock.RequestBody(2)
	if !strings.Contains(toolRound, "clone-channel run publishes its work through the channel") {
		t.Fatalf("the bash tool must have REFUSED the incident command and told the model so.\n"+
			"Body 2 (the request after the tool round) does not carry the refusal — the guard did not fire, or its reason never reached the run:\n%s",
			briefingPublishExcerpt(toolRound))
	}
	// ③b THE SERVER'S REFS are the ACTUAL evidence: a refusal that ran after the
	// write would leave `task/x` behind and this is where that shows.
	headsCmd := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname)", "refs/heads")
	headsOut, headsErr := headsCmd.CombinedOutput()
	if headsErr != nil {
		t.Fatalf("read the fixture server's refs: %v\n%s", headsErr, headsOut)
	}
	if strings.Contains(string(headsOut), "refs/heads/task/x") {
		t.Fatalf("the guarded push WROTE refs/heads/task/x on the server — the refusal ran after the write:\n%s", headsOut)
	}
	if !strings.Contains(string(headsOut), "refs/heads/master") {
		t.Fatalf("the fixture server lost its integration branch (refs were: %s) — nothing about this arm is measurable then", headsOut)
	}
	t.Logf("server heads after the guarded push: %s", strings.Join(strings.Fields(string(headsOut)), " "))
}

// replaceMockURL points an env slice's model endpoint at mock. cloneGateEnv
// mints its OWN mock internally, so a case that needs to read the bodies must
// swap the endpoint for the mock it holds. Both alias pairs are rewritten
// (ANTHROPIC_*/DEEPSEEK_*, the same two MockEnv writes, because the client
// resolves one pair or the other — claude_client.go).
func replaceMockURL(t *testing.T, env []string, url string) []string {
	t.Helper()
	const baseURLSuffix = "_BASE_URL"
	out := make([]string, 0, len(env))
	swapped := 0
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasSuffix(k, baseURLSuffix) {
			out = append(out, k+"="+url)
			swapped++
			continue
		}
		out = append(out, kv)
	}
	if swapped == 0 {
		t.Fatalf("cloneGateEnv carries no model-endpoint key (%s) to swap — the walk cannot read the child's request bodies; env keys:\n%s",
			baseURLSuffix, strings.Join(env, "\n"))
	}
	return out
}

// briefingPublishExcerpt shows the briefing around the publish label (or its
// tail when the label is absent) so a failure prints what the model actually
// received rather than a bare boolean.
func briefingPublishExcerpt(body string) string {
	const window = 1200
	if idx := strings.Index(body, tools.TaskBriefingPublishLabel); idx >= 0 {
		end := idx + window
		if end > len(body) {
			end = len(body)
		}
		return body[idx:end]
	}
	if len(body) > window {
		return "…(tail)" + body[len(body)-window:]
	}
	return body
}
