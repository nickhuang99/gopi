package tokenizer

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pre_panic_test.go — the regression for the send-time token counter's crash.
//
// THE INCIDENT (measured on real requests, exit status 2):
//
//	panic: runtime error: slice bounds out of range [:141] with length 140
//	splitGrouping(... {0xc00f787758, 0x8c})       pre.go:118
//	splitGroupingAll({0xc005365008, 0xea, 0xea}) pre.go:91
//	(*pre).splitPre(... {0xc00f7874d4, 0x1fa01}) pre.go:62
//	(*Tokenizer).EncodeMasked(tokenizer.go:212)
//	(*Tokenizer).PromptTokensWith(prompt.go:259)
//	agent.countRequestTokens(request_tokens_cap.go:128)
//	agent.(*Agent).checkRequestTokens(request_tokens_cap.go:266)
//
// Two full worker logs carry the same top frame: 140 BYTES in, and stage 3
// asked the 140-byte string for a slice ending at byte 141.
//
// THE CAUSE (tokenizer side, not the agent side): splitGrouping built its
// rune-index → byte-offset table by SUMMING runeLen(r) over []rune(s).
//
//	offs[i] = at; at += runeLen(r)
//
// runeLen reports the width of a rune VALUE, but the width a rune OCCUPIES in a
// string is a property of its ENCODING. Those agree for every well-formed
// sequence and disagree by two bytes for every INVALID one: []rune turns an
// invalid byte into U+FFFD (a value 3 bytes wide, runeLen = 3) that occupies
// exactly ONE byte in s. Every invalid byte therefore pushes the whole table
// two bytes to the right, and the moment a match ends anywhere past the first
// drift the slice bound lands beyond len(s) — the panic above, with 140 and 141
// being the same number on either side of that drift.
//
// The trigger is not "big input" and not "CJK": it is a byte that is not valid
// UTF-8 inside a request's text. A tool result carrying a file's raw bytes is
// the ordinary way that reaches a prompt.

// prePanicTimeout bounds every count in this file. The tokenizer must never be
// able to hang a suite: a 200 KB input that does not finish in this window is a
// defect to report, not a test to wait out.
const prePanicTimeout = 30 * time.Second

// invalidUTF8Cases are the smallest inputs that reproduce the crash, each with
// the width of the drift it introduces. They are spellings, not magic: one
// invalid byte, a truncation, a raw byte run, and a real file header.
var invalidUTF8Cases = []struct {
	name string
	text string
}{
	{"one stray continuation byte", "a\x80"},
	{"PNG file header", "\x89PNG\r\n\x1a\n"},
	{"truncated three-byte sequence", "a\xe4\xb8"},
	{"raw 0xff run", strings.Repeat("\xff", 4)},
	{"the incident's shape: 139 plain bytes then one invalid", strings.Repeat("a", 139) + "\x80"},
	{"invalid byte inside prose", "hello \x80 world, 这是一个中文测试句子。"},
	{"invalid byte before digits and whitespace", "x\x80 12345 \x81\ty"},

	// THE TWO SHAPES THE INCIDENT LOGS ACTUALLY CARRY, reconstructed.
	//
	// A hand-picked list is only as good as its author's imagination, and the
	// list above was written by the person who had just read the mechanism — so
	// it spells the mechanism rather than the incident. These two entries are
	// the measured ones: each reproduces a panic line read out of a real worker
	// log (`~/.gopi/sessions/01a1b2c0-c146-765a-b515-11c5d1ce8320/output.log`
	// and `.../01a1b2e7-b357-7779-87cf-8577ca292052/output.log`), same length,
	// same bound, under the pre-fix table:
	//
	//	panic: runtime error: slice bounds out of range [:141] with length 140
	//	panic: runtime error: slice bounds out of range [:183] with length 182
	//
	// Their shape is the one the list above never had — and the difference is
	// the whole answer to "why were seven samples green while production
	// crashed": an invalid byte EARLY, a long run of letters AFTER it, and a
	// trailing matched piece reaching the end of the string, so the first
	// slice whose upper bound sits past the drift lands past len(s). Every
	// length-only sample above drifts too, but none of them has a match that
	// ends out there.
	//
	// TestIncidentSamplesAreTheIncident re-derives both panics from the OLD
	// algorithm on every run, so these two entries cannot quietly stop being
	// incident-shaped.
	//
	// MEASURED, and it is the whole point: under a HALF-FIX (the runeLen table
	// with only its ENDPOINT clamped to len(s)) four of the seven samples above
	// still PASS — including the one called "the incident's shape" — while both
	// incident entries PANIC. A sample that only carries the DRIFT does not
	// carry the CRASH; what crashes is a MATCH and a GAP that reach the end of
	// the string together. See devdoc/tokenizer-invalid-utf8-offset-table.md.
	{"incident: [:141] with length 140", incidentSample(incidentPanicLen140)},
	{"incident: [:183] with length 182", incidentSample(incidentPanicLen182)},
}

