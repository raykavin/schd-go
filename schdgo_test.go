package goschd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type MockLogger struct {
	t *testing.T
}

func (m *MockLogger) WithField(key string, value any) Logger {
	return m
}

func (m *MockLogger) WithError(err error) Logger {
	return m
}

func (m *MockLogger) Debug(args ...any) {
	m.t.Log(args...)
}

func (m *MockLogger) Info(args ...any) {
	m.t.Log(args...)
}

func TestAddAndExecutionRunOnce(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	doneCh := make(chan struct{})

	task := &Task{
		Interval: "100ms",
		RunOnce:  true,
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error {
			close(doneCh)
			return nil
		},
	}

	err := scheduler.Add("task1", task)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	// Wait for the task to execute.
	select {
	case <-doneCh:
		// Task executed as expected.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("task did not execute within expected time")
	}

	// Allow some time for the task to be deleted if RunOnce.
	time.Sleep(100 * time.Millisecond)
	_, err = scheduler.Lookup("task1")
	if err == nil {
		t.Fatal("expected task to be deleted after run once, but it still exists")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestAddTaskInvalidFunction(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))

	// No TaskFunc or FuncWithTaskContext provided.
	task := &Task{
		Interval: "100ms",
	}

	err := scheduler.Add("invalid", task)
	if err == nil {
		t.Fatal("expected error when adding task with nil function, got nil")
	}
	if !errors.Is(err, ErrTaskFuncNil) {
		t.Fatalf("expected ErrTaskFuncNil, got %v", err)
	}
}

func TestDuplicateTaskAdd(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	task := &Task{
		Interval: "100ms",
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error { return nil },
	}
	err := scheduler.Add("dup", task)
	if err != nil {
		t.Fatalf("unexpected error adding first task: %v", err)
	}

	task2 := &Task{
		Interval: "100ms",
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error { return nil },
	}
	err = scheduler.Add("dup", task2)
	if err == nil {
		t.Fatal("expected error when adding duplicate task id, got nil")
	}
	if !errors.Is(err, ErrTaskIDInUse) {
		t.Fatalf("expected ErrTaskIDInUse, got %v", err)
	}
}

func TestDelTask(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	task := &Task{
		Interval: "100ms",
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error { return nil },
	}
	err := scheduler.Add("taskDel", task)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	scheduler.Del("taskDel")
	_, err = scheduler.Lookup("taskDel")
	if err == nil {
		t.Fatal("expected error looking up deleted task, got nil")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestLookupTask(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	task := &Task{
		Interval: "100ms",
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error { return nil },
	}
	err := scheduler.Add("lookupTask", task)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	clone, err := scheduler.Lookup("lookupTask")
	if err != nil {
		t.Fatalf("unexpected error looking up task: %v", err)
	}

	if clone.id != "lookupTask" {
		t.Fatalf("expected task id %q, got %q", "lookupTask", clone.id)
	}
}

func TestInvalidInterval(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	task := &Task{
		Interval: "not-a-duration-or-cron",
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error { return nil },
	}
	err := scheduler.Add("invalidInterval", task)
	if err == nil {
		t.Fatal("expected error when adding task with invalid interval, got nil")
	}
}

func TestScheduleTaskWithStartAfter(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	doneCh := make(chan struct{})
	startAfter := time.Now().Add(200 * time.Millisecond)

	task := &Task{
		Interval:   "50ms",
		FirstRun:   false,
		RunOnce:    true,
		StartAfter: startAfter,
		TaskFunc: func(ctx context.Context) error {
			close(doneCh)
			return nil
		},
	}

	err := scheduler.Add("startAfterTask", task)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	// Ensure the task does not run before the scheduled start.
	select {
	case <-doneCh:
		t.Fatal("task executed before startAfter")
	case <-time.After(150 * time.Millisecond):
		// Expected: not executed yet.
	}

	// Now wait long enough for the task to fire.
	select {
	case <-doneCh:
		// Task executed as expected.
	case <-time.After(150 * time.Millisecond):
		t.Fatal("task did not execute after startAfter delay")
	}
}

func TestCronIntervalTask(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	doneCh := make(chan struct{})

	// Use a cron expression that fires every second.
	task := &Task{
		Interval: "* * * * * *", // cron expression (with seconds field)
		RunOnce:  true,
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error {
			close(doneCh)
			return nil
		},
	}

	err := scheduler.Add("cronTask", task)
	if err != nil {
		t.Fatalf("unexpected error adding cron task: %v", err)
	}

	select {
	case <-doneCh:
		// Task executed as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("cron task did not execute within expected time")
	}
}

func TestErrFuncInvocation(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	errCh := make(chan error, 1)

	task := &Task{
		Interval: "100ms",
		RunOnce:  true,
		FirstRun: true,
		TaskFunc: func(ctx context.Context) error {
			return errors.New("task error")
		},
		ErrFunc: func(err error) {
			errCh <- err
		},
	}

	err := scheduler.Add("errorTask", task)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	select {
	case e := <-errCh:
		if e.Error() != "task error" {
			t.Fatalf("expected error 'task error', got %v", e)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("error function was not invoked")
	}
}

func TestRunSingleInstance(t *testing.T) {
	scheduler := NewTaskScheduler(WithLogger(&MockLogger{t: t}))
	var mu sync.Mutex
	count := 0
	wg := sync.WaitGroup{}
	wg.Add(1)

	// The task sleeps long enough that if it were run concurrently,
	// the counter would increment more than once.
	task := &Task{
		Interval:          "100ms",
		RunOnce:           true,
		RunSingleInstance: true,
		FirstRun:          true,
		TaskFunc: func(ctx context.Context) error {
			mu.Lock()
			count++
			mu.Unlock()
			time.Sleep(150 * time.Millisecond)
			wg.Done()
			return nil
		},
	}

	err := scheduler.Add("singleInstanceTask", task)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}

	wg.Wait()
	mu.Lock()
	if count != 1 {
		t.Errorf("expected task to run once due to single instance constraint, got count %d", count)
	}
	mu.Unlock()
}

func TestClone(t *testing.T) {
	original := &Task{
		Interval:          "100ms",
		RunOnce:           false,
		RunSingleInstance: true,
		FirstRun:          true,
		TaskFunc:          func(ctx context.Context) error { return nil },
	}
	original.TaskContext = TaskContext{
		Context: context.Background(),
		id:      "original",
	}

	clone := original.Clone()
	if clone == original {
		t.Fatal("clone should not be the same pointer as original")
	}

	if clone.Interval != original.Interval ||
		clone.RunOnce != original.RunOnce ||
		clone.RunSingleInstance != original.RunSingleInstance ||
		clone.FirstRun != original.FirstRun ||
		clone.TaskContext.id != original.TaskContext.id {
		t.Fatal("clone does not match original task properties")
	}
}

func TestWithLogger(t *testing.T) {
	mockLogger := &MockLogger{t: t}
	scheduler := NewTaskScheduler(WithLogger(mockLogger))

	if scheduler.logger != mockLogger {
		t.Fatal("scheduler logger was not set correctly with WithLogger option")
	}

}
