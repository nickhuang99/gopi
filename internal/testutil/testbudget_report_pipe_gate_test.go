package testutil

// THE REPORT-PIPE GATE: a GREEN budget report may not print pipe noise.
//
// WHAT IS GUARDED
// ---------------
// The report's "which tests ran" list is produced by a shell helper in
// scripts/test-budget.sh (`list_executed_tests`). Measured 2026-10-13 on a real
// green run of ./cmd/agent (4383 tests, exit=0 outcome=pass, 5.0 MB stream), the
// report printed these two lines in the middle of its summary:
//
//	sort: write failed: 'standard output': Broken pipe
//	sort: write error
//
// The shape was `… | sort -u | head -N`: `head` exits at the cap and closes the
// pipe, the still-writing `sort` gets SIGPIPE, and `set -uo pipefail` raises the
// pipeline's status to sort's 2 — so the noise lands in a GREEN report and reads
// like a defect of the run under report (Rule 15). The same file had already
// fixed the identical shape elsewhere (`list_failure_tests`' body slice, whose
// comment names the defect); the helper was missed, and nothing checked.
//
// WHY THIS GATE RUNS THE SHELL AND NOT THE BYTES
// ----------------------------------------------
// Any warning against producing a pipe is reproducible BY PRODUCING IT: this
// file extracts the shipped helper's body from the script (brace-matched, so
// what runs is the shipped bytes and not a transcription), runs it under the
// script's own `set -uo pipefail` against a stream holding MORE distinct test
// names than the cap reports, and requires stderr to be empty. The reverted
// shape runs beside it as the POSITIVE CONTROL — it must produce the two noise
// lines, on the same fixture, in the same shell. A green arm that is silent
// because the fixture is too small to fill the pipe buffer is therefore
// impossible: the control would be silent too, and the control is asserted.
//
// The output is pinned as well, because "no noise" is not the only requirement:
// the fix has to keep the helper's behaviour (the first N names, de-duplicated
// and sorted — the legacy arm's stdout and the shipped arm's stdout must be the
// same bytes, and must equal the expectation computed here).
//
// The cap and the action word are READ FROM THE SCRIPT (Rule 13): a second copy
// of either in this file is how the fixture and the helper drift apart.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// budgetReportPipeFnName is the report helper this gate guards.
	budgetReportPipeFnName = "list_executed_tests"
	// budgetReportPipeCapConst / …ActionConst are the script's own constants:
	// the fixture is built and the expectations are computed from their values,
	// never from a literal repeated here.
	budgetReportPipeCapConst    = "executed_names_report_n"
	budgetReportPipeActionConst = "executed_action"
	// budgetReportPipeConsumerPipeline matches a pipeline stage that closes its
	// input early — the shape the helper must not carry.
	budgetReportPipeConsumerPipeline = `\|\s*head\b`
	// budgetReportPipeSortFailure / …SortWriteError are the two stderr lines the
	// control arm must reproduce. They are asserted, deliberately: they are the
	// observable this whole gate exists for, and a fixture that stopped
	// producing them must fail the control rather than pass silently.
	budgetReportPipeSortFailure    = "write failed: 'standard output': Broken pipe"
	budgetReportPipeSortWriteError = "sort: write error"
	// budgetReportPipeNoInheritedSigPipe is how the arm's shell is told to ignore
	// SIGPIPE itself. The suite runs with SIGPIPE IGNORED (Go's own runtime
	// disposition for a test process, inherited by every child it spawns), and a
	// shell that inherits SIG_IGN gives it to its children — `sort` then meets
	// the closed pipe with EPIPE, a write(2) error, and exits 2 WITHOUT printing
	// anything (measured 2026-10-13: the same runner printed the two noise lines
	// under `bash` and nothing under a Go-spawned `bash`, exit 141). The control
	// arm has to meet the defect the way the real entry point does — a shell
	// with the DEFAULT SIGPIPE disposition — so the arm restores it explicitly.
	budgetReportPipeNoInheritedSigPipe = `trap "" PIPE`
	// budgetReportPipeCapacityBytes is this host's pipe buffer (F_GETPIPE_SZ =
	// 65536, measured 2026-10-13). The fixture must exceed it, or the producer's
	// write completes before the consumer exits and nothing is observed.
	budgetReportPipeCapacityBytes = 65536
	// budgetReportPipeFixtureRows is how many distinct tests the fixture stream
	// declares: far above the cap (so the slice bites) and far above the pipe
	// buffer (so the control fires).
	budgetReportPipeFixtureRows = 4096
	// budgetReportPipeFixturePkg / …TestPrefix spell the fixture's own events.
	budgetReportPipeFixturePkg        = "github.com/nickhuang99/gopi/internal/budgetpipefixture"
	budgetReportPipeFixtureTestPrefix = "TestBudgetReportPipeFixture"
	// budgetReportPipeChildLocale is the message locale every arm PINS onto the
	// shell it spawns. "C" makes `sort`'s and `grep`'s diagnostics byte-stable
	// across hosts — see budgetReportPipeChildEnv for the measurement that
	// forces it.
	budgetReportPipeChildLocale = "C"
	// budgetReportPipeEnvAssignment marks an environment entry as a KEY=VALUE
	// pair, so the name can be split off without assuming a separator position.
	budgetReportPipeEnvAssignment = "="
	// budgetReportPipeLocaleEnvNames / …NoiseProducerNames are slices (not
	// constants — a Go array is not a constant expression); they are declared as
	// vars beside the const block below.
)

