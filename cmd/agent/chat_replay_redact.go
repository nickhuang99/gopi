// chat_replay_redact.go — the redaction pass for the chat replay route.
//
// WHY THIS FILE EXISTS. `gopi chat --replay` films the REAL chat surface, and a
// real answer's own text carries identifying runs: the operator's `~` is not
// expanded on the wire (`/home/user/…`), a session is named by its uuid, a
// sandbox by its record. The renderer route (`gopi replay`) has masked those
// runs before any pixel existed since it landed; this route drew them, and
// tools/replay/verify.py's fourth self-check said so in as many words — "no
// redaction log at <scratch>/redactions.tsv" — with a follow-up requirement
// either way: a POSITIVE CONTROL (at least one run actually replaced, so the
// withholding claim is not vacuous) and CONTAINMENT (no recorded run in any
// delivered artifact).
//
// THE DEFINITION IS NOT HERE. The shapes, the length-preserving substitution, the
// log format and the log's name are internal/replay's (redact.go), ONE definition
// exported for both producers (Rule 14): a shape added for the renderer is
// withheld on this route at the same moment, and the two routes cannot disagree
// about what they withhold while each route's own check stays green.
//
// WHERE THE MASK SITS, AND WHY IT IS THERE. It runs at the SEAM, on the bytes the
// surface is about to be handed — before `renderer.Write(delta)` draws them and
// before the beat's ledger row is cut from the same text. So the film's pixels
// and the ledger's rows are the SAME masked text by construction, which is what
// verify.py's traceability check compares (it re-applies the same substitution to
// the source line): a mask applied downstream of one of the two would make them
// differ by exactly the withheld run.
//
// THE STREAM IS THE HARD PART, and the tail-hold is the honest answer to it. A
// shape can straddle two fragments of one stream: the stub splits a text block
// in half, and a uuid or a path in the middle of a sentence lands half in one
// delta and half in the next. Masking each delta alone passes BOTH halves (8-4-4
// is not 8-4-4-4-12) and paints the head of the identifier the next delta
// completes — a leak that a shape-based check cannot see either, because it looks
// for whole shapes. Holding the last maxIdentShapeRunes runes back is sufficient
// AND provably so: a shape that started in the released text and ended in the
// held text would be longer than 36 runes, and no shape is. The hold costs 36
// runes of latency, released on the next delta or at the turn's end.

package main

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/replay"
)

// maskText masks one COMPLETE text (a typed line, a tool-activity row). A
// fragment that is knowingly part of a stream goes through maskAnswerStream
// instead: this one judges what it is handed, which is only safe when nothing
// follows.
//
// It is nil-safe, so every seam can call it unconditionally: a run without the
// beat protocol has no driver and no pass, and gets its own bytes back.
func (d *chatReplayBeatDriver) maskText(s string) string {
	if d == nil {
		return s
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out, _ := d.red.Mask(s)
	return out
}

// maskRow is maskText under the driver's own lock, and it is the form the LINE
// LAYER takes (chatInput.redact): a row written by the layer itself — a notice,
// a panel — is masked on its way to the band, so a run's own chrome cannot carry
// an identifying run into a filmed frame. The lock is not optional: the log's
// rows must be ordered against the beats they belong to, and the layer writes
// from the same goroutine that emits beats.
func (d *chatReplayBeatDriver) maskRow(s string) string { return d.maskText(s) }

// maskAnswerStream feeds ONE answer delta into the turn's stream and returns the
// chunks that may now be drawn and recorded. The return is what the seams hand to
// BOTH consumers, so the film and the ledger cannot see different text.
//
// The mask is applied to the WHOLE pending stream (held + delta) and the prefix
// that no shape can still reach is released; the rest is held (see the file
// comment for why maxIdentShapeRunes is the right hold).
func (d *chatReplayBeatDriver) maskAnswerStream(delta string) []string {
	if d == nil {
		if delta == "" {
			return nil
		}
		return []string{delta}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if delta != "" {
		d.pending += delta
	}
	if d.pending == "" {
		return nil
	}
	masked, _ := d.red.Mask(d.pending)
	rs := []rune(masked)
	if len(rs) <= replay.MaxIdentShapeRunes() {
		// Everything received could still grow a shape: nothing is shown yet. It
		// is released by the next delta or by flushAnswerHold at the turn's end.
		return nil
	}
	cut := len(rs) - replay.MaxIdentShapeRunes()
	d.pending = string(rs[cut:])
	return []string{string(rs[:cut])}
}

// flushAnswerHold releases whatever is still held. The turn's end is the moment a
// stream is known to be complete, so this is where the tail stops being held —
// and it must be called BEFORE the renderer commits the answer's tail, or the
// last runes of the answer would never be drawn at all (the hold is on the way to
// BOTH consumers, not only on the ledger's row).
//
// It reports an error only when a returned chunk could not be handed on; the
// caller reports it rather than swallowing it (Rule 15).
func (d *chatReplayBeatDriver) flushAnswerHold(draw func(string) error) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	held := d.pending
	d.pending = ""
	d.mu.Unlock()
	if held == "" {
		return nil
	}
	return draw(held)
}

// closeRedaction persists the substitution log. It is called from the driver's
// own close, so a run that ends normally leaves a complete log behind; the file
// itself is the record verify.py's positive control reads.
func (d *chatReplayBeatDriver) closeRedaction() error {
	if d == nil || d.red == nil {
		return nil
	}
	return d.red.Flush()
}

// redactionSummary is the sentence the run prints about its own pass. A run that
// replaced nothing says so explicitly: verify.py treats an empty log as a VACUOUS
// withholding claim, and a silent zero would leave an operator unable to tell
// "nothing identifying was there" from "the pass never ran" (Rule 15).
func redactionSummary(d *chatReplayBeatDriver, logPath string) string {
	if d == nil || d.red == nil {
		return ""
	}
	n := d.red.Masks()
	if n == 0 {
		return fmt.Sprintf(chatReplayRedactionNone, logPath)
	}
	return fmt.Sprintf(chatReplayRedactionDone, n, logPath)
}

// The two sentences the run prints about its own pass. They are named constants so
// the wording is stated once, and both END by naming the log: the file is the
// record, and a reader has to be able to find it.
//
// WHY THE ZERO CASE IS NOT SILENCE. tools/replay/verify.py's withholding check
// treats an empty log as a VACUOUS claim and fails it ("the redaction pass
// replaced NOTHING"), and an operator watching a run cannot tell a silent zero
// from a pass that never ran. So the run says which of the two happened.
const (
	chatReplayRedactionDone = chatReplayNotice + "redacted %d identifying run(s) before the surface drew them — recorded in %s\n"
	chatReplayRedactionNone = chatReplayNotice + "the redaction pass replaced NOTHING: no identifying run reached this run's text — the log at %s is empty, which verify.py reads as a vacuous withholding claim\n"
)
