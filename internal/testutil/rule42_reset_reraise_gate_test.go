package testutil

// Rule 42 (destructive process signals) — the `signal.Reset`-to-re-raise half,
// as a CHECK.
//
// WHY THIS FILE EXISTS. PROJECT.md's Rule 42 cites `go test ./internal/testutil/
// -run Rule42` as its machine check. On master that command matched NO test:
// `go test` printed "no tests to run" and exited 0, so the rule's own check was
// a vacuum that read green (measured 2026-10-24, this tree —
// `go test ./internal/testutil/ -run Rule42 -v` → "[no tests to run] PASS").
// A named check that does not exist is worse than no check at all, because its
// green is indistinguishable from a clean tree. This file is the check.
//
// WHAT THE RULE BANS, exactly. Not `signal.Reset` in general — Reset has honest
// uses (a test un-ignoring a signal it ignored) — but RESET USED TO RE-RAISE:
// `signal.Reset(sig)` immediately followed by a self-kill, so that the process
// dies by the signal's default action. That spelling is banned because Reset is
// `cancel(sig, disableSignal)` over EVERY registered channel: restoring "the"
// disposition DISMANTLES a handler another package installed, as a side effect
// of this component's own exit. Measured twice in this repository — a subprocess
// lost its os/signal handler, and internal/testutil/suiterootsignal.go records
// the 90s hang that followed.
//
// THE OBSERVATION IS WINDOWED, because that is how the defect is written. A
// re-raise is a RESET and a `Kill(Getpid(), …)` within a few lines of each
// other, in that order; requiring them on adjacent lines would let a comment
// between them launder the site, and scanning the whole file would classify an
// unrelated kill elsewhere in it as part of the same act (measured: the
// worker-control e2e fixture both resets a signal it ignored and signals a
// CHILD, and the child kill must not make its fixture reset a re-raise).
//
// THE ESCAPE IS PER SITE AND IT IS THE RULE'S OWN (Rule 42's "annotate + a test
// that fails if the silent form comes back"): a re-raise that genuinely cannot
// reach the default disposition any other way carries `rule42-ok: <reason>` on
// the statement or the line above, naming WHY and WHICH measurement proves the
// alternative fails. There are NO such sites in this tree as of this change —
// the four that existed (internal/tools/testmain_test.go, internal/signalrec,
// internal/lineedit/raw.go, cmd/agent/merge_run.go) were each replaced by
// releasing every registration that means to die, and the alternative was
// measured failing (see the report). The escape exists so the NEXT one can be
// reviewed instead of silently re-adopted; when it is used, this gate reports
// it, so an annotation is a loud decision and never a quiet one.
//
// RATCHET-DOWN (Rule 39's discipline): an escape annotation attached to NOTHING
// — whose statement is no longer a re-raise, or whose site is gone — is a hard
// error, not a harmless leftover. A permit must be deleted in the same change
// that fixes the site, or the table ratchets up by neglect.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// rule42ResetCall is the banned call, composed rather than written whole so
	// this file does not match its own scan (the gate walks the working tree).
	rule42ResetCall = "signal" + "." + "Reset("
	// rule42Escape is the rule's per-site escape: annotate the site, name the
	// reason and the measurement that proves no release-only form works.
	rule42Escape = "rule42-ok:"
	// rule42KillWindow is how far below a `signal.Reset(` its self-kill may
	// appear and still be the SAME act. A re-raise is written as two adjacent
	// statements; the window tolerates a comment between them (which is how
	// every real site in this repo's history was written) and nothing more. Ten
	// lines is comfortably above the widest such gap seen and far below a
	// function length, so an unrelated child-kill elsewhere cannot be read as
	// part of it.
	rule42KillWindow = 10
	// rule42ScanFileCap bounds the walk. A cap that is hit is DISCLOSED (Rule
	// 15: never a silent truncation that reads like a pass).
	rule42ScanFileCap = 6000
	// rule42ScanFloor is the non-vacuity floor, in the same spirit as Rule 36's
	// corpus gate: a scan that judged almost nothing is a broken scan, not a
	// clean tree. This repository has thousands of Go files; a floor in the low
	// hundreds passes on any real checkout and fails on a mis-rooted walk.
	rule42ScanFloor = 200
)

// rule42Site is one `signal.Reset(` call found in the tree, with the reading the
// gate needs: whether it is a re-raise, and whether it carried the escape.
type rule42Site struct {
	path      string // path relative to the scan root, for a stable report
	line      int    // 1-based line of the Reset call
	text      string // the call's own line, trimmed
	reraise   bool   // a self-kill follows within rule42KillWindow
	annotated bool   // the escape comment is on the statement or the line above
}

// rule42Root finds the module root from the test's working directory (this
// package's tests run in internal/testutil).
func rule42Root(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Dir(filepath.Dir(wd))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module root %s holds no go.mod (%v) — the scan would judge nothing and "+
			"read green for the wrong reason", root, err)
	}
	return root
}

