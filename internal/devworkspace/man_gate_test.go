package devworkspace

// Tests for man_gate.go — the delivery channel's MAN-SOURCE gate.
//
// The fixture is a REAL repository driven through the channel's REAL steps
// (Open → commit → AttemptDelivery), because the whole question is what the
// BINARY this attempt built prints: only a real build and a real generator run
// can answer it, and a stub generator would prove nothing about the binary the
// gate reads.
//
// The arms are a MATCHED PAIR over ONE fixture (a man source whose generated
// block the fixture generator splices from the build's own output):
//
//	arm 1 (red)   — the source still says OLD while the help text now says NEW:
//	                the build wins, the region is STALE, the delivery is refused
//	                and the blocker NAMES the page.
//	arm 2 (green) — the same tree with the region already regenerated: the
//	                delivery goes through and the gate says nothing.
//
// A gate that "always refuses" fails arm 2; a gate that never refuses fails arm
// 1; a gate reading a PRE-BUILD binary (or the source tree instead of the
// binary) fails arm 1 as well, because the drift it must see is created by the
// build the attempt runs.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/mansource"
)

const (
	// manFixtureModule is the module line a fixture generator's `go run` needs:
	// without one, `go run <file>` refuses outside GOPATH (measured: "go.mod file
	// not found in current directory or any parent directory"), so the fixture
	// repo carries the minimum that makes the gate's real invocation work.
	manFixtureModule = "module fixture\n\ngo 1.21\n"

	// manFixturePage is the fixture man source the generator rewrites: a REAL
	// path under the SHARED source directory (the gate looks that directory up by
	// its shared spelling, so the fixture must use it too).
	manFixturePage = mansource.SourceDirRel + "/gopi-version.md"

	// manFixtureBinary is where the fixture's declared build writes its binary —
	// the SHARED artifact path the gate hands to the generator.
	manFixtureBinary = mansource.BuiltBinaryRel

	// manFixtureHelp is what the fixture binary prints: the build writes it, so
	// the help text a source must match is decided by the BUILD, not by the tree.
	manFixtureHelp = "new"

	// manFixtureStale is the OLD help text arm 1 leaves in the source.
	manFixtureStale = "old"

	// manFixturePageCount is how many man sources the fixture tree carries — ONE.
	// It is the number the green arm's measurement sentence must report, so the
	// arm pins the COUNT and not merely the presence of a sentence: a gate that
	// printed "0 page(s) compared" would otherwise pass an arm whose whole job is
	// to refuse a green that compared nothing.
	manFixturePageCount = 1
)

// manFixtureBuildCommand is the fixture's DECLARED build: it writes a script at
// the shared artifact path that prints the CURRENT help text from a file the test
// can move. That indirection is what lets one build command serve both arms and
// lets arm 1 prove the gate read the binary this build produced — the help text
// changes at BUILD time, after the tree was committed.
func manFixtureBuildCommand() string {
	return "mkdir -p bin && printf '#!/bin/sh\\necho %s\\n' \"$(cat help.txt)\" > " +
		manFixtureBinary + " && chmod +x " + manFixtureBinary
}

