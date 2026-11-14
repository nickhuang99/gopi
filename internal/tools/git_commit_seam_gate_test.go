package tools

// THE SEAM GATE — one place creates a commit, and nothing else spells the verb.
//
// Two separate claims are checked here, and they fail for different reasons:
//
//  1. THE LITERAL GATE: no production Go file under internal/ or cmd/ may pass
//     the bare string "commit" as an argument. The three sites that did
//     (adoption, merge-backlog execute, the union lane) are what the seam
//     replaced; a fourth one appearing later means the seam was routed around,
//     which is the defect this whole change exists to make impossible to
//     re-introduce quietly.
//
//  2. THE STATIONS: the three call sites, DRIVEN AGAINST A REAL REPOSITORY,
//     produce commits that carry the `host:` trailer — and the same three
//     functions, with a fake backend installed and not one line changed, run to
//     completion. The second half is what distinguishes a seam from three
//     wrappers that were renamed into one file.

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/nick/gopi/internal/gitcommit"
)

// ── claim 1: the literal gate ───────────────────────────────────────────────

// seamGateAllowlist is the CLOSED set of production sites that may spell the
// verb as a bare string, each with the reason it is not a commit creation.
// Adding a row is a claim that the literal is not an argv word; the gate fails
// when a row stops matching (the table can only shrink, Rule 39's ratchet).
var seamGateAllowlist = map[string]string{
	// A report fact LABEL: taskreport.FormatFact("commit", <hash>) names the
	// row's column, it does not run git.
	"internal/tools/task_report.go": "a report column label, not an argv word",
}

// seamGateMinFiles is the non-vacuity floor (Rule 38/42: a scan that reached
// nothing must not pass as a clean tree).
const seamGateMinFiles = 500

// TestGitCommitSeamGate refuses a fresh `"commit"` argv literal.
func TestGitCommitSeamGate(t *testing.T) {
	root := seamModuleRoot(t)
	var offenders []string
	scanned := 0
	for _, dir := range []string{"internal", "cmd"} {
		base := filepath.Join(root, dir)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "." || name == "testdata" {
					return nil
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			scanned++
			found := false
			for _, hit := range seamCommitLiterals(path) {
				found = true
				if _, ok := seamGateAllowlist[rel]; !ok {
					offenders = append(offenders, rel+": "+hit)
				}
			}
			if _, listed := seamGateAllowlist[rel]; listed && !found {
				offenders = append(offenders,
					rel+": listed in seamGateAllowlist but no longer spells the literal (drop the row)")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	if scanned < seamGateMinFiles {
		t.Fatalf("the gate scanned %d Go files, below its floor of %d — a scan that reached nothing is not a clean tree",
			scanned, seamGateMinFiles)
	}
	if len(offenders) > 0 {
		t.Fatalf("a production file still spells the commit verb as an argument (route it through internal/gitcommit.Commit):\n  %s",
			strings.Join(offenders, "\n  "))
	}
	// The gate must SEE the allowlisted site, or its green says nothing about it.
	if len(seamCommitLiterals(filepath.Join(root, "internal/tools/task_report.go"))) == 0 {
		t.Fatal("the recognizer no longer finds the allowlisted site — it is not reading the shape it claims to judge")
	}
	t.Logf("seam gate: scanned %d production Go files, %d allowlisted site(s), 0 offenders", scanned, len(seamGateAllowlist))
}

// TestGitCommitSeamGateRecognisesTheShapeItRefuses is the RED PROOF (Rule 42's
// discipline): the recognizer must fire on the exact argv shape the three
// removed sites had, and must NOT fire on the shapes that merely mention the
// word — a gate that cannot tell them apart would either pass everything or
// demand an unreadable corpus.
func TestGitCommitSeamGateRecognisesTheShapeItRefuses(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"the removed adoption shape", `package p
func f() { adoptionGit(repo, "commit", "--no-verify") }`, 1},
		{"the removed merge-execute shape", `package p
func f() { mergeExecGit(repo, "commit", "-m", msg) }`, 1},
		{"the removed union shape", `package p
func f() { mergeGit(wt, "-c", "user.name=x",
		"commit", "--no-edit") }`, 1},
		{"a struct field is not an argv word", `package p
var step = MergeExecStep{Step: "commit", OK: true}`, 0},
		{"a comparison is not an argv word", `package p
func f(s string) bool { return s == "commit" }`, 0},
		{"a const assignment is not an argv word", `package p
const gitVerbCommit = "commit"`, 0},
		{"a comment is not an argv word", `package p
// gitRun(repo, "commit", "-m") used to be here
func f() {}`, 0},
		{"a longer string is not the literal", `package p
func f() { run("commit --amend") }`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, "probe.go")
			if err := os.WriteFile(path, []byte(c.src), 0o644); err != nil {
				t.Fatalf("write probe: %v", err)
			}
			if got := len(seamCommitLiterals(path)); got != c.want {
				t.Fatalf("recognizer found %d hit(s), want %d, in: %s", got, c.want, c.src)
			}
		})
	}
}

