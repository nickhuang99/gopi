package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// TestTopLevelUsageListsSubcommands guards against regressing "the user has
// no idea how to use the exposed CLI tools": gopi -h must advertise every
// command group so they are discoverable, while the DETAIL belongs to each
// command's own -h (the 2026-09-13 help-format epic: the top-level help is an
// index, not the manual).
func TestTopLevelUsageListsSubcommands(t *testing.T) {
	for _, sub := range []string{
		"gopi task <subcommand>", "gopi memory <subcommand>",
		"gopi tool vision_analyze",
		"gopi tool text2img", "gopi tool code-search", "gopi worker",
		"gopi tool <command>",
	} {
		if !strings.Contains(usage, sub) {
			t.Errorf("top-level gopi -h usage is missing %q:\n%s", sub, usage)
		}
	}
	// `gopi merger` is the ONE verb this page must NOT carry, and the absence is
	// a DECISION rather than an omission: the verb is RETIRED (2026-09-19) and
	// refuses at every spelling, `-h` included, while this page's own preamble
	// promises that every command it lists "accepts -h for its full help". A
	// row that cannot honour the promise is a front-door pointer to a dead end
	// (Rule 15) — task 01a113a8…, operator report 「merger 既然都不存在来为什么
	// 还要提一遍？这不是混淆吗？」.
	//
	// The presence check is ANCHORED (`  gopi merger` at a row head), never a
	// substring: `gopi merge` contains it, and so does nothing else on the page
	// — but the anchored form is what stays true if `merge` ever gains a
	// sub-word. The reverse direction lives in cli_help_hidden_verbs_test.go
	// (TestTopLevelHelpHidesUnadvertisedVerbs), which asserts it against the
	// BUILT BINARY for every suppressed verb; this one keeps the in-process
	// render honest so the two cannot disagree.
	row := regexp.MustCompile(`(?m)^ {2}gopi ` + regexp.QuoteMeta(CliVerbMerger) + `(?: |$)`)
	if m := row.FindString(usage); m != "" {
		t.Errorf("top-level gopi -h carries a `gopi merger` index row (%q) — the verb is retired and REFUSES -h, so the row cannot honour the page's own \"accepts -h for its full help\" promise (task 01a113a8…).\n"+
			"The word still routes and the retirement is documented on `gopi task -h`; what goes away is the front-door row.", strings.TrimSpace(m))
	}
	// The old flat verbs must NOT be advertised as the surface (they live on
	// only as deprecation aliases). Advertising them would push users back
	// to the inconsistent form the group restructure removes.
	for _, gone := range []string{
		"gopi memory_search", "gopi memory_store", "gopi memory_get",
		"gopi memory_update", "gopi memory_delete", "gopi memory_feedback_list",
		"gopi web_search", "gopi web_fetch",
	} {
		if strings.Contains(usage, gone) {
			t.Errorf("top-level gopi -h still advertises the flat verb %q — should show the grouped form", gone)
		}
	}
	if !strings.Contains(usage, "accepts -h for its full help") {
		t.Errorf("top-level usage should point users at per-subcommand -h:\n%s", usage)
	}
}

// TestTopLevelUsageAdvertisesEveryDispatchedVerb is the anti-omission guard
// the 2026-09-13 help-format epic exists for: `gopi tool proxy`, `gopi chat`,
// `gopi memory brew`, `gopi tool act`, `gopi tool voice_input`, `gopi tool scratch-gc` and
// `gopi tool screen_capture` were all dispatchable yet missing from `gopi -h`, so
// they were undiscoverable (memory #1252 flagged the proxy case as P0).
//
// The expected set is DERIVED, never hand-listed: every predicate in the
// main.go dispatch chain (cliContractDispatchPredicates, itself guarded by
// TestCLIContractInventoryMatchesDispatch against the real chain) must resolve
// through dispatcherOfToken to a token the top-level index advertises. Adding a
// subcommand to main.go therefore forces an entry here — the help can only
// become complete, never quietly incomplete.
func TestTopLevelUsageAdvertisesEveryDispatchedVerb(t *testing.T) {
	// The web tool mirrors are advertised under the GROUP word, `gopi tool web
	// search` (it dispatches the web_search wire verb) — the only tokens whose
	// advertised spelling differs from the dispatch token. The bare top-level
	// `gopi web search` / `gopi web fetch` rows were REMOVED from the index on
	// the operator's ruling (2026-10-28): sitting one line under
	// `gopi web [--tools]  Local browser chat UI (SSE)`, they made the two
	// meanings of `web` indistinguishable. Both spellings still dispatch; the
	// tools are published in the Tools group instead.
	// BOTH web tokens advertise the SAME composite row, `gopi tool web` — one
	// row for the pair (spelling them `tool web search` / `tool web fetch` would
	// fold each description across a line break, the clutter this index
	// refuses). Its description column names the two TOOL words, which is what
	// the exception list carries, so the row and the naming rule are one
	// reading. A `<search|fetch>` alternation here was the alternative-branch
	// shorthand the cross-group gate refuses; the tool page keeps it.
	advertised := map[string]string{
		toolnames.ToolWebSearch: "tool web",
		toolnames.ToolWebFetch:  "tool web",
	}
	predToToken := map[string]string{}
	for token, pred := range dispatcherOfToken {
		predToToken[pred] = token
	}
	for _, pred := range cliContractDispatchPredicates {
		if pred == "MemorySyncSubcommand" {
			continue // first token is "memory" — covered by MemorySubcommand
		}
		token, ok := predToToken[pred]
		if !ok {
			continue // a group sub-verb without a dispatch token of its own
		}
		word, ok := advertised[token]
		if !ok {
			word = token
		}
		if completionSuppressesVerb(token) {
			// Deliberately unpublished (operator ruling 2026-10-06, task
			// 01a10dba…): `gopi ui` / `gopi cli` still dispatch and still answer
			// their own -h, and this guard's job — "a dispatched verb must not be
			// INVISIBLE by accident" — does not apply to an absence that is a
			// DECISION. The negative direction is not left unguarded: it is
			// pinned, verb by verb, in cli_help_hidden_verbs_test.go
			// (TestTopLevelHelpHidesUnadvertisedVerbs +
			// TestSuppressedVerbsStillDispatch), so re-adding the row is red there
			// rather than silently green here.
			continue
		}
		if !strings.Contains(usage, "gopi "+word) {
			t.Errorf("`gopi %s` is dispatchable (%s in main.go) but missing from the top-level command index — add it to config.commandIndex (memory #1252: an undiscoverable subcommand is a P0)", token, pred)
		}
	}
}

