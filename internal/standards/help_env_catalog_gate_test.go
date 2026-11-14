package standardgate

// The advertised-env-name gate: every env-shaped name a USAGE/HELP literal
// names must be a row of internal/config's EnvVars catalog — or be recorded,
// by name, in the exemption list below.
//
// WHY IT EXISTS (the defect it makes unrepresentable, measured 2026-09-28,
// task 01a0ece4-a3ba-7a79-8f76-7cf55a49b1d6). `gopi task -h` advertised
//
//	Flags: --min-age-h (default 24, env GOPI_WORKTREE_SWEEP_MIN_AGE_H),
//	       --max (default 50, env GOPI_WORKTREE_SWEEP_MAX)
//
// while NO row in internal/config.EnvVars carried either name — so
// `gopi --help-env`, the surface that paragraph's own reader is pointed at
// ("the third column is the CLI flag … each knob is reachable as an
// environment variable"), documented neither, CONFIG.md tabulated neither, and
// no gate could see it: the CONFIG.md document gate asserted the doc and the
// catalog agree, which a name in NEITHER satisfies perfectly. The knob worked
// (its reader is internal/tools' worktreeSweepOptionsFrom); only the
// operator-visible INDEX was missing it, and an index that omits a knob is how
// an operator concludes the knob does not exist.
//
// WHY THE GATE IS ABOUT THE ADVERTISEMENT, NOT THE CATALOG. A catalog row is a
// statement; a help page is a PROMISE — the page tells a reader "set this
// variable". So the direction asserted here is the one a reader can act on:
// advertised ⇒ catalogued. The reverse is deliberately not asserted (a catalog
// row need not appear on a subcommand page — `gopi --help-env` documents it,
// and internal/config's TestTopLevelHelpExposesEveryUserFacingKnob owns the
// front-door question with its own rule).
//
// RED PROOF: the predicate below is a pure function over (source text, catalog),
// so the fixture half of this file feeds it a usage literal advertising a name
// the catalog does not carry and demands a finding — the same code path the
// real-tree assertion runs. Deleting the catalog row a page advertises turns
// the real-tree test red naming that name and page.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// helpEnvUsageNameMarker is what makes a string-literal declaration a
	// USAGE/HELP literal: the identifier holds this word (case-insensitively).
	// That is the spelling the tree already uses for every help page — `usage`,
	// `*Usage`, `*UsageHead`, `tasksUsage`, `workerUsageHead` — so the rule
	// needs no hand-kept list of page variables, and a page added tomorrow is
	// covered the moment it follows the convention.
	helpEnvUsageNameMarker = "usage"
	// helpEnvHelpNameMarker is its sibling: the internal/config help surface
	// names its declarations `*Help*`.
	helpEnvHelpNameMarker = "help"
	// helpEnvMinAdvertised is the read-floor: a scan returning fewer
	// env-shaped names than this has lost the literals it was pointed at (a
	// changed declaration shape, a re-wrapped page) and must fail loud rather
	// than read green over an empty set.
	helpEnvMinAdvertised = 4
	// helpEnvMinSources is the read-floor for the file walk itself: the two
	// trees below hold dozens of *Usage* declarations today; a walk that finds
	// none of their files is broken, not clean.
	helpEnvMinSources = 2
)

// helpEnvScanRoots are the trees whose usage/help literals are scanned:
// cmd/agent owns the subcommand pages (`gopi task -h`, `gopi worker -h`), and
// internal/config owns the front-door and env-table renderers. Both are
// scanned as PRODUCTION files only — the same rule every other gate applies, so
// a fixture's own advertised name is not scored as an operator promise.
var helpEnvScanRoots = []string{"cmd/agent", "internal/config"}

// helpEnvDeclPattern matches a string-literal declaration: `Name = ` followed
// by a backtick or a double quote, with an optional concatenation prefix
// (`chatUsage = ` + "`" + `x` + "`" + ` + chatOnceUsage`).
var helpEnvDeclPattern = regexp.MustCompile("(?m)^[ \t]*(?:const|var)?[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[ \t]*(`|\")")

