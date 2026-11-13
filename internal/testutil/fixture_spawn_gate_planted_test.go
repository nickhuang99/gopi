package testutil

// The RED PROOF for the widened recognizer (task 01a1888d): the gate's own
// predicates are run against a PLANTED tree that carries the spellings which used
// to be invisible, so "the gate is green on the real tree" can never be a green
// about a scanner that cannot see this shape at all.
//
// What the planted tree carries, and why each row is here:
//
//   - a bare `"sh"` hold and a bare `"bash"` hold — the measured false negative.
//     A `sleep 60` was found standing inside the checkout (PPid=systemd, its test
//     binary gone) while this gate read `offending 0` on that very tree, because
//     the shell arm accepted only the ABSOLUTE `"/bin/(sh|bash)"`.
//   - a `/bin/sleep` hold — the SAME hole on the direct arm: the program was
//     matched only as the bare `"sleep"`, so an absolute path was invisible.
//   - a `sh -c "exit 0"` control — a shell spawn with NO hold is not a fixture,
//     and the widened arm must not start reporting short-lived shells (a gate
//     that over-reports is as unusable as one that under-reports).
//   - a covered bare-`"bash"` hold — the shape the real tree now uses; it must be
//     judged, and judged COVERED, so widening did not turn every shell into a
//     violation.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// sleepFixtureOldShellSpawnSpelling is the PRE-WIDENING shell arm, kept here
	// ONLY as the negative control: the red proof must show the planted bare-shell
	// holds were invisible to it (0 matches) while they are visible now (2 sites).
	// It is deliberately the old regex — a proof that quoted the NEW one would
	// prove nothing about the hole that was repaired.
	sleepFixtureOldShellSpawnSpelling = `exec\.(Command|CommandContext)\(\s*"/bin/(sh|bash)"`
	// sleepFixtureOldDirectSpawnSpelling is the pre-widening direct arm, whose
	// program alternatives are all BARE names — so an absolute `/bin/sleep` was
	// invisible to it too.
	sleepFixtureOldDirectSpawnSpelling = `exec\.(Command|CommandContext)\(\s*("sleep"|MockDaemonBinary|testDaemonSleepArg|sleepFixtureName)`
	// sleepFixturePlantedFile is the file name of the planted fixture inside the
	// temp tree (the walker only reads `_test.go` files).
	sleepFixturePlantedFile = "planted_fixture_test.go"
	// sleepFixturePlantedWantSites is how many long-lived holds the planted tree
	// carries: two bare shells, one absolute-path sleep. The shell with no hold is
	// out of scope by its duration, so it must NOT be counted.
	sleepFixturePlantedWantSites = 3
	// sleepFixturePlantedWantOffenders is how many of those carry no cover.
	sleepFixturePlantedWantOffenders = 3
	// sleepFixturePlantedBashLine is the planted tree's bare-`"bash"` hold: the
	// measured survivor's spelling (a `sleep 60` standing in the checkout while
	// this gate read `offending 0`).
	sleepFixturePlantedBashLine = 10
)

// plantedFixtureSource is the planted tree's content. It is written as Go source
// rather than built with exec so the proof is about the RECOGNIZER, never about
// this host's ability to spawn anything.
const plantedFixtureSource = `package planted

import "os/exec"

func bareShellHold() *exec.Cmd {
	return exec.Command("sh", "-c", "sleep 300")
}

func bareBashHold() *exec.Cmd {
	return exec.Command("bash", "-c", "sleep 60")
}

func absoluteSleepHold() *exec.Cmd {
	return exec.Command("/bin/sleep", "300")
}

func shortLivedShell() *exec.Cmd {
	return exec.Command("sh", "-c", "exit 0")
}
`

