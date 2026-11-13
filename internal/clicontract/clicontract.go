// Package clicontract is the MERGE-TIME half of the CLI contract drift
// guard. The test-time half (cmd/agent/cli_contracts_test.go
// TestCLIContractInventoryMatchesDispatch) already proves that every
// dispatch predicate in main.go has a backing usage-inventory case — but it
// only fires when someone runs `go test`, which the merger never does.
//
// 2026-09-12 voice_input incident: bd3d34a5 landed on master with
// `|| isVoiceSubcommand(dispatchArgs)` added to the main.go dispatch chain
// while the contract suite's mirror was NOT updated. TestCLIContract-
// InventoryMatchesDispatch went red on master (26 wired vs 25 tracked) and
// stayed red until someone ran the suite. This package asks the same drift
// question ON the merge path: parse the branch tip's main.go dispatch chain
// and its cli_contracts_test.go mirror, and report the predicates that
// disagree — token-free, deterministic, and computed from `git show` text
// alone (no checkout, no worktree mutation, no build, no test run, no LLM).
//
// WHAT IT IS NOT: a build or a test run. It catches exactly ONE failure
// mode — main.go's dispatch chain and the contract suite's predicate mirror
// drifting apart — which is the failure mode that produced the incident.
// Compile errors, unrelated red tests, and inventory-fingerprint drift need
// the full build/test gate this package deliberately does not implement
// (see devdoc/merge-time-contract-gate-20260912.md for the measured cost
// argument).
//
// The two file paths it reads are a CONTRACT with the CLI test suite: if
// either moves, the caller's reader returns an error and the caller
// ABSTAINS (never refuses) — visible in the sweep output rather than silent
// or permanently blocking.
package clicontract

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The two files examined at a rev. Branch-relative POSIX paths, read with
// `git show <rev>:<path>`.
const (
	// DispatchChainFile owns the `if isCompletionSubcommand(dispatchArgs)
	// || …` chain: the ACTUAL set of wired predicates.
	DispatchChainFile = "cmd/agent/main.go"
	// ContractSuiteFile owns cliContractDispatchPredicates: the mirror the
	// test-time guard holds in lockstep with the chain.
	ContractSuiteFile = "cmd/agent/cli_contracts_test.go"
)

// Dispatch-chain parsing anchors. main.go's chain is a single `if` whose
// conditions are `||`-joined `isXxxSubcommand(<args>)` calls; it starts at
// the first predicate (everything before it is unrelated dispatch logic) and
// ends at the `{` opening the block.
//
// The predicate's ARGUMENT is captured, not hardcoded: pinning the literal
// `(dispatchArgs)` made the gate silently abstain (merge unchanged, forever)
// the day main.go renamed that local variable — a gate nobody notices is not
// a gate. The first call's argument name is treated as canonical and every
// other token must agree, so a chain of calls on DIFFERENT variables is a
// shape change (abstention), not a finding.
const (
	dispatchChainStart = "if isCompletionSubcommand("
	dispatchChainOpen  = "{"
	dispatchJoin       = "||"
	// chainParseMinPredicates guards against parsing a refactored or
	// truncated chain as a valid one: the real chain carries ~26 predicates
	// and only grows, so anything under this floor is a shape change (an
	// abstention), not a finding.
	chainParseMinPredicates = 10
)

// Mirror-list parsing anchors: cliContractDispatchPredicates is a
// []string{…} of bare predicate names (no `is` prefix, no call
// parentheses), each a double-quoted string literal.
const (
	mirrorListStart  = "cliContractDispatchPredicates = []string"
	mirrorListEnd    = "}"
	mirrorNamePrefix = "is"

	// List-literal anatomy shared by every anchor above: a `var x = []T{`
	// anchor is CONSUMED by its own text, and the `{` that actually opens the
	// body is the first character of what follows. Without this the body of
	// `…Predicates = []string{` is `… = []string{"A"}` — the END anchor is
	// skipped and the parse is a silent substring accident.
	listOpenBrace = "{"
)

// Token-map anchors: dispatcherOfToken maps each inventory first-token to the
// predicate that dispatches it. The project's guard uses that map to prove
// every mirrored predicate has a contract CASE, not merely a mirror entry —
// so a branch that updates cliContractDispatchPredicates but forgets this map
// still leaves TestCLIContractInventoryMatchesDispatch red on master. The
// VALUES are parsed (the predicate names); the keys are deliberately NOT
// resolved, because they are constants (CliVerbAct) and tool-wire names
// (toolnames.ToolVoiceInput) whose resolution would need the compiler. A
// non-empty value set is exactly the evidence needed: some token routes to
// that predicate.
// DispatcherTokenMapAnchor is the declaration text a revision must carry for
// the gate to be able to check the token-map arm at all. Exported because the
// git-side caller ABSTAINS (rather than guessing) when a revision's contract
// suite does not declare it — the 2026-09-12 live demonstration showed a
// silent fail-closed here would freeze a repo either way, so the caller needs
// the anchor's text, not a byte offset.
const DispatcherTokenMapAnchor = "dispatcherOfToken"

