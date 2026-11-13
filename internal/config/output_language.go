package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// output_language — the REPLY-language knob (one knob, three channels, one
// resolution).
//
// WHY IT EXISTS (operator-requested, briefings/brief-output-language-knob.md):
// the operator types English and wants gopi to ANSWER him in Chinese. The
// capability was never missing; what was missing was a place to say it. So
// this is deliberately a KNOB, not a framework feature: nothing about the
// prompt registry, the section model or the injection points changes, and with
// the knob unset NOTHING is emitted — a run that configures nothing sends the
// byte-identical system prompt it sent before this file existed.
//
// SCOPE (ruled, not negotiable here): the REPLY language only. The model's
// reasoning language is untouched — that would be a different knob with a
// different justification (a token-cost optimisation), and the operator asked
// for the reply half alone.
//
// CHANNELS, highest first — the same three the rest of internal/config uses:
//
//	rank 1  CLI flag `--output-language TAG`   (hoisted into the env BEFORE the
//	                                            subcommand chain, so `gopi chat`
//	                                            and a task run both see it)
//	rank 2  env  GOPI_OUTPUT_LANGUAGE=TAG
//	rank 3  settings.json "output_language": "TAG"
//	rank 4  unset — the knob is silent (no directive, byte-identical prompt)
//
// The flag and the env channel are ONE predicate at runtime (the flag is
// published into the env by BootstrapEnvFromArgs, exactly like `--tmpdir`,
// because the group verbs and the chat path return from main() before the
// agent-path config block). That is not a lost precedence rung: the flag writes
// the variable UNCONDITIONALLY, so a flag beats an operator's export — while an
// unrelated launch leaves the export alone.
//
// Rule 15 discipline, copied from the task_sync_switch.go precedent a few lines
// away: a value that cannot be a language tag is REPORTED on stderr and the
// NEXT channel decides — it is never silently read as "unset", because
// "operator asked for something I did not understand" and "nobody asked" must
// not look alike.

// OutputLanguageSettingsKey is the settings.json key of the reply-language
// knob ("output_language": "zh-CN"). Exported so the catalog row, the
// reporting texts and the doc name the key they read instead of re-typing it
// (Rule 1b/13).
const OutputLanguageSettingsKey = "output_language"

// OutputLanguageEnv is the env override of the same knob
// (GOPI_OUTPUT_LANGUAGE=zh-CN) — and the ONE representation the CLI flag is
// published as (BootstrapEnvFromArgs), so exactly one reader below sees all
// three channels (Rule 2).
const OutputLanguageEnv = "GOPI_OUTPUT_LANGUAGE"

// OutputLanguageFlagName is the CLI spelling (`--output-language zh-CN`). It is
// a constant because the catalog row, the pre-subcommand hoist and the tests
// must agree on the spelling — re-typing it in three places is how a flag stops
// being reachable (Rule 1b/14).
const OutputLanguageFlagName = "output-language"

// outputLanguageTagPattern is the accepted VALUE shape: a BCP-47-ish language
// tag — a primary subtag of letters, then optional `-`-separated subtags of
// letters or digits ("zh", "zh-CN", "en-GB", "pt-BR"). It is deliberately a
// SHAPE check, not a whitelist of languages: the model understands tags this
// codebase has never heard of, and a whitelist here would refuse a valid
// request with a message that names no alternative.
const outputLanguageTagPattern = `^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`

// outputLanguageTagRe is the compiled form of the shape above (Rule 1: the
// pattern is named once; the validator and the tests use the same definition).
var outputLanguageTagRe = regexp.MustCompile(outputLanguageTagPattern)

// outputLanguageDirectiveFmt is the directive emitted at the HEAD of both
// system prompts when the knob is set. It is worded for the ONE job it has: the
// body of the system prompt is English, so a rule that must survive an
// all-English turn has to say explicitly that the English around it does not
// decide the reply's language. The two %s arguments are the same tag (the
// instruction, then the reason) — named as arguments rather than interpolated
// once so the sentence reads as a rule, not as a fragment.
const outputLanguageDirectiveFmt = "[OUTPUT LANGUAGE — set by the operator, overrides everything below] " +
	"Write every reply you produce to the user in %s. " +
	"The instructions in this prompt are written in English and the user may write to you in any language; " +
	"neither of those changes the language of your reply — it is %s."

