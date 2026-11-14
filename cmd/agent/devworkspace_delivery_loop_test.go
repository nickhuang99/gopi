package main

// devworkspace_delivery_loop_test.go — the OUTER delivery loop's arithmetic,
// proved without a model and without a git fixture.
//
// What is being pinned here is NOT "the steps run in this order" (that is the
// pipeline this epic replaced). It is the three properties the loop exists for:
//
//	1. an attempt that comes back blocked spends ONE agent round, and the
//	   briefing that round receives carries the blocker's words VERBATIM and
//	   NO step prescription (the task's red line);
//	2. at most devworkspace.MaxDeliveryRounds attempts run, and the log says
//	   which one it is ("delivery attempt 1/3") — the number in the line is the
//	   number that bounds the loop (single-definition);
//	3. an exhausted run spends the closing report round EXACTLY ONCE, does not
//	   count it against the rounds, and returns the ledger reason built from the
//	   last blocker.
//
// A success after a blocked attempt is the case the whole epic is about, so it
// gets its own test: the loop must not stop at the first blocker.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/tools"
)

// fakeLoopState builds a cloneRunState whose attempts are scripted and whose
// agent rounds are recorded instead of spawned.
type fakeLoopState struct {
	state      *cloneRunState
	attempts   int
	briefings  []string
	agentDirs  []string
	agentCalls int
	log        strings.Builder
	// rowStatuses is the SCRIPT the row-status read walks, one entry per read;
	// past the script every read answers liveRowStatus. agentRoundsAtStatusRead
	// is how many agent rounds had already started at each read, which is what
	// proves a cancellation stops the round BEFORE it spawns.
	rowStatuses            []string
	statusReads            int
	statusReadErrs         error
	agentCallsAtStatusRead []int
}

// liveRowStatus is the status of a row nobody has retired — the value every
// script falls back to so the loop's ordinary behaviour is the default.
const liveRowStatus = "in_progress"

// newFakeLoop scripts the attempts with results, in order. Each entry is one
// attempt's (blocker, error) outcome; a nil blocker with a nil error means the
// attempt DELIVERED. Past the script, every attempt blocks with blocker.
func newFakeLoop(t *testing.T, attempts []*devworkspace.Blocker, final *devworkspace.Blocker, agentErr error) *fakeLoopState {
	t.Helper()
	f := &fakeLoopState{}
	f.state = &cloneRunState{
		cfg: devworkspace.Config{TaskID: "01a150eb-0000-7000-8000-000000000001"},
		dir: "/ws/01a150eb-0000-7000-8000-000000000001",
		agentRound: func(dir, briefing string) error {
			f.agentCalls++
			f.agentDirs = append(f.agentDirs, dir)
			f.briefings = append(f.briefings, briefing)
			return agentErr
		},
	}
	// THE ROW-STATUS READ IS SCRIPTED, and its default is a LIVE row: without a
	// script entry the loop must behave exactly as it did before the retirement
	// check existed (that is the "uncancelled rows are unchanged" half).
	f.state.rowStatus = func(taskID string) (string, error) {
		f.agentCallsAtStatusRead = append(f.agentCallsAtStatusRead, f.agentCalls)
		i := f.statusReads
		f.statusReads++
		if f.statusReadErrs != nil {
			return "", f.statusReadErrs
		}
		if i < len(f.rowStatuses) {
			return f.rowStatuses[i], nil
		}
		return liveRowStatus, nil
	}
	f.state.attempt = func() (devworkspace.Result, *devworkspace.Blocker, error) {
		i := f.attempts
		f.attempts++
		res := devworkspace.Result{Dir: f.state.dir, Branch: "master", Attempts: 1}
		if i < len(attempts) {
			blk := attempts[i]
			if blk == nil {
				res.Pushed = true
				res.Detail = "pushed master to origin after 1 attempt(s)"
			}
			return res, blk, nil
		}
		return res, final, nil
	}
	return f
}

// dirtyBlocker is the blocker a dirty workspace produces, with git's own words.
func dirtyBlocker() *devworkspace.Blocker {
	return &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindDirty,
		Words: " M internal/devworkspace/devworkspace.go\n M cmd/agent/cli_tasks.go\n",
		Paths: []string{" M internal/devworkspace/devworkspace.go", " M cmd/agent/cli_tasks.go"},
	}
}

// TestDeliveryLoopSpendsOneAgentRoundPerBlocker is the epic's success shape: the
// first attempt is blocked (the agent left its work uncommitted), the agent gets
// the blocker's own words, and the SECOND attempt delivers.
func TestDeliveryLoopSpendsOneAgentRoundPerBlocker(t *testing.T) {
	f := newFakeLoop(t, []*devworkspace.Blocker{dirtyBlocker(), nil}, nil, nil)
	res, err := f.state.finish(&f.log)
	if err != nil {
		t.Fatalf("the loop must deliver once the agent round unblocks it: %v", err)
	}
	if !res.Pushed {
		t.Fatalf("want a delivered result, got %+v", res)
	}
	if f.attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (blocked, then delivered)", f.attempts)
	}
	if f.agentCalls != 1 {
		t.Fatalf("agent rounds = %d, want exactly 1 (one per blocker)", f.agentCalls)
	}
	if f.state.rounds != 2 {
		t.Fatalf("rounds = %d, want 2 (the round that landed is the last one spent)", f.state.rounds)
	}
	out := f.log.String()
	// THE LOG NAMES THE ATTEMPT AND THE BLOCKER'S OWN WORDS (Rule 15).
	for _, want := range []string{
		fmt.Sprintf("delivery attempt 1/%d", devworkspace.MaxDeliveryRounds),
		fmt.Sprintf("delivery attempt 2/%d", devworkspace.MaxDeliveryRounds),
		"blocked (dirty)",
		" M internal/devworkspace/devworkspace.go",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the log must carry %q:\n%s", want, out)
		}
	}
	// AND THE BRIEFING IS FACTS ONLY: the workspace, git's words, the round
	// number — no step list (the red line: no "first git add, then git commit").
	briefing := f.briefings[0]
	if !strings.Contains(briefing, f.state.dir) {
		t.Fatalf("the briefing must name the workspace:\n%s", briefing)
	}
	if !strings.Contains(briefing, " M internal/devworkspace/devworkspace.go") {
		// (leading space included on purpose — see verbatimWords)
		t.Fatalf("the briefing must carry the tool's words VERBATIM:\n%s", briefing)
	}
	for _, banned := range []string{"git add", "git commit", "git stash", "第一步", "第二步"} {
		if strings.Contains(briefing, banned) {
			t.Fatalf("the briefing must not prescribe steps (%q found):\n%s", banned, briefing)
		}
	}
}

