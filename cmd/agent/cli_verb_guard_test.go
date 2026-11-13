package main

// cli_verb_guard_test.go — the runaway-run guard (task 01a0dd02-d0a2): a
// DISPATCHED top-level verb left over as a positional must REFUSE the run
// instead of being sent to the model as a prompt.
//
// The bug: the dispatch chain reads the verb from the FIRST argument only
// (`gopi version`), while the run path accepted any leftover positional as the
// prompt — so `gopi --timeout 3 version` started a real agent run: a session
// directory was minted and a live `[auto] version` ledger row was registered
// for a word the operator meant as a COMMAND.
//
// Two halves, both here because they must agree:
//
//  1. the VOCABULARY contract — config.PositionalVerbs (the words the
//     internal/config guard refuses, which cannot import this package) is
//     EXACTLY the vocabulary this package dispatches (cliVerbs +
//     topLevelCommandNames). A verb added to the dispatch surface without a
//     word in that set would re-open the hole silently.
//  2. the ARGV contract — driven through the REAL binary (Rule 31): the
//     refused shapes exit 1 with a pointed message and mint NOTHING, and every
//     legitimate run shape (bare prompt, non-verb word, --prompt <verb>) still
//     reaches the run path byte-identically (a session is minted — the run
//     path's own signature, and the only cheap proof that a refusal did NOT
//     happen).

