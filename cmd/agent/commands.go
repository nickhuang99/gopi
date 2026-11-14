package main

// commands.go — the shared slash-command table (R4 of
// devdoc/web-align-p1.md): CLI subcommands exposed inside BOTH interactive
// carriers (`gopi chat` REPL and the `gopi web` browser page) via the SAME
// registry, so a command learned in one carrier behaves identically in the
// other.
//
// Design (design-chat-web-alignment.md R4): slash commands are the
// HUMAN-triggered bypass — deterministic, zero token cost, output rendered
// into the conversation flow but NEVER sent to the model (no LLM turn is
// spent, nothing enters the model context or the server-owned history).
// The model keeps its own path to the same information via its tools and
// `bash gopi …` — the human/model entry points stay layered (the DSH
// session.ts precedent: command results are session events, not prompts).
//
// Every command runs the SHARED engine functions from internal/* directly
// (no subprocess, no binary dependency), reusing the same validation the
// CLI entry points do. Command help is a HARD standard (the 2026-08-28
// lesson: a subcommand without -h is incomplete).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/text2img"
	"github.com/nickhuang99/gopi/internal/tools"
)

// CommandContext carries the process-level environment a command may need.
// Engine functions resolve their DB paths from GOPI_* envs themselves; ctx
// exists for future per-carrier additions (e.g. an inline task-list id)
// without breaking the registry signature.
//
// REBASE NOTE (delivery round 2/3): this struct carries TWO INDEPENDENT SEAMS
// and the rebase is where they met. origin/master's b4c3b474 (R7) added the
// PRODUCT SINK — `Artifacts`, "what did this command produce"; 56cf319e added
// the SESSION FACTS — `Session`, "what session is the command running inside".
// They answer different questions, so BOTH fields stay: dropping either would
// delete a landed capability (Rule 15's mirror — a silent loss).
//
// Artifacts is the PRODUCT SINK: a command that renders something a carrier
// could actually USE (today only /text2img's PNG pages) records the file path
// here, so a carrier can pick the picture up without parsing the human summary
// line. A nil sink is legal and is what the terminal REPL passes: it means
// "this carrier has nowhere to put a product", and the command still returns
// its summary text unchanged. The registry signature (string, error) is NOT
// widened by this — a second return value would have made every one of the
// table's commands carry a field only one of them fills.
type CommandContext struct {
	// Artifacts is opt-in and nil by default (the terminal REPL passes nil).
	Artifacts *CommandArtifacts
	// Session is that addition, made real by the portability census (epic
	// 01a1b818-e02f): a command whose answer describes THIS session (/info,
	// /context) reads the facts the carrier already holds instead of
	// re-deriving them — see commands_session.go for the struct and for why six
	// of chat's eight REPL-native words deliberately stay carrier-local.
	// The zero value is legal: a command that needs a fact it lacks refuses by
	// name rather than printing a blank (Rule 15).
	Session CommandSession
}

// CommandArtifacts collects the file paths one command run produced, in render
// order. It is a plain accumulator (no locking): exactly one command runs at a
// time in a carrier, and the carrier reads the slice after Run returns.
type CommandArtifacts struct {
	// PNGs are the rendered pages, in the order the engine wrote them. Empty
	// means "this command produced no picture".
	PNGs []string
}

// addPNG records one rendered page. A nil receiver is a no-op, which is what
// makes the sink optional for every caller that does not have one.
func (a *CommandArtifacts) addPNG(path string) {
	if a == nil || path == "" {
		return
	}
	a.PNGs = append(a.PNGs, path)
}

// Command is one slash command in the shared registry. Run executes the
// command and returns the human-readable result text (rendered into the
// conversation flow in both carriers) plus an error for failure paths.
// Run NEVER talks to the LLM.
type Command struct {
	// Name is the canonical command word WITHOUT the leading slash
	// (e.g. "task"). Aliases may map additional words to the same Command.
	Name string
	// Usage is the single-command help line block shown by
	// `/help <name>` — the HARD help standard: every command documents
	// itself, every subcommand accepts a help request.
	Usage string
	// Run executes the command with the argv tail after the command word
	// (e.g. "/task list --status completed" → args = ["list", "--status",
	// "completed"]).
	Run func(args []string, ctx CommandContext) (string, error)
}

// aliases maps alternate spellings to canonical command names (resolved by
// lookupCommand; see commandAliases for the actual table). These are the
// interactive slash-command spelling surface (/t2i → /text2img) — USER word
// tables, deliberately independent of the tool wire protocol (a tool rename
// must not require editing user aliases; a "/text2img" stays the user's
// command even if the tool constant's value changes — the aliases point at the
// slash COMMAND name below, not at a tool constant).
var commandAliases = map[string]string{
	"t2i":   "text2img",
	"tasks": "task",
	"logs":  "log",
}

// commandHelpRequested reports whether an argv is a help request for a
// command ("-h" / "--help" as the first token, matching the CLI convention
// that any subcommand answers its own help).
func commandHelpRequested(args []string) bool {
	return len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help")
}

// lookupCommand resolves a command word (case-insensitive) through the
// registry + alias table.
func lookupCommand(word string) (*Command, bool) {
	word = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(word, "/")))
	if word == "" {
		return nil, false
	}
	if c, ok := slashCommands[word]; ok {
		return c, true
	}
	if canon, ok := commandAliases[word]; ok {
		if c, ok := slashCommands[canon]; ok {
			return c, true
		}
	}
	return nil, false
}

