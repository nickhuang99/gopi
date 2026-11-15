<!-- doc-class: normative -->
# Devdoc: the test that wrote into the operator's live profile

**Branch:** `fix/rawlog-test-home-leak` · **Commit:** `dbbe5329`
**Files:** `internal/testutil/profileguard.go` (new), `internal/testutil/profileguard_test.go` (new),
`internal/gopilog/rawlog_test.go`, `internal/gopilog/rounds_test.go`

## The defect (measured, 2026-09-12)

A read-only verification run of `go test ./internal/gopilog/` **rewrote the
operator's live raw log.** Evidence, not inference:

```
$ ls -la ~/.gopi/raw/rounds-test.log
… md5 6d44ff811cba043c54574606fe387248
… 23:26:05  →  23:40:29      # mtime moved by the "read-only" run
```

Seven tests in `rounds_test.go` call `writeRawLog`, a helper that resolves

```go
filepath.Join(os.Getenv("HOME"), ".gopi", "raw")
```

Every one of the seven omitted `t.Setenv("HOME", t.TempDir())`, so `$HOME` was
still the operator's, and the helper dutifully created/overwrote a real file
in the live profile. This is the *second* instance of the class — commit
`f6694fc8` did the same thing for the web-signal test.

Root cause is not "a missing line in seven tests": it is that **a path that
resolves through `$HOME` is only hermetic while the caller remembers to
sandbox it**, and nothing in the code said so. Seven copies of that memory
failed at once.

## The fix — a GUARD at the write seam, anchored outside the test

`internal/testutil/profileguard.go`:

| Function | Job |
| --- | --- |
| `OperatorProfileDir()` | the operator's live `<home>/.gopi` |
| `PathLiesInOperatorProfile(path)` | is this path in it? (component-wise) |
| `RefuseOperatorProfileWrite(t, what, dir)` | **refuse**, naming the seam and the fix |

The design decision that makes the guard work at all:

> **The anchor is `os/user`'s `HomeDir`, never `os.UserHomeDir()`.**

`os.UserHomeDir()` prefers `$HOME` since Go 1.12 — so inside a leaked test it
*is* the operator's home, which is precisely the case being detected. A guard
that reads the same knob as the code it guards agrees with every leak. The OS
user record (passwd / directory service) is the one identity a test cannot
fake; verified to ignore a `HOME` override under **both** `CGO_ENABLED=0` and
`CGO_ENABLED=1`.

Three more decisions, each with the alternative it rejected:

1. **Refusal, not silent re-point** (Rule 15). Re-pointing the write at a
   `t.TempDir()` would make the suite green while leaving the test's intent
   broken — the operator would never learn the sandbox failed to engage.
2. **An unverifiable guard FAILS** (Rule 15b). If `user.Current()` errors, the
   test fails rather than skips: a guard that cannot run is the silent
   permission the contract exists to remove.
3. **`scratch.PathWithin`, not `strings.HasPrefix`.** A sandbox is routinely a
   *sibling* (`~/.gopi-backup`, or a TempDir whose name merely contains the
   profile string). A prefix check flags those, turns every hermetic test red,
   and gets the guard deleted — the opposite of Rule 13's intended failure
   mode. Component-wise comparison keeps them usable.

The guard lives at the seam (`writeRawLog`'s own write), so the assurance is
**inherited by every future caller**, not re-asserted per test. A caller that
forgets to sandbox is now loud instead of destructive.

## Verification

- **RED-PROOF (captured, then restored):** reverting the seven `HOME`
  overrides makes `TestParseHTTPRoundsBaseStreaming` FAIL with
  `writeRawLog: refusing to write into the OPERATOR'S LIVE gopi profile "/home/user/.gopi/raw"`.
  The guard is therefore load-bearing, not decorative.
- **GREEN:** `go test ./internal/gopilog/` → ok. Real-profile fingerprint
  (`ls -la` md5 of `~/.gopi/raw`) is `6d44ff811cba043c54574606fe387248`
  **before and after** a full package run; `rounds-test.log` / `fmt-detail.log`
  mtime **and** size unchanged.
- `internal/testutil/profileguard_test.go` — 4 tests: the anchor must not
  follow a sandbox override; the refusal catches the live profile bare, with a
  trailing separator, and via a `..` detour; the negative case pins that a
  sibling sandbox is NOT flagged.

## Not done (honest)

- **`GopiProfileDirName` is a second definition site.** `internal/config` also
  joins `".gopi"` (it cannot import `testutil` — a test-support package).
  Rule 14 says the consolidation belongs in a small leaf constants package
  (`internal/gopipaths` or similar). Deliberately **not** bundled here: a new
  package plus the config rewrite is its own change, and this fix is the leak.
- The remaining `$HOME`-resolving helpers elsewhere in the tree are not
  audited; the guard only protects the seams it is wired into. An audit that
  adds `RefuseOperatorProfileWrite` to every profile-writing test seam is the
  natural follow-up.
- Pre-existing master failures are unrelated and reproduce on `949bf47f`:
  `TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent`
  (the latter caused by untracked `briefings/*.md`).
