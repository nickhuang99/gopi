package config

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// ExecveMaxStringBytes is the kernel's ceiling on ONE string in an execve
// argument or environment block — the parameter Linux calls MAX_ARG_STRLEN
// (`fs/exec.c`: `32 * PAGE_SIZE`, i.e. 131072 on a 4 KiB-page build). It is a
// PER-STRING limit, independent of the much larger total (ARG_MAX on this host
// is 2097152), which is why one long value is enough to break every spawn.
//
// The domain is `NAME=value` NUL-terminated — the form the kernel counts — so
// the budget for a VALUE is this minus the name, the '=' and a NUL.
const ExecveMaxStringBytes = 131072

// The two bytes a `NAME=value` environment entry spends besides the name and
// the value: the '=' separator and the terminating NUL (named because the
// arithmetic below is a LIMIT calculation, not a magic number, Rule 1).
const (
	envEntrySeparatorBytes = 1
	envEntryNulBytes       = 1
)

// EnvEntryOverflow reports whether an environment entry `name=value` is longer
// than the kernel will accept, i.e. whether publishing it would make EVERY
// subsequent fork/exec in this process fail E2BIG ("argument list too long").
//
// Exported so a spawner or a test can ask the same question the publisher asks
// (Rule 14: one criterion, not a second arithmetic).
func EnvEntryOverflow(name, value string) bool {
	return len(name)+envEntrySeparatorBytes+len(value)+envEntryNulBytes > ExecveMaxStringBytes
}

// processOnlyPrompt carries a prompt that is TOO LONG to be published into the
// environment, so the run still sees it after the publication is skipped (Rule
// 15: a skip is announced, never a silent drop).
//
// WHY IN-PROCESS AND NOT A FILE: the value already exists in this process (it
// was read from --prompt/`@file` or from the settings document); handing it to
// the run needs no second artifact, no cleanup, and no dependency (Rule 35).
// Children do not need it — every consumer of GOPI_PROMPT is this very process's
// run path — which is exactly why not publishing it costs nothing.
var processOnlyPrompt struct {
	mu    sync.Mutex
	value string
}

// promptCapWarnOut is where the oversized-prompt diagnostic goes. A variable
// (like ambiguousWidthWarnOut beside it) so a test can capture the sentence
// without capturing the whole process's stderr.
var promptCapWarnOut io.Writer = os.Stderr

// promptCapWarnFmt is the ONE sentence for the skip, and it carries the NUMBERS
// (Rule 15: an announced skip names what it measured — "too long" without a
// byte count leaves the operator unable to act).
//
// IT DELIBERATELY DOES NOT QUOTE THE KERNEL'S MESSAGE. The failure this skip
// prevents is E2BIG, whose strerror text is the one phrase that appears in every
// log a reader greps to decide whether a run survived — quoting it here would
// make the diagnostic ITSELF look like the failure (measured: the first wording
// of this sentence put the phrase into a run whose children all spawned fine, so
// `grep -c` on the run's log could not answer the question it exists to answer).
// The refusal is named by its E2BIG class and the failure is described, never
// reproduced.
const promptCapWarnFmt = "[gopi] config: the task prompt (%d bytes; %d bytes as the %s " +
	"environment entry) exceeds the kernel's %d-byte per-string execve limit " +
	"(MAX_ARG_STRLEN) — it is NOT published into the environment, because one " +
	"string that long makes EVERY child this process spawns fail at exec with " +
	"the kernel's E2BIG PER-STRING overflow refusal (git rev-parse for the clone " +
	"channel's ground and its anchor gate; sh for notify.sh and the task-row " +
	"writeback), which turns a large prompt into a runtime that cannot spawn " +
	"anything. The prompt still reaches THIS run in-process " +
	"(config.ProcessOnlyPrompt); pass a file reference (--prompt @file, which " +
	"the run reads itself) to give children a clean environment.\n"

