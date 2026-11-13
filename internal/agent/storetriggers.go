package agent

// eventDrivenStoreTriggers is the EVENT-DRIVEN half of the store-timing
// contract and the single definition both renderings below and the guard test
// reference (Rule 1b/13 — a prose contract duplicated into a test drifts the
// moment either side is edited; the test asserts BOTH the constant and its two
// renderings).
//
// Why events and not only "at task end": the pre-existing routing line said
// "mid-task notable find/pitfall → store NOW" but named NO event a model can
// RECOGNIZE in the conversation. The three below are exactly the moments the
// knowledge exists and the context is about to move on — and 上下文随时可能重置,
// so a lesson held "for the final answer" is a lesson that can be lost before
// it is written. Task end is therefore the FLOOR of the contract, never its
// ceiling. (Harness-side hard guarantees stay where they were:
// maybeInjectTaskStartSearch / maybeEnforceCompletionStore.)
//
// It lives in the two surfaces with NO prompt-density budget: the
// harness-injected task-start checkpoint (taskStartSearchMessage) and the
// memory tool description — the routing block is already at its 4500-char
// density ceiling (measured 4495/4500), so event text there would only buy a
// compression fight.
//
// It sits in its own file deliberately: loop.go is where the corpus's
// `loop.go:NNN` citations point (devdoc/hippocampus.md, agent-five-organs-
// ontology.md, chat-context-self-knowledge.md and friends), so growing it
// inside the existing constants would re-key those line numbers and make a
// dozen landed docs stale. That cost is real — a citation is prose a human has
// to re-point by hand — and it is why the string lives here.
//
// HISTORY, so nobody satisfies a check that does not exist. The gate that
// ASSERTED those citations (the now-deleted devdoc line-anchor gate) was
// deleted with internal/docclass in 5a35f61. Nothing validates a `loop.go:NNN`
// reference now: the docs still
// spell them, and they can rot silently. The census that replaced that gate
// (`config-anchor-census.go --check`) went with the documentation-citation
// family in 78bdf05a (2026-11-05), so NOTHING executable checks an anchor now.
// The reason to prefer a new file is churn, and only churn.
//
// SPLIT (chat-memory-timing): the string is now two definitions, because it has
// two KINDS of reader. eventDrivenStoreEventList is the EVENTS — the half that is
// true of every surface that can store at all, the chat block included.
// eventDrivenStoreTaskEndClause is the worker's framing of WHY the events are a
// floor, and it is FALSE on a chat turn: "task end" is a moment a conversation
// does not have. eventDrivenStoreTriggers keeps the concatenation, so both task
// surfaces (loop.go's checkpoint and the memory tool description) and the guard
// tests read the byte-identical contract they read before — no rendering drifted,
// only the chat block got a way to cite the events WITHOUT inheriting a deadline
// it cannot reach.
const eventDrivenStoreEventList = `the moment the user says 记住 / remember this, teaches you a mechanism, rule or pitfall, ` +
	`or a recurring pitfall gets solved — memory(action=store) THAT round, because 上下文随时可能重置, 未记录=丢失`

// eventDrivenStoreTaskEndClause is the TASK-RUN framing of the floor: true of a
// worker (a task really does end, and the summary is really its last chance) and
// false of a chat turn (there is no end to defer to — the operator's instruction
// is the trigger). It is a separate definition so the chat block can cite the
// events while REFUSING this clause, instead of rendering it verbatim and
// asserting a deadline that does not exist on that surface (Rule 15: a false
// instruction is worse than a missing one, because it is followed).
const eventDrivenStoreTaskEndClause = `(task end is the floor, not the ceiling)`

// eventDrivenStoreTriggers is the FULL worker-facing contract = the events + the
// task-end framing. Byte-identical to its pre-split spelling; the two task
// surfaces and TestEventDrivenStoreTriggers are unchanged.
const eventDrivenStoreTriggers = eventDrivenStoreEventList + " " + eventDrivenStoreTaskEndClause