// manFixtureGenerator is the fixture's generator: a REAL program at the SHARED
// generator path, parsing the invocation the gate documents
// (`<root> <binary> [--check]`) and splicing the binary's output into the page's
// SYNOPSIS region — the same contract the shipped generator has, on a page whose
// body is a single line, so the fixture stays readable.
// manFixtureGenerator renders the fixture's generator SOURCE: a real program at
// the SHARED generator path, parsing the invocation the gate documents
// (`<root> <binary> [--check]`) and splicing the binary's output into the page's
// SYNOPSIS region — the shipped generator's contract, on a page whose body is a
// single line, so the fixture stays readable.
//
// IT IMPORTS NOTHING FROM THIS MODULE, and that is measured, not stylistic: the
// fixture repo is its own module (that is what `go.mod` is there for), so a
// `go run` inside it cannot resolve `github.com/nick/gopi/internal/mansource` —
// the first version did exactly that and the gate abstained with
// "no required module provides package …", i.e. the red arm would have PASSED for
// the wrong reason. The marker strings are therefore spliced into the source from
// the SHARED constants (Rule 14: one definition, referenced here), so the fixture
// still cannot disagree with the real writer about where a region begins.
func manFixtureGenerator() string {
	return `package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	usage     = "usage: fixture <root> <binary> [--check]"
	checkFlag = "--check"
	pageRel   = "` + manFixturePage + `"
	stepFloor = 2
	beginMark = "` + mansource.SynopsisBegin + `"
	endMark   = "` + mansource.SynopsisEnd + `"
)

// staleView is the verdict line the fixture generator prints for a drifted page:
// the SAME token the shipped generator prints (mansource.StaleToken), so the
// gate's recogniser and this fixture cannot disagree about what "stale" says.
const staleView = " — ` + mansource.StaleToken + ` (regenerate with ` + mansource.RegenerateHint + `)"

// block is mansource.Block's shape: the begin marker, a newline, the body, the
// end marker and a trailing newline.
func block(body string) string {
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return beginMark + "\n" + body + endMark + "\n"
}

// splice replaces the region between the markers with fresh — the one operation
// the gate is measuring.
func splice(doc, fresh string) (string, error) {
	start := strings.Index(doc, beginMark)
	if start < 0 {
		return "", fmt.Errorf("begin marker not found")
	}
	stop := strings.Index(doc[start:], endMark)
	if stop < 0 {
		return "", fmt.Errorf("end marker not found")
	}
	stop += start + len(endMark)
	if stop < len(doc) && doc[stop] == '\n' {
		stop++
	}
	return doc[:start] + fresh + doc[stop:], nil
}

func main() {
	args := os.Args[1:]
	check := false
	var pos []string
	for _, a := range args {
		if a == checkFlag {
			check = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) < stepFloor {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	root, bin := pos[0], pos[1]
	out, err := exec.Command(bin).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %s: %v\n", bin, err)
		os.Exit(2)
	}
	help := strings.TrimSpace(string(out))
	page := filepath.Join(root, filepath.FromSlash(pageRel))
	doc, err := os.ReadFile(page)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %v\n", err)
		os.Exit(2)
	}
	updated, err := splice(string(doc), block("\n"+help+"\n"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %s: %v\n", pageRel, err)
		os.Exit(2)
	}
	if updated == string(doc) {
		fmt.Printf("mancensus: %s — ` + mansource.UpToDateToken + `\n", pageRel)
		return
	}
	if check {
		fmt.Printf("mancensus: %s%s\n", pageRel, staleView)
		os.Exit(1)
	}
	if err := os.WriteFile(page, []byte(updated), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("mancensus: %s — regenerated\n", pageRel)
}
`
}

// manFixturePageDoc renders the fixture man source whose SYNOPSIS region carries
// body — the one line the generator compares against the binary's output.
//
// It is composed with mansource.Block, the SAME function the generator splices
// with, so the committed page and the regenerated page are byte-identical for the
// same body: a fixture that spelled the markers by hand would drift on whitespace
// and make the green arm unreachable for a reason that has nothing to do with the
// gate (measured: the first version added one newline and both arms committed
// nothing).
func manFixturePageDoc(body string) string {
	block := mansource.Block(mansource.SynopsisBegin, mansource.SynopsisEnd, "\n"+body+"\n")
	return "# gopi-version (fixture)\n\n" + block + "\n"
}

