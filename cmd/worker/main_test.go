package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockAcknowledger struct {
	mock.Mock
}

func (m *MockAcknowledger) Ack(multiple bool) error {
	args := m.Called(multiple)
	return args.Error(0)
}

func (m *MockAcknowledger) Nack(multiple bool, requeue bool) error {
	args := m.Called(multiple, requeue)
	return args.Error(0)
}

func TestUpdateTaskStatus_Completed(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPool.Close()
	}()

	taskID := "task-123"
	status := "COMPLETED"
	result := "Success result"

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, result = \\$2, updated_at = NOW\\(\\) WHERE task_id = \\$3").
		WithArgs(status, result, taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = updateTaskStatus(mockPool, taskID, status, result)
	assert.NoError(t, err)
}

func TestUpdateTaskStatus_Running(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPool.Close()
	}()

	taskID := "task-456"
	status := "RUNNING"

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, updated_at = NOW\\(\\) WHERE task_id = \\$2").
		WithArgs(status, taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = updateTaskStatus(mockPool, taskID, status, "")
	assert.NoError(t, err)
}

func TestUpdateTaskStatus_Failed(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPool.Close()
	}()

	taskID := "task-789"
	status := "FAILED"
	failReason := "Something went wrong"

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, result = \\$2, updated_at = NOW\\(\\) WHERE task_id = \\$3").
		WithArgs(status, failReason, taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = updateTaskStatus(mockPool, taskID, status, failReason)
	assert.NoError(t, err)
}

func TestUpdateTaskStatus_DBError(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPool.Close()
	}()

	taskID := "task-err"
	status := "RUNNING"
	dbError := errors.New("connection error")

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, updated_at = NOW\\(\\) WHERE task_id = \\$2").
		WithArgs(status, taskID).
		WillReturnError(dbError)

	err = updateTaskStatus(mockPool, taskID, status, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), dbError.Error())
}

func TestUpdateTaskStatus_NoRowsAffected(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet())
		mockPool.Close()
	}()

	taskID := "task-not-found"
	status := "COMPLETED"
	result := "Result data"

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, result = \\$2, updated_at = NOW\\(\\) WHERE task_id = \\$3").
		WithArgs(status, result, taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err = updateTaskStatus(mockPool, taskID, status, result)
	assert.NoError(t, err)
}

func TestProcessMessage_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	mockAck := new(MockAcknowledger)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet(), "DB expectations not met")
		mockPool.Close()
		mockAck.AssertExpectations(t)
	}()

	taskID := "proc-task-ok"
	input := "some valid input"
	taskMsg := TaskMessage{TaskID: taskID, Input: input}
	body, _ := json.Marshal(taskMsg)

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, updated_at = NOW\\(\\) WHERE task_id = \\$2").
		WithArgs("RUNNING", taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, result = \\$2, updated_at = NOW\\(\\) WHERE task_id = \\$3").
		WithArgs("COMPLETED", pgxmock.AnyArg(), taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	originalSleepFunc := sleepFunc
	sleepFunc = func(d time.Duration) {}
	defer func() { sleepFunc = originalSleepFunc }()

	err = processMessage(mockPool, body, mockAck)

	assert.NoError(t, err, "processMessage should return nil on success")
}

func TestProcessMessage_UnmarshalError(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	mockAck := new(MockAcknowledger)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet(), "DB should not be called on unmarshal error")
		mockPool.Close()
		mockAck.AssertExpectations(t)
	}()

	invalidBody := []byte(`{"task_id": "bad-json", "input"`)

	err = processMessage(mockPool, invalidBody, mockAck)

	assert.Error(t, err, "processMessage should return error on unmarshal failure")
	assert.Contains(t, err.Error(), "JSON unmarshal error", "Error message should indicate JSON failure")
}

func TestProcessMessage_UpdateRunningError(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	mockAck := new(MockAcknowledger)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet(), "DB expectations not met")
		mockPool.Close()
		mockAck.AssertExpectations(t)
	}()

	taskID := "proc-task-fail1"
	input := "input data"
	taskMsg := TaskMessage{TaskID: taskID, Input: input}
	body, _ := json.Marshal(taskMsg)
	dbError := errors.New("db connection failed")

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, updated_at = NOW\\(\\) WHERE task_id = \\$2").
		WithArgs("RUNNING", taskID).
		WillReturnError(dbError)

	err = processMessage(mockPool, body, mockAck)

	assert.Error(t, err, "processMessage should return error on DB failure")
	assert.Contains(t, err.Error(), "db update RUNNING error", "Error message should indicate RUNNING update failure")
	assert.Contains(t, err.Error(), dbError.Error())
}

func TestProcessMessage_UpdateCompletedError(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	mockAck := new(MockAcknowledger)
	defer func() {
		assert.NoError(t, mockPool.ExpectationsWereMet(), "DB expectations not met")
		mockPool.Close()
		mockAck.AssertExpectations(t)
	}()

	taskID := "proc-task-fail2"
	input := "input data"
	taskMsg := TaskMessage{TaskID: taskID, Input: input}
	body, _ := json.Marshal(taskMsg)
	dbError := errors.New("cannot write result")

	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, updated_at = NOW\\(\\) WHERE task_id = \\$2").
		WithArgs("RUNNING", taskID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mockPool.ExpectExec("UPDATE tasks SET status = \\$1, result = \\$2, updated_at = NOW\\(\\) WHERE task_id = \\$3").
		WithArgs("COMPLETED", pgxmock.AnyArg(), taskID).
		WillReturnError(dbError)

	originalSleepFunc := sleepFunc
	sleepFunc = func(d time.Duration) {}
	defer func() { sleepFunc = originalSleepFunc }()

	err = processMessage(mockPool, body, mockAck)

	assert.Error(t, err, "processMessage should return error on second DB failure")
	assert.Contains(t, err.Error(), "db update COMPLETED error", "Error message should indicate COMPLETED update failure")
	assert.ErrorIs(t, err, dbError, "Expected error '%v' to be wrapped", dbError)
}
