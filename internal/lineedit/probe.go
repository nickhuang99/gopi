package lineedit

// probe.go — the CAPABILITY PROBE: ask the terminal, do not read its name.
//
// WHY (the defect this file exists to fix). The colour decision was made from
// DECLARATIONS: `TERM` carrying the substring "256color" and `COLORTERM` saying
// "truecolor". A declaration is a string in the environment and it can be wrong
// in both directions — a terminal forwarded over ssh keeps the name of the
// machine it was started on, a multiplexer passes its own name through, a
// wrapper sets COLORTERM for a pipe. The decision was therefore made about the
// NAME of a terminal, not about the terminal.
//
// WHAT A MEASUREMENT IS. A terminal answers questions. This probe writes four
// queries and reads what comes back inside a BOUNDED window (probeWindowMS), on
// the SAME descriptor the editor reads — no second process, no terminfo lookup,
// no new dependency (Rule 35): one in-process action whose evidence is the
// terminal's own answers.
//
// THE FOUR QUESTIONS (the wire facts live in internal/sgr, where the sequences
// are composed):
//
//   - `CSI 6 n` (DSR, cursor position) — LIVENESS. Every terminal that can
//     answer anything answers this, so it is asked first: it is the one reply
//     that separates "an answering terminal" from "silence".
//   - `CSI c` (DA1, primary device attributes) — identity, and a colour FLOOR.
//     DA1 has NO colour-depth parameter (`4` is SIXEL GRAPHICS — see
//     sgr.DAParamSixelGraphics); `22` says "ANSI colour exists at all".
//   - `OSC 11 ; ? ST` (background colour) — a POSITIVE truecolour signal: an
//     `rgb:` triplet with four hex digits per channel cannot come out of a
//     16-colour terminal.
//   - `DCS + q <hex> ST` (XTGETTCAP, terminfo `colors` and `RGB`) — the ONLY
//     query that answers DEPTH with a number (`colors=256`; `RGB=1` for 24-bit).
//     xterm's ctlseqs(ms) documents both names as recognised by it.
//
// WHAT SILENCE MEANS: "unknown", never "no colour". A terminal that answers
// nothing is one this probe could not question; the DECLARATION is then still
// there to be used, as a FALLBACK (Capability.Source).
//
// THE KEYSTROKE QUESTION, which the epic asked to settle by measurement. Answer
// bytes arrive on stdin, which is the editor's input: an unhandled reply
// `CSI ? 1 ; 2 c` would reach the decoder as `ESC [ ? 1 ; 2 c`, and the decoder
// reads CSI sequences as MOTIONS — a DA1 reply would move the cursor, and a
// `CSI 6 n` reply would be worse. Three rules therefore govern this file:
//
//  1. THE PROBE RUNS BEFORE THE FIRST READ, on the goroutine that owns the
//     descriptor (Editor.Run calls continueProbe). Nothing is reading yet, so
//     there is no race to lose a keystroke in;
//  2. EVERY BYTE THAT IS NOT PART OF AN ANSWER IS PREFETCHED (t.prefetch) and
//     REPLAYED, in order, by ReadEvent before it touches the descriptor — so a
//     key the operator pressed during the probe reaches the decoder exactly as
//     typed, and the bound on how long they could have typed is the probe's own
//     window;
//  3. THE DIVISION IS MADE BY THE GRAMMAR, not by luck: only a byte string that
//     parses as one of the four replies is an answer, and everything else —
//     including bytes that look like the START of a reply but never complete —
//     is input.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/sgr"
	"golang.org/x/sys/unix"
)

