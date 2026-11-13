package main

// chat_surface_bound_round_decision_test.go — the minimal repro for the fixture defect
// that made TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator red at
// chat_surface_bound_e2e_test.go:1449 while the CONSOLE was behaving exactly as designed.
//
// THE MEASURED FAILURE. The mock classified each round by `strings.Contains(tail, "at its
// bound")` over the whole last user message. A chat turn's history is FOLDED
// (internal/agent turnfold): the round after a turn completes opens with a one-line recap
// whose `anchors:` quote that turn's tool result VERBATIM. So from the first refusal
// onwards every round's tail carried the phrase inside an ANCHOR, and the mock classified
// every later round as a fresh refusal. The trace, taken off the wire on this task's own
// tree (the round index is the mock's own count):
//
//	round 42  answeredAlready=true  refused=true   ← the real refusal (a tool_result block)
//	round 43  answeredAlready=false refused=true   ← the OPERATOR's renewed line "carry on"
//	(run ends: the mock answers the period marker forever, so period 2 never asks)
//
// ⇒ the operator's line was swallowed as a refusal, the renewal was announced on the
// console, and the renewed period never started. The failure SURFACED as "the renewed
// period announced no self-driven turn 1/20", i.e. as a defect in the code under test.
//
// THE CRITERION THIS PINS: the verdict comes from the CONTENT-BLOCK KIND of the last
// block of the last user message, never from a substring of the message. An anchor is a
// quote ABOUT the past; the present answer is the block.
//
// The payloads are the smallest faithful samples (Rule 10.4): each is one content array
// in the shape the wire actually carries, with the field order the encoder produces.

import "testing"

func TestBoundRoundDecisionReadsTheBlockNotTheProse(t *testing.T) {
	// foldRecap is what the round after a completed turn opens with: a text block whose
	// `anchors:` quote the previous turn's tool result — INCLUDING, from period 1's
	// refusal onwards, the refusal wording. This is the shape that poisoned the old
	// classifier.
	foldRecap := `{"type":"text","text":"[system] turn folded — the history this note replaces is on disk in this session's transcript\nasked: self-driven-20\nwhat this turn did: 2 round(s), 1 tool call(s).\nconclusion: PERIOD-ONE-DONE\nanchors: chat_surface\n- chat_surface action=type refused: this period's self-driven loop is at its bound…","input":null}`
	operatorLine := `{"type":"text","text":"carry on","input":null}`
	refusalBlock := `{"type":"tool_result","input":null,"tool_use_id":"call_1","content":"[tool_use id: call_1]\nchat_surface action=type refused: this period's self-driven loop is at its bound (20 of 20 turns used) — it does not extend itself"}`
	acceptedBlock := `{"type":"tool_result","input":null,"tool_use_id":"call_1","content":"[tool_use id: call_1]\nchat_surface action=type typed=true bound_in_force=20 turns_used=1/20 pending=true — this line becomes the NEXT turn when the current one ends."}`

	// wire assembles one request body in the shape the client sends it: the user message
	// is the last thing on the wire, and the blocks are its content array.
	wire := func(blocks ...string) string {
		body := `{"model":"m","messages":[{"role":"assistant","content":"…"},{"role":"user","content":[`
		for i, b := range blocks {
			if i > 0 {
				body += ","
			}
			body += b
		}
		return body + `]}],"tools":[{"name":"read"}]}`
	}

	cases := []struct {
		name         string
		body         string
		wantAnswered bool
		wantRefused  bool
		why          string
	}{
		{
			name: "the operator's renewed line, after a fold recap that QUOTES the refusal",
			body: wire(foldRecap, operatorLine),
			// THE REGRESSION. Pre-fix this read (false, true): the anchor's wording drove
			// the verdict, the operator's line was answered as a refusal, and the renewed
			// period never asked.
			wantAnswered: false,
			wantRefused:  false,
			why:          "a text block is an ASK; the anchor inside it is a quote about the past, not the present answer",
		},
		{
			name:         "the tool's own refusal, carried in the tool_result block",
			body:         wire(refusalBlock),
			wantAnswered: true,
			wantRefused:  true,
			why:          "the marker inside the tool_result block IS the present decision",
		},
		{
			name:         "the tool's acceptance, carried in the tool_result block",
			body:         wire(acceptedBlock),
			wantAnswered: true,
			wantRefused:  false,
			why:          "a tool_result block without the marker is an accepted turn, not a refusal",
		},
		{
			name: "an acceptance whose result text mentions nothing, after a recap quoting a refusal",
			body: wire(foldRecap, acceptedBlock),
			// The recap is BEFORE the result here, which is the shape right after a refusal
			// closes period 1 and the operator renews: the LAST block still decides.
			wantAnswered: true,
			wantRefused:  false,
			why:          "the last block is the tool_result and it carries no marker — the earlier anchor must not leak into it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answered, refused := boundRoundDecision(tc.body)
			if answered != tc.wantAnswered || refused != tc.wantRefused {
				t.Fatalf("boundRoundDecision = (answeredAlready=%t, refused=%t), want (%t, %t): %s\nbody: %s",
					answered, refused, tc.wantAnswered, tc.wantRefused, tc.why, tc.body)
			}
		})
	}
}
