package tools

// chat_write_guard_test.go — the CHAT WRITE BOUNDARY's write/edit arm, after it
// became RULE-DRIVEN: the same (chatpolicy.Policy, posture) pair the bash arm is
// judged by (chat_policy_gate.go) now answers "may this chat write to this path?".
//
// WHAT THESE TESTS ARE ABOUT. Not git, not a tracked index, not a compiled list
// of trees: the operator's ```chat-policy fence. Each test freezes an in-memory
// table with SetChatPolicy (the ONE channel the dispatch reads), so no test here
// depends on this repository's own index, its GOPI.md or any real filesystem
// state — the table IS the input, ChatWritePathRefusal is the output.
//
// THE THREE VALUES ARE THE WHOLE SPEC, and each is asserted with its NEGATION
// too, because the two ways to lose this arm are silent: an arm that allowed
// everything would pass an allow-only table, and one that refused everything
// would pass a refusal-only table. The prompt/forbidden pair is the load-bearing
// distinction — collapsing either into the other is the failure the layer exists
// to prevent — so the assertions key on the two distinguishable prefixes rather
// than on prose.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/devdoc"
)

// chatWriteTestRepoRoot is the repository root the tests resolve paths against:
// a spelling no test asserts to exist, because the normalization is a prefix
// subtraction and the rules never consult the disk.
const chatWriteTestRepoRoot = "/home/user/work/GoPi"

// chatWriteSampleFence is the rule table the write arm is measured against — the
// same two rules the sample fence in the repository's GOPI.md carries: the
// document corpus is ALLOWED, every other target is a QUESTION.
func chatWriteSampleFence() string {
	return chatPolicyFence(
		"allow write "+devdoc.Root+" *",
		"prompt write *",
	)
}

// TestChatWritePathRefusalAllowsTheDocumentCorpusByRule is the allow half: a
// target under the corpus is written with NO refusal and NO approval request, in
// both spellings a tool call arrives in (repo-relative and absolute), at any
// depth — because `allow write devdoc *` says so, and the trailing `*` is worth
// zero specificity, i.e. it covers the rest of the path and nothing else.
func TestChatWritePathRefusalAllowsTheDocumentCorpusByRule(t *testing.T) {
	freezeChatRuleTable(t, chatWriteSampleFence(), postureOnRequest())

	for _, p := range []string{
		devdoc.Root + "/chat-devdoc-write-allowance.md",
		chatWriteTestRepoRoot + "/" + devdoc.Root + "/chat-devdoc-write-allowance.md",
		chatWriteTestRepoRoot + "/" + devdoc.Root + "/sub/dir/notes.md",
		// The tree itself, as a bare path: [write, devdoc] still matches
		// `allow write devdoc *` (a trailing wildcard may match no words).
		devdoc.Root,
		devdoc.Root + "/",
	} {
		if got := ChatWritePathRefusal(p, chatWriteTestRepoRoot); got != "" {
			t.Errorf("ChatWritePathRefusal(%q) did not return the allow decision:\n%s", p, got)
		}
	}
}

// TestChatWritePathRefusalPromptsForASourceTarget is the prompt half: a source
// path matches `prompt write *`, so the tool call receives the APPROVAL text —
// naming the deciding rule's source and line — and explicitly NOT the refusal.
// Both facts are asserted: "stopped and asks" is only true if it is also not
// "refused", which is how the model learns to ask instead of giving up.
func TestChatWritePathRefusalPromptsForASourceTarget(t *testing.T) {
	freezeChatRuleTable(t, chatWriteSampleFence(), postureOnRequest())

	for _, p := range []string{
		"internal/agent/loop.go",
		chatWriteTestRepoRoot + "/internal/agent/loop.go",
		"CONFIG.md",
	} {
		got := ChatWritePathRefusal(p, chatWriteTestRepoRoot)
		if !strings.Contains(got, chatWriteApprovalPrefix) {
			t.Fatalf("ChatWritePathRefusal(%q) did not stop for approval:\n%s", p, got)
		}
		if strings.Contains(got, "Refused by the CHAT WRITE BOUNDARY") {
			t.Fatalf("ChatWritePathRefusal(%q) answered as a REFUSAL — prompt must not collapse into forbidden:\n%s", p, got)
		}
		// The evidence rule: the operator is told WHICH line asked, not just
		// that something did.
		if !strings.Contains(got, "write *") || !strings.Contains(got, "test chat-policy:") {
			t.Errorf("ChatWritePathRefusal(%q) does not name the deciding rule's pattern and source:line:\n%s", p, got)
		}
	}
}

