package config

// help_knob_discoverability_test.go — the guard for the CLASS the
// output-language defect belongs to, plus the exact page assertions of the
// instance that opened it.
//
// THE DEFECT (measured 2026-09-27, task 01a0dee0). The reply-language
// preference shipped COMPLETE — three channels (settings key `output_language`,
// `GOPI_OUTPUT_LANGUAGE`, `--output-language`), a resolver, a prompt directive,
// its own tests — and `gopi -h`, the page a user reads first, mentioned no
// language anywhere: `grep -i language` over the rendered front door returned
// ZERO hits. `gopi --help-env` documented it, so the knob was real; but nobody
// looks for a knob they have never heard of, so "real" and "discoverable" were
// not the same thing. The Epic's question is exactly that gap: *how can a user
// know this without help text?*
//
// WHY THE GUARD IS ABOUT THE CLASS, NOT THE ROW. The neighbouring
// TestTopLevelUsageShowsSettingsKeysAndFlags spot-checks a hand-picked flag
// list (`--timeout`, `--compact`, `--db`), and a spot-check can only ever prove
// the rows somebody remembered to add — which is how a knob ends up with a
// catalog row and no front-door sentence. So this file (a) pins the instance
// byte-for-byte, and (b) walks the WHOLE catalog with ONE discovery rule, so
// the next user-facing knob cannot ship invisible either.
//
// RED PROOF (Rule 10.4 / minimal sample): reverting the `--output-language` row
// in help.go turns TestTopLevelHelpShowsOutputLanguage red with the missing
// spellings named, and TestTopLevelHelpExposesEveryUserFacingKnob red with the
// catalog row that has no page exposure. A guard that passes on the un-fixed
// tree proves nothing.

import (
	"strings"
	"testing"
)

// userFacingKnobHelpWord is the ONE word whose presence in a catalog row's help
// prose makes that row a statement about the USER'S experience of the agent
// (the words it answers in) rather than about the machinery (paths, cadences,
// timeouts, engines).
//
// It is deliberately a SMALL, justified vocabulary rather than "everything the
// catalog lists": the front door is an index (~160 lines, guarded by
// cmd/agent's topLevelUsageMaxLines), and a rule that demanded a page row for
// every knob would fight that budget instead of serving the reader. A word
// joins this list when a user's first question about gopi can be that word —
// "what language does it answer in?" is such a question; "what is the
// compaction target ratio?" is not (the reader who has that question is already
// in `gopi --help-env`).
const userFacingKnobHelpWord = "language"

// userFacingKnobHelpExempt records the catalog rows the discovery rule matches
// and that are DELIBERATELY not on the front door, each with its reason. An
// exemption is a decision, not a silence: the gate prints it, and a row that
// leaves the catalog makes its stale entry an error below.
var userFacingKnobHelpExempt = map[string]string{
	// (empty at the time of writing — the rule's only match is the
	// output-language row this task exposed; a future match must either reach
	// the page or be listed here by name WITH a reason.)
}

// isUserFacingKnob reports whether a catalog row's help prose is a statement
// about how the agent interacts with the user.
func isUserFacingKnob(e envKey) bool {
	return strings.Contains(strings.ToLower(e.Help), userFacingKnobHelpWord)
}

