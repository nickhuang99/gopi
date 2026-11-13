package testutil

// The BUDGET half of the suite-hang fix (Rules 1a/2/15): ONE per-package test
// budget, in ONE place, and a timeout that NAMES itself.
//
// WHAT THIS FILE GUARDS
// ---------------------
// Go gives every package a wall-clock test budget and its default is 10
// minutes. On a loaded 4-core host shared with other agents, internal/tools
// (1080 tests, hundreds of fresh SQLite scratch DBs) needs longer than that:
// measured 189 s / 266 s / 386 s quiet, >600 s at load 42-63. The verdict a
// reader sees is then `FAIL github.com/nick/gopi/internal/tools 600.3xx s` —
// text that reads like a failing assertion while proving nothing about the
// code (Rule 15: a failure must NAME itself).
//
// The fix is a raised, DECLARED budget (scripts/test-budget.sh) rather than a
// flag sprinkled through ad-hoc invocations. These gates keep it that way:
//
//  1. the budget constant exists in the script and PARSES as a Go duration;
//  2. `make test` routes through the script (a revert to a bare `go test ./...`
//     re-inherits the 10m default silently — the defect this fixes);
//  3. no other committed dev/test workflow runs `go test` without naming its
//     own `-timeout`;
//  4. the script's timeout report still names the timeout — the one string the
//     whole deliverable exists to make unmistakable.
//
// Every check is static and dependency-free (stdlib + the files themselves), so
// it runs inside the normal `go test ./...` gate.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	// budgetScriptPath is the single source of the per-package budget.
	budgetScriptPath = "scripts/test-budget.sh"
	// budgetMakeTarget is the Makefile target that must route through it.
	budgetMakeTarget = "test:"
	// budgetConstantRe finds the declared budget in the script.
	budgetConstantRe = `(?m)^test_budget="([^"]+)"`
	// budgetTimeoutFlagRe finds a named per-package budget on a `go test` line.
	budgetTimeoutFlagRe = `-timeout[ =]`
	// budgetGoTestRe finds a `go test` INVOCATION line: an optional env-assignment
	// prefix and/or `$(` command substitution, then `go test`. Anchored at line
	// start on purpose — a line that merely mentions `go test` in prose (the
	// coverage helper explaining why it is not `go test -cover`) runs nothing
	// and must not be gated.
	budgetGoTestRe = `^\s*(?:[A-Za-z_][A-Za-z0-9_]*=[^\s]*\s+)*(?:\$\()?go test\b`
	// budgetTimeoutNamer is the phrase the script must print when the budget
	// trips: the reader must be told a TIMEOUT happened, not a failure.
	budgetTimeoutNamer = "TIMEOUT, NOT AN ASSERTION FAILURE"
	// budgetTimeoutEvidenceLabel heads the expiry evidence (the `panic: test
	// timed out after …` line) inside the TIMEOUT block.
	budgetTimeoutEvidenceLabel = "the budget expired here:"
	// budgetTimeoutFailureLabel heads the run's ordinary assertion failures,
	// so they are never presented as the timeout itself.
	budgetTimeoutFailureLabel = "separate assertion failures in the same run (NOT the timeout):"
	// budgetTripMarker is the panic text the script looks for in the JSON
	// stream (`panic: test timed out after 25m0s`).
	budgetTripMarker = "panic: test timed out after"
	// budgetExitCode is the script's dedicated exit code for a budget trip —
	// distinct from 1 (a real failure), which is what made the two
	// indistinguishable before.
	budgetExitCode = "3"
	// budgetExemptScripts are workflows whose `go test` budget is deliberately
	// their own: the e2e smoke drives testsuite/ against a mock LLM, a
	// different suite with a different (much smaller) meaningful budget. An
	// exemption names the file — never a wildcard — and the gate asserts every
	// exempt file still exists, so a deletion cannot quietly widen it.
	// budgetLatestName is the canonical "latest stream" name. The script may
	// carry it ONCE, as a declared constant, and never as a `>` target.
	budgetLatestName = "last.json"
	// budgetLatestConstPrefix is the script's assignment prefix for that
	// constant — the one line allowed to hold the literal (Rule 13).
	budgetLatestConstPrefix = `latest_link_name="`
	// budgetStreamScopeLib is the shared out-of-scope predicate: sourced by the
	// script, and executed directly by the dynamic tests below with synthetic
	// input (so the mingled case is ASSERTED, not waited for).
	budgetStreamScopeLib = "scripts/lib/stream-scope.sh"
	// budgetFailuresBanNeedle is the marker text a passing test can PRINT as its
	// own fixture: it may still be printed (a reader recognises the shape), but
	// selecting rows with it is the defect this gate bans.
	budgetFailuresBanNeedle = `--- FAIL: [^`
	// budgetFailuresReportCall is the executable call that renders the block.
	budgetFailuresReportCall = "report_failures"
	// budgetScopeReportCall is the script's invocation of that report on a
	// completed run — checked on an executable line, never on a comment.
	budgetScopeReportCall = `report_stream_scope_anomaly "${args[@]}"`
	// sealRunStreamCall makes the run's COMPLETED stream read-only before the
	// canonical pointer names it. Without it the pointer is an alias to a
	// writable file, and anything that follows the name (a hand-typed redirect,
	// an out-of-date checkout of this script) truncates finished evidence.
	sealRunStreamCall = `seal_run_stream`
	// publishLatestCall is the pointer publication ordering is checked against.
	publishLatestCall = `publish_latest_stream`
	// budgetShellBin runs the library under test.
	budgetShellBin = "bash"
	// The three env names the dynamic scope test passes its fixture FILES
	// through (values, never positional arguments — a scope pattern may itself
	// be a flag pair, which would shift positional file arguments).
	budgetScopeFileEnv  = "TB_SCOPE_FILE"
	budgetStreamFileEnv = "TB_STREAM_FILE"
	budgetStreamPkgsEnv = "TB_STREAM_PKGS_FILE"
	// budgetScopeFixtureModule is the module path the synthetic scope fixtures
	// are written under — a name no real module uses, so a fixture package can
	// never be confused with a repo package.
	budgetScopeFixtureModule = "example.test/scopefixture"
	// budgetScopeFixturePkg is one package INSIDE the requested scope.
	budgetScopeFixturePkg = budgetScopeFixtureModule + "/internal/tools"
	// budgetScopeForeignPkg is the neighbour's package — the one a shared
	// stream carries and this run did not ask for (the measured defect).
	budgetScopeForeignPkg = budgetScopeFixtureModule + "/internal/agent"
	// budgetScopeOtherModulePkg is a package of a DIFFERENT module: outside
	// every scope by definition (the smoke gate executes the script from a
	// fixture module), so it must never be reported as evidence of mingling.
	budgetScopeOtherModulePkg = "other.test/elsewhere"
	// budgetStreamJSONLine is one `go test -json` event line, formatted with a
	// package + test name.
	budgetStreamJSONLine = `{"Action":"pass","Package":"%s","Test":"%s","Elapsed":0.1}` + "\n"
)