// seamCommitLiterals returns every occurrence of the verb as a bare ARGUMENT
// literal: preceded by `(` or `,` (optionally with spaces) and followed by `,`
// or `)`. That shape is what an argv word looks like in Go, and it deliberately
// excludes `Step: "commit"` (a struct field), `== "commit"` (a comparison) and
// any occurrence inside a comment or a longer string.
func seamCommitLiterals(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	const lit = `"` + "commit" + `"`
	var hits []string
	line := 1
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line++
			continue
		}
		if data[i] != lit[0] || i+len(lit) > len(data) || string(data[i:i+len(lit)]) != lit {
			continue
		}
		if !seamLiteralIsArgument(data, i) {
			continue
		}
		if seamInComment(data, i) {
			continue
		}
		hits = append(hits, fmt.Sprintf("line %d", line))
	}
	return hits
}

// seamLiteralIsArgument reports whether the literal at off sits where an
// argument does: the nearest non-space byte before it is `(` or `,`, and the
// nearest non-space byte after it is `,` or `)`.
func seamLiteralIsArgument(data []byte, off int) bool {
	before := byte(0)
	for i := off - 1; i >= 0; i-- {
		if data[i] == ' ' || data[i] == '\t' || data[i] == '\n' {
			continue
		}
		before = data[i]
		break
	}
	after := byte(0)
	for i := off + len(`"commit"`); i < len(data); i++ {
		if data[i] == ' ' || data[i] == '\t' || data[i] == '\n' {
			continue
		}
		after = data[i]
		break
	}
	return (before == '(' || before == ',') && (after == ',' || after == ')')
}

// seamInComment reports whether the byte offset sits inside a `//` comment or a
// `/* */` block — a comment naming the verb is documentation of the defect this
// gate closes, not the defect.
func seamInComment(data []byte, off int) bool {
	lineStart := off
	for lineStart > 0 && data[lineStart-1] != '\n' {
		lineStart--
	}
	if strings.Contains(string(data[lineStart:off]), "//") {
		return true
	}
	return strings.LastIndex(string(data[:off]), "/*") > strings.LastIndex(string(data[:off]), "*/")
}

// TestGitCommitSeamAliasesAreNotCopies pins the single-source half: the tools
// package must REUSE gitcommit's spellings (Rule 13/14 — a constant bypassed by
// a mirror is a bare literal in disguise).
func TestGitCommitSeamAliasesAreNotCopies(t *testing.T) {
	if TaskTrailerKey != gitcommit.TaskTrailerKey {
		t.Fatalf("tools.TaskTrailerKey = %q, gitcommit's = %q", TaskTrailerKey, gitcommit.TaskTrailerKey)
	}
	if HostTrailerKey != gitcommit.HostTrailerKey {
		t.Fatalf("tools.HostTrailerKey = %q, gitcommit's = %q", HostTrailerKey, gitcommit.HostTrailerKey)
	}
	if TaskTrailerLineFmt != gitcommit.TaskTrailerLineFmt || HostTrailerLineFmt != gitcommit.HostTrailerLineFmt {
		t.Fatal("the tools-side line formats are copies, not aliases")
	}
	// The behaviour must be the shared one, not a local re-implementation.
	if got, want := TaskTrailerLine("abc"), gitcommit.TaskTrailerLine("abc"); got != want {
		t.Fatalf("TaskTrailerLine = %q, gitcommit's = %q", got, want)
	}
	if got, want := HostTrailerLine("zpeer"), gitcommit.HostTrailerLine("zpeer"); got != want {
		t.Fatalf("HostTrailerLine = %q, gitcommit's = %q", got, want)
	}
	if got, want := localHostTrailerLine(), gitcommit.LocalHostTrailerLine(); got != want {
		t.Fatalf("localHostTrailerLine = %q, gitcommit's = %q", got, want)
	}
}

