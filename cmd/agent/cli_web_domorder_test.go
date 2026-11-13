// cli_web_domorder_test.go — the page's ORDERING gate: DOM order = SSE arrival
// order.
//
// The defect it pins (measured on the shipped page): runTurn appended an empty
// assistant bubble BEFORE sending the POST (`addMsg('assistant',”)`), and every
// tool card was appended to the log's END as its own frame arrived. So the
// space on screen was `answer` then `cards` — the exact reverse of the order the
// frames came in, and of what the TUI prints — and a reader could not tell which
// answer belonged to which call.
//
// Two halves, because neither sees the defect alone:
//
//  1. TestWebDomOrder_ToolCardPrecedesItsAnswer DRIVES the shipped page through
//     the harness: it stages the stream so the tool-call frame is delivered
//     while the answer has streamed nothing, observes the DOM at that instant
//     (no bubble may exist), then completes the turn and requires the card to
//     sit BEFORE the bubble. It also holds the two non-regressions the epics
//     name: a pure-answer turn still draws its bubble, and a slash turn opens
//     none (R4).
//  2. TestWebDomOrder_BubbleIsNotOpenedBeforeTheRequest is the static half —
//     the ordering decision lives in ONE place (`openBubble`), so a re-inlined
//     `addMsg('assistant'` at the top of runTurn (the pre-fix shape) must fail
//     loudly rather than silently restore the reversed order.
package main

import (
	"os/exec"
	"strings"
	"testing"
)

// domOrderScenarioName is the scenario the harness is asked to run alone;
// `--only` exits 1 when nothing matches, so deleting the scenario fails this
// gate instead of turning it green-blind.
const domOrderScenarioName = "DOM order equals ARRIVAL order"

// TestWebDomOrder_ToolCardPrecedesItsAnswer is the driven half of the gate.
//
// It requires the card to be mounted while no assistant bubble exists, the card
// to sort BEFORE the bubble once the answer streams, exactly one bubble per
// turn, and the pure-answer / slash-turn behaviours to be unchanged.
func TestWebDomOrder_ToolCardPrecedesItsAnswer(t *testing.T) {
	node := nodePath(t)
	out, runErr := exec.Command(node, pageHarnessPath, "--only", domOrderScenarioName, "--page", menuNavGoFile).CombinedOutput()
	text := string(out)
	if !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) || runErr != nil {
		t.Fatalf("the DOM-order scenario did not pass (err=%v):\n%s", runErr, text)
	}
	t.Logf("DOM-order scenario:\n%s", text)
}

// TestWebDomOrder_BubbleIsNotOpenedBeforeTheRequest pins the SHAPE of the fix on
// the page source, so the reversed order cannot come back unnoticed.
//
// The pre-fix line was `const bubble=(message.startsWith('/'))?null:addMsg('assistant',”);`
// — an unconditional append before the fetch. The fix mounts the bubble from
// inside openBubble() only. What is asserted: (a) openBubble exists, (b) its
// only addMsg call is the deferred one, and (c) no `addMsg('assistant'` call
// survives inside runTurn OUTSIDE openBubble — i.e. the bubble's creation is
// reachable from the frame handlers alone.
func TestWebDomOrder_BubbleIsNotOpenedBeforeTheRequest(t *testing.T) {
	script := pageScript(t)

	const openDecl = "function openBubble(){"
	if !strings.Contains(script, openDecl) {
		t.Fatalf("the page no longer declares %q — the deferred bubble is the ordering fix, and without it the answer is mounted before the POST again", openDecl)
	}

	// runTurn's body, from its own declaration to the NEXT top-level function
	// that follows it (the page's functions are declared at column 0).
	start := strings.Index(script, "async function runTurn(")
	if start < 0 {
		t.Fatal("the page no longer declares `async function runTurn(`")
	}
	body := script[start:]
	if next := strings.Index(body[len("async function runTurn("):], "\nfunction "); next >= 0 {
		body = body[:len("async function runTurn(")+next]
	}

	// openBubble's own `addMsg('assistant'` is the ONE legitimate site; every
	// other occurrence inside runTurn would be a pre-request mount.
	openAt := strings.Index(body, openDecl)
	if openAt < 0 {
		t.Fatalf("openBubble is not declared inside runTurn — the bubble must be created by the turn that owns it:\n%s", body)
	}
	closeAt := strings.Index(body[openAt:], "\n  }")
	if closeAt < 0 {
		t.Fatalf("openBubble's body could not be delimited:\n%s", body)
	}
	openBody := body[openAt : openAt+closeAt]
	if !strings.Contains(openBody, "addMsg('assistant','',true)") {
		t.Errorf("openBubble no longer mounts the bubble through addMsg('assistant','',true):\n%s", openBody)
	}
	outside := strings.Replace(body, openBody, "", 1)
	if strings.Contains(outside, "addMsg('assistant'") {
		t.Errorf("runTurn mounts an assistant bubble OUTSIDE openBubble — that is the pre-fix shape (the bubble would be appended before the request, and the cards would arrive below it):\n%s", outside)
	}

	// The first delta is what mounts it: the `delta` branch must call
	// openBubble() BEFORE it renders, and the `tool` branch must NOT.
	deltaAt := strings.Index(outside, "if(ev==='delta'){")
	if deltaAt < 0 {
		t.Fatal("the frame handler no longer branches on 'delta'")
	}
	deltaBranch := outside[deltaAt:]
	if nl := strings.Index(deltaBranch, "}else if("); nl >= 0 {
		deltaBranch = deltaBranch[:nl]
	}
	if !strings.Contains(deltaBranch, "openBubble();") {
		t.Errorf("the delta branch does not call openBubble() — the answer's first frame is the moment the bubble must appear:\n%s", deltaBranch)
	}
	toolAt := strings.Index(outside, "if(ev==='tool'){")
	if toolAt >= 0 {
		toolBranch := outside[toolAt:]
		if nl := strings.Index(toolBranch, "}else if("); nl >= 0 {
			toolBranch = toolBranch[:nl]
		}
		if strings.Contains(toolBranch, "openBubble()") {
			t.Errorf("the tool branch opens the bubble — a tool card must never mount the answer's row (the card comes first, the answer later):\n%s", toolBranch)
		}
	}
}
