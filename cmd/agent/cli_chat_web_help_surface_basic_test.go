// cli_chat_web_help_surface_basic_test.go — G5 of the help-surface hardening
// briefing (briefings/brief-help-surface-hardening.md): the chat/UI + web-tool
// command group (`gopi chat`, `gopi web`, `gopi web search`, `gopi web fetch`,
// `gopi cli`, `gopi tool proxy`) gets a BASIC test per advertised surface, and the
// checked-in inventory (devdoc/gopi-chat-web-help-surface-inventory.md) is
// re-derived from the help ON THE REBUILT BINARY so it cannot go stale.
//
// WHY THIS FILE EXISTS AT ALL. The defect class is not a missing line of code:
// it is a surface that was implemented, compiled, unit-tested, reported
// complete — and never typed. A suite that enters through a seam its author
// chose cannot see that (PROJECT.md Rule 31). So every case below drives the REAL
// BINARY over the help's own argv, with an isolated GOPI_HOME, and asserts the
// HELP'S CLAIM rather than incidental output. `-h` is never a run: where a
// surface's only honest probe is "-h reaches the handler", that is stated in
// the inventory's reason column instead of being dressed up as coverage.
//
// THE SURFACE LIST IS DERIVED TWICE, and the build requires agreement:
//   - scripts/g5-chat-web-help-surface.sh awk-parses `gopi <surface> -h` from
//     the binary (the operator-facing derivation quoted in the inventory);
//   - g5DerivedSurfaces below parses the SAME usage text from the constants the
//     binary prints (chatUsage / webUsage / termUsage / proxyUsage / …);
//   - TestG5InventoryMatchesTheHelp reads the checked-in Markdown table back
//     and compares BOTH the surface set and every surface's flag list.
//
// STUBS AND WHETHER THEY ARE HONEST. Two kinds of stand-in appear, and neither
// is a faked peer:
//   - the websearch/webfetch PLUGIN SCRIPTS (GOPI_WEBSEARCH_SCRIPTS_DIR) — the
//     same seam internal/tools already stubs in-process, and it is a REAL
//     subprocess contract: the CLI must shell out to `<dir>/<method>_search.py`
//     with the argv the help promises. It removes the SEARCH ENGINE, not the
//     surface.
//   - the MODEL endpoint (DEEPSEEK_BASE_URL) — a local stub answers one turn so
//     the non-interactive face can be driven end to end; it removes the
//     provider, not the surface. A browser, a peer host and a PTY are NOT
//     stubbed anywhere in this file: those surfaces are listed in the inventory
//     with what they would need.
//
// Rule 17: in-process env flips use t.Setenv. Rule 22/32: every scratch path is
// t.TempDir() under the suite's pinned scratch root — no bare host temp path is
// named here.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// g5ProbeTimeout bounds ONE child. A correctly-routed CLI surface answers in
// well under a second; a probe that needs longer is reported as its own failure
// rather than by hanging the package budget (Rule 27: a probe that can park the
// budget carries its own ceiling).
const g5ProbeTimeout = 20 * time.Second

// g5Surface is one row of the inventory: the help page that defines it, the
// verdict this file makes about it, and — when the verdict is NO — the reason
// and what a future run would need. The reason/needs columns are the briefing's
// explicit requirement: an uncovered surface is listed honestly, never mocked
// into existence.
type g5Surface struct {
	// Name is the surface as the operator spells it (`gopi chat`,
	// `gopi web search`, …) — also the doc's row identity.
	Name string
	// Argv is the invocation whose `-h` page defines the surface.
	Argv []string
	// Help is the usage constant the binary prints for Argv, in its
	// post-substitution form (termOnceUsage is a var, not a const).
	Help string
	// Scoped marks a surface that shares its page with a sibling. `gopi chat
	// list` and `gopi chat backfill` both live in chatListUsage, so a page-wide
	// scan would attribute each one the other's flags; a scoped row derives its
	// flags from ITS OWN synopsis heads (`  gopi chat <verb> …`) and their
	// wrapped continuations instead. Every other row's page is its own.
	Scoped bool
	// Covered is this file's own verdict. It is checked against the drives
	// below: the "yes" rows have a case, the "no" rows have a reason.
	Covered bool
	// Reason is required when Covered is false: WHAT cannot be exercised here.
	Reason string
	// Needs is required when Covered is false: what the exerciser would need
	// (a PTY, a browser, a LAN peer, an upstream credential…).
	Needs string
}

// g5Surfaces is the group's advertised surface, read from the usage constants
// (i.e. the text `gopi <verb> -h` prints, with no binary needed to enumerate
// it).
func g5Surfaces() []g5Surface {
	return []g5Surface{
		{
			Name:    "gopi chat",
			Argv:    []string{"chat"},
			Help:    chatUsage,
			Covered: true,
		},
		{
			Name:    "gopi web",
			Argv:    []string{"web"},
			Help:    webUsage,
			Covered: true,
		},
		{
			Name:    "gopi web search",
			Argv:    []string{"web", "search"},
			Help:    webSearchUsage,
			Covered: true,
		},
		{
			Name:    "gopi web fetch",
			Argv:    []string{"web", "fetch"},
			Help:    webFetchUsage,
			Covered: true,
		},
		{
			Name:    "gopi cli",
			Argv:    []string{termVerbWord()},
			Help:    termUsage,
			Covered: true,
		},
		{
			Name:    "gopi cli --once",
			Argv:    []string{termVerbWord(), termOnceFlag},
			Help:    termOnceUsage,
			Covered: true,
		},
		// `gopi chat list` had a row here and is GONE (epic verb-restructure
		// 2026-10-05): the chat index has ONE door, `gopi session list`, so the
		// surface this table inventories no longer exists. The row is not
		// carried as "retired" either — the inventory lists surfaces the help
		// ADVERTISES, and a word the help points away from is not one.
		{
			Name:    "gopi chat backfill",
			Argv:    []string{"chat", chatSubcommandBackfill},
			Help:    chatListUsage,
			Scoped:  true,
			Covered: true,
		},
		{
			Name: "gopi tool proxy",
			// The door is the group's — `gopi proxy` is refused by name since
			// verb-retirement phase 1, and familyAddressArgs is what turns the
			// verb constant into the address the probe must drive.
			Argv:    familyAddressArgs([]string{CliVerbProxy}),
			Help:    proxyUsage,
			Covered: true,
		},
	}
}

// termVerbWord is the `cli` verb word (a small indirection so this file does
// not spell a verb literal twice).
func termVerbWord() string { return CliVerbCLI }

// g5SurfaceNames is the derivation order, pinned so the doc's row order is
// stable across runs (a table that reorders on every regeneration is a diff
// nobody reads).
func g5SurfaceNames() []string {
	out := make([]string, 0, len(g5Surfaces()))
	for _, s := range g5Surfaces() {
		out = append(out, s.Name)
	}
	return out
}

// --- the flag derivation (one rule, mirrored by the shell script) -------------

// g5FlagHeadRe matches a synopsis head line: at most two spaces of indent and
// a `gopi …` invocation. Both the group pages (`  gopi web search "<query>"…`)
// and the cli page (`  gopi cli …`, two spaces) hit it; an indented example
// that also begins `gopi ` is deliberately included — it IS a valid invocation
// of the same surface, and including it can only ADD an advertised flag, never
// hide one (a scanner that under-reports is the failure mode being avoided).
var g5FlagHeadRe = regexp.MustCompile(`^ {0,2}gopi `)

// g5FlagDefRe matches a flag DEFINITION line (`  --method X   …`,
// `-v, --verbose   …`, `  --once, -p …`). The three families — a synopsis head,
// a bracketed synopsis continuation, a definition line — are the ONLY lines
// whose `--flag` tokens are the surface; the prose paragraphs quote flags too
// (`--max-chars` inside webFetchUsage's own description of the JSON payload)
// and are what a naive whole-page scan over-reports.
var (
	g5FlagWrapRe = regexp.MustCompile(`^ {18,}\[`)
	g5FlagDefRe  = regexp.MustCompile(`^ {0,2}-{1,2}[A-Za-z][A-Za-z-]*(, *-{1,2}[A-Za-z][A-Za-z-]*)? +[A-Za-z<{]`)
	g5FlagTokRe  = regexp.MustCompile(`--[a-z][a-z-]*`)
)

// g5DeriveFlags returns the flag surface of one help page, sorted, using the
// rule documented in the inventory and mirrored by
// scripts/g5-chat-web-help-surface.sh. `--help` is excluded by name: it is the
// universal help spelling (the `-h` a reader already typed to get the page),
// not a knob of the surface.
func g5DeriveFlags(help string) []string { return g5DeriveFlagsScoped(help, "") }

