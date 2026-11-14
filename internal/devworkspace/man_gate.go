package devworkspace

// The clone channel's MAN-SOURCE GATE — the leg that re-derives every man
// source's GENERATED regions from the binary this attempt just built, and
// refuses the delivery while any of them has drifted.
//
// THE DEFECT THIS CLOSES (row e7a09e4, measured on master 2026-10-28). A row
// edited cmd/agent's help text and did not regenerate the man sources. Nothing in
// the delivery path looked, so master carried a drifted `man/gopi-version.md` for
// two hours until an unrelated row ran the gate by accident and fixed it.
//
// WHY NOTHING CAUGHT IT, measured rather than assumed — two independent holes,
// and this leg closes the second one only:
//
//  1. `finishCleanupBranchCheckVerify` (internal/tools/task_finish_premerge.go)
//     runs the branch-scoped smoke suite — `TestManSourcesMatchTheirHelpPage`
//     among it — but its own header says it MARKS and does not block ("IT MARKS,
//     IT DOES NOT BLOCK"), and it never gates the push.
//  2. The DELIVERY leg itself (AttemptDelivery) runs the declared build and the
//     push and NO test at all. `mancensus --check` was reachable only by a human
//     running `make man-check`; no production code invoked it (measured:
//     `grep -rn 'standards/testdata/mancensus.go' --include=*.go .` was comments
//     and test fixtures only).
//
// THE FACT SOURCE IS THE BINARY, NEVER THE SOURCE TREE. The generator reads each
// page's usage rows from `gopi <verb> -h` — the surface an operator sees — so it
// measures the pair (man source, this branch's help text) and nothing else. That
// is why this leg runs AFTER the declared build: a check that read a stale
// `bin/gopi` would answer about the previous branch's binary and call a drifted
// page "up to date" — the exact false green this gate exists to prevent, so the
// binary is built first and the check never falls back to one it did not build.
//
// RULE 35 — ZERO NEW PERSISTENT DEPENDENCY. No script, no cron line, no hook, no
// resident, no config key. This is ONE MORE STEP inside the delivery attempt the
// channel already runs, and the program it invokes already shipped
// (`internal/standards/testdata/mancensus.go`, whose `--check` its own header
// documents). Nothing ticks.
//
// RULE 15 — NO SILENT PASS. Four shapes, and only ONE of them blocks:
//
//	no man sources / no generator in this tree  → an ABSTENTION naming what was
//	                                              looked for (a fixture repo, a
//	                                              nested module: the question is
//	                                              unanswerable HERE, which is not
//	                                              the answer "clean")
//	every generated region matches the binary    → silent (a healthy delivery
//	                                              stays quiet)
//	at least one source is STALE                 → a BLOCKER naming every STALE
//	                                              page, with the generator's own
//	                                              words and its own repair
//	                                              command (`make man`) — never a
//	                                              hand-written SYNOPSIS block,
//	                                              which is the drift itself
//	the generator could not run                  → an ABSTENTION with its output
//
// A stale source is the run's OWN defect (the branch moved the help text), so it
// blocks: the next agent round regenerates and commits, and the retry measures
// the same pair again.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/mansource"
)

const (
	// manGateGoBinary / manGateGoRunVerb run the generator FROM SOURCE: it is a
	// `main` package under testdata (never part of the module's build), so
	// `go run <file>` is the invocation its own header prints and the one a
	// reader reproduces by hand.
	manGateGoBinary  = "go"
	manGateGoRunVerb = "run"

	// manGateRepoArg is the repo root the generator takes: the workspace itself,
	// spelled `.` because the command runs THERE.
	manGateRepoArg = "."

	// manGateTimeout bounds one generator run: it compiles one small package
	// (stdlib + internal/mansource) and shells the binary 13 times, so the bound
	// is generous rather than tight — a wedged `go run` must not hold a delivery
	// attempt open forever.
	manGateTimeout = 5 * time.Minute

	// manGateRefusalPref opens the blocker's sentence, so a ledger reason and a
	// test both recognise it without parsing the whole paragraph. It carries no
	// channel prefix because Report() already opens with one — two prefixes read as
	// two separate failures.
	manGateRefusalPref = "the man sources do not match the binary this branch built"

	// manGateStaleExit is the generator's own status for "at least one source is
	// stale" (`if check && stale > 0 { os.Exit(1) }`). Every OTHER non-zero status
	// is the generator being unable to answer (a missing binary, an unreadable
	// source), which is an ABSTENTION and must not be reported as drift.
	manGateStaleExit = 1

	// manGateMeasuredPrefix is the wording the GREEN half opens with, and it is
	// the answer to a question this gate could not previously be asked: DID IT
	// RUN? Until this line existed a healthy delivery printed nothing at all for
	// the man half, so a run's own log could not distinguish "measured every page
	// and all matched" from "this step never executed" — measured on row
	// 01a1a3b5's delivery log (2026-11-04), which carries `census gate`, `build`
	// and `push` lines for its single attempt and NOT ONE man line, from a binary
	// that demonstrably contains this gate's text. A gate whose only output is a
	// refusal is a gate a reader cannot audit after the fact.
	manGateMeasuredPrefix = "clone-channel: man gate measured"

	// manGateComparedFloor is the smallest number of pages a GREEN verdict may be
	// based on. The generator prints one verdict line per page, so an exit 0 whose
	// output names NO page is not "every page matches" — it is "nothing was
	// compared", and the two read identically to every caller that only looks at
	// the status. The floor refuses that vacancy instead of banking it as health
	// (Rule 15), and the deviation is reported as an ABSTENTION that prints BOTH
	// numbers, so a scan that looked at nothing cannot pass as a clean one.
	manGateComparedFloor = 1
)

