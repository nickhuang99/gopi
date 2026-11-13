package sessionevent

// counters.go — THE UNIT NAME OF EVERY SESSION COUNTER, and the provenance of
// the number a surface prints under it.
//
// WHY THIS FILE EXISTS (the defect it answers). One session
// (1988d8a0-874b-4f2a-976b-2e8dd37f0079, 2026-10-04) answered "how many rounds
// was this?" with FOUR different numbers — 12, 28, 24, 27 — on four surfaces,
// each of which named its unit (iteration / round / turn / event) without ever
// saying WHAT it counted or WHAT its denominator was. Not one of the numbers was
// wrong: they count four different real things. The defect was in the WORDS — a
// reader could only conclude "the same thing has four values", and a briefing
// written against that reading attributed every number to the wrong producer.
//
// So the unit names live here, once, and every surface that prints a count
// prints the unit it counted with them (PROJECT.md Rule 1/13/14: a shared value has
// ONE definition in a leaf package, and a constant that is defined and then
// bypassed by bare literals is a defect). This package is the leaf those
// consumers already share for exactly this reason — zero project imports — so a
// count printed by internal/gopilog and one printed by cmd/agent cannot drift.
//
// THE COUNTERS OF ONE SESSION, and why they legitimately disagree:
//
//	CounterUnitRound — one JSONL `request` event: one send to the model, one
//	  HTTP request/response. THIS IS THE ROUND, and it is the unit the word
//	  round names on every surface (Rule 14: one meaning per word). The
//	  operator's ruling (2026-10-12) is explicit: round ≡ iteration ≡ one HTTP
//	  request/response (one JSONL `request` row), counting notify messages and
//	  human input alike — there is no "human round" beside it. A round that
//	  carried no content is still a round (it was a call that ran and returned).
//	CounterUnitLoopPass — `session_end`'s `total_iterations`: the count the
//	  AGENT LOOP itself recorded. For a chat session it is incremented once per
//	  REPL turn ("one turn = one logical iteration regardless of internal ReAct
//	  rounds", cmd/agent/cli_chat.go), so it is a NAMED NEIGHBOUR of the round
//	  and not a second spelling of it.
//
//	  IT IS SPELLED "loop passes" AND NOT "iterations" (2026-10-16, measured on
//	  session 5c6cd329-3e34-42c0-96b2-9a406961b1e1). While this unit read "loop
//	  iterations", one CLI printed the word iteration for TWO values in the same
//	  run: `gopi session log <id> --rounds` headed its table "one JSONL `request`
//	  row … = one iteration" (134 rows) while the COST block two screens over
//	  printed "Iterations: 31" — the loop's own count. The word iteration now
//	  names the ROUND alone (CounterUnitRound: one `request` row), and the loop's
//	  count owns a word of its own that collides with nothing ("a pass", never
//	  "an iteration", never "a turn").
//	CounterUnitSessionRound — one `request` event that ended up CARRYING
//	  content. gopilog's reader drops a request whose accumulated blocks are all
//	  empty (flushIter), so this is <= CounterUnitRound. It is what a view has a
//	  BODY for, and a named neighbour of the round rather than a competing
//	  definition of it.
//	CounterUnitTurn — ONE TURN of the conversation, whatever opened it. The
//	  count is `chats.turn_count` (the ledger's record of how many turns the
//	  session RAN), and it is the ONLY thing the word "turns" names on any
//	  surface (the operator's ruling, 2026-10-27, quoted at CounterUnitTurn's
//	  own comment). It is NOT a round: one turn routinely takes many rounds
//	  (measured 2026-10-11: one log held 128 `request` rows under 16 turns).
//	CounterUnitHumanTurn — the person-only SUBSET of the turns above: the rows
//	  the OPERATOR typed. It is a detail of the turn count, never a rival total
//	  for it, so it prints under its own non-rival word and never under
//	  "turns". The producer is internal/gopilog's turn reader (SessionTurns /
//	  SessionTurnBounds); the substring "turns" inside its word is why every
//	  assertion about the bare word must be made on a word boundary, not with
//	  strings.Contains.
//	CounterUnitRawRequest — one `→ REQUEST` marker in the raw HTTP log. The raw
//	  log numbers sends the JSONL never recorded (non-streaming meta calls,
//	  retries), so this is >= CounterUnitRound and the offset is NOT constant
//	  (measured over the profile's runs: k = 0..4), which is why the two are
//	  paired by token identity and never by an offset.
//
// MEASURED on the session above: loop iterations (now "loop passes") 12, rounds
// 31, content-carrying rounds 28, raw requests 31, turns 24, frames 27.
//
// RE-MEASURED on session 5c6cd329-3e34-42c0-96b2-9a406961b1e1 (2026-10-16, the
// session this file's third unit was split for): `request` rows 134 (the ROUND),
// `session_end` total_iterations 31 (the LOOP PASS — it counts the notify-driven
// and failed REPL turns as well, which is why it exceeds the operator's own rows),
// operator rows 16 (the TURN). Three quantities, three words, no two alike.
//
// THE EXCHANGE SPLIT IS REVOKED (2026-10-12), and this is the record of why.
// A previous change (commit c04bcb345) renamed the `request`-row unit to
// "exchanges" and reserved the word round for the operator row, on the theory
// that one word for two quantities must be split into two words. The operator
// reversed it: the round IS the `request` row — that is the unit every surface
// must print, and "human round" is not a round at all but a TURN. Splitting the
// word did not remove the disagreement; it moved it (`session log --rounds`
// printed 1 while the same log held 14 `request` rows, because the surface
// tabulated turns under the word round). One word, one quantity: ROUND is the
// `request` row, TURNS is the neighbour.
//
// TWO FORMER ENTRIES ARE STILL GONE, and their removal stands: CounterUnitFrame
// ("frames") was the REPLAY's own unit — one JSONL-derived frame row. The
// playback advanced by frames, so "how long is this" had a fourth answer that no
// other surface shared, and `gopi session replay --round N` selected a FRAME
// while `gopi session log <id> N` selected a round. The replay now steps by
// CounterUnitRound — the same unit, from the same reader (internal/gopilog), so
// the two surfaces cannot print different numbers for the same session. The
// dump's own screenfuls are still counted, as a SECOND material, and the replay
// names them as dump frames rather than lending them a counter of their own: a
// screenful of cells is not a round. (CounterUnitHumanTurn, retired then as the
// replay's unit, came back in 2026-10-12 and is today the name of the
// OPERATOR-row SUBSET — the same word, a different job.)
// The unit names, and — for the counts that share a session — the NEIGHBOUR each
// one is NOT. A count printed without its neighbour named is the shape the
// 5c6cd329 defect took: three surfaces printed 134 / 31 / 16 and every one of
// them could be read as "the number of turns".
const (
	// CounterUnitLoopPass is the unit of `session_end`'s total_iterations —
	// what the agent loop counted for itself. It is NOT the number of
	// iterations: the word iteration is the ROUND's (CounterUnitRound).
	//
	// SPELLED "loop passes", never "loop iterations" (2026-10-16): see this
	// file's header for the session where the old spelling printed 31 under a
	// word another surface used for 134.
	CounterUnitLoopPass = "loop passes"
	// JSONKeyTurns is the WIRE-NAME of a session's TURN count — `chats.turn_count`
	// on every row (tools.ChatSessionDetail.TurnCount), which is the SAME quantity
	// the human table prints under "turns" and the same one
	// CounterUnitTurn names. One word, one quantity, on both faces (Rule 14).
	//
	// IT WAS SPELLED "repl_turns" FOR ONE DAY, and the reversal is the ruling
	// (2026-10-27). That spelling was invented to keep the wire face apart from a
	// rival reading of "turns" (the person's own rows), and the operator's verdict
	// is that the rival does not exist: an external notify is an input and a
	// self-driven turn is a turn, so there is nothing to disambiguate and a
	// qualifier on the wire only teaches a consumer that the number is something
	// other than the turn count. `repl_turns` survives as the ANNOUNCED
	// deprecated alias below, for one migration window.
	//
	// A STRUCT TAG CANNOT HOLD A CONSTANT, so the tag on the field carries the
	// same spelling as a literal and a test pins the two together by declaring
	// a field WITH this constant and comparing tags through reflection
	// (`cmd/agent/session_json_wire_test.go`) — the shape
	// internal/tools/tasknotify.go already uses for taskNotifyLedgerJSONKey.
	JSONKeyTurns = "turns"
	// JSONKeyTurnsCompleted is the wire-name of the LEDGER's completed count
	// (`chats.turn_count`) — the quantity JSONKeyTurns carried until 2026-11-04,
	// published under its own key so nothing was deleted when the bare word moved
	// to the log's OPENED axis.
	//
	// WHY IT MOVED: the ledger advances when a turn COMPLETES, the log records a
	// seam when one OPENS, and a turn that is interrupted or still running opens
	// without ever charging the ledger. Measured on session
	// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: the index row said `turns 29` while
	// `session replay --turns` printed 30 rows and the replay screen's last band
	// read `──── turn 30` — one session, one word, two numbers. `turn N` is a
	// POSITION, so the bare wire key carries the position count; a script that
	// needs the ledger quantity reads this key, one place to the right, with the
	// change stated here rather than discovered.
	JSONKeyTurnsCompleted = "turns_completed"
	// DeprecatedJSONKeyReplTurns is the QUALIFIED wire key this count used to
	// ride — published today ONLY so that an external script already parsing it
	// keeps working while its author moves to JSONKeyTurns.
	//
	// IT IS AN ALIAS WITH A DEADLINE, NOT A SECOND NAME: same value
	// (ChatSessionDetail.TurnCount), announced as deprecated on every row
	// (JSONKeyDeprecatedTurns) and removed once this tree reaches the date
	// recorded in DeprecatedJSONKeyReplTurnsRemoveAfter. Deleting it silently is
	// the defect it is here to prevent — an external consumer's field
	// disappearing with no notice (Rule 15: a name whose meaning a reader must
	// guess, or whose disappearance nobody announced, is the failure).
	DeprecatedJSONKeyReplTurns = "repl_turns"
	// DeprecatedJSONKeyReplTurnsRemoveAfter is the removal date of
	// DeprecatedJSONKeyReplTurns, and it is a DEADLINE a reader of the schema can
	// act on: the alias exists for migration only. It is stated here rather
	// than in the report alone because the JSON face's readers outlive any one
	// run's report.
	DeprecatedJSONKeyReplTurnsRemoveAfter = "2026-12-31"
	// JSONKeyDeprecatedTurns is the WIRE-NAME of the per-row disclosure that
	// accompanies the alias above: it states, on the payload itself, that the
	// key is a deprecated alias of JSONKeyTurns and names the key to
	// move to. A deprecation a consumer only learns about from a changelog is
	// one most consumers never learn about (Rule 15: the machine-readable face
	// must not be the one that loses the fact a human note carries).
	JSONKeyDeprecatedTurns = "deprecated_keys"
	// DeprecatedReplTurnsKeyNote is the one sentence printed INSIDE that block, on
	// every row: it names the alias, the value it still holds and the key
	// to read instead. It rides the payload because an external script's author
	// may never read this repository's report, and a deprecation nobody receives
	// is a silent removal with extra steps (Rule 15).
	DeprecatedReplTurnsKeyNote = "`repl_turns` here is a deprecated alias: it holds the same value as `turns` (this session's turn count along the axis `turn N` numbers — NOT `chats.turn_count`, which rides `turns_completed`) and is removed after " + DeprecatedJSONKeyReplTurnsRemoveAfter + ". Read `turns` instead"
	// JSONKeyHumanTurns is the wire-name of the MEASURED person-only SUBSET of
	// the turns — the SAME quantity `gopi session log <id> --turns` tabulates
	// (CounterUnitHumanTurn), shipped so a script can reach the number that
	// table prints instead of re-deriving it from the log. It is a DETAIL: it
	// never rides the bare key, and it is OMITTED when nobody measured it, with
	// JSONKeyTurnsNote saying why (Rule 15a).
	JSONKeyHumanTurns = "human_turns"
	// JSONKeyHumanTurnsKnown reports whether JSONKeyHumanTurns is a
	// MEASUREMENT; JSONKeyTurnsNote names the reason when it is not.
	JSONKeyHumanTurnsKnown = "human_turns_known"
	// JSONKeyTurnsNote is the wire-name of the stated reason the human-turn
	// subset is absent (tools.ChatTurnsNote*, Rule 15: absence is stated). It
	// never accompanies the turn count, which needs no log to exist.
	JSONKeyTurnsNote = "human_turns_note"
	// JSONKeyTurnsProvenance is the wire-name of the field that says WHICH HALF
	// of the merged listing a row's turn block belongs to — and, on the half
	// that has no turn count at all, that it has none.
	//
	// WHY THIS KEY EXISTS (epic, 2026-11-04). `gopi session list` answers ONE
	// session id with TWO rows: kind=chat, which HAS a turn count, and kind=run,
	// which is a run log and has none. Both rows used to publish `turns` (the run
	// row as 0), so a reader — or a `--json` consumer summing the column — could
	// not tell a measured zero from "no chats row stands behind this number at
	// all". The PA ruling keeps BOTH rows (they are different facts, not a
	// duplicate) and forbids collapsing them into one number; what it requires is
	// that each row carry its provenance, so the ambiguity is removed without
	// deleting anything.
	//
	// THE RUN ROW THEREFORE OMITS `turns`/`turns_completed` rather than
	// publishing 0: a 0 is a measurement claim ("this session ran no turns") and
	// a run log makes no such claim (Rule 15 — the absent fact is stated by this
	// key instead). This key is present on EVERY row, both halves, so a consumer
	// reads one field to learn what to do next instead of inferring the species
	// from whether a number happens to be zero.
	JSONKeyTurnsProvenance = "turns_provenance"
	// TurnsProvenanceChat is JSONKeyTurnsProvenance's value for a kind=chat row:
	// the number under `turns` is the session LOG's turn-opening count — the axis
	// `turn N`, a replay band and `--turns` all number along (gopilog.SessionTurns,
	// surfaced as tools.ChatSessionDetail.TurnsOpened). It names the PRODUCER
	// rather than repeating the word `turns`, because a provenance marker that
	// repeats the label is the ambiguity it is here to remove.
	TurnsProvenanceChat = "session-log-turn-openings"
	// TurnsProvenanceRun is JSONKeyTurnsProvenance's value for a kind=run row: the
	// row is a run LOG, no `chats` row stands behind it, so there is no turn
	// count to publish — `turns`, `turns_completed` and the deprecated
	// `repl_turns` alias are OMITTED on this row rather than shipped as 0. The
	// same session id's kind=chat row (when one exists) is where the count lives.
	TurnsProvenanceRun = "no-chats-row"
	// CounterUnitRound is the unit of the JSONL's `request` events — one send to
	// the model, whether or not it carried content: the HTTP request/response
	// pair the log recorded. THIS IS THE ROUND (Rule 14: one meaning per word),
	// and it is what `gopi session log <id> --rounds`, `gopi session replay
	// --rounds`'s `rounds=`, `gopi session stats`' round figure and the log's
	// own iteration count all name.
	//
	// IT WAS SPELLED "exchanges" FOR ONE DAY (2026-10-11 → 2026-10-12), and the
	// reversal is the lesson: the word the operator reads on four surfaces is
	// the word the unit carries, and a unit whose own name is not the name a
	// reader uses produces the very disagreement the rename meant to remove
	// (measured after the split: `log --rounds` 1, statement rows 14, `replay
	// rounds=` 1 — three surfaces, two quantities, one log).
	CounterUnitRound = "rounds"
	// CounterUnitSessionRound is the unit of the rounds that CARRIED content —
	// a named neighbour of the ROUND (CounterUnitRound), not a competing
	// spelling of it. It is what `gopi session log <id> --rounds` prints as the
	// CONTENT line and what an empty round's table row is marked against.
	CounterUnitSessionRound = "content-carrying rounds"
	// CounterUnitRawRequest is the unit of the raw HTTP log's `→ REQUEST`
	// markers: one request the raw log recorded.
	//
	// ITS WORD CARRIES NO "round" (measured, 2026-10-11). The verb
	// `gopi session log <id> --rounds` prints it on one line of a table whose
	// header also names the round, so while this unit was spelled "raw HTTP
	// rounds" one screen used one word for two different quantities — measured
	// on session aa48a7eb-a5ab-4507-9c4e-b717bf3583cd, with only 29 operator
	// rows in a log of 169 rounds. The bare word ROUND names the ROUND and
	// nothing else (Rule 14: one meaning per word); this unit is a request the
	// RAW LOG recorded, which is also why it is >= CounterUnitRound and why the
	// two are paired by token identity and never by an offset.
	CounterUnitRawRequest = "raw HTTP requests"
	// CounterUnitHumanTurn is the unit of the PERSON-ONLY SUBSET of the turn
	// count: how many of those turns opened at a row the operator typed
	// (internal/gopilog's SessionTurns — the rows written with
	// source=human/queue). It is a DETAIL of the turn count, never a rival
	// total for it, and that is why this word carries its own qualifier.
	//
	// IT HAS WORN TWO OTHER SPELLINGS, and both were rivals. First the bare word
	// (one word, two values); then "turns (operator rows)", which still put
	// "turns" first, so a reader skimming a table saw TWO turn counts side by
	// side and a parenthetical decided which was which. The operator's ruling
	// (2026-10-27) is that there is ONE turn count — an external notify is an
	// input, a self-driven turn is a turn — so the subset must stop presenting
	// itself as a primary reading of the word. A surface that prints both prints
	// the total first, under "turns".
	//
	// THE SUBSTRING TRAP: "human turns" CONTAINS "turns". Any assertion or grep
	// checking the bare word must work on a WORD BOUNDARY (regexp `\bturns \d`,
	// or strip the qualified clause first) — a plain strings.Contains(row,
	// "turns ") matches this word and would report a collision on a correct row.
	//
	// It is NOT the LOOP PASS either (CounterUnitLoopPass): measured on
	// 41e5dade-71e1-4efc-a012-456d0532e762, 66 human turns against 200 turns the
	// session ran — and 993 rounds. Three quantities, three words.
	CounterUnitHumanTurn = "human turns"
	// CounterUnitOtherTurn is the unit of the REST of the turn count: the turns
	// nobody at the prompt opened — a `gopi notify` delivery, the model's own
	// `chat_surface type` line. It exists so a total and its person-only subset
	// ACCOUNT FOR EACH OTHER on one screen instead of sitting beside each other
	// as two rival readings of one word.
	//
	// WHY IT IS NEEDED (measured 2026-11-01, session
	// e1e22e9f-eee0-4c33-a238-0fca04534ad9): `gopi session stats` printed
	//
	//	turns        329
	//	human turns   83
	//
	// and NOTHING on that screen said where the other 246 came from. A reader
	// can only take that as "the two turn numbers disagree" — the exact shape
	// the operator reported ("314 is neither a turn nor a round"). The 246 is a
	// MEASURED quantity, not a rounding difference: the log holds 247
	// notify-opened seams and 83 human ones (330 seams total), and the ledger's
	// 329 is that same count one behind, because the turn in flight has not
	// closed yet.
	//
	// IT IS A RESIDUAL, NOT A THIRD COUNTER: derived as total − subset and
	// printed only when BOTH are measured, so it can never claim a number of its
	// own. The total stays the turn count (Rule 14: one meaning per word).
	CounterUnitOtherTurn = "other turns"
	// CounterUnitToolCall is the unit of the `--toolcall N` selector: one tool
	// invocation, in log order. It is deliberately NOT a round and it never
	// shares that word — several calls happen inside one exchange and an
	// exchange can carry none, which is why counting them under "rounds" (the
	// defect that opened this task: `gopi session stats --rounds` printed
	// exactly the session's `tool_use` count) was wrong.
	CounterUnitToolCall = "tool calls"
)

