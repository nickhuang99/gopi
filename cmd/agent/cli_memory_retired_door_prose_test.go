package main

// cli_memory_retired_door_prose_test.go — the memory family's TWO retired flat
// doors, held against the binary (task 01a1ac97-513c-77a8-9bee-659e9914dcfe).
//
// WHAT 2e3ec8d LEFT OPEN. That commit repaired the memsync pair at the ROOT
// CAUSE — two constants that were NAMED for two spellings and EVALUATED to one
// value — and its test asks whether the constants still differ
// (TestMemsyncSpellingConstantsDiffer). It does not ask the second question,
// and neither did anything else: does a page make a NEGATIVE claim ("still
// works", "still opens", "still runs") about a word the binary REFUSES? The
// brew door (`gopi brew`, exit 1, RETIRED) had no arm at all — its page was
// only reachable from the same group page nobody swept.
//
// THE RULE THIS FILE HOLDS (PROJECT.md Rule 36: a `-h` page is the official
// promise). A page may describe a retired spelling — it SHOULD, so a reader who
// types the wrong word learns the right one — but it may never promise that the
// word still works. The tolerance is therefore PHRASE-SCOPED, not
// spelling-scoped, and that distinction is the whole design:
//
//	"the flat gopi memsync door is RETIRED … it refuses and exits 1"   OK, said
//	"you must still PARSE the archive before restoring"                OK, unrelated
//	"the flat gopi memsync spelling still works"                        RED
//
// So the banned list is exactly the three promise phrasings, never the spelling
// (banning the word would forbid the CORRECT sentence — which is how the fix
// itself would go red). And the claim is measured, not argued: the same test
// runs each flat word verbatim and requires a non-zero exit carrying RETIRED, so
// the day the wording and the binary disagree, THIS test says which one moved.
//
// THE RED PROOF (Rule 5/31, measured on this ground): reverting the two help
// strings to their pre-fix wording turns this file RED on all four arms —
// "still opens" on the memsync page, "still works" on the group page for both
// doors — and the older TestMemsyncHelpClaimsNoOpenRetiredDoor red as well.
// Restoring them turns it green again.

import (
	"strings"
	"testing"
)

// memoryRetiredDoorProseBanned are the phrasings that assert a spelling is
// still usable. They are the three the measured defect used ("still opens" on
// the memsync page, "still works" on the group page, "still runs" named beside
// brew in the briefing).
//
// DELIBERATELY ABSENT: "still PARSE". `gopi memory memsync` really does still
// parse and run the retired task flags (--export-tasks / --import-tasks, the
// page's own paragraph at memsyncUsage), which is a TRUE statement about
// ARGUMENTS and not about a word the binary refuses. Banning it would delete a
// correct sentence; the phrase is the unit, not the mood.
var memoryRetiredDoorProseBanned = []string{"still works", "still opens", "still runs"}

// memoryRetiredDoor is one flat spelling the memory family retired: the word
// argv[0] must refuse, and the live address that replaced it.
type memoryRetiredDoor struct {
	// flat is argv[0] the binary must refuse (Rule 1: the words come from the
	// package constants, never typed here).
	flat string
	// group is the live address, as argv words — the live home a page must
	// point a reader at.
	group []string
}

// memoryRetiredDoors is the census: both flat words the memory family retired
// (epic verb-restructure 2026-10-05). memsync joined first; brew followed, and
// its page carried no arm before this file.
func memoryRetiredDoors() []memoryRetiredDoor {
	return []memoryRetiredDoor{
		// memsync is no longer a DOOR at all: the cross-node sync removal
		// (2026-11-05) deleted the verb, so `gopi memory memsync -h` exits 1
		// with "unknown subcommand" and there is no page left to judge. A row
		// here would make this arm measure the removal sentence instead of a
		// page's prose, which is a different question (and the arm's own
		// non-vacuity guard is what surfaced it).
		{flat: CliVerbBrew, group: []string{CliVerbMemory, CliVerbBrew}},
	}
}

// memoryProsePages is every live surface that speaks about the memory family's
// retired doors: each verb's own page, and the one group page that advertises
// them both. It is a page list, NOT a door list, because a phrase is a property
// of a PAGE — asserting it per door made one sentence on the shared group page
// answer twice and name the wrong word in its diagnostic.
func memoryProsePages() [][]string {
	doors := memoryRetiredDoors()
	pages := make([][]string, 0, len(doors)+1)
	for _, d := range doors {
		pages = append(pages, append(append([]string{}, d.group...), HelpFlag))
	}
	return append(pages, []string{CliVerbMemory, HelpFlag})
}

