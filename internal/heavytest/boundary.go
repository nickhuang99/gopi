package heavytest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/boolvocab"
)

// boundary.go — WHO MAY ENABLE THE HEAVY SET.
//
// THE DEFECT THIS CLOSES (2026-09-17). EnvName is an ordinary INHERITABLE
// environment variable, and the heavy set is the most expensive thing this
// repository can be asked to do: minutes of real per-test disk work, wide
// parallelism, host-condition-dependent verdicts. Inheritance is how a value
// travels from a shell to everything below it — so a resident worker, the
// merger's child, or any `gopi task run` executor started from a shell that
// happened to carry `GOPI_HEAVY_TESTS=1` (an agent shell, a briefing that said
// "run the full suite", a wrapper script) silently burned an hour of host CPU
// on tests the operator deliberately keeps OFF by default. The classification
// (which tests are heavy, and that the default is smoke-only) is CORRECT and
// is untouched: what was missing is a question the var alone cannot answer —
// is THIS process an operator invocation, or work?
//
// THE ANSWER IS THE SAME ONE THE AGENT GUARD ALREADY ASKS
// (internal/config/agentguard.go, 2026-09-12 ruling). A gopi AGENT process
// exports its own session handle (GOPI_EXECUTOR_SESSION_ID) and its chain
// anchor (GOPI_AGENT_SESSION), and every process below it inherits them; the
// operator's own terminal has neither. A task child additionally carries the
// wrapper identity this harness sets AT SPAWN (GOPI_TASK_WRAPPER_ID,
// GOPI_TASK_RUN_ID). So an agent/work context is directly observable, and a
// human who means it declares the terminal he is sitting at
// (GOPI_HUMAN_TERMINAL — the same negative signature agentguard uses, set
// per-invocation on the operator's own command line and by the documented
// `make test-heavy` route).
//
// WHY THE MIRRORED NAMES (Rule 14's exception, with the agentguard precedent):
// this package is a LEAF — the in-package tests of internal/scratch, internal/
// tools and cmd/agent must be able to import it, so it may import nothing that
// could close a cycle (config → scratch, tools → config). The names below are
// therefore a documented MIRROR of their definition sites, and a drift test
// (internal/config/boundary_mirror_test.go, which may import both sides)
// fails the build if either side moves — a mirror with a check, never a
// mirror with a hope.
const (
	// AgentSessionEnv is mirror of config.AgentSessionEnv: the session handle a
	// gopi AGENT process exports for itself and everything it spawns.
	AgentSessionEnv = "GOPI_EXECUTOR_SESSION_ID"
	// HermesSessionEnv is mirror of config.HermesSessionIDEnv: the foreign
	// harness signature, for the case where no gopi ancestor exported one.
	HermesSessionEnv = "HERMES_SESSION_ID"
	// AgentAncestorEnv is mirror of config.AgentSessionAncestorEnv: the
	// agent-chain anchor marker.
	AgentAncestorEnv = "GOPI_AGENT_SESSION"
	// TaskWrapperEnv is mirror of tools.TaskWrapperEnv: set AT SPAWN for a
	// `gopi task run` executor child (its wrapper row id).
	TaskWrapperEnv = "GOPI_TASK_WRAPPER_ID"
	// TaskRunIDEnv is mirror of tools.TaskRunIDEnv: the run/close-out row the
	// wrapper stamps before the spawn.
	TaskRunIDEnv = "GOPI_TASK_RUN_ID"
	// HumanTerminalEnv is mirror of config.HumanTerminalEnv: the operator's
	// negative signature. It must NOT be exported from a shell profile (every
	// agent below it would inherit the declaration); correct use is
	// per-invocation on the operator's own command line, or the Makefile's
	// test-heavy target, which sets it for exactly that reason.
	HumanTerminalEnv = "GOPI_HUMAN_TERMINAL"
)

