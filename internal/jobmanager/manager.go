package jobmanager

import (
	"io"
	"sync"

	"github.com/google/uuid"
)

// Manager is responsible for creating and tracking jobs, managing the
// lifecycle of jobs, and coordinating the output of jobs for streaming.
// It ensures safe concurrent access to a collection of jobs.
type Manager struct {
	// NOTE: The jobs map will grow unbounded with no way to remove items. Since
	// the stated assumption is 'everything will fit in memory', that's fine.
	// In a real system, we'd want to provide a means to remove items and
	// potentially a cleanup job running in a background goroutine.
	// NOTE: Currently this is a map of the concrete implementation of Job, which
	// is fine for now, since we only have one kind of Job. In future, if we
	// had other kinds of Job, like RemoteJob, ScheduledJob, BatchJob, whatever,
	// then refactor this to an interface.
	jobs map[string]*Job

	mu sync.RWMutex
}

// NewManager creates a job manager in a state that is ready to accept new
// jobs to run.
func NewManager() *Manager {
	return &Manager{
		jobs: make(map[string]*Job),
	}
}

// RunJob creates a UUID and creates a new Job with the provided program, args,
// and created UUID. It then adds the job to the collection in the Manager
// and starts the job.
func (m *Manager) RunJob(
	program string,
	args []string,
) (string, error) {
	id := uuid.NewString()

	job, err := NewJob(id, program, args)
	if err != nil {
		return "", err
	}

	if err := job.Start(); err != nil {
		return "", err
	}

	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	return id, nil
}

// StopJob calls the Stop method on the job with the given id.
func (m *Manager) StopJob(id string) error {
	job, err := m.getJob(id)
	if err != nil {
		return err
	}

	return job.Stop()
}

// QueryJob gets the Status of the job.
func (m *Manager) QueryJob(id string) (*JobStatus, error) {
	job, err := m.getJob(id)
	if err != nil {
		return nil, err
	}

	return job.Status(), nil
}

// StreamJobOutput returns an io.ReadCloser with output from the job with the
// given id. Call Close on the io.ReadCloser to terminate the stream and clean
// up.
func (m *Manager) StreamJobOutput(id string) (io.ReadCloser, error) {
	job, err := m.getJob(id)
	if err != nil {
		return nil, err
	}

	return job.StreamOutput(), nil
}

func (m *Manager) Shutdown() {
	m.mu.RLock()
	jobs := make([]*Job, 0, len(m.jobs))

	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup

	for _, job := range jobs {
		state := job.State()
		if state == JobStateStarted || state == JobStateStarting {
			wg.Go(func() {
				if err := job.Stop(); err != nil {
					// NOTE: In the context of a Shutdown, treating Stop as a 'best
					// effort' and ignoring any errors.

					// TODO: If observability was in scope, we could bubble these errors,
					// log them, and capture relevent metrics.
				}
			})
		}
	}

	wg.Wait()
}

func (m *Manager) getJob(id string) (*Job, error) {
	m.mu.RLock()
	job, exists := m.jobs[id]
	m.mu.RUnlock()

	if !exists {
		return nil, ErrJobNotFound
	}

	return job, nil
}