// TestChatWritePathRefusalForbidsByRule is the forbidden half: a rule the
// operator marked `forbidden` refuses outright — no approval text, the boundary
// named, and the deciding rule quoted. The SAME call shape under a path the
// table allows still passes, so the refusal is the rule's, not the arm's.
func TestChatWritePathRefusalForbidsByRule(t *testing.T) {
	freezeChatRuleTable(t, chatPolicyFence(
		"forbidden write internal",
		"allow write "+devdoc.Root+" *",
		"prompt write *",
	), postureOnRequest())

	refused := []string{
		"internal/agent/loop.go",
		chatWriteTestRepoRoot + "/internal/agent/loop.go",
		"internal",
	}
	for _, p := range refused {
		got := ChatWritePathRefusal(p, chatWriteTestRepoRoot)
		if !strings.Contains(got, "Refused by the CHAT WRITE BOUNDARY") {
			t.Fatalf("ChatWritePathRefusal(%q) did not refuse on a `forbidden` rule:\n%s", p, got)
		}
		if strings.Contains(got, chatWriteApprovalPrefix) {
			t.Fatalf("ChatWritePathRefusal(%q) asked for approval on a FORBIDDEN rule — nothing is askable:\n%s", p, got)
		}
		// The rule is quoted as the operator WROTE it (pattern + source:line) and
		// its decision is stated in words: the two halves of "which line said no".
		if !strings.Contains(got, "`write internal`") || !strings.Contains(got, "`forbidden`") {
			t.Errorf("the refusal does not quote the deciding rule:\n%s", got)
		}
	}
	// The same arm, a path the table allows: still silent. A refusal that fired
	// on every path would pass the table above.
	if got := ChatWritePathRefusal(devdoc.Root+"/notes.md", chatWriteTestRepoRoot); got != "" {
		t.Errorf("the forbidden rule leaked onto the allowed corpus:\n%s", got)
	}
	// And a path no rule mentions falls to the POSTURE — ask, not refuse.
	got := ChatWritePathRefusal("/home/user/.gopi/sessions/abc/files/notes.md", chatWriteTestRepoRoot)
	if !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Errorf("a target outside every rule must take the posture's `prompt` answer:\n%s", got)
	}
}

// TestChatWritePathRefusalFollowsThePostureWhenNoRuleMatches pins the LAYER
// ORDER (package chatpolicy's semantics, not this file's): `Matched=false` hands
// the question to the startup posture. A yolo posture therefore allows a path no
// rule mentions — the arm adds no opinion of its own — while the on-request
// posture asks about it.
func TestChatWritePathRefusalFollowsThePostureWhenNoRuleMatches(t *testing.T) {
	const outside = "/home/user/.gopi/sessions/abc/files/notes.md"

	freezeChatRuleTable(t, chatPolicyFence("allow write "+devdoc.Root+" *"), postureOnRequest())
	if got := ChatWritePathRefusal(outside, chatWriteTestRepoRoot); !strings.Contains(got, chatWriteApprovalPrefix) {
		t.Fatalf("on-request posture: an unmatched target must be a QUESTION, got:\n%s", got)
	}

	freezeChatRuleTable(t, chatPolicyFence("allow write "+devdoc.Root+" *"),
		chatpolicy.NewPosture(chatpolicy.DecisionAllow, "test"))
	if got := ChatWritePathRefusal(outside, chatWriteTestRepoRoot); got != "" {
		t.Fatalf("allow-everything posture: an unmatched target must be permitted, got:\n%s", got)
	}
}

// --- the TRACKED sample, and the operator's own file -------------------------

// gopiRuleDocName is the project rule file's name at the repository root, spelled
// once (the same spelling internal/agent uses for the project-context loader).
const gopiRuleDocName = "GOPI.md"

// chatPolicySampleFixture is the tracked sample rule table, relative to this
// package's directory. It exists because the operator's own GOPI.md is
// UNTRACKED and gitignored: a test that reads THAT file answers SKIP (nobody
// wrote one), FAIL (a host copy carrying a narrower fence) or PASS (a workspace
// copy with the fence appended) for the same commit — a verdict about whose
// machine ran the suite, not about this code (Rule 30's mirror image: a tracked
// input is a test, an untracked one is a coin flip).
const chatPolicySampleFixture = "chat-policy-sample.md"

