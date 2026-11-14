// Package toolnames is the single source of truth for the agent's tool
// names. Every consumer that references a tool by name — the schema
// definitions, the dispatch switch, the parallel-safety / polling /
// content-returning maps, context-trim logic, the raw-log formatter, the
// CLI mirrors — MUST use these constants, never a bare string literal
// (Rule 1: named constants). Renaming or merging a tool is then a one-line
// change in this file instead of an N-site grep-and-replace.
//
// The names' string VALUES are the wire protocol with the LLM — the model
// emits exactly these strings as tool_use names. Do not change a value
// here without changing it everywhere the model is told (schema Names and
// the routing prompt), i.e. without an actual rename task.
package toolnames

// ToolTask action wire values — the task tool's "action" argument values.
// Single source of truth (Rule 1): never type "view"/"commit"/"review"
// inline where the task action is dispatched on, validated, or described.
const (
	ToolTaskView    = "view"
	ToolTaskCommit  = "commit"
	ToolTaskReview  = "review"
	ToolTaskReframe = "reframe"
)

// ToolJobAction values are the job tool's action wire parameter — the
// model emits exactly one of these as the "action" argument value.
// Single source of truth (Rule 1): never type "start"/"output"/"kill"
// inline where a job action is dispatched on, validated, or described.
const (
	ToolJobActionStart  = "start"
	ToolJobActionOutput = "output"
	ToolJobActionKill   = "kill"
)

// ToolMemory action wire values — the memory tool's "action" argument
// values. Single source of truth (Rule 1): never type "search"/"store"/
// "update"/"feedback" inline where a memory action is dispatched on,
// validated, or described. The values mirror the retired memory_* tool
// wire names (memory_search etc.) minus the "memory_" prefix — a model
// that used to call memory_search now calls memory(action=search).
const (
	ToolMemoryActionSearch   = "search"
	ToolMemoryActionStore    = "store"
	ToolMemoryActionUpdate   = "update"
	ToolMemoryActionFeedback = "feedback"
)

// Tool ARGUMENT wire names — the `input` keys the model emits inside a
// tool_use block, and the keys the dispatcher reads back out of
// args (map[string]interface{}). They are the second half of the same wire
// protocol as the tool names above, and they are named here for the same
// reason (Rule 1/13: a key typed inline at one site and read inline at
// another is a defect waiting for a typo).
//
// WHY THIS BLOCK EXISTS AT ALL — measured 2026-10-04, task
// 01a103c2-36e2-7808-bc42-d652762c4691, host peer1. In one session's raw log
// 17 `edit` tool_use blocks were recorded, and 6 of them carried their
// replacement text under the key `newText` instead of `newStr`. The
// dispatcher read the absent `newStr` as "" and called
// tools.Edit(path, oldStr, "", replaceAll) — which replaced the matched text
// with NOTHING (deleting the matched line) and returned
// "Successfully edited <path>". Silent content loss, no error, 6/6. The two
// shapes below are what makes that class of defect detectable: the exact
// names, and the known misnomer the model confuses them with.
const (
	// ToolArgPath is the file path argument (read/write/edit/git_blame…).
	ToolArgPath = "path"
	// ToolArgOldStr is `edit`'s "exact string to find" argument.
	ToolArgOldStr = "oldStr"
	// ToolArgNewStr is `edit`'s "replacement string" argument. It is the ONLY
	// key a replacement may arrive under: the dispatcher must never default it.
	ToolArgNewStr = "newStr"
	// ToolArgNewTextMisspelling is a KNOWN model-side misnomer for
	// ToolArgNewStr (a back-formation from `write`'s "newText"/"content"
	// vocabulary). It is NOT an accepted alias — accepting it would put two
	// spellings of one parameter on the wire and neither would be the schema.
	// It is named so the dispatcher can REFUSE it by name, quoting what the
	// model sent, instead of silently reading an empty replacement; a refusal
	// the model can act on beats a success it cannot trust.
	ToolArgNewTextMisspelling = "newText"

	// ToolArgContent is `write`'s payload argument: the bytes to put in the
	// file. Named here because THREE readers must agree on the spelling — the
	// dispatcher that reads it, the chat boundary that judges the target, and
	// the tests that drive the dispatch with the same map the model sends — and
	// because a test that typed "content" would pass against a dispatcher that
	// had drifted to another key (Rule 1/13: a value compared or stored is
	// never a typed literal).
	ToolArgContent = "content"
)

// TaskSubmitIndependentSentinel is the ONE spelling a caller may pass as
// parent_task_id to say "this item does NOT depend on my previous submission".
// It is a SENTINEL, not a task id: no ledger row may carry it, so it must be
// consumed BEFORE the engine validates the parent.
//
// WHY IT LIVES HERE AND NOT IN internal/agent (moved 2026-11-07, task
// 01a1b448). It began as `taskSubmitIndependentSentinel` in
// internal/agent/task_submit_chain.go, unexported, and that placement made the
// value reachable from exactly ONE of its two consumers: the task_submit TOOL
// handler. The CLI face (`gopi task submit --parent-task-id independent`,
// cmd/agent/cli_tasks.go) could not see it, so the string was passed to the
// engine as a real id and the row was refused with
// `parent_task_id "independent" does not exist` — one parameter name, two
// faces, two different meanings, and the CLI page never said so.
//
// internal/toolnames is the leaf package Rule 14 asks for (constants only, no
// imports of its own), and it is the package that already owns every value
// shared across the tool/CLI boundary. internal/agent renders the sentence
// that teaches the spelling from this constant; cmd/agent reads the same
// constant to give that spelling the meaning the tool gives it. ONE
// definition, two references — no site re-types the literal (Rule 1/13).
const TaskSubmitIndependentSentinel = "independent"

