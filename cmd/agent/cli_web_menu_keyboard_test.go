// cli_web_menu_keyboard_test.go — the B9 keyboard-ARBITRATION gate: the
// keydown/keyup PAIR, which the B9 scenario in cli_web_page_scenarios.js could
// not see.
//
// Why this file exists (the defect it pins): the page bound menuRefresh to
// ['click','keyup'], and menuRefresh rebuilt the menu with the highlight at 0.
// The keydown branch moved the selection (menuMove) and the keyup that followed
// a few milliseconds later re-ran the detect core and put it back — so the
// arrows looked dead, Enter could never accept the row the user walked to, and
// every arrow press spent a /api/menu round-trip. The old scenario drove
// menuMove() DIRECTLY (with no keyup), which is exactly the call the bug's
// second half undid: a green B9 with a broken menu.
//
// The gate is therefore split in two halves, because neither can see the defect
// alone:
//
//  1. TestWebMenu_KeydownKeyupPairKeepsTheHighlight DRIVES the shipped page
//     through the harness with a real keydown+keyup PAIR (and an 'input' event
//     from typing), so a listener that undoes a move fails here.
//  2. TestWebMenu_NavigationKeysAreNamedConstants is the static half: the key
//     NAMES are one carrier the listener guard and the arbitration branch share
//     (Rule 1b/13) — a literal re-inlined into either site would let the guard
//     and the branch drift apart silently, with no runtime signal at all.
package main

import (
	"os/exec"
	"strings"
	"testing"
)

// menuNavScenarioName is the name of the navigation scenario the harness is
// asked to run alone. `--only` must match at least one scenario (the harness
// exits 1 on an empty match), so deleting the scenario fails the test below
// instead of turning the gate green-blind.
const menuNavScenarioName = "B9 arrow-nav"

// menuNavGoFile is the page the harness extracts the shipped script from,
// relative to this package's directory (the harness defaults to it too; naming
// it here makes the gate independent of the harness's own default).
const menuNavGoFile = "cli_web.go"

// pageHarnessPath is the harness the two gates below drive.
const pageHarnessPath = "cli_web_page_harness.js"

// menuHarnessPass is the tally the harness prints when every check passed, and
// menuHarnessFail the one it prints when any did not.
const (
	menuHarnessPass = "PASS:"
	menuHarnessFail = "FAIL"
)

// nodePath returns the node binary the page harness runs under, SKIPPING with
// the limitation spelled out when the box has none (a silent skip is a deleted
// test wearing a green tick — the same posture as the M2b page gate).
func nodePath(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		node, err = exec.LookPath("nodejs")
	}
	if err != nil {
		t.Skip("skipping the menu-keyboard gate: no `node` on PATH (the page's keyboard behaviour cannot be driven without it)")
	}
	return node
}

// TestWebMenu_KeydownKeyupPairKeepsTheHighlight drives the REAL page through a
// keydown+keyup pair and requires the highlight to survive it.
//
// The scenario it runs asserts, for the shipped page: ArrowDown advances the
// highlight through the keydown/keyup pair and is never undone; ArrowUp walks it
// back; a pure arrow key spends NO /api/menu round-trip; a caret click on the
// same query keeps the highlight; Enter completes the HIGHLIGHTED candidate
// without sending; a NEW query resets the highlight to the top; Escape closes
// and leaves nothing behind; and a preserved highlight is CLAMPED when the same
// query comes back with a shorter list.
func TestWebMenu_KeydownKeyupPairKeepsTheHighlight(t *testing.T) {
	node := nodePath(t)
	out, runErr := exec.Command(node, pageHarnessPath, "--only", menuNavScenarioName, "--page", menuNavGoFile).CombinedOutput()
	text := string(out)
	if !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) || runErr != nil {
		t.Fatalf("the menu-keyboard scenario did not pass (err=%v):\n%s", runErr, text)
	}
	t.Logf("menu-keyboard scenario:\n%s", text)
}