// chatPolicyFixtureDriftSource names the fence chatWriteSampleFence() declares
// when it is parsed for the drift comparison below — a label the parse errors can
// quote, never a path on disk.
const chatPolicyFixtureDriftSource = "test chat-policy (the fence chatWriteSampleFence declares)"

// chatPolicyFenceClose is the Markdown fence's closing line, spelled once for the
// malformed-file arm below (the spelling chatPolicyFence composes).
const chatPolicyFenceClose = "```\n"

// chatRuleFileParseVerdict is what the OPTIONAL leg can honestly say about a rule
// file at path: present=false when there is no file (a project with no rules — a
// valid state, not a failure), and a non-nil error when the file exists but does
// not parse. It PARSES only. Nothing about the operator's own content is
// asserted here, because that file is not this repository's specification: a
// checkout that carries a narrower or wider fence is a legitimate machine, while
// an unparseable one breaks the chat before it starts — that is the only part
// every copy must share, and the part a broken fence produces.
func chatRuleFileParseVerdict(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if _, perr := chatpolicy.Parse(path, string(data)); perr != nil {
		return true, fmt.Errorf("%s does not parse as a rule table: %w", path, perr)
	}
	return true, nil
}

// readChatPolicySample parses the tracked fixture. A MISSING fixture FAILS rather
// than skips: the file is committed with this test, so its absence is a defect on
// the machine that checked the tree out, not an environment without rules.
func readChatPolicySample(t *testing.T) (*chatpolicy.Policy, string) {
	t.Helper()
	path := filepath.Join("testdata", chatPolicySampleFixture)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the tracked sample %s: %v — it ships with this test, so a missing file is a defect, never a skip",
			path, err)
	}
	pol, perr := chatpolicy.Parse(path, string(data))
	if perr != nil {
		t.Fatalf("%s does not parse as a rule table: %v", path, perr)
	}
	return pol, path
}

// repoRootFromPackageDir is the repository root as seen from this package's test
// working directory. It is a PATH computation, not an assertion: the test that
// uses it skips when the file is absent (a checkout whose operator has not
// written its rule file yet, or a linked worktree without one).
func repoRootFromPackageDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate the repository root above %q", dir)
	return ""
}

// TestRepoGOPIDocChatPolicyFenceIsValid is the END-TO-END leg, and it runs
// against the REAL repository root's GOPI.md — not a testdata copy, because the
// thing that must be true is a property of the file an operator actually writes:
// chatpolicy.Parse(that file) returns NO error, and the sample fence it carries
// really does allow the document corpus and really does make a source target a
// question. A copy could pass while the operator's file failed to parse, which is
// exactly the failure a broken ```chat-policy fence produces at startup.
//
// A checkout without the file SKIPS: the rule file is OPTIONAL (a project with no
// fence is a valid empty table answered by the posture), so its absence is not a
// failure of this code.
// TestChatPolicySampleFixtureIsTheSpecification is the REQUIRED leg: the tracked
// sample at internal/tools/testdata/chat-policy-sample.md parses, carries exactly
// one fence, carries rules, bans nothing — and then its CHARACTER is asserted
// through the same arm a chat's write goes through (the document corpus passes,
// a source target is a QUESTION). This is the assertion the old end-to-end leg
// tried to make against the repository root's GOPI.md, which is untracked: the
// verdict of that leg was a function of whose machine ran the suite.
//
// The same two rules are ALSO built in memory (chatWriteSampleFence) and compared
// rule for rule, so the fixture cannot drift away from the shape the rest of this
// file's tests measure against.
func TestChatPolicySampleFixtureIsTheSpecification(t *testing.T) {
	pol, path := readChatPolicySample(t)

	if !pol.Fenced {
		t.Fatalf("%s carries no ```chat-policy fence — the sample must be a fence, not prose", path)
	}
	if pol.Len() == 0 {
		t.Fatalf("%s carries a fence but no rules", path)
	}
	for _, r := range pol.Rules {
		if r.Decision == chatpolicy.DecisionForbidden {
			t.Errorf("%s:%d marks %q `forbidden` — the sample leaves `forbidden` EMPTY "+
				"(the posture and `prompt write *` carry the boundary)", path, r.Line, r.PatternString())
		}
	}

	// The fixture equals the in-memory fence the other tests in this file use, so
	// a rule added to one side and not the other is caught here rather than in a
	// comment nobody re-reads.
	want, err := chatpolicy.Parse(chatPolicyFixtureDriftSource, chatWriteSampleFence())
	if err != nil {
		t.Fatalf("the in-memory sample fence does not parse: %v", err)
	}
	if pol.Len() != want.Len() {
		t.Fatalf("%s carries %d rules, chatWriteSampleFence declares %d — the fixture and the "+
			"in-memory table must be the same table", path, pol.Len(), want.Len())
	}
	for i, r := range pol.Rules {
		w := want.Rules[i]
		if r.Decision != w.Decision || strings.Join(r.Pattern, " ") != strings.Join(w.Pattern, " ") {
			t.Errorf("%s:%d declares %s %q, chatWriteSampleFence declares %s %q — the tracked "+
				"sample drifted from the table the other tests measure",
				path, r.Line, r.Decision, r.PatternString(),
				w.Decision, w.PatternString())
		}
	}

	SetChatPolicy(pol, postureOnRequest())
	t.Cleanup(ResetChatPolicyToDefaults)

	for _, p := range []string{
		chatWriteTestRepoRoot + "/" + devdoc.Root + "/sample-note.md",
		chatWriteTestRepoRoot + "/" + devdoc.Root + "/sub/dir/notes.md",
	} {
		if got := ChatWritePathRefusal(p, chatWriteTestRepoRoot); got != "" {
			t.Errorf("%s does not allow a write into %s/ (%s):\n%s", path, devdoc.Root, p, got)
		}
	}
	for _, p := range []string{
		chatWriteTestRepoRoot + "/internal/agent/loop.go",
		chatWriteTestRepoRoot + "/README.md",
	} {
		if got := ChatWritePathRefusal(p, chatWriteTestRepoRoot); !strings.Contains(got, chatWriteApprovalPrefix) {
			t.Errorf("%s does not make the non-corpus target %s a QUESTION:\n%s", path, p, got)
		}
	}
}

