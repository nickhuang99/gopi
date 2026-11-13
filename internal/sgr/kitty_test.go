package sgr

import (
	"strings"
	"testing"
)

// TestKeyboardProtocolSequences pins the exact wire bytes of the four sequences
// the negotiation uses. The spelling is the protocol's, so a change here is a
// change to a wire format: the assertions are byte-equal on purpose.
func TestKeyboardProtocolSequences(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"query", KeyboardProtocolQuerySeq(), "\x1b[?u"},
		{"push disambiguate", KeyboardProtocolPushSeq(KeyboardProtocolDisambiguateEscapeCodes), "\x1b[>1u"},
		{"push zero", KeyboardProtocolPushSeq(0), "\x1b[>0u"},
		{"pop one", KeyboardProtocolPopSeq(KeyboardProtocolPopEntries), "\x1b[<1u"},
		{"reply with one", KeyboardProtocolReplySeq(1), "\x1b[?1u"},
		{"reply with zero", KeyboardProtocolReplySeq(0), "\x1b[?0u"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	// The push and pop are a SYMMETRIC pair by construction — one entry each —
	// which is what lets Close restore exactly what Open changed.
	if KeyboardProtocolPushEntries != KeyboardProtocolPopEntries {
		t.Errorf("push (%d) and pop (%d) entries differ: a run would leave the terminal changed",
			KeyboardProtocolPushEntries, KeyboardProtocolPopEntries)
	}
}

// TestParseKeyboardProtocolReply is the parser's contract, and its two halves
// have different failure costs: a reply it MISSES costs the feature (the
// terminal is judged unsupported), while a KEY SEQUENCE it CLAIMS would swallow
// a keystroke the operator typed inside the probe window. Both directions are
// pinned here.
func TestParseKeyboardProtocolReply(t *testing.T) {
	complete := []struct {
		name  string
		in    string
		flags int
	}{
		{"flags one", "\x1b[?1u", 1},
		{"flags zero", "\x1b[?0u", 0},
		{"no parameter", "\x1b[?u", 0},
		{"multi-digit flags", "\x1b[?17u", 17},
		{"reply followed by a keystroke", "\x1b[?1uxy", 1},
	}
	for _, c := range complete {
		reply, verdict := ParseKeyboardProtocolReply([]byte(c.in))
		if verdict != KeyboardProtocolComplete {
			t.Errorf("%s: verdict = %v, want Complete", c.name, verdict)
			continue
		}
		if reply.Flags != c.flags {
			t.Errorf("%s: flags = %d, want %d", c.name, reply.Flags, c.flags)
		}
		if want := len(c.in) - len("xy"); c.name == "reply followed by a keystroke" && reply.Bytes != want {
			t.Errorf("%s: Bytes = %d, want %d (the reply's own span only)", c.name, reply.Bytes, want)
		}
	}

	// NOT a reply: every one of these must reach the key decoder untouched.
	notReply := []struct {
		name string
		in   string
	}{
		{"shift-enter CSI-u (a KEY, not an answer)", "\x1b[13;2u"},
		{"CSI-u two-byte form", "\x1b[Au"},
		{"plain text", "hello"},
		{"a bare carriage return", "\r"},
		{"private mode set (CSI ? 25 h)", "\x1b[?25h"},
		{"device attributes (CSI > 0 c)", "\x1b[>0c"},
		{"absurd flags parameter", "\x1b[?99999999999999999999u"},
	}
	for _, c := range notReply {
		if _, verdict := ParseKeyboardProtocolReply([]byte(c.in)); verdict != KeyboardProtocolNoReply {
			t.Errorf("%s: verdict = %v, want NoReply", c.name, verdict)
		}
	}

	// PARTIAL: a reply is a byte stream and may arrive split. Every strict
	// prefix of a reply is Partial — never NoReply (that would replay half a
	// sequence to the decoder) and never Complete (that would invent an answer).
	full := "\x1b[?1u"
	for i := 1; i < len(full); i++ {
		prefix := full[:i]
		if _, verdict := ParseKeyboardProtocolReply([]byte(prefix)); verdict != KeyboardProtocolPartial {
			t.Errorf("prefix %q (%d bytes of %q): verdict = %v, want Partial", prefix, i, full, verdict)
		}
	}
}

// TestReplyParserNeverClaimsAKeySequence is the property the prefetch path
// depends on, stated as a sweep rather than as examples: for every sequence the
// key decoder knows, this parser must answer NoReply — otherwise the probe
// would eat the operator's key and the editor would never see it.
func TestReplyParserNeverClaimsAKeySequence(t *testing.T) {
	keySequences := []string{
		"\r", "\n", "\x1b\r", "\x1b[13;2u", "\x1b[13;5u", "\x1b[A", "\x1b[1;5C",
		"\x1b[3~", "\x1b[200~", "\x1b[201~", "a", "\x1b[1;3B", "\x1b[H",
	}
	for _, seq := range keySequences {
		if got, verdict := ParseKeyboardProtocolReply([]byte(seq)); verdict != KeyboardProtocolNoReply {
			t.Errorf("the parser claimed %q (verdict %v, flags %d) — a key typed during the probe would be eaten",
				seq, verdict, got.Flags)
		}
	}
}

// TestKeyboardProtocolReplyRoundTrips pins that the emitted reply spelling and
// the parser agree: the test's own fixture generator and the production parser
// are one grammar (Rule 14).
func TestKeyboardProtocolReplyRoundTrips(t *testing.T) {
	for _, flags := range []int{0, 1, 2, 17} {
		seq := KeyboardProtocolReplySeq(flags)
		reply, verdict := ParseKeyboardProtocolReply([]byte(seq))
		if verdict != KeyboardProtocolComplete {
			t.Fatalf("reply %q for flags %d: verdict = %v, want Complete", seq, flags, verdict)
		}
		if reply.Flags != flags {
			t.Errorf("reply %q parsed as flags %d, want %d", seq, reply.Flags, flags)
		}
		if reply.Bytes != len(seq) {
			t.Errorf("reply %q consumed %d bytes, want %d", seq, reply.Bytes, len(seq))
		}
	}
}

// TestKittySpellingsAreComposedFromTheVocabulary keeps the new module on the
// package's own rule: no sequence is a typed literal, every one names the
// introducer, the prefix and the final byte it is built from. The gate
// (internal/standards/sgr_single_source_gate_test.go) enforces this
// tree-wide; this assertion pins the three facts the kitty builders must share
// with the rest of the package.
func TestKittySpellingsAreComposedFromTheVocabulary(t *testing.T) {
	for _, seq := range []string{
		KeyboardProtocolQuerySeq(),
		KeyboardProtocolPushSeq(1),
		KeyboardProtocolPopSeq(1),
		KeyboardProtocolReplySeq(1),
	} {
		if !strings.HasPrefix(seq, CSI) {
			t.Errorf("%q does not start with the package's CSI introducer", seq)
		}
		if !strings.HasSuffix(seq, string(FinalCSIu)) {
			t.Errorf("%q does not end with the named final byte", seq)
		}
	}
	if FinalCSIu == FinalSGR || FinalCSIu == FinalPrivateModeOn {
		t.Errorf("FinalCSIu collides with another command's final byte (%c)", FinalCSIu)
	}
}
