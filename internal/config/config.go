// Package config centralises GoPi's run-time configuration so every tuning
// knob can be expressed three interchangeable ways, in precedence order:
//
//	CLI flag  --xxx        (highest)
//	env var   GOPI_XXX     (per-file reads already exist; this package is the
//	                       single source of truth for the *mapping*)
//	settings.json          (key "xxx")
//	default                (lowest)
//
// Previously each signal was read independently from os.Getenv scattered
// across loop.go / rawlog.go / tools/timeout.go / claude_client.go,
// which meant there was no single place listing every knob, no way to set the
// same knob in settings.json, and no way to override it from the command line.
// This package exists so `gopi -h` can advertise every knob with its env var,
// settings key and default, and so a user can configure GoPi whichever way
// they prefer.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/thinkmode"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// ThinkingMode is the settings.json "thinking" value: the canonical strings
// "enabled" / "disabled". UnmarshalJSON also accepts bools (true/false) and
// the "on"/"off"/"1"/"0" spellings for backward compatibility with older
// configs; any other value is a parse error so a typo cannot silently flip
// the mode. Zero value ("") means "unset — the code's built-in default
// (disabled) applies".
type ThinkingMode string

const (
	// Values come from internal/thinkmode, the single source of truth for the
	// thinking wire values (Rule 14) — a settings-file spelling must not drift
	// from what the client puts on the wire, or "enabled" in settings would
	// stop matching the request builder's comparison.
	ThinkingEnabled  ThinkingMode = thinkmode.Enabled
	ThinkingDisabled ThinkingMode = thinkmode.Disabled
)

// UnmarshalJSON accepts both the canonical string form ("enabled" /
// "disabled") and a JSON bool (true → enabled, false → disabled), so old
// settings files that used booleans keep working.
func (m *ThinkingMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch {
		case IsTrueValue(s):
			*m = ThinkingEnabled
			return nil
		case IsFalseValue(s):
			*m = ThinkingDisabled
			return nil
		}
		return fmt.Errorf("invalid thinking value %q (want \"enabled\" or \"disabled\")", s)
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*m = ThinkingEnabled
		} else {
			*m = ThinkingDisabled
		}
		return nil
	}
	return fmt.Errorf("invalid thinking value: %s", data)
}

