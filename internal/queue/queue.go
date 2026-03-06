package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Data Transfer Object (DTO): A pure data carrier with no business logic.
// Serialization Tags: `json` tags define the wire format, decoupling internal field names from the external API contract.
type Task struct {
	Payload    string    `json:"payload"`
	RetryCount int       `json:"retry_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// Interface Segregation Principle (ISP): Only two methods — consumers aren't forced to depend on methods they don't use.
// Dependency Inversion Principle (DIP): High-level modules (worker, API) depend on this abstraction, not on concrete Redis implementation.
type Queue interface {
	Enqueue(ctx context.Context, task Task) error
	Dequeue(ctx context.Context) (*Task, error)
}

// Adapter Pattern: Adapts the Redis client (third-party library) to conform to our domain-specific Queue interface.
// Composition over Inheritance: RedisQueue "has-a" redis.Client, not "is-a".
type RedisQueue struct {
	rdb *redis.Client
}

// Factory Function Pattern: Encapsulates object creation.
// Open/Closed Principle (OCP): Returns the Queue interface, not *RedisQueue.
// We can swap Redis for Kafka/RabbitMQ by creating a new struct that implements Queue — without changing any consumer code.
func NewRedisQueue(rdb *redis.Client) Queue {
	return &RedisQueue{rdb: rdb}
}

// Producer Method (Producer-Consumer Pattern)
func (q *RedisQueue) Enqueue(ctx context.Context, task Task) error {
	// Set default creation time if missing
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	// Serialize: Convert Struct -> JSON String
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Push to Redis List
	// FIFO Queue via Redis List: LPush (head) + BRPop (tail) = First-In-First-Out
	// LPush → Redis command to push an item to the head of a list
	// "webhook_queue" → name of the Redis list
	return q.rdb.LPush(ctx, "webhook_queue", data).Err()
}

// Consumer Method (Producer-Consumer Pattern)
func (q *RedisQueue) Dequeue(ctx context.Context) (*Task, error) {
	// BRPop → Redis command to pop an item from the tail of a list
	// 0 → block indefinitely if the list is empty (wait until a task arrives)
	// "webhook_queue" → name of the list
	// result, err := q.rdb.BRPop(ctx, 0, "webhook_queue").Result()
	// if err != nil {
	// 	return nil, err
	// }

	// FIX: Change 0 to 1 * time.Second
	// Heartbeat Pattern : This creates a "Heartbeat". Every 1 second, the Redis command will
	// "timeout" and return a redis.Nil error if no task is found.
	// This gives our worker a chance to check ctx.Done() and exit cleanly.
	result, err := q.rdb.BRPop(ctx, 1*time.Second, "webhook_queue").Result()
	if err != nil {
		return nil, err
	}

	// Deserialization
	// result[1] is the JSON string
	var task Task
	err = json.Unmarshal([]byte(result[1]), &task)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}