// budgetLatestWriteRe matches a `>` redirection whose target names the
// canonical latest file: the write that made the artifact shared.
var budgetLatestWriteRe = regexp.MustCompile(`>>?\s*[^\s]*` + regexp.QuoteMeta(budgetLatestName))

// budgetExemptScripts is declared as a var (a map is not a constant).
var budgetExemptScripts = map[string]string{
	"scripts/e2e_smoke.sh": "drives testsuite/ (mock-LLM e2e), which declares its own smoke_timeout for a different suite",
	"scripts/e2e-m418-yield.sh": "single-test `-run TestM418YieldProbe` probe: one test cannot reach a 10m " +
		"per-package cap, so the full-suite budget is not the right knob here (and the probe deliberately discards its output)",
	// scripts/e2e-m421-claim-lock.sh USED TO BE EXEMPT HERE. The script drove the
	// cross-node claim lock's E2E (its only in-tree caller was the M4.21 probe
	// matrix), and both were deleted with that protocol -- an exemption for a
	// file that is not in the tree is refused by the gate below, because a permit
	// that outlives its subject hides the next stale one.
	// scripts/docclass-red-proofs.sh USED TO BE EXEMPT HERE. The script and the
	// gates it red-proofed (internal/docclass's manifest/anchor baselines) were
	// deleted with the document pipeline (removal slice 3/5), so an exemption for
	// it would name a file that is not in the tree — and the gate below refuses a
	// stale exemption for exactly that reason.
}

