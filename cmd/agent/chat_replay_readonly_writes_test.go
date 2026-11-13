package main

// chat_replay_readonly_writes_test.go — THE WRITES A REPLAY MUST NOT PERFORM,
// asserted against the REAL writers in the operator's profile (epic
// `gopi chat --replay` must be really read-only; measured 2026-10-12).
//
// WHY THIS IS NOT A UNIT CASE (Rule 31). The claim is not "replaymode.Active()
// returns true" — that is one bool. The claim is "the slot registry does not
// file this run into the operator's profile, and the editor's history path
// answers none". Both are properties of the REAL writer reached through its
// REAL entry point, with a REAL profile directory in front of it: the registry
// writes through config.GopiDir() and the history through the same resolver, so
// a case that stubbed either would be asserting its own stub.
//
// THE BEFORE/AFTER IS THE ASSERTION, not the flag: the profile directory is
// read for its exact file list and byte contents before and after the call, and
// the case fails on any difference. "Nothing was written" is a claim about a
// directory, and only the directory can decide it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replaymode"
)

// replayWritesTestProfile is the scratch profile this case points gopi at: an
// operator's profile with one slot already registered, so the case measures a
// DIRECTORY WITH CONTENT rather than an empty one (a writer that only ever
// creates a first file would pass against an empty directory for the wrong
// reason).
func replayWritesTestProfile(t *testing.T) string {
	t.Helper()
	// t.Setenv is the Rule 17 spelling: the scope is the TEST, and the profile
	// this case measures is what config.GopiDir() resolves from — GOPI_HOME plus
	// the profile leaf, so the scratch directory below is the profile ROOT and
	// the operator's own /home/nick/.gopi is never in the picture.
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir, err := config.GopiDir()
	if err != nil {
		t.Fatalf("the scratch profile could not be resolved: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("the scratch profile directory could not be created: %v", err)
	}
	if _, err := RegisterChatSessionSlot(dir, "/someone/elses/session"); err != nil {
		t.Fatalf("seeding the profile with one registered slot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, chatHistoryFileName), []byte("an entry the operator typed\n"), 0o600); err != nil {
		t.Fatalf("seeding the profile with a history file: %v", err)
	}
	return dir
}

// replayWritesProfileBytes is the whole profile as (name → contents), read
// through the SAME reader for the before and the after halves so the two maps
// are comparable by construction.
func replayWritesProfileBytes(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the profile %s could not be listed: %v", dir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("the profile entry %s could not be read: %v", e.Name(), rerr)
		}
		out[e.Name()] = string(data)
	}
	return out
}

// TestChatReplaySlotRegistryWritesNothing is THE regression for the defect the
// operator measured (a replay rewriting `chat_session_id_2.txt` and so pointing
// `notify.cmd` at a directory nobody reads).
//
// It drives the REAL arm-point call, twice: once while this process is NOT
// replaying — the control, which must DO the write, or the case would pass
// against a registry that is simply broken — and once while it is. The control
// half is what makes the second half evidence instead of tautology.
func TestChatReplaySlotRegistryWritesNothing(t *testing.T) {
	dir := replayWritesTestProfile(t)
	before := replayWritesProfileBytes(t, dir)

	// CONTROL: not replaying ⇒ the write HAPPENS. Without this arm the case
	// below would be green for a registry that never wrote anything at all.
	if _, ok := chatSessionSlotRegister("/an/ordinary/session/dir/session.jsonl", termToolStylesFor(lineedit.ColourOff)); !ok {
		t.Fatalf("the control registered nothing while this process is NOT replaying: the registry is not writing, so the case below measures nothing")
	}
	control := replayWritesProfileBytes(t, dir)
	if control[ChatSessionSlotName(1)] == before[ChatSessionSlotName(1)] {
		t.Fatalf("the control did not change the profile: slot 1 still reads %q, so a later unchanged-profile assertion would be vacuous", control[ChatSessionSlotName(1)])
	}

	// ARMED: replaying ⇒ the arm point refuses, and the profile is untouched.
	replaymode.Arm("/a/recording/that/does/not/exist.jsonl")
	t.Cleanup(replaymode.Disarm)
	if !replaymode.Active() {
		t.Fatalf("replaymode.Active() is false after Arm: the flag every write path consults is not in force")
	}
	entry, ok := chatSessionSlotRegister("/a/replay/scratch/session.jsonl", termToolStylesFor(lineedit.ColourOff))
	if ok {
		t.Fatalf("the slot registry registered while a replay was armed (entry %+v): a replay must file nothing into the operator's index", entry)
	}
	if entry.Identity != "" {
		t.Fatalf("the refused registration still returned an identity (%q): a refused write must not hand back a value a caller could act on", entry.Identity)
	}

	after := replayWritesProfileBytes(t, dir)
	if len(after) != len(control) {
		t.Fatalf("the profile's file count moved while a replay was armed: %d before, %d after", len(control), len(after))
	}
	for name, want := range control {
		if got := after[name]; got != want {
			t.Fatalf("the profile entry %s changed while a replay was armed:\n got %q\nwant %q", name, got, want)
		}
	}
}

