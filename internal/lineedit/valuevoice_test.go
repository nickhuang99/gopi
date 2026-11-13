package lineedit

// valuevoice_test.go — the DICTATION PROVENANCE the submitted line is measured
// by (valuevoice.go).
//
// WHY THIS FILE EXISTS AT ALL, as the operator stated the requirement: the turn's
// marker must be "does this line still hold voice-originated characters", NEVER
// "was voice mode open". The difference is one concrete scenario — dictate a
// sentence, delete it, type your own, press Enter — and it is the case the third
// test below pins. Every other case here exists so that "the count fell to zero"
// is a property of the BUFFER's own mutations rather than of one code path that
// happened to be exercised.
//
// The tests are at the seam (no goroutine, no terminal), because the property is
// about the buffer: the carrier-level proof that the marker reaches a real
// request runs in cmd/agent.

import "testing"

// voiceTestEditor builds an editor with an inert source and a discarded writer —
// the same rig lineinsert_test.go uses, so a case here can also drive the insert
// road that production takes.
func voiceTestEditor() *Editor {
	return NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
}

// TestDictationInsert_AppendsAndCountsEveryDictatedRune is the positive half:
// the road a `/voice` transcript takes stamps every rune it inserts, and the
// count is the LENGTH OF THE TEXT rather than a flag something could flip.
func TestDictationInsert_AppendsAndCountsEveryDictatedRune(t *testing.T) {
	ed := voiceTestEditor()
	const dictated = "deploy the staging box"
	if !ed.InsertDictationAtEnd(dictated) {
		t.Fatal("the dictation road refused the insert")
	}
	if got := ed.Value().Text(); got != dictated {
		t.Fatalf("the line reads %q, want %q", got, dictated)
	}
	if got, want := ed.Value().VoiceRunes(), len([]rune(dictated)); got != want {
		t.Fatalf("VoiceRunes = %d, want %d — the dictation road did not stamp every rune it inserted", got, want)
	}
}

// TestDictationInsert_TypedTextIsNotDictation is the control: the ORDINARY
// insert road (what a completer, a paste and the cooked reader use) stamps
// nothing, so a line the operator typed carries no provenance at all.
func TestDictationInsert_TypedTextIsNotDictation(t *testing.T) {
	ed := voiceTestEditor()
	if !ed.InsertIntoLineAtEnd("deploy the staging box") {
		t.Fatal("the ordinary insert road refused the text")
	}
	if got := ed.Value().VoiceRunes(); got != 0 {
		t.Fatalf("VoiceRunes = %d on a TYPED line, want 0 — the ordinary insert road claimed dictation provenance", got)
	}
}

// TestDictationThenMixedTyping is the composition: typed text before, dictated
// text after (the shape a real session produces — the operator types a lead-in,
// then dictates the rest), and the mix is counted rather than rounded to a
// boolean.
func TestDictationThenMixedTyping(t *testing.T) {
	ed := voiceTestEditor()
	if !ed.InsertIntoLineAtEnd("fix: ") {
		t.Fatal("the typed insert was refused")
	}
	if !ed.InsertDictationAtEnd("转写文本") {
		t.Fatal("the dictation insert was refused")
	}
	if got := ed.Value().Text(); got != "fix: 转写文本" {
		t.Fatalf("the line reads %q, want %q", got, "fix: 转写文本")
	}
	if got, want := ed.Value().VoiceRunes(), len([]rune("转写文本")); got != want {
		t.Fatalf("VoiceRunes = %d, want %d — only the dictated runes carry provenance", got, want)
	}
}

// TestDictationDeletedAndRetypedCarriesNoProvenance is THE case the requirement
// names: dictating a sentence, deleting it and typing one's own must leave the
// line with NO voice-originated characters, so the submitted turn carries no
// prefix.
//
// It is driven through the SAME mutations an operator has: Ctrl+U (KillToStart)
// to clear the line, then typing. The assertion is on the count the carrier
// measures, not on the mechanism that lowered it.
func TestDictationDeletedAndRetypedCarriesNoProvenance(t *testing.T) {
	ed := voiceTestEditor()
	if !ed.InsertDictationAtEnd("转写文本") {
		t.Fatal("the dictation insert was refused")
	}
	// Ctrl+U — "delete what I said, I will type it myself".
	ed.Value().KillToStart()
	if got := ed.Value().VoiceRunes(); got != 0 {
		t.Fatalf("after KillToStart VoiceRunes = %d, want 0 — the deleted transcript still claims provenance", got)
	}
	// The operator's own words.
	if ed.Value().InsertString("typed instead") > 0 {
		t.Fatal("the retyped text did not fit")
	}
	if got := ed.Value().VoiceRunes(); got != 0 {
		t.Fatalf("VoiceRunes = %d on a line that was dictated-then-retyped, want 0 — this is exactly the false provenance claim the requirement forbids", got)
	}
	if got := ed.Value().Text(); got != "typed instead" {
		t.Fatalf("the line reads %q, want %q", got, "typed instead")
	}
}

