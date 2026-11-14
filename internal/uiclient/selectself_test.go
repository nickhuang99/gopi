package uiclient

// selectself_test.go — the CLAIM rule: "which of these artifacts is mine?"
//
// WHY THESE CASES EXIST. A run reading its own screen (a chat surface's unarmed
// path, or `gopi ui_state` spawned as its child) has several candidate artifacts
// on an ordinary host and one fact a scan cannot derive: which surface it IS.
// The rule that answers with that fact must do three things a plausible
// implementation gets wrong — take the claim seriously, refuse rather than guess
// when the claim settles nothing, and never let a claim that matched NOTHING
// resolve to the nearest candidate. Each case below pins one of them against the
// real scanner (uiclient.Candidates over a fixture tree) rather than against a
// hand-built Candidate list: the resolver and the scanner must agree about what a
// candidate IS, and a hand-built list would agree with whatever the rule did.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// selfFixturePIDs are the two surfaces the cases below install. The first is a
// LIVE pid (this test process) — the identity check compares /proc against the
// record's own `started=` stamp, so only a real, currently-running pid can be
// asserted as "mine" — and the second is one the kernel provably cannot have
// (above the pid ceiling), so that surface's artifact reads as EXITED and a case
// can show that a live claim is not answered with a dead artifact.
const selfFixtureAbsentPID = 8_000_000

// selfFixtureStrangerPID is a pid no artifact in these fixtures records: the
// claim in the miss case must match NEITHER half, or the case would pass on the
// half that did match.
const selfFixtureStrangerPID = 7_999_999

// writeSelfChatDump writes a REAL chat dump (the product's sink) at path.
//
// It exists rather than re-using the suite's writeFixtureDump because the
// SURFACE marker is part of the identity: the reader refuses an artifact whose
// file NAME and whose record PREFIX disagree (ReadDump: the two halves
// disagreeing about the writer is reported, never resolved by picking a winner),
// and a fixture that left the marker at its default would install exactly that
// contradiction — a chat-screen.dump carrying a ui record — so every case here
// would have failed on the fixture instead of on the rule it pins.
func writeSelfChatDump(t *testing.T, path string, pid int, started time.Time, line string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	d, err := NewScreenDumper(DumpConfig{
		Path: path,
		Cols: 40,
		Rows: 6,
		Identity: DumpIdentity{
			Surface:          SurfaceChat,
			PID:              pid,
			Target:           "self:chat-tools",
			Tools:            true,
			ColsRows:         [2]int{40, 6},
			TerminalColsRows: [2]int{40, 6},
			Started:          started,
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}
	// The sink's throttle runs on its own clock, so the fixture advances it past
	// the interval rather than sleeping (the reader is what is under test here).
	fake := int64(0)
	d.now = func() time.Time {
		fake++
		return time.Unix(1_800_000_000+fake*3600, 0)
	}
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + line + "\r\nstatus: idle\r\n")); err != nil {
		t.Fatalf("write fixture dump: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the fixture dump was not installed at %s: %v", path, err)
	}
	return path
}

// liveForeignSurface starts a REAL second process and returns its pid together
// with that process's verifiable start instant, so an artifact it "wrote" reads
// back as LIVE by the same check that verifies this run's own.
//
// WHY A REAL PROCESS AND NOT A FABRICATED PID. The reader's liveness rule is
// (pid, start ticks) against /proc: a pid that is not running, or that is running
// with a different start instant, is NOT a live surface. A fixture that stamped a
// second artifact with this process's own pid would be claiming that one process
// wrote two artifacts in two session directories — an impossible shape that would
// make the case pass for the wrong reason (the pid half alone would answer it).
// The process is a plain `sleep`; it is NOT a load generator (Rule 27) and it is
// killed with the test.
func liveForeignSurface(t *testing.T) (int, time.Time) {
	t.Helper()
	// The stand-in is named as a gopi-shaped process (`exec -a`), because the
	// reader corroborates liveness with /proc/<pid>/cmdline: a plain `sleep` is a
	// LIVE pid whose command line is not a gopi surface at all, which the reader
	// reports as FOREIGN — a correct verdict, and the wrong fixture for a case
	// about two surfaces that are both really live.
	script := "exec -a " + foreignSurfaceArgv0 + " sleep " + sleepSeconds
	cmd := exec.Command("bash", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a second process to act as the other surface: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()    //stylegate:ignore kills the select-self probe process this fixture started once its claim was read
		_, _ = cmd.Process.Wait() //stylegate:ignore reaps the killed probe so the fixture leaves no zombie
	})
	started, err := ProcessStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("ProcessStartTime(%d): %v", cmd.Process.Pid, err)
	}
	return cmd.Process.Pid, started
}

