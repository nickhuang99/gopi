package tools

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// The age render: now= anchor, stored=/age= (the WRITE instant) and
// about= (a date the row's own text states) as two independent facts.
//
// The defect this pins: `when=` was ONE derived column carrying whichever of
// the two semantics won at store time, so a row describing a 2026-08-01
// incident and written today rendered as if it had been written on 2026-08-01,
// and nothing in the result said what "now" was — an age with no anchor is a
// number the reader cannot check.
// ---------------------------------------------------------------------------

// TestMemoryAgeRenderAnchorAndFields: the result's FIRST line is the now=
// anchor, and one hit renders stored= + age= (+ about= when its text states a
// date). Fixed clock: the assertion pins the arithmetic, never the wall clock.
func TestMemoryAgeRenderAnchorAndFields(t *testing.T) {
	fixedNow := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	out, err := renderMemorySearchResultAt(`[{"id":3,"topic":"sweep","question":"why did the sweep reap it?","answer":"it listed branches by ancestry","tier":"warm","confidence":0.8,"lesson_type":"pitfall","when_at":"2026-07-01","distilled_at":`+
		itu(fixedNow.Add(-40*24*time.Hour).Unix())+`,"sources":["repo-a"]}]`, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(out, "\n")
	if want := memoryNowAnchorPrefix + fixedNow.Format(time.RFC3339); first != want {
		t.Errorf("the result's first line must be the now= anchor %q, got %q\n%s", want, first, out)
	}
	storedInstant := fixedNow.Add(-40 * 24 * time.Hour)
	for _, want := range []string{
		memoryStoredPrefix + storedInstant.Local().Format(whenAtDateTimeLayout),
		memoryAgePrefix + "40" + memoryAgeUnit,
		// The row's own when_at is 2026-07-01 and its text states no date, so
		// there is NO about= — the two facts are not the same field any more.
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, memoryAboutPrefix) {
		t.Errorf("a row whose text states no date must render no about=:\n%s", out)
	}
}

// TestMemoryAgeRenderStoredAndAboutAreIndependent is the epic's acceptance
// case, end to end through the REAL store and search: a row whose text carries
// a past date is rendered with BOTH the instant it was written and the date it
// talks about, and neither overwrites the other.
func TestMemoryAgeRenderStoredAndAboutAreIndependent(t *testing.T) {
	setMemoryDB(t)
	const stated = "2026-08-01"
	before := time.Now().Add(-time.Second).UTC()
	if _, err := MemoryStore("sweep replay", "the worktree vanished on "+stated+" during the sweep",
		"an answer body long enough to clear the store's garbage gate", "src", nil); err != nil {
		t.Fatal(err)
	}
	out, err := MemorySearch("worktree vanished during the sweep", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	block := firstHitBlock(t, out)

	// stored= is the WRITE instant (distilled_at, today-ish), not the stated date.
	storedAt := mustStoredInstant(t, block)
	if storedAt.Before(before.Add(-2 * time.Second)) {
		t.Errorf("stored= must be the write instant, got %s (searched at %s)\n%s",
			storedAt.Format(time.RFC3339), before.Format(time.RFC3339), block)
	}
	if stated == storedAt.UTC().Format(whenAtDateLayout) {
		t.Errorf("stored= must not be the date the row's TEXT states:\n%s", block)
	}
	// about= is the date the text states, exactly.
	if !strings.Contains(block, memoryAboutPrefix+stated) {
		t.Errorf("the date the row's text states must render as about=%s:\n%s", stated, block)
	}
	// And the two do not overwrite each other: both fields are on the same line.
	if !strings.Contains(block, memoryStoredPrefix) || !strings.Contains(block, memoryAboutPrefix) {
		t.Errorf("one rendered line must carry stored= AND about=:\n%s", block)
	}
	// The in-result age agrees with the anchor on the same result.
	anchor := mustNowAnchor(t, out)
	if days := int(time.Since(anchor).Hours() / 24); days < 0 {
		t.Errorf("the now= anchor must not be in the future, got %s", anchor.Format(time.RFC3339))
	}
}

// TestMemoryAgeRenderLegacyRowFallsBackToWhenAt: a row written before
// distilled_at existed has no write column, so the render falls back to
// when_at — the same instant the old when= showed, unchanged — and still
// reports about= from the text when the text states a date.
func TestMemoryAgeRenderLegacyRowFallsBackToWhenAt(t *testing.T) {
	fixedNow := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	out, err := renderMemorySearchResultAt(`[{"id":9,"topic":"legacy","question":"what happened on 2026-08-02?","answer":"power loss","tier":"warm","confidence":0.5,"lesson_type":"lesson","when_at":"2026-08-02"}]`, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantStored := memoryStoredPrefix + time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC).Local().Format(whenAtDateTimeLayout)
	if !strings.Contains(out, wantStored) {
		t.Errorf("a row without distilled_at must render stored= from when_at:\n%s", out)
	}
	if !strings.Contains(out, memoryAgePrefix+"30"+memoryAgeUnit) {
		t.Errorf("the fallback instant must produce an age:\n%s", out)
	}
	if !strings.Contains(out, memoryAboutPrefix+"2026-08-02") {
		t.Errorf("the stated date must still render as about=:\n%s", out)
	}
}

// TestMemoryAgeRenderUnknownStoredIsStated: undatable is STATED, never
// fabricated (Rule 15) — and a future date in the text is not an observation,
// so it is dropped from about= exactly as deriveWhenAt drops it from when_at.
func TestMemoryAgeRenderUnknownStoredIsStated(t *testing.T) {
	fixedNow := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	out, err := renderMemorySearchResultAt(`[{"id":11,"topic":"undated","question":"no marker here","answer":"no date either","tier":"warm","confidence":0.5,"lesson_type":"lesson"}]`, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, memoryStoredUnknownMarker) {
		t.Errorf("an undatable row must say stored=unknown:\n%s", out)
	}
	if strings.Contains(out, memoryAgePrefix) {
		t.Errorf("an undatable row must render no age=:\n%s", out)
	}

	future, err := renderMemorySearchResultAt(`[{"id":12,"topic":"planned","question":"the rollout is planned for 2099-01-01","answer":"no date either","tier":"warm","confidence":0.5,"lesson_type":"lesson"}]`, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(future, memoryAboutPrefix) {
		t.Errorf("a FUTURE date is not an observation and must not render as about=:\n%s", future)
	}
}

// TestMemoryAgeRenderAnchorSurvivesTheSectionMerge: the now= anchor is the
// RESULT's first line even after the [distilled]/[session] sections are
// labelled — an anchor buried inside a section would not be an anchor.
func TestMemoryAgeRenderAnchorSurvivesTheSectionMerge(t *testing.T) {
	fixedNow := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	distilled, err := renderMemorySearchResultAt(`[{"id":1,"topic":"t","question":"q","answer":"a","tier":"warm","confidence":0.5,"lesson_type":"lesson","distilled_at":`+
		itu(fixedNow.Unix())+`}]`, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeMemorySearchSections(distilled, memorySessionSectionHeader+"\nno notes")
	first, _, _ := strings.Cut(merged, "\n")
	if first != memoryNowAnchorPrefix+fixedNow.Format(time.RFC3339) {
		t.Errorf("the anchor must stay the merged result's first line, got %q\n%s", first, merged)
	}
	if strings.Count(merged, memoryNowAnchorPrefix) != 1 {
		t.Errorf("the anchor must appear exactly once, got:\n%s", merged)
	}
	// The distilled half is still labelled FIRST among the sections.
	if strings.Index(merged, memoryDistilledSectionHeader) > strings.Index(merged, memorySessionSectionHeader) {
		t.Errorf("the distilled section must still come first:\n%s", merged)
	}
}

// --- helpers -------------------------------------------------------------

// itu renders a Unix second count as the JSON number the wire contract carries.
func itu(v int64) string {
	return strconv.FormatInt(v, 10)
}

// firstHitBlock returns the distilled half's first hit block (the meta line
// plus its Q/A), with the section label and the anchor already trimmed.
func firstHitBlock(t *testing.T, out string) string {
	t.Helper()
	if _, rest, ok := strings.Cut(out, memoryDistilledSectionHeader+"\n"); ok {
		out = rest
	}
	if head, _, ok := strings.Cut(out, "\n\n"+memorySessionSectionHeader); ok {
		out = head
	}
	block, _, _ := strings.Cut(out, "\n---\n")
	return block
}

// mustStoredInstant reads the stored= field out of a rendered block and parses
// it back into an instant (the render's own layout — the same one the reader
// copies). The value carries one space (a calendar datetime), so it is matched
// by its own shape rather than by space-splitting the meta line.
func mustStoredInstant(t *testing.T, block string) time.Time {
	t.Helper()
	m := storedFieldRe.FindStringSubmatch(block)
	if m == nil {
		t.Fatalf("the row was stored by this test, so it must carry a stored instant:\n%s", block)
	}
	ts, err := time.ParseInLocation(whenAtDateTimeLayout, m[1], time.Local)
	if err != nil {
		t.Fatalf("stored=%q is not parseable back: %v\n%s", m[1], err, block)
	}
	return ts
}

// storedFieldRe reads the stored= value (a calendar datetime with one space).
var storedFieldRe = regexp.MustCompile(regexp.QuoteMeta(memoryStoredPrefix) + `([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2})`)

// mustNowAnchor parses the result's first-line now= anchor.
func mustNowAnchor(t *testing.T, out string) time.Time {
	t.Helper()
	first, _, _ := strings.Cut(out, "\n")
	field := fieldValue(t, first, memoryNowAnchorPrefix)
	ts, err := time.Parse(time.RFC3339, field)
	if err != nil {
		t.Fatalf("the now= anchor %q is not RFC3339: %v\n%s", field, err, out)
	}
	return ts
}

// fieldValue returns the space-delimited word carrying prefix (prefix stripped)
// — the render's fields are one space-separated meta line.
func fieldValue(t *testing.T, line, prefix string) string {
	t.Helper()
	for _, f := range strings.Fields(line) {
		if strings.HasPrefix(f, prefix) {
			return strings.TrimPrefix(f, prefix)
		}
	}
	t.Fatalf("no %q field on the line %q", prefix, line)
	return ""
}
