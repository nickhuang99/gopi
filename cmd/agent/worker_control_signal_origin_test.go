package main

// worker_control_signal_origin_test.go — the CHECK for WHERE a `gopi worker
// --stop` process gets its process-wide SIGINT handler from.
//
// WHY A SOURCE CHECK AND NOT A RUNTIME ONE. The property under test is an
// ORDER between two statements that are BOTH true at runtime and both invisible
// in the signal mask: the subcommand chain returns from main() long before the
// agent path is reached, so the handler registration that covers `worker
// --stop` is the one that runs BEFORE the chain — `signalrec.Install("")` at
// main.go. A runtime observation cannot separate "the recorder installed it"
// from "something later did", and the earlier claim that the agent path's
// `signal.Notify` (main.go, the bare-prompt branch) covered this verb was an
// ATTRIBUTION error precisely because no test pinned the reachability. This
// file pins the two facts that make the attribution decidable, in the repo's
// own family of source-shape gates.
//
// WHAT IT CHECKS (two arms, both named on failure):
//
//  1. `signalrec.Install` is called BEFORE the subcommand chain's `return`, so
//     every verb that leaves main() at that chain — `worker` among them — runs
//     with the recorder's handlers already installed. Moving the call after the
//     chain makes the guard red by name.
//  2. The worker control path installs and CLAIMS its own handlers:
//     `workerControlCommand` calls `installWorkerControlSignals`, and that
//     function both registers (signal.Notify) and claims
//     (signalrec.ClaimSignalHandlers) before it returns its uninstaller.
//     Dropping either half makes the guard red.
//
// WHAT IT DOES NOT CHECK (honesty over coverage): it reads SOURCE TEXT, so it
// proves the statements are present and ordered, never that they executed; a
// registration reached through another name is invisible to it; and it says
// nothing about what the handlers DO — the runtime behaviour is
// cmd/agent/worker_control_sigint_e2e_test.go's subject, deliberately not
// duplicated here.
//
// ARM 3 EXISTS BECAUSE ARMS 1–2 WERE NOT ENOUGH (measured 2026-10-16, this
// task). Arms 1–2 pin the ORDER of two statements; they say nothing about the
// LINE NUMBERS the two devdocs print as their evidence. Between the docs being
// drafted and this commit the chain's head moved from 628 to 631 (an unrelated
// merge landed in the worktree), and NOTHING went red: order was intact, the
// prose was off by three lines, and a document whose entire claim is "line X is
// unreachable from here" would have shipped citing the wrong X. So arm 3 pins
// the citations themselves — every line number the two devdocs print for a
// `main.go` position must equal the line the SOURCE actually puts that anchor
// on, and every stated DELTA (how many lines apart two anchors are) must equal
// the measured one. A doc that drifts now fails here by name instead of
// misleading the next reader.
//
// WHY ARM 3 WAS REBUILT AGAIN (2026-10-27, third drift in eleven days). Pinning
// the docs' absolute line numbers turned the guard into a tripwire for every
// unrelated edit above main(): 657 → 667 → 673, and each time the "fix" was to
// retype the number in the prose, which is a red that teaches nothing and
// recurs by construction. The claim the documents actually make is an ORDER
// plus a symbol ("the recorder's registration is armed before the chain that
// returns for `worker`, and the bare-prompt `signal.Notify(sigCh, …)` is
// unreachable from that verb"), and only the ORDER is durable. So the contract
// was inverted:
//
//   - the documents resolve their anchors BY SYMBOL and hand the reader the
//     command that resolves them (`grep -n …`); arm 3 requires those spellings
//     to still be there, so the symbol route cannot be quietly dropped;
//   - a line number that a document still prints is CHECKED, exactly as before
//     (a citation is only accepted if the source puts the anchor in its window
//     on that line) — so reintroducing the old habit is caught immediately and
//     in the doc that did it;
//   - the DELTAS the documents argue from ("eighteen lines before", "250 lines
//     below that return") stay asserted, because a delta survives an insertion
//     above both anchors and a doc whose argument is a gap must not drift.
//
// A document that prints no number at all is therefore GREEN, and that is the
// intended outcome, not a vacuous pass: the symbol floor and the delta floor
// give this arm something real to read in every run. The third document below
// (`signalrec-claim-vs-reraise.md`) was added here because it carried the same
// stale `main.go` citation with NO guard pointing at it — it drifted silently
// through all three of the repairs above (measured 2026-10-27).

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	// The two files the two arms read, by name (Rule 1b: the name IS the
	// matched value).
	wcsMainFile    = "main.go"
	wcsControlFile = "cli_worker_control.go"

	// Arm 1's anchors: the recorder's own call, the subcommand chain's head as
	// it is literally spelled, and the word inside that chain that makes
	// `gopi worker` leave main() there.
	wcsInstallCall    = `signalrec.Install(`
	wcsChainHead      = "if isConfigSubcommand(dispatchArgs)"
	wcsChainWorkerArm = "isWorkerSubcommand(dispatchArgs)"

	// Arm 2's anchors.
	wcsControlCommandFunc = "func workerControlCommand("
	wcsInstallerFunc      = "func installWorkerControlSignals("
	wcsInstallerCall      = "installWorkerControlSignals()"
	wcsNotifyCall         = "signal.Notify("
	wcsClaimCall          = "signalrec.ClaimSignalHandlers()"
)

