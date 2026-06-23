package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job represents a scheduled task.
type Job struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Schedule         string            `json:"schedule"` // cron expression: "*/5 * * * *" (every 5 min)
	Prompt           string            `json:"prompt"`   // message to send to the agent
	Enabled          bool              `json:"enabled"`
	EnabledToolsets  []string          `json:"enabled_toolsets,omitempty"`
	Delivery         []string          `json:"delivery,omitempty"`
	Silent           bool              `json:"silent,omitempty"`
	LastRun          int64             `json:"last_run,omitempty"`
	LastStatus       string            `json:"last_status,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	CreatedAt        int64             `json:"created_at"`
	UpdatedAt        int64             `json:"updated_at"`
	Extra            map[string]string `json:"extra,omitempty"`
}

// JobResult holds the output of a completed cron job.
type JobResult struct {
	JobID      string `json:"job_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	Duration   int64  `json:"duration_ms"`
}

// Executor runs a cron job's agent task.
type Executor interface {
	ExecuteJob(ctx context.Context, job Job) (*JobResult, error)
}

// DeliveryHandler sends job results to targets.
type DeliveryHandler interface {
	Deliver(ctx context.Context, job Job, result *JobResult) error
}

// Scheduler manages cron jobs with file-based persistence.
// It uses a simple 1-minute ticker to check for due jobs.
// Supports standard 5-field cron expressions: minute hour day month weekday.
type Scheduler struct {
	mu       sync.Mutex
	jobs     map[string]*Job
	ticker   *time.Ticker
	cancel   context.CancelFunc
	executor Executor
	delivery DeliveryHandler
	dataDir  string
	running  bool
}

// NewScheduler creates a new cron scheduler.
func NewScheduler(dataDir string, executor Executor, delivery DeliveryHandler) (*Scheduler, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cron data dir: %w", err)
	}

	s := &Scheduler{
		jobs:     make(map[string]*Job),
		executor: executor,
		delivery: delivery,
		dataDir:  dataDir,
	}

	if err := s.loadJobs(); err != nil {
		slog.Warn("Failed to load cron jobs", "error", err)
	}

	return s, nil
}

// Start begins the scheduler's ticker (checks every minute).
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	tickerCtx, cancel := context.WithCancel(ctx)
	s.ticker = time.NewTicker(1 * time.Minute)
	s.cancel = cancel
	s.running = true

	go s.tickLoop(tickerCtx)
	slog.Info("Cron scheduler started")
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	if s.cancel != nil {
		s.cancel()
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}
	s.running = false
	slog.Info("Cron scheduler stopped")
}

// AddJob creates a new cron job.
func (s *Scheduler) AddJob(ctx context.Context, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		job.ID = fmt.Sprintf("cron_%d", time.Now().UnixNano())
	}
	if job.CreatedAt == 0 {
		job.CreatedAt = time.Now().Unix()
	}
	job.UpdatedAt = time.Now().Unix()

	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("job %q already exists", job.ID)
	}

	// Validate cron expression
	if err := validateCronExpr(job.Schedule); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", job.Schedule, err)
	}

	s.jobs[job.ID] = &job
	return s.saveJobsLocked()
}

// UpdateJob modifies an existing cron job.
func (s *Scheduler) UpdateJob(ctx context.Context, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.ID]; !ok {
		return fmt.Errorf("job %q not found", job.ID)
	}

	if job.Schedule != s.jobs[job.ID].Schedule {
		if err := validateCronExpr(job.Schedule); err != nil {
			return fmt.Errorf("invalid schedule %q: %w", job.Schedule, err)
		}
	}

	job.UpdatedAt = time.Now().Unix()
	s.jobs[job.ID] = &job
	return s.saveJobsLocked()
}

// DeleteJob removes a cron job.
func (s *Scheduler) DeleteJob(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return fmt.Errorf("job %q not found", id)
	}

	delete(s.jobs, id)
	return s.saveJobsLocked()
}

// GetJob returns a job by ID.
func (s *Scheduler) GetJob(ctx context.Context, id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	return job, nil
}

// ListJobs returns all cron jobs.
func (s *Scheduler) ListJobs(ctx context.Context) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}
	return jobs
}

// tickLoop checks for due jobs every minute.
func (s *Scheduler) tickLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ticker.C:
			s.checkAndRun(ctx)
		}
	}
}

// checkAndRun finds due jobs and executes them.
func (s *Scheduler) checkAndRun(ctx context.Context) {
	now := time.Now()

	s.mu.Lock()
	due := make([]Job, 0)
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		if isDue(job.Schedule, now, job.LastRun) {
			due = append(due, *job)
		}
	}
	s.mu.Unlock()

	for _, job := range due {
		s.runJob(ctx, job)
	}
}

