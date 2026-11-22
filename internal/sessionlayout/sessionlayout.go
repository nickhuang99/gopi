// Package sessionlayout is the single source of truth for the SHAPE of a gopi
// sessions tree: given <gopi dir>/sessions, which "*.jsonl" is a session
// RECORD, which is a SIDE FILE of the same session, and which is a legacy
// flat-layout record.
//
// WHY IT EXISTS. The sessions tree is a two-level directory tree with three
// layouts, and every reader used to rediscover its shape by globbing
// "*.jsonl" and counting files. That is only correct while a session
// DIRECTORY holds exactly one JSONL — and it stopped being true the day
// signalrec started filing its provenance record beside the session log
// (internal/signalrec.FileName, one JSON record per caught termination signal
// for the very same session). A glob then reported ONE session as TWO:
//
//	<sessions>/<uuid>/session.jsonl              the record
//	<sessions>/<uuid>/signal-provenance.jsonl    a side file of that record
//
// The test-side accounting was fixed first (internal/testutil: layout-keyed,
// a dir-with-a-record = exactly one session). This package is that fix's
// PRODUCTION home, so a reader classifies by LAYOUT instead of by extension:
//
//	legacy flat   <sessions>/<uuid>.jsonl             record (the root IS its dir)
//	task run      <sessions>/<task_id>/<uuid>.jsonl   record (one per RUN)
//	bare session  <sessions>/<uuid>/session.jsonl     THE record of that dir
//	side file     <sessions>/<uuid>/<known>.jsonl     NOT a record — same session
//
// THE TWO DIRECTORY SHAPES. A session dir comes in two forms and they hold a
// different NUMBER of records:
//
//   - the BARE form holds RecordName ("session.jsonl") — exactly one session
//     ran here, so any other JSONL beside it is an artifact of that session
//     (the registered side files; anything unregistered is reported as
//     unknown rather than counted);
//   - the TASK-RUN form holds "<uuid>.jsonl" — one file per RUN of the task,
//     so several files are several run logs, and each IS a record.
//
// The classification is a NAME rule plus that LAYOUT rule, never a content
// probe: reading every candidate's bytes to decide whether it "looks like" a
// session would turn a directory listing into a full-tree parse, and the
// answer must be the same for a truncated, empty or still-being-written file.
//
// WHY NOT internal/signalrec. That package owns the side file's VALUE
// (signalrec.FileName) but it is not a leaf: it reads the process's own
// cgroup and imports config, and every reader of the sessions tree —
// internal/gopilog (which cmd/agent, internal/tools and the CLI all sit on) —
// would then link the signal recorder for a filename. Instead this package is
// the leaf (stdlib only, zero project imports), it names the side file once,
// and TestSideFileNamesMatchWriters asserts its name EQUALS
// signalrec.FileName — one value, one production home, drift impossible.
//
// THE RULE FOR A NEW *.jsonl IN A SESSION DIR: register it here (as a side
// file) or it is reported as an UNKNOWN artifact. Silent hiding is the one
// outcome this package refuses: an unregistered file is still not counted as
// a record in a bare session dir, but a caller that lists side files sees it
// named as unknown, so a genuinely stray artifact stays visible.
package sessionlayout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