// budgetReportPipeLocaleEnvNames are the four variables that select a gettext
// message catalogue: the two the pin WRITES (LANG, LC_ALL) and the two selectors
// it STRIPS (LC_MESSAGES, LANGUAGE — LC_ALL outranks them, but a leftover
// pointing at a translation this arm did not choose is a second, silent input).
var budgetReportPipeLocaleEnvNames = []string{"LANG", "LC_ALL", "LC_MESSAGES", "LANGUAGE"}

// budgetReportPipeNoiseProducerNames are the program-name prefixes a pipe
// control's stderr must carry, one per non-empty line. THIS IS THE
// LOCALE-INDEPENDENT OBSERVABLE: gettext translates the message BODY, never the
// `sort:`/`grep:` prefix, so a control whose producer died on a closed pipe is
// recognisable in every catalogue (measured 2026-10-16 — the zh_CN failure was
// `sort: 写入失败: 标准输出: 断开的管道`, whose prefix is byte-identical to the
// C one).
var budgetReportPipeNoiseProducerNames = []string{"sort:", "grep:"}

// budgetReportPipeArm is one run of the helper under test.
type budgetReportPipeArm struct {
	exit   int
	stdout string
	stderr string
}

// TestBudgetReportNeverPrintsPipeNoise is the acceptance gate: the shipped
// helper is silent on the real entry point's own shell options, the reverted
// shape beside it is not, and both print the same names.
func TestBudgetReportNeverPrintsPipeNoise(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v — the declared per-package test budget is gone", budgetScriptPath, err)
	}
	script := string(raw)

	body := budgetReportPipeFuncBody(t, script)
	assertBudgetReportHelperCarriesNoEarlyExitConsumer(t, body)
	shellOptions := budgetReportPipeShellOptions(t, script)

	cap := budgetReportPipeIntConst(t, script, budgetReportPipeCapConst)
	action := budgetReportPipeStringConst(t, script, budgetReportPipeActionConst)

	names := budgetReportPipeFixtureNames()
	if len(names) <= cap {
		t.Fatalf("the fixture declares %d distinct tests but the report cap is %d — the slice would never "+
			"be exercised, so a green arm would prove nothing", len(names), cap)
	}

	dir := t.TempDir()
	streamPath := filepath.Join(dir, "fixture-stream.json")
	writeBudgetReportPipeFixture(t, streamPath, action, names)
	if got := budgetReportPipeFixtureBytes(t, streamPath); got <= budgetReportPipeCapacityBytes {
		t.Fatalf("the fixture stream is %d bytes; it must exceed the %d-byte pipe buffer or the producer's "+
			"write finishes before the consumer exits and neither arm observes anything",
			got, budgetReportPipeCapacityBytes)
	}

	legacyBody := filepath.Join(dir, "legacy-body.sh")
	if err := os.WriteFile(legacyBody, []byte(budgetReportPipeLegacyBody), filePerm); err != nil {
		t.Fatalf("write the control arm's helper: %v", err)
	}
	shippedBody := filepath.Join(dir, "shipped-body.sh")
	if err := os.WriteFile(shippedBody, []byte(body), filePerm); err != nil {
		t.Fatalf("write the shipped helper's body: %v", err)
	}

	// The helper prints `package  test` (the sed stage joins the two fields with
	// two spaces), sorted and de-duplicated; the expectation is built from the
	// fixture's own names so a change in either half shows up as a diff.
	wantLines := make([]string, 0, cap)
	for _, name := range names[:cap] {
		wantLines = append(wantLines, budgetReportPipeFixturePkg+"  "+name)
	}
	want := strings.Join(wantLines, "\n") + "\n"

	control := runBudgetReportPipeArm(t, dir, streamPath, legacyBody, cap, action, shellOptions)
	// TWO halves, and the first is the one that must never depend on the host.
	//
	// (a) LOCALE-INDEPENDENT: the reverted shape must make its producer SPEAK —
	// every non-empty stderr line carrying the producer's program prefix. That
	// is the defect itself, and no catalogue can hide it.
	if !budgetReportPipeMatchProducerNoise(control.stderr) {
		t.Fatalf("the reverted shape did NOT reproduce the report noise on a %d-test fixture (%d bytes) — the "+
			"control proves nothing, so this gate cannot see a regression. Raise %s so the producer "+
			"out-writes the pipe buffer.\nstderr: %q",
			len(names), budgetReportPipeFixtureBytes(t, streamPath), "budgetReportPipeFixtureRows", control.stderr)
	}
	// (b) THE PIN'S OWN WITNESS: the same two English lines. This is asserted
	// only because the arm PINS the child's locale (budgetReportPipeChildEnv) —
	// with the pin in force it is deterministic, and it fails loudly if someone
	// removes the pin, which is a real regression of this gate's readability.
	// Measured 2026-10-16: without the pin, the identical arm printed
	// `sort: 写入失败: 标准输出: 断开的管道` under the operator's LANG=zh_CN.UTF-8.
	if !strings.Contains(control.stderr, budgetReportPipeSortFailure) ||
		!strings.Contains(control.stderr, budgetReportPipeSortWriteError) {
		t.Fatalf("the reverted shape reproduced a producer error (%q) but NOT the English text this host's "+
			"locale pin should have produced — the arm's message locale is not the one "+
			"budgetReportPipeChildEnv pins (%q), so the control's observable has become a property of the "+
			"environment again.\nstderr: %q",
			budgetReportPipeNoiseProducerNames, budgetReportPipeChildLocale, control.stderr)
	}
	if control.exit == 0 {
		t.Fatalf("the reverted shape exited 0 — the noise it printed does not raise the pipeline's status on " +
			"this shell, so the pre-fix report was noisy but the pre-fix exit code was not (measured: 2)")
	}

	shipped := runBudgetReportPipeArm(t, dir, streamPath, shippedBody, cap, action, shellOptions)
	if shipped.stderr != "" {
		t.Fatalf("the SHIPPED report helper wrote to stderr:\n%q\n"+
			"A green report must be silent: this is the noise the run under report gets blamed for (Rule 15).", shipped.stderr)
	}
	if shipped.exit != 0 {
		t.Fatalf("the shipped helper exited %d, want 0 (stderr %q)", shipped.exit, shipped.stderr)
	}
	if shipped.stdout != want {
		t.Fatalf("the shipped helper's names changed.\ngot:\n%s\nwant (the first %d of %d distinct tests, "+
			"de-duplicated and sorted):\n%s", shipped.stdout, cap, len(names), want)
	}
	if control.stdout != shipped.stdout {
		t.Fatalf("the fix changed the report's behaviour.\nbefore:\n%s\nafter:\n%s", control.stdout, shipped.stdout)
	}
	if got := strings.Count(shipped.stdout, "\n"); got != cap {
		t.Fatalf("the report lists %d name(s), want the cap %d (executed_names_report_n)", got, cap)
	}
}

