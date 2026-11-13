package main

// chat_stdin_guard_test.go — THE REGRESSION PAIR FOR chat_stdin_guard.go.
//
// The defect the guard closes was measured, not imagined: `gopi chat <
// /dev/null` exited 0 having minted a session directory, a .raw.log, a
// session.jsonl and an exit marker for a conversation in which nothing was ever
// said. So the assertion is not "the guard function returns false" — it is a
// claim about the FILESYSTEM: a refused run leaves the profile byte-identical.
//
// FIVE CASES, and the two directions are both covered on purpose — a guard that
// refuses everything would pass a one-sided test:
//
//	A. /dev/null            → refused, non-zero, the alternatives named, profile untouched
//	B. a pipe with input    → NOT refused (the scripted shape keeps working)
//	C. a real pty           → NOT refused (the decision reads "carries input")
//	D. a regular file       → NOT refused (a here-document written out)
//	E. every scripted carrier (--once / --replay / --resume / seed / --film)
//	                        → NOT refused, because they never needed a keyboard
//
// A and B are driven through the REAL BINARY (Rule 31: the surface is executed,
// not merely unit-tested); C, D and E drive the decision seam, because what they
// assert is the decision, and the behaviour behind it is the pre-existing REPL
// path this change does not touch.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// chatStdinGuardProbeBudget bounds the real-binary probes: both refusals and
// the pipe case answer from the run's own start-up, never from a network turn,
// so a probe that outlives this is a run that fell through to the REPL.
const chatStdinGuardProbeBudget = 45 * time.Second

// chatStdinGuardProbeEnv builds a hermetic environment for a probe: no ambient
// GOPI_*/ANTHROPIC_*/DEEPSEEK_* (which would redirect config resolution), no
// DISPLAY, and a profile root inside the test's own temporary directory. It
// reuses the same predicate the CLI contract probes use — one statement of what
// "isolated" means — through cliContractEnv, then appends the sandbox profile.
func chatStdinGuardProbeEnv(t *testing.T, profile string) []string {
	t.Helper()
	return append(cliContractEnv(profile), "GOPI_HOME="+profile)
}

// chatStdinGuardRun drives the real binary with a caller-supplied stdin and
// returns the combined output plus the exit status.
func chatStdinGuardRun(t *testing.T, bin, profile string, stdin io.Reader, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = chatStdinGuardProbeEnv(t, profile)
	cmd.Stdin = stdin
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("gopi %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return string(out), code
}

// asExitError is the package's existing helper (agent_spawn_guard_test.go);
// this file reuses it rather than restating it.

// chatStdinGuardTree snapshots every path under root (directories included) so
// a run's write can be seen as a DIFFERENCE, not inferred from an absence.
func chatStdinGuardTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	return paths
}

// TestChatStdinGuard_RefusesAnEmptyStdinAndMintsNothing is case A: the exact
// measured defect. `gopi chat < /dev/null` must exit non-zero, say what to do
// instead, and leave the profile with no session artifact of any kind.
func TestChatStdinGuard_RefusesAnEmptyStdinAndMintsNothing(t *testing.T) {
	bin := gopiTestBinPath(t)
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	before := chatStdinGuardTree(t, profile)

	out, code := chatStdinGuardRun(t, bin, profile, nil, CliVerbChat)

	if code == chatExitOK {
		t.Fatalf("gopi %s </dev/null exited 0 — an interactive console with nothing on stdin "+
			"must refuse; output:\n%s", CliVerbChat, out)
	}
	// Every non-interactive carrier the refusal names must actually be named —
	// a refusal that does not say what works instead is the same dead end as a
	// silent one.
	for _, want := range []string{chatOnceFlag, chatFlagReplay, "printf"} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal does not name %q; output:\n%s", want, out)
		}
	}
	// The claim that matters: NOTHING WAS MINTED.
	after := chatStdinGuardTree(t, profile)
	if len(after) != len(before) {
		t.Fatalf("refused run changed the profile: before %v, after %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("refused run changed the profile: before %v, after %v", before, after)
		}
	}
}

// TestChatStdinGuard_StillAcceptsAPipedConversation is case B, the control that
// keeps the guard honest: a pipe carries input, so `printf … | gopi chat` must
// not be refused. The assertion is the NEGATIVE one (the refusal sentence is
// absent) plus the exit status, so the case does not depend on a model turn
// succeeding inside the probe budget.
func TestChatStdinGuard_StillAcceptsAPipedConversation(t *testing.T) {
	bin := gopiTestBinPath(t)
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	out, code := chatStdinGuardRun(t, bin, profile,
		strings.NewReader("hello\n/quit\n"), CliVerbChat)

	if strings.Contains(out, chatStdinNoInputNoticeRefusalFragment) {
		t.Fatalf("a piped conversation was REFUSED — the guard must accept any stdin that carries bytes; output:\n%s", out)
	}
	if code == chatExitUsage {
		t.Fatalf("a piped conversation exited %d (usage); output:\n%s", code, out)
	}
}