// TestTopLevelHelpDoesNotAdvertiseTheAmbiguousWebToolRows pins the REVERSE of
// the removal above: `gopi web search "<query>"` / `gopi web fetch <url>` must
// not come back to the front door.
//
// The reason is stated on the index itself (internal/config/help.go, the
// "TWO ROWS DELIBERATELY ABSENT" note): the neighbouring row is
// `gopi web [--tools]   Local browser chat UI (SSE)`, so a front-door reader
// cannot tell which `web` a line is about — the operator's words, 2026-10-28:
// 「这个会有歧义，删除顶级help里的」. The tools keep their published home one
// group down (`gopi tool web <search|fetch>`, held by
// TestTopLevelUsageAdvertisesEveryDispatchedVerb's `tool web search` mapping),
// so this absence LOSES nothing discoverable — which is what makes it a
// decision rather than an omission, the same shape as `gopi merger` above.
//
// The check is ANCHORED (a row head + the word), never a bare substring:
// `gopi web [--tools]` must stay, and the tool-group lines legitimately spell
// `web` too.
func TestTopLevelHelpDoesNotAdvertiseTheAmbiguousWebToolRows(t *testing.T) {
	for _, gone := range []string{
		"gopi web search",
		"gopi web fetch",
	} {
		row := regexp.MustCompile(`(?m)^ {2}` + regexp.QuoteMeta(gone) + `(?: |$)`)
		if m := row.FindString(usage); m != "" {
			t.Errorf("top-level gopi -h carries the ambiguous row %q — `web` on this page has TWO meanings (the chat UI carrier and this tool group's door), and the neighbouring `gopi web [--tools]` row is what makes the pair unreadable. The tools are published as `gopi tool web search` / `gopi tool web fetch` in the Tools group; re-adding these rows is RED here rather than looking like harmless tidying.", strings.TrimSpace(m))
		}
	}
	// The decision must not turn into a DISCOVERABILITY loss: the chat UI row
	// itself stays, and the tools stay listed under the group word.
	if !regexp.MustCompile(`(?m)^ {2}gopi web \[--tools\]`).MatchString(usage) {
		t.Errorf("top-level gopi -h no longer advertises `gopi web [--tools]` — removing the two tool rows must not take the chat UI carrier with them")
	}
	if !strings.Contains(usage, "gopi tool web") {
		t.Errorf("top-level gopi -h does not publish the web tools anywhere (`gopi tool web` missing) — the two rows were removed because they are ambiguous, NOT because the tools should be invisible")
	}
}

// TestTopLevelHelpDoesNotRepeatTaskSubcommands pins the operator's ruling of
// 2026-10-28: a `gopi task` SUBCOMMAND gets no top-level row of its own.
//
// The rule the page is built on is that the top-level index names each verb
// ONCE; the `gopi task <subcommand>` row already lists the family (`archive`,
// `show`, `submit`, `sync`, …). A second row spelled `gopi task archive
// <branch>` therefore says the same word twice and, worse, teaches a reader
// that `archive` stands BESIDE `task` rather than under it — the operator's
// words: 「archive 应该在 task 的 subcommand，根本不应该出现在顶级 help 菜单里」.
//
// The check is anchored on the dispatch spelling, so the `gopi task
// <subcommand>` row (which merely MENTIONS `archive` in its description) stays
// green, and the subcommand's own page keeps stating the full flag set with a
// reason (`gopi task archive -h`, cli_archive.go) — an absence here costs no
// discoverability.
func TestTopLevelHelpDoesNotRepeatTaskSubcommands(t *testing.T) {
	// One entry per subcommand that has (or has had) a redundant top-level row.
	// A new one is added HERE together with the row's removal, so the pair can
	// never drift apart.
	for _, gone := range []string{
		"gopi task archive",
	} {
		row := regexp.MustCompile(`(?m)^ {2}` + regexp.QuoteMeta(gone) + `(?: |$)`)
		if m := row.FindString(usage); m != "" {
			t.Errorf("top-level gopi -h carries a redundant subcommand row %q — `%s` is a SUBCOMMAND of `gopi task` and that family's row already lists it; a second row makes it look like a sibling of `task`. Its own page (`%s -h`) is where the flags belong.", strings.TrimSpace(m), gone, gone)
		}
	}
	// The absence must not become a DISCOVERABILITY loss: the family row stays
	// and still names the subcommand.
	if !regexp.MustCompile(`(?m)^ {2}gopi task <subcommand>`).MatchString(usage) {
		t.Errorf("top-level gopi -h no longer advertises `gopi task <subcommand>` — removing the redundant row must not take the family row with it")
	}
	if !strings.Contains(usage, "archive") {
		t.Errorf("top-level gopi -h no longer mentions `archive` anywhere — the subcommand must stay discoverable through the `gopi task` row's own list")
	}
}

// TestTopLevelUsageIsAlignedAndTerse locks the FORMAT half of the epic: the
// rendered help is an aligned index, not a ragged essay. No line may run past
// the wrap budget, and the attachment's careless fragments must stay gone.
// (Column-level alignment is asserted in internal/config, next to the renderer.)
func TestTopLevelUsageIsAlignedAndTerse(t *testing.T) {
	for i, line := range strings.Split(usage, "\n") {
		// Display width, not bytes (config.DisplayWidth): the budget is a
		// terminal's columns, and the renderer wraps to the same export.
		if w := config.DisplayWidth(line); w > config.HelpWrapCol {
			t.Errorf("gopi -h line %d is %d display cols (> %d): %q", i+1, w, config.HelpWrapCol, line)
		}
	}
	// No careless leftovers: the attachment's dangling fragment (a bare "see
	// the full family help" pointer standing in for content) must be gone.
	for _, gone := range []string{
		"run 'gopi task' or", // the attachment's sloppy pointer
		"for the full family help",
	} {
		if strings.Contains(usage, gone) {
			t.Errorf("gopi -h still carries the careless fragment %q", gone)
		}
	}
}

// TestTopLevelUsageDelegatesEnvVarsToHelpEnv locks the 2026-09-13 help-format
// contract: the top-level help is the everyday front door, so the complete
// env-var table lives behind `gopi --help-env` instead of being inlined
// (inlining it made `gopi -h` ~470 lines). The catalog is still fully
// documented — asserted here by rendering BOTH surfaces and requiring each
// EnvVars entry to appear in the --help-env table, and the table's absence from
// the top-level text (so the two can never silently merge back).
func TestTopLevelUsageDelegatesEnvVarsToHelpEnv(t *testing.T) {
	table := config.FormatEnvTable()
	for _, e := range config.EnvVars {
		if !strings.Contains(table, e.Env) {
			t.Errorf("`gopi --help-env` is missing env var %q — the table is the complete surface", e.Env)
		}
		// A table row always carries the settings key and the help prose; the
		// top-level index must NOT (it names only the common knobs).
		if strings.Contains(usage, e.Env) && strings.Contains(usage, e.Help) {
			t.Errorf("top-level gopi -h inlines the env-table row for %q — the complete table belongs to `gopi --help-env`", e.Env)
		}
	}
	if !strings.Contains(usage, "--help-env") {
		t.Errorf("top-level gopi -h must point at `gopi --help-env`:\n%s", usage)
	}
	// The everyday help must stay an index: the env table alone was ~180 lines.
	if got := len(strings.Split(usage, "\n")); got > topLevelUsageMaxLines {
		t.Errorf("top-level gopi -h is %d lines — over the %d-line budget; move detail to `gopi <cmd> -h` / `gopi --help-env`", got, topLevelUsageMaxLines)
	}
}

// topLevelUsageMaxLines caps the everyday help so it cannot drift back into the
// ~470-line dump the help-format epic removed. The cap only fails on real bloat.
//
// RAISED 160 -> 176 on 2026-10-02, with the measurement that forced it: at the
// old cap the text measured EXACTLY 160 lines (the comment's "well under it" had
// stopped being true, and nothing said so until a change tripped it — the
// reverted-out one-row form measured 174). The three former second binaries —
// gopi-replay, ledger-window, text2img-verify — are now subcommands of the ONE
// binary (Rule 35), and documenting them costs ONE index row: 14 lines, so the
// text measures 174. The cap sits 2 lines above the current text, not 14: the
// next row added here trips it again, which is the cap doing its job — the fix
// then is a decision about what the index is FOR, not another raise.
//
// RAISED 176 -> 177 on 2026-10-10 (task 01a11f70…), and this is such a
// decision, taken with the arithmetic rather than around it. The `gopi task`
// row now enumerates taskverbs.IndexRowVerbs — FamilyVerbs ∪ SyncVerbs — so the
// reader who opens this page first can reach `gopi task send` (moving a row to
// another machine was previously spelled only in a negative-titled section at
// the bottom of `gopi task -h`). The row's summary is 292 columns: at this
// page's own width the summary column starts at 30, so a wrapped line carries
// 58 columns and five lines hold 290. The names alone are 262, and 292 > 290 —
// the row cannot be squeezed into the five lines its pre-fix form occupied, and
// there is no shorter honest phrasing of names that must ALL be listed
// (IndexRowVerbs is the enumeration; dropping a name is the defect being fixed).
// So the page goes to 177 and the cap follows it — the raise is one line, paid
// for by the row that a reader actually needs, not a general loosening: the
// record above (160 → 176) shows the direction this cap is supposed to travel.
const topLevelUsageMaxLines = 179

