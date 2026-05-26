package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
)

var nc *nats.Conn

type OrchestrationRequest struct {
	Goal string `json:"goal"`
}

type Task struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Input string `json:"input"`
}

func main() {
	var err error
	// Connect to NATS
	nc, err = nats.Connect("nats://localhost:4222")
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()
	log.Println("Connected to NATS successfully.")

	r := gin.Default()

	r.POST("/v1/orchestrate", func(c *gin.Context) {
		var req OrchestrationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// 1. Decompose the Goal into sub-tasks (Mocked for POC)
		// In an L5 system, an LLM call here would analyze the goal and generate a DAG of tasks.
		log.Printf("Decomposing goal: %s", req.Goal)
		tasks := []Task{
			{ID: "task-1", Type: "research", Input: "Research step for: " + req.Goal},
			{ID: "task-2", Type: "summarize", Input: "Summarize findings"},
		}

		// 2. Publish tasks to NATS message queue
		var responses []string
		for _, task := range tasks {
			taskData, _ := json.Marshal(task)
			
			// Use Request/Reply pattern over NATS with a 5-second timeout
			log.Printf("[ROUTER] Publishing task to NATS: %s", task.ID)
			msg, err := nc.Request("agent.tasks", taskData, 5*time.Second)
			if err != nil {
				log.Printf("Task %s failed or timed out: %v", task.ID, err)
				responses = append(responses, fmt.Sprintf("%s: Error", task.ID))
				continue
			}
			responses = append(responses, fmt.Sprintf("%s: %s", task.ID, string(msg.Data)))
		}

		c.JSON(http.StatusOK, gin.H{
			"goal":    req.Goal,
			"results": responses,
		})
	})

	log.Println("Starting Orchestrator Router on :8082...")
	r.Run(":8082")
}
