package tools

// chat_ops_authorization.go — WHICH operations a chat turn may START ON ITS
// OWN, judged by the CATEGORY OF THE ACTION the operator has to take by hand,
// plus the operator's authorisation, instead of by a stopwatch.
//
// THE CRITERION BEFORE THIS ONE, AND THE VACUUM IT LEFT. It judged a call by a
// declared DURATION (≤ 30 s = start it, ≤ 60 s = only with the operator's own
// budget, else ask), with a vocabulary of "long classes" as the fallback. Two
// defects, both measured, and they are the same defect:
//
//   - an ORDINARY OPERATION was refused for having no declared budget. `git
//     status`, `ls`, `gopi task list` are work the operator would have done by
//     hand in ten seconds; asking a human about them is exactly the stalling the
//     operator's 2026-10-10 ruling names ("all research/operation except of
//     coding/debugging should be duty of gopi chat"). Under all-ask — the
//     DEFAULT posture — every undeclared call fell to ASK, so the criterion was
//     strictly WIDER than it looked: deleting the 30/60 constants alone would
//     not have relaxed it, it would have made EVERY undeclared call ask. This
//     file exists in the shape it does to avoid that vacuum.
//   - a DURATION is not knowable here (nothing in gopi times commands, and a
//     store of measured durations would be a database built to answer a question
//     a CATEGORY answers better, Rule 35), and the stopwatch asked the model to
//     declare `timeout_sec` for a call whose duration it cannot know — i.e. to
//     self-attest. The bash tool's `timeout_sec` is an EXECUTION CAP (it kills
//     the process group), never the criterion: this file no longer reads it at
//     all.
//
// THE OPERATOR'S RULING replaces the question with a CLASSIFICATION. Every bash
// call is exactly one of three classes, and a reader can place a command in one
// by asking "what IS this action", never "how long would it take":
//
//	ChatOpClassOperatorWork — work the operator would otherwise do by hand:
//	    ops, DevOps, the network, a script, a document, a query, a diagnosis, a
//	    bug row, ledger/housekeeping edits, a short job. RUN IT. No permission
//	    question exists for this class — that is the point of the ruling.
//	ChatOpClassRepoWrite — WRITING THE REPOSITORY: editing source, running the
//	    gate against that change, staging/committing. This is the ONE hard no-go
//	    and it did not move (Rule 37, internal/tools/chat_write_guard.go): it is
//	    a TASK (`gopi task submit`), however fast it would be.
//	ChatOpClassLong — the action is ITSELF long: a whole-tree test, a full
//	    build, a package install, a merge, a worker, a serve/sync sweep. These
//	    must be ASKED about first, because the operator is the one who pays the
//	    minutes and cannot take an unbounded operation back.
//
// THE STANDING POSTURE STILL EXISTS and now means one thing: when a LONG
// operation carries no authorisation from THIS turn's operator message,
// `GOPI_CHAT_OP_AUTO` (ChatOpAutoEnv) > settings.json "chat_op_auto"
// (ChatOpAutoSettingsKey) > ALL-ASK decides whether the console may start it
// anyway. The posture is deliberately NOT consulted for the other two classes: an
// operator-work call needs no permission at any setting, and a repository write
// is never opened by a knob.
//
// WHAT IS AND IS NOT KNOWABLE (Rule 15, the honesty half). A classification is a
// property of the WORDS, and the words are in the command line — so the two
// CLASS predicates are exact over the vocabulary they carry and say so: a command
// matching no word is OPERATOR WORK (the safe direction the operator chose: the
// console is authorised to do what a human does), while a false negative on the
// repo-write/long vocabulary is possible and is stated in the refusal, which
// names the class it actually read. The operator's authorisation for the long
// class is read from THIS turn's own message ("最多 5 分钟", "跑吧") and is never
// stored, so the gate cannot be left open by a previous turn.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// ChatOpAutoEnv / ChatOpAutoSettingsKey are the two channels of the standing
// posture, ALIASED from the config package rather than re-spelled here (Rule
// 13/14: one definition, and the config layer is where the settings FILE's key
// names live — settings.go's catalog row binds them, so a second spelling in this
// package would be the mirror defect the rule names).
const (
	ChatOpAutoEnv         = config.ChatOpAutoEnv
	ChatOpAutoSettingsKey = config.ChatOpAutoSettingsKey
)

// ChatOpChannelAllAsk / ChatOpChannelAuto name the two postures in one place,
// because three readers render them (the refusal, the prompt, and the setting's
// own reporting) and a fourth spelling would drift (Rule 13/14).
const (
	ChatOpChannelAllAsk = "all-ask"
	ChatOpChannelAuto   = "auto"
)