const (
	// JSONLSuffix is the extension of every JSONL artifact a sessions tree
	// holds. Named once (PROJECT.md Rule 1b/13): the record scan and the
	// side-file scan can never disagree about what "a JSONL file" is.
	JSONLSuffix = ".jsonl"

	// sessionStartScanBuf bounds the scan sessionStartID makes for the
	// session_start row. The row is written before the first turn, so a cap of
	// one line's worth of room is generous; a longer cap would let one corrupt
	// record cost a large read for an id this function is only a fallback for.
	sessionStartScanBuf = 64 * 1024

	// EventTypeSessionStart is the session_start row's type, taken from its ONE
	// definition (Rule 14) — this package must not carry a second spelling of a
	// value the reader in internal/sessionevent owns.
	EventTypeSessionStart = sessionevent.EventTypeSessionStart

	// RecordName is the session RECORD's file name in the bare-session
	// layout (session-dir-unify S1): a session dir holds exactly one, and the
	// session uuid lives inside the file (its session_start event) and in the
	// DIRECTORY name — never in this file's name.
	RecordName = "session.jsonl"

	// ArtifactsDirName is the subdirectory of a SESSION DIR that holds what a
	// run produced ABOUT that session, as opposed to what the session produced:
	// a chat's hand-written notes, a recording's filmed replay. It is a NON-JSONL
	// member, so Classify above reads it as KindOther and never as a record — a
	// session dir carrying artifacts is still exactly one session.
	//
	// The name is defined HERE, in the leaf that owns the shape of a sessions
	// tree, because it has two writers that must agree (Rule 14): the chat write
	// boundary (internal/tools.ChatSessionArtifactsDirName is an alias of this
	// constant) hands a chat its destination, and internal/replay puts a filmed
	// recording under it. A second spelling would be a second answer to "where
	// does a session's own output live", which is exactly the drift the single
	// source rule closes.
	ArtifactsDirName = "artifacts"

	// LineTerminator is the byte that TERMINATES a line of a session record.
	//
	// It is named here, in the leaf that owns the SHAPE of a sessions tree,
	// because two packages now have to agree about it and neither may import
	// the other: the READER (internal/gopilog) uses it to tell a line that ends
	// with a terminator from a final line that does not, and the WRITER
	// (internal/agent) uses it to refuse appending to a record whose last byte
	// is not one. A second spelling would be a second answer to "is this record
	// well-formed", and the whole point of the torn-tail classification is that
	// there is ONE answer.
	LineTerminator = '\n'

	// SessionMessagesDirName is the subdirectory of a SESSION DIR that holds
	// the INBOUND messages an external process handed to that session
	// (`gopi notify --session <id> --message ...`): the durable inbox the
	// round boundary loads, written by another process while this one may not
	// be running at all.
	//
	// It is a DIRECTORY, not a JSONL artifact, so the side-file registry above
	// does not claim it: the dir arm of Classify asks DirHasRecord, so a
	// session dir holding this subdirectory is still classified as exactly ONE
	// session (the same reason ArtifactsDirName needs no registration).
	SessionMessagesDirName = "messages"

	// provenanceSideFileName is the signal-provenance side file's name — the
	// value of the writer's constant (internal/signalrec.FileName), which is
	// NOT imported here because that package is not a leaf (see the package
	// comment). TestSideFileNamesMatchWriters pins the two spellings
	// together, so this cannot drift from what the recorder actually writes.
	provenanceSideFileName = "signal-provenance.jsonl"

	// SessionDirNotReadyFormat is the REFUSAL for the one state a session
	// OPERAND can name that is neither ready nor unknown: the directory is
	// there and holds no record, because a run's session directory is created
	// when the run is CLAIMED and its record is written a moment later, when
	// the run STARTS.
	//
	// WHY IT LIVES HERE, in the leaf that owns the shape of a sessions tree
	// (Rules 13/14). TWO faces resolve a session operand and both must answer
	// this state the same way, because a sender does not know which one it is
	// talking to: the local DELIVER face (cmd/agent.resolveNotifySessionDir)
	// and the peer handler (internal/tools.notifyPeerSessionDir). Neither may
	// import the other — internal/tools is below cmd/agent, and the local face
	// reaches gopilog, which imports internal/tools — so the ONE sentence they
	// must share is a value, not code, and this package is the leaf both
	// already import for DirHasRecord.
	//
	// WHY IT IS A SEPARATE SENTENCE FROM "unknown session" (measured
	// 2026-11-05, row 01a1ac2f-84c6). Both faces used DirHasRecord as their
	// only test and reported this window with their unknown-session sentence,
	// verbatim the words used for an id that will never exist. The two states
	// want OPPOSITE responses — an unknown id ends the delivery, an unwritten
	// record wants the same bytes again in a few seconds — and the measured
	// cost was a coordination message to a just-claimed peer that was one
	// retry away from being abandoned for good.
	//
	// IT STAYS A REFUSAL. A directory with no record is not a session, and
	// writing into one is a delivery to nobody; what changes is only the
	// REASON, which now names the path, the record looked for, the mechanism
	// that produced the window, and the action (Rule 15: a refusal a reader
	// cannot follow is a dead end). The parenthetical names the OTHER cause,
	// so an operator who retries correctly and still sees it knows what it
	// means: the run died before its first write, and the empty directory is
	// what it left behind.
	SessionDirNotReadyFormat = "%s holds no session record (%s) yet — a run's session directory is created when the run is claimed and its record is written when the run starts, so this one is most likely NOT READY YET: retry in a few seconds with the same operand. (If it stays empty the run died before its first write and there is nothing to deliver to — that is a dead directory, still not an unknown id)"

	// SessionRootInboxRefusalFormat is the REFUSAL for a session operand whose
	// record resolves directly under the sessions ROOT — the LEGACY FLAT
	// layout (`<root>/<uuid>.jsonl`), whose "container" is the tree itself.
	//
	// WHY IT IS A REFUSAL AND NOT A DELIVERY (measured 2026-11-14, this host).
	// Both notify faces build the inbox as `<container>/messages/inbox.jsonl`
	// from `RecordContainer(record)`, and for a flat record that container IS
	// the sessions root — so the message lands in ONE inbox that every
	// flat-layout session shares, and each of them reads the others'. Measured
	// on a sandbox profile: a flat record at the root plus
	// `gopi notify --session <id>` printed "queued", exited 0 and wrote
	// `<root>/messages/inbox.jsonl`. That is a CROSS-DELIVERY (a message
	// addressed to one session is taken by any of them), which no retry
	// repairs — so the operand is refused and the reader is told to address the
	// session by its DIRECTORY.
	//
	// The live host's root holds no `*.jsonl` at all (measured 2026-11-14), so
	// this path is only reachable on a tree that still carries the pre-session
	// layout. The four records the epic found in `<root>/messages/inbox.jsonl`
	// were written by the DIFFERENT defect fixed on 2026-10-13 (a second
	// `filepath.Dir()` peel); this sentence closes the remaining way in.
	//
	// It lives HERE, in the leaf that owns the shape of a sessions tree, for the
	// reason SessionDirNotReadyFormat does: the local face (cmd/agent) and the
	// peer handler (internal/tools) both resolve this operand and neither may
	// import the other, so the ONE sentence they must share is a value.
	SessionRootInboxRefusalFormat = "session %s resolves to the flat-layout record %s, whose CONTAINER is the sessions root itself — so its inbox would be the root's own %s/ directory: ONE inbox SHARED by every flat-layout session, where each of them would read messages addressed to the others. A real delivery needs a session CONTAINER: address the session by its DIRECTORY NAME (a directory under the sessions root that holds a %s record) instead of by a run uuid or a flat record's id"

	// SessionsRootIsNotASessionDirFormat is the REFUSAL for a DIRECTORY operand
	// that IS the sessions ROOT — the operand `/home/<user>/.gopi/sessions`,
	// i.e. the tree that CONTAINS session directories handed to a verb that
	// asked for one of them.
	//
	// WHY IT IS ITS OWN SENTENCE (measured 2026-11-14, this host). The root also
	// holds no record, so the ordinary "not a session directory" refusal covers
	// it — and says nothing about the one fact that matters: the caller is
	// pointing at the CONTAINER OF EVERYTHING. A delivery built from it is
	// `<root>/messages/inbox.jsonl`, the tree-wide inbox that no session reads
	// (the sibling of SessionRootInboxRefusalFormat, which refuses the same
	// depth reached through a flat-layout RECORD). Naming the operand as what it
	// actually is turns "this looks empty, maybe I typed it wrong" into "you
	// named the tree, name a directory inside it".
	SessionsRootIsNotASessionDirFormat = "%s is the sessions ROOT — the tree that CONTAINS session directories, never one of them: its own %s/ subdirectory would be a tree-wide inbox no session reads. Name the session by a DIRECTORY under it that holds a %s record"

	// ReservedSessionDirNameFormat is the REFUSAL for a DIRECTORY operand whose
	// BASE NAME is reserved by the sessions tree — SessionMessagesDirName or
	// ArtifactsDirName.
	//
	// WHY THE NAME ALONE IS DECISIVE (measured 2026-11-14, this host). A
	// directory with no record was accepted whenever it happened to hold ANY
	// `.jsonl` that is not a registered side file, and both reserved names are
	// EXACTLY such a name in practice: `messages/` holds `inbox.jsonl` (the
	// deliverable this epic is about — delivered, printed "queued", exited 0,
	// and an inbox no session ever reads) and `artifacts/` holds the run's own
	// captured records. So the shape test alone let the tree's own bookkeeping
	// directories claim to be sessions. The names cannot be sessions by
	// construction: this package joins `%s` onto a session CONTAINER (inside),
	// never onto the root, so a directory NAMED that is the tree's, not a
	// session's.
	//
	// IT STAYS A REFUSAL, never a silent redirect to "the nearest real session":
	// a sender that typed `messages` made a mistake about WHICH session, and
	// only the sender can supply the right answer (Rule 15: a refusal names its
	// cause and the way out).
	ReservedSessionDirNameFormat = "%s is a session's %s/ directory — it is the tree's own bookkeeping INSIDE a session, never a session: an inbox or an artifact directory holds records ABOUT a session, so a delivery into it reaches nobody. Name the session by the DIRECTORY that holds its %s record"

	// SessionDirNoRecordFormat is the REFUSAL for a directory that holds
	// `.jsonl` data but NO SESSION RECORD — the shape test's blind spot, named.
	//
	// The criterion (SessionDirRefusal) is not "does it hold a .jsonl":
	// a session record is a .jsonl whose FIRST events carry the session's
	// identity (a session_start event), which is what every session surface
	// reads to answer "who is this". MEASURED on this host 2026-11-14: of the
	// 2914 directories under ~/.gopi/sessions holding a .jsonl record, 2913
	// carry a session_start and EXACTLY ONE does not — the orphan inbox
	// <root>/messages/inbox.jsonl this epic is about. So the test costs no
	// legitimate session directory a delivery, and it catches the family
	// (a capture dir, a sibling artifact dir) that no name rule can enumerate.
	//
	// A record that is EMPTY is excluded on purpose: a run's session record is
	// created before its first row is written, and that window must keep
	// answering NOT READY / retry (SessionDirNotReadyFormat) rather than
	// "this is not a session" — the two states want opposite actions.
	SessionDirNoRecordFormat = "%s holds .jsonl data but no SESSION RECORD: a session record carries a session_start identity and none of the .jsonl here has one, so this is a capture/artifact directory rather than a session — a delivery into it reaches nobody. Name the session by the DIRECTORY that holds its record, and read `gopi session list` for the ones that exist"

	// ExitMarkerName is the CHAT EXIT MARKER's file name in a session dir —
	// the ONE spelling, read by the writer (cmd/agent.armChatExitMarker) and by
	// the reader that annotates an orphan dump
	// (internal/agent.ReadChatExitMarker's callers in crashdump.go).
	//
	// IT IS DELIBERATELY NOT A `.jsonl` NAME. In a session dir a JSONL is
	// either the record or a registered SIDE FILE (sideFileNames above, whose
	// whole meaning is "an artifact OF the session record beside it"), and this
	// file is neither: it is one RUN's evidence, the family this tree already
	// classifies as run-evidence (`chat-screen.dump`, `console.log`,
	// `metadata.json`). Registering it as a side file would say it annotates the
	// RECORD (it outlives nothing — a resurrected session writes its own), and
	// the file is not a JSONL document anyway: it is one JSON object describing
	// a process's last phase, so a reader that opened it as a record would parse
	// a shape no record has.
	//
	// WHY THE NAME LIVES HERE: this package owns the shape of a sessions tree,
	// so a new file a session dir holds is named in ONE place (Rule 14) — the
	// defect this prevents is the exact one that made 142 orphan dumps claim
	// `session_id: "session"`: a writer and a reader spelling the same artifact
	// differently.
	ExitMarkerName = "chat-exit.marker"

	// prefetchSideFileName is the typing-time memory-prefetch record's name —
	// the value of the writer's constant (config.MemoryPrefetchRecordName),
	// for the same leaf reason as provenanceSideFileName above: this package
	// imports NOTHING (stdlib only), and taking the value from internal/config
	// would close a cycle — internal/config already imports THIS package for
	// its scratch-tree resolution. TestSideFileNamesMatchWriters pins the two
	// spellings together, so this cannot drift from what the executor writes.
	//
	// WHY IT IS A SIDE FILE AND NOT A STRAY: the knob is DEFAULT ON (see
	// config/memory_prefetch.go), and its executor opens this file with
	// O_CREATE|O_APPEND for the whole run — so EVERY `gopi chat` session
	// directory carries it, empty or not, by design. Unregistered, it read as
	// "an unknown .jsonl" and made every test that asserts "one session, no
	// stray artifacts" fail for a feature that was working exactly as designed:
	// the file is an artifact of THAT session, which is the definition of a
	// side file. (Rule 35: the record lands in the session directory the run
	// already creates, and is swept with it — no new artifact outside it.)
	prefetchSideFileName = "memory-prefetch.jsonl"
)

