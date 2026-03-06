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

// Command Pattern: Processor encapsulates the "execute webhook" action.
// The Task is the Command object carrying payload + metadata (retry count).
// Dependency Injection (DI): All dependencies (rdb, queue, backoff) are injected via the constructor — not created internally. This enables testing and swapping.
type Processor struct {
	rdb     *redis.Client
	queue   queue.Queue      // DI: Depends on Queue interface, not RedisQueue concrete type.
	backoff backoff.Strategy // DI: Depends on Strategy interface, not ExponentialStrategy concrete type.
}

// Factory Function Pattern: Constructs a fully-initialized Processor.
// Constructor Injection: All dependencies are required at creation time, preventing partially-initialized objects.
func NewProcessor(rdb *redis.Client, q queue.Queue, b backoff.Strategy) *Processor {
	return &Processor{
		rdb:     rdb,
		queue:   q,
		backoff: b,
	}
}

// Template Method Pattern: Process defines the skeleton algorithm:
// 1. Execute the job → 2. If success, done → 3. If failure, handle retry.
// Subsets of this logic (backoff calculation, queue interaction) are delegated to injected strategies.
func (p *Processor) Process(ctx context.Context, task *queue.Task) {
	fmt.Printf("🔄 Processing Task: %s (Attempt %d)\n", task.Payload, task.RetryCount+1)

	// Step 1: Execute the job (Command Execution) :  Simulate HTTP Request (The "Job")
	success := p.simulateWebhookCall(task.Payload)

	if success {
		fmt.Printf("✅ Success: %s\n", task.Payload)
		return // Done! Task is already removed from queue by Dequeue
	}

	// Step 2: Delegate failure handling
	p.handleFailure(ctx, task)
}

// Simulation Stub: In production, this would be an actual HTTP POST.
// Separation of Concerns: The retry/backoff logic doesn't care HOW the webhook is delivered — only whether it succeeded or failed.
func (p *Processor) simulateWebhookCall(payload string) bool {
	// Simulate 50% chance of failure
	time.Sleep(500 * time.Millisecond) // Network latency
	return rand.Intn(2) == 1
}

// Circuit Breaker Pattern (simplified): After max retries, the task is "tripped" to the dead letter state instead of retrying forever.
// This prevents infinite retry loops from consuming resources.
func (p *Processor) handleFailure(ctx context.Context, task *queue.Task) {
	// Circuit Breaker Threshold: Stop retrying after 5 attempts.
	if task.RetryCount >= 5 {
		// Dead Letter Queue Pattern (placeholder): Failed tasks are dropped here.
		// In production, push to a "dead_letter_queue" for manual inspection/replay.
		fmt.Printf("💀 Dead Letter: Task failed too many times. Dropping %s\n", task.Payload)
		return
	}

	// Strategy Pattern Usage: Delegates timing calculation to the injected strategy.
	// Polymorphism: p.backoff.GetNextInterval() calls whichever concrete implementation was injected at construction time.
	waitDuration := p.backoff.GetNextInterval(task.RetryCount)
	task.RetryCount++

	fmt.Printf("⚠️ Failed. Retrying in %v (Attempt %d)\n", waitDuration, task.RetryCount+1)

	// Deferred Execution Pattern: Schedule future retry via Redis Sorted Set.
	// We cannot use the main queue because that's for "Now".
	// We use a Redis "ZSET" (Sorted Set) where Score = Execution Timestamp.
	p.scheduleRetry(ctx, task, waitDuration)
}

// Scheduler Pattern: Uses Redis ZSET (Sorted Set) where Score = Unix timestamp.
// This creates a time-based priority queue — tasks "become ready" when current time >= their score.
func (p *Processor) scheduleRetry(ctx context.Context, task *queue.Task, delay time.Duration) {
	// We need to re-serialize the task to store it
	// (Skipping error handling for brevity, but critical in prod)
	// In real app: create a queue.Serialize(task) helper

	// Serialization: Marshal the entire task including updated retry_count, so the next attempt knows it's attempt #N.
	data, err := json.Marshal(task)
	if err != nil {
		log.Printf("ERROR: Could not marshal task for retry: %v", err)
		return
	}

	// Time-Based Scoring: Score = "execute at" timestamp.
	// The retry poller queries for Score <= Now to find "due" tasks.
	executeAt := time.Now().Add(delay).Unix()

	// Redis ZADD: Atomic insertion into the sorted set. Add to ZSET
	// Member is now the valid JSON string: {"payload":"Payment 500","retry_count":1}
	err = p.rdb.ZAdd(ctx, "retry_schedule", redis.Z{
		Score:  float64(executeAt),
		Member: data, // Pass the byte slice directly (go-redis handles it)
	}).Err()

	if err != nil {
		log.Printf("Error scheduling retry: %v", err)
	}
}
