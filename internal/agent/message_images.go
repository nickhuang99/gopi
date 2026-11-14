package agent

import "github.com/nickhuang99/gopi/internal/deepseek"

// derefImages converts the image pointers a caller collected (positional image
// arguments, each validated before it reaches an Agent method) into the value
// slice a user message carries.
//
// It exists because the message field is `Images []ClaudeImageSource` — ONE
// user message carrying N image blocks — while every entry point (runEngine,
// Agent.Chat, Agent.ChatOps, `gopi web`) still receives images as
// `[]*deepseek.ClaudeImageSource`. A nil entry is skipped rather than
// dereferenced: an absent image is an absent block, and the callers' own
// validation already refuses to build one.
func derefImages(images []*deepseek.ClaudeImageSource) []deepseek.ClaudeImageSource {
	if len(images) == 0 {
		return nil
	}
	out := make([]deepseek.ClaudeImageSource, 0, len(images))
	for _, img := range images {
		if img == nil {
			continue
		}
		out = append(out, *img)
	}
	return out
}