// The NEIGHBOUR each of the three session counts is NOT — one line per surface,
// printed beside the number (Rule 15: absence is stated, and a reader who has to
// infer which quantity they are looking at is the reader the defect was written
// for).
//
// WHY THEY ARE CONSTANTS AND NOT PROSE AT EACH CALL SITE (Rule 1/13/14): five
// surfaces print these three counts, and a self-declaration that lives at five
// call sites is five chances to drift back into "the same thing has three
// values". One definition each; a consumer reads it.
const (
	// CounterUnitRoundIsNot tells a reader of the ROUND count which two
	// quantities it is not — the two numbers that sit beside it on the same
	// screens.
	CounterUnitRoundIsNot = "NOT a turn (one turn routinely takes many rounds) and NOT a loop pass"
	// CounterUnitLoopPassIsNot tells a reader of the LOOP PASS count which
	// quantity it is not.
	CounterUnitLoopPassIsNot = "NOT a round (one `request` row is one round) and NOT a turn (the ledger's count)"
	// CounterUnitTurnIsNot tells a reader of the TURN count which quantities it
	// is not.
	CounterUnitTurnIsNot = "NOT a round (one turn routinely takes many) and NOT a loop pass"
	// CounterUnitTurnBoundary is the unit of the turn SEAMS a session LOG
	// recorded: one per content that OPENED a turn (the reader's TurnBoundary,
	// internal/gopilog.SessionTurnBounds). It is the axis the tables' TURN
	// column numbers against, and it is NOT the ledger's turn count.
	//
	// WHY IT NEEDS A WORD OF ITS OWN (measured 2026-11-04, session
	// e1e22e9f-eee0-4c33-a238-0fca04534ad9): one `gopi session log <id> --turns`
	// printed `turns = 619` in its header — the ledger's `chats.turn_count` —
	// over rows whose TURN column ran to 621, and the two numbers sat under the
	// ONE word "turn" with only the header's sentence explaining either. Both
	// are true and they count different events: the LEDGER advances when a turn
	// COMPLETES (`tools.ChatRecordTurnOutcome`, one write per finished turn),
	// while a BOUNDARY is recorded when a turn OPENS. A session that was
	// interrupted — or is still running — therefore holds boundaries the ledger
	// never charged, and the gap is exactly the turns that opened and never
	// closed (2 on the session above: `state interrupt`, last two boundaries).
	// Printing one number under the other's word made an operator who looked up
	// turn 621 land on turn 619's rows.
	CounterUnitTurnBoundary = "turn boundaries"
	// CounterUnitTurnBoundaryIsNot tells a reader of the BOUNDARY count which
	// quantities it is not — the sentence printed beside it whenever the two
	// turn numbers differ.
	CounterUnitTurnBoundaryIsNot = "NOT the ledger's completed count (the ledger advances when a turn COMPLETES; a boundary is recorded when one OPENS, so an interrupted or still-running session holds boundaries the ledger never charged) and NOT a round"
	// CounterUnitTurnCompleted is the LEDGER's `chats.turn_count` under its OWN
	// word: the turns that COMPLETED.
	//
	// WHY IT NO LONGER WEARS THE BARE WORD (epic, 2026-11-04, session
	// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f): `gopi session list` printed `turns
	// 29` (the ledger) while `session replay --turns` and `chat --replay` both
	// printed 30 for the SAME session, because those two number their rows along
	// the log's turn-opening axis and the session's last turn opened without
	// closing. One session, one word, two numbers — the collision Rule 14 forbids.
	//
	// THE BARE WORD BELONGS TO THE AXIS AN ORDINAL WALKS. `--turn N`, a replay band
	// number and a table row are POSITIONS, and only a position count can carry
	// one: a completed-count can sit behind (this session) AND can run ahead
	// (measured on e364e357-a9ea-4210-a0d3-c606c6997f03: ledger 209, log 34
	// boundaries), so anchoring `turn N` on it addresses a different turn on
	// either side of that line. So `turns` is the log's turn-opening count on
	// EVERY surface, and this word carries the ledger's number as a measured
	// ANNOTATION beside it — printed only when the two differ, in numbers.
	// SPELLED SHORTER THAN IT READS (9 cells), because `session show`'s key column
	// is 12 cells wide and every key must fit in 11 so ONE separating cell
	// survives (cmd/agent/cli_session_token_total_label_test.go's budget — a key
	// as wide as the column welds its value to its label). The line prints
	// directly under the bare `turns` row, which is what makes the short spelling
	// unambiguous.
	CounterUnitTurnCompleted = "completed"
	// CounterUnitTurnCompletedIsNot tells a reader of the completed count which
	// quantities it is not — printed beside it wherever both numbers appear.
	CounterUnitTurnCompletedIsNot = "NOT the count `turn N` addresses (that ordinal walks the log's turn-opening axis, which this count can sit behind OR ahead of) and NOT a round"
)

