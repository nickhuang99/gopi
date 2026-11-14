package standardgate

// retired_pipeline_reference_gate_test.go — Rule 36's retired-surface gate: the
// check that keeps a REMOVED capability out of the corpus.
//
// THE DEFECT IT ANSWERS. The document pipeline is gone (e6bfa10 cut the verb and
// internal/docindex, 29882b3 dropped its gates, `tools/docclass/` went with
// them). The sweep that stopped the corpus teaching it was a ONE-OFF: 82
// documents were annotated by hand and nothing ever checked the annotation
// again, so a normative page can still tell a reader to run
// `python3 tools/docclass/classify.py` — and one did, six times, on the tree this
// gate was written against. Rule 36 makes classification a promise "in the SAME
// commit"; this file is the part of that promise a machine can keep.
//
// HOW IT IS PROVEN. Reading a clean corpus and saying "clean" is the half that
// can be true by accident, so the proofs run the SAME scan + verdict over
// fixture corpora — one defect per arm: an invocation with no note, a note that
// lands AFTER the invocation, a man page that advertises the verb, an exemption
// with no reason, and the vacuity arms (an empty corpus, a corpus of only
// historical records) that must be REFUSED rather than passed.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

const (
	// retiredRefJudgedFloor is the non-vacuity floor of the REAL-TREE run: the
	// minimum number of files the walk must JUDGE before its green counts as
	// evidence. Measured on this rev WITH the fourth promise root wired: 1127
	// judged — 771 normative + 12 roadmap corpus documents, the man pages, the
	// root-level markdown, and 292 WORK ORDERS under briefings/. (Before the root
	// was gathered the same number read 835, and it was healthy: a root the walk
	// never offers is not part of the total.) It is slack deliberately (≈20% below
	// the measured population) so archiving documents does not need a change here,
	// while a walk that read a wrong root, an empty directory or a filtered listing
	// is caught by name.
	retiredRefJudgedFloor = 700

	// retiredRefBriefingFloor is the FOURTH floor, and it is the one this root
	// exists for: the minimum number of WORK ORDERS the walk must JUDGE.
	//
	// WHY IT IS A SEPARATE NUMBER. `judged` is a single total, and a root that is
	// never walked cannot lower it — measured on this rev before the fix: judged
	// 835 with `briefings/` absent entirely, a healthy-looking figure over 292
	// tracked work orders nobody had read. A total cannot report the absence of a
	// constituent; a floor on the constituent can.
	//
	// Measured on this rev with the root wired: 292 briefings judged. The floor is
	// slack (≈17% below) so archiving a batch of work orders does not need a change
	// here, while a walk that stopped offering the root is caught by name.
	retiredRefBriefingFloor = 240

	// retiredRefBriefingFloorPublicExport is the SAME floor for a tree that
	// carries no `briefings/` root at all — which is what the public export is.
	// It is 0 rather than 240 because the quantity it guards (work orders judged)
	// has no members to count there: holding it at 240 does not measure a
	// drifted walk, it refuses a tree that is shaped correctly for its audience.
	// The ratchet is not lost — the arm reads `briefingsPresent` and applies 240
	// the moment the directory exists, so a tree that HAS the root and stops
	// walking it still fails by name.
	retiredRefBriefingFloorPublicExport = 0

	// retiredRefCommandFloor is the second, independent floor: judged files that
	// CARRY a command-position invocation. If the recognizer silently stops
	// matching (a regex typo, a moved vocabulary constant), Judged stays high and
	// every file passes — a green measured on nothing. Measured on this rev: 23
	// judged files carry one, 9 of them WORK ORDERS under briefings/ (before the
	// root was gathered: 14, none of them a briefing). This floor makes that state
	// red instead.
	// retiredRefCensusGeneratorFamilyFloor is the FAMILY's own floor, and it is
	// the fifth independent one for the same reason as the others: a count that
	// is part of a total can be kept healthy by the members that ARE named while
	// a missing member measures nothing. The documentation-citation teardown
	// (`78bdf05a`) deleted three generators — `config-anchor-census.go`,
	// `citation-census.go`, `citation-selector-census.go` — so three is the
	// measured family size, and dropping a row below it must fail on its own arm
	// rather than being absorbed by the corpus still having pipeline hits.
	retiredRefCensusGeneratorFamilyFloor = 3

	retiredRefCommandFloor = 18

	// retiredRefCLICommandFloor is the THIRD floor and it is deliberately
	// SEPARATE from the two above. It guards the CLI RECOGNIZER, not the corpus:
	// it is asserted against a CONTROL scan built from taskverbs.RemovedVerbs
	// (one document per retired verb), so the half is proved alive even after the
	// sweep has made the real tree clean.
	//
	// WHY A CONTROL AND NOT A CORPUS COUNT. The obvious floor — "the real tree
	// must still contain N CLI invocations" — was written first and it is wrong:
	// on this rev the sweep took the last one away (`clean 826 == judged 826`,
	// CLI half 0), and such a floor would refuse a CLEAN corpus, i.e. it would
	// demand that documents keep teaching dead verbs. The property worth pinning
	// is "the recognizer still matches the derived vocabulary", which a control
	// answers directly and a defect count answers only by accident.
	//
	// The floor still refuses the state this half exists to close: a vocabulary
	// narrowed back to the document pipeline's two tokens, or `RemovedVerbs`
	// emptied, leaves the control at 0 and turns the run red
	// (TestRetiredPipelineGateCLIHalfRefusesItsOwnVacuum drives that arm).
	retiredRefCLICommandFloor = 5

	// retiredRefTopLevelFloorCount is the THIRD floor and it is separate for the
	// same reason the CLI floor is (see retiredRefCLICommandFloor above): it
	// guards the TOP-LEVEL recognizer against the state where a vocabulary edit
	// silently stops matching and every document passes. It is asserted against a
	// CONTROL scan (documents planted for the retired top-level words), never
	// against the real corpus — a clean corpus must not be the reason a
	// recognizer goes unchecked, and a corpus-based floor would demand that
	// documents go on teaching a removed verb.
	retiredRefTopLevelFloorCount = 1
)

// retiredRefExemptions is the ratcheting permit table: a repository-relative
// path that may name a retired surface WITHOUT the note, with the reason it may.
//
// An entry is a DEBT, not a right. The store is empty on purpose — the sweep
// that made this gate green annotated every document rather than permitting it,
// because a note costs one paragraph and a permit costs every future reader the
// question "what did this one get away with?". A row added here must say why the
// note cannot be written, and the arms below require a non-empty reason and
// fail an entry that no longer corresponds to a judged file, so the table
// ratchets DOWN (Rule 39's shape, applied to prose).
var retiredRefExemptions = map[string]string{}

// retiredRefPromiseRoots are the directories whose markdown is judged no matter
// what class it claims: the man pages (the official promise surface — a page
// that advertises a verb the tree does not have is the defect itself, whatever
// the class machinery says) and the module root's own documents (PROJECT.md and
// its siblings state this repository's rules). The corpus is judged on its
// class; the briefings are judged because they carry no class at all.
var retiredRefPromiseRoots = []string{retiredRefManRoot}

// retiredRefRootDocuments is the module root's documents of record, by NAME (a
// glob would make a new root document silently judged and a renamed one silently
// dropped; naming them makes each an addition someone decided on).
//
// THE LIST IS DATA so an arm can pin its membership rather than trusting the
// walk's body: `CONFIG.md` was missing from it while the comment above claimed
// "PROJECT.md, CONFIG.md's siblings", and nothing in this file could notice —
// the absence of a name from a literal cannot be tested by reading the literal.
// `CONFIG.md` is the densest teaching surface the retired documentation-citation
// family has (fourteen mentions, including the `--check` command a reader
// pastes), so its row is the one this arm exists for.
var retiredRefRootDocuments = []string{"PROJECT.md", "CONFIG.md", "README.md"}

