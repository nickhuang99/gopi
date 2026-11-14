package tools

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/taskreport"
)

// GopiHomeEnvName is the ALIAS of the profile-home variable (config.GopiDirEnv —
// Rule 13/14: ONE definition, referenced here and never re-spelled). It exists so
// the shell-environment seam can ask its question without importing the name's
// value as a literal.
const GopiHomeEnvName = config.GopiDirEnv

// GopiHomeNamesCodeCheckout is the ALIAS of config's ONE predicate (Rule 14: the
// definition, its constants and the reasoning live in the leaf package; this
// package references it so the seam that assembles a child's environment and the
// caller that reports the strip cannot answer the same question differently).
func GopiHomeNamesCodeCheckout(home string) bool {
	return config.GopiHomeNamesCodeCheckout(home)
}

// Wrapper-child identity propagation (epic: reframe-telemetry-doublecount,
// 2026-09-10). `gopi task run` marks its child by EXPORTING the wrapper row's
// identity into the child's environment (TaskWrapperEnv, TaskChildSessionEnv —
// see cli_tasks.go runTaskRunOnce). The child reads those names to decide it
// already HAS a ledger row. That read is correct in the child and a LIE in
// every process below it: a bash/job descendant that re-launches gopi (a
// nested `go test ./internal/agent/`, a `python -c` driver, a shell loop)
// inherits the same env, votes itself the wrapper child, and writes ITS
// telemetry under the WRAPPER's task_id — session-01a0898e's single reframe
// dispatch ended up with 22 rows that way.
//
// So the identity is stripped from every shell the harness spawns: only the
// direct `task run` child (whose env was set with cmd.Env at spawn) may carry
// it. A descendant then auto-registers its OWN row — the provenance is
// honest again, and any row a descendant writes is visibly its own.
//
// single source of truth for the list: the exported env-name constants
// (TaskWrapperEnv / TaskChildSessionEnv from this package,
// signalrec.SessionDirEnv / signalrec.TaskIDEnv from internal/signalrec — a
// value shared across packages is referenced at its ONE definition site,
// never mirrored, Rule 14).

// signalrecProvenanceEnvVars are the SIGNAL-PROVENANCE handoff names this
// package strips for the same reason as the wrapper identity above: both name
// THIS process's own session directory. A descendant shell that re-launches
// gopi (a `go test` of an agent package, a `bash -c`) would otherwise file its
// provenance records into its PARENT's session — over-attribution in the file
// post-mortems are read from.
//
// internal/signalrec is importable here without a cycle (it depends only on
// internal/config), so these are the definition-site constants, not a mirror.
var signalrecProvenanceEnvVars = []string{signalrec.SessionDirEnv, signalrec.TaskIDEnv}

// wrapperIdentityEnvVars is the wrapper-child identity set stripped from every
// spawned shell (Rule 2/13: defined once, referenced by bashChildEnv + JobStart).
//
// TaskRunResumeFromEnv travels with the pair for the SAME reason: it is the
// door's declaration about THIS run ("this run continues session X"), read by
// taskRunResumeWitness to stamp the notify payload. A shell that re-launched
// gopi while inheriting it would announce a resume it is not part of — a false
// witness is worse than an absent one (Rule 15), so it is stripped at exactly
// the place the wrapper identity is stripped.
var wrapperIdentityEnvVars = append([]string{TaskWrapperEnv, TaskChildSessionEnv, TaskRunResumeFromEnv}, signalrecProvenanceEnvVars...)

// reportContractEnvVars is the report contract PAIR stripped from every
// spawned shell by the ownership predicate below (Rule 2/13: defined once,
// referenced by bashChildEnv + JobStart).
var reportContractEnvVars = []string{taskreport.PathEnv, taskreport.OwnEnv}

