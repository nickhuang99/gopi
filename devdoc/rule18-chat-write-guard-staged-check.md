<!-- doc-class: normative -->
# Rule 18 fix: the chat write guard's staged-index check, de-indented to guard clauses

> **SCORE: superseded by master's own fix — read this paragraph first.** While this branch
> was being written, master landed the **same repair of the same site**, byte-for-byte
> identically: `9c7bb9f8 fix(standards): drop the else-after-return in the chat write guard
> (Rule 18)` (2026-10-05 15:51), six minutes after this branch's base `e02538fa`
> (15:45). The two are independent: neither run saw the other's work (Rule 26 — the
> duplicate-dispatch allowance is for repair, and this is what a duplicate looks like).
> `git diff master HEAD -- internal/tools/chat_write_guard.go` is **empty** — after the
> delivery-point sync the two files are identical, so this branch ships **no source
> change**; what it adds to master is this record (master's own doc is
> `devdoc/standards-gate-red-rule18-chat-write-guard.md`) plus the Rule 31 runtime A/B
> below, which master's doc does not carry. Both branches' `baseline.go` and
> `internal/standards/gate_test.go` are untouched.

## The hit

`go test ./internal/standards/ -run 'TestStandardGates/TestGate3ElseAfterReturnRedProof'`
exits **1** at this branch's base, master tip `e02538fa`, on a **pre-existing** site:

```
gate_test.go:101: Rule 18 (no else after terminating if): 3 total hits, 0 baselined, 2 escaped,
                                                  1 new (unannotated), 0 baseline entries
gate_test.go:113: Rule 18 (no else after terminating if): NEW violation (checklist 4.3)
                  at internal/tools/chat_write_guard.go:839
```

The construct, as it stood:

```go
	if staged, ok := chatStagedPaths(root); !ok {
		return chatGitInvocationVerdict{shape: chatGitIndexUnreadableSubject}
	} else if bad, found := chatFirstDisallowedStagedPath(staged); found {
		return chatGitInvocationVerdict{path: bad}
	}
```

`TestStandardGateDataReport` confirmed the shape of the red before any edit — Rule 18:
`hits=3 baselined=0 escaped=2 NEW=1`, with every other rule in the report at `NEW=0`.

## Provenance

`git log -S 'chatFirstDisallowedStagedPath(staged); found' --oneline -- internal/tools/chat_write_guard.go`
→ **`a8bf8ce2` — "feat(chat): allow a chat to write and commit devdoc/ (the office document area)"**
(2026-10-05). The gate therefore was red on master **before** this run's branch existed;
this branch carries only the fix. No other run's branch was touched (Rule 23).

There was **no baseline permit** for this file at all
(`internal/standards/baseline.go` names `chat_write_guard.go` nowhere), so the site was
new-on-arrival and the ratchet had room only downward.

## Why the else was there — and why it is not needed

The `else if` did not express a decision that needs a branch: `chatStagedPaths`' failure
arm `return`s, so the `else` existed purely to keep `staged` in scope for the second
predicate. That is Go **statement scoping**, not control flow — the same diagnosis the
earlier Rule 18 fixes in this repo reached (`devdoc/rule18-voice-device-else.md`), and
the same one master's `9c7bb9f8` reached independently.

## The fix (guard clauses, no `else-ok:`)

`staged` is hoisted out of the `if` statement (the call still runs exactly once, after the
pathspec loop, exactly as before), the failure arm becomes a flat guard, and the second
predicate follows it at the same indentation level:

```go
	staged, ok := chatStagedPaths(root)
	if !ok {
		return chatGitInvocationVerdict{shape: chatGitIndexUnreadableSubject}
	}
	if bad, found := chatFirstDisallowedStagedPath(staged); found {
		return chatGitInvocationVerdict{path: bad}
	}
	return chatGitInvocationVerdict{allowed: true}
```

The sibling predicate in the same file already had this shape
(`chatGitCommitInvocationVerdict`), so the fix makes the file consistent with itself rather
than introducing a second style.

**Unchanged, deliberately:** the order of the two checks, which refusal each one produces
(`chatGitIndexUnreadableSubject`, then `{path: bad}`), the message strings, the comments
and the function's behaviour. The diff is 4 insertions / 2 deletions, all inside
`chatGitAddInvocationVerdict`.

## Evidence that the shape — not the gate — was fixed

- Before: `TestGate3ElseAfterReturnRedProof` FAIL, `NEW=1` (above).
- After: `go test ./internal/standards/ -count=1` → `ok` (the whole gate package).
- `internal/standards/baseline.go` and `internal/standards/gate_test.go`: **untouched**
  (`git status --porcelain` names `internal/tools/chat_write_guard.go` only, plus this file).
- The real surface was executed (Rule 31), with an A/B proof against the PRE-fix binary built
  from `git show HEAD:internal/tools/chat_write_guard.go` (unchanged by this diff — the fix
  preserves behaviour by construction). Same fixture, same prompt, both builds:
  pre-fix and post-fix produce the **byte-identical** refusal
  `Refused by the CHAT WRITE BOUNDARY (git add would touch \`internal/tools/outside.go\`)…`
  when an outside path is staged, and both return `exit=0` (allowed) when the index holds
  only `devdoc/x.md`. The invocation, the fixture and the diffs are in the task report.
- **The live run's refusal came out of the branch THIS DIFF CHANGED** (the staged-index
  check itself, not another arm of the guard): the fixture's index held
  `internal/x.go`, the model's `git add devdoc/x.md` named an allowed path, and the
  refusal named the *staged* path — which is exactly the second guard clause this diff
  de-indented. Transcripts: `artifacts/rule31-stdout.txt`, `artifacts/rule31-stderr.txt`
  in this run's session directory; the scratch fixture is
  `artifacts/rule31-scratch/`.
