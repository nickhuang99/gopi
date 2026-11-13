package main

// chat_queue_test.go — the `/queue` contract, asserted on the TYPE rather than
// through a terminal.
//
// WHAT A UNIT CASE CAN AND CANNOT PROVE HERE (Rule 31's unit half). The
// behaviour `/queue` exists for is a LOOP behaviour: does a parked prompt become
// the next turn without an extra Enter, and does the turn in flight survive it?
// That is a claim about cli_chat.go's turn boundary, so it is asserted
// end-to-end, by driving the REAL binary on a pty
// (testsuite/chat_queue_pty_test.go). What is asserted HERE is the other half —
// the FIFO's own contract, which the E2E case must be able to TRUST: order,
// the bound and its refusal, the clear, and the one property the whole feature
// rests on ("a parked prompt either runs or is explicitly cleared — never
// silently dropped").

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestChatQueue_FIFOOrderAndTake is the ordering contract: prompts leave in the
// order they were parked, and Take reports exhaustion rather than an empty
// string (an empty prompt is indistinguishable from "nothing queued" — the
// distinction the loop's next branch depends on).
func TestChatQueue_FIFOOrderAndTake(t *testing.T) {
	var q chatQueue
	prompts := []string{"first", "second", "third"}
	for i, p := range prompts {
		pos, ok, refusal := q.Add(p)
		if !ok {
			t.Fatalf("Add(%q) refused although the queue holds %d of %d: %s", p, i, chatQueueBound, refusal)
		}
		if pos != i+1 {
			t.Fatalf("Add(%q) reported position %d, want %d (the note tells the operator WHERE it landed)", p, pos, i+1)
		}
	}
	if got := q.Len(); got != len(prompts) {
		t.Fatalf("Len() = %d after %d adds", got, len(prompts))
	}
	// Items is a copy: mutating what it returns must not reach the queue, or a
	// list rendering would be able to edit what it printed.
	items := q.Items()
	items[0] = "clobbered"
	if got := q.Items()[0]; got != prompts[0] {
		t.Fatalf("Items() handed out the queue's own storage: the caller's write reached the queue (%q)", got)
	}
	for i, want := range prompts {
		got, ok := q.Take()
		if !ok {
			t.Fatalf("Take() #%d reported an empty queue although %d prompt(s) remained", i+1, len(prompts)-i)
		}
		if got != want {
			t.Fatalf("Take() #%d = %q, want %q — the queue must be FIFO: a prompt that overtakes another reorders the operator's own conversation", i+1, got, want)
		}
	}
	if got, ok := q.Take(); ok || got != "" {
		t.Fatalf("Take() on an empty queue = (%q, %v), want (\"\", false)", got, ok)
	}
	if q.Len() != 0 {
		t.Fatalf("Len() = %d after draining the queue", q.Len())
	}
}

// TestChatQueue_BoundRefusesByNameAndKeepsEveryAcceptedPrompt is the epic's bound
// assertion at the type level: the (chatQueueBound+1)-th add is REFUSED, the
// refusal NAMES the bound, and — the point of refusing rather than dropping —
// every prompt accepted before it is still there afterwards, in order.
func TestChatQueue_BoundRefusesByNameAndKeepsEveryAcceptedPrompt(t *testing.T) {
	var q chatQueue
	accepted := make([]string, 0, chatQueueBound)
	for i := 0; i < chatQueueBound; i++ {
		p := fmt.Sprintf("parked-%02d", i)
		if _, ok, refusal := q.Add(p); !ok {
			t.Fatalf("Add #%d was refused at depth %d, before the bound (%d) was reached: %s", i+1, i, chatQueueBound, refusal)
		}
		accepted = append(accepted, p)
	}
	over := "one-too-many"
	pos, ok, refusal := q.Add(over)
	if ok {
		t.Fatalf("Add(%q) was ACCEPTED at the bound: the queue grew to %d items past its ceiling of %d — the silent overflow this bound exists to prevent", over, q.Len(), chatQueueBound)
	}
	if pos != 0 {
		t.Errorf("the over-bound refusal reported position %d, want 0 (nothing was parked)", pos)
	}
	// Rule 15: the refusal must name the bound it enforced and the command that
	// refused, or the reader cannot act on it.
	if !strings.Contains(refusal, fmt.Sprintf("%d", chatQueueBound)) {
		t.Errorf("the over-bound refusal does not name the bound (%d): %q", chatQueueBound, refusal)
	}
	if !strings.Contains(refusal, "/"+chatQueueCommandName) {
		t.Errorf("the over-bound refusal does not name the command that refused: %q", refusal)
	}
	if !strings.Contains(refusal, "/"+chatQueueCommandName+" "+chatQueueClearWord) {
		t.Errorf("the over-bound refusal does not name the way out (`/%s %s`): %q", chatQueueCommandName, chatQueueClearWord, refusal)
	}
	// NOTHING WAS DROPPED: the accepted prompts are all still queued, in order.
	if got := q.Len(); got != chatQueueBound {
		t.Fatalf("Len() = %d after the refused add, want %d — a refusal must not cost an accepted prompt", got, chatQueueBound)
	}
	if got := q.Items(); strings.Join(got, ",") != strings.Join(accepted, ",") {
		t.Fatalf("the queue holds %v after the refused add, want %v", got, accepted)
	}
}

