// section_test.go — the SECTION export's red lines (`--system` / `--user`).
//
// The defect family these tests exist for:
//
//   - a filtered export written to the SAME name as the full page would destroy
//     the one artifact that can be diffed against the raw HTTP log, and the loss
//     would be silent (the name would look right, the bytes would be a subset) —
//     TestSectionExportsHaveTheirOwnNames;
//   - `--system --user` choosing one of the two silently is the failure Rule 15
//     names: the operator asked for both and would read one — TestSectionFor
//     RefusesBothSelectors;
//   - a section export that ALSO changed the full page would break the "two
//     exports of one request are byte-equal" promise — TestFullPageIsUnchangedBy
//     Sections compares the SectionAll render against the page rendered by the
//     pre-section entry point (Render), which still exists for that reason;
//   - a half-section (system + tools but no message headings, say) would be the
//     retired truncation wearing a flag's name — TestSystemSectionCarriesNo
//     Messages and its mirror assert the whole other side is ABSENT, by heading.
package ctxdump

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSectionForRefusesBothSelectors is the mutual-exclusion rule: the two
// selectors name the two halves of ONE request, so the pair is not a union — it
// is a request for the full page under a filtered name, which is exactly the
// confusion the two names exist to prevent. The refusal must NAME both flags.
func TestSectionForRefusesBothSelectors(t *testing.T) {
	sec, err := SectionFor(true, true)
	if err == nil {
		t.Fatalf("SectionFor(system, user) returned no error and section %q — one of the two halves would be dropped silently", sec)
	}
	for _, want := range []string{SystemSelector, UserSelector, "pick one"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// TestSectionForMapsSelectors pins the rest of the mapping, including the zero
// value: NO selector is the full page, because every caller written before
// sections existed passes nothing.
func TestSectionForMapsSelectors(t *testing.T) {
	cases := []struct {
		system, user bool
		want         Section
	}{
		{false, false, SectionAll},
		{true, false, SectionSystem},
		{false, true, SectionUser},
	}
	for _, c := range cases {
		got, err := SectionFor(c.system, c.user)
		if err != nil {
			t.Fatalf("SectionFor(%v, %v): %v", c.system, c.user, err)
		}
		if got != c.want {
			t.Errorf("SectionFor(%v, %v) = %q, want %q", c.system, c.user, string(got), string(c.want))
		}
	}
}

// TestFullPageIsTheZeroValue pins the type's own default: a Section nobody set is
// the full page, so an un-edited caller cannot acquire a filter.
func TestFullPageIsTheZeroValue(t *testing.T) {
	var s Section
	if !s.IsFull() || !s.Valid() {
		t.Fatalf("the zero Section is not the full page (full=%v valid=%v) — every pre-section caller would export a subset", s.IsFull(), s.Valid())
	}
	if !s.WantsSystem() || !s.WantsTools() || !s.WantsMessages() {
		t.Errorf("the full page must want every section (system=%v tools=%v messages=%v)", s.WantsSystem(), s.WantsTools(), s.WantsMessages())
	}
	if w := s.LineWords(); w != "" {
		t.Errorf("the full page appends %q to the screen line — its line must keep the exact shape it has always had", w)
	}
}

// TestSectionExportsHaveTheirOwnNames is the data-loss guard: the full page's
// name is the evidence's address and must not move, and neither filtered view may
// take it.
func TestSectionExportsHaveTheirOwnNames(t *testing.T) {
	turnAddr := TurnAddress(3, 21, 30, 30)
	roundAddr := RoundAddress(7)
	cases := []struct {
		sec  Section
		addr Address
		want string
	}{
		{SectionAll, turnAddr, "context-turn3.md"},
		{SectionSystem, turnAddr, "context-turn3-system.md"},
		{SectionUser, turnAddr, "context-turn3-messages.md"},
		{SectionAll, roundAddr, "context-round7.md"},
		{SectionSystem, roundAddr, "context-round7-system.md"},
		{SectionUser, roundAddr, "context-round7-messages.md"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		got := c.sec.FileName(c.addr)
		if got != c.want {
			t.Errorf("Section(%q).FileName = %q, want %q", string(c.sec), got, c.want)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("two exports share the name %q (%q and %q) — one would overwrite the other", got, prev, string(c.sec))
		}
		seen[got] = string(c.sec)
	}
	// The full page's name must remain the ONE constant the promise names.
	if got := SectionAll.FileName(turnAddr); got != turnAddr.FileName() {
		t.Errorf("the full page's section name %q is not the address's own name %q — the diffable artifact changed its address", got, turnAddr.FileName())
	}
}

// TestSystemSectionCarriesNoMessages is the `--system` criterion: the address
// table, the system prompt and the tools, and NOTHING of the conversation.
func TestSystemSectionCarriesNoMessages(t *testing.T) {
	req := fixtureRequest(t)
	var buf bytes.Buffer
	if _, err := RenderSection(&buf, req, TurnAddress(1, 1, 5, 5), SectionSystem); err != nil {
		t.Fatalf("RenderSection(system): %v", err)
	}
	page := buf.String()
	for _, want := range []string{RequestHeading, SystemHeading, ToolsHeading, "SYSTEM-PROMPT-MARKER"} {
		if !strings.Contains(page, want) {
			t.Errorf("the system export does not carry %q", want)
		}
	}
	for _, unwanted := range []string{MessagesHeading, "hello ", "### 1 · user"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the system export carries %q — a `--system` page must not include the conversation", unwanted)
		}
	}
	// It says on its own page that it is a view (the name is the caller's when
	// --out is used, so the page is the only thing that can state it).
	if !strings.Contains(page, "section export:") {
		t.Errorf("the filtered page does not state that it is a section export:\n%s", firstLines(page, 6))
	}
}

// TestUserSectionCarriesNoSystemOrTools is the `--user` criterion, the mirror.
func TestUserSectionCarriesNoSystemOrTools(t *testing.T) {
	req := fixtureRequest(t)
	var buf bytes.Buffer
	if _, err := RenderSection(&buf, req, TurnAddress(1, 1, 5, 5), SectionUser); err != nil {
		t.Fatalf("RenderSection(user): %v", err)
	}
	page := buf.String()
	for _, want := range []string{RequestHeading, MessagesHeading, "hello ", "ponder"} {
		if !strings.Contains(page, want) {
			t.Errorf("the messages export does not carry %q", want)
		}
	}
	for _, unwanted := range []string{SystemHeading, ToolsHeading, "SYSTEM-PROMPT-MARKER", "- `bash`"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the messages export carries %q — a `--user` page must not include the system prompt or the tool list", unwanted)
		}
	}
}

// TestSectionCountsReportOnlyWhatWasWritten pins the Summary/Counts half of the
// epic's requirement: the one screen line must not claim to have measured a
// section the file does not hold. A system export reports ZERO messages; a
// messages export reports the real stream and no tool definitions.
func TestSectionCountsReportOnlyWhatWasWritten(t *testing.T) {
	req := fixtureRequest(t)
	var sysBuf, userBuf, allBuf bytes.Buffer
	sysCounts, err := RenderSection(&sysBuf, req, TurnAddress(1, 1, 5, 5), SectionSystem)
	if err != nil {
		t.Fatalf("RenderSection(system): %v", err)
	}
	if sysCounts.Messages != 0 {
		t.Errorf("the system export reports %d message(s) — it wrote none", sysCounts.Messages)
	}
	if sysCounts.ToolDefs != len(req.Tools) {
		t.Errorf("the system export reports %d tool definition(s), want %d", sysCounts.ToolDefs, len(req.Tools))
	}
	userCounts, err := RenderSection(&userBuf, req, TurnAddress(1, 1, 5, 5), SectionUser)
	if err != nil {
		t.Fatalf("RenderSection(user): %v", err)
	}
	if userCounts.Messages != len(req.Messages) {
		t.Errorf("the messages export reports %d message(s), want %d", userCounts.Messages, len(req.Messages))
	}
	if userCounts.ToolDefs != 0 {
		t.Errorf("the messages export reports %d tool definition(s) — it wrote no tool section", userCounts.ToolDefs)
	}
	allCounts, err := RenderSection(&allBuf, req, TurnAddress(1, 1, 5, 5), SectionAll)
	if err != nil {
		t.Fatalf("RenderSection(all): %v", err)
	}
	if allCounts.Messages != len(req.Messages) || allCounts.ToolDefs != len(req.Tools) {
		t.Errorf("the full export reports %d message(s)/%d tool(s), want %d/%d",
			allCounts.Messages, allCounts.ToolDefs, len(req.Messages), len(req.Tools))
	}
}

// TestSummaryLineNamesTheSection pins the screen line: the full export keeps its
// exact shape (no clause), and each filtered export appends one clause naming
// what it holds. Without it, two exports of one turn print identical lines and a
// reader cannot tell which file the size belongs to.
func TestSummaryLineNamesTheSection(t *testing.T) {
	addr := TurnAddress(2, 10, 20, 20)
	full := Summary{Path: "/tmp/f.md", Bytes: 10, Address: addr, Section: SectionAll}
	if line := full.Line(); !strings.Contains(line, "address: TURN 2") || strings.Contains(line, "section:") {
		t.Errorf("the full line changed shape: %q", line)
	}
	for _, c := range []struct {
		sec  Section
		want string
	}{
		{SectionSystem, SectionSystemWords},
		{SectionUser, SectionUserWords},
	} {
		line := Summary{Path: "/tmp/f.md", Bytes: 10, Address: addr, Section: c.sec}.Line()
		if !strings.Contains(line, c.want) {
			t.Errorf("the %q line does not name the section (%q): %q", string(c.sec), c.want, line)
		}
		if strings.Count(line, "\n") != 1 {
			t.Errorf("the %q screen line is not ONE line: %q", string(c.sec), line)
		}
	}
}

// TestWriteSectionWritesItsOwnFile is the end-to-end half of the naming rule:
// the file ON DISK for a filtered export is a different file from the full
// page's, and both exist afterwards.
func TestWriteSectionWritesItsOwnFile(t *testing.T) {
	dir := t.TempDir()
	req := fixtureRequest(t)
	addr := TurnAddress(4, 40, 44, 44)
	full, err := Write(dir, req, addr)
	if err != nil {
		t.Fatalf("Write(full): %v", err)
	}
	sys, err := WriteSection(dir, req, addr, SectionSystem)
	if err != nil {
		t.Fatalf("WriteSection(system): %v", err)
	}
	usr, err := WriteSection(dir, req, addr, SectionUser)
	if err != nil {
		t.Fatalf("WriteSection(user): %v", err)
	}
	if full.Path == sys.Path || full.Path == usr.Path || sys.Path == usr.Path {
		t.Fatalf("the three exports did not land on three files: %q %q %q", full.Path, sys.Path, usr.Path)
	}
	wantFull := filepath.Join(dir, "context-turn4.md")
	if full.Path != wantFull {
		t.Errorf("the full page is not at %q: %q — its name is the diffable evidence's address", wantFull, full.Path)
	}
	for _, c := range []struct{ path, name string }{
		{sys.Path, "context-turn4-system.md"},
		{usr.Path, "context-turn4-messages.md"},
	} {
		if filepath.Base(c.path) != c.name {
			t.Errorf("a section export landed at %q, want %q", c.path, c.name)
		}
		if _, serr := os.Stat(c.path); serr != nil {
			t.Errorf("the section export's file is not on disk: %v", serr)
		}
	}
	// The evidence still holds the whole request after the two filtered writes.
	page, rerr := os.ReadFile(full.Path)
	if rerr != nil {
		t.Fatalf("re-reading the full page: %v", rerr)
	}
	if !strings.Contains(string(page), MessagesHeading) || !strings.Contains(string(page), SystemHeading) {
		t.Errorf("the full page was overwritten by a filtered export — its bytes are no longer the whole request")
	}
}

// TestFullPageIsUnchangedBySections is the compatibility criterion: the sectioned
// renderer must emit EXACTLY the bytes the pre-section entry point emits when the
// section is the full page. `Render` is that pre-section entry point (it
// delegates here), so this compares the two doors directly — and it must be an
// empty-diff, not a "looks the same".
func TestFullPageIsUnchangedBySections(t *testing.T) {
	req := fixtureRequest(t)
	addr := TurnAddress(5, 50, 55, 55)
	var legacy, sectioned bytes.Buffer
	if _, err := Render(&legacy, req, addr); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := RenderSection(&sectioned, req, addr, SectionAll); err != nil {
		t.Fatalf("RenderSection(all): %v", err)
	}
	if legacy.String() != sectioned.String() {
		t.Fatalf("the full page differs between the two doors — len %d vs %d", legacy.Len(), sectioned.Len())
	}
	if !strings.Contains(legacy.String(), MessagesHeading) || !strings.Contains(legacy.String(), SystemHeading) || !strings.Contains(legacy.String(), ToolsHeading) {
		t.Errorf("the full page lost a section")
	}
	if strings.Contains(legacy.String(), "section export:") {
		t.Errorf("the full page carries the section banner — the full page is not a view")
	}
}

// TestUnknownSectionIsRefused pins Rule 15's other half: a Section value this
// package cannot render is named, never treated as the full page (which would
// make a typo export MORE than it asked for).
func TestUnknownSectionIsRefused(t *testing.T) {
	if _, err := RenderSection(&bytes.Buffer{}, fixtureRequest(t), TurnAddress(1, 1, 2, 2), Section("bogus")); err == nil {
		t.Errorf("RenderSection accepted an unknown section")
	}
	if _, err := WriteSection(t.TempDir(), fixtureRequest(t), TurnAddress(1, 1, 2, 2), Section("bogus")); err == nil {
		t.Errorf("WriteSection accepted an unknown section")
	}
}

// firstLines is the snippet a failure message shows for a page: enough to see the
// header, and never the whole 40 KB body.
func firstLines(page string, n int) string {
	lines := strings.SplitN(page, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
