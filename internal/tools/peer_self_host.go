package tools

// peer_self_host.go — the ONE answer to "does this peer alias name THIS host?",
// and the refusal every about-to-write peer leg owes when the answer is yes and
// the caller declared no home (Rule 14: one sentence per family, one enforcement
// point; Rule 15: a refusal that names itself and offers a way out).
//
// WHY THIS EXISTS (measured 2026-10-08, master 087f506df, task 01a117c9-1111).
// A probe running inside a private sandbox (Rule 34) ran
//
//	GOPI_HOME=$SB GOPI_SYNC_PEERS=peer1 gopi task sync --peer peer1 --next-eligible <id>
//
// and FOUR fixture rows landed in the OPERATOR's real `~/.gopi/memory.db` — two
// of them were then dispatched as real work. The rows carried the
// `from-<peer>` tag, which is written only at IMPORT, so they provably arrived
// through the peer leg rather than through a local submit.
//
// The two halves of the mechanism, both measured:
//
//	①  the alias names THIS machine. `~/.ssh/config` carries `Host peer1` →
//	   192.0.2.6, and 192.0.2.6 IS this host's own address. So `--peer
//	   peer1` is not a remote machine at all: it is an `ssh` to self, and the
//	   peer-side argv runs HERE.
//	②  nothing binds the profile. The peer-side argv is prefixed with
//	   `GOPI_HOME=… PATH=…` ONLY when `--remote-home` is non-empty
//	   (syncRowRemotePrefix). Absent it, the peer's own login shell decides —
//	   which, for an alias pointing at self, is the OPERATOR's profile.
//
// Either half alone is harmless; together they make a leg whose whole purpose
// is isolation write into the one ledger isolation exists to protect. And the
// caller did everything right — its own profile was correct, the peer was
// declared, the verb was the documented one — and the row still landed in real
// work. That is why the fix is a REFUSAL rather than a better default: the
// caller's intent is not inferable from the command line.
//
// WHEN IT REFUSES. A self-peer leg that is about to WRITE and was given no
// `--remote-home`. Two sentences, ONE verdict:
//
//	the caller's ledger is the operator's own  → "this write would land in the
//	ledger this run is working from";
//	the caller is already isolated (a sandbox, or an explicit --db) → "the peer
//	side would run in the OPERATOR's profile, which is NOT the ledger this run
//	is working from".
//
// The second is the measured incident: a sandbox's own isolation is exactly
// what a self-peer write defeats. Both are refusals because in BOTH cases the
// profile the peer would use was never chosen by the caller — it is whatever
// the machine's login shell resolves, and on a self-peer that is never a REMOTE
// machine's profile. `--remote-home DIR` is the way out, and it is the designed
// one: it binds GOPI_HOME (and DIR/bin on PATH) on the peer-side argv, which is
// how a same-host leg is supposed to work — two private profiles, nothing
// operator-visible.
//
// WHAT IT DOES NOT TOUCH. A real remote peer (`--peer hp` from this host) is
// never local, so the ordinary deployment is byte-for-byte unchanged. A
// read-only leg is not refused at all — see the SCOPE note at the end.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// selfPeerSSHDirName / selfPeerSSHConfigName locate the ssh CLIENT's own
	// configuration — the file whose `HostName` decides what an alias
	// RESOLVES to, as opposed to what it is merely called.
	selfPeerSSHDirName    = ".ssh"
	selfPeerSSHConfigName = "config"

	// selfPeerSSHConfigFieldSep and selfPeerSSHFieldBreaks are ssh's two
	// accepted key/value separators and its two field breaks, folded the same
	// way the sibling reader does it (ssh accepts `HostName x` and
	// `HostName=x`).
	selfPeerSSHConfigFieldSep = "="
	selfPeerSSHFieldBreaks    = " \t"

	// selfPeerSSHKeyHost / selfPeerSSHKeyHostName are the two stanza keys this
	// reader understands. The vocabulary is ssh's; naming them here is what
	// keeps the reader from matching on bare strings (Rule 1).
	selfPeerSSHKeyHost     = "host"
	selfPeerSSHKeyHostName = "hostname"

	// selfPeerRefusalPrefix opens every refusal this file produces, so a reader
	// scanning a log finds the family without reading the sentence.
	selfPeerRefusalPrefix = "refusing a WRITE leg against a peer that names THIS host: "
)

