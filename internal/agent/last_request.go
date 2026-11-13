// last_request.go — the request the model was last handed, read by a CARRIER
// that wants to tell a person what it sent.
//
// WHY AN ACCESSOR AND NOT A SECOND DERIVATION. The engine already keeps this
// value: `lastClaudeRequest` is set at the ONE place a request is actually sent
// (request_tokens_cap.go, in chatOnceGated, right where the send happens), and
// the crash dump's digest is built from it. A console that re-read the raw HTTP
// log to answer the same question would pay for a tail scan, would be answering
// about the FILE rather than about the round that just ran, and would be a
// second derivation of one fact (Rule 14). This exposes the existing field; it
// computes nothing.
//
// WHAT IT IS, PRECISELY. The request built for the MOST RECENT round of the
// current turn — the last body this process handed to the provider. On a turn
// that ran many rounds (a turn routinely takes dozens of them) it is the LAST
// round: the one carrying the whole accumulated conversation plus this turn's
// input, which is exactly the object a reader asking "what did this turn send?"
// is looking for.
//
// LIFETIME AND SAFETY. The engine writes it on the run goroutine; a carrier
// reads it AFTER its run call has returned, when no writer exists any more. It
// is read-only from here (a pointer to the engine's own struct, never a copy):
// the carrier's job is to COUNT and to quote one line, never to mutate.
package agent

import "github.com/nick/gopi/internal/deepseek"

// LastClaudeRequest is the request the model was last handed (nil before the
// first round of a session, and on every run that never built one).
//
// A nil answer is a MEASUREMENT that says "no request exists yet", not an error:
// the caller renders it as the absence it is (Rule 15).
func (a *Agent) LastClaudeRequest() *deepseek.ClaudeRequest {
	if a == nil {
		return nil
	}
	return a.lastClaudeRequest
}