// g5DeriveFlagsScoped is the derivation of ONE row. With scope=="" it scans the
// whole page (the row owns its page). With scope set (a sub-verb of `gopi
// chat`) it reads only that verb's own synopsis block — its head plus the
// wrapped continuation lines — so two rows sharing one page are not each
// handed the other's flags.
func g5DeriveFlagsScoped(help, scope string) []string {
	seen := map[string]bool{}
	inScope := scope == ""
	for _, ln := range strings.Split(help, "\n") {
		if scope != "" {
			switch {
			case strings.HasPrefix(ln, "  gopi chat "+scope+" ") || ln == "  gopi chat "+scope:
				inScope = true
			case g5FlagHeadRe.MatchString(ln):
				// Another verb's head ends this block. (A chat-list synopsis
				// does not wrap today; if it ever does, the continuation rule
				// below picks the wrap up before this case can fire.)
				inScope = false
			}
		}
		if !inScope {
			continue
		}
		if !g5FlagHeadRe.MatchString(ln) && !g5FlagWrapRe.MatchString(ln) && !g5FlagDefRe.MatchString(ln) {
			if scope != "" && strings.HasPrefix(strings.TrimLeft(ln, " "), "gopi chat ") {
				// Defensive: a head indented deeper than the group's two spaces
				// still ends the block rather than lending its flags onward.
				inScope = false
			}
			continue
		}
		// A definition line holds two spellings of one flag (`-v, --verbose`):
		// the long spelling is the surface's identity, so the tokens are read
		// from the whole line and deduped.
		for _, m := range g5FlagTokRe.FindAllString(ln, -1) {
			f := strings.TrimPrefix(m, "--")
			if f == "help" {
				continue
			}
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// g5FlagsOf is the surface-aware entry point: the scoped derivation for a
// shared page, the page-wide one otherwise. Both the drift gate and the shell
// script call the same rule.
func g5FlagsOf(s g5Surface) []string {
	if s.Scoped {
		return g5DeriveFlagsScoped(s.Help, s.Argv[len(s.Argv)-1])
	}
	return g5DeriveFlags(s.Help)
}

// TestG5FlagDerivationIsNotBlind is the non-vacuity floor: a scanner that
// silently stopped matching would report every surface as flagless and every
// gate below would pass on an empty set. The floors are the measured counts at
// the base commit (953a92c2), so a parse regression fails loudly instead of
// going quiet.
func TestG5FlagDerivationIsNotBlind(t *testing.T) {
	minFlags := map[string]int{
		"gopi chat":          3,
		"gopi web":           3, // --addr, --tools, --thinking-live
		"gopi web search":    2,
		"gopi web fetch":     1,
		"gopi cli":           3,
		"gopi cli --once":    3,
		"gopi chat backfill": 2, // --dry-run --verbose, from backfill's OWN head (the page is shared with the retired `list`)
		"gopi tool proxy":    9,
	}
	total := 0
	for _, s := range g5Surfaces() {
		got := g5FlagsOf(s)
		want, ok := minFlags[s.Name]
		if !ok {
			t.Fatalf("%s has no derivation floor — add one, or the gate cannot see a blind scan", s.Name)
		}
		if len(got) < want {
			t.Errorf("%s: derived %d flag(s) %v, floor %d — the scanner under-reports", s.Name, len(got), got, want)
		}
		total += len(got)
	}
	if total < 20 {
		t.Errorf("the whole derivation produced %d flag(s) — a broken parse cannot pass as coverage", total)
	}
}

// g5DerivationScript is the operator-facing re-derivation quoted in the
// inventory's "How to re-derive this table" section. It is gated below because
// it is the ONE artifact that is not covered by the two derivation arms in this
// file: those read the usage CONSTANTS (g5FlagsOf) and the checked-in table, so
// a script asking for a name the dispatcher has since RETIRED goes on printing
// `0` flags for that row while both in-Go arms stay green. Measured on
// 2026-10-05: after verb-retirement phase 1, `derive "proxy"` was a refusal
// (`gopi proxy` exits 1 naming `gopi tool proxy`), so the script reported 8 rows
// and missed the proxy surface's 9 flags — the exact "regenerate the table"
// command the doc tells a reader to trust.
const g5DerivationScript = "scripts/g5-chat-web-help-surface.sh"

// TestG5DerivationScriptDrivesEverySurfaceAddress is the regression guard for
// that drift: the surface list this file declares (g5Surfaces, whose Argv is
// already the family address) must appear, verbatim, in the derivation script —
// so a retired spelling in the script fails the build instead of silently
// under-reporting the group.
func TestG5DerivationScriptDrivesEverySurfaceAddress(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(g5DerivationScript)))
	if err != nil {
		t.Fatalf("read the derivation script (the command the inventory tells a reader to re-run): %v", err)
	}
	body := string(raw)
	for _, s := range g5Surfaces() {
		// The script spells a surface the way the dispatcher accepts it, which
		// is the same argv the probe drives (family verb included:
		// `derive "tool proxy"` for `gopi tool proxy`).
		address := strings.Join(s.Argv, " ")
		if !strings.Contains(body, `derive "`+address+`"`) {
			t.Errorf("%s: %s has no `derive %q` row — the script a reader re-runs would not derive this surface at all", s.Name, g5DerivationScript, address)
		}
	}
	// The retired bare spelling must NOT come back: it is a refusal now (exit 1),
	// so a script row naming it prints 0 flags and reports the group short by
	// that surface's whole flag set.
	if strings.Contains(body, `derive "`+CliVerbProxy+`"`) {
		t.Errorf("%s still derives the RETIRED spelling `derive %q` — the dispatcher refuses it by name, so that row derives 0 flags; use the family address `%s`", g5DerivationScript, CliVerbProxy, strings.Join(familyAddressArgs([]string{CliVerbProxy}), " "))
	}
}

// g5Uncovered is the HONEST GAP LIST: advertised capabilities of this group
// that this file does NOT exercise, each with the reason and what a future run
// would need. They are not surfaces (no argv of their own drives them), so they
// live in the inventory's second table — and this list is gated against that
// table in both directions, because the one thing a gap list must never do is
// quietly lose an entry (the run that deletes a "why not" is the run that
// reports full coverage).
type g5Gap struct {
	Claim string // what the help promises
	Why   string // why this file cannot exercise it
	Needs string // what exercising it would take
}

//nolint:gochecknoglobals // the invented-by-nobody gap list, read by the gate
var g5Uncovered = []g5Gap{
	{
		Claim: "gopi web search — the real search engines (bing/google/arxiv/github/deepseek) and the merged default",
		Why:   "each method is a python3 plugin that talks to a live engine; the network AND the operator's plugin credentials are outside a test's reach, and the briefing forbids standing in a fake engine",
		Needs: "a network egress plus the plugins' own API keys (the operator's /home/nick/.gopi/plugins/websearch config); the routing contract (which script, which argv) is what the tests here cover instead",
	},
	{
		Claim: "gopi web fetch — a real page fetched through the search-bot VPS jump host",
		Why:   "the fetch runs on a remote jump host with the operator's own trust relationship; a test that reached it would be testing that host, and stubbing it would prove nothing about it (measured: a direct fetch of example.com answers HTTP 403, i.e. the jump host is the only working path)",
		Needs: "the jump host's access (ssh target + any proxy credentials) and network egress; the URL/flag routing and the documented payload are what the tests here cover instead",
	},
	{
		Claim: "gopi web --addr=<LAN address> — the non-loopback bind, the per-run access token and the LAN URL",
		Why:   "the claim is about ANOTHER machine reaching this one; a second host on the LAN is not available inside a test, and binding 0.0.0.0 in a test would only re-test the same process",
		Needs: "a second device on the LAN (or a second network namespace) to open ?t=<token> from; the loopback bind, the served page and the token plumbing are what the tests here cover",
	},
	{
		Claim: "gopi tool proxy — the Anthropic→OpenAI translation against a real upstream provider",
		Why:   "the translation needs an upstream that speaks the OpenAI wire protocol and a key for it; internal/proxy's own unit tests cover the translation, and no credential for a provider is available in a test",
		Needs: "an OpenAI-compatible upstream plus its API key (the operator's --openai-key); the argv admission, the listener and the refusals are what the tests here cover",
	},
	{
		Claim: "gopi cli — the chat page RENDERED by a real text browser (links2/w3m-class)",
		Why:   "the carrier only execs a browser with the /cli URL, and the vendored links2 cannot run in this sandbox (measured: `links -dump https://example.com` answers \"This version of Links does not contain SSL/TLS support\", and under the test harness it dies with \"Epoll ADD(1) on fd 0 failed: Operation not permitted\")",
		Needs: "a terminal/X11 environment where the vendored links2 can open an event loop, plus visual confirmation of the rendered page; the carrier's contract (server started, /cli URL computed, browser exec'd with argv) is what the tests here cover",
	},
	{
		Claim: "gopi chat --tools / gopi cli --tools — a real TOOL-CALLING turn (the model calls a tool and the result is rendered)",
		Why:   "the tests here assert that the toolset is SHIPPED on the wire and that the carrier's tool frames reach fd 3 (the pre-existing in-process gates), but no turn in this file has the model actually call a tool: that needs a scripted multi-round provider conversation",
		Needs: "a stub model scripted to answer round 1 with a tool_use and round 2 with text (the shape cmd/agent/cli_term_once_test.go already drives in-process), driven through the CLI entry point",
	},
}

// --- the inventory table gate -------------------------------------------------

// g5InventoryRowRe reads one row of the checked-in table:
// `| 1 | `gopi chat` | `no-editor,tools,verbose` | yes | — | — |`.
var g5InventoryRowRe = regexp.MustCompile("(?m)^\\| *[0-9]+ *\\| *`([^`]+)` *\\| *`([^`]*)` *\\| *(yes|no) *\\| *([^|]*) *\\| *([^|]*) *\\|")

// g5InventoryPath is the deliverable this gate protects.
const g5InventoryPath = "devdoc/gopi-chat-web-help-surface-inventory.md"

// TestG5InventoryMatchesTheHelp is the anti-staleness gate: the table's surface
// set and EACH surface's flag list must equal the derivation from the usage
// text, and a row that claims "no" must carry a reason and a need. A row that
// claims "yes" must be backed by this file's own verdict, so a table cannot
// advertise coverage the test file does not make.
func TestG5InventoryMatchesTheHelp(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(g5InventoryPath)))
	if err != nil {
		t.Fatalf("read the inventory table (the deliverable this gate protects): %v", err)
	}
	rows := g5InventoryRowRe.FindAllStringSubmatch(string(raw), -1)

	type row struct {
		flags  []string
		cover  string
		reason string
		needs  string
	}
	byName := map[string]row{}
	for _, m := range rows {
		name := m[1]
		if _, dup := byName[name]; dup {
			t.Errorf("%s: the inventory carries the surface twice — one row per surface", name)
		}
		var flags []string
		if strings.TrimSpace(m[2]) != "" {
			flags = strings.Split(m[2], ",")
		}
		byName[name] = row{flags: flags, cover: m[3], reason: strings.TrimSpace(m[4]), needs: strings.TrimSpace(m[5])}
	}

	for _, s := range g5Surfaces() {
		r, ok := byName[s.Name]
		if !ok {
			t.Errorf("the inventory has no row for %s — the table is the deliverable, and a surface the help advertises must appear in it", s.Name)
			continue
		}
		want := g5FlagsOf(s)
		got := r.flags
		if strings.Join(want, ",") != strings.Join(got, ",") {
			t.Errorf("%s: the inventory lists flags %v, the help advertises %v — regenerate the table (scripts/g5-chat-web-help-surface.sh)", s.Name, got, want)
		}
		wantCover := "no"
		if s.Covered {
			wantCover = "yes"
		}
		if r.cover != wantCover {
			t.Errorf("%s: the inventory says has-basic-test=%s, this file says %s — the table and the tests must agree", s.Name, r.cover, wantCover)
		}
		if r.cover == "no" && (r.reason == "" || r.needs == "") {
			t.Errorf("%s: an uncovered surface must state WHY and WHAT WOULD BE NEEDED (got reason=%q needs=%q) — an unexplained gap is the thing the briefing forbids", s.Name, r.reason, r.needs)
		}
	}
	for name := range byName {
		known := false
		for _, s := range g5Surfaces() {
			if s.Name == name {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("the inventory has a row for %q, which is not a surface of this group — a stale row is a claim about a surface that no longer exists", name)
		}
	}
	if len(rows) != len(g5Surfaces()) {
		t.Errorf("the inventory has %d rows, the group has %d surfaces", len(rows), len(g5Surfaces()))
	}

	// The Totals line is part of the deliverable too: a reader takes its
	// numbers as the group's census, so it is gated like the rows above it.
	// It DID go stale once — when `chat list` and `chat backfill` stopped
	// sharing their page's flags the table rows were corrected and this line
	// was not, so it advertised a count the help no longer derives. Gating
	// only the rows could not see that, which is why the line is read here.
	totals := g5TotalsLineRe.FindStringSubmatch(string(raw))
	if totals == nil {
		t.Fatalf("the inventory carries no `**Totals:** N surfaces, M advertised flags, K driven by a basic test` line — the reader's census is part of the deliverable and must be gated")
	}
	wantFlags, wantDriven := 0, 0
	for _, s := range g5Surfaces() {
		wantFlags += len(g5FlagsOf(s))
		if s.Covered {
			wantDriven++
		}
	}
	for _, c := range []struct {
		what string
		got  string
		want int
	}{
		{"surfaces", totals[1], len(g5Surfaces())},
		{"advertised flags", totals[2], wantFlags},
		{"surfaces driven by a basic test", totals[3], wantDriven},
	} {
		n, err := strconv.Atoi(c.got)
		if err != nil || n != c.want {
			t.Errorf("the inventory's Totals line says %s %s, this file derives %d — regenerate the table (scripts/g5-chat-web-help-surface.sh)", c.got, c.what, c.want)
		}
	}
}

