package main

// WHAT "one daemon per profile" MEANS — in ONE place (epic
// daemon-scope-per-profile, 2026-09-16).
//
// A gopi PROFILE is its data root: $GOPI_HOME/.gopi, or ~/.gopi when GOPI_HOME
// is unset (internal/config.GopiDir — the same resolution every other gopi data
// path uses, so "this profile's daemon" is answerable from one identity).
//
// THE PROPERTY THIS FILE EXISTS FOR: the transient systemd scope unit a
// `--daemon` start asks for is a function of the PROFILE, so
//
//   - one profile = one unit name (a second daemon of the same profile is the
//     FLOCK's business, never a second scope unit),
//   - two profiles on one host = two unit names, and
//   - a long-lived production daemon can never occupy the name a test profile's
//     launcher asks for (measured before this change: the launcher's PID was
//     the suffix, and a test harness runs every case of one package under ONE
//     launcher pid — see devdoc/daemon-scope-per-profile.md).
//
// The identity is HASHED, not spelled: the readable tail alone is not unique
// (two profiles can both be named ".../gopi"), and the full path contains
// characters systemd refuses in a unit name (it is also unbounded in length).

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
)

const (
	// daemonUnitPartSep joins the parts of a daemon UNIT NAME (the component,
	// the profile's readable tail, its hash) and of a cgroupfs scope directory
	// name. One separator, referenced by every site that builds such a name: a
	// second spelling is how two names that must agree stop matching (Rules
	// 1b/13).
	daemonUnitPartSep = "-"
	// daemonProfileHashBytes is how many leading SHA-256 bytes of the profile
	// identity go into the unit name. 6 bytes = 12 hex characters = 48 bits: a
	// collision needs the birthday bound (2^24 ≈ 17 million profiles on one
	// host), while the name stays short enough to read in `systemctl --user
	// list-units`.
	daemonProfileHashBytes = 6
	// daemonProfileTailMaxLen bounds the READABLE part of the profile tag. The
	// readable tail is for the operator's eyes only (the hash carries the
	// identity), so it is capped rather than allowed to push the unit name
	// toward systemd's limit.
	daemonProfileTailMaxLen = 24
	// daemonProfileUnknownMark is the tag used when the profile's identity
	// CANNOT be resolved (no home directory, an unusable GOPI_HOME). The start
	// is still allowed — a daemon whose profile cannot be named must not be a
	// daemon that does not start — but the name must not silently pretend to
	// carry an identity it does not have (Rule 15). The launcher's pid is
	// appended to this mark so two such starts still do not collide with each
	// other.
	daemonProfileUnknownMark = "nohome"
	// daemonUnitNameMaxLen is systemd's own limit on a unit name (the kernel's
	// 255-byte file-name limit). The name built here can NEVER reach it — the
	// hash is fixed-width and the readable half is capped (see
	// daemonProfileHashBytes / daemonProfileTailMaxLen) — so it is stated as a
	// documented BOUND rather than guarded by an unreachable check: the length
	// is pinned by TestDaemonProfileUnitTagIsUniqueAndReadable, which fails if a
	// future change lets the name grow without bound.
	daemonUnitNameMaxLen = 255
	// daemonUnitNameSafeFallback replaces a sane name whose every character was
	// rejected by the unit-name charset below. A NAME IS REQUIRED (systemd
	// refuses a leading "-" and an empty name), so one is provided rather than
	// letting an unsanitised path reach argv.
	daemonUnitNameSafeFallback = "profile"
)

// daemonProfileIdentity resolves THIS profile's canonical identity — the value
// the unit name is derived from. ok=false means it could not be resolved (the
// caller must NOT invent one: two profiles sharing an invented identity would
// collide, which is the whole defect being fixed).
func daemonProfileIdentity() (string, bool) {
	gd, err := config.GopiDir()
	if err != nil || strings.TrimSpace(gd) == "" {
		return "", false
	}
	abs, err := filepath.Abs(gd)
	if err != nil {
		return "", false
	}
	// CANONICALISE so two spellings of one profile are ONE name: a symlinked
	// profile directory (or a symlinked ancestor of it) must not produce a
	// second unit. The directory usually does not exist yet at START time, so
	// the deepest EXISTING ancestor is resolved and the remainder re-appended —
	// EvalSymlinks on the full path would simply fail.
	return daemonProfileCanonicalPath(abs), true
}

// daemonProfileCanonicalPath resolves path's symlinks, tolerating a path whose
// leaf (or several leaves) do not exist yet.
func daemonProfileCanonicalPath(path string) string {
	remainder := ""
	for p := path; ; {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(p)
		if parent == p {
			// Reached the filesystem root without resolving anything: the path
			// is returned as given rather than guessed at.
			return path
		}
		remainder = filepath.Join(filepath.Base(p), remainder)
		p = parent
	}
}

// daemonProfileUnitTag is the profile's token inside a unit name:
// "<readable tail>-<hex of the identity's first daemonProfileHashBytes bytes>".
// The two halves are complementary on purpose — the tail is what an operator
// recognises, the hash is what makes it unique (the tail alone is not: two
// profiles can both be named "gopi").
func daemonProfileUnitTag(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return daemonProfileReadableTail(identity) + daemonUnitPartSep +
		hex.EncodeToString(sum[:daemonProfileHashBytes])
}

// daemonProfileReadableTail is the human half of the tag: the profile's own
// directory name, or — for the default profile, whose directory is the shared
// ".gopi" every such profile has — the HOME directory it lives in.
func daemonProfileReadableTail(identity string) string {
	base := filepath.Base(identity)
	if base == "" || base == "." || base == string(filepath.Separator) || base == config.ProfileDirName {
		base = filepath.Base(filepath.Dir(identity))
	}
	return daemonUnitNameSafe(base)
}

// daemonUnitNameSafe renders s as a token systemd accepts inside a unit name:
// lowercased, every character outside [a-z0-9._] replaced by the separator, no
// leading separator (systemd refuses a name that starts with one), and capped
// at daemonProfileTailMaxLen. An empty result falls back to
// daemonUnitNameSafeFallback — a name is required, so one is produced.
func daemonUnitNameSafe(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteString(daemonUnitPartSep)
		}
	}
	out := strings.Trim(b.String(), daemonUnitPartSep)
	if len(out) > daemonProfileTailMaxLen {
		out = out[:daemonProfileTailMaxLen]
	}
	// The cap can leave a trailing separator (`-`): trimmed again so the name
	// never ends in one.
	out = strings.Trim(out, daemonUnitPartSep)
	if out == "" {
		return daemonUnitNameSafeFallback
	}
	return out
}

// daemonProfileUnitTagNow is daemonProfileUnitTag for THIS process's profile.
// An unresolvable profile yields daemonProfileUnknownMark plus this launcher's
// pid, so the name is still unique among concurrent starts while SAYING that no
// identity was resolved.
func daemonProfileUnitTagNow() string {
	identity, ok := daemonProfileIdentity()
	if !ok {
		return daemonProfileUnknownMark + daemonUnitPartSep + strconv.Itoa(os.Getpid())
	}
	return daemonProfileUnitTag(identity)
}