// TestDeliveryLoopStopsAtMaxDeliveryRoundsAndSpendsOneReportRound is the
// exhausted path: every attempt is blocked, so the loop runs exactly
// MaxDeliveryRounds attempts, hands back the LAST blocker's words, spends ONE
// closing report round (not counted against the rounds), and returns the ledger
// reason.
func TestDeliveryLoopStopsAtMaxDeliveryRoundsAndSpendsOneReportRound(t *testing.T) {
	f := newFakeLoop(t, nil, dirtyBlocker(), nil)
	_, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("a run whose every delivery attempt was blocked must fail")
	}
	if f.attempts != devworkspace.MaxDeliveryRounds {
		t.Fatalf("attempts = %d, want the named limit %d", f.attempts, devworkspace.MaxDeliveryRounds)
	}
	// THE ARITHMETIC OF THE OUTER LOOP: a blocked attempt that has a NEXT
	// attempt gets one agent round (that is MaxDeliveryRounds-1 of them); the
	// LAST blocked attempt does not get a retry round — the closing report round
	// is the one that answers it. So the total is MaxDeliveryRounds, spent
	// exactly once each, and the report round is not an extra attempt.
	if want := devworkspace.MaxDeliveryRounds; f.agentCalls != want {
		t.Fatalf("agent rounds = %d, want %d (%d retry rounds + the ONE closing report round)",
			f.agentCalls, want, want-1)
	}
	reason := err.Error()
	if !strings.Contains(reason, fmt.Sprintf("did not land in %d attempt(s)", devworkspace.MaxDeliveryRounds)) {
		t.Fatalf("the ledger reason must name the round count: %s", reason)
	}
	if !strings.Contains(reason, " M internal/devworkspace/devworkspace.go") {
		t.Fatalf("the ledger reason must carry the last blocker's words: %s", reason)
	}
	if !strings.Contains(reason, f.state.dir) {
		t.Fatalf("the ledger reason must name the KEPT workspace: %s", reason)
	}
	// THE CLOSING ROUND ASKS FOR A REPORT, AND DOES NOT RE-ATTEMPT.
	closing := f.briefings[len(f.briefings)-1]
	if !strings.Contains(closing, "写报告") {
		t.Fatalf("the closing round must ask for the report:\n%s", closing)
	}
	if strings.Contains(closing, "交付尝试最多") {
		t.Fatalf("the closing round must not be another delivery round:\n%s", closing)
	}
	for _, banned := range []string{"git add", "git commit", "git stash"} {
		if strings.Contains(closing, banned) {
			t.Fatalf("the closing briefing must not prescribe steps (%q found):\n%s", banned, closing)
		}
	}
	// THE ANNOUNCEMENT IS ONE LINE PER ATTEMPT, and the count is taken over
	// whole lines: "delivery attempt 3/3" also prefixes the blocked line that
	// names the kind, so a substring count would see two (measured).
	out := f.log.String()
	announced := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line),
			fmt.Sprintf("delivery attempt %d/%d", devworkspace.MaxDeliveryRounds, devworkspace.MaxDeliveryRounds)) {
			announced++
		}
	}
	if announced != 1 {
		t.Fatalf("the last attempt must be announced exactly once, found %d:\n%s", announced, out)
	}
}

// TestDeliveryLoopRetryFailureDoesNotStopTheLoop pins the politeness: an agent
// round that exits non-zero is a fact to log, not a reason to abandon the
// delivery — the next attempt runs anyway.
func TestDeliveryLoopRetryFailureDoesNotStopTheLoop(t *testing.T) {
	f := newFakeLoop(t, []*devworkspace.Blocker{dirtyBlocker(), nil}, nil, errors.New("exit status 1"))
	res, err := f.state.finish(&f.log)
	if err != nil {
		t.Fatalf("a failed agent round must not end the loop: %v", err)
	}
	if !res.Pushed || f.attempts != 2 {
		t.Fatalf("the loop must still run the next attempt, got pushed=%v attempts=%d", res.Pushed, f.attempts)
	}
	if !strings.Contains(f.log.String(), "the delivery round's agent exited with") {
		t.Fatalf("the failed round must be reported by name:\n%s", f.log.String())
	}
}