// TestSleepFixtureGateSeesABareShellHold is the red proof, and it names BOTH
// sides: the planted sites turn the widened recognizer RED, and the pre-widening
// spellings do not see them at all.
func TestSleepFixtureGateSeesABareShellHold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sleepFixturePlantedFile)
	if err := os.WriteFile(path, []byte(plantedFixtureSource), 0o644); err != nil {
		t.Fatalf("plant the fixture tree: %v", err)
	}

	sites, err := sleepFixtureSitesInFile(path)
	if err != nil {
		t.Fatalf("scan the planted tree: %v", err)
	}
	if len(sites) != sleepFixturePlantedWantSites {
		t.Fatalf("the widened recognizer judged %d site(s) in the planted tree, want %d (a short-lived `sh -c \"exit 0\"` "+
			"is out of scope by its hold; every long-lived hold must be seen) — sites: %+v",
			len(sites), sleepFixturePlantedWantSites, sites)
	}
	// THE GATE'S OWN JUDGING, over the planted tree. An empty baseline means no
	// frozen rows, so `offending` is exactly "the gate would FAIL here" — the
	// verdict is the real one, not a re-implementation of it.
	verdict := sleepFixtureJudge(dir, sites, map[string]string{})
	for _, s := range sites {
		t.Logf("planted site seen: %s:%d covered=%q", filepath.Base(s.file), s.line, s.covered)
	}
	if verdict.offenders != sleepFixturePlantedWantOffenders {
		t.Fatalf("the gate judged %d offender(s) in the planted tree, want %d — every planted hold is deliberately "+
			"uncovered, so the gate must turn RED on this tree (red messages: %v)",
			verdict.offenders, sleepFixturePlantedWantOffenders, verdict.red)
	}
	if len(verdict.red) < sleepFixturePlantedWantOffenders {
		t.Fatalf("the gate reported %d red message(s) for %d offender(s) — a count with no message names no file",
			len(verdict.red), verdict.offenders)
	}
	// The freeze is what would hide an offender, so the ONE row that names the
	// bare-`bash` planted site is judged too: with it frozen the same tree is NOT
	// red for that site — proof the ratchet is what distinguishes "new leak" from
	// "pre-existing", and that the planted sites are otherwise indistinguishable
	// from the ones the real tree carries.
	bareBash := sleepFixturePlantedSiteFor(sites, sleepFixturePlantedBashLine)
	if bareBash.covered != "" || bareBash.line == 0 {
		t.Fatalf("the planted bare-`\"bash\"` hold at line %d was not judged as uncovered (covered=%q) — the red proof "+
			"must exercise the spelling the measured survivor used",
			sleepFixturePlantedBashLine, bareBash.covered)
	}
	frozenOne := sleepFixtureJudge(dir, sites, map[string]string{
		sleepFixtureBaselineKey(dir, bareBash): sleepFixtureFrozenReason,
	})
	if frozenOne.frozen != 1 || frozenOne.offenders != verdict.offenders-1 {
		t.Fatalf("freezing the bare-`\"bash\"` site changed the census to frozen=%d offenders=%d, want frozen=1 "+
			"offenders=%d — the baseline must be what hides a site, and nothing else",
			frozenOne.frozen, frozenOne.offenders, verdict.offenders-1)
	}
	// The NEGATIVE CONTROL: the same text through the OLD spelling. Zero matches is
	// the hole that was repaired — and it is asserted, not described, so a later
	// "widening" that silently narrows again fails here.
	oldShell := regexp.MustCompile(sleepFixtureOldShellSpawnSpelling)
	oldDirect := regexp.MustCompile(sleepFixtureOldDirectSpawnSpelling)
	shellHits, directHits := 0, 0
	for _, line := range strings.Split(plantedFixtureSource, "\n") {
		if oldShell.MatchString(line) {
			shellHits++
		}
		if oldDirect.MatchString(line) {
			directHits++
		}
	}
	if shellHits != 0 {
		t.Fatalf("the OLD shell arm matched %d planted line(s) — if the pre-widening spelling already caught a bare `\"sh\"` "+
			"or `\"bash\"`, this red proof is not exercising the hole it claims to", shellHits)
	}
	if directHits != 0 {
		t.Fatalf("the OLD direct arm matched %d planted line(s) — it was supposed to be blind to an absolute `/bin/sleep`, "+
			"and the whole point of this arm is that it is not any more", directHits)
	}
	t.Logf("red proof: widened recognizer judged %d planted site(s), %d of them uncovered; the pre-widening spellings "+
		"matched %d (shell) and %d (direct) of the same lines — the hole is real and it is closed",
		len(sites), verdict.offenders, shellHits, directHits)
}

// sleepFixturePlantedSiteFor returns the judged site at the given line, or a
// zero site when the recognizer did not see it.
func sleepFixturePlantedSiteFor(sites []sleepFixtureSite, line int) sleepFixtureSite {
	for _, s := range sites {
		if s.line == line {
			return s
		}
	}
	return sleepFixtureSite{}
}
