package main

// cli_session_gc_test.go — the `gopi session gc` argv contract, and the one
// property the epic states as a hard requirement: THE DEFAULT IS A DRY RUN.
//
// WHY THIS TEST IS SEPARATE FROM THE PREDICATE'S OWN (internal/scratch/
// sessions_gc_test.go): the predicate there is proven never to delete; what is
// proven HERE is that the VERB never reaches it without being asked. Those are
// different failures — a predicate that deletes nothing, called by a verb that
// calls the destructive arm by default, still destroys a tree — and only the
// argv layer can be wrong about it.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// gcFixtureHome pins GOPI_HOME at a fresh scratch tree and materialises a
// sessions root with one aged, unclaimed container, returning the root and the
// container's path.
func gcFixtureHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		t.Fatalf("resolve the sessions root: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create %s: %v", root, err)
	}
	dir := filepath.Join(root, gcFixtureUUID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write the log: %v", err)
	}
	old := time.Now().Add(-(scratch.SessionsGCStaleAge + 48*time.Hour))
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("age %s: %v", dir, err)
	}
	return root, dir
}

const gcFixtureUUID = "0f8e2a10-9999-4000-8000-00000000cc09"

// ① THE DEFAULT IS A DRY RUN — flagless `gopi session gc` reports candidates and
// deletes NOTHING.
//
// The fixture home has no ledger at all, so the UNREFERENCED predicate cannot be
// answered and the verb must REFUSE (exit 1) rather than collect: that is the
// same arm, and the directory must still be there afterwards. The assertion is
// on the DIRECTORY, not on the text — a verb that printed "dry run" and deleted
// anyway would pass a text-only check.
func TestSessionGCFlaglessDeletesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		t.Fatalf("resolve the sessions root: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create %s: %v", root, err)
	}
	dir := filepath.Join(root, gcFixtureUUID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	old := time.Now().Add(-(scratch.SessionsGCStaleAge + 48*time.Hour))
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("age %s: %v", dir, err)
	}

	code := runSessionGC(nil)
	if code != 0 && code != 1 {
		t.Fatalf("flagless gc exited %d, want 0 (a dry run) or 1 (a refusal), never a destructive success", code)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a FLAGLESS `gopi session gc` deleted the container: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("a FLAGLESS `gopi session gc` deleted the sessions root: %v", err)
	}
}

// ② `--apply` AND `--dry-run` TOGETHER ARE REFUSED BY NAME. Preferring one
// silently would make the operator's intent a coin flip (Rule 15).
func TestSessionGCDryRunAndApplyAreExclusive(t *testing.T) {
	opts, code := parseSessionGCArgs([]string{sessionGCDryRunFlag, sessionGCApplyFlag})
	if code != 2 {
		t.Fatalf("--dry-run --apply exited %d, want the usage refusal 2 (opts %+v)", code, opts)
	}
}

// ③ THE FLAGS PARSE AS THE PAGE PROMISES: a bare count is DAYS, a duration is
// honoured, and the default window is the scratch constant.
func TestSessionGCArgParsing(t *testing.T) {
	cases := []struct {
		args  []string
		apply bool
		win   time.Duration
		json  bool
	}{
		{nil, false, scratch.SessionsGCStaleAge, false},
		{[]string{sessionGCApplyFlag}, true, scratch.SessionsGCStaleAge, false},
		{[]string{"--older-than", "30"}, false, 30 * 24 * time.Hour, false},
		{[]string{"--older-than", "72h"}, false, 72 * time.Hour, false},
		{[]string{"--older-than=5"}, false, 5 * 24 * time.Hour, false},
		{[]string{sessionGCJSONFlag}, false, scratch.SessionsGCStaleAge, true},
	}
	for _, c := range cases {
		opts, code := parseSessionGCArgs(c.args)
		if code != -1 {
			t.Fatalf("%v: exit %d, want -1 (continue)", c.args, code)
		}
		if opts.apply != c.apply || opts.olderThan != c.win || opts.json != c.json {
			t.Fatalf("%v: parsed %+v, want apply=%v window=%s json=%v", c.args, opts, c.apply, c.win, c.json)
		}
	}
	// A REFUSED VALUE IS A USAGE ERROR, not a silently-defaulted window.
	for _, bad := range []string{"0", "-1", "soon", ""} {
		if _, code := parseSessionGCArgs([]string{"--older-than", bad}); code != 2 {
			t.Fatalf("--older-than %q exited %d, want the usage refusal 2", bad, code)
		}
	}
}

