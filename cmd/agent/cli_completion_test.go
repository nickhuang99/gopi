package main

// cli_completion_test.go — locks the completion candidate sets (cli_completion.go)
// against the ACTUAL dispatch surfaces, so a new verb/subcommand added to a
// dispatcher without updating the completion table fails the test. Also
// guards the generated scripts' key invariants (bash/zsh/fish syntax sanity,
// every group verb present, every top-level verb present).

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

// TestCompletionCandidateSetsCoverTopLevel locks the first-level candidate
// set against the dispatch chain in main() (the isXxxSubcommand predicates).
//
// THE MIRROR IS THE DISPATCH SURFACE, AND NOTHING ELSE (epic verb-retirement,
// 2026-10-05). This list used to hold ~35 words because the dispatch chain
// carried ~35 entries; the retirement removed one entry from each half at the
// same time, so `want` below shrank with the chain rather than being
// overridden. A word may not be re-added here "because it is still useful":
// completion is the one surface that TEACHES a spelling, and the whole point
// of the epic is that the retired spellings must not be taught (they refuse
// now — cli_verb_aliases.go is where they live, with the address).
func TestCompletionCandidateSetsCoverTopLevel(t *testing.T) {
	// Every verb the dispatch chain recognizes must be completable.
	//
	// EVERY ENTRY IS THE CONSTANT (Rule 1/13/14). The list was a mixture of
	// bare literals and constants; the literals are what let it drift from the
	// table it mirrors (a rename in cli_verbs.go was invisible to a word typed
	// here), so the residue was constant-ised with the rest of this package's
	// argv spellings (task 01a10e039a5e7a31ad0ba8455e7dc3ee, 2026-10-06).
	want := []string{
		// EVERY ENTRY IS THE CONSTANT (Rule 1/13/14), and the list is EXACTLY
		// topLevelCommandNames — the retirement's surface, where every local
		// tool and the four session readers are reached through their family
		// door instead of a second top-level spelling. The words master's argv
		// round kept here (`archive`, `checkpoint`, `capture`, `proxy`, `shot`,
		// `annotate`, `gif`, `ledger-window`, `text2img-verify`, `sandbox`,
		// `setup`, the tool wire names …) are TOP-LEVEL verbs on master and are
		// REFUSED on this branch: `gopi archive` answers with `gopi task
		// archive`, so offering it in completion would teach a spelling the
		// binary refuses. That is the union resolution of this file — master's
		// constant-ised STYLE with the branch's retired SET — and the two loops
		// below are what force them to agree in both directions.
		CliVerbVersion, CliVerbWorker, CliVerbMerger, CliVerbMemory, CliVerbWeb,
		CliVerbTask, CliVerbChat,
		CliVerbConfig,
		CliVerbNotify,
		CliVerbUI, // dispatched and UNPUBLISHED (master cc8289b0: 暂不暴露) — completionSuppressedVerbs withholds it
		CliVerbCLI,
		CliVerbUIState,
		// The session family verb (list/show/log/stats/raw/resume/checkpoint):
		// dispatched by isSessionSubcommand, so it belongs in this
		// hand-maintained mirror like every other verb.
		CliVerbSession,
		// The tool GROUP verb (cli_tool.go): the ONE door every local tool is
		// reached through since the retirement — the group names its commands on
		// `gopi tool -h`, so the top level offers the door, not the tools.
		CliVerbTool,
		// The screen family verb (cli_screen.go): dispatched by
		// isScreenSubcommand, the same shape as `session` and `tool` above — the
		// top level offers the door, and `gopi screen -h` names the sub-verbs.
		CliVerbScreen,
		// The SAME reader addressed at the top level (cli_screen_replay_verb.go):
		// dispatched by isScreenReplaySubcommand, so tab must offer it too — the
		// loop below is what makes the two directions agree.
		CliVerbScreenReplay,
	}
	for _, v := range want {
		if !containsString(topLevelCommandNames, v) {
			t.Errorf("topLevelCommandNames missing %q — add it (and the group's own completion set if new)", v)
		}
	}
	// And nothing completable may be a word the dispatcher does NOT handle —
	// a completion offering a dead verb is worse than none (usage error on
	// pick), and offering a RETIRED verb is worse still (it refuses by design).
	for _, v := range topLevelCommandNames {
		if !containsString(want, v) {
			t.Errorf("topLevelCommandNames contains %q but no dispatcher handles it", v)
		}
	}
}