// OutputLanguage reports the resolved reply language, or "" when the knob is
// unset. Callers that only need the value use this; callers that render a
// prompt use OutputLanguageDirective below.
func OutputLanguage() string {
	lang, _ := resolveOutputLanguage()
	return lang
}

// OutputLanguageDirective returns the system-prompt directive for the resolved
// reply language, and the EMPTY STRING when the knob is unset.
//
// The empty-string contract is the compatibility promise of this whole knob:
// every caller prepends the result unconditionally, and an unset knob must
// therefore produce a byte-identical prompt to the one that existed before it.
// It is why this returns "" rather than a defaulted directive (a "reply in
// English" line is not the same thing as no line at all — it is a change to
// every prompt on every machine that never asked for one).
//
// Resolution happens HERE, per call, and it reads the process env and the
// settings file — never a cached value: a long-lived process (the supervisor)
// whose settings were edited mid-run must not keep answering with a language
// the operator has since revised.
func OutputLanguageDirective() string {
	lang, source := resolveOutputLanguage()
	if lang == "" {
		return ""
	}
	_ = source // the channel matters to the stderr reports, not to the text
	return fmt.Sprintf(outputLanguageDirectiveFmt, lang, lang)
}

// resolveOutputLanguage is the ONE resolution (Rule 2): it answers WHICH
// language and WHICH channel decided — the second half because "unset because
// nobody asked" and "unset because I refused what you wrote" are the same empty
// value and two different facts, and only the reporting below can tell them
// apart.
//
// A value that fails the shape check is reported on stderr and the NEXT channel
// decides (never read as unset): an operator who wrote `output_language:
// "chinese (simplified)"` must see that their declaration was not understood,
// rather than silently get English replies and no explanation.
//
// A REFUSED declaration is NOT the same event as an unset one, and the two
// must not be reported alike: a naive "no value came out of the END of the
// chain" report would print nothing for `GOPI_OUTPUT_LANGUAGE=zz-ZZ
// output_language=zh-CN` — a declaration the operator DID make was discarded,
// and the silence would be... silence. So the refusal is REPORTED where it
// happens (immediately, naming the channel and the value) and the fact that a
// declaration was discarded is CARRIED to the end of the chain, where the final
// channel says which value it did adopt and which declaration it did not.
func resolveOutputLanguage() (lang string, source string) {
	// refused is the declaration a channel refused, spelled as "<key>=<value>"
	// — the FIRST one only (the chain reports one value per channel, and the
	// end-of-chain line names the declaration the operator is most likely to be
	// looking at). It is what makes "refused" observable at the END of the
	// chain rather than only at the moment of refusal.
	refused := ""
	if raw := strings.TrimSpace(os.Getenv(OutputLanguageEnv)); raw != "" {
		if ok := isOutputLanguageTag(raw); ok {
			return raw, OutputLanguageEnv
		}
		refused = OutputLanguageEnv + "=" + strconv.Quote(raw)
		fmt.Fprintf(os.Stderr, "config: %s=%q %s — %s; the next channel decides\n",
			OutputLanguageEnv, raw, outputLanguageRefusalFmt, outputLanguageRefusalConsequence)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// The settings file is the declaration of record: if it cannot be
		// read, say so rather than reporting "nobody asked" (Rule 15).
		fmt.Fprintf(os.Stderr, "config: %s unreadable (%v) — %s resolves to unset\n",
			ResolveSettingsPath(), err, OutputLanguageSettingsKey)
		return "", outputLanguageUnset
	}
	if cfg == nil {
		return "", outputLanguageUnset
	}
	raw := strings.TrimSpace(cfg.OutputLanguage)
	if raw == "" {
		// The LAST channel, and nobody asked: with no declaration discarded
		// anywhere up the chain, this is the honest "not declared" — and with
		// one discarded, the same empty VALUE above is a REFUSAL, which the
		// caller must be able to tell apart (that is the whole reason these are
		// two constants and not one).
		if refused != "" {
			refuseOutputLanguage(refused, outputLanguageNothingAdopted)
			return "", outputLanguageRefused
		}
		return "", outputLanguageUnset
	}
	if !isOutputLanguageTag(raw) {
		fmt.Fprintf(os.Stderr, "config: %s=%q %s — %s\n",
			OutputLanguageSettingsKey, raw, outputLanguageRefusalFmt, outputLanguageNoDirective)
		// The refused declaration here is the SETTINGS one (the last channel),
		// named explicitly: `refused` holds only an EARLIER channel's refusal,
		// and a settings-only refusal would pass it "" — printing nothing at
		// all. That was this line's own first draft, and the binary-level test
		// caught it (the settings-refused case was the one red on the fixed
		// tree): the very silence this task exists to remove, reproduced one
		// branch over.
		refuseOutputLanguage(outputLanguageSettingsKeySpelled(raw), outputLanguageNothingAdopted)
		return "", outputLanguageRefused
	}
	// A usable LAST channel: the operator gets the value he asked for, and
	// still the report naming what was discarded on the way (Rule 15 — a
	// declaration that did not take effect is never silently dropped).
	if refused != "" {
		refuseOutputLanguage(refused, outputLanguageAdopted(OutputLanguageSettingsKey, raw))
	}
	return raw, OutputLanguageSettingsKey
}

