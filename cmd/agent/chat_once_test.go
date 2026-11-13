package main

// chat_once_test.go — the unit surface of `gopi chat --once`
// (cmd/agent/chat_once.go). The END-TO-END cases (the real binary driving a
// real turn) live in testsuite/chat_once_test.go; what is pinned here is the
// part a subprocess cannot show: the argv vocabulary, the prompt-source
// precedence, and the help contract a caller reads.

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestChatOnceRequested pins the DISPATCH predicate: which argv tails select the
// one-shot face. It is the first decision `gopi chat` makes, and getting it
// wrong sends a scripted prompt into the interactive REPL (where it would wait
// on a terminal that will never answer).
func TestChatOnceRequested(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--once"}, true},
		{[]string{"-p"}, true},
		{[]string{"--once", "--tools"}, true},
		{[]string{"say OK", "--once"}, true},
		{[]string{"--prompt", "say OK"}, true},
		{[]string{"--prompt=say OK"}, true},
		// The interactive shapes must NOT be captured: a seed path, --tools
		// alone, no arguments at all.
		{[]string{}, false},
		{[]string{"--tools"}, false},
		{[]string{"~/.gopi/raw/gopi-raw-x.log"}, false},
		{[]string{"list"}, false},
		{[]string{"--no-editor"}, false},
	}
	for _, c := range cases {
		if got := isChatOnceRequested(c.args); got != c.want {
			t.Errorf("isChatOnceRequested(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestChatOnceParseArgs pins the prompt precedence and the argv split: the
// one-shot words are consumed here, everything else must reach the REPL
// UNCHANGED (the REPL's own parser owns --tools/--no-editor/--dump-screen and a
// session seed, and a flag dropped here is a flag the user set in vain).
func TestChatOnceParseArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantPrompt string
		wantRest   []string
	}{
		{"short flag before the prompt", []string{"-p", "say OK"}, "say OK", nil},
		{"short flag after the prompt", []string{"say OK", "-p"}, "say OK", nil},
		{"bare --once with a positional", []string{"--once", "say OK"}, "say OK", nil},
		{"explicit prompt flag", []string{"--once", "--prompt", "say OK"}, "say OK", nil},
		{"explicit prompt flag joined", []string{"--once", "--prompt=say OK"}, "say OK", nil},
		{"tools rides through to the REPL", []string{"--once", "say OK", "--tools"}, "say OK", []string{"--tools"}},
		{"a seed rides through to the REPL", []string{"--once", "--no-editor"}, "", []string{"--no-editor"}},
		{"flag order is free", []string{"--tools", "--once", "say OK"}, "say OK", []string{"--tools"}},
		{"help rides through", []string{"--once", "-h"}, "", []string{"-h"}},
		{"no prompt at all", []string{"--once"}, "", nil},
	}
	for _, c := range cases {
		got, err := chatOnceParseArgs(c.args)
		if err != nil {
			t.Errorf("%s: chatOnceParseArgs(%v) error: %v", c.name, c.args, err)
			continue
		}
		if got.prompt != c.wantPrompt {
			t.Errorf("%s: prompt = %q, want %q", c.name, got.prompt, c.wantPrompt)
		}
		if strings.Join(got.rest, " ") != strings.Join(c.wantRest, " ") {
			t.Errorf("%s: rest = %v, want %v", c.name, got.rest, c.wantRest)
		}
	}
}

// TestChatOnceParseArgs_RefusesTwoPrompts: `-p a b` must NOT be read as "prompt
// a, resume b". The REPL's second positional is a session SEED, so a
// reinterpretation would silently answer a different question than the caller
// asked — measured on the first live run of this face, where `-p a -p b` leaked
// "b" into the seed slot and reported an unrelated "resume target" error.
func TestChatOnceParseArgs_RefusesTwoPrompts(t *testing.T) {
	for _, args := range [][]string{
		{"-p", "a", "-p", "b"},
		{"--prompt", "a", "b"},
		{"a", "b"},
	} {
		got, err := chatOnceParseArgs(args)
		if err == nil {
			t.Errorf("chatOnceParseArgs(%v) = %+v, want an error (two prompts given)", args, got)
			continue
		}
		if !strings.Contains(err.Error(), "more than one prompt") {
			t.Errorf("chatOnceParseArgs(%v) error = %q, want it to name the two prompts", args, err)
		}
	}
}

// TestChatOnceParseArgs_PromptFlagNeedsAValue: a trailing --prompt with nothing
// after it is a usage error, never a silent empty prompt.
func TestChatOnceParseArgs_PromptFlagNeedsAValue(t *testing.T) {
	if _, err := chatOnceParseArgs([]string{"--prompt"}); err == nil {
		t.Error("chatOnceParseArgs([--prompt]) = nil error, want a usage error")
	}
}

// TestChatOnceOneLine pins the multi-line fold: a chat message is ONE line on
// this carrier, so a multi-line prompt is joined rather than truncated at its
// first break (dropping two thirds of a prompt silently is the defect this
// avoids).
func TestChatOnceOneLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"one line", "one line"},
		{"first\nsecond", "first second"},
		{"first\r\nsecond\r\n", "first second"},
		{"  padded  \n\n  lines  ", "padded lines"},
		{"", ""},
		{"\n\n\n", ""},
	}
	for _, c := range cases {
		if got := chatOnceOneLine(c.in); got != c.want {
			t.Errorf("chatOnceOneLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestChatOnceReadPrompt_FromAPipe is the `echo '…' | gopi chat --once` shape on
// the unit level: the lines are drained and joined, blank lines dropped.
func TestChatOnceReadPrompt_FromAPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		_, _ = w.WriteString("bootstrap starter prompt\n\nto push the flywheel\n") //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
		_ = w.Close()                                                              //stylegate:ignore closes the fixture write end
	}()
	got, err := chatOnceReadPrompt(r)
	_ = r.Close() //stylegate:ignore closes the fixture read end
	if err != nil {
		t.Fatalf("chatOnceReadPrompt: %v", err)
	}
	if want := "bootstrap starter prompt\nto push the flywheel"; got != want {
		t.Errorf("chatOnceReadPrompt = %q, want %q", got, want)
	}
}

// TestChatOnceUsageDocumentsTheContract is the help contract: the page a caller
// reads must name both flag spellings, all three prompt sources and all three
// exit statuses. It fails if the one-shot section is dropped from chatUsage —
// which is the RULE 31 hazard this face has: a flag that exists in the parser
// and nowhere in the help is a flag nobody can find.
func TestChatOnceUsageDocumentsTheContract(t *testing.T) {
	wants := []string{
		chatOnceFlag,
		chatOnceShortFlag,
		chatOncePromptFlag,
		"stdin",
		"1 = the turn failed",
		"2 = usage error",
		"never a silent success",
	}
	for _, w := range wants {
		if !strings.Contains(chatUsage, w) {
			t.Errorf("chatUsage does not document %q — `gopi chat --once` would be an undocumented surface", w)
		}
	}
	// The invocation the operator asked for, spelled out in the help.
	if !strings.Contains(chatUsage, "gopi chat --once --tools") {
		t.Error("chatUsage does not carry the piped one-shot example")
	}
}

// TestChatOnceUsageIsOnePage: the one-shot section is part of the chat page,
// not a second page a reader must know to ask for.
func TestChatOnceUsageIsOnePage(t *testing.T) {
	if !strings.Contains(chatUsage, chatOnceUsage) {
		t.Error("chatOnceUsage is not joined into chatUsage — the one-shot contract would be invisible to `gopi chat -h`")
	}
	if !strings.Contains(chatUsage, "gopi chat --once") {
		t.Error("the chat usage's SYNOPSIS line does not mention the one-shot face")
	}
}

// TestChatOnceParseArgs_DriverKnobs pins the pipe-back driver's argv surface:
// which spellings arm it, how each cap is read, and that the knobs never leak
// into the argv the REPL parses (a leaked --max-turns would reach the REPL's own
// parser and be read as a session SEED — the exact class of silent
// reinterpretation this parser exists to refuse).
func TestChatOnceParseArgs_DriverKnobs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantTurns  int
		wantSet    bool
		wantMax    int
		wantStop   string
		wantPrompt string
		wantRest   []string
	}{
		{name: "turns then prompt", args: []string{"--once", chatOnceMaxTurnsFlag, "3", "go"}, wantTurns: 3, wantSet: true, wantPrompt: "go"},
		{name: "turns joined", args: []string{"--once", chatOnceMaxTurnsFlag + "=3", "go"}, wantTurns: 3, wantSet: true, wantPrompt: "go"},
		{name: "turns zero is uncapped but still armed", args: []string{"--once", chatOnceMaxTurnsFlag, "0", "go"}, wantTurns: 0, wantSet: true, wantPrompt: "go"},
		{name: "negative turns is the explicit uncapped spelling", args: []string{"--once", chatOnceMaxTurnsFlag, "-1", "go"}, wantTurns: -1, wantSet: true, wantPrompt: "go"},
		{name: "max-seconds alone fails below", args: []string{"--once", chatOnceMaxTurnsFlag, "4", "--max-seconds", "60", "go"}, wantTurns: 4, wantSet: true, wantMax: 60, wantPrompt: "go"},
		{name: "max-seconds joined", args: []string{"--once", chatOnceMaxTurnsFlag + "=4", "--max-seconds=60", "go"}, wantTurns: 4, wantSet: true, wantMax: 60, wantPrompt: "go"},
		{name: "sentinel", args: []string{"--once", chatOnceMaxTurnsFlag, "9", "--stop-on", "TASK COMPLETE", "go"}, wantTurns: 9, wantSet: true, wantStop: "TASK COMPLETE", wantPrompt: "go"},
		{name: "all three, order free", args: []string{"--stop-on", "DONE", "--max-seconds", "5", chatOnceMaxTurnsFlag, "2", "go", "--once"}, wantTurns: 2, wantSet: true, wantMax: 5, wantStop: "DONE", wantPrompt: "go"},
		{name: "tools still rides through", args: []string{"--once", chatOnceMaxTurnsFlag, "2", "go", "--tools"}, wantTurns: 2, wantSet: true, wantPrompt: "go", wantRest: []string{"--tools"}},
	}
	for _, c := range cases {
		got, err := chatOnceParseArgs(c.args)
		if err != nil {
			t.Errorf("%s: chatOnceParseArgs(%v) error: %v", c.name, c.args, err)
			continue
		}
		if got.turns != c.wantTurns || got.turnsSet != c.wantSet {
			t.Errorf("%s: turns=%d turnsSet=%v, want %d/%v", c.name, got.turns, got.turnsSet, c.wantTurns, c.wantSet)
		}
		if got.maxSeconds != c.wantMax {
			t.Errorf("%s: maxSeconds=%d, want %d", c.name, got.maxSeconds, c.wantMax)
		}
		if got.sentinel != c.wantStop {
			t.Errorf("%s: sentinel=%q, want %q", c.name, got.sentinel, c.wantStop)
		}
		if got.prompt != c.wantPrompt {
			t.Errorf("%s: prompt=%q, want %q", c.name, got.prompt, c.wantPrompt)
		}
		if strings.Join(got.rest, " ") != strings.Join(c.wantRest, " ") {
			t.Errorf("%s: rest=%v, want %v", c.name, got.rest, c.wantRest)
		}
	}
}

