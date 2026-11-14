package main

// chat_replay_mints_no_session_test.go — A REPLAY RUN LANDS NOWHERE: no session
// directory, no session.jsonl, no metadata.json, no .raw.log (epic: `gopi chat
// --replay` must be really read-only; this row removes the scratch session root
// that used to hide the mint).
//
// WHY THE BEFORE/AFTER IS THE ASSERTION (Rule 31, and the shape
// chat_replay_readonly_writes_test.go already established). The claim is not
// "replaymode.Active() is true" and not "the guard runs" — it is a claim about a
// DIRECTORY: the operator's real sessions tree, file for file, byte for byte,
// is the same after a replay-shaped run as before. So the case builds a real
// sessions root with a real recording in it, takes the tree's file list and the
// sha256 of every file, runs the run's own seam, and fails on any difference —
// an added file, a removed file, or a single changed byte.
//
// THE SEAM DRIVEN IS THE ONE THE VERB CALLS. `gopi chat` builds its agent through
// newAgent → agent.NewAgent, and the two lines inside NewAgent that MINT a run's
// record are agent.NewRawLoggerForRun and agent.NewSessionLoggerForRun
// (internal/agent/loop.go). This case calls those two with the same arguments
// NewAgent passes (the run uuid, no task id) instead of building a whole Agent,
// because a whole Agent also constructs a provider client — a dependency that
// has nothing to do with "does a replay write a session" and would make the
// assertion conditional on a network configuration. The minters ARE the mint;
// they are the functions whose MkdirAll/OpenFile create the artifacts.
//
// WHAT IS ASSERTED, in order:
//  1. both minters return (nil, nil) under a replay — the run has no record;
//  2. the operator's sessions tree is byte-identical (file list + sha256);
//  3. no new directory appeared anywhere under the profile root either — the
//     scratch tree the old relocation allocated lived inside the same profile,
//     and a replay must not mint one of those either (Rule 35: a replay run adds
//     no artifact for anyone to reclaim);
//  4. with the flag DISARMED the same two calls DO mint a record — otherwise a
//     minter that simply never works would pass this case for the wrong reason.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// replayMintTestRunUUID is the run uuid the case hands the minters — what
	// NewAgent mints for one run, and therefore what names the files a mint
	// WOULD create (<uuid>.raw.log beside session.jsonl in a bare layout).
	replayMintTestRunUUID = "5f2a7c1e-0000-4000-8000-00000000beef"
	// replayMintTestSessionDir is the recorded session this case replays: the
	// directory the operator would have on disk, in the bare layout
	// (sessions/<session-uuid>/session.jsonl).
	replayMintTestSessionDir = "11111111-2222-4333-8444-555555555555"
	// replayMintTestRecordingEntry is the file inside it that the replay reads
	// — named through sessionlayout so the case and the reader cannot disagree
	// about what a bare session's record is called.
	replayMintTestRecordingEntry = "session.jsonl"
	// replayMintTestRecordingBody is the recording's content: one recorded event
	// line, the shape a replay parses.
	replayMintTestRecordingBody = `{"type":"session_start","sessionId":"11111111-2222-4333-8444-555555555555"}` + "\n"
	// replayMintTestProfileFile is a second file in the profile, outside the
	// sessions tree, so the case measures a profile WITH content rather than an
	// empty home that any writer could pass by accident on the first write.
	replayMintTestProfileFile  = "settings.json"
	replayMintTestSettingsBody = `{"model":"test-model"}`
)

