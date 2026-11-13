package main

// chat_replay_dump_catalogue_test.go — THE SCREEN DUMP A REPLAY MUST NOT WRITE
// INTO THE RECORDING, and the reason the catalogue's "EXHAUSTIVE" claim is now a
// MACHINE rather than a comment (epic: `gopi chat --replay` must be really
// read-only).
//
// THE DEFECT, MEASURED (2026-10-26). `chatResolveDumpPath` had three branches
// and none of them asked `replaymode.Active()`:
//
//	if ag != nil && ag.SessionLog != nil {
//	    return filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), uiclient.ChatDumpFileName), nil
//	}
//
// While a replay is armed, `ag.SessionLog` IS the recording's own logger when the
// agent was handed one — so that branch writes `chat-screen.dump` INTO the very
// session directory the run is re-running, and the guarantee the verb prints
// ("a replay writes nothing at all: it reads the recording in place") becomes
// false. The catalogue in cmd/agent/chat_replay.go had SEVEN entries and
// `grep -ci dump` on its output was 0.
//
// WHY THE SECOND HALF OF THIS FILE EXISTS. The catalogue's own comment invited a
// reader to compare it against the call sites of `replaymode.Active()`. That is a
// claim with no machine behind it, and the screen dump is what it cost: the gate
// existed, the invitation existed, and the site was still uncatalogued. So the
// invitation became `TestChatReplayDumpCatalogueIsExhaustive` below: every
// PRODUCTION call site of the flag must be claimed by a row of
// `replayActiveSiteRows`, and every catalogue phrase those rows name must appear
// in `replayRefusalCatalogue()`'s output. A new gate with no catalogue entry
// fails; a catalogue entry with no gate fails; a gate nobody accounted for fails.
//
// THE RED PROOF IS RECORDED, not asserted here (Rule 5's `Ran:`/`Result:`): this
// file is written against the FIXED tree, and the fix is what makes
// TestChatReplayDumpStaysOutsideTheRecording green — see the task report for the
// run of these same test names against `git show HEAD:cmd/agent/chat_dump.go`
// (the pre-fix resolver), where the dump DOES land in the recording directory and
// the case is RED.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/uiclient"
)

// replayCatalogueSite is ONE production call site of `replaymode.Active()` and
// the catalogue phrase that announces it. The phrase is matched against
// replayRefusalCatalogue()'s OUTPUT (the sentence the run actually prints), never
// against this table, so the table cannot vouch for itself.
type replayCatalogueSite struct {
	// file is the repo-relative path of the file holding the gate.
	file string
	// symbol is the function the gate lives in, so a reader can find it without
	// a line number that the next edit invalidates.
	symbol string
	// phrase is the substring of replayRefusalCatalogue()'s output that names
	// this write. It is the JOIN between code and announcement.
	phrase string
}

// replayActiveSiteRows is THE enumeration: one row per production gate on the
// replay flag. It is exhaustive BY THE MACHINE BELOW, not by this comment — a
// gate missing from the table fails the walk, and a row with no live gate fails
// the same case, so the table cannot ratchet up by neglect or down by a
// forgotten edit.
//
// `cmd/agent/chat_dump.go` carries TWO gates and one row: they answer the same
// question (where may the dump go? / which sentence announces it?) for the same
// write, and both are cells of that write's answer.
// replayDumpTestSessionID names the recording the dump case replays: the id the
// production minter turns into <root>/<id>/session.jsonl.
const replayDumpTestSessionID = "11111111-2222-4333-8444-5555555555dd"

