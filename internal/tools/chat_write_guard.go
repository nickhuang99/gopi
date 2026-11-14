package tools

// chat_write_guard.go — the CHAT SURFACE's write boundary, mechanism half: the
// ONE tokenizer a chat's command line is read through, and the write/edit arm's
// path predicate.
//
// NEITHER HALF CARRIES A JUDGEMENT. Which commands may run, and where a write may
// land, are both the operator's ```chat-policy fence plus this run's frozen
// posture (chat_policy_gate.go); what lives here is the tokenizer that turns a
// command line into the words chatpolicy matches (chatShellTokens /
// chatShellClauses — chatpolicy's OWN entry points take words, never text, so one
// splitter serves both) and ChatWritePathRefusal, which spells a path as a rule's
// words and returns the decision's text. There is no verb table and no tree list
// left in this file: both were the operator's judgement compiled into a binary.
//
// WHY IT LIVES HERE AND NOT IN internal/agent. Two consumers need the same
// answer — the chat SYSTEM PROMPT, which must teach the boundary (it is a prompt,
// so the model can only obey a boundary it can read), and the TOOL DISPATCH,
// which must enforce it — and Rule 13/14 forbid the two from holding separate
// spellings of one rule. This package is where gopi's command-line reading
// already lives (shared_stash_guard.go's clause parser, the preamble /
// option-operand tables, stripQuotes), so the reader below reuses that knowledge
// instead of growing a second, weaker parser in internal/agent — the exact way a
// guard comes to permit the shape its sibling refuses.
//
// THE DEFECT (operator ruling 2026-09-28, task 01a0e454). A chat session owns no
// branch and no worktree: it is started in whatever checkout the operator stands
// in and writes there, unattributed. `git commit` / `checkout` / the shared
// family from that surface rewrites the operator's own tree, creates a commit no
// ledger row owns, or — the worst shape — moves uncommitted work belonging to
// another live run onto the shared stack (the shape
// internal/tools/shared_stash_guard.go already refuses for one verb family).
//
// WHY THE CONSTRAINT AND NOT A WORKTREE: worktrees exist so execution is
// DETERMINISTIC; a chat's writes are model-driven, i.e. probabilistic, and
// handing them the worktree lifecycle reintroduces the coupling the worker split
// removes. So the boundary is: artifacts go to the run's OWN session directory
// (off every git work tree by construction), and anything that must be COMMITTED
// is routed to a task, where the worker's deterministic worktree already exists.
//
// READ-ONLY GIT IS NOT REFUSED (status/log/show/diff/ls-files/rev-parse): a
// console that cannot inspect the tree cannot answer questions about it, and
// reading mutates nothing.