// TestThinkingBudgetHelpMatchesCode — P1-1 drift guard: the catalog Help
// string for GOPI_THINKING_BUDGET must agree with the code default
// (deepseek.DefaultClaudeThinkingBudget). Free-text defaults in the catalog
// are unvalidated; this test is the tripwire (the 2048→1024 halving in
// d3ef449 was never reflected in help).
func TestThinkingBudgetHelpMatchesCode(t *testing.T) {
	want := "default " + strconv.Itoa(deepseek.DefaultClaudeThinkingBudget)
	found := false
	for _, e := range config.EnvVars {
		if e.Env == "GOPI_THINKING_BUDGET" {
			found = true
			if !strings.Contains(e.Help, want) {
				t.Errorf("GOPI_THINKING_BUDGET Help %q does not state the code default %q — update the catalog when the constant changes", e.Help, want)
			}
		}
	}
	if !found {
		t.Fatal("GOPI_THINKING_BUDGET missing from EnvVars catalog")
	}
	// The rendered help must carry it too (catalog → help pipeline intact).
	if !strings.Contains(config.FormatEnvTable(), want) {
		t.Errorf("--help-env does not show the code default %q", want)
	}
}

// TestTopLevelUsageShowsVisionFeature guards the core requirement: the new
// vision_analyze feature (image understanding / OCR) must be discoverable from
// gopi -h — both as a CLI subcommand and as an agent tool.
func TestTopLevelUsageShowsVisionFeature(t *testing.T) {
	for _, want := range []string{
		"gopi tool vision_analyze <image>", // subcommand advertised in the index
		"--vision-model NAME",              // vision knobs already listed
		"GOPI_VISION_MODEL",
		"Image understanding", // what the feature does
		"OCR",                 // the OCR use case
		"vision model",        // where the work happens
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level gopi -h is missing %q", want)
		}
	}
	// The detail (formats, size cap) lives in the command's own help.
	for _, want := range []string{
		"gopi tool vision_analyze <image_path>",
		"jpg, jpeg, png, gif, webp",
		"32 MiB",
	} {
		if !strings.Contains(visionUsage, want) {
			t.Errorf("gopi tool vision_analyze -h is missing %q", want)
		}
	}
}

// flat folds every run of whitespace (spaces, newlines, the indent of a
// wrapped line) into a single space, so a PROSE assertion matches the rendered
// help regardless of where the renderer wrapped the sentence. Structural
// assertions keep using strings.Contains on the raw text.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestTopLevelUsageShowsNativeImageInput guards the Epic's core deliverable:
// passing an image FILE as the positional argument must be documented as
// NATIVE vision input (auto-switch to the vision model, pixels processed
// directly — no OCR round-trip), and --prompt must be advertised as the way
// to give the instruction alongside file/image positionals.
// The prose checks run on the whitespace-folded text: these blocks are
// rendered through the column/wrap renderer, so where a sentence breaks is a
// layout property, not a contract.
func TestTopLevelUsageShowsNativeImageInput(t *testing.T) {
	folded := flat(usage)
	for _, want := range []string{
		"--prompt TEXT",     // task instruction flag
		"image.png",         // the image-file positional, named in Inputs
		"auto-switches",     // auto model switch
		"no OCR round-trip", // NOT an OCR round-trip
		"GOPI_VISION_MODEL", // the vision model env var
		"under $GOPI_HOME",  // --home semantics still documented
	} {
		if !strings.Contains(folded, want) {
			t.Errorf("top-level gopi -h is missing %q", want)
		}
	}
}

// TestLogUsageShowsVisionFilter guards the log-side requirement: gopi session log must
// expose a way to filter image processing (vision_analyze) calls.
func TestLogUsageShowsVisionFilter(t *testing.T) {
	for _, want := range []string{
		"--vision",       // the family shortcut flag
		"vision_analyze", // the underlying tool name
		"image processing",
	} {
		if !strings.Contains(logUsage, want) {
			t.Errorf("gopi session log -h is missing %q", want)
		}
	}
}

// TestTopLevelUsageShowsText2img guards the Epic's core requirement: the
// text2img feature (text→PNG rendering) must be discoverable from gopi -h; the
// full parameter surface and validated defaults live in its own -h.
func TestTopLevelUsageShowsText2img(t *testing.T) {
	for _, want := range []string{
		"gopi tool text2img <input.txt>", // subcommand advertised in the index
		"CJK",                            // Chinese supported
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level gopi -h is missing %q", want)
		}
	}
	// The detail stays one hop away, in `gopi tool text2img -h`.
	for _, want := range []string{
		"--size", "--cols", "--lh", "--lines", "--margin", "--font", // flag surface
		"Default 20",                // validated default font size
		"auto-paginates",            // pagination documented
		"zero-dependency", "no CGO", // no external deps
		"gopi tool text2img notes.txt", // usage example
	} {
		if !strings.Contains(text2imgUsage, want) {
			t.Errorf("gopi tool text2img -h is missing %q", want)
		}
	}
}

// TestText2imgUsageShowsDefaults guards the subcommand's own -h: the validated
// claude-version defaults must be documented so users can tune from them.
func TestText2imgUsageShowsDefaults(t *testing.T) {
	for _, want := range []string{
		"gopi tool text2img <input.txt> [output.png]",
		"--size", "--cols", "--lh", "--lines", "--margin", "--font",
		"20px", "50", "32", "90", "12",
		"output-1.png",
	} {
		if !strings.Contains(text2imgUsage, want) {
			t.Errorf("text2imgUsage is missing %q", want)
		}
	}
}

// TestLogUsageShowsText2imgFilter guards the log-side requirement: gopi session log
// must expose a way to filter text2img (text→PNG rendering) calls.
func TestLogUsageShowsText2imgFilter(t *testing.T) {
	for _, want := range []string{
		"--text2img", // the family shortcut flag
		"text2img",   // the underlying tool name
		"text→PNG rendering",
	} {
		if !strings.Contains(logUsage, want) {
			t.Errorf("gopi session log -h is missing %q", want)
		}
	}
}