// chatOpMinuteSecs / chatOpHourSecs convert the units the operator's own words
// may name (Rule 1: a compared value is a constant).
const (
	chatOpMinuteSecs = 60
	chatOpHourSecs   = 3600
)

// chatOpSecondUnits / chatOpMinuteUnits / chatOpHourUnits are the unit words the
// operator's message may carry when it authorises a long operation. Longest-first
// at the match site, so `min` cannot shadow `minutes` and `m` cannot shadow `min`.
var (
	chatOpSecondUnits = []string{"seconds", "second", "secs", "sec", "秒钟", "秒", "s"}
	chatOpMinuteUnits = []string{"minutes", "minute", "mins", "min", "分钟", "分", "m"}
	chatOpHourUnits   = []string{"hours", "hour", "hrs", "hr", "小时", "h"}
)

// bashTimeoutArgName / ChatBashTimeoutArg are the bash tool's per-call time
// budget argument. It is NOT part of the criterion any more — it is the tool's
// EXECUTION CAP (it kills the process group at that number), and a cap cannot say
// what an action IS. It stays named here because two surfaces state that very
// fact to a reader (the prompt's rule (6) and the refusal), and because the tool
// schema and those sentences must spell one word (Rule 1/13/14).
const (
	bashTimeoutArgName = "timeout_sec"
	ChatBashTimeoutArg = bashTimeoutArgName
)

// ChatOpClass is the CATEGORY of one operation — the whole criterion, in three
// values. It is exported (Rule 14) because the criterion's second surface, the
// chat prompt in internal/agent, RENDERS these classes and the same-origin test
// (internal/agent/chat_ops_category_test.go) compares the rendered vocabulary
// with the vocabulary this file matches on.
type ChatOpClass int

const (
	// ChatOpClassOperatorWork is work the operator would otherwise do by hand:
	// the class a chat RUNS without asking anybody.
	ChatOpClassOperatorWork ChatOpClass = iota
	// ChatOpClassRepoWrite is WRITING THE REPOSITORY (source edit, the gate run
	// against that edit, a stage/commit): the console's own work under the
	// 2026-10-24 ruling, so it runs without a question.
	ChatOpClassRepoWrite
	// ChatOpClassRefMove is the DESTRUCTIVE REF-MOVE FAMILY (branch force-move or
	// force-delete, update-ref, a force push). It is NOT a stronger repo-write: the
	// ruling that lets the console commit does not let it move a published ref
	// (PROJECT.md Rule 37), and before this class existed `git branch` matched no
	// category line and fell through to OPERATOR WORK — measured 2026-10-24:
	// `git branch -f main HEAD~3` => OPERATOR WORK, allowed.
	ChatOpClassRefMove
	// ChatOpClassLong is an action that is ITSELF long — the only class that
	// must be asked about first.
	ChatOpClassLong
)

// String renders a class in the words the refusal and the prompt print. One
// spelling (Rule 1/13): a second one would let the prompt teach a class label the
// refusal never uses.
func (c ChatOpClass) String() string {
	switch c {
	case ChatOpClassRepoWrite:
		return "REPOSITORY WRITE"
	case ChatOpClassRefMove:
		return "REF MOVE"
	case ChatOpClassLong:
		return "LONG OPERATION"
	default:
		return "OPERATOR WORK"
	}
}

// THE CATEGORY TABLE IS DATA, NOT THIS FILE'S LITERALS. The words that decide the
// three classes live in ONE place — the ```chat-op-category fence of the project's
// GOPI.md, parsed by internal/chatpolicy and falling back to its built-in table
// (chatpolicy.DefaultCategories). This file used to carry three Go slices
// (chatOpRepoWriteWords / chatOpGateWords / chatOpLongOperationWords) and that was
// the defect the rule layer exists to end: revising "which operations are a
// repository write" was a code change and a release, and the words a model was
// taught (the prompt) and the words the guard matched were two copies that could
// drift apart.
//
// THE TABLE IS FROZEN BESIDE THE POLICY — chat_policy_gate.go's
// SetChatOpCategories, resolved at the same startup moment from the same document
// and held under its OWN lock, because the two answer two questions about one line
// ("what is this action?" vs "may it run?") and a single holder would invite a
// reader to treat one as the other.
//
// THE ENFORCEMENT BOUNDARY IS NOT THIS FILE. A category class is a CLASSIFICATION:
// it decides what a console may START ON ITS OWN, never whether a line is allowed
// to run. That question belongs to chatBashPolicyMatch (```chat-policy fence +
// posture), and no line of this fence can answer it — the two tables are different
// types parsed from different fences (internal/chatpolicy/category.go states the
// vacuum that separation prevents: a class word in the enforcement table would
// change the policy when somebody merely renamed a label).

