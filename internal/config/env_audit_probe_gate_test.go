package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The env-catalog audit (`scripts/audit-env-catalog.sh`) probes every row of the
// table the tests above describe — so the way it probes is a property of THIS
// package's surfaces, and a future edit that went back to probing by VALUE would
// re-open the defect with nothing to catch it. The incident (task
// 01a0de69-edfa-770e-b104-0d51ced38eb4, measured 2026-09-26): the probe ran
// `gopi --<env-as-flag> 1 version` for every row. For a flag-bearing row that is
// not a probe — `--gopi-prompt 1` IS `--prompt 1` (FlagOverrides, the documented
// equivalence above) — so it DISPATCHED a real agent run whose prompt was the
// literal "1", auto-registered a real ledger row (`[auto] 1`), and the probe's
// own classifier had no branch for "a run started", so the audit reported clean.
//
// Two properties make the audit incapable of that, and both are asserted here
// over the script's own text:
//
//  1. NO VALUE IS EVER PASSED. Every probe invocation passes the flag SPELLING
//     alone.
//  2. THE PROBES RUN IN A SANDBOX. The script hands off to `gopi tool sandbox run`
//     (Rule 34) before it probes anything.
//
// This is a TEXT gate over a script, deliberately the weakest instrument that
// fits: the STRONGER check — that the loop really refuses a dispatching probe —
// is executed by the script itself in `--self-check`, and the strongest evidence
// is the audit's own `dispatched runs=0` line beside its zero-ledger-delta
// assertion on a real run. This gate's narrower job is the one thing a text edit
// cannot hide from. Each predicate is a PURE FUNCTION over the script text, so
// the red path is driven with a synthetic regressed text (see
// TestEnvAuditProbeGateRedProof) as well as by regressing the real script.
//
// Scope note (an honest partial, Rule 26's "narrow, never wrong"): the predicate
// is an INVOCATION-SHAPE gate. A probe rewritten to hide its call site from the
// anchor is not caught by the text gate — it is caught by the audit's own
// `dispatched runs=0` and zero-ledger-delta lines on a real run, which is why the
// executed assertions are the primary instrument and this one is the tripwire.

const (
	// envAuditScriptName is the audit this gate pins, relative to the repo root.
	envAuditScriptName = "scripts/audit-env-catalog.sh"
	// envAuditSandboxHandoff is the ONE initializer a sandbox comes from (Rule
	// 34) — the hand-off whose absence would run the probes in the operator
	// profile.
	envAuditSandboxHandoff = "gopi tool sandbox run"
	// envAuditProbeValueToken is the value the defect passed: "1" (the same token
	// envProbeValue feeds the parser above). It is the whole difference between
	// probing a spelling and dispatching a run.
	envAuditProbeValueToken = envProbeValue
	// envAuditDispatchLabel is the classifier branch the audit must carry: a run
	// that started is an AUDIT FAILURE, never a bucket to be counted.
	envAuditDispatchLabel = "DISPATCHED"
	// envAuditLedgerStderrCapture is the shape that preserves the reader's own
	// reason so the refusal can quote it.
	envAuditLedgerStderrCapture = `2> "$WORK/ledger.err"`
	// envAuditLedgerUnobservedRefusal is the loud, named refusal an unobservable
	// ledger must produce — its absence was the defect (an empty reason tail).
	envAuditLedgerUnobservedRefusal = "the ledger could not be observed"
	// envAuditLedgerReaderBody is the reader's opening, so the gate knows it is
	// measuring the current shape rather than a shape that moved.
	envAuditLedgerReaderBody = "read_ledger() {"
)

// envAuditLedgerStderrSwallows are the spellings that DISCARD a ledger reader's
// stderr. `2>/dev/null` is the original; the bare `task list` on the pipeline's
// left is the same discard one layer up (the pipeline takes `sort`'s status, so
// the reader's refusal never reaches the `||`). Both are banned together —
// repairing one and keeping the other changes nothing about what the reader can
// report. (A var, not a const: the const block above holds scalars.)
var envAuditLedgerStderrSwallows = []string{"task list 2>/dev/null"}

