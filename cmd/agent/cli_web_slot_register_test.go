package main

// cli_web_slot_register_test.go — the WEB console's entry in the chat slot
// registry (chat_session_slots.go's RegisterChatSessionSlot, armed from runWeb).
//
// THE DEFECT THIS PINS (measured 2026-11-08): `gopi web` started without writing
// ~/.gopi/chat_session_id_<0..9>.txt, so `notify.cmd`'s broadcast — the
// claimed/completed/failed events the ledger's own machinery execs — had no
// address for a web session and delivered nothing to it. `gopi chat` had the
// ARM-POINT all along (chatSessionSlotRegister, one caller in
// chat_surface_surface.go); the web face simply never called it.
//
// WHY THE REAL BINARY AND NOT A UNIT CALL (Rule 31): the deliverable is a CALL
// SITE, and a call site that no run reaches is exactly the shape a unit test
// cannot see. So this test starts `gopi web --addr=127.0.0.1:0` in an isolated
// profile and reads the registry off disk — the same reading the sink command
// does.
//
// WHAT IT ASSERTS, and why each half matters separately:
//
//	PRESENCE   some chat_session_id_*.txt names <this run's session directory> —
//	           the address half `gopi notify --session` resolves;
//	LIVENESS   that entry's pid half names a process that is running RIGHT NOW
//	           — a slot whose pid is dead is reclaimable and gets reused, so
//	           "a file exists" alone would pass on a stale entry.
//
// The identity is spelled by config.FormatChatSurfaceIdentity on both sides, so
// this file does not re-invent the `<pid>:<session-dir>` shape (Rule 13).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// webSlotTestReply is the mock provider's answer; no turn is even needed for the
// registration, but the server needs a provider to start.
const webSlotTestReply = "OK"

// webSlotRegistryEntries reads every slot file in a profile's registry and
// returns their trimmed contents, keyed by path. A missing directory is empty,
// not an error: the assertion is about what is THERE.
func webSlotRegistryEntries(t *testing.T, gopiDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	matches, err := filepath.Glob(filepath.Join(gopiDir, ChatSessionSlotPrefix+"*"+ChatSessionSlotSuffix))
	if err != nil {
		t.Fatalf("glob the slot registry in %s: %v", gopiDir, err)
	}
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("read slot %s: %v", m, err)
		}
		out[m] = strings.TrimSpace(string(data))
	}
	return out
}

// TestWebRunRegistersItsChatSlot is the deliverable's regression guard: a real
// `gopi web` launch leaves a LIVE entry for its own session in the registry the
// sink reads.
func TestWebRunRegistersItsChatSlot(t *testing.T) {
	home := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: webSlotTestReply})
	defer llm.Close()

	p := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"}, "",
		testutil.MockEnv(t, home, llm.URL()), "")
	defer p.Stop()

	// The session record is the readiness signal the sibling live cases use: it
	// is created at agent construction, and the registration happens right
	// after it — same start-up leg, so waiting on it and then polling the
	// registry is a bounded wait on an event that follows it.
	sessRoot := filepath.Join(home, ".gopi", config.SessionsDirName)
	p.WaitFor("the sandboxed session JSONL", func() bool { return testutil.SessionRecordPath(sessRoot) != "" })
	sessionDir := filepath.Dir(testutil.SessionRecordPath(sessRoot))
	gopiDir := filepath.Join(home, ".gopi")

	entryPath, identity := "", ""
	p.WaitFor("a chat slot naming this run's session", func() bool {
		for path, value := range webSlotRegistryEntries(t, gopiDir) {
			if dir, ok := ChatSessionSlotSessionDir(value); ok && filepath.Clean(dir) == filepath.Clean(sessionDir) {
				entryPath, identity = path, value
				return true
			}
		}
		return false
	})

	// The path must be the registry the SINK reads: <profile>/.gopi/chat_session_id_*.txt.
	if filepath.Dir(entryPath) != gopiDir {
		t.Errorf("the slot landed in %s, want the profile registry %s", filepath.Dir(entryPath), gopiDir)
	}
	// LIVENESS: the pid half is the web server this test started, and it is
	// alive — a stale entry would be reclaimed by the next registration, so
	// this is what separates "written" from "usable".
	pidField, _, ok := strings.Cut(identity, config.ChatSurfaceIdentitySeparator)
	if !ok {
		t.Fatalf("slot %s carries %q — no pid half", entryPath, identity)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(pidField))
	if err != nil || pid <= 0 {
		t.Fatalf("slot %s carries %q — the pid half is not a positive pid", entryPath, identity)
	}
	if !tools.PidAlive(pid) {
		t.Errorf("slot %s names pid %d, which is not running — the entry is not usable by the sink", entryPath, pid)
	}
	if !p.Running() {
		t.Fatalf("the web server exited; the registered pid is not its own:\n%s", p.Stderr())
	}
}
