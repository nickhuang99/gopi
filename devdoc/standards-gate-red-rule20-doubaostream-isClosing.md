<!-- doc-class: historical -->
# The Rule 20 red on master — the real hit is `doubaostream`, not `shot/x11.go`

Task `01a11e1d-a226-7d6a-acde-6dbc4b6f2975` · branch
`task/01a11e1da2267d6aacde6dbc4b6f2975` · base = master tip `f5ffc27e2`.

## What the briefing said, and what the tree measured

The briefing blamed `internal/shot/x11.go:1064 getRootImage` for master's Rule 20
red. **That is a mis-attribution.** `getRootImage` is ESCAPED, and it has been
escaped since the gate landed: the marker sits on line 1063, inside the
contiguous comment block directly above the func, which is exactly the window
`linesAbove()` (`internal/standards/gate.go`) collects — it walks upward from the
offending line while every line is still a `//` or `/*` line, so the two-line doc
comment plus the marker line is one contiguous block. The data report says so in
its own row:

```
Rule 20 (no get/is/handle prefix on an accessor) (checklist 1.4): hits=2 baselined=0 escaped=1 NEW=1
  escape: internal/shot/x11.go:1064  [getRootImage — drop the get prefix (accessor) or take an argument (command)]
```

`escaped=1` is `x11.go:1064`; the one hit that is neither baselined nor escaped
is named by the failure line, not by the escape line:

```
NEW violation (checklist 1.4) at internal/doubaostream/session.go:398
  [isClosing — drop the is prefix (accessor) or take an argument (command)]
```

`internal/doubaostream/session.go:398 func (s *Session) isClosing() bool` was
introduced by `823305a17` *feat(voice): streaming dictation*, an ancestor of
master (`git merge-base --is-ancestor 823305a17 master` → YES) — confirmed by
`git_blame` on `session.go:395-402`, every line of which is `823305a17`.

**Why the mis-attribution is worth writing down**: two rows of the same report
carry the same `file:line` shape, and only one of them is a defect. The rule is
to read the `NEW violation …` line, never to infer the offender from a
`file:line` that appears in the report at all — the escape list exists precisely
to keep reviewed, deliberate hits VISIBLE, so it prints offenders and permits in
one list. (This is the same trap recorded in memory id #3219.)

## The remedy: GATE `A` — rename in place

Rule 20 carries two sanctioned exits, and the choice between them is decided by
whether the name mirrors an external spec:

* **`// name-ok: <reason>`** — NOT taken here. That escape is for a name the code
  is not free to choose: `getRootImage` mirrors the X11 `GetImage` request it
  implements, so renaming it would obscure the wire spec. Nothing external names
  `isClosing`; it is our own accessor.
* **baseline.go** — NOT taken here, and this one is structural: the Rule 20
  baseline is `[]string{}` *by construction*, and a permit list may only ratchet
  down (`accessorPrefixBaseline`'s own comment: "the baseline stays empty"). It
  exists for legacy sites that predate the gate; this site is NEW code.
* **The rename** — taken. `isClosing` → **`closeRequested`**.

The new name is chosen to say what the accessor *reports*, not to avoid a
prefix: the flag it reads is `closed`, and the semantic documented in the very
comment above it is that the flag is set **before** the connection is closed —
so the question the method answers is "has a close been REQUESTED?", not "is the
session closed". `closing` was rejected as a name because it reads as the
present-participle of `Close` (i.e. "a close is in flight"), which is a different
fact from the one the flag carries.

Diff (Rule 4, surgical — the method, its doc comment, and its two call sites in
`readLoop`; the semantics and the locking are untouched):

```
internal/doubaostream/session.go:395-398   // isClosing reports … → // closeRequested reports …
internal/doubaostream/session.go:400       func (s *Session) isClosing() bool → func (s *Session) closeRequested() bool
internal/doubaostream/session.go:360       if s.isClosing() { → if s.closeRequested() {
internal/doubaostream/session.go:369       if s.isClosing() { → if s.closeRequested() {
```

`grep -rn isClosing internal/ cmd/` → no hits. `internal/shot/x11.go` is
deliberately untouched: it is not a defect, and its `name-ok:` marker is the
correct, reviewed disposition.

## Verification (observed output, not expectation)

* Before the fix, at this branch's base `f5ffc27e2` (= master tip):
  `Rule 20 (no get/is/handle prefix on an accessor) (checklist 1.4): hits=2 baselined=0 escaped=1 NEW=1`
  plus the failure line
  `NEW violation (checklist 1.4) at internal/doubaostream/session.go:398 [isClosing — …]`.
* After the fix:
  `Rule 20 (no get/is/handle prefix on an accessor): 1 total hits, 0 baselined, 1 escaped, 0 new (unannotated), 0 baseline entries`
  and in the data report `Rule 20 … (checklist 1.4): hits=1 baselined=0 escaped=1 NEW=0`.
* Note the shape of the arithmetic: the hit count went **2 → 1**. The rename
  removed the hit from the predicate's output entirely — it was not re-classified
  as an escape and it was not baselined, so Rule 20's `escaped=1` now names
  `internal/shot/x11.go:1064` alone, and `NEW=0`. (Re-classifying a hit as an
  escape would have shown `escaped=2`; adding a permit would have shown
  `baselined=1`. Neither happened.)
* `go build ./...` → exit 0.
* `go test ./internal/doubaostream/` → green, including
  `session_finish_drain_test.go` and `ws_read_deadline_test.go`, which drive the
  renamed accessor through `readLoop`.

## The measurement's home

This is a `historical` doc: it is the record of this run at `f5ffc27e2`. The
numbers above are working-tree measurements at that commit, not standing claims
about master — a later commit will move them, and that is expected, not staleness.
