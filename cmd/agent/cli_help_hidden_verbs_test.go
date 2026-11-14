package main

// cli_help_hidden_verbs_test.go — the REVERSE guard for the publish decision
// (operator ruling, 2026-10-06, task 01a10dba…): `gopi ui` and `gopi cli` are
// "暂不暴露" — their code stays and the commands still run, but NOTHING
// advertises them.
//
// WHY A REVERSE GUARD AND NOT JUST THE EDIT. Every other test in this package
// pushes the surface in ONE direction: a dispatched verb MUST be advertised
// (usage_test.go's TestTopLevelUsageAdvertisesEveryDispatchedVerb /
// TestTopLevelHelpCoversEveryDispatchedSubcommand, cli_verb_census_test.go's
// `advertised` column). Left alone, that direction makes the suppression
// unrepresentable — the first person to notice a red build "fixes" it by putting
// the two rows back, and the operator's decision is undone by a test that
// cannot tell a decision from an omission. So this file asserts the opposite
// direction for exactly the verbs the ruling names, and it asserts it against
// the BUILT BINARY, not against the help constant: a stale binary is the
// classic false green (Rule 31 — the surface must be executed).
//
// THE THREE THINGS IT PINS
//
//	1. the published surfaces are silent about them — `gopi -h` has no index row
//	   and no `gopi <TAB>` candidate list offers the word (bash/zsh/fish, the
//	   three scripts that are actually installed);
//	2. the verbs still WORK — `gopi ui -h` / `gopi cli -h` run and print their
//	   own page, so "hidden" never decays into "removed" (the ruling was, and is,
//	   code-and-command-kept);
//	3. the index points at nothing it does not carry — the general form of the
//	   defect this task also fixed (`gopi ui_state`'s summary used to read "read a
//	   live `gopi ui` screen…", a pointer to a row the index had just dropped:
//	   Rule 15, a dead end in the front door).
//
// The word check is ANCHORED (`^  gopi <verb>` / a bare candidate word), never a
// substring: `gopi ui_state` CONTAINS "gopi ui", so strings.Contains would report
// the suppression as leaked while the screen is correct — the false positive that
// makes a guard useless.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// helpIndexVerbs is the ONE reader of an index verb from rendered help text: the
// rows the renderer indents by two spaces, the word being the first token after
// `gopi `. It is the same shape internal/standards/commands-census.go and
// cmd/agent/cli_verb_census_test.go read, so none of the three can disagree about
// what a page advertises.
var helpIndexVerbs = regexp.MustCompile(`(?m)^ {2}gopi ([a-z][a-z0-9_-]*)(?: |$)`)

// helpIndexVerbSet renders a help page and returns the verbs its INDEX rows
// advertise (keys of the returned map).
func helpIndexVerbSet(t *testing.T, page string) map[string]bool {
	t.Helper()
	return helpIndexVerbSetFrom(t, page, "the rendered page")
}

// helpIndexVerbSetFrom is the same reader over a page whose provenance the caller
// names — so a binary run and an in-process render are distinguishable in the
// failure text.
func helpIndexVerbSetFrom(t *testing.T, page, what string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range helpIndexVerbs.FindAllStringSubmatch(page, -1) {
		out[m[1]] = true
	}
	if len(out) < censusMinRows {
		t.Fatalf("%s advertised only %d verbs (floor %d) — the index did not render, so this guard would pass blind",
			what, len(out), censusMinRows)
	}
	return out
}

