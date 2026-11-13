package tasksource

import (
	"strings"

	"github.com/nick/gopi/internal/config"
)

// Derive resolves the source value ONE row is written with. It is the single
// enforcement point of the operator's ruling: a human claim survives only when
// the process provides evidence for it.
//
// The ladder, in order (each step is a measurement, not a preference):
//
//  1. requested == "" (the writer expressed no preference): the value is
//     DERIVED from the process signature (DefaultFor) — never defaulted to
//     human.
//  2. requested is a HUMAN claim: kept iff the signature SUPPORTS it
//     (HumanEvidence). An agent session asking for "human" is downgraded to the
//     derived non-human value — this is the abuse the package exists to stop.
//     A caller that wants the claim REFUSED rather than rewritten asks
//     HumanClaimDeniedNow first (the CLI's `--source human` flag does: a typed
//     claim is refused, a library default is derived).
//  3. requested is the retired spelling "auto": no role was expressed, so it is
//     re-derived (a machine row's honest role is known by the writer, which is
//     why every merger/worker/server call site now names its constant).
//  4. anything else known: kept as written (a role that speaks for itself —
//     worker, merger, server, agent — needs no evidence).
//  5. an UNKNOWN spelling: re-derived. The ledger boundary (TaskSubmit)
//     refuses an unrecognized spelling BEFORE calling here, so this branch
//     only covers a library caller that skipped that validation — and even
//     then the row records an honest role rather than a fabricated one.
func Derive(requested string, sig config.AgentSpawnSignature) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return DefaultFor(sig)
	}
	if IsHumanClaim(requested) {
		if HumanEvidence(sig) {
			return Human
		}
		return DefaultFor(sig)
	}
	if requested == legacyAuto {
		// "auto" is the retired conflation; the writer did not say which
		// machine role it is, so the honest record is the derived one.
		return DefaultFor(sig)
	}
	if value, ok := Normalize(requested); ok {
		return value
	}
	return DefaultFor(sig)
}

// DeriveNow is Derive against the LIVE process signature. It is the only place
// the environment is read for provenance, so a test can drive Derive with a
// synthesized signature while production has exactly one env reader.
func DeriveNow(requested string) string {
	return Derive(requested, config.AgentSpawnSignatureNow())
}

// HumanClaimDeniedNow answers the SAME question Derive asks (HumanEvidence
// against the live process signature) as a PREDICATE a caller can REFUSE on.
//
// It exists so an explicit `--source human` gets a loud refusal instead of the
// silent rewrite Derive performs for a LIBRARY caller — measured 2026-09-23:
// `gopi task submit … --source human` inside an agent session exited 0 and
// stored `agent`, with the real rule living only in a source comment, while
// `-h` advertised human as a selectable value (Rule 15: a refusal the caller
// cannot see is a silent failure). Derive keeps its rewrite for the writers
// that pass a source as an internal default; the TYPED FLAG is refused.
func HumanClaimDeniedNow() bool {
	return !HumanEvidence(config.AgentSpawnSignatureNow())
}

// HumanEvidence reports whether this process may claim a PERSON filed the row.
//
// Two independent evidences are accepted:
//
//   - the operator's explicit per-invocation claim (GOPI_HUMAN_TERMINAL), the
//     same escape the spawn guard honours, and
//   - the ABSENCE of any agent chain above this process (sig.AgentRun() is
//     false) — the CLI runs where a person typed it; that is the strongest
//     signal available without an authentication channel.
//
// The worker check is NOT redundant with AgentRun(). AgentRun() deliberately
// reports false for a worker's own child — the spawn guard EXEMPTS that chain
// (it is the operator's sanctioned executor) — so `!AgentRun()` alone would
// hand the executor's follow-up rows a human claim. Measured by this package's
// own table: without the guard below, "worker child asks for human" returned
// `human`. A worker's child is machinery; it may claim a person only through
// the operator's explicit terminal claim, checked first.
func HumanEvidence(sig config.AgentSpawnSignature) bool {
	if sig.HumanTerminal {
		return true
	}
	if sig.ParentIsWorker {
		return false
	}
	return !sig.AgentRun()
}

// DefaultFor derives the honest source for a row whose writer expressed no
// preference, from the process signature alone.
//
// Ordering matters: a worker's own child is inside an agent session (the
// executor IS an LLM session), so the procfs parent check must be consulted
// BEFORE the environment signature — otherwise every worker-filed follow-up row
// would read `agent` and lose the distinction the operator asked for.
func DefaultFor(sig config.AgentSpawnSignature) string {
	if sig.ParentIsWorker {
		return Worker
	}
	if sig.AgentRun() {
		return Agent
	}
	return Human
}
