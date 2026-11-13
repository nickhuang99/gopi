package config

import (
	"os"

	"github.com/nick/gopi/internal/boolvocab"
)

// resume_session.go — the ONE explicit opt-in that lets a resume MINT A NEW
// SESSION instead of continuing an existing one.
//
// WHY THIS IS AN ENVIRONMENT VARIABLE AND NOT A FLAG, by the same argument its
// sibling ResumeAllowMissingGround carries: the resume face has two addresses
// (`gopi session resume <target>` and `gopi --session <id> ["<prompt>"]`) plus
// the chat face's `--resume`, and one of them (the bare `gopi --session`) has no
// flag slot at all — its argv tail is the prompt. A single reader below reaches
// every face, so the switch cannot be honoured on one surface and silently
// ignored on another.
//
// WHY IT IS *NOT* IN THE settings.go CATALOG. The catalog is the operator's
// supported knob table (`gopi --help-env`). This one re-arms the precise
// degradation the refusal exists to prevent: a resume that resolves a
// CHECKPOINT but no session RECORD used to mint a brand-new sessions/<uuid>/
// directory and report nothing, so the operator believed an old session grew
// while it sat untouched (measured 2026-10-28 on an isolated profile: `gopi chat
// --resume <run-dir>/<id>.raw.log` with the JSONL deleted exited 0 and created
// sessions/a49c1546-…/). Advertising it beside ordinary knobs would invite its
// use; naming it in the refusal's own repair line — where an operator meets it —
// does not.
//
// It is still a REAL variable with a real reader (the Rule 21 shape): the gate
// that requires every env-name constant to be consumed is satisfied by
// ResumeAllowNewSession below, and the refusal's repair line spells it.
const ResumeAllowNewSessionEnv = "GOPI_RESUME_ALLOW_NEW_SESSION"

// ResumeAllowNewSession reports whether the operator EXPLICITLY asked a resume
// that cannot continue an existing session to start a fresh one instead.
//
// Anything other than a truthy token (boolvocab: 1/true/yes/on) is "not asked",
// which is the only direction a typo may fail in: a mistyped value must never
// re-arm the silent mint, because that failure is invisible — the run succeeds,
// prints no error, and the work lands in a directory nobody asked for.
func ResumeAllowNewSession() bool {
	return boolvocab.IsTrue(os.Getenv(ResumeAllowNewSessionEnv))
}