// TestCompletionCandidateSetsMatchGroupDispatch locks the second-level sets
// against the real dispatch tables:
//   - memory set == memorySubcommandNames (cli_memory.go's canonical list)
//   - checkpoint set == the checkpoint dispatch cases
//   - web set == search|fetch
func TestCompletionCandidateSetsMatchGroupDispatch(t *testing.T) {
	// memory: the completion set must mirror the canonical group list the
	// dispatcher itself uses (memorySubcommandNames).
	for _, sub := range memorySubcommandNames {
		if !containsString(memoryCompletionNames, sub) {
			t.Errorf("memoryCompletionNames missing %q (memorySubcommandNames has it)", sub)
		}
	}
	for _, sub := range memoryCompletionNames {
		if !containsString(memorySubcommandNames, sub) {
			t.Errorf("memoryCompletionNames has %q but memorySubcommandNames does not — dispatch would reject it", sub)
		}
	}
}

// TestTaskCompletionNamesMatchDispatch locks the derivations in BOTH
// directions against the runTasksSubcommand dispatch switch source
// (cli_tasks.go):
//
//   - every case label must be a DECLARED dispatch spelling
//     (taskverbs.DispatchVerbs, itself the derived union of the four groups);
//   - every DECLARED spelling must have a case label (no stale vocabulary);
//   - a case label that is NOT in the advertised ledger surface must belong to
//     one of the named dispatch-only groups — sync (its own `-h`), git
//     infrastructure (the merger's `-h`) or the service aliases — so the
//     ledger surface is exactly the completion set and nothing more.
//
// The dispatch surface is resolved with the AST (dispatchVerbLabels, in
// cli_tasks_dispatch_gate_test.go), NOT by regex-scanning for quoted case
// labels: the labels are internal/taskverbs constants now (Rule 1b), and a
// text scan cannot see through a constant — it would have reported every verb
// as missing while the dispatch was in fact complete.
func TestTaskCompletionNamesMatchDispatch(t *testing.T) {
	src, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	labels := dispatchVerbLabels(t, src, taskverbConsts(t))
	seen := map[string]bool{}
	for _, verb := range labels {
		seen[verb] = true
	}
	if len(seen) < 20 {
		t.Fatalf("resolved only %d dispatch verbs — the AST scan lost the dispatcher", len(seen))
	}
	// Direction 1: the dispatcher accepts EXACTLY the declared union.
	declared := map[string]bool{}
	for _, v := range taskverbs.DispatchVerbs {
		declared[v] = true
	}
	for verb := range seen {
		if !declared[verb] {
			t.Errorf("runTasksSubcommand dispatches %q but taskverbs.DispatchVerbs does not declare it — add it to the right group in the leaf package", verb)
		}
	}
	for _, verb := range taskverbs.DispatchVerbs {
		if !seen[verb] {
			t.Errorf("taskverbs.DispatchVerbs declares %q but runTasksSubcommand has no case for it — a declared spelling must keep dispatching", verb)
		}
	}
	// Direction 2: the advertised ledger surface is the completion set; every
	// other dispatchable spelling is accounted for by a named group.
	nonLedger := map[string]string{}
	for _, v := range taskverbs.SyncVerbs {
		nonLedger[v] = "SyncVerbs"
	}
	for _, v := range taskverbs.MergerVerbs {
		nonLedger[v] = "MergerVerbs"
	}
	for _, v := range taskverbs.ServiceAliases {
		nonLedger[v] = "ServiceAliases"
	}
	// The RETIRED spellings (worker-run-entry 2026-09-16): `run` keeps a
	// dispatch case so a script that types it hears where it went, and is
	// advertised nowhere. It is a declared non-ledger group like the others —
	// if this arm is dropped, `run` fails as an undeclared dispatch-only verb.
	for _, v := range taskverbs.RemovedVerbs {
		nonLedger[v] = "RemovedVerbs"
	}
	for _, verb := range labels {
		if containsString(taskCompletionNames, verb) {
			continue // advertised ledger verb — tab offers it
		}
		if _, ok := nonLedger[verb]; !ok {
			t.Errorf("dispatch verb %q is neither in the ledger surface nor in a declared non-ledger group — it would be a dispatch-only spelling with no documented home", verb)
		}
	}
	for _, verb := range taskCompletionNames {
		if !seen[verb] {
			t.Errorf("taskCompletionNames has %q but runTasksSubcommand has no case for it", verb)
		}
	}
}

