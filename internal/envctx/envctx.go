// Package envctx owns the SYSTEM ENVIRONMENT description: the file gopi
// writes about the host it runs on, and the bounded SUMMARY+POINTER block the
// agent's prompt carries about that file.
//
// WHY IT EXISTS (measured 2026-10-01, task 01a0f687): an agent had no idea what
// machines it could reach. Asked about a peer node, it either shelled out to
// probe the network live or GUESSED — the session that opened this work
// confirmed `ssh hp` was usable only by trying it, and the addresses on the
// host disagree with each other (see the conflict section below). hermes solved
// this with an infrastructure-injection mechanism: read one description file,
// inject a SUMMARY plus a POINTER, tolerate its absence silently. This package
// is that mechanism, under gopi's rules.
//
// THE TWO HALVES, AND WHY THEY ARE ONE PACKAGE.
//
//	Write/Measure  produce the file. Every address in it was MEASURED by this
//	               run (a syscall, a /proc read, a file read) at a recorded
//	               time — never remembered, never inferred.
//	Load           read the file and return the prompt block. It does NOT
//	               re-measure: the file is the record, so what the agent is
//	               told is exactly what a human reading the file is told.
//
// CONFLICTS ARE RECORDED, NOT RESOLVED (Rule 15). The one measured here: the
// ssh config maps BOTH `alpha` and `nas` to 192.0.2.5, and the alias lines
// themselves spell addresses that differ from their own HostName (rpi5 = .4 on
// the Host line, .14 on HostName). A generator that picked one silently would
// be manufacturing a fact it never measured; this one prints both, names the
// ambiguity, and tells the reader how to settle it. The file also states, per
// section, WHAT WAS MEASURED and WHAT IS ONLY TEXT — the two are not the same
// evidence and the reader must be able to tell them apart.
//
// SCOPE (deliberate, Rule 35 zero dependency). Generation is a ONE-SHOT,
// operator-invoked command (`gopi config env --write`). Nothing regenerates it
// in the background, no daemon watches it, no timer refreshes it, and gopi
// never writes it during a run. A stale file is visible as stale: it carries
// its own generation timestamp and the hostname it was measured on, and Load
// says so on stderr when the hostname no longer matches.
package envctx

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// FileName is the environment-description file's NAME, inside the gopi
	// profile root. ONE spelling (Rule 1b/13): the generator writes it, the
	// reader resolves it, and the operator is told it — all from here.
	//
	// WHY NOT THE PROJECT'S DIRECTORY (and so not PROJECT.md's neighbour): the
	// content is a HOST property, not a project's. GOPI.md is the CLIENT's own
	// rule file — untracked, placed per checkout, and read from the run's cwd
	// (an instruction surface, not a node inventory) — whereas a node inventory
	// belongs to the MACHINE: it lives outside every checkout and must never be
	// committed to a repository (it names internal addresses and node roles).
	FileName = "environment.md"

	// fileMode is the file's permission: readable by its owner only. The file
	// carries no credential — the generator never writes one — but a node
	// inventory is reconnaissance material and does not default to world
	// readable.
	fileMode = os.FileMode(0o600)
	// dirMode is used only when the profile root does not exist yet.
	dirMode = os.FileMode(0o700)

	// summaryBeginLine / summaryEndLine delimit the machine-readable summary
	// inside the file. The block is DELIMITED rather than re-derived so the
	// reader and the writer cannot drift: what the prompt carries is literally
	// the text a human reads first, not a second rendering of the same facts
	// that could disagree with the file (Rule 14 — one definition of the
	// summary, in the artifact itself).
	summaryBeginLine = "[envctx-summary-begin]"
	summaryEndLine   = "[envctx-summary-end]"

	// maxSummaryAliases is how many ssh aliases the summary line lists before
	// it stops and says how many it dropped. The rendering is bounded; the
	// file is not (Rule 15: a truncation is announced, never silent).
	maxSummaryAliases = 14
	// maxSummaryConflictLines is the same bound for the conflict listing. A
	// host with many conflicts gets its first few named in the prompt and the
	// remainder counted — the full list is always in the file.
	maxSummaryConflictLines = 4

	// MaxSummaryChars is the HARD ceiling on the summary the prompt carries,
	// and it is ENFORCED rather than documented: Load refuses to inject a
	// summary longer than this and says so on stderr, naming the file and the
	// size. Truncating instead would hand the agent a half-truth it could not
	// tell was halved (Rule 15); injecting it anyway would let a generator
	// change grow every prompt on the machine. The file itself stays complete
	// and readable — only the always-resident block is bounded.
	MaxSummaryChars = 2048

	// sshDirName / sshConfigName locate the ONE declared external source the
	// generator reads. Its absence is tolerated: a host with no ssh config
	// still gets a file, with the node section saying so.
	sshDirName    = ".ssh"
	sshConfigName = "config"

	// sourceSSHConfig / sourceLive are the SOURCE labels the file uses to say
	// where a fact came from. They are values a reader greps for, so they are
	// named here rather than typed at each site (Rule 1b).
	sourceSSHConfig = "~/.ssh/config"
	sourceLive      = "live measurement (this run)"
)

// Facts is everything the generator measured. Every field is a MEASURED value:
// a syscall, a /proc read, or a line read out of a named file. Nothing here is
// defaulted, inferred, or carried over from a previous run.
type Facts struct {
	// GeneratedAt is when this measurement ran.
	GeneratedAt time.Time
	// Hostname is the machine that was measured (os.Hostname).
	Hostname string
	// OS is the PRETTY_NAME from /etc/os-release ("" when unreadable).
	OS string
	// Addresses are this host's non-loopback IPv4 addresses, "iface  a.b.c.d".
	Addresses []string
	// Gateway is the IPv4 default gateway from /proc/net/route ("" if none).
	Gateway string
	// Nodes are the ssh-config aliases, in file order.
	Nodes []Node
	// Conflicts are the disagreements between sources, in the order found.
	// They are REPORTED, never resolved — see the package comment.
	Conflicts []Conflict
	// Sources are the inputs that were readable, by label.
	Sources []string
	// Missing lists declared sources that were NOT readable. A missing source
	// is announced in the file, so a reader can tell "no nodes exist" from
	// "the node list could not be read".
	Missing []string
}

