package jobs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/logger"
)

func TestProcessor(t *testing.T) {
	logger := logger.NewDevelopmentLogger()
	storage := NewMemoryStorage(logger)
	config := DefaultConfig()

	processor, _ := NewProcessor(config, storage, logger)

	ctx := context.Background()
	err := processor.Start(ctx)
	require.NoError(t, err)
	defer processor.Stop(ctx)

	t.Run("EnqueueAndProcess", func(t *testing.T) {
		// Register a test handler
		handlerCalled := false
		handler := JobHandlerFunc(func(ctx context.Context, job Job) (interface{}, error) {
			handlerCalled = true
			return "test result", nil
		})

		err := processor.RegisterHandler("test_job", handler)
		require.NoError(t, err)

		// Create and enqueue a job
		job := NewJob("test_job", map[string]interface{}{
			"message": "hello world",
		})

		err = processor.Enqueue(ctx, job)
		require.NoError(t, err)

		// Wait for job to be processed
		time.Sleep(200 * time.Millisecond)

		// Verify job was processed
		assert.True(t, handlerCalled)

		// Check job status
		jobInfo, err := processor.GetJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, JobStatusCompleted, jobInfo.Status)
		assert.Equal(t, "test result", jobInfo.Result)
	})

	t.Run("EnqueueInFuture", func(t *testing.T) {
		handlerCalled := false
		handler := JobHandlerFunc(func(ctx context.Context, job Job) (interface{}, error) {
			handlerCalled = true
			return nil, nil
		})

		err := processor.RegisterHandler("future_job", handler)
		require.NoError(t, err)

		job := NewJob("future_job", map[string]interface{}{})

		// Enqueue job to run in the future
		err = processor.EnqueueIn(ctx, job, 100*time.Millisecond)
		require.NoError(t, err)

		// Should not be processed immediately
		time.Sleep(50 * time.Millisecond)
		assert.False(t, handlerCalled)

		// Should be processed after delay
		time.Sleep(100 * time.Millisecond)
		assert.True(t, handlerCalled)
	})

	t.Run("JobRetry", func(t *testing.T) {
		attempts := 0
		handler := JobHandlerFunc(func(ctx context.Context, job Job) (interface{}, error) {
			attempts++
			if attempts < 3 {
				return nil, fmt.Errorf("intentional failure")
			}
			return "success", nil
		})

		err := processor.RegisterHandler("retry_job", handler)
		require.NoError(t, err)

		job := NewJob("retry_job", map[string]interface{}{})
		job.MaxRetries = 3

		err = processor.Enqueue(ctx, job)
		require.NoError(t, err)

		// Wait for retries to complete
		time.Sleep(2 * time.Second)

		// Verify job eventually succeeded
		jobInfo, err := processor.GetJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, JobStatusCompleted, jobInfo.Status)
		assert.Equal(t, 3, attempts)
	})

	t.Run("JobFailure", func(t *testing.T) {
		handler := JobHandlerFunc(func(ctx context.Context, job Job) (interface{}, error) {
			return nil, fmt.Errorf("permanent failure")
		})

		err := processor.RegisterHandler("fail_job", handler)
		require.NoError(t, err)

		job := NewJob("fail_job", map[string]interface{}{})
		job.MaxRetries = 2

		err = processor.Enqueue(ctx, job)
		require.NoError(t, err)

		// Wait for job to fail
		time.Sleep(1 * time.Second)

		jobInfo, err := processor.GetJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, JobStatusFailed, jobInfo.Status)
		assert.Contains(t, jobInfo.LastError, "permanent failure")
	})
}