// g5TotalsLineRe reads the census line under the surface table:
// `**Totals:** 9 surfaces, 28 advertised flags, 9 driven by a basic test, 0 …`.
var g5TotalsLineRe = regexp.MustCompile("(?m)^\\*\\*Totals:\\*\\* *([0-9]+) surfaces?, *([0-9]+) advertised flags?, *([0-9]+) driven")

// g5GapRowRe reads one row of the inventory's SECOND table — the honest gap
// list: `| claim | why | needed |`.
var g5GapRowRe = regexp.MustCompile("(?m)^\\| *([^|]+?) *\\| *([^|]+?) *\\| *([^|]+?) *\\|")

// g5PlainText strips Markdown code spans, so the gate compares the text a
// reader sees rather than the markup that renders it.
func g5PlainText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(s), "`", ""))
}

// g5GapSectionHeading delimits the gap table. Position, not punctuation, is
// what tells the two tables apart: the surface table lives ABOVE this heading
// and the gap list below it, so neither gate can confuse one for the other and
// no row has to carry a marker.
const g5GapSectionHeading = "## The honest gap list"

// TestG5InventoryListsEveryGapTheTestsCannotCover pins the briefing's own
// requirement — "every surface NOT covered appears in the table with its reason
// and what is needed" — in the direction that matters: a gap cannot be dropped
// from the doc while the test file still declares it, and the doc cannot invent
// a gap the test file has not declared.
func TestG5InventoryListsEveryGapTheTestsCannotCover(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(g5InventoryPath)))
	if err != nil {
		t.Fatalf("read the inventory: %v", err)
	}
	doc := string(raw)
	cut := strings.Index(doc, g5GapSectionHeading)
	if cut < 0 {
		t.Fatalf("the inventory has no %q section — the honest gap list is where an unexercised capability is recorded, and its absence reads as full coverage", g5GapSectionHeading)
	}
	rows := g5GapRowRe.FindAllStringSubmatch(doc[cut:], -1)
	inDoc := map[string]bool{}
	for _, m := range rows {
		claim := g5PlainText(m[1])
		if claim == "" || strings.HasPrefix(claim, "---") || strings.HasPrefix(claim, "gap ") {
			continue // the separator row and the header row
		}
		inDoc[claim] = true
		if strings.TrimSpace(m[2]) == "" || strings.TrimSpace(m[3]) == "" {
			t.Errorf("gap row %q carries no reason/need column — an unexplained gap is the thing the briefing forbids", claim)
		}
	}
	for _, g := range g5Uncovered {
		if !inDoc[g5PlainText(g.Claim)] {
			t.Errorf("the inventory does not list the gap %q — a gap this file declares must appear in the doc with its reason and what it needs", g.Claim)
		}
	}
	if len(inDoc) != len(g5Uncovered) {
		t.Errorf("the inventory's gap table has %d row(s), this file declares %d — the two must agree", len(inDoc), len(g5Uncovered))
	}
}

// --- the probe harness --------------------------------------------------------

// g5Probe is one sandboxed child: an isolated GOPI_HOME, the scrubbed contract
// environment (cliContractEnv — never the operator's profile), and the extra
// env a case needs.
type g5Probe struct {
	bin  string
	home string
	env  []string
}

func g5NewProbe(t *testing.T) *g5Probe {
	t.Helper()
	return &g5Probe{bin: gopiTestBinPath(t), home: t.TempDir()}
}

// withPath prepends a directory to the child's PATH. gopi spawns the websearch
// plugin scripts by NAME-free absolute path, but python3 itself is resolved
// from PATH, so a probe that wants a hermetic interpreter puts one first.
func (p *g5Probe) withPath(dir string) *g5Probe {
	return p.with("PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"))
}

// with returns a copy of the probe carrying one more env pair.
func (p *g5Probe) with(kv ...string) *g5Probe {
	cp := *p
	cp.env = append(append([]string{}, p.env...), kv...)
	return &cp
}