// The probe's own bounds. Each is a policy decision with a failure mode behind
// it, never an arbitrary size (Rule 1).
const (
	// probeWindowMS is how long the probe waits for answers IN TOTAL. It is
	// also the bound on how long a key pressed during the probe is held in the
	// prefetch, which is why it is small: a real terminal answers in well under
	// a millisecond (measured — see probe_pty_test.go), so the window exists
	// for a terminal that answers SLOWLY, not for one that never answers.
	probeWindowMS = 120
	// probeSliceMS is one poll slice inside the window.
	probeSliceMS = 15
	// probeMaxBytes bounds the captured answer bytes, so a terminal spraying an
	// unbounded string cannot grow this buffer without limit. Far above any real
	// answer set (the four queries' answers measure ~60 bytes on xterm); it is
	// the bound, not the expectation.
	probeMaxBytes = 512
	// probeReadBytes bounds ONE read inside the probe.
	probeReadBytes = 256
	// probePartialBytes bounds the bytes HELD while an incomplete sequence
	// waits for its next read. A terminal's reply is short (maxCSIParams is 8
	// parameters); a longer unterminated run is not a reply waiting to finish,
	// it is input, and it must not be held indefinitely.
	probePartialBytes = 64
)

// probeDepthQueries is HOW MANY questions in the probe's own query list ask for
// a DEPTH capability (sgr's XTGETTCAP names: `colors` and `RGB`). It is
// DERIVED from that list rather than typed (Rule 13): the completeness
// predicate counts answers to depth questions, so a third name added to
// CapabilityQueryNames must move this count with it — otherwise the probe would
// wait for a reply to a question it does not ask. The derivation's own guard is
// TestProbeStopPredicateCountsTheDepthQueriesItAsks, which drives the predicate
// over one answer per query the probe actually writes.
var probeDepthQueries = len(sgr.CapabilityQueryNames())

// colorEntries256 is the terminfo `colors` value at which a terminal has the
// 256-entry palette (Rule 1b: the outside world's vocabulary, compared in
// exactly one place — CapabilityMeasurement.Depth).
const colorEntries256 = 256

// finalCPR is the Cursor Position Report's final byte (`CSI r ; c R`).
const finalCPR = 'R'

// The probe's answer vocabulary: the words a record prints. Named (Rule 1b)
// because the writer and every reader (a test, a report, `gopi ui_state`) must
// spell them the same way.
const (
	ProbeAnswerNone    = "none"
	ProbeAnswerPartial = "partial"
	ProbeAnswerFull    = "full"
	ProbeAnswerYes     = "yes"
	ProbeAnswerNo      = "no"
	ProbeAnswerUnknown = "unknown"
)

// CapabilityMeasurement is what a probe MEASURED — facts, each carrying the
// evidence that produced it, so a reader (the dump artifact, a report) can check
// the conclusion instead of trusting it.
type CapabilityMeasurement struct {
	// Answered reports that at least one answer byte came back.
	Answered bool
	// CursorPositionReport reports the liveness query's answer.
	CursorPositionReport bool
	// DA1 is the DA1 answer verbatim, escaped ("" = none came back).
	DA1 string
	// BackgroundColour is the OSC 11 payload verbatim ("" = none).
	BackgroundColour string
	// Colors is the XTGETTCAP `colors` value (0 = unknown: no answer, or an
	// answer this parser could not read). 8, 16, 256 and 16777216 are the
	// values a terminfo database uses.
	Colors int
	// RGBKnown reports that XTGETTCAP answered the `RGB` capability at all;
	// RGB reports its value (non-zero = the terminal addresses 24-bit colour).
	RGBKnown bool
	RGB      bool
	// depthAnswers is HOW MANY of the depth questions the probe asked have been
	// ANSWERED — either arm: a value (`colors=256`, `RGB=1`) or an XTGETTCAP
	// refusal (`0 + r <name>`), which is the terminal stating it does not have
	// the capability. It is the completeness predicate's own fact about the
	// stream: not "do I have the answers I want" but "does every question have
	// an answer", which is what makes the probe's stop condition independent of
	// the ORDER the kernel delivers replies in. It is deliberately not part of
	// Summary(): it is read by exactly one caller (probeAnswersComplete) during
	// the probe and is meaningless to a reader of the finished measurement,
	// whose per-question evidence is already spelled out one field per question
	// (`colors`, `rgb`).
	depthAnswers int
	// Unrecognized is how many answer bytes no rule above claimed. REPORTED
	// rather than dropped: non-zero means the terminal said something this
	// parser does not understand — the difference between "silence" and "a
	// reply we could not read".
	Unrecognized int
	// Bytes is how many answer bytes were captured in total.
	Bytes int
	// Queries is the exact byte string written to the terminal, escaped for a
	// human reader. It is part of the measurement because a conclusion about a
	// terminal is only checkable against the QUESTION that was asked.
	Queries string
	// ElapsedMS is how long the probe waited before it stopped.
	ElapsedMS int
}

