package tools

// task_sync_git_lag_test.go — the regression guard for the STALENESS ALARM
// (task 01a13440, the cadence/face half of "bare `master` is not a revision").
//
// THE DEFECT THIS PINS. A git pass that reports every STEP as a success can
// still leave this host's integration ref frozen: measured 2026-10-13 on
// nick-sager, `refs/heads/master` sat 141 commits behind the peer's tip and was
// a purely ancestor of it, while the log of every pass said success. The
// number was invisible, so a report of the form "gate X is red on master" could
// not be re-checked against any revision and already-fixed defects read as
// still red (devdoc/bare-master-is-not-a-revision.md).
//
// So the pass now MEASURES the distance between its own integration ref and the
// peer's mirrored tip, records it (`GitPassReport.IntegrationLag`) and its
// failure rides the ordinary per-failed-step stderr passthrough
// (`gitSyncStepStaleWarning`). These cases drive REAL git in REAL repositories
// — the verdict is a ref's value and a count, so a text fixture would prove
// nothing — and they are PAIRED: the silent case must stay silent, or the
// alarm becomes noise nobody reads.

import (
	"fmt"
	"strings"
	"testing"
)

const (
	// gitSyncLagTestRole names the component whose tick fired the pass in these
	// cases. The RESIDENT labels live in cmd/agent (scratch_gc_tick.go) and are
	// not visible from this package; the alarm only renders the word into its
	// line, so the test's own label is enough — and keeping it a constant means
	// a reworded line is a compile-time error here, not a silently different
	// assertion (Rule 1b).
	gitSyncLagTestRole      = "worker"
	gitSyncLagTestSeedMsg   = "lag-fixture-seed"
	gitSyncLagTestLocalMsgs = 2 // local commits the peer does not have (=> diverged)
	gitSyncLagTestAhead     = 3 // commits the peer has and the client does not
)

// gitSyncLagFixture is a real client checkout with a real bare peer and the
// delivery lane's mirror ref (`refs/remotes/gitPushcheck/master`) pointing at the
// peer's tip — the state in which the lag is answerable.
type gitSyncLagFixture struct {
	Peer   string // bare peer repository (the pass's server target: a local path)
	Client string // the client checkout whose integration ref is measured
	Tip    string // the peer's master tip
}

// gitSyncLagBuildFixture builds it: a bare peer seeded through a scratch
// worktree, a client clone of it, `ahead` further commits on the peer, and the
// peer's tip mirrored into the client the way the delivery lane writes it
// (gitPeerMirrorRefspec — a FORCED fetch onto the reserved mirror ref, which no
// worktree holds).
func gitSyncLagBuildFixture(t *testing.T, ahead int) gitSyncLagFixture {
	t.Helper()
	root := t.TempDir()
	peer := root + "/peer.git"
	integrationClaimTestGit(t, root, "init", "-q", "--bare", "-b", IntegrationBranchName, peer)

	work := root + "/work"
	integrationClaimTestGit(t, root, "init", "-q", "-b", IntegrationBranchName, work)
	integrationClaimTestGit(t, work, "commit", "-q", "--allow-empty", "-m", gitSyncLagTestSeedMsg)
	integrationClaimTestGit(t, work, "push", "-q", peer, "refs/heads/"+IntegrationBranchName)

	// The client is cloned at the SEED, and only then does the peer move ahead:
	// a host frozen behind its peer is the state under test, and a clone taken
	// after the ahead-commits would start aligned and prove nothing.
	client := root + "/client"
	integrationClaimTestGit(t, root, "clone", "-q", "-b", IntegrationBranchName, peer, client)

	for i := 0; i < ahead; i++ {
		integrationClaimTestGit(t, work, "commit", "-q", "--allow-empty", "-m", "peer-ahead")
	}
	integrationClaimTestGit(t, work, "push", "-q", peer, "refs/heads/"+IntegrationBranchName)
	tip := integrationClaimTestGit(t, work, "rev-parse", "HEAD")
	// The mirror leg, exactly as the lane writes it (forced, onto the reserved
	// ref) — the measured premise of every case below.
	integrationClaimTestGit(t, client, "fetch", "-q", "--force", "--no-tags", peer,
		"+refs/heads/"+IntegrationBranchName+":"+gitSyncLagRef)
	return gitSyncLagFixture{Peer: peer, Client: client, Tip: tip}
}

