package stylegate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// This file is the RED PROOF the ratchet's own failure mode lacked.
//
// TestStyleGate compares a READ COUNT against a CEILING table, and a ceiling is a
// claim about the WHOLE TREE at one commit — a claim that silently expires. The
// recurrence this arm exists for (2026-10-28, measured on e256279): the tree had
// 78 ignored-error sites against a ceiling of 77, 1 test-sleep site against 0, and
// 2 unchecked-close sites against 0, and NOTHING in the package distinguished
//
//	"a site drifted in"            (the reading moved)
//	from
//	"the threshold was relaxed"    (the ceiling moved)
//
// from
//	"the detector went blind"      (the reading did not move but no longer sees)
//
// All three produce the same shape of evidence — a number and a table — so the
// gate could be argued back to green by moving the number instead of the sites,
// and no test in the repository would have failed.
//
// The three arms below pin the three distinguishable propositions. They are
// fixtures, not the repo scan (TestStyleGate owns that): each builds a throwaway
// module, scans it, and asserts what a ratchet MUST conclude from the reading it
// got.

// ratchetVerdict is the decision a ceiling makes about one reading, expressed as
// the gate decides it and NOT as the gate reports it. Keeping the decision in a
// named function is what lets the arms below assert the DECISION; the gate's
// message (and the counters behind it) stay where they are — this arm never
// touches internal/stylegate's gate file or its ceiling table.
type ratchetVerdict struct {
	// over is true when the reading exceeds the ceiling: drift a run must fix or
	// annotate, never absorb.
	over bool
	// headroom is ceiling - reading. Negative is the drift; a ceiling sitting at
	// zero headroom may only ratchet DOWN (Rule: 只能向下棘轮).
	headroom int
}

// ratchetDecision is the ONE predicate TestStyleGate applies to a (reading,
// ceiling) pair. It is deliberately trivial: the point is not cleverness, it is
// that "did this reading exceed its ceiling" has a single named definition the
// arms can drive, so a future refactor of the gate's loop cannot quietly change
// what "over" means without turning these arms red.
func ratchetDecision(reading, ceiling int) ratchetVerdict {
	return ratchetVerdict{over: reading > ceiling, headroom: ceiling - reading}
}