// THE USER-FACE UNIT — the operator's ruling, 2026-10-22.
//
// The unit every session surface PRINTS is the TURN: one user input — one
// operator row to the next. What happens inside it (how many HTTP
// request/response pairs, how many tool calls) is the turn's DETAIL, carried in
// a parenthetical, never promoted to a top-level counter beside it. The
// operator's words:
//
//	用户只认为……一个请求，就是用户手动输入或者语音输入的一个文字的请求，然后呢？
//	这中间 agent Harness 去跟这个 LLM 做了多少次的这个 request response 或者说
//	tool call……所以你不能把这两个东西混为一谈
//
// This REVERSES the 2026-10-12 spelling that made the round the printed unit and
// the turn its neighbour (that ruling is quoted at length above, in
// CounterUnitRound's own comment, and it is kept there as history — it is no
// longer the shape a surface prints). Nothing about the MEASUREMENT changed:
// internal/gopilog's SessionRounds still counts `request` rows and SessionTurns
// still counts operator rows, and the JSONL's `iteration` field still numbers
// sends. What changed is which of them is the SUBJECT of a line and which is the
// annotation.
//
// The shape is copied from the face that was already right — `gopi chat
// --replay`'s notice (`chatReplayUses`): the TURN count first, then the inner
// quantities as parenthesised detail. These constants are what the session
// surfaces share so the same sentence is not re-typed at three call sites
// (Rule 14).
const (
	// CounterUnitTurn is the word the turn count is printed under on the
	// session surfaces: the plural noun, bare. It is NOT CounterUnitHumanTurn
	// ("turns (operator rows)") — that spelling exists to DISAMBIGUATE a count
	// on a table that also prints the neighbour, and it is what
	// `session log --turns` prints. A surface whose SUBJECT is the turn prints
	// this one, and its neighbours arrive in the parenthetical instead.
	CounterUnitTurn = "turns"
	// CounterUnitTurnOne is the same unit for one step: "turn 3/8".
	CounterUnitTurnOne = "turn"
	// CounterUnitTurnLabelFmt is how ONE step names itself, turn first.
	CounterUnitTurnLabelFmt = "turn %d/%d"
	// CounterUnitTurnNoDetail says a turn carried nothing measurable inside
	// it. Printing a bare "()" would look like a rendering fault (Rule 15).
	CounterUnitTurnNoDetail = "no requests recorded inside this turn"
)

