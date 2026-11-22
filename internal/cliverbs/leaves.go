package cliverbs

// leaves.go — THE LEAF-VERB SOURCE: one row per (family, leaf) pair, for the
// families whose second level a human types as a standalone word.
//
// WHY IT EXISTS (task 01a1e5a2, operator's ruling 2026-11-17). `gopi task
// submit`, `gopi session show`, `gopi memory store` — the LEAF words of a family
// — were in NO vocabulary that internal/config's run-path guards consult, so
// `gopi submit`, `gopi list`, `gopi show` fell through the dispatch chain and
// were SPENT AS PROMPTS: a session directory, a paid agent run, and an
// `[auto] submit` ledger row (measured in this profile's ledger: 6 such rows).
//
// The family WORDS were already covered (`PositionalVerbs`), and so were the
// single-word leaves that happen to name a whole command somewhere (`archive`,
// `show`, `raw`). What no table held is the pair (family, leaf) — the thing the
// refusal must SPELL ("you meant `gopi task submit`, see `gopi task submit -h`")
// and the thing a shadow list would drift from (Rule 13/14).
//
// WHY A ROW TABLE AND NOT A BARE []string. A leaf word alone is not enough to
// refuse safely: `list` is a leaf of `task`, `memory` AND `session`, and it is
// also a legitimate one-word prompt for a person who meant "list". The refusal
// therefore points at the family whose surface page actually documents that
// leaf, and that pointing fact must be recorded once, in one place — here.
//
// WHY IT IS NOT DERIVED FROM THE FAMILIES' DISPATCH TABLES. It is a COPY of
// them, and it is a copy for one measured reason: the families live in
// cmd/agent (package main — unimportable, Rule 14's leaf reason), and the three
// that live in leaf packages (taskverbs, internal/tools) do not agree on how a
// leaf row is spelled (`tools.LedgerBackupVerb` vs a bare string inside
// `memorySubcommandNames`). What makes the copy safe is the DRIFT GATE, in both
// directions, on the real binary and on the source tables it came from
// (internal/cliverbs/leaves_drift_test.go + cmd/agent/cli_leaf_verbs_drift_test.go):
// a leaf added to a family without a row here fails, and a row here whose leaf
// no longer dispatches fails.
//
// A family is listed ONLY when the bare leaf word would otherwise reach the run
// path — i.e. the word dispatches at ONE level only (as a family member). The
// families whose leaves also dispatch at TOP level (`gopi screen`, `gopi config`,
// `gopi completion`) are deliberately absent: a word the dispatch chain already
// resolves is not a fall-through risk, and `gopi show` (which the dispatch chain
// resolves to the RETIRED `show` spelling) is refused with its own pointed
// message rather than with a family pointer. The gate asserts that absence.
type FamilyLeaf struct {
	// Family is the GROUP verb spelling the leaf lives under (`task`).
	Family string
	// Leaf is the family's second-level word (`submit`).
	Leaf string
	// Note is the ONE-line hint the refusal appends for this family, saying
	// what the family is FOR, so a reader who never knew the leaf existed learns
	// where to look. Optional ("" = no note).
	Note string
}

