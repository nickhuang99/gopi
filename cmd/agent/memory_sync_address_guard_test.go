package main

// memory_sync_address_guard_test.go — the guard against a retirement report (or
// any other production comment) pointing a reader at a DELETED command.
//
// THE DEFECT THIS FILE EXISTS FOR (measured 2026-11-06, task 01a1ad90-40d0).
// Commit 5677cc6e deleted the cross-node sync CLI family, and with it
// `gopi memory sync`. Three RUNTIME retirement reports kept teaching it as the
// replacement:
//
//	cmd/agent/memory_sync_on_store_retired.go  memorySyncOnStoreRetiredReplacement
//	cmd/agent/memory_sync_tick_retired.go      memorySyncTickRetiredCause
//	internal/config/config.go                  MemorySyncTickRetiredReplacement
//
// The first two of those files NO LONGER EXIST (deleted 2026-11-06, task
// 01a1b003-0869): the per-knob report functions were replaced by ONE registry
// read by `gopi config show` (internal/config/retired_settings_keys.go), so each
// retired knob's prose now lives in a table ROW (Purpose / Retired / Replacement)
// instead of in a function that printed on every start. The property this file
// asserts is unchanged, and the runtime arm below judges the registry's
// Replacement columns — so the second dead end is refused wherever the sentence is
// kept. A reader who wants one of those two old constants should look for its row
// instead: `grep -n "MemorySyncTickRetiredSettingsKey"
// internal/config/retired_settings_keys.go`.
//
// Measured on that build, `gopi memory sync --peer zpeer --push` printed
// `gopi memory: unknown subcommand "sync"` and exited 1, so an operator who
// followed the retirement report's own advice hit a second dead end and paid a
// second round trip to learn it. `internal/cliverbs`' MemorySyncVerb + its three
// flags were still exported with no reader in the tree at all — a shared value
// advertising a spelling nothing dispatches.
//
// WHY A GATE AND NOT JUST A FIX. `merge` is the precedent: its verb was removed,
// the constant lingered, and the address kept reappearing in prose because
// nothing measured it. A rule in a document cannot see the next paragraph
// somebody writes; this can. The subject is ONE property a machine can read: an
// address that appears in production Go must be introduced by a retirement
// marker, so no reader can copy a command that cannot run.
//
// THE TOLERANCE IS PARAGRAPH-SCOPED AND MARKER-BEFORE, deliberately:
//
//	// the since-DELETED `gopi memory sync --peer P --push` held the round   OK
//	// run `gopi memory sync --peer P --push` by hand                        RED
//	// `gopi memory sync` was DELETED                                        RED (marker AFTER)
//
// The marker must come BEFORE the address because a note below the invocation is
// no note to the reader who already pasted the command — the same shape
// PROJECT.md Rule 36 records for a retired mechanism's annotation. Aggregating by
// PARAGRAPH (not by line) is what makes a multi-line comment judgeable: the
// marker and the address are routinely on different lines.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// deletedMemorySyncAddress is the DELETED spelling this gate judges. It is a
// literal ON PURPOSE and must stay one: it is the subject of the measurement, so
// promoting it to a production constant would let the day the address changes
// leave the gate green over a tree that teaches the new spelling
// (memory-recorded lesson, task 01a1945b — a guard whose target is a shared
// constant cannot see a rename).
//
// NARROWED 2026-11-06, and the narrowing is the whole point of keeping it a
// literal: the VERB came back (`gopi memory sync`, manual door, one session per
// invocation), so the bare address `gopi memory sync --peer P --push` is a
// command that RUNS — a gate still judging it would go red on correct prose and
// get suppressed. What stays deleted is the PERIODIC face: `--every <dur>`
// dispatched a loop that re-ran the session on a cadence. The judged address is
// therefore the flag, not the verb.
const deletedMemorySyncAddress = "gopi memory sync --every"