// Config is the resolved, type-checked view of GoPi's tunable knobs. Every
// field maps 1:1 to a documented setting key and (where one exists) an
// env var. A zero/omit value means "unset — fall back to the code's
// built-in default", so callers that already apply their own default (the
// per-file env parses) keep working.
type Config struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`

	// Vision endpoint (multimodal image understanding): separate model + base
	// URL so a future switch to another multimodal provider is zero-code
	// (settings.json only). Falls back to DeepSeek's Anthropic-compatible
	// endpoint and the same api_key by default.
	VisionModel   string `json:"vision_model,omitempty"`
	VisionBaseURL string `json:"vision_base_url,omitempty"`

	// DefaultDevice is the capture client's configured default device — tier 2
	// of the voice-input device-selection policy. Empty means unconfigured, and
	// the policy falls through to the system default capture (tier 3) and then
	// the first enumerated device (tier 4).
	DefaultDevice string `json:"default_device,omitempty"`

	// AudioEngine selects `gopi tool voice_input`'s transcription engine by the
	// SHAPE of the work: "recording" (cloud batch, the DEFAULT), "streaming"
	// (cloud real-time) or "local" (on this machine). Empty means UNCONFIGURED,
	// which resolves to the default through the ONE resolver in cmd/agent.
	//
	// optional-string semantics (Rule 15a): `omitempty` is correct because ""
	// IS a legal, meaningful value — "nobody chose" — and the resolver's
	// terminal default answers it. The pre-rename key and env var
	// (AudioEngineLegacySettingsKey / AudioEngineLegacyEnv) and the pre-rename
	// engine NAMES are still read; see LegacyVoiceEngine / LegacyDoubao* below.
	AudioEngine string `json:"audio_engine,omitempty"`

	// AudioEngineParam holds one PARAMETER BLOCK PER ENGINE. It is a struct
	// rather than a map so an unknown block or an unknown knob inside a block
	// is a defined, reportable outcome (Rule 15) instead of a silently retained
	// map entry nothing reads — see AudioEngineUnknown in audio_engine_param.go.
	//
	// `omitempty` on a struct field has no effect in encoding/json (a struct is
	// never "empty"), so its absence here is not an omission but the honest
	// spelling: the block is only ever WRITTEN by an operator, and this field
	// exists to READ one.
	AudioEngineParam AudioEngineParam `json:"audio_engine_param"`

	// AudioVerbose is the voice surfaces' DEBUG-SINK switch: the per-partial
	// transcript stream and the streaming channel's counters (partial count, the
	// first/final partial instants, the vendor's logid/reqid, how much audio went
	// out) are printed only when this is on. The transcript and the ordinary
	// per-run lines are printed either way.
	//
	// It is a PLAIN bool, not a pointer, and the difference from MemoryPrefetch
	// is the DEFAULT: this knob defaults to OFF, so an absent key and a declared
	// `false` mean the same thing and nothing is lost by collapsing them (the
	// pointer exists next door only because a default-ON knob must distinguish
	// "nobody asked" from "the file said off").
	//
	// Read by config.ResolveAudioVerbose (Rule 2), which applies the flag and the
	// env channel over this key.
	AudioVerbose bool `json:"audio_verbose,omitempty"`

	// ── the PRE-RENAME surface, still read (2026-10-09) ────────────────────
	//
	// Every field below is the old spelling of something now inside
	// AudioEngineParam, kept so a settings file written before the rename keeps
	// working. They are NORMALISED into the nested block at load time
	// (normalizeAudioParams) and the run that found one says so on stderr,
	// naming the key to write next — a knob that silently stopped taking effect
	// would be a worse outcome than one that never existed (Rule 15).
	//
	// They carry `json:"-"`? NO — deliberately not: a shadow field whose tag is
	// "-" is never populated, and the whole point is to READ what the old
	// document says. They are written by nothing (gopi only reads settings), so
	// the deprecation cannot round-trip into a file gopi writes.
	LegacyVoiceEngine string `json:"voice_engine,omitempty"`

	LegacyDoubaoKeyFile        string `json:"doubao_key_file,omitempty"`
	LegacyDoubaoAPIKey         string `json:"doubao_api_key,omitempty"`
	LegacyDoubaoResourceID     string `json:"doubao_resource_id,omitempty"`
	LegacyDoubaoHotwords       string `json:"doubao_hotwords,omitempty"`
	LegacyDoubaoBaseURL        string `json:"doubao_base_url,omitempty"`
	LegacyDoubaoStreamEndpoint string `json:"doubao_stream_endpoint,omitempty"`
	LegacyDoubaoAppID          string `json:"doubao_app_id,omitempty"`
	LegacyDoubaoToken          string `json:"doubao_token,omitempty"`

	// AudioEngineParamUnknown carries the `audio_engine_param` document as
	// written, so the blocks and knobs this build does not model can be NAMED
	// (it is filled by rawTopLevel, see DecodeAudioEngineParam). A settings key
	// silently ignored is the failure Rule 15 bans.
	AudioEngineParamUnknown map[string]json.RawMessage `json:"-"`
	// audioEngineParamUnknown is DecodeAudioEngineParam's verdict for the same
	// object: which block or knob inside it this build does not model. It is
	// unexported because the REPORT is the surface (AudioEngineUnknownError),
	// not the raw list — two readers of one list is how a message and a check
	// drift apart.
	audioEngineParamUnknown []AudioEngineUnknown
	// rawTopLevel is the settings document's own top level, kept so a question
	// about what the FILE carries (a retired key, an unmodelled block) can be
	// answered without a second read — see SettingsCarriesKey.
	rawTopLevel map[string]json.RawMessage

	// GopiHomePath relocates the gopi data root: ~/.gopi becomes
	// $GOPI_HOME/.gopi. Set by the --home flag (or the GOPI_HOME env var
	// directly); every gopi-data path (settings, sessions, raw logs,
	// memory.db, plugins) resolves under it. The test/sandbox isolation knob.
	GopiHomePath string `json:"gopi_home,omitempty"`

	SettingsPath string `json:"-"` // resolved --settings / GOPI_SETTINGS / default

	// Prompt is the task instruction given via --prompt (or GOPI_PROMPT).
	// When set, positional arguments are treated as input FILES/IMAGES to
	// attach; when empty, the first positional is itself the prompt text (or
	// a text file whose contents become the prompt).
	Prompt string `json:"prompt,omitempty"`

	// promptFrom records the CHANNEL the value in Prompt came from — flag,
	// settings, or environment (see internal/config/cli_verb_guard.go's
	// promptSource). It is unexported and never serialised: it is provenance
	// for THIS resolution, not configuration. The run path's verb guard reads
	// it because the string alone cannot answer its question — an inherited
	// GOPI_PROMPT and a `--prompt` on this very command line are the same
	// string and opposite answers (task 01a0de86, 2026-09-26).
	promptFrom promptSource

	// Positionals are the non-flag arguments (input files / prompt text),
	// collected flag-value-aware by FlagOverrides so a flag's VALUE (e.g.
	// --model deepseek-v4-flash-vision-exp) can never be mistaken for an
	// input file. json:"-" — never serialised.
	Positionals []string `json:"-"`

	// PositionalVerbFlag is the option token the POSITIONALS follow, as the
	// operator spelled it (`--db`), or "" when no option precedes them. It
	// exists for ONE reader: the run path's positional-verb refusal names the
	// flag that was misplaced, so the operator is told which flag to move
	// rather than merely that the order is wrong. Filled by FlagOverrides in
	// the same walk that collects Positionals (scanPrefixingRunFlag), so the
	// two can never disagree about where the positionals begin.
	// json:"-" — never serialised.
	PositionalVerbFlag string `json:"-"`

	// SetEnvVars records which environment keys were explicitly overridden by
	// a CLI flag (json:"-" — never serialised). ApplyEnv(fromFlags=true) uses
	// it to propagate ONLY the knobs the user actually passed on the command
	// line. Without this, an unset plain bool/int flag (zero value) would be
	// force-written to the process env (e.g. GOPI_DEBUG=off, GOPI_TIMEOUT_SEC=0),
	// which the downstream env readers misinterpret as an explicit setting —
	// the "gopi prints the full request/stream log" regression.
	SetEnvVars map[string]bool `json:"-"`

	// --- thinking / reasoning ---
	// Thinking is the settings.json "thinking" knob ("enabled"/"disabled").
	// nil = unset → the code default (disabled). CLI --thinking and the
	// GOPI_THINKING env var feed the same field.
	Thinking        *ThinkingMode `json:"thinking,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	// EffortValues is the settings.json "effort_values" list — the effort
	// value whitelist THIS provider accepts, declared as data (schema, not
	// code). Empty/absent → the DeepSeek baseline
	// [minimal low medium high max]. The agent validates the requested
	// effort against this list before wiring it (config.resolveReasoningEffort);
	// no cross-provider mapping is performed — the request sends exactly
	// what the user asked for, and a vendor that still rejects it is
	// handled by the minimal-core degradation ladder (error_1214.go).
	EffortValues   []string `json:"effort_values,omitempty"`
	ThinkingBudget int      `json:"thinking_budget,omitempty"`
	HideThinking   bool     `json:"hide_thinking,omitempty"`
	KeepThinking   bool     `json:"keep_thinking,omitempty"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
	ContextWindow  int      `json:"context_window,omitempty"`

	// --- agent runtime ---
	TimeoutSec int  `json:"timeout,omitempty"` // overall agent timeout in seconds (0 = unlimited)
	Debug      bool `json:"debug,omitempty"`

	// --- logging ---
	RawLog    string `json:"raw_log,omitempty"` // "on" | "off"
	RawLogDir string `json:"raw_log_dir,omitempty"`

	// --- tools (bash / file / websearch / memory) ---
	BashTimeoutSec   int    `json:"bash_timeout_sec,omitempty"`
	BashIdleSec      int    `json:"bash_idle_sec,omitempty"`
	ToolTimeoutSec   int    `json:"tool_timeout_sec,omitempty"`
	AllowDangerous   bool   `json:"allow_dangerous,omitempty"`
	WebSearchScripts string `json:"websearch_scripts_dir,omitempty"`
	MemoryDB         string `json:"memory_db,omitempty"`

	// NoEnforceMemory disables BOTH harness memory checkpoints (task-start
	// memory_search + task-end memory_store) — A/B isolation for measuring
	// pure prompt persuasion (MEMORY-PROMPT-PERSUASION.md §4.1).
	NoEnforceMemory bool `json:"no_enforce_memory,omitempty"`

	// NoEnforceMemoryConsult disables the REVIEW-TIME memory-consult
	// accountability checkpoint (candidate C of the memory-consult epic): the
	// one-shot question "which work could a memory search have saved?" asked
	// at the first committed item. It is a SEPARATE switch from
	// NoEnforceMemory by design — the checkpoint is a measurement instrument
	// that costs one short prompt, so an A/B that wants an untouched baseline
	// turns it off ALONE while the task-start search stays forced.
	NoEnforceMemoryConsult bool `json:"no_enforce_memory_consult,omitempty"`

	// The settings.json keys `peak_hours` and `ignore_peak_hours` USED to be
	// declared here, for the peak-hour cost guard — a launcher that refused to
	// start any gopi invocation inside DeepSeek's 高峰时段 (Beijing 09:00-12:00 /
	// 14:00-18:00, billed at 2x). The guard was removed outright
	// (devdoc/peak-guard-removal.md): it approximated "will this run spend
	// money?" by the command verb, which is false — `gopi task show` costs
	// nothing yet was refused, the merger's mandated verify child was killed by
	// the clock (breaking the merge flow), and the e2e suite went red inside a
	// window.
	//
	// BACKWARD TOLERANCE, deliberately: a user's settings.json may still carry
	// either key. settingsFromFile decodes with a plain json.Unmarshal (no
	// DisallowUnknownFields), so an undeclared key is silently IGNORED — the
	// file keeps loading, nothing errors, and nothing honours the guard. Pinned
	// by TestRemovedPeakHoursSettingsKeysAreTolerated.

	// ScratchGC is the settings.json switch for the RESIDENT scratch sweep
	// (cmd/agent's worker/merger tick): the durable, deployment-level off
	// switch ("scratch_gc": false). nil = unset = enabled — a deployment that
	// upgrades into the sweep gets a bounded scratch tree without editing
	// anything. The env face is GOPI_TEST_SCRATCH_GC_MIN=0 (see
	// internal/scratch.GCIntervalEnvName).
	ScratchGC *bool `json:"scratch_gc,omitempty"`

	// ScratchGCStaleMin is the settings.json value of the sweep's STALE-WINDOW
	// knob — the age, in MINUTES, past which an unheld scratch entry is
	// garbage ("scratch_gc_stale_min": 90). 0/absent = the built-in default
	// (scratch.DefaultStaleAge — the ONE definition of the number, in the leaf
	// package); the env face is scratch.GCStaleAgeEnvName and the flag face is
	// --scratch-gc-stale-min (ScratchGCStaleMinFlagName). It is the declarative
	// layer of a knob whose single publisher is PublishScratchGCStaleMin and
	// whose single reader is scratch.ResolveStaleAge.
	ScratchGCStaleMin int `json:"scratch_gc_stale_min,omitempty"`

	// The settings.json key `merge_tick` USED to be declared here (MergeTickMin),
	// as the cadence of the RESIDENT MERGE TICK — a worker-round pass that spawned
	// `gopi merge --fast` as a BLOCKING child. It is RETIRED (2026-09-27): merging
	// is `gopi merge`'s job, and the worker must not do another component's work
	// (it takes the worker — the thing that runs tasks — offline for the duration,
	// and two workers contend for the merge flock). See
	// devdoc/worker-merge-tick-retired.md.
	//
	// BACKWARD TOLERANCE, the same shape the retired `peak_hours` /
	// `ignore_peak_hours` keys keep: a settings.json written while the tick existed
	// still carries `merge_tick`, and that file must keep loading with every OTHER
	// knob intact. FromSettingsFile decodes with a plain json.Unmarshal (no
	// DisallowUnknownFields), so an undeclared key is silently IGNORED — the file
	// loads, nothing honours it. Pinned by
	// TestRetiredMergeTickSettingsKeyIsTolerated; the resulting SILENCE is closed
	// on the worker/merge path by the retired-knob report in
	// cmd/agent/retired_tick_knobs.go (Rule 15: a retired knob is REPORTED, never
	// silently ignored).

	// (MemorySyncTickMin — the RESIDENT memory-sync tick's cadence — USED to be
	// the settings struct field here. The tick is RETIRED 2026-10-25: each round
	// that fired it spawned one cross-node ledger-sync child per declared peer
	// and blocked the claim loop for 315-534s. The key is no longer decoded, which
	// is SAFE rather than silent: FromSettingsFile uses a plain json.Unmarshal (no
	// DisallowUnknownFields), so a settings.json still carrying `memory_sync_tick`
	// loads with every other knob intact, and the retirement report
	// (cmd/agent/memory_sync_tick_retired.go) NAMES it back at that operator
	// together with what survives in its place. The two spellings live in
	// config.MemorySyncTickRetiredEnvName / MemorySyncTickRetiredSettingsKey.)

	// --- node identity (bug041 "which node am I?") ---
	// Single canonical pair (32450a1 landed two Role/SyncTo declarations —
	// one plain string, one NodeRole-typed — breaking the build; the typed
	// pair below is the ONE source, consumed by ResolveNodeRole/IsServerNode).
	// Role declares THIS machine's topology identity ("server" =
	// dispatcher/center; "client" = compute node reporting back to SyncTo).
	// SyncTo is the ssh alias of the node this client reports/syncs to
	// (server-side it stays empty). Resolved once via ResolveNodeRole
	// (env GOPI_NODE_ROLE/GOPI_SYNC_TO > here). Declared, never
	// hostname-guessed — same philosophy as compute_nodes/sync_peers.
	Role   NodeRole `json:"role,omitempty"`
	SyncTo string   `json:"sync_to,omitempty"`

	// --- memsync peers (M4.13 delta sync) ---
	// SyncPeers is the task-ledger peer whitelist (settings key
	// "sync_peers"): ssh config aliases this node exchanges TASK-LEDGER
	// deltas with (task sync / claim broadcast / git channel). Resolved via
	// config.ResolveSyncPeers (CLI > env GOPI_SYNC_PEERS > here). Same
	// declared-topology philosophy as compute_nodes. Empty = single-node
	// task mode: ALL task cross-node machinery is disabled (HasSyncPeers).
	SyncPeers []string `json:"sync_peers,omitempty"`

	// TaskSync is the TASK-ROW SYNCHRONIZATION switch (settings key
	// "task_sync"; env GOPI_TASK_SYNC): does this node AUTOMATICALLY exchange
	// task-ledger rows with its declared peers (claim broadcast, terminal
	// push, serve-tick delta/git passes, submit forward)? Default false = OFF,
	// and OFF is also the UPGRADE posture: a node that arrives with a stale
	// role/sync_to keeps behaving locally until an operator turns it on.
	//
	// It is deliberately NOT the same axis as Role (identity: conflict
	// arbitration + branch push-back) or SyncPeers (the declared topology both
	// the automatic legs and the code-delivery faces resolve through): a client
	// node with task_sync=false still hands its branch back at finish-cleanup.
	// Resolved in exactly one place, config.TaskSyncEnabled (Rule 2).
	TaskSync bool `json:"task_sync,omitempty"`

	// ChatOpAuto is the chat console's STANDING POSTURE for the ONE class it must
	// ask about — a LONG operation (settings key "chat_op_auto"; env
	// GOPI_CHAT_OP_AUTO). The criterion is the CATEGORY of the action, never a
	// clock (tools.ChatOpClassify / ChatOpAdmit): OPERATOR WORK runs, a REPOSITORY
	// WRITE is refused and routed to a task, and only a long operation (whole-tree
	// suite, full build, package install, merge/worker/serve) needs the operator's
	// word. Default false = all-ask; true = the console may also start one itself.
	//
	// The knob lives on this struct and is read in exactly one place
	// (tools.ChatOpChannel, Rule 2). It is deliberately NOT the same axis as
	// TaskSync (row exchange) or Role (identity): this one answers "may the
	// console act before asking?" and nothing else.
	ChatOpAuto bool `json:"chat_op_auto,omitempty"`

	// OutputLanguage is the REPLY-language knob (settings key
	// "output_language"; env GOPI_OUTPUT_LANGUAGE; CLI --output-language).
	// When set to a language tag (e.g. "zh-CN") gopi prepends a small
	// output-language directive to BOTH system prompts it assembles — the task
	// path (assembleSystemPrompt) and the plain chat path
	// (chatSystemPromptServed). When EMPTY no directive is emitted at all, so
	// an unconfigured run sends the byte-identical system prompt it sent
	// before the knob existed.
	//
	// It is deliberately a VALUE knob, not a switch: "" (unset) is the silent
	// state and there is no vocabulary of "off" to get wrong. The value's
	// SHAPE (a BCP-47-ish tag) is validated in exactly one place —
	// config.resolveOutputLanguage (Rule 2) — which reports an unusable value
	// on stderr and lets the next channel decide. Scope is the REPLY language
	// only: the model's reasoning language is not affected (operator ruling,
	// briefings/brief-output-language-knob.md).
	OutputLanguage string `json:"output_language,omitempty"`

	// MemoryPrefetch is the TYPING-TIME MEMORY PREFETCH switch (settings key
	// "memory_prefetch"; env GOPI_MEMORY_PREFETCH; CLI --memory-prefetch): does
	// this process fetch memory hits in the BACKGROUND while the operator types,
	// so the turn's model input can carry them? Default ON, and OFF means no
	// goroutine is started and the prompt is byte-identical to the pre-knob tree.
	// Read by config.MemoryPrefetchEnabled (Rule 2), which reports an unusable env
	// value on stderr and lets the next channel decide (Rule 15).
	//
	// It is a POINTER because the default is ON. nil = the settings file did not
	// mention the key at all — nobody asked, and the built-in default answers;
	// non-nil = the file DID declare it, and the declaration is the answer in
	// BOTH directions, so `{"memory_prefetch": false}` turns the knob off instead
	// of reading as "unset" (the ScratchGC precedent). A plain bool could not
	// carry that distinction: an absent key and a declared `false` are the same
	// value, which was harmless only while the default was itself false.
	MemoryPrefetch *bool `json:"memory_prefetch,omitempty"`

	// ChatIdealGeometry is the chat surface's IDEAL GEOMETRY (settings key
	// "chat_ideal_geometry"; env GOPI_CHAT_IDEAL_GEOMETRY; CLI
	// --chat-ideal-geometry): the size the startup screen, the tips panel and
	// the info bar are designed for, as "COLSxROWS". Read by
	// config.ChatIdealGeometry (Rule 2), which reports an unusable value on
	// stderr and lets the next channel decide (Rule 15) — never silently the
	// default.
	ChatIdealGeometry string `json:"chat_ideal_geometry,omitempty"`

	// ChatSurfaceTurns is the chat_surface tool's SELF-DRIVEN TURN BUDGET
	// (settings key "chat_surface_turns"; env GOPI_CHAT_SURFACE_TURNS; CLI
	// --chat-surface-turns): how many turns `chat_surface action=type` may add
	// to ONE period, and the ceiling a max_turns argument is clamped to. Read
	// by config.ChatSurfaceTurns (Rule 2), which reports an unusable value on
	// stderr and lets the next channel decide (Rule 15) — never silently the
	// default.
	ChatSurfaceTurns string `json:"chat_surface_turns,omitempty"`

	// AmbiguousWidth is how an East Asian Width AMBIGUOUS (A) rune is measured
	// on the terminal gopi draws into (settings key "ambiguous_width"; env
	// GOPI_AMBIGUOUS_WIDTH; CLI --ambiguous-width): "wide" (two cells — the CJK
	// reading, e.g. box-drawing U+2500 and gopi's own turn marker U+258C),
	// "narrow" (one cell — the Western reading, and the historical behaviour),
	// or empty = the LOCALE decides (LC_ALL, then LC_CTYPE, then LANG: zh/ja/ko
	// are wide, everything else narrow). Read by
	// config.ResolveAmbiguousWidthMode, which reports an unusable value on
	// stderr and lets the next channel decide (Rule 15). Every width decision
	// in internal/lineedit and internal/text2img is made through it, so the
	// editor and the renderer cannot disagree about the same cell.
	AmbiguousWidth string `json:"ambiguous_width,omitempty"`

	// FilmTerminal is the terminal emulator a FILM is photographed in (settings
	// key "film_terminal"; env GOPI_FILM_TERMINAL; CLI --film-terminal). It is
	// one of the names in internal/shot's terminal registry — an emulator, not a
	// path: the name is resolved against that table and an unknown one is
	// REFUSED with the usable list, never silently replaced by the default
	// (Rule 15). Empty = the built-in default (xfce4-terminal since the operator's
	// 2026-10-04 ruling — xterm's ISO8859-1 font draws a Chinese surface as
	// boxes; xterm stays selectable), which is what every caller who says
	// nothing has always got.
	//
	// Why it is a knob: the film's purpose is to photograph the surface the
	// OPERATOR sees, and the operator's terminal is a property of their desktop,
	// not of this pipeline. Declared here so the flag, the env var and the file
	// cannot hold three ideas of which terminal a film is made in; resolved in
	// ONE place (shot.ResolveFilmTerminal, Rule 2/34).
	FilmTerminal string `json:"film_terminal,omitempty"`

	// --- memory-asset peers (distilled_qa knowledge sync) ---
	// MemoryPeers is the KNOWLEDGE-ASSET peer whitelist (settings key
	// "memory_peers"): the ssh config aliases this node may move knowledge to.
	// THREE doors read it — the two `gopi memory backup` legs (`--peer <alias>`
	// ships a ledger copy, `--serve` is that peer's side) AND the RESTORED
	// manual session `gopi memory sync --peer <alias>` (one unique-only
	// bidirectional run by default; `5677cc6e` had deleted that verb, and it
	// came back 2026-11-06 as a manual door). An alias that is not on this list
	// is refused before any ssh argv exists. Deliberately
	// DECOUPLED from sync_peers (2026-09-02 bugfix): single-node task mode
	// clears sync_peers to [] while the knowledge channel — a deliberately kept
	// cross-node feature — stays available. Resolved via
	// config.ResolveMemoryPeers (env GOPI_MEMORY_PEERS > here).
	MemoryPeers []string `json:"memory_peers,omitempty"`

	// MemorySyncOnStore is the RETIRED direction of the store-triggered asset
	// sync (settings key "memory_sync_on_store"), kept DECLARED so a typo in it
	// is still an error instead of a silent ignore. The leg it configured is
	// gone (2026-10-27, epic memory-store-sync-decouple): every well-formed
	// value now resolves to "off" via
	// config.ResolveMemorySyncOnStoreDirection (env GOPI_MEMORY_SYNC_ON_STORE >
	// here), the ONE reader of both channels, and an operator who still sets
	// push/pull is told so on the start path
	// (cmd/agent/memory_sync_on_store_retired.go).
	MemorySyncOnStore string `json:"memory_sync_on_store,omitempty"`

	// --- task staleness (stale-REPORT grace) ---
	// TaskStallSec is the heartbeat-stall grace: an in_progress row whose
	// last_toolcall_at (created_at fallback) is older than this many
	// seconds is reported as STALE (the PRIMARY signal — a live run
	// touches last_toolcall_at on every tool batch; a wedged/dead process
	// does not). Settings key "task_stall_sec"; env GOPI_TASK_STALL_SEC.
	// Resolved in exactly one place — config.ResolveTaskStallGrace.
	// 0 = off (detection disabled).
	//
	// It drives REPORTING ONLY. gopi used to also DISPOSE the rows this
	// grace flagged, on a second, longer grace; that half is gone
	// (devdoc/zombie-sweep-removed.md) and a stale row is now left exactly
	// where it is, because a row nothing could close out is a defect in the
	// close-out path and the report is how it stays visible instead of being
	// swept away.
	TaskStallSec int `json:"task_stall_sec,omitempty"`

	// TaskVerifyTimeoutSec is the WALL-CLOCK CAP on the child that
	// `gopi task verify` spawns (seconds). Settings key
	// "task_verify_timeout_sec"; env GOPI_TASK_VERIFY_TIMEOUT_SEC; CLI
	// `gopi task verify --timeout N` (which wins). Resolved in exactly one
	// place — config.ResolveTaskVerifyTimeout. 0 = unlimited (the explicit
	// opt-out); absent = DefaultTaskVerifyTimeoutSec. Unlike the grace
	// periods above, an absent value is BOUNDED by default: an unbounded
	// default parks an unattended caller forever (see the constant).
	TaskVerifyTimeoutSec int `json:"task_verify_timeout_sec,omitempty"`

	// --- temp-workspace root (tmpdir epic 2026-09-14; parameterised 2026-09-16) ---
	// TmpDir is the directory gopi and EVERY child it spawns must use as their
	// temp workspace. It is an ORDINARY knob (Rule: a parameter, not a banner):
	// settable through the same three channels, with the same precedence, as
	// every other entry in EnvVars — --tmpdir > GOPI_TMPDIR > the settings
	// "tmpdir" key > the built-in default (<home>/workspace/_tmp, an ordinary
	// directory: scratch.DefaultTmpDir). A caller-exported TMPDIR is still
	// honoured as the ambient override (settings only FILL an unset variable).
	//
	// Why it exists at all: /tmp is a directory on the `/` partition on this
	// host and the ambient TMPDIR is normally unset, so every spawned child
	// (task-run gopi, the bash tool, git) resolved its temp workspace to /tmp by
	// IMPLICIT fallback — 23.5G of residue accumulated there. main.go publishes
	// the resolved value into gopi's OWN environment before any spawn, so the
	// children (which start from os.Environ()) inherit it without a per-spawn
	// change. A value that cannot be provided or written fails LOUDLY, naming
	// path + reason + channel (scratch.TmpDirUnusableError) — never a silent
	// fallback to the ambient root.
	// See devdoc/tmpdir-param-not-banner.md.
	TmpDir string `json:"tmpdir,omitempty"`

	// SandboxDir is the root under which agent SANDBOXES live — the standard
	// private gopi profile an isolated run gets (settings key "sandbox_dir",
	// env GOPI_SANDBOX_DIR, flag --sandbox-dir). It is an ORDINARY knob with
	// the same three channels and the same precedence as TmpDir, resolved by
	// ResolveSandboxRoot; the built-in default is
	// SandboxRootDefault() = <home>/workspace/gopi-sandbox.
	//
	// Why it exists: an agent that needed an isolated profile used to invent
	// one per script (`SB=$(mktemp -d)`, `~/workspace/sbx-$$`), each with a
	// different shape and no reclaim. ONE configured root + ONE initializer
	// (internal/sandbox) makes the shape reproducible and the reclaimer
	// (scratch.SweepSandboxes, riding the existing scratch-gc tick) possible.
	// See devdoc/agent-sandbox-unify.md and GOPI.md's sandbox rule.
	SandboxDir string `json:"sandbox_dir,omitempty"`

	// --- per-repo build contract (rebuild-after-merge epic 2026-09-02) ---
	// Repos is the per-repo table (settings key "repos"): a map from repo
	// path to that repo's build declaration. A repo meant to be a gopi
	// task field SHOULD declare its build here — after a successful merge
	// round the server runs repos.<repo>.build so the deployed binary
	// follows master. Declared, never discovered (same philosophy as
	// compute_nodes/sync_peers). A repo with NO entry has no build
	// contract — the server skips the rebuild silently.
	Repos map[string]RepoConfig `json:"repos,omitempty"`

	// --- task state-transition notifications (task-notify settings epic) ---
	// Notify is the DURABLE task-notify section (settings key "notify"):
	// webhook/cmd/events/pattern/async. Env (GOPI_TASK_NOTIFY_*) is read by
	// ResolveNotify and wins over this section when set; settings.json is
	// the persistent layer so a serve daemon spawned with NO notify env
	// (Hermes background / shell wrapper / restart — the 2026-09-02 stale
	// delivery incident) still delivers to the configured target. Zero-value
	// section = notify unconfigured (silently disabled, backward compat).
	Notify NotifySettings `json:"notify,omitempty"`
}

// --- per-repo build contract (rebuild-after-merge epic 2026-09-02) ---

// RepoConfig is ONE repo's declaration in the settings "repos" table.
type RepoConfig struct {
	// Build is the shell command that rebuilds this repo's deployable
	// artifact, run in the repo working directory (master checked out)
	// after a successful merge round. Declared, never discovered — a
	// docs-only repo, or a repo whose build isn't declared, is skipped
	// silently (no error, no guess). "make build", "CGO_ENABLED=0 go
	// build -o bin/gopi ./cmd/agent/", anything the repo needs.
	Build string `json:"build,omitempty"`
	// BuildTimeoutSec caps the build subprocess (default
	// repoBuildTimeoutDefaultSec). 0 = default; negative = no timeout.
	BuildTimeoutSec int `json:"build_timeout_sec,omitempty"`

	// WorktreeRoot declares where THIS repo's task worktrees belong
	// (settings key "worktree_root", inside the repo's "repos" entry — the
	// same table the build contract uses, Rule 12's precedent). Absent or
	// blank = worktreeroots.DefaultRoot() (<home>/workspace), so a repo that
	// declares nothing keeps the documented behaviour. It exists because a
	// worktree is a several-hundred-MB WRITE, and the 2026-09-13 /dev/shm
	// assessment found the root was a sentence in GOPI.md rather than a
	// declared contract — a `git worktree add /dev/shm/...` was a judgement
	// call. Declared, never discovered: a path is repo-specific data, not a
	// knob (env only carries the one sandbox override, WorktreeRootEnvName).
	WorktreeRoot string `json:"worktree_root,omitempty"`

	// ArchiveBuryOverride is the EXPLICIT, RECORDED permission to archive a
	// branch whose owning ledger row says the work finished (completed /
	// verified) — the ONLY way past the archive-why owner gate for the
	// unattended lanes (the merge-time compile gate), which cannot be asked a
	// question at runtime. The text IS the reason: it is what an operator must
	// write to say "yes, bury this finished deliverable, and here is why".
	// Absent (the default) = the refusal stands, so a bare `git tag`-shaped
	// burial can never happen by configuration drift.
	ArchiveBuryOverride string `json:"archive_bury_override,omitempty"`
}

// RepoArchiveBuryOverrideKey is the settings key carrying a repo's explicit
// permission to archive a finished task's branch (Rule 1b: the key is spelled
// once, and the refusal message names it from here).
const RepoArchiveBuryOverrideKey = "archive_bury_override"

// ArchiveBuryOverrideFor returns the explicit archive-bury permission recorded
// for repo ("" = none, i.e. the owner gate refuses). Errors and a missing
// entry both read as "no permission": a settings file that cannot be READ is
// never a licence to bury a deliverable (Rule 15 — the safe direction).
func ArchiveBuryOverrideFor(repo string) string {
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return ""
	}
	rc, ok := cfg.Repos[repo]
	if !ok {
		return ""
	}
	return strings.TrimSpace(rc.ArchiveBuryOverride)
}

// NotifySettings is the settings.json "notify" section — the durable
// task-notify configuration. Every field mirrors one GOPI_TASK_NOTIFY_*
// env var; ResolveNotify merges the sources per-knob (env > settings >
// code default — the config package's standing convention). The JSON
// default in the section header (events "claimed,in_progress,completed,
// failed", async false) matches the env defaults in internal/tools
// tasknotify.go, so a settings-only "notify" with just a cmd/webhook
// behaves identically to the env-only configuration it replaces.
type NotifySettings struct {
	// Webhook is the URL to HTTP-POST the JSON payload to on each
	// subscribed transition (GOPI_TASK_NOTIFY_WEBHOOK).
	Webhook string `json:"webhook,omitempty"`
	// Cmd is the shell command run with the JSON payload on stdin
	// (GOPI_TASK_NOTIFY_CMD).
	Cmd string `json:"cmd,omitempty"`
	// Events is the comma list of statuses/synthetic alarm events that fire
	// (GOPI_TASK_NOTIFY_EVENTS); "all" = every status plus every synthetic
	// alarm. Empty = the code default
	// claimed,in_progress,completed,failed,superseded (internal/tools'
	// taskNotifyEventsDefault, which every WRITABLE status must either join
	// or be recorded as a deliberate exclusion from — Rule 1/13).
	Events string `json:"events,omitempty"`
	// Pattern is the glob filter over the task's tags + task_id
	// (GOPI_TASK_NOTIFY_PATTERN); empty = broadcast.
	Pattern string `json:"pattern,omitempty"`
	// Async makes delivery fire-and-forget in a goroutine so a slow
	// webhook never delays the ledger writeback (GOPI_TASK_NOTIFY_ASYNC).
	Async bool `json:"async,omitempty"`
}

// NotifySettingsKey is the settings.json key carrying the durable
// task-notify section ("notify": {webhook, cmd, events, pattern, async}).
const NotifySettingsKey = "notify"

// notifySettingsEnv names the five GOPI_TASK_NOTIFY_* env vars — the
// one-source-of-truth mapping between each settings field and its env
// override (Rule 2: each knob read in exactly one place).
const (
	NotifyWebhookEnv = "GOPI_TASK_NOTIFY_WEBHOOK"
	NotifyCmdEnv     = "GOPI_TASK_NOTIFY_CMD"
	NotifyEventsEnv  = "GOPI_TASK_NOTIFY_EVENTS"
	NotifyPatternEnv = "GOPI_TASK_NOTIFY_PATTERN"
	NotifyAsyncEnv   = "GOPI_TASK_NOTIFY_ASYNC"
)

// ResolveNotify merges the task-notify configuration's sources, highest
// precedence first: env (GOPI_TASK_NOTIFY_WEBHOOK/CMD/EVENTS/PATTERN/ASYNC)
// > settings.json "notify" section > zero-value (notify disabled). Env is
// the ONE-OFF override channel; settings.json is the DURABLE layer — a
// serve daemon whose spawn context carries no notify env (the 2026-09-02
// incident: restart dropped GOPI_TASK_NOTIFY_CMD and stale events silently
// stopped landing) must still deliver to the settings-configured target.
//
// Per-knob resolution: an env var SET (even to "") overrides that knob's
// settings value — the operator's explicit choice wins; an UNSET env var
// falls through to settings. A settings read error (missing/malformed file)
// degrades fail-soft to the env/default view — a config slip must not
// wedge the notify side-channel, it silently disables it (backward compat:
// no notify config = no delivery, no error).
func ResolveNotify() NotifySettings {
	s := NotifySettings{}
	// Settings layer first (the durable base), then env overrides per knob.
	if cfg, err := FromSettingsFile(ResolveSettingsPath()); err == nil {
		s = cfg.Notify
	}
	if v, present := os.LookupEnv(NotifyWebhookEnv); present {
		s.Webhook = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(NotifyCmdEnv); present {
		s.Cmd = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(NotifyEventsEnv); present {
		s.Events = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(NotifyPatternEnv); present {
		s.Pattern = strings.TrimSpace(v)
	}
	if v, present := os.LookupEnv(NotifyAsyncEnv); present {
		s.Async = envBoolValue(v)
	}
	return s
}

// envBoolValue parses a truthy env value ("1/true/yes/on/enabled" enable;
// everything else incl. empty = false) — the same contract as EnvBool,
// without the process-env read (ResolveNotify already has the raw string).
func envBoolValue(v string) bool {
	return IsTrueValue(v)
}

// --- memsync peers (M4.13 delta sync) ---

// SyncPeersSettingsKey is the settings.json key carrying the memsync peer
// whitelist ("sync_peers": ["peer1", "hp"]). Same philosophy as
// compute_nodes: topology is DECLARED, never discovered — a sync peer name
// is an ssh config alias the transport connects through (`ssh <name> ...`),
// and a name not on the whitelist is never synced with.
const SyncPeersSettingsKey = "sync_peers"

// SyncPeersEnv is the env override for the peer whitelist
// (GOPI_SYNC_PEERS): a comma-separated alias list. Read in exactly one
// place — ResolveSyncPeers (Rule 2).
const SyncPeersEnv = "GOPI_SYNC_PEERS"

// ResolveSyncPeers merges the peer whitelist's sources, highest precedence
// first: CLI flags > env (GOPI_SYNC_PEERS, comma-separated) >
// settings.json "sync_peers" > none. Malformed values are ERRORS, never
// silent fallbacks (the compute_nodes convention). Empty result = sync is
// unconfigured (manual `--peer` names are then refused: undeclared peers
// must not be ssh'd by a daemon).
func ResolveSyncPeers(cliPeers []string) ([]string, error) {
	if len(cliPeers) > 0 {
		return normalizeSyncPeers(cliPeers)
	}
	if raw := strings.TrimSpace(os.Getenv(SyncPeersEnv)); raw != "" {
		parts := strings.Split(raw, ",")
		return normalizeSyncPeers(parts)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", SyncPeersSettingsKey, err)
	}
	return normalizeSyncPeers(cfg.SyncPeers)
}

// HasSyncPeers reports whether ANY sync peer is declared (settings.json
// sync_peers / GOPI_SYNC_PEERS, resolved error-silent). This is the
// SINGLE-NODE gate: a deployment with no declared peer is single-node, and
// every cross-node serve-tick pass (git sync, claim broadcast remote leg,
// terminal push) must short-circuit on this before spawning any remote work.
// An error resolving the whitelist (missing settings file, malformed value)
// is treated as "no peers" — fail-soft to the single-node default rather
// than blocking the serve pass (a config slip must degrade to local, the
// safe posture, not wedge the daemon).
func HasSyncPeers() bool {
	peers, err := ResolveSyncPeers(nil)
	return err == nil && len(peers) > 0
}

// normalizeSyncPeers validates the whitelist invariants: non-empty aliases,
// no duplicates. Returns nil for an empty list.
func normalizeSyncPeers(peers []string) ([]string, error) {
	return normalizePeerList("sync peer", peers)
}

// --- memory-asset peers (distilled_qa knowledge sync, decoupled 2026-09-02) ---

// MemoryPeersSettingsKey is the settings.json key carrying the memory-asset
// peer whitelist ("memory_peers": ["hp"]). Same declared-topology philosophy
// as sync_peers — but for the KNOWLEDGE channel only. The single-node task
// refactor cleared sync_peers (task sync disabled) while memory-asset sync
// stays a deliberately kept cross-node feature: asset sync must validate
// against memory_peers, never sync_peers, or the single-node config would
// silently kill it too.
const MemoryPeersSettingsKey = "memory_peers"

// MemoryPeersEnv is the env override for the memory-asset peer whitelist
// (GOPI_MEMORY_PEERS): a comma-separated alias list. Read in exactly one
// place — ResolveMemoryPeers (Rule 2).
const MemoryPeersEnv = "GOPI_MEMORY_PEERS"

// MemorySyncTimeoutEnv overrides the absolute bound on ONE ssh leg of an
// asset sync session, in SECONDS (>0). It lives here — the leaf constants
// package both the tools layer and the CLI can import (Rule 14) — because
// the leg's bound is resolved in internal/tools (assetSyncSSHBound, the
// single reader, Rule 2).
//
// BOTH FACES ARE LIVE AGAIN (2026-11-06). The CLI face (`--timeout`) was
// removed with the verb in 5677cc6e and this comment correctly recorded a knob
// nothing could tune; the verb was restored as a MANUAL door
// (cmd/agent/cli_memory_sync.go), so `gopi memory sync --timeout N` publishes
// into this variable and an operator may equally export it. The flag wins when
// both are set (Rule 2: one reader, assetSyncSSHBound).
//
// WHY IT EXISTS AT ALL: the leg carried this node's WHOLE local corpus and the
// peer applied every row through the MemoryStore path before answering (measured
// 2026-09-26: 5209 rows / 15 MB, 47.5s to apply), so a bound copied from the
// task-ledger delta channel SIGKILLed a healthy transfer.
const MemorySyncTimeoutEnv = "GOPI_MEMORY_SYNC_TIMEOUT_SEC"

// ResolveMemoryPeers merges the memory-asset peer whitelist's sources,
// highest precedence first: env (GOPI_MEMORY_PEERS, comma-separated) >
// settings.json "memory_peers" > none. Empty result = the knowledge channel is
// unconfigured (a `gopi memory backup --peer X` ship is then refused:
// undeclared peers must not be ssh'd).
func ResolveMemoryPeers() ([]string, error) {
	if raw := strings.TrimSpace(os.Getenv(MemoryPeersEnv)); raw != "" {
		parts := strings.Split(raw, ",")
		return normalizePeerList("memory peer", parts)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", MemoryPeersSettingsKey, err)
	}
	return normalizePeerList("memory peer", cfg.MemoryPeers)
}

// HasMemoryPeers reports whether ANY memory-asset peer is declared
// (settings.json memory_peers / GOPI_MEMORY_PEERS, resolved error-silent).
// Unlike HasSyncPeers this is NOT a single-node gate — memory-asset sync is
// a deliberately kept cross-node feature even in single-node task mode — it
// only answers "is the asset topology declared at all".
func HasMemoryPeers() bool {
	peers, err := ResolveMemoryPeers()
	return err == nil && len(peers) > 0
}

// normalizePeerList validates a peer whitelist's shared invariants:
// non-empty aliases, no duplicates. Returns nil for an empty list.
// what labels the kind in error messages ("sync peer" / "memory peer").
func normalizePeerList(what string, peers []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for i, p := range peers {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("%s #%d: name must not be empty", what, i+1)
		}
		if seen[p] {
			return nil, fmt.Errorf("%s %q: duplicate name", what, p)
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

// ResolveSettingsPath is the single resolution point for which settings.json
// file to read: GOPI_SETTINGS env override, else the default under the gopi
// data root. Empty string when no home directory exists.
func ResolveSettingsPath() string {
	if p := strings.TrimSpace(os.Getenv(SettingsPathEnv)); p != "" {
		return p
	}
	return DefaultSettingsPath()
}

// SettingsPathEnv is the environment channel of the settings-file path, named
// (Rule 1/13) because three layers spell it: this resolver, the CLI's own
// `--settings` binding and every test that points a run at a scratch profile
// (Rule 32: a test pins the environment rather than writing the operator's own
// settings file — which it could not do through a bare literal without drifting
// from this reader).
const SettingsPathEnv = "GOPI_SETTINGS"

// DefaultSettingsPath returns the default settings file
// (~/.gopi/settings.json), or "" if no home directory is found.
func DefaultSettingsPath() string {
	gd, err := GopiDir()
	if err != nil {
		return ""
	}
	return filepath.Join(gd, "settings.json")
}

// --- task verify's child wall-clock cap (verify-ops defect 2026-09-29) ---

// TaskVerifyTimeoutEnv is the env override for `gopi task verify`'s child
// wall-clock cap (GOPI_TASK_VERIFY_TIMEOUT_SEC, seconds). Read in exactly one
// place — ResolveTaskVerifyTimeout (Rule 2).
const TaskVerifyTimeoutEnv = "GOPI_TASK_VERIFY_TIMEOUT_SEC"

// TaskVerifyTimeoutSettingsKey is the settings.json key of the same knob.
const TaskVerifyTimeoutSettingsKey = "task_verify_timeout_sec"

// DefaultTaskVerifyTimeoutSec is the DEFAULT cap on `gopi task verify`'s child
// (30 min), and it is a BOUND, not a convenience: the pre-fix default was 0 =
// unlimited, so an unattended or scripted caller (a sandbox, a demo batch, CI)
// that ran `gopi task verify <id>` without a flag could be parked FOREVER by a
// child that never returns — measured 2026-09-29: the materials batch had to be
// truncated from the OUTSIDE (`timeout 40 gopi task verify …` → exit 124),
// because the verb itself carried no ceiling. 30 min is chosen to be generous
// for a real verify pass (a build plus a branch-scoped smoke set) and finite for
// everything else. An operator whose suite genuinely needs longer raises it
// through either channel; --timeout 0 remains the explicit, deliberate
// "unlimited" escape.
const DefaultTaskVerifyTimeoutSec = 30 * 60

// ResolveTaskVerifyTimeout merges the three sources for the verify child's cap,
// highest precedence first (the package's standing convention): an EXPLICIT CLI
// value > env (GOPI_TASK_VERIFY_TIMEOUT_SEC) > settings.json
// ("task_verify_timeout_sec") > DefaultTaskVerifyTimeoutSec.
//
// cliSet says the user TYPED --timeout, which a bare int cannot express: 0 is a
// meaningful value here (unlimited) and so is "absent". That distinction is the
// defect this signature exists to prevent — the pre-fix parser could not tell
// `gopi task verify <id>` (no flag) from `gopi task verify <id> --timeout 0`,
// so the default was in practice hard-wired to 0.
//
// A NEGATIVE value from any channel is an ERROR, never a silent fallback
// (Rule 15); a malformed env value is an error too, unlike the sweep knobs that
// report-and-keep — this cap decides how long a process PARKED, so guessing is
// the wrong repair.
func ResolveTaskVerifyTimeout(cliVal int, cliSet bool) (int, error) {
	if cliSet {
		if cliVal < 0 {
			return 0, fmt.Errorf("config: --timeout: negative verify timeout")
		}
		return cliVal, nil
	}
	if raw := strings.TrimSpace(os.Getenv(TaskVerifyTimeoutEnv)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("config: %s: invalid seconds %q", TaskVerifyTimeoutEnv, raw)
		}
		if n < 0 {
			return 0, fmt.Errorf("config: %s: negative verify timeout", TaskVerifyTimeoutEnv)
		}
		return n, nil
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", TaskVerifyTimeoutSettingsKey, err)
	}
	if cfg.TaskVerifyTimeoutSec > 0 {
		return cfg.TaskVerifyTimeoutSec, nil
	}
	if cfg.TaskVerifyTimeoutSec < 0 {
		return 0, fmt.Errorf("config: %s: negative verify timeout", TaskVerifyTimeoutSettingsKey)
	}
	return DefaultTaskVerifyTimeoutSec, nil
}

// TaskStallSecEnv is the env override for the heartbeat-stall grace
// (GOPI_TASK_STALL_SEC, seconds). Read in exactly one place —
// ResolveTaskStallGrace (Rule 2).
const TaskStallSecEnv = "GOPI_TASK_STALL_SEC"

// taskStallSecEnvLegacy is the pre-parameterization env spelling
// (GOPI_TASK_STALE_SEC, the M4.5 stale-threshold override). Kept as a
// fallback alias: deployments that set the old name keep working after the
// rename to GOPI_TASK_STALL_SEC. Read in exactly one place — the stall
// resolution below.
const taskStallSecEnvLegacy = "GOPI_TASK_STALE_SEC"

// TaskStallSecSettingsKey is the settings.json key carrying the heartbeat-stall
// grace ("task_stall_sec").
const TaskStallSecSettingsKey = "task_stall_sec"

// ResolveTaskStallGrace merges the sources for the ONE grace gopi still has —
// the heartbeat-stall grace — highest precedence first (the config package's
// standing convention): CLI flag > env (GOPI_TASK_STALL_SEC, with
// GOPI_TASK_STALE_SEC as the pre-rename fallback) > settings.json
// ("task_stall_sec") > default. A caller passes 0 for a CLI knob the user did
// not set (0 = "absent", the lower sources fill the gap — unlike compute_nodes,
// a NEGATIVE or malformed value is an ERROR, never a silent fallback).
//
// IT IS ONE GRACE BECAUSE IT IS ONE DECISION (2026-10-26, Rule 43 companion).
// This resolver used to return a TRIPLE — stall, max-run, and an
// auto-disposal grace derived as 2× the resolved stall — because the zombie
// sweeper consumed all three: detection, a wallclock backstop, and the delay
// before it DISPOSED what it had found. The sweeper is gone
// (devdoc/zombie-sweep-removed.md) and the surviving leg only REPORTS, so
// max-run and dispose lost their only reader. A knob nothing reads is worse
// than an absent one (Rule 21): it reads as a working control surface, and an
// operator who sets `task_dispose_sec` would conclude a disposal policy is in
// force while nothing consumes the value at all. So they were removed with the
// mechanism rather than left behind as shells.
func ResolveTaskStallGrace(cliStallSec int) (int, error) {
	return resolveGraceInt("stall", cliStallSec, TaskStallSecEnv,
		TaskStallSecSettingsKey, defaultTaskStallGraceSec)
}

// defaultTaskStallGraceSec is the heartbeat-stall default (30 min — the
// M4.5 stale threshold's value, kept as the parameterized default so
// existing behaviour is unchanged until an operator tunes it).
const defaultTaskStallGraceSec = 30 * 60

// resolveGraceInt resolves ONE grace knob across its three sources with the
// CLI>env>settings precedence (a positive CLI/env value wins; a missing
// settings key falls through to the default). Negative values are errors
// (a typo'd grace must not silently disable stale detection).
func resolveGraceInt(what string, cliVal int, envName, settingsKey string, def int) (int, error) {
	if cliVal > 0 {
		return cliVal, nil
	}
	if cliVal < 0 {
		return 0, fmt.Errorf("config: --%s: negative grace period", what)
	}
	if raw := strings.TrimSpace(os.Getenv(envName)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("config: %s: invalid seconds %q", envName, raw)
		}
		if n < 0 {
			return 0, fmt.Errorf("config: %s: negative grace period", envName)
		}
		if n > 0 {
			return n, nil
		}
		return 0, nil // explicit 0 = disabled
	}
	// Legacy spelling fallback (stall knob only): GOPI_TASK_STALE_SEC predates
	// the parameterized grace and is still honoured when the new name is
	// unset — a deployment that tunes the old env keeps its override.
	if envName == TaskStallSecEnv {
		if raw := strings.TrimSpace(os.Getenv(taskStallSecEnvLegacy)); raw != "" {
			n, err := strconv.Atoi(raw)
			if err == nil && n > 0 {
				return n, nil
			}
		}
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", settingsKey, err)
	}
	if settingsKey == TaskStallSecSettingsKey && cfg.TaskStallSec > 0 {
		return cfg.TaskStallSec, nil
	}
	return def, nil
}

// --- per-repo build contract (rebuild-after-merge epic 2026-09-02) ---

// TmpDirSettingsKey is the settings.json key carrying the temp-workspace root
// gopi and its children must use ("tmpdir": "/home/me/workspace/gopi-tmp").
// It is an ALIAS of the one definition in internal/scratch (Rule 14: the scratch
// package's refusal message and cmd/agent's injection report both name the key,
// and the package error text cannot import config), so a rename is a one-line
// change rather than a grep over prose.
//
// Deliberately NOT in the EnvVars catalog: that catalog is "every env var gopi
// READS", and gopi never reads TMPDIR — it WRITES its own copy (main.go) so the
// children inherit one. Declared, never discovered (same philosophy as
// compute_nodes/sync_peers/repos).
const TmpDirSettingsKey = scratch.TmpDirSettingsKey

// Deliberately NOT the ambient variable: TMPDIR itself is the value a CALLER
// exports (and the one gopi WRITES), so a knob named TMPDIR would be both the
// input and the output of the same mechanism. GOPI_TMPDIR is gopi's OWN
// spelling of the same knob — the name the catalog entry advertises and the one
// the readers agree on (Rule 13/14: one definition, aliased never mirrored).
//
// TmpDirFlagName is the CLI spelling of the same knob, named here so the
// catalog entry, the parser's case and the help text cannot drift (Rule 1b).
const (
	TmpDirEnvName  = scratch.GopiTmpDirEnvName
	TmpDirFlagName = "tmpdir"
)

// ScratchGCStaleMinFlagName is the CLI spelling of the scratch sweep's
// STALE-WINDOW knob (minutes; the threshold sibling of --scratch-gc, which
// carries the CADENCE). Named once here (Rule 1b) so the catalog row, the
// parser's case and the help text cannot drift, exactly like TmpDirFlagName.
const ScratchGCStaleMinFlagName = "scratch-gc-stale-min"

// ValidateScratchGCStaleMinEnv reports whether the knob's variable currently
// holds a value the sweep can honour. It exists so the REFUSAL has ONE
// definition (Rule 14) reachable from both entry points a `gopi
// --scratch-gc-stale-min …` can take: the agent path (config.applyFlag) and a
// subcommand hanging off the flag (cmd/agent's scratch-gc verb, whose flag was
// already hoisted into the environment by BootstrapEnvFromArgs before the verb
// ran — so the verb validates the VARIABLE, which is the same value either way).
//
// An empty variable is not an error: it means "nothing configured".
func ValidateScratchGCStaleMinEnv() error {
	raw := strings.TrimSpace(os.Getenv(scratch.GCStaleAgeEnvName))
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		// Rule 15: a threshold that cannot be honoured must not degrade into
		// "sweep with something else" — that is how a mistyped watchdog
		// reclaims nothing for weeks while its log reads healthy. The off
		// switch is the cadence knob, never a threshold of zero.
		return fmt.Errorf("%s=%q is not a positive number of minutes (use --%s N, set %s=N, or %s=0 to disable the sweep)",
			scratch.GCStaleAgeEnvName, raw, ScratchGCStaleMinFlagName, scratch.GCStaleAgeEnvName, scratch.GCIntervalEnvName)
	}
	return nil
}