// The inner quantities — what a TURN carried, printed as its ANNOTATION.
//
// The unit words are the existing neighbours' (CounterUnitRound,
// CounterUnitToolCall): the parenthetical does not rename them, it demotes them
// from a headline to a detail (Rule 14: one meaning per word, whatever the
// prominence).
const (
	// CounterDetailRequestsFmt is the parenthetical's request half. It says
	// "request", not "round", because that is what the number IS: one JSONL
	// `request` row = one HTTP request/response. The surfaces that still name a
	// round (the addressable per-round views) use CounterUnitRound for it.
	CounterDetailRequestsFmt = "%d request(s)"
	// CounterUnitInnerRequest is the same quantity as a LABEL — the stats box's
	// demoted key. A surface that prints the turn count as its headline still
	// owes the reader the request count, and it prints it under a word that
	// names the thing measured rather than under the headline's word.
	CounterUnitInnerRequest = "requests"
	// CounterUnitInnerRequestOne is that word's singular, for a line that
	// numbers ONE request ("avg 27,056,960/request"). Typed here rather than
	// sliced off the plural at the call site: a suffix trim is a rename waiting
	// to break silently (Rule 13).
	CounterUnitInnerRequestOne = "request"
	// CounterDetailToolCallsFmt is the parenthetical's tool-call half.
	CounterDetailToolCallsFmt = "%d tool call(s)"
	// CounterDetailJoin separates the halves inside the one parenthetical.
	CounterDetailJoin = ", "
	// CounterRoundRangeFmt is how ONE TURN states the interval of ROUNDS it
	// covered, in the ROUND ADDRESS SPACE (`--round N`): "rounds 143–152".
	//
	// It exists because the two units sat side by side without either naming
	// the other's extent: a listing whose row number was a step and whose label
	// read "round 152/152" let an operator ask for a whole turn as `--round
	// 152` and get ONE round of it (measured: 3,419 bytes against the turn's
	// 12,436, with eight tool calls missing). The interval is what makes
	// "a turn is not a round number" checkable by the reader.
	CounterRoundRangeFmt = "rounds %d–%d"
	// CounterRoundRangeEmpty says a turn covered no round at all, so the
	// interval has no first or last. A bare "rounds " would read as a rendering
	// fault (Rule 15).
	CounterRoundRangeEmpty = "no rounds inside this turn"
	// CounterRoundIsOneSpotFmt is what a per-round view says about ITSELF: the
	// round is one round of a turn, the turn is the unit, and the way to read
	// the whole thing is named — with the flag, so the reader can copy it.
	// Args: turn, turn total, the round interval (FormatTurnRoundRange), flag,
	// turn.
	CounterRoundIsOneSpotFmt = "this view is ONE round, not a turn: it belongs to turn %d/%d, which covers %s — for the whole turn ask %s %d"
	// CounterRoundNoTurn is that sentence's honest other half: a round that no
	// turn span covers (a session whose log recorded no operator row) has no
	// turn to name, and inventing one would be a numbering the ledger does not
	// have (Rule 15).
	CounterRoundNoTurn = "this view is ONE round, not a turn: no turn spans this round (the log recorded no operator row to open one)"
	// CounterDetailOpen / CounterDetailClose wrap the annotation. They are
	// named so the shape ("turn first, detail in parentheses") is one spelling
	// rather than three renderers that happen to agree today.
	CounterDetailOpen  = "  ("
	CounterDetailClose = ")"
)