// SyncRowRemoteHomeFlagName is `--remote-home DIR`: the peer-side argv's
// ISOLATION channel, and the WAY OUT every refusal above hands the caller. It
// is declared HERE, in the layer that renders the binding (syncRowRemotePrefix),
// because the flag's meaning is a fact about this leg — cmd/agent references
// this constant rather than re-spelling it (Rule 13: a constant defined and
// then bypassed by a literal is the defect; Rule 14: one definition, and the
// consumer imports it).
const SyncRowRemoteHomeFlagName = "remote-home"

// errSelfPeerWrite is the sentinel every refusal wraps, so a caller (or a test)
// can tell "the topology refused this" from "the transport failed" through the
// ordinary error chain (errors.Is) — the distinction the family already draws
// for undeclared peers.
var errSelfPeerWrite = errors.New("peer names this host")

// selfPeerLocalAddrs returns every address this host answers to: its
// non-loopback interface addresses AND the addresses its own name resolves to.
//
// The NAME half is not decoration: a container, a host whose alias points at
// its DNS name, or a configuration that reaches this machine by name rather
// than by address would all be missed by an interface-list comparison alone,
// and every miss is a silent write into the operator's profile.
//
// An unmeasurable half contributes no addresses — and that is safe in this
// direction only because the verdict below does not depend on this set being
// COMPLETE: an empty set means "not provably local", which is the conservative
// answer for a predicate whose false positive breaks a working deployment.
func selfPeerLocalAddrs() map[string]bool {
	addrs := map[string]bool{}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			ifaceAddrs, aerr := iface.Addrs()
			if aerr != nil {
				continue
			}
			for _, a := range ifaceAddrs {
				ipnet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					addrs[ip4.String()] = true
				}
			}
		}
	}
	if name, err := os.Hostname(); err == nil {
		if looked, lerr := net.LookupHost(name); lerr == nil {
			for _, ip := range looked {
				parsed := net.ParseIP(ip)
				if parsed == nil {
					continue
				}
				if ip4 := parsed.To4(); ip4 != nil {
					addrs[ip4.String()] = true
				}
			}
		}
	}
	return addrs
}

// selfPeerResolvedHost reads the `HostName` the ssh CLIENT's own config gives
// this alias, as TEXT.
//
// `ssh -G` is deliberately NOT used: it expands a stanza through a live
// resolution, and this predicate is asked on every write leg — an accidental
// lookup in the middle of a transport is a dependency nobody asked for (Rule
// 35), and the config text is what the self-reference is spelled in anyway.
//
// WHICH FILES, AND WHY ALL OF THEM. ssh reads `$HOME/.ssh/config` — so $HOME is
// the FIRST and authoritative source, and it is the one a run's own environment
// controls. The other two exist because gopi's profile is not always `$HOME`:
//
//	$HOME/.ssh/config            what ssh itself reads (the operator's, when
//	                             nothing renamed HOME);
//	GOPI_HOME/.ssh/config        a Rule 34 sandbox's own config, when it has one
//	                             (config.GopiHome is GOPI_HOME-aware);
//	<OS user record home>/.ssh/config   the operator's, even if a wrapper
//	                             renamed or unset HOME (the anchor the profile
//	                             guard uses, for the same reason);
//
// They are deduped and tried in that order, first `HostName` wins. WHY THE
// THIRD IS NEEDED AT ALL: `--remote-home` binds the PEER side and is parsed only
// after ssh has already used THIS process's `$HOME` to locate its config, so the
// alias genuinely resolves through the operator's file even when the caller runs
// in a private profile — and a predicate that read only the caller's home would
// answer "unknown" for the exact alias it was built to catch.
//
// A file that is absent, unreadable, or carries no `HostName` for the alias
// yields "" — unknown, never a guess. It is ~/.ssh/config — the client's own
// configuration for reaching nodes — not a credential store: private keys and
// known_hosts are different files and are not opened here.
func selfPeerResolvedHost(alias string) string {
	for _, p := range selfPeerSSHConfigPaths() {
		if h := sshConfigHostNameFor(p, alias); h != "" {
			return h
		}
	}
	return ""
}

