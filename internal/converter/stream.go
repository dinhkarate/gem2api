package converter

import (
	"time"

	"gem2api/internal/gemini"
	"gem2api/internal/openai"
)

// StreamState tracks state between streaming frames for delta computation.
type StreamState struct {
	PrevText     string
	PrevThoughts string
	RequestID    string
	Model        string
}

// NewStreamState creates initial streaming state.
func NewStreamState(model, requestID string) *StreamState {
	return &StreamState{
		Model:     model,
		RequestID: requestID,
	}
}

// FrameToChunk converts a parsed frame to an SSE chunk, computing the delta
// from the previous snapshot. Returns nil if there's no new content.
func (ss *StreamState) FrameToChunk(frame *gemini.ParsedFrame) *openai.ChatCompletionChunk {
	// Compute text delta (snapshot streaming: each frame has full text)
	delta := ""
	if len(frame.Text) > len(ss.PrevText) {
		delta = frame.Text[len(ss.PrevText):]
		ss.PrevText = frame.Text
	}

	// Compute thoughts delta
	thoughtsDelta := ""
	if len(frame.Thoughts) > len(ss.PrevThoughts) {
		thoughtsDelta = frame.Thoughts[len(ss.PrevThoughts):]
		ss.PrevThoughts = frame.Thoughts
	}

	content := thoughtsDelta + delta
	if content == "" && !frame.IsFinal {
		return nil // No new content yet
	}

	chunk := &openai.ChatCompletionChunk{
		ID:      ss.RequestID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   ss.Model,
		Choices: []openai.Choice{
			{
				Index: 0,
			},
		},
	}

	if frame.IsFinal {
		finishReason := "stop"
		chunk.Choices[0].FinishReason = &finishReason
		chunk.Choices[0].Delta = &openai.Message{}
	} else if content != "" {
		chunk.Choices[0].Delta = &openai.Message{
			Content: content,
		}
	}

	return chunk
}