// TestTaskCompletionNamesMatchFamilyHelp locks the tab surface to the HELP
// surface (the Epic's core invariant: `gopi task <TAB>` must offer exactly the
// subcommands the family help documents — never dispatch-only internal verbs,
// never deprecated aliases). Every completion candidate must appear as a
// documented "gopi task X" line in tasksUsage (cli_tasks.go); help is the
// single source of truth the tab follows.
//
// NEW INVARIANT (CLI-surface restructure 2026-09-15): the documented set is
// now a SUPERSET — the ledger verbs (completion candidates, each with its own
// usage block) plus the cross-node sync verbs, which the family help names as a
// folded synopsis and hands to their own manual. So the test asserts:
//
//   - every completion candidate IS documented here (the ledger surface);
//   - every documented `gopi task X` line names either a ledger verb or a
//     SyncVerbs member — a service or git-infrastructure verb re-appearing as
//     a documented line fails TestNoServiceOrSyncVerbInLedgerGroup.
func TestTaskCompletionNamesMatchFamilyHelp(t *testing.T) {
	documented := documentedFamilyVerbs(t)
	// Every completion candidate must be documented in the family help.
	for _, verb := range taskCompletionNames {
		if !documented[verb] {
			t.Errorf("taskCompletionNames offers %q but tasksUsage (the family help) documents no 'gopi task %s' line — tab must follow help: add the usage block", verb, verb)
		}
	}
	// The family help must not document a verb the tab does not offer, unless
	// it is a verb with an advertised home elsewhere (the sync verbs).
	for verb := range documented {
		if containsString(taskCompletionNames, verb) {
			continue
		}
		if containsString(taskverbs.SyncVerbs, verb) {
			continue // its detail is `gopi task sync -h`; the line here is a pointer
		}
		t.Errorf("tasksUsage documents 'gopi task %s' but it is neither a ledger verb nor a sync verb — every documented spelling needs exactly one advertised home", verb)
	}
}

// documentedFamilyVerbs extracts the "  gopi task <verb>" heads of the family
// usage constant.
func documentedFamilyVerbs(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	const marker = "const tasksUsage = `"
	i := strings.Index(string(src), marker)
	if i < 0 {
		t.Fatal("tasksUsage const not found in cli_tasks.go")
	}
	rest := string(src)[i+len(marker):]
	end := strings.Index(rest, "`")
	if end < 0 {
		t.Fatal("tasksUsage raw string unterminated in cli_tasks.go")
	}
	docRe := regexp.MustCompile(`(?m)^  gopi task ([a-z][a-z-]*)([ <\[]|$)`)
	documented := map[string]bool{}
	for _, m := range docRe.FindAllStringSubmatch(rest[:end], -1) {
		documented[m[1]] = true
	}
	return documented
}

