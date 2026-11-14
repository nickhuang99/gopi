package main

// signal_delivery_identity_gate_test.go — the ANTI-REGRESSION gate over the
// SIGNAL-DELIVERY class in this package's own test sources.
//
// THE DEFECT CLASS, AND WHY IT IS STATED AS A CLASS. On 2026-10-17 SIGINT was
// delivered to `systemd --user` five times from a test binary and stopped the
// operator's whole GNOME session (`journalctl -b | grep "Received SIGINT from
// PID"`). The shape that did it was a TEARDOWN sending a signal to a pid it had
// held across a `t.Cleanup`:
//
//	proc, err := os.FindProcess(pid)   // on unix this ALWAYS succeeds
//	_ = proc.Signal(os.Interrupt)      // → SIGINT to whoever owns that NUMBER now
//
// The pid-targeting framing is the DEFECT; the file it lived in is not. The
// brief that commissioned this gate named `merge_every_child_test.go:217`, and
// by the time the work started that file had been deleted whole by the merge
// suite's retirement (5a9af79) — as had its sibling. A gate keyed on a FILE NAME
// would therefore have protected nothing at all, which is exactly why this one is
// keyed on the CLASS and walks every `*_test.go` in this package.
//
// WHAT IT REFUSES — an adjudicable signal delivered without an incarnation check
// or an owned handle, in every spelling the tree has ever used:
//
//	syscall.Kill(<pid expr>, syscall.SIGINT|SIGTERM|SIGKILL)  // the pid form
//	syscall.Kill(<pid expr>, os.Interrupt|os.Kill)            // the same signals, other names
//	<expr>.Signal(syscall.SIGTERM)                            // the *os.Process form
//	<expr>.Signal(os.Interrupt)                               // the PRE-FIX spelling above
//
// WHAT IT DOES NOT REFUSE, and each omission is deliberate:
//
//   - `signal.Ignore(syscall.SIGINT)` / `signal.Reset(…)` — registration, not
//     delivery (named in signalIdentityGateNonDeliveryCalls with the reason).
//   - `syscall.Kill(pid, 0)` — a liveness probe; it carries no signal name.
//   - `syscall.Kill(<address of a handle this file created>, …)` — the OWNERSHIP
//     door. A pid taken from your own `*exec.Cmd` / `*testutil.Proc` cannot be
//     recycled out from under you, so it needs no incarnation read; the gate
//     TRACES that fact in the file (a parameter of a handle type, an
//     `exec.Command(…)` binding, `os.Getpid()`, or a value derived from one) and
//     counts it as an OWNED site. A `*os.Process` reached as `cmd.Process` is
//     owned; one handed in as a bare parameter is NOT, because that is exactly
//     what `os.FindProcess(pid)` builds out of a number.
//   - `syscall.Kill(-pgid, …)` — a GROUP address. Rule 42 bans it in its own
//     right, but converting the group direction is a different change with a
//     different blast radius, so those sites are CENSUSED (counted, floored and
//     printed), never silently skipped. THIS is the population the TERM/KILL
//     widening left behind, and the count is the pointer to that review.
//
// THE TERM/KILL FAMILY WAS SCOPE-DEBT, NOT A PRINCIPLE, and it is paid here.
// The earlier version of this file excluded SIGTERM/SIGKILL on the argument that
// widening without converting would ship a gate that is red on the tree it lands
// in — true, and an argument for doing the conversion FIRST. That is what
// happened: every TERM/KILL site in this package was adjudicated one at a time
// (the per-site table is in the run's report), the ones that address a pid read
// back from a log, a marker file, a /proc scan or a state dir now read the
// identity at delivery time and go through `testutil.KillIfIdentical`, and the
// signal set below was widened afterwards. `os.Kill` is `syscall.SIGKILL` and
// `os.Interrupt` is `syscall.SIGINT`, so the set is keyed on the LEAF of the
// spelling and covers all five names.
//   - `testutil.KillIfIdentical(…)` and any call made inside a function body
//     that already reads/delivers through an identity — the GUARDED door. These
//     are COUNTED (as guarded sites), never flagged, because a guard that is not
//     measured is a guard nobody can tell from a deleted one.
//
// WHAT A SITE IS: A COMMAND POSITION, NEVER A MENTION. A call is
// signal-bearing only when its CALLEE is the signal operation itself
// (`syscall.Kill`, `x.Signal`, the `signal.*` registration doors, the identity
// guard, a named file-local helper). What its arguments CONTAIN is not the
// criterion, and reading arguments for every callee was a measured false
// positive: an assertion that merely FORMATS a signal —
//
//	t.Fatalf("… want %v (SIGKILL) — …", got, syscall.SIGKILL)
//
// — was classified as a signal-bearing site, landed in the loud "unclassified"
// bucket and turned this package red on worker_daemon_foreground_reap_test.go:105.
// Nothing about it was a string literal (the file is parsed without
// ParseComments and nothing inspects a *ast.BasicLit — measured: the
// literal-only fixture classified 0 sites); it was the ARGUMENT scan. The
// criterion is Rule 36's, one level down: an invocation counts where the command
// stands, never where its text is quoted. The ONE place an argument is still
// read is the identity guard's own door, and only after the callee was named as
// a signal-side command (`signalIdentityGateCarriesASignalArgument`). Pinned by
// TestSignalIdentityGateCountsCommandPositionsNotMentions, whose fixture holds a
// mention and a real call in ONE file so a green cannot be bought by quieting
// both.
//
// THE ALLOW-LIST IS CHECKED, NOT DECLARED (Rule 15). A site may carry
// `signal-identity-ok: <reason>`, and the reason is only honoured when the
// ENCLOSING FUNCTION BODY also calls the identity machinery
// (`testutil.KillIfIdentical`, `testutil.ReadProcIdentity`,
// `testutil.AwaitProcIdentity`, or `.Alive()` on a captured identity). A marker
// on a function that delivers by bare pid is still RED — an escape that asserts
// a check the code does not perform is the failure mode this whole gate is
// about, one level up.
//
// NON-VACUOUS BY CONSTRUCTION. The scan PRINTS how many files it walked and
// parsed, and how many SIGINT-bearing call sites it classified into each of the
// three buckets (delivery / guarded / neither). Each bucket has a floor: a walk
// that read nothing fails, a recogniser that stopped matching fails, and a
// "guarded" bucket that silently emptied fails — because the green line must be
// distinguishable from a scan that never ran. Its predicates are pinned by
// fixture arms (one planted violation per spelling → exactly one finding; the
// guarded call → zero findings but ONE guarded site), so a predicate that
// quietly stopped matching fails here instead of turning the whole tree green.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --- the gate's own named constants (Rule 1) --------------------------------

