package goschd

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorhill/cronexpr"
)

// Defines the logger interface
type Logger interface {
	WithField(key string, value any) Logger
	WithError(err error) Logger

	// Standard log functions
	Debug(args ...any)
	Info(args ...any)
}

var (
	// ErrTaskIDInUse is returned when attempting to add a task with an ID that already exists.
	ErrTaskIDInUse = errors.New("task id already in use")

	// ErrTaskFuncNil is returned when a task is created without a function to execute.
	ErrTaskFuncNil = errors.New("task function cannot be nil")

	// ErrTaskInterval is returned when a task is created without a valid interval.
	ErrTaskInterval = errors.New("task interval must be defined")

	// ErrInvalidTaskInterval is returned when a task is created with an invalid interval format.
	ErrInvalidTaskInterval = errors.New("invalid task interval format")

	// ErrTaskNotFound is returned when attempting to lookup a task that doesn't exist.
	ErrTaskNotFound = errors.New("could not find task within the task list")
)

// Scheduler manages a collection of scheduled tasks. It provides thread-safe
// operations for adding, removing, and looking up tasks by their unique identifiers.
type Scheduler struct {
	sync.RWMutex                  // RWMutex for managing concurrent access to tasks.
	logger       Logger           // Logger for logging task information.
	tasks        map[string]*Task // Map of scheduled tasks, keyed by their unique ID.
}

// NewTaskScheduler creates and returns a new instance of the task scheduler.
// The scheduler is ready to use immediately after creation.
func NewTaskScheduler(opts ...SchedulerOpts) *Scheduler {
	scheduler := &Scheduler{
		tasks: make(map[string]*Task),
	}

	for _, opt := range opts {
		opt(scheduler)
	}

	return scheduler
}

// Add registers a new task with the scheduler using the provided ID.
// The task will be validated and scheduled according to its configuration.
// Returns an error if the ID is already in use or if the task configuration is invalid.
//
// Example:
//
//	task := &Task{
//	    Interval: "5s",
//	    TaskFunc: func(ctx context.Context) error {
//	        fmt.Println("Task executed")
//	        return nil
//	    },
//	}
//	err := scheduler.Add("my-task", task)
func (s *Scheduler) Add(id string, task *Task) error {
	s.logTaskAdd(id)

	if err := s.validate(task); err != nil {
		return err
	}

	task.ctx, task.cancel = context.WithCancel(context.Background())
	task.TaskContext.id = id

	s.Lock()
	defer s.Unlock()

	if _, ok := s.tasks[id]; ok {
		return ErrTaskIDInUse
	}

	task.id = id

	s.tasks[id] = task

	return s.scheduleTask(task)
}

// Del removes a task from the scheduler by its ID.
// The task's context will be cancelled and its timer will be stopped.
// If the task doesn't exist, this operation is a no-op.
func (s *Scheduler) Del(name string) {
	t, err := s.Lookup(name)
	if err != nil {
		return
	}

	defer t.cancel()

	t.Lock()
	defer t.Unlock()

	if t.timer != nil {
		defer t.timer.Stop()
	}

	s.Lock()
	defer s.Unlock()

	delete(s.tasks, name)
}

// Lookup retrieves a task by its ID. Returns a clone of the task to prevent
// external modifications to the original task state.
// Returns ErrTaskNotFound if the task doesn't exist.
func (s *Scheduler) Lookup(name string) (*Task, error) {
	s.RLock()
	defer s.RUnlock()

	t, ok := s.tasks[name]
	if ok {
		return t.Clone(), nil
	}

	return t, ErrTaskNotFound
}

// Tasks returns a map of all scheduled tasks, keyed by their IDs.
// The returned tasks are clones to prevent external modifications.
// This method is useful for inspecting the current state of all scheduled tasks.
func (s *Scheduler) Tasks() map[string]*Task {
	s.RLock()
	defer s.RUnlock()

	m := make(map[string]*Task)

	for k, v := range s.tasks {
		m[k] = v.Clone()
	}

	return m
}

