package hippocampus

// Decision is the Pro-style decision step's output: EXACTLY ONE verb plus the
// fields that verb needs. One struct (not a union) keeps the JSON contract
// flat — the fields a verb does not use stay zero and are ignored.
type Decision struct {
	// Verb is the chosen action: one of VerbNone / VerbStore / VerbFetch /
	// VerbUpdate / VerbFeedback. An illegal verb is a recorded failure, never
	// a silent no-op.
	Verb string `json:"verb"`
	// Query is an optional refined retrieval query the decision may carry
	// (recorded in the action for audit).
	Query string `json:"query,omitempty"`
	// Store fields.
	Topic    string   `json:"topic,omitempty"`
	Question string   `json:"question,omitempty"`
	Answer   string   `json:"answer,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	// Fetch / Update / Feedback target row.
	ID int `json:"id,omitempty"`
	// Update fields.
	Tier       string   `json:"tier,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	// Feedback fields.
	Rating string `json:"rating,omitempty"`
	// Reason is the human-auditable justification (recorded for every verb).
	Reason string `json:"reason,omitempty"`
}
