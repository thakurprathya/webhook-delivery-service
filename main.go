package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func main() {
	// 1. Initialize the Client
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379", // The address of our Docker container
	})

	// 2. Test Connection
	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Connected to Valkey:", pong)

	// 3. Basic "Set" and "Get" (Learning the Key-Value Concept)
	err = rdb.Set(ctx, "firstKey", "firstValue", 0).Err()
	if err != nil {
		panic(err)
	}

	val, _ := rdb.Get(ctx, "firstKey").Result()
	fmt.Println("firstKey : ", val)
}
