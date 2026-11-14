package tools

// peer_repo_path.go — the ONE resolver for "the checkout path of the PEER's
// repository" (Rule 14: a value every delivery leg shares lives in exactly one
// place and is referenced, never mirrored).
//
// THE DEFECT THIS REPLACES (measured 2026-10-25, evidence in the filing run's
// session directory): three delivery legs each carried their OWN constant for
// this path — `gitSyncPeerRepo` (serve pass / `gopi task push`),
// `harvestPeerRepo` (harvest push-back) and `finishCleanupPeerRepo` (close-out
// push) — and all three spelled `~/work/GoPi`. This host declares its delivery
// point somewhere else, under another spelling:
//
//	$ gopi config show | grep -E 'dev_repo|repos '
//	  dev_repo  "peer1:/home/user/work/gopi"
//	  repos     {"/home/user/work/gopi":{...}}
//
// and on the peer those two paths are DIFFERENT REPOSITORIES, not two names for
// one: measured `git -C <p> rev-list --max-parents=0` answers disjoint roots
// (359 commits vs 12025). So every leg pushed into a repo that could never
// accept the branch (non-fast-forward), the mirror ref
// `refs/remotes/gitPushcheck/master` took the OTHER repository's tip, and the
// staleness alarm printed a merge prescription that would have grafted 12025
// unrelated commits into master.
//
// THE PRIORITY, and it is all this resolver knows:
//
//	① the DECLARED DELIVERY POINT — the path half of config.DevRepoConfigured()
//	   (`GOPI_DEV_REPO` > settings `dev_repo`; that precedence has ONE reader,
//	   in internal/config, Rule 2 — this file never reads either key itself);
//	② the settings `repos` table — the per-repo merge/build contract's own keys
//	   are repo paths, and the merger sweeps exactly that list, so the table is
//	   this host's second statement of "my system repo lives here". Used only
//	   when it is UNAMBIGUOUS: the entry for the checkout being delivered from,
//	   or the only entry there is. Several unrelated entries are not a
//	   declaration of one peer path and the arm says so instead of picking;
//	③ peerRepoPathLastResort — the historical spelling, kept as a NAMED
//	   fallback (Rule 1) and ANNOUNCED on stderr whenever it decides the value
//	   (Rule 15: a resolution that fell through to a guess must not read like a
//	   declaration — that silence is what let this defect live).
//
// An empty answer is never produced: the legs need a path, and the last-resort
// arm is a value, not a refusal. What a reader gets instead of silence is the
// sentence saying which arm produced it.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// peerRepoPathLastResort is the LAST-RESORT path half for the peer's repo
	// checkout — the spelling the three deleted per-leg constants all carried
	// (`~/work/GoPi`). It survives ONLY as this one fallback: a host that
	// declares no delivery point and has no usable `repos` entry still has to
	// dial something, and the note peerRepoPathNote prints names it as a
	// fallback every time it is used (Rule 15, never a silent default).
	peerRepoPathLastResort = "~/work/GoPi"

	// peerRepoPathDeclaredChannelLabel names arm ① in the resolution sentence.
	peerRepoPathDeclaredChannelLabel = "the declared delivery point"

	// peerRepoPathReposChannelLabel names arm ② in the resolution sentence.
	peerRepoPathReposChannelLabel = "the settings repos table"

	// peerRepoPathLastResortChannelLabel names arm ③ in the resolution sentence.
	peerRepoPathLastResortChannelLabel = "the LAST-RESORT constant"

	// peerRepoPathDeliverySep separates the ssh alias from the path in a
	// declared delivery point (`<alias>:<path>`). It is the package's existing
	// separator constant, referenced rather than re-spelled (Rule 1b).
	peerRepoPathDeliverySep = peerRemoteTailSep

	// peerRepoPathURLMarker marks a spelling that is a git URL, not a
	// `<host>:<path>` delivery point (an `ssh://` form, or a scp-like
	// `user@host:path`). Its presence means "this value's ':' does not
	// separate a host from a path", so it must not be parsed as one.
	peerRepoPathURLMarker  = "://"
	peerRepoPathUserMarker = "@"

	// peerRepoPathAmbiguousReposFmt is arm ②'s refusal: several table entries
	// and no way to tell which one is the delivered repo. Arity: entry count,
	// the sorted list.
	peerRepoPathAmbiguousReposFmt = "the settings %s table carries %d entries (%s) and none is the checkout being delivered from, so it does not declare ONE peer path"

	// peerRepoPathLastResortNoteFmt is the ANNOUNCEMENT printed once per
	// process when the fallback decides the value. It names the arm, the value,
	// WHY every other arm declined, and what the operator can do about it —
	// the three things the silent constant never said. Arity: reason, value.
	peerRepoPathLastResortNoteFmt = "peer repo path: FALLING BACK to the LAST-RESORT constant %q (%s) — no delivery point is declared in dev_repo and the repos table does not name one either; declare the peer's checkout path with the `%s` setting and every delivery leg will use it\n"

	// peerRepoPathDeclaredNoteFmt is the positive half: the arm that DID decide
	// the value says so when it is asked to explain itself (Rule 15). Arity:
	// path, channel, the declaration it was parsed out of.
	peerRepoPathDeclaredNoteFmt = "peer repo path %q resolved from %s (declared as %q)"
)

