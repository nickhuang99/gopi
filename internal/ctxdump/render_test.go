// render_test.go — the red lines of the /context export, pinned.
//
// Each test names the defect it would catch, because these are the ones a
// future "simplification" would reintroduce silently:
//
//   - a per-block character budget would make the page a SUBSET of the request
//     while its heading claimed the whole thing — measured: the system prompt is
//     183,161 characters on this host and DefaultSystemChars=20000 showed 11,156
//     of them (TestSystemBlocksAreVerbatimNeverClipped);
//   - an image block emitted as base64 would make the page the JSON again
//     (TestImageBlocksAreSummarizedNeverDumped);
//   - an append-mode writer would put a SECOND page in one file when the same
//     address was exported twice (TestReExportingTheSameAddressOverwrites);
//   - decoration the model never produced — a `N chars, N tokens` heading, a
//     Totals table, a truncation note — would sit in the middle of the bytes a
//     reader is diffing against the raw log (TestThePageCarriesNoCountersOrNotes);
//   - a turn number in the file name that is not the turn the file HOLDS would
//     be a name lying about its own bytes — the `/screen` artifact's defect
//     (TestWriteNamesTheFileForTheTurnItHolds).
package ctxdump

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/rawlogfmt"
)

// fixtureSystemText is deliberately LARGER than the retired DefaultSystemChars
// budget so a reintroduced clip fires: it is the exact shape of the defect
// (a long rule text under a heading that claims a bigger size).
//
// It carries no line beginning with '#' on purpose — the extraction below cuts
// the section at the next heading, and a fixture that looked like markdown would
// make the READER's job ambiguous rather than the renderer's. The real system
// prompt (GOPI.md) does contain markdown headings; that is why the cross-check
// against a real raw log is done with `Contains` on the whole block, not with a
// section cut.
var fixtureSystemText = "SYSTEM-PROMPT-MARKER\n" + strings.Repeat("rule line: 不许截断。\n", 4000)

// fixtureRequest is a small request carrying one of every block kind the
// renderer branches on, including an image whose base64 must never surface.
func fixtureRequest(t *testing.T) *deepseek.ClaudeRequest {
	t.Helper()
	imgBytes := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 64)
	imgB64 := base64.StdEncoding.EncodeToString(imgBytes)
	return &deepseek.ClaudeRequest{
		Model:     "deepseek-flash",
		MaxTokens: 16384,
		System:    []deepseek.ClaudeSystemBlock{{Type: "text", Text: fixtureSystemText}},
		Tools:     []deepseek.ClaudeToolDef{{Name: "bash", Description: "run a command"}},
		Messages: []deepseek.ClaudeMessage{
			{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "hello " + strings.Repeat("x", 9000)}}},
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{
				{Type: "thinking", Thinking: "pondering"},
				{Type: "tool_use", ID: "call_1", Name: "bash", Input: map[string]interface{}{"command": strings.Repeat("y", 5000)}},
			}},
			{Role: "user", Content: []deepseek.ClaudeContentBlock{
				{Type: "tool_result", ToolUseID: "call_1", Content: "result " + strings.Repeat("z", 9000)},
				{Type: "image", Source: &deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: imgB64}},
			}},
		},
	}
}