// TestGitSyncClientPassClosesTheGapAndTheAlarmStaysSilent is the GREEN/happy
// leg, and the CONTROL that keeps the alarm credible: a real client pass against
// a real (local-path) peer ADVANCES the integration ref to the peer tip, and the
// alarm then says nothing at all. A pass that moved the ref and still warned
// would train the reader to ignore the word WARNING.
func TestGitSyncClientPassClosesTheGapAndTheAlarmStaysSilent(t *testing.T) {
	fx := gitSyncLagBuildFixture(t, gitSyncLagTestAhead)

	// The fixture must really BE the defect's shape before the pass runs: 3
	// behind AND purely so (ahead=0), which is what makes the pass's own
	// fast-forward the true prescription.
	lag, ahead, detail := gitSyncIntegrationLag(fx.Client)
	if lag != "3" || ahead != gitSyncLagAlignedValue {
		t.Fatalf("fixture: lag = %q ahead = %q, want \"3\"/\"0\" — detail %q", lag, ahead, detail)
	}

	rep := GitSyncClientPass(fx.Client, fx.Peer)
	if got := integrationClaimTestGit(t, fx.Client, "rev-parse", "refs/heads/"+IntegrationBranchName); got != fx.Tip {
		t.Fatalf("the pass left refs/heads/%s at %s, want the peer tip %s — steps: %s",
			IntegrationBranchName, got, fx.Tip, passStepSummary(rep))
	}
	if rep.IntegrationLag != "0" {
		t.Errorf("IntegrationLag = %q after the ref reached the peer tip, want \"0\" — steps: %s",
			rep.IntegrationLag, passStepSummary(rep))
	}
	if line, warn := rep.GitSyncStaleAlarm(gitSyncLagTestRole); warn {
		t.Errorf("the alarm fired on a pass that closed the gap: %q", line)
	}
	// The SUMMARY LINE must stay quiet too: `integration_lag=0` on every
	// healthy pass would be noise on the one line every face prints.
	if strings.Contains(rep.Line(), gitSyncLagSummaryField) {
		t.Errorf("the pass's summary line carries %s on an aligned host: %q", gitSyncLagSummaryField, rep.Line())
	}
	st := passStepNamed(rep, gitSyncStepStaleWarning)
	if st == nil {
		t.Fatalf("the pass recorded no %s step — the measurement is not on the report: %s",
			gitSyncStepStaleWarning, passStepSummary(rep))
	}
	if !st.OK {
		t.Errorf("%s step = FAIL (%q) on a pass whose ref reached the peer tip", gitSyncStepStaleWarning, st.Detail)
	}
}