func TestMemoryQueue(t *testing.T) {
	logger := logger.NewDevelopmentLogger()
	config := DefaultQueueConfig()
	queue := NewMemoryQueue("test_queue", config, logger)

	ctx := context.Background()

	t.Run("PushAndPop", func(t *testing.T) {
		job := NewJob("test", map[string]interface{}{})

		err := queue.Push(ctx, job)
		require.NoError(t, err)

		size, err := queue.Size(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), size)

		poppedJob, err := queue.Pop(ctx)
		require.NoError(t, err)
		require.NotNil(t, poppedJob)
		assert.Equal(t, job.ID, poppedJob.ID)

		size, err = queue.Size(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), size)
	})

	t.Run("PriorityOrdering", func(t *testing.T) {
		// Create jobs with different priorities
		lowPriorityJob := NewJob("low", map[string]interface{}{})
		lowPriorityJob.Priority = 1

		highPriorityJob := NewJob("high", map[string]interface{}{})
		highPriorityJob.Priority = 10

		mediumPriorityJob := NewJob("medium", map[string]interface{}{})
		mediumPriorityJob.Priority = 5

		// Push in random order
		err := queue.Push(ctx, lowPriorityJob)
		require.NoError(t, err)

		err = queue.Push(ctx, highPriorityJob)
		require.NoError(t, err)

		err = queue.Push(ctx, mediumPriorityJob)
		require.NoError(t, err)

		// Pop should return highest priority first
		job1, err := queue.Pop(ctx)
		require.NoError(t, err)
		assert.Equal(t, "high", job1.Type)

		job2, err := queue.Pop(ctx)
		require.NoError(t, err)
		assert.Equal(t, "medium", job2.Type)

		job3, err := queue.Pop(ctx)
		require.NoError(t, err)
		assert.Equal(t, "low", job3.Type)
	})

	t.Run("PauseAndResume", func(t *testing.T) {
		job := NewJob("test", map[string]interface{}{})

		err := queue.Push(ctx, job)
		require.NoError(t, err)

		err = queue.Pause(ctx)
		require.NoError(t, err)

		assert.True(t, queue.IsPaused(ctx))

		// Should not be able to pop when paused
		poppedJob, err := queue.Pop(ctx)
		assert.Error(t, err)
		assert.Nil(t, poppedJob)

		err = queue.Resume(ctx)
		require.NoError(t, err)

		assert.False(t, queue.IsPaused(ctx))

		// Should be able to pop after resume
		poppedJob, err = queue.Pop(ctx)
		require.NoError(t, err)
		assert.NotNil(t, poppedJob)
	})
}

func TestScheduler(t *testing.T) {
	logger := logger.NewDevelopmentLogger()
	storage := NewMemoryStorage(logger)
	config := DefaultConfig()

	processor, _ := NewProcessor(config, storage, logger)
	ctx := context.Background()
	err := processor.Start(ctx)
	require.NoError(t, err)
	defer processor.Stop(ctx)

	scheduleStorage := NewMemoryScheduleStorage(logger)
	schedulerConfig := DefaultSchedulerConfig()
	schedulerConfig.TickInterval = 100 * time.Millisecond // Fast ticking for tests

	scheduler := NewScheduler(schedulerConfig, processor, scheduleStorage, logger)
	err = scheduler.Start(ctx)
	require.NoError(t, err)
	defer scheduler.Stop(ctx)

	t.Run("CreateAndExecuteSchedule", func(t *testing.T) {
		executed := false
		handler := JobHandlerFunc(func(ctx context.Context, job Job) (interface{}, error) {
			executed = true
			return "scheduled job executed", nil
		})

		err := processor.RegisterHandler("scheduled_job", handler)
		require.NoError(t, err)

		// Create a schedule that runs every second
		schedule := NewSchedule("test_schedule", "scheduled_job", "*/1 * * * * *", map[string]interface{}{
			"test": "data",
		})

		err = scheduler.Schedule(ctx, schedule)
		require.NoError(t, err)

		// Wait for schedule to execute
		time.Sleep(1500 * time.Millisecond)

		assert.True(t, executed)

		// Verify schedule info
		scheduleInfo, err := scheduler.GetSchedule(ctx, schedule.ID)
		require.NoError(t, err)
		assert.Equal(t, "test_schedule", scheduleInfo.Name)
		assert.True(t, scheduleInfo.Enabled)
	})

	t.Run("DisableSchedule", func(t *testing.T) {
		executed := false
		handler := JobHandlerFunc(func(ctx context.Context, job Job) (interface{}, error) {
			executed = true
			return nil, nil
		})

		err := processor.RegisterHandler("disabled_job", handler)
		require.NoError(t, err)

		schedule := NewSchedule("disabled_schedule", "disabled_job", "*/1 * * * * *", map[string]interface{}{})

		err = scheduler.Schedule(ctx, schedule)
		require.NoError(t, err)

		// Disable the schedule
		err = scheduler.DisableSchedule(ctx, schedule.ID)
		require.NoError(t, err)

		// Wait and verify it doesn't execute
		time.Sleep(1500 * time.Millisecond)
		assert.False(t, executed)

		// Re-enable and verify it executes
		err = scheduler.EnableSchedule(ctx, schedule.ID)
		require.NoError(t, err)

		time.Sleep(1500 * time.Millisecond)
		assert.True(t, executed)
	})
}

