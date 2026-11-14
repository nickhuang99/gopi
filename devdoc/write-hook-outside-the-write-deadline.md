<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The write hook runs outside the write's deadline

`internal/tools/timeout.go:ClassifyHookTimeout`, `internal/tools/write.go:runClassifyWrittenFile`,
`internal/tools/write_classify_seam_test.go`. Baseline `7222527`, task
`01a131e2-8a1d-7765-a0d8-65cc69e98140`.

## The defect

`write` and `edit` each wrapped TWO operations in ONE deadline
(`FileToolTimeout`, default 30s, `GOPI_TOOL_TIMEOUT_SEC`): the byte-landing leg
(`atomicWriteFile`, milliseconds of local I/O) and the post-write document-classification
hook (`docindex.ClassifyWrittenFile`, which shells out to the repo's own generators
`python3 tools/docclass/classify.py --only <path>` and `banner.py`).

The two legs have nothing in common. The hook is a best-effort SIDE EFFECT whose cost
is machine state, not this process's business: it measured **10.3s on an idle host**
here and **35.07s** on a loaded one. Inside a 30s deadline it therefore fired on a write
that had ALREADY SUCCEEDED, and the caller was handed

```
write: <path>: timed out after 30s
```

for a write whose bytes and whose `DOC-INDEX.tsv` row were both on disk —
`internal/tools/write.go` said so itself, one line above the call: *"a classifier that
cannot run must not fail a write the caller asked to happen."* The structure and the
contract disagreed; this change removes the structure.

## What the deadline could not observe (the second half)

A file-tool deadline wraps a staged write: the content goes to a same-directory temp
file, is fsynced, and is renamed over the target. `withTimeout` has no cancellation over
`os.File` I/O — an op that ignores the context is "left to finish harmlessly". Measured
(`~/.gopi/sessions/01a131e2-8fd5-…/evidence/red-after.txt`, baseline binary):

```
at process return:  doc_bytes=17139  docindex_rows=0
20s later:          doc_bytes=17139  docindex_rows=1
```

Every byte was on disk when the error was returned, and the hook wrote its row **20
seconds after** that error left the building. So a bare `timed out after 30s` invites a
retry of a write that already landed — hence `writeTimeoutNote`, appended to BOTH tools'
deadline errors: *"the content may already have landed on disk (the operation is left to
finish); read the file before retrying."*

## The shape of the fix

- `writeWithTimeout` / `editWithTimeout` wrap the byte-landing leg ONLY; the hook is
  called by `runClassifyWrittenFile` after `withTimeoutErr` has returned for the last
  time. Both doors are symmetric — `editImpl` had the hook inline too.
- The hook is reached through a package-level seam,
  `var classifyWrittenFile = docindex.ClassifyWrittenFile`, so a test substitutes a
  classifier with a controllable duration and proves the seam WITHOUT a Python
  interpreter and without manufacturing machine load (Rule 27).
- The hook's own upper bound is `ClassifyHookTimeout = 60 * time.Second`, an
  **independent named constant with no env knob**. It is a HANG STOP, not a performance
  budget: sized above the worst measured cost so an ordinary slow host is never
  reported, and when it fires the hook is abandoned and REPORTED while the write stays a
  success. `classifyHookTimeout` is the effective variable — a test substitutes it;
  production never assigns it, so the effective value IS the constant.
- Rule 15: a hook that fails, and a hook that outlives its bound, are two different
  facts and are reported as two different sentences on stderr via `taskNotifyLog`
  (`ErrClassifyHookTimeout` is the sentinel that tells them apart). Neither is ever
  returned to the caller.
- No new environment variable (Rule 2, Rule 35): the knob would be a second, unmeasured
  answer to a question the code can bound for itself, and the existing
  `GOPI_TOOL_TIMEOUT_SEC` reader stays the single reader of the file-tool knob.

The ORDERING is load-bearing and unchanged: the generator READS the document it
classifies (its content decides the class), so a document that is not on disk yet cannot
be classified by it.

## How it is verified

Six tests in `internal/tools/write_classify_seam_test.go`: a hook that sleeps 7× the
write's own deadline (write and edit both return nil, the file is complete, and the
elapsed time proves the hook was REACHED rather than skipped); the hook receives the
written path; a failing hook is reported with the generator's own reason; a hook
outliving its bound is reported by name and bound; the timeout message carries the
landed-bytes recipe.

**RED PROOF**: mutating `writeImpl` back to the old shape (hook inside the deadline
closure) makes the guard fail with
`write: …/slow-hook.md: timed out after 100ms — the content may already have landed …`;
the restore was verified byte-identically against an explicit file
(`sha256 3cedf8c6…` for both) and the guard re-ran green.

**Real-machine evidence** (re-run end to end on the delivered tree, one script,
`/tmp/red-green-run.sh`, output `/tmp/evidence-01a131e2/`) — `GOPI_TOOL_TIMEOUT_SEC=1`
with a real `./bin/gopi` `write` of a 14797-byte `devdoc`:

```
RED   (baseline binary, 7222527):  env GOPI_TOOL_TIMEOUT_SEC=1 /tmp/gopi-baseline-bin < /tmp/repro-prompt.txt
      stdout: ERR write: devdoc/zz-timeout-repro.md: timed out after 1s
      file:   14797 bytes, sha256 36c5865f…   docindex_rows_at_return=0
      20s later: docindex_rows_20s_later=1     ← the "timed out" write kept writing
GREEN (this branch's ./bin/gopi):  env GOPI_TOOL_TIMEOUT_SEC=1 ./bin/gopi < /tmp/repro-prompt.txt
      stdout: OK
      file:   14828 bytes, sha256 0f7d9083…   docindex_rows=1
```

14828 = the requested 14797 + the generator's 31-byte class banner, and the
`DOC-INDEX.tsv` row proves the hook still ran on the same command that returned OK.

### A config-doc anchor this change had to re-point

`CONFIG.md` documents `GOPI_TOOL_TIMEOUT_SEC` / `GOPI_BASH_TIMEOUT_SEC` /
`GOPI_BASH_IDLE_SEC` with a `internal/tools/timeout.go:NNN` anchor each. The three
readers sat at lines 53/58/63 before this change; inserting the new constant moved them
to 83/88/93 — and `TestConfigDocRefsLandOnContent` caught it (`internal/tools/timeout.go:63`
now lands on a BLANK line). The three anchors are re-pointed at the new reader lines in
the same commit, which is exactly what the gate exists for: the doc had been *resolving*
the whole time, so only a content check could see it had stopped meaning anything.

`go test ./internal/tools/ -run 'TestWriteSucceedsWhenTheClassifyHookOutlivesTheWriteDeadline|TestEditSucceedsWhenTheClassifyHookOutlivesTheEditDeadline|TestClassifyHookIsCalledWithTheExpandedPath|TestClassifyHookFailureIsRecordedNotReturned|TestClassifyHookExceedingItsOwnBoundIsRecordedNotReturned|TestWriteTimeoutMessageSaysTheBytesMayHaveLanded' -count=1` → 6/6 PASS.
