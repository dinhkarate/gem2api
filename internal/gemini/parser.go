package gemini

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"
)

var lengthMarkerRe = regexp.MustCompile(`(\d+)\n`)

// ParseAllFrames reads all frames from the Gemini web response body.
// Implements Google's length-prefixed framing protocol where the length marker
// is counted in UTF-16 code units (matching JavaScript's String.length).
func ParseAllFrames(body io.Reader) ([]ParsedFrame, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	content := string(data)

	// Strip anti-hijacking prefix ")]}'  "
	if idx := strings.Index(content, "\n"); idx != -1 {
		prefix := strings.TrimSpace(content[:idx])
		if strings.HasPrefix(prefix, ")]}") {
			content = content[idx+1:]
		}
	}

	frames, _ := parseFrames(content)
	return frames, nil
}

// parseFrames implements the core framing protocol parser.
// Each frame format: [length]\n[json_payload]\n
// Length includes the \n after digits and the \n after payload (counted in UTF-16 code units).
func parseFrames(content string) ([]ParsedFrame, string) {
	var frames []ParsedFrame
	consumedPos := 0
	totalLen := len(content)

	for consumedPos < totalLen {
		// Skip whitespace
		for consumedPos < totalLen && isWhitespace(content[consumedPos]) {
			consumedPos++
		}
		if consumedPos >= totalLen {
			break
		}

		// Match length marker: digits followed by newline
		match := lengthMarkerRe.FindStringIndex(content[consumedPos:])
		if match == nil {
			break
		}

		submatch := lengthMarkerRe.FindStringSubmatch(content[consumedPos:])
		if submatch == nil {
			break
		}

		lengthVal := submatch[1]
		length := 0
		for _, c := range lengthVal {
			length = length*10 + int(c-'0')
		}

		// Content starts right after the digits (at the \n position)
		startContent := consumedPos + match[0] + len(lengthVal)

		// Count UTF-16 code units to find end position
		charCount, unitsFound := getCharCountForUTF16Units(content, startContent, length)
		if unitsFound < length {
			// Incomplete frame
			break
		}

		endPos := startContent + charCount
		chunk := strings.TrimSpace(content[startContent:endPos])
		consumedPos = endPos

		if chunk == "" {
			continue
		}

		// Parse the JSON envelope
		var parsed []json.RawMessage
		if err := json.Unmarshal([]byte(chunk), &parsed); err != nil {
			continue
		}

		// Each parsed entry is an envelope like ["wrb.fr", null, "<content>"]
		for _, entry := range parsed {
			frame := extractFrame(entry)
			if frame != nil {
				frames = append(frames, *frame)
			}
		}
	}

	return frames, content[consumedPos:]
}

// getCharCountForUTF16Units converts UTF-16 code unit count to Go string character count.
// Google's API uses JavaScript's String.length (UTF-16 code units) for length markers.
func getCharCountForUTF16Units(s string, startIdx int, utf16Units int) (charCount int, unitsFound int) {
	runes := []rune(s[startIdx:])
	count := 0
	units := 0

	for i := 0; i < len(runes) && units < utf16Units; i++ {
		r := runes[i]
		// Characters above U+FFFF need surrogate pairs (2 UTF-16 code units)
		u := 1
		if r > 0xFFFF {
			u = 2
		}
		if units+u > utf16Units {
			break
		}
		units += u
		count += len(string(r)) // byte length of this rune in UTF-8
	}

	return count, units
}

// extractFrame extracts a ParsedFrame from a single envelope entry.
func extractFrame(entry json.RawMessage) *ParsedFrame {
	var items []json.RawMessage
	if err := json.Unmarshal(entry, &items); err != nil {
		return nil
	}

	if len(items) < 3 {
		return nil
	}

	// Check tag is "wrb.fr"
	var tag string
	if err := json.Unmarshal(items[0], &tag); err != nil || tag != "wrb.fr" {
		return nil
	}

	// Index 2 is the double-encoded content string
	var contentStr string
	if err := json.Unmarshal(items[2], &contentStr); err != nil {
		return nil
	}

	frame, _ := parseContentJSON(contentStr)
	return frame
}