// chatOpCategoryTable returns the frozen category table. A nil table (a process
// that never resolved one) is replaced by the built-in defaults so the criterion
// never judges against nothing: the safe reading of "no table" is the SHIPPED
// vocabulary, not "nothing is a repository write".
func chatOpCategoryTable() *chatpolicy.CategoryTable {
	if tab := ChatOpCategoryFrozen(); tab != nil {
		return tab
	}
	return defaultChatOpCategories()
}

// defaultChatOpCategories parses the built-in table. A parse failure of a constant
// embedded in the binary is a programming error; the criterion must not then judge
// by a half-read table, so it falls back to an EMPTY table — every line lands in
// the default class (operator work), which is the operator's own direction and the
// direction Rule 15 says to fail towards.
func defaultChatOpCategories() *chatpolicy.CategoryTable {
	tab, err := chatpolicy.DefaultCategories()
	if err != nil {
		return &chatpolicy.CategoryTable{Source: "built-in chat-op-category defaults (unparseable)"}
	}
	return tab
}

// chatOpClassOfWord maps a category line's class word to the class value. The
// table's words are chatpolicy's (parsed), so the mapping is the ONE place the two
// vocabularies meet; an unknown word can only come from a table this package did
// not parse, and it names no class (ok=false) rather than guessing one.
func chatOpClassOfWord(word string) (ChatOpClass, bool) {
	switch word {
	case chatpolicy.CategoryOperatorWorkWord:
		return ChatOpClassOperatorWork, true
	case chatpolicy.CategoryRepoWriteWord:
		return ChatOpClassRepoWrite, true
	case chatpolicy.CategoryRefMoveWord:
		return ChatOpClassRefMove, true
	case chatpolicy.CategoryLongWord:
		return ChatOpClassLong, true
	}
	return ChatOpClassOperatorWork, false
}

// ChatOpRepoWriteVocabulary / ChatOpLongOperationVocabulary hand the class table's
// words to the OTHER surface (the prompt) as copies, so a caller cannot mutate the
// criterion's own vocabulary — and so the prompt has exactly one source to render
// (Rule 13/14; the same-origin test compares the rendering with a real verdict).
func ChatOpRepoWriteVocabulary() []string {
	return append([]string(nil), chatOpCategoryTable().WordsOf(chatpolicy.CategoryRepoWriteWord)...)
}

// ChatOpLongOperationVocabulary is ChatOpRepoWriteVocabulary for the long class.
func ChatOpLongOperationVocabulary() []string {
	return append([]string(nil), chatOpCategoryTable().WordsOf(chatpolicy.CategoryLongWord)...)
}

// ChatOpRenderedClassList renders words as one readable phrase for the prompt
// (the class's own words, joined by "/") — the spelling the model is handed. It
// exists so the prompt types no operation name at all.
//
// THE TRAILING WILDCARD IS DROPPED. A category line is written `git add *` because
// that is how a rule says "this verb, whatever its arguments"; a PROMPT that
// printed the asterisk would teach the model to type it. The words inside a
// rendered list are therefore the rule's prefix, which is what a reader needs, and
// the same rendering is what the same-origin test compares against.
func ChatOpRenderedClassList(words []string) string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, strings.TrimSuffix(w, " "+chatpolicy.CategoryWildcardWord))
	}
	return strings.Join(out, "/")
}

// chatOpVocabularyRendered renders ONE class's whole vocabulary for a sentence the
// prompt and a refusal both print. The words come from the frozen table (never a
// literal here), so what the model reads is what the classifier matches.
func chatOpVocabularyRendered(class ChatOpClass) string {
	word := ""
	switch class {
	case ChatOpClassRepoWrite:
		word = chatpolicy.CategoryRepoWriteWord
	case ChatOpClassLong:
		word = chatpolicy.CategoryLongWord
	default:
		return ""
	}
	return ChatOpRenderedClassList(chatOpCategoryTable().WordsOf(word))
}