// peerRepoPathResolution is the resolver's whole answer: the path the legs
// carry, the arm that produced it, and the sentence saying so (Rule 15: the
// resolution and its provenance travel together, so a caller can print the
// finding without a second lookup).
type peerRepoPathResolution struct {
	// Path is the path half of an `<alias>:<path>` target. Never empty.
	Path string
	// Channel names the arm: the declared delivery point, the repos table, or
	// the LAST-RESORT constant.
	Channel string
	// Reason is the one sentence naming the declaration this value came from,
	// or — on the fallback arm — why every declaration declined.
	Reason string
}

// peerRepoPathNoteOnce dedupes the stderr announcement. The resolver is
// niladic and several faces ask it per line (a pass renders a target per step,
// the close-out chain per delivery), so a note printed per call would bury the
// log it is meant to inform. One note per (arm, path) per process is the
// shape that carries the finding without drowning it.
var (
	peerRepoPathNoteMu   sync.Mutex
	peerRepoPathNotedSet = map[string]bool{}
)

// peerRepoPathNote prints the resolution note once per (channel, path) pair per
// process. It writes to STDERR: the pass's stdout is a contract several faces
// and tests match on, and a resolution note is a diagnostic, not a report line.
func peerRepoPathNote(res peerRepoPathResolution) {
	key := res.Channel + "\x00" + res.Path
	peerRepoPathNoteMu.Lock()
	defer peerRepoPathNoteMu.Unlock()
	if peerRepoPathNotedSet[key] {
		return
	}
	peerRepoPathNotedSet[key] = true
	fmt.Fprint(os.Stderr, peerRepoPathNoteText(res))
}

// peerRepoPathNoteText is the ONE rendering of the resolution note, split out
// from the emission so a test asserts the sentence a reader actually gets
// rather than the format string behind it (Rule 1b: one spelling, referenced —
// and a test that re-spells a format string asserts nothing about the output).
func peerRepoPathNoteText(res peerRepoPathResolution) string {
	if res.Channel == peerRepoPathLastResortChannelLabel {
		return fmt.Sprintf(peerRepoPathLastResortNoteFmt, res.Path, res.Reason, config.DevRepoSettingsKey)
	}
	return fmt.Sprintf(peerRepoPathDeclaredNoteFmt+"\n", res.Path, res.Channel, res.Reason)
}

// peerRepoPath resolves the peer checkout path for the process itself (the
// niladic face every leg that has no subject repository calls).
func peerRepoPath() string {
	return peerRepoPathResolve("").Path
}

// peerRepoPathForRepo resolves it for a KNOWN checkout: `repo` is the
// repository being delivered from, and it lets arm ② recognise the `repos`
// table entry that describes that checkout instead of treating the table as a
// flat list. An empty `repo` is the niladic face.
func peerRepoPathForRepo(repo string) string {
	return peerRepoPathResolve(repo).Path
}

// peerRepoPathResolve is the ONE implementation behind both faces above.
func peerRepoPathResolve(repo string) peerRepoPathResolution {
	declined := make([]string, 0, 2)
	// ① the declared delivery point (env > settings dev_repo, read in
	// internal/config — Rule 2: this package never reads that knob itself).
	declared, channel, why := peerRepoPathFromDeclaredDeliveryPoint()
	if declared != "" {
		res := peerRepoPathResolution{Path: declared, Channel: peerRepoPathDeclaredChannelLabel, Reason: channel}
		peerRepoPathNote(res)
		return res
	}
	if why != "" {
		declined = append(declined, why)
	}
	// ② the repos table — unambiguous entries only.
	fromTable, why := peerRepoPathFromReposTable(repo)
	if fromTable != "" {
		res := peerRepoPathResolution{Path: fromTable, Channel: peerRepoPathReposChannelLabel, Reason: why}
		peerRepoPathNote(res)
		return res
	}
	if why != "" {
		declined = append(declined, why)
	}
	// ③ the LAST-RESORT constant, ANNOUNCED.
	reason := "no declaration found"
	if len(declined) > 0 {
		reason = strings.Join(declined, "; ")
	}
	res := peerRepoPathResolution{Path: peerRepoPathLastResort, Channel: peerRepoPathLastResortChannelLabel, Reason: reason}
	peerRepoPathNote(res)
	return res
}