// ProcessOnlyPrompt returns the prompt that could not be published into the
// environment, or "" when the publication carried it. It is the READER side of
// the skip below, and the run path consults it exactly where it consults the
// variable (cmd/agent's readInput) — one publisher, one reader (Rule 2).
func ProcessOnlyPrompt() string {
	processOnlyPrompt.mu.Lock()
	defer processOnlyPrompt.mu.Unlock()
	return processOnlyPrompt.value
}

// setProcessOnlyPrompt records (or clears) the in-process carrier.
func setProcessOnlyPrompt(v string) {
	processOnlyPrompt.mu.Lock()
	defer processOnlyPrompt.mu.Unlock()
	processOnlyPrompt.value = v
}

// publishPrompt is the prompt's ONE publication point (Rule 2). A value the
// environment can carry is published exactly as before, and the in-process
// carrier is cleared so the variable stays the single channel for it. A value it
// CANNOT carry is kept in-process and announced with its bytes and the limit —
// never published, because publishing it is what breaks every child.
//
// `set` is ApplyEnv's own writer (the precedence funnel: flags win, settings
// only fill), passed in rather than duplicated, so this function can never
// publish through a second, subtly different path.
func publishPrompt(set func(env, value string), value string) {
	if value == "" {
		return
	}
	if EnvEntryOverflow(PromptEnvVar, value) {
		setProcessOnlyPrompt(value)
		// Structure-preserving: the '=' and the NUL are counted, so the number
		// printed is the entry the kernel would have refused.
		entryBytes := len(PromptEnvVar) + envEntrySeparatorBytes + len(value) + envEntryNulBytes
		// deliberately ignored: a diagnostic that cannot be written must not
		// abort the run it describes (the prompt is already carried in-process).
		_, _ = fmt.Fprintf(promptCapWarnOut, promptCapWarnFmt,
			len(value), entryBytes, PromptEnvVar, ExecveMaxStringBytes)
		return
	}
	setProcessOnlyPrompt("")
	set(PromptEnvVar, value)
}

// EnvBoolOn is the canonical ON value of every boolean env knob. It exists so
// a SPAWNER that must arm a knob for a child (or a test asserting the value a
// child received) references ONE definition of "on" instead of re-typing "1"
// (Rule 1b/14) — the domain is EnvBool's truthy set, and this spelling is the
// one ApplyEnv writes and the one the operator documentation shows.
const EnvBoolOn = "1"

// EnvBool reports whether an enabling "on" env flag is set. It accepts the
// documented truthy values ("1", "on", "yes", "true", "enabled") and treats
// everything else — including "0", "off", "false", "no", and the empty string
// — as DISABLED. This is the SINGLE SOURCE OF TRUTH for boolean ENV parsing
// (PROJECT.md Rule 2): agent.boolEnvTrue and deepseek's debugLog reader both
// delegate here.
//
// The SPELLINGS themselves are not defined here: they live in the
// internal/boolvocab leaf package, which every other channel of the same
// domain (this package's raw-string readers, internal/agent's env gates,
// internal/tools' default-ON switches) imports rather than re-spelling
// (PROJECT.md Rule 14 — one definition, no mirror). The vocabulary is a leaf for
// the reason it drifted six ways before: internal/heavytest must not reach
// this package, and a consumer that cannot import the definition mirrors it.
//
// Why not `os.Getenv(...) != ""`: ApplyEnv force-writes the canonical "off"
// for disabled switches (e.g. GOPI_DEBUG=off whenever --debug is absent), so
// a non-empty check treats that literal "off" as ENABLED — a real regression
// seen twice (agent debug dump flooding stderr 2026-08-29; deepseek retry
// logging 2026-08-30). Truthy-only parsing makes the env channel safe
// regardless of how the value got there.
func EnvBool(name string) bool {
	return IsTrueValue(os.Getenv(name))
}

// IsTrueValue reports whether a raw value carries one of the ENABLING spellings
// (boolvocab.TrueValues). Exported so callers that already HOLD a raw string —
// a settings-file value, a JSON field, a test's synthetic environment — check
// it without re-reading the process env or re-spelling the list (Rule 14).
func IsTrueValue(raw string) bool {
	return boolvocab.IsTrue(raw)
}

