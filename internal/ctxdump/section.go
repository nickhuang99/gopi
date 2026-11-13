// section.go — the ONE declaration of this package's EXPORT SECTIONS: the full
// page, and the two derived views an operator asks for when the full one is
// 70k+ tokens they do not need to read.
//
// WHY A DERIVED VIEW EXISTS AT ALL, AND WHY IT IS NOT A MODE. The operator's
// ruling of 2026-11-09 (render.go states it) is that a debug export is VERBATIM
// and has no `--full` flag: the page IS the request's bytes, so it can be diffed
// against the raw HTTP log. A section export does not weaken that, because it
// does not produce a smaller version of THE SAME ARTIFACT — it produces a
// DIFFERENT artifact under a DIFFERENT NAME:
//
//	context-turn<N>.md           the whole request        (the evidence; unchanged)
//	context-turn<N>-system.md    ## Request + ## System prompt + ## Tools
//	context-turn<N>-messages.md  ## Request + ## Messages
//
// The three files can sit in one directory without any of them claiming to be
// another: a reader (or a diff) knows which one is the request by its name, and
// the filtered page states on its own first line that it is not the full body.
// That naming rule is why the file names live HERE and not in the CLI: a name is
// a claim about the bytes beside it, and both carriers (the `gopi session
// context` verb and the `/context` slash command) must make the same claim.
//
// THE ADDRESS TABLE STAYS IN BOTH FILTERED VIEWS. `## Request` is the only
// place an artifact says WHICH request it holds — and a filtered page's name
// carries the request's address only when the caller let this package derive it
// (`--out PATH` replaces the name with the caller's). Dropping the table would
// leave a page of prose with nothing to attach it to a turn.

package ctxdump

import (
	"fmt"
	"path/filepath"
)

// Section is which part of a request an export writes.
//
// The ZERO VALUE IS THE FULL PAGE, deliberately: a caller that has no opinion
// (and every caller written before sections existed) exports the whole request,
// so the ordinary path cannot accidentally acquire a filter.
type Section string

const (
	// SectionAll is the whole request — the verbatim evidence page whose name
	// and bytes are fixed (FileNamePattern).
	SectionAll Section = ""
	// SectionSystem is the model's FIXED PREFIX: the address table, the system
	// prompt blocks as sent, and the tool definitions. It is the half that stays
	// identical across a session's turns.
	SectionSystem Section = "system"
	// SectionUser is the CONVERSATION: the address table and the message stream,
	// with every block (text, thinking, tool calls, results, images) rendered
	// exactly as the full page renders them.
	SectionUser Section = "user"
)

// The selectors' SPELLINGS. They are declared here — not in either carrier —
// because the conflict sentence below has to name the two words an operator
// typed, and two spellings of one flag is the drift Rule 14 forbids.
const (
	// SystemSelector is the flag that asks for SectionSystem.
	SystemSelector = "--system"
	// UserSelector is the flag that asks for SectionUser.
	UserSelector = "--user"
)

// The section artifacts' names. Each is derived from the same ordinal the full
// page uses, with a SUFFIX that states the view — so the three files of one turn
// sort together and none of them can be mistaken for another.
const (
	// SystemFileNameFormat is a TURN-scoped system-only artifact's name.
	SystemFileNameFormat = "context-turn%d-system.md"
	// UserFileNameFormat is a TURN-scoped messages-only artifact's name.
	UserFileNameFormat = "context-turn%d-messages.md"
	// RoundSystemFileNameFormat is a ROUND-scoped system-only artifact's name.
	RoundSystemFileNameFormat = "context-round%d-system.md"
	// RoundUserFileNameFormat is a ROUND-scoped messages-only artifact's name.
	RoundUserFileNameFormat = "context-round%d-messages.md"
	// SystemFileNamePattern / UserFileNamePattern / RoundSystemFileNamePattern /
	// RoundUserFileNamePattern are the same names as PROSE spells them, for a
	// usage page that must not print a concrete number it is not writing.
	SystemFileNamePattern      = "context-turn<N>-system.md"
	UserFileNamePattern        = "context-turn<N>-messages.md"
	RoundSystemFileNamePattern = "context-round<N>-system.md"
	RoundUserFileNamePattern   = "context-round<N>-messages.md"
)

