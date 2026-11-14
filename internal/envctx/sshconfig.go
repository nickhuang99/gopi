package envctx

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// sshconfig.go — the ONE declared external source, read as TEXT.
//
// WHAT THIS IS AND IS NOT. Everything it produces is "the file says so" —
// static text, at the revision on disk, that NOBODY VERIFIED is reachable. The
// node list is therefore labelled as written-not-verified in the generated
// file, and the address of a node is never upgraded to a measured fact by
// being read here. The live half of the file (this host's interfaces, the
// default route) is the measured half; the two are kept apart on purpose so a
// reader can tell which claim has a syscall behind it.
//
// The parser is deliberately small — stanza boundaries, the four keys a
// reader needs — rather than a full ssh_config implementation. It does not
// match patterns, expand wildcards, or resolve Include. A key it does not know
// is skipped, and the generated file lists only what was found.

const (
	sshKeyHost     = "host"
	sshKeyHostName = "hostname"
	sshKeyUser     = "user"
	sshKeyPort     = "port"

	// sshWildcardAny is the pattern `Host *`, a default stanza rather than a
	// node. It is skipped: rendering it as a node would invent a machine.
	sshWildcardAny = "*"
	// sshConfigFieldSep splits a stanza line into key and value. ssh accepts
	// spaces and `=`, so both are folded to spaces first.
	sshConfigFieldSep = "="
)

// readSSHConfig parses the ssh config into nodes. It returns:
//
//	nodes   — every stanza whose alias is a name (non-wildcard), in file order
//	missing — a human-readable reason the file was not usable ("" when it was)
//	err     — set only for a genuinely unreadable file (permissions, I/O)
//
// An ABSENT file is not an error and not a failure: a host that never
// configured ssh has no nodes, and the caller records that as a missing source
// so the file can say so (Rule 15 — silence would read as "no nodes exist").
func readSSHConfig(path string) (nodes []Node, missing string, err error) {
	if strings.TrimSpace(path) == "" {
		return nil, "the home directory could not be resolved", nil
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, "not present on this host", nil
		}
		return nil, "", rerr
	}

	var current *Node
	for i, raw := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value := splitSSHLine(line)
		switch strings.ToLower(key) {
		case sshKeyHost:
			current = nil
			fields := strings.Fields(value)
			if len(fields) == 0 {
				continue
			}
			node, ok := newNode(fields, lineNo)
			if !ok {
				continue // a wildcard/default stanza is not a node
			}
			nodes = append(nodes, node)
			current = &nodes[len(nodes)-1]
		case sshKeyHostName:
			if current == nil {
				continue
			}
			current.Address = strings.TrimSpace(value)
			current.AddressFrom = "HostName, line " + strconv.Itoa(lineNo)
		case sshKeyUser:
			if current == nil {
				continue
			}
			current.User = strings.TrimSpace(value)
		case sshKeyPort:
			if current == nil {
				continue
			}
			current.Port = strings.TrimSpace(value)
		}
	}
	return nodes, "", nil
}

// newNode builds a Node from a Host line's patterns. The FIRST pattern is the
// alias; any pattern that looks like an IPv4 literal is recorded as the
// stanza's inline address, which is how a line like
//
//	Host peer 192.0.2.4
//	        HostName 192.0.2.14
//
// states two different addresses for one node. A stanza whose only pattern is
// a wildcard is not a node.
func newNode(fields []string, lineNo int) (Node, bool) {
	alias := ""
	var inline []string
	for _, f := range fields {
		if f == sshWildcardAny || strings.ContainsAny(f, "*?") {
			continue
		}
		if alias == "" {
			alias = f
			continue
		}
		if looksLikeIPv4(f) {
			inline = append(inline, f)
		}
	}
	if alias == "" {
		return Node{}, false
	}
	node := Node{Alias: alias, Line: lineNo}
	if len(inline) > 0 {
		node.Inline = inline[0]
	}
	if looksLikeIPv4(alias) {
		// `Host 192.0.2.5` (no name): the pattern itself is the address, and
		// the "alias" a user types is that literal. Record it as the address
		// with no hostname stanza rather than pretending a name exists.
		node.Address = alias
		node.AddressFrom = "Host line " + strconv.Itoa(lineNo) + " (an address used as its own alias)"
	}
	return node, true
}

// splitSSHLine splits a stanza line into its key and value, folding ssh's two
// accepted separators (whitespace and `=`) into one. Quoted values keep their
// quotes: the value is reported as written, never re-encoded.
func splitSSHLine(line string) (key, value string) {
	line = strings.ReplaceAll(line, sshConfigFieldSep, " ")
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

// looksLikeIPv4 reports whether s is a dotted quad of four numeric octets. It
// is shape-checking only — this parser never resolves or dials an address.
func looksLikeIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// detectConflicts finds the disagreements BETWEEN SOURCES, and inside the
// config file itself:
//
//  1. one address claimed by several aliases — the measured hp/nas case, where
//     the address alone does not identify the machine;
//  2. a stanza whose Host line spells an address its own HostName contradicts —
//     the measured rpi5 case.
//
// Both are RECORDED. Neither is resolved, and neither is dropped from the node
// list: a reader gets every address the disk states, plus a sentence saying
// they disagree.
func detectConflicts(nodes []Node) []Conflict {
	var conflicts []Conflict
	byAddress := map[string][]string{}
	for _, n := range nodes {
		if n.Address == "" {
			continue
		}
		byAddress[n.Address] = append(byAddress[n.Address], n.Alias)
		if n.Inline != "" && n.Address != n.Inline {
			conflicts = append(conflicts, Conflict{
				Kind:    conflictAliasInline,
				Subject: n.Alias,
				Detail: "`" + n.Alias + "`: the Host line spells " + n.Inline +
					" while its HostName says " + n.Address +
					" — one stanza, two addresses; the file does not decide which one a connection uses",
			})
		}
	}
	addresses := make([]string, 0, len(byAddress))
	for addr := range byAddress {
		addresses = append(addresses, addr)
	}
	sort.Strings(addresses)
	for _, addr := range addresses {
		aliases := byAddress[addr]
		if len(aliases) < 2 {
			continue
		}
		conflicts = append(conflicts, Conflict{
			Kind:    conflictAddressShared,
			Subject: addr,
			Detail: addr + " is claimed by " + strconv.Itoa(len(aliases)) + " ssh aliases (" +
				strings.Join(aliases, ", ") +
				") — the address alone does NOT identify the machine; settle it with `ssh <alias> hostname` before naming a node",
		})
	}
	return conflicts
}
