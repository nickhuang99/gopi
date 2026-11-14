package envctx

// envctx_test.go — the regression proof for the environment description.
//
// THE RED ARM (Rule 5: every fix ships a minimal repro). The defect this
// work closes is "an agent asked about a peer node guesses or probes live,
// because nothing tells it what is on this host" — so the tests below assert
// the PROPERTY that fixes it, on a fixture whose answer is known in advance:
//
//	arm 1  a description is generated from a fixture ssh config and the
//	       CONFLICT the real host has (two aliases, one address) is present in
//	       the artifact — and both aliases keep that address, neither is dropped;
//	arm 2  the prompt block carries the same summary the file shows (one
//	       renderer, not two), bounded by MaxSummaryChars;
//	arm 3  the RED arm — the description RENAMED AWAY: Load reports the absence
//	       on stderr, injects nothing, and the caller's project context is
//	       returned BYTE-IDENTICAL (the run does not break);
//	arm 4  a description measured on ANOTHER host is injected WITH the mismatch
//	       stated and on stderr — it is never silently adopted as this machine.
//
// Nothing is mocked: real temp directories, real files, real bytes. The fixture
// ssh config is written here rather than read from the operator's home, so the
// assertions do not depend on one machine's inventory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureSSHConfig is a config with the SAME shape as this host's: two aliases
// claiming one address, a stanza whose Host line contradicts its HostName, a
// wildcard default stanza that must NOT become a node, and an address used as
// its own alias.
const fixtureSSHConfig = `Host alpha 192.0.2.50
        HostName 192.0.2.50
        User alpha

Host beta 192.0.2.60
        HostName 192.0.2.61

Host shared 192.0.2.50
        HostName 192.0.2.50

Host *
    ForwardX11 yes
`

// writeFixtureConfig materialises fixtureSSHConfig under a temp HOME and points
// the generator at it. The directory is returned so a test can prove the
// wildcard stanza produced no node.
func writeFixtureConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, sshDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sshConfigName), []byte(fixtureSSHConfig), 0o600); err != nil {
		t.Fatalf("write fixture ssh config: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GOPI_HOME", "") // the fixture HOME is the one that must be measured
	return filepath.Join(dir, sshConfigName)
}

// fixtureFacts builds Facts from the fixture for the rendering tests, so they
// do not depend on this machine's live interfaces.
func fixtureFacts(t *testing.T) Facts {
	t.Helper()
	path := writeFixtureConfig(t)
	nodes, missing, err := readSSHConfig(path)
	if err != nil {
		t.Fatalf("readSSHConfig(%s): %v", path, err)
	}
	if missing != "" {
		t.Fatalf("fixture ssh config reported missing: %s", missing)
	}
	return Facts{
		GeneratedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		Hostname:    "fixturehost",
		OS:          "Fixture Linux 1.0",
		Addresses:   []string{"eth0  192.0.2.9"},
		Gateway:     "192.0.2.1  (via eth0)",
		Nodes:       nodes,
		Sources:     []string{sourceLive, sourceSSHConfig},
		Conflicts:   detectConflicts(nodes),
	}
}

// writeDescription renders facts to a temp file and returns its path.
func writeDescription(t *testing.T, f Facts) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(Render(f)), fileMode); err != nil {
		t.Fatalf("write description: %v", err)
	}
	return path
}

// captureStderrValue redirects os.Stderr for the duration of fn and returns
// what was written. Synchronous (a real file, read after fn), so there is no
// pipe-drain race.
func captureStderrValue(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	f, err := os.CreateTemp(t.TempDir(), "stderr-*.log")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	os.Stderr = f
	defer func() {
		os.Stderr = old
		f.Close() //stylegate:ignore close of a file this test wrote and is about to read
	}()
	fn()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(b)
}

// TestSummaryIsTheSameTextInTheFileAndThePrompt is the ONE-RENDERER assertion:
// the block the prompt carries is the file's own summary section, so the two
// cannot drift (a second rendering could disagree with what a human reads).
func TestSummaryIsTheSameTextInTheFileAndThePrompt(t *testing.T) {
	f := fixtureFacts(t)
	path := writeDescription(t, f)

	var block Block
	var ok bool
	stderr := captureStderrValue(t, func() { block, ok = Load(path) })
	if !ok {
		t.Fatalf("Load(%s) injected nothing; stderr=%s", path, stderr)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read description: %v", err)
	}
	if !strings.Contains(string(file), block.Summary) {
		t.Errorf("the injected summary is not the file's own text.\ninjected:\n%s\nfile:\n%s", block.Summary, file)
	}
	if !strings.Contains(block.Text(), path) {
		t.Errorf("the prompt block does not name the file it points at (%s):\n%s", path, block.Text())
	}
	if !strings.Contains(block.Text(), "read") {
		t.Errorf("the prompt block carries no read-the-file pointer:\n%s", block.Text())
	}
}