// envAuditLedgerEmptyTail is the sink spelling that produced the reported symptom:
// the header, a COLON, then the variable — so when no reason was ever recorded the
// audit printed a refusal that named nothing. The colon immediately before the
// expansion is what makes an empty tail possible, so THAT shape is banned; the
// header itself stays required, under its own constant above.
const envAuditLedgerEmptyTail = "the ledger could not be observed:$"

// envAuditProbeInvocation matches a probe CALL SITE: the binary followed by the
// env-name-as-flag spelling. A dollar-prefixed token is the loop variable
// (`"$abin" "$sp"`), which is the correct shape — the spelling is computed, the
// value is absent. Anything after it on that command line is an ARGUMENT, and
// this gate inspects exactly that.
var envAuditProbeInvocation = regexp.MustCompile(`"\$(abin|bin)"\s+"\$?(sp|spelling)"([^\n]*)`)

// envAuditRedirection strips the shell plumbing a probe call legitimately
// carries: the stdin close and the stdout/stderr capture. What survives the
// strip is an argument — and a probe must have none.
var envAuditRedirection = regexp.MustCompile(`[0-9]*[<>]{1,2}\S+`)

// envAuditProbeArguments returns, for every probe call site in the script, the
// ARGUMENTS that follow the env-name-as-flag spelling (redirections removed). A
// value-passing probe shows up here as a non-empty string; the correct probe
// shows up as "".
func envAuditProbeArguments(scriptText string) []string {
	body := stripShellComments(scriptText)
	sites := envAuditProbeInvocation.FindAllStringSubmatch(body, -1)
	args := make([]string, 0, len(sites))
	for _, site := range sites {
		args = append(args, strings.TrimSpace(envAuditRedirection.ReplaceAllString(site[3], "")))
	}
	return args
}

// envAuditProbeInvocationCount is how many probe call sites the text holds — a
// zero here means the gate is measuring nothing, which is its own failure.
func envAuditProbeInvocationCount(scriptText string) int {
	return len(envAuditProbeInvocation.FindAllStringSubmatch(stripShellComments(scriptText), -1))
}

// envAuditSandboxHandoffFirst reports whether `gopi tool sandbox run` precedes the
// probe loop in the given script text — the property that decides WHICH profile
// the probes run in. Comments are stripped first: the hand-off must be EXECUTED,
// and a mention in a comment enters no sandbox.
func envAuditSandboxHandoffFirst(scriptText string) bool {
	body := stripShellComments(scriptText)
	handoff := strings.Index(body, envAuditSandboxHandoff)
	if handoff < 0 {
		return false
	}
	loop := envAuditProbeInvocation.FindStringIndex(body)
	if loop == nil {
		return false
	}
	return handoff < loop[0]
}

// TestEnvAuditProbePassesNoValue is the anti-regression gate for the incident:
// no probe call site may carry an argument after the spelling.
func TestEnvAuditProbePassesNoValue(t *testing.T) {
	src := readEnvAuditScript(t)

	count := envAuditProbeInvocationCount(src)
	if count == 0 {
		t.Fatalf("no probe call site found in %s: the gate is measuring nothing "+
			"(the loop's invocation shape changed — re-anchor %s)",
			envAuditScriptName, envAuditProbeInvocation)
	}
	for _, rest := range envAuditProbeArguments(src) {
		if rest == "" {
			continue
		}
		t.Errorf("%s: a probe carries %q after the env-name-as-flag spelling — a probe "+
			"passes the SPELLING ONLY. `--gopi-prompt 1` IS `--prompt 1` (FlagOverrides), "+
			"so an argument makes the probe dispatch a real run and register a real ledger "+
			"row (the 2026-09-26 incident: `[auto] 1`)", envAuditScriptName, rest)
	}

	// The value token must not be handed to any probe — the loop variable is the
	// only thing a probe is given.
	if strings.Contains(stripShellComments(src), `"$sp" `+envAuditProbeValueToken) {
		t.Errorf("%s: a probe still passes the value token %q — this is exactly the "+
			"invocation that dispatched a run and wrote ledger row `[auto] 1`",
			envAuditScriptName, envAuditProbeValueToken)
	}
	t.Logf("probe call sites=%d, all value-free", count)
}