func TestJobBuilder(t *testing.T) {
	t.Run("BuildJobWithAllOptions", func(t *testing.T) {
		job := NewJobBuilder("test_job").
			WithPayload(map[string]interface{}{"key": "value"}).
			WithQueue("custom_queue").
			WithPriority(10).
			WithMaxRetries(5).
			WithTimeout(60 * time.Second).
			WithTags([]string{"tag1", "tag2"}).
			WithMetadata(map[string]interface{}{"env": "test"}).
			Build()

		assert.Equal(t, "test_job", job.Type)
		assert.Equal(t, "custom_queue", job.Queue)
		assert.Equal(t, 10, job.Priority)
		assert.Equal(t, 5, job.MaxRetries)
		assert.Equal(t, 60*time.Second, job.Timeout)
		assert.Equal(t, "value", job.Payload["key"])
		assert.Equal(t, []string{"tag1", "tag2"}, job.Tags)
		assert.Equal(t, "test", job.Metadata["env"])
	})
}

func TestJobSystem(t *testing.T) {
	logger := logger.NewDevelopmentLogger()
	config := DefaultConfig()
	config.Scheduler.Enabled = true

	system, err := NewJobSystem(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = system.Start(ctx)
	require.NoError(t, err)
	defer system.Stop(ctx)

	t.Run("RegisterHandlerAndProcess", func(t *testing.T) {
		executed := false
		err := system.RegisterHandlerFunc("system_test", func(ctx context.Context, job Job) (interface{}, error) {
			executed = true
			return "processed", nil
		})
		require.NoError(t, err)

		job := NewJob("system_test", map[string]interface{}{})
		err = system.Enqueue(ctx, job)
		require.NoError(t, err)

		time.Sleep(200 * time.Millisecond)
		assert.True(t, executed)
	})

	t.Run("CreateSchedule", func(t *testing.T) {
		executed := false
		err := system.RegisterHandlerFunc("system_schedule", func(ctx context.Context, job Job) (interface{}, error) {
			executed = true
			return nil, nil
		})
		require.NoError(t, err)

		schedule := NewSchedule("system_test_schedule", "system_schedule", "*/1 * * * * *", map[string]interface{}{})
		err = system.Schedule(ctx, schedule)
		require.NoError(t, err)

		time.Sleep(1500 * time.Millisecond)
		assert.True(t, executed)
	})

	t.Run("GetStats", func(t *testing.T) {
		stats, err := system.GetStats(ctx)
		require.NoError(t, err)

		assert.NotNil(t, stats.Processor)
		assert.NotNil(t, stats.Scheduler)
		assert.True(t, stats.Processor.TotalJobs >= 0)
	})

	t.Run("HealthCheck", func(t *testing.T) {
		err := system.Health(ctx)
		assert.NoError(t, err)
	})
}

func TestJobSystemBuilder(t *testing.T) {
	logger := logger.NewDevelopmentLogger()

	t.Run("BuildWithDefaults", func(t *testing.T) {
		system, err := NewJobSystemBuilder(logger).Build()
		require.NoError(t, err)
		assert.NotNil(t, system)
	})

	t.Run("BuildWithCustomConfig", func(t *testing.T) {
		system, err := NewJobSystemBuilder(logger).
			WithConcurrency(20).
			WithStorage("memory", nil).
			WithQueue("custom", QueueConfig{
				Workers:  10,
				Priority: 5,
				MaxSize:  2000,
			}).
			EnableMonitoring().
			Build()

		require.NoError(t, err)
		assert.NotNil(t, system)
	})

	t.Run("BuildAndStart", func(t *testing.T) {
		ctx := context.Background()

		system, err := NewJobSystemBuilder(logger).
			RegisterHandlerFunc("builder_test", func(ctx context.Context, job Job) (interface{}, error) {
				return "ok", nil
			}).
			BuildAndStart(ctx)

		require.NoError(t, err)
		defer system.Stop(ctx)

		assert.True(t, system.IsStarted())
	})
}

func TestPayloadExtractor(t *testing.T) {
	payload := map[string]interface{}{
		"string_field":   "test value",
		"int_field":      42,
		"float_field":    3.14,
		"bool_field":     true,
		"time_field":     "2023-01-01T12:00:00Z",
		"duration_field": "30s",
		"slice_field":    []interface{}{"item1", "item2"},
		"map_field": map[string]interface{}{
			"nested": "value",
		},
	}

	extractor := NewJobPayloadExtractor(payload)

	t.Run("GetString", func(t *testing.T) {
		value, err := extractor.GetString("string_field")
		require.NoError(t, err)
		assert.Equal(t, "test value", value)

		optional := extractor.GetStringOptional("missing_field", "default")
		assert.Equal(t, "default", optional)
	})

	t.Run("GetInt", func(t *testing.T) {
		value, err := extractor.GetInt("int_field")
		require.NoError(t, err)
		assert.Equal(t, 42, value)

		// Test float to int conversion
		value, err = extractor.GetInt("float_field")
		require.NoError(t, err)
		assert.Equal(t, 3, value)
	})

	t.Run("GetBool", func(t *testing.T) {
		value, err := extractor.GetBool("bool_field")
		require.NoError(t, err)
		assert.True(t, value)
	})

	t.Run("GetTime", func(t *testing.T) {
		value, err := extractor.GetTime("time_field")
		require.NoError(t, err)
		expected, _ := time.Parse(time.RFC3339, "2023-01-01T12:00:00Z")
		assert.Equal(t, expected, value)
	})

	t.Run("GetDuration", func(t *testing.T) {
		value, err := extractor.GetDuration("duration_field")
		require.NoError(t, err)
		assert.Equal(t, 30*time.Second, value)
	})

	t.Run("GetSlice", func(t *testing.T) {
		value, err := extractor.GetSlice("slice_field")
		require.NoError(t, err)
		assert.Len(t, value, 2)
		assert.Equal(t, "item1", value[0])
	})

	t.Run("GetMap", func(t *testing.T) {
		value, err := extractor.GetMap("map_field")
		require.NoError(t, err)
		assert.Equal(t, "value", value["nested"])
	})

	t.Run("Validate", func(t *testing.T) {
		err := extractor.Validate([]string{"string_field", "int_field"})
		assert.NoError(t, err)

		err = extractor.Validate([]string{"missing_field"})
		assert.Error(t, err)
	})
}

func TestJobValidator(t *testing.T) {
	utils := NewJobUtils(logger.NewDevelopmentLogger())

	t.Run("ValidJob", func(t *testing.T) {
		job := NewJob("test_type", map[string]interface{}{
			"key": "value",
		})
		job.Queue = "test_queue"

		err := utils.ValidateJob(job)
		assert.NoError(t, err)
	})

	t.Run("InvalidJob", func(t *testing.T) {
		// Missing job type
		job := Job{
			Queue: "test_queue",
		}
		err := utils.ValidateJob(job)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "job type is required")

		// Invalid timeout
		job = NewJob("test", nil)
		job.Timeout = -1 * time.Second
		err = utils.ValidateJob(job)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be positive")

		// Invalid priority
		job = NewJob("test", nil)
		job.Priority = 150
		err = utils.ValidateJob(job)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "priority must be between")
	})
}