// wcsSourceFloor is the minimum size of each file the scan reads. It is the
// guard's READ-FLOOR: a scan that read an empty file, or the wrong file, would
// find no anchors and could pass by finding nothing. Both files are far larger
// than this, so the floor only ever fires on a broken read.
const wcsSourceFloor = 4096

func wcsReadSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — this guard reads the package's own sources and cannot run without them", path, err)
	}
	if len(b) < wcsSourceFloor {
		t.Fatalf("%s is %d bytes, below the %d-byte read floor — the scan would be vacuous", path, len(b), wcsSourceFloor)
	}
	return string(b)
}

// wcsIndexOfLine returns the 0-based line number of the FIRST line containing
// needle, or -1. Comments are NOT skipped: both anchors are code spellings whose
// prose mentions carry a different shape (a comma or no paren), so a comment
// cannot shadow them — and if one ever did, the guard failing loudly is the
// right direction (Rule 15).
func wcsIndexOfLine(src, needle string) int {
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

// TestRecorderInstallsBeforeTheSubcommandChain is arm 1: the process-wide
// signal recorder must be armed before the chain that returns for `worker`.
func TestRecorderInstallsBeforeTheSubcommandChain(t *testing.T) {
	src := wcsReadSource(t, wcsMainFile)

	install := wcsIndexOfLine(src, wcsInstallCall)
	if install < 0 {
		t.Fatalf("%s carries no %q call — the source anchor moved; this guard must be updated deliberately, not silently", wcsMainFile, wcsInstallCall)
	}
	chain := wcsIndexOfLine(src, wcsChainHead)
	if chain < 0 {
		t.Fatalf("%s carries no subcommand chain head (%q)", wcsMainFile, wcsChainHead)
	}
	// The chain line must still route the worker verb — otherwise the premise
	// ("`worker` leaves main() at that chain") no longer holds and this guard is
	// checking the wrong thing.
	if !strings.Contains(strings.Split(src, "\n")[chain], wcsChainWorkerArm) {
		t.Fatalf("%s:%d is the subcommand chain but no longer routes %q — `gopi worker` may no longer return from main() there, so the attribution this guard pins has to be re-derived", wcsMainFile, chain+1, wcsChainWorkerArm)
	}
	// The chain's body is its `return`, on the next non-empty line.
	body := -1
	lines := strings.Split(src, "\n")
	for i := chain + 1; i < len(lines) && i < chain+5; i++ {
		if strings.TrimSpace(lines[i]) == "return" {
			body = i
			break
		}
	}
	if body < 0 {
		t.Fatalf("%s:%d is the subcommand chain but no bare `return` follows it within 5 lines", wcsMainFile, chain+1)
	}

	if install > body {
		t.Fatalf("THE RECORDER IS ARMED TOO LATE: %s calls %q at line %d, but the subcommand chain returns at line %d — every verb that leaves main() there (including `gopi worker --stop`) would run with NO process-wide SIGINT/SIGTERM handler from the recorder, which is exactly the attribution this guard exists to keep true",
			wcsMainFile, wcsInstallCall, install+1, body+1)
	}
	t.Logf("arm 1: %s:%d calls %q; the subcommand chain returns at %s:%d — the recorder is armed first",
		wcsMainFile, install+1, wcsInstallCall, wcsMainFile, body+1)
}

// TestWorkerControlPathInstallsAndClaimsItsOwnSignals is arm 2: the control
// verb's own handler is installed AND claimed, from the function that runs it.
func TestWorkerControlPathInstallsAndClaimsItsOwnSignals(t *testing.T) {
	src := wcsReadSource(t, wcsControlFile)

	command := wcsIndexOfLine(src, wcsControlCommandFunc)
	if command < 0 {
		t.Fatalf("%s carries no %q — the control entry point moved or was renamed", wcsControlFile, wcsControlCommandFunc)
	}
	installer := wcsIndexOfLine(src, wcsInstallerFunc)
	if installer < 0 {
		t.Fatalf("%s carries no %q — the control path no longer has its own handler installer", wcsControlFile, wcsInstallerFunc)
	}
	call := wcsIndexOfLine(src, wcsInstallerCall)
	if call < 0 {
		t.Fatalf("%s never calls %q — the control command would wait on a signal disposition it never chose again (the defect this file pins)", wcsControlFile, wcsInstallerCall)
	}
	if call < command || call > installer {
		t.Fatalf("%s:%d calls %q, but that call sits outside the run of source between %q (line %d) and its definition (line %d) — the installer must be invoked BY the control command that needs it",
			wcsControlFile, call+1, wcsInstallerCall, wcsControlCommandFunc, command+1, installer+1)
	}

	// The installer's own body: register, then claim. Both spellings must be
	// INSIDE it (after its `func` line), never merely somewhere in the file.
	bodyLines := strings.Split(src, "\n")[installer:]
	body := strings.Join(bodyLines, "\n")
	if !strings.Contains(body, wcsNotifyCall) {
		t.Fatalf("%s: %q contains no %q — the control command would not reset an inherited SIG_IGN, which is the half of the fix that makes Ctrl-C observable at all",
			wcsControlFile, wcsInstallerFunc, wcsNotifyCall)
	}
	if !strings.Contains(body, wcsClaimCall) {
		t.Fatalf("%s: %q contains no %q — the recorder would keep ownership of the graceful path and may re-raise with the default disposition one grace later, killing the command mid-abort (the same ordering the bare-prompt path documents)",
			wcsControlFile, wcsInstallerFunc, wcsClaimCall)
	}
	t.Logf("arm 2: %s:%d (%s) calls %q at line %d, and the installer registers (%q) and claims (%q)",
		wcsControlFile, command+1, wcsControlCommandFunc, wcsInstallerCall, call+1, wcsNotifyCall, wcsClaimCall)
}

const (
	// Arm 3's documents, by name. The third is included because it carried the
	// same stale `main.go` citation with NO guard pointing at it — it drifted
	// silently through every repair the first two received (measured 2026-10-27).
	wcsAttributionDoc = "../../devdoc/worker-control-sigint-attribution.md"
	wcsCtrlCDoc       = "../../devdoc/worker-control-ctrl-c.md"
	wcsReraiseDoc     = "../../devdoc/signalrec-claim-vs-reraise.md"

	// The two resolve commands every one of these documents hands its reader.
	// Requiring the spelling keeps the SYMBOL route in place: a future editor
	// cannot delete the grep hint and leave a bare number behind.
	wcsResolveInstall = `grep -n 'signalrec.Install("")' cmd/agent/main.go`
	wcsResolveNotify  = `grep -n 'signal.Notify(sigCh' cmd/agent/main.go`

	// The anchor spellings the docs must keep SPELLING — the durable half of
	// each claim, and the part an insertion above main() cannot invalidate.
	wcsDocNeedsInstall = "signalrec.Install"
	// The CALL spelling, comma included — NOT the bare `signal.Notify(sigCh`
	// that the resolve command itself contains. Measured 2026-10-27 by this
	// arm's own red proof: with the short spelling the `grep -n '...'` hint
	// satisfied the floor, so deleting the claim's own spelling stayed green.
	wcsDocNeedsNotify = "signal.Notify(sigCh,"
	wcsDocNeedsChain  = "isWorkerSubcommand(dispatchArgs)"

	// The floor for a devdoc read (a doc far smaller than this means the wrong
	// file was opened).
	wcsDocFloor = 2048
)

// wcsRequireDocSpellings asserts one document still carries the spellings its
// argument is made of. A missing one is fatal BY NAME: the reader losing the
// anchor is the failure this arm exists to prevent, and it is invisible to any
// number check (there is no number left to disagree with).
func wcsRequireDocSpellings(t *testing.T, path, text string, needs ...string) {
	t.Helper()
	for _, n := range needs {
		if !strings.Contains(text, n) {
			t.Fatalf("%s no longer carries %q — this document's claim is spelled in SYMBOLS (that is what the three 2026-10 line-number drifts replaced); restore the spelling, not a number", path, n)
		}
	}
}

type wcsCitation struct {
	line int
	ctx  string
}

// TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas is arm 3. Three things
// are asserted, in order of what survives a refactor of main():
//
//  1. the SYMBOLS — every document still spells the anchors its argument is
//     made of, and the attribution doc still hands the reader the two `grep -n`
//     commands that resolve them. This is the durable half: an insertion above
//     `main()` moves every line and changes none of these.
//  2. any LINE NUMBER a document still prints — checked exactly as before, in
//     the ±60-char window that names the anchor, so the old habit cannot come
//     back unnoticed.
//  3. the DELTAS the documents argue from — a gap survives an insertion above
//     both anchors, so a doc whose claim IS a distance must not drift.
//
// "No number printed" is a PASS: the documents resolve by symbol and the reader
// runs the command. The floors below (spellings, deltas) are what stops that
// from becoming a scan that reads nothing.
func TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas(t *testing.T) {
	src := wcsReadSource(t, wcsMainFile)
	installLine := wcsAnchorLine(t, src, wcsInstallCall)
	chainLine := wcsAnchorLine(t, src, wcsChainHead)
	notifyLine := wcsAnchorLine(t, src, "signal.Notify(sigCh, signalrec.GracefulStopSignals()...)")

	docs := map[string]string{
		wcsAttributionDoc: wcsReadDoc(t, wcsAttributionDoc),
		wcsCtrlCDoc:       wcsReadDoc(t, wcsCtrlCDoc),
		wcsReraiseDoc:     wcsReadDoc(t, wcsReraiseDoc),
	}

	// Check 0 — the SYMBOL floor. Every one of the three documents argues from
	// the recorder and the bare-prompt registration; the attribution doc also
	// argues from the chain, and is the one that must keep the resolve commands
	// a reader needs instead of a number.
	for path, text := range docs {
		wcsRequireDocSpellings(t, path, text, wcsDocNeedsInstall, wcsDocNeedsNotify)
	}
	wcsRequireDocSpellings(t, wcsAttributionDoc, docs[wcsAttributionDoc],
		wcsDocNeedsChain, wcsResolveInstall, wcsResolveNotify)

	// Check A — a citation windowed on the recorder's own name. The window is
	// ±60 chars: wide enough to hold a sentence's subject, narrow enough that a
	// neighbouring code block cannot claim it (that looseness is what made the
	// first draft of this arm pin the wrong anchor — measured).
	installSeen := 0
	for path, text := range docs {
		for _, c := range wcsMainGoNumbers(text) {
			if !strings.Contains(c.ctx, wcsDocNeedsInstall) {
				continue
			}
			installSeen++
			if c.line != installLine {
				t.Fatalf("%s cites main.go:%d where %q is the subject, but main.go puts that call on line %d — a document whose claim IS the line number must not drift (drop the number and spell the anchor, which is what the other documents do, or fix it to %d)",
					path, c.line, wcsDocNeedsInstall, installLine, installLine)
			}
		}
	}

	// Check B — the `(main.go:N` → `return` at `:M)` shape, if a document still
	// prints it. Here the NUMBER PAIR is the claim, so both ends are asserted,
	// including that the return is the chain's very next line.
	pat := regexp.MustCompile("main\\.go:(\\d+)`\\s*→\\s*`return`\\s*at\\s*`:(\\d+)")
	chainSeen := 0
	for path, text := range docs {
		for _, m := range pat.FindAllStringSubmatch(text, -1) {
			n, _ := strconv.Atoi(m[1])  //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
			mm, _ := strconv.Atoi(m[2]) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
			chainSeen++
			if n != chainLine || mm != chainLine+1 {
				t.Fatalf("%s prints the chain as `main.go:%d` → `return` at `:%d`, but the source has the chain head on line %d and its `return` on line %d — spell the head's own anchor (%q) instead of the pair",
					path, n, mm, chainLine, chainLine+1, wcsChainWorkerArm)
			}
		}
	}

	// Check C — a citation windowed on the bare-prompt path's own registration.
	notifySeen := 0
	for path, text := range docs {
		for _, c := range wcsMainGoNumbers(text) {
			if !strings.Contains(c.ctx, "signal.Notify") && !strings.Contains(c.ctx, "bare-prompt") {
				continue
			}
			if c.line == installLine || c.line == chainLine {
				continue // claimed by an earlier check's anchor
			}
			notifySeen++
			if c.line != notifyLine {
				t.Fatalf("%s cites main.go:%d for the bare-prompt %q, but the source puts it on line %d — spell `signal.Notify(sigCh` instead, which is what `%s` resolves",
					path, c.line, "signal.Notify", notifyLine, wcsResolveNotify)
			}
		}
	}
	if notifySeen == 0 {
		// No absolute citation left for this anchor — which is the INTENDED
		// state after the 2026-10-27 rebuild; Check 0 already asserted every
		// document still spells `signal.Notify(sigCh`, so the anchor is not
		// unchecked, it is checked in the form a reader can resolve.
		t.Logf("arm 3: no printed bare-prompt line number (by design) — the anchor is covered by the spelling floor in check 0")
	}

	// The stated DELTAS, which the docs print in prose. Both are checked because
	// a distance survives an insertion ABOVE both anchors while an absolute
	// number does not: these are the two figures the argument is actually made
	// of, and the two that must keep being true. The scan is scoped to the two
	// WORKER-CONTROL documents: `signalrec-claim-vs-reraise.md` states its own
	// "219 lines below", which is the gap measured on the tree of 2026-09-25 —
	// a NARRATIVE figure (Rule 38), not a current claim, and rewriting it would
	// destroy the record rather than repair it.
	claimText := docs[wcsAttributionDoc] + docs[wcsCtrlCDoc]
	lower := strings.ToLower(claimText)
	installToChain := chainLine - installLine
	returnToNotify := notifyLine - (chainLine + 1)

	words := map[string]int{"fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20}
	stated := 0
	for w, v := range words {
		if !regexp.MustCompile(w + " lines before").MatchString(lower) {
			continue
		}
		stated++
		if v != installToChain {
			t.Fatalf("a devdoc states the recorder sits %s lines before the subcommand chain, but the source measures %d (install=%d → chain=%d)",
				w, installToChain, installLine, chainLine)
		}
	}
	if stated == 0 {
		t.Fatalf("arm 3 found no stated recorder→chain delta — the docs assert one in prose and it must be checked, not assumed")
	}

	// The return→notify distance: "250 lines below that return" (this document)
	// and "returns from main() 250 lines earlier" (worker-control-ctrl-c.md)
	// are the SAME figure stated from the two ends — both are checked, because
	// the second spelling slipped past the first draft of this rebuild (found by
	// its own red proof, 2026-10-27: mutating it stayed green). This delta is
	// the second half of the unreachability argument, and it replaces the old
	// floor that required a printed `main.go:N` for the bare-prompt registration
	// — a floor that demanded exactly the drifting citation the documents no
	// longer print.
	below := regexp.MustCompile(`(\d+) lines (?:below|earlier)`)
	belowSeen := 0
	for _, m := range below.FindAllStringSubmatch(claimText, -1) {
		// m[1] is a (\d+) capture, so no non-numeric parse can fail here; an
		// out-of-range value leaves n=0 and the delta comparison below fails.
		n, _ := strconv.Atoi(m[1]) //stylegate:ignore a (\d+) capture cannot fail to parse; overflow leaves 0 and the delta check below fails
		belowSeen++
		if n != returnToNotify {
			t.Fatalf("a devdoc states the distance between the chain's `return` and the bare-prompt registration as %d lines (%q), but the source measures %d (return=%d → notify=%d)",
				n, m[0], returnToNotify, chainLine+1, notifyLine)
		}
	}
	if belowSeen == 0 {
		t.Fatalf("arm 3 found no stated return→notify delta — the unreachability argument is made of that distance and it cannot go unchecked")
	}

	t.Logf("arm 3: symbols spelled in %d doc(s); %d install / %d chain / %d notify printed citation(s) agree with the source (install=%d, chain=%d, notify=%d); deltas %d recorder→chain (%d× stated) and %d return→notify (%d× stated)",
		len(docs), installSeen, chainSeen, notifySeen, installLine, chainLine, notifyLine, installToChain, stated, returnToNotify, belowSeen)
}

// wcsAnchorLine returns the 1-based line main.go puts needle on, or 0.
func wcsAnchorLine(t *testing.T, src, needle string) int {
	t.Helper()
	if i := wcsIndexOfLine(src, needle); i >= 0 {
		return i + 1
	}
	t.Fatalf("main.go carries no line containing %q — arm 3's anchor moved and the guard must be re-derived deliberately", needle)
	return 0
}

// wcsMainGoNumbers extracts every `main.go:NNNN` a doc prints, with a ±60-char
// window around it.
func wcsMainGoNumbers(doc string) []wcsCitation {
	pat := regexp.MustCompile(`main\.go:(\d{3,4})`)
	var out []wcsCitation
	for _, m := range pat.FindAllStringSubmatchIndex(doc, -1) {
		n, err := strconv.Atoi(doc[m[2]:m[3]])
		if err != nil {
			continue
		}
		out = append(out, wcsCitation{line: n, ctx: doc[max(0, m[0]-60):min(len(doc), m[1]+60)]})
	}
	return out
}

func wcsReadDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — this arm checks the two devdocs that carry the attribution", path, err)
	}
	if len(b) < wcsDocFloor {
		t.Fatalf("%s is %d bytes, below the %d-byte floor — the wrong document was read", path, len(b), wcsDocFloor)
	}
	return string(b)
}