// sideFileNames are the SIDE FILES a session DIRECTORY legitimately holds
// beside its session record: artifacts of the SAME session, never a second
// session. The list is deliberately a NAME match, not a pattern: a new
// artifact must be added here on purpose, and until it is, it is reported as
// unknown rather than silently ignored.
var sideFileNames = []string{
	provenanceSideFileName,
	prefetchSideFileName,
}

// SideFileNames returns the registered side-file names (a copy — callers must
// not be able to mutate the classification).
func SideFileNames() []string {
	out := make([]string, len(sideFileNames))
	copy(out, sideFileNames)
	return out
}

// IsJSONL reports whether name is a JSONL artifact of a sessions tree.
func IsJSONL(name string) bool { return strings.HasSuffix(name, JSONLSuffix) }

// IsSideFile reports whether name is a registered side file of a session.
func IsSideFile(name string) bool {
	for _, s := range sideFileNames {
		if name == s {
			return true
		}
	}
	return false
}

// Kind classifies one entry of a sessions tree.
type Kind uint8

const (
	// KindOther: not an artifact of the record family — metadata.json, a
	// report, a raw log, a crash dump, a subdirectory that holds no record.
	KindOther Kind = iota
	// KindRecord: this entry IS a session record (a file), or a session dir
	// (a directory that holds at least one).
	KindRecord
	// KindSideFile: a JSONL side file of the session whose record sits beside
	// it. Never a second session.
	KindSideFile
	// KindUnknown: a JSONL inside a BARE session dir that neither the record
	// rule nor the side-file registry claims. That dir holds exactly one
	// record, so this file is an artifact of it — reported, never counted.
	KindUnknown
)

