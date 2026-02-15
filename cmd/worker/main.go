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
	// 1. Infrastructure
	rdb, err := platform.GetRedisClient()
	if err != nil {
		log.Fatalf("Could not initialize infrastructure: %v", err)
	}
	fmt.Println("🔌 Worker Connected to Valkey")

	// 2. Setup Dependencies
	queueSvc := queue.NewRedisQueue(rdb)

	// Strategy Pattern: We choose Exponential Backoff
	retryStrategy := backoff.NewExponentialStrategy(1*time.Second, 2.0, 1*time.Hour)

	processor := worker.NewProcessor(rdb, queueSvc, retryStrategy)

	// 3. Start the "Scheduler" (The Retry Poller)
	// This runs in the background and checks for tasks that are ready to be retried.
	go startRetryPoller(rdb, queueSvc)

	// 4. Start Worker Pool (Scalability)
	// We start 5 workers. They will all consume from the SAME queue concurrently.
	// This is the "Competing Consumers" pattern.
	numWorkers := 5
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())

	fmt.Printf("🚀 Starting %d Workers...\n", numWorkers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			startConsumer(ctx, workerID, queueSvc, processor)
		}(i)
	}

	// 5. Graceful Shutdown (The "Clean Exit")
	// If we hit Ctrl+C, we want to stop accepting new tasks but finish current ones.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // Block until signal received

	fmt.Println("\n🛑 Shutting down workers...")
	cancel()  // Tell workers to stop
	wg.Wait() // Wait for them to finish
	fmt.Println("✅ Workers stopped. Bye!")
}

// startConsumer is the infinite loop for each worker
func startConsumer(ctx context.Context, id int, q queue.Queue, p *worker.Processor) {
	fmt.Printf("Worker %d started\n", id)
	for {
		// Check for shutdown signal BEFORE asking for work
		select {
		case <-ctx.Done():
			fmt.Printf("Worker %d stopping (Context Done)\n", id)
			return
		default:
			// "Busy Loop" logic
		}

		// A. Get Task
		// Note: In production, use a timeout on Dequeue so we can check ctx.Done() frequently.
		// For this project, we assume Dequeue blocks but handles context cancellation if you implemented it.
		// Since our Dequeue uses 0 (infinite wait), this will block until a task arrives.
		// FIX: Heartbeat Dequeue (Waits max 1 second) (queue updated as shutting down was getting blocked)
		task, err := q.Dequeue(ctx)
		if err != nil {
			// A. Context Canceled (Ctrl+C during the 1s wait)
			if errors.Is(err, context.Canceled) {
				fmt.Printf("Worker %d stopping (Dequeue Canceled)\n", id)
				return
			}

			// B. Timeout (redis.Nil) -> This is GOOD. It means "No jobs, just checking in."
			// We continue the loop to check ctx.Done() again.
			if errors.Is(err, redis.Nil) {
				continue
			}

			// C. Real Error (Connection died, etc.)
			log.Printf("Worker %d error: %v", id, err)
			time.Sleep(1 * time.Second) // Exponential backoff in real apps
			continue
		}

		// B. Process Task
		p.Process(ctx, task)
	}
}

// startRetryPoller checks the ZSET for tasks that are ready to run
func startRetryPoller(rdb *redis.Client, q queue.Queue) {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		// 1. Query ZSET for tasks with Score <= Now
		now := float64(time.Now().Unix())

		// ZRangeByScore retrieves items that are "due"
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

		// 2. Move them back to Main Queue
		for _, member := range results {
			fmt.Printf("⏰ Scheduler: Moving task back to queue: %s\n", member)

			// Parse the JSON back to a Task
			var task queue.Task
			_ = json.Unmarshal([]byte(member), &task)

			// Push to main queue
			q.Enqueue(context.Background(), task)

			// Remove from ZSET
			rdb.ZRem(context.Background(), "retry_schedule", member)
		}
	}
}