// TestRepoGOPIDocChatPolicyFenceIsValid is the OPTIONAL leg: it reads whatever
// ```chat-policy rule file THIS checkout actually has and asserts the one thing
// every copy must share — that it PARSES.
//
// WHAT IT DELIBERATELY NO LONGER ASSERTS: the fence's content. The file is the
// operator's, it is untracked and gitignored, and a host copy (a fence with no
// write rule) or a workspace copy (the fence appended) are both legitimate
// machines. Asserting content here made the same commit yield SKIP, FAIL or PASS
// depending on the checkout, which is a verdict about the environment rather than
// about this code — the specification lives in TestChatPolicySampleFixtureIsTheSpecification
// against the tracked fixture instead.
//
// ABSENT = SKIP, with the reason spelled out: a checkout with no local rule file
// is a valid empty table (the posture then answers every command), so its absence
// is not a failure of this code — but it is also not a pass, and saying so is the
// honest verdict.
func TestRepoGOPIDocChatPolicyFenceIsValid(t *testing.T) {
	path := filepath.Join(repoRootFromPackageDir(t), gopiRuleDocName)
	present, err := chatRuleFileParseVerdict(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !present {
		t.Skipf("the repository has no local rule file (%s is absent) — the rule file is OPTIONAL, "+
			"so this checkout has no operator fence to validate; the SPECIFICATION is asserted "+
			"against the tracked fixture by TestChatPolicySampleFixtureIsTheSpecification",
			gopiRuleDocName)
	}
}

// TestChatPolicyRuleFileMalformedRootFails proves the optional leg above is not a
// no-op: the same verdict function, pointed at a file that exists but carries a
// BROKEN fence, must FAIL. The broken file is written to t.TempDir() — never
// edited into this checkout, because the checkout's GOPI.md is the operator's own
// file and mutating it would be the very untracked-state dependence these tests
// exist to remove (Rule 6/Rule 29).
//
// Both broken shapes the parser reports are exercised: a rule line with an
// unreadable decision (line-level) and a fence that is never closed (file-level).
func TestChatPolicyRuleFileMalformedRootFails(t *testing.T) {
	dir := t.TempDir()

	// THE CONTROL, first: a file with the sample's own fence in it must come back
	// present AND parse-clean, otherwise the failing verdicts below would prove
	// nothing about brokenness.
	good := filepath.Join(dir, "good-"+gopiRuleDocName)
	if err := os.WriteFile(good, []byte(chatWriteSampleFence()), 0o600); err != nil {
		t.Fatalf("write the control rule file: %v", err)
	}
	present, err := chatRuleFileParseVerdict(good)
	if err != nil {
		t.Fatalf("the control rule file was rejected: %v", err)
	}
	if !present {
		t.Fatalf("the control rule file was reported ABSENT — the verdict function is not reading "+
			"what this test wrote (%s)", good)
	}

	for _, tc := range []struct {
		name  string
		body  string
		wants string
	}{
		{
			name: "unreadable decision word",
			body: "```" + "chat-policy\n" +
				"allow write " + devdoc.Root + " *\n" +
				"permit write *\n" +
				chatPolicyFenceClose,
			wants: "unknown decision",
		},
		{
			name: "unclosed fence",
			body: "```" + "chat-policy\n" +
				"allow write " + devdoc.Root + " *\n" +
				"prompt write *\n",
			wants: "never closed",
		},
	} {
		path := filepath.Join(dir, "broken-"+strings.ReplaceAll(tc.name, " ", "-")+"-"+gopiRuleDocName)
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatalf("%s: write the broken rule file: %v", tc.name, err)
		}
		present, err := chatRuleFileParseVerdict(path)
		if !present {
			t.Errorf("%s: the file was reported ABSENT — a defect is being read as \"no rules\"", tc.name)
		}
		if err == nil {
			t.Errorf("%s: a syntactically broken fence was ACCEPTED — the optional leg would be a no-op "+
				"and a broken %s would reach the chat as a silent table", tc.name, gopiRuleDocName)
			continue
		}
		if !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("%s: the refusal does not name the defect (%q): %v", tc.name, tc.wants, err)
		}
	}
}