// Entry is one classified member of the sessions tree. Path is the entry's
// own path (for a directory entry, the directory); Name its base name.
type Entry struct {
	Path  string
	Name  string
	Kind  Kind
	IsDir bool
}

// Classify classifies ONE entry of the sessions tree.
//
// parentIsRoot says whether parent is the sessions ROOT itself (the home of
// the legacy flat layout), and it is half the layout key: directly under the
// root a bare "<uuid>.jsonl" IS a record, while the same file one level down
// sits inside a session dir and is judged by that dir's shape (see the
// package comment).
func Classify(parent string, parentIsRoot bool, e os.DirEntry) Entry {
	name := e.Name()
	ent := Entry{Path: filepath.Join(parent, name), Name: name, IsDir: e.IsDir()}
	switch {
	case e.IsDir():
		// A session container is a record only when it HOLDS one: a dir with
		// no JSONL (attachments-only, a dir being created, a resident
		// process's dir) is not a session, and must not be counted as one.
		if DirHasRecord(ent.Path) {
			ent.Kind = KindRecord
		}
	case !IsJSONL(name):
		// metadata.json, a report, a crash dump, a raw log.
	case IsSideFile(name):
		ent.Kind = KindSideFile
	case parentIsRoot || name == RecordName:
		// The legacy flat record, or the bare-session record's fixed name.
		ent.Kind = KindRecord
	case HasBareRecord(parent):
		// Inside a session dir whose shape is the BARE one: that dir holds
		// exactly one session, so this file is an artifact beside its record
		// (the side files are registered above; an unregistered one lands
		// here and is reported, never counted).
		ent.Kind = KindUnknown
	default:
		// A task-run dir holds one JSONL per run: each is a run log — a
		// record of its own.
		ent.Kind = KindRecord
	}
	return ent
}

