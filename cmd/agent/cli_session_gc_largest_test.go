package main

// The two arms `gopi session gc` grew for the size problem (measured 2026-10-30:
// 83 GB of session containers, *.raw.log 64.4 GiB across 1095 files, and the
// age-based judgement able to reclaim 14.7 MiB of it).
//
// The contract driven here is the same shape the family's own gc test uses: the
// assertions are on the DIRECTORY, never on the printed text — a verb that said
// "dry run" and wrote anyway would pass a text-only check.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	gcLargestUUID = "0f8e2a10-4444-4000-8000-00000000bb01"
	gcTrimUUID    = "0f8e2a10-5555-4000-8000-00000000bb02"
)

// gcSizedContainer makes one session container holding a payload of exactly
// size bytes, so `--largest` has a measurable subject.
func gcSizedContainer(t *testing.T, root, name string, size int64) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), make([]byte, size), 0o600); err != nil {
		t.Fatalf("write a payload in %s: %v", name, err)
	}
	return dir
}

// gcRawLog writes a synthetic raw log of about size bytes with the writer's own
// shape (a `#` header, then round markers), so the trim arm has a real subject.
func gcRawLog(t *testing.T, dir string, size int64) string {
	t.Helper()
	path := filepath.Join(dir, gcTrimUUID+scratch.SessionRawLogFileSuffix)
	var b strings.Builder
	b.WriteString("# gopi session raw HTTP log — created 2026-10-30T00:00:00Z pid=1 session=" + gcTrimUUID + "\n")
	for int64(b.Len()) < size {
		b.WriteString("→ REQUEST\n")
		b.WriteString(`{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("y", 200) + `"}]}` + "\n")
		b.WriteString("← RESPONSE\n")
		b.WriteString(`{"content":[{"type":"text","text":"ok"}]}` + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write the fixture raw log: %v", err)
	}
	return path
}

// ① `--largest` IS READ-ONLY AND ORDERS BY SIZE. The two containers' sizes
// disagree with their names, so a name-ordered implementation cannot pass, and
// every payload must still be on disk afterwards.
func TestSessionGCLargestIsReadOnlyAndOrdered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// The plan's UNREFERENCED half needs a READABLE ledger, and an unreadable one
	// is a refusal BY DESIGN (sessions_gc.go: "the task ledger could not be read
	// — every candidate is kept"), which the verb reports as exit 1. The scaffold
	// hands out the path (Rule 32: never a hand-rolled one) and the product's own
	// EnsureLedger creates the schema — so the test drives the SAME ledger shape
	// a host has, with every table empty.
	dbPath := testutil.NewTestDBPath(t)
	if err := tools.EnsureLedger(dbPath); err != nil {
		t.Fatalf("create the test ledger at %s: %v", dbPath, err)
	}
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		t.Fatalf("resolve the sessions root: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create %s: %v", root, err)
	}
	small := gcSizedContainer(t, root, gcTrimUUID, 1<<20)
	big := gcSizedContainer(t, root, gcLargestUUID, 3<<20)

	opts, code := parseSessionGCArgs([]string{sessionGCLargestFlag, "2"})
	if code != -1 {
		t.Fatalf(`--largest 2 was refused with code %d`, code)
	}
	if !opts.largestSet || opts.largest != 2 {
		t.Fatalf("--largest 2 parsed as set=%v n=%d", opts.largestSet, opts.largest)
	}
	if rc := runSessionGC([]string{sessionGCLargestFlag, "2"}); rc != 0 {
		t.Fatalf("`session gc --largest 2` exited %d, want 0", rc)
	}
	// READ-ONLY: both payloads are exactly where they were written.
	for _, d := range []string{small, big} {
		fi, serr := os.Stat(filepath.Join(d, "payload.bin"))
		if serr != nil {
			t.Fatalf("--largest removed %s: %v", d, serr)
		}
		if fi.Size() < 1<<20 {
			t.Fatalf("--largest changed the payload in %s (now %d bytes)", d, fi.Size())
		}
	}
	// And the ORDER is the library's own measured promise.
	plans, perr := scratch.LargestSessions(root, scratch.SessionsGCStaleAge, 2)
	if perr != nil {
		t.Fatalf("LargestSessions: %v", perr)
	}
	if len(plans) != 2 || plans[0].Name != gcLargestUUID {
		t.Fatalf("the largest container is not first: %+v", plans)
	}
}