func TestCronValidation(t *testing.T) {
	utils := NewJobUtils(logger.NewDevelopmentLogger())

	validExpressions := []string{
		"0 0 * * *",    // Daily at midnight
		"0 */2 * * *",  // Every 2 hours
		"30 9 * * 1-5", // 9:30 AM on weekdays
		"0 0 1 * *",    // First day of every month
		"0 0 0 1 1 *",  // January 1st (with seconds)
	}

	for _, expr := range validExpressions {
		t.Run("Valid_"+expr, func(t *testing.T) {
			err := utils.validateCronExpression(expr)
			assert.NoError(t, err, "Expression should be valid: %s", expr)
		})
	}

	invalidExpressions := []string{
		"* * * *",         // Too few fields
		"* * * * * * *",   // Too many fields
		"60 * * * *",      // Invalid minute (60)
		"* 25 * * *",      // Invalid hour (25)
		"* * 32 * *",      // Invalid day (32)
		"* * * 13 *",      // Invalid month (13)
		"* * * * 8",       // Invalid weekday (8)
		"invalid * * * *", // Non-numeric value
	}

	for _, expr := range invalidExpressions {
		t.Run("Invalid_"+expr, func(t *testing.T) {
			err := utils.validateCronExpression(expr)
			assert.Error(t, err, "Expression should be invalid: %s", expr)
		})
	}
}

func TestJobCloner(t *testing.T) {
	cloner := NewJobCloner()

	original := NewJob("test_type", map[string]interface{}{
		"key": "value",
		"nested": map[string]interface{}{
			"inner": "data",
		},
		"slice": []interface{}{"item1", "item2"},
	})
	original.Tags = []string{"tag1", "tag2"}
	original.Metadata = map[string]interface{}{"env": "test"}

	clone := cloner.CloneJob(original)

	// Verify fields are copied
	assert.Equal(t, original.ID, clone.ID)
	assert.Equal(t, original.Type, clone.Type)
	assert.Equal(t, original.Queue, clone.Queue)

	// Verify deep copy of payload
	assert.Equal(t, original.Payload["key"], clone.Payload["key"])

	// Modify clone and verify original is unchanged
	clone.Payload["key"] = "modified"
	assert.NotEqual(t, original.Payload["key"], clone.Payload["key"])

	// Verify nested map is deep copied
	nestedClone := clone.Payload["nested"].(map[string]interface{})
	nestedClone["inner"] = "modified"

	nestedOriginal := original.Payload["nested"].(map[string]interface{})
	assert.NotEqual(t, nestedOriginal["inner"], nestedClone["inner"])
}

