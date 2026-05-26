package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	
	// Vertex AI SDK (Enterprise IAM)
	vertexai "cloud.google.com/go/vertexai/genai"
	
	// Google AI Studio SDK (Developer API Key)
	googleai "github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

var rdb *redis.Client
var vertexClient *vertexai.Client
var googleAIClient *googleai.Client

type LLMRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"` // e.g., "gemini-2.5-flash" (Vertex) or "gemini-3.1-pro-preview" (Google AI)
}

type LLMResponse struct {
	Response string `json:"response"`
	Provider string `json:"provider"`
	Cached   bool   `json:"cached"`
}

func init() {
	// 1. Initialize Redis client for Semantic Caching
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "secretpassword",
		DB:       0,
	})

	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully.")

	// 2. Initialize Vertex AI Client (Enterprise ADC)
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID != "" {
		vertexClient, err = vertexai.NewClient(ctx, projectID, "us-central1")
		if err != nil {
			log.Printf("Failed to initialize Vertex AI client: %v", err)
		} else {
			log.Println("Initialized Vertex AI client successfully.")
		}
	}

	// 3. Initialize Google AI Studio Client (API Key)
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey != "" {
		googleAIClient, err = googleai.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			log.Printf("Failed to initialize Google AI client: %v", err)
		} else {
			log.Println("Initialized Google AI Studio client successfully.")
		}
	}
}

func main() {
	r := gin.Default()

	r.POST("/v1/completions", handleCompletion)

	log.Println("Starting Multi-Provider AI Gateway on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleCompletion(c *gin.Context) {
	ctx := c.Request.Context()
	var req LLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Model == "" {
		req.Model = "gemini-2.5-flash" // Default
	}

	// 1. Semantic Caching Check
	cacheKey := fmt.Sprintf("cache:%s:%s", req.Model, req.Prompt)
	val, err := rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		log.Printf("[CACHE HIT] Returning cached response for prompt: %s", req.Prompt)
		c.JSON(http.StatusOK, LLMResponse{
			Response: val,
			Provider: "Redis Semantic Cache",
			Cached:   true,
		})
		return
	}

	// 2. Cache Miss - Call Primary Model based on Provider Routing
	log.Printf("[CACHE MISS] Forwarding request to %s", req.Model)
	response, providerUsed, err := callLLMAPI(ctx, req.Model, req.Prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 3. Save to Cache
	err = rdb.Set(ctx, cacheKey, response, 1*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to set cache: %v", err)
	}

	c.JSON(http.StatusOK, LLMResponse{
		Response: response,
		Provider: providerUsed,
		Cached:   false,
	})
}

// callLLMAPI routes to either Vertex AI or Google AI Studio based on the requested model
func callLLMAPI(ctx context.Context, modelName, prompt string) (string, string, error) {
	// Route to Google AI Studio if using gemini-3.1-pro-preview
	if modelName == "gemini-3.1-pro-preview" {
		if googleAIClient == nil {
			return "", "", fmt.Errorf("Google AI Studio client is not initialized (missing GEMINI_API_KEY)")
		}
		model := googleAIClient.GenerativeModel(modelName)
		model.SetTemperature(0.2)

		resp, err := model.GenerateContent(ctx, googleai.Text(prompt))
		if err != nil {
			return "", "", fmt.Errorf("Google AI Studio call failed: %v", err)
		}

		if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
			if textPart, ok := resp.Candidates[0].Content.Parts[0].(googleai.Text); ok {
				return string(textPart), "Google AI Studio (API Key)", nil
			}
		}
		return "No text returned from Gemini", "Google AI Studio", nil
	}

	// Default to Vertex AI for enterprise models like gemini-2.5-flash
	if vertexClient == nil {
		return "", "", fmt.Errorf("Vertex AI client is not initialized (missing GCP_PROJECT_ID)")
	}
	model := vertexClient.GenerativeModel(modelName)
	model.SetTemperature(0.2)

	resp, err := model.GenerateContent(ctx, vertexai.Text(prompt))
	if err != nil {
		return "", "", fmt.Errorf("Vertex AI call failed: %v", err)
	}

	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		if textPart, ok := resp.Candidates[0].Content.Parts[0].(vertexai.Text); ok {
			return string(textPart), "GCP Vertex AI (Enterprise IAM)", nil
		}
	}
	return "No text returned from Gemini", "GCP Vertex AI", nil
}