// assertBudgetReportHelperCarriesNoEarlyExitConsumer is the cheap structural
// half: the shipped body must not carry a pipeline into a consumer that exits
// at the cap. Comments are stripped first — the helper DOCUMENTS the defect it
// avoids, and the documentation names the shape.
func assertBudgetReportHelperCarriesNoEarlyExitConsumer(t *testing.T, body string) {
	t.Helper()
	re := regexp.MustCompile(budgetReportPipeConsumerPipeline)
	for i, line := range strings.Split(body, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "#") {
			continue
		}
		if re.MatchString(code) {
			t.Errorf("%s: the report helper pipes into an early-exit consumer (line %d):\n%s\n"+
				"`head` closes the pipe at the cap, the producer gets SIGPIPE, and `set -uo pipefail` prints "+
				"`sort: … Broken pipe` in the middle of a GREEN report. Slice captured text with awk instead — "+
				"the shape %s' body slice uses.", budgetScriptPath, i+1, code, "list_failure_tests")
		}
	}
}

// budgetReportPipeFuncBody returns the helper's shipped source, brace-matched
// from its definition to the first closing brace in column 1.
func budgetReportPipeFuncBody(t *testing.T, script string) string {
	t.Helper()
	start := strings.Index(script, budgetReportPipeFnName+"() {")
	if start < 0 {
		t.Fatalf("%s: %s() is gone — the report helper this gate guards no longer exists (Rule 15)",
			budgetScriptPath, budgetReportPipeFnName)
	}
	rest := script[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("%s: %s() has no closing brace in column 1 — this gate cannot extract the shipped body",
			budgetScriptPath, budgetReportPipeFnName)
	}
	return rest[:end+len("\n}\n")]
}