// TestTestBudgetHasASingleSourceAndNamesATimeout is the anti-regression gate
// for the load-sensitivity fix. Without it the budget is a convention: the next
// contributor re-inlines `go test ./...` in the Makefile (or in a workflow) and
// the 600s cliff returns with no artifact recording why.
func TestTestBudgetHasASingleSourceAndNamesATimeout(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	script, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v — the declared per-package test budget is gone", budgetScriptPath, err)
	}
	assertBudgetConstantParses(t, string(script))
	assertBudgetScriptNamesItsTimeout(t, string(script))
	assertMakeTestRoutesThroughTheBudget(t, root)
	assertEveryWorkflowNamesItsBudget(t, root, script)
	assertBudgetTimeoutReportIsBinarySafe(t, string(script))
	assertBudgetStreamIsPerRunAndTheLatestIsAPointer(t, string(script))
	assertBudgetStreamScopeAnomalyIsChecked(t, root, string(script))
	assertBudgetFailuresComeFromTheEvents(t, root, string(script))
	assertBudgetScopePrefixMatchesGoMod(t, root, string(script))
	assertNoQuotedFixedStreamPath(t, string(script))
}

// assertNoQuotedFixedStreamPath is the Rule 13 half of the fix: the shared name
// must exist ONCE, as a declared constant, and never as a quoted literal at a
// use site (a second copy is how the two drift apart — one gets renamed, the
// other keeps writing the old shared target).
func assertNoQuotedFixedStreamPath(t *testing.T, script string) {
	t.Helper()
	definition := 0
	for i, line := range strings.Split(script, "\n") {
		if !strings.Contains(line, `"`+budgetLatestName+`"`) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), budgetLatestConstPrefix) {
			definition++
			continue
		}
		t.Errorf("%s:%d carries the bare literal %q — reference the declared constant (Rule 1b/13):\n%s",
			budgetScriptPath, i+1, budgetLatestName, strings.TrimSpace(line))
	}
	if definition != 1 {
		t.Errorf("%s declares the canonical latest name %d times, want exactly 1 (Rule 1a)",
			budgetScriptPath, definition)
	}
}

