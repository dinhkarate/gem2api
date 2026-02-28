package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"gem2api/internal/openai"
)

// BuildPrompt converts OpenAI messages into a single prompt string for Gemini web.
// Since the web API only accepts one message per request, we concatenate all messages.
func BuildPrompt(messages []openai.Message) string {
	if len(messages) == 0 {
		return ""
	}

	// Single user message: return it directly
	if len(messages) == 1 && messages[0].Role == "user" {
		return extractText(messages[0].Content)
	}

	var systemMsg string
	var parts []string

	for _, msg := range messages {
		text := extractText(msg.Content)
		if text == "" {
			continue
		}

		switch msg.Role {
		case "system":
			systemMsg = text
		case "user":
			parts = append(parts, fmt.Sprintf("User: %s", text))
		case "assistant":
			parts = append(parts, fmt.Sprintf("Assistant: %s", text))
		}
	}

	var result strings.Builder

	if systemMsg != "" {
		result.WriteString(fmt.Sprintf("[System Instructions: %s]\n\n", systemMsg))
	}

	if len(parts) > 1 {
		result.WriteString(strings.Join(parts, "\n\n"))
	} else if len(parts) == 1 {
		// Single user message after system prompt — strip the "User: " prefix
		result.WriteString(strings.TrimPrefix(parts[0], "User: "))
	}

	return result.String()
}

// extractText gets the text content from a Message's Content field.
// Content can be a string or []ContentPart.
func extractText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, part := range v {
			if m, ok := part.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		if b, err := json.Marshal(content); err == nil {
			var s string
			if json.Unmarshal(b, &s) == nil {
				return s
			}
		}
		return fmt.Sprintf("%v", content)
	}
}