// refuseOutputLanguage is the ONE end-of-chain report for a REFUSED
// declaration (Rule 2: one reporting site, so the two consumers below cannot
// disagree about the wording). It names the declaration that was not adopted
// AND what the chain did instead — "I did not understand you" must never be
// expressible as "nobody asked".
//
// It does NOT restate WHY the value was refused: the inline refusal fired at
// the moment the value was read, immediately above this line, and repeating the
// reason here would double the text on stderr without adding a fact. What only
// this line can say is the CHAIN's verdict — nothing adopted vs. a later
// channel's value adopted — which is what makes a refusal distinguishable from
// an unset knob at the end of the run.
//
// It writes nothing when nothing was refused: an operator who configured
// nothing must still get a silent run (the compatibility contract of this
// whole knob — the unset case emits NOTHING, stderr included).
func refuseOutputLanguage(refused, outcome string) {
	if refused == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "config: %s %s; %s\n", refused, OutputLanguageNotAdoptedClaim, outcome)
}

// outputLanguageSettingsKeySpelled renders the LAST channel's own declaration
// as the "<key>=<value>" spelling refuseOutputLanguage reports, so an unusable
// settings value is named as a refused declaration — not only as the inline
// line printed where it was read.
func outputLanguageSettingsKeySpelled(raw string) string {
	return OutputLanguageSettingsKey + "=" + strconv.Quote(raw)
}

// outputLanguageAdopted renders "the chain adopted this value at this channel"
// — the other half of a refusal report.
func outputLanguageAdopted(key, raw string) string {
	return fmt.Sprintf(outputLanguageAdoptedFmt, key+"="+strconv.Quote(raw))
}

// OutputLanguageReport is the ONE validation/reporting ENTRY POINT of this
// knob, for every surface that exposes it but never assembles a prompt.
//
// WHY IT MUST BE CALLED, AND WHY IT IS A FUNCTION AND NOT A COMMENT: the
// resolution above guards a value the operator wrote, and until this existed it
// ran ONLY on the agent prompt path (internal/agent's
// systemPromptWithOutputLanguage). Every OTHER surface that exposes the knob —
// the top-level `gopi -h`, `gopi --help-env`, `gopi config show`, `gopi chat`,
// a task run — returns from main() BEFORE that path, so a declaration the
// operator wrote was read by NOTHING: measured on the built binary
// (2026-09-27), both the env and the flag case exited 0 with ZERO bytes on
// stderr. The validation was dead code on the real path, and the knob
// advertised on the help page disagreed with the behaviour of the same process
// that printed it.
//
// WHAT IT DOES — and what it deliberately does NOT:
//
//   - a USABLE declaration produces NO output. The knob's contract is that an
//     unset knob emits nothing at all (a caller prepends the empty directive
//     and the prompt stays byte-identical), and a surface that printed a line
//     for every correctly configured run would make the knob noisy exactly
//     where it works. The value itself is the proof it was understood, and
//     `gopi config show` reports it by name.
//   - a REFUSED declaration produces the same reports the prompt path
//     produces (the inline refusal where the value was read, and the
//     end-of-chain line naming what was not adopted) — because they come from
//     the SAME resolution (Rule 2). There is no second validator here: this
//     function calls it.
//
// It takes no arguments and returns nothing, because the resolution reads the
// process env and the settings file and reports to stderr — the whole of what
// a non-prompt surface needs. main() calls it before the subcommand chain (see
// the note there): the surfaces this closes are precisely the ones that return
// early, so a call placed anywhere later would be bypassed by all of them.
func OutputLanguageReport() {
	_, _ = resolveOutputLanguage()
}