// runJob executes a cron job and delivers the result.
func (s *Scheduler) runJob(ctx context.Context, job Job) {
	if s.executor == nil {
		slog.Warn("No executor configured for cron job", "job_id", job.ID)
		return
	}

	slog.Info("Running cron job", "job_id", job.ID, "name", job.Name)

	// Mark as running
	s.mu.Lock()
	job.LastStatus = "running"
	job.LastRun = time.Now().Unix()
	if j, ok := s.jobs[job.ID]; ok {
		j.LastStatus = job.LastStatus
		j.LastRun = job.LastRun
	}
	s.mu.Unlock()

	start := time.Now()
	result, err := s.executor.ExecuteJob(ctx, job)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		result = &JobResult{
			JobID:     job.ID,
			Status:    "failed",
			Error:     err.Error(),
			StartedAt: start.Unix(),
		}
	}
	result.FinishedAt = time.Now().Unix()
	result.Duration = duration

	// Update status
	s.mu.Lock()
	if j, ok := s.jobs[job.ID]; ok {
		j.LastStatus = result.Status
		j.LastError = result.Error
		j.UpdatedAt = time.Now().Unix()
	}
	_ = s.saveJobsLocked()
	s.mu.Unlock()

	// Deliver
	if s.delivery != nil && !job.Silent {
		if err := s.delivery.Deliver(ctx, job, result); err != nil {
			slog.Error("Failed to deliver cron result", "job_id", job.ID, "error", err)
		}
	}

	s.saveResult(result)
	slog.Info("Cron job completed", "job_id", job.ID, "status", result.Status, "duration_ms", duration)
}

// saveResult persists a job result.
func (s *Scheduler) saveResult(result *JobResult) {
	dir := filepath.Join(s.dataDir, "results", result.JobID)
	os.MkdirAll(dir, 0o700)

	data, _ := json.MarshalIndent(result, "", "  ")
	path := filepath.Join(dir, fmt.Sprintf("%d.json", result.FinishedAt))
	os.WriteFile(path, data, 0o600)
}

// loadJobs loads jobs from disk.
func (s *Scheduler) loadJobs() error {
	path := filepath.Join(s.dataDir, "jobs.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var jobs []Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return fmt.Errorf("parse jobs: %w", err)
	}

	for i := range jobs {
		j := jobs[i]
		s.jobs[j.ID] = &j
	}
	return nil
}

// saveJobsLocked persists jobs to disk.
func (s *Scheduler) saveJobsLocked() error {
	jobs := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.dataDir, "jobs.json")
	return os.WriteFile(path, data, 0o600)
}

// isDue checks if a job should run based on its cron schedule, the current time,
// and when it last ran.
func isDue(schedule string, now time.Time, lastRun int64) bool {
	if lastRun > 0 {
		last := time.Unix(lastRun, 0)
		if now.Sub(last) < 55*time.Second {
			return false // ran less than a minute ago
		}
	}

	fields := strings.Fields(schedule)
	if len(fields) < 5 {
		return false
	}

	// Simple matching: minute hour day month weekday
	// Supports: specific values, */N ranges, and wildcard *
	return matchField(fields[0], now.Minute()) &&
		matchField(fields[1], now.Hour()) &&
		matchField(fields[2], now.Day()) &&
		matchField(fields[3], int(now.Month())) &&
		matchField(fields[4], int(now.Weekday()))
}

// matchField checks if a cron field value matches the current value.
// Supports: *, specific number, */N
func matchField(field string, value int) bool {
	if field == "*" {
		return true
	}

	if strings.HasPrefix(field, "*/") {
		stepStr := strings.TrimPrefix(field, "*/")
		step, err := strconv.Atoi(stepStr)
		if err != nil || step <= 0 {
			return false
		}
		return value%step == 0
	}

	// Comma-separated values
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if n, err := strconv.Atoi(part); err == nil && n == value {
			return true
		}
		// Range: N-M
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) == 2 {
				lo, err1 := strconv.Atoi(rangeParts[0])
				hi, err2 := strconv.Atoi(rangeParts[1])
				if err1 == nil && err2 == nil && value >= lo && value <= hi {
					return true
				}
			}
		}
	}

	return false
}

// validateCronExpr validates a 5-field cron expression.
func validateCronExpr(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("expected 5 fields (minute hour day month weekday), got %d", len(fields))
	}
	// Basic validation: each field should be *, a number, or */N
	for i, field := range fields {
		if field == "*" {
			continue
		}
		if strings.HasPrefix(field, "*/") {
			if _, err := strconv.Atoi(strings.TrimPrefix(field, "*/")); err != nil {
				return fmt.Errorf("field %d: invalid step expression %q", i, field)
			}
			continue
		}
		if _, err := strconv.Atoi(field); err != nil {
			// Try range
			if !strings.Contains(field, "-") && !strings.Contains(field, ",") {
				return fmt.Errorf("field %d: invalid value %q", i, field)
			}
		}
	}
	return nil
}