// DirHasRecord reports whether dir holds a session record — a session dir's
// shape test, and the layout key for the DIRECTORY form of a session.
func DirHasRecord(dir string) bool {
	subs, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, se := range subs {
		if se.IsDir() || !IsJSONL(se.Name()) || IsSideFile(se.Name()) {
			continue
		}
		return true
	}
	return false
}

// SessionDirRefusal answers "may a DIRECTORY be taken as a session dir?" — the
// ONE criterion both notify faces apply before delivering into it, and the
// SECOND half of "what is a session" (the first being DirHasRecord).
//
// A directory is a session dir when it holds a session RECORD, and "record"
// here is not "any .jsonl": it is a .jsonl that carries the session's IDENTITY
// (a session_start event, the row every session surface reads to answer "who is
// this"). Three facts complete the criterion, and the first two are measured
// rather than theoretical — the sessions tree keeps its own BOOKKEEPING
// directories INSIDE sessions under reserved names, and each of them holds a
// `.jsonl` a shape test cannot tell from a record:
//
//	<session>/messages/inbox.jsonl    the inbound inbox (delivery target)
//	<session>/artifacts/*.jsonl       the run's captured records
//
// ① A RESERVED NAME can never be a session: this package joins
// `SessionMessagesDirName` onto a session CONTAINER (inside), never onto the
// root, so a directory NAMED that is the tree's bookkeeping. Measured on this
// host 2026-11-14 (task 01a1d7fb): `--session messages` was accepted, delivered
// into `<root>/messages/messages/inbox.jsonl` and reported "queued" with exit
// 0 — and `/home/user/.gopi/sessions/messages/inbox.jsonl` already existed (4
// records from the earlier peel defect), which is why the shape test said YES.
// `ArtifactsDirName` closes the CLASS of reserved names rather than the
// instance.
// ② A MISSING IDENTITY is the same verdict for the records that carry no
// layout signal (a task-run dir's several `<uuid>.jsonl`, a capture dir, a
// sibling artifact dir): of the 2914 directories under this host's sessions
// root holding a .jsonl record, 2913 carry a session_start and EXACTLY ONE
// does not — the orphan inbox above. Two exemptions, each measured:
// a directory holding `RecordName` IS a bare session by the layout rule
// (HasBareRecord) whatever that file holds — one suite fixture writes it as
// `{}` mid-bootstrap and was refused by an identity-only test; and an EMPTY
// record is a run whose first row is not written yet, which must keep
// answering NOT READY (retry), never "this is not a session".
// ③ The sessions ROOT ITSELF is never a session dir: it is the container of
// every session, and its inbox would be the tree-wide one.
//
// ok=true returns the record the dir stands for — the newest one that carries
// an identity, so a task-run container holding several runs resolves as every
// other surface resolves it (Rules 13/14: ONE answer to "which record does this
// dir stand for"). recordPath is "" when the dir holds no record at all (the
// caller then decides: "not ready yet" or "no such session") and when a record
// is present but carries no identity (that case returns an ERROR, because it is
// a refusal rather than an absence).
//
// THE REFUSAL SENTENCES LIVE IN THIS PACKAGE, not in the callers, and that is
// structural: the local face (cmd/agent.resolveNotifySessionDir) and the peer
// handler (internal/tools.notifyPeerSessionDir) both ask this question and
// neither may import the other. A value they share is the only shape that keeps
// the two faces saying the same thing.
func SessionDirRefusal(dir, sessionsRoot string) (recordPath string, err error) {
	clean := filepath.Clean(dir)
	if sessionsRoot != "" && clean == filepath.Clean(sessionsRoot) {
		return "", fmt.Errorf(SessionsRootIsNotASessionDirFormat,
			dir, SessionMessagesDirName, RecordName)
	}
	switch base := filepath.Base(clean); base {
	case SessionMessagesDirName, ArtifactsDirName:
		return "", fmt.Errorf(ReservedSessionDirNameFormat, dir, base, RecordName)
	}
	records := recordsIn(clean)
	if len(records) == 0 {
		return "", nil
	}
	// THE BARE LAYOUT'S OWN FILE IS DECISIVE, whatever it holds: a directory
	// holding `RecordName` IS a bare session dir by the layout rule this package
	// owns (HasBareRecord), and a record that is empty or still being written is
	// the run's own file — refusing it would refuse a real session. MEASURED
	// (2026-11-14, this fix's own suite): a fixture that writes RecordName with
	// `{}` — no session_start yet, exactly a record mid-bootstrap — was refused
	// by an identity-only test. The layout answers that case; identity is only
	// needed for the records that carry NO layout signal.
	if HasBareRecord(clean) {
		return newestOf(records).Path, nil
	}
	var identified []Record
	for _, rec := range records {
		if recordCarriesIdentity(rec.Path) {
			identified = append(identified, rec)
		}
	}
	if len(identified) == 0 {
		return "", fmt.Errorf(SessionDirNoRecordFormat, dir)
	}
	return newestOf(identified).Path, nil
}