// retiredAddressMarkers are the words that turn a mention of the address into a
// STATEMENT ABOUT ITS REMOVAL rather than an instruction. Each is required to
// appear BEFORE the address inside the same paragraph. Lowercase forms are
// included because ordinary English prose uses them ("was deleted", "the
// since-removed …"); the assertion is about position, not case.
var retiredAddressMarkers = []string{"DELETED", "RETIRED", "deleted", "removed", "retired"}

// "removed" and "retired" are listed in both cases on purpose: ordinary English
// prose uses them lowercase ("the retired `…`", "a removed verb"), and the
// assertion is about POSITION, not about shouting. Capitalised-only was measured
// to report three legitimate comments in the gate's own vocabulary file — prose
// that already said "retired" a few words before the address.

// productionGoFilesUnder returns every non-test .go file under root, walking the
// tree so an UNTRACKED file is scanned too: a detector that reads only the index
// skips the file being written, which is exactly the state a new comment is in
// when its author is about to check it (measured as a real hole in
// internal/testutil's Rule42 gate, 2026-10-24).
func productionGoFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == "bin" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

// commentParagraphs groups a Go file's COMMENT lines into paragraphs: a run of
// comment lines is one paragraph and ends at a line of code (including the
// `package` / `import` / `func` lines around it) or at a blank source line.
//
// A line that is only `//` also ends the paragraph — an empty comment line is how
// this repo separates one idea from the next inside a single block comment, so
// treating it as part of the same paragraph would let a marker written for the
// paragraph ABOVE silence a paragraph BELOW.
//
// String literals are NOT comments and are not scanned by this half; the runtime
// sentences are covered by TestRetirementReportsNameNoDeletedVerb, which reads
// them from the constants themselves rather than from their source text.
func commentParagraphs(src string) []string {
	var paragraphs []string
	var buf []string
	flush := func() {
		if len(buf) > 0 {
			paragraphs = append(paragraphs, strings.Join(buf, "\n"))
			buf = nil
		}
	}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			flush()
			continue
		}
		if trimmed == "//" || trimmed == "// " {
			flush()
			continue
		}
		buf = append(buf, trimmed)
	}
	flush()
	return paragraphs
}

// paragraphHasUnmarkedAddress reports whether ANY occurrence of the address in
// the paragraph has no retirement marker before it. Returning the offending
// paragraph rather than a bool keeps the failure message copy-pasteable.
func paragraphHasUnmarkedAddress(paragraph string) bool {
	offset := 0
	for {
		idx := strings.Index(paragraph[offset:], deletedMemorySyncAddress)
		if idx < 0 {
			return false
		}
		abs := offset + idx
		before := paragraph[:abs]
		marked := false
		for _, marker := range retiredAddressMarkers {
			if strings.Contains(before, marker) {
				marked = true
				break
			}
		}
		if !marked {
			return true
		}
		offset = abs + len(deletedMemorySyncAddress)
	}
}