// Measured reports whether the probe obtained anything a decision can rest on.
// A liveness answer alone is NOT enough: "the terminal answers" is not "the
// terminal has colour depth", and treating it as evidence would let a
// measurement of nothing displace a declaration that said 256.
func (m CapabilityMeasurement) Measured() bool {
	return m.Colors > 0 || m.RGB || m.BackgroundColour != ""
}

// Depth is the measured colour depth: the deepest tier the evidence supports.
func (m CapabilityMeasurement) Depth() TermDepth {
	if m.RGB || strings.Contains(m.BackgroundColour, sgr.OSCColourPrefix) {
		return DepthTruecolor
	}
	if m.Colors >= colorEntries256 {
		return Depth256
	}
	return DepthBasic
}

// Summary renders the measurement as ONE line of `name=value` pairs, for a
// record or a report: the evidence rides beside the verdict, so a reader who
// distrusts the verdict can re-derive it.
func (m CapabilityMeasurement) Summary() string {
	da1 := m.DA1
	if da1 == "" {
		da1 = probeAnswerDash
	}
	osc := m.BackgroundColour
	if osc == "" {
		osc = probeAnswerDash
	}
	return "probe=" + probeWord(m) +
		" cpr=" + probeBoolWord(m.CursorPositionReport) +
		" da1=" + da1 +
		" osc11=" + osc +
		" colors=" + strconv.Itoa(m.Colors) +
		" rgb=" + rgbWord(m) +
		" bytes=" + strconv.Itoa(m.Bytes) +
		" unread=" + strconv.Itoa(m.Unrecognized) +
		" elapsed_ms=" + strconv.Itoa(m.ElapsedMS)
}

// probeAnswerDash marks a field the measurement has no value for. It is a word,
// not an empty string: an empty field in a `name=value` record is ambiguous
// between "absent" and "empty", and a reader must be able to tell them apart.
const probeAnswerDash = "-"

// probeBoolWord renders a measured boolean in the record's own vocabulary.
func probeBoolWord(v bool) string {
	if v {
		return ProbeAnswerYes
	}
	return ProbeAnswerNo
}

// probeWord is the measurement's one-word verdict on the terminal.
func probeWord(m CapabilityMeasurement) string {
	switch {
	case !m.Answered:
		return ProbeAnswerNone
	case m.Measured():
		return ProbeAnswerFull
	default:
		return ProbeAnswerPartial
	}
}

// rgbWord renders the RGB capability in the answer's own vocabulary.
func rgbWord(m CapabilityMeasurement) string {
	if !m.RGBKnown {
		return ProbeAnswerUnknown
	}
	return probeBoolWord(m.RGB)
}

// ProbeQueriesFor returns the byte string the probe writes, for a caller that
// wants to print or assert it without running a terminal. ONE function, so the
// question a measurement answers is never spelled twice.
func ProbeQueriesFor() string {
	q := sgr.CursorPositionQuery() + sgr.DeviceAttributesQuery() + sgr.BackgroundColourQuery()
	for _, name := range sgr.CapabilityQueryNames() {
		q += sgr.GetCapabilityQuery(name)
	}
	return q
}

// EscapedQueries renders the probe's questions so a report can quote them
// unambiguously (ESC and the string terminators are invisible in a dump).
func EscapedQueries() string { return escapeBytes(ProbeQueriesFor()) }