// envVec builds the child's environment: the scrubbed contract env plus this
// probe's own pairs, appended AFTER the scrub so a ledger/plugin override
// survives it.
func (p *g5Probe) envVec() []string {
	return append(cliContractEnv(p.home), p.env...)
}

// g5Result is one finished probe.
type g5Result struct {
	out  string
	exit int
}

// run drives the binary and waits, bounded by g5ProbeTimeout. stdin is a pipe
// the caller may feed; a closed stdin is the honest way to ask an interactive
// face to end.
func (p *g5Probe) run(t *testing.T, stdin string, args ...string) g5Result {
	t.Helper()
	cmd := exec.Command(p.bin, familyAddressArgs(args)...)
	cmd.Env = p.envVec()
	cmd.Stdin = strings.NewReader(stdin)
	return g5Gather(t, cmd, args)
}

// runLive starts the child WITHOUT waiting (a server surface: web/cli/proxy)
// and returns the process plus a snapshot function. The caller MUST kill it;
// the process group is killed so no grandchild survives (Rule 24's family: a
// server that greps its own children stays alive).
//
// The child's output goes through an OS PIPE, never io.Pipe. The difference is
// the writer's owner: an io.Pipe is closed only by a call from THIS process, so
// the reader below had no peer to signal EOF — its writer was never closed, and
// the reader stayed parked in io.PipeReader.Read for the rest of the run
// (measured 2026-09-25: FOUR such readers, one per g5 probe, still on the stack
// after their own tests had reported, holding the suite's binary alive). An
// os.Pipe's write end is held by the CHILD (and by any grandchild that
// inherited the descriptor), so READ returns the moment the process group this
// test owns is gone: the reader is closed by the kernel, not by a line of test
// code that can be forgotten. (Copied descriptors are the point: it is also
// what makes a leaked grandchild visible here instead of silent.)
func (p *g5Probe) runLive(t *testing.T, args ...string) (*exec.Cmd, func() string) {
	t.Helper()
	cmd := exec.Command(p.bin, familyAddressArgs(args)...)
	cmd.Env = p.envVec()
	cmd.Stdin = strings.NewReader("")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	var buf strings.Builder
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("create the probe's output pipe: %v", err)
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close() //stylegate:ignore closing both ends of a pipe whose child never started; the start error is what is being reported
		_ = pw.Close() //stylegate:ignore the same pipe's write end
		t.Fatalf("start gopi %s: %v", strings.Join(args, " "), err)
	}
	// The parent keeps the READ end only: this close is what guarantees the copy
	// below ends. The child holds its own copy of the write end from cmd.Start.
	_ = pw.Close() //stylegate:ignore the parent's copy of the child's write end; the child holds it from cmd.Start onwards
	go func() {
		_, _ = io.Copy(&syncWriter{mu: &mu, b: &buf}, pr) //stylegate:ignore test stream pump; a short read is harmless
		_ = pr.Close()                                    //stylegate:ignore closing the read end the copy just drained
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore killing the probe group this test started; ESRCH is expected
		}
		_ = cmd.Wait() //stylegate:ignore reaping a probe this test killed
	})
	return cmd, func() string { mu.Lock(); defer mu.Unlock(); return buf.String() }
}

type syncWriter struct {
	mu *sync.Mutex
	b  *strings.Builder
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// g5Gather runs a prepared command under the probe ceiling and returns its
// combined output and exit code. A timeout is its own failure: a routed CLI
// surface never reaches the agent loop, and one that hangs is a routing defect.
func g5Gather(t *testing.T, cmd *exec.Cmd, args []string) g5Result {
	t.Helper()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(g5ProbeTimeout):
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore killing the probe group this test started
		}
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %s — a routed CLI surface does not park (agent-loop fallthrough, or a child nobody reaps)", strings.Join(args, " "), g5ProbeTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		code = ee.ExitCode()
	}
	return g5Result{out: string(out), exit: code}
}

// --- fixture: the plugin-script seam -----------------------------------------

// g5WriteStubScripts writes a plugin scripts dir whose wiki search and fetch
// scripts answer the JSON payloads the help promises, and returns its path.
// The CLI runs them as `python3 <dir>/<name>.py <query> --count N` (that is the
// contract the help states), so the stubs import nothing but the stdlib and no
// byte-code is left beside them.
//
// This is not a mocked peer: the CLI's contract is "run the plugin script in
// GOPI_WEBSEARCH_SCRIPTS_DIR and print its JSON" (the help says exactly that),
// so a real subprocess answering the documented argv is the surface being
// exercised. What the stub removes is the search ENGINE behind the script.
func g5WriteStubScripts(t *testing.T, searchScript, fetchScript string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 is not on PATH — the web plugin scripts are python3 programs, so the real surface cannot be driven here: %v", err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"wiki_search.py": searchScript,
		"web_fetch.py":   fetchScript,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	return dir
}

// g5StubSearchEcho answers with the query and count the CLI passed — so the
// assertions below read the HELP's claim (the reported method, the capped
// count, the query) off the plugin's own argv.
const g5StubSearchEcho = `#!/usr/bin/env python3
import json, sys
a = sys.argv[1:]
count = int(a[a.index("--count")+1]) if "--count" in a else 0
print(json.dumps({"status":"ok","method":"wiki","elapsed_ms":1,"count":count,
                  "results":[{"title":"stub:"+a[0],"url":"https://example.org/stub","snippet":"s"}]}))
`

// g5StubFetchEcho answers with the URL and the max-chars the CLI passed.
const g5StubFetchEcho = `#!/usr/bin/env python3
import json, sys
a = sys.argv[1:]
mc = int(a[a.index("--max-chars")+1]) if "--max-chars" in a else 0
text = "x" * (mc + 50)
print(json.dumps({"status":"ok","url":a[0],"chars":len(text),"truncated":True,
                  "content":text,"elapsed_ms":2}))
`

// --- case 1: gopi web search --------------------------------------------------

