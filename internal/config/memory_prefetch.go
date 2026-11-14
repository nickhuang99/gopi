package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// memory_prefetch — the TYPING-TIME MEMORY PREFETCH switch (one knob, one
// reader, three channels, DEFAULT ON with an explicit OFF).
//
// WHY IT EXISTS (epic: retrieval the model never asks for is retrieval it pays
// for). Every existing memory read is model-driven: the model emits
// `memory(action=search)` and gets hits. That round costs a tool call, and by
// the time it lands the model has already committed to a plan. This knob arms a
// READ-ONLY background fetch that runs while the operator TYPES, so the hits are
// already in hand when the turn is sealed — the retrieval itself is token-free
// (Rule 35: no new persistent dependency; the record lands in the session
// directory that every run already creates).
//
// DEFAULT ON, AND OFF MEANS OFF (revised 2026-10-04 — the operator's ruling, and
// the reason is measured, not aesthetic): the whole gate was built and left
// disarmed, so the feature existed and never ran (Rule 11's failure mode). The
// evidence is the corpus itself — `find ~/.gopi/sessions -name memory-prefetch.jsonl`
// answered 14 out of ~83,000 session directories on 2026-10-06, i.e. the switch
// was armed for roughly one session in six thousand and the long sessions that
// motivated the gate had none: a build that never fires is not a feature, it is a
// cost that only the reader of the code pays. The default is therefore ON, and
// the OFF state is
// UNCHANGED and still exact: nothing is started, no goroutine exists, no query
// is issued, and the prompt is byte-identical to the pre-knob tree. That is why
// the switch is an explicit three-channel knob rather than a heuristic
// ("prefetch when the line looks long"): a heuristic would make the OFF state
// unobservable, and an operator could not turn the feature off.
//
// THE ONE THING A DEFAULT-ON KNOB NEEDS AND A DEFAULT-OFF KNOB DOES NOT: with
// the pre-fix default, an absent declaration and a declared OFF were the same
// value and the same ANSWER (off), so reading the settings key as a plain bool
// was harmless. Flipping the default makes them different answers — an absent
// key must now mean ON while `{"memory_prefetch": false}` must mean OFF — so the
// settings channel's declaration test is a POINTER (Config.MemoryPrefetch, the
// ScratchGC precedent): nil = nobody asked, non-nil = the file declared this
// value. The pointer is read in exactly ONE place, the resolution below.

// MemoryPrefetchSettingsKey is the settings.json key of the prefetch switch
// ("memory_prefetch": true). Exported so the resolution, the catalog row, the
// flag-arity table and the tests name ONE spelling (Rules 1b/13/14).
const MemoryPrefetchSettingsKey = "memory_prefetch"

// MemoryPrefetchEnv is the env override of the same knob
// (GOPI_MEMORY_PREFETCH=1) and the middle rung of the resolution. Rule 2:
// exactly one place reads it beyond the flag hoist, which writes this same
// representation.
const MemoryPrefetchEnv = "GOPI_MEMORY_PREFETCH"

// MemoryPrefetchFlagName is the CLI flag (`--memory-prefetch`). Its value is
// carried ON THE ENVIRONMENT (BootstrapEnvFromArgs) rather than on a second
// structure, so the flag and an operator's export are the SAME channel — the
// shape --output-language uses, and the reason the flag reaches `gopi chat`
// (a subcommand that returns from main() before the agent-path config block).
const MemoryPrefetchFlagName = "memory-prefetch"

// memoryPrefetchDefault is the switch's value when NEITHER channel declares it.
// Named (Rule 1) because the default IS the contract: ON — the typing-time fetch
// runs unless an operator says otherwise, and every way of saying otherwise (the
// flag, the env var, the settings key) is spelled on the two help surfaces and in
// CONFIG.md §2c.
//
// It is the ONLY place the default lives (Rule 2): the resolution below returns
// it, and no other file compares against a literal.
const memoryPrefetchDefault = true

// MemoryPrefetchDefaultClaim is the ONE wording of the default that the help
// surfaces print and the help guards assert (Rules 1b/13/14). It is a constant
// for the reason the other help spellings are: two pages (the `gopi -h` Options
// row and the `gopi chat -h` section) plus their tests would otherwise carry
// four hand-typed claims about the same default, and the first edit to one of
// them would leave the others lying about what the binary does.
const MemoryPrefetchDefaultClaim = "default ON"

