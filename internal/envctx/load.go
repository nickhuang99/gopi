package envctx

import (
	"fmt"
	"os"
	"strings"
)

// load.go — the READ side: the summary+pointer block that rides the agent's
// context, and the behaviour when the description is absent.
//
// WHAT THE BLOCK CARRIES, AND WHY IT IS THIS SMALL. The summary (generated
// from the host's own measurements, typically a few hundred characters) plus
// the ABSOLUTE PATH of the full file. The full section list — nodes, roles,
// storage, services, conflicts — is deliberately NOT in the prompt: it is a
// document, and the agent reads documents with `read`. This is the
// pointer-table discipline (only pointers stay in context; full content is
// loaded on demand) applied to the prompt, and it is what keeps the injection
// inside the prompt's density budget.
//
// ABSENCE IS A RESULT, NEVER AN ERROR (the same contract LoadProjectContext
// keeps): a run on a host that never generated a description proceeds normally,
// with one stderr line saying the environment is unknown to this run and how to
// produce it. Nothing is injected, nothing blocks, the exit status is
// untouched.
//
// THE TWO-WAY STALENESS CHECK. The file records the hostname it was measured
// on. When Load runs on a different hostname, it says so on stderr and STILL
// injects — the file is the best description anyone has, and dropping it would
// trade a labelled approximation for silence. A file that was copied between
// machines is exactly when this matters.

// promptBlockTitle is the heading the block carries into the prompt. It is a
// VALUE (a reader greps for it in a session log) and lives here.
const promptBlockTitle = "## System environment (this host)"

// promptBlockPointer is the pointer sentence: it names the full file and the
// verb that regenerates it, so the agent never has to guess where the rest is.
//
// It names WHAT THE FILE ACTUALLY CARRIES (this host, its interfaces and
// gateway, every ssh alias, every recorded conflict) rather than the wider
// wish-list the file was once described with. A pointer that promises roles,
// storage and services sends the reader to a file that has none of them — the
// reader spends a round discovering the pointer lied (Rule 15: the failure is
// the silence, and an overclaim is a silence with a claim on top).
const promptBlockPointer = "Full description — this host, its interfaces and gateway, every ssh alias and " +
	"every recorded conflict — is in %s. Read that file when a question is about this machine's " +
	"network, peers, or reachable services; re-measure it with `gopi config env --write`."

// promptBlockCaveat is the one sentence that keeps the injection honest: the
// summary is a file's own text at a recorded moment, not a probe of the network
// happening now.
const promptBlockCaveat = "It is a description recorded at the timestamp it states — the node " +
	"addresses are text read from an ssh config, not a live probe; treat an address as " +
	"unverified until the host answers."

// Block is the rendered prompt injection: the summary lines, the pointer, and
// the caveat. It returns "" when there is no usable description.
type Block struct {
	// Summary is the delimited section's body, verbatim from the file.
	Summary string
	// Path is the file it came from.
	Path string
	// MeasuredOn is the hostname the file claims it was measured on ("" when
	// the file does not say).
	MeasuredOn string
	// CurrentHost is this run's hostname.
	CurrentHost string
}

// Stale reports whether the description was measured on a different host than
// the one this run is on. A file with no recorded hostname is NOT stale: it
// makes no claim to have been measured anywhere.
func (b Block) Stale() bool {
	return b.MeasuredOn != "" && b.CurrentHost != "" && b.MeasuredOn != b.CurrentHost
}

// Text is the full prompt block, ready to append to a system message. A zero
// Block (no summary, no path) renders as "" — an empty injection, never a
// pointer to nowhere.
func (b Block) Text() string {
	if strings.TrimSpace(b.Summary) == "" && strings.TrimSpace(b.Path) == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(promptBlockTitle + "\n")
	sb.WriteString(strings.TrimRight(b.Summary, "\n") + "\n")
	sb.WriteString(fmt.Sprintf(promptBlockPointer, b.Path) + "\n")
	sb.WriteString(promptBlockCaveat + "\n")
	if b.Stale() {
		// The mismatch is stated IN the prompt as well as on stderr: an agent
		// reasoning about "this host" must not read another machine's
		// addresses as its own.
		sb.WriteString(fmt.Sprintf("NOTE: this description was measured on host `%s`, while this run is on `%s` — treat the addresses as belonging to the other machine.\n",
			b.MeasuredOn, b.CurrentHost))
	}
	return sb.String()
}