import (
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/gitargv"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// ChatWriteToolBash / ChatWriteToolWrite name the two tool arms this guard can
// refuse (Rule 1: the arm names are values, not prose typed at each site).
const (
	ChatWriteToolBash  = "bash"
	ChatWriteToolWrite = "write"
)

// ChatSessionArtifactsDirName is the subdirectory of a run's OWN session
// directory where a chat's hand-written artifacts belong — the destination the
// boundary names and the refusal repeats. The name itself is DEFINED ONCE, in
// the leaf that owns the shape of a sessions tree
// (sessionlayout.ArtifactsDirName): internal/replay puts a filmed recording
// under the same subdirectory of the session it replayed, so one constant has
// two writers and neither may carry its own spelling (Rule 14). This alias is
// what the prompt (internal/agent) and the refusal read.
const ChatSessionArtifactsDirName = sessionlayout.ArtifactsDirName

// NO VERB LIVES IN THIS FILE. The thirteen git subcommand constants that used
// to sit here (add, commit, push, reset, …) were the compiled answer to "may this
// chat do that?" — the exact shape the operator's rule table replaces: a value
// compared against a parsed command word IS a policy branch, however it is
// spelled, and every one of those verbs was compared somewhere in this file.
// What remains is the tokenizer (words in, clauses out) and the write/edit path
// arm; the judgement between them is chatpolicy.Decide's.

const (
	// ChatGitMessageFlag is git commit's message flag: the ONE thing that makes a
	// commit's content predictable from the command line, which is why the
	// allowance requires it. EXPORTED because the chat prompt teaches the exact
	// commit shape and must spell the same flag (Rule 13/14: one definition, two
	// readers — internal/agent cannot re-type it).
	ChatGitMessageFlag = "-m"
	// chatGitConfigOption is git's inline-config flag — the one entry of
	// gitOptionsWithOperand that names no repository.
	chatGitConfigOption = "-c"
	// chatGitFlagTerminator ends git's option parsing: after it a word is an
	// operand (a pathspec), never a subcommand.
	chatGitFlagTerminator = "--"
)

// chatGitClauseSeparators end a simple command (the shared-stash guard's own
// list, one definition): the guard must not read a subcommand of a LATER clause
// as the subcommand of the git word in an earlier one.
const chatGitClauseSeparators = stashClauseSeparators

// chatGitPreambleWords are the wrappers that may legitimately precede the `git`
// command word — including the SHELL wrappers, because `bash -c 'git commit …'`
// is a real spelling an agent emits and reading it as "no git here" would be the
// blind spot this guard exists to avoid.
//
// IT IS THE LEAF PACKAGE'S TABLE PLUS THE SHELL WRAPPERS, not a second copy of git's
// vocabulary (Rule 13/14): gitargv.OperatorWords is where the wrapper set lives, and
// a copy here would drift the moment either side learned a command. Only the four
// interpreter words are added, and they are added HERE because they are a chat-side
// fact (a body handed to an interpreter is a script) rather than a git one — to
// gitargv they are plain words, i.e. "the git token is somebody's argument".
var chatGitPreambleWords = func() map[string]bool {
	w := make(map[string]bool, len(gitargv.OperatorWords)+len(chatShellInterpreterWords))
	for k := range gitargv.OperatorWords {
		w[k] = true
	}
	for k := range chatShellInterpreterWords {
		w[k] = true
	}
	return w
}()

func chatShellRedirectionWord(w string) (isRedirection, consumesNext bool) {
	i := 0
	for i < len(w) && w[i] >= '0' && w[i] <= '9' {
		i++
	}
	if i >= len(w) || (w[i] != '>' && w[i] != '<') {
		return false, false
	}
	for i < len(w) && (w[i] == '>' || w[i] == '<') {
		i++
	}
	return true, i >= len(w)
}
func ChatBashWriteRefusal(command string) string {
	return chatBashWriteRefusalIn(command, "", "", "")
}

// ChatBashWriteRefusalIn is ChatBashWriteRefusal with the invocation's working
// directory supplied (the bash tool's workdir, which is optional and defaults to
// the process cwd). It exists so the pathspec judgement resolves against the
// repository the command will actually run in — and so an unresolvable repository
// is a REFUSAL rather than an accidental pass.
func ChatBashWriteRefusalIn(command, workdir string) string {
	return chatBashWriteRefusalIn(command, workdir, "", "")
}

// ChatBashWriteRefusalAuthorized is ChatBashWriteRefusalIn with the operator's
// own turn message supplied, which is the ONLY thing that opens the destructive
// half of the repository's cleanup faces (chat_cleanup_authorization.go). It
// exists as a separate spelling so no caller can open that gate by forgetting to
// pass something: the three-argument forms hold it CLOSED.
func ChatBashWriteRefusalAuthorized(command, workdir, operatorTurn string) string {
	return chatBashWriteRefusalIn(command, workdir, "", operatorTurn)
}

// chatBashWriteRefusalIn is the ONE implementation; the exported spellings above
// and the agent-side hook all land here.
//
// THE DECISION IS THE OPERATOR'S, MADE IN WORD ORDER:
//
//	tokenize (chatShellTokens) -> split into clauses (chatShellClauses) ->
//	posture.Resolve(policy, clauseWords) for each -> the MOST RESTRICTIVE wins
//	(chatBashPolicyMatch) -> map the three-valued answer onto what a tool call
//	gets back.
//
// The mapping, and the reason each arm is what it is:
//
//	allow     -> ""   the command runs. The rule said so, or the posture is yolo.
//	prompt    -> chatBashPolicyText: NOT run, NOT refused. A human is the only
//	             authority, and their own words in this turn are the channel that
//	             satisfies the question (chat_approval_authorization.go); with no
//	             such words the model asks its operator in conversation and
//	             retries only on a yes. Collapsing this into either neighbour is
//	             the defect the constant chatWriteApprovalBoard is written
//	             against.
//	forbidden -> chatBashPolicyForbiddenRefusal: refused, nothing to ask, and the
//	             text names the rule and the two routes that remain.
//
// Nothing here reads git's grammar, resolves $VAR/~, or models ssh's options: the
// tokenizer sees words, the rules see words, and a shape no rule mentions is the
// POSTURE's question — which is exactly the "unreadable, therefore refused"
// special case this rewrite deleted (a `$FILES` operand is now just a word, and
// whether it may be staged is a sentence someone wrote).
//
// THE ONE THING KEPT BESIDE THE RULES is the cleanup faces' authorization gate
// (chat_cleanup_authorization.go): it is the operator's own turn, and it is a
// NARROWING of an allow, never a substitute for a rule — so it is asked LAST, and
// only when the rules already said allow. A postured/forbidden line never reaches
// it.
func chatBashWriteRefusalIn(command, workdir, repoRoot, operatorTurn string) string {
	return chatBashWriteRefusalMatch(command, chatBashPolicyMatch(command), operatorTurn)
}

// chatBashWriteRefusalMatch renders ONE already-decided answer for a command line
// — the single three-way mapping both the frozen-pair arm and the CARRIER arm
// (chat_policy_carrier.go) go through, so `prompt` cannot mean one thing to one
// caller and something else to the other.
func chatBashWriteRefusalMatch(command string, m chatpolicy.Match, operatorTurn string) string {
	switch m.Decision {
	case chatpolicy.DecisionForbidden:
		return chatBashPolicyForbiddenRefusal(command, m)
	case chatpolicy.DecisionAllow:
		// Allowed by the operator's rules (or by a yolo posture): the only gate
		// that still applies is the cleanup faces' own destructive half, which
		// the operator opens with their own words.
		return chatCleanupApplyRefusalFor(command, operatorTurn)
	default:
		// Prompt — every other value, including a Match that failed to resolve
		// (the posture still answered, so Decision is one of the three; a decode
		// bug must land on the ASKING side, never on allow).
		//
		// THE QUESTION IS ANSWERABLE (chat_approval_authorization.go). A prompt
		// decision is the operator's own "ask me first", so the operator's OWN
		// ANSWER in this turn's message is what runs it: without that read the
		// text below told the model to ask and retry, the retry met the same rule,
		// and the boundary was a permanent deadlock rather than a question
		// (measured: every bash/write call under the shipped `prompt *` table came
		// back "command NOT run" and never executed).
		//
		// An approved prompt then runs the SAME remaining gate an `allow` does —
		// the cleanup faces' destructive half still needs the operator to have
		// named cleanup, so an approval can never open more than the rule it
		// answered, and it can never reach a `forbidden` rule (that arm returned
		// above).
		if ChatTurnApprovesTheToolQuestion(operatorTurn) {
			return chatCleanupApplyRefusalFor(command, operatorTurn)
		}
		return chatBashPolicyText(command, m)
	}
}

// chatCleanupApplyRefusalFor renders the refusal for a cleanup face's `--apply`
// when this turn's operator message did not authorize it ("" when it did, or when
// the command runs no such face).
func chatCleanupApplyRefusalFor(command, operatorTurn string) string {
	requests := chatCleanupApplyRequests(command)
	if len(requests) == 0 {
		return ""
	}
	if ChatTurnAuthorizesCleanup(operatorTurn) {
		return ""
	}
	return chatCleanupApplyRefusal(requests[0].face)
}

const (
	shellBashCommandWord = "bash"
	shellShCommandWord   = "sh"
	shellZshCommandWord  = "zsh"
	shellDashCommandWord = "dash"
)

func chatShellClauses(words []string) [][]string {
	clauses := make([][]string, 0, 1)
	current := make([]string, 0, 4)
	for _, w := range words {
		if w == chatClauseBreakWord {
			if len(current) > 0 {
				clauses = append(clauses, current)
			}
			current = make([]string, 0, 4)
			continue
		}
		current = append(current, w)
	}
	if len(current) > 0 {
		clauses = append(clauses, current)
	}
	return clauses
}

// chatClauseBreakWord is a SENTINEL: a separator token the tokenizer EMITS (a word
// nothing else in a command line can be, since a NUL cannot appear in an exec'd
// argument list) rather than a string this reader compares a user's word against.
// The distinction matters: a word named ";" is not a separator once it is quoted,
// and only the tokenizer decides which is which.
const chatClauseBreakWord = "\x00sep"

// chatShellTokens is the ONE tokenizer: it splits on shell whitespace and on the
// clause separators, drops the quote characters it consumed, and emits
// chatClauseBreakWord for an UNQUOTED separator. Quoting is tracked by state, not
// by counting characters, so an apostrophe inside double quotes is literal text
// (`git commit -m "a chat's document"` is one word) while the same character in the
// open begins a single-quoted region.
//
// A HERE-DOCUMENT BODY IS DATA ONLY WHEN IT IS LITERAL (chat_shell_heredoc.go
// states the defect, the four keeping cases and the measurement). The body's
// lines are skipped, so a command the body merely QUOTES is not classified as if
// the shell were about to run it — unless the body can EXECUTE: the command
// reading it is a shell interpreter, the line pipes it into one, the delimiter is
// UNQUOTED (bash expands such a body — its backticks and `$( … )` run), or a line
// carries the delimiter plus a tail bash would not accept as a terminator.
func chatShellTokens(command string) []string {
	var (
		tokens     []string
		cur        strings.Builder
		quote      byte
		lineClause [][]string
		clause     []string
		linePiped  bool
		openers    []chatShellHeredocOpener
		keepBody   bool
	)
	flushWord := func() {
		if cur.Len() == 0 {
			return
		}
		w := cur.String()
		cur.Reset()
		tokens = append(tokens, w)
		clause = append(clause, w)
	}
	closeClause := func() {
		flushWord()
		if len(clause) > 0 {
			lineClause = append(lineClause, clause)
			clause = nil
		}
	}
	// noteOpener records a here-document opened at i; the delimiter's own bytes
	// are consumed so they never reach a word.
	noteOpener := func(i int) int {
		flushWord()
		delim, end, quoted, ok := chatShellHeredocDelimAt(command, i)
		if !ok {
			return i
		}
		openers = append(openers, chatShellHeredocOpener{delim: delim, quoted: quoted})
		// An UNQUOTED delimiter makes the body EXPANDED: bash runs every backtick
		// and `$( … )` inside it while reading, before the command that owns the
		// heredoc sees a byte (chat_shell_heredoc.go carries the measurement). Such
		// a body is a script and stays classified — the fail-closed direction.
		if !quoted || linePiped || chatShellClauseIsInterpreter(clause) {
			keepBody = true
		}
		for _, closed := range lineClause {
			if chatShellClauseIsInterpreter(closed) {
				keepBody = true
			}
		}
		return end - 1
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
		case c == '<':
			// The opener is read from the RAW text, where the heredoc operator's
			// quoting still distinguishes it from a message that spells one.
			if next := noteOpener(i); next != i {
				i = next
				continue
			}
			cur.WriteByte(c)
		case c == '`':
			closeClause()
			tokens = append(tokens, chatClauseBreakWord)
		case c == chatShellDollarSign && i+1 < len(command) && command[i+1] == chatShellParenOpen:
			// A `$( … )` SUBSTITUTION IS A COMMAND SUBSTITUTION: the shell RUNS
			// what is inside it before the word carrying it is assembled, so its
			// body is a command in its own right — the same thing a backtick
			// already is one case above. NOT descending here is what made the
			// classification of one spelling depend on its punctuation:
			// `PROBE_SUB=$(git branch -f main HEAD~3)` tokenised into the
			// single word `PROBE_SUB=$(git`, which matches no category row and fell
			// through to OPERATOR WORK — the class that RUNS — while the backtick
			// spelling of the same command was already REF MOVE.
			//
			// It is a BREAK, not a wrapper: the body's commands are judged as
			// commands (the fail-closed direction), and the word the substitution
			// was folded into (`PROBE_SUB=`) rejoins the stream after the body,
			// because a `$( … )` is a WORD PART — so `echo "$(git status)"` also
			// tokenises to the `git status` clause its backtick twin produces.
			// Without that rejoin `cd "$(dirname "$0")"` would lose its `cd`.
			closeClause()
			carry := cur.String()
			cur.Reset()
			tokens = append(tokens, chatClauseBreakWord)
			// Skip BOTH opener bytes (`$` and `(`) and read the body from the
			// byte after the parenthesis.
			depth := 1
			i++
			for i++; i < len(command) && depth > 0; i++ {
				ch := command[i]
				if ch == chatShellQuoteDouble || ch == chatShellQuoteSingle {
					// Quoting is re-entered inside the body: a quoted `)` is text.
					if end := chatShellQuotedEnd(command, i+1, ch); end > 0 {
						cur.WriteString(command[i+1 : end])
						i = end
						continue
					}
				}
				switch ch {
				case chatShellParenOpen:
					depth++
				case chatShellParenClose:
					depth--
					if depth == 0 {
						continue
					}
				case ' ', '\t':
					// The body is a command line, so its words split exactly as the
					// outer stream's do.
					flushWord()
					continue
				}
				cur.WriteByte(ch)
			}
			// The body's LAST word must close before the carriage rejoins the
			// stream, or the two fuse into one token (`masterPROBE_SUB=`), which
			// matches no category row and would have demoted the very line this
			// case exists to judge.
			flushWord()
			cur.WriteString(carry)
			i--
		case strings.IndexByte(chatGitClauseSeparators, c) >= 0:
			closeClause()
			tokens = append(tokens, chatClauseBreakWord)
			if c == chatShellPipeSeparator {
				linePiped = true
			}
			if c == refGuardLineSep[0] {
				// The line just ended: skip the bodies this line opened when they
				// are DATA, then forget the line's own bookkeeping.
				i = chatShellHeredocSkip(command, i+1, openers, linePiped, keepBody) - 1
				lineClause, linePiped, openers, keepBody = nil, false, nil, false
			}
		case c == ' ' || c == '\t':
			flushWord()
		default:
			cur.WriteByte(c)
		}
	}
	flushWord()
	return tokens
}