// TestGitSyncClientPassAlarmNamesTheLocalWorkWhenThePairDiverged is the RED leg
// of the DIVERGENCE classification, and the defect it pins is the PRESCRIPTION,
// not the number. The client's integration ref carries commits the peer does
// not (the pass may not fast-forward it — client arms NEVER create a merge
// commit), so the gap survives the pass; the pre-fix alarm answered that with
// "`gopi task push` (this pass) advances it", i.e. it named the very pass whose
// client arm refuses to rebase/merge BY DESIGN — a prescription that can never
// succeed, repeated on every tick while the ref stayed frozen (measured
// 2026-10-13, filed as task 01a134ca). So this case asserts the THREE things a
// true prescription owes: the local-only count is NAMED, the fast-forward
// promise is ABSENT, and an executable next step is PRESENT.
func TestGitSyncClientPassAlarmNamesTheLocalWorkWhenThePairDiverged(t *testing.T) {
	fx := gitSyncLagBuildFixture(t, gitSyncLagTestAhead)
	// A local commit on the integration branch: the pair is now DIVERGED, so no
	// fast-forward is possible and the gap is not closable by this pass.
	for i := 0; i < gitSyncLagTestLocalMsgs; i++ {
		integrationClaimTestGit(t, fx.Client, "commit", "-q", "--allow-empty", "-m", "local-only")
	}
	localTip := integrationClaimTestGit(t, fx.Client, "rev-parse", "refs/heads/"+IntegrationBranchName)

	rep := GitSyncClientPass(fx.Client, fx.Peer)

	if got := integrationClaimTestGit(t, fx.Client, "rev-parse", "refs/heads/"+IntegrationBranchName); got != localTip {
		t.Fatalf("the client arm MOVED a diverged integration ref to %s (was %s) — the client never rebases the peer", got, localTip)
	}
	if rep.IntegrationLag == "0" || rep.IntegrationLag == gitSyncLagUnknown || rep.IntegrationLag == "" {
		t.Fatalf("IntegrationLag = %q on a host left behind the peer, want the measured count — steps: %s",
			rep.IntegrationLag, passStepSummary(rep))
	}
	if rep.IntegrationLag != "3" {
		t.Errorf("IntegrationLag = %q, want %q (the peer's commits this host lacks)", rep.IntegrationLag, "3")
	}
	line, warn := rep.GitSyncStaleAlarm(gitSyncLagTestRole)
	if !warn {
		t.Fatalf("no alarm on a pass that left refs/heads/%s %s commits behind — the freeze is silent again (Rule 15): %s",
			IntegrationBranchName, rep.IntegrationLag, passStepSummary(rep))
	}
	for _, want := range []string{"WARNING", rep.IntegrationLag, gitSyncLagRef, rep.IntegrationAhead, gitSyncLagManualMergeCommand} {
		if !strings.Contains(line, want) {
			t.Errorf("alarm line %q must contain %q (the behind count, the ref it was measured against, the LOCAL-ONLY count, and the executable next step)", line, want)
		}
	}
	// FAIL-WITHOUT-THE-FIX: the diverged prescription must NOT be the pass's own
	// command. `gopi task push` is the client arm that just refused, so naming it
	// here is the no-op advice this classification exists to remove.
	if strings.Contains(line, gitSyncLagPassPrescription) {
		t.Errorf("the diverged alarm names the pass's own command (%q) — on a diverged pair that pass cannot advance the ref, so the prescription is a no-op: %q",
			gitSyncLagPassPrescription, line)
	}
	// The summary line carries BOTH numbers, which is what makes the log line
	// self-classifying: 3 behind with ahead=0 is one pass away from aligned,
	// 3 behind with ahead=2 can never be closed by the pass.
	if !strings.Contains(rep.Line(), gitSyncLagAheadSummaryField+"="+rep.IntegrationAhead) {
		t.Errorf("the pass summary line %q must carry %s=%s", rep.Line(), gitSyncLagAheadSummaryField, rep.IntegrationAhead)
	}
	// The failed step is what carries the sentence to the CLI faces' stderr
	// passthrough — the alarm needs no channel of its own (Rule 35).
	st := passStepNamed(rep, gitSyncStepStaleWarning)
	if st == nil || st.OK {
		t.Fatalf("%s step must be recorded as FAILED when the gap survives; got %+v", gitSyncStepStaleWarning, st)
	}
	// and the same number is on the pass's ONE summary line, which is what an
	// operator greps a log for without knowing a step name exists.
	if !strings.Contains(rep.Line(), gitSyncLagSummaryField+"="+rep.IntegrationLag) {
		t.Errorf("the pass summary line %q must carry %s=%s", rep.Line(), gitSyncLagSummaryField, rep.IntegrationLag)
	}
	if !strings.Contains(st.Detail, rep.IntegrationLag) {
		t.Errorf("step detail %q must carry the count (%s)", st.Detail, rep.IntegrationLag)
	}
	if _, warn := rep.GitSyncStaleAlarm(gitSyncLagTestRole); !warn {
		t.Error("the alarm must render from the recorded step, not from a second measurement")
	}
}

// TestGitSyncClientPassAlarmKeepsThePassPrescriptionWhenPurelyBehind is the
// CONTROL, paired with the case above: on a pair that is behind and NOT
// diverged, the pass's own fast-forward really is the remedy, and the alarm's
// text must be byte-for-byte what it has always been — same sentence, same
// constant. Without this leg the classification could be "fixed" by weakening
// the working case (an alarm that never promises anything is as useless as one
// that lies), so the two are asserted against their own constants.
func TestGitSyncClientPassAlarmKeepsThePassPrescriptionWhenPurelyBehind(t *testing.T) {
	fx := gitSyncLagBuildFixture(t, gitSyncLagTestAhead)
	lag, ahead, detail := gitSyncIntegrationLag(fx.Client)
	if lag != "3" || ahead != gitSyncLagAlignedValue {
		t.Fatalf("fixture: lag = %q ahead = %q, want \"3\"/\"0\" (purely behind) — detail %q", lag, ahead, detail)
	}
	localRef := refsHeadsPrefix + IntegrationBranchName
	if want := fmt.Sprintf(gitSyncLagBehindFormat, localRef, lag, gitSyncLagRef); detail != want {
		t.Fatalf("purely-behind detail = %q, want the unchanged text %q", detail, want)
	}
	// The rendered alarm line carries that same sentence (the step's Detail is
	// the ONE rendering — Rule 14), and it DOES name the pass's command, because
	// on this shape the pass is the true remedy.
	line, warn := (&GitPassReport{Role: "client", IntegrationLag: lag, IntegrationAhead: ahead,
		Steps: []GitPassStep{{Step: gitSyncStepStaleWarning, Detail: detail}}}).GitSyncStaleAlarm(gitSyncLagTestRole)
	if !warn {
		t.Fatal("no alarm on a purely-behind pair")
	}
	if !strings.Contains(line, gitSyncLagPassPrescription) {
		t.Errorf("the purely-behind alarm must keep naming the pass that closes it (%q): %q", gitSyncLagPassPrescription, line)
	}
	if strings.Contains(line, gitSyncLagManualMergeCommand) {
		t.Errorf("the purely-behind alarm names the DIVERGED remedy (%q) — that is a merge commit the client arm never creates: %q",
			gitSyncLagManualMergeCommand, line)
	}
	// And the summary line stays exactly as it was: no divergence field when
	// there is no divergence (an operator's grep for integration_lag=3 must not
	// have to learn a second field for the ordinary shape).
	if strings.Contains(rep2Line(lag, ahead), gitSyncLagAheadSummaryField) {
		t.Errorf("a purely-behind summary line carries %s: %q", gitSyncLagAheadSummaryField, rep2Line(lag, ahead))
	}
}

