package converter

import (
	"fmt"
	"time"

	"gem2api/internal/gemini"
	"gem2api/internal/openai"
)

// FramesToCompletion converts parsed Gemini frames to an OpenAI chat completion response.
func FramesToCompletion(frames []gemini.ParsedFrame, model, requestID string) *openai.ChatCompletionResponse {
	// Use the last frame with text (snapshot streaming = cumulative text)
	var finalText string
	var thoughts string
	for _, f := range frames {
		if f.Text != "" {
			finalText = f.Text
		}
		if f.Thoughts != "" {
			thoughts = f.Thoughts
		}
	}

	// Prepend thoughts for thinking models
	content := finalText
	if thoughts != "" {
		content = fmt.Sprintf("<think>\n%s\n</think>\n\n%s", thoughts, finalText)
	}

	finishReason := "stop"

	return &openai.ChatCompletionResponse{
		ID:      requestID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: &openai.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: &finishReason,
			},
		},
	}
}
