package sessionevent

// harness_briefing_test.go — THE TWO PRODUCERS' RENDERED TEXT, TESTED BY THE
// PREDICATES A READER USES.
//
// THE DEFECT THIS GUARDS. Two production points hand a child agent its briefing
// on stdin (`cmd/agent/cli_tasks.go`'s task-run / worker child and
// `internal/tools/tasks.go`'s independent-verify child). The child records that
// stdin text as its first user row, and the record files it `source=human`
// because the stdin seam has no other honest face. Nothing downstream could then
// tell a briefing from something the operator typed: `session log --turns`
// collapsed the gaps between framework briefings into spurious turn seams, and
// the row's first line became `chats.first_msg` / the `session list` title — the
// framework's instructions standing in for the person's opening line.
//
// THE FIX IS ONE MARKER AND ONE WRAPPER (Rule 14): the value lives here, the two
// producers call BriefingOnStdin, and the reader's OWN predicate recognises what
// the producer wrote. This file pins the three things a shared marker can get
// wrong:
//
//	(1) MARKED ⇒ (IsHarnessInjected, no turn): the producer's output must be
//	    recognised by both predicates, under the human face it is recorded on.
//	(2) A REAL OPERATOR ROW IS THE COUNTER-EXAMPLE: the same predicates must
//	    answer the opposite way for a line the person typed, or the marker has
//	    swallowed the operator instead of the framework.
//	(3) THE WRAPPER IS IDEMPOTENT AND CHEAP: a second call must not stack
//	    markers, and the runes it spends must be measured (the ledger folds a
//	    briefing's first line into 200 runes; internal/agent's own test pins the
//	    STORED row after the fold, this one pins the cost).
//
// The BRIEFING BODIES below are the two producers' real first lines, quoted
// rather than re-invented: a fixture that paraphrased them would pass while the
// producers drifted.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/inputsource"
)

// briefingFixtureBodies are the two producers' real renderings, one per point.
// Each is the text the parent hands over BEFORE the marker — the body, not the
// wrapped form.
var briefingFixtureBodies = []struct {
	name string
	body string
}{
	{
		name: "cmd/agent/cli_tasks.go's task-run / worker briefing",
		// taskRunBriefing's composition: the summary section first.
		body: "[Task summary]\nbug: the two stdin-briefing producers carry no harness marker\n[Briefing payload]\n## 现象\n\n同一个缺陷在两个生产点仍然存在。",
	},
	{
		name: "internal/tools/tasks.go's independent-verify briefing",
		// taskVerifyBriefing's opening sentence.
		body: `You are an independent verification instance for ledger task 01a188da-60ae-761f-8810-30c633456575 ("the marker row"). Locate the delivered fix, build the project, and run the SMOKE tests against the acceptance criteria below.`,
	},
}

// TestBriefingOnStdinRendersAHarnessRow is arm (1): the text the producer hands
// the child IS a harness row to both predicates every reader uses.
func TestBriefingOnStdinRendersAHarnessRow(t *testing.T) {
	for _, f := range briefingFixtureBodies {
		marked := BriefingOnStdin(f.body)
		if !IsHarnessInjected(marked) {
			t.Errorf("%s: the wrapped briefing is NOT recognised as a harness injection — `session log --turns` will count the framework's briefing as the operator's turn", f.name)
		}
		// THE FACE IS THE HUMAN ONE, and that is the whole point: the stdin seam
		// records every row this way, so the predicate must answer for it.
		// IT OPENS A TURN, AND THAT IS THE 2026-11-01 FIX (measured on the
		// dispatched RUN session 01a1944e-900f-70b7-81ac-24eda447b38b): a run
		// session has no operator, so the briefing it was dispatched with is the
		// ONLY complete input it ever receives — and while this arm asserted the
		// opposite, NOTHING in a 136-row log opened a turn. The reader's
		// request-row fallback then handed every exchange a turn of its own, and
		// `--turns` printed 127 turns of one round each (the exchange count
		// wearing the turn's word).
		//
		// The row is STILL framework chatter (asserted below), and the two are
		// not in conflict: it is the same pair a `gopi notify` delivery has
		// carried since the 2026-10-27 ruling — harness-written AND an input,
		// because the rule is about what the harness must ANSWER, not about who
		// typed the row.
		if !IsTurnOpeningRow(marked, inputsource.Human) {
			t.Errorf("%s: the wrapped briefing does NOT open a turn — a dispatched run then records no input at all, and its turn count collapses onto its round count", f.name)
		}
		if !IsFrameworkChatter(marked) {
			t.Errorf("%s: the wrapped briefing is not framework chatter, so some surface will render it as the operator's line", f.name)
		}
		// The body itself must be UNCHANGED: the marker is a prefix, not a
		// rewrite (a producer's bytes are the briefing's content).
		if !strings.HasSuffix(marked, f.body) {
			t.Errorf("%s: the wrapper altered the briefing body", f.name)
		}
		if !strings.HasPrefix(marked, HarnessBriefingOnStdinMarker) {
			t.Errorf("%s: the wrapper did not put the marker at the head — the reader tests a LEADING marker", f.name)
		}
	}
}

