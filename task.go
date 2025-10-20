package goschd

import (
	"context"
	"sync"
	"time"
)

// TaskContext provides contextual information for task execution, including
// the task's unique identifier and execution context.
type TaskContext struct {
	Context context.Context // Context for managing the task's lifecycle.
	id      string          // Unique identifier for the task.
}

// ID returns the unique identifier of the task.
func (ctx TaskContext) ID() string { return ctx.id }

// Task represents a scheduled task with its configuration and execution parameters.
// It supports various execution modes including one-time runs, recurring intervals,
// and cron-based scheduling.
type Task struct {
	sync.RWMutex // Managing concurrent modifications to task properties.

	// TaskContext provides contextual information and ID for the task.
	TaskContext TaskContext

	// Interval specifies when the task should run. Can be a duration string (e.g., "5s", "1m")
	// or a cron expression (e.g., "0 0 * * *" for daily at midnight).
	Interval string

	// RunOnce indicates if the task should run only once and then be removed from the scheduler.
	RunOnce bool

	// FirstRun indicates if the task should run immediately upon being scheduled.
	FirstRun bool

	// RunSingleInstance prevents concurrent instances of the task from running.
	// If true, a new execution will be skipped if the previous one is still running.
	RunSingleInstance bool

	// StartAfter specifies the earliest time when the task should start executing.
	// The task will not run before this time.
	StartAfter time.Time

	// TaskFunc is the main function to execute. It receives a context for cancellation.
	// Either TaskFunc or FuncWithTaskContext must be provided.
	TaskFunc func(ctx context.Context) error

	// ErrFunc is called when TaskFunc returns an error.
	ErrFunc func(error)

	// FuncWithTaskContext is an alternative to TaskFunc that receives TaskContext instead of context.Context.
	// Either TaskFunc or FuncWithTaskContext must be provided.
	FuncWithTaskContext func(TaskContext) error

	// ErrFuncWithTaskContext is called when FuncWithTaskContext returns an error.
	ErrFuncWithTaskContext func(TaskContext, error)

	// Private fields for internal state management
	id       string             // Unique identifier for the task.
	running  sync.Mutex         // Mutex to manage task's running state.
	timer    *time.Timer        // Timer for scheduling the task execution.
	ctx      context.Context    // Context for managing the task's lifecycle.
	cancel   context.CancelFunc // Cancel function to terminate the task's context.
	interval time.Duration      // The duration parsed from interval string.
}

// Clone creates a deep copy of the task. This is useful for creating task templates
// or when you need to inspect a task's configuration without affecting the original.
// The clone shares the same context and timer references as the original.
func (t *Task) Clone() *Task {
	task := new(Task)

	t.safeOps(func() {
		task.TaskFunc = t.TaskFunc
		task.FuncWithTaskContext = t.FuncWithTaskContext
		task.ErrFunc = t.ErrFunc
		task.ErrFuncWithTaskContext = t.ErrFuncWithTaskContext
		task.Interval = t.Interval
		task.StartAfter = t.StartAfter
		task.RunOnce = t.RunOnce
		task.FirstRun = t.FirstRun
		task.RunSingleInstance = t.RunSingleInstance
		task.TaskContext = t.TaskContext
		task.id = t.id
		task.ctx = t.ctx
		task.cancel = t.cancel
		task.timer = t.timer
	})

	return task
}

// safeOps executes the provided function while holding the task's write lock.
// This ensures thread-safe access to the task's properties.
func (t *Task) safeOps(fn func()) {
	t.Lock()
	defer t.Unlock()

	fn()
}
