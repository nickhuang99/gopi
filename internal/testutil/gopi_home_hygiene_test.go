package testutil

// The gopi-PROFILE-HYGIENE contract (epic 2026-09-12, "worker create logs not
// within designate path"). Two host-level shell entry points write into
// $HOME/.gopi and neither was covered by anything:
//
//   * scripts/gopi-worker-up.sh — the detached worker launcher. Its old
//     default logdir was the profile ROOT ("$HOME/.gopi"), so every launch
//     dropped worker-<ts>-<i>.log next to memory.db instead of into the
//     designated <gopi-dir>/logs/ area (where scratch-gc-watchdog.sh already
//     writes). A shell path default is invisible to `go test` — hence this
//     test: it drives the launcher's --dry-run seam with a SANDBOX HOME and
//     asserts the path contract directly.
//
//   * scripts/gopi-home-gc.sh — the retention sweep. Its one unacceptable
//     failure is deleting the LIVE database, so that is asserted as a hard
//     invariant against a synthetic profile whose live DB is hash-pinned.
//
// Both scripts are driven here through their own argument surface (never by
// reimplementing their logic): --dry-run / --home exist for exactly this.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
)

// Contract values shared with the launcher script (Rule 14: ONE definition,
// referenced by the test rather than mirrored as literals): gopiWorkerUpScript
// is the launcher's file name; gopiWorkerRetiredLogRootFlag is the option the
// launcher USED to accept and must now REFUSE (it is named here — not in
// internal/tools, which no longer defines it — because the launcher's own
// refusal message is the only place it still appears).
const (
	gopiWorkerUpScript           = "gopi-worker-up.sh"
	gopiWorkerRetiredLogRootFlag = "--worker-log-root"
	taskOutputLogSuffixDryRun    = "<task-id>/output.log"
)

// retiredWorkerLogName matches the RETIRED per-worker log file name — the
// timestamp-index shape `worker-<yyyymmdd>-<hhmmss>-<i>.log` whose absence is
// the point of this epic (a name carrying no task id). Anchored on the exact
// shape so the launcher's legitimate fixed `worker-startup.log` is not a
// false positive.
var retiredWorkerLogName = regexp.MustCompile(`worker-[0-9]{8}-[0-9]{6}-[0-9]+\.log`)

// runScript runs one repo script with a private sandbox HOME and returns its
// combined output, failing the test on a non-zero exit.
func runScript(t *testing.T, env []string, name string, args ...string) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	script := filepath.Join(root, scriptsDirName, name)
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, out)
	}
	return string(out)
}