// The three characters a `$( … )` substitution is spelled with. They are NAMED
// (Rule 1) because the tokenizer compares command bytes against them.
const (
	chatShellDollarSign  = '$'
	chatShellParenOpen   = '('
	chatShellParenClose  = ')'
	chatShellQuoteSingle = '\''
	chatShellQuoteDouble = '"'
)

// chatShellQuotedEnd reports the index of the byte that CLOSES the quoted region
// opened at `from` (a region whose opening quote is quote), or -1 when the region
// never closes. It exists so a reader that walks a substitution's body by hand
// honours quoting the same way the tokenizer's own state machine does: a quoted
// `)` is text, not the end of the body.
func chatShellQuotedEnd(s string, from int, quote byte) int {
	for i := from; i < len(s); i++ {
		if s[i] == '\\' && quote != chatShellQuoteSingle {
			i++
			continue
		}
		if s[i] == quote {
			return i
		}
	}
	return -1
}

const (
	commandWordEval  = "eval"
	xargsCommandWord = "xargs"
	findCommandWord  = "find"
)
const chatGitReservedVerbSubject = "a git reserved verb used as a command"
const (
	chatPatchPreambleHead = "From"
	chatPatchPreambleDate = "Sep"
)
const chatGitWrappedPayloadSubject = "a wrapped command payload"

