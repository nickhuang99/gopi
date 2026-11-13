package stylegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// truncatedConstFixture is the shape the gate used to misreport: a t.Skip
// whose reason is a NAMED constant, and one whose reason is a named constant
// concatenated with a literal (`longReason + " — …"`, exactly the shape
// cmd/agent/cli_term_once_test.go:640 writes). Both are compile-time string
// constants — the rule exists to refuse a DYNAMIC reason, and neither
// propagates a runtime value.
//
// The two control arms below keep the fix honest, because "no finding" is the
// dangerous direction: a reason that is genuinely too short must still be
// refused, and a reason built at run time must still be refused. A gate that
// went green by accepting every constant would fail both.
const truncatedConstFixture = `package p

import "testing"

const longReason = "the kernel exposes this table only on a Linux host with a mounted procfs, so the measurement cannot be taken here"
const shortReason = "too short"

func TestLongConstReason(t *testing.T) {
	if true {
		t.Skip(longReason)
	}
}

func TestConstPlusLiteralReason(t *testing.T) {
	if true {
		t.Skip(longReason + " — a named constant concatenated with a literal is still a literal reason")
	}
}

func TestShortConstReason(t *testing.T) {
	if true {
		t.Skip(shortReason)
	}
}

func TestDynamicReason(t *testing.T) {
	reason := "dynamic reason built at run time by the test itself"
	if true {
		t.Skip(reason)
	}
}
`

// TestSkipReasonIsALongNamedConstant is the red proof for the truncated-const
// miss: go/types renders a constant LONGER THAN 72 CHARS as `"prefix..."`, so
// parsing tv.Value.String() cannot unquote it and the site was reported as
// "t.Skip without a literal reason string". The value itself is NOT truncated
// (constant.StringVal returns it exactly — a standalone probe measured this
// before the change), so the reading must come from the value, not from the
// rendering.
//
// Measured before the fix (on this tree):
//
//	test-skip findings = 3, want 2 — lines 10 and 16 are constants
//
// and after it: the two constant arms are clean, the two controls still red.
func TestSkipReasonIsALongNamedConstant(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip_test.go"), []byte(truncatedConstFixture), 0o644); err != nil {
		t.Fatalf("write skip_test.go: %v", err)
	}
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findingsByRule(rep)[RuleTestSkip]

	var longConst, constPlusLit, tooShort, dynamic bool
	for _, f := range got {
		switch {
		case f.Line == 10:
			longConst = true
		case f.Line == 16:
			constPlusLit = true
		case f.Line == 22:
			tooShort = true
		case f.Line == 29:
			dynamic = true
		}
	}
	if longConst {
		t.Errorf("line 10 skips with a 108-char NAMED CONSTANT — a literal reason; the reading must come from the constant VALUE, not from go/types' truncated rendering: %+v", got)
	}
	if constPlusLit {
		t.Errorf("line 16 skips with `const + literal`, which folds to one string constant — the truncation of the folded rendering is why it was missed: %+v", got)
	}
	// The control arms: the fix must not turn a genuinely bad reason green.
	if !tooShort {
		t.Errorf("line 22 skips with the 9-char constant %q — SHORTER than MinReasonLen (%d), so it must still be a finding: %+v", "too short", MinReasonLen, got)
	}
	if !dynamic {
		t.Errorf("line 29 skips with a value computed at run time — that is exactly what the rule refuses, and it must still be a finding: %+v", got)
	}
	if len(got) != 2 {
		var lines []string
		for _, f := range got {
			lines = append(lines, strings.TrimSpace(f.Detail))
		}
		t.Errorf("test-skip findings = %d, want exactly 2 (the too-short constant and the dynamic reason); got messages: %v", len(got), lines)
	}
}