const (
	// signalIdentityGateTestSuffix selects the files in scope. Production code
	// is not this gate's business: a production delivery point has its own
	// review, and this gate is about the TEST binary that reached the operator's
	// session manager.
	signalIdentityGateTestSuffix = "_test.go"

	// signalIdentityGateEscape marks a delivery site the author has decided is
	// legitimate. The marker alone is not enough — see the enforcing predicate
	// (signalIdentityGateEnclosingBodyCarriesTheCheck).
	signalIdentityGateEscape = "signal-identity-ok:"

	// signalIdentityGateMinFiles is the READ-FLOOR (Rule 15): a scan that
	// parsed fewer test files than this did not read the package, and its clean
	// line would be a pass over nothing. Measured at this branch's tip:
	// 626 *_test.go files in cmd/agent; the floor sits well below that because
	// its job is to catch a walk that found NOTHING, not to pin today's count.
	signalIdentityGateMinFiles = 300

	// signalIdentityGateMinSignalSites is the CLASSIFICATION floor: signal-bearing
	// call sites the scan could sort into a bucket — delivery, guarded, owned or
	// neither. Measured at this branch's tip with the signal set widened to
	// SIGINT|SIGTERM|SIGKILL: the SIGINT sites plus every TERM/KILL site in the
	// package's own tests, whose per-site adjudication is the run's report. If the
	// recogniser stopped matching, this count falls and a tree full of bare
	// deliveries would read as clean.
	signalIdentityGateMinSignalSites = 20

	// signalIdentityGateMinGuardedSites is the floor on the GOOD bucket, and it
	// is the one that makes "no findings" mean something: with this at 0, a
	// recogniser that matched nothing at all would produce the same empty
	// findings list as a tree whose every delivery goes through the guard.
	// Measured at this branch's tip: the sites that read `testutil.ReadProcIdentity`
	// and deliver through `testutil.KillIfIdentical`.
	signalIdentityGateMinGuardedSites = 3

	// signalIdentityGateMinOwnedSites is the floor on the OTHER legal bucket: a
	// delivery addressed to a handle the file itself created (its own `*exec.Cmd`
	// or the `*testutil.Proc` wrapping one). It is measured too, for the same
	// reason — an ownership bucket that silently emptied would turn "owned" into
	// "nobody looked".
	signalIdentityGateMinOwnedSites = 3

	// signalIdentityGateMinGroupSites is the floor on the CENSUS bucket for
	// group-addressed deliveries (`syscall.Kill(-pgid, …)`). They are not this
	// gate's refusals — a group id is nobody's handle — but the population is the
	// pointer to the review that owns that direction (this run's report names it),
	// so a scan that stopped seeing them would be reporting "nothing to review".
	signalIdentityGateMinGroupSites = 1
)

// signalIdentityGateCheckCalls are the calls whose presence in a function body
// turns an escape marker into a legitimate delivery (and which classify an
// otherwise-unrecognised SIGINT-bearing call as GUARDED): the identity machinery
// this package routes its signals through.
var signalIdentityGateCheckCalls = map[string]bool{
	"testutil.KillIfIdentical":   true,
	"testutil.ReadProcIdentity":  true,
	"testutil.AwaitProcIdentity": true,
}

// signalIdentityGateNonDeliveryCalls are the `pkg.Func` spellings that mention
// SIGINT without DELIVERING it. Naming them is the whole point (Rule 15): a
// scanner that just counted SIGINT mentions would report the registration sites
// as violations, and the first reviewer to hit that would weaken the gate.
var signalIdentityGateNonDeliveryCalls = map[string]string{
	"signal.Ignore": "registration: it REPLACES the disposition, it signals nothing",
	"signal.Reset":  "registration: it restores the default disposition, it signals nothing",
	"signal.Notify": "registration: it routes the signal to a channel, it signals nothing",
	"uint64":        "a type conversion over the signal's numeric value (a signal MASK), not a delivery at all",

	// The three below are FILE-LOCAL HELPERS whose parameter merely SPELLS the
	// signal:
	//
	//	chatSignalCloseoutNotice(input, sig)  → formats the closeout sentence
	//	wdStopSignalFn(pid, sig)              → a seam variable pointing at syscall.Kill
	//	signalRunChild(pid, sig)              → a PRODUCTION wrapper (cli_tasks_worker_stop.go)
	//
	// NONE of them delivers from the call site in a `*_test.go` file: the
	// production helper's own delivery is guarded by the run-child ownership
	// check, and the seam is asserted by the ESRCH case that swaps it out. They
	// are named here so they cannot be mistaken for deliveries the gate skipped.
	"chatSignalCloseoutNotice": "a closeout-notice FORMATTER: it writes a sentence about the signal, it signals nothing",
	"wdStopSignalFn":           "a test seam variable that production points at syscall.Kill; the ESRCH case swaps it out and asserts the refusal",
	"signalRunChild":           "a PRODUCTION wrapper (cli_tasks_worker_stop.go); its delivery legs check run-child ownership, and from a _test.go it is a caller, not a raw delivery",
}

// signalIdentityGateSignalNames is the SIGNAL SET this gate is about, keyed on
// the LEAF of the spelling used at the call site.
//
// It began as SIGINT alone, because that is the signal that stopped the
// operator's desktop (2026-10-17). SIGTERM/SIGKILL/os.Kill were left out on the
// argument that "widening the gate without converting those sites would ship a
// gate that is red on the tree it lands in" — true, and therefore a debt, not a
// principle. That debt is paid here: every TERM/KILL site in this package was
// adjudicated one at a time before the set grew (the per-site table is the
// run's report), so the wider set lands GREEN on the tree it lands in.
//
// `Interrupt` and `Kill` are the `os.*` aliases: `os.Interrupt` IS SIGINT and
// `os.Kill` IS SIGKILL. A gate that read only the `syscall.*` spelling would
// miss `proc.Signal(os.Kill)` — the same delivery under its shorter name.
var signalIdentityGateSignalNames = map[string]bool{
	"SIGINT":    true,
	"Interrupt": true,
	"SIGTERM":   true,
	"SIGKILL":   true,
	"Kill":      true,
}

// signalIdentityGateOwnedProcConstructors are the calls that build a handle the
// CALLER OWNS. A `*testutil.Proc` wraps its own `*exec.Cmd` and delivers through
// `p.cmd.Process.Signal`, so `p.Signal(SIGTERM)` is a delivery to a handle this
// file created — Rule 42's ownership half, measured rather than asserted.
var signalIdentityGateOwnedProcConstructors = map[string]bool{
	"testutil.StartGopi":    true,
	"testutil.StartGopiBin": true,
	"testutil.StartProc":    true,
}

// --- findings ----------------------------------------------------------------

// signalIdentityGateFinding is one delivery site the gate refused, with
// everything the message needs to be actionable without re-reading the file.
type signalIdentityGateFinding struct {
	file    string // relative to the scanned root
	line    int
	form    string // the spelling the finding was matched by
	snippet string
	escape  string // the reason carried by a marker that did NOT satisfy the check
}

func (f signalIdentityGateFinding) String() string {
	base := fmt.Sprintf("%s:%d: a signal is delivered without an identity or ownership check (%s) — route it through testutil.KillIfIdentical", f.file, f.line, f.form)
	if f.escape != "" {
		base += fmt.Sprintf("; the escape marker %q is present but its function body carries no identity check", f.escape)
	}
	return base
}

// --- the scan ----------------------------------------------------------------

// signalIdentityGateCensus counts what ONE file contributed, so the walk can
// report buckets instead of a single un-diagnosable total.
type signalIdentityGateCensus struct {
	filesWalked       int
	filesParsed       int
	signalSites       int      // signal-bearing calls classified into one of the buckets
	deliverySites     int      // … of which are DELIVERY forms (unguarded unless escaped)
	ownedSites        int      // … of which address a handle the file itself created (Rule 42's ownership half)
	groupSites        int      // … of which address a process GROUP — counted, not adjudicated here (see the census text)
	guardedSites      int      // … of which are deliveries through the identity machinery
	unclassifiedSites int      // … of which match neither bucket (neither is a review, so it is loud)
	unclassifiedAt    []string // where those sat, so the gate names them instead of counting them
	escapedSites      int
}