// Node is one ssh-config stanza: the alias(es), the address it resolves to, and
// where that address was written.
type Node struct {
	// Alias is the first (non-wildcard) pattern on the Host line.
	Alias string
	// Address is what the alias resolves to: HostName when present, else an
	// address literal written on the Host line itself.
	Address string
	// AddressFrom names the line the address came from, so a reader can check
	// it in one grep.
	AddressFrom string
	// Inline is an address literal that appeared on the Host line when a
	// HostName ALSO exists. Its presence is the self-contradiction the file
	// records.
	Inline string
	// User and Port are carried only because a reader needs them to compare
	// what is written with what a connection actually negotiates.
	User string
	Port string
	// Line is the Host line's number in the config file.
	Line int
}

// Conflict is one disagreement between two recorded sources. Kind is a named
// class (never a bare literal at the comparison sites, Rule 1b).
type Conflict struct {
	// Kind is the conflict class: conflictAddressShared or
	// conflictAliasInline.
	Kind string
	// Subject is what the conflict is about (an address, an alias).
	Subject string
	// Detail is the human-readable sentence, ready to print.
	Detail string
}

// Conflict classes. The prompt and the file both branch on these.
const (
	// conflictAddressShared: several aliases resolve to one address.
	conflictAddressShared = "address-shared-by-aliases"
	// conflictAliasInline: the Host line spells an address its own HostName
	// contradicts.
	conflictAliasInline = "alias-line-contradicts-hostname"
)

// Path resolves the environment file's absolute location in the gopi profile.
func Path() (string, error) {
	dir, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve the gopi profile directory: %w", err)
	}
	return filepath.Join(dir, FileName), nil
}

// Measure collects every fact the file reports. It reads and never writes; a
// failure of any SINGLE probe is recorded as a missing source (or an empty
// field) rather than aborting the run — a host whose /etc/os-release is
// unreadable is still a host with a hostname and a routing table.
func Measure() Facts {
	f := Facts{GeneratedAt: time.Now()}
	f.Hostname, _ = os.Hostname() //stylegate:ignore a host that cannot report its own name is still measurable in every other dimension; the empty value is rendered as unknown by the report
	f.OS = osPrettyName()
	f.Addresses = localIPv4()
	f.Gateway = defaultGateway()

	nodes, missing, err := readSSHConfig(sshConfigPath())
	if err != nil {
		f.Missing = append(f.Missing, sourceSSHConfig+" — "+err.Error())
	}
	if missing != "" {
		f.Missing = append(f.Missing, sourceSSHConfig+" — "+missing)
	}
	f.Nodes = nodes
	f.Sources = append(f.Sources, sourceLive)
	if missing == "" && err == nil {
		f.Sources = append(f.Sources, sourceSSHConfig)
	}
	f.Conflicts = detectConflicts(nodes)
	return f
}

// sshConfigPath is the ONE resolution of the ssh config's location: the real
// home (config.GopiHome — GOPI_HOME-aware, so a sandbox measures its own
// home), not the gopi profile root.
func sshConfigPath() string {
	home, err := config.GopiHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, sshDirName, sshConfigName)
}

// osPrettyName reads PRETTY_NAME from /etc/os-release. An unreadable or
// unparsable file yields "" — the report says "unknown" rather than inventing
// a distribution.
func osPrettyName() string {
	const osReleasePath = "/etc/os-release"
	const prettyKey = "PRETTY_NAME="
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, prettyKey) {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prettyKey)), `"`)
	}
	return ""
}

// localIPv4 lists this host's non-loopback IPv4 addresses as "iface  address",
// interface-sorted, one address per entry (an interface with two addresses
// yields two entries — peer1's enp0s25/wlo1 case is exactly why a single
// "this host's IP" is not a fact).
func localIPv4() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			out = append(out, iface.Name+"  "+ip4.String())
		}
	}
	sort.Strings(out)
	return out
}

// defaultGateway reads the IPv4 default route out of /proc/net/route. The
// gateway column is little-endian hex. An absent default route (a host with no
// LAN) yields "".
func defaultGateway() string {
	const routePath = "/proc/net/route"
	const defaultDestination = "00000000"
	const fieldIface, fieldDestination, fieldGateway = 0, 1, 2
	data, err := os.ReadFile(routePath)
	if err != nil {
		return ""
	}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 { // header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) <= fieldGateway {
			continue
		}
		if fields[fieldDestination] != defaultDestination {
			continue
		}
		ip, ok := hexLittleEndianIPv4(fields[fieldGateway])
		if !ok {
			continue
		}
		return ip + "  (via " + fields[fieldIface] + ")"
	}
	return ""
}

// hexLittleEndianIPv4 converts /proc/net/route's hex gateway column to dotted
// quad. It reports ok=false rather than returning a half-parsed address.
func hexLittleEndianIPv4(hex string) (string, bool) {
	if len(hex) != 8 {
		return "", false
	}
	var b [4]byte
	for i := 0; i < 4; i++ {
		v, err := parseHexByte(hex[i*2 : i*2+2])
		if err != nil {
			return "", false
		}
		b[3-i] = v
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]), true
}

func parseHexByte(s string) (byte, error) {
	var v int
	_, err := fmt.Sscanf(s, "%02x", &v)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}