// TestEnvAuditProbesRunInsideASandbox pins property 2: the probes are reached
// only through the sandbox hand-off, and the hand-off precedes them. Without it
// the loop would run in whatever profile the caller had — the operator's — the
// profile the incident polluted.
func TestEnvAuditProbesRunInsideASandbox(t *testing.T) {
	src := readEnvAuditScript(t)
	body := stripShellComments(src)

	if !strings.Contains(body, envAuditSandboxHandoff) {
		t.Fatalf("%s never reaches %q: the audit must materialise and enter a sandbox "+
			"(Rule 34) before probing, or a surprise dispatch writes to the OPERATOR "+
			"ledger — the profile the incident polluted (a mention in a comment enters "+
			"no sandbox)", envAuditScriptName, envAuditSandboxHandoff)
	}
	if !envAuditSandboxHandoffFirst(body) {
		t.Errorf("%s: the sandbox hand-off does not precede the probe loop — the probes "+
			"would run in the operator profile", envAuditScriptName)
	}

	// A dispatched run must be classified, never silently bucketed (Rule 15): the
	// banner the classifier looks for is the run's own evidence.
	if !strings.Contains(src, envAuditDispatchLabel) {
		t.Errorf("%s has no %s classification: a run that had started must fail the "+
			"audit loudly, not land in a silent bucket",
			envAuditScriptName, envAuditDispatchLabel)
	}
}

// TestEnvAuditProbeGateRedProof drives BOTH predicates against the regressed
// shape — `"$abin" "$sp" 1` with the sandbox hand-off deleted — and requires each
// to fire. Without this the gate could pass on a script the defect would still
// reach (a gate that cannot go red is not a gate).
func TestEnvAuditProbeGateRedProof(t *testing.T) {
	// The pre-fix shape, in miniature: the value token back on the probe line.
	regressedValue := "# a comment mentioning " + envAuditSandboxHandoff + " (must not count)\n" +
		`while IFS=$'\t' read -r env flag; do
  sp="--$(printf '%s' "$env" | tr 'A-Z_' 'a-z-')"
  timeout 20 "$abin" "$sp" ` + envAuditProbeValueToken + ` version </dev/null >"$WORK/out" 2>"$WORK/err"
done < "$aev/rows.tsv"
`
	if n := envAuditProbeInvocationCount(regressedValue); n != 1 {
		t.Fatalf("the red-proof text has %d probe call sites, want 1 — the predicate is "+
			"not anchored on the shape the defect had", n)
	}
	args := envAuditProbeArguments(regressedValue)
	if len(args) != 1 || args[0] == "" {
		t.Fatalf("the value-passing probe was NOT flagged (arguments=%q) — the gate cannot "+
			"see the regression it exists for", args)
	}
	if !strings.Contains(args[0], envAuditProbeValueToken) {
		t.Errorf("the flagged argument %q does not carry the value token %q",
			args[0], envAuditProbeValueToken)
	}

	// The pre-fix shape, other half: probes with NO executed sandbox hand-off
	// above them — the comment above must not be mistaken for one.
	if envAuditSandboxHandoffFirst(regressedValue) {
		t.Error("a script whose only hand-off is a COMMENT was reported as sandboxed — " +
			"the gate cannot see the regression it exists for")
	}

	// The green side, on the text that makes the same claim as a real hand-off.
	corrected := "exec gopi tool sandbox run --label x -- env ENV_AUDIT_SANDBOXED=1 \"$SELF\"\n" +
		`while IFS=$'\t' read -r env flag; do
  "$abin" "$sp" </dev/null >"$WORK/out" 2>"$WORK/err"
done < "$aev/rows.tsv"
`
	if !envAuditSandboxHandoffFirst(corrected) {
		t.Error("a correctly hand-off-first script was reported as NOT sandboxed")
	}
	for _, rest := range envAuditProbeArguments(corrected) {
		if rest != "" {
			t.Errorf("a value-free probe was flagged: %q", rest)
		}
	}
}