// TestBashScriptStructure sanity-checks the emitted bash script: the register
// line, first-level and every group's subcommand must appear, and the script
// must be syntactically loadable (balanced case/esac via the top-level
// structure presence).
func TestBashScriptStructure(t *testing.T) {
	s := renderBashCompletion()
	for _, want := range []string{
		"complete -o default -F _gopi gopi",
		"_gopi()",
		"COMP_WORDS[COMP_CWORD]",
		// The PUBLISHED set, not topLevelCommandNames: the renderer's first-level
		// list is what `gopi <TAB>` actually OFFERS (topLevelCompletionNames =
		// dispatch vocabulary − completionSuppressedVerbs), so asserting the
		// dispatch vocabulary here demanded the script offer exactly the verbs
		// the suppression withholds. MEASURED (Rule 31): the pre-fix assertion
		// was red on master at b7b39a57 — `cc8289b0` moved the renderer onto
		// topLevelCompletionNames and left this line reading the wider list, so
		// the bash arm failed by name on the two verbs the ruling had just
		// withdrawn. Reading the published set keeps the two halves of one fact
		// (the renderer emits it, the test expects it) in ONE place.
		`compgen -W "` + strings.Join(topLevelCompletionNames(), " "),
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
	// Each group verb must have its subcommand compgen arm — and the SET is
	// DERIVED from the renderer's own table (subcommandSets), not hand-typed
	// beside it. A hand-typed pair drifted here exactly as this test's own
	// comment warns: it still named `checkpoint` and `completion` after the
	// renderer's table had moved them under their families (`session
	// checkpoint`, `config completion`), so the guard failed on words it had
	// invented rather than on the script (task 01a10e039a5e7a31ad0ba8455e7dc3ee, 2026-10-06).
	for _, v := range completionGroupVerbs() {
		if !strings.Contains(s, v+")") {
			t.Errorf("bash script missing group arm for %q", v)
		}
		if !strings.Contains(s, strings.Join(subcommandSets[v], " ")) {
			t.Errorf("bash script missing %q subcommand list", v)
		}
	}
	// Code-search hyphen survives (the script's quoted compgen -W).
	if !strings.Contains(s, CliVerbCodeSearch) {
		t.Errorf("bash script missing code-search candidate")
	}
}

// completionGroupVerbs is the group-verb set a script must carry an arm for,
// read from the RENDERER'S OWN table (subcommandSets) so the assertion cannot
// name a group the renderer does not emit — the drift this file's own comment
// describes, in the one place the list had been typed by hand twice.
func completionGroupVerbs() []string {
	out := make([]string, 0, len(subcommandSets))
	for v := range subcommandSets {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// TestZshScriptStructure sanity-checks the zsh function (autoload
// convention: #compdef header + raw function body, no wrapper).
func TestZshScriptStructure(t *testing.T) {
	s := renderZshCompletion()
	for _, want := range []string{
		"#compdef gopi",
		"commands=(",
		"_describe 'command' commands",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
	if strings.Contains(s, "compdef _gopi gopi") {
		t.Errorf("zsh autoload file must NOT carry a trailing compdef (that is the sourced-script convention)")
	}
	if strings.Contains(s, "_gopi() {") {
		t.Errorf("zsh autoload file must NOT wrap the body in a function (the file IS the body)")
	}
	for _, v := range completionGroupVerbs() {
		if !strings.Contains(s, v+")") {
			t.Errorf("zsh script missing group arm for %q", v)
		}
	}
	if !strings.Contains(s, CliVerbCodeSearch) {
		t.Errorf("zsh script missing code-search candidate")
	}
}

// TestFishScriptStructure sanity-checks the fish file.
func TestFishScriptStructure(t *testing.T) {
	s := renderFishCompletion()
	for _, want := range []string{
		"complete -c gopi -n '__fish_use_subcommand'",
		"complete -c gopi -n '__fish_seen_subcommand_from memory'",
		"complete -c gopi -n '__fish_seen_subcommand_from task'",
		"complete -c gopi -n '__fish_seen_subcommand_from web'",
		"code-search",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("fish script missing %q", want)
		}
	}
}

// TestCompletionUsageAdvertised checks the top-level -h advertises where the
// completion verb LIVES (discoverability standard). The address moved with the
// verb-retirement epic (2026-10-05): `gopi config completion` is `gopi config
// completion` now, and the index row spells the address the refusal prints —
// the two must agree, which is why the expectation names the group.
func TestCompletionUsageAdvertised(t *testing.T) {
	for _, want := range []string{
		gopiProgramWord + " " + CliVerbConfig + " " + CliVerbCompletion + " <shell>",
		"shell tab-completion",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level usage missing %q — the completion verb must be discoverable", want)
		}
	}
}

// TestCompletionHelpHasShells — `gopi config completion -h` must list the shells.
func TestCompletionHelpHasShells(t *testing.T) {
	for _, want := range []string{"bash", "zsh", "fish", "gopi config completion bash"} {
		if !strings.Contains(completionUsage, want) {
			t.Errorf("completionUsage missing %q", want)
		}
	}
}

// TestTaskCompletionNamesAreTheSharedFamilySurface pins the DERIVATION (Rule
// 13/14): the `gopi task <TAB>` candidate set must BE taskverbs.FamilyVerbs —
// the same shared leaf surface the top-level `gopi -h` task row enumerates —
// not a hand-typed copy that can drift from it. This is the completion half of
// the anti-restatement guard; internal/config's
// TestNoProductionFileRestatesTheTaskVerbEnumeration is the help half.
//
// NEW INVARIANT (CLI-surface restructure 2026-09-15): FamilyVerbs is the LEDGER
// surface, so this asserts the completion set is EXACTLY the ledger verbs —
// a dispatch-only spelling (a service alias, a git-infrastructure atom, a
// cross-node sync verb) that reappears here fails the disjointness half below.
func TestTaskCompletionNamesAreTheSharedFamilySurface(t *testing.T) {
	if len(taskCompletionNames) != len(taskverbs.FamilyVerbs) {
		t.Fatalf("taskCompletionNames has %d entries, taskverbs.FamilyVerbs has %d — the completion surface must derive from the shared list",
			len(taskCompletionNames), len(taskverbs.FamilyVerbs))
	}
	for i := range taskverbs.FamilyVerbs {
		if taskCompletionNames[i] != taskverbs.FamilyVerbs[i] {
			t.Errorf("taskCompletionNames[%d] = %q, taskverbs.FamilyVerbs[%d] = %q — tab must offer exactly the advertised family surface",
				i, taskCompletionNames[i], i, taskverbs.FamilyVerbs[i])
		}
	}
	if !containsString(taskCompletionNames, taskverbs.Status) {
		t.Errorf("taskCompletionNames omits %q — `gopi task status` dispatches (cli_tasks.go) and must be completable", taskverbs.Status)
	}
	// The ledger surface must not carry a dispatch-only group: completion is
	// the advertised surface, and those groups have their own homes.
	for _, v := range append(append([]string{}, taskverbs.SyncVerbs...), taskverbs.MergerVerbs...) {
		if containsString(taskCompletionNames, v) {
			t.Errorf("taskCompletionNames offers %q — it belongs to %s, which is advertised outside the ledger group (sync: `gopi task sync -h`; git infra: `gopi merger -h`)",
				v, groupOf(t, v))
		}
	}
	for _, v := range taskverbs.ServiceAliases {
		if containsString(taskCompletionNames, v) {
			t.Errorf("taskCompletionNames offers the service alias %q — merger is canonical and adverised at top level (`gopi merger -h`)", v)
		}
	}
}

// groupOf names the group a dispatch-only spelling belongs to (diagnostics).
func groupOf(t *testing.T, verb string) string {
	t.Helper()
	for _, g := range []struct {
		name string
		list []string
	}{
		{"taskverbs.SyncVerbs", taskverbs.SyncVerbs},
		{"taskverbs.MergerVerbs", taskverbs.MergerVerbs},
		{"taskverbs.ServiceAliases", taskverbs.ServiceAliases},
		{"taskverbs.RemovedVerbs", taskverbs.RemovedVerbs},
	} {
		for _, v := range g.list {
			if v == verb {
				return g.name
			}
		}
	}
	return "a dispatch-only group"
}
