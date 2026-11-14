package agent

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// token_reconciliation.go — the RUNTIME reconciliation of the two prompt counts a
// reader has always had to compare by hand: this engine's own LOCAL prediction and
// the SERVER's prompt_tokens for the SAME request.
//
// THE GAP THIS CLOSES (task 01a1b28b, the epic's gap B). The deviation between the
// two numbers was measured ONCE, by hand, and recorded in a source comment
// (context_tokens.go: 141,548 predicted against 144,967 charged — 97.6%, i.e. the
// server charged 2.4% more than the engine predicted). Nothing reconciled it at
// run time, so the question the number raises had no answer available anywhere:
// **is 2.4% a CONSTANT of the accounting (a framing overhead that is always the
// same size), or does it DRIFT with the content?** One manual sample cannot
// distinguish those, and they are not equally harmless: a constant bias can be
// corrected once in the reader's head, while a drifting one means the status row
// quietly lies by a growing margin as the conversation grows.
//
// WHY THE SESSION JSONL AND NOT THE STATUS ROW. The row is a live snapshot whose
// budget is spent to equality (statusPinSlackCells is 0), and this is a
// PER-REQUEST fact — a history. The JSONL already IS that history, every usage
// fact already lives there, `gopi session log <id>` already reads it, and writing
// there needs no new command, no resident process and no timer (Rule 35).
//
// THE FIELDS ARE NAMED (Rule 1) and the DERIVED percentage is deliberately stored
// as BASIS POINTS in an integer: a session log is a durable record whose rows are
// compared arithmetically, so a float would make two rows disagree about a tie
// depending on how they were rounded. Basis points keep the comparison exact, and
// the reader renders them.

const (
	// reconFieldPredictedTokens is the LOCAL prediction for the request just sent:
	// agent.Context.CalibratedTokens() read BEFORE the response, i.e. exactly the
	// number the status row was showing at that moment — the pair a reader
	// compares, not some internal intermediate.
	reconFieldPredictedTokens = "predicted_tokens"

	// reconFieldServerPromptTokens is the prompt-side count the response reported
	// (deepseek.ClaudeUsage.PromptSideTokens: input + cache-creation + cache-read).
	reconFieldServerPromptTokens = "server_prompt_tokens"

	// reconFieldDeltaTokens is server − predicted: POSITIVE means the server
	// charged more than the engine predicted (the engine under-counts), negative
	// means it charged less. The sign is the information — an absolute difference
	// would hide which direction the bias runs.
	reconFieldDeltaTokens = "delta_tokens"

	// reconFieldDeltaPctBasisPoints is delta as basis points of the prediction
	// (delta * 10000 / predicted), so a reader — or a test — compares 2.4% as the
	// integer 240 and two rows cannot disagree by rounding.
	reconFieldDeltaPctBasisPoints = "delta_pct_bp"

	// reconFieldPredictionAnchored records WHICH prediction the row measured, and a
	// reader MUST group by it before folding (Context.PredictionAnchored states the
	// two populations and the measured rows that forced this field into existence).
	//
	//	true  — the prediction was a MEASURED base plus an estimated tail, so the
	//	        delta measures the TAIL only. Small by construction.
	//	false — the whole prompt was the local BPE estimate, so the delta measures
	//	        the WHOLE-PROMPT error. This is the figure "2.4% off" is about.
	//
	// Without it the two land in one column and their mean is a number about
	// nothing: measured on this mechanism's first real chat, 0/1/0/94 bp — mean 24
	// bp, i.e. "accurate to a quarter of a percent", which is true of the three
	// anchored rows and false of the one unanchored row that was 0.94% out.
	reconFieldPredictionAnchored = "prediction_anchored"

	// reconFieldFrame is WHICH stream frame carried the measurement: the response's
	// message_start or its message_delta. The two frames agree on every prompt-side
	// counter (measured across 144 rounds of session 8981132f), but recording which
	// one answered makes a later discrepancy attributable instead of "somewhere in
	// the stream".
	reconFieldFrame = "frame"

	// reconFrameStart / reconFrameDelta are the two spellings of reconFieldFrame.
	// They are the stream's own event names (the same strings
	// internal/agent/loop.go switches on), so a reader grepping the raw log for a
	// frame finds the same word in the JSONL.
	reconFrameStart = "message_start"
	reconFrameDelta = "message_delta"

	// reconBasisPointsScale is 100% in basis points (10,000 bp = 100%). Named
	// because the arithmetic below multiplies by it (Rule 1).
	reconBasisPointsScale = 10000
)