// PublishScratchGCStaleMin resolves the scratch sweep's stale window from every
// configuration channel and publishes it into the knob's ONE environment name
// (scratch.GCStaleAgeEnvName), returning the value it published (0 = nothing
// published, the default applies).
//
// This is the knob's SINGLE PUBLISHER — the counterpart of
// ResolveTmpDirConfigured for the temp workspace, and the reason the sweep has
// NO configuration reader of its own in cmd/agent: every face (the resident
// tick, the one-shot CLI verb, the memory-base arm, internal/testutil's suite
// pin) consumes scratch.ResolveStaleAge, which reads the variable this function
// wrote. One writer, one reader (Rule 2).
//
// PRECEDENCE is the project's standing one, and each step is decided by the
// machinery that already owns it rather than a second policy here:
//
//	--scratch-gc-stale-min N  the FLAG (flagCfg.ScratchGCStaleMin ≠ 0) — wins
//	                          over everything, including the operator's own
//	                          exported environment variable
//	GOPI_TEST_SCRATCH_STALE_MIN=N   already set (exported by the caller) → left
//	                          standing, never overwritten (the tmpdir rule: gopi
//	                          never clobbers a caller's explicit choice)
//	settings.json "scratch_gc_stale_min": N   the declarative layer, published
//	                          only into an UNSET variable (settings < env)
//	default (scratch.DefaultStaleAge, 6h)     nothing configured
//
// WHY THE SETTINGS VALUE IS RESOLVED HERE AND NOT CARRIED AS AN ENV VALUE THE
// AGENT PATH READS LATER (measured on this worktree 2026-09-18): the agent path
// calls config.ApplyEnv(settingsCfg,false) — "fill only what is unset" — so a
// settings value pre-published into the variable would become indistinguishable
// from an operator export and silently OUTRANK the settings file it came from.
// One publisher, applied at the pre-chain moment the sweep's hosts (worker,
// merger, task run) actually pass through, keeps the order right everywhere.
//
// A malformed or non-positive value is REPORTED and ignored, never fatal and
// never an off switch (Rule 15): a threshold that can never be exceeded reaps
// nothing, which is the defect this knob exists to fix; the off switch is the
// cadence knob (GCIntervalEnvName=0 / the settings `scratch_gc` switch).
func PublishScratchGCStaleMin(settingsCfg, flagCfg *Config) int {
	env := os.Getenv(scratch.GCStaleAgeEnvName)
	set := func(n int, source string) int {
		if err := os.Setenv(scratch.GCStaleAgeEnvName, strconv.Itoa(n)); err != nil {
			// Rule 15b: a failed publication leaves every face on the default
			// window while the operator believes the configured one is in force.
			fmt.Fprintf(os.Stderr, "scratch-gc: cannot publish %s=%d from %s: %v\n",
				scratch.GCStaleAgeEnvName, n, source, err)
			return 0
		}
		return n
	}
	report := func(n int, source string) int {
		fmt.Fprintf(os.Stderr, "scratch-gc: stale window %d minutes from %s is not usable — using the default %v\n",
			n, source, scratch.DefaultStaleAge)
		return 0
	}
	// ① The FLAG always wins — including over the operator's own exported value.
	if flagCfg != nil && flagCfg.ScratchGCStaleMin != 0 {
		if flagCfg.ScratchGCStaleMin <= 0 {
			return report(flagCfg.ScratchGCStaleMin, ScratchGCStaleMinFlagName)
		}
		return set(flagCfg.ScratchGCStaleMin, ScratchGCStaleMinFlagName)
	}
	// ② A value already in the environment stands — whether the operator
	//    exported it or an earlier publication wrote it. Filling over it would
	//    let a settings value silently outrank the operator (Rule 2's "never
	//    clobber the caller's explicit choice", the tmpdir rule).
	if env != "" {
		return 0
	}
	// ③ settings.json, the declarative layer — published only into an UNSET
	//    variable, which is exactly the "settings < env" half of the order.
	if settingsCfg != nil && settingsCfg.ScratchGCStaleMin != 0 {
		if settingsCfg.ScratchGCStaleMin <= 0 {
			return report(settingsCfg.ScratchGCStaleMin, scratch.GCStaleAgeSettingsKey)
		}
		return set(settingsCfg.ScratchGCStaleMin, scratch.GCStaleAgeSettingsKey)
	}
	return 0 // nothing configured: the default applies, no variable written
}

