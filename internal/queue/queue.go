package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// --------------------------------------------------------------- ADAPTER PATTERN + "Dependency Inversion Principle" ---------------------------------------------------------------

// 1. The Data Structure (DTO)
type Task struct {
	Payload    string    `json:"payload"`
	RetryCount int       `json:"retry_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// 2. The Interface (Contract)
// This is the "Dependency Inversion Principle".
type Queue interface {
	Enqueue(ctx context.Context, task Task) error
	Dequeue(ctx context.Context) (*Task, error)
}

// 3. The Implementation (Adapter Pattern)
// This adapts Redis to our Queue interface.
type RedisQueue struct {
	rdb *redis.Client
}

// Factory Function
// we return the Interface `Queue`, not the struct `*RedisQueue`.
func NewRedisQueue(rdb *redis.Client) Queue {
	return &RedisQueue{rdb: rdb}
}

// Enqueue (Producer)
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
	// LPush → Redis command to push an item to the head of a list
	// "webhook_queue" → name of the Redis list
	return q.rdb.LPush(ctx, "webhook_queue", data).Err()
}

// Dequeue (Consumer)
func (q *RedisQueue) Dequeue(ctx context.Context) (*Task, error) {
	// BRPop → Redis command to pop an item from the tail of a list
	// 0 → block indefinitely if the list is empty (wait until a task arrives)
	// "webhook_queue" → name of the list
	result, err := q.rdb.BRPop(ctx, 0, "webhook_queue").Result()
	if err != nil {
		return nil, err
	}

	// result[1] is the JSON string
	var task Task
	err = json.Unmarshal([]byte(result[1]), &task)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}
