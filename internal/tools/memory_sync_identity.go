// Package tools — memory_sync_identity.go: the PEER IDENTITY DIGEST that makes
// the asset channel incremental.
//
// The problem this closes (measured 2026-11-15 on a live pair): `SyncExport`
// has a `SinceUnix` filter, but every production call site passed only
// `DBPath`, so each session exported the WHOLE local knowledge base and the
// peer then probed every row one question at a time. On a converged pair that
// is ~30k rows shipped to discover a 94-row difference.
//
// WHY THE INCREMENT IS AN IDENTITY SET AND NOT A TIME WATERMARK — measured,
// and the measurement is the whole reason this file looks the way it does:
// a peer's `distilled_at` is stamped by the RECEIVING node at import time
// (`memory.go`'s insert path writes `time.Now().Unix()`), and the peer also
// writes rows of its OWN. So the peer's full-table `max(distilled_at)` is
// raised by rows we never sent: on the measured pair it sat at 21:12:29 while
// our newest row the peer actually held was 21:09:51, and filtering with it
// would have dropped 72 of the 94 genuinely-missing rows — 23% recall, i.e. a
// filter that "saves 99.9%" precisely because it loses the work.
//
// The identity digest has no such failure mode. It is the SAME key the import
// probe already uses (`substr(question,1,?)` with `dedupQuestionRunes`), so
// "the peer already has this row" means exactly what it has always meant, and
// a row the peer does not hold can never be filtered out. Nothing here is
// persisted (Rule 35): the digest is fetched from the live peer on every run
// and discarded when the run ends.
package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// AssetIdentityDigestBytes is the width of one digest entry on the wire: the
// first 8 bytes of sha256(identity key). 8 bytes is not a security boundary —
// it is a collision budget: at 30k identities the birthday probability is
// ~2.4e-11, and a collision only means one row is treated as already-present
// by a peer that holds a different row with the same prefix, which the import
// probe then catches (the probe is the authority; the digest only sizes the
// patch).
const AssetIdentityDigestBytes = 8

// assetIdentityDigestEncoding is base64's raw URL alphabet: fixed width per
// entry, no character that needs shell quoting, so the peer can print one
// entry per line and the initiator can read the stream verbatim.
var assetIdentityDigestEncoding = base64.RawURLEncoding

// AssetIdentityKey returns the identity of a knowledge row: the same
// question[:dedupQuestionRunes] prefix the store's exact-dedup key and the
// import probe use. Two rows that the store would dedup to one row share this
// key, which is what makes "the peer has it" and "we should not send it" the
// same statement.
func AssetIdentityKey(question string) string {
	return truncateRunes(question, dedupQuestionRunes)
}

// AssetIdentityDigest folds one identity key into its wire form.
func AssetIdentityDigest(question string) string {
	sum := sha256.Sum256([]byte(AssetIdentityKey(question)))
	return assetIdentityDigestEncoding.EncodeToString(sum[:AssetIdentityDigestBytes])
}

// AssetIdentitySet is the peer's known-identity set, as read off the wire.
type AssetIdentitySet struct {
	// Digests holds every entry the peer reported.
	Digests map[string]struct{}
	// Lines is how many entries the peer printed (a count that disagrees with
	// len(Digests) means the stream carried a duplicate, which is harmless but
	// worth stating rather than hiding).
	Lines int
}

// Count is the number of distinct identities the peer reported.
func (s *AssetIdentitySet) Count() int {
	if s == nil {
		return 0
	}
	return len(s.Digests)
}

// Has reports whether the peer already holds this question's identity.
func (s *AssetIdentitySet) Has(question string) bool {
	if s == nil || len(s.Digests) == 0 {
		return false
	}
	_, ok := s.Digests[AssetIdentityDigest(question)]
	return ok
}