// MergeTickRetiredEnvName is the RETIRED env face of the resident merge tick's
// cadence (GOPI_MERGE_TICK_MIN). The knob, its reader, its publisher and the
// worker-round call site that consumed it are GONE (2026-09-27) — the worker no
// longer merges; `gopi merge` does (devdoc/worker-merge-tick-retired.md).
//
// IT IS THE ONE SURVIVING SPELLING ON PURPOSE (Rule 1b/13/14): the retirement
// refusal in cmd/agent must be able to NAME the dead knob, and its replacement,
// in the operator's own words. A refusal that said "a retired cadence knob was
// set" would leave an operator grepping for a name nobody spells any more.
//
// IT HAS NO READER BY DESIGN, and it must never be given one: the single reader
// this constant's successor had (cmd/agent.mergeTickInterval) was deleted with
// the tick. The Rule 21 env-reader gate is satisfied the same way the other
// retired spellings on this host satisfy it — by the constant being EXEMPT for a
// stated reason at its own declaration site, never by inventing a consumer for a
// knob that must not be consumed. The sibling on the shell side is the retired
// output option in cmd/agent/cli_tasks_worker_output.go's retirement check.
const MergeTickRetiredEnvName = "GOPI_MERGE_TICK_MIN" // env-reader-exempt: retired 2026-09-27 — the reader WAS the deleted tick; a retirement refusal must name the knob and must never consume it