// helpEnvNamePattern is the join key: an env-shaped name inside a usage
// literal. It is the operator-visible spelling, so it is what a reader acts on.
var helpEnvNamePattern = regexp.MustCompile(`\b(GOPI|ANTHROPIC|DEEPSEEK)_[A-Z0-9_]+\b`)

// helpEnvCatalogExempt records the env-shaped names a usage/help literal may
// name WITHOUT a catalog row, each with its reason. It is a decision list, not
// a silencer: the gate fails when an entry stops being advertised (a stale
// permission) and when an entry gains a catalog row (the exemption must then be
// deleted in the same commit).
//
// Both entries name constants rather than re-typing the spelling (Rule 1b/13):
// config.AgencyEnv and config.HumanTerminalEnv are the ONE definitions of those
// two names.
var helpEnvCatalogExempt = map[string]string{
	// The improvement-proposal switch. It is advertised in the `gopi task
	// improvements` paragraph precisely to tell the reader the switch is NOT an
	// operator knob (`REPORT-ONLY face … Writes nothing`), and CONFIG.md §2f
	// ("A/B and measurement switches") documents it as deliberately outside the
	// catalog: the pass exists so a measurement can be armed without editing
	// code.
	config.AgencyEnv: "advertised as a switch the READER does not own — " +
		"CONFIG.md §2f records it as deliberately outside the catalog",
	// The operator's own negative signature, named on the page so the reader
	// knows a row's `human` source is derived from a process with no agent
	// chain above it. It is an ATTRIBUTION claim the agent cannot forge, never
	// a knob: cataloguing it would advertise it as settable.
	config.HumanTerminalEnv: "an attribution claim (the operator's negative " +
		"signature), not a settable knob — the page names it to explain `--source`",
	// The PRE-RENAME names of the audio configuration surface (2026-10-09:
	// `voice_engine` → `audio_engine`, the flat `doubao_*` keys →
	// `audio_engine_param.<engine>.<knob>`). They are named on the voice page
	// for the ONE reason this list exists: the reader who has not migrated has
	// to be able to recognise the variable they already export, and be told it
	// is still read. A catalog row is wrong for them in both directions —
	// `gopi --help-env` renders FROM the catalog, so a row would advertise a
	// retired name as the knob to SET (and CONFIG.md would tabulate it as one),
	// while the CURRENT spelling's own row is the promise this page must keep.
	// Rule 2 is satisfied without one: both names are read at the SAME rung by
	// the ONE resolver that owns the knob (config.ResolveAudioRecording), and
	// config.AudioLegacyEnvNote reports the migration per run.
	config.AudioEngineLegacyEnv: "a PRE-RENAME alias, still read at the same rung as " +
		"`" + config.AudioEngineEnv + "` — named on the page so an unmigrated reader " +
		"recognises it; a catalog row would advertise the retired name as the one to set",
	config.DoubaoKeyFileEnv: "the PRE-RENAME alias of `" + config.AudioKeyFileEnv +
		"`, still read at the same rung by the ONE credential resolver — same reason " +
		"as the engine selector's alias above",
}

