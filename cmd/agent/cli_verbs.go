package main

import (
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/tools"
)

// cliVerbs is the single source of truth for the TOP-LEVEL gopi CLI verb
// words (the user-facing dispatch tokens `gopi <verb> …`). Rule 1: a
// top-level command's spelling is defined exactly once here and referenced
// by every site that must agree on it:
//
//   - the isXxxSubcommand argv predicates (each compares args[0] against
//     its constant — never a bare literal),
//   - the ssh-passthrough lexical scan in isTasksSubcommand (a remote
//     "gopi task …" string is re-dispatched by re-finding the verb),
//   - the contract-suite dispatcherOfToken keys (cli_contracts_test.go),
//     which now reference the constants so a rename cannot silently drift
//     the test's token map away from the predicate.
//
// NAMESPACE BOUNDARY — three vocabularies, three homes:
//
//  1. Tool WIRE names (web_search, web_fetch, vision_analyze, text2img) are
//     the LLM tool identifiers and live in internal/toolnames; when a CLI
//     verb happens to equal a wire name the predicate references
//     toolnames.ToolXxx (cf. isWebSearchSubcommand etc.). The merged
//     toolnames fix (47ca58d) already converted those sites.
//  2. TOP-LEVEL user verbs below (task, web, memory, log, …) are a
//     SEPARATE user-facing namespace: they must not be forced onto the
//     wire-name constants (a tool rename must not rename the CLI word).
//     This block is their home.
//  3. GROUP sub-verbs (gopi task <list|submit|…>, gopi memory
//     <get|list|…>, gopi web <search|fetch>, gopi chat <list|backfill>)
//     are family-internal vocabularies that already live as named lists /
//     switches inside their family file (memorySubcommandNames,
//     taskCompletionNames, webCompletionNames, …) with their own
//     drift-guard tests — a single top-level cliVerbs constant per
//     sub-verb would scatter one family across files. They stay in the
//     family file (see the per-family note above each group switch).
//
// Renaming a top-level verb = edit the constant here; the compiler then
// points at every predicate/scan/test site that must follow (any missed
// bare literal in a predicate breaks go vet's constant-compare signal and
// the contract suite's drift guard).
const (
	// CliVerbArchive is the `gopi task archive` verb — the ONE supported way to put
	// a branch into the decided-exclusion namespace (refs/tags/archive/<branch>)
	// from a shell, with the WHY recorded. It exists because the same act done
	// with a bare `git tag` left nine branches buried with no reason and no
	// ledger row on 2026-09-16 21:04 (epic hand-archive-gate).
	CliVerbArchive = "archive"
	// CliVerbBrew is the `gopi memory brew` verb — distill reusable lessons.
	CliVerbBrew = "brew"
	// CliVerbChat is the `gopi chat` verb — interactive REPL carrier.
	CliVerbChat = "chat"
	// CliVerbCheckpoint is the `gopi session checkpoint` verb (checkpoint list/export).
	CliVerbCheckpoint = "checkpoint"
	// CliVerbCLI is the `gopi cli` group verb — the TEXT-MODE browser chat
	// carrier (a second view over the `gopi web` engine). Its second-level
	// verb and its flags live in cli_term.go with the rest of the carrier.
	CliVerbCLI = "cli"
	// CliVerbCodeSearch is the `gopi tool code-search` verb — the HYPHENATED
	// CLI name of the code_search TOOL (deliberately not toolnames.ToolCodeSearch:
	// a tool rename must not rename the command word).
	CliVerbCodeSearch = "code-search"
	// CliVerbConfig is the `gopi config` verb — the CONFIGURATION READER
	// (epic config-show). Its sub-verb `show` prints the effective
	// configuration with provenance. It exists because `gopi config show`
	// was documented (cmd/agent/merge_prompt.md §2) yet dispatched nowhere:
	// the two words reached the bare-prompt agent path, launching a whole
	// LLM session (and an `[auto] config show` ledger row) instead of
	// answering the question.
	CliVerbConfig = "config"
	// CliVerbCompletion is the `gopi config completion` verb — shell tab-completion.
	CliVerbCompletion = "completion"
	// CliVerbNotify is the `gopi notify` verb — the SUBSCRIBER's door to the
	// task-event notify stream (events / failed / sink). It exists because the
	// notify pipeline PRODUCES events while nothing consumed them: the
	// operator's sink is one 51 MB line of concatenated JSON (every event
	// delivered before e2b7e379 lacked a terminator), one record in it is
	// structurally damaged, and a consumer that restarted had no cursor. The
	// verb is a THIN face over internal/notifyfeed, which owns the framing,
	// the resynchronisation and the cursor — and it adds no resident process,
	// no timer and no script (Rule 35): --follow is a foreground poll loop the
	// caller ends, and the cursor is a file the caller names. See
	// devdoc/notify-subscription.md.
	CliVerbNotify = "notify"
	// CliVerbLog is the `gopi session log` verb — JSONL session log reader.
	CliVerbLog = "log"
	// CliVerbMemory is the `gopi memory` group verb. It is an ALIAS of the
	// tools package constant — the backup feature's ssh ship leg runs
	// `gopi memory backup --serve` on the peer, so the group word and its
	// wire contract are one decision with one definition (Rule 14). Keeping
	// a second literal here is what Rule 13 bans.
	CliVerbMemory = tools.CliVerbMemoryGroup
	// CliVerbMemsync is the RETIRED `gopi memory memsync` word. It is kept as a
	// REFUSAL row (cli_verb_aliases.go) so a script or a shell history that types
	// it hears where its one surviving local function went, instead of falling
	// through to the bare-prompt path and buying a real agent run. Its
	// node→node sibling, `gopi memory sync`, is NOT in this position: it is a
	// live verb again (restored 2026-11-06 as a manual door), dispatched from
	// cli_memory.go against tools.MemorySyncVerb — no alias table involved.
	CliVerbMemsync = "memsync"
	// NO `CliVerbMerge`, AND ITS ABSENCE IS THE DECISION (epic merge-removal,
	// item 2). The word no longer dispatches: the top-level surface was removed
	// first (help row, dispatch clause, the dispatch file), because the point of
	// this slice is the operator-visible ask — `gopi help` stops advertising a
	// merge run. The word is still RECOGNISED, but by the refusal table rather
	// than by a verb constant (cli_verb_aliases.go's verbAliasMerge): a word
	// that leaves the dispatch chain and is not refused BY NAME falls through to
	// the bare-prompt path and costs a model call for what the operator meant as
	// a command — the measured 2026-09-30 incident this table exists for.
	//
	// The merge ENGINE (merge_run.go and the merge internals) is NOT deleted
	// here and still runs: the git atoms dispatch it as `gopi task merge`
	// (cmd/agent/cli_verbs.go's family block and cli_tasks.go's atoms case).
	// What is gone is the TOP-LEVEL word. Later epic items own the engine's own
	// deletion.
	// CliVerbMerger is the `gopi merger` verb — RETIRED (2026-09-19,
	// devdoc/merger-retired.md), kept so every spelling is recognised. It is NOT
	// a ledger verb: a merger merges branches and claims nothing.
	//
	// ITS PROCESS FORM IS A CRONJOB, not a service (epic
	// daemon-machinery-removal, 2026-09-17): the host's scheduler starts it,
	// it runs ONE tick and exits — or, since epic merger-self-driving
	// (2026-09-18), `--every D` repeats that same tick in-process, in the
	// FOREGROUND, under a flock taken once and held for the loop's whole life,
	// ended by a signal. The worker still has a resident face; the two
	// therefore live side by side at the top level as peer COMPONENTS, which is
	// the property this comment records — not a shared daemon form.
	//
	// HISTORY (why this is a promotion, not a new command): the merger
	// was born as `gopi task serve` (M4.5/P2 dispatcher, 3612bfa3) and
	// renamed `gopi task merger` by the serve/worker-split (8ef21fda,
	// 2026-09-03) — while `gopi worker` was deliberately made TOP-LEVEL in
	// the same split (60ab0a3e isWorkerSubcommand). The group placement
	// survived the rename, which read as "merger is a sub-command of task"
	// even though the merger is task-INDEPENDENT (it merges what it finds in
	// the settings.json "repos" sweep list; whether a ledger row tracks the
	// branch is irrelevant). `gopi task merger` and the older
	// `gopi task serve` stay as deprecated dispatch aliases for existing
	// deployments/scripts.
	CliVerbMerger = "merger"
	// CliVerbScratchGC is the `gopi tool scratch-gc` verb — the ONE-SHOT sweep of
	// the suite scratch home, the face a host-level watchdog (systemd timer /
	// cron) executes. Its `--tick` arm is the one-shot face of the pass in
	// scratch_gc_tick.go. CORRECTED 2026-10-30 (task 01a18ef7): this used to
	// say "the resident half of the sweep rides the worker/merger tick"; no
	// resident process calls it any more, so there is no resident half.
	CliVerbScratchGC = cliverbs.ScratchGC
	// CliVerbLedgerWindow is the `gopi tool ledger-window` verb — the shell-side entry
	// point to internal/ledgerwindow: two wall-clock bounds in, the epoch seconds
	// of those bounds out. It was `cmd/ledger-window` until 2026-10-02.
	CliVerbLedgerWindow = cliverbs.LedgerWindow
	// CliVerbTokens is the `gopi tool tokens` verb — count a request's prompt
	// tokens with internal/tokenizer and put that number beside the one the
	// server reported, either for one body (--body) or for every request in a
	// raw HTTP log (--raw). It exists because a token count nobody compares
	// against the server is a number with no truth value: the operator asked
	// for exactly this comparison, and a script beside the repo cannot measure
	// the tokenizer the binary actually ships.
	CliVerbTokens = "tokens"
	// CliVerbText2imgVerify is the `gopi tool text2img-verify` verb — render the `知`
	// fixture through internal/text2img and print the font/size/pages it used,
	// writing the PNG for pixel-level (IoU) verification. It was
	// `cmd/text2img-verify` until 2026-10-02.
	CliVerbText2imgVerify = cliverbs.Text2imgVerify
	// CliVerbSandbox is the `gopi tool sandbox` verb — the ONE agent-sandbox
	// initializer's CLI face (create/show). It exists so a script, a briefing or
	// a human gets the STANDARD private profile by asking for it, instead of
	// inventing a `$SB` directory with a shape of its own and no reclaimer. See
	// internal/sandbox and GOPI.md's sandbox rule.
	CliVerbSandbox = "sandbox"
	// CliVerbProxy is the `gopi tool proxy` verb — HTTP proxy upstream.
	CliVerbProxy = "proxy"
	// CliVerbRaw is the `gopi session raw` verb — raw HTTP log reader.
	CliVerbRaw = "raw"
	// CliVerbResume is the `gopi session resume` verb — resume an interrupted session.
	CliVerbResume = cliverbs.ResumeLeaf
	// CliVerbScreen is the `gopi screen` GROUP verb — the readers of a RECORDED
	// SCREEN (`gopi screen replay <id>`). A GROUP rather than a top-level verb
	// for the playback, because the 2026-10-03 abolition
	// (devdoc/replay-abolished-verb-single-route.md) settled that a reader of a
	// recording belongs under the noun that names the artifact: `gopi session`
	// holds the family that inspects ONE session, and this one holds the reader
	// that handles a screen artifact's BYTES. The word `replay` therefore lives
	// one level down in both families and answers two different questions
	// (cli_screen.go's header states the split).
	CliVerbScreen = cliverbs.Screen
	// CliVerbScreenReplay is the `gopi screen-replay <SESSION-ID|FILE>` verb —
	// the TOP-LEVEL address of the recorded screen's byte reader
	// (cli_screen_replay_verb.go). It is the SUB-VERB's second face, not a
	// second implementation: `gopi screen replay <id>` and this verb share the
	// operand resolver, the writer, the three ending sentences and the frame
	// comparison. Named here so the completion vocabulary, the contract
	// inventory and the positional guard all read ONE constant (Rule 14).
	//
	// WHY THIS WORD AND NOT `replay`: the top-level `gopi replay` was abolished
	// (devdoc/replay-abolished-verb-single-route.md) and is pinned as banned on
	// four surfaces by session_replay_abolition_test.go — guard vocabulary,
	// census row, built binary, source identifier. `screen-replay` carries the
	// artifact's noun and what is done with it, and keeps the banned word from
	// returning as the prefix of a live command.
	CliVerbScreenReplay = cliverbs.ScreenReplayVerb
	// CliVerbShot is the `gopi tool shot` verb — screenshot a URL.
	CliVerbShot = cliverbs.Shot
	// CliVerbAnnotate is the `gopi tool annotate` verb — draw marks (rectangle,
	// point, arrow, text label) onto an EXISTING PNG, writing a NEW file. It is
	// the compositing counterpart of CliVerbCapture: capture RECORDS what the
	// screen showed, annotate POINTS AT something in a record, so the two can
	// never be one verb (a captured record that drew on itself would not be
	// evidence). The engine is internal/text2img (annotate.go), the one package
	// that owns a glyph rasteriser — no new tool (Rule 3), no new dependency.
	CliVerbAnnotate = "annotate"
	// CliVerbGif is the `gopi tool gif` verb — turn a PNG frame sequence into ONE
	// animated GIF, each frame carrying the words that explain it. It is the
	// TIME dimension of the picture surface: capture/shot/text2img/annotate all
	// produce STILLS, and a still cannot show a sequence (a state machine
	// stepping, a screen changing, a before/after). The engine is
	// internal/gifgen (stdlib image/gif), which draws captions with
	// internal/text2img's rasteriser — no new tool (Rule 3), no new dependency
	// (Rule 35: no ffmpeg/ImageMagick/gifsicle, no new module).
	CliVerbGif = "gif"
	// CliVerbCapture is the `gopi tool capture` verb — capture the LIVE X11
	// desktop: the whole screen (`--screen`), the CURRENT window (`--window`)
	// or an arbitrary rectangle (`--area WxH[+X+Y]`). It is the CLI face of
	// the screen_capture TOOL and calls the very same engine
	// (tools.ScreenCaptureWith) — one verb with a mode, because the capability
	// stopped being "a screen" once it grew the window and rectangle shapes
	// (the common OS screenshot triple).
	CliVerbCapture = "capture"
	// CliVerbScreenCapture is the `gopi tool screen_capture` verb — capture the LIVE
	// X11 desktop. Not a bare literal: the word IS toolnames.ToolScreenCapture
	// (the CLI mirror of the tool, so `gopi tool screen_capture` and the tool call
	// share ONE dispatch). Referenced here so the dispatcherOfToken contract
	// distinguishes user verbs from tool-wire mirrors. It is the WHOLE-SCREEN
	// spelling of CliVerbCapture and forwards a mode flag to it, so the tool's
	// own name stays a greppable, working command.
	CliVerbScreenCapture = "screen_capture"
	// CliVerbSetup is the `gopi config setup` verb — bring a host to a run-ready state
	// in one run (profile root, settings.json, sessions/, task ledger). It is
	// the zero-dependency replacement for a deployment shell script: every path
	// it provisions is created by gopi itself, at run time.
	CliVerbSetup = "setup"
	// CliVerbStats is the `gopi session stats` verb — session statistics.
	CliVerbStats = "stats"
	// CliVerbSession is the `gopi session` group verb — the session surfaces
	// (list/show + the log/stats/raw/resume aliases) as ONE family. It is a
	// GROUP verb like task/memory/web: the bare word prints the family page
	// and a second level names the verb.
	CliVerbSession = cliverbs.Session
	// CliVerbTask is the `gopi task` group verb.
	CliVerbTask = "task"
	// CliVerbTool is the `gopi tool` group verb — gopi's LOCAL TOOLS under one
	// noun (command-hierarchy epic, slice 1/5). It exists because the top level
	// is grouped by concept dimension or by high-frequency closed loop, and a
	// one-shot local action (shot, act, capture, proxy, annotate, gif, …) is
	// neither: each was crowding the top level with a word nobody types in a
	// loop. The group is a PURE NAME→IMPLEMENTATION MAP (cli_tool.go): every
	// word under it is still its own top-level verb, and `gopi tool <word> …`
	// forwards to that verb's OWN predicate with the same argv tail — one
	// implementation, one help page, one exit status, two spellings, nothing
	// duplicated (the operator's ruling: "无非就是把一个命令映射到一个实现").
	CliVerbTool = "tool"
	// CliVerbVersion is the `gopi version` verb. Not a bare literal: the word
	// has a SECOND reader outside this package (internal/config's
	// positional-verb guard prints the corrected line for a refused synonym,
	// which must name this command), and cmd/agent is package main — so the
	// value lives in the leaf package both can import (Rule 14), exactly like
	// CliVerbMemory below.
	CliVerbVersion = cliverbs.Version
	// CliVerbWeb is the `gopi web` group verb (search|fetch tools + server).
	CliVerbWeb = "web"

	// CliVerbWorker is the `gopi worker` verb — the task worker loop.
	CliVerbWorker = cliverbs.Worker

	// CliVerbUI is the `gopi ui` verb — the Bubble Tea TERMINAL chat surface:
	// a second VIEW over the same engine as `gopi web` (a loopback SSE client of
	// POST /api/chat), not a mode of another carrier — raw mode, the alternate
	// screen and per-frame diffs belong to no other surface.
	CliVerbUI = "ui"

	// CliVerbUIState is the `gopi ui_state` verb — READ a live `gopi ui`
	// screen state in ONE call, with no pixels and no vision model. It is the
	// reader of the self-observation dump the `gopi ui --dump-screen` sink
	// writes (internal/uiclient/dump.go): discovery, identity, ambiguity
	// refusal and the exited-target diagnosis. The word is UNDERSCORED on
	// purpose — it is the TOOL-shaped spelling of a surface the agent reaches
	// through `bash` (the screen_capture / vision_analyze convention, see
	// devdoc/cli-naming-convention.md and cli_completion_exceptions_test.go).
	CliVerbUIState = "ui_state"
)

