package notifyfeed

// sink.go — resolving WHICH file a configured notify target writes to.
//
// WHY THIS IS A GUESS AND MUST SAY SO. `notify.cmd` is an arbitrary shell
// command (that is its whole value: a file, a webhook, an ssh leg, a pipeline).
// There is therefore no field anywhere that says "the sink is /path/file", and
// inventing one would be the second source of truth the notify epic exists to
// avoid. What CAN be done honestly is recognise the shapes that name a file,
// report the recognition, and REFUSE — loudly, with the reason — when the
// command does not name one, so a consumer is told to pass --sink explicitly
// instead of being handed a wrong path.
//
// The three recognised shapes are the ones that exist in practice on this host
// and in the documentation:
//
//	cat >> /path/file.jsonl                 (the documented default)
//	... | tee -a /path/file.jsonl           (fan-out: keep a copy locally)
//	... >> /path/file.jsonl                 (any command, still a redirect)
//
// An ssh leg (`| ssh host "cat >> /remote"`) deliberately resolves NOTHING: the
// path it names is on the OTHER host and reading it here would be wrong.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// NotifyCmdEnv is the env var naming the notify command — the canonical
// spelling is config's (Rule 14: one definition), aliased here so this
// package's refusal text and its tests cannot drift from the knob the resolver
// reads.
const NotifyCmdEnv = config.NotifyCmdEnv

// SinkSource names HOW a sink path was decided, so a consumer can tell a
// configuration-derived path from one it typed (the same provenance a
// `gopi tool sandbox show` reader gets).
const (
	// SinkSourceFlag is an explicit --sink PATH: authoritative, no guessing.
	SinkSourceFlag = "flag"
	// SinkSourceNotifyCmd is a path recognised inside the configured
	// notify.cmd. It is a DERIVATION, and the caller prints it as one.
	SinkSourceNotifyCmd = "notify.cmd"
	// SinkSourceNone is the answer when nothing named a file.
	SinkSourceNone = ""
)

// The tokens a redirect is recognised by. Declared once (Rule 1b) so the
// recogniser, its diagnostics and its tests cannot drift.
const (
	// redirectOperator is the shell redirect the documented `cat >> FILE` sink
	// command uses. This recogniser is deliberately APPEND-VS-OVERWRITE
	// AGNOSTIC — it looks only for the `>` character, because both spellings
	// name the same file and a sink that is overwritten each time is still the
	// stream a consumer must read.
	redirectOperator = ">"
	// teeAppendFlag is `tee -a FILE` — the fan-out shape (write a local copy
	// AND pipe onward).
	teeAppendFlag = "-a"
	// teeCommand is the command teeAppendFlag belongs to.
	teeCommand = "tee"
	// teeAppendLongFlag is tee's long spelling of the same append request.
	teeAppendLongFlag = "--append"
	// shellQuote are the characters a shell would strip from a path token.
	shellQuote = `"'`
)

// ResolveSink decides which file to read, in the documented precedence: an
// explicit flag wins, else the path recognised inside the configured notify
// command, else nothing (SinkSourceNone, and the caller refuses).
//
// `whence` is returned alongside the path because the answer is a DERIVATION
// for the second channel, and a consumer that cannot see which channel answered
// cannot tell "the operator configured this" from "the tool guessed".
func ResolveSink(flagPath, notifyCmd string) (string, string) {
	if p := strings.TrimSpace(flagPath); p != "" {
		return p, SinkSourceFlag
	}
	if p, ok := SinkFromNotifyCmd(notifyCmd); ok {
		return p, SinkSourceNotifyCmd
	}
	return "", SinkSourceNone
}

// SinkFromNotifyCmd recognises the file a notify command appends to. It reports
// ok=false when the command names no local file.
//
// IT IS QUOTE-AWARE, and that is the whole difficulty. A fan-out command is
// commonly `… | tee -a LOCAL > /dev/null` (write a copy here, pipe onward) and a
// forwarding command is `… | ssh peer "cat >> /remote/file"` — and in the
// second the redirect is INSIDE the peer's quoted argument. A recogniser that
// scanned raw text for the first `>>` would return `/remote/file`, a path on
// another machine, and every read of it would be a lie about this host's stream.
// So tokens are resolved with their quoting, a target is only taken from a
// redirect this host actually performs, and an `ssh` token ends the scan: what
// follows it is the peer's command line.
func SinkFromNotifyCmd(cmdLine string) (string, bool) {
	tokens := tokenizeNotifyCmd(cmdLine)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.quoted {
			continue
		}
		if isRemoteCommand(tok.text) {
			// Everything after this belongs to the peer. Reporting nothing is
			// the honest answer: this host has no such file.
			return "", false
		}
		if target, ok := redirectTarget(tokens, i); ok && !isNullSink(target) {
			return target, true
		}
		if target, ok := teeTarget(tokens, i); ok && !isNullSink(target) {
			return target, true
		}
	}
	return "", false
}

// cmdToken is one shell word, with the quoting that carried it.
type cmdToken struct {
	// text is the word with its quotes stripped, exactly as a shell would hand
	// it to the program.
	text string
	// quoted is true when any part of the word was quoted — which is what makes
	// it a candidate for "this is another program's argument, not this host's
	// redirect".
	quoted bool
}