// ParseAssetIdentityDigest reads the peer's line-oriented digest stream.
// A malformed line is an ERROR, never a silently-dropped entry: dropping one
// entry makes the peer look emptier than it is, which re-sends rows (harmless)
// — but dropping a line we failed to parse could also be a truncated stream,
// and a truncated digest read as complete would filter out rows the peer does
// not hold. Refusing is the only reading that cannot lose data.
func ParseAssetIdentityDigest(stream []byte) (*AssetIdentitySet, error) {
	set := &AssetIdentitySet{Digests: make(map[string]struct{})}
	for i, line := range strings.Split(string(stream), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) != assetDigestWireLen() {
			return nil, fmt.Errorf("identity digest: line %d is %d byte(s), want %d (%q)",
				i+1, len(line), assetDigestWireLen(), line)
		}
		if _, err := assetIdentityDigestEncoding.DecodeString(line); err != nil {
			return nil, fmt.Errorf("identity digest: line %d: %w", i+1, err)
		}
		set.Lines++
		set.Digests[line] = struct{}{}
	}
	return set, nil
}

// assetDigestWireLen is the fixed width of one encoded entry — computed from
// the encoding rather than typed as a literal (Rule 1/13).
func assetDigestWireLen() int {
	raw := make([]byte, AssetIdentityDigestBytes)
	return len(assetIdentityDigestEncoding.EncodeToString(raw))
}

// AssetIdentitySetFromWire builds the exclude-set from a patch's carried
// digest list (the initiator's known identities, as the peer reads them).
// An EMPTY or absent list yields a nil set, which callers treat as "no
// filtering" — the pre-existing full-export behaviour.
func AssetIdentitySetFromWire(digests []string) *AssetIdentitySet {
	if len(digests) == 0 {
		return nil
	}
	set := &AssetIdentitySet{Digests: make(map[string]struct{}, len(digests))}
	for _, d := range digests {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		set.Lines++
		set.Digests[d] = struct{}{}
	}
	if len(set.Digests) == 0 {
		return nil
	}
	return set
}

// WireAssetIdentityDigest renders a digest set as the list a patch carries.
// Sorted so one run's payload is reproducible.
func WireAssetIdentityDigest(set *AssetIdentitySet) []string {
	if set == nil || len(set.Digests) == 0 {
		return nil
	}
	out := make([]string, 0, len(set.Digests))
	for d := range set.Digests {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// SortedAssetIdentityDigest renders a digest set as the peer's own line
// stream. Both sides share this so a locally-composed digest and an ssh'd one
// are byte-identical (that equality is what the offline test asserts).
func SortedAssetIdentityDigest(questions []string) string {
	digests := make([]string, 0, len(questions))
	for _, q := range questions {
		digests = append(digests, AssetIdentityDigest(q))
	}
	sort.Strings(digests)
	var b strings.Builder
	for _, d := range digests {
		b.WriteString(d)
		b.WriteString("\n")
	}
	return b.String()
}

// AssetPeerIdentityProbeFlag is the PEER-SIDE flag: an initiator ssh's
// `gopi memory sync --dump-asset-identities`, which prints this node's
// question-identity digest on stdout and imports nothing. It is a probe, not a
// mode — it never writes, never reads stdin, and never opens an ssh leg of its
// own.
const AssetPeerIdentityProbeFlag = "dump-asset-identities"

// assetIdentityProbeRemoteArgv is the ONE ssh argv element that asks the peer
// for its digest (Rule 13: composed from constants, never re-typed).
func assetIdentityProbeRemoteArgv() string {
	return syncRemoteGopi + " " + CliVerbMemoryGroup + " " + MemorySyncVerb +
		" " + syncFlagPrefixRow + AssetPeerIdentityProbeFlag
}

// assetIdentityProbeTimeout bounds the probe leg. It is deliberately much
// shorter than the session bound (`MemorySyncTimeoutEnv`, default 900s): a
// probe that cannot answer in seconds is a probe that is not going to answer,
// and the degraded path (full export) costs one round trip, not a hung run.
const assetIdentityProbeTimeout = 120 * time.Second

// assetIdentityProbeHook is the test seam (the established
// syncSSHExchangeHook pattern): tests answer the probe in-process instead of
// ssh'ing, and setting it back to nil restores the real leg.
var assetIdentityProbeHook func(peer string) ([]byte, error)

// MemoryAssetsPeerIdentities fetches the peer's known-identity digest.
//
// A failure here is NOT fatal to the session: the caller degrades to a full
// export and says so in one line (Rule 15 — degrade loudly). The error is
// returned so the caller can print WHY it degraded; it is never swallowed.
func MemoryAssetsPeerIdentities(peer string, cliPeers []string, dbPath string) (*AssetIdentitySet, error) {
	if err := syncAssetsPeerWhitelisted(peer, cliPeers); err != nil {
		return nil, err
	}
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("memsync identities: %w", err)
	}
	if assetIdentityProbeHook != nil {
		stream, err := assetIdentityProbeHook(peer)
		if err != nil {
			return nil, err
		}
		return ParseAssetIdentityDigest(stream)
	}
	stream, err := assetIdentityProbeSSH(peer)
	if err != nil {
		return nil, err
	}
	return ParseAssetIdentityDigest(stream)
}

// assetIdentityProbeSSH is the real probe leg: one ssh, no stdin body, the
// peer's digest on stdout.
func assetIdentityProbeSSH(peer string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), assetIdentityProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+syncSSHConnectTimeout,
		peer,
		assetIdentityProbeRemoteArgv())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// The same cross-node version skew the serve leg names: a peer whose
		// gopi predates this flag answers with an unknown-flag error, and the
		// operator needs to be told to upgrade it, not to read a bare exit
		// status.
		if txt := strings.TrimSpace(stderr.String()); txt != "" {
			if syncAssetsStalePeerHint(txt) || strings.Contains(txt, AssetPeerIdentityProbeFlag) {
				return nil, fmt.Errorf("identity probe on %q: %w (stderr: %s)\n%s",
					peer, err, txt, syncAssetsUpgradeHint(peer))
			}
			return nil, fmt.Errorf("identity probe on %q: %w (stderr: %s)", peer, err, txt)
		}
		return nil, fmt.Errorf("identity probe on %q: %w", peer, err)
	}
	return stdout.Bytes(), nil
}