import (
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// cliVerbGuardRunProbeTimeout bounds the probes that are EXPECTED to reach the
// run path. They point at an unreachable provider (a refused local port), so
// the run fails in seconds; the timeout only exists so a hang cannot wedge the
// suite (the agent-loop detector of cli_contracts_test.go, applied to the
// shapes that must NOT be refused).
const cliVerbGuardRunProbeTimeout = 60 * time.Second

// dispatchedVerbWords is the dispatch surface this gate protects, enumerated
// from the constants exactly as the contract suite enumerates it: every word
// `gopi <TAB>` offers (topLevelCommandNames), plus the dispatched verbs that
// set deliberately omits (completion/raw/stats — see cli_completion_test.go)
// and the two DEPRECATION aliases of `gopi web search|fetch` (dispatched —
// dispatcherOfToken lists both — and deliberately absent from completion, which
// offers the canonical spelling only). The tool-wire verbs are asserted to BE
// in topLevelCommandNames, so a tool rename cannot quietly drop a word from the
// guard's vocabulary. It is a helper rather than inline code because three
// vocabularies (the guard's list, the spelling table, the synonym table) are
// compared against the same set.
func dispatchedVerbWords(t *testing.T) map[string]bool {
	t.Helper()
	dispatched := map[string]bool{}
	for _, v := range topLevelCommandNames {
		dispatched[v] = true
	}
	// The two RETIRED-WITH-A-POINTER spellings the chain still reaches (their
	// predicates print a redirect and exit 1): `web_search`/`web_fetch` are the
	// previous epic's refused spellings, and they belong in this vocabulary
	// because the guard's question is "does the dispatch chain READ this word",
	// not "does it run work" — a word the chain reads is a word that never
	// becomes a prompt.
	for _, v := range []string{toolnames.ToolWebSearch, toolnames.ToolWebFetch} {
		dispatched[v] = true
	}
	// And the TOOL-WIRE words must be ABSENT from it (epic verb-retirement,
	// 2026-10-05): screen_capture / text2img / vision_analyze / voice_input /
	// keyword_search used to be top-level verbs as well, and are refused by
	// name now — they are reached only as `gopi tool <word>`. A regression
	// that re-added one to the chain would make the command unreachable behind
	// its own refusal, which the assertions below catch; this is the positive
	// statement of the same fact.
	for _, v := range []string{toolnames.ToolScreenCapture, toolnames.ToolText2img, toolnames.ToolVisionAnalyze, toolnames.ToolVoiceInput, toolnames.ToolKeywordSearch} {
		if dispatched[v] {
			t.Fatalf("tool-wire verb %q is BACK in the dispatch set — the local tools have ONE door (`gopi tool <word>`); a second one would be shadowed by its own refusal", v)
		}
	}
	return dispatched
}

// TestPositionalVerbGuardVocabularyMatchesDispatch pins the guard's word set
// to the dispatch surface it protects, in BOTH directions:
//
//	(a) every word the guard refuses IS dispatched by this binary — a refusal
//	    for a word that is not a command would break ordinary prompt text.
//	(b) every dispatched top-level verb IS refused — a verb missing from the
//	    set is the bug this test exists for.
//
// The dispatch surface is enumerated from the constants (cliVerbs membership
// is asserted by TestCLIUserVerbTokensAreAllFromCliVerbs against the main.go
// dispatch chain) plus topLevelCommandNames (the completion-advertised set,
// which additionally carries the tool-wire verbs: screen_capture, text2img,
// vision_analyze, voice_input).
func TestPositionalVerbGuardVocabularyMatchesDispatch(t *testing.T) {
	dispatched := dispatchedVerbWords(t)

	// The words refused BY NAME at argv[0] — two classes that must agree with
	// this list in both directions or the hole stays open in whichever argv
	// position the operator happened to type:
	//
	//	(b) the KNOWN SPURIOUS SPELLINGS (cli_undispatched_tool_words.go) — a
	//	    real command under a spelling this CLI deliberately does not
	//	    dispatch (`code_search` for the dash-cased `code-search`);
	//	(c) the KNOWN SYNONYMS (cli_verb_aliases.go) — a word that MEANS a
	//	    command without being a respelling of it (`buildinfo` → `version`,
	//	    task 01a0f451…: three of those each bought a whole agent run).
	//
	// Both are mapped to their ADDRESS — the command line the refusal sends the
	// reader to, which for most rows is `gopi <canonical>` and for the retired
	// web flat twins is the LIVE home (`gopi tool web search`), because their
	// canonical word is itself refused and a hint pointing at another refusal is
	// a dead end (Rule 15). The checks below ("not dispatched itself", "its
	// address really runs") run once per class rather than once per table.
	spurious := map[string]string{}
	for _, e := range undispatchedToolWords {
		if prev, dup := spurious[e.word]; dup {
			t.Fatalf("cli_undispatched_tool_words lists %q twice (%q and %q)", e.word, prev, e.canonical)
		}
		spurious[e.word] = e.refusalAddress()
	}
	for _, a := range verbAliases {
		if prev, dup := spurious[a.alias]; dup {
			t.Fatalf("cli_verb_aliases lists %q, already refused as a spelling pointing at %q", a.alias, prev)
		}
		spurious[a.alias] = a.refusalAddress()
	}
	for word, address := range spurious {
		if word == "" {
			t.Fatalf("a refusal row carries an empty WORD — the gate would be vacuous")
		}
		if dispatched[word] {
			t.Errorf("%q is BOTH dispatched and refused by name — the command would be unreachable", word)
		}
		if address == "" {
			// A HOMELESS row: the capability is gone (`gopi ui` and `gopi cli`
			// were two of three views of one engine), so there is no address to
			// run and the message prints none. Legal ONLY because the row says
			// so; a homed row that lost its address would be a hint pointing at
			// nothing, which is what checkVerbAliasRows refuses.
			if !homelessRefusalWord(word) {
				t.Errorf("%q prints no address but is not one of the withdrawn commands — a refusal with a home must send the reader there", word)
			}
			continue
		}
		if !refusalAddressDispatches(t, address) {
			t.Errorf("%q points at %q, which this binary does not run — the refusal's hint leads nowhere", word, address)
		}
	}

	// ARM (3) FOR THE GUARD'S OWN ADDRESS TABLE: the corrected line the guard
	// prints must be the SAME address the argv[0] refusal prints, or the two
	// shells of one refusal disagree — which is how `gopi --timeout 3 ui` came
	// to say "put the command first: `gopi ui`", a hint pointing straight back
	// into the refusal it came from (found by RUNNING the pair, not by reading
	// the code). A homeless row is "" on BOTH sides: the corrected-line slot is
	// then not rendered at all, which is why the empty string is a legal value
	// here and the message shape changes with it.
	//
	// It walks BOTH refusal tables, because the guard's vocabulary covers both:
	// the SYNONYM table (cli_verb_aliases.go) and the SPELLING table
	// (cli_undispatched_tool_words.go) — the latter's addresses were the flat
	// command words (`code-search`) until the retirement refused them, at which
	// point the correction had to move to the surviving command line too.
	toolWordList := make([]string, 0, len(undispatchedToolWords))
	for _, e := range undispatchedToolWords {
		toolWordList = append(toolWordList, e.word)
	}
	seen := map[string]bool{}
	for _, table := range [][]string{verbAliasWords(), toolWordList} {
		for _, word := range table {
			seen[word] = true
			want := strings.TrimPrefix(refusalAddressOf(word), gopiProgramWord+" ")
			if want == gopiProgramWord {
				want = "" // a bare program word means "no address", not "the program"
			}
			got, ok := config.AliasAddresses()[word]
			if !ok {
				t.Errorf("%q is refused at argv[0] but absent from config's alias-address table — `gopi --timeout 3 %s` would print a corrected line for the wrong word", word, word)
			} else if got != want {
				t.Errorf("%q: the argv[0] refusal sends the reader to %q, but the positional refusal corrects it to %q — the two shells of one refusal disagree", word, want, got)
			}
		}
	}
	for word, addr := range config.AliasAddresses() {
		if !seen[word] {
			t.Errorf("config's alias-address table carries %q (→ %q), which cmd/agent does not refuse — the two vocabularies disagree", word, addr)
		}
	}

	guarded := map[string]bool{}
	for _, v := range config.PositionalVerbs {
		if guarded[v] {
			t.Fatalf("config.PositionalVerbs lists %q twice", v)
		}
		guarded[v] = true
		if dispatched[v] {
			continue
		}
		if _, ok := spurious[v]; !ok {
			t.Errorf("config.PositionalVerbs guards %q, which this binary neither dispatches nor names as a refused spelling/synonym of a real command — a word with no command behind it must stay prompt text", v)
		}
	}
	for v := range dispatched {
		if !guarded[v] {
			t.Errorf("verb %q is dispatched but missing from config.PositionalVerbs — `gopi --timeout 3 %s` would still run as a prompt", v, v)
		}
	}
	// The other direction for class (b): a word refused at argv[0] must be
	// refused after a run flag too, or the two refusal surfaces disagree and
	// the hole stays open in whichever order the operator typed.
	for word := range spurious {
		if !guarded[word] {
			t.Errorf("%q is refused by name at argv[0] (cli_undispatched_tool_words.go's spelling table, or cli_verb_aliases.go's synonym table) but missing from config.PositionalVerbs — `gopi --timeout 3 %s` would still run as a prompt", word, word)
		}
	}
}

// TestRunPathRefusesVerbAfterRunFlag is the bug's acceptance probe on the real
// binary: the flag-prefixed verb is refused with a pointed message, exits
// non-zero, and mints NOTHING (no session directory = no run path, hence no
// ledger row — the row registration is gated on the session log).
func TestRunPathRefusesVerbAfterRunFlag(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range []struct {
		name string
		args []string
		verb string
		// address is the live command line the refusal must send the reader to
		// — NOT always `tc.verb`. Both ways of reaching this guard were probed
		// for this union and they disagree in exactly one case, which is why
		// the expectation is a field rather than a derivation:
		//
		//   * a DISPATCHED verb (`version`, `config`) is at the top level, so
		//     "put the command first" is the truth and the address IS the word;
		//   * a RETIRED verb (`resume`, `stats`) is dispatched NOWHERE at the
		//     top level since the verb-retirement epic, so its guard-side
		//     address carries the SURVIVING command line (`session resume`).
		//
		// The branch this merged from got here by another route — its commit
		// removed the four words from the chain, which made them aliases with a
		// two-word address in `internal/config`'s table below. Master's later
		// pass reached the same four words and left their rows alone, so master
		// still prints `Put the command first: \`gopi resume\`` and this test —
		// the only LIVE check of that sentence — still expected it. Measured on
		// the merged tree (job-4, 2026-10-06): both cases failed with the real
		// message, which reads `Put the command first: \`gopi session resume\``.
		// The branch's corrected line is the accurate one: a word nothing
		// dispatches must not be advertised as a top-level command.
		address string
	}{
		{name: "the reported repro", args: []string{"--timeout", "3", CliVerbVersion}, verb: CliVerbVersion, address: CliVerbVersion},
		{name: "group verb with a sub-verb", args: []string{"--timeout", "3", CliVerbConfig, "show"}, verb: CliVerbConfig, address: CliVerbConfig},
		{name: "resume", args: []string{"--timeout", "3", CliVerbResume, "no-such-thing-at-all"}, verb: CliVerbResume, address: CliVerbSession + " " + CliVerbResume},
		{name: "stats (absent from the completion set)", args: []string{"--timeout", "3", CliVerbStats}, verb: CliVerbStats, address: CliVerbSession + " " + CliVerbStats},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			// RAW: the refusal is the subject; the family door maps argv[0] and
			// would answer a different question.
			out, err := runCLIContractProbeRaw(t, bin, home, tc.args...)
			if err == nil {
				t.Fatalf("gopi %s: exit 0 — the verb was swallowed instead of refused\n--- output ---\n%s",
					strings.Join(tc.args, " "), out)
			}
			// The message must NAME the word, the address that WORKS and the
			// prompt escape: a bare "refused" leaves the operator with no way
			// forward. The `gopi <address>` form is asserted first because it is
			// the one the operator copies — and because it is the only one of
			// the three that can be wrong while still looking right.
			for _, want := range []string{tc.verb, "`" + gopiProgramWord + " " + tc.address + "`", "--prompt " + tc.verb} {
				if !strings.Contains(out, want) {
					t.Errorf("gopi %s: refusal does not mention %q\n--- output ---\n%s",
						strings.Join(tc.args, " "), want, out)
				}
			}
			assertNoRunArtifacts(t, strings.Join(tc.args, " "), home, out)
		})
	}
}

