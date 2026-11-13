package main

// cli_web_files_cache.go — the PERSISTENT half of the Files API cache: the
// content→file_id map a `gopi web` process leaves behind for the NEXT process.
//
// WHY IT EXISTS (measured, not assumed). The cache in cli_web_files.go is
// process memory. Every `gopi web` restart therefore forgot which bytes were
// already uploaded, and the SAME PNG came back with a NEW id — measured on the
// live account: 2fc2bed7… (one run) and 82d27704… (the next run) are two
// objects holding identical bytes. Nothing reclaims them: gopi uploads with no
// expiry and implements no deletion, and the account is capped at 25 GiB /
// 10 000 files. So a restart is not a cosmetic loss of a cache — it is a
// permanent, silent consumption of an account slot per picture.
//
// WHY A FILE AND NOT A SERVER-SIDE ANSWER (the two alternatives, both refused
// after being measured on 2026-11-07):
//
//  1. `expires_after` on the upload — ACCEPTED (HTTP 200) and USELESS here: the
//     response object carries no expiry field at all, so nothing tells us when
//     an object dies; and expiry is actively WRONG for this carrier, because
//     `s.history` keeps the file reference for the whole conversation — a turn
//     an hour later would reference an object that no longer exists. Trading a
//     leak for a broken turn is not a trade.
//  2. `GET {base}/files` (measured: 200 with the beta header) — it lists
//     id / filename / created_at / mime_type and NO CONTENT DIGEST, so it can
//     never answer "is THIS picture already uploaded". Even filtered by
//     filename it answers with every `web-turn.png` the account holds.
//
// So the identity of the bytes is knowledge only the CLIENT has, and the only
// place it can survive a restart is the profile on disk. That is the whole
// dependency this file adds, and it answers Rule 35's four questions:
//
//   - WHAT BREAKS WITHOUT IT: the re-upload above, once per restart, with no
//     reclamation — an account that fills up and then refuses every picture.
//   - WHY NOT IN-PROCESS: a process cannot outlive its own exit. The measured
//     alternatives are refused above.
//   - WHO RECLAIMS IT, AND HOW: nothing has to. It is a BOUNDED TABLE, not a
//     log: at most webFilesCacheMax entries (~30 KB), rewritten in place by an
//     atomic rename, so it cannot grow without limit. DELETING THE FILE IS
//     SAFE AND IS THE REPAIR — the next run re-uploads and starts a fresh
//     table. It lives under the profile (<gopi dir>/web-files-cache.json), so
//     a sandbox has its own and a `rm -rf` of the profile takes it with it.
//   - WHAT THE NEXT READER SEES: a versioned JSON object whose `entries` map
//     is exactly the in-memory cache, and a corrupt/unreadable file is
//     REPORTED on stderr and then ignored (Rule 15) — it never fails a turn,
//     because a cache miss costs one upload and a hard failure costs the turn.
//
// It is therefore NOT a second source of truth: cli_web_files.go's map is
// authoritative for the process, and this file is its write-through echo.
//
// # RECENCY IS PERSISTED, AND WHY BOTH HALVES OF THE OLD DEFECT LIVED HERE
//
// The table is bounded, so it must decide WHICH entry survives a full table.
// The policy is LRU (least-recently-USED first, see cli_web_files.go's put) and
// it needs an ordering, so every entry carries the logical tick at which it was
// last admitted or last looked up. Two consequences a reader must not undo:
//
//   - The ORDER LIVES ON DISK. Without it, a restarted process would know the
//     ids but not which of them are old, so its very first eviction would pick
//     arbitrarily — the same picture could be evicted immediately after being
//     admitted, re-uploaded, and evicted again.
//   - THE LOAD PATH NEVER TRUNCATES AT RANDOM. The previous version filled the
//     in-memory map by ranging over a Go map and breaking at webFilesCacheMax
//     — and map iteration order is deliberately randomised, so on an oversize
//     document WHICH 256 entries survived differed between two starts of the
//     same process. A picture that had been cached could silently vanish and be
//     re-uploaded (one account slot each time). The load path below therefore
//     keeps the MOST RECENTLY USED entries, with the KEY as a deterministic
//     tie-break, and reports how many entries it had to drop (Rule 15).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nick/gopi/internal/config"
)