func (c *signalIdentityGateCensus) addFile(o signalIdentityGateCensus) {
	c.filesWalked += o.filesWalked
	c.filesParsed += o.filesParsed
	c.signalSites += o.signalSites
	c.deliverySites += o.deliverySites
	c.ownedSites += o.ownedSites
	c.groupSites += o.groupSites
	c.guardedSites += o.guardedSites
	c.unclassifiedSites += o.unclassifiedSites
	c.unclassifiedAt = append(c.unclassifiedAt, o.unclassifiedAt...)
	c.escapedSites += o.escapedSites
}

// signalIdentityGateScan walks ONE directory's *_test.go files and returns the
// refusals it found plus the census. A file that does not parse is an ERROR, not
// a skip: a gate with a hole shaped like whatever broke Go's parser is not a
// gate.
func signalIdentityGateScan(t *testing.T, dir string) ([]signalIdentityGateFinding, signalIdentityGateCensus) {
	t.Helper()
	findings := []signalIdentityGateFinding{}
	census := signalIdentityGateCensus{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), signalIdentityGateTestSuffix) {
			continue
		}
		census.filesWalked++
		path := filepath.Join(dir, e.Name())
		f, c, err := signalIdentityGateScanFile(path)
		if err != nil {
			t.Fatalf("%v", err) // a parse failure must never read as a clean file
		}
		findings = append(findings, f...)
		census.addFile(c)
	}
	return findings, census
}

// signalIdentityGateScanFile parses ONE test source and sorts every
// SIGINT-bearing call in it into a bucket.
//
// The file is parsed WITHOUT `parser.ParseComments` ON PURPOSE (measured trap,
// memory #18015): this gate's own prose — and every post-mortem in this package
// — quotes the banned spelling, and a scan that read comments would count the
// EXPLANATION of the defect as the defect. Working over the AST alone cannot see
// a comment at all, and neither can the `strings.Contains` line test below
// (which reads source TEXT for the guarded door, a shape the AST names
// identically but whose argument it cannot order).
func signalIdentityGateScanFile(path string) ([]signalIdentityGateFinding, signalIdentityGateCensus, error) {
	census := signalIdentityGateCensus{}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, census, fmt.Errorf("read %s: %w", path, err)
	}
	escaped := signalIdentityGateEscapedLines(src)
	bindings := signalIdentityGateLocalBindings(src)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, census, fmt.Errorf("parse %s: %w", path, err)
	}
	census.filesParsed = 1

	findings := []signalIdentityGateFinding{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		form, isDelivery, carriesSignal := signalIdentityGateClassify(call, bindings)
		if !carriesSignal {
			return true
		}
		census.signalSites++
		pos := fset.Position(call.Pos())
		snippet := signalIdentityGateSnippet(string(src), pos.Line)

		if !isDelivery {
			// A signal-bearing call that is NOT one of the delivery forms is
			// either the guarded door or another helper that takes an identity
			// (groundKill's wrapper). It counts as GUARDED only when the
			// enclosing body really carries the check — the same predicate the
			// escape marker is held to, so the two cannot disagree.
			if signalIdentityGateEnclosingBodyCarriesTheCheck(file, call) {
				census.guardedSites++
				return true
			}
			census.unclassifiedSites++
			census.unclassifiedAt = append(census.unclassifiedAt, fmt.Sprintf("%s:%d %s", filepath.Base(path), pos.Line, snippet))
			return true
		}

		census.deliverySites++
		// A GROUP address is the other direction of Rule 42 and is CENSUSED,
		// not adjudicated here: `syscall.Kill(-pgid, …)` computes its address
		// from a recorded pgid, and the callers (the suite's stall watchdog and
		// the group teardown fixtures) are converted by their own change. It is
		// counted so it can never masquerade as "unclassified darkness", and
		// floored so the population cannot silently vanish.
		if signalIdentityGateAddressesAGroup(call) {
			census.groupSites++
			return true
		}
		// OWNERSHIP: a delivery to a handle this file created satisfies Rule 42
		// without an incarnation read — the address cannot outlive its owner.
		// This is not an escape marker: it is a fact the gate can trace in the
		// file (see signalIdentityGateAddressesAnOwnedHandle).
		if signalIdentityGateAddressesAnOwnedHandle(file, call, bindings) {
			census.ownedSites++
			return true
		}
		reason, marked := escaped[pos.Line]
		if !marked {
			findings = append(findings, signalIdentityGateFinding{
				file:    filepath.Base(path),
				line:    pos.Line,
				form:    form,
				snippet: snippet,
			})
			return true
		}
		census.escapedSites++
		if signalIdentityGateEnclosingBodyCarriesTheCheck(file, call) {
			census.guardedSites++
			return true
		}
		findings = append(findings, signalIdentityGateFinding{
			file:    filepath.Base(path),
			line:    pos.Line,
			form:    form,
			snippet: snippet,
			escape:  reason,
		})
		return true
	})
	return findings, census, nil
}

