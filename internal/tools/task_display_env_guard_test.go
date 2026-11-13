package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// ---------------------------------------------------------------------------
// Family A guard — a display contract must not inherit the ambient process
// signature.
//
// THE DEFECT THIS PREVENTS (devdoc/internal-tools-whole-package-triage-20260925.md,
// family A). internal/tasksource derives a row's provenance from the process's
// own spawn signature (be17a9bd), and config.AgentSpawnSignatureNow reads that
// signature from the ENVIRONMENT. A fixture that asserts a display contract
// without pinning the signature therefore renders one value in a developer's
// plain shell and another inside a gopi session — measured 2026-09-25 on master
// 351736f9, and again while writing this guard: with Gopi's session env exported
// TestTaskShowBriefingFromPayload's demand for `human (gopi task submit CLI)`
// became the worker's provenance, while under `env -i` the SESSION-briefing rows
// (which assert the derivation's NON-human branch) went red instead. Two
// opposite reds, one cause.
//
// WHY A GUARD AND NOT ONLY A FIX. The fix landed once (4f2255ac added the pin;
// family A's own row broadened it to the pin the three fixtures call now), but
// nothing detected the NEXT display contract to be written without it — and the
// ambient direction is INVISIBLE to a bare `go test`, so the reviewer who would
// notice is exactly the one whose environment hides it. This guard makes the
// requirement mechanical.
//
// WHAT IT CHECKS:
//
//  1. COVERAGE — every test function in tasks_test.go that asserts a
//     PROVENANCE-display VALUE (a literal spelling of the submitted_by / source
//     line, e.g. "submitted_by:   human (gopi task submit CLI)") must pin the
//     process signature through testutil.PinBareTerminalProcess. A SHAPE-only
//     assertion ("submitted_by:   ") is not a value contract and needs no pin —
//     asserting the line exists is env-independent.
//  2. LISTING INTEGRITY — every test named in taskDisplayEnvPinnedTests must
//     still exist and still assert the line, so deleting or renaming a guarded
//     test cannot silently retire the guard (Rule 15: a check that can pass by
//     absence is not a check).
//
// WHY THE GUARD READS THE TEST SOURCE. Asserting on RENDERED output would need
// the ambient environment, which is the very variable being controlled.
//
// HONEST LIMITATION (Rule 15 — a check that overstates its coverage is worse than
// one that never landed). The coverage half is a SOURCE scan for a
// provenance-field assertion carrying LITERAL text, so it cannot see two shapes:
// an assertion built as `"submitted_by:   " + someVar` where someVar is itself
// ambient-derived, and an assertion reached through a helper this file cannot
// resolve. Referencing a CONSTANT (`"source:         " + tasksource.Merger`, the
// shape in task_create_unified_test.go) is exempt BY CONSTRUCTION and that is
// intended — a constant reference is the env-independent form this guard exists to
// push fixtures toward. The remaining hole is narrow (it needs a fixture that both
// builds the expectation from a variable AND derives that variable from the
// environment) and it is recorded here rather than papered over.

// taskDisplayEnvGuardFile is this guard's own file name — skipped by the scan, so
// the guard's own fixtures and red-proof strings are never mistaken for the
// contracts it polices.
const taskDisplayEnvGuardFile = "task_display_env_guard_test.go"

// taskDisplayEnvPinnedTests names the provenance-display fixtures that MUST pin
// the process signature before asserting a value. A new value-asserting fixture
// is a violation until it calls the pin.
var taskDisplayEnvPinnedTests = []string{
	"TestTaskShowBriefingFromPayload",
	"TestTaskShowBriefingFromSession",
	"TestTaskShowBriefingMissingSession",
}

// taskDisplayEnvPinCall is the ONE pin helper a fixture must call
// (internal/testutil, Rule 14: its single definition — a fixture may not roll its
// own t.Setenv trio, because a per-test copy would drift from the vocabulary).
const taskDisplayEnvPinCall = "testutil.PinBareTerminalProcess"