// TestTopLevelHelpHidesUnadvertisedVerbs is the acceptance check of the ruling,
// on the REAL binary AND on the in-process render (the same two surfaces the
// census gate reads, so the suppression cannot be true of one and false of the
// other).
func TestTopLevelHelpHidesUnadvertisedVerbs(t *testing.T) {
	bin := gopiTestBinPath(t)
	out, err := runCLIContractProbeEnv(t, bin, t.TempDir(), HelpFlagLong)
	if err != nil {
		t.Fatalf("`gopi --help` exited with an error: %v\n--- output ---\n%s", err, out)
	}

	for _, verb := range completionSuppressedVerbs {
		// The anchored row shape: `  gopi ui` (optionally followed by an
		// operand), never `  gopi ui_state` — see the file comment.
		row := regexp.MustCompile(`(?m)^ {2}gopi ` + regexp.QuoteMeta(verb) + `(?: |$)`)
		if m := row.FindString(out); m != "" {
			t.Errorf("the built binary's `gopi -h` still publishes the suppressed verb %q:\n  %s\n"+
				"The operator ruled it 暂不暴露 (task 01a10dba…): remove its config.commandIndex row.",
				verb, strings.TrimSpace(m))
		}
		if configAdvertisesVerb(verb) {
			t.Errorf("internal/config's rendered index (config.FormatUsage) still publishes the suppressed verb %q — "+
				"the binary and the constant disagree, so one of the two was edited alone", verb)
		}
	}

	// Non-vacuity, in the other direction: the verbs the operator KEPT must still
	// be there. Without this, deleting the whole index would pass.
	for _, kept := range []string{CliVerbChat, CliVerbWeb, CliVerbUIState} {
		if !strings.Contains(out, "gopi "+kept) {
			t.Errorf("the built binary's `gopi -h` no longer mentions %q — the suppression was meant to hide two verbs, not the group", kept)
		}
	}
}

// configAdvertisesVerb reports whether internal/config's rendered top-level index
// carries an index ROW for verb (anchored, never a substring).
func configAdvertisesVerb(verb string) bool {
	row := regexp.MustCompile(`(?m)^ {2}gopi ` + regexp.QuoteMeta(verb) + `(?: |$)`)
	return row.MatchString(config.FormatUsage())
}