var replayActiveSiteRows = []replayCatalogueSite{
	// The session-record pair. The gate lives in the RESUMED function of each
	// pair and nowhere else: `NewRawLoggerForRun` / `NewSessionLoggerForRun` are
	// one-line delegations to their resumed counterpart (`return
	// NewRawLoggerForResumedRun(taskID, RunIdentity{SessionID: runUUID})`), so the
	// row names the function that actually asks the flag — a row naming the
	// delegator is a claim about a call site that does not exist, which is what
	// direction 2 of the test catches (measured: naming the delegators instead
	// left the walk reporting both real gates as unclaimed AND both rows as
	// gates that no longer ask the flag).
	{"internal/agent/rawlog.go", "NewRawLoggerForResumedRun", "this run's own session record"},
	{"internal/agent/session.go", "NewSessionLoggerForResumedRun", "this run's own session record"},
	{"internal/agent/chatregister.go", "chatRegisterEnabled", "the chats table"},
	{"internal/agent/memory_replay_guard.go", "replayCaptureSkipped", "every auto-memory store"},
	{"internal/agent/memory_replay_guard.go", "memoryWriteReplayRefusal", "every auto-memory store"},
	{"internal/agent/selfrecord.go", "recordSelfNotes", "every auto-memory store"},
	{"cmd/agent/chat_session_slots.go", "chatSessionSlotRegister", ChatSessionSlotPrefix},
	{"cmd/agent/lineedit_wire.go", "chatHistoryPath", chatHistoryFileName},
	{"cmd/agent/lineedit_wire.go", "newChatInputWithPalette", chatHistoryFileName},
	{"cmd/agent/chat_prefetch.go", "chatPrefetchFor", "memory prefetch"},
	{"cmd/agent/chat_prefetch.go", "fetch", "memory prefetch"},
	{"cmd/agent/chat_prefetch_turn.go", "startTurnLookup", "memory prefetch"},
	// The screen-dump / raw-SGR-stream pair. Both rows carry the phrase WITHOUT
	// its last five words, on purpose. A phrase is matched against
	// replayRefusalCatalogue()'s OUTPUT by substring, so a phrase that runs to
	// the very end of its entry silently depends on its own entry being the
	// LAST one — and the raw-stream entry now follows it (measured: with the
	// full tail in the phrase, adding the stream row below turned BOTH these
	// rows red at direction 3, with the catalogue naming both writes and this
	// table simply asking for a string that no longer existed contiguously).
	// The stable part is what identities the write; the tail is prose.
	{"cmd/agent/chat_dump.go", "chatResolveDumpPath", "the screen dump"},
	{"cmd/agent/chat_dump.go", "chatDumpPathNotice", "the screen dump"},
	// The RAW SCREEN BYTE STREAM (cmd/agent/chat_sgr_stream.go, commit 1d9b0487; the
	// phrase reads "screen" since the 2026-11-08 sgr→screen rename — the bytes are a
	// screen paint sequence, and a model told "raw SGR stream" was being told the
	// name of one escape class inside it).
	// Same redirect as the dump and for the same measured reason: a replay reads
	// the recording, so an armed stream must not land in it. The row names
	// chatResolveSgrPath because THAT is where the flag is asked — the print-side
	// `createSgrStream`/`chatSgrStreamNotice` path deliberately asks nothing (one
	// gate per write, Rule 13), so it holds no gate for this table to claim.
	{"cmd/agent/chat_sgr_stream.go", "chatResolveSgrPath", "the raw screen byte stream"},
	// THE START-UP GROUND LINE is the ONE row here that is not a WRITE: it is the
	// sentence a write claim would appear in. `chatGroundReport` renders the
	// ordinary "this session writes in <dir> … lands uncommitted …" line, which
	// under a replay states a behaviour the flag above stops — measured on master
	// before this row existed: `gopi chat --replay <id>` printed that sentence
	// while the refusal catalogue two lines below said every write was
	// short-circuited. It is claimed here because the machine's question is "does
	// every site that asks the flag announce its answer?", and this site's answer
	// is a DIFFERENT SENTENCE, not a suppressed write.
	{"internal/tools/chat_ground.go", "ChatGroundFor", "the start-up ground line"},
}

// replayActiveSitesUnderRepo walks the tree for PRODUCTION (non-test) Go files
// that call replaymode.Active(), and returns "path:symbol" for every such call —
// the symbol being the enclosing func, resolved by scanning backwards for the
// nearest `func ` line. Line numbers are deliberately NOT part of the identity:
// this is a census of SITES, and a site that moves is the same site.
//
// The walk covers the whole tree rather than `internal/` + `cmd/`, because the
// flag's package is importable from anywhere and a gate in a third directory
// would be exactly the site nobody remembers to catalogue.
func replayActiveSitesUnderRepo(t *testing.T) map[string]map[string]bool {
	t.Helper()
	root := repoRoot(t)
	sites := map[string]map[string]bool{}
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "bin", "devdoc":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		lines := strings.Split(string(data), "\n")
		symbol := ""
		for i, line := range lines {
			if strings.HasPrefix(line, "func ") {
				symbol = line
			}
			if !strings.Contains(line, "replaymode.Active()") {
				continue
			}
			// A COMMENT that mentions the flag is not a gate. The measurement
			// that made this file necessary was taken with a `grep` that counted
			// comment lines as sites; the catalogue's old prose did the same. The
			// code below asks the ONE question the flag asks: is the call in a
			// statement, or is it being named?
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			_ = i
			if symbol == "" {
				symbol = line
			}
			if sites[rel] == nil {
				sites[rel] = map[string]bool{}
			}
			sites[rel][symbol] = true
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("the tree could not be walked from %s: %v", root, walkErr)
	}
	return sites
}