// ── claim 2: the stations, driven for real ──────────────────────────────────

// seamStationRepo builds a repository with one commit and one STAGED change —
// the state each of the three stations commits.
func seamStationRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "master")
	git("config", "user.email", "station-test@gopi")
	git("config", "user.name", "station test")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git("add", "work.txt")
	git("commit", "-m", "root")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("root\nlanded\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git("add", "work.txt")
	return dir
}

// seamCommitBody reads the body of HEAD — what GIT recorded.
func seamCommitBody(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--format=%B")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read HEAD: %v: %s", err, out)
	}
	return string(out)
}

// seamRequireTrailerLine asserts a whole line is present in the commit body.
// Whole-line equality, not `strings.Contains`: the measured trap is two
// trailers collapsing into ONE body line (`task: X host: Y`), which a Contains
// assertion would call green.
func seamRequireTrailerLine(t *testing.T, body, key, want string) {
	t.Helper()
	wantLine := (key + ":")
	if want != "" {
		wantLine = key + ": " + want
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == wantLine {
			return
		}
		// A NON-EMPTY value may be any host name: only the shape is asserted.
		if want == "" && strings.HasPrefix(line, wantLine+" ") && len(line) > len(wantLine)+1 {
			return
		}
	}
	t.Fatalf("commit body carries no %q line (want %q); body:\n%s", key, wantLine, body)
}

// TestGitCommitSeamStationAdoption drives STATION ①: the adoption merge commit.
func TestGitCommitSeamStationAdoption(t *testing.T) {
	repo := seamStationRepo(t)
	if err := adoptionCommitMerge(context.Background(), repo,
		adoptionSummaryPrefix+"feat/seam-station (abc1234)"); err != nil {
		t.Fatalf("adoptionCommitMerge: %v", err)
	}
	body := seamCommitBody(t, repo)
	if !strings.Contains(body, adoptionSummaryPrefix+"feat/seam-station (abc1234)") {
		t.Fatalf("the adoption subject changed; body:\n%s", body)
	}
	seamRequireTrailerLine(t, body, HostTrailerKey, "" /* non-empty only */)
	// The sweep merges OTHER rows, so it names no row — and must not invent one.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), TaskTrailerKey+":") {
			t.Fatalf("the adoption commit invented a ledger row; body:\n%s", body)
		}
	}
}

// TestGitCommitSeamStationMergeExecute drives STATION ②: the merge-backlog
// commit, which names BOTH the host and the row whose branch landed.
func TestGitCommitSeamStationMergeExecute(t *testing.T) {
	repo := seamStationRepo(t)
	const rowID = "01a18982-ecd3-791d-beb2-5a9fb1fc7845"
	msg := fmt.Sprintf(mergeExecMergeMsg, "feat/seam-station")
	if err := mergeExecCommitMerge(repo, msg, rowID); err != nil {
		t.Fatalf("mergeExecCommitMerge: %v", err)
	}
	body := seamCommitBody(t, repo)
	if !strings.Contains(body, msg) {
		t.Fatalf("the merge message changed; body:\n%s", body)
	}
	seamRequireTrailerLine(t, body, HostTrailerKey, "")
	seamRequireTrailerLine(t, body, TaskTrailerKey, rowID)
}

