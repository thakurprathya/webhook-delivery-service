package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"webhook-delivery/internal/platform"
	"webhook-delivery/internal/queue"
	"webhook-delivery/internal/ratelimit"
)

// Request DTO (Data Transfer Object): Pure data carrier for incoming JSON.
// Single Responsibility: Only responsible for request shape, no business logic.
// json.RawMessage defers parsing of "data" — we don't need to know its shape.
type WebhookRequest struct {
	UserID string          `json:"user_id"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Response DTO: Defines the API's response contract.
type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func main() {
	// Composition Root: This is where ALL dependencies are wired together.
	// Dependency Injection happens here — the "main" function is the only place that knows about concrete implementations.

	// Singleton Pattern Usage: GetRedisClient() returns the single shared instance.
	rdb, err := platform.GetRedisClient()
	if err != nil {
		// Fail Fast Principle: Crash immediately if infrastructure is unavailable.
		log.Fatalf("Could not initialize infrastructure: %v", err)
	}
	fmt.Println("🔌 Producer Connected to Valkey")

	// Strategy Pattern (Dependency Injection): We choose the concrete algorithm
	limiter := ratelimit.NewFixedWindowLimiter(rdb, 5, 60*time.Second)
	// limiter = ratelimit.NewNoOpLimiter()

	// Adapter Pattern (Dependency Injection): We program to the Queue interface.
	// Swapping Redis for Kafka/RabbitMQ requires changing only this line.
	var queueSvc queue.Queue = queue.NewRedisQueue(rdb)

	// Closure-Based Handler: The handler "closes over" limiter and queueSvc,
	// avoiding global state. This is Go's idiomatic alternative to class-based controllers.
	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		// Guard Clause Pattern: Fail fast on wrong HTTP method.
		// REST Compliance: Only POST is valid for creating resources.
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
			return
		}

		// Deserialization: Parse wire format (JSON) into domain DTO.
		var req WebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		// Fail Fast Validation: Reject invalid input before doing any work.
		// Defense in Depth: Validate at the boundary (API layer).
		if req.UserID == "" {
			http.Error(w, "Field 'user_id' is required", http.StatusBadRequest)
			return
		}

		// Strategy Pattern Usage: limiter.Allow() calls whichever concrete implementation was injected above (FixedWindow or NoOp).
		// Context Propagation: r.Context() allows cancellation if client disconnects.
		allowed, err := limiter.Allow(r.Context(), req.UserID)
		if err != nil {
			// Security Principle: Log details internally, show generic error to user.
			// Never leak internal error messages to the client.
			log.Printf("Rate limit error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !allowed {
			// HTTP 429 Too Many Requests: Standard status code for rate limiting.
			http.Error(w, "Rate limit exceeded. Try again later.", http.StatusTooManyRequests)
			return
		}

		// Producer-Consumer Pattern (Producer Side): Create task and enqueue.
		// The API doesn't process the task — it delegates to the worker via the queue.
		// Asynchronous Processing: Decouples request handling from execution.
		newTask := queue.Task{
			Payload:    string(req.Data),
			RetryCount: 0,
		}

		// Adapter Pattern Usage: queueSvc.Enqueue() calls RedisQueue's implementation, but this code only knows about the Queue interface.
		if err := queueSvc.Enqueue(r.Context(), newTask); err != nil {
			log.Printf("Queue error: %v", err)
			http.Error(w, "Failed to accept task", http.StatusInternalServerError)
			return
		}

		// HTTP 202 Accepted: Standard status code for async operations.
		// The request is acknowledged but NOT yet processed.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)

		response := APIResponse{
			Status:  "success",
			Message: "Request accepted and queued for delivery.",
		}
		json.NewEncoder(w).Encode(response)
	})

	log.Println("🚀 API Server running on port 8080")
	// Defensive Server Configuration: Timeouts prevent Slowloris attacks
	// where a malicious client holds connections open indefinitely.
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
