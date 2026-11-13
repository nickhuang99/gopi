// cli_web_page_scenario_drivers_test.go — the drivers for every page scenario
// group that had none, plus the guard that keeps the gap from reopening.
//
// Why this file exists (two defects, measured at c628e1fc):
//
//  1. cli_web_page_scenarios.js carried 17 scenarios and only 7 of them were ever
//     executed by `go test ./cmd/agent/` — the other 10 needed `--only` from a Go
//     test and no Go test asked for them. A scenario nobody executes is a check
//     that CANNOT FAIL: putting back the defect each one guards left `go test`
//     green. TestPageScenariosDriveTheUnguardedGroups is the driver for those 10.
//
//  2. The gap was not a one-off. Scenario-only commits kept landing with no
//     driver beside them (4ce71e252 📷, 53619d99e mic, be00e79f2 voice streaming,
//     e1c344e2a R6) while the two early ones (964b2404b, 827fe4e7c) shipped both
//     halves. TestEveryPageScenarioHasADriver is the GUARD: it reads the scenario
//     file and the package's own `--only` selectors and fails when a scenario has
//     no driver, so the 18th scenario cannot repeat the hole.
//
// The selectors and path constants are reused from the existing gates
// (pageHarnessPath / menuNavGoFile / menuHarnessPass / menuHarnessFail /
// lookPathNode / runNode — Rule 13/14: this file declares NO second copy of
// them). The one name it does declare is pageScenariosSource: the single
// Go-side declaration of the scenario file's name.
//
// Nothing here edits the scenario file or makes the harness run every scenario
// by default — one red check would drown the rest. Driving belongs on the Go side.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The scenario-name substrings the harness is asked to run alone. The harness
// matches with `name.indexOf(selector) >= 0`, so each constant selects its whole
// group. They are CONSTANTS (Rule 1/13/14): an inline string here and in the
// scenario file could drift apart silently and the drift would read as "green"
// while running nothing — the harness only refuses an EMPTY match.
const (
	b12QueueScenarioName          = "B12 composer stays usable"
	b12WithdrawScenarioName       = "B12 withdraw takes a WAITING message"
	b12SteerScenarioName          = "B12 Alt+Enter STEERS"
	followBottomScenarioName      = "B3 follow-bottom"
	markdownScenarioName          = "B8 markdown"
	detectCoreScenarioName        = "B9 the detect core"
	replayScenarioName            = "B5 replay"
	resetQueuedNoticeScenarioName = "B1/B2"
	identityBannerScenarioName    = "identity banner"
	voiceLiveScenarioName         = "voice live"
	voiceEscKeepScenarioName      = "voice esc"
	voiceEscArbiScenarioName      = "voice esc arbi"
	voiceEscRungsScenarioName     = "voice esc rungs"
	voiceEscLayerScenarioName     = "voice esc layer"
	imageBudgetScenarioName       = "image budget"
	themeCycleScenarioName        = "B13 theme"
)

// pageScenariosSource is the scenario file the harness runs and the guard below
// reads. It is declared ONCE, here, and is not a capture of any path another
// file already names (Rule 13).
const pageScenariosSource = "cli_web_page_scenarios.js"

// untracedScenarioGroup is one row of the driver table: the selector to hand the
// harness, and why that group is worth executing at all.
type untracedScenarioGroup struct {
	selector string
	why      string
}