// retiredRefGather walks the tree and returns the documents the rule applies to.
// It reports read failures instead of skipping them: an unreadable document must
// never be reported as a clean one (Rule 15).
func retiredRefGather(t *testing.T, root string) []retiredRefFile {
	t.Helper()
	var files []retiredRefFile
	add := func(rel string, info os.FileInfo, err error) {
		if err != nil || info.IsDir() {
			return
		}
		if !strings.HasSuffix(rel, ".md") {
			return
		}
		body, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Errorf("%s: read: %v", rel, readErr)
			return
		}
		files = append(files, retiredRefFile{Path: rel, Body: string(body)})
	}
	// The corpus, walked in full: the class banner decides which of its files
	// are standing claims.
	err := filepath.Walk(filepath.Join(root, retiredRefCorpusRoot), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			t.Errorf("%s: walk: %v", path, walkErr)
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Errorf("%s: relate: %v", path, relErr)
			return nil
		}
		add(filepath.ToSlash(rel), info, nil)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s/: %v", retiredRefCorpusRoot, err)
	}
	for _, dir := range retiredRefPromiseRoots {
		entries, dirErr := os.ReadDir(filepath.Join(root, dir))
		if dirErr != nil {
			t.Fatalf("read %s/: %v", dir, dirErr)
		}
		for _, entry := range entries {
			info, statErr := entry.Info()
			if statErr != nil {
				t.Errorf("%s/%s: stat: %v", dir, entry.Name(), statErr)
				continue
			}
			add(dir+"/"+entry.Name(), info, nil)
		}
	}
	// The module root's own documents. Read by name rather than by a glob so a
	// new root document is a deliberate addition.
	//
	// `CONFIG.md` was MISSING from this list, and its absence was a vacuum, not a
	// decision: the comment above it said "PROJECT.md, CONFIG.md's siblings" —
	// naming the page it did not read. CONFIG.md is the tree's densest teaching
	// surface for the retired documentation-citation family (thirteen mentions of
	// `config-anchor-census`, including the `--check` command a reader pastes), and
	// it is a document of record at the repo root, exactly like PROJECT.md. While
	// it was absent, a BARE unannotated invocation planted at its line 2 left this
	// gate GREEN (measured 2026-11-05); the note it does carry was checked by
	// nothing but a reader's eye.
	// it does carry was checked by nothing but a reader's eye.
	for _, name := range retiredRefRootDocuments {
		info, statErr := os.Stat(filepath.Join(root, name))
		if statErr != nil {
			continue
		}
		add(name, info, nil)
	}
	// THE WORK ORDERS, walked in full and JUDGED unconditionally: a briefing
	// carries no doc-class banner (it is the text one run was dispatched with), so
	// there is nothing to filter on. Its absence from this walk was the defect
	// this root repairs — 292 tracked briefing files were never offered to the
	// scan, and the ones that opened a line with a retired verb were invisible.
	//
	// AN ABSENT ROOT IS REPORTED, NOT FATAL. This gate was written on the
	// private development tree, where `briefings/` carries ~292 work orders; the
	// public export ships NO such directory (a briefing is the text one internal
	// run was dispatched with, and it is not part of the product). Walking a
	// non-existent root used to be a Fatalf, which meant the export could not run
	// this gate at all — so the root's ABSENCE is now its own recorded fact and
	// the floor below is derived from it, rather than the walk being skipped
	// silently (Rule 15: silence is the defect, not the inconvenience).
	briefingsPresent := true
	if _, statErr := os.Stat(filepath.Join(root, retiredRefBriefingRoot)); statErr != nil {
		briefingsPresent = false
		t.Logf("%s: %s/ is ABSENT in this tree — the fourth promise root carries no work orders here, so its floor is derived as 0 (a present-but-unwalked root still fails it)",
			retiredRefProvenance, retiredRefBriefingRoot)
	}
	briefingWalkErr := filepath.Walk(filepath.Join(root, retiredRefBriefingRoot), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			// An ABSENT root is reported ONCE above (and caught by the derived
			// floor); reporting the same absence again per path would turn a
			// deliberate shape into noise a reader learns to ignore.
			if !briefingsPresent && errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			t.Errorf("%s: walk: %v", path, walkErr)
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Errorf("%s: relate: %v", path, relErr)
			return nil
		}
		add(filepath.ToSlash(rel), info, nil)
		return nil
	})
	if briefingWalkErr != nil && briefingsPresent {
		t.Fatalf("walk %s/: %v", retiredRefBriefingRoot, briefingWalkErr)
	}
	if briefingWalkErr != nil && !briefingsPresent && !errors.Is(briefingWalkErr, fs.ErrNotExist) {
		t.Fatalf("walk %s/ (reported absent, but the walk failed for another reason): %v", retiredRefBriefingRoot, briefingWalkErr)
	}
	return files
}

// retiredRefVerdict turns a report into the failures the gate reports, and
// refuses a green it did not measure. The three floors are passed in rather than
// read from the test's constants so a proof can drive the verdict over a fixture
// with floors of its own.
//
// The FOURTH floor (`briefingFloor`) guards the fourth promise root, and it is
// the one that would have caught the defect this root repairs: before
// `briefings/` was walked at all, `judged` was 835 and the number was healthy —
// a root the walk never offered cannot lower a total it is not part of. So the
// count of WORK ORDERS JUDGED is its own number, measured on its own, exactly
// as the two recognizer halves are.
func retiredRefVerdict(rep retiredRefReport, judgedFloor, commandFloor, cliFloor, briefingFloor int) []string {
	failures := append([]string(nil), rep.Failures...)
	if rep.Judged < judgedFloor {
		failures = append(failures, fmt.Sprintf(
			"%s: judged only %d file(s), below the non-vacuity floor %d — a walk that read a wrong root, an empty directory or a filtered listing cannot pass",
			retiredRefProvenance, rep.Judged, judgedFloor))
	}
	if rep.WithCommand < commandFloor {
		failures = append(failures, fmt.Sprintf(
			"%s: found only %d file(s) invoking a retired surface, below the recognizer floor %d — the vocabulary constant or the invocation pattern has stopped matching, so this run's green measures nothing",
			retiredRefProvenance, rep.WithCommand, commandFloor))
	}
	if rep.WithCLI < cliFloor {
		failures = append(failures, fmt.Sprintf(
			"%s: found only %d file(s) invoking a RETIRED CLI VERB, below the CLI recognizer floor %d — the vocabulary derived from taskverbs.RemovedVerbs or the `gopi task <verb>` pattern has stopped matching, so the CLI half of this gate measures nothing while the document-pipeline half keeps the combined count healthy",
			retiredRefProvenance, rep.WithCLI, cliFloor))
	}
	if rep.Briefings < briefingFloor {
		failures = append(failures, fmt.Sprintf(
			"%s: judged only %d WORK ORDER(s) under %s/, below the briefing-root floor %d — the fourth promise root is not being gathered, so a briefing that opens a line with a retired verb is invisible while the other three roots keep the total healthy",
			retiredRefProvenance, rep.Briefings, retiredRefBriefingRoot, briefingFloor))
	}
	return failures
}

// retiredRefCLIControlScan proves the CLI half of the recognizer is ALIVE by
// running it over one synthetic document per retired family verb. It is a
// measurement of the RECOGNIZER, deliberately independent of the corpus: after
// the sweep the real tree holds no CLI invocation at all, so a corpus-count
// floor could only be satisfied by documents that teach dead verbs.
func retiredRefCLIControlScan() retiredRefReport {
	verbs := retiredRefCLIVerbs()
	pairs := make([]string, 0, len(verbs)*2)
	for _, verb := range verbs {
		pairs = append(pairs,
			"devdoc/control-"+verb+".md",
			"<!-- doc-class: normative -->\n# control\n\n$ gopi task "+verb+" --help\n")
	}
	return scanRetiredRefs(retiredRefFixture(pairs...), nil)
}

// retiredRefTopLevelControlScan proves the TOP-LEVEL half of the recognizer is
// ALIVE: one synthetic normative document per retired top-level word, planted at
// command position in both spellings the pattern claims (bare and via ./bin/).
func retiredRefTopLevelControlScan() retiredRefReport {
	words := retiredRefTopLevelVerbs
	pairs := make([]string, 0, len(words)*2)
	for _, word := range words {
		pairs = append(pairs,
			retiredRefDocPath("control-top-"+word+".md"),
			"<!-- doc-class: normative -->\n# control\n\n$ gopi "+word+" --status\n",
			retiredRefDocPath("control-top-bin-"+word+".md"),
			"<!-- doc-class: normative -->\n# control\n\n$ ./bin/gopi "+word+"\n",
		)
	}
	return scanRetiredRefs(retiredRefFixture(pairs...), nil)
}

