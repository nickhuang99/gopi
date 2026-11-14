package main

// chat_session_slots_test.go — the registry's invariants, pinned:
//
//	IDEMPOTENCE   a run re-registering does not consume a second slot;
//	THE CEILING   the 11th distinct session does not grow the set;
//	RECLAIM ORDER free slot → an entry whose subject is GONE → REFUSAL, and
//	              never the eviction of a chat that still looks alive
//	              (task 01a12d05: "the oldest by mtime" did exactly that).
//
// The last item is the one this file changed shape for. The pre-fix rule
// replaced the smallest mtime — a fact about when a run STARTED, not about
// whether it still exists — so a long-lived console could be evicted in favour
// of nothing. The tests below therefore pin BOTH directions of the new rule:
// a dead entry IS reclaimed, and a live one is NOT, with the refusal made
// visible by its own message.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// chatSlotTestDir is a registry directory OFF the "/" partition (Rule 22) — the
// same device-scratch helper the ledger fixtures use, so a test can never mint
// the operator's real registry into the source tree.
func chatSlotTestDir(t *testing.T) string {
	t.Helper()
	return testutil.TestDeviceScratchDir(t, "chat-slots")
}

// chatSlotSessions builds n distinct session directories under root.
func chatSlotSessions(t *testing.T, root string, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, "session-"+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		out = append(out, dir)
	}
	return out
}

// chatSlotWrite pins one slot's FILE CONTENTS verbatim — the fixture for the
// cases a registration cannot produce: an entry whose subject is gone, and an
// entry whose identity is malformed. What is under test is the CHOICE between
// entries, and its input is the text each slot carries.
func chatSlotWrite(t *testing.T, dir string, index int, identity string) {
	t.Helper()
	if err := os.WriteFile(ChatSessionSlotPath(dir, index), []byte(identity+"\n"), chatSessionSlotFileMode); err != nil {
		t.Fatalf("write slot %d: %v", index, err)
	}
}

// chatSlotFillAll writes a value into every slot, so the next registration has
// no free slot to take and must decide between the occupants.
func chatSlotFillAll(t *testing.T, dir string, identities []string) {
	t.Helper()
	if len(identities) != ChatSessionSlotLimit {
		t.Fatalf("chatSlotFillAll needs exactly %d identities, got %d", ChatSessionSlotLimit, len(identities))
	}
	for i, identity := range identities {
		chatSlotWrite(t, dir, i, identity)
	}
}

// chatSlotDeadPidAndDir returns an identity whose pid half is a process this
// test MEASURED to be gone and whose session directory was never created — the
// "swept scratch run" shape that filled nine of this machine's ten slots.
func chatSlotDeadPidAndDir(t *testing.T, root string, i int) string {
	t.Helper()
	return config.FormatChatSurfaceIdentity(chatSlotDeadPid(t), filepath.Join(root, "gone", "session-"+string(rune('a'+i))))
}

// chatSlotDeadPid returns a pid MEASURED dead on this host — the premise of
// every reclaim assertion, so it is measured by the shared fixture rather than
// assumed (internal/testutil.StartReapedProcess spawns a mock, reaps it, and
// fails the test unless kill(pid,0) answers ESRCH).
//
// THE FIXTURE THAT RE-ENTERED ITS OWN SUITE (found here, filed as its own bug
// task). This used to re-exec `os.Args[0]` — the test binary — with `-test.run=`
// and a test name the probe does not have. Go's flag package parses `-test.run=`
// as an EMPTY selector (the value belongs in the same argv element) and a bare
// trailing argument is not an error, so the probe ran THE WHOLE cmd/agent SUITE —
// including this very test, which spawned another probe, without bound. Measured
// 2026-10-12 on this host: a chain of 26 live
// `agent.test -test.run= TestThisNameSelectsNoTestInTheProbe` processes, three
// outer `go test` runs blocked for more than fourteen minutes, and every
// iteration re-running the whole suite.
//
// So the rule this fixture now states: a dead pid needs a process that CANNOT run
// this suite, and the suite already owns one — the portable mock in
// internal/testutil, which also carries the process-group reaper (throwaway
// processes have an owner).
func chatSlotDeadPid(t *testing.T) int {
	t.Helper()
	return testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
}