// TestChatOnceParseArgs_DriverCompanionsNeedArming: --max-seconds and --stop-on
// are stop conditions OF the driver, so they mean nothing without the turn cap. A
// silent no-op is the defect this refuses (a caller who forgot the turn cap would
// otherwise watch a single turn run and wonder why the sentinel did nothing).
func TestChatOnceParseArgs_DriverCompanionsNeedArming(t *testing.T) {
	for _, args := range [][]string{
		{"--once", "go", "--max-seconds", "5"},
		{"--once", "go", "--stop-on", "DONE"},
	} {
		got, err := chatOnceParseArgs(args)
		if err == nil {
			t.Errorf("chatOnceParseArgs(%v) = %+v, want a usage error naming the missing %s", args, got, chatOnceMaxTurnsFlag)
			continue
		}
		if !strings.Contains(err.Error(), chatOnceMaxTurnsFlag) {
			t.Errorf("chatOnceParseArgs(%v) error = %q, want it to name %s", args, err, chatOnceMaxTurnsFlag)
		}
	}
}

// TestChatOnceLegacyTurnsSpellingIsRefusedByName is the minimal repro of the
// rename's ONE new risk: `--turns` used to BE the cap here while it is a LISTING
// word on gopi's other surfaces, so the retired spelling must be refused with the
// working one in the SENTENCE — not silently read as a cap (the old behaviour)
// and not silently ignored (a no-op that runs one turn while the caller believes
// they armed a loop). Both shapes are walked, bare and joined.
func TestChatOnceLegacyTurnsSpellingIsRefusedByName(t *testing.T) {
	for _, argv := range [][]string{
		{chatOnceLegacyTurnsFlag, "2", "go"},
		{chatOnceLegacyTurnsFlag + chatDumpFlagSep + "2", "go"},
	} {
		args := append([]string{"--once"}, argv...)
		got, err := chatOnceParseArgs(args)
		if err == nil {
			t.Fatalf("chatOnceParseArgs(%v) = %+v, want a refusal — the retired spelling must never be read as the cap", args, got)
		}
		if !strings.Contains(err.Error(), chatOnceLegacyTurnsFlag) {
			t.Errorf("the refusal does not name the word that was typed: %q", err)
		}
		if !strings.Contains(err.Error(), chatOnceMaxTurnsFlag) {
			t.Errorf("the refusal does not name the working spelling %s, so the caller has no runnable command: %q", chatOnceMaxTurnsFlag, err)
		}
	}
}