// ── THE TWO PER-INVOCATION CHANNELS ARE FAIL-CLOSED ──────────────────────────
//
// THE PINNED DECISION (and it is a decision, not a default): the knob's three
// channels do NOT share one error policy.
//
//	rank 1  `--output-language TAG`   EXPLICIT, per invocation      → FATAL
//	rank 2  GOPI_OUTPUT_LANGUAGE=TAG  EXPLICIT, for this run's env  → FATAL
//	rank 3  settings.json value       a per-MACHINE preference      → warn + continue
//
// WHY THE SPLIT. A per-invocation declaration is a statement about THIS run:
// the operator typed it, or exported it for this shell, and every outcome of
// the run is then attributed to a knob that was never in effect. Measured on
// master 6936c787 (2026-09-27):
//
//	$ gopi --output-language "not a lang!!" version ; echo $?
//	config: GOPI_OUTPUT_LANGUAGE="not a lang!!" is not a language tag …   ← stderr
//	v0.1.0 …                                                             ← the run PROCEEDED
//	0                                                                    ← indistinguishable
//
// The last line is the whole defect: a typo that did nothing and a correct
// declaration produced the same exit status, so no script, no launcher and no
// operator could tell them apart. A failure that is not expressed in the
// channel a caller reads is the silence Rule 15 forbids — the report was
// THERE, and still the run said "fine".
//
// The SETTINGS value keeps warn-and-continue, deliberately. It is read by
// EVERY gopi process on the host — `gopi -h`, a background worker, a merger
// tick — so making it fatal would turn one stale line in one file into "every
// command on this machine fails", with the remedy (edit that file) unavailable
// from the failing invocation and the failure attributed to whatever the
// operator happened to run. The asymmetry is the reason for the asymmetry: the
// person who gets a per-invocation refusal is the person who typed it, and the
// fix is on the same command line he is already editing. Warn-and-continue is
// the policy this knob shipped with for the settings channel, and it stays.
//
// ONE PREDICATE, ONE MESSAGE SHAPE (Rule 2/14). Both channels below go through
// isOutputLanguageTag — the same predicate the resolution uses, so "what counts
// as a value" still has exactly one answer — and both render the same sentence
// with the channel substituted. There is no second validator: the refusal path
// and the warning path cannot form two opinions of what a language tag is.

// OutputLanguagePerInvocationExitCode is the exit status of a REFUSED
// per-invocation declaration (the CLI flag, or the env var exported for this
// run). It is the POSIX USAGE class (2): the command line is wrong, and the
// operator's next action is to re-issue it — the same class, and the same
// value, cmd/agent uses for a refused worker command line
// (workerUnexpectedArgExitCode). Deliberately NOT 1 (the run itself failed —
// nothing ran here) and not AgentGuardRefusalExitCode (3: a launch that is
// FORBIDDEN, a different operator action). Rule 1: the code lives here, at one
// definition site, so the CLI, the tests and CONFIG.md name the same number.
const OutputLanguagePerInvocationExitCode = 2

// OutputLanguageFatalLabel is the one phrase both fatals open with. It says
// what the exit status cannot say to a reader of a log: the process did NOT
// proceed — no version, no help page, no session, no prompt.
const OutputLanguageFatalLabel = "refusing to run"

// outputLanguageFatalPrefix is the rendered head of both fatal messages.
const outputLanguageFatalPrefix = "gopi: " + OutputLanguageFatalLabel + ": "

// outputLanguageMissingValueClaim is the refusal sentence for a flag that was
// given with NO value (`--output-language` at the end of argv, or
// `--output-language=`). It still carries the "is not a language tag" class
// wording, so one phrase identifies the refusal on either channel.
const outputLanguageMissingValueClaim = "received no value (is not a " +
	OutputLanguageValueShape + " — want e.g. " + OutputLanguageExampleTags + ")"

// OutputLanguageMissingValuePhrase is the exported alias of the sentence above
// (the shape §"Export for the tests" of this file's other aliases): the binary
// test asserts that a valueless flag is refused as a MISSING VALUE rather than
// read as "no flag at all", and a test matching on its own copy of the sentence
// keeps passing after a rewording (Rule 14 — one definition, every reader
// imports it).
const OutputLanguageMissingValuePhrase = outputLanguageMissingValueClaim

