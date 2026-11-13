package testsuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
)

// chat_paste_spill_pty_test.go — the paste spill on the carrier the defect was
// reported from: a REAL terminal, the raw-mode editor, real bytes.
//
// The scripted harness (internal/lineedit) asserts the cost, the byte-exactness
// and the rendered grid; cmd/agent asserts the wiring. What only a PTY can
// assert is the SHAPE OF THE DELIVERY: a terminal hands a pasted block to the
// read loop as full chunks of the read buffer, and the editor must both stay
// responsive (a keystroke after the paste still submits) and leave a REFERENCE on
// the input line while the bytes land in a file under the run's own session
// directory.

const (
	// ptyPasteBodyBytes is how much content the paste carries. It is well past
	// the editor's buffer bound's comfort zone and past the paste threshold by
	// many chunks, which is the point: this is the paste that used to freeze the
	// prompt for minutes.
	ptyPasteBodyBytes = 64 * 1024
	// ptyPasteLine is the line that repeats inside the paste body. It carries no
	// prompt marker and no shell metacharacter — it is content.
	ptyPasteLine = "pasted content line with some words in it\n"
	// ptySubmitGap is how long the harness waits before the submit Enter: many
	// times the editor's paste-burst windows, so the Enter is unambiguously the
	// user's own (the separation chat_pty_test.go uses).
	ptySubmitGap = 2 * time.Second
)

// TestChatPTY_LongPasteIsSpilledAndKeepsTheEditorResponsive pins the two halves
// of the paste defect end to end:
//
//	(A) the editor stays responsive — the paste is answered, not frozen: the
//	    submit arrives afterwards and the model's answer comes back inside the
//	    harness's own deadline (a per-rune splice at this size measured minutes);
//	(B) the paste is SPILLED — the user message carries a reference, and the file
//	    the reference names holds the pasted bytes under this run's session dir.
func TestChatPTY_LongPasteIsSpilledAndKeepsTheEditorResponsive(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "answer after a large paste."})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	body := buildPasteBody()
	// writeBurst, not writeGapped: a terminal sends a pasted block back to back,
	// and the kernel hands the child full read buffers of it.
	chat.writeBurst(t, body)

	// The reference is rendered as soon as the first chunk is spilled, so this
	// wait answers "did the spill happen at all".
	pasted := chat.waitFor(t, chatPasteRefMark, ptyPromptTimeout)
	if !strings.Contains(pasted, chatPasteRefMark) {
		t.Fatalf("the paste left no reference on the input line; output:\n%s", pasted)
	}
	// The pasted CONTENT must not be on the screen: the line shows where the
	// bytes went, not the bytes (a 64 KiB paste rendered would be thousands of
	// rows of re-layout — the second half of the freeze).
	if strings.Contains(pasted, strings.TrimRight(ptyPasteLine, "\n")) {
		t.Fatalf("the pasted content itself was painted on the screen; output:\n%s", pasted)
	}

	time.Sleep(ptySubmitGap) //stylegate:allow-sleep separates the submit Enter from the paste's own burst window, so the Enter is the user's
	chat.write(t, "\r")
	out := chat.waitFor(t, "answer after a large paste.", ptyPromptTimeout)
	if !strings.Contains(out, chatPromptMark) {
		t.Fatalf("the prompt was never re-rendered after the paste; output:\n%s", out)
	}

	// THE EDITOR IS STILL ALIVE: a keystroke typed now must still reach it.
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"user": 1, "assistant": 1, "session_end": 1})
	users := sess.UserContents()
	if len(users) != 1 {
		t.Fatalf("user messages = %d, want 1", len(users))
	}
	if strings.Contains(users[0], strings.TrimRight(ptyPasteLine, "\n")) {
		t.Fatalf("the pasted content was sent as the message itself (%d bytes) — it must be spilled, with only a "+
			"reference on the line", len(users[0]))
	}
	if got := strings.Count(users[0], "["+lineedit.PasteRefKeyword+" "); got != 1 {
		t.Fatalf("the message carries %d references, want ONE: a paste delivered as many reads is ONE paste, and its "+
			"tail belongs to the same file (message %d bytes)", got, len(users[0]))
	}
	ref := pasteRefInMessage(t, users[0])
	// THE THREE DIRECTORIES MUST AGREE — this is the assertion the defect broke,
	// and it is the reason the reference is read through the production reader
	// rather than through a local copy of the format:
	//
	//	(1) the WRITE directory — the session dir the run spilled into;
	//	(2) the directory the REFERENCE names;
	//	(3) the directory THIS TEST resolves from the session tree, through the
	//	    SAME layout-keyed scan the production readers use (sessionlayout).
	//
	// Asserting only "the file the reference names is readable" would pass on a
	// reference pointing anywhere at all; the contract is that the pointer
	// resolves inside the very session whose message carries it.
	sessDir := sessionDirOf(t, home, sess)
	if got := filepath.Base(sessDir); got != sess.ID {
		t.Fatalf("the session dir is %q but session_start declares id %q — the reference's own directory cannot be "+
			"resolved reliably", got, sess.ID)
	}
	wantDir := filepath.Join(sessDir, lineedit.PasteDirName)
	if got := filepath.Dir(ref.Path); got != wantDir {
		t.Fatalf("the reference names %s, but this session's spill directory is %s (session dir %s): the message "+
			"points outside the run that wrote it", got, wantDir, sessDir)
	}
	got, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatalf("the message's reference names an unreadable file (%s): %v", ref.Path, err)
	}
	// PasteRefFor renders `size` from the same byte count the store wrote, so a
	// reference that understates or overstates the file is a second, quieter
	// disagreement between the pointer and the bytes.
	if ref.Size != len(got) {
		t.Fatalf("the reference declares size=%dB but the file holds %d bytes: the pointer and the payload "+
			"disagree (%s)", ref.Size, len(got), ref.Path)
	}
	if ref.Source != inputsource.Human {
		t.Fatalf("the reference declares source=%q, want %q: this run spilled a paste typed at the prompt, and a reader "+
			"must be able to tell which face put the bytes on the line (%s)", ref.Source, inputsource.Human, ref.Path)
	}
	if !strings.Contains(string(got), strings.TrimRight(ptyPasteLine, "\n")) {
		t.Fatalf("the spilled file does not hold the pasted content: %d bytes, first 80: %q", len(got), firstN(got, 80))
	}
}