// TestDeliveryLoopReportsANonBlockerFailureAsAnError pins the OTHER error arm:
// a broken attempt (no declared build, an unreadable branch, a push that was not
// a race) is returned immediately and spends NO agent round — no agent can
// conjure a missing declaration.
func TestDeliveryLoopReportsANonBlockerFailureAsAnError(t *testing.T) {
	f := newFakeLoop(t, nil, nil, nil)
	f.state.attempt = func() (devworkspace.Result, *devworkspace.Blocker, error) {
		f.attempts++
		return devworkspace.Result{Dir: f.state.dir}, nil,
			errors.New("clone channel: no build command is declared for this repo")
	}
	_, err := f.state.finish(&f.log)
	if err == nil || !strings.Contains(err.Error(), "no build command is declared") {
		t.Fatalf("want the broken-attempt error verbatim, got %v", err)
	}
	if f.attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (a broken attempt is not retried)", f.attempts)
	}
	if f.agentCalls != 0 {
		t.Fatalf("agent rounds = %d, want 0", f.agentCalls)
	}
}

// TestDeliveryLoopNothingToDeliverIsNotAFailure is the whole point of row
// 01a18244: the fork at the end of finish() used to answer "every attempt was
// blocked" and "there was nothing to deliver" with the SAME sentence
// ("delivery did not land"), so an items=0 counter-proof run — asked to produce
// no change, producing none — was recorded `failed`.
//
// The two facts are now two exits, and this test pins BOTH halves of the split:
// the empty kind returns the nothing-to-deliver classification with its own
// words, and every kind that represents a STEP refusing still returns the
// exhausted failure.
func TestDeliveryLoopNothingToDeliverIsNotAFailure(t *testing.T) {
	empty := &devworkspace.Blocker{
		Kind: devworkspace.BlockerKindEmpty,
		Words: " /ws carries no commit to deliver: `git rev-list --count HEAD --not origin/master` " +
			"answered 0 against origin/master, so there is nothing to pull and nothing to push",
	}
	f := newFakeLoop(t, nil, empty, nil)
	res, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("the exit must still carry its reason as an error (the caller reads its classification)")
	}
	if !errors.Is(err, errNothingToDeliver) {
		t.Fatalf("the empty kind must be classified nothing-to-deliver, got %v", err)
	}
	reason := err.Error()
	if !strings.Contains(reason, "nothing to deliver") {
		t.Fatalf("the reason must say the run had nothing to deliver: %s", reason)
	}
	if strings.Contains(reason, "delivery did not land") {
		t.Fatalf("a ground with nothing to publish must NOT wear the failure's sentence: %s", reason)
	}
	if res.Pushed {
		t.Fatalf("no push happened, so the result must not claim one: %+v", res)
	}
	// The closing round still runs, and it is the LAST one: the retry rounds
	// before it are the loop's existing behaviour (an empty attempt still hands
	// back to the agent, which may decide the work belongs committed HERE) — what
	// this test pins is that the round the run ENDS on tells the truth.
	if want := devworkspace.MaxDeliveryRounds; f.agentCalls != want {
		t.Fatalf("agent rounds = %d, want %d", f.agentCalls, want)
	}
	closing := f.briefings[len(f.briefings)-1]
	if !strings.Contains(closing, "不算失败") {
		t.Fatalf("the closing round must state that the run is not a failure:\n%s", closing)
	}
	if !strings.Contains(closing, "no commit") && !strings.Contains(closing, empty.Words) {
		t.Fatalf("the closing round must carry the measurement verbatim:\n%s", closing)
	}
	// AND THE CHANNEL'S OWN LOG SAYS THE SAME THING AS THE REASON.
	if !strings.Contains(f.log.String(), "nothing to deliver") {
		t.Fatalf("the run's log must carry the exit's own sentence:\n%s", f.log.String())
	}
}

// TestDeliveryLoopRealFailuresStillFail is the anti-widening half: the empty
// split must not have turned a refused delivery into a success. Each of the
// four refusing kinds still returns an error, and it is NOT the
// nothing-to-deliver classification.
func TestDeliveryLoopRealFailuresStillFail(t *testing.T) {
	refusing := []*devworkspace.Blocker{
		dirtyBlocker(),
		{Kind: devworkspace.BlockerKindPull, Words: "CONFLICT (content): Merge conflict in a.txt"},
		{Kind: devworkspace.BlockerKindBuild, Words: "exit status 1"},
		{Kind: devworkspace.BlockerKindPush, Words: "! [rejected] master -> master (non-fast-forward)"},
	}
	for _, blk := range refusing {
		blk := blk
		t.Run(string(blk.Kind), func(t *testing.T) {
			f := newFakeLoop(t, nil, blk, nil)
			_, err := f.state.finish(&f.log)
			if err == nil {
				t.Fatalf("the %s kind is a refused delivery and must fail", blk.Kind)
			}
			if errors.Is(err, errNothingToDeliver) {
				t.Fatalf("the %s kind must NOT be classified nothing-to-deliver: %v", blk.Kind, err)
			}
			if !strings.Contains(err.Error(), "delivery did not land") {
				t.Fatalf("the %s kind must keep the failure's sentence: %v", blk.Kind, err)
			}
			if strings.Contains(err.Error(), "nothing to deliver") {
				t.Fatalf("the %s kind must not read as an empty ground: %v", blk.Kind, err)
			}
		})
	}
}