// peerRepoPathFromDeclaredDeliveryPoint answers arm ①'s path half: the value
// of config.DevRepoConfigured() up to (and excluding) its `<alias>:` prefix.
//
// The second return value names the CHANNEL the declaration came from
// (`GOPI_DEV_REPO`, `settings dev_repo`), and the third is the reason the arm
// declined — which is a non-empty sentence precisely when a delivery point WAS
// declared but cannot serve as a repo path (a git URL, a value with an empty
// path half). A broken settings document is reported the same way rather than
// silently dropping to the next arm (Rule 15).
func peerRepoPathFromDeclaredDeliveryPoint() (string, string, string) {
	declared, source, err := config.DeliveryRepoDeclared()
	if err != nil {
		return "", "", fmt.Sprintf("%s could not be read (%v)", config.DevRepoSettingsKey, err)
	}
	declared = strings.TrimSpace(declared)
	if declared == "" {
		return "", "", fmt.Sprintf("%s declares no delivery point", config.DevRepoSettingsKey)
	}
	// A URL is not a `<host>:<path>` delivery point: its ':' belongs to the
	// scheme or to the scp shorthand, so the half after it is a repo spec for
	// the PEER's Git to resolve, not a filesystem path this process may hand to
	// a delivery leg (the same refusal the delivery-target resolver makes, for
	// the same measured reason).
	if strings.Contains(declared, peerRepoPathURLMarker) || strings.Contains(declared, peerRepoPathUserMarker) {
		return "", "", fmt.Sprintf("%s declares %q, which is a git URL rather than a `<alias>:<path>` delivery point", config.DevRepoSettingsKey, declared)
	}
	path := declared
	if head, tail, ok := strings.Cut(declared, peerRepoPathDeliverySep); ok {
		path = tail
		if strings.TrimSpace(head) == "" || strings.TrimSpace(tail) == "" {
			return "", "", fmt.Sprintf("%s declares %q, whose path half is empty", config.DevRepoSettingsKey, declared)
		}
	}
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	if path == "" {
		return "", "", fmt.Sprintf("%s declares %q, whose path half is empty", config.DevRepoSettingsKey, declared)
	}
	return path, source, ""
}

// peerRepoPathFromReposTable answers arm ②: the `repos` table's own key for the
// checkout being delivered from, or its only key. Several unrelated keys are an
// AMBIGUOUS answer and the arm declines by name rather than picking one (a
// silent pick would be a coin flip deciding a delivery destination).
func peerRepoPathFromReposTable(repo string) (string, string) {
	table, err := config.DeliveryReposTable()
	if err != nil {
		return "", fmt.Sprintf("the %s table could not be read (%v)", config.ReposSettingsKey, err)
	}
	paths := make([]string, 0, len(table))
	for p := range table {
		if strings.TrimSpace(p) == "" {
			continue
		}
		paths = append(paths, strings.TrimSpace(p))
	}
	if len(paths) == 0 {
		return "", fmt.Sprintf("the %s table is empty", config.ReposSettingsKey)
	}
	// The SUBJECT arm: the entry describing the checkout being delivered from.
	// Compared on resolved paths (a symlinked or relatively-spelled spelling of
	// the same directory must still match), and only when a subject was named.
	if subject := strings.TrimSpace(repo); subject != "" {
		if real, err := filepath.EvalSymlinks(subject); err == nil {
			subject = real
		}
		for _, p := range paths {
			candidate := p
			if real, err := filepath.EvalSymlinks(p); err == nil {
				candidate = real
			}
			if candidate == subject {
				return p, fmt.Sprintf("%s entry %q describes the checkout being delivered from", config.ReposSettingsKey, p)
			}
		}
	}
	if len(paths) == 1 {
		return paths[0], fmt.Sprintf("%s has exactly one entry, %q", config.ReposSettingsKey, paths[0])
	}
	return "", fmt.Sprintf(peerRepoPathAmbiguousReposFmt, config.ReposSettingsKey, len(paths), strings.Join(sortedCopy(paths), ", "))
}

// sortedCopy returns a sorted copy so a sentence's list is deterministic (a
// map's iteration order is not, and a diagnostic that reorders itself between
// two identical failures reads as two different findings).
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
