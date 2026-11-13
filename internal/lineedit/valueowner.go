package lineedit

import "sync/atomic"

// valueowner.go — WHO EDITS Value, and what every other reader gets.
//
// THE INVARIANT. `Value` has ONE writer while a read loop runs: the goroutine
// inside `Editor.Run` (its drains and repaints are its own work on its own
// goroutine). With no loop at all — a direct-drive caller: every unit test in this
// package, and any caller wiring an editor by hand — the writer is the single
// goroutine driving the buffer, which is the same discipline one step earlier.
//
// THE READ ROAD. A mutation PUBLISHES: `Value.tick` copies the buffer into one
// atomic slot, so the slot always holds the contents as of the last completed
// mutation. Every read is then served by that copy — `Text`, and therefore `Len`,
// `Cursor`, `Lines` and `RowsFor`, which are defined in terms of it — with ONE
// exception: a read taken on the read loop's own goroutine reads the live fields,
// because the loop is the only writer and the repaint reads the model it just
// edited.
//
// WHY NOT A LOCK. Rule 15. A mutex over the buffer would leave the two goroutines'
// passes interleaved exactly as before — one goroutine's splice landing inside
// another's read of the same runes — and merely stop the detector from seeing it.
// The fields are therefore unguarded on purpose; the only atomic in this design
// holds the snapshot, which is data two goroutines legitimately share.
//
// WHY THE READ ROAD IS NOT DECIDED BY "WHO BUILT IT". Two wrong answers were
// measured on the road to this one, both in this task's own acceptance loop
// (`go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_' -count=1`), and both are
// the same mistake seen from two sides: giving a goroutine ownership of the buffer
// because it constructed the editor around it.
//
//   - A per-Run owner (cleared when `Run` returned) left the model ownerless
//     BETWEEN two lines — `ReadLine` calls `Run` once per line — exactly when the
//     caller polls. 3 runs of 4, `Value.Reset` vs `Value.Text`.
//   - Keeping the constructing goroutine as a fallback owner left the same window
//     at the OTHER end: before the loop's first `Run` noted itself, the caller that
//     built the editor was the owner and read the live fields while the loop
//     entered `Reset`. 2 consecutive runs, 4 blocks each, same pair.
//
// The measured pair, from master `9aba625b`, is what this closes:
//
//	Write  at 0x00c000a121b0 by goroutine 73:
//	  lineedit.(*Value).Reset()             value.go:108
//	  lineedit.(*Editor).Run()              editor.go:1204
//	  cmd/agent.(*chatInput).ReadLine()     lineedit_wire.go:165   <- the READ LOOP
//	Previous read at 0x00c000a121b0 by goroutine 61:
//	  lineedit.(*Value).Text()              value.go:85
//	  cmd/agent.keyE2EEditorText()          chat_surface_key_e2e_test.go:200
//
// The census that made this the right shape: EVERY production reader of the model
// is already loop-side (Editor.repaint, clampedMenu, paintMenu, submit,
// handleKey — all reached from Run):
//
//	grep -rn "e\.val\.\|\.Value()\." --include=*.go internal cmd | grep -v _test
//
// The one foreign reader was the harness's own buffer poll — "has the run's editor
// taken the typed line yet?". So the design keeps the loop's hot path exactly as it
// was (no copy per read) and makes EVERY other read structurally incapable of
// touching the live buffer, without asking the caller which goroutine it is on.

// valueLoopGoid is a test seam: it reports the read-loop identity the VALUE layer
// should use. Production leaves it nil — the live answer is the buffer's own
// recorded loop (noteValueLoop). It exists because a `Value` built alone has no
// editor to ask, and the guard has to be able to state "a read loop owns this
// buffer" without building a whole editor around it: the same shape as
// bandgate.go's read hook and parkgate.go's acknowledge window, both announced
// rather than silently active (Rule 15).
var valueLoopGoid atomic.Int64

// noteValueLoop records the read loop's identity on the buffer, so a read can tell
// whether it is on the loop.
//
// The identity is STICKY — a zero id is ignored — and that is a measured
// requirement rather than a preference: `ReadLine` calls `Run` ONCE PER LINE, so
// `Run` returns between lines and any owner cleared on that return would leave the
// model ownerless exactly while a caller waits for the next line. The buffer keeps
// the loop that owns it: ownership is a LIFETIME fact, not a per-Run one. The
// per-Run fact stays where it belongs (`Editor.loopGoid`, consulted fresh by each
// audience decision).
func (v *Value) noteValueLoop(id int64) {
	if v == nil || id == 0 {
		return
	}
	v.loopGoid.Store(id)
}