// slashUnknownCommandError is the ONE refusal both carriers share: it names the
// word that was refused and the FULL slash vocabulary its carrier accepts —
// spelled exactly as the carrier's /help table spells it (leading slash, one
// entry per accepted word) — plus the help pointer AND the literal-send channel.
// The word list is passed in because the accepted set is per-carrier (the REPL
// answers words the shared table cannot see); the RENDERING is not, so the two
// refusals cannot drift apart in shape (Rule 13/14).
//
// WHY IT NAMES THE ESCAPE (epic 01a104cb): a refusal is the one place a user
// learns that the line they wrote was read as a command — and until the
// classifier existed, a line the user meant as TEXT had no second spelling, so
// the refusal was a dead end. The escape it names is the same one
// classifyChatLine implements (chatSlashEscape), rendered on the refused word
// itself, so the guidance cannot describe a channel the classifier does not have.
func slashUnknownCommandError(word string, accepted []string) error {
	bare := strings.TrimPrefix(word, chatSlashTrigger)
	return fmt.Errorf("unknown command \"/%s\" — available: %s; try /help; to send it to the model as text, write %s%s",
		bare, slashWordList(accepted), chatSlashEscape, bare)
}

// execSlashCommand parses one raw slash input line (e.g. "/task list
// --status completed" or "/help task") and executes it against the shared
// registry. It returns the command result text. The leading "/" is optional
// in the argument (chat passes the trimmed line; the web handler passes the
// message verbatim) — anything NOT resolvable as a command is an error
// listing the available commands.
//
// This is the SINGLE shared dispatch used by both carriers (`gopi chat`'s
// REPL and `gopi web`'s handleChat), so the two carriers cannot drift: same
// table, same parse, same output.
func execSlashCommand(input string, ctx CommandContext) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty command")
	}
	fields := strings.Fields(input)
	word := strings.TrimPrefix(fields[0], "/")

	// /help [command]: the registry's own help. With a command word it
	// prints that command's usage (the "every command has -h" standard, in
	// both directions); bare /help prints the full table.
	if word == helpCommandName || word == helpShortWord {
		if len(fields) > 1 {
			name := strings.TrimPrefix(fields[1], "/")
			if c, ok := lookupCommand(name); ok {
				return c.Usage, nil
			}
			return "", fmt.Errorf("unknown command %q — try /help", name)
		}
		return slashHelpText(), nil
	}

	c, ok := lookupCommand(word)
	if !ok {
		return "", slashUnknownCommandError(word, sharedCarrierCommandNames())
	}
	args := fields[1:]
	if commandHelpRequested(args) {
		return c.Usage, nil
	}
	return c.Run(args, ctx)
}

// slashCommandNames returns the sorted command words (canonical names only —
// aliases stay a discoverability shortcut, not a help-table entry).
func slashCommandNames() []string {
	names := make([]string, 0, len(slashCommands))
	for name := range slashCommands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sharedCarrierCommandNames is the accepted vocabulary of the carriers whose
// slash surface IS the shared table (`gopi web` and `gopi term`): the
// registry's canonical words plus the conversation-reset ALIAS, which both of
// them honour through isResetCommand BEFORE the table lookup. A refusal that
// listed only the canonical names would deny a word those surfaces do accept.
func sharedCarrierCommandNames() []string {
	names := slashCommandNames()
	if webClearAliasName != webClearCommandName {
		names = append(names, webClearAliasName)
	}
	sort.Strings(names)
	return names
}

// chatReplCommandNames is the FULL vocabulary `gopi chat`'s REPL accepts, in
// ONE place: the shared table's canonical words, the REPL-native session words
// (chatSessionWords — the words its own switch answers, which the shared table
// cannot see), and the shared table's reset word, whose second spelling
// (webClearAliasName) the same switch accepts beside it. The REPL's refusal and
// its /help page therefore describe the SAME surface; a vocabulary that omits a
// word the switch honours is the defect this list exists to prevent.
func chatReplCommandNames() []string {
	names := slashCommandNames()
	seen := make(map[string]bool, len(names)+len(chatSessionWords)+1)
	for _, name := range names {
		seen[name] = true
	}
	for _, word := range chatSessionWords {
		if !seen[word] {
			seen[word] = true
			names = append(names, word)
		}
	}
	if !seen[webClearCommandName] {
		names = append(names, webClearCommandName)
	}
	sort.Strings(names)
	return names
}

// slashWordList renders command words as the slash vocabulary a carrier ACCEPTS:
// every entry carries the leading "/" (the trigger isSlashCommand and the
// carriers' switches test for), one spelling per word, sorted — so what a
// refusal names is literally what the surface's /help table prints.
func slashWordList(words []string) string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, "/"+w)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// slashHelpText renders the full command table for /help (both carriers).
func slashHelpText() string {
	var b strings.Builder
	b.WriteString("Slash commands (executed locally — never sent to the model):\n")
	for _, name := range slashCommandNames() {
		c := slashCommands[name]
		// First line of each usage block is the one-line summary.
		first := strings.SplitN(strings.TrimSpace(c.Usage), "\n", 2)[0]
		fmt.Fprintf(&b, "  /%-11s %s\n", name, first)
	}
	b.WriteString("  /help <cmd>  detailed usage for one command (each also accepts -h)\n")
	b.WriteString("Type any other line to chat with the model.")
	return b.String()
}

// ---------------------------------------------------------------------------
// Command implementations — each wraps the SHARED engine functions the
// corresponding CLI subcommand uses (no subprocess; the CLI's flag parsing
// is re-done as plain argv switches sized for interactive typing).
// ---------------------------------------------------------------------------