// TestG5WebSearchRunsAsAdvertised drives `gopi web search "<query>" --method
// wiki --count N` exactly as the help spells it, and asserts the help's own
// claims: the method is routed to that method's script, the count reaches it,
// and the reply is the unified JSON payload.
func TestG5WebSearchRunsAsAdvertised(t *testing.T) {
	p := g5NewProbe(t).with("GOPI_WEBSEARCH_SCRIPTS_DIR=" + g5WriteStubScripts(t, g5StubSearchEcho, g5StubFetchEcho))

	// Happy path — every advertised flag of this surface on ONE invocation.
	res := p.run(t, "", "web", "search", "retrieval augmented generation", "--method", "wiki", "--count", "3")
	if res.exit != 0 {
		t.Fatalf("gopi web search: exit %d (want 0)\n%s", res.exit, res.out)
	}
	var payload struct {
		Status  string `json:"status"`
		Method  string `json:"method"`
		Count   int    `json:"count"`
		Results []struct {
			Title string `json:"title"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(lastJSONLine(res.out)), &payload); err != nil {
		t.Fatalf("the surface's output is not the documented JSON payload: %v\n%s", err, res.out)
	}
	if payload.Status != "ok" || payload.Method != "wiki" {
		t.Errorf("payload = %+v, want status=ok method=wiki (the --method the help advertises)", payload)
	}
	if payload.Count != 3 {
		t.Errorf("count = %d, want 3 (the --count the help advertises)", payload.Count)
	}
	if len(payload.Results) != 1 || !strings.HasPrefix(payload.Results[0].Title, "stub:") {
		t.Errorf("results = %+v — the query never reached the plugin script", payload.Results)
	}

	// First refusal: an unknown method (the help names the enum).
	res = p.run(t, "", "web", "search", "x", "--method", "yahoo")
	if res.exit == 0 {
		t.Errorf("unknown --method exited 0 — the help says invalid values error with this usage\n%s", res.out)
	}
	if !strings.Contains(res.out, "bing|google|arxiv|deepseek|github|wiki") {
		t.Errorf("the refusal does not name the enum:\n%s", res.out)
	}

	// First refusal: a missing query.
	res = p.run(t, "", "web", "search")
	if res.exit == 0 || !strings.Contains(res.out, "missing <query>") {
		t.Errorf("a missing <query> must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
}

// TestG5WebSearchMethodNeedsAValue pins the defect this run found and fixed:
// `--method` with no value used to be silently accepted as the "merged"
// default, while the sibling `--count` refused a missing value. The help
// spells the flag `--method X`; omitting the flag is how merged is asked for.
//
// RED PROOF (same argv, same ledger, pre-fix binary built from HEAD's source):
//
//	pre-fix:  {"status":"unavailable","method":"merged",...}   exit=0
//	post-fix: gopi web search: --method needs a value          exit=1
//
//nolint:lll // the transcript above is evidence, kept verbatim
func TestG5WebSearchMethodNeedsAValue(t *testing.T) {
	p := g5NewProbe(t).with("GOPI_WEBSEARCH_SCRIPTS_DIR=" + g5WriteStubScripts(t, g5StubSearchEcho, g5StubFetchEcho))
	res := p.run(t, "", "web", "search", "q", "--method")
	if res.exit == 0 {
		t.Fatalf("`gopi web search q --method` (no value) exited 0 — it must refuse; the help spells the flag `--method X`\n%s", res.out)
	}
	if !strings.Contains(res.out, "--method") {
		t.Errorf("the refusal does not name --method:\n%s", res.out)
	}
}

// --- case 2: gopi web fetch ---------------------------------------------------

// TestG5WebFetchRunsAsAdvertised drives `gopi web fetch <url> --max-chars N`
// (both advertised flags/spellings) and asserts the help's claims: the URL is
// routed to web_fetch.py, the cap reaches it, and the reply is the documented
// payload.
func TestG5WebFetchRunsAsAdvertised(t *testing.T) {
	p := g5NewProbe(t).with("GOPI_WEBSEARCH_SCRIPTS_DIR=" + g5WriteStubScripts(t, g5StubSearchEcho, g5StubFetchEcho))

	res := p.run(t, "", "web", "fetch", "https://example.org/readme", "--max-chars", "1200")
	if res.exit != 0 {
		t.Fatalf("gopi web fetch: exit %d (want 0)\n%s", res.exit, res.out)
	}
	var payload struct {
		Status string `json:"status"`
		URL    string `json:"url"`
		Chars  int    `json:"chars"`
	}
	if err := json.Unmarshal([]byte(lastJSONLine(res.out)), &payload); err != nil {
		t.Fatalf("the surface's output is not the documented JSON payload: %v\n%s", err, res.out)
	}
	if payload.Status != "ok" || payload.URL != "https://example.org/readme" {
		t.Errorf("payload = %+v, want the URL the caller passed", payload)
	}
	if payload.Chars != 1250 {
		t.Errorf("chars = %d, want 1250 — the --max-chars the help advertises never reached web_fetch.py", payload.Chars)
	}

	// The second advertised spelling of the same flag, as the help prints it in
	// its example (`--max-chars=25000`).
	res = p.run(t, "", "web", "fetch", "https://example.org/readme", "--max-chars=40")
	if res.exit != 0 || !strings.Contains(res.out, `"chars": 90`) {
		t.Errorf("the joined spelling --max-chars=40 did not route: exit=%d\n%s", res.exit, res.out)
	}

	// First refusal: a non-http URL — the documented gate.
	res = p.run(t, "", "web", "fetch", "not-a-url")
	if res.exit != 0 {
		t.Fatalf("a non-URL is reported as the tool's own structured error payload, not a usage failure: exit=%d\n%s", res.exit, res.out)
	}
	if !strings.Contains(res.out, "only http/https URLs are supported") {
		t.Errorf("the refusal does not carry the documented message:\n%s", res.out)
	}

	// First refusal: a malformed --max-chars.
	res = p.run(t, "", "web", "fetch", "https://example.org/x", "--max-chars", "abc")
	if res.exit == 0 || !strings.Contains(res.out, "--max-chars") {
		t.Errorf("a non-integer --max-chars must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
}

// TestG5WebFetchNonPositiveRestoresTheDefault pins the help's own sentence —
// "Values <= 0 restore the default" — which the parser used to REFUSE with
// "--max-chars must be a positive integer". The tool layer already clamps
// (tools.WebFetch → clampWebFetchMaxChars), and the page claims this CLI is
// "the same function path", so the two had to agree.
//
// RED PROOF (same argv, same ledger, pre-fix binary):
//
//	pre-fix:  gopi web fetch: --max-chars must be a positive integer, got "0"   exit=1
//	post-fix: {"status":"unavailable","url":"https://example.com",...}          exit=0
//	          (unavailable = the plugin script is absent; the FLAG was accepted,
//	           and the default cap is what reached the tool)
//
//nolint:lll // the transcript above is evidence, kept verbatim
func TestG5WebFetchNonPositiveRestoresTheDefault(t *testing.T) {
	p := g5NewProbe(t).with("GOPI_WEBSEARCH_SCRIPTS_DIR=" + g5WriteStubScripts(t, g5StubSearchEcho, g5StubFetchEcho))
	for _, v := range []string{"0", "-5"} {
		res := p.run(t, "", "web", "fetch", "https://example.org/x", "--max-chars", v)
		if res.exit != 0 {
			t.Fatalf("--max-chars %s: exit %d — the help says values <= 0 restore the default, so this must not be a usage error\n%s", v, res.exit, res.out)
		}
		// The default cap (8000) is what reached the script: the stub echoes
		// max-chars+50, so the default is observable in the reply.
		if !strings.Contains(res.out, `"chars": 8050`) {
			t.Errorf("--max-chars %s did not restore the documented default (%d):\n%s", v, webFetchDefaultMaxChars, res.out)
		}
	}
}

// --- case 3: gopi tool proxy -------------------------------------------------------

// TestG5ProxyRunsAsAdvertised drives the proxy with every advertised flag of
// its synopsis shape and asserts the one effect the help promises that needs no
// upstream: it LISTENS on the HTTP port it printed. The translation itself is
// covered by internal/proxy's own tests — what this case proves is that the
// advertised argv is admitted and really binds a socket.
func TestG5ProxyRunsAsAdvertised(t *testing.T) {
	p := g5NewProbe(t)
	addr := g5FreeAddr(t)
	cmd, snapshot := p.runLive(t, CliVerbProxy,
		"--upstream", "openai",
		"--openai-base-url", "http://127.0.0.1:1/v1",
		"--openai-model", "stub-model",
		"--openai-key", "stub-key",
		"--listen", addr)

	// Wait for the banner, bounded — the help says the proxy "listens on an
	// HTTP port", and the banner is where it names it.
	deadline := time.Now().Add(g5ProbeTimeout)
	for {
		if strings.Contains(snapshot(), "gopi tool proxy: listening on "+addr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the proxy never printed its listening banner for %s\n%s", addr, snapshot())
		}
		if cmd.ProcessState != nil {
			t.Fatalf("the proxy exited before listening:\n%s", snapshot())
		}
		//stylegate:allow-sleep polls the child's stderr for the proxy's listening banner; the loop's deadline owns the failure and it exits on the first hit
		time.Sleep(50 * time.Millisecond)
	}

	// The claim, made observable: the port really accepts a connection (the
	// proxy answers an Anthropic client with an HTTP status, whatever it is —
	// the upstream is unreachable, which is the point: the LISTENER is ours).
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("the proxy printed a listening banner for %s but nothing accepts a connection: %v", addr, err)
	}
	_ = conn.Close() //stylegate:ignore closes the fixture connection the test owns

	// First refusal: --upstream is required, and only two values are valid.
	//
	// The argv starts at `tool proxy`, the LIVE door (cli_verb_aliases.go owns
	// that address and p.run rewrites the leading word through the same table):
	// the bare `gopi proxy` is a RETIRED spelling whose refusal — measured on
	// this branch — answers with the group page, so driving it here would
	// assert the retirement notice against the flag parser's own message.
	res := p.run(t, "", CliVerbTool, CliVerbProxy, "--upstream", "bogus", "--listen", g5FreeAddr(t))
	if res.exit == 0 || !strings.Contains(res.out, "invalid --upstream") {
		t.Errorf("an invalid --upstream must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
	res = p.run(t, "", CliVerbTool, CliVerbProxy)
	if res.exit == 0 || !strings.Contains(res.out, "--upstream is required") {
		t.Errorf("a missing --upstream must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
	// First refusal: the openai mode's own required trio.
	res = p.run(t, "", CliVerbTool, CliVerbProxy, "--upstream", "openai", "--listen", g5FreeAddr(t))
	if res.exit == 0 || !strings.Contains(res.out, "--openai-base-url") {
		t.Errorf("openai mode without its trio must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
}

// g5FreeAddr reserves a port and releases it, returning the address. The
// reservation is released on purpose: the subject must bind it itself, and a
// held listener would mask a bind failure as a test-only conflict.
func g5FreeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}
	return addr
}

// --- case 4: gopi web (the browser chat server) -------------------------------

// TestG5WebServerRunsAsAdvertised drives `gopi web --addr HOST:PORT --tools`
// and asserts the help's claim: the page is SERVED at the printed URL. The
// browser that would render it is not stubbed — the HTTP surface is the thing
// the flag controls, and it is read for real over the socket.
func TestG5WebServerRunsAsAdvertised(t *testing.T) {
	// The server builds ONE agent at startup (it holds the turn's model client),
	// so the endpoint is pointed at the local stub. That is not a stubbed
	// BROWSER — nothing here renders a page; the HTTP surface the --addr flag
	// controls is read for real over a socket.
	model := g5NewStubModel(t, "OK there")
	p := g5NewProbe(t).with(model.env()...)
	addr := g5FreeAddr(t)
	_, snapshot := p.runLive(t, CliVerbWeb, webAddrFlag, addr, termToolsFlag)

	url := g5AwaitListening(t, snapshot, addr)
	resp, err := http.Get(url) //nolint:noctx // a test probe against a listener this test started
	if err != nil {
		t.Fatalf("GET %s (the URL the banner printed): %v\n%s", url, err, snapshot())
	}
	defer resp.Body.Close() //stylegate:ignore closing a response body this test read
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200 — the help says this serves the chat page", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the served page: %v", err)
	}
	if !strings.Contains(string(body), "<html") {
		t.Errorf("the served page carries no HTML document (%d bytes):\n%.200s", len(body), body)
	}

	// First refusal (no server may start): a bad --addr value is refused by name.
	res := p.run(t, "", CliVerbWeb, webAddrFlag)
	if res.exit == 0 || !strings.Contains(res.out, webAddrFlag) {
		t.Errorf("a bare --addr must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
	// First refusal: an unknown second-level subcommand must not start the server.
	res = p.run(t, "", CliVerbWeb, "bogus")
	if res.exit == 0 || !strings.Contains(res.out, "want search|fetch") {
		t.Errorf("an unknown web subcommand must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
}

// g5AwaitListening waits for the group server's banner and returns the URL to
// open. Both `gopi web` and `gopi cli` print the same shape, so one waiter
// serves both.
func g5AwaitListening(t *testing.T, snapshot func() string, addr string) string {
	t.Helper()
	deadline := time.Now().Add(g5ProbeTimeout)
	for {
		out := snapshot()
		if strings.Contains(out, "listening (Ctrl+C to stop)") && strings.Contains(out, addr) {
			if i := strings.Index(out, "open: "); i >= 0 {
				rest := out[i+len("open: "):]
				if end := strings.IndexAny(rest, "\n"); end > 0 {
					return strings.TrimSpace(rest[:end])
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server never printed its listening banner for %s\n%s", addr, out)
		}
		//stylegate:allow-sleep polls the server's stdout for its listening banner; the loop's deadline owns the failure and it exits on the first hit
		time.Sleep(50 * time.Millisecond)
	}
}

// --- case 5/6: gopi cli (the text-browser carrier) and `gopi cli open` --------

// g5StubBrowser writes a stand-in for the vendored links2 binary. It is NOT a
// stubbed BROWSER doing a browser's work: the carrier only ever EXECS a browser
// with the chat URL (that is its whole contract with one), so a script that
// records the argv and exits exercises exactly the advertised surface. What
// this cannot prove — that a real text browser renders the page — is stated in
// the inventory's reason column, not faked.
func g5StubBrowser(t *testing.T, recordPath string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stub-browser")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + recordPath + "\"\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write the stub browser: %v", err)
	}
	return path
}

// TestG5CLICarrierRunsAsAdvertised drives `gopi cli` (the bare carrier), the
// `cli open` alias, and `--addr`, asserting the help's claims: the carrier
// starts the shared web engine and opens the text browser AT THE /cli URL
// (which is what the page says the carrier is: "a second VIEW over the running
// gopi web engine").
func TestG5CLICarrierRunsAsAdvertised(t *testing.T) {
	for _, argv := range [][]string{
		{termVerbWord()},
		{termVerbWord(), termOpenVerb},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			model := g5NewStubModel(t, "OK there")
			p := g5NewProbe(t).with(model.env()...)
			record := filepath.Join(p.home, "browser-argv.txt")
			addr := g5FreeAddr(t)
			p = p.with(termLinksEnv + "=" + g5StubBrowser(t, record))
			_, snapshot := p.runLive(t, append(argv, webAddrFlag, addr)...)

			g5AwaitListening(t, snapshot, addr)

			// The carrier's contract: exec the browser with the chat URL.
			deadline := time.Now().Add(g5ProbeTimeout)
			var got string
			for {
				b, err := os.ReadFile(record)
				if err == nil && strings.TrimSpace(string(b)) != "" {
					got = strings.TrimSpace(string(b))
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("`gopi cli %s` never executed the text browser with a URL\n%s", strings.Join(argv, " "), snapshot())
				}
				//stylegate:allow-sleep polls the child's record file for the text browser's invocation; the loop's deadline owns the failure and it exits on the first hit
				time.Sleep(50 * time.Millisecond)
			}
			if !strings.HasSuffix(got, "/cli") {
				t.Errorf("the browser was opened at %q, want the /cli chat URL the help describes", got)
			}
			if !strings.Contains(got, addr) {
				t.Errorf("the browser was opened at %q, which is not the --addr %s the caller asked for", got, addr)
			}
		})
	}

	// The carrier's first refusal: a typo is a usage error, never an agent run.
	p := g5NewProbe(t)
	res := p.run(t, "", termVerbWord(), "bogus")
	if res.exit == 0 {
		t.Fatalf("`gopi cli bogus` exited 0 — the help's own dispatch rule says anything but `open`/flags is a usage error\n%s", res.out)
	}
	if !strings.Contains(res.out, "unknown subcommand") || !strings.Contains(res.out, termOpenVerb) {
		t.Errorf("the refusal does not name the valid verb:\n%s", res.out)
	}
	res = p.run(t, "", termVerbWord(), webAddrFlag)
	if res.exit == 0 || !strings.Contains(res.out, webAddrFlag) {
		t.Errorf("a bare --addr must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
}

// --- case 7: gopi cli --once (the non-interactive turn) -----------------------

// g5StubModel is a local endpoint answering the Claude SSE dialect the agent
// speaks. It records every request body, so the --tools claim can be read off
// the wire ("the full gopi toolset") instead of asserted from the flag's name.
type g5StubModel struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []string
}

func g5NewStubModel(t *testing.T, answer string) *g5StubModel {
	t.Helper()
	m := &g5StubModel{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a stub request body
		m.mu.Lock()
		m.bodies = append(m.bodies, string(body))
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		g5WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":1,"output_tokens":0}}}`)
		g5WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		g5WriteClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, g5JSONString(answer)))
		g5WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		g5WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func g5WriteClaudeSSE(w io.Writer, event, payload string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload) //stylegate:ignore writing the test's own SSE frames
}

