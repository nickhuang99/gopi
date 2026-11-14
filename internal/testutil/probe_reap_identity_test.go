package testutil

// probe_reap_identity_test.go — the reap of this package's leaked probe workers
// addresses a PROCESS, never a pid NUMBER (Rule 42).
//
// THE DEFECT (measured 2026-10-27). `reapProbeWorkers` collected bare `[]int`
// out of a `comm == "gopi"` scan and delivered with `syscall.Kill(pid, sig)`.
// A pid is a number the kernel recycles, so between the scan and the signal that
// number can name a completely different process — and the reap retries across a
// five-second poll loop, which is a wide window for exactly that. The delivery
// still "succeeds" (kill(2) has no idea which process it reached) and nothing in
// the output can tell an operator that the sweep of a test's own leaked worker
// stopped somebody else's process instead.
//
// This tree already has the ONE guarded delivery — `KillIfIdentical`, which
// re-reads pid+starttime immediately before the signal, refuses a reused pid,
// and refuses a systemd user instance whose SIGINT ends the operator's whole
// desktop session (38 call sites before this fix; not one of them was the reap).
//
// WHAT IS ASSERTED, and why each arm is a criterion rather than a nicety:
//
//	1. THE SOURCE CARRIES NO BARE-NUMBER DELIVERY — the defect shape is one
//	   inline `syscall.Kill(pid, …)` line that no behavioural assertion would
//	   notice coming back, so it is read out of the file (Rule 41: a finding is
//	   only closed when a machine can see it). This arm is RED on the pre-fix
//	   file (two occurrences) and GREEN after.
//	2. THE GUARD REFUSES A RECYCLED IDENTITY — the forged starttime is the
//	   shape a recycled pid presents, so this measures the discriminator itself
//	   rather than "some check exists"; the refusal is CONFIRMED by reading
//	   /proc and requiring the live child still running.
//	3. THE GUARD STILL DELIVERS TO THE REAL IDENTITY — without this arm, arm 2
//	   could be green because the delivery path is broken or because nothing
//	   here dies of a signal. It is the negative control that makes arm 2 mean
//	   something.
//	4. THE SEAM IS WHAT THE REAP CALLS — the reap's own delivery must go through
//	   probeReapDelivery; a sourced gate that the function exists but is unused
//	   would be a vacuum (Rule 36: a green that measured nothing).

import (
	"bytes"
	"errors"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// probeReapSourceFile is the file whose delivery shape arm 1 judges. It is
	// the reap's own file: the scan, the poll loop and the signal all live there.
	probeReapSourceFile = "glm_night_watch_test.go"
	// probeReapBareDeliveryForm is the banned spelling. It is the whole defect
	// in one string: a signal addressed to a NUMBER.
	probeReapBareDeliveryForm = "syscall.Kill(pid"
	// probeReapSeamForm is the guarded seam the reap must call instead.
	probeReapSeamForm = "probeReapDelivery("
	// probeReapFixtureThrows is the fixture binary and
	// probeReapFixtureSeconds how long it lives — far longer than this test, so
	// "still alive after a refused signal" is an observation and not a race
	// against a natural exit.
	probeReapFixtureThrows  = "sleep"
	probeReapFixtureSeconds = "30"
)

