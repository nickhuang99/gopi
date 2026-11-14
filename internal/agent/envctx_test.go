package agent

// envctx_test.go — the assembly proof: this host's environment description
// rides the EXISTING project-context system message on BOTH agent paths, and
// its absence changes nothing but a stderr line.
//
// The two arms are the epic's acceptance in miniature:
//
//	present  the summary AND the file's path are in the string the agent's
//	         Context carries — the same string the task path (loop.go) and the
//	         chat paths (chat_ops.go, the chat request builder) all send.
//	absent   the file RENAMED AWAY: the returned context carries no environment
//	         text at all (byte-identical to the project context alone), the
//	         absence is on stderr with the remedy, and nothing errors.
//
// The fixture is written under a temp GOPI_HOME, so the test measures a
// description it created rather than whatever this developer machine happens to
// have on disk.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/envctx"
)

// envBlockFixture writes a description into a temp GOPI_HOME's profile and
// returns (profileFile, home).
func envBlockFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	path, err := envctx.Path()
	if err != nil {
		t.Fatalf("envctx.Path under a temp GOPI_HOME: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(path, []byte(envctx.Render(envBlockFacts())), 0o600); err != nil {
		t.Fatalf("write description: %v", err)
	}
	return path
}

// envBlockFacts is a description with the one property the epic cares about:
// two aliases on one address, recorded as a conflict.
func envBlockFacts() envctx.Facts {
	return envctx.Facts{
		Hostname:    "fixturehost",
		OS:          "Fixture Linux 1.0",
		Addresses:   []string{"eth0  192.0.2.9"},
		Gateway:     "192.0.2.1  (via eth0)",
		Sources:     []string{"live measurement (this run)"},
		Conflicts:   nil,
		Nodes:       nil,
		GeneratedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
}

// TestProjectContextBlockCarriesTheEnvironmentSummary is the injection proof:
// the block reaches the agent's context, and it reaches it as a POINTER (the
// file's absolute path is in the text) rather than as the whole document.
func TestProjectContextBlockCarriesTheEnvironmentSummary(t *testing.T) {
	path := envBlockFixture(t)
	read := captureStderr(t)
	got := projectContextBlock()
	stderr := read()

	if !strings.Contains(got, "## System environment (this host)") {
		t.Fatalf("the environment block is not in the agent's context:\n%s", got)
	}
	if !strings.Contains(got, path) {
		t.Errorf("the block does not carry the pointer to the full file (%s):\n%s", path, got)
	}
	if !strings.Contains(got, "fixturehost") {
		t.Errorf("the block does not carry the measured host:\n%s", got)
	}
	if !strings.Contains(got, "gopi config env --write") {
		t.Errorf("the block does not say how to re-measure:\n%s", got)
	}
	if !strings.Contains(stderr, envctx.FileName) {
		t.Errorf("the load was not reported on stderr: %q", stderr)
	}
	// The block is a SUMMARY, not the document: the node table's header must
	// not be in the always-resident text (it is what `read` is for).
	if strings.Contains(got, "## Nodes — TEXT") {
		t.Errorf("the whole document was injected instead of the summary+pointer:\n%s", got)
	}
}

// TestProjectContextBlockIsUnchangedWhenTheDescriptionIsAbsent is the RED arm:
// rename the file away and the agent's context must come back as the project
// context ALONE — no environment text, no error, and the absence stated on
// stderr with the remedy.
func TestProjectContextBlockIsUnchangedWhenTheDescriptionIsAbsent(t *testing.T) {
	path := envBlockFixture(t)
	if err := os.Rename(path, path+".renamed"); err != nil {
		t.Fatalf("rename the description away: %v", err)
	}

	read := captureStderr(t)
	got := projectContextBlock()
	stderr := read()

	if strings.Contains(got, "## System environment (this host)") {
		t.Errorf("a renamed-away description still produced a block:\n%s", got)
	}
	if got != LoadProjectContext("") {
		t.Errorf("the context differs from the project context alone.\ngot:  %q\nwant: %q", got, LoadProjectContext(""))
	}
	if !strings.Contains(stderr, "no environment description found") {
		t.Errorf("the absence is not on stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "gopi config env --write") {
		t.Errorf("the absence line does not carry the remedy: %q", stderr)
	}
}

// TestChatPromptDoesNotDuplicateTheEnvironmentBlock guards the RULE this
// injection depends on: the block is carried ONCE, through Context.ProjectContext,
// which every prompt path already shares. If a path started rendering it
// separately the machine's inventory would be paid for twice per turn.
func TestChatPromptDoesNotDuplicateTheEnvironmentBlock(t *testing.T) {
	envBlockFixture(t)
	read := captureStderr(t)
	ctx := projectContextBlock()
	_ = read()

	if n := strings.Count(ctx, "## System environment (this host)"); n != 1 {
		t.Errorf("the environment block appears %d times in the assembled context, want exactly 1", n)
	}
	// The chat and task request builders both take Context.ProjectContext; the
	// count above plus this assertion is what keeps a second renderer from
	// appearing without a test noticing.
	if strings.Count(chatSystemPromptServed(&Agent{}), "## System environment") != 0 {
		t.Error("the chat SYSTEM PROMPT renders the environment block — it belongs on the shared context, not in a per-path prompt")
	}
}