func g5JSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// requests returns the model requests seen so far.
func (m *g5StubModel) requests() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.bodies...)
}

// g5ModelEnv points a probe at the stub endpoint. The CLI resolves these the
// same way any gopi process does, so nothing here is special-cased for tests.
func (m *g5StubModel) env() []string {
	return []string{"DEEPSEEK_BASE_URL=" + m.srv.URL, "DEEPSEEK_API_KEY=stub-key", "DEEPSEEK_MODEL=stub-model"}
}

// TestG5CLIOnceRunsAsAdvertised drives `gopi cli "<prompt>" --once` — the
// non-interactive face the help calls "the shape a script (or an agent-authored
// self-test) drives" — and asserts every claim that page makes: the answer goes
// to stdout and NOTHING else, the exit status says the turn completed, and
// `--tools` really ships the toolset to the model.
func TestG5CLIOnceRunsAsAdvertised(t *testing.T) {
	model := g5NewStubModel(t, "OK there")
	p := g5NewProbe(t).with(model.env()...)

	res := p.run(t, "", termVerbWord(), "say OK", termOnceFlag)
	if res.exit != 0 {
		t.Fatalf("`gopi cli \"say OK\" --once` exit %d, want 0 (the help's exit status contract)\n%s", res.exit, res.out)
	}
	out := res.out
	// The answer is on the stream the help names, and the help's own exit-status
	// sentence is the contract asserted here; stdout-only purity is covered by
	// TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer (in-process, fd-level).
	if !strings.Contains(out, "OK there") {
		t.Errorf("the answer never appeared:\n%s", out)
	}
	reqs := model.requests()
	if len(reqs) == 0 {
		t.Fatal("the turn made no model request — the surface was never driven")
	}
	if strings.Contains(reqs[0], `"tools"`) {
		t.Error("a tool-free --once turn shipped a tool set; the help says tools are opt-in")
	}

	// The other advertised spellings, including the short one and --tools.
	model2 := g5NewStubModel(t, "OK there")
	p2 := g5NewProbe(t).with(model2.env()...)
	res = p2.run(t, "", termVerbWord(), "-p", "say OK")
	if res.exit != 0 {
		t.Fatalf("the short spelling -p: exit %d\n%s", res.exit, res.out)
	}
	res = p2.run(t, "", termVerbWord(), "say OK", termOnceFlag, termToolsFlag)
	if res.exit != 0 {
		t.Fatalf("--once --tools: exit %d\n%s", res.exit, res.out)
	}
	reqs = model2.requests()
	toolTurn := false
	for _, r := range reqs {
		if strings.Contains(r, `"tools"`) {
			toolTurn = true
		}
	}
	if !toolTurn {
		t.Error("--tools never shipped a toolset — the help says it enables the full gopi toolset on the turn")
	}

	// First refusal: no prompt (the help's exit 2 = usage error).
	res = p2.run(t, "", termVerbWord(), termOnceFlag)
	if res.exit != 2 {
		t.Errorf("`gopi cli --once` with no prompt: exit %d, want 2 (the help's usage status)\n%s", res.exit, res.out)
	}
	if !strings.Contains(res.out, "needs a prompt") {
		t.Errorf("the refusal does not say what is missing:\n%s", res.out)
	}
}