// assertBudgetStreamIsPerRunAndTheLatestIsAPointer is the anti-regression gate
// for the SHARED-WRITE-TARGET defect (measured 2026-09-19: every budget run
// redirected `go test -json` into ONE host-wide file with `>`, so a run's
// summary could be rendered from a stream holding a neighbour's events — one
// file held 2422 cmd/agent events followed by 1328 internal/tools events).
//
// It supersedes the earlier "keep a per-run COPY after the fact" contract,
// whose intent (a reported number stays citable, measured 2026-09-13) is
// satisfied better here: the per-run name is the redirect target from the FIRST
// byte, so there is nothing to copy and no window in which two runs share a
// file. Three properties, all machine-checked:
//
//  1. the ONLY `go test -json` redirection targets a per-run variable;
//  2. that variable's name carries the start EPOCH and the PID (the epoch alone
//     is second-granular — two runs launched in the same second would collide,
//     which is the same defect with a smaller window);
//  3. the canonical "latest" name is a POST-HOC symlink, never a write target.
func assertBudgetStreamIsPerRunAndTheLatestIsAPointer(t *testing.T, script string) {
	t.Helper()
	redirected := false
	for _, line := range strings.Split(script, "\n") {
		code := stripComment(line)
		if !strings.Contains(code, "go test") || !strings.Contains(code, "-json") {
			continue
		}
		redirected = true
		if !strings.Contains(code, `>"$raw_json"`) {
			t.Errorf("%s redirects its `go test -json` stream somewhere other than the per-run "+
				"$raw_json: %s\nA fixed (shared) target is the defect this gate exists to prevent — "+
				"a concurrent run truncates it and its events land in this run's summary",
				budgetScriptPath, strings.TrimSpace(line))
		}
		if budgetLatestWriteRe.MatchString(code) {
			t.Errorf("%s writes to the canonical latest name (%s): the pointer must be published "+
				"with `ln -s` + rename, never opened for writing — a shared write target is exactly "+
				"what let a neighbour's events into a run's summary", budgetScriptPath, budgetLatestName)
		}
	}
	if !redirected {
		t.Fatalf("no `go test -json` redirection found in %s — the gate is looking at the wrong "+
			"shape (vacuous pass guard)", budgetScriptPath)
	}
	for _, want := range []string{
		`run_tag="$raw_json_prefix${start_epoch}-$$"`,
		`raw_json="$budget_dir/$run_tag$raw_json_suffix"`,
		`latest_link_name="last.json"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("%s no longer contains %q — the per-run stream name must carry the start "+
				"epoch AND the pid, and the canonical latest name must be a declared constant "+
				"(Rule 1a)", budgetScriptPath, want)
		}
	}
	if !strings.Contains(script, "ln -s ") {
		t.Errorf("%s no longer creates the latest pointer as a symlink — the canonical %s name "+
			"must be a post-hoc pointer to a completed run's stream, never a write target",
			budgetScriptPath, budgetLatestName)
	}
	// The pointer is an ALIAS to this run's evidence, so publishing it without
	// sealing the target first re-opens the same hole one level down: anything
	// that still writes through the name (a hand-typed redirect, an out-of-date
	// checkout of this script) truncates a FINISHED run's stream. Measured
	// 2026-09-19, exactly that destroyed one of two acceptance runs' streams
	// after the run had reported its summary.
	if seal, publish := callLineIndex(script, sealRunStreamCall), callLineIndex(script, publishLatestCall); seal < 0 {
		t.Errorf("%s never calls %s — the COMPLETED stream must be made read-only before %s "+
			"points at it, or a writer following the pointer silently destroys this run's evidence",
			budgetScriptPath, sealRunStreamCall, budgetLatestName)
	} else if publish < 0 {
		t.Errorf("%s never calls %s", budgetScriptPath, publishLatestCall)
	} else if seal > publish {
		t.Errorf("%s seals the stream AFTER publishing the pointer (line %d vs %d) — the window "+
			"between the two is exactly when a writer following %s truncates a finished run's stream",
			budgetScriptPath, seal+1, publish+1, budgetLatestName)
	}
	if !strings.Contains(script, "stream=%s") {
		t.Errorf("%s does not stamp the per-run stream name — the stamp is how a reader finds the "+
			"stream a quoted verdict came from", budgetScriptPath)
	}
}

// assertBudgetStreamScopeAnomalyIsChecked pins the second half of the fix: when
// the stream DOES carry a package the run was not asked for, the script must say
// so instead of summarising it as its own evidence (Rule 15). The predicate is
// delegated to scripts/lib/stream-scope.sh so the gate can call it with
// synthetic input — the mingled case is asserted below, not waited for.
func assertBudgetStreamScopeAnomalyIsChecked(t *testing.T, root, script string) {
	t.Helper()
	for _, want := range []string{
		`stream_scope_lib="scripts/lib/stream-scope.sh"`,
		`. "$root/$stream_scope_lib"`,
		`anomaly_marker="STREAM OUT OF SCOPE"`,
		"gopi_stream_scope_anomalies",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("%s no longer contains %q — a stream carrying a package outside the requested "+
				"scope must be REPORTED (Rule 15), not summarised as this run's own evidence",
				budgetScriptPath, want)
		}
	}
	// The CALL is checked on CODE lines only: a commented-out call still
	// satisfies a substring test, which would let the report be disabled while
	// the gate stayed green (the failure mode a red-proof exists to catch).
	callFound := false
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(stripComment(line), budgetScopeReportCall) {
			callFound = true
			break
		}
	}
	if !callFound {
		t.Errorf("%s no longer CALLS %q on an executable line — the anomaly report would never "+
			"run at the end of a test run", budgetScriptPath, budgetScopeReportCall)
	}
	libPath := filepath.Join(root, filepath.FromSlash(budgetStreamScopeLib))
	raw, err := os.ReadFile(libPath)
	if err != nil {
		t.Fatalf("read %s: %v — the shared scope predicate is gone", budgetStreamScopeLib, err)
	}
	for _, fn := range []string{"gopi_stream_packages", "gopi_scope_packages", "gopi_stream_scope_anomalies"} {
		if !strings.Contains(string(raw), fn+"()") {
			t.Errorf("%s no longer defines %s() — the script and the gate must share ONE predicate "+
				"definition (Rule 14)", budgetStreamScopeLib, fn)
		}
	}
}

// assertBudgetScopePrefixMatchesGoMod pins the module prefix the scope check
// compares against. The script READS it from the running module's go.mod (the
// branch-scoped smoke gate executes the script inside a fixture module), and
// falls back to a declared constant; that constant must equal this repo's own
// module path or the fallback would mark every package out of scope — a warning
// that cries wolf is one a reader learns to ignore.
func assertBudgetScopePrefixMatchesGoMod(t *testing.T, root, script string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	module := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "module ") {
			module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			break
		}
	}
	if module == "" {
		t.Fatalf("go.mod declares no module line — cannot pin the scope prefix")
	}
	if !strings.Contains(script, `module_import_path_fallback="`+module+`"`) {
		t.Errorf("%s does not declare module_import_path_fallback=%q (this repo's go.mod module "+
			"path) — the fallback would be wrong wherever go.mod is unreadable, and the scope "+
			"check would then flag every package", budgetScriptPath, module)
	}
	if !strings.Contains(script, `"$root/go.mod"`) {
		t.Errorf("%s never reads the module path from go.mod — the scope check must follow the "+
			"module it actually runs in (the branch-scoped smoke gate runs this script inside a "+
			"fixture module)", budgetScriptPath)
	}
}

// assertBudgetTimeoutReportIsBinarySafe pins the fix for a silent failure
// found while exercising the timeout path (see the report's "tripped:" evidence):
// a `go test -json` stream can carry a NUL byte (a test feeding binary data to
// a tool), which makes grep classify the WHOLE stream as binary and print
// nothing — so the timeout block announced a TIMEOUT with an EMPTY list of what
// tripped. That is Rule 15's silent failure wearing the disguise of a correct
// message: the label was right, the evidence was blank. `grep -a` is what makes
// the marker survive.
func assertBudgetTimeoutReportIsBinarySafe(t *testing.T, script string) {
	t.Helper()
	for _, line := range strings.Split(script, "\n") {
		if !strings.Contains(line, "timeout_panic_marker") {
			continue
		}
		if strings.Contains(line, "grep -a") {
			return
		}
	}
	t.Errorf("%s greps the JSON stream for the timeout marker without `grep -a` — a stream "+
		"carrying a NUL byte (binary test data) makes grep print NOTHING, so the TIMEOUT block "+
		"would name no tripped package (silent failure, Rule 15)", budgetScriptPath)
}

// callLineIndex returns the 0-based index of the first line that CALLS name —
// an executable line (comments stripped), not a mention in prose nor the
// function's own definition. -1 when no such call exists.
func callLineIndex(script, name string) int {
	for i, line := range strings.Split(script, "\n") {
		code := strings.TrimSpace(stripComment(line))
		if code == "" {
			continue
		}
		if strings.HasPrefix(code, name+" ") || strings.HasSuffix(code, name) {
			return i
		}
	}
	return -1
}

// assertBudgetConstantParses pins check 1: a declared budget exists and is a
// duration Go will accept. A typo would silently fall back to the 10m default
// (Go rejects the flag and fails the run instead — loud, but on a loaded host
// during someone else's task), so it is checked here, cheaply.
func assertBudgetConstantParses(t *testing.T, script string) {
	t.Helper()
	re := regexp.MustCompile(budgetConstantRe)
	m := re.FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s declares no `test_budget=\"…\"` — the per-package budget must live there "+
			"(Rule 1a: one named constant, one place)", budgetScriptPath)
	}
	if _, err := time.ParseDuration(m[1]); err != nil {
		t.Errorf("%s declares test_budget=%q, which Go cannot parse: %v", budgetScriptPath, m[1], err)
	}
	if !strings.Contains(script, "-timeout \"$test_budget\"") {
		t.Errorf("%s declares a budget but never passes it to `go test -timeout` — the declared "+
			"value would be inert", budgetScriptPath)
	}
}

