package tools

// The verify-retry briefing states its smoke prescription ONCE (task
// 01a188fa-0bf9-72ee-8f06-f911a0da42a3).
//
// WHY THIS ARM EXISTS, and why it did not exist before. Row 01a188fa was filed
// against `taskVerifyBriefing` in tasks.go, which spelled the cap-risk
// instruction twice back to back. That function is GONE: commit 199223a
// (2026-10-30 23:45:55, `refactor(task): retire verify / retry-delivery /
// reconcile as dispatch-only removals`) deleted the whole mechanism, and a probe
// of the live tree found the phrase nowhere in Go source. So the delivered fix
// had no producer left to fix — but the DEFECT was alive one producer over:
// `MergeVerifyFileVerifyRetry` (verify_gate.go) rendered the same three smoke
// facts twice, in step 4 and again in step 5, because each numbered step had been
// written to stand alone. Measured on the live producer, before the fix:
//
//	PAYLOAD_LEN=3969
//	count(make test)=2
//	count(scoped form)=2
//
// A briefing has ONE reader — one child — so "standalone steps" is not a
// property it needs, and the repetition it bought is a reader who cannot tell
// whether the second statement is a second requirement.
//
// COUNTING THE RENDERED PAYLOAD, not the source: MergeVerifyRowPayloadForTest
// reads back what the gate actually filed, so a second copy reintroduced
// anywhere in the format string fails here whatever it is wrapped in (a source
// count could pass while the producer had drifted).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/smokegate"
)

const (
	// verifyRetrySmokeCommandProbes are the spellings that each occur exactly
	// ONCE when the prescription is stated once: the smoke command, its scoped
	// form, and the doc that qualifies them. They are named here rather than
	// inlined because this file exists to stop a duplication of exactly these
	// values (Rule 1/13) — a test that re-typed them would drift with them.
	verifyRetrySmokeCommandProbe = "`" + smokegate.Command + "`"

	// verifyRetrySmokeWanted is how many times each probe may appear in ONE
	// rendered payload.
	verifyRetrySmokeWanted = 1
)

// TestTheVerifyRetryBriefingStatesTheSmokeCommandOnce pins the count the live
// verify-retry briefing renders at.
func TestTheVerifyRetryBriefingStatesTheSmokeCommandOnce(t *testing.T) {
	repo := vgRepo(t)
	vgCommitFile(t, repo, "fix/once", "once.txt", "once\n", "fix: once")
	vgLedger(t)
	oldID := vgSubmitRow(t, repo, "fix/once", "bug", "bug: the briefing probe row")

	newID, err := MergeVerifyFileVerifyRetry(MergeVerifyVerifyRetryOptions{
		Repo: repo, Branch: "fix/once", Master: "master",
		PreTip:    "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f",
		AfterTip:  "1f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f",
		OldTaskID: oldID,
		Reason:    "the verify child produced no verdict",
		Evidence:  "child exit 0, no verdict recorded",
	})
	if err != nil {
		t.Fatalf("file verify retry: %v", err)
	}
	payload, err := MergeVerifyRowPayloadForTest(newID)
	if err != nil {
		t.Fatalf("read back the filed payload: %v", err)
	}

	// The negative control: a payload this test never inspects would make every
	// count below trivially 0 and pass. The gate's own opening line is the
	// witness that the read-back is the briefing.
	if !strings.Contains(payload, "VERIFIER") {
		t.Fatalf("the read-back payload is not the verify-retry briefing (no VERIFIER line), so its counts mean nothing:\n%s", payload)
	}

	for _, probe := range []struct{ what, s string }{
		{"the smoke command", verifyRetrySmokeCommandProbe},
		{"the scoped form", "`make test-budget"},
		{"the scoped-form slot", "<the Go packages your diff touched>"},
	} {
		if got := strings.Count(payload, probe.s); got != verifyRetrySmokeWanted {
			t.Errorf("the verify-retry briefing states %s %d time(s), want %d — one child reads one briefing, so a"+
				" second statement is read as a second requirement:\n%s", probe.what, got, verifyRetrySmokeWanted, payload)
		}
	}
}
