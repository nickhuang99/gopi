package envctx

import (
	"fmt"
	"strings"
	"time"
)

// render.go — the artifact. ONE renderer produces BOTH the file on disk and
// the block the prompt carries: the block is the file's OWN summary section,
// delimited by two marker lines, so what the agent is told is literally what a
// human reading the file is told first. A second rendering of the same facts
// could drift from the file; a delimited section cannot (Rule 14).
//
// EVERY SECTION THAT IS NOT A MEASUREMENT SAYS SO. The node table is read out
// of the ssh config — it is TEXT, unverified, and labelled as such, including
// the explicit warning that a name resolves through an ssh alias that may
// itself be wrong. A reader who cannot tell a syscall from a file line will
// treat a stale address as a fact; that is the failure this file exists to
// prevent, so the distinction is printed, not implied.

const (
	// FileTitle is the artifact's first line: the name a reader greps for in a
	// file that has been copied to an evidence directory or a peer's home, and
	// the string the `gopi config env` help advertises as what it prints.
	// Exported because TWO readers need the same value: the renderer writes it
	// and the CLI/test surfaces assert on it (Rule 14 — one definition).
	FileTitle = "# System environment — measured description of this host"
	// unknownValue is what a field renders as when the probe produced nothing.
	// It is a WORD, so an absent value never reads as an empty fact (Rule 15).
	unknownValue = "unknown"
	// generatedLineFormat carries the timestamp AND the host it was measured on
	// — the two things a reader needs to judge staleness.
	generatedLineFormat = "> Generated %s on host `%s` by `gopi config env --write`. " +
		"NOT refreshed automatically: it describes the machine as it was at that moment."

	// selfNameLine names the artifact INSIDE itself. A file that a reader may
	// find copied elsewhere (a peer's home, an evidence directory) has to say
	// what it is and which command produces it, or it becomes an anonymous
	// inventory whose provenance nobody can reconstruct (Rule 15).
	selfNameLine = "> This file is `%s`, written into the gopi profile — outside version control, " +
		"because it is a HOST property and it names internal addresses."
)

// Render produces the file's full text from measured facts. It is pure: same
// Facts in, same bytes out, so the writer and the tests share one definition of
// the artifact.
func Render(f Facts) string {
	var b strings.Builder
	b.WriteString(FileTitle + "\n")
	b.WriteString(fmt.Sprintf(generatedLineFormat+"\n", f.GeneratedAt.Format(time.RFC3339), orUnknown(f.Hostname)))
	b.WriteString(fmt.Sprintf(selfNameLine, FileName) + "\n\n")

	writeSummary(&b, f)
	writeHowToRead(&b)
	writeHostSection(&b, f)
	writeNodeSection(&b, f)
	writeConflictSection(&b, f)
	writeSourceSection(&b, f)
	return b.String()
}

// writeSummary writes the delimited block the prompt carries verbatim.
func writeSummary(b *strings.Builder, f Facts) {
	b.WriteString(summaryBeginLine + "\n")
	b.WriteString(summaryLine(f) + "\n")
	b.WriteString("ssh aliases (from " + sourceSSHConfig + "; TEXT as written, NOT verified reachable): " +
		aliasSummary(f.Nodes) + "\n")
	b.WriteString("conflicts on disk (recorded, NOT resolved): " + conflictSummary(f.Conflicts) + "\n")
	if len(f.Missing) > 0 {
		b.WriteString("sources that could not be read (so their facts are ABSENT, not empty): " +
			strings.Join(f.Missing, "; ") + "\n")
	}
	b.WriteString(summaryEndLine + "\n\n")
}

// summaryLine is the one-line identity of the measured host.
func summaryLine(f Facts) string {
	addrs := "(no non-loopback IPv4 address)"
	if len(f.Addresses) > 0 {
		addrs = strings.Join(f.Addresses, ", ")
	}
	gw := f.Gateway
	if gw == "" {
		gw = unknownValue
	}
	return fmt.Sprintf("this host: %s — %s; addresses: %s; default gateway: %s; measured %s",
		orUnknown(f.Hostname), orUnknown(f.OS), addrs, gw, f.GeneratedAt.Format(time.RFC3339))
}

// aliasSummary renders the alias→address list, bounded, with the dropped count
// stated rather than silently truncated (Rule 15).
func aliasSummary(nodes []Node) string {
	if len(nodes) == 0 {
		return "(none readable)"
	}
	var parts []string
	shown, dropped := nodes, 0
	if len(shown) > maxSummaryAliases {
		dropped = len(shown) - maxSummaryAliases
		shown = shown[:maxSummaryAliases]
	}
	for _, n := range shown {
		addr := n.Address
		if addr == "" {
			addr = "(no address written)"
		}
		parts = append(parts, n.Alias+"="+addr)
	}
	line := strings.Join(parts, ", ")
	if dropped > 0 {
		line += fmt.Sprintf(" (+%d more in this file)", dropped)
	}
	return line
}