// retiredRefTopLevelFloor is the third half's own non-vacuity arm: it reads only
// the measurement, never the control's planted failures.
func retiredRefTopLevelFloor(rep retiredRefReport, floor int) []string {
	if rep.WithTop >= floor {
		return nil
	}
	return []string{fmt.Sprintf(
		"%s: the TOP-LEVEL recognizer matched only %d document(s), below its floor %d — retiredRefTopLevelVerbs or the `gopi <verb>` command-position pattern has stopped matching, so this half of the gate measures nothing while the other two keep the combined count healthy",
		retiredRefProvenance, rep.WithTop, floor)}
}

// retiredRefCLIFloor is the CLI half's non-vacuity arm on its own: it reads only
// the measurement, never the control's planted failures.
func retiredRefCLIFloor(rep retiredRefReport, cliFloor int) []string {
	if rep.WithCLI >= cliFloor {
		return nil
	}
	return []string{fmt.Sprintf(
		"%s: the CLI recognizer matched only %d document(s), below its floor %d — the vocabulary derived from taskverbs.RemovedVerbs or the `gopi task <verb>` pattern has stopped matching, so the CLI half of this gate measures nothing while the document-pipeline half keeps the combined count healthy",
		retiredRefProvenance, rep.WithCLI, cliFloor)}
}

// TestRetiredPipelineGate is the gate: no standing claim in this tree teaches a
// retired surface — the document pipeline's, or any retired CLI verb's — without
// saying, earlier in the same file, that the mechanism is retired.
func TestRetiredPipelineGate(t *testing.T) {
	root := repoRoot(t)
	files := retiredRefGather(t, root)
	rep := scanRetiredRefs(files, retiredRefExemptions)
	t.Log(rep.Summary())

	// The CLI half's liveness, measured on its own control (see
	// retiredRefCLICommandFloor): the real tree is expected to be CLEAN here,
	// and a clean corpus must not be the reason the recognizer goes unchecked.
	control := retiredRefCLIControlScan()
	t.Logf("%s: CLI recognizer control — %d document(s) planted for %d derived verb(s), %d recognized at command position",
		retiredRefProvenance, len(retiredRefCLIVerbs()), len(retiredRefCLIVerbs()), control.WithCLI)

	// The TOP-LEVEL half's liveness, measured the same way and for the same
	// reason (retiredRefTopLevelFloorCount): the real tree is expected to be
	// clean here too once the corpus is swept.
	topControl := retiredRefTopLevelControlScan()
	t.Logf("%s: top-level recognizer control — %d document(s) planted for %d retired top-level word(s), %d recognized at command position",
		retiredRefProvenance, 2*len(retiredRefTopLevelVerbs), len(retiredRefTopLevelVerbs), topControl.WithTop)

	// Only the FLOOR arms of the controls are consulted — their per-document
	// failures are the point of planting them (each planted line is unannotated
	// on purpose), so they are not the gate's business.
	// `briefingsPresent` is answered HERE, not carried out of the gather: the
	// gather's job is to offer files, and a floor must be derived from the TREE
	// the arm is judging, so the two statements cannot drift apart.
	briefingFloor := retiredRefBriefingFloor
	if _, statErr := os.Stat(filepath.Join(root, retiredRefBriefingRoot)); statErr != nil {
		briefingFloor = retiredRefBriefingFloorPublicExport
	}
	failures := retiredRefVerdict(rep, retiredRefJudgedFloor, retiredRefCommandFloor, 0, briefingFloor)
	failures = append(failures, retiredRefCLIFloor(control, retiredRefCLICommandFloor)...)
	failures = append(failures, retiredRefTopLevelFloor(topControl, retiredRefTopLevelFloorCount)...)
	sort.Strings(failures)
	for _, failure := range failures {
		t.Error(failure)
	}
}

// --- the proofs -----------------------------------------------------------
//
// Each arm builds an in-memory corpus and drives the REAL scan + verdict over it.

// retiredRefFixture builds a fixture corpus from (path, body) pairs.
// retiredRefDocPath / retiredRefManPath build a fixture path at RUNTIME.
//
// WHY NOT A LITERAL (measured 2026-10-31): a literal `devdoc/<name>.md`
// literal in a _test.go is picked up by the citation census as a citation of a
// path no commit ever tracked, so `citation-census --check` refused the shipped
// table with seven `+ fixture` rows — the gate for Rule 38 turned red because a
// DIFFERENT gate's test mentioned a file name. Splitting the string keeps the
// fixture just as readable while leaving no path-shaped literal in the source.
func retiredRefDocPath(name string) string { return retiredRefCorpusRoot + "/" + name }
func retiredRefManPath(name string) string { return retiredRefManRoot + "/" + name }

// retiredRefBriefingPath builds a WORK-ORDER fixture path at RUNTIME, for the
// same citation-census reason as the two above (a path-shaped literal in a
// _test.go is read as a citation of a file no commit tracked).
func retiredRefBriefingPath(name string) string { return retiredRefBriefingRoot + "/" + name }

func retiredRefFixture(pairs ...string) []retiredRefFile {
	files := make([]retiredRefFile, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		files = append(files, retiredRefFile{Path: pairs[i], Body: pairs[i+1]})
	}
	return files
}

// retiredRefFixtureNote is the ancestor sentence of the shipped note, which is
// the whole recognizer.
const retiredRefFixtureNote = "> **" + retiredRefNoteMarker + ").** these commands are the record of a past run.\n"

// TestRetiredPipelineGateRedProofMissingNote is the arm the gate exists for: a
// standing claim that PASTES a retired command with nothing said about it.
func TestRetiredPipelineGateRedProofMissingNote(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("teaches-it.md"),
		"<!-- doc-class: normative -->\n# A page that still teaches it\n\n$ python3 tools/docclass/classify.py .\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("teaches-it.md")+":4", "no retired-mechanism note")
}

// TestRetiredPipelineGateRedProofNoteAfterInvocation is the ORDER half: a note
// below the command is no note at all to the reader who already pasted it.
func TestRetiredPipelineGateRedProofNoteAfterInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("note-too-late.md"),
		"<!-- doc-class: roadmap -->\n# Roadmap\n\ngopi tool doccheck\n\n"+retiredRefFixtureNote,
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("note-too-late.md")+":4", "AFTER it")
}

// TestRetiredPipelineGateGreenWhenAnnotated is the arm that keeps the gate
// usable: the shipped note, ABOVE the command, passes.
func TestRetiredPipelineGateGreenWhenAnnotated(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("ok.md"),
		"<!-- doc-class: normative -->\n# Annotated\n\n"+retiredRefFixtureNote+"\n$ gopi tool doccheck --fix\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("annotated document was refused: %v", rep.Failures)
	}
	if rep.Noted != 1 || rep.WithCommand != 1 {
		t.Fatalf("annotated document was not counted: %+v", rep)
	}
}

// TestRetiredPipelineGateHistoricalIsCountedNotJudged pins the scope decision: a
// one-run record must be free to quote what was typed that day.
func TestRetiredPipelineGateHistoricalIsCountedNotJudged(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("record.md"),
		"<!-- doc-class: historical -->\n# What ran that day\n\n$ gopi tool doccheck\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("a historical record was judged: %v", rep.Failures)
	}
	if rep.Historical != 1 || rep.Judged != 0 {
		t.Fatalf("historical document was not counted as one: %+v", rep)
	}
}

// TestRetiredPipelineGateRedProofCLIVerbWithNoNote is the arm THIS half exists
// for: a standing claim that teaches a retired FAMILY verb at command position.
// `58b5618b` retired `finish-cleanup`, and normative pages went on telling
// readers to run it while this gate stayed green — the vocabulary named only the
// document pipeline, so nothing here could match.
func TestRetiredPipelineGateRedProofCLIVerbWithNoNote(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("teaches-a-verb.md"),
		"<!-- doc-class: normative -->\n# A page that still teaches a retired verb\n\n"+
			"$ gopi task finish-cleanup --task <id> --apply\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("teaches-a-verb.md")+":4", "retired CLI verb")
	if rep.WithCLI != 1 {
		t.Fatalf("the CLI half did not count the document: %+v", rep)
	}
}