// TestProbeReapCarriesNoBareNumberDelivery is arm 1: the file that reaps this
// package's leaked workers does not address a signal to a pid alone.
func TestProbeReapCarriesNoBareNumberDelivery(t *testing.T) {
	src, err := os.ReadFile(probeReapSourceFile)
	if err != nil {
		t.Fatalf("read %s: %v — the arm cannot judge a file it did not read", probeReapSourceFile, err)
	}
	text := string(src)

	// THE VACUUM GUARD FIRST: an empty or truncated read would make every count
	// below zero and pass silently. The reap's own scan line is the witness that
	// this is really the file under judgement.
	const reapWitness = `strings.TrimSpace(string(comm)) != procname.NameGopi`
	if !strings.Contains(text, reapWitness) {
		t.Fatalf("%s does not carry the comm==%q scan this arm is about (read %d bytes) — the file moved and this gate is now judging nothing",
			probeReapSourceFile, "gopi", len(text))
	}

	// THE JUDGEMENT IS MADE ON CODE, NOT ON PROSE (Rule 42's own recognizer
	// rule: a cite is not an invocation). The file EXPLAINS this defect in a
	// comment that necessarily quotes the banned spelling — a naive substring
	// count would turn that explanation into a violation and the honest fix
	// would be to stop writing the explanation down. So the source is parsed and
	// re-printed WITHOUT its comments, and only the resulting program text is
	// judged.
	code := probeReapCodeWithoutComments(t, src)

	if got := strings.Count(code, probeReapBareDeliveryForm); got != 0 {
		t.Errorf("%s carries %d bare %q delivery(ies) — a pid is a NUMBER THE KERNEL RECYCLES, and the reap holds its candidates across a five-second poll loop; the delivery must go through %s, which takes pid+starttime and refuses a reused pid by name (Rule 42)",
			probeReapSourceFile, got, probeReapBareDeliveryForm, probeReapSeamForm)
	}
	if got := strings.Count(code, "syscall.Kill("); got != 0 {
		t.Errorf("%s still calls syscall.Kill %d time(s) — the only delivery from this file is %s (measured: neither remaining use can be justified; a bare kill here reaches whatever now owns the number)",
			probeReapSourceFile, got, probeReapSeamForm)
	}
	if got := strings.Count(code, probeReapSeamForm); got == 0 {
		t.Errorf("%s never calls %s — the reap would then deliver no signal at all, and this gate would be measuring an unused seam (Rule 36: a green that measured nothing)",
			probeReapSourceFile, probeReapSeamForm)
	}
}

// TestProbeReapDeliveryRefusesARecycledIdentity is arms 2-3: the seam refuses a
// forged incarnation of a live process, and still delivers to the real one.
func TestProbeReapDeliveryRefusesARecycledIdentity(t *testing.T) {
	mp, live := probeReapFixture(t)

	// ── (a) A FORGED STARTTIME IS REFUSED ────────────────────────────────────
	//
	// Same pid, starttime + 1. The kernel never hands one (pid, starttime) pair
	// to two processes, so this IS the shape a recycled pid presents.
	forged := live
	forged.StartTime = live.StartTime + 1
	err := probeReapDelivery(forged, probeReapOwnProfile(t), syscall.SIGTERM)
	if err == nil {
		t.Fatalf("the reap seam DELIVERED to pid %d on a forged starttime (%d, recorded %d) — that is the recycled-pid path, and it ends at whoever owns the number",
			forged.Pid, forged.StartTime, live.StartTime)
	}
	// THE REFUSAL MUST BE ABOUT THE INCARNATION, whatever wording carries it.
	// Two seams can answer here — this file's own incarnation re-read and
	// KillIfIdentical's — so the assertion is on the MEANING (the incarnation /
	// the recycled number), not on one seam's phrasing. A refusal for any other
	// reason leaves the recycled-pid path open while this arm reads green.
	if msg := err.Error(); !strings.Contains(msg, "incarnation") && !strings.Contains(msg, "identity changed") {
		t.Errorf("the refusal came from a cause that is not the incarnation: %v\n— an unrelated refusal leaves the recycled-pid path open while this arm reads green", err)
	}
	// THE REFUSAL IS MEASURED, not taken from the error return.
	if alive, why := live.Alive(); !alive {
		t.Fatalf("the fixture child (pid %d) died or changed under a REFUSED signal: %s — the seam did not actually withhold delivery", live.Pid, why)
	}

	// ── (a2) OWNERSHIP IS A SECOND QUESTION, AND IT IS ASKED ─────────────────
	//
	// The incarnation test ALONE is not ownership: measured on this host
	// 2026-10-27, KillIfIdentical(<a live process the caller never started>, 0)
	// returns nil — it agrees about anyone whose starttime matches its own
	// reading. So a live, correctly-identified child must STILL be refused when
	// the reap was told to clean a profile that child does not belong to. This
	// is the arm that would have caught a guard which only re-read starttime.
	foreignProfile := filepath.Join(t.TempDir(), "not-this-reaps-profile")
	if err := probeReapDelivery(live, foreignProfile, syscall.SIGTERM); err == nil {
		t.Fatalf("the reap seam DELIVERED to pid %d (a live child, exact incarnation) while told to clean profile %s — the child belongs to %s, so this is a signal to a process the caller does not own (Rule 42)",
			live.Pid, foreignProfile, probeReapOwnProfile(t))
	} else if !strings.Contains(err.Error(), "not this reap's to stop") {
		t.Errorf("the ownership refusal must say it is about ownership, got: %v", err)
	}
	if alive, why := live.Alive(); !alive {
		t.Fatalf("the fixture child (pid %d) died under a REFUSED (foreign-profile) signal: %s — the seam delivered anyway", live.Pid, why)
	}

	// ── (b) THE REAL IDENTITY STILL DELIVERS ─────────────────────────────────
	//
	// Otherwise a seam that refuses EVERYTHING (an unreadable /proc, a
	// too-strict comparison) would pass (a) too, and every reap would become a
	// silent leak — the defect the reap exists to close.
	if err := probeReapDelivery(live, probeReapOwnProfile(t), syscall.SIGTERM); err != nil {
		t.Fatalf("the reap seam refused the RECORDED incarnation of a live process (pid %d): %v — a seam that delivers nothing turns every reap into a leak", live.Pid, err)
	}
	st := probeReapObserveDeath(t, mp, live.Pid)
	if !st.Signaled() || st.Signal() != syscall.SIGTERM {
		t.Fatalf("the guarded delivery ended the fixture in %+v, want a signal death by SIGTERM — the guard runs BEFORE the delivery, so a broken send must not read as a successful stop", st)
	}
}

