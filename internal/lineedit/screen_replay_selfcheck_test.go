package lineedit

// screen_replay_selfcheck_test.go — the SELF-VERIFIABLE check for the paint
// channel, runnable by an agent with no operator and no new tooling.
//
// WHY IT EXISTS. The chat screen's fidelity question ("did the display lose
// text?") has three channels, and the middle one — what the surface actually
// painted — is only observable through a recorded pty byte stream plus an
// interpreter. The other two channels already have one-call checks
// (`gopi session log <id> --response --text` is the authoritative transcript;
// `gopi ui_state --compare` answers "is this line on the painted grid AND in the
// transcript?"). This file is the third leg: it replays a RECORDED pty capture
// through the SAME interpreter the dump sink uses, at several read sizes, and
// asserts the interpreter's grid does not depend on how the stream was chunked.
//
// THE CRITERION, and why a chunk size is the right axis: a terminal is a byte
// STREAM. The app writes one paint frame; the OS hands it to the reader in
// whatever pieces it likes (a tty buffer, a select() return, a 64-byte read).
// The split point is therefore an artifact of the READER, never a fact about the
// paint — so the same bytes at two read sizes MUST produce one screen. When they
// do not, the difference is not "a wrap" and not "the layout": it is the
// interpreter inventing or losing text, which is precisely what a reader of the
// dump cannot distinguish from a real display loss (this file's RED ancestor:
// `internal/lineedit/screen_chunk_carry_test.go`, and the measured artifact in
// devdoc/chat-paint-stream-chunk-fidelity.md).
//
// HOW AN AGENT RUNS IT (one command, no dump needed beyond the capture):
//
//	GOPI_REPLAY_RAW=/path/to/pty.raw \
//	GOPI_REPLAY_COLS=117 GOPI_REPLAY_ROWS=26 \
//	go test ./internal/lineedit/ -run TestScreenReplayAgreesAtEveryChunkSize -v
//
// The geometry comes from the capture's own dump header (`terminal=117x26
// model=117x26 grid=117x26`), and with `--dump-screen` the header is the proof
// that the numbers are the layout's and not a fallback. Set GOPI_REPLAY_CHUNK to
// check one specific read size instead of the default ladder, and GOPI_REPLAY_OUT
// to also write the (whole-feed) grid to a file for a side-by-side diff against
// another emulator's render of the same bytes.
//
// It SKIPS without GOPI_REPLAY_RAW: there is no capture to replay, and an
// unconditional pass would assert nothing (a skip that names its missing input is
// honest; a green that measured nothing is not).

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// replayChunkLadder is the read-size ladder the criterion is checked over.
// 1 and 2 split every sequence at every possible offset (the maximal case);
// the larger values model the buffer sizes a real reader uses (a tty read, a
// select() return, a copy buffer). Named (Rule 1) because the VALUES are the
// subject: a single size cannot distinguish "the interpreter is chunk-proof"
// from "this particular split happened to be harmless".
var replayChunkLadder = []int{1, 2, 3, 7, 13, 64, 333, 4096}

// replayEnv* are the environment hooks this check reads. They are process-level
// inputs by design: the capture's path and geometry come from a DIFFERENT
// process's run (the surface being observed), so they cannot be compiled in.
const (
	replayEnvRaw   = "GOPI_REPLAY_RAW"
	replayEnvCols  = "GOPI_REPLAY_COLS"
	replayEnvRows  = "GOPI_REPLAY_ROWS"
	replayEnvChunk = "GOPI_REPLAY_CHUNK"
	replayEnvOut   = "GOPI_REPLAY_OUT"
)

// replayFeed runs data through a fresh Screen in step-byte reads (step <= 0
// means one call with the whole stream: the reference every existing caller —
// and every other assertion in this package — uses).
func replayFeed(data []byte, cols, rows, step int) *Screen {
	s := NewScreen(cols, rows)
	if step <= 0 {
		s.Feed(data)
		return s
	}
	for i := 0; i < len(data); i += step {
		j := i + step
		if j > len(data) {
			j = len(data)
		}
		s.Feed(data[i:j])
	}
	return s
}

// TestScreenReplayAgreesAtEveryChunkSize is the check. It reads ONE recorded pty
// capture and asserts the interpreter paints the same screen (and keeps the same
// scrollback) at every read size in the ladder.
func TestScreenReplayAgreesAtEveryChunkSize(t *testing.T) {
	rawPath := os.Getenv(replayEnvRaw)
	if rawPath == "" {
		t.Skipf("%s is not set: no captured pty stream to replay, so this check would assert nothing", replayEnvRaw)
	}
	data, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("cannot read the capture %s: %v", rawPath, err)
	}
	cols, _ := strconv.Atoi(os.Getenv(replayEnvCols)) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
	rows, _ := strconv.Atoi(os.Getenv(replayEnvRows)) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
	if cols <= 0 || rows <= 0 {
		t.Fatalf("%s/%s must carry the capture's geometry (read it off the dump's header, e.g. `terminal=117x26 grid=117x26`): got cols=%d rows=%d",
			replayEnvCols, replayEnvRows, cols, rows)
	}

	whole := replayFeed(data, cols, rows, 0)
	if out := os.Getenv(replayEnvOut); out != "" {
		if err := os.WriteFile(out, []byte(whole.Dump().Text()+"\n"), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", out, err)
		}
	}

	ladder := replayChunkLadder
	if v := os.Getenv(replayEnvChunk); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("%s=%q is not a positive read size", replayEnvChunk, v)
		}
		ladder = []int{n}
	}

	for _, step := range ladder {
		got := replayFeed(data, cols, rows, step)
		if whole.Dump().Text() != got.Dump().Text() {
			t.Errorf("capture %s at %dx%d: reading it %d bytes at a time paints a DIFFERENT screen than reading it whole — the interpreter's grid depends on how the stream was chunked, so the dump cannot be trusted as the paint\n%s",
				rawPath, cols, rows, step, firstDifferingRow(whole, got))
		}
		if whole.ScrollbackText() != got.ScrollbackText() {
			t.Errorf("capture %s at %dx%d: reading it %d bytes at a time keeps different scrollback (%d lines vs %d whole) — lines the terminal scrolled off are being lost by the read size",
				rawPath, cols, rows, step, len(got.Scrollback()), len(whole.Scrollback()))
		}
	}
	t.Logf("replayed %s (%d bytes) at %dx%d over %d read sizes: identical grids and scrollback",
		rawPath, len(data), cols, rows, len(ladder))
}

// firstDifferingRow renders the first row where two grids disagree, so a failure
// message points at the cells instead of asking the reader to diff two dumps.
// It is a diagnostic aid inside a failure path, never part of the verdict.
func firstDifferingRow(want, got *Screen) string {
	a := strings.Split(want.Dump().Text(), "\n")
	b := strings.Split(got.Dump().Text(), "\n")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			short := func(s string) string {
				if len(s) > 140 {
					return s[:140] + "…"
				}
				return s
			}
			return "first row that differs:\n  whole: " + short(a[i]) + "\n  chunk: " + short(b[i])
		}
	}
	return "the grids differ in length only"
}
