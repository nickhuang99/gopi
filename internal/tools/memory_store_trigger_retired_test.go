package tools

// memory_store_trigger_retired_test.go — the REGRESSION GUARD of epic
// memory-store-sync-decouple (2026-10-27): `memory store` must never again spawn
// a `gopi memory sync` child, and its return text must say nothing about a peer,
// a push or a sync.
//
// WHY A TEST AND NOT A NOTE. The deleted mechanism was 1033 lines of trigger plus
// six test files, and what it did was invisible from the store's side: the store
// returned in milliseconds whether or not a 307-second push had just started
// behind it. That asymmetry is exactly what lets the leg come back unnoticed — a
// caller added an "after store" hook and nothing in the tree objects. This file is
// the objection: it drives the REAL store path and then asks the OPERATING SYSTEM
// whether a child exists, so a reinstated trigger fails here instead of on a
// peer's ledger lock.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The probe row. The store refuses a body with no distilled content (the
// garbage gate), so a one-letter fixture would fail before reaching the spawn
// check the test is about.
const (
	memoryStoreProbeTopic    = "store decoupled from cross-node sync"
	memoryStoreProbeQuestion = "what does a successful memory store mean?"
	memoryStoreProbeAnswer   = "the row was written to this node's own memory.db; nothing about the call reaches a peer, spawns a child process, or reports a direction"
	memoryStoreProbeSource   = "epic memory-store-sync-decouple"
)

// TestMemoryStoreSpawnsNoSyncChild — the acceptance the epic states in one line:
// store a row, then look for a `gopi memory sync` process. There must be none.
//
// The check is a pgrep-equivalent done in-process (a /proc walk over this
// process's own children) rather than a shell `pgrep -f`, because a pgrep would
// also match THIS test binary's own path if the harness ever ran it as a child of
// something with the words in argv — and a false positive here is a false alarm on
// the exact property being guarded.
func TestMemoryStoreSpawnsNoSyncChild(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", db)

	if _, err := MemoryStore(memoryStoreProbeTopic, memoryStoreProbeQuestion, memoryStoreProbeAnswer, memoryStoreProbeSource, nil); err != nil {
		t.Fatalf("MemoryStore: %v", err)
	}
	for _, pid := range memoryStoreTriggerChildPIDs(t) {
		argv := memoryStoreTriggerArgv(pid)
		if strings.Contains(argv, "memory") && strings.Contains(argv, "sync") {
			t.Fatalf("a `memory sync` child was spawned by a store: pid=%d argv=%q", pid, argv)
		}
	}
}

// TestMemoryStoreReturnSaysNothingAboutSync — the store's reply is the whole
// success statement: the row was written. A reply that mentions a peer, a push
// direction or a sync is a promise the store no longer keeps, and an LLM reading
// it would believe a peer had been told.
func TestMemoryStoreReturnSaysNothingAboutSync(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", db)

	reply, err := MemoryStore(memoryStoreProbeTopic, "how does a store report success?",
		"a store's success criterion is the local ledger write and nothing else; the row is readable from this node's own database immediately after the call returns", memoryStoreProbeSource, nil)
	if err != nil {
		t.Fatalf("MemoryStore: %v", err)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatal("MemoryStore returned an empty reply: a store that says nothing cannot be told from one that failed")
	}
	lower := strings.ToLower(reply)
	for _, banned := range []string{"peer", "push", "sync", "peerbox", "hp"} {
		if strings.Contains(lower, banned) {
			t.Fatalf("the store reply mentions %q — a store's success is the LOCAL write and nothing else: %q", banned, reply)
		}
	}
}

// memoryStoreTriggerChildPIDs lists this process's live children from /proc.
// Reading the kernel's own table is the only way to answer "did something get
// spawned" without a shell (Rule 27 is about load, not about this: a /proc read
// generates none).
func memoryStoreTriggerChildPIDs(t *testing.T) []int {
	t.Helper()
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("read /proc: %v", err)
	}
	var pids []int
	for _, e := range entries {
		pid, ok := memoryStoreTriggerAtoi(e.Name())
		if !ok {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue // a process that exited between the readdir and the read
		}
		// The parent pid is field 4 of /proc/<pid>/stat, but the comm field
		// (field 2) is parenthesized and may contain spaces, so the parse starts
		// after the LAST ')'.
		body := string(stat)
		close := strings.LastIndex(body, ")")
		if close < 0 || close+2 >= len(body) {
			continue
		}
		fields := strings.Fields(body[close+2:])
		if len(fields) < 2 {
			continue
		}
		if ppid, ok := memoryStoreTriggerAtoi(fields[1]); ok && ppid == self {
			pids = append(pids, pid)
		}
	}
	return pids
}

// memoryStoreTriggerArgv returns a process's argv joined by spaces, or "" when it
// cannot be read (exited, or gone between the checks).
func memoryStoreTriggerArgv(pid int) string {
	b, err := os.ReadFile(filepath.Join("/proc", memoryStoreTriggerAtoiStr(pid), "cmdline"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
}

// memoryStoreTriggerAtoi is strconv.Atoi without pulling in a second spelling of
// the same call for a /proc entry name (which is all digits or not a pid at all).
func memoryStoreTriggerAtoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// memoryStoreTriggerAtoiStr renders a pid back as a /proc entry name.
func memoryStoreTriggerAtoiStr(pid int) string {
	if pid == 0 {
		return "0"
	}
	var digits []byte
	for pid > 0 {
		digits = append([]byte{byte('0' + pid%10)}, digits...)
		pid /= 10
	}
	return string(digits)
}
