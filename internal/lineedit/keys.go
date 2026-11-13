package lineedit

import (
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// KeyCode identifies one decoded input key.
type KeyCode int

// Decoded key codes. Only the keys the editor acts on get a code; every other
// control byte decodes to KeyNone and is dropped (an unhandled control byte
// must never be inserted into the buffer as text).
const (
	KeyNone KeyCode = iota
	KeyRune
	KeyEnter
	KeyNewline
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	// KeyPageUp/KeyPageDown scroll the EDITING LINE's window (the user's own
	// input, when it is taller than the rows the layout gave it). They are the
	// paged motion of the pair the key row advertises; KeyScrollUp/KeyScrollDown
	// are the one-row motion (Alt+arrow, Ctrl+P/N).
	KeyPageUp
	KeyPageDown
	KeyScrollUp
	KeyScrollDown
	KeyWordLeft
	KeyWordRight
	KeyKillToEnd
	KeyKillToStart
	KeyKillWordBack
	KeyKillWordForward
	KeyYank
	KeyInterrupt
	KeyEOF
	KeyClearScreen
	KeyPasteStart
	KeyPasteEnd
	// KeyTab is the completion-accept key (A7): with a menu open it applies the
	// selection; with none it is ignored (the editor inserts no tab — a chat
	// message is not indented source code).
	KeyTab
	// KeyEscape is a LONE Escape (the decoder reports it only once the
	// ambiguity with an escape-sequence prefix has been resolved by the
	// editor's bounded ESC wait). It closes the completion menu.
	KeyEscape
	// KeyLiveFinish is Ctrl+G — "I am DONE SPEAKING".
	//
	// IT EXISTS BECAUSE THE LIVE VOICE MODE NEEDS A KEY THAT MEANS EXACTLY ONE
	// THING, and no existing key can carry it. A live dictation session ends when
	// the SPEAKER says so; the alternatives all mean something else: Enter submits
	// the line (the transcript is not finished being written), Escape is the
	// surface's "interrupt / leave" (its meaning is the operator's own text and
	// must not be overloaded with the dictation's), Ctrl+C ends the session, and
	// Ctrl+D is EOF. Ctrl+G is BEL — the terminal's own "attention" byte, unused
	// by every other binding here, and reachable in every terminal without a
	// modifier the terminal might swallow.
	//
	// THE EDITOR HAS NO HANDLER for it, which is what makes the key FREE: it
	// decodes (rather than being dropped as an unhandled control byte), it is
	// offered to the key gate, and when no gate claims it the editor does nothing
	// with it — the same inertness KeyTab has with no menu open. A surface that
	// wants "done speaking" claims it at its gate (cmd/agent's voice mode does,
	// and only while an utterance is in flight).
	KeyLiveFinish
)

// Key is one decoded input event.
type Key struct {
	Code KeyCode
	Rune rune
	Alt  bool

	// Origin says where this key's bytes came from (keysource.go). It is
	// stamped by the ONE place that knows — the read's own decoder pass
	// (Editor.applyRead) — so a key's provenance cannot be lost between the
	// source and the handler, and a consumer never has to infer it from timing
	// or from the key's content.
	//
	// THE ZERO VALUE IS "TYPED", which is what every existing caller means by a
	// Key it constructs by hand (a test driving handleKey, the injector's own
	// table): a key literal carries no provenance claim, and claiming "injected"
	// by default would be a lie about the terminal.
	Origin ReadOrigin
}

// Control bytes the decoder recognizes. Named (Rule 1b) because they are the
// terminal's wire values, compared in more than one place.
const (
	ctrlC   = 0x03
	tabByte = 0x09
	ctrlD   = 0x04
	ctrlH   = 0x08
	ctrlL   = 0x0c
	ctrlN   = 0x0e
	ctrlP   = 0x10
	ctrlU   = 0x15
	ctrlW   = 0x17
	ctrlY   = 0x19
	ctrlK   = 0x0b
	ctrlA   = 0x01
	ctrlB   = 0x02
	ctrlE   = 0x05
	ctrlF   = 0x06
	// ctrlG is BEL. Named here with its siblings because it is now compared in
	// two places — the decoder below and the live-dictation key gate — and an
	// unnamed 0x07 read as "the bell" is exactly how the wrong byte gets claimed
	// (see KeyLiveFinish).
	ctrlG          = 0x07
	crByte         = 0x0d
	lfByte         = 0x0a
	delByte        = 0x7f
	escSeqMaxBytes = 64
	utf8LeadMask   = 0x80
)

// Decoder turns a raw terminal byte stream into Key events, holding an
// incomplete escape sequence (or a rune split across two reads) for the next
// Feed. Holding rather than guessing matters: `\x1b` alone is a prefix of both
// "Up" and "Alt+Enter", and a half-delivered rune is not a rune.
type Decoder struct {
	// mu guards buf. The BUFFER HAS ONE WRITER (the read loop, through Feed and
	// FlushEscape) — the lock is not there to admit a second one. It is there
	// because the decoder's STATE is also READ from outside that goroutine: a
	// surface that asks "is the ESC-resolution window open yet" (BareEscape) or
	// "is a read a self-contained unit" (Pending) may do so from a driver
	// goroutine, and an unguarded read of a slice header the loop is reslicing is
	// a data race on the pointer and the length, not just on the bytes. Guarding
	// the whole decoder is what gives those readers a happens-before edge with
	// Feed without pretending WriteAccess is shared.
	mu  sync.Mutex
	buf []byte
}

// NewDecoder builds an empty decoder.
func NewDecoder() *Decoder { return &Decoder{} }

// Feed decodes every complete key in p and returns them in order. Any
// incomplete tail stays buffered.
func (d *Decoder) Feed(p []byte) []Key {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.buf = append(d.buf, p...)
	keys := make([]Key, 0, len(p))
	for {
		k, n, ok := d.decodeOneLocked()
		if !ok || n <= 0 {
			break
		}
		d.buf = d.buf[n:]
		if k.Code != KeyNone {
			keys = append(keys, k)
		}
	}
	// Copy the remainder so the (grown) slice does not pin p.
	d.buf = append([]byte(nil), d.buf...)
	return keys
}

// decodeOneLocked decodes the key at the head of the buffer. ok is false when the
// buffer holds only an incomplete sequence. It is the body of Feed and requires
// d.mu to be held — the name says so, because the only way to get it wrong is to
// call it without the lock.
func (d *Decoder) decodeOneLocked() (Key, int, bool) {
	if len(d.buf) == 0 {
		return Key{}, 0, false
	}
	b := d.buf[0]
	switch {
	case b == escByte:
		return d.decodeEscapeLocked()
	case b == crByte:
		return Key{Code: KeyEnter}, 1, true
	case b == lfByte:
		return Key{Code: KeyNewline}, 1, true
	case b == delByte || b == ctrlH:
		return Key{Code: KeyBackspace}, 1, true
	case b == tabByte:
		return Key{Code: KeyTab}, 1, true
	case b < printableMin:
		return decodeControl(b), 1, true
	default:
		if b&utf8LeadMask != 0 && !utf8.FullRune(d.buf) {
			return Key{}, 0, false
		}
		r, size := utf8.DecodeRune(d.buf)
		return Key{Code: KeyRune, Rune: r}, size, true
	}
}

// BareEscape reports whether the decoder holds exactly one byte: an ESC whose
// sequence has not completed. The editor uses it to bound its wait (a lone ESC
// is a key of its own — the completion menu's close — but is also the prefix of
// every arrow key, so the two readings are told apart by a timeout, exactly as
// vim's ttimeout does). Without that bound a bare ESC would sit in the buffer
// until the NEXT key arrived, and Escape would appear to do nothing.
func (d *Decoder) BareEscape() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bareEscapeLocked()
}

