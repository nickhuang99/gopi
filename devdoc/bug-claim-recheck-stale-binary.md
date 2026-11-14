<!-- doc-class: historical -->
# Re-measurement of the two claims in `bug-worker-stale-binary.md`

**Run**: task `01a1362e-290b-78e0-a707-a9c0aa477647`, host hosta, 2026-10-16.
**Subject**: `/home/user/.gopi/sessions/01a1355d-d0f4-71b1-8d97-5305dd229431/artifacts/bug-worker-stale-binary.md`
(Rule 41 bug evidence, filed 2026-10-14 00:52).
**Why here**: the epic that this run was dispatched for includes "把 bug 报告里两条
经不起复现的论断按实测澄清". The two claims below were judged at reframe time to be
the ones that do not survive reproduction. They are clarified by MEASUREMENT here,
not by argument — and where a claim turns out to hold, that is recorded too.

The bug report's third and central claim — that a run's `bash` surfaces are
executed by a different checkout's binary — **survives and is the reason this
run exists**; it is measured in
`devdoc/session-records-its-own-binary.md`. This file handles only the two that
did not.

---

## Exhibit 1 — CLAIM (a): "`git grep -c 'this guard never deletes it for you' HEAD` = 0（该文案不在源码里）"

**Verdict: does not survive. The claim is false AS STATED, and it is false for a
mechanical reason worth recording, because that reason is what makes the
report's conclusion look right while the observation is wrong.**

### What was re-measured

The original command cannot be re-run verbatim (the run's ground
`/home/user/workspace/gopi-01a1355dd0f471b18d975305dd229431` no longer exists,
and its branch is the only survivor), so the SAME command was run against the
SAME ref — the branch the report itself names, at the tip that is still in the
repo:

```console
$ git log -1 --format='%H %ad %s' --date=iso origin/task/01a1355dd0f471b18d975305dd229431
1b8bd26c8e5dc455b5c29747e478b2ecb00342c9 2026-10-14 00:51:40 +0800 fix(path-policy): a repository-naming option removes the cd carry's authority

$ git grep -c "this guard never deletes it for you" origin/task/01a1355dd0f471b18d975305dd229431
origin/task/01a1355dd0f471b18d975305dd229431:devdoc/worktree-path-must-not-exist.md:1
origin/task/01a1355dd0f471b18d975305dd229431:internal/tools/worktree_fresh_path.go:1
exit=0

$ git merge-base --is-ancestor e64cc0cf9 origin/task/01a1355dd0f471b18d975305dd229431
YES (exit 0)          # the guard's own commit is an ancestor of that branch's tip
```

The sentence is in `internal/tools/worktree_fresh_path.go` at that ref, and the
commit that introduced it (`e64cc0cf9`, 2026-09-23, "fix(worktree): a worktree
path must NOT exist before we create it") is an ancestor of that branch's tip.
It is on master too, unchanged:

```console
$ git show HEAD:internal/tools/worktree_fresh_path.go | sed -n '68,71p'
const worktreeFreshPathRemedy = "choose another path, or inspect the existing entry " +
	"(it may be another run's live work) and remove it by hand before retrying — this guard never deletes it for you"
```

### Why the report read 0 — the mechanism (this is the part worth keeping)

`git grep <pattern> HEAD` searches **the ref `HEAD` of whatever repository git
resolves from the current directory**. Inside a **linked worktree**, `HEAD` is
that worktree's branch (`task/01a1355dd0f4…`) — so the command's answer depends
on where it was typed, and the report typed it in the run's ground. On the
report's own ref the answer is `1`, as measured above.

The report's conclusion — *"the running binary is not the one built from this
branch"* — happens to be **true for an independent reason** (measured in the
devdoc: the worker's exe is `/home/user/work/GoPi/bin/gopi`, from another
checkout entirely). So a false observation supported a true conclusion, and
nothing in the report would ever have surfaced that. **This is the concrete cost
of using a ref-relative probe as evidence**: it cannot distinguish "the string is
not in this code" from "I asked a different code".

