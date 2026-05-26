package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// In an L5 system, we use a connection pool (pgxpool) instead of single connections
var dbPool *pgxpool.Pool
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// IngestionBatcher handles high-throughput WebSocket streams by batching inserts
type IngestionBatcher struct {
	eventChan chan string
	batchSize int
	mu        sync.Mutex
}

func newBatcher(size int) *IngestionBatcher {
	b := &IngestionBatcher{
		eventChan: make(chan string, size*2),
		batchSize: size,
	}
	go b.startBatchProcessor()
	return b
}

func (b *IngestionBatcher) startBatchProcessor() {
	var batch []string
	ticker := time.NewTicker(500 * time.Millisecond) // flush every 500ms even if not full

	for {
		select {
		case event := <-b.eventChan:
			batch = append(batch, event)
			if len(batch) >= b.batchSize {
				b.flush(batch)
				batch = make([]string, 0, b.batchSize)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				b.flush(batch)
				batch = make([]string, 0, b.batchSize)
			}
		}
	}
}

func (b *IngestionBatcher) flush(batch []string) {
	ctx := context.Background()
	
	// Start a transaction for bulk insert
	tx, err := dbPool.Begin(ctx)
	if err != nil {
		log.Printf("Failed to begin tx: %v", err)
		return
	}
	defer tx.Rollback(ctx)

	// Build a bulk insert query (in reality, we'd use pgx.CopyFrom for massive scale)
	// For this POC, we use a simple loop.
	for _, content := range batch {
		// MOCK EMBEDDING: Generate a random 768-dim vector to simulate an OpenAI/Vertex embedding model
		embedding := generateMockEmbedding(768)
		_, err := tx.Exec(ctx, "INSERT INTO stream_events (content, embedding) VALUES ($1, $2)", content, embedding)
		if err != nil {
			log.Printf("Insert failed: %v", err)
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("Commit failed: %v", err)
	} else {
		log.Printf("[BATCH INGEST] Successfully upserted %d real-time events to pgvector", len(batch))
	}
}

func main() {
	ctx := context.Background()
	var err error

	// 1. Initialize PostgreSQL connection pool
	connString := "postgres://postgres:secretpassword@localhost:5432/streaming_rag?sslmode=disable"
	dbPool, err = pgxpool.New(ctx, connString)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer dbPool.Close()

	// 2. Setup pgvector schema
	initDB(ctx)

	// 3. Start Batch Ingester
	batcher := newBatcher(100) // Batch up to 100 events for efficient DB inserts

	// 4. Setup HTTP & WebSocket Server
	r := gin.Default()
	
	// WebSocket endpoint for real-time ingestion
	r.GET("/ws/ingest", func(c *gin.Context) {
		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}
		defer ws.Close()
		log.Println("New WebSocket client connected for high-speed ingestion")

		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				log.Println("Client disconnected")
				break
			}
			// Push to buffered channel for batching
			batcher.eventChan <- string(message)
		}
	})

	// RAG Query endpoint
	r.GET("/v1/query", func(c *gin.Context) {
		query := c.Query("q")
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Query param 'q' is required"})
			return
		}

		// Mock embedding the search query
		queryEmbedding := generateMockEmbedding(768)

		// L5 Pattern: Perform Cosine Distance search (`<=>`) over the vector column
		rows, err := dbPool.Query(ctx, 
			"SELECT content, 1 - (embedding <=> $1) as similarity FROM stream_events ORDER BY embedding <=> $1 LIMIT 3", 
			queryEmbedding)
		
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var results []map[string]interface{}
		for rows.Next() {
			var content string
			var sim float32
			if err := rows.Scan(&content, &sim); err != nil {
				continue
			}
			results = append(results, map[string]interface{}{"content": content, "similarity": sim})
		}

		c.JSON(http.StatusOK, gin.H{
			"query":   query,
			"context": results,
		})
	})

	log.Println("Starting Streaming RAG engine on :8081...")
	r.Run(":8081")
}

func initDB(ctx context.Context) {
	// Enable pgvector
	_, err := dbPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		log.Fatalf("Failed to create vector extension: %v", err)
	}

	// Create table for streaming events
	schema := `
	CREATE TABLE IF NOT EXISTS stream_events (
		id bigserial PRIMARY KEY,
		content text NOT NULL,
		embedding vector(768),
		created_at timestamp DEFAULT current_timestamp
	);
	`
	_, err = dbPool.Exec(ctx, schema)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
	
	// Create HNSW index for ultra-fast vector search (L5 optimization)
	_, err = dbPool.Exec(ctx, "CREATE INDEX IF NOT EXISTS stream_events_emb_idx ON stream_events USING hnsw (embedding vector_cosine_ops)")
	if err != nil {
		log.Printf("Note: HNSW index creation failed (might already exist): %v", err)
	}
	
	log.Println("pgvector schema initialized successfully.")
}

// generateMockEmbedding creates a random vector to simulate an embedding model call
func generateMockEmbedding(dim int) string {
	var buffer bytes.Buffer
	buffer.WriteString("[")
	for i := 0; i < dim; i++ {
		if i > 0 {
			buffer.WriteString(",")
		}
		buffer.WriteString(fmt.Sprintf("%f", rand.Float32()))
	}
	buffer.WriteString("]")
	return buffer.String()
}
