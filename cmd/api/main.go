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

// 1. Request DTO (Data Transfer Object)
type WebhookRequest struct {
	UserID string          `json:"user_id"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// 2. Response DTO
type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func main() {
	// A. Infrastructure (Singleton)
	rdb, err := platform.GetRedisClient()
	if err != nil {
		log.Fatalf("Could not initialize infrastructure: %v", err)
	}
	fmt.Println("🔌 Producer Connected to Valkey")

	// B. Services (Dependency Injection)
	// Strategy Pattern (FixedWindowLimiter).
	limiter := ratelimit.NewFixedWindowLimiter(rdb, 5, 60*time.Second)
	// limiter = ratelimit.NewNoOpLimiter()

	// Adapter Pattern: Connecting Redis to our generic Queue interface.
	var queueSvc queue.Queue = queue.NewRedisQueue(rdb)

	// C. The Handler Wrapper
	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		// STEP 1: Method Validation (REST Compliance)
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
			return
		}

		// STEP 2: Parse & Validate JSON Body
		var req WebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		// Validation: "Fail Fast"
		if req.UserID == "" {
			http.Error(w, "Field 'user_id' is required", http.StatusBadRequest)
			return
		}

		// STEP 3: Rate Limiting (The "Doorman")
		// We use the Context from the request so if the user cancels
		// the request (closes tab), we stop processing.
		allowed, err := limiter.Allow(r.Context(), req.UserID)
		if err != nil {
			// Log the error internally, but show generic 500 to user (Security)
			log.Printf("Rate limit error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !allowed {
			// We return 429 Too Many Requests (Standard Code)
			http.Error(w, "Rate limit exceeded. Try again later.", http.StatusTooManyRequests)
			return
		}

		// STEP 4: Enqueue the Task (Producer)
		// We treat the 'Data' they sent as the payload.
		// We convert the raw bytes back to string for storage.
		newTask := queue.Task{
			Payload:    string(req.Data),
			RetryCount: 0,
		}

		if err := queueSvc.Enqueue(r.Context(), newTask); err != nil {
			log.Printf("Queue error: %v", err)
			http.Error(w, "Failed to accept task", http.StatusInternalServerError)
			return
		}

		// STEP 5: Success Response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // 202 Accepted (Standard for async jobs)

		response := APIResponse{
			Status:  "success",
			Message: "Request accepted and queued for delivery.",
		}
		json.NewEncoder(w).Encode(response)
	})

	log.Println("🚀 API Server running on port 8080")
	// Standard Timeout protections (Crucial for Scaled Apps to prevent Slowloris attacks)
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