// parseSlashTaskArgs parses /task's argv (mirrors cli_tasks.go's flag set:
// --status filter, --task-id, --model) sized for interactive typing.
func parseSlashTaskArgs(args []string) (map[string]string, error) {
	opts := map[string]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := ""
		switch {
		case strings.HasPrefix(a, "--status="):
			val = strings.TrimPrefix(a, "--status=")
			opts["status"] = val
			continue
		case a == "--status":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--status needs a value")
			}
			i++
			opts["status"] = args[i]
			continue
		case strings.HasPrefix(a, "--task-id="):
			opts["task-id"] = strings.TrimPrefix(a, "--task-id=")
			continue
		case a == "--task-id":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--task-id needs a value")
			}
			i++
			opts["task-id"] = args[i]
			continue
		case strings.HasPrefix(a, "--model="):
			opts["model"] = strings.TrimPrefix(a, "--model=")
			continue
		case a == "--model":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--model needs a value")
			}
			i++
			opts["model"] = args[i]
			continue
		case strings.HasPrefix(a, "--reason="):
			opts["reason"] = strings.TrimPrefix(a, "--reason=")
			continue
		case a == "--reason":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--reason needs a value")
			}
			i++
			opts["reason"] = args[i]
			continue
		case strings.HasPrefix(a, "--query="):
			opts["query"] = strings.TrimPrefix(a, "--query=")
			continue
		case a == "--query":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--query needs a value")
			}
			i++
			opts["query"] = args[i]
			continue
		case a == slashTaskAllFlag:
			// Boolean, no value: the SAME `--all` the CLI's `task list` takes,
			// so the conversational view and the terminal view agree on
			// whether an archived (resolved) tombstone is rendered.
			opts["all"] = slashTaskFlagSet
			continue
		default:
			// Positional: the subcommand word, then the free-text summary /
			// show id / search query (all remaining words join the second
			// positional — interactive summaries are typed with spaces and
			// quotes are a convenience, not a parse rule).
			if _, ok := opts["sub"]; !ok {
				opts["sub"] = a
			} else {
				positional = append(positional, a)
			}
		}
	}
	opts["positional"] = strings.Join(positional, " ")
	opts["positional"] = strings.TrimSuffix(strings.TrimPrefix(opts["positional"], `"`), `"`)
	return opts, nil
}

// runSlashTask implements /task <list|show|update|search> by calling the
// tools.Task* engine functions directly (the same engines `gopi task` uses).
// The task ledger stays append-only and content-immutable: /task update only
// moves the status — no delete, no summary edit (M4.5; mirrors tasks.go).
func runSlashTask(args []string, ctx CommandContext) (string, error) {
	opts, err := parseSlashTaskArgs(args)
	if err != nil {
		return "", err
	}
	sub := opts["sub"]
	if sub == "" {
		return taskCommandUsage, nil
	}
	switch sub {
	case taskverbs.List:
		// The archived-row visibility predicate is tools.TaskListVisible — the
		// SAME call the CLI's `task list` makes (cmd/agent/cli_tasks.go,
		// runTaskList). This line used to call tools.TaskList bare, so a chat's
		// `/task list` rendered the rows the terminal view had just hidden: the
		// same archived column with two verdicts (measured 2026-09-29).
		return tools.TaskListVisible(opts["status"], opts["all"] == slashTaskFlagSet)
	case taskverbs.Show:
		id := opts["positional"]
		if id == "" {
			return "", fmt.Errorf("/task show needs a task id")
		}
		return tools.TaskShow(id)
	case taskverbs.New, taskverbs.Next:
		// `next` (the manual claim) is served by the queue runner now: the
		// replacement is `gopi worker`, which claims AND runs the next eligible
		// row (epic worker-run-entry — the retired `gopi task run` spelling it
		// used to name would send the reader to a removal message).
		return "", fmt.Errorf(`/task %s was removed (M4.5) — new: use "/task %s" in a gopi session or "gopi task %s" on the shell; next: "%s" claims and runs the next eligible row`,
			sub, taskverbs.Submit, taskverbs.Submit, taskProgName)
	case taskverbs.Update:
		id := opts["positional"]
		if id == "" {
			return "", fmt.Errorf("/task update needs a task id")
		}
		if r := opts["reason"]; r != "" {
			return tools.TaskUpdateReason(id, opts["status"], r, "")
		}
		return tools.TaskUpdate(id, opts["status"])
	case taskverbs.Search:
		q := opts["positional"]
		if q == "" {
			q = opts["query"]
		}
		if q == "" {
			return "", fmt.Errorf("/task search needs a query")
		}
		return tools.TaskSearch(q)
	case "-h", "--help", "help":
		return taskCommandUsage, nil
	default:
		// The spelled-out surface is the FAMILY's engine surface (the four
		// verbs this slash command implements), derived from the shared leaf
		// package so the message cannot drift from the switch above (Rule 13).
		return "", fmt.Errorf("unknown task subcommand %q — want %s",
			sub, strings.Join([]string{taskverbs.List, taskverbs.Show, taskverbs.Update, taskverbs.Search}, "|"))
	}
}

// slashTaskAllFlag is the boolean flag the conversational `/task list` shares
// with the CLI's `task list` (cmd/agent/cli_tasks.go, taskFlagMember "all"):
// WITHOUT it both views drop archived (resolved) tombstones, WITH it both keep
// them. One spelling, one predicate (Rule 13) — the defect it closes is a
// view that took the flag's VALUE from nowhere and therefore answered the
// opposite of the terminal view.
const slashTaskAllFlag = "--all"

// slashTaskFlagSet is the opts-map value a slash parser stores for a BOOLEAN
// flag that was present (the flag's presence, never a comparison of its
// spelling at the decision site, is what the engine reads).
const slashTaskFlagSet = "true"

// taskCommandUsage is /help task (mirrors `gopi task -h`'s engine surface).
const taskCommandUsage = `Usage: /task <subcommand> [args]
  /task list [--status STATUS] [--all]
                                      list ledger tasks (optionally filtered);
                                      archived rows (tags contain "resolved")
                                      are hidden — the view reports how many
                                      it hid, and --all includes them (same
                                      rule, same flag as ` + "`gopi task list`" + `)
  /task show <task_id>                show one task's full row
  /task update <task_id> [--status STATUS] [--reason WHY]
                                      move a task's status (content immutable;
                                      --reason records why; default if omitted)
  /task search <query>                FTS search over the ledger
Tasks are the cross-session audit ledger — append-only, content immutable
(no delete, no summary edit). Enqueue via gopi task submit. Engine:
internal/tools/tasks.go (same as ` + "`gopi task`" + `).`