// assertNoRunArtifacts proves the refusal happened BEFORE the run path: no
// session directory was minted under the sandbox GOPI_HOME (the run path's own
// signature — GOPI_HOME is the PARENT of the data dir, so the session store is
// $GOPI_HOME/.gopi/sessions), and the run path never printed its ledger
// registration banner. A level error here would make the assertion vacuous,
// which is why both spellings are checked.
func assertNoRunArtifacts(t *testing.T, argv, home, output string) {
	t.Helper()
	for _, dir := range []string{filepath.Join(home, ".gopi", "sessions"), filepath.Join(home, "sessions")} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue // no session dir at all — the good case
		}
		if err != nil {
			t.Fatalf("gopi %s: read sandbox sessions dir %s: %v", argv, dir, err)
		}
		if len(entries) > 0 {
			t.Fatalf("gopi %s: minted session artifact(s) under %s — the verb reached the run path instead of being refused", argv, dir)
		}
	}
	if strings.Contains(output, "task ledger: registered") {
		t.Fatalf("gopi %s: the run path registered a ledger row — the verb was swallowed\n--- output ---\n%s", argv, output)
	}
}

// TestRunPathStillRunsBarePromptShapes is the other side of the guard: the
// shapes that ARE a run must be untouched by it. Every case here must reach
// the run path — asserted by its own signature (a session directory minted
// under the sandbox GOPI_HOME) and by the ABSENCE of the refusal text.
//
// The provider is an unreachable local port with a dummy key, so the run fails
// in seconds and nothing is sent anywhere; what is asserted is the ROUTE, not
// the answer (the same technique as cli_contracts_test.go's no-agent-loop
// contract, inverted: here the run path is the expected destination).
func TestRunPathStillRunsBarePromptShapes(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	refusedProvider := []string{"--base-url", "http://127.0.0.1:9", "--api-key", "dummy"}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "literal prompt", args: []string{"--timeout", "3", "say hi"}},
		{name: "non-verb word", args: []string{"--timeout", "3", "frobnicate"}},
		{name: "explicit --prompt version", args: []string{"--timeout", "3", "--prompt", CliVerbVersion}},
		{name: "verb word beside an explicit prompt", args: []string{"--timeout", "3", "--prompt", "summarize this", CliVerbVersion}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			args := append(append([]string{}, refusedProvider...), tc.args...)
			out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout, args...) //stylegate:ignore runs the fixture probe; the captured output is the assertion
			if strings.Contains(out, "is a gopi command, not prompt text") {
				t.Fatalf("gopi %s: REFUSED a legitimate run\n--- output ---\n%s", strings.Join(args, " "), out)
			}
			if !sessionMinted(home) {
				t.Fatalf("gopi %s: no session directory was minted — the shape did not reach the run path (behavior changed)\n--- output ---\n%s",
					strings.Join(args, " "), out)
			}
		})
	}
}

