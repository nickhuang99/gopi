package main

// gopibin_identity_refusal_signature_gate_test.go — the REFUSAL SIGNATURE's two
// halves, and the reason both are checked.
//
// THE DEFECT THIS PINS (measured 2026-10-14, task 01a13c43, master cbdaca0): the
// runner recognises a freshness refusal by the sentence the guard renders
// (scripts/lib/stream-failures.sh, stream_freshness_signature). On master the
// CONTENT arm — the arm a mid-run content change actually fires — rendered its own
// wording and carried NO marker at all, so the ordinary shape (a production source
// edited while the suite ran) produced refusals the runner could not see, and the
// summary named none of them: N unattributed `--- FAIL:` rows.
//
// WHY IT IS A GATE AND NOT A COMMENT. The signature exists twice — composed in Go
// (freshnessRefusalSignature), matched in shell (stream_freshness_signature) — and
// a shell literal beside a Go literal drifts silently: the shell side simply stops
// matching and the summary goes quiet about a condition it used to name (Rule 15,
// a silent empty answer). So the gate reads BOTH files and compares them (Rule
// 14: one value, two readers, one checked definition), which is the same shape
// TestFreshnessMarkerSpellingsAgree already established for the marker.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// refusalSignatureName / refusalSignatureLibConstName and the pair beside them
	// are the two NAMES each of the two composed lines is spelled under (Rule
	// 13/14 — one value, two readers).
	refusalSignatureName          = "freshnessRefusalSignature"
	refusalSignatureLibConstName  = "stream_freshness_signature"
	refusalCriterionPrefixName    = "freshnessCriterionPrefix"
	refusalCriterionPrefixLibName = "stream_freshness_criterion_prefix"
	// refusalArmSourceFile is the file every refusal arm composes its message in;
	// refusalArmComposer opens an arm's message and refusalArmEnd closes the
	// binFreshness literal it is returned in.
	refusalArmSourceFile  = "gopibin_identity_guard_test.go"
	refusalArmComposer    = "Refusal: fmt.Sprintf("
	refusalArmEnd         = ", nil"
	refusalArmFloor       = 3
	refusalMarkerSourceFl = "task_session_cwd_freshness_evidence_test.go"
	// refusalSignatureTextName is the OTHER constant that carries the signature:
	// freshnessRefusalText composes it, and the mtime arm's evidence message
	// composes THAT (a carrier of a carrier). An arm is judged by a carrier set
	// rather than by one spelling, because the value reaches the reader through
	// composition and the point of the gate is that no arm is left without it.
	refusalSignatureTextName = "freshnessRefusalText"
	refusalMarkerConstantN   = "cwdProbeFreshnessMarker"
)

// TestEveryRefusalArmCarriesTheSignature is the BOTH-DIRECTIONS arm: every
// composer the guard can return a refusal from must print the signature AND open
// its criterion line with the prefix the runner relays.
func TestEveryRefusalArmCarriesTheSignature(t *testing.T) {
	root := cwdProbeModuleRoot(t)
	guard := filepath.Join(root, sourceTreeCmdDir, "agent", refusalArmSourceFile)
	body := mustRead(t, guard)
	arms := refusalArmWindows(body)
	if len(arms) < refusalArmFloor {
		t.Fatalf("found %d refusal arm(s) in %s; want at least %d (build identity, content, mtime×2) — a scan that "+
			"stopped finding them would make this gate pass by finding nothing", len(arms), guard, refusalArmFloor)
	}
	for i, arm := range arms {
		if !strings.Contains(arm, refusalSignatureName) && !strings.Contains(arm, refusalSignatureTextName) {
			t.Errorf("refusal arm #%d composes neither %s nor %s into its message — a refusal the runner cannot recognise is a "+
				"condition the report cannot name:\n%s", i+1, refusalSignatureName, refusalSignatureTextName, arm)
		}
		if !strings.Contains(arm, refusalCriterionPrefixName) {
			t.Errorf("refusal arm #%d does not open its criterion line with %s — the runner relays the guard's criterion "+
				"by that prefix, so an arm that stopped printing it would leave the summary re-deriving a decision the "+
				"guard already made:\n%s", i+1, refusalCriterionPrefixName, arm)
		}
	}
}