// ③b THE DAY SUFFIX THE PAGE PROMISES IS PARSED, AND ONLY THAT ONE SHAPE.
// Measured defect this pins (2026-10-30): the page said `--older-than 336d`,
// time.ParseDuration has no day unit, so `--older-than 8d` exited 2 and printed
// the whole help page. "8" and "8d" must name ONE window; "8days" must not be
// mistaken for it.
func TestGCDurationAcceptsTheDaySuffix(t *testing.T) {
	accept := []struct {
		raw  string
		want time.Duration
	}{
		{"8", 8 * sessionGCDay},
		{"8d", 8 * sessionGCDay},   // the defect
		{" 8d ", 8 * sessionGCDay}, // TrimSpace still applies
		{"192h", 192 * time.Hour},  // a Go duration, unchanged
		{"336d", 336 * sessionGCDay},
		{"336h", 336 * time.Hour},
		{"1d", sessionGCDay},
	}
	for _, c := range accept {
		got, err := parseGCDuration(c.raw)
		if err != nil {
			t.Fatalf("parseGCDuration(%q) refused a value the page accepts: %v", c.raw, err)
		}
		if got != c.want {
			t.Fatalf("parseGCDuration(%q) = %s, want %s", c.raw, got, c.want)
		}
	}
	// THE ACCEPTANCE ITSELF: the two spellings are one window, not two.
	asDays, err := parseGCDuration("8d")
	if err != nil {
		t.Fatalf("8d: %v", err)
	}
	asCount, err := parseGCDuration("8")
	if err != nil {
		t.Fatalf("8: %v", err)
	}
	if asDays != asCount {
		t.Fatalf("8d = %s but 8 = %s — the two spellings must name ONE window", asDays, asCount)
	}
}

// ③c THE REFUSALS STAY REFUSALS — a fix that widens the door is worse than the
// defect: a zero, a negative, a longer word and an empty value are all usage
// errors (exit 2), and a bare suffix with no count is one too.
func TestGCDurationRefusesWhatItShould(t *testing.T) {
	for _, bad := range []string{"0", "-1", "0d", "-1d", "8days", "8D", "8dd", "d", "d8", "", "   ", "soon"} {
		win, err := parseGCDuration(bad)
		if err == nil {
			t.Fatalf("parseGCDuration(%q) accepted it as %s, want a refusal", bad, win)
		}
		if win != 0 {
			t.Fatalf("parseGCDuration(%q) returned %s beside an error, want the zero duration", bad, win)
		}
		// The surface an operator actually meets: the argv parser turns the
		// refusal into exit 2 (the usage page), never into a defaulted window.
		if _, code := parseSessionGCArgs([]string{sessionGCOlderThanFlag, bad}); code != 2 {
			t.Fatalf("--older-than %q exited %d, want the usage refusal 2", bad, code)
		}
	}
	// And the day-suffixed spellings reach the same window through the argv
	// parser, not only through parseGCDuration.
	for _, ok := range []string{"8", "8d"} {
		opts, code := parseSessionGCArgs([]string{sessionGCOlderThanFlag, ok})
		if code != -1 {
			t.Fatalf("--older-than %q exited %d, want -1 (continue)", ok, code)
		}
		if opts.olderThan != 8*sessionGCDay {
			t.Fatalf("--older-than %q gave window %s, want %s", ok, opts.olderThan, 8*sessionGCDay)
		}
	}
}

// ③d THE PAGE AND THE PARSER AGREE about the unit the page prints: a rendered
// window must be a spelling the parser accepts (the defect was exactly a page
// advertising a spelling the parser rejected).
func TestGCDaysRenderingRoundTrips(t *testing.T) {
	for _, d := range []time.Duration{sessionGCDay, 8 * sessionGCDay, scratch.SessionsGCStaleAge, 336 * sessionGCDay} {
		rendered := formatGCDays(d)
		back, err := parseGCDuration(rendered)
		if err != nil {
			t.Fatalf("the page renders %s for %s and the parser refuses it: %v", rendered, d, err)
		}
		if back != d {
			t.Fatalf("%s rendered as %q parses back to %s, want %s", d, rendered, back, d)
		}
	}
}