// MergeTickRetiredSettingsKey is the settings.json key that carried the same
// retired cadence ("merge_tick"). It is named here for the same reason as the
// env spelling beside it: the refusal must name it, and its own channel
// (the worker/merge start path) must REPORT it, because the settings decoder
// tolerates an undeclared key SILENTLY — a file still carrying this key is
// otherwise a knob nobody mentions (Rule 15). See
// TestRetiredMergeTickSettingsKeyIsTolerated for the tolerance half.
const MergeTickRetiredSettingsKey = "merge_tick"

// MergeTickRetiredReplacement names what an operator should use instead. It is
// the ONE statement of the replacement (Rule 1b: the refusal and any help text
// render from this constant rather than re-typing the sentence), and it is the
// honest answer to the question the deleted tick's own comment raised ("what
// drives the merge pass?"): nothing does, by design — `gopi task merge` is a
// foreground command an operator runs (the standalone verb went 2026-11-01).
const MergeTickRetiredReplacement = "run `gopi task merge` in the foreground — the worker does not merge"

// The RETIRED resident memory-sync tick's two spellings survive HERE, and only
// so a retirement can be SPOKEN. The tick itself is gone (2026-10-25): its
// driver spawned one cross-node ledger-sync child per declared peer every 30
// minutes — that child's verb was `gopi memory sync --peer P --push` (deleted by
// 5677cc6e on 2026-11-05, restored as a MANUAL door on 2026-11-06; what stays
// retired is the CADENCE, not the verb) — and
// each of those runs blocked the worker's round for
// 315-534s measured in ~/.gopi/logs/worker.log — a task queue that stops
// claiming for the length of a knowledge-base push. Unattended convergence is now
// the operator's own decision. The doors that moved knowledge between hosts are
// both MANUAL: the one-way COPY (`gopi memory backup`) and the converging
// session (`gopi memory sync --peer`, restored 2026-11-06). Nothing schedules
// either, so two hosts' knowledge bases diverge until a human acts
// (config.LedgerCopyForwardReplacement is the one statement of it).
//
// WHY THE NAMES STAY (the same shape the retired merge tick's pair has, Rules 2
// and 14): the settings decoder IGNORES an undeclared key silently, so a
// settings.json still carrying `memory_sync_tick` would be a knob nobody ever
// mentions. The constants are what the retirement report in
// cmd/agent/memory_sync_tick_retired.go NAMES back at that operator, and they
// are deliberately exempt from the env-reader gate for exactly that reason: a
// retirement must be able to spell the knob it refuses, and must never consume
// it. They are NOT catalogued (`internal/config/settings.go` EnvVars) and NOT
// published, because a catalogue row or a publisher would advertise a cadence
// nothing honours.
const (
	// MemorySyncTickRetiredEnvName is the RETIRED env face of the resident
	// memory-sync tick's cadence.
	MemorySyncTickRetiredEnvName = "GOPI_MEMORY_SYNC_TICK_MIN" // env-reader-exempt: retired 2026-10-25 — the reader WAS the deleted tick; a retirement report must name the knob and must never consume it

	// MemorySyncTickRetiredSettingsKey is the settings.json key that carried the
	// same cadence.
	MemorySyncTickRetiredSettingsKey = "memory_sync_tick"
)

