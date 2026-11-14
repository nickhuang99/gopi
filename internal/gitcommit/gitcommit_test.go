package gitcommit

// The seam's proofs. Three things are being established, in this order:
//
//  1. a commit made through Commit() CARRIES the trailer lines, verified by
//     reading them back out of the commit object (not by comparing the string
//     we sent — a message that never reached the commit is exactly the failure
//     a string comparison cannot see);
//  2. the lines the package renders are the shape `git log --grep` matches;
//  3. the backend is REPLACEABLE — with a fake installed, the same call runs,
//     which is what makes this a seam rather than three wrappers renamed.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// seamRepo is a real repository: the assertions are about what GIT recorded,
// so git has to be the one that recorded it.
func seamRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "master")
	runGit(t, dir, "config", "user.email", "seam-test@gopi")
	runGit(t, dir, "config", "user.name", "seam test")
	runGit(t, dir, "commit", "--allow-empty", "-m", "root")
	return dir
}

// runGit is this file's own fixture driver. It is NOT the code under test: the
// assertions read what the SEAM wrote, and a fixture that went through the seam
// could not tell "git recorded it" from "we both use the same helper".
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBinary, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// recordingBackend is the fake: it records every invocation and delegates to
// real git unless a canned answer was queued for a verb.
type recordingBackend struct {
	mu    sync.Mutex
	calls []Invocation
	// answerByKey answers a trailer READ-BACK: the value git would report for
	// that trailer key. It is keyed by KEY rather than by verb because both
	// read-backs are `git log` — one per trailer — and a verb-keyed map could
	// not tell the two apart (which is exactly how the first version of this
	// fake reported the host value for the task trailer).
	answerByKey map[string][]byte
}

func (b *recordingBackend) Run(ctx context.Context, inv Invocation) ([]byte, error) {
	b.mu.Lock()
	b.calls = append(b.calls, inv)
	b.mu.Unlock()
	for key, out := range b.answerByKey {
		want := fmt.Sprintf(trailerValueOnlyFmt, key)
		for _, a := range inv.Args {
			if strings.Contains(a, want) {
				return out, nil
			}
		}
	}
	return defaultBackend{}.Run(ctx, inv)
}

func (b *recordingBackend) sawVerb(verb string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.calls {
		for _, a := range c.Args {
			if a == verb {
				return true
			}
		}
	}
	return false
}

func (b *recordingBackend) commitArgs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.calls {
		for _, a := range c.Args {
			if a == gitVerbCommit {
				return c.Args
			}
		}
	}
	return nil
}