// TestTopLevelUsageShowsTaskLedger guards the task-as-memory ledger's
// discoverability: gopi -h must advertise the task family, its resident
// process forms, and its append-only semantics (records permanent, no delete —
// nick's iron rule). The per-verb detail lives in `gopi task -h`.
func TestTopLevelUsageShowsTaskLedger(t *testing.T) {
	// The command index REFLOWS each row at a column width, so a multi-word
	// phrase can be split across a line break ("… — append-only, never\n
	// deleted.") purely as a function of how long the ROW is. Asserting such a
	// phrase against the raw rendering therefore tests the column arithmetic,
	// not the semantics: adding one verb to the family enumeration moved the
	// break and failed this test while the semantics were intact. Whitepace is
	// collapsed before the phrase checks below (single-word markers are
	// unaffected).
	flat := strings.Join(strings.Fields(usage), " ")
	for _, want := range []string{
		"gopi task <subcommand>",     // the family entry, with its verb enumeration
		"submit", "verify", "status", // representative LEDGER verbs in the enumeration
		"append-only, never deleted", // append-only semantics stated
		// The task row's verb enumeration is DERIVED from
		// taskverbs.FamilyVerbs (internal/taskverbs), so the row no longer
		// spells the verbs: TestTopLevelUsageTaskVerbEnumMatchesCompletion
		// checks the enumeration itself against the completion set.
		//
		// The git-infrastructure atoms (adopt/merge/…) and the services are
		// NOT in the enumeration any more (CLI-surface restructure
		// 2026-09-15): the ledger row lists ledger verbs, and each non-ledger
		// group has its own index row below ('gopi worker') or its own -h
		// ('gopi task sync -h'). `adopt` is therefore deliberately absent here
		// — asserted by TestNoServiceOrSyncVerbInLedgerGroup.
		//
		// `gopi merger` is ABSENT BY DECISION, and this row is where a reader
		// would have found it (task 01a113a8…, 2026-10-06): the verb is RETIRED
		// and refuses even -h, so it is no longer a top-level index row. What
		// the page must still do is POINT at the retirement, which it does on
		// the task family's own page — asserted just below, because dropping
		// the row without keeping the explanation is the Rule 15 defect the
		// removal could otherwise introduce.
		"gopi worker", // the only task-executing process form
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("top-level gopi -h is missing %q", want)
		}
	}
	// The retirement must stay DISCOVERABLE after the row is gone. `gopi merger`
	// no longer has an index row here (it cannot honour the page's "-h works for
	// everything listed" promise), so the surviving statement of the decision
	// lives on `gopi task -h`, which is reachable and runnable. Asserted on the
	// RENDERED page, so a silent deletion of the prose is red (Rule 15: a
	// retirement that is not documented is its own bug report).
	//
	// `gopi merge -h` USED TO CARRY IT TOO and can no longer: that verb left the
	// command surface (epic merge-removal, item 2), so the word now reaches the
	// retirement refusal in cli_verb_aliases.go — which is a runnable page in the
	// sense that matters here (an operator typing it learns the truth), just not
	// a help page this table can hold to a prose contract.
	if !strings.Contains(tasksUsage, "RETIRED") {
		t.Errorf("`gopi task -h` no longer states that the resident merger daemon is RETIRED — the top-level index row was removed, so this page is where the decision is explained now")
	}
	if !strings.Contains(tasksUsage, CliVerbMerger) {
		t.Errorf("`gopi task -h` no longer names %q at all — the retirement note must name the word an operator may still type, or the explanation answers a question nobody asked", CliVerbMerger)
	}
	// The enumerations must cover the whole family surface: every task
	// sub-verb offered by tab completion appears in the task index entry.
	taskEntry := taskIndexEntry(t)
	for _, v := range taskCompletionNames {
		if !strings.Contains(taskEntry, v) {
			t.Errorf("the `gopi task` index entry does not enumerate %q — the family surface must be listed", v)
		}
	}
	// The deprecated alias must not be re-advertised at the top level, and the
	// service/git-infrastructure spellings must not return to the ledger row's
	// enumeration (they have their own homes: `gopi merge -h` / `gopi task
	// sync -h`; `gopi merger -h` was one of them until the resident daemon was
	// retired — 2026-09-19, devdoc/merger-retired.md).
	if strings.Contains(usage, "gopi task serve [") {
		t.Errorf("top-level gopi -h advertises the deprecated 'gopi task serve [' verb — task merger is canonical")
	}
	// THE ROW'S ENUMERATION WAS WIDENED TO taskverbs.IndexRowVerbs BY
	// task 01a11f70… (2026-10-10): the SYNC verbs are now ON the row, because
	// moving a row to another machine is a thing the family does and the row's
	// head claims the whole family (the defect: `send` was unreachable from the
	// first page a reader opens — see TestTopLevelUsageTaskRowCarriesTheWholeFamily).
	// What the NEGATIVE half of this check still governs is the other two
	// groups, which gained nothing: a service spelling or a git-infrastructure
	// atom on the row would still be the double-home the 2026-09-15 restructure
	// removed, and neither can reach the row even by accident, because
	// IndexRowVerbs is FamilyVerbs ∪ SyncVerbs and nothing else.
	for _, v := range append(append([]string{}, taskverbs.MergerVerbs...), taskverbs.ServiceAliases...) {
		if strings.Contains(taskEntry, v) {
			t.Errorf("the `gopi task` index row enumerates %q — it is advertised outside the ledger group (git infrastructure: `gopi merge -h`; the services: `gopi worker -h`), and IndexRowVerbs is FamilyVerbs ∪ SyncVerbs only", v)
		}
	}
	// And the split itself must hold: the row may name a sync verb, the
	// completion surface may not (asserted in full by
	// TestTopLevelUsageTaskVerbEnumMatchesCompletion; this is the cheap half
	// next to the row read).
	for _, v := range taskverbs.SyncVerbs {
		if !strings.Contains(taskEntry, v) {
			t.Errorf("the `gopi task` index row does not enumerate the cross-node verb %q — IndexRowVerbs is the row's enumeration", v)
		}
		if containsString(taskCompletionNames, v) {
			t.Errorf("`gopi task <TAB>` offers the cross-node verb %q — the row names it, tab must not: tab IS the ledger group", v)
		}
	}
	// The merger index entry is GONE (task 01a113a8…, 2026-10-06): the verb is
	// RETIRED and refuses `-h`, so an index row could only advertise a command
	// that answers help with a refusal. The checks that used to read that row
	// are re-pointed at the pages where the retirement IS stated (`gopi task -h`
	// and `gopi merge -h`), so the fact they guard — the retirement is
	// documented, and no retired run form is offered — survives the removal at
	// the surface a reader can actually reach.
	//
	// The negative half stays on the top-level page, because THAT is where the
	// old defect would reappear: if the row comes back, it must not come back
	// carrying `--daemon`/`--every`/`--once`/`--status`. An operator would read a
	// supported flag on the index row and meet a refusal on the command (Rule 15:
	// a retirement that is not documented is its own bug report).
	if strings.Contains(indexEntryOrEmpty(), "  gopi "+CliVerbMerger+" ") {
		retiredRow := indexEntryOrEmpty()
		if strings.Contains(retiredRow, daemonFlagLong) {
			t.Errorf("the `gopi %s` index row is back and still offers %s — the merger refuses that flag (it detached into a hidden service); the index must not advertise a resident daemon:\n%s",
				CliVerbMerger, daemonFlagLong, retiredRow)
		}
		// The flag spellings are literals here for the same reason
		// merger_retired_face_test.go's are: what an operator might READ on the
		// index row is the subject, and the daemon's own constants went with the
		// daemon (epic merger-delete-dead-impl, 2026-09-19). `--every` is the
		// spelling test, not an internal name to keep in sync.
		for _, gone := range []string{"--every", "--once", "--status"} {
			if strings.Contains(retiredRow, gone) {
				t.Errorf("the `gopi %s` index row is back and offers %s — the resident merger is retired and the flag is refused by name:\n%s",
					CliVerbMerger, gone, retiredRow)
			}
		}
	}
	// And the pages that DO explain it must keep explaining it, in the operator's
	// words: the word, and the retirement. Without this, deleting the note in the
	// same change that removes the row leaves the decision unrecorded on any
	// runnable page.
	for _, page := range []struct {
		name string
		text string
	}{
		{"gopi task -h", tasksUsage},
	} {
		if !strings.Contains(page.text, "RETIRED") || !strings.Contains(page.text, CliVerbMerger) {
			t.Errorf("%s no longer carries the retirement note (needs both %q and %q) — the top-level index row was removed, so these pages are where an operator learns the verb is dead and what replaced it",
				page.name, "RETIRED", CliVerbMerger)
		}
	}
}