// The provenance words: which producer fed a count is itself part of the count,
// because the same label means different things on the two paths (see the file
// header). A surface prints one of these beside the number rather than leaving
// the reader to guess which producer ran (Rule 15: absence is stated).
const (
	// CounterSourceSessionEnd marks a count taken from the session_end event.
	CounterSourceSessionEnd = "session_end total_iterations"
	// CounterSourceSessionEndAccumulated marks a pass count that SUMS the
	// `session_end` events of the whole log, one per run segment.
	//
	// Why the qualifier exists (task 01a17ae2, measured on session
	// 4a0957a3-bb4e-4337-a761-20606c87d1fd): a session that was resumed holds
	// several run segments, and a resume that ran no turn writes its own
	// `session_end total_iterations: 0` — because the REPL's pass counter is
	// per PROCESS (cmd/agent/cli_chat.go, task 01a16fb3). A reader that kept
	// only the LAST end therefore reported 0 passes for a session that had run
	// 29. The session-level count is the sum over the segments, and a surface
	// printing it says so: the same log's tail literally reports `0` for the
	// run that just ended, so "session_end total_iterations" alone would name
	// the producer while leaving the SPAN wrong.
	CounterSourceSessionEndAccumulated = "the SUM of every `session_end` total_iterations in the log, one per run segment — a resumed session keeps the passes its earlier runs recorded instead of an empty last segment's 0"
	// CounterSourceDerived marks a token/elapsed figure DERIVED from the JSONL
	// because the session recorded no end — a live, killed or crashed run.
	//
	// IT NO LONGER QUALIFIES A PASS COUNT (2026-10-26, measured on session
	// 41e5dade-71e1-4efc-a012-456d0532e762): the reader used to fill
	// TotalIterations with the `request`-row count whenever session_end was
	// missing, and this word was the caption printed beside it. On that session
	// the two surfaces then read
	//
	//	Iterations: 468  (rounds — one JSONL `request` row = …)
	//	Passes:     468  (loop passes — derived from JSONL `request` events …)
	//
	// — one number under two words, while the LIVE screen of the same run read
	// turn120 (the REPL's own pass counter, and the only real pass figure that
	// existed). A caption saying "derived" does not make a ROUND count into a
	// PASS count: the pass count is a fact `session_end` records and no other
	// row does, so when it is absent the honest answer is
	// CounterPassUnrecorded, never the neighbour's number.
	CounterSourceDerived = "derived from JSONL `request` events — this session recorded no session_end"
	// CounterPassUnrecorded is what a surface prints where the LOOP PASS count
	// belongs when the session never wrote `session_end` (a live, killed or
	// crashed run). It replaces a fabricated number: the pass count has ONE
	// producer — the agent loop's own `total_iterations` — and a reader that
	// printed the `request`-row count here was printing the ROUNDS under the
	// PASS word (measured 2026-10-26: 468/468 on session 41e5dade).
	CounterPassUnrecorded = "not recorded — this session wrote no `session_end`, so the loop never reported its pass count (the `request`-row count is the ROUNDS, a different quantity)"
)

