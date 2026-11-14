package main

// chat_replay_memory_evidence.go — the VERB-LEVEL half of the replay memory
// gate's announcement (epic: `gopi chat --replay` must not print memory refusals
// into the band that draws the recording).
//
// WHY THIS FILE EXISTS AT ALL. The gate itself is internal/agent's
// (memory_replay_guard.go): one decision (replaymode.Active), one sentence
// vocabulary (replaymode.Refuse), one writer for the evidence file. What lives
// here is the single spelling the cmd/agent call sites share, so three files do
// not each invent their own destination — and so a future memory refusal written
// in this package has an obvious place to call.
//
// THE RULE IT ENCODES. On a replay the refusal is RECORDED and not DRAWN:
// agent.RecordReplayMemoryRefusal appends the sentence (with the instant) to the
// profile's logs/replay-memory-refusals.log, and the caller prints NOTHING. That
// is Rule 15 satisfied by a durable record rather than by a chat message — the
// screen of a replay is somebody else's conversation, and a `[gopi replay]
// refused to …` row inside it reads as though THIS run had tried to write.
//
// A failure to write the record is the one case that speaks: it goes to stderr,
// which is not the answer band, because losing the refusal silently would be
// worse than the sentence the epic exists to keep off the screen.

import (
	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/replaymode"
)

// announceReplayMemoryRefusal records one memory-write refusal for `what`,
// building the sentence through replaymode.Refuse so the vocabulary cannot
// drift from the other sites (Rule 14).
func announceReplayMemoryRefusal(what string) {
	sentence := replaymode.Refuse(what)
	if err := agent.RecordReplayMemoryRefusal(sentence); err != nil {
		replaymode.Noticef("%s (the refusal record could not be written: %v)\n", sentence, err)
	}
}