// ChatWritePathRefusal judges ONE write/edit target against the frozen
// (chatpolicy.Policy, posture) pair — the SAME table and the SAME posture the
// bash arm is judged by (chat_policy_gate.go). No path list, no git probe and no
// index question stands between a chat's write and its answer: "where may this
// console write?" is the operator's sentence, written in the project's
// ```chat-policy fence, exactly as "which commands may it run?" is.
//
// THE PATH BECOMES THE RULE'S WORDS, and the mapping is the one an operator
// writes without being taught: the fence's vocabulary is command words, so a
// write target is spelled the way a command that mentions it would be. The tool
// verb comes first — the tool's own name, read from its ONE definition
// (toolnames) rather than typed here — then ONE word per path segment, spelled
// repo-relative and slash-separated.
//
// devdoc-ref-ok: the write targets on the four lines below are vocabulary
// EXAMPLES of the matcher (what a segment list looks like), not citations.
//
//	write devdoc/chat-notes.md   ->  [write, devdoc, chat-notes.md]
//	allow write devdoc *         ->  matched by [write, devdoc, …]
//	forbidden write internal cmd ->  matched by [write, internal] and [write, cmd, …]
//
// A trailing `*` therefore means exactly what it means on a command line:
// `allow write devdoc *` permits every file under devdoc/ (and devdoc/ itself),
// `prompt write *` is the catch-all for every other target. NO SECOND MATCHER
// EXISTS — the longest prefix wins, an equal-length tie is file order, and a
// trailing `*` is worth zero specificity: all three are chatpolicy's own
// (DecideWords through Posture.Resolve), so a rule cannot mean one thing to the
// bash arm and another to write.
//
// THE THREE ANSWERS, and only three (this surface has no interactive approval
// channel — chat_policy_gate.go states why):
//
//	allow     -> "" (the write proceeds)
//	prompt    -> the approval text, naming the deciding rule's source:line
//	forbidden -> the refusal, naming the same evidence
//
// The path normalization is the one this file has always used
// (chatRepoRelativePath, then chatRepoRelativeSlash): an absolute path inside
// the resolved repo root becomes repo-relative, anything else keeps its cleaned
// spelling. A path OUTSIDE the repository is therefore spelled by its leading
// segments (`/home/user/.gopi/sessions/<id>/artifacts/x.md` -> `home nick …`),
// which no `write <tree>` rule mentions, so the POSTURE answers — the operator's
// question for a target no rule has an opinion about, which is the safe
// direction for a path nobody wrote a rule for.
func ChatWritePathRefusal(path, repoRoot string) string {
	return ChatWritePathRefusalAuthorized(path, repoRoot, "")
}

