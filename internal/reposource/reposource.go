// Package reposource is THE ONE answer to "which repository does this run
// work in?" — three sources, in a fixed order, and one refusal that names all
// three.
//
// THE ORDER (most specific first, operator ruling 2026-10-23):
//
//  1. the row's own deliverable.repo   — the row already says where it works
//  2. the current working directory    — the process is standing in a repo
//  3. settings dev_repo                — the host's OPTIONAL fallback
//  4. none of them                     — ONE error, listing all three
//
// WHY dev_repo IS LAST AND WHY IT IS OPTIONAL. It exists because there used to
// be two parallel workflow channels and a host had to CHOOSE one; with a single
// channel left, the choice — and therefore the requirement — is gone. It
// degrades to a fallback value. A host that never declares it is not
// misconfigured: a run started inside any git repository works, which is the
// whole point of the channel's retirement. The previous revision printed a
// DEPRECATION line onto every worker start of such a host; the operator struck
// that down on 2026-10-23, and THIS package is the replacement: a cwd that is a
// git repository is a NORMAL path and prints nothing at all.
//
// THE ONLY THING THAT SPEAKS IS THE REFUSAL, and it speaks exactly once, after
// source 3: every source's own measured verdict is listed, so a reader learns
// whether to `cd` or to declare dev_repo without a second experiment (Rule 15 —
// no silence, no vagueness).
//
// NO NEW KNOB, NO NEW VERB (Rule 35/Rule 3): the package reads what already
// exists and is called from the surfaces that already run.
package reposource

// reposource.go — the resolver. Everything a caller needs is Input + Resolve;
// the message shapes live here so the refusal and its test render ONE text
// (Rule 1b).

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// SourceRowRepo, SourceCWD and SourceDevRepo are the three source NAMES and
	// the order they are tried in — the labels the refusal prints, so a reader
	// can tell which step declined (Rule 1b: one spelling, used by the
	// resolution, the refusal and the tests).
	SourceRowRepo = "row deliverable.repo"
	SourceCWD     = "current working directory"
	SourceDevRepo = "settings dev_repo"

	// probeTimeout bounds ONE git probe. A probe that hangs is a probe that is
	// not answering; the run must not hang with it.
	probeTimeout = 10 * time.Second

	// gitProg / gitTopLevelFlag spell the ONE git question this package asks:
	// "where is the top level of the work tree this directory is inside?".
	// `rev-parse --show-toplevel` fails outside a work tree, so its exit status
	// is the answer, not its output.
	gitProg          = "git"
	gitDirFlag       = "-C"
	gitTopLevelVerb  = "rev-parse"
	gitTopLevelFlag  = "--show-toplevel"
	gitProbeFailNote = "git rev-parse --show-toplevel"
)

// The refusal's text, spelled once. The header names the state; each numbered
// line is a source's OWN verdict (filled from the probe, never re-described);
// the tail tells the reader the three concrete fixes. A refusal without a next
// step is a complaint.
const (
	// RefusalHeader opens the refusal. It says "every source declined" because
	// that is the ONLY condition under which anything is printed.
	RefusalHeader = "no run repository could be resolved — all three sources declined:"
	// RefusalRemedyFmt is the closing line: one fix per source, in the same
	// order, so the reader can pick the one that fits (cd, name the row's repo,
	// declare dev_repo).
	RefusalRemedyFmt = "fix ONE of them: cd into the repository the run should work in (%s), " +
		"name the row's own repo with `gopi task update <id> --repo <path>` (%s), " +
		"or declare %s in settings.json / `%s=host:/path` (%s)"
	// DeclinedNoRowRepo is the verdict of source 1 when the row names nothing.
	DeclinedNoRowRepo = "the row names no repo"
	// DeclinedNoDevRepo is the verdict of source 3 when the host declares
	// nothing — an ABSENT optional knob, which is why it is worded as a fact
	// rather than as a fault.
	DeclinedNoDevRepo = "not configured (optional — a run started inside a git repository does not need it)"
)

// Probe is ONE source's result: what it declared, and what the measurement
// said. It is filled for EVERY source that was tried, which is what lets the
// refusal list all three instead of only the last one that declined.
type Probe struct {
	// Source is one of the three Source* labels.
	Source string
	// Declared is the value this source offered, "" when it offered nothing.
	Declared string
	// Verdict is the one-line measured answer: "resolved: <dir>" or the reason
	// it declined. It is the text the refusal prints verbatim.
	Verdict string
	// Dir is the directory this source resolved to ("" when it declined).
	Dir string
}

// Resolution is the answer: the directory the run works in, and WHICH source
// decided it (Rule 15's positive half — a decision is never anonymous).
type Resolution struct {
	Dir    string
	Source string
	// Probes holds every source tried before the winner, in order, plus the
	// winner itself. Empty sources are still recorded (they are the ones a
	// reader most needs named when the OTHER two failed).
	Probes []Probe
}

// Input is everything the resolution may consult — passed IN rather than read
// from the process, so the decision is testable and has exactly one reader for
// each fact (the caller owns the settings file and the ledger, this package
// owns the order).
type Input struct {
	// RowRepo is the row's deliverable.repo ("" when the row names none or no
	// row is involved).
	RowRepo string
	// CWD is the directory the run was started in.
	CWD string
	// DevRepo is the host's declared dev_repo value, verbatim ("" when absent).
	DevRepo string
	// RenderDevRepo turns a declared dev_repo into a LOCAL checkout path —
	// `host:/path` and `file://` spellings included. The clone channel already
	// owns that reading (devworkspace.SourceRepoDisplayPath); this package
	// takes it as a function so it can be tested without a settings file and so
	// there is no second spelling of it here (Rule 14).
	RenderDevRepo func(string) (string, error)
}