// escapeBytes renders control bytes as `\e` / `\xNN` so a quoted sequence reads
// exactly as it goes on the wire.
func escapeBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == 0x1b:
			b.WriteString(`\e`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// CapabilityMeasurement returns the last measurement this terminal produced
// (the zero value when no probe ran).
func (t *Terminal) CapabilityMeasurement() CapabilityMeasurement { return t.measured }

// continueProbe runs the probe at most once for this terminal. It is the
// editor's entry point (Editor.Run), so the probe happens on the goroutine that
// owns the descriptor, before the first read.
func (t *Terminal) continueProbe() {
	t.probeOnce.Do(func() { t.measured = t.runProbe() })
}

// ProbeCapability runs the probe NOW, once, and returns what it measured. A
// caller that has an open terminal and wants the answer before the editor
// starts (the chat wiring resolves its palette before the first frame) calls
// this; continueProbe then finds it already done.
func (t *Terminal) ProbeCapability() CapabilityMeasurement {
	t.continueProbe()
	return t.measured
}

// runProbe is the measurement itself: write the questions, read the answers
// inside the window, prefetch everything that is not an answer.
//
// IT ENDS EARLY WHEN THE ANSWERS ARE IN, and the condition is neither a
// COUNTDOWN nor a fixed quiet period — both were measured to be wrong (see the
// file header's note on this). It is A STATEMENT ABOUT THE STREAM: every
// question the probe asked has an answer (probeAnswersComplete), which is the
// one condition that cannot be satisfied while replies are still in flight. The
// condition it replaced named only the answers a DECISION needed, and a subset
// test is satisfiable on a partial stream — that was the defect (see
// probeAnswersComplete for the measurement). Only when every question is
// settled is there nothing left to wait for, and the remaining window would be
// spent holding the operator's own keystrokes in the prefetch for no gain.
func (t *Terminal) runProbe() CapabilityMeasurement {
	start := time.Now()
	m := CapabilityMeasurement{Queries: EscapedQueries()}
	t.write(ProbeQueriesFor())
	t.flush()
	raw := t.collectAnswers()
	m.ElapsedMS = int(time.Since(start).Milliseconds())
	m = interpretAnswers(raw, m)
	return m
}

// collectAnswers reads with the probe's bounds and returns the answer bytes,
// holding everything else (t.addPrefetch) for the read loop.
func (t *Terminal) collectAnswers() []byte {
	if t.inputFD < fdMinValid {
		return nil
	}
	deadline := time.Now().Add(probeWindowMS * time.Millisecond)
	var out, held []byte
	seen := CapabilityMeasurement{}
	for {
		if probeAnswersComplete(seen) {
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		ready, err := t.pollAnswer()
		if err != nil {
			// A poll failure inside the probe is not the editor's error to
			// raise: the read loop's own poll surfaces it. The probe reports
			// what it managed to measure (Rule 15 — the empty measurement IS
			// the record).
			break
		}
		if !ready {
			continue
		}
		buf := make([]byte, probeReadBytes)
		got, err := t.in.Read(buf)
		if got > 0 {
			held = append(held, buf[:got]...)
			held, out = sortProbeBytes(held, out, t)
			// The evidence seen so far decides whether to keep waiting, and it
			// is re-derived from the answers rather than tracked in a second
			// place: `seen` is the same parser the final measurement uses.
			seen = interpretAnswers(out, CapabilityMeasurement{})
		}
		if err != nil || len(out) >= probeMaxBytes {
			break
		}
	}
	// Whatever is still held when the window closes was never an answer: it is
	// the operator's input, and it is replayed in order with the rest.
	t.addPrefetch(held)
	return out
}

// probeAnswersComplete reports whether EVERY question the probe asked has a
// settled answer on the stream — which is the only condition that says there is
// nothing left to read.
//
// WHY EVERY QUESTION AND NOT ONLY THE ONES A DECISION NEEDS (the defect this
// predicate carries the fix for, measured 2026-10-04 on this branch). The
// predicate used to be `CursorPositionReport && (Colors > 0 || RGBKnown)` — a
// SUBSET completeness test, and a subset is satisfiable while the rest of the
// stream is still in flight. A pty is a byte STREAM with no message framing
// (there is no packet mode on a Linux pty's output side), so the probe's reads
// return whatever replies have LANDED, in any order: the replies for `RGB` and
// the cursor report could satisfy the old predicate while the `colors` reply —
// written by the same responder, still travelling — was never in any read the
// probe made. The probe then exited EARLY and reported a terminal that had
// answered 256 as having no palette at all.
//
// MEASURED, 100 repetitions of TestProbeMeasuresDepthFromRealAnswers' arm with
// both sides instrumented: 31 failures, EVERY ONE an early exit
// (elapsed_ms=0, never a window expiry) with the responder at wrote=2..5 of its
// 5 replies and the captured byte total short of the answer set's 84 (32, 57,
// 59, 60 or 67 bytes). The predicate's dependence on ARRIVAL ORDER is the
// mechanism; the window was never reached, which is why a longer window or a
// settle delay could not have fixed it.
//
// THE DEPTH ARM IS SATISFIED BY EITHER WORD, which is what keeps the shape
// order-free: `colors` answered with a number, or an XTGETTCAP refusal
// (`0 + r <name>`), which the parser records as an answer in its own right —
// and for `RGB` a refusal sets RGBKnown. So a terminal that HAS the capability
// and one that refuses it both settle with no dependence on which reply the
// kernel delivers first.
func probeAnswersComplete(m CapabilityMeasurement) bool {
	if !m.CursorPositionReport || m.DA1 == "" || m.BackgroundColour == "" {
		return false
	}
	return m.depthAnswers >= probeDepthQueries || (m.Colors > 0 && m.RGBKnown)
}

// sortProbeBytes splits the bytes held so far into complete answers (appended
// to out) and the tail still waiting for its next read (returned as held). A
// byte that cannot begin a reply — or that begins one this probe did not ask
// for — is PREFETCHED immediately and in order.
func sortProbeBytes(held, out []byte, t *Terminal) ([]byte, []byte) {
	i := 0
	for i < len(held) {
		if n := probeReplyLen(held[i:]); n > 0 {
			out = append(out, held[i:i+n]...)
			i += n
			continue
		}
		if probeReplyIncomplete(held[i:]) {
			break
		}
		t.addPrefetch(held[i : i+1])
		i++
	}
	rest := append([]byte(nil), held[i:]...)
	if len(rest) > probePartialBytes {
		// Too long to be a reply waiting to complete: it is input. Prefetch it
		// rather than hold the operator's keystrokes hostage.
		t.addPrefetch(rest)
		rest = nil
	}
	return rest, out
}

// pollAnswer waits one slice for input, reporting whether the descriptor has
// something ready. A SIGWINCH is drained here rather than deferred: the probe
// owns the descriptor for the length of its window, and a resize arriving in it
// must not be left undrained (the read loop's own wake would then find the pipe
// full and the editor would resize one keystroke late).
func (t *Terminal) pollAnswer() (bool, error) {
	fds := []unix.PollFd{
		{Fd: int32(t.inputFD), Events: unix.POLLIN},
		{Fd: int32(t.winchPipeReadFD), Events: unix.POLLIN},
	}
	n, err := unix.Poll(fds, probeSliceMS)
	if err != nil {
		if err == unix.EINTR {
			return false, nil
		}
		return false, fmt.Errorf("lineedit: poll during the capability probe: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	if fds[1].Revents&unix.POLLIN != 0 {
		t.drainWinchPipe()
	}
	return fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0, nil
}

// probeReplyLen reports the length of the complete probe reply starting at
// p[0], or 0 when p does not begin with one (complete OR incomplete).
func probeReplyLen(p []byte) int {
	if len(p) < 2 || p[0] != 0x1b {
		return 0
	}
	switch p[1] {
	case sgr.CSIStart:
		return csiReplyLen(p)
	case sgr.OSCIntro, sgr.DCS[1]:
		return stringReplyLen(p)
	}
	return 0
}

// probeReplyIncomplete reports whether p is a PREFIX of a reply the probe asked
// for — the only case in which bytes are held rather than prefetched.
func probeReplyIncomplete(p []byte) bool {
	if len(p) == 1 {
		return p[0] == 0x1b
	}
	if p[0] != 0x1b {
		return false
	}
	switch p[1] {
	case sgr.CSIStart, sgr.OSCIntro, sgr.DCS[1]:
		// A reply introducer with no terminator yet: hold it — one more read
		// may complete it. Anything else (SGR the operator's paste carried,
		// an unknown escape) falls through to "not a reply".
		return probeReplyLen(p) == 0
	}
	return false
}

// csiReplyLen measures a complete CSI reply: introducer, parameters, final byte.
func csiReplyLen(p []byte) int {
	for i := 2; i < len(p); i++ {
		if c := p[i]; c >= sgr.FinalByteMin && c <= sgr.FinalByteMax {
			return i + 1
		}
	}
	return 0
}

// stringReplyLen measures a complete STRING reply (OSC or DCS): it ends at BEL
// or ST, and an unterminated one is not an answer.
func stringReplyLen(p []byte) int {
	for i := 2; i < len(p); i++ {
		if p[i] == oscBEL {
			return i + 1
		}
		if p[i] == sgr.Esc && i+1 < len(p) && p[i+1] == oscSTFinal {
			return i + 2
		}
	}
	return 0
}

// interpretAnswers reads the captured answer bytes into the measurement. It is
// a PURE function of its arguments (no clock, no descriptor, no package state),
// so the parsing rules are testable against captured terminal responses without
// a terminal — and a second probe can never read the first one's numbers.
func interpretAnswers(raw []byte, m CapabilityMeasurement) CapabilityMeasurement {
	m.Bytes = len(raw)
	m.Answered = len(raw) > 0
	st := newProbeParse(&m)
	s := string(raw)
	for len(s) > 0 {
		k := strings.IndexByte(s, 0x1b)
		if k < 0 {
			m.Unrecognized += len(s)
			break
		}
		m.Unrecognized += k
		s = s[k:]
		n, ok := st.apply(s)
		if !ok {
			m.Unrecognized++
			s = s[1:]
			continue
		}
		s = s[n:]
	}
	return m
}

// probeParse is the parser's own state for ONE interpretation: the measurement
// being built, plus the fields a reply's leaf writes (so nothing travels
// through a package variable).
type probeParse struct{ m *CapabilityMeasurement }

// newProbeParse starts an interpretation over m.
func newProbeParse(m *CapabilityMeasurement) *probeParse { return &probeParse{m: m} }

// apply applies ONE reply (starting at s[0], an ESC) to the measurement,
// returning its length in bytes and whether it was a reply this probe asked for.
func (p *probeParse) apply(s string) (int, bool) {
	if len(s) < 2 {
		return 0, false
	}
	switch s[1] {
	case sgr.CSIStart:
		n := csiReplyLen([]byte(s))
		if n <= 0 {
			return 0, false
		}
		return n, p.applyCSI(s[2:n-1], s[n-1])
	case sgr.OSCIntro:
		n := stringReplyLen([]byte(s))
		if n <= 0 {
			return 0, false
		}
		return n, p.applyOSC(stringBody(s[:n]))
	case sgr.DCS[1]:
		n := stringReplyLen([]byte(s))
		if n <= 0 {
			return 0, false
		}
		return n, p.applyDCS(stringBody(s[:n]))
	}
	return 0, false
}

// stringBody strips a complete string reply's introducer (`ESC ]` / `ESC P`)
// and its terminator (BEL or ST), leaving the payload.
func stringBody(s string) string {
	body := s[2:]
	body = strings.TrimSuffix(body, sgr.OSCStringTerminator)
	return strings.TrimSuffix(body, string(rune(oscBEL)))
}

// applyCSI records the two CSI replies the probe asked for: DA1
// (`CSI ? … c`) and the cursor position report (`CSI r ; c R`). It reports
// whether the sequence was one of them — a CSI the probe did not ask for MOVES
// THE CURSOR when the decoder reads it, so it must never be counted as evidence.
func (p *probeParse) applyCSI(body string, final byte) bool {
	switch {
	case final == sgr.FinalDA1 && strings.HasPrefix(body, string(sgr.DA1PrivatePrefix)):
		p.m.DA1 = escapeBytes(sgr.CSI + body + string(final))
		return true
	case final == finalCPR:
		p.m.CursorPositionReport = true
		return true
	default:
		return false
	}
}

// applyOSC records the background-colour answer: OSC 11 with an `rgb:` payload,
// whose presence IS the truecolour signal. Any other OSC is not an answer — a
// title-setting sequence the operator's shell emitted, say — and is reported
// as unrecognised rather than folded in.
func (p *probeParse) applyOSC(body string) bool {
	sel, payload, ok := strings.Cut(body, sgr.OSCParamSeparator)
	if !ok || sel != strconv.Itoa(sgr.OSCBackgroundColour) {
		return false
	}
	if !strings.HasPrefix(payload, sgr.OSCColourPrefix) {
		return false
	}
	p.m.BackgroundColour = payload
	return true
}

// applyDCS records an XTGETTCAP answer. Its body is `1 + r <hex payload>`
// (valid) or `0 + r` (the terminal does not know the name) — and the SECOND is
// the valuable one: it is the terminal SAYING "no such capability", which is a
// measurement, not silence. Both are answers, so both report true.
//
// THE SPACING IS THE OUTSIDE WORLD'S, SO IT IS TOLERATED — measured 2026-10-01:
// xterm's ctlseqs(ms) spells the reply `DCS 1 + r Pt ST`, a real responder wrote
// `1+r…`, and xterm.js writes `1+r…` too. A parser that insisted on one spelling
// silently read the other as UNRECOGNISED — the measurement came back empty and
// the declaration won, which is the exact defect this probe exists to remove
// (the failing arm is kept as a test: TestXTGETTCAPReplySpellingsBothParse).
func (p *probeParse) applyDCS(body string) bool {
	dense := strings.ReplaceAll(body, " ", "")
	valid := strings.HasPrefix(dense, "1+r")
	unknown := strings.HasPrefix(dense, "0+r")
	if !valid && !unknown {
		return false
	}
	// A REFUSAL CARRIES THE NAME IT REFUSES — `0 + r <hex of the name>` — and it
	// is a measurement in its own right: the terminal has SAID "I have no such
	// capability". Recording it as silence would let a terminal that refuses
	// `RGB` look exactly like one that never answered, which is the difference
	// between "16-colour terminal" and "unknown".
	if unknown {
		if name, ok := decodeDCSPayload(strings.TrimPrefix(dense, "0+r")); ok {
			p.refuseCapability(strings.TrimSpace(name))
		}
		return true
	}
	payload, ok := decodeDCSPayload(strings.TrimPrefix(dense, "1+r"))
	if !ok {
		return true
	}
	name, value, ok := strings.Cut(payload, "=")
	if !ok {
		return true
	}
	p.applyCapability(name, value)
	return true
}

// decodeDCSPayload decodes XTGETTCAP's hex payload (two digits per character)
// back into `name=value` text.
func decodeDCSPayload(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	out := make([]byte, 0, len(raw)/sgr.DCSPayloadHexDigits)
	for i := 0; i+sgr.DCSPayloadHexDigits <= len(raw); i += sgr.DCSPayloadHexDigits {
		v, err := strconv.ParseUint(raw[i:i+sgr.DCSPayloadHexDigits], 16, 8)
		if err != nil {
			return "", false
		}
		out = append(out, byte(v))
	}
	return string(out), true
}

// refuseCapability records the terminal's explicit refusal of a capability:
// known-and-absent, which is not the same fact as never-asked.
func (p *probeParse) refuseCapability(name string) {
	switch name {
	case sgr.TerminfoColors:
		// A refusal of `colors` leaves Colors at 0 (unknown), which is what the
		// measurement can support: the terminal did not say how many it has.
		p.m.Colors = 0
		p.m.depthAnswers++
	case sgr.TerminfoRGB:
		p.m.RGBKnown = true
		p.m.RGB = false
		p.m.depthAnswers++
	}
}

// applyCapability folds ONE decoded `name=value` pair into the measurement.
func (p *probeParse) applyCapability(name, value string) {
	switch name {
	case sgr.TerminfoColors:
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			p.m.Colors = n
			p.m.depthAnswers++
		}
	case sgr.TerminfoRGB:
		p.m.RGBKnown = true
		p.m.RGB = value != "" && value != "0"
		p.m.depthAnswers++
	}
}