// ChatWritePathRefusalAuthorized is ChatWritePathRefusal with the operator's own
// turn message supplied — the ONLY thing that can satisfy a `prompt` decision
// (chat_approval_authorization.go), exactly as it is for the bash arm. It is a
// separate spelling so no caller can open that channel by forgetting to pass
// something: the two-argument form holds it CLOSED, which is what the default
// guard (and every test that measures the asking behaviour) uses.
func ChatWritePathRefusalAuthorized(path, repoRoot, operatorTurn string) string {
	pol, posture := ChatPolicyFrozen()
	return chatWritePathRefusalMatch(path, operatorTurn, posture.Resolve(pol, chatWriteRuleWords(path, repoRoot)))
}

// chatWritePathRefusalMatch renders ONE already-decided answer for a write target.
// It exists so the CARRIER arm (chat_policy_carrier.go) can degrade the decision
// and render it through the SAME three-way mapping — a second switch would be a
// second answer to "what does `prompt` return on a write?" (Rule 14).
func chatWritePathRefusalMatch(path, operatorTurn string, m chatpolicy.Match) string {
	switch m.Decision {
	case chatpolicy.DecisionAllow:
		return ""
	case chatpolicy.DecisionForbidden:
		return chatWritePolicyForbiddenRefusal(path, m)
	default:
		// A prompt decision is a QUESTION, and the operator's answer in THIS
		// turn is what answers it. Without that read the approval text below
		// asked the model to retry a call that would meet the same rule and
		// return the same text — a deadlock wearing a question's clothes.
		if ChatTurnApprovesTheToolQuestion(operatorTurn) {
			return ""
		}
		// THIS RUN'S OWN SESSION DIRECTORY IS NOT A QUESTION (Rule 37: it is
		// where a chat's artifacts belong — and it lies outside every git work
		// tree, so no `write <tree>` rule mentions it and the POSTURE answers,
		// which is `prompt *` by default). Only the prompt branch is exempt: a
		// `forbidden` rule already returned above and still forbids it.
		if chatWriteTargetIsOwnSessionArtifact(path) {
			return ""
		}
		return chatWritePolicyText(path, m)
	}
}