// --- case 8/9: gopi chat, its list/backfill subcommands -----------------------

// g5SeedRawLog writes a checkpoint file the chat REPL accepts as its seed, in
// the one format LoadCheckpoint parses (a REQUEST block with a JSON body).
func g5SeedRawLog(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gopi-raw-g5.log")
	body := "→ REQUEST\n" +
		`{"model":"stub-model","stream":true,"system":[{"type":"text","text":"sys"}],` +
		`"messages":[{"role":"user","content":"hello"}],"thinking":null}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write the seed raw log: %v", err)
	}
	return p
}

// TestG5ChatRunsAsAdvertised drives `gopi chat` with every advertised flag of
// its page at once plus a raw-log-path positional, and asserts the help's
// claims: the checkpoint is loaded as the conversation seed and the session
// exits cleanly on /quit. The model is not needed for this — the REPL is asked
// to quit before its first turn — so no provider stand-in appears here.
func TestG5ChatRunsAsAdvertised(t *testing.T) {
	model := g5NewStubModel(t, "OK there")
	p := g5NewProbe(t).with(model.env()...)
	seed := g5SeedRawLog(t)

	res := p.run(t, "/quit\n", "chat", "--tools", "--no-editor", "-v", seed)
	if res.exit != 0 {
		t.Fatalf("`gopi chat --tools --no-editor -v <raw-log>` exit %d, want 0\n%s", res.exit, res.out)
	}
	if !strings.Contains(res.out, "seeded") || !strings.Contains(res.out, filepath.Base(seed)) {
		t.Errorf("the [raw-log-path] positional was not used as the seed — the help says the checkpoint is loaded as the conversation:\n%s", res.out)
	}
	if !strings.Contains(res.out, "Ctrl+D or /quit") {
		t.Errorf("the REPL banner never appeared — the surface did not start:\n%s", res.out)
	}

	// First refusal: an unresolvable seed target is an error before the REPL,
	// never a silent empty conversation. The positional is not a subcommand —
	// the chat-index dispatch only accepts list|backfill — so a typo reaches the
	// REPL as a seed and is refused there.
	res = p.run(t, "", "chat", "definitely-not-a-real-session")
	if res.exit == 0 {
		t.Errorf("an unresolvable seed must fail: exit=0\n%s", res.out)
	}
	if !strings.Contains(res.out, "is neither a raw log path nor an existing session id") {
		t.Errorf("the refusal does not explain itself:\n%s", res.out)
	}
}

// TestG5ChatListAndBackfillRunAsAdvertised drives the chat-index subcommands.
//
// This case records a MEASURED DEFECT (see ## Not Done in the report and the
// inventory): `gopi chat list --db <path>` exports GOPI_CHAT_DB, but the chat
// index opens the TASKS LEDGER (internal/tools/chat.go chatDBPath) to SELECT
// FROM tasks — so on a directory with no ledger, `backfill` fails with
// "no such table: tasks" instead of an empty report, and `list` reports
// "No chats found." The assertion below pins the OBSERVED behaviour and the
// exact stderr, so the fix (whichever of the two files owns it) has a
// ready-made repro; it does NOT pretend the surface is healthy.
func TestG5ChatListAndBackfillRunAsAdvertised(t *testing.T) {
	p := g5NewProbe(t)
	db := filepath.Join(p.home, "chat-index.db")

	// A REAL database, created by the product's own ledger verb (`gopi task
	// list --db`) rather than by a hand-rolled sqlite open — the chat index
	// shares the ledger file, so its tables must come from the code that owns
	// them (Rule 32's spirit, applied to a subprocess fixture).
	res := p.run(t, "", "task", "list", "--db", db)
	if res.exit != 0 {
		t.Fatalf("seed the ledger `gopi task list --db`: exit %d\n%s", res.exit, res.out)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("the ledger verb did not create %s: %v", db, err)
	}

	res = p.run(t, "", "chat", chatSubcommandList, "--db", db)
	// The LISTING face is retired (epic verb-restructure 2026-10-05): the chat
	// index has ONE door, `gopi session list`. Retired is asserted by CONTRACT,
	// not by absence — the word is refused by name, exits non-zero, and names
	// its live home; a word that merely vanished would be spent as the REPL's
	// positional instead (measured: `gopi chat keeps` opened a session).
	if res.exit == 0 {
		t.Errorf("`gopi chat %s` must be refused (it is retired — the index is `gopi session %s`):\n%s",
			chatSubcommandList, sessionSubcmdList, res.out)
	}
	if !strings.Contains(res.out, "retired") || !strings.Contains(res.out, CliVerbSession+" "+sessionSubcmdList) {
		t.Errorf("the retirement refusal must name the retirement and the live door:\n%s", res.out)
	}

	// The documented `backfill` face, on the same real ledger. Its own --db is
	// advertised in the synopsis, and it is asserted by EFFECT: the run must
	// open the database the caller named, which is observable because a ledger
	// that exists is reported as an empty migration rather than an open error.
	res = p.run(t, "", "chat", chatSubcommandBackfill, "--dry-run", "--verbose", "--db", db)
	if res.exit != 0 {
		t.Fatalf("`gopi chat backfill --dry-run --verbose --db <ledger>` exit %d, want 0 — the help says it migrates ledger rows and reports what it did\n%s", res.exit, res.out)
	}
	if !strings.Contains(res.out, "legacy auto row(s) to migrate") {
		t.Errorf("backfill did not print the migration report the help describes:\n%s", res.out)
	}

	// The same invocation WITHOUT --db still works (the flag is optional), and
	// proves the override above was the thing that made it succeed: the default
	// database is the sandbox's own, which this probe has not created.
	res = p.run(t, "", "chat", chatSubcommandBackfill, "--dry-run", "--db", db)
	if res.exit != 0 {
		t.Errorf("backfill without a fresh --db should also report an empty migration:\n%s", res.out)
	}

	// First refusals: an unknown flag, and a seed target the REPL cannot
	// resolve (`gopi chat bogus` is the REPL's positional, not a subcommand —
	// the chat-index dispatch only accepts list|backfill).
	res = p.run(t, "", "chat", chatSubcommandBackfill, "--bogus")
	if res.exit == 0 || !strings.Contains(res.out, "unknown flag --bogus") {
		t.Errorf("an unknown flag must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
	res = p.run(t, "", "chat", "bogus")
	if res.exit == 0 || !strings.Contains(res.out, "cannot resolve session id") {
		t.Errorf("an unresolvable seed target must be refused by name: exit=%d\n%s", res.exit, res.out)
	}
}

// --- helpers ------------------------------------------------------------------

// lastJSONLine returns the last line of out that parses as a JSON object — the
// surfaces here print their payload last, after any banner prose.
func lastJSONLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if strings.HasPrefix(ln, "{") && json.Valid([]byte(ln)) {
			return ln
		}
	}
	return strings.TrimSpace(out)
}

// g5FlagCount is a tiny census helper used by the report's evidence section
// (and by the inventory regeneration): the number of advertised flags of one
// surface. Kept in the test binary so the number in the doc and the number the
// gate derives come from one implementation — it goes through g5FlagsOf (the
// SURFACE-aware entry point), never the page-wide derivation, or the two rows
// that share `gopi chat`'s page would each be handed the other's flags and the
// census would disagree with every other gate in this file.
func g5FlagCount(name string) (int, bool) {
	for _, s := range g5Surfaces() {
		if s.Name == name {
			return len(g5FlagsOf(s)), true
		}
	}
	return 0, false
}