// ChatOpAdmission is the verdict for one bash call: whether a chat turn may START
// it, WHICH CLASS it fell in, and why. The reason is carried (not just the
// boolean) because the refusal must name what it read, and because a test asserts
// the DISTINCTION between "the operator authorised this long operation" and "the
// posture is auto" (two different facts with the same value — the distinction
// config.TaskSyncLine exists to print).
type ChatOpAdmission struct {
	// class is the category the command fell in.
	class ChatOpClass
	// authorizedByOperator is true when THIS turn's message declared a budget,
	// which is what opens the long class.
	authorizedByOperator bool
	// channel is the posture that decided an UNAUTHORISED long operation
	// (ChatOpChannelAllAsk | ChatOpChannelAuto).
	channel string
	// reason names the rule that decided, in the words the refusal prints.
	reason string
}

// Allowed reports the verdict.
func (a ChatOpAdmission) Allowed() bool { return a.reason == "" }

// Class reports the category the call fell in — exported because the prompt's
// same-origin test and the refusal both name it.
func (a ChatOpAdmission) Class() ChatOpClass { return a.class }

// Reason names the rule that decided, in the words the refusal prints.
func (a ChatOpAdmission) Reason() string { return a.reason }

// Channel reports which posture decided an unauthorised long operation.
func (a ChatOpAdmission) Channel() string { return a.channel }