// deltaPercentBasisPoints renders delta as basis points of predicted, for the row
// the caller is about to write.
//
// A NON-POSITIVE PREDICTION HAS NO RELATIVE READING: the caller could only have got
// here with a prediction of 0 if the tokenizer priced nothing, and "n% of nothing"
// is not a fact. It returns the field's ABSENCE sentinel (reconDeltaUnknownBasis)
// rather than a fabricated 0, which would read as "the two agree" — the same
// Rule 15 distinction the rest of this package keeps between "measured zero" and
// "not measured".
// The SECOND return value is the point, and it is a bool rather than a sentinel
// integer ON PURPOSE. An integer sentinel cannot do this job: whatever value it
// picks, that value is also a legal reading (a deviation of exactly 0 bp is a
// perfect match, and 0 is the sentinel this function first used — the row then said
// "the two agree exactly" about a comparison that never happened). Presence in the
// JSONL is the only spelling of "not measured" that cannot be confused with a
// measurement, so the caller OMITS the field when ok is false (Rule 15).
func deltaPercentBasisPoints(predicted, delta int) (int, bool) {
	if predicted <= 0 {
		return 0, false
	}
	return delta * reconBasisPointsScale / predicted, true
}

// reconcilePromptTokens records ONE request's local-vs-server prompt count on the
// session log: what the local tokenizer predicted for the body just sent, what the
// API says it charged, and the signed difference between them.
//
// WHY IT IS A METHOD ON Agent AND NOT A BARE CALL: two things must be true at once
// — a session logger must be open (a bare `gopi "<prompt>"` run without one has no
// JSONL at all, and inventing a file for it would create a dependency nobody asked
// for, Rule 35), and the context must be there to have produced a prediction. Both
// absences are SILENT BY DESIGN here but for opposite reasons: no logger means
// there is no record to write into (not a defect), and no prediction means the
// reconciliation would be a claim about nothing. Neither is an error a caller can
// act on, so neither fails the run — but neither fabricates a 0 either (Rule 15).
//
// A WRITE FAILURE IS REPORTED, not swallowed: the session log is the deliverable
// of this whole mechanism, and a logger that could not write it is a fact the run
// has to be able to see. It is NOT turned into a returned error — the two call
// sites sit in the middle of a successful model round, and replacing the work
// with a complaint about its bookkeeping would be the wrong trade. So the failure
// goes to the channel the run already reports through, the Agent's logger
// (`[gopi] …`, stderr in every production wiring): "cannot fail the run" and
// "nobody hears it" are different claims, and Rule 15 forbids the second.
func (a *Agent) reconcilePromptTokens(usage deepseek.ClaudeUsage, predicted int, frame string) {
	anchored := a.Ctx.PredictionAnchored()
	if a == nil || a.SessionLog == nil || !usage.HasPromptMeasurement() {
		return
	}
	server := usage.PromptSideTokens()
	// NO PREDICTION, NO RECONCILIATION: with predicted <= 0 the delta is a number
	// without a referent and the percentage is undefined. The request still
	// measures (SetPromptTokens runs as before); only this row is skipped.
	if predicted <= 0 {
		return
	}
	// The write cannot fail the run: a session log that refuses a row is already a
	// broken log, and this row is evidence, not the work. Dropped deliberately —
	// the failure would otherwise replace a successful model round with an error
	// about bookkeeping.
	if err := a.SessionLog.LogTokenReconciliation(predicted, server, server-predicted, frame, anchored); err != nil {
		wrapped := fmt.Errorf("session log refused the token reconciliation row (frame %q, predicted %d, server %d): %w",
			frame, predicted, server, err)
		if a.Logger != nil {
			a.Logger.Printf("[gopi] %v", wrapped)
			return
		}
		fmt.Fprintf(os.Stderr, "[gopi] %v\n", wrapped)
	}
}
