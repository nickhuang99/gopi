package main

// cli_contracts_test.go — CLI command-surface contract testsuite (regression
// canary for the 2026-09-07 "unexpected positional argument" routing bug,
// epic brief-cli-contract-testsuite).
//
// WHY THIS SUITE EXISTS: the bug's ROOT CAUSE was a subcommand refactor that
// SILENTLY changed argument routing. `gopi --settings X task submit "multi
// word summary"` shredded the quoted summary into spurious positionals
// ("unexpected positional argument \"is\"") because argv fell into the wrong
// code path — and NO test caught it. Every test here pins a subcommand's
// routing CONTRACT: given argv, the process must reach that subcommand's
// handler (right output fingerprint), exit fast (short timeout), and NEVER
// fall through to the agent loop (which would burn an LLM session on a
// protocol handshake — the M4.13 lesson).
//
// METHOD: run the REAL built binary against an ISOLATED GOPI_HOME (t.TempDir)
// with a scrub of ambient GOPI_*/ANTHROPIC_*/DEEPSEEK_* env — the same
// sandbox technique as home_settings_test.go / cli_version_test.go — and
// assert on the OUTPUT FINGERPRINT + exit code + absence of session
// artifacts. No network, no LLM, no mock: the CLI contract is about handler
// routing, not model behavior. A short exec timeout (cliContractTimeout) is
// the agent-loop detector: a correctly-routed fast handler returns in well
// under a second; a route that falls through to the agent loop hangs on the
// LLM handshake and the timeout turns the test RED — the right failure
// signal.
//
// The authoritative subcommand inventory is asserted against the dispatch
// chain in main.go (TestCLIContractInventoryMatchesDispatch), so a refactor
// that adds/removes a dispatcher cannot silently leave a command untested.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// cliContractTimeout bounds one gopi CLI probe. A correctly-routed
	// subcommand handler (usage print / parse error / fast operation) exits
	// in well under a second. A route that FALLS THROUGH to the agent loop
	// starts an LLM session and hangs far past this — the timeout is the
	// no-agent-loop assertion (M4.13: fallthrough cost = burned LLM session).
	cliContractTimeout = 15 * time.Second
)

// cliContractCase is one entry of the authoritative subcommand inventory: the
// argv whose -h must route to that subcommand's handler, and the output
// fingerprint proving it did.
type cliContractCase struct {
	argv        string   // human-readable invocation (also the subtest name)
	args        []string // argv as exec'd
	fingerprint string   // substring the output must contain (usage head)
}

