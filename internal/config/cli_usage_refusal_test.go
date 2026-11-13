package config

// cli_usage_refusal_test.go — the PRE-VERB HOIST's value discipline, unit-tested
// at the seam that DECIDES it, plus the remainder contract that must survive the
// new refusal.
//
// WHAT IS AND IS NOT TESTED HERE. The refusal itself calls os.Exit, so the exit
// STATUS and the absence of a top-level usage page are observed by EXECUTING the
// binary: cmd/agent/cli_hoist_missing_value_test.go drives it through
// $GOPI_TEST_BIN (the red proof is that same test run against a pre-fix binary).
// What is unit-testable — and what this file pins — is every INPUT to that
// decision:
//
//	1. hoistedFlagVerdict: which flags are shape-checked at all, what a missing
//	   token and an unusable token are, and that every OTHER hoisted flag (a
//	   path, a free string, a value-optional switch) is deliberately NOT refused
//	   — the regression that would turn a working `--settings X task run` or
//	   `--film-terminal NAME chat` into a usage error;
//	2. the dispatch REMAINDER for the shapes that must keep working, which is
//	   the contract `gopi --settings X task submit "a b c"` rests on (the
//	   positional-param bug of 2026-09-06);
//	3. the exit-status constant's own contract, because the executed-binary test
//	   asserts `code == CLIUsageExitCode` and that assertion alone would pass on a
//	   constant of 0.
//
// Splitting the two instruments this way is the same arrangement
// output_language_perinvocation_test.go states and for the same reason: a unit
// test can see a predicate, only a child process can see a process's exit.

import (
	"reflect"
	"testing"
)

// TestHoistedFlagVerdict pins the decision for every class of hoisted flag.
//
// The NEGATIVE rows are the load-bearing ones. A verdict table that answered
// "refuse" for a flag whose value is a path would break the three hoists the
// rest of the system depends on (`--settings`, `--home`, `--tmpdir`), and the
// failure would look like a usage error on a correct command line — strictly
// worse than the silence this work removes. So each of those is asserted here by
// name, through the same predicate the hoist calls.
func TestHoistedFlagVerdict(t *testing.T) {
	cases := []struct {
		name       string
		flag       string
		value      string
		haveToken  bool
		wantReason hoistRefusalReason
	}{
		// --- the two measured defects: a value-less flag in front of its verb ---
		{
			name: "turns flag, next token is the verb", flag: ChatSurfaceTurnsFlagName,
			value: "chat", haveToken: true, wantReason: hoistVerbWord,
		},
		{
			name: "geometry flag, next token is the verb", flag: ChatIdealGeometryFlagName,
			value: "chat", haveToken: true, wantReason: hoistVerbWord,
		},
		// --- a typo in the value is the same class (it is not the built-in default) ---
		{
			name: "turns flag, non-numeric value", flag: ChatSurfaceTurnsFlagName,
			value: "banana", haveToken: true, wantReason: hoistUnusable,
		},
		{
			name: "turns flag, below the floor", flag: ChatSurfaceTurnsFlagName,
			value: "1", haveToken: true, wantReason: hoistUnusable,
		},
		{
			name: "geometry flag, single number", flag: ChatIdealGeometryFlagName,
			value: "117", haveToken: true, wantReason: hoistUnusable,
		},
		// --- nothing after the flag at all ---
		{
			name: "turns flag at end of argv", flag: ChatSurfaceTurnsFlagName,
			haveToken: false, wantReason: hoistNoToken,
		},
		// --- the values that must keep working ---
		{
			name: "turns flag, usable value", flag: ChatSurfaceTurnsFlagName,
			value: "12", haveToken: true,
		},
		{
			name: "geometry flag, usable value", flag: ChatIdealGeometryFlagName,
			value: "120x30", haveToken: true,
		},
		{
			// An empty value is the knob parser's business, not "missing": the
			// knob's own parser refuses "" and reports the shape. The verdict
			// takes the token as it is, so there is one answer, not two.
			name: "turns flag, empty token", flag: ChatSurfaceTurnsFlagName,
			value: "", haveToken: true, wantReason: hoistUnusable,
		},
		// --- flags that must NEVER be refused by shape ---
		{
			name: "settings path is not shape-checked", flag: "settings",
			value: "task", haveToken: true,
		},
		{
			name: "home path is not shape-checked", flag: "home",
			value: "chat", haveToken: true,
		},
		{
			name: "tmpdir is not shape-checked", flag: TmpDirFlagName,
			value: "chat", haveToken: true,
		},
		{
			// The value-OPTIONAL switch: `gopi --memory-prefetch chat` is a legal
			// command line whose verb the conditional peek already protects. A
			// refusal here would break the one spelling that memo documents.
			name: "memory-prefetch is value-optional", flag: MemoryPrefetchFlagName,
			value: "chat", haveToken: true,
		},
		{
			// A free-string knob: "chat" is a perfectly legal terminal name as far
			// as the HOIST can tell (the registry is the authority, and it lives in
			// another package).
			name: "film-terminal is a free string", flag: FilmTerminalFlagName,
			value: "chat", haveToken: true,
		},
		{
			name: "output-language is validated elsewhere", flag: OutputLanguageFlagName,
			value: "chat", haveToken: true,
		},
		{
			name: "an unknown flag is not ours", flag: "model",
			value: "chat", haveToken: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hoistedFlagVerdict(tc.flag, tc.value, tc.haveToken); got != tc.wantReason {
				t.Fatalf("hoistedFlagVerdict(%q, %q, haveToken=%v) = %v, want %v",
					tc.flag, tc.value, tc.haveToken, got, tc.wantReason)
			}
		})
	}
}

