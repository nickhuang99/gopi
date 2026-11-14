package main

// chat_ground_replay_readonly_notice_test.go — THE START-UP LINE A REPLAY MUST
// NOT PRINT AS IF IT WERE AN ORDINARY CHAT (row 01a1a629; measured 2026-11-04).
//
// THE DEFECT, MEASURED. `gopi chat --replay <id>` printed, on stderr, byte for
// byte what an ordinary chat prints:
//
//	[gopi] chat: this session writes in /home/user/work/gopi — the MAIN CHECKOUT
//	of /home/user/work/gopi. No worktree and no branch of its own: every
//	read/write/edit/bash round lands uncommitted in that checkout's working
//	area.
//
// Every clause of that sentence is false under `--replay`: internal/replaymode
// refuses the session mint, the slot registration, the editor history and every
// tool round's effect, and the refusal catalogue printed two lines below says so.
// The line was written against a criterion that does not cover this run — "is the
// cwd a git checkout?" (tools.ChatGroundFor) — so a run that writes NOTHING was
// told where its writes land.
//
// WHY THIS IS NOT A UNIT CASE (Rule 31). The claim is not "a bool is set" — it is
// that the LINE THE BINARY PRINTS changes. So the case drives the REAL entry point
// (chatGroundReport, the one function cmd/agent/cli_chat.go calls at start-up,
// which prints through the same renderer the operator's stderr carries) in a REAL
// git checkout, with the replay flag ARMED for real (replaymode.Arm), and reads
// what it wrote to os.Stderr.
//
// THE CONTROL ARM IS WHAT MAKES IT EVIDENCE, in both directions:
//
//   - NOT replaying ⇒ the ordinary sentence must appear VERBATIM, and it must not
//     have been softened into the read-only one. Without this arm a fix that
//     deleted the line for every run would pass for the right reason and be
//     wrong for every ordinary chat (the defect the operator's criterion names:
//     "原本正确的措辞未被改坏").
//   - replaying ⇒ the ordinary sentence must be ABSENT (the assertion is on the
//     three phrases that state a write, not on the sentence's punctuation, so a
//     future rewording cannot quietly restore the claim) and the read-only family
//     must name the directory and say this run writes nothing.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatGroundReplayOrdinaryWriteClaims are the phrases of the ORDINARY line that
// state a write. They are quoted from the production constants rather than
// re-derived, and the assertion is "none of these may appear while a replay is
// armed" — a criterion about the CLAIM, so a reworded line that keeps the claim
// is still caught.
var chatGroundReplayOrdinaryWriteClaims = []string{
	"this session writes in",
	"lands uncommitted",
	"no branch to attribute the work to",
}

// chatGroundReplayReadOnlyClaims are the phrases the read-only family must carry:
// what this run is, and what it does NOT do. "NOTHING" is capitalised in the
// production text so a reader skimming a console cannot mistake it for the
// ordinary line's "writes in".
var chatGroundReplayReadOnlyClaims = []string{
	"writes NOTHING",
	"REPLAY",
	"read-only",
}

// chatGroundNoticeRepo builds a REAL git repository at a temp path and returns
// it. The line under test branches on a fact about the FILESYSTEM (is this
// directory a work tree's main checkout?), so a fixture that faked the answer
// would be asserting its own stub — the same reason
// internal/tools/chat_ground_test.go builds one.
func chatGroundNoticeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed: the main-checkout shape cannot be built")
	}
	repo := filepath.Join(t.TempDir(), "chat-ground-notice-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"checkout", "-b", "master"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, repo, err, out)
		}
	}
	return repo
}

// chatGroundNoticeCapture runs the REAL start-up report with the process standing
// in dir, and returns the bytes it wrote to stderr. os.Stderr is swapped for a
// pipe and restored before the read, so a failure inside chatGroundReport cannot
// leave the test binary writing into a closed pipe.
func chatGroundNoticeCapture(t *testing.T, dir string) string {
	t.Helper()
	t.Chdir(dir)
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	// The resolution itself is asserted by TestChatGroundForCarriesTheReadOnlyFactItself;
	// here the subject is the bytes on stderr. The error is CHECKED rather than
	// discarded (Rule 15b): a resolution failure is exactly the state in which the
	// printed line says less than this case assumes, and the stylegate's
	// ignored-error ceiling has no room for one more.
	if _, rerr := chatGroundReport(nil); rerr != nil {
		os.Stderr = old
		wErr, readErr := w.Close(), r.Close()
		t.Fatalf("chatGroundReport: %v (closing the capture pipes: write=%v read=%v)", rerr, wErr, readErr)
	}
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("close the capture pipe: %v", cerr)
	}
	os.Stderr = old
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, rerr := r.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if rerr != nil {
			break
		}
	}
	if cerr := r.Close(); cerr != nil {
		t.Fatalf("close the capture reader: %v", cerr)
	}
	return string(buf)
}

