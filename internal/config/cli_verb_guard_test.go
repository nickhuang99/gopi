package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// cli_verb_guard_test.go — unit cover for the run path's positional-verb
// refusal (cli_verb_guard.go). The END-TO-END contract (the real binary
// refusing and the escape hatches still running) lives in cmd/agent's
// cli_verb_guard_test.go, which also pins PositionalVerbs against the actual
// dispatch surface; these cases pin the CLASSIFICATION itself, on the same
// argument slice the run path classifies (FlagOverrides output — flag-aware
// positionals).

// verbsRefused pins the ordinary refusals: a dispatched verb word left over
// after the run flags.
func TestPositionalCommandVerbRefusesLeftoverVerb(t *testing.T) {
	for _, args := range [][]string{
		{"--timeout", "3", "version"},
		{"--timeout", "3", "task", "list"},
		{"--timeout", "4", "--base-url", "http://127.0.0.1:9", "--api-key", "dummy", "resume", "no-such-thing"},
		{"config"},                   // bare verb word
		{"--timeout", "600", "log"},  // a verb with no further argument
		{"--compact", "code-search"}, // boolean flag first, then the verb
		// The two words measured in EUAUDIT slice 4 TOOLS-1 (2026-09-26):
		// `gopi --timeout 3 code_search` and `gopi --timeout 3 keyword_search
		// -h` both used to start a real run (the run path accepted the leftover
		// positional as the prompt). They are in the vocabulary now — the
		// second one BECAUSE the fix made it a command (naming rule 1).
		{"--timeout", "3", "code_search", "EUAuditWidget"},
		{"--timeout", "3", "keyword_search"},
		// The dash twins of the underscore commands: a spelling the naming
		// rule says is NOT the command (rule 1 keeps the tool's underscore).
		{"--timeout", "3", "vision-analyze", "shot.png"},
	} {
		cfg, err := FlagOverrides(args)
		if err != nil {
			t.Fatalf("FlagOverrides(%q): %v", args, err)
		}
		t.Setenv(PromptEnvVar, "")
		if got := PositionalCommandVerb(cfg); got == "" {
			t.Errorf("PositionalCommandVerb(%q) = \"\", want the verb word refused", args)
		}
	}
}

// TestPositionalCommandVerbAllowsRunShapes is the other half of the contract:
// every shape a real run takes must STILL run — a refusal here breaks prompt
// text (`"version"` as a word in a sentence is caught by the same rule, which
// is why the refusal must name `--prompt`).
func TestPositionalCommandVerbAllowsRunShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		prompt string
	}{
		{name: "plain prompt", args: []string{"--timeout", "3", "say hi"}},
		{name: "non-verb word", args: []string{"--timeout", "3", "frobnicate"}},
		// A spelled-out prompt containing a tool word is prompt text: the
		// guard classifies POSITIONALS, not prose (Rule: the refusal is a
		// statement about this binary's surface, never a guess about intent).
		{name: "tool word inside prose", args: []string{"--timeout", "3", "explain the code_search tool"}},
		{name: "flag value is not a positional", args: []string{"--model", "version", "say hi"}},
		{name: "no positional at all", args: []string{"--timeout", "3"}},
		{name: "explicit --prompt", args: []string{"--timeout", "3", "--prompt", "version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := FlagOverrides(tc.args)
			if err != nil {
				t.Fatalf("FlagOverrides(%q): %v", tc.args, err)
			}
			t.Setenv(PromptEnvVar, tc.prompt)
			if got := PositionalCommandVerb(cfg); got != "" {
				t.Fatalf("PositionalCommandVerb(%q) = %q, want no refusal (this is a legitimate run)", tc.args, got)
			}
		})
	}
}

// TestPositionalCommandVerbNilConfig — the predicate is total: a caller with
// no parsed config has nothing to classify, never a panic.
func TestPositionalCommandVerbNilConfig(t *testing.T) {
	t.Setenv(PromptEnvVar, "")
	if got := PositionalCommandVerb(nil); got != "" {
		t.Fatalf("PositionalCommandVerb(nil) = %q, want \"\"", got)
	}
}

// TestPositionalCommandVerbRefusalNamesBothSpellings — the message is the
// deliverable: it must name the word, the FIRST-argument form and the
// `--prompt` escape hatch. A refusal that only says "no" leaves the operator
// guessing (Rule 15: never a silent failure — and never a useless one).
func TestPositionalCommandVerbRefusalNamesBothSpellings(t *testing.T) {
	msg := PositionalCommandVerbError("version").Error()
	for _, want := range []string{`"version"`, "gopi version", "--prompt version"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q is missing %q", msg, want)
		}
	}
}