// ── the TURN↔ROUND JOIN's sentences ────────────────────────────────────────
//
// A reverse lookup states TWO numbers in one line (a round's owner, or a
// turn's interval), so the risk it carries is the defect this package exists
// for, one layer out: the reader takes one of the two numbers for the other.
// Each shape below therefore names BOTH units with the unit words declared
// above, and the shapes live HERE rather than in internal/gopilog because the
// CLI, the tests and any future reader must spell them identically (Rule 14).
const (
	// CounterRoundOwnerFmt answers "which turn does this round belong to?".
	// Args: the round, the unit word, the turn, the unit word, the turn's own
	// round interval ("turn 1 covers rounds 1–12").
	CounterRoundOwnerFmt = "round %d belongs to %s %d — %s %d covers %s"
	// CounterTurnOwnsFmt answers "which rounds does this turn contain?".
	// Args: the unit word, the turn, the turn's round interval, the session's
	// turn total.
	CounterTurnOwnsFmt = "%s %d covers %s (of %d %s)"
	// CounterTurnNoRoundsFmt is the honest other half: the turn exists in the
	// session's numbering and ran no request, so it has an interval of nothing
	// — a different answer from "no such turn", which is an error.
	// Args: the unit word, the turn, the session's turn total.
	CounterTurnNoRoundsFmt = "%s %d of %d ran no request, so it covers no round"
	// CounterJoinTotalSuffix is appended by a caller that printed the whole
	// mapping rather than one line: it says how many turns were listed, so a
	// truncated or empty listing discloses its own size (Rule 15).
	CounterJoinTotalFmt = "%d %s"
)