// parseContentJSON extracts text, metadata, and status from the inner JSON.
func parseContentJSON(contentStr string) (*ParsedFrame, error) {
	var content []json.RawMessage
	if err := json.Unmarshal([]byte(contentStr), &content); err != nil {
		return nil, fmt.Errorf("parse inner content: %w", err)
	}

	frame := &ParsedFrame{}

	// Conversation metadata at content[1]: [cid, rid] or [cid, rid, rcid, ...]
	if len(content) > 1 && string(content[1]) != "null" {
		var meta []json.RawMessage
		if err := json.Unmarshal(content[1], &meta); err == nil {
			if len(meta) > 0 {
				json.Unmarshal(meta[0], &frame.ConversationID)
			}
			if len(meta) > 1 {
				json.Unmarshal(meta[1], &frame.ResponseID)
			}
			if len(meta) > 2 {
				json.Unmarshal(meta[2], &frame.ChoiceID)
			}
		}
	}

	// Response text at content[4][0][1][0]
	if len(content) > 4 && string(content[4]) != "null" {
		var candidates []json.RawMessage
		if err := json.Unmarshal(content[4], &candidates); err == nil && len(candidates) > 0 {
			var candidate []json.RawMessage
			if err := json.Unmarshal(candidates[0], &candidate); err == nil {
				// Text at [1][0]
				if len(candidate) > 1 && string(candidate[1]) != "null" {
					var textParts []json.RawMessage
					if err := json.Unmarshal(candidate[1], &textParts); err == nil && len(textParts) > 0 {
						json.Unmarshal(textParts[0], &frame.Text)
					}
				}

				// Finality: [2] non-null = final chunk
				if len(candidate) > 2 && string(candidate[2]) != "null" {
					frame.IsFinal = true
				}

				// Status code at [8][0] (2 = final)
				if len(candidate) > 8 && string(candidate[8]) != "null" {
					var statusArr []json.RawMessage
					if err := json.Unmarshal(candidate[8], &statusArr); err == nil && len(statusArr) > 0 {
						var code int
						if json.Unmarshal(statusArr[0], &code) == nil && code == 2 {
							frame.IsFinal = true
						}
					}
				}

				// Thoughts at [37][0][0] (thinking models)
				if len(candidate) > 37 && string(candidate[37]) != "null" {
					var thoughts []json.RawMessage
					if err := json.Unmarshal(candidate[37], &thoughts); err == nil && len(thoughts) > 0 {
						var innerThoughts []json.RawMessage
						if err := json.Unmarshal(thoughts[0], &innerThoughts); err == nil && len(innerThoughts) > 0 {
							json.Unmarshal(innerThoughts[0], &frame.Thoughts)
						}
					}
				}
			}
		}
	}

	// Context string at content[25] = stream complete
	if len(content) > 25 && string(content[25]) != "null" {
		frame.IsFinal = true
	}

	return frame, nil
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// StreamParser reads frames one at a time for streaming responses.
// For streaming, we accumulate the buffer and parse incrementally.
type StreamParser struct {
	body   io.Reader
	buffer string
	done   bool
}

// NewStreamParser creates a parser that reads frames incrementally.
func NewStreamParser(body io.Reader) *StreamParser {
	return &StreamParser{body: body}
}

// Next reads and returns the next content frame. Returns io.EOF when done.
func (sp *StreamParser) Next() (*ParsedFrame, error) {
	for {
		// Try parsing existing buffer first
		if sp.buffer != "" {
			frames, remainder := parseFrames(sp.buffer)
			sp.buffer = remainder
			for _, f := range frames {
				if f.Text != "" || f.IsFinal {
					fc := f
					return &fc, nil
				}
			}
		}

		if sp.done {
			return nil, io.EOF
		}

		// Read more data
		buf := make([]byte, 8192)
		n, err := sp.body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			// Strip anti-hijacking prefix on first read
			if strings.Contains(chunk, ")]}'") {
				if idx := strings.Index(chunk, "\n"); idx != -1 {
					prefix := strings.TrimSpace(chunk[:idx])
					if strings.HasPrefix(prefix, ")]}") {
						chunk = chunk[idx+1:]
					}
				}
			}
			sp.buffer += chunk
		}
		if err != nil {
			sp.done = true
			if sp.buffer == "" {
				return nil, io.EOF
			}
			// Process remaining buffer
		}
	}
}

// Helper for UTF-16 length — exported for testing
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}