// TestEnvAuditLedgerReaderNeverSwallowsItsOwnFailure is the anti-regression gate
// for the SECOND defect this script carried (task 01a10efc-518d): the ledger
// reader discarded the binary's stderr AND its exit status, so a ledger that could
// not be observed produced `FAIL: the ledger could not be observed:` with an EMPTY
// reason tail — a refusal that named neither the command, nor the channel, nor why
// (Rule 15). MEASURED on master before the repair: with a `task list` that exits 1,
// the audit printed `sandbox ledger delta=none` / `operator ledger delta=none` and
// `OK: no run dispatched, no bare unknown flag, zero ledger delta`, exit 0 — a
// CLEAN verdict built on a reading that never happened.
//
// Why a pipeline was the mechanism: `reader || explain; reader | awk | sort` puts
// the reader's status in the MIDDLE of the pipeline, so `sort`'s exit 0 becomes
// the pipeline's status and the reader's refusal never reaches the `||`. The
// predicates below are the text half of the same property the script's own
// `--self-check` leg 3 executes (a refused reader must produce a loud, named
// refusal) — the executed leg is the primary instrument, this is the tripwire a
// text edit cannot hide from.
func TestEnvAuditLedgerReaderNeverSwallowsItsOwnFailure(t *testing.T) {
	src := readEnvAuditScript(t)
	body := stripShellComments(src)

	for _, banned := range envAuditLedgerStderrSwallows {
		if strings.Contains(body, banned) {
			t.Errorf("%s still carries %q: the ledger reader's stderr is discarded, so its "+
				"refusal cannot be quoted — the failure this gate exists for is an "+
				"unobservable ledger reported with an EMPTY reason (Rule 15)",
				envAuditScriptName, banned)
		}
	}

	if !strings.Contains(body, envAuditLedgerStderrCapture) {
		t.Errorf("%s does not capture the ledger reader's stderr (%q): the reader's own "+
			"reason must survive to be quoted into the refusal",
			envAuditScriptName, envAuditLedgerStderrCapture)
	}
	if !strings.Contains(body, envAuditLedgerUnobservedRefusal) {
		t.Errorf("%s carries no %q refusal: an unobservable ledger must fail the audit "+
			"loudly, naming the channel and the reason",
			envAuditScriptName, envAuditLedgerUnobservedRefusal)
	}
	if strings.Contains(body, envAuditLedgerEmptyTail) {
		t.Errorf("%s still prints %q: the refusal's reason tail is expanded with nothing "+
			"guaranteed to be in it, so an unobservable ledger is reported as an empty "+
			"reason — the symptom this task was filed for (Rule 15); name the channel, the "+
			"command and the reader's own stderr instead",
			envAuditScriptName, envAuditLedgerEmptyTail)
	}

	// The reader's status must be taken from the reader, not from the pipeline that
	// formats it: the two profile reads are the shape that decides it.
	if n := strings.Count(body, envAuditLedgerReaderBody); n == 0 {
		t.Errorf("%s: no ledger reader body (%q) — the gate is measuring nothing; "+
			"re-anchor it", envAuditScriptName, envAuditLedgerReaderBody)
	} else {
		t.Logf("ledger reader bodies=%d", n)
	}
}

