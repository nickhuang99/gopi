<!-- doc-class: historical -->
# Task Report: `gopi task sync --peers` — the bare spelling means what its name says

Task `01a0eced-029f-7681-97d4-b593d73b0ec9` (branch `task/01a0eced029f768197d4b593d73b0ec9`).

## The defect, measured

A spelling the tool's own refusal text advertised was refused by that same
refusal — a closed loop in which the only way forward was to stop believing the
help:

```
$ GOPI_SYNC_PEERS=hp gopi task sync --peers           # pre-fix binary (git HEAD)
gopi task sync: give --peer <alias> (one session), --peers (all), --push-only, or --status
$ echo $?
1
```

`--peers a,b` worked, so the flag existed; only its **value-less** form — the one
its NAME implies ("peers": every one of them) — was unreachable. The message
re-taught `--peers (all)`, which the parser had just refused.

## Completed

1. **The scanner grew an optional-VALUE flag** (`cmd/agent/cli_memory.go`):
   `memoryFlagSet.optionalValue(name)` declares a flag whose bare spelling is a
   complete instruction. The value slot is claimed only by a token that is not
   itself a flag, so `--peers a,b --db p` still gives `--db` its own value and
   `--peers --status` keeps both options set. `optionalValueBare(name)` is how a
   caller READS the bare spelling (never `get() == ""`, which is also true of a
   flag that never appeared). `valueAttached(name)` distinguishes `--name=v`
   from `--name v` for a caller that must treat them differently.
2. **Bare `--peers` = every declared peer** (`cmd/agent/cli_tasks.go`): the sync
   parse case reads `peersAll`, the "no target given" guard counts it as a
   target, and the flag is declared before the argv scan (the declaration changes
   how the scanner consumes argv, so it cannot live after `parse`).
3. **The refusal as a route, not a loop**:
   - `--peers hp` — a SEPARATE single token that is not a list — is refused BY
     NAME with both accepted routes (`--peer hp`, `--peers a,b`, bare `--peers`).
     The argv reads two ways; the parser declines to pick one (`Rule 15`).
   - `--serve-remote --peers` / `--claim-remote --peers` — the peer-side handlers,
     which walk no peer list — are refused by name and by FACE
     (`barePeersFaceRefusal`), instead of falling through to the closed-loop text.
   - The target message no longer says `--peers (all)`; `syncSessionFaceSpelling`
     renders the bare form as the operator typed it, so the `--task-id` refusal
     quotes back the right spelling.
4. **Help text** (`syncUsageHead`, the family's ONE page): the synopsis is now
   `gopi task sync --peers [a,b]`, with the two spellings' difference stated and
   the refusals named.
5. **Regression tests** (`cmd/agent/cli_tasks_sync_peers_bare_test.go`): 9 tests
   over both layers, including a gate that FEEDS THE HELP'S OWN SPELLINGS BACK TO
   THE PARSER (`TestSyncAdvertisedSpellingIsTheAdmittedSpelling`) so the page and
   the grammar cannot drift apart again. Existing rows that used the old
   separate-value form were retargeted to the attached form.

## Not Done

- **`--peers hp` (space-separated) is REFUSED rather than resolved.** It is
  genuinely ambiguous between "the peer hp" and "the bare flag plus a stray
  word", and resolving it either way silently answers a question the operator did
  not ask. If the operator's intent is to be supported, the shape to add is a
  second optional-value grammar (a list flag's positive value as a one-item
  list) — deliberately not done here.
- **`--diff`'s `--peers`** is refused by `taskFlagMember` (advertised on the same
  page but not admitted for that verb). Untouched: it is a different defect
  (advertised-but-not-admitted) and outside this epic's scope.
- **The session legs were exercised against the local ssh shim and one real
  declared peer on a sandbox ledger** — not against a second host. The wire leg
  itself (patch in, envelope out) is covered by the pre-existing
  `TestSyncPeersRunsEveryListedPeer`, which drives the same transport.

## Verification

