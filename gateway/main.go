package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/vertexai/genai"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client
var vertexClient *genai.Client
var projectID string

type LLMRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

type LLMResponse struct {
	Response string `json:"response"`
	Cached   bool   `json:"cached"`
}

func init() {
	// Initialize Redis client for Semantic Caching
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

	// Initialize Vertex AI client
	projectID = os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		log.Println("WARNING: GCP_PROJECT_ID environment variable is not set. Gateway will fall back to mocked responses.")
	} else {
		// Use application default credentials
		vertexClient, err = genai.NewClient(ctx, projectID, "us-central1")
		if err != nil {
			log.Fatalf("Failed to initialize Vertex AI client: %v", err)
		}
		log.Println("Initialized Vertex AI client successfully.")
	}
}

func main() {
	r := gin.Default()

	r.POST("/v1/completions", handleCompletion)

	log.Println("Starting AI Gateway on :8080...")
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

	// 1. Semantic Caching Check
	cacheKey := fmt.Sprintf("cache:%s:%s", req.Model, req.Prompt)
	val, err := rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		log.Printf("[CACHE HIT] Returning cached response for prompt: %s", req.Prompt)
		c.JSON(http.StatusOK, LLMResponse{
			Response: val,
			Cached:   true,
		})
		return
	}

	// 2. Cache Miss - Call Primary Model (GCP Vertex AI)
	log.Printf("[CACHE MISS] Forwarding request to Primary Model")
	response, err := callVertexAI(ctx, req.Prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Vertex AI call failed: %v", err)})
		return
	}

	// 3. Shadow Routing (Asynchronous)
	// We duplicate the request to an experimental model to compare outputs offline
	go func(prompt string) {
		shadowCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Printf("[SHADOW ROUTING] Sending prompt asynchronously...")
		_ = simulateExperimentalModelCall(shadowCtx, prompt) // Ignore error in shadow route
	}(req.Prompt)

	// 4. Save to Cache
	err = rdb.Set(ctx, cacheKey, response, 1*time.Hour).Err()
	if err != nil {
		log.Printf("Failed to set cache: %v", err)
	}

	c.JSON(http.StatusOK, LLMResponse{
		Response: response,
		Cached:   false,
	})
}

// callVertexAI calls Gemini 1.5 Flash on Google Cloud, or falls back to a mock if GCP_PROJECT_ID is not set.
func callVertexAI(ctx context.Context, prompt string) (string, error) {
	if vertexClient == nil {
		// Fallback mock mode
		time.Sleep(500 * time.Millisecond)
		return fmt.Sprintf("[Mocked] Vertex AI response to: %s", prompt), nil
	}

	model := vertexClient.GenerativeModel("gemini-2.5-flash")
	model.SetTemperature(0.2)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		if textPart, ok := resp.Candidates[0].Content.Parts[0].(genai.Text); ok {
			return string(textPart), nil
		}
	}
	return "No text returned from Gemini", nil
}

// simulateExperimentalModelCall mocks a call to a newer, experimental model
func simulateExperimentalModelCall(ctx context.Context, prompt string) error {
	time.Sleep(700 * time.Millisecond)
	return nil
}