// rule42IsSelfKill reports whether one line raises a signal at THIS process.
// `syscall.Kill(os.Getpid(), …)` and `syscall.Kill(syscall.Getpid(), …)` are the
// two spellings in this tree; a kill aimed at any other pid is out of scope by
// construction (Rule 42's ban is about re-reaching yourself, and a child kill is
// the caller's own handle).
func rule42IsSelfKill(line string) bool {
	if !strings.Contains(line, "Kill(") {
		return false
	}
	return strings.Contains(line, "Getpid()")
}

// rule42Scan reads every .go file under root and returns the Reset sites it
// found. Untracked files are INCLUDED on purpose: a re-raise written into a
// scratch file is still a re-raise the next reader will copy.
func rule42Scan(t *testing.T, root string) (sites []rule42Site, filesWalked int, capped bool) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// The repository's own harness keeps whole checkouts under
			// .git and (on some hosts) a scratch root; neither is source.
			switch info.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filesWalked >= rule42ScanFileCap {
			capped = true
			return filepath.SkipAll
		}
		filesWalked++
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Errorf("reading %s: %v — a file the gate cannot read is NOT a pass", path, rerr)
			return nil
		}
		lines := strings.Split(string(data), "\n")
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		for i, line := range lines {
			if !strings.Contains(line, rule42ResetCall) {
				continue
			}
			// A comment that merely MENTIONS the call is documentation, not a
			// site — and this repository has many (each one explains why the
			// spelling is banned). Only a line whose non-comment part carries
			// the call counts.
			if idx := strings.Index(line, rule42ResetCall); idx > 0 {
				if strings.Contains(line[:idx], "//") {
					continue
				}
			} else if idx < 0 {
				continue
			}
			site := rule42Site{path: rel, line: i + 1, text: strings.TrimSpace(line)}
			for j := i + 1; j < len(lines) && j <= i+rule42KillWindow; j++ {
				if rule42IsSelfKill(lines[j]) {
					site.reraise = true
					break
				}
			}
			// The escape rides the statement's own line or the line above.
			if strings.Contains(line, rule42Escape) {
				site.annotated = true
			} else if i > 0 && strings.Contains(lines[i-1], rule42Escape) {
				site.annotated = true
			}
			sites = append(sites, site)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Slice(sites, func(a, b int) bool {
		if sites[a].path != sites[b].path {
			return sites[a].path < sites[b].path
		}
		return sites[a].line < sites[b].line
	})
	return sites, filesWalked, capped
}

// rule42Judge applies the rule to the sites and returns the violations.
func rule42Judge(sites []rule42Site) (violations, annotated, benign []string) {
	for _, s := range sites {
		at := s.path + ":" + itoa42(s.line)
		switch {
		case s.reraise && s.annotated:
			annotated = append(annotated, at+"  ("+s.text+")")
		case s.reraise:
			violations = append(violations, at+"  ("+s.text+")")
		default:
			benign = append(benign, at+"  ("+s.text+")")
		}
	}
	return violations, annotated, benign
}