// tokenizeNotifyCmd splits a command line into shell words, honouring single
// and double quotes (and backslash escapes outside single quotes). It is not a
// shell — it does not expand anything — but it resolves quoting, which is the
// property the recogniser depends on.
func tokenizeNotifyCmd(line string) []cmdToken {
	var (
		out     []cmdToken
		cur     []rune
		quoted  bool
		inWord  bool
		quote   rune
		escaped bool
	)
	flush := func() {
		if inWord {
			out = append(out, cmdToken{text: string(cur), quoted: quoted})
		}
		cur, quoted, inWord, escaped = nil, false, false, false
	}
	for _, r := range line {
		switch {
		case escaped:
			cur = append(cur, r)
			escaped = false
			inWord = true
		case quote != 0:
			if r == quote {
				quote = 0
				quoted = true
				continue
			}
			if r == '\\' && quote == '"' {
				escaped = true
				continue
			}
			cur = append(cur, r)
		case r == '\'' || r == '"':
			quote = r
			quoted = true
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		case r == '\\':
			escaped = true
			inWord = true
		default:
			cur = append(cur, r)
			inWord = true
		}
	}
	flush()
	return out
}

// redirectCommandNames are the programs an ssh-style leg runs as. The list is
// short on purpose: recognising a command as remote wrongly would refuse a
// local sink, while missing one only costs a wrong path — so it is the
// conservative direction that matters, and `ssh` is the only spelling in use.
var redirectCommandNames = []string{"ssh", "scp", "rsync", "sshpass"}

// isRemoteCommand reports whether a token names a program that forwards the
// payload to another host — the point after which no path this host sees is the
// sink.
func isRemoteCommand(tok string) bool {
	for _, name := range redirectCommandNames {
		if tok == name || strings.HasSuffix(tok, "/"+name) {
			return true
		}
	}
	return false
}

// nullSinks are the targets that are not a stream at all — discarding, or the
// terminal. A fan-out command writes its copy with `tee -a FILE` and discards
// the pipeline's own stdout with `> /dev/null`, so a recogniser that stopped at
// the redirect would name the bit bucket.
var nullSinks = []string{"/dev/null", "/dev/stdout", "/dev/stderr", "/dev/zero", "-"}

// isNullSink reports whether a resolved target names no readable stream.
func isNullSink(path string) bool {
	for _, n := range nullSinks {
		if path == n {
			return true
		}
	}
	return false
}

// redirectTarget takes the file named by the redirect at or fused into tokens[i].
// It accepts the operator as its own word (`>> FILE`, `> FILE`), fused with a
// file descriptor (`1>> FILE`), and fused with the target (`>>FILE`). The
// prefix-before-the-operator must be empty or all digits, so an argument that
// merely CONTAINS a `>` (`-o x>y`) is never read as a redirect.
func redirectTarget(tokens []cmdToken, i int) (string, bool) {
	text := tokens[i].text
	first := strings.IndexAny(text, ">")
	if first < 0 {
		return "", false
	}
	if prefix := text[:first]; prefix != "" && !allDigits(prefix) {
		return "", false
	}
	// The run of operator characters (a fused `2>>`, a bare `>` or `>>`), then
	// whatever is fused after it.
	rest := strings.TrimLeft(text[first:], redirectOperator)
	if rest != "" {
		return trimShellToken(rest), true
	}
	if i+1 >= len(tokens) {
		return "", false
	}
	// The target is the next word. It is taken whether or not it was quoted:
	// a redirect this host performs is a redirect this host performs, and the
	// quoting only tells a shell what to strip.
	if target := trimShellToken(tokens[i+1].text); target != "" {
		return target, true
	}
	return "", false
}

// teeTarget takes the file named by `tee -a FILE` / `tee --append FILE` at
// tokens[i]. It exists because a FAN-OUT writes its local copy through tee
// rather than through a redirect of the pipeline — the shape the notify epic's
// own subscription contract recommends.
func teeTarget(tokens []cmdToken, i int) (string, bool) {
	if !strings.HasSuffix(tokens[i].text, teeCommand) && tokens[i].text != teeCommand {
		return "", false
	}
	for j := i + 1; j < len(tokens) && j <= i+teeFlagLookahead; j++ {
		t := tokens[j].text
		if t == teeAppendFlag || t == teeAppendLongFlag {
			if j+1 >= len(tokens) {
				return "", false
			}
			return trimShellToken(tokens[j+1].text), true
		}
	}
	return "", false
}

// teeFlagLookahead is how far past `tee` the append flag may sit (its own short
// flags, then -a/--append).
const teeFlagLookahead = 4

// allDigits reports whether every rune is an ASCII digit (the file-descriptor
// prefix of a fused redirect). An empty string is NOT all digits — the caller
// checks that case separately.
func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// trimShellToken strips the quoting a shell would have removed, and refuses a
// token that is not a path at all (a flag, or the empty string) so the caller
// falls through to "nothing named a file" rather than to a bogus path.
func trimShellToken(tok string) string {
	tok = strings.Trim(tok, shellQuote)
	if tok == "" || strings.HasPrefix(tok, "-") {
		return ""
	}
	return tok
}

// SinkRefusal explains why no sink could be resolved, naming the configured
// command so the reader can see what was inspected. It is a message, not an
// error value: the caller has the exit code, this is what the operator reads.
func SinkRefusal(notifyCmd string) string {
	if strings.TrimSpace(notifyCmd) == "" {
		return "notify: no sink configured and none given — set notify.cmd (or " + NotifyCmdEnv + "), or pass --sink PATH"
	}
	return fmt.Sprintf("notify: the configured command names no local file to read — it is %q; "+
		"pass --sink PATH to name the stream explicitly (a command is arbitrary, so this is a refusal, not a guess)", notifyCmd)
}
