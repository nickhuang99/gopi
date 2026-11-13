package main

// cli_memory_ledger_guard_test.go — the regression guard for the "a typo'd --db
// reads as an empty knowledge base, and a READ creates a 128 KiB file" defect
// (measured 2026-11-06, epic 01a1ac96).
//
// THREE ARMS, and all three are needed:
//
//	read-only refusal — every READ-ONLY subcommand given an explicit --db that
//	  does not exist must exit non-zero, name BOTH the path it was given and the
//	  ledger that is active, and leave no file behind (os.Stat, not a shell
//	  `test -f` whose exit code nobody read).
//	writer control   — a WRITING subcommand given the same kind of path must
//	  still succeed AND say it created the ledger, so "empty" and "brand new"
//	  are different sentences (Rule 15).
//	positive control — the same read-only verbs against a REAL ledger must still
//	  answer normally. Without this arm a "fix" that refuses every --db would
//	  pass the first two, which is exactly how a strictness change breaks the
//	  ordinary use it was meant to protect.
//
// The whole table runs against t.TempDir() paths under testutil.MockEnv's own
// GOPI_HOME, so no arm can reach the operator's live profile (Rule 32).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// memoryLedgerProbeMockURL is a closed loopback port: none of these verbs makes
// a model call, and a run that somehow tried fails fast and visibly.
const memoryLedgerProbeMockURL = "http://127.0.0.1:1/"

// memoryLedgerProbeIDPlaceholder stands in for a row id the reverse arm has to
// read off the seeded ledger (see the `get` row).
const memoryLedgerProbeIDPlaceholder = "<seeded-row-id>"

// generatedProbeRowID is the id the REFUSAL arm uses: a positive integer that
// parses, so the run reaches the ledger guard instead of stopping at argv
// validation.
const generatedProbeRowID = "5"

// withRowID substitutes the placeholder id in one case's argv.
func withRowID(argv []string, id string) []string {
	out := append([]string{}, argv...)
	for i, a := range out {
		if a == memoryLedgerProbeIDPlaceholder {
			out[i] = id
		}
	}
	return out
}

// memoryLedgerGuardCase is one row of the read-only table.
type memoryLedgerGuardCase struct {
	// name is the sub-verb spelling in the failure message.
	name string
	// argv is the full argument list after `gopi`, WITHOUT --db (the test
	// appends "--db <missing path>" so every row names the same shape).
	argv []string
}

// memoryLedgerReadOnlyCases is EVERY read-only dispatch this guard covers. A
// new read-only sub-verb belongs here; a reviewer can see the family in one
// place.
func memoryLedgerReadOnlyCases() []memoryLedgerGuardCase {
	return []memoryLedgerGuardCase{
		{name: "search", argv: []string{CliVerbMemory, "search", "ledger-guard-probe"}},
		// memoryLedgerProbeIDPlaceholder is substituted with a REAL row id in the
		// reverse arm: `get` on an id that is not in the ledger fails for its own
		// (correct) reason, which would mask the property that arm measures.
		{name: "get", argv: []string{CliVerbMemory, "get", memoryLedgerProbeIDPlaceholder}},
		{name: "list", argv: []string{CliVerbMemory, "list", "--limit", "2"}},
		{name: "provenance", argv: []string{CliVerbMemory, memoryProvenanceVerb}},
		{name: "provenance --counts", argv: []string{CliVerbMemory, memoryProvenanceVerb, "--" + memoryProvenanceCountsMode}},
		{name: "feedback --list", argv: []string{CliVerbMemory, "feedback", "--list"}},
		{name: "feedback --suggest-cold", argv: []string{CliVerbMemory, "feedback", "--suggest-cold"}},
		{name: "feedback --recompute", argv: []string{CliVerbMemory, "feedback", "--recompute"}},
		{name: "converge-keywords --dry-run",
			argv: []string{CliVerbMemory, memoryConvergeKeywordsVerb, "--dry-run"}},
		{name: "backfill-tokens --dry-run",
			argv: []string{CliVerbMemory, memoryBackfillVerb, "--dry-run"}},
	}
}