// sleepSeconds is how long the stand-in surface lives — longer than any case here
// runs, so the pid is verifiably alive for the whole assertion — and
// foreignSurfaceArgv0 is the gopi-shaped name it carries (see above).
const (
	sleepSeconds        = "60"
	foreignSurfaceArgv0 = "gopi-other-surface"
)

// requireTwoLiveArtifacts asserts the premise this case rests on — BOTH artifacts
// read as live, so the general rule has an ambiguity it cannot resolve — and it
// retries the SCAN within a bound before it gives up.
//
// WHY A RETRY IS HONEST HERE, and not the case tolerating a wrong fixture. The
// liveness verdict is derived from /proc: the artifact's own (pid, started) stamp
// against the target's stat, AND its command line against the "is this a gopi
// surface" check (cmdlineIsGopi). A process that has forked but not yet exec'd
// has a READABLE BUT EMPTY cmdline, and an empty-but-present command line reads
// as "not a gopi surface" — the FOREIGN verdict — so for the microseconds of that
// window the reader is right to call the artifact's writer unverifiable, and the
// ambiguity this case is about does not yet exist. Measured: this case passed
// repeatedly standalone and failed inside the package budget, where the
// fork→exec window is wide enough to be observed (the reader's verdict is
// evidence of what it saw, never of what the process is).
//
// The retry re-reads the SAME artifacts through the same scanner; a real
// single-live profile stays single-live and still fails, with the per-candidate
// state printed so the failure says WHY. It is bounded by
// selfPremiseAttempts × selfPremiseWait, never an unbounded poll.
func requireTwoLiveArtifacts(t *testing.T, root string, cands []Candidate) []Candidate {
	t.Helper()
	var last []Candidate
	for attempt := 1; attempt <= selfPremiseAttempts; attempt++ {
		last = cands
		if _, liveErr := SelectLive(cands, false); liveErr != nil {
			return cands
		}
		if attempt < selfPremiseAttempts {
			time.Sleep(selfPremiseWait) //stylegate:allow-sleep bounded poll (selfPremiseAttempts × selfPremiseWait) for a just-forked process to finish exec'ing
			rescan, _, err := Candidates([]string{root}, time.Now())
			if err != nil {
				t.Fatalf("re-scanning %s: %v", root, err)
			}
			cands = rescan
		}
	}
	var b strings.Builder
	for _, c := range last {
		fmt.Fprintf(&b, "\n  pid=%d state=%s reason=%q path=%s", c.Record.PID, c.State, c.Reason, c.Path)
	}
	t.Fatalf("after %d scans the general rule still answers with a single live artifact; this case needs TWO (a real ambiguity) or it proves nothing%s",
		selfPremiseAttempts, b.String())
	return last
}

// selfPremiseAttempts / selfPremiseWait bound the fixture's premise retry, and
// the bound is deliberately wider than the observation it waits out: the
// fork→exec window is microseconds, while a genuinely single-live profile never
// turns double-live at any attempt (measured — `-count=30` green, and each
// attempt re-reads through the same scanner the rule uses).
const (
	selfPremiseAttempts = 4
	selfPremiseWait     = 50 * time.Millisecond
)