// runSlashLog implements /log [subcommand] on the internal/gopilog reader —
// the same library `gopi session log` uses, limited to the read-only surface useful
// inside a conversation (no file-path args, no mutating flags).
//
// Subcommands: last [N] (recent sessions), summary <session-id>, read
// <session-id> (full formatted transcript), search <query> (grep across the
// latest sessions), sessions (the summary table). Raw-log / per-request
// dumps stay CLI-only (paths are unwieldy in chat).
func runSlashLog(args []string, ctx CommandContext) (string, error) {
	if len(args) == 0 {
		return logCommandUsage, nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "-h", "--help", "help":
		return logCommandUsage, nil
	case logSubLast:
		n := 5
		if len(rest) > 0 {
			if _, err := fmt.Sscanf(rest[0], "%d", &n); err != nil || n < 1 {
				return "", fmt.Errorf("/log last: %q is not a positive count", rest[0])
			}
		}
		return formatRecentSessions(n)
	case logSubSessions:
		return formatRecentSessions(20)
	case logSubSummary:
		if len(rest) == 0 {
			return "", fmt.Errorf("/log summary needs a session id")
		}
		s, err := loadSessionForSlash(rest[0])
		if err != nil {
			return "", err
		}
		return gopilog.FormatSummary(s), nil
	case logSubRead:
		if len(rest) == 0 {
			return "", fmt.Errorf("/log read needs a session id")
		}
		s, err := loadSessionForSlash(rest[0])
		if err != nil {
			return "", err
		}
		return gopilog.FormatSession(s), nil
	case logSubSearch:
		if len(rest) == 0 {
			return "", fmt.Errorf("/log search needs a query")
		}
		return slashLogSearch(strings.Join(rest, " "))
	default:
		return "", fmt.Errorf("unknown log subcommand %q — want %s|%s|%s|%s|%s", sub,
			logSubLast, logSubSessions, logSubSummary, logSubRead, logSubSearch)
	}
}

// formatRecentSessions renders the N most recent sessions as the same
// summary table `gopi session log --last N` prints (maxAge=0 — sessions may be
// arbitrarily old, the interactive caller picks N).
func formatRecentSessions(n int) (string, error) {
	summaries, err := gopilog.LoadAllSummaries(0)
	if err != nil {
		// A fresh GOPI_HOME has no sessions dir yet — that's "empty", not
		// an error the interactive user can act on.
		return "no sessions recorded yet", nil
	}
	if len(summaries) == 0 {
		return "no sessions recorded yet", nil
	}
	if n > len(summaries) {
		n = len(summaries)
	}
	return "\n" + gopilog.FormatSummaryTable(summaries[:n]), nil
}

// loadSessionForSlash loads one session's full record by id (or by a unique
// id prefix — interactive typing convenience, mirroring `gopi session log`'s
// short-ID support via gopilog.FindSession).
func loadSessionForSlash(id string) (*gopilog.Session, error) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		return nil, fmt.Errorf("resolve sessions dir: %w", err)
	}
	path, err := gopilog.FindSession(dir, id)
	if err != nil {
		return nil, fmt.Errorf("session lookup: %w", err)
	}
	if path == "" {
		return nil, fmt.Errorf("no session matches %q — get ids from /log last", id)
	}
	return gopilog.ParseFile(path)
}

// slashLogSearch greps the most recent sessions for a query (the /log
// convenience for "what did I do about X earlier") — bounded to the last 10
// sessions so the output stays conversational.
func slashLogSearch(query string) (string, error) {
	summaries, err := gopilog.LoadAllSummaries(0)
	if err != nil {
		return "", fmt.Errorf("load sessions: %w", err)
	}
	if len(summaries) > 10 {
		summaries = summaries[:10]
	}
	var b strings.Builder
	total := 0
	for _, s := range summaries {
		sess, err := gopilog.ParseFile(s.Path)
		if err != nil {
			continue
		}
		// Windowed: this leg greps the last 10 sessions, where an unwindowed
		// hit on a large argument would bury the answer ten times over.
		out := gopilog.FormatGrep(sess, query, nil, false)
		if out != "" && !strings.Contains(out, "no matches") {
			fmt.Fprintf(&b, "--- %s ---\n%s\n", s.SessionID, out)
			total++
		}
	}
	if total == 0 {
		return fmt.Sprintf("no matches for %q in the last %d sessions", query, len(summaries)), nil
	}
	return b.String(), nil
}

// logCommandUsage is /help log.
const logCommandUsage = `Usage: /log <subcommand> [args]
  /log last [N]            recent sessions' summary table (default 5)
  /log sessions            the last 20 sessions
  /log summary <id>        one session's summary (id from /log last)
  /log read <id>           one session's full formatted transcript
  /log search <query>      grep recent sessions (bounded to the last 10)
Engine: internal/gopilog (same reader as ` + "`gopi session log`" + `).`

