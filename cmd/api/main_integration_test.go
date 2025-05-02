package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Ross1116/task-queue/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testRouter *gin.Engine
	testApp    *Application
	dbPool     *pgxpool.Pool
	publisher  task.QueuePublisher
)

func TestMain(m *testing.M) {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("INFO: Could not load .env from current directory, trying ../.env")
		err = godotenv.Load("../../.env")
		if err != nil {
			fmt.Printf("WARN: Failed to load .env file from ./ or ../: %v. Relying on existing environment variables.\n", err)
		} else {
			fmt.Println("INFO: Successfully loaded .env from ../.env")
		}
	} else {
		fmt.Println("INFO: Successfully loaded .env from current directory")
	}

	dbURL := os.Getenv(task.DatabaseURLKey)
	mqURL := os.Getenv(task.RabbitMQURLKey)

	if dbURL == "" || mqURL == "" {
		fmt.Printf("DEBUG: DATABASE_URL='%s', RABBITMQ_URL='%s'\n", dbURL, mqURL)
		fmt.Println("Skipping API integration tests: DATABASE_URL and/or RABBITMQ_URL not set.")
		return
	}

	dbPool, err = pgxpool.Connect(context.Background(), dbURL)
	if err != nil {
		fmt.Printf("Integration test setup failed: Cannot connect to database (%s): %v\n", task.DatabaseURLKey, err)
		os.Exit(1)
	}

	publisherImpl, err := NewRabbitMQPublisher(mqURL)
	if err != nil {
		fmt.Printf("Integration test setup failed: Cannot connect to RabbitMQ (%s): %v\n", task.RabbitMQURLKey, err)
		if dbPool != nil {
			dbPool.Close()
		}
		os.Exit(1)
	}
	publisher = publisherImpl

	testApp = &Application{
		DB:        dbPool,
		Publisher: publisher,
	}

	gin.SetMode(gin.TestMode)
	testRouter = gin.New()
	testRouter.POST("/tasks", testApp.createTaskHandler)
	testRouter.GET("/tasks/:id/status", testApp.getTaskStatusHandler)
	testRouter.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	fmt.Println("INFO: Running integration tests...")
	exitCode := m.Run()

	fmt.Println("INFO: Closing integration test resources...")
	if publisher != nil {
		if err := publisher.Close(); err != nil {
			fmt.Printf("WARN: Error closing publisher: %v\n", err)
		}
	}
	if dbPool != nil {
		dbPool.Close()
	}
	fmt.Println("INFO: Integration test resources closed.")
	os.Exit(exitCode)
}