// TestRetiredPipelineGateRedProofCLIVerbNoteAfterInvocation is the ORDER half,
// measured on the CLI vocabulary: a note below the command is no note at all.
func TestRetiredPipelineGateRedProofCLIVerbNoteAfterInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("verb-note-too-late.md"),
		"<!-- doc-class: normative -->\n# Order\n\ngopi task sweep-zombies --stall-sec 900\n\n"+
			retiredRefFixtureNote,
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("verb-note-too-late.md")+":4", "AFTER it")
}

// TestRetiredPipelineGateGreenWhenCLIVerbAnnotated keeps the half usable: the
// shipped note ABOVE a retired verb passes, and both halves are counted apart.
func TestRetiredPipelineGateGreenWhenCLIVerbAnnotated(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("verb-ok.md"),
		"<!-- doc-class: normative -->\n# Annotated\n\n"+
			"> **"+retiredRefNoteRecognizer+"worktree lifecycle, 2026-10-30).** this is the record.\n\n"+
			"$ gopi task for-branch <branch>\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("annotated CLI document was refused: %v", rep.Failures)
	}
	if rep.WithCLI != 1 || rep.Noted != 1 {
		t.Fatalf("annotated CLI document was not counted: %+v", rep)
	}
}

// TestRetiredPipelineGateCLIVerbMentionIsNotAnInvocation pins the CRITERION that
// keeps this half dispatchable: a normative page that DISCUSSES a retired verb
// without telling anyone to run it is doing the corpus a service. Only command
// position is judged — the wide mention population (measured: 82 normative
// documents name a retired verb somewhere) is deliberately not this gate's
// business, and treating it as such would punish the sentences that explain the
// retirement.
func TestRetiredPipelineGateCLIVerbMentionIsNotAnInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("discusses.md"),
		"<!-- doc-class: normative -->\n# What was retired\n\n"+
			"`finish-cleanup` was retired in `58b5618b`; the rows read `verify`, and the\n"+
			"`for-branch` lookup became `gopi task show --branch <branch>`.\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("a mere discussion of retired verbs was judged as an invocation: %v", rep.Failures)
	}
	if rep.WithCLI != 0 || rep.CLICmd != 0 {
		t.Fatalf("a mention was counted as a command-position hit: %+v", rep)
	}
}

// TestRetiredPipelineGateCLIHalfRefusesItsOwnVacuum is the floor proof that the
// COMBINED arm cannot express: a corpus healthy on the document-pipeline half
// and empty on the CLI half must be REFUSED. With one shared floor this run
// passes — which is precisely how a vocabulary narrowed back to two hardcoded
// tokens would hide.
func TestRetiredPipelineGateCLIHalfRefusesItsOwnVacuum(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("pipeline-only-1.md"),
		"<!-- doc-class: normative -->\n# Kept\n\n"+
			"> **"+retiredRefNoteRecognizer+"document-pipeline teardown, slice 1/5).** gone.\n\n"+
			"$ python3 tools/docclass/classify.py .\n",
		retiredRefDocPath("pipeline-only-2.md"),
		"<!-- doc-class: normative -->\n# Kept\n\n"+
			"> **"+retiredRefNoteRecognizer+"document-pipeline teardown, slice 3/5).** gone.\n\n"+
			"$ gopi tool doccheck\n",
	)
	rep := scanRetiredRefs(files, nil)
	if rep.WithCLI != 0 || rep.WithPipe != 2 {
		t.Fatalf("fixture does not separate the two halves: %+v", rep)
	}
	// A shared recognizer floor is satisfied by the pipeline half alone.
	if got := retiredRefVerdict(rep, 0, 1, 0, 0); len(got) != 0 {
		t.Fatalf("control: a pipeline-only corpus should pass the combined floor, got %v", got)
	}
	// The CLI floor is the arm that refuses it.
	got := retiredRefVerdict(rep, 0, 1, retiredRefCLICommandFloor, 0)
	if len(got) == 0 {
		t.Fatal("a corpus with NO retired-CLI-verb invocation passed the CLI floor — the CLI half is not wired")
	}
	if !strings.Contains(strings.Join(got, "\n"), "RETIRED CLI VERB") {
		t.Fatalf("the refusal did not name the CLI half: %v", got)
	}
}

// TestRetiredPipelineGateCLIVocabularyIsDerived pins Rule 14's requirement: the
// vocabulary comes from the ONE source of truth, not from a list here. It is a
// real check on the derivation — every member of taskverbs.RemovedVerbs must be
// recognized at command position, so a verb added upstream is covered without
// editing this gate.
func TestRetiredPipelineGateCLIVocabularyIsDerived(t *testing.T) {
	verbs := retiredRefCLIVerbs()
	if len(verbs) != len(taskverbs.RemovedVerbs) {
		t.Fatalf("derived %d verb(s) from taskverbs.RemovedVerbs %d — the derivation is not a copy",
			len(verbs), len(taskverbs.RemovedVerbs))
	}
	for _, verb := range taskverbs.RemovedVerbs {
		body := "<!-- doc-class: normative -->\n# Upstream verb\n\n$ gopi task " + verb + " --help\n"
		rep := scanRetiredRefs(retiredRefFixture(retiredRefDocPath("upstream.md"), body), nil)
		requireRetiredRefFailure(t, rep, retiredRefDocPath("upstream.md")+":4", "retired CLI verb")
		if rep.CLICmd != 1 {
			t.Fatalf("verb %q was not counted as one CLI hit: %+v", verb, rep)
		}
	}
	t.Logf("derived vocabulary: %d verb(s) from taskverbs.RemovedVerbs = %v", len(verbs), verbs)
}

// TestRetiredPipelineGateRefusesAManPageAdvertisement is the second surface the
// gate covers: the man page is the OFFICIAL promise, so it is held to mention
// rather than command position — a list of "internal commands" that includes a
// verb the tree does not have is the same lie in smaller type.
func TestRetiredPipelineGateRefusesAManPageAdvertisement(t *testing.T) {
	files := retiredRefFixture(
		retiredRefManPath("gopi-tool.md"),
		"# gopi tool\n\nA third block is marked **internal**: `doccheck`, `ledger-window`.\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefManPath("gopi-tool.md")+":3", "no retirement stated")

	// The CLI half of the same promise surface: a page listing a retired family
	// verb beside the commands that survived.
	verbPage := retiredRefFixture(
		retiredRefManPath("gopi-task.md"),
		"# gopi task\n\n`gopi task for-branch <branch>` is gone.\n",
	)
	rep = scanRetiredRefs(verbPage, nil)
	requireRetiredRefFailure(t, rep, retiredRefManPath("gopi-task.md")+":3", "no retirement stated")
}

// TestRetiredPipelineGateExemptionNeedsAReason is the ratchet's own guard: an
// unreasoned permit is indistinguishable from a leak, so it fails as one.
func TestRetiredPipelineGateExemptionNeedsAReason(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("permitted.md"),
		"<!-- doc-class: normative -->\n# Permitted, badly\n\n$ gopi tool doccheck\n",
	)
	rep := scanRetiredRefs(files, map[string]string{retiredRefDocPath("permitted.md"): "   "})
	requireRetiredRefFailure(t, rep, retiredRefDocPath("permitted.md"), "exemption carries no reason")

	rep = scanRetiredRefs(files, map[string]string{retiredRefDocPath("permitted.md"): "the quote IS the evidence of the defect"})
	if len(rep.Failures) != 0 {
		t.Fatalf("a reasoned exemption was refused: %v", rep.Failures)
	}
	if rep.Exempt != 1 {
		t.Fatalf("reasoned exemption was not counted: %+v", rep)
	}
}