// cliContractInventory is the authoritative catalog of top-level CLI surface
// entries. Each entry routes `gopi <argv> -h` to a distinct handler whose
// usage fingerprint is the assertion. The catalog MUST match the dispatch
// chain in main.go — TestCLIContractInventoryMatchesDispatch parses the
// chain and compares (drift guard, Report-A TestGOPIRule3 pattern).
var cliContractInventory = []cliContractCase{
	{argv: "-h", args: []string{"-h"}, fingerprint: "GoPi — a tool-using AI coding agent"},
	// THE ENTRY POINTS MOVED, THE PAGES DID NOT (epic verb-retirement,
	// 2026-10-05). Every case below that reads `gopi tool <cmd>` / `gopi
	// session <sub>` / `gopi config <sub>` / `gopi memory <sub>` was a bare
	// top-level verb until the retirement; its args were rewritten to the
	// group spelling the refusal now prints, and its FINGERPRINT was kept
	// unchanged — because the page is the same page reached through its ONE
	// remaining door. That is what makes this file still the live-binary
	// contract for every verb's help text: a page that changes shape, or an
	// address that stops resolving, fails here either way. The retired
	// SPELLINGS' own contract (exit 1, the address, no run) is
	// cli_verb_aliases_test.go.
	{argv: "tool code-search", args: []string{CliVerbTool, CliVerbCodeSearch, "-h"}, fingerprint: "GoPi code-search subcommand"},
	{argv: "tool keyword_search", args: []string{CliVerbTool, toolnames.ToolKeywordSearch, "-h"}, fingerprint: cliUsageTitle(keywordSearchUsage)},
	{argv: CliVerbConfig, args: []string{CliVerbConfig, "-h"}, fingerprint: "gopi config — inspect the configuration actually in force"},
	{argv: "config show", args: []string{CliVerbConfig, configShowVerb, "-h"}, fingerprint: "gopi config — inspect the configuration actually in force"},
	{argv: "config setup", args: []string{CliVerbConfig, CliVerbSetup, "-h"}, fingerprint: "gopi config setup — bring this host to a run-ready state in ONE run."},
	{argv: "config completion", args: []string{CliVerbConfig, CliVerbCompletion, "-h"}, fingerprint: "gopi config completion — generate shell tab-completion scripts"},
	{argv: CliVerbNotify, args: []string{CliVerbNotify, "-h"}, fingerprint: notifyUsageHead},
	{argv: "version", args: []string{"version", "-h"}, fingerprint: "Usage: gopi version"},
	// worker takes its fingerprint from the usage constant
	// (cliContractFingerprint) — the title line IS the contract there.
	// CliVerbMerger is NOT here: the resident merger daemon is RETIRED
	// (2026-09-19, devdoc/merger-retired.md) and its face is asserted by
	// merger_retired_face_test.go instead — a -h entry pins a live help page,
	// and a retired verb has none.
	//
	// `gopi merge` LEFT THIS INVENTORY for the same reason (epic merge-removal,
	// item 2): the word no longer dispatches, so it has no `-h` page — its
	// `mergeUsageHead` constant is still in the tree and still used by the
	// retired-suite survivors, but `gopi merge -h` now reaches the ALL-HOMELESS
	// refusal (cli_verb_aliases.go's verbAliasMerge row), not a page. An entry
	// here would pin a fingerprint nothing prints.
	{argv: CliVerbWorker, args: []string{CliVerbWorker, "-h"}},
	{argv: "task archive", args: []string{CliVerbTask, taskverbs.Archive, "-h"}, fingerprint: cliUsageTitle(archiveUsage())},
	{argv: "memory", args: []string{"memory", "-h"}, fingerprint: "GoPi memory — the memory tool family as one subcommand group."},
	{argv: "memory search", args: []string{"memory", "search", "-h"}, fingerprint: "GoPi memory — the memory tool family"},
	{argv: "memory backup", args: []string{"memory", tools.LedgerBackupVerb, "-h"}, fingerprint: "GoPi memory backup — snapshot the authoritative ledger and ship it to a peer."},
	{argv: "memory brew", args: []string{CliVerbMemory, CliVerbBrew, "-h"}, fingerprint: "GoPi brew — distill reusable lessons"},
	{argv: "web_search", args: []string{"web_search", "-h"}, fingerprint: cliUsageTitle(webSearchUsage)},
	{argv: "web_fetch", args: []string{"web_fetch", "-h"}, fingerprint: cliUsageTitle(webFetchUsage)},
	{argv: "tool vision_analyze", args: []string{CliVerbTool, toolnames.ToolVisionAnalyze, "-h"}, fingerprint: "GoPi vision_analyze subcommand"},
	{argv: "tool text2img", args: []string{CliVerbTool, toolnames.ToolText2img, "-h"}, fingerprint: cliUsageTitle(text2imgUsage)},
	{argv: "tool text2img-verify", args: []string{CliVerbTool, CliVerbText2imgVerify, "-h"}, fingerprint: cliUsageTitle(text2imgVerifyUsage)},
	{argv: "tool screen_capture", args: []string{CliVerbTool, toolnames.ToolScreenCapture, "-h"}, fingerprint: cliUsageTitle(screenCaptureUsage)},
	// capture is the mode-aware face of the same engine: the whole-screen
	// spelling (--screen), the current window (--window) and a rectangle
	// (--area WxH[+X+Y]). Its -h fingerprint is pinned here so a rename of the
	// verb, the flag set or the usage text fails the live-binary smoke.
	{argv: "tool capture", args: []string{CliVerbTool, CliVerbCapture, "-h"}, fingerprint: cliUsageTitle(captureUsage)},
	{argv: "tool act", args: []string{CliVerbTool, CliVerbAct, "-h"}, fingerprint: cliUsageTitle(actUsage)},
	{argv: "tool annotate", args: []string{CliVerbTool, CliVerbAnnotate, "-h"}, fingerprint: cliUsageTitle(annotateUsage)},
	{argv: "tool gif", args: []string{CliVerbTool, CliVerbGif, "-h"}, fingerprint: cliUsageTitle(gifUsage)},
	{argv: "tool shot", args: []string{CliVerbTool, CliVerbShot, "-h"}, fingerprint: cliUsageTitle(shotUsageText())},
	{argv: "tool proxy", args: []string{CliVerbTool, CliVerbProxy, "-h"}, fingerprint: cliUsageTitle(proxyUsage)},
	{argv: "tool voice_input", args: []string{CliVerbTool, toolnames.ToolVoiceInput, "-h"}, fingerprint: cliUsageTitle(voiceInputUsage)},
	{argv: "tool scratch-gc", args: []string{CliVerbTool, CliVerbScratchGC, "-h"}, fingerprint: scratchGCUsageFingerprint},
	{argv: "tool ledger-window", args: []string{CliVerbTool, CliVerbLedgerWindow, "-h"}, fingerprint: ledgerWindowUsage},
	{argv: "tool sandbox", args: []string{CliVerbTool, CliVerbSandbox, "-h"}, fingerprint: sandboxUsageFingerprint},
	// The two verbs that were separate binaries until 2026-10-02. Each keeps
	// its own -h page (both print a usage line and exit 0, which is the
	// contract this case pins). `replay` was the third and is gone: it is
	// `gopi chat --replay` now, a flag on the chat verb, and a verb that no
	// longer exists has no page to pin.
	{argv: CliVerbSession, args: []string{CliVerbSession, "-h"}, fingerprint: "GoPi session subcommands — inspect and continue sessions"},
	// The family's second level: `session list -h` is the family page (the arm
	// prints it), while `session log -h` must be the LOG verb's own page — the
	// family page promises exactly that ("the flags are that verb's own").
	// Found by running it: the first build answered every `session <verb> -h`
	// with the family page, because -h was intercepted before the sub-verb.
	{argv: "session list", args: []string{CliVerbSession, sessionSubcmdList, "-h"}, fingerprint: "GoPi session subcommands — inspect and continue sessions"},
	{argv: "session log", args: []string{CliVerbSession, CliVerbLog, "-h"}, fingerprint: "gopi session log — GoPi JSONL session log reader"},
	{argv: "session stats", args: []string{CliVerbSession, CliVerbStats, "-h"}, fingerprint: "gopi session stats — GoPi session statistics"},
	{argv: "session raw", args: []string{CliVerbSession, CliVerbRaw, "-h"}, fingerprint: "gopi session raw — GoPi raw HTTP log"},
	{argv: "session resume", args: []string{CliVerbSession, CliVerbResume, "-h"}, fingerprint: "Usage: gopi session resume"},
	// The PLAYBACK's own page, reached through the family's second level: it is
	// the page that teaches the transport keys and the material model, so it is
	// pinned here rather than only in the sub-verb's own test. This row is the
	// POSITIVE half of the abolition gate
	// (session_replay_abolition_test.go): the value came back through the
	// family's door, not through a revived top-level verb.
	{argv: "session replay", args: []string{CliVerbSession, sessionSubcmdReplay, "-h"}, fingerprint: sessionReplayUsageHead},
	{argv: "session checkpoint", args: []string{CliVerbSession, CliVerbCheckpoint, "-h"}, fingerprint: "GoPi checkpoint & resume subcommands"},
	// The screen GROUP and its playback reader (cli_screen.go /
	// cli_screen_replay.go) — the SECOND question about a recording. Two rows
	// because they are two pages: the group's index, and the sub-verb's own. The
	// sub-verb row is what keeps the split honest — `session replay` RENDERS,
	// `screen replay` WRITES BYTES — so a rewrite that dropped either head fails
	// the live-binary smoke.
	{argv: CliVerbScreen, args: []string{CliVerbScreen, "-h"}, fingerprint: "GoPi screen subcommands"},
	{argv: "screen replay", args: []string{CliVerbScreen, screenSubcmdReplay, "-h"}, fingerprint: screenReplayUsageHead},
	// The TOP-LEVEL address of the same reader (cli_screen_replay_verb.go). Two
	// pages for one implementation on purpose: the group's sub-verb and this
	// verb differ in OPERAND SHAPE (`--verify A B` here, `--verify OTHER`
	// against an implicit session there) and in exit status on a divergence, so
	// a rewrite that dropped either head must fail the live-binary smoke.
	{argv: CliVerbScreenReplay, args: []string{CliVerbScreenReplay, "-h"}, fingerprint: screenReplayVerbUsageHead},
	// The ROUND-LEVEL note index's reader: a second-level sub-verb with its own
	// page and its own implementation (cli_session_notes.go). Pinned here so a
	// rename of the word or a page rewrite that drops its head fails the
	// live-binary smoke — the same contract every sibling above carries.
	{argv: "session notes", args: []string{CliVerbSession, sessionSubcmdNotes, "-h"}, fingerprint: sessionNotesUsageFingerprint},
	// The context EXPORT's own page: a second-level sub-verb whose page documents
	// the artifact (what goes in the FILE) and the one line that goes on the
	// screen — two facts a rewrite could silently drop while the command still
	// runs. The fingerprint is the usage constant's own first line, so a rename of
	// the word or a page rewrite fails the live-binary smoke.
	{argv: "session context", args: []string{CliVerbSession, sessionSubcmdContext, "-h"}, fingerprint: sessionContextUsageFingerprint},
	// The playback: a second-level sub-verb of the session group, so its own -h
	// page (not the family page) is the contract. The fingerprint is the usage
	// constant itself, so a page rewrite that drops the head fails here.
	//
	// This row is ALSO the POSITIVE half of the abolition gate
	// (session_replay_abolition_test.go): the value came back through the
	// family's door, not through a revived top-level verb.
	{argv: "session replay", args: []string{CliVerbSession, sessionSubcmdReplay, "-h"}, fingerprint: sessionReplayUsageHead},
	// The tool group verb (cli_tool.go): its page is the direct/grouped index
	// of gopi's local tools. Pinned here so a rename of the group word or the
	// page's first line fails the live-binary smoke.
	{argv: CliVerbTool, args: []string{CliVerbTool, "-h"}, fingerprint: toolUsageTitle},
	{argv: "task", args: []string{"task", "-h"}, fingerprint: "GoPi task subcommands"},
	// The pre-split spelling of the merger: dispatch is KEPT (scripts
	// and running deployments must not break) but its -h is ONE pointer line
	// to the canonical verb, never the family page (epic cli-help-dedup).
	{argv: "task serve", args: []string{"task", taskverbs.Serve, "-h"}, fingerprint: taskServeAliasPointerFingerprint()},
	{argv: "task dedup-scan", args: []string{"task", taskverbs.DedupScan, "-h"}, fingerprint: "gopi task dedup-scan [--live] [--db PATH]"},
	{argv: "chat", args: []string{"chat", "-h"}, fingerprint: "Usage: gopi chat"},
	{argv: "web", args: []string{"web", "-h"}, fingerprint: "gopi web — serve a local browser chat UI"},
	// `gopi ui` (the Bubble Tea terminal chat) and `gopi cli` (the text-mode
	// browser carrier) are dispatched by isUISubcommand and isTermSubcommand.
	// They were MISSING from this inventory: the drift guard compares this list
	// against main.go's chain, so their absence surfaced first as a count
	// mismatch (main.go 20, this list 18) and then as the two predicates with no
	// case. Each pins its own live -h page, which is this list's contract —
	// fingerprints DERIVED from the page's own usage constant (Rule 13), never a
	// copy of the title line.
	{argv: CliVerbUI, args: []string{CliVerbUI, "-h"}, fingerprint: uiUsageFingerprint},
	{argv: CliVerbCLI, args: []string{CliVerbCLI, "-h"}, fingerprint: cliUsageTitle(termUsage)},
	{argv: CliVerbUIState, args: []string{CliVerbUIState, "-h"}, fingerprint: uiStateUsageTitleLine},
	{argv: "web search", args: []string{"web", "search", "-h"}, fingerprint: cliUsageTitle(webSearchUsage)},
	{argv: "web fetch", args: []string{"web", "fetch", "-h"}, fingerprint: cliUsageTitle(webFetchUsage)},
}

