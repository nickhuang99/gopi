// Package tasktags is the single source of truth for task-ledger TAG
// values shared across packages (Rule 14: a value two packages must agree
// on lives in ONE small exported-constant leaf package — never mirrored,
// never typed inline, Rule 1b/13).
//
// The tag string is a persisted value: it is written into tasks.tags,
// compared by exact comma-token match at every consumer, and searched by
// the bug-flow tally. A second definition of these strings, or a bare
// literal at a consumer, silently drifts from the rows already on disk —
// that is precisely the rule-14 failure this package exists to prevent.
//
// taskverdict is the sibling precedent (the reframe verdict vocabulary) and
// this package follows the same shape: constants only, no imports of
// project packages, so no consumer can create an import cycle.
package tasktags

const (
	// BugTag is the reserved bug-flow tag: the entry point of the
	// bug-submit → dispatch → verify closure. An EXACT comma-token match
	// ("bug" inside "bugflow" or "debug" does NOT count — the tag set is
	// comma-split, never substring-matched).
	BugTag = "bug"

	// VerifiedReworkTag marks an append-only follow-up row that exists
	// because the row it points at (parent_task_id) was RE-EXAMINED AFTER
	// it had already been verified. It is carried by the follow-up row,
	// never by the completed work row.
	//
	// Why a tag rather than a status: the verified-closure gate exists to
	// stop a completed bug row from being declared done without an
	// independent verdict. That gate is keyed on the row's STATE, and a
	// follow-up row is itself a completed bug row whose only deliverable is
	// the re-examination itself. Without this marker the gate would demand
	// new work after every re-examination — the exact recursion the gate's
	// own anti-loop guard is there to prevent. The marker makes the
	// follow-up self-describing on disk (visible in `gopi task show`), so
	// the exemption is data a reader can see and audit, not a hidden
	// special case in a predicate.
	VerifiedReworkTag = "verified-rework"

	// ReworkLineageTag marks a row that is the REWORK SUCCESSOR of the row it
	// names as parent_task_id — the rows the verify FAIL verdict mints
	// (`<id>-r1`) and the branch-rework lanes file.
	//
	// WHY IT EXISTS (the defect it closes, measured 2026-10-21). Rule 33's
	// dependency gate asks "is the parent's work SATISFIED?" and answers it
	// from taskParentReleaseStatuses {completed, verified, superseded}, where
	// `failed` and `cancelled` park the child FOREVER. That is the right
	// reading for an ordinary child — it built on the parent's deliverable,
	// and a failed parent delivered nothing. It is the WRONG reading for a
	// rework row: the rework exists BECAUSE the parent failed, so its edge
	// means "the parent row has reached its verdict, whatever it was", not
	// "the parent succeeded". Without this marker every `<id>-r1` row is born
	// [queue-skip] — parked by the very verdict that asked for it.
	//
	// So the marker selects a DIFFERENT EDGE SEMANTICS, never a wider release
	// set: an unmarked child of a failed parent still parks (adding `failed`
	// to taskParentReleaseStatuses would have released all of them, which is
	// the semantic error this tag exists to avoid). The predecessor of this
	// approach is internal/tools/rework_reparent_children.go: "THE FIX IS THE
	// EDGE, NEVER THE RELEASE SET".
	//
	// The string is a PERSISTED value (it lands in tasks.tags), so it is
	// pinned by this package's spelling test like its siblings.
	ReworkLineageTag = "rework"

	// RerouteTag marks an append-only successor row that exists because the row
	// it names as parent FAILED WITHOUT STARTING — its own recorded evidence
	// (category=network, http=0, iter=0, recoverable=true) says the first
	// request never left the host, so the machine, not the work, is what
	// failed. It rides BESIDE ReworkLineageTag, never instead of it: the
	// dependency edge is the rework edge (Rule 33 — the parent reached a
	// terminal verdict, which is what a failed parent is), while this tag
	// records WHY the successor was minted, so the dispatch tally and the
	// operator's audit can tell a machine-health reroute from a verdict rework
	// without re-reading the parent's failure digest.
	//
	// The string is a PERSISTED value (it lands in tasks.tags), so it is pinned
	// by this package's spelling test like its siblings.
	RerouteTag = "reroute"

	// CloneLineageTag marks a CLEAN-ROOM COPY row: the row `gopi task clone`
	// minted, whose `parent_task_id` names the row it was copied from.
	//
	// WHY IT EXISTS (operator ruling 2026-11-05, memory #27273). A successor
	// row's lineage used to be carried by its ID SPELLING — `<uuid>-c1` — and
	// that is the defect: an id with a suffix appended is not a uuid any more,
	// so every downstream reader that treats the id as an identity
	// (parent_task_id, deliverable, notify's session routing, `--resume`'s
	// selector) stops being able to tell which row is meant. A NEW row now gets
	// a NEW uuid, and the FACT that it is a copy is data on the row — this tag,
	// beside ReworkLineageTag and the ReworkOfTagPrefix entry naming the source.
	//
	// The string is a PERSISTED value (it lands in tasks.tags), so it is pinned
	// by this package's spelling test like its siblings.
	CloneLineageTag = "clone"

	// ReworkOfTagPrefix prefixes the tag that NAMES the row a successor was
	// minted from: `rework-of:<source-task-id>`. It rides BESIDE
	// ReworkLineageTag (the edge semantics, Rule 33) and beside CloneLineageTag
	// (why the successor exists), never instead of either.
	//
	// Why the source id is DATA on the row rather than a slice of its id: the
	// same ruling. `parent_task_id` already carries the edge, so this tag is the
	// human- and grep-readable half — an operator scanning `gopi task show`
	// reads which row was copied without resolving the parent column.
	//
	// The value is a PERSISTED prefix (it lands in tasks.tags), pinned by this
	// package's spelling test like its siblings.
	ReworkOfTagPrefix = "rework-of:"

	// FixtureTag is the reserved FIXTURE tag: a row seeded to DECORATE a queue
	// (a rig's proof ledger, a demo, a screenshot) rather than to be executed.
	// It is a first-class unclaimable marker, not a convention: the ONE shared
	// claimability predicate (taskRowClaimable) refuses any row carrying it, so
	// the row is [queue-skip] BY CONSTRUCTION in the pending view and can never
	// be claimed, transferred or dispatched through any door.
	//
	// Why it exists (measured 2026-10-13): a rig seeded two labelled pending
	// rows into a profile its own foreground workers drained, and BOTH were
	// claimed and executed as real agent runs — one ran 22 turns / 998k tokens.
	// A claim cannot be recalled by the rig that seeded the row: a claimed row
	// belongs to the worker. The recipe that worked before this tag existed (a
	// fixture parented to a row that never reaches completed, parked forever by
	// Rule 33) is still valid and is pinned alongside this one.
	//
	// The string is a PERSISTED value (it lands in tasks.tags), so it is
	// pinned by a test in this package like its siblings.
	FixtureTag = "fixture"
)