// bareEscapeLocked is BareEscape's body; it requires d.mu to be held.
func (d *Decoder) bareEscapeLocked() bool {
	return len(d.buf) == 1 && d.buf[0] == escByte
}

// FlushEscape resolves a pending lone ESC as the Escape key, consuming it.
func (d *Decoder) FlushEscape() Key {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.bareEscapeLocked() {
		return Key{}
	}
	d.buf = d.buf[:0]
	return Key{Code: KeyEscape}
}

// Pending reports how many bytes the decoder is holding back for the next Feed
// — an incomplete escape sequence, or the beginning of a rune split across two
// reads. It answers "has this read delivered a self-contained sequence": a
// caller that treats one read as a unit (the editor's paste spill) must not do
// so while a fragment of the previous read is still pending.
func (d *Decoder) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.buf)
}

// Take returns the bytes the decoder is holding — the same bytes Pending counts
// — and clears them. It is for the ONE caller that has decided to handle those
// bytes ITSELF: the paste envelope's opening read, which must assemble the marker
// a read boundary split, so the decoder is not left holding a fragment the
// assembler has already filed (which would decode the same bytes twice).
func (d *Decoder) Take() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := append([]byte(nil), d.buf...)
	d.buf = d.buf[:0]
	return out
}

// decodeControl maps a control byte to its key (KeyNone for unhandled ones).
func decodeControl(b byte) Key {
	switch b {
	case ctrlA:
		return Key{Code: KeyHome} // readline Ctrl+A = beginning of line
	case ctrlE:
		return Key{Code: KeyEnd}
	case ctrlB:
		return Key{Code: KeyLeft}
	case ctrlF:
		return Key{Code: KeyRight}
	case ctrlK:
		return Key{Code: KeyKillToEnd}
	case ctrlU:
		return Key{Code: KeyKillToStart}
	case ctrlW:
		return Key{Code: KeyKillWordBack}
	case ctrlY:
		return Key{Code: KeyYank}
	case ctrlL:
		return Key{Code: KeyClearScreen}
	case ctrlP, ctrlN:
		// Ctrl+P/Ctrl+N are the readline history motions — while the whole line
		// is on screen. When the editing line's window has hidden rows, they
		// scroll the WINDOW instead: a message taller than its rows has no room
		// left for the history, and a key that cannot reach the user's own text
		// is the worse loss (the history motion resumes the moment the text fits
		// or the window reaches the caret's end — see handleKey).
		if b == ctrlP {
			return Key{Code: KeyScrollUp}
		}
		return Key{Code: KeyScrollDown}
	case ctrlG:
		// Ctrl+G = "I am done speaking" (see KeyLiveFinish). It is decoded, never
		// filtered: a key a surface may claim has to reach the gate.
		return Key{Code: KeyLiveFinish}
	case ctrlC:
		return Key{Code: KeyInterrupt}
	case ctrlD:
		return Key{Code: KeyEOF}
	default:
		return Key{Code: KeyNone}
	}
}