// TestChatSessionSlotRegistrationIsIdempotentAndBounded walks the first half of
// the contract: ten distinct sessions fill the ten slots IN ORDER, the registry
// never grows past the ceiling, and re-registering an existing session rewrites
// ITS OWN slot instead of taking another.
//
// The eleventh registration is deliberately NOT here: what it does when the
// registry is full is now a decision between occupants (alive vs gone), and it
// has its own tests — TestChatSessionSlotReclaimsOnlyAnEntryWhoseSubjectIsGone
// and TestChatSessionSlotRefusesToEvictALiveChat.
func TestChatSessionSlotRegistrationIsIdempotentAndBounded(t *testing.T) {
	dir := chatSlotTestDir(t)
	sessions := chatSlotSessions(t, dir, ChatSessionSlotLimit)

	for i, session := range sessions[:ChatSessionSlotLimit] {
		entry, err := RegisterChatSessionSlot(dir, session)
		if err != nil {
			t.Fatalf("register %s: %v", session, err)
		}
		if entry.Index != i {
			t.Fatalf("register #%d used slot %d, want %d (slots fill in order)", i, entry.Index, i)
		}
	}
	entries, err := ReadChatSessionSlots(dir)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(entries) != ChatSessionSlotLimit {
		t.Fatalf("the registry holds %d entries after %d registrations, want %d — the ceiling did not hold",
			len(entries), ChatSessionSlotLimit, ChatSessionSlotLimit)
	}

	// IDEMPOTENCE: re-registering a session that is already registered rewrites
	// its own slot, and the registry's SIZE does not move. Two probes — the first
	// slot and a middle one — so a mechanism that happened to work for one index
	// alone cannot pass.
	for _, probe := range []int{0, 4} {
		before := len(entries)
		entry, err := RegisterChatSessionSlot(dir, sessions[probe])
		if err != nil {
			t.Fatalf("re-register %s: %v", sessions[probe], err)
		}
		if entry.Index != probe {
			t.Fatalf("re-registering %s used slot %d, want its own slot %d", sessions[probe], entry.Index, probe)
		}
		after, err := ReadChatSessionSlots(dir)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if len(after) != before {
			t.Fatalf("re-registering %s changed the entry count %d → %d", sessions[probe], before, len(after))
		}
	}
}

// TestChatSessionSlotReclaimsOnlyAnEntryWhoseSubjectIsGone is the POSITIVE half
// of the full-registry rule: when no slot is free, an entry whose subject is
// measurably gone is reused — and every entry in the fixture except the one the
// test intends to be reclaimed is a LIVE directory, so a mechanism that reclaimed
// by position or by mtime would take the wrong slot and fail here.
//
// TWO DEAD SHAPES, in one registry, in a stated order: slot 3's session
// directory still exists but its pid is gone, slot 7's directory is gone
// entirely. Slot 3 is earlier, so it is the one taken — the order is part of the
// contract, not an accident of the fixture.
func TestChatSessionSlotReclaimsOnlyAnEntryWhoseSubjectIsGone(t *testing.T) {
	dir := chatSlotTestDir(t)
	root := t.TempDir()
	live := chatSlotSessions(t, dir, ChatSessionSlotLimit)

	identities := make([]string, ChatSessionSlotLimit)
	for i, session := range live {
		// THIS process's own pid: alive by construction. (The fixture used to
		// spell "alive" as pid 1, which is NOT a fact this test can measure: to an
		// unprivileged caller kill(1,0) answers EPERM, and tools.PidAlive reads a
		// failed signal as "not running" — measured 2026-10-12, and it made this
		// test claim the newcomer took slot 0.)
		identities[i] = config.FormatChatSurfaceIdentity(os.Getpid(), session)
	}
	// slot 3: alive-looking directory, DEAD pid.
	identities[3] = config.FormatChatSurfaceIdentity(chatSlotDeadPid(t), live[3])
	// slot 7: swept directory, arbitrary pid — reclaimable on the first ground.
	identities[7] = chatSlotDeadPidAndDir(t, root, 7)
	chatSlotFillAll(t, dir, identities)

	entry, err := RegisterChatSessionSlot(dir, filepath.Join(root, "newcomer"))
	if err != nil {
		t.Fatalf("register with a full registry holding two dead entries: %v", err)
	}
	if entry.Index != 3 {
		t.Fatalf("the newcomer took slot %d, want 3 — the first entry whose SUBJECT is gone", entry.Index)
	}
	// And nothing live was touched: every surviving entry still names its own
	// session.
	after, err := ReadChatSessionSlots(dir)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(after) != ChatSessionSlotLimit {
		t.Fatalf("the registry holds %d entries, want %d", len(after), ChatSessionSlotLimit)
	}
	for _, e := range after {
		if e.Index == 3 {
			continue
		}
		if e.Identity != identities[e.Index] {
			t.Fatalf("slot %d now holds %q — a LIVE entry was rewritten; it held %q", e.Index, e.Identity, identities[e.Index])
		}
	}
}