// chatWriteRuleWords turns a write target into the rule's words: the tool verb,
// then one word per segment of the repo-relative spelling.
func chatWriteRuleWords(path, repoRoot string) []string {
	rel := chatRepoRelativeSlash(chatRepoRelativePath(path, repoRoot))
	words := make([]string, 0, 4)
	words = append(words, toolnames.ToolWrite)
	for _, seg := range strings.Split(rel, "/") {
		if seg != "" && seg != "." {
			words = append(words, seg)
		}
	}
	return words
}

// chatWritePolicyText renders a PROMPT decision for a write/edit target: the
// bash arm's approval text with the two nouns this arm needs (a path, a write),
// keeping the ONE prefix a caller and the tests key on — prompt is neither an
// allow nor a refusal, and the two must be distinguishable without parsing prose.
func chatWritePolicyText(path string, m chatpolicy.Match) string {
	var b strings.Builder
	b.WriteString(chatWriteApprovalPrefix)
	b.WriteString(" `")
	b.WriteString(path)
	b.WriteString("`")
	if rule := chatPolicyRuleRendered(m); rule != "" {
		b.WriteString(" — matched rule `")
		b.WriteString(rule)
	}
	b.WriteString(".\n")
	if ChatOperatorAbsent() {
		// No operator on this run: the asking sentence below names an answer
		// that cannot arrive (see chat_operator_presence.go). The refusal is
		// still the SAME kind of answer — a question nobody can answer — and the
		// one prefix above is what a caller keys on.
		b.WriteString("This write target matches a rule the operator marked `")
		b.WriteString(decisionPromptWord())
		b.WriteString("` (or matches no rule at all, in which case the startup posture answers). The file was NOT " +
			"written.\n")
		b.WriteString(chatUnattendedPromptBody())
		return b.String()
	}
	b.WriteString("This write target matches a rule the operator marked `")
	b.WriteString(chatpolicy.DecisionPrompt.String())
	b.WriteString("` (or matches no rule at all, in which case the startup posture answers). It is therefore neither " +
		"allowed nor forbidden — it needs a HUMAN decision, and the human's decision is the approval channel on this " +
		"surface: the operator's own next message. The file was NOT written.\nDO THIS NOW: ask the operator in the " +
		"conversation — quote the exact path and say what you would put there — and wait for their answer. If they " +
		"say yes (any plain affirmative in their message: \"yes\", \"go ahead\", \"可以\", \"跑吧\" …), call `" +
		toolnames.ToolWrite + "` with the same path again: their words in that turn write it. If they say no, use " +
		"the narrower destination they approve of. Do not retry before they answer, and do not re-spell the path to " +
		"slip past the rule.")
	return b.String()
}