// outputLanguageLegalShapeLine is the line that teaches the VALUE VOCABULARY at
// the moment of refusal, built from the same atoms the `gopi -h` row and the
// `--help-env` catalog row are built from (OutputLanguageValueQualifier /
// ValueShape / ExampleTags / NotAValueClaim) — so the error a user gets and the
// page they read cannot advertise different value sets (Rule 13/14). It names
// the NEGATIVE half too: `chinese` is what a user actually types first, and a
// refusal that only listed legal tags would leave him guessing why his own
// attempt was not one of them.
const outputLanguageLegalShapeLine = "legal shape: a " + OutputLanguageValueQualifier + " " +
	OutputLanguageValueShape + " — e.g. " + OutputLanguageExampleTags + " (" + OutputLanguageNotAValueClaim + ")"

// outputLanguageFatalHintFlag is the flag channel's remedy line — the fact
// that makes a fatal right here: the declaration was made on THIS command line.
const outputLanguageFatalHintFlag = "`--" + OutputLanguageFlagName +
	"` was given on THIS command line, so the value must be usable — a typo here is a mistake to fix, not a declaration to skip"

// outputLanguageFatalHintEnv is the env channel's remedy line, the same fact
// spelled for the variable the caller exported for this run.
const outputLanguageFatalHintEnv = OutputLanguageEnv +
	" was exported for THIS run, so the value must be usable — a typo here is a mistake to fix, not a declaration to skip"

// outputLanguageWarnAndContinueClaim is the ONE wording of WHY the settings
// channel keeps the softer policy. It is the decision's justification, printed
// where an operator meets it, so the split reads as a rule rather than as an
// inconsistency between two refusals of the same value class.
const outputLanguageWarnAndContinueClaim = "a per-machine preference must not break every command run on that machine"

// outputLanguageFatalPolicyLine states the pinned split inside the refusal:
// the operator who just got a fatal learns, in the same breath, which channel
// instead gets a warning — otherwise the next thing he tries is setting the
// value in settings.json and wondering why that one does not stop him.
const outputLanguageFatalPolicyLine = "a per-invocation declaration is fatal; a settings.json `" +
	OutputLanguageSettingsKey + "` value is only reported and the run continues (" +
	outputLanguageWarnAndContinueClaim + ")"

// outputLanguageCanonicalExampleTag is the worked example the usage lines show
// — ONE tag, named here, so the flag hint and the env hint cannot advertise two
// different spellings (Rule 14). It is a member of the vocabulary constant
// above, never a new token.
const outputLanguageCanonicalExampleTag = "zh-CN"

// outputLanguageFatalUsageFlag / …Env are the copy-pasteable ways out, one per
// channel: supply a legal tag, or drop the declaration entirely (the knob is
// OPTIONAL — "send no directive" is a legal, silent choice, and a refusal that
// did not say so would read as "you must set this").
const (
	outputLanguageFatalUsageFlag = "usage: `gopi --" + OutputLanguageFlagName + " " +
		outputLanguageCanonicalExampleTag + " <verb>` — or drop the flag to send no directive at all"
	outputLanguageFatalUsageEnv = "usage: `export " + OutputLanguageEnv + "=" +
		outputLanguageCanonicalExampleTag + "` — or unset it to send no directive at all"
)

// OutputLanguageFlagWasGiven reports whether argv carries an EXPLICIT
// `--output-language` token (either spelling: `--output-language X` or
// `--output-language=X`), and returns the value it carried ("" when the token
// had no operand at all).
//
// It lives here, beside the resolution, because the flag's presence is a fact
// about the knob — not about argv in general — and because the refusal below
// must decide it with the same token grammar the hoist uses
// (BootstrapEnvFromArgs), never with a second, looser scanner that could
// disagree about what "the operator gave the flag" means.
func OutputLanguageFlagWasGiven(args []string) (value string, given bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		body := strings.TrimPrefix(a, "--")
		name := body
		if k := strings.IndexByte(body, '='); k >= 0 {
			// `--name=value`: the value travels in the same token. Only the
			// knob's OWN name is ours — every other `--x=y` is somebody
			// else's flag and is left for its owner (the hoist's rule).
			name = body[:k]
			if name == OutputLanguageFlagName {
				return body[k+1:], true
			}
			continue
		}
		if name != OutputLanguageFlagName {
			continue
		}
		if i+1 < len(args) {
			return args[i+1], true
		}
		// A dangling flag: present, with NO value — which is exactly the
		// "missing value" refusal below, not a case to skip.
		return "", true
	}
	return "", false
}

