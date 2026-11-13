package tools

// chat_shell_heredoc_test.go — the regression guard for task 01a17e03-12f0:
// "gopi notify was classified REF-MOVE and refused".
//
// THE MEASURED DEFECT. Writing a message file with a heredoc and posting it with
// `gopi notify --message "$(cat file)"` is ONE legal console action. Its body was
// being tokenised as if the shell would run it, so a body that merely QUOTED
// `git push origin master` — the ordinary shape of an explanation — classified the
// whole line as the most restrictive class and the NOTIFY ITSELF was refused as a
// ref move.
//
// The three assertions the fix must hold simultaneously, and neither half alone:
//
//	(a) a heredoc body written to a FILE by a non-interpreter is DATA  → OPERATOR WORK
//	(b) a heredoc body read by a SHELL is the script                    → REF MOVE
//	(c) a short notify with no heredoc is unchanged                     → OPERATOR WORK
//
// (b) is the direction that keeps the fix from becoming a hole: the widening it
// prevents is "stop classifying after a heredoc", which would let
// `bash <<'EOF' … git push … EOF` through.

import (
	"strings"
	"testing"
)

// heredocRegressionDelim is the quoted delimiter the fixture writes — quoted so
// the body is literal, which is how a message file is written.
const heredocRegressionDelim = "EOF"

// heredocRegressionCommands is ONE table of the shapes the criterion must judge,
// each with the class it must produce. It is data, not three hand-written tests,
// because the two directions (a) and (b) are the same reader asked twice.
var heredocRegressionCommands = []struct {
	name string
	cmd  string
	want ChatOpClass
}{
	{
		name: "notify/heredoc-body-quoting-a-ref-move-is-data",
		cmd: "cd /home/nick/work/gopi\n" +
			"cat > /tmp/reply.md <<'" + heredocRegressionDelim + "'\n" +
			"## Q2\n" +
			"\n" +
			"git branch -f main HEAD~3                  # operator runs this\n" +
			"EOF\n" +
			"./bin/gopi notify --session 66243525-16ab-4ce7-9133-84d74c539840 --message \"$(cat /tmp/reply.md)\"",
		want: ChatOpClassOperatorWork,
	},
	{
		name: "notify/heredoc-body-quoting-update-ref-and-force-push",
		cmd: "cat > /tmp/r.md <<'" + heredocRegressionDelim + "'\n" +
			"git update-ref refs/heads/main HEAD~1\n" +
			"git branch -D main\n" +
			"git reset --hard HEAD~3\n" +
			"EOF\n" +
			"gopi notify --session s1 --message \"$(cat /tmp/r.md)\"",
		want: ChatOpClassOperatorWork,
	},
	{
		name: "notify/heredoc-bodied-append",
		cmd: "cat >> /tmp/r.md <<'" + heredocRegressionDelim + "'\n" +
			"git branch -f main HEAD~3\n" +
			"EOF\n" +
			"gopi notify --session s1 --message \"$(cat /tmp/r.md)\"",
		want: ChatOpClassOperatorWork,
	},
	{
		name: "bash/heredoc-body-is-the-script",
		cmd: "bash <<'" + heredocRegressionDelim + "'\n" +
			"git branch -f main HEAD~3\n" +
			"EOF",
		want: ChatOpClassRefMove,
	},
	{
		name: "bash/heredoc-body-through-a-pipe-is-the-script",
		cmd: "cat <<'" + heredocRegressionDelim + "' | bash\n" +
			"git branch -f main HEAD~3\n" +
			"EOF",
		want: ChatOpClassRefMove,
	},
	{
		name: "sh/pathed-interpreter-still-keeps-the-body",
		cmd: "/bin/sh <<'" + heredocRegressionDelim + "'\n" +
			"git branch -f main HEAD~3\n" +
			"EOF",
		want: ChatOpClassRefMove,
	},
	{
		name: "notify/short-message-no-heredoc",
		cmd:  `gopi notify --session s1 --message "the build is green"`,
		want: ChatOpClassOperatorWork,
	},
	{
		name: "notify/message-that-quotes-the-operator-however-it-is-quoted",
		cmd:  `gopi notify --session s1 --message 'a; git push origin master'`,
		want: ChatOpClassOperatorWork,
	},
	{
		name: "refmove/after-the-body-is-still-judged",
		cmd: "cat > /tmp/r.md <<'" + heredocRegressionDelim + "'\n" +
			"git branch -f main HEAD~3\n" +
			"EOF\n" +
			"git update-ref refs/heads/x HEAD",
		want: ChatOpClassRefMove,
	},
	{
		name: "refmove/carried-in-by-echo-of-a-message-is-not-a-ref-move-but-the-real-one-is",
		cmd: "cat > /tmp/r.md <<'" + heredocRegressionDelim + "'\n" +
			"nothing here\n" +
			"EOF\n" +
			"gopi notify --session s1 --message \"$(cat /tmp/r.md)\" && git update-ref refs/heads/x HEAD",
		want: ChatOpClassRefMove,
	},
	{
		name: "operator-work/an-unterminated-body-is-data-to-EOF-as-bash-reads-it",
		cmd: "cat > /tmp/r.md <<'" + heredocRegressionDelim + "'\n" +
			"git push origin master",
		want: ChatOpClassOperatorWork,
	},
}

