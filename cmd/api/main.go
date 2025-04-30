package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

type QueuePublisher interface {
	Publish(taskID string, input string) error
	DeclareQueue() error
	Close() error
}

type TaskStatusResponse struct {
	TaskID    string         `json:"task_id"`
	Status    string         `json:"status"`
	Input     string         `json:"input,omitempty"`
	Result    sql.NullString `json:"result"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CreateTaskRequest struct {
	Input string `json:"input" binding:"required"`
}

type TaskMessage struct {
	TaskID string `json:"task_id"`
	Input  string `json:"input"`
}

type CreateTaskResponse struct {
	TaskID string `json:"task_id"`
}

type RabbitMQPublisher struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
	url     string
}

func NewRabbitMQPublisher(url string) (*RabbitMQPublisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ at %s: %w", url, err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	publisher := &RabbitMQPublisher{
		conn:    conn,
		channel: ch,
		url:     url,
	}

	if err := publisher.DeclareQueue(); err != nil {
		publisher.Close()
		return nil, err
	}

	log.Printf("Successfully connected to RabbitMQ and declared queue '%s'", taskQueueName)
	return publisher, nil
}

func (p *RabbitMQPublisher) DeclareQueue() error {
	q, err := p.channel.QueueDeclare(
		taskQueueName, true, false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue '%s': %w", taskQueueName, err)
	}
	p.queue = q
	return nil
}

func (p *RabbitMQPublisher) Publish(taskID string, input string) error {
	message := TaskMessage{
		TaskID: taskID,
		Input:  input,
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal task message: %w", err)
	}

	err = p.channel.Publish(
		"", p.queue.Name, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		})
	if err != nil {
		log.Printf("Failed to publish message, potential connection issue: %v", err)
		return fmt.Errorf("failed to publish message to queue '%s': %w", p.queue.Name, err)
	}
	return nil
}

func (p *RabbitMQPublisher) Close() error {
	var errChan, errConn error
	if p.channel != nil {
		errChan = p.channel.Close()
		log.Println("RabbitMQ channel closed.")
	}
	if p.conn != nil {
		errConn = p.conn.Close()
		log.Println("RabbitMQ connection closed.")
	}
	if errChan != nil {
		return fmt.Errorf("error closing channel: %w", errChan)
	}
	if errConn != nil {
		return fmt.Errorf("error closing connection: %w", errConn)
	}
	return nil
}

type Application struct {
	DB        DBTX
	Publisher QueuePublisher
}

const (
	rabbitMQURLKey = "RABBITMQ_URL"
	databaseURLKey = "DATABASE_URL"
	portKey        = "PORT"
	taskQueueName  = "task_queue"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, reading environment variables directly")
	}

	rabbitMQURL := getEnv(rabbitMQURLKey, "amqp://guest:guest@localhost:5672/")
	databaseURL := getEnv(databaseURLKey, "")
	port := getEnv(portKey, "8080")

	log.Printf("RabbitMQ URL: %s", rabbitMQURL)

	if databaseURL == "" {
		log.Fatalf("FATAL: %s environment variable not set.", databaseURLKey)
	}

	dbpool, err := pgxpool.Connect(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer dbpool.Close()
	log.Println("Successfully connected to PostgreSQL.")

	publisher, err := NewRabbitMQPublisher(rabbitMQURL)
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ publisher: %v", err)
	}
	defer publisher.Close()

	app := &Application{
		DB:        dbpool,
		Publisher: publisher,
	}

	router := gin.Default()

	router.POST("/tasks", app.createTaskHandler)
	router.GET("/tasks/:id/status", app.getTaskStatusHandler)

	serverAddr := ":" + port
	log.Printf("Starting API server on %s", serverAddr)
	if err := router.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	if fallback != "" {
		log.Printf("Using default value for %s: %s", key, fallback)
	}
	return fallback
}

func (app *Application) createTaskHandler(c *gin.Context) {
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	taskID := uuid.New().String()
	log.Printf("Generated TaskID: %s for input: %s", taskID, req.Input)

	status := "PENDING"
	insertSQL := `INSERT INTO tasks (task_id, status, task_input) VALUES ($1, $2, $3)`
	_, err := app.DB.Exec(context.Background(), insertSQL, taskID, status, req.Input)
	if err != nil {
		log.Printf("Error inserting task %s into database: %v", taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task record"})
		return
	}
	log.Printf("Successfully inserted task %s into database with status PENDING", taskID)

	err = app.Publisher.Publish(taskID, req.Input)
	if err != nil {
		log.Printf("Error publishing task %s to RabbitMQ: %v", taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Task created but failed to queue for processing"})
		return
	}
	log.Printf("Successfully published task %s to RabbitMQ queue '%s'", taskID, taskQueueName)

	c.JSON(http.StatusAccepted, CreateTaskResponse{TaskID: taskID})
}

func (app *Application) getTaskStatusHandler(c *gin.Context) {
	taskIDParam := c.Param("id")
	log.Printf("Received GET /tasks/%s/status request", taskIDParam)

	_, err := uuid.Parse(taskIDParam)
	if err != nil {
		log.Printf("Invalid Task ID format: %s, error: %v", taskIDParam, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Task ID format"})
		return
	}

	selectSQL := `
        SELECT status, task_input, result, created_at, updated_at
        FROM tasks
        WHERE task_id = $1
    `
	var status string
	var input string
	var result sql.NullString
	var createdAt time.Time
	var updatedAt time.Time

	ctx := c.Request.Context()
	err = app.DB.QueryRow(ctx, selectSQL, taskIDParam).Scan(
		&status,
		&input,
		&result,
		&createdAt,
		&updatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Printf("Task ID %s not found in database", taskIDParam)
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		} else {
			log.Printf("Error querying task status for ID %s: %v", taskIDParam, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve task status"})
		}
		return
	}

	response := TaskStatusResponse{
		TaskID:    taskIDParam,
		Status:    status,
		Input:     input,
		Result:    result,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	log.Printf("Successfully retrieved status for task %s: Status=%s", taskIDParam, status)
	c.JSON(http.StatusOK, response)
}