// TestChatReplayHistoryPathIsNone pins the second writer: the editor's history
// file is the operator's own recall list, and a replay TYPES the recording's
// prompts into the real editor. The path must answer "" (the editor's documented
// memory-only mode) while a replay is armed, and the ordinary path otherwise —
// again a control, so "empty" is not confused with "the resolver is broken".
func TestChatReplayHistoryPathIsNone(t *testing.T) {
	dir := replayWritesTestProfile(t)

	ordinary := chatHistoryPath()
	if ordinary == "" {
		t.Fatalf("the control resolved no history path while this process is NOT replaying: the case below could not tell a refusal from a broken resolver")
	}
	if want := filepath.Join(dir, chatHistoryFileName); ordinary != want {
		t.Fatalf("the history path is %q, want %q", ordinary, want)
	}

	replaymode.Arm("/a/recording.jsonl")
	t.Cleanup(replaymode.Disarm)
	if got := chatHistoryPath(); got != "" {
		t.Fatalf("a replay resolved the history path %q: the recording's prompts would be appended to the operator's own recall list", got)
	}
}

// TestChatReplayRegistersNoChatRow pins the third writer at the predicate all
// three chats-table hooks ask (RegisterChatSession / RecordChatTurn /
// CloseChatRow): while a replay is armed it answers false, so no hook can reach
// the operator's ledger. The control arm is the inverse reading — the switch
// must still be ON for an ordinary run, or an accidental default-off would look
// exactly like this refusal.
func TestChatReplayRegistersNoChatRow(t *testing.T) {
	if !agent.ChatRegisterEnabled() {
		t.Fatalf("chat registration reads OFF while this process is NOT replaying: the control is missing and the armed reading below proves nothing")
	}
	replaymode.Arm("/a/recording.jsonl")
	t.Cleanup(replaymode.Disarm)
	if agent.ChatRegisterEnabled() {
		t.Fatalf("chat registration reads ON while a replay is armed: the run would write a chats row for a session that exists only in a scratch tree")
	}
	// The ordinary switch is NOT consulted while replaying, and that ordering is
	// what the flag guarantees: a ledger write is refused whatever GOPI_CHAT_REGISTER says.
	t.Setenv(agent.ChatRegisterEnv, "")
	if agent.ChatRegisterEnabled() {
		t.Fatalf("chat registration reads ON while a replay is armed and the ordinary switch is unset")
	}
}

// TestChatReplayRefusalNamesEveryWriter keeps the announcement honest: the
// catalogue this verb prints must NAME each writer the flag actually stops. A
// list that drifted from the code would be a claim the run cannot support —
// which is the whole failure mode the announcement exists to close (Rule 15).
func TestChatReplayRefusalNamesEveryWriter(t *testing.T) {
	cat := replayRefusalCatalogue()
	for _, want := range []string{
		ChatSessionSlotPrefix, // the slot registry (cmd/agent/chat_session_slots.go)
		chatHistoryFileName,   // the editor history (cmd/agent/lineedit_wire.go)
		"chats table",         // the ledger hooks (internal/agent/chatregister.go)
		"SetToolExecutor",     // the tool dispatch (cmd/agent/chat_replay.go)
		"auto-memory store",   // the capture paths (internal/agent/memory_replay_guard.go)
		"memory prefetch",     // the retrieval (cmd/agent/chat_prefetch.go)
	} {
		if !strings.Contains(cat, want) {
			t.Fatalf("the refusal catalogue does not name %q:\n%s", want, cat)
		}
	}
	if replaymode.Refuse("x") == "" {
		t.Fatalf("replaymode.Refuse returned an empty sentence: a short-circuit must be announced, never silent")
	}
}