// rep2Line renders the pass's ONE summary line for a client report carrying the
// given lag pair — the smallest report the summary renderer can classify.
func rep2Line(lag, ahead string) string {
	return (&GitPassReport{Role: "client", IntegrationLag: lag, IntegrationAhead: ahead}).Line()
}

// TestGitSyncIntegrationLagUnmeasurableIsNotAWarning pins the third state: a
// client that has never mirrored the peer (or an unreadable repo) gets
// `unmeasurable`, NOT a fabricated 0 and NOT a warning. A number nobody could
// take must not read as the all-clear (Rule 15a), and a warning about it would
// be noise.
func TestGitSyncIntegrationLagUnmeasurableIsNotAWarning(t *testing.T) {
	fx := gitSyncLagBuildFixture(t, gitSyncLagTestAhead)
	if code := integrationClaimTestGitExit(t, fx.Client, "update-ref", "-d", gitSyncLagRef); code != 0 {
		t.Fatalf("fixture: could not delete %s (exit %d)", gitSyncLagRef, code)
	}

	lag, ahead, detail := gitSyncIntegrationLag(fx.Client)
	if lag != gitSyncLagUnknown || ahead != gitSyncLagUnknown {
		t.Fatalf("lag = %q with no mirror ref, want %q (never a fabricated 0) — detail %q", lag, gitSyncLagUnknown, detail)
	}
	rep := &GitPassReport{Role: "client", IntegrationLag: lag}
	if line, warn := rep.GitSyncStaleAlarm(gitSyncLagTestRole); warn {
		t.Errorf("the alarm fired on an unmeasurable lag: %q", line)
	}
	// A pass that never measured anything (a skipped pass) must stay silent too:
	// the zero value of the field is "", and it is not the alarm's business.
	if line, warn := (&GitPassReport{Role: "client"}).GitSyncStaleAlarm(gitSyncLagTestRole); warn {
		t.Errorf("the alarm fired on a pass that measured nothing: %q", line)
	}
}

// TestGitSyncServerArmDoesNotInventTheLag is the honesty guard for the OTHER
// role: the ref the client compares against is the client's durable mirror of
// the SERVER's tip, while the same-named ref on a server is a pass-private
// reservation fetched from its peers — a different fact wearing the same name.
// So the server arm reports `unmeasurable` with that reason and never fails the
// step. Inventing a number there would be the silent-success shape this epic
// exists to end.
func TestGitSyncServerArmDoesNotInventTheLag(t *testing.T) {
	lag, detail := gitSyncServerLag()
	if lag != gitSyncLagUnknown {
		t.Fatalf("server lag = %q, want %q", lag, gitSyncLagUnknown)
	}
	if !strings.Contains(detail, IntegrationBranchName) {
		t.Errorf("the server reason must name the ref it declined to measure: %q", detail)
	}
}

// passStepNamed returns the report's step with that name (nil when it never
// ran), and passStepSummary renders every step for a failure message.
func passStepNamed(rep *GitPassReport, name string) *GitPassStep {
	for i := range rep.Steps {
		if rep.Steps[i].Step == name {
			return &rep.Steps[i]
		}
	}
	return nil
}

func passStepSummary(rep *GitPassReport) string {
	if rep == nil {
		return "(nil report)"
	}
	var b strings.Builder
	b.WriteString("lag=" + rep.IntegrationLag + " pull=" + rep.PullFF + " steps=[")
	for _, st := range rep.Steps {
		mark := "ok"
		if !st.OK {
			mark = "FAIL"
		}
		b.WriteString(mark + " " + st.Step + ": " + st.Detail + "; ")
	}
	b.WriteString("]")
	return b.String()
}