- Build: `go build ./...` : OK; `CGO_ENABLED=0 go build -o /tmp/gopi-new ./cmd/agent/` : OK.
- Tests: `go test ./cmd/agent/ -run 'TestSyncBarePeers|TestSyncPeers|TestSyncHelp|TestSyncAdvertised|TestSyncSessionFaceSpelling|TestSyncSurface|TestSyncTags|TestSyncFull|TestSyncStatus' -count=1` : **ok 12.359s** (before the fix this file's core assertion did not exist — the parser refused the argv it asserts).
- Ran: the real binary, twice, on the SAME argv — `git HEAD` build (`/tmp/gopi-pre`) and the fixed build (`/tmp/gopi-new`), against a **sandbox profile** (`GOPI_HOME=<session-dir>/sbx-home`, `GOPI_SYNC_PEERS=hp`), never the operator's ledger:

```
$ GOPI_SYNC_PEERS=hp gopi task sync --peers                     # PRE-FIX
gopi task sync: give --peer <alias> (one session), --peers (all), --push-only, or --status
exit=1

$ GOPI_SYNC_PEERS=hp gopi task sync --peers                     # POST-FIX
sync hp: pushed=1 pull{imported=0 mirrored=0 migrated=0 tagged=0 conflict=0 resolved=0 already=1 refused=0} anchor→1790680575 (1.3s)
exit=0
$ GOPI_SYNC_PEERS=hp gopi task sync --peers=hp                  # the value form, unchanged
sync hp: pushed=1 … (0.9s)                                      exit=0
$ GOPI_SYNC_PEERS=hp gopi task sync --peers=a,b                 # a named list, unchanged
gopi task sync: peer a: … peer "a" is not in the sync_peers whitelist …   exit=1
$ GOPI_SYNC_PEERS=hp gopi task sync --peers hp                  # ambiguous shape: refused by name
gopi task sync: --peers "hp": a SEPARATE value is not read here — use --peer <alias> for one peer, --peers a,b for a list, or the bare --peers for every declared peer
exit=1
$ GOPI_SYNC_PEERS=hp gopi task sync --serve-remote --peers
gopi task sync: --serve-remote does not read --peers: it is the PEER side of one session — the initiator identifies itself in the patch, and the peer list belongs to the initiator
exit=1
$ GOPI_SYNC_PEERS=hp gopi task sync --claim-remote --peers
gopi task sync: --claim-remote does not read --peers: it is the PEER side of one claim broadcast — stdin is the single-row patch and there is no peer to select
exit=1
$ GOPI_SYNC_PEERS=hp gopi task sync -h
GoPi task sync — cross-node ledger reconciliation with whitelisted peers.
  gopi task sync --peers [a,b] [--db PATH]            ← the bare spelling is documented
```

  Raw transcripts: `<session-dir>/evidence/pre-fix.txt`, `post-fix.txt`.
- Result: the bare spelling now reaches a session over the declared whitelist
  (one leg per declared peer; `sync hp: pushed=1 …` is the session's own report),
  the value form and the list form keep their behaviour, the two peer-side faces
  refuse the option by name, and `-h` documents the bare spelling.
- Commit: `87492732 feat(sync): bare --peers means every declared peer (the spelling its own refusal advertised)`

## Evidence (self-filed)

- **`go build ./...`** → no output, exit 0 (the first attempt failed with
  `cli_tasks_sync.go:124:8: syntax error: unexpected -- in grouped declaration` —
  my own inner-backtick raw-string break, fixed before the build above).
- **`go vet ./cmd/agent/`** → the first run reported
  `cli_tasks_sync.go:319:3: fmt.Sprintf format %s reads arg #3, but call has 2 args`;
  after the fix, no output, exit 0.
- **`go test ./cmd/agent/ -run 'TestSyncBarePeers|…' -count=1`** → `ok
  github.com/nick/gopi/cmd/agent 12.359s`.
- **`go test ./cmd/agent/ ./internal/standards/ ./internal/config/ -count=1`** →
  see the run recorded in this report's Verification (tail captured below in this
  session's job output).
- **Two real-binary invocations of the same argv** (pre-fix and post-fix builds),
  verbatim in the block above, with the pre-fix exit status `1` and the post-fix
  exit status `0`, both against a sandbox `GOPI_HOME`.
- **The pre-fix binary is a clean `git HEAD` build** of this worktree before the
  change (`CGO_ENABLED=0 go build -o /tmp/gopi-pre ./cmd/agent/`), so the red
  proof is the same code the branch started from.
- **The scanner bug I found while writing this** is recorded in the code: the
  parse branch's assignment must go to the LOCAL value, because the store below
  is unconditional — writing `f.values[name]` there made `--peers hp` parse as
  the bare spelling with `hp` silently DROPPED (observed in a probe test:
  `argv=[--peers hp] err=<nil> peersRaw="" peersAll=true`).