// cliContractFingerprint returns the substring this contract asserts for one
// inventory entry. For the two SERVICE verbs (worker, merger) the fingerprint
// is DERIVED from the live usage constant rather than mirrored here: their
// pages were rewritten wholesale by the epic "worker help is misleading", and a
// copy of the old title line kept this catalog asserting a page that no longer
// exists (Rule 13 — a mirrored literal drifts from its single source of truth).
// The assertion still has teeth: the derived token is the FIRST line of the
// constant, which `gopi <verb> -h` must print through the real binary.
func cliContractFingerprint(argv string) string {
	switch argv {
	case CliVerbWorker:
		return cliUsageTitle(workerUsageHead)
	}
	for _, tc := range cliContractInventory {
		if tc.argv == argv {
			return tc.fingerprint
		}
	}
	return ""
}

// cliUsageTitle is the first line of a usage constant — the capability
// one-liner a user reads before anything else.
func cliUsageTitle(usage string) string {
	head, _, _ := strings.Cut(strings.TrimSpace(usage), "\n")
	return head
}

// dispatcherOfToken maps each inventory first-token to the isXxx predicate
// that dispatches it. Used by the drift guard to prove every dispatcher in
// the main.go chain has a contract case. The top-level user verbs reference
// the cliVerbs constants (cli_verbs.go — Rule 1 SSOT) so a verb rename
// cannot silently drift this token map away from the predicates that
// compare against the same constants. Tool-wire names (web_search,
// web_fetch, vision_analyze, text2img) reference the toolnames constants,
// which the predicates compare against too.
var dispatcherOfToken = map[string]string{
	CliVerbCompletion:           "CompletionSubcommand",
	CliVerbConfig:               "ConfigSubcommand",
	CliVerbNotify:               "NotifySubcommand",
	CliVerbVersion:              "VersionSubcommand",
	CliVerbWorker:               "WorkerSubcommand",
	CliVerbMerger:               "MergerSubcommand",
	CliVerbArchive:              "ArchiveSubcommand",
	CliVerbMemory:               "MemorySubcommand",
	toolnames.ToolWebSearch:     "WebSearchSubcommand",
	toolnames.ToolWebFetch:      "WebFetchSubcommand",
	toolnames.ToolVisionAnalyze: "VisionSubcommand",
	toolnames.ToolText2img:      "Text2imgSubcommand",
	toolnames.ToolScreenCapture: "ScreenCaptureSubcommand",
	CliVerbCapture:              "CaptureSubcommand",
	CliVerbAnnotate:             "AnnotateSubcommand",
	CliVerbGif:                  "GifSubcommand",
	CliVerbCodeSearch:           "CodeSearchSubcommand",
	toolnames.ToolKeywordSearch: "KeywordSearchSubcommand",
	CliVerbLog:                  "LogSubcommand",
	CliVerbStats:                "StatsSubcommand",
	CliVerbRaw:                  "RawSubcommand",
	CliVerbResume:               "ResumeSubcommand",
	CliVerbCheckpoint:           "CheckpointSubcommand",
	CliVerbProxy:                "ProxySubcommand",
	CliVerbTask:                 "TasksSubcommand",
	CliVerbTool:                 "ToolSubcommand",
	CliVerbSession:              "SessionSubcommand",
	CliVerbScreen:               "ScreenSubcommand",
	CliVerbScreenReplay:         "ScreenReplaySubcommand",
	CliVerbChat:                 "ChatSubcommand",
	CliVerbCLI:                  "TermSubcommand",
	CliVerbUI:                   "UISubcommand",
	CliVerbUIState:              "UIStateSubcommand",
	CliVerbWeb:                  "WebSubcommand",
	CliVerbBrew:                 "BrewSubcommand",
	CliVerbShot:                 "ShotSubcommand",
	CliVerbAct:                  "ActSubcommand",
	CliVerbScratchGC:            "ScratchGCSubcommand",
	CliVerbSetup:                "SetupSubcommand",
	CliVerbSandbox:              "SandboxSubcommand",
	CliVerbLedgerWindow:         "LedgerWindowSubcommand",
	CliVerbText2imgVerify:       "Text2imgVerifySubcommand",
	toolnames.ToolVoiceInput:    "VoiceSubcommand",
}