// TestChatOpHeredocBodyIsDataNotACommand drives the criterion surface the chat
// guard actually calls (ChatOpClassifyIn / ChatOpRefusalIn), so a passing
// assertion is about the answer the console gets, not about a helper's shape.
//
// EVERY ARM IS JUDGED IN THE UNMEASURABLE DIRECTORY (chatOpUnmeasurableDir,
// chat_ops_unmeasurable_fixture_test.go), for the reason the whole table exists:
// these assertions are about whether a here-document BODY is data or a script, so
// the class must come from the WORDS. Passing "" would read as "the process cwd"
// (the contract ChatOpClassifyIn and chatOpRunDirRoot both state), and the arms
// whose `want` is REF MOVE would then be demoted to REPOSITORY WRITE in a checkout
// that is clean and one commit ahead of its upstream — measured 2026-11-02 in the
// shared main checkout, where three heredoc arms failed for exactly that.
func TestChatOpHeredocBodyIsDataNotACommand(t *testing.T) {
	unmeasurable := chatOpUnmeasurableDir(t)
	for _, tc := range heredocRegressionCommands {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChatOpClassifyIn(tc.cmd, unmeasurable); got != tc.want {
				t.Fatalf("class = %v, want %v\ncommand:\n%s\ntokens: %q",
					got, tc.want, tc.cmd, ChatShellTokens(tc.cmd))
			}
			// The refusal is the second half of the complaint: a notify refused by
			// the CATEGORY criterion is the measured defect, and an empty string is
			// what the guard returns when the call may start.
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

// TestChatShellTokensHeredocBodyLeavesNoWords pins the tokenizer half on its own,
// which is where the defect lived: the body's words must not appear in the stream
// at all when the body is data, and must appear when it is a script.
func TestChatShellTokensHeredocBodyLeavesNoWords(t *testing.T) {
	data := "cat > /tmp/r.md <<'" + heredocRegressionDelim + "'\n" +
		"MARKER_INSIDE_THE_BODY\n" +
		"EOF\n" +
		"echo after"
	bodyWord := "MARKER_INSIDE_THE_BODY"
	tokens := ChatShellTokens(data)
	if strings.Contains(strings.Join(tokens, " "), bodyWord) {
		t.Fatalf("a here-document body written to a file leaked into the token stream: %q", tokens)
	}
	if !strings.Contains(strings.Join(tokens, " "), "echo after") {
		t.Fatalf("the command AFTER the body must still be tokenised: %q", tokens)
	}

	script := "bash <<'" + heredocRegressionDelim + "'\n" +
		bodyWord + "\n" +
		"EOF"
	if got := strings.Join(ChatShellTokens(script), " "); !strings.Contains(got, bodyWord) {
		t.Fatalf("a here-document body FED TO AN INTERPRETER must stay in the stream: %q", got)
	}
}

// TestChatShellHeredocOpWordKeepsAQuotedOperator keeps the fix from reading an
// OPTION'S VALUE as a here-document opener: `--message "<<EOF"` is a message that
// quotes the delimiter, and reading it as an opener would stop classifying the
// next line — a widening nobody declared.
//
// The next line is a plain `git branch -f main HEAD~3`, so the assertion is about
// the TOKENISER keeping it a command at all; judged in the unmeasurable directory
// (chatOpUnmeasurableDir) so the class cannot come from the checkout `go test`
// happens to stand in.
func TestChatShellHeredocOpWordKeepsAQuotedOperator(t *testing.T) {
	cmd := "gopi notify --session s1 --message \"<<EOF\"\n" +
		"git branch -f main HEAD~3"
	if got := ChatOpClassifyIn(cmd, chatOpUnmeasurableDir(t)); got != ChatOpClassRefMove {
		t.Fatalf("an option value spelling a here-doc operator must not open a body: class = %v, tokens = %q",
			got, ChatShellTokens(cmd))
	}
}