// manFixtureOrigin builds an origin repository carrying everything the gate needs
// to ask its question: the module line, the generator, a man source written
// against the OLD help text, and the OLD help text itself. Both arms then move the
// help text (the build reads it), so "the source is stale" is created by the
// branch rather than by the fixture's first commit.
func manFixtureOrigin(t *testing.T) string {
	t.Helper()
	origin := newOrigin(t)
	write := func(rel, content string) {
		path := filepath.Join(origin, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", manFixtureModule)
	write(mansource.GeneratorRel, manFixtureGenerator())
	write(manFixturePage, manFixturePageDoc(manFixtureStale))
	write("help.txt", manFixtureStale)
	gitIn(t, origin, "add", "go.mod", mansource.GeneratorRel, manFixturePage, "help.txt")
	gitIn(t, origin, "commit", "-q", "-m", "the man fixture: module, generator, page, help text")
	return origin
}

// manFixtureWorkspace opens a workspace against origin, commits one edit on the
// row's own branch (so there IS something to deliver), and returns its path.
func manFixtureWorkspace(t *testing.T, origin string, edits ...[2]string) string {
	t.Helper()
	cfg := cloneCfg(t, origin)
	cfg.TaskID = testTaskID
	cfg.GroundBranch = groundBranchName
	cfg.BaseBranch = DefaultBranch
	cfg.Build = manFixtureBuildCommand()
	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	paths := make([]string, 0, len(edits))
	for _, e := range edits {
		path := filepath.Join(res.Dir, filepath.FromSlash(e[0]))
		if err := os.WriteFile(path, []byte(e[1]), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, e[0])
	}
	args := append([]string{"add"}, paths...)
	gitIn(t, res.Dir, args...)
	gitIn(t, res.Dir, "commit", "-q", "-m", "the row's own change")
	return res.Dir
}

// manFixtureCfg is the channel's config for the fixture, pointed at origin.
func manFixtureCfg(t *testing.T, origin string) Config {
	t.Helper()
	cfg := cloneCfg(t, origin)
	cfg.TaskID = testTaskID
	cfg.GroundBranch = groundBranchName
	cfg.BaseBranch = DefaultBranch
	cfg.Build = manFixtureBuildCommand()
	return cfg
}

// TestManGateRefusesADriftedSourceAndNamesThePage is the RED arm: the man source
// was written against the OLD help text, the branch moved the help text, and the
// delivery must be refused BEFORE the push — naming the STALE page and the
// generator's repair command, never a hand-written block.
func TestManGateRefusesADriftedSourceAndNamesThePage(t *testing.T) {
	origin := manFixtureOrigin(t)
	cfg := manFixtureCfg(t, origin)
	// The row's own change is one unrelated file: the man source stays exactly as
	// origin had it, and the DRIFT is created by the next commit, which moves the
	// help text this branch's binary prints — the shape of a row that edits a `-h`
	// text and forgets to regenerate.
	dir := manFixtureWorkspace(t, origin, [2]string{"note.txt", "the row's own change\n"})
	// The tree is written by the build, not by the commit: the build's own input
	// moves, which is why a pre-build check could not see this.
	if err := os.WriteFile(filepath.Join(dir, "help.txt"), []byte(manFixtureHelp), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "help.txt")
	gitIn(t, dir, "commit", "-q", "-m", "the help text this branch prints")

	before := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch))
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery returned an error, not a blocker: %v", err)
	}
	if blk == nil {
		t.Fatalf("the delivery was allowed although %s does not match the binary this attempt built "+
			"(pushed=%v detail=%q)", manFixturePage, res.Pushed, res.Detail)
	}
	t.Logf("the gate's own refusal:\n%s", blk.Report())
	if blk.Kind != BlockerKindMan {
		t.Fatalf("blocker kind = %q, want %q (words: %s)", blk.Kind, BlockerKindMan, blk.Words)
	}
	for _, want := range []string{manFixturePage, mansource.StaleToken, manGateRefusalPref, mansource.RegenerateHint} {
		if !strings.Contains(blk.Report(), want) {
			t.Errorf("the refusal does not name %q:\n%s", want, blk.Report())
		}
	}
	// THE BINARY WAS BUILT FIRST — that is the proof these words are about this
	// branch's help text and not a stale artifact's.
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(manFixtureBinary))); err != nil {
		t.Errorf("the gate refused before the declared build ran: %v", err)
	}
	if after := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch)); after != before {
		t.Fatalf("origin's %s moved (%s → %s) although the man gate refused the delivery",
			DefaultBranch, before, after)
	}
	if res.Pushed {
		t.Errorf("Result.Pushed is true on a refused delivery")
	}
}