// ownerGoroutine is the read loop that has taken this buffer up: zero while none
// has.
//
// Zero is the answer that matters, and it has TWO shapes which the two sides of
// this file treat differently on purpose. A buffer under an editor that no read
// loop has reached yet is the boot window — a caller may be about to start one, so
// nobody may read the live fields (the measured second window above). A buffer
// built alone has no loop at all and will never get one — a direct-drive value —
// and its single goroutine is the only writer there is, so its mutations must
// publish or its own reads would go stale (measured 2026-09-28: with nothing
// publishing, `TestA5B_ScrollRegionNeverMovesTheChrome` read an empty copy of a
// buffer the test had typed into).
//
// Both are served by the same two rules below: the WRITER publishes whenever no
// owner exists or the caller IS the owner, and the only road that reads the live
// fields is the owner's own.
func (v *Value) ownerGoroutine() int64 {
	if id := v.loopGoid.Load(); id != 0 {
		return id
	}
	return valueLoopGoid.Load()
}

// publish stores the buffer's contents as of this completed mutation and reports
// whether the CALLER may read the live fields.
//
// Every mutation calls it through `Value.tick`, so "did you remember to publish?"
// has no call site to get wrong (Rule 14), and the copy is fresh EVERY time: a
// foreign reader holds the snapshot it loaded — the string under its own call stack
// for as long as it uses it — and reusing the array would race the very reader this
// exists for. A splice that does not overflow the capacity moves the SAME backing
// array (spliceRunes → copy), so a snapshot holding that array would be a window
// rather than a copy. One memmove on the writer's goroutine, beside an edit that
// already moved the same bytes.
func (v *Value) publish() {
	// A CALLER THAT IS NOT THE OWNER STORES NOTHING — and touches nothing: the
	// owner could be mid-splice in v.runes and mid-motion in v.pos, and a reader
	// that stored would be a second writer to the very slot it came to read,
	// overwriting a publication produced by the layer above it (measured
	// 2026-09-28 in this task: publishing from both roads put `Value.Text` on the
	// harness's goroutine back against the loop's `Value.spliceRunes`, 4–7 blocks a
	// run).
	if owner := v.ownerGoroutine(); owner != 0 && goid() != owner {
		return
	}
	// The writer — the read loop, or the single goroutine of a value no loop has
	// taken up. Publish the buffer as it stands.
	//
	// The copy is fresh EVERY time, never the previous snapshot's storage: a reader
	// holds the snapshot it loaded (the string under its own call stack for as long
	// as it uses it), and overwriting that array in place would race the very
	// reader this exists for. One memmove on the writer's goroutine, beside an edit
	// that already moved the same bytes.
	v.pub.Store(valueSnapshot{pos: v.pos, runes: append([]rune(nil), v.runes...), voice: v.copyVoice()})
}

// snapshot returns the last published contents. It never touches the live buffer,
// which is what a caller that is not the read loop is allowed to read.
func (v *Value) snapshot() valueSnapshot {
	s, _ := v.pub.Load().(valueSnapshot)
	return s
}

// read reports which road a READ from the calling goroutine takes, and the
// contents it is served. `Text`/`Len`/`Cursor` go through it, and the tests state
// the road with it (valueowner_foreign_test.go).
//
// It is the ONE door, and that is deliberate: the road a reader takes and the
// storage it reads are the same decision, so a caller can never read the buffer by
// taking the copy's name (or the reverse).
//
// Only the OWNER reads the live fields. With no owner recorded the answer is the
// published copy — which is the writer's own last publication when the caller is
// the only goroutine there is (see ownerGoroutine), so a direct-drive caller still
// reads what it just wrote.
func (v *Value) read() (string, valueSnapshot) {
	if owner := v.ownerGoroutine(); owner != 0 && goid() == owner {
		return valueRoadOwner, valueSnapshot{pos: v.pos, runes: v.runes, voice: v.voice}
	}
	return valueRoadSnapshot, v.snapshot()
}

const (
	// valueRoadOwner is the road a read takes on the read loop's own goroutine:
	// the live buffer, with no copy and no snapshot in the way.
	valueRoadOwner = "owner"
	// valueRoadSnapshot is the road a read takes anywhere else: the published
	// copy of the buffer as of the last completed mutation.
	valueRoadSnapshot = "snapshot"
)