// taskIndexEntry returns the command-index entry for `gopi task <subcommand>`.
func taskIndexEntry(t *testing.T) string {
	t.Helper()
	return indexEntry(t, "  gopi task <subcommand>")
}

// indexEntryOrEmpty returns the command-index row for `gopi merger`, or "" when
// the row is absent — which is the CORRECT state since task 01a113a8… (the verb
// is retired and refuses -h, so it is published nowhere). indexEntry itself
// t.Fatalf's on a missing row, and that is right for a row the page MUST carry;
// this reader is for the one row whose absence is the decision, so the checks
// that guard it can run in both states instead of asserting the row exists.
func indexEntryOrEmpty() string {
	prefix := "  gopi " + CliVerbMerger + " "
	i := strings.Index(usage, prefix)
	if i < 0 {
		return ""
	}
	rest := usage[i:]
	if end := strings.Index(rest[1:], "\n  gopi "); end > 0 {
		return rest[:end+1]
	}
	return rest
}

// indexEntry returns the command-index row starting with prefix, including its
// wrapped continuation lines (up to the next "  gopi " row).
func indexEntry(t *testing.T, prefix string) string {
	t.Helper()
	i := strings.Index(usage, prefix)
	if i < 0 {
		t.Fatalf("top-level gopi -h has no %q index entry", strings.TrimSpace(prefix))
	}
	rest := usage[i:]
	if end := strings.Index(rest[1:], "\n  gopi "); end > 0 {
		return rest[:end+1]
	}
	return rest
}

// TestTopLevelUsageTaskVerbEnumMatchesCompletion: gopi -h and `gopi task <TAB>`
// must advertise the SAME surface (tab follows the help) — the index entry is
// the top-level half of that invariant (cli_completion_test.go pins the family
// help half).
//
// THE INVARIANT WAS RESTATED BY task 01a11f70… (2026-10-10), and this is the
// correction rather than a relaxation: the two surfaces answer DIFFERENT
// questions and were only ever equal by accident of the row enumerating
// FamilyVerbs.
//
//   - `gopi -h`'s task row is an INDEX ROW: it answers "what does the task
//     family do, and can I reach the capability from here?" — so it enumerates
//     taskverbs.IndexRowVerbs (FamilyVerbs ∪ SyncVerbs), because moving a row
//     to another node is something the family does and the row's own head
//     (`gopi task <subcommand>`) claims the whole family.
//   - `gopi task <TAB>` answers "what can I type next" — and a sync verb's
//     documented home is `gopi task sync -h`, so tab must NOT offer it
//     (taskCompletionNames IS taskverbs.FamilyVerbs; the group split is
//     untouched).
//
// So the assertion is now the CONTAINMENT that is actually true and that the
// defect violated in the other direction: the index row must carry EVERY name
// on BOTH surfaces (it is the union), the completion surface must still be
// exactly the ledger group, and neither may carry a spelling the other's group
// has no home for. The old equality would have said "the row is wrong" about
// the fix — the row gaining a name is the point. See
// TestTopLevelUsageTaskRowCarriesTheWholeFamily for the red→green proof.
func TestTopLevelUsageTaskVerbEnumMatchesCompletion(t *testing.T) {
	head := "gopi task <subcommand>"
	if !strings.Contains(usage, head) {
		t.Fatalf("top-level gopi -h is missing the %q enumeration head", head)
	}
	enumRegion := taskIndexEntry(t)
	// ① The completion surface is still a subset of the row (tab follows help).
	for _, v := range taskCompletionNames {
		if !strings.Contains(enumRegion, v) {
			t.Errorf("top-level gopi -h task enumeration (after %q) is missing %q — gopi task <TAB> offers it, help must too", head, v)
		}
	}
	// ② The completion surface is EXACTLY the ledger group — the fix must not
	// have widened it (a sync verb in tab completion would advertise a group
	// whose home is `gopi task sync -h`).
	for _, v := range taskverbs.SyncVerbs {
		if containsString(taskCompletionNames, v) {
			t.Errorf("taskCompletionNames offers the sync verb %q — the completion surface IS taskverbs.FamilyVerbs, whose "+
				"non-membership is the 2026-09-15 restructure; the index row may name it, tab may not", v)
		}
	}
	// ③ And the row carries the whole family — the ledger verbs AND the
	// cross-node legs (the fix's own assertion; the row is the union).
	for _, v := range taskverbs.IndexRowVerbs {
		if !strings.Contains(enumRegion, v) {
			t.Errorf("the `gopi task` index row does not enumerate %q — IndexRowVerbs is the row's enumeration "+
				"(FamilyVerbs ∪ SyncVerbs), and a member it omits is a capability unreachable from the first page "+
				"a reader opens (task 01a11f70…: `send` was exactly that)", v)
		}
	}
	// The deprecated serve alias must never appear in the enumeration.
	if strings.Contains(enumRegion, "serve") {
		t.Errorf("top-level gopi -h task enumeration still lists 'serve' — the deprecated alias must not be tab/help surface (merger is canonical)")
	}
}

// TestTopLevelUsageTaskRowCarriesTheWholeFamily is the RED→GREEN proof for the
// `send` half of task 01a11f70…, and it is deliberately written so it measures
// the shipped page rather than a paraphrase:
//
//   - the DEFECT is reproduced from the row's pre-fix SUMMARY SOURCE — the exact
//     expression help.go carried (`strings.Join(taskverbs.FamilyVerbs, ", ")`
//     with no cross-node clause) — and the predicate must flag it, naming
//     `send` and `peer`;
//   - the SHIPPED row must pass the same predicate, so a green run cannot be a
//     blind one and the predicate cannot drift from the live assertion.
//
// It also pins the two words the acceptance names: `send` on the row, and the
// reader's intent (`another machine` / `peer`) reachable from the row's summary.
// A future edit that trims the clause to buy page lines turns this red rather
// than quietly restoring the unreachable capability.
func TestTopLevelUsageTaskRowCarriesTheWholeFamily(t *testing.T) {
	// The RENDERED index entry (the row plus its wrapped continuation lines —
	// taskIndexEntry already reads exactly that region). Names are wrapped as
	// whole words, so a flat substring test on it is the user-visible truth.
	shipped := taskIndexEntry(t)

	// WHAT THIS GATE WAS, and why it now measures the same acceptance from the
	// other side. Until 2026-11-05 it required the row to enumerate FamilyVerbs
	// ∪ SyncVerbs — `send` in particular — because a row-moving spelling that no
	// page names is unreachable from the first page a reader opens (task
	// 01a11f70). The surface is deleted: the row must now offer LIVE verbs only,
	// so "carries `send`" would be an assertion that the page lies. The defect
	// the gate exists for has not gone away — it has INVERTED, and the
	// acceptance the operator stated ("a reader who does not already know the
	// verb must be able to reach the capability") is what stays.
	//
	// ① The row enumerates exactly the LIVE family — no retired spelling, and no
	// omission of a live one.
	live := taskverbs.IndexRowVerbs
	if len(live) == 0 {
		t.Fatal("taskverbs.IndexRowVerbs is empty — the row's enumeration would be unverifiable")
	}
	for _, v := range live {
		if !strings.Contains(shipped, v) {
			t.Errorf("the `gopi -h` task row omits the live verb %q — the row enumerates taskverbs.IndexRowVerbs "+
				"(FamilyVerbs ∪ SyncVerbs), and IndexRowVerbs is the ONE place that decides what a reader is offered", v)
		}
	}
	for _, v := range syncVerbRemovalSpellings {
		if containsString(live, v) {
			t.Errorf("taskverbs.IndexRowVerbs still carries the deleted spelling %q — the front-door row would offer a "+
				"command that can only refuse", v)
		}
	}

	// ② THE RED PROOF, on the predicate above: a row that still offered a
	// deleted spelling must be flagged by the same loop. The pre-2026-11-05 row
	// is the probe (its enumeration came from a union that still contained the
	// eight), so the check below is measured against text that really shipped.
	//
	// The predicate "every live verb is present" cannot fail on this probe (the
	// probe is a superset), which is exactly why the retired half below exists:
	// a gate with only a positive direction stays green while the shape comes
	// back.
	preDeletion := "The task-as-memory ledger: " + strings.Join(append(append([]string{}, taskverbs.FamilyVerbs...),
		syncVerbRemovalSpellings...), ", ") + " — append-only, never deleted; `send` moves a row to a `peer`."
	flagged := 0
	for _, v := range syncVerbRemovalSpellings {
		if strings.Contains(preDeletion, v) {
			flagged++
		}
	}
	if flagged != len(syncVerbRemovalSpellings) {
		t.Fatalf("the pre-deletion probe carries %d of %d deleted spellings — the predicate measures presence, so a "+
			"probe that does not carry them all cannot prove it would flag one", flagged, len(syncVerbRemovalSpellings))
	}
	// And the SHIPPED row must be clean by the same rule.
	for _, v := range syncVerbRemovalSpellings {
		if containsString(strings.Fields(shipped), v) {
			t.Errorf("the `gopi -h` task row still offers the deleted spelling %q as a live member", v)
		}
	}

	// ③ The reader's INTENT words must still be reachable from the page a
	// reader opens first. The old acceptance was "`send` a row to a `peer`";
	// the capability is gone but the reader's question is not, so the page must
	// carry the word that answers it and point at where the work goes.
	flat := strings.ToLower(strings.Join(strings.Fields(shipped), " "))
	if !strings.Contains(flat, "task") {
		t.Errorf("the `gopi -h` task row does not speak the ledger's own subject — got %q", flat)
	}
	for _, want := range []string{"submit"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the `gopi -h` task row does not carry %q — filing work (a row, including one for another host) "+
				"is the surviving route and the row must name it", want)
		}
	}
}

