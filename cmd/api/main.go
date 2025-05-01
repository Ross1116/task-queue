package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ross1116/task-queue/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
)

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

	log.Printf("INFO: Successfully connected to RabbitMQ and declared queue '%s'", task.TaskQueueName)
	return publisher, nil
}

func (p *RabbitMQPublisher) DeclareQueue() error {
	q, err := p.channel.QueueDeclare(
		task.TaskQueueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue '%s': %w", task.TaskQueueName, err)
	}
	p.queue = q
	return nil
}

func (p *RabbitMQPublisher) Publish(taskID string, input string) error {
	message := task.TaskMessage{
		TaskID: taskID,
		Input:  input,
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal task message: %w", err)
	}

	err = p.channel.Publish(
		"",
		p.queue.Name,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		})
	if err != nil {
		log.Printf("ERROR: Failed to publish message for task %s: %v", taskID, err)
		return fmt.Errorf("failed to publish message to queue '%s': %w", p.queue.Name, err)
	}
	return nil
}

func (p *RabbitMQPublisher) Close() error {
	var errChan, errConn error
	if p.channel != nil {
		log.Println("INFO: Closing RabbitMQ channel...")
		errChan = p.channel.Close()
	}
	if p.conn != nil {
		log.Println("INFO: Closing RabbitMQ connection...")
		errConn = p.conn.Close()
	}
	if errChan != nil {
		return fmt.Errorf("error closing RabbitMQ channel: %w", errChan)
	}
	if errConn != nil {
		return fmt.Errorf("error closing RabbitMQ connection: %w", errConn)
	}
	log.Println("INFO: RabbitMQ resources closed.")
	return nil
}

type Application struct {
	DB        task.DBTX
	Publisher task.QueuePublisher
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("INFO: No .env file found, reading environment variables directly.")
	}

	rabbitMQURL := task.GetEnv(task.RabbitMQURLKey, "amqp://guest:guest@localhost:5672/")
	databaseURL := task.GetEnv(task.DatabaseURLKey, "")
	port := task.GetEnv(task.PortKey, "8080")

	log.Printf("INFO: API Service starting...")
	log.Printf("INFO: RabbitMQ URL: %s", rabbitMQURL)
	log.Printf("INFO: Database URL: %s", databaseURL)
	log.Printf("INFO: Port: %s", port)

	if databaseURL == "" {
		log.Fatalf("FATAL: %s environment variable not set.", task.DatabaseURLKey)
	}

	dbCtx := context.Background()
	dbpool, err := pgxpool.Connect(dbCtx, databaseURL)
	if err != nil {
		log.Fatalf("FATAL: Unable to connect to database: %v", err)
	}
	defer func() {
		log.Println("INFO: Closing database pool...")
		dbpool.Close()
		log.Println("INFO: Database pool closed.")
	}()
	log.Println("INFO: Successfully connected to PostgreSQL.")

	publisher, err := NewRabbitMQPublisher(rabbitMQURL)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize RabbitMQ publisher: %v", err)
	}
	defer publisher.Close()

	app := &Application{
		DB:        dbpool,
		Publisher: publisher,
	}

	router := gin.Default()

	router.POST("/tasks", app.createTaskHandler)
	router.GET("/tasks/:id/status", app.getTaskStatusHandler)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	serverAddr := ":" + port
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("INFO: Shutdown signal received, initiating graceful shutdown...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("FATAL: Server forced to shutdown: %v", err)
		}
		log.Println("INFO: HTTP server gracefully stopped.")
	}()

	log.Printf("INFO: Starting API server on %s", serverAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("FATAL: Failed to start server: %v", err)
	}

	log.Println("INFO: API Service shut down complete.")

}

func (app *Application) createTaskHandler(c *gin.Context) {
	var req task.CreateTaskRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("WARN: createTaskHandler - Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	taskID := uuid.NewString()
	log.Printf("INFO: createTaskHandler - Generated TaskID: %s for input: %s", taskID, req.Input)

	status := "PENDING"
	insertSQL := `INSERT INTO tasks (task_id, status, task_input) VALUES ($1, $2, $3)`
	_, err := app.DB.Exec(c.Request.Context(), insertSQL, taskID, status, req.Input)
	if err != nil {
		log.Printf("ERROR: createTaskHandler - Error inserting task %s into database: %v", taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task record"})
		return
	}
	log.Printf("INFO: createTaskHandler - Inserted task %s with status PENDING", taskID)

	err = app.Publisher.Publish(taskID, req.Input)
	if err != nil {
		log.Printf("ERROR: createTaskHandler - Failed to publish task %s to queue.", taskID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Task created but failed to queue for processing"})
		return
	}
	log.Printf("INFO: createTaskHandler - Published task %s to queue '%s'", taskID, task.TaskQueueName)

	c.JSON(http.StatusAccepted, task.CreateTaskResponse{TaskID: taskID})
}

func (app *Application) getTaskStatusHandler(c *gin.Context) {
	taskIDParam := c.Param("id")
	log.Printf("INFO: getTaskStatusHandler - Received request for TaskID: %s", taskIDParam)

	_, err := uuid.Parse(taskIDParam)
	if err != nil {
		log.Printf("WARN: getTaskStatusHandler - Invalid Task ID format: %s, error: %v", taskIDParam, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Task ID format"})
		return
	}

	selectSQL := `
        SELECT status, task_input, result, created_at, updated_at
        FROM tasks
        WHERE task_id = $1
    `
	var resp task.TaskStatusResponse
	resp.TaskID = taskIDParam

	err = app.DB.QueryRow(c.Request.Context(), selectSQL, taskIDParam).Scan(
		&resp.Status,
		&resp.Input,
		&resp.Result,
		&resp.CreatedAt,
		&resp.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Printf("INFO: getTaskStatusHandler - Task ID %s not found", taskIDParam)
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		} else {
			log.Printf("ERROR: getTaskStatusHandler - Error querying task status for ID %s: %v", taskIDParam, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve task status"})
		}
		return
	}

	log.Printf("INFO: getTaskStatusHandler - Successfully retrieved status for task %s: Status=%s", taskIDParam, resp.Status)
	c.JSON(http.StatusOK, resp)
}

var _ task.QueuePublisher = (*RabbitMQPublisher)(nil)