// TestChatWritePathRefusalWithNoRulesAsksForEveryTarget is the MINIMAL REPRODUCTION
// of the defect an end-to-end case in cmd/agent was actually measuring while it
// named the edit tool (measured 2026-10-21, task 01a159ad).
//
// THE SHAPE. A run whose project context carries no ```chat-policy fence freezes
// the SHIPPED default table, which is one line — `prompt *`. Under it, and under
// the default on-request posture, EVERY write target is a question for a human;
// a `--once` console has no human, so the file is never written and the tool
// result the model receives is the approval request. A test that drives
// `chat --once --tools` at a fixture repository and then asserts on the file's
// bytes is therefore asserting, unknowingly, about the BOUNDARY — and its red is
// not evidence about whatever the case is named after.
//
// WHY THIS BELONGS HERE. The e2e case's premise was invisible: nothing in it said
// "this run may write at all", so the run silently landed on the default table.
// Here the premise is the ASSERTION: with no rules and the default posture the
// target is questioned, and the SAME target stops being questioned once a rule
// allows it (the `allow` half is the negation, so a table that permitted
// everything could not pass the pair).
func TestChatWritePathRefusalWithNoRulesAsksForEveryTarget(t *testing.T) {
	const target = chatWriteTestRepoRoot + "/notes.md" // a fixture file outside any allowed corpus

	t.Run("no rules, default posture: questioned", func(t *testing.T) {
		// ResetChatPolicyToDefaults is exactly what a run with no project context
		// file freezes: the built-in `prompt *` table and on-request.
		ResetChatPolicyToDefaults()
		t.Cleanup(ResetChatPolicyToDefaults)
		got := ChatWritePathRefusal(target, chatWriteTestRepoRoot)
		if !strings.HasPrefix(got, chatWriteApprovalPrefix) {
			t.Fatalf("with no rules the target %s must be a QUESTION (the shipped default table is `prompt *`) "+
				"— a case that assumed it was written would be measuring the boundary, got:\n%s", target, got)
		}
		if strings.Contains(got, "Refused by the CHAT WRITE BOUNDARY") {
			t.Errorf("a prompt decision must not be rendered as a refusal — the two are different answers:\n%s", got)
		}
	})

	t.Run("a rule allows it: silent pass", func(t *testing.T) {
		freezeChatRuleTable(t, chatPolicyFence("allow write notes.md"), postureOnRequest())
		if got := ChatWritePathRefusal(target, chatWriteTestRepoRoot); got != "" {
			t.Fatalf("a rule that allows the target must produce no text at all, got:\n%s", got)
		}
	})
}