// TestDeliveryBriefingsCarryNoStepList is the red line as a unit test over BOTH
// briefings: the facts are the workspace, the tool's words and the round number,
// and nothing else. It is separate from the two tests above so a prescription
// added to EITHER briefing fails here by name.
func TestDeliveryBriefingsCarryNoStepList(t *testing.T) {
	blk := dirtyBlocker()
	dir := "/ws/task"
	// THE EMPTY KIND IS ITS OWN BRIEFING, and this is the arm that keeps it: the
	// sentence "被 <kind> 这一步挡下了" makes a reader look for a step that
	// refused, which is precisely the ghost the split removes (row 01a17dee was
	// told the PULL had blocked it while its workspace held nothing to publish).
	emptyBriefing := deliveryRetryBriefing(&devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindEmpty,
		Words: "carries no commit to deliver",
	}, dir, 1)
	for _, want := range []string{dir, "交付没有内容可发布", "carries no commit to deliver"} {
		if !strings.Contains(emptyBriefing, want) {
			t.Fatalf("the empty briefing must carry %q:\n%s", want, emptyBriefing)
		}
	}
	if strings.Contains(emptyBriefing, "这一步挡下了") {
		t.Fatalf("the empty briefing must not say a STEP blocked the delivery — nothing refused:\n%s", emptyBriefing)
	}
	// AND IT NAMES THE TWO GHOSTS ONLY TO DENY THEM, in the same sentence.
	for _, denial := range []string{"不是 pull 被拒", "也不是冲突"} {
		if !strings.Contains(emptyBriefing, denial) {
			t.Fatalf("the empty briefing must deny %q explicitly:\n%s", denial, emptyBriefing)
		}
	}
	for name, briefing := range map[string]string{
		"retry":     deliveryRetryBriefing(blk, dir, 1),
		"exhausted": deliveryExhaustedBriefing(blk, dir, devworkspace.MaxDeliveryRounds),
	} {
		// The WORDS are asserted with their leading spaces intact: `" M file"` and
		// `"M file"` are different git facts (unstaged vs staged), so a briefing
		// that trims them has quietly changed the tool's answer.
		for _, want := range []string{dir, " M internal/devworkspace/devworkspace.go", string(blk.Kind)} {
			if !strings.Contains(briefing, want) {
				t.Fatalf("%s briefing must carry %q:\n%s", name, want, briefing)
			}
		}
		for _, banned := range []string{"git add", "git commit", "git stash", "git checkout", "第一步", "先 commit"} {
			if strings.Contains(briefing, banned) {
				t.Fatalf("%s briefing must not prescribe %q:\n%s", name, banned, briefing)
			}
		}
	}
}

// TestDeliveryExhaustedBriefingPutsEachFactInItsSlot pins WHICH fact sits WHERE.
//
// A `strings.Contains` arm cannot catch an argument SWAP: the pre-fix call was
// `(rounds, blk.Kind, dir, words)` against a template of `(rounds, dir, kind,
// words)`, so every fact was still present and every Contains assertion still
// passed while the sentence told the reader "工作区被保留，在 empty。最后挡下的是
// /ws/task 这一步" (measured, row 01a18242). Position is the property, so this
// test asserts position: the path must land where the kept-workspace sentence
// names its directory, and the kind must land between the step sentence's own
// prefix and suffix.
func TestDeliveryExhaustedBriefingPutsEachFactInItsSlot(t *testing.T) {
	blk := dirtyBlocker()
	// A directory whose text cannot be mistaken for a kind, and a kind that
	// cannot be mistaken for a path: the swap this test exists for had a path
	// like "/ws/task" and the kind "empty".
	const dir = "/home/user/workspace/task-01a18242"
	briefing := deliveryExhaustedBriefing(blk, dir, devworkspace.MaxDeliveryRounds)

	// THE PATH IS THE KEPT WORKSPACE, in the sentence that says so.
	kept := deliveryExhaustedKeptPrefix + dir
	if !strings.Contains(briefing, kept) {
		t.Fatalf("the exhausted briefing must spell %q (the path in the kept-workspace slot):\n%s", kept, briefing)
	}
	// THE KIND IS THE STEP THAT REFUSED, between the two tokens that say so.
	step := deliveryExhaustedStepPrefix + string(blk.Kind) + deliveryExhaustedStepSuffix
	if i := strings.Index(briefing, step); i < 0 {
		t.Fatalf("the exhausted briefing must spell %q (the kind in the step slot):\n%s", step, briefing)
	} else if k := strings.Index(briefing, deliveryExhaustedKeptPrefix); k > i {
		t.Fatalf("the kept-workspace sentence must come BEFORE the step sentence (kept at %d, step at %d):\n%s",
			k, i, briefing)
	}
	// AND THE SWAP ITSELF IS REFUSED BY NAME, so a future edit that reintroduces
	// it fails here even if it invents a new spelling for one slot.
	for _, swapped := range []string{
		deliveryExhaustedKeptPrefix + string(blk.Kind),
		deliveryExhaustedStepPrefix + dir + deliveryExhaustedStepSuffix,
	} {
		if strings.Contains(briefing, swapped) {
			t.Fatalf("the two facts are in each other's slot (%q found):\n%s", swapped, briefing)
		}
	}
	// The ROUND COUNT is still the first thing the block says.
	if !strings.Contains(briefing, fmt.Sprintf("：%d 轮交付尝试全部被挡下", devworkspace.MaxDeliveryRounds)) {
		t.Fatalf("the closing briefing must open with the round count:\n%s", briefing)
	}
}