// Tool names — the single source of truth (Rule 1). The value of each
// constant is the exact tool_use name string the LLM emits.
const (
	ToolBash  = "bash"
	ToolRead  = "read"
	ToolWrite = "write"
	ToolEdit  = "edit"
	// ToolTask is the merged single tool for the task-list + in-session
	// working area (tool-family merge, task family): its action parameter
	// selects view | commit | review | reframe. ToolTodo ("todo") and
	// ToolWriteTaskArea ("write_task_area") were merged into it and are
	// GONE — no consumer may reintroduce them (Rule 13: a bare literal or
	// mirrored constant of the retired names is a defect). Action wire
	// values live as ToolTaskView / ToolTaskCommit / ToolTaskReview /
	// ToolTaskReframe.
	//
	// ToolTaskEvaluate ("task_evaluate") was RETIRED by the reframe+evaluate
	// merge (SPEC-reframe-evaluate-merge.md §7.5): free-text evaluation
	// BEFORE any investigation was a weak signal (the model could only guess
	// from the summary) and the root of the bureaucracy loop — "too big" was
	// a zero-cost answer, so the queue bounced the same summary forever. Its
	// function is absorbed by action=reframe, whose verdict is emitted AFTER
	// the investigation with harness-enforced conditional payloads (roadmap /
	// reason), so every exit carries NEW work. Do NOT reintroduce the name
	// (Rule 13).
	ToolTask          = "task"
	ToolCodeSearch    = "code_search"
	ToolKeywordSearch = "keyword_search"
	// ToolMemory is the merged single tool for the knowledge base
	// (tool-family merge, memory family): its action parameter selects
	// search | store | update | feedback
	// (ToolMemoryActionSearch / ToolMemoryActionStore /
	// ToolMemoryActionUpdate / ToolMemoryActionFeedback). The former
	// separate memory tools (ToolMemorySearch "memory_search" etc.) were
	// merged into it and are GONE — no consumer may reintroduce them
	// (Rule 13: a bare literal or mirrored constant of the retired names
	// is a defect).
	ToolMemory = "memory"
	// ToolJob is the merged single tool for background-job management
	// (tool-family merge, job family): its action parameter selects
	// start | output | kill (ToolJobActionStart / ToolJobActionOutput /
	// ToolJobActionKill). The former separate job tools were merged into
	// it and are gone.
	ToolJob           = "job"
	ToolWebSearch     = "web_search"
	ToolWebFetch      = "web_fetch"
	ToolTaskSubmit    = "task_submit"
	ToolGitBlame      = "git_blame"
	ToolContextTrim   = "context_trim"
	ToolVisionAnalyze = "vision_analyze"
	ToolText2img      = "text2img"
	// ToolVoiceInput is the speech-to-text entry point (Fun-ASR-Nano via the
	// official FunASR llama.cpp runtime). It is a CLI verb only for now — no
	// model tool, because transcription happens before the model sees
	// anything: the text lands in the user's input box for review.
	ToolVoiceInput = "voice_input"
	// ToolScreenCapture is the live-X11-desktop capture tool (the "eye" of
	// the computer-use loop). Its value is the exact tool_use name the LLM
	// emits; the CLI mirror is CliVerbScreenCapture (the same string, so
	// `gopi tool screen_capture` and the tool call are ONE code path).
	ToolScreenCapture = "screen_capture"

	// ToolChatSurface is the CHAT-SCOPED tool: a chat turn's own eye and hand
	// on the surface it is answering in (read its own painted screen as a cell
	// grid, compare a line against the authoritative transcript, type its own
	// next input, stop the self-driven loop).
	//
	// It is deliberately NOT in claudeToolDefs. Rule 3 fixes that slice as the
	// SSOT of the ordinary tool surface, and its two gates
	// (TestGOPIRule3_MatchesClaudeToolDefs pins it against GOPI.md's inventory,
	// TestToolClassesCoverTheToolSurface demands a class for every entry) mean
	// an entry there is a new CORE tool by definition. This one exists only on
	// a path that IS the surface: internal/agent's chatOpsToolDefs appends it
	// for the tool-enabled chat wire and nothing else, so the task path's tool
	// list stays byte-identical (asserted by
	// TestChatSurface_ToolDefOnlyOnTheChatWire).
	//
	// Its action verbs are ChatSurfaceActionSee / Compare / Type / Stop (see
	// internal/agent/chat_surface.go) — the same vocabulary the REPL's
	// loop-control seam branches on, so one constant set governs both the wire
	// enum and the control flow.
	ToolChatSurface = "chat_surface"
)