// The screen line's section clause, and the page's own statement of itself.
const (
	// SectionLineFormat is what a FILTERED export appends to the one screen
	// line. The full export appends NOTHING (SectionAllWords), so the ordinary
	// line keeps the exact shape it has always had.
	SectionLineFormat = " — section: %s"
	// SectionSystemWords names what a system-only export contains.
	SectionSystemWords = "system only (## Request + ## System prompt + ## Tools, no messages)"
	// SectionUserWords names what a messages-only export contains.
	SectionUserWords = "messages only (## Request + ## Messages, no system prompt, no tools)"
	// PageTitleSectionSuffixFormat is appended BELOW a FILTERED page's H1, so the
	// file states in its first paragraph that it is a view and not the request.
	PageTitleSectionSuffixFormat = "(section export: %s only — NOT the request's full body)\n\n"
)

// SectionConflictReason is the ONE sentence both carriers print when an operator
// asked for both views. They are not combinable: `--system --user` would be the
// full page under a filtered name, which is exactly the confusion the two names
// exist to prevent. Refusing is the only honest answer (Rule 15: never pick one
// silently).
const SectionConflictReason = "both " + SystemSelector + " and " + UserSelector +
	" were given — a request has one system prompt and one message list, so an export shows ONE side; pick one"

// SectionFor resolves the two selector booleans into the one section to export.
//
// It is a function rather than two branches in each carrier so the conflict is
// refused by ONE sentence at ONE place: the CLI parser and the slash command's
// parser both call it, and neither can forget the check.
func SectionFor(system, user bool) (Section, error) {
	switch {
	case system && user:
		return SectionAll, fmt.Errorf("%s", SectionConflictReason)
	case system:
		return SectionSystem, nil
	case user:
		return SectionUser, nil
	default:
		return SectionAll, nil
	}
}

// Valid reports whether s names a section this package can render.
func (s Section) Valid() bool {
	switch s {
	case SectionAll, SectionSystem, SectionUser:
		return true
	}
	return false
}

// IsFull reports whether s is the whole request (the verbatim evidence page).
func (s Section) IsFull() bool { return s == SectionAll }

// Name is the section's own word, for a diagnostic or a title.
func (s Section) Name() string {
	if s == SectionAll {
		return "full"
	}
	return string(s)
}

// LineWords is the clause a filtered export appends to the screen line; the full
// export appends nothing at all.
func (s Section) LineWords() string {
	switch s {
	case SectionSystem:
		return fmt.Sprintf(SectionLineFormat, SectionSystemWords)
	case SectionUser:
		return fmt.Sprintf(SectionLineFormat, SectionUserWords)
	default:
		return ""
	}
}

// WantsSystem, WantsTools and WantsMessages are the three questions the renderer
// asks. They are methods on the type rather than `s != SectionUser` tests at each
// site, so the answer is stated once.
func (s Section) WantsSystem() bool   { return s == SectionAll || s == SectionSystem }
func (s Section) WantsTools() bool    { return s == SectionAll || s == SectionSystem }
func (s Section) WantsMessages() bool { return s == SectionAll || s == SectionUser }

// FileName is the artifact's name for this section at that address.
//
// THE FULL PAGE'S NAME IS UNCHANGED — it delegates to Address.FileName(), the
// constant the "diffable against the raw log" promise has always meant — and the
// filtered views append their own suffix, so no filtered file can overwrite the
// evidence or be mistaken for it.
func (s Section) FileName(a Address) string {
	round := a.Axis == AxisRound
	switch s {
	case SectionSystem:
		if round {
			return fmt.Sprintf(RoundSystemFileNameFormat, a.Round)
		}
		return fmt.Sprintf(SystemFileNameFormat, a.Turn)
	case SectionUser:
		if round {
			return fmt.Sprintf(RoundUserFileNameFormat, a.Round)
		}
		return fmt.Sprintf(UserFileNameFormat, a.Turn)
	}
	return a.FileName()
}

// Path is s.FileName(a) under dir.
func (s Section) Path(dir string, a Address) string {
	return filepath.Join(dir, s.FileName(a))
}