// OutputLanguageRefuseUnusablePerInvocation is the FAIL-CLOSED gate for the two
// per-invocation channels, and it is called by cmd/agent/main.go BEFORE the
// subcommand chain — the same position OutputLanguageReport() occupies, and for
// the same reason: the surfaces it must stop (`gopi -h`, `gopi --help-env`,
// `gopi config show`, `gopi chat`, `gopi version`, a task run) are exactly the
// ones that return from main() before the agent prompt path.
//
// It REPORTS and EXITS (non-zero) — it does not return an error — because the
// policy is a process decision, the same shape the sibling refusal already
// uses (config.RefuseAgentSpawn, internal/config/agentguard.go). Keeping the
// two policies in one place is what makes them comparable: a reader of this
// file can see the fatal and the warning side by side.
//
// argv is read ONLY for the flag's presence — an EXPLICIT declaration is what
// makes the refusal legitimate. A value that merely reached the environment
// from settings.json (ApplyEnv's fill) is not the operator's command line, and
// it keeps the warn-and-continue path; that separation is why the flag is
// scanned on argv rather than inferred from the env.
//
// A USABLE declaration writes the knob's env name — the hoist
// BootstrapEnvFromArgs already performed, re-asserted here so this function is
// correct wherever it is called from, and so the ONE representation the
// resolution reads is set by exactly one predicate (Rule 2). An UNSET channel
// does nothing at all: the knob's compatibility contract (unset emits nothing)
// is untouched by this gate.
func OutputLanguageRefuseUnusablePerInvocation(args []string) {
	if value, given := OutputLanguageFlagWasGiven(args); given {
		if trimmed := strings.TrimSpace(value); trimmed == "" || !isOutputLanguageTag(trimmed) {
			claim := outputLanguageRefusalFmt
			if strings.TrimSpace(value) == "" {
				claim = outputLanguageMissingValueClaim
			}
			outputLanguageFatal("--"+OutputLanguageFlagName+" "+strconv.Quote(value),
				claim, outputLanguageFatalHintFlag, outputLanguageFatalUsageFlag)
		}
		// The flag outranks the env channel by being WRITTEN into it — the
		// same unconditional publish BootstrapEnvFromArgs performs.
		os.Setenv(OutputLanguageEnv, value)
	}
	// The env channel is checked AFTER the publish, so a usable flag has
	// already replaced an ambient export: the refusal below can only fire for
	// a value that is genuinely the operator's (exported, and not overridden
	// by a flag). An empty or whitespace-only variable is NOT a declaration —
	// the doc's "absent or empty means no directive" contract, and the reason
	// `GOPI_OUTPUT_LANGUAGE= gopi -h` stays silent and exits 0.
	raw := strings.TrimSpace(os.Getenv(OutputLanguageEnv))
	if raw == "" || isOutputLanguageTag(raw) {
		return
	}
	outputLanguageFatal(OutputLanguageEnv+"="+strconv.Quote(raw),
		outputLanguageRefusalFmt, outputLanguageFatalHintEnv, outputLanguageFatalUsageEnv)
}

// outputLanguageFatal prints the refusal and exits with the per-invocation
// exit code: the ONE fatal site for both channels, so the two cannot drift
// into two message shapes (Rule 2/14). declared is the declaration as the
// operator wrote it, claim is the sentence that says what is wrong with the
// value (the shape refusal, or "received no value"), and hint/usage are the
// channel's remedy pair.
func outputLanguageFatal(declared, claim, hint, usage string) {
	fmt.Fprintf(os.Stderr, "%s%s %s\n  %s\n  %s\n  %s\n  %s\n",
		outputLanguageFatalPrefix, declared, claim, hint, outputLanguageLegalShapeLine,
		outputLanguageFatalPolicyLine, usage)
	os.Exit(OutputLanguagePerInvocationExitCode)
}