// TestRunPathRefusesVerbWithAmbientPromptInEnv is the regression pair for task
// 01a0de86 (2026-09-26): IDENTICAL argv, the ambient GOPI_PROMPT the only
// difference, and the SAME refusal both times — no run, no session, no ledger
// row.
//
// The defect it pins: the guard asked the environment whether a prompt existed
// (`os.Getenv(PromptEnvVar) != ""`) instead of asking what prompt was in force.
// A probe spelling `gopi --gopi-prompt 1 version` — the documented
// env-name-as-flag equivalence of `--prompt 1`, where the prompt "1" was
// legitimate FOR THAT INVOCATION — published GOPI_PROMPT=1 into the process
// environment. The variable outlives the process, so every later unrelated
// `gopi --timeout 3 version` found the guard silent and started a REAL run
// whose prompt was the junk "1" while the leftover `version` was swallowed: a
// session dir, an `[auto] 1` ledger row, a worker slot.
//
// Both legs go through the REAL binary (Rule 31), because the question is what
// the process does, not what a predicate returns: the env var must reach the
// child as an ordinary ambient variable, exactly as a probe's leak does.
func TestRunPathRefusesVerbWithAmbientPromptInEnv(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range []struct {
		name   string
		envVal string
	}{
		{name: "GOPI_PROMPT unset", envVal: ""},
		{name: "GOPI_PROMPT set by a probe (the leak)", envVal: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			args := []string{"--timeout", "3", CliVerbVersion}
			out, err, timedOut := cliContractProbeEnvVar(bin, home, cliVerbGuardRunProbeTimeout, config.PromptEnvVar, tc.envVal, args...)
			if timedOut {
				t.Fatalf("gopi %s: TIMEOUT — the run path was entered instead of refusing", strings.Join(args, " "))
			}
			if err == nil {
				t.Fatalf("gopi %s (GOPI_PROMPT=%q): exit 0 — the verb was swallowed instead of refused\n--- output ---\n%s",
					strings.Join(args, " "), tc.envVal, out)
			}
			if !strings.Contains(out, "is a gopi command, not prompt text") {
				t.Fatalf("gopi %s (GOPI_PROMPT=%q): the refusal text is absent — this is not the verb refusal\n--- output ---\n%s",
					strings.Join(args, " "), tc.envVal, out)
			}
			assertNoRunArtifacts(t, strings.Join(args, " "), home, out)
		})
	}
}