// runSlashText2img implements /text2img <text> [--out P] [--size N] on the
// shared tools.Text2Img engine (the same engine `gopi tool text2img` and the
// text2img tool use). The engine takes a SOURCE FILE (like the CLI), so the
// text is written to a temp source file first.
//
// TWO outputs, one run: the human summary TEXT (what both carriers have always
// rendered into the conversation) AND the rendered page PATHS, recorded on the
// carrier's artifact sink. The paths are what makes the picture usable — a
// carrier can now attach it instead of printing where it went. The engine
// already reports them (text2img.Result.Outputs), so this is a handover, not a
// second render: the sink-less caller (the terminal REPL) keeps exactly the
// behaviour it had.
func runSlashText2img(args []string, ctx CommandContext) (string, error) {
	if commandHelpRequested(args) {
		return text2imgCommandUsage, nil
	}
	opts := map[string]string{}
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--out":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--out needs a path")
			}
			i++
			opts["out"] = args[i]
		case a == "--size":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--size needs a number")
			}
			i++
			opts["size"] = args[i]
		case a == "--cols":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--cols needs a number")
			}
			i++
			opts["cols"] = args[i]
		case strings.HasPrefix(a, "--out="), strings.HasPrefix(a, "--size="), strings.HasPrefix(a, "--cols="):
			kv := strings.SplitN(a, "=", 2)
			opts[strings.TrimPrefix(kv[0], "--")] = kv[1]
		default:
			words = append(words, a)
		}
	}
	text := strings.Join(words, " ")
	if text == "" {
		return text2imgCommandUsage, nil
	}
	// The engine renders a source FILE: stage the text in a temp file (the
	// interactive caller typed the text, not a path — either is accepted;
	// an existing path is passed through untouched).
	src := text
	staged := false
	if _, err := os.Stat(text); err != nil {
		tmp, err := scratch.CreateTemp(scratch.RuntimeClassText2Img, "gopi-slash-text2img-*.txt")
		if err != nil {
			return "", fmt.Errorf("stage text: %w", err)
		}
		if _, err := tmp.WriteString(text); err != nil {
			tmp.Close() //stylegate:ignore temp file closed before the rename — the rename is the checked step
			return "", fmt.Errorf("stage text: %w", err)
		}
		tmp.Close() //stylegate:ignore temp file closed before the rename — the rename is the checked step
		src = tmp.Name()
		staged = true
		defer os.Remove(src)
	}
	_ = staged

	out := opts["out"]
	if out == "" {
		// Default output mirrors the CLI: a temp PNG carrying the moment.
		out = filepath.Join(scratch.DumpDir(scratch.RuntimeClassText2Img),
			fmt.Sprintf(slashText2ImgNamePattern, time.Now().UnixNano()%1e9))
	}
	tOpts := text2img.Options{}
	if v, err := strconv.Atoi(opts["size"]); err == nil && v > 0 {
		tOpts.Size = v
	}
	if v, err := strconv.Atoi(opts["cols"]); err == nil && v > 0 {
		tOpts.Cols = v
	}
	summary, res, err := tools.Text2ImgDetail(src, out, tOpts)
	if err != nil {
		return "", err
	}
	for _, page := range res.Outputs {
		ctx.Artifacts.addPNG(page)
	}
	return summary, nil
}

// text2imgCommandUsage is /help text2img.
const text2imgCommandUsage = `Usage: /text2img <text> [--out PATH] [--size PX] [--cols N]
Render text to PNG with the pure-Go renderer (same engine as gopi tool text2img).
  /text2img "GOPI-P1-42"                  → auto path under the workspace scratch
                                            home (~/workspace/gopi-test-scratch/dumps)
  /text2img --out /tmp/x.png "hello"      → explicit path
Web carrier: the rendered page is attached to the conversation, so the NEXT
message carries it to the model as an image (Files API reference, no inline
bytes). A page that cannot be read back is reported as an error, never hidden
behind a path.`

// slashText2ImgNamePattern names the auto-generated /text2img output inside the
// scratch dumps tree — a named constant (Rule 1b) because the CLI usage text
// and the writer must agree on it.
const slashText2ImgNamePattern = "gopi-slash-%d.png"