// LedgerCopyForwardReplacement is the ONE statement of what actually survives
// for moving this node's knowledge to another host — rendered by EVERY
// retirement report that used to name a replacement (Rule 1b/14: the retired
// memory-sync tick's report and the retired store-triggered report both render
// this constant, so the two cannot drift and neither re-types a command).
//
// WHY IT REPLACES AN OLDER SENTENCE (2026-11-06, and the reason the old one was
// a defect rather than a style choice): it told an operator to run
// `gopi memory sync --peer P --push`, which at that moment had just been DELETED
// by 5677cc6e (epic cross-node-sync-removal) — the binary answered
// `gopi memory: unknown subcommand "sync"` and exited 1. A retirement report
// whose REPLACEMENT is another dead command is worse than no replacement: it
// converts "this knob does nothing" into "this knob does nothing and here is a
// new mistake to make", and the operator pays a second round trip to find out.
//
// WHAT IT SAYS NOW, AND WHY EACH CLAUSE IS IN IT: TWO doors exist and they are
// NOT interchangeable, so the sentence names both. (1) The CONVERGING one came
// back the same day this comment was written: `gopi memory sync --peer <alias>`
// (cmd/agent/cli_memory_sync.go) runs ONE unique-only session, manually — that
// is the replacement for the RETIRED SCHEDULING, which is what a retired
// cadence knob actually leaves unserved. (2) The COPYING one is
// `gopi memory backup` (internal/tools/ledger_backup.go): snapshot via SQLite's
// own VACUUM INTO, verify the artifact read-only, ship over one ssh leg to a
// `memory_peers` peer (peer side `gopi memory backup --serve`) — one-way, so it
// REPLACES the peer's ledger rather than converging with it. Naming only (1)
// would let an operator believe a cadence is back; naming only (2) would leave
// import unmentioned entirely.
const LedgerCopyForwardReplacement = "nothing is SCHEDULED — both cross-host doors are manual. To CONVERGE, run one session by hand: `gopi memory sync --peer <alias>` (bidirectional by default; `--push` or `--pull` for one direction), which imports what the peer has and exports what it lacks. To COPY, `gopi memory backup` snapshots this ledger with SQLite's own VACUUM INTO, verifies the artifact read-only and ships it over one ssh leg to a peer of the declared memory_peers topology (the peer side is `gopi memory backup --serve`) — one-way: it imports nothing, so it REPLACES the peer's knowledge base, and restoring onto a ledger is a manual `mv` onto a stopped one (`gopi memory backup -h`)"