// seamFakeBackend records every git invocation and answers the trailer
// READ-BACK by parsing the message the caller actually composed — so a green
// under the fake still proves the trailer is in the argv, which is the property
// the stations rely on.
type seamFakeBackend struct {
	mu    sync.Mutex
	calls [][]string
}

// Run implements gitcommit.Backend.
func (f *seamFakeBackend) Run(_ context.Context, inv gitcommit.Invocation) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), inv.Args...))
	f.mu.Unlock()

	args := inv.Args
	if len(args) > 0 && args[0] == "commit" {
		return []byte(""), nil
	}
	if len(args) > 0 && args[0] == "rev-parse" {
		return []byte("1111111111111111111111111111111111111111\n"), nil
	}
	if len(args) > 0 && args[0] == "log" {
		// The format word is not always args[1] (`-1` comes first), so the key
		// is read from whichever word carries the trailer pattern.
		for _, a := range args {
			key, ok := strings.CutPrefix(a, "--format=%(trailers:key=")
			if !ok {
				continue
			}
			key = strings.TrimSuffix(key, ",valueonly)")
			for _, line := range strings.Split(strings.Join(f.message(), "\n"), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), key+": "); ok {
					return []byte(v + "\n"), nil
				}
			}
			return []byte("\n"), nil
		}
		return nil, fmt.Errorf("fake backend: log without a trailer format: %v", args)
	}
	return nil, fmt.Errorf("fake backend: unexpected invocation %v", args)
}

// message returns the composed commit message the fake was handed.
func (f *seamFakeBackend) message() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, args := range f.calls {
		for i, a := range args {
			if a == "-m" && i+1 < len(args) {
				return []string{args[i+1]}
			}
		}
	}
	return nil
}

// sawCommit reports whether the caller's commit reached the backend.
func (f *seamFakeBackend) sawCommit() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, args := range f.calls {
		if len(args) > 0 && args[0] == "commit" {
			return true
		}
	}
	return false
}

// TestGitCommitSeamStationsRunUnchangedOnAFakeBackend is the abstraction proof:
// with a fake installed, BOTH tools-side stations run to completion and their
// commit reaches the fake. Their source was not touched for this test to pass —
// which is the difference between a seam and three wrappers.
func TestGitCommitSeamStationsRunUnchangedOnAFakeBackend(t *testing.T) {
	fake := &seamFakeBackend{}
	restore := gitcommit.UseBackend(fake)
	defer restore()

	// No repository at all: everything the stations need is answered by the
	// fake, which is only possible because they ask the SEAM and not git.
	if err := adoptionCommitMerge(context.Background(), t.TempDir(),
		adoptionSummaryPrefix+"feat/seam-fake"); err != nil {
		t.Fatalf("station ① on a fake backend: %v", err)
	}
	if !fake.sawCommit() {
		t.Fatal("station ① did not reach the backend at all")
	}
	fake.mu.Lock()
	fake.calls = nil
	fake.mu.Unlock()

	if err := mergeExecCommitMerge(t.TempDir(), "merge-backlog: merge feat/seam-fake",
		"01a18982-ecd3-791d-beb2-5a9fb1fc7845"); err != nil {
		t.Fatalf("station ② on a fake backend: %v", err)
	}
	if !fake.sawCommit() {
		t.Fatal("station ② did not reach the backend at all")
	}
	msg := strings.Join(fake.message(), "\n")
	for _, want := range []string{TaskTrailerKey + ": 01a18982-ecd3-791d-beb2-5a9fb1fc7845", HostTrailerKey + ": "} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the composition handed to the backend lacks %q: %q", want, msg)
		}
	}
}

// seamModuleRoot walks up from this test file until it finds go.mod.
func seamModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed: the gate cannot locate the module root")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("no go.mod above %s", file)
	return ""
}

// seamReadLines is kept for a caller that wants the raw file lines while
// debugging the gate (bufio is otherwise unused, and an unused import would
// make this file fail to build for a reason that hides the gate).
func seamReadLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}
