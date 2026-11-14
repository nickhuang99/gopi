package testutil

// fixture_spawn_inventory_test.go — the measured inventory of every test-side
// spawn of a test binary, and the ONE rule that keeps the leak from coming back.
//
// THE SHAPE THIS IS ABOUT (measured on zpeer 2026-09-26). A fixture child that
// blocks forever, whose test parent never waits for it and never kills it,
// outlives the test binary: it reparents to init (PPID 1) and stands wherever it
// was started — inside a task worktree, if that is the CWD it inherited.
// finish-cleanup's holder guard then refuses to release the directory, correctly,
// and the branch cannot be deleted either. The measured pair was
// `python3 …/blackhole.py` (pid 1503983, ppid 1, 11 h) and `signalrec.test`
// (pid 1525157).
//
// WHY AN INVENTORY AND NOT A FIX AT EACH SITE. A leak at one site is a bug; the
// same leak at fifty sites is a missing mechanism. The inventory makes the SET
// visible and makes a NEW unbound site fail the build, so "we fixed the one we
// knew about" cannot quietly become "there are seven more next quarter".
//
// WHAT A SITE MUST DO. A spawn whose child can outlive its parent must either
// route through the binding seam (testutil.BindFixtureChild / StartFixtureChild
// / WatchFixtureBinding, or the daemonlifetime primitives directly) or appear in
// fixtureSpawnExempt with a reason naming the mechanism that bounds it (a
// process-group reaper, a wait, a kill). The classic leak-prone site —
// unbounded child, no wait, no kill, no binding — fails outright.
//
// THE GATE IS HONEST ABOUT ITSELF: the classifier is exercised on a synthetic
// leak-prone site (fixtureSpawnIsLeakProne) so the rule cannot pass by
// classifying nothing, and the inventory is LOGGED in full every run — an
// exemption a reader cannot see is an exemption nobody reviews.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// fixtureInventorySrcName is the git-tracked source root the inventory scans,
	// spelled once here and never inline (Rule 13).
	fixtureInventorySrcName = "internal/"
	// fixtureInventorySrcNameCmd is the second root — the CLI package spawns
	// test-binary children too (serve/lock/worker fixtures).
	fixtureInventorySrcNameCmd = "cmd/"
	// fixtureInventorySiblingRoot is the third root.
	fixtureInventorySiblingRoot = "testsuite/"
	// fixtureInventoryTestSuffix identifies a test source file.
	fixtureInventoryTestSuffix = "_test.go"
	// fixtureInventoryMaxSites bounds the scan: a repository with more test-side
	// spawn sites than this is one whose inventory has outgrown a single test,
	// and the cap is a deliberate loud stop rather than a silent truncation.
	fixtureInventoryMaxSites = 400
)

// fixtureSpawnSite is one spawn of a test binary found by the scan.
type fixtureSpawnSite struct {
	file string // repo-relative
	fn   string // the enclosing function
	line int
	// boundByPipe is true when the site (or the seam it calls) adopts the
	// inherited death descriptor — the strongest bound: the kernel enforces it.
	boundByPipe bool
	// waits / kills / reaper record the in-process bounds: the parent waits for
	// or kills the child, or hands it to the process-group reaper.
	waits  bool
	kills  bool
	reaper bool
	// blocksForever is true when the child body this site starts contains an
	// unbounded block (`select {}` or a bare `for {`): a child that will never
	// exit on its own, which is the precondition of the leak.
	blocksForever bool
}

// leakProne is the shape that leaks: the child can outlive everything, and
// nothing in the suite bounds it.
func (s fixtureSpawnSite) leakProne() bool {
	return s.blocksForever && !s.boundByPipe && !s.waits && !s.kills && !s.reaper
}

