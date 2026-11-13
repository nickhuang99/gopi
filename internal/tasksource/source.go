// Package tasksource is the single source of truth for a task row's
// PROVENANCE — the ledger's `source` column — and it is a LEAF package (only
// constants and the derivation of one value) so every writer can import it
// without creating a cycle (Rule 14).
//
// WHY IT EXISTS (the defect it closes, measured 2026-09-23). `gopi task submit`
// used to default the source to "human": cmd/agent's runTaskSubmit had
//
//	if source == "" { source = "human" }
//
// so EVERY row an AGENT filed through the CLI landed as a human claim — on the
// operator's ledger 779 rows read `human`, and 457 of them carried a session id
// and a model (they were agent rows). The operator's ruling, verbatim:
// 「only true human can claim human, for other cases, either 'merger', worker,
// server, or simply agent are all acceptable sources」.
//
// THE VOCABULARY (one home, Rule 14 — no writer spells one of these inline):
//
//	human  — the ONLY value that claims a person filed the row. Granted on
//	         measured evidence, never as a default.
//	agent  — an LLM agent session filed it (a task_submit tool call, a reframe
//	         decomposition, a `gopi task submit` typed from an agent's shell).
//	         "llm" is the historical spelling of this value and stays an
//	         accepted ALIAS on input.
//	worker — the task executor's own follow-up row (the worker loop, a
//	         verify-driven re-open).
//	merger — the merge path's machine-enqueued row (conflict / hand-over /
//	         ceiling-rework backlog). "auto" is the historical spelling it
//	         shared with the harness and stays an accepted ALIAS on input.
//	server — the resident node machinery: startup self-registration, and rows
//	         replicated by the sync layer.
//	forward — a row a MIRROR LEG forwarded to the authoritative ledger
//	         (internal/tools/task_submit_forward.go): its author is the
//	         operator/agent on the submitting node, its HOME is the peer's
//	         ledger. It is NOT the merger's machine enqueue, which is what the
//	         retired "auto" alias made every forwarded row read as.
//	agency-proposal — the unattended miner's proposal row (internal/agencyscan
//	         via internal/tools/agency.go): not a person, not an LLM tool call,
//	         not the merge path. Its dedup counts rows carrying the
//	         agency-proposal TAG; the source names the same thing.
//	mirror-terminal — a TERMINAL row this ledger only COPIES: the sync layer's
//	         mirror-history insert for a finished row it had never seen
//	         (internal/tools/memory_sync_task.go importTaskMirrorTx). It is
//	         NOT `server`: a fresh replicated row is this node's own enqueue
//	         of work to run (server), while a mirrored terminal row is never
//	         enqueued here — it is the record of a run that happened elsewhere,
//	         and `server` would narrate an enqueue this node never made. It is
//	         NOT `forward` either: `forward` is the OTHER direction — a row
//	         THIS node filed to a peer, whose local copy the transport
//	         produced. Arrival of somebody else's finished work and departure
//	         of this node's own row are different sentences, so the DECISION
//	         (recorded on the constant) is two values, not one.
//
// THE EVIDENCE (reused, never re-invented). config.AgentSpawnSignatureNow() is
// the process-signature reader the worker/`task run` spawn guard already
// maintains (internal/config/agentguard.go, researched on the live host
// 2026-09-12): GOPI_EXECUTOR_SESSION_ID / HERMES_SESSION_ID / GOPI_AGENT_SESSION
// (pid-anchored) / GOPI_HUMAN_TERMINAL, plus the procfs parent check. It is the
// ONE definition of "is this process inside an agent session", so provenance
// reads the same signals the refusal does instead of a second, drifting copy.
//
// HONEST LIMIT (Rule 15 — no silent claim). The signature travels in the
// ENVIRONMENT, so the derivation can prove "no agent chain sits above this
// process"; it cannot prove the presence of a person. An unattended script
// whose environment is clean therefore still reads `human`. That residual is
// deliberate and named: it is the same evidence the spawn guard already trusts
// for the operator's own escape, and it is strictly better than a default that
// stamped `human` unconditionally. A deliberate forgery
// (`env -u GOPI_EXECUTOR_SESSION_ID gopi task submit …`) defeats it exactly as
// it defeats the guard.
package tasksource

import "strings"