// untracedScenarioGroups is the table. Adding a scenario to the page's
// behaviour means adding one row here — that is the whole cost of keeping the
// group under `go test`.
func untracedScenarioGroups() []untracedScenarioGroup {
	return []untracedScenarioGroup{
		{
			selector: b12QueueScenarioName,
			why:      "a message typed during a turn must not be dropped and the textarea must not be disabled: the mid-turn message has to become a VISIBLE queued entry (runTurn/enqueueMessage/drainQueue).",
		},
		{
			selector: b12WithdrawScenarioName,
			why:      "the withdrawal had NO scenario: the harness already carried a queueCancels bucket and the page already carried withdrawQueued/撤销, so a page that rendered a button posting nothing — or cancelled with a re-derived id — would satisfy every count in the group above. This is the only assertion that the waiting message can be taken back and that its turn then NEVER runs.",
		},
		{
			selector: b12SteerScenarioName,
			why:      "Alt+Enter posts the shared steering sentinel and returns immediately instead of waiting for the queue to drain — the sentinel is stripped server-side, and a wrong one would ship the operator's text as a literal message.",
		},
		{
			selector: followBottomScenarioName,
			why:      "follow-bottom is CONDITIONAL: it must only yank when the reader was already near the bottom, and the jump-to-latest affordance must appear when they were not (an unconditional yank steals the scroll position while reading).",
		},
		{
			selector: markdownScenarioName,
			why:      "the markdown renderer is textContent-only (a <script> payload must render as inert text), streams incrementally, and bounds an unterminated fence — an innerHTML shortcut here is an XSS hole that no other Go test reads.",
		},
		{
			selector: detectCoreScenarioName,
			why:      "the slash/@ detection core is pure (it takes the text it is handed and reads no DOM), which is what lets the menu arbitrate the keyboard in one place; the B9 arrow-nav driver next door covers only the keydown/keyup pair.",
		},
		{
			selector: replayScenarioName,
			why:      "a replayed transcript rebuilds tool cards from the server's /api/transcript rows, with the harness's own `[tool_use id: …]` header stripped and textContent-only rendering.",
		},
		{
			selector: resetQueuedNoticeScenarioName,
			why:      "the reset notice and the queued notice are the SHARED wire strings the server writes — the page must not invent its own copy, or the operator reads two different sentences for one event.",
		},
		{
			selector: identityBannerScenarioName,
			why:      "the banner asks the SERVER which session/model it is driving instead of guessing client-side; a page that hard-codes an identity lies about the session it is writing into.",
		},
		{
			selector: voiceLiveScenarioName,
			why:      "the STREAMING voice route draws draft frames while the operator is still speaking and falls back to start/stop when the server refuses — the [voice:] driver next door covers the one-shot route only and never enters this path.",
		},
		{
			selector: voiceEscKeepScenarioName,
			why:      "ESCAPE during a recording must STOP AND KEEP what was recognised (caret at the end) and never take the cancel route: the key an operator presses to start proof-reading an ASR result used to delete that result, irreversibly. Shift+Escape is the discard, and Ctrl+U clears the whole composer unannounced.",
		},
		{
			selector: voiceEscArbiScenarioName,
			why:      "THE SETTLE WINDOW: the recording has stopped, the engine's sentence is already in the composer, and no turn is running. Escape used to fall through every branch there and do nothing at all. Now plain Escape keeps the sentence and puts the caret at the end, Shift+Escape takes exactly that sentence back out, and the discard is SPOKEN (a discard with no trace is indistinguishable from a broken key). The scenario also drives the case that outranks everything: an EDITED sentence must REFUSE to be discarded, because deleting it would delete the operator's own typing.",
		},
		{
			selector: voiceEscRungsScenarioName,
			why:      "THE FOUR RUNGS ARE DISJOINT AND ORDERED: menu / recording / turn / settled-sentence, decided by ONE arbiter both key listeners read. A rung that can co-fire with another is an else-if chain by another name, and the settle window co-firing with an open recording is exactly the gap; this scenario holds a settled sentence and a live recording at the same time and requires the recording rung to win.",
		},
		{
			selector: voiceEscLayerScenarioName,
			why:      "A VISIBLE VOICE LAYER OUTRANKS A STREAMING TURN (operator's report, 2026-11-09): with a settled sentence on screen and a response still streaming, Escape aborted the RESPONSE and left the layer exactly where it was — the key addressed everything except the one visible thing it was aimed at. The arbiter now asks micVoiceLayerUp() (the composer still holds what voice inserted) BEFORE the turn, and the same scenario measures the other half: the SAME record with the composer no longer matching it is a RESIDUAL sentence, and it must NOT steal the interrupt. Both halves are unreadable from Go — the state is a DOM value compared against the page's own record — and the only instrument for 'was the turn actually aborted' is api.aborted(), the page's own AbortController signal.",
		},
		{
			selector: themeCycleScenarioName,
			why:      "THE THEME IS THREE STATES, NOT TWO: with nothing stored the page FOLLOWS the system (and a matchMedia change re-applies without a reload), a stored preference wins over a later system flip, and the header button CYCLES light → dark → auto so the follow state is reachable by clicking instead of by knowing about localStorage. A storage read that throws (a private window) must degrade to following the system, never break the page. No Go test can read any of this: the state lives in a DOM attribute and a browser store, so the scenario drives the shipped page's own themeApply/click path.",
		},
		{
			selector: imageBudgetScenarioName,
			why:      "the page's base64 cap (MAX_B64) must be DERIVED from the decoded-byte ceiling (MAX_IMAGE_BYTES) by the same 4/3 expansion the server uses — the cross-language equality gate: a second typed number here silently disagrees with the server's body cap.",
		},
	}
}

