package handler

import (
	"net/http"
	"time"

	"gem2api/internal/gemini"
	"gem2api/internal/openai"

	"github.com/gin-gonic/gin"
)

// ModelsHandler returns the list of available Gemini models in OpenAI format.
func ModelsHandler(c *gin.Context) {
	var models []openai.ModelData

	for id := range gemini.KnownModels {
		models = append(models, openai.ModelData{
			ID:      id,
			Object:  "model",
			Created: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			OwnedBy: "google",
		})
	}

	c.JSON(http.StatusOK, openai.ModelsResponse{
		Object: "list",
		Data:   models,
	})
}