// ChatOpChannel reports the operator's standing posture for LONG chat-initiated
// operations: `GOPI_CHAT_OP_AUTO` (env) > settings.json "chat_op_auto" >
// ChatOpChannelAllAsk. Resolution shape and the falsey vocabulary are
// config.TaskSyncEnabled's (Rule 2/13/14: one knob, one reader, three labels), and
// an env value that is NEITHER spelling is announced on stderr and left to the
// settings channel — never silently read as "ask", which is the defect Rule 15
// bans for a switch whose off state is indistinguishable from its default.
func ChatOpChannel() string {
	if raw := strings.TrimSpace(os.Getenv(ChatOpAutoEnv)); raw != "" {
		if config.IsTrueValue(raw) {
			return ChatOpChannelAuto
		}
		if config.IsFalseValue(raw) {
			return ChatOpChannelAllAsk
		}
		fmt.Fprintf(os.Stderr, "tools: %s=%q is not a recognised boolean (%s/%s) — ignored; %s decides\n",
			ChatOpAutoEnv, raw, config.EnvBoolOn, "off", ChatOpAutoSettingsKey)
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil {
		// The settings file is the declaration of record; unreadable means the
		// answer is unknown, and unknown resolves to the SAFE posture (ASK) —
		// announced, never silent.
		fmt.Fprintf(os.Stderr, "tools: %s unreadable (%v) — %s resolves to its default (%s)\n",
			config.ResolveSettingsPath(), err, ChatOpAutoSettingsKey, ChatOpChannelAllAsk)
		return ChatOpChannelAllAsk
	}
	if cfg != nil && cfg.ChatOpAuto {
		return ChatOpChannelAuto
	}
	return ChatOpChannelAllAsk
}

// ChatOpClassify places ONE command line in exactly one of the three classes.
//
// ORDER IS THE CRITERION'S MEANING, and it is repo-write first: `go test
// ./internal/tools/` matches both the source-tree gate words and the long
// vocabulary, and the operator's ruling is that the gate run over the console's
// own change is a REPOSITORY WRITE (a task), not a long operation to ask about.
// Everything else defaults to OPERTOR WORK — the class that runs — because that is
// the operator's own default ("everything except coding/debugging is yours").
//
// THE COMMAND IS TOKENISED, NOT SEARCHED. substrings.Contains is gone: the words a
// category line declares are matched against the words the ONE tokenizer produced
// (ChatShellTokens, the same token stream the enforcement arm judges and the rule
// layer's Decide reads), and a rule matches when it is a PREFIX of the line's
// words. This is a DELIBERATE semantic change from the substring predicate it
// replaces, and it is the direction that makes the table writable: `xmake test y`
// is no longer a whole-tree suite, `git commit -m "a chat's document"` is one
// clause however its apostrophe is quoted, and `> internal/agent/x.go` names the
// source tree through the directory-prefix word `internal/` instead of a
// fourteen-line list of paths that could only ever be incomplete. A clause is
// classified on its own and the LINE takes the MOST RESTRICTIVE class, the same
// composition the enforcement arm applies.
func ChatOpClassify(command string) ChatOpClass {
	return chatOpClassifyIn(command, "")
}

// ChatOpClassifyIn is ChatOpClassify with the invocation's WORKING DIRECTORY
// supplied (the bash tool's own `workdir` argument; "" = the process cwd).
//
// It exists because the caller (the bash tool) may name a directory and the
// answer must not depend on where the PROCESS happens to stand; both spellings
// land in the same one implementation (Rule 13/14), and neither reads anything
// from the directory: since the operator's 2026-11-03 ruling ("push 都可以", row
// 01a1a786) every `git push` spelling is REPOSITORY WORK by its WORDS, so the
// directory is carried for the callers' sakes, not to decide a push.
func ChatOpClassifyIn(command, workdir string) ChatOpClass {
	return chatOpClassifyIn(command, workdir)
}

// chatOpClassifyIn is the ONE classifier: the category table's words and the ONE
// clause splitter. The exported spellings above both land here. It carries no
// demotion of its own — the two push demotions (dry run, clone ground) and the
// fast-forward one are GONE with the ref-move membership of `git push`
// (2026-11-03 ruling, row 01a1a786): nothing about a push is left to measure.
func chatOpClassifyIn(command, workdir string) ChatOpClass {
	tab := chatOpCategoryTable()
	worst := ChatOpClassOperatorWork
	clauses := chatOpLineClauses(command)
	// THE DESCENDED COMMANDS ARE JUDGED, NOT JUST THE TYPED ONES. A clause that
	// hands a script to an interpreter (`bash -c '…'`, `eval '…'`, `sh -c …`) is
	// not a command to classify — its ARGUMENT is, and so is every command inside
	// a `$( … )` substitution the ONE tokenizer already cuts into clauses. Without
	// this descent the classification of one command depended on its PUNCTUATION:
	// `git branch -f main HEAD~3` and its backtick spelling read REF MOVE,
	// while `PROBE_SUB=$(…)`, `bash -c '…'` and `eval '…'` read OPERATOR WORK —
	// the class that RUNS. The descent is fail-closed in both directions: a nested
	// command that matches no category row contributes OPERATOR WORK, exactly as a
	// top-level one does, so descending can only ever RAISE a line's class.
	for _, judged := range chatOpJudgedClauses(clauses) {
		words := judged.words
		rule, ok := tab.MatchCategory(words)
		if !ok {
			continue
		}
		class, known := chatOpClassOfWord(rule.Category)
		if !known {
			continue
		}
		if class > worst {
			worst = class
		}
	}
	return worst
}

// chatOpLineClauses splits a command line into the token streams one clause at a
// time — the ONE tokenizer and the ONE clause splitter (chatShellTokens /
// chatShellClauses), never a second implementation. An empty line has no clause.
func chatOpLineClauses(command string) [][]string {
	return chatShellClauses(chatShellTokens(command))
}

// chatOpMaxNestedDepth bounds the descent. A script can hand another script to
// another interpreter (`bash -c "eval 'bash -c …'"`), and the recursion has to
// stop somewhere; four levels is deeper than any real command and shallow enough
// that a pathological line cannot spend the turn inside the classifier. Reaching
// the limit is not a reason to demote: the clauses found on the way DOWN are
// already judged, and a body beyond the limit is the body of a command that was
// itself judged.
const chatOpMaxNestedDepth = 4

// chatOpShellCommandFlag is the interpreter flag that says "the next argument is
// the script": `bash -c '<script>'`, `sh -ec '<script>'`, `zsh -lc '<script>'`.
// The flag can be combined with others, so the test is whether the option word
// CARRIES it, not whether it equals it.
const chatOpShellCommandFlag = "c"

// chatOpJudgedClause is ONE clause to classify, plus the index of the top-level
// clause it came from. The origin is what makes the run-directory carry work
// across the descent: a nested command runs in the directory its ENCLOSING
// clause's `cd` selected, not in the directory of the clause after it.
type chatOpJudgedClause struct {
	words  []string
	origin int
}

// chatOpJudgedClauses expands the top-level clauses into every command the shell
// will actually run: each clause itself, then — recursively, bounded by
// chatOpMaxNestedDepth — whatever commands that clause hands to an interpreter.
// The expansion is purely ADDITIVE, so the composition in chatOpClassifyIn (the
// most restrictive class wins) keeps its meaning.
func chatOpJudgedClauses(clauses [][]string) []chatOpJudgedClause {
	out := make([]chatOpJudgedClause, 0, len(clauses))
	for i, words := range clauses {
		out = append(out, chatOpJudgedClause{words: words, origin: i})
		for _, nested := range chatOpNestedClauses(words, 1) {
			out = append(out, chatOpJudgedClause{words: nested, origin: i})
		}
	}
	return out
}

// chatOpNestedClauses returns the commands ONE clause will run besides itself:
// the script an interpreter argument carries (recursively descended), and the
// commands a `$( … )` substitution inside it contributes — which the ONE
// tokenizer has ALREADY cut into separate clauses, so a substitution's body never
// appears here and needs no second reader.
func chatOpNestedClauses(words []string, depth int) [][]string {
	if depth > chatOpMaxNestedDepth {
		return nil
	}
	inner := chatOpScriptClauses(words)
	var out [][]string
	for _, clause := range inner {
		out = append(out, clause)
		out = append(out, chatOpNestedClauses(clause, depth+1)...)
	}
	return out
}

// chatOpScriptClauses reads the SCRIPT out of a clause whose head is a shell
// interpreter, and returns the commands it names — or nil when the clause is not
// an interpreter invocation, or hands it a FILE rather than a command line.
//
// THE DIRECTION IS THE ARGUMENT'S, NOT THE HEAD'S. `bash -c 'git status'` is a
// READ because `git status` is; `bash script.sh` names a file whose contents the
// classifier cannot read, so it contributes nothing (and the clause itself
// already matched the table, if it matched anything). Returning nil is therefore
// never a demotion: it is "nothing more to add".
func chatOpScriptClauses(words []string) [][]string {
	// THE HEAD IS FOUND BY THE ONE SHARED WALK, so the wrapper→operand rule
	// (`sudo -u nick sh -c …`) cannot be right in one reader and missing here
	// (task 01a19d8a).
	i, ok := commandHeadIdx(words, gitCommandPreambleWords, func(w string) bool {
		return chatShellInterpreterWords[w]
	})
	if !ok || i >= len(words) {
		return nil
	}
	head := words[i]
	if slash := strings.LastIndexByte(head, '/'); slash >= 0 {
		head = head[slash+1:]
	}
	if !chatShellInterpreterWords[head] {
		return nil
	}
	args := words[i+1:]
	if head == commandWordEval || head == xargsCommandWord {
		// `eval` joins its arguments into the command it runs, and `xargs`
		// appends what it read to its own — so each is a command line in its own
		// right once its own options are dropped.
		return chatOpLineClauses(strings.Join(chatOpDropInterpreterOptions(args), " "))
	}
	for j, arg := range args {
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			// A script FILE: nothing the classifier can read.
			return nil
		}
		if !strings.Contains(arg, chatOpShellCommandFlag) || j+1 >= len(args) {
			continue
		}
		return chatOpLineClauses(strings.Join(args[j+1:], " "))
	}
	return nil
}