// cliContractDispatchPredicates lists the isXxx predicates wired into the
// main.go dispatch chain, in order. The drift guard parses THIS constant (a
// hand-maintained mirror kept in lockstep by the same edit that touches
// main.go) and asserts: (a) every predicate has a contract case, (b) every
// case's token maps to a predicate that exists. Add a dispatcher to main.go →
// add it here AND give it a cliContractInventory entry.
//
// IT SHRANK WITH THE CHAIN (epic verb-retirement, 2026-10-05). The ~30
// predicates removed here did not lose their implementations or their help
// pages — only their top-level dispatch entry. Their words are refused by name
// now (cli_verb_aliases.go), which is why a retired verb is NOT listed below
// and NOT given a cliContractInventory case: the inventory's job is to pin a
// LIVE help page, and a retired word has none (the same reasoning the
// UndispatchedToolWordSubcommand and MergerSubcommand arms state). The
// refusal-side contract lives in cli_verb_aliases_test.go, which drives every
// retired word on the real binary.
var cliContractDispatchPredicates = []string{
	"ConfigSubcommand", "NotifySubcommand", "VersionSubcommand", "WorkerSubcommand", "MergerSubcommand",
	// "MergeSubcommand" was here and left with the dispatch itself (epic
	// merge-removal, item 2). This list is compared against the predicates
	// main.go's chain actually calls, so keeping the name would assert a
	// dispatcher this binary does not have.
	// "MemorySyncSubcommand" was here and left with its dispatch (the cross-node
	// sync removal, 2026-11-05): main.go no longer calls the predicate, and this
	// list is compared against what the chain actually calls.
	"MemorySubcommand", "MemoryBackupSubcommand",
	"WebSearchSubcommand", "WebFetchSubcommand",
	"TasksSubcommand", "ChatSubcommand", "WebSubcommand",
	"UISubcommand", "TermSubcommand",
	"UIStateSubcommand",
	"SessionSubcommand",
	"ScreenSubcommand",
	"ScreenReplaySubcommand",
	"ToolSubcommand",
	"UndispatchedToolWordSubcommand",
}

// dispatchPredicatesInMain reads the ACTUAL dispatch line from main.go and
// returns its predicate names — the true source of truth the constant above
// must match.
//
// The MARKER is the chain's FIRST predicate, and it moved once: it was
// `isCompletionSubcommand` until the verb-retirement epic (2026-10-05) removed
// that verb's dispatch entry, at which point this helper (and the census
// generator, which keys on the same literal) could not find the chain at all
// and failed loudly. Whoever edits the chain's first arm updates this marker —
// and that failure is the reminder.
func dispatchPredicatesInMain(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(raw)
	marker := "if isConfigSubcommand(dispatchArgs)"
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("main.go dispatch chain line not found (refactored?) — update cliContractDispatchPredicates/cliContractDispatchLine")
	}
	brace := strings.Index(src[start:], "{")
	if brace < 0 {
		t.Fatalf("main.go dispatch chain unterminated")
	}
	line := src[start : start+brace]
	line = strings.TrimSpace(strings.TrimPrefix(line, "if "))
	var preds []string
	for _, tok := range strings.Split(line, "||") {
		p := strings.TrimSpace(tok)
		if strings.HasPrefix(p, "is") && strings.HasSuffix(p, "(dispatchArgs)") {
			preds = append(preds, strings.TrimSuffix(strings.TrimPrefix(p, "is"), "(dispatchArgs)"))
		}
	}
	if len(preds) < 10 {
		t.Fatalf("main.go dispatch parse yielded only %d predicates — line changed shape?", len(preds))
	}
	return preds
}