// TestManGateStaysQuietWhenTheSourceMatchesTheBuiltBinary is the CONTROL arm: the
// same fixture, the same build, the source already regenerated — the delivery
// goes through, and the gate reports WHAT IT COMPARED without refusing anything.
//
// "Quiet" changed meaning with the measurement line, and the change is the point:
// a green half that printed nothing at all could not be told apart, in a delivery
// log, from a step that never ran (measured on row 01a1a3b5's log, 2026-11-04:
// `census gate`, `build` and `push` lines, zero man lines). So the arm now asserts
// BOTH halves of the shape — the measured sentence NAMES the page and a non-zero
// count, and the abstention sentence is absent because nothing was unmeasurable.
func TestManGateStaysQuietWhenTheSourceMatchesTheBuiltBinary(t *testing.T) {
	origin := manFixtureOrigin(t)
	cfg := manFixtureCfg(t, origin)
	// The same two moves as the red arm — the help text moves — but the source is
	// regenerated in the SAME commit, which is what `make man` produces.
	dir := manFixtureWorkspace(t, origin,
		[2]string{"help.txt", manFixtureHelp},
		[2]string{manFixturePage, manFixturePageDoc(manFixtureHelp)})

	var log strings.Builder
	cfg.Log = &log
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("the delivery was refused although the source matches the built binary (%s):\n%s",
			blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("the delivery did not push although nothing was wrong (detail=%q)", res.Detail)
	}
	if strings.Contains(log.String(), manGateNotePrefix) {
		t.Errorf("a matching source must not be reported as unmeasurable, but the gate abstained:\n%s", log.String())
	}
	// THE GREEN HALF SAYS WHAT IT MEASURED (deliverable: refuse a green that
	// compared nothing). Without this, "the gate ran and every page matched" and
	// "this step never executed" are the same log.
	measured := log.String()
	if !strings.Contains(measured, manGateMeasuredPrefix) {
		t.Fatalf("the gate matched the sources but never said what it compared — an auditable green "+
			"must name its page count (Rule 15); log:\n%s", measured)
	}
	if !strings.Contains(measured, manFixturePage) {
		t.Errorf("the measurement line does not name the page it compared (%s); log:\n%s", manFixturePage, measured)
	}
	// THE COUNT IS THE ANTI-VACUITY HALF: "measured 0 pages" would satisfy every
	// other assertion here while proving nothing ran. The fixture carries exactly
	// one page, so the sentence must carry that number.
	if !strings.Contains(measured, fmt.Sprintf("%s — %d page(s) compared and up to date: ",
		manGateMeasuredPrefix, manFixturePageCount)) {
		t.Errorf("the measurement line does not carry the count it compared (%d page(s)); log:\n%s",
			manFixturePageCount, measured)
	}
}

// TestManGateAbstainsOnATreeWithNoManSources pins the THIRD shape (Rule 15):
// a tree that carries no man sources at all is UNMEASURABLE, not clean — and it
// must say so instead of passing quietly, because a silent pass would make "no
// page here" and "every page is current" read the same.
func TestManGateAbstainsOnATreeWithNoManSources(t *testing.T) {
	origin := newOrigin(t)
	cfg := manFixtureCfg(t, origin)
	// One committed edit, so the delivery has something to publish; the tree has
	// no `man/` directory and no generator.
	dir := manFixtureWorkspace(t, origin, [2]string{"README.md", "origin, edited\n"})
	var log strings.Builder
	cfg.Log = &log
	if _, blk, err := AttemptDelivery(context.Background(), cfg, dir); err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	} else if blk != nil {
		t.Fatalf("a tree with no man sources must be unmeasurable, not refused (%s):\n%s", blk.Kind, blk.Report())
	}
	if !strings.Contains(log.String(), manGateNotePrefix) {
		t.Fatalf("the gate must SAY it could not measure this tree (Rule 15) — log:\n%s", log.String())
	}
}

