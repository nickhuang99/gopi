package main

// cli_web_align_writeboundary_test.go — the regression arm for the R2 fixture's
// own precondition (measured 2026-10-22, task 01a1625b).
//
// THE DEFECT THIS PINS: webServerForTestTools builds a webServer by hand, so it
// inherits the `tools` package's DEFAULT write boundary (the built-in `prompt *`
// table with the on-request posture) unless it freezes the operator's posture
// exactly as runWeb does at startup. Without that freeze the fixture is not a
// stand-in for `gopi web` at all: its scripted tool round comes back
// `[NEEDS OPERATOR APPROVAL — command NOT run]` and the target file is never
// created, so the R2 case's "the tool round really wrote" assertion fails for a
// reason that has nothing to do with the feature it certifies.
//
// WHY A PREDICATE AND NOT A SECOND COPY OF THE R2 ASSERTION: the R2 case already
// reads the written file back from disk — that is the behavioural proof, and
// duplicating it here would only widen the same surface. What it cannot do is
// say WHY the write was refused, and the boundary is the one thing the fixture
// gets wrong silently (a refusal is a normal, perfectly streamed tool result).
// So this arm reads the FROZEN pair the dispatch itself judges by, and names it
// when the fixture stopped freezing it.
//
// The arm is deliberately two-sided: the baseline half asserts the shipped
// default ASKS (otherwise a process that already allowed everything would make
// the fixture half vacuous), and the fixture half asserts the freeze happened.

import (
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestWebTestToolFixtureFreezesTheChatWriteBoundary is the guard: it goes red if
// webServerForTestTools stops freezing the chat write boundary from the posture
// its own fixture declares.
func TestWebTestToolFixtureFreezesTheChatWriteBoundary(t *testing.T) {
	tools.ResetChatPolicyToDefaults()

	// BASELINE — the shipped default asks. A package whose default already
	// allowed everything would satisfy the arm below with no freeze at all.
	if _, post := tools.ChatPolicyFrozen(); post.AllowsEverything() {
		t.Fatalf("the shipped chat write policy already allows everything (posture %q) — this arm could no longer distinguish a fixture freeze from the package default", post.String())
	}

	_, srv, _ := webServerForTestTools(t, true, filepath.Join(t.TempDir(), "guard-target.txt"))
	defer srv.Close()

	if _, post := tools.ChatPolicyFrozen(); !post.AllowsEverything() {
		t.Fatalf("webServerForTestTools did not freeze the chat write boundary: frozen posture = %q (source %q), want the allow posture it declares (%q) — the R2 fixture's tool round would be answered by the package default instead of running the write",
			post.String(), post.Source(), chatpolicy.PostureYoloWord)
	}
}
