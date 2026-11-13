package tools

// Unit pins for the cmd target's stdin framing helper (taskNotifyCmdStdin).
//
// Kept apart from tasknotify_lineframe_test.go on purpose: that file is the
// red-proof fixture copied into a pre-fix tree, and a helper that does not
// exist yet would make the pre-fix run fail at COMPILE time — evidence that the
// suite could not build, not that the defect was observed. This file exercises
// the helper directly and is only meaningful once the helper exists.

import (
	"bytes"
	"strings"
	"testing"
)

// TestTaskNotifyCmdStdinFraming pins the framing function itself: exactly one
// trailing terminator, terminator count idempotent under re-framing, and the
// JSON bytes untouched (the fix adds a line break — it never rewrites JSON).
func TestTaskNotifyCmdStdinFraming(t *testing.T) {
	const terminator = "\n"
	cases := []struct {
		name string
		body string
	}{
		{name: "json.Marshal output (no terminator)", body: `{"a":1}`},
		{name: "caller already terminated", body: "{\"a\":1}" + terminator},
		{name: "caller over-terminated", body: "{\"a\":1}" + terminator + terminator + terminator},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			framed := taskNotifyCmdStdin([]byte(tc.body))
			if got := strings.Count(string(framed), terminator); got != 1 {
				t.Errorf("framed payload carries %d terminators, want exactly 1: %q", got, framed)
			}
			if !bytes.HasSuffix(framed, []byte(terminator)) {
				t.Errorf("framed payload is not terminated: %q", framed)
			}
			// The JSON is never rewritten — only the line break is guaranteed.
			if got, want := bytes.TrimRight(framed, terminator), bytes.TrimRight([]byte(tc.body), terminator); !bytes.Equal(got, want) {
				t.Errorf("framing altered the payload: got %q, want %q", got, want)
			}
			// Idempotent: re-framing an already-framed payload is a no-op.
			if again := taskNotifyCmdStdin(framed); !bytes.Equal(again, framed) {
				t.Errorf("framing is not idempotent: %q → %q", framed, again)
			}
		})
	}
}
