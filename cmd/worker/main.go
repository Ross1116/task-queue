package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

const (
	rabbitMQURLKey = "RABBITMQ_URL"
	databaseURLKey = "DATABASE_URL"
	taskQueueName  = "task_queue"
)

type TaskMessage struct {
	TaskID string `json:"task_id"`
	Input  string `json:"input"`
}

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

	err = ch.Qos(
		1,
		0,
		false,
	)
	failOnError(err, "Failed to set QoS")
	log.Println("INFO: QoS Prefetch Count set to 1")

	msgs, err := ch.Consume(
		q.Name, "", false, false, false, false, nil,
	)
	failOnError(err, fmt.Sprintf("Failed to register a consumer for queue '%s'", q.Name))
	log.Printf("INFO: Consumer registered. Waiting for messages on queue '%s'. To exit press CTRL+C", q.Name)

	forever := make(chan bool)

	go func(db *pgxpool.Pool) {
		for d := range msgs {
			log.Printf("==> Received message (delivery tag %d): %s", d.DeliveryTag, d.Body)

			var taskMsg TaskMessage
			err := json.Unmarshal(d.Body, &taskMsg)
			if err != nil {
				log.Printf("ERROR: Failed to parse message JSON (%s): %v", d.Body, err)
				continue
			}
			log.Printf("INFO: Processing task %s (Input: %s)", taskMsg.TaskID, taskMsg.Input)

			err = updateTaskStatus(db, taskMsg.TaskID, "RUNNING", "")
			if err != nil {
				log.Printf("ERROR: Failed to update task %s status to RUNNING: %v", taskMsg.TaskID, err)
				continue
			}
			log.Printf("INFO: Task %s status updated to RUNNING", taskMsg.TaskID)

			processingTime := 5 * time.Second
			log.Printf("INFO: Task %s simulating work for %v...", taskMsg.TaskID, processingTime)
			time.Sleep(processingTime)
			log.Printf("INFO: Task %s work simulation complete.", taskMsg.TaskID)

			simulatedResult := fmt.Sprintf("Processed input: '%s' successfully after %v", taskMsg.Input, processingTime)
			err = updateTaskStatus(db, taskMsg.TaskID, "COMPLETED", simulatedResult)
			if err != nil {
				log.Printf("ERROR: Failed to update task %s status to COMPLETED: %v", taskMsg.TaskID, err)
				continue
			}
			log.Printf("INFO: Task %s status updated to COMPLETED", taskMsg.TaskID)

			log.Printf("INFO: Acknowledging message for task %s (delivery tag %d)", taskMsg.TaskID, d.DeliveryTag)
			err = d.Ack(false)
			if err != nil {
				log.Printf("ERROR: Failed to acknowledge message for task %s (delivery tag %d): %v", taskMsg.TaskID, d.DeliveryTag, err)
			} else {
				log.Printf("INFO: Message acknowledged successfully for task %s.", taskMsg.TaskID)
			}

		}
		log.Println("INFO: RabbitMQ consumption channel closed. Exiting processing goroutine.")
		close(forever)
	}(dbpool)

	<-forever
	log.Println("INFO: Worker shutting down.")
}

func updateTaskStatus(db *pgxpool.Pool, taskID string, status string, result string) error {
	var sql string
	var args []any

	if status == "COMPLETED" && result != "" {
		sql = `UPDATE tasks SET status = $1, result = $2, updated_at = NOW() WHERE task_id = $3`
		args = []any{status, result, taskID}
	} else {
		sql = `UPDATE tasks SET status = $1, updated_at = NOW() WHERE task_id = $2`
		args = []any{status, taskID}
	}

	cmdTag, err := db.Exec(context.Background(), sql, args...)
	if err != nil {
		return fmt.Errorf("database exec error: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		log.Printf("WARN: Attempted to update status for task %s, but no rows were affected.", taskID)
	}

	return nil
}