// TestChatOnceParseArgs_RefusesBadDriverValues: a non-numeric cap, a negative or
// absurd duration, an empty sentinel and a repeated sentinel are all USAGE
// errors — each one would otherwise silently change when (or whether) the loop
// ends, which is the least debuggable failure a driver can have.
func TestChatOnceParseArgs_RefusesBadDriverValues(t *testing.T) {
	for _, args := range [][]string{
		// THE RETIRED SPELLING IS REFUSED BY NAME, never read as a cap: on this
		// face `--turns` used to mean the cap while on every other gopi surface it
		// means "list the turns", so a silent reinterpretation would leave one
		// spelling meaning two things (Rule 15 — say what happened).
		{"--once", chatOnceLegacyTurnsFlag, "2", "go"},
		{"--once", chatOnceLegacyTurnsFlag + "=2", "go"},
		{"--once", chatOnceMaxTurnsFlag, "three", "go"},
		{"--once", chatOnceMaxTurnsFlag}, // no value
		{"--once", chatOnceMaxTurnsFlag, "2", "--max-seconds", "-1", "go"},
		{"--once", chatOnceMaxTurnsFlag, "2", "--max-seconds", "99999999", "go"},
		{"--once", chatOnceMaxTurnsFlag, "2", "--max-seconds", "soon", "go"},
		{"--once", chatOnceMaxTurnsFlag, "2", "--stop-on", "   ", "go"},
		{"--once", chatOnceMaxTurnsFlag, "2", "--stop-on", "A", "--stop-on", "B", "go"},
		{"--once", chatOnceMaxTurnsFlag, "2", "--stop-on"}, // no value
	} {
		if got, err := chatOnceParseArgs(args); err == nil {
			t.Errorf("chatOnceParseArgs(%v) = %+v, want a usage error", args, got)
		}
	}
}