// assertBudgetScriptNamesItsTimeout pins check 4: the script must still turn a
// budget trip into an unmistakable message and a distinct exit code.
func assertBudgetScriptNamesItsTimeout(t *testing.T, script string) {
	t.Helper()
	if !strings.Contains(script, budgetTimeoutNamer) {
		t.Errorf("%s no longer prints %q — a budget trip must be labelled as a TIMEOUT "+
			"(Rule 15: never let it read as an assertion failure)", budgetScriptPath, budgetTimeoutNamer)
	}
	if !strings.Contains(script, budgetTripMarker) {
		t.Errorf("%s no longer detects %q in the test stream — it could not tell a wall-clock "+
			"trip from a failing assertion", budgetScriptPath, budgetTripMarker)
	}
	if !strings.Contains(script, "exit_code=3") && !strings.Contains(script, "exit "+budgetExitCode) {
		t.Errorf("%s no longer exits %s on a budget trip — a distinct exit code is what makes "+
			"the timeout scriptable", budgetScriptPath, budgetExitCode)
	}
	// The TIME-OUT evidence and an ordinary assertion failure must be labelled
	// apart: the first acceptance run to trip the budget printed real `--- FAIL:`
	// lines under the timeout header, which reads as "these tests timed out".
	// Each label names the other explicitly, so a reader never has to guess.
	for _, marker := range []string{budgetTimeoutEvidenceLabel, budgetTimeoutFailureLabel} {
		if !strings.Contains(script, marker) {
			t.Errorf("%s no longer prints %q — a budget trip must present the expiry evidence and the "+
				"run's ordinary failures as two different things (Rule 15: the failure names itself)",
				budgetScriptPath, marker)
		}
	}
}

