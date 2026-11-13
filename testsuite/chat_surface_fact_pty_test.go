package testsuite

// chat_surface_fact_pty_test.go — the ARM STATE belongs on the REQUEST, not on
// the screen (the operator's ruling, 2026-10-11).
//
// THE DEFECT IT PINS. `gopi chat --tools` used to state its own arm state as a
// `[gopi] …` prose line on the operator's screen: first in the answer band, then
// (after the 2026-10-03 ruling that tips belong on the startup screen) in that
// screen's own notices section as the short copy `screen dump → /home/…/chat-screen.dump`.
// The operator's verdict: the screen is for the HUMAN, and the arm state is the
// MODEL's business — it belongs in the very request it describes. This case is the
// gate for that disposition, and it is deliberately written against the CHANNELS
// rather than against one wording, because the last fix's e2e assertion was
// defeated by exactly that: it looked for `[gopi] chat_surface` in the band while
// the surviving line's stream spelling carried no such prefix (measured
// 2026-10-11, the gate's blind spot).
//
// WHAT IS ASSERTED, and why each half is load-bearing:
//
//  1. NO CHAT-SURFACE CHROME ANYWHERE IN THE PTY STREAM — not below the box, and
//     not inside it either. Two markers, because the defect had two spellings: the
//     `[gopi] chat_surface` notice and the `screen dump → …` section copy. A run
//     that printed either one fails here, whichever carrier it chose.
//  2. THE BOX WAS DRAWN, so check 1 cannot pass vacuously on a run that never
//     reached the startup screen (the failure mode chat_banner_pty_test.go
//     documents at this carrier). The closing corner of the border is the proof
//     the commit finished.
//  3. THE FACT MOVED INSTEAD OF VANISHING (Rule 15) — on BOTH carriers it now has:
//     the model's (the system prompt of a REAL request the mock LLM received, read
//     out of the recorded request body) and the operator's (the panel's own
//     `surface` fact row, which is a fact about this run beside its session id,
//     not a notice).
//
// The evidence kept beside the run is the pty stream and the mock's request body,
// so both verdicts are reproducible from artifacts rather than from a re-run.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"golang.org/x/sys/unix"
)

const (
	// chatSurfaceChromeNotice is the `[gopi] …` spelling of the notice this case
	// removes. It is spelled HERE, independently of the renderer's constants, so
	// the verdict is about the stream the operator received (the same discipline
	// chat_banner_band_clean_pty_test.go states for its own markers).
	chatSurfaceChromeNotice = chatPanelChromeMarker + "chat_surface"
	// chatSurfacePanelCopy is the SECTION's short copy of the same notice — the
	// spelling that survived the previous fix and produced this task's blind spot.
	// Both halves of it are checked: the section drew `screen dump → <path>` and
	// the stale `[gopi] chat: screen dump …` line is caught by the marker sweep
	// below.
	chatSurfacePanelCopy = "screen dump →"
	// chatSurfaceRawLogSuffix is how this run's wire log is named on disk: the raw
	// tree is discovered rather than derived, because the file's name embeds the
	// session uuid — the run's own business, owned by the package that writes it.
	chatSurfaceRawLogSuffix = ".raw.log"
	// chatSurfaceFactProbe is the fragment of the arm note the raw log assertion
	// looks for: a substring the note always carries (its own opening words), so
	// the check is about the note ARRIVING rather than about its exact wording.
	chatSurfaceFactProbe = "writes its own screen: every frame it paints lands in"
	// chatSurfaceStaleStreamLine identifies the ONE legitimate line that carries
	// the panel copy's words: the pre-layout artifact-plumbing notice. It is named
	// so the occurrence accounting below can tell "the operator was told where the
	// dump went" from "the arm state is on the screen again".
	chatSurfaceStaleStreamLine = "gopi chat: screen dump"
	// chatSurfacePanelFact is the panel's own vocabulary for the moved fact
	// (cmd/agent/chat_panel_notice.go's chatBannerSurfaceScreenArmed). Spelled as
	// the grid shows it, so the assertion reads what a reader sees.
	chatSurfacePanelFact = "screen readable"
	// chatSurfaceUserLine is the line sent to make the run issue a REAL request: a
	// chat that never answered has no request to read, and the model-side half of
	// this case would then be untestable.
	chatSurfaceUserLine = "surface-fact-case\r"
	// chatSurfaceAnswer is the mock's scripted reply, short and single-line so it
	// lands in the band without pushing rows around.
	chatSurfaceAnswer = "surface-fact-case-answer"
)

