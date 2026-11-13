package main

// cli_web_page_harness_route_test.go — the guard for the page harness's TURN
// COUNTER.
//
// THE DEFECT THIS PINS (measured, task 01a1b574): the harness counted a turn as
// "a URL whose string starts with '/api/chat'". Every chat route starts with
// that prefix, so when the page grew the waiting-message REGISTRATION
// (/api/chat/queue, commit 1a2883c) a single mid-turn message began arriving in
// the counter TWICE — once as a registration, once as the turn it later becomes.
// `state.posts` therefore read one too many at BOTH of the B12 scenario's
// counting points (got 2 want 1 while turn 1 ran, got 3 want 2 after it
// drained), and the group went red on master with the PAGE behaving correctly.
// The counter was the defect, not the product: the raw request log — captured
// in this task's session directory as probe_b12_turncount.txt — shows the two
// requests are different routes with different bodies.
//
// TWO independent things could silently re-open it, so this file checks both:
//
//  1. THE HARNESS'S ROUTE LITERALS MUST EQUAL THE SERVER'S. The harness cannot
//     import Go (it is plain node), so it carries its own copy of the route
//     names; a rename on either side would desync them and the counter would
//     start counting nothing — or a DIFFERENT route — with no test noticing.
//  2. THE COUNTER MUST BE KEYED ON THE ROUTE, NOT ON A PREFIX. A future route
//     named '/api/chat…' would otherwise become a "turn" by accident. This is
//     checked against the shipped harness file itself (the source of truth for
//     how the page is driven), not against a restatement of it.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// pageHarnessFile is the harness source this test reads. It is the ONE name
// (Rule 13): pageHarnessPath already declares it for the tests that RUN it.
const pageHarnessFile = pageHarnessPath

// harnessRouteConstRe reads a route literal out of the harness source:
// `const CHAT_TURN_ROUTE = '/api/chat';`.
var harnessRouteConstRe = regexp.MustCompile(`(?m)^const ([A-Z][A-Z0-9_]*_ROUTE) = '([^']*)';$`)

// harnessRouteConsts returns the harness's declared route names, keyed by the
// harness's own constant name.
func harnessRouteConsts(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(pageHarnessFile)
	if err != nil {
		t.Fatalf("read %s: %v — the harness the drivers run must be readable, or this guard judges nothing", pageHarnessFile, err)
	}
	out := map[string]string{}
	for _, m := range harnessRouteConstRe.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		t.Fatalf("found no route literal in %s — the regex or the declarations changed, so this guard would pass while checking nothing", pageHarnessFile)
	}
	return out
}

// TestPageHarnessRoutesAgreeWithTheServer pins the harness's route copies
// against the Go constants the mux actually registers.
//
// A silent desync here is exactly the B12 shape one level up: the harness would
// keep "counting turns" on a route nothing POSTs to, every scenario would still
// pass on an empty counter, and the sequential-drain assertion would be
// vacuous.
func TestPageHarnessRoutesAgreeWithTheServer(t *testing.T) {
	want := map[string]string{
		"CHAT_TURN_ROUTE":         webChatPath,
		"CHAT_QUEUE_ROUTE":        webChatQueuePath,
		"CHAT_QUEUE_CANCEL_ROUTE": webChatQueueCancelPath,
		"CHAT_RESET_ROUTE":        webChatResetPath,
		"CHAT_INTERRUPT_ROUTE":    webChatInterruptPath,
	}
	got := harnessRouteConsts(t)
	for name, serverPath := range want {
		harnessPath, ok := got[name]
		if !ok {
			t.Errorf("%s is not declared in %s — the harness's turn counter would fall back to whatever it counted before",
				name, pageHarnessFile)
			continue
		}
		if harnessPath != serverPath {
			t.Errorf("%s = %q in %s but the server registers %q — the harness counts a route nothing POSTs to, so its turn tally is not about the shipped page",
				name, harnessPath, pageHarnessFile, serverPath)
		}
	}
}

// TestPageHarnessCountsTurnsOnTheRouteNotAPrefix is the regression arm: it
// fails on the exact expression that produced the +1.
//
// It reads the harness source and refuses a turn bucket selected by a string
// PREFIX test. The prefix spelling is the defect, so it is named literally here
// rather than paraphrased — a paraphrase would let the old line back in under a
// new variable name.
func TestPageHarnessCountsTurnsOnTheRouteNotAPrefix(t *testing.T) {
	raw, err := os.ReadFile(pageHarnessFile)
	if err != nil {
		t.Fatalf("read %s: %v", pageHarnessFile, err)
	}
	src := string(raw)

	// THE EXACT DEFECT: a bucket whose condition is "the URL starts with the
	// chat prefix" rather than "the URL IS the chat route".
	prefixSpelling := `String(url).indexOf('/api/chat') === 0`
	if strings.Contains(src, prefixSpelling) {
		t.Fatalf("%s still selects a bucket with %q — every '/api/chat…' route matches that test, which is how the queue REGISTRATION became a counted TURN (+1 on both B12 counting points)",
			pageHarnessFile, prefixSpelling)
	}

	// And the positive half, so this cannot pass by the harness no longer having
	// a turn bucket at all: the counter must compare against the ROUTE constant.
	if !strings.Contains(src, "route === CHAT_TURN_ROUTE") {
		t.Fatalf("%s no longer selects its turn bucket by comparing the request route against CHAT_TURN_ROUTE — state.posts would stop being a count of turns, and every sequential-drain assertion built on it would be about something else",
			pageHarnessFile)
	}

	// The registration bucket must be SEPARATE and counted separately: a scenario
	// can then tell "the page told the server a message waits" from "the page ran
	// a turn", which are the two different facts B12 is made of.
	if !strings.Contains(src, "route === CHAT_QUEUE_ROUTE") {
		t.Fatalf("%s no longer recognises the waiting-message REGISTRATION route — a mid-turn message would either fall through to the turn bucket again or disappear from every count",
			pageHarnessFile)
	}
	if !strings.Contains(src, "state.queueRegistrations.push") {
		t.Fatalf("%s no longer records registrations — B12's assertions cannot distinguish a queued message from a delivered turn",
			pageHarnessFile)
	}
}