// FamilyLeaves is the table. Order is by family, then by leaf, so the source is
// scannable and a test can compare it to the families' own lists in that order.
var FamilyLeaves = []FamilyLeaf{
	// task — the ledger family (`gopi task -h`). Its leaves are the ledger
	// operations and the retired spellings that still answer with a pointer.
	{Family: Task, Leaf: "adopt"},
	{Family: Task, Leaf: "archive"},
	{Family: Task, Leaf: "audit-merges"},
	{Family: Task, Leaf: "branch-refs"},
	{Family: Task, Leaf: "clone"},
	{Family: Task, Leaf: "dedup-scan"},
	{Family: Task, Leaf: "diff"},
	{Family: Task, Leaf: "finish-cleanup"},
	{Family: Task, Leaf: "for-branch"},
	{Family: Task, Leaf: "harvest"},
	{Family: Task, Leaf: "improvements"},
	{Family: Task, Leaf: "list"},
	{Family: Task, Leaf: "merge"},
	{Family: Task, Leaf: "merge-execute"},
	{Family: Task, Leaf: "merged-cleanup"},
	{Family: Task, Leaf: "merger"},
	{Family: Task, Leaf: "new"},
	{Family: Task, Leaf: "next"},
	{Family: Task, Leaf: "orphans"},
	{Family: Task, Leaf: "pull"},
	{Family: Task, Leaf: "push"},
	{Family: Task, Leaf: "receive"},
	{Family: Task, Leaf: "reconcile"},
	{Family: Task, Leaf: "retry-delivery"},
	{Family: Task, Leaf: "run"},
	{Family: Task, Leaf: "search"},
	{Family: Task, Leaf: "send"},
	{Family: Task, Leaf: "serve"},
	{Family: Task, Leaf: "show"},
	{Family: Task, Leaf: "status"},
	{Family: Task, Leaf: "stranded"},
	{Family: Task, Leaf: "submit"},
	{Family: Task, Leaf: "sweep-worktrees"},
	{Family: Task, Leaf: "sweep-zombies"},
	{Family: Task, Leaf: "sync"},
	{Family: Task, Leaf: "sync-pass"},
	{Family: Task, Leaf: "update"},
	{Family: Task, Leaf: "verify"},
	// session — the session family (`gopi session -h`).
	{Family: Session, Leaf: "checkpoint"},
	{Family: Session, Leaf: "context"},
	{Family: Session, Leaf: "gc"},
	{Family: Session, Leaf: Log},
	{Family: Session, Leaf: "list"},
	{Family: Session, Leaf: "notes"},
	{Family: Session, Leaf: Raw},
	{Family: Session, Leaf: "replay"},
	{Family: Session, Leaf: "resume"},
	{Family: Session, Leaf: Show},
	{Family: Session, Leaf: Stats},
	// memory — the knowledge-base family (`gopi memory -h`).
	{Family: "memory", Leaf: "backfill-tokens"},
	{Family: "memory", Leaf: "backup"},
	{Family: "memory", Leaf: "brew"},
	{Family: "memory", Leaf: "capture-stats"},
	{Family: "memory", Leaf: "converge-keywords"},
	{Family: "memory", Leaf: "delete"},
	{Family: "memory", Leaf: "feedback"},
	{Family: "memory", Leaf: "get"},
	{Family: "memory", Leaf: "list"},
	{Family: "memory", Leaf: "provenance"},
	{Family: "memory", Leaf: "search"},
	{Family: "memory", Leaf: "store"},
	{Family: "memory", Leaf: "sync"},
	{Family: "memory", Leaf: "update"},
	// web — the local browser chat UI and its two web tools (`gopi web -h`).
	{Family: Web, Leaf: WebFetch},
	{Family: Web, Leaf: WebSearch},
	// tool — the local tools under one noun (`gopi tool -h`).
	{Family: Tool, Leaf: Act},
	{Family: Tool, Leaf: "annotate"},
	{Family: Tool, Leaf: Capture},
	{Family: Tool, Leaf: CodeSearch},
	{Family: Tool, Leaf: "gif"},
	{Family: Tool, Leaf: "keyword_search"},
	{Family: Tool, Leaf: LedgerWindow},
	{Family: Tool, Leaf: "proxy"},
	{Family: Tool, Leaf: Sandbox},
	{Family: Tool, Leaf: ScratchGC},
	{Family: Tool, Leaf: ScreenCapture},
	{Family: Tool, Leaf: "shot"},
	{Family: Tool, Leaf: "text2img"},
	{Family: Tool, Leaf: Text2imgVerify},
	{Family: Tool, Leaf: "tokens"},
	{Family: Tool, Leaf: "vision_analyze"},
	{Family: Tool, Leaf: "voice_input"},
	{Family: Tool, Leaf: Web},
}

// FamilyLeafNotes is the per-family hint the refusal appends, so a reader who
// never knew the leaf existed learns what the family is for. Exactly ONE entry
// per family present in FamilyLeaves (a test asserts the two agree: a family
// with no note would print a pointer with no purpose, and a note with no family
// would be dead text).
var FamilyLeafNotes = map[string]string{
	Task:     "the task-as-memory ledger",
	Session:  "the session family — list/show plus the log readers",
	"memory": "the knowledge base",
	Web:      "the local browser chat UI and the web tools",
	Tool:     "every local tool under one noun",
}

// FamilyLeafWord is the ONE lookup the run-path guard makes: it answers the
// (family, leaf) row for a word, or nil when the word is no leaf of any
// covered family.
//
// FIRST-ROW-WINS, and that is a decision rather than an artifact. Several leaves
// belong to more than one family (`list` is a leaf of `task`, `session` AND
// `memory`; `show` of `task` and `session`), and a refusal can print ONE
// address. The table's own order is therefore the preference order, and the
// ledger family is listed first because it is the one a bare `gopi list` most
// often means on this host — a claim the drift gate keeps honest by asserting
// the lookup agrees with the table's first row for every duplicated word.
//
// It is a linear scan on purpose: the table is a few dozen rows, the caller
// runs it at most once per process (on the bare-prompt face only), and a
// pre-built index would be a second definition of the same set (Rule 13).
func FamilyLeafWord(word string) *FamilyLeaf {
	if word == "" {
		return nil
	}
	for i := range FamilyLeaves {
		if FamilyLeaves[i].Leaf == word {
			return &FamilyLeaves[i]
		}
	}
	return nil
}

// FamilyLeafReachableFloor is the MEASURED floor on how many distinct leaf words
// can actually reach the run path — the dispatch chain runs FIRST, so a leaf
// word that also names a top-level command (`web`, `proxy`, `annotate`, …) is
// resolved long before any positional guard sees it.
//
// It is a floor, not a count, and it is named because the drift gate's whole
// non-vacuity rests on it (Rule 1): a table that had drifted entirely into
// dispatch-resolved words would refuse nothing in the field while every
// both-directions arm stayed green. Measured on this tree 2026-11-17 by
// cmd/agent's TestLeafVerbReachableRowsAreNonTrivial, which PRINTS the two sets
// and fails below this number.
const FamilyLeafReachableFloor = 20

// Families returns the distinct family spellings the table covers, in first-seen
// order — the list the drift gate walks.
func Families() []string {
	seen := make(map[string]bool, len(FamilyLeafNotes))
	out := make([]string, 0, len(FamilyLeafNotes))
	for i := range FamilyLeaves {
		f := FamilyLeaves[i].Family
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}