// selfPeerSSHConfigPaths is the ONE ordered, deduped list of ssh client configs
// this predicate consults — the three homes above, each contributing at most one
// path, and a home that cannot be resolved contributing none (rather than an
// empty string that would be joined into a wrong path).
func selfPeerSSHConfigPaths() []string {
	paths := make([]string, 0, 3)
	add := func(home string) {
		home = strings.TrimSpace(home)
		if home == "" {
			return
		}
		p := filepath.Join(home, selfPeerSSHDirName, selfPeerSSHConfigName)
		for _, seen := range paths {
			if seen == p {
				return
			}
		}
		paths = append(paths, p)
	}
	// $HOME first: it is what ssh itself reads.
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}
	if home, err := config.GopiHome(); err == nil {
		add(home)
	}
	add(selfPeerOperatorHome())
	return paths
}

// selfPeerOperatorHome is the OPERATOR's home, read from the OS user record
// rather than `$HOME`.
//
// The distinction is the whole point: inside a Rule 34 sandbox `$HOME` /
// GOPI_HOME is the sandbox, while the ssh config that resolves the alias
// belongs to the human who owns the machine. The user record is the one
// identity a sandbox cannot shadow — the anchor internal/testutil's profile
// guard uses for the same reason.
func selfPeerOperatorHome() string {
	u, err := user.Current()
	if err != nil || strings.TrimSpace(u.HomeDir) == "" {
		return ""
	}
	return u.HomeDir
}