// budgetReportPipeIntConst / …StringConst read a declared constant out of the
// script, so the fixture and the expectations follow the script (Rule 13).
func budgetReportPipeIntConst(t *testing.T, script, name string) int {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + name + `=([0-9]+)\s*$`).FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s: %s is not declared as a bare integer — the report's cap has moved into a shape this "+
			"gate cannot read", budgetScriptPath, name)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s: %s=%q does not parse: %v", budgetScriptPath, name, m[1], err)
	}
	return n
}

func budgetReportPipeStringConst(t *testing.T, script, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^` + name + `="([^"]*)"\s*$`).FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("%s: %s is not declared as a quoted string — the event field this fixture writes has moved "+
			"into a shape this gate cannot read", budgetScriptPath, name)
	}
	return m[1]
}

// budgetReportPipeFixtureNames is the fixture's test set: budgetReportPipeFixtureRows
// distinct names, one event per name.
func budgetReportPipeFixtureNames() []string {
	names := make([]string, 0, budgetReportPipeFixtureRows)
	for i := 0; i < budgetReportPipeFixtureRows; i++ {
		names = append(names, budgetReportPipeFixtureTestPrefix+strconv.Itoa(i))
	}
	sort.Strings(names)
	return names
}

// writeBudgetReportPipeFixture writes a `go test -json` stream whose only events
// are the ones the helper selects — Action/ Package / Test, in the field order
// the helper's pattern requires, one event per distinct test name.
func writeBudgetReportPipeFixture(t *testing.T, path, action string, names []string) {
	t.Helper()
	var b strings.Builder
	for _, name := range names {
		b.WriteString(`{"Time":"2026-10-13T19:00:00+08:00","Action":"`)
		b.WriteString(action)
		b.WriteString(`","Package":"`)
		b.WriteString(budgetReportPipeFixturePkg)
		b.WriteString(`","Test":"`)
		b.WriteString(name)
		b.WriteString(`","Elapsed":0.01}`)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), filePerm); err != nil {
		t.Fatalf("write the fixture stream %s: %v", path, err)
	}
}

// runBudgetReportPipeArm runs one helper body in the script's own shell options,
// against the fixture, with the script's own constant VALUES passed in.
func runBudgetReportPipeArm(t *testing.T, dir, streamPath, bodyPath string, namesCap int, action, shellOptions string) budgetReportPipeArm {
	t.Helper()
	runner := filepath.Join(dir, "arm-"+filepath.Base(bodyPath)+".sh")
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	// The ONE shell-option line the script itself sets: it is what turns the
	// producer's SIGPIPE into the report's status, so a runner without it would
	// not measure the defect.
	b.WriteString(budgetReportPipeNoInheritedSigPipe + "\n")
	b.WriteString(shellOptions + "\n")
	b.WriteString("raw_json=" + budgetReportPipeQuote(streamPath) + "\n")
	b.WriteString(budgetReportPipeActionConst + "=" + budgetReportPipeQuote(action) + "\n")
	b.WriteString(budgetReportPipeCapConst + "=" + strconv.Itoa(namesCap) + "\n")
	b.WriteString(". " + budgetReportPipeQuote(bodyPath) + "\n")
	b.WriteString(budgetReportPipeFnName + "\n")
	if err := os.WriteFile(runner, []byte(b.String()), filePerm); err != nil {
		t.Fatalf("write the arm runner %s: %v", runner, err)
	}

	cmd := exec.Command("bash", runner)
	cmd.Dir = dir
	// The arm's message locale is an INPUT to this gate, not an ambient
	// accident: without this line the producer inherits the test process's
	// environment verbatim and speaks whatever catalogue the operator's shell
	// selects (see budgetReportPipeChildEnv).
	cmd.Env = budgetReportPipeChildEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	arm := budgetReportPipeArm{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return arm
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("run %s: %v", runner, err)
	}
	arm.exit = ee.ExitCode()
	return arm
}

