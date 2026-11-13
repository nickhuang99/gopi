// The LIVE INTERIM DRAFT REGION's gate.
//
// Why this file exists (the defect it pins, measured): while a live recording
// ran, the page drew the recognised-so-far text as an interim region inside the
// composer. micDraftClear GUARDED the removal by IDENTITY
// (cur.slice(head, head+len) === liveDraft.text) and then PERFORMED it by
// LENGTH (head + cur.slice(head.length+liveDraft.text.length)). An operator who
// typed one character INSIDE the region made the two disagree — the guard said
// "not mine any more", nothing was removed, liveDraft was then nulled, and the
// recognised sentence was inserted at a caret sitting in the middle of the
// ORPHANED draft:
//
//  1. interim in composer   : "请注意 尼好"
//  2. operator edits interim: "请注意 尼X好"
//  3. after settle          : "请注意 尼X 你好世界。好"   ← split, draft left behind
//
// It is a PRE-EXISTING defect (the same page before the ESC refactor garbles
// identically) that the ESC refactor made ORDINARY: Escape is now STOP AND KEEP,
// so pressing it in order to proof-read the sentence is the expected act, and
// proof-reading means putting the caret into the recognised text.
//
// The gate is split in two halves, because neither can see the defect alone:
//
//  1. TestWebVoiceDraft_OperatorEditInsideTheRegionSurvives DRIVES the shipped
//     page through the harness: a real draft is drawn, the operator's edit is
//     simulated, ESC stops the recording, the final frame settles. A page that
//     locates the region one way and removes it another fails HERE.
//  2. TestWebVoiceDraft_RegionIsLocatedByOneReading is the static half: the
//     guard and the removal must come from ONE reading of "where is the draft"
//     (micDraftRegion). The length-slice removal — the exact expression whose
//     disagreement IS the defect — must be gone for good.
package main

import (
	"os/exec"
	"strings"
	"testing"
)

// voiceDraftScenarioName is the scenario the harness is asked to run alone. It
// is a CONSTANT (Rule 1/13), and its VALUE matters: `--only` must match at
// least one scenario (the harness exits 1 on an empty match), so deleting the
// scenario fails the gate below instead of turning it green-blind.
const voiceDraftScenarioName = "voice draft"

// voiceDraftScenarioPhrase is the composer the scenario must end with once the
// operator's in-region edit has been preserved and the sentence has landed after
// it. It is spelled here so the assertion names the OUTCOME, not just a tally.
const voiceDraftScenarioPhrase = "请注意 尼X好 你好世界。"

// voiceDraftOldRemoval is the length-slice expression the fix removed: the head
// plus a slice taken by liveDraft.text.length, which is arithmetic on a length
// the composer no longer agrees with. Its presence means the guard and the
// removal have drifted apart again.
const voiceDraftOldRemoval = "head+cur.slice(head.length+liveDraft.text.length)"

// inputSetSelectionRange is the DOM call every caret move goes through, spelled
// once so the count below is not a bare literal (Rule 1/13).
const inputSetSelectionRange = "input.setSelectionRange("

// pageSelectionWriterCount is how many sites in the page may call it: micCaretTo
// (the voice path's ONE writer, clamped into the composer) and menuComplete
// (arithmetic on the completion it just wrote, outside the voice path).
const pageSelectionWriterCount = 2

// TestWebVoiceDraft_OperatorEditInsideTheRegionSurvives drives the REAL page: a
// live draft is on screen, the operator types one character INSIDE it, ESC stops
// the recording, and the settled sentence must land AFTER that edit with no
// orphaned fragment. It also pins the two neighbouring cases the same code path
// owns — an UNEDITED draft is replaced (never left beside the sentence) and a
// draft the operator deleted stays deleted.
func TestWebVoiceDraft_OperatorEditInsideTheRegionSurvives(t *testing.T) {
	node := nodePath(t)
	out, runErr := exec.Command(node, pageHarnessPath, "--only", voiceDraftScenarioName, "--page", menuNavGoFile).CombinedOutput()
	text := string(out)
	if !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) || runErr != nil {
		t.Fatalf("the live-draft-region scenario did not pass (err=%v):\n%s", runErr, text)
	}
	// The tally alone could pass on a scenario that asserted the WRONG outcome
	// (a scenario is code too), so the compositor's end state is named here.
	if !strings.Contains(text, voiceDraftScenarioPhrase) {
		t.Fatalf("the scenario did not report the composer ending as %q:\n%s", voiceDraftScenarioPhrase, text)
	}
	t.Logf("live-draft-region scenario:\n%s", text)
}

// TestWebVoiceDraft_RegionIsLocatedByOneReading is the static half.
//
// It asserts that micDraftClear asks micDraftRegion where the region is — one
// reading shared with the guard — and that the length-slice removal is gone. The
// defect was not "the slice was off by one"; it was that two different questions
// ("is it still mine?" by identity, "where does it end?" by length) were answered
// separately and nothing made them agree.
func TestWebVoiceDraft_RegionIsLocatedByOneReading(t *testing.T) {
	script := pageScript(t)

	if strings.Contains(script, voiceDraftOldRemoval) {
		t.Fatalf("the page still removes the interim region by LENGTH (%q) — the guard compares the region by identity, so one character typed inside the draft makes the two disagree and the draft is orphaned", voiceDraftOldRemoval)
	}
	for _, want := range []string{
		"function micDraftRegion(",
		"const region=micDraftRegion();",
		"region.intact",
		"region.lost",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("the page does not carry %q — the interim region must be located ONCE (micDraftRegion) so the guard and the removal cannot drift apart", want)
		}
	}
	// THE CARET IS CLAMPED INTO THE COMPOSER, never left as a raw index: a
	// settle that trusts a stale selectionStart is the other half of how a
	// sentence lands in the middle of text it did not write. Exactly TWO sites
	// may touch the selection — micCaretTo (the voice path's one writer) and
	// menuComplete (the completion caret, which is arithmetic on the completion
	// it just wrote and deliberately not part of the voice path).
	if n := strings.Count(script, inputSetSelectionRange); n != pageSelectionWriterCount {
		t.Fatalf("the page calls %s at %d sites; the voice path must route every caret move through micCaretTo (got %d, want %d — micCaretTo and menuComplete)",
			inputSetSelectionRange, n, n, pageSelectionWriterCount)
	}
}