// signalIdentityGateClassify answers two questions about one call: does it own a
// SIGINT mention at all, and does it DELIVER it?
//
// OWNERSHIP IS THE WHOLE SUBTLETY, and it is what makes this predicate honest.
// A mention belongs to the SMALLEST call that encloses it — never to an ancestor
// that merely passes a closure along. Two measured shapes on this tree turned
// the first version of this gate red for the wrong reason:
//
//	t.Cleanup(func() { signal.Reset(syscall.SIGINT) })   // the RUNNER, not the delivery
//	sigintMask := uint64(1) << (uint64(syscall.SIGINT)-1) // a MASK, not a delivery
//
// In both, an outer call "contains" the signal name, and a predicate that read
// arguments recursively would flag the container. So the walk stops at every
// nested CallExpr: that inner call owns its own arguments, and it is classified
// on its own terms. (`uint64` and the three `signal.*` registration doors are
// named in signalIdentityGateNonDeliveryCalls with the reason they are legal.)
//
// Delivery forms (the ones this gate refuses):
//
//	syscall.Kill(x, syscall.SIGINT|os.Interrupt)   → the pid form
//	x.Signal(syscall.SIGINT|os.Interrupt)          → the *os.Process form
//
// Everything else that owns a mention (`signal.Ignore`, `signal.Reset`, a mask
// conversion, a helper taking an identity) comes back with ownsSignal=true,
// isDelivery=false — and the caller then decides whether the enclosing body
// earned the GUARDED bucket.
//
// THE COMMAND POSITION IS THE ONLY PLACE A SITE CAN STAND, and it is decided by
// the CALLEE alone — never by what its arguments contain. The false positive
// that made this package red (worker_daemon_foreground_reap_test.go:105) was
// exactly that reading:
//
//	t.Fatalf("… want %v (SIGKILL) — …", got, syscall.SIGKILL)   // a MENTION
//
// The gate called it a signal-bearing site because ONE OF ITS ARGUMENTS named a
// signal, so a FORMATTER was classified as a delivery point and the site landed
// in the loud "unclassified" bucket. Nothing about the STRING was involved — the
// parser does not even read a *ast.BasicLit here. The criterion this gate now
// uses is the one Rule 36 states for a retired mechanism: an invocation counts
// where the command stands, never where its text is quoted. So a call is a site
// only when its CALLEE is the signal operation itself (`syscall.Kill`,
// `x.Signal`, the `signal.*` registration doors, the identity guard, a named
// file-local helper); an argument-scanning fallback would put every formatter,
// assertion and log line back in scope.
func signalIdentityGateClassify(call *ast.CallExpr, bindings map[string]ast.Expr) (form string, isDelivery bool, ownsSignal bool) {
	name := signalIdentityGateCallName(call)
	if name == "" {
		// Not a command position this gate recognises (a call on a composite
		// literal, a type conversion rendered as a paren expression, …).
		return "", false, false
	}
	if reason, named := signalIdentityGateNonDeliveryCalls[name]; named {
		_ = reason // the table's reason is documentation; the DECISION is "not a delivery"
		return "", false, false
	}
	if signalIdentityGateCheckCalls[name] {
		// THE GUARDED DOOR IS A COMMAND POSITION but never a delivery: the call
		// IS the identity check, so the enclosing-body predicate below credits
		// it (that is what makes the guarded bucket measurable at all).
		//
		// It counts ONLY when it actually CARRYIES a signal argument —
		// `testutil.KillIfIdentical(id, syscall.SIGINT)` does, and that is a
		// delivery routed through the guard; `testutil.ReadProcIdentity(pid)`
		// names no signal at all and is not a site. This is the ONE place an
		// argument is read, and the callee had to be named as a signal-side
		// command first — never a formatter, never an assertion.
		return "", false, signalIdentityGateCarriesASignalArgument(call, bindings)
	}
	switch {
	case name == "syscall.Kill":
		if len(call.Args) == 2 && signalIdentityGateIsTheSignalResolved(call.Args[1], bindings) {
			return "syscall.Kill(pid, " + signalIdentityGateArgSpelling(call.Args[1]) + ")", true, true
		}
		// `syscall.Kill(pid, 0)` is a liveness probe and carries no signal name.
		return "", false, false
	case signalIdentityGateCallLeaf(call) == "Signal":
		// The LEAF distinguishes the form, never the qualifier: the dominant
		// spelling on this tree is `cmd.Process.Signal(sig)`, whose receiver is
		// a SELECTOR rather than a package name, so the rendered name is a bare
		// "Signal". A `strings.HasSuffix(name, ".Signal")` test therefore
		// classified that entire population as "neither form" (measured: 17
		// sites fell through that hole before this arm read the leaf).
		if len(call.Args) == 1 && signalIdentityGateIsTheSignalResolved(call.Args[0], bindings) {
			return "Process.Signal(" + signalIdentityGateArgSpelling(call.Args[0]) + ")", true, true
		}
		return "", false, false
	}
	return "", false, false
}

// signalIdentityGateCarriesASignalArgument reports whether ONE OF THIS CALL'S
// ARGUMENTS names a signal — and it is used for exactly one callee class: the
// identity guard's own door (see signalIdentityGateClassify). It is NOT a
// general "does this call mention a signal" test, and it must never become one
// again: that predicate is what classified `t.Fatalf("… %v (SIGKILL)", got,
// syscall.SIGKILL)` as a signal-bearing site and turned this package red.
//
// A mention belongs to the SMALLEST call that encloses it, so the walk stops at
// every nested CallExpr: that inner call owns its own arguments and is
// classified on its own terms. (`uint64` and the three `signal.*` registration
// doors are named in signalIdentityGateNonDeliveryCalls with the reason they are
// legal.)
func signalIdentityGateCarriesASignalArgument(call *ast.CallExpr, bindings map[string]ast.Expr) bool {
	found := false
	for _, arg := range call.Args {
		ast.Inspect(arg, func(n ast.Node) bool {
			if found {
				return false
			}
			switch v := n.(type) {
			case *ast.CallExpr:
				if v != call {
					return false
				}
				return true
			case *ast.SelectorExpr:
				if _, ok := v.X.(*ast.Ident); ok {
					if signalIdentityGateSignalNames[v.Sel.Name] {
						found = true
						return false
					}
				}
			case *ast.Ident:
				if signalIdentityGateSignalNames[v.Name] || signalIdentityGateIsTheSignalResolved(v, bindings) {
					found = true
					return false
				}
			}
			return true
		})
	}
	return found
}

// signalIdentityGateCallName renders the callee as `pkg.Func` when it is
// package-qualified, and as the bare identifier otherwise (`uint64`).
func signalIdentityGateCallName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok {
			return pkg.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	}
	return ""
}

// signalIdentityGateCallLeaf renders the LEAF of the callee: `syscall.Kill` →
// "Kill", `cmd.Process.Signal` → "Signal", `p.Signal` → "Signal".
func signalIdentityGateCallLeaf(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	}
	return ""
}

// signalIdentityGateOwnedHandleTypes are the parameter types that already ARE a
// handle: a function that receives one of these was handed the `*exec.Cmd` (or
// the `*os.Process`) that started the child, and `cmd.Process.Pid` is therefore
// the caller's own child, not a number it read from somewhere.
var signalIdentityGateOwnedHandleTypes = map[string]bool{
	"Cmd":         true, // *exec.Cmd — only a Start produces one
	"Proc":        true, // *testutil.Proc — wraps its own *exec.Cmd
	"MockProcess": true, // the pgid-scoped handle the testutil helpers own
	// `Process` is DELIBERATELY ABSENT as a TYPE and present only on the FIELD
	// chain (signalIdentityGateChainStartsAtAnOwned): a bare `*os.Process` is
	// precisely what `os.FindProcess(pid)` constructs out of a NUMBER, and that
	// is the original post-mortem shape (`os.FindProcess` on unix always
	// succeeds). A `*os.Process` reached as `cmd.Process` is owned because its
	// ROOT is the *exec.Cmd; a `*os.Process` handed in as a parameter is not
	// evidence of ownership at all.
}

// signalIdentityGateAddressesAGroup reports whether a delivery is addressed to a
// process GROUP (`syscall.Kill(-pgid, …)`), which Rule 42 bans in its own right
// and which THIS gate deliberately does not adjudicate.
//
// WHY IT IS A CENSUS AND NOT A REFUSAL: the group direction is the subject of
// its own review — the addresses are computed from a recorded pgid, the callers
// are the suite's stall watchdog and the group teardown fixtures, and converting
// them is a different change with a different blast radius. Widening this gate
// to refuse them would ship a gate that is red on the tree it lands in, which is
// the exact failure the TERM/KILL debt came from. So they are COUNTED, PRINTED
// and floored: a group can never pass as "unclassified darkness", and the count
// is a number a reader can act on.
func signalIdentityGateAddressesAGroup(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	un, ok := call.Args[0].(*ast.UnaryExpr)
	return ok && un.Op == token.SUB
}