func TestTaskLifecycle_Integration(t *testing.T) {
	if testRouter == nil {
		t.Skip("Skipping integration test: Test environment not initialized (check TestMain)")
	}
	require.NotNil(t, dbPool, "Database pool must be initialized for integration tests")

	inputData := fmt.Sprintf("integration_test_api_%d", time.Now().UnixNano())
	createPayload := task.CreateTaskRequest{Input: inputData}
	payloadBytes, _ := json.Marshal(createPayload)

	w := httptest.NewRecorder()
	reqCreate, _ := http.NewRequest(http.MethodPost, "/tasks", bytes.NewBuffer(payloadBytes))
	reqCreate.Header.Set("Content-Type", "application/json")
	testRouter.ServeHTTP(w, reqCreate)

	require.Equal(t, http.StatusAccepted, w.Code, "Expected HTTP 202 Accepted on task creation")

	var createResp task.CreateTaskResponse
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err, "Failed to decode create task response")
	require.NotEmpty(t, createResp.TaskID, "Task ID should not be empty")
	taskID := createResp.TaskID
	t.Logf("Created Task ID: %s", taskID)

	time.Sleep(200 * time.Millisecond)
	wGetInitial := httptest.NewRecorder()
	reqGetInitial, _ := http.NewRequest(http.MethodGet, "/tasks/"+taskID+"/status", nil)
	testRouter.ServeHTTP(wGetInitial, reqGetInitial)

	require.Equal(t, http.StatusOK, wGetInitial.Code, "Expected HTTP 200 OK getting initial status")

	var statusRespInitial task.TaskStatusResponse
	err = json.Unmarshal(wGetInitial.Body.Bytes(), &statusRespInitial)
	require.NoError(t, err, "Failed to decode initial status response")
	assert.Equal(t, taskID, statusRespInitial.TaskID)
	assert.Contains(t, []string{"PENDING", "RUNNING"}, statusRespInitial.Status, "Initial status should be PENDING or RUNNING")
	assert.Equal(t, inputData, statusRespInitial.Input)

	t.Logf("Polling for final status of task %s (requires worker to be running)...", taskID)
	maxWait := 30 * time.Second
	pollInterval := 1 * time.Second
	startTime := time.Now()
	var finalStatusResp task.TaskStatusResponse

	for time.Since(startTime) < maxWait {
		wGetPoll := httptest.NewRecorder()
		reqGetPoll, _ := http.NewRequest(http.MethodGet, "/tasks/"+taskID+"/status", nil)
		testRouter.ServeHTTP(wGetPoll, reqGetPoll)

		if wGetPoll.Code != http.StatusOK {
			if wGetPoll.Code == http.StatusNotFound {
				t.Fatalf("Polling: Task %s not found, assuming failure.", taskID)
			}
			t.Logf("Polling: Received status code %d, waiting...", wGetPoll.Code)
			time.Sleep(pollInterval)
			continue
		}

		err = json.Unmarshal(wGetPoll.Body.Bytes(), &finalStatusResp)
		if err != nil {
			t.Logf("Polling: Error decoding response: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		t.Logf("Polling: Current status = %s", finalStatusResp.Status)
		if finalStatusResp.Status == "COMPLETED" || finalStatusResp.Status == "FAILED" {
			break
		}
		time.Sleep(pollInterval)
	}

	require.True(t, time.Since(startTime) < maxWait, "Task %s did not reach terminal state within timeout (%v)", taskID, maxWait)
	assert.Equal(t, "COMPLETED", finalStatusResp.Status, "Task should have status COMPLETED (check worker)")
	assert.True(t, finalStatusResp.Result.Valid, "Result should be valid for a completed task")
	assert.NotEmpty(t, finalStatusResp.Result.String, "Result string should not be empty for a completed task")
	assert.Contains(t, finalStatusResp.Result.String, inputData, "Result should contain original input")
	assert.True(t, finalStatusResp.UpdatedAt.After(finalStatusResp.CreatedAt), "UpdatedAt should be after CreatedAt")

	var dbStatus string
	err = dbPool.QueryRow(context.Background(), "SELECT status FROM tasks WHERE task_id = $1", taskID).Scan(&dbStatus)
	require.NoError(t, err, "Failed to query final status from DB")
	assert.Equal(t, "COMPLETED", dbStatus, "Final status in DB should be COMPLETED")
}

func TestGetNonExistentTask_Integration(t *testing.T) {
	if testRouter == nil {
		t.Skip("Skipping integration test: Test environment not initialized")
	}

	nonExistentTaskID := uuid.NewString()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tasks/"+nonExistentTaskID+"/status", nil)
	testRouter.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "Expected HTTP 404 Not Found for non-existent task")
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Equal(t, "Task not found", errBody["error"])
}

func getQueueMessageCount(t *testing.T, mqURL, queueName string) (int, error) {
	conn, err := amqp.Dial(mqURL)
	if err != nil {
		return -1, fmt.Errorf("mq check failed to connect: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return -1, fmt.Errorf("mq check failed to open channel: %w", err)
	}
	defer ch.Close()

	q, err := ch.QueueInspect(queueName)
	if err != nil {
		return -1, fmt.Errorf("mq check failed to inspect queue '%s': %w", queueName, err)
	}
	return q.Messages, nil
}
