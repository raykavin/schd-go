package goschd

// SchedulerOptions defines configuration options for the Scheduler.
type SchedulerOpts func(*Scheduler)

// WithLogger sets a custom logger for the Scheduler.
func WithLogger(logger Logger) SchedulerOpts {
	return func(s *Scheduler) {
		s.logger = logger
	}
}
