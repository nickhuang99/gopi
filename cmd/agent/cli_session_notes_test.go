package main

// cli_session_notes_test.go — `gopi session notes`: the REAL binary reading the
// round-level note index.
//
// WHY AN END-TO-END ARM. The tools-layer test proves the index answers a query;
// the agent-layer test proves the recorder writes to it. Neither can catch a
// defect in the SEAM BETWEEN THEM — a CLI that opens a different database, a
// sub-verb that is not dispatched, a page that promises a flag the parser
// refuses. This file runs the built binary, exactly as an operator would.
//
// The row is seeded through the PRODUCTION writer (tools.ChatNoteRecord) into the
// database the scaffold handed this test (Rule 32), and the child then opens that
// same file — one opener, two processes, which is exactly the property under
// test.

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// notesIndexDBEnv is the chat-index override this probe must scrub from the
// inherited environment: with it set, the child would read a DIFFERENT database
// than the one the fixture seeded. It mirrors internal/tools' private constant
// (one env name, spelled here because the scrub list must name it).
const notesIndexDBEnv = "GOPI_CHAT_DB"

// notesHostEnv is the node-identity override: cleared so the machine the row
// carries is the one this process really resolves.
const notesHostEnv = "GOPI_SYNC_NODE"

// notesTestProfileEnv is the environment for a notes probe: this process's own
// environment minus every gopi-data knob, plus the isolated profile root. The
// ambient scrub matters — an inherited GOPI_MEMORY_DB or GOPI_CHAT_DB would send
// the probe at the OPERATOR's live index instead of the fixture.
func notesTestProfileEnv(t *testing.T, profileRoot, dbPath string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case config.GopiDirEnv, config.MemoryDBEnv, config.MemoryDBEnvLegacy, notesIndexDBEnv, notesHostEnv:
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		config.GopiDirEnv+"="+profileRoot,
		config.MemoryDBEnv+"="+dbPath,
		notesHostEnv+"=",
	)
}

// TestSessionNotesReadsTheIndexThroughTheRealBinary is the CLI contract: the
// verb is dispatched, it answers with the seeded note, it names the tier that
// matched, and it prints the machine and the FULL path — the two provenance
// facts that disambiguate two hosts whose home directories are the same string.
func TestSessionNotesReadsTheIndexThroughTheRealBinary(t *testing.T) {
	t.Setenv(notesHostEnv, "")
	// The scaffold's writer shape: a fresh per-test database, the ledger env
	// pinned at it, the operator's profile refused (Rule 32).
	dbPath := testutil.NewTestDBPath(t)
	profileRoot := t.TempDir()
	env := notesTestProfileEnv(t, profileRoot, dbPath)

	const notePath = "/home/user/.gopi/sessions/sess-cli/self-record.md"
	if err := tools.ChatNoteRecord(tools.ChatNote{
		SessionID: "sess-cli",
		Turn:      7,
		Note:      "记录：轮级索引已上线，entrypoint note-cli-needle",
		Path:      notePath,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The REAL verb, in a process that sees only that database.
	out, err := runNotesProbe(t, env, CliVerbSession, sessionSubcmdNotes, "note-cli-needle")
	if err != nil {
		t.Fatalf("`gopi session notes` failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"note-cli-needle", "via fts", "sess-cli", "turn 7",
		tools.OriginHostName(), notePath,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the CLI output is missing %q:\n%s", want, out)
		}
	}

	// The CJK route through the same real binary: a fragment INSIDE a longer
	// run, which the unicode61 index cannot answer (a contiguous CJK run is one
	// token there).
	out, err = runNotesProbe(t, env, CliVerbSession, sessionSubcmdNotes, "轮级索引")
	if err != nil {
		t.Fatalf("`gopi session notes 轮级索引` failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "via trigram") {
		t.Fatalf("a CJK fragment must be answered by the trigram tier:\n%s", out)
	}

	// And the refusal: no query is a USAGE error, never an empty search that
	// reads as "you have no notes".
	out, err = runNotesProbe(t, env, CliVerbSession, sessionSubcmdNotes)
	if err == nil {
		t.Fatalf("a bare `gopi session notes` must be refused, got exit 0:\n%s", out)
	}
	if !strings.Contains(out, "give a query") {
		t.Fatalf("the refusal must say what is missing:\n%s", out)
	}
}

// TestSessionNotesHelpIsItsOwnPageAndTheFamilyListsTheVerb pins the two
// discoverability contracts: `-h` prints THIS verb's page (not the family page),
// and the family's own usage teaches the word, so `gopi session <TAB>` — and the
// page a reader lands on — reach it.
func TestSessionNotesHelpIsItsOwnPageAndTheFamilyListsTheVerb(t *testing.T) {
	t.Setenv(notesHostEnv, "")
	dbPath := testutil.NewTestDBPath(t)
	env := notesTestProfileEnv(t, t.TempDir(), dbPath)

	out, err := runNotesProbe(t, env, CliVerbSession, sessionSubcmdNotes, HelpFlag)
	if err != nil {
		t.Fatalf("`gopi session notes -h` failed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, sessionNotesUsageFingerprint) {
		t.Fatalf("the sub-verb must answer its OWN page:\n%s", out)
	}
	if !strings.Contains(out, sessionNotesLimitFlag) {
		t.Fatalf("the verb's page must advertise its own flag:\n%s", out)
	}

	family, err := runNotesProbe(t, env, CliVerbSession, HelpFlag)
	if err != nil {
		t.Fatalf("`gopi session -h` failed: %v\n%s", err, family)
	}
	if !strings.Contains(family, CliVerbSession+" "+sessionSubcmdNotes) {
		t.Fatalf("the family page must teach `gopi session %s`:\n%s", sessionSubcmdNotes, family)
	}
}

// runNotesProbe runs the built binary with the notes probe's environment.
func runNotesProbe(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(gopiTestBinPath(t), args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}
