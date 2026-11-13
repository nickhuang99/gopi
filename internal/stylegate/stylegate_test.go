package stylegate

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestParseVerbs(t *testing.T) {
	cases := []struct {
		format string
		want   []verb
	}{
		{"plain text", nil},
		{"100%% sure", nil},
		{"ctx: %v", []verb{{letter: 'v', semanticOffset: 6}}},
		{"%w and %v", []verb{{letter: 'w', semanticOffset: 1}, {letter: 'v', semanticOffset: 8}}},
		{"%+v", []verb{{letter: 'v', flags: "+", semanticOffset: 2}}},
		{"%[2]v", []verb{{letter: 'v', argIndex: 2, semanticOffset: 4}}},
		{"%q: %v", []verb{{letter: 'q', semanticOffset: 1}, {letter: 'v', semanticOffset: 5}}},
		{"trailing %", nil},
	}
	for _, c := range cases {
		got := parseVerbs(c.format)
		if len(got) != len(c.want) {
			t.Errorf("parseVerbs(%q) = %d verbs, want %d (%v)", c.format, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseVerbs(%q)[%d] = %+v, want %+v", c.format, i, got[i], c.want[i])
			}
		}
	}
}

func TestLiteralVerbOffset(t *testing.T) {
	// A tab before the verb: one escape char in the source, one decoded char.
	lit := &ast.BasicLit{Kind: token.STRING, Value: "\"a\\tb: %v\""}
	off, ok := literalVerbOffset(0, lit.Value, "a\tb: %v", 6)
	if !ok {
		t.Fatal("offset not resolved")
	}
	if want := 8; off != want {
		t.Errorf("offset = %d, want %d", off, want)
	}
	// An escape form outside the exact model must be refused, never guessed.
	if _, ok := literalVerbOffset(0, "\"\\x41 %v\"", "A %v", 2); ok {
		t.Error("\\x escape must be refused (offset cannot be trusted)")
	}
	if _, ok := literalVerbOffset(0, "`raw %v`", "raw %v", 4); ok {
		t.Error("backquoted literal must be refused")
	}
}

// ---------------------------------------------------------------------------
// End-to-end fixture: one temp module exercising every rule.
// ---------------------------------------------------------------------------

const fixtureMod = `module fixture

go 1.24
`

const fixtureProd = `package fixture

import (
	"errors"
	"fmt"
	"os"
)

func guarded(err error) error {
	if err != nil {
		return fmt.Errorf("guarded: %v", err) // EDITOR: nil-safe, mechanical edit
	}
	return nil
}

func unguarded(err error) error {
	return fmt.Errorf("unguarded: %v", err) // must stay %v (err may be nil)
}

func flags(err error) error {
	if err != nil {
		return fmt.Errorf("flags: %+v", err) // %+v prints a stack — never %w
	}
	return nil
}

func shadowed(err error) error {
	if err != nil {
		func() {
			err := errors.New("inner") // shadow: a DIFFERENT object
			_ = err
		}()
		return fmt.Errorf("outer: %v", err) // still nil-safe (outer err unchanged)
	}
	return nil
}

func discarded() {
	_, _ = os.Hostname()            // ignored error, no reason
	_ = os.Setenv("X", "1")         // ignored error + a test-only rule
	_ = os.Remove("/nonexistent")   //stylegate:ignore best-effort cleanup of a probe file
}

func bareClose(f *os.File) {
	f.Close()        // unchecked close — finding
	defer f.Close()  // deferred close — informational, not a finding
}
`

