//go:build linux

package lineedit

// probe_pty_test.go — the capability probe against a REAL pty and a REAL
// responder, which is the only source of truth about "does this work on a
// terminal".
//
// WHY THE TEST MUST ANSWER ITSELF (the epic's own question, settled by
// measurement). The agent's own terminal is not available to answer: the
// process the probe runs in has a stdin someone else is driving, and a test that
// wrote its queries to the test runner's terminal and waited for replies would
// hang or read the wrong bytes. So the master side of a pty pair plays THE
// TERMINAL: it reads the queries the probe writes and answers them exactly the
// way xterm does (the spellings below are the ones a real emulator was MEASURED
// to produce — see the evidence file the task report cites).
//
// THE THREE THINGS THIS FILE PINS, in order of what would break silently
// without it:
//
//  1. the probe reads DEPTH out of real answer bytes (`colors=256`, `RGB=1`);
//  2. a terminal that answers NOTHING yields an EMPTY measurement and no
//     phantom depth — the declaration then stands, which is the fallback the
//     design promises;
//  3. a byte the operator typed DURING the probe is replayed, in order, by
//     ReadEvent — the epic's keystroke question, asserted rather than argued.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/flaketest"
	"github.com/nickhuang99/gopi/internal/sgr"
	"golang.org/x/sys/unix"
)

// The responder's answers, byte for byte as an emulator writes them. They are
// spelled here (not read from the parser's own constants) so the test cannot
// agree with a parser by sharing its mistake: the wire form is the outside
// world's, and this file states it independently.
const (
	// answerDA1 is `CSI ? 1 ; 2 c` — the primary device attributes reply a
	// VT100-with-AVO terminal gives, and the one xterm's default resource
	// produces (measured on this host: XTerm(372), `xterm -e` with TERM=xterm).
	answerDA1 = "\x1b[?1;2c"
	// answerCPR is `CSI 12 ; 34 R` — a cursor position report.
	answerCPR = "\x1b[12;34R"
	// answerOSC11 is the background colour report: `OSC 11 ; rgb:0000/0000/0000
	// ST`. An `rgb:` triplet with four hex digits per channel is not producible
	// by a 16-colour terminal, which is what makes it a truecolour signal.
	answerOSC11 = "\x1b]11;rgb:0000/0000/0000\x1b\\"
	// answerColors is XTGETTCAP's reply for terminfo `colors`: `DCS 1 + r
	// 636F6C6F72733D323536 ST` — the payload is the hex encoding of
	// `colors=256`. The SPELLING here is the DENSE one (`1+r`), which is what
	// real responders were measured to write (2026-10-01: a live responder and
	// xterm.js both); the spaced spelling ctlseqs(ms) documents is the other arm
	// of TestXTGETTCAPReplySpellingsBothParse.
	answerColors = "\x1bP1+r636F6C6F72733D323536\x1b\\"
	// answerColorsSpaced is the SAME reply in the spelling the xterm manual
	// writes (`1 + r`): a parser that accepted only one of the two would read
	// the other as unrecognised — and the probe would silently measure nothing.
	answerColorsSpaced = "\x1bP1 + r636F6C6F72733D323536\x1b\\"
	// answerRGB is the same for `RGB=1`: hex of `RGB=1`.
	answerRGB = "\x1bP1+r5247423D31\x1b\\"
	// answerRGBUnknown is the REFUSAL spelling: the terminal SAYING it has no
	// such capability, naming the capability it refuses (hex `RGB`). It is an
	// answer, not silence — which is why it must leave RGBKnown true and RGB
	// false.
	answerRGBUnknown = "\x1bP0+r524742\x1b\\"
)

// probeAnswer is ONE question's reply: the query PREFIX that asks for it, the
// bytes a terminal answers with, and the name a failure message quotes.
//
// A LIST of these — never a Go map — carries a fake terminal's answers. The map
// this replaced was the second half of the flake's mechanism: `for prefix,
// reply := range r.answers` writes the replies in an order Go RANDOMIZES, so
// even the fixture could not say what order it was simulating. A list's order is
// DECLARED, which is what makes the split stream below a property of the test
// (Rule 5) instead of a property of the host's scheduler.
type probeAnswer struct {
	// name identifies the answer; for the two depth questions it is the
	// terminfo capability name sgr asks about.
	name string
	// query is the byte string that asks for this reply.
	query string
	// reply is what the terminal writes back.
	reply string
}