// ratchetFixture writes a throwaway module holding exactly n unannotated
// ignored-error sites, so a reading can be manufactured without touching the
// repo (Rule 6: this test's ground is a temp dir it creates and owns).
func ratchetFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package fixture\n\nimport \"fmt\"\n\n")
	b.WriteString("func boom() (int, error) { return 0, fmt.Errorf(\"boom\") }\n\n")
	for i := 0; i < n; i++ {
		// One site per statement: `_ = err` on an error-typed value with no
		// reason comment is exactly what RuleIgnoredError reports.
		b.WriteString("func site")
		b.WriteString(string(rune('A' + i)))
		b.WriteString("() {\n\t_, _ = boom()\n}\n\n")
	}
	files := map[string]string{
		"go.mod":              fixtureMod,
		"ratchet.go":          b.String(),
		"ratchet_fix_test.go": "package fixture\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestRatchetDriftIsMeasuredAgainstTheThreshold is the DRIFT RED PROOF: a reading
// that grew past a fixed ceiling must be reported as OVER, and the same reading
// against a ceiling that matches it must not be. Without the control half, a
// predicate that always says "over" would satisfy this arm.
//
// It also pins the RATCHET DIRECTION: the ceiling is never the thing that moves.
// The fixture's ceiling stays whatever the caller passes, and the arm asserts the
// headroom arithmetic that makes "relaxing the threshold" a visible, negative-
// headroom event rather than a silent edit.
func TestRatchetDriftIsMeasuredAgainstTheThreshold(t *testing.T) {
	// CONTROL: reading == ceiling → not over, zero headroom. A ceiling sitting at
	// its own measurement is the post-sweep state, and it is GREEN.
	if v := ratchetDecision(77, 77); v.over {
		t.Fatalf("reading 77 against ceiling 77 reported OVER — a ceiling equal to its measurement is the "+
			"swept state, not drift; got %+v", v)
	}
	// THE RED: one more site than the ceiling allows, which is the exact shape
	// measured on e256279 (78 > 77).
	if v := ratchetDecision(78, 77); !v.over {
		t.Fatalf("reading 78 against ceiling 77 did NOT report OVER — this is the drift the gate exists to "+
			"catch (measured on e256279: ignored-error 78 / ceiling 77), and a ratchet that stays silent "+
			"about it is a threshold nobody enforces; got %+v", v)
	}
	if v := ratchetDecision(78, 77); v.headroom != -1 {
		t.Errorf("drift headroom = %d, want -1 — the arithmetic is what makes a RELAXED ceiling visible as a "+
			"negative headroom rather than as an unremarkable edit (Rule 1: 只能向下棘轮)", v.headroom)
	}

	// MEASURED THROUGH THE REAL SCANNER, not only through the arithmetic: the
	// instrument must still SEE its subject. A detector that went blind would
	// report 0 findings and the gate would read green forever — the failure mode
	// this arm's third proposition names. Two fixtures, 1 site and 3 sites: the
	// reading must MOVE with the fixture, which is the property a blind detector
	// cannot fake.
	for _, n := range []int{1, 3} {
		dir := ratchetFixture(t, n)
		rep, err := Scan(Options{Root: dir, IncludeTests: true})
		if err != nil {
			t.Fatalf("Scan(%d-site fixture): %v", n, err)
		}
		got := 0
		for _, f := range rep.Findings {
			if f.Rule == RuleIgnoredError {
				got++
			}
		}
		if got != n {
			t.Fatalf("a %d-site fixture produced %d %s finding(s) — the detector must see every site it is the "+
				"threshold for; a reading of 0 is the BLIND shape, and a gate comparing it to any ceiling then "+
				"reads green forever (findings: %v)", n, got, RuleIgnoredError, rep.Findings)
		}
		if !ratchetDecision(got, n-1).over {
			t.Fatalf("the measured %d-site reading against ceiling %d must report OVER", got, n-1)
		}
		if ratchetDecision(got, n).over {
			t.Fatalf("the measured %d-site reading against ceiling %d must NOT report OVER", got, n)
		}
	}
}

// TestRatchetCitesOnlyRevisionsThatResolve is the PROVENANCE half of the
// ratchet, and it carries its own CONTROL and its own RED.
//
// The two failures are one family: a ceiling whose REASONING cites a revision
// that does not resolve is a threshold whose authority cannot be re-derived —
// Rule 38's "a census figure names the commit it was measured at", applied to the
// ratchet's own prose. The census measured the shape: a shipped table cited
// e4066012, an object this repository does not hold, and every diagnostic stayed
// green while the table claimed "the reading IS this revision".
//
// The arm is a FIXTURE, not a skip: it builds a throwaway repository, writes a
// ceiling table into it that cites the fixture's own HEAD (the CONTROL — that
// citation must resolve), then the same table citing a well-formed hash no commit
// has (the RED — that citation must be reported). A git-less environment fails
// loudly rather than passing quietly (Rule 15).
func TestRatchetCitesOnlyRevisionsThatResolve(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gatePath := filepath.Join(dir, "gate_table.go")
	write := func(citation string) {
		t.Helper()
		body := "package fixture\n\n// ceiling measured at " + citation + "\nconst ceiling = 77\n"
		if err := os.WriteFile(gatePath, []byte(body), 0o644); err != nil {
			t.Fatalf("write the fixture ceiling table: %v", err)
		}
	}
	write("WORKING")
	git("init", "-q")
	git("add", "gate_table.go")
	git("commit", "-q", "-m", "fixture ceiling")
	head := git("rev-parse", "HEAD")

	// CONTROL: a citation of the fixture's own HEAD resolves, so the predicate
	// below is not one that rejects everything.
	write(head)
	if out, ok := objectInRepo(dir, headOf(t, dir)); !ok {
		t.Fatalf("CONTROL FAILED: the fixture HEAD %s does not resolve (%s) — this arm's red half would be "+
			"vacuous", head, out)
	}
	cites := citedRevisions(t, gatePath)
	if len(cites) != 1 || !strings.EqualFold(cites[0], head) {
		t.Fatalf("the citation reader found %v in a table citing %s — it must see the spelling it is "+
			"the guard for", cites, head)
	}

	// THE RED: a well-formed hash no commit has. It must be reported, and the
	// report must quote the revision so the reader can follow it.
	const absent = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	write(absent)
	for _, rev := range citedRevisions(t, gatePath) {
		errText, ok := objectInRepo(dir, rev)
		if ok {
			t.Fatalf("a citation of %s resolved in a repository whose only commit is %s — the predicate is "+
				"not measuring the object store", rev, head)
		}
		if !strings.Contains(errText, absent) {
			t.Errorf("the refusal for %s does not name the revision it could not resolve (%q): %s",
				rev, absent, errText)
		}
	}
	t.Logf("ratchet provenance: HEAD %s resolves; %s reported absent", head, absent)
}

// headOf is the fixture repository's own HEAD, read from the repository rather
// than remembered, so the CONTROL cannot drift from the commit it names.
func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("read the fixture HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// citedRevisions returns every full 40-character hex run appearing in path —
// the revision spellings a comment can be held to. Read from the file, never
// from a list (Rule 35: no second artifact to keep in step).
func citedRevisions(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the gate file %s: %v", path, err)
	}
	seen := map[string]bool{}
	var out []string
	for _, field := range strings.Fields(string(b)) {
		field = strings.Trim(field, "`'\"(),.;:")
		if len(field) != 40 {
			continue
		}
		allHex := true
		for _, r := range field {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				allHex = false
				break
			}
		}
		if !allHex || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

// objectInRepo reports whether rev is an object this repository can show, and
// the command's own output when it cannot — the criterion is `git cat-file -t`,
// never a substring check on a hash (Rule 41: the claim carries its evidence).
func objectInRepo(root, rev string) (string, bool) {
	out, err := exec.Command("git", "-C", root, "cat-file", "-t", rev).CombinedOutput()
	// The revision is NAMED by this function, never left to the command: `git
	// cat-file`'s own failure text is `could not get object info` with no
	// revision in it (measured), so a refusal built from its output alone tells
	// the reader nothing about WHICH citation is unresolvable (Rule 15).
	if err != nil {
		return rev + ": " + strings.TrimSpace(string(out)), false
	}
	return rev + " is a " + strings.TrimSpace(string(out)), true
}