// GROUP sub-verb constants that carry meaning OUTSIDE their family file — the
// retired-run dispatch and the source-text gates (the repo-context sandbox
// guard, the spawn guard's test) must name the SAME word (Rule 1b: never a bare
// literal at a decision site).
//
// taskVerbRun is `gopi task run` — the RETIRED runner spelling (epic
// worker-run-entry, 2026-09-16: running a row is `gopi worker`'s single job).
// It IS taskverbs.Run: this alias exists so the dispatch, the removal message
// and the gates read the word from the family leaf package instead of
// restating it (Rule 13/14: one definition, referenced; the compiler follows a
// rename). The guard that used to refuse it to an agent is gone with the
// VERB's behaviour — `gopi task run` now only prints where running moved, and
// the agent protection lives at `gopi worker` (main.go's guardVerbOrExit).
const taskVerbRun = taskverbs.Run

// taskVerbList / taskVerbSubmit are the two `gopi task` sub-verbs the ORDER
// surface test drives (epic task-list-order-surface): `list` is the verb whose
// printed order and `-h` promise must agree, `submit` is how its fixture mints
// real ledger rows. Named from the family leaf package (Rule 13/14) so a rename
// cannot leave the test probing a verb that no longer exists.
const (
	taskVerbList   = taskverbs.List
	taskVerbSubmit = taskverbs.Submit
)

// The TOP-LEVEL help vocabulary: the words a bare usage query is spelled with
// (`gopi -h`, `gopi --help`, `gopi help`, plus the env-table twin). They are
// named here because main's help scan (which prints usage and exits 0) must
// recognize exactly these spellings — a spelling printed by main but missing
// from the scan would make the usage unreadable.
// The `gopi version` spelling set: the word plus the two flags
// isVersionSubcommand answers on. They are named because TWO sites must agree
// on them — that predicate and the version handler's dispatch (Rule 13:
// reference, never re-type).
const (
	// VersionFlagLong is the long version flag (`gopi --version`).
	VersionFlagLong = "--version"
	// VersionFlagShort is the short version flag (`gopi -v`).
	VersionFlagShort = "-v"
)

const (
	// HelpFlag is the short usage-query flag.
	HelpFlag = "-h"
	// HelpFlagLong is the long usage-query flag.
	HelpFlagLong = "--help"
	// HelpWord is the bare `gopi help` word.
	HelpWord = "help"
	// HelpEnvFlagLong / HelpEnvWord are the env-table query spellings
	// (`gopi --help-env`, `gopi help-env`).
	HelpEnvFlagLong = "--help-env"
	HelpEnvWord     = "help-env"
)