// decodeEscapeLocked decodes an escape sequence: CSI/SS3 (arrows, home/end, delete,
// bracketed paste, CSI-u), the ESC-prefixed Alt forms, and Alt+Enter.
func (d *Decoder) decodeEscapeLocked() (Key, int, bool) {
	if len(d.buf) < 2 {
		return Key{}, 0, false
	}
	switch d.buf[1] {
	case '[', 'O':
		return d.decodeCSILocked()
	case crByte, lfByte:
		return Key{Code: KeyNewline, Alt: true}, 2, true
	case delByte, ctrlH:
		return Key{Code: KeyKillWordBack, Alt: true}, 2, true
	}
	r, size := utf8.DecodeRune(d.buf[1:])
	if r == utf8.RuneError && !utf8.FullRune(d.buf[1:]) {
		return Key{}, 0, false
	}
	switch r {
	case 'b', 'B':
		return Key{Code: KeyWordLeft, Alt: true}, 1 + size, true
	case 'f', 'F':
		return Key{Code: KeyWordRight, Alt: true}, 1 + size, true
	case 'd', 'D':
		return Key{Code: KeyKillWordForward, Alt: true}, 1 + size, true
	}
	return Key{Code: KeyRune, Rune: r, Alt: true}, 1 + size, true
}

