// cli_memory_capture_stats_help_test.go — `gopi memory capture-stats` answers
// `-h` with ITS OWN page, and an unknown argument is still refused by name.
//
// THE MEASURED DEFECT THIS FILE PINS (2026-10-30): the sub-verb exited 1 with
// `unknown argument "-h"` and printed NOTHING, because parseMemoryCaptureStats
// saw the help spelling before any help early-exit — while its four siblings in
// the same group (runMemorySubcommand, runMemorySync, runMemoryBackup) printed a
// page and exited 0. The cost was not only ergonomic: cmd/agent/
// man_source_gate_test.go's flag vocabulary (manFlagVocabulary →
// mansource.HelpSubcommands → subcommandPage) treats a NON-ZERO `-h` as "this
// sub-verb has no page of its own", so capture-stats' flags dropped out of the
// vocabulary and a TRUE man sentence about --root/--json would have been refused
// as an invented flag.
//
// EVERY CASE DRIVES THE REAL BINARY (Rule 31): the page is what `gopi memory
// capture-stats -h` prints, never what the constant says, because a constant
// nothing routes to is not a page. The sandbox/env driver is the family's own
// (g3RunProbe, cli_memory_help_surface_basic_test.go) — one build per suite, a
// scrubbed environment and a per-probe timeout, so this file adds no fixture.
package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/mansource"
)

