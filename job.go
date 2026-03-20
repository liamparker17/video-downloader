package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Job represents a download job with its current state.
type Job struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	PageURL   string    `json:"pageUrl"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Progress  float64   `json:"progress"`
	Speed     string    `json:"speed"`
	Filename  string    `json:"filename"`
	Error     string    `json:"error,omitempty"`
	AudioOnly bool      `json:"audioOnly"`
	Quality   string    `json:"quality"`
	CreatedAt time.Time `json:"createdAt"`

	// Internal — not serialized
	ctx    context.Context
	cancel context.CancelFunc
}

// JobStore manages all download jobs in memory.
type JobStore struct {
	jobs sync.Map
	seq  uint64
	mu   sync.Mutex
}

var store = &JobStore{}

// CreateJob creates a new job in "pending" status and returns it.
func (s *JobStore) CreateJob(url, pageURL, title, quality string, audioOnly bool) *Job {
	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("%d_%d", time.Now().UnixMilli(), s.seq)
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:        id,
		URL:       url,
		PageURL:   pageURL,
		Title:     title,
		Status:    "pending",
		Quality:   quality,
		AudioOnly: audioOnly,
		CreatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}
	s.jobs.Store(id, job)
	return job
}

// GetJob retrieves a job by ID.
func (s *JobStore) GetJob(id string) (*Job, bool) {
	val, ok := s.jobs.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Job), true
}

// ListJobs returns all jobs.
func (s *JobStore) ListJobs() []*Job {
	var result []*Job
	s.jobs.Range(func(_, val any) bool {
		result = append(result, val.(*Job))
		return true
	})
	return result
}

// CancelJob cancels a running job.
func (s *JobStore) CancelJob(id string) bool {
	job, ok := s.GetJob(id)
	if !ok {
		return false
	}
	if job.Status == "completed" || job.Status == "failed" {
		s.jobs.Delete(id)
		return true
	}
	job.cancel()
	job.Status = "failed"
	job.Error = "Cancelled by user"
	return true
}

// StartCleanup runs a background goroutine that evicts completed/failed jobs older than 1 hour.
func (s *JobStore) StartCleanup() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			s.jobs.Range(func(key, val any) bool {
				job := val.(*Job)
				if (job.Status == "completed" || job.Status == "failed") &&
					now.Sub(job.CreatedAt) > time.Hour {
					s.jobs.Delete(key)
					log.Printf("[CLEANUP] Evicted job %s", job.ID)
				}
				return true
			})
		}
	}()
}