// TestPositionalCommandVerbResolvedIgnoresAmbientPrompt is the regression for
// task 01a0de86 (2026-09-26): the disarmer must read the prompt IN FORCE for
// this invocation, never an ambient GOPI_PROMPT that a lower-precedence
// channel left behind.
//
// The measured incident: a probe spelled `gopi --gopi-prompt 1 version`, which
// is the documented env-name-as-flag equivalence of `--prompt 1`. The prompt
// was legitimately "1" FOR THAT INVOCATION — and it published GOPI_PROMPT=1
// into the process environment, where it outlived the invocation. Every LATER
// `gopi --timeout 3 version` then saw `os.Getenv(PromptEnvVar) != ""`, skipped
// the refusal, and started a REAL run whose prompt was the junk "1" while the
// leftover `version` was swallowed: a session, an `[auto] 1` ledger row, a
// worker slot.
//
// The pair is the acceptance: IDENTICAL argv, the ambient var the only
// difference, and the same refusal both times — while the invocation that
// really does carry `--prompt 1` still runs (the legitimate case, untouched).
func TestPositionalCommandVerbResolvedIgnoresAmbientPrompt(t *testing.T) {
	// A settings file is deliberately ABSENT (its path points into an empty
	// dir), so the settings layer contributes no prompt and the only prompt
	// that could be "in force" is the flag/env one under test.
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SettingsEnv, filepath.Join(home, "no-such-settings.json"))

	verbArgs := []string{"--timeout", "3", "version"}

	t.Run("ambient GOPI_PROMPT unset: refused", func(t *testing.T) {
		t.Setenv(PromptEnvVar, "")
		cfg, err := LoadMerged(verbArgs)
		if err != nil {
			t.Fatalf("LoadMerged(%q): %v", verbArgs, err)
		}
		if got := PositionalCommandVerb(cfg); got == "" {
			t.Fatalf("PositionalCommandVerb(%q) = \"\" with no prompt in force, want the verb refused", verbArgs)
		}
	})

	t.Run("ambient GOPI_PROMPT set: SAME refusal", func(t *testing.T) {
		t.Setenv(PromptEnvVar, "1")
		cfg, err := LoadMerged(verbArgs)
		if err != nil {
			t.Fatalf("LoadMerged(%q): %v", verbArgs, err)
		}
		// The value legitimately resolves into the field (the run path's
		// readInput consumes GOPI_PROMPT the same way), so the refusal is not
		// about the value being absent — it is about the CHANNEL being unable
		// to speak for this invocation.
		if cfg.promptFrom != promptSourceEnv {
			t.Fatalf("fixture: LoadMerged(%q) recorded prompt source %d, want the env channel (%d)", verbArgs, cfg.promptFrom, promptSourceEnv)
		}
		if got := PositionalCommandVerb(cfg); got != "version" {
			t.Fatalf("PositionalCommandVerb(%q) = %q with GOPI_PROMPT=%q left behind by a probe, want \"version\" refused — an ambient var is not the prompt in force",
				verbArgs, got, "1")
		}
	})

	t.Run("--prompt really given: still a run (legitimate case)", func(t *testing.T) {
		t.Setenv(PromptEnvVar, "")
		// The invocation the probe MEANT to audit, spelled as the operator
		// spells it: the positional beside it is an input FILE token.
		args := []string{"--timeout", "3", "--prompt", "1", "version"}
		cfg, err := LoadMerged(args)
		if err != nil {
			t.Fatalf("LoadMerged(%q): %v", args, err)
		}
		if cfg.Prompt != "1" {
			t.Fatalf("LoadMerged(%q).Prompt = %q, want %q (the flag layer must win)", args, cfg.Prompt, "1")
		}
		if got := PositionalCommandVerb(cfg); got != "" {
			t.Fatalf("PositionalCommandVerb(%q) = %q, want no refusal: an explicit --prompt makes the positional an input FILE", args, got)
		}
	})

	t.Run("GOPI_PROMPT as the only channel: the refusal is the SAFE reading", func(t *testing.T) {
		t.Setenv(PromptEnvVar, "1")
		cfg, err := LoadMerged(verbArgs)
		if err != nil {
			t.Fatalf("LoadMerged(%q): %v", verbArgs, err)
		}
		// The fixture asserts the settings layer contributed nothing, so the
		// only candidate prompt is the environment's — the channel whose
		// provenance is unknowable at read time. The guard refuses, and the
		// refusal message names the unambiguous spelling for an operator who
		// really does mean the word as prompt text.
		if cfg.Prompt != "1" {
			t.Fatalf("LoadMerged(%q).Prompt = %q, want the env value to reach the field (the run path still consumes GOPI_PROMPT: cmd/agent/main.go's readInput)",
				verbArgs, cfg.Prompt)
		}
		if got := PositionalCommandVerb(cfg); got != "version" {
			t.Fatalf("PositionalCommandVerb(%q) = %q with only an env prompt, want \"version\" refused", verbArgs, got)
		}
		if !strings.Contains(PositionalCommandVerbRefusal(cfg, "version").Error(), "--prompt version") {
			t.Fatal("the refusal must hand back the unambiguous spelling (`--prompt version`)")
		}
	})
}