// TestMemoryCaptureStatsHelpIsItsOwnPage pins the PAGE: `-h` exits 0, prints
// something, carries the verb's own head — and is NOT the group page, because
// "a page of its own" is exactly what the man-source reader compares for.
func TestMemoryCaptureStatsHelpIsItsOwnPage(t *testing.T) {
	out, code := g3RunProbe(t, t.TempDir(), nil, CliVerbMemory, memoryCaptureStatsVerb, HelpFlag)
	if code != 0 {
		t.Fatalf("`gopi memory %s %s` must print the page and exit 0 (measured defect: it exited 1 with `unknown argument %q`):\n%s",
			memoryCaptureStatsVerb, HelpFlag, HelpFlag, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("`gopi memory %s %s` printed nothing — a help request answered by silence", memoryCaptureStatsVerb, HelpFlag)
	}
	if !strings.Contains(out, memoryCaptureStatsUsageHead) {
		t.Errorf("the page must carry the verb's own head %q:\n%s", memoryCaptureStatsUsageHead, out)
	}
	// THE HEAD MUST BE THE FIRST LINE. A page that printed a banner and then the
	// group's usage above it would still "contain" the head, and the operator's
	// acceptance sentence — `-h` prints its page and the first line is the title
	// — is about the page as it is READ, not as it is searched.
	if first, _, _ := strings.Cut(strings.TrimLeft(out, "\n"), "\n"); strings.TrimRight(first, " \t\r") != memoryCaptureStatsUsageHead {
		t.Errorf("the FIRST line of `gopi memory %s %s` must be the page title\n want: %q\n got:  %q\n%s",
			memoryCaptureStatsVerb, HelpFlag, memoryCaptureStatsUsageHead, first, out)
	}
	// A PAGE PLUS A REFUSAL LINE, ON STDOUT, AT EXIT 0 — the shape neither of the
	// two checks above can see. Measured reason for this arm: the drive returns
	// CombinedOutput, so a run that printed the page AND wrote the parser's
	// refusal exits 0 while every assertion so far looks green; that is exactly
	// the defect this file exists for (`unknown argument "-h"`), reached by a
	// different order of operations. So the refusal wording is refused HERE, in
	// the page's own output, by the constant the parser writes it with.
	if strings.Contains(out, memoryCaptureStatsUnknownArgPrefix) {
		t.Errorf("the help page must not carry the parser's refusal (%q) — page + refusal line at exit 0 is the exact shape of the measured defect (Rule 15: answer the page INSTEAD of parsing it):\n%s",
			memoryCaptureStatsUnknownArgPrefix, out)
	}
	// The comparison the man-source reader makes: verbatim group page = no page.
	if strings.Contains(out, "GoPi memory — the memory tool family as one subcommand group.") {
		t.Errorf("`gopi memory %s -h` printed the GROUP page — subcommandPage() would read that as \"no page of its own\" and drop --%s/--%s from the man-flag vocabulary:\n%s",
			memoryCaptureStatsVerb, memoryCaptureStatsRootFlag, memoryCaptureStatsJSONFlag, out)
	}
	if out == memoryUsage {
		t.Errorf("the sub-verb's page is the group page byte for byte — it must be a page of its own:\n%s", out)
	}

	// THE VOCABULARY ITSELF. The prose above explains the consequence; this
	// asserts it, through the SAME reader the man gate uses, so a page that
	// printed its flags as a sentence rather than as rows would fail here.
	flags := map[string]bool{}
	for _, f := range mansource.HelpFlagTokens(out) {
		flags[f] = true
	}
	for _, want := range []string{memoryCaptureStatsRootFlag, memoryCaptureStatsJSONFlag} {
		if !flags["--"+want] {
			t.Errorf("--%s is not a DEFINITION ROW on the page, so it never enters the man-flag vocabulary (%v):\n%s",
				want, mansource.HelpFlagTokens(out), out)
		}
	}
	// And the synopsis the group page carries is reused here, not re-typed: a
	// reader who learns the invocation on either page types the same argv.
	if !strings.Contains(out, memoryCaptureStatsUsageLine) {
		t.Errorf("the page must carry the synopsis line the group help advertises (%q):\n%s", memoryCaptureStatsUsageLine, out)
	}
}

// TestMemoryCaptureStatsHelpWinsOverTheFlagParser pins the ORDER: the help
// spelling is answered wherever it stands in argv, even behind flags the parser
// would otherwise consume — and the long spelling too (both are the package's
// own constants, never a re-typed literal).
func TestMemoryCaptureStatsHelpWinsOverTheFlagParser(t *testing.T) {
	dir := t.TempDir()
	for _, argv := range [][]string{
		{CliVerbMemory, memoryCaptureStatsVerb, "--" + memoryCaptureStatsRootFlag, dir, "--" + memoryCaptureStatsJSONFlag, HelpFlag},
		{CliVerbMemory, memoryCaptureStatsVerb, HelpFlagLong},
		{CliVerbMemory, memoryCaptureStatsVerb, "--" + memoryCaptureStatsRootFlag + "=" + filepath.Join(dir, "absent"), HelpFlagLong},
	} {
		out, code := g3RunProbe(t, t.TempDir(), nil, argv...)
		if code != 0 {
			t.Errorf("`gopi %s` must be the PAGE, exit 0 (got %d):\n%s", strings.Join(argv, " "), code, out)
			continue
		}
		if !strings.Contains(out, memoryCaptureStatsUsageHead) {
			t.Errorf("`gopi %s` did not print the page:\n%s", strings.Join(argv, " "), out)
		}
	}
}

// TestMemoryCaptureStatsUnknownArgumentIsStillRefused is the other half of Rule
// 15: only the HELP spellings changed route. A genuinely unknown argument is
// still refused BY NAME, with exit 1 — a typo must never be read as "the default
// corpus", because that would survey the wrong corpus and still print a table.
func TestMemoryCaptureStatsUnknownArgumentIsStillRefused(t *testing.T) {
	dir := t.TempDir()
	out, code := g3RunProbe(t, t.TempDir(), nil, CliVerbMemory, memoryCaptureStatsVerb, "--"+memoryCaptureStatsRootFlag+"s", dir)
	if code == 0 {
		t.Fatalf("an unknown flag must exit 1, not run the survey:\n%s", out)
	}
	if !strings.Contains(out, memoryCaptureStatsRootFlag+"s") {
		t.Errorf("the refusal must name the argument it does not know (%q):\n%s", memoryCaptureStatsRootFlag+"s", out)
	}
	if !strings.Contains(out, memoryCaptureStatsUnknownArgPrefix) {
		t.Errorf("the refusal must say what went wrong:\n%s", out)
	}
	// A missing value is the parser's other refusal, unchanged by this work.
	out, code = g3RunProbe(t, t.TempDir(), nil, CliVerbMemory, memoryCaptureStatsVerb, "--"+memoryCaptureStatsRootFlag)
	if code == 0 {
		t.Fatalf("--%s without a directory must be refused:\n%s", memoryCaptureStatsRootFlag, out)
	}
	if !strings.Contains(out, "needs a directory") {
		t.Errorf("the refusal must name what is missing:\n%s", out)
	}
}