// recordCarriesIdentity reports whether one record file identifies a session:
// it carries a session_start event, OR it is EMPTY — the run-created-its-file-
// but-has-not-written-the-first-row window, which every other surface treats as
// a session in progress (the caller's NOT READY arm is what answers it).
//
// An unreadable file is NOT an identity: the caller that asks this question is
// about to write beside it, and "I could not read it" must never be reported as
// "it is a session" (Rule 15).
func recordCarriesIdentity(recordPath string) bool {
	fi, err := os.Stat(recordPath)
	if err != nil {
		return false
	}
	if fi.Size() == 0 {
		return true
	}
	return sessionStartID(recordPath) != ""
}

// newestOf returns the most recently written record of a set — NewestRecordInDir's
// ordering, applied to the records a caller has already filtered. The zero
// answer for an empty set is a caller bug, and the empty Record it returns
// carries an empty Path that no writer treats as a file.
func newestOf(records []Record) Record {
	if len(records) == 0 {
		return Record{}
	}
	newest := records[0]
	var newestMod time.Time
	for _, r := range records {
		fi, err := os.Stat(r.Path)
		if err != nil {
			continue
		}
		if newestMod.IsZero() || fi.ModTime().After(newestMod) {
			newest, newestMod = r, fi.ModTime()
		}
	}
	return newest
}

// HasBareRecord reports whether dir is a BARE session dir — one that holds the
// fixed RecordName, and therefore exactly one session record.
func HasBareRecord(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, RecordName))
	return err == nil && fi.Mode().IsRegular()
}

// SideFilesIn returns the side files found in a session dir, as base NAMES:
// the registered ones, plus (labelled " (unknown .jsonl)") any other JSONL the
// record rule did not claim for that dir's shape. Used for diagnostics and for
// the accounting form below — an unknown member is visible, never silently
// absorbed.
func SideFilesIn(dir string) []string {
	subs, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	bare := HasBareRecord(dir)
	var out []string
	for _, se := range subs {
		if se.IsDir() || !IsJSONL(se.Name()) || se.Name() == RecordName {
			continue // never a side file: not JSONL, a subdir, or the record itself
		}
		if !bare && !IsSideFile(se.Name()) {
			continue // a task-run dir: every JSONL is a run record
		}
		if IsSideFile(se.Name()) {
			out = append(out, se.Name())
			continue
		}
		out = append(out, se.Name()+" (unknown .jsonl)")
	}
	return out
}

// Record is one session record found in a sessions tree: the record FILE's
// path, the session dir that contains it (the flat layout's root is its own
// dir), and the side files filed beside it (base names, unknown ones labelled
// — join them with Dir for a path).
type Record struct {
	Path      string
	Dir       string
	SideFiles []string
}

// Records walks the sessions tree one level down and returns every session
// RECORD FILE in it — the layout-keyed scan every reader shares.
//
// A bare session dir contributes exactly ONE record however many side files
// it holds, and a dir holding only side files contributes none; a task-run dir
// contributes one record per run file; a bare "*.jsonl" at the root is a
// legacy flat record. The walk descends exactly ONE level: a session's own run
// dirs hold their JSONL directly, so anything deeper is an attachment, not a
// record.
func Records(root string) ([]Record, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		ent := Classify(root, true, e)
		switch {
		case !ent.IsDir && ent.Kind == KindRecord:
			out = append(out, Record{Path: ent.Path, Dir: root})
		case ent.IsDir && ent.Kind == KindRecord:
			out = append(out, recordsIn(ent.Path)...)
		}
	}
	return out, nil
}

