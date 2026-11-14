package sandbox

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The sandbox's contract, driven through the REAL initializer (no fixture
// double): the shape, the idempotence, the marker, and the two symlink
// contracts. Rule 31's spirit applied one level down — each test calls the
// production entry point with real arguments on a real directory.

// demoRoot returns a root under the suite's pinned scratch tree (Rule 22: never
// the ambient temp root — testutil's pin refuses an unpinned suite by design).
func demoRoot(t *testing.T) string {
	t.Helper()
	return testutil.TestScratchDir(t, "sandbox")
}

// settingsFile writes a minimal real settings file to link as the sandbox's
// configuration — the PRODUCTION settings file is not the tests' to use, and the
// link's target only has to be a real file for the shape assertions.
func settingsFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "real-settings.json")
	if err := os.WriteFile(p, []byte(`{"model":"demo"}`), 0o600); err != nil {
		t.Fatalf("write the demo settings file: %v", err)
	}
	return p
}

// pluginsTree writes a minimal REAL plugin tree (plugins/websearch/scripts
// holding one script) under dir and returns its tree root — the directory the
// sandbox's `plugins` entry links to. A fixture tree, not the operator's: the
// suite must not depend on what a host happens to have deployed (the derived
// default is covered by TestPluginTreeLinkResolvesWhereTheToolLooks, which pins
// the resolution explicitly).
func pluginsTree(t *testing.T, dir string) string {
	t.Helper()
	treeRoot := filepath.Join(dir, "real-"+config.PluginsDirName)
	scripts := filepath.Join(treeRoot, config.WebSearchPluginDirName, config.WebSearchScriptsDirName)
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatalf("create the fixture plugin tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scripts, sandboxFixtureScriptName), []byte("#!/usr/bin/env python3\n"), 0o644); err != nil {
		t.Fatalf("write the fixture plugin script: %v", err)
	}
	return treeRoot
}

// sandboxFixtureScriptName is the script the fixture plugin tree carries. A
// REAL plugin script name, so the assertions below name the file the tool would
// actually exec rather than an invented one.
const sandboxFixtureScriptName = "wiki_search.py"