// TestProductionProseNamesTheDeletedMemorySyncVerbOnlyInThePastTense is the
// real-tree arm: no production Go comment may introduce the deleted address
// without a retirement marker before it.
//
// It is non-vacuous by construction: it requires a floor on the files walked and
// on the paragraphs that DO mention the address, so a walk that read nothing, or
// a sweep that silently stopped matching, fails instead of passing green.
func TestProductionProseNamesTheDeletedMemorySyncVerbOnlyInThePastTense(t *testing.T) {
	root := repoRoot(t)
	files := productionGoFilesUnder(t, root)
	if len(files) < 500 {
		t.Fatalf("walked only %d production .go file(s) under %s — a sweep that reads nothing cannot pass", len(files), root)
	}

	judged := 0
	var violations []string
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, paragraph := range commentParagraphs(string(raw)) {
			if !strings.Contains(paragraph, deletedMemorySyncAddress) {
				continue
			}
			judged++
			if paragraphHasUnmarkedAddress(paragraph) {
				violations = append(violations, rel+"\n    "+strings.ReplaceAll(paragraph, "\n", "\n    "))
			}
		}
	}

	// FLOOR: this repository mentions the address in several production
	// paragraphs (the two retirement reports, their config constant, the
	// machinery's own header). A sweep that judged none of them has stopped
	// measuring — that is a refusal, not a pass.
	//
	// THE NUMBER CAME DOWN WITH THE ADDRESS (2026-11-06, 5 → 4) and that is a
	// measurement, not a concession: the subject is now the `--every` face
	// alone, and four production paragraphs mention THAT. Measured by
	// `grep -rn "gopi memory sync --every" --include=*.go . | grep -v _test.go`:
	// internal/config/config.go (MemorySyncEveryRetiredReplacement),
	// internal/signalrec/signalrec.go twice (one-shot handler notes),
	// cmd/agent/signal_verdict.go once. If a future narrowing shrinks it
	// further, lower this floor in the same commit WITH the new count written
	// down — a floor left above the population fails loudly, which is the
	// behaviour wanted; a floor dragged below it silently is not.
	const minJudgedParagraphs = 4
	if judged < minJudgedParagraphs {
		t.Fatalf("judged only %d paragraph(s) mentioning %q across %d file(s) — the sweep has stopped finding what it measures",
			judged, deletedMemorySyncAddress, len(files))
	}

	for _, v := range violations {
		t.Errorf("a production comment names the DELETED address %q with no retirement marker before it — an operator who follows it runs a command that exits 1 (`unknown flag --every`). Name it as removed (DELETED / RETIRED / since-removed) BEFORE the address, or point at the surviving one-way copy `gopi memory backup`:\n%s",
			deletedMemorySyncAddress, v)
	}
	t.Logf("judged %d paragraph(s) mentioning %q across %d production .go file(s); %d violation(s)",
		judged, deletedMemorySyncAddress, len(files), len(violations))
}

// TestRetirementReportsNameNoDeletedVerb is the RUNTIME half — the half that
// actually reached operators. Every sentence a retirement PRINTS is now a field
// of the ONE registry (internal/config.RetiredSettingsKeys), so this reads them
// where they live instead of judging their source text, and it requires the
// positive half too: a replacement that names nothing is as useless as one that
// names a dead command.
//
// The floor is part of the assertion: a registry that came back EMPTY would make
// every loop below vacuous, so the row count is checked first.
func TestRetirementReportsNameNoDeletedVerb(t *testing.T) {
	rows := config.RetiredSettingsKeys()
	const minRegistryRows = 5
	if len(rows) < minRegistryRows {
		t.Fatalf("the retired-knob registry carries %d row(s), want at least %d — a registry that lost its rows makes every arm below vacuous", len(rows), minRegistryRows)
	}

	for _, row := range rows {
		// Every row must be QUERYABLE by its own settings key: `gopi config show`
		// reaches a row through RetiredSettingsKeyInfo, and a row it cannot reach
		// is a row nothing reports.
		if row.SettingsKey != "" {
			if got, ok := config.RetiredSettingsKeyInfo(row.SettingsKey); !ok || got.SettingsKey != row.SettingsKey {
				t.Errorf("RetiredSettingsKeyInfo(%q) did not answer with the registry's own row — the key's explanation is unreachable from the verb that prints it", row.SettingsKey)
			}
		}
		for _, text := range []string{row.Purpose, row.Retired, row.Replacement} {
			if strings.TrimSpace(text) == "" {
				t.Errorf("registry row %q/%q carries an EMPTY field — an explanation with a missing half is the startup hook's defect (it never said what the knob did) coming back", row.SettingsKey, row.EnvName)
			}
		}
		// The knob must be NAMEABLE: at least one of its two faces is a spelling.
		if row.SettingsKey == "" && row.EnvName == "" {
			t.Errorf("registry row %+v names neither a settings key nor an env var", row)
		}
		// The replacement text is the one that reached operators, so it is what
		// this arm judges for the deleted address: mentioning it is allowed ONLY
		// when the same sentence marks it DELETED (the address itself is the
		// since-removed `gopi memory sync`, whose surviving door is the one-way
		// copy `gopi memory backup`).
		if strings.Contains(row.Replacement, deletedMemorySyncAddress) && !strings.Contains(row.Replacement, "DELETED") {
			t.Errorf("the replacement for %q names the deleted address %q without marking it DELETED — a replacement sentence that hands an operator a command which exits 1 is worse than none:\n%s",
				row.SettingsKey+row.EnvName, deletedMemorySyncAddress, row.Replacement)
		}
	}

	// The surviving cross-host door must be NAMED by every replacement whose knob
	// belonged to the CROSS-NODE MEMORY family, or the report tells an operator
	// what is gone and nothing about what is left.
	//
	// The SET is named rather than counted over the whole registry on purpose: a
	// knob whose replacement is a DIFFERENT door (the merge tick's `gopi task
	// merge`, the pull switch's `gopi task submit`) must not be dragged into this
	// one, and an arm that demanded every row name it would force exactly that.
	memoryFamily := map[string]bool{
		config.MemorySyncTickRetiredSettingsKey: true,
		config.MemorySyncOnStoreSettingsKey:     true,
	}
	named := 0
	for _, row := range rows {
		if !memoryFamily[row.SettingsKey] {
			continue
		}
		if !strings.Contains(row.Replacement, "gopi memory backup") {
			t.Errorf("the replacement for %q does not name the surviving cross-host door `gopi memory backup` — the retirement report would leave an operator with nothing to run:\n%s",
				row.SettingsKey, row.Replacement)
		}
		named++
	}
	if named != len(memoryFamily) {
		t.Errorf("judged %d of the %d cross-node memory row(s) the registry is expected to carry — a registry that dropped one makes this arm vacuous, and an arm that judged nothing must never read green",
			named, len(memoryFamily))
	}
}

