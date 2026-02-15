package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"webhook-delivery/internal/backoff"
	"webhook-delivery/internal/queue"

	"github.com/redis/go-redis/v9"
)

// Processor handles the lifecycle of a task
type Processor struct {
	rdb     *redis.Client
	queue   queue.Queue
	backoff backoff.Strategy
}

func NewProcessor(rdb *redis.Client, q queue.Queue, b backoff.Strategy) *Processor {
	return &Processor{
		rdb:     rdb,
		queue:   q,
		backoff: b,
	}
}

func (p *Processor) Process(ctx context.Context, task *queue.Task) {
	fmt.Printf("🔄 Processing Task: %s (Attempt %d)\n", task.Payload, task.RetryCount+1)

	// 1. Simulate HTTP Request (The "Job")
	success := p.simulateWebhookCall(task.Payload)

	if success {
		fmt.Printf("✅ Success: %s\n", task.Payload)
		return // Done! Task is already removed from queue by Dequeue
	}

	// 2. Handle Failure
	p.handleFailure(ctx, task)
}

func (p *Processor) simulateWebhookCall(payload string) bool {
	// Simulate 50% chance of failure
	time.Sleep(500 * time.Millisecond) // Network latency
	return rand.Intn(2) == 1
}

func (p *Processor) handleFailure(ctx context.Context, task *queue.Task) {
	// A. Check Max Retries (Circuit Breaker logic)
	if task.RetryCount >= 5 {
		fmt.Printf("💀 Dead Letter: Task failed too many times. Dropping %s\n", task.Payload)
		// TODO: In a real app, push this to a "dead_letter_queue" list for manual inspection
		return
	}

	// B. Calculate Wait Time
	waitDuration := p.backoff.GetNextInterval(task.RetryCount)
	task.RetryCount++ // Increment counter

	fmt.Printf("⚠️ Failed. Retrying in %v (Attempt %d)\n", waitDuration, task.RetryCount+1)

	// C. Schedule Future Retry (The "Scheduler")
	// We cannot use the main queue because that's for "Now".
	// We use a Redis "ZSET" (Sorted Set) where Score = Execution Timestamp.
	p.scheduleRetry(ctx, task, waitDuration)
}

func (p *Processor) scheduleRetry(ctx context.Context, task *queue.Task, delay time.Duration) {
	// We need to re-serialize the task to store it
	// (Skipping error handling for brevity, but critical in prod)
	// In real app: create a queue.Serialize(task) helper

	// We marshal the WHOLE task struct, so we keep the payload AND the retry_count
	data, err := json.Marshal(task)
	if err != nil {
		log.Printf("ERROR: Could not marshal task for retry: %v", err)
		return
	}

	// Score = Now + Delay
	executeAt := time.Now().Add(delay).Unix()

	// Add to ZSET
	// Member is now the valid JSON string: {"payload":"Payment 500","retry_count":1}
	err = p.rdb.ZAdd(ctx, "retry_schedule", redis.Z{
		Score:  float64(executeAt),
		Member: data, // Pass the byte slice directly (go-redis handles it)
	}).Err()

	if err != nil {
		log.Printf("Error scheduling retry: %v", err)
	}
}