// sessionDirOf returns the directory holding the session record the caller
// loaded, resolved through the SHARED layout-keyed scan (sessionlayout.Records —
// the same call internal/testutil.LoadSession and the production readers make),
// so this test's idea of "which directory is this session" cannot drift from the
// reader's.
func sessionDirOf(t *testing.T, gopiHome string, sess *testutil.Session) string {
	t.Helper()
	root := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName)
	records, err := sessionlayout.Records(root)
	if err != nil {
		t.Fatalf("session layout: cannot list %s: %v", root, err)
	}
	for _, r := range records {
		if r.Path == sess.Path {
			return r.Dir
		}
	}
	t.Fatalf("the loaded session record %s is not among the %d record(s) %s reports — this test and the shared "+
		"layout scan disagree about which files are session records", sess.Path, len(records), root)
	return ""
}

// chatPasteRefMark is the reference's opening marker as it appears on the screen:
// the value comes from internal/lineedit (one definition), so a change to the
// reference's spelling cannot leave this test asserting an obsolete shape.
var chatPasteRefMark = "[" + lineedit.PasteRefKeyword + " "

// buildPasteBody returns the pasted block: whole lines, so no read ends inside a
// multi-byte sequence (the editor spills only self-contained reads).
func buildPasteBody() string {
	var b strings.Builder
	b.Grow(ptyPasteBodyBytes + len(ptyPasteLine))
	for b.Len() < ptyPasteBodyBytes {
		b.WriteString(ptyPasteLine)
	}
	return b.String()
}

