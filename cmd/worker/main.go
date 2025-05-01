package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ross1116/task-queue/internal/task"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

var sleepFunc = time.Sleep

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("INFO: No .env file found, proceeding with environment variables or defaults.")
	}

	rabbitMQURL := task.GetEnv(task.RabbitMQURLKey, "amqp://guest:guest@localhost:5672/")
	databaseURL := task.GetEnv(task.DatabaseURLKey, "")

	log.Printf("INFO: Worker Service starting...")
	log.Printf("INFO: RabbitMQ URL: %s", rabbitMQURL)
	log.Printf("INFO: Database URL: %s", databaseURL)

	if databaseURL == "" {
		log.Fatalf("FATAL: %s environment variable not set.", task.DatabaseURLKey)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbpool, err := pgxpool.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to PostgreSQL database: %v", err)
	}
	defer func() {
		log.Println("INFO: Closing database pool...")
		dbpool.Close()
		log.Println("INFO: Database pool closed.")
	}()
	log.Println("INFO: Successfully connected to PostgreSQL.")

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to RabbitMQ: %v", err)
	}
	defer func() {
		log.Println("INFO: Closing RabbitMQ connection...")
		if err := conn.Close(); err != nil {
			log.Printf("ERROR: Failed to close RabbitMQ connection cleanly: %v", err)
		} else {
			log.Println("INFO: RabbitMQ connection closed.")
		}
	}()
	log.Println("INFO: Successfully connected to RabbitMQ")

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("FATAL: Failed to open a channel: %v", err)
	}
	log.Println("INFO: Successfully opened a channel")

	q, err := ch.QueueDeclare(
		task.TaskQueueName, true, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("FATAL: Failed to declare queue '%s': %v", task.TaskQueueName, err)
	}
	log.Printf("INFO: Queue '%s' declared successfully", q.Name)

	prefetchCount := 1
	err = ch.Qos(prefetchCount, 0, false)
	if err != nil {
		log.Fatalf("FATAL: Failed to set QoS: %v", err)
	}
	log.Printf("INFO: QoS Prefetch Count set to %d", prefetchCount)

	msgs, err := ch.Consume(
		q.Name, "", false, false, false, false, nil,
	)
	if err != nil {
		log.Fatalf("FATAL: Failed to register a consumer for queue '%s': %v", q.Name, err)
	}
	log.Printf("INFO: Consumer registered. Waiting for messages on queue '%s'. Press CTRL+C to exit.", q.Name)

	processingDone := make(chan struct{})
	go func(db task.DBTX) {
		defer close(processingDone)
		for {
			select {
			case <-ctx.Done():
				log.Println("INFO: Shutdown signal received, stopping message consumption...")
				return
			case d, ok := <-msgs:
				if !ok {
					log.Println("INFO: RabbitMQ consumption channel closed by server.")
					return
				}
				err := processMessage(db, d.Body, &d)
				if err != nil {
					log.Printf("ERROR: Failed to process message (delivery tag %d): %v. Message will be Nacked (no requeue).", d.DeliveryTag, err)
					nackErr := d.Nack(false, false)
					if nackErr != nil {
						log.Printf("ERROR: Failed to Nack message (delivery tag %d): %v", d.DeliveryTag, nackErr)
					}
				} else {
					log.Printf("INFO: Acknowledging message (delivery tag %d)", d.DeliveryTag)
					ackErr := d.Ack(false)
					if ackErr != nil {
						log.Printf("ERROR: Failed to acknowledge message (delivery tag %d): %v", d.DeliveryTag, ackErr)
					} else {
						log.Printf("INFO: Message acknowledged successfully (delivery tag %d).", d.DeliveryTag)
					}
				}
			}
		}
	}(dbpool)

	select {
	case <-ctx.Done():
		log.Println("INFO: Waiting for processing goroutine to finish...")
		<-processingDone
		log.Println("INFO: Processing goroutine finished.")
	case <-processingDone:
		log.Println("WARN: Processing goroutine exited unexpectedly.")
	}

	log.Println("INFO: Worker Service shut down complete.")
}

func processMessage(db task.DBTX, body []byte, ack task.Acknowledger) error {
	var taskMsg task.TaskMessage
	err := json.Unmarshal(body, &taskMsg)
	if err != nil {
		log.Printf("ERROR: processMessage - Failed to parse message JSON: %v", err)
		return fmt.Errorf("JSON unmarshal error: %w", err)
	}
	log.Printf("INFO: processMessage - Processing task %s (Input: %s)", taskMsg.TaskID, taskMsg.Input)

	ctx := context.Background()
	err = updateTaskStatus(ctx, db, taskMsg.TaskID, "RUNNING", "")
	if err != nil {
		log.Printf("ERROR: processMessage - Failed to update task %s status to RUNNING: %v", taskMsg.TaskID, err)
		return fmt.Errorf("db update RUNNING error: %w", err)
	}
	log.Printf("INFO: processMessage - Task %s status updated to RUNNING", taskMsg.TaskID)

	processingTime := 5 * time.Second
	log.Printf("INFO: processMessage - Task %s simulating work for %v...", taskMsg.TaskID, processingTime)
	sleepFunc(processingTime)
	log.Printf("INFO: processMessage - Task %s work simulation complete.", taskMsg.TaskID)
	simulatedResult := fmt.Sprintf("Processed input: '%s' successfully after %v", taskMsg.Input, processingTime)

	err = updateTaskStatus(ctx, db, taskMsg.TaskID, "COMPLETED", simulatedResult)
	if err != nil {
		log.Printf("ERROR: processMessage - Failed to update task %s status to COMPLETED: %v", taskMsg.TaskID, err)
		return fmt.Errorf("db update COMPLETED error: %w", err)
	}
	log.Printf("INFO: processMessage - Task %s status updated to COMPLETED with result.", taskMsg.TaskID)

	log.Printf("INFO: processMessage - Task %s processed successfully.", taskMsg.TaskID)
	return nil
}

func updateTaskStatus(ctx context.Context, db task.DBTX, taskID string, status string, result string) error {
	var sql string
	var args []interface{}

	switch status {
	case "COMPLETED":
		sql = `UPDATE tasks SET status = $1, result = $2, updated_at = NOW() WHERE task_id = $3`
		args = []interface{}{status, result, taskID}
	case "FAILED":
		sql = `UPDATE tasks SET status = $1, result = $2, updated_at = NOW() WHERE task_id = $3`
		args = []interface{}{status, result, taskID}
	default:
		sql = `UPDATE tasks SET status = $1, updated_at = NOW() WHERE task_id = $2`
		args = []interface{}{status, taskID}
	}

	cmdTag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("database exec error updating task %s to %s: %w", taskID, status, err)
	}

	if cmdTag.RowsAffected() == 0 {
		log.Printf("WARN: updateTaskStatus - Attempted to update status for task %s to %s, but no rows were affected (task may not exist?).", taskID, status)
	}

	return nil
}

var _ task.Acknowledger = (*amqp.Delivery)(nil)