// TestMemoryReadOnlyRefusesAbsentExplicitDB is the core arm: the CLI must treat
// an explicitly named, absent --db as an ERROR, and must not create it.
func TestMemoryReadOnlyRefusesAbsentExplicitDB(t *testing.T) {
	home := t.TempDir()
	env := testutil.MockEnv(t, home, memoryLedgerProbeMockURL)
	work := t.TempDir()
	activeLedger := filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName)

	for _, tc := range memoryLedgerReadOnlyCases() {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh directory per case so "the file was not created" cannot be
			// confused with "another case created it".
			dir := t.TempDir()
			missing := filepath.Join(dir, "typo.db")
			// The placeholder row id is filled with an integer: the refusal must be
			// the GUARD's, not an argv-validation error that happens to be
			// non-zero too (that would be a green on the wrong sentence).
			argv := append(withRowID(tc.argv, generatedProbeRowID), config.LedgerSourceFlag, missing)

			stdout, stderr, code := testutil.RunGopi(t, argv, "", env, work)

			if code == 0 {
				t.Fatalf("gopi %s exited 0 on a --db that does not exist — that is the defect: an absent path must not read as an empty knowledge base.\nstdout:\n%s\nstderr:\n%s",
					tc.name, stdout, stderr)
			}
			// The message must name the path the user WROTE and the ledger that
			// IS active; a generic "no such file" answers neither question.
			if !strings.Contains(stderr, missing) {
				t.Errorf("the refusal never names the path it was given (%s):\n%s", missing, stderr)
			}
			if !strings.Contains(stderr, activeLedger) {
				t.Errorf("the refusal never names the ACTIVE ledger (%s):\n%s", activeLedger, stderr)
			}
			// THE FILE ITSELF — os.Stat, so "not created" is a measurement and
			// not an inference from the exit code.
			if _, err := os.Stat(missing); err == nil {
				t.Fatalf("a READ-ONLY verb created %s — a read must leave the filesystem alone", missing)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat %s: %v", missing, err)
			}
		})
	}
}

// TestMemoryWriterCreatesAndSaysSo is the writer half of "empty != newly
// created": creating a ledger stays allowed, and the run must SAY it created
// one.
func TestMemoryWriterCreatesAndSaysSo(t *testing.T) {
	home := t.TempDir()
	env := testutil.MockEnv(t, home, memoryLedgerProbeMockURL)
	work := t.TempDir()
	fresh := filepath.Join(t.TempDir(), "brand-new.db")

	_, stderr, code := testutil.RunGopi(t, []string{
		CliVerbMemory, "store",
		"--topic", "ledger guard probe",
		"--question", "does a writer still create an explicitly named ledger?",
		"--answer", "it does, and the run says so instead of leaving empty and new looking alike",
		"--keywords", "ledger-guard,writer-control",
		config.LedgerSourceFlag, fresh,
	}, "", env, work)
	if code != 0 {
		t.Fatalf("a WRITER must keep the ability to create an explicitly named ledger; exit=%d\n%s", code, stderr)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("the writer did not create %s: %v", fresh, err)
	}
	if !strings.Contains(stderr, fresh) || !strings.Contains(stderr, "created a new knowledge base") {
		t.Fatalf("the writer created a ledger without saying so — 'empty' and 'newly created' must not be the same shape.\nstderr:\n%s", stderr)
	}

	// A second write onto the SAME file must NOT claim it created it again.
	_, stderr2, code2 := testutil.RunGopi(t, []string{
		CliVerbMemory, "store",
		"--topic", "ledger guard probe 2",
		"--question", "does the notice repeat on an existing ledger?",
		"--answer", "no — the notice is a one-time statement about creation",
		"--keywords", "ledger-guard,notice-once",
		config.LedgerSourceFlag, fresh,
	}, "", env, work)
	if code2 != 0 {
		t.Fatalf("second store failed: exit=%d\n%s", code2, stderr2)
	}
	if strings.Contains(stderr2, "created a new knowledge base") {
		t.Fatalf("the notice repeated on a ledger that already existed — it must be a statement about THIS run:\n%s", stderr2)
	}
}