// selfFixtureTree builds a sessions tree with two chat dumps under a pinned
// profile and returns (sessionsRoot, the two session dirs, the candidates a real
// scan found).
func selfFixtureTree(t *testing.T) (string, string, string, []Candidate) {
	t.Helper()
	profile := t.TempDir()
	t.Setenv("GOPI_HOME", profile)
	root := filepath.Join(profile, config.ProfileDirName, config.SessionsDirName)
	mine := filepath.Join(root, "11111111-1111-4111-8111-111111111111")
	theirs := filepath.Join(root, "22222222-2222-4222-8222-222222222222")
	writeSessionRecord(t, filepath.Join(mine, "session.jsonl"))
	writeSessionRecord(t, filepath.Join(theirs, "session.jsonl"))
	// THIS run's surface: a live incarnation, stamped with this process's own
	// verifiable start instant (liveStartStamp's binding, computed here so the
	// case does not depend on another test file's helper).
	started, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	writeSelfChatDump(t, filepath.Join(mine, ChatDumpFileName), os.Getpid(), started, "the row only MY run painted")
	writeSelfChatDump(t, filepath.Join(theirs, ChatDumpFileName), selfFixtureAbsentPID,
		time.Now().Add(-time.Hour).UTC().Truncate(time.Second), "the row THEIR run painted")
	cands, _, err := Candidates([]string{root}, time.Now())
	if err != nil {
		t.Fatalf("Candidates(%s): %v", root, err)
	}
	if len(cands) != 2 {
		t.Fatalf("the fixture scan found %d candidates, want 2", len(cands))
	}
	return root, mine, theirs, cands
}

// TestSelectBySelf_ClaimTakesThisRunsArtifact is the acceptance case for the
// deterministic arm: with TWO chat artifacts on hand — one of them this process's
// own (pid + session directory), the other an exited stranger's — the claim
// selects THIS run's artifact.
//
// It fails on the rule this replaced (SelectLive): the two artifacts are not both
// live, so SelectLive would answer with the one live candidate too — which is
// precisely why the case ALSO asserts the selection is the claimed one by PATH and
// pid, and a second case below covers the two-LIVE shape where SelectLive refuses.
func TestSelectBySelf_ClaimTakesThisRunsArtifact(t *testing.T) {
	_, mine, _, cands := selfFixtureTree(t)
	claim := SelfIdentity{PID: os.Getpid(), SessionDir: mine}

	sel, available, err := SelectBySelf(cands, claim, false)
	if !available {
		t.Fatal("the rule reported itself unavailable with a non-empty claim")
	}
	if err != nil {
		t.Fatalf("the claim was refused: %v", err)
	}
	if got := filepath.Dir(sel.Candidate.Path); got != mine {
		t.Fatalf("the claim selected %s, want this run's own artifact under %s", sel.Candidate.Path, mine)
	}
	if sel.Candidate.Record.PID != os.Getpid() {
		t.Fatalf("the selected artifact records pid %d, want this process's %d", sel.Candidate.Record.PID, os.Getpid())
	}
	if !strings.Contains(sel.Candidate.Dump.Text(), "only MY run painted") {
		t.Fatalf("the selected artifact is not this run's screen:\n%s", sel.Candidate.Dump.Text())
	}
	// And the half that matched is reportable: the caller must be able to say HOW
	// it was identified rather than only that it was.
	if by := SelfMatchBy(sel.Candidate, claim); by != selfMatchByBoth {
		t.Fatalf("SelfMatchBy = %q, want %q (the fixture matches on both halves)", by, selfMatchByBoth)
	}
}

// TestSelectBySelf_TwoLiveArtifactsClaimDecides is the case the general rule
// cannot answer, and the reason this rule exists: TWO artifacts that are BOTH
// live (the operator's chat beside the agent's) are an ambiguity to SelectLive
// whatever the caller knows — while a caller whose claim was PUBLISHED to it
// resolves it deterministically.
//
// Both fixtures carry this process's pid and start stamp, so both read as live;
// only the session directory distinguishes them, which is exactly the fact a
// process boundary destroys and the identity channel restores.
func TestSelectBySelf_TwoLiveArtifactsClaimDecides(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("GOPI_HOME", profile)
	root := filepath.Join(profile, config.ProfileDirName, config.SessionsDirName)
	mine := filepath.Join(root, "33333333-3333-4333-8333-333333333333")
	theirs := filepath.Join(root, "44444444-4444-4444-8444-444444444444")
	started, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	// Both runs own a session (the record is what makes a dump's directory
	// resolvable to a session at all — SessionDirOfDump), because the fact this
	// case is about is "two live surfaces with two different sessions".
	writeSessionRecord(t, filepath.Join(mine, "session.jsonl"))
	writeSessionRecord(t, filepath.Join(theirs, "session.jsonl"))
	foreignPID, foreignStarted := liveForeignSurface(t)
	writeSelfChatDump(t, filepath.Join(mine, ChatDumpFileName), os.Getpid(), started, "row: mine")
	writeSelfChatDump(t, filepath.Join(theirs, ChatDumpFileName), foreignPID, foreignStarted, "row: theirs")
	cands, _, err := Candidates([]string{root}, time.Now())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("scan found %d candidates, want 2", len(cands))
	}
	// The premise: the general rule does NOT answer this — it refuses.
	cands = requireTwoLiveArtifacts(t, root, cands)

	// The claim resolves it.
	sel, _, err := SelectBySelf(cands, SelfIdentity{PID: os.Getpid(), SessionDir: mine}, false)
	if err != nil {
		t.Fatalf("the claim was refused over two live artifacts: %v", err)
	}
	if filepath.Dir(sel.Candidate.Path) != mine {
		t.Fatalf("the claim selected %s, want this run's own artifact under %s", sel.Candidate.Path, mine)
	}
	if by := SelfMatchBy(sel.Candidate, SelfIdentity{SessionDir: mine}); by != selfMatchBySession {
		t.Fatalf("a session-only claim matched by %q, want %q", by, selfMatchBySession)
	}
}