// OutputLanguageScopeClaim is the ONE sentence every help surface uses to state
// the knob's SCOPE (the reply only — the model's reasoning is untouched; that
// ruling is the header of this file). It is a constant so the two surfaces a
// user reads — the top-level `gopi -h` Options row and the `gopi --help-env`
// catalog row — and the gate asserting the page still carries it quote the SAME
// words (Rules 1b/13/14): a scope that is only implied is how "write your
// replies in zh-CN" gets read as "translate everything", including the
// reasoning the operator never asked to change.
const OutputLanguageScopeClaim = "reply language only — the model's reasoning is unaffected"

// The VALUE VOCABULARY — what a user is allowed to write after the flag, the
// env var or the settings key. It is a group of constants, built from each
// other, because the surfaces below used to disagree by omission: the refusal
// message taught the vocabulary ("want e.g. zh, zh-CN, en-GB") while the help
// page showed only the placeholder `TAG` and taught nothing, so a user reading
// the page could not tell whether the value is a language NAME, an enum, or a
// tag. Measured 2026-09-27 (task 01a0e049): `gopi -h` rendered
// `--output-language TAG` with no definition of TAG and no example anywhere on
// the page.
//
// WHY THE PAGE, NOT JUST THE ERROR. Teaching the shape only at the moment of
// failure means the user learns the vocabulary by being refused first — and the
// operator's literal question ("is `chinese` the right value?") is answerable
// only from the page: a value that is refused is reported, but a question asked
// BEFORE the first attempt gets no report at all.
//
// OutputLanguageValueShape is the generic name of the value class; the refusal
// message and the page both spell the value as "<qualifier> <shape>", and the
// negative claim below is the half no error message can deliver in advance.
const OutputLanguageValueShape = "language tag"

// OutputLanguageValueQualifier names the standard the shape comes from, so a
// reader knows the accepted values are the ones their locale/BCP-47 knowledge
// already supplies — and NOT a private gopi vocabulary they would have to look
// up. Kept separate from the shape so both the refusal message ("is not a
// language tag") and the page ("a BCP-47-ish language tag") are built from one
// word.
const OutputLanguageValueQualifier = "BCP-47-ish"

// OutputLanguageExampleTags is the worked-examples list: concrete legal values,
// not a description of them. It is what makes the vocabulary usable without
// consulting any specification — and it is ONE constant so the page, the
// catalog row and the refusal messages cannot teach three different sets.
const OutputLanguageExampleTags = "zh, zh-CN, en-GB"

// OutputLanguageNotAValueClaim is the NEGATIVE half of the vocabulary: the
// spellings that look right and are not. It exists because the knob's shape is
// a regex, so `chinese` (a language NAME, which is how most people would try to
// answer "which language?") and `zh_CN` (a LOCALE, the Java/POSIX spelling) are
// both refused — and a page that lists only legal values leaves a user to
// discover that by being refused first.
const OutputLanguageNotAValueClaim = "NOT a language name (`chinese`) or a locale (`zh_CN`)"

// outputLanguageRefusalFmt is the ONE refusal sentence both channels report
// when a value fails the shape check. It is built from the vocabulary constants
// above, so the error a user gets and the page they read teach the same class
// with the same examples (Rule 13) — the two used to be written independently,
// which is how the page ended up teaching nothing at all.
const outputLanguageRefusalFmt = "is not a " + OutputLanguageValueShape +
	" (want e.g. " + OutputLanguageExampleTags + ")"

// OutputLanguageRowSummary is the one-line summary the TOP-LEVEL `gopi -h`
// Options block renders for this knob.
//
// WHY THIS CONSTANT EXISTS (the defect it closes): the knob shipped with a
// catalog row, so `gopi --help-env` documented it — while `gopi -h`, the page a
// user actually reads first, said nothing about language anywhere (measured
// 2026-09-27: `grep -i language` over the rendered page returned zero hits).
// The preference was therefore real and undiscoverable at the same time: a
// user could not learn it existed without already knowing it existed. The
// summary is exported (Rule 14) because the row and the gate on the row must
// name the same text; the spellings inside it come from the constants above,
// never re-typed (Rule 1b), so the row cannot advertise a flag the parser does
// not accept or a settings key the resolver does not read.
//
// The row carries the VALUE VOCABULARY, not just the knob's existence: `TAG` as
// a placeholder names the syntax and withholds the alphabet, so a reader could
// not answer "is `chinese` a legal value?" from the page — the operator's own
// question (task 01a0e049). The vocabulary half is what makes the row
// actionable, and it is spelled with the SAME constants the refusal message
// uses, so the page and the error cannot teach different value sets.
const OutputLanguageRowSummary = "language gopi ANSWERS in — a " + OutputLanguageValueQualifier +
	" " + OutputLanguageValueShape + " (" + OutputLanguageExampleTags + "; " +
	OutputLanguageNotAValueClaim + "), via " + OutputLanguageEnv + " or " +
	OutputLanguageSettingsKey + "; " + OutputLanguageScopeClaim

