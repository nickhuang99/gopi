# GoPi Configuration — the central reference

One page for **every** way gopi is configured: the files it reads, the
environment variables it honours, the CLI flags that override them, and the
rules by which a value wins.

This document is derived from the source, not from memory: every table row
names the file and line where the value is *read* (or the single resolution
function that owns it). If a row here disagrees with the code, the code wins —
and the disagreement is a bug in this file (see [Keeping this page
honest](#keeping-this-page-honest)).

- Rendered twin: `gopi --help-env` prints the same env / settings-key / flag
  triples (generated from the catalog, `internal/config/settings.go:609`).
- Operator-facing twin: `gopi -h` (`internal/config/help.go:520`).
- Anchor: the task-dispatch briefing for this doc is the epic "gopi config
  settings is not fully documented … a fully centralized doc like CONFIG.md".

---

## 1. The four channels and their precedence

Every tuning knob can be expressed **three interchangeable ways**, and always
resolves in this order (`internal/config/env.go:75`, `internal/config/env.go:75`):

| rank | channel | example | where it is applied |
|---|---|---|---|
| 1 (highest) | **CLI flag** | `--timeout 300` | `config.FlagOverrides` (`internal/config/flags.go:55`) |
| 2 | **environment variable** | `GOPI_TIMEOUT_SEC=300` | already in the process env |
| 3 | **settings.json key** | `"timeout": 300` | `config.FromSettingsFile` (`internal/config/settings.go:482`) |
| 4 (lowest) | **built-in default** | `0` = unlimited | the reader's named constant |

The mechanics, exactly as implemented:

1. `FlagOverrides(args)` parses the command line into a `*Config` of
   *explicitly passed* overrides. It also records, per flag actually seen, the
   env key it owns in `Config.SetEnvVars` (`internal/config/config.go:264`).
   Unknown flags are a hard error — never silently ignored.
2. The settings file is read (`FromSettingsFile`); a **missing file is not an
   error** (all defaults apply), a **malformed file is a hard exit** naming the
   path and the JSON problem (`cmd/agent/main.go:737`).
3. `config.ApplyEnv(base, false)` pushes the settings values into the process
   environment **only for variables that are unset or empty** — so a variable
   the operator already exported (or a flag already wrote) is never clobbered.
4. `config.ApplyEnv(overrides, true)` pushes **only the flags the user actually
   passed** (`SetEnvVars`), unconditionally.

Step 4's `SetEnvVars` guard is not a detail — it is the fix for a real
regression in which a bare `gopi <prompt>` force-wrote `GOPI_DEBUG=off` and
every other unpassed knob's zero value into the env, and a literal `"off"` read
as *enabled* by a `os.Getenv(...) != ""` check flooded stderr with the entire
request log (`internal/config/env.go:83`).

### 1a. Boolean knobs are truthy-only

`config.EnvBool` (`internal/config/env.go:40`) is the single source of truth:
**enabling** spellings are `1, true, on, yes, enabled`; **every other value —
including `0`, `off`, `false`, `no`, and the empty string — is disabled**
(`internal/boolvocab/boolvocab.go`). This is why the settings/flag channel can
emit a literal `off` without inverting the switch.

A few older readers still accept their own vocabulary (`GOPI_RAW_LOG=OFF` is
compared case-insensitively against `"OFF"`, `internal/agent/rawlog.go:82`);
where a knob is listed below, its accepted spellings are stated.

### 1b. Durations are integer units, never Go duration strings

Every duration knob is carried as an **integer** — seconds for the runtime and
task knobs, minutes for the scratch-sweep knobs, days nowhere. A Go
`time.Duration` JSON field would decode `"timeout": 300` as 300 nanoseconds;
that is why the settings model uses `TimeoutSec int` / `ScratchGCStaleMin int`
(`internal/config/config.go:226`). The unit of each knob is stated in its row.

### 1c. Precedence exceptions — knobs that rank their own channels

Three knobs deliberately do **not** go through the plain `ApplyEnv` fill loop,
because their *input* name would collide with their *output* value. Each has
exactly one resolution function that spells the order:

| knob | resolved by | order (highest first) |
|---|---|---|
| temp workspace (`tmpdir`) | `config.ResolveTmpDirConfigured` (`internal/config/tmpdir.go:79`) | `--tmpdir` → a caller-exported `TMPDIR` → `GOPI_TMPDIR` → settings `tmpdir` → default `<home>/workspace/_tmp` |
| agent sandbox root (`sandbox_dir`) | `config.ResolveSandboxRootConfigured` (`internal/config/sandbox.go:207`) | `--sandbox-dir` → `GOPI_SANDBOX_DIR` → settings `sandbox_dir` → default `<home>/workspace/gopi-sandbox` (the `gopi tool sandbox` verb accepts `--sandbox-dir` itself and routes it through THIS function — one precedence, one resolution; no ambient channel: nothing outside gopi reads a sandbox root) |
| web-search plugin scripts (`websearch_scripts_dir`) | `config.WebSearchScriptsDir` (`internal/config/plugins.go:93`) | `GOPI_WEBSEARCH_SCRIPTS_DIR` → default `<gopi dir>/plugins/websearch/scripts`. NO settings/flag channel is applied at this reader: the settings key and `--websearch-scripts-dir` reach it by being published into the environment as the same variable (`config.ApplyEnv`, `internal/config/env.go:75`), so there is exactly ONE reader of the name and the sandbox creator links the same tree the tool reads |
| scratch stale window | `config.PublishScratchGCStaleMin` (`internal/config/config.go:1253`) | `--scratch-gc-stale-min` → `GOPI_TEST_SCRATCH_STALE_MIN` → settings `scratch_gc_stale_min` → default 360 min |
| toolchain build cache bound | `scratch.ResolveToolchainCacheMaxBytes` (`internal/scratch/toolchain_cache.go:358`) | `gopi tool scratch-gc gocache --max-gib N` → `GOPI_BUILD_CACHE_MAX_GIB` → default `25` GiB (`scratch.DefaultToolchainCacheMaxBytes`) |
| notify (`notify.*`) | `config.ResolveNotify` (`internal/config/config.go:766`) | an env var that is **set (even to "")** → settings `notify.*` → notify disabled |

The notify case is per-knob: `GOPI_TASK_NOTIFY_CMD=""` *disables* the command
even when settings declares one — the operator's explicit empty value wins.

---

## 2. The settings file

### 2a. Which file is read

Resolution order (`deepseek.SettingsPath`, `internal/deepseek/config.go:39`;
identical to `config.ResolveSettingsPath`, `internal/config/config.go:971`):

1. `--settings PATH` on the command line
2. `GOPI_SETTINGS` environment variable
3. `<gopi data root>/settings.json` — i.e. `$GOPI_HOME/.gopi/settings.json`,
   or `$HOME/.gopi/settings.json`

On this host `~/.gopi/settings.json` is a **symlink** (`.ds_hp_sync_settings.json`);
edit the target, and when a tool writes it, follow the link
(`follow_symlink=true` in `edit`/`write`).

`--home PATH` relocates the whole data root **and the settings read**
(`cmd/agent/main.go:563` injects `GOPI_HOME` *before* the settings path is
resolved). The relative order matters: resolving the path first was a real bug
where `--home X` silently loaded the real `~/.gopi/settings.json`, including a
live `api_key`.

Unknown keys are **tolerated silently** — a settings file that still carries a
removed key (e.g. `peak_hours`) keeps loading and the key is ignored
(`internal/config/config.go:278`).

### 2b. The gopi data root

`GOPI_HOME` (or `--home`) replaces `~` in the profile path: the data root
becomes `$GOPI_HOME/.gopi` (`config.GopiDir` → `config.ProfileDirIn`, `internal/config/gopihome.go:58`).
Everything profile-scoped resolves under it: `settings.json`, `sessions/`,
`raw/`, `memory.db`, `plugins/`, `tasks.db`, `logs/`, `worker/`, `merger/`.

A fresh data root is provisioned by **`gopi config setup`** — one idempotent run
that materialises everything (profile root, `settings.json` as `{}` when absent,
`sessions/`, and the ledger with its schema) — see `devdoc/deployment-setup-verb.md`.
Without it, everything
except `settings.json` already self-provisions on first use, so the verb's real
value is the explicit run-ready contract and the discoverable file to edit.

### 2c. Every settings key

Rows marked **flag** carry a CLI flag of the same knob (see §3); `—` means the
knob is reachable only through env/settings. "read at" is the production site
that consumes the value.

The distinction is not cosmetic, and `gopi -h` words its claim to match it: the
env-name-as-flag escape hatch (`--gopi-timeout-sec` ≡ `--timeout`) covers the
**flag-bearing rows only**. For a `—` row the env-name spelling parses to no
flag, and the refusal says so by NAME — `--gopi-effort-values names
GOPI_EFFORT_VALUES — a real GoPi knob with NO CLI flag of its own (…) Reach it
through its environment variable … or its settings.json key "…"` — never the bare
`unknown flag`, which would send the operator hunting a typo that does not exist.
The claim and the behaviour are gated together by
`internal/config/help_env_claim_test.go` over the whole catalog, so a new row
cannot drift them apart.

#### Endpoint (DeepSeek / Anthropic-compatible)

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `api_key` | `ANTHROPIC_AUTH_TOKEN`, then `DEEPSEEK_API_KEY` | `--api-key` | API credential. Unset → hard error at client construction | `internal/deepseek/claude_client.go:648` |
| `base_url` | `ANTHROPIC_BASE_URL`, then `DEEPSEEK_BASE_URL` | `--base-url` | Endpoint root; the client appends `/messages` per prefix rules. Default `https://api.deepseek.com` | `internal/deepseek/claude_client.go:666` |
| `model` | `ANTHROPIC_MODEL`, then `DEEPSEEK_MODEL` | `--model` | Model name. Code fallback `deepseek-v4-flash`. **The flag writes BOTH env names** so a shell-exported `ANTHROPIC_MODEL` cannot beat it | `internal/deepseek/config.go:84` |
| `vision_model` | `GOPI_VISION_MODEL` | `--vision-model` | Multimodal model for `vision_analyze`. Default `deepseek-v4-flash-vision-exp` | `internal/deepseek/claude_client.go:731` |
| `vision_base_url` | `GOPI_VISION_BASE_URL` | `--vision-base-url` | Vision endpoint. Default `https://api.deepseek.com/anthropic` | `internal/deepseek/claude_client.go:718` |
| — | `GOPI_TOKENS_COUNT_URL` | — | `gopi tool tokens --compare` measurement endpoint: an Anthropic-compatible `/v1/messages/count_tokens` on the host the measured requests go to. It answers with the **server's own** `input_tokens` for the exact bytes sent, which is the ground truth the tool exists to measure against; unset means "no server number", reported as such — the tool never substitutes a local estimate for the server's count | `internal/config/settings.go:102` |
| — | `GOPI_TOKENS_COUNT_KEY` | — | The credential that endpoint is called with. It rides the same narrow channel as the other inline credentials (the process environment is inherited by every child gopi spawns, so settings.json is the narrower one); an empty key is sent as an empty `Authorization` header, so a local endpoint needing no credential works with neither set | `internal/config/settings.go:110` |

`messages_path` is a **settings-only** knob with no env var and no flag
(`internal/deepseek/config.go:23`): when set, the client posts to exactly
`{base_url}{messages_path}` instead of guessing the `/anthropic` vs `/v1`
suffix. This is how a non-DeepSeek Anthropic-compatible provider is enabled
with zero code.

#### Data root, settings path, prompt

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `gopi_home` | `GOPI_HOME` | `--home` | Data root override; `~/.gopi` becomes `$GOPI_HOME/.gopi` (the join is `config.ProfileDirIn`, so a creator and a reader cannot disagree). A `gopi tool sandbox create` directory is exactly such a home: its profile root `<sandbox>/.gopi` is materialised by the initializer, which is why `GOPI_HOME="$SB" gopi session log --last` lists the sandbox's own sessions **A value whose LEAF is the profile leaf (`.gopi`) is refused** (exit 2, before any verb dispatch and before anything is materialised): it names a PROFILE, not a home, and the one arithmetic above turns it into `<value>/.gopi/.gopi` — a SECOND profile holding its own ledger, `sessions/` and plugins, which no reader of the profile the operator meant ever sees. Two measured spellings, one predicate (the leaf, whatever the parent): `GOPI_HOME=<sandbox>/.gopi` (the sandbox's own profile path, as `gopi tool sandbox -h` prints it — no `.sandbox-owner.json` there, so `SweepSandboxes`, which sweeps a sandbox ROOT's children, never sees it, while the sandbox's own ledger and `sessions/` stay empty) and `GOPI_HOME=<home>/.gopi` for an ORDINARY home with no sandbox anywhere in the path (measured 2026-10-13 on host-b: it resolved to `<home>/.gopi/.gopi`, 2 ledger rows against the real 1376, and `gopi task submit` printed a normal `Task queued: <id>` with exit 0). The refusal names the value, the arithmetic and the correction (`GOPI_HOME=<parent>`); `GOPI_ALLOW_NESTED_PROFILE=1` honours the value as a warning instead, announcing the degradation. | `internal/config/gopihome.go:287` |
| — | `GOPI_ALLOW_NESTED_PROFILE` | — | Opt-out from the near-miss refusal above (`config.LookupSandboxProfileMisuse`): with it set, a `GOPI_HOME` that names a profile is allowed — with a printed warning naming the nested profile it creates — instead of refused. Exists because the guard is a POLICY about one spelling, not a claim that a nested profile is unusable; an operator's deliberate switch is honoured and announced | `internal/config/gopihome.go:106` |
| — (path only) | `GOPI_SETTINGS` | `--settings` | Which settings file to read; not a settable key | `internal/config/config.go:971` |
| `prompt` | `GOPI_PROMPT` | `--prompt` | Task instruction. The flag and the settings key make positional args input files/images and stop the run path refusing a leftover top-level verb word as prompt text; an INHERITED `GOPI_PROMPT` carries the value but not that authority, because the variable outlives the process that set it (`internal/config/cli_verb_guard.go:163`). `--prompt @file.md` expands a file reference. **CAP: the value is published into `GOPI_PROMPT` only while the entry fits the kernel's per-string execve limit `config.ExecveMaxStringBytes` = 131072 bytes (`MAX_ARG_STRLEN`, counted as `NAME=value\\0`), and that is a PER-STRING limit independent of the much larger ARG_MAX.** ABOVE the cap the variable is NOT published — one string that long makes every subsequent fork/exec in the process fail E2BIG (`fork/exec /usr/local/bin/git: argument list too long` → the clone channel's ground resolution and its anchor gate; `fork/exec /usr/bin/sh: argument list too long` → notify.sh and the task-row writeback), i.e. a large prompt would break the whole runtime instead of the one round. The prompt is still carried IN-PROCESS (`config.ProcessOnlyPrompt`, read by the same reader) and the skip is announced on stderr with both numbers — prompt bytes and the entry bytes vs the limit. Children get a clean environment, which is why an oversized prompt must be passed as a file reference the run reads itself | `cmd/agent/main.go:1127` (`internal/config/env.go:102` publishes) |