// TestSystemBlocksAreVerbatimNeverClipped is THE test this epic exists for.
//
// The defect it catches: a per-block character budget (DefaultSystemChars) that
// cut the system prompt to a fraction of itself and labelled the cut — so the
// page answered "what did the model receive" with a subset. The operator's ruling
// (2026-11-09) is that the export is verbatim by definition: there is no `--full`
// flag and no completeness mode, so the ONLY correct system section is the block's
// own bytes.
//
// The criterion is BYTE EQUALITY with the input, not "no truncation marker":
// equality also catches a re-wrapped, re-indented or reordered block, which a
// marker-absence check would pass.
func TestSystemBlocksAreVerbatimNeverClipped(t *testing.T) {
	req := fixtureRequest(t)
	var buf bytes.Buffer
	if _, err := Render(&buf, req, TurnAddress(1, 1, 5, 5)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := systemSection(t, buf.String(), 1)
	if got != fixtureSystemText {
		t.Fatalf("the rendered system[1] is not the block's own bytes (%d chars rendered, %d in the request) — a context export that is a subset of the request is the defect this test exists for",
			len(got), len(fixtureSystemText))
	}
	// The retired budget's own number must not survive anywhere in the page: a
	// reintroduced clip that happened to leave the text alone (a short block) would
	// still print the label.
	for _, marker := range []string{"truncated: showed", "… ["} {
		if strings.Contains(buf.String(), marker) {
			t.Errorf("the page carries %q — no block is clipped any more, so a label claiming a cut is a lie in the other direction", marker)
		}
	}
}

// systemSection pulls one system block's body back out of the page: everything
// between `### system[N]` and the next heading. It is how a reader would do it
// with sed, which is the point of the heading carrying nothing but the ordinal.
func systemSection(t *testing.T, page string, n int) string {
	t.Helper()
	head := fmt.Sprintf(SystemBlockHeadingFormat, n)
	i := strings.Index(page, head)
	if i < 0 {
		t.Fatalf("the page has no %q heading:\n%s", head, sectionAround(page, SystemHeading))
	}
	body := page[i+len(head):]
	body = strings.TrimPrefix(body, "\n\n")
	// The renderer writes the block, then one blank line, then the next heading
	// (`%s\n\n` + `### …` / `## …`). So the block ends at the first blank line
	// followed by a heading — which is exact, because the block's own bytes are
	// the thing being measured and this fixture carries no heading of its own.
	if j := strings.Index(body, "\n\n#"); j >= 0 {
		body = body[:j]
	}
	return body
}

// TestImageBlocksAreSummarizedNeverDumped is the epic's hard requirement: the
// raw base64 must not reach the page, and what replaces it must be the fact a
// reader can act on (kind, size, hash).
func TestImageBlocksAreSummarizedNeverDumped(t *testing.T) {
	req := fixtureRequest(t)
	var buf bytes.Buffer
	counts, err := Render(&buf, req, TurnAddress(1, 1, 5, 5))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	page := buf.String()
	imgB64 := req.Messages[2].Content[1].Source.Data
	if strings.Contains(page, imgB64) {
		t.Fatalf("the page contains the image's base64 — a page carrying it IS the JSON again")
	}
	if strings.Contains(page, imgB64[:64]) {
		t.Fatalf("the page contains a 64-byte slice of the base64 — even a prefix is raw payload")
	}
	if counts.ImageBlocks != 1 {
		t.Errorf("ImageBlocks = %d, want 1", counts.ImageBlocks)
	}
	// The summary carries the three facts and nothing machine-only.
	for _, want := range []string{"[image: png", "sha256:"} {
		if !strings.Contains(page, want) {
			t.Errorf("the image summary is missing %q; page section: %s", want,
				sectionAround(page, "[image:"))
		}
	}
	// A nil source must not panic, and must say what it does not know.
	if got := ImageSummary(nil); !strings.Contains(got, ImageNotDecodable) {
		t.Errorf("ImageSummary(nil) = %q, want it to name the undecidable hash", got)
	}
	// A file_id source has no bytes at all: it is named, not hashed.
	if got := ImageSummary(&deepseek.ClaudeImageSource{Type: "file", FileID: "file-abc"}); !strings.Contains(got, "file-abc") {
		t.Errorf("ImageSummary(file) = %q, want the file id", got)
	}
}

// TestReExportingTheSameAddressOverwrites is the "one page per file" property:
// the SAME address exported twice must leave ONE page, not two appended.
//
// The measured defect: `gopi session context <id> --last --out /tmp/ctx.md` run
// twice left a file holding two `## Request` sections. The cause is a writer
// opened without O_TRUNC; the cure is ExportOpenFlags, and this test pins the
// BEHAVIOUR rather than the flag so a caller that grows its own OpenFile is
// caught too.
func TestReExportingTheSameAddressOverwrites(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "mine.md")
	req := fixtureRequest(t)
	first, err := WriteTo(out, dir, req, TurnAddress(1, 1, 5, 5))
	if err != nil {
		t.Fatalf("WriteTo(first): %v", err)
	}
	firstPage, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("read back the first export: %v", rerr)
	}
	second, err := WriteTo(out, dir, req, TurnAddress(1, 1, 5, 5))
	if err != nil {
		t.Fatalf("WriteTo(second): %v", err)
	}
	secondPage, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("read back the second export: %v", rerr)
	}
	if n := strings.Count(string(secondPage), RequestHeading); n != 1 {
		t.Fatalf("the file holds %d %q sections after two exports of the same address, want 1:\n%s",
			n, RequestHeading, sectionAround(string(secondPage), RequestHeading))
	}
	// The strongest statement of "overwrite": the second file is BYTE-EQUAL to the
	// first, and no larger. A writer that appended nothing but a header would fail
	// the count check above; one that appended nothing at all but grew the file
	// through a BOM would fail this one.
	if !bytes.Equal(firstPage, secondPage) {
		t.Errorf("two exports of the same request produced different bytes (%d vs %d) — the page is not a function of the request alone",
			first.Bytes, second.Bytes)
	}
	if second.Bytes != first.Bytes {
		t.Errorf("the file grew from %d to %d bytes across two exports of the same address — that is an append, not an overwrite",
			first.Bytes, second.Bytes)
	}
}