// replayMintTestProfile builds the scratch profile: a real sessions root holding
// the recording this case replays, plus one non-session file beside it. It
// returns the profile root and the recording's path.
func replayMintTestProfile(t *testing.T) (profileRoot, recordingPath string) {
	t.Helper()
	// Rule 17's spelling: the scope is the TEST, and the profile is what
	// config.GopiDir()/SessionsRoot() resolve from, so /home/user/.gopi is never
	// in the picture.
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir, err := config.GopiDir()
	if err != nil {
		t.Fatalf("the scratch profile could not be resolved: %v", err)
	}
	root, err := config.SessionsRoot()
	if err != nil {
		t.Fatalf("the scratch profile's sessions root could not be resolved: %v", err)
	}
	sessionDir := filepath.Join(root, replayMintTestSessionDir)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("the recorded session directory could not be created: %v", err)
	}
	recordingPath = filepath.Join(sessionDir, replayMintTestRecordingEntry)
	if err := os.WriteFile(recordingPath, []byte(replayMintTestRecordingBody), 0o644); err != nil {
		t.Fatalf("the recording could not be written: %v", err)
	}
	// The other files a real recorded session carries, so the tree the case
	// measures looks like the operator's: the raw HTTP log and the metadata the
	// mint writes. A replay must leave ALL of them alone, not just the JSONL.
	for name, body := range map[string]string{
		replayMintTestSessionDir + ".raw.log": "recorded raw log\n",
		"metadata.json":                       `{"session_id":"` + replayMintTestSessionDir + `"}`,
	} {
		if werr := os.WriteFile(filepath.Join(sessionDir, name), []byte(body), 0o644); werr != nil {
			t.Fatalf("seeding the recorded session with %s: %v", name, werr)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, replayMintTestProfileFile), []byte(replayMintTestSettingsBody), 0o644); err != nil {
		t.Fatalf("seeding the profile with %s: %v", replayMintTestProfileFile, err)
	}
	return home, recordingPath
}

// replayMintTreeDigest is a whole tree as an ordered list of
// "relative-path<TAB>sha256" lines — the ONE reader used for the before and the
// after halves, so the two are comparable by construction. Directories are
// entries in their own right (a "path<dir>" line), because "a replay created no
// directory" is half of the claim.
func replayMintTreeDigest(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			out = append(out, rel+"\t<dir>")
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		out = append(out, rel+"\t"+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("the tree %s could not be read for its digest: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// replayMintAssertSameTree fails, naming every difference, when before and after
// are not the same tree. The diff is printed rather than described, because "the
// tree changed" is exactly the kind of claim a reader must be able to check.
func replayMintAssertSameTree(t *testing.T, what string, before, after []string) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("%s: the file list changed across the replay — %d entr(y/ies) before, %d after\nbefore:\n%s\nafter:\n%s",
			what, len(before), len(after), strings.Join(before, "\n"), strings.Join(after, "\n"))
		return
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("%s: entry %d changed across the replay\n  before: %s\n  after:  %s", what, i, before[i], after[i])
		}
	}
}

// TestReplayMintsNoSessionRecord is the epic's own acceptance case: a replay
// creates nothing, and the tree it read is byte-identical afterwards.
func TestReplayMintsNoSessionRecord(t *testing.T) {
	profileRoot, recordingPath := replayMintTestProfile(t)
	sessionsRoot, err := config.SessionsRoot()
	if err != nil {
		t.Fatalf("the sessions root could not be resolved: %v", err)
	}

	beforeProfile := replayMintTreeDigest(t, profileRoot)
	beforeSessions := replayMintTreeDigest(t, sessionsRoot)

	replaymode.Arm(recordingPath)
	t.Cleanup(replaymode.Disarm)

	// THE SEAM: the two calls agent.NewAgent makes to give a run its record
	// (internal/agent/loop.go), with NewAgent's own arguments for a bare chat.
	rawLog, rawErr := agent.NewRawLoggerForRun("", replayMintTestRunUUID)
	if rawErr != nil {
		t.Fatalf("NewRawLoggerForRun under a replay returned an error (%v) — a replay is not a failure, it is a run that needs no HTTP record", rawErr)
	}
	if rawLog != nil {
		_ = rawLog.Close() //stylegate:ignore the case is already failing; closing what should not exist must not mask that
		t.Fatalf("NewRawLoggerForRun opened a raw log for a replay run (%s) — the run's whole purpose is to read a recording, and no HTTP exchange exists to record", rawLog.Path())
	}
	sessionLog, sessionErr := agent.NewSessionLoggerForRun("test-model", "", replayMintTestRunUUID, nil)
	if sessionErr != nil {
		t.Fatalf("NewSessionLoggerForRun under a replay returned an error (%v) — the callers treat this as best-effort and would print a failure for a run that is behaving", sessionErr)
	}
	if sessionLog != nil {
		_ = sessionLog.Close() //stylegate:ignore the case is already failing; closing the unexpected logger must not mask it
		t.Fatalf("NewSessionLoggerForRun minted a session for a replay run (%s) — the operator's sessions tree must gain nothing", sessionLog.Path())
	}

	replayMintAssertSameTree(t, "the sessions tree", beforeSessions, replayMintTreeDigest(t, sessionsRoot))
	replayMintAssertSameTree(t, "the operator's profile", beforeProfile, replayMintTreeDigest(t, profileRoot))

	// THE RECORDING ITSELF: the run read it, so its bytes must still be the bytes
	// the operator recorded (an append would be the unrecoverable half of this
	// defect — the artifact is the only copy of the session).
	got, rerr := os.ReadFile(recordingPath)
	if rerr != nil {
		t.Fatalf("the recording could not be re-read after the replay: %v", rerr)
	}
	if string(got) != replayMintTestRecordingBody {
		t.Fatalf("the recording changed across the replay:\n  before: %q\n  after:  %q", replayMintTestRecordingBody, string(got))
	}
	// And the session record the replay read is still the one the operator's
	// index resolves, through the tree's own accounting (sessionlayout).
	if path := sessionlayout.RecordPath(filepath.Dir(recordingPath)); path != recordingPath {
		t.Fatalf("the recorded session's own record no longer resolves to %s (%s) — the replay disturbed the tree's accounting", recordingPath, path)
	}
}

