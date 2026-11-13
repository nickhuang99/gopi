package main

// devworkspace_channel_prefix_test.go — THE STUTTER DEFECT'S REGRESSION GATE
// (measured 2026-10-30/31, PROBE_PREFIX_COUNT=2).
//
// WHAT WENT WRONG. exhausted() composed its reason by pasting the channel's
// prefix into its own template AND interpolating (*Blocker).Report() — which
// opens EVERY Kind branch with the SAME prefix. The row's reason and the
// returned error therefore read:
//
//	clone channel: delivery did not land in 3 attempt(s); last blocker (dirty): clone channel: refusing to pull: …
//
// WHY THE ASSERTION IS Count(...) == 1 AND NEVER Contains(...). Contains is TRUE
// FOR A STUTTER: a double-prefixed line still contains one. Only the COUNT of
// the prefix in the composed line distinguishes "the speaker said its name once"
// from "two composition sites both said it", so that is what this file asserts —
// on the returned error (the ledger's reason is cerr.Error() verbatim,
// cmd/agent/cli_tasks.go) and on the log line the operator reads.
//
// The reverse proof (in the task report): delete the strip inside
// devworkspace.CloneChannelBody — or replace the cloneChannelReason() call in
// exhausted() with a hand-pasted template again — and this test prints `got 2`
// and FAILs. A green here is therefore non-vacuous.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
)

// TestCloneChannelExhaustedReasonCarriesThePrefixExactlyOnce is ①: the
// exhausted arm — the epic's measured defect — says the channel's name exactly
// once in BOTH places the reason becomes visible.
func TestCloneChannelExhaustedReasonCarriesThePrefixExactlyOnce(t *testing.T) {
	f := newFakeLoop(t, nil, dirtyBlocker(), nil)
	last, err := f.state.exhausted(&f.log, devworkspace.Result{Dir: f.state.dir, Branch: "master"}, dirtyBlocker())
	if err == nil {
		t.Fatal("the exhausted arm must fail the run")
	}
	reason := last.Detail + err.Error()

	// ① THE RETURNED ERROR (== the ledger row's reason, verbatim).
	if got := strings.Count(err.Error(), devworkspace.CloneChannelPrefix); got != 1 {
		t.Fatalf("the reason must carry the channel prefix exactly once, got %d in:\n%s", got, err.Error())
	}
	// ② THE OPERATOR'S LOG LINE, read off the same buffer the loop wrote.
	logged := f.log.String()
	if got := strings.Count(logged, devworkspace.CloneChannelPrefix); got != 1 {
		t.Fatalf("the run's log must carry the channel prefix exactly once, got %d in:\n%s", got, logged)
	}
	// ③ AND IT STILL SAYS WHAT HAPPENED — the prefix collapsing must not have
	//    eaten the body it was composed over (Rule 15: a line with no reason is
	//    not an improvement on a stuttering one). The dirty Kind's own words are
	//    the ones Report() writes for it.
	if !strings.Contains(err.Error(), "delivery did not land") {
		t.Fatalf("the reason must still say the delivery did not land:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "refusing to pull") {
		t.Fatalf("the reason must still carry the blocker's own sentence:\n%s", err.Error())
	}
	// ④ THE PREFIX IS THE LINE'S FIRST WORD, not merely present once: a
	//    composition that moved it into the middle would pass ① and read as a
	//    different speaker.
	if !strings.HasPrefix(err.Error(), devworkspace.CloneChannelPrefix) {
		t.Fatalf("the reason must OPEN with the channel prefix: %q", err.Error())
	}
	// ⑤ ONE CLOSING ROUND, not zero and not one per attempt: this arm owes a
	//    report, and the prefix fix must not have changed that.
	if f.agentCalls != 1 {
		t.Fatalf("exhausted must spend exactly 1 closing report round, got %d", f.agentCalls)
	}
	if reason == "" {
		t.Fatal("unreadable: the composed reason is empty")
	}
}