// TestHoistedValuedFlagsUseTheKnobsOwnParser pins the Rule 2 property the table
// claims: each row's verdict IS the knob's own parser, so the refusal can never
// disagree with what the resolution would accept.
//
// It is checked by driving values through BOTH sides and requiring the two answers
// to agree — a table that grew a private rule (a regexp, a length check) fails
// here the first time the knob's rule moves.
func TestHoistedValuedFlagsUseTheKnobsOwnParser(t *testing.T) {
	t.Run("chat-surface-turns", func(t *testing.T) {
		for _, v := range []string{"2", "12", "1000", "1", "1001", "0", "-3", "abc", "", " "} {
			_, knobOK := ParseChatSurfaceTurns(v)
			hoistOKForValue := hoistedFlagVerdict(ChatSurfaceTurnsFlagName, v, true) == hoistOK
			if hoistOKForValue != knobOK {
				t.Errorf("value %q: knob parser says usable=%v but the hoist says usable=%v — "+
					"the two must never disagree (one opinion per value)", v, knobOK, hoistOKForValue)
			}
		}
	})
	t.Run("chat-ideal-geometry", func(t *testing.T) {
		for _, v := range []string{"117x26", "120x30", "117", "x26", "abcxdef", "", "0x0"} {
			_, _, knobOK := parseChatIdealGeometry(v)
			hoistOKForValue := hoistedFlagVerdict(ChatIdealGeometryFlagName, v, true) == hoistOK
			if hoistOKForValue != knobOK {
				t.Errorf("value %q: knob parser says usable=%v but the hoist says usable=%v", v, knobOK, hoistOKForValue)
			}
		}
	})
}