// TestMemoryReadOnlyAcceptsExistingExplicitDB is the reverse arm: a --db that
// really exists keeps working, unchanged. Without it a guard that refuses every
// --db would look green.
func TestMemoryReadOnlyAcceptsExistingExplicitDB(t *testing.T) {
	home := t.TempDir()
	env := testutil.MockEnv(t, home, memoryLedgerProbeMockURL)
	work := t.TempDir()
	existing := filepath.Join(t.TempDir(), "real.db")

	// Materialise it with a WRITER verb (the only legal way for a test to create
	// a ledger), then read it back with each read-only verb.
	seedOut, stderr, code := testutil.RunGopi(t, []string{
		CliVerbMemory, "store",
		"--topic", "reverse arm",
		"--question", "does a real ledger still answer its read-only readers?",
		"--answer", "yes — the guard is about ABSENT paths, not about --db",
		"--keywords", "ledger-guard,reverse-arm",
		config.LedgerSourceFlag, existing,
	}, "", env, work)
	if code != 0 {
		t.Fatalf("seeding the fixture ledger failed: exit=%d\n%s", code, stderr)
	}
	seededID := memoryLedgerStoredID(t, seedOut)

	for _, tc := range memoryLedgerReadOnlyCases() {
		t.Run(tc.name, func(t *testing.T) {
			argv := append(withRowID(tc.argv, seededID), config.LedgerSourceFlag, existing)
			stdout, stderr, code := testutil.RunGopi(t, argv, "", env, work)
			if code != 0 {
				t.Fatalf("a read-only verb refused an EXISTING --db: exit=%d\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if strings.Contains(stderr, "will not create that file") {
				t.Fatalf("the refusal fired on a file that exists:\n%s", stderr)
			}
		})
	}
}

// memoryLedgerStoredID reads the row id out of `gopi memory store`'s own
// confirmation line ("Memory stored (id=N).") — the ONE channel a CLI user has
// to learn what was written, so the test uses the same one.
func memoryLedgerStoredID(t *testing.T, storeStdout string) string {
	t.Helper()
	const prefix, suffix = "Memory stored (id=", ")."
	i := strings.Index(storeStdout, prefix)
	if i < 0 {
		t.Fatalf("the writer's confirmation carries no row id — the reverse arm cannot address the seeded row:\n%s", storeStdout)
	}
	rest := storeStdout[i+len(prefix):]
	j := strings.Index(rest, suffix)
	if j < 0 {
		t.Fatalf("malformed store confirmation: %q", storeStdout)
	}
	return rest[:j]
}

// TestMemoryCaptureStatsRefusesDBAndCreatesNothing covers the one read-only
// sub-verb that is NOT in the shared refusal table: `capture-stats` opens no
// ledger at all (it reads session logs), so it refuses --db through its OWN
// parser. The property that matters is identical — non-zero exit, no file — and
// it is asserted here so a future refactor cannot quietly route it through the
// ledger opener and reintroduce the creation.
func TestMemoryCaptureStatsRefusesDBAndCreatesNothing(t *testing.T) {
	home := t.TempDir()
	env := testutil.MockEnv(t, home, memoryLedgerProbeMockURL)
	missing := filepath.Join(t.TempDir(), "typo.db")

	stdout, stderr, code := testutil.RunGopi(t, []string{
		CliVerbMemory, memoryCaptureStatsVerb, config.LedgerSourceFlag, missing,
	}, "", env, t.TempDir())
	if code == 0 {
		t.Fatalf("capture-stats accepted --db and exited 0 — it opens no ledger, so it must refuse the flag.\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("capture-stats created %s", missing)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", missing, err)
	}
}

// TestMemoryLedgerGuardUsesTheGroupPageVocabulary pins the contract to the page
// a user reads: the refusal the guard prints and the paragraph the `gopi memory`
// help carries are the SAME promise, so a later edit to one cannot silently
// leave the other describing a different mechanism (Rule 13: one spelling).
func TestMemoryLedgerGuardUsesTheGroupPageVocabulary(t *testing.T) {
	for _, want := range []string{
		"--db PATH",
		"IS NOT AN EMPTY KNOWLEDGE BASE",
		"created a new knowledge base at",
	} {
		if !strings.Contains(memoryUsage, want) {
			t.Errorf("the group page never says %q — the contract the guard enforces must be readable before it fires", want)
		}
	}
}