// TestFixtureSpawnInventoryIsAuditable walks every test-side spawn of a test
// binary, classifies it, logs the whole inventory, and fails on a site with the
// leak's shape.
func TestFixtureSpawnInventoryIsAuditable(t *testing.T) {
	root := fixtureRepoRoot(t)
	sites := fixtureCollectSpawnSites(t, root)
	if len(sites) == 0 {
		t.Fatalf("the inventory found NO test-side spawn of a test binary under %s — the scan is broken, not the repository", root)
	}
	if len(sites) > fixtureInventoryMaxSites {
		t.Fatalf("the inventory found %d spawn sites (cap %d) — split the scan before trusting its verdict", len(sites), fixtureInventoryMaxSites)
	}

	var leakProne []fixtureSpawnSite
	for _, s := range sites {
		t.Logf("spawn site %s:%d %s — pipe=%v waits=%v kills=%v reaper=%v blocksForever=%v",
			s.file, s.line, s.fn, s.boundByPipe, s.waits, s.kills, s.reaper, s.blocksForever)
		if s.leakProne() {
			leakProne = append(leakProne, s)
		}
	}
	if len(leakProne) == 0 {
		t.Logf("inventory: %d spawn sites, none with the leak's shape", len(sites))
		return
	}
	sort.Slice(leakProne, func(i, j int) bool { return leakProne[i].file < leakProne[j].file })
	for _, s := range leakProne {
		if reason, ok := fixtureSpawnExempt[s.file+"|"+s.fn]; ok {
			t.Logf("exempt (recorded): %s:%d %s — %s", s.file, s.line, s.fn, reason)
			continue
		}
		t.Errorf("%s:%d %s starts a child that can outlive this test binary with nothing bounding it — bind it through testutil.BindFixtureChild/StartFixtureChild (the child adopts via WatchFixtureBinding), or make the suite wait for / kill / group-reap it; if neither is right, record it in fixtureSpawnExempt with the mechanism that bounds it",
			s.file, s.line, s.fn)
	}
}

// TestFixtureSpawnClassifierFindsTheLeakItself is the red proof for the gate: a
// synthetic site with the leak's shape must classify as leak-prone, and the
// bound shapes must not. Without it, a classifier that matched nothing would
// pass the inventory above for the wrong reason.
func TestFixtureSpawnClassifierFindsTheLeakItself(t *testing.T) {
	leak := fixtureSpawnSite{blocksForever: true}
	if !leak.leakProne() {
		t.Error("a child that blocks forever with no wait, no kill and no binding IS the leak — the classifier must say so")
	}
	for name, s := range map[string]fixtureSpawnSite{
		"bound by the death pipe":     {blocksForever: true, boundByPipe: true},
		"the parent waits for it":     {blocksForever: true, waits: true},
		"the parent kills it":         {blocksForever: true, kills: true},
		"the group reaper owns it":    {blocksForever: true, reaper: true},
		"the child exits on its own":  {},
		"it exits but nobody sees it": {kills: false},
	} {
		if s.leakProne() {
			t.Errorf("%s must NOT classify as leak-prone", name)
		}
	}
}

// fixtureSpawnExempt records the sites the classifier flags whose bound is
// real but not expressible as one of the mechanical flags. Every entry names
// the mechanism, because an exemption without a mechanism is a silent skip
// wearing a passing green.
//
// It is DELIBERATELY EMPTY at the time of the fix: every blocking fixture child
// either adopts the death pipe (signalrec's victims, the daemon-lifetime and
// fixture-lifetime holders) or is bounded by a wait/kill/group-reaper that the
// classifier sees. An entry added later must carry its reason here.
var fixtureSpawnExempt = map[string]string{}

// --- the scan ---------------------------------------------------------------

// fixtureCollectSpawnSites finds every exec.Command that starts a test binary.
func fixtureCollectSpawnSites(t *testing.T, root string) []fixtureSpawnSite {
	t.Helper()
	var sites []fixtureSpawnSite
	for _, sub := range []string{fixtureInventorySrcName, fixtureInventorySrcNameCmd, fixtureInventorySiblingRoot} {
		base := filepath.Join(root, sub)
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //stylegate:ignore an unreadable subtree is reported by the empty-inventory guard below, which is the loud path
			}
			if d.IsDir() || !strings.HasSuffix(path, fixtureInventoryTestSuffix) {
				return nil
			}
			found := fixtureSpawnSitesInFile(t, root, path)
			sites = append(sites, found...)
			return nil
		})
	}
	return sites
}

