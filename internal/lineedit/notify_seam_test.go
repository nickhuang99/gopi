package lineedit

import "testing"

// notify_seam_test.go — the wake seam's RESOLUTION (SourceNotifier).
//
// The defect this pins is a silent one and it cost the operator a whole
// assurance feature: production's *Terminal exposes its notifier as
// WakeNotifier() — deliberately named that way, because a *Terminal carrying
// both a `Notify` method and a `WakeNotifier` field would collide (see raw.go) —
// while both call sites resolved the seam with a bare `src.(Notifier)`
// assertion. The assertion therefore found NOTHING on the one source that
// matters, no seam was installed, the read loop parked in an UNBOUNDED read, and
// a status publish could not reach the screen until a keystroke happened to wake
// the loop. Nothing failed loudly: the editor kept its no-op sink, and only a
// caller that asked NotifyEnabled heard about it.
//
// The regression guard is the RESOLUTION itself, over BOTH spellings a source
// may use, plus the negative case (a source with no seam resolves to nil, so a
// caller can announce the degradation instead of assuming the publish arrived).

// directSeamSource is a Source that IS a Notifier — the spelling the tests'
// scripted sources use.
type directSeamSource struct {
	Source   // the source half is irrelevant here; only the seam is under test
	notified chan struct{}
}

func (s *directSeamSource) Notify() {
	if s.notified != nil {
		close(s.notified)
	}
}

func (s *directSeamSource) NotifyEnabled() bool { return true }

// wakeSeamSource is a Source that exposes its notifier as WakeNotifier() — the
// spelling PRODUCTION uses (*Terminal).
type wakeSeamSource struct {
	Source
	notified chan struct{}
}

func (s *wakeSeamSource) NotifyTheReader() {
	if s.notified != nil {
		close(s.notified)
	}
}

func (s *wakeSeamSource) NotifyEnabled() bool { return true }

// seamNotifier is the sink a wakeSeamSource hands out.
type seamNotifier struct{ s *wakeSeamSource }

func (n seamNotifier) Notify()             { n.s.NotifyTheReader() }
func (n seamNotifier) NotifyEnabled() bool { return true }

func (s *wakeSeamSource) WakeNotifier() Notifier { return seamNotifier{s: s} }

// bareSource is a Source that exposes no seam at all.
type bareSource struct{ Source }

// TestSourceNotifierResolvesBothSpellings is the guard: every spelling a source
// may use is resolved, and a source with no seam resolves to nil.
func TestSourceNotifierResolvesBothSpellings(t *testing.T) {
	// The spelling the tests' scripted sources use.
	direct := &directSeamSource{notified: make(chan struct{})}
	got := SourceNotifier(direct)
	if got == nil {
		t.Fatal("a source implementing Notifier directly resolved NO notifier: the publish would never wake the read loop")
	}
	got.Notify()
	select {
	case <-direct.notified:
	default:
		t.Fatal("the resolved notifier did not call through to the source's own seam")
	}

	// The spelling PRODUCTION uses — the one the bare assertion missed, and the
	// reason the status row refreshed only on a keystroke in a real terminal.
	wake := &wakeSeamSource{notified: make(chan struct{})}
	got = SourceNotifier(wake)
	if got == nil {
		t.Fatal("a source exposing its seam as WakeNotifier() resolved NO notifier: this is the production shape, so the status row would refresh only on the next keystroke")
	}
	got.Notify()
	select {
	case <-wake.notified:
	default:
		t.Fatal("the resolved notifier did not call through to the source's WakeNotifier seam")
	}

	// The negative: nothing to resolve is nil, never a no-op sink (which would
	// look installed to a caller that only checked for non-nil, and would make
	// NotifyEnabled a lie).
	if got := SourceNotifier(&bareSource{}); got != nil {
		t.Fatalf("a source with no seam resolved %#v, want nil", got)
	}
	if got := SourceNotifier(nil); got != nil {
		t.Fatalf("a nil source resolved %#v, want nil", got)
	}
}
