# Task-Queue (Go, RabbitMQ, PostgreSQL)

A compact reference implementation that shows how to:

1. accept work over a REST API (Gin)  
2. persist a job record (PostgreSQL)  
3. publish and consume messages (RabbitMQ)  
4. process work asynchronously in a separate worker  
5. query job status and result  

The codebase is intentionally small but follows production-style patterns: clear layout, graceful shutdown, interface-driven tests, and Docker-first local development.

---

## Quick start (Docker Compose)

```bash
git clone https://github.com/Ross1116/task-queue.git
cd task-queue
docker compose up --build
```

When the stack is running:

| Component | URL / Port                      | Default credentials |
|-----------|---------------------------------|---------------------|
| REST API  | http://localhost:8080           | –                   |
| RabbitMQ  | http://localhost:15672          | guest / guest       |
| PostgreSQL| localhost:5432                 | your_db_user / your_strong_password |
| Frontend  | http://localhost:3000 (run `npm i && npm run dev` in `frontend/`) | – |

Example:

```bash
# enqueue
id=$(curl -s -X POST :8080/tasks \
        -H 'Content-Type: application/json' \
        -d '{"input":"hello world"}' | jq -r .task_id)

# poll
curl :8080/tasks/$id/status
```

---

## API reference

### Endpoints

| Method | Path               | Purpose                    |
|--------|--------------------|----------------------------|
| POST   | /tasks             | Create and enqueue a task  |
| GET    | /tasks/{id}/status | Fetch status and result    |
| GET    | /health            | Liveness probe             |

### Payloads

#### Create task

Request body

```jsonc
{
  "input": "any string"   // required
}
```

Response `202 Accepted`

```json
{
  "task_id": "a3e2c3c4-..."   // UUID
}
```

#### Task status

Successful response `200 OK`

```json
{
  "task_id": "a3e2c3c4-...",
  "status": "COMPLETED",          // PENDING | RUNNING | COMPLETED | FAILED
  "input": "any string",
  "result": "Processed input: ...", // null until completed/failed
  "created_at": "2024-05-01T12:34:56Z",
  "updated_at": "2024-05-01T12:35:01Z"
}
```

If the task is unknown, the service returns `404 Not Found`.

---

## Configuration

Environment variables (read directly or via `.env`):

| Variable          | Default in Compose                                              | Used by |
|-------------------|-----------------------------------------------------------------|---------|
| `DATABASE_URL`    | `postgres://your_db_user:your_strong_password@local_postgres:5432/your_db_name?sslmode=disable` | API, Worker |
| `RABBITMQ_URL`    | `amqp://guest:guest@local_rabbitmq:5672/`                       | API, Worker |
| `PORT`            | `8080`                                                          | API     |
| `ALLOWED_ORIGINS` | `http://localhost:3000,http://127.0.0.1:3000`                   | API     |

---

## Project layout

```
cmd/
  api/       # REST service
  worker/    # background processor
internal/
  task/      # domain types, interfaces, helpers
frontend/    # Next.js UI (optional)
docker-compose.yml
```

---

## Implementation notes

* Infrastructure is abstracted behind interfaces (`QueuePublisher`, `DBTX`, `Acknowledger`) to simplify unit and integration testing.  
* Both services implement graceful shutdown with `context` and `signal.NotifyContext`.  
* Worker sets `prefetch=1` on RabbitMQ for back-pressure and at-least-once delivery.  
* Integration tests (`*_integration_test.go`) spin up containerised dependencies to verify the complete flow.