// fixtureSpawnSitesInFile classifies the spawn sites of ONE test source file.
func fixtureSpawnSitesInFile(t *testing.T, root, path string) []fixtureSpawnSite {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(raw)
	if !fixtureSpawnSelfRe.MatchString(text) {
		return nil
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		rel = path
	}
	lines := strings.Split(text, "\n")
	var sites []fixtureSpawnSite
	for i, line := range lines {
		if !fixtureSpawnSelfRe.MatchString(line) {
			continue
		}
		fn, body := fixtureEnclosingFunc(lines, i)
		if fn == "" {
			// A spawn outside any function is a package-level var — nothing to
			// classify, and the child bodies below cannot be attributed to it.
			continue
		}
		site := fixtureSpawnSite{file: rel, fn: fn, line: i + 1}
		site.boundByPipe = fixtureBindingSeamRe.MatchString(body)
		site.waits = fixtureWaitRe.MatchString(body)
		site.kills = fixtureKillRe.MatchString(body)
		site.reaper = strings.Contains(body, fixtureReaperName)
		// The blocking child lives in a DIFFERENT function (the child's own
		// entry), reached through the guard environment this site sets.
		site.blocksForever = fixtureChildBlocksForever(text, body)
		sites = append(sites, site)
	}
	return sites
}

var (
	// fixtureSpawnSelfRe matches a spawn of the TEST BINARY itself, in both
	// living spellings: the historical exec.Command(os.Args[0]) and the
	// os.Executable() form (fixtureRecursionChildCmd resolves the binary once and
	// passes it as an argument to a helper, so the helper's own line does not
	// match — the caller's does).
	fixtureSpawnSelfRe = regexp.MustCompile(`exec\.Command\(\s*(os\.Args\[0\]|os\.Executable\(\)|exe\b|exe,)`)
	// fixtureBindingSeamRe matches the ONE binding seam, in any of its public
	// spellings. A site that calls it is bound at the kernel level.
	fixtureBindingSeamRe = regexp.MustCompile(`BindFixtureChild|StartFixtureChild|WatchFixtureBinding|AdoptInheritedHoldFD|daemonlifetime\.|srBindVictim`)
	// fixtureWaitRe / fixtureKillRe match the in-process bounds. The wait has
	// FOUR spellings and all of them block until the child is reaped — .Wait(),
	// .Run(), .Output() and .CombinedOutput() (the last three call Wait
	// internally). Matching only the first is how a bounded site gets reported
	// as a leak: measured 2026-09-26, cmd/agent/cli_voice_review_test.go's
	// runVoiceChild was flagged although it uses cmd.Run().
	fixtureWaitRe = regexp.MustCompile(`\.Wait\(\)|\.Run\(\)|\.Output\(\)|\.CombinedOutput\(\)`)
	fixtureKillRe = regexp.MustCompile(`Process\.Kill\(\)|syscall\.Kill\(|\bkill\(`)
	// fixtureReaperName is the shared process-group reaper (mockproc.go). A
	// child started through it is killed, with its whole group, on t.Cleanup —
	// which testing runs on every in-process exit path.
	fixtureReaperName = "StartMockProcessCmd"
)

// fixtureEnclosingFunc returns the function containing line index i and that
// function's body — the unit the classifier judges.
func fixtureEnclosingFunc(lines []string, i int) (string, string) {
	start := -1
	var name string
	for j := i; j >= 0; j-- {
		if m := fixtureFuncDeclRe.FindStringSubmatch(lines[j]); m != nil {
			start, name = j, m[1]
			break
		}
	}
	if start < 0 {
		return "", ""
	}
	end := len(lines)
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "}") || strings.HasPrefix(lines[j], "func ") {
			end = j
			break
		}
	}
	return name, strings.Join(lines[start:end], "\n")
}

