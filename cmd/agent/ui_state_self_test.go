package main

// ui_state_self_test.go — the verb's CLAIM rule, exercised through the verb's
// real entry point.
//
// THE TWO ARMS THESE CASES PIN, and why each needs a live PREMISE rather than
// only an assertion:
//
//  1. CLAIMED: two chat artifacts on one profile, both written by a surface that
//     is STILL RUNNING (this test process, and a second process started for the
//     purpose). The general rule cannot choose between them — it refuses, and the
//     case asserts that refusal FIRST, so the deterministic answer that follows
//     cannot be credited to "there was only one live surface anyway".
//  2. REFUSED: a published identity that matches NOTHING while several artifacts
//     are on hand must not resolve to a candidate. The negative control is made
//     explicit: the artifact a laxer rule would have substituted is present,
//     readable and live, and the case asserts the answer was a refusal by name and
//     that the remedy the refusal names (`--pid`) then works.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/uiclient"
)

// The fixture's vocabulary (Rule 1b: a case that typed these inline would be a
// second spelling of the identity's shape).
const (
	// ownIdentityPIDStranger is a pid no artifact records: the claim in the
	// refusal case must match NEITHER half, or the case would pass on the half
	// that did match.
	ownIdentityPIDStranger = 7_999_999
	// ownIdentityOtherSurface is the name carried by the stand-in process that
	// plays "the other chat surface" (gopi-shaped, because the reader corroborates
	// liveness with /proc/<pid>/cmdline).
	ownIdentityOtherSurface = "gopi-other-chat-surface"
	// ownIdentitySleepSeconds keeps that stand-in alive far past any assertion here.
	ownIdentitySleepSeconds = "60"
	// ownIdentityMatchBoth is the matching-half word uiclient.SelfMatchBy returns
	// when BOTH halves of a claim hold (its selfMatchByBoth is unexported and
	// internal to that package; this case asserts the same word arrives in the
	// verb's output).
	ownIdentityMatchBoth = "pid+session"
	// ownIdentityRecordLine is a minimal session record, so a dump's directory
	// resolves to a session (uiclient.SessionDirOfDump) — the fact session claims
	// are matched against.
	ownIdentityRecordLine = `{"type":"session_start","sessionId":"fixture"}` + "\n"
)

// ownChatFixture is one chat dump this suite installs.
type ownChatFixture struct {
	dir  string
	path string
	pid  int
}

// ownChatFixtureProfile pins the profile and scratch home so discovery sees ONLY
// this case's dumps, and returns the sessions root.
func ownChatFixtureProfile(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	profile := t.TempDir()
	t.Setenv("GOPI_HOME", profile)
	t.Setenv("GOPI_TEST_HOME", home)
	t.Setenv("GOPI_TEST_SCRATCH_HOME", filepath.Join(home, "workspace", "gopi-test-scratch"))
	// The channel is cleared for the general-rule control arms: a value inherited
	// from the developer's own shell must never decide a case.
	t.Setenv(config.ChatSurfaceIdentityEnv, "")
	root := filepath.Join(profile, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir the fixture sessions root: %v", err)
	}
	return root
}

// writeOwnChatDump writes a REAL chat dump (the product's own sink, chat surface
// marker included) into a fresh session directory under root, and returns it.
//
// The session record is written beside it because the SESSION half of a claim is
// resolved from the artifact's location (uiclient.SessionDirOfDump walks up to the
// record): without one, a dump has no session to be claimed by, and the case would
// silently be about the pid half alone.
func writeOwnChatDump(t *testing.T, root, sessionID string, pid int, started time.Time, line string) ownChatFixture {
	t.Helper()
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionlayout.RecordName), []byte(ownIdentityRecordLine), 0o644); err != nil {
		t.Fatalf("write the session record in %s: %v", dir, err)
	}
	path := filepath.Join(dir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		Cols: 60,
		Rows: 8,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              pid,
			Target:           "self:chat-tools",
			Tools:            true,
			ColsRows:         [2]int{60, 8},
			TerminalColsRows: [2]int{60, 8},
			Started:          started,
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + line + "\r\nstatus: idle\r\n")); err != nil {
		t.Fatalf("paint the fixture dump: %v", err)
	}
	//stylegate:allow-sleep lets the fixture's change-detecting sink take its frame before the artifact is read back
	time.Sleep(uiStateFixtureRefreshWait)
	if _, err := d.Write([]byte("\x1b[H" + line + "\x1b[3;1Hstatus: idle · turns 2\r\n")); err != nil {
		t.Fatalf("repaint the fixture dump: %v", err)
	}
	return ownChatFixture{dir: dir, path: path, pid: pid}
}