// stripWrapperIdentity returns a copy of env without the wrapper-child
// identity vars, plus the names that were actually removed (the caller logs
// the strip — a silent strip would hide which identity was inherited).
//
// It also drops the SPEND-BOUNDARY names (tools.SpendBoundaryEnvVars): the
// heavy-set opt-in and the operator's terminal declaration must never reach a
// descendant shell. A shell is where an hour of heavy tests would be spent
// (`go test ./...` typed into it, or a script it runs), and an inherited
// declaration would let that shell claim to be the operator.
func stripWrapperIdentity(env []string) (stripped []string, removed []string) {
	stripped = make([]string, 0, len(env))
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if isWrapperIdentityEnv(name) || isSpendBoundaryEnv(name) || isForeignReportContract(env, name) {
			removed = append(removed, name)
			continue
		}
		if IsCheckoutHomeEnv(name, value) {
			removed = append(removed, name)
			continue
		}
		stripped = append(stripped, kv)
	}
	return stripped, removed
}

// IsCheckoutHomeEnv reports whether the env entry (name, value) publishes a
// GOPI_HOME that NAMES A CODE CHECKOUT, i.e. one a spawned shell must not
// inherit.
//
// WHY A SHELL MUST NOT INHERIT IT (epic: no child resolves a throwaway profile).
// GOPI_HOME is documented as a HOME; the profile root is `<GOPI_HOME>/.gopi`.
// A run standing in its own git worktree can have that worktree handed to it as
// GOPI_HOME (a launcher's exported value, a script's `export GOPI_HOME=$PWD`, a
// sandbox that IS the checkout) — and then every descendant shell resolves
// `<worktree>/.gopi` as its profile: a throwaway profile nothing sweeps, whose
// files DIRTY the checkout, and into which notify deliveries vanish without a
// trace. Measured 2026-10-15 on this run: its bash-tool child carried
// GOPI_HOME=<the run worktree>.
//
// THE KEPT ARM IS THE LOAD-BEARING HALF (Rule 41: the criterion, not a guess).
// `GOPI_HOME=<sandbox>` — a real directory holding `.gopi`, which is exactly
// what `gopi tool sandbox` materialises — carries NO checkout marker, so it is
// NOT refused and keeps inheriting down: the sandboxed-profile path the whole
// machinery depends on is untouched. The predicate is config's ONE definition
// (config.GopiHomeNamesCodeCheckout); this function only asks it about an env
// entry, and only for the variable that denotes a home.
func IsCheckoutHomeEnv(name, value string) bool {
	if name != GopiHomeEnvName {
		return false
	}
	return GopiHomeNamesCodeCheckout(value)
}

// checkoutHomeStripRecord renders the ONE-LINE record a spawned shell's caller
// emits when GOPI_HOME was dropped because it named a code checkout (Rule 15:
// the strip is announced, with the value and the reason). Returned as a string
// so the seam that reads the environment can render it from the SAME values the
// strip used, and so a test can assert the record without capturing stderr.
//
// It renders nothing for every other removal: the wrapper-identity and report
// strips have their own reported surfaces, and a checkout strip is the one this
// seam can newly cause.
func checkoutHomeStripRecord(removed []string, env []string) string {
	dropped := false
	for _, name := range removed {
		if name == GopiHomeEnvName {
			dropped = true
			break
		}
	}
	if !dropped {
		return ""
	}
	value := ""
	for _, kv := range env {
		if name, v, ok := strings.Cut(kv, "="); ok && name == GopiHomeEnvName {
			value = v
			break
		}
	}
	return fmt.Sprintf(
		"%s the spawned shell does NOT inherit %s=%s: that value names a code "+
			"checkout (it carries %s), so its profile root would be a throwaway "+
			"inside the checkout itself. The shell resolves the operator's own "+
			"profile instead; set %s to a real home to give it a private one.\n",
		checkoutHomeStripPrefix, GopiHomeEnvName, value, config.CheckoutMarkerName, GopiHomeEnvName)
}

// checkoutHomeStripPrefix opens the record above. A named constant so the
// producer and any consumer that greps for it agree (Rule 1/14).
const checkoutHomeStripPrefix = "[gopi] environment:"

// stripCheckoutHome and its companions below are exercised through
// stripWrapperIdentity (the ONE list every spawned shell is built from); the
// exported predicates are the seam cmd/agent calls for its own report.

// isSpendBoundaryEnv reports whether name is one of the spend-boundary names.
func isSpendBoundaryEnv(name string) bool { return IsSpendBoundaryEnv(name) }