// TestTopLevelHelpShowsOutputLanguage pins the INSTANCE: the reply-language
// preference must be visible on `gopi -h`, naming all three channels a reader
// can use and stating the scope the knob actually has.
//
// The expected spellings are read from the knob's OWN constants (Rules 1b/13/14)
// — never re-typed — so a rename of the flag or the settings key moves the page
// and this assertion together, and the test cannot pin a spelling the parser
// does not accept.
func TestTopLevelHelpShowsOutputLanguage(t *testing.T) {
	usage := FormatUsage()
	flat := strings.Join(strings.Fields(usage), " ")

	for _, want := range []string{
		flagPrefix + OutputLanguageFlagName, // --output-language
		OutputLanguageEnv,                   // GOPI_OUTPUT_LANGUAGE
		OutputLanguageSettingsKey,           // output_language (the settings key)
		OutputLanguageScopeClaim,            // reply only — reasoning unaffected
		"ANSWERS",                           // the capability sentence, not the mechanism
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("`gopi -h` does not show the reply-language knob: missing %q — the knob exists in "+
				"three channels and a user cannot discover it from the front door (the defect task "+
				"01a0dee0 opened this file for)", want)
		}
	}

	// The page's own claim must be TRUE of the parser: the flag it advertises
	// is accepted (a page advertising a flag the parser refuses is the worse
	// half of the same defect class — cmd/agent's help-surface sweep, and
	// TestEnvNameClaimMatchesReality_WholeCatalog for the env-name spelling,
	// cover the rest of the catalog).
	if _, err := FlagOverrides([]string{flagPrefix + OutputLanguageFlagName, "zh-CN", "version"}); err != nil {
		t.Errorf("`gopi -h` advertises %s%s but the parser refuses it: %v",
			flagPrefix, OutputLanguageFlagName, err)
	}
}

// TestTopLevelHelpExposesEveryUserFacingKnob is the anti-omission half: walk the
// WHOLE catalog, apply the ONE discovery rule, and fail for every matching row
// that has no front-door exposure — unless it is exempted BY NAME with a
// reason.
//
// Discovery, not enumeration: a knob added to the catalog tomorrow is covered
// the moment its help says what it does for the user. That is the property the
// output-language defect lacked — it was a complete feature that no rule
// required the page to mention.
func TestTopLevelHelpExposesEveryUserFacingKnob(t *testing.T) {
	flat := strings.Join(strings.Fields(FormatUsage()), " ")

	if len(EnvVars) == 0 {
		t.Fatal("catalog is empty — the rule below would scan nothing")
	}

	var scanned int
	sawLanguageKnob := false
	for _, e := range EnvVars {
		if !isUserFacingKnob(e) {
			continue
		}
		scanned++
		if e.Env == OutputLanguageEnv {
			sawLanguageKnob = true
		}
		if _, exempt := userFacingKnobHelpExempt[e.Env]; exempt {
			continue
		}
		// Every channel the catalog attaches to this knob must be readable off
		// the page: the env var always, the flag when it has one, the
		// settings key when it has one.
		if !strings.Contains(flat, e.Env) {
			t.Errorf("%s is a user-facing knob (its help says so) but `gopi -h` never names it — "+
				"a user has no way to discover it; expose it on the front door or add it to "+
				"userFacingKnobHelpExempt with a reason", e.Env)
		}
		if e.CLIFlag != "" && !strings.Contains(flat, flagPrefix+e.CLIFlag) {
			t.Errorf("%s has the flag %s%s but `gopi -h` never shows the flag a user would type",
				e.Env, flagPrefix, e.CLIFlag)
		}
		if e.Key != "" && e.Key != settingsPathKey && !strings.Contains(flat, e.Key) {
			t.Errorf("settings.json key %q (%s) is not named in `gopi -h` — the reader cannot find "+
				"the key to set", e.Key, e.Env)
		}
	}

	// NON-VACUITY FLOOR, twice over: the rule must still match something, and it
	// must still match the row that prompted the file — otherwise a reworded
	// catalog entry (or a broken predicate) would turn this gate green by
	// scanning nothing.
	if scanned == 0 {
		t.Fatalf("the discovery rule (%q in the catalog help) matched no row — the predicate is "+
			"measuring nothing; fix it rather than deleting the gate", userFacingKnobHelpWord)
	}
	if !sawLanguageKnob {
		t.Errorf("the discovery rule no longer matches %s — the knob this file was written for would "+
			"silently leave the gate's scope", OutputLanguageEnv)
	}

	// An exemption for a row that no longer exists (or no longer matches the
	// rule) is a stale permission: it would let a future row reuse the name
	// silently.
	for env, reason := range userFacingKnobHelpExempt {
		if _, ok := envByName()[env]; !ok {
			t.Errorf("userFacingKnobHelpExempt lists %q, which is not in the catalog — remove the stale "+
				"exemption (reason recorded: %s)", env, reason)
		}
	}
}