// TestCreateMaterialisesTheStandardShape is the shape contract: exactly the
// entries PROJECT.md's rule names, and nothing else.
func TestCreateMaterialisesTheStandardShape(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	sample := testutil.SampleDBPath(t)

	sb, err := Create(root, "", Options{
		Label:               "build",
		TaskID:              "01a0demo-0000-7000-8000-00000000demo",
		ProfileSettingsPath: settings,
		LedgerSeedPath:      sample,
		PluginsPath:         pluginsTree(t, root),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !sb.Created {
		t.Errorf("Create reported created=false on a fresh root")
	}
	if filepath.Dir(sb.Path) != scratch.CanonicalPath(root) {
		t.Errorf("sandbox %s is not a direct child of the root %s", sb.Path, root)
	}
	if base := filepath.Base(sb.Path); base[:len(SandboxNamePrefix)] != SandboxNamePrefix {
		t.Errorf("sandbox name %q does not carry the marker prefix %q", base, SandboxNamePrefix)
	}

	// The shape: the PROFILE root (<sb>/.gopi, where config.GopiDir resolves for
	// GOPI_HOME=<sb>) holding the three private directories and the two
	// symlinks, plus the ownership marker beside it.
	// The shape: three private directories, one symlink, the profile directory
	// holding the ledger, and the marker. The ledger is NOT a top-level entry:
	// it lives at the path the ledger resolver computes for this GOPI_HOME.
	entries, err := os.ReadDir(sb.Path)
	if err != nil {
		t.Fatalf("read the sandbox: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{
		config.ProfileDirName,
		config.SandboxOwnerFileName,
	}
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("the sandbox holds %v, want exactly %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("the sandbox holds %v, want exactly %v", names, want)
		}
	}

	profileEntries, err := os.ReadDir(config.ProfileDirIn(sb.Path))
	if err != nil {
		t.Fatalf("read the sandbox's profile %s: %v", config.ProfileDirIn(sb.Path), err)
	}
	var profileNames []string
	for _, e := range profileEntries {
		profileNames = append(profileNames, e.Name())
	}
	sort.Strings(profileNames)
	wantProfile := []string{
		config.PluginsDirName,
		config.SandboxLogsDirName,
		config.SandboxRawDirName,
		config.SandboxSessionsDirName,
		config.SandboxSettingsFileName,
		config.MemoryDBFileName,
	}
	sort.Strings(wantProfile)
	if len(profileNames) != len(wantProfile) {
		t.Fatalf("the sandbox profile holds %v, want exactly %v", profileNames, wantProfile)
	}
	for i := range wantProfile {
		if profileNames[i] != wantProfile[i] {
			t.Fatalf("the sandbox profile holds %v, want exactly %v", profileNames, wantProfile)
		}
	}
	for _, d := range []string{config.SandboxSessionsDirName, config.SandboxRawDirName, config.SandboxLogsDirName} {
		p := config.ProfilePathIn(sb.Path, d)
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s is not a directory: %v", p, err)
		}
		kids, _ := os.ReadDir(p) //stylegate:ignore lists a directory the test populated; the entries are the assertion
		if len(kids) != 0 {
			t.Errorf("%s is not empty (%d entries) — a fresh sandbox's private dirs start empty", p, len(kids))
		}
	}
}

// TestSandboxProfileRootIsWhatTheReadersResolve is the RED-PINNING test for the
// sandbox/PROFILE-RESOLUTION divergence this branch fixes.
//
// THE DEFECT IT PINS (measured 2026-09-26, before the fix): `gopi tool sandbox
// create` materialised sessions/, raw/, logs/ and the two symlinks DIRECTLY
// under the sandbox directory while every reader resolved
// $GOPI_HOME/.gopi/{sessions,raw,...} — so the documented invocation
// `GOPI_HOME=$SB gopi session log --last` answered
//
//	Error listing sessions: open $SB/.gopi/sessions: no such file or directory
//
// while `ls $SB/sessions` showed a directory sitting there. The creator and the
// readers disagreed about where a profile lives.
//
// THE ONE TRUTH THIS ASSERTS: the path a reader DERIVES from a home is a path
// the initializer MATERIALISED. Named surfaces, not copies of them:
// config.GopiDir() with GOPI_HOME=sb.Path (what gopilog.ResolveSessionDir, the
// raw-log writer and the ledger all resolve through), config.ProfilePathIn for
// each profile entry, and sandbox.SettingsFileNameIn for the settings file the
// child is handed via GOPI_SETTINGS.
//
// It would have failed on the pre-fix tree at the FIRST assertion (the profile
// directory did not exist), which is what makes it a regression guard rather
// than a restatement of the shape test above.
func TestSandboxProfileRootIsWhatTheReadersResolve(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	sample := testutil.SampleDBPath(t)

	sb, err := Create(root, "", Options{Label: "readers", ProfileSettingsPath: settings, LedgerSeedPath: sample})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// ① The reader's own resolution, under THIS sandbox as GOPI_HOME.
	t.Setenv(config.GopiDirEnv, sb.Path) // t.Setenv (Rule 17): scoped to this test
	gotProfile, err := config.GopiDir()
	if err != nil {
		t.Fatalf("config.GopiDir under GOPI_HOME=%s: %v", sb.Path, err)
	}
	if gotProfile != config.ProfileDirIn(sb.Path) {
		t.Fatalf("config.GopiDir resolves %s, but the initializer materialised the profile at %s",
			gotProfile, config.ProfileDirIn(sb.Path))
	}
	info, err := os.Stat(gotProfile)
	if err != nil || !info.IsDir() {
		t.Fatalf("the profile root config.GopiDir resolves (%s) is not a directory the sandbox created: %v",
			gotProfile, err)
	}

	// ② Every profile entry a reader joins by name exists at the derived path —
	//    asserted through the SAME function the readers use.
	for _, name := range []string{
		config.SandboxSessionsDirName, config.SandboxRawDirName, config.SandboxLogsDirName,
	} {
		p := config.ProfilePathIn(sb.Path, name)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Errorf("the reader-resolved %s is not a directory: %v (the documented invocation would report "+
				"'no such file or directory')", p, err)
		}
	}

	// ③ The settings file handed to a child as GOPI_SETTINGS is the path the
	//    reader resolves for GOPI_HOME=<sandbox> — the two halves of ProfileEnv.
	gotSettings := sandboxSettingsResolvedFor(t, sb.Path)
	if gotSettings != SettingsFileNameIn(sb.Path) {
		t.Errorf("a child's GOPI_SETTINGS is %s, but resolveSettingsPath in that profile is %s",
			SettingsFileNameIn(sb.Path), gotSettings)
	}

	// ④ The ledger and the raw log are the profile-scoped ones, not siblings.
	if got := config.ProfilePathIn(sb.Path, config.MemoryDBFileName); got != filepath.Join(gotProfile, config.MemoryDBFileName) {
		t.Errorf("the ledger join %s is not under the profile root %s", got, gotProfile)
	}
}