// slashSearchRoot returns the code-search root: GOPI_CODE_SEARCH_ROOT env
// wins (kept for parity with the tool), else the working directory. The
// interactive carriers run inside the user's project, so cwd is the useful
// default — the same root the web server process serves.
//
// SURVIVOR (Rule 43): the /code-search slash command that used to own this was
// deleted on 2026-11-07 (the chat surface is an agent entry, not an IDE), but
// the web carrier's @ file menu still asks for the SAME root (detectFileMenu,
// cli_web.go), so the symbol stays and the menu keeps looking where the tool
// looks.
func slashSearchRoot() string {
	if r := os.Getenv(codeSearchRootEnvName); r != "" {
		return r
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// codeSearchRootEnvName is the env var slashSearchRoot honours — named here
// (Rule 1) because the value is a shared surface: `gopi tool code-search` and
// the @ menu resolve the same root through it.
const codeSearchRootEnvName = "GOPI_CODE_SEARCH_ROOT"

// splitQuotedArgs splits an argv into parts, keeping double-quoted spans as
// single parts.
//
// SURVIVOR (Rule 43): its first user was `/memory store "topic" "q" "a"`,
// deleted on 2026-11-07 with the /memory slash command. The splitter stays
// because another command needs the same job: the ssh-argv walk in cli_tasks.go
// receives a remote command line that may arrive as ONE login-shell-quoted
// element, and this is what turns it back into words — a general-purpose
// helper, not the deleted command's private business.
func splitQuotedArgs(args []string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, a := range args {
		if inQuote {
			if strings.HasSuffix(a, `"`) {
				cur.WriteString(" ")
				cur.WriteString(strings.TrimSuffix(a, `"`))
				parts = append(parts, cur.String())
				cur.Reset()
				inQuote = false
				continue
			}
			cur.WriteString(" ")
			cur.WriteString(a)
			continue
		}
		if strings.HasPrefix(a, `"`) {
			body := strings.TrimPrefix(a, `"`)
			if strings.HasSuffix(body, `"`) && len(body) > 0 {
				parts = append(parts, strings.TrimSuffix(body, `"`))
			} else {
				cur.WriteString(body)
				inQuote = true
			}
			continue
		}
		parts = append(parts, a)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// webClearCommandName is the conversation-reset command word in the shared
// table (B1). It is deliberately NOT an alias of the chat REPL's native
// /clear: an alias resolves through execSlashCommand, which would route around
// the carrier's own reset (web must clear under s.mu; chat mutates its
// chatSession) — so each carrier keeps its native handler, and both reach the
// same table word (chat's `case` accepts both spellings).
const webClearCommandName = "new"

// webClearAliasName is the chat REPL's long-standing spelling of the SAME
// reset (`/clear`, documented in chatUsage since chat mode landed). It is
// deliberately absent from commandAliases: that table rewrites a word to a
// shared-table canonical name, which would dispatch the carrier-agnostic ACK
// instead of the carrier's own reset. The word is instead accepted by the
// carriers' reset handlers (chat's `case`, the web reset predicate), so
// `/clear` and `/new` are one behaviour with two spellings — not two resets.
const webClearAliasName = "clear"

// The chat REPL's session-state command words (the "R4 four"): they mutate
// chatSession, which the shared table cannot see, so the REPL handles them
// natively — but their SPELLING is still a named constant, not an inline
// literal, because chatHelpText and the REPL switch must agree on it.
const (
	// logCommandName is the shared registry's own word for the session-log
	// reader (the entry below is keyed and Named by it), and the word the startup
	// screen names when it points a reader at the FULL session id
	// (chatBannerLogListWord's tip): one spelling, so the entry, the tip and
	// /help cannot describe the command with three different words.
	logCommandName   = "log"
	quitCommandName  = "quit"
	exitCommandName  = "exit"
	toolsCommandName = "tools"
	helpCommandName  = "help"
	// helpShortWord is the second spelling of the help word ("/?"), accepted
	// by both the REPL switch and execSlashCommand's `word == "?"` branch.
	helpShortWord = "?"
)

// The /log SUBCOMMAND words — the grammar runSlashLog's switch answers
// (last|sessions|summary|read|search). They are named for the same reason
// logCommandName is: the switch, its refusal sentence and the startup screen's
// tip must read ONE spelling each, so a rename can never leave a carrier
// advertising a subcommand the handler refuses.
//
// These are REPL VERBS, not directory names. The profile's sessions ROOT is
// config.SessionsDirName; logSubSessions is the /log subcommand answered by
// internal/gopilog's reader, and the exhaustive Rule-1b guard that keeps the
// ROOT single-sourced (TestSessionsDirNameNoHardcodedLiterals, internal/config)
// exempts this ONE definition line by name, with the subcommand reading written
// down beside the guard's other exemptions.
const (
	logSubLast     = "last"
	logSubSessions = "sessions"
	logSubSummary  = "summary"
	logSubRead     = "read"
	logSubSearch   = "search"
)

// chatHelpText renders the chat REPL's help for one raw input line: the REPL
// banner + the shared command table for a bare "/help", and the requested
// command's own usage block for "/help <cmd>".
//
// The REPL intercepts /help before the shared dispatch (it must, to answer for
// its three NATIVE words, which are not table entries), so this is the REPL's
// copy of execSlashCommand's help contract — one behaviour per word, and the
// native words are answered here instead of "unknown command".
//
// Separator — "\n" or a space — is accepted, as is a leading slash on the
// argument ("/help /task"), matching execSlashCommand's tolerance.
func chatHelpText(raw string) (string, error) {
	// The leading dash/slash of the help word itself is stripped, so "/help",
	// "-help" and "?" all read the same; whatever follows is the target.
	fields := strings.Fields(strings.TrimLeft(strings.TrimSpace(raw), "-/"))
	rest := fields[1:]
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
		// Bare /help (or /help -h): the REPL's own banner plus the table.
		return chatUsage + slashHelpText(), nil
	}
	word := strings.ToLower(strings.TrimPrefix(rest[0], "/"))
	if usage, ok := chatNativeUsage(word); ok {
		return usage, nil
	}
	if c, ok := lookupCommand(word); ok {
		return c.Usage, nil
	}
	return "", fmt.Errorf("unknown command %q — try /help", word)
}

// chatNativeUsage returns the usage block of a REPL-native session command
// (the words chat intercepts before the shared table), so /help <native> is
// answered like every other command rather than reported as unknown.
func chatNativeUsage(word string) (string, bool) {
	switch word {
	case quitCommandName, exitCommandName:
		return quitCommandUsage, true
	case webClearAliasName, webClearCommandName:
		return webClearCommandUsage, true
	case toolsCommandName:
		return toolsCommandUsage, true
	case chatQueueCommandName:
		// The QUEUE's usage block lives in chat_queue.go, beside the vocabulary
		// and the bound it documents — the word, the refusal sentence and the help
		// are one reading of ONE const block, so a rename cannot leave /help
		// documenting a spelling the switch no longer answers.
		return chatQueueUsage, true
	case chatInfoCommandName:
		// /INFO's block lives in chat_info.go for the same reason, one step
		// further: its subject is the RUNNING session (the logger's UUID, the
		// ground, the tools flag), which no shared table can see — so the word,
		// its renderer and its usage text are one file's reading of one session.
		return chatInfoUsage, true
	case chatScreenCommandName:
		// /SCREEN's block lives in chat_screen.go: the bounds it documents ARE the
		// constants the capture enforces, so /help screen cannot promise a budget
		// the command does not keep.
		return chatScreenUsage, true
	case chatConfigCommandName:
		// /CONFIG's block lives in chat_config.go — one definition, shared by
		// /help, the table entry and the REPL switch.
		return chatConfigUsage, true
	case chatContextCommandName:
		// /CONTEXT's block lives in chat_context.go: the artifact's NAME FORMAT it
		// prints IS the constant the export writes with (ctxdump.FileNamePattern),
		// so /help context cannot advertise a destination the code does not produce.
		return chatContextUsage, true
	case chatVoiceCommandName:
		// /VOICE's block lives in chat_voice.go: the word, the window length the
		// recorder enforces and the refusal sentences are one const block's
		// reading, and /help voice must document the SAME window the loop keeps.
		return voiceChatUsage, true
	}
	return "", false
}

// quitCommandUsage is /help quit (and /help exit — one behaviour, two
// spellings, exactly like /clear and /new).
const quitCommandUsage = `Usage: /quit (also /exit)
End the interactive session and exit (Ctrl+D on an empty prompt does the same).
/quit and /exit are ONE command: two spellings, no difference in behaviour.
The session's CHATS row is stamped on the way out, exactly as a normal exit.`

// toolsCommandUsage is /help tools.
const toolsCommandUsage = `Usage: /tools
Flip the session between tool-enabled chat (ops mode — the model may issue
real tool calls) and plain conversational Q&A. Equivalent to starting with
gopi chat --tools for the rest of the session.`

// webClearCommandUsage is the shared entry's self-documentation (the HARD help
// standard: every command answers -h).
const webClearCommandUsage = `Usage: /new
Clear the conversation and start fresh: the transcript the next turn seeds
from is dropped and the display is wiped. "/clear" is the same reset (the chat
REPL's spelling). Local and immediate — never a model turn.`

// webClearCommand is the shared-table affordance for the B1 reset: it is what
// makes `/new` appear in /help and in the completion surface of BOTH carriers
// (one table, two carriers — R4). Its Run is a carrier-agnostic ACK: the
// reset itself is state the table cannot see (web: s.history under s.mu;
// chat: chatSession.history), so each carrier handles the word before the
// table dispatch and then renders this command's output — the same shape as
// the REPL-native commands that already live outside the table, with the
// discoverability of a table entry.
func webClearCommand(_ []string, _ CommandContext) (string, error) {
	return webResetBodyMessage, nil
}

// slashCommands is THE shared registry both carriers consult (R4's core
// deliverable). Order here is irrelevant — /help prints sorted.
var slashCommands = map[string]*Command{
	// B1: the conversation-reset word. Its canonical name is the constant
	// (never a bare literal — the page JS and the help contract share it).
	webClearCommandName: {
		Name:  webClearCommandName,
		Usage: webClearCommandUsage,
		Run:   webClearCommand,
	},
	"task": {
		Name:  "task",
		Usage: taskCommandUsage,
		Run:   runSlashTask,
	},
	logCommandName: {
		Name:  logCommandName,
		Usage: logCommandUsage,
		Run:   runSlashLog,
	},
	"text2img": {
		Name:  "text2img",
		Usage: text2imgCommandUsage,
		Run:   runSlashText2img,
	},
	// THE SESSION-DESCRIPTIVE PAIR (epic 01a1b818-e02f). Both were REPL-native
	// words that read facts a web server also holds — its one persistent agent's
	// session logger, its raw HTTP log, its own ground — so they belong in the
	// table where BOTH carriers dispatch them to ONE renderer. What stays
	// carrier-local is the console seam (chat answers /info through
	// WriteNoticeRuns so the labels draw dim; see commands_session.go) and the
	// FACTS, which arrive in CommandContext.Session.
	chatInfoCommandName: {
		Name:  chatInfoCommandName,
		Usage: chatInfoUsage,
		Run:   slashInfoCommand,
	},
	chatContextCommandName: {
		Name:  chatContextCommandName,
		Usage: chatContextUsage,
		Run:   slashContextCommand,
	},
	chatConfigCommandName: {
		// /CONFIG is the shared table's word for the runtime snapshot
		// (runtime_config_snapshot.go). Its block lives in chat_config.go, whose
		// four channel names ARE the constants the renderer compares against, so
		// /help config cannot promise a report shape the code does not produce.
		Name:  chatConfigCommandName,
		Usage: chatConfigUsage,
		Run:   slashConfigCommand,
	},
	// NO `/code-search` AND NO `/memory` ENTRY, BY DECISION (operator,
	// 2026-11-07): gopi's chat surface is an AGENT ENTRY, not an IDE or a
	// copilot. Printing a tool's result into the transcript for a human to read
	// is the model's job, done through the tools themselves (`code_search`,
	// `memory` — both still defined in internal/agent/loop.go, untouched); a
	// slash command that bypasses the model made the console a second, worse
	// front end for the same two engines. Typing either word now takes the
	// ordinary unknown-command path, and their names are gone from /help.
}

// ---------------------------------------------------------------------------
// THE INPUT CLASSIFIER (epic 01a104cb): one line in — command, or text?
// ---------------------------------------------------------------------------
//
// WHY A CLASSIFIER AND NOT A PREFIX TEST. Until this, EVERY carrier asked
// `strings.HasPrefix(trimmed, "/")` and nothing else, so any line that happened
// to OPEN with a slash was a command: pasting a path (`/tmp/CN-result.gif`) or a
// pattern (`/^gopi/`) was refused with "unknown command" and never reached the
// model — there was no channel to send it either. The defect is not the trigger,
// it is that the SHAPE of the line was never examined, and that a line which is
// NOT a command had nowhere to go.
//
// THE CRITERION, decided here and stated so it can be argued with (the epic
// asked for the criterion and its reason, not only the behaviour):
//
//  1. LITERAL-SEND. A line whose first token is "//" + a single word
//     ("//quit") is sent to the model as literal text with ONE slash removed
//     ("/quit"). This exists because a command-SHAPED line has no other way to
//     reach the model: it is the escape channel, and it is the ONLY string the
//     classifier ever rewrites. Its own spelling is deliberately not "//"
//     followed by anything with a slash in it, so a comment line or a
//     double-slash path ("//tmp/x") stays byte-identical text (see 3).
//  2. COMMAND. The first token is "/" + a word that contains NO second slash
//     (a path cannot be one word) AND that word is either (a) accepted by the
//     carrier's vocabulary, or (b) spelled in the vocabulary's own alphabet
//     (letters, digits, "-", "_") — i.e. it LOOKS like a command word the
//     carrier does not have. (b) is what keeps "unknown command" working: a
//     refused command must still be refused, or a typo would silently become a
//     question for the model. It is also why "/^gopi/" is text: "^" is not in
//     the alphabet, so the token is not command-shaped at all.
//  3. TEXT — EVERYTHING ELSE, verbatim, no rewrite: a line with a SECOND slash
//     in its first token (an absolute path, a path with an extension, a regex
//     with a delimiter), a lone "/" or a lone "//", "/" followed by blank
//     space, and any ordinary sentence. A path is the commonest thing a human
//     pastes, and it is text by its own shape — the user does not escape it.
//
// The reason the SHAPE test is second-slash based rather than "look it up in the
// vocabulary": a user pasting a path must not be refused for the privilege of
// discovering the escape, and a single slash is what a human types to mean "this
// is a command" — so the ambiguity is resolved by what the token IS, never by
// what the model would prefer. `/quit` (a word the carrier has) stays a command;
// `/etc` (a path of one segment, a word the carrier lacks) is a REFUSAL, and
// the refusal now teaches the escape — the honest limit of the criterion,
// documented in devdoc/chat-slash-literal-send.md.
const (
	// chatSlashTrigger opens a command line; chatSlashEscape opens the
	// literal-send channel (Rule 1b/13: one definition, referenced by every
	// carrier — never typed as a literal at a call site).
	chatSlashTrigger = "/"
	chatSlashEscape  = "//"
)

// chatLineKind is the classifier's verdict for one input line.
type chatLineKind int

const (
	// chatLineText is the model's input.
	chatLineText chatLineKind = iota
	// chatLineCommand resolves to the carrier's command dispatcher.
	chatLineCommand
	// chatLineUnknown is command-shaped and names a word the carrier does not
	// accept: it is REFUSED (with the escape channel named), never sent.
	chatLineUnknown
	// chatLineEscape is text the caller asked for literally; in.text is the
	// message with one leading slash removed.
	chatLineEscape
)

// chatInputLine is one classified line: its kind, the command WORD it names
// (kind command/unknown/escape) and the message it carries (kind text/escape).
type chatInputLine struct {
	kind chatLineKind
	word string
	text string
}

// classifyChatLine decides whether ONE input line is a command or the model's
// text, and returns what the caller must do with it (the Criterion above).
// `accepts` is the carrier's own vocabulary — per-carrier, because a word one
// surface answers is another's unknown command (`/quit` is the REPL's, the
// browser has no way to end a server-side session) — and nil means "the
// vocabulary is not consulted", which classifies by shape alone.
func classifyChatLine(line string, accepts func(word string) bool) chatInputLine {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return chatInputLine{kind: chatLineText, text: line}
	}
	head := strings.Fields(trimmed)[0]

	// 1) THE LITERAL-SEND CHANNEL: "//word" → the text "/word".
	if word, ok := strings.CutPrefix(head, chatSlashEscape); ok &&
		word != "" && !strings.Contains(word, chatSlashTrigger) {
		literal, _ := strings.CutPrefix(trimmed, chatSlashTrigger)
		return chatInputLine{kind: chatLineEscape, word: strings.ToLower(word), text: literal}
	}

	// 2) COMMAND-SHAPED: "/" + one word, accepted by the carrier or spelled in
	// the vocabulary's alphabet.
	if word, ok := strings.CutPrefix(head, chatSlashTrigger); ok && word != "" &&
		!strings.Contains(word, chatSlashTrigger) {
		accepted := accepts != nil && accepts(word)
		if accepted || chatCommandWordShape(word) {
			kind := chatLineUnknown
			if accepted {
				kind = chatLineCommand
			}
			return chatInputLine{kind: kind, word: strings.ToLower(word), text: trimmed}
		}
	}

	// 3) TEXT, verbatim: a path, a pattern, a lone slash, a sentence.
	return chatInputLine{kind: chatLineText, text: line}
}

// chatCommandWordShape reports whether a word is spelled in the command
// vocabulary's own alphabet (letters, digits, "-", "_", starting alphanumeric).
// It is what makes "/nonexistent" a REFUSED command rather than a question for
// the model, and what keeps "/^gopi/" out of the command class entirely.
func chatCommandWordShape(word string) bool {
	for i, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '_'):
		default:
			return false
		}
	}
	return word != ""
}