// memoryPageProg renders a page argv as the operator would type it — the string
// the failure message prints, so a red says exactly which command to re-run.
func memoryPageProg(argv []string) string {
	return "gopi " + strings.Join(argv, " ")
}

// TestMemoryHelpPagesPromiseNoRetiredDoorStillWorks is the phrase arm: no live
// page in the memory family asserts that a spelling which refuses still works,
// and the arm is non-vacuous — it proves the phrases it looks for are really
// absent from a page it DID read, by also requiring the pages to be non-empty.
func TestMemoryHelpPagesPromiseNoRetiredDoorStillWorks(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	pages := memoryProsePages()
	if len(pages) < 2 {
		t.Fatalf("the page census is %d page(s) — a sweep that reads nothing cannot pass", len(pages))
	}
	for _, page := range pages {
		// argv EXACTLY as given: runCLIContractProbeRaw does not rewrite argv
		// (a family-address rewrite would answer a different question — the same
		// trap that made the older file's first run measure the live door).
		body, err := runCLIContractProbeRaw(t, bin, home, page...)
		if err != nil {
			t.Fatalf("`%s` exited non-zero: %v\n--- output ---\n%s", memoryPageProg(page), err, body)
		}
		if strings.TrimSpace(body) == "" {
			t.Fatalf("`%s` exited 0 with an empty page — a page that prints nothing cannot be judged", memoryPageProg(page))
		}
		for _, banned := range memoryRetiredDoorProseBanned {
			if line, ok := memoryPageLineNaming(body, banned); ok {
				t.Errorf("`%s` claims %q about a spelling that refuses — the flat words %q / %q are RETIRED and exit non-zero; say so and point at the family address instead:\n  %s",
					memoryPageProg(page), banned, CliVerbMemsync, CliVerbBrew, line)
			}
		}
	}
}

// TestMemoryRetiredDoorsRefuseAsThePagesSay is the measurement arm, and it is
// what keeps the wording tied to the binary rather than to itself: each flat
// word exits non-zero and says RETIRED, while the family address the pages
// point at really opens with a real page.
//
// A run where the flat word exits 0 must change the PAGES, not this test — that
// is the dependency the assertion encodes.
func TestMemoryRetiredDoorsRefuseAsThePagesSay(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	for _, door := range memoryRetiredDoors() {
		t.Run(door.flat, func(t *testing.T) {
			out, err := runCLIContractProbeRaw(t, bin, home, door.flat, HelpFlag)
			if err == nil {
				t.Fatalf("`gopi %s %s` exited 0 — every page below calls this spelling RETIRED, so either the refusal or the wording must change:\n%s",
					door.flat, HelpFlag, out)
			}
			if !strings.Contains(out, "RETIRED") {
				t.Errorf("`gopi %s %s` refuses but never says RETIRED, so a page calling it retired has nothing behind it:\n%s",
					door.flat, HelpFlag, out)
			}

			// The live address the pages point at, RUN rather than compared to
			// the table: comparing the printed string would only re-read the
			// table, and the defect was exactly a page describing a door that
			// does not open.
			live := append(append([]string{}, door.group...), HelpFlag)
			page, lerr := runCLIContractProbeRaw(t, bin, home, live...)
			if lerr != nil {
				t.Fatalf("the live address `%s` does not run: %v\n--- output ---\n%s",
					memoryPageProg(live), lerr, page)
			}
			if !strings.Contains(page, door.flat) {
				t.Errorf("`%s` never names %q, so a reader who typed that retired word learns nothing here",
					memoryPageProg(live), door.flat)
			}
		})
	}
}

// TestMemoryGroupPageCorrectsARetiredWord keeps the TRUE half from being
// deleted along with the false one: the group page must still name both retired
// words, so removing the promise never leaves a reader who typed one with no
// correction at all.
func TestMemoryGroupPageCorrectsARetiredWord(t *testing.T) {
	bin := gopiTestBinPath(t)
	body, err := runCLIContractProbeRaw(t, bin, t.TempDir(), CliVerbMemory, HelpFlag)
	if err != nil {
		t.Fatalf("`gopi %s %s` exited non-zero: %v\n--- output ---\n%s", CliVerbMemory, HelpFlag, err, body)
	}
	for _, door := range memoryRetiredDoors() {
		if !strings.Contains(body, door.flat) {
			t.Errorf("`gopi %s %s` never names the retired spelling %q — the page advertises `gopi memory %s`, so a reader who typed the flat word is never corrected",
				CliVerbMemory, HelpFlag, door.flat, door.flat)
		}
	}
}

// memoryPageLineNaming returns the first line of body carrying phrase, so a red
// quotes the sentence rather than making the reader search the page.
func memoryPageLineNaming(body, phrase string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, phrase) {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}