// TestChatSessionSlotRefusesToEvictALiveChat is the NEGATIVE half, and the
// behaviour change this task exists for: with every slot held by an entry that
// still looks alive, the registration is REFUSED — no slot is rewritten, and the
// error says what it refused, naming the occupants (Rule 15: 静默降级必须出声).
//
// The assertion on the MESSAGE is not decoration: a refusal nobody can read is
// the silent eviction it replaced.
func TestChatSessionSlotRefusesToEvictALiveChat(t *testing.T) {
	dir := chatSlotTestDir(t)
	live := chatSlotSessions(t, dir, ChatSessionSlotLimit)

	identities := make([]string, ChatSessionSlotLimit)
	for i, session := range live {
		// This process's own pid: alive by construction, and not zombie yet.
		identities[i] = config.FormatChatSurfaceIdentity(os.Getpid(), session)
	}
	chatSlotFillAll(t, dir, identities)

	_, err := RegisterChatSessionSlot(dir, filepath.Join(t.TempDir(), "newcomer"))
	if err == nil {
		t.Fatal("a registration into a registry whose every entry looks alive was ALLOWED — a live chat was evicted in silence")
	}
	for _, want := range []string{"refusing to evict", "still look alive", "slot 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not say %q; it says: %v", want, err)
		}
	}
	// NOTHING was rewritten: the registry is exactly the fixture.
	after, rerr := ReadChatSessionSlots(dir)
	if rerr != nil {
		t.Fatalf("read back: %v", rerr)
	}
	if len(after) != ChatSessionSlotLimit {
		t.Fatalf("the registry holds %d entries, want %d", len(after), ChatSessionSlotLimit)
	}
	for _, e := range after {
		if e.Identity != identities[e.Index] {
			t.Fatalf("slot %d was rewritten to %q despite the refusal — the registry holds %q", e.Index, e.Identity, identities[e.Index])
		}
	}
}

// TestChatSessionSlotReclaimabilityReadsEvidenceNotAge pins the PREDICATE the
// two tests above depend on, one input at a time, so a change to it fails here
// with the case named rather than three tests later.
//
// The UNKNOWN arm is the important one: `self:` and an empty pid half are not
// evidence that a process is gone, so they are KEEPs — the same "every unknown
// is a KEEP" rule the scratch sweep applies. An entry with no directory at all
// has nothing to deliver to and is the one malformed shape the writer may reuse.
func TestChatSessionSlotReclaimabilityReadsEvidenceNotAge(t *testing.T) {
	root := t.TempDir()
	aliveDir := filepath.Join(root, "alive")
	if err := os.MkdirAll(aliveDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	deadPid := chatSlotDeadPid(t)

	cases := []struct {
		name     string
		identity string
		want     bool
	}{
		{"directory gone", config.FormatChatSurfaceIdentity(os.Getpid(), filepath.Join(root, "gone")), true},
		{"pid dead, directory alive", config.FormatChatSurfaceIdentity(deadPid, aliveDir), true},
		{"pid alive, directory alive", config.FormatChatSurfaceIdentity(os.Getpid(), aliveDir), false},
		{"pid unknown (self), directory alive", config.FormatChatSurfaceIdentity(0, aliveDir), false},
		{"no directory half at all", config.FormatChatSurfaceIdentity(os.Getpid(), ""), true},
	}
	for _, tc := range cases {
		if got := chatSessionSlotReclaimable(tc.identity); got != tc.want {
			t.Fatalf("%s: chatSessionSlotReclaimable(%q) = %v, want %v", tc.name, tc.identity, got, tc.want)
		}
	}
}

// TestChatSessionSlotRegistryDirFollowsThisRunsOwnTree is the ISOLATION arm of
// task 01a12d05, and it is a pair because the two halves are the defect:
//
//	(a) a run that did NOT relocate resolves the OPERATOR's profile root —
//	    character for character, so the control arm cannot be passed by a
//	    function that simply moved every registry somewhere new;
//	(b) a run that DID relocate (a replay/film run publishing GOPI_SESSIONS_ROOT)
//	    resolves ITS OWN tree, so a scratch run can no longer occupy the
//	    operator's registry.
//
// The test drives the REAL resolution (config.ChatSlotsDir, the function
// cmd/agent calls) rather than a copy, and it asserts the two roots DIFFER — a
// relocation that resolved to the same directory would satisfy neither.
func TestChatSessionSlotRegistryDirFollowsThisRunsOwnTree(t *testing.T) {
	home := t.TempDir()
	scratch := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.SessionsRootEnv, "")

	dir, err := chatSessionSlotDir()
	if err != nil {
		t.Fatalf("chatSessionSlotDir (profile run): %v", err)
	}
	profileRoot := filepath.Join(home, config.ProfileDirName)
	if dir != profileRoot {
		t.Fatalf("an ordinary run resolves its registry to %q, want the profile root %q — the control arm moved", dir, profileRoot)
	}

	t.Setenv(config.SessionsRootEnv, filepath.Join(scratch, config.SessionsDirName))
	relocated, err := chatSessionSlotDir()
	if err != nil {
		t.Fatalf("chatSessionSlotDir (relocated run): %v", err)
	}
	// VERBATIM: the directory the caller named, not one derived from it.
	if relocated != scratch {
		t.Fatalf("a relocated run resolves its registry to %q, want the tree it named %q", relocated, scratch)
	}
	if relocated == dir {
		t.Fatalf("the relocated run's registry is the profile's own %q — the isolation is not there", dir)
	}

	// END TO END ON THE FILESYSTEM: a registration made while relocated leaves
	// the operator's registry untouched and is readable from the relocated tree.
	entry, err := RegisterChatSessionSlot(relocated, filepath.Join(scratch, config.SessionsDirName, "s"))
	if err != nil {
		t.Fatalf("register in the relocated tree: %v", err)
	}
	if got := filepath.Dir(entry.Path); got != scratch {
		t.Fatalf("the slot was written to %q, want %q", got, scratch)
	}
	//stylegate:ignore glob for optional cleanup; the pattern is a constant so ErrBadPattern is unreachable
	if matches, _ := filepath.Glob(filepath.Join(profileRoot, ChatSessionSlotPrefix+"*"+ChatSessionSlotSuffix)); len(matches) != 0 {
		t.Fatalf("the profile registry gained %v during a relocated run", matches)
	}
	if _, ok := os.Stat(ChatSessionSlotPath(relocated, entry.Index)); ok != nil {
		t.Fatalf("the relocated tree holds no slot %d: %v", entry.Index, ok)
	}
}