// TestChatOnceSentinelFound_IsWholeWord: the sentinel must not fire on a
// substring — `DONE` inside `ABANDONED` would end a loop the engine was still
// working on, which is the one mistake this matcher exists to prevent.
func TestChatOnceSentinelFound_IsWholeWord(t *testing.T) {
	cases := []struct {
		answer   string
		sentinel string
		want     bool
	}{
		{"all finished. DONE", "DONE", true},
		{"DONE", "DONE", true},
		{"DONE.", "DONE", true},
		{"we are ABANDONED here", "DONE", false},
		{"the answer contains done in lower case", "done", true},
		{"nothing to report", "DONE", false},
		{"status: DONE — handing back", "DONE", true},
		{"", "DONE", false},
	}
	for _, c := range cases {
		if got := chatOnceSentinelFound(c.answer, c.sentinel); got != c.want {
			t.Errorf("chatOnceSentinelFound(%q, %q) = %v, want %v", c.answer, c.sentinel, got, c.want)
		}
	}
}

// TestChatOnceDriverResolveStop pins the stop policy — which condition fires,
// and in what order — because that order is what the operator reads. The caps
// are checked last on purpose: an engine that restated itself AND hit the cap on
// the same turn is better described as "no progress".
func TestChatOnceDriverResolveStop(t *testing.T) {
	const a1 = "turn one answered at length, so the turn is not degenerate."
	const a2 = "turn two is a different answer at length, as it should be."

	newDriver := func(opts chatOnceDriverOptions) *chatOnceDriver {
		return newChatOnceDriver(opts, "seed")
	}
	t.Run("turns cap", func(t *testing.T) {
		d := newDriver(chatOnceDriverOptions{turns: 2})
		if d.resolveStop(a1) {
			t.Fatal("the loop stopped after ONE turn with the turn cap at 2")
		}
		if !d.resolveStop(a2) {
			t.Fatal("the loop did not stop at the turn cap of 2")
		}
		if d.stopReason != chatOnceStopTurns || d.stopText != "2" {
			t.Errorf("stop = %s/%q, want %s/2", d.stopReason, d.stopText, chatOnceStopTurns)
		}
	})
	t.Run("no progress beats the cap", func(t *testing.T) {
		d := newDriver(chatOnceDriverOptions{turns: 5})
		d.resolveStop(a1)
		if !d.resolveStop(a1) {
			t.Fatal("an identical answer did not stop the loop")
		}
		if d.stopReason != chatOnceStopNoProgress {
			t.Errorf("stop = %s, want %s", d.stopReason, chatOnceStopNoProgress)
		}
	})
	t.Run("sentinel beats the cap", func(t *testing.T) {
		d := newDriver(chatOnceDriverOptions{turns: 1, sentinel: "DONE"})
		if !d.resolveStop(a1 + " DONE") {
			t.Fatal("the sentinel did not stop the loop")
		}
		if d.stopReason != chatOnceStopSentinel {
			t.Errorf("stop = %s, want %s", d.stopReason, chatOnceStopSentinel)
		}
	})
	t.Run("max-seconds", func(t *testing.T) {
		d := newDriver(chatOnceDriverOptions{turns: -1, maxSeconds: 1})
		d.start = time.Now().Add(-2 * time.Second)
		if !d.resolveStop(a1) {
			t.Fatal("the wall-clock cap did not stop the loop")
		}
		if d.stopReason != chatOnceStopMaxSeconds {
			t.Errorf("stop = %s, want %s", d.stopReason, chatOnceStopMaxSeconds)
		}
	})
	t.Run("an armed driver with no caps keeps going", func(t *testing.T) {
		d := newDriver(chatOnceDriverOptions{turns: -1})
		if d.resolveStop(a1) || d.resolveStop(a2) {
			t.Error("an uncapped driver stopped for a reason it was not given")
		}
	})
}

// TestChatOnceUsageDocumentsTheDriver: the help page is the ONLY place a caller
// learns the driver exists, so the contract it implements is asserted against
// the text — the flags, the separator, the stop reasons and the exit statuses.
func TestChatOnceUsageDocumentsTheDriver(t *testing.T) {
	for _, want := range []string{
		chatOnceMaxTurnsFlag,
		chatOnceMaxSecondsFlag,
		chatOnceStopFlag,
		chatOnceTurnSep,
		"no progress",
		"NOT a failure",
		"1 = a turn FAILED",
	} {
		if !strings.Contains(chatUsage, want) {
			t.Errorf("chatUsage does not document %q — the driver would be an undocumented surface", want)
		}
	}
}