**What actually remains of claim (a)**: the report's *inference* — "this text is
not in the branch's source" — is refuted; the guard text is exactly where the
branch says it is. The underlying phenomenon the report was reaching for (a
binary whose code is not the branch's code) is real, is measured elsewhere, and
is what this epic makes **pinnable after the fact** instead of guessed.

---

## Exhibit 2 — CLAIM (b): "同一句里的 `$S` 被拒（目标目录存在），而同地址的字面写法走正常路径；`rm -rf probe-dir2` 此前成功，但只在 `echo "R2=$?"` 里带 `$?` 的命令也被同一句拒绝"

**Verdict: reproduces — but it is NOT a defect and NOT related to claim (a). It
is a documented, deliberate fail-closed trade-off, and the report presents it
next to a binary-staleness claim where it does not belong.**

### What the code does (read, and it says so itself)

`internal/tools/bash.go:896-907` — the fail-closed rule is evaluated **only
after a dangerous pattern has already matched**:

```go
	// cwd whitelist: every path-like token must stay inside the working
	// directory.
	for _, tok := range strings.Fields(command) {
		// Fail-closed on shell expansion (dsh hardening): a token carrying
		// $VAR, $(...) or backticks names a target that cannot be statically
		// verified …
		// Trade-off: a harmless "$PWD" echo beside a dangerous command is
		// rejected too — a rejection costs a retry, a fail-open costs data.
		if unverifiableToken(tok) {
			return fmt.Sprintf("token %q contains shell variable/command substitution — cannot verify its target", tok)
		}
```

`unverifiableToken` = "the token contains `$` or a backtick after quotes are
stripped" (`bash.go:911-913`). So:

```console
$ git grep -n "cannot verify its target" HEAD
HEAD:internal/tools/bash.go:902:			return fmt.Sprintf("token %q contains shell variable/command substitution — cannot verify its target", tok)
```

- A command that matches a dangerous pattern AND carries a `$`-bearing token is
  refused — including a `$?` that names nothing on the filesystem. **That is the
  intended behaviour, stated in the comment directly above it.**
- A command with no dangerous pattern is not checked at all, which is why
  `rm -rf probe-dir2` succeeded and the same command with `echo "R2=$?"` was
  refused. The report records both facts correctly; it just reads the pair as a
  contradiction instead of as the rule.

### What is wrong with the report's framing of (b)

It is listed inside a "现象（实测）" list under a heading about **a stale worker
binary**, which invites the reader to read it as a second symptom of the same
cause. It is not: it is a sandbox-policy decision implemented in `bash.go`,
compiled into whichever binary runs, reached only for commands that already
matched a dangerous pattern, and **its own code comment names it a trade-off
with a stated preference** ("a rejection costs a retry, a fail-open costs data").
The one genuinely open question in it — *should the check be scoped to
path-like tokens rather than every `$`-bearing token?* — is a policy question
about `bash.go`, not about binary staleness, and belongs to its own row if
anybody wants it changed.

**What remains of claim (b)**: the observed behaviour is real and reproducible.
The implication that it is a defect, or that it is evidence for the staleness
claim, does not survive.

---

## Summary table

| claim | survives? | why |
|---|---|---|
| a run's `bash` runs a different checkout's binary | **YES** | measured: worker exe `/home/user/work/GoPi/bin/gopi`, ground's `bin/gopi` absent; the reason this run exists |
| (a) "the guard text is not in the branch's source" | **NO** | `git grep -c … origin/task/01a1355dd0f4…` = 1 in `internal/tools/worktree_fresh_path.go`; the original probe was ref-relative (`HEAD` inside a linked worktree = that worktree's branch) |
| (b) `$`-bearing token refused beside a dangerous pattern | **YES, as behaviour** | `bash.go:896-907`, a stated fail-closed trade-off |
| (b) read as a defect / as staleness evidence | **NO** | policy of `bash.go`, independent of which binary runs it |

## Reproduction (verbatim)

```bash
cd <any checkout of this repo>
git grep -c "this guard never deletes it for you" origin/task/01a1355dd0f471b18d975305dd229431
git merge-base --is-ancestor e64cc0cf9 origin/task/01a1355dd0f471b18d975305dd229431; echo $?
git grep -n "cannot verify its target" HEAD
```

## What was NOT verified

- The literal command the report ran (`… HEAD` in that specific worktree) cannot
  be re-run: that worktree was cleaned up and does not exist
  (`ls: cannot access '/home/user/workspace/gopi-01a1355dd0f471b18d975305dd229431'`).
  The re-measurement above therefore substitutes the branch tip, which is the
  strongest available instrument and is stated rather than implied.
- Whether the report's `git grep` printed `0` because of ref-relative resolution
  or for some other reason is an INFERENCE from the mechanism of `git grep` in a
  linked worktree; the measured fact is that the string IS in that branch. Do
  not upgrade the inference to a measurement.