// WorkEnvVars are the environment names whose PRESENCE in this process's
// environment says "an agent is above me, or I am a task child" — i.e. this is
// WORK, never an operator invocation. Values are not inspected (an empty value
// is still a presence), exactly as agentguard reads them.
var WorkEnvVars = []string{
	AgentSessionEnv,
	HermesSessionEnv,
	AgentAncestorEnv,
	TaskWrapperEnv,
	TaskRunIDEnv,
}

// The refusal's wording lives here, so the run's stderr block, the skip reason
// and the ledger line an operator greps cannot drift apart (Rule 14).
const (
	// RefusalPrefix heads every refusal line (the greppable marker).
	RefusalPrefix = "HEAVY TESTS REFUSED"
	// refusalTag marks the ledger rows a refused heavy set writes, so a run's
	// ledger distinguishes "skipped by classification" from "REFUSED because
	// this was work" — the difference between the default and a defect.
	refusalTag = "REFUSED"
	// The refusal is LOUD by construction: the block is written to stderr
	// unconditionally, where a t.Logf would be shown only under -v or on
	// failure — and a refused heavy test SKIPS (the package PASSES), so a bare
	// `go test ./...` prints neither. stderr is what a `-v` run and the `-json`
	// stream carry; the ledger row (refusalTag) is what makes the refusal
	// COUNTABLE in every run, and the Makefile's smoke target always sets it.
	refusalHeader = RefusalPrefix +
		": this process is a WORK context (an agent session is above it, or it IS a task child),\n" +
		"  so the heavy set stays OFF even though " + EnvName + " is set.\n"
	refusalWhy = "  Heavy tests are the OPERATOR's deliberate, off-peak choice; a value that was " +
		"INHERITED\n  (a dev shell, a briefing that says \"run the full suite\", a wrapper script) must " +
		"never buy\n  an hour of host CPU that the default forbids.\n"
	refusalEffect = "  WHAT HAPPENED: the heavy set did NOT run — this is a REFUSAL, not a silent " +
		"no-op.\n  The run stays SMOKE-ONLY, exactly as if " + EnvName + " had been unset: the " +
		"classification\n  is untouched, every heavy test still SKIPS itself, and the skip ledger " +
		"records a\n  " + refusalTag + " row so the refusal is countable (`make test` leaves its ledger at " +
		".heavy-skipped.tsv).\n"
	refusalRoute = "  OPERATOR ROUTE (works first try): from your own terminal —\n" +
		"      make test-heavy                     the documented opt-in (whole tree, off-peak)\n" +
		"      GOPI_HUMAN_TERMINAL=1 go test -run <Test> -v ./<pkg>/     one heavy test\n" +
		"  Both DECLARE the terminal you are sitting at. Declare it per invocation, never in a\n" +
		"  shell profile: a profile would speak for every agent started below it.\n"
	// declarationLine is the announcement printed when the operator DID declare
	// his terminal: an override must never be silent (Rule 15).
	declarationLine = RefusalPrefix + ": " + HumanTerminalEnv +
		" is declared, so the inherited work signature is stepped over and the heavy set RUNS."
)

// WorkSignature returns the work-signature names PRESENT according to lookup —
// the boundary's ONE detector, taking the environment as a function so a caller
// holding an env LIST (a spawn seam about to hand an environment to a child)
// can ask the same question the live process asks (Rule 2: one predicate;
// agentguard's pure-decision precedent). Presence, not truthiness: an empty
// value still says an agent is above (agentguard reads them the same way).
func WorkSignature(lookup func(string) string) []string {
	var out []string
	for _, name := range WorkEnvVars {
		if strings.TrimSpace(lookup(name)) != "" {
			out = append(out, name)
		}
	}
	return out
}

// WorkRefusal returns the reason this process may not enable the heavy set, or
// "" when it may. It is the boundary's ONE live-process predicate (Rule 2):
// Require calls it, so the tested predicate IS the production predicate.
//
// Precedence: an explicit operator declaration wins over an inherited signature
// (the declaration is the operator's own act and cannot be inherited — a shell
// profile that exported it would speak for every agent below it, which is why
// agentguard documents it as per-invocation); with no declaration, ANY work
// signature refuses.
func WorkRefusal() (reason string, signature []string, declared bool) {
	return WorkRefusalWith(os.Getenv)
}