// TestCLIUserVerbTokensAreAllFromCliVerbs pins the dispatcherOfToken map's
// USER-verb keys to the cliVerbs SSOT block (cli_verbs.go): every top-level
// user verb in the contract map must BE a cliVerbs constant, and every
// cliVerbs constant must appear in the contract map. The map itself
// references the constants (a rename cannot silently drift it) — this guard
// closes the reverse direction: deleting a constant, or adding a verb whose
// contract case was forgotten, fails here. Tool-wire tokens (web_search,
// web_fetch, vision_analyze, text2img) live in toolnames and are exempt —
// they are covered by TestCLIToolMirrorVerbsMatchConstants in
// cli_toolnames_contract_test.go. topLevelCommandNames is NOT used as the
// source: it is the completion-advertised SUBSET (deliberately omits
// completion/raw/stats per cli_completion_test.go) while cliVerbs is the
// full dispatch-verb SSOT.
func TestCLIUserVerbTokensAreAllFromCliVerbs(t *testing.T) {
	// Every top-level user verb the dispatch recognizes, enumerated from the
	// cliVerbs constants (referencing the constants keeps a rename a compile
	// error here — never a silent drift).
	allVerbs := []string{
		CliVerbBrew, CliVerbChat, CliVerbCheckpoint, CliVerbCodeSearch, CliVerbConfig,
		CliVerbNotify,
		CliVerbCompletion, CliVerbLog, CliVerbMemory,
		CliVerbCLI,
		CliVerbUI,
		CliVerbMerger, CliVerbProxy, CliVerbRaw, CliVerbResume, CliVerbShot, CliVerbStats,
		CliVerbTask, CliVerbVersion, CliVerbWeb, CliVerbWorker,
		CliVerbAct, CliVerbScratchGC, CliVerbArchive, CliVerbSetup,
		CliVerbAnnotate,
		CliVerbGif,
		CliVerbUIState,
		CliVerbCapture,
		CliVerbSandbox,
		// The two commands that were separate programs under cmd/ until
		// 2026-10-02 (two verb files): dispatched user verbs like any other, so
		// they are cliVerbs members and this guard says so. (`replay` was the
		// third; it was abolished — it is `gopi chat --replay` now, a flag.)
		CliVerbLedgerWindow,
		CliVerbText2imgVerify,
		// The session FAMILY verb (list/show + the four thin aliases over
		// log/stats/raw/resume): dispatched by isSessionSubcommand like any
		// other user verb, so it is a cliVerbs member and this guard says so.
		CliVerbSession,
		// The screen FAMILY verb (cli_screen.go): dispatched by
		// isScreenSubcommand like any other user verb, so it is a cliVerbs
		// member and this guard says so.
		CliVerbScreen,
		// The TOP-LEVEL address of the same reader (cli_screen_replay_verb.go):
		// dispatched by isScreenReplaySubcommand, a cliVerbs member like every
		// other verb.
		CliVerbScreenReplay,
		// The tool GROUP verb: dispatched by isToolSubcommand and advertised in
		// the top-level index, so it is a cliVerbs member like every other verb.
		CliVerbTool,
	}
	verbSet := map[string]bool{}
	for _, v := range allVerbs {
		verbSet[v] = true
	}
	// Every dispatcherOfToken key whose predicate is a pure user verb must be
	// a cliVerbs member (tool-wire keys are toolnames constants — skip them).
	for token, d := range dispatcherOfToken {
		switch d {
		case "WebSearchSubcommand", "WebFetchSubcommand", "VisionSubcommand",
			"Text2imgSubcommand", "ScreenCaptureSubcommand", "VoiceSubcommand",
			"KeywordSearchSubcommand":
			continue // tool-wire surface — governed by the toolnames contract
		}
		if !verbSet[token] {
			t.Fatalf("dispatcherOfToken key %q (→ %s) is not a cliVerbs constant — add the verb to cli_verbs.go", token, d)
		}
	}
	// Reverse: every cliVerbs verb must have a dispatcherOfToken entry —
	// a constant no contract case routes is either stale or missing coverage.
	for _, v := range allVerbs {
		if _, ok := dispatcherOfToken[v]; !ok {
			t.Fatalf("cliVerbs %q has no dispatcherOfToken entry — stale constant or missing contract case", v)
		}
	}
}

