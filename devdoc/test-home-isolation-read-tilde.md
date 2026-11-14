<!-- doc-class: normative -->
# Test Report: TestRead_TildeExpansion no longer writes into the developer's real $HOME

Ticket: test-code-discipline audit §1.3 (`devdoc/test-code-discipline-audit.md:117-128`), epic
"bug(test): test code writes scratch into the developer's REAL $HOME".

## Root cause

`internal/tools/read_test.go` exercised `~/` expansion by **writing into the real home**:

```go
home, _ := os.UserHomeDir()
fileName := "gopi-read-test-" + randomSuffix() + ".txt"   // randomSuffix() == filepath.Base(os.TempDir())
path := filepath.Join(home, fileName)
os.WriteFile(path, ...)                                   // real ~/ !
defer os.Remove(path)                                     // the only thing that cleans it
```

Two defects in one site:

1. **The real home is the one tree on this host that is not scratch** — every other test path
   goes through `testutil` scratch (tmpfs, GC'd, off `/`). A write into `~` is safe *only*
   because of the `defer`: any path that skips the defer (`os.Exit`, a panic in another test of
   the same binary, `SIGKILL` of the test binary) leaves a stray file in the developer's state.
2. **`randomSuffix()` was not random** — it returned `filepath.Base(os.TempDir())`, i.e. the
   same constant string for every call in a run, so the "unique" name was a fixed name.

The fixture home was available all along: `expandHome` (`internal/tools/tilde.go`) resolves
`~` via `os.UserHomeDir`, which honours `$HOME` — so `t.Setenv("HOME", t.TempDir())` redirects
expansion into a throwaway directory. That is already the established pattern in
`internal/tools/tilde_test.go` (`TestExpandHome_BareTilde`, `TestEdit_TildeExpansion`).

## Completed

- `internal/tools/read_test.go`
  - `TestRead_TildeExpansion` rewritten: a fixture `$HOME` (`t.TempDir()`, mode tightened to
    0700 after writing the fixture file) is installed with `t.Setenv`; the file is created and
    read *inside* that fixture, so `~/x` expansion is still proven end-to-end while the real
    home is never written.
  - Added the `$HOME` before/after assertion the acceptance asks for: `homeEntryNames()` lists
    the real home before the fixture install and after the read; any new entry fails the test.
    The real home is therefore only *read* — never entered by the tool.
  - Added `TestHomeEntryNamesProbe_DetectsNewEntry`: the probe itself is not vacuous — it is
    shown to *detect* an added entry (on a `t.TempDir()` listing), so a broken listing can never
    make the real-home assertion silently pass.
  - `TestRead_TildeAlone` (same class: it also resolved the real `~` via `os.UserHomeDir`) now
    runs against a fixture home; the bare-`~`-is-a-directory contract is unchanged.
  - Deleted `randomSuffix()` — dead after the rewrite (its remaining callers: none; grep-verified).
- `internal/tools/tilde_test.go`
  - `TestRead_TildeAloneStillDirectoryError` and `TestEdit_TildeExpansion` no longer look up the
    real home with `os.UserHomeDir()` first (the `_ = home` idiom proved it was unused); they
    install a fixture `$HOME` only.
  - New shared constant `homeEnvName = "HOME"` in `read_test.go` — the env seam named once
    instead of the `"HOME"` literal repeated per test (Rule 1b/13).
- `internal/tools/write_test.go:94` was inspected and **left alone**: its `os.UserHomeDir()` is
  a guarded precondition (`t.Skip` when home is unresolvable) before it redirects `$HOME` to a
  `t.TempDir()`, so it never writes to the real home.

Production code is untouched (`internal/tools/tilde.go` and every non-test file are unchanged).

## Not Done

- No mechanical gate was added for this class (a test that calls `os.UserHomeDir()` and then
  writes under it). The existing Go `/tmp` gate (`internal/testutil/scripts_gate_test.go`)
  covers path *location*; a real-home write check would be a new predicate with its own
  false-positive surface (many legitimate `os.UserHomeDir()` reads). Left to the audit owner.
- The other `os.UserHomeDir()` test sites were inspected and are read-only, so they were left
  alone: `internal/config/gopihome_test.go:30,48` (compares `GopiDir()` against `$HOME/.gopi`,
  writes nothing), `testsuite/webfetch_contract_test.go:275` (reads the deployed plugin script).
- The remaining audit classes (49 `os.Setenv` sites §2.1, 8 unchecked `os.Chdir` restores §2.2)
  are separate tickets and are not touched here.
- `go test ./internal/standards/` is RED on this host — but it is red on **master too**, not from
  this branch: the Rule 20 escape on `internal/shot/x11.go:744` (`getRootImage`) and the Rule 19
  escape on `internal/agent/crashdump.go:198` fire identically in the main repo at `master`
  (`870e5ed`/`d78b83e`), and `git diff master -- internal/standards/ internal/shot/x11.go` on
  this branch is empty. Pre-existing master red, owned by the standards work, not by this fix.

## Verification

```
$ gofmt -l internal/tools/read_test.go internal/tools/tilde_test.go     # (no output)
$ go vet ./internal/tools/                                              # OK
$ CGO_ENABLED=0 go build ./...                                          # OK (no output)

# acceptance: the tilde tests pass AND the real $HOME is untouched
$ ls -A ~ > /tmp/home-before-final.txt && wc -l < /tmp/home-before-final.txt
104
$ go test -count=1 -run 'TestRead_Tilde' -v ./internal/tools/
--- PASS: TestRead_TildeExpansion (0.00s)
--- PASS: TestRead_TildeAlone (0.00s)
--- PASS: TestRead_TildeAloneStillDirectoryError (0.00s)
ok  github.com/the operator/gopi/internal/tools

$ go test -count=1 ./internal/tools/          # full package suite
ok  github.com/the operator/gopi/internal/tools  284.912s

$ ls -A ~ > /tmp/home-after-final.txt && diff /tmp/home-before-final.txt /tmp/home-after-final.txt
HOME-UNCHANGED
$ ls -A ~ | grep -c 'gopi-read-test'
0
```

**Red proof (the probe is live, not vacuous).** Re-run on the merged tree (worktree
`~/workspace/gopi-read-tilde-home`, HEAD = master + 626360a): a temporary
`os.WriteFile(realHome + "/gopi-redproof.txt")` inserted before the assertion made the test fail
with exactly one diff entry, then the injection was reverted (the file was removed by the
injection's own `defer`, and `ls -A ~ | grep -c gopi-read-test` = 0):

```
--- FAIL: TestRead_TildeExpansion (0.00s)
    read_test.go:144: test must not touch the real home /home/user:
         before [ ... gopi-ab-raw-b gopi-resume-demo ... ]
         after  [ ... gopi-ab-raw-b gopi-redproof.txt gopi-resume-demo ... ]
FAIL
```

A second, permanent guard makes the probe itself non-vacuous: `TestHomeEntryNamesProbe_DetectsNewEntry`
proves the listing *changes* when an entry appears, so a broken `homeEntryNames` cannot make the
real-home assertion pass silently.

Commit: 626360a (initial fix) + the follow-up commit on this branch (merged master
`d78b83e` + the non-vacuous-probe test). See the recovery line below.

## Deliverable / recovery

- Branch: `fix/read-tilde-test-real-home`
- Recovery: `git show fix/read-tilde-test-real-home:internal/tools/read_test.go`
  (worktree `~/workspace/gopi-read-tilde-home`, shared object store — the commit is already in
  the main repo's refs, no push needed).