// sshConfigHostNameFor returns the `HostName` a given ssh config file gives the
// alias, or "" when the file carries no HostName for it.
//
// MATCHING IS LITERAL ON PURPOSE. ssh itself matches patterns and honours
// `Host *` defaults; reproducing that grammar would be a second ssh
// implementation whose disagreements with the real client stay invisible until
// they matter. The configs this leg must read — a `Host peer1 / HostName
// 192.0.2.6` self-reference, and ordinary named stanzas — are literal ones,
// and anything fancier yields "" = unknown, which the caller treats as "not
// provably local" rather than as a fact.
func sshConfigHostNameFor(path, alias string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	inStanza := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value := splitSSHConfigLine(line)
		switch strings.ToLower(key) {
		case selfPeerSSHKeyHost:
			inStanza = false
			for _, field := range strings.Fields(value) {
				if field == alias {
					inStanza = true
					break
				}
			}
		case selfPeerSSHKeyHostName:
			if !inStanza {
				continue
			}
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// splitSSHConfigLine splits a stanza line into key and value, folding ssh's two
// accepted separators into one (ssh accepts `HostName x` and `HostName=x`).
func splitSSHConfigLine(line string) (key, value string) {
	line = strings.ReplaceAll(line, selfPeerSSHConfigFieldSep, " ")
	i := strings.IndexAny(line, selfPeerSSHFieldBreaks)
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

// selfPeerAliasIsLocal reports whether `alias` names THIS host, together with
// the sentence naming the evidence — so a refusal can quote what it measured
// instead of asserting a conclusion.
//
// FOUR answers:
//
//   - the alias IS one of this host's own addresses (written literally in the
//     config) → local;
//   - the alias RESOLVES to one of them (`Host peer1` → 192.0.2.6, this
//     host's own address) → local, and this is the measured case;
//   - the alias is this host's own NAME (`peer1` on peer1) → local — the same
//     identity check the claim leg already performs before filtering a peer
//     equal to the node's own name (claimBroadcastNodeName);
//   - everything else, including an unmeasurable address set or a name that
//     does not resolve → NOT local.
func selfPeerAliasIsLocal(alias string) (bool, string) {
	name := strings.TrimSpace(alias)
	if i := strings.LastIndex(name, "@"); i >= 0 { // a `user@host` alias names the host after the '@'
		name = strings.TrimSpace(name[i+1:])
	}
	if name == "" {
		return false, ""
	}
	addrs := selfPeerLocalAddrs()
	if local, why := selfPeerAddrIsLocal(name, addrs); local {
		return true, why
	}
	host := selfPeerResolvedHost(name)
	if host != "" && host != name {
		if local, why := selfPeerAddrIsLocal(host, addrs); local {
			return true, fmt.Sprintf("the alias %q resolves to %s, and %s", name, host, why)
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return false, ""
	}
	if strings.TrimSpace(hostname) == "" || !strings.EqualFold(name, strings.TrimSpace(hostname)) {
		return false, ""
	}
	return true, fmt.Sprintf("the alias %q is this host's own name (%s)", name, strings.TrimSpace(hostname))
}

// selfPeerAddrIsLocal is the address half, kept separate so the alias and what
// it resolves to are judged by ONE comparison.
func selfPeerAddrIsLocal(addr string, addrs map[string]bool) (bool, string) {
	if strings.HasPrefix(addr, "localhost") {
		return true, fmt.Sprintf("%q is the loopback of this host", addr)
	}
	parsed := net.ParseIP(addr)
	if parsed == nil {
		return false, ""
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return false, ""
	}
	if addrs[ip4.String()] {
		return true, fmt.Sprintf("it is one of THIS host's addresses (%s)", ip4.String())
	}
	return false, ""
}

// selfPeerWriteRefusal is the ONE verdict every about-to-write peer leg asks
// before it spawns ssh. nil = proceed; otherwise a refusal that names the
// alias, the profile the write would land in, and the way out.
//
// THE CRITERION, in one sentence (it is the acceptance criterion of this fix):
//
//	a peer leg must not run the peer process in a profile the CALLER did not
//	choose; on a self-peer, "no --remote-home" leaves that choice to the local
//	login shell, which is not a remote machine's profile at all.
//
// `ledger` is the ledger THIS caller is writing from (memoryDBPathOr(opts.DBPath)),
// used only to render the accurate sentence — the verdict is the same either
// way. It is a PATH rather than GopiDir() because they diverge exactly where it
// matters: a caller can stand in the operator's profile while pointing the leg
// at a private `--db`, and the sentence must then say "the operator's profile,
// which is not the ledger you are working from" instead of claiming the write
// lands in a ledger the caller is not using.
//
// `what` names the leg in the peer's own vocabulary ("row send", "transfer") so
// the operator can tell WHICH face refused.
func selfPeerWriteRefusal(alias, remoteHome, ledger, what string) error {
	if strings.TrimSpace(alias) == "" || strings.TrimSpace(remoteHome) != "" {
		return nil
	}
	local, why := selfPeerAliasIsLocal(alias)
	if !local {
		return nil
	}
	// The two sentences, one verdict. Both name where the write WOULD land, and
	// both hand the caller the same way out. The sentence is assembled in two
	// steps so the SENTINEL stays the single wrapped error (errors.Is) and the
	// leg's own name can open the line without the format string having to
	// count one more verb than the reader expects.
	landing := "the OPERATOR's own profile (~/" + config.ProfileDirName + "/" + config.MemoryDBFileName + ")"
	if selfPeerLedgerIsOperatorProfile(ledger) {
		landing = "the profile THIS caller resolves (" + strings.TrimSpace(ledger) + ")"
	}
	detail := fmt.Sprintf(
		"%s for peer %s: it names THIS host (%s), and no --remote-home was given — so the peer-side process decides its own profile, and on this alias that is %s. The ledger a self-peer write reaches is never a remote machine's. Pass --remote-home DIR to BIND it: that exports GOPI_HOME=DIR (and DIR/bin on PATH) to the peer-side argv, which is how a same-host leg is meant to run (two private profiles — e.g. `gopi sandbox create --label peer` — so the operator's ledger is never on the path). If you meant a real remote machine, use ITS alias: this one resolves to this host",
		what, alias, why, landing)
	return fmt.Errorf("%w: %s%s", errSelfPeerWrite, selfPeerRefusalPrefix, detail)
}

// selfPeerLedgerIsOperatorProfile reports whether a ledger path given to a
// write leg resolves to the OPERATOR's own ledger (`~/.gopi/memory.db`, from
// the OS user record). It only chooses which sentence the refusal prints.
//
// An empty or unresolvable path answers false: a leg that cannot name its own
// ledger must not be described as writing the operator's.
func selfPeerLedgerIsOperatorProfile(ledger string) bool {
	p := strings.TrimSpace(ledger)
	if p == "" {
		return false
	}
	home := selfPeerOperatorHome()
	if home == "" {
		return false
	}
	return samePath(p, filepath.Join(home, config.ProfileDirName, config.MemoryDBFileName))
}

// samePath compares two paths by CLEANED ABSOLUTE form, so `~/.gopi/memory.db`,
// `/home/user/./.gopi/memory.db` and a relative walk to the same file agree.
// A path that cannot be made absolute falls back to its cleaned form (a
// comparison, never an error — this is a sentence chooser, not a gate).
func samePath(a, b string) bool {
	norm := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(p)
	}
	return norm(a) == norm(b)
}

// SCOPE, stated so this file is not mistaken for coverage it does not have.
//
// COVERED — the ROW legs, i.e. the faces that export/import a row through a
// peer process whose profile the caller did not bind:
//
//	SyncRowSendEx            (the push: `gopi task send`)
//	SyncTransferTakeOver     (the round whose SIGN half is a peer-side write:
//	                          `gopi task receive --takeover`)
//
// NOT REFUSED — the READ-ONLY legs. `SyncTransferPickRow` (the OFFER) and the
// ssh+sqlite fetch of `SyncRowReceive` read the peer's ledger and write nothing,
// so they stay UNBOUND:
//
//	* an unbound read is the only shape that reaches a REAL remote ledger at
//	  all. `harvestRemoteDBPath` is `~/.gopi/memory.db` — a RELATIVE path the
//	  peer's login shell expands. A gate that bound the caller's GOPI_HOME onto
//	  that ssh would make a sandboxed pull look for `<sandbox>/.gopi/memory.db`
//	  in the REMOTE filesystem, where it does not exist: the read would break,
//	  and it would break by returning "row not found" rather than by refusing,
//	  which is the worst shape a fix can have;
//	* on a self-peer the read is answered by this same machine, and the wrong
//	  ledger is a real hazard — but this leg has its own isolation channel for
//	  it (`--remote-db`, an absolute path the caller passes explicitly, which
//	  the measured probe used). A WRITE is different in kind: it is the effect
//	  nobody can undo (append-only, Rule 30's neighbouring concern), so it is
//	  the write that carries the refusal.
//
// NOT COVERED — the OTHER write faces of the family, named here rather than
// implied away: the claim broadcast (`--claim-remote`, memory_sync_claim.go) and
// the session leg (`--serve-remote`, memory_sync_session.go) also write on the
// peer. They are guarded today only by a self-exclusion FILTER at the claim
// call site — it skips a peer equal to this node's identity NAME, which does not
// catch an alias that RESOLVES here (the measured half ① of this defect).
// Extending this refusal to them is a separate change with its own measurements;
// the honest statement is that this fix closes the row legs, and that the same
// predicate is now available to close the others.
