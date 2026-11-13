package lineedit

import "sync"

// bandgate.go — the editing-model READ hook, and the guard it exists for.
//
// The invariant this package states is that the READ LOOP is the layout's only
// writer and the editing model's only reader: Editor.Run mutates and reads
// Value, and every other goroutine that wants a row on the band QUEUES it
// (Editor.WriteLines / Editor.WriteBandRow → drainBand). The defect measured on
// 2026-09-28 (26 `WARNING: DATA RACE` blocks in ONE run of cmd/agent's
// `go test -race -run TestChatSurfaceKey_`) was that the flow seam performed the
// band write AND e.repaint() itself — and repaint reads the model
// (clampedMenu → Value.RowsFor → Value.Text) — while Run's Value.Reset() wrote
// it from the read loop's goroutine.
//
// A -race report proves that race after the fact, on a sampled interleaving, and
// only under the race detector. The hook below makes the same property
// observable DETERMINISTICALLY and without -race: Value's reads report
// themselves, so a guard can arm a sentinel, park the read loop on a gate (no
// loop-side read can fire it), call the seam from a foreign goroutine, and
// require the sentinel to stay untouched. A read there is the defect stated in
// the invariant's own terms, whatever the scheduler did.
//
// It is a nil check in production, never a behavioural branch, and it is the
// same shape as parkgate.go's acknowledge-window hook — one seam per guarded
// window, both announced (Rule 15: a test-only seam that is live is REPORTED,
// never silently active — see BandModelReadHookEnabled).
var (
	bandReadHookMu sync.Mutex
	bandReadHook   func()
)

// setBandModelReadHook installs (or clears) the editing-model read hook. Tests
// only.
func setBandModelReadHook(h func()) {
	bandReadHookMu.Lock()
	defer bandReadHookMu.Unlock()
	bandReadHook = h
}

// runBandModelReadHook runs the installed hook, if any. It is called from
// Value's reads.
func runBandModelReadHook() {
	bandReadHookMu.Lock()
	h := bandReadHook
	bandReadHookMu.Unlock()
	if h != nil {
		h()
	}
}

// BandModelReadHookEnabled reports whether a hook is installed. Production
// reports false; a guard's own precondition asserts it is wired when the guard
// wants the deterministic observation.
func BandModelReadHookEnabled() bool {
	bandReadHookMu.Lock()
	defer bandReadHookMu.Unlock()
	return bandReadHook != nil
}