// refusalArmWindows returns the source text of every refusal arm: from each
// `Refusal: fmt.Sprintf(` to the `}, nil)` that closes its binFreshness literal.
// Text scanning rather than a regex, because the messages span lines and carry
// quotes, escapes and nested calls (a pattern tuned to one of those details stops
// matching the moment the message is reformatted).
func refusalArmWindows(body string) []string {
	var arms []string
	rest := body
	for {
		at := strings.Index(rest, refusalArmComposer)
		if at < 0 {
			return arms
		}
		rest = rest[at:]
		end := strings.Index(rest, refusalArmEnd)
		if end < 0 {
			return arms
		}
		arms = append(arms, rest[:end])
		rest = rest[end+len(refusalArmEnd):]
	}
}

// TestFreshnessSignatureAndCriterionPrefixSpellingsAgree is the DRIFT arm over the
// two composed-line constants the runner reads: the signature and the criterion
// prefix. Both are compared file-to-file, the way TestFreshnessMarkerSpellingsAgree
// compares the marker.
func TestFreshnessSignatureAndCriterionPrefixSpellingsAgree(t *testing.T) {
	root := cwdProbeModuleRoot(t)
	guardPath := filepath.Join(root, sourceTreeCmdDir, "agent", refusalArmSourceFile)
	libPath := filepath.Join(root, cwdProbeScriptsDir, "lib", cwdProbeStreamFailuresLib)

	// The Go side COMPOSES the signature from the marker constant (that is the
	// point — the marker cannot be deleted from the message while the sentence
	// stands), so it is read as a Go EXPRESSION and evaluated against the marker's
	// value; comparing it to a hand-typed sentence here would be a third spelling.
	marker, err := stringConstValue(filepath.Join(root, sourceTreeCmdDir, "agent", refusalMarkerSourceFl), refusalMarkerConstantN)
	if err != nil {
		t.Fatalf("read the guard's marker: %v", err)
	}
	goSignature, err := stringConstValue(guardPath, refusalSignatureName)
	if err != nil {
		t.Fatalf("read the guard's signature: %v", err)
	}
	libSignature, err := shellVarValue(libPath, refusalSignatureLibConstName)
	if err != nil {
		t.Fatalf("read the runner's signature: %v", err)
	}
	gotSignature := composedConstValue(goSignature, refusalMarkerConstantN, marker)
	if gotSignature != libSignature {
		t.Fatalf("the two spellings of the refusal signature disagree:\n  %s (composed) = %q\n  %s = %q",
			refusalSignatureName, gotSignature, refusalSignatureLibConstName, libSignature)
	}

	goPrefix, err := stringConstValue(guardPath, refusalCriterionPrefixName)
	if err != nil {
		t.Fatalf("read the guard's criterion prefix: %v", err)
	}
	libPrefix, err := shellVarValue(libPath, refusalCriterionPrefixLibName)
	if err != nil {
		t.Fatalf("read the runner's criterion prefix: %v", err)
	}
	if goPrefix != libPrefix {
		t.Fatalf("the two spellings of the criterion prefix disagree:\n  %s = %q\n  %s = %q",
			refusalCriterionPrefixName, goPrefix, refusalCriterionPrefixLibName, libPrefix)
	}
}

// composedConstValue evaluates the one SHAPE that matters here: a constant whose
// value is Go string concatenation — `"literal" + otherConst + "literal"`. It
// drops the `+` operators and the quotes AROUND each operand, substitutes the
// other constant's value, and returns the sentence the compiler would build.
// Anything else (a bare literal, a formatting call) is returned trimmed, so a
// constant that stops being a composition is compared as the text it is.
func composedConstValue(expr, otherName, otherValue string) string {
	value := expr
	for _, glue := range []string{`" + `, ` + "`} {
		value = strings.ReplaceAll(value, glue, "")
	}
	value = strings.ReplaceAll(value, otherName, otherValue)
	return strings.Trim(value, `"`)
}

// mustRead reads a file or fails the test — the gate reads files as TEXT on
// purpose (it is about the FILES agreeing, not about importing one side).
func mustRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