// budgetReportPipeQuote wraps a path in single quotes, escaping any embedded
// quote, so the fixture paths (which live under a scratch directory) reach bash
// verbatim.
func budgetReportPipeQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// budgetReportPipeChildEnv is the environment EVERY arm's shell is spawned
// with: this process's own environment with the four message-locale variables
// REPLACED by budgetReportPipeChildLocale.
//
// WHY (measured 2026-10-16). The arms read a producer's stderr as the control's
// observable, and `sort`/`grep` diagnose through gettext — so the SAME shape over
// the SAME fixture prints different bytes depending on the locale this test
// process happens to run under. The failure this function removes:
//
//	$ LANG=zh_CN.UTF-8 go test ./internal/testutil/ -run TestBudgetReportNeverPrintsPipeNoise
//	    stderr: "sort: 写入失败: 标准输出: 断开的管道\nsort: 写入错误\n"
//	--- FAIL: TestBudgetReportNeverPrintsPipeNoise (0.13s)
//
// The control HAD reproduced the defect — its own quoted stderr was the noise —
// while the assertion matched the English literal, so the gate failed with a
// message that contradicted its own evidence (Rule 15's shape). The same command
// under `LC_ALL=C` passed. The observable was therefore a property of the HOST,
// not of the shape under test.
//
// REPLACED, never appended: a duplicated entry in a child's environment is
// decided by the READER, not by one rule — glibc's gettext path and Go's
// os.Getenv take the FIRST copy while dash and bash take the LAST (measured 2026-10-16;
// see internal/tools/git_child_locale.go, ChildLocaleDuplicateKeyRule, for the
// reading and its host), so `append(os.Environ(), "LC_ALL=C")` is not a pin at
// all — the inherited Chinese value still wins for whatever reader looks first.
// This is the same construction, and the same trap, as internal/tools'
// gitChildLocaleEnv (Rule 14: one definition per shared value; this package
// cannot import that one — it is unexported there — so the reason is repeated
// here rather than the defect).
func budgetReportPipeChildEnv() []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, budgetReportPipeEnvAssignment)
		if budgetReportPipeLocaleEnvName(name) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "LANG="+budgetReportPipeChildLocale, "LC_ALL="+budgetReportPipeChildLocale)
}

// budgetReportPipeLocaleEnvName reports whether name selects a message
// catalogue.
func budgetReportPipeLocaleEnvName(name string) bool {
	for _, n := range budgetReportPipeLocaleEnvNames {
		if n == name {
			return true
		}
	}
	return false
}

// budgetReportPipeMatchProducerNoise reports whether stderr carries the
// signature of a producer that died on a closed pipe: every non-empty line
// begins with one of budgetReportPipeNoiseProducerNames.
//
// It is the SECOND, locale-independent half of the control's observable. The
// pinned locale already makes the English literals match on this host, but a
// pin protects only against the catalogues the arm can predict; the program-name
// prefix is not translated by anything, so this predicate still recognises the
// defect when the message bodies arrive in a catalogue the arm has never seen
// (the measured zh_CN case: prefix `sort:` intact, body `写入失败: …`).
//
// An empty stderr returns false — "no producer spoke" is not the noise.
func budgetReportPipeMatchProducerNoise(stderr string) bool {
	sawLine := false
	for _, line := range strings.Split(stderr, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sawLine = true
		matched := false
		for _, name := range budgetReportPipeNoiseProducerNames {
			if strings.HasPrefix(strings.TrimSpace(line), name) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return sawLine
}

// budgetReportPipeShellOptions returns the ONE shell-option line the script sets
// (its own `set -uo pipefail`), read from the script so the arm runs under the
// options that produced the defect rather than under a paraphrase of them.
func budgetReportPipeShellOptions(t *testing.T, script string) string {
	t.Helper()
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "set -") && strings.Contains(trimmed, "pipefail") {
			return trimmed
		}
	}
	t.Fatalf("%s sets no `pipefail` shell option — this gate's arms would run without the mechanism that "+
		"turns the producer's SIGPIPE into a report line (Rule 15)", budgetScriptPath)
	return ""
}