// TestManGateMeasuresTheBinaryThisBranchBuiltNotAStaleOne pins the ONE ordering
// claim the gate makes (Rule 31: the fact source is the artifact this attempt
// produced): a PRE-BUILD binary that still prints the OLD help text must not be
// able to call a drifted source "up to date".
//
// The fixture makes the two answers DIFFER, which is what makes the arm decisive:
// the tree's committed source carries the OLD text and a stale `bin/gopi` (left
// over from a previous build) prints the OLD text too — so a gate that ran BEFORE
// the declared build would see a perfect match and pass. The declared build then
// writes a binary that prints the NEW text, and the delivery must be refused.
func TestManGateMeasuresTheBinaryThisBranchBuiltNotAStaleOne(t *testing.T) {
	origin := manFixtureOrigin(t)
	cfg := manFixtureCfg(t, origin)
	dir := manFixtureWorkspace(t, origin,
		[2]string{"help.txt", manFixtureHelp},
		[2]string{"note.txt", "the row's own change\n"})
	// THE STALE ARTIFACT: what a previous build left behind, printing the text the
	// committed source was written against.
	staleBin := filepath.Join(dir, filepath.FromSlash(manFixtureBinary))
	if err := os.MkdirAll(filepath.Dir(staleBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleBin, []byte("#!/bin/sh\necho "+manFixtureStale+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The premise, MEASURED: against the stale binary the source is current, so a
	// pre-build check would have nothing to report — and this arm would then be
	// indistinguishable from the green one.
	if out, err := runManGenerator(context.Background(), dir); err != nil || !strings.Contains(out, "up to date") {
		t.Fatalf("the fixture's premise does not hold: the stale binary must make the source current "+
			"(err=%v, output=%s)", err, out)
	}

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery returned an error, not a blocker: %v", err)
	}
	if blk == nil {
		t.Fatalf("a stale pre-build binary was taken for the fact source: the delivery went through "+
			"(%s) although the source does not match the binary this attempt built", res.Detail)
	}
	t.Logf("the gate's own refusal:\n%s", blk.Report())
	if blk.Kind != BlockerKindMan {
		t.Fatalf("blocker kind = %q, want %q (words: %s)", blk.Kind, BlockerKindMan, blk.Words)
	}
	if !strings.Contains(blk.Report(), manFixturePage) || !strings.Contains(blk.Report(), mansource.StaleToken) {
		t.Fatalf("the refusal must name the STALE page %s:\n%s", manFixturePage, blk.Report())
	}
}

// TestManGateRefusesAGreenThatComparedNothing pins the VACUITY floor: a generator
// that exits 0 having named no page is NOT "every page matches" — it is "nothing
// was compared", and before this arm existed the two were the same nil return.
//
// The arm drives the SHIPPED recogniser over the shipped generator's own two
// shapes rather than a fixture, because the question is about the GATE's reading
// and the fixture generator cannot be made to print an empty success without
// inventing a third program that agrees with nobody (Rule 15's own lesson: a proof
// that re-implements its subject proves nothing about it).
func TestManGateRefusesAGreenThatComparedNothing(t *testing.T) {
	// (1) THE VACUOUS OUTPUT: exit 0 with no verdict line at all. The page counter
	// must read zero, which is what the gate turns into an abstention.
	silent := ""
	if pages := manUpToDatePages(silent); len(pages) != 0 {
		t.Fatalf("an empty generator output yielded %d up-to-date page(s) (%v) — the counter invented a scan",
			len(pages), pages)
	}
	// (2) THE NOISE OUTPUT: lines that are not verdicts must not be counted either,
	// or a generator that printed a banner and compared nothing would read as a
	// successful scan.
	noise := "mancensus: building graph\nmancensus: 0 page(s) checked\n"
	if pages := manUpToDatePages(noise); len(pages) != 0 {
		t.Fatalf("non-verdict lines were counted as up-to-date pages (%v) — the recogniser is too loose", pages)
	}
	// (3) THE REAL SHAPE: the shipped generator's per-page line, read through the
	// shared token, must be counted — otherwise the floor refuses every healthy
	// tree and the gate becomes an always-abstain.
	real := fmt.Sprintf("mancensus: %s — %s\n", manFixturePage, mansource.UpToDateToken)
	if pages := manUpToDatePages(real); len(pages) != manFixturePageCount {
		t.Fatalf("the shipped verdict line yielded %d page(s) (%v), want %d — the green half cannot count what "+
			"the generator prints, so `%s` would refuse every real tree",
			len(pages), pages, manFixturePageCount, manGateMeasuredPrefix)
	}
	// (4) THE FLOOR ITSELF is what the gate compares against, and it must be
	// non-zero: a floor of 0 would restore the vacancy this arm exists to refuse.
	if manGateComparedFloor < 1 {
		t.Fatalf("manGateComparedFloor = %d: a floor that admits zero restores the vacuous green", manGateComparedFloor)
	}
}

// TestManGateReportsWhatItComparedOnTheRealTree is the MEASUREMENT arm, and it
// runs on THIS repository — the shipped generator, the shipped binary, the shipped
// pages — because the count it asserts is a fact about the real corpus and no
// fixture can stand in for it.
//
// It is the answer to the question a delivery log could not previously be asked:
// after a green attempt, how many pages did the man half actually compare? A run
// of this arm reports the number (12 on the rev it landed), and a tree whose man
// sources have drifted turns it red: exit≠0 naming the page, which is exactly the
// refusal the delivery channel then prints.
func TestManGateReportsWhatItComparedOnTheRealTree(t *testing.T) {
	root := repoRootDir(t)
	// The fact source is the binary, so the tree needs one; a tree that cannot
	// build one is UNMEASURABLE and this arm must say so rather than pass.
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(mansource.BuiltBinaryRel))); err != nil {
		t.Skipf("no built binary at %s (%v) — run `make build`; the man sources are measured against it, "+
			"never against a source constant", mansource.BuiltBinaryRel, err)
	}
	var log strings.Builder
	cfg := Config{Log: &log}
	blk := manCensusGate(context.Background(), cfg, root)
	got := log.String()
	if blk != nil {
		t.Fatalf("the real tree's man sources do not match its binary — regenerate with `%s` and commit "+
			"the result (the SYNOPSIS region is copied from `gopi <verb> -h`, never written by hand):\n%s",
			mansource.RegenerateHint, blk.Report())
	}
	// THE GREEN MUST CARRY ITS COUNT (the vacuity floor, one level up): a nil
	// blocker with no measurement line means the gate ran and reported nothing,
	// which is the shape this arm exists to rule out.
	if !strings.Contains(got, manGateMeasuredPrefix) {
		t.Fatalf("the gate passed the real tree without reporting what it compared — a green nobody can "+
			"audit; log:\n%s", got)
	}
	// THE NUMBER IS READ BACK FROM THE SENTENCE THE GATE PRINTED, not recounted
	// from the generator's own lines: the sentence is what an operator sees in a
	// delivery log, so THAT is the artifact under test. Parsing it also keeps the
	// expected count out of this file — the corpus grows and shrinks, and an arm
	// that hardcoded 12 would need editing every time a page is added.
	measured, pages, ok := manGateMeasuredCount(got)
	if !ok {
		t.Fatalf("the gate's measurement line carries no page count, yet it passed — a green that compared "+
			"nothing is the defect this floor exists for; log:\n%s", got)
	}
	if measured < manGateComparedFloor {
		t.Fatalf("the gate reported %d page(s) compared (floor %d) and still passed; log:\n%s",
			measured, manGateComparedFloor, got)
	}
	if len(pages) != measured {
		t.Fatalf("the measurement line says %d page(s) but names %d (%v) — the count and the list disagree; "+
			"log:\n%s", measured, len(pages), pages, got)
	}
	t.Logf("the gate's own measurement on the real tree: %d page(s) compared", measured)
}

// manGateMeasuredCount reads back the count and the page list from the sentence
// manGateMeasured printed — the ONE artifact a delivery log carries for the green
// half. ok is false when the line is absent or carries no number, which is the
// vacancy the floor refuses.
//
// It parses the PRINTED sentence rather than the generator's own output on
// purpose: the sentence is what a reader audits after the fact, so the arm that
// pins the green must assert about that sentence and not about its inputs.
func manGateMeasuredCount(log string) (n int, pages []string, ok bool) {
	for _, line := range strings.Split(log, "\n") {
		_, after, found := strings.Cut(line, manGateMeasuredPrefix+" — ")
		if !found {
			continue
		}
		// `<n> page(s) compared and up to date: <a>, <b>, …` — the shape
		// manGateMeasured writes, split on its OWN wording so a rewording fails
		// this arm instead of silently parsing nothing.
		countText, listText, ok := strings.Cut(after, " page(s) compared and up to date: ")
		if !ok {
			return 0, nil, false
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(countText))
		if err != nil {
			return 0, nil, false
		}
		for _, page := range strings.Split(listText, ",") {
			if page = strings.TrimSpace(page); page != "" {
				pages = append(pages, page)
			}
		}
		return parsed, pages, true
	}
	return 0, nil, false
}