// TestTopLevelUsageShowsSettingsKeysAndFlags: the help must map each env var
// back to a settings.json key and a CLI flag, so users have three equivalent
// ways to configure gopi (flag > env > settings > default).
func TestTopLevelUsageShowsSettingsKeysAndFlags(t *testing.T) {
	for _, want := range []string{
		"settings.json",                        // the config file is documented
		"Precedence:",                          // the order is stated
		"environment variable > settings.json", // env sits between flags and settings
		"--timeout", "--compact", "--db",
		"--help-env", // deep-dive available
		"Examples:",  // usage examples present
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level gopi -h is missing %q", want)
		}
	}
}

// TestTopLevelUsageShowsVoiceInput guards the Epic's core requirement: the
// voice-input feature (speech-to-text) must be discoverable from gopi -h. The
// whole feature used to be invisible: the `gopi tool voice_input` dispatcher
// existed (cmd/agent/cli_voice.go, wired into main.go's chain) but help.go never
// mentioned it, so nobody could find or test it.
//
// The ENGINE half of this assertion changed on 2026-10-07: the terminal default
// flipped from Fun-ASR-Nano to the doubao cloud engine, so the index row now
// advertises the cloud engine as the default and names `--engine local` as the
// way to keep the audio on the machine (devdoc/doubao-asr-integration.md §3).
func TestTopLevelUsageShowsVoiceInput(t *testing.T) {
	// The top-level page is an INDEX (message-length discipline: 468 → ~130
	// lines), so it carries the verb, what it does and how to reach the two
	// server shapes; the engine contract and the usage shapes live in the
	// verb's own -h. Both halves are asserted — the feature stays discoverable
	// from `gopi -h`, and `gopi tool voice_input -h` still owns the detail.
	for _, want := range []string{
		"gopi tool voice_input <audio>", // the verb, in the command index
		"doubao",                        // the DEFAULT engine is advertised as such
		"DEFAULT",                       // ...and named as the default, not as an option
		"Fun-ASR-Nano",                  // the on-machine engine is still advertised
		// The command index wraps to the page width, so a phrase that spans a
		// line break can only be asserted in pieces — the FLAGS are the stable
		// half (Rule 1b: the spelling the parser reads, not the wrapped prose).
		"--engine local",          // ...and how to reach it, spelled as the flag
		"--server", "serves HTTP", // the server shape is advertised
		"--client", "records a mic",
		"voice_input -h", // pointer to the verb's own help
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level gopi -h is missing %q", want)
		}
	}
	for _, want := range []string{
		"gopi tool voice_input <audio-file>",            // one-shot usage shape
		"--chunk N",                                     // the mandatory-for-long-audio flag
		"gopi tool voice_input --server --listen :7788", // server shape A
		"gopi tool voice_input --server --stdio",        // server shape B
		"gopi tool voice_input talk.wav",                // usage example
		"speech-to-text",                                // what the feature does
		"Fun-ASR-Nano",                                  // the default engine (the design record)
		"--engine NAME",                                 // the engine selector flag
		"--engine recording",                            // the cloud engine, spelled
		"~/.doubao/apikey.txt",                          // where its key comes from
		"volc.seedasr.auc",                              // the resource the cloud engine sends
		"HTTP 403",                                      // the streaming probe's honest outcome, stated
		"POST /transcribe",                              // the HTTP contract
		"no agent tool",                                 // CLI-verb-only, stated
		// Capture client (client half of the same verb).
		"--client",                 // the flag that selects it
		"--list-devices",           // device enumeration
		"--device ID",              // device selection
		"--seconds N",              // recording length
		"--server-url",             // where to post
		"pulse:",                   // the stable-id namespace is documented
		"16kHz mono signed-16-bit", // the pinned capture contract
		"arecord",                  // the zero-dependency route, named
		// Device SELECTION POLICY (devdoc/voice-input-device-policy.md). The
		// ordering is contractual: a user must be able to read it here rather
		// than infer it from the code, and the tier names are quoted verbatim
		// because the run prints them.
		"--device ID",            // tier 1, the only override
		"configured default",     // tier 2 (+ the knob the reader honours)
		"GOPI_VOICE_DEVICE",      // ...named, so the knob is findable
		"system default capture", // tier 3
		"first enumerated",       // tier 4
		"CARD INDEX",             // the identity rule: index spellings refused
		"--list-devices",         // the recovery path named in the error
	} {
		if !strings.Contains(voiceUsageText(), want) {
			t.Errorf("gopi tool voice_input -h is missing %q", want)
		}
	}
}