// budgetReportPipeFixtureBytes reports the fixture's size, so a control arm that
// stayed silent is diagnosed with the number that decides it.
func budgetReportPipeFixtureBytes(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture stream %s: %v", path, err)
	}
	return st.Size()
}

// budgetReportPipeLegacyBody is the helper's source as it shipped before the
// fix, verbatim from HEAD at the fix's base (9d8626d): the reverted shape, kept
// here as the gate's positive control so the red arm is the SHAPE and not a
// paraphrase of it.
const budgetReportPipeLegacyBody = `list_executed_tests() {
    grep -a -o -- "\"Action\":\"$executed_action\",\"Package\":\"[^\"]*\",\"Test\":\"[^\"]*\"" "$raw_json" 2>/dev/null |
        sed 's/.*"Package":"//; s/","Test":"/  /; s/"$//' |
        sort -u | head -"$executed_names_report_n"
}
`

// TestBudgetReportPipeChildEnvPinsTheCatalogueAndThePredicateReadsAny is the
// minimal repro of the 2026-10-16 failure, kept as a unit guard so it does not
// cost a whole-package run to notice the regression.
//
// The failure it pins: the arms read a producer's stderr, `sort`/`grep` diagnose
// through gettext, and the arms inherited the ambient catalogue — so the gate's
// verdict was decided by the host's locale instead of by the shape under test.
//
// Two mutations turn this red, and they are the two halves of the fix:
//  1. append the pin instead of REPLACING the variables (the duplicated-entry
//     trap: which copy a reader takes is READER-dependent — glibc and Go the
//     FIRST, the shells the LAST — so the inherited value would survive for
//     some reader; internal/tools/git_child_locale.go, ChildLocaleDuplicateKeyRule);
//  2. drop the selector names from the strip list, or make the predicate demand
//     the English body (which is exactly the pre-fix assertion).
func TestBudgetReportPipeChildEnvPinsTheCatalogueAndThePredicateReadsAny(t *testing.T) {
	// A hostile ambient catalogue, set through t.Setenv so it cannot leak into a
	// sibling test (Rule 17).
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANGUAGE", "zh_CN")
	t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")

	env := budgetReportPipeChildEnv()
	counts := map[string]int{}
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, budgetReportPipeEnvAssignment)
		if !ok {
			continue
		}
		counts[name]++
		if budgetReportPipeLocaleEnvName(name) && !strings.HasSuffix(kv, budgetReportPipeChildLocale) {
			t.Errorf("the arm's environment carries %q — a catalogue variable that survived the pin, so the "+
				"producer would speak the host's language again", kv)
		}
	}
	// EXACTLY one of each: two would be the append trap, where which copy the
	// child's libc reads depends on the reader, not on a fixed order.
	for _, name := range budgetReportPipeLocaleEnvNames {
		want := 1
		if name == "LANGUAGE" || name == "LC_MESSAGES" {
			want = 0 // stripped outright, never re-set
		}
		if got := counts[name]; got != want {
			t.Errorf("the arm's environment carries %d %s entries, want %d — the pin must REPLACE "+
				"LANG/LC_ALL and STRIP the selectors, never append (an appended duplicate is not the entry "+
				"the child reads).\n%v", got, name, want, env)
		}
	}

	// The locale-INDEPENDENT predicate, on the exact bytes this host produced
	// under zh_CN.UTF-8 (captured 2026-10-16), then on the C shapes, then on the
	// two negatives.
	localized := "sort: 写入失败: 标准输出: 断开的管道\nsort: 写入错误\n"
	if !budgetReportPipeMatchProducerNoise(localized) {
		t.Errorf("the predicate did not recognise the measured localized noise %q — the observable would "+
			"depend on the catalogue again, which is the defect this gate was fixed for", localized)
	}
	for _, english := range []string{
		"sort: write failed: 'standard output': Broken pipe\nsort: write error\n",
		"grep: write error: Broken pipe\n",
	} {
		if !budgetReportPipeMatchProducerNoise(english) {
			t.Errorf("the predicate did not recognise the C-locale noise %q", english)
		}
	}
	// Negatives: silence is not the noise, and an unrelated diagnostic is not
	// either — otherwise the control would pass on any stderr at all.
	for _, notNoise := range []string{"", "\n", "some other diagnostic\n", "sort: wrote everything fine\nfine\n"} {
		if budgetReportPipeMatchProducerNoise(notNoise) {
			t.Errorf("the predicate called %q the producer noise — the control must not accept arbitrary "+
				"stderr", notNoise)
		}
	}
}