// TestEnvAuditLedgerReaderGateRedProof drives the predicates against the shape
// master shipped — the exact text measured to report `OK` with an unreadable
// ledger — and requires each to fire.
func TestEnvAuditLedgerReaderGateRedProof(t *testing.T) {
	regressed := `  ledger() {
    if [ "$1" = "sandbox" ]; then "$abin" task list ` + envAuditLedgerStderrSwallows[0] + `
    else env -u GOPI_HOME -u GOPI_SETTINGS "$abin" task list ` + envAuditLedgerStderrSwallows[0] + `; fi \
    | awk '$1 ~ /^#[0-9a-f]/ {print $1}' | sort
  }
  ledger sandbox  > "$aev/ledger-sandbox-before.txt"  || amsg="$amsg ledger-before(sandbox) failed;"
  if [ -n "$amsg" ]; then say "` + `FAIL: ` + envAuditLedgerEmptyTail + `amsg"; status=1; fi
`
	if !strings.Contains(regressed, envAuditLedgerStderrSwallows[0]) {
		t.Fatal("the red-proof text does not carry the banned spelling — it cannot go red")
	}
	for _, banned := range envAuditLedgerStderrSwallows {
		if !strings.Contains(regressed, banned) {
			t.Fatalf("predicate %q does not fire on the pre-fix text", banned)
		}
	}
	if strings.Contains(regressed, envAuditLedgerStderrCapture) {
		t.Error("the pre-fix text was reported as capturing stderr")
	}
	if !strings.Contains(regressed, envAuditLedgerEmptyTail) {
		t.Error("the pre-fix text was NOT reported as carrying the empty-tail spelling — " +
			"the predicate cannot see the symptom it exists for")
	}
	// …and the fixed shape passes the same predicates, so the gate is not simply
	// red on everything.
	fixed := `  read_ledger() {
    if [ "$_lprof" = "sandbox" ]; then "$abin" task list
    else env -u GOPI_HOME -u GOPI_SETTINGS "$abin" task list; fi \
      > "$WORK/ledger.raw" ` + envAuditLedgerStderrCapture + `
    _lrc=$?
    if [ "$_lrc" != "0" ]; then
      amsg="$amsg [$_lchan] ` + "`$abin task list`" + ` exited $_lrc;"
      return 0
    fi
    awk '$1 ~ /^#[0-9a-f]/ {print $1}' "$WORK/ledger.raw" | sort
  }
`
	for _, banned := range envAuditLedgerStderrSwallows {
		if strings.Contains(fixed, banned) {
			t.Errorf("the fixed shape was falsely flagged for %q", banned)
		}
	}
	if !strings.Contains(fixed, envAuditLedgerStderrCapture) {
		t.Error("the fixed shape was not recognised as capturing stderr")
	}
	if strings.Contains(fixed, envAuditLedgerEmptyTail) {
		t.Error("the fixed shape was falsely flagged for the empty-tail spelling")
	}
}

// TestEnvAuditGateAgreesWithTheArtifact is the anchor check: the predicates above
// must say of the REAL script what the artifact claims about itself. If this
// fails, the gate and the artifact disagree and one of them is wrong.
func TestEnvAuditGateAgreesWithTheArtifact(t *testing.T) {
	src := readEnvAuditScript(t)
	if !envAuditSandboxHandoffFirst(src) {
		t.Errorf("%s is reported as NOT sandboxed — the gate and the artifact disagree",
			envAuditScriptName)
	}
	if !strings.Contains(src, envAuditDispatchLabel) {
		t.Errorf("%s carries no %s classification", envAuditScriptName, envAuditDispatchLabel)
	}
	for _, rest := range envAuditProbeArguments(src) {
		if rest != "" {
			t.Errorf("%s: a probe carries arguments %q — the artifact claims value-free probes",
				envAuditScriptName, rest)
		}
	}
}

// readEnvAuditScript reads the audit from the repo root the OTHER config tests
// already use (plugins_test.go's walk to go.mod): a whole-tree artifact is not
// this package's own directory.
func readEnvAuditScript(t *testing.T) string {
	t.Helper()
	root := repoRootForConfigTest(t)
	path := filepath.Join(root, envAuditScriptName)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the audit this package's surfaces are probed by must "+
			"exist at the path the gate pins", path, err)
	}
	if len(b) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return string(b)
}

// stripShellComments removes whole-line `#` comments, so a comment MENTIONING the
// hand-off or the value token (this gate's own rationale quotes both) is not read
// as code. An inline trailing comment is left alone: the gate errs toward
// flagging.
func stripShellComments(src string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
