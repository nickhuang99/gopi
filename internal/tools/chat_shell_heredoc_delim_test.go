package tools

// chat_shell_heredoc_delim_test.go — the DELIMITER-QUOTING half of the
// here-document reading, measured 2026-10-28 (task 01a1825a-3ed4).
//
// THE SWITCH IS THE SHELL'S OWN, not a heuristic: an UNQUOTED delimiter makes
// bash EXPAND the body — every backtick and every command substitution in it is a
// command the shell RUNS while reading, before the command that owns the heredoc
// sees a byte. Any quote or backslash ANYWHERE in the delimiter makes the whole
// body literal. Measured with git stubbed so a run is visible (the header of
// chat_shell_heredoc.go carries the transcript):
//
//	<<EOF   with a backtick'd push in the body  → REAL-GIT-RAN: push origin master
//	<<EOF   with a substitution'd push          → REAL-GIT-RAN: push origin master
//	<<'EOF' / <<"EOF" / <<\EOF / <<E"OF"        → the text, nothing runs
//
// So the four shapes below are the criterion, and the direction of each is the
// direction this reading must not blur:
//
//	quoted delimiter, non-interpreter head, no pipe  → LITERAL   → body is DATA
//	unquoted delimiter                               → EXPANDS   → body stays a SCRIPT
//	`bash <<'EOF'`                                   → the body IS the script
//	`… EOF | bash`                                   → bash accepts no such terminator
//
// The last one was a HOLE in the shipped reading, not a hypothetical: the header
// promised "a delimiter found anywhere on a line carrying a pipe … keeps the
// body", and the implementation did the opposite — measured on the pre-fix tree,
// `cat <<'EOF' … EOF | bash` classified OPERATOR WORK, i.e. the console would
// have run it. bash's own answer pins why the body must be kept: it never accepts
// `EOF | bash` as a terminator (it prints that whole line as body text and warns
// `here-document at line 1 delimited by end-of-file`).

import (
	"strings"
	"testing"
)

// The shapes a delimiter can be written in, and the body each one makes. One
// body text is reused so the ONLY variable between the arms is the delimiter.
const (
	heredocDelimQuotingShape     = "EOF"
	heredocDelimQuotingBodyPush  = "git branch -f main HEAD~3"
	heredocDelimQuotingBodyTick  = "(`git branch -f main HEAD~3`)"
	heredocDelimQuotingProbePath = "/tmp/probe.md"
)

var heredocDelimiterQuotingShapes = []struct {
	name string
	cmd  string
	want ChatOpClass
}{
	{
		name: "literal/single-quoted-delimiter-is-data",
		cmd: "cat > " + heredocDelimQuotingProbePath + " <<'" + heredocDelimQuotingShape + "'\n" +
			heredocDelimQuotingBodyTick + "\n" + heredocDelimQuotingShape,
		want: ChatOpClassOperatorWork,
	},
	{
		name: "literal/double-quoted-delimiter-is-data",
		cmd: "cat > " + heredocDelimQuotingProbePath + " <<\"" + heredocDelimQuotingShape + "\"\n" +
			"`" + heredocDelimQuotingBodyPush + "`\n" + heredocDelimQuotingShape,
		want: ChatOpClassOperatorWork,
	},
	{
		name: "literal/backslash-escaped-delimiter-is-data",
		cmd: "cat > " + heredocDelimQuotingProbePath + " <<\\" + heredocDelimQuotingShape + "\n" +
			"`" + heredocDelimQuotingBodyPush + "`\n" + heredocDelimQuotingShape,
		want: ChatOpClassOperatorWork,
	},
	{
		name: "literal/partially-quoted-delimiter-is-data",
		cmd: "cat > " + heredocDelimQuotingProbePath + " <<E\"OF\"\n" +
			"`" + heredocDelimQuotingBodyPush + "`\n" + heredocDelimQuotingShape,
		want: ChatOpClassOperatorWork,
	},
	{
		name: "expands/unquoted-delimiter-keeps-a-backticked-body",
		cmd: "cat <<" + heredocDelimQuotingShape + "\n`" +
			heredocDelimQuotingBodyPush + "`\n" + heredocDelimQuotingShape,
		want: ChatOpClassRefMove,
	},
	{
		name: "expands/unquoted-delimiter-keeps-a-plain-body-too",
		cmd: "cat <<" + heredocDelimQuotingShape + "\n" +
			heredocDelimQuotingBodyPush + "\n" + heredocDelimQuotingShape,
		want: ChatOpClassRefMove,
	},
	{
		name: "expands/unquoted-tab-stripped-operator-keeps-the-body",
		cmd: "cat <<-" + heredocDelimQuotingShape + "\n`" +
			heredocDelimQuotingBodyPush + "`\n" + heredocDelimQuotingShape,
		want: ChatOpClassRefMove,
	},
	{
		name: "expands/one-unquoted-opener-keeps-every-body-on-the-line",
		cmd: "cat <<'" + heredocDelimQuotingShape + "' > /tmp/a.md\nhello\n" + heredocDelimQuotingShape + "\n" +
			"cat <<" + heredocDelimQuotingShape + " > /tmp/b.md\n`" +
			heredocDelimQuotingBodyPush + "`\n" + heredocDelimQuotingShape,
		want: ChatOpClassRefMove,
	},
	{
		name: "interpreter/quoted-delimiter-still-keeps-the-script",
		cmd: "bash <<'" + heredocDelimQuotingShape + "'\n" +
			heredocDelimQuotingBodyPush + "\n" + heredocDelimQuotingShape,
		want: ChatOpClassRefMove,
	},
	{
		name: "pipe/terminator-carrying-a-pipe-is-not-a-terminator",
		cmd: "cat <<'" + heredocDelimQuotingShape + "'\n" +
			heredocDelimQuotingBodyPush + "\n" + heredocDelimQuotingShape + " | bash",
		want: ChatOpClassRefMove,
	},
	{
		name: "pipe/opener-carrying-a-pipe-keeps-the-script",
		cmd: "cat <<'" + heredocDelimQuotingShape + "' | bash\n" +
			heredocDelimQuotingBodyPush + "\n" + heredocDelimQuotingShape,
		want: ChatOpClassRefMove,
	},
}