// THE RETIRED WORKER-PULL SWITCH's two spellings survive HERE, and only so a
// retirement can be SPOKEN. The switch itself is gone (2026-11-05/11-06, batches
// 1 and 5 of the cross-node removal): its reader `config.TaskPullEnabled`, its
// resolver `resolveTaskPull`, its banner `config.TaskPullLine`, the settings key,
// the env name, the catalogue row, the CONFIG.md row and the two test files were
// all deleted, and the ONLY verb it ever let through —
// `gopi task receive --takeover` — prints a removal sentence and exits. Zero
// readers in Go: `grep -rn 'task_pull' --include=*.go internal cmd` returns
// nothing.
//
// WHY THE NAMES STAY (the shape the two retirements above established, Rules 2
// and 14): the settings decoder IGNORES an undeclared key silently, so a
// settings.json still carrying `task_pull` is a knob NOBODY ever mentions — it
// prints in `gopi config show` (that verb reports the file's own text, and it is
// RIGHT to) while nothing reads it, and the operator believes it still gates
// something (measured on this host: the operator's settings file carried
// `"task_pull": true` and every start path said nothing). The constants are what
// the retirement report in cmd/agent/retired_pull_and_sync_every_knobs.go NAMES
// back at that operator, and they are deliberately exempt from the env-reader
// gate for exactly that reason: a retirement must be able to spell the knob it
// refuses, and must never consume it. They are NOT catalogued
// (`internal/config/settings.go` EnvVars) and NOT published, because a catalogue
// row or a publisher would advertise a switch nothing honours.
const (
	// TaskPullRetiredEnvName is the RETIRED env face of the worker-pull switch.
	TaskPullRetiredEnvName = "GOPI_TASK_PULL" // env-reader-exempt: retired 2026-11-06 — the reader WAS the deleted switch (config.TaskPullEnabled, batch 5 commit d7d836e5); a retirement report must name the knob and must never consume it

	// TaskPullRetiredSettingsKey is the settings.json key that carried the same
	// switch. The struct field `TaskPull bool \`json:"task_pull,omitempty"\`` was
	// deleted with it, so the decoded Config has no way to answer "the operator
	// still sets it" — only the FILE can (config.Config.SettingsCarriesKey).
	TaskPullRetiredSettingsKey = "task_pull"
)

// TaskPullRetiredReplacement names what an operator should do instead. It is the
// ONE statement of the replacement (Rule 1b: the report renders from this
// constant rather than re-typing the sentence), and the command in it was RUN
// before it was written — `gopi task receive -h` on 2026-11-06 prints
// "gopi task receive: removed — the cross-node row legs are DELETED (2026-11-05)
// … Work that must run on another host is FILED there: `gopi task submit
// "<briefing>"` on that host, which stamps that host's own repo and ledger".
// The old text this line replaces ("ask a peer for one row with `task receive
// --takeover`") is a command that no longer runs, which is why it is not copied.
const TaskPullRetiredReplacement = "there is no pull direction any more: work that must run on another host is FILED there — run `gopi task submit \"<briefing>\"` on that host, which stamps its own repo and ledger — and a local queue that is empty simply stays empty"