// probeReapFixture starts a child of this test through the package's SANCTIONED
// spawn helper and captures its identity — the one process on this host whose
// delivery is provably the caller's to make.
//
// StartMockProcess (not exec.Command) is not a style preference here: it arms
// Pdeathsig and an own process group, so a fixture cannot outlive a SIGKILLed
// test binary and stand in this checkout forever; fixture_spawn_gate_test.go
// fails the build on a hand-rolled long-lived `sleep`.
func probeReapFixture(t *testing.T) (*MockProcess, ProcIdentity) {
	t.Helper()
	mp := StartMockProcess(t, probeReapFixtureThrows, probeReapFixtureSeconds)
	pid := mp.Pid()
	if pid <= 0 {
		t.Fatalf("the fixture child did not start (pid %d)", pid)
	}
	// AwaitProcIdentity, not ReadProcIdentity: a just-spawned child's argv is
	// published by the kernel a beat after fork, and a half-filled identity
	// would read as "reused" against its own process.
	id, err := AwaitProcIdentity(pid)
	if err != nil {
		t.Fatalf("capture the identity of the fixture child (pid %d): %v", pid, err)
	}
	return mp, id
}

// probeReapObserveDeath waits for the fixture to end and returns its wait
// status — the observation that a DELIVERED signal really landed. It goes
// through MockProcess.Wait, which does NOT signal anything, so the death it
// reports is the one the seam under test caused.
func probeReapObserveDeath(t *testing.T, mp *MockProcess, pid int) syscall.WaitStatus {
	t.Helper()
	err := mp.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the fixture (pid %d) ended without an exit status (err=%v) — a delivery the seam reported as successful did not kill it", pid, err)
	}
	st, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("the fixture (pid %d) exit status is not a syscall.WaitStatus: %T", pid, exitErr.Sys())
	}
	return st
}

// probeReapCodeWithoutComments re-prints a Go source file with its comments
// dropped, so a gate can judge what the file DOES without punishing it for
// explaining what it does not do.
//
// The AST is parsed WITHOUT parser.ParseComments, so comments are never
// attached and the printer cannot emit them; a parse failure is a REFUSAL, not
// a pass — a gate that silently judged an empty string would read green on a
// file it could not read at all (Rule 36).
func probeReapCodeWithoutComments(t *testing.T, src []byte) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, probeReapSourceFile, src, 0)
	if err != nil {
		t.Fatalf("parse %s (read %d bytes): %v — the gate refuses to judge a file it could not parse", probeReapSourceFile, len(src), err)
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		t.Fatalf("re-print %s without comments: %v", probeReapSourceFile, err)
	}
	return buf.String()
}

// probeReapOwnProfile is the profile a process started BY THIS TEST belongs to,
// resolved through the same function the production scan uses
// (config.GopiDirFromEnviron over the process's own environment) — so the arm
// exercises the real predicate rather than a hand-built string.
func probeReapOwnProfile(t *testing.T) string {
	t.Helper()
	dir := config.GopiDirFromEnviron(os.Environ())
	if strings.TrimSpace(dir) == "" {
		t.Fatalf("this test's own environment resolves to no gopi profile — the ownership arm cannot be driven from a value it did not measure")
	}
	return dir
}
