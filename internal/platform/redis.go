package platform

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// 1. The Singleton Instance (Private)
// Singleton Pattern: Private package-level variables ensure only one instance exists.
// Encapsulation: These are unexported (lowercase), hiding implementation details from consumers.
var (
	redisInstance *redis.Client
	once          sync.Once // This is the guard. It has a boolean flag inside (internally) that flips from false to true after the function runs. It is thread-safe (mutex locked).
	initErr       error
)

// Singleton Accessor: This is the single global access point to the Redis instance.
// Information Hiding Principle: Callers never see the raw redis.Options or connection logic.
func GetRedisClient() (*redis.Client, error) {
	// 2. sync.Once ensures the function inside is executed ONLY ONCE.
	// Thread-Safety: sync.Once uses an internal mutex + atomic flag.
	// Even if 100 goroutines call GetRedisClient() simultaneously,
	// this block runs for the first one, and others wait until it's done.
	once.Do(func() {
		// A. Load Config
		// We ignore the error here because in PRODUCTION (Docker/Kubernetes),
		// there is no .env file; variables are injected directly into the OS, so nothing will be loaded.
		_ = godotenv.Load()

		addr := os.Getenv("VALKEY_ADDR")
		if addr == "" {
			addr = "localhost:6379" // Default fallback
		}

		dbStr := os.Getenv("VALKEY_DB")
		db := 0
		if dbStr != "" {
			var err error
			db, err = strconv.Atoi(dbStr)
			if err != nil {
				// Fail Fast Principle: Return immediately on invalid config rather than proceeding with a bad state.
				initErr = fmt.Errorf("invalid VALKEY_DB: %w", err)
				return
			}
		}

		// B. Initialize Connection
		fmt.Println("⚡ Initializing Redis Connection (Singleton Creation)...")
		rdb := redis.NewClient(&redis.Options{
			Addr: addr,
			DB:   db,
		})

		// Fail Fast Principle: Verify connection health at startup. Better to crash immediately than serve broken requests silently.
		// C. Verify Connection (Fail Fast)
		// PING ensures the server is actually reachable.
		if _, err := rdb.Ping(context.Background()).Result(); err != nil {
			initErr = fmt.Errorf("failed to connect to valkey at %s: %w", addr, err)
			return
		}

		// Assign to the global variable
		redisInstance = rdb
	})

	// 3. Return the stored instance
	// If initErr is set (from the `once` block), we return it.
	if initErr != nil {
		return nil, initErr
	}
	return redisInstance, nil
}