// RecordInDir returns the record held by ONE session dir: its RecordName file,
// or the single "*.jsonl" run file of a task-run dir. It is the ACCOUNTING
// form ("this dir is one session, and here is its log"), where Records is the
// SCANNING form (every run file is its own record). ok is false for a dir
// holding no record (only side files, or nothing at all).
func RecordInDir(dir string) (Record, bool) {
	rs := recordsIn(dir)
	if len(rs) == 0 {
		return Record{}, false
	}
	return rs[0], true
}

// NewestRecordInDir returns the MOST RECENTLY WRITTEN record of one run
// directory — the ONE answer to "which record does this run directory stand
// for?" (Rules 13/14: internal/gopilog's task-id resolution and
// internal/tools' ledger stamp both ask it, and two loops would drift).
//
// It is RecordInDir's ordering-aware sibling, and the difference matters
// exactly where the two layouts differ: a BARE session dir holds one record
// (both agree), while a TASK-RUN dir holds one record per run — a killed run
// plus its retry, or (after the identity unify) the runs of one row that all
// carry the row's id. Every caller that renders "this directory's session"
// wants the latest one, because that is the run whose ledger row, holder and
// turn count a reader is looking at.
//
// ok is false for a directory holding no record — the ordinary "nothing ran
// here yet" absence, never a guess.
func NewestRecordInDir(dir string) (Record, bool) {
	rs := recordsIn(dir)
	if len(rs) == 0 {
		return Record{}, false
	}
	return newestOf(rs), true
}

// recordsIn returns every record file inside one session dir, carrying that
// dir's side files on the first of them.
func recordsIn(dir string) []Record {
	subs, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	side := SideFilesIn(dir)
	var out []Record
	for _, se := range subs {
		if se.IsDir() || !IsJSONL(se.Name()) {
			continue
		}
		if !IsSideFile(se.Name()) {
			if HasBareRecord(dir) && se.Name() != RecordName {
				continue // an artifact beside the bare session's record
			}
			out = append(out, Record{Path: filepath.Join(dir, se.Name()), Dir: dir})
		}
	}
	if len(out) > 0 {
		out[0].SideFiles = side
	}
	return out
}

// RecordContainer returns the directory that CONTAINS a session record — the
// session dir a record's own INBOX belongs to (`gopi notify`'s
// <dir>/(SessionMessagesDirName), the same dir a caller reports as "the session
// dir"). It is the ONE definition of that rule (PROJECT.md Rules 13/14): the local
// face (cmd/agent/cli_notify.go, resolveNotifySessionDir) and the cross-peer
// handler (internal/tools/notify_peer.go, notifyPeerSessionDir) both call it,
// because the two faces resolving the same operand must not answer "which dir
// holds this session" differently.
//
// THE RULE IS A PROPERTY OF THE PATH, NEVER OF THE BASENAME. The record was
// found by an exact id match, so its own directory is its container in ALL
// THREE layouts:
//
//	legacy flat   <sessions>/<uuid>.jsonl             dir IS the sessions root
//	bare session  <sessions>/<uuid>/session.jsonl     dir is <sessions>/<uuid>
//	task run      <sessions>/<task_id>/<uuid>.jsonl   dir is <sessions>/<task_id>
//
// A basename test cannot decide this, and that is the measured defect this
// function exists to prevent (Rule 5's red proof, 2026-10-13): "the file is not
// named RecordName, so peel one level" reads a RUN RECORD as if it were a flat
// one — the run record's name is its own uuid — and peels <task_id> away,
// landing the message in <sessions>/messages/, a directory no session ever
// reads, while the verb exits 0 and reports success. The task-run layout is the
// ONLY one where the container differs from the record's dir, and no name can
// tell it from the flat layout: the path alone settles it.
func RecordContainer(recordPath string) string { return filepath.Dir(recordPath) }

// ContainerIsSessionsRoot reports whether a record's CONTAINER is the sessions
// tree root itself — i.e. the record matched the LEGACY FLAT layout
// (`<root>/<uuid>.jsonl`), whose container is the tree every session shares.
//
// WHY THE QUESTION MUST BE ASKED AT ALL (measured 2026-11-14, this host, in a
// sandbox profile). RecordContainer answers "which directory holds this
// record" correctly for all three layouts, and for the flat one the honest
// answer is "the root". But the inbox is `<container>/(SessionMessagesDirName)`,
// so the flat layout's inbox is ONE directory shared by every flat-layout
// session — each of them would read messages addressed to the others. Measured:
// a flat record at the root plus `gopi notify --session <id>` printed
// "queued", exited 0 and wrote `<root>/messages/inbox.jsonl`; a second session
// added to the same root would have read the first one's message.
//
// So the two notify faces REFUSE that operand by name
// (SessionRootInboxRefusalFormat) instead of delivering into a shared inbox.
// The predicate lives here, beside RecordContainer, because the local face
// (cmd/agent) and the peer handler (internal/tools) both ask it and neither may
// import the other (Rules 13/14: one answer, two callers).
//
// An EMPTY sessionsRoot is answered false, never true: a caller that could not
// resolve the tree has already failed somewhere else and must not be handed a
// refusal about a path it never compared (Rule 15 — a wrong refusal hides the
// real one).
func ContainerIsSessionsRoot(recordPath, sessionsRoot string) bool {
	if strings.TrimSpace(recordPath) == "" || strings.TrimSpace(sessionsRoot) == "" {
		return false
	}
	return filepath.Clean(filepath.Dir(recordPath)) == filepath.Clean(sessionsRoot)
}