// TestSelectBySelf_ForeignClaimIsRefusedNotSubstituted is the honesty half, and
// the one that matters most: a claim that matches NOTHING while several artifacts
// are on hand must REFUSE — never fall back to "the one I found".
//
// The negative control is explicit: the fixture contains a perfectly good,
// readable, live artifact, so an implementation that resolved the failure by
// answering with it would pass a laxer assertion and is caught here by PATH.
func TestSelectBySelf_ForeignClaimIsRefusedNotSubstituted(t *testing.T) {
	_, mine, theirs, cands := selfFixtureTree(t)
	// A claim neither half of which matches: the pid is one no artifact records,
	// and the directory is on no candidate — the caller believes it is a surface
	// this scan cannot see (a different profile, a run that has moved on). Leaving
	// this process's REAL pid in would have made the case pass without exercising
	// the miss: the pid half legitimately answers this run's own artifact, which is
	// a different case (pinned above), not a claim that matched nothing.
	foreign := SelfIdentity{PID: selfFixtureStrangerPID, SessionDir: filepath.Join(theirs, "not-a-session-dir")}

	sel, available, err := SelectBySelf(cands, foreign, false)
	if !available {
		t.Fatal("the rule reported itself unavailable with a non-empty claim")
	}
	if err == nil {
		t.Fatalf("a claim matching nothing was answered with %s", sel.Candidate.Path)
	}
	var miss *SelfIdentityMissError
	if !errors.As(err, &miss) {
		t.Fatalf("the refusal is %T (%v), want *SelfIdentityMissError", err, err)
	}
	if !SelfIdentityMiss(err) {
		t.Error("SelfIdentityMiss does not recognise its own refusal type")
	}
	if sel.Candidate.Path != "" {
		t.Fatalf("a refused selection still carries the candidate %s", sel.Candidate.Path)
	}
	for _, want := range []string{"matches NONE", "--pid", "pid="} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, err.Error())
		}
	}
	if !strings.Contains(err.Error(), mine) && !strings.Contains(err.Error(), theirs) {
		t.Errorf("the refusal names no candidate path:\n%s", err.Error())
	}
	// The negative control, stated: the artifact a substitution would have
	// picked IS present and readable.
	if !strings.Contains(strings.Join(pathsOf(cands), " "), mine) {
		t.Fatal("the fixture lost the artifact a laxer rule would have substituted")
	}
}

