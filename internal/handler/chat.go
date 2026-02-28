package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"gem2api/internal/converter"
	"gem2api/internal/gemini"
	"gem2api/internal/openai"

	"github.com/gin-gonic/gin"
)

// ChatHandler handles /v1/chat/completions requests.
type ChatHandler struct {
	Client *gemini.Client
}

// Handle processes chat completion requests (streaming and non-streaming).
func (h *ChatHandler) Handle(c *gin.Context) {
	var req openai.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, openai.ErrorResponse{
			Error: openai.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, openai.ErrorResponse{
			Error: openai.ErrorDetail{
				Message: "messages is required and must not be empty",
				Type:    "invalid_request_error",
			},
		})
		return
	}

	// Convert messages to single prompt
	prompt := converter.BuildPrompt(req.Messages)
	model := req.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	requestID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

	// Call Gemini web API
	body, err := h.Client.Generate(c.Request.Context(), prompt, model)
	if err != nil {
		log.Printf("Generate error: %v", err)
		c.JSON(http.StatusInternalServerError, openai.ErrorResponse{
			Error: openai.ErrorDetail{
				Message: fmt.Sprintf("Gemini error: %v", err),
				Type:    "server_error",
			},
		})
		return
	}
	defer body.Close()

	if req.Stream {
		h.handleStream(c, body, model, requestID)
	} else {
		h.handleNonStream(c, body, model, requestID)
	}
}

func (h *ChatHandler) handleNonStream(c *gin.Context, body io.ReadCloser, model, requestID string) {
	frames, err := gemini.ParseAllFrames(body)
	if err != nil && len(frames) == 0 {
		log.Printf("Parse error: %v", err)
		c.JSON(http.StatusInternalServerError, openai.ErrorResponse{
			Error: openai.ErrorDetail{
				Message: fmt.Sprintf("Failed to parse response: %v", err),
				Type:    "server_error",
			},
		})
		return
	}

	resp := converter.FramesToCompletion(frames, model, requestID)
	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, body io.ReadCloser, model, requestID string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	state := converter.NewStreamState(model, requestID)
	parser := gemini.NewStreamParser(body)
	flusher, _ := c.Writer.(http.Flusher)

	// Send initial role chunk
	initChunk := &openai.ChatCompletionChunk{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openai.Choice{
			{
				Index: 0,
				Delta: &openai.Message{Role: "assistant"},
			},
		},
	}
	writeSSE(c, initChunk)
	if flusher != nil {
		flusher.Flush()
	}

	for {
		frame, err := parser.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Stream parse error: %v", err)
			break
		}

		chunk := state.FrameToChunk(frame)
		if chunk != nil {
			writeSSE(c, chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}

		if frame.IsFinal {
			break
		}
	}

	// Send [DONE] sentinel
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeSSE(c *gin.Context, chunk *openai.ChatCompletionChunk) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
}