// RecordPath returns the path of the FIRST record under root ("" when the tree
// holds none yet or cannot be read) — the readiness probe for "has this run
// written its session yet", built on the same accounting as Records so a probe
// and a final assertion cannot disagree about what a session is.
func RecordPath(root string) string {
	rs, err := Records(root)
	if err != nil || len(rs) == 0 {
		return ""
	}
	return rs[0].Path
}

// SessionID resolves the IDENTITY of the session a record path belongs to —
// the one answer every writer that names a session in a FILE must use, rather
// than each deriving it from the basename.
//
// WHY IT EXISTS (measured, 2026-10-20, session 8616b46c-db23-41cb-a657-c3a519abd998):
// the orphan-crash-dump backfill derived the id as
// TrimSuffix(filepath.Base(recordPath), ".jsonl"). In the bare-session layout
// that basename is the FIXED name `session.jsonl`, so the derived id was the
// literal string "session" — every such dump on this host claimed
// session_id="session" (142 of them), and the reader that retracts a dump once
// its session has recorded a `session_end` compares that claim against the
// id it derives from the DUMP's basename instead. On a dump named
// <dir>/session.dump.json that derived id is also "session", so the comparison
// passed and the WRONG id never surfaced: the bug was a wrong value that
// nonetheless agreed with itself.
//
// The order is the same one internal/gopilog's display id uses, and it is
// deliberate: the RECORDED identity (the session_start event's sessionId) wins,
// because a session's own log is the authority on who it is; the layout is a
// fallback for a log that died before it wrote its first row.
func SessionID(recordPath string) string {
	if id := sessionStartID(recordPath); id != "" {
		return id
	}
	if filepath.Base(recordPath) == RecordName {
		return filepath.Base(RecordContainer(recordPath))
	}
	return strings.TrimSuffix(filepath.Base(recordPath), JSONLSuffix)
}

// RecordTailUnterminated reports whether the record at recordPath ends without
// a line terminator (LineTerminator) — i.e. whether its last line is a fragment
// the writer never finished. A zero-length record reports (false, nil); a
// record that cannot be opened or read reports its error, because "the file is
// not there" and "the tail is fine" are different facts and the caller that
// asks this question is about to WRITE to it (Rule 15: no silent fallback).
//
// WHY IT LIVES HERE AND NOT IN THE READER OR THE WRITER. Two packages need this
// one answer and one of them may not import the other (internal/gopilog imports
// internal/agent; the agent package therefore cannot call the reader). This
// package is the leaf they both already import for the shape of a sessions tree,
// and the line terminator is part of that shape, so the predicate is stated
// once, next to the constant it tests.
//
// THE MEASURED REASON IT EXISTS. Three records on this host were left ending
// mid-record (1,286,144 / 512,000 / 405,504 bytes, torn fragments of 1,530 /
// 469 / 135 bytes). A reader can only REFUSE such a record. A writer that
// appends to it fuses the unreachable fragment with the next run's first line,
// and the damage then stops being a legible "torn tail" and becomes two
// records' worth of permanent MID-FILE corruption that no reader can separate.
func RecordTailUnterminated(recordPath string) (bool, error) {
	f, err := os.Open(recordPath)
	if err != nil {
		return false, err
	}
	defer f.Close() //stylegate:ignore one-byte probe opened read-only; a failed close cannot corrupt the record

	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	size := fi.Size()
	if size == 0 {
		return false, nil
	}
	// Read the LAST byte only: this probe runs before every append to a record
	// that can be hundreds of megabytes, so a full read would make the guard the
	// most expensive thing in the run.
	if _, err := f.Seek(size-1, io.SeekStart); err != nil {
		return false, err
	}
	var last [1]byte
	if _, err := io.ReadFull(f, last[:]); err != nil {
		return false, err
	}
	return last[0] != LineTerminator, nil
}

// sessionStartID reads the sessionId field out of the session_start event —
// the first row of every record a gopi writer creates. It stops at the first
// session_start it finds (that row is written and synced before the first
// turn, so it is never far into the file), and returns "" when the record is
// unreadable or carries no such event, which is the caller's signal to fall
// back to the layout.
func sessionStartID(recordPath string) string {
	f, err := os.Open(recordPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, sessionStartScanBuf), sessionStartScanBuf)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Type == EventTypeSessionStart && ev.SessionID != "" {
			return ev.SessionID
		}
	}
	return ""
}
