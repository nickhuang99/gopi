package inputsource_test

// inputsource_test.go — the vocabulary's own contract, asserted from OUTSIDE the
// package (an external test package, so it may import a CONSUMER: internal/lineedit
// imports inputsource, and an in-package test importing lineedit would be a cycle).
//
// WHAT ONLY THIS FILE CAN SHOW. Each consumer's own cases prove its half — the
// reference renders the value it was given, the inbox stamps the value the CLI
// passed. None of them proves the thing that binds the halves: that the FOUR
// values are distinct, that the OLD spelling this change migrates away from is
// now refused, and that a value which round-trips through the reference format
// comes back unchanged (the format's `source=` field is a wire spelling, and a
// vocabulary member that cannot survive it is not a member).
//
// The FOURTH value added by task 01a10f9b (`Queue`) is why the "distinct" half is
// not a formality: `queue` is the value that used to be answered as `human`, so a
// test that merely collected the constants would have passed all along. The set
// is enumerated here so the next arm cannot be added without landing in it.

import (
	"testing"

	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
)

// The old spellings. They were the VALUES before this change (`notify` was
// "gopi notify --session"), and a tree that still accepted one would let two
// spellings of one face live side by side — the defect the single definition
// exists to prevent (Rule 14).
const (
	legacyNotifySource = "gopi notify --session"
	emptySource        = ""
	unknownSource      = "operator"
)

// TestInputSourceValuesAreDistinctAndKnown pins the vocabulary at its own edge.
func TestInputSourceValuesAreDistinctAndKnown(t *testing.T) {
	all := []string{inputsource.Human, inputsource.Queue, inputsource.Notify, inputsource.LLM}
	seen := map[string]bool{}
	for _, v := range all {
		if v == emptySource {
			t.Errorf("a face has the empty value — an unset source would read as that face")
		}
		if seen[v] {
			t.Errorf("two faces share the value %q", v)
		}
		seen[v] = true
		if !inputsource.Known(v) {
			t.Errorf("Known(%q) = false for a value this package defines", v)
		}
	}
	for _, bad := range []string{emptySource, legacyNotifySource, unknownSource} {
		if inputsource.Known(bad) {
			t.Errorf("Known(%q) = true, want false — it is not one of the four faces", bad)
		}
	}
}

// TestInputSourceValueSurvivesTheReferenceFormat is the binding between the
// vocabulary and the wire spelling: every face must round-trip through the paste
// reference, and the legacy spelling must NOT come back as a known value.
func TestInputSourceValueSurvivesTheReferenceFormat(t *testing.T) {
	for _, src := range []string{inputsource.Human, inputsource.Queue, inputsource.Notify, inputsource.LLM} {
		line := lineedit.PasteRefFor(lineedit.PasteRef{Path: "/s/paste-1.txt", Bytes: 12, Runes: 12, Source: src})
		info, ok := lineedit.PasteRefInfoFor(line)
		if !ok {
			t.Fatalf("the reference %q does not parse", line)
		}
		if info.Source != src {
			t.Errorf("the reference round-tripped source %q, want %q (%q)", info.Source, src, line)
		}
		if info.Size != 12 {
			t.Errorf("the reference lost its size: %q → %d", line, info.Size)
		}
		if !lineedit.SourcesAreKnown(line) {
			t.Errorf("a reference this package's own values produced is reported as unknown: %q", line)
		}
	}
	// The OLD spelling is readable as DATA (it is what a session written before
	// this change holds) and is NOT one of the faces: a reader that treated it as
	// one would keep two names for the notify face alive.
	old := "[paste 12: /s/paste-1.txt source=" + legacyNotifySource + " size=12B]"
	info, ok := lineedit.PasteRefInfoFor(old)
	if !ok {
		t.Fatalf("the old-format reference %q does not parse", old)
	}
	if inputsource.Known(info.Source) {
		t.Errorf("the legacy source %q is accepted as a face — the vocabulary must have one spelling per face", info.Source)
	}
}