// TestGopiWorkerUpAttributesLogsToSessionDirs is the regression test for the
// launcher's half of the output contract, RE-SCOPED TWICE: by epic
// worker-log-session-attribution (2026-09-12, which removed the flat public
// per-worker log file) and again by epic child-stream-task-output (2026-09-17,
// which removed the attribution ROOT the launcher used to hand out).
//
// The launcher now owns exactly ONE thing: its own pre-claim record. A task's
// text is written by the executor child into the ROW's session directory
// (<gopi-dir>/sessions/<task-id>/output.log) at the spawn seam, for every spawn
// path alike — so there is no root for a launcher to pass and no flag for it to
// spell. What this test pins is therefore (1) that the launcher announces the
// destination and passes NO output option, (2) that it keeps launching workers
// and appending to the ONE fixed pre-claim record, and (3) that the RETIRED
// option is refused loudly rather than silently ignored (Rule 15).
//
// WHY the per-worker file stays gone: `worker-<ts>-<i>.log` names a TIMESTAMP,
// not a task id, so given a failed task there is no path from the id to its
// output (concurrent workers' timestamps overlap — the 04:12 incident killed 6
// workers inside ~70 s).
func TestGopiWorkerUpAttributesLogsToSessionDirs(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	cases := []struct {
		name        string
		home        string
		gopiHome    string
		wantRootRel string // expected attribution root relative to the sandbox
	}{
		{name: "GOPI_HOME set", home: "sandbox-home", gopiHome: "sandbox-gopi", wantRootRel: "sandbox-gopi/.gopi/sessions"},
		{name: "GOPI_HOME unset", home: "sandbox-home", gopiHome: "", wantRootRel: "sandbox-home/.gopi/sessions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			home := filepath.Join(tmp, tc.home)
			repo := filepath.Join(tmp, "repo")
			binDir := filepath.Join(repo, "bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatalf("mkdir bin: %v", err)
			}
			// A stub binary: the launcher must be drivable without a real
			// worker (it only needs an executable at $REPO/bin/gopi).
			stub := "#!/bin/sh\necho \"stub worker ran: $*\"\n"
			if err := os.WriteFile(filepath.Join(binDir, "gopi"), []byte(stub), 0o755); err != nil {
				t.Fatalf("write stub: %v", err)
			}
			env := []string{
				"HOME=" + home,
				"GOPI_WORKER_REPO=" + repo,
			}
			// An explicitly empty GOPI_HOME means "unset" for the child (the
			// script tests [[ -n ${GOPI_HOME:-} ]]).
			if tc.gopiHome == "" {
				env = append(env, "GOPI_HOME=")
			} else {
				env = append(env, "GOPI_HOME="+filepath.Join(tmp, tc.gopiHome))
			}
			wantRoot := filepath.Join(tmp, tc.wantRootRel)

			// The path contract: N workers, NO output option, and the shape a
			// task's text takes (the row's own session directory).
			out := runScript(t, env, gopiWorkerUpScript, "3", "--dry-run")
			if !strings.Contains(out, taskOutputLogSuffixDryRun) {
				t.Fatalf("the dry-run must state where a task's output goes (%s), got:\n%s", taskOutputLogSuffixDryRun, out)
			}
			// The retired option must not be handed to any worker: the
			// destination belongs to the row, not to the launcher.
			if strings.Contains(out, gopiWorkerRetiredLogRootFlag) &&
				!strings.Contains(out, "retired") {
				t.Fatalf("a worker command still carries the retired output option, got:\n%s", out)
			}
			// The root the launcher used to announce is gone; what it announces
			// now is its OWN record.
			if strings.Contains(out, "output_root=") {
				t.Fatalf("the launcher still announces an attribution root it no longer owns:\n%s", out)
			}
			// THE REGRESSION: the retired per-worker, timestamp-named file
			// (worker-<yyyymmdd>-<hhmmss>-<i>.log) must not be announced
			// anywhere. The one legitimate stable record — the fixed
			// worker-startup.log — is deliberately NOT matched by this
			// pattern: it carries no timestamp and is appended, never
			// multiplied into a file per launch.
			for _, line := range strings.Split(out, "\n") {
				if retiredWorkerLogName.MatchString(line) {
					t.Fatalf("the retired per-worker public log path is back: %s", line)
				}
			}

			// The REAL launch (stub binary, so nothing claims a task): the
			// worker is started with NO output option, and the ONE fixed
			// pre-claim record is appended — never a file per launch.
			out = runScript(t, env, gopiWorkerUpScript, "1")
			// The worker's OWN output does not come back on the launcher's
			// stdout any more — that is the point: it is appended to the ONE
			// fixed pre-claim record (asserted below), never to a fresh
			// per-launch file.
			if !strings.Contains(out, taskOutputLogSuffixDryRun) {
				t.Fatalf("the launch must name where a task's output goes, got:\n%s", out)
			}
			startupLog := filepath.Join(strings.TrimSuffix(wantRoot, "/sessions"), "logs", "worker-startup.log")
			body, err := os.ReadFile(startupLog)
			if err != nil {
				t.Fatalf("the fixed pre-claim record %s must exist after a launch: %v\n%s", startupLog, err, out)
			}
			// The launched worker's argv carries NO output option (the stub
			// echoes "$*", so the assertion reads the real argv).
			if strings.Contains(string(body), gopiWorkerRetiredLogRootFlag) {
				t.Fatalf("the launched worker's argv still carries the retired option %s; %s holds: %q",
					gopiWorkerRetiredLogRootFlag, startupLog, body)
			}
			if !strings.Contains(string(body), "stub worker ran: worker") {
				t.Fatalf("the launcher must have started a worker; %s holds: %q", startupLog, body)
			}
			// Nothing may sit directly in logs/ except that stable record —
			// a second file means the per-worker pile is back.
			entries, err := os.ReadDir(filepath.Dir(startupLog))
			if err != nil {
				t.Fatalf("read logs dir: %v", err)
			}
			for _, e := range entries {
				if e.Name() != filepath.Base(startupLog) {
					t.Errorf("logs/ must hold only %s after a launch; found %s", filepath.Base(startupLog), e.Name())
				}
			}

			// THE NEGATIVE CASE (Rule 15, no silent failure): the retired
			// output-root option must be REFUSED with the replacement named —
			// a launcher spelled against the old contract must not get a
			// silently different layout.
			cmd := exec.Command("bash",
				filepath.Join(mustRepoRoot(t), scriptsDirName, gopiWorkerUpScript),
				"1", gopiWorkerRetiredLogRootFlag, wantRoot)
			cmd.Env = append(os.Environ(), env...)
			negOut, negErr := cmd.CombinedOutput()
			if negErr == nil {
				t.Fatalf("passing the retired output root must FAIL, got exit 0:\n%s", negOut)
			}
			neg := string(negOut)
			if !strings.Contains(neg, "unexpected argument") {
				t.Fatalf("the refusal must name the offending argument (Rule 15), got:\n%s", negOut)
			}
			if !strings.Contains(neg, taskOutputLogSuffixDryRun) {
				t.Fatalf("the refusal must name the replacement (%s), got:\n%s", taskOutputLogSuffixDryRun, negOut)
			}
			if strings.Contains(neg, "stub worker ran:") {
				t.Fatalf("NO worker may be launched behind a retired option:\n%s", negOut)
			}
		})
	}
}