// TestThePageCarriesNoCountersOrNotes pins the operator's second ruling: the page
// holds the model's bytes and the address that identifies them — no decorative
// statistics, no notes about what the tool did.
//
// The defect it catches is the old page's `### system[1] — 4711 chars, 1281
// tokens` heading, its Totals table, its truncation preamble and its "the full
// schema is in the raw log" sentence. None of those are bytes the model received,
// and each one sits exactly where a reader diffing against the raw log must not
// find anything.
func TestThePageCarriesNoCountersOrNotes(t *testing.T) {
	req := fixtureRequest(t)
	var buf bytes.Buffer
	if _, err := Render(&buf, req, TurnAddress(1, 1, 5, 5)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	page := buf.String()
	for _, banned := range []string{
		"## Totals",                          // the totals table
		"truncated",                          // the truncation preamble and the Totals note
		"rendered by name and size",          // the tools explanation
		"The exact body this model received", // the page's own preamble
		"Truncated blocks are labelled",      // the truncation preamble
	} {
		if strings.Contains(page, banned) {
			t.Errorf("the page carries %q — the export is the model's bytes plus the address that names them, never a statistic about them:\n%s",
				banned, sectionAround(page, banned))
		}
	}
	// THE BAN IS ON HEADINGS, NOT ON THE WHOLE PAGE: the model's own system text
	// legitimately speaks of characters and tokens (the rules this very epic's
	// operator wrote do), so a page-wide substring ban would fail on the CONTENT
	// it exists to preserve. What must never appear is a HEADING carrying a
	// measurement — that is the shape the old page had (`### system[1] — 4711
	// chars, 1281 tokens`), and it sits exactly where a reader looks for the
	// block and finds a count instead.
	for _, line := range strings.Split(page, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		for _, bannedInHeading := range []string{"chars", "tokens", "block(s)"} {
			if strings.Contains(line, bannedInHeading) {
				t.Errorf("a heading carries %q: %q — headings name what follows and nothing about it",
					bannedInHeading, line)
			}
		}
	}
	// The headings that REMAIN must carry the address and nothing measured. This
	// is the positive half: a page stripped down to nothing would pass the loop
	// above, so the shapes that must survive are asserted too.
	for _, want := range []string{
		fmt.Sprintf(PageTitleTurnFormat, 1),
		RequestHeading,
		SystemHeading,
		ToolsHeading,
		MessagesHeading,
		fmt.Sprintf(MessageHeadingFormat, 1, "user"),
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lost %q — the decorative lines were removed, not the structure that lets a reader find the bytes", want)
		}
	}
	// AND THE FURNITURE SPEAKS IN TURNS ONLY (operator ruling, 2026-11-10): the
	// title and the address table carry the TURN and no round number, no interval
	// of rounds and no tool-call count. Measured on the FURNITURE, never the whole
	// page: the system prompt below is verbatim model bytes and does mention
	// rounds (this repository's own rules do, at length), and that rule wins.
	if n := furnitureRoundHits(t, page); n != 0 {
		t.Errorf("the page's own furniture carries %d round mention(s), want 0:\n%s", n, furniture(page))
	}
	if !strings.Contains(furniture(page), fmt.Sprintf("turn %d", 1)) {
		t.Errorf("the furniture does not state the turn the page holds:\n%s", furniture(page))
	}
	for _, banned := range []string{"tool block", "block(s)", "rounds", "rendered round"} {
		if strings.Contains(strings.ToLower(furniture(page)), banned) {
			t.Errorf("the furniture carries %q — the page header names the TURN, never a request count or a tool count:\n%s",
				banned, furniture(page))
		}
	}
}