// Refusal is the ONE error a failed resolution returns. It carries the probes
// so a caller can also render them as data; Error() is the operator-facing text.
type Refusal struct {
	Probes []Probe
}

// Error renders the refusal: the header, one numbered line per source carrying
// THAT source's own measured verdict, and the three remedies. It never says
// "deprecated" and it never names a single source as the culprit — the whole
// point is that a reader sees which of the three to fix.
func (r *Refusal) Error() string {
	var b strings.Builder
	b.WriteString(RefusalHeader)
	for i, p := range r.Probes {
		fmt.Fprintf(&b, "\n  %d. %s: %s", i+1, p.Source, p.Verdict)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, RefusalRemedyFmt, SourceCWD, SourceRowRepo, "dev_repo", "GOPI_DEV_REPO", SourceDevRepo)
	return b.String()
}

// Resolve answers which repository this run works in, trying the three sources
// in order and returning the FIRST that resolves. An error is a *Refusal and is
// returned ONLY after all three declined.
//
// It PRINTS NOTHING and has no side effect: a source that resolves early is a
// normal path and the absence of output is the deliverable (the deprecation
// line this replaces printed on every worker start of a host whose cwd WAS a
// git repository, which is the operator's ruling of 2026-10-23 reversed).
func Resolve(in Input) (Resolution, error) {
	var probes []Probe
	// 1. the row's own deliverable.repo — the most specific statement there is.
	p := probeRowRepo(in.RowRepo)
	probes = append(probes, p)
	if p.Dir != "" {
		return Resolution{Dir: p.Dir, Source: SourceRowRepo, Probes: probes}, nil
	}
	// 2. the current working directory — a process standing in a repository is
	// the ordinary shape, and the one the retirement exists to make legal.
	p = probeCWD(in.CWD)
	probes = append(probes, p)
	if p.Dir != "" {
		return Resolution{Dir: p.Dir, Source: SourceCWD, Probes: probes}, nil
	}
	// 3. the host's optional fallback.
	p = probeDevRepo(in)
	probes = append(probes, p)
	if p.Dir != "" {
		return Resolution{Dir: p.Dir, Source: SourceDevRepo, Probes: probes}, nil
	}
	return Resolution{Probes: probes}, &Refusal{Probes: probes}
}

// probeRowRepo measures source 1.
func probeRowRepo(rowRepo string) Probe {
	rowRepo = strings.TrimSpace(rowRepo)
	if rowRepo == "" {
		return Probe{Source: SourceRowRepo, Verdict: DeclinedNoRowRepo}
	}
	dir, why := topLevel(rowRepo)
	if dir == "" {
		return Probe{Source: SourceRowRepo, Declared: rowRepo,
			Verdict: fmt.Sprintf("names %q, which is not a git work tree (%s)", rowRepo, why)}
	}
	return Probe{Source: SourceRowRepo, Declared: rowRepo,
		Verdict: "resolved: " + dir, Dir: dir}
}

// probeCWD measures source 2.
func probeCWD(cwd string) Probe {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return Probe{Source: SourceCWD, Verdict: "the process has no readable working directory"}
	}
	dir, why := topLevel(cwd)
	if dir == "" {
		return Probe{Source: SourceCWD, Declared: cwd,
			Verdict: fmt.Sprintf("%s is not inside a git work tree (%s)", cwd, why)}
	}
	return Probe{Source: SourceCWD, Declared: cwd, Verdict: "resolved: " + dir, Dir: dir}
}

// probeDevRepo measures source 3: the declared value rendered to a local path,
// then probed like any other directory.
func probeDevRepo(in Input) Probe {
	declared := strings.TrimSpace(in.DevRepo)
	if declared == "" {
		return Probe{Source: SourceDevRepo, Verdict: DeclinedNoDevRepo}
	}
	render := in.RenderDevRepo
	if render == nil {
		render = func(v string) (string, error) { return v, nil }
	}
	local, err := render(declared)
	if err != nil {
		return Probe{Source: SourceDevRepo, Declared: declared,
			Verdict: fmt.Sprintf("declares %q, which names no local checkout path (%v)", declared, err)}
	}
	dir, why := topLevel(local)
	if dir == "" {
		return Probe{Source: SourceDevRepo, Declared: declared,
			Verdict: fmt.Sprintf("declares %q → %q, which is not a git work tree (%s)", declared, local, why)}
	}
	return Probe{Source: SourceDevRepo, Declared: declared,
		Verdict: fmt.Sprintf("declares %q → %q, resolved: %s", declared, local, dir), Dir: dir}
}

// topLevel asks git for the top level of the work tree dir is inside, and
// returns the reason as a one-line verdict when it is not one. It never returns
// a directory it did not measure: an unresolvable path is an empty answer plus
// a sentence, which is what makes the refusal's per-source lines evidence
// rather than guesses.
func topLevel(dir string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitProg, gitDirFlag, dir, gitTopLevelVerb, gitTopLevelFlag)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Sprintf("%s: %s", gitProbeFailNote, firstLine(text))
	}
	top := firstLine(text)
	if top == "" {
		return "", gitProbeFailNote + " printed nothing"
	}
	return top, ""
}

// firstLine keeps a git message to ONE line: a verdict that wraps is a verdict
// that pushes the other two sources off the reader's screen.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