// MemoryPrefetchDefaultClaimOpening is the same claim in SENTENCE-OPENING form
// ("Default ON"), for a page that starts a clause with it. It exists as its own
// constant rather than being derived at the call site (a case transform is a
// function, not a constant) for the reason the whole help-text block is
// constants: the wording is asserted by tests, and a page that composed it by
// hand would be a second definition.
const MemoryPrefetchDefaultClaimOpening = "Default ON"

// memoryPrefetchNotDeclared is the source label for "no channel mentions the
// knob" — kept distinct from a declared OFF so a caller that reports the switch
// can tell "nobody asked" from "the operator said off" (Rule 15: an ambiguous
// report is a silent failure with better manners).
const memoryPrefetchNotDeclared = "not declared"

// MemoryPrefetchRecordName is the file name of the per-run prefetch record —
// one JSON line per prefetch run (query, hits, elapsed ms, outcome, whether the
// result was adopted into the prompt). It is a file-NAME constant (Rule 1)
// because three places spell it: the executor that appends to it, the catalog
// help that advertises it, and the tests that read it back. It lives in the
// SESSION directory that already exists for every run (Rule 35: no new
// persistent dependency — no daemon, no scheduler, no second database; the file
// is swept with the session it belongs to).
const MemoryPrefetchRecordName = "memory-prefetch.jsonl"

// MemoryPrefetchEnabled reports whether typing-time memory prefetch is ON.
//
// Resolution: `GOPI_MEMORY_PREFETCH` (MemoryPrefetchEnv) > settings.json
// "memory_prefetch" (MemoryPrefetchSettingsKey) > the built-in default (ON, see
// memoryPrefetchDefault). The env channel accepts
// the documented truthy vocabulary (IsTrueValue: "1", "on", "yes", "true",
// "enabled") AND the documented falsey set (IsFalseValue: "0", "off", "false",
// "no"), so an ApplyEnv-written "off" is understood rather than misread as
// enabled. A value that is NEITHER spelling (a typo like "ture") cannot answer
// the question: it is reported on stderr and the settings channel decides —
// never silently read as "off", which beside a default would be
// indistinguishable from the default (Rule 15).
//
// The settings channel is read HERE, directly, and not through ApplyEnv: a
// `gopi chat` invocation returns from main() at the subcommand chain and never
// reaches the agent-path config block, so relying on that publication would
// silently drop the settings channel for the very verb this knob exists for.
func MemoryPrefetchEnabled() bool {
	on, _ := resolveMemoryPrefetch()
	return on
}

// MemoryPrefetchSource names the channel that decided the switch, for a surface
// that must report it (the CLI's own verbose line). It is returned BESIDE the
// value rather than recomputed, so the report and the switch cannot disagree.
func MemoryPrefetchSource() (bool, string) { return resolveMemoryPrefetch() }

// resolveMemoryPrefetch is the ONE resolution (Rule 2).
func resolveMemoryPrefetch() (enabled bool, source string) {
	if raw := strings.TrimSpace(os.Getenv(MemoryPrefetchEnv)); raw != "" {
		if IsTrueValue(raw) {
			return true, MemoryPrefetchEnv
		}
		if IsFalseValue(raw) {
			return false, MemoryPrefetchEnv
		}
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a recognised boolean (%s/off) — ignored; %s decides\n",
			MemoryPrefetchEnv, raw, EnvBoolOn, MemoryPrefetchSettingsKey)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// The settings file is the declaration of record; unreadable means the
		// answer is unknown, and unknown must not be reported as enabled
		// (Rule 15: the safe posture is the default, and it is announced).
		fmt.Fprintf(os.Stderr, "config: %s unreadable (%v) — %s resolves to its built-in %s\n",
			ResolveSettingsPath(), err, MemoryPrefetchSettingsKey, MemoryPrefetchDefaultClaim)
		return memoryPrefetchDefault, memoryPrefetchNotDeclared
	}
	// THE DECLARATION TEST, not the VALUE (the one place a default-ON knob
	// differs from a default-OFF one): nil means the settings file did not
	// mention the key at all — nobody asked — and the built-in default answers.
	// A non-nil pointer means the file DID declare it, and what it declared is
	// the answer in BOTH directions, `true` and `false` alike.
	if cfg != nil && cfg.MemoryPrefetch != nil {
		return *cfg.MemoryPrefetch, MemoryPrefetchSettingsKey
	}
	return memoryPrefetchDefault, memoryPrefetchNotDeclared
}

