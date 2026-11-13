package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The surfaces `gopi memory brew` advertises its flag set on, and the one that
// ENFORCES it, are three different places. Until 2026-11-05 they disagreed in
// both directions at once:
//
//   - the MEMORY GROUP page (`gopi memory -h`) advertised
//     `[--since DURATION] [--verbose]` — two flags this verb has never parsed,
//     borrowed from the neighbouring `memory sync` / `task sync` pages;
//   - the verb's own page (`gopi memory brew -h`) listed the real five;
//   - and the parser was PERMISSIVE (no expect()/boolean() declaration), so
//     `gopi memory brew --sinc 2099-01-01` was ACCEPTED and the pipeline ran —
//     paying for a full LLM sweep on a typo, which is the expensive half.
//
// These tests pin all three to ONE vocabulary: the flag names in cli_brew.go,
// which the parser table and both help pages are built from.

// brewSynopsisFlags returns the `--flag` tokens of a synopsis line, sorted and
// deduplicated, with any `=VALUE`/` N` operand left behind. The line is the
// unit, so a wrapped synopsis must be joined by the caller.
func brewSynopsisFlags(line string) []string {
	re := regexp.MustCompile(`--([a-z][a-z-]*)`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(line, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// brewGroupPageSynopsis returns the ONE line the memory group page devotes to
// this verb's synopsis — `  gopi memory brew […]` inside memoryUsage. The
// group page carries a second mention (`'gopi memory brew -h' carries the flag
// set`), which is prose and carries no bracket, so the bracket is the
// discriminator.
func brewGroupPageSynopsis(t *testing.T) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^  gopi memory ` + regexp.QuoteMeta(CliVerbBrew) + ` \[.*$`)
	m := re.FindString(memoryUsage)
	if m == "" {
		t.Fatalf("the memory group page carries no synopsis line for %q — the verb a reader can only find by knowing its name", CliVerbBrew)
	}
	return m
}

// brewUsageLine returns the Usage line of the verb's own page, without the
// two-space indent the page renders.
func brewUsageLine(t *testing.T) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(brewSynopsis) + `$`)
	if !re.MatchString(brewUsage) {
		t.Fatalf("brewUsage does not render the shared synopsis %q — the page and the constant have diverged:\n%s", brewSynopsis, brewUsage)
	}
	return brewSynopsis
}

// TestBrewFlagSurfaceIsOneVocabularyInThreePlaces is the regression guard for
// the defect above: the flag set the GROUP page prints, the flag set the VERB
// page prints, and the set the PARSER declares must be the same five flags.
//
// RED BEFORE THE FIX, twice over and independently:
//   - the group page named `since` and `verbose`, which appear on neither of
//     the other two surfaces (so this test fails on the very first comparison);
//   - the parser declared nothing at all, so expect/boolean produced an empty
//     set behind a permissive parser that accepted every typo.
func TestBrewFlagSurfaceIsOneVocabularyInThreePlaces(t *testing.T) {
	group := brewSynopsisFlags(brewGroupPageSynopsis(t))
	verb := brewSynopsisFlags(brewUsageLine(t))
	parsed := brewSynopsisFlags(brewSynopsis)
	declared := append(append([]string{}, brewBooleanFlagNames...), brewValueFlagNames...)
	sort.Strings(declared)

	for _, cmp := range []struct {
		name string
		got  []string
	}{
		{"memory group page (`gopi memory -h`)", group},
		{"verb page (`gopi memory brew -h`)", verb},
		{"shared synopsis constant (brewSynopsis)", parsed},
		{"parser declaration (newBrewFlagSet)", declared},
	} {
		if got, want := strings.Join(cmp.got, ","), strings.Join(declared, ","); got != want {
			t.Errorf("%s advertises/declares {%s}, but the parser declares {%s} — a page may not promise a flag the parser refuses, and the parser may not accept one the page never mentions", cmp.name, got, want)
		}
	}

	// A floor, so a scanner that stopped finding flags cannot pass as a clean
	// surface: the verb has five flags and the assertion above would be vacuous
	// over an empty list.
	if len(declared) != 5 {
		t.Fatalf("the brew flag set is %d flags {%s}, want the measured five — if this is a deliberate change, move the pages with it", len(declared), strings.Join(declared, ","))
	}
}

// TestBrewFlagSurfaceTokensAreTheOnesADocumentPromises names the flags
// literally, so the shared-constant indirection above cannot hide a rename: a
// reader of the group page was promised `--dry-run/--limit/--session/--dir/
// --json` and must keep getting exactly those.
func TestBrewFlagSurfaceTokensAreTheOnesADocumentPromises(t *testing.T) {
	want := []string{brewFlagDir, brewFlagDryRun, brewFlagJSON, brewFlagLimit, brewFlagSession}
	got := brewSynopsisFlags(brewGroupPageSynopsis(t))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the group page prints flags {%s}, want {%s}", strings.Join(got, ","), strings.Join(want, ","))
	}
	// The two flags the page used to advertise are gone from EVERY surface.
	// `--since` belongs to memory sync / task sync and `--verbose` to chat;
	// brew has never parsed either, so naming them here is a promise the CLI
	// breaks on the next keystroke.
	for _, dead := range []string{"since", "verbose"} {
		for _, s := range []string{brewSynopsis, brewGroupPageSynopsis(t), brewUsage} {
			if strings.Contains(s, "--"+dead) {
				t.Errorf("`--%s` is still advertised on a brew surface, though the parser does not accept it:\n%s", dead, s)
			}
		}
	}
}

// TestBrewFlagSurfaceRefusesUnknownFlags is the PARSER half: strict mode is the
// only thing that makes an unknown flag cost nothing. Each case is a spelling
// that a user types by accident, and every one of them used to be ACCEPTED and
// followed by a real LLM sweep.
func TestBrewFlagSurfaceRefusesUnknownFlags(t *testing.T) {
	for _, bad := range [][]string{
		{"--sinc", "2099-01-01"},  // one keystroke from --since, which is not ours anyway
		{"--since", "24h"},        // the group page used to advertise exactly this
		{"--verbose"},             // …and this
		{"--dry-runn"},            // a typo of a REAL flag: the most likely one
		{"--dry-run", "--limitt"}, // a legal flag plus a typo of a second one
	} {
		_, err := newBrewFlagSet().parse(bad)
		if err == nil {
			t.Errorf("brew accepted %v — an unknown flag must be REFUSED before the engine is built (a typo used to cost a full LLM sweep)", bad)
			continue
		}
		if !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("brew's refusal of %v says %q, want it to name the unknown flag (Rule 15)", bad, err)
		}
	}
	// POSITIVE CONTROL: the same parser admits a declared flag. Without this,
	// the assertions above would pass against a parser that refuses everything.
	if _, err := newBrewFlagSet().parse([]string{"--json"}); err != nil {
		t.Errorf("control: --json is a declared flag, parse refused it: %v", err)
	}
}

// brewFixtureDir / brewFixtureSession are the sample VALUES the documented
// spellings are driven with. They are declared here, with a name, rather than
// inlined at the call site: what this arm pins is the FLAG VOCABULARY the verb
// page prints (`--dir PATH`, `--session PATH`), not any particular host path,
// so the value is arbitrary and the gate's fixture topology asks for exactly
// this shape — one declared home instead of a bare host literal at the site
// (internal/standards/worktree_root_gate.go, `worktreeFixtureDeclaredValue`).
const (
	brewFixtureDir     = "/tmp"
	brewFixtureSession = "/tmp/s.jsonl"
)

// TestBrewFlagSurfaceAdmitsTheDocumentedSpellings is the reverse arm: strict
// mode must not break a single legal invocation. Each spelling here is one the
// verb page prints, in the shape the page prints it.
func TestBrewFlagSurfaceAdmitsTheDocumentedSpellings(t *testing.T) {
	fs := newBrewFlagSet()
	pos, err := fs.parse([]string{
		"--dry-run", "--limit", "3", "--session", brewFixtureSession, "--dir", brewFixtureDir, "--json",
	})
	if err != nil {
		t.Fatalf("the documented flag set was refused: %v", err)
	}
	if len(pos) != 0 {
		t.Errorf("parse returned positionals %v, want none", pos)
	}
	if !fs.has(brewFlagDryRun) || !fs.has(brewFlagJSON) {
		t.Errorf("the boolean flags did not register: dry-run=%v json=%v", fs.has(brewFlagDryRun), fs.has(brewFlagJSON))
	}
	if fs.get(brewFlagLimit) != "3" || fs.get(brewFlagDir) != brewFixtureDir || fs.get(brewFlagSession) != brewFixtureSession {
		t.Errorf("values landed wrong: limit=%q dir=%q session=%q",
			fs.get(brewFlagLimit), fs.get(brewFlagDir), fs.get(brewFlagSession))
	}
}