// decodeCSILocked decodes a CSI/SS3 sequence starting at buf[1]. It requires
// d.mu to be held.
func (d *Decoder) decodeCSILocked() (Key, int, bool) {
	body := d.buf[1:]
	if body[0] == 'O' {
		if len(body) < 2 {
			return Key{}, 0, false
		}
		return mapFinal(body[1], ""), 3, true
	}
	// CSI: parameters (0x30-0x3f), intermediates (0x20-0x2f), final (0x40-0x7e).
	i := 1
	for i < len(body) && i < escSeqMaxBytes {
		c := body[i]
		if c >= 0x40 && c <= 0x7e {
			// body excludes the ESC, so the consumed length is i+2: ESC + the
			// sequence through its final byte.
			return mapFinal(c, string(body[1:i])), i + 2, true
		}
		i++
	}
	if i >= escSeqMaxBytes {
		// Absurdly long sequence: drop the ESC and resync rather than
		// buffering forever.
		return Key{Code: KeyNone}, 1, true
	}
	return Key{}, 0, false
}

// mapFinal maps a CSI final byte plus its parameters to a key.
func mapFinal(final byte, params string) Key {
	mod := csiModifier(params)
	switch final {
	case 'A':
		// Alt+Up is the ONE-ROW scroll of the editing line's window (the paged
		// motion is PageUp/PageDown); a plain Up stays the history motion it has
		// always been.
		if modifierIsWord(mod) {
			return Key{Code: KeyScrollUp}
		}
		return Key{Code: KeyUp}
	case 'B':
		if modifierIsWord(mod) {
			return Key{Code: KeyScrollDown}
		}
		return Key{Code: KeyDown}
	case 'C':
		if modifierIsWord(mod) {
			return Key{Code: KeyWordRight}
		}
		return Key{Code: KeyRight}
	case 'D':
		if modifierIsWord(mod) {
			return Key{Code: KeyWordLeft}
		}
		return Key{Code: KeyLeft}
	case 'H':
		return Key{Code: KeyHome}
	case 'F':
		return Key{Code: KeyEnd}
	case '~':
		return mapTilde(params)
	case 'u':
		// CSI-u (kitty/modern terminals): "13;2u" = Shift+Enter, "13;5u" =
		// Ctrl+Enter — both mean "insert a newline", never submit.
		if strings.HasPrefix(params, "13;") {
			return Key{Code: KeyNewline, Alt: true}
		}
		return Key{Code: KeyNone}
	default:
		return Key{Code: KeyNone}
	}
}

// CSI parameter helpers. Modifier encoding: 1 = none, 2 = Shift, 3 = Alt,
// 5 = Ctrl; a value >= 2 in the LAST parameter position means a modifier was
// held (Readline/xterm convention).
const (
	csiModNone = 1
	csiModWord = 5 // Ctrl+arrow = word motion
	csiModAlt  = 3
)

// csiModifier extracts the modifier from a parameter list ("1;5" → 5).
func csiModifier(params string) int {
	if params == "" {
		return csiModNone
	}
	parts := strings.Split(params, ";")
	last := parts[len(parts)-1]
	if n, err := strconv.Atoi(strings.TrimSuffix(last, "$")); err == nil {
		return n
	}
	return csiModNone
}

// modifierIsWord reports whether a modifier means "word motion".
func modifierIsWord(mod int) bool { return mod == csiModWord || mod == csiModAlt }

// mapTilde maps the "~"-terminated function-key sequences the editor cares
// about (home/end/delete in their VT220 spellings, and the paging keys — PageUp
// and PageDown are CSI 5~ / 6~, which is what xterm and every terminal of its
// family send, and what `gopi chat`'s own scroll mechanism is bound to).
func mapTilde(params string) Key {
	switch strings.TrimSuffix(params, ";") {
	case "1", "7":
		return Key{Code: KeyHome}
	case "5":
		return Key{Code: KeyPageUp}
	case "6":
		return Key{Code: KeyPageDown}
	case "4", "8":
		return Key{Code: KeyEnd}
	case "3":
		return Key{Code: KeyDelete}
	case "200":
		return Key{Code: KeyPasteStart}
	case "201":
		return Key{Code: KeyPasteEnd}
	default:
		return Key{Code: KeyNone}
	}
}
