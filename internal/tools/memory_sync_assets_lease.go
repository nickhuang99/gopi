package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/runlock"
)

// The asset import lease — ONE writer per (peer, database).
//
// WHY this exists (measured defect): two `gopi memory sync --serve-assets`
// processes for the SAME peer and the SAME ledger used to run concurrently and
// neither could see the other. The only probe the import had,
// syncAssetsQuestionExistsQ, answers "is this row already here" — never "is
// somebody else importing this batch right now" — so the second copy re-asked
// the question for every already-imported row: 1.18 TB read, 0 rows written,
// the WAL write lock held end to end, every core busy for over an hour.
//
// The lease is a KERNEL lock (internal/runlock's flock), not a state file with
// a pid in it:
//
//   - it needs no expiry, no heartbeat and no reaper, because the kernel drops
//     it when the holder DIES — the failure mode a pid file cannot survive
//     (measured 2026-11-15: every interrupted initiator left the peer's
//     serve-assets running as an orphan for a full hour);
//   - it adds no new storage (Rule 35): the lock file lives beside the ledger
//     it guards, in the same directory the ledger and its WAL already occupy;
//   - it is the SAME primitive the run-directory lock already uses, so there is
//     one flock implementation in this tree, not two.
//
// The lease is keyed on the PEER plus the resolved database path: two imports of
// DIFFERENT peers' rows into one ledger do not collide, and one peer's import
// into two different ledgers does not collide either.

const (
	// memoryAssetLeaseDirPrefix names the directory that holds one lock file
	// per (peer, database) pair, created inside the ledger's own directory.
	memoryAssetLeaseDirPrefix = "memsync-import-lease-"
	// memoryAssetLeaseKeyHashLen is how many hex characters of the database
	// path's digest go into the lease directory name. The peer name is carried
	// in readable form beside it, so an operator looking at the directory knows
	// WHICH peer without opening the file; the digest disambiguates two
	// databases of the same name in different directories.
	memoryAssetLeaseKeyHashLen = 16
	// memoryAssetLeasePeerSep joins the readable peer name to the digest.
	memoryAssetLeasePeerSep = "-"
	// memoryAssetLeaseUnnamedPeer labels a patch that named no node, so a lock
	// directory is never named after an empty string.
	memoryAssetLeaseUnnamedPeer = "unnamed-peer"
)

// memoryAssetImportLease is the held lease; a nil one means "no lease was
// taken" (the caller must still be able to report that).
type memoryAssetImportLease struct {
	handle *runlock.Handle
	dir    string
}

// memoryAssetLeaseDir resolves the lease directory for one (peer, database)
// pair. It is deterministic: the same peer and the same ledger resolve to the
// same directory in every process, which is the whole mechanism.
func memoryAssetLeaseDir(dbPath, peer string) string {
	peer = strings.TrimSpace(peer)
	if peer == "" {
		peer = memoryAssetLeaseUnnamedPeer
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dbPath)))
	digest := hex.EncodeToString(sum[:])
	if len(digest) > memoryAssetLeaseKeyHashLen {
		digest = digest[:memoryAssetLeaseKeyHashLen]
	}
	name := memoryAssetLeaseDirPrefix + memoryAssetLeaseSafeName(peer) + memoryAssetLeasePeerSep + digest
	return filepath.Join(filepath.Dir(dbPath), name)
}

// memoryAssetLeaseSafeName keeps a peer name usable as a path component: any
// character that is not alphanumeric, a dash or an underscore becomes an
// underscore. A peer is an ssh alias, so this normally changes nothing.
func memoryAssetLeaseSafeName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// memoryAssetTryLease takes the lease if nobody holds it. It returns the held
// lease, or nil plus a NON-NIL *runlock.HeldError naming the live holder. An I/O
// failure is returned as its own error — a refusal that says "busy" must never
// stand in for one that says "I could not tell" (Rule 15).
func memoryAssetTryLease(dbPath, peer string) (*memoryAssetImportLease, error) {
	dir := memoryAssetLeaseDir(dbPath, peer)
	h, err := runlock.Lock(dir)
	if err != nil {
		var held *runlock.HeldError
		if errors.As(err, &held) {
			return nil, held
		}
		return nil, fmt.Errorf("memsync assets lease %s: %w", dir, err)
	}
	return &memoryAssetImportLease{handle: h, dir: dir}, nil
}

// release drops the lease. A failure here is reported, never swallowed: a lock
// the caller believes it released but did not would make every later import
// wait a full stale interval for nothing.
func (l *memoryAssetImportLease) release() error {
	if l == nil || l.handle == nil {
		return nil
	}
	if err := l.handle.Release(); err != nil {
		return fmt.Errorf("memsync assets lease %s: release: %w", l.dir, err)
	}
	l.handle = nil
	return nil
}

// memoryAssetLeaseSkipReason is the sentence a skipped import reports. It names
// the row count AND the owner, because "0 rows written" alone is ambiguous
// between "nothing to do" and "somebody else is doing it" — and only the second
// one means the caller should wait rather than assume the corpus is clean.
func memoryAssetLeaseSkipReason(rows int, owner string) string {
	return fmt.Sprintf("memsync assets import: skipped %d row(s) — this batch is already being imported by %s (import lease held; no row was probed or written)", rows, owner)
}

// memoryAssetLeaseOwner renders the holder of a busy lease for a human: the
// host, pid and acquisition time the holder itself stamped, or an explicit
// statement that the stamp was unreadable. It never invents a name (Rule 15).
func memoryAssetLeaseOwner(err error) string {
	var held *runlock.HeldError
	if !errors.As(err, &held) {
		return ""
	}
	if held.Holder.PID != 0 {
		host := held.Holder.Host
		if host == "" {
			host = memoryAssetLeaseUnnamedPeer
		}
		return fmt.Sprintf("pid %d on %s since %s", held.Holder.PID, host, held.Holder.AcquiredAt)
	}
	if held.ReadErr != nil {
		return fmt.Sprintf("an unreadable holder stamp (%v)", held.ReadErr)
	}
	return "an unnamed holder"
}