// ownChatOtherSurface starts a REAL second process and returns its (pid, start
// instant) pair, so an artifact it "wrote" reads back as a LIVE surface by the
// same /proc check that verifies this run's own. It is a plain sleeper, never a
// load generator (Rule 27), and it is killed with the case.
func ownChatOtherSurface(t *testing.T) (int, time.Time) {
	t.Helper()
	script := "exec -a " + ownIdentityOtherSurface + " sleep " + ownIdentitySleepSeconds
	cmd := exec.Command("bash", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a second process to act as the other chat surface: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()    //stylegate:ignore kills the ui_state child this test started once its dump was read
		_, _ = cmd.Process.Wait() //stylegate:ignore reaps that child so the fixture leaves no zombie behind
	})
	started, err := uiclient.ProcessStartTime(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("ProcessStartTime(%d): %v", cmd.Process.Pid, err)
	}
	return cmd.Process.Pid, started
}

// TestUIStateClaimsOwnChatDumpAmongTwoLiveSurfaces is the acceptance case for the
// verb side of the epic: on a profile with TWO live chat surfaces, the verb with
// no flags answers with THIS run's own dump — because the identity this run
// published to its children was inherited by the process that runs the verb.
func TestUIStateClaimsOwnChatDumpAmongTwoLiveSurfaces(t *testing.T) {
	root := ownChatFixtureProfile(t)
	started, err := uiclient.ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	otherPID, otherStarted := ownChatOtherSurface(t)
	mine := writeOwnChatDump(t, root, "aaaaaaaa-1111-4111-8111-111111111111", os.Getpid(), started, "the row only MY run painted")
	theirs := writeOwnChatDump(t, root, "bbbbbbbb-2222-4222-8222-222222222222", otherPID, otherStarted, "the row THEIR run painted")

	// PREMISE, asserted before the claim is set: with no identity published, the
	// general rule refuses over two live surfaces — so the answer below cannot be
	// credited to "there was only one live surface anyway".
	out, code := uiStateRun(t, []string{"--target", string(uiclient.SurfaceChat)})
	if code != uiStateExitAmbiguous {
		t.Fatalf("without an identity the verb exited %d, want the ambiguity refusal %d\n%s", code, uiStateExitAmbiguous, out)
	}
	for _, want := range []string{mine.path, theirs.path, "refusing to guess"} {
		if !strings.Contains(out, want) {
			t.Errorf("the premise refusal does not carry %q:\n%s", want, out)
		}
	}

	// The identity a chat run publishes to its children (the very value the bash
	// tool's environment carries into this process).
	t.Setenv(config.ChatSurfaceIdentityEnv, config.FormatChatSurfaceIdentity(os.Getpid(), mine.dir))
	out, code = uiStateRun(t, []string{"--target", string(uiclient.SurfaceChat)})
	if code != 0 {
		t.Fatalf("with this run's identity published the verb exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "dump="+mine.path) {
		t.Fatalf("the verb did not read this run's own artifact %s:\n%s", mine.path, out)
	}
	if strings.Contains(out, theirs.path) {
		t.Fatalf("the verb reported the OTHER surface's artifact:\n%s", out)
	}
	if !strings.Contains(out, "the row only MY run painted") {
		t.Fatalf("the grid is not this run's own screen:\n%s", out)
	}
	if !strings.Contains(out, "pid="+strconv.Itoa(os.Getpid())) {
		t.Fatalf("the header does not name this process's pid:\n%s", out)
	}
	if want := "by=" + ownIdentityMatchBoth; !strings.Contains(out, want) {
		t.Errorf("the observation does not say HOW the artifact was claimed (%s missing):\n%s", want, out)
	}
}

// TestUIStateRefusesAForeignIdentityNeverSubstitutes is the honesty half: an
// identity that matches NOTHING while several artifacts are on hand must be
// REFUSED, with the claim and the candidates named — never answered with a
// nearest candidate.
func TestUIStateRefusesAForeignIdentityNeverSubstitutes(t *testing.T) {
	root := ownChatFixtureProfile(t)
	started, err := uiclient.ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	otherPID, otherStarted := ownChatOtherSurface(t)
	mine := writeOwnChatDump(t, root, "cccccccc-3333-4333-8333-333333333333", os.Getpid(), started, "the row only MY run painted")
	theirs := writeOwnChatDump(t, root, "dddddddd-4444-4444-8444-444444444444", otherPID, otherStarted, "the row THEIR run painted")

	// A claim whose two halves match neither artifact: a pid no artifact records,
	// and a directory that is on no candidate.
	foreign := config.FormatChatSurfaceIdentity(ownIdentityPIDStranger, filepath.Join(root, "not-a-session-dir"))
	t.Setenv(config.ChatSurfaceIdentityEnv, foreign)

	out, code := uiStateRun(t, []string{"--target", string(uiclient.SurfaceChat)})
	if code == 0 {
		t.Fatalf("a foreign identity was accepted and answered with a screen:\n%s", out)
	}
	if !strings.Contains(out, "matches NONE") {
		t.Errorf("the refusal does not say the claim matched nothing:\n%s", out)
	}
	if !strings.Contains(out, "--pid") {
		t.Errorf("the refusal does not name its remedy (--pid):\n%s", out)
	}
	for _, cand := range []string{mine.path, theirs.path} {
		if !strings.Contains(out, cand) {
			t.Errorf("the refusal does not list the candidate %s:\n%s", cand, out)
		}
	}
	if strings.Contains(out, "grid_begin") {
		t.Errorf("a refusal also printed a screen body:\n%s", out)
	}

	// The remedy the refusal names WORKS, which is what makes the refusal useful
	// rather than merely honest: --pid still wins outright.
	out, code = uiStateRun(t, []string{"--target", string(uiclient.SurfaceChat), "--pid", strconv.Itoa(mine.pid)})
	if code != 0 {
		t.Fatalf("--pid after the refusal exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "dump="+mine.path) {
		t.Fatalf("--pid did not read the named surface's dump:\n%s", out)
	}
}

// TestUIStateOwnIdentityIsCarriedInJSON pins the machine rendering of the claim:
// a JSON caller must be able to learn HOW the artifact was claimed without parsing
// an English sentence out of another field.
func TestUIStateOwnIdentityIsCarriedInJSON(t *testing.T) {
	root := ownChatFixtureProfile(t)
	started, err := uiclient.ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	mine := writeOwnChatDump(t, root, "eeeeeeee-5555-4555-8555-555555555555", os.Getpid(), started, "the row only MY run painted")
	t.Setenv(config.ChatSurfaceIdentityEnv, config.FormatChatSurfaceIdentity(os.Getpid(), mine.dir))

	out, code := uiStateRun(t, []string{"--target", string(uiclient.SurfaceChat), "--json"})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, `"claim"`) {
		t.Errorf("the JSON rendering carries no claim field:\n%s", out)
	}
	if !strings.Contains(out, "by="+ownIdentityMatchBoth) {
		t.Errorf("the JSON claim does not name the matching half:\n%s", out)
	}
}
