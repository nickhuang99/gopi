<!-- doc-class: historical -->
# Task Report: the chat write boundary — PA/chat may write AND commit `devdoc/`

**Task:** `01a10a2c-e404-74d1-b226-37bce40e39b6`
**Branch:** `task/01a10a2ce40474d1b22637bce40e39b6`
**Worktree:** `/home/user/workspace/gopi-01a10a2ce40474d1b22637bce40e39b6`
**Supersedes** the blanket rule of `devdoc/chat-write-boundary.md` (task
`01a0e45e`), which put `devdoc/` on the forbidden side. Everything else that doc
established — the two halves, the dispatch-point gate keyed on `ChatMode`, the
tracked-path CRITERION, the session directory for what is not committed, the task
route for code — is unchanged and is load-bearing here.

---

## The ruling this implements

The operator, 2026-10-05:

> **"我让你写 devdoc 就是要让你自己 git commit，否则要你写干什么？就是一行命令的事情，
> 而且是新文件也不会有 conflict，怕什么？你不 commit 那么 merge 就不能工作，你明白吗？"**

> **"PA 写文档是你的基本工作，git checkin 并不是大的工作，写一个任务改文档完全无必要，
> 你知道运行一个任务的代价有多么大啊？要 sandbox/worktree/branch，所以，要改，PA 是办公
> 文档工作，这个必须要允许。"**

Three reasons, and the second is a hard failure rather than a preference:

1. **The cost is asymmetric** — a task chain (sandbox → worktree → branch → worker
   → merge) costs tens of times a one-line `git add && git commit` of a markdown
   file.
2. **Not committing is the failure mode** — a document left in the working tree
   makes the main checkout dirty, and `gopi merge` then reports *"tracked files
   modified — left alone for a human"* and stops entirely. So "allow the write,
   forbid the commit" is not a weaker version of the allowance; it is worse than
   no allowance, because it produces the exact state that halts the merge queue.
3. **The risk is negligible** — a new file cannot conflict, and a commit IS a
   rollback point.

## Completed

### 1. The allowance is a real, narrow mechanism — not a rule in prose

`internal/tools/chat_write_guard.go` now answers about **verb + target path**,
where it previously answered about the verb alone:

- **`ChatWriteAllowedPrefixes`** — the one writable tree, taken from
  `docclass.Root` (the corpus reader's own definition, Rule 13/14). It is
  consulted FIRST in `ChatWritePathRefusal`, so the allowance costs no git probe.
- **`ChatBashWriteRefusal(command)` / `ChatBashWriteRefusalIn(command, workdir)`**
  now read each invocation as *(verb, arguments, repository)* and give three
  verdicts, not two:
  1. a verb outside the partly-allowed pair → refused outright (the banned set is
     unchanged: push, stash, worktree, branch, checkout, switch, reset, restore,
     merge, rebase, tag, cherry-pick);
  2. `add` / `commit` → refused **unless** its invocation is one of two narrow
     shapes, and each shape answers about the PATHS it would stage or record;
  3. anything the reader cannot classify → refused (Rule 15).
- **The `add` shape** (`chatGitAddInvocationVerdict`): every pathspec must resolve
  inside the allowed tree, no option may appear (`-A`, `-u`, `--all`, `-f`,
  `-N`, `--`, …), no directory may be named, and the real INDEX is then re-read —
  what git actually staged must also be inside the tree.
- **The `commit` shape** (`chatGitCommitInvocationVerdict`): a message option with
  a non-empty message, no pathspec, no option other than `-m` (`-a`, `--amend`,
  `-F`, `-C`, `--no-verify`, … are all refused by name), and — the half that makes
  the allowance safe at all — **the index is read**, so a commit cannot record a
  path that some other command or the operator staged outside the tree.

### 2. The reader was rebuilt to be able to judge PATHS at all

The previous reader split the command on literal separators and matched the word
`git`, so its correctness rested on word boundaries a shell does not respect. It
is now a tokenizer (`chatShellTokens`) that tracks quoting **by state**, which is
what makes a message readable:

- `git commit -m "docs: a chat's document"` is ONE invocation with ONE argument.
  Before, the message's own spaces were word separators, so the commit judgement
  saw `a`, `chat's`, `document` as pathspecs and refused the exact command the
  allowance exists to permit.
- An apostrophe inside double quotes is literal text, not an unclosed single
  quote. The earlier `strings.Count(quote)%2` test called that commit unreadable.
- The stripping the reader needs for the prefilter, and the tokenizing it needs
  for the judgement, are now two separate steps; handing the tokenizer the
  stripped text was the bug that made the message unreadable.

The reader also gained, each with a test:

- **Clause-initial recognition** — a word is not a string, so `grep -rn "git
  push" docs` reads as a grep while `git status; git commit -m x` reads as two
  invocations.
- **Wrapper clauses** (`bash -c '…'`, `eval`, `ssh`, `xargs`, `find`) and a
  reserved verb used as a command (`stash`, `worktree`, `branch`, `restore`,
  `switch`, `rebase`, `cherry-pick`) are ASSERTIONS — refused by name, with the
  verb named when the payload holds one.
- **Shell expansions** (`git add $FILES`, `git -C $REPO add …`) are refusals: a
  resolver asked about `""` must never turn an unread pathspec into an allowed
  one.
- **Environment prefixes** (`GIT_DIR=/r git …`) are skipped, so the word after
  them is read as the head.
- **The patch preamble** (`From <hash> Mon Sep 17 <date>`) ends the reading:
  `git am`/`apply` take content, not arguments.
- **A git word that never reaches a subcommand** (`git --version`) is NOT a
  refusal — it mutates nothing, and refusing it made an unrelated read-only clause
  poison the whole line.

### 3. Both halves were changed in the same commit

`internal/agent/chat_write_boundary.go` — the paragraph the model reads — now
teaches five rules in the order it needs them: no git mutation beyond the
allowance; **the allowance itself, with the exact legal shape**; no artifact into
any other tracked path; the session directory for anything else; a code change is
a task. Both limbs were rewritten (the `--tools` limb now says the document is
writable AND committable; the no-tools limb still says it cannot write and must
not claim it did).

**The prompt cannot drift from the mechanism**: it renders
`tools.ChatOutrightBannedGitSubcommands()` (the banned list MINUS the
partly-allowed pair), `tools.ChatPartlyAllowedGitVerbs`,
`tools.ChatGitMessageFlag`, and the allowed tree — each from its one definition,
never typed (Rule 1/13/14).

### 4. The hook passes the working directory

`chatToolWriteGuard` (internal/agent) now passes the bash tool's `workdir`
(argument name in a named constant) into `ChatBashWriteRefusalIn`. Without it the
pathspec judgement would resolve against whatever directory the gopi process
happens to stand in rather than the repository the command will run in.

### 5. A gate the reader tripped, and the honest fix

`TestPeerAliasGuardDocumentedAtEverySSHLeg` failed with *"chat_write_guard.go
spawns ssh but names no validating site"* — the reader's wrapper table spelled the
word `ssh` to RECOGNISE an invocation, and the gate reads any bare spelling in
this package as a spawn. The word now lives in `peer_alias.go` (the file the gate
designates as the one home for ssh's own spelling, and the file it excludes from
the sweep) as `sshCommandWord`, referenced from both places: the gate passes, the
table is unchanged in behaviour, and there is one definition of the word (Rule
13/14). The reader spawns nothing — it compares a word — and the constant's own
comment says so.

## Completed (continued)

### 6. The gates and the cleanliness of the delivery

- **The gate's own words about the allowance**: the prompt half now asserts that
  the partly-allowed verbs are NOT taught as banned, and that the allowed tree and
  the commit flag are named — the failure mode being a prompt that forbids the
  staging the mechanism permits.
- **A whole-directory `gofmt -w` reflowed nine files this diff has no business
  touching.** They were identified by `git diff -w --numstat` (no non-whitespace
  delta in seven of them) and **reverted** (`git restore <those files>`), so the
  branch carries only the files the change actually modifies.

## Files

| file | what changed |
|---|---|
| `internal/tools/chat_write_guard.go` | the mechanism: the allowance list, the pathspec-aware verdicts, the rewritten reader/tokenizer, the refusal texts |
| `internal/tools/chat_write_guard_test.go` | the tables: 29 refusals, 15 reads allowed, the allowance (per-file / mixed / traversal / directory / expansion / option), the staged-index pair, the fail-closed table, the prefix-vs-substring predicate |
| `internal/tools/peer_alias.go` | `sshCommandWord` — ssh's own spelling, moved to the file the peer-alias gate designates as its home |
| `internal/agent/chat_write_boundary.go` | the prompt: five rules, both limbs, every rendered value taken from its one definition |
| `internal/agent/chat_write_boundary_test.go` | the prompt assertions + the two dispatch-point acceptance tests |
| `internal/agent/chat_write_guard.go` | the hook passes the bash tool's `workdir` into the guard |
| `devdoc/chat-devdoc-write-allowance.md` | this report |
| `devdoc/DOC-INDEX.tsv` | the new doc's row (Rule 36) |

## Not Done (honest)

- **A live end-to-end acceptance has not landed.** A `gopi chat --tools` session
  was started against a throwaway fixture repo with a brief that asks for exactly
  the allowed sequence, and it was still running when this report was written; the
  fixture showed no document in `devdoc/` at the last observation. The mechanism is
  proven at the **dispatch point** (`TestChatTurnMayWriteAndCommitADocumentInTheCorpus`
  drives `Agent.executeToolDefault` with the real tool-call arguments, and observes
  HEAD move and `git show --stat` carry the document; the paired test observes
  `git add -A` refused at the same point), and that is the seam the model's tool
  calls pass through. But a dispatch-point test is not a model that chose the
  shape, and **Rule 31's requirement — the surface INVOKED with real arguments —
  is met only up to the seam, not through the model.** The live run is recorded in
  `## Evidence` below with the invocation verbatim, so the reader can judge which
  leg it establishes.
- **`git commit --allow-empty` is permitted** (it stages nothing and the index
  check passes on a clean index). It is not in the ruling's scope either way;
  nothing was written specifically to allow it, and nothing forbids it.
- **`version` / rules-file changes (`GOPI.md`, `CONFIG.md`)** stay forbidden per
  the ruling's table; they are covered by the tracked-path criterion, and any
  change to them is to be committed by the task that owns them.
- **The `go test ./internal/standards/` gate run** was launched as a background job
  and had not reported when this report was written; the two suites the diff
  touches (`./internal/tools`, `./internal/agent`, and the branch-scoped
  `make test-budget` run that covers both) are recorded below.

## Verification

- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-chatdoc ./cmd/agent/` → OK;
  `go build ./...` → OK; `go vet ./internal/tools/ ./internal/agent/` → clean.
- Tests: `go test ./internal/tools/ -run 'TestChatWrite|TestChatBash|TestChatPartly|TestPeerAliasGuard'`
  → `ok github.com/nick/gopi/internal/tools`;
  `go test ./internal/agent/ -run 'TestChat'` → `ok github.com/nick/gopi/internal/agent`.
  The tables: 29 refused command shapes, 15 allowed read-only shapes, the
  allowance per-file / mixed-path / traversal / directory / expansion / option
  refusals, the staged-index refusal AND its positive (same command permitted
  once the index is clean), the unreadable-shape fail-closed table, and the
  prefix-vs-substring predicate (asserted directly, because a mis-match here
  permits a WRITE rather than firing a refusal).
  `make test-budget PKGS='./internal/tools ./internal/agent'` → **exit 1**, three
  failures, NONE in the changed code: `TestDocAnchorsResolveInNormativeDocs`
  (15 NEW drift entries, every one an anchor in `internal/tools/tasks.go`,
  `internal/lineedit/editor.go`, `cmd/agent/cli_*.go`, `cmd/agent/commands.go` —
  files this diff does not touch, and **zero** naming the changed files or the new
  devdoc), `TestPeerAliasGuardDocumentedAtEverySSHLeg` (fixed here — see below),
  and `TestRunCloseoutSelectionReportsAHoleAndNotAMiss` (does not fail when run on
  its own: `go test ./internal/tools/ -run TestRunCloseoutSelectionReportsAHoleAndNotAMiss`
  → `ok`, i.e. an ordering/serialisation interaction, not an assertion in the
  changed code).
- Ran: the live surface, verbatim:
  `/tmp/gopi-chatdoc chat --tools < /tmp/chatdoc-brief.md` with the process
  standing in a throwaway repository (`git init` + `devdoc/seed.md` +
  `internal/s.go` committed), the brief asking for
  `write devdoc/chat-allowance-live.md` → `git add <that file>` →
  `git commit -m "docs: chat allowance live check"`, and forbidding source edits,
  push and branches. A first attempt ran in the foreground and was killed by the
  idle-output guard (the session prints nothing while it talks to the model); it
  was restarted as `job-1`.
- Result: the fixture was observed twice while the session ran — `git status
  --porcelain` **empty** and `devdoc/` holding only `seed.md` — and at t+624s
  `job-1` was still `running`, so it was killed. **The live leg is therefore NOT
  established**: a different `gopi chat` session (`98be332a-16a5-4a7e-95ac-ff7835878484`,
  started at the same time and standing in THIS worktree) was observed emitting
  its tool calls normally, including a `bash` call that ran `git worktree` and got
  `Refused by the CHAT WRITE BOUNDARY (bash ran 'git worktree')` — but that is
  the **pre-change** binary's refusal of a verb the change does not touch, i.e.
  evidence that the guard is live on the chat path, NOT evidence that the
  allowance works through the model. The allowance is established at the SEAM
  (`TestChatTurnMayWriteAndCommitADocumentInTheCorpus`: the document is created,
  `git add <file>` and `git commit -m` both pass the guard, HEAD moves,
  `git show --stat` carries the file, porcelain ends empty; and the same dispatch
  point refuses `git add -A`) — which is the seam every model tool call passes
  through, but is not a model that chose the shape.
- Commit: `a8bf8ce2` on `task/01a10a2ce40474d1b22637bce40e39b6`
  (`feat(chat): allow a chat to write and commit devdoc/ (the office document
  area)`) — the change itself, code + tests + this report. Two doc-only commits
  follow it (`6e889d1f`, and the one that lands this line) whose only content is
  this hash; **the code is in `a8bf8ce2`**.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output (run after each edit round; the last one
  after the tests were green).
- `go test ./internal/tools/ -run 'TestChatWrite|TestChatBash|TestChatPartly'` →
  `ok github.com/nick/gopi/internal/tools 0.612s`.
- `go test ./internal/agent/ -run 'TestChat'` →
  `ok github.com/nick/gopi/internal/agent 1.682s`.
- The commit-message bug, observed rather than reasoned: the earlier run of the
  allowance test printed
  `Refused by the CHAT WRITE BOUNDARY (git commit would touch 'a')` — the
  message's own first word judged as a pathspec, i.e. the allowance refused the
  command it was built to permit. Fixed by tokenizing the RAW command (the
  stripped text is used only for the prefilter).
- The staged-index test initially measured nothing: staging an already-committed,
  unmodified file leaves `git diff --cached` empty, so the index looked clean and
  the test reported "a commit was permitted" while nothing was staged. The fixture
  now MODIFIES the file before staging it.
