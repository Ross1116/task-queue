package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ross1116/task-queue/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockQueuePublisher struct {
	mock.Mock
}

var _ task.QueuePublisher = (*MockQueuePublisher)(nil)

func (m *MockQueuePublisher) Publish(taskID string, input string) error {
	args := m.Called(taskID, input)
	return args.Error(0)
}

func (m *MockQueuePublisher) DeclareQueue() error {
	args := m.Called()
	if len(args) > 0 {
		return args.Error(0)
	}
	return nil
}

func (m *MockQueuePublisher) Close() error {
	args := m.Called()
	if len(args) > 0 {
		return args.Error(0)
	}
	return nil
}

func setupTestRouter(t *testing.T) (*gin.Engine, pgxmock.PgxPoolIface, *MockQueuePublisher) {
	gin.SetMode(gin.TestMode)

	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")

	mockPublisher := new(MockQueuePublisher)
	mockPublisher.On("DeclareQueue").Return(nil).Maybe()

	app := &Application{
		DB:        mockPool,
		Publisher: mockPublisher,
	}

	router := gin.New()
	router.POST("/tasks", app.createTaskHandler)
	router.GET("/tasks/:id/status", app.getTaskStatusHandler)

	return router, mockPool, mockPublisher
}

func TestCreateTaskHandler_Success(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet(), "pgxmock DB expectations not met")
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	inputPayload := `{"input": "process this data"}`
	expectedInput := "process this data"
	expectedStatus := "PENDING"

	mockPool.ExpectExec("INSERT INTO tasks").
		WithArgs(pgxmock.AnyArg(), expectedStatus, expectedInput).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mockPublisher.On("Publish", mock.AnythingOfType("string"), expectedInput).Return(nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(inputPayload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code, "Expected HTTP status 202 Accepted")

	var respBody task.CreateTaskResponse
	err := json.Unmarshal(w.Body.Bytes(), &respBody)
	require.NoError(t, err, "Failed to unmarshal response body")
	assert.NotEmpty(t, respBody.TaskID, "Expected a non-empty task_id in response")
	_, err = uuid.Parse(respBody.TaskID)
	assert.NoError(t, err, "Expected task_id to be a valid UUID")
}

func TestCreateTaskHandler_InvalidInput(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	invalidPayload := `{"inpu": "missing t"}`

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(invalidPayload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "Expected HTTP status 400 Bad Request")
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Contains(t, errBody["error"], "Invalid request body")
}

func TestCreateTaskHandler_DatabaseError(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	inputPayload := `{"input": "process this data"}`
	expectedInput := "process this data"
	dbError := errors.New("connection refused")

	mockPool.ExpectExec("INSERT INTO tasks").
		WithArgs(pgxmock.AnyArg(), "PENDING", expectedInput).
		WillReturnError(dbError)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(inputPayload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Equal(t, "Failed to create task record", errBody["error"])
}

func TestCreateTaskHandler_PublisherError(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	inputPayload := `{"input": "process this data"}`
	expectedInput := "process this data"
	publisherError := errors.New("queue is full")

	mockPool.ExpectExec("INSERT INTO tasks").
		WithArgs(pgxmock.AnyArg(), "PENDING", expectedInput).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockPublisher.On("Publish", mock.AnythingOfType("string"), expectedInput).Return(publisherError)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(inputPayload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Equal(t, "Task created but failed to queue for processing", errBody["error"])
}

func TestGetTaskStatusHandler_Success(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	testTaskID := uuid.NewString()
	expectedStatus := "COMPLETED"
	expectedInput := "some input"
	expectedResult := sql.NullString{String: "{\"output\": \"result data\"}", Valid: true}
	fixedTime := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"status", "task_input", "result", "created_at", "updated_at"}).
		AddRow(expectedStatus, expectedInput, expectedResult, fixedTime, fixedTime.Add(time.Minute*5))
	mockPool.ExpectQuery("SELECT status, task_input, result, created_at, updated_at FROM tasks").
		WithArgs(testTaskID).
		WillReturnRows(rows)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tasks/"+testTaskID+"/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var respBody task.TaskStatusResponse
	err := json.Unmarshal(w.Body.Bytes(), &respBody)
	require.NoError(t, err, "Failed to unmarshal response body")

	assert.Equal(t, testTaskID, respBody.TaskID)
	assert.Equal(t, expectedStatus, respBody.Status)
	assert.Equal(t, expectedInput, respBody.Input)
	assert.Equal(t, expectedResult, respBody.Result)
	assert.Equal(t, fixedTime.UTC(), respBody.CreatedAt.UTC())
	assert.Equal(t, fixedTime.Add(time.Minute*5).UTC(), respBody.UpdatedAt.UTC())
}

func TestGetTaskStatusHandler_NotFound(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	testTaskID := uuid.NewString()
	dbErrNotFound := pgx.ErrNoRows

	mockPool.ExpectQuery("SELECT status, task_input, result, created_at, updated_at FROM tasks").
		WithArgs(testTaskID).
		WillReturnError(dbErrNotFound)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tasks/"+testTaskID+"/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Equal(t, "Task not found", errBody["error"])
}

func TestGetTaskStatusHandler_InvalidUUID(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	invalidTaskID := "not-a-valid-uuid"

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tasks/"+invalidTaskID+"/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Equal(t, "Invalid Task ID format", errBody["error"])
}

func TestGetTaskStatusHandler_DatabaseError(t *testing.T) {
	router, mockPool, mockPublisher := setupTestRouter(t)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPublisher.AssertExpectations(t)
		mockPool.Close()
	}()

	testTaskID := uuid.NewString()
	dbError := errors.New("unexpected database error")

	mockPool.ExpectQuery("SELECT status, task_input, result, created_at, updated_at FROM tasks").
		WithArgs(testTaskID).
		WillReturnError(dbError)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tasks/"+testTaskID+"/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var errBody map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Equal(t, "Failed to retrieve task status", errBody["error"])
}