// subtestName turns a scenario selector into a t.Run name. A selector may carry
// '/' ('B1/B2 …'), which t.Run reads as a subtest SEPARATOR — so the run name
// replaces it, keeping each group exactly one level deep under the parent.
func subtestName(selector string) string {
	return strings.NewReplacer("/", "-", " ", "_").Replace(selector)
}

// TestPageScenariosDriveTheUnguardedGroups runs every scenario group that no
// other Go test drives, one subtest per group, and requires each to pass.
//
// Each subtest asserts the harness's own tally (menuHarnessPass present,
// menuHarnessFail absent) AND that the run succeeded. The bare "FAIL" prefix
// catches both a red check and the harness's empty-match refusal (`--only`
// matching nothing exits 1 with `FAIL: no scenario name contains …`), so a
// selector that rotted away cannot read as a pass.
func TestPageScenariosDriveTheUnguardedGroups(t *testing.T) {
	node, err := lookPathNode()
	if err != nil {
		t.Skip("skipping the page-scenario drivers: no `node` on PATH (every scenario runs the page's OWN JS under node, so there is no way to drive them without one)")
	}
	groups := untracedScenarioGroups()
	if len(groups) == 0 {
		t.Fatal("the driver table is empty — this test would pass while driving nothing")
	}
	for _, g := range groups {
		g := g
		t.Run(subtestName(g.selector), func(t *testing.T) {
			out, runErr := runNode(t, node, pageHarnessPath, "--only", g.selector, "--page", menuNavGoFile)
			text := string(out)
			if runErr != nil || !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) {
				t.Fatalf("the %q scenario group did not pass (err=%v):\n%s\nwhy this group is driven: %s",
					g.selector, runErr, text, g.why)
			}
			t.Logf("%q — %s", g.selector, g.why)
			t.Logf("harness output:\n%s", text)
		})
	}
}

// Anti-vacuum floors: a scan that found nothing must not pass as a clean
// corpus. Both were MEASURED at this revision (17 scenarios, 14 selectors) and
// are deliberately below those numbers — they exist to catch a broken scan, not
// to freeze the counts (every selector is separately required to match a real
// scenario, so a stale one is already red).
const (
	pageScenarioScanFloor = 17
	pageSelectorScanFloor = 10
)

var (
	// scenarioNameRe reads a scenario's name out of the shipped scenario file.
	scenarioNameRe = regexp.MustCompile(`(?m)^    name: '((?:[^'\\]|\\.)*)',$`)
	// selectorConstRe reads a declared selector: the `*ScenarioName` convention
	// the four pre-existing drivers already follow. The `const` keyword is
	// optional because a single-constant declaration is written on one line
	// (`const domOrderScenarioName = "…"`) while a block is tab-indented — a
	// pattern that required one shape would silently miss the other, and the
	// scenarios behind the missed ones would read as undriven.
	selectorConstRe = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Za-z_][A-Za-z0-9_]*ScenarioName)\s*=\s*"([^"]*)"`)
	// selectorUseRe reads a selector ACTUALLY HANDED TO `--only`, in either
	// shape: a bare literal, or a named constant. The assignment forms are
	// deliberately excluded — the identifier is followed by ',' or ')' only, so
	// a LOOP VARIABLE (`"--only", g.selector`) is not mistaken for a selector
	// name and cannot fail the scan on an identifier it never declared.
	selectorUseRe = regexp.MustCompile(`"--only",\s*(?:"([^"]*)"|([A-Za-z_][A-Za-z0-9_]*)\s*[,)])`)
	// tableRowRe reads a selector used as a row of the driver table. A row IS a
	// use: it is the value the loop above hands to `--only`. Without this shape
	// the guard measured DECLARATIONS, and its own red proof passed while a row
	// had been deleted (measured: the constant stayed declared, nothing ran it).
	tableRowRe = regexp.MustCompile(`(?m)^\s*selector:\s*([A-Za-z_][A-Za-z0-9_]*)\s*,`)
)