// Benchmark tests
// func BenchmarkJobEnqueue(b *testing.B) {
// 	logger := logger.NewDevelopmentLogger()
// 	storage := NewMemoryStorage(logger)
// 	config := DefaultConfig()
// 	processor := NewProcessor(config, storage, logger)
//
// 	ctx := context.Background()
// 	processor.Start(ctx)
// 	defer processor.Stop(ctx)
//
// 	// Register a simple handler
// 	processor.RegisterHandlerFunc("bench_job", func(ctx context.Context, job Job) (interface{}, error) {
// 		return nil, nil
// 	})
//
// 	b.ResetTimer()
//
// 	for i := 0; i < b.N; i++ {
// 		job := NewJob("bench_job", map[string]interface{}{
// 			"iteration": i,
// 		})
// 		processor.Enqueue(ctx, job)
// 	}
// }

func BenchmarkQueueOperations(b *testing.B) {
	logger := logger.NewDevelopmentLogger()
	config := DefaultQueueConfig()
	queue := NewMemoryQueue("bench_queue", config, logger)

	ctx := context.Background()

	b.ResetTimer()

	b.Run("Push", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			job := NewJob("bench", map[string]interface{}{})
			queue.Push(ctx, job)
		}
	})

	b.Run("Pop", func(b *testing.B) {
		// Pre-populate queue
		for i := 0; i < b.N; i++ {
			job := NewJob("bench", map[string]interface{}{})
			queue.Push(ctx, job)
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			queue.Pop(ctx)
		}
	})
}

// Helper functions for tests

func waitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Condition not met within timeout: %s", message)
}

// Mock implementations for testing

type mockStorage struct {
	jobs map[string]Job
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		jobs: make(map[string]Job),
	}
}

func (s *mockStorage) SaveJob(ctx context.Context, job Job) error {
	s.jobs[job.ID] = job
	return nil
}

func (s *mockStorage) GetJob(ctx context.Context, jobID string) (*Job, error) {
	job, exists := s.jobs[jobID]
	if !exists {
		return nil, ErrJobNotFound
	}
	return &job, nil
}

func (s *mockStorage) UpdateJob(ctx context.Context, job Job) error {
	if _, exists := s.jobs[job.ID]; !exists {
		return ErrJobNotFound
	}
	s.jobs[job.ID] = job
	return nil
}

func (s *mockStorage) DeleteJob(ctx context.Context, jobID string) error {
	delete(s.jobs, jobID)
	return nil
}

func (s *mockStorage) GetJobsByStatus(ctx context.Context, status JobStatus, limit int) ([]Job, error) {
	var jobs []Job
	for _, job := range s.jobs {
		if job.Status == status {
			jobs = append(jobs, job)
			if limit > 0 && len(jobs) >= limit {
				break
			}
		}
	}
	return jobs, nil
}

func (s *mockStorage) GetJobsByQueue(ctx context.Context, queueName string, limit int) ([]Job, error) {
	var jobs []Job
	for _, job := range s.jobs {
		if job.Queue == queueName {
			jobs = append(jobs, job)
			if limit > 0 && len(jobs) >= limit {
				break
			}
		}
	}
	return jobs, nil
}

func (s *mockStorage) GetExpiredJobs(ctx context.Context) ([]Job, error) {
	return []Job{}, nil
}

func (s *mockStorage) GetRetryableJobs(ctx context.Context) ([]Job, error) {
	return []Job{}, nil
}

func (s *mockStorage) EnqueueJob(ctx context.Context, queueName string, job Job) error {
	return nil
}

func (s *mockStorage) DequeueJob(ctx context.Context, queueName string) (*Job, error) {
	return nil, nil
}

func (s *mockStorage) GetQueueSize(ctx context.Context, queueName string) (int64, error) {
	return 0, nil
}

func (s *mockStorage) GetStats(ctx context.Context) (*StorageStats, error) {
	return &StorageStats{}, nil
}

func (s *mockStorage) GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error) {
	return &QueueStats{}, nil
}

func (s *mockStorage) CleanupJobs(ctx context.Context, olderThan time.Time) error {
	return nil
}

func (s *mockStorage) CleanupQueue(ctx context.Context, queueName string) error {
	return nil
}

func (s *mockStorage) Close() error {
	return nil
}