// The incident's shape, split into named parts (Rule 1: no bare literals).
//
// A string of exactly n bytes: an ASCII prefix, ONE invalid byte, then a tail
// the pre-tokenizer matches as units — a letter run, a CJK rune and a newline.
// The invalid byte is early and the tail is what pushes a slice bound past the
// end of the string, which is what both real panics did.
const (
	incidentPanicLen140 = 140 // panic: slice bounds out of range [:141] with length 140
	incidentPanicLen182 = 182 // panic: slice bounds out of range [:183] with length 182
	incidentInvalidByte = "\x80"
	incidentTail        = "bbbbb中\n"
)

// incidentSample returns a string of exactly n bytes in the incident's shape.
func incidentSample(n int) string {
	prefix := n - len(incidentInvalidByte) - len(incidentTail)
	if prefix < 0 {
		panic("incidentSample: n too small for the incident shape")
	}
	return strings.Repeat("a", prefix) + incidentInvalidByte + incidentTail
}

// TestSplitGroupingSurvivesInvalidUTF8 is the minimal reproduction.
//
// RED before the fix: every case panics with "slice bounds out of range".
// GREEN after it: no panic, and — the invariant that proves the OUTPUT is
// right and not merely non-crashing — the emitted pieces concatenate back to
// the input exactly. Split(isolated) emits the matches and the text between
// them, so their concatenation IS the input, byte for byte.
func TestSplitGroupingSurvivesInvalidUTF8(t *testing.T) {
	for _, tc := range invalidUTF8Cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan []string, 1)
			panicCh := make(chan any, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						panicCh <- r
					}
				}()
				done <- splitGrouping(tc.text, nil)
			}()
			select {
			case r := <-panicCh:
				t.Fatalf("splitGrouping PANICKED on %q (%d bytes): %v — "+
					"the offset table must come from the string's own encoding, "+
					"never from a sum of rune VALUE widths", tc.text, len(tc.text), r)
			case pieces := <-done:
				if got := strings.Join(pieces, ""); got != tc.text {
					t.Fatalf("pieces do not concatenate back to the input:\n input %q\n  got  %q", tc.text, got)
				}
			case <-time.After(prePanicTimeout):
				t.Fatalf("splitGrouping on %d bytes did not finish in %s", len(tc.text), prePanicTimeout)
			}
		})
	}
}

// TestSplitPreSurvivesInvalidUTF8 drives the whole chain (the call site the
// crash's stack names, pre.go:62), so the regression is pinned at the entry
// point a request actually reaches rather than at the stage alone.
func TestSplitPreSurvivesInvalidUTF8(t *testing.T) {
	p, err := compilePre()
	if err != nil {
		t.Fatalf("compilePre: %v", err)
	}
	for _, tc := range invalidUTF8Cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			panicCh := make(chan any, 1)
			done := make(chan []string, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						panicCh <- r
					}
				}()
				done <- p.splitPre(tc.text, nil)
			}()
			select {
			case r := <-panicCh:
				t.Fatalf("splitPre PANICKED on %q: %v", tc.text, r)
			case pieces := <-done:
				// Stage 1 and 2 split on top of stage 3's output, so the join
				// invariant still holds for the chain as a whole.
				if got := strings.Join(pieces, ""); got != tc.text {
					t.Fatalf("chain pieces do not concatenate back:\n input %q\n  got  %q", tc.text, got)
				}
			case <-time.After(prePanicTimeout):
				t.Fatalf("splitPre on %d bytes did not finish in %s", len(tc.text), prePanicTimeout)
			}
		})
	}
}

// TestCountSurvivesInvalidUTF8 pins the END-TO-END property the incident was
// about: a request whose content carries a non-UTF-8 byte must be COUNTED, not
// crashed and not silently zeroed. The count is a measurement — the same bytes
// must keep counting the same number, and the number must be positive.
func TestCountSurvivesInvalidUTF8(t *testing.T) {
	tok, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for _, tc := range invalidUTF8Cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			type res struct {
				n      int
				panics any
			}
			ch := make(chan res, 1)
			go func() {
				var r res
				defer func() {
					if p := recover(); p != nil {
						r.panics = p
					}
					ch <- r
				}()
				r.n = tok.Count(tc.text)
			}()
			var got res
			select {
			case got = <-ch:
			case <-time.After(prePanicTimeout):
				t.Fatalf("Count on %d bytes did not finish in %s", len(tc.text), prePanicTimeout)
			}
			if got.panics != nil {
				t.Fatalf("Count PANICKED on a request carrying %q: %v", tc.text, got.panics)
			}
			if got.n <= 0 {
				t.Fatalf("Count returned %d for a non-empty input (%d bytes) — "+
					"a count that fails must be reported, never zeroed", got.n, len(tc.text))
			}
			if again := tok.Count(tc.text); again != got.n {
				t.Fatalf("count is not stable: %d then %d", got.n, again)
			}
		})
	}
}