// TestEveryPageScenarioHasADriver is the GUARD: every scenario in the shipped
// file must be selected by some `--only` selector this package declares, so a
// scenario added without a driver fails HERE instead of passing forever.
//
// It fails in BOTH directions, because either one alone is a false green:
//   - a scenario no selector matches → "no driver" (the defect this epic fixes);
//   - a selector that matches no scenario → a dead driver, which would make the
//     harness print `FAIL: no scenario name contains …` only when something
//     finally ran it.
//
// It reads the SCENARIO FILE (the names) and the package's own *_test.go files
// (the selectors). It never writes anything, and it does not edit the scenarios.
func TestEveryPageScenarioHasADriver(t *testing.T) {
	scenarios, err := pageScenarioNames(t)
	if err != nil {
		t.Fatalf("%v", err)
	}
	selectors, err := pageScenarioSelectors(t)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if len(scenarios) < pageScenarioScanFloor {
		t.Fatalf("read only %d scenario name(s) from %s (floor %d) — the scan found nothing useful, so it cannot judge coverage",
			len(scenarios), pageScenariosSource, pageScenarioScanFloor)
	}
	if len(selectors) < pageSelectorScanFloor {
		t.Fatalf("found only %d `--only` selector(s) in this package (floor %d) — the scan is broken, so every scenario would read as undriven",
			len(selectors), pageSelectorScanFloor)
	}

	var undriven []string
	for _, name := range scenarios {
		driven := false
		for _, sel := range selectors {
			if strings.Contains(name, sel) {
				driven = true
				break
			}
		}
		if !driven {
			undriven = append(undriven, name)
		}
	}

	var dead []string
	for _, sel := range selectors {
		matched := false
		for _, name := range scenarios {
			if strings.Contains(name, sel) {
				matched = true
				break
			}
		}
		if !matched {
			dead = append(dead, sel)
		}
	}

	if len(undriven) > 0 {
		t.Errorf("%d of %d page scenario(s) have NO Go driver — `go test` never executes them, so a revert of the fix each one guards stays green.\nAdd the group to untracedScenarioGroups() (a selector constant + one row) and, if it is a new capability, a `*ScenarioName` constant the table uses:\n%s",
			len(undriven), len(scenarios), strings.Join(undriven, "\n"))
	}
	if len(dead) > 0 {
		t.Errorf("%d `--only` selector(s) match NO scenario — a driver that runs nothing (the harness would exit 1 with `FAIL: no scenario name contains …` the moment it were executed):\n%s",
			len(dead), strings.Join(dead, "\n"))
	}
	t.Logf("scenarios=%d selectors=%d undriven=%d dead-selectors=%d",
		len(scenarios), len(selectors), len(undriven), len(dead))
}

// pageScenarioNames reads the scenario names out of the shipped scenario file.
// The file is a sibling of this package's directory, so the read is relative to
// the package dir — the cwd `go test` always uses.
func pageScenarioNames(t *testing.T) ([]string, error) {
	t.Helper()
	src, err := os.ReadFile(pageScenariosSource)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pageScenariosSource, err)
	}
	var names []string
	for _, m := range scenarioNameRe.FindAllStringSubmatch(string(src), -1) {
		names = append(names, strings.ReplaceAll(m[1], `\'`, `'`))
	}
	return names, nil
}

// pageScenarioSelectors collects every selector this package ACTUALLY EXECUTES:
// a bare literal handed to `--only`, a constant handed to `--only`, and a
// constant sitting in a row of the driver table (a row is a use — it is the
// value the loop passes on).
//
// It measures USES and never bare declarations, because a declared constant that
// nothing executes is a driver in name only: the guard's own red proof caught
// exactly that, reporting "undriven=0" with a table row deleted.
func pageScenarioSelectors(t *testing.T) ([]string, error) {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no *_test.go in this package: the selector scan found nothing to read")
	}
	// Declarations first: a `--only` argument may be a CONSTANT, so the name
	// table has to exist before uses resolve. One name declared twice with
	// different values is refused rather than guessed — neither choice would be
	// evidence of what actually runs.
	nameValue := map[string]string{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		for _, m := range selectorConstRe.FindAllStringSubmatch(string(src), -1) {
			name, val := m[1], m[2]
			if prev, dup := nameValue[name]; dup && prev != val {
				return nil, fmt.Errorf("%s is declared with two different values (%q and %q), so a `--only` use of it cannot be resolved", name, prev, val)
			}
			nameValue[name] = val
		}
	}

	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	// A name resolves through its declaration; an undeclared one is refused, not
	// skipped — skipping it would silently drop whatever it was driving.
	resolve := func(name string) error {
		val, ok := nameValue[name]
		if !ok {
			return fmt.Errorf("%s is handed to `--only` (or sits in the driver table) but no `%s = \"…\"` declaration was found, so the guard cannot tell what it selects", name, name)
		}
		add(val)
		return nil
	}

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		text := string(src)
		for _, m := range selectorUseRe.FindAllStringSubmatch(text, -1) {
			if m[1] != "" {
				add(m[1])
				continue
			}
			if err := resolve(m[2]); err != nil {
				return nil, err
			}
		}
		for _, m := range tableRowRe.FindAllStringSubmatch(text, -1) {
			if err := resolve(m[1]); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
