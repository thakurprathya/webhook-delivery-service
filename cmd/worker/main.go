package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"webhook-delivery/internal/backoff"
	"webhook-delivery/internal/platform"
	"webhook-delivery/internal/queue"
	"webhook-delivery/internal/worker"

	"github.com/redis/go-redis/v9"
)

func main() {
	// --------------------------------------------------------------- COMPOSITION ROOT (Dependency Injection Wiring) ---------------------------------------------------------------

	// Singleton Pattern Usage: Returns the single shared Redis instance.
	rdb, err := platform.GetRedisClient()
	if err != nil {
		// Fail Fast Principle: Don't start workers if infrastructure is down.
		log.Fatalf("Could not initialize infrastructure: %v", err)
	}
	fmt.Println("🔌 Worker Connected to Valkey")

	// Adapter Pattern Usage: Program to Queue interface, not RedisQueue.
	queueSvc := queue.NewRedisQueue(rdb)

	// Strategy Pattern (Dependency Injection): Choose the backoff algorithm.
	// Open/Closed Principle: Worker code is closed for modification.
	retryStrategy := backoff.NewExponentialStrategy(1*time.Second, 2.0, 1*time.Hour)

	// Factory Pattern + Constructor Injection: All dependencies are injected upfront.
	processor := worker.NewProcessor(rdb, queueSvc, retryStrategy)

	// Polling Pattern: Background goroutine that periodically checks for due retries.
	// Separation of Concerns: Retry scheduling is decoupled from task processing.
	go startRetryPoller(rdb, queueSvc)

	// --------------------------------------------------------------- COMPETING CONSUMERS PATTERN ---------------------------------------------------------------
	// Worker Pool Pattern: Multiple goroutines consume from the SAME queue concurrently.
	// This is the "Competing Consumers" pattern — tasks are distributed across workers. for scalabiility increase workers
	numWorkers := 5
	var wg sync.WaitGroup

	// Context-Based Cancellation: A single cancel() propagates shutdown
	// signal to ALL workers simultaneously via context.
	ctx, cancel := context.WithCancel(context.Background())

	fmt.Printf("🚀 Starting %d Workers...\n", numWorkers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		// Fan-Out Pattern: Each goroutine is an independent consumer.
		go func(workerID int) {
			defer wg.Done()
			startConsumer(ctx, workerID, queueSvc, processor)
		}(i)
	}

	// --------------------------------------------------------------- GRACEFUL SHUTDOWN PATTERN ---------------------------------------------------------------
	// Signal Handling: Listen for OS-level termination signals (Ctrl+C, Docker stop).
	// Buffered channel (size 1) prevents signal loss if we're not yet listening.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	// Blocking Wait: Main goroutine sleeps here until shutdown signal arrives.
	<-quit

	fmt.Println("\n🛑 Shutting down workers...")
	// Cascading Cancellation: cancel() triggers ctx.Done() in all workers.
	cancel()
	// Barrier Pattern: wg.Wait() blocks until ALL workers have finished their current task and exited cleanly. No data loss.
	wg.Wait()
	fmt.Println("✅ Workers stopped. Bye!")
}

// Consumer Loop: Each worker runs this infinite loop.
// Cooperative Cancellation Pattern: Workers check ctx.Done() between tasks to respond to shutdown signals promptly.
func startConsumer(ctx context.Context, id int, q queue.Queue, p *worker.Processor) {
	fmt.Printf("Worker %d started\n", id)
	for {
		// Priority Select: Check for shutdown BEFORE asking for work.
		// This prevents starting a new task when we're supposed to be stopping.
		select {
		case <-ctx.Done():
			fmt.Printf("Worker %d stopping (Context Done)\n", id)
			return
		default:
			// "Busy Loop" logic : Continue to dequeue
		}

		// Get Task
		// Note: In production, use a timeout on Dequeue so we can check ctx.Done() frequently.
		// For this project, we assume Dequeue blocks but handles context cancellation if you implemented it.
		// Since our Dequeue uses 0 (infinite wait), this will block until a task arrives.
		// FIX: Heartbeat Dequeue (Waits max 1 second) (queue updated as shutting down was getting blocked)
		// Heartbeat Dequeue: BRPop with 1s timeout (see queue.go).
		// This ensures we re-check ctx.Done() every second.
		task, err := q.Dequeue(ctx)
		if err != nil {
			// Context Canceled: Shutdown signal received during dequeue wait.
			if errors.Is(err, context.Canceled) {
				fmt.Printf("Worker %d stopping (Dequeue Canceled)\n", id)
				return
			}

			// Sentinel Error Check: redis.Nil means "no task available" (heartbeat timeout).
			// This is expected behavior, not an error — continue the loop.
			if errors.Is(err, redis.Nil) {
				continue
			}

			// Real Error: Log and back off to prevent tight error loops.
			// Defensive Programming: Sleep prevents CPU spinning on persistent errors.
			log.Printf("Worker %d error: %v", id, err)
			time.Sleep(1 * time.Second) // Exponential backoff in real apps
			continue
		}

		// Command Pattern Usage: Delegate execution to the Processor.
		// The consumer loop doesn't know HOW tasks are processed.
		p.Process(ctx, task)
	}
}

// Polling Pattern + Scheduler Pattern: Periodically moves "due" tasks from the retry sorted set back to the main queue.
// Time-Based Priority Queue: Redis ZSET Score = Unix timestamp of when the task should execute. We query for Score <= Now.
func startRetryPoller(rdb *redis.Client, q queue.Queue) {
	// Ticker Pattern: Creates a regular heartbeat for polling.
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		// 1. Query ZSET for tasks with Score <= Now
		now := float64(time.Now().Unix())

		// Range Query on Sorted Set: Get all tasks with score (timestamp) <= now.
		// These are tasks whose "wait time" has expired and are ready to retry.
		results, err := rdb.ZRangeByScore(context.Background(), "retry_schedule", &redis.ZRangeBy{
			Min: "-inf",
			Max: fmt.Sprintf("%f", now),
		}).Result()

		if err != nil {
			log.Printf("Poller Error: %v", err)
			continue
		}

		if len(results) == 0 {
			continue
		}

		// Re-enqueue Pattern: Move tasks from retry set back to main queue.
		for _, member := range results {
			fmt.Printf("⏰ Scheduler: Moving task back to queue: %s\n", member)

			// Deserialization: Reconstruct task from stored JSON.
			var task queue.Task
			_ = json.Unmarshal([]byte(member), &task)

			// Adapter Pattern Usage: Enqueue via the Queue interface.
			q.Enqueue(context.Background(), task)

			// Cleanup: Remove processed item from retry set to prevent re-processing.
			rdb.ZRem(context.Background(), "retry_schedule", member)
		}
	}
}