// conflictSummary renders the conflicts, bounded the same way. "none recorded"
// is a VALUE: it says the check ran and found nothing, which is not the same as
// a check that never ran.
func conflictSummary(conflicts []Conflict) string {
	if len(conflicts) == 0 {
		return "none recorded"
	}
	var parts []string
	shown, dropped := conflicts, 0
	if len(shown) > maxSummaryConflictLines {
		dropped = len(shown) - maxSummaryConflictLines
		shown = shown[:maxSummaryConflictLines]
	}
	for _, c := range shown {
		parts = append(parts, c.Subject+" ("+c.Kind+")")
	}
	line := strings.Join(parts, "; ")
	if dropped > 0 {
		line += fmt.Sprintf(" (+%d more in this file)", dropped)
	}
	return line
}

func writeHowToRead(b *strings.Builder) {
	b.WriteString("## How to read this file\n\n")
	b.WriteString("Two kinds of claim live here and they are NOT equally strong:\n\n")
	b.WriteString("  * MEASURED — this run called the kernel or read /proc on this host at the\n")
	b.WriteString("    timestamp above. These are the `this host` lines and the default gateway.\n")
	b.WriteString("  * TEXT — read out of a named file and reported AS WRITTEN. Nobody dialled\n")
	b.WriteString("    it, so a node may be down, renamed or addressed differently today. The ssh\n")
	b.WriteString("    alias table is this kind, and so is every conflict below.\n\n")
	b.WriteString("A conflict is RECORDED, never resolved. When two sources disagree, both\n")
	b.WriteString("addresses stay in this file: picking one would be manufacturing a fact. To\n")
	b.WriteString("settle a node, ask the node — `ssh <alias> hostname` — before naming it.\n\n")
}

func writeHostSection(b *strings.Builder, f Facts) {
	b.WriteString("## This host — MEASURED\n\n")
	b.WriteString("  hostname          " + orUnknown(f.Hostname) + "\n")
	b.WriteString("  os                " + orUnknown(f.OS) + "\n")
	b.WriteString("  default gateway   " + orUnknown(f.Gateway) + "\n")
	if len(f.Addresses) == 0 {
		b.WriteString("  ipv4 addresses    (none — no non-loopback interface is up)\n")
	} else {
		for i, a := range f.Addresses {
			label := "ipv4 addresses"
			if i > 0 {
				label = ""
			}
			b.WriteString(fmt.Sprintf("  %-17s %s\n", label, a))
		}
	}
	b.WriteString("\n")
}

func writeNodeSection(b *strings.Builder, f Facts) {
	b.WriteString("## Nodes — TEXT (ssh aliases as written in " + sourceSSHConfig + ")\n\n")
	if len(f.Nodes) == 0 {
		b.WriteString("  (no readable alias; every address below would be a guess, so none is written)\n\n")
		return
	}
	b.WriteString("  alias            address            written as / user / port\n")
	for _, n := range f.Nodes {
		addr := n.Address
		if addr == "" {
			addr = unknownValue
		}
		detail := n.AddressFrom
		if n.Inline != "" {
			detail += "; Host line also spells " + n.Inline
		}
		if n.User != "" {
			detail += "; user " + n.User
		}
		if n.Port != "" {
			detail += "; port " + n.Port
		}
		b.WriteString(fmt.Sprintf("  %-16s %-18s %s\n", n.Alias, addr, detail))
	}
	b.WriteString("\n  The alias and `HostName` are the ONLY resolution path recorded here. An\n")
	b.WriteString("  alias whose `Host *` defaults apply differently than you expect resolves\n")
	b.WriteString("  somewhere else — this section reports what the file says, not what a\n")
	b.WriteString("  connection would do.\n\n")
}

func writeConflictSection(b *strings.Builder, f Facts) {
	b.WriteString("## Conflicts between sources — RECORDED, NOT RESOLVED\n\n")
	if len(f.Conflicts) == 0 {
		b.WriteString("  (none) — the sources read on this run agree with each other.\n\n")
		return
	}
	for i, c := range f.Conflicts {
		b.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, c.Kind, c.Detail))
	}
	b.WriteString("\n  These are printed instead of a single chosen answer ON PURPOSE. A resolver\n")
	b.WriteString("  would hide the disagreement and leave a future reader holding a wrong fact\n")
	b.WriteString("  (gopi's no-silent-failure rule): the disagreement is information.\n\n")
}

func writeSourceSection(b *strings.Builder, f Facts) {
	b.WriteString("## Sources\n\n")
	for _, s := range f.Sources {
		b.WriteString("  read      " + s + "\n")
	}
	for _, m := range f.Missing {
		b.WriteString("  MISSING   " + m + "\n")
	}
	if len(f.Sources) == 0 && len(f.Missing) == 0 {
		b.WriteString("  (no source was consulted)\n")
	}
	b.WriteString("\n")
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return unknownValue
	}
	return s
}