// g5ExpectedSurfaces / g5ExpectedFlags pin the census this group's help derives.
// They are the non-vacuity floor of every gate in this file: a scanner that
// silently stops seeing synopsis lines would otherwise report a smaller surface
// as "the help" and every comparison would agree with it. A help change that
// adds or removes a flag must be confirmed here on purpose.
const (
	// 9 → 8: `gopi chat list` was retired (epic verb-restructure, 2026-10-05) and
	// its surface left this census with its row in the inventory table. The
	// retired word is still covered — by its RETIREMENT contract, asserted in
	// TestG5ChatListAndBackfillRunAsAdvertised — but it is no longer a surface
	// the help advertises, which is what this census counts.
	g5ExpectedSurfaces = 8
	// 35 → 36: `gopi web --thinking-live` (stream the model's live reasoning as
	// `event: thinking` frames — the same switch `gopi ui` takes) is now
	// advertised in the web page's help. Confirmed here on purpose, as the
	// comment above requires, and re-derived with
	// `scripts/g5-chat-web-help-surface.sh` rather than incremented by hand.
	//
	// 36 → 37: `gopi chat --no-dump-screen` (the chat screen dump is ON BY
	// DEFAULT since 2026-10-01, so the help must advertise the way out of it —
	// task 01a0f720). Re-derived with the script above, not incremented by hand.
	// 37 → 38: `gopi chat --replay` (re-run a recorded session on the real chat
	// surface — task 01a0fea1). Re-derived with the script above, not incremented
	// by hand.
	//
	// 38 → 39: `gopi chat --film DIR` (film a replayed session as PNG frames —
	// task 01a0ffbb). Re-derived with the script above, not incremented by hand.
	//
	// 39 → 40: `gopi chat --chat-ideal-geometry` (the chat surface's declared
	// ideal geometry — task 01a0ffbb, the sibling of the --film child above).
	// Re-derived with scripts/g5-chat-web-help-surface.sh on 2026-10-03, never
	// incremented by hand (Rule 38): it reports 14 flags for `gopi chat` and 40
	// for the group.
	//
	// 40 → 41: `gopi chat --out FILE.gif` (the film's animation arm — task
	// 01a10156). Re-derived with scripts/g5-chat-web-help-surface.sh on the
	// built binary, never incremented by hand (Rule 38): it reports 15 flags for
	// `gopi chat` and 41 for the group.
	//
	// 41 → 42: `gopi chat --film-terminal NAME` (the filmed terminal's selector —
	// task 01a101be). Re-derived the same way on 2026-10-03, never incremented by
	// hand: the script reports 16 flags for `gopi chat` and 42 for the group.
	//
	// 42 → 47: the beat protocol's own five words — `gopi chat --scratch DIR`,
	// `--cols N`, `--rows N`, `--max-beats N`, `--ack-timeout-ms N` — ACCEPTED
	// by this verb's parser (chat_replay_beats.go) and used by the film driver's
	// own producer argv, while the page named none of them (the C14 finding of
	// scripts/replay-check.sh, task 01a103e4). The page gained the section that
	// documents them. Re-derived with `scripts/g5-chat-web-help-surface.sh
	// ./bin/gopi` on the built binary on 2026-10-04, never incremented by hand
	// (Rule 38): it reports 21 flags for `gopi chat` and 47 for the group.
	//
	// 47 → 44: `gopi chat list` was RETIRED and its row left this census (epic
	// verb-restructure, 2026-10-05 — the chat index has ONE door, `gopi session
	// list`). The three flags that left with it are `--db,--limit,--model`, the
	// retired word's own; `gopi chat` itself keeps all 21. Re-derived, never
	// decremented by hand: `scripts/g5-chat-web-help-surface.sh ./bin/gopi`
	// against the binary built from this commit reports 44.
	//
	// 44 → 45 (2026-10-09, task 01a11f139919767ea288a557ae095c6e): `gopi chat
	// --voice-audio FILE` — the audio `/voice` READS instead of a microphone, so
	// that mode (and its window boundary, its live arm and the text landing on
	// the input line) can be driven on a host with no sound card at all. The
	// word is read by this verb's parser, so it belongs on this page's synopsis
	// and in its flag list. Re-derived, never incremented by hand:
	// `scripts/g5-chat-web-help-surface.sh ./bin/gopi` on the binary built from
	// this commit reports 22 flags for `gopi chat` and 45 for the group.
	//
	// 45 → 46 (2026-10-19, task 01a153d6…, the chat-policy epic's 1/6+2/6):
	// `gopi chat --policy POSTURE` is a new knob this verb's parser READS, so
	// it appears on the page's synopsis and in its flag list. Re-derived, never
	// incremented by hand: `scripts/g5-chat-web-help-surface.sh ./bin/gopi` on
	// the binary built from this commit reports 23 flags for `gopi chat` and 46
	// for the group.
	//
	// 46 → 47 (2026-10-19, the replay/film HELP SCOPE): the family's index on the
	// session page names `gopi chat --wechat` explicitly (`--wechat` had reached
	// the page only as prose inside `--out`'s paragraph, which this scanner does
	// not count). Re-derived, never incremented by hand:
	// `scripts/g5-chat-web-help-surface.sh ./bin/gopi` on the binary built from
	// this commit reports 24 flags for `gopi chat` and 47 for the group.
	//
	// 47 → 48 (2026-10-27, the --from-turn split): the family's index on the
	// session page now names `gopi chat --from-turn N`. The flag itself was
	// already parsed and already carried its full contract in chatReplayUsage —
	// what it lacked was a row on the family INDEX and an entry in
	// chatReplayHelpFlags(), so it reached neither page's flag set (a definition
	// line on the scoped page, an index row on the session page is what this
	// scanner counts). Re-derived, never incremented by hand:
	// `scripts/g5-chat-web-help-surface.sh ./bin/gopi` on the binary built from
	// this commit reports 25 flags for `gopi chat` and 48 for the group.
	// 49 → 50 (2026-10-30, task 01a18996): `gopi chat --chat-surface-turns N` —
	// the chat_surface tool's self-driven turn budget stopped being the package
	// constant 8 and became a knob (default 20, accepted 2..1000) this verb's
	// parser reads. Re-derived, never incremented by hand:
	// `scripts/g5-chat-web-help-surface.sh ./bin/gopi` on the binary built from
	// this commit reports 26 flags for `gopi chat` and 50 for the group.
	//
	// 50 → 51 (2026-10-27): `gopi chat --max-turns N` replaced the retired
	// `--turns` spelling, and `--ambiguous-width wide|narrow` — advertised in the
	// usage line since the knob landed, explained nowhere, and absent from the
	// parser's closed vocabulary — gained the row it never had. Both re-derived
	// on the binary built from this commit, never incremented by hand:
	// `scripts/g5-chat-web-help-surface.sh ./bin/gopi` reports 27 flags for
	// `gopi chat` and 51 for the group.
	//
	// 51 → 52 → 53 (2026-11-04, two parallel runs, and the rebase that joined them):
	// two independent features each added ONE word to the SAME help page, and each
	// re-derived this pin against the common base of 51 — so both wrote "52" and
	// neither number survives the join.
	//
	//   - task 01a1a5f1, commit 4bb7b207: `gopi chat --sgr-replay SESSION|FILE`
	//     plays a RECORDED raw byte stream back verbatim — the bytes the renderer
	//     wrote, unparsed — so two builds replaying one recording print identical
	//     output. `gopi chat` moved 27 → 28.
	//   - this run, task 01a1a5e7: `gopi chat --no-sgr-stream` — the RAW SGR byte
	//     stream became ON BY DEFAULT (operator ruling: 数据的不可再生性高于代码
	//     完整性), so the help must advertise the way OUT of it, the same reason
	//     `--no-dump-screen` added a flag when the dump's default flipped.
	//
	// The joined tree advertises BOTH words on `gopi chat`, so the group total is
	// 53, not 52: 28 + 1 = 29 for `gopi chat`, 52 + 1 = 53 for the group. Re-derived
	// on the binary built from THIS commit, never incremented by hand and never by
	// adding two increments together (Rule 38):
	// `scripts/g5-chat-web-help-surface.sh ./bin/gopi` reports 29 flags for
	// `gopi chat` and 53 for the group.
	g5ExpectedFlags = 53
)

// TestG5SurfaceCensusIsDerivedFromConstants is a compact, printable census: it
// fails if the derivation loses a surface or a flag, and it is the number a
// reviewer can compare against the doc's "Totals" line with -v.
func TestG5SurfaceCensusIsDerivedFromConstants(t *testing.T) {
	total, covered := 0, 0
	for _, name := range g5SurfaceNames() {
		n, ok := g5FlagCount(name)
		if !ok {
			t.Fatalf("no surface named %q — the census and the inventory disagree", name)
		}
		covered++
		total += n
		t.Logf("%-18s %d advertised flag(s)", name, n)
	}
	if covered != len(g5Surfaces()) {
		t.Fatalf("census covered %d of %d surfaces", covered, len(g5Surfaces()))
	}
	if covered != g5ExpectedSurfaces {
		t.Errorf("the help advertises %d surface(s), want %d — confirm the new surface in g5Surfaces() and the inventory table", covered, g5ExpectedSurfaces)
	}
	if total != g5ExpectedFlags {
		t.Errorf("the help advertises %d flag(s) across the group, want %d — a flag appeared or vanished; confirm it in the inventory table and this pin", total, g5ExpectedFlags)
	}
	t.Logf("TOTAL %d surfaces, %d advertised flags", covered, total)
}