// TestCommandHintVocabularyContract is the near-miss TABLE's own contract,
// checked at the layer that DEFINES it (the live-binary half lives in
// cmd/agent/cli_verb_hint_guard_test.go, which also pins every address against
// the real dispatch surface this leaf package cannot see).
//
// It exists because the table grew a row whose evidence was a LEDGER ROW rather
// than a sentence (`chats`, task 01a194e1): the row's `run_cmd` was `gopi
// chats`, no verb of that spelling exists, and the leftover positional became a
// real run's prompt — a session directory plus an `[auto] chats` row. A word
// earns a place here only by that measurement, and the assertions below are the
// ones a careless addition would break.
func TestCommandHintVocabularyContract(t *testing.T) {
	guarded := map[string]bool{}
	for _, v := range PositionalVerbs {
		guarded[v] = true
	}
	seen := map[string]bool{}
	for _, row := range CommandHintRows() {
		if row.Word == "" {
			t.Fatalf("near-miss row %+v has an empty word — a row with no word can never be matched, so the table would silently be shorter than it reads", row)
		}
		if seen[row.Word] {
			t.Errorf("near-miss vocabulary lists %q twice", row.Word)
		}
		seen[row.Word] = true
		// A word in PositionalVerbs is refused EARLIER, by the other guard, with
		// a different message ("put the command first") — a row here would be
		// unreachable, and the operator would get the wrong advice for the same
		// word depending on argv position.
		if guarded[row.Word] {
			t.Errorf("%q is already refused by PositionalVerbs — a second row here is unreachable (the earlier guard answers first, with a different message)", row.Word)
		}
		for _, r := range row.Routes {
			if r.Command == "" || r.Answers == "" {
				t.Errorf("%q carries an empty route %+v — the clause renders `gopi ` followed by nothing", row.Word, r)
			}
		}
	}
}

// TestChatsIsANearMissOfTheChatVerb pins the row that THIS epic added, by the
// three facts that make it correct and by the one that would make it wrong.
//
// WHY A DEDICATED CASE AND NOT ONLY THE TABLE WALK: the generic walk above
// cannot tell `chats` apart from a row someone deleted — a table that lost this
// word still passes it. This case fails the moment the row goes away, which is
// the regression the epic exists to prevent (`gopi chats` spending a whole run
// again).
func TestChatsIsANearMissOfTheChatVerb(t *testing.T) {
	var row *CommandHintRow
	for _, r := range CommandHintRows() {
		if r.Word == commandHintChatsWord {
			cp := r
			row = &cp
		}
	}
	if row == nil {
		t.Fatalf("%q is not in the near-miss vocabulary — `gopi chats` reaches the run path as a prompt again "+
			"(measured 2026-11-01: a session directory and an `[auto] chats` ledger row)", commandHintChatsWord)
	}
	if row.Address != cliverbs.Chat {
		t.Errorf("%q addresses %q, want %q — the word is the PLURAL of the chat verb",
			row.Word, row.Address, cliverbs.Chat)
	}
	// The listing door is what the operator who types the plural actually
	// wants, and it is a TWO-word sub-verb, which is why it is a ROUTE and not
	// the address (this vocabulary's address must be a word the chain
	// DISPATCHES, pinned in cmd/agent).
	var listing bool
	for _, r := range row.Routes {
		if r.Command == cliverbs.Session+" "+commandHintSessionListWord {
			listing = true
		}
	}
	if !listing {
		t.Errorf("%q prints no route to `gopi %s %s` — the refusal would send the reader to the console page and never mention the listing",
			row.Word, cliverbs.Session, commandHintSessionListWord)
	}
	// And the word must NOT be in the other two vocabularies: an alias/spelling
	// row is refused in EVERY argv shape, `-h` included, which would take the
	// legal `gopi chats -h` help query away from the operator.
	if got := PositionalCommandVerb(&Config{Positionals: []string{commandHintChatsWord}}); got != "" {
		t.Errorf("PositionalCommandVerb(%q) = %q — the near-miss is refused earlier, with advice that does not fit it "+
			"(there is no command to put first)", commandHintChatsWord, got)
	}
}

// TestChatsNearMissRefusalText — the message itself, on the argument shape the
// defect was measured on (`gopi chats`, ONE positional).
func TestChatsNearMissRefusalText(t *testing.T) {
	t.Setenv(PromptEnvVar, "")
	cfg, err := FlagOverrides([]string{"chats"})
	if err != nil {
		t.Fatalf("FlagOverrides: %v", err)
	}
	got := PositionalCommandHintRefusal(cfg)
	if got == "" {
		t.Fatalf("gopi chats was NOT refused — the word would be spent as a run's prompt")
	}
	for _, want := range []string{
		`"chats"`,
		"--prompt chats",
		"gopi " + cliverbs.Chat + " -h",
		"gopi " + cliverbs.Session + " " + commandHintSessionListWord,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal does not mention %q — a reader is left without that route:\n%s", want, got)
		}
	}
	// The escape hatch is a REAL one: an explicit prompt in force means the
	// word is literal text and must run.
	if got := PositionalCommandHintRefusal(&Config{Positionals: []string{commandHintChatsWord}, Prompt: "chats", promptFrom: promptSourceFlag}); got != "" {
		t.Errorf("with an explicit prompt in force the word must run, got refusal:\n%s", got)
	}
}