// MemorySyncEveryRetiredEnvName is the RETIRED env face of the periodic
// memory-sync leg's cadence. Deleted 2026-11-06 (batch 5, commit d7d836e5)
// together with its reader `ResolveMemorySyncEvery`, its default
// `MemorySyncEveryDefaultSeconds` (300), the loop that consumed them
// (`cmd/agent/cli_memory_sync_every.go`, batch 1) and the CONFIG.md row — so the
// spelling `gopi memory sync --every <dur> --peer P` is refused by the parser
// even though the VERB itself was restored (see the replacement constant below).
//
// IT HAS NO SETTINGS-DECODER COUNTERPART, AND NONE IS INVENTED: this knob was
// ENV-ONLY for its whole life — there was never a `memory_sync_every` settings
// key, the flag `--every` was the other face, and a settings key invented here
// would be a knob that never existed in a file an operator wrote. The retirement
// therefore reports the ENV face alone (Rule 1b's corollary: a constant exists
// because a real spelling exists, not to make a pair look tidy).
const MemorySyncEveryRetiredEnvName = "GOPI_MEMORY_SYNC_EVERY_SEC" // env-reader-exempt: retired 2026-11-06 — the reader WAS ResolveMemorySyncEvery, deleted with the --every loop (batch 5 commit d7d836e5); a retirement report must name the knob and must never consume it

// MemorySyncEveryRetiredReplacement names what an operator should do instead of
// setting a cadence for an automatic leg. It is the ONE statement of the
// replacement (Rule 1b). MEASURED, and re-measured after the restoration: the
// VERB came back on 2026-11-06 (`gopi memory sync --peer <alias>`, a manual
// one-shot session), so this constant must NOT say the verb is gone — but
// `--every` did NOT come back: its reader `ResolveMemorySyncEvery`, its default
// and its loop went in batch 5 (commit `d7d836e5`), so that spelling is refused
// by the parser. What is missing is the CADENCE, not the door, and a one-shot
// `memory store` still writes this node's ledger only.
const MemorySyncEveryRetiredReplacement = "the DELETED `gopi memory sync --every` cadence has nothing left to set: its periodic loop, its reader and its 300s default went in batch 5 (commit d7d836e5), because a cadence nobody watches is what the resident tick was retired for — so do not hand that spelling to an operator. The surviving door is one invocation of `gopi memory sync --peer <alias>`, which IS one session and exits, and re-running it when you want another is the replacement. A `memory store` writes THIS node's ledger only; nothing schedules a peer session"

// ComputeNodesRetiredEnvName / ComputeNodesRetiredSettingsKey are the two
// spellings of the DELETED `compute_nodes` whitelist (the env face and the
// settings key). The knob went in `bf87a5d03` (2026-10-22) together with its
// only would-be consumer, cmd/agent/dispatch.go's runDispatchOnce, which had
// ZERO call sites — so "delete, not wire" was the ruling, and every reader,
// the Config field, the catalog row and the CONFIG.md row went with it.
//
// THE NAMES STAY HERE FOR THE SAME REASON EVERY OTHER RETIREMENT'S DO (Rules 2
// and 14): the settings decoder IGNORES an undeclared key silently, so a
// settings.json still carrying `compute_nodes` is a knob nobody ever mentions
// unless something deliberately speaks it — and until 2026-11-13 what spoke was
// the one-liner `(unrecognized key — … so it is IGNORED)`, which is true and
// useless: it cannot tell a typo from a knob this build deliberately removed.
// They are exempt from the env-reader gate for exactly that reason: a
// retirement must be able to spell the knob it refuses, and must never consume
// it.
const (
	// ComputeNodesRetiredEnvName is the RETIRED env face of the whitelist.
	ComputeNodesRetiredEnvName = "GOPI_COMPUTE_NODES" // env-reader-exempt: retired 2026-10-22 (bf87a5d03) — the reader WAS ResolveComputeNodes, deleted with the knob; a retirement report must name the knob and must never consume it

	// ComputeNodesRetiredSettingsKey is the settings.json key that carried it.
	ComputeNodesRetiredSettingsKey = "compute_nodes"
)

// ComputeNodesRetiredReplacement is what an operator should do instead. The one
// fact worth carrying is the CORRECTION, because the field most often
// misunderstood is `repo`: it was never "the default repo a submit runs in" —
// it was the git PUSH-BACK target for a dispatched run's finished branch
// (`git push <repo> HEAD`), and it only ever reached a remote node through the
// dispatcher that no longer exists. The rows are filed on the host that will
// run them now (Rule 28 / Rule 40), and each row carries its own repo in
// `deliverable.repo`.
const ComputeNodesRetiredReplacement = "nothing dispatches any more, so there is no topology to declare: work that must run on another host is FILED there — `gopi task submit \"<briefing>\"` ON that host stamps that host's own repo and ledger (Rule 28 / Rule 40) — and each row names its own repository in `deliverable.repo`. Note what this knob's `repo` field actually was: the git PUSH-BACK target for a dispatched run's finished branch (`git push <repo> HEAD`), never a default repo for `task_submit`"

// ReposSettingsKey is the settings.json key carrying the per-repo build
// table ("repos": {"/path/to/repo": {"build": "make build"}}). Declared,
// never discovered — same philosophy as compute_nodes/sync_peers.
const ReposSettingsKey = "repos"

// RepoBuildTimeoutEnv overrides the per-repo build timeout (seconds) for
// ALL repos when the repo entry has no build_timeout_sec. Read in exactly
// one place — ResolveRepoBuild (Rule 2). 0/absent = default; negative =
// no timeout.
const RepoBuildTimeoutEnv = "GOPI_REPO_BUILD_TIMEOUT_SEC"

// repoBuildTimeoutDefaultSec is the build subprocess ceiling when neither
// the repo entry nor the env override declares one.
const repoBuildTimeoutDefaultSec = 300 // 5 min

// ResolveRepoBuild returns the build contract for a repo path: the
// settings.json "repos" table entry for that path. found=false when the
// repo has NO build contract — a repo absent from the table, OR (since
// architecture-fix-v2 the repos table is ALSO the merger's sweep list) a
// merge-only repo that declares NO build command. The caller skips the
// rebuild silently for such repos (docs-only / merged-not-rebuilt; no
// error, no guess). Env/CLI do not carry a per-repo build command — the
// table is the single source (a build command is repo-specific data, not
// a knob).
func ResolveRepoBuild(repo string) (RepoConfig, bool, error) {
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return RepoConfig{}, false, fmt.Errorf("config: %s: %w", ReposSettingsKey, err)
	}
	rc, ok := cfg.Repos[repo]
	if !ok {
		return RepoConfig{}, false, nil
	}
	// Architecture-fix-v2: the repos table is ALSO the merger's sweep list —
	// an entry may be a PURE merge target (a repo the merger sweeps but does
	// not rebuild) that declares NO build command. Such an entry is a valid
	// sweep target but has no build CONTRACT → found=false, no error (the
	// caller skips the rebuild silently, exactly like an absent entry).
	if strings.TrimSpace(rc.Build) == "" {
		return RepoConfig{}, false, nil
	}
	// Timeout: repo entry > env > default. 0/absent repo entry falls
	// through to env; absent env → default; negative repo entry = no
	// timeout (explicit opt-out).
	timeout := repoBuildTimeoutDefaultSec
	if rc.BuildTimeoutSec < 0 {
		timeout = -1 // no timeout
	} else if rc.BuildTimeoutSec > 0 {
		timeout = rc.BuildTimeoutSec
	} else if raw := strings.TrimSpace(os.Getenv(RepoBuildTimeoutEnv)); raw != "" {
		n, aerr := strconv.Atoi(raw)
		if aerr != nil {
			return RepoConfig{}, false, fmt.Errorf("config: %s: invalid seconds %q", RepoBuildTimeoutEnv, raw)
		}
		if n < 0 {
			timeout = -1
		} else if n > 0 {
			timeout = n
		}
	}
	rc.BuildTimeoutSec = timeout
	return rc, true, nil
}

// ResolveRepoWorktreeRoot returns the worktree root a repo's tasks must use:
// the ambient override when set (worktreeroots.Resolve reads it — ONE reader,
// Rule 2), else this repo's declared `worktree_root`, else the shared default
// <home>/workspace.
//
// A repo ABSENT from the repos table is NOT an error: it gets the default root,
// because the default is the documented behaviour and the table is a place to
// declare an EXCEPTION. This is deliberately unlike ResolveRepoBuild, where an
// absent entry means "no contract, skip" — a build that nobody declared must
// not be invented, while a root that nobody declared has a defined default.
//
// An unreadable/invalid settings file IS an error and is propagated: falling
// back to the default root on a broken settings file would silently authorise
// worktrees in a place the operator may have deliberately moved away from.
func ResolveRepoWorktreeRoot(repo string) (worktreeroots.Root, error) {
	declared := ""
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// The env override needs no settings file, so a broken settings file
		// must not defeat an explicit sandbox pin; but it IS reported whenever
		// it is what decided the answer.
		if root, rerr := worktreeroots.Resolve(""); rerr == nil && root.Source != worktreeroots.SourceDefault {
			return root, nil
		}
		return worktreeroots.Root{}, fmt.Errorf("config: %s: %w", ReposSettingsKey, err)
	}
	if rc, ok := cfg.Repos[repo]; ok {
		declared = rc.WorktreeRoot
	}
	root, err := worktreeroots.Resolve(declared)
	if err != nil {
		return worktreeroots.Root{}, fmt.Errorf("config: %s[%s].%s: %w", ReposSettingsKey, repo, worktreeroots.WorktreeRootSettingsKey, err)
	}
	return root, nil
}

// ResolveMergerRepos returns the ordered list of repo paths the merger
// sweeps each tick — the settings.json "repos" table keys (epic
// architecture-fix-v2, config-driven pure git sweep, 2026-09-03). Each
// entry is a repo the merger cd's to and sweeps every tick (merge clean
// unprotected branches / dispatch conflicts on the normal task queue,
// then rebuild its declared build if it has one). The map's keys are the
// sweep list; the build contract is optional (a repo with no build
// command is still merged, just not rebuilt). Deterministic order (sorted)
// so a single-instance daemon's per-repo sweep is reproducible across
// restarts. No repos table / empty = no sweep targets (the merger no-ops).
func ResolveMergerRepos() ([]string, error) {
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", ReposSettingsKey, err)
	}
	names := make([]string, 0, len(cfg.Repos))
	for path := range cfg.Repos {
		if strings.TrimSpace(path) == "" {
			continue
		}
		names = append(names, path)
	}
	sort.Strings(names)
	return names, nil
}
