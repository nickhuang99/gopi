package gopilog

// taskemit_gate_test.go — the anti-regression GATE for the binary-briefing fix
// (PROJECT.md Rule 13: a guard that exists but is bypassed by the next emitter is
// a defect, and only an exhaustive test closes the hole).
//
// The defect was that s.Task — the session's accepted prompt — reached the
// rendered output RAW at several independent emit sites. The fix routes every
// emitter through the shared guard (tasktext.go). A future emitter added
// without the guard would silently reopen the defect, so this test scans the
// package's PRODUCTION files and fails any line that reads or prints a
// captured-prompt field unless that line is one of these shapes:
//
//   - a line carrying a sessionTask* call (the guard itself),
//   - a presence check (`if s.Task != ""`) — no bytes are emitted,
//   - an assignment (`s.Task = …`, `currentIter.UserPrompt = …`) — a write,
//     not a render,
//   - a comment line (prose may name the field).
//
// The field pattern is anchored (`\.Task\b`, `\.UserPrompt\b`) so task-list
// types — TaskID, Tasks, Summary — are not mistaken for the captured prompt.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// taskEmitFieldRe matches a captured-prompt FIELD ACCESS on any receiver
// (s.Task, ss.Task, iter.UserPrompt, currentIter.UserPrompt).
var taskEmitFieldRe = regexp.MustCompile(`\.(Task|UserPrompt)\b`)

// taskAssignRe matches a WRITE to such a field: `currentIter.UserPrompt = …`.
var taskAssignRe = regexp.MustCompile(`^\w+\.(Task|UserPrompt)\s*=`)

// taskPresenceRe matches a line that only tests the field, emitting nothing
// (a bare or `if`-guarded comparison).
var taskPresenceRe = regexp.MustCompile(`^(if\s+)?\w+\.(Task|UserPrompt)\s*(==|!=)`)

// guardMarkers are the shapes that make a render line safe.
var guardMarkers = []string{
	"sessionTaskText", // block form, structure preserved
	"sessionTaskLine", // single-line form
	"sessionTaskLines",
}

// TestEveryTaskEmitSiteGoesThroughTheGuard is the Rule-13 sweep: every
// production line that touches the captured prompt must be a guard call, a
// presence check, an assignment or a comment.
func TestEveryTaskEmitSiteGoesThroughTheGuard(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned, guarded := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !taskEmitFieldRe.MatchString(line) {
				continue
			}
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "//"), strings.HasPrefix(trimmed, "*"):
				continue // prose about the field
			case containsAny(line, guardMarkers):
				guarded++
				continue // the guarded emit
			case taskPresenceRe.MatchString(trimmed):
				continue // `if s.Task != "" {` — emits nothing
			case taskAssignRe.MatchString(trimmed):
				continue // a write, not a render
			}
			t.Errorf("%s:%d touches the captured prompt without the guard — route it through sessionTaskText/sessionTaskLine/sessionTaskLines:\n  %s",
				name, i+1, trimmed)
		}
	}
	// A broken glob must not pass the gate vacuously: both the tree under scan
	// and the guarded emit sites have to be seen.
	if scanned < 5 {
		t.Fatalf("scanned only %d production files — the gate read the wrong tree", scanned)
	}
	if guarded < 3 {
		t.Fatalf("found only %d guarded emit site(s) — the guard is not wired", guarded)
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