// TestDeliveryExhaustedBriefingDoesNotInventAStepForTheEmptyKind pins the second
// half of the empty kind's split: nothing refused, so the closing round must not
// say a step did. The empty kind reaches the exhausted path whenever an agent
// round after a blocker leaves the workspace still holding no commit.
func TestDeliveryExhaustedBriefingDoesNotInventAStepForTheEmptyKind(t *testing.T) {
	const dir = "/home/user/workspace/task-01a18242"
	briefing := deliveryExhaustedBriefing(&devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindEmpty,
		Words: "0\n",
	}, dir, devworkspace.MaxDeliveryRounds)

	if !strings.Contains(briefing, deliveryExhaustedKeptPrefix+dir) {
		t.Fatalf("the empty closing briefing must still name the KEPT workspace (%q):\n%s",
			deliveryExhaustedKeptPrefix+dir, briefing)
	}
	if strings.Contains(briefing, deliveryExhaustedStepPrefix) {
		t.Fatalf("nothing refused, so the empty closing briefing must not open a step sentence (%q):\n%s",
			deliveryExhaustedStepPrefix, briefing)
	}
	if strings.Contains(briefing, string(devworkspace.BlockerKindEmpty)) {
		t.Fatalf("the empty closing briefing must not name the kind as if it were a step (%q found):\n%s",
			devworkspace.BlockerKindEmpty, briefing)
	}
	for _, want := range []string{deliveryEmptyMeasurement, "不是 pull 被拒", "也不是冲突", "写报告"} {
		if !strings.Contains(briefing, want) {
			t.Fatalf("the empty closing briefing must carry %q:\n%s", want, briefing)
		}
	}
	for _, banned := range []string{"git add", "git commit", "git stash", "git checkout", "第一步", "先 commit"} {
		if strings.Contains(briefing, banned) {
			t.Fatalf("the empty closing briefing must not prescribe %q:\n%s", banned, briefing)
		}
	}
}

// ============================================================================
// ROW 01a18392 — the delivery loop reads the ROW at its round boundary.
//
// THE DEFECT: finish() was unconditional. An operator cancelled a row and the
// loop still spent all MaxDeliveryRounds attempts, spawning a bare-prompt agent
// round between every pair of them — and each of those rounds registered a NEW
// ledger row, so one cancelled row kept producing work. The three tests below
// are the whole criterion: a retired row stops BEFORE the next spawn, and an
// ordinary row is untouched (the same three rounds, the same three rounds'
// arithmetic, the same sentences).
// ============================================================================

// TestDeliveryLoopStopsWhenTheRowIsAlreadyRetired pins the earliest boundary: a
// row retired BEFORE the loop starts spends nothing at all — no attempt, no
// agent round, and neither the exhausted nor the nothing-to-deliver sentence
// (both of which would tell a reader a delivery judgement happened that the
// row's owner never asked for).
func TestDeliveryLoopStopsWhenTheRowIsAlreadyRetired(t *testing.T) {
	f := newFakeLoop(t, nil, dirtyBlocker(), nil)
	f.rowStatuses = []string{tools.TaskStatusCancelled}
	res, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("a retired row must stop the loop with an error, not run it")
	}
	if !errors.Is(err, errRowRetired) {
		t.Fatalf("the exit must be classified as a retirement, got %v", err)
	}
	if errors.Is(err, errNothingToDeliver) {
		t.Fatalf("a retired row is not the nothing-to-deliver fact: %v", err)
	}
	if f.attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (the boundary is before the first attempt)", f.attempts)
	}
	if f.agentCalls != 0 {
		t.Fatalf("agent rounds = %d, want 0 (a retired row must spawn nothing)", f.agentCalls)
	}
	if res.Pushed {
		t.Fatalf("nothing was pushed, so the result must not claim one: %+v", res)
	}
	// NEITHER TERMINAL SENTENCE IS ALLOWED TO APPEAR.
	for _, banned := range []string{"delivery did not land", "nothing to deliver"} {
		if strings.Contains(f.log.String(), banned) || strings.Contains(err.Error(), banned) {
			t.Fatalf("a retired row must not be written with %q:\n%s\n%s", banned, f.log.String(), err.Error())
		}
	}
	// AND THE REASON NAMES THE STATUS AND THE KEPT WORKSPACE.
	reason := err.Error()
	if !strings.Contains(reason, tools.TaskStatusCancelled) {
		t.Fatalf("the reason must name the status that ended the loop: %s", reason)
	}
	if !strings.Contains(reason, f.state.dir) {
		t.Fatalf("the reason must name the KEPT workspace: %s", reason)
	}
}

// TestDeliveryLoopStopsBeforeTheRetryRoundIsSpawned is the measured shape: the
// first attempt runs, the row is cancelled while it runs, and the retry round
// that WOULD have spawned a bare-prompt agent (and a new ledger row) is not
// started. The read order is what proves it — the status was read AFTER the
// first round's spawn count was 0 and the agent count never moved.
func TestDeliveryLoopStopsBeforeTheRetryRoundIsSpawned(t *testing.T) {
	f := newFakeLoop(t, nil, dirtyBlocker(), nil)
	// first read: still live → attempt 1 runs; second read: cancelled → stop.
	f.rowStatuses = []string{liveRowStatus, tools.TaskStatusCancelled}
	_, err := f.state.finish(&f.log)
	if err == nil || !errors.Is(err, errRowRetired) {
		t.Fatalf("the loop must stop with the retirement classification, got %v", err)
	}
	if f.attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (the cancel landed during the first attempt)", f.attempts)
	}
	if f.agentCalls != 0 {
		t.Fatalf("agent rounds = %d, want 0 — the retry round is exactly what must NOT be spawned", f.agentCalls)
	}
	if f.state.rounds != 1 {
		t.Fatalf("rounds = %d, want 1 (the attempt that ran before the row was retired)", f.state.rounds)
	}
	// THE SECOND READ HAPPENED AT ZERO AGENT ROUNDS: the boundary is placed
	// before the spawn, not after it (a read taken after the spawn would prove
	// nothing about the spawn).
	if len(f.agentCallsAtStatusRead) < 2 {
		t.Fatalf("the loop must read the row at both boundaries, got %d read(s)", len(f.agentCallsAtStatusRead))
	}
	// THE ONE ATTEMPT'S BLOCKER IS STILL SAID OUT LOUD — the stop does not hide
	// why the first attempt failed.
	if !strings.Contains(f.log.String(), "blocked (dirty)") {
		t.Fatalf("the first attempt's blocker must still be reported:\n%s", f.log.String())
	}
	if strings.Contains(f.log.String(), "delivery attempt 2/") {
		t.Fatalf("a retired row must not log a second attempt:\n%s", f.log.String())
	}
}

