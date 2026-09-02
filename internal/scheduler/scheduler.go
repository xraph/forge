// Package scheduler runs periodic work off one goroutine and one timer.
//
// Forge and its extensions want a lot of periodic work: health check rounds,
// metric collection, storage cleanup, compression, cache eviction, lease
// renewal. Written the obvious way, each of those is its own goroutine parked
// on its own time.Ticker. That cost is per subsystem and it accumulates with
// every extension an app installs, so an app is never fully idle: each ticker
// is a runtime timer that wakes a core to send on a channel nobody was waiting
// on except one parked goroutine.
//
// A Scheduler holds every job in one heap ordered by next run, so the process
// arms exactly one timer for the earliest of them and sleeps until then.
package scheduler

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

// Runner is the work a job does on each run. The context is cancelled when the
// scheduler stops, so a long job can notice and return.
type Runner func(ctx context.Context)

// Scheduler runs registered jobs on their intervals.
//
// The zero value is not usable; call New.
type Scheduler struct {
	name string

	mu      sync.Mutex
	jobs    jobHeap
	nextID  uint64
	started bool
	wake    chan struct{}

	stop   context.CancelFunc
	ctx    context.Context
	done   chan struct{}
	timer  *time.Timer
	inWork sync.WaitGroup
}

// New creates a scheduler. Jobs may be registered before or after Start.
func New(name string) *Scheduler {
	return &Scheduler{
		name: name,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

// Name reports the scheduler's name, for logs and diagnostics.
func (s *Scheduler) Name() string { return s.name }

// Every registers fn to run every interval and returns a function that
// cancels it. A non-positive interval registers nothing and returns a no-op.
//
// The first run happens one interval from now, matching a ticker.
//
// A job never overlaps itself: if a run is still going when the next one is
// due, that run is skipped rather than queued. Runs happen on their own
// goroutines, so a slow job delays only itself.
func (s *Scheduler) Every(name string, interval time.Duration, fn Runner) (cancel func()) {
	if interval <= 0 || fn == nil {
		return func() {}
	}

	s.mu.Lock()

	s.nextID++
	j := &job{
		id:       s.nextID,
		name:     name,
		interval: interval,
		run:      fn,
		next:     time.Now().Add(interval),
	}

	heap.Push(&s.jobs, j)
	s.mu.Unlock()

	s.signal()

	return func() { s.cancel(j.id) }
}

// Start begins running jobs. It is idempotent.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()

	if s.started {
		s.mu.Unlock()

		return nil
	}

	s.started = true
	s.ctx, s.stop = context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Unlock()

	go s.loop()

	return nil
}

// Stop halts the scheduler and waits for any in-flight job runs to return or
// for ctx to expire. It is idempotent.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()

	if !s.started {
		s.mu.Unlock()

		return nil
	}

	s.started = false
	stop := s.stop
	s.mu.Unlock()

	stop()
	<-s.done

	// Wait for running jobs, but not past the caller's deadline.
	finished := make(chan struct{})

	go func() {
		s.inWork.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Len reports how many jobs are registered.
func (s *Scheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.jobs.Len()
}

// signal nudges the loop to recompute its sleep, for a job added or removed
// ahead of the one it is currently waiting for.
func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}

func (s *Scheduler) cancel(id uint64) {
	s.mu.Lock()

	for i, j := range s.jobs {
		if j.id == id {
			heap.Remove(&s.jobs, i)

			break
		}
	}

	s.mu.Unlock()
	s.signal()
}

// loop is the single goroutine. It sleeps until the earliest job is due, runs
// everything that has come due, and sleeps again.
func (s *Scheduler) loop() {
	defer close(s.done)

	// One timer for the whole scheduler, rearmed rather than recreated.
	s.timer = time.NewTimer(time.Hour)
	defer s.timer.Stop()

	for {
		wait, ok := s.untilNext()

		if !ok {
			// Nothing registered: sleep until a job shows up or we stop.
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
				continue
			}
		}

		s.timer.Reset(wait)

		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			// A job was added or removed; recompute the wait.
			if !s.timer.Stop() {
				select {
				case <-s.timer.C:
				default:
				}
			}
		case <-s.timer.C:
			s.runDue()
		}
	}
}

// untilNext reports how long until the earliest job is due, and whether there
// is one at all.
func (s *Scheduler) untilNext() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.jobs.Len() == 0 {
		return 0, false
	}

	return max(time.Until(s.jobs[0].next), 0), true
}

// runDue starts every job whose time has come and schedules its next run.
func (s *Scheduler) runDue() {
	now := time.Now()

	s.mu.Lock()

	var due []*job

	for s.jobs.Len() > 0 && !s.jobs[0].next.After(now) {
		j := s.jobs[0]

		// Reschedule from now, not from the missed deadline, so a job that
		// runs long or a process that was descheduled does not come back to a
		// backlog of overdue runs.
		j.next = now.Add(j.interval)
		heap.Fix(&s.jobs, 0)

		if j.running {
			continue // previous run still going; skip this one
		}

		j.running = true

		due = append(due, j)
	}

	s.mu.Unlock()

	for _, j := range due {
		s.inWork.Add(1)

		go func(j *job) {
			defer func() {
				// A job is arbitrary subsystem code. A panic here would take
				// the process down from a goroutine nobody can recover in.
				_ = recover()

				s.mu.Lock()
				j.running = false
				s.mu.Unlock()

				s.inWork.Done()
			}()

			j.run(s.ctx)
		}(j)
	}
}

// job is one registered piece of periodic work.
type job struct {
	id       uint64
	name     string
	interval time.Duration
	run      Runner
	next     time.Time
	running  bool
	index    int
}

// jobHeap orders jobs by next run time.
type jobHeap []*job

func (h jobHeap) Len() int           { return len(h) }
func (h jobHeap) Less(i, j int) bool { return h[i].next.Before(h[j].next) }
func (h *jobHeap) Push(x any)        { j := x.(*job); j.index = len(*h); *h = append(*h, j) }
func (h jobHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index, h[j].index = i, j }

func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	j := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]

	return j
}

// defaultScheduler is the process-wide scheduler that framework subsystems
// share.
//
// One process wants one timer wheel. Giving every subsystem its own Scheduler
// would rebuild the problem this package exists to solve, one level up: the
// goroutine and timer would be per subsystem again rather than per ticker.
var (
	defaultOnce      sync.Once
	defaultSchedular *Scheduler
)

// Default returns the shared scheduler, starting it on first use.
//
// It has no Stop: it belongs to the process, not to any one app, and it costs
// one parked goroutine when nothing is registered. Cancel individual jobs with
// the function Every returns.
func Default() *Scheduler {
	defaultOnce.Do(func() {
		defaultSchedular = New("forge")
		_ = defaultSchedular.Start(context.Background())
	})

	return defaultSchedular
}