// assertBudgetFailuresComeFromTheEvents pins the STATIC half of the failure-list
// fix (the dynamic half is testbudget_failures_gate_test.go): the block that
// names this run's failures must be rendered from the stream's structured events
// — through the shared predicate — and must NOT be harvested by the text shape
// of a `--- FAIL:` line.
//
// The banned expression is checked on CODE lines only, and the check does NOT
// forbid the string outright: the readable marker is still PRINTED (a reader
// recognises `--- FAIL: <test>`), it is simply no longer used to SELECT rows —
// a PASSING test prints that same line whenever it quotes a `go test` result
// (measured 2026-10-12: 2 of 6 reported "failures" were exactly that, and
// neither name exists as a test function). The distinction the gate can hold is
// therefore WHERE the literal appears: inside a printf format (printing), never
// as a grep pattern (selecting).
func assertBudgetFailuresComeFromTheEvents(t *testing.T, root, script string) {
	t.Helper()
	for _, want := range []string{
		`stream_failures_lib="scripts/lib/stream-failures.sh"`,
		`. "$root/$stream_failures_lib"`,
		"gopi_stream_failures",
		"gopi_stream_failure_count",
		"gopi_stream_package_failures",
		"report_failures",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("%s no longer contains %q — the failures block must be rendered from the stream's "+
				"STRUCTURED events through the shared predicate (Rule 14), never from the text shape of a "+
				"marker line a passing test can print", budgetScriptPath, want)
		}
	}
	// The CALL must be on an executable line: a commented-out call satisfies a
	// substring test while the block is never rendered.
	callFound := false
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(stripComment(line), budgetFailuresReportCall) {
			callFound = true
			break
		}
	}
	if !callFound {
		t.Errorf("%s never CALLS %q on an executable line — the failures block would never print",
			budgetScriptPath, budgetFailuresReportCall)
	}
	// The banned SELECTOR: `grep … '--- FAIL: …'`. Checked on code lines, where
	// a printf format does not match (the pattern carries the bracket class the
	// format does not).
	for i, line := range strings.Split(script, "\n") {
		code := stripComment(line)
		if !strings.Contains(code, budgetFailuresBanNeedle) {
			continue
		}
		if strings.Contains(code, "grep") || strings.Contains(code, "-o --") {
			t.Errorf("%s:%d still SELECTS failures by the text shape of a marker line — a passing test "+
				"prints that line as its own fixture text, which is the defect:\n%s",
				budgetScriptPath, i+1, strings.TrimSpace(line))
		}
	}
	// The predicate itself lives in the shared library, defining the functions
	// the script calls (Rule 14: one definition, callable by the gate).
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetFailuresLib)))
	if err != nil {
		t.Fatalf("read %s: %v — the shared failure predicate is gone", budgetFailuresLib, err)
	}
	for _, fn := range []string{
		"gopi_stream_failure_events", "gopi_stream_failures", "gopi_stream_failed_packages",
		"gopi_stream_package_failures", "gopi_stream_build_output", "gopi_stream_failure_count",
	} {
		if !strings.Contains(string(raw), fn+"()") {
			t.Errorf("%s no longer defines %s() — the script and the gate must share ONE predicate "+
				"definition (Rule 14)", budgetFailuresLib, fn)
		}
	}
}