const fixtureTest = `package fixture

import (
	"os"
	"testing"
	"time"
)

func TestSleep(t *testing.T) {
	time.Sleep(10 * time.Millisecond) // finding
	time.Sleep(10 * time.Millisecond) //stylegate:allow-sleep too short to justify
	time.Sleep(10 * time.Millisecond) //stylegate:allow-sleep waits out the 30ms debounce window
}

func TestSetenv(t *testing.T) {
	os.Setenv("A", "1") // finding
}

func TestSkip(t *testing.T) {
	t.Skip()                   // finding: no reason
	t.Skipf("dyn %d", 1)       // finding: not a literal reason
	t.Skip("x")                // finding: reason too short
	t.Skip("properly explained reason for skipping") // ok
}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":          fixtureMod,
		"fixture.go":      fixtureProd,
		"fixture_test.go": fixtureTest,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func findingsByRule(rep *Report) map[string][]Finding {
	m := map[string][]Finding{}
	for _, f := range rep.Findings {
		m[f.Rule] = append(m[f.Rule], f)
	}
	return m
}

func TestScanFixtureRules(t *testing.T) {
	dir := writeFixture(t)
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findingsByRule(rep)

	// errorf-verb: 4 sites in the fixture (guarded, unguarded, flags,
	// shadowed). The flags site (%+v) is deliberately NOT a finding, and the
	// mechanically-editable subset is exactly the nil-safe ones.
	if n := len(got[RuleErrorfVerb]); n != 3 {
		t.Errorf("errorf-verb findings = %d, want 3 (%v)", n, got[RuleErrorfVerb])
	}
	editable := 0
	for _, f := range got[RuleErrorfVerb] {
		if f.EditWhat == "%v->%w" {
			editable++
			if f.EditLen != 1 {
				t.Errorf("edit length = %d, want 1", f.EditLen)
			}
		}
	}
	if editable != 2 {
		t.Errorf("editable errorf-verb sites = %d, want 2 (guarded + shadowed-outer)", editable)
	}

	// The %+v site (fixture.go line 22) must not be reported at all.
	for _, f := range got[RuleErrorfVerb] {
		if f.Line == 22 {
			t.Errorf("line 22 is %%+v and must not be a finding: %+v", f)
		}
	}

	// ignored-error: os.Hostname / os.Setenv / os.Remove, minus the one with
	// the documented deliberate-ignore comment.
	// Three: the shadowed `_ = err`, the multi-value `_, _ = os.Hostname()`
	// and `_ = os.Setenv(...)`. The fourth site carries the ignore marker and
	// must NOT be reported.
	if n := len(got[RuleIgnoredError]); n != 3 {
		t.Errorf("ignored-error findings = %d, want 3 (%v)", n, got[RuleIgnoredError])
	}
	for _, f := range got[RuleIgnoredError] {
		if f.Callee == "" {
			t.Errorf("finding without a callee key: %+v", f)
		}
	}
	// The marker line must be genuinely absent from the report.
	for _, f := range got[RuleIgnoredError] {
		if f.Line == 41 {
			t.Errorf("line 41 declares an ignore reason and must not be reported: %+v", f)
		}
	}

	if n := len(got[RuleUncheckedClose]); n != 1 {
		t.Errorf("unchecked-close findings = %d, want 1 (%v)", n, got[RuleUncheckedClose])
	}
	if rep.DeferredCloses != 1 {
		t.Errorf("deferred closes = %d, want 1", rep.DeferredCloses)
	}
	if n := len(got[RuleTestSleep]); n != 1 {
		t.Errorf("test-sleep findings = %d, want 1 (%v)", n, got[RuleTestSleep])
	}
	if n := len(got[RuleTestSetenv]); n != 1 {
		t.Errorf("test-setenv findings = %d, want 1 (%v)", n, got[RuleTestSetenv])
	}
	if n := len(got[RuleTestSkip]); n != 3 {
		t.Errorf("test-skip findings = %d, want 3 (%v)", n, got[RuleTestSkip])
	}
}

// TestNilSafetyIsObjectScoped pins the soundness property the whole %v→%w
// sweep rests on: a guard on one object must never authorise an edit on a
// different object that merely shares the name.
//
// TestSetenvScopeIsPerCall pins the rule added AFTER the sweep shipped: an
// automated os.Setenv→t.Setenv conversion inside a closure is a REAL bug (my
// own Batch 3 introduced it — cmd/agent's dispatch test then asserted about
// the wrong ledger, bisected to a73e063). t.Setenv's undo runs at TEST
// CLEANUP, so a per-call flip never unwinds where the author meant it to.
// The three fixture closures pin the discrimination:
//
//	closure(computed value)   → FINDING   (the hazard: per-call flip)
//	closure(constant value)   → clean     (a table case cannot mislead a reader)
//	func(t *testing.T){...}   → clean     (subtest owns its T; the scope is right)
func TestSetenvScopeIsPerCall(t *testing.T) {
	// perCallSetenvLine is the 1-based line of the t.Setenv inside the
	// per-call closure in the fixture below. Keep it in sync with src: the
	// assertion's whole point is that the OTHER two closure shapes stay clean.
	const perCallSetenvLine = 10
	dir := t.TempDir()
	src := `package p

import (
	"os"
	"testing"
)