// signalIdentityGateAddressArgument returns the expression that carries the
// ADDRESS of a delivery — which is NOT the same argument in the two forms:
//
//	syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)   → Args[0]
//	cmd.Process.Signal(syscall.SIGTERM)              → the RECEIVER (`cmd.Process`)
//
// Reading Args[0] in both would inspect the SIGNAL of the `.Signal` form and
// conclude that no delivery is addressed to anything (measured: it made the
// ownership bucket report 0 on a tree where every `.Signal` call addresses its
// own child).
func signalIdentityGateAddressArgument(call *ast.CallExpr) (ast.Expr, bool) {
	if signalIdentityGateCallName(call) == "syscall.Kill" {
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	return sel.X, true
}

// signalIdentityGateAddressesAnOwnedHandle answers Rule 42's OWNERSHIP half at
// the source: is the ADDRESS argument a handle the enclosing function created
// itself — its own `exec.Command`/`exec.CommandContext`/`os.StartProcess`
// result, a `*testutil.Proc` from StartGopi, or a parameter whose TYPE is one of
// those — rather than a number read from a ledger, a /proc scan or a name match?
//
// Ownership is not a guessable property, so the gate only accepts a shape it can
// trace inside the file: the name must be BOUND (by `:=` or a parameter) to a
// constructor or a handle type. Everything else stays a finding — a pid that
// arrived from `findChildPidByEnv` or `wdParseStart` is the defect class this
// gate exists for, even though it is very likely right.
func signalIdentityGateAddressesAnOwnedHandle(file *ast.File, call *ast.CallExpr, bindings map[string]ast.Expr) bool {
	addr, ok := signalIdentityGateAddressArgument(call)
	if !ok {
		return false
	}
	if signalIdentityGateIsASelfAddress(addr) {
		return true
	}
	owned := signalIdentityGateOwnedNames(file, call, bindings)
	return signalIdentityGateIsAValueOfAnOwnedHandle(addr, owned, bindings)
}

// signalIdentityGateChainStartsAtAnOwned walks an address expression of the form
// `cmd`, `cmd.Process`, `cmd.Process.Pid`, `s.Cmd.Process.Pid` — a handle and a
// short chain of HANDLE FIELDS — and reports whether its ROOT is an owned name.
//
// Only field names that are themselves handles or a pid are accepted on the
// chain: accepting an arbitrary selector would let `someMap[pid].Process` pass,
// and the point is that the value never left the caller's own object graph.
func signalIdentityGateChainStartsAtAnOwned(addr ast.Expr, owned map[string]bool) bool {
	switch v := addr.(type) {
	case *ast.Ident:
		return owned[v.Name]
	case *ast.SelectorExpr:
		if !signalIdentityGateOwnedHandleTypes[v.Sel.Name] && v.Sel.Name != "Pid" {
			return false
		}
		return signalIdentityGateChainStartsAtAnOwned(v.X, owned)
	}
	return false
}

// signalIdentityGateOwnedNames collects every identifier bound to a handle this
// caller created: a parameter whose type is a handle type, and every
// `x := <owned constructor>(…)`.
//
// EVERY ENCLOSING SCOPE COUNTS, not the innermost one. The dominant teardown
// shape on this tree is
//
//	cmd := exec.Command(bin, …)          // the handle
//	t.Cleanup(func() { cmd.Process.Signal(syscall.SIGKILL) })
//
// and a predicate that judged only the closure's own body would call that a
// bare pid — measured: it did exactly that for 5 sites, because the ownership
// fact lives one scope out. A closure CAPTURES its parent's variables, so the
// union of the scopes enclosing the call is the honest scope.
func signalIdentityGateOwnedNames(file *ast.File, call *ast.CallExpr, bindings map[string]ast.Expr) map[string]bool {
	owned := map[string]bool{}
	fileFuncNames := signalIdentityGateHandleReturningFuncs(file)
	var scopes []struct {
		fields *ast.FieldList
		body   *ast.BlockStmt
	}
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		var fields *ast.FieldList
		switch f := n.(type) {
		case *ast.FuncDecl:
			body, fields = f.Body, f.Type.Params
		case *ast.FuncLit:
			body, fields = f.Body, f.Type.Params
		}
		if body == nil || call.Pos() < body.Pos() || call.End() > body.End() {
			return true
		}
		scopes = append(scopes, struct {
			fields *ast.FieldList
			body   *ast.BlockStmt
		}{fields, body})
		return true
	})
	// Two passes, so a binding derived from a handle declared in the SAME
	// scope (`pid := cmd.Process.Pid`, then `victim := pid`) is owned too.
	// Functions in this package are small; a fixpoint is not needed beyond
	// this and would only widen what the gate silently accepts.
	for pass := 0; pass < 2; pass++ {
		for _, sc := range scopes {
			signalIdentityGateAddOwnedInScope(sc.fields, sc.body, bindings, owned, fileFuncNames)
		}
	}
	return owned
}

// signalIdentityGateHandleReturningFuncs names every file-local function whose
// RETURN TYPE is a handle — `wdForeground(t, bin, env) *exec.Cmd` is a factory
// for something the caller then owns.
func signalIdentityGateHandleReturningFuncs(file *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type == nil || fn.Type.Results == nil {
			continue
		}
		for _, res := range fn.Type.Results.List {
			if signalIdentityGateIsAHandleType(res.Type) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// signalIdentityGateAddOwnedInScope merges ONE enclosing scope's owned names.
func signalIdentityGateAddOwnedInScope(fields *ast.FieldList, body *ast.BlockStmt, bindings map[string]ast.Expr, owned map[string]bool, handleFuncs map[string]bool) {
	{
		if fields != nil {
			for _, field := range fields.List {
				if !signalIdentityGateIsAHandleType(field.Type) {
					continue
				}
				for _, n := range field.Names {
					owned[n.Name] = true
				}
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, isIdent := lhs.(*ast.Ident)
			if !isIdent {
				continue
			}
			rhs := assign.Rhs[i]
			if signalIdentityGateIsAnOwnedConstructor(rhs, bindings) {
				owned[id.Name] = true
				continue
			}
			if call, isCall := rhs.(*ast.CallExpr); isCall && handleFuncs[signalIdentityGateCallName(call)] {
				owned[id.Name] = true
				continue
			}
			if signalIdentityGateIsAValueOfAnOwnedHandle(rhs, owned, bindings) {
				owned[id.Name] = true
			}
		}
		return true
	})
}

// signalIdentityGateIsAnOwnedConstructor reports whether the expression STARTS a
// handle the caller owns: `exec.Command(…)`, `os.StartProcess(…)` or one of the
// testutil starters that wrap them.
func signalIdentityGateIsAnOwnedConstructor(e ast.Expr, bindings map[string]ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok {
		if bound, ok := bindings[id.Name]; ok {
			return signalIdentityGateIsAnOwnedConstructor(bound, bindings)
		}
		return false
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	return signalIdentityGateIsAnOwnedConstructorName(signalIdentityGateCallName(call))
}

// signalIdentityGateIsAnOwnedConstructorName is the NAME half of the predicate,
// shared with the call-site arm (which has the *ast.CallExpr already).
func signalIdentityGateIsAnOwnedConstructorName(name string) bool {
	switch name {
	case "exec.Command", "exec.CommandContext", "os.StartProcess":
		return true
	}
	return signalIdentityGateOwnedProcConstructors[name]
}

// signalIdentityGateIsASelfAddress reports whether the address is THIS process —
// `os.Getpid()`. Rule 42 asks "did the caller own what it signals"; a process
// owns itself, and a test that drives its own signal handling (the SIGHUP/TERM
// drain cases) is the one shape where that is the whole point.
func signalIdentityGateIsASelfAddress(addr ast.Expr) bool {
	call, ok := addr.(*ast.CallExpr)
	return ok && signalIdentityGateCallName(call) == "os.Getpid"
}

// signalIdentityGateIsAValueOfAnOwnedHandle reports whether `expr` is DERIVED
// from a handle the caller created — `cmd.Process.Pid`, or a name already bound
// to one. A number taken out of your own `*exec.Cmd` is still your child's
// number; a number taken out of a log line or a /proc scan is not.
func signalIdentityGateIsAValueOfAnOwnedHandle(expr ast.Expr, owned map[string]bool, bindings map[string]ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.Ident:
		if owned[v.Name] {
			return true
		}
		if bound, ok := bindings[v.Name]; ok {
			return signalIdentityGateIsAValueOfAnOwnedHandle(bound, owned, bindings)
		}
		return false
	case *ast.SelectorExpr:
		return signalIdentityGateIsAValueOfAnOwnedHandle(v.X, owned, bindings)
	case *ast.CallExpr:
		return signalIdentityGateIsAnOwnedConstructor(v, bindings) || signalIdentityGateIsASelfAddress(v)
	}
	return false
}

// signalIdentityGateIsAHandleType reports whether a parameter's type is (or
// points at) one of the handle types above.
func signalIdentityGateIsAHandleType(e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return signalIdentityGateOwnedHandleTypes[v.Sel.Name]
	case *ast.Ident:
		return signalIdentityGateOwnedHandleTypes[v.Name]
	}
	return false
}

// signalIdentityGateSmallestBodyOwning is signalIdentityGateSmallestBody plus
// the DECLARATION that body belongs to (its parameter list is where a handle
// type is visible).
func signalIdentityGateSmallestBodyOwning(file *ast.File, call *ast.CallExpr) (*ast.BlockStmt, ast.Node) {
	var smallest *ast.BlockStmt
	var owner ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch f := n.(type) {
		case *ast.FuncDecl:
			body = f.Body
		case *ast.FuncLit:
			body = f.Body
		}
		if body == nil {
			return true
		}
		if call.Pos() < body.Pos() || call.End() > body.End() {
			return true
		}
		if smallest == nil || body.Pos() >= smallest.Pos() {
			smallest = body
			owner = n
		}
		return true
	})
	return smallest, owner
}