// pastePathInMessage extracts the reference the message carries: the absolute
// path it names, plus the byte count the reference itself states.
//
// It goes through the PRODUCTION reader (PasteRefInfoFor) rather than a private
// copy of the format. That is not a convenience — it is the defect this helper
// used to carry. A local parser cut the path at the first `]`, which was correct
// only while a reference was `[paste N: /abs/path]`; once the producer added the
// named `source=` and `size=` fields INSIDE the brackets (PasteRefFor,
// internal/lineedit/pastestore.go), the local parser returned
// `/abs/path source=human size=65562B` and os.ReadFile reported ENOENT on a
// concatenation that no writer ever created — measured 2026-10-11 on this very
// test, whose message carried exactly that tail. The file was always there; the
// reader was wrong. One format, one reader (the same fix, with the same
// diagnosis, already applied to the editor-side suite:
// internal/lineedit/paste_spill_test.go's pastePathFromRef).
func pasteRefInMessage(t *testing.T, msg string) lineedit.PasteRefInfo {
	t.Helper()
	info, ok := lineedit.PasteRefInfoFor(msg)
	if !ok {
		t.Fatalf("the message carries no paste reference: %q", firstN([]byte(msg), 200))
	}
	if info.Runes <= 0 {
		t.Fatalf("the reference's rune count is %d, want a positive number (%q)", info.Runes, firstN([]byte(msg), 200))
	}
	if !filepath.IsAbs(info.Path) {
		t.Fatalf("the reference names a relative path %q — it is the only handle on the bytes, so it must resolve "+
			"from any working directory (%q)", info.Path, firstN([]byte(msg), 200))
	}
	return info
}

// firstN trims byte slices for assertion messages.
func firstN(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}

// TestPasteRefReader_ResolvesTheProductionSpelling is the regression guard for
// the defect that made TestChatPTY_LongPasteIsSpilledAndKeepsTheEditorResponsive
// fail at its read: the test's own helper parsed the reference with a PRIVATE
// copy of the format (marker → `: ` → first `]`), which was correct only while a
// reference was `[paste N: /abs/path]`. Once the producer put the named
// `source=` and `size=` fields INSIDE the brackets, the private parser returned
// `/abs/path source=human size=65562B` and os.ReadFile reported ENOENT on a
// concatenation no writer ever created.
//
// It is deliberately PTY-free: the defect was in the READER, so a guard that
// needed a terminal to reproduce it would only run where a terminal exists, and
// the next format change would go unnoticed in every other suite. It exercises
// the WHOLE chain the PTY test depends on — the store writes the file, PasteRefFor
// renders the reference, the TEST'S OWN helper reads it back, os.ReadFile opens
// what the helper returned — so a private parser reintroduced here fails here.
func TestPasteRefReader_ResolvesTheProductionSpelling(t *testing.T) {
	// A path with a space: it is what makes "take the path up to the first space"
	// look reasonable, and it is exactly what the production reader must survive
	// (pasteRefFieldCut cuts at ` <key>=` for this reason).
	dir := filepath.Join(t.TempDir(), "session with a space")
	body := []byte(ptyPasteLine)
	store := lineedit.NewPasteStoreFor(dir, inputsource.Human)
	ref, err := store.Spill(body)
	if err != nil {
		t.Fatalf("spill: %v", err)
	}

	msg := "look at this " + lineedit.PasteRefFor(ref)
	if !strings.Contains(msg, refFieldSourceProbe) {
		t.Fatalf("the rendered reference carries no %s field: %q — this guard exists to pin the reader against the "+
			"producer's CURRENT spelling, and the spelling it checks is no longer the one rendered",
			inputsource.Human, msg)
	}

	got := pasteRefInMessage(t, msg)
	if got.Path != ref.Path {
		t.Fatalf("the helper read the path as %q, want %q: something is parsing the reference locally instead of "+
			"through the production reader, and the named fields are landing in the path", got.Path, ref.Path)
	}
	if got.Size != len(body) {
		t.Fatalf("the helper read size=%d, want %d", got.Size, len(body))
	}
	if got.Source != inputsource.Human {
		t.Fatalf("the helper read source=%q, want %q", got.Source, inputsource.Human)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("the path the helper returned is unreadable (%s): %v", got.Path, err)
	}
	if string(data) != string(body) {
		t.Fatalf("the file holds %q, want %q", data, body)
	}
}

// refFieldSourceProbe is the rendered form of the source field this guard pins
// the reader against. It is BUILT from the production spelling rather than typed,
// because a literal here would keep passing after the field was renamed — which
// is the very failure mode the guard exists to catch.
var refFieldSourceProbe = "source=" + inputsource.Human