// TestChatPTY_ArmFactRidesTheRequestNotTheScreen is the operator-visible
// acceptance for the 2026-10-11 ruling.
func TestChatPTY_ArmFactRidesTheRequestNotTheScreen(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	evidence := testutil.TestScratchDir(t, "chat-surface-fact")
	dumpPath := evidence + "/chat-surface-fact.dump"

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatSurfaceAnswer})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: chatBannerPTYCols, Row: chatBannerPTYRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	// THE GEOMETRY IS PROVEN, NOT ASSUMED: a pty that never received the size
	// reports 0 columns and the surface then (rightly) declines to draw a box the
	// case would misread as "the stream is clean".
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != chatBannerPTYCols {
		t.Fatalf("the pty reports %d columns, want the %d this harness installed", cols, chatBannerPTYCols)
	}

	// The startup flush: the prompt is painted BEFORE the screen is committed, so
	// the bytes after it are the flush the notices used to ride.
	promptOut := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	startup := promptOut + chat.collect(t, chatPanelCollectWindow)
	chatSurfaceKeep(t, evidence, "pty-startup.txt", startup)

	// ---- 1. the startup screen was drawn, so this case can speak about the stream
	boxEnd := strings.LastIndex(startup, chatPanelBoxBottomGlyph)
	if boxEnd < 0 {
		t.Fatalf("the startup screen was never written to this pty, so this case cannot speak about the stream; stream:\n%s",
			chatBannerVisible(startup))
	}

	// ---- 2. no armed/unarmed chrome, in either spelling ----------------------
	//
	// The whole stream is swept, not just the band below the box: the previous fix
	// moved the sentence INTO the box (and its fallback put it back under it), so a
	// band-only assertion is exactly the blind spot that let this defect live
	// through one gate.
	if idx := strings.Index(startup, chatSurfaceChromeNotice); idx >= 0 {
		t.Errorf("the chat startup stream still carries the arm-state notice (%q) — the operator's ruling is that the screen is for the human:\n%s",
			chatSurfaceChromeNotice, chatBannerVisible(startup[idx:]))
	}
	// The panel copy's spelling is checked by OCCURRENCE ACCOUNTING rather than by
	// a bare substring, because ONE legitimate line carries the same words: the
	// pre-layout plumbing notice (`gopi chat: screen dump → <path> …`, printed
	// before the terminal is taken, i.e. before any screen exists) states where
	// this run's artifact went, and the operator can act on that. Every occurrence
	// of the spelling must therefore BE that line — an armed/unarmed copy drawn into
	// the box, or its fallback printed under it, has no such prefix and fails here.
	plumbing, spelled := strings.Count(startup, chatSurfaceStaleStreamLine), strings.Count(startup, chatSurfacePanelCopy)
	if plumbing == 0 {
		t.Fatalf("the run printed no artifact-plumbing line (%q), so this check cannot distinguish a clean stream from an unread one:\n%s",
			chatSurfaceStaleStreamLine, chatBannerVisible(startup))
	}
	if spelled != plumbing {
		t.Errorf("the arm-state panel copy (%q) appears %d time(s) in the startup stream but the plumbing line only %d — an armed/unarmed copy is on the screen again. stream:\n%s",
			chatSurfacePanelCopy, spelled, plumbing, chatBannerVisible(startup))
	}

	// ---- 3. the fact MOVED, on both carriers --------------------------------
	//
	// (a) THE OPERATOR'S: the panel's own surface fact row. It is what keeps this
	// case from being a silent deletion: the screen still says which arm this run
	// is on, beside its session id and its model.
	if !strings.Contains(startup[:boxEnd], chatSurfacePanelFact) {
		t.Errorf("the startup screen does not state this run's surface fact (%q); the stream is clean because the fact was LOST, not moved. stream:\n%s",
			chatSurfacePanelFact, chatBannerVisible(startup))
	}
	// (b) THE MODEL'S: the system prompt of a real request this run sent. The turn
	// is driven from the pty so the request is the carrier's own — typed as
	// KEYSTROKES (writeGapped) and then submitted with its own CR, because a burst
	// write is read as a PASTE by the editor and the trailing CR then lands inside
	// the bracketed-paste framing instead of submitting the line (measured: the
	// same bytes in one write left the text sitting at the prompt).
	chat.writeGapped(t, strings.TrimSuffix(chatSurfaceUserLine, "\r"))
	chat.write(t, "\r")
	chat.waitFor(t, chatSurfaceAnswer, ptyPromptTimeout)

	deadline := time.Now().Add(ptyPromptTimeout)
	var body string
	for time.Now().Before(deadline) {
		if b := llm.RequestBody(1); strings.TrimSpace(b) != "" {
			body = b
			break
		}
		time.Sleep(chatPanelReadTick) //stylegate:allow-sleep the mock's arrival is the wait's own cadence
	}
	if body == "" {
		t.Fatalf("the run sent no request to the mock LLM, so the model-side half of this case is untestable")
	}
	chatSurfaceKeep(t, evidence, "request-1.json", body)

	marker := agent.ChatSurfaceFactMarker()
	if !strings.Contains(body, jsonEscaped(marker)) {
		t.Errorf("the request's system prompt does not carry the arm fact (marker %q) — the fact left the screen and did not arrive on the wire", marker)
	}
	// AND IT NAMES THE ARTIFACT, which is the one operand the model cannot derive:
	// without it the note would state the capability and withhold where it lives.
	if !strings.Contains(body, dumpPath) {
		t.Errorf("the request's arm note does not name this run's dump (%s), so the model cannot quote the artifact it reads", dumpPath)
	}
	// (c) THE RUN'S OWN RAW LOG, which is the artifact an operator or a later
	// agent actually reads back (`gopi session raw`): the fact has to be in the
	// RECORD the run wrote, not only in what the mock happened to receive. This
	// is the same claim the report makes, measured on the run's own wire log.
	chatSurfaceAssertRawLog(t, home)
	t.Logf("request 1 carries the arm fact; pty stream:\n%s", chatBannerVisible(startup))
}