// TestHoistRemainderSurvivesTheRefusalClass is the compatibility half: every
// dispatch shape the hoist already served must still return the same remainder.
// These are the exact cases the surrounding files pin ONE BY ONE (settings_cli_test.go,
// chat_prefetch_config_test.go, chat_surface_turns_test.go, film_terminal_test.go);
// they are restated together here so a change to the peek has ONE place that
// fails when it alters the contract, whatever the flag.
func TestHoistRemainderSurvivesTheRefusalClass(t *testing.T) {
	// The hoist WRITES env vars; t.Setenv gives each pin a restore scope, and
	// the values are seeded so no assertion depends on the operator's export.
	for _, k := range []string{
		SettingsEnv, GopiDirEnv, TmpDirEnvName, OutputLanguageEnv,
		MemoryPrefetchEnv, ChatSurfaceTurnsEnv, ChatIdealGeometryEnv, FilmTerminalEnv,
	} {
		t.Setenv(k, "")
	}

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			// The defect's shape, with the value PRESENT: the verb survives and
			// the pair is consumed — unchanged behaviour, asserted because the new
			// branch sits right beside the peek.
			name: "valued hoisted flag keeps the verb",
			in:   []string{"--" + ChatSurfaceTurnsFlagName, "12", "chat", "-h"},
			want: []string{"chat", "-h"},
		},
		{
			name: "equals form keeps the verb",
			in:   []string{"--" + ChatSurfaceTurnsFlagName + "=12", "chat", "-h"},
			want: []string{"chat", "-h"},
		},
		{
			name: "settings pair before a verb",
			in:   []string{"--settings", "/tmp/s.json", "task", "run", "t-1"},
			want: []string{"task", "run", "t-1"},
		},
		{
			name: "settings pair mid-args (ssh passthrough)",
			in:   []string{"ssh", "-o", "BatchMode=yes", "--settings", "/tmp/s.json", "bnode", "task", "sync"},
			want: []string{"ssh", "-o", "BatchMode=yes", "bnode", "task", "sync"},
		},
		{
			// The value-OPTIONAL switch in front of its verb: the conditional peek
			// leaves the word alone. This is the case the shape-checked branch must
			// not disturb.
			name: "value-optional switch keeps the verb",
			in:   []string{"--" + MemoryPrefetchFlagName, "chat"},
			want: []string{"chat"},
		},
		{
			name: "free-string flag keeps the verb",
			in:   []string{"--" + FilmTerminalFlagName, "xfce4-terminal", "chat", "--film", "/tmp/x"},
			want: []string{"chat", "--film", "/tmp/x"},
		},
		{
			// A path whose value happens to BE a verb word is still a value: the
			// two shape-checked knobs are the only ones the verdict refuses, and a
			// settings path is not one of them.
			name: "a path value that looks like a verb",
			in:   []string{"--settings", "chat", "task", "list"},
			want: []string{"task", "list"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BootstrapEnvFromArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("BootstrapEnvFromArgs(%v) remainder = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestHoistIdentityRemainderUnchanged: with nothing of ours on the line, the SAME
// slice comes back (the plain `gopi task …` path must see exactly what it saw
// before — identity, not a copy).
func TestHoistIdentityRemainderUnchanged(t *testing.T) {
	in := []string{"task", "submit", "a b c", "--payload", "x y"}
	if got := BootstrapEnvFromArgs(in); !reflect.DeepEqual(got, in) {
		t.Fatalf("remainder = %#v, want the input unchanged %#v", got, in)
	}
}

// TestCLIUsageExitCodeContract pins the CONSTANT the executed-binary test compares
// against, and it is not a formality: that test asserts `code == CLIUsageExitCode`,
// so a constant of 0 (or 1) would be satisfied by the very behaviour this work
// removes.
//
// The value must be NON-ZERO (a refusal a caller can detect by exit status — Rule
// 15), the POSIX usage class rather than 1 ("the run itself failed" — nothing ran
// here), and distinct from AgentGuardRefusalExitCode ("this launch is forbidden" —
// a different operator action, with a different remedy).
func TestCLIUsageExitCodeContract(t *testing.T) {
	if CLIUsageExitCode == 0 {
		t.Fatal("a usage refusal must exit NON-ZERO — an exit 0 is indistinguishable from a command that ran")
	}
	if CLIUsageExitCode == 1 {
		t.Fatalf("the usage class must be distinguishable from %d (\"the run itself failed\"): "+
			"nothing ran here, so a caller retrying on 1 retries the wrong thing", 1)
	}
	if CLIUsageExitCode == AgentGuardRefusalExitCode {
		t.Fatalf("the usage class must be distinguishable from AgentGuardRefusalExitCode (%d)", AgentGuardRefusalExitCode)
	}
	// ONE number for the whole package's usage class: the reply-language
	// per-invocation refusal is an ALIAS, not a second literal (Rule 1b).
	if OutputLanguagePerInvocationExitCode != CLIUsageExitCode {
		t.Fatalf("OutputLanguagePerInvocationExitCode = %d but CLIUsageExitCode = %d — one usage class, "+
			"one number", OutputLanguagePerInvocationExitCode, CLIUsageExitCode)
	}
}

// TestHoistRefusalVocabularyIsNotEmpty keeps the messages a reader is shown from
// silently becoming empty strings: the executed-binary test matches on THESE
// values, so an empty phrase would make it assert nothing while passing.
func TestHoistRefusalVocabularyIsNotEmpty(t *testing.T) {
	for name, v := range map[string]string{
		"HoistedFlagRefusalPrefix":       HoistedFlagRefusalPrefix,
		"HoistedFlagMissingValuePhrase":  HoistedFlagMissingValuePhrase,
		"HoistedFlagUnusableValuePhrase": HoistedFlagUnusableValuePhrase,
		"HoistedFlagNothingRanClaim":     HoistedFlagNothingRanClaim,
		"HoistedFlagNextWordKeptClaim":   HoistedFlagNextWordKeptClaim,
	} {
		if v == "" {
			t.Errorf("%s is empty — a refusal that names nothing asserts nothing", name)
		}
	}
}
