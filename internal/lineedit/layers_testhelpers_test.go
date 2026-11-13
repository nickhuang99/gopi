package lineedit

import "strings"

// skipRequestRows advances past the turn's REQUEST row(s) when the grid row at
// `from` holds one, and answers the first row of the ANSWER's own body.
//
// The request row is the turn's HEAD, not part of the answer's text: the cell
// grid helpers below reconstruct "the answer as the grid shows it", and the
// operator's own line is not the answer. The shape of the request row is taken
// from the layer table itself (requestRowRows), so this helper never re-derives
// the row's geometry — it asks the same function the renderer asked.
func skipRequestRows(lines []string, from int, prompt string, cols int) int {
	for _, want := range requestRowRows(prompt, cols) {
		if from >= len(lines) {
			return from
		}
		if strings.TrimRight(lines[from], " ") != strings.TrimRight(want, " ") {
			return from
		}
		from++
	}
	return from
}