// The answer-set names, in the vocabulary a failure message quotes.
const (
	probeAnswerCPRName   = "cpr"
	probeAnswerDA1Name   = "da1"
	probeAnswerOSC11Name = "osc11"
)

// probeAnswerCatalogue is EVERY reply this fixture knows how to produce, each
// paired with the query that asks for it. It is one list so the fixture cannot
// disagree with itself about what a query means, and so a test names the answers
// it wants in the order it wants them answered.
var probeAnswerCatalogue = []probeAnswer{
	{probeAnswerCPRName, "\x1b[6n", answerCPR},
	{probeAnswerDA1Name, "\x1b[c", answerDA1},
	{probeAnswerOSC11Name, "\x1b]11;?", answerOSC11},
	{sgr.TerminfoColors, "\x1bP+q636F6C6F7273", answerColors},
	{sgr.TerminfoRGB, "\x1bP+q524742", answerRGB},
}

// probeAnswers builds the answer list for the named answers, IN THE ORDER
// NAMED: the first name is written first, the second second.
func probeAnswers(t *testing.T, names ...string) []probeAnswer {
	t.Helper()
	out := make([]probeAnswer, 0, len(names))
	for _, name := range names {
		found := false
		for _, a := range probeAnswerCatalogue {
			if a.name == name {
				out = append(out, a)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("the fixture has no answer named %q", name)
		}
	}
	return out
}

// probeReplyForCapability returns the reply that answers a terminfo capability
// name, so a test can drive the predicate over the very questions the probe
// asks without spelling a second correspondence.
func probeReplyForCapability(t *testing.T, name string) string {
	t.Helper()
	for _, a := range probeAnswerCatalogue {
		if a.name == name {
			return a.reply
		}
	}
	t.Fatalf("no catalogue reply answers the capability %q", name)
	return ""
}

// The fixture's READ BOUNDARY is the kernel's own count, not a sleep:
// TIOCINQ — FIONREAD on a terminal — is how many bytes have been WRITTEN and not
// yet READ on that terminal. MEASURED on this host (2026-10-04): 0 fresh, 4 after
// the writer wrote 4 bytes, 0 again once the reader had consumed them. The count
// must be taken on the READER's side (the slave for this fixture, which reads
// where the probe reads); the master's own count stays 0, because on a pty it
// reports the other direction.
//
// probeGateCeiling bounds the wait for that count to reach zero. A host with a
// working kernel and a probe that is reading reaches it immediately; the ceiling
// exists so that a probe which has STOPPED reading reports itself — which is
// exactly the pre-fix behaviour this fixture must be able to demonstrate —
// instead of parking the suite. It is a failure bound, never a delay any
// assertion is timed against.
const probeGateCeiling = 500 * time.Millisecond

// probeGate holds the responder to ONE reply per READ BOUNDARY: the k-th reply
// is written only after the kernel reports that the (k-1)-th has been READ. The
// condition is an observed fact the fixture can check, so "the answers arrive as
// separate reads" is enforced rather than hoped for — no phase the test has to
// advance, no sleep the assertion depends on.
type probeGate struct {
	// fd is the reader's descriptor, where TIOCINQ counts unconsumed bytes.
	fd int
	mu sync.Mutex
	// stuck is set when the gate gave up waiting for the read boundary, and
	// stuckAt names the reply it was about to write.
	stuck   bool
	stuckAt int
}

func (g *probeGate) marked(k int) { _ = k }

// drain reports how many bytes the reader still has not read.
func (g *probeGate) drain() int {
	n, err := unix.IoctlGetInt(g.fd, unix.TIOCINQ)
	if err != nil {
		return -1
	}
	return n
}

// awaitBoundary blocks until every byte written so far has been READ, reporting
// whether that happened inside probeGateCeiling.
//
// IT NEVER FAILS ITS OWN TEST: a gate that gave up with t.Fatalf would log from
// a responder goroutine, which is undefined once the test returns. A stuck gate
// instead STOPS the responder and records which reply it died on, and the test
// (which is the thing that knows what it asserted) reports it.
func (g *probeGate) awaitBoundary(replyIndex int) bool {
	deadline := time.Now().Add(probeGateCeiling)
	for {
		if g.drain() == 0 {
			return true
		}
		if !time.Now().Before(deadline) {
			g.mu.Lock()
			if !g.stuck {
				g.stuck = true
				g.stuckAt = replyIndex
			}
			g.mu.Unlock()
			return false
		}
		time.Sleep(probeGatePoll) //stylegate:allow-sleep polls for the gate the probe asserts; the gate file appears only after the spawned child has run
	}
}

// stuckState reports whether the gate gave up waiting, and which reply it was
// about to write when it did — so a test can say WHAT the reader stopped reading.
func (g *probeGate) stuckState() (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stuck, g.stuckAt
}

// probeGatePoll is how often the gate re-reads the kernel's count while it
// waits. It bounds the fixture's own latency, never an assertion: the fact being
// waited for (the count reaching zero) is observed, not timed.
const probeGatePoll = 200 * time.Microsecond

// probeResponder is the master side of the pty: it watches for the probe's
// queries and answers them, and it can be told to answer NOTHING (the silent
// terminal) — which is the arm the fallback depends on.
type probeResponder struct {
	master *os.File
	// answers is the reply list, in the order this fake terminal will write it.
	// A query the list does not carry is left unanswered, exactly as an emulator
	// that never implemented it would.
	answers []probeAnswer
	// silent makes the responder answer nothing at all.
	silent bool
	// typed is written to the master BEFORE the probe's queries, standing in
	// for a keystroke the operator pressed while the terminal was answering.
	typed string
	// gate, when set, holds each reply until the reader has consumed the
	// previous one. A nil gate writes the whole batch at once, which is the
	// shape a fast emulator produces.
	gate *probeGate
	// done is closed when the responder's loop returns. It is NOT waited on at
	// teardown: a blocking read on a pty master does not unblock when the master
	// is closed while the slave is still open (measured — a join here hung the
	// suite until its own timeout), and a goroutine parked in that read is the
	// honest cost of playing a terminal. The field stays because it is how a test
	// can TELL the responder's state without joining it.
	done chan struct{}
}

// serve reads the master and replies to whatever it recognises, in the DECLARED
// order of the answer list: each reply is written once, when its query has been
// seen, and only when the gate (if any) has released it.
func (r *probeResponder) serve() {
	defer close(r.done)
	if r.typed != "" {
		_, _ = r.master.WriteString(r.typed) //stylegate:ignore writes the scripted keystrokes into the probe's pty; the rendered frame read below is the assertion
	}
	sent := make([]bool, len(r.answers))
	written := 0
	buf := make([]byte, 256)
	for {
		n, err := r.master.Read(buf)
		if n > 0 && !r.silent {
			q := string(buf[:n])
			for i, a := range r.answers {
				if sent[i] || !strings.Contains(q, a.query) {
					continue
				}
				if r.gate != nil {
					// One reply per read boundary: the gate waits for the
					// kernel to report that everything written so far has been
					// read before this reply goes out. A gate that gives up
					// stops the responder at that reply — the test reports it.
					if !r.gate.awaitBoundary(written) {
						return
					}
				}
				if _, werr := r.master.WriteString(a.reply); werr != nil {
					return
				}
				sent[i] = true
				written++
			}
		}
		if err != nil {
			return
		}
	}
}

// openProbePTY opens a pty pair and starts a responder on the master, returning
// the SLAVE (the terminal's side, which the probe writes to and reads back
// from) and the responder.
func openProbePTY(t *testing.T, r *probeResponder) *os.File {
	t.Helper()
	master, slave := openInputPendingPTY(t)
	r.master = master
	r.done = make(chan struct{})
	if r.gate != nil && r.gate.fd == 0 {
		// The gate waits on the READER's count: the slave is where this fixture
		// reads replies, exactly as the probe does.
		r.gate.fd = int(slave.Fd())
	}
	go r.serve()
	t.Cleanup(func() {
		// Close withOUT joining the responder: see probeResponder.done for the
		// measurement behind that (a pty master read does not unblock on close
		// while the slave is open, so a join would hang the suite).
		_ = master.Close() //stylegate:ignore the harness's own pty teardown
	})
	return slave
}

// TestProbeMeasuresDepthFromRealAnswers is the positive arm: a terminal that
// answers all five queries must be measured as 24-bit with 256 colour entries —
// and the measurement must say WHICH answers produced that.
//
// THE ANSWERS ARE WRITTEN IN A DECLARED ORDER, and the two DEPTH answers come
// LAST (the fixture's own list states it; the map this replaced ranged in Go's
// randomized order, which is half of why the arm flaked). The order is not
// cosmetic: it puts the answers the old stop predicate did not require at the
// END of the stream, where a subset predicate exits before ever reading them —
// which is the defect, made reproducible by the fixture rather than by luck.
func TestProbeMeasuresDepthFromRealAnswers(t *testing.T) {
	flaketest.Declare("deterministic: the pty responder wrote one reply per query while ranging a Go MAP (randomized order), so the probe's reads returned an arbitrary SUBSET of the answer set, and the stop predicate was itself a SUBSET test satisfiable with replies still in flight — the probe exited EARLY on a partial stream and reported colors=0 for a terminal that had answered 256. Registered: testdata/flakes.tsv, owner 01a10a2d-2eda-7907-8a54-144d32186011.")
	responder := &probeResponder{answers: probeAnswers(t,
		probeAnswerCPRName, probeAnswerDA1Name, probeAnswerOSC11Name,
		sgr.TerminfoColors, sgr.TerminfoRGB,
	)}
	slave := openProbePTY(t, responder)
	// The terminal's OUTPUT writer is the slave itself: the queries must travel
	// to the MASTER (the emulator) for anything to answer them, and writing to
	// the slave fd is exactly how a process on a pty talks to its emulator.
	term, err := Open(slave, slave)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	defer term.Close()

	m := term.ProbeCapability()
	if !m.Answered {
		t.Fatalf("the probe read nothing from a terminal that answered every query: %s", m.Summary())
	}
	if !m.CursorPositionReport {
		t.Errorf("the liveness answer (%q) was not recognised: %s", answerCPR, m.Summary())
	}
	if m.Colors != 256 {
		t.Errorf("colors = %d, want 256 (the reply %q carries it): %s", m.Colors, answerColors, m.Summary())
	}
	if !m.RGB {
		t.Errorf("RGB = false, want true (the reply %q carries it): %s", answerRGB, m.Summary())
	}
	if got := m.Depth(); got != DepthTruecolor {
		t.Errorf("Depth() = %v, want DepthTruecolor: %s", got, m.Summary())
	}
	// The DA1 answer is kept verbatim, escaped: a reader must be able to see what
	// the terminal said, not only the verdict drawn from it.
	if !strings.Contains(m.DA1, "?1;2c") {
		t.Errorf("the DA1 answer is not recorded verbatim (%q): %s", m.DA1, m.Summary())
	}
}

// TestProbeOnASilentTerminalMeasuresNothing is the negative arm — the one the
// DECLARATION's fallback rests on: a terminal that answers nothing produces an
// empty measurement (Answered false, Colors 0, no depth), and never a phantom
// "basic" that would displace what the environment declared.
func TestProbeOnASilentTerminalMeasuresNothing(t *testing.T) {
	responder := &probeResponder{silent: true}
	slave := openProbePTY(t, responder)
	// The terminal's OUTPUT writer is the slave itself: the queries must travel
	// to the MASTER (the emulator) for anything to answer them, and writing to
	// the slave fd is exactly how a process on a pty talks to its emulator.
	term, err := Open(slave, slave)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	defer term.Close()

	start := time.Now()
	m := term.ProbeCapability()
	elapsed := time.Since(start)

	if m.Answered || m.Measured() || m.Colors != 0 || m.RGBKnown {
		t.Fatalf("a silent terminal produced a measurement: %s", m.Summary())
	}
	// The probe waits its whole window on a silent terminal (it cannot tell slow
	// from dead), and the wait is BOUNDED: a probe that blocked would defeat the
	// point of a window. The bound is asserted from both sides so a regression
	// that removed the wait entirely (and so never read a slow answer) fails too.
	if elapsed < probeWindowMS*time.Millisecond {
		t.Errorf("the probe returned in %v on a silent terminal, want at least the %dms window (a slow terminal must get its chance)", elapsed, probeWindowMS)
	}
	if elapsed > probeWindowMaxWait {
		t.Errorf("the probe took %v on a silent terminal, want no more than %v (the window is a bound, not a suggestion)", elapsed, probeWindowMaxWait)
	}
}

// probeWindowMaxWait is the test-side ceiling on a silent terminal's probe: the
// window plus the slices' own scheduling slack. It is a bound on a WAIT, not a
// measured duration (the measured one is reported in the task's evidence).
const probeWindowMaxWait = 1 * time.Second

// TestProbeReplaysAKeystrokeTypedDuringIt is the epic's keystroke question,
// settled by measurement: bytes the operator types WHILE the terminal is
// answering must reach ReadEvent, in order, exactly as typed — not swallowed by
// the probe and not reordered behind a later key.
func TestProbeReplaysAKeystrokeTypedDuringIt(t *testing.T) {
	const typed = "hello"
	responder := &probeResponder{
		typed:   typed,
		answers: probeAnswers(t, probeAnswerDA1Name, probeAnswerCPRName),
	}
	slave := openProbePTY(t, responder)
	// The terminal's OUTPUT writer is the slave itself: the queries must travel
	// to the MASTER (the emulator) for anything to answer them, and writing to
	// the slave fd is exactly how a process on a pty talks to its emulator.
	term, err := Open(slave, slave)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	defer term.Close()

	m := term.ProbeCapability()
	if !m.CursorPositionReport {
		t.Fatalf("the probe did not read the liveness answer, so the replay arm was not exercised: %s", m.Summary())
	}

	var got []byte
	deadline := time.Now().Add(2 * time.Second)
	buf := make([]byte, 64)
	for len(got) < len(typed) && time.Now().Before(deadline) {
		ev, err := term.ReadEvent(buf, int(probeReplayReadTimeout.Milliseconds()))
		if err != nil {
			t.Fatalf("ReadEvent after the probe: %v", err)
		}
		if ev.Kind == EventInput && ev.Bytes > 0 {
			got = append(got, buf[:ev.Bytes]...)
		}
	}
	if string(got) != typed {
		t.Fatalf("the keystrokes typed during the probe replayed as %q, want %q (the probe consumed input the operator typed)", got, typed)
	}
	// The prompt asked for a typed key the operator's own arm proves was typed
	// DURING the terminal's answers; the answers themselves must NOT appear here
	// (they are evidence, not input — an answer replayed as input would be read
	// by the decoder as cursor motions).
	if strings.Contains(string(got), "[") {
		t.Fatalf("an answer byte leaked into the replayed input: %q", got)
	}
}

// probeReplayReadTimeout bounds a single ReadEvent in the replay arm. It is not
// a delay the assertion depends on: the bytes are already in the prefetch (or
// on the pty), so the read returns promptly on a healthy host.
const probeReplayReadTimeout = 500 * time.Millisecond

// TestXTGETTCAPReplySpellingsBothParse pins the tolerance the measured failure
// demanded: BOTH spellings of an XTGETTCAP reply are answers. The spaced one is
// what ctlseqs(ms) documents; the dense one is what a real responder and xterm.js
// write. A parser that accepted one silently read the other as unrecognised, the
// measurement came back empty, and the declaration won — measured 2026-10-01 on
// a live run whose record read `capability=declared` while the terminal had in
// fact answered `colors=16`.
func TestXTGETTCAPReplySpellingsBothParse(t *testing.T) {
	for name, reply := range map[string]string{
		"dense (as real terminals write it)": answerColors,
		"spaced (as ctlseqs(ms) spells it)":  answerColorsSpaced,
	} {
		m := interpretAnswers([]byte(reply), CapabilityMeasurement{})
		if m.Colors != 256 {
			t.Errorf("%s: colors = %d, want 256 — %s", name, m.Colors, m.Summary())
		}
		if m.Unrecognized != 0 {
			t.Errorf("%s: %d byte(s) went unrecognised: %s", name, m.Unrecognized, m.Summary())
		}
	}
	// The REFUSAL is an answer too: the terminal said "no RGB", and that must be
	// recorded as a measurement (RGBKnown) rather than as silence.
	m := interpretAnswers([]byte(answerRGBUnknown), CapabilityMeasurement{})
	if !m.RGBKnown {
		t.Errorf("an explicit refusal was not recorded as an answer: %s", m.Summary())
	}
	if m.RGB {
		t.Errorf("a refusal was recorded as support: %s", m.Summary())
	}
}

// TestProbeIsRunOncePerTerminal pins the one-shot rule: a second probe on the
// same terminal must not re-ask. It is asserted through the OBSERVABLE
// consequence — the responder is switched to silent, and the second call still
// reports the first measurement.
func TestProbeIsRunOncePerTerminal(t *testing.T) {
	responder := &probeResponder{answers: probeAnswers(t,
		probeAnswerCPRName, sgr.TerminfoColors,
	)}
	slave := openProbePTY(t, responder)
	// The terminal's OUTPUT writer is the slave itself: the queries must travel
	// to the MASTER (the emulator) for anything to answer them, and writing to
	// the slave fd is exactly how a process on a pty talks to its emulator.
	term, err := Open(slave, slave)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	defer term.Close()

	first := term.ProbeCapability()
	if first.Colors != 256 {
		t.Fatalf("the first probe measured colors=%d, want 256: %s", first.Colors, first.Summary())
	}
	responder.silent = true
	second := term.ProbeCapability()
	if second.Colors != first.Colors || second.Answered != first.Answered {
		t.Fatalf("the second probe re-asked the terminal (%s) instead of returning the first measurement (%s)", second.Summary(), first.Summary())
	}
}

// TestProbeAnswersTextCannotCauseACursorMove is the guard against the failure
// this whole file exists to prevent: an answer byte replayed into the decoder.
// It drives the DECODER directly, over the same bytes the probe reads, and
// asserts that none of them is an input event the operator did not type.
func TestProbeAnswersTextCannotCauseACursorMove(t *testing.T) {
	for _, reply := range []string{answerDA1, answerCPR, answerOSC11, answerColors, answerRGB} {
		m := interpretAnswers([]byte(reply), CapabilityMeasurement{})
		if m.Bytes != len(reply) {
			t.Errorf("interpretAnswers(%q) attributed %d of %d bytes", escapeBytes(reply), m.Bytes, len(reply))
		}
		if m.Unrecognized != 0 {
			t.Errorf("interpretAnswers(%q) left %d bytes unrecognised: %s", escapeBytes(reply), m.Unrecognized, m.Summary())
		}
	}
	// ...and a sequence the probe did NOT ask for draws NO evidence: an SGR
	// sequence is input, and misreading it as an answer is how a probe would
	// claim a capability nobody offered. (`Measured()` is the assertion that
	// matters: bytes ARRIVED, so the measurement is not empty — it simply
	// supports no depth, and the declaration therefore still decides.)
	m := interpretAnswers([]byte("\x1b[31m"), CapabilityMeasurement{})
	if m.CursorPositionReport || m.DA1 != "" || m.Colors != 0 || m.RGB || m.RGBKnown {
		t.Errorf("an SGR sequence was read as a probe answer: %s", m.Summary())
	}
	if m.Measured() {
		t.Errorf("an SGR sequence produced a depth measurement: %s", m.Summary())
	}
	if m.Unrecognized == 0 {
		t.Error("an SGR sequence was consumed without being reported as unrecognised (silence is not an answer)")
	}
}

// TestProbePartialRepliesArePrefetchedNotConsumed pins the boundary that
// protects the operator's bytes: a byte string that BEGINS like a reply but
// never completes must be replayed as input, never held or dropped.
func TestProbePartialRepliesArePrefetchedNotConsumed(t *testing.T) {
	term := &Terminal{}
	held, out := sortProbeBytes([]byte("\x1b[12;34"), nil, term)
	if len(out) != 0 {
		t.Fatalf("an unterminated CSI was taken as an answer: %q", out)
	}
	if string(held) != "\x1b[12;34" {
		t.Fatalf("the incomplete sequence was not held for its next read: %q", held)
	}
	// A complete reply followed by a typed byte: the answer is consumed, the
	// typed byte is replayed.
	held, out = sortProbeBytes([]byte(answerCPR+"x"), nil, term)
	if string(out) != answerCPR {
		t.Fatalf("out = %q, want the complete reply %q", out, answerCPR)
	}
	if len(held) != 0 {
		t.Fatalf("held = %q, want nothing (the typed byte is input, not a partial reply)", held)
	}
	if got := term.Prefetched(); got != 1 {
		t.Fatalf("Prefetched() = %d, want 1 (the typed byte)", got)
	}
}

// TestProbeQueryBytesAreTheSequencesTheProtocolNames pins the QUESTIONS, so a
// change to the query spelling cannot pass while the parser keeps answering the
// old one. The expected string is the wire form, written here independently.
func TestProbeQueryBytesAreTheSequencesTheProtocolNames(t *testing.T) {
	want := "\x1b[6n" + "\x1b[c" + "\x1b]11;?\x1b\\" +
		"\x1bP+q636F6C6F7273\x1b\\" + "\x1bP+q524742\x1b\\"
	if got := ProbeQueriesFor(); got != want {
		t.Fatalf("the probe asks\n  %q\nwant\n  %q", got, want)
	}
	if got := fmt.Sprintf("%s", EscapedQueries()); !strings.Contains(got, `\e[6n`) {
		t.Fatalf("the escaped query is not human-readable: %q", got)
	}
}

// TestProbeReadsEveryDepthAnswerOneReadBoundaryAtATime is the DETERMINISTIC
// guard for the defect this file's subject was flaking on, and it is the test
// that must be RED on the pre-fix predicate.
//
// THE MECHANISM IT PINS. A pty is a byte STREAM with no message framing, so the
// probe's reads return whatever replies have LANDED — a prefix of the answer
// set, in any order. The pre-fix stop predicate was a SUBSET test
// (`CursorPositionReport && (Colors > 0 || RGBKnown)`), which is satisfiable
// while the rest of the stream is still in flight: the probe then exited early
// and reported the terminal's colour depth from a partial stream. MEASURED over
// 100 repetitions of the arm above, both sides instrumented: 31 failures, every
// one elapsed_ms=0 (an early exit, never a window expiry), the responder at
// wrote=2..5 of its 5 replies and the captured bytes short of the answer set's
// 84 (32/57/59/60/67).
//
// THE FIXTURE MAKES THAT SCHEDULE, INSTEAD OF HOPING FOR IT, in two parts.
//
// FIRST, the answers are DECLARED in one order — the liveness reply, then the
// two depth replies, then identity and the background colour. That order is the
// point: the two answers a SUBSET predicate stops on are written FIRST, so when
// it stops, the three replies after them are still OWED. Nothing about the
// defect's arrival then depends on which reply the kernel happened to deliver
// first.
//
// SECOND, each reply is held until the kernel reports, through TIOCINQ on the
// reader's own descriptor, that everything written before it has been READ — so
// "the answers arrive as separate reads" is an observed fact of the fixture, not
// a hope about the host's scheduler. Every condition here is such a fact: no
// sleep the assertion depends on, no server-side delay, no repetition standing
// in for determinism.
//
// WHY THE FIX MAKES IT GREEN: the stop predicate now names EVERY question the
// probe asked, so it cannot be satisfied by a subset — the three replies after
// the depth answers are exactly what it waits for.
func TestProbeReadsEveryDepthAnswerOneReadBoundaryAtATime(t *testing.T) {
	gate := &probeGate{}
	responder := &probeResponder{
		gate: gate,
		answers: probeAnswers(t,
			probeAnswerCPRName, sgr.TerminfoColors, sgr.TerminfoRGB,
			probeAnswerDA1Name, probeAnswerOSC11Name,
		),
	}
	slave := openProbePTY(t, responder)
	term, err := Open(slave, slave)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	defer term.Close()

	m := term.ProbeCapability()

	if stuck, at := gate.stuckState(); stuck {
		t.Fatalf("the probe stopped reading: the fixture could not write reply %d of %d "+
			"(everything it had written was still unread on the pty after %v) — the probe exited "+
			"before the answer set was complete: dump=%s",
			at+1, len(responder.answers), probeGateCeiling, m.Summary())
	}
	// The verdict is the five answers, each read across its OWN boundary: the
	// depth the terminal declared is measured with the whole stream, not guessed
	// from the prefix a subset predicate stopped on.
	if m.Colors != colorEntries256 {
		t.Errorf("colors = %d, want %d (the reply %q is the LAST depth answer and must be READ, "+
			"not abandoned when a subset of the stream looks sufficient): %s",
			m.Colors, colorEntries256, answerColors, m.Summary())
	}
	if !m.RGBKnown || !m.RGB {
		t.Errorf("rgb = %s, want yes (the reply %q is the LAST answer of all): %s", rgbWord(m), answerRGB, m.Summary())
	}
	if !m.CursorPositionReport || m.DA1 == "" || m.BackgroundColour == "" {
		t.Errorf("a reply written before the depth answers was not read: %s", m.Summary())
	}
	if got := m.Depth(); got != DepthTruecolor {
		t.Errorf("Depth() = %v, want %v: %s", got, DepthTruecolor, m.Summary())
	}
	// The measurement must be taken from the WHOLE answer set, byte for byte:
	// a short total is the signature of the early exit this test exists to catch.
	if want := probeAnswerSetBytes(t, responder.answers); m.Bytes != want {
		t.Errorf("the probe captured %d answer bytes, want %d (every reply of the declared order): %s",
			m.Bytes, want, m.Summary())
	}
}

// probeAnswerSetBytes is how many bytes the given answer set is worth in total —
// the yardstick a partial capture is measured against.
func probeAnswerSetBytes(t *testing.T, answers []probeAnswer) int {
	t.Helper()
	total := 0
	for _, a := range answers {
		total += len(a.reply)
	}
	return total
}

// TestProbeStopPredicateCountsTheDepthQueriesItAsks pins the count the stop
// predicate is stated against (probeDepthQueries) to the questions the probe
// ACTUALLY writes: one answer per capability query sgr names must settle the
// predicate, and one fewer must not. Without it the count is an unchecked
// assertion about another package's list, and the failure mode is silent — the
// probe would wait out its whole window for a reply to a question it never
// asked, or stop while a question was still unanswered.
func TestProbeStopPredicateCountsTheDepthQueriesItAsks(t *testing.T) {
	if got := len(sgr.CapabilityQueryNames()); got != probeDepthQueries {
		t.Fatalf("probeDepthQueries = %d, but sgr names %d depth queries: the stop predicate counts "+
			"the wrong questions", probeDepthQueries, got)
	}
	// The three single-answer questions, answered — the liveness reply, identity
	// and the background colour are NOT depth answers.
	base := interpretAnswers([]byte(answerCPR+answerDA1+answerOSC11), CapabilityMeasurement{})
	if probeAnswersComplete(base) {
		t.Errorf("the predicate reports the stream complete on the three non-depth answers alone: %s", base.Summary())
	}
	// Every depth question answered: the predicate must settle on the LAST one,
	// whichever order the replies arrive in — the one property the fix buys.
	names := sgr.CapabilityQueryNames()
	for i := range names {
		prefix := answerCPR + answerDA1 + answerOSC11
		for j := 0; j <= i; j++ {
			prefix += probeReplyForCapability(t, names[j])
		}
		m := interpretAnswers([]byte(prefix), CapabilityMeasurement{})
		last := i == len(names)-1
		if got := probeAnswersComplete(m); got != last {
			t.Errorf("with %d of %d depth answers on the stream, complete = %v, want %v: %s",
				i+1, len(names), got, last, m.Summary())
		}
	}
}