const (
	// dispatcherTokenMapStart anchors the map literal.
	dispatcherTokenMapStart = "dispatcherOfToken = map[string]string"
	// dispatcherTokenMapEnd closes it (the map literal holds no nested braces).
	dispatcherTokenMapEnd = "}"
)

// quotedLiteral matches one double-quoted string literal.
var quotedLiteral = regexp.MustCompile(`"([^"]*)"`)

// dispatchCall matches one `isXxxSubcommand(<arg>)` condition: group 1 is the
// predicate name (XxxSubcommand — the same spelling the contract mirror
// lists, minus the `is` prefix), group 2 the argument it dispatches on.
var dispatchCall = regexp.MustCompile(`^is([A-Za-z0-9_]*Subcommand)\(([A-Za-z0-9_]+)\)$`)

// Drift is the comparison result for one branch tip against the merge
// target: two disjoint predicate-name sets, both empty when the tip is
// consistent with the contract suite it would land.
type Drift struct {
	// WiredUntracked are predicates the branch newly wires in main.go while
	// the contract suite (tip OR target — whichever side wins the merge)
	// does not track them. This is the bd3d34a5 voice_input shape.
	WiredUntracked []string
	// TrackedUnwired are predicates the branch stops wiring in main.go while
	// the contract suite still tracks them (the removal side of the same
	// drift — the test goes red on either side of the count mismatch).
	TrackedUnwired []string
}

// Empty reports whether the branch tip shows no contract drift.
func (d Drift) Empty() bool {
	return len(d.WiredUntracked) == 0 && len(d.TrackedUnwired) == 0
}

// Detail renders the refusal one-liner: what drifted, and the fix.
func (d Drift) Detail() string {
	var parts []string
	if len(d.WiredUntracked) > 0 {
		parts = append(parts, fmt.Sprintf(
			"main.go newly wires dispatch predicate(s) %v that %s does not track — add them to cliContractDispatchPredicates (and give each a cliContractInventory case) in this same branch",
			d.WiredUntracked, ContractSuiteFile))
	}
	if len(d.TrackedUnwired) > 0 {
		parts = append(parts, fmt.Sprintf(
			"main.go no longer wires dispatch predicate(s) %v that %s still tracks — remove them from cliContractDispatchPredicates (and their inventory cases) in this same branch",
			d.TrackedUnwired, ContractSuiteFile))
	}
	return strings.Join(parts, "; ")
}

// Compare reports the contract drift a branch tip INTRODUCES relative to the
// merge target it is about to land on.
//
// tip* is the branch tip's (main.go chain, contract mirror); target* is the
// merge target's (master's) pair. Scoping the comparison to what the branch
// CHANGED keeps the gate from refusing a branch for pre-existing drift it
// merely inherited (a rebase carries master's own main.go along), and the
// mirror coverage is the UNION of both sides because either side's
// cli_contracts_test.go can be the one that survives the merge:
//
//   - WiredUntracked = (wired(tip) − wired(target)) − (mirror(tip) ∪ mirror(target))
//   - TrackedUnwired = (wired(target) − wired(tip)) ∩ (mirror(tip) ∪ mirror(target))
//
// Both are over-approximations of "the merged tree will be inconsistent":
// the union can only suppress a refusal (a false negative on a branch whose
// test-file edit is masked by the target's), never manufacture one. A gate
// on the merge path must prefer a missed catch over a spurious refusal.
func Compare(tipMain, tipMirror, targetMain, targetMirror, tipTokens []string) Drift {
	tipWired := setOf(tipMain)
	targetWired := setOf(targetMain)
	tokenMapped := setOf(tipTokens)
	covered := map[string]bool{}
	for _, p := range tipMirror {
		covered[p] = true
	}
	for _, p := range targetMirror {
		covered[p] = true
	}
	var d Drift
	for _, p := range tipMain {
		// BOTH halves must hold for a predicate to count as tracked: the mirror
		// entry (the count match) AND an inventory token routed to it (the case
		// match). Only-the-mirror is the half-fixed branch that still lands red.
		if targetWired[p] || (covered[p] && tokenMapped[p]) {
			continue
		}
		d.WiredUntracked = append(d.WiredUntracked, p)
	}
	for _, p := range targetMain {
		if tipWired[p] || !covered[p] {
			continue
		}
		d.TrackedUnwired = append(d.TrackedUnwired, p)
	}
	sort.Strings(d.WiredUntracked)
	sort.Strings(d.TrackedUnwired)
	return d
}