// TestGitCommitSeamWritesBothTrailers is the headline: one call, and the
// commit object itself reports a `task:` and a `host:` line.
func TestGitCommitSeamWritesBothTrailers(t *testing.T) {
	repo := seamRepo(t)
	const taskID = "01a18982-ecd3-791d-beb2-5a9fb1fc7845"

	res, err := Commit(context.Background(), repo,
		Message{Subject: "feat: the seam writes the lines", Body: "body prose"},
		Options{TaskID: taskID, NoVerify: true, AllowEmpty: true, Host: "seam-host"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(res.Hash) != 40 {
		t.Fatalf("Commit hash = %q, want a full 40-char sha", res.Hash)
	}
	body := runGit(t, repo, "log", "-1", "--format=%B")
	want := []string{TaskTrailerKey + ": " + taskID, HostTrailerKey + ": seam-host"}
	for _, line := range want {
		found := false
		for _, got := range strings.Split(body, "\n") {
			if strings.TrimSpace(got) == line {
				found = true
			}
		}
		if !found {
			t.Fatalf("commit body does not carry %q; body:\n%s", line, body)
		}
	}
	// The body prose is preserved verbatim, between subject and trailers.
	if !strings.Contains(body, "body prose") {
		t.Fatalf("the caller's body text was lost; body:\n%s", body)
	}
	// ONE LINE PER TRAILER is the measured trap: two lines in a single
	// `--trailer` value become ONE body line, and `--grep` then matches
	// neither. Counting kept lines is what catches that.
	if got, wantN := len(res.Trailers), 2; got != wantN {
		t.Fatalf("Result.Trailers = %d lines, want %d (verified, not assumed)", got, wantN)
	}
}

// TestGitCommitSeamNoLedgerRowOmitsTaskTrailer pins the independence the two
// lines have: a merge lane or an adoption sweep has no row, and a made-up id is
// worse than none.
func TestGitCommitSeamNoLedgerRowOmitsTaskTrailer(t *testing.T) {
	repo := seamRepo(t)
	if _, err := Commit(context.Background(), repo,
		Message{Subject: "merge-backlog: merge feat/x"}, Options{AllowEmpty: true, Host: "seam-host"}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	body := runGit(t, repo, "log", "-1", "--format=%B")
	if strings.Contains(body, TaskTrailerKey+":") {
		t.Fatalf("a commit with no ledger row invented one; body:\n%s", body)
	}
	if !strings.Contains(body, HostTrailerKey+": seam-host") {
		t.Fatalf("the host line is missing for a rowless commit; body:\n%s", body)
	}
}

// TestGitCommitSeamRefusesAnUnsignedCommit is the Rule 15 half, and it is the
// reason the read-back exists: a commit that does NOT carry the line must be an
// ERROR, never a success. The fake here answers git's trailer reader with
// nothing at all — the exact shape of a commit whose message lost its block.
func TestGitCommitSeamRefusesAnUnsignedCommit(t *testing.T) {
	repo := seamRepo(t)
	fake := &recordingBackend{answerByKey: map[string][]byte{
		TaskTrailerKey: []byte("\n"),
		HostTrailerKey: []byte("\n"),
	}}
	restore := UseBackend(fake)
	defer restore()

	_, err := Commit(context.Background(), repo,
		Message{Subject: "feat: the line never lands"}, Options{AllowEmpty: true, Host: "seam-host"})
	if err == nil {
		t.Fatal("Commit reported success for a commit whose trailer reads back empty")
	}
	if !strings.Contains(err.Error(), "no "+`"host"`+" trailer") {
		t.Fatalf("the refusal does not name the missing trailer: %v", err)
	}
	if !fake.sawVerb(gitVerbLog) {
		t.Fatal("the read-back never asked git for the trailer")
	}
}

// TestGitCommitSeamBackendIsReplaceable is the abstract-is-real proof: with a
// fake installed, the SAME Commit call runs and every git invocation it needs
// arrives at the fake — argv included, so a caller's flags are visible there.
func TestGitCommitSeamBackendIsReplaceable(t *testing.T) {
	repo := seamRepo(t)
	fake := &recordingBackend{answerByKey: map[string][]byte{
		TaskTrailerKey: []byte("01a18982-ecd3-791d-beb2-5a9fb1fc7845\n"),
		HostTrailerKey: []byte("seam-host\n"),
	}}
	// The hash is not a trailer, so it goes through real git — the fake lets
	// every invocation it has no answer for reach git, and `rev-parse HEAD`
	// then reports the root commit. The assertion below is therefore about
	// WHERE the value came from, which is checked by the recorded calls.
	restore := UseBackend(fake)
	defer restore()

	res, err := Commit(context.Background(), repo,
		Message{Subject: "feat: through a fake"},
		Options{TaskID: "01a18982-ecd3-791d-beb2-5a9fb1fc7845", NoVerify: true,
			NoEdit: true, AllowEmpty: true, Config: []string{"user.name=seam"}, Host: "seam-host"})
	if err != nil {
		t.Fatalf("Commit through the fake: %v", err)
	}
	if len(res.Hash) != 40 {
		t.Fatalf("hash = %q, want a full sha", res.Hash)
	}
	if !fake.sawVerb(gitVerbRevParse) {
		t.Fatal("the seam never asked the backend for the commit hash")
	}
	args := fake.commitArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{
		flagConfig + " user.name=seam",
		gitVerbCommit,
		flagNoVerify,
		flagNoEdit,
		flagMessage,
		TaskTrailerKey + ": 01a18982-ecd3-791d-beb2-5a9fb1fc7845",
		HostTrailerKey + ": seam-host",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the commit argv is missing %q: %v", want, args)
		}
	}
}

// TestGitCommitSeamRefusals pins the two inputs it cannot honestly commit.
func TestGitCommitSeamRefusals(t *testing.T) {
	repo := seamRepo(t)
	cases := []struct{ name, host, subject, wantSubstr string }{
		{"blank subject", "seam-host", "  ", "empty commit subject"},
		{"unspellable host", "bad host'name", "feat: x", "not spellable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Commit(context.Background(), repo,
				Message{Subject: c.subject}, Options{Host: c.host})
			if err == nil || !strings.Contains(err.Error(), c.wantSubstr) {
				t.Fatalf("Commit(%s) error = %v, want one naming %q", c.name, err, c.wantSubstr)
			}
		})
	}
}