// manGateStep is the command the refusal reports it ran — the generator's own
// documented `--check` invocation, rendered from the shared spellings
// (Rules 13/14) rather than retyped here.
var manGateStep = strings.Join([]string{
	manGateGoBinary, manGateGoRunVerb, mansource.GeneratorRel,
	manGateRepoArg, mansource.BuiltBinaryRel, mansource.CheckFlag,
}, " ")

// manCensusGate re-derives every man source's generated regions from the binary
// this attempt just built and returns the blocker for the FIRST run that reports
// drift. A nil return means the sources are either up to date or unmeasurable —
// and the second case is printed, never swallowed.
//
// It runs AFTER the declared build (the binary is the fact source — see the file
// header) and BEFORE the push.
func manCensusGate(ctx context.Context, cfg Config, dir string) *Blocker {
	absent := manGateAbsence(dir)
	if absent != "" {
		manGateNote(cfg.Log, absent)
		return nil
	}
	out, err := runManGenerator(ctx, dir)
	if err == nil {
		// THE GREEN HALF MUST SAY WHAT IT MEASURED (this file's header, Rule 15).
		// A silent pass is indistinguishable, in a delivery log, from a step that
		// never ran; and an exit 0 that compared nothing would be banked as health
		// by every caller that only reads the status. Both facts are printed, and
		// the vacancy is an ABSTENTION rather than a pass.
		compared := manUpToDatePages(out)
		if len(compared) < manGateComparedFloor {
			manGateNote(cfg.Log, fmt.Sprintf("%s exited 0 but named no up-to-date page, so it compared "+
				"NOTHING (%d page(s) measured; output: %s) — the man sources were NOT measured",
				mansource.GeneratorRel, len(compared), tail(out)))
			return nil
		}
		manGateMeasured(cfg.Log, compared)
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != manGateStaleExit {
		// THE GENERATOR COULD NOT ANSWER: a build of its own failure, a binary it
		// cannot exec, a source it cannot read. Reported, never taken for a pass
		// (Rule 15) and never dressed as drift.
		manGateNote(cfg.Log, fmt.Sprintf("the man generator %s could not be run (%v; output: %s) "+
			"— the man sources were NOT measured", mansource.GeneratorRel, err, tail(out)))
		return nil
	}
	stale := manStalePages(out)
	if len(stale) == 0 {
		// The generator said "stale" and named no page: that is not a clean bill of
		// health and it is not a page to fix either.
		manGateNote(cfg.Log, fmt.Sprintf("the man generator exited %d but named no %s page (output: %s) "+
			"— the drift was NOT measured", manGateStaleExit, mansource.StaleToken, tail(out)))
		return nil
	}
	return &Blocker{
		Kind:  BlockerKindMan,
		Step:  manGateStep,
		Words: manRefusalWords(out, stale),
	}
}

// manGateAbsence answers whether the question can be asked of THIS tree at all,
// and says what was looked for when it cannot (Rule 15: "there is no man source
// here" and "the man sources are current" are different facts, and a fixture
// repository is the first one).
func manGateAbsence(dir string) string {
	var looked []string
	missing := func(rel string) bool {
		looked = append(looked, rel)
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		return err != nil
	}
	if missing(mansource.GeneratorRel) {
		return "this workspace carries no man generator (" + strings.Join(looked, ", ") +
			"), so the man sources are not measured here"
	}
	srcDir := filepath.Join(dir, filepath.FromSlash(mansource.SourceDirRel))
	if entries, err := os.ReadDir(srcDir); err != nil || len(entries) == 0 {
		looked = append(looked, mansource.SourceDirRel)
		return "this workspace carries no man sources under " + mansource.SourceDirRel + "/ (" +
			strings.Join(looked, ", ") + "), so there is nothing to re-derive here"
	}
	return ""
}

// runManGenerator runs the generator's documented `--check` invocation inside the
// workspace, bounded. It is the ONE place the program is executed, so the command
// a refusal reports is the command that ran (Rules 13/14).
func runManGenerator(ctx context.Context, dir string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, manGateTimeout)
	defer cancel()
	return run(cctx, dir, manGateGoBinary, manGateGoRunVerb,
		mansource.GeneratorRel, manGateRepoArg, mansource.BuiltBinaryRel, mansource.CheckFlag)
}

