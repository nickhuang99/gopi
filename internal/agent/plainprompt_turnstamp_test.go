package agent

// RP-1 TURN-STAMP BOUNDARY (the in-process repro, Rule 5) — a plain prompt's
// FIRST request carries no harness turn stamp, and every other request still
// does.
//
// WHY THIS FILE EXISTS. The contract the shell surface makes is `gopi
// [options] "<prompt>"` ⇒ "`\"<prompt>\"`  The task text" (the help page's own
// Inputs row), and cmd/agent's out-of-process probe measures it at the real
// request boundary (cli_runpath_boundary_rp1_test.go). That probe spawns the
// binary per case and is the RIGHT acceptance instrument, and it is slow — so
// the diagnosis needs a case that lives in the package that OWNS the defect.
// This is that case: in process, no child, no profile, no ledger.
//
// THE DEFECT, AND WHERE THE STAMP ACTUALLY LANDED. `requestMessages` appends the
// per-request turn stamp (clock.go) as a USER-role message, and
// internal/deepseek/claude_client.go's BuildClaudeRequest MERGES consecutive
// user text ("the API requires alternating roles"). On the plain-prompt surface
// the preceding user message IS the operator's text, so "appended" meant
// "spliced into the prompt": the measured wire content was
// `RP1-BOUNDARY-PLAIN-PROMPT-4f2a[system] [t+0s] — first request of this
// session. session budget: …` — 128 characters where the prompt is 30. The
// merge happens in the CLIENT, so reordering the array here cannot fix it; what
// fixes it is declining to mint the TEXT while still minting the STAMP.
//
// THE THREE ARMS, because the cheap ways to pass arms other than A are the
// reasons this file has three:
//
//	A — a plain prompt's FIRST request is the prompt, byte for byte.
//	B — the SAME agent's SECOND request IS stamped, and it is a GAP stamp
//	    (turnStampGapSuffix), not a second "first request of this session": the
//	    exemption withheld the TEXT, never the MINT, so lastRequest still
//	    advanced and the next request's gap is measured from a moment the model
//	    was really spoken to (Rule 15 — an absent measurement reported as absent,
//	    not invented).
//	C — a TASK surface's first request is stamped as before: the exemption is
//	    keyed on the plain-prompt surface, not on "the first request".
//
// Arm C is the over-correction guard. `gopi task run` children, the merger and
// the chat surfaces reach this same assembly point, and the stamp is the
// every-round time signal their own fixtures pin (cli_lineedit_test.go's
// `stamps == requests`; sessionbudget_everyround_test.go). Deleting the stamp
// outright would satisfy A and break all of them.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// TestPlainPrompt_FirstRequestCarriesNoTurnStamp is the fix's own regression
// case, in three arms (A/B/C above).
//
// It drives `requestMessages` — the ONE place the stamp is appended, and the
// function the product change routes through — with a real clock, so the claim
// is about the request the wire would carry rather than about a render helper.
func TestPlainPrompt_FirstRequestCarriesNoTurnStamp(t *testing.T) {
	// ARM A — the plain prompt's first request.
	plain := plainPromptStampProbe(t)
	if got := plain.requestMessages(false); len(got) != 1 || got[0].Content != ppPromptToken {
		t.Fatalf("a plain prompt's FIRST request carries %d message(s), last = %q — want the prompt ALONE (%d chars): the wire merges consecutive user text, so an appended stamp lands inside the operator's own text (RP-1; clock.go turnStampForRequest)",
			len(got), stampShape(got), len(ppPromptToken))
	}

	// ARM B — the SAME agent's second request. Asserted on the SUFFIX, not on
	// "a stamp is present": a run that skipped the mint would send a second
	// FIRST stamp here, and the run would then report its own time base as
	// unmeasured while counting from nothing.
	second := plain.requestMessages(false)
	if len(second) != 2 {
		t.Fatalf("the same agent's SECOND request carries %d message(s) — the exemption must not leak past the first request", len(second))
	}
	stamp := second[len(second)-1].Content
	if !strings.Contains(stamp, turnStampGapSuffix) {
		t.Fatalf("the second request's stamp is %q, want it to carry %q — the exemption withholds the TEXT, never the MINT: lastRequest must still have advanced, or the run's gap is measured from a moment nobody was spoken to",
			stamp, turnStampGapSuffix)
	}
	if strings.Contains(stamp, turnStampFirstSuffix) {
		t.Fatalf("the second request re-minted a FIRST stamp (%q) — the first request's exemption threw away the mint instead of only its text (Rule 15)", stamp)
	}
	if second[0].Content != ppPromptToken {
		t.Fatalf("the second request lost the operator's prompt: %q", second[0].Content)
	}

	// ARM C — a task surface's first request: the exemption is keyed on the
	// SURFACE, so a `task run` child / merger / chat run keeps the stamp it has
	// always carried.
	task := plainPromptStampProbe(t)
	task.plainPrompt = false
	first := task.requestMessages(false)
	if len(first) != 2 {
		t.Fatalf("a task surface's first request carries %d message(s), want 2 (the briefing plus its stamp) — the exemption must not have been widened to every first request", len(first))
	}
	if got := first[len(first)-1].Content; !strings.Contains(got, turnStampFirstSuffix) {
		t.Fatalf("a task surface's first request stamp is %q, want it to carry %q", got, turnStampFirstSuffix)
	}
}

// TestPlainPrompt_TaskSurfaceKeepsOneStampPerRequest states arm C as the
// RELATION its own fixtures rely on rather than as one sample: on a non-plain
// surface EVERY assembled request carries exactly one stamp — the exemption
// must be invisible from there (cli_lineedit_test.go's `stamps == requests`).
func TestPlainPrompt_TaskSurfaceKeepsOneStampPerRequest(t *testing.T) {
	ag := plainPromptStampProbe(t)
	ag.plainPrompt = false
	for i := 1; i <= ppStampRequests; i++ {
		first := ag.requestMessages(false)
		stamp := first[len(first)-1]
		if !strings.Contains(stamp.Content, turnStampFirstSuffix) && !strings.Contains(stamp.Content, turnStampGapSuffix) {
			t.Fatalf("request %d of a task surface carries no stamp: %+v", i, stamp)
		}
	}
}

// ppStampRequests is how many requests the relation above samples — enough for
// the FIRST arm and several GAP arms, small enough to stay a unit test.
const ppStampRequests = 3

// plainPromptStampProbe builds the minimal surface whose REQUEST ASSEMBLY is
// the subject: one operator message, a live clock, and the surface flag the
// caller sets.
//
// It deliberately does NOT run a loop: the subject is the request the run
// assembles, and a probe that answered a mock would measure the same assembly
// through three more moving parts. The end-to-end half lives where it belongs —
// cmd/agent's out-of-process probe (Rule 31) and
// plainprompt_firstmessage_test.go's loop-level cases.
func plainPromptStampProbe(t *testing.T) *Agent {
	t.Helper()
	// A throwaway profile: requestMessages itself reaches no database, but the
	// Agent's other wiring must not be able to reach the operator's (Rule 32 —
	// no hand-rolled path, no ledger touched).
	newTestAgentEnv(t)
	ag := &Agent{Ctx: &Context{}, Clock: NewSessionClock()}
	ag.plainPrompt = true
	ag.Ctx.AddUserMessage(ppPromptToken)
	return ag
}

// stampShape renders an assembled request for a failure message.
func stampShape(msgs []deepseek.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(m.Role)
		b.WriteString(":")
		b.WriteString(headChars(m.Content))
	}
	return b.String()
}
