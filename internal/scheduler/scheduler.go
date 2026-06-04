package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type RunFunc func(ctx context.Context, prompt string) (string, error)

type Scheduler struct {
	mu      sync.Mutex
	store   *Store
	runFn   RunFunc
	cancels map[string]context.CancelFunc
	running map[string]*atomic.Bool
}

func NewScheduler(store *Store, runFn RunFunc) *Scheduler {
	return &Scheduler{
		store:   store,
		runFn:   runFn,
		cancels: make(map[string]context.CancelFunc),
		running: make(map[string]*atomic.Bool),
	}
}

var started atomic.Bool

func (s *Scheduler) Start(ctx context.Context) {
	if !started.CompareAndSwap(false, true) {
		return
	}

	go func() {
		<-ctx.Done()
		s.shutdown()
	}()

	for _, job := range s.store.List() {
		if job.Enabled {
			s.startJob(job)
		}
	}
}

func (s *Scheduler) AddJob(job *Job) error {
	if err := job.Validate(); err != nil {
		return err
	}
	if job.ID == "" {
		job.ID = jobID(job.Name)
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()

	if err := s.store.Save(job); err != nil {
		return err
	}

	if job.Enabled {
		s.startJob(job)
	}

	return nil
}

func (s *Scheduler) UpdateJob(job *Job) error {
	existing, ok := s.store.Get(job.ID)
	if !ok {
		return fmt.Errorf("job %q not found", job.ID)
	}

	job.CreatedAt = existing.CreatedAt
	job.LastRunAt = existing.LastRunAt
	job.LastResult = existing.LastResult
	job.RunCount = existing.RunCount
	job.UpdatedAt = time.Now()

	if err := job.Validate(); err != nil {
		return err
	}
	if err := s.store.Save(job); err != nil {
		return err
	}

	s.stopJob(job.ID)
	if job.Enabled {
		s.startJob(job)
	}

	return nil
}

func (s *Scheduler) DeleteJob(id string) error {
	s.stopJob(id)

	if !s.store.Delete(id) {
		return fmt.Errorf("job %q not found", id)
	}
	return nil
}

func (s *Scheduler) RunJob(id string) {
	job, ok := s.store.Get(id)
	if !ok {
		return
	}

	s.mu.Lock()
	runFlag, exists := s.running[id]
	if !exists {
		runFlag = &atomic.Bool{}
		s.running[id] = runFlag
	}
	s.mu.Unlock()

	if !runFlag.CompareAndSwap(false, true) {
		slog.Warn("scheduler: job already running, skipping manual run", "job", id)
		return
	}
	defer runFlag.Store(false)

	s.execute(job)
}

func (s *Scheduler) ListJobs() []*Job {
	return s.store.List()
}

func (s *Scheduler) GetJob(id string) (*Job, bool) {
	return s.store.Get(id)
}

func (s *Scheduler) startJob(job *Job) {
	interval, err := ParseInterval(job.Interval)
	if err != nil {
		slog.Error("scheduler: invalid interval", "job", job.ID, "interval", job.Interval, "err", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	s.cancels[job.ID] = cancel
	runFlag := &atomic.Bool{}
	s.running[job.ID] = runFlag
	s.mu.Unlock()

	// Run immediately on start, then on schedule.
	go func() {
		s.executeLocked(job, runFlag)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.executeLocked(job, runFlag)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Scheduler) stopJob(id string) {
	s.mu.Lock()
	if cancel, ok := s.cancels[id]; ok {
		cancel()
		delete(s.cancels, id)
	}
	delete(s.running, id)
	s.mu.Unlock()
}

func (s *Scheduler) executeLocked(job *Job, runFlag *atomic.Bool) {
	if !runFlag.CompareAndSwap(false, true) {
		slog.Warn("scheduler: job still running, skipping tick", "job", job.ID, "name", job.Name)
		return
	}
	defer runFlag.Store(false)

	s.execute(job)
}

func (s *Scheduler) execute(job *Job) {
	if s.runFn == nil {
		return
	}

	slog.Info("scheduler: executing job", "job", job.ID, "name", job.Name)

	prompt := job.Prompt
	if job.Continue {
		prompt += "\n\n" + DefaultContinuePrompt
	}

	ctx := context.Background()
	if job.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(job.TimeoutSec)*time.Second)
		defer cancel()
	}

	now := time.Now()
	result, err := s.runFn(ctx, prompt)

	job.LastRunAt = now
	job.RunCount++
	if err != nil {
		job.LastResult = fmt.Sprintf("error: %s", err)
	} else {
		job.LastResult = fmt.Sprintf("ok (%d chars)", len(result))
	}
	s.store.Save(job)
}

func (s *Scheduler) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.cancels {
		cancel()
		delete(s.cancels, id)
	}
}