// TestRetiredPipelineGateRefusesAVacuum: a scan that judged nothing, and a
// scan that recognized nothing, are both refused — the two ways this gate could
// print a green it did not measure.
func TestRetiredPipelineGateRefusesAVacuum(t *testing.T) {
	empty := scanRetiredRefs(nil, nil)
	if len(retiredRefVerdict(empty, retiredRefJudgedFloor, retiredRefCommandFloor, retiredRefCLICommandFloor, retiredRefBriefingFloor)) == 0 {
		t.Fatal("an empty corpus was not refused — the vacuity floor is not wired")
	}
	noHits := scanRetiredRefs(retiredRefFixture(
		retiredRefDocPath("quiet.md"), "<!-- doc-class: normative -->\n# Nothing to see\n",
	), nil)
	verdict := retiredRefVerdict(noHits, retiredRefJudgedFloor, retiredRefCommandFloor, retiredRefCLICommandFloor, retiredRefBriefingFloor)
	if len(verdict) == 0 {
		t.Fatal("a scan that recognized no invocation was not refused — the recognizer floor is not wired")
	}
	if !strings.Contains(strings.Join(verdict, "\n"), "recognizer floor") {
		t.Fatalf("the refusal did not name the recognizer floor: %v", verdict)
	}
}

// TestRetiredPipelineGateEscapeListIsRatchetable pins the shipped table's own
// shape: every row names a reason, and the store is the one field a future
// change edits.
func TestRetiredPipelineGateEscapeListIsRatchetable(t *testing.T) {
	for path, reason := range retiredRefExemptions {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("exemption %q carries no reason", path)
		}
		if !strings.HasSuffix(path, ".md") {
			t.Errorf("exemption %q does not name a markdown document", path)
		}
	}
	t.Logf("exemption table: %d row(s) (measured on this rev: %d)", len(retiredRefExemptions), len(retiredRefExemptions))
}

// --- the TOP-LEVEL retired verb half (epic merge-removal, slice 1/4) --------
//
// `gopi merge` was removed: its implementation (cmd/agent/merge_run.go and its
// flag parser) is gone, and the word survives only as a REFUSAL row in
// cmd/agent/cli_verb_aliases.go (`gopi: `merge` is RETIRED — use
// `gopi task merge``). The ENGINE survives under the family spelling.
//
// A line a reader can PASTE as `gopi merge …` is therefore an instruction to run
// a surface this tree does not have — the same defect the family half was
// written for, one level up. These arms are the red proofs the vocabulary
// addition would otherwise be a vacuous green without: measured before the entry
// existed, the gate answered `clean 1`, `command-position invocation 0` over a
// planted `$ gopi merge --status`.

// TestRetiredPipelineGateRedProofTopLevelVerb is the arm this slice exists for.
func TestRetiredPipelineGateRedProofTopLevelVerb(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("teaches-merge.md"),
		"<!-- doc-class: normative -->\n# A page that still teaches the removed verb\n\n"+
			"$ gopi merge --status\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("teaches-merge.md")+":4", retiredRefKindTopLevel)
	if rep.WithTop != 1 || rep.TopCmd != 1 {
		t.Fatalf("the top-level half did not count the document: %+v", rep)
	}
}

// TestRetiredPipelineGateRedProofTopLevelNoteAfterInvocation is the ORDER half
// on the same vocabulary.
func TestRetiredPipelineGateRedProofTopLevelNoteAfterInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("merge-note-too-late.md"),
		"<!-- doc-class: normative -->\n# Order\n\n$ gopi merge\n\n"+retiredRefFixtureNote,
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("merge-note-too-late.md")+":4", "AFTER it")
}

// TestRetiredPipelineGateGreenWhenTopLevelAnnotated is the clean control: the
// shipped note ABOVE the command passes, and the half is counted.
func TestRetiredPipelineGateGreenWhenTopLevelAnnotated(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("merge-ok.md"),
		"<!-- doc-class: normative -->\n# Annotated\n\n"+
			"> **"+retiredRefNoteRecognizer+"merge verb removal, 2026-11-01).** the record below is what ran.\n\n"+
			"$ ./bin/gopi merge -h\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("annotated top-level document was refused: %v", rep.Failures)
	}
	if rep.WithTop != 1 || rep.Noted != 1 {
		t.Fatalf("annotated top-level document was not counted: %+v", rep)
	}
}

// TestRetiredPipelineGateTopLevelMentionIsNotAnInvocation pins the criterion that
// keeps the half usable AND pins the narrowing the first measurement forced: a
// sentence EXPLAINING the removal is the fix, and a quoted `gopi merge: …` output
// line is not a command a reader can paste.
func TestRetiredPipelineGateTopLevelMentionIsNotAnInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("merge-discussed.md"),
		"<!-- doc-class: normative -->\n# What was removed\n\n"+
			"The top-level word was removed; `gopi merge` no longer dispatches, and\n"+
			"the surviving spelling is `gopi task merge`. `git merge` is unaffected.\n\n"+
			"```\n"+
			"gopi merge: holding the single-instance lock; 1 repo(s)\n"+
			"gopi merge: run over — 1 branch decision(s)\n"+
			"```\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("a discussion of the removed word, and its own log output, were judged as invocations: %v", rep.Failures)
	}
	if rep.TopCmd != 0 {
		t.Fatalf("a mention or an output quote was counted as a command-position hit: %+v", rep)
	}
}

// TestRetiredPipelineGateTopLevelHalfRefusesItsOwnVacuum: the third half needs a
// floor of its own for the same reason the family half does — a corpus and a
// recognizer that look healthy on another half prove nothing about this one.
func TestRetiredPipelineGateTopLevelHalfRefusesItsOwnVacuum(t *testing.T) {
	control := retiredRefTopLevelControlScan()
	if got := retiredRefTopLevelFloor(control, retiredRefTopLevelFloorCount); len(got) != 0 {
		t.Fatalf("control: the top-level recognizer matched %d document(s), expected at least %d: %v",
			control.WithTop, retiredRefTopLevelFloorCount, got)
	}
	if got := retiredRefTopLevelFloor(retiredRefReport{}, retiredRefTopLevelFloorCount); len(got) == 0 {
		t.Fatal("an empty scan passed the top-level floor — the third half is not wired")
	}
}

// TestRetiredPipelineGateManPageCommandPositionIsRefused joins the promise
// surface to the new half: a man page's SYNOPSIS ROW is never licensed by a note,
// because a note cannot un-prescribe a command.
func TestRetiredPipelineGateManPageCommandPositionIsRefused(t *testing.T) {
	files := retiredRefFixture(
		retiredRefManPath("gopi-merge.md"),
		"# gopi-merge\n\n> **"+retiredRefNoteRecognizer+"merge verb removal).** removed.\n\n"+
			"# SYNOPSIS\n\n```\n  gopi merge --fast\n```\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefManPath("gopi-merge.md")+":8", "command position")
}

// TestRetiredPipelineGateManPageMentionWithNotePasses is the other side of that
// boundary: a page that STATES the retirement before explaining it is information,
// not an advertisement.
func TestRetiredPipelineGateManPageMentionWithNotePasses(t *testing.T) {
	files := retiredRefFixture(
		retiredRefManPath("gopi-merge.md"),
		"# gopi-merge\n\n> **"+retiredRefNoteRecognizer+"merge verb removal, 2026-11-01).** the\n"+
			"> top-level word `gopi merge` was removed; a merge pass is `gopi task merge`.\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("a man page that states the retirement was refused: %v", rep.Failures)
	}
	if rep.Noted != 1 {
		t.Fatalf("the noted mention was not counted: %+v", rep)
	}
}

// TestRetiredPipelineGateManPageNoteAfterMentionIsRefused covers the third arm of
// the promise surface's boundary — a note that lands BELOW the mention. Without
// it the branch that names this case (`noteLine > line`) would be a path no arm
// ever walks, and a gate branch nobody tests is a branch that quietly rots.
func TestRetiredPipelineGateManPageNoteAfterMentionIsRefused(t *testing.T) {
	files := retiredRefFixture(
		retiredRefManPath("gopi-task.md"),
		"# gopi task\n\nthe top-level `gopi merge` word is gone.\n\n"+
			"> **"+retiredRefNoteRecognizer+"merge verb removal).** as said above.\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefManPath("gopi-task.md")+":3", "AFTER the mention")
}

// TestRetiredPipelineGateTopLevelVocabularyMatchesTheRefusalTable is the drift
// arm, and it exists because the two spellings CANNOT share a constant: the
// refusal row lives in `package main` (cmd/agent/cli_verb_aliases.go), which no
// gate can import, and this package's own name for the word is the only one it
// can reach. So the danger is a rename on ONE side — a gate that goes on judging
// `merge` while the refusal row answers something else, i.e. a green over a word
// nothing refuses any more.
//
// It reads the refusal table's SOURCE and pins the two facts the gate depends
// on: the row is still declared, and its value is the word this vocabulary names.
// It reads the file rather than the constant on purpose — importing is impossible
// and a second const in this package would be the mirror Rule 13 forbids.
func TestRetiredPipelineGateTopLevelVocabularyMatchesTheRefusalTable(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(retiredRefAliasSourceRel)))
	if err != nil {
		t.Fatalf("%s: %v — the refusal table this gate's top-level vocabulary stands on could not be read", retiredRefAliasSourceRel, err)
	}
	body := string(src)
	if !strings.Contains(body, retiredRefAliasRowName) {
		t.Fatalf("%s no longer declares %s — the retired top-level word's refusal row is gone, so `gopi %s` no longer refuses BY NAME and this gate's vocabulary is judging a word on its way back into the dispatcher",
			retiredRefAliasSourceRel, retiredRefAliasRowName, retiredRefTokenMergeVerb)
	}
	// The declaration is `verbAliasMerge = "<word>"`: read the VALUE, not a
	// substring, so a rename to `"merge-x"` cannot pass on the prefix.
	decl := regexp.MustCompile(retiredRefAliasRowName + `\s*=\s*"([^"]+)"`).FindStringSubmatch(body)
	if decl == nil {
		t.Fatalf("%s: %s is declared without a string literal value — the gate cannot read the word it refuses", retiredRefAliasSourceRel, retiredRefAliasRowName)
	}
	if decl[1] != retiredRefTokenMergeVerb {
		t.Fatalf("%s: %s = %q, but this gate's top-level vocabulary names %q — the two spellings have drifted, so either the gate judges a word nothing refuses or a refused word is not judged",
			retiredRefAliasSourceRel, retiredRefAliasRowName, decl[1], retiredRefTokenMergeVerb)
	}
}