// TestDeliveryLoopUnretiredRowsKeepTheirRounds is the OTHER half of the
// criterion, and the one that must not move: a row that stays live runs the
// same MaxDeliveryRounds attempts and spends the same number of agent rounds as
// before the retirement check existed. Two live statuses are walked — the one a
// running row carries and the one a row carries after its own close-out — so the
// check cannot be widened into "any status stops the loop" unnoticed.
func TestDeliveryLoopUnretiredRowsKeepTheirRounds(t *testing.T) {
	for _, status := range []string{tools.TaskStatusInProgress, tools.TaskStatusCompleted, ""} {
		status := status
		t.Run("status="+status, func(t *testing.T) {
			f := newFakeLoop(t, nil, dirtyBlocker(), nil)
			f.state.rowStatus = func(string) (string, error) { return status, nil }
			_, err := f.state.finish(&f.log)
			if err == nil {
				t.Fatal("every attempt was blocked, so the run must fail")
			}
			if errors.Is(err, errRowRetired) {
				t.Fatalf("status %q must NOT stop the loop: %v", status, err)
			}
			if f.attempts != devworkspace.MaxDeliveryRounds {
				t.Fatalf("attempts = %d, want the named limit %d (unchanged behaviour)",
					f.attempts, devworkspace.MaxDeliveryRounds)
			}
			if f.agentCalls != devworkspace.MaxDeliveryRounds {
				t.Fatalf("agent rounds = %d, want %d (unchanged behaviour)",
					f.agentCalls, devworkspace.MaxDeliveryRounds)
			}
			if !strings.Contains(err.Error(), "delivery did not land") {
				t.Fatalf("an exhausted live row keeps its own sentence: %v", err)
			}
		})
	}
}

// TestDeliveryLoopUnreadableRowStatusKeepsTheLoopRunning pins the failure mode of
// the check itself: a status read that CANNOT be taken is not permission to stop
// (Rule 15 — a read failure is reported, never flattened into a verdict). The
// loop must then behave exactly as it did before the check existed, which is the
// only safe direction: a ledger hiccup must not silently abandon a delivery.
func TestDeliveryLoopUnreadableRowStatusKeepsTheLoopRunning(t *testing.T) {
	f := newFakeLoop(t, nil, dirtyBlocker(), nil)
	f.statusReadErrs = errors.New("no such table: tasks")
	_, err := f.state.finish(&f.log)
	if err == nil || errors.Is(err, errRowRetired) {
		t.Fatalf("an unreadable status must not stop the loop, got %v", err)
	}
	if f.attempts != devworkspace.MaxDeliveryRounds || f.agentCalls != devworkspace.MaxDeliveryRounds {
		t.Fatalf("the loop must run its usual rounds, got attempts=%d agentRounds=%d",
			f.attempts, f.agentCalls)
	}
	if !strings.Contains(f.log.String(), "the ledger status could not be read") {
		t.Fatalf("the failed read must be reported by name:\n%s", f.log.String())
	}
}

// TestDeliveryLoopAlreadyDeliveredIsItsOwnCompletion pins the FIFTH outcome
// (row 01a182df, measured 2026-10-28): the clone channel's own push moves
// `origin/<base>` onto this run's tip, so the empty gate's one-dimensional
// reading — `git rev-list --count HEAD --not origin/base` — answers 0 on the
// VERY NEXT attempt. That 0 was read as "this run produced nothing" and the run
// was recorded as an empty ground while its work sat on the integration branch.
//
// The gate now takes a second reading (the tip this run pushed, measured
// reachable from the base) and answers with its own kind, so this arm pins the
// three things that must stay true:
//
//	① the exit is its OWN sentinel — not the empty one and not a failure;
//	② its sentence is DIFFERENT from both of theirs (Rule 15: two opposite facts
//	   never share one sentence);
//	③ it spends NO agent round: nothing is owed, so a round spawned to say
//	   "nothing to do" would be a process minted for nothing.
func TestDeliveryLoopAlreadyDeliveredIsItsOwnCompletion(t *testing.T) {
	delivered := &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindAlreadyDelivered,
		Step:  "rev-list --count HEAD --not origin/master",
		Words: "/ws/ground carries no commit to deliver — but this run's delivery already landed (abc1234): the tip this run pushed is reachable from master",
	}
	f := newFakeLoop(t, nil, delivered, nil)
	res, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("the exit must still carry its reason as an error (the caller reads its classification)")
	}
	if !errors.Is(err, errAlreadyDelivered) {
		t.Fatalf("the already-delivered kind must be classified by its own sentinel, got %v", err)
	}
	if errors.Is(err, errNothingToDeliver) {
		t.Fatalf("the two zeroes must not collapse into ONE classification: %v", err)
	}
	reason := err.Error()
	// ② THE SENTENCES MUST DIFFER, in both directions.
	if strings.Contains(reason, "delivery did not land") {
		t.Fatalf("the delivery DID land — it must not wear the failure's sentence: %s", reason)
	}
	if !strings.Contains(reason, "ALREADY LANDED") {
		t.Fatalf("the reason must say the delivery already landed: %s", reason)
	}
	if strings.Contains(reason, "nothing to pull and nothing to push") {
		t.Fatalf("the reason must not borrow the EMPTY ground's sentence: %s", reason)
	}
	// ③ NO ROUND IS SPENT — not even the closing one, which the empty arm does
	// take (that one may still owe a report; this one does not).
	if f.agentCalls != 0 {
		t.Fatalf("agent rounds = %d, want 0 (nothing is owed, so no round is spawned)", f.agentCalls)
	}
	if res.Pushed {
		t.Fatalf("no push happened in this attempt, so the result must not claim one: %+v", res)
	}
	// AND THE OPERATOR'S LOG SAYS THE SAME THING AS THE ROW'S REASON.
	if !strings.Contains(f.log.String(), "ALREADY LANDED") {
		t.Fatalf("the run's log must carry the exit's own sentence:\n%s", f.log.String())
	}
}