// Named constants — Project Rule 1.
const (
	// webFilesCacheFileName is the table's file name under the gopi profile
	// directory. Named here so the reader, the writer and any repair note spell
	// it once (Rule 13).
	webFilesCacheFileName = "web-files-cache.json"

	// webFilesCacheFileVersion is the schema version this build WRITES: entries
	// carry their recency tick (webFilesCacheEntryDoc), which is what makes an
	// LRU eviction survive a restart.
	webFilesCacheFileVersion = 2

	// webFilesCacheFileVersionUnordered is the schema the PREVIOUS build wrote:
	// a bare map of content key → file id with no ordering at all. It is still
	// READ (a table from yesterday is worth keeping), and its entries are
	// ordered deterministically by key — never by Go's randomised map
	// iteration — so the upgrade cannot reshuffle which pictures are kept.
	webFilesCacheFileVersionUnordered = 1

	// webFilesCacheFileMode keeps the table private to the operator. It holds
	// no credential — only opaque account-scoped file ids — but it describes
	// what the account was shown, which is nobody else's business.
	webFilesCacheFileMode = 0o600

	// webFilesCacheDirMode is the mode used when the profile directory has to
	// be created. Private to the operator, matching the file's own mode.
	webFilesCacheDirMode = 0o700

	// webFilesCacheTempPattern is the atomic-write scratch name. The rename is
	// what makes a concurrent reader see the whole table or the previous one,
	// never half of either; a same-directory temp file is required for the
	// rename to stay within one filesystem.
	webFilesCacheTempPattern = webFilesCacheFileName + ".tmp-*"
)

// webFilesCacheEntryDoc is ONE row of the table: the file id and the tick at
// which it was last used. The tick is what an eviction reads; the id is what a
// turn sends.
type webFilesCacheEntryDoc struct {
	ID   string `json:"id"`
	Used int64  `json:"used"`
}

// webFilesCacheDoc is the on-disk shape. One field carries the whole cache, so
// a future reader can see the table and its version without knowing this Go
// file; `version` is what turns "I do not understand this" into a decision, and
// `clock` is the logical tick the entries' `used` values are relative to (it is
// persisted so a restart continues the ordering instead of restarting it at 0,
// which would make every old entry look newer than every new one).
type webFilesCacheDoc struct {
	Version int                              `json:"version"`
	Clock   int64                            `json:"clock"`
	Entries map[string]webFilesCacheEntryDoc `json:"entries"`
}

// webFilesCacheLegacyDoc is the version-1 shape: no ordering information.
type webFilesCacheLegacyDoc struct {
	Version int               `json:"version"`
	Entries map[string]string `json:"entries"`
}

// webFilesCachePath resolves where the table lives: the gopi profile directory
// every other per-profile state uses (config.GopiDir honours GOPI_HOME, so a
// test and a sandbox get their own without any extra knob).
func webFilesCachePath() (string, error) {
	dir, err := config.GopiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, webFilesCacheFileName), nil
}

// webFilesCacheVerdict is the schema decision for one document: which shape it
// is in, and (for an oversize legacy table) the entries to keep.
type webFilesCacheLoad struct {
	version int
	clock   int64
	entries map[string]webFilesCacheEntryDoc
}

// decodeWebFilesCacheDoc parses a table in EITHER supported version and returns
// it in the CURRENT shape.
//
// The two shapes are decoded through ONE struct with a raw `entries` field
// rather than two passes, because a version-2 document read as version 1 would
// unmarshal every entry to the empty string and silently discard the table.
func decodeWebFilesCacheDoc(raw []byte) (webFilesCacheLoad, error) {
	var head struct {
		Version int             `json:"version"`
		Clock   int64           `json:"clock"`
		Entries json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return webFilesCacheLoad{}, err
	}
	load := webFilesCacheLoad{version: head.Version, clock: head.Clock,
		entries: make(map[string]webFilesCacheEntryDoc)}
	switch head.Version {
	case webFilesCacheFileVersion:
		if len(head.Entries) > 0 {
			if err := json.Unmarshal(head.Entries, &load.entries); err != nil {
				return webFilesCacheLoad{}, err
			}
		}
	case webFilesCacheFileVersionUnordered:
		var legacy map[string]string
		if len(head.Entries) > 0 {
			if err := json.Unmarshal(head.Entries, &legacy); err != nil {
				return webFilesCacheLoad{}, err
			}
		}
		// Deterministic ordering, and the reason is a measured defect: ranging a
		// Go map yields a different order on every start, so the OLD load path
		// kept a different arbitrary subset each time. Sorted keys make the
		// upgrade reproducible; the resulting ticks are the sorted position, so
		// the entry that sorts last is the most recently used one.
		keys := make([]string, 0, len(legacy))
		for k := range legacy {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			load.entries[k] = webFilesCacheEntryDoc{ID: legacy[k], Used: int64(i + 1)}
		}
		load.clock = int64(len(keys))
	default:
		return webFilesCacheLoad{}, fmt.Errorf("unsupported version %d (this build reads %d and %d)",
			head.Version, webFilesCacheFileVersionUnordered, webFilesCacheFileVersion)
	}
	return load, nil
}

