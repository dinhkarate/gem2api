package gemini

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseAllFrames_RealFormat(t *testing.T) {
	// Simulate the response format from gemini.google.com
	// Inner structure: [null, [cid, rid], null, null, [[rcid, ["text"], null, ...]]]
	innerContent := `[null,["c_abc123","r_def456"],null,null,[["rc_ghi789",["Hello! How are you doing today?"],null,null,null,null,null,null,[2]]]]`

	// Build the outer frame: [["wrb.fr", null, "<innerContent>"]]
	outerFrame := `[["wrb.fr",null,"` + jsonEscapeString(innerContent) + `"]]`

	// Google's format: )]}'  \n + length-prefixed frames
	// Length is in UTF-16 code units and includes the \n after digits + content + trailing \n
	frameWithNewlines := "\n" + outerFrame + "\n"
	utf16Len := UTF16Len(frameWithNewlines)
	response := ")]}'\n" + fmt.Sprintf("%d", utf16Len) + frameWithNewlines

	frames, err := ParseAllFrames(strings.NewReader(response))
	if err != nil {
		t.Fatalf("ParseAllFrames error: %v", err)
	}

	t.Logf("Parsed %d frames", len(frames))

	if len(frames) == 0 {
		t.Fatal("Expected at least 1 frame, got 0")
	}

	f := frames[0]
	t.Logf("Frame: text=%q cid=%q rid=%q final=%v", f.Text, f.ConversationID, f.ResponseID, f.IsFinal)

	if f.Text != "Hello! How are you doing today?" {
		t.Errorf("Expected text %q, got %q", "Hello! How are you doing today?", f.Text)
	}
	if f.ConversationID != "c_abc123" {
		t.Errorf("Expected cid %q, got %q", "c_abc123", f.ConversationID)
	}
	if f.ResponseID != "r_def456" {
		t.Errorf("Expected rid %q, got %q", "r_def456", f.ResponseID)
	}
}

func TestParseAllFrames_MultipleFrames(t *testing.T) {
	// Simulate snapshot streaming: each frame has cumulative text
	frame1Inner := `[null,["c_abc","r_def"],null,null,[["rc_ghi",["Hello"],null,null,null,null,null,null,null]]]`
	frame2Inner := `[null,["c_abc","r_def"],null,null,[["rc_ghi",["Hello! How are you?"],null,null,null,null,null,null,[2]]]]`

	frame1JSON := `[["wrb.fr",null,"` + jsonEscapeString(frame1Inner) + `"]]`
	frame2JSON := `[["wrb.fr",null,"` + jsonEscapeString(frame2Inner) + `"]]`

	// Also include a non-content frame (like "di" frames)
	diFrame := `[["di",123]]`

	response := ")]}'\n" +
		buildFrame(frame1JSON) +
		buildFrame(diFrame) +
		buildFrame(frame2JSON)

	frames, err := ParseAllFrames(strings.NewReader(response))
	if err != nil {
		t.Fatalf("ParseAllFrames error: %v", err)
	}

	t.Logf("Parsed %d frames", len(frames))

	if len(frames) != 2 {
		t.Fatalf("Expected 2 frames, got %d", len(frames))
	}

	if frames[0].Text != "Hello" {
		t.Errorf("Frame 0 text: want %q, got %q", "Hello", frames[0].Text)
	}
	if frames[1].Text != "Hello! How are you?" {
		t.Errorf("Frame 1 text: want %q, got %q", "Hello! How are you?", frames[1].Text)
	}
	if !frames[1].IsFinal {
		t.Error("Frame 1 should be final")
	}
}

func TestParseAllFrames_EmptyPrefix(t *testing.T) {
	frame1Inner := `[null,["c_1","r_2"],null,null,[["rc_3",["Test response"]]]]`
	frame1JSON := `[["wrb.fr",null,"` + jsonEscapeString(frame1Inner) + `"]]`

	response := ")]}'\n" + buildFrame(frame1JSON)

	frames, err := ParseAllFrames(strings.NewReader(response))
	if err != nil {
		t.Fatalf("ParseAllFrames error: %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("Expected 1 frame, got %d", len(frames))
	}
	if frames[0].Text != "Test response" {
		t.Errorf("Text: want %q, got %q", "Test response", frames[0].Text)
	}
}

func TestUTF16LenASCII(t *testing.T) {
	// ASCII: 1 code unit per character
	if n := UTF16Len("hello"); n != 5 {
		t.Errorf("UTF16Len(hello) = %d, want 5", n)
	}
}

func TestUTF16LenEmoji(t *testing.T) {
	// Emoji (U+1F600) needs 2 UTF-16 code units (surrogate pair)
	if n := UTF16Len("😀"); n != 2 {
		t.Errorf("UTF16Len(😀) = %d, want 2", n)
	}
	// "hi😀" = 2 + 2 = 4 units
	if n := UTF16Len("hi😀"); n != 4 {
		t.Errorf("UTF16Len(hi😀) = %d, want 4", n)
	}
}

// helpers

func jsonEscapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func buildFrame(payload string) string {
	withNewlines := "\n" + payload + "\n"
	utf16Len := UTF16Len(withNewlines)
	return fmt.Sprintf("%d", utf16Len) + withNewlines
}
