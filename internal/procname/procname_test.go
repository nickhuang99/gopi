package procname

// The /proc/<pid>/stat PARSE is the one thing this package must get right for
// every liveness consumer in the repo: internal/scratch's holder registration,
// internal/tools' task-row identity, the merger's takeover probe and the shell
// launcher all read the same column (starttime, field 22). Two of those sites
// used to carry their OWN copy of the parse, which is why this file pins the
// trap here — at the single definition they now delegate to (Rules 13/14).
//
// The trap: field 2 (comm) is parenthesised and may ITSELF contain spaces and
// parentheses (`(bash (deleted))`, `gopi worker`), so a whitespace split of the
// whole line shifts every later column by however many spaces the name held —
// and returns a neighbouring NUMBER instead of an error. That is the worst
// shape a liveness judgement can get: the wrong incarnation, confidently.

import (
	"fmt"
	"strings"
	"testing"
)

const (
	// trapStartTicks is the starttime written into every fixture below. It is
	// distinguishable from the padding values on purpose: a parser that reads
	// the wrong column returns one of THOSE and the assertion names it.
	trapStartTicks = "987654321"
	// trapPid / trapParent are the fixture's pid and ppid columns (fields 1/4).
	trapPid    = "4242"
	trapParent = "31337"
	// paddingBase seeds the filler fields 5..21 so every column is unique
	// (a fixture of repeated zeros cannot tell a shifted read from a correct one).
	paddingBase = 70000
)

// statLine renders "<pid> (<comm>) <state> … " with fields 3..22 present.
//
// Field numbering is the kernel's: 1 pid, 2 the parenthesised comm, 3 the state
// letter. The tokens AFTER comm are therefore field 3 onward — 20 of them
// through starttime (field 22), which is exactly what StatField counts.
func statLine(comm, startTicks string) string {
	tokens := []string{trapPid, "(" + comm + ")", "S", trapParent}
	for i := 0; i < StatFieldStartTime-5; i++ { // fields 5 .. 21
		tokens = append(tokens, fmt.Sprintf("%d", paddingBase+i))
	}
	tokens = append(tokens, startTicks)
	return strings.Join(tokens, " ") + "\n"
}

// TestStatFieldTrapCommShapes pins the parse against the comm shapes the kernel
// really produces, through the SAME field number every caller passes.
func TestStatFieldTrapCommShapes(t *testing.T) {
	cases := []struct {
		comm    string
		comment string
	}{
		{"bash (deleted)", "THE trap shape: spaces AND parentheses in comm"},
		{"gopi worker", "this repo's own worker name — a space, no parens"},
		{"(bash)", "a substituted shell: the name IS parenthesised"},
		{"a ((((b", "lopsided brackets (the LAST ')' still terminates comm)"},
		{"cuda-gdb", "an ordinary name (the control: no space, no paren)"},
	}
	for _, tc := range cases {
		t.Run(tc.comm, func(t *testing.T) {
			line := statLine(tc.comm, trapStartTicks)

			got, ok := StatField(line, StatFieldStartTime)
			if !ok {
				t.Fatalf("%s: StatField(field %d) answered unusable on a well-formed line %q",
					tc.comment, StatFieldStartTime, line)
			}
			if got != trapStartTicks {
				t.Fatalf("stat comm %q: starttime = %q, want %q — the parse read a neighbouring column, "+
					"which is the shift a whitespace split produces", tc.comm, got, trapStartTicks)
			}

			// The same line through the value-shaped sibling and the
			// neighbouring columns (a correct parser reads all three).
			if ticks, err := StatStartTime(line); err != nil || fmt.Sprint(ticks) != trapStartTicks {
				t.Fatalf("stat comm %q: StatStartTime = (%d, %v), want (%s, nil)", tc.comm, ticks, err, trapStartTicks)
			}
			if state, ok := StatField(line, StatFieldState); !ok || state != "S" {
				t.Fatalf("stat comm %q: state = (%q, %v), want (\"S\", true)", tc.comm, state, ok)
			}
			if parent, ok := StatField(line, StatFieldParent); !ok || parent != trapParent {
				t.Fatalf("stat comm %q: parent = (%q, %v), want (%q, true)", tc.comm, parent, ok, trapParent)
			}
		})
	}
}

// TestStatFieldRefusesWhatItCannotAnswer pins the failure shape callers rely on:
// "cannot answer" is (false)/an error, NEVER a fabricated value. Both live
// callers turn that into (0,false) or propagate the error, so a parser that
// invented a 0 here would be indistinguishable from a real tick count.
func TestStatFieldRefusesWhatItCannotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"no comm field at all", "4242 R 1 2 3"},
		{"empty line", ""},
		{"line ends at comm", "4242 (bash) "},
		{"line too short for field 22", "4242 (bash) S 31337 1 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if v, ok := StatField(tc.line, StatFieldStartTime); ok {
				t.Fatalf("StatField(%q) = (%q, true) — a line that cannot answer must not produce a value", tc.line, v)
			}
			if v, err := StatStartTime(tc.line); err == nil {
				t.Fatalf("StatStartTime(%q) = (%d, nil) — want an error, never a silent zero", tc.line, v)
			}
		})
	}

	// A non-numeric column is parseable-as-a-field and still not a tick count.
	if v, err := StatStartTime(statLine("bash (deleted)", "not-a-number")); err == nil {
		t.Fatalf("a non-numeric starttime column answered (%d, nil) — want an error", v)
	}
	// Below the first addressable field the request is meaningless, not empty.
	if v, ok := StatField(statLine("bash", trapStartTicks), StatFieldState-1); ok {
		t.Fatalf("a field below the state column answered (%q, true) — the post-comm list cannot address it", v)
	}
}

// TestNaiveWhitespaceSplitMisreadsTheTrapLine is the RED PROOF, kept as a test
// so the defect cannot be "simplified" back in: it replays the form every
// hand-rolled copy of this parse reaches for — strings.Fields over the WHOLE
// line, indexed by field number − 1 — and shows it returns a WRONG value on the
// trap line (not an error, which is what makes it dangerous).
//
// This is why the repo has ONE parse (Rules 13/14) and why the three former
// copies had to delegate: the naive body is not a shorter way to the same
// answer, it is a different answer for exactly the processes whose name is
// interesting.
func TestNaiveWhitespaceSplitMisreadsTheTrapLine(t *testing.T) {
	naive := func(line string, field int) (string, bool) {
		fields := strings.Fields(line)
		idx := field - 1 // the naive kernel-numbering assumption, comm counted as ONE token
		if idx >= len(fields) {
			return "", false
		}
		return fields[idx], true
	}

	for _, comm := range []string{"bash (deleted)", "gopi worker"} {
		line := statLine(comm, trapStartTicks)
		got, ok := naive(line, StatFieldStartTime)
		if ok && got == trapStartTicks {
			t.Fatalf("the naive whitespace split agreed with the real column for comm %q — this red proof "+
				"no longer proves anything; the fixture must keep a trap comm", comm)
		}
		t.Logf("comm %q: naive split reads %q (ok=%v) where the line's field %d is %q — "+
			"a wrong incarnation, silently, which is the defect the delegated parse removes",
			comm, got, ok, StatFieldStartTime, trapStartTicks)
	}

	// And the definitive form of the trap: the naive read is a NUMBER, so no
	// caller can notice. A parse that fails loudly is merely unusable; one that
	// invents a plausible tick count is a liveness lie.
	if _, err := StatStartTime(statLine("bash (deleted)", trapStartTicks)); err != nil {
		t.Fatalf("the real parse must ACCEPT the trap line: %v", err)
	}
}