// TestDeliveryLoopAlreadyDeliveredIsNotARealFailure is the anti-widening half,
// stated the other way round: the FIFTH outcome must not have turned a refused
// delivery into a completion either. The four refusing kinds still reach
// exhausted(), and none of them is classified already-delivered.
func TestDeliveryLoopAlreadyDeliveredIsNotARealFailure(t *testing.T) {
	refusing := []*devworkspace.Blocker{
		dirtyBlocker(),
		{Kind: devworkspace.BlockerKindPull, Words: "CONFLICT (content): Merge conflict in a.txt"},
		{Kind: devworkspace.BlockerKindBuild, Words: "exit status 1"},
		{Kind: devworkspace.BlockerKindPush, Words: "! [rejected] master -> master (non-fast-forward)"},
	}
	for _, blk := range refusing {
		blk := blk
		t.Run(string(blk.Kind), func(t *testing.T) {
			f := newFakeLoop(t, nil, blk, nil)
			_, err := f.state.finish(&f.log)
			if err == nil {
				t.Fatalf("the %s kind is a refused delivery and must fail", blk.Kind)
			}
			if errors.Is(err, errAlreadyDelivered) {
				t.Fatalf("the %s kind must NOT be classified already-delivered: %v", blk.Kind, err)
			}
			if !strings.Contains(err.Error(), "delivery did not land") {
				t.Fatalf("the %s kind must keep the failure's sentence: %v", blk.Kind, err)
			}
		})
	}
}

// TestDeliveryLoopInheritedDeliveryIsItsOwnCompletion pins the SIXTH outcome
// (epic: inherited delivery): the THIRD meaning of the same `count == 0` — HEAD
// is a STRICT ancestor of the integration branch, so the content this ground was
// handed is already on the base and this run pushed nothing.
//
// The defect this arm closes is measured, not theorised: classified as EMPTY,
// the loop did NOT break, so each of up to MaxDeliveryRounds rounds spawned a
// real agent round that re-measured an immutable fact and registered a new
// ledger row (52 sessions whose log carries `carries no commit to deliver`).
//
// Three things must stay true:
//
//	① the exit is its OWN sentinel — not the empty one, not the already-delivered
//	   one, and not a failure;
//	② its sentence is DIFFERENT from all three of theirs (Rule 15);
//	③ it spends NO agent round and the loop makes NO SECOND ATTEMPT: nothing an
//	   agent round does can change "your content is already on the base".
func TestDeliveryLoopInheritedDeliveryIsItsOwnCompletion(t *testing.T) {
	inherited := &devworkspace.Blocker{
		Kind: devworkspace.BlockerKindInheritedDelivery,
		Step: "rev-list --count HEAD --not origin/master",
		Words: "/ws/ground carries no commit to deliver — 39bd44a is contained by origin/master, so " +
			"the content this run was handed is already on the integration branch: there is nothing to push, " +
			"and this is NOT a failure. so there is nothing to pull and nothing to push",
	}
	f := newFakeLoop(t, nil, inherited, nil)
	res, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("the exit must still carry its reason as an error (the caller reads its classification)")
	}
	if !errors.Is(err, errInheritedDelivery) {
		t.Fatalf("the inherited kind must be classified by its own sentinel, got %v", err)
	}
	for _, foreign := range []error{errNothingToDeliver, errAlreadyDelivered} {
		if errors.Is(err, foreign) {
			t.Fatalf("the three zeroes must not collapse into ONE classification (%v vs %v): %v",
				err, foreign, err)
		}
	}
	reason := err.Error()
	// ② THE SENTENCES MUST DIFFER, in every direction.
	if strings.Contains(reason, "delivery did not land") {
		t.Fatalf("nothing was refused here — it must not wear the failure's sentence: %s", reason)
	}
	if strings.Contains(reason, "ALREADY LANDED") {
		t.Fatalf("this run pushed nothing, so it must not claim its own push landed: %s", reason)
	}
	if !strings.Contains(reason, "ALREADY ON THE INTEGRATION BRANCH") {
		t.Fatalf("the reason must state the fact this kind exists for: %s", reason)
	}
	// ③ NO ROUND IS SPENT, and the loop stops after ONE attempt: the count of
	// attempts is the number of agent rounds the empty kind used to burn.
	if f.agentCalls != 0 {
		t.Fatalf("agent rounds = %d, want 0 (nothing is owed, so no round is spawned)", f.agentCalls)
	}
	if f.attempts != 1 {
		t.Fatalf("delivery attempts = %d, want 1 — an immutable fact must not be re-measured per round", f.attempts)
	}
	if strings.Contains(f.log.String(), fmt.Sprintf("delivery attempt 2/%d", devworkspace.MaxDeliveryRounds)) {
		t.Fatalf("the loop must not schedule a second delivery attempt for an inherited delivery:\n%s", f.log.String())
	}
	if res.Pushed {
		t.Fatalf("no push happened in this attempt, so the result must not claim one: %+v", res)
	}
	if !strings.Contains(f.log.String(), "ALREADY ON THE INTEGRATION BRANCH") {
		t.Fatalf("the run's log must carry the exit's own sentence:\n%s", f.log.String())
	}
}