// TestSelectBySelf_SingleArtifactIsAnsweredEvenOnAMiss pins the one deliberate
// exception: with exactly ONE artifact on hand there is nothing to confuse it
// with, so a claim that matches nothing still answers with it — the same arm
// SelectBySession takes, and the behaviour every caller had before this rule
// existed (a run whose identity could not be published keeps working).
func TestSelectBySelf_SingleArtifactIsAnsweredEvenOnAMiss(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("GOPI_HOME", profile)
	root := filepath.Join(profile, config.ProfileDirName, config.SessionsDirName)
	only := filepath.Join(root, "55555555-5555-4555-8555-555555555555")
	writeSessionRecord(t, filepath.Join(only, "session.jsonl"))
	writeSelfChatDump(t, filepath.Join(only, ChatDumpFileName), selfFixtureAbsentPID,
		time.Now().Add(-time.Hour).UTC().Truncate(time.Second), "the only row")
	cands, _, err := Candidates([]string{root}, time.Now())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	sel, available, err := SelectBySelf(cands, SelfIdentity{PID: os.Getpid()}, false)
	if !available || err != nil {
		t.Fatalf("available=%t err=%v, want the scarcity arm to answer", available, err)
	}
	if filepath.Dir(sel.Candidate.Path) != only {
		t.Fatalf("selected %s, want the only artifact on hand (%s)", sel.Candidate.Path, only)
	}
	if by := SelfMatchBy(sel.Candidate, SelfIdentity{PID: os.Getpid()}); by != "" {
		t.Fatalf("SelfMatchBy = %q for an unclaimed artifact, want the empty answer", by)
	}
}

// TestSelectBySelf_NoClaimMeansTheGeneralRule pins the boundary between the two
// rules: with an EMPTY claim the rule reports itself UNAPPLIED (so a caller falls
// back to SelectLive and says nothing about a claim), rather than reporting a miss
// over a claim the caller never made.
func TestSelectBySelf_NoClaimMeansTheGeneralRule(t *testing.T) {
	_, _, _, cands := selfFixtureTree(t)
	sel, available, err := SelectBySelf(cands, SelfIdentity{}, false)
	if available {
		t.Error("the rule reported itself APPLIED with an empty claim")
	}
	if err != nil {
		t.Errorf("an empty claim produced the error %v", err)
	}
	if sel.Candidate.Path != "" {
		t.Errorf("an unapplied rule chose %s", sel.Candidate.Path)
	}
}

// TestSelectBySelf_ClaimMatchingTwoArtifactsRefuses is the third arm — a claim
// that matches MORE than one artifact. Two artifacts the caller's own claim
// covers are two screens it cannot choose between, whatever the claim says, so
// the answer is the ambiguity refusal rather than the first match.
//
// The shape is real: a run whose dump was re-written by a respawn (two artifacts
// in one session directory), or two dumps the same directory walk resolves to one
// session.
func TestSelectBySelf_ClaimMatchingTwoArtifactsRefuses(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("GOPI_HOME", profile)
	root := filepath.Join(profile, config.ProfileDirName, config.SessionsDirName)
	mine := filepath.Join(root, "66666666-6666-4666-8666-666666666666")
	writeSessionRecord(t, filepath.Join(mine, "session.jsonl"))
	started, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	writeSelfChatDump(t, filepath.Join(mine, ChatDumpFileName), os.Getpid(), started, "row a")
	// A SECOND artifact that resolves to the SAME session directory: one level
	// down, the way a recipe's artifact subdirectory holds a copy. SessionDirOfDump
	// walks up to the session, so BOTH candidates answer the one directory — which
	// is the shape this case is about.
	writeSelfChatDump(t, filepath.Join(mine, "run1", ChatDumpFileName), os.Getpid(), started, "row b")
	cands, _, err := Candidates([]string{root}, time.Now())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("scan found %d candidates, want 2", len(cands))
	}
	_, _, err = SelectBySelf(cands, SelfIdentity{SessionDir: mine}, false)
	var ambiguous *AmbiguityError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("two artifacts claimed by one directory gave %v, want *AmbiguityError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("the refusal lists %d candidates, want both", len(ambiguous.Candidates))
	}
}

// TestSelectBySelf_NoArtifactsIsTheAbsenceError pins the last arm: a claim with
// nothing to claim from is the ABSENCE of a dump (ErrNoCandidates), not a miss —
// the two are different facts and a caller renders them with different routes out.
func TestSelectBySelf_NoArtifactsIsTheAbsenceError(t *testing.T) {
	_, available, err := SelectBySelf(nil, SelfIdentity{PID: os.Getpid()}, false)
	if !available {
		t.Fatal("the rule reported itself unavailable with a non-empty claim")
	}
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("err = %v, want ErrNoCandidates", err)
	}
	if SelfIdentityMiss(err) {
		t.Error("the absence of a dump was reported as a claim that missed")
	}
}

// pathsOf renders the candidate paths, so a failure message names what the scan
// actually found rather than a count.
func pathsOf(cands []Candidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Path)
	}
	return out
}