// TestPointerNamesOnlyWhatTheFileCarries is the ACCURACY arm of the pointer
// sentence: the injected block sends the reader to the full file, and the
// reason to go there must be something the file actually contains. A pointer
// that promises roles, storage or services lands the reader in a file with no
// such section, so the reader pays a round to learn the pointer overstated —
// the overclaim is the defect, not the round.
//
// The assertion is the weak one on purpose: the pointer must NOT name the three
// sections this artifact has never carried. It does not require a vocabulary,
// so the sentence stays free to be rewritten.
func TestPointerNamesOnlyWhatTheFileCarries(t *testing.T) {
	f := fixtureFacts(t)
	path := writeDescription(t, f)
	var block Block
	var ok bool
	captureStderrValue(t, func() { block, ok = Load(path) })
	if !ok {
		t.Fatalf("Load(%s) injected nothing", path)
	}
	text := block.Text()
	for _, overclaimed := range []string{"roles", "storage", "services and"} {
		if strings.Contains(text, overclaimed) {
			t.Errorf("the pointer promises %q, which the description never carries:\n%s", overclaimed, text)
		}
	}
	if !strings.Contains(text, path) {
		t.Errorf("the pointer must still name the full description's path:\n%s", text)
	}
}

// TestSharedAddressIsRecordedNotResolved is the acceptance criterion for the
// conflict: two aliases on ONE address must both survive, and the ambiguity
// must be named. Neither alias may be dropped and no winner may be chosen.
func TestSharedAddressIsRecordedNotResolved(t *testing.T) {
	f := fixtureFacts(t)
	text := Render(f)

	var aliases []string
	for _, n := range f.Nodes {
		aliases = append(aliases, n.Alias)
	}
	for _, want := range []string{"alpha", "beta", "shared"} {
		if !strings.Contains(strings.Join(aliases, ","), want) {
			t.Errorf("alias %q was dropped from the node list (all: %v)", want, aliases)
		}
	}
	if strings.Contains(strings.Join(aliases, ","), sshWildcardAny) {
		t.Errorf("the `Host *` default stanza became a node: %v", aliases)
	}
	shared := 0
	for _, n := range f.Nodes {
		if n.Address == "192.0.2.50" {
			shared++
		}
	}
	if shared < 2 {
		t.Fatalf("expected >=2 aliases on 192.0.2.50 (the conflict source), got %d", shared)
	}
	if !strings.Contains(text, conflictAddressShared) {
		t.Errorf("the artifact does not record the shared-address conflict (%s)", conflictAddressShared)
	}
	if !strings.Contains(text, "alpha") || !strings.Contains(text, "shared") {
		t.Errorf("the conflict sentence does not name the aliases that share the address:\n%s", text)
	}
	if !strings.Contains(text, conflictAliasInline) {
		t.Errorf("the artifact does not record the %s conflict (beta: Host=192.0.2.60, HostName=192.0.2.61)", conflictAliasInline)
	}
	// Both spellings survive: the inline address AND the HostName.
	if !strings.Contains(text, "192.0.2.60") || !strings.Contains(text, "192.0.2.61") {
		t.Errorf("one of the two contradictory addresses was dropped from the file:\n%s", text)
	}
}

// TestExplicitConflictClassIsNotResolved is the direct check on the wording: a
// conflict is not "resolved", and the file says how to settle it.
func TestRecordedConflictTellsTheReaderHowToSettleIt(t *testing.T) {
	text := Render(fixtureFacts(t))
	if !strings.Contains(text, "RECORDED, NOT RESOLVED") {
		t.Error("the artifact does not declare the conflicts recorded-not-resolved policy")
	}
	if !strings.Contains(text, "hostname") {
		t.Error("the artifact does not tell the reader how to settle a shared address (ask the node)")
	}
}

// TestAbsentDescriptionIsFatalToNothing is the RED arm: the file is RENAMED
// AWAY. The run must proceed — Load injects nothing, says so on stderr, and the
// caller's project context comes back byte-identical. This is the arm that goes
// red if a future change makes the description required.
func TestAbsentDescriptionIsFatalToNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("present"), fileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	renamed := filepath.Join(dir, FileName+".renamed")
	if err := os.Rename(path, renamed); err != nil {
		t.Fatalf("rename the description away: %v", err)
	}

	const projectContext = "project rules that must survive"
	var block Block
	var ok bool
	stderr := captureStderrValue(t, func() { block, ok = Load(path) })
	if ok {
		t.Fatalf("Load reported success with the description renamed away: %+v", block)
	}
	if block.Text() != "" {
		t.Errorf("an absent description produced a block: %q", block.Text())
	}
	if got := Combine(projectContext, block, ok); got != projectContext {
		t.Errorf("Combine changed the project context when nothing was injected: got %q, want %q", got, projectContext)
	}
	if !strings.Contains(stderr, "no environment description found") {
		t.Errorf("the absence was not reported on stderr: %q", stderr)
	}
	if !strings.Contains(stderr, path) {
		t.Errorf("the absence line does not name the path it looked at: %q", stderr)
	}
	if !strings.Contains(stderr, "gopi config env --write") {
		t.Errorf("the absence line does not carry the remedy: %q", stderr)
	}
}

