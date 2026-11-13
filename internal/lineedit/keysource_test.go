package lineedit

// keysource_test.go — the two facts of keysource.go, asserted where they are
// decidable: WHICH ARM a read came from, and WHO got the key first.
//
// WHY THE GRID IS NOT THE JUDGE HERE. The acceptance cases in inject_test.go
// judge a whole interaction on the painted cell grid, because the interaction is
// what they are about. These cases are about a SEAM: the claim is "a key's bytes
// and its provenance are separate facts, and an owner can consume a key before
// the editor applies it". Both are observable as data — the gate's own record and
// the editor's buffer — so they are asserted as data, and the grid is used only
// for the negative half (a consumed key really did not reach the line).

import (
	"sync"
	"testing"
	"time"
)

// gateWait bounds a wait for the read loop to reach the gate. It is a deadline
// that turns a hang into a dump, never the assertion.
const gateWait = 3 * time.Second

// gateKind names which of the KeyGate's three callbacks fired.
type gateKind string

const (
	gateConsumed  gateKind = "consumed"  // GateKey said yes: the editor did NOT apply the key
	gateUnclaimed gateKind = "unclaimed" // the editor has no handler for it either
	gateReleased  gateKind = "released"  // the editor applied it
)

// gateRecord is one callback, in arrival order.
type gateRecord struct {
	kind gateKind
	key  Key
}

// recordingGate is a KeyGate that records every callback and consumes whatever
// its predicate says. It stands in for the replay's control layer, which is the
// production consumer of this seam.
type recordingGate struct {
	mu      sync.Mutex
	recs    []gateRecord
	consume func(Key) bool
}

func (g *recordingGate) note(kind gateKind, k Key) {
	g.mu.Lock()
	g.recs = append(g.recs, gateRecord{kind: kind, key: k})
	g.mu.Unlock()
}

func (g *recordingGate) GateKey(k Key) bool {
	if g.consume != nil && g.consume(k) {
		g.note(gateConsumed, k)
		return true
	}
	return false
}

func (g *recordingGate) KeyUnclaimed(k Key) { g.note(gateUnclaimed, k) }
func (g *recordingGate) KeyRelease(k Key)   { g.note(gateReleased, k) }

// snapshot copies the records taken so far.
func (g *recordingGate) snapshot() []gateRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gateRecord(nil), g.recs...)
}

// waitFor records polls until n callbacks have been recorded, and returns them.
func (g *recordingGate) waitFor(t *testing.T, n int) []gateRecord {
	t.Helper()
	deadline := time.Now().Add(gateWait)
	for {
		recs := g.snapshot()
		if len(recs) >= n {
			return recs
		}
		if time.Now().After(deadline) {
			t.Fatalf("the key seam recorded %d callback(s) within %s, want %d: %+v", len(recs), gateWait, n, recs)
		}
		time.Sleep(injectPollStep) //stylegate:allow-sleep bounded poll of the key seam's recorded callbacks; the injector exposes no completion signal
	}
}

// waitLine polls until the editor's buffer holds want (the read loop owns the
// buffer, so the test must wait for its own keystroke to land).
func (h *injectHarness) waitLine(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(gateWait)
	for {
		if got := h.ed.val.Text(); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the editing line holds %q within %s, want %q\n--- screen ---\n%s",
				h.ed.val.Text(), gateWait, want, h.rows())
		}
		time.Sleep(injectPollStep) //stylegate:allow-sleep bounded poll of the editor buffer the read loop owns; nothing tells the test when its keystroke lands
	}
}

// TestKeyOriginSeparatesAnInjectedReadFromATypedOne is the DISCRIMINATING case
// of the origin half, and it is built the only way the claim can be made: the
// SAME RUNE is delivered twice, once through the injector and once through the
// inner source (the position a terminal occupies), and the two reads must report
// different origins. Byte equality is the point — nothing in the bytes tells the
// two apart, so an implementation that guessed from content or from timing would
// fail this case while passing any single-arm test.
func TestKeyOriginSeparatesAnInjectedReadFromATypedOne(t *testing.T) {
	g := &recordingGate{}
	h := newInjectHarness(t, &stubCompleter{})
	h.ed.SetKeyGate(g)

	// (1) INJECTED: the recording's own typing, through the injector.
	if res := h.inj.InjectBytes("x"); !res.Accepted {
		t.Fatalf("injecting %q was refused: %s", "x", res.Why)
	}
	recs := g.waitFor(t, 1)
	if recs[0].kind != gateReleased {
		t.Fatalf("the injected rune was reported as %q, want %q", recs[0].kind, gateReleased)
	}
	if recs[0].key.Origin != ReadOriginInjected {
		t.Fatalf("the injected read's key carries origin %v, want %v (the injector served it)",
			recs[0].key.Origin, ReadOriginInjected)
	}
	if recs[0].key.Rune != 'x' {
		t.Fatalf("the injected key is %q, want %q", recs[0].key.Rune, 'x')
	}

	// (2) TYPED: the same rune, arriving through the source the editor opened on
	// — the arm a terminal's bytes take.
	h.src.push(scriptStep{data: []byte("x")})
	recs = g.waitFor(t, 2)
	if recs[1].key.Origin != ReadOriginTyped {
		t.Fatalf("a read delivered by the inner source carries origin %v, want %v (the terminal arm)",
			recs[1].key.Origin, ReadOriginTyped)
	}
	// Both keys were applied: the gate declined them, which is what "no owner
	// wanted this key" means.
	h.waitLine(t, "xx")
}

