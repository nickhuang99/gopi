package lineedit

import (
	"strings"
	"testing"
	"time"
)

// TestLineEditorValueRuneBuffer walks the A1 editing model: insert, backspace,
// delete, the cursor motions (including word motions), kill + yank — asserted on
// the buffer and the RUNE cursor, which is the contract the renderer draws.
func TestLineEditorValueRuneBuffer(t *testing.T) {
	v := NewValue(0, 0)
	v.InsertString("hello world")
	if got := v.Text(); got != "hello world" {
		t.Fatalf("text = %q, want %q", got, "hello world")
	}
	if v.Cursor() != len([]rune("hello world")) {
		t.Fatalf("cursor = %d, want at end (%d)", v.Cursor(), len("hello world"))
	}

	v.MoveLeft()
	v.MoveLeft()
	if v.Cursor() != 9 {
		t.Fatalf("after 2×left cursor = %d, want 9", v.Cursor())
	}
	v.Backspace()
	if got := v.Text(); got != "hello wold" {
		t.Fatalf("after backspace text = %q, want %q", got, "hello wold")
	}
	if v.Cursor() != 8 {
		t.Fatalf("after backspace cursor = %d, want 8", v.Cursor())
	}
	v.DeleteForward()
	if got := v.Text(); got != "hello wod" {
		t.Fatalf("after delete text = %q, want %q", got, "hello wod")
	}

	v.Home()
	if v.Cursor() != 0 {
		t.Fatalf("Home cursor = %d, want 0", v.Cursor())
	}
	v.End()
	if v.Cursor() != v.Len() {
		t.Fatalf("End cursor = %d, want %d", v.Cursor(), v.Len())
	}

	// Word motions: from the end, two word-lefts lands on the start of "wod".
	v.MoveWordLeft()
	if got := v.Cursor(); got != 6 {
		t.Fatalf("MoveWordLeft cursor = %d, want 6", got)
	}
	v.MoveWordLeft()
	if got := v.Cursor(); got != 0 {
		t.Fatalf("second MoveWordLeft cursor = %d, want 0", got)
	}
	v.MoveWordRight()
	if got := v.Cursor(); got != 5 {
		t.Fatalf("MoveWordRight cursor = %d, want 5 (past 'hello' + space)", got)
	}

	// Kill + yank: Ctrl+U takes everything before the cursor, Ctrl+Y puts it back.
	v.KillToStart()
	if got := v.Text(); got != " wod" {
		t.Fatalf("KillToStart text = %q, want %q", got, " wod")
	}
	v.Yank()
	if got := v.Text(); got != "hello wod" {
		t.Fatalf("after yank text = %q, want %q", got, "hello wod")
	}

	v.Home()
	v.KillToEnd()
	if got := v.Text(); got != "" {
		t.Fatalf("KillToEnd text = %q, want empty", got)
	}
	v.Yank()
	if got := v.Text(); got != "hello wod" {
		t.Fatalf("yank after KillToEnd = %q, want %q", got, "hello wod")
	}
}

// TestLineEditorValueWideRunes asserts the buffer counts RUNES (a CJK character
// is one cursor step, two display cells) — the property that separates a
// rune-cursor editor from a byte- or column-cursor one.
func TestLineEditorValueWideRunes(t *testing.T) {
	v := NewValue(0, 0)
	v.InsertString("知知知")
	if v.Len() != 3 {
		t.Fatalf("len = %d, want 3 runes", v.Len())
	}
	if got := v.colsUpTo(v.Cursor()); got != 6 {
		t.Fatalf("display columns = %d, want 6", got)
	}
	v.MoveLeft()
	v.Backspace()
	if got := v.Text(); got != "知知" {
		t.Fatalf("text after deleting one wide rune = %q, want %q", got, "知知")
	}
}

// TestLineEditorValueBound refuses insertions past the buffer bound instead of
// silently truncating (Rule 15: a refusal must be visible, which is what the
// editor's bell reports).
func TestLineEditorValueBound(t *testing.T) {
	v := NewValue(4, 0)
	if !v.InsertRune('a') || !v.InsertRune('b') || !v.InsertRune('c') || !v.InsertRune('d') {
		t.Fatal("first four insertions must be accepted")
	}
	if v.InsertRune('e') {
		t.Fatal("fifth insertion must be refused at the rune bound")
	}
	if got := v.Text(); got != "abcd" {
		t.Fatalf("text = %q, want %q (refusal must not corrupt the buffer)", got, "abcd")
	}
	if n := v.InsertString("xyz"); n != 3 {
		t.Fatalf("InsertString refused = %d, want 3", n)
	}
}

