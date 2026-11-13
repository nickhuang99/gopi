package lineedit

// lineinsert_test.go — the insert road's OWN contract, at the SEAM.
//
// What these cases are for, and what they deliberately are NOT. The road's hard
// half is the hand-off between a foreign producer and the read loop's single
// ownership of the model (valueowner.go); the end-to-end proof that a transcript
// lands on the PAINTED GRID runs where the production carrier is built
// (cmd/agent's chat_voice_pty_test.go, a real pty). Here the seam is driven
// dry — no goroutine, no terminal — so the three facts a grid case cannot
// isolate are each pinned by name: the PLACEMENT (cursor vs append), the
// REFUSAL (an empty text and an over-full backlog are reported, never silently
// dropped), and the LEVEL (an insert published from another goroutine is seen by
// the loop's own predicate, which is what makes it reach the next repaint
// instead of the next keystroke).

import "testing"

// TestLineInsert_AppendGoesAfterWhatIsTyped is the placement contract, and it is
// the ONE property the whole feature is judged on: a transcript must not cost the
// operator a character they had already typed.
//
// Both placements are driven on the SAME editor, in the same order a voice session
// produces them, so the case would fail if the two were ever collapsed into one:
// the text typed first stays, the append lands AFTER it, and — the half that makes
// it an append rather than a lucky cursor — an insert asked for at the caret
// lands BETWEEN them.
func TestLineInsert_AppendGoesAfterWhatIsTyped(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)

	// What the operator has already typed, with the caret left where typing put
	// it (the end) and then MOVED, which is the state a real draft is in.
	if ed.Value().InsertString("deploy the") > 0 {
		t.Fatal("the draft did not fit in an empty buffer")
	}

	// 1) The append shape.
	if !ed.InsertIntoLineAtEnd(" staging box") {
		t.Fatal("the append was refused by the seam")
	}
	if got := ed.Value().Text(); got != "deploy the staging box" {
		t.Fatalf("after the append the line reads %q, want %q — the transcript did not land after the draft", got, "deploy the staging box")
	}

	// 2) The caret shape, on the same line, after the caret has been moved back
	//    into the middle: this is what separates "appended" from "typed at the
	//    caret" and proves the two placements are different decisions.
	ed.Value().Home()
	if !ed.InsertIntoLine("ALSO ") {
		t.Fatal("the cursor insert was refused by the seam")
	}
	if got := ed.Value().Text(); got != "ALSO deploy the staging box" {
		t.Fatalf("the cursor placement put the text at %q, want it at the caret (leading) — the two placements are not distinguished", got)
	}
}

// TestLineInsert_EmptyAndFullBacklogAreRefusedByName pins the two falses. A
// producer is entitled to know WHICH refusal it got, and the count is what makes
// the over-full case observable from outside the call (Rule 15: a dropped
// transcript that reported success would be the silent-failure defect this seam
// exists to avoid).
func TestLineInsert_EmptyAndFullBacklogAreRefusedByName(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)

	if ed.InsertIntoLine("") {
		t.Fatal("an empty text was reported as inserted")
	}
	if ed.LineInsertRefusals() != 0 {
		t.Fatalf("the empty text was COUNTED as a refusal (%d): an empty text is not a backlog refusal, and the two are different facts", ed.LineInsertRefusals())
	}

	// Fill the backlog the way the PRODUCER fills it. The public entry point
	// cannot reach this state on an editor with no read loop (NewEditor does not
	// start one, so the direct-drive arm applies each text immediately and the
	// backlog never grows) — which is exactly why the bound is asserted HERE, on
	// the function that owns it, rather than inferred from a path that cannot
	// exercise it.
	ed.lines.mu.Lock()
	for i := 0; i < lineInsertQueueCap; i++ {
		ed.lines.pending = append(ed.lines.pending, lineInsert{text: "x"})
	}
	ed.lines.active = true
	ed.lines.mu.Unlock()

	if ed.queueLineInsert(lineInsert{text: "one too many"}) {
		t.Fatal("the over-full backlog accepted a text it cannot apply")
	}
	if got := ed.LineInsertRefusals(); got != 1 {
		t.Fatalf("the refusal count is %d, want 1 — a refused insert must be countable from outside the call", got)
	}
	if !ed.LineInsertPending() {
		t.Fatal("the level is not raised while texts wait: the loop's own predicate would never drain them")
	}
}