// ── THE HELP-TEXT HALF (the knob's discoverability) ──────────────────────────
//
// THE STATE THIS CLOSES (measured on the built binary at this task's base
// commit 6c17d819): the knob shipped
// COMPLETE — three channels, a resolver, an executor, records, tests, a CONFIG.md
// §2c row — and neither `gopi -h` nor `gopi chat -h` mentioned it:
//
//	$ gopi -h 2>&1 | grep -i prefetch   ; echo $?
//	1                                   ← the front door said nothing
//	$ gopi chat -h 2>&1 | grep -i prefetch ; echo $?
//	1                                   ← nor did the verb it exists for
//
// That is the SAME class as the reply-language defect
// (help_knob_discoverability_test.go documents it: a knob can be real and
// undiscoverable at once). The previous answer was "nobody can know", and it was
// not a decision — nothing required either page to mention the knob.
//
// WHY THE NAMES BELOW ARE CONSTANTS (Rules 1b/13/14). Two surfaces advertise this
// knob — the TOP-LEVEL Options row (internal/config/help.go's optionRows) and the
// chat page's own section (cmd/agent/chat_prefetch_usage.go) — and the tests
// assert what those two print. Four hand-typed spellings of "before the verb"
// would drift into four claims, so every SPELLING either surface prints is
// defined once, here, next to the knob: the invocation example (derived from the
// flag and the verb constant), the row summary, and the pre-verb clause both
// surfaces carry verbatim.

// MemoryPrefetchInvocation is the copy-pasteable spelling of the knob — the
// PRE-VERB one, and the only position the hoist accepts: the flag is consumed by
// config.BootstrapEnvFromArgs while no non-flag word has been seen yet
// (prefetch_bound_env.go holds the reason). The verb word comes from
// cliverbs.Chat — the constant the dispatcher switches on — so the example cannot
// name a verb gopi does not dispatch.
const MemoryPrefetchInvocation = "--" + MemoryPrefetchFlagName + " " + cliverbs.Chat

// MemoryPrefetchVerbPositionClaim is the POSITION constraint, and it is the one
// sentence BOTH help surfaces print verbatim: the flag is a TOP-LEVEL one, so it
// must precede the verb, and after the verb the verb's own parser REFUSES it by
// name (`gopi chat --memory-prefetch` exits 2 with "unknown flag"). Without that
// half, a page would advertise a command line the verb rejects — the worse of the
// two failures this block exists to remove.
//
// It is kept short enough to sit on ONE line at the chat page's continuation
// indent AND inside a wrapped top-level Options row (config.HelpWrapCol), which
// is what makes one wording serve both surfaces instead of two paraphrases that
// drift. The refusal's details are prose each page adds, not part of the claim.
const MemoryPrefetchVerbPositionClaim = "a TOP-LEVEL flag: write it BEFORE the verb, never after"

// MemoryPrefetchRowSummary is the summary the TOP-LEVEL `gopi -h` Options block
// renders for this knob (internal/config/help.go's optionRows). The front door is
// the page a user reads FIRST, and until this row existed the knob was absent
// from it.
//
// It carries the facts needed to USE the knob — that the fetch is read-only and
// rides into the next turn, the pre-verb position, bare = ON, the precedence, and
// the default — and deliberately not the mechanics (the deadline, the DSN, the
// query shape): this page is an INDEX, capped by cmd/agent's topLevelUsageMaxLines
// so one row cannot grow into a manual. The full row is CONFIG.md §2c plus
// `gopi --help-env`, and the chat page (cmd/agent/chat_prefetch_usage.go) carries
// the longer prose. Measured: adding this row took the rendered page from 169
// lines to 173 of its 176-line budget.
const MemoryPrefetchRowSummary = "typing-time READ-ONLY memory prefetch for `gopi " + cliverbs.Chat +
	"` — the hits ride into the next turn, as in `gopi " + MemoryPrefetchInvocation + "`; " +
	MemoryPrefetchVerbPositionClaim +
	"; bare = ON; `--" + MemoryPrefetchFlagName + "=off` > " + MemoryPrefetchEnv + " > settings `" +
	MemoryPrefetchSettingsKey + "`; " + MemoryPrefetchDefaultClaim
