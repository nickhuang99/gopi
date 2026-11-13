package tools

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/signalrec"
)

// TestSessionidRefusalCarriesTheOperandProvenance pins the clause this change
// adds: an id→path refusal must say WHICH CHANNEL handed the process the
// inadmissible string, because that is the one question the shape gate in
// internal/sessionid cannot answer (it is a leaf and reads no environment) and
// the question the extra-digit attribution actually had to settle by hand.
//
// The three arms are the three verdicts the clause can honestly reach. The
// fourth case — the one this must never do — is inventing a source it did not
// measure: with the channel unset the clause says so instead of blaming an
// environment that was never read (Rule 15).
func TestSessionidRefusalCarriesTheOperandProvenance(t *testing.T) {
	// The measured row: a uuid with one extra hex character (37 chars, 33
	// under the dashes) — the shape bug row 01a1472c-2919 was filed for.
	const extraDigit = "01a1452c-928d-7e0f9-9ef3-52c1482cfbc3"
	const ledgerID = "01a1452c-928d-7e0f-9ef3-52c1482cfbc3"

	t.Run("channel unset: says unset, invents no source", func(t *testing.T) {
		t.Setenv(signalrec.TaskIDEnv, "")
		msg := sessionidRefusal(extraDigit).Error()
		if !strings.Contains(msg, signalrec.TaskIDEnv) || !strings.Contains(msg, "unset") {
			t.Errorf("refusal with the channel unset = %q, want it to name %s as unset", msg, signalrec.TaskIDEnv)
		}
		if strings.Contains(msg, "EQUALS the ledger id this process was handed") {
			t.Errorf("refusal = %q, want no attribution to a channel that was never set", msg)
		}
	})

	t.Run("operand equals the handed ledger id: producer is upstream", func(t *testing.T) {
		t.Setenv(signalrec.TaskIDEnv, extraDigit)
		msg := sessionidRefusal(extraDigit).Error()
		if !strings.Contains(msg, "EQUALS") || !strings.Contains(msg, "upstream") {
			t.Errorf("refusal when the operand came FROM the channel = %q, want it to say the producer is upstream of this process", msg)
		}
	})

	t.Run("operand differs from the handed ledger id: this process's own text", func(t *testing.T) {
		t.Setenv(signalrec.TaskIDEnv, ledgerID)
		msg := sessionidRefusal(extraDigit).Error()
		if !strings.Contains(msg, "DIFFERS") {
			t.Errorf("refusal when the operand came from the caller's own text = %q, want it to say the operand DIFFERS from the handed id", msg)
		}
		if !strings.Contains(msg, ledgerID) {
			t.Errorf("refusal = %q, want the channel's declared value %q named so a reader can compare", msg, ledgerID)
		}
	})

	t.Run("the leaf's sentence is not duplicated: hint arms survive the wrap", func(t *testing.T) {
		t.Setenv(signalrec.TaskIDEnv, "")
		// The grouping arm still prints internal/sessionid's derivable repair...
		grouping := sessionidRefusal("01a12dc6f44d73479b952382a4a7d352").Error()
		if !strings.Contains(grouping, "01a12dc6-f44d-7347-9b95-2382a4a7d352") {
			t.Errorf("grouping-arm refusal = %q, want the leaf's canonical row id named as the repair", grouping)
		}
		// ...and an id with NO near-miss still invents none (Rule 15: a
		// fabricated repair is worse than a bare refusal).
		if msg := sessionidRefusal("zzzz").Error(); strings.Contains(msg, "did you mean") {
			t.Errorf("no-near-miss refusal = %q, want NO fabricated repair", msg)
		}
		// The wrap must not swallow the leaf's own sentence: the refusal is
		// still recognisably internal/sessionid's, so the two seams and this
		// clause cannot drift (Rules 13/14).
		if !strings.Contains(grouping, "task id shape:") {
			t.Errorf("refusal = %q, want the leaf's own sentence carried through the wrap", grouping)
		}
	})
}

// TestTaskSessionDirRefusalNamesTheOperandSource drives the SEAM, not the
// helper: the path-element gate must be the surface that carries the clause,
// so a caller holding the bad spelling learns the source without a second
// lookup (Rule 31: the changed surface is exercised end to end).
//
// The operand here is the BRANCH/WORKTREE spelling (a uuid's 32 hex chars with
// the dashes dropped) rather than the extra-digit one, and that is a measured
// constraint of this baseline, not a preference: at this revision
// internal/sessionid refuses only the grouping arm
// (`git merge-base --is-ancestor 3c479d342 HEAD` → exit 1 — the extra-digit
// arm lives on branch task/01a14596573f7b59b8cec2645a2c80f6 and is not merged
// here), so `taskSessionDir` accepts the extra-digit operand on this tree. The
// provenance clause is arm-agnostic: it fires for whatever the leaf refuses,
// which is exactly why it is written against the seam and not against one arm.
func TestTaskSessionDirRefusalNamesTheOperandSource(t *testing.T) {
	const branchSpelling = "01a1452c928d7e0f9ef352c1482cfbc3"
	t.Setenv(signalrec.TaskIDEnv, "")

	_, err := taskSessionDir(branchSpelling)
	if err == nil {
		t.Fatalf("taskSessionDir(%q) = nil error, want the shape gate to refuse it", branchSpelling)
	}
	msg := err.Error()
	if !strings.Contains(msg, "task session dir:") {
		t.Errorf("err = %q, want the seam's own prefix", msg)
	}
	if !strings.Contains(msg, "operand source:") || !strings.Contains(msg, signalrec.TaskIDEnv) {
		t.Errorf("err = %q, want the operand-source clause attached at the seam", msg)
	}
	// The clause carries its own prefix; the wrapper must not add a second one
	// (measured once on this change: "operand source: operand source: …").
	if n := strings.Count(msg, "operand source:"); n != 1 {
		t.Errorf("err carries %d operand-source prefixes, want exactly 1: %q", n, msg)
	}
}
