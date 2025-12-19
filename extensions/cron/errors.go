package cron

import (
	"github.com/xraph/forge/errors"
)

// Error codes for the cron extension.
const (
	// ErrCodeJobNotFound indicates a job with the given ID was not found
	ErrCodeJobNotFound = "CRON_JOB_NOT_FOUND"

	// ErrCodeJobAlreadyExists indicates a job with the given ID already exists
	ErrCodeJobAlreadyExists = "CRON_JOB_ALREADY_EXISTS"

	// ErrCodeInvalidSchedule indicates the cron expression is invalid
	ErrCodeInvalidSchedule = "CRON_INVALID_SCHEDULE"

	// ErrCodeInvalidConfig indicates the configuration is invalid
	ErrCodeInvalidConfig = "CRON_INVALID_CONFIG"

	// ErrCodeJobDisabled indicates an operation was attempted on a disabled job
	ErrCodeJobDisabled = "CRON_JOB_DISABLED"

	// ErrCodeHandlerNotFound indicates a handler with the given name was not registered
	ErrCodeHandlerNotFound = "CRON_HANDLER_NOT_FOUND"

	// ErrCodeExecutionNotFound indicates an execution with the given ID was not found
	ErrCodeExecutionNotFound = "CRON_EXECUTION_NOT_FOUND"

	// ErrCodeExecutionTimeout indicates a job execution exceeded its timeout
	ErrCodeExecutionTimeout = "CRON_EXECUTION_TIMEOUT"

	// ErrCodeExecutionCancelled indicates a job execution was cancelled
	ErrCodeExecutionCancelled = "CRON_EXECUTION_CANCELLED"

	// ErrCodeMaxRetriesExceeded indicates a job exceeded its maximum retry attempts
	ErrCodeMaxRetriesExceeded = "CRON_MAX_RETRIES_EXCEEDED"

	// ErrCodeSchedulerNotRunning indicates the scheduler is not running
	ErrCodeSchedulerNotRunning = "CRON_SCHEDULER_NOT_RUNNING"

	// ErrCodeNotLeader indicates the operation requires leader status in distributed mode
	ErrCodeNotLeader = "CRON_NOT_LEADER"

	// ErrCodeStorageError indicates a storage operation failed
	ErrCodeStorageError = "CRON_STORAGE_ERROR"

	// ErrCodeLockAcquisitionFailed indicates failed to acquire distributed lock
	ErrCodeLockAcquisitionFailed = "CRON_LOCK_ACQUISITION_FAILED"

	// ErrCodeJobRunning indicates the job is already running
	ErrCodeJobRunning = "CRON_JOB_RUNNING"

	// ErrCodeInvalidJobType indicates the job type (handler vs command) is invalid
	ErrCodeInvalidJobType = "CRON_INVALID_JOB_TYPE"
)

var (
	// ErrJobNotFound is returned when a job is not found
	ErrJobNotFound = errors.New("job not found")

	// ErrJobAlreadyExists is returned when trying to create a job with an existing ID
	ErrJobAlreadyExists = errors.New("job already exists")

	// ErrInvalidSchedule is returned when a cron expression is invalid
	ErrInvalidSchedule = errors.New("invalid cron schedule")

	// ErrInvalidConfig is returned when the configuration is invalid
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrJobDisabled is returned when attempting to trigger a disabled job
	ErrJobDisabled = errors.New("job is disabled")

	// ErrHandlerNotFound is returned when a handler is not registered
	ErrHandlerNotFound = errors.New("handler not found")

	// ErrExecutionNotFound is returned when an execution is not found
	ErrExecutionNotFound = errors.New("execution not found")

	// ErrExecutionTimeout is returned when a job execution times out
	ErrExecutionTimeout = errors.New("execution timeout")

	// ErrExecutionCancelled is returned when a job execution is cancelled
	ErrExecutionCancelled = errors.New("execution cancelled")

	// ErrMaxRetriesExceeded is returned when max retries are exceeded
	ErrMaxRetriesExceeded = errors.New("max retries exceeded")

	// ErrSchedulerNotRunning is returned when the scheduler is not running
	ErrSchedulerNotRunning = errors.New("scheduler not running")

	// ErrNotLeader is returned when operation requires leader in distributed mode
	ErrNotLeader = errors.New("not leader")

	// ErrStorageError is returned when a storage operation fails
	ErrStorageError = errors.New("storage error")

	// ErrLockAcquisitionFailed is returned when failed to acquire distributed lock
	ErrLockAcquisitionFailed = errors.New("lock acquisition failed")

	// ErrJobRunning is returned when trying to start an already running job
	ErrJobRunning = errors.New("job is already running")

	// ErrInvalidJobType is returned when job type is invalid
	ErrInvalidJobType = errors.New("invalid job type: must have either handler or command")

	// ErrInvalidData is returned when invalid data is received from storage
	ErrInvalidData = errors.New("invalid data received from storage")
)