// TestDeletedMemorySyncAddressGuardRedProof plants the two shapes the defect
// took and requires the detector to REFUSE them — plus the two clean shapes it
// must ACCEPT, because a gate that is red on everything is not a gate.
func TestDeletedMemorySyncAddressGuardRedProof(t *testing.T) {
	redFixtures := []struct {
		name      string
		paragraph string
	}{
		{
			name:      "an instruction to run the deleted verb",
			paragraph: "// cross-node propagation on a timer: run `gopi memory sync --every 5m --peer P --push` by hand",
		},
		{
			name:      "a claim the verb still opens a session",
			paragraph: "// `gopi memory sync --every 5m --peer P` is still one bidirectional\n// session on a cadence",
		},
		{
			name:      "a marker placed AFTER the address",
			paragraph: "// the `gopi memory sync --every 5m` loop was DELETED",
		},
	}
	for _, f := range redFixtures {
		if !paragraphHasUnmarkedAddress(f.paragraph) {
			t.Errorf("red proof FAILED — the detector accepted %q; the guard would go green over the exact sentence that sent operators to a dead command",
				f.name)
		}
	}

	greenFixtures := []struct {
		name      string
		paragraph string
	}{
		{
			name:      "the shape this tree actually uses",
			paragraph: "// the since-DELETED `gopi memory sync --every 5m --peer P --push` held the round for 315-534s",
		},
		{
			name:      "a marker on an earlier line of the same paragraph",
			paragraph: "// the child's loop was the since-DELETED\n// `gopi memory sync --every 5m --peer P --push`, removed by 5677cc6e",
		},
		{
			name:      "no mention at all",
			paragraph: "// the surviving door is `gopi memory backup`",
		},
	}
	for _, f := range greenFixtures {
		if paragraphHasUnmarkedAddress(f.paragraph) {
			t.Errorf("green proof FAILED — the detector refused the CLEAN shape %q; a gate red on correct prose gets suppressed, and a suppressed gate protects nothing",
				f.name)
		}
	}
}