// TestChatOpHeredocDelimiterQuotingDecidesTheBody drives the criterion surface the
// chat guard actually calls (ChatOpClassifyIn / ChatOpRefusalIn), so each
// assertion is about the answer the console gets back — not about a helper's shape.
//
// The arms whose `want` is REF MOVE end in a plain `git push origin master`, and
// their subject is the DELIMITER, not the tree: judged in the unmeasurable
// directory (chatOpUnmeasurableDir, chat_ops_unmeasurable_fixture_test.go) so the
// class cannot be cleared by a checkout that happens to be clean and one commit
// ahead of its upstream. Passing "" would mean "the process cwd" — measured
// 2026-11-02, that is how this arm failed in the shared main checkout.
func TestChatOpHeredocDelimiterQuotingDecidesTheBody(t *testing.T) {
	unmeasurable := chatOpUnmeasurableDir(t)
	for _, tc := range heredocDelimiterQuotingShapes {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChatOpClassifyIn(tc.cmd, unmeasurable); got != tc.want {
				t.Fatalf("class = %v, want %v\ncommand:\n%s\ntokens: %q",
					got, tc.want, tc.cmd, ChatShellTokens(tc.cmd))
			}
			refusal := ChatOpRefusalIn(tc.cmd, unmeasurable, "")
			if tc.want == ChatOpClassOperatorWork && refusal != "" {
				t.Fatalf("OPERATOR WORK must never be refused, got:\n%s", refusal)
			}
			// THE HALF THAT SURVIVED THE 2026-11-05 RULING. This arm used to assert that a
			// non-operator-work class is REFUSED. Ref, repo-write and long operations are
			// now admitted (repo-write unconditionally, ref-move by the ruling, long by
			// this machine's `auto` posture), so the refusal is no longer the property the
			// tokenizer is being judged on. What the tokenizer itself must still get right
			// is the CLASS — that a heredoc body read by a shell is the script, and a body
			// carried as data is not. The class assertion is above this line; this one now
			// pins that the criterion ANSWERED (a non-empty verdict) rather than fell silent.
			if tc.want != ChatOpClassOperatorWork && refusal != "" && !strings.Contains(refusal, "CHAT OPS CATEGORY") {
				t.Fatalf("class %v produced a refusal that is not the category criterion's: %q", tc.want, refusal)
			}
		})
	}
}

// TestChatShellHeredocSkipRefusesATerminatorCarryingATail pins the reading that
// makes the `EOF | bash` shape safe, on its own: a body must not be swallowed when
// one of its lines carries the delimiter AND a tail bash would reject.
func TestChatShellHeredocSkipRefusesATerminatorCarryingATail(t *testing.T) {
	opener := []chatShellHeredocOpener{{delim: heredocDelimQuotingShape, quoted: true}}
	command := "cat <<'" + heredocDelimQuotingShape + "'\n" + heredocDelimQuotingBodyPush + "\n" +
		heredocDelimQuotingShape + " | bash\n"
	if next := chatShellHeredocSkip(command, 0, opener, false, false); next != 0 {
		t.Fatalf("a body whose terminator line carries a tail must stay classified, skip resumed at %d", next)
	}

	clean := "cat <<'" + heredocDelimQuotingShape + "'\n" + heredocDelimQuotingBodyPush + "\n" +
		heredocDelimQuotingShape + "\n"
	if next := chatShellHeredocSkip(clean, 0, opener, false, false); next != len(clean) {
		t.Fatalf("a properly terminated literal body must be skipped entirely, skip resumed at %d of %d", next, len(clean))
	}

	unquoted := []chatShellHeredocOpener{{delim: heredocDelimQuotingShape}}
	if next := chatShellHeredocSkip(clean, 0, unquoted, false, false); next != 0 {
		t.Fatalf("an UNQUOTED delimiter expands the body, so nothing may be skipped; resumed at %d", next)
	}
}