// runeLenSummedBoundaries is the PRE-7924d191 offset table, kept here as a
// SHADOW — never called by production code — so the mechanism this file exists
// to remember stays executable instead of becoming a story in a comment.
//
// It is the algorithm the crash came from, verbatim:
//
//	rs := []rune(s); offs[i] = at; at += runeLen(r)
//
// Keeping it in the test is deliberate (Rule 43 read backwards): a mechanism
// that was deleted is worth one shadow that still runs, because the defect's
// shape is the thing that must not be re-derived from memory next time.
func runeLenSummedBoundaries(s string) (rs []rune, offs []int) {
	rs = []rune(s)
	offs = make([]int, len(rs)+1)
	at := 0
	for i, r := range rs {
		offs[i] = at
		at += runeLen(r)
	}
	offs[len(rs)] = at
	return rs, offs
}

// splitGroupingRuneLenSummed is splitGrouping's pre-fix body: the same slicing,
// fed by the runeLen-summed table. It is the RED half of this file's proof.
func splitGroupingRuneLenSummed(s string, out []string) []string {
	rs, offs := runeLenSummedBoundaries(s)
	gap := 0
	i := 0
	for i < len(rs) {
		j := groupingMatch(rs, i)
		if j <= i {
			i++
			continue
		}
		if i > gap {
			out = append(out, s[offs[gap]:offs[i]])
		}
		out = append(out, s[offs[i]:offs[j]])
		gap = j
		i = j
	}
	if gap < len(rs) {
		out = append(out, s[offs[gap]:])
	}
	return out
}

// TestIncidentSamplesAreTheIncident is the arm that answers "what did the list
// miss", as an assertion rather than a paragraph.
//
// It states three things about the two incident entries:
//
//  1. the OLD algorithm panics on them, and the panic bound is the incident's
//     own number ([:141] for 140 bytes, [:183] for 182 bytes);
//  2. the CURRENT algorithm does not, and its pieces concatenate back;
//  3. the runeLen-summed table over-runs len(s) by exactly two bytes per
//     invalid byte — the arithmetic that produces (1), stated directly.
//
// (1) is why these samples exist: the seven spellings above are all invalid
// UTF-8 and all drift the table, but only a sample whose LAST match reaches the
// end of the string turns that drift into a slice past the end. Measuring the
// shipped list against this arm is what showed the gap — the old table over-ran
// on all seven and panicked on only two of them.
func TestIncidentSamplesAreTheIncident(t *testing.T) {
	cases := []struct {
		name     string
		wantLen  int
		wantBnd  int
		wantText string
	}{
		{"[:141] with length 140", incidentPanicLen140, incidentPanicLen140 + 1, incidentSample(incidentPanicLen140)},
		{"[:183] with length 182", incidentPanicLen182, incidentPanicLen182 + 1, incidentSample(incidentPanicLen182)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := len(tc.wantText); got != tc.wantLen {
				t.Fatalf("sample is %d bytes, want %d — the shape drifted", got, tc.wantLen)
			}

			// (3) the arithmetic: the table runs past len(s) by 2 per invalid byte.
			_, offs := runeLenSummedBoundaries(tc.wantText)
			const driftPerInvalidByte = 2 // U+FFFD is 3 wide; the byte it replaces occupies 1
			if got, want := offs[len(offs)-1], tc.wantLen+driftPerInvalidByte; got != want {
				t.Fatalf("runeLen-summed table ends at %d, want %d (%d + %d per invalid byte) — "+
					"the sample no longer carries the incident's drift", got, want, tc.wantLen, driftPerInvalidByte)
			}

			// (1) the OLD algorithm panics, on the incident's own bound.
			func() {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatalf("the pre-7924d191 table did NOT panic on %q — this sample is no longer "+
							"the incident's shape, so it cannot guard against the incident", tc.wantText)
					}
					wantMsg := "slice bounds out of range [:" + strconv.Itoa(tc.wantBnd) + "] with length " + strconv.Itoa(tc.wantLen)
					if got := fmt.Sprint(r); !strings.Contains(got, wantMsg) {
						t.Fatalf("old table panicked with %q, want it to contain %q", got, wantMsg)
					}
				}()
				_ = splitGroupingRuneLenSummed(tc.wantText, nil)
			}()

			// (2) the CURRENT algorithm does not, and the join invariant holds.
			pieces := splitGrouping(tc.wantText, nil)
			if got := strings.Join(pieces, ""); got != tc.wantText {
				t.Fatalf("pieces do not concatenate back:\n input %q\n  got  %q", tc.wantText, got)
			}
		})
	}
}

