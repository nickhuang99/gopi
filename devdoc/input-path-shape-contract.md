<!-- doc-class: normative -->
# The path-shape contract of a bare positional (`gopi … /tmp/fx/nope.txt`)

Status: implemented 2026-09-27. Finding: end-user audit **slice 8, RP-2 (F2,
high)** — the audit doc `devdoc/end-user-audit-slice8-runpath.md` is commit
`c3f14810` on branch `peerbox/task/01a0dcfadafb709d808c8bc93369aa63` (that commit
is the largest one containing the doc, not necessarily that branch's tip —
verified with `git show c3f14810:devdoc/…`, never by a branch name alone).
Task row: `01a0dec6-280c-71f8-adf4-90067106a49f`.

## The defect, as measured by the audit

| spelling | before | after |
|---|---|---|
| `gopi … /tmp/fx/nope.txt` | rc=0, a real run, a session dir + ledger row, and the PATH STRING sent as the task text | rc=1, refusal naming the path, `0` session dirs, `0` ledger rows |
| `gopi … @/tmp/fx/nope.txt` | rc=1, `Error reading @ payload file …: no such file or directory` | unchanged |

Two spellings of ONE input cannot have opposite failure semantics in silence.
`gopi -h` Usage line 2 (`gopi [options] input.txt   Run one task read from a
text file`) and the `image.png` Inputs row ("a positional naming an EXISTING
.png/… file") describe a file positional as READING a file; the audit's S2b
probe (echo endpoint) proved the bare spelling instead ran a real task whose
user text was the path — reproduced here at wire level (`req-013.json`), and
again against the pre-fix binary built from this worktree (rc=0, `+1` session
dir, `+32` ledger rows).

## The rule that closes it (narrow on purpose)

A refusal is a statement about the token's SHAPE, never a guess about what the
operator meant — so the classifier is deliberately small:

| token | verdict | why |
|---|---|---|
| `/abs/path.txt`, `./missing.md`, `sub/dir/note.txt`, `~/x.png` | refused if missing | contains `/` |
| `/tmp/fx/nope`, `./frobnicate`, `../anything` | refused if missing | contains `/` (a separator IS path shape, extension or not) |
| `missing.png`, `pic.webp` | refused if missing | the documented attachment extension (no separator needed) |
| `notes.txt`, `BRIEF.MD` | refused if missing | the text-payload extensions the page itself spells |
| `frobnicate`, `test`, `version` | prompt text (unchanged) | no separator, no known extension |
| `check the tests in foo_test.go and tell me what fails` | prompt text (unchanged) | whitespace ⇒ a sentence |
| `your task is written in image ~/x.png`, `see /tmp/nonexistent.png for details` | prompt text (unchanged) | whitespace ⇒ prose that MENTIONS a path |

The whitespace rule is what keeps the two documented prose controls
(`cmd/agent/main_test.go`'s `TestReadInput_LiteralPromptText`,
`TestReadInput_ImageExtensionWithoutFile`) green, and it is why an arbitrary
"looks like a filename" heuristic was NOT built: a single word ending in an
unknown extension is not decidable from the word, and a wrong guess costs a run.

The refusal message names the path, the shape datum, the missing-file reason and
BOTH legal spellings (current wording; the earlier pre-`4f645ecc` template is
quoted below in "The refusal message is part of the contract"):

```
no such file at "/tmp/fx/nope.txt" (it contains the path separator "/") — gopi
reads a path-shaped positional as a FILE, so this is refused rather than sent to the
model as prompt text. Spell the file with @/tmp/fx/nope.txt, or pass the text
deliberately: `--prompt "/tmp/fx/nope.txt"` (or the escaped literal \@/tmp/fx/nope.txt)
```

It runs in `readInput`, i.e. **before** the agent is constructed — measured: no
session dir, no ledger row, `0` bytes on stdout, in the same sandbox that a
positive control fills with rows seconds later.

## A second defect found by executing the advised spelling

The refusal tells the operator to use `\@/tmp/fx/nope.txt` for a literal prompt
starting with `@`. Executing that advice (probe P9 / C4) showed it did NOT work
on a positional: the `\@` branch lived only in `readPayloadFile`, reached from
`--prompt` and `GOPI_PROMPT`, so `\@x` as a positional fell through to the
missing-file classification — the refusal would have advised a spelling that
does not work. `readInput` now carries that escape branch (`literalPromptEscape`,
one definition, also used by the three `\@` comparisons in the same file), and
C4 proves the wire text is the literal `@…` string (rc=0, run proceeds).

## Where the code lives

| file | what |
|---|---|
| `cmd/agent/inputpath.go` | `pathShapedReason`, `missingPathPositional`, `pathSeparator`, `prosePathExtensions`, `literalPromptEscape` — the classifier, its constants and the finding's doc-comment |
| `cmd/agent/main.go` (`readInput`) | the refusal call, placed after `os.Stat` fails and before the token becomes prompt text; the `\@` escape branch |
| `internal/config/help.go` (`inputRows`) | the `gopi -h` contract sentence |
| `cmd/agent/inputpath_test.go` | classifier table + `readInput` regression + positive controls |
| `internal/config/help_pathshape_test.go` | the page must state the contract (word-level, wrap-insensitive) |

## Tests, and the red proof

`cmd/agent/inputpath_test.go` — `TestPathShapedReason` (the classifier table,
including a separate assertion that `missing.png` is refused by the IMAGE branch
and not by the `.txt`/`.md` loop), `TestReadInput_MissingPathShapedRefused`,
`…_MatchesAtTwin`, `…_ProseAndPlainWordsStillRun`,
`…_ExistingPathShapesUnchanged`, `…_PromptFlagMakesTheRefusalSpellingsReal`.

Red-then-green: stubbing `pathShapedReason` to return `""` fails
`TestPathShapedReason` and the `TestReadInput_MissingPathShaped*` cases (the
positive controls stay green, which is the point — the refusal is the only thing
the stub can reach); restoring the body goes green.

`internal/config/help_pathshape_test.go` gates the page. It keys on the words
the classifier implements (`a token that LOOKS like a path`, `is a FILE`,
`refused by name`, `exit 1`, both spellings, the extension list) inside the
Inputs block, with wrap/indent collapsed, so the page cannot drift from the
behaviour. The pre-existing no-input promise is asserted too, so the edit could
not quietly drop it.

Branch-scoped runs on this branch (`go test` of the touched packages; the whole
package is 300s+ and is the merge path's job, Rule 27):

```
go test ./internal/config/ -count=1 -run 'Help|Usage|Format|TaskEnum|EnvClaim'   → ok
go test ./cmd/agent/ -count=1 -run 'PathShaped|ReadInput|HelpPathshape'         → ok
go vet ./cmd/agent/ ./internal/config/                                          → exit 0
```

**An honest note about `./cmd/agent` as a whole.** Its full-package run is RED
for two tests that have nothing to do with this change —
`TestScratchGCDepsOriginReadsExeNotArgv0` (a `deps`-verdict fixture expecting
`foreign`, seeing `unknown`) and `TestWorkerLifecyclePass_AutoDisposesConfirmedZombie`
(suite timeout: `cmd/agent` is far past the 5-minute default). Both were measured
unchanged on a **pristine detached worktree at the same HEAD** (no fix present),
so neither is caused by this work; both are pre-existing and belong to whoever
owns those areas.

## Evidence (real binary, echo endpoint, sandbox)

Sandbox `/home/user/workspace/gopi-sandbox/sandbox-rp2final2-eab6343f` (created
through `gopi sandbox create`, Rule 34 — never `mktemp`); local
Anthropic-shaped SSE echo endpoint (0 real tokens) recording every request body;
fixtures under the *session* directory, so the worktree stays clean (Rule 30).

The two runs below are the ones this doc is built on. The isolated first pass
(`P1…P9`, echo endpoint on `127.0.0.1:18131`) is what found the `\@` defect and
is quoted in the table above; the **final pass** is the one whose binary this
branch ships, and it is the pair the acceptance criterion asks for:

```
$ bin/gopi --db <sb>/.gopi/memory.db /tmp/fx/nope.txt      # fixed binary (md5 197cf354…)
rc=1  sessions_delta=+0  ledger_delta=+0  echo bodies 45->45 (none reached the endpoint)
no such file at "/tmp/fx/nope.txt" (it contains the path separator "/") — gopi reads a
path-shaped positional as a FILE, so this is refused rather than sent to the model as
prompt text. Spell the file with @/tmp/fx/nope.txt, or pass the text deliberately:
`--prompt "/tmp/fx/nope.txt"` (or the escaped literal \@/tmp/fx/nope.txt)
# NOTE: this block was captured before the grammatical repair 4f645ecc, when the
# message still read `"/tmp/fx/nope.txt" it contains the path separator "/", but no
# such file exists — …`; the wording above is the delivered one. Counters unaffected.

$ bin/gopi --db <sb>/.gopi/memory.db @/tmp/fx/nope.txt      # the @PATH twin
rc=1  sessions_delta=+0  ledger_delta=+0  echo bodies 45->45 (none reached the endpoint)
Error reading @ payload file "/tmp/fx/nope.txt": open /tmp/fx/nope.txt: no such file
or directory (escape a literal prompt starting with @ as \@)
```

BEFORE/AFTER, same argv, same sandbox, same endpoint — the defect and its fix
as a pair (`pc-PREFIX_nope_txt` / `pc-FIXED_nope_txt` in the session's evidence
dir). Pre-fix binary built from this worktree before the fix
(`bin-gopi-pre`, md5 `7b96188208364e83d2e1d3dd1a0b9b3c`):

```
BEFORE  rc=0  sessions +1  ledger +18  bodies 59->61  wire text = '/tmp/fx/nope.txt'
AFTER   rc=1  sessions +0  ledger  +0  bodies 61->61  (no request reached the endpoint)
```

Full positive/negative matrix of the final pass (`final-measure.tsv`; the F1/F2
pair is the acceptance criterion, `F14` a control proving the echo endpoint is
the thing that produces the deltas):

| probe | argv | rc | sessions | ledger | bodies |
|---|---|---|---|---|---|
| F1 bare missing `.txt` | `/tmp/fx/nope.txt` | 1 | +0 | +0 | 45→45 |
| F2 `@` twin | `@/tmp/fx/nope.txt` | 1 | +0 | +0 | 45→45 |
| F3 bare missing, no ext | `/tmp/fx/nope` | 1 | +0 | +0 | 45→45 |
| F4 ext, no separator | `nope_missing.txt` | 1 | +0 | +0 | 45→45 |
| F5 relative, no ext | `./frobnicate` | 1 | +0 | +0 | 45→45 |
| F6 plain word | `frobnicate` | 0 | +1 | +18 | 45→47 |
| F7 prose mentioning a path | sentence with `/tmp/nonexistent.png` | 0 | +1 | +18 | 47→49 |
| F8 existing text file | real `.txt` | 0 | +1 | +18 | 49→51 |
| F9 existing image | `--prompt describe shot.png` | 0 | +1 | −40 | 51→53 |
| F10 `\@` escape | `\@…` literal | 0 | +1 | +18 | 53→55 |
| F11 `--prompt <path>` | text spelled deliberately | 0 | +1 | +18 | 55→57 |
| F12 directory positional | existing dir | 1 | +0 | +0 | 57→57 |
| F13 `--prompt` + token | both | 0 | +1 | +18 | 57→59 |
| F14 bad endpoint | `--base-url http://127.0.0.1:9` | 1 | +0 | +0 | 59→59 |

`F12` keeps its pre-existing text (`Error reading input file: … is a directory`)
— the new refusal does not pre-empt it. `F14` is why the echo endpoint matters:
a refused endpoint also yields rc=1 with zero deltas, so it proves the deltas in
`F6…F11` come from the run, not from the harness.

Source of the measured binary: `SRC_TIP 4d7cb918`, `SRC_DIRTY_SHA 31c5aadd`
(the fix's files) — the branch tip that carries them is the commit this doc
shipped in; the md5 covers the CODE, not the commit, so it is reproduced by
rebuilding the tip (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`), never by
checking out a commit named here.

### Independent re-measurement (this session, fresh artifacts)

The acceptance line was re-run from zero on the tip `36317d40`, not re-read:
`CGO_ENABLED=0 go build -o /tmp/rp2-verify-gopi ./cmd/agent/` reproduces
`md5 197cf354c6eeb7b20a18c5045a97604e` (`cmp`-identical to `bin/gopi`), the
scoped tests pass (`cmd/agent -run 'PathShape|ReadInput'` → `ok 2.623s`,
`internal/config -run 'PathShape|Inputs'` → `ok 2.556s`, `gopi -h` = `--help` =
`1771cffe…`), and the red proof re-observed in a throwaway detached worktree
(stub `pathShapedReason` → 3 tests FAIL / `FAIL 1.572s`; restore → `ok 9.466s`).

Detection was verified from scratch on sandbox `sandbox-rp2final-8505fb19`
(device `66309` ≠ `/` `66307`) with the echo endpoint (**launched with the
sandbox as its cwd**) on `127.0.0.1:37111`:

```
PRECONDITION sessions=6 ledger=62 tables=25 ; /tmp/fx/nope.txt exists=no
bare_missing    rc=1 sessions 6->6 (+0) ledger 62->62 (+0) bodies 0->0
at_missing      rc=1 sessions 6->6 (+0) ledger 62->62 (+0) bodies 0->0
plain_word      rc=0 sessions 6->7 (+1) ledger 62->80 (+18) bodies 0->2
prose_mention   rc=0 sessions 7->8 (+1) ledger 80->98 (+18) bodies 2->4
existing_txt    rc=0 sessions 8->9 (+1) ledger 98->116 (+18) bodies 4->6
escaped_at      rc=0 sessions 9->10 (+1) ledger 116->134 (+18) bodies 6->8
directory       rc=1 sessions 10->10 (+0) ledger 134->134 (+0) bodies 8->8
```

The refusal pair's zeros are a measurement, not a dead probe: the SAME counters
move on the controls seconds later, and the wire carries `frobnicate` /
`see /tmp/fx/nope.txt for details` in exactly their own runs' bodies while the
missing path appears in the task-text bodies **0** times. The `.png` positional
still attaches (`F9` in the table above, rc=0). The worktree is clean
(`git status --porcelain` prints nothing).

## Independent re-measurement #2 (sandboxed, `GOPI_HOME` = sandbox), 2026-09-27

A third-party re-run of the acceptance line, executed through `gopi sandbox run` so the child
inherits the sandbox as `GOPI_HOME` — the two refusals returned rc=1 with **zero** delta in
session dirs, `tasks` rows and request bodies, while four positive controls moved those very
counters in the same process (so the zeros are a measurement, not a dead probe). Verbatim
numbers, refusal texts and the collateral disclosure of an earlier, non-sandboxed probe of the
same session are in the task report's
"Independent acceptance re-measurement #2" and "Disclosure" sections
(`$GOPI_TASK_REPORT_PATH`).

**Lesson worth keeping (probe hygiene, not this fix):** a `gopi` run started without
`GOPI_HOME` pointing into a sandbox resolves the OPERATOR's profile — it writes session dirs
and an auto-registered, CLAIMABLE ledger row naming the prompt as its summary. For any run-path
probe, `gopi sandbox run … -- <bin> …` is the shape that keeps the write where it belongs.

## Help-page hash after the repair (supersedes `1771cffe…`)

The 160-line budget repair (see the report's "Post-review repair" section) rewrote the
Inputs rows and other one-line entries, so the page bytes changed:

```
pre-repair page  (165 lines): md5 1771cffe94415768a1d4e23966860a99
post-repair page (159 lines): md5 89f06f51367a779a515eb07f6c4e107a
```
`gopi -h` and `gopi --help` still render IDENTICAL bytes (verified: both `89f06f51…`), and
`TestTopLevelUsageDelegatesEnvVarsToHelpEnv` (the 160-line budget) passes.

## The refusal message is part of the contract — so it must be a sentence

The first shipped wording pasted the shape reason beside the path:

```
"/tmp/fx/nope.txt" it contains the path separator "/", but no such file exists — gopi reads …
```

`%q` followed by a phrase written as a full clause (`it contains …`) is not a sentence, and
this message is the ONLY thing the operator sees when the refusal fires. Fixed in
`missingPathPositional` to give every fact its own clause, and pinned by
`TestMissingPathPositional_NamesThePathInOneGrammaticalClause` (RED on the old template,
GREEN after — both assertions fired with the transcript quoted in the report):

```
no such file at "/tmp/fx/nope.txt" (it contains the path separator "/") — gopi reads a
path-shaped positional as a FILE, so this is refused rather than sent to the model as
prompt text. Spell the file with @/tmp/fx/nope.txt, or pass the text deliberately:
`--prompt "/tmp/fx/nope.txt"` (or the escaped literal \@/tmp/fx/nope.txt)
```

Measured shape invariant: a refusal is emitted with `rc=1` and the ledger file is
BYTE-IDENTICAL afterwards (`db_same=YES`), so the message is also a proof that nothing was
written — `…/rp2-evidence/tip2-measure.tsv`.