// TestKeyGateConsumesTheKeyBeforeTheEditorAppliesIt is the OTHER half: a key the
// owner claims must not reach the line at all. The gate here claims every SPACE,
// so the line ends up holding the two runs either side of it — and the negative
// evidence is the buffer, which is exactly what a control layer's keys must not
// pollute.
func TestKeyGateConsumesTheKeyBeforeTheEditorAppliesIt(t *testing.T) {
	g := &recordingGate{consume: func(k Key) bool { return k.Code == KeyRune && k.Rune == ' ' }}
	h := newInjectHarness(t, &stubCompleter{})
	h.ed.SetKeyGate(g)

	for _, s := range []string{"a", " ", "b"} {
		if res := h.inj.InjectBytes(s); !res.Accepted {
			t.Fatalf("injecting %q was refused: %s", s, res.Why)
		}
	}
	recs := g.waitFor(t, 3)
	if recs[1].kind != gateConsumed {
		t.Fatalf("the claimed key was reported as %q, want %q", recs[1].kind, gateConsumed)
	}
	if recs[1].key.Rune != ' ' || recs[1].key.Origin != ReadOriginInjected {
		t.Fatalf("the claimed key is %q/%v, want ' '/%v", recs[1].key.Rune, recs[1].key.Origin, ReadOriginInjected)
	}
	// THE CONSUMED KEY IS ABSENT FROM THE LINE: the claim is that the editor did
	// not apply it, and the line is the only witness of that.
	h.waitLine(t, "ab")
}

// TestKeyGateIsANoOpWhenNoneIsInstalled is the default half of the seam, and it
// is the half that protects every existing surface: with no owner installed the
// editor must behave exactly as it did before the seam existed. The check is a
// key that lands on the line — the same observable the consuming case uses, so
// "nothing changed" and "the key was consumed" cannot be confused.
func TestKeyGateIsANoOpWhenNoneIsInstalled(t *testing.T) {
	h := newInjectHarness(t, &stubCompleter{})
	h.ed.SetKeyGate(nil) // explicitly: the default, and it must be indistinguishable from it
	if res := h.inj.InjectBytes("z"); !res.Accepted {
		t.Fatalf("injecting %q was refused: %s", "z", res.Why)
	}
	h.waitLine(t, "z")
}

// TestKeyGateHearsAKeyTheEditorIgnores pins the THIRD callback: a decoded key
// with no handler (the decoder's Alt-modified rune is the one shape the read loop
// passes by) is reported as UNCLAIMED rather than silently vanishing, so an owner
// that wants such a key can take it while an owner that does not is told the run
// never saw it.
func TestKeyGateHearsAKeyTheEditorIgnores(t *testing.T) {
	g := &recordingGate{}
	h := newInjectHarness(t, &stubCompleter{})
	h.ed.SetKeyGate(g)

	// ESC followed by a rune in ONE read is the bare-Alt shape ("not a binding
	// we own"), and it arrives from the terminal arm.
	h.src.push(scriptStep{data: []byte("\x1bq")})

	recs := g.waitFor(t, 1)
	if recs[0].kind != gateUnclaimed {
		t.Fatalf("the ignored key was reported as %q, want %q (origin %v)", recs[0].kind, gateUnclaimed, recs[0].key.Origin)
	}
	if recs[0].key.Code != KeyRune || !recs[0].key.Alt {
		t.Fatalf("the ignored key is %+v, want an Alt-modified rune", recs[0].key)
	}
	if recs[0].key.Origin != ReadOriginTyped {
		t.Fatalf("the ignored key carries origin %v, want %v", recs[0].key.Origin, ReadOriginTyped)
	}
}