// TestOperatorRowsAreTheCounterExample is arm (2), and it is the arm that keeps
// the fix honest: the same two predicates must call a typed line the person's
// own. A marker that classified those too would pass arm (1) and destroy the
// turn count in the other direction (one conversation, zero turns).
func TestOperatorRowsAreTheCounterExample(t *testing.T) {
	asks := []string{
		"你自己去执行去看看",
		"1：false 2:开吧。3.这个是真的示例，你可以先试验一下。",
		// A person DISCUSSING the marker is not a marked row: only a LEADING
		// marker makes one, and this row's marker is in the middle of prose.
		"为什么 " + HarnessBriefingOnStdinMarker + " 会出现在 STORED 行里？",
		"",
	}
	for _, ask := range asks {
		if IsHarnessInjected(ask) {
			t.Errorf("%q was classified as a harness injection — the operator's own line will be folded into the framework's chatter", ask)
		}
		if ask == "" {
			continue // the empty row is nobody's input; IsTurnOpeningRow's tail is asserted elsewhere
		}
		if !IsTurnOpeningRow(ask, inputsource.Human) {
			t.Errorf("%q does NOT open a turn — the operator's own line vanished from `session log --turns`", ask)
		}
	}
}

// TestBriefingOnStdinIsIdempotent is arm (3), the wrapper's own contract: a
// producer that already marked its text (or a briefing that passes the seam
// twice) must not accumulate markers. The check is the READER's predicate, so
// "already marked" and "recognised as an injection" cannot disagree.
func TestBriefingOnStdinIsIdempotent(t *testing.T) {
	body := briefingFixtureBodies[0].body
	once := BriefingOnStdin(body)
	twice := BriefingOnStdin(once)
	if twice != once {
		t.Fatalf("the wrapper stacked its marker:\n once: %q\ntwice: %q", once, twice)
	}
	if strings.Count(twice, HarnessBriefingOnStdinMarker) != 1 {
		t.Fatalf("the marker appears %d times after two wraps, want 1", strings.Count(twice, HarnessBriefingOnStdinMarker))
	}
	// A row already carrying ANOTHER harness marker is left alone for the same
	// reason: it is already the framework's row, and a second marker would make
	// the head of the stored line the marker's own name.
	other := HarnessMessagePrefix + "TASK-START CHECKPOINT: …"
	if got := BriefingOnStdin(other); got != other {
		t.Fatalf("an already-injected row was re-marked:\n got: %q\nwant: %q", got, other)
	}
	// Leading whitespace before the marker is still "already marked" — the
	// predicate trims, so an assembled message beginning "\n\n[system] …" is
	// recognised and must not be wrapped a second time.
	padded := "\n\n" + HarnessBriefingOnStdinMarker + body
	if got := BriefingOnStdin(padded); got != padded {
		t.Fatalf("a leading-whitespace-padded marked row was re-marked:\n got: %q\nwant: %q", got, padded)
	}
	// The empty briefing stays empty: a marker with nothing behind it would be a
	// stored row whose whole content is the framework's label.
	if got := BriefingOnStdin(""); got != "" {
		t.Fatalf("BriefingOnStdin(%q) = %q, want %q", "", got, "")
	}
}

// TestFramedBriefingIsTheComposedRow pins the SECOND face a dispatched child
// records its briefing on, and the reason the frame is not a marker by itself.
//
// Measured: this profile's tree holds 1118 session logs whose first row opens
// with the bare frame, so declaring the frame an injection reclassifies the whole
// corpus (it turned ten internal/gopilog golden tests red when tried). What IS an
// injection is the COMPOSITION — the frame wrapped around a briefing that went
// through this wrapper — because that row can only exist once a producer marked
// its text.
func TestFramedBriefingIsTheComposedRow(t *testing.T) {
	body := briefingFixtureBodies[0].body
	framed := HarnessEpicGoalFrameMarker + "\n" + BriefingOnStdin(body)
	if !IsHarnessInjected(framed) {
		t.Fatal("the framed+marked row is NOT recognised as an injection — every dispatched child that takes the framed face will read as the operator's opening turn")
	}
	if !IsTurnOpeningRow(framed, inputsource.Human) {
		t.Fatal("the framed+marked row does NOT open a turn — a dispatched run taking this face then records no input, and its turn count collapses onto its round count (measured: 01a1944e, 127 rounds / 127 turns)")
	}
	// THE COUNTER-EXAMPLE, and it is the measured decision this marker did NOT
	// take: the bare frame around an UNMARKED body is not an injection.
	unmarked := HarnessEpicGoalFrameMarker + "\n" + body
	if IsHarnessInjected(unmarked) {
		t.Fatal("the BARE frame is classified as an injection — that reclassifies the 1118 already-recorded briefings this profile holds, a corpus-wide decision this change deliberately did not make")
	}
	if !IsTurnOpeningRow(unmarked, inputsource.Human) {
		t.Fatal("the bare frame must still open a turn (it is the pre-existing corpus's shape), so this fixture proves nothing about the composition above")
	}
}

// TestTheMarkerCostsAMeasuredNumberOfRunes pins the PRICE, because it is paid out
// of a bounded budget: the ledger folds a briefing's first line into
// agent.AutoTaskSummaryBudget (200) runes, so every rune this marker spends
// pushes the decisive text of a stored row to the right.
//
// The number is asserted, not described: a future rewording that doubles it
// changes what a stored row can still say, and internal/agent's own test pins
// that the decisive line SURVIVES the fold at this cost.
func TestTheMarkerCostsAMeasuredNumberOfRunes(t *testing.T) {
	const wantRunes = 16 // "[gopi briefing] " — 15 characters + the separating space
	if got := len([]rune(HarnessBriefingOnStdinMarker)); got != wantRunes {
		t.Fatalf("the marker costs %d runes, want %d — the stored-row budget (100 runes of headroom under the 200-rune fold) is sized to this number; rewording it changes what a ledger summary can still say", got, wantRunes)
	}
}