// TestDeliveryLoopDoesNotRetryAGroundThatNeverCommitted is the DELIVERY-LOOP half
// of bug row 01a1907d-family (measured 2026-10-31 on 01a18d61-509f).
//
// THE INCIDENT, in the row's own log (verbatim, /home/user/.gopi/sessions/
// 01a18d61-509f-7ba9-aeaa-4526f249b637/output.log, three identical lines then
// the summary):
//
//	clone-channel: /home/user/workspace/01a18d61-… carries no commit to deliver:
//	  `rev-list --count HEAD --not origin/master` answered 0 against origin/master, …
//	gopi worker: clone channel: nothing to deliver — … (after 3 delivery attempt(s), …)
//
// The row was a TOO_BIG reframe: its deliverable is a DECOMPOSITION (four child
// rows) and its ground was still standing on the commit the branch was cut from,
// so no attempt could ever produce a commit. The empty kind did not break the
// loop, so all MaxDeliveryRounds attempts ran and an agent round ran between each
// pair of them, re-measuring one immutable fact.
//
// THE FIX IS THE BRAKE, and this test pins BOTH directions of it: a ground that
// never committed spends ONE attempt (the one that MEASURED the fact) and no
// agent round at all beyond the closing report; a ground that is merely BLOCKED
// keeps every round it had before (TestDeliveryLoopSpendsOneAgentRoundPerBlocker
// and TestDeliveryLoopNothingToDeliverIsNotAFailure hold that side, and the
// abstention arm below holds the third).
func TestDeliveryLoopDoesNotRetryAGroundThatNeverCommitted(t *testing.T) {
	f := newFakeLoop(t, nil, &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindEmpty,
		Step:  "rev-list --count HEAD --not origin/master",
		Words: " /ws carries no commit to deliver: `rev-list --count HEAD --not origin/master` answered 0",
	}, nil)
	probes := 0
	f.state.groundProducedNothing = func(ground, branch string) tools.GroundProducedNothingRead {
		probes++
		return tools.GroundProducedNothingRead{
			Measured: true,
			Nothing:  true,
			Pending:  false,
			Evidence: "fixture: HEAD = cut point, worktree clean",
		}
	}
	if _, err := f.state.finish(&f.log); err == nil || !errors.Is(err, errNothingToDeliver) {
		t.Fatalf("the run must still END with the nothing-to-deliver conclusion, got %v", err)
	}
	if f.attempts != 1 {
		t.Fatalf("attempts = %d, want 1 — a ground that never committed cannot be helped by a second delivery round "+
			"(the incident spent %d)", f.attempts, devworkspace.MaxDeliveryRounds)
	}
	if probes == 0 {
		t.Fatal("the brake never asked the ground — a fixture that fails to reach the code under test is a vacuum, not a green")
	}
	// ONE agent round: the closing report round. The RETRY rounds (the ones the
	// brake exists to stop) would make this > 1 before the report round runs.
	if f.agentCalls != 1 {
		t.Fatalf("agent rounds = %d, want 1 (the closing report round only; retry rounds are the waste this fixes)", f.agentCalls)
	}
	// Reclaim is NOT asserted here: the fake loop's /ws/... directory does not
	// exist on disk, so whether it was removed is the fixture's business.
	// TestDeliveryLoopNothingToDeliverIsNotAFailure covers the empty kind's own
	// reclaim path.
	if out := f.log.String(); !strings.Contains(out, "NEVER produced a commit") {
		t.Fatalf("the log must state the measured fact that stopped the loop:\n%s", out)
	}
}

// TestDeliveryLoopKeepsRoundsWhenTheGroundIsNotMeasured is the ABSTENTION arm:
// a ground whose produced-nothing question could not be answered, or one that is
// AHEAD of its cut point, or one that carries an uncommitted change, must keep
// every round it had. Each is a different reason NOT to brake, and folding them
// into "empty ⇒ stop" would cut a real delivery short (Rule 15).
func TestDeliveryLoopKeepsRoundsWhenTheGroundIsNotMeasured(t *testing.T) {
	cases := map[string]tools.GroundProducedNothingRead{
		"unmeasured":       {Measured: false, Evidence: "fixture: the cut point could not be read"},
		"ahead of cut":     {Measured: true, Nothing: false, Evidence: "fixture: HEAD is ahead"},
		"uncommitted work": {Measured: true, Nothing: true, Pending: true, Evidence: "fixture: worktree dirty"},
	}
	for name, read := range cases {
		read := read
		t.Run(name, func(t *testing.T) {
			f := newFakeLoop(t, nil, &devworkspace.Blocker{
				Kind:  devworkspace.BlockerKindEmpty,
				Words: " /ws carries no commit to deliver",
			}, nil)
			f.state.groundProducedNothing = func(ground, branch string) tools.GroundProducedNothingRead { return read }
			if _, err := f.state.finish(&f.log); err == nil {
				t.Fatal("the empty kind still ends with the nothing-to-deliver conclusion")
			}
			if want := devworkspace.MaxDeliveryRounds; f.attempts != want {
				t.Fatalf("attempts = %d, want %d — this ground must NOT be braked (%s)", f.attempts, want, read.Evidence)
			}
			if strings.Contains(f.log.String(), "NEVER produced a commit") {
				t.Fatalf("the brake must not fire:\n%s", f.log.String())
			}
		})
	}
}