// taskDisplayEnvFields are the two provenance-derived lines `task show` renders
// (tasks.go: `source:` and `submitted_by:`, both fed by tasksource's derivation).
// Referenced as a list so a THIRD provenance line added to the renderer is one
// edit here rather than a silent gap.
var taskDisplayEnvFields = []string{taskDisplayEnvSourceField, taskDisplayEnvSubmittedByField}

// taskDisplayEnvSourceField / taskDisplayEnvSubmittedByField are the rendered
// field names. They are the CONTRACT's spellings, and they are spelled once:
// taskDisplayEnvSubmittedByField is asserted against the renderer's own output by
// TestDisplayContractFieldSpellingsMatchTheRenderer below, so a renderer rename
// reddens this guard instead of leaving it scanning for a field nobody prints.
const (
	taskDisplayEnvSourceField      = "source:"
	taskDisplayEnvSubmittedByField = "submitted_by:"
)

// taskDisplayEnvValueAssertRe matches an assertion on a provenance field that
// spells a VALUE — the field name, spacing, then a character that is not the
// closing quote. `"submitted_by:   "` (shape only) does not match;
// `"submitted_by:   human (gopi task submit CLI)"` does.
var taskDisplayEnvValueAssertRe = regexp.MustCompile(
	`"(` + strings.Join(quoteMetaAll(taskDisplayEnvFields), "|") + `)[ ]+[^" ]`)

// taskDisplayEnvAnyAssertRe matches an assertion on a provenance field at all
// (value or shape) — part 2's "this fixture still asserts the contract" test.
var taskDisplayEnvAnyAssertRe = regexp.MustCompile(
	`"(` + strings.Join(quoteMetaAll(taskDisplayEnvFields), "|") + `)`)

// TestDisplayContractsPinTheProcessSignature is the family-A guard: it fails if a
// future edit re-couples a display contract to the worker's provenance.
func TestDisplayContractsPinTheProcessSignature(t *testing.T) {
	sources := readTaskDisplayEnvSources(t)
	// One lookup across every scanned file, for the listing check below.
	bodies := map[string]string{}
	for _, src := range sources {
		for name, body := range src.bodies {
			bodies[name] = body
		}
	}

	for _, src := range sources {
		for name, body := range src.bodies {
			if !taskDisplayEnvValueAssertRe.MatchString(body) {
				continue
			}
			if taskDisplayEnvCoupled(body) {
				t.Errorf("%s asserts a provenance VALUE in %s without pinning the process signature.\n"+
					"Add %s(t) as the fixture's first act: the value it asserts must come from the\n"+
					"renderer's vocabulary, not from whatever environment `go test` was launched in\n"+
					"(a gopi session's shell exports %s and %s, either of which flips the derivation).\n"+
					"Family A: devdoc/internal-tools-whole-package-triage-20260925.md",
					name, src.file, taskDisplayEnvPinCall, config.AgentSessionEnv, config.HermesSessionIDEnv)
			}
		}
	}

	for _, name := range taskDisplayEnvPinnedTests {
		body, ok := bodies[name]
		if !ok {
			t.Errorf("guarded display-contract test %q exists in no scanned test file — the guard\n"+
				"cannot cover a test that is gone. If it was renamed, update\n"+
				"taskDisplayEnvPinnedTests in the same commit; if it was deliberately retired, say\n"+
				"so there and in the report.", name)
			continue
		}
		if !taskDisplayEnvAnyAssertRe.MatchString(body) {
			t.Errorf("guarded display-contract test %q asserts no provenance line at all — the\n"+
				"listing is stale, and a guard covering nothing here reports green while the\n"+
				"contract it was written for is unasserted. Restore the assertion or drop %q from\n"+
				"taskDisplayEnvPinnedTests with a reason.", name, name)
		}
	}

	t.Run("red_proof_the_predicate_catches_an_unpinned_contract", func(t *testing.T) {
		// The child-process proof lives in the report (a real `go test` over a
		// mutated copy of this file, observed RED then GREEN). What is pinned HERE
		// is that the predicate these errors ride on still discriminates — a future
		// edit that relaxes it would otherwise retire the guard silently, since a
		// green guard and a no-op guard look identical.
		unpinned := "func TestX(t *testing.T) {\n\t" +
			`if !strings.Contains(out, "` + taskDisplayEnvValueLiteral() + `") {` + "\n\t}\n}\n"
		pinned := "func TestX(t *testing.T) {\n\t" + taskDisplayEnvPinCall + "(t)\n\t" +
			`if !strings.Contains(out, "` + taskDisplayEnvValueLiteral() + `") {` + "\n\t}\n}\n"
		shapeOnly := "func TestX(t *testing.T) {\n\t" +
			`if !strings.Contains(out, "` + taskDisplayEnvSubmittedByField + `   ") {` + "\n\t}\n}\n"
		if !taskDisplayEnvCoupled(unpinned) {
			t.Errorf("RED PROOF FAILED: the predicate accepted an unpinned VALUE assertion, so the\n"+
				"guard cannot detect the defect it exists for:\n%s", unpinned)
		}
		if taskDisplayEnvCoupled(pinned) {
			t.Errorf("RED PROOF FAILED: the predicate rejected a fixture that DOES pin — it would\n"+
				"fail every correct edit:\n%s", pinned)
		}
		if taskDisplayEnvCoupled(shapeOnly) {
			t.Errorf("RED PROOF FAILED: a shape-only assertion is env-independent and must not be\n"+
				"demanded to pin, else the guard cries wolf and gets deleted:\n%s", shapeOnly)
		}
	})
}