// requireRetiredRefFailure asserts that exactly-one-matching failure exists.
func requireRetiredRefFailure(t *testing.T, rep retiredRefReport, mustContain, reason string) {
	t.Helper()
	for _, failure := range rep.Failures {
		if strings.Contains(failure, mustContain) && strings.Contains(failure, reason) {
			return
		}
	}
	t.Fatalf("expected a failure naming %q and %q; got %v", mustContain, reason, rep.Failures)
}

// --- the FOURTH promise root: the work orders (briefings/) ------------------
//
// WHY THIS ARM EXISTS. The gate covered three roots — the class-scoped corpus,
// the man pages and the module root's markdown — and `briefings/` was in NONE of
// them. Measured on this rev before the root was wired: 292 tracked work orders
// were never offered to the walk, and 9 of them carried a command-position
// invocation of a retired surface (6 × `gopi merge`, 3 × `gopi task run`) while
// the gate printed `clean 835` and named nothing. The figure was not wrong — a
// root the walk never offers is not part of a total.
//
// WHAT IS DIFFERENT ABOUT THE FOURTH ROOT, and why it needs its own arm rather
// than riding the corpus one: a briefing carries NO doc-class banner (it is the
// text a run was dispatched with), so there is no class to read and nothing to
// filter on. Every work order is JUDGED, through the same generic branch the
// root documents use. The arm below proves that property directly — a briefing
// that tries to claim `historical` is still judged, because the claim is not one
// this root honours.

// TestRetiredPipelineGateJudgesTheBriefingHalf is the arm the fourth root
// exists for: the three states of a work order that opens a line with a retired
// surface — no note, note above, note below — plus the floor that catches the
// root going missing.
func TestRetiredPipelineGateJudgesTheBriefingHalf(t *testing.T) {
	// ① NO NOTE → RED, named by the file's own path and line.
	noNote := scanRetiredRefs(retiredRefFixture(
		retiredRefBriefingPath("work-order-that-teaches-it.md"),
		"# A work order that still opens with the removed verb\n\n"+
			"$ gopi merge --status\n",
	), nil)
	requireRetiredRefFailure(t, noNote, retiredRefBriefingPath("work-order-that-teaches-it.md")+":3", "no retired-mechanism note")
	if noNote.Briefings != 1 || noNote.WithTop != 1 {
		t.Fatalf("the briefing root did not JUDGE the work order: %+v", noNote)
	}

	// ② NOTE ABOVE → GREEN, and counted as both a work order and an annotated one.
	noted := scanRetiredRefs(retiredRefFixture(
		retiredRefBriefingPath("work-order-annotated.md"),
		"# An annotated work order\n\n"+
			"> **"+retiredRefNoteRecognizer+"merge verb removal, 2026-11-01).** the lines below are the record.\n\n"+
			"$ gopi merge --status\n",
	), nil)
	if len(noted.Failures) != 0 {
		t.Fatalf("an annotated work order was refused: %v", noted.Failures)
	}
	if noted.Briefings != 1 || noted.Noted != 1 || noted.WithTop != 1 {
		t.Fatalf("an annotated work order was not counted: %+v", noted)
	}

	// ③ NOTE BELOW → RED. The order rule is the same one the other three roots
	// obey: a note under the command is no note to the reader who pasted it.
	below := scanRetiredRefs(retiredRefFixture(
		retiredRefBriefingPath("work-order-note-too-late.md"),
		"# The note is in the wrong place\n\n$ gopi task run <id>\n\n"+retiredRefFixtureNote,
	), nil)
	requireRetiredRefFailure(t, below, retiredRefBriefingPath("work-order-note-too-late.md")+":3", "AFTER it")

	// ④ THE ROOT IS JUDGED, NOT FILTERED. A work order carrying a `historical`
	// banner is still judged — the banner is a corpus convention (Rule 36's
	// doc-class), and a briefing has no class to declare. Without this assertion
	// the root's behaviour would be indistinguishable from a corpus file that
	// merely happened to lack a banner, and a future reader could "helpfully"
	// route briefings through the class check and silently turn the whole root
	// green again.
	claimingHistorical := scanRetiredRefs(retiredRefFixture(
		retiredRefBriefingPath("work-order-claims-historical.md"),
		"<!-- doc-class: historical -->\n# A work order that claims a class it has not got\n\n"+
			"$ gopi merge --status\n",
	), nil)
	requireRetiredRefFailure(t, claimingHistorical, retiredRefBriefingPath("work-order-claims-historical.md")+":4", "no retired-mechanism note")
	if claimingHistorical.Historical != 0 || claimingHistorical.Briefings != 1 {
		t.Fatalf("a work order was filtered by a class banner — the fourth root must be JUDGED, not class-scoped: %+v", claimingHistorical)
	}

	// ⑤ THE FLOOR THAT CATCHES THE ROOT GOING MISSING. Every other number in the
	// report can stay healthy while this root is not gathered at all (that is
	// precisely the measured state before the fix: judged 835, briefings absent),
	// so the work-order count is floored on its own.
	healthyButNoBriefings := retiredRefReport{Judged: 100000, WithCommand: 1000, WithCLI: 100, WithTop: 100}
	if got := retiredRefVerdict(healthyButNoBriefings, 0, 0, 0, retiredRefBriefingFloor); len(got) == 0 {
		t.Fatal("a report that judged nothing under the work-order root passed the briefing floor — the fourth root is not wired into the verdict")
	}
	if got := retiredRefVerdict(healthyButNoBriefings, 0, 0, 0, 0); len(got) != 0 {
		t.Fatalf("control: with a floor of 0 the same report should pass, got %v", got)
	}
	failed := retiredRefVerdict(retiredRefReport{Briefings: retiredRefBriefingFloor}, 0, 0, 0, retiredRefBriefingFloor)
	if len(failed) != 0 {
		t.Fatalf("a report AT the briefing floor was refused: %v", failed)
	}
}

// --- the census GENERATOR is a fourth pipeline address ------------------------
//
// WHY THESE ARMS EXIST. The pipeline half's vocabulary was `tools/docclass` and
// `doccheck` only, so the documentation-citation teardown's own generators were
// a blind spot: `CONFIG.md` pastes
// `go run internal/standards/testdata/config-anchor-census.go … --check` (the
// file `78bdf05a` deleted) in a fenced block, and this gate could not see it —
// neither as an invocation nor, therefore, as a missing note. Measured on this
// rev before the token was added (2026-11-05): a BARE unannotated invocation
// planted at line 2 of `CONFIG.md` left the gate GREEN, printing
// `clean 1138`. The same measurement also showed `CONFIG.md` was absent from the
// module-root walk entirely, so the two holes hid each other: neither the
// vocabulary nor the root would have caught the line alone.