// TestGitCommitSeamRendersTheDocumentedLines pins the spellings — the shape
// `git log --grep="task: <id>"` matches — and that both keys exist and differ.
func TestGitCommitSeamRendersTheDocumentedLines(t *testing.T) {
	if got, want := TaskTrailerLine(" abc123 "), TaskTrailerKey+": abc123"; got != want {
		t.Fatalf("TaskTrailerLine = %q, want %q (an id is trimmed once)", got, want)
	}
	if got, want := HostTrailerLine("peer1"), HostTrailerKey+": peer1"; got != want {
		t.Fatalf("HostTrailerLine = %q, want %q", got, want)
	}
	if TaskTrailerKey == HostTrailerKey {
		t.Fatalf("the ledger key and the host key are the same word (%q)", TaskTrailerKey)
	}
	if TaskTrailerLine("") != "" || HostTrailerLine("") != "" {
		t.Fatal("a blank value produced a line; a reference to nothing is not a reference")
	}
	if TaskTrailerLine("bad'id") != "" || HostTrailerLine("bad host") != "" {
		t.Fatal("an unspellable value produced a line")
	}
}

// TestGitCommitSeamTrailersListIsOrderedAndIndependent pins the SET the two
// doors share: row first, host second, host present without a row.
func TestGitCommitSeamTrailersListIsOrderedAndIndependent(t *testing.T) {
	got := Trailers("01a18982-ecd3-791d-beb2-5a9fb1fc7845")
	if len(got) != 2 {
		t.Fatalf("Trailers with a row = %v, want 2 lines", got)
	}
	if !strings.HasPrefix(got[0], TaskTrailerKey+": ") || !strings.HasPrefix(got[1], HostTrailerKey+": ") {
		t.Fatalf("Trailers order = %v, want task then host", got)
	}
	rowless := Trailers("")
	if len(rowless) != 1 || !strings.HasPrefix(rowless[0], HostTrailerKey+": ") {
		t.Fatalf("Trailers with no row = %v, want the host line alone", rowless)
	}
}

// TestGitCommitSeamFailureNamesGitsCause pins Rule 15b: a failed git call
// carries git's own diagnostic rather than an empty sentence.
func TestGitCommitSeamFailureNamesGitsCause(t *testing.T) {
	_, err := Commit(context.Background(), "/nonexistent-repo-for-seam-test",
		Message{Subject: "feat: nowhere"}, Options{Host: "seam-host"})
	if err == nil {
		t.Fatal("Commit in a nonexistent repo reported success")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("the failure does not name git: %v", err)
	}
	if strings.HasSuffix(err.Error(), ": ") {
		t.Fatalf("the failure ends with an empty cause: %v", err)
	}
}

// TestGitCommitSeamAmendAndAllowEmptyReachTheArgv pins the two flags this
// package offers that no current caller uses yet: they must be real argv, not
// Options fields that are silently dropped.
func TestGitCommitSeamAmendAndAllowEmptyReachTheArgv(t *testing.T) {
	opts := Options{Amend: true, AllowEmpty: true}
	argv := strings.Join(commitArgs(opts, "x"), " ")
	for _, want := range []string{flagAmend, flagAllowEmpty} {
		if !strings.Contains(argv, want) {
			t.Fatalf("commitArgs dropped %q: %s", want, argv)
		}
	}
}

// TestGitCommitSeamDiagnosticLineIsNotEmpty keeps the error renderer honest: a
// silent git failure must still produce a sentence (Rule 15 — never an empty
// cause, which reads as "no reason at all").
func TestGitCommitSeamDiagnosticLineIsNotEmpty(t *testing.T) {
	if got := diagnosticLine([]byte("\n\n  \nreal cause\nmore\n")); got != "real cause" {
		t.Fatalf("diagnosticLine = %q, want the first non-blank line", got)
	}
	if got := diagnosticLine(nil); got != "" {
		t.Fatalf("diagnosticLine(nil) = %q, want empty so the caller falls back to err.Error()", got)
	}
}