// TestGopiHomeGcNeverTouchesTheLiveDB is the epic's second half as a hard
// invariant: the retention sweep, applied to a synthetic profile, must never
// select the live database (or its -wal/-shm) for deletion, must keep its
// bytes identical, must relocate the root clutter into backups/ and logs/, and
// must drop only the provably-empty files.
func TestGopiHomeGcNeverTouchesTheLiveDB(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	root := t.TempDir()
	home := filepath.Join(root, "gopi")
	if err := os.MkdirAll(filepath.Join(home, "backups"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	live := filepath.Join(home, "memory.db")
	write := func(rel string, data string) string {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		return p
	}
	write("memory.db", "LIVE-DB-CONTENT")
	write("memory.db-wal", "live-wal")
	liveSha := sha256File(t, live)
	// Root clutter: a snapshot (must move to backups/), a worker log (must
	// move to logs/), a zero-byte stub (must go).
	straySnapshot := write("memory.db.bak-pre-m1", "old-snapshot")
	strayWorkerLog := write("worker-20260911-042401-1.log", "worker output")
	zeroStub := write("gopi.db", "")
	// An orphan sidecar: its base was deleted long ago, so it can only be
	// garbage.
	orphanWal := write("memory.db.bak-gone-wal", "orphan")
	// A zero-byte stub INSIDE backups/: not a generation. If it were counted as
	// one it would occupy a keep-N slot and evict a real snapshot (observed on
	// the real profile), so it must be deleted outright.
	zeroBackup := write("backups/memory.db.bak-pre-legacy-purge-20260910-094506", "")
	// Ten backups with distinct ages: the newest 3 are kept, the rest pruned.
	old := time.Now().Add(-40 * 24 * time.Hour)
	// keptCount is 2, not 3: the stray root snapshot above is FRESH (written
	// seconds ago), so ② counts it as a backup generation in its own right —
	// that is exactly what "prune the post-relocation view" means. 11
	// candidates, newest 3 survive: gen00, gen01 and the stray; gen02..gen09
	// go. Before the ordering fix this assertion could not even be written,
	// because the stray was not a candidate until the NEXT sweep.
	keptCount := 2
	pruned := []string{}
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("memory.db.bak-gen%02d", i)
		p := write(filepath.Join("backups", name), fmt.Sprintf("backup-%d", i))
		// gen00 is newest; older gens are steadily older than the age rule.
		mtime := old.Add(time.Duration(i) * -24 * time.Hour)
		if i < keptCount {
			mtime = time.Now().Add(time.Duration(-i) * time.Minute)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		if i >= keptCount {
			pruned = append(pruned, name)
		}
	}

	// Dry run first: nothing may change.
	before := snapshotTree(t, home)
	out := runScript(t, []string{"HOME=" + root}, "gopi-home-gc.sh", "--home", home, "--keep", "3", "--age", "14")
	if snap := snapshotTree(t, home); snap != before {
		t.Fatalf("dry run changed the profile:\nbefore=%s\nafter =%s\n%s", before, snap, out)
	}
	if strings.Contains(out, "delete "+live+" ") {
		t.Fatalf("dry run proposes deleting the LIVE db:\n%s", out)
	}

	// Apply.
	out = runScript(t, []string{"HOME=" + root}, "gopi-home-gc.sh", "--home", home, "--keep", "3", "--age", "14", "--apply")
	if got := sha256File(t, live); got != liveSha {
		t.Fatalf("the LIVE db changed: %s != %s\n%s", got, liveSha, out)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("the LIVE db is gone: %v\n%s", err, out)
	}
	if _, err := os.Stat(live + "-wal"); err != nil {
		t.Fatalf("the live -wal is gone: %v\n%s", err, out)
	}
	// Relocations.
	if _, err := os.Stat(filepath.Join(home, "backups", filepath.Base(straySnapshot))); err != nil {
		t.Errorf("root snapshot was not relocated into backups/: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "logs", filepath.Base(strayWorkerLog))); err != nil {
		t.Errorf("root worker log was not relocated into logs/: %v\n%s", err, out)
	}
	if _, err := os.Stat(straySnapshot); !os.IsNotExist(err) {
		t.Errorf("root snapshot still at the profile root")
	}
	// Deletions: only the provably-empty stub and the orphan sidecar.
	if _, err := os.Stat(zeroStub); !os.IsNotExist(err) {
		t.Errorf("zero-byte stub survived")
	}
	if _, err := os.Stat(zeroBackup); !os.IsNotExist(err) {
		t.Errorf("zero-byte backup stub survived (it would hold a keep-N slot)")
	}
	if _, err := os.Stat(orphanWal); !os.IsNotExist(err) {
		t.Errorf("orphan -wal survived")
	}
	// Retention: the two newest survive, the older ones are pruned.
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("memory.db.bak-gen%02d", i)
		_, err := os.Stat(filepath.Join(home, "backups", name))
		if i < keptCount && err != nil {
			t.Errorf("newest backup %s was pruned: %v", name, err)
		}
		if i >= keptCount && err == nil {
			t.Errorf("old backup %s survived the sweep", name)
		}
	}
	// The stray root snapshot is the one that exposes the ordering bug: ① homes
	// it and ② must judge it in the SAME run. Fresh, so within keep-N — it is
	// here, at its new address, having been counted once.
	if _, err := os.Stat(filepath.Join(home, "backups", filepath.Base(straySnapshot))); err != nil {
		t.Errorf("stray root snapshot missing from backups/ after one run: %v", err)
	}
	for _, p := range pruned {
		if strings.Contains(out, "delete "+filepath.Join(home, "backups", p)) {
			t.Logf("pruned as planned: %s", p)
		}
	}
}

// TestGopiHomeGcPrunesInTheSameRunItRelocates is the regression test for the
// ordering bug found by driving the script (not by reading it): ② the backups
// prune ran against the CURRENT contents of backups/, while ① the root
// relocations were still only PLANNED. So a first sweep homed stray snapshots
// and left them there — the user's complaint ("backups should be cleaned up
// regularly") came back one cycle later, and an aged snapshot sitting at the
// root survived a full extra week.
//
// The setup is deliberately SENSITIVE to that order: every snapshot is at the
// ROOT and all but one are far older than the age rule, so the age rule alone
// allows exactly one survivor — and --keep 1 pins the same expectation through
// the keep-N rule, so the two rules cannot mask each other. If ② ignores the
// planned relocations, backups/ after run 1 is non-empty (the pre-fix
// behaviour, measured on this exact sandbox: relocated=4, deleted=0 on run 1,
// then run 2 deleting 2 — i.e. convergence took two weekly cycles).
func TestGopiHomeGcPrunesInTheSameRunItRelocates(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "gopi")
	if err := os.MkdirAll(filepath.Join(home, "backups"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "memory.db"), []byte("LIVE"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}
	aged := []string{"memory.db.bak-old20", "memory.db.bak-old18", "memory.db.bak-old16"}
	for i, name := range aged {
		p := filepath.Join(home, name)
		if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		mt := time.Now().Add(-time.Duration(20-2*i) * 24 * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	fresh := filepath.Join(home, "memory.db.bak-fresh")
	if err := os.WriteFile(fresh, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("write fresh: %v", err)
	}

	out := runScript(t, []string{"HOME=" + root}, "gopi-home-gc.sh",
		"--home", home, "--apply", "--keep", "1", "--age", "14")

	// ONE run must decide both halves: nothing left at the root, and the
	// surviving set is exactly the age-rule survivor.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read home: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "memory.db.bak") {
			t.Errorf("snapshot still at the profile root after one --apply: %s\n%s", e.Name(), out)
		}
	}
	backups, err := os.ReadDir(filepath.Join(home, "backups"))
	if err != nil {
		t.Fatalf("read backups: %v", err)
	}
	var survivors []string
	for _, e := range backups {
		survivors = append(survivors, e.Name())
	}
	if len(survivors) != 1 || survivors[0] != "memory.db.bak-fresh" {
		t.Fatalf("want only the fresh snapshot to survive one run, got %v\n%s", survivors, out)
	}
	// And the prune must be real, not reported: the aged files are gone.
	for _, name := range aged {
		if _, err := os.Stat(filepath.Join(home, "backups", name)); !os.IsNotExist(err) {
			t.Errorf("aged snapshot %s was not pruned in the run that relocated it", name)
		}
	}
	if !strings.Contains(out, "pruned=3") {
		t.Errorf("one run should report relocated=1 pruned=3 (keep=1 keeps only the fresh one), got:\n%s", out)
	}
}

// TestGopiHomeGcDedupesByteIdenticalGenerations covers ②b. The retention clock
// alone is not enough for the reported "too many backups": a batch loop
// produced a family of snapshots of the SAME state (48 MB each) inside one
// night, all well inside the age window, so every one of them was "young
// enough to keep". Byte-identical copies protect nothing extra, so only the
// newest may survive. Measured on the HP peer: that was one 13-file family of
// exactly 48488448 bytes / 646 MB, and hashing every member (the size collides,
// the bytes do not always) showed 10 distinct states -> 3 real duplicates.
func TestGopiHomeGcDedupesByteIdenticalGenerations(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "gopi")
	if err := os.MkdirAll(filepath.Join(home, "backups"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "memory.db"), []byte("LIVE"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}
	// The HP shape: a family of snapshots, SAME bytes, minutes apart — plus one
	// distinct, more recent generation that must never be touched.
	same := []string{
		"memory-glm-audit.db.bak-20260910-034528",
		"memory-glm-audit.db.bak-batch01",
		"memory-glm-audit.db.bak-batch06-20260910-043837",
		"memory-glm-audit.db.bak-20260910-050744",
	}
	base := time.Now().Add(-2 * time.Hour)
	for i, name := range same {
		p := filepath.Join(home, "backups", name)
		if err := os.WriteFile(p, []byte("IDENTICAL-AUDIT-DB"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	distinct := filepath.Join(home, "backups", "memory.db.bak-distinct-state")
	if err := os.WriteFile(distinct, []byte("A-DIFFERENT-STATE"), 0o644); err != nil {
		t.Fatalf("write distinct: %v", err)
	}

	out := runScript(t, []string{"HOME=" + root}, "gopi-home-gc.sh",
		"--home", home, "--apply", "--keep", "8", "--age", "14")

	// keep=8/age=14 would retain ALL five: the prune here can only come from ②b.
	entries, err := os.ReadDir(filepath.Join(home, "backups"))
	if err != nil {
		t.Fatalf("read backups: %v", err)
	}
	var survivors []string
	for _, e := range entries {
		survivors = append(survivors, e.Name())
	}
	if len(survivors) != 2 {
		t.Fatalf("want 2 survivors (newest of the duplicate family + the distinct one), got %v\n%s", survivors, out)
	}
	// The NEWEST of the identical family is the one kept (list is newest-first).
	kept := "memory-glm-audit.db.bak-20260910-050744"
	if _, err := os.Stat(filepath.Join(home, "backups", kept)); err != nil {
		t.Errorf("the newest duplicate generation %s was not the one kept: %v", kept, err)
	}
	if _, err := os.Stat(distinct); err != nil {
		t.Errorf("a distinct generation was deleted: %v", err)
	}
	if !strings.Contains(out, "duplicate generations") {
		t.Errorf("the plan should report the dedupe class, got:\n%s", out)
	}
	// Idempotent: a second run has nothing left to do.
	out2 := runScript(t, []string{"HOME=" + root}, "gopi-home-gc.sh",
		"--home", home, "--apply", "--keep", "8", "--age", "14")
	if !strings.Contains(out2, "pruned=0") {
		t.Errorf("second run should be a no-op, got:\n%s", out2)
	}
}

// sha256File hashes a file (compared before/after the sweep).
func sha256File(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// snapshotTree renders a stable "path=size,sha256" listing of a directory tree,
// so a dry run that wrote ANY byte is caught.
func snapshotTree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		lines = append(lines, rel+"="+sha256File(t, p))
		return nil
	})
	return strings.Join(lines, "\n")
}

// mustRepoRoot resolves the repo root for a test that needs to run a script
// directly (the runScript helper's own resolution, exposed for the
// negative-path case where a non-zero exit is expected).
func mustRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}