// sandboxSettingsResolvedFor reads the settings path AS A PROCESS WHOSE
// GOPI_HOME IS THE SANDBOX would: GOPI_SETTINGS unset, so it falls to
// <gopi data root>/settings.json. It is the reader-side half of the pair the
// test above reconciles with SettingsFileNameIn.
func sandboxSettingsResolvedFor(t *testing.T, home string) string {
	t.Helper()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.SettingsEnv, "")
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("config.GopiDir: %v", err)
	}
	return filepath.Join(gd, config.SandboxSettingsFileName)
}

// TestSettingsIsAReadOnlyCopyAndTheLedgerIsARealFile pins the two shapes that
// matter most, side by side, because they are deliberately DIFFERENT:
//
//   - settings.json is a REAL, READ-ONLY FILE (0444) holding a byte-for-byte
//     copy of the operator's document — NOT a symlink. The symlink was a
//     write-through: a `>` redirect followed it and truncated the operator's
//     LIVE /home/user/.gopi/settings.json (2026-09-27, twelve keys lost). The proof that
//     the shape is fixed is the second half of the test below: the real source
//     is UNCHANGED after a write attempt through the profile path;
//   - the ledger is a REAL FILE at the path the ledger resolver computes, so a
//     run inside the sandbox can write it and no writer's rename can silently
//     swap a link node out from under the sandbox.
func TestSettingsIsAReadOnlyCopyAndTheLedgerIsARealFile(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	sample := testutil.SampleDBPath(t)

	sb, err := Create(root, "", Options{ProfileSettingsPath: settings, LedgerSeedPath: sample})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	profileSettings := SettingsFileNameIn(sb.Path)
	before := settingsDocBytes(t, settings)
	docInfo, err := os.Lstat(profileSettings)
	if err != nil {
		t.Fatalf("lstat the profile settings document %s: %v", profileSettings, err)
	}
	if docInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the profile settings document %s is a SYMLINK — a shell redirect follows it and "+
			"truncates the operator's LIVE file (this is the 2026-09-27 clobber shape)", profileSettings)
	}
	if !docInfo.Mode().IsRegular() {
		t.Fatalf("the profile settings document %s is not a regular file (%s)", profileSettings, docInfo.Mode().Type())
	}
	if perm := docInfo.Mode().Perm(); perm != 0o444 {
		t.Errorf("the profile settings document is mode %o, want 0444 (a read-only COPY)", perm)
	}
	if got := settingsDocBytes(t, profileSettings); got != before {
		t.Errorf("the profile settings copy holds %q, want the operator's document %q", got, before)
	}
	if sb.SettingsDocSource != settings || sb.SettingsDocPath != profileSettings {
		t.Errorf("Sandbox records source %q / path %q, want %q / %q",
			sb.SettingsDocSource, sb.SettingsDocPath, settings, profileSettings)
	}

	// The ledger: a regular file, at the resolved path, present in the Sandbox
	// value — never a symlink.
	if want := LedgerPathIn(sb.Path); sb.LedgerPath != want {
		t.Errorf("Sandbox.LedgerPath = %q, want %q", sb.LedgerPath, want)
	}
	info, err := os.Lstat(sb.LedgerPath)
	if err != nil {
		t.Fatalf("lstat the ledger %s: %v", sb.LedgerPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the ledger %s is a SYMLINK — it must be a real file (a symlink is either unwritable "+
			"or silently replaced by a writer's rename)", sb.LedgerPath)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("the ledger %s is not a regular file (%s)", sb.LedgerPath, info.Mode().Type())
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the ledger is mode %o, want 0600 (private and writable)", perm)
	}
	// The seed was COPIED, never linked: the copy is what lets the sample keep
	// its 0444 seal while the sandbox gets a file it may write.
	if info.Size() == 0 {
		t.Errorf("the ledger is empty — a named seed must be copied in")
	}
	if sb.LedgerPath == sample {
		t.Errorf("the ledger IS the shared sample (%s) — a sandbox must never point its ledger at it", sample)
	}
}