// AssetIdentitiesDegradedLine is the ONE sentence the operator sees when the
// probe could not run and the session therefore streams the whole local store.
// It names the reason and what to do, and it is printed on every degrading run
// (never a silent asymmetry, Rule 15).
func AssetIdentitiesDegradedLine(peer string, cause error) string {
	return fmt.Sprintf("increment unavailable from %q (%v) — exporting the FULL local store this run; "+
		"upgrade %q's gopi to get the identity digest (`ssh %s \"cd <repo> && git pull && go build -o bin/gopi ./cmd/agent\"`)",
		peer, cause, peer, peer)
}

// LocalAssetIdentityDigest renders THIS node's known-identity digest — the
// answer to the peer's probe. It reads the same rows the export would send
// (non-node local rows with a question and an answer), so the digest and the
// export agree on what "a row we hold" means.
//
// A DB that cannot be read is an ERROR, never an empty digest: an empty digest
// tells the initiator "the peer holds nothing", which re-sends everything
// (merely wasteful) — but it would also hide a broken peer, and Rule 15
// forbids a silent failure dressed as an answer.
func LocalAssetIdentityDigest(dbPath string) (string, int, error) {
	if dbPath == "" {
		dbPath = memoryDBPath()
	}
	ctx := context.Background()
	db, err := openStoreDB(ctx, dbPath)
	if err != nil {
		return "", 0, fmt.Errorf("identity digest: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(question,'') FROM distilled_qa
		 WHERE COALESCE(question,'') != '' AND COALESCE(answer,'') != ''`)
	if err != nil {
		return "", 0, fmt.Errorf("identity digest: %w", err)
	}
	defer rows.Close()
	var questions []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return "", 0, fmt.Errorf("identity digest: scan: %w", err)
		}
		questions = append(questions, q)
	}
	if err := rows.Err(); err != nil {
		return "", 0, fmt.Errorf("identity digest: %w", err)
	}
	return SortedAssetIdentityDigest(questions), len(questions), nil
}