// signalIdentityGateIsTheSignal reports whether the expression names one of the
// signals in signalIdentityGateSignalNames, through ANY of its spellings.
func signalIdentityGateIsTheSignal(e ast.Expr) bool {
	name, ok := signalIdentityGateSelectorLeaf(e)
	if !ok {
		return false
	}
	return signalIdentityGateSignalNames[name]
}

// signalIdentityGateIsTheSignalResolved is signalIdentityGateIsTheSignal plus ONE
// hop through a file-local binding: `sig := syscall.SIGINT; syscall.Kill(pid, sig)`
// is the same defect as the inline spelling, and a gate that only read literals
// would let it through by renaming the signal. Measured on this tree when this
// resolver was added: no site used the indirection — so this arm is the
// anti-regression for a shape that is currently absent, not a fix for one that
// fired.
func signalIdentityGateIsTheSignalResolved(e ast.Expr, bindings map[string]ast.Expr) bool {
	if signalIdentityGateIsTheSignal(e) {
		return true
	}
	if id, ok := e.(*ast.Ident); ok {
		if bound, ok := bindings[id.Name]; ok {
			return signalIdentityGateIsTheSignal(bound)
		}
	}
	return false
}

// signalIdentityGateLocalBindings indexes every `name := <expr>` / `var name = <expr>`
// of ONE file. File-scoped on purpose: a cross-file binding would have to answer
// "which of these two names does this file see", which is the compiler's question
// and not a gate's.
func signalIdentityGateLocalBindings(src []byte) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bindings.go", src, 0)
	if err != nil {
		return out // the caller already fails loudly on a parse error
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE || len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					out[id.Name] = s.Rhs[i]
				}
			}
		case *ast.ValueSpec:
			if len(s.Names) != len(s.Values) {
				return true
			}
			for i, name := range s.Names {
				out[name.Name] = s.Values[i]
			}
		}
		return true
	})
	return out
}

// signalIdentityGateArgSpelling renders the signal argument for the message.
func signalIdentityGateArgSpelling(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := v.X.(*ast.Ident); ok {
			return pkg.Name + "." + v.Sel.Name
		}
	case *ast.Ident:
		// The INDIRECTION spelling: `sig := syscall.SIGINT; Kill(pid, sig)`. A
		// bare name must be rendered as itself, or the finding reads "?" and the
		// reader cannot tell which variable carried the signal.
		return v.Name
	}
	return "?"
}

// signalIdentityGateSelectorLeaf returns the LEAF identifier of a qualified
// expression (`syscall.SIGINT` → "SIGINT"). A bare identifier is returned as-is
// so a dot-imported spelling is not silently missed.
func signalIdentityGateSelectorLeaf(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name, true
	case *ast.Ident:
		return v.Name, true
	}
	return "", false
}

// signalIdentityGateEscapedLines maps every line that carries the escape marker
// (and the line below it, the "reason directly above the call" idiom this repo
// uses everywhere) to the reason text after the marker. The escape is
// LINE-scoped: it never covers a file.
func signalIdentityGateEscapedLines(src []byte) map[int]string {
	out := map[int]string{}
	for i, line := range strings.Split(string(src), "\n") {
		idx := strings.Index(line, signalIdentityGateEscape)
		if idx < 0 {
			continue
		}
		reason := strings.TrimSpace(line[idx+len(signalIdentityGateEscape):])
		out[i+1] = reason
		out[i+2] = reason
	}
	return out
}

// signalIdentityGateEnclosingBodyCarriesTheCheck is the ALLOW-LIST'S ENFORCEMENT
// and the GUARDED bucket's only credential: a call is honoured only when the
// function (or func literal) that CONTAINS it also calls the identity machinery.
// It walks the file's AST and picks the SMALLEST enclosing body, so a
// `t.Cleanup(func(){…})` is judged on its own body rather than on the test that
// registers it.
func signalIdentityGateEnclosingBodyCarriesTheCheck(file *ast.File, call *ast.CallExpr) bool {
	smallest, _ := signalIdentityGateSmallestBodyOwning(file, call)
	if smallest == nil {
		return false
	}
	found := false
	ast.Inspect(smallest, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok {
			if signalIdentityGateCheckCalls[pkg.Name+"."+sel.Sel.Name] {
				found = true
				return false
			}
		}
		// `.Alive()` on a captured identity is the third checked door.
		if sel.Sel.Name == "Alive" && len(c.Args) == 0 {
			found = true
			return false
		}
		return true
	})
	return found
}

