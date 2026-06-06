package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type TickHandler func(job *Job, prompt string)

type Scheduler struct {
	mu      sync.Mutex
	store   *Store
	handler TickHandler
	cancels map[string]context.CancelFunc
	running map[string]*atomic.Bool
}

func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{
		store:   store,
		cancels: make(map[string]context.CancelFunc),
		running: make(map[string]*atomic.Bool),
	}
}

func (s *Scheduler) SetTickHandler(h TickHandler) {
	s.handler = h
}

var started atomic.Bool

func (s *Scheduler) Start() {
	if !started.CompareAndSwap(false, true) {
		return
	}

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

	go func() {
		s.tick(job, runFlag)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.tick(job, runFlag)
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

func (s *Scheduler) tick(job *Job, runFlag *atomic.Bool) {
	if !runFlag.CompareAndSwap(false, true) {
		slog.Warn("scheduler: previous run still in progress, skipping", "job", job.ID, "name", job.Name)
		return
	}
	defer runFlag.Store(false)

	slog.Info("scheduler: tick", "job", job.ID, "name", job.Name)

	now := time.Now()
	job.LastRunAt = now
	job.RunCount++
	job.LastResult = "queued"
	s.store.Save(job)

	if s.handler != nil {
		prompt := job.Prompt
		if job.Continue {
			prompt += "\n\n" + DefaultContinuePrompt
		}
		s.handler(job, prompt)
	}
}