// TestSuppressedVerbsStillDispatch is the other half of the ruling, and the half
// an over-eager clean-up would break: hidden is NOT removed. Every suppressed verb
// must still be ROUTED by the real binary — and WHAT IT ANSWERS depends on which
// kind of suppression it is (see completionSuppressedVerbs):
//
//	(a) LIVE (ui, cli): `gopi <verb> -h` exits 0 with its OWN page.
//	(b) RETIRED (merger): `gopi <verb> -h` exits NON-ZERO with the retirement
//	    sentence, which NAMES the verb and the surviving face. The word routing
//	    is the whole point — a caller that kept the old spelling in a script or a
//	    service unit must read a decision, not "unknown command" (Rule 15).
//
// The shared assertion is the one that makes both halves the same rule: the verb
// does not fall through to the top-level index (that would be the "de-dispatched"
// failure in either direction), and it is a word the dispatch chain really
// carries.
func TestSuppressedVerbsStillDispatch(t *testing.T) {
	bin := gopiTestBinPath(t)
	// The kind of each suppressed verb, stated once. `merger` is the retired
	// one; the other two are live commands whose publicity was withdrawn
	// (operator ruling 2026-10-06, task 01a10dba…).
	retired := map[string]bool{CliVerbMerger: true}
	for _, verb := range completionSuppressedVerbs {
		t.Run(verb, func(t *testing.T) {
			out, err := runCLIContractProbeEnv(t, bin, t.TempDir(), verb, HelpFlag)
			if retired[verb] {
				if err == nil {
					t.Fatalf("`gopi %s -h` exited 0 — a RETIRED verb answered a help query with a page.\n"+
						"That is the state this task removed from the index: the row promised \"accepts -h for its full help\" and the verb cannot honour it.\n--- output ---\n%s", verb, out)
				}
				// The retirement must be EXPLAINED, not silent, and it must name
				// the word the operator typed plus what replaced it.
				if !strings.Contains(out, verb) {
					t.Errorf("`gopi %s -h` refuses without naming the verb — a refusal that does not say what was refused is a bare error (Rule 15):\n%s", verb, out)
				}
				if !strings.Contains(out, "retired") {
					t.Errorf("`gopi %s -h` refuses without stating the retirement, so a caller cannot tell a decision from a typo:\n%s", verb, out)
				}
				// It must NOT claim a surviving `gopi merge` face: that word
				// left the command surface (epic merge-removal item 2), so a
				// refusal pointing at it would send the reader from one refusal
				// into another — the dead-end hint checkVerbAliasRows refuses.
				// What it must carry instead is the corrected pointer sentence.
				if !strings.Contains(out, mergerRetiredPointer) {
					t.Errorf("`gopi %s -h` refuses without the pointer sentence %q:\n%s", verb, mergerRetiredPointer, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("`gopi %s -h` exited with an error (%v) — the verb was de-dispatched rather than un-advertised\n--- output ---\n%s",
					verb, err, out)
			}
			// Its OWN page, not the top-level index (which is what a
			// fall-through to the bare-prompt/help path would print).
			if !strings.Contains(out, "gopi "+verb) {
				t.Errorf("`gopi %s -h` does not print a page naming the verb:\n%s", verb, out)
			}
			if strings.Contains(out, "Every local tool under one noun") {
				t.Errorf("`gopi %s -h` printed the TOP-LEVEL index — the verb fell through to the usage path", verb)
			}
		})
		// Both kinds share this: the word is in the DISPATCH inventory, so a
		// rename cannot leave the guard above probing a word nobody routes —
		// suppression is a publish decision, never a de-dispatch.
		if _, ok := dispatcherOfToken[verb]; !ok {
			t.Errorf("%q is suppressed but has no dispatcherOfToken entry — it was de-dispatched, and suppression is not de-dispatch", verb)
		}
	}
}

// TestCompletionSuppressesTheSameVerbsAsHelp pins the invariant the CLI-completion
// epic states ("completion follows the help") to the suppression: the `gopi <TAB>`
// candidate lists must offer exactly the verbs `gopi -h` explains. Both directions
// are checked, so re-adding either half alone is red:
//
//	(a) no suppressed verb appears in ANY generated script's FIRST-LEVEL list;
//	(b) the first-level list equals the built binary's own index verb set.
//
// Each shell's script is parsed for ITS first-level list (`compgen -W "…"` for
// bash, the `commands=(…)` block for zsh, the `__fish_use_subcommand` line for
// fish) — never for the whole file: the second-level arms legitimately carry words
// that are not top-level verbs.
func TestCompletionSuppressesTheSameVerbsAsHelp(t *testing.T) {
	bin := gopiTestBinPath(t)
	out, err := runCLIContractProbeEnv(t, bin, t.TempDir(), HelpFlagLong)
	if err != nil {
		t.Fatalf("`gopi --help` failed: %v", err)
	}
	advertised := helpIndexVerbSet(t, out)

	for _, shell := range completionShellNames {
		var script string
		switch shell {
		case "bash":
			script = renderBashCompletion()
		case "zsh":
			script = renderZshCompletion()
		case "fish":
			script = renderFishCompletion()
		default:
			t.Fatalf("no renderer for shell %q — the candidate list to check would be guessed", shell)
		}
		offered := firstLevelCandidates(t, shell, script)

		for _, verb := range completionSuppressedVerbs {
			if offered[verb] {
				t.Errorf("the generated %s script offers %q as a first-level candidate:\n"+
					"  the word is suppressed from `gopi -h`, and completion follows the help — a candidate the help does not explain "+
					"is exactly the drift TestTopLevelHelpCoversEveryDispatchedSubcommand's reverse direction forbids", shell, verb)
			}
		}
		// (b) the two published surfaces agree, word for word.
		for v := range offered {
			if !advertised[v] {
				t.Errorf("the %s script offers %q, which the built binary's `gopi -h` does not explain", shell, v)
			}
		}
		// (b) is deliberately ONE-WAY — offered ⇒ explained — and the other
		// direction is NOT asserted here. MEASURED on this tree: the index
		// carries three rows the candidate list does not offer (`act`, `stats`,
		// `raw`, all of them ALIAS rows — the help tells a reader what they
		// alias, while completion offers the canonical spelling only). That is a
		// pre-existing, deliberate difference in what the two surfaces are FOR,
		// not drift this task introduced, and asserting equality would fail the
		// suite for a reason unrelated to the suppression — a guard that cries
		// wolf is one people learn to skip. What IS asserted is the direction
		// that makes an offer safe: a candidate must be a word the help explains.
		_ = advertised
		if len(offered) < censusMinRows {
			t.Fatalf("the %s first-level list parsed as %d words (floor %d) — the parse broke and the checks above are vacuous",
				shell, len(offered), censusMinRows)
		}
	}
}

// firstLevelCandidates extracts ONE shell script's `gopi <TAB>` word list. Each
// arm is anchored on the renderer's own spelling (cli_completion.go), so a
// renderer rewrite fails here loudly instead of yielding an empty set that makes
// the comparison vacuous.
func firstLevelCandidates(t *testing.T, shell, script string) map[string]bool {
	t.Helper()
	var body string
	switch shell {
	case "bash":
		m := regexp.MustCompile(`(?m)^ +COMPREPLY=\( \$\(compgen -W "([^"]*)" -- "\$cur"\) \)$`).FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("the bash script has no first-level compgen line — the renderer changed shape")
		}
		body = m[1]
	case "fish":
		m := regexp.MustCompile(`(?m)^complete -c gopi -n '__fish_use_subcommand' -a '([^']*)'$`).FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("the fish script has no first-level `complete -c gopi` line — the renderer changed shape")
		}
		body = m[1]
	case "zsh":
		m := regexp.MustCompile(`(?s)commands=\(\n(.*?)\n\)\n`).FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("the zsh script has no `commands=(…)` block — the renderer changed shape")
		}
		body = m[1]
	default:
		t.Fatalf("no first-level reader for shell %q", shell)
	}
	out := map[string]bool{}
	for _, w := range regexp.MustCompile(`[a-z][a-z0-9_-]*`).FindAllString(body, -1) {
		out[w] = true
	}
	return out
}