// ④ THE VERB IS IN THE FAMILY'S VOCABULARY — the property that reaches the
// dispatcher, the completion set and the family page from ONE definition
// (Rule 13: one definition, referenced).
func TestSessionGCIsInTheFamilyVocabulary(t *testing.T) {
	found := false
	for _, v := range sessionSubcommandNames {
		if v == sessionSubcmdGC {
			found = true
		}
	}
	if !found {
		t.Fatalf("%q is not in sessionSubcommandNames — the dispatcher, completion and the family page all read that slice",
			sessionSubcmdGC)
	}
	// The family page must name it, or the verb exists unadvertised.
	if !strings.Contains(sessionUsage, "gopi session "+sessionSubcmdGC) {
		t.Fatalf("the family usage page does not name `gopi session %s`", sessionSubcmdGC)
	}
}

// ⑤ THE JSON DOCUMENT CARRIES THE SAME VERDICT AS THE TEXT RENDER.
//
// A script and a human must not be able to disagree about what would be
// collected: the two renders read the SAME plan slice, and this pins that the
// JSON's count matches the plan's own.
func TestSessionGCJSONMatchesThePlan(t *testing.T) {
	plans := []scratch.SessionsPlan{
		{Path: "/x/" + gcFixtureUUID, Name: gcFixtureUUID, Age: 100 * time.Hour, Bytes: 2048, Keep: false,
			Reason: "aged"},
		{Path: "/x/fresh", Name: "fresh", Age: time.Hour, Bytes: 10, Keep: true,
			Reason: scratch.SessionsKeepFresh},
	}
	out, err := sessionGCJSON("/x", scratch.SessionsGCStaleAge, plans, 0, 0, true)
	if err != nil {
		t.Fatalf("render json: %v", err)
	}
	for _, want := range []string{`"collectable": 1`, `"bytes": 2048`, `"keep": false`, `"keep": true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("the JSON document is missing %s:\n%s", want, out)
		}
	}
	// The TEXT render must agree.
	text := sessionGCText("/x", scratch.SessionsGCStaleAge, plans, true, 0, 0)
	if !strings.Contains(text, "collectable=1") {
		t.Fatalf("the text render disagrees with the plan:\n%s", text)
	}
	if !strings.Contains(text, "NOTHING was deleted") {
		t.Fatalf("a dry run's render does not say nothing was deleted:\n%s", text)
	}
}

// ⑥ THE PAGE'S FIRST LINE IS THE FINGERPRINT the help contract prints, and the
// page names every flag the parser accepts.
func TestSessionGCUsageNamesItsFlags(t *testing.T) {
	first := strings.SplitN(sessionGCUsage, "\n", 2)[0]
	if first != sessionGCUsageFingerprint {
		t.Fatalf("the page's first line is %q, want the fingerprint %q", first, sessionGCUsageFingerprint)
	}
	for _, f := range []string{sessionGCApplyFlag, sessionGCDryRunFlag, sessionGCOlderThanFlag, sessionGCJSONFlag} {
		if !strings.Contains(sessionGCUsage, f) {
			t.Fatalf("the page does not name %s", f)
		}
	}
}

// ⑦ THE FOUR KEEP REASONS ARE DISTINCT CONSTANTS. A reason typed inline twice
// drifts into a reason nobody can grep for (Rule 1b), and a summary that folded
// two causes into one sentence would hide which predicate is holding the tree.
func TestSessionGCKeepReasonsAreDistinct(t *testing.T) {
	reasons := []string{
		scratch.SessionsKeepNotOurs,
		scratch.SessionsKeepFresh,
		scratch.SessionsKeepHeld,
		scratch.SessionsKeepHolderRecord,
		scratch.SessionsKeepLedgerRow,
		scratch.SessionsKeepSelfCwd,
		scratch.SessionsKeepUnmeasurable,
	}
	seen := map[string]bool{}
	for _, r := range reasons {
		if r == "" {
			t.Fatalf("a keep reason is empty")
		}
		if seen[r] {
			t.Fatalf("two keep reasons share the text %q", r)
		}
		seen[r] = true
	}
	if reflect.ValueOf(reasons).Len() != 7 {
		t.Fatalf("the reason set changed size")
	}
}