// TestDisplayContractFieldSpellingsMatchTheRenderer ties the two field names this
// guard scans for to the renderer's OWN output, so the guard cannot decay into
// scanning for a field nobody prints: if `task show` renames `submitted_by:`, this
// fails and the constants move with it (Rule 13 — one spelling, referenced).
func TestDisplayContractFieldSpellingsMatchTheRenderer(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := TaskNew("spelling fixture", "t-field-spelling", "sess-spelling", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow("t-field-spelling")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, field := range taskDisplayEnvFields {
		if !strings.Contains(out, field) {
			t.Errorf("the renderer no longer prints %q, so the family-A guard scans for a field\n"+
				"that does not exist. Move the constant with the renderer:\n%s", field, out)
		}
	}
}

// taskDisplayEnvCoupled reports whether a body asserts a provenance VALUE without
// the pin — the guard's own predicate, factored out so the red-proof above
// exercises the real decision instead of a paraphrase of it.
func taskDisplayEnvCoupled(body string) bool {
	if !taskDisplayEnvValueAssertRe.MatchString(body) {
		return false
	}
	return !strings.Contains(body, taskDisplayEnvPinCall)
}

// taskDisplayEnvValueLiteral builds the value-shaped assertion the red-proof feeds
// the predicate. It is assembled from its two halves so this guard file does not
// itself carry the bare literal it forbids elsewhere — a gate that trips on its own
// escape hatch is a gate nobody can read.
func taskDisplayEnvValueLiteral() string {
	return taskDisplayEnvSubmittedByField + "   human (gopi task submit CLI)"
}

// taskDisplayEnvSource is one scanned test file: its name (for the failure
// message) and the extracted fixtures.
type taskDisplayEnvSource struct {
	file   string
	bodies map[string]string
}

// readTaskDisplayEnvSources reads EVERY test file in this package and extracts
// their fixtures.
//
// SCOPE IS THE WHOLE PACKAGE, not one file — the guard's first draft scanned only
// tasks_test.go, and the red-proof that replaced a CONSTANT reference with a
// literal in task_create_unified_test.go went undetected (`ok`, the mutation
// invisible). A guard over one file answers "is this file clean", which is not the
// question: a provenance contract can be asserted from any test file in the
// package, and one package is one suite sharing one ambient environment.
//
// packageDir is resolved from this file's OWN location (runtime.Caller), so the
// guard works whatever cwd the suite runs in.
func readTaskDisplayEnvSources(t *testing.T) []taskDisplayEnvSource {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller: cannot locate the guard's own directory")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []taskDisplayEnvSource
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") || name == taskDisplayEnvGuardFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out = append(out, taskDisplayEnvSource{file: name, bodies: newTaskDisplayEnvScanner(t, string(data)).bodies()})
	}
	if len(out) == 0 {
		t.Fatalf("no _test.go files found beside the guard in %s — the scan would pass vacuously", dir)
	}
	return out
}