// scheduleTask sets up the initial scheduling for a task based on its configuration.
// It parses the interval, handles FirstRun and StartAfter settings, and creates
// the appropriate timer for task execution.
func (s *Scheduler) scheduleTask(t *Task) error {
	var err error
	now := time.Now()

	t.interval, err = s.getInterval(t.Interval, now)
	if err != nil {
		return ErrInvalidTaskInterval
	}

	runFn := func() {
		s.execTask(t)
		if !t.FirstRun && !t.RunOnce {
			s.logNextRun(t)
		}
	}

	if t.FirstRun {
		runFn()
	}

	_ = time.AfterFunc(time.Until(t.StartAfter), func() {
		var err error

		t.safeOps(func() { err = t.ctx.Err() })
		if err != nil {
			return
		}

		t.safeOps(func() {
			t.timer = time.AfterFunc(t.interval, runFn)
		})
	})

	s.logNextRun(t)

	return nil
}

// getInterval parses the interval string and returns the corresponding duration.
// It supports both standard Go duration strings (e.g., "5s", "1m", "1h") and
// cron expressions (e.g., "0 0 * * *"). For cron expressions, it calculates
// the time until the next execution.
func (s *Scheduler) getInterval(intervalStr string, now time.Time) (
	time.Duration,
	error,
) {
	d, err := time.ParseDuration(intervalStr)
	if err == nil {
		return d, nil
	}

	expr, err := cronexpr.Parse(intervalStr)
	if err != nil {
		return 0, err
	}

	next := expr.Next(now)

	return next.Sub(now), nil
}

// execTask executes a task in a separate goroutine, handling various execution modes
// and error scenarios. It respects the RunSingleInstance setting, calls appropriate
// error handlers, and manages task lifecycle for RunOnce tasks.
func (s *Scheduler) execTask(t *Task) {
	go func() {
		start := time.Now()

		if t.RunSingleInstance {
			if !t.running.TryLock() {
				return
			}

			defer t.running.Unlock()
		}

		var err error
		if t.FuncWithTaskContext != nil {
			err = t.FuncWithTaskContext(t.TaskContext)
		} else {
			err = t.TaskFunc(t.ctx)
		}

		duration := time.Since(start)

		s.logTaskFinished(t, duration)

		if err != nil && (t.ErrFunc != nil || t.ErrFuncWithTaskContext != nil) {
			if t.ErrFuncWithTaskContext != nil {
				go t.ErrFuncWithTaskContext(t.TaskContext, err)
			} else {
				go t.ErrFunc(err)
			}
		}

		if t.FirstRun {
			t.FirstRun = false
		}

		if t.RunOnce {
			defer s.Del(t.id)
		}
	}()

	if !t.RunOnce {
		t.safeOps(func() {
			if t.timer != nil {
				t.timer.Reset(t.interval)
			}
		})
	}
}

func (s *Scheduler) validate(t *Task) error {
	if t.TaskFunc == nil && t.FuncWithTaskContext == nil {
		return ErrTaskFuncNil
	}

	if len(t.Interval) == 0 {
		return ErrTaskInterval
	}

	return nil
}

func (s *Scheduler) logTaskAdd(id string) {
	if s.logger == nil {
		return
	}

	if s.logger != nil {
		s.logger.WithField("task_id", id).
			Info("Adding task to scheduler manager...")
	}
}

func (s *Scheduler) logTaskFinished(t *Task, duration time.Duration) {
	if s.logger == nil {
		return
	}

	s.logger.
		WithField("task_id", t.id).
		WithField("duration", duration.String()).
		Debug("Task execution finished")
}

func (s *Scheduler) logNextRun(t *Task) {
	if s.logger == nil {
		return
	}

	var nextRunStr string

	nextRunTime := time.Now()
	nextRunStr = nextRunTime.String()

	s.logger.
		WithField("next_run", nextRunStr).
		WithField("task_id", t.id).
		Debug("Task scheduled successfully")
}