// The ledger's provenance vocabulary (Rule 1b/14: the ONLY place these strings
// are written; every writer references the constant).
const (
	// Human is the sole claim of a person's authorship. It is granted on
	// measured evidence (see Derive) and never as a default.
	Human = "human"
	// Agent is an LLM agent session's own row.
	Agent = "agent"
	// Worker is the task executor's follow-up row.
	Worker = "worker"
	// Merger is the merge path's machine-enqueued row.
	Merger = "merger"
	// Server is the resident node machinery's row (self-registration, sync
	// replication).
	Server = "server"
	// Forward is a row a MIRROR LEG forwarded to the authoritative ledger.
	// The writer is internal/tools/task_submit_forward.go (the wire row AND
	// the mirror echo); the author is the submitting node's operator/agent,
	// and the row's HOME is the peer's ledger. It is its own value because
	// the retired "auto" alias resolved to Merger, narrating every forwarded
	// submit as a merge-path enqueue.
	Forward = "forward"
	// AgencyProposal is the unattended miner's proposal row: internal/agencyscan
	// mines the ledger and files what it finds through the ONE submit funnel
	// (internal/tools/agency.go), since a LEAF package cannot own the write.
	// The miner's own tag (agencyscan.TagAgencyProposal) carries the identical
	// spelling; the two name the same thing — a row no person and no LLM tool
	// call filed.
	AgencyProposal = "agency-proposal"
	// MirrorTerminal is a TERMINAL row this ledger only COPIES — the
	// mirror-history insert the sync layer does for a finished row it has
	// never seen (importTaskMirrorTx, internal/tools/memory_sync_task.go).
	//
	// THE DECISION (Rule 15: a provenance claim is never silently rounded).
	// The sync layer writes provenance at TWO shapes, and they are two values
	// because they answer different questions:
	//
	//	FRESH replication (importTaskFreshTx) — the row arrives as PENDING
	//	work this node will run. The row IS an enqueue, and the enqueue is
	//	this node's own machinery, so it carries Server (an exporter that
	//	names its own source keeps it; Server is the default when the
	//	exporter names none).
	//	MIRROR insert (importTaskMirrorTx) — the row arrives TERMINAL and is
	//	never run here (it never touches the runnable set). Nothing about it
	//	is an enqueue, so it does not carry Server; and it is not the
	//	transport's own product the way a forward's local echo is
	//	(tasksource.Forward), because the work is somebody else's and simply
	//	arrives. MirrorTerminal is that sentence: copied history.
	//
	// Before this value existed the INSERT spelled the raw literal "sync" —
	// a spelling NO validator accepted: tasksource.Known("sync") = false,
	// Normalize("sync") = ("", false), so the row `task show` rendered
	// through orDash kept its raw spelling, and any read-side validation
	// (validTaskSource) would have refused the ledger's own writer's output.
	MirrorTerminal = "mirror-terminal"
)

// The retired spellings the ledger still carries and the operators still type.
// They are ALIASES, not vocabulary: a row written today never carries one, and
// Normalize maps an incoming one onto the value it always meant.
const (
	// legacyLLM is the historical spelling of Agent (internal/agent's
	// taskSourceLLM, and ~633 rows on the operator's ledger).
	legacyLLM = "llm"
	// legacyAuto is the historical spelling the merger and the harness SHARED
	// (~993 rows). Its two meanings cannot be told apart in a stored row, which
	// is exactly why it was retired: a reader could not say whether a row was
	// machine-enqueued by the merge path or auto-registered at startup.
	legacyAuto = "auto"
)

// lexical is every spelling the ledger accepts, current vocabulary first. It
// is the ONE list validTaskSource-style readers consult (Rule 14).
var lexical = []string{Human, Agent, Worker, Merger, Server, Forward, AgencyProposal, MirrorTerminal, legacyLLM, legacyAuto}

// Known reports whether name is an accepted source spelling (current vocabulary
// or a legacy alias). It is the ledger's validation predicate.
func Known(name string) bool {
	for _, v := range lexical {
		if name == v {
			return true
		}
	}
	return false
}

// Normalize maps an accepted spelling onto the current vocabulary. ok reports
// whether the input was RECOGNIZED AT ALL — a current value (returned
// unchanged) and a retired alias (returned as the value it always meant) are
// both ok; only an invented spelling returns ("", false).
//
// The distinction between "current" and "alias" is made by comparing the result
// with the input, NOT by a second boolean: an earlier revision returned
// `modern` here and a caller read it as "recognized", which silently skipped
// canonicalization for exactly the aliases the branch existed for. One
// question, one answer.
func Normalize(name string) (value string, ok bool) {
	switch name {
	case Human, Agent, Worker, Merger, Server, Forward, AgencyProposal, MirrorTerminal:
		return name, true
	case legacyLLM:
		return Agent, true
	case legacyAuto:
		// "auto" claimed nothing specific: the harness's startup registration
		// and the merger's machine enqueue both wrote it. Reading it as the
		// MERGER (its majority writer, and the one still writing it when this
		// package landed) is the honest resolution for HISTORY; a WRITER
		// passing "auto" today is re-derived instead (Derive), because a
		// caller that did not say which role it is has no claim to record.
		return Merger, true
	}
	return "", false
}

// IsHumanClaim reports whether name asserts a person's authorship — the one
// claim the framework checks for evidence.
func IsHumanClaim(name string) bool {
	return strings.TrimSpace(name) == Human
}

// Policy renders the accepted vocabulary for a human-readable error message, so
// every refusal says the same thing (the retired aliases are NAMED — a caller
// passing "llm" or "auto" must not read as a typo).
func Policy() string {
	return strings.Join([]string{
		Human, Agent + " (" + legacyLLM + ")", Worker, Merger + " (" + legacyAuto + ")", Server, Forward, AgencyProposal, MirrorTerminal,
	}, "|")
}
