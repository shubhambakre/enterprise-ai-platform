# Enterprise AI Infrastructure Platform

A unified microservices-based AI Infrastructure Platform designed to handle high-concurrency LLM workflows, real-time RAG, and distributed agent orchestration.

## Architecture

This monorepo contains three core services:

1. **`gateway` (Go)**: A high-performance reverse proxy for LLM requests. It implements Semantic Caching (via Redis) to save inference costs and Shadow Routing for asynchronous evaluation of new model endpoints.
2. **`streaming-rag` (Go + Postgres/pgvector)**: *[WIP]* An ingestion and query engine that accepts a continuous stream of events via WebSockets, embedding and storing them in real-time to ground LLM responses in mutating data.
3. **`orchestrator` (Go + NATS + Python)**: *[WIP]* A distributed agentic execution engine. Complex tasks are broken down by a Router and published to a NATS message queue, where stateless Worker nodes process them concurrently.

## Prerequisites

- Docker and Docker Compose
- Go 1.22+
- Python 3.11+ (for Agent Workers)

## Quick Start (Local Testing)

1. **Start the Infrastructure**
   ```bash
   docker compose up -d
   ```
   This spins up:
   - Redis (Semantic Caching)
   - PostgreSQL + pgvector (Streaming RAG DB)
   - NATS (Agent Orchestrator Queue)

2. **Run the AI Gateway**
   ```bash
   cd gateway
   go build -o gateway-bin main.go
   ./gateway-bin
   ```

3. **Test Semantic Caching**
   ```bash
   # First request (Cache Miss) - Takes ~500ms
   curl -X POST http://localhost:8080/v1/completions \
     -H "Content-Type: application/json" \
     -d '{"prompt": "How do I reset my password?", "model": "gemini-pro"}'

   # Second request (Cache Hit) - Takes <5ms
   curl -X POST http://localhost:8080/v1/completions \
     -H "Content-Type: application/json" \
     -d '{"prompt": "How do I reset my password?", "model": "gemini-pro"}'
   ```