// IsSpendBoundaryEnv reports whether name is one of the spend-boundary names
// (SpendBoundaryEnvVars — the single source of truth; this is the predicate,
// not a second list). Exported because cmd/agent's executor spawn seam strips
// the same two names and must ask the same question (Rule 2: one predicate).
func IsSpendBoundaryEnv(name string) bool {
	for _, v := range SpendBoundaryEnvVars {
		if name == v {
			return true
		}
	}
	return false
}

// isForeignReportContract reports whether the env entry name belongs to the
// task-report contract AND that contract is NOT this process's own.
//
// WHY the report pair is conditional where the identity names above are
// unconditional (epic nested-report-path-clobber, 2026-09-15): the path is the
// destination the SESSION writes its narrative to, and the documented way it
// does so is through the bash tool (`cat > "$GOPI_TASK_REPORT_PATH"` — the
// seam's own contract, exercised by the cmd/agent seam test). Stripping it
// unconditionally would break the very run that legitimately owns it, so
// ownership decides, and ownership is a property of the DESTINATION: the pair
// survives only when the path lies inside the session directory THIS process
// recorded for itself (taskreport.ProcessOwnsContract — the leaf package's ONE
// predicate; no marker is required, because the harness exports none).
//
// That one rule cuts the whole inheritance chain the defect came from: a bash
// tool call by the session itself keeps its destination (the report gets
// written), while a bash call by ANY other process — a parent session's shell
// before this one existed, a nested gopi's parent, a shell that never recorded
// a session directory — loses the pair, so a `gopi` launched from that shell
// can neither read the path (the startup guard in internal/agent refuses it)
// nor write it (StripInheritedReportContract at its own spawn). The path was
// never that shell's, so nothing a descendant does with it is legitimate.
//
// An entry whose name is PathEnv/OwnEnv but carries an EMPTY value is removed
// with the rest: an empty PathEnv is not a destination, and leaving it behind
// would only invite a reader to treat it as one.
func isForeignReportContract(env []string, name string) bool {
	if name != taskreport.PathEnv && name != taskreport.OwnEnv {
		return false
	}
	// The ownership decision is the leaf package's ONE predicate
	// (taskreport.ProcessOwnsContract), judged against the directory THIS
	// process recorded for its own session (signalrec.SessionDir — the
	// in-process authority on that fact, kept current by cmd/agent at session
	// start). Where the process has recorded no directory there is nothing it
	// can prove ownership against, so the pair is treated as foreign and
	// dropped: the safe direction, because the pair's only legitimate use is
	// writing the session's own report through the bash tool.
	return !taskreport.ProcessOwnsContract(signalrec.SessionDir())
}

// isWrapperIdentityEnv reports whether name is one of the wrapper-child
// identity vars (exact match — a prefix match would also strip GOPI_TASK_*).
func isWrapperIdentityEnv(name string) bool {
	for _, v := range wrapperIdentityEnvVars {
		if name == v {
			return true
		}
	}
	return false
}

// StripInheritedReportContract returns env without the task-report contract
// pair (taskreport.PathEnv / taskreport.OwnEnv) and the names actually
// removed. It is the UNCONDITIONAL half of the report hygiene, for the one
// caller that is about to (re)export its own contract or owns no report at
// all: the `task run` wrapper. That process must never pass an inherited path
// down — its child is a DIFFERENT run, whose report belongs at its own path —
// so the pair is cleared here and the wrapper's own pair is appended at the
// spawn seam (cmd/agent cli_tasks.go), which is the single writer of both
// names (Rule 2).
//
// The conditional half (isForeignReportContract, used by
// stripWrapperIdentity) covers every OTHER process in the tree: it keeps the
// pair only when the owner marker names the process itself, which is exactly
// the executor about to write its narrative.
func StripInheritedReportContract(env []string) (stripped []string, removed []string) {
	stripped = make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == taskreport.PathEnv || name == taskreport.OwnEnv {
			removed = append(removed, name)
			continue
		}
		stripped = append(stripped, kv)
	}
	return stripped, removed
}
