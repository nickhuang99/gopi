package sigsource

// capture_state.go — the VOCABULARY of "was the kernel's word captured for this
// delivery", and the ONE function that answers it.
//
// WHY A STATE IS NEEDED AT ALL (measured 2026-10-27, 297 signal records in
// ~/.gopi/sessions/*/signal-provenance.jsonl). 290 of them carried no si_code
// key at all, and a reader of the file could not tell three different facts
// apart: "this delivery carried no kernel source", "the binary that wrote it
// predates the capture", and "the capture refused to arm on this host". All
// three were rendered the same way — by silence (Rule 15: silence is the defect,
// not the inconvenience). Rule 15a forbids the obvious shortcut of writing
// si_code=0 as the marker, because SI_USER IS 0 and a reader would then read
// every "not captured" as "an ordinary process sent this".
//
// So the record gets a field of its OWN, with a CLOSED vocabulary, and this
// file is its single definition (Rules 1/13/14: no bare keywords at the call
// site, one home for a shared value).

import (
	"os"
	"sync"
)

// The states a record's si_capture field can carry. They are operator-facing
// words — an operator's jq filter and a test key on THESE constants.
const (
	// CaptureCaptured — a trampoline was installed for this signal AND it
	// captured this delivery, so the si_code fields beside it are the kernel's
	// own answer.
	CaptureCaptured = "captured"
	// CaptureNotCaptured — this host HAS the capture, and it was not in force
	// for this delivery (released before the signal arrived, or no siginfo
	// reached the table). The record then carries NO si_code: the kernel was
	// never asked, and a fabricated 0 would read as SI_USER.
	CaptureNotCaptured = "not-captured"
	// CaptureArmFailed — the kernel refused an rt_sigaction (or the chain could
	// not be trusted), so the capture never took effect in this process. The
	// reason carries the refusal verbatim.
	CaptureArmFailed = "arm-failed"
	// CaptureUnsupported — this build has no capture implementation at all
	// (non-Linux, or Linux without amd64). The reason names the target.
	CaptureUnsupported = "unsupported-host"
	// CaptureNoDelivery — the record is not about a signal DELIVERY at all (a
	// KindIntent line, written by the KILLER before it signals), so the question
	// "was the kernel's word captured for it" has no answer to give. It exists
	// instead of an empty string because si_capture is a required key: an empty
	// value would be indistinguishable from a producer that forgot to stamp it
	// (Rule 15a), and the whole point of the field is that no record is silent.
	CaptureNoDelivery = "no-delivery"
)

// CaptureReasonUnarmed is the reason a CaptureNotCaptured record carries when
// the ground truth is "nothing captured this delivery" rather than a refusal.
// One spelling, so a reader that aggregates reasons does not have to match prose.
const CaptureReasonUnarmed = "no captured siginfo for this delivery: the capture was not armed for this signal when it arrived"

// armOutcome holds the LAST arming failure in this process ("" = none). It is a
// process-lifetime fact, not a per-signal one, and it exists so the record can
// say WHY the kernel's word is missing instead of only that it is: an arm
// refusal used to reach stderr once, at install time, where no reader of the
// provenance file would ever find it.
var armOutcome struct {
	mu     sync.Mutex
	reason string
}

// noteArmFailure records the outcome of an arming attempt ("" clears it). Arm
// calls it; nothing else does, so the answer cannot drift from the attempt.
func noteArmFailure(reason string) {
	armOutcome.mu.Lock()
	defer armOutcome.mu.Unlock()
	armOutcome.reason = reason
}

// armFailure returns the refusal that stopped the capture from arming, or "".
func armFailure() string {
	armOutcome.mu.Lock()
	defer armOutcome.mu.Unlock()
	return armOutcome.reason
}

// Describe answers, for ONE delivery, in what state the capture found itself —
// the answer internal/signalrec stamps onto every record it writes as
// si_capture (+ its reason).
//
// THE PRECEDENCE IS STATED ONCE, HERE (Rule 2's discipline applied to a fact
// rather than a deadline): a target with no implementation outranks an arm
// refusal (nothing could have been installed to fail), which outranks the
// per-delivery question. A caller that re-derived this order could disagree with
// this function about the very same process.
func Describe(sig os.Signal) (state, reason string) {
	if why := UnsupportedReason(); why != "" {
		return CaptureUnsupported, why
	}
	if why := armFailure(); why != "" {
		return CaptureArmFailed, why
	}
	if Delivered(sig).Valid {
		return CaptureCaptured, ""
	}
	return CaptureNotCaptured, CaptureReasonUnarmed
}