// WorkRefusalWith is WorkRefusal against a supplied environment lookup, so a
// seam that holds an env LIST (rather than this process's environment) can be
// judged by the SAME predicate — the test that ties the spawn seam to the
// boundary would otherwise re-implement the rule and drift from it.
func WorkRefusalWith(lookup func(string) string) (reason string, signature []string, declared bool) {
	signature = WorkSignature(lookup)
	if len(signature) == 0 {
		return "", nil, false
	}
	declared = truthy(lookup(HumanTerminalEnv))
	if declared {
		return "", signature, true
	}
	return "inherited work signature: " + strings.Join(signature, ", "), signature, false
}

// DeclaredOperatorTerminal reports whether raw is one the boundary accepts as
// the operator's declaration. Exported so internal/config — the side that owns
// the original truthy set — can PIN the two parsers together in a test: this
// package cannot import config (see the header), so the honest alternative to a
// silent re-implementation is a checked mirror.
func DeclaredOperatorTerminal(raw string) bool { return truthy(raw) }

// truthy reports whether a raw value declares operator intent, drawn from the
// ONE vocabulary definition (internal/boolvocab — a leaf, so this package can
// import it; PROJECT.md Rule 14 no longer permits the mirror the earlier revision
// of this file carried). Only truthy values enable, so a canonical
// "off"/"false" written by a wrapper stays off.
func truthy(raw string) bool { return boolvocab.IsTrue(raw) }

// refuseHeavyRun announces the refusal LOUDLY (stderr, unconditionally), writes
// the countable ledger row and skips t. Never a silent ignore: an operator
// reading the run's output learns what happened and what runs it instead.
func refuseHeavyRun(t *testing.T, criterion, reason string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(refusalHeader)
	b.WriteString("  WHY THIS PROCESS: " + reason + "\n")
	b.WriteString(refusalWhy)
	b.WriteString(refusalEffect)
	b.WriteString(refusalRoute)
	block := b.String()

	ledgerMu.Lock()
	// stderr, not t.Logf: a refused heavy test SKIPS, i.e. the package PASSES,
	// and go test keeps a passing package's log lines to itself. Measured
	// 2026-09-18 (no -v, passing package): go test prints NOTHING — not even
	// what this write puts on stderr — so the unconditional block is what `-v`
	// runs and the `-json` stream carry. What makes a refusal visible in a
	// bare run is the run's LEDGER row below (the Makefile's smoke target
	// always sets SkipLedgerEnv), so a refused heavy set is countable in every
	// run and readable in the ones that look.
	if _, err := fmt.Fprint(os.Stderr, block); err != nil {
		t.Logf("heavy refusal: cannot write to stderr: %v", err)
	}
	ledgerMu.Unlock()

	recordRefusal(t, criterion, reason)
	t.Skip(RefusalPrefix + ": " + reason + " (heavy set NOT started; operator route: make test-heavy)")
}

// recordRefusal appends the refusal to the run's skip ledger, so the run stays
// enumerable. Same best-effort contract as recordSkip: an unwritable path never
// turns a smoke run red, but the failure is REPORTED (Rule 15b).
func recordRefusal(t *testing.T, criterion, reason string) {
	t.Helper()
	t.Logf("%s: %s — %s", RefusalPrefix, criterion, reason)
	path := strings.TrimSpace(os.Getenv(SkipLedgerEnv))
	if path == "" {
		return
	}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("heavy refusal ledger: cannot create %s: %v", filepath.Dir(path), err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Logf("heavy refusal ledger: cannot open %s: %v", path, err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Logf("heavy refusal ledger: close %s: %v", path, cerr)
		}
	}()
	if _, err := f.WriteString(t.Name() + "\t" + refusalTag + ": " + reason + "\n"); err != nil {
		t.Logf("heavy refusal ledger: write %s: %v", path, err)
	}
}