// chatSurfaceAssertRawLog reads the run's raw HTTP log out of the profile it was
// given and asserts the marker is in it. It searches the profile's raw directory
// rather than naming a file: the record's name embeds the session uuid, which is
// the run's own business (gopilog owns that derivation, and re-deriving it here
// would be a second implementation of session discovery).
func chatSurfaceAssertRawLog(t *testing.T, home string) {
	t.Helper()
	var logs []string
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, chatSurfaceRawLogSuffix) {
			logs = append(logs, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read this run's raw log tree under %s: %v", home, err)
	}
	if len(logs) == 0 {
		t.Fatalf("this run wrote no raw HTTP log under %s, so the fact's arrival in the RECORD is unverified", home)
	}
	blob, readErr := os.ReadFile(logs[0])
	if readErr != nil {
		t.Fatalf("read the raw log %s: %v", logs[0], readErr)
	}
	if !strings.Contains(string(blob), chatSurfaceFactProbe) {
		t.Errorf("the raw log %s does not carry the arm note (probe %q) — the fact is not in the record this run keeps",
			logs[0], chatSurfaceFactProbe)
	}
	t.Logf("the run's own raw log %s carries the arm note", logs[0])
}

// chatSurfaceKeep writes one piece of this case's evidence beside the run's other
// artifacts (the scratch tree is swept after the run, so a verdict that lived only
// in a report could not be re-read).
func chatSurfaceKeep(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), chatBannerEvidencePerm); err != nil {
		t.Fatalf("keep %s as evidence: %v", name, err)
	}
	t.Logf("kept %s", path)
}

// jsonEscaped is the marker as the wire spells it: the note's own `[`/`]`/space/
// `—` are JSON-safe, but a request body escapes `"` — so the comparison is made
// against the string as the ENCODER would have written it, through the standard
// encoder rather than a hand-rolled replacement (a second escaper is how a check
// starts agreeing with a body nobody sent).
func jsonEscaped(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return strings.Trim(string(b), `"`)
}