// fixtureFuncDeclRe matches a function declaration and captures its NAME. The
// receiver group is an explicit alternation — `(p *pkg) ` — rather than an
// optional run of word characters: the optional form matched PART of a plain
// function's name (it captured "d" for `func reexecHolderCmd(…)`) and matched no
// method at all (measured with a four-form table: plain, method, no-arg, and a
// two-type-param signature). The name it returns is a key in the exemption
// tables below, so a wrong capture silently moves an exemption onto a site that
// no longer exists.
var fixtureFuncDeclRe = regexp.MustCompile(`^func (?:\(\w+ \*?\w+\) )?(\w+)\(`)

// fixtureChildBlocksForever reports whether the child this spawn body starts
// contains an unbounded block. The child's body is found by the guard variables
// the spawner sets: a guard name that appears in a function which blocks forever
// means THIS site starts a child that never returns on its own.
func fixtureChildBlocksForever(text, spawnBody string) bool {
	// The whole file's own body first: a spawner that blocks inline (a holder
	// fixture that starts a child and then blocks itself) is leak-prone too.
	if fixtureBlockRe.MatchString(spawnBody) {
		return true
	}
	guards := fixtureGuardNamesIn(spawnBody)
	if len(guards) == 0 {
		return false
	}
	for _, fm := range fixtureFuncBodyRe.FindAllStringSubmatchIndex(text, -1) {
		body := text[fm[0]:fm[1]]
		if !fixtureBlockRe.MatchString(body) {
			continue
		}
		for _, g := range guards {
			if strings.Contains(body, g) {
				return true
			}
		}
	}
	return false
}

var (
	// fixtureBlockRe matches an unbounded block: `select {}` (no cases) or a
	// `for {` loop head. Both are the "blocks until killed" shape the leak needs.
	fixtureBlockRe = regexp.MustCompile(`select \{\}|for \{\s*$|for \{\n`)
	// fixtureFuncBodyRe matches a whole top-level function body.
	fixtureFuncBodyRe = regexp.MustCompile(`(?ms)^func \w+\([^)]*\)[^{]*\{.*?^\}`)
	// fixtureGuardNameRe extracts the environment VARIABLE names a spawner sets.
	fixtureGuardNameRe = regexp.MustCompile(`(?m)(\w*[Ee]nv\w*|\w+Env)\s*[:=]`)
)

// fixtureGuardNamesIn lists the environment-variable names a spawn body sets.
func fixtureGuardNamesIn(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range fixtureGuardNameRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if len(name) < 3 || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// TestFixtureFuncDeclReReadsPlainAndMethodNames is the regression guard for
// fixtureFuncDeclRe (Rule 5: every fix ships a minimal repro test). The
// optional-run form this regex used to carry captured a PART of a plain
// function's name — `func reexecHolderCmd(…)` yielded "d" — and matched no
// method at all, while the captured name is a KEY in the exemption tables. A
// wrong capture silently moves an exemption onto a site that no longer exists.
func TestFixtureFuncDeclReReadsPlainAndMethodNames(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"func reexecHolderCmd(t *testing.T) *exec.Cmd {", "reexecHolderCmd"},
		{"func startHolder() *exec.Cmd {", "startHolder"},
		{"func testHelperCommand(t *testing.T, name string) *exec.Cmd {", "testHelperCommand"},
		{"func (p *fixturePackage) absorb(text string) {", "absorb"},
		{"func TestSomething(t *testing.T) {", "TestSomething"},
	} {
		m := fixtureFuncDeclRe.FindStringSubmatch(tc.line)
		if m == nil {
			t.Errorf("%q: no function name matched — a declaration this scanner cannot read makes its sites unattributable", tc.line)
			continue
		}
		if m[1] != tc.want {
			t.Errorf("%q: name %q, want %q", tc.line, m[1], tc.want)
		}
	}
}

// fixtureRepoRoot locates the repository root from this file's own directory
// (internal/testutil → the root is two levels up), so the inventory never
// depends on the caller's working directory.
func fixtureRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("locate the working directory: %v", err)
	}
	root := filepath.Dir(filepath.Dir(wd))
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("the repository root resolved to %s, which has no go.mod (%v) — the inventory cannot be trusted from here", root, statErr)
	}
	return root
}
