package agent

import (
	"os"
	"strconv"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tasksource"
)

// chat_provenance_test.go — the ordering contract that makes a chat row's
// `source` mean anything.
//
// MEASURED DEFECT (2026-10-03): a verification probe in the OPERATOR's own
// profile (`GOPI_AGENT_SESSION=<the agent's pid> gopi chat --once -p '…'`)
// wrote `human` into the chats table — the exact false claim the column exists
// to prevent. The cause was not the derivation, it was WHEN it ran: cmd/agent
// re-exports the anchor with the running process's OWN pid at startup
// (exportAgentSessionAnchor — correct for the spawn guard, whose question is
// "am I the session?"), and the chat row is written minutes later on the first
// completed turn. A live derivation at that moment therefore sees the process's
// own anchor and concludes "no agent above me".
//
// The fix is a snapshot taken before the overwrite. These tests pin BOTH
// halves: the snapshot survives the overwrite, and the snapshot is what the row
// is written with.

func TestCaptureConsoleProvenanceSurvivesTheAnchorOverwrite(t *testing.T) {
	// A FRESH process state for this test only: the package-level snapshot is
	// once-per-process, so the variables are reset around it (and restored).
	resetConsoleProvenanceForTest()

	// The inherited evidence: an agent's pid, i.e. an agent sits above us.
	t.Setenv(config.AgentSessionEnv, "probe-session")
	t.Setenv(config.AgentSessionAncestorEnv, "999999")
	t.Setenv(config.HumanTerminalEnv, "")

	CaptureConsoleProvenance()

	// …and now the process does what cmd/agent does at startup: it becomes the
	// session and overwrites the anchor with its own pid.
	t.Setenv(config.AgentSessionAncestorEnv, strconv.Itoa(os.Getpid()))

	if got := chatSessionSource(); got != tasksource.Agent {
		t.Errorf("chat provenance after the anchor overwrite = %q, want %q — the reading must be taken BEFORE "+
			"the process claims the session, or every verification probe records as the operator's own console", got, tasksource.Agent)
	}
}

func TestChatProvenanceIsHumanWithoutAnAgentChain(t *testing.T) {
	resetConsoleProvenanceForTest()

	// The operator's own console: nothing above us at all.
	t.Setenv(config.AgentSessionEnv, "")
	t.Setenv(config.AgentSessionAncestorEnv, "")
	t.Setenv(config.HumanTerminalEnv, "")

	CaptureConsoleProvenance()
	if got := chatSessionSource(); got != tasksource.Human {
		t.Errorf("an unclaimed console recorded %q, want %q", got, tasksource.Human)
	}
}

// TestChatProvenanceCaptureIsIdempotent is the anti-regression for the second
// half of the trap: `gopi chat` reaches printLogPaths more than once (main's
// banner and the REPL's own), and cmd/agent calls the capture beside the
// anchor export on every one of those paths. A second capture must NOT re-read
// an environment the first one already saw overwritten — that is the same bug
// one call later.
func TestChatProvenanceCaptureIsIdempotent(t *testing.T) {
	resetConsoleProvenanceForTest()

	// The env shape below is the WHOLE fixture, so each half is load-bearing:
	//
	//   - the session handle is PRESENT, and
	//   - the anchor carries a pid that is NOT ours (999999 — any other pid
	//     does), so AnchorInherited is true while AnchorSelf stays false: an
	//     agent sits ABOVE this process. That reading — inherited anchor,
	//     no self-claim — is the accepted agent signature AgentRun() returns
	//     true for, and it is why the value need not name a live process
	//     (config reads the anchor as a pid, and a foreign pid is a chain).
	//   - the human-terminal claim is PINNED OFF, and that pin is the half
	//     that is easy to lose: AgentRun() consults HumanTerminal FIRST, before
	//     the anchor, so an INHERITED GOPI_HUMAN_TERMINAL=1 (the shape the
	//     heavy path exports — `make test-heavy` runs the whole suite with it)
	//     makes this process "the operator's own console" and the FIRST capture
	//     reads `human`; the latch then correctly hands back `human` and this
	//     test reds for a reason that has nothing to do with the ordering it
	//     exists to pin. MEASURED 2026-10-04 on master 05c7d78ca: green in a
	//     clean environment, red with exactly "the second capture overwrote the
	//     first: \"human\", want \"agent\"" under GOPI_HUMAN_TERMINAL=1. The two
	//     siblings above carry the same pin; this one lost it.
	t.Setenv(config.AgentSessionEnv, "probe-session")
	t.Setenv(config.AgentSessionAncestorEnv, "999999")
	t.Setenv(config.HumanTerminalEnv, "")
	CaptureConsoleProvenance()

	// A later call, made after the process overwrote the anchor with its own
	// pid AND dropped the session handle, must return the FIRST reading.
	t.Setenv(config.AgentSessionAncestorEnv, strconv.Itoa(os.Getpid()))
	t.Setenv(config.AgentSessionEnv, "")
	CaptureConsoleProvenance()

	if got := chatSessionSource(); got != tasksource.Agent {
		t.Errorf("the second capture overwrote the first: %q, want %q", got, tasksource.Agent)
	}
}
