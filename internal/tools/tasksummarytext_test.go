package tools

// Regression guard for the binary-summary defect (epic binary-summary-guard):
// `gopi task list --status failed > out.txt` produced a file with NUL bytes
// because ONE ledger row's summary WAS a raw ELF executable image (measured row
// 01a09f6d-7753-7321-ba33-8b32b09213e7). Root cause: a bare-prompt run in
// /home/user/workspace/peak-bin was started with the built binary as its
// positional argument; readInput reads an existing non-image file as prompt
// TEXT, so the executable became the prompt and the start hook stored
// `[auto] ` + its first 200 runes as the ledger summary.
//
// The fixture below is the REAL byte prefix of that row's summary — not a
// synthetic "\x00\x01" stand-in — so the test fails if any layer starts
// letting those bytes through again (Rule 10: verify on the smallest real
// sample).

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// elfSummaryFixture is the SHAPE of the offending ledger row: the "[auto] "
// ledger prefix followed by ~200 bytes of a real ELF64 head (the row captured
// exactly the first 200 runes of the briefing, which was the executable). The
// bytes are the real ones (\x7fELF + the ELF64 header words), repeated to fill
// the cap, so the fixture exercises the DOMINANT-binary path the real row hit.
func elfSummaryFixture() string {
	head := []byte{
		0x7f, 'E', 'L', 'F',
		0x02, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x3e, 0x00, 0x01, 0x00,
	}
	var b []byte
	for len(b) < taskSummaryMaxRunes {
		b = append(b, head...)
	}
	b = b[:taskSummaryMaxRunes]
	return "[auto] " + string(b) + " help"
}

// TestTaskSummarySanitizerIsIdentityForText pins the contract that makes this
// guard safe to apply everywhere: ordinary headlines — CJK, emoji, quotes,
// punctuation — pass through byte-for-byte. A sanitizer that rewrote a
// legitimate Chinese briefing would trade one display defect for another.
func TestTaskSummarySanitizerIsIdentityForText(t *testing.T) {
	for _, s := range []string{
		"resolve merge conflict: fix/cwd-and-lifetime",
		"[auto] 背景（the operator：harvest 时是否也 push HP 分支回 peer1？）",
		"fix(build): master tip bd42e827 does not compile — undefined: atomic",
		"emoji 🙂 and symbols → ✓ § ¶",
		"tabs\tand\nnewlines fold to spaces",
	} {
		got := sanitizeTaskSummary(s)
		want := s
		if strings.ContainsAny(s, "\t\n") {
			want = strings.Join(strings.Fields(s), " ")
		}
		if got != want {
			t.Errorf("sanitizeTaskSummary(%q) = %q, want %q — ordinary text must be identity", s, got, want)
		}
	}
}