// signalIdentityGateSnippet returns the source line, trimmed — the message's
// quote, so a reviewer sees the call without opening the file.
func signalIdentityGateSnippet(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// --- the gate ----------------------------------------------------------------

// TestNoTestDeliversSIGINTWithoutAnIncarnationCheck is the gate: no `*_test.go`
// in this package may deliver SIGINT to a pid (or an `*os.Process`) without the
// incarnation check. Every finding is reported in ONE pass, with the file, the
// line, the spelling and the door to use instead.
func TestNoTestDeliversSIGINTWithoutAnIncarnationCheck(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	findings, census := signalIdentityGateScan(t, dir)

	// The census is PRINTED, not merely asserted: a green run must carry the
	// number of files it read, or "clean" is indistinguishable from "read
	// nothing" (Rule 15).
	// The buckets are NOT all subsets of each other and the line does not imply
	// they are: `owned` and `escaped` count DELIVERY forms, while `guarded`
	// counts every call that sits in a body carrying the identity machinery —
	// which includes the guarded DOOR itself (`testutil.KillIfIdentical(…)`), a
	// call that is not a delivery form at all.
	t.Logf("signal-delivery gate: walked %d %s file(s) in %s, parsed %d; %d signal-bearing site(s) → %d delivery form(s), of which %d owned-handle and %d escaped; %d identity-guarded (all forms); %d group-addressed (censused, not adjudicated here); %d unclassified",
		census.filesWalked, signalIdentityGateTestSuffix, dir, census.filesParsed,
		census.signalSites, census.deliverySites, census.ownedSites, census.escapedSites, census.guardedSites,
		census.groupSites, census.unclassifiedSites)

	if census.filesParsed < signalIdentityGateMinFiles {
		t.Fatalf("the gate parsed only %d %s file(s) (floor %d) — the walk found nothing, so its clean line would be a pass over an empty set",
			census.filesParsed, signalIdentityGateTestSuffix, signalIdentityGateMinFiles)
	}
	if census.signalSites < signalIdentityGateMinSignalSites {
		t.Fatalf("the gate CLASSIFIED only %d signal-bearing site(s) (floor %d) — the recogniser stopped matching, so a tree full of bare deliveries would read as clean",
			census.signalSites, signalIdentityGateMinSignalSites)
	}
	if census.ownedSites < signalIdentityGateMinOwnedSites {
		t.Fatalf("the gate recognised only %d OWNED-HANDLE delivery site(s) (floor %d) — the ownership predicate stopped matching, so every one of those sites would silently become an escape nobody reviewed",
			census.ownedSites, signalIdentityGateMinOwnedSites)
	}
	if census.groupSites < signalIdentityGateMinGroupSites {
		t.Fatalf("the gate recognised only %d GROUP-addressed site(s) (floor %d) — that census is the pointer to the review that owns the group direction, and an emptied census reads as 'there is nothing to review'",
			census.groupSites, signalIdentityGateMinGroupSites)
	}
	if census.unclassifiedSites > 0 {
		t.Errorf("%d SIGINT-bearing call(s) fit neither the delivery forms nor the guarded bucket — a site this gate cannot classify is a site it is not checking; name it in signalIdentityGateNonDeliveryCalls (with the reason) or route it through the guard:\n  %s",
			census.unclassifiedSites, strings.Join(census.unclassifiedAt, "\n  "))
	}

	if len(findings) == 0 {
		// THE ZERO HAS TO EARN ITS MEANING. "No findings" is only good news if
		// the guarded door is actually present and measurable: on a tree where
		// the recogniser had stopped matching, or where the guard had been
		// deleted along with the bare calls, this list would be empty for the
		// wrong reason — the same green over the same nothing.
		if census.guardedSites < signalIdentityGateMinGuardedSites {
			t.Fatalf("no findings, but the gate recognised only %d GUARDED delivery site(s) (floor %d) — this zero means the guard is gone, not that every delivery uses it",
				census.guardedSites, signalIdentityGateMinGuardedSites)
		}
		return
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%d test site(s) deliver a signal WITHOUT an identity or ownership check.\n"+
		"A pid is a NUMBER THE KERNEL RECYCLES: signalling it is a statement about the number, not\n"+
		"about the process behind it, and on this host that reached `systemd --user` five times in one\n"+
		"boot and stopped the operator's GNOME session (journalctl -b 'Received SIGINT from PID …').\n"+
		"Capture the identity once (`testutil.ReadProcIdentity`) and deliver through it\n"+
		"(`testutil.KillIfIdentical(id, syscall.SIGTERM)`); a site that cannot use that door carries\n"+
		"`%s <reason>` AND an identity check in the same function body.\n",
		len(findings), signalIdentityGateEscape)
	for _, f := range findings {
		fmt.Fprintf(&b, "\n  %s\n      %s\n", f, f.snippet)
	}
	t.Error(b.String())
}

// TestSignalIdentityGateCatchesEverySpelling is the gate's own RED PROOF at the
// smallest sample (Rule 10): one planted violation per spelling must produce
// exactly one finding each, and every legal shape must produce none. Without
// this a predicate that silently stopped matching would leave the whole tree
// green while refusing nothing.
func TestSignalIdentityGateCatchesEverySpelling(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantForm    string // empty = must NOT be flagged
		wantGuarded bool   // the legal shape is a GUARDED site
		wantLine    int    // 0 = the fixture's single-line default
	}{
		{
			name:     "pid form",
			body:     "syscall.Kill(pid, syscall.SIGINT)",
			wantForm: "syscall.Kill(pid, syscall.SIGINT)",
		},
		{
			name:     "the pre-fix os.Interrupt spelling",
			body:     "syscall.Kill(pid, os.Interrupt)",
			wantForm: "syscall.Kill(pid, os.Interrupt)",
		},
		{
			name:     "the *os.Process form",
			body:     "proc.Signal(syscall.SIGINT)",
			wantForm: "Process.Signal(syscall.SIGINT)",
		},
		{
			name:     "the *os.Process form, os.Interrupt",
			body:     "proc.Signal(os.Interrupt)",
			wantForm: "Process.Signal(os.Interrupt)",
		},
		{
			name: "a liveness probe is not a delivery",
			body: "syscall.Kill(pid, 0)",
		},
		{
			name: "registration is not a delivery",
			body: "signal.Ignore(syscall.SIGINT)",
		},
		{
			name:     "the signal bound to a variable first",
			body:     "sig := syscall.SIGINT\n\tsyscall.Kill(pid, sig)",
			wantForm: "syscall.Kill(pid, sig)",
			wantLine: 13,
		},
		{
			name:        "the GUARDED door",
			body:        "testutil.KillIfIdentical(id, syscall.SIGINT)",
			wantGuarded: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package main\n\nimport (\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n\n\t\"github.com/nickhuang99/gopi/internal/testutil\"\n)\n\nfunc f(pid int, proc *os.Process, id testutil.ProcIdentity) {\n\t" + tc.body + "\n}\n"
			path := filepath.Join(dir, "fixture_test.go")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			findings, census, err := signalIdentityGateScanFile(path)
			if err != nil {
				t.Fatalf("scan fixture: %v", err)
			}
			if tc.wantForm == "" {
				if len(findings) != 0 {
					t.Fatalf("the gate flagged a legal shape %q:\n%v", tc.body, findings)
				}
				if tc.wantGuarded && census.guardedSites != 1 {
					t.Errorf("the guarded shape was not counted as a guarded site (census %+v) — a guard nobody counts is a guard nobody can tell from a deleted one", census)
				}
				if !tc.wantGuarded && census.guardedSites != 0 {
					t.Errorf("a non-delivery shape was counted as GUARDED (census %+v) — the bucket must mean 'delivered through the identity'", census)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("the gate reported %d findings for %q, want exactly 1 (it must be red on it and quiet about nothing else):\n%v",
					len(findings), tc.body, findings)
			}
			if findings[0].form != tc.wantForm {
				t.Fatalf("finding form = %q, want %q", findings[0].form, tc.wantForm)
			}
			wantLine := tc.wantLine
			if wantLine == 0 {
				wantLine = 12
			}
			if findings[0].line != wantLine {
				t.Errorf("finding landed on line %d, want %d — the reported line must be the call's own", findings[0].line, wantLine)
			}
			if census.signalSites != 1 || census.deliverySites != 1 {
				t.Errorf("census = %+v, want exactly 1 signal site and 1 delivery — the read-floors measure these", census)
			}
		})
	}
}

// TestSignalIdentityGateCountsCommandPositionsNotMentions is the MINIMAL
// REPRODUCTION of the false positive this gate shipped with (Rule 10-5), and it
// is the anti-regression for the shape rather than for one file.
//
// The measured defect (worker_daemon_foreground_reap_test.go:105):
//
//	t.Fatalf("the foreground worker's SysProcAttr.Pdeathsig = %v, want %v (SIGKILL) — "+
//		"without it the worker outlives a SIGKILLed test binary …", got, syscall.SIGKILL)
//
// Nothing about that line DELIVERS a signal: it is an assertion that FORMATS one,
// and its "SIGKILL" appears three ways — inside the format string's text, inside
// a struct-field selector, and as a value handed to a formatter. The pre-fix
// recogniser scanned the ARGUMENTS of every call, so this FORMATTER became a
// signal-bearing site in the loud "unclassified" bucket and the whole package
// went red. (It was never the string literal: the file is parsed without
// ParseComments and a *ast.BasicLit is inspected by nothing — measured, the
// literal-only fixture classified 0 sites.)
//
// The criterion now is Rule 36's, applied here: an invocation counts where the
// COMMAND stands, never where its text is quoted. So this test pins both halves
// in ONE fixture — the mention contributes nothing, and the real call one line
// away is still a delivery — because a green reached by widening the exclusion
// until the delivery disappeared would be the same green over nothing.
func TestSignalIdentityGateCountsCommandPositionsNotMentions(t *testing.T) {
	src := `package main

import (
	"os"
	"syscall"
	"testing"
)

func f(t *testing.T, got syscall.Signal, pid int) {
	// A formatter's FORMAT STRING quotes the signal's name; the parser never
	// reads a BasicLit, so this half has always been safe and must stay so.
	t.Fatalf("the worker outlives a SIGKILLed test binary (SIGKILL)")

	// A formatter whose ARGUMENT is the signal — the measured defect. A mention
	// is not an invocation: t.Fatalf is the callee, and it delivers nothing.
	t.Logf("want %v (SIGKILL) — got %v", syscall.SIGKILL, got)

	// A struct field selector reads a value; it signals nothing.
	_ = got

	// THE REAL CALL, one line away: this is a command position and stays one.
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "mentions_test.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	findings, census, err := signalIdentityGateScanFile(path)
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if census.signalSites != 1 {
		t.Fatalf("the fixture produced %d signal-bearing site(s), want exactly 1 — the two MENTIONS (a format string's text and a formatter's signal argument) are not invocations, and the one real call is: %+v",
			census.signalSites, census)
	}
	if census.deliverySites != 1 {
		t.Fatalf("the real call was not counted as a delivery (census %+v) — a recogniser that stopped matching the call to stay quiet about the mention would pass this test for the wrong reason", census)
	}
	if census.unclassifiedSites != 0 {
		t.Fatalf("%d site(s) landed in the unclassified bucket (at %v) — a mention must never enter it: that is the false positive this arm exists to pin",
			census.unclassifiedSites, census.unclassifiedAt)
	}
	if len(findings) != 1 {
		t.Fatalf("the gate reported %d finding(s), want exactly 1 (the real call, and nothing for the mentions): %v", len(findings), findings)
	}
	if findings[0].form != "syscall.Kill(pid, syscall.SIGKILL)" {
		t.Errorf("the finding names the form %q — it must name the CALL, not the mention beside it", findings[0].form)
	}
}

// TestSignalIdentityGateEscapeIsCheckedNotDeclared pins the ALLOW-LIST's
// boundary: the marker alone is NOT enough, and the same marker inside a
// function that does carry the identity check IS enough. A gate that honoured a
// bare marker would let the defect back in one comment at a time.
func TestSignalIdentityGateEscapeIsCheckedNotDeclared(t *testing.T) {
	const header = "package main\n\nimport (\n\t\"syscall\"\n\n\t\"github.com/nickhuang99/gopi/internal/testutil\"\n)\n\n"

	t.Run("a marker with no identity check is still red", func(t *testing.T) {
		dir := t.TempDir()
		src := header + "func f(pid int) {\n\t// " + signalIdentityGateEscape + " this child is mine, honestly\n\t_ = syscall.Kill(pid, syscall.SIGINT)\n}\n"
		path := filepath.Join(dir, "unchecked_test.go")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		findings, census, err := signalIdentityGateScanFile(path)
		if err != nil {
			t.Fatalf("scan fixture: %v", err)
		}
		if census.deliverySites != 1 || census.escapedSites != 1 {
			t.Fatalf("census = %+v, want 1 delivery / 1 escaped — the fixture did not exercise the boundary", census)
		}
		if len(findings) != 1 {
			t.Fatalf("a marker with NO identity check in the body produced %d findings, want 1 — the allow-list would be a comment, not a check:\n%v",
				len(findings), findings)
		}
		if findings[0].escape == "" {
			t.Error("the finding does not name the marker that failed to satisfy it — the reader cannot tell why the escape was refused")
		}
	})

	t.Run("a marker inside a body that DOES check identity is honoured", func(t *testing.T) {
		dir := t.TempDir()
		src := header + "func f(pid int) {\n\tid, err := testutil.ReadProcIdentity(pid)\n\tif err != nil {\n\t\treturn\n\t}\n\t// " + signalIdentityGateEscape + " delivered only while it is the recorded incarnation\n\t_ = syscall.Kill(id.Pid, syscall.SIGINT)\n}\n"
		path := filepath.Join(dir, "checked_test.go")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		findings, census, err := signalIdentityGateScanFile(path)
		if err != nil {
			t.Fatalf("scan fixture: %v", err)
		}
		if census.deliverySites != 1 || census.escapedSites != 1 || census.guardedSites != 1 {
			t.Fatalf("census = %+v, want 1 delivery / 1 escaped / 1 guarded", census)
		}
		if len(findings) != 0 {
			t.Fatalf("a marker whose body reads the identity was refused anyway:\n%v", findings)
		}
	})
}

// TestSignalIdentityGateRefusesAnEmptyDirectory pins the read-floor's other
// half: a scan over a directory holding no test sources must be SEEN as empty
// (0/0) rather than mistaken for a clean tree.
func TestSignalIdentityGateRefusesAnEmptyDirectory(t *testing.T) {
	findings, census := signalIdentityGateScan(t, t.TempDir())
	if census.filesParsed != 0 || len(findings) != 0 {
		t.Fatalf("an empty directory reported parsed=%d findings=%d, want 0/0", census.filesParsed, len(findings))
	}
	if census.filesParsed >= signalIdentityGateMinFiles {
		t.Fatalf("the floor (%d) is not above an empty walk — it cannot detect a scan that read nothing", signalIdentityGateMinFiles)
	}
}