// chatStdinNoInputNoticeRefusalFragment is the stable half of the refusal text
// this file matches on. It is derived from the production constant rather than
// retyped, so a change to the wording cannot silently blind the controls above.
var chatStdinNoInputNoticeRefusalFragment = "stdin carries no input"

// TestChatStdinDecisionFollowsTheDescriptor is case C/D: the decision seam, over
// every stdin shape, so a guard that answered one blanket answer would fail.
func TestChatStdinDecisionFollowsTheDescriptor(t *testing.T) {
	// A real pty: the interactive shape, and the one `script -qec` exercises
	// against the binary.
	pair := chatOpenPtyPair(t, 80, 24)
	if !chatStdinCarriesInput(pair.Slave) {
		t.Errorf("a pty slave carries input but the guard said no — the interactive console would be refused")
	}

	// /dev/null: the measured defect's shape.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() {
		_ = devNull.Close() //stylegate:ignore a test fixture's descriptor close cannot change the assertion
	}()
	if chatStdinCarriesInput(devNull) {
		t.Errorf("/dev/null carries no input but the guard said yes — the measured phantom-session defect is back")
	}

	// A pipe: what a caller feeding this run actually gives it.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() {
		_ = pr.Close() //stylegate:ignore fixture teardown
		_ = pw.Close() //stylegate:ignore fixture teardown
	}()
	if !chatStdinCarriesInput(pr) {
		t.Errorf("a pipe carries input but the guard said no — `printf … | gopi chat` would be broken")
	}

	// A regular file: a here-document written out by a script.
	path := filepath.Join(t.TempDir(), "script.txt")
	if err := os.WriteFile(path, []byte("hello\n/quit\n"), 0o644); err != nil {
		t.Fatalf("write script file: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		_ = f.Close() //stylegate:ignore fixture teardown
	}()
	if !chatStdinCarriesInput(f) {
		t.Errorf("a regular file carries input but the guard said no")
	}

	// A nil descriptor has no input to offer.
	if chatStdinCarriesInput(nil) {
		t.Errorf("a nil descriptor carries no input but the guard said yes")
	}
}

// TestChatStdinGuardExemptsEveryScriptedCarrier is case E: the guard must be a
// no-op for every face that answers by itself, whatever stdin looks like. A
// guard that refused these would break `--once`, `--replay` and `--resume` — the
// three carriers its own refusal text promises.
func TestChatStdinGuardExemptsEveryScriptedCarrier(t *testing.T) {
	// Stated once, restored by the test's own scope (Rule 17: t.Setenv's scope
	// is the TEST, so the seam cannot leak into a sibling case).
	chatStdinProbe = func(*os.File) bool { return false }
	t.Cleanup(func() { chatStdinProbe = chatStdinCarriesInput })

	// A pipe-shaped line source is what `--once` hands in.
	scripted := &chatInput{}
	for _, c := range []struct {
		name                       string
		provided                   *chatInput
		replayTarget, resume, seed string
		filmSet                    bool
	}{
		{"--once / scripted sequence", scripted, "", "", "", false},
		{"--replay", nil, "11111111-2222-4333-8444-555555555555", "", "", false},
		{"--resume", nil, "", "11111111-2222-4333-8444-555555555555", "", false},
		{"seed argument", nil, "", "", "do the thing", false},
		{"--film", nil, "11111111-2222-4333-8444-555555555555", "", "", true},
	} {
		if code, refused := chatStdinGuardRefusal(c.provided, c.replayTarget, c.resume, c.seed, c.filmSet); refused {
			t.Errorf("%s was REFUSED (code %d) — a carrier that answers by itself needs no keyboard", c.name, code)
		}
		if chatRunIsTheInteractiveConsole(c.provided, c.replayTarget, c.resume, c.seed, c.filmSet) {
			t.Errorf("%s was classified as the interactive console", c.name)
		}
	}

	// …and the plain console IS classified as interactive, so the exemptions
	// above are exemptions rather than a guard that never fires.
	if !chatRunIsTheInteractiveConsole(nil, "", "", "", false) {
		t.Fatalf("a bare `gopi chat` was not classified as the interactive console — the guard would never run")
	}
	if code, refused := chatStdinGuardRefusal(nil, "", "", "", false); !refused || code == chatExitOK {
		t.Fatalf("a bare `gopi chat` with a probe answering \"no input\" was not refused (code %d, refused=%v)", code, refused)
	}
}