// TestDescriptionMeasuredOnAnotherHostIsFlagged is the copied-file arm: a
// description that claims another hostname is injected WITH the mismatch said
// out loud, in the block and on stderr — never adopted silently as this
// machine's own inventory.
func TestDescriptionMeasuredOnAnotherHostIsFlagged(t *testing.T) {
	f := fixtureFacts(t)
	f.Hostname = "someotherhost"
	path := writeDescription(t, f)

	var block Block
	var ok bool
	stderr := captureStderrValue(t, func() { block, ok = Load(path) })
	if !ok {
		t.Fatalf("a copied description was dropped entirely; stderr=%s", stderr)
	}
	if !block.Stale() {
		t.Fatalf("the staleness check did not fire (measured on %q, current %q)", block.MeasuredOn, block.CurrentHost)
	}
	if !strings.Contains(block.Text(), "someotherhost") {
		t.Errorf("the block does not state the mismatched host:\n%s", block.Text())
	}
	if !strings.Contains(stderr, "someotherhost") {
		t.Errorf("the mismatch is not on stderr: %q", stderr)
	}
}

// TestSummaryBudgetIsEnforcedNotTruncated pins the density contract: an
// oversized summary is REFUSED by name, never silently halved — and the file
// stays on disk, complete, for the reader the pointer sends there.
func TestSummaryBudgetIsEnforcedNotTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := summaryBeginLine + "\n" + strings.Repeat("x", MaxSummaryChars+1) + "\n" + summaryEndLine + "\n"
	if err := os.WriteFile(path, []byte(body), fileMode); err != nil {
		t.Fatalf("seed oversized description: %v", err)
	}
	var ok bool
	stderr := captureStderrValue(t, func() { _, ok = Load(path) })
	if ok {
		t.Fatal("an over-budget summary was injected")
	}
	if !strings.Contains(stderr, "budget") {
		t.Errorf("the refusal does not name the budget: %q", stderr)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		t.Errorf("the file was altered by the budget check: err=%v size=%v", err, fi)
	}
}

// TestFileWithoutSummarySectionIsReportedByName covers the hand-edited file:
// present, not ours, and reported as such rather than half-parsed into a block.
func TestFileWithoutSummarySectionIsReportedByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("some other file entirely\n"), fileMode); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var ok bool
	stderr := captureStderrValue(t, func() { _, ok = Load(path) })
	if ok {
		t.Fatal("a file with no summary section produced a block")
	}
	if !strings.Contains(stderr, summaryBeginLine) {
		t.Errorf("the report does not name the marker it looked for: %q", stderr)
	}
}

// TestMissingSSHConfigStillProducesAFile is the other tolerance arm: a host
// with no ssh config gets a description that SAYS the source was missing —
// "no nodes" and "the node list could not be read" must not look alike.
func TestMissingSSHConfigStillProducesAFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOPI_HOME", "")
	f := Measure()
	if len(f.Nodes) != 0 {
		t.Fatalf("invented %d node(s) with no ssh config: %+v", len(f.Nodes), f.Nodes)
	}
	text := Render(f)
	if !strings.Contains(text, "MISSING") {
		t.Errorf("a missing source is not announced in the artifact:\n%s", text)
	}
	if !strings.Contains(text, "no readable alias") {
		t.Errorf("the node section does not distinguish 'none readable' from 'none exist':\n%s", text)
	}
}

// TestMeasureReportsThisHostsOwnFacts is the live arm: on the real host, the
// generator must produce a non-empty identity and a gateway for THIS machine —
// the properties the agent cannot know today.
func TestMeasureReportsThisHostsOwnFacts(t *testing.T) {
	f := Measure()
	if strings.TrimSpace(f.Hostname) == "" {
		t.Error("Measure produced no hostname")
	}
	if len(f.Sources) == 0 {
		t.Error("Measure recorded no source at all")
	}
	if f.GeneratedAt.IsZero() {
		t.Error("Measure produced no timestamp — a description without one cannot be judged stale")
	}
	text := Render(f)
	if !strings.Contains(text, f.Hostname) {
		t.Errorf("the artifact does not name the host it measured:\n%s", text)
	}
	if !strings.Contains(text, f.GeneratedAt.Format(time.RFC3339)) {
		t.Errorf("the artifact does not carry its generation timestamp:\n%s", text)
	}
}