// TestUnrenderableJSONIsStatedNotCountedAsZero pins the branches that would
// otherwise be a SILENT ZERO.
//
// json.Marshal can fail (a value of a channel or func type reaches these fields
// through `interface{}`), and the discarded result used to be rendered straight
// into a count: the tool line read "schema 0 chars", the tool_use section
// "(no input)", and the unknown-block section "0 chars" — three readings that
// claim the request held nothing, on the one page whose whole purpose is to be
// an honest view of it (Rule 15: a failure is STATED, never rendered as a
// measurement of zero).
func TestUnrenderableJSONIsStatedNotCountedAsZero(t *testing.T) {
	unrenderable := func() interface{} { return make(chan int) }
	req := &deepseek.ClaudeRequest{
		Model: "fixture",
		Tools: []deepseek.ClaudeToolDef{{Name: "broken", Description: "a schema no marshaller can take", InputSchema: unrenderable()}},
		Messages: []deepseek.ClaudeMessage{
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{
				{Type: "tool_use", ID: "call_broken", Name: "broken", Input: map[string]interface{}{"chan": make(chan int)}},
			}},
			{Role: "user", Content: []deepseek.ClaudeContentBlock{
				{Type: "brand_new_wire_type", Input: map[string]interface{}{"chan": make(chan int)}},
			}},
		},
	}
	var buf bytes.Buffer
	if _, err := Render(&buf, req, TurnAddress(1, 1, 5, 5)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	page := buf.String()

	// ② The tool_use block names the failure instead of falling through to
	// "(no input)" — which is the message for a call that genuinely carried none.
	if !strings.Contains(page, "input UNMEASURABLE") {
		t.Errorf("an unmeasurable tool input is not stated as such:\n%s", sectionAround(page, "tool_use `broken`"))
	}
	if strings.Contains(page, ToolInputEmpty) {
		t.Errorf("an unmeasurable input rendered as %q, the message reserved for a call that carried no input", ToolInputEmpty)
	}

	// ③ The unrecognised block type reports the failure rather than an empty section.
	if !strings.Contains(page, "unrenderable") {
		t.Errorf("an unrenderable unknown block is not stated as such:\n%s", sectionAround(page, "brand_new_wire_type"))
	}

	// ① The tool definition is still LISTED by name: the schema is no longer
	// rendered, so the unmeasurable schema is no longer a silent zero on this
	// page at all — but a tool that vanished from the list would be.
	if !strings.Contains(page, fmt.Sprintf(ToolsLineFormat, "broken")) {
		t.Errorf("the unmeasurable tool disappeared from the tools list:\n%s", sectionAround(page, ToolsHeading))
	}
}

// TestRenderRefusesAnEmptyRequest pins that no request is an error rather than an
// empty page (an empty page reads as "this request held nothing").
func TestRenderRefusesAnEmptyRequest(t *testing.T) {
	if _, err := Render(&bytes.Buffer{}, nil, TurnAddress(1, 1, 5, 5)); err == nil {
		t.Errorf("Render(nil) returned no error — an empty page would read as 'this request held nothing'")
	}
	// An invalid ADDRESS is refused too: a page written under an unmeasurable turn
	// is the mislabelled artifact this epic removes.
	if _, err := Render(&bytes.Buffer{}, fixtureRequest(t), TurnAddress(0, 0, 0, 0)); err == nil {
		t.Errorf("Render(turn=0) returned no error — its header would name a turn nobody measured")
	}
}