// loadFromDisk fills the in-memory maps from the table, ONCE. Every failure is
// reported and then swallowed: a cache that cannot be read is a cache miss,
// never a reason to refuse the operator's picture (Rule 15 keeps the report;
// the turn keeps working).
func (c *webFilesCache) loadFromDisk() {
	path, err := webFilesCachePath()
	if err != nil {
		webFilesCacheWarnf("cannot resolve the cache path (%v) — this run remembers file ids in memory only", err)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			webFilesCacheWarnf("cannot read %s (%v) — this run remembers file ids in memory only", path, err)
		}
		return
	}
	load, err := decodeWebFilesCacheDoc(raw)
	if err != nil {
		webFilesCacheWarnf("%s is not a table this build understands (%v) — ignoring it; delete the file to start a fresh table", path, err)
		return
	}

	// THE SURVIVORS ARE CHOSEN BY RECENCY, NEVER BY MAP ITERATION ORDER. An
	// oversize document (a hand-edited table, or one written by a build with a
	// larger bound) is trimmed to the most recently used entries; the trim is
	// REPORTED, because a silently shrunk cache is a silent re-upload later.
	keys := make([]string, 0, len(load.entries))
	for key := range load.entries {
		keys = append(keys, key)
	}
	// Most recently used first; the key breaks ties so two starts of the same
	// process agree byte for byte on what they keep.
	sort.Slice(keys, func(i, j int) bool {
		a, b := load.entries[keys[i]], load.entries[keys[j]]
		if a.Used != b.Used {
			return a.Used > b.Used
		}
		return keys[i] < keys[j]
	})
	dropped := 0
	for _, key := range keys {
		if len(c.ids) >= webFilesCacheMax {
			dropped++
			continue
		}
		c.ids[key] = load.entries[key].ID
		c.used[key] = load.entries[key].Used
	}
	c.clock = load.clock
	if dropped > 0 {
		webFilesCacheWarnf("%s held %d entries and the table is bounded at %d — kept the %d most recently used and DROPPED %d (they will be re-uploaded if they come back)",
			path, len(keys), webFilesCacheMax, len(c.ids), dropped)
	}
}

// saveToDisk writes the whole table atomically. It is called on every
// admission or recency change, which is rare (once per NEW picture, or once per
// turn that re-references an old one) and cheap (a ~30 KB file), so there is no
// dirty-flag bookkeeping to get wrong.
//
// A failure here is reported, never fatal: the id is still in memory, so the
// RUN that just uploaded keeps working; only the NEXT process re-uploads.
func (c *webFilesCache) saveToDisk() {
	path, err := webFilesCachePath()
	if err != nil {
		webFilesCacheWarnf("cannot resolve the cache path (%v) — this run's file ids are not persisted", err)
		return
	}
	doc := webFilesCacheDoc{
		Version: webFilesCacheFileVersion,
		Clock:   c.clock,
		Entries: make(map[string]webFilesCacheEntryDoc, len(c.ids)),
	}
	for key, id := range c.ids {
		doc.Entries[key] = webFilesCacheEntryDoc{ID: id, Used: c.used[key]}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		// No user-controlled value can make these field types unsupported, so
		// this is a programming error wearing a runtime shape; it is still
		// reported rather than dropped.
		webFilesCacheWarnf("cannot encode the cache (%v) — this run's file ids are not persisted", err)
		return
	}
	dir := filepath.Dir(path)
	// The profile directory is created by whoever needs it first (no single
	// component owns "make ~/.gopi"), and a fresh profile — a sandbox, a test's
	// temp home — legitimately has none yet. Creating it here is what lets the
	// very first upload of a brand-new profile be remembered.
	if err := os.MkdirAll(dir, webFilesCacheDirMode); err != nil {
		webFilesCacheWarnf("cannot create the profile directory %s (%v) — this run's file ids are not persisted", dir, err)
		return
	}
	tmp, err := os.CreateTemp(dir, webFilesCacheTempPattern)
	if err != nil {
		webFilesCacheWarnf("cannot create the cache's scratch file in %s (%v) — this run's file ids are not persisted", dir, err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()        //stylegate:ignore the write already failed; closing cannot change the outcome
		_ = os.Remove(tmpName) //stylegate:ignore best-effort removal of the scratch file we just made
		webFilesCacheWarnf("cannot write %s (%v) — this run's file ids are not persisted", tmpName, err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName) //stylegate:ignore best-effort removal of the scratch file we just made
		webFilesCacheWarnf("cannot close %s (%v) — this run's file ids are not persisted", tmpName, err)
		return
	}
	if err := os.Chmod(tmpName, webFilesCacheFileMode); err != nil {
		_ = os.Remove(tmpName) //stylegate:ignore best-effort removal of the scratch file we just made
		webFilesCacheWarnf("cannot set mode %#o on %s (%v) — this run's file ids are not persisted", webFilesCacheFileMode, tmpName, err)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName) //stylegate:ignore best-effort removal of the scratch file we just made
		webFilesCacheWarnf("cannot publish %s (%v) — this run's file ids are not persisted", path, err)
		return
	}
}

// webFilesCacheWarnf reports a cache problem without failing anything. It goes
// to stderr through the carrier's own prefix so a session log shows whose
// message it is; every call site is a condition the operator can repair by
// deleting one file, which the message says.
func webFilesCacheWarnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gopi web: files cache: "+format+"\n", args...)
}
