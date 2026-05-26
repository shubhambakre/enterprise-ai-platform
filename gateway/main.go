package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()
var rdb *redis.Client

type LLMRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

type LLMResponse struct {
	Response string `json:"response"`
	Cached   bool   `json:"cached"`
}

func init() {
	// Initialize Redis client
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "secretpassword", // No password set
		DB:       0,                // use default DB
	})

	// Check Redis connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully.")
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
	var req LLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. Semantic Caching Check (Simulated with exact string match for simplicity in POC)
	// In a real L5 system, we would embed the prompt here and do a cosine similarity search in Redis.
	cacheKey := fmt.Sprintf("cache:%s:%s", req.Model, req.Prompt)
	val, err := rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		// Cache hit!
		log.Printf("[CACHE HIT] Returning cached response for prompt: %s", req.Prompt)
		c.JSON(http.StatusOK, LLMResponse{
			Response: val,
			Cached:   true,
		})
		return
	}

	// 2. Cache Miss - Forward to Primary Model
	log.Printf("[CACHE MISS] Forwarding request to Primary Model: %s", req.Model)
	response := simulatePrimaryModelCall(req.Prompt)

	// 3. Shadow Routing (Asynchronous)
	// We want to test a new "experimental-model" without impacting the user's critical path.
	go func(prompt string) {
		log.Printf("[SHADOW ROUTING] Sending prompt to experimental-model asynchronously...")
		shadowResp := simulateExperimentalModelCall(prompt)
		// In a real system, we'd log this comparison to BigQuery or Cloud Logging for offline evaluation.
		log.Printf("[SHADOW ROUTING] Primary got '%s', Shadow got '%s'", response, shadowResp)
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

// simulatePrimaryModelCall mocks an external API call to an LLM provider (e.g., Vertex AI)
func simulatePrimaryModelCall(prompt string) string {
	time.Sleep(500 * time.Millisecond) // simulate network/inference latency
	return fmt.Sprintf("Primary model response to: %s", prompt)
}

// simulateExperimentalModelCall mocks a call to a newer, experimental model
func simulateExperimentalModelCall(prompt string) string {
	time.Sleep(700 * time.Millisecond)
	return fmt.Sprintf("Experimental model response to: %s", prompt)
}