// TestWebMenu_NavigationKeysAreNamedConstants is the static half of the gate.
//
// It asserts (a) the page declares the key names ONCE and references them, i.e.
// the literal appears exactly at its own declaration and nowhere else — the
// inline-escape guard Rule 13 asks for, applied across the Go↔JS boundary;
// (b) the keyup listener is GUARDED by the pure-navigation set rather than
// refreshing unconditionally; and (c) the unconditional `sel:0` rebuild — the
// exact line whose removal IS the fix — is gone for good.
func TestWebMenu_NavigationKeysAreNamedConstants(t *testing.T) {
	script := pageScript(t)

	// (a) One declaration per key name, referenced everywhere else.
	declarations := []struct {
		name    string
		decl    string   // the page-side declaration the value must appear in
		value   string   // the DOM key name it must hold
		readers []string // the sites that must read the constant, not the literal
		why     string
	}{
		{
			name:    "ArrowDown",
			decl:    "const KEY_ARROW_DOWN=",
			value:   "ArrowDown",
			readers: []string{"e.key===KEY_ARROW_DOWN", "MENU_NAV_KEYS=new Set([KEY_ARROW_DOWN"},
			why:     "the keydown branch that walks the menu down and the keyup guard that must ignore that key's keyup",
		},
		{
			name:    "ArrowUp",
			decl:    "const KEY_ARROW_UP=",
			value:   "ArrowUp",
			readers: []string{"e.key===KEY_ARROW_UP", "KEY_ARROW_DOWN,KEY_ARROW_UP"},
			why:     "the up branch and the keyup guard's set",
		},
		{
			name:    "Enter",
			decl:    "const KEY_ENTER=",
			value:   "Enter",
			readers: []string{"e.key===KEY_ENTER"},
			why:     "the accept branch, the Alt+Enter steering branch and the plain submit branch",
		},
		{
			name:    "Tab",
			decl:    "const KEY_TAB=",
			value:   "Tab",
			readers: []string{"e.key===KEY_TAB"},
			why:     "the accept branch (Tab completes like Enter)",
		},
		{
			name:    "Escape",
			decl:    "const KEY_ESCAPE=",
			value:   "Escape",
			readers: []string{"e.key===KEY_ESCAPE"},
			why:     "the close branch and the stop-the-turn branch",
		},
	}
	for _, tc := range declarations {
		i := strings.Index(script, tc.decl)
		if i < 0 {
			t.Errorf("%s: the page no longer declares %q — %s would be spelled inline again", tc.name, tc.decl, tc.why)
			continue
		}
		line := script[i:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, "'"+tc.value+"'") {
			t.Errorf("%s: the page declares %q carrying %q, want the DOM key name %q", tc.name, tc.decl, line, tc.value)
		}
		// The literal may appear exactly once: in its own declaration. A second
		// occurrence is an inline escape that can drift from the constant.
		if got := strings.Count(script, "'"+tc.value+"'"); got != 1 {
			t.Errorf("%s: the literal '"+tc.value+"' occurs %d time(s) in the page script, want exactly 1 (its declaration %q) — reference the constant instead (Rule 1b/13)",
				tc.name, got, tc.decl)
		}
		for _, reader := range tc.readers {
			if !strings.Contains(script, reader) {
				t.Errorf("%s: the page no longer reads the constant at %q — %s", tc.name, reader, tc.why)
			}
		}
	}

	// (b) The set of pure-navigation keys exists and carries all five.
	if !strings.Contains(script, "const MENU_NAV_KEYS=new Set([") {
		t.Fatalf("the page no longer declares MENU_NAV_KEYS — the keyup listener has no way to tell a navigation key from a text edit")
	}
	for _, key := range []string{"KEY_ARROW_DOWN", "KEY_ARROW_UP", "KEY_TAB", "KEY_ESCAPE", "KEY_ENTER"} {
		if !strings.Contains(script, key) {
			t.Errorf("MENU_NAV_KEYS must carry %s (a pure-navigation key whose keyup must not re-run the detect core)", key)
		}
	}

	// (c) The keyup listener is GUARDED, and no unguarded keyup refresh and no
	//     unconditional 'sel:0' rebuild survives anywhere in the page.
	if !strings.Contains(script, "addEventListener('keyup',e=>{if(!MENU_NAV_KEYS.has(e.key))menuRefresh();})") {
		t.Errorf("the keyup listener must skip a pure-navigation key: `input.addEventListener('keyup',e=>{if(!MENU_NAV_KEYS.has(e.key))menuRefresh();})`")
	}
	if strings.Contains(script, "['click','keyup'].forEach") {
		t.Errorf("the page still binds menuRefresh to the keyup event unconditionally — that is the defect: the keyup undid every arrow press")
	}
	if strings.Contains(script, "items:got.candidates,sel:0,") {
		t.Errorf("menuRefresh still rebuilds the open menu with the highlight at 0 — the highlight must survive a refresh of the same query")
	}
	// A refresh that can neither reset nor preserve would be the fix's other
	// failure mode: menuRefresh must READ the caller's reset decision.
	if !strings.Contains(script, "async function menuRefresh(resetSel)") {
		t.Errorf("menuRefresh must take the caller's reset decision (resetSel): a new query resets, a same-query refresh preserves")
	}
	// …and the clamp must be present, or a shorter list leaves the highlight
	// past the end where nothing is selected and Enter has nothing to accept.
	if !strings.Contains(script, "Math.min(menu.sel,got.candidates.length-1)") {
		t.Errorf("the preserved highlight must be CLAMPED into the new candidate list (Math.min(menu.sel, got.candidates.length-1))")
	}
	// The typing path is the ONE that resets: it must pass the reset, so a new
	// query can never inherit the previous list's highlight.
	if !strings.Contains(script, "addEventListener('input',()=>menuRefresh(true))") {
		t.Errorf("the input handler must ask for a reset (menuRefresh(true)) — a NEW query starts its list at the top")
	}
}