// TestWriteNamesTheFileForTheTurnItHolds is the artifact-name rule: the name is
// a claim about the bytes beside it.
func TestWriteNamesTheFileForTheTurnItHolds(t *testing.T) {
	dir := t.TempDir()
	req := fixtureRequest(t)
	sum, err := Write(dir, req, TurnAddress(42, 400, 420, 420))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := filepath.Join(dir, "context-turn42.md")
	if sum.Path != want {
		t.Errorf("path = %q, want %q", sum.Path, want)
	}
	if !strings.HasSuffix(sum.Path, fmt.Sprintf(FileNameFormat, 42)) {
		t.Errorf("the name does not use FileNameFormat with the turn: %q", sum.Path)
	}
	onDisk, rerr := os.ReadFile(sum.Path)
	if rerr != nil {
		t.Fatalf("read back: %v", rerr)
	}
	if int64(len(onDisk)) != sum.Bytes {
		t.Errorf("the reported size (%d) is not the file's (%d) — the one line on screen must describe the file", sum.Bytes, len(onDisk))
	}
	if !strings.Contains(string(onDisk), "turn 42") {
		t.Errorf("the page header does not name the turn it holds")
	}
	// An unnamed turn is REFUSED, never written as turn 0.
	if _, err := Write(dir, req, TurnAddress(0, 0, 0, 0)); err == nil {
		t.Errorf("Write(turn=0) wrote a file — its name would not describe its bytes")
	} else if !strings.Contains(err.Error(), ErrTurnUnmeasurable.Error()) {
		t.Errorf("the turn-0 refusal does not name the unmeasurable turn: %v", err)
	}
	// The screen line carries the path and the counts, and no page body. Those
	// counts are the SCREEN's, which is why they survive the page's own ban on
	// statistics: the screen is one line and the file is the bytes.
	//
	// AND THE SCREEN LINE SPEAKS IN TURNS ONLY (operator ruling, 2026-11-10): the
	// unit is the TURN, never a tool call and never a round. The screen is the
	// most operator-visible surface this package has, so the count it carries is
	// the FILE's (bytes, messages) rather than the model's internal traffic.
	line := sum.Line()
	if !strings.Contains(line, sum.Path) || !strings.Contains(line, "bytes") {
		t.Errorf("the console line does not describe the artifact: %q", line)
	}
	for _, banned := range []string{"tool", "round", "block(s)"} {
		if strings.Contains(strings.ToLower(line), banned) {
			t.Errorf("the console line carries %q — it counts TURNS' artifacts in bytes and messages, never in tool calls or rounds: %q",
				banned, line)
		}
	}
	if !strings.Contains(line, fmt.Sprintf("TURN %d", 42)) {
		t.Errorf("the console line does not state the turn the artifact holds: %q", line)
	}
	// ONE line — the epic's rule. A single trailing newline is the terminator, not
	// a second line, so the count is taken after stripping it.
	if got := strings.Count(strings.TrimRight(line, "\n"), "\n"); got != 0 {
		t.Errorf("the console line is %d lines long, want 1: %q", got+1, line)
	}
}

// TestWriteToHonoursAnExplicitDestination pins the --out door, and that a
// derived write still lands in the directory it was given.
func TestWriteToHonoursAnExplicitDestination(t *testing.T) {
	base := t.TempDir()
	out := filepath.Join(base, "nested", "mine.md")
	sum, err := WriteTo(out, filepath.Join(base, "sessions"), fixtureRequest(t), TurnAddress(3, 21, 30, 30))
	if err != nil {
		t.Fatalf("WriteTo(explicit): %v", err)
	}
	if sum.Path != out {
		t.Errorf("path = %q, want the explicit %q", sum.Path, out)
	}
	if _, serr := os.Stat(out); serr != nil {
		t.Errorf("the explicit destination was not created: %v", serr)
	}
	derived, err := WriteTo("", filepath.Join(base, "sessions"), fixtureRequest(t), TurnAddress(3, 21, 30, 30))
	if err != nil {
		t.Fatalf("WriteTo(derived): %v", err)
	}
	if derived.Path != filepath.Join(base, "sessions", fmt.Sprintf(FileNameFormat, 3)) {
		t.Errorf("the derived path is not FileNameFormat in the given directory: %q", derived.Path)
	}
}