// IsFalseValue reports whether a raw value carries one of the DISABLING
// spellings (boolvocab.FalseValues). It is the falsey counterpart of
// IsTrueValue, for the default-ON switches: `!IsFalseValue(v)` is the
// convention every "off" vocabulary in internal/agent and internal/tools
// follows.
func IsFalseValue(raw string) bool {
	return boolvocab.IsFalse(raw)
}

// ApplyEnv pushes the values in cfg into the process environment so the
// existing per-file os.Getenv parses in internal/agent and internal/tools pick
// them up unchanged (Rule 2: each env var is still read in exactly one place;
// we don't duplicate the parsing, only the feeding).
//
// Precedence, highest→lowest: flags > env > settings > default.
// Because env is already in the process, injection cannot blindly override it
// or flags would LOSE to a developer's shell-exported variable. So:
//
//	ApplyEnv(settingsCfg, false)  // only fill env vars NOT already set
//	ApplyEnv(flagCfg,    true)   // flags always win over env
//
// Boolean knobs are emitted canonically ("1" / "off") that the existing
// readers already accept (they accept "1", "enabled", "off", "false", ...).
func ApplyEnv(cfg *Config, fromFlags bool) {
	set := func(env, value string) {
		if fromFlags {
			// Only force-write a flag override if the user actually passed that
			// flag. An unpassed plain bool/int knob is a zero value in cfg and
			// must NOT be pushed into the process env — the downstream env
			// readers treat a non-empty value (e.g. GOPI_DEBUG="off") as an
			// explicit setting, which flooded stderr with the full request log.
			if cfg.SetEnvVars != nil && !cfg.SetEnvVars[env] {
				return
			}
			os.Setenv(env, value)
			return
		}
		if v, present := os.LookupEnv(env); !present || v == "" {
			os.Setenv(env, value)
		}
	}

	// Strings — only set when non-empty (empty CLI/settings value means unset,
	// letting the built-in default win).
	setStr := func(env, v string) {
		if v != "" {
			set(env, v)
		}
	}
	// Integers — set when >0 (0 means "use code default") or when explicitly
	// given on the CLI (fromFlags), so `--timeout 0` (unlimited) is honoured.
	setInt := func(env string, n int) {
		if n > 0 || (fromFlags && n == 0) {
			set(env, strconv.Itoa(n))
		}
	}
	setDur := func(env string, sec int) {
		if sec > 0 || (fromFlags && sec >= 0) {
			set(env, strconv.Itoa(sec))
		}
	}
	// Pointer bools (nil = unset).
	setBoolPtr := func(env string, b *bool) {
		if b == nil {
			return
		}
		if *b {
			set(env, "1")
		} else {
			set(env, "off")
		}
	}
	// setThinkingMode feeds the canonical "enabled"/"disabled" strings the
	// agent's GOPI_THINKING switch parses verbatim (a bool knob would emit
	// "1"/"off", which the switch would reject as invalid).
	setThinkingMode := func(env string, m *ThinkingMode) {
		if m == nil {
			return
		}
		set(env, string(*m))
	}
	// Plain bools — only propagate from settings when true (a config file
	// can flip a switch on but not globally safe-flag others); from flags the
	// explicit value always wins.
	setBool := func(env string, b bool) {
		if fromFlags || b {
			if b {
				set(env, "1")
			} else {
				set(env, "off")
			}
		}
	}

	// Gopi data root — must be injected BEFORE anything else reads it
	// (every GopiDir() call site resolves the profile through it).
	setStr("GOPI_HOME", cfg.GopiHomePath)

	// Endpoint (higher-priority Anthropic_* first, then parallel DeepSeek_*
	// fallbacks so a settings-only key reaches the reader).
	setStr("ANTHROPIC_AUTH_TOKEN", cfg.APIKey)
	setStr("ANTHROPIC_BASE_URL", cfg.BaseURL)
	setStr("ANTHROPIC_MODEL", cfg.Model)
	setStr("DEEPSEEK_API_KEY", cfg.APIKey)
	setStr("DEEPSEEK_BASE_URL", cfg.BaseURL)
	setStr("DEEPSEEK_MODEL", cfg.Model)

	// Vision (multimodal image understanding) — separate endpoint knobs.
	setStr("GOPI_VISION_MODEL", cfg.VisionModel)
	setStr("GOPI_VISION_BASE_URL", cfg.VisionBaseURL)

	// Reply language: the settings-file channel reaches the ONE resolver
	// (config.resolveOutputLanguage) through the same env representation the
	// CLI flag and an operator export use (Rule 2 — one reader). Applied only
	// when non-empty, so an unset settings key never writes the variable and
	// can therefore never outrank a caller's own export (the fill-only trap
	// the tmpdir / merge-tick publications around it also avoid).
	setStr(OutputLanguageEnv, cfg.OutputLanguage)

	// The chat surface's ideal geometry, applied through the SAME
	// representation the flag and an operator export use, so ONE reader
	// (config.resolveChatIdealGeometry) sees all three channels (Rule 2). Fill
	// only when non-empty, so an unset settings key can never outrank a
	// caller's own export.
	setStr(ChatIdealGeometryEnv, cfg.ChatIdealGeometry)

	// The chat_surface tool's self-driven turn budget, through the SAME
	// representation the flag and an operator export use, so ONE reader
	// (config.resolveChatSurfaceTurns) sees all three channels (Rule 2). Fill
	// only when non-empty, so an unset settings key can never outrank a
	// caller's own export.
	setStr(ChatSurfaceTurnsEnv, cfg.ChatSurfaceTurns)

	// The A-class width reading, applied through the SAME representation the
	// flag and an operator export use, so ONE reader
	// (config.ResolveAmbiguousWidthMode) sees all three channels (Rule 2).
	setStr(AmbiguousWidthEnv, cfg.AmbiguousWidth)

	// Capture client's configured default device (tier 2 of the device policy).
	setStr(VoiceDeviceEnv, cfg.DefaultDevice)

	// Second voice-input engine (doubao/Volcengine cloud). The selection itself
	// (default recording) and the key file path are ordinary string knobs; the key
	// VALUE never enters the environment (applyFlag reads it from cfg only when
	// the operator named a KEY FILE).
	setStr(AudioEngineEnv, cfg.AudioEngine)
	setStr(AudioKeyFileEnv, cfg.AudioEngineParam.Recording.KeyFile)
	setStr(AudioResourceIDEnv, cfg.AudioEngineParam.Recording.ResourceID)
	setStr(AudioHotwordsEnv, cfg.AudioEngineParam.Recording.Hotwords)
	// The two ENDPOINTS are ordinary addresses, not secrets: publishing them
	// follows the same shape as the knobs above, so a child gopi process and
	// the CLI's own resolution cannot disagree about which host is in use.
	setStr(AudioAPIBaseURLEnv, cfg.AudioEngineParam.Recording.APIBaseURL)
	setStr(AudioStreamEndpointEnv, cfg.AudioEngineParam.Streaming.Endpoint)
	// The LOCAL engine's two deployment paths, published for the same reason and
	// with the same shape as the knobs above: a fleet pins them once, in one
	// settings document, and every child resolves the same deployment. They are
	// PATHS, not secrets.
	setStr(AudioLocalModelsEnv, cfg.AudioEngineParam.Local.ModelsDir)
	setStr(AudioLocalRuntimeEnv, cfg.AudioEngineParam.Local.RuntimeBin)
	// DoubaoAPIKey / DoubaoAppID / DoubaoToken are deliberately ABSENT here, and
	// that absence is the rule rather than an oversight: this environment is
	// inherited by every child gopi spawns (the bash tool, the task executor),
	// so republishing a credential would create a second, wider copy of it than
	// the 0600 settings document it was read from. Their env channels are READ
	// when an operator exports them by hand — see each key's own comment.

	// Input framing. The prompt goes through publishPrompt, NOT setStr: it is
	// the one knob whose VALUE can be larger than the kernel will carry, and
	// publishing such a value is what breaks every child (see the function).
	publishPrompt(set, cfg.Prompt)

	// Thinking / reasoning
	setStr("GOPI_REASONING_EFFORT", cfg.ReasoningEffort)
	// effort_values: the provider's own legal effort list (settings schema,
	// data not code) — joined as a comma-separated GOPI_EFFORT_VALUES env
	// var; internal/agent validates the requested effort against it. A nil
	// list sets nothing → the reader falls back to the DeepSeek baseline.
	if len(cfg.EffortValues) > 0 {
		set("GOPI_EFFORT_VALUES", strings.Join(cfg.EffortValues, ","))
	}
	setInt("GOPI_THINKING_BUDGET", cfg.ThinkingBudget)
	setInt("GOPI_MAX_TOKENS", cfg.MaxTokens)
	setInt("GOPI_CONTEXT_WINDOW", cfg.ContextWindow)
	setThinkingMode("GOPI_THINKING", cfg.Thinking)
	setBool("GOPI_HIDE_THINKING", cfg.HideThinking)
	setBool("GOPI_KEEP_THINKING", cfg.KeepThinking)

	// Runtime
	setDur("GOPI_TIMEOUT_SEC", cfg.TimeoutSec)
	if cfg.Compact != nil {
		b := bool(*cfg.Compact)
		setBoolPtr("GOPI_COMPACT", &b)
	}
	setBool("GOPI_DEBUG", cfg.Debug)

	// Compaction
	if cfg.CompactTrigger != nil {
		set("GOPI_COMPACT_TRIGGER", strconv.FormatFloat(*cfg.CompactTrigger, 'f', -1, 64))
	}
	if cfg.CompactTarget != nil {
		set("GOPI_COMPACT_TARGET", strconv.FormatFloat(*cfg.CompactTarget, 'f', -1, 64))
	}
	setInt("GOPI_COMPACT_KEEP_RECENT", cfg.CompactKeepRecent)
	setInt("GOPI_COMPACT_MAX_SUMMARY_TOKENS", cfg.CompactMaxSummaryToks)
	setDur("GOPI_COMPACT_COOLDOWN", cfg.CompactCooldownSec)
	setBoolPtr("GOPI_COMPACT_REFERENCE_ONLY", cfg.CompactReferenceOnly)

	// Logging
	setStr("GOPI_RAW_LOG", cfg.RawLog)
	setStr("GOPI_RAW_LOG_DIR", cfg.RawLogDir)

	// Tools
	setInt("GOPI_TOOL_TIMEOUT_SEC", cfg.ToolTimeoutSec)
	setInt("GOPI_BASH_TIMEOUT_SEC", cfg.BashTimeoutSec)
	setInt("GOPI_BASH_IDLE_SEC", cfg.BashIdleSec)
	setBool("GOPI_ALLOW_DANGEROUS", cfg.AllowDangerous)
	setStr(WebSearchScriptsDirEnvName, cfg.WebSearchScripts)
	setStr("GOPI_MEMORY_DB", cfg.MemoryDB)
	setBool("GOPI_NO_ENFORCE_MEMORY", cfg.NoEnforceMemory)
	setBool("GOPI_NO_ENFORCE_MEMORY_CONSULT", cfg.NoEnforceMemoryConsult)

	// Resident scratch sweep: the settings switch only ever turns the sweep
	// OFF. OFF is published to the process as
	// GOPI_TEST_SCRATCH_GC_MIN=0 — the same env the cadence reader consumes
	// (scratch.GCIntervalEnvName), so there is exactly ONE reader and ONE
	// meaning for the knob (Rule 2/13: a switch that only exists in the
	// settings file would be a second, silently-diverging source of truth).
	if cfg.ScratchGC != nil && !*cfg.ScratchGC {
		set(scratch.GCIntervalEnvName, "0")
	}
}
