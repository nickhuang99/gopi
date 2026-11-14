package tools

import (
	"testing"

	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// Producer-shaped dirty TAGGED display (what Info.Display() actually prints).
func TestTmpRealDirtyTaggedShape(t *testing.T) {
	tip := "8380ce9caeef"
	name := "v20260918-0932-" + tip
	display := name + gopiversion.DirtySuffix + " (" + tip + ")"
	t.Logf("producer-shaped dirty tagged display = %q", display)
	if versionMatches(display, tip) {
		t.Errorf("HOLE: versionMatches(%q, %q) = true; a dirty build must never claim the tip", display, tip)
	}
}
