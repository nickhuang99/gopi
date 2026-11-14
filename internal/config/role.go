// Node identity (bug041): settings.json declares WHICH node this process
// runs on ("role": "server"|"client") and, for clients, where its sync
// deltas go ("sync_to"). The banner makes that identity visible at startup
// so an agent session never has to GUESS its node from the hostname.
package config

import (
	"fmt"
	"os"
	"strings"
)

// NodeBanner renders the one-line node-identity banner (bug041): the
// node's declared role from settings.json, printed at startup so a Bond
// session knows WHICH node it is running on ("[gopi] node: hp (client→peer1)")
// before it reasons about the topology. Empty role → no banner line (unset
// role means the topology is undeclared; never guess from the hostname).
// Format: server → "(server)"; client with sync_to → "(client→peer1)".
func NodeBanner(role, syncTo string) string {
	if role == "" {
		return ""
	}
	if syncTo != "" {
		return fmt.Sprintf("[gopi] node: %s (%s→%s)\n", nodeName(), role, syncTo)
	}
	return fmt.Sprintf("[gopi] node: %s (%s)\n", nodeName(), role)
}

// nodeName returns a display name for this machine's declared identity.
// Rule: topology is DECLARED, not discovered — this is a DISPLAY label only
// (lowercased hostname for the banner), never a behavioral input; behavior
// branches read cfg.Role, never this string.
func nodeName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return strings.ToLower(h)
}