// TestLineEditorDecoderKeys asserts the byte→key decoding that gives the editor
// its bindings, including the two newline encodings: LF is always content, CR
// (a real Enter) is a submit candidate.
func TestLineEditorDecoderKeys(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []KeyCode
	}{
		{"runes", "ab", []KeyCode{KeyRune, KeyRune}},
		{"enter-cr", "\r", []KeyCode{KeyEnter}},
		{"newline-lf", "\n", []KeyCode{KeyNewline}},
		{"arrows", "\x1b[A\x1b[B\x1b[C\x1b[D", []KeyCode{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{"word-motions", "\x1b[1;5D\x1b[1;5C", []KeyCode{KeyWordLeft, KeyWordRight}},
		{"home-end", "\x1b[H\x1b[F\x1b[1~\x1b[4~", []KeyCode{KeyHome, KeyEnd, KeyHome, KeyEnd}},
		{"alt-enter", "\x1b\r", []KeyCode{KeyNewline}},
		{"shift-enter-csi-u", "\x1b[13;2u", []KeyCode{KeyNewline}},
		{"delete", "\x1b[3~", []KeyCode{KeyDelete}},
		{"ctrl", "\x03\x04\x0b\x15\x19", []KeyCode{KeyInterrupt, KeyEOF, KeyKillToEnd, KeyKillToStart, KeyYank}},
		{"bracketed-paste", "\x1b[200~hi\x1b[201~", []KeyCode{KeyPasteStart, KeyRune, KeyRune, KeyPasteEnd}},
		{"unknown-control-dropped", "\x02\x01", []KeyCode{KeyLeft, KeyHome}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewDecoder()
			got := d.Feed([]byte(c.in))
			if len(got) != len(c.want) {
				t.Fatalf("decoded %d keys (%v), want %d (%v)", len(got), codes(got), len(c.want), c.want)
			}
			for i := range got {
				if got[i].Code != c.want[i] {
					t.Fatalf("key %d = %v, want %v", i, got[i].Code, c.want[i])
				}
			}
		})
	}
}

// TestLineEditorDecoderHoldsIncompleteSequence is the property that makes parsing
// bytes (rather than reading keys) correct: an escape sequence split across two
// reads must not be guessed at.
func TestLineEditorDecoderHoldsIncompleteSequence(t *testing.T) {
	d := NewDecoder()
	if keys := d.Feed([]byte("\x1b")); len(keys) != 0 {
		t.Fatalf("a lone ESC must be held, got %v", codes(keys))
	}
	keys := d.Feed([]byte("[A"))
	if len(keys) != 1 || keys[0].Code != KeyUp {
		t.Fatalf("split Up decoded to %v, want [KeyUp]", codes(keys))
	}
}

// TestLineEditorDecoderSplitsRune asserts a multi-byte rune split across two
// reads is assembled, not rendered as two replacement characters.
func TestLineEditorDecoderSplitsRune(t *testing.T) {
	full := []byte("知")
	d := NewDecoder()
	if keys := d.Feed(full[:1]); len(keys) != 0 {
		t.Fatalf("half a rune must be held, got %v", codes(keys))
	}
	keys := d.Feed(full[1:])
	if len(keys) != 1 || keys[0].Rune != '知' {
		t.Fatalf("split rune decoded to %v", keys)
	}
}

// codes renders decoded keys for failure messages.
func codes(keys []Key) []KeyCode {
	out := make([]KeyCode, len(keys))
	for i, k := range keys {
		out[i] = k.Code
	}
	return out
}

