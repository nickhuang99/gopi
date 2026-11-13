package scratch

// The drift gate for the disk-exhaustion vocabulary: the shell entry point's
// markers and diagnostic-shape guard must BE the values this package defines.
//
// WHY A TEST AND NOT A COMMENT: the two surfaces are a Go package and a shell
// script, so no shared constant can bind them. Nobody retypes a kernel phrase on
// purpose — they fix one side and forget the other, and the surface that stopped
// matching then reports a full disk as a code failure with nothing to say it did.
// The test reads the FILE (never a copy of its text), so it fails exactly when
// the two stop agreeing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiskExhaustionMarkersAgreeAcrossTheTwoSurfaces is the gate: the entry
// point's marker set must equal DiskExhaustionMarkers, and its diagnostic-shape
// guard must still carry DiskExhaustionDiagnosticShape.
func TestDiskExhaustionMarkersAgreeAcrossTheTwoSurfaces(t *testing.T) {
	root := repoRootForDeclaredTreeGate(t)
	rel := filepath.FromSlash(DiskExhaustionShellScriptPath)
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("the shell half is unreadable (%s): %v — this gate's anchor is gone, which must never read as 'in sync'",
			rel, err)
	}
	text := string(body)

	shellMarkers, ok := shellVarValue(text, DiskExhaustionShellMarkersVar)
	if !ok {
		t.Fatalf("%s declares no %s — the shell half lost its marker vocabulary, and a classifier with no markers reports every full disk as a code failure",
			rel, DiskExhaustionShellMarkersVar)
	}
	shellSet := map[string]bool{}
	for _, m := range strings.Split(shellMarkers, "|") {
		if m = strings.TrimSpace(m); m != "" {
			shellSet[m] = true
		}
	}
	// BOTH DIRECTIONS. A marker the shell knows and Go does not is a claim no Go
	// reader can make; a marker Go knows and the shell does not is a filesystem
	// refusal the entry point calls a code failure.
	for _, want := range DiskExhaustionMarkers {
		if !shellSet[want] {
			t.Errorf("marker %q is absent from %s's %s — the entry point would report that filesystem refusal as a test failure (Rule 13/14: one value, two readers)",
				want, rel, DiskExhaustionShellMarkersVar)
		}
		delete(shellSet, want)
	}
	for extra := range shellSet {
		t.Errorf("marker %q is in %s's %s but NOT in scratch.DiskExhaustionMarkers — the surfaces disagree about what a filesystem refusal is; add it to this package's list (its ONE definition), not to either reader alone",
			extra, rel, DiskExhaustionShellMarkersVar)
	}

	shape, ok := shellVarValue(text, DiskExhaustionShellShapeVar)
	if !ok {
		t.Fatalf("%s declares no %s — without the diagnostic guard a real compile error carrying a disk phrase would be relabelled a full disk",
			rel, DiskExhaustionShellShapeVar)
	}
	if got := shape; got != DiskExhaustionDiagnosticShape {
		t.Errorf("%s's %s is %q, want the shared shape %q (Rule 13/14)",
			rel, DiskExhaustionShellShapeVar, got, DiskExhaustionDiagnosticShape)
	}
}

// shellVarValue reads `name=value` out of a shell script's declarations, taking
// the value to the end of the line and stripping the quoting a shell assignment
// may carry. A deliberately NARROW read: the caller compares an exact value, and
// a shell parser here would be a second, weaker implementation of the shell.
func shellVarValue(text, name string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		rest, found := strings.CutPrefix(line, name+"=")
		if !found {
			continue
		}
		return strings.Trim(strings.TrimSpace(rest), `"'`), true
	}
	return "", false
}