func TestPerCallFlip(t *testing.T) {
	hook := func(name string) {
		t.Setenv("GOPI_MEMORY_DB", name) // finding: per-call value, wrong undo scope
	}
	table := []func(){
		func() { t.Setenv("GOPI_MEMORY_DB", "constant.db") }, // clean: constant
	}
	t.Run("sub", func(t *testing.T) {
		t.Setenv("GOPI_MEMORY_DB", "sub.db") // clean: the subtest owns this T
	})
	_ = os.Getenv
	_ = hook
	_ = table
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scope_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write scope_test.go: %v", err)
	}
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findingsByRule(rep)[RuleTestSetenvScope]
	if len(got) != 1 {
		t.Fatalf("test-setenv-scope findings = %d, want exactly 1 (the per-call closure): %+v", len(got), got)
	}
	if got[0].Line != perCallSetenvLine {
		t.Errorf("the finding must be the per-call closure (line %d), got line %d", perCallSetenvLine, got[0].Line)
	}
	for _, f := range findingsByRule(rep)[RuleTestSetenv] {
		t.Errorf("the constant/subtest sites must not be test-setenv findings either: %+v", f)
	}
}

// TestChdirRuleFlagsStatementPositionChdir pins RuleTestChdir's
// discrimination: a cwd change whose error nobody looks at is reported, while
// the correct shapes stay clean. The rule exists because a silently failed
// RESTORE leaves every later test in the binary inside a removed
// t.TempDir() — the cascade it causes (relative-path opens, `go build .` in
// the wrong tree) never names the real cause
// (devdoc/test-code-discipline-audit.md §2.2).
func TestChdirRuleFlagsStatementPositionChdir(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import (
	"os"
	"testing"
)

func TestDeferRestore(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd) // finding: deferred restore, unchecked
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRestore(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { os.Chdir(dir) }) // finding: closure restore, unchecked
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestBareChdir(t *testing.T) {
	os.Chdir(t.TempDir()) // finding: bare statement
}

func TestDeferredClosureBody(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	defer func() {
		os.Chdir(dir) // finding: closure body, statement position, unchecked
	}()
}

func TestDeferredClosureChecked(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	defer func() {
		if err := os.Chdir(dir); err != nil { // clean: checked in the body
			t.Errorf("restore cwd: %v", err)
		}
	}()
}

func TestChdirT(t *testing.T) {
	t.Chdir(t.TempDir()) // clean: t.Chdir restores the cwd and fails the test
}

func TestCheckedIn(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(old)
	if err := os.Chdir(t.TempDir()); err != nil { // clean: checked at the call
		t.Fatal(err)
	}
}

func TestHelperReturnsError(t *testing.T) {
	move := func(d string) error { return os.Chdir(d) } // clean: the error travels
	_ = move
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cwd_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write cwd_test.go: %v", err)
	}
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findingsByRule(rep)[RuleTestChdir]
	if len(got) != testChdirFindingCount {
		t.Fatalf("test-chdir findings = %d, want %d (defer + cleanup closure + bare + deferred body): %+v",
			len(got), testChdirFindingCount, got)
	}
	for _, f := range got {
		if f.Callee != "os.Chdir" || f.Head != "os.Chdir" {
			t.Errorf("finding must be keyed to os.Chdir, got %+v", f)
		}
		if f.Detail == "" {
			t.Errorf("finding carries no detail: %+v", f)
		}
	}
}

// testChdirFindingCount is the number of deliberate violation sites in the
// fixture above (the other shapes are the correct ones the rule must spare).
const testChdirFindingCount = 4