// manStalePages returns the man sources the generator called STALE, in the order
// it printed them. The verdict is recognised by the SHARED token (Rule 14) — a
// second spelling on either side would let a real drift read as "up to date".
func manStalePages(out string) []string {
	var pages []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, mansource.StaleToken) {
			continue
		}
		if page := manPageOnLine(line); page != "" {
			pages = append(pages, page)
		}
	}
	return pages
}

// manPageOnLine pulls the page path out of ONE generator line, which its own
// header fixes as `mancensus: <page> — <verdict> …`. A line carrying the token but
// no recognisable page is skipped here and shows up verbatim in the blocker's
// words, so nothing is lost by the parse (Rule 15).
func manPageOnLine(line string) string {
	_, after, ok := strings.Cut(line, "mancensus: ")
	if !ok {
		return ""
	}
	page, _, ok := strings.Cut(after, " — ")
	if !ok {
		return ""
	}
	page = strings.TrimSpace(page)
	if !strings.HasPrefix(page, mansource.SourceDirRel+"/") {
		return ""
	}
	return page
}

// manRefusalWords composes the blocker's words: ONE sentence the channel owns
// FIRST — it names the STALE pages, which is the fact the next round acts on and
// the one that must survive a 200-rune ledger summary — and then the generator's
// own output, verbatim (it carries its own per-page repair hint, `make man`).
//
// The order is deliberate (the decisive-line extractor's own lesson, row
// 01a18124): the generator prints a line per page, healthy ones included ("up to
// date"), so a summary that led with its raw output could quote a healthy page as
// the decision. A reader must also never be told to hand-write a SYNOPSIS block:
// that region is COPIED from `-h`, so a hand-written one is drift by construction.
func manRefusalWords(out string, stale []string) string {
	return fmt.Sprintf("%s — %s: %s (%d page(s)). Regenerate, never edit the block by hand (the SYNOPSIS "+
		"region is copied verbatim from `gopi <verb> -h`, so a hand-written one is drift by construction): "+
		"run `%s`, commit the result, and the next attempt re-measures the same pair.\n%s",
		manGateRefusalPref, mansource.StaleToken, strings.Join(stale, ", "), len(stale),
		mansource.RegenerateHint, strings.TrimSpace(out))
}

// manUpToDatePages returns the man sources the generator reported as CURRENT, in
// the order it printed them. It is the green half's counter and the reason the
// green half can be audited: the same per-page verdict lines the refusal parses
// for STALE, read the other way (Rule 13 — the recognition goes through the
// generator's own words, never a second spelling of them).
func manUpToDatePages(out string) []string {
	var pages []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, mansource.UpToDateToken) {
			continue
		}
		if page := manPageOnLine(line); page != "" {
			pages = append(pages, page)
		}
	}
	return pages
}

// manGateMeasured prints the GREEN half's sentence: how many pages were compared
// and which ones. It is deliberately always printed — the opposite of the
// abstention, which is printed only when something could not be measured — because
// "the gate ran and found N pages current" is the fact a delivery log has to
// carry for the gate to be auditable at all (see manGateMeasuredPrefix).
func manGateMeasured(w io.Writer, pages []string) {
	if w == nil || len(pages) == 0 {
		return
	}
	fmt.Fprintf(w, manGateMeasuredPrefix+" — %d page(s) compared and up to date: %s\n",
		len(pages), strings.Join(pages, ", "))
}

// manGateNotePrefix is the wording the abstention opens with, spelled once so the
// gate's own printer and the tests that assert it cannot drift apart.
const manGateNotePrefix = "clone-channel: man gate abstained"

// manGateNote prints a NON-BLOCKING sentence: the abstention half of this gate.
// It is the one place an unmeasurable tree is spoken, so "nothing to report" and
// "never measured" cannot look alike (Rule 15).
func manGateNote(w io.Writer, sentence string) {
	if w == nil || strings.TrimSpace(sentence) == "" {
		return
	}
	fmt.Fprintf(w, manGateNotePrefix+" — %s\n", sentence)
}
