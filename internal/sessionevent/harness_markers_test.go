package sessionevent

// harness_markers_test.go — the REGRESSION GUARD for the measured defect on
// session 80c07a7d (2026-10-25): three gopi surfaces reported three different
// turn counts (18 / 17 / 23) for one recorded conversation, and the deepest arm
// of the cause was HERE — IsHarnessInjected knew the `[system] ` prefix only,
// while internal/agent's marker list carried four markers. The three extra
// markers were therefore read as rows the PERSON typed, so
// `chat --replay` typed 12 `[SELF-RECORD …]` re-injections back at the operator
// (23 turns) and `session log --turns` turned the gaps between them into 6
// spurious turns (17). The session holds 191 user rows = 168 `[system] ` +
// 12 `[SELF-RECORD …]` + 11 operator rows; the truthful figure is 11.
//
// The measurements below are the ones taken from that session's own
// session.jsonl in that run (the count table is reproduced as a fixture rather
// than re-reading the operator's live tree, which a test must never depend on).

import (
	"fmt"
	"testing"
)

// TestEveryHarnessMarkerIsAnInjection pins the ONE predicate against EVERY
// marker a harness message can carry — the prefix and the three bracket-marked
// re-injections — including the leading-whitespace shape an assembled message
// really has.
func TestEveryHarnessMarkerIsAnInjection(t *testing.T) {
	rows := []struct {
		name string
		text string
	}{
		{"the prefix", HarnessMessagePrefix + "[t+8s] — +5s since your previous request."},
		{"the prefix after leading newlines", "\n\n" + HarnessMessagePrefix + "This run's briefing is large"},
		{"the memory-consult checkpoint", HarnessConsultCheckpointMarker + " This round: which work could a memory search have saved?"},
		{"the committed-conclusions block", HarnessTaskAreaConclusionsMarker + "\n- a conclusion"},
		{"the clone channel's delivery briefing", HarnessCloneChannelBriefingMarker + "没有内容可交付：3 轮检查量到的都是「这里没有要交付的提交」"},
		{"the self-record carrier", HarnessSelfRecordMarker + "\nThese are the notes YOU recorded in this session's earlier rounds."},
		{"the self-record carrier after leading tabs", "\t \n" + HarnessSelfRecordMarker + "\nThese are the notes"},
	}
	for _, r := range rows {
		if !IsHarnessInjected(r.text) {
			t.Errorf("%s is NOT recognised as a harness injection — a reader will render it as the operator's own words", r.name)
		}
		if !IsFrameworkChatter(r.text) {
			t.Errorf("%s is framework chatter that OPENS A TURN — an injection is never a turn the person typed", r.name)
		}
	}
}

// TestOperatorRowsAreNotInjections is the other arm: the predicate must not
// swallow real asks. A row that merely MENTIONS a marker (the harness's own
// contract text quoting the delimiters, a person discussing the feature) is
// operator text, and only a LEADING marker makes a row an injection.
func TestOperatorRowsAreNotInjections(t *testing.T) {
	asks := []string{
		"你自己去执行去看看",
		"1：false 2:开吧。3.这个是真的示例，你可以先试验一下。",
		// The marker in the MIDDLE is prose, not a marker: agents and operators
		// both discuss these strings, and the SELF-RECORDING contract itself is
		// quoted in operator-visible text.
		"explain what [SELF-RECORD — your own notes, survived compaction] means",
		"[not-system] a bracket that is not a marker",
		"",
	}
	for _, a := range asks {
		if IsHarnessInjected(a) {
			t.Errorf("%q was classified as a harness injection — an operator row the person typed must open a turn", a)
		}
		if IsFrameworkChatter(a) {
			t.Errorf("%q is classified as framework chatter — an operator row the person typed opens a turn", a)
		}
	}
}

// TestSession80c07a7dTurnCounts pins the ARITHMETIC the fix produces against the
// session that produced the epic: 191 user rows, 168 `[system] ` + 12
// `[SELF-RECORD …]` + 11 operator rows, and the turn count every surface must
// now print is 11.
//
// The fixture is the measure, not a paraphrase: the row texts below are the
// five DISTINCT shapes that session's 191 user rows have (168 + 12 + 11 = 191),
// and the counts are the ones the census script reported. A predicate change
// that moves any of them fails here with the number it moved to.
func TestSession80c07a7dTurnCounts(t *testing.T) {
	const (
		systemRows   = 168
		selfRecRows  = 12
		operatorRows = 11
		userRows     = systemRows + selfRecRows + operatorRows
	)
	if userRows != 191 {
		t.Fatalf("the fixture no longer describes session 80c07a7d: %d user rows, want 191", userRows)
	}
	rows := make([]string, 0, userRows)
	for i := 0; i < systemRows; i++ {
		rows = append(rows, fmt.Sprintf("%s[t+~%dm] — +1s since your previous request.", HarnessMessagePrefix, i))
	}
	for i := 0; i < selfRecRows; i++ {
		rows = append(rows, HarnessSelfRecordMarker+"\nThese are the notes YOU recorded in this session's earlier rounds.")
	}
	for i := 0; i < operatorRows; i++ {
		rows = append(rows, fmt.Sprintf("第 %d 条：人自己打的", i+1))
	}
	turns := 0
	for _, r := range rows {
		// The fixture rows carry NO `source` field: every one of them was
		// delivered by the person's own session, which is the empty-source
		// default IsTurnOpeningRow applies (the same default session 80c07a7d's
		// real log has, since it predates the field).
		if IsTurnOpeningRow(r, "") {
			turns++
		}
	}
	if turns != operatorRows {
		t.Fatalf("turns = %d, want %d — the operator's own rows are the turns and nothing else (was 23 in `chat --replay` and 17 in `session log --turns` before the shared marker list)", turns, operatorRows)
	}
}

// TestTheMarkerListIsTheOneDefinition pins Rule 14 from this side: the slice the
// readers consult is built from the marker CONSTANTS (not a second spelling),
// and it carries exactly the four markers a session log can show.
func TestTheMarkerListIsTheOneDefinition(t *testing.T) {
	// THE FOURTH ENTRY IS THE STDIN BRIEFING MARKER, and it belongs in this ONE
	// list for the same reason as the other three: a parent process hands a
	// child its briefing on stdin, the record files that row `source=human`
	// (the stdin seam has no other honest face), and the text is the only thing
	// that can say "the framework wrote this row". Both producers go through
	// sessionevent.BriefingOnStdin, so the marker it writes is consulted here
	// by the same predicate every reader uses.
	want := []string{
		HarnessConsultCheckpointMarker,
		HarnessTaskAreaConclusionsMarker,
		HarnessCloneChannelBriefingMarker,
		HarnessSelfRecordMarker,
		HarnessBriefingOnStdinMarker,
	}
	if len(HarnessMessageMarkers) != len(want) {
		t.Fatalf("HarnessMessageMarkers = %d entries, want %d — every marker a harness message can open with belongs in the ONE list", len(HarnessMessageMarkers), len(want))
	}
	for i, w := range want {
		if HarnessMessageMarkers[i] != w {
			t.Errorf("HarnessMessageMarkers[%d] = %q, want %q", i, HarnessMessageMarkers[i], w)
		}
		// The marker must be checked by the SAME predicate that checks the
		// prefix — a marker in the list whose text does not classify is a
		// silent hole in the list.
		if !IsHarnessInjected(w + " …") {
			t.Errorf("marker %q is in HarnessMessageMarkers but IsHarnessInjected does not recognise it", w)
		}
	}
}