// TestHelpAdvertisedEnvNamesAreCatalogued is the assertion: every env-shaped
// name a usage/help literal names is a catalog row, or is exempted above by
// name with a reason.
func TestHelpAdvertisedEnvNamesAreCatalogued(t *testing.T) {
	root := repoRoot(t)
	files := helpEnvSourceFiles(t, root)
	if len(files) < helpEnvMinSources {
		t.Fatalf("the help-source walk found %d file(s) (floor %d) — the scan is pointed at nothing",
			len(files), helpEnvMinSources)
	}
	catalog := helpEnvCatalogNames()
	advertised := map[string][]string{}
	for _, rel := range files {
		for _, name := range helpEnvAdvertisedNames(readFileString(t, filepath.Join(root, filepath.FromSlash(rel)))) {
			advertised[name] = append(advertised[name], rel)
		}
	}
	if len(advertised) < helpEnvMinAdvertised {
		t.Fatalf("the scan found only %d advertised env name(s) (floor %d) — a page literal was lost "+
			"or reshaped, and this gate must fail rather than read green over an empty set: %v",
			len(advertised), helpEnvMinAdvertised, helpEnvSortedKeys(advertised))
	}

	var missing []string
	for name, where := range advertised {
		if catalog[name] {
			continue
		}
		if _, exempt := helpEnvCatalogExempt[name]; exempt {
			continue
		}
		missing = append(missing, name+" (advertised in "+strings.Join(helpEnvUniq(where), ", ")+")")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d env name(s) are advertised by a usage/help literal but are NOT in the "+
			"internal/config EnvVars catalog:\n  %s\n"+
			"A page advertising a variable is a promise: the reader sets it and expects it to be "+
			"documented. `gopi --help-env` renders FROM the catalog, so an uncatalogued name is "+
			"absent from the very surface the page points at, and CONFIG.md cannot tabulate it "+
			"either (the CONFIG.md document gate checked the doc against the catalog, which "+
			"such a name satisfies in NEITHER). Add the catalog row — and its CONFIG.md line — or record "+
			"the name in helpEnvCatalogExempt with the reason it is not an operator knob.",
			len(missing), strings.Join(missing, "\n  "))
	}

	// The exemption list ratchets in both directions: an entry whose name is no
	// longer advertised is a stale permission, and one that HAS a catalog row is
	// a decision that has been overtaken (delete it in the same commit).
	for name, reason := range helpEnvCatalogExempt {
		if _, ok := advertised[name]; !ok {
			t.Errorf("helpEnvCatalogExempt lists %q, which no usage/help literal advertises any more — "+
				"remove the stale exemption (recorded reason: %s)", name, reason)
		}
		if catalog[name] {
			t.Errorf("helpEnvCatalogExempt lists %q, but the catalog now carries a row for it — "+
				"the exemption is overtaken; delete it (recorded reason: %s)", name, reason)
		}
	}
}

