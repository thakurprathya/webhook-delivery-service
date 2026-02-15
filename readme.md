# 🚀 Resilient Webhook Delivery Service

A scalable, fault-tolerant webhook delivery system built in **Go (Golang)** and **Valkey (Redis)**. This project demonstrates how to handle high-throughput HTTP requests with rate limiting, asynchronous processing, and exponential backoff retries—mirroring the architecture of systems like Stripe or GitHub webhooks.

---

## 🏗 Architecture & Design

This system is designed using the **Producer-Consumer** pattern to decouple ingestion from processing, ensuring high availability even during traffic spikes.

### **Core Components**

1.  **API (The Producer):**
    * **Role:** Acts as the entry point. Validates requests, enforces rate limits, and pushes tasks to the queue.
    * **Scalability:** Stateless. Can be horizontally scaled behind a load balancer.
    * **Design Pattern:** **RESTful API** with Strict Schema Validation (DTOs).

2.  **Valkey / Redis (The Broker):**
    * **Role:** Serves as the high-performance backbone for:
        * **Rate Limiting Counters:** (Atomic INCR operations).
        * **Task Queue:** (Lists for FIFO processing).
        * **Retry Scheduling:** (Sorted Sets for delayed execution).
    * **Why Valkey?** In-memory speed (sub-millisecond latency) prevents database bottlenecks.

3.  **Worker (The Consumer):**
    * **Role:** Polls the queue, executes the webhook HTTP call, and handles failures.
    * **Scalability:** Tunable concurrency. You can run 5, 50, or 500 worker goroutines depending on load.
    * **Reliability:** Implements **Exponential Backoff** to prevent thundering herd problems on failing destinations.

---

## 🛠️ Tech Stack & Concepts

| Concept | Implementation | Why? |
| :--- | :--- | :--- |
| **Language** | Golang 1.23+ | High concurrency, strict typing, and compiled performance. |
| **Database** | Valkey (Redis Fork) | Ultra-fast in-memory storage for queues and counters. |
| **Concurrency** | Goroutines & Channels | Efficiently managing thousands of concurrent worker threads. |
| **Architecture** | Hexagonal / Clean | Separation of concerns (API vs. Logic vs. Infrastructure). |
| **Rate Limiting** | Token Bucket / Fixed Window | Protects the system from abuse and overflow. |
| **Resiliency** | Exponential Backoff | Prevents overwhelming a down server with retries. |
| **Shutdown** | Graceful Shutdown | Ensures no data loss when the server restarts or deploys. |

---

## 📂 Project Structure

We follow the **Standard Go Project Layout** to ensure maintainability and modularity.

```text
webhook-delivery/
├── cmd/
│   ├── api/            # Entry point for the HTTP Server
│   │   └── main.go
│   └── worker/         # Entry point for the Background Worker
│       └── main.go
├── internal/
│   ├── backoff/        # Strategy Pattern: Retry logic (Exponential, Linear)
│   ├── platform/       # Singleton Pattern: Database connections (Redis)
│   ├── queue/          # Adapter Pattern: Queue operations (Enqueue/Dequeue)
│   ├── ratelimit/      # Strategy Pattern: Rate limiting logic
│   └── worker/         # Command Pattern: Task execution logic
├── .env                # Configuration (Git-ignored)
├── go.mod              # Dependency definitions
└── README.md           # This file
```

---

# ⚙️ Design Patterns Implemented

## Singleton Pattern
- Used in `internal/platform/redis.go`
- Ensures single Redis connection pool instance

## Strategy Pattern
- `internal/ratelimit` → FixedWindow / TokenBucket
- `internal/backoff` → Exponential / Linear
- Allows runtime algorithm swapping

## Adapter Pattern
- `internal/queue`
- Uses generic Queue interface
- Can replace Redis with Kafka/RabbitMQ easily

## Command Pattern
- `internal/worker`
- Encapsulates webhook request as `Task`
- Contains payload + retry metadata

## Factory Pattern
- `NewRedisQueue`
- `NewProcessor`
- Handles dependency injection & complex object creation

---

# 🚀 Getting Started

## Prerequisites
- Go 1.21+
- Docker

---

## Step 1: Start Infrastructure

```bash
docker run -d --name my-valkey -p 6379:6379 valkey/valkey
```

---

## Step 2: Configuration

Create `.env` file:

```ini
VALKEY_ADDR=localhost:6379
VALKEY_DB=0
```

---

## Step 3: Run API

```bash
go run cmd/api/main.go
```

Expected:
```
🚀 API Server running on port 8080
```

---

## Step 4: Run Worker

```bash
go run cmd/worker/main.go
```

Expected:
```
🚀 Starting 5 Workers...
```

---

# 🧪 Testing

## 1️⃣ Success Case

```bash
curl -X POST http://localhost:8080/send \
     -H "Content-Type: application/json" \
     -d '{"user_id": "user_123", "data": {"event": "order_created", "amount": 50}}'
```

Expected:
- API → Request Accepted
- Worker → Success log

---

## 2️⃣ Rate Limiting Test

```bash
for i in {1..6}; do curl -X POST http://localhost:8080/send \
     -H "Content-Type: application/json" \
     -d '{"user_id": "spammer", "data": "spam"}' ; done
```

Expected:
```
429 Too Many Requests
```

---

## 3️⃣ Retry & Backoff Test

Sample Logs:

```
Processing Task (Attempt 1)
Failed. Retrying in 1s (Attempt 2)
Scheduler: Moving task back to queue
Processing Task (Attempt 2)
Success
```

---

# 🔮 Future Roadmap

## Distributed Streaming
- Replace Redis Lists with Kafka or RabbitMQ
- Strict ordering + disk durability

## Distributed Locking
- Implement Redis Redlock
- Ensure single scheduler in multi-replica deployment

## Dead Letter Queue (DLQ)
- Tasks failing 5 times move to DLQ
- Add manual replay UI

## Observability
- Prometheus → Queue Depth + Worker Latency
- OpenTelemetry → Distributed tracing

---

# 📈 Scaling Vision

Designed to scale toward **1M+ RPS** with:
- Horizontal API scaling
- Worker concurrency tuning
- Distributed messaging
- Observability + metrics
- Failure isolation

---