// TestTopLevelHelpPointsAtNoCommandItDropped is the GENERAL form of the dangling
// reference this task fixed, so the next suppression cannot reintroduce it
// silently: every command the index NAMES in a code span (`gopi <word>`) must be
// a command the same index carries.
//
// It caught the real defect: `gopi ui_state`'s summary read "read a live
// `gopi ui` screen as a cell grid", and after the ruling the index no longer
// carried `gopi ui` — a front-door pointer to a row that is not there (Rule 15:
// silence is the defect, and so is a pointer that resolves to nothing).
//
// The scan is over the RENDERED page (so a wrapped or reflowed row is judged as
// the reader sees it) and reads ONLY backticked spans: `gopi --help-env` is a
// flag, `gopi <cmd> -h` is a placeholder, and prose that merely names a verb is
// not a pointer.
func TestTopLevelHelpPointsAtNoCommandItDropped(t *testing.T) {
	usage := config.FormatUsage()
	carried := helpIndexVerbSet(t, usage)

	spans := regexp.MustCompile("`gopi ([a-z_][a-z0-9_-]*)").FindAllStringSubmatch(usage, -1)
	if len(spans) < 5 {
		t.Fatalf("found only %d backticked `gopi <word>` spans — the index no longer points anywhere, so this guard is vacuous", len(spans))
	}
	for _, m := range spans {
		word := m[1]
		if carried[word] {
			continue
		}
		t.Errorf("the top-level index names `gopi %s` in a code span but carries no `gopi %s` row — "+
			"the pointer sends the reader to a command this page does not publish (the `gopi ui_state` defect, task 01a10dba…).\n"+
			"Either reword the span to name what the command does, or restore the row.", word, word)
	}
}

// TestHelpHiddenVerbsAreDispatchedNotGone is the read-floor for the suppression
// list itself: it must name verbs this binary dispatches (a suppressed word that
// routes nowhere is a typo, and every check above would pass on it), and it must
// be the SAME list the completion renderers subtract — asserted by construction
// (topLevelCompletionNames) rather than by comparing two hand-typed lists.
func TestHelpHiddenVerbsAreDispatchedNotGone(t *testing.T) {
	if len(completionSuppressedVerbs) == 0 {
		t.Fatal("the suppression list is empty — the ruling named two verbs, and an empty list silently un-does it")
	}
	seen := map[string]bool{}
	for _, v := range completionSuppressedVerbs {
		if seen[v] {
			t.Errorf("%q is listed twice — one row per suppressed verb", v)
		}
		seen[v] = true
		if _, ok := dispatcherOfToken[v]; !ok {
			t.Errorf("%q is suppressed but not dispatched by this binary — suppression is a publish decision, never a de-dispatch", v)
		}
		if !containsString(topLevelCommandNames, v) {
			t.Errorf("%q is suppressed but absent from topLevelCommandNames — the dispatch vocabulary and the suppression list disagree about what exists", v)
		}
	}
	// The published list is the dispatch list MINUS the suppressed set, by
	// construction: check both directions on the real function.
	published := topLevelCompletionNames()
	if len(published) != len(topLevelCommandNames)-len(completionSuppressedVerbs) {
		t.Errorf("topLevelCompletionNames() has %d words, want %d (%d dispatched − %d suppressed)",
			len(published), len(topLevelCommandNames)-len(completionSuppressedVerbs), len(topLevelCommandNames), len(completionSuppressedVerbs))
	}
	for _, v := range published {
		if seen[v] {
			t.Errorf("topLevelCompletionNames() still offers the suppressed verb %q", v)
		}
	}
}