#### Thinking & reasoning

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `thinking` | `GOPI_THINKING` | `--thinking` | `enabled` / `disabled`. Default **disabled** (the API's implicit default burns the whole `max_tokens` budget on reasoning). Accepts `"enabled"/"disabled"`; legacy bools `true/false` and `on/off/1/0` are also accepted on decode | `internal/agent/loop.go:3440` |
| `reasoning_effort` | `GOPI_REASONING_EFFORT` | `--reasoning-effort` | Depth, validated against `effort_values`. `off` is a harness-only value meaning "thinking disabled" and is never sent on the wire | `internal/agent/loop.go:3371` |
| `effort_values` | `GOPI_EFFORT_VALUES` (comma list) | — | The provider's legal effort list, as data. Empty → `minimal,low,medium,high,max`. `off` is rejected as a member | `internal/config/env.go:229` |
| `thinking_budget` | `GOPI_THINKING_BUDGET` | `--thinking-budget` | Tokens per round; the harness aborts a round that exceeds it and re-issues with thinking off. Default `1024` (`deepseek.DefaultClaudeThinkingBudget`) | `internal/agent/loop.go:3619` |
| `hide_thinking` | `GOPI_HIDE_THINKING` | `--hide-thinking` | Hide the live reasoning display only; thinking itself stays enabled. Truthy-only | `internal/agent/loop.go:3490` |
| `keep_thinking` | `GOPI_KEEP_THINKING` | `--keep-thinking` | Replay reasoning in assistant history (A/B control). Default off — reasoning is stripped from the wire each round | `internal/agent/loop.go:3496` |
| `max_tokens` | `GOPI_MAX_TOKENS` | `--max-tokens` | Per-response output cap. Default `16384` (`deepseek.DefaultClaudeMaxTokens`) | `internal/agent/loop.go:3320` |
| `context_window` | `GOPI_CONTEXT_WINDOW` | `--context-window` | Context window used for compaction math. Default `200000` | `internal/agent/loop.go:3496` |
| `request_token_cap` | `GOPI_REQUEST_TOKEN_CAP` | — | **Send-time ceiling**: a request whose prompt counts above this is NOT sent (the gate counts it on the exact BPE basis, shrinks once, then refuses with `counted=X cap=Y`). Default `200000` (`agent.RequestTokensCap`). A value that is not a positive integer is IGNORED — the default stands, so an unreadable ceiling can never mean "no ceiling". This is a SEND decision, not a trigger: `context_window`'s `chars/4` triggers are unchanged | `internal/agent/request_tokens_cap.go:59` |

Precedence **inside** the thinking trio (all three are plain env reads, so the
flag/env/settings order of §1 still holds between them):

1. An explicit `GOPI_THINKING` wins (`enabled`/`disabled` as stated);
   `GOPI_REASONING_EFFORT=off` overrides even an explicit `enabled`, with a
   warning.
2. No explicit thinking, but an explicit valid non-`off` effort →
   thinking auto-enabled (the effort implies it).
3. Neither set → disabled.

#### Agent runtime

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `timeout` | `GOPI_TIMEOUT_SEC` | `--timeout` | Overall agent deadline in **seconds**. `0` = unlimited (the default) | `internal/agent/loop.go:3706` |
| `compact` | `GOPI_COMPACT` | `--compact` | **WHICH** reduction runs once the automatic path is opted into: on = LLM summarizer, `off`/`false`/`0` = fold + deterministic trim only. It no longer decides **whether** the conversation is reduced at all — that is `GOPI_CONTEXT_AUTOREDUCE` below | `internal/agent/loop.go:3803`, `internal/agent/loop.go:2308` |
| — | `GOPI_CONTEXT_AUTOREDUCE` | — | **Whether** the automatic reduction path (fold → compact → trim) may run at all. Default **off**: a context that crosses the trigger level is ANNOUNCED to the operator (used/limit/window + "start a new session" + `session_search`) and **nothing is removed**, so the message sequence is unchanged. `1`/`true` re-enables the reduction, in which case `GOPI_COMPACT` picks which one. No settings key and no flag on purpose — a run opts in from its own environment | `internal/agent/loop.go:3814`, `internal/agent/context_pressure.go` |
| `debug` | `GOPI_DEBUG` | `--debug` | Debug logging (also prints the resolved temp root). Truthy-only | `internal/scratch/tmproot.go:718` |

#### Memory compaction

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `compact_trigger` | `GOPI_COMPACT_TRIGGER` | `--compact-trigger` | Fraction of the window at which compaction fires. Default `0.75` (`DefaultCompactTriggerRatio`) — valid range `(0,1)` | `internal/agent/compact.go:391` |
| `compact_target` | `GOPI_COMPACT_TARGET` | `--compact-target` | Fraction the history is shrunk to. Default `0.35` (`DefaultCompactTargetRatio`) — valid range `(0,1)` | `internal/agent/compact.go:51` |
| `compact_keep_recent` | `GOPI_COMPACT_KEEP_RECENT` | `--compact-keep-recent` | Newest messages never compacted. Default `8` | `internal/agent/compact.go:393` |
| `compact_max_summary_tokens` | `GOPI_COMPACT_MAX_SUMMARY_TOKENS` | `--compact-max-summary-tokens` | Summarizer output budget. Default `min(5% of window, 12000)` = `10000` at a 200K window | `internal/agent/compact.go:394` |
| `compact_cooldown` | `GOPI_COMPACT_COOLDOWN` | `--compact-cooldown` | Seconds of pause after a summarizer failure. Default `600` | `internal/agent/compact.go:395` |
| `compact_reference_only` | `GOPI_COMPACT_REFERENCE_ONLY` | `--compact-reference-only` | Wrap injected memory in a REFERENCE ONLY block. Default on | `internal/agent/compact.go:396`, `internal/agent/loop.go:3574` |

#### Turn fold (the note as a substitute)

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| — | `GOPI_TURN_FOLD_KEEP_TURNS` | — | How many of the newest COMPLETE turns the turn fold keeps VERBATIM as an operator escape hatch; only turns older than this window are replaced by their `[IMPORTANT]` note. Default `0` (`turnFoldRecentTurnsDefault`) — **no window at all, every completed turn folds**; valid range `n >= 0`; a negative or unparseable value warns and falls back | `internal/agent/turnfold.go:500`, `internal/agent/loop.go:3894` |

**The note is a SUBSTITUTE, and the fold runs at every round boundary.** A
completed turn's raw messages LEAVE the request and ONE note takes their place
(task `01a1bafd-d5f6-7c1e-b88e-42373f79ae1c`). There is **no size threshold**: the
fold does not read `EstimatedTokens()` or `compact_trigger` at all, so a
conversation of any size folds as soon as a later turn begins. Commit `4c8c44df`
added such a threshold (plus a default raw window of 8) and it was **measured
inert** — session `a596a78e-9594-4b4f-8768-3135e42f4835`: 39 turns, a request of
285 messages and 93,059 estimated tokens against a `0.75 × 200000 = 150000`
trigger, so `gopi session context --last --user` carried **143 raw user rows and
135 raw tool_result blocks and zero `turn folded` notes**.

What still stops a fold is only what would LOSE information — the run's premise at
index 0 (task runs only), a turn that is already a note, a text-only turn, a turn
holding an image, and a turn holding a compaction block or a re-injected record
carrier (`foldableTurnRanges` / `turnRangeFoldable`,
`internal/agent/turnfold.go`). The raw history is never destroyed: it stays in
`session.jsonl`, which `keyword_search` with `scope=session` reads.

#### Reply language (which language gopi answers in)

The operator types English and wants to be ANSWERED in Chinese — a per-machine
preference, not a framework feature. One value knob, three channels; **absent or
empty means NO directive at all**, so a run that configures nothing sends the
byte-identical system prompt it sent before this knob existed (measured: the raw
log's `system` block is 201 bytes on both master and this change).

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `output_language` | `GOPI_OUTPUT_LANGUAGE` | `--output-language` | Language gopi writes its **replies** in, as a BCP-47-ish language tag (`zh-CN`, `en-GB`, `pt-BR`, `zh-Hans`). **It is a language TAG — not a language name and not a locale**: `chinese` and `zh_CN` are refused (the separator is `-`, never `_`). Unset/empty = no directive emitted (not "reply in English" — nothing). When set, a one-sentence directive is prepended to the HEAD of BOTH system prompts gopi assembles: the task path (`assembleSystemPrompt`) and the plain chat path (`chatSystemPromptServed`), which is the surface the operator was looking at. `gopi chat --tools` is covered by the first (it sends the same `a.SystemPrompt`). **Scope is the REPLY only** — the model's reasoning language is untouched (a separate justification, a separate knob, deliberately not bundled). A value that is not a language tag is **reported on stderr and the next channel decides** (Rule 15) — never silently read as unset. The accepted set is a SHAPE, never a whitelist of languages: any tag the model understands is legal, so a language this codebase has never heard of still works. The report reaches **every surface that exposes the knob**, not only the prompt path: `gopi -h`, `gopi --help-env`, `gopi config show`, `gopi chat` and a task run all return from `main()` before the agent path, so `config.OutputLanguageReport()` runs the same resolution **before the subcommand chain** (the surfaces it closes are exactly the ones that return early). A refused declaration is therefore also **distinguishable from an unset knob** — the resolution reports `refused` (a value was declared and discarded) versus `not declared` (nobody asked), and the end-of-chain line names the declaration it did not adopt alongside the value it adopted instead. A **usable** value still prints nothing (the knob's contract: unset emits nothing). precedence is the ordinary one: `--output-language` > `GOPI_OUTPUT_LANGUAGE` > settings — **but the three channels do NOT share one error policy, and that asymmetry is the pinned decision** (see the block comment at `internal/config/output_language.go:273`): an **explicit per-invocation** declaration — the flag on THIS command line, or the variable exported for THIS run — that is not a language tag is **FATAL**, exit code **2** (`config.OutputLanguagePerInvocationExitCode`, the POSIX usage class), before the subcommand chain, so `gopi --output-language "not a lang!!" version` prints the refusal naming the flag, the value and the legal shape, does **not** print the version, and exits 2; a value given with no operand at all (`--output-language` at the end of argv, `--output-language=`, or an exported-but-EMPTY `GOPI_OUTPUT_LANGUAGE=`) is the same fatal, because "declared and unusable" is never read as "declared nothing". The **settings.json** value keeps the softer policy deliberately: it is read by **every** gopi process on the host (a worker, a merger tick, `gopi -h`), so making one stale line fatal would fail every command run on that machine, with the remedy unavailable from the failing invocation — an unusable `output_language` is therefore reported on stderr and the run continues at exit 0. A **usable** value still prints nothing, and an unset knob still emits nothing (the knob's contract). The flag is hoisted into the env before the subcommand chain (`config.BootstrapEnvFromArgs`), which is what makes it reach `gopi chat` and a task run — neither passes the agent-path config block. **Not** emitted into the compaction prompt (that is a summariser writing into the context, not a reply to the user). Both help surfaces state the value vocabulary: the `gopi -h` row, the fatal refusal and the warning are built from the same constants (`OutputLanguageValueShape`, `OutputLanguageExampleTags`, `OutputLanguageNotAValueClaim`), so the page and the error cannot teach different value sets | `internal/config/output_language.go:632`, `internal/config/output_language.go:141`, `internal/config/output_language.go:113`, `internal/config/output_language.go:113`, `internal/agent/loop.go:1111` |

#### Typing-time memory prefetch (background retrieval while you type)

A read-only retrieval that runs in the **background** while you type in
`gopi chat`, so its hits are already in hand when the turn is sealed. It costs
the model no tool call (the fetch is token-free) and it costs the operator no
latency (it samples the editor's published snapshot on its own clock, opens its
own read-only connection under its own bound, and a turn sealed mid-query is
sealed **without** the section). On hits, a provenance-labelled block is
appended to the turn's **outbound message** — never to the display, so a
background retrieval cannot edit the line you are looking at — and the block
tells the model to **judge** the hits (they were matched by keyword, not by
meaning) and to answer without them when none applies. **Default ON (since
2026-10-04), and OFF means OFF**: no goroutine exists, no query is issued, and
the prompt is byte-identical to the pre-knob tree. The flip was a measured
decision, not a preference: the gate was built complete and left disarmed, so
`find ~/.gopi/sessions -name memory-prefetch.jsonl` answered **14** out of
~83,000 session directories on 2026-10-06 (measured) — the switch was armed for
roughly one session in six thousand, and the long sessions that motivated it had
none: a build that never fires is a cost only the reader of the code pays
(Rule 11's failure mode). The OFF state is unchanged and is reachable on all
three channels; what changed is only which answer silence produces.

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `memory_prefetch` | `GOPI_MEMORY_PREFETCH` | `--memory-prefetch` | Typing-time memory prefetch, **default ON**. Three channels, ordinary precedence (`--memory-prefetch` > `GOPI_MEMORY_PREFETCH` > settings `memory_prefetch` > the built-in default) resolved at ONE site; the env channel accepts the truthy/falsey vocabulary (`1`/`on`/`yes`/`true`/`enabled`, `0`/`off`/`false`/`no`), and a value that is **neither** is reported on stderr with the **next channel deciding** (Rule 15) — never silently read as the default, which is what keeps a typo from being indistinguishable from a deliberately-set value. **The settings channel tests DECLARATION, not value**: `Config.MemoryPrefetch` is a pointer (the `scratch_gc` precedent), so an ABSENT key means nobody asked and the built-in default answers, while `{"memory_prefetch": false}` IS a declaration and turns the knob off — under the pre-flip plain bool those two were one value, which was harmless only while the default was itself false. The flag is **hoisted into the env before the subcommand chain** (`config.BootstrapEnvFromArgs`), which is what makes `gopi --memory-prefetch chat` reach `gopi chat` (it returns from `main()` before the agent-path config block); its value is **optional**, so the hoist takes the next token only when that token is a boolean spelling and otherwise leaves it in argv — a **bare** `--memory-prefetch` means ON, and `gopi --memory-prefetch chat` therefore arms the knob and still runs chat. **The lookup is READ-ONLY BY CONSTRUCTION**: it opens the ledger through `tools.LedgerReadOnlyDSNIn` (`mode=ro` + `query_only(true)` + its own busy timeout, `250ms` — deliberately not production's 15s, because the ledger's own timeout exists so a foreground writer **queues** rather than fails) and runs the production search shape (FTS5 `MATCH`, `bm25` ranking, `distilled_qa` join) as a **direct query**, because the production entry point resolves its own path and **migrates** a cold default DB — a keystroke must never be a writer. Under its own deadline (`1500ms`) it **gives up** and records that; against a ledger held by a writer it gives up at its busy timeout. **Degradation is stated, never silent**: a session with no session directory, a record that cannot be opened, and a ledger with no FTS table are each reported (the last as an **empty** result — nothing is distilled on this host yet). The cooked input path (piped stdin, `--no-editor`, a terminal that could not enter raw mode) has no in-progress line to sample, so the knob is armed and **records nothing** — there is no "typed-so-far" state to read. Each run leaves ONE JSON line per event in the session directory's `memory-prefetch.jsonl` (query / hits / ms / outcome / adopted), the ONE place a run's evidence belongs: no daemon, no scheduler, no second database (Rule 35) | `internal/config/memory_prefetch.go:122`, `internal/config/memory_prefetch.go:130`, `cmd/agent/chat_prefetch.go:140`, `cmd/agent/chat_prefetch.go:1082` |

**Where each half lives**: the pure decisions (keyword extraction for CJK and
ASCII, the complexity gate whose primary signal is the keyword count and whose
secondary is the input length, the debounce + differential state machine, the
token-free budget trim, and the labelled RAG rendering) are in
`internal/memoryprefetch` — no third-party dependency, no database, no goroutine,
testable without a fixture. Its ONE project import is `internal/cjkseg` (gse over
its embedded `zh_s` dictionary): CJK candidates are the words that segmenter
returns — the SAME granularity the memory index is written at — and the 2-rune
sliding window survives only as the path taken when the segmenter refuses, where
it is recorded as the named `cjk-tokenizer-gap` degradation (Rule 15) rather than
quietly diluting the retrieval key. The executor (the goroutine, the read-only
handle, the bound, the record, and the single adoption point) is `cmd/agent/chat_prefetch.go`,
wired into the chat REPL beside the status surface; the adoption happens at the
outbound-message boundary, and `Display` is untouched.

#### Chat surface ideal geometry (the size the chrome is drawn for)

The chat surface draws three pieces of chrome that a narrow terminal cannot
carry: the startup screen's boxed mark with its side panel of tips, the reserved
editing line plus its status row, and the info/status bar's aligned column list.
They were tuned at **117×26** — the geometry every live measurement in this repo
was taken at (the operator's own terminal, the pty every dump test allocates, the
film's `--cols`/`--rows`) — but that pair lived as a bare literal in **two**
places (the replay flag defaults and the beat protocol's), so nothing but
convention kept the live surface and the film that claims to reproduce it at the
same size. The knob below is the declaration of that size.

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `film_terminal` | `GOPI_FILM_TERMINAL` | `--film-terminal` | Which terminal emulator `gopi chat --film` photographs the surface IN, by NAME. Default `xfce4-terminal` — the operator's 2026-10-04 ruling: a CJK surface is NEVER filmed in xterm, whose default font is the X11 core bitmap `fixed` (6x13) with `CHARSET_REGISTRY ISO8859` / `CHARSET_ENCODING 1` only, so it carries zero CJK glyphs and every Chinese cell films as a hollow fallback box while the mis-measured advance width shoves the neighbouring ASCII along (measured 2026-10-04: the same line, real glyph in xfce4-terminal, boxes in xterm — `devdoc/film-cjk-default-terminal.md`). The operator named `gnome-terminal` and `xfce4-terminal`, both fontconfig+TrueType with fallback; only the second is usable here, so it is the default. `xterm` stays selectable and its historical measurements stand (706x342 = 117x26 x 6.034 px/cell, 106/106 settled frames). The name is resolved at **ONE** site — `shot.ResolveFilmTerminal` — which reads the four channels in the project's ordinary order (`--film-terminal` > `GOPI_FILM_TERMINAL` > settings `film_terminal` > the built-in default) and returns which one decided, so a report can say *where* the film's terminal came from instead of presenting a default as a discovery. Both the PRE-verb spelling (`gopi --film-terminal xfce4-terminal chat --film …`) and the POST-verb one (`gopi chat --film --film-terminal xfce4-terminal`) reach the same reader: the pre-verb form is hoisted into `GOPI_FILM_TERMINAL` before the subcommand chain (`config.BootstrapEnvFromArgs` → `config.PublishFilmTerminalFlag`, the same mechanism `--chat-ideal-geometry` uses), while the post-verb form does NOT pass through that writer at all: the chat verb's own walk reads the name and HANDS IT ON AS AN ARGUMENT (`config.PublishFilmTerminalFlag` has no caller under `cmd/`, and `cmd/agent/cli_chat.go` never spells `GOPI_FILM_TERMINAL`). The two channels therefore do not share a writer — they do not need to: they meet at the ONE site that decides, `shot.ResolveFilmTerminal`, which takes the argument first and otherwise reads that env name, so the flag, the env and the settings key are three channels that CANNOT disagree about which terminal the film runs in. **The VALUE is a terminal NAME, validated against the launch-shape registry** (`internal/shot/terminalregistry.go`): each row carries that terminal's exec protocol, its geometry unit and its window-selection rule, because they are NOT interchangeable — measured 2026-10-03, xterm takes geometry in CELLS in two argv slots and runs the command after `-e`, xfce4-terminal takes `--geometry=WxC` in one and runs it after `-x`, and the two are the reason xterm's flags must never be written into another terminal's argv. An **unknown name is refused** with the reason and the list of usable names, and a name that is KNOWN but not usable is refused with *its own* reason — `gnome-terminal` is in the registry and unusable here (its binary does not draw the window: it asks the D-Bus-activated `org.gnome.Terminal` service to draw it, and that service uses the display *it* inherits, not the private one a film run owns — with no session bus reachable it exits 1 with "Error constructing proxy for org.gnome.Terminal: … Timeout was reached" and maps nothing; with a session bus it maps only its own 10x10 placeholder). **Nothing falls back to the default** on a bad name (Rule 15): a film whose terminal nobody chose would be a picture of the wrong screen. A terminal with no session-log switch (`xfce4-terminal`) still films — the frames are the evidence — and the missing producer log is ANNOUNCED (`shot.SessionLogAbsentNotice`), never left as an empty file that reads as "the producer printed nothing". | `internal/config/film_terminal.go:32`, `internal/config/film_terminal.go:36`, `internal/config/film_terminal.go:38`, `internal/config/film_terminal.go:42`, `internal/shot/terminalregistry.go:439`, `internal/shot/terminalregistry.go:405`, `internal/shot/terminalregistry.go:376`, `internal/config/settings.go:669`, `cmd/agent/chat_film.go:366`, `cmd/agent/cli_chat.go:runChatWithInput` |
| `chat_ideal_geometry` | `GOPI_CHAT_IDEAL_GEOMETRY` | `--chat-ideal-geometry` | The chat surface's ideal geometry, as `COLSxROWS`. Default `117x26`. Three channels, ordinary precedence (`--chat-ideal-geometry` > `GOPI_CHAT_IDEAL_GEOMETRY` > settings `chat_ideal_geometry` > the built-in default), resolved at **ONE** site — `config.ChatIdealGeometry` — which BOTH the live surface (`cmd/agent/chat_geometry.go`, applied through `lineedit.Editor.SetCanvasCols`) and the replay/beat filmer (`cmd/agent/chat_film.go`'s `--cols`/`--rows` defaults for the producer it forces, `cmd/agent/chat_replay_beats.go`'s beat defaults) call, so the surface and the film it feeds can never hold two ideas of the designed size. The flag is **hoisted into the env before the subcommand chain** (`config.BootstrapEnvFromArgs`, writing the knob's ONE env representation through `config.PublishChatIdealGeometryFlag`), which is what makes `gopi --chat-ideal-geometry 100x30 chat` reach chat; the verb ALSO accepts the post-verb spelling (`gopi chat --chat-ideal-geometry 100x30`, `--chat-ideal-geometry=100x30`), published through the same writer, because the closed vocabulary must not refuse a flag the help page advertises. **What the geometry DOES**: a terminal at least this wide draws the canvas at exactly this width (the remainder to the right is left blank — the box is not stretched, and the info bar's columns are pinned to a fixed cell count so extra room cannot be spent on them anyway); a **narrower** terminal keeps its own width and is **told what it lost** (Rule 15 — never a silent downgrade). **Rows are never imposed**: the status row is anchored to the screen's LAST row, so a canvas claiming fewer rows than the terminal has would park the status line mid-screen (the "composer is not at the bottom" defect the layout's own comments record); the ideal row count therefore only ever appears in the degradation sentence and in the film's default. A value that is not `COLSxROWS` is **reported on stderr and the next channel decides** — never silently read as unset, and never fatal for the settings channel, so a typo cannot make `gopi chat` unusable (the same soft policy `memory_prefetch` and the settings channel of `output_language` use). The geometry **is recorded, and that is what a replay reads**: every session writes the terminal it was ON into its own header — `session_start` (and `metadata.json`) carries `terminalGeometry`, one object of `{"state":"measured","cols":N,"rows":M}` or `{"state":"unknown","reason":"…"}` (the vocabulary and both constructors live in `internal/sessionevent/geometry.go`; `internal/terminalgeom` is the ONE measurement of stdout→stderr→stdin, so two runs cannot record two answers for one terminal). `gopi chat --replay` reads it back (`internal/replaydrive/geometry.go`) and makes it this run's ideal geometry, through the same writer the flag uses, so a re-run draws at the size the session really happened at; an explicit `--chat-ideal-geometry` still wins, and either way the source is printed. A recording that carries **no** field (a session from before this existed) or an explicit `unknown` falls back to the ideal **and says so** — never a silent downgrade; a field that is present but undecodable is reported, never read as silence (Rule 15). `gopi chat --replay --film` shoots at the recording's size when no `--cols`/`--rows` was given, and the frames are that size (measured: a `100x30` recording filmed to `604x394` px frames). **Resume is unaffected**: a geometry is a property of the terminal a session ran on, not a knob of the resumed conversation | `internal/config/chat_geometry.go:126`, `internal/config/chat_geometry.go:152`, `internal/config/chat_geometry.go:209`, `cmd/agent/chat_geometry.go:116`, `internal/lineedit/editor.go:571`, `cmd/agent/chat_film.go:252`, `internal/sessionevent/geometry.go:68`, `internal/terminalgeom/terminalgeom.go:37`, `internal/replaydrive/geometry.go:42`, `cmd/agent/chat_replay_geometry.go:117` |
| `ambiguous_width` | `GOPI_AMBIGUOUS_WIDTH` | `--ambiguous-width` | How an East Asian Width **AMBIGUOUS (A)** rune is measured — box-drawing `U+2500–257F`, the turn marker `▌` (`lineedit.TurnMarker`), em dashes, arrows, enclosed numbers: `wide` = two cells (a CJK terminal), `narrow` = one cell (a Western terminal, and the historical behaviour), unset = **the LOCALE decides** (`LC_ALL`, then `LC_CTYPE`, then `LANG`: `zh`/`ja`/`ko` wide, everything else narrow). Three channels, ordinary precedence, resolved at **ONE** site — `config.ResolveAmbiguousWidthMode`, read by `config.IsAmbiguousWide` — because three packages measure runes (`internal/lineedit` `runeCells`/`isWideRune`, which `screenattr.go`'s cell grid also reads; `internal/config` `IsWideRune`; `internal/text2img` `isWideRuneForGrid`, which deliberately stays A-narrow) and the A table lives in the leaf package all of them can import (Rule 14). **Why it is a knob and not a measurement**: this host renders `─` two cells under a CJK font and **one** cell under xterm's default `fixed` face — so no in-process measurement is right for every terminal, and the locale is the variable the CJK conventions are already keyed on. An unusable value is **reported on stderr once** and the next channel decides (Rule 15). The measurement is in `devdoc/lineedit-eaw-ambiguous.md` | `internal/config/ambiguous_width.go:109`, `internal/config/ambiguous_width.go:126`, `internal/lineedit/render.go:725` |

**Where each half lives**: the knob itself (the three channels, the ONE resolver
returning `(cols, rows, source)`, the ONE validator and the refusal text) is
`internal/config/chat_geometry.go` — the source label rides every report, so
"the screen is 117 wide because you asked for it" and "because nobody asked" stay
distinguishable. What the SURFACE does with the answer is
`cmd/agent/chat_geometry.go`: the degradation sentence (thresholds measured via
`chatBannerMinCols()` and `statusColumnsAtCols`, never re-typed) and the canvas
width `min(terminal, ideal)`. The canvas mechanism is `lineedit`'s, not a second
geometry: `Editor.SetCanvasCols` bounds the width `EnableRegion` and every resize
install the reserved layout at, through the ONE `canvasWidth()` — the install, the
resize path and the answer renderer's column count cannot disagree.

#### Chat surface self-driven turn budget (how far one period may run)

The `chat_surface` tool's `action=type` lets a turn type its OWN next turn — the
self-driven loop — and the number that keeps that safe used to be a package
constant (`8`) with no channel of its own: an operator running a long autonomous
verification could not raise it without a rebuild, and the model's only lever
(`max_turns`) can TIGHTEN a period, never lengthen one. The bound is now a knob.
**What did not change** is the guarantee: a period still ENDS at the bound, the
model still cannot renew it (only a line the OPERATOR submits resets the
counter), and the number is announced on the console as
`self-driven turn 1/20 of this period`.

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `chat_surface_turns` | `GOPI_CHAT_SURFACE_TURNS` | `--chat-surface-turns` | How many self-driven turns `chat_surface action=type` may add to ONE **period** — the stretch of conversation between two lines a HUMAN wrote. Default **20** (raised from the former fixed `8`, which was too small for the long autonomous run the seam exists for). Accepted range `2..1000`; a value below the floor is REFUSED by the resolver (reported on stderr, next channel decides) rather than installed, so no channel can produce a bound that fires before the first self-driven turn. Three channels, ordinary precedence (`--chat-surface-turns` > `GOPI_CHAT_SURFACE_TURNS` > settings `chat_surface_turns` > the built-in default), resolved at **ONE** site — `config.ChatSurfaceTurns` — which `internal/agent` reads through `chatSurfaceConfiguredBound`. The flag is **hoisted into the env before the subcommand chain** (`config.BootstrapEnvFromArgs` → `config.PublishChatSurfaceTurnsFlag`), which is what makes `gopi --chat-surface-turns 40 chat` reach chat; the verb ALSO accepts the post-verb spelling (`gopi chat --chat-surface-turns 40`) through the same writer, so ONE spelling and ONE writer serve both and the closed vocabulary never refuses an advertised flag. **The configured value is the CEILING `max_turns` is clamped into**: `max_turns` below `chatSurfaceRestartFloor` rises to the floor, above the configured bound clamps DOWN, so the model can shorten a period but can never lengthen one — and the session's own bound is re-checked against the ceiling on every read, so a future writer cannot install a larger one by accident. The resolved number reaches the model through the tool DESCRIPTION and the `max_turns` schema default (Rule 11: the description is the only persuasion surface), so the tool cannot tell a configured run a stale budget. | `internal/config/chat_surface_turns.go:110`, `internal/config/chat_surface_turns.go:129`, `internal/config/chat_surface_turns.go:181`, `internal/agent/chat_surface.go:795`, `internal/agent/chat_surface.go:808`, `internal/agent/chat_surface.go:821`, `internal/agent/chat_surface.go:248` |

#### Logging

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `raw_log` | `GOPI_RAW_LOG` | `--raw-log` | Raw HTTP log. Default **on**; disabled only when the value is `OFF` (case-insensitive) | `internal/agent/rawlog.go:301` |
| `raw_log_dir` | `GOPI_RAW_LOG_DIR` | `--raw-log-dir` | Legacy flat raw-log directory. Default `<gopi dir>/raw`. New sessions co-locate their raw log with the session JSONL, but a configured value still wins | `internal/agent/rawlog.go:158` |

#### Tools

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `tool_timeout_sec` | `GOPI_TOOL_TIMEOUT_SEC` | `--tool-timeout` | read/write/edit timeout in seconds. Default `30` | `internal/tools/timeout.go:53` |
| `bash_timeout_sec` | `GOPI_BASH_TIMEOUT_SEC` | `--bash-timeout` | bash absolute timeout in seconds. Default `120` | `internal/tools/timeout.go:58` |
| `bash_idle_sec` | `GOPI_BASH_IDLE_SEC` | `--bash-idle` | bash no-output timeout in seconds — kills an interactive prompt or an infinite printer faster than the absolute cap. Default `120` | `internal/tools/timeout.go:63` |
| `allow_dangerous` | `GOPI_ALLOW_DANGEROUS` | `--allow-dangerous` | Permit dangerous commands (`rm -rf`, …) | `internal/tools/bash.go:99` |
| `websearch_scripts_dir` | `GOPI_WEBSEARCH_SCRIPTS_DIR` | `--websearch-scripts-dir` | Where the web-search plugin scripts live (the python3 plugin scripts `gopi web search` / `gopi web fetch` exec). Default `<gopi dir>/plugins/websearch/scripts` — i.e. `<GOPI_HOME>/.gopi/plugins/websearch/scripts`, the path a sandbox resolves and links. Resolved by `config.WebSearchScriptsDir` (ONE reader of this env var; `internal/tools` and `internal/sandbox` both call it, so the tool and the sandbox creator cannot disagree about the path) | `internal/config/plugins.go:93` |
| `memory_db` | `GOPI_MEMORY_DB` (legacy `GOPI_HIPPO_DB`) | `--db` | The ledger + knowledge base. Default `<gopi dir>/memory.db`. A relative path resolves against the process cwd — a zero-byte `memory.db` in the repo root is the classic probe accident. **A path that DOES NOT EXIST is not an empty knowledge base**: `gopi memory`'s READ-ONLY sub-verbs (`search`, `get`, `list`, the reporting modes of `provenance`, `feedback --list|--suggest-cold|--recompute`, and the `--dry-run` modes of `converge-keywords`/`backfill-tokens`) refuse an explicitly named path that is absent — exit non-zero, name the path and the active ledger, create nothing — while the WRITING ones still create it and print `created a new knowledge base at <path>` (`cmd/agent/cli_memory_ledger_guard.go`; the ranking below is unchanged, so any OTHER caller of `config.ResolveLedger` keeps creating). **Ranked chain, highest first** (ONE resolution, `config.ResolveLedger`): the `--db` flag of the run/verb that owns it > `GOPI_MEMORY_DB` > `GOPI_HIPPO_DB` > this settings key > the default. A LEADING `--db` before a verb (`gopi --db X memory search q`) is the run-flag spelling, not the verb's: the positional-verb guard refuses it BY NAME rather than reading it as a ledger — put the flag after the verb that owns it | `internal/config/memory_env.go:118`, `:145` |
| `no_enforce_memory` | `GOPI_NO_ENFORCE_MEMORY` | `--no-enforce-memory` | Disable both harness memory checkpoints (task-start search / task-end store) — A/B isolation | `internal/agent/loop.go:2261` |
| `no_enforce_memory_consult` | `GOPI_NO_ENFORCE_MEMORY_CONSULT` | `--no-enforce-memory-consult` | Disable only the review-time memory-consult checkpoint; the task-start search stays forced | `internal/agent/loop.go:2280` |

#### Voice input

`gopi tool voice_input` with **no argument and no `--client`** is the default shape:
it records from this machine's microphone and transcribes it **here, with the local
engine**. One process, no hop — the same command that used to require starting a
`--server` and pointing a `--client` at it. Its recording is bounded by a named
default length (`--seconds`, 10s when unset), so the microphone is stopped by a
clock and never left open by a forgotten flag.

The weights and the runtime are looked for in **two** rungs, in this order, and the
first that holds them wins (one walk, `voiceResolveDeployment` in
`cmd/agent/cli_voice_local.go`):

| rung | model directory | runtime |
|---|---|---|
| 1 — **the default**: the gopi profile | `<gopi dir>/voice` | `<gopi dir>/voice/llama-funasr-cli` |
| 2 — the **legacy** deployment (compatibility) | `$HOME/work/funasr-models` | `$HOME/work/runtime/llama.cpp/build/bin/llama-funasr-cli` |

`<gopi dir>` is `config.GopiDir` (`$GOPI_HOME/.gopi`, else `~/.gopi`) — the one place
in this story gopi owns, which is why rung 1 is the default: the local engine is
~914 MB of weights plus a foreign C++ runtime gopi neither builds nor ships, so the
directory it is deployed in has to be one gopi can name without a flag. **Rung 2 is a
path gopi never creates and does not maintain** (absent entirely on a host that never
installed it), so it is read only when rung 1 holds no complete deployment — and a run
that DOES take a path from rung 2 prints a migration note (rung 2's path, rung 1's
path, and the runtime file name to move with the weights), so depending on it is
visible rather than silent — the note rides the resolution, which happens once, up
front, for every shape of the verb, so `--list-devices` announces it too (it reads no
model, and the note is a statement about this host's deployment, not about that run).
If **neither** rung holds a deployment the run does not start: it names the file that
failed the check and prints an install instruction —
both GGUF files and the runtime binary with the directory each goes in — followed by
the way to need none of them (`--engine doubao`). The two rungs resolve
**independently**: weights in the profile with the binary still in `$HOME/work` (or the
reverse) is a working installation.

`--models` / `--runtime` are a **third** answer, not a rung: naming either one skips
the search for that path and uses exactly what was named (no fallback), because a run
that reads other weights than the ones it was told to read is worse than a run that
fails. **Which engine runs and where it found its deployment is PRINTED, once per local
transcription, before it starts**: one sentence naming the engine, the model directory,
the runtime path and the rung that supplied them (rung 1 / rung 2 / `--models`+`--runtime`
/ caller-supplied) — the file run, the bare capture and `gopi chat`'s `/voice` mode all
print that same line (one spelling, `voiceEngineProvenanceLabel` in
`cmd/agent/cli_voice_local.go`), and it is silent when `--engine doubao` was chosen,
because that engine reads neither the weights nor the runtime. They are read only by the
shapes that transcribe **here** (a file run, the bare capture, a server): `--client` is
transcribed on the host it posts to (which owns its own weights) and `--list-devices`
opens no model, so both are refused at parse time with the flag named rather than
accepted and ignored.

**Contradictory flag combinations are refused at PARSE time, before any recording.**
The invocation's shape (bare capture / audio file / `--server` / `--client` /
`--list-devices`) is decided first, then every flag is checked against the shape it
was given on; the refusal names the flag, the shape and the correction, and it is
emitted before a recorder is spawned — so a mistake that is knowable up front never
costs a recording. The matrix is pinned by
`cmd/agent/cli_voice_refusal_test.go` (`TestVoiceParseRefusesContradictoryCombinations`
for the refusals, `TestVoiceParseDefaultsAreTheCaptureShapeAndItsFlags` for the
combinations that must stay legal — a refusal layer that over-triggers would break
the default shape first). Refused pairs include: `--client`+`--server`,
`--stdio`+`--listen`, an audio file on a `--server`/`--list-devices` run, `--seconds`
/ `--device` / `--record` on a shape that records nothing, `--chunk` anywhere but a
file run or a server, `--server-url` on a shape that never posts, `--models` /
`--runtime` on a shape that opens no weights, `--display` without `--self`, and
`--audio-source` on a shape that opens no capture (a `--server` or `--list-devices`
run — the flag REPLACES the microphone, so on a shape that has none it would be read
by nothing; `cmd/agent/cli_voice.go`, refused by `voiceAudioSourceShapes`). A
`--audio-source` path that is empty, missing or a directory is refused earlier
still, at parse time, and a PCM/WAV whose container declares a geometry other than
the capture contract is refused where it is read
(`cmd/agent/cli_voice_audio_source.go`).

<!-- MERGE NOTE (re-land run 01a11715, task/01a1169f491171eb9941869336a706b5):
     this block was a conflict between master's regenerated settings table and the
     branch's copy of the same five rows. Master's rows are kept WHOLESALE — they are
     a strict superset (the new `doubao_api_key` inline-credential row) carrying the
     settings.go line references the CURRENT tree resolves, and `config_doc_gate`
     fails the build on a stale one. From the branch, exactly one line is folded in:
     voice_engine's description, whose `(default)` placement it replaced with the
     explicit "the resolution is stated" clause — that clause is the branch's change
     and is the ONLY part of this block that was not already master's. No other branch
     line here carried information: the other four rows were byte-identical to the
     base's, which master had already superseded. The note sits HERE, above the
     header, and not between the header and the rows: this table WAS READ BY A
     GATE (`internal/standards/config_doc_flag_gate_test.go`, deleted in
     `78bdf05a` — so the parser rule below is a record of why this note sits
     here, not a check that runs today), whose parser
     closes the table on any line that is not table-shaped — an intervening
     prose line silently removes the rows below it from the census (measured on
     this very hunk: 57 rows parsed instead of 63). A comment about a table
     belongs outside it, and now is. -->

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `default_device` | `GOPI_VOICE_DEVICE` | — | Default capture device for `gopi tool voice_input` (the bare capture and `--client` alike), as a device **id** printed by `--list-devices` (never a card index). Unset → system default capture → first enumerated device | `internal/config/settings.go:286` |
| `audio_engine` | `GOPI_AUDIO_ENGINE` | `--engine` (subcommand flag) | The transcription engine, named for the **shape of the work**: `recording` (Volcengine cloud **batch**, file transcription / `volc.seedasr.auc` — **the DEFAULT**: 0B of models, the audio is **uploaded** to `openspeech.bytedance.com`, one transcript per file); `streaming` (Volcengine **v2 real-time**, `api/v2/asr`, cluster `volcengine_streaming_common` — a **PARTIAL** transcript while you are still speaking; needs the `streaming` block's credential pair or a KEY=VALUE file, and is **never selected by default** — a machine with nothing deployed still transcribes); `sauc` (Volcengine **v3 SAUC**, `api/v3/sauc`, resource `volc.seedasr.sauc.duration`, Doubao streaming speech-recognition model 2.0 — the v2 engine's JOB on the newer protocol and a **different credential shape**: it takes the legacy `APPID`/`TOKEN` **pair**, while the single-key shape answers **403** there; **TWO endpoints, one per arm** — a **file** run (`gopi tool voice_input`) opens `sauc/bigmodel_nostream` (batch semantics: it answers once, at the end), while **`gopi chat`'s `/voice` live arm opens `sauc/bigmodel_async`**, the **bidirectional** one, so the text is rewritten in the editing line WHILE the operator speaks (measured 2026-10-13: first text ~2.5 s into a 4.8 s clip, ~300 ms between rewrites); `audio_engine_param.sauc.endpoint` overrides **both**; knobs and defaults in the note below the table; never selected by default); or `local` (Fun-ASR-Nano, needs the 910MB of weights on disk; nothing leaves the machine). Unset resolves to `recording`, and that resolution is **stated** — every transcribing shape prints the engine it selected. The pre-rename key `voice_engine`, the env `GOPI_VOICE_ENGINE` and the pre-rename engine names `doubao` / `doubao-stream` are **still read and still honoured**, and a run that reads one prints a `renamed configuration` notice naming the replacement | `internal/config/settings.go:84` |
| `audio_engine_param.recording.key_file` | `GOPI_AUDIO_KEY_FILE` | `--doubao-key-file` (subcommand flag) | Path of a **file** holding a credential. Default `/home/user/.doubao/apikey.txt` — the legacy location, kept working. A file named on any explicit channel (flag, env, block key) **outranks** an inline value. ONE knob, TWO formats: the `recording` engine reads the single-line API key, BOTH cloud streaming engines read a `KEY=VALUE` document (`APPID=…` / `TOKEN=…`) — `streaming` (v2) and `sauc` (v3) — one location, no second credential mechanism. Its VALUE is never printed; a message may name only the path, and a file wider than `0600` is reported with its mode | `cmd/agent/cli_voice_engine.go:370` |
| `audio_engine_param.recording.api_key` | `GOPI_AUDIO_API_KEY` | — | The recording engine's API key **inline**, in its nested block — the rung between an explicitly named key FILE and the `/home/user/.doubao/apikey.txt` fallback. `gopi does not republish this value into the environment`. Example shape: `"audio_engine_param": {"recording": {"api_key": "<doubao-api-key>"}}` | `cmd/agent/cli_voice_engine.go:388` |
| `audio_engine_param.recording.api_base_url` | `GOPI_AUDIO_API_BASE_URL` | `--doubao-base-url` (subcommand flag) | The recording engine's endpoint **root**, from which BOTH of its URLs derive (`doubaoasr.SubmitURLFor` / `QueryURLFor`). Default `https://openspeech.bytedance.com/api/v3/auc/bigmodel`. One root, so the submit and the query can never be pointed at two hosts. `--doubao-submit-url` / `--doubao-query-url` remain **more specific** | `internal/doubaoasr/doubaoasr.go:52` |
| `audio_engine_param.recording.resource_id` | `GOPI_AUDIO_RESOURCE_ID` | `--doubao-resource` (subcommand flag) | The recording engine's `X-Api-Resource-Id`. Default `volc.seedasr.auc` — never left to the SDK's implicit default. Three channels, in precedence order: the flag > the env name > this nested key | `internal/config/audio_engine_param.go:152` |
| `audio_engine_param.recording.hotwords` | `GOPI_AUDIO_HOTWORDS` | `--doubao-hotwords` (subcommand flag) | Comma-separated hotwords (sent as `request.corpus.context`). Three channels, in precedence order: the flag > the env name > this nested key | `cmd/agent/cli_voice_engine.go:530` |
| `audio_engine_param.streaming.endpoint` | `GOPI_AUDIO_STREAM_ENDPOINT` | `--doubao-stream-endpoint` (subcommand flag) | The **streaming** engine's endpoint (a `wss://` URL); the same block carries `audio_engine_param.streaming.cluster`, the product line the vendor authorises and bills by (`volcengine_streaming_common` by default — a wrong value is a not-granted refusal, never a fallback, and it is reachable from the nested block only: `doubaostream.Options.Cluster` at `internal/doubaostream/client.go:147`). Default `wss://openspeech.bytedance.com/api/v2/asr`. A value that is not `wss://` is refused by the pre-flight check, naming this knob, before any audio is opened | `internal/config/audio_engine_param.go:78` |
| `audio_engine_param.streaming.app_id` | `GOPI_AUDIO_APP_ID` | — | The streaming engine's `APPID`, inline in its nested block. `gopi does not republish this value into the environment`. A credential **file** named on an explicit channel still wins when it exists; the inline pair answers when it does not. Composable: one half may come from the environment while the other comes from here | `internal/config/audio_engine_param.go:142` |
| `audio_engine_param.streaming.token` | `GOPI_AUDIO_TOKEN` | — | The streaming engine's `TOKEN` (the second half of the same credential pair). Same discipline as the APPID beside it: settings/env channels, no CLI flag, never republished, never printed. `gopi config show` renders both as `<set: N chars>` | `internal/config/audio_engine_param.go:142` |
| `audio_engine_param.sauc.app_id` | `GOPI_AUDIO_APP_ID` | — | The **v3 SAUC** engine's `APPID` — the FIRST half of the legacy console pair, and the SAME two env names the `streaming` block publishes (one vendor issues one application id per account; a second pair of names for the same value would be a name an operator has to discover before the one they already export can be used). The pair became configurable in gopi's own document on 2026-10-14: until then this engine's credential could only live in the vendor's `/home/user/.doubao/v2_voice.env`, so a deployment that was correct everywhere else silently depended on another program's directory. A HALF-STATED PAIR (this set, `token` unset) is refused **by name** before any audio moves, never completed from another channel. Never printed: `gopi config show` renders it as `<set: N chars>` | `internal/config/audio_engine_param.go:470` |
| `audio_engine_param.sauc.token` | `GOPI_AUDIO_TOKEN` | — | The v3 SAUC engine's `TOKEN` (the second half of the same credential pair) — same channels, same discipline and the same refusal as the APPID beside it. It is the value sent as `X-Api-Access-Key` beside `X-Api-App-Key`; the single-key shape answers **403** on this endpoint | `internal/config/audio_engine_param.go:470` |
| `audio_engine_param.local.models_dir` | `GOPI_AUDIO_LOCAL_MODELS` | `--models` (subcommand flag) | The **local** engine's weights directory — its **first** settings/env surface (until 2026-10-09 only `--models` existed, so a headless deployment spelled two absolute paths every invocation). A path named here is **explicit**: the two-stage deployment search may not override it | `cmd/agent/cli_voice.go:1015` |
| `audio_engine_param.local.runtime_bin` | `GOPI_AUDIO_LOCAL_RUNTIME` | `--runtime` (subcommand flag) | The **local** engine's runtime executable (`llama-funasr-cli`) — new surface, same reason and the same **explicitness** contract as the weights directory beside it | `cmd/agent/cli_voice.go:1019` |

**The v3 SAUC engine's credential is the legacy console PAIR, not a single key** — and since 2026-10-14 that pair lives in gopi's own settings document. `X-Api-App-Key` + `X-Api-Access-Key`, held as `audio_engine_param.sauc.app_id` / `.token` (the vocabulary the `streaming` block already uses for the same vendor pair, or `$GOPI_AUDIO_APP_ID` / `$GOPI_AUDIO_TOKEN` over it). This is measured, not documented: on this account the v3 endpoint answers the single-key shape (`api_key`, the `recording` engine's credential) with HTTP 403 `requested resource not granted`, which reads like a missing entitlement, and the legacy pair with `101 Switching Protocols` (`internal/doubaosauc/credentials.go:1`). The shape is therefore refused **by name** before a socket is opened (`cmd/agent/cli_voice_sauc.go:553`) — a run that reports 403 has a shape mismatch, and the fix is the credential, not a console toggle. The same pre-flight refuses a HALF-STATED pair (`cmd/agent/cli_voice_sauc.go:553`), which is the shape a mixed configuration always takes: one half present, the other not.

**Where the v3 credential comes from, in ONE order** (`cmd/agent/cli_voice_sauc.go`, `voiceSaucCredentialSource`) — every message, the pre-flight check, the privacy notice, the start line and the request assembly read it from there:

```
a FILE named on an explicit channel
      --doubao-key-file  >  $GOPI_AUDIO_KEY_FILE ($GOPI_DOUBAO_KEY_FILE)
                         >  settings "audio_engine_param.recording.key_file"
   >  the COMPLETE inline pair
      settings "audio_engine_param.sauc.app_id" + ".token"
      (or $GOPI_AUDIO_APP_ID + $GOPI_AUDIO_TOKEN over it)
   >  the LEGACY vendored file  /home/user/.doubao/v2_voice.env  — ANNOUNCED, never silent
```

A named **file** outranks the inline pair for the same reason it does on the `recording` engine: a path is the operator's own statement about where the credential lives, and the v2 arm settled the question the same way. The **legacy file** is the rung that keeps every pre-existing deployment working, and a run that ends on it says so — `[voice] sauc credential: falling back to the legacy doubao file <path> — this pair now belongs in gopi's own settings (audio_engine_param.sauc.app_id / .token), which outranks it` — so an operator who never configured that path learns both that it answered and where the pair belongs now. A file named by an explicit channel that **cannot be read** does not refuse the run while a complete inline pair is configured (the substitution is announced too), and a **half-stated pair is refused outright**: the two halves are ONE credential, so a stated half mixed with another channel's half authenticates as neither application. Messages carry the credential's **origin** — a path, a settings path like `settings audio_engine_param.sauc.app_id/token`, or an env name — and never a value: the privacy banner, the `channel →` start line and the chat `/voice` row all report it, and a settings document that carries the pair is tightened to `0600` with the change announced.

**The v3 SAUC engine has TWO endpoints, and each ARM gets the one its own contract needs.** `audio_engine_param.sauc.endpoint` is the ONE knob and it wins **verbatim** on both arms; the two *defaults* differ on purpose (`voiceSaucEffectiveEndpoint` for a **file** run, `voiceSaucLiveEndpoint` for the **live** one), because the arms' shapes are not the same:

| arm | default endpoint | why |
|---|---|---|
| file — `gopi tool voice_input`, `gopi voice --engine sauc FILE` | `sauc/bigmodel_nostream` (`doubaosauc.DefaultEndpoint`) | the contract is ONE audio path in, ONE string out; the endpoint answers once, at the end |
| live — `gopi chat`'s `/voice` | `sauc/bigmodel_async` (`doubaosauc.EndpointBidirectional`) | the whole point of the arm is text WHILE the operator speaks; `bigmodel_nostream` would answer nothing until the utterance was over — i.e. the six-second window loop's behaviour, with none of its reasons |

MEASURED (2026-10-13, task `01a13303`, `/tmp/rec1.wav` 4.8 s, real run through the live arm): the bidirectional endpoint's first text arrived **2.5 s in** (the clip's first speech starts at ~2.0 s of audio), then **8 partials**, each a full REWRITE of the sentence (`能听到` → `能听到吗` → … → `能听到吗？不知道能不能听到声音呢？`, quoted verbatim: these are the service's own Chinese transcript, meaning roughly "I can hear it" → "can you hear me?" → … → "can you hear me? I can't tell whether my voice is coming through"), roughly 300 ms apart, the last one flagged `definite` — and the session ended on its own when the capture ran out. `payload_msg` does **not** wrap these responses (the vendor's SDK snapshot suggests it does): the body is `{"audio_info":…,"result":{"text":…}}` directly, which is what `internal/doubaosauc/client.go` decodes.

**Where the doubao key may live, in one place** — the precedence is spelled once
(`cmd/agent/cli_voice_engine.go`, `voiceDoubaoKeySource`) and every message, the
pre-flight dependency check and the request assembly read it from there:

```
--doubao-key-file  >  $GOPI_AUDIO_KEY_FILE  ($GOPI_DOUBAO_KEY_FILE)
                   >  settings "audio_engine_param.recording.key_file"
                      (or ".streaming.key_file" — ONE knob, ONE file, TWO
                      formats: the v2 streaming engine AND the v3 `sauc` engine
                      read this document's APPID/TOKEN pair)
                   >  $GOPI_AUDIO_API_KEY    ($GOPI_DOUBAO_API_KEY)
                   >  settings "audio_engine_param.recording.api_key"
                   >  /home/user/.doubao/apikey.txt (legacy fallback)
```

The bracketed spellings are the **pre-rename** names, read at the same rung
(2026-10-09 renamed the surface: `voice_engine` → `audio_engine`, and the flat
`doubao_*` keys → `audio_engine_param.<engine>.<knob>`). A run that reads one
prints a `[gopi] renamed configuration:` line naming the replacement, once, on
stderr — the migration is the operator's to make and gopi's job is to make sure
it is not made by accident.
```

A key **file** named on an explicit channel wins over the inline value: a stored
path is a deliberate statement about where the credential lives, and the inline
key is what fills the gap when nobody made one. This is why the setting exists —
the operator's credential for the *other* model behind the same endpoint
(`api_key`, reused by `vision_analyze`) already lives in `settings.json`, and the
voice credential had no reason to remain alone in a vendor's own directory.

**The nested parameter object, and why the engine is the OUTER key.** Each
engine owns a block, because the two cloud engines' parameter surfaces were
measured against each other and do not unify — the batch engine authenticates
with ONE key over TWO HTTP URLs with a submit-then-poll protocol, the streaming
engine with an APPID+TOKEN **pair** carried in an `Authorization` header and
inside every binary frame over ONE `wss://` socket. A flat surface cannot say
"`token` is not a legal key for `batch`" (it would either accept a document that
cannot run, or grow a vendor prefix until it is the old surface again), and a
per-engine BLOCK says it in the shape the operator reads. The three blocks and
their knobs are listed in §2c; the full argument, including the rejected
`provider` + `mode` alternative, is `devdoc/audio-engine-config-surface.md` §2.
A block or knob this build does not model is **named and refused**, never
ignored: `[gopi] renamed configuration: settings "audio_engine_param" carries
"recording.api_ky" (accepted here: api_key, key_file, api_base_url, resource_id,
hotwords)`.

**Two rules ride with that setting, and both are about the DOCUMENT.**

1. **A document carrying a credential is made private.** Before the value is
   read, `settings.json`'s mode is narrowed to `0600` when it is wider, and the
   change is announced on stderr naming the path and both modes
   (`internal/config/credential_settings.go`, `EnsureSettingsPrivateForCredential`).
   Measured on this host 2026-10-07: the live `~/.gopi/settings.json` is a symlink
   to `.ds_flash_settings.json` and its mode is `0664` — world-readable — while it
   already carries `api_key`. Writing a second secret into it without touching the
   mode would be a security downgrade dressed as a configuration improvement, so
   the file is what gets fixed, not the credential's location. The tightening
   follows the **symlink to the target** (that is where the bytes are, and it is
   what the write path's own `rename(2)` behaviour requires), it happens only when
   a credential VALUE is actually present (an empty or absent `doubao_api_key`
   leaves the operator's file untouched, mode included), and a document whose
   keys cannot be established is reported as "could not tell" rather than
   assumed clean.
2. **It has no CLI flag and is never republished.** A credential on the command
   line lands in the process table for every user on the host to read. And unlike
   every other settings knob, this one is **not** pushed into the environment by
   `config.ApplyEnv`: that environment is inherited by every child gopi spawns,
   so republishing the value would be a *second, wider* copy of the credential
   than the `0600` file. `GOPI_DOUBAO_API_KEY` is read when an operator exports it
   themselves (a catalog row that ignored it would be the silent failure Rule 15
   bans), and the help table says so. `gopi config show` renders the value as its
   shape — `<set: N chars>` — for every key in
   `internal/config.CredentialSettingsKeys` (the top-level spelling) or accepted
   by `config.IsAudioCredentialParamName` (a dotted leaf inside
   `audio_engine_param`, which the renderer prints one row per leaf precisely so
   that a nested credential has a name to redact by — a whole-block JSON cell
   would have none). `EnsureSettingsPrivateForCredential` asks the same two
   questions through `SettingsDocumentCarriesCredential`, so a document holding
   a **nested** live key is narrowed to `0600` exactly like a flat one.

#### Temp workspace

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `sandbox_dir` | `GOPI_SANDBOX_DIR` | `--sandbox-dir` | The root under which agent **sandboxes** live — the ONE standard private gopi profile (`gopi tool sandbox create`). Default `<home>/workspace/gopi-sandbox`; resolved by the same one-function/three-channel rule as `tmpdir`. Distinguished from `tmpdir` on purpose: `tmpdir` is a swept throwaway tree, the sandbox root is a tree whose entries carry an ownership marker and are reclaimed only when their owner is gone and they have aged out (`scratch.SandboxStaleAge`, 72h). A sandbox IS a `GOPI_HOME`: its profile lives at `<sandbox>/.gopi` (`config.ProfileDirIn`), which is what the initializer materialises and every reader resolves. ONE entry in that profile is a **symlink to the REAL operator file, read-only by contract** — `plugins` (the plugin tree the web-search scripts resolve under, so `GOPI_HOME=$SB gopi web search` reads the operator's real plugins instead of a directory nobody created); a host with no plugin tree is named at create time rather than left to be discovered by a failing search. The settings entry is NOT a link: it is a read-only **copy** (`0444`) of the operator's document, because a link was followed by a shell redirect and destroyed the live `settings.json` on 2026-09-27 (`internal/sandbox/settings_doc.go`) | `internal/config/sandbox.go:212`, `internal/scratch/sandbox_gc.go:307` |
| — (env-only) | `GOPI_SANDBOX_LEDGER_SEED` | — | A database **copied** to become a new sandbox's ledger (`<sandbox>/.gopi/memory.db`) — the sandbox's own **writable** ledger, never a symlink and never the seed itself. Unlike `settings.json` (a read-only 0444 COPY, never a link) the ledger is a WRITE TARGET, so sharing the read-only sample through it is exactly the defect this closes. Unset ⇒ the ledger starts `0600`-empty and the ledger's own first open initializes it. **Reachable from the verb as `gopi tool sandbox create|run --ledger-seed PATH`** (2026-11-01, task `01a19574`): that flag FEEDS `sandbox.Options.LedgerSeedPath` and does NOT re-read the environment, so the precedence above still has ONE site (`ledgerSeedPath()`); the em dash in this row's flag column is about the GLOBAL config parser, which has no `--ledger-seed`, and is why a caller who needs the seed can now name it without exporting anything. Set by the test scaffold (`testutil.SampleDBPath`, the ONE sample definition) or a script; a seed that is missing, is not a SQLite database, or lives **INSIDE the sandbox** (the self-referential shape: a sandbox configured by its own contents — the property the refuted symlink-shape branch guarded for its link, now enforced for the seed channel) **refuses the create**, loudly and by name | `internal/sandbox/ledger.go:130` |
| `tmpdir` | `GOPI_TMPDIR` (a caller-exported `TMPDIR` outranks it) | `--tmpdir` | The temp root gopi **publishes as `TMPDIR` for itself and every child** (task-run children, the bash tool, git). Default `<home>/workspace/_tmp`. A value that cannot be created or written fails **loudly**, naming path + reason + channel — gopi never falls back to the ambient `/tmp` | `internal/config/tmpdir.go:79`, `internal/scratch/tmproot.go:786` |
| `tmpdir_headroom_bytes` | `GOPI_TMPDIR_HEADROOM_BYTES` | — | Free-space **floor** (in bytes) the resolved temp workspace is compared against, measured with `statfs` on the partition holding it — the criterion that turns "the temp root is nearly full" into a named line instead of a `no space left on device` printed from an unrelated package's link step. Default `2147483648` (2 GiB, `scratch.TmpDirHeadroomFloorBytes`): the smallest build this host runs, so below it the next `go build` is a coin flip. **LOUD, never fatal** — the run continues and the warning names path + device + free bytes + the reclaim candidates (all of them report-only, none deletes). A malformed value is reported and falls back to the default, never read as `0` (which would disable the criterion silently). The verdict is also a row of `gopi config show` (`temp root headroom`) | `internal/scratch/tmproot.go:1025` (`TmpDirHeadroomFloorEnvName`), `internal/scratch/tmproot.go:1118` (`os.Getenv`) |

| `dev_repo` | `GOPI_DEV_REPO` | — | **THE OPTIONAL THIRD SOURCE of the run-repository chain**, and — when it names a `host:path` — **THE CLONE CHANNEL'S SWITCH.** A run resolves its repository in ONE fixed order (`internal/reposource`): ① the row's own `deliverable.repo` → ② the current working directory's git repo → ③ this value → ④ **nothing left, and only then a refusal** whose text lists each source's own measured verdict plus one remedy per source (`gopi config show` prints the resolved answer read-only as its `run repo` row). **Empty/absent is NORMAL, not deprecated**: a run started inside any git repository works, and nothing is printed — the empty-dev_repo deprecation line and its mute key were withdrawn on 2026-10-23. On the clone channel's own terms: A `host:path` naming the repository a task WORKSPACE is cloned from — the same spelling a `repos` entry's path uses (`peer1:/home/user/work/gopi`). **Non-empty = the clone channel**: `gopi worker` (same command, same arguments, same exit-code meanings) clones `dev_repo` into `workspace_root/<task-id>` and per task runs **clean → `pull --rebase` → the declared build → `push`** (clean FIRST: the workspace is checked with `git status --porcelain` and a workspace with uncommitted paths is REFUSED in the channel's own words — the offending paths, up to `devworkspace.MaxReportedDirtyPaths` of them, plus git's own listing — because git would otherwise answer `cannot pull with rebase: You have unstaged changes` with exit 128; untracked files do not block, and the channel never stashes, commits or cleans), returning to the pull when the push is REFUSED (origin moved first), up to `devworkspace.MaxPushAttempts` (3) attempts with exponential jittered backoff (0.5s / 2s / 8s). **A refusal is not the end of the run**: each blocked step comes back as a `devworkspace.Blocker` (kind `dirty`/`pull`/`build`/`push`/`disk-exhaustion`/`man`/`test-compile`/`empty`/`delivered`/`inherited` + the tool's output VERBATIM) and the run gives it to ONE agent round (the workspace path and those words, no step list) before trying again — at most `devworkspace.MaxDeliveryRounds` (3) delivery attempts; a successful push RECLAIMS the workspace (the code is at origin), and when every attempt is blocked the run STOPS, records the reason on the row, KEEPS the workspace, writes `gopi-push-brake.txt` into it for an exhausted push brake, and spends ONE closing round (not counted against the limit) having the agent write the report. **Empty/absent = today's worktree channel, unchanged byte for byte** — this key never changes the meaning of any existing key, and an older binary ignores it. There is deliberately no third boolean: the value IS the switch. **A row's ADDRESS and its DELIVERY DESTINATION are two different refs** (`devworkspace.Config.GroundBranch` / `.BaseBranch`): the workspace STANDS on and `deliverable.branch` records the row's own `task/<id>`, while the loop's `pull --rebase` and its `push` both address the INTEGRATION branch — the server does not carry the row's ref before its first delivery, so pulling it by its address fails outright (`fatal: couldn't find remote ref`, git 2.43.0), and that address is what makes `gopi task for-branch task/<id>` resolve back to exactly one row. The build command is only ever the one declared in `repos.<path>.build` (never discovered — a repo that declares none refuses the run) | `internal/config/devrepo.go:78` (`DevRepoConfigured`), `internal/config/devrepo.go:130` (`CloneChannelConfigured`), `internal/devworkspace/devworkspace.go:848` (`AttemptDelivery`), `cmd/agent/devworkspace_channel.go:131` (the ADDRESS/DESTINATION stamp), `internal/agent/cloneground.go:162` (the same stamp on the bare-prompt face) |
| `workspace_root` | `GOPI_WORKSPACE_ROOT` | — | The **reclaimable** parent directory the clone channel opens one per-task subdirectory in (`workspace_root/<task-id>`). Default `<home>/workspace` — the SAME root and meaning the worktree channel uses (`worktreeroots.DefaultRoot`), so the two channels coexist. The subdirectory exists only for the task's lifetime: it is deleted once that task's push lands. A relative value is an ERROR rather than a relative root, and `~` is expanded by the one tilde reader (`worktreeroots.ExpandTilde`) — and a value that cannot be made absolute is an ERROR rather than a relative root. **The SOURCE label the reader returns is the ROOT RESOLVER's, never this key's** (`WorkspaceRootConfigured`): `worktreeroots.Resolve` consults `GOPI_WORKTREE_ROOT` BEFORE the declared value, so with both set the label names the variable that actually decided the PATH and appends the outranked declaration and its value in parentheses (`settings worktree_root / GOPI_WORKTREE_ROOT (overrides GOPI_WORKSPACE_ROOT=/x)`) — one key with two values, stated instead of silent. Two channels naming the SAME directory are not a conflict and are labelled as both | `internal/config/devrepo.go:103` (`WorkspaceRootConfigured`), `internal/devworkspace/devworkspace.go:458` (`Open`), `internal/devworkspace/devworkspace.go:814` (`Reclaim`) |

Note the asymmetry that trips readers up: **`TMPDIR` is what gopi writes; a
caller-exported `TMPDIR` is only an input.** `GOPI_TMPDIR` exists so the knob's
own name is never simultaneously its input and its output.

#### Where a task's workspace lands, and which repository `task_submit` defaults to

Two questions this page answers only in fragments, and one report that used to
answer them WRONG (bug measured 2026-11-13).

**"unrecognized key" means "no production reader", never "not a struct field".**
`gopi config show` marks a settings key `(unrecognized key — no version of this
build models it, so it is IGNORED)` by asking `config.ModelledSettingsKeys()`
(`internal/config/settings_write_guard.go`). There are **two** shapes of
production reader, and the list folds in both:

| shape | example | how it is read |
|---|---|---|
| a decoded `Config` field | `tmpdir`, `repos`, `sync_peers` | the struct's json tag (reflection) |
| a **top-level-only** key, deliberately NOT a struct field | `dev_repo`, `workspace_root` | `Config.rawTopLevel` → `rawTopLevelString` (`internal/config/devrepo.go`), registered in `topLevelOnlySettingsKeys` |

The second set is **not transcribed** anywhere: `topLevelOnlySettingsKeys`
carries the key constant *and* the reader together, so "modelled" cannot drift
from "read" (Rules 1b/13/14). Before 2026-11-13 the answer came from the struct
alone, so `config show` called `dev_repo` and `workspace_root` IGNORED while the
same binary's worker banner — in that run's own log — printed
`clone channel ON (dev_repo=peer1:/home/user/work/gopi via settings dev_repo; workspace_root=/home/user/workspace via settings workspace_root)`
and built its ground under it. A key with **no** reader is still marked, which is
what makes the note useful for a typo.

**Where a per-task workspace lands** — `workspace_root` (above), and the
configuration verb now PRINTS the resolved answer with the channel that decided
it, because the raw key value is not the answer on a host that overrides it:

```
$ gopi config show | grep 'workspace root'
  workspace root         /home/user/workspace (from settings workspace_root)
$ GOPI_WORKTREE_ROOT=/x gopi config show | grep 'workspace root'
  workspace root         /x (from settings worktree_root / GOPI_WORKTREE_ROOT (overrides settings workspace_root=/home/user/workspace))
```

The row is `config.WorkspaceRootConfigured`'s own `(path, source)` pair — the
same call the worker, the clone channel, the reclaim sweep and the submit-time
ground test make (the count is a measurement, not a claim:
`grep -rn 'WorkspaceRootConfigured()' --include=*.go internal/ cmd/ | grep -v _test.go | wc -l`
prints `15` on this rev, one of which is the definition itself at
`internal/config/devrepo.go:103`, i.e. **14 call sites**) — so the report cannot
disagree with the run it describes.

**There are TWO independent chains under this one row, and they are NOT stages
of each other.** Reading them as one list is the mistake this paragraph exists
to prevent, because they answer different questions and never consult each
other:

| resolver | decides | precedence (highest first) | call sites |
|---|---|---|---|
| `config.WorkspaceRootConfigured` (`internal/config/devrepo.go:103`) | the root the **clone channel** opens per-task clone grounds in | `GOPI_WORKTREE_ROOT` (ambient pin, see below) > `GOPI_WORKSPACE_ROOT` > settings `workspace_root` > `<home>/workspace` (`worktreeroots.DefaultRoot`) | 14 |
| `config.ResolveRepoWorktreeRoot` (`internal/config/config.go:1527`) | the root a **task's ground / worktree** is created under | `GOPI_WORKTREE_ROOT` > `repos[<repo>].worktree_root` > `<home>/workspace` (`worktreeroots.DefaultRoot`) | 8 |

The second row **never reads `GOPI_WORKSPACE_ROOT` and never reads settings
`workspace_root`** — `RepoConfig` has no `workspace_root` field at all
(`grep -rn 'json:"workspace_root"' internal/config/` prints nothing; the only
root tag there is `json:"worktree_root,omitempty"`, `internal/config/config.go:657`), and `ResolveRepoWorktreeRoot` passes exactly one
declared value to `worktreeroots.Resolve` (`internal/worktreeroots/roots.go`),
the repo's own `worktree_root`. So a repo declaring a `worktree_root` does **not**
move the clone channel's root, and declaring `workspace_root` does not move a
repo's worktree root. Measured with one settings file holding **only**
`repos["/home/user/work/gopi"].worktree_root=/home/user/workspace/BB`
(no `workspace_root` key anywhere):

```
$ GOPI_SETTINGS=$W/settings2.json gopi config show | grep -E '^  workspace root|worktree_root:'
  workspace root         /home/user/workspace (from default)
      worktree_root: /home/user/workspace/BB (settings worktree_root)
```

Two rows, two directories: the clone channel fell through to the default while
the repo's worktree root was the declared `BB` — the two chains disagreeing is
the normal case, not a defect.

`GOPI_WORKTREE_ROOT` is shared by both, which is why it looks like one chain: it
is the **ambient pin** every root knob honours (Rule 2's ONE reader,
`worktreeroots.Resolve`'s first branch), and when it wins the row's `source`
NAMES the declaration it outranked rather than silently dropping it. Measured,
same settings file with both declared:

```
$ GOPI_SETTINGS=$W/settings.json gopi config show | grep -E '^  workspace root|worktree_root:'
  workspace root         /home/user/workspace/AA (from settings workspace_root)
      worktree_root: /home/user/workspace/BB (settings worktree_root)
$ GOPI_SETTINGS=$W/settings.json GOPI_WORKSPACE_ROOT=/home/user/workspace/CC gopi config show | grep -E '^  workspace root'
  workspace root         /home/user/workspace/CC (from GOPI_WORKSPACE_ROOT)
$ GOPI_SETTINGS=$W/settings.json GOPI_WORKSPACE_ROOT=/home/user/workspace/CC GOPI_WORKTREE_ROOT=/home/user/workspace/DD gopi config show | grep -E '^  workspace root'
  workspace root         /home/user/workspace/DD (from settings worktree_root / GOPI_WORKTREE_ROOT (overrides GOPI_WORKSPACE_ROOT=/home/user/workspace/CC))
```

Neither resolver has a CLI flag of its own; `--workspace-dir`-style overrides do
not exist, and the only ambient channel is the two env vars above.

**Which repository `task_submit` defaults to: THERE IS NO SETTINGS KEY, and none
is invented here.** `taskSubmitResolveDefaultRepo`
(`internal/tools/tasks.go`) asks the **current working directory's durable
repository** and nothing else — not `dev_repo`, not the `repos` table. Measured
on this host, a non-repo cwd is REFUSED rather than defaulted:

```
$ cd /tmp && gopi task submit "probe"
gopi task submit: task submit: --repo default: repo "/tmp" is not a git work tree (git rev-parse failed: exit status 128: fatal: not a git repository (or any of the parent directories): .git)
$ cd /tmp && gopi task submit "probe" --repo /tmp
gopi task submit error: task_submit: repo "/tmp" is not a git work tree (git rev-parse failed: exit status 128: fatal: not a git repository (or any of the parent directories): .git)
```

So a submit from outside a repository names its repo explicitly — `--repo <path>`
on the CLI, the `repo` parameter of the `task_submit` tool — and THAT is the
supported answer. `dev_repo` feeds a *different* question (which repository a RUN
works in — `internal/reposource`'s ① row `deliverable.repo` → ② cwd → ③
`dev_repo`) plus the clone channel's switch; it does not feed submit's default.

**`compute_nodes` is RETIRED (deleted 2026-10-22, `bf87a5d03`), and what its
`repo` field meant is worth stating because it is routinely mis-remembered.** The
whitelist carried `{name, max_concurrent, report_to, repo}` and its `repo` was
the **git push-back target for a dispatched run's finished branch** (`git push
<repo> HEAD`) — never "the default repo when submitting from a non-repo
directory". It went with its only would-be consumer (`dispatch.go`'s
`runDispatchOnce`, zero call sites: `gopi task serve` is a retired-daemon refusal
and `gopi task run --nodes` went with that verb). A settings file still carrying
it is reported by `gopi config show` from the ONE retirement registry
(`internal/config/retired_settings_keys.go`) — what it did, why it went, and what
to do instead — rather than by the generic "unrecognized" one-liner, which cannot
tell a typo from a deliberate removal.


#### Scratch sweep (resident GC)

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `scratch_gc` | `GOPI_TEST_SCRATCH_GC_MIN` | `--scratch-gc` | Cadence in **minutes** for the scratch sweep (the tick is `scratchGCTick`, whose production door is `gopi tool scratch-gc --tick` — corrected 2026-10-30: no resident worker/merger loop fires it). Settings can only turn it **off** (`"scratch_gc": false` publishes `GOPI_TEST_SCRATCH_GC_MIN=0`, the single reader's off value). Unset = enabled | `internal/scratch/gc.go:116`, `internal/scratch/gc.go:118` |
| `scratch_gc_stale_min` | `GOPI_TEST_SCRATCH_STALE_MIN` | `--scratch-gc-stale-min` | Stale window in **minutes**: an unheld scratch entry older than this is garbage. Default `360` (6h). A **held** entry is never collected. A malformed/non-positive value is reported and falls back to the default — it is *not* an off switch (the cadence knob is) | `internal/scratch/gc.go:92`, `internal/config/config.go:1253` |
| agent-side Go toolchain cache bound (dir resolved as `--path` > `$GOCACHE` > go env file > `<user cache dir>/go-build`) | `GOPI_BUILD_CACHE_MAX_GIB` | *(none — env only)* | The size bound of the **agent-side** Go toolchain cache, in **GiB** — the cache `go` itself fills (its DIRECTORY resolved with the toolchain's own precedence: `--path` > `$GOCACHE` > `GOCACHE` in the go env file `~/.config/go/env` > `<user cache dir>/go-build`; the pre-fix resolver skipped the file channel and reported a cache `go` was not using), as opposed to the merge gate's own per-repo cache. `gopi tool scratch-gc gocache` measures the cache BODY against it and prints the **GRAIN** the figure is counted at (`grain=cache-entry`): the cache **ENTRY**, i.e. the `<hex>-a` index / `<hex>-d` data file pair inside a two-hex shard, which is the unit the toolchain's own trim reclaims (`cmd/go/internal/cache`'s `trimSubdir`). `--trim` therefore removes whole cache ENTRIES, OLDEST FIRST down to the bound, keeping any entry whose newest file is inside the `scratch.ToolchainCacheEvictMinAge` (24h) grace window — so a shard holding one fresh entry still gives up the cold entries beside it, which is what makes a reclaim bite on a LIVE cache (every shard of which holds something fresh). No shard DIRECTORY and no root file is ever removed, so the toolchain's `README`/`trim.txt`/`log.txt`/`CACHEDIR.TAG` bookkeeping survives; the shard-grain reading is still printed as `shard_reclaimable=` for comparison. When a cache is over its bound with nothing past the grace window, the arm prints a sentence saying so rather than a bare `reclaimable=0`. A malformed/non-positive value is reported and falls back to the default `25` (`scratch.DefaultToolchainCacheMaxBytes`); the invocation's own `--max-gib` outranks it. Measured subject (host-b, 2026-10-12): this cache held 121 GiB and the partition holding it (`/dev/nvme0n1p7`, device `66309`, `/home`) was 96% full while `/` (device `66307`) had 131 GiB free — the measurement Rule 22's off-`/` test cannot make | `internal/scratch/toolchain_cache.go:358`, `internal/scratch/toolchain_cache.go:72` |
| holder-scan wall-clock bound | `GOPI_HOLDER_SCAN_DEADLINE_SEC` | *(none — env only)* | Wall-clock bound, in **seconds**, on ONE `scratch.LiveHolders` / `scratch.ScanHolders` pass over `/proc` (cwd + exe + every fd + a maps file per process). Default `8` (`scratch.HolderScanDefaultBudget`) — measured on this host: one scan of 478 processes costs 1.35–1.53 s and yields ~56 000 paths. `0` (or any non-positive value) is the deliberate **unbounded** form; a malformed value is reported and falls back to the default — it is *not* an off switch. Past the bound the scan stops before the next process and reports `Truncated`: every **delete** caller then keeps the candidate (fail-closed — "not scanned" is never read as "not held"), and the report says the scan was truncated rather than claiming a holder it never saw | `internal/scratch/holder.go:192` |

#### Resident merge tick — RETIRED 2026-09-27

There is **no** row for this knob any more, and that is the change: `merge_tick` /
`GOPI_MERGE_TICK_MIN` used to set the cadence of a worker-round pass that spawned
`gopi merge --fast` as a **blocking** child (both the verb and its `--fast` arm
are since gone: a merge pass is `gopi task merge`, which has no such flag). The
knob, its publisher
(`config.PublishMergeTickMin`), its single reader and the worker-loop call site are
**deleted** — the worker must not merge (a merge is `gopi task merge`'s job; a worker
running one stops running tasks while two live workers contend for the merge flock).
Report: `devdoc/worker-merge-tick-retired.md`.

**SETTING EITHER SPELLING IS REPORTED, NOT SILENTLY IGNORED.** An operator whose
environment exports `GOPI_MERGE_TICK_MIN`, or whose `settings.json` still carries
`"merge_tick"`, is told on the worker start path that the knob is
retired, why, and what to run instead (`run \`gopi task merge\` in the foreground`) —
the same loud-retirement shape the retired `gopi merger` daemon uses. The settings
face is reported even though the decoder ignores an undeclared key silently, so the
file must be **read** to notice it (`config.Config.SettingsCarriesKey`,
`cmd/agent/retired_tick_knobs.go`). The file keeps loading: a settings.json written
while the tick existed is still valid, with every other knob intact.

**WHAT DRIVES UNATTENDED MERGING NOW: NOTHING, BY DESIGN.** There is no timer, no
systemd unit, no cron entry, no daemon and no replacement driver — and none is to be
added (Rule 35). `gopi task merge` is a **foreground command an operator runs**
(until 2026-11-01 this row read `gopi merge`; that top-level verb was removed by
the merge-removal epic and `./bin/gopi merge` exits **1** today); the
merge work lock and the queue are unchanged (the `--fast` arm left with the
verb — the atom has no such flag, `gopi task merge --fast` is refused as an
unknown flag).

#### Memory-asset sync — the STORE trigger, and the RETIRED resident tick

**Retired 2026-10-25: the resident memory-sync tick.** There is **no**
`memory_sync_tick` row any more, and that is the change. `memory_sync_tick` /
`GOPI_MEMORY_SYNC_TICK_MIN` used to set the cadence of a worker-round pass that
spawned `gopi memory sync --peer P --push` as a **blocking** child — each fired
run held the round for **315–534 s**, measured in `~/.gopi/logs/worker.log`. The
knob, its publisher (`config.PublishMemorySyncTickMin`), its single reader, its
catalogue row and the worker-loop call site are **deleted**; the spellings survive
only as `config.MemorySyncTickRetiredEnvName` / `MemorySyncTickRetiredSettingsKey`,
which the retirement report names back at an operator still setting either face
(`cmd/agent/memory_sync_tick_retired.go`, called from `tmpDirBootstrapFromArgs`).
Report: `devdoc/memory-sync-tick-retired.md`.

**WHAT DRIVES UNATTENDED CONVERGENCE NOW: NOTHING, BY DESIGN** — so two hosts'
knowledge bases **diverge** until a human acts. There is no automatic leg and no
replacement driver, and none is to be added (Rule 35).

**A HUMAN CLOSES THE GAP BY HAND, and there are two doors — one converges, one
copies.** The converging door is `gopi memory sync --peer <alias>`
(`cmd/agent/cli_memory_sync.go`), restored 2026-11-06 as a MANUAL verb: ONE
unique-only session against a declared `memory_peers` peer, bidirectional unless
`--push` / `--pull` names one direction, idempotent (identity is the exact
question, an existing row is never touched, so a re-run after convergence imports
~0). The copying door is `gopi memory backup`: it snapshots this ledger (SQLite's
own `VACUUM INTO`), verifies the artifact read-only, reports path + size + sha256
and ships it over one ssh leg to a declared `memory_peers` peer (`--serve` is the
peer side, ssh'd by the initiator). It imports nothing back, so the peer's
knowledge base is replaced, not merged; restoring a snapshot onto a live ledger is
a manual `mv` onto a STOPPED one (`gopi memory backup -h`).

**WHAT DID NOT COME BACK WITH THE VERB, and must not be re-added:** the `--every
<dur>` periodic loop (it re-ran the session on a cadence — the knob's env face is
still `config.MemorySyncEveryRetiredEnvName`, its reader is gone), the
`memory_sync_on_store` trigger (a store never spawns a peer session) and the
resident worker/merger tick (the 315–534 s rounds above). `gopi memory sync` had
been deleted 2026-11-05 by `5677cc6e` along with the rest of the cross-node sync
CLI family — the TOOLS layer underneath it (`MemoryAssetsPeerSessionMode`,
`MemoryAssetsServeRemote`, `MemoryAssetsDryRun`, the `memory_peers` whitelist) was
never removed, which is why the restoration is a wiring job, not a rewrite.
Report: `devdoc/memory-sync-manual-door-restored.md`.

<!-- settings key | env | flag | semantics / default | read at -->
| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `memory_sync_on_store` | `GOPI_MEMORY_SYNC_ON_STORE` | *(none — there is no flag face)* | **RETIRED 2026-10-27** (epic `memory-store-sync-decouple`): the key still PARSES and still has exactly ONE reader, `config.ResolveMemorySyncOnStoreDirection`, but **every well-formed value now resolves to `off`** — `push`, `pull`, `off` and an absent key all mean the same thing, because the leg this knob configured (`memory_store` spawning an asynchronous `gopi memory sync` child) **no longer exists**. A successful store's criterion is the LOCAL `/home/user/.gopi/memory.db` write and nothing else: no child process, no ssh leg, no peer's ledger lock, and nothing in the store's reply mentions a peer, a push or a sync. Values are still validated — a malformed value (`pushh`) is an **error**, never read as a deliberate `off` — and an operator whose env or settings file still names `push`/`pull` is told on the start path (`cmd/agent/memory_sync_on_store_retired.go`, `reportRetiredMemorySyncOnStoreKnob`, called from `tmpdir_inject.go` beside the retired memory-sync tick's report) rather than left believing it fires something. **Those manual spellings are themselves RETIRED (2026-11-05)**: `gopi memory sync`, its `--every` loop and the `--push`/`--pull`/`--serve-assets` directions were deleted with the cross-node sync family, so `reportRetiredMemorySyncOnStoreKnob` still tells the operator the truth about THIS key (`off`) but the replacement it must name is `gopi memory backup` — a snapshot plus its ssh ship leg, not a peer-synchronised ledger. `memory_peers` survives as `memory backup`'s whitelist. Retiring the trigger retired three failure shapes with it: the 176–307 s rounds that held a PEER's write lock while re-pushing this node's whole corpus, the orphaned `gopi memory sync` child a quit chat left at PPID=1, and the close-out that KILLED an in-flight round. See `devdoc/memory-store-sync-decouple.md` | `internal/config/memory_sync_on_store.go:72` |

**There is no store trigger left to have properties.** `memory_store` writes the
row, returns, and that is the whole of it: a store can be told apart from a failed
store by the row being readable, and by nothing else.

**The manual verb those guarantees belonged to is gone as well (2026-11-05).** The
paragraph that used to stand here described the contract of the since-DELETED
`gopi memory sync` — one session per ledger, enforced by a kernel flock on
`<ledger>.memory-sync.run`, a concurrent run refused in 18 ms with status `3` and
`another sync is already running against <db> (one session per ledger) — nothing
was done`, and one appended line per failed or refused run in
`<profile>/logs/memory-sync.log`. **None of that runs now**: its dispatcher
`cmd/agent/cli_memory_sync.go` was deleted by `5677cc6e` with the whole cross-node
sync CLI family, so there is no session to lock, no status `3` to test for, and no
new `memory-sync.log` line to grep. What the operator holds instead is the one-way
copy `gopi memory backup`, whose own guarantees are this page's `memory backup`
rows and `gopi memory backup -h`; its history is below, marked as history.

```
2026-10-27T06:57:38+08:00 memory-sync-failed peer=nosuchhost.invalid mode=push exit=1 elapsed=0.0s db=/…/memory.db last="tools.MemoryAssetsPeerSessionMode(peer=nosuchhost.invalid, mode=push) returned: memsync assets: ssh nosuchhost.invalid: exit status 255 (stderr: ssh: Could not resolve hostname nosuchhost.invalid: Name or service not known)"
```

  **Historical record only — nothing appends this line any more.** The shape a
  failed run used to write: the peer, the mode, the elapsed time, the **exit
  code**, the ledger and the **last action** the run took, verbatim; a refused
  concurrent run carried the different token `memory-sync-busy`. The file was
  append-only, with no rotation and no reader inside gopi — an operator's `grep`
  was the consumer, in the same directory `worker.log` uses. Since `5677cc6e`
  deleted the verb, `grep memory-sync-failed /home/user/.gopi/logs/memory-sync.log`
  reaches only lines a PRE-deletion binary wrote: it is evidence about the past,
  never a way to check what an operator's copy did today.

**Read the retired tick's own contract nowhere: it is gone.** Its reasoning lives
in `devdoc/memory-sync-tick-retired.md`; the page below describes only what still
runs.

#### Task ledger: identity and topology

| settings key | env | flag | semantics / default | read at |
|---|---|---|---|---|
| `role` | `GOPI_NODE_ROLE` | — | `server` (dispatcher) or `client` (compute node). Declared, never hostname-guessed; an unknown spelling = undeclared | `internal/config/node_role.go:52` |
| `sync_to` | `GOPI_SYNC_TO` | — | **Delivery destination** of this client: the ssh alias its finished task branches are handed to; empty on the server. Resolved in exactly one place — `config.ResolveNodeRole` (env over settings) — and read by the delivery alias selector `peerRemoteDeclaredAlias` (`internal/tools/peer_remote_target.go`, which takes it FIRST and falls back to the first usable `sync_peers` entry), so it is the alias the finish-cleanup push, the peer push-back and every other peer-pushing leg dial. A declared value that cannot be handed to git/ssh (option-shaped, or carrying a colon) is never silently dropped: the `sync_peers` fallback is taken **and** the resolution's diagnosis names the declared value, the env var, git's/ssh's own refusal and the destination actually in force | `internal/config/node_role.go:52`, `internal/tools/peer_remote_target.go:281` |
| `sync_peers` | `GOPI_SYNC_PEERS` (comma list) | — | **Task-ledger** peer whitelist (ssh aliases). Empty = single-node task mode: all cross-node task machinery short-circuits. **The task-ledger verbs that consumed it (`task sync`/`diff`/`harvest`/`push`/`send`/`receive`/`pull`) were DELETED 2026-11-05**; the key, its reader and the adoption sweep survive | `internal/config/config.go:410` |
| `task_sync` | `GOPI_TASK_SYNC` | — | **Task-row** synchronization switch, **default OFF**. OFF makes every AUTOMATIC task-ROW crossing a no-op (claim broadcast, terminal-state push, serve-tick delta pass, submit forward). **ON is NOT a working cross-node protocol, and this row says so plainly rather than implying one behind a flag:** the MANUAL verbs (`task sync`/`diff`/`harvest --peer`, `task push`) **were DELETED 2026-11-05**, and every peer leg the AUTOMATIC crossings spoke over is **RETIRED** (`internal/tools/remote_leg_retired.go`) — so with ON each crossing refuses **locally and immediately**, naming the removal, without opening a connection: a `role: client` submit is **refused** (`task_submit: REFUSED — this row was NOT written to any local ledger …`) and the run's end hook logs `terminal push: FAILED`. OFF reports nothing because it runs nothing. A row crosses hosts no longer through a ledger-to-ledger protocol but by being filed on the host that will run it (`gopi task submit` there, Rule 28 / Rule 40). It does NOT gate the CODE-DELIVERY axis: a finished task's branch push-back, the serve git/ref pass and the adoption sweep are decided by the declared topology (`sync_peers` + role), so ONE declared peer means one coherent "code delivery is on" state while rows stay independently off. The serve-tick legs' one-shot face was `gopi task sync-pass --once`, **also deleted 2026-11-05**; the automatic tick is what runs them now, and the verb that used to reach the daemon (`gopi task serve`) is a retired-daemon refusal. Independent of `role` (identity: conflict arbitration + branch push-back), the removed `compute_nodes` (dispatch) and `memory_peers` (knowledge assets), none of which it gates. OFF is also the upgrade posture: a node whose settings still carry `role: client` + `sync_to` does not start forwarding. The startup banner names WHICH of the three channels decided the value: `off (default — …)` when neither channel mentions the knob, `off (declared in task_sync: false — …)` when the settings file carries the key, `off (GOPI_TASK_SYNC: false — …)` when the env var supplied it (and the same channel name on the ON line) — so an operator can tell "my declaration was read" from "nobody declared anything" without decoding a value that is identical in both | `internal/config/task_sync_switch.go:100` |
| `memory_peers` | `GOPI_MEMORY_PEERS` (comma list) | — | **Memory-asset** peer whitelist, deliberately decoupled from `sync_peers` so asset sync survives single-node task mode | `internal/config/config.go:848` |
| `chat_op_auto` | `GOPI_CHAT_OP_AUTO` | — | The **chat console's standing posture** for the ONE class whose minutes the operator must authorise — a LONG operation — **default OFF = all-ask**. The criterion it feeds (task 01a14731, operator ruling) judges a chat-initiated operation by the **CATEGORY OF THE ACTION**, never by a clock: `tools.ChatOpClassify` places every bash command in exactly one of three classes and `tools.ChatOpAdmit` decides it. **(a) OPERATOR WORK** — ops / DevOps / the network / a script / a document / a query / a diagnosis / a bug row / ledger-and-housekeeping edits / a short job — **RUNS, with no permission question at all, at every posture**: it is the work the operator would otherwise have done by hand (`ChatOpClassOperatorWork`). **(b) REPOSITORY WRITE** — a source-tree edit (an in-place `sed -i`, a `gofmt -w`, a redirection into `internal/`/`cmd/`), the project's gate run over that change (`make test-budget`, `go test ./internal/...`, `golangci-lint run`), or a `git add`/`commit`/`push`/branch move — is **REFUSED, and no posture opens it**: it is a task (`gopi task submit`), because a chat owns no branch and no worktree. **(c) LONG OPERATION** — the whole-tree suite (`make test`, `go test ./...`), the full build (`go build ./...`), a package install, a `gopi task merge` / `gopi worker` / `gopi task sync` sweep — **must be asked about first**, and THIS knob is the operator's standing answer for that one class: absent or false = ALL-ASK (ask every time), true = the console may also start an unauthorised long operation itself. Both vocabularies are the exported tables `tools.ChatOpLongOperationVocabulary` / `ChatOpRepoWriteVocabulary` — the SAME words the prompt renders and the refusal names, so a class cannot move on one side only. The call's own `timeout_sec` is deliberately NOT consulted any more: it is the bash tool's EXECUTION CAP (it kills the process group), never a declaration of what the action IS. The authorisation for a long operation is read from THIS turn's operator message (`at most 5 minutes` / `give it 10 minutes`) as the MINIMUM named, never stored. Resolved by `tools.ChatOpChannel` (env > settings > default), the ONE reader; an unrecognised spelling is reported on stderr and left to the settings channel, an unreadable settings file lands on all-ask. Configuration only — the whole criterion is stated in `devdoc/chat-ops-authorization.md` | `internal/tools/chat_ops_authorization.go:316` |
| — | `GOPI_MEMORY_SYNC_TIMEOUT_SEC` | `gopi memory sync --timeout SECONDS` | **LIVE again since 2026-11-06: BOTH faces.** The absolute bound of ONE cross-node asset-sync ssh leg, in seconds (default `900`). It was RETIRED 2026-11-05 when `5677cc6e` deleted the verb that published into it (`--timeout`) and left the machinery with no production caller; the verb came back as a MANUAL door (`cmd/agent/cli_memory_sync.go`), so the flag publishes into this variable and an operator may equally export it. **The flag wins when both are set** — one reader, `assetSyncSSHBound` (`internal/tools/memory_sync_assets.go`), reads the variable; the CLI exports the flag into it. A malformed or non-positive value is an error, never a silent default. Note the bound must exceed the peer's apply time: the leg carries this node's WHOLE corpus and the peer applies every row before answering (measured 2026-09-26: 5209 rows / 15 MB, 47.5 s) | `internal/config/config.go:869` |
| — | `GOPI_TASK_SYNC_FULL_TIMEOUT_SEC` | **REMOVED 2026-11-05.** It overrode the ceiling of the ssh leg of a FULL reconciliation, and that leg (`SyncFullReconcile` → `syncPeerSessionFull`) was deleted with the rest of the cross-node sync surface — no production caller remained (`gopi task sync --full <peer>` no longer exists). The work-sized model it overrode went with it (`internal/tools/memory_sync_full_bound.go`, deleted); the surviving delta leg keeps its fixed `15s` stall detector and no operator knob raises it. A reader who still exports the variable changes nothing | — (removed) |
| `repos` | — | — | The per-repo table; see §2d | `internal/config/config.go:623` |
| `notify` | `GOPI_TASK_NOTIFY_WEBHOOK` / `_CMD` / `_EVENTS` / `_PATTERN` / `_ASYNC` | — | Durable task-notify section; see §2e | `internal/config/config.go:745` |

#### Task staleness — the stale-REPORT grace

Resolved in one place, `config.ResolveTaskStallGrace`
(`internal/config/config.go:1085`): CLI > env > settings > default. A negative
value is an error; an explicit `0` **disables** the knob.

| settings key | env | semantics / default | read at |
|---|---|---|---|
| `task_stall_sec` | `GOPI_TASK_STALL_SEC` (legacy `GOPI_TASK_STALE_SEC`) | Heartbeat-stall grace in seconds: an `in_progress` row whose recorded liveness fact (last tool batch, else the tasklist block) is older than this renders as **heartbeat stale** in `gopi task list` / `gopi task show <id>`. Default `1800` (30 min); `0` disables the marker. **It is a DISPLAY threshold only** — the stale sweep that used to REPORT (and, before that, dispose) rows is deleted 2026-11-05, so nothing fires on this value, nothing is written, and gopi never disposes, retries or rewrites a stale row (`devdoc/zombie-sweep-removed.md`) | `internal/config/config.go:565` |

**Two knobs were REMOVED on 2026-10-26, with the mechanism they served.**
`task_max_run_sec` / `GOPI_TASK_MAX_RUN_SEC` (the wallclock backstop) and
`task_dispose_sec` / `GOPI_TASK_DISPOSE_SEC` (the auto-disposal grace) existed
only to feed the zombie sweeper, and the sweeper is deleted whole. They are gone
from the settings catalog, the resolver and every consumer — **a value still set
in a deployment is ignored, not read**; `gopi config show` does not list them. A
knob nothing reads is worse than an absent one: it reads as a working control
surface while no code consumes it, so leaving them behind as accepted-and-ignored
keys was not an option (Rule 21).

#### `gopi task verify` — the child's wall-clock cap

Resolved in one place, `config.ResolveTaskVerifyTimeout`
(`internal/config/config.go:1034`): explicit CLI `--timeout` > env >
settings > default. Negative or malformed values are **errors** (never a
silent fallback) — this cap decides how long a caller PARKS, so guessing is
the wrong repair. A timed-out verify records **no verdict**: the caller gets
one machine-readable result line and exit **4** (timeout) or **5** (any other
no-verdict exit); a judged run exits **0** and the verb's help page tabulates
the whole contract.

| settings key | env | semantics / default | read at |
|---|---|---|---|
| `task_verify_timeout_sec` | `GOPI_TASK_VERIFY_TIMEOUT_SEC` | Wall-clock cap in seconds on the child `gopi task verify` spawns; CLI `--timeout N` wins over both channels. Default `1800` (30 min) — a **bound**, because the pre-fix default `0` (unlimited) parked unattended callers forever. `--timeout 0` is the explicit, deliberate "unlimited" escape and is the ONLY way to get one | `internal/config/config.go:1001` |

| `GOPI_SYNC_NODE` | Node identity override for sync **self-tagging** (same-host two-node simulations); default = hostname | `internal/tools/memory_sync_delta.go:277` |
| `GOPI_SYNC_FULL_EVERY` | **REMOVED 2026-11-05.** The periodic-full backstop it paced had no production caller left once the cross-node sync surface went: `SyncFullReconcile`, the per-peer counter and the `MustSyncFullEvery` report face were deleted together with the knob, so the row survives only to tell a reader who still exports it that nothing consumes it | — (removed) |
| `GOPI_ADOPTION_INTERVAL` | Serve-tick adoption-sweep interval in seconds. Default `900`; `0`/negative = disabled | `cmd/agent/cli_tasks_adopt.go:42` |
| `GOPI_WORKER_MAX_ROUNDS` | Worker loop round cap. `0` = unlimited (drain-then-exit) | `cmd/agent/cli_tasks_worker.go:79` |
| `GOPI_WORKER_IDLE_EXITS` | Worker self-exit after N consecutive empty-queue rounds. Default `3`; `0` = wait forever (the pre-fix behaviour) | `cmd/agent/cli_tasks_worker.go:119` |
| `GOPI_SKIP_CLAIM_BROADCAST` | Set to `1` to skip the claim broadcast after a task claim | `internal/config/settings.go:22` |
| `GOPI_ANCHOR_GUARD` | Set to `0` to disable the memsync anchor guard | `internal/tools/memory_sync_reconcile.go:52` |
| `GOPI_WORKER_IDLE_SEC` | Drained-queue re-check cadence in **seconds** for `gopi worker` — how long a worker whose queue looks empty sleeps before re-checking (default `30`; `--once` exits instead of waiting). Absent/`0`/negative = the default, and a malformed value is reported and the default kept (never a silent disable) | `internal/config/settings.go:43` |
| `GOPI_BREW_INTERVAL_SEC` | Pace of the **session-exit `gopi memory brew` trigger** (the same command, always through the memory group), in seconds (default `86400` = 24h; `0` disables the trigger entirely, `>0` overrides the interval). `gopi memory brew -h` advertises this knob — that page is where it is discoverable | `internal/tools/memory_brew.go:94` |
| `GOPI_WORKTREE_SWEEP_MIN_AGE_H` | Age threshold in **hours** below which `gopi task sweep-worktrees` never reclaims a linked-worktree registration: `gopi task sweep-worktrees --min-age-h N` (default `24`). The threshold is measured on **git's own registration record**, never the directory mtime, and the holder pass protects anything younger that a live process (`cwd`/`fd`/`exe`/`maps`) or a non-terminal ledger row still claims. A malformed value is reported on stderr and the default kept. Advertised by `gopi task -h` (the `sweep-worktrees` paragraph) | `internal/tools/worktree_sweep.go:419` |
| `GOPI_WORKTREE_SWEEP_MAX` | Cap on how many registrations **ONE** `gopi task sweep-worktrees` run reclaims — the blast-radius throttle: `gopi task sweep-worktrees --max N` (default `50`), so an unattended sweep that suddenly finds a hundred stale registrations takes a bounded bite. A malformed value is reported on stderr and the default kept | `internal/tools/worktree_sweep.go:429` |
| `GOPI_HIPPO_DB` | **Legacy alias** for the knowledge-base path, superseded by `GOPI_MEMORY_DB` / `--db` | `internal/config/memory_env.go:40` |

The last four rows are the **sweep/trigger knobs a command page advertises**:
each is named in the `-h` text of the verb it belongs to (`gopi worker`,
`gopi memory brew`, `gopi task sweep-worktrees`), and each has its catalog row here so
`gopi --help-env` — the table those pages point a reader at — documents it too.
A page advertising a variable whose name the catalog does not carry is the
defect `internal/standards/help_env_catalog_gate_test.go` makes unrepresentable:
the reader is told to set a variable that no operator-visible index lists, and
no existing gate could see it (the doc/catalog gates compare the two halves of
an omission and find them consistent).

The same rule produced the row below, which is `gopi task update`'s escape rather
than a pacing knob: it too is advertised by a command page (`gopi task -h`'s
delivery-proof paragraph), so `gopi --help-env` must list it. It has no settings
key and no flag of its OWN — `--force` is the typed face of the same decision,
not a second knob — which is why it sits here instead of in the §2c table.

| env | semantics | read at |
|---|---|---|
| `GOPI_TASK_DELIVERY_PROOF_ALLOW` | Operator's escape from the close-out **delivery proof**: truthy releases a `completed` writeback whose delivery claim could not be measured, and the release is **recorded** on the row's trail as `delivery-proof-unverified` (`gopi task show` renders it), so "proven" and "let through by decision" stay distinguishable. The escape does **nothing** else: the measurement still runs, the dirty-close-out, residue, tip and merge-backlog gates never see the value, and no path but `taskUpdateDeliveryForced` accepts it, so the harness writeback and the merger cannot inherit a release nobody typed. Absent or false = OFF (the default: an unprovable delivery claim refuses the write). The typed twin is `gopi task update <row> --force`; both faces are ONE decision (`deliveryProofAllowanceFor`), so an env-only run, a flag-only run and a run with both cannot diverge — and the record says which asked | `internal/config/settings.go:60` (the constant) → `internal/tools/task_delivery_proof.go:1239` (the ONE reader, `config.EnvBool`) |

#### Harness pacing and auxiliary tooling

| env | semantics / default | read at |
|---|---|---|
| `GOPI_NUDGE_ROUNDS` | Rounds without a task list before the harness nudges. Default `5` | `internal/agent/loop.go:3513` |
| `GOPI_NUDGE_TOOLS` | Tool rounds without a task list before the harness nudges. Default `3` | `internal/agent/loop.go:3519` |
| `GOPI_BASH_LOCALE` | Locale for bash-tool child processes. Default `C` | `internal/tools/bash.go:175` |
| `GOPI_MERGE_ANCHOR_GATE` | *(none — there is no settings key or flag face)* | The merge-time **PUBLISHED-ANCHOR gate** — the fifth machine check the merger asks before `git merge`, about the MERGE RESULT (the union of the two revisions, materialised through the union lane's own builder). It asks the `internal/gendoc` edge whether the tree about to land still publishes anchors its own sources resolve, and **refuses** the branch when the union's document of record fails the gate package's anchored document test while BOTH the target and the branch pass it alone — the drift shape no single revision can show (a branch that regenerated its document correctly, on a union where the other side moved the same anchor source). It is the only value the gate reads as a switch: `0` (`false`/`off`/`no` are NOT accepted — `0` alone) turns the lane into an abstention, which is also its red proof; anything else (or unset) leaves it ENABLED. Every other outcome abstains and is stated in the merger log: no published document, no edge, no anchor source in the union's change, a union the union lane could not build or that carries conflicts, a gate package the union cannot run, a closure that does not COMPILE (the union-compile lane owns that), a **red target** or a **branch that fails alone** (neither established anything about the union — measured 2026-10-13: the integration ref this lane was written against is itself red on `TestCommandsDoc*`), an unusable scratch tree or shared cache, a budget that cannot be read, and a failure the runner reported without naming a failing test | `internal/tools/merge_anchor_gate.go:94` (the constant) → `:1076` (the ONE reader, `mergeAnchorGateDisabled`) |
| `GOPI_MERGE_ANCHOR_GATE_TIMEOUT_SEC` | *(none — there is no settings key or flag face)* | Per-command budget, in seconds, for each measurement the **published-anchor gate** runs (the union's document tests and the two premise legs). Unset or empty = the union lane's own default (`mergeUnionGateDefaultTimeoutSec`), so the merge-time gates cannot disagree about how long a measurement may take. A malformed or non-positive value is an **error the gate abstains on** — never a silent fallback to the default | `internal/tools/merge_anchor_gate.go:102` (the constant) → `:1057` (the ONE reader, `mergeAnchorGateTimeout`) |
| `GOPI_MERGE_LOCALE` | Locale pinned onto every git child of the **merge lane**. Default `C`, because the lane **classifies** a probe merge by reading git's own diagnostics (since 2026-09-26 the index is the primary evidence and the prose only the fallback, but a translated diagnostic still breaks anything that reads it). Replaces the whole gettext family (`LANG`/`LC_ALL`/`LC_MESSAGES`/`LANGUAGE`) — appending `LC_ALL` does not override an inherited `LANG`. Precedent: `GOPI_BASH_LOCALE`. The one-shot `gopi merge` face that owned this pin was deleted (epic merge-verb-removal, item 3); the reading now lives in the CLI suite's shared git helper, and the tool-side half (`tools.ChildLocaleEnvPinning`) is unchanged | `cmd/agent/gitcmd_test.go:36` |
| `GOPI_FILM_LOCALE` | Locale pinned onto the **filmed terminal child** of `gopi chat --film`. Default `C.UTF-8` (glibc's built-in UTF-8 locale — compiled in, nothing to install), falling back to whichever of `C.utf8` / `en_US.UTF-8` the host's own `locale -a` lists. The pin exists because a terminal DECODES its pty stream as text: under `LANG=C` (the ambient state of this host) VTE has no UTF-8 in its codeset and advances **one cell per BYTE**, so the same Chinese line films ≈15.41 cell-widths wide instead of ≈11.17 (measured 2026-10-04, red→green, `devdoc/film-child-locale-pin.md`). The value must be UTF-8 **and** installed; either failure is a **refusal naming the command, the reason and this knob** — never a fallback to the caller's locale, since an unverified pin is worse than none (`setlocale` returns NULL for an uninstalled locale and the process runs on in `"C"`, so the film would look fixed and film mojibake). The child's environment **replaces** every inherited `LANG`/`LANGUAGE`/`LC_*` (appending does not override: `getenv(3)` returns the first match) and the caller's own environment is never touched. Env-only, deliberately not a settings key. Precedents: `GOPI_BASH_LOCALE`, `GOPI_MERGE_LOCALE` | `internal/shot/filmlocale.go:273` |
| `GOPI_SHOT_CHROME` | Chromium binary for `gopi tool shot`. Default: the playwright cache, else `PATH` | `internal/shot/chrome.go:23` |
| `GOPI_CODE_SEARCH_ROOT` | Root directory the interactive code-search index is built from | `cmd/agent/commands.go:593` |
| `GOPI_WEB_ADDR` | `gopi web` listen address. Default `localhost:PORT`; a non-loopback bind is token-gated | `cmd/agent/cli_web.go:562` |
| `GOPI_WEB_TOKEN` | Pin the `gopi web` LAN access token. Default: a random per-run token | `cmd/agent/cli_web.go:996` |
| `GOPI_WEB_LOCAL_TRUST` | `0` removes the loopback trust shortcut, so even a local request must present the token | `cmd/agent/cli_web.go:791` |
| `GOPI_WEB_TOOLS` | Enable the `gopi web` / `gopi ui` agent tools — **any non-empty value** enables them | `cmd/agent/cli_web.go:967` |
| `GOPI_THINKING_LIVE` | Stream the model's LIVE REASONING to the carrier as `event: thinking` frames — one raw fragment per frame (`{"text":"…"}`, no marker, no SGR), so a client can draw the model's own thinking while it works. **Truthy-only** (`0`/`false`/empty = OFF, unlike its sibling above: a new knob has no history to preserve, and "`0` turns it on" is the reading nobody expects), **OFF by default** — with it off nothing is installed on the engine, so the wire carries not one extra frame. `--thinking-live` overrides it and is the SAME switch on both carriers (`gopi web` and `gopi ui` — one vocabulary); `gopi ui --server` REFUSES it, because that client starts no engine and a remote engine's own switches are its own. The frames are display-only content: they never enter the assistant history (that is `keep_thinking`'s axis, untouched), and a client that does not know the frame refuses it loudly rather than rendering it as the answer | `cmd/agent/cli_web.go:856` |
| `GOPI_ERROR_DUMP_DIR` | Directory for deepseek error-path dumps (debugging). **First-ranked source, and it is DURABLE** — an export from a shell profile is inherited by every gopi and every child it spawns, so the tree it names keeps growing after the debugging session ends. Default when unset: `scratch.DumpDir(minimalCoreDumpClass)` = the scratch dumps tree's `gopi-dumps/` class directory, off `/` (`internal/deepseek/minimal_core.go:43`). **Census it with `gopi tool scratch-gc families --tree scratch-dumps`** when the variable is unset — that name reaches the DEFAULT location (`<scratch home>/dumps/gopi-dumps`, bounded by the declared tree `scratch-dumps`) and it is the one that works out of the box; **`--tree dumps`** censuses the OTHER tree this knob can name, the PROFILE's `dumps/` (declared tree `sibling-dumps`), and on a default configuration that tree does not exist and the walk exits 1 — the same pre-fix reading, correctly reporting "nothing was ever pointed here" rather than "nothing to collect" | `internal/deepseek/minimal_core.go:36` |
| `GOPI_1214_DUMP_DIR` | Directory for error-1214 degradation dumps (debugging). Same precedence and the same durable-in-a-shell-profile caveat as `GOPI_ERROR_DUMP_DIR`. Default when unset: `scratch.DumpDir(guardDumpClass)` = the scratch dumps tree's `gopi-1214-dumps/` class directory (`internal/deepseek/error_1214.go:59`). Census with **`gopi tool scratch-gc families --tree scratch-dumps-1214`** for the default location (declared tree `scratch-dumps-1214`), or **`--tree dumps-1214`** for the profile tree the knob itself may have pointed elsewhere (declared tree `sibling-dumps-1214`; absent on a default configuration, and the walk says so) | `internal/deepseek/error_1214.go:52` |
| `GOPI_SESSION_BUDGET` | The session budget the **system prompt** teaches the model to plan against, in MINUTES — it is NOT a wall-clock cap (that is a different knob with a different job: see the `gopi task verify` section above). Unset = `60` on a **dispatched worker** (a planning ceiling with a real referent: an epic to split, a dispatcher waiting). **On `gopi chat` the default is PUBLISHED AS `0` = UNLIMITED**, and that is the whole point of this row: the tool-enabled chat path sends the same TASK prompt as a worker, so before 2026-10-07 a chat was taught a 60-minute ceiling it had no way to spend — measured on a PA chat that reached t+5527s, read itself as "92× over the budget" and proposed ending early, while the same prompt's own `[CHAT SURFACE]` block said a turn is "conversation, not a task run". The chat default is written at the launch seam (`cmd/agent/chat_session_budget.go`, called from `cmd/agent/cli_chat.go:runChatWithInput` — below the `-h`/refusal/`--film` returns, so a usage query changes nothing) and it is stated on stderr when applied. **It is a DEFAULT, NOT AN ENFORCEMENT**: an exported value — `GOPI_SESSION_BUDGET=20`, or `0` — is the operator's own decision and is honoured verbatim; `gopi worker` and every non-chat entry keep the `60` default untouched. `0` means unlimited end to end (no budget line, no escalation, no commit-beat refusal); a positive value overrides; an unparsable value is reported on stderr and the default kept | `internal/agent/loop.go:434`, `internal/agent/loop.go:2504`, `cmd/agent/chat_session_budget.go:88` |
| `GOPI_SESSION_BUDGET_REFUSAL` | The KILL SWITCH for the commit-beat gate that `GOPI_SESSION_BUDGET` arms: past `sessionBudgetWarnPercent` (75%) of the budget with tracked items still open, `task(action=commit)` refuses a further subtask until the run lands or splits. Unset = the gate is ON; `0`/`false`/`off` = OFF. It exists because enforcement must never be the thing standing between a run and its work — an operator who is blocked turns it off without rebuilding. A chat's `0` budget never reaches it (the gate is only armed by a budget there is a percentage OF) | `internal/agent/loop.go:450` |
| `GOPI_UI_DUMP` | `gopi ui` writes its OWN interpreted screen state to a text file so another process can read the surface as text instead of grabbing pixels. `1` (any truthy value) picks the path — beside the run's session artifacts, else a scratch directory — and prints it before the TUI takes the terminal; any other value **IS** the path. `--dump-screen[=PATH]` overrides it | `cmd/agent/cli_ui.go:458` |
| `GOPI_UI_DUMP_MODE` | How that dump interprets the render stream: `ansi` (default — escape sequences interpreted as the terminal would) or `plain` (bytes as literal text, LF/CR the only controls). An unparsable value is reported and the default kept. `--dump-mode` overrides it | `cmd/agent/cli_ui.go:634` |
| `GOPI_CHAT_DUMP` | `gopi chat` writes its OWN interpreted screen state — including the reserved answer band the layout paints — to `chat-screen.dump` so another process can read the surface as text (`gopi ui_state --target chat`) instead of grabbing pixels. **The dump is ON BY DEFAULT** (2026-10-01): the path is printed before the REPL starts, and `--no-dump-screen` (or a falsey value here: `0`/`false`/`off`/`no`/`disabled`) is the way out — a declined run hands the editor's paint stream through **unchanged** and is byte-identical to a run from before the capability existed. A truthy value (`1`) picks the path (beside this run's session artifacts, else a scratch directory); any other value **IS** the path. `--dump-screen[=PATH]` overrides it (it picks the DESTINATION, and re-arms under a falsey env). The flag wins over this variable. A run with no reserved layout (a small terminal, or piped input) still arms the sink and SAYS the grid will stay unpainted (`frames=0`) rather than leaving a blank grid a reader would misread | `cmd/agent/chat_dump.go:207` |
| `GOPI_SCREEN_STREAM` | The renderer's **RAW bytes** — every paint write, verbatim, unthrottled, no frame dropped — appended to <session-id>.screen.stream in the run's own session directory, as `len(\n)`-prefixed frames between a `#gopi-screen-stream …` header and a `#gopi-screen-stream-end …` trailer. It is the ALGORITHM-INDEPENDENT ground truth beside `chat-screen.dump`, which is this program's **interpretation** (throttled to `dumpMinInterval`, reduced to the cells the grid model expresses): the dump cannot tell a reader whether a wrong cell came from the renderer or from the interpreter, and these bytes can — they replay through any interpreter, and `gopi session replay <id>` draws them FIRST (`--from auto`), degrading to the dump only with an explicit self-reported line. **The name is terminal-agnostic on purpose**: the switch used to live in the chat flag namespace (`GOPI_CHAT_SGR_STREAM`), which meant a worker run — no screen of its own, but output a reader still wants verbatim — could not be asked at all. **ON BY DEFAULT FOR `gopi chat`** (operator ruling, 2026-10-31: the bytes of a paint sequence cannot be re-created afterwards, so a run that did not record has nothing to replay, while the cost of recording is one file beside the dump). Unset = **the SURFACE's own default**;

**THE PRECEDENCE IS GRADED, AND THE GRADING IS ONE FUNCTION** — `config.ScreenStreamResolve` (`internal/config/screenstream.go:170`), which every surface calls, so no surface can invent a fourth rung:

1. **argv — this run's own command line** (`--screen-stream[=PATH]` / `--no-screen-stream`). It wins outright: the words were typed one second ago. When BOTH spellings appear the walk applies them in argv order, so the LAST one wins, and the run announces the state it ended in.
2. **env — `GOPI_SCREEN_STREAM`** (`config.ScreenStreamFromEnv`, `internal/config/screenstream.go:108`). It wins over the surface default and over the pre-rename alias, and it is the ONLY channel a surface with no flag namespace has — which is why it exists: a worker run has no command line of its own to carry the word.
3. **the surface default** (`ScreenStreamDefaultChat` = armed, `ScreenStreamDefaultWorker` = declined), and its answer names `origin=default` so a declined run is never silent.

A settings key would be added at rung 3½ (CONFIG.md §1 puts a settings key below the environment); there is none today.

**AND THE WORKER SIDE NOW USES THE SAME SWITCH.** `gopi worker` arms the same `uiclient.SgrStream` (`cmd/agent/worker_screen_stream.go:136`), because the question an operator asks is not "which surface is this" but "what did this run put on the wire": a round's own record — the claim line, the workdir line, the close-out, and `worktreerelease.ReleaseReporting` — is a stream a reader may want verbatim. What it records is **the round's equivalent output**: the bytes the round wrote to its OWN stream, through the one stdout door (`workerRoundAnnounce`). It does **not** record a terminal's answer or a repaint — a worker that is not on a tty has no terminal to be equivalent to, and claiming one would be the dishonesty this epic forbids. **The worker's default is DECLINED** (`ScreenStreamDefaultWorker`): progress text is not a screen, so recording it is opt-in.

**A RUN THAT PRODUCED NOTHING SAYS SO AND LEAVES NO FILE.** An armed round with no bytes to record prints `本 run 无屏幕输出 (this run produced no screen output: …)` and **removes** the header-and-trailer-only artifact it opened, naming the path it removed — because a 0-frame stream on disk is exactly what a reader takes as evidence that a screen existed (`cmd/agent/worker_screen_stream.go:242`). The `ls` that finds nothing IS the evidence, and the removal line is what makes it re-derivable. A removed-unsuccessfully file is reported as its own finding rather than folded into the empty case. The loop face (an idle or drained queue that ran no round at all) opens nothing and prints the same sentence with `path=(none — no round ran)`.

`--screen-stream[=PATH]` overrides it — **joined form only**, because this verb's remaining positional argument is a session seed — and `--no-screen-stream` (takes no value) declines it; when both spellings appear the LAST one on the command line wins, and a declined run announces the decision at startup and again in its closing report (the off state is never silent). The off state costs the render path one nil check (no syscall, no allocation, no file): the artifact does not exist at all. On a `--film` command line both spellings are refused by name (the painting process is the second gopi inside the private terminal and does not inherit argv); the variable works there, since the producer inherits it. A replay's stream is redirected into the chat's scratch class, never the recording's session directory — the same contract `GOPI_CHAT_DUMP` keeps. **`GOPI_CHAT_SGR_STREAM` is the PRE-RENAME ALIAS and is still read** (`cliverbs.LegacyScreenStreamEnv`), with the current name winning when both are set; the run that finds only the legacy spelling says so on stderr, so a shell profile from before the rename is visible rather than merely tolerated. Files written before the rename (`.sgr.stream`, `#gopi-sgr-stream` header) are still READ by both `gopi chat --screen-replay` and `gopi session replay` — `uiclient.ScreenStreamSuffixes` is the one list both ask — but nothing new is written under that name | `cmd/agent/chat_sgr_stream.go:171`, `internal/config/screenstream.go:170`, `cmd/agent/worker_screen_stream.go:136`, `internal/cliverbs/cliverbs.go:586` |

## MERGE NOTE (rework run 01a0f645ab907201a9a0d124216a1215)

The conflict this section resolves was in the `GOPI_THINKING_LIVE` /
`GOPI_ERROR_DUMP_DIR` / `GOPI_1214_DUMP_DIR` rows above — both sides edited the
same three-row window.

**NOTHING from master was dropped.** The resolution is a true UNION:

- master's `GOPI_THINKING_LIVE` row — kept verbatim (it is untouched by the
  reworked branch, which never knew about the knob);
- the branch's two ENRICHED dump-directory rows — kept verbatim. They supersede
  master's short rows (`Directory for deepseek error-path dumps (debugging)`),
  which were dropped **because the enriched row carries master's whole content
  plus the precedence, the durable-in-a-shell-profile caveat, the resolved
  default and the `gopi tool scratch-gc families --tree` census path**. Every fact in
  master's short row survives inside the kept row; no master line is lost.

Both sides' rows are therefore present, which is why this file needs no
hand-written row: the union is a superset of both parents.

#### A/B and measurement switches

These are read in production but are not in the env catalog (they exist so a
measurement or a harness barrier can be armed without editing code):

| env | semantics | read at |
|---|---|---|
| `GOPI_AGENCY` | Enables the improvement-proposal pass (off by default; the read-only `gopi task improvements` ignores it) | `internal/config/agency.go:29` |
| `GOPI_AGENCY_MIN_FAMILY` | Recurrence-size gate for the proposal pass; malformed/non-positive falls back to the caller's default | `internal/config/agency.go:42` |

### 2d. The `repos` table — build contract, sweep list, worktree root

Each key is a **repo path**; the value declares that repo's contract
(`RepoConfig`, `internal/config/config.go:653`). The table is *also* the
merger's sweep list: every key is a repo the merger cd's to and sweeps each
tick (`ResolveMergerRepos`, `internal/config/config.go:1414`).

```json
"repos": {
  "/home/you/work/GoPi": {
    "build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/",
    "build_timeout_sec": 600,
    "worktree_root": "/home/you/workspace"
  },
  "/home/you/Downloads/games/Maps": {}
}
```

| key | semantics |
|---|---|
| `build` | Command that rebuilds this repo's deployable artifact, run in the repo with master checked out after a merge round. **Absent/blank = pure merge target**: swept, never rebuilt, no error (`ResolveRepoBuild`, `internal/config/config.go:1370`). The SAME command is the merge-time BUILD gate's first leg — and since 2026-10-12 a Go module gets a SECOND leg, the test-tree compile (`go test -run '^$' -count=1 ./...`), because this declared command compiles no `_test.go` file: measured, a branch dropping `internal/tools.NotifyEventLedgerDivergence` was green to it while a `cmd/agent` redproof test would not compile (`mergeBuildGateLegs`, `internal/tools/merge_build_gate.go`; no new key — the leg is derived from the checkout being a Go module) |
| `build_timeout_sec` | Build subprocess ceiling. `0`/absent → `GOPI_REPO_BUILD_TIMEOUT_SEC`, then 300 s; **negative = no timeout** |
| `worktree_root` | Where this repo's task worktrees belong. Absent → `GOPI_WORKTREE_ROOT`, then `<home>/workspace` (`internal/worktreeroots/roots.go:240`). In a **test process** the default is redirected under that run's scratch root, so a suite can never mint a worktree in the operator's real workspace (`internal/worktreeroots/scratch_pin.go`; an explicit `GOPI_WORKTREE_ROOT` or a declared `worktree_root` still wins) |
| `GOPI_FINISH_CLEANUP_BRANCH_CHECKS` | *(none — there is no settings key or flag face)* | The close-out's **branch-scoped test-run** switch (default **ON**; only the `0`/`false`/`off`/`no` vocabulary turns it off, `internal/boolvocab`). The lane it controls is what SURVIVED the removal of the client trial merge: it runs the branch's OWN tests **in the branch's own worktree, at the branch's own revision** — no fetch, no `git merge`, no merged tree, no rebuild (`finishCleanupBranchCheckVerify`, `internal/tools/task_finish_premerge.go`). Packages come from `internal/smoke.TouchedPackages(<ground>, <integration ref>, HEAD)` — the branch's own diff, which is the question the author can act on — and the command from `internal/smokegate.ScopedForPackages` (`make test-budget PKGS='…'`, never the whole-tree `make test`, Rule 27). It exists because `go build` compiles no `_test.go`: a red the branch itself introduces is invisible to every compiler. Every unmeasurable case (switch off, no ground on the delivered branch, a dirty ground, no declared scoped target, an integration ref that does not resolve, a diff naming no Go package, a budget trip, an unresolved module set) **abstains and is reported** in the step's detail (`— not measured: …`) — an abstention never blocks a delivery (Rule 15). A **refusal** is reserved for the branch's own red test run, and the verdict is written to the row's own trail either way (`branch-checks-verified` / `branch-checks-unverified` / `branch-checks-refused`); it is **recorded, not blocking** — whether the branch merges, and whether the COMPOSED tree is sound, is decided once by the merger on the integration branch | `internal/tools/task_finish_premerge.go:77` (the constant) → `:239` (the ONE reader, `finishCleanupBranchCheckOn`) |
 The **text is the reason** — the only way past the archive-why owner gate for unattended lanes. Absent (the default) = the refusal stands |

### 2e. The `notify` section

Durable task-transition notifications (`NotifySettings`,
`internal/config/config.go:729`). The section is the layer that survives a
restart: a daemon spawned without notify env still delivers to the configured
target.

```json
"notify": {
  "cmd": "bash /home/you/.gopi/notify/notify.sh",
  "events": "claimed,in_progress,completed,failed,stale,build-failed",
  "async": false
}
```

| key | env | semantics |
|---|---|---|
| `webhook` | `GOPI_TASK_NOTIFY_WEBHOOK` | URL that receives the JSON payload by HTTP POST |
| `cmd` | `GOPI_TASK_NOTIFY_CMD` | Shell command run with the JSON payload on **stdin**. The shipped SAMPLE is `bash ~/.gopi/notify/notify.sh` (repo copy: `scripts/notify/notify.sh`) — see the sample-sink paragraph below for what it does and the slot contract it reads |
| `events` | `GOPI_TASK_NOTIFY_EVENTS` | Comma list of statuses / synthetic alarms; `all` = every status plus every synthetic alarm. Default `claimed,in_progress,completed,failed,superseded` — every WRITABLE status must be a default member or a recorded exclusion (`taskNotifyEventsExcludedDefault` + `taskNotifyEventsMembers`, `internal/tools/tasknotify.go`), and `TestTaskNotifyEventsDefaultCoversEveryWritableStatus` fails the build on a status in neither |
| `pattern` | `GOPI_TASK_NOTIFY_PATTERN` | Glob filter over the task's tags + task id; empty = broadcast |
| `async` | `GOPI_TASK_NOTIFY_ASYNC` | Fire-and-forget delivery so a slow webhook never delays the ledger writeback |

**The sample sink and the chat-slot contract.** The shipped sample target is
`scripts/notify/notify.sh` (deployed to `~/.gopi/notify/notify.sh`, mode `0755`).
It exists because a chat console is NOT a descendant of the process that emits a
task transition: nothing in gopi hardcodes a notification mechanism (gopi execs
the configured command and hands the payload over on stdin), so "deliver this to
my chat" is the COMMAND's job.

- **The registry it reads.** A `gopi chat` run registers its surface identity in
  the profile root as `<profile>/chat_session_id_<0..9>.txt` — **10 slots, a
  fixed and bounded set** (`ChatSessionSlotLimit`, `cmd/agent/chat_session_slots.go:88`;
  registration at the chat ARM point, `cmd/agent/chat_surface_surface.go:495`).
  Each file holds the SAME value the environment channel publishes
  (`GOPI_CHAT_SURFACE_ID`, `<pid|self>:<session-directory>`), and the script
  hands the directory half to `gopi notify --session <dir> --message <summary>`.
- **No cleanup step, and none needed.** Registration is idempotent on the session
  (a re-registering run rewrites its own slot) and a NEW session with all ten
  taken replaces the OLDEST slot (smallest mtime) — which is the entry a crash
  left behind. A stale slot costs one refused delivery in the script's loop; the
  slot files are the only state, and nothing ticks, schedules or watches them
  (Rule 35).
- **The anchor is always written.** Whatever the ten deliveries do, the payload is
  appended to `<profile>/notify/task-events.jsonl` last, so a machine with no chat
  running still keeps the stream this document's consumer half reads.
- **The payload names the ledger it came from — when that ledger is not yours.**
  `taskNotifyPayload` carries `ledger` (`json:"ledger,omitempty"`), set by
  `taskNotifyOriginLedger` ONLY when the emitting ledger is not the profile's own
  `<gopi dir>/memory.db`, and `build_message` renders it as `ledger=<path>` in the
  one-line console message. The reason is a measured traceability gap, not
  decoration: `memory_db`/`--db` moves the ROW while the notify target keeps
  resolving from `settings.json` in the profile root (`GOPI_HOME` is the knob that
  moves BOTH), so a probe run that isolates only its ledger delivers events whose
  task ids exist in no ledger the console can open — measured 2026-10-23 on two
  such rows. The stamp is absent in the steady state, so an ordinary event's
  payload and message are byte-identical to before. See
  `devdoc/notify-event-origin-ledger.md`.
- **One consequence, stated rather than hidden.** Because the sample command is a
  SHELL SCRIPT, the sink-path recogniser has no file to derive from a command
  line: `gopi notify sink` reports `(none)` with `notify.cmd: bash
  ~/.gopi/notify/notify.sh`, and a reader must name the stream explicitly
  (`gopi notify events --sink ~/.gopi/notify/task-events.jsonl`). The recogniser
  is deliberately left as it is — guessing a path out of an arbitrary script
  would be the lie its own header refuses.

**Consuming the stream (`gopi notify`).** The section above is the PRODUCER
half; the consumer half is `gopi notify` (`internal/notifyfeed`,
`cmd/agent/cli_notify.go`). It adds **no knob of its own** — it resolves the
existing `notify.cmd` and recognises the file that command appends to
(`gopi notify sink` prints the path AND which channel decided it; an explicit
`--sink PATH` wins, and a command naming no local file — a webhook, an `ssh`
leg — is REFUSED by name rather than guessed). Two properties of the sink shape
a consumer must know, both measured on the operator's own sink (2026-10-01):

- **It is not line-oriented.** Every event delivered before the producer-side
  framing fix (e2b7e379, 2026-09-20) was appended with no terminator, so a
  pre-fix sink is ONE line of concatenated JSON (`wc -l` = 1) and a
  line-oriented reader reports 1 record for the whole file. `gopi notify` reads
  both framings.
- **One record in it is structurally damaged** (a 2026-09-12 probe inlined into
  a `zombie` field swallowed the next object's tail), so a strict decoder stops
  there — 92,134 records in. The reader skips the damaged span, reports it on
  stderr, and recovers the records after it.

A consumer that must resume across restarts passes `--cursor PATH`; the cursor
stores the offset plus the first unread record's identity, so a truncated or
rotated sink is refused (`ErrCursorStale`) rather than silently mis-resumed. The
full contract — the three subscription routes, the event classes, and the two
ways the stream's failure count differs from the ledger's — is
`devdoc/notify-subscription.md`.

### 2f. Environment-only knobs (no settings key, no flag)

A small set of knobs is deliberately **environment-only**. They describe the
TERMINAL the operator is sitting at rather than the behaviour of a gopi process,
so they belong in a shell profile — not in a shared settings file (which would
apply them to runs that are not at that terminal) and not on a per-invocation
flag (which would make them a thing to remember to type).

| env | values | semantics | read at |
|---|---|---|---|
| `GOPI_PALETTE` | unset (follow the terminal's depth), `basic`, `palette256`, `hermes`, `hermes-light` | Which emphasis table `gopi chat` and `gopi ui` draw. **Unset**, the table follows the terminal's own depth, so the default is deliberate on each tier rather than the stock console colours: `hermes` where `COLORTERM` advertises 24-bit (see the next row), `palette256` where `TERM` declares the 256-colour palette (contains `256color`), `basic` on the 8/16-colour floor. Named explicitly: `basic` = the 8-colour theme-independent table (the floor, unchanged); `palette256` = the gold family as xterm-256 entries (`38;5;n`: banner `#FFD700`, body cube 230, prompt cube 186) — fixed by the xterm standard, so the user's theme does not remap it; `hermes` / `hermes-light` = the Hermes-derived warm GOLD palettes (dark / light background), 24-bit, the answer body taking Hermes' own text colour (`#FFF8DC` dark, `#3D2F13` light). A table the terminal cannot draw is never emitted (a terminal that does not understand `38;2` or `38;5;n` prints its parameters as text): the run draws the deepest table the terminal CAN draw and prints ONE stderr line naming the table asked for, why it is not drawable, and `COLORTERM=truecolor` as the enabler. An unrecognised value is reported on stderr the same way and the run then follows the terminal's depth | `internal/lineedit/colour.go:497` |
| `COLORTERM` | `truecolor` (also matched as `TRUECOLOR`) | The terminal's own advertisement that it renders 24-bit colour. It is the ONLY truecolour signal gopi reads: no terminfo lookup and no probe (a probe would write bytes before the first frame). A `GOPI_PALETTE` naming a gold table on a terminal that does not advertise it is downgraded to the deepest table the terminal CAN draw (`palette256` where `TERM` declares it, else `basic`), with one stderr line naming this variable as the enabler | `internal/lineedit/colour.go:439` |
| `NO_COLOR` | any value, **presence is the signal** (empty counts) | No SGR byte anywhere in the chat chrome. Ranks below `--no-color` and above `TERM=dumb` | `internal/lineedit/colour.go:84` |
| `TERM` | `dumb` = no attributes at all; a value containing `256color` = the terminal declares the 256-entry palette | Two readings, one variable. `dumb`: the terminal declares it renders no attributes — colour off (the raw-mode editor still emits its own cursor placement and line erasing: those are layout, not colour, and no palette decision governs them). The `256color` marker (the TI/terminfo spelling: `xterm-256color`, `tmux-256color`, `screen-256color`): the terminal declares `38;5;n` support, which is what puts the run on the `palette256` table by default instead of the 8-colour floor. Detection is a case-insensitive substring test and NOT a terminfo lookup — a terminfo read is a new dependency for one bit of information the terminal already states | `internal/lineedit/colour.go:53` |

The precedence for the colour switch itself is unchanged and lives in ONE
function (`internal/lineedit/colour.go:84`, `ColourWanted`):
`--no-color` → `NO_COLOR` → `TERM=dumb` → the sink is not a terminal → colour on.
The palette SELECTION and BOTH depth advertisements (the `COLORTERM` truecolour
advertisement and the `TERM` 256-colour declaration) are the colour-ON side of
that same decision (`CapabilityForEnv`, `internal/lineedit/colour.go:439`) — the
single point where the tier is resolved, and the only reader of either signal —
so a redirected run emits nothing and prints no palette notice, whatever
`GOPI_PALETTE` says. The table is then chosen by `PaletteForCapability`
(`internal/lineedit/colour.go:329`), which is also where a downgrade is announced.

#### The delivery-proof escape is the one knob here that ALSO has a typed face

`GOPI_TASK_DELIVERY_PROOF_ALLOW` is read exactly like the knobs above
(`config.EnvBool`, no settings key), and it is listed here because it began life
environment-only. It no longer is: `gopi task update <row> --force` (epic
01a17dcd) is the same decision with a typed spelling, which is why this paragraph
exists instead of a row in the table — the section header's claim ("no … flag")
would be false for it.

The two faces are **one decision**: `deliveryProofAllowanceFor(flag bool)` reads
the variable and takes the flag, returns ONE boolean plus which of them asked, so
a flag-only run and an env-only run cannot diverge (and a run with both says
both). What the escape does NOT do is skip the measurement — the delivery proof
still runs, still reads the row's tip against its declared destination, and a
waived close still records `delivery-proof-unverified` on the row's trail (which
`gopi task show` renders). Nothing else is waived: the dirty-close-out, residue,
tip and merge-backlog gates never see this value, and no path but
`taskUpdateDeliveryForced` accepts it, so the harness writeback and the merger cannot
inherit a release nobody typed.

---

## 3. CLI flags

`--flag`, `--flag=VALUE` and `--flag VALUE` are all accepted. A boolean knob
takes an optional value (`--compact`, `--compact=false`, `--compact off`). Any
flag not in the table below is a hard **`unknown flag`** error — a typo is never
silently ignored. A **single-dash token** (`-z`, `-x`) is refused the same way:
gopi has no short options, and such a token is never taken as prompt text
(`internal/config/flags.go:75`) — pass the prompt as a positional, or use
`--prompt -z` to say it literally.

**Every catalogued knob can also be spelled with its full env name**:
`--gopi-timeout-sec 5` ≡ `--timeout 5` (`internal/config/flags.go:90`). This is
an escape hatch, not the primary spelling.

Flags whose name differs from the settings key:

| flag | settings key / env | note |
|---|---|---|
| `--api-key` | `api_key` / `ANTHROPIC_AUTH_TOKEN` **and** `DEEPSEEK_API_KEY` | writes both env names |
| `--base-url` | `base_url` / `ANTHROPIC_BASE_URL` **and** `DEEPSEEK_BASE_URL` | writes both env names |
| `--model` | `model` / `ANTHROPIC_MODEL` **and** `DEEPSEEK_MODEL` | writes both env names |
| `--db` | `memory_db` / `GOPI_MEMORY_DB` | a LEADING `--db` is a RUN flag (`gopi --db X <prompt>` records it on the run); the verbs that own a ledger flag take it after the verb (`gopi memory search q --db X`) |
| `--tool-timeout` | `tool_timeout_sec` | |
| `--bash-timeout` | `bash_timeout_sec` | |
| `--bash-idle` | `bash_idle_sec` | |
| `--timeout` | `timeout` | seconds |
| `--scratch-gc` | `scratch_gc` / `GOPI_TEST_SCRATCH_GC_MIN` | value is a cadence in **minutes**; `--scratch-gc=false` disables. `--scratch-gc 30` is accepted (a bare numeric value is consumed as the value) |
| `--home` | `GOPI_HOME` | also redirects the settings-file read |
| `--settings` | `GOPI_SETTINGS` | |
| `--prompt` | `GOPI_PROMPT` | `@file` expands |

**Subcommand flags are not top-level flags.** `--engine` (voice_input),
`--min-age-h` (task sweep-worktrees), `--nodes`, `--peer`, `--once`,
`--takeover`, … belong to their verb and are
documented by that verb's own
`-h`. The catalog deliberately leaves their `CLIFlag` column empty so the
top-level parser does not advertise (and therefore reject) them.

**One subcommand flag wrote an environment variable the loop reads — and it went
with the verb it belonged to.** The removed top-level merge verb's own
`--handover-budget MIN` flag (minutes;
default `45`, `0` = unbounded, negative refused) published `GOPI_TIMEOUT_SEC=MIN*60`
for the window in which the run started its hand-over child, because that is the
loop's OWN documented deadline (§3 above, read at `internal/agent/loop.go:3706`)
rather than a second bound invented beside it. That flag was parsed by
`cmd/agent/merge_handover_budget.go`, which was deleted with the one-shot merge
verb (epic merge-verb-removal, item 3): **no subcommand flag writes an environment
variable today**, and `GOPI_TIMEOUT_SEC` is reached only through the ordinary
precedence above. An ambient `GOPI_TIMEOUT_SEC` was never overwritten by that
channel and still is not.**

**Two flags are hoisted before the subcommand chain**
(`config.BootstrapEnvFromArgs`, `internal/config/settings.go:1088`):
`--settings/--home/--tmpdir/--scratch-gc-stale-min`. The group verbs that return
from `main()` at the subcommand dispatch (`gopi task run`, `gopi worker`, and the
retired `gopi merger` entry point, which refuses by name before it does anything
— `devdoc/merger-retired.md`) would otherwise never see the agent path's config block
— a `gopi --settings X task run T` would silently run under the default
provider. The hoist also **removes** those tokens from the argv the subcommand
chain dispatches on (`gopi --settings X task submit "a b c"` keeps its quoting).

---

## 4. Configuration by process form

Not every process reads every knob — which is exactly why one knob can look
"configured but ignored".

| process form | configuration path |
|---|---|
| bare `gopi <prompt>` / `gopi task run` child | full: flags → settings → env → default (`cmd/agent/main.go:710-769`) |
| `gopi worker`, `gopi task …` group verbs (and the retired `gopi merger` door) | `config.BootstrapEnvFromArgs` (hoisted flags only) → their own verb flags; the verbs then consume the resident-sweep and topology knobs (corrected 2026-10-30: the worker/merger tick loops named here are gone) |
| `gopi --help-env` | reads nothing — renders the catalog (`internal/config/settings.go:609`) |

Configuration that is **not** a knob but looks like one:

- **Node identity banner** — `role`/`sync_to` from settings (or their env
  overrides) produce the `[gopi] node: <host> (<role>→<sync_to>)` line
  (`internal/config/node_role.go:80`). The hostname is a *display label only*;
  behaviour branches read the declared role.
- **Task-sync banner** — the same banner prints the task-row-sync state on the
  next line, `[gopi] task-sync: on|off …` (`internal/config/task_sync_switch.go:213`).
  Unlike the identity line it is NEVER suppressed: "off" is the switch's
  DEFAULT, so an absent line could not be told apart from "nothing declared",
  and the operator must be able to see on the first screen why rows stop
  crossing the network. The line also names the DECIDING CHANNEL — `off
  (default — …)` vs `off (declared in task_sync: false — …)` vs `off
  (GOPI_TASK_SYNC: false — …)` — because "off" is two facts (an operator's
  declaration, and an absence) that carry the same value.
- **PROJECT.md / GOPI.md** are the agent's rule files, loaded from the run's
  cwd — resolved as an ordered pair, `PROJECT.md` first (`internal/agent/
  projectctx.go`). `PROJECT.md` is the PROJECT's engineering contract and IS
  **tracked** at this repo root (in `rootDocAllowlist`), so every clone and
  worktree carries it. `GOPI.md` is the CLIENT's own file — this machine's
  operations policy — and is deliberately **not tracked** (this repo's
  `.gitignore` carries no whitelist for it): each host's operator places it, and
  one machine's local policy never travels through a clone. A directory holding
  BOTH injects `PROJECT.md` and names the ignored `GOPI.md` on stderr; a
  directory holding only `GOPI.md` behaves as it did before the split. The
  read is unchanged in shape — `gopi --gopi-prompt 1 <verb>` still reports
  `loaded <name> (N bytes)`. The chat-policy and chat-op-category fences are
  read from the CLIENT's file with the opposite priority (they are local
  policy, and `PROJECT.md` carries none).
- **`.gopi/chat-policy.md`** — the **TRACKED project policy file**, and the one
  place a PROJECT declares its own action-class table: the `chat-op-category`
  rows that say which command lines are a repository write, a ref move, or a
  long operation. It is read at the CHECKOUT ROOT — the directory holding
  `.git`, whether that entry is a DIRECTORY (a clone) or a FILE (a linked
  worktree) — so it governs the whole project from any subdirectory, and it
  travels with the clone by construction: that is the difference from `GOPI.md`,
  which is a machine's own file and never travels. The built-in fallback names
  **no path at all** — a table compiled into a binary cannot know a project's
  layout, which is why the path rows moved out of it (the shipped table used to
  carry `internal/`, `cmd/` and `web/`, so in a project whose source lives in
  `src/` not one row matched and an in-place edit of its own source classified
  as operator work).
- **THE CHAT ACTION-CLASS PRIORITY: `.gopi/chat-policy.md` > the
  `chat-op-category` fence in `GOPI.md` > the built-in table.** Highest first,
  and the first channel that DECLARES a table is the only one that answers: the
  tracked project file **REPLACES** the built-in table, it never merges, so a
  word gopi happens to ship can never reach into a project that did not declare
  it (a merge would refuse a user's own `cmd/` as a repository write by a rule
  about somebody else's repository). Three states, each a different answer:
  **absent** → the next channel answers, which is the normal state of a project
  that declares no policy; **malformed** → the parse error is reported as
  `file:line: what is wrong` and the next channel answers, because a line you
  wrote must either take effect or be named, never silently dropped; **well
  formed** → it is the whole table for this project. A file that exists and
  carries **no** `chat-op-category` fence declared nothing, so that is reported
  and the next channel answers (reading it as "nothing is a repository write"
  would be a widening nobody declared). Grammar, error shape and tie rule are
  shared with the `GOPI.md` fence because all three channels go through ONE
  parser. Resolution and the three states:
  `internal/agent/chat_policy_category_file.go:133`;
  the walk to the checkout root:
  `internal/agent/chat_policy_category_file.go:75`; the priority applied
  once: `cmd/agent/chat_policy_flag.go:150`; the path-free fallback:
  `internal/chatpolicy/category.go:571`.

---

## 5. Traps worth knowing before you debug a "config didn't work"

1. **A zero value is not an override.** `"timeout": 0` means "use the code
   default" through the settings channel; only an explicit *flag* `--timeout 0`
   is honoured as "unlimited" (`internal/config/env.go:120`).
2. **A settings value never clobbers your shell.** If you exported
   `GOPI_TIMEOUT_SEC`, the settings file cannot change it. Unset the variable
   to let the file apply.
3. **A flag writes its env var *name*, not the settings key** — `--model` beats
   `ANTHROPIC_MODEL` *and* `DEEPSEEK_MODEL`, because the resolver checks the
   Anthropic name first and a shell export would otherwise win.
4. **An empty env value is still an explicit value** for the notify knobs (§1c)
   and for the fill loop's "already set" test — `GOPI_X=""` counts as *unset*
   for the fill loop, but as *explicitly empty* for `ResolveNotify`.
5. **`GOPI_HIDE_THINKING=0` does not hide anything.** Truthy-only parsing means
   only `1/true/on/yes/enabled` enable it. The old `!= ""` check inverted this.
6. **Malformed values are not silently ignored everywhere.** `sync_peers`,
   `memory_peers`, the effort list and the scratch stale window
   **report** their problem; grace periods error out on negative values. Read
   stderr.
7. **A renamed knob keeps its legacy spelling alive** for one alias:
   `GOPI_TASK_STALE_SEC` still drives the stall grace when
   `GOPI_TASK_STALL_SEC` is unset; `GOPI_HIPPO_DB` still supplies the ledger
   path when `GOPI_MEMORY_DB` is unset.
8. **A settings file read error can be silent.** `ResolveNotify`,
   `HasSyncPeers`, `HasMemoryPeers` and `ArchiveBuryOverrideFor` degrade
   fail-soft on an unreadable file (safe direction: no peers / no notify / no
   burial permission). The agent path, by contrast, **hard-exits** on a
   malformed settings file (`cmd/agent/main.go:737`).
9. **`--home` must be spelled before anything reads a path.** The flag is
   injected into `GOPI_HOME` ahead of the settings resolution; there is no
   ordering for you to remember, but a *settings file with a `gopi_home` key*
   cannot redirect the settings read (it is read after the path is fixed).
10. **The repo root is a hostile place for a config experiment.** A relative
    `--db memory.db` (or a probe's relative path) leaves a zero-byte sqlite at
    the repo root, which the tracked-junk gate will flag. Use a scratch path.

---

## 6. Where the rest of the story lives

| topic | document |
|---|---|
| Temp-workspace parameter (why it is not a banner) | `devdoc/tmpdir-param-not-banner.md` |
| Single-node shape (`role`, `sync_peers`, `task_sync`; `compute_nodes` was deleted 2026-10-22) | `devdoc/singlenode-config.md` |
| Config/options layer review — the P0/P1 findings behind many of the guards in this doc | `devdoc/review-cli-config-options.md`, `devdoc/review2-cli-config-options.md` |
| Reasoning-effort / effort-values design (and the measurement that the endpoint IGNORES both `reasoning_effort` and `budget_tokens`) | `devdoc/thinking-budget.md` |
| Zombie grace periods and auto-disposal | `devdoc/zombie-grace-disposal-2026-09-03.md` |
| Task-notify settings section | `devdoc/task-ledger-m46-47.md` |
| Repos table = merger sweep list | `devdoc/merger-config-sweep-2026-09-04.md` |
| Peak-hour guard removal (and why removed keys are still tolerated) | `devdoc/peak-guard-removal.md` |
| Making a fresh host run-ready in ONE run (`gopi config setup`, the config group) — what self-provisions and what must be provisioned | `devdoc/deployment-setup-verb.md` |

---

## Keeping this page honest

The catalog in `internal/config/settings.go:609` (`EnvVars`) is the machine
source of truth for env ⇄ settings-key ⇄ flag. This page adds what the catalog
does not carry: the **defaults, units, and which process reads what**.

TWO machine checks touch this page today, and (see ANCHOR SEMANTICS below)
**neither of them reads its `file.go:NNN` anchors**, so a stale anchor here is
invisible to the build:

- `internal/standards/sandbox_settings_doc_gate.go:111` — the sandbox-shape walk
  covers this page, so a sentence here calling a sandbox profile's
  `settings.json` a LINK, or stating the copy shape without `0444`, is red
  (`TestSandboxSettingsDocGateScansTheRealTree`).
- `internal/standards/scratch_gate.go:166` — the root-doc rule: a document of
  record at the repo root stays TRACKED and stays trackable through `/*.md`.

A THIRD check used to stand here —
`internal/standards/config_doc_flag_gate_test.go` (`TestConfigDocFlagColumnMatchesCatalog`,
§2c's CLI-flag cell against `EnvVarsFlagLabels`) — and it is gone with the same
teardown (`78bdf05a` deleted the file outright: `git log --oneline -1
--diff-filter=D -- internal/standards/config_doc_flag_gate_test.go` names that
commit). A FOURTH used to stand here too —
`internal/standards/testdata/config-anchor-census.go`, the census that MEASURED the
anchor population without judging it (A5 below) — also deleted in `78bdf05a`. So
nothing on this page measures the anchor population or the flag column today;
A1–A6 below are prose obeyed by authors and judged by no machine.

**WHO RAN `--check`, and when** (added 2026-10-27, retired 2026-11-05, after a drift
landed on master while every gate was green on the branch that caused it). The
measurement above WAS driven by `TestMergeConfigAnchorGate*` in `internal/tools/`,
which shelled the generator's own `--check` leg. Being wired was not the same as
being REACHED: the close-out's branch-scoped test run addresses the packages a
branch's diff touched, so a commit that moved a line in `cmd/agent/` ran
`./cmd/agent/...` and never `./internal/tools/...`. That hole was closed by adding
the gate's package to the SAME check list as a second member built from the tree
alone — no branch, no diff, so no diff decided whether it ran
(`make test-budget PKGS='./internal/tools/...'`). Both the arm and the leg it drove
were deleted in `b8cad761`/`0ee5745c`, so nothing adds that package any more and no
package abstention is needed either: there is no check to reach.
`devdoc/config-anchor-census-gate-scope.md`
is the measurement. **What these arms COST was measured too** (PAST tense: the arms
themselves are gone, the measurement is what a future census check should budget
against), because the package's ten-minute cliff invites the wrong repair: at
`153c41b` all 13 arms passed on the
DEFAULT path in `9.790 s` (`15.174 s` with an empty `GOCACHE`, so a fresh box paid
the full compile of the generator), and the cliff belonged to the whole
`internal/tools` PACKAGE — `397.080 s` at load 6.72 — not to that gate. So classify
no such arm heavy if one is ever written again: they were the only default-path
check over the 228-anchor
census, and a `heavytest.Require` arm does not run on the merge/verify path. The
measurement is `devdoc/config-anchor-gate-is-not-heavy.md`.

Two guards a reader might assume are on this page but are not, stated because
assuming them is the expensive mistake: `cmd/agent/usage_test.go:154`
(`TestTopLevelUsageDelegatesEnvVarsToHelpEnv`) keeps the CATALOG equal to
`gopi --help-env` and never opens this document; and
`internal/agent/gopidoc_sync_test.go:54` (`TestGOPIRule3_MatchesClaudeToolDefs`)
compares PROJECT.md's Rule 3 tool inventory with `claudeToolDefs` — it reads
PROJECT.md, not this page. Both are real guards; neither is a guard on CONFIG.md.

### ANCHOR SEMANTICS — what a `file.go:NNN` anchor on this page CLAIMS

A `path/file.go:NNN` reference on this page is a published, citable fact: it says
*the line that DECIDES what this row says is over there*. **No gate enforces
that, today.** The gates that once did left `internal/standards` whole in
`29882b3` (2026-10-22, doc-pipeline removal slice 1/5): the CONFIG.md document
gate, and the shared anchor-semantics implementation it was the only caller of.
No
replacement was written — a zero-dependency project does not grow a mechanism
back by accident (Rule 35), and reviving one is its own decision. What stands in
their place is a MEASUREMENT you run plus this page's own editing criterion:
**A1–A6 below are prose obeyed by authors, not by the build.**

The measurement it USED to be, and the three commands that re-derived it — kept
only so a reader can see what shape the measurement had:

> **Retired mechanism (documentation-citation teardown, 2026-11-05).** The three
> commands below **cannot run**: `internal/standards/testdata/config-anchor-census.go`,
> `citation-census.go` and `citation-selector-census.go` were deleted in
> `78bdf05a`/`b8cad761` together with the `.tsv` tables they wrote and the gates
> that drove them. `go run …` on any of them prints `stat … no such file or
> directory`; `go test -run TestMergeConfigAnchorGate` prints `[no tests to run]`
> and exits 0 (an EMPTY GREEN, not a pass). The anchor semantics A1–A6 below are
> therefore **prose obeyed by authors and judged by no machine** — which is what
> the paragraphs above already say, and the deletion is what makes it literally
> true. The only checks that still run over this page are the corpus gates:
>
> ```bash
> go test ./internal/standards/ -run RetiredPipeline -count=1
> go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader -count=1
> ```

```
# RETIRED — nothing below can run (see the note above); the paths are gone.
go run internal/standards/testdata/config-anchor-census.go . \
    internal/standards/testdata/config-anchor-census.tsv            # regenerated the table
go run internal/standards/testdata/config-anchor-census.go . \
    internal/standards/testdata/config-anchor-census.tsv --check    # re-derived; exit 1 on any delta
sed -n '1,12p' internal/standards/testdata/config-anchor-census.tsv  # the reading the (deleted) table carried
```

`internal/standards/testdata/config-anchor-census.tsv` **was** the table of record:
one row per distinct anchor in document order, the class of the line it lands
on, that line's excerpt, then a second half carrying the acceptance verdict A5
defines. **Its header names the generator and the reading it was measured at** —
a commit, plus a note when the worktree it was derived in had uncommitted reads
— so a census figure is never retyped into this prose (Rule 38): read the
header, and re-derive rather than quote. The anchor GRAMMAR
and the table's location **used to** be defined once, in the now-deleted
package `internal/configanchor` — the pattern itself in the constant
`RefPatternSrc` (the generator counted with it and the leg decided "still
published" with it, so the two could not disagree about which references
exist) and the paths in `TableRel` / `GeneratorRel` / `CensusCitationTables` —
so the generator could not measure a
different population than the one the table records (Rules 13/14) — **that
package is gone too** (`0ee5745c` deleted `anchor.go` + `publication.go`;
`internal/standards/citation_gate.go` + `citation_selector_gate.go` were the
last readers, deleted in `1e3b0976` together with the last path constants in
`census_tables.go`). There is no
live definition of the grammar: A1–A6 plus the tables' own recorded shape are
all a reader has. A `--check`
delta means the anchors moved: regenerate the TABLE, and re-point only the
anchors whose sentence is about the current tree (A6).

- **A1 — placement.** An anchor names the LINE THAT CARRIES THE CLAIM: the
  declaration the row names or quotes (`func isUIStateSubcommand`, the verb's
  routed predicate, the usage/title constant the entry quotes, a `type`/`var`/
  `const` that IS the thing described), or the FIRST LINE OF ITS DOC COMMENT. A
  line inside a usage string's body, and a member of a `const (` block, are also
  legitimate — the criterion does not judge them, because a legitimate anchor and a
  drifted one share that shape and a guess there would red the correct majority.
- **A2 — prohibition.** An anchor must NOT point at (i) a line carrying no
  content at all — only brackets or separators (`}`, `)`, `},`) — because such a
  line states nothing, whatever the sentence looks like; or (ii) a line that
  MENTIONS the symbol the row names without being its declaration — a CALL SITE
  above all (`runLogSubcommand(args[1:])` where the declaration is ten lines
  below; a caller decides nothing).
- **A3 — semantic distance.** A finding says HOW wrong the anchor is: where the
  declaration actually is and the gap. One or two lines is the signature of an
  anchor that DRIFTED (lines were inserted above it) — re-point it; a longer gap
  is the signature of an anchor typed onto another symbol.
- **A4 — out of scope**, stated rather than discovered by false positives: a
  path not ending in `.go`, a file with no top-level declarations, and a line of
  ordinary content that names nothing of the row's (a usage string's body, a
  table line, a quoted comment) — none of them is judged.

When this arm landed (2026-10-14) it found and this change fixed 14 anchors, 11
of them landing on a bare `}` or a blank line, and one — the `plugins.go` row —
drifted exactly one line into the body of the func it named (`:94` → `:93`).

**Rule for editors:** when a knob's default, unit or resolution order changes,
update the reader's line reference here in the same commit as the code. A row
whose "read at" no longer resolves is the exact rot this page exists to prevent
— which is why the census above is worth running before you hand the branch
over, even though nothing blocks on it.

- **A5 — the acceptance verdict, and the witness it is measured against.** A4
  above declares a line of ordinary content out of scope; that is a statement
  about what A1–A4 can judge, NOT a statement that such an anchor is correct.
  The 2026-10-15 audit closed that gap with a WITNESS this page supplies for
  free: **a row names its knob** (`GOPI_...`), and the cited Go file spells that
  same token as a quoted literal on the line that READS it — so an anchor can be
  settled mechanically, from the top down, instead of by reading the prose.
  `internal/standards/testdata/config-anchor-census.go` **derived** one verdict per
  anchor and wrote them into `config-anchor-census.tsv` (never hand-edit that
  table; regenerate it, Rule 38 — both the generator and the table are gone since
  `78bdf05a`, so the verdicts below are a RECORD, not something you can ask for
  today):
  - **AT-CLAIM** — the target line itself names the row's knob (its env-constant
    declaration, or the read). Settled: correct.
  - **AT-REGION** — the target sits in the COMMENT BLOCK that names the knob,
    which is one paragraph of prose about it. Settled: accepted on purpose —
    the row points a reader at the region, and re-pointing it at a single line
    would claim a precision the sentence does not have.
  - **CARRIER-AT:n** — the target is a comment line, its block names the knob
    NOWHERE, and the knob's ONE carrier sits BELOW it within 3 lines: the text
    moved under the number. **Not settled — re-point the citation at `:n`**
    (read `n` from the tree; never type it). This is the audit's whole worklist,
    and its size is reported as `unsettled (CARRIER-AT): N` in the table.
  - **REGION-UNWITNESSED / NO-KNOB** — no carrier inside the cited block, or the
    row names no knob at all. Settled: accepted — no mechanical witness exists,
    so nothing here is evidence of rot.
  Three conditions together identify the drift, and each was measured on this
  tree before it was written, because a wider rule red the correct majority:
  the target must be a comment; its block must not name the knob; and the
  carrier must be BELOW it. `internal/tools/memory_sync_reconcile.go:58` (cites
  the DEFAULT constant, a line of code, carrier two lines ABOVE) and
  `internal/scratch/gc.go:118` (a second claim of a multi-anchor row, carrier
  above) are the counter-examples those conditions exist to exclude.

- **A6 — the provenance criterion: re-point, or freeze.** An anchor whose
  sentence describes the CURRENT tree is re-pointed. An anchor whose sentence
  describes an OLDER tree is FROZEN and carried with its reason — re-pointing it
  would FABRICATE a citation for a state that no longer exists, and this page's
  own history is evidence its reader wants. The two anchors this page has moved
  twice, measured (`git show <commit>:<file> | sed -n Np`):
  - the settings read in `cmd/agent/main.go`: cited at **486** when the page was
    written, re-pointed to **706** (`b34c7f86d`, 2026-10-13) because the file had
    grown above it and the cited line had gone BLANK — the sentence is about the
    CURRENT read, so `:706`, the measured `config.FromSettingsFile(settingsPath)`
    line, was right for that tree. Then `612016700` (2026-10-13 18:22)
    refactored the file under it: the read landed at **699** and the WRITE of the
    hard exit at **711**, leaving `:706` inside a comment block, and
    `ab428b993` (2026-10-14) re-pointed its two citations (`:41`, `:971`) at
    **711**, the line that IS the malformed-file fact. (Re-derived 2026-10-15 with
    `git show 612016700:cmd/agent/main.go | sed -n '699p;711p'`: the refactor
    moved the file, not the sentence — the citation still names a line
    `main.go` has, which is why it was repairable.)
  - the `prompt` row's read in the same file: **805** → **1016**
    (`e75bd0959`, 2026-10-13 12:26 — the drift that left a NON-empty line
    behind) → **1009** (this audit), where `os.Getenv(config.PromptEnvVar)`
    actually is. Three ordinals for one sentence, each correct on its own tree:
    nothing here is frozen, because every one of these sentences describes the
    CURRENT reader.
  The 2026-10-15 audit re-pointed exactly three of the 194 anchors —
  the `prompt` reader in `cmd/agent/main.go` `:1016` → `:1009`, the worker
  round-cap constant in `cmd/agent/cli_tasks_worker.go` `:81` → `:82`, and the
  finish-cleanup branch-check constant in `internal/tools/task_finish_premerge.go`
  `:76` → `:77` — and the table's
  `unsettled (CARRIER-AT): 0` line is how the next reader sees that the worklist
  is empty.

## Keeping a census citation alive across a rebase (Rule 38's second half)

> **Retired mechanism (documentation-citation teardown, 2026-11-05).** The two
> tables this section is about (`config-anchor-census.tsv`, `citation-baseline.tsv`)
> and the generators that wrote them were deleted in `78bdf05a`/`b8cad761`; the
> commands in item 1 below **cannot run**. What follows is kept as the CRITERION a
> future census generator must meet, not as a recipe anybody can execute here.

`internal/standards/testdata/config-anchor-census.tsv` and
`internal/standards/testdata/citation-baseline.tsv` each carried a
`# measured-at: <rev> [provenance: committed]` line naming the revision their
reading corresponds to. That citation is the one identifier `git rebase` throws
away: a rebase REPLAYS commits, so a replayed commit keeps its tree and gets a NEW
id, and the old id survives only until `gc` collects it. The delivery loop runs
`clean → pull --rebase → …`, so a table citing the tip it was measured at cites a
commit the next delivery replaces — and "re-derive on the rebased tip" only moves
the citation onto the next tip that will be replaced. Measured over this history:
**18 of 18 citations naming a revision already published on the integration branch
survive; 8 of 8 naming an unpublished tip are now `fatal: Not a valid object
name`.** "It is my own parent" is a coincidence, not a criterion — 8 healthy
citations name a non-parent.

What the authors of those generators did, in the order it mattered:

1. **Cite a published revision when you have the choice.** Both generators read a
   revision straight out of the object store (no second checkout — Rule 29). The
   shape they accepted — retired with them, and the shape to rebuild if such a
   generator ever comes back — was:

   ```
   go run <generator>.go . <table>.tsv --print --at <published-rev>
   ```

2. **If a fresh tip is unavoidable, take the reading AFTER the last history
   rewrite.** The delivery loop's re-drive leg
   (`internal/devworkspace/census_redrive.go`, also deleted in `0ee5745c`) measured
   after its own `pull --rebase` for exactly this reason: the citation then named
   the tip that push publishes. A run that re-derives at its own HEAD and hands over
   measured one rewrite too early — that is the recurrence, stated as a schedule.
3. **Never hand-edit the `# measured-at:` line.** The generator was the only
   writer, and its write leg refused a reading that is not a commit.
4. **A `--check` red is not automatically the citation's fault.** A row delta
   (`-table` / `+tree`) means `CONFIG.md` moved and the TABLE must be re-derived;
   a bare provenance failure means only the citation died. Fixing the citation
   alone leaves a green citation over a wrong population.

**PUBLICATION was measured, so a dead citation could not wait for `gc`.** Both
`--check` legs asked three questions about the cited revision, and the third one is
the answer to "will a clone still have this?": does the object exist
(`git cat-file -e`), is it on this branch's history (citation table only:
`git merge-base --is-ancestor <rev> HEAD`), and does ANY REF reach it
(`git for-each-ref --contains <rev>`). The last one used to be missing, and its
absence was not a harmless gap: a citation of a commit a rebase had replayed and no
ref named printed GREEN — the object was still held, `--print --at <rev>` genuinely
reproduced the rows — and only `git gc` collecting that unreachable object turned
the table red, days later, for a reason no reader could still see. The two refusals
were deliberately separate sentences, `is NOT AN OBJECT in this repository` (the
rebase shape: the commit is gone) and `reachable from NO REF` / the PUBLICATION gap
(the commit is here and collectable) — same exit status, different finding,
different repair. A citation on a local unpublished tip still passed all three,
which is why points 1 and 2 above stay practice rather than a gate.

This section carries no `file.go:NNN` anchor on purpose: it sits after the last
anchored paragraph, so adding it moves no census row and the table's existing
citation stays true. The full mechanism is
`devdoc/census-citation-cannot-survive-a-rebase.md`.