// The two EMPTY states of the resolution, and why they are two constants.
//
// `lang == ""` says only that no directive is emitted; it does not say WHY, and
// the why is two different facts with two different remedies:
//
//	outputLanguageUnset    NOBODY ASKED — no channel declared a value at all.
//	                       Correct behaviour, nothing to fix.
//	outputLanguageRefused   A DECLARATION WAS DISCARDED — the operator wrote a
//	                       value and this process did not understand it. The
//	                       run continues (the next channel decides, or nothing
//	                       is emitted), and the operator must be TOLD, because
//	                       from his side the knob was configured.
//
// Collapsing them into one "not declared" is the defect this pair exists to
// prevent: it makes "I refused what you wrote" report itself as "nobody asked",
// which is exactly the silence Rule 15 forbids — and it contradicts the
// contract this file's own resolution documents. Named constants (Rule 1)
// because the tests pin both and a bare literal would make the two
// indistinguishable in a failure message — which is the whole point.
const (
	outputLanguageUnset   = "not declared"
	outputLanguageRefused = "refused"
)

// OutputLanguageRefusalPhrase is the ONE wording of the shape refusal — used by
// both channels' inline lines below, and by the tests in BOTH packages
// (internal/config and cmd/agent's binary-driving cases).
//
// Exported for the reason OutputLanguageScopeClaim is (Rules 1b/13/14): a
// second copy of this sentence in a test is a silent pass waiting to happen —
// reword the refusal and a test matching on its own copy keeps passing while
// asserting nothing about the binary. One definition, every reader imports it.
// It ALIASES the sibling task's composed definition (outputLanguageRefusalFmt)
// rather than restating the sentence: two spellings of the refusal prose in one
// file is exactly the drift Rule 14 exists to stop.
const OutputLanguageRefusalPhrase = outputLanguageRefusalFmt

// OutputLanguageNotAdoptedClaim is the ONE wording of the chain's end verdict,
// and the signature an operator can grep for to tell a REFUSED declaration from
// an unset knob. Exported for the same reason as the phrase above: the surfaces
// that report it and the tests that assert it must share one spelling.
const OutputLanguageNotAdoptedClaim = "was not adopted"

// The two halves of the end-of-chain outcome sentence (refuseOutputLanguage).
// Each is named because they are the two END STATES a refused declaration can
// reach, and the refusal report must name the one that actually happened:
//
//	outputLanguageNothingAdopted  the whole chain was unusable → no directive.
//	outputLanguageAdoptedFmt      a later channel's value WAS adopted (%s).
const (
	outputLanguageNothingAdopted = "no channel supplied a usable value, so no directive is emitted"
	outputLanguageAdoptedFmt     = "adopting %s instead"
)

// outputLanguageRefusalConsequence is the tail of the INLINE refusal printed at
// the moment an env value fails the shape check. It names what happens
// immediately ("this channel did not supply the value") without claiming the
// knob is off — the next channel may still supply one, and the end-of-chain
// report (refuseOutputLanguage) is what states the final outcome. "ignored"
// alone would leave the operator unable to tell "the run has no language" from
// "a later channel decided", which is the ambiguity this wording removes.
const outputLanguageRefusalConsequence = "this channel supplies no value"

// outputLanguageNoDirective is the settings channel's inline refusal tail: the
// settings file is the LAST channel, so a value refused there is the end of the
// chain — the directive is not emitted (unlike the env case, where a later
// channel may still supply one). Separate from
// outputLanguageRefusalConsequence precisely because the consequences differ.
const outputLanguageNoDirective = "no directive is emitted"

// isOutputLanguageTag reports whether raw has the accepted VALUE shape. It is
// the single predicate the resolution uses (Rule 2) and is exercised directly
// by the tests, so "what counts as a value" has exactly one answer.
func isOutputLanguageTag(raw string) bool {
	return outputLanguageTagRe.MatchString(raw)
}