// sharedSlashWordKnown reports whether the SHARED command table accepts a word:
// its canonical names, its aliases (lookupCommand resolves both), and the two
// words execSlashCommand answers OUTSIDE the registry — "help" and its "?"
// spelling (the registry's own help verb), plus the reset alias the reset path
// accepts. It is the vocabulary of `gopi web` and `gopi term` — the carriers
// whose slash surface IS that table.
func sharedSlashWordKnown(word string) bool {
	w := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(word, chatSlashTrigger)))
	if _, ok := lookupCommand(w); ok {
		return true
	}
	return w == helpCommandName || w == helpShortWord || w == webClearAliasName
}

// chatSessionWordKnown reports whether a word is one of the chat REPL's OWN
// session words — the ones its switch answers and the shared table cannot see
// (chatSessionWords is the ONE list; the completion menu, the refusal and /help
// are all built from it).
func chatSessionWordKnown(word string) bool {
	w := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(word, chatSlashTrigger)))
	for _, name := range chatSessionWords {
		if name == w {
			return true
		}
	}
	return false
}

// chatReplWordKnown reports whether the chat REPL accepts a word: the shared
// table, or one of the REPL-native session words its own switch answers and the
// table cannot see (chatSessionWords — ONE list, the same one the completion
// menu and /help are built from).
func chatReplWordKnown(word string) bool {
	return sharedSlashWordKnown(word) || chatSessionWordKnown(word)
}

// isSlashCommand reports whether a line is command-SHAPED — the first token is
// "/" + one word with no second slash (the SHAPE limb of the criterion above).
// It is the escaped-literal and path questions answered for callers that only
// need the shape (a scripted transcript's split); a carrier deciding what to DO
// with the line asks classifyChatLine for the verdict.
func isSlashCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, chatSlashTrigger) {
		return false
	}
	word, ok := strings.CutPrefix(strings.Fields(trimmed)[0], chatSlashTrigger)
	return ok && word != "" && !strings.Contains(word, chatSlashTrigger)
}