// TestLineEditorValuePasteBurstClassification pins the A3 decision table: the
// state that decides whether a newline is content or a submit.
func TestLineEditorValuePasteBurstClassification(t *testing.T) {
	base := time.Unix(editorTestEpoch, 0)

	t.Run("typing is not a burst", func(t *testing.T) {
		v := NewValue(0, 0)
		now := base
		for i := 0; i < 10; i++ {
			v.NoteKeys(1, ReadOriginTyped, now)
			now = now.Add(60 * time.Millisecond) // human typing cadence
		}
		if v.BurstActive() {
			t.Fatal("slow single keys must not register as a paste burst")
		}
		if v.Enter(ReadOriginTyped, now) {
			t.Fatal("an Enter after typing must submit, not be read as pasted content")
		}
	})

	t.Run("burst makes Enter content", func(t *testing.T) {
		// THE NARROW ARM FIRST: a burst armed by the read's SIZE alone is judged
		// by PasteBurstCharInterval — the gap this package defines as "the same
		// arrival" — because a terminal that COALESCED a few typed keys produces
		// the same read with no paste behind it (see
		// paste_burst_evidence_test.go, which pins the Enter 50 ms later).
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars, ReadOriginTyped, base)
		if !v.BurstActive() {
			t.Fatalf("%d back-to-back keys must be a burst", PasteBurstMinChars)
		}
		if !v.Enter(ReadOriginTyped, base.Add(PasteBurstCharInterval)) {
			t.Fatalf("an Enter %v after a size-armed read must be read as the paste's own tail, not a submit", PasteBurstCharInterval)
		}
		if got := v.Text(); got != "\n" {
			t.Fatalf("text = %q, want the pasted newline in the buffer", got)
		}
	})

	t.Run("a framed burst keeps the full window", func(t *testing.T) {
		// THE WIDE ARM: the terminal FRAMED the block (CSI 200 ~ / 201 ~), so it
		// stated it was a paste, and PasteEnterSuppressWindow — untouched at 120 ms
		// — is what turns "pasted N lines" into one message.
		v := NewValue(0, 0)
		v.NoteBurst(PasteBurstMinChars, ReadOriginTyped, base)
		if !v.BurstActive() || !v.BurstFramed() {
			t.Fatalf("a framed burst must arm and record itself as framed (active=%v framed=%v)", v.BurstActive(), v.BurstFramed())
		}
		if !v.Enter(ReadOriginTyped, base.Add(PasteEnterSuppressWindow-time.Millisecond)) {
			t.Fatal("an Enter inside the framed suppress window must be read as content, not a submit")
		}
		if got := v.Text(); got != "\n" {
			t.Fatalf("text = %q, want the pasted newline in the buffer", got)
		}
	})

	t.Run("burst expires", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars, ReadOriginTyped, base)
		if v.Enter(ReadOriginTyped, base.Add(PasteEnterSuppressWindow+time.Millisecond)) {
			t.Fatal("an Enter past the window is the user pressing Enter — it must submit")
		}
	})

	t.Run("newlines inside a paste are content", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars, ReadOriginTyped, base)
		// A pasted block whose copy ended with a blank line: every newline of
		// it is content, and each one extends the window, so nothing submits.
		for i := 0; i < 3; i++ {
			if !v.Enter(ReadOriginTyped, base) {
				t.Fatalf("newline %d inside the paste must be content", i)
			}
		}
		if got := v.Text(); got != "\n\n\n" {
			t.Fatalf("text = %q, want the three pasted newlines preserved in order", got)
		}
		if !strings.Contains(v.Text(), "\n") {
			t.Fatal("multiline content must be preserved")
		}
	})

	// THE DEFECT THIS TABLE NOW GUARDS (measured, task 01a189b3): four keys
	// arriving 8 ms apart — a fast human, an IME committing a word, a key
	// repeat — used to ACCUMULATE into a burst and turn the next Enter into a
	// newline with no explanation. The unit of the classification is ONE read,
	// so a run of one-key reads is typing however fast it comes, and the Enter
	// after it must submit.
	t.Run("fast single keys are typing, not a paste", func(t *testing.T) {
		v := NewValue(0, 0)
		now := base
		for i := 0; i < 4; i++ {
			v.NoteKeys(1, ReadOriginTyped, now)
			now = now.Add(PasteBurstCharInterval) // the interval itself: the closest a human's keys are put
			if v.BurstActive() {
				t.Fatalf("read %d carried ONE key at an %v gap and armed the burst — fast typing is being read as a paste", i+1, PasteBurstCharInterval)
			}
		}
		if v.Enter(ReadOriginTyped, now) {
			t.Fatal("the Enter after four fast single keys was read as pasted content; it must submit")
		}
		if got := v.Text(); got != "" {
			t.Fatalf("text = %q, want no newline in the buffer (the Enter submitted, it did not insert)", got)
		}
	})

	// The other half of the same criterion: ONE read carrying the whole block is
	// still a paste, so a multi-line write is still ONE message rather than N
	// turns. Both rows have to hold at once — that is what makes the fix a
	// criterion and not a threshold that was merely turned down.
	t.Run("one read of a multiline block is still one message", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteKeys(4, ReadOriginTyped, base) // "a\nb\n" arrives in ONE read
		if !v.BurstActive() {
			t.Fatalf("ONE read of %d keys must arm the burst — a pasted block would otherwise submit per line", 4)
		}
		if !v.Enter(ReadOriginTyped, base) {
			t.Fatal("a CR inside a pasted block must be content")
		}
		if got := v.Text(); !strings.Contains(got, "\n") {
			t.Fatalf("text = %q, want the pasted newline kept in the buffer", got)
		}
	})

	// AN INJECTED READ IS NEVER A PASTE, at any size: a script replaying a turn,
	// a tool driving the surface and a test all want their keys APPLIED, and
	// none of them is a person pasting onto this terminal. So an injected read
	// neither arms the suppression nor has its Enter swallowed.
	t.Run("an injected read never arms the burst", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars*2, ReadOriginInjected, base)
		if v.BurstActive() {
			t.Fatalf("an INJECTED read of %d keys armed the burst — a tool's own keys would then be swallowed", PasteBurstMinChars*2)
		}
		if v.Enter(ReadOriginInjected, base) {
			t.Fatal("an injected Enter was read as pasted content; it must submit")
		}
	})

	t.Run("an injected Enter is not swallowed by a pasted block", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars, ReadOriginTyped, base) // a real paste armed it
		if !v.BurstActive() {
			t.Fatalf("ONE read of %d keys must arm the burst", PasteBurstMinChars)
		}
		// An injected read arriving inside the window CLEARS it (it is not part
		// of the paste), so the injected Enter that follows submits.
		v.NoteKeys(1, ReadOriginInjected, base)
		if v.Enter(ReadOriginInjected, base) {
			t.Fatal("an ENTER delivered by an injector was swallowed as pasted content")
		}
	})
}