// TestChatSessionSlotValueIsTheSurfaceIdentityShape pins the ONE property that
// makes the subscriber script possible: the value written is the SAME string
// config.FormatChatSurfaceIdentity renders for the environment channel (Rule 13
// — one spelling of one identity), and its directory half is what
// `gopi notify --session` is handed.
func TestChatSessionSlotValueIsTheSurfaceIdentityShape(t *testing.T) {
	dir := chatSlotTestDir(t)
	session := filepath.Join(dir, "session-x")
	if err := os.MkdirAll(session, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entry, err := RegisterChatSessionSlot(dir, session)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	want := config.FormatChatSurfaceIdentity(os.Getpid(), session)
	if entry.Identity != want {
		t.Fatalf("slot holds %q, want %q (the environment channel's own rendering)", entry.Identity, want)
	}
	// The reader the script's counterpart uses parses it back.
	got, ok := ChatSessionSlotSessionDir(entry.Identity)
	if !ok || got != session {
		t.Fatalf("ChatSessionSlotSessionDir(%q) = %q, %v; want %q, true", entry.Identity, got, ok, session)
	}
	if _, ok := ChatSessionSlotSessionDir(config.FormatChatSurfaceIdentity(os.Getpid(), "")); ok {
		t.Fatal("an identity with no directory half parsed as deliverable — a delivery to \"\" must be refused, not attempted")
	}
	// The file on disk is exactly the identity plus one newline, and it is not
	// group- or world-readable (it names the operator's session directories).
	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		t.Fatalf("read slot: %v", err)
	}
	if string(raw) != entry.Identity+"\n" {
		t.Fatalf("slot file holds %q, want %q + newline", raw, entry.Identity)
	}
	fi, err := os.Stat(entry.Path)
	if err != nil {
		t.Fatalf("stat slot: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != chatSessionSlotFileMode {
		t.Fatalf("slot mode is %o, want %o", perm, chatSessionSlotFileMode)
	}
}

// TestChatSessionSlotNamesMatchTheBound pins the NAME SHAPE against the limit:
// the two constants define the channel the shell script globs, and a limit that
// disagreed with the names would leave readable slots nothing ever scans.
func TestChatSessionSlotNamesMatchTheBound(t *testing.T) {
	dir := chatSlotTestDir(t)
	if ChatSessionSlotLimit <= 0 {
		t.Fatalf("ChatSessionSlotLimit = %d", ChatSessionSlotLimit)
	}
	for i := 0; i < ChatSessionSlotLimit; i++ {
		name := ChatSessionSlotName(i)
		if name == ChatSessionSlotPrefix+ChatSessionSlotSuffix {
			t.Fatalf("slot %d renders an empty index: %q", i, name)
		}
		if got := filepath.Base(ChatSessionSlotPath(dir, i)); got != name {
			t.Fatalf("ChatSessionSlotPath(%d) = %q, want base %q", i, got, name)
		}
	}
	// A slot FILE that cannot be read is reported, not skipped in silence
	// (Rule 15): the reader's error names the path.
	if err := os.MkdirAll(ChatSessionSlotPath(dir, 3), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := ReadChatSessionSlots(dir); err == nil {
		t.Fatal("a slot that is a directory produced no error — an unreadable slot must be reported")
	}
}