// ② `--largest` WITH `--apply` IS REFUSED BY NAME: it is a reader, so an apply
// beside it would be an option that silently does nothing (Rule 15).
func TestSessionGCLargestRefusesApply(t *testing.T) {
	if _, code := parseSessionGCArgs([]string{sessionGCLargestFlag, sessionGCApplyFlag}); code != 2 {
		t.Fatalf("--largest --apply exited %d, want the usage refusal 2", code)
	}
	if _, code := parseSessionGCArgs([]string{sessionGCLargestFlag, sessionGCTrimRawLogFlag, "64MiB"}); code != 2 {
		t.Fatalf("--largest --trim-raw-log exited %d, want the usage refusal 2 (two different questions)", code)
	}
}

// ③ `--trim-raw-log` DEFAULTS TO A DRY RUN: the report names the candidate, and
// the file is byte-identical afterwards (size AND mtime).
func TestSessionGCTrimRawLogDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		t.Fatalf("resolve the sessions root: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create %s: %v", root, err)
	}
	dir := filepath.Join(root, gcTrimUUID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	path := gcRawLog(t, dir, 1<<20)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}

	if rc := runSessionGC([]string{sessionGCTrimRawLogFlag, "128KiB"}); rc != 0 {
		t.Fatalf("`session gc --trim-raw-log 128KiB` exited %d, want 0", rc)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture after the dry run: %v", err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("the DRY RUN wrote to the log: %d -> %d bytes, mtime %v -> %v",
			before.Size(), after.Size(), before.ModTime(), after.ModTime())
	}
}

// ④ `--trim-raw-log --apply` LANDS THE BOUND: the file ends at or under the cap,
// still opens with its header, carries one trim marker, and ends on a whole line.
func TestSessionGCTrimRawLogApplyBoundsTheFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		t.Fatalf("resolve the sessions root: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create %s: %v", root, err)
	}
	dir := filepath.Join(root, gcTrimUUID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	path := gcRawLog(t, dir, 1<<20)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}
	capBytes := int64(128 << 10)
	if before.Size() <= capBytes {
		t.Fatalf("the fixture (%d bytes) does not exceed the cap (%d)", before.Size(), capBytes)
	}

	if rc := runSessionGC([]string{sessionGCTrimRawLogFlag, "128KiB", sessionGCApplyFlag}); rc != 0 {
		t.Fatalf("`session gc --trim-raw-log 128KiB --apply` exited %d, want 0", rc)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the log after the apply: %v", err)
	}
	if after.Size() > capBytes {
		t.Fatalf("the log is %d bytes, above the %d-byte cap", after.Size(), capBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the bounded log: %v", err)
	}
	if !strings.HasPrefix(string(data), "# gopi session raw HTTP log — created ") {
		t.Fatalf("the bounded log no longer opens with its creation header")
	}
	if n := strings.Count(string(data), scratch.SessionsRawLogTrimMarkerPrefix); n != 1 {
		t.Fatalf("the bounded log carries %d trim markers, want exactly 1", n)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("the bounded log does not end with a newline — its last line is incomplete")
	}
	if enc := time.Since(after.ModTime()); enc < 0 {
		t.Fatalf("the bounded log's mtime is in the future (%v)", enc)
	}
}

// ⑤ THE SIZE SPELLING: a bare count is bytes, a unit suffix multiplies, and a
// unit nobody knows is refused rather than silently read as bytes.
func TestSessionGCTrimSizeParsing(t *testing.T) {
	ok := map[string]int64{
		"1024":   1024,
		"64MiB":  64 << 20,
		"64mib":  64 << 20,
		"512KiB": 512 << 10,
		"2GiB":   2 << 30,
		"1GB":    1 << 30,
		"64 MiB": 64 << 20,
		"128kb":  128 << 10,
		"3g":     3 << 30,
		"16K":    16 << 10,
	}
	for raw, want := range ok {
		got, err := parseGCSizeBytes(raw)
		if err != nil {
			t.Fatalf("parseGCSizeBytes(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("parseGCSizeBytes(%q) = %d, want %d", raw, got, want)
		}
	}
	for _, bad := range []string{"", "0", "-5", "abc", "64XiB", "MiB"} {
		if _, err := parseGCSizeBytes(bad); err == nil {
			t.Fatalf("parseGCSizeBytes(%q) was accepted", bad)
		}
	}
}