// symbolName pulls the identifier out of a `func …` declaration line, so the
// table's `symbol` and the walk's answer compare as names rather than spellings.
func symbolName(decl string) string {
	decl = strings.TrimSpace(strings.TrimPrefix(decl, "func "))
	// Method form: "(a *Agent) name(" — take the tail after the receiver. The
	// receiver is recognised by its OPENING paren, never by ")" anywhere in the
	// line: an ordinary declaration's own empty parameter list ("name() bool")
	// also contains ") ", and reading that as a receiver answered "bool" for
	// every such gate (measured while writing this case).
	if strings.HasPrefix(decl, "(") {
		if k := strings.Index(decl, ") "); k >= 0 {
			decl = decl[k+2:]
		}
	}
	if k := strings.IndexAny(decl, "( "); k >= 0 {
		decl = decl[:k]
	}
	return strings.TrimSpace(decl)
}

// TestChatReplayDumpCatalogueIsExhaustive is THE machine replacement for the
// catalogue's old prose claim. It fails in THREE directions:
//
//  1. a production call site of replaymode.Active() that no row claims — the
//     screen dump's own failure mode, caught at the moment it is written;
//  2. a row whose gate no longer exists — an entry that outlived its code is as
//     misleading as a missing one, and it would let the list swell quietly;
//  3. a row whose phrase is absent from replayRefusalCatalogue()'s OUTPUT — the
//     join between the code and the sentence the run prints at arm time.
//
// Non-vacuity is enforced too: the walk must find at least the two directories'
// worth of sites, and the catalogue must not be empty, so a walk that read
// nothing cannot pass as a clean tree (Rule 15 — a scan that found nothing must
// be distinguishable from a scan that never ran).
func TestChatReplayDumpCatalogueIsExhaustive(t *testing.T) {
	sites := replayActiveSitesUnderRepo(t)
	if len(sites) < 2 {
		t.Fatalf("the walk found gates in %d file(s) — a tree this size with the flag in use has more, so this is a broken scan, not a catalogue problem (found: %v)", len(sites), sites)
	}

	claimed := map[string]map[string]bool{}
	for _, row := range replayActiveSiteRows {
		if claimed[row.file] == nil {
			claimed[row.file] = map[string]bool{}
		}
		claimed[row.file][row.symbol] = true
	}

	// DIRECTION 1: every live gate is claimed.
	var unclaimed []string
	for file, syms := range sites {
		for sym := range syms {
			if !claimed[file][symbolName(sym)] {
				unclaimed = append(unclaimed, file+": "+symbolName(sym))
			}
		}
	}
	sort.Strings(unclaimed)
	if len(unclaimed) > 0 {
		t.Fatalf("these production gates on replaymode.Active() are in NO catalogue row — a new write the replay does not announce:\n  %s\nAdd a row to replayActiveSiteRows AND a phrase to replayRefusalCatalogue().", strings.Join(unclaimed, "\n  "))
	}

	// DIRECTION 2: every row's gate still exists.
	var stale []string
	for _, row := range replayActiveSiteRows {
		if !sites[row.file][row.symbol] && !hasSymbolNamed(sites[row.file], row.symbol) {
			stale = append(stale, row.file+": "+row.symbol)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("these catalogue rows name gates that no longer ask replaymode.Active() — delete the row (and its catalogue phrase) or restore the gate:\n  %s", strings.Join(stale, "\n  "))
	}

	// DIRECTION 3: every phrase is in the sentence the run PRINTS.
	catalogue := replayRefusalCatalogue()
	if strings.TrimSpace(catalogue) == "" {
		t.Fatalf("replayRefusalCatalogue() is empty: the arm-time sentence would announce nothing")
	}
	var unannounced []string
	for _, row := range replayActiveSiteRows {
		if !strings.Contains(catalogue, row.phrase) {
			unannounced = append(unannounced, row.file+": "+row.symbol+" wants phrase "+row.phrase)
		}
	}
	sort.Strings(unannounced)
	if len(unannounced) > 0 {
		t.Fatalf("these gates are not announced by replayRefusalCatalogue() — the code refuses a write the run never says it refuses (Rule 15):\n  %s\ncatalogue:\n%s", strings.Join(unannounced, "\n  "), catalogue)
	}
}

// hasSymbolNamed reports whether the walk saw a function whose NAME matches, so a
// rename of the receiver spelling does not read as a deleted gate.
func hasSymbolNamed(syms map[string]bool, name string) bool {
	for decl := range syms {
		if symbolName(decl) == name {
			return true
		}
	}
	return false
}

// TestChatReplayDumpCatalogueNamesTheScreenDump is the operator-visible half: the
// sentence printed at arm time must NAME the screen dump, and must say it was
// REDIRECTED rather than dropped (the artifact still exists — it just cannot land
// in the recording). The assertion is on the printed output, not on a private
// variable, because the output is what a reader of the run has.
func TestChatReplayDumpCatalogueNamesTheScreenDump(t *testing.T) {
	catalogue := replayRefusalCatalogue()
	if !strings.Contains(catalogue, "screen dump") {
		t.Fatalf("the refusal catalogue does not name the screen dump:\n%s", catalogue)
	}
	if !strings.Contains(catalogue, "redirected") {
		t.Fatalf("the catalogue names the screen dump but does not say it is redirected rather than dropped — a reader would conclude the replay has no dump at all:\n%s", catalogue)
	}
	// The arm-time SENTENCE (not just the list) says which one is the redirect:
	// a reader of stderr never sees the list on its own.
	if !strings.Contains(replayRefusalNotice, "REDIRECTED") {
		t.Fatalf("the arm-time sentence does not distinguish a redirected write from a refused one:\n%s", replayRefusalNotice)
	}
}

// replayDumpTestSessionLog builds a REAL SessionLogger for a recording on disk —
// the exact object chatResolveDumpPath's session branch tests for — so the case
// below measures the code path that WAS the defect rather than a stand-in for it.
//
// The logger is minted by the PRODUCTION minter (agent.NewSessionLoggerForRun)
// while no replay is armed, into a sessions root the case owns
// (GOPI_SESSIONS_ROOT), so `Path()` is a real path on disk with real session
// artifacts in it rather than a struct literal in another package.
func replayDumpTestSessionLog(t *testing.T) (*agent.SessionLogger, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(config.SessionsRootEnv, root)
	logger, err := agent.NewSessionLoggerForRun("replay-dump-catalogue-test", "", replayDumpTestSessionID, nil)
	if err != nil {
		t.Fatalf("the production session minter failed: %v", err)
	}
	if logger == nil || logger.Path() == "" {
		t.Fatalf("the session minter returned no logger (a replay flag left armed by an earlier case would do this): logger=%v", logger)
	}
	return logger, filepath.Dir(logger.Path())
}

// TestChatReplayDumpStaysOutsideTheRecording is THE regression for the measured
// defect, on the real resolver with the real object in hand.
//
// The setup is the defect's own premise, verbatim: a recording directory on disk
// (session.jsonl + metadata.json, the files a replay reads and must not touch)
// and an agent whose SessionLog points at it. Before the fix, the resolver's
// session-directory branch answered
// `<recording>/chat-screen.dump` — a path INSIDE the tree being replayed. After
// it, the answer is a scratch path, and the recording directory's file list and
// byte contents are unchanged.
//
// THE CONTROL ARM IS WHAT MAKES THIS EVIDENCE: with the flag disarmed the SAME
// call must resolve INSIDE the session directory. Without it, a resolver that had
// simply lost its session branch would pass for the right reason and be wrong for
// every ordinary run.
func TestChatReplayDumpStaysOutsideTheRecording(t *testing.T) {
	ag, recordingDir := func() (*agent.Agent, string) {
		logger, dir := replayDumpTestSessionLog(t)
		return &agent.Agent{SessionLog: logger}, dir
	}()
	recordingPath := filepath.Join(recordingDir, "session.jsonl")
	if _, err := os.Stat(recordingPath); err != nil {
		t.Fatalf("the minter's session record is not at %s: %v — the case would measure a directory that holds no recording", recordingPath, err)
	}

	// Snapshot the recording tree: file list + contents, read through the same
	// helper for the before and after halves so the two are comparable by
	// construction (the shape chat_replay_readonly_writes_test.go established).
	before := replayWritesProfileBytes(t, recordingDir)

	// CONTROL: not replaying ⇒ the dump lands BESIDE the recording. This is the
	// behaviour every ordinary chat depends on (`gopi ui_state --target chat`
	// scans the sessions tree), so a fix that redirected unconditionally would be
	// a different defect wearing a green test.
	ordinary, err := chatResolveDumpPath(chatDumpDefault(), ag)
	if err != nil {
		t.Fatalf("the ordinary (not-replaying) resolver failed: %v", err)
	}
	if want := filepath.Join(recordingDir, uiclient.ChatDumpFileName); ordinary != want {
		t.Fatalf("with no replay armed the dump resolved to %q — want %q (beside the session record). The control is missing, so the armed reading below proves nothing", ordinary, want)
	}

	// ARMED: replaying ⇒ the dump is OUTSIDE the recording tree.
	replaymode.Arm(recordingPath)
	t.Cleanup(replaymode.Disarm)
	if !replaymode.Active() {
		t.Fatalf("replaymode.Active() is false after Arm: the flag the resolver consults is not in force")
	}
	got, err := chatResolveDumpPath(chatDumpDefault(), ag)
	if err != nil {
		t.Fatalf("the replay resolver failed: %v", err)
	}
	if got == "" {
		t.Fatalf("a replay resolved NO dump path: the artifact is a visualization product and must still exist, outside the recording (the fix is a redirect, not a refusal)")
	}
	absGot, aerr := filepath.Abs(got)
	if aerr != nil {
		t.Fatalf("the resolved path %q could not be made absolute: %v", got, aerr)
	}
	absRec, rerr := filepath.Abs(recordingDir)
	if rerr != nil {
		t.Fatalf("the recording directory could not be made absolute: %v", rerr)
	}
	if absGot == absRec || strings.HasPrefix(absGot, absRec+string(filepath.Separator)) {
		t.Fatalf("a replay resolved its dump to %q — INSIDE the recording tree %q. A replay writes nothing into the recording it re-runs; the dump belongs in the chat's own scratch class", absGot, absRec)
	}
	if filepath.Dir(absGot) == absRec {
		t.Fatalf("a replay resolved its dump beside the recording (%q): the same session directory", absGot)
	}

	// An EXPLICIT path is redirected too: a command line can name the recording
	// by accident, and the resolver cannot tell that from a deliberate choice.
	explicit := filepath.Join(recordingDir, "operator-asked-for-this.dump")
	gotExplicit, err := chatResolveDumpPath(chatDumpOptions{path: explicit, requested: true, stated: true}, ag)
	if err != nil {
		t.Fatalf("the replay resolver failed for an explicit path: %v", err)
	}
	absExplicit, aerr2 := filepath.Abs(gotExplicit)
	if aerr2 != nil {
		t.Fatalf("the explicit-path answer %q could not be made absolute: %v", gotExplicit, aerr2)
	}
	if absExplicit == absRec || strings.HasPrefix(absExplicit, absRec+string(filepath.Separator)) {
		t.Fatalf("a replay honoured an explicit dump path INSIDE the recording tree (%q): the redirect must not depend on the operator's spelling", gotExplicit)
	}

	// THE RECORDING ITSELF: unchanged, file for file, byte for byte.
	after := replayWritesProfileBytes(t, recordingDir)
	if len(after) != len(before) {
		t.Fatalf("the recording directory's file count moved: %d before, %d after", len(before), len(after))
	}
	names := make([]string, 0, len(before))
	for name := range before {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if got := after[name]; got != before[name] {
			t.Fatalf("the recording entry %s changed while a replay resolved its dump:\n got %q\nwant %q", name, got, before[name])
		}
	}
}