// TestTopLevelHelpCoversEveryDispatchedSubcommand generalizes the per-feature
// discoverability guards above: EVERY predicate wired into main.go's dispatch
// chain must have a help entry in gopi -h. Those guards are written one
// feature at a time, so a subcommand added without its own guard ships
// invisible (the voice_input case: dispatched since bd3d34a5, absent from help
// for as long as it existed). This closes the CLASS, not the instance.
//
// MAPPING: each predicate name (XxxSubcommand, minus the "is" prefix) maps to
// the verb its help entry must carry, rendered as "gopi <verb>" — the same
// shape help.go uses, so a bare word in prose (or a deprecated-alias mention)
// never satisfies the guard. A predicate with no mapping FAILS: adding a
// dispatcher forces a deliberate decision about how it is documented.
func TestTopLevelHelpCoversEveryDispatchedSubcommand(t *testing.T) {
	// Predicate → the "gopi <verb>" word(s) its help entry must contain.
	helpWordOfPredicate := map[string][]string{
		"CompletionSubcommand": {CliVerbCompletion},
		"ConfigSubcommand":     {CliVerbConfig + " " + configShowVerb},
		"NotifySubcommand":     {CliVerbNotify},
		"VersionSubcommand":    {CliVerbVersion},
		"WorkerSubcommand":     {CliVerbWorker},
		"MergerSubcommand":     {CliVerbMerger},
		// "MergeSubcommand" is GONE with the dispatch (epic merge-removal,
		// item 2): the map is keyed by predicate NAME, so a row whose predicate
		// no longer exists in main.go's chain would assert help coverage for a
		// dispatcher this binary does not have.
		"ArchiveSubcommand": {CliVerbArchive},
		"MemorySubcommand":  {CliVerbMemory},
		// "MemorySyncSubcommand" left with its dispatch (cross-node sync
		// removal, 2026-11-05). This map is keyed by predicate NAME, so a row
		// whose predicate no longer exists in main.go's chain would assert help
		// coverage for a dispatcher this binary does not have.
		"MemoryBackupSubcommand": {CliVerbMemory + " " + tools.LedgerBackupVerb},
		// The two WEB tool spellings are carried by ONE index row,
		// `gopi tool web`, whose description names both tool words — the same
		// shape ToolSubcommand uses. They cannot map to `gopi web search` /
		// `gopi web fetch`: those two rows were DELIBERATELY withdrawn
		// (internal/config/help.go: "TWO ROWS DELIBERATELY ABSENT", operator
		// ruling 2026-10-28) because beside `gopi web [--tools] Local browser
		// chat UI` a reader cannot tell which `web` they are looking at, and
		// mapping them here asserted a row the page does not print — the gate
		// was red on every rev since that ruling (measured at 116de34).
		"WebSearchSubcommand":     {CliVerbTool + " " + CliVerbWeb},
		"WebFetchSubcommand":      {CliVerbTool + " " + CliVerbWeb},
		"VisionSubcommand":        {toolnames.ToolVisionAnalyze},
		"Text2imgSubcommand":      {toolnames.ToolText2img},
		"CodeSearchSubcommand":    {CliVerbCodeSearch},
		"KeywordSearchSubcommand": {toolnames.ToolKeywordSearch},
		"LogSubcommand":           {CliVerbLog},
		"StatsSubcommand":         {CliVerbStats},
		"RawSubcommand":           {CliVerbRaw},
		"ResumeSubcommand":        {CliVerbResume},
		"CheckpointSubcommand":    {CliVerbCheckpoint},
		"ProxySubcommand":         {CliVerbProxy},
		"TasksSubcommand":         {CliVerbTask},
		"ChatSubcommand":          {CliVerbChat},
		"WebSubcommand":           {CliVerbWeb + " ["},
		"TermSubcommand":          {CliVerbCLI + " ["},
		"UISubcommand":            {CliVerbUI + " ["},
		"UIStateSubcommand":       {CliVerbUIState},
		"BrewSubcommand":          {CliVerbBrew},
		// "MemsyncSubcommand" left with its dispatch (cross-node sync removal,
		// 2026-11-05): the flat word is a RETIRED ALIAS that refuses by name and
		// points at `gopi task dedup-scan`; it has no dispatch predicate and no
		// help row of its own.
		"ShotSubcommand":           {CliVerbShot},
		"ScreenCaptureSubcommand":  {toolnames.ToolScreenCapture},
		"CaptureSubcommand":        {CliVerbCapture},
		"ActSubcommand":            {CliVerbAct},
		"AnnotateSubcommand":       {CliVerbAnnotate},
		"GifSubcommand":            {CliVerbGif},
		"ScratchGCSubcommand":      {CliVerbScratchGC},
		"SetupSubcommand":          {CliVerbSetup},
		"SandboxSubcommand":        {CliVerbSandbox},
		"LedgerWindowSubcommand":   {CliVerbLedgerWindow},
		"Text2imgVerifySubcommand": {CliVerbText2imgVerify},
		"VoiceSubcommand":          {toolnames.ToolVoiceInput},
		// The session family verb: one index line (`gopi session <subcommand>`)
		// advertises all six spellings, and the family's own page carries the
		// rest — the same shape `gopi task` uses (which is why task maps here
		// too rather than listing its sub-verbs).
		"SessionSubcommand": {CliVerbSession},
		// The screen GROUP (cli_screen.go): one index line advertises the whole
		// group, and `gopi screen -h` documents every word inside it — the same
		// shape session and tool use above.
		"ScreenSubcommand": {CliVerbScreen},
		// The TOP-LEVEL address of the recorded-screen reader
		// (cli_screen_replay_verb.go): its own index row, so it maps here rather
		// than to the group's.
		"ScreenReplaySubcommand": {CliVerbScreenReplay},
		// The tool GROUP verb: one index line advertises the whole group, and
		// every word inside it is documented at `gopi tool -h` (cli_tool.go).
		"ToolSubcommand": {CliVerbTool},
	}
	for _, pred := range dispatchPredicatesInMain(t) {
		if pred == undispatchedToolWordsSubcommand {
			// The refusal has no help page to be advertised from: `gopi
			// code_search` prints a pointed refusal naming the canonical verb
			// and exits 1, and `gopi -h` documents the COMMANDS, never the
			// spellings it refuses (devdoc/cli-naming-convention.md rule 3a).
			// Its contract is cli_undispatched_tool_words_test.go.
			continue
		}
		wants, ok := helpWordOfPredicate[pred]
		if !ok {
			t.Errorf("dispatch predicate %s has no help mapping — gopi -h does not document this subcommand; add its help entry to internal/config/help.go AND map it here", pred)
			continue
		}
		for _, want := range wants {
			// A verb the operator has withheld from the front door (see
			// completionSuppressedVerbs) has no index row to look for, by
			// decision — the negative direction is pinned in
			// cli_help_hidden_verbs_test.go instead. The check reads the
			// mapping's own first word, so the suppression list stays the ONE
			// place that decides what is published.
			if completionSuppressesVerb(strings.Fields(want)[0]) {
				continue
			}
			if !strings.Contains(usage, "gopi "+want) {
				t.Errorf("dispatch predicate %s is wired in main.go but gopi -h has no %q entry — the subcommand is undiscoverable", pred, "gopi "+want)
			}
		}
	}
	// Reverse direction: gopi <TAB> must never offer a verb gopi -h does not
	// explain (completion follows the help — the CLI-completion epic's
	// invariant, applied to the top-level verb namespace). The list is
	// topLevelCompletionNames(), i.e. what the renderers actually OFFER: a
	// suppressed verb is withheld from both surfaces together, so the invariant
	// is checked over the published set rather than over the dispatch
	// vocabulary (which would fail on a verb we deliberately do not publish).
	for _, verb := range topLevelCompletionNames() {
		if !strings.Contains(usage, "gopi "+verb) {
			t.Errorf("topLevelCommandNames offers %q but gopi -h never mentions %q — completion would suggest a verb the help does not explain", verb, "gopi "+verb)
		}
	}
}

// --- the removed direction: a sync verb must NOT be advertised, but the page
// --- must still answer the reader who greps for it ---
//
// WHAT THIS BLOCK USED TO BE, and why it is INVERTED rather than deleted. From
// 2026-09-15 to 2026-11-05 the cross-node sync family lived on this page as an
// index section ("Moving a row to or from another machine") carrying each
// verb's spelling and syntax, with the flag semantics on `gopi task sync -h`.
// Four assertions enforced that shape: the section heading is locatable, every
// verb in taskverbs.SyncVerbs is spelled `gopi task <verb>` on the page, its
// synopsis placeholder is present, and no sync FLAG leaks onto the page.
//
// THE SURFACE IS DELETED (2026-11-05): taskverbs.SyncVerbs is empty and its
// eight members are in RemovedVerbs. All four assertions are therefore inverted
// BY THE SAME EVENT, and the inversion is the interesting half — deleting the
// block would remove the only measurement of the page, in a change whose whole
// risk is that the page keeps advertising a protocol that no longer exists (a
// reader types `gopi task send`, gets a removal sentence, and is right to call
// the help a liar).
//
// The requirement becomes: the page must NOT offer the eight as runnable, it
// MUST still answer a reader who greps for them, and it MUST name where the
// capability went. The red proof is the SHIPPED pre-deletion page, which
// offers all eight — so a green below has been shown capable of red on the
// exact text this change removed (Rule 10.4).