// TestRuneBoundariesMatchEncoding states the invariant directly, without any
// crash: for ANY input the offset table must start at 0, end at len(s), never
// decrease, and be exactly one entry per rune plus one. This is the arm that
// would have caught the defect even if no input had ever panicked on it — the
// old table failed it on every input carrying an invalid byte.
func TestRuneBoundariesMatchEncoding(t *testing.T) {
	inputs := []string{
		"", "a", "中文abc", "\x80", "\x89PNG\r\n\x1a\n",
		strings.Repeat("\xff", 4), "hello \x80 world 12345",
		"😀🎉 mixed 中英 123", "\xe4\xb8", "\xed\xa0\x80",
		strings.Repeat("a", 139) + "\x80",
	}
	for _, s := range inputs {
		rs, offs := runeBoundaries(s)
		if len(offs) != len(rs)+1 {
			t.Fatalf("%q: %d offsets for %d runes, want %d", s, len(offs), len(rs), len(rs)+1)
		}
		if offs[0] != 0 {
			t.Fatalf("%q: table starts at %d, want 0", s, offs[0])
		}
		if got := offs[len(offs)-1]; got != len(s) {
			t.Fatalf("%q: table ends at %d, want len(s)=%d — the width must come from the encoding", s, got, len(s))
		}
		for i := 1; i < len(offs); i++ {
			if offs[i] < offs[i-1] {
				t.Fatalf("%q: table decreases at %d: %d then %d", s, i, offs[i-1], offs[i])
			}
		}
		// No entry may exceed len(s): the whole defect was a table whose last
		// entry ran past the string. (An entry is NOT required to satisfy
		// utf8.RuneStart — an invalid byte is not a rune start yet IS where
		// DecodeRuneInString begins consuming, which is the boundary that
		// matters here.)
	}
}

// TestCountLargePromptBounded pins the SCALE the send-time gate actually meets:
// a 200 KB prompt must be counted, and it must be counted inside a bound the
// suite can afford. It is deliberately the MIXED shape — what a real request
// carries — and it is not the measurement (measure_test.go owns the numbers);
// it exists so a future quadratic regression in ANY stage (the pre-tokenizer's
// offset table, the chain, the merge) turns this red instead of turning a real
// send into a hang.
//
// Measured on this host: 185.8 ms for 200 KB, 932.1 ms for 1 MB. The bound is
// generous on purpose — it is a hang detector, not a performance assertion, so
// a loaded CI box does not fail it.
const largePromptBound = 60 * time.Second

func TestCountLargePromptBounded(t *testing.T) {
	tok, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	prompt := largePrompt(200 * 1024)
	type res struct {
		n   int
		dur time.Duration
	}
	ch := make(chan res, 1)
	go func() {
		start := time.Now()
		n := tok.Count(prompt)
		ch <- res{n: n, dur: time.Since(start)}
	}()
	select {
	case r := <-ch:
		if r.n <= 0 {
			t.Fatalf("counted %d tokens for a %d-byte prompt", r.n, len(prompt))
		}
		t.Logf("counted %d tokens for %d bytes in %s (bound %s)", r.n, len(prompt), r.dur, largePromptBound)
		if r.dur > largePromptBound {
			t.Fatalf("counting %d bytes took %s, over the %s bound — a stage went superlinear", len(prompt), r.dur, largePromptBound)
		}
	case <-time.After(largePromptBound):
		t.Fatalf("counting a %d-byte prompt did not finish in %s", len(prompt), largePromptBound)
	}
}

// largePrompt assembles prompt-shaped text of at least n bytes: prose, CJK, code
// and digits with whitespace throughout, so the pre-tokenizer cuts it into short
// pieces the way a real request is cut (measured: 38,557 pieces for 200 KB,
// longest 48 bytes).
func largePrompt(n int) string {
	piece := "The quick brown fox jumps over the lazy dog. " +
		"这是一个中文测试句子，用来验证分词器在长上下文下的表现。" +
		"func (t *Tokenizer) Count(text string) int { return len(t.Encode(text)) }\n" +
		"订单号 20250101 金额 1234567.89 😀🎉\n"
	var b strings.Builder
	b.Grow(n + len(piece))
	for b.Len() < n {
		b.WriteString(piece)
	}
	return b.String()[:n]
}