// TestTaskSummarySanitizerEscapesBinary is the unit face of the guard's
// IN-PLACE form (text with a little corruption in it): NULs and invalid UTF-8
// never survive, the result is valid UTF-8, and the bytes are still VISIBLE in
// the row (the escape keeps the evidence — a silent strip would hide that
// corruption was captured at all).
func TestTaskSummarySanitizerEscapesBinary(t *testing.T) {
	got := sanitizeTaskSummary("resolve merge conflict\x00 for fix/x — steps\x01two")
	if strings.ContainsRune(got, 0) {
		t.Fatalf("sanitized summary still carries NUL: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("sanitized summary is not valid UTF-8: %q", got)
	}
	if !strings.Contains(got, "[bin \\x00]") {
		t.Errorf("escaped run must be visible in place, got %q", got)
	}
	// The escaped form keeps the text's SHAPE: the words around the
	// corruption are still where the reader expects them.
	if !strings.Contains(got, "resolve merge conflict[bin \\x00] for fix/x") {
		t.Errorf("in-place escape must preserve the surrounding text, got %q", got)
	}
	// A lone invalid byte (latin-1 é as 0xe9) is corruption too, and a valid
	// multi-byte rune next to it must not be swallowed with it.
	mixed := sanitizeTaskSummary("caf\xe9 中文 ok")
	if !utf8.ValidString(mixed) || strings.ContainsRune(mixed, 0) {
		t.Errorf("invalid byte sequence survived: %q", mixed)
	}
	if !strings.Contains(mixed, "中文") {
		t.Errorf("valid text adjacent to corruption must survive: %q", mixed)
	}
}

// TestTaskSummarySanitizerBlobForm covers the DOMINANT-binary shape — the
// measured defect: a summary that IS an executable image. Escaping such a
// string run-by-run interleaves dozens of markers with the binary's own byte
// fragments (measured on the real row: 35 markers, unreadable). The blob form
// reports it once, with the captured size and the magic head.
func TestTaskSummarySanitizerBlobForm(t *testing.T) {
	got := sanitizeTaskSummary(elfSummaryFixture())
	if strings.ContainsRune(got, 0) || !utf8.ValidString(got) {
		t.Fatalf("sanitized summary is not text: %q", got)
	}
	if n := strings.Count(got, "["+taskSummaryBinaryMarker+" "); n != 1 {
		t.Errorf("a blob-dominant summary must render exactly ONE marker, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "[bin \\x7fELF\\x02\\x01\\x01\\x00") {
		t.Errorf("blob marker must carry the magic head, got %q", got)
	}
	// The text BEFORE and AFTER the blob is still there — the row does not
	// lose the little that was readable.
	if !strings.HasPrefix(got, "[auto] [bin ") || !strings.HasSuffix(got, " help") {
		t.Errorf("blob form must keep the surrounding text, got %q", got)
	}
}

// TestTaskSummarySanitizerBoundsLength pins the rune cap: the store must not
// accept a headline longer than the ledger row it renders.
func TestTaskSummarySanitizerBoundsLength(t *testing.T) {
	got := sanitizeTaskSummary(strings.Repeat("知", taskSummaryMaxRunes*2))
	if n := utf8.RuneCountInString(got); n > taskSummaryMaxRunes+1 { // +1 for the ellipsis
		t.Errorf("summary has %d runes, want <= %d", n, taskSummaryMaxRunes+1)
	}
	if !strings.HasSuffix(got, taskSummaryEllipsis) {
		t.Errorf("truncated summary must be marked: %q", got)
	}
}

// TestTaskListNeverRendersBinarySummary is the END-TO-END regression for the
// reported defect, covering BOTH layers of the fix:
//
//   - the WRITE path (TaskNew): what the engine stores is already text;
//   - the RENDER path (TaskList): a row inserted RAW by an older binary — the
//     fixture reproduces the legacy row directly through SQL — still renders a
//     NUL-free, UTF-8-valid line, which is what makes `gopi task list > f`
//     produce a text file.
func TestTaskListNeverRendersBinarySummary(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)

	// Layer 1: the write path.
	if _, err := TaskNew(elfSummaryFixture(), "t-elf-write", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Layer 2: a legacy row, byte-for-byte as the broken binary stored it.
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	// `task new` rows are born in_progress; the reported listing is the failed
	// filter, so move it there (the write-path assertion is on the COLUMN, not
	// on the status).
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ? WHERE task_id = ?`, taskStatusFailed, "t-elf-write"); err != nil {
		t.Fatalf("mark write-path row failed: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, 0)`,
		"t-elf-raw", elfSummaryFixture(), taskStatusFailed, "auto"); err != nil {
		t.Fatalf("raw legacy insert: %v", err)
	}

	got, err := TaskList(taskStatusFailed)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.ContainsRune(got, 0) {
		t.Fatalf("TaskList output carries NUL bytes (the reported defect):\n%q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("TaskList output is not valid UTF-8:\n%q", got)
	}
	// Both rows are present and escaped — the guard must not DROP the row.
	if !strings.Contains(got, "t-elf-raw") || !strings.Contains(got, "t-elf-write") {
		t.Errorf("both rows must render:\n%s", got)
	}
	if !strings.Contains(got, "[bin \\x7fELF") {
		t.Errorf("escaped binary marker missing from the ledger line:\n%s", got)
	}
	// Every line of the listing is text a shell pipeline can carry: no
	// control byte other than the newline that separates rows.
	for i, line := range strings.Split(got, "\n") {
		for _, r := range line {
			if r < 0x20 || r == 0x7f {
				t.Errorf("line %d carries control rune %q: %q", i, r, line)
			}
		}
	}

	// The stored row is text as well: a reader that trusts the COLUMN (sync,
	// another node's list, an FTS consumer) must not find the raw bytes there.
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT summary FROM tasks WHERE task_id = ?`, "t-elf-write").Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.ContainsRune(stored, 0) || !utf8.ValidString(stored) {
		t.Errorf("stored summary is not text: %q", stored)
	}
}

// TestTaskShowNeverRendersBinarySummary covers the OTHER render face of the
// same column (`task show` — summary, briefing payload and the children
// block), because an operator diagnosing a failed row reads that view.
func TestTaskShowNeverRendersBinarySummary(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: elfSummaryFixture(),
		Payload: "briefing with a stray NUL:\x00 and more text",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT task_id FROM tasks LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("id: %v", err)
	}

	got, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.ContainsRune(got, 0) || !utf8.ValidString(got) {
		t.Fatalf("task show output is not text:\n%q", got)
	}
	// Payload structure is preserved for legitimate multi-line briefings; only
	// the corruption is escaped.
	if !strings.Contains(got, "briefing with a stray NUL:") || !strings.Contains(got, "and more text") {
		t.Errorf("payload text around the corruption must survive:\n%s", got)
	}
}
