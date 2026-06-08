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
	mu               sync.Mutex
	store            *Store
	handler          TickHandler
	cancels          map[string]context.CancelFunc
	running          map[string]*atomic.Bool
	consecutiveFails map[string]int
}

func NewScheduler(store *Store) *Scheduler {
	return &Scheduler{
		store:            store,
		cancels:          make(map[string]context.CancelFunc),
		running:          make(map[string]*atomic.Bool),
		consecutiveFails: make(map[string]int),
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

	s.rescan()

	// Watch for new jobs added via TUI/CLI while running.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.rescan()
		}
	}()
}

// rescan loads jobs from the store and starts any not yet running.
func (s *Scheduler) rescan() {
	s.mu.Lock()
	known := make(map[string]bool, len(s.cancels))
	for id := range s.cancels {
		known[id] = true
	}
	s.mu.Unlock()

	for _, job := range s.store.List() {
		if !job.Enabled {
			continue
		}
		s.mu.Lock()
		_, exists := s.cancels[job.ID]
		s.mu.Unlock()
		if !exists {
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

	s.mu.Lock()
	delete(s.running, id)
	delete(s.consecutiveFails, id)
	s.mu.Unlock()

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
	runFlag, exists := s.running[job.ID]
	if !exists {
		runFlag = &atomic.Bool{}
		s.running[job.ID] = runFlag
	}
	s.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("scheduler: job goroutine panicked, restarting", "job", job.ID, "name", job.Name, "panic", r)
				s.stopJob(job.ID)
				s.startJob(job)
			}
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.safeTick(job, runFlag)

		for {
			select {
			case <-ticker.C:
				s.safeTick(job, runFlag)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Scheduler) safeTick(job *Job, runFlag *atomic.Bool) {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			s.consecutiveFails[job.ID]++
			fails := s.consecutiveFails[job.ID]
			s.mu.Unlock()

			slog.Error("scheduler: tick panicked", "job", job.ID, "name", job.Name, "consecutive_fails", fails, "panic", r)

			if fails >= 3 {
				slog.Error("scheduler: too many consecutive panics, disabling job", "job", job.ID, "name", job.Name)
				job.Enabled = false
				s.store.Save(job)
				s.stopJob(job.ID)
			}
		}
	}()
	s.tick(job, runFlag)

	// Reset consecutive failures on success.
	s.mu.Lock()
	delete(s.consecutiveFails, job.ID)
	s.mu.Unlock()
}

func (s *Scheduler) stopJob(id string) {
	s.mu.Lock()
	if cancel, ok := s.cancels[id]; ok {
		cancel()
		delete(s.cancels, id)
	}
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