// assertMakeTestRoutesThroughTheBudget pins check 2: `make test` is the gate
// every worker and the report templates cite, so it must carry the declared
// budget. A bare `go test ./...` there is the original defect.
func assertMakeTestRoutesThroughTheBudget(t *testing.T, root string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(raw)
	idx := strings.Index(makefile, budgetMakeTarget)
	if idx < 0 {
		t.Fatalf("the Makefile has no %q target — the budget's entry point is gone", budgetMakeTarget)
	}
	body := makefile[idx+len(budgetMakeTarget):]
	if next := strings.Index(body, "\n\n"); next >= 0 {
		body = body[:next]
	}
	if !strings.Contains(body, budgetScriptPath) {
		t.Errorf("the Makefile's %s target does not call %s — `go test ./...` alone re-inherits "+
			"Go's 10m default, the wall-clock cap that a loaded host trips before internal/tools "+
			"finishes (see devdoc/test-budget-single-source.md).\nrecipe:\n%s",
			strings.TrimSuffix(budgetMakeTarget, ":"), budgetScriptPath, body)
	}
}

// assertEveryWorkflowNamesItsBudget pins check 3: any other committed workflow
// that runs `go test` must name its own budget, so no path through the repo can
// re-introduce a silent 10m cap. The budget script itself is the one file that
// satisfies this by construction.
//
// The predicate is per INVOCATION LINE, with comments stripped first: a file
// that merely documents `go test` in prose (the coverage helper explaining why
// it is not `go test -cover`) runs nothing and must not be gated. Requiring the
// flag on the invoking line is what makes the budget visible where a reader
// looks for the wall-clock cap.
func assertEveryWorkflowNamesItsBudget(t *testing.T, root string, script []byte) {
	t.Helper()
	scanned, exempted := 0, 0
	goTestRe := regexp.MustCompile(budgetGoTestRe)
	timeoutRe := regexp.MustCompile(budgetTimeoutFlagRe)
	for _, rel := range auditedWorkflowFiles(t, root) {
		if rel == budgetScriptPath {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		_, exempt := budgetExemptScripts[rel]
		python := strings.HasSuffix(rel, ".py")
		for i, line := range strings.Split(string(raw), "\n") {
			code := stripComment(line)
			if python {
				code = stripPythonComment(line)
			}
			if !goTestRe.MatchString(code) {
				continue
			}
			scanned++
			if exempt {
				exempted++
				continue
			}
			if timeoutRe.MatchString(code) {
				continue
			}
			t.Errorf("%s:%d runs `go test` with no `-timeout` — it inherits Go's 10m per-package cap, "+
				"which a loaded host trips on internal/tools (a 600s FAIL that proves nothing). "+
				"Pass a budget, or route through %s (Rule 1a/2/15)", rel, i+1, budgetScriptPath)
		}
	}
	if scanned == 0 {
		t.Fatalf("the budget gate found no `go test` invocation anywhere in %v — it is looking in the "+
			"wrong place (vacuous pass guard)", auditedScriptRoots)
	}
	t.Logf("budget gate: %d `go test` invocation(s) found, %d exempt by name (of %d exemptions)",
		scanned, exempted, len(budgetExemptScripts))
	_ = script
}

// auditedWorkflowFiles lists the committed dev/test entry points that could run
// a Go test: scripts/, devdoc/ harnesses and testdata/ drivers. It mirrors the
// /tmp gate's roots — one definition of "a workflow file" would be better, but
// the roots live in scripts_gate_test.go and this gate must not silently drift
// from it, so it reads the same relative directories.
func auditedWorkflowFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dirName := range auditedScriptRoots {
		base := filepath.Join(root, dirName)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".sh") && !strings.HasSuffix(name, ".py") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return out
}

// TestBudgetExemptionsStillExist fails a stale exemption in
// budgetExemptScripts: an exemption whose file is gone silently widens the rule
// for a path nobody reviews any more.
func TestBudgetExemptionsStillExist(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	for rel, reason := range budgetExemptScripts {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("budgetExemptScripts exempts %s (%q) but the file does not exist — delete the "+
				"exemption", rel, reason)
		}
	}
}