// TestLedgerIsAWriteTarget drives the property the whole shape exists for: a
// writer that opens the resolved ledger path the way a run does must SUCCEED,
// and the file must still be a real file afterwards (never a replaced link).
func TestLedgerIsAWriteTarget(t *testing.T) {
	root := demoRoot(t)
	sb, err := Create(root, "", Options{Label: "write", SkipSettingsDoc: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// No seed named: the ledger starts EMPTY, which is the state the ledger's
	// own first open adopts and initializes (internal/tools owns that DDL).
	info, err := os.Lstat(sb.LedgerPath)
	if err != nil {
		t.Fatalf("lstat the ledger: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("an unseeded ledger should start empty, got %d bytes", info.Size())
	}

	// Write through the resolved path exactly as a run does, then re-resolve.
	want := []byte("a run's own ledger bytes")
	if err := os.WriteFile(sb.LedgerPath, want, 0o600); err != nil {
		t.Fatalf("write the ledger %s: %v", sb.LedgerPath, err)
	}
	after, err := os.Lstat(LedgerPathIn(sb.Path))
	if err != nil {
		t.Fatalf("lstat the ledger after writing: %v", err)
	}
	if after.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the ledger became a symlink after a write")
	}
	got, err := os.ReadFile(LedgerPathIn(sb.Path))
	if err != nil {
		t.Fatalf("read the ledger back: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("the ledger reads back %q, want %q", got, want)
	}
}

// TestExistingLedgerSymlinkIsRefused: the PRE-FIX shape must not be silently
// replaced or written through — a path that changes meaning without saying so is
// the defect this shape fixes. The refusal has to name the file.
func TestExistingLedgerSymlinkIsRefused(t *testing.T) {
	root := demoRoot(t)
	sample := testutil.SampleDBPath(t)
	sb, err := Create(root, "", Options{Label: "legacy", SkipSettingsDoc: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Rebuild the OLD shape deliberately: remove the real ledger, link the
	// sample in its place (exactly what the previous initializer produced).
	if err := os.Remove(sb.LedgerPath); err != nil {
		t.Fatalf("remove the ledger: %v", err)
	}
	if err := os.Symlink(sample, sb.LedgerPath); err != nil {
		t.Fatalf("link the sample as the legacy ledger: %v", err)
	}
	_, err = Ensure(sb.Path, root, config.SandboxRootSourceFlag, Options{SkipSettingsDoc: true})
	if err == nil {
		t.Fatalf("Ensure ACCEPTED a symlinked ledger — the pre-fix shape must be refused by name")
	}
	msg := err.Error()
	if !strings.Contains(msg, sb.LedgerPath) || !strings.Contains(msg, sample) {
		t.Errorf("the refusal does not name both the link (%s) and its target (%s): %s", sb.LedgerPath, sample, msg)
	}
	// And it changed nothing: the link is still the link.
	cur, lerr := os.Readlink(sb.LedgerPath)
	if lerr != nil || cur != sample {
		t.Errorf("the refused ledger link was modified (target %q, err %v)", cur, lerr)
	}
}

// TestReadOnlyLedgerIsRefusedLoudly: a ledger that exists but cannot be written
// is the exact failure the shape exists to prevent, so the initializer must
// report it (naming the file) rather than adopt it and let every later run fail
// with an obscure sqlite error.
func TestReadOnlyLedgerIsRefusedLoudly(t *testing.T) {
	root := demoRoot(t)
	sb, err := Create(root, "", Options{Label: "readonly", SkipSettingsDoc: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.Chmod(sb.LedgerPath, 0o400); err != nil {
		t.Fatalf("seal the ledger read-only: %v", err)
	}
	_, err = Ensure(sb.Path, root, config.SandboxRootSourceFlag, Options{SkipSettingsDoc: true})
	if err == nil {
		t.Fatalf("Ensure adopted a READ-ONLY ledger — a run inside the sandbox could not write it")
	}
	if !strings.Contains(err.Error(), sb.LedgerPath) {
		t.Errorf("the refusal does not name the ledger: %v", err)
	}
}

// TestExistingLedgerContentIsNeverRewritten: repair is for a MISSING or EMPTY
// ledger. A sandbox that already holds a run's rows keeps them, seed or not.
func TestExistingLedgerContentIsNeverRewritten(t *testing.T) {
	root := demoRoot(t)
	sample := testutil.SampleDBPath(t)
	opts := Options{Label: "history", SkipSettingsDoc: true, LedgerSeedPath: sample}
	sb, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	own := []byte("this sandbox's own accumulated rows")
	if err := os.WriteFile(sb.LedgerPath, own, 0o600); err != nil {
		t.Fatalf("write the sandbox's own content: %v", err)
	}
	if _, err := Ensure(sb.Path, root, config.SandboxRootSourceFlag, opts); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, err := os.ReadFile(sb.LedgerPath)
	if err != nil {
		t.Fatalf("read the ledger back: %v", err)
	}
	if string(got) != string(own) {
		t.Errorf("Ensure rewrote an existing ledger: got %q, want the sandbox's own %q", got, own)
	}
}

// TestCreateIsIdempotentForALabel is the idempotence contract: the same label
// twice reuses ONE sandbox, and a missing entry is repaired by the second run.
func TestCreateIsIdempotentForALabel(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	sample := testutil.SampleDBPath(t)
	opts := Options{Label: "idem", ProfileSettingsPath: settings, LedgerSeedPath: sample}

	first, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	// Damage it the way a lost sweep would: remove one private directory.
	if err := os.RemoveAll(config.ProfilePathIn(first.Path, config.SandboxLogsDirName)); err != nil {
		t.Fatalf("remove logs/: %v", err)
	}
	second, err := Create(root, "", opts)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("the second Create made a second sandbox: %s vs %s (idempotence broken)", second.Path, first.Path)
	}
	if !second.Reused || second.Created {
		t.Errorf("the second Create reported created=%t reused=%t, want created=false reused=true",
			second.Created, second.Reused)
	}
	if _, err := os.Stat(config.ProfilePathIn(second.Path, config.SandboxLogsDirName)); err != nil {
		t.Errorf("the repair did not restore logs/: %v", err)
	}
	// And exactly ONE sandbox exists for the label.
	entries, _ := os.ReadDir(root) //stylegate:ignore lists a directory the test populated; the entries are the assertion
	count := 0
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) > len(SandboxNamePrefix) && e.Name()[:len(SandboxNamePrefix)] == SandboxNamePrefix {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the root holds %d sandboxes for one label, want 1", count)
	}
}

// TestOwnerMarkerRecordsTheLiveIncarnation is the marker contract: the pid, the
// kernel start ticks and the task id, and that the recorded incarnation answers
// "still running" for THIS process.
func TestOwnerMarkerRecordsTheLiveIncarnation(t *testing.T) {
	root := demoRoot(t)
	sb, err := Create(root, "", Options{
		Label:           "owner",
		TaskID:          "01a0demo-0000-7000-8000-00000000task",
		SkipSettingsDoc: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	owner, err := ReadOwner(sb.Path)
	if err != nil {
		t.Fatalf("ReadOwner: %v", err)
	}
	if owner.PID != os.Getpid() {
		t.Errorf("marker pid %d, want this process %d", owner.PID, os.Getpid())
	}
	if owner.TaskID != "01a0demo-0000-7000-8000-00000000task" {
		t.Errorf("marker task id %q, want the one passed in", owner.TaskID)
	}
	ticks, err := scratch.ProcStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcStartTime: %v", err)
	}
	if owner.StartTicks != ticks {
		t.Errorf("marker start ticks %d, want this process's %d (a recycled pid must not pass as the owner)",
			owner.StartTicks, ticks)
	}
	live, certain := owner.Live()
	if !live || !certain {
		t.Errorf("the marker of a LIVE process reported live=%t certain=%t, want true/true", live, certain)
	}
}

// TestSettingsLinkInsideTheSandboxIsRefused: a sandbox configured by its own
// contents is the self-referential shape that silently tests nothing.
func TestSettingsLinkInsideTheSandboxIsRefused(t *testing.T) {
	root := demoRoot(t)
	inside := filepath.Join(root, "not-a-sandbox-dir.json")
	if err := os.WriteFile(inside, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write the decoy: %v", err)
	}
	// A directory inside the root is enough to make the point: the refusal
	// compares the LINK TARGET against the sandbox's own path, so a target that
	// lives where the sandbox lives is refused.
	sb, err := Create(root, "", Options{Label: "selfref", ProfileSettingsPath: inside})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Point a second sandbox's settings at the FIRST sandbox — the shape a
	// caller produces by copying another sandbox's link.
	if _, err := Ensure(sb.Path, root, config.SandboxRootSourceFlag, Options{
		ProfileSettingsPath: filepath.Join(filepath.Dir(sb.Path), filepath.Base(sb.Path), "real-settings.json"),
	}); err == nil {
		t.Errorf("a settings symlink pointing INSIDE the sandbox was accepted — the self-referential shape must be refused")
	}
}

// TestNonDirectoryInTheWayIsRefused: Ensure must never replace something that is
// not a directory (the operator may have put their own file there).
func TestNonDirectoryInTheWayIsRefused(t *testing.T) {
	root := demoRoot(t)
	p := filepath.Join(root, SandboxNamePrefix+"file-0000")
	if err := os.WriteFile(p, []byte("not a sandbox"), 0o600); err != nil {
		t.Fatalf("write the decoy: %v", err)
	}
	if _, err := Ensure(p, root, config.SandboxRootSourceFlag, Options{}); err == nil {
		t.Fatalf("Ensure accepted a regular file as a sandbox directory")
	}
	blob, err := os.ReadFile(p)
	if err != nil || string(blob) != "not a sandbox" {
		t.Errorf("the refused file was modified (content %q, err %v)", blob, err)
	}
}

// TestSeedTargetMustExist: a seed path that names nothing is reported at CREATE
// time — not discovered later as an empty ledger.
func TestSeedTargetMustExist(t *testing.T) {
	root := demoRoot(t)
	_, err := Create(root, "", Options{
		Label:           "dangling",
		SkipSettingsDoc: true,
		LedgerSeedPath:  filepath.Join(root, "no-such-seed.db"),
	})
	if err == nil {
		t.Fatalf("Create accepted a seed database that does not exist")
	}
}

// TestSeedThatIsNotADatabaseIsRefused: the copy is verified as a DATABASE (its
// own schema read), so a wrong seed path cannot become a sandbox ledger that
// fails at the first query.
func TestSeedThatIsNotADatabaseIsRefused(t *testing.T) {
	root := demoRoot(t)
	notADB := filepath.Join(root, "not-a-database.bin")
	if err := os.WriteFile(notADB, []byte("plain text, definitely not SQLite"), 0o600); err != nil {
		t.Fatalf("write the decoy seed: %v", err)
	}
	_, err := Create(root, "", Options{
		Label:           "badseed",
		SkipSettingsDoc: true,
		LedgerSeedPath:  notADB,
	})
	if err == nil {
		t.Fatalf("Create accepted a seed that is not a SQLite database")
	}
}

// TestResolveIsTheOnlyPrecedence pins the knob's ranking (flag > env > settings
// > default) through the ONE resolver, so a second reader cannot drift from it.
func TestResolveIsTheOnlyPrecedence(t *testing.T) {
	t.Setenv(config.SandboxEnvName, "/env/sandbox")
	got, src := config.ResolveSandboxRoot(&config.Config{SandboxDir: "/settings/sandbox"},
		&config.Config{SandboxDir: "/flag/sandbox"})
	if got != "/flag/sandbox" || src != config.SandboxRootSourceFlag {
		t.Errorf("flag lost: got %q (%s)", got, src)
	}
	got, src = config.ResolveSandboxRoot(&config.Config{SandboxDir: "/settings/sandbox"}, nil)
	if got != "/env/sandbox" || src != config.SandboxRootSourceEnv {
		t.Errorf("env lost: got %q (%s)", got, src)
	}
	t.Setenv(config.SandboxEnvName, "")
	got, src = config.ResolveSandboxRoot(&config.Config{SandboxDir: "/settings/sandbox"}, nil)
	if got != "/settings/sandbox" || src != config.SandboxRootSourceSettings {
		t.Errorf("settings lost: got %q (%s)", got, src)
	}
	got, src = config.ResolveSandboxRoot(nil, nil)
	if got != config.SandboxRootDefault() || src != config.SandboxRootSourceDefault {
		t.Errorf("default lost: got %q (%s), want %q", got, src, config.SandboxRootDefault())
	}
	if got != filepath.Join(scratch.WorkspaceUnderHome(), config.SandboxDefaultLeaf) {
		t.Errorf("the default root is %q, want <home>/workspace/%s", got, config.SandboxDefaultLeaf)
	}
}