// TestChatGroundReplayNoticeStatesNoWrite is THE regression: with a recording
// being re-run, the start-up line must not claim the writes the ordinary three
// sentences claim — and with no replay armed, it must claim exactly what it
// always claimed.
func TestChatGroundReplayNoticeStatesNoWrite(t *testing.T) {
	repo := chatGroundNoticeRepo(t)

	// CONTROL: an ordinary chat in a repository's main checkout. The sentence is
	// asserted by its own words, so "the line is still printed and still means
	// what it meant" is measured rather than assumed.
	ordinary := chatGroundNoticeCapture(t, repo)
	if !strings.Contains(ordinary, "this session writes in "+repo) {
		t.Fatalf("an ordinary chat no longer prints the write sentence it always printed — the fix must not have removed a TRUE line:\n%s", ordinary)
	}
	if !strings.Contains(ordinary, "MAIN CHECKOUT") {
		t.Fatalf("the ordinary line no longer names the main checkout:\n%s", ordinary)
	}
	for _, claim := range chatGroundReplayReadOnlyClaims {
		if strings.Contains(ordinary, claim) {
			t.Fatalf("an ordinary chat printed the READ-ONLY line (%q) while NOT replaying — the fix was applied to the wrong half:\n%s", claim, ordinary)
		}
	}

	// ARMED: the same directory, the same writer, this time re-running a
	// recording. The write claim must be GONE and the read-only family must be
	// there, still naming the directory.
	replaymode.Arm("/a/recording/that/is/not/read/by/this/case.jsonl")
	t.Cleanup(replaymode.Disarm)
	if !replaymode.Active() {
		t.Fatalf("replaymode.Active() is false after Arm: the flag the report consults is not in force, so the armed reading below measures nothing")
	}
	replay := chatGroundNoticeCapture(t, repo)
	for _, claim := range chatGroundReplayOrdinaryWriteClaims {
		if strings.Contains(replay, claim) {
			t.Fatalf("a REPLAY printed %q: this run writes nothing (internal/replaymode refuses the session mint, the slot registration, the editor history and every tool round), so that sentence states a behaviour the code contradicts:\n%s", claim, replay)
		}
	}
	for _, claim := range chatGroundReplayReadOnlyClaims {
		if !strings.Contains(replay, claim) {
			t.Fatalf("a REPLAY did not say %q — the line must state what this run IS, not merely omit what it is not:\n%s", claim, replay)
		}
	}
	if !strings.Contains(replay, repo) {
		t.Fatalf("the read-only line does not name the directory the session stands in (%s): a replay that names no tree leaves the operator unable to say which checkout it ran against:\n%s", repo, replay)
	}
	if !strings.Contains(replay, "MAIN CHECKOUT") {
		t.Fatalf("the read-only line does not name the checkout it leaves untouched:\n%s", replay)
	}
}

// TestChatGroundReplayNoticeInAPlainDirectory pins the SECOND shape: a replay
// started where there is no checkout at all. The ordinary line's claim there is a
// different sentence ("no branch to attribute the work to") and it must be gone
// for the same reason — the criterion is "this run writes nothing", not "this
// directory happens to be a checkout".
func TestChatGroundReplayNoticeInAPlainDirectory(t *testing.T) {
	plain := t.TempDir() // deliberately NOT a repo

	ordinary := chatGroundNoticeCapture(t, plain)
	if !strings.Contains(ordinary, "this session writes in "+plain) {
		t.Fatalf("an ordinary chat in a plain directory no longer prints the write sentence:\n%s", ordinary)
	}
	if !strings.Contains(ordinary, "no git checkout here") {
		t.Fatalf("the ordinary plain-directory line no longer says what the directory is:\n%s", ordinary)
	}

	replaymode.Arm("/a/recording.jsonl")
	t.Cleanup(replaymode.Disarm)
	replay := chatGroundNoticeCapture(t, plain)
	for _, claim := range chatGroundReplayOrdinaryWriteClaims {
		if strings.Contains(replay, claim) {
			t.Fatalf("a REPLAY in a plain directory printed %q:\n%s", claim, replay)
		}
	}
	for _, claim := range chatGroundReplayReadOnlyClaims {
		if !strings.Contains(replay, claim) {
			t.Fatalf("a REPLAY in a plain directory did not say %q:\n%s", claim, replay)
		}
	}
	if !strings.Contains(replay, plain) {
		t.Fatalf("the read-only line does not name the directory (%s):\n%s", plain, replay)
	}
}

// TestChatGroundForCarriesTheReadOnlyFactItself pins the CORRECTION of the first
// attempt, which is the part a reviewer cannot see from the two cases above:
// WHICH LAYER decides. The first implementation set
// `report.ReadOnly = replaymode.Active()` in cmd/agent/chat_ground.go — the
// CALLER — and that is a bypass: `tools.ChatGroundFor` is the single producer of
// this resolution, so a fact expressed one level above it leaves the false
// sentence live for every other caller of the producer. The flag is now asked
// inside `ChatGroundFor` (internal/replaymode imports only stdlib, so there is no
// cycle), and this case asserts the fact ON THE PRODUCER'S OWN ANSWER — no
// caller involved, nothing to forget.
//
// It is the arm that would have caught the first attempt: with ReadOnly set at
// the call site, `tools.ChatGroundFor(...)` answers false while a recording is
// being re-run, and this case fails by name.
func TestChatGroundForCarriesTheReadOnlyFactItself(t *testing.T) {
	repo := chatGroundNoticeRepo(t)

	// The producer, asked directly, with NO replay in force: a write happens.
	if got := tools.ChatGroundFor("any-session", repo); got.ReadOnly {
		t.Fatalf("tools.ChatGroundFor answered ReadOnly=true with no recording armed — an ordinary chat does write, and the field is not a default: %+v", got)
	}

	replaymode.Arm("/a/recording.jsonl")
	t.Cleanup(replaymode.Disarm)
	got := tools.ChatGroundFor("any-session", repo)
	if !got.ReadOnly {
		t.Fatalf("tools.ChatGroundFor answered ReadOnly=false while a recording is being re-run — the fact is being decided ABOVE the producer, so any other caller of it still states the write claim: %+v", got)
	}
	// The rest of the resolution must be UNAFFECTED: the replay changes what is
	// claimed about writing, never whose directory this is.
	if !got.InMainCheckout || got.Repo != repo || got.Dir != repo {
		t.Fatalf("the replay changed the resolution itself, not only the write claim — the directory and its checkout are facts about the filesystem and must not move: %+v", got)
	}
}