// setOf indexes names for membership tests.
func setOf(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// ParseDispatchChain extracts the predicate names wired into main.go's
// dispatch chain (`is` prefix and `(dispatchArgs)` suffix stripped, in chain
// order). An error means the chain could not be located or parsed — the
// caller abstains rather than refusing.
func ParseDispatchChain(src string) ([]string, error) {
	start := strings.Index(src, dispatchChainStart)
	if start < 0 {
		return nil, fmt.Errorf("dispatch chain not found (looked for %q)", dispatchChainStart)
	}
	brace := strings.Index(src[start:], dispatchChainOpen)
	if brace < 0 {
		return nil, fmt.Errorf("dispatch chain unterminated")
	}
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(src[start:start+brace]), "if "))
	var (
		preds   []string
		arg     string
		skipped int
	)
	for _, tok := range strings.Split(line, dispatchJoin) {
		m := dispatchCall.FindStringSubmatch(strings.TrimSpace(tok))
		if m == nil {
			// main.go's chain is nothing but isXxxSubcommand calls; anything
			// else on the line means the chain changed shape.
			skipped++
			continue
		}
		if arg == "" {
			arg = m[2]
		} else if m[2] != arg {
			return nil, fmt.Errorf("dispatch chain mixes argument names (%q vs %q) — chain changed shape?", arg, m[2])
		}
		preds = append(preds, m[1])
	}
	if skipped > 0 {
		return nil, fmt.Errorf("dispatch chain carries %d non-predicate condition(s) — chain changed shape?", skipped)
	}
	if len(preds) < chainParseMinPredicates {
		return nil, fmt.Errorf("dispatch parse yielded only %d predicates — chain changed shape?", len(preds))
	}
	return preds, nil
}

// ParseDispatcherTokenMap extracts the predicate names that
// dispatcherOfToken's VALUES route tokens to (the map's keys are deliberately
// unresolved — see the anchor comment). An error means the map could not be
// located or parsed; the caller then supplies NO token coverage, which can
// only make the gate MORE willing to refuse (a fail-closed abstention would
// be wrong here: the branch's own drift is what the gate is judging).
func ParseDispatcherTokenMap(src string) ([]string, error) {
	body, err := listBody(src, dispatcherTokenMapStart, dispatcherTokenMapEnd)
	if err != nil {
		return nil, fmt.Errorf("dispatcher token map: %w", err)
	}
	var preds []string
	for _, m := range quotedLiteral.FindAllStringSubmatch(body, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		preds = append(preds, strings.TrimPrefix(name, mirrorNamePrefix))
	}
	return preds, nil
}

// ParseContractMirror extracts the predicate names from
// cliContractDispatchPredicates in the contract suite source (list order
// preserved). An error means the mirror could not be located or parsed — the
// caller abstains.
func ParseContractMirror(src string) ([]string, error) {
	body, err := listBody(src, mirrorListStart, mirrorListEnd)
	if err != nil {
		return nil, fmt.Errorf("mirror list: %w", err)
	}
	var preds []string
	for _, m := range quotedLiteral.FindAllStringSubmatch(body, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		preds = append(preds, strings.TrimPrefix(name, mirrorNamePrefix))
	}
	return preds, nil
}

// listBody returns the text between a list's opening anchor and its closing
// brace. A ZERO-length body is a shape error: callers must never accept an
// empty list as a valid parse ("no mirror at all" and "a mirror tracking
// nothing" must not be indistinguishable — an empty parse would silently
// satisfy a slice comparison).
func listBody(src, startAnchor, endAnchor string) (string, error) {
	start := strings.Index(src, startAnchor)
	if start < 0 {
		return "", fmt.Errorf("not found (looked for %q)", startAnchor)
	}
	rest := src[start+len(startAnchor):]
	openIdx := strings.Index(rest, listOpenBrace)
	if openIdx < 0 {
		return "", fmt.Errorf("unterminated (looked for %q)", listOpenBrace)
	}
	// The anchor text ends at `{`; the OPEN brace of the body is the next one.
	// A list whose element type itself ends in `{` (`[]T{`) re-states it, so
	// the first-`{`-after-the-anchor rule is what makes both the slice and the
	// struct-literal shape parse identically.
	rest = rest[openIdx+len(listOpenBrace):]
	if strings.Contains(rest[:len(rest)-len(strings.TrimLeft(rest, " \t\r\n"))], "}") {
		return "", fmt.Errorf("unexpected list shape (looked for a body right after %q)", listOpenBrace)
	}
	end := strings.Index(rest, endAnchor)
	if end < 0 {
		return "", fmt.Errorf("unterminated (looked for %q)", endAnchor)
	}
	body := rest[:end]
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("empty list body")
	}
	return body, nil
}