// TestChatQueue_RefusesAnEmptyPrompt pins the second refusal: whitespace is not a
// prompt. Accepting it would park a line the loop's own blank-line skip then eats
// — a silent drop wearing an accepted prompt's clothes.
func TestChatQueue_RefusesAnEmptyPrompt(t *testing.T) {
	var q chatQueue
	for _, blank := range []string{"", "   ", "\t", "\n  \n"} {
		if _, ok, refusal := q.Add(blank); ok {
			t.Fatalf("Add(%q) was accepted: an empty prompt would be eaten by the loop's blank-line skip, which is the silent drop this queue must not have", blank)
		} else if !strings.Contains(refusal, "/"+chatQueueCommandName) {
			t.Errorf("the empty-prompt refusal does not name the command: %q", refusal)
		}
	}
	if q.Len() != 0 {
		t.Fatalf("Len() = %d after four refused adds", q.Len())
	}
}

// TestChatQueue_ClearReportsWhatItDroppedAndIsIdempotent pins the clear: it
// reports the REAL count (the note is built from this number), it empties the
// queue, and a second clear is a stated no-op rather than an error.
func TestChatQueue_ClearReportsWhatItDroppedAndIsIdempotent(t *testing.T) {
	var q chatQueue
	for _, p := range []string{"a", "b", "c"} {
		if _, ok, refusal := q.Add(p); !ok {
			t.Fatalf("Add(%q): %s", p, refusal)
		}
	}
	if n := q.Clear(); n != 3 {
		t.Fatalf("Clear() reported %d dropped prompts, want 3 — the note's count is this return value", n)
	}
	if q.Len() != 0 {
		t.Fatalf("Len() = %d after Clear()", q.Len())
	}
	if _, ok := q.Take(); ok {
		t.Fatal("a prompt survived Clear()")
	}
	if n := q.Clear(); n != 0 {
		t.Fatalf("Clear() on an empty queue reported %d, want 0 (the no-op gets its own wording, so the number must be real)", n)
	}
}

// TestChatQueue_ListRenderingIsEmptyWhenEmptyAndNamesEveryPrompt pins the list:
// nothing queued renders NO rows (the caller owns the empty sentence), and a
// queued prompt appears with its 1-based position and the depth/bound header.
// The visibility half of the epic lives on this text, so it is asserted rather
// than eyeballed.
func TestChatQueue_ListRenderingIsEmptyWhenEmptyAndNamesEveryPrompt(t *testing.T) {
	var q chatQueue
	if lines := chatQueueListLines(&q); len(lines) != 0 {
		t.Fatalf("an empty queue rendered %d row(s): %q", len(lines), lines)
	}
	for _, p := range []string{"alpha", "beta"} {
		if _, ok, refusal := q.Add(p); !ok {
			t.Fatalf("Add(%q): %s", p, refusal)
		}
	}
	lines := chatQueueListLines(&q)
	if len(lines) != q.Len()+1 {
		t.Fatalf("the rendering has %d row(s) for %d queued prompt(s), want one header plus one row each: %q", len(lines), q.Len(), lines)
	}
	if !strings.Contains(lines[0], fmt.Sprintf("%d of %d", q.Len(), chatQueueBound)) {
		t.Errorf("the header does not state the depth AND the bound: %q", lines[0])
	}
	for i, p := range q.Items() {
		if !strings.Contains(lines[i+1], fmt.Sprintf("%d. %s", i+1, p)) {
			t.Errorf("row %d does not read as `%d. %s`: %q", i+1, i+1, p, lines[i+1])
		}
	}
}

// TestChatQueue_SeamRefusalIsEditorOnly pins the carrier gate on the ONE
// predicate the dispatcher consults: a run whose input layer is not the
// interactive editor cannot carry a parked prompt into a turn, so it is REFUSED
// (named, with the reason) rather than accepted into a queue nothing consumes.
//
// The negative control is the cooked source itself: if this predicate answered
// "" for a pipe, `/queue` would silently accept prompts in exactly the runs that
// can never run them.
func TestChatQueue_SeamRefusalIsEditorOnly(t *testing.T) {
	cooked, closeCooked := newChatInputCooked(strings.NewReader(""), io.Discard)
	defer closeCooked()
	if cooked.Mode() != chatInputCooked {
		t.Fatalf("the fixture is in mode %q, want %q", cooked.Mode(), chatInputCooked)
	}
	refusal := chatQueueSeamRefusal(cooked)
	if refusal == "" {
		t.Fatal("a cooked run was allowed to queue: it reads one line per caller push, so a parked prompt could never become a turn")
	}
	if !strings.Contains(refusal, chatFlagNoEditor) {
		t.Errorf("the seam refusal does not name the mode that cannot carry a queued turn (%s): %q", chatFlagNoEditor, refusal)
	}
	// The positive control: the predicate is about the EDITOR the dispatcher
	// checks for, and a live editor-backed input must be allowed to queue — a
	// predicate that refused everything would pass the leg above and break the
	// feature in every run that CAN carry a parked prompt. A nil input is the
	// degenerate third case (no input layer at all), and it is refused for the
	// same reason a pipe is.
	// The scriptSource fixture is this package's existing scripted line source
	// (lineedit_wire_test.go), and it is EMPTY on purpose: the predicate reads
	// the input layer's mode (which editor is installed) and never touches the
	// source, so nothing has to be fed. Going through the real constructor keeps
	// the control from drifting away from production's own construction.
	editor := newChatInputWithSource(&scriptSource{}, io.Discard, chatPrompt)
	if editor.Mode() != chatInputEditor {
		t.Fatalf("the editor fixture is in mode %q, want %q", editor.Mode(), chatInputEditor)
	}
	if refusal := chatQueueSeamRefusal(editor); refusal != "" {
		t.Fatalf("an editor-backed run was refused: %q — the seam predicate must accept exactly the mode that has a turn boundary", refusal)
	}
	if refusal := chatQueueSeamRefusal(nil); refusal == "" {
		t.Fatal("a run with no input layer at all was allowed to queue")
	}
}
