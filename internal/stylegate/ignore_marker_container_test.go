package stylegate

// The ignore marker on a CONTAINER HEADER — the shape a reader writes and the
// scanner used to drop.
//
// Measured site (before this test existed):
//
//	cmd/agent/worker_stop_group_self_guard_test.go
//	  86  //stylegate:ignore fixture teardown of the probe's own grandchild; ...
//	  87  if grandID, grandErr := testutil.ReadProcIdentity(fixture.grand); grandErr == nil {
//	  88      _ = testutil.KillIfIdentical(grandID, syscall.SIGKILL)
//	  89  }
//
// The author's reason sits on the line ABOVE the `if`, the violating statement
// is the block's first statement one line below the header. `lineComment`
// walked upward and stopped at the first non-comment line (the `if` header),
// and `ignoreReason` passed stmt=nil so the forward leg could not reach it
// either — the marker was silently discarded and the site kept counting against
// RuleIgnoredError's ceiling. Neither half is a corner case: "comment the
// statement, then open its block" is the ordinary Go shape.
//
// The probe pins BOTH directions, which is the whole point: the escaped site
// must go quiet AND a bare site of the same shape must still be reported, so a
// "fix" that silences the detector cannot pass.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ignoredRemoveShape is the violating statement every probe site uses: one
// blank-assigned call whose result implements error, which is exactly what
// RuleIgnoredError reports.
const ignoredRemoveShape = "_ = os.Remove(v)"

// probeCompanion is the production file the fixture package needs, and it is
// load-bearing rather than cosmetic. `go list -deps ./...` does NOT walk test
// imports, so a package whose only file is a _test.go importing "testing" has
// no export data for it, every call in that file type-checks to nothing,
// EVERY site comes back undecidable — and the scan reports zero findings with
// no failure at all. Measured while writing this test: the same probe that
// reported nothing here reported its bare site immediately once this file
// existed. Keep it out of the assertions.
const probeCompanion = `package p

import "testing"

var _ = testing.Short
`

// containerMarkerProbe is the fixture: four sites of ONE shape, two of which
// carry a marker the scanner must resolve and two of which must stay reported.
const containerMarkerProbe = `package p

import (
	"os"
	"testing"
)

func TestMarkerOnContainerHeader(t *testing.T) {
	//stylegate:ignore the probe's own teardown; ESRCH is the expected error here
	if v, err := os.Hostname(); err == nil {
		_ = os.Remove(v)
	}
}

func TestMarkerTwoHeadersUp(t *testing.T) {
	//stylegate:ignore the probe's own teardown; ESRCH is the expected error here
	if v, err := os.Hostname(); err == nil {
		if len(v) > 0 {
			_ = os.Remove(v)
		}
	}
}

func TestBareSiteIsStillReported(t *testing.T) {
	if v, err := os.Hostname(); err == nil {
		_ = os.Remove(v)
	}
}

func TestMarkerSeparatedByABlankLine(t *testing.T) {
	//stylegate:ignore the probe's own teardown; ESRCH is the expected error here

	if v, err := os.Hostname(); err == nil {
		_ = os.Remove(v)
	}
}
`

// writeProbe lays the fixture package down and returns its root.
func writeProbe(t *testing.T, testSrc string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":        fixtureMod,
		"probe.go":      probeCompanion,
		"probe_test.go": testSrc,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// probeSiteLines reports the 1-based line of every ignoredRemoveShape statement
// in src, in source order.
func probeSiteLines(t *testing.T, src string) []int {
	t.Helper()
	var out []int
	for i, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == ignoredRemoveShape {
			out = append(out, i+1)
		}
	}
	return out
}

func TestContainerHeaderMarkerIsReadByTheSiteInside(t *testing.T) {
	dir := writeProbe(t, containerMarkerProbe)
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(rep.PackagesFailed) > 0 {
		t.Fatalf("type information failed, so every verdict below would be unsound: %v", rep.PackagesFailed)
	}

	reported := map[int]bool{}
	for _, f := range rep.Findings {
		if f.Rule == RuleIgnoredError {
			reported[f.Line] = true
		}
	}

	// The four sites, in source order:
	//   [0] marker on its container's header          → must NOT be reported
	//   [1] marker two headers up                     → must NOT be reported
	//   [2] bare site                                 → MUST still be reported
	//   [3] marker detached from the header by a blank → MUST still be reported
	lines := probeSiteLines(t, containerMarkerProbe)
	if len(lines) != 4 {
		t.Fatalf("probe self-check: found %d `%s` site(s) %v, want 4 — the fixture and the assertions drifted apart",
			len(lines), ignoredRemoveShape, lines)
	}
	for i, wantReported := range []bool{false, false, true, true} {
		line := lines[i]
		if reported[line] != wantReported {
			t.Errorf("site %d at line %d: reported=%v, want reported=%v (%s findings at %v)",
				i, line, reported[line], wantReported, RuleIgnoredError, reported)
		}
	}
}

// TestIgnoreMarkerAfterAMultiLineStatementIsRead pins the second half of the
// same root cause: `ignoreReason` passed stmt=nil, so reasonOverSpan's forward
// leg (the comments from the site's line to the END of its statement) never
// ran for the rules that resolve their reason through it. A marker at the end
// of a multi-line statement — where an editor's "comment the statement"
// gesture lands it — was silently discarded.
//
// The probe uses t.Skip with a DYNAMIC reason, because that is the shape that
// actually routes through ignoreReason; checkAssign passes its statement
// explicitly, so the same comment on a `_ = call(...)` statement was already
// read before the fix and would have made this a green-before-green test.
func TestIgnoreMarkerAfterAMultiLineStatementIsRead(t *testing.T) {
	const src = `package p

import (
	"fmt"
	"os"
	"testing"
)

func TestMarkerAtTheEndOfAMultiLineCall(t *testing.T) {
	t.Skip(
		fmt.Sprintf("dynamic reason %d", os.Getpid()), //stylegate:ignore the reason is built at run time; documented here
	)
}
`
	dir := writeProbe(t, src)
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if n := rep.Counts[RuleTestSkip]; n != 0 {
		t.Errorf("%s findings = %d, want 0 — the marker sits INSIDE the statement's span and must be read: %+v",
			RuleTestSkip, n, rep.Findings)
	}
}