// chatOpDropInterpreterOptions drops the option words from an interpreter's
// argument list, so the SCRIPT is what remains: `xargs -0 -r git push` and
// `eval -n git push` both keep `git push` and lose the flags.
func chatOpDropInterpreterOptions(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ChatOpClassWord is the table word that declares a class — exported so the prompt
// and the tests name the class by the spelling the fence uses rather than by an
// index.
func ChatOpClassWord(c ChatOpClass) string {
	switch c {
	case ChatOpClassRepoWrite:
		return chatpolicy.CategoryRepoWriteWord
	case ChatOpClassLong:
		return chatpolicy.CategoryLongWord
	default:
		return chatpolicy.CategoryOperatorWorkWord
	}
}

// ChatOpAdmit judges ONE chat bash call against the operator's ruling. command is
// the command line itself; operatorTurn is THIS turn's operator message, from
// which an authorisation for a long operation is read and from which nothing is
// stored.
//
// The rules, one per class:
//
//   - OPERATOR WORK runs. It is the operator's own work arriving on their own
//     console, and asking about it is the defect this criterion replaced.
//   - REPOSITORY WRITE is the console's to perform. The operator's 2026-10-24
//     ruling REVERSED the former hard no-go: a chat that cannot commit cannot
//     finish anything, and the measured case is that the task route is not always
//     available — during that night's incident the dispatched workers were the
//     thing breaking the machine, so "route it to a worker" was routing it to the
//     unreliable party. The console IS the operator's hand (Rule 37's own PA
//     duty), and writing the repository is part of that work.
//   - LONG OPERATION runs only when THIS turn's operator message authorised it,
//     or when the operator's standing posture is `auto` (they opted into
//     operations they did not bound); otherwise it is the question the console
//     must ask.
func ChatOpAdmit(command, operatorTurn string) ChatOpAdmission {
	return ChatOpAdmitIn(command, "", operatorTurn)
}

// ChatOpAdmitIn is ChatOpAdmit with the invocation's working directory supplied —
// the same directory ChatOpClassifyIn reads, so the tool dispatch can judge a
// call in the directory it will actually run in (a clone ground's delivery push
// is a fact about WHERE the push runs, chat_push_ground.go).
func ChatOpAdmitIn(command, workdir, operatorTurn string) ChatOpAdmission {
	adm := ChatOpAdmission{class: ChatOpClassifyIn(command, workdir), channel: ChatOpChannel()}
	if adm.class == ChatOpClassLong {
		if chatOpDeclaredBudgetSeconds(operatorTurn) > 0 {
			adm.authorizedByOperator = true
			return adm
		}
		if adm.channel == ChatOpChannelAuto {
			return adm
		}
		adm.reason = chatOpLongUndeclaredReason
		return adm
	}
	if adm.class == ChatOpClassRefMove {
		// A REF MOVE RUNS — the operator's 2026-11-05 ruling, restated after the
		// console had refused one too many times: "凡是有限制你的，你就去把它改掉，立刻
		// 马上把它改掉，限制的全部改掉."
		//
		// THIS REVERSES THE PREVIOUS LINE OF THIS BRANCH, which read "THE REF MOVE IS
		// THE OPERATOR'S DECISION EVERY TIME (PROJECT.md Rule 37)" and refused
		// unconditionally. That rule was correct about one thing and wrong about the
		// remedy: a ref move DOES decide what other work stays reachable, and the
		// answer the operator chose is not "the console asks" but "the console does
		// it and reports what it moved". The refusal made the console a step the
		// operator had to babysit, which is the defect they named.
		//
		// WHAT THIS DOES NOT REMOVE, because it is not a permission question at all:
		// the DISCIPLINE. A ref move still runs only where this run can see its
		// consequence and state it — name the exact ref, name what it moves from and
		// to, and say what the commits stepped over were. That is a practice the
		// prompt states (ChatOpRepoWriteRefusalSentence), not a gate.
		return adm
	}
	// OPERATOR WORK and REPOSITORY WRITE both run: nothing to decide at any
	// posture. The commit path keeps the discipline Rule 8 and Rule 30 state
	// (stage files ONE BY ONE, leave the tree clean) — that is a practice, not
	// a permission, and the prompt says so.
	return adm
}

// The reason strings a refusal prints. Each is a NAMED constant (Rule 1) because
// the refusal switches on it and a test asserts which rule decided.
const (
	chatOpLongUndeclaredReason = "the action itself is long and nobody authorised that time in this turn"
	// chatOpRefMoveReason is the destructive ref-move family: moving a published
	// ref is the operator's decision in THEIR turn (PROJECT.md Rule 37), and no
	// standing posture stands in for it.
	chatOpRefMoveReason = "this moves a ref — the operator's decision, never the console's"
)

// ChatOpRefusal renders the refusal for a chat bash call the CATEGORY criterion
// does not admit, or "" when the call may start. It names the class it read, the
// class that would have run, and the ONE next move that is a sentence (for a long
// operation) rather than a retry.
//
// REPOSITORY WRITE no longer reaches this function: it is admitted unconditionally
// (ChatOpAdmit), so the only class that can be refused here is LONG.
func ChatOpRefusal(command, operatorTurn string) string {
	return ChatOpRefusalIn(command, "", operatorTurn)
}

// ChatOpRefusalIn is ChatOpRefusal with the invocation's working directory
// supplied — the spelling the tool dispatch uses, so the refusal a chat reads is
// the answer for the directory its command will run in (ChatOpAdmitIn). An empty
// workdir is the process cwd, which is the one-argument form's reading.
func ChatOpRefusalIn(command, workdir, operatorTurn string) string {
	adm := ChatOpAdmitIn(command, workdir, operatorTurn)
	if adm.Allowed() {
		return ""
	}
	intro := "Refused by the CHAT OPS CATEGORY (" + adm.class.String() + " — " + adm.reason + "): this console " +
		"judges an operation by WHAT IT IS, not by how long it takes. " + ChatOpOperatorWorkSentence() + " "
	return intro + ChatOpLongRefusalSentence(adm.channel)
}

// ChatOpOperatorWorkSentence is the class that RUNS, as the prompt and every
// refusal state it (one rendering, two readers — Rule 13/14).
func ChatOpOperatorWorkSentence() string {
	return "OPERATOR WORK — ops, DevOps, the network, a script, a document, a query, a diagnosis, a bug row, " +
		"ledger/housekeeping edits and a short job — is YOURS to run in this turn with no permission question at " +
		"all, at every posture. " + "LONG OPERATIONS — the whole-tree suite, a full build, a package install, a " +
		"merge/worker/serve sweep (" + chatOpVocabularyRendered(ChatOpClassLong) + ") — must be ASKED " +
		"about first. "
}

// ChatOpRepoWriteRefusalSentence is now the ADMISSION's words: REPOSITORY WRITE
// runs (the operator's 2026-10-24 ruling reversed the hard no-go), and what
// survives is the DISCIPLINE the commit path keeps — staging one file at a time
// (Rule 8), a clean tree before handing over (Rule 30), and never a ref move
// (branch/reset/push) that writes over work this run does not own. It is a
// practice to state, not a permission to ask for.
func ChatOpRepoWriteRefusalSentence() string {
	return "A REPOSITORY WRITE — editing source, running the project's gate over your own change " +
		"(" + chatOpVocabularyRendered(ChatOpClassRepoWrite) + "), or staging/committing — is YOURS to " +
		"perform in this turn: there is no permission question for it. Keep the commit path's discipline — " +
		"`git add` ONE file at a time (never `-A` or `.`), `git status --porcelain` empty before you report " +
		"done, and no ref move (branch reset, force spellings, push) over work this run does not own."
}

// ChatOpLongRefusalSentence is the long class's half: the question to ask, and the
// two ways an operator opens the gate.
func ChatOpLongRefusalSentence(channel string) string {
	return "ASK the operator before starting it (`这个要跑 5 分钟，可以吗?`) — their own authorisation in a later " +
		"turn opens it, e.g. \"跑吧，最多 5 分钟\" (that is the declaration this console reads: a number with a " +
		"time unit in the operator's message). A standing `" + ChatOpAutoEnv + "=1` / settings.json \"" +
		ChatOpAutoSettingsKey + "\" = `" + ChatOpChannelAuto + "` also opens the undeclared case (this machine's " +
		"posture is `" + channel + "`); `" + ChatOpChannelAllAsk + "` is the default."
}

// chatOpTaskSubmitRoute is the ONE spelling of the routing sentence's command —
// `gopi task submit` — composed from the leaf constants that own the words (Rule
// 1/13/14), so the refusal and the prompt cannot render two different verbs.
func chatOpTaskSubmitRoute() string {
	return runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Submit
}

// chatOpDeclaredBudgetSeconds reads the budget THIS turn's operator message
// declared, in seconds (0 = none). It is read as the MINIMUM the operator named: a
// message that both offers and caps ("give it a minute, but do not exceed 30 s")
// authorises the cap. That reading is deliberately crude, and its limit is stated
// (Rule 15): a paraphrase outside the unit vocabulary is a false negative, it
// lands on ASK, and the refusal prints the exact spelling that opens the gate —
// the safe direction, and a wrong answer the operator corrects in one sentence.
func chatOpDeclaredBudgetSeconds(turn string) int {
	best := 0
	for i := 0; i < len(turn); i++ {
		if turn[i] < '0' || turn[i] > '9' {
			continue
		}
		j := i
		for j < len(turn) && turn[j] >= '0' && turn[j] <= '9' {
			j++
		}
		n, err := strconv.Atoi(turn[i:j])
		if err != nil || n <= 0 {
			i = j - 1
			continue
		}
		rest := strings.TrimLeft(turn[j:], " \t")
		secs, ok := chatOpUnitSeconds(rest)
		if !ok {
			i = j - 1
			continue
		}
		if total := n * secs; best == 0 || total < best {
			best = total
		}
		i = j - 1
	}
	return best
}

// chatOpUnitSeconds matches a unit word at the start of rest, longest spelling
// first, returning the seconds one of it is worth. An ASCII unit must END at a
// non-letter, so `5 monleys` is not five minutes; CJK units have no such
// boundary and are matched as they are written.
func chatOpUnitSeconds(rest string) (int, bool) {
	for _, u := range chatOpHourUnits {
		if chatOpUnitMatches(rest, u) {
			return chatOpHourSecs, true
		}
	}
	for _, u := range chatOpMinuteUnits {
		if chatOpUnitMatches(rest, u) {
			return chatOpMinuteSecs, true
		}
	}
	for _, u := range chatOpSecondUnits {
		if chatOpUnitMatches(rest, u) {
			return 1, true
		}
	}
	return 0, false
}

// chatOpUnitMatches reports whether rest begins with the unit word u and, for an
// ASCII unit, does not continue as a longer word.
func chatOpUnitMatches(rest, u string) bool {
	if !strings.HasPrefix(rest, u) {
		return false
	}
	if len(u) == 0 || u[0] > 0x7f {
		return true
	}
	tail := rest[len(u):]
	if tail == "" {
		return true
	}
	c := tail[0]
	return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
}