// TestSessionMintStillWorksOutsideAReplay is this case's CONTROL. Without it a
// minter that never minted at all — a typo that returned nil unconditionally —
// would pass the case above and take every real session's record with it. The
// same two calls, the flag disarmed, must produce a real session directory, a
// real JSONL and the metadata beside it.
func TestSessionMintStillWorksOutsideAReplay(t *testing.T) {
	profileRoot, _ := replayMintTestProfile(t)
	sessionsRoot, err := config.SessionsRoot()
	if err != nil {
		t.Fatalf("the sessions root could not be resolved: %v", err)
	}
	before := replayMintTreeDigest(t, sessionsRoot)

	replaymode.Disarm()
	rawLog, rawErr := agent.NewRawLoggerForRun("", replayMintTestRunUUID)
	if rawErr != nil {
		t.Fatalf("NewRawLoggerForRun outside a replay failed (%v) — the live path must keep its raw log", rawErr)
	}
	if rawLog == nil {
		t.Fatal("NewRawLoggerForRun outside a replay returned no logger — the guard is refusing every run, not just a replay")
	}
	defer func() {
		if cerr := rawLog.Close(); cerr != nil {
			t.Errorf("closing the live raw log: %v", cerr)
		}
	}()
	sessionLog, sessionErr := agent.NewSessionLoggerForRun("test-model", rawLog.Path(), replayMintTestRunUUID, nil)
	if sessionErr != nil {
		t.Fatalf("NewSessionLoggerForRun outside a replay failed (%v) — the live path must keep its session record", sessionErr)
	}
	if sessionLog == nil {
		t.Fatal("NewSessionLoggerForRun outside a replay returned no logger — the guard is refusing every run, not just a replay")
	}
	defer func() {
		if cerr := sessionLog.Close(); cerr != nil {
			t.Errorf("closing the live session log: %v", cerr)
		}
	}()

	after := replayMintTreeDigest(t, sessionsRoot)
	if len(after) <= len(before) {
		t.Fatalf("a live run minted nothing: the sessions tree had %d entr(y/ies) before and %d after\nbefore:\n%s\nafter:\n%s",
			len(before), len(after), strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
	// The record the live run minted must be IN this profile, under the
	// run-uuid's own directory — the layout NewAgent relies on, chosen by
	// sessionlayout rather than by a literal repeated here.
	if dir := filepath.Dir(sessionLog.Path()); dir != filepath.Join(sessionsRoot, replayMintTestRunUUID) {
		t.Fatalf("the live run's session record landed in %s, want %s", dir, filepath.Join(sessionsRoot, replayMintTestRunUUID))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(sessionLog.Path()), "metadata.json")); err != nil {
		t.Fatalf("the live run's session directory carries no metadata.json: %v", err)
	}
	if _, err := os.Stat(rawLog.Path()); err != nil {
		t.Fatalf("the live run's raw log is not on disk: %v", err)
	}
	_ = profileRoot
}