// chatWritePolicyForbiddenRefusal renders a FORBIDDEN decision: the target is
// wrong to write at all, so there is nothing to ask — the text says so, names the
// deciding rule, and hands the model the routes that exist.
func chatWritePolicyForbiddenRefusal(path string, m chatpolicy.Match) string {
	var b strings.Builder
	b.WriteString(chatWriteBoundaryRefusalMarker + " (")
	b.WriteString(toolnames.ToolWrite)
	b.WriteString(" `")
	b.WriteString(path)
	b.WriteString("`): ")
	if rule := chatPolicyRuleRendered(m); rule != "" {
		b.WriteString("it matches the rule `")
		b.WriteString(rule)
		b.WriteString(", which the operator marked `")
		b.WriteString(chatpolicy.DecisionForbidden.String())
		b.WriteString("`")
	} else {
		b.WriteString("the startup posture is `")
		b.WriteString(chatpolicy.DecisionForbidden.String())
		b.WriteString("` and no rule allows it")
	}
	b.WriteString(", so this chat may not author there at all. The file was NOT written.\n")
	b.WriteString("DO THIS INSTEAD: keep the artifact in THIS run's own session directory (the one named by [CURRENT " +
		"SESSION] in your prompt, e.g. <that directory>/" + ChatSessionArtifactsDirName + "/<name>.md) and hand the " +
		"operator that path; for a code change — or anything that must be committed — file a task with `gopi task " +
		"submit` and report its task id: the worker gets a deterministic worktree of its own. If the operator's " +
		"intent is genuinely to widen this boundary, that is their sentence to write in the project's " +
		"```chat-policy fence, not something to route around.")
	return b.String()
}

func chatRepoRelativeSlash(rel string) string {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return trimmed
	}
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(trimmed)), "/")
}

func chatRepoRelativePath(path, root string) string {
	clean := strings.TrimSpace(path)
	if clean == "" || root == "" {
		return clean
	}
	if !strings.HasPrefix(clean, "/") {
		return clean
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	if strings.HasPrefix(clean, prefix) {
		return strings.TrimPrefix(clean, prefix)
	}
	return clean
}

// NO REFUSAL SUBJECTS LIVE HERE ANY MORE. The block that stood in this place
// named what a PARTS-OF-A-VERB reader had failed to read (`add with no pathspec`,
// `commit without a message`, `the command line`) so one formatter could answer for
// every shape it could not classify. Those subjects existed only because the
// boundary read git's grammar and treated an unread shape as a refusal; there is no
// such reader now, and no such subject. A refusal today is chatpolicy's `forbidden`
// decision and the approval request is its `prompt` one — both rendered in
// chat_policy_gate.go, both about a RULE rather than about a verb's shape.