// TestNilSafetyIsObjectScoped pins the soundness property the whole %v→%w
// sweep rests on: a guard on one object must never authorise an edit on a
// different object that merely shares the name.
func TestNilSafetyIsObjectScoped(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import "fmt"

func f() error {
	var err error
	if err != nil {
		func() {
			inner := fmt.Errorf("x")
			if inner != nil {
				_ = inner
			}
		}()
		return fmt.Errorf("outer: %v", err)
	}
	return nil
}

func g(err error) error {
	if err != nil {
		_ = err
	}
	var other = err
	_ = other
	if err != nil {
		return fmt.Errorf("second: %v", err)
	}
	return nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Scan(Options{Root: dir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var editable []Finding
	for _, f := range rep.Findings {
		if f.Rule == RuleErrorfVerb && f.EditWhat != "" {
			editable = append(editable, f)
		}
	}
	if len(editable) != 2 {
		t.Fatalf("editable sites = %d, want 2 (both guarded err uses): %v", len(editable), editable)
	}
	for _, f := range editable {
		if f.Line != 14 && f.Line != 26 {
			t.Errorf("unexpected editable line %d", f.Line)
		}
	}
}

// ---------------------------------------------------------------------------
// Escape-recognition shapes the sweep MISSED (2026-09-27). Each shape below is
// one that master measured as a violation even though the site was correctly
// written, which is what the epic calls a gate-side miss. The literal-reason
// case is the one the repo's own conventions produce most often: every
// `t.Skipf("…" + "…", x)` in the tree is written across two source lines, and
// only the bare-literal case was recognised.
// ---------------------------------------------------------------------------

// TestSkipReasonIsAConcatenatedLiteral pins the shape master missed: a reason
// written as a concatenation of literals is ONE string constant to the
// language, and it is a literal reason — the rule exists to refuse a DYNAMIC
// reason (`fmt.Sprintf`, a variable), and a concatenation propagates no runtime
// value. Before the fix the scanner decoded the type checker's constant
// rendering, which TRUNCATES a long value to `"prefix..."`; the raw string then
// cannot be unquoted and the site was reported as "without a literal reason".
func TestSkipReasonIsAConcatenatedLiteral(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import "testing"

func TestConcatReason(t *testing.T) {
	if true {
		t.Skipf("this host cannot generate %s, so the translated-output "+
			"condition cannot be produced here — the deterministic guard "+
			"still covers the pin.", "C.UTF-8")
	}
}

func TestDynamicReason(t *testing.T) {
	reason := "dynamic"
	if true {
		t.Skipf(reason) // finding: not a literal reason
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write skip_test.go: %v", err)
	}
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findingsByRule(rep)[RuleTestSkip]
	if len(got) != 1 {
		t.Fatalf("test-skip findings = %d, want exactly 1 (the dynamic reason): %+v", len(got), got)
	}
	if got[0].Line != 16 {
		t.Errorf("the finding must be the DYNAMIC reason (line 16), got line %d", got[0].Line)
	}
}

// TestSetenvScopeAcceptsADocumentedReason pins the second half of the miss: the
// sibling rules (test-setenv, test-sleep, ignored-error) all accept an explicit
// `//stylegate:ignore <reason>` (or Rule 15's prose form) at the site, but
// test-setenv-scope reported its sites with no way to declare a reviewed
// exception at all — so a correctly reasoned site had no escape to spell.
func TestSetenvScopeAcceptsADocumentedReason(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import "testing"

func TestPerCallFlipDocumented(t *testing.T) {
	hook := func(name string) {
		//stylegate:ignore the flip must undo when THIS HOOK returns, not at test cleanup
		t.Setenv("GOPI_MEMORY_DB", name)
	}
	_ = hook
}

func TestPerCallFlipUndocumented(t *testing.T) {
	hook := func(name string) {
		t.Setenv("GOPI_MEMORY_DB", name) // finding: per-call value, wrong undo scope
	}
	_ = hook
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scope_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write scope_test.go: %v", err)
	}
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := findingsByRule(rep)[RuleTestSetenvScope]
	if len(got) != 1 {
		t.Fatalf("test-setenv-scope findings = %d, want exactly 1 (the undocumented closure): %+v", len(got), got)
	}
	if got[0].Line != 15 {
		t.Errorf("the finding must be the UNDOCUMENTED closure (line 15), got line %d", got[0].Line)
	}
}

// TestSetenvScopeConstantNamedConstIsClean pins the third shape: the rule's own
// doc narrows precision to the PER-CALL flip, and a site installing a NAMED
// constant (EnvTerm, term256 — the ordinary spelling in this repo) installs the
// same value every call, so it cannot mislead a later assertion. The predicate
// used to test the syntactic literal alone, so every named constant read as a
// dynamic value.
func TestSetenvScopeConstantNamedConstIsClean(t *testing.T) {
	dir := t.TempDir()
	src := `package p

import "testing"

const envTerm = "TERM"

func TestConstantNamedConst(t *testing.T) {
	inClosure(t, func(t *testing.T) {
		t.Setenv(envTerm, "xterm-256color") // clean: constant per call
	})
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fixtureMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "const_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write const_test.go: %v", err)
	}
	rep, err := Scan(Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// The closure takes its own *testing.T, so this asserts the predicate too:
	// a named constant must not be reported as a per-call flip.
	for _, f := range findingsByRule(rep)[RuleTestSetenvScope] {
		t.Errorf("a constant-valued t.Setenv is not a per-call flip: %+v", f)
	}
}