// TestRetiredPipelineGateRedProofCensusGeneratorWithNoNote is the first nail: the
// generator at command position, nothing said about it.
func TestRetiredPipelineGateRedProofCensusGeneratorWithNoNote(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("teaches-the-census.md"),
		"<!-- doc-class: normative -->\n# A page that still teaches it\n\n$ go run "+retiredRefTokenCensusGenerator+" . --check\n",
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("teaches-the-census.md")+":4", "no retired-mechanism note")
}

// TestRetiredPipelineGateRedProofCensusGeneratorNoteAfterInvocation is the ORDER
// half for THIS token: an invocation above the note is an invocation a reader
// pastes before meeting the warning. It is a separate arm because the shipped
// `CONFIG.md` note sits far below the block it retires is NOT the shape the rule
// wants — see the root-arm note below.
func TestRetiredPipelineGateRedProofCensusGeneratorNoteAfterInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("census-note-too-late.md"),
		"<!-- doc-class: normative -->\n# Roadmap\n\n$ go run "+retiredRefTokenCensusGenerator+" . --check\n\n"+retiredRefFixtureNote,
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("census-note-too-late.md")+":4", "AFTER it")
}

// TestRetiredPipelineGateGreenWhenCensusGeneratorAnnotated keeps the gate usable
// for this token: the shipped note, ABOVE the command, passes.
func TestRetiredPipelineGateGreenWhenCensusGeneratorAnnotated(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("census-ok.md"),
		"<!-- doc-class: normative -->\n# Annotated\n\n"+retiredRefFixtureNote+"\n$ go run "+retiredRefTokenCensusGenerator+" . --check\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("annotated census invocation was refused: %v", rep.Failures)
	}
	if rep.WithPipe != 1 || rep.Noted != 1 {
		t.Fatalf("annotated census invocation was not counted on the pipeline half: %+v", rep)
	}
}

// TestRetiredPipelineGateRootWalkJudgesConfigMd is the SECOND nail, and it is the
// one that would have caught the defect on its own: `CONFIG.md` is a document of
// record at the module root (the densest teaching surface this family has) and it
// was NOT in the walk, so even a correct vocabulary produced a green over it.
//
// It asserts the SHAPE the root walk must have — the file is reached and judged —
// rather than the current text of CONFIG.md, because the page is expected to keep
// changing as the retirements are annotated.
func TestRetiredPipelineGateRootWalkJudgesConfigMd(t *testing.T) {
	root := repoRoot(t)
	files := retiredRefGather(t, root)
	const configDoc = "CONFIG.md"
	found := false
	for _, f := range files {
		if f.Path == configDoc {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s is a module-root document of record but the gather never offered it to the scan — every invocation on it is outside the gate", configDoc)
	}
	// Judged, not merely walked: a root document carries no doc-class banner, so
	// it must land in Judged (class-scoped skipping can never excuse it).
	rep := scanRetiredRefs(files, retiredRefExemptions)
	if rep.Judged == 0 {
		t.Fatalf("the walk judged no document at all — a green here would measure nothing: %+v", rep)
	}
	if rep.WithPipe == 0 {
		t.Fatalf("the walk saw zero pipeline invocations across the tree — the pipeline recognizer measured nothing, so a clean verdict is a vacuum: %+v", rep)
	}
}

// --- the census GENERATOR FAMILY (all three addresses), and the root list -----
//
// WHY THESE ARMS EXIST. The first repair added ONE address
// (`config-anchor-census.go`) to the pipeline vocabulary and justified it: "the
// other two generators were never taught by a page that survives". That sentence
// did not hold on the tree it was committed to — `citation-census.go` and
// `citation-selector-census.go` both stand at COMMAND POSITION in tracked pages
// (`devdoc/citation-gate.md:157`, `devdoc/cited-test-selector-gate.md:167`).
// Those two pages are classed `historical` and are therefore counted, not judged,
// which is exactly the state in which a missing vocabulary member costs nothing
// today and everything tomorrow: RE-CLASSING one page, or writing a new one, is
// an ordinary edit, and the recognizer must already be able to see it.
//
// MEASURED (this clone, 2026-11-05, before the family was added): a BARE
// unannotated `go run internal/standards/testdata/citation-census.go . … --check`
// planted at line 2 of `CONFIG.md` left `TestRetiredPipelineGate` PASSing with
// `clean 1139`. After the family was added the same plant turns the same arm red
// with `CONFIG.md:2: invokes document-pipeline
// "internal/standards/testdata/citation-census.go", but the retired-mechanism
// note is at :1265`.

// TestRetiredPipelineGateCensusGeneratorFamilyIsNamed pins the VOCABULARY's own
// completeness, which no corpus count can: the teardown deleted three generators
// (`78bdf05a`), and a family that keeps a member out measures the rest while
// reading like coverage.
func TestRetiredPipelineGateCensusGeneratorFamilyIsNamed(t *testing.T) {
	if len(retiredRefCensusGenerators) < retiredRefCensusGeneratorFamilyFloor {
		t.Fatalf("the census-generator vocabulary holds %d address(es), below the family floor %d (%v) — the documentation-citation teardown deleted THREE generators and a reader can paste any of them",
			len(retiredRefCensusGenerators), retiredRefCensusGeneratorFamilyFloor, retiredRefCensusGenerators)
	}
	// No member may be a PREFIX of another: the alternation is not
	// longest-first, so a prefix member would shadow the longer one and turn a
	// `citation-selector-census.go` hit into a `citation-census.go` hit — a
	// right verdict attributed to the wrong address.
	for _, short := range retiredRefCensusGenerators {
		for _, long := range retiredRefCensusGenerators {
			if short != long && strings.HasPrefix(long, short) {
				t.Fatalf("%q is a prefix of %q: the alternation would report the wrong address", short, long)
			}
		}
	}
	// And every member must be REACHABLE through BOTH patterns. A vocabulary row
	// wired into neither (or into only the man-page half) is a name in a comment.
	for _, generator := range retiredRefCensusGenerators {
		line := "$ go run " + generator + " . --check"
		if !retiredRefInvocationPattern.MatchString(line) {
			t.Fatalf("%q is in the family but the INVOCATION pattern does not match %q", generator, line)
		}
		if !retiredRefMentionPattern.MatchString(line) {
			t.Fatalf("%q is in the family but the MENTION pattern does not match %q — a man page could advertise it", generator, line)
		}
	}
}

// TestRetiredPipelineGateRedProofEveryCensusGeneratorIsJudged is the control
// that keeps the family arm from being a list of strings: each member, planted
// BARE in a normative page with no note, must be refused BY ADDRESS. A member
// that no arm ever drives is the shape that reads as coverage and measures
// nothing (Rule 38).
func TestRetiredPipelineGateRedProofEveryCensusGeneratorIsJudged(t *testing.T) {
	for _, generator := range retiredRefCensusGenerators {
		files := retiredRefFixture(
			retiredRefDocPath("teaches-"+filepath.Base(generator)+".md"),
			"<!-- doc-class: normative -->\n# A page that still teaches it\n\n$ go run "+generator+" . --check\n",
		)
		rep := scanRetiredRefs(files, nil)
		requireRetiredRefFailure(t, rep, `"`+generator+`"`, "no retired-mechanism note")
	}
}

// TestRetiredPipelineGateRedProofCitationCensusNoteAfterInvocation is the ORDER
// half for a NON-shipped member: the rule is about the reader meeting the
// warning before the command, and it must hold for every address in the family,
// not only for the one `CONFIG.md` happens to carry.
func TestRetiredPipelineGateRedProofCitationCensusNoteAfterInvocation(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("citation-note-too-late.md"),
		"<!-- doc-class: normative -->\n# Roadmap\n\n$ go run "+retiredRefTokenCitationCensus+" . --check\n\n"+retiredRefFixtureNote,
	)
	rep := scanRetiredRefs(files, nil)
	requireRetiredRefFailure(t, rep, `"`+retiredRefTokenCitationCensus+`"`, "AFTER it")
}