// taskDisplayEnvScanner finds test function bodies in one file's source.
type taskDisplayEnvScanner struct {
	t   *testing.T
	src string
}

func newTaskDisplayEnvScanner(t *testing.T, src string) *taskDisplayEnvScanner {
	t.Helper()
	return &taskDisplayEnvScanner{t: t, src: src}
}

// bodies extracts every `func TestX(t *testing.T)` body, keyed by function name.
//
// Brace counting runs over a MASK in which string/rune/comment contents are
// blanked, because the fixtures contain braces inside literals (JSON payloads in
// raw strings, `{` in format text). Counting over the raw text would close a body
// early and let a violating fixture hide past the truncation point — the exact way
// a text guard passes while covering nothing.
func (s *taskDisplayEnvScanner) bodies() map[string]string {
	s.t.Helper()
	mask := s.masked()
	bodies := map[string]string{}
	const marker = "\nfunc "
	for i := 0; ; {
		j := strings.Index(mask[i:], marker)
		if j < 0 {
			break
		}
		start := i + j + 1 // index of "func " in both src and mask
		rest := mask[start:]
		if !strings.HasPrefix(rest, "func Test") {
			i = start + len("func")
			continue
		}
		// The name lives AFTER the "func " keyword, so the search starts past it —
		// scanning from 0 finds the keyword's own space and yields an empty name.
		after := rest[len("func "):]
		nameEnd := strings.IndexAny(after, "( ")
		if nameEnd < 0 {
			s.t.Fatalf("malformed source: cannot read the name of %q", rest[:40])
		}
		name := strings.TrimSpace(after[:nameEnd])
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			s.t.Fatalf("malformed source: func %s has no body", name)
		}
		depth, end := 0, -1
		for k := open; k < len(rest); k++ {
			switch rest[k] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = k
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			s.t.Fatalf("malformed source: func %s body is unbalanced", name)
		}
		// Bodies are sliced from src (the real text): the mask exists only for
		// counting, and the assertions the guard looks for live inside literals.
		bodies[name] = s.src[start+open : start+end+1]
		i = start + len("func")
	}
	return bodies
}

// masked returns src with the CONTENT of every string, rune literal and comment
// replaced by spaces, so brace counting sees only real code. Newlines are
// preserved so byte offsets and line structure stay aligned with src.
func (s *taskDisplayEnvScanner) masked() string {
	src := s.src
	out := []byte(src)
	blank := func(from, to int) {
		for k := from; k < to && k < len(out); k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	for i := 0; i < len(src); {
		switch src[i] {
		case '"':
			j := i + 1
			for j < len(src) && src[j] != '"' && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(src) && src[j] == '"' {
				j++
			}
			blank(i, j)
			i = j
		case '`':
			j := i + 1
			for j < len(src) && src[j] != '`' {
				j++
			}
			if j < len(src) {
				j++
			}
			blank(i, j)
			i = j
		case '\'':
			j := i + 1
			for j < len(src) && src[j] != '\'' && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(src) && src[j] == '\'' {
				j++
			}
			blank(i, j)
			i = j
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				j := i
				for j < len(src) && src[j] != '\n' {
					j++
				}
				blank(i, j)
				i = j
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				j := i + 2
				for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
					j++
				}
				j += 2
				if j > len(src) {
					j = len(src)
				}
				blank(i, j)
				i = j
				continue
			}
			i++
		default:
			i++
		}
	}
	return string(out)
}

// quoteMetaAll escapes every field name for the alternation above.
func quoteMetaAll(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, regexp.QuoteMeta(f))
	}
	return out
}