// TestRunPathStillRunsWithExplicitPromptBesideAFilePositional is the
// DO-NOT-WEAKEN half, through the real binary: `--prompt X <file>` must still
// run, with the positional treated as an input FILE rather than a mistyped
// verb. Its signature is the run path itself (a session dir under the sandbox
// GOPI_HOME) and the absence of the refusal text.
func TestRunPathStillRunsWithExplicitPromptBesideAFilePositional(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	taskFile := filepath.Join(t.TempDir(), "task.txt")
	if err := os.WriteFile(taskFile, []byte("say hi\n"), 0o600); err != nil {
		t.Fatalf("write task file: %v", err)
	}
	// A verb WORD as the file's name is the shape the guard must not touch: an
	// explicit --prompt is the operator saying "this word is input".
	verbFile := filepath.Join(t.TempDir(), CliVerbVersion)
	if err := os.WriteFile(verbFile, []byte("say hi\n"), 0o600); err != nil {
		t.Fatalf("write verb-named task file: %v", err)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "--prompt with an explicit text and a file positional", args: []string{"--timeout", "3", "--prompt", "summarize", taskFile}},
		{name: "--prompt with a verb-named file positional", args: []string{"--timeout", "3", "--prompt", "summarize", verbFile}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--base-url", "http://127.0.0.1:9", "--api-key", "dummy"}, tc.args...)
			out, _ := runCLIContractProbeEnvTimeout(t, bin, home, cliVerbGuardRunProbeTimeout, args...) //stylegate:ignore runs the fixture probe; the captured output is the assertion
			if strings.Contains(out, "is a gopi command, not prompt text") {
				t.Fatalf("gopi %s: REFUSED a legitimate run\n--- output ---\n%s", strings.Join(args, " "), out)
			}
			if !sessionMinted(home) {
				t.Fatalf("gopi %s: no session directory was minted — the shape did not reach the run path (behavior changed)\n--- output ---\n%s",
					strings.Join(args, " "), out)
			}
		})
	}
}

// sessionMinted reports whether the sandbox GOPI_HOME gained a session
// artifact — the run path's own signature, and (per cli_version_test.go) the
// gate the ledger registration rides on.
func sessionMinted(home string) bool {
	for _, dir := range []string{filepath.Join(home, ".gopi", "sessions"), filepath.Join(home, "sessions")} {
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// runCLIContractProbeEnvTimeout is runCLIContractProbeEnv with a caller-chosen
// bound: the shared probe uses the 15s no-agent-loop detector, while the cases
// here EXPECT the run path and only need a hang guard.
func runCLIContractProbeEnvTimeout(t *testing.T, bin, home string, limit time.Duration, args ...string) (string, error) {
	t.Helper()
	out, err, timedOut := cliContractProbeWithTimeout(bin, home, limit, args...)
	if timedOut {
		t.Fatalf("gopi %s: TIMEOUT after %v\n--- output ---\n%s", strings.Join(args, " "), limit, out)
	}
	return out, err
}

// cliContractProbeEnvVar runs the probe with ONE extra environment entry
// (`<name>=<value>`) on top of the isolated base environment, so a pair of
// probes can differ in exactly one ambient variable and nothing else.
//
// envName comes from config (Rule 1b/14): the regression this serves is about
// THIS variable's channel, and a re-typed spelling would quietly stop testing
// it if the constant ever moved.
func cliContractProbeEnvVar(bin, home string, limit time.Duration, envName, envVal string, args ...string) (string, error, bool) {
	return cliContractProbeWithEnv(bin, home, limit, []string{envName + "=" + envVal}, args...)
}