// TestRetiredAliasWordsAreAbsentFromTheHelpIndex closes the ONE direction the
// merge-verb removal left unguarded, and it is the direction the removal was
// ABOUT: the acceptance condition of epic merge-removal item 2 is the operator
// -visible ask — "`gopi help` stops advertising a merge run" — and until this
// test nothing in this package tied that ask to anything a machine re-measures.
//
// WHY THE EXISTING GUARDS DO NOT COVER IT. `TestTopLevelHelpHidesUnadvertisedVerbs`
// above pins exactly the words the 2026-10-06 ruling names
// (completionSuppressedVerbs = {ui, cli, merger}); the `merge` word is not in
// that set, so a `merge` row reappearing on the index would pass it. The G7
// census (`cli_verb_census_test.go`) does compare `advertised` against the built
// binary, but (a) it is heavy and skips by default, and (b) its subject is the
// DISPATCHED verb set, from which `merge` has already left — the census records
// the demotion in prose, not as a row. So the removal's own claim ("no merge row
// on the front door") had no executable check; measured on this tree before this
// test, `grep -rn FormatUsage cli_verb_aliases_test.go` matched nothing.
//
// THE ASSERTION IS THE TABLE'S, NOT A HAND-WRITTEN LIST (Rules 13/14): every
// `verbAliases` word is a spelling this binary deliberately does NOT dispatch as
// a top-level command, so no such word may carry an index row. A row ADDED to the
// table arrives here with no test edit; a row whose word is put BACK into the
// index turns this red naming the word.
//
// ANCHORED ROWS, never substrings — the same reason the file comment gives:
// `gopi merge` is not a prefix of any current row, but `gopi log` IS a prefix of
// nothing and `gopi resume` would match a `gopi resume` line only, while a
// substring check would trip over prose like "resume an interrupted session"
// (which the index legitimately carries). The binary's page and the in-process
// render are BOTH read, so the edit of one alone cannot pass.
func TestRetiredAliasWordsAreAbsentFromTheHelpIndex(t *testing.T) {
	words := verbAliasWords()
	if len(words) < censusMinRows {
		t.Fatalf("the alias table listed only %d words (floor %d) — an empty or truncated table would make this guard pass blind", len(words), censusMinRows)
	}

	bin := gopiTestBinPath(t)
	out, err := runCLIContractProbeEnv(t, bin, t.TempDir(), HelpFlagLong)
	if err != nil {
		t.Fatalf("`gopi --help` exited with an error: %v\n--- output ---\n%s", err, out)
	}

	// Two readers over the same claim: the BUILT BINARY (Rule 31 — the surface
	// must be executed) and the in-process render (the constant the binary is
	// built from). A disagreement between them is itself the defect the
	// suppressed-verb guard names, so both are asserted.
	pages := []struct {
		what  string
		verbs map[string]bool
	}{
		{"the built binary's `gopi -h`", helpIndexVerbSetFrom(t, out, "the built binary's `gopi -h`")},
		{"internal/config's rendered index (config.FormatUsage)", helpIndexVerbSetFrom(t, config.FormatUsage(), "config.FormatUsage()")},
	}

	for _, p := range pages {
		for _, w := range words {
			if p.verbs[w] {
				t.Errorf("%s publishes an index row for %q — that word is in verbAliases, so the binary refuses it as a retired spelling and the row sends the reader from the front door into a refusal (epic merge-removal item 2: `gopi help` stops advertising the removed merge verb).\n"+
					"Remove the row from config.commandIndex; the refusal itself lives in cli_verb_aliases.go and must KEEP answering.",
					p.what, w)
			}
		}
	}

	// NON-VACUITY IN THE OTHER DIRECTION: the words the binary really dispatches
	// must still be on the page. Without this, emptying the index would pass.
	for _, kept := range []string{CliVerbTask, CliVerbChat, CliVerbTool, CliVerbMemory} {
		if !pages[0].verbs[kept] {
			t.Errorf("the built binary's `gopi -h` no longer publishes %q — this guard removes retired spellings, not the command surface", kept)
		}
	}
}