// TestRetiredPipelineGateGreenWhenCitationCensusAnnotated keeps that member
// usable: the shipped note ABOVE the command passes, so the arm proves
// recognition rather than an unconditional refusal.
func TestRetiredPipelineGateGreenWhenCitationCensusAnnotated(t *testing.T) {
	files := retiredRefFixture(
		retiredRefDocPath("citation-ok.md"),
		"<!-- doc-class: normative -->\n# Annotated\n\n"+retiredRefFixtureNote+"\n$ go run "+retiredRefTokenCitationCensus+" . --check\n",
	)
	rep := scanRetiredRefs(files, nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("annotated citation-census invocation was refused: %v", rep.Failures)
	}
	if rep.WithPipe != 1 || rep.Noted != 1 {
		t.Fatalf("annotated citation-census invocation was not counted on the pipeline half: %+v", rep)
	}
}

// TestRetiredPipelineGateRootDocumentsIncludeConfigMd pins the root LIST, which
// is the half no vocabulary can compensate for: `CONFIG.md` was absent from it
// while the comment above claimed it, and every invocation on that page was
// outside the gate no matter how good the recognizer was.
func TestRetiredPipelineGateRootDocumentsIncludeConfigMd(t *testing.T) {
	const configDoc = "CONFIG.md"
	for _, name := range retiredRefRootDocuments {
		if name == configDoc {
			return
		}
	}
	t.Fatalf("%s is a module-root document of record (the densest teaching surface the retired census family has) but it is absent from retiredRefRootDocuments %v — no pattern can compensate for a file the walk never offers",
		configDoc, retiredRefRootDocuments)
}

// TestRetiredPipelineGateRootWalkOffersEveryRootDocument is the same claim
// measured on the REAL tree rather than on the list: every named root document
// that exists on disk must actually come back from the gather. It is the arm
// that would have caught the defect even if the list had been right and the
// walk wrong.
func TestRetiredPipelineGateRootWalkOffersEveryRootDocument(t *testing.T) {
	root := repoRoot(t)
	files := retiredRefGather(t, root)
	offered := make(map[string]bool, len(files))
	for _, f := range files {
		offered[f.Path] = true
	}
	checked := 0
	for _, name := range retiredRefRootDocuments {
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr != nil {
			continue
		}
		checked++
		if !offered[name] {
			t.Errorf("%s exists at the module root and is named in retiredRefRootDocuments, but the gather never offered it to the scan", name)
		}
	}
	if checked == 0 {
		t.Fatalf("no named root document exists on disk — this arm measured nothing (retiredRefRootDocuments %v)", retiredRefRootDocuments)
	}
	t.Logf("%s: every one of %d named root document(s) present on disk was offered to the scan", retiredRefProvenance, checked)
}

// --- the FIFTH half: the retired two-word MEMORY sub-verb --------------------
//
// WHY THIS ARM EXISTS. Every other half reads a word at a position the others
// define: the top-level half reads the word right after `gopi`, the CLI half
// reads what follows `task`. The retired `gopi memory sync` sits one level below
// a group that is STILL LIVE (`gopi memory` dispatches search|store|backup|…),
// so a matcher keyed on "the word after `gopi`" finds `memory` — a command that
// runs — and stops. Measured (2026-11-06, task 01a1ad90-40d0): two `normative`
// pages stood at COMMAND POSITION teaching the deleted invocation and this gate
// printed a green over both, because none of its four vocabularies contained the
// phrase.
//
// The proofs below are the minimum reproduction: the invocation a reader can
// paste is REFUSED, the note placed above it licenses the record, and the
// position rule is the same one the other halves are held to — a note BELOW the
// invocation is no note at all.
func TestRetiredPipelineGateRedProofMemorySubVerbWithNoNote(t *testing.T) {
	body := "<!-- doc-class: normative -->\n# Periodic leg\n\nThe loop:\n\n" +
		"```\n" + retiredRefMemorySyncPhrase + " --peer hp --push --every 5m\n```\n"
	rep := scanRetiredRefs(retiredRefFixture(retiredRefDocPath("periodic.md"), body), nil)
	// :7, not :6 — the fence opener is line 6 and the command is its first body
	// line. Written down because an off-by-one here reads exactly like a
	// detector that fired on the wrong line.
	requireRetiredRefFailure(t, rep, retiredRefDocPath("periodic.md")+":7", "retired memory sub-verb")
	if rep.MemSyncCmd != 1 || rep.WithMemSync != 1 {
		t.Fatalf("the retired memory sub-verb was not counted in its OWN bucket: MemSyncCmd=%d WithMemSync=%d (%+v)",
			rep.MemSyncCmd, rep.WithMemSync, rep)
	}
	// And it must NOT be smuggled into the pipeline bucket: a green on that half
	// would then be reporting coverage the memory half never had.
	if rep.PipelineCmd != 0 {
		t.Fatalf("the memory sub-verb was counted as a document-pipeline hit (%d) — the buckets must stay apart",
			rep.PipelineCmd)
	}
}

func TestRetiredPipelineGateRedProofMemorySubVerbNoteAfterInvocation(t *testing.T) {
	body := "<!-- doc-class: normative -->\n# Periodic leg\n\n" +
		"$ " + retiredRefMemorySyncPhrase + " --peer hp --push " + retiredRefMemorySyncEveryFlag + " 5m\n\n" +
		"> **" + retiredRefNoteMarker + ").** that spelling is gone.\n"
	rep := scanRetiredRefs(retiredRefFixture(retiredRefDocPath("after.md"), body), nil)
	requireRetiredRefFailure(t, rep, retiredRefDocPath("after.md")+":4", "AFTER it")
}

func TestRetiredPipelineGateGreenWhenMemorySubVerbAnnotated(t *testing.T) {
	body := "<!-- doc-class: normative -->\n# Periodic leg\n\n" +
		"> **" + retiredRefNoteMarker + ").** the verb below was deleted.\n\n" +
		"```\n" + retiredRefMemorySyncPhrase + " --peer hp --push --every 5m\n```\n"
	rep := scanRetiredRefs(retiredRefFixture(retiredRefDocPath("ok.md"), body), nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("a note ABOVE the invocation must license the record; got %v", rep.Failures)
	}
	if rep.MemSyncCmd != 1 {
		t.Fatalf("the annotated invocation was not counted (MemSyncCmd=%d)", rep.MemSyncCmd)
	}
}

// TestRetiredPipelineGateMemorySubVerbMentionIsNotAnInvocation pins the other
// side of the criterion: a sentence DISCUSSING the retired spelling (the shape
// every page in this corpus now uses, since the retirement note itself has to
// name what was removed) must not be reported as teaching it.
func TestRetiredPipelineGateMemorySubVerbMentionIsNotAnInvocation(t *testing.T) {
	body := "<!-- doc-class: normative -->\n# Periodic leg\n\n" +
		"The `" + retiredRefMemorySyncPhrase + "` verb was deleted; the surviving door is a one-way copy.\n"
	rep := scanRetiredRefs(retiredRefFixture(retiredRefDocPath("mention.md"), body), nil)
	if len(rep.Failures) != 0 {
		t.Fatalf("a prose mention must not be judged as an invocation; got %v", rep.Failures)
	}
	if rep.MemSyncCmd != 0 {
		t.Fatalf("a prose mention was counted as a command-position hit (%d)", rep.MemSyncCmd)
	}
}

// TestRetiredPipelineGateRefusesAManPageMemorySubVerbAdvertisement: the man page
// is the promise surface, so a MENTION is refused there without a note above it.
func TestRetiredPipelineGateRefusesAManPageMemorySubVerbAdvertisement(t *testing.T) {
	rep := scanRetiredRefs(retiredRefFixture(
		retiredRefManPath("gopi-memory.md"),
		"# gopi memory\n\nRefresh a peer's rows on a cadence with `"+retiredRefMemorySyncPhrase+" --peer X "+retiredRefMemorySyncEveryFlag+" 5m`.\n",
	), nil)
	requireRetiredRefFailure(t, rep, retiredRefManPath("gopi-memory.md")+":3", "no retirement stated")
}
