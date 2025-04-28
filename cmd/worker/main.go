package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

const (
	rabbitMQURLKey = "RABBITMQ_URL"
	taskQueueName  = "task_queue"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("INFO: No .env file found, proceeding with environment variables or defaults.")
	}

	rabbitMQURL := getEnv(rabbitMQURLKey, "amqp://guest:guest@localhost:5672/")
	log.Printf("INFO: Worker starting. Attempting to connect to RabbitMQ at %s", rabbitMQURL)

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()
	log.Println("INFO: Successfully connected to RabbitMQ")

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer ch.Close()
	log.Println("INFO: Successfully opened a channel")

	q, err := ch.QueueDeclare(
		taskQueueName, // name
		true,          // durable (MUST match publisher)
		false,         // delete when unused (auto-delete)
		false,         // exclusive
		false,         // no-wait
		nil,           // args
	)
	if err != nil {
		log.Fatalf("Failed to declare queue '%s' : %v", taskQueueName, err)
	}
	log.Printf("INFO: Consumer registered successfully. Waiting for messages on queue '%s'. To exit press CTRL+C", q.Name)

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer tag (empty means auto-generate)
		false,  // auto-ack (IMPORTANT: set to false for manual ack)
		false,  // exclusive
		false,  // no-local (not relevant for basic queues)
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer for queue '%s': %v", q.Name, err)
	}
	log.Printf("INFO: Consumer registered successfully. Waiting for messages on queue '%s'. To exit press CTRL+C", q.Name)

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Printf("Received message: %s", d.Body)
		}
		log.Println("INFO: RabbitMQ consumption channel closed. Exiting processing goroutine.")
		close(forever)
	}()

	<-forever
	log.Println("INFO: Worker shutting down.")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	log.Printf("Using default value for %s: %s", key, fallback)
	return fallback
}