// TestFromRawLogSelectsTheRoundItSaysItDid is the --round / --last contract, and
// the naming consequence: the returned ordinal is the ROUND the request really
// is, so the artifact cannot be named after a neighbour.
//
// IT IS NAMED FOR THE ROUND BECAUSE THAT IS THE AXIS IT WALKS. A raw log holds
// one record per HTTP request/response, so its sequence numbers are ROUNDS; the
// previous name said "Turn" and that is the very conflation this epic removed
// from the code — leaving it in the test name would keep teaching the wrong word
// at the one place an author copies from.
func TestFromRawLogSelectsTheRoundItSaysItDid(t *testing.T) {
	path := writeFixtureLog(t, 4)
	// A numbered round walks forward and stops there.
	for _, want := range []int{1, 2, 3, 4} {
		req, got, err := FromRawLog(path, want)
		if err != nil {
			t.Fatalf("FromRawLog(round %d): %v", want, err)
		}
		if got != want {
			t.Errorf("FromRawLog(round %d) reported round %d", want, got)
		}
		if len(req.Messages) == 0 || req.Messages[0].Content[0].Text != fmt.Sprintf("round-%d", want) {
			t.Errorf("FromRawLog(round %d) returned a different round's body", want)
		}
	}
	// --last selects the newest, and REPORTS its real ordinal (not 0, not a
	// head-sample count).
	req, got, err := FromRawLog(path, 0)
	if err != nil {
		t.Fatalf("FromRawLog(last): %v", err)
	}
	if got != 4 {
		t.Errorf("FromRawLog(last) reported round %d, want 4 — the name must match the bytes", got)
	}
	if req.Messages[0].Content[0].Text != "round-4" {
		t.Errorf("FromRawLog(last) returned %q, want the newest body", req.Messages[0].Content[0].Text)
	}
	// Out of range is REFUSED with the range that would have worked — and the
	// refusal must speak the axis the selector did.
	_, _, err = FromRawLog(path, 9)
	if err == nil {
		t.Fatalf("FromRawLog(round 9) succeeded on a 4-request log")
	}
	if !strings.Contains(err.Error(), "holds 4 request(s)") || !strings.Contains(err.Error(), "no ROUND 9") {
		t.Errorf("the out-of-range refusal does not name the count and the round: %v", err)
	}
	if !strings.Contains(err.Error(), "NOT a turn") {
		t.Errorf("the out-of-range refusal does not separate the two axes, so a reader cannot tell which number they gave: %v", err)
	}
}

// writeFixtureLog writes a FRAMED raw log holding n request records, each
// carrying a body that identifies its own turn, so selection can be proved by
// content rather than by a counter the reader also owns.
func writeFixtureLog(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.raw.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture log: %v", err)
	}
	defer f.Close()
	for i := 1; i <= n; i++ {
		req := deepseek.ClaudeRequest{
			Model: "fixture",
			Messages: []deepseek.ClaudeMessage{{
				Role:    "user",
				Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: fmt.Sprintf("round-%d", i)}},
			}},
		}
		body, merr := json.Marshal(req)
		if merr != nil {
			t.Fatalf("marshal request %d: %v", i, merr)
		}
		if _, werr := f.Write(rawlogfmt.Encode(rawlogfmt.TagRequest, body)); werr != nil {
			t.Fatalf("write request %d: %v", i, werr)
		}
	}
	return path
}

// sectionAround returns the text around the first occurrence of marker, for a
// failure message that shows the reader what was actually written.
func sectionAround(page, marker string) string {
	i := strings.Index(page, marker)
	if i < 0 {
		return "(marker absent)"
	}
	end := i + 200
	if end > len(page) {
		end = len(page)
	}
	return page[i:end]
}

// furniture is the part of a page that the RENDERER wrote — everything above
// the system prompt, i.e. the title and the address table. It is the boundary
// the operator's ruling draws (2026-11-10): the FURNITURE must speak in turns
// and nothing else, while the body below `## System prompt` is the model's own
// bytes, verbatim, and legitimately mentions rounds (this repository's GOPI.md
// and PROJECT.md do, at length). A page-wide substring ban would therefore
// measure the content the export exists to preserve, not the renderer.
func furniture(page string) string {
	if i := strings.Index(page, SystemHeading); i >= 0 {
		return page[:i]
	}
	return page
}

// furnitureRoundHits counts case-insensitive "round" occurrences in the part of
// the page the RENDERER wrote. The acceptance criterion is 0 — a round number is
// a model-layer detail and must not reach an operator-facing page header.
func furnitureRoundHits(t *testing.T, page string) int {
	t.Helper()
	return strings.Count(strings.ToLower(furniture(page)), "round")
}