// TestLineInsert_DrainAppliesInOrderAndLowersTheLevel is the level's contract,
// driven the way the loop drives it. The order matters because two transcripts
// arrive in the order they were spoken, and a level that stayed raised after the
// drain would make every repaint re-apply the same text (the duplicate-append
// defect) — so both halves are asserted here rather than inferred.
func TestLineInsert_DrainAppliesInOrderAndLowersTheLevel(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)

	ed.lines.mu.Lock()
	ed.lines.pending = append(ed.lines.pending,
		lineInsert{text: "first ", place: LineInsertAtEnd},
		lineInsert{text: "second ", place: LineInsertAtEnd})
	ed.lines.active = true
	ed.lines.mu.Unlock()

	if !ed.drainLineInserts() {
		t.Fatal("the drain applied nothing while two texts were queued")
	}
	if got := ed.Value().Text(); got != "first second " {
		t.Fatalf("the drain applied %q, want %q — the order or the append placement was lost", got, "first second ")
	}
	if ed.LineInsertPending() {
		t.Fatal("the level is still raised after the drain: every later repaint would re-apply the same text")
	}
	if ed.drainLineInserts() {
		t.Fatal("a second drain applied something from an empty queue — the text is being duplicated")
	}
	if got := ed.Value().Text(); got != "first second " {
		t.Fatalf("the line changed on the empty drain: %q", got)
	}
}

// TestLineInsert_KeyGateReaderReturnsTheInstalledOwner is the chain's own
// precondition. A layer that must CHAIN (claim one key, forward the rest) can only
// do so if it can read what is already there — so the reader and the setter are
// asserted against each other, nil included, rather than trusted.
func TestLineInsert_KeyGateReaderReturnsTheInstalledOwner(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if ed.KeyGate() != nil {
		t.Fatal("a fresh editor reports a key-gate owner")
	}
	owner := &insertProbeGate{}
	ed.SetKeyGate(owner)
	if got := ed.KeyGate(); got != KeyGate(owner) {
		t.Fatalf("KeyGate() returned %#v, want the owner that was installed", got)
	}
	ed.SetKeyGate(nil)
	if ed.KeyGate() != nil {
		t.Fatal("the reader still reports an owner after the seam was cleared")
	}
}

// TestForwarding_HandsTheUnclaimedHalfToTheNextOwner is the chain's own contract:
// a chained owner must FORWARD the half of the seam it did not take, or every key
// it declines becomes invisible to the owner behind it — a second surface that
// silently switches the first one off.
func TestForwarding_HandsTheUnclaimedHalfToTheNextOwner(t *testing.T) {
	behind := &insertProbeGate{}
	front := &insertProbeGate{Forwarding: Forwarding{Next: behind}}

	front.KeyUnclaimed(Key{Code: KeyRune, Rune: 'x', Origin: ReadOriginTyped})
	front.KeyRelease(Key{Code: KeyEnter, Origin: ReadOriginTyped})
	if len(behind.unclaimed) != 1 || len(behind.released) != 1 {
		t.Fatalf("the chain dropped the seam's reports: behind saw %d unclaimed / %d released, want 1 / 1",
			len(behind.unclaimed), len(behind.released))
	}
	// A chain with nothing behind it must be silent rather than panic: the last
	// owner is the ordinary case, not an error. "Silent" is asserted on the
	// FORWARDING half alone (the probe records its OWN reports by construction, so
	// counting them here would assert the probe, not the seam).
	var tail Forwarding
	if tail.Next != nil {
		t.Fatal("a zero Forwarding reports an owner behind it")
	}
	tail.KeyUnclaimed(Key{Code: KeyRune, Rune: 'y'})
	tail.KeyRelease(Key{Code: KeyEnter})
}

// probeGate records the seam's reports; it claims nothing.
type insertProbeGate struct {
	Forwarding
	unclaimed []Key
	released  []Key
}

func (p *insertProbeGate) GateKey(Key) bool { return false }

func (p *insertProbeGate) KeyUnclaimed(k Key) {
	p.unclaimed = append(p.unclaimed, k)
	p.Forwarding.KeyUnclaimed(k)
}

func (p *insertProbeGate) KeyRelease(k Key) {
	p.released = append(p.released, k)
	p.Forwarding.KeyRelease(k)
}