// helpEnvAdvertisedNames returns every env-shaped name inside the USAGE/HELP
// string literals of one source text, sorted and deduplicated. It is a pure
// function over text so the red proofs below run the production code path.
//
// WHAT IS SCANNED, and why it is a convention rather than a list: a string
// declaration whose IDENTIFIER carries "usage" or "help" is a page the operator
// reads. Comments are NOT scanned (a doc comment mentioning a variable is
// prose about the code, not a promise to the reader) — the declaration form is
// what the help renderers actually print.
func helpEnvAdvertisedNames(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range helpEnvDeclPattern.FindAllStringSubmatchIndex(src, -1) {
		ident := src[m[2]:m[3]]
		lower := strings.ToLower(ident)
		if !strings.Contains(lower, helpEnvUsageNameMarker) && !strings.Contains(lower, helpEnvHelpNameMarker) {
			continue
		}
		lit := helpEnvLiteral(src, m[4])
		if lit == "" {
			continue
		}
		for _, name := range helpEnvNamePattern.FindAllString(lit, -1) {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// helpEnvLiteral returns the string literal starting at index i (which points
// at its opening backtick or quote). A raw literal ends at its closing backtick;
// an interpreted one ends at the first unescaped quote. A literal that never
// closes on the same text returns "" — the declaration shape was not one this
// gate can read, and guessing would invent findings.
func helpEnvLiteral(src string, i int) string {
	quote := src[i]
	if quote == '`' {
		end := strings.IndexByte(src[i+1:], '`')
		if end < 0 {
			return ""
		}
		return src[i : i+1+end+1]
	}
	for j := i + 1; j < len(src); j++ {
		if src[j] == '\\' {
			j++
			continue
		}
		if src[j] == quote {
			return src[i : j+1]
		}
		if src[j] == '\n' {
			return "" // an interpreted literal never spans a line
		}
	}
	return ""
}

// helpEnvCatalogNames is the catalog's own set, read from the live table rather
// than re-parsed from its source: the far side of this comparison must be the
// same data `gopi --help-env` renders, so a row that reaches the table is
// catalogued by construction.
func helpEnvCatalogNames() map[string]bool {
	out := make(map[string]bool, len(config.EnvVars))
	for _, e := range config.EnvVars {
		if e.Env != "" {
			out[e.Env] = true
		}
	}
	return out
}

// helpEnvSourceFiles lists the production .go files under helpEnvScanRoots.
func helpEnvSourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dir := range helpEnvScanRoots {
		base := filepath.Join(root, filepath.FromSlash(dir))
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	sort.Strings(out)
	return out
}

// helpEnvSortedKeys returns a map's keys sorted, for a diagnosis line.
func helpEnvSortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// helpEnvUniq deduplicates a list, keeping first-appearance order.
func helpEnvUniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// --- red proofs ---------------------------------------------------------------
//
// A green run over the real tree could equally mean the predicate is blind.
// Each proof feeds the SAME function a poisoned fixture and demands the finding,
// plus a control that the fixture's honest twin yields none — 原则无效，检查才有效.

func TestHelpEnvCatalogRedProofUncataloguedAdvertisement(t *testing.T) {
	// A page advertising a variable the catalog does not carry: the finding
	// must name it. (This is the exact shape of the defect that opened the
	// file — the sweep-worktrees paragraph, before its catalog rows existed.)
	bad := helpEnvAdvertisedNames("const fakeUsage = `Flags: --min-age-h (default 24, env GOPI_NOT_A_REAL_KNOB)`\n")
	if len(bad) != 1 || bad[0] != "GOPI_NOT_A_REAL_KNOB" {
		t.Fatalf("red proof failed: the fixture yielded %v, want exactly GOPI_NOT_A_REAL_KNOB", bad)
	}
	if helpEnvCatalogNames()[bad[0]] {
		t.Fatalf("the fixture's name IS catalogued — pick a name the catalog does not carry: %s", bad[0])
	}

	// The control: the same page naming a REAL catalogued knob is clean.
	real := config.WorktreeSweepMinAgeEnvName
	control := helpEnvAdvertisedNames("const fakeUsage = `Flags: --min-age-h (default 24, env " + real + ")`\n")
	if len(control) != 1 || control[0] != real {
		t.Fatalf("control failed: the fixture yielded %v, want exactly %s", control, real)
	}
	if !helpEnvCatalogNames()[real] {
		t.Fatalf("the control's name %s is not catalogued — this gate would then be asserting nothing", real)
	}

	// A name in a COMMENT is not an advertisement: the page is what the
	// renderers print, so prose about the code must not be scored.
	commented := helpEnvAdvertisedNames("// fakeUsage mentions GOPI_NOT_A_REAL_KNOB in a comment\nconst other = `plain`\n")
	if len(commented) != 0 {
		t.Fatalf("a commented mention was scored as an advertisement: %v", commented)
	}

	// The help-named sibling is scanned too (internal/config's help renderers).
	helpNamed := helpEnvAdvertisedNames("var fakeHelpText = \"env GOPI_NOT_A_REAL_KNOB\"\n")
	if len(helpNamed) != 1 || helpNamed[0] != "GOPI_NOT_A_REAL_KNOB" {
		t.Fatalf("a *Help*-named literal was not scanned: %v", helpNamed)
	}
}

func TestHelpEnvCatalogRedProofTheExemptionsAreLive(t *testing.T) {
	// The two exempt names must still be advertised by some usage literal and
	// must still be absent from the catalog — otherwise the exemption list has
	// gone stale in one direction or the other, which is the state this gate
	// exists to refuse (a permission nobody re-read).
	root := repoRoot(t)
	catalog := helpEnvCatalogNames()
	advertised := map[string]bool{}
	for _, rel := range helpEnvSourceFiles(t, root) {
		for _, n := range helpEnvAdvertisedNames(readFileString(t, filepath.Join(root, filepath.FromSlash(rel)))) {
			advertised[n] = true
		}
	}
	for _, name := range []string{config.AgencyEnv, config.HumanTerminalEnv} {
		if !advertised[name] {
			t.Errorf("%s is exempted but no usage/help literal advertises it — the exemption is dead weight", name)
		}
		if catalog[name] {
			t.Errorf("%s is exempted but the catalog carries a row for it — delete the exemption", name)
		}
	}
}