// TestDictationDeletedRuneByRuneCarriesNoProvenance is the same outcome through
// the slowest path — Backspace one character at a time — because an operator who
// proofs-reads a transcript deletes exactly like this, and the bookkeeping must
// not depend on which key they reached for.
func TestDictationDeletedRuneByRuneCarriesNoProvenance(t *testing.T) {
	ed := voiceTestEditor()
	const dictated = "abc"
	if !ed.InsertDictationAtEnd(dictated) {
		t.Fatal("the dictation insert was refused")
	}
	for i := 0; i < len(dictated); i++ {
		if !ed.Value().Backspace() {
			t.Fatalf("Backspace %d reported nothing to delete", i+1)
		}
	}
	if got := ed.Value().VoiceRunes(); got != 0 {
		t.Fatalf("after deleting every dictated rune VoiceRunes = %d, want 0", got)
	}
}

// TestDictationPartiallyDeletedCountsTheSurvivors is the middle state, and it is
// why the answer is a COUNT: the operator deleted SOME of the transcript, so the
// line still holds dictated characters and the prefix must still be there.
func TestDictationPartiallyDeletedCountsTheSurvivors(t *testing.T) {
	ed := voiceTestEditor()
	if !ed.InsertDictationAtEnd("abc") {
		t.Fatal("the dictation insert was refused")
	}
	if !ed.Value().Backspace() {
		t.Fatal("Backspace reported nothing to delete")
	}
	if got, want := ed.Value().VoiceRunes(), 2; got != want {
		t.Fatalf("VoiceRunes = %d after deleting one of three dictated runes, want %d", got, want)
	}
}

// TestDictationSurvivesAnInsertBeforeIt is the offset-correctness case: a
// dictation landing after text that is LATER inserted in front of it must keep
// the provenance on the right runes. A range list keyed on absolute offsets is
// what this catches; the parallel bit slice cannot drift.
func TestDictationSurvivesAnInsertBeforeIt(t *testing.T) {
	ed := voiceTestEditor()
	if !ed.InsertDictationAtEnd("DICTATED") {
		t.Fatal("the dictation insert was refused")
	}
	ed.Value().Home()
	if ed.Value().InsertString("typed:") > 0 {
		t.Fatal("the typed prefix did not fit")
	}
	if got, want := ed.Value().Text(), "typed:DICTATED"; got != want {
		t.Fatalf("the line reads %q, want %q", got, want)
	}
	if got, want := ed.Value().VoiceRunes(), len("DICTATED"); got != want {
		t.Fatalf("VoiceRunes = %d, want %d — the provenance did not follow the runes when text was inserted before them", got, want)
	}
}

// TestDictationClearedByResetAndSetText pins the two mutations whose new text is
// NOT a splice of the old: a fresh line (Reset, at the start of every read) and a
// history recall (SetText) both carry the operator's own text and no dictation.
func TestDictationClearedByResetAndSetText(t *testing.T) {
	ed := voiceTestEditor()
	if !ed.InsertDictationAtEnd("DICTATED") {
		t.Fatal("the dictation insert was refused")
	}
	ed.Value().SetText("recalled line")
	if got := ed.Value().VoiceRunes(); got != 0 {
		t.Fatalf("after SetText VoiceRunes = %d, want 0 — a recalled line is the operator's text, not a transcript", got)
	}
	ed.Value().Reset()
	if got := ed.Value().VoiceRunes(); got != 0 {
		t.Fatalf("after Reset VoiceRunes = %d, want 0", got)
	}
}

// TestDictationBitsTrackTheRunesOnEveryMutation is the invariant that makes the
// count trustworthy: the provenance slice is ALWAYS the same length as the rune
// slice. A violation means the count can name characters that are not on the
// line, which is the false-provenance claim one layer up.
func TestDictationBitsTrackTheRunesOnEveryMutation(t *testing.T) {
	ed := voiceTestEditor()
	v := ed.Value()
	steps := []struct {
		name string
		do   func()
	}{
		{"dictated insert", func() { v.insertRunesStamped([]rune("转写文本"), voiceOriginated) }},
		{"typed insert", func() { v.InsertString("abc") }},
		{"backspace", func() { v.Backspace() }},
		{"delete forward", func() { v.Home(); v.DeleteForward() }},
		{"kill to end", func() { v.End(); v.KillToEnd() }},
		{"kill to start", func() { v.KillToStart() }},
		{"yank", func() { v.Yank() }},
		{"replace range", func() { v.ReplaceRange(0, 1, "zz") }},
		{"set text", func() { v.SetText("recalled") }},
		{"reset", func() { v.Reset() }},
	}
	for _, s := range steps {
		s.do()
		if got, want := len(v.voice), len(v.runes); got != want {
			t.Fatalf("after %s: %d provenance bits for %d runes — the two slices must stay in step", s.name, got, want)
		}
	}
}