// familyRemovalSectionHead opens the family page's RETIRED section — the block
// a reader lands on when they grep for a row-moving verb. It is a NAMED
// constant (Rule 1b) because the gate LOCATES the section through it, so a
// reworded heading fails loudly instead of letting an unfindable section be
// measured as a clean empty region.
const familyRemovalSectionHead = "RETIRED: MOVING A ROW TO ANOTHER MACHINE"

// familyRemovalIntentWords are the words a reader who does NOT know the verb's
// name greps with — the same intent words the pre-deletion heading carried, so
// the deletion cannot quietly drop the section's discoverability. "another
// machine" was the acceptance string of task 01a11f70 (2026-10-10); "peer" is
// the term this tool's settings key (sync_peers) and its refusals use, so a
// reader who meets it there must find it here.
var familyRemovalIntentWords = []string{"another machine", "peer"}

// familyRemovalReplacements are the addresses the removal note must name: the
// VERB that files work on another host, and the LEDGER verb that absorbed the
// one non-peer capability of the deleted family. Saying "removed" without them
// leaves the reader holding a capability with nowhere to go.
var familyRemovalReplacements = []string{
	"gopi task submit",
	taskDedupScanProg,
}

// advertisedSyncSynopses returns the deleted family's SURFACE SHAPES that a page
// body still offers. It is the ONE predicate shared by the gate and its red
// proof, so the two cannot drift.
//
// IT MATCHES THE SPELLING *WITH* AN ARGUMENT, not the bare name: the removal
// note deliberately names all eight ("send, receive, pull, sync, …"), and a
// predicate that went red on a mention would make the honest note impossible to
// write. What must not appear is a line that offers the verb as RUNNABLE —
// `gopi task <verb>` followed by a flag, a `[` or a `<` placeholder.
func advertisedSyncSynopses(body string) []string {
	var found []string
	for _, verb := range syncRemovedVerbSpellingsForHelpTest {
		// The spelling at the head of a LINE, followed by anything that makes it
		// look runnable (a flag, a `[`, a `<`, or just the column padding before
		// a description). The `-` exclusion is what keeps `gopi task sync-pass`
		// from being read as `gopi task sync` plus an argument.
		spelling := "gopi task " + verb
		for at := 0; ; {
			i := strings.Index(body[at:], spelling)
			if i < 0 {
				break
			}
			i += at
			after := body[i+len(spelling):]
			if after == "" || (after[0] != '-' && strings.ContainsRune(" \t[<", rune(after[0]))) {
				end := strings.IndexByte(after, '\n')
				if end < 0 {
					end = len(after)
				}
				found = append(found, spelling+after[:end])
				break
			}
			at = i + len(spelling)
		}
	}
	return found
}

// syncRemovedVerbSpellingsForHelpTest is the deleted family, taken from the
// PRODUCTION list (syncVerbRemovalSpellings) so there is ONE definition of "the
// eight" (Rule 13/14): a spelling added there without a page check is exactly
// the drift this prevents.
var syncRemovedVerbSpellingsForHelpTest = syncVerbRemovalSpellings

// TestDeletedSyncVerbsAreNotAdvertised is the inverted requirement: the family
// page must not offer any of the deleted eight as a runnable command, and must
// still answer a reader who arrives looking for one.
func TestDeletedSyncVerbsAreNotAdvertised(t *testing.T) {
	body := familyUsageBody(t)
	if len(body) < 2000 {
		t.Fatalf("extracted only %d bytes of tasksUsage — the extraction broke, so a green run would prove nothing", len(body))
	}
	// ① The extraction must be reading the family help: a verb that IS still
	// live has to be on the page, or the region below is not the page.
	if !strings.Contains(body, "gopi task submit") {
		t.Fatal("the extracted region carries no `gopi task submit` — it is not the family page, so every assertion below measures nothing")
	}

	// ② No deleted spelling is offered as runnable.
	for _, shape := range advertisedSyncSynopses(body) {
		t.Errorf("`gopi task -h` still advertises %q — that surface is DELETED (2026-11-05); a page that offers it "+
			"sends the reader to a verb which can only refuse, which makes the help a liar (the retirement note is "+
			"where those words belong)", shape)
	}

	// ③ The retirement is LOCATED, and it speaks the reader's words.
	head := strings.Index(body, familyRemovalSectionHead)
	if head < 0 {
		t.Fatalf("the family page carries no %q section — the eight deleted spellings must still be ANSWERED somewhere "+
			"on the page an operator greps, or their removal is invisible unless they happen to type one", familyRemovalSectionHead)
	}
	section := body[head:]
	if end := strings.Index(section, "\nThe git-infrastructure atoms"); end > 0 {
		section = section[:end]
	}
	lowerSection := strings.ToLower(section)
	for _, word := range familyRemovalIntentWords {
		if !strings.Contains(lowerSection, word) {
			t.Errorf("the retirement section never uses %q — a reader who does not already know the verb's name greps "+
				"for the INTENT, and this section is the only region that can answer it", word)
		}
	}
	for _, verb := range syncRemovedVerbSpellingsForHelpTest {
		if !strings.Contains(section, verb) {
			t.Errorf("the retirement section does not name %q — the sentence that says the family is gone must list the "+
				"family, or a reader who typed that one word finds nothing about it", verb)
		}
	}

	// ④ The replacements are named: a removal that leaves the reader with no
	// next command is a dead end dressed as an explanation (Rule 15).
	for _, want := range familyRemovalReplacements {
		if !strings.Contains(section, want) {
			t.Errorf("the retirement section does not name %q — it says the family is gone without saying where the "+
				"capability went", want)
		}
	}
}

// TestDeletedSyncVerbHelpGateIsNotBlind is the RED PROOF (Rule 10.4): the same
// predicate runs over the page AS IT SHIPPED before the deletion — the eight
// synopses verbatim — and must go red on it.
func TestDeletedSyncVerbHelpGateIsNotBlind(t *testing.T) {
	preDeletion := `Moving a row to or from another machine — 'gopi task sync -h' explains each of
these in full; this page gives only the spelling and the shape:
  gopi task sync --peer <alias>            run a sync pass against ONE peer
  gopi task diff --peer <alias>            what this node and the peer disagree on
  gopi task harvest --peer ALIAS           pull the peer's finished branches
  gopi task push                           push finished branches to the peers
  gopi task send --task-id T               hand ONE row to the peer's ledger
  gopi task receive --task-id T            take ONE row off the peer's ledger
  gopi task pull --task-id T               the same leg, with the per-row report
  gopi task sync-pass --once               run every pass once, by hand
`
	found := advertisedSyncSynopses(preDeletion)
	if len(found) == 0 {
		t.Fatal("the predicate found nothing to complain about in the PRE-DELETION page — it cannot go red, so a green " +
			"run on the shipped page proves nothing")
	}
	// Every spelling must be caught, not merely some: a predicate that missed
	// one would leave that verb free to come back unnoticed.
	for _, verb := range syncRemovedVerbSpellingsForHelpTest {
		hit := false
		for _, shape := range found {
			if strings.Contains(shape, "gopi task "+verb+" ") {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("the red proof does not flag %q — a spelling the predicate cannot see is one it cannot keep out", verb)
		}
	}
}
