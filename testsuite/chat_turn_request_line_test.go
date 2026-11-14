package testsuite

// chat_turn_request_line_test.go — the RETIREMENT proof for the per-turn
// `[gopi] request: …` notice, and the SURVIVAL proof for the capability it used
// to advertise.
//
// WHAT WAS REMOVED AND WHY. Every turn — a normal one AND a failed one — ended by
// pushing one notice line onto the console through `chatWriteTurnRequestLine`
// (cmd/agent/chat_turn_request.go): the request's message count, the first line
// of "your" message and the `/context` destination. It was unconditional, it was
// on every turn, and both of its facts were wrong in practice: the excerpt walk
// ran BACKWARDS over `role: "user"` messages, so on a session whose history had
// been turn-folded it quoted an INJECTED system row rather than anything the
// operator typed. The operator's own verdict was that it is noise.
//
// WHAT MUST SURVIVE. `/context` itself: the command, its usage page and the
// `context-turn<N>.md` export. It is the actual capability — the line was only a
// signpost, and a signpost printed on every turn is a cost paid by every reader.
//
// WHY THE REAL BINARY AND NOT A UNIT TEST. The notice's whole defect was that a
// PERSON meets it every turn, on the stream the rest of the console uses. A unit
// test can show `chatTurnRequestLine` builds no string any more and prove nothing
// about what a shell sees (PROJECT.md Rule 31). testutil.RunGopi drives the
// actual `gopi chat` process, so the assertions below are about the operator's
// own stream.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/ctxdump"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The fixtures. The prompt's first line is distinctive on purpose: the
// retirement assertion is not "some text disappeared" but "the run that WOULD
// have quoted this made one model request and printed no request line".
const (
	chatNoisePrompt = "read the ledger and tell me the count"
	chatNoiseAnswer = "Done — the ledger holds what you asked about, and this answer is long enough to close the round."
	// chatRetiredRequestNoticePrefix is the notice's own opening. Every console
	// stream below is scanned for it, so a re-introduction anywhere on the turn
	// path is red rather than a silent regression.
	chatRetiredRequestNoticePrefix = "[gopi] request:"
)

// TestChatTurnPrintsNoRequestSummaryNotice is the operator's acceptance case,
// inverted: ONE real `gopi chat --once` turn that genuinely sends a request, and
// the console carries NO `[gopi] request:` notice on either stream.
//
// The "genuinely sends a request" half is what keeps this from being a green that
// measured nothing: a run that never reached the model would print no notice for
// the uninteresting reason. RequestCount is asserted to be exactly 1, so the
// absence is measured on a turn that DID build and send a request.
func TestChatTurnPrintsNoRequestSummaryNotice(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatNoiseAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "-p", chatNoisePrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat --once exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("the model was called %d time(s), want exactly 1 — the notice's absence must be measured on a turn that really sent a request", n)
	}
	if strings.Contains(stdout, chatNoiseAnswer) == false {
		t.Errorf("the turn's answer is missing from stdout — the run did not get far enough for the assertion to mean anything:\n%s", stdout)
	}
	for _, stream := range []struct{ name, body string }{{"stdout", stdout}, {"stderr", stderr}} {
		if strings.Contains(stream.body, chatRetiredRequestNoticePrefix) {
			t.Errorf("%s still carries the retired per-turn notice %q — it is printed on EVERY turn and quotes a turn-folded system row:\n%s",
				stream.name, chatRetiredRequestNoticePrefix, stream.body)
		}
	}
}

// TestChatContextStillWritesTheArtifact is the capability the retired notice used
// to name, proven at the surface a person uses: a scripted `/context` line in
// `gopi chat --once` still writes `context-turn<N>.md` into the session directory
// and still says so in one line.
//
// The command had no dependency on the notice, and this test is what keeps that
// true — retire the signpost, keep the road.
func TestChatContextStillWritesTheArtifact(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatNoiseAnswer})
	defer llm.Close()

	// `--no-editor` rather than `--once`: `--once` takes ONE prompt line and
	// exits, so the `/context` line after it would never be read and the test
	// would be measuring the absence of an artifact it never asked for. The
	// scripted three lines are exactly what an operator would type, and
	// `--no-editor` is the mode that reads them all.
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--no-editor"},
		chatNoisePrompt+"\n/context\n/quit\n", testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("the scripted session exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	want := fmt.Sprintf(ctxdump.FileNameFormat, 1)
	path := findFileNamed(t, home, want)
	if path == "" {
		t.Fatalf("/context wrote no %s under this run's profile — the export the retired notice pointed at is gone\n--- stderr ---\n%s", want, stderr)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("%s exists but is not a readable non-empty artifact: %v", path, err)
	}
	// The one-line notice `/context` has always printed, naming the file it wrote.
	if !strings.Contains(stderr, strings.TrimSpace(strings.Split(fmt.Sprintf(ctxdump.LineFormat, path, 0, 0), "(")[0])) {
		t.Errorf("the console never said %q — /context writes the file silently:\n%s", path, stderr)
	}
	// And the retired per-turn notice did not come back on this path either.
	if strings.Contains(stdout, chatRetiredRequestNoticePrefix) || strings.Contains(stderr, chatRetiredRequestNoticePrefix) {
		t.Errorf("the retired per-turn notice is back on the /context path")
	}
}

// findFileNamed walks root and returns the first regular file whose basename is
// name, or "" when there is none. The session directory's NAME is a uuid and its
// layout has three forms; the artifact's own name is the fact under test, so the
// search is by name rather than by a path this test would have to guess.
func findFileNamed(t *testing.T, root, name string) string {
	t.Helper()
	found := ""
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() && d.Name() == name {
			found = p
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s for %s: %v", root, name, err)
	}
	return found
}

// TestChatHelpNamesContextAndItsArtifact is the discoverability half at the real
// surface: `gopi chat -h` — the page an operator actually reads before starting a
// session — must name the command AND the file it writes, because the per-turn
// signpost that used to name them is gone.
//
// The retired paragraph is asserted ABSENT. It said "EVERY TURN ENDS WITH ONE
// LINE", which was true only while the notice existed; a page that keeps promising
// it is the same defect one document further out.
func TestChatHelpNamesContextAndItsArtifact(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "-h"}, "",
		testutil.MockEnv(t, home, "http://127.0.0.1:1"), work)
	if code != 0 {
		t.Fatalf("gopi chat -h exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	page := stdout + stderr
	if !strings.Contains(page, "/context") {
		t.Errorf("`gopi chat -h` never names /context — with the per-turn notice retired, the command is otherwise undiscoverable")
	}
	if want := ctxdump.FileNamePattern; !strings.Contains(page, want) {
		t.Errorf("`gopi chat -h` does not name the artifact /context writes (%q)", want)
	}
	if strings.Contains(page, "EVERY TURN ENDS WITH ONE LINE") {
		t.Errorf("`gopi chat -h` still promises a per-turn summary line the console no longer prints")
	}
	// The retired vocabulary stays out (the 2026-11-07 decision).
	for _, gone := range []string{"/code-search", "/memory"} {
		if strings.Contains(page, gone) {
			t.Errorf("`gopi chat -h` re-advertises the retired %s", gone)
		}
	}
}