// TestCLIContractInventoryMatchesDispatch is the drift guard. The contract
// suite is only as good as its coverage of the real dispatch surface; this
// test closes the loop both ways:
//   - every isXxx predicate in main.go's chain maps to a contract case
//     (a new subcommand without a test fails here);
//   - every dispatch predicate in the shared constant still exists in
//     main.go (a removed/renamed subcommand with a stale case fails here).
func TestCLIContractInventoryMatchesDispatch(t *testing.T) {
	inMain := dispatchPredicatesInMain(t)
	if len(inMain) != len(cliContractDispatchPredicates) {
		t.Fatalf("dispatch predicate count mismatch: main.go has %d (%v), contract suite tracks %d — add/remove the case and update cliContractDispatchPredicates",
			len(inMain), inMain, len(cliContractDispatchPredicates))
	}
	for i := range inMain {
		if inMain[i] != cliContractDispatchPredicates[i] {
			t.Fatalf("dispatch predicate order/name drift at %d: main.go=%q contract=%q — update cliContractDispatchPredicates to mirror main.go", i, inMain[i], cliContractDispatchPredicates[i])
		}
	}

	// Every predicate must be covered by a contract case's first token.
	covered := map[string]bool{}
	for _, tc := range cliContractInventory {
		if tc.argv == "-h" {
			continue
		}
		if d, ok := dispatcherOfToken[tc.args[0]]; ok {
			covered[d] = true
		}
	}
	var missing []string
	for _, p := range cliContractDispatchPredicates {
		if p == "UndispatchedToolWordSubcommand" {
			// `gopi code_search` (and the dash twins) has NO help page to pin:
			// the predicate REFUSES the word by name and exits 1, so a usage
			// fingerprint would assert a page that deliberately does not exist.
			// Its contract case is cli_undispatched_tool_words_test.go, which
			// drives every word on the real binary and asserts the refusal plus
			// the no-run proof (the same shape as MergerSubcommand's retired
			// face, one file below).
			if covered["UndispatchedToolWordSubcommand"] {
				t.Fatalf("an inventory entry maps to UndispatchedToolWordSubcommand — the refusal has no -h page to fingerprint")
			}
			continue
		}
		if p == "MergerSubcommand" {
			// `gopi merger` has NO live help page to pin: the resident merger
			// daemon is RETIRED (2026-09-19, devdoc/merger-retired.md) and the
			// verb refuses with exit 1, so an inventory entry asserting a usage
			// fingerprint would assert a page that deliberately does not exist.
			// Its coverage lives in merger_retired_face_test.go, which drives
			// every spelling of this predicate on the real binary and asserts
			// the refusal (exit != 0, the recorded cause, nothing written) —
			// that file is the contract case, this map is not.
			if !covered["MergerSubcommand"] {
				continue
			}
			missing = append(missing, p)
			continue
		}
		if p == "MemorySyncSubcommand" {
			// `memory sync` is dispatched by isMemorySyncSubcommand but its
			// first token is "memory" (shared with the group). The "memory
			// sync" contract case covers it.
			if !covered["MemorySubcommand"] {
				missing = append(missing, p)
			}
			continue
		}
		if p == "MemoryBackupSubcommand" {
			// Same shared-group shape as MemorySyncSubcommand: `memory
			// backup` is dispatched by isMemoryBackupSubcommand, whose first
			// token is the group word "memory". The "memory backup" contract
			// case covers it.
			if !covered["MemorySubcommand"] {
				missing = append(missing, p)
			}
			continue
		}
		if !covered[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("dispatch predicates without a contract case: %v — add a cliContractInventory entry", missing)
	}

	// Reverse: every inventory token must map to a predicate that the main.go
	// chain actually wires (stale cases for removed subcommands fail).
	for _, tc := range cliContractInventory {
		if tc.argv == "-h" {
			continue
		}
		d, ok := dispatcherOfToken[tc.args[0]]
		if !ok {
			continue // group sub-verbs (memory search, web fetch …) share the group dispatcher
		}
		found := false
		for _, p := range inMain {
			if p == d {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("contract case %q maps to dispatcher %s which main.go no longer wires — subcommand removed/renamed?", tc.argv, d)
		}
	}
}

// TestCLIContractEachSubcommandRoutesToItsHandler is the per-subcommand
// contract test. For every inventory entry it runs the real binary with an
// isolated GOPI_HOME and a short timeout, asserting:
//  1. exit 0 (a -h usage query is a success — the zero-side-effect contract),
//  2. output carries that subcommand's usage fingerprint (its -h handler ran
//     — not the top-level -h, not a fallthrough),
//  3. no "unexpected positional" / panic (the shredding bug's signature),
//  4. the sandbox GOPI_HOME gained NO session artifacts (the strongest
//     no-agent-loop signature: a fallthrough opens a session JSONL).
func TestCLIContractEachSubcommandRoutesToItsHandler(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range cliContractInventory {
		t.Run(tc.argv, func(t *testing.T) {
			out, home := runCLIContractProbe(t, bin, tc.args...)
			fingerprint := cliContractFingerprint(tc.argv)
			if fingerprint == "" {
				// Rule 15: an absent fingerprint would make the assertion
				// below vacuous (strings.Contains out "" is always true).
				t.Fatalf("gopi %s: no fingerprint resolve for this entry — add one (literal or derived from the usage constant)", tc.argv)
			}
			if !strings.Contains(out, fingerprint) {
				t.Fatalf("gopi %s: output missing fingerprint %q\n--- output ---\n%s", tc.argv, fingerprint, out)
			}
			if strings.Contains(out, "unexpected positional") || strings.Contains(out, "panic:") {
				t.Fatalf("gopi %s: routing-bug signature in output:\n%s", tc.argv, out)
			}
			assertNoCLIContractSession(t, tc.argv, home)
		})
	}
}

// runCLIContractProbe runs one gopi probe with a scrubbed env and an isolated
// GOPI_HOME under a hard cliContractTimeout; returns combined output and the
// sandbox home. A timeout (agent-loop fallthrough) fails the test.
func runCLIContractProbe(t *testing.T, bin string, args ...string) (string, string) {
	t.Helper()
	home := t.TempDir()
	out, err := runCLIContractProbeEnv(t, bin, home, args...)
	if err != nil {
		t.Fatalf("gopi %s: exit error: %v\n--- output ---\n%s", strings.Join(args, " "), err, out)
	}
	return out, home
}

// runCLIContractProbeAllowExit is the exit-tolerant variant for negative
// cases: returns (out, err) without failing on non-zero exit — the caller
// asserts on the error. Still enforces the no-agent-loop timeout.
func runCLIContractProbeAllowExit(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	return runCLIContractProbeEnv(t, bin, t.TempDir(), args...)
}

// runCLIContractProbeEnv runs the binary with cliContractEnv(home) and kills
// it past cliContractTimeout (the no-agent-loop detector).
// runCLIContractProbeEnv drives the binary through its FAMILY door: argv[0] is
// translated by familyAddressArgs, every other word is passed through untouched.
// Callers name the command they MEAN, and the spelling that dispatches it may
// have moved under a group (verb-retirement phase 1). Only the door moves — the
// assertions are exactly as they were.
func runCLIContractProbeEnv(t *testing.T, bin, home string, args ...string) (string, error) {
	t.Helper()
	return runCLIContractProbeRaw(t, bin, home, familyAddressArgs(args)...)
}

// runCLIContractProbeRaw is the same probe with argv EXACTLY as given — what a
// REFUSAL test drives, because there the spelling that must NOT dispatch is the
// subject (`gopi keyword_search …` must exit non-zero and name `gopi tool
// keyword_search`). Mapping it would answer a different question.
func runCLIContractProbeRaw(t *testing.T, bin, home string, args ...string) (string, error) {
	t.Helper()
	out, err, timedOut := cliContractProbeWithTimeout(bin, home, cliContractTimeout, args...)
	if timedOut {
		t.Fatalf("gopi %s: TIMEOUT after %v — argv fell through to the agent loop (no-agent-loop contract violated)", strings.Join(args, " "), cliContractTimeout)
	}
	return out, err
}

// cliContractProbeWithTimeout is the ONE probe implementation behind both
// bounds: the shared no-agent-loop detector (whose expiry IS the failure) and
// a caller-supplied limit for probes that legitimately expect the RUN path and
// need the bound only as a hang guard (cli_verb_guard_test.go).
func cliContractProbeWithTimeout(bin, home string, limit time.Duration, args ...string) (string, error, bool) {
	return cliContractProbeWithEnv(bin, home, limit, nil, args...)
}

// cliContractProbeWithEnv is the same probe with caller-supplied environment
// entries layered on TOP of the isolated base environment — the shape a
// regression pair needs, where the argv is identical and one ambient variable
// is the only difference (cli_verb_guard_test.go's probe-prompt pair). An
// entry whose value is "" is passed through as an empty value, i.e. the
// variable EXISTS: the distinction "set but empty" vs "absent" is exactly the
// kind of channel state such a pair must be able to express.
func cliContractProbeWithEnv(bin, home string, limit time.Duration, extra []string, args ...string) (string, error, bool) {
	// argv[0] is NOT mapped here, deliberately: this is the probe the refusal
	// tests drive (`gopi <retired word> -h` must exit non-zero and name its
	// family address), so rewriting the door would answer a different question.
	// A test that wants the family address asks for it with
	// familyAddressArgs(args) at its own call site.
	cmd := exec.Command(bin, args...)
	cmd.Env = append(cliContractEnv(home), extra...)
	type probeResult struct {
		out []byte
		err error
	}
	done := make(chan probeResult, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- probeResult{out, err}
	}()
	select {
	case r := <-done:
		return string(r.out), r.err, false
	case <-time.After(limit):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		r := <-done
		return string(r.out), r.err, true
	}
}

// cliContractMasksAmbientDisplay reports whether a probe's environment should
// drop the ambient DISPLAY.
//
// IT EXISTS BECAUSE AN INHERITED DISPLAY MAKES A PROBE HOST-DEPENDENT, and the
// measurement is why: this host's DISPLAY is a FORWARDED one (`localhost:15.0`,
// an ssh -X leg) whose TCP connect is accepted by the forwarder and then answers
// NOTHING — the client blocks until the X11 layer's own io bound (20s), so a
// screen_capture/act probe that inherits it dies against its 15s contract
// budget with no output at all, i.e. EXACTLY the signature of an argv that fell
// through to the agent loop. Measured on this host: `DISPLAY=localhost:15.0
// gopi tool screen_capture --out X` = 28-52s (no output when killed), while the same
// argv with DISPLAY unset or empty refuses in 0.01s with `no X11 display`.
//
// So a probe that does not NEED a display gets a DETERMINISTIC one: unset means
// "no display", which is a state every X11 guard in this binary answers
// immediately and by name. A probe that DOES need one opts in through its case's
// own env (g6DisplayEnv), which is applied AFTER this and therefore wins — the
// case owns the decision, the default is only what an unopinionated case gets.
const cliContractMasksAmbientDisplay = true

// cliContractDisplayEnvName is the variable masked above.
const cliContractDisplayEnvName = "DISPLAY"

// cliContractEnv builds the probe environment: os.Environ minus every ambient
// GOPI_*/ANTHROPIC_*/DEEPSEEK_* var (a dev shell — or a gopi task-run child
// — exports GOPI_SETTINGS / ANTHROPIC_MODEL / GOPI_TASK_WRAPPER_ID etc.,
// which would redirect config resolution or label the process), minus the
// ambient DISPLAY (see cliContractMasksAmbientDisplay), plus the isolated
// GOPI_HOME. GOPI_HOME redirects ALL data writes away from the real profile.
func cliContractEnv(home string) []string {
	drop := func(k string) bool {
		if cliContractMasksAmbientDisplay && k == cliContractDisplayEnvName {
			return true
		}
		return strings.HasPrefix(k, "GOPI_") ||
			strings.HasPrefix(k, "ANTHROPIC_") ||
			strings.HasPrefix(k, "DEEPSEEK_")
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop(kv[:i]) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GOPI_HOME="+home, "GOPI_BREW_INTERVAL_SEC=0")
}

// assertNoCLIContractSession fails when the probe's sandbox GOPI_HOME gained a
// session artifact — the signature of an agent-loop fallthrough (a correctly
// routed CLI handler never opens a session). Same shape as
// cli_version_test.go's assertNoSessionArtifact, generalized to the whole
// subcommand surface.
func assertNoCLIContractSession(t *testing.T, argv, home string) {
	t.Helper()
	dir := filepath.Join(home, "sessions")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return // no session dir at all — the good case
	}
	if err != nil {
		t.Fatalf("gopi %s: read sandbox sessions dir: %v", argv, err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("gopi %s: created session artifact(s) %v — argv leaked into the session store (agent-loop fallthrough)", argv, names)
	}
}

// --- Repro-case pin: the 2026-09-07 routing bug ---------------------------

// TestCLIContractSettingsConfigFlagRoutesToTaskHandler pins the exact repro
// of the "unexpected positional argument" bug: `gopi --settings X task submit
// "multi word summary" --payload ...`. Pre-fix (isTasksSubcommand ssh
// branch) the --settings flag pair was misread as an ssh host and the quoted
// summary was word-split into positionals → `unexpected positional argument
// "is"`. Post-fix (b9ea37a) dispatch runs on the config-stripped remainder.
// The test asserts the argv reaches the TASK handler — task-queued output,
// never the agent loop, never a shredding error — for a MISSING settings path
// and a REAL sandbox settings file (the config-load path must not disturb
// routing either).
func TestCLIContractSettingsConfigFlagRoutesToTaskHandler(t *testing.T) {
	bin := gopiTestBinPath(t)

	// A real (sandbox) settings file exercises the config-load path too.
	sandbox := t.TempDir()
	settingsPath := filepath.Join(sandbox, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write sandbox settings: %v", err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"missing-settings-path", []string{"--settings", filepath.Join(sandbox, "does-not-exist.json"), "task", "submit", "multi word summary contract test", "--payload", "payload-body"}},
		{"real-settings-path", []string{"--settings", settingsPath, "task", "submit", "multi word summary contract test", "--payload", "payload-body"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, home := runCLIContractProbe(t, bin, tc.args...)
			if !strings.Contains(out, "Task queued") {
				t.Fatalf("gopi %s: output missing task-queued confirmation — did NOT route to task handler\n--- output ---\n%s", strings.Join(tc.args, " "), out)
			}
			if strings.Contains(out, "unexpected positional") {
				t.Fatalf("gopi %s: routing-bug signature present (quoted summary shredded)\n--- output ---\n%s", strings.Join(tc.args, " "), out)
			}
			assertNoCLIContractSession(t, strings.Join(tc.args, " "), home)
		})
	}
}

// TestCLIContractTaskSubmitMissingSummaryIsFastError asserts the negative
// contract for `task submit` without a summary: a fast usage error, NOT an
// agent-loop hang and NOT a silent success.
func TestCLIContractTaskSubmitMissingSummaryIsFastError(t *testing.T) {
	bin := gopiTestBinPath(t)
	out, err := runCLIContractProbeAllowExit(t, bin, "task", "submit")
	if err == nil {
		t.Fatalf("gopi task submit (no summary): expected a usage error, got success\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "missing required parameter") {
		t.Fatalf("gopi task submit (no summary): output lacks the usage error\n--- output ---\n%s", out)
	}
	if strings.Contains(out, "unexpected positional") {
		t.Fatalf("gopi task submit (no summary): shredding signature present\n--- output ---\n%s", out)
	}
}

// TestCLIContractUnknownVerbIsFastUsageError asserts the negative contract
// for an unknown subcommand/verb: a fast exit-1 usage error naming the verb,
// no agent-loop fallthrough (the pre-refactor failure mode — unknown argv
// used to reach the agent loop and burn a session).
func TestCLIContractUnknownVerbIsFastUsageError(t *testing.T) {
	bin := gopiTestBinPath(t)
	cases := []struct {
		name string
		args []string
		want string // substring expected in the error output
	}{
		{"task-unknown", []string{"task", "frobnicate"}, `unknown subcommand "frobnicate"`},
		{"web-unknown", []string{"web", "bogus"}, `unknown subcommand "bogus"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := runCLIContractProbeAllowExit(t, bin, tc.args...) //stylegate:ignore runs the fixture probe; the captured output is the assertion
			if !strings.Contains(out, tc.want) {
				t.Fatalf("gopi %s: output lacks %q\n--- output ---\n%s", strings.Join(tc.args, " "), tc.want, out)
			}
		})
	}
}

// --- The deprecation contract (epic cli-help-dedup) ----------------------

// taskServeAliasPointerFingerprint is the one-line help the deprecated
// `gopi task serve` alias prints. It is ASSEMBLED from the same constants the
// pointer function uses, so this inventory entry cannot pin a sentence the
// live pointer no longer prints — and it cannot be satisfied by the family
// usage page either (that page's fingerprint is `task`'s own entry: the two
// entries would then assert the same string for two different contracts).
//
// Rule 13: every verb word comes from its SSOT (taskverbs.Serve / mergeProgTopLevel)
// — nothing is re-typed here. The line it pins was re-pointed at `gopi merge`
// when the resident merger daemon was retired (2026-09-19,
// devdoc/merger-retired.md): the alias must not point at a verb that refuses.
func taskServeAliasPointerFingerprint() string {
	return "gopi task " + taskverbs.Serve + " is a deprecated alias"
}

// taskServeAliasOldPromise is the phrase the pre-retirement pointer line used
// ("the merger is now the top-level `gopi merger`"), kept ONLY as the thing the
// re-pointed line must no longer say: the verb it promises refuses since
// 2026-09-19 (devdoc/merger-retired.md).
const taskServeAliasOldPromise = "is now the top-level"

// TestCLIContractTaskServeAliasPrintsOneLinePointer is the anti-regression
// for the defect this epic fixes: `gopi task serve -h` used to dump the whole
// `gopi task` family usage, leaving the operator to diff two pages to find
// out which spelling is canonical. The contract is now:
//  1. exit 0 (a help query is a success),
//  2. output IS the pointer line — it starts with it,
//  3. output is ONE line (the family page is ~400 lines: asserting the family
//     fingerprint is ABSENT pins "pointer, not dump" in the same probe),
//  4. it names the canonical verb, so the reader can act,
//  5. no agent-loop fallthrough (the shared probe's session guard).
//
// The -h path must also stay side-effect free: the probe's isolated GOPI_HOME
// gains no lock file (a `task serve -h` that started the daemon would create
// serve.lock — the "help must never daemonize" half of the contract).
func TestCLIContractTaskServeAliasPrintsOneLinePointer(t *testing.T) {
	bin := gopiTestBinPath(t)
	out, home := runCLIContractProbe(t, bin, CliVerbTask, taskverbs.Serve, "-h")

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("gopi task %s -h printed %d lines — the alias contract is ONE pointer line, not a help page:\n%s",
			taskverbs.Serve, len(lines), out)
	}
	if want := taskServeAliasPointerFingerprint(); !strings.HasPrefix(lines[0], want) {
		t.Fatalf("gopi task %s -h: first line %q does not start with %q", taskverbs.Serve, lines[0], want)
	}
	// The pointer must name a verb that WORKS — and this clause was itself the
	// failing half twice over, so its history is the contract. It asserted
	// `gopi merge` as the canonical home, on the reasoning that `gopi merger` is
	// retired (2026-09-19) and the reader must not be sent to a refusing verb.
	// The epic merge-removal then removed that verb too, which made the
	// assertion demand a DEAD END: the pointer's whole value is that the
	// spelling it names can be typed and does something. So it now asserts the
	// surviving spelling — the family atom (`gopi task merge`), which runs the
	// same engine under the same lock. Measured before the edit:
	// `gopi task merge feat-x --repo <probe>` → "merge feat-x merged".
	if !strings.Contains(out, mergeFaceProg) {
		t.Fatalf("gopi task %s -h: the pointer never names the working %q verb:\n%s", taskverbs.Serve, mergeFaceProg, out)
	}
	if strings.Contains(out, taskServeAliasOldPromise) {
		t.Fatalf("gopi task %s -h: the pointer still says %q — it sends the reader to the RETIRED merger verb (2026-09-19, devdoc/merger-retired.md):\n%s",
			taskverbs.Serve, taskServeAliasOldPromise, out)
	}
	family := cliContractFingerprint(CliVerbTask)
	if strings.Contains(out, family) {
		t.Fatalf("gopi task %s -h still prints the family usage (%q present) — replace the dump with the pointer:\n%s",
			taskverbs.Serve, family, out)
	}
	if _, err := os.Stat(filepath.Join(home, serveLockFilename)); err == nil {
		t.Fatalf("gopi task %s -h created %s — a help query must never take the daemon's lock",
			taskverbs.Serve, serveLockFilename)
	}
}

// TestCLIContractLegacySpellingsStillDispatch pins the "never remove a
// dispatch case" rule of this epic with the REAL binary: every deprecated or
// re-homed spelling must still REACH its handler (never the agent loop, never
// an "unknown subcommand"), even when the new contract makes it refuse.
// Only the routing is asserted here — each spelling's own contract test
// asserts what the handler then prints.
func TestCLIContractLegacySpellingsStillDispatch(t *testing.T) {
	bin := gopiTestBinPath(t)
	cases := []struct {
		name      string
		args      []string
		wantRoute string // a token only the reached handler can print
	}{
		{
			// Deprecated flat tool verb: the dispatch case is KEPT and now
			// REFUSES by name, pointing at the live home (epic verb-restructure
			// 2026-10-05: `gopi tool web search`). It used to forward with a
			// deprecation note; the routing claim is the same either way —
			// the word must reach ITS handler, never the agent loop.
			"web_search", []string{toolnames.ToolWebSearch, "-h"}, "gopi web_search: retired",
		},
		{
			// The twin-row audit MOVED OFF the memsync channel (2026-11-05)
			// and is a LEDGER verb now: `gopi task dedup-scan`. This case is
			// the re-homed spelling, driven end to end so a reader who arrives
			// from the removal sentence finds the audit actually running.
			//
			// The OLD address (`memory memsync --dedup-scan`) is not merely
			// retired here — the whole node→node patch channel is deleted, so
			// its word is refused by name pointing at this one (see
			// verbAliases). That is the different half of the same move, and it
			// is covered by the retired-alias cases rather than repeated here.
			"task dedup-scan", []string{CliVerbTask, taskverbs.DedupScan, "--" + taskDedupScanLiveFlag},
			"rows scanned",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			// Exit-code tolerant ON PURPOSE: some legacy spellings now
			// REFUSE (exit 1) with a pointer to their new home — that IS
			// their contract. Routing is what this test guards, so a
			// non-zero exit is not a failure here; reaching the WRONG
			// handler (or the agent loop) is.
			out, err := runCLIContractProbeEnv(t, bin, home, tc.args...)
			if !strings.Contains(out, tc.wantRoute) {
				t.Fatalf("gopi %s did not reach its handler (missing %q, exit %v) — a dispatch case was removed:\n%s",
					strings.Join(tc.args, " "), tc.wantRoute, err, out)
			}
			if strings.Contains(out, "unknown subcommand") || strings.Contains(out, "unexpected positional") {
				t.Fatalf("gopi %s: routing-bug signature present\n%s", strings.Join(tc.args, " "), out)
			}
			assertNoCLIContractSession(t, strings.Join(tc.args, " "), home)
		})
	}
}