func itoa42(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestRule42ResetToReraiseIsBannedOrAnnotated is the check PROJECT.md cites.
//
// It prints EVERY site it finds (the ones that pass, the ones that are annotated
// and why), because a check whose findings are invisible cannot be reviewed: the
// operator must be able to see that a re-raise which stays is a DECISION.
func TestRule42ResetToReraiseIsBannedOrAnnotated(t *testing.T) {
	root := rule42Root(t)
	sites, walked, capped := rule42Scan(t, root)
	if capped {
		t.Fatalf("the scan hit its %d-file cap — this run judged a fragment of the tree and cannot "+
			"report a clean result", rule42ScanFileCap)
	}
	if walked < rule42ScanFloor {
		t.Fatalf("the scan judged only %d Go file(s) under %s (floor %d) — a walk that read almost "+
			"nothing is a broken walk, not a clean tree", walked, root, rule42ScanFloor)
	}
	violations, annotated, benign := rule42Judge(sites)
	t.Logf("Rule 42 — signal.Reset-to-re-raise: %d file(s) walked, %d site(s) of %s, %d benign, %d annotated, %d violation(s)",
		walked, len(sites), rule42ResetCall, len(benign), len(annotated), len(violations))
	for _, b := range benign {
		t.Logf("  benign (no self-kill follows): %s", b)
	}
	for _, a := range annotated {
		t.Logf("  ANNOTATED re-raise (reviewed escape): %s", a)
	}
	for _, v := range violations {
		t.Errorf("  VIOLATION: %s — a re-raise whose default disposition is restored by a BLANKET "+
			"signal.Reset, which dismantles every other package's registration for that signal. Release "+
			"each subscription that means to die (signalrec.ReleaseSignalHandlers, a package's own "+
			"signal.Stop, testutil.JoinSuiteRunRootSignalHandler) and re-raise; if that provably cannot "+
			"reach the default action, annotate the statement with `%s <why + which measurement>`", v, rule42Escape)
	}
	if len(violations) > 0 {
		t.Fatalf("%d unannotated signal.Reset-to-re-raise site(s) — see the lines above", len(violations))
	}
	// RATCHET-DOWN: an escape that now guards nothing is an error. This is the
	// half that keeps the permit table honest — the site it was written for was
	// repaired, so the permit must go in the SAME change (Rule 39's discipline).
	for _, s := range sites {
		if s.annotated && !s.reraise {
			t.Errorf("%s:%d carries a %s escape but is NOT a re-raise any more (no self-kill within %d "+
				"lines) — the site was repaired or the annotation moved, so the permit must be deleted "+
				"in the same change; a permit that still matches nothing ratchets the rule up by neglect",
				s.path, s.line, rule42Escape, rule42KillWindow)
		}
	}
}

// TestRule42GateRedProof proves the gate is not a vacuum: it must flag a
// re-raise it is given, and it must accept the same code once annotated. Run on
// a throwaway tree, so the proof cannot be satisfied by the repository's own
// state (a gate that only ever reads green proves nothing — the defect this file
// repairs was exactly a cited check that read green while judging nothing).
func TestRule42GateRedProof(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Arm 1: the defect, in the spelling all four real sites used.
	write("red.go", `package probe

import (
	"os"
	"os/signal"
	"syscall"
)

func f() {
	sig := syscall.SIGINT
	signal.Stop(make(chan os.Signal))
	`+rule42ResetCall+`sig)
	_ = syscall.Kill(os.Getpid(), sig)
}
`)
	// Arm 2: the same act, reviewed and annotated.
	write("annotated.go", `package probe

import (
	"os"
	"os/signal"
	"syscall"
)

func g() {
	sig := syscall.SIGINT
	signal.Stop(make(chan os.Signal))
	// `+rule42Escape+` measured: no release-only form reaches the default action here.
	`+rule42ResetCall+`sig)
	_ = syscall.Kill(os.Getpid(), sig)
}
`)
	// Arm 3: an honest Reset — a fixture un-ignoring a signal it ignored. No
	// self-kill follows, so it must NOT be reported as a re-raise.
	write("benign.go", `package probe

import (
	"os/signal"
	"syscall"
)

func h() {
	signal.Ignore(syscall.SIGINT)
	`+rule42ResetCall+`syscall.SIGINT)
}
`)
	// Arm 4: a comment that merely names the call — documentation, not a site.
	write("comment.go", `package probe

// Why `+rule42ResetCall+` is banned: it cancels every registration.
func i() {}
`)
	sites, walked, _ := rule42Scan(t, dir)
	if walked != 4 {
		t.Fatalf("the red-proof walk judged %d file(s), want 4 — the fixture is not what the arms "+
			"assume, so this proof would assert about the wrong tree", walked)
	}
	violations, annotated, benign := rule42Judge(sites)
	if len(sites) != 3 {
		t.Fatalf("found %d site(s) in the fixture, want 3 (the comment arm must not count): %+v", len(sites), sites)
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "red.go:") {
		t.Fatalf("the un-annotated re-raise was NOT flagged (violations=%v) — the gate is a vacuum", violations)
	}
	if len(annotated) != 1 || !strings.Contains(annotated[0], "annotated.go:") {
		t.Fatalf("the annotated re-raise was not reported as a reviewed escape (annotated=%v)", annotated)
	}
	if len(benign) != 1 || !strings.Contains(benign[0], "benign.go:") {
		t.Fatalf("the honest (non-re-raise) Reset was misclassified (benign=%v) — a gate that fails an "+
			"honest use is a gate people route around", benign)
	}
	// Arm 5 — the RATCHET. An escape on a Reset that is no longer a re-raise is
	// an error, so a permit whose site was repaired cannot survive its fix.
	ratchet := filepath.Join(t.TempDir())
	if err := os.WriteFile(filepath.Join(ratchet, "stale.go"), []byte(`package probe

import (
	"os/signal"
	"syscall"
)

func j() {
	// `+rule42Escape+` this site was repaired and the permit was not deleted.
	`+rule42ResetCall+`syscall.SIGINT)
}
`), 0o600); err != nil {
		t.Fatalf("write the ratchet fixture: %v", err)
	}
	rsites, _, _ := rule42Scan(t, ratchet)
	if len(rsites) != 1 || !rsites[0].annotated || rsites[0].reraise {
		t.Fatalf("the ratchet fixture did not produce the state it exists to describe: %+v", rsites)
	}
	// The assertion the real gate makes (duplicated here because the real one
	// reads the repository, where no such row exists by design).
	if !(rsites[0].annotated && !rsites[0].reraise) {
		t.Fatalf("the ratchet condition is not expressible: %+v", rsites[0])
	}
}