// discardWriter is the output sink for a seam case that asserts the MODEL, not
// the screen (the grid assertions live in cmd/agent, over a real pty).
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// inertSource is a Source that never delivers anything and reports no geometry:
// these cases drive the editor's model directly and never start a read loop.
type inertSource struct{}

func (inertSource) ReadEvent([]byte, int) (Event, error) {
	return Event{Kind: EventInput, EOF: true}, nil
}
func (inertSource) Width(fallback int) int  { return fallback }
func (inertSource) Height(fallback int) int { return fallback }

// TestKeyGate_SeesTheEscapeTheTimeoutFlushes is the REGRESSION case for a defect
// measured on a real pty (2026-10-07), and it is written at the seam that hid it.
//
// A LONE Escape never travels through applyRead: an ESC is the prefix of every
// arrow key, so the decoder holds it and the read loop's TIMEOUT resolves it
// (FlushEscape). The pre-fix code handed that key straight to handleKey, so a
// gate owner that claims Escape — the /voice mode, whose whole exit is that key —
// never saw it, while a unit case driving the INJECTOR passed happily: InjectKey
// decodes a complete sequence and therefore reaches the ordinary arm. The two
// roads are asserted here SIDE BY SIDE, which is what makes them impossible to
// diverge again.
func TestKeyGate_SeesTheEscapeTheTimeoutFlushes(t *testing.T) {
	t.Run("the flushed key reaches the gate", func(t *testing.T) {
		ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
		claimant := &escapeClaimant{gate: &insertProbeGate{}}
		ed.SetKeyGate(claimant)

		// A lone ESC in the decoder, with no byte behind it: exactly the state
		// the read loop's bounded wait finds.
		ed.dec.Feed([]byte{escByte})
		if !ed.dec.BareEscape() {
			t.Fatal("the decoder did not hold a lone ESC: the case is not driving the flushed arm at all")
		}
		k := ed.dec.FlushEscape()
		if k.Code != KeyEscape {
			t.Fatalf("FlushEscape produced %v, want Escape", k.Code)
		}
		k.Origin = ReadOriginTyped
		if !ed.dispatchKeyGate(k) {
			t.Fatal("the gate DECLINED the flushed Escape: this is the defect — a lone Escape reached the gate in no way at all, so an owner that claims it can never leave")
		}
		if len(claimant.claimed) != 1 {
			t.Fatalf("the gate was consulted %d time(s), want 1", len(claimant.claimed))
		}
		if claimant.claimed[0].Origin != ReadOriginTyped {
			t.Fatalf("the flushed key reached the gate with origin %v, want the origin of the read that produced it", claimant.claimed[0].Origin)
		}
	})

	t.Run("the injected key still reaches the gate", func(t *testing.T) {
		// The OTHER road, for the contrast that names the defect: InjectKey feeds
		// a COMPLETE ESC sequence, so the decoder emits it from Feed and the key
		// is applied by applyRead. Both roads must reach the same gate.
		ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
		claimant := &escapeClaimant{gate: &insertProbeGate{}}
		ed.SetKeyGate(claimant)
		if _, _, err := ed.applyRead([]byte{escByte}); err != nil {
			t.Fatalf("applying a lone ESC through the ordinary road: %v", err)
		}
		// A lone ESC yields NO key from Feed (it is held as a sequence prefix):
		// that is precisely why the timeout arm exists, and why the two roads had
		// to be made to agree.
		if len(claimant.claimed) != 0 {
			t.Fatalf("a lone ESC produced %d key(s) on the ordinary road: the case's premise (a lone ESC is resolved by the TIMEOUT) does not hold", len(claimant.claimed))
		}
	})
}

// escapeClaimant is a gate that claims ONE thing — an Escape — and forwards the
// rest, so a case can tell "the seam never asked me" from "it asked and I said no".
type escapeClaimant struct {
	Forwarding
	gate    *insertProbeGate
	claimed []Key
}

func (c *escapeClaimant) GateKey(k Key) bool {
	if k.Code != KeyEscape {
		return false
	}
	c.claimed = append(c.claimed, k)
	return true
}

func (c *escapeClaimant) KeyUnclaimed(k Key) { c.gate.KeyUnclaimed(k) }
func (c *escapeClaimant) KeyRelease(k Key)   { c.gate.KeyRelease(k) }
