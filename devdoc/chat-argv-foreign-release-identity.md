<!-- doc-class: historical -->
# The chat argv fixture names the pipe a foreign release took

Task `01a1077a-dcc6-78c1-a119-aab23e0141ab`. The standing claim this doc makes
about master: **a foreign release of the argv fixture's descriptors is reported
with the identity of the pipe that was lost, and a census that could not run is
never reported as a census that found nothing.** The machine check is
`TestChatRunArgsIdentityCensusNamesTheLostPipe` plus the two existing
foreign-release guards.

## The report a reader gets

Before this change every occurrence of the family read the same way, whatever
pipe it was about:

```
chatRunArgs: the stdout stream's WRITE end (fd 7, opened by THIS fixture) was
already released when this fixture came to close it (kernel reading "",
resolvable=false) — released by a path OUTSIDE this fixture.
```

`fd 7` is a HANDLE, and at the moment of the report it names nothing (the
kernel's reading is `""`), so the sentence could not say which of the pipes the
fixture opened had been taken, and no other observation — a `strace` close, an
`lsof` row, a sibling occurrence — could be matched against it. Measured after
the change, same two guards, same shape:

```
chatRunArgs: the stderr stream's read end (fd 3, identity pipe:[55434216]) was
released by a path OUTSIDE this fixture … No other descriptor in this process
holds pipe:[55434216].

chatRunArgs: the stdout stream's WRITE end (fd 7, identity pipe:[55434221],
opened by THIS fixture) was already released … released by a path OUTSIDE this
fixture. No other descriptor in this process holds pipe:[55434221].
```

`pipe:[inode]` is the kernel's own name for the pipe OBJECT, and it is the
string that appears in every other sighting of that pipe. The identity is
recorded while the fixture still certainly holds the number
(`chatRunArgsDescriptorIdentity`), because a released number answers `""`.

## Why the identity, and why a census

The deliverable's question was "who releases the fixture's fd". The honest
answer this run can support has three parts, and the change is shaped by them.

**1. The release is a NUMBER release, not a wrapper finalizer.** The fixture
holds both wrappers until the release: `outW`/`errW` are assigned to
`os.Stdout`/`os.Stderr` (`chat_argv_test.go`, `chatRunArgs`) and are read again
afterwards — `int(outW.Fd())` for the verdict and `outW.Close()` for the
release itself. A GC finalizer cannot run on a reachable object, so no
`os.NewFile` wrapper of this fixture's was collected and none closed this
number. An outside path released the NUMBER. This is the elimination that rules
out the family's best-known actor (`internal/borrowedfd` — a dropped wrapper's
finalizer) for THIS occurrence, and it is why the fix is instrumentation rather
than a `Detach` call at one of the production wrapped-fd sites.

**2. The observable damage is the product's OWN status contract.** The case
that failed asserts `runChat([--tools -h])` returns `chatExitOK`. The help path
is `cli_chat.go`:

```go
if helpWanted && len(rest) == 0 {
	if _, werr := os.Stdout.WriteString(chatUsage); werr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", werr)
		return chatExitFailed
	}
	return chatExitOK
}
```

So a released stdout produces exactly the recorded
`runChat([--tools -h]) = 1, want 0` — the failure and the report are one event,
confirmed in the product source rather than inferred from the fixture.

**3. No single actor can be named from the evidence a green run leaves.** The
reproduction (`make test-budget PKGS='./cmd/agent'`, ~719–740 s) is not
deterministic: `run-1791171049-4098189.json` finished `outcome=pass` (3052
subtest passes, 0 failures) with `load_end=10.73 7.77 4.85` on 4 cores, and the
failures that DID occur in that same run are this fixture's two self-driving
guards, which produce the foreign release on purpose with
`syscall.Close(fd)`. The only other by-number closers in the tree are the
fixtures that release a claim they took with `syscall.Dup`/`F_DUPFD`
(`daemon_death_pipe_shape_test.go`, `borrowed_fd_sites_test.go`,
`internal/daemonlifetime/*_test.go`) — including `bfdRawFixture`, which frees
its dup while its comment says the caller owns it. That is a class of candidate
actors which expands as the suite changes, and a name picked from a green run
would be a guess. What the fixture can do instead is what this change makes it
do: report the pipe's identity, sweep for a later claim on that same pipe, and
leave the task of naming the actor to the run that reproduces it.

## The census, and its three answers

`chatRunArgsReuseCensus(identity, ownFds...)` asks `/proc/self/fd` whether any
descriptor this fixture did NOT open now names the same pipe object, ignoring
the fixture's own four ends (all four, both pipes: two ends of one pipe carry
the same `pipe:[inode]`, so without that filter the sibling end would be
reported on every occurrence and the census would name nothing foreign).

It answers with three DIFFERENT sentences, and the third is the one the pre-fix
report could not have produced:

- a holder exists → `The pipe <ident> is ALSO held by fd <n> …` — the closer's
  own witness, a descriptor a later reader can inspect;
- no holder → `No other descriptor in this process holds <ident>`;
- **no identity to search for** → `No search for another holder of the pipe
  could be run: the kernel named no identity for it (<what was recorded>)`. An
  unrun search is never rendered as an empty result.

The two identities that are not measurements are kept apart for the same
reason: `chatRunArgsIdentityUnknown` (procfs said nothing is open there — a
fact about the descriptor) versus `chatRunArgsIdentityUnreadable` (procfs
refused for another reason — a limit of the observer). Rule 15: an unmeasured
state is named, never implied.

## What this change does NOT do

- It does not identify the actor. No occurrence was observed with its closer;
  the argument above is an elimination (not a finalizer) plus a
  non-determinism observation, and both are stated as such.
- It does not weaken the classifier: `os.ErrClosed` (this fixture's own earlier
  release) and `EBADF` (a foreign release) stay distinguishable, and the unknown
  errno arm stays an error.
- The write-end arms of `AlreadyReleased`/`Clean` are still driven by the
  guards and the classifier test, not by a measured end-to-end occurrence.

## Verification

- `go build ./...` → rc=0. `go vet ./cmd/agent/` → rc=0.
- `go test ./cmd/agent/ -run 'TestChatRunArgs|TestChatCLIVocabulary' -count=1 -v`
  → 5/5 PASS, `ok github.com/nick/gopi/cmd/agent` (rc=0), with the two identity
  lines above in the log.
- `TestChatRunArgsIdentityCensusNamesTheLostPipe` RED on the pre-fix census
  shape (an unnamed identity answered with the no-other-holder sentence):
  6 assertion lines, `RED_RC=1`, recorded in the task session's
  `evidence/identity-guard-red-final.txt`; green after restore
  (`evidence/postfix-standalone-final.txt`).
- `make test-budget PKGS='./cmd/agent'` — the declared reproduction and the
  acceptance gate. Green: `run-1791172440-29124.json`, `stamp: …
  elapsed=1010s load_end=14.47 12.91 11.99 cores=4 exit=0 outcome=pass`,
  `BUDGET_RC=0` (`evidence/budget-postfix.log`), with 3238 pass / 0 fail actions
  and the case under repair passing. A follow-up run on the same tree
  (`run-1791172835-100811.json`) failed on a DIFFERENT case,
  `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` ("the REPL did
  not finish within 20s after the injected ctrl-c") — untouched by this change
  and disclosed in the task report rather than repaired here; `…/--tools` passed
  in that run too.
