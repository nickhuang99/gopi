package tools

// The staged smoke entry point's CLOSURE — what the fixture carries, and what it
// must never carry.
//
// pmsStageEntryPoint used to mirror the whole scripts/ tree into the fixture. The
// tree carries real Go packages of its own (scripts/lib/ledgersql is one), and
// the fixture module is stdlib-only on purpose (Rule 35: a `require` there would
// make this gate depend on a warm module cache), so that package failed AT BUILD
// on every run and printed
//
//	FAIL pmcfixture/scripts/lib/ledgersql [setup failed]
//
// into the branch-check refusal — permanently, and whether or not the branch
// under test had anything to do with it. These two arms are the pair that keeps
// the defect from coming back: one says the foreign package is OUT, the other
// says nothing the entry point actually needs went out with it.
import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// pmcForeignGoPkg is the package the fixture must never stage: it imports
	// modernc.org/sqlite, which a stdlib-only fixture module cannot resolve.
	pmcForeignGoPkg = "scripts/lib/ledgersql"
	// pmcSourcedRefRe matches a `.`/`source` line's target as the scripts spell
	// it — `"$root/scripts/lib/x.sh"` or `"$root/$var"` — so the reader below
	// derives what the entry point needs from the entry point's own text.
	pmcSourcedRefRe = `^[[:space:]]*(?:\.|source)[[:space:]]+"?\$root/(\S+?)"?[[:space:]]*$`
	// pmcAssignRe matches a shell assignment, where a `$root/$var` target's
	// value is declared.
	pmcAssignRe = `^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)=["']?([^"'\s$]+)["']?`
	// pmcSourcedFloor is the least number of sourced files the closure walk must
	// find before its answer means anything: an entry point that sources nothing
	// would make both arms below pass vacuously.
	pmcSourcedFloor = 1
)

// TestStagedEntryPointClosureLeavesOutTheForeignGoPackage is the defect's own
// regression: the fixture's scripts tree must not contain the Go package whose
// import a stdlib-only module cannot resolve.
func TestStagedEntryPointClosureLeavesOutTheForeignGoPackage(t *testing.T) {
	dir := t.TempDir()
	pmsStageEntryPoint(t, pmsRepoRoot(t), dir)
	staged := pmsStagedScriptFiles(t, dir)

	if len(staged) == 0 {
		t.Fatalf("the stage put no files under %s/ — the closure must converge to the entry point "+
			"and what it sources, never to nothing", pmsScriptsDir)
	}
	for _, rel := range staged {
		if strings.HasPrefix(rel, pmcForeignGoPkg) {
			t.Errorf("the fixture carries %s — a Go package importing a third-party module inside a "+
				"stdlib-only fixture module fails AT BUILD on every run, and its [setup failed] line "+
				"lands in the refusal an operator reads", rel)
		}
	}
	// The Go-file count is the general shape of the same defect (Rule 15: an
	// environment-dependent green is named, not assumed). The entry point is
	// shell, so a staged .go file is a package the fixture module would try to
	// build with no `require` block to satisfy it.
	if goFiles := pmsStagedGoFiles(t, dir); goFiles != 0 {
		t.Errorf("the staged closure carries %d Go file(s); the fixture module declares no `require`, "+
			"so any third-party import among them fails the build in place of the test under check",
			goFiles)
	}
	t.Logf("the fixture stages %d file(s) from %s/, none of them %s", len(staged), pmsScriptsDir,
		pmcForeignGoPkg)
}

// TestStagedEntryPointClosureCoversEverySourcedFile is the other half, and the
// one that keeps the narrowing honest: every file the entry point's own text
// sources must be IN the staged tree. Under-staging is the failure mode a
// hand-written list drifts into, and it would surface as a source-time error in
// the fixture rather than as a verdict about the branch.
func TestStagedEntryPointClosureCoversEverySourcedFile(t *testing.T) {
	root := pmsRepoRoot(t)
	dir := t.TempDir()
	pmsStageEntryPoint(t, root, dir)
	staged := map[string]bool{}
	for _, rel := range pmsStagedScriptFiles(t, dir) {
		staged[rel] = true
	}

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pmsEntryPoint)))
	if err != nil {
		t.Fatalf("read %s: %v", pmsEntryPoint, err)
	}
	sourced := pmsSourcedFilesOf(t, string(raw))
	if len(sourced) < pmcSourcedFloor {
		t.Fatalf("found %d sourced file(s) in %s — a walk that reads nothing must be a loud failure, "+
			"not a vacuous pass", len(sourced), pmsEntryPoint)
	}
	for _, rel := range sourced {
		if !staged[rel] {
			t.Errorf("the entry point sources %s and the fixture did not stage it — the fixture would "+
				"die at source time and report a verdict about the fixture, not about the branch", rel)
		}
	}
	// The entry point itself, executable as the repo has it.
	if !staged[pmsEntryPoint] {
		t.Fatalf("the fixture's scripts tree carries no %s", pmsEntryPoint)
	}
	srcInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(pmsEntryPoint)))
	if err != nil {
		t.Fatalf("stat %s: %v", pmsEntryPoint, err)
	}
	dstInfo, err := os.Stat(filepath.Join(dir, filepath.FromSlash(pmsEntryPoint)))
	if err != nil {
		t.Fatalf("stat the staged %s: %v", pmsEntryPoint, err)
	}
	if want := srcInfo.Mode().Perm() & 0o111; dstInfo.Mode().Perm()&0o111 != want {
		t.Errorf("the staged entry point carries mode %v, want its executable bits %v preserved",
			dstInfo.Mode().Perm(), want)
	}
	t.Logf("the staged closure carries all %d file(s) %s sources: %s", len(sourced), pmsEntryPoint,
		strings.Join(sourced, ", "))
}

// pmsSourcedFilesOf resolves the files a shell script sources at the top level:
// a literal `$root/scripts/lib/x.sh`, and a `$root/$name` whose assignment in the
// same text names one. It is deliberately a READER of the script, so the arm
// above cannot pass by agreeing with a list somebody wrote down.
func pmsSourcedFilesOf(t *testing.T, text string) []string {
	t.Helper()
	sourcedRef := regexp.MustCompile(pmcSourcedRefRe)
	assign := regexp.MustCompile(pmcAssignRe)
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if m := assign.FindStringSubmatch(line); m != nil {
			values[m[1]] = strings.TrimRight(m[2], `.,;:)"'`)
		}
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		m := sourcedRef.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		target := strings.TrimRight(m[1], `.,;:)"'`)
		if strings.HasPrefix(target, "$") {
			name := strings.Trim(target, "${}")
			if v, ok := values[name]; ok {
				target = v
			}
		}
		out = append(out, target)
	}
	return out
}