// Load reads the environment description and returns the prompt block. The
// second result is false when nothing could be injected (absent, unreadable, or
// carrying no summary section) — the caller then injects nothing and no error
// is produced, because the description is OPTIONAL.
//
// path may be "" to resolve the standard location; tests pass their own.
func Load(path string) (Block, bool) {
	if strings.TrimSpace(path) == "" {
		resolved, err := Path()
		if err != nil {
			reportAbsent("", err.Error())
			return Block{}, false
		}
		path = resolved
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			reportAbsent(path, "")
			return Block{}, false
		}
		reportAbsent(path, err.Error())
		return Block{}, false
	}
	summary, host, ok := splitSummary(string(data))
	if !ok {
		// The file exists but does not carry the delimited summary: it was
		// written by something else, or hand-edited past the markers. Reported
		// by name — injecting a guessed block from arbitrary text would be the
		// silent-failure shape this whole feature is built to avoid.
		fmt.Fprintf(os.Stderr,
			"[gopi] warning: %s exists but carries no %s…%s summary section — continuing without the environment block\n",
			path, summaryBeginLine, summaryEndLine)
		return Block{}, false
	}
	if len(summary) > MaxSummaryChars {
		// Too big to be always-resident. Named, with the size and the remedy,
		// rather than truncated: a halved summary is a summary the agent
		// cannot know is halved.
		fmt.Fprintf(os.Stderr,
			"[gopi] warning: %s carries a %d-char summary (budget %d) — continuing without the environment block; trim the summary section or raise envctx.MaxSummaryChars\n",
			path, len(summary), MaxSummaryChars)
		return Block{}, false
	}
	current, _ := os.Hostname() //stylegate:ignore an unresolvable hostname only disables the staleness note; the block itself is still injected
	block := Block{Summary: summary, Path: path, MeasuredOn: host, CurrentHost: current}
	if block.Stale() {
		fmt.Fprintf(os.Stderr,
			"[gopi] warning: %s was measured on host %q but this run is on %q — injecting it with the mismatch stated\n",
			path, block.MeasuredOn, block.CurrentHost)
	} else {
		fmt.Fprintf(os.Stderr, "[gopi] loaded %s (%d bytes)\n", FileName, len(data))
	}
	return block, true
}

// reportAbsent is the ONE place the absence line's shape lives. An absent
// description and an unreadable one differ by the reason, and both end with the
// remedy, so the operator reading stderr learns what to do rather than only
// what is missing (Rule 15).
func reportAbsent(path, reason string) {
	where := path
	if where == "" {
		where = FileName
	}
	if reason != "" {
		fmt.Fprintf(os.Stderr,
			"[gopi] no environment description found at %s (%s) — this run does not know this machine's nodes, addresses or roles; generate one with `gopi config env --write`\n",
			where, reason)
		return
	}
	fmt.Fprintf(os.Stderr,
		"[gopi] no environment description found at %s — this run does not know this machine's nodes, addresses or roles; generate one with `gopi config env --write`\n",
		where)
}

// splitSummary extracts the delimited section and the measured-on hostname from
// the file's text. The hostname is recovered from the generated line, so the
// block can flag a description copied from another machine without a second
// metadata format to keep in sync.
func splitSummary(text string) (summary, host string, ok bool) {
	start := strings.Index(text, summaryBeginLine)
	if start < 0 {
		return "", "", false
	}
	rest := text[start+len(summaryBeginLine):]
	end := strings.Index(rest, summaryEndLine)
	if end < 0 {
		return "", "", false
	}
	body := strings.Trim(rest[:end], "\n")
	if strings.TrimSpace(body) == "" {
		return "", "", false
	}
	return body, hostFromText(text), true
}

// hostFromText reads the hostname out of the generated line ("... on host `X`
// by ..."). Absent or unparsable, it returns "" — the staleness check then
// simply does not fire.
func hostFromText(text string) string {
	const marker = " on host `"
	i := strings.Index(text, marker)
	if i < 0 {
		return ""
	}
	rest := text[i+len(marker):]
	j := strings.Index(rest, "`")
	if j <= 0 {
		return ""
	}
	return rest[:j]
}

// Combine folds the environment block into the project context string the
// agent's context already carries, so the block rides the EXISTING system
// message (Context.ProjectContext) on both the task and chat paths instead of
// adding a field, a message or a prompt assembly step (no new prompt
// mechanism). Returns projectContext unchanged when there is no block.
func Combine(projectContext string, block Block, present bool) string {
	text := block.Text()
	if !present || text == "" {
		return projectContext
	}
	if strings.TrimSpace(projectContext) == "" {
		return text
	}
	return strings.TrimRight(projectContext, "\n") + "\n\n" + text
}
