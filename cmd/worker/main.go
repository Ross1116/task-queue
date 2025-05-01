package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

type Acknowledger interface {
	Ack(multiple bool) error
	Nack(multiple bool, requeue bool) error
}

var sleepFunc = time.Sleep

type TaskMessage struct {
	TaskID string `json:"task_id"`
	Input  string `json:"input"`
}

const (
	rabbitMQURLKey = "RABBITMQ_URL"
	databaseURLKey = "DATABASE_URL"
	taskQueueName  = "task_queue"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	if fallback != "" {
		log.Printf("INFO: Environment variable %s not set. Using default: %s", key, fallback)
	} else {
		log.Printf("INFO: Environment variable %s not set. No default provided.", key)
	}
	return fallback
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("INFO: No .env file found, proceeding with environment variables or defaults.")
	}

	rabbitMQURL := getEnv(rabbitMQURLKey, "amqp://guest:guest@localhost:5672/")
	databaseURL := getEnv(databaseURLKey, "")
	if databaseURL == "" {
		log.Fatalf("FATAL: %s environment variable not set.", databaseURLKey)
	}

	log.Printf("INFO: Worker starting...")

	dbpool, err := pgxpool.Connect(context.Background(), databaseURL)
	failOnError(err, "Failed to connect to PostgreSQL database")
	defer dbpool.Close()
	log.Println("INFO: Successfully connected to PostgreSQL.")

	conn, err := amqp.Dial(rabbitMQURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()
	log.Println("INFO: Successfully connected to RabbitMQ")

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()
	log.Println("INFO: Successfully opened a channel")

	q, err := ch.QueueDeclare(
		taskQueueName, true, false, false, false, nil,
	)
	failOnError(err, fmt.Sprintf("Failed to declare queue '%s'", taskQueueName))
	log.Printf("INFO: Queue '%s' declared successfully", q.Name)

	prefetchCount := 1
	err = ch.Qos(prefetchCount, 0, false)
	failOnError(err, "Failed to set QoS")
	log.Printf("INFO: QoS Prefetch Count set to %d", prefetchCount)

	msgs, err := ch.Consume(
		q.Name, "", false, false, false, false, nil,
	)
	failOnError(err, fmt.Sprintf("Failed to register a consumer for queue '%s'", q.Name))
	log.Printf("INFO: Consumer registered. Waiting for messages on queue '%s'. To exit press CTRL+C", q.Name)

	forever := make(chan bool)

	go func(db DBTX) {
		for d := range msgs {
			err := processMessage(db, d.Body, &d)
			if err != nil {
				log.Printf("ERROR: Failed to process message (delivery tag %d): %v. Message will be Nacked.", d.DeliveryTag, err)
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
		log.Println("INFO: RabbitMQ consumption channel closed. Exiting processing goroutine.")
		close(forever)
	}(dbpool)

	<-forever
	log.Println("INFO: Worker shutting down.")
}

func processMessage(db DBTX, body []byte, ack Acknowledger) error {
	log.Printf("==> Processing raw message: %s", body)

	var taskMsg TaskMessage
	err := json.Unmarshal(body, &taskMsg)
	if err != nil {
		log.Printf("ERROR: Failed to parse message JSON (%s): %v", body, err)
		return fmt.Errorf("JSON unmarshal error: %w", err)
	}
	log.Printf("INFO: Processing task %s (Input: %s)", taskMsg.TaskID, taskMsg.Input)

	err = updateTaskStatus(db, taskMsg.TaskID, "RUNNING", "")
	if err != nil {
		log.Printf("ERROR: Failed to update task %s status to RUNNING: %v", taskMsg.TaskID, err)
		return fmt.Errorf("db update RUNNING error: %w", err)
	}
	log.Printf("INFO: Task %s status updated to RUNNING", taskMsg.TaskID)

	processingTime := 5 * time.Second
	log.Printf("INFO: Task %s simulating work for %v...", taskMsg.TaskID, processingTime)
	sleepFunc(processingTime)
	log.Printf("INFO: Task %s work simulation complete.", taskMsg.TaskID)
	simulatedResult := fmt.Sprintf("Processed input: '%s' successfully after %v", taskMsg.Input, processingTime)

	err = updateTaskStatus(db, taskMsg.TaskID, "COMPLETED", simulatedResult)
	if err != nil {
		log.Printf("ERROR: Failed to update task %s status to COMPLETED: %v", taskMsg.TaskID, err)
		return fmt.Errorf("db update COMPLETED error: %w", err)
	}
	log.Printf("INFO: Task %s status updated to COMPLETED with result.", taskMsg.TaskID)

	log.Printf("INFO: Task %s processed successfully.", taskMsg.TaskID)
	return nil
}

func updateTaskStatus(db DBTX, taskID string, status string, result string) error {
	var sql string
	var args []interface{}

	if status == "COMPLETED" {
		sql = `UPDATE tasks SET status = $1, result = $2, updated_at = NOW() WHERE task_id = $3`
		args = []interface{}{status, result, taskID}
	} else if status == "FAILED" {
		sql = `UPDATE tasks SET status = $1, result = $2, updated_at = NOW() WHERE task_id = $3`
		args = []interface{}{status, result, taskID}
	} else {
		sql = `UPDATE tasks SET status = $1, updated_at = NOW() WHERE task_id = $2`
		args = []interface{}{status, taskID}
	}

	cmdTag, err := db.Exec(context.Background(), sql, args...)
	if err != nil {
		return fmt.Errorf("database exec error updating task %s to %s: %w", taskID, status, err)
	}

	if cmdTag.RowsAffected() == 0 {
		log.Printf("WARN: Attempted to update status for task %s to %s, but no rows were affected (task may not exist?).", taskID, status)
	}

	return nil
}

var _ Acknowledger = (*amqp.Delivery)(nil)

