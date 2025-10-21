package jobmanager

import (
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/nixpig/jobworker/internal/jobmanager/cgroups"
)

// Manager is responsible for creating and managing Jobs.
type Manager struct {
	// NOTE: The jobs map will grow unbounded with no way to remove items. Since
	// the stated assumption is 'everything will fit in memory', that's fine.
	// In a real system, we'd want to provide a means to remove items and
	// potentially a cleanup job running in a background goroutine.
	// NOTE: Currently this is a map of the concrete implementation of Job, which
	// is fine for now, since we only have one kind of Job. In future, if we
	// had other kinds of Job, like RemoteJob, ScheduledJob, BatchJob, whatever,
	// then refactor this to an interface.
	jobs       map[string]*Job
	cgroupRoot string

	mu sync.Mutex
}

// NewManager creates a new Manager ready to run Jobs.
func NewManager(cgroupRoot string) (*Manager, error) {
	if err := cgroups.ValidateCgroupRoot(cgroupRoot); err != nil {
		return nil, err
	}

	return &Manager{
		jobs:       make(map[string]*Job),
		cgroupRoot: cgroupRoot,
	}, nil
}

func NewManagerWithDefaults() (*Manager, error) {
	return NewManager("/sys/fs/cgroup")
}

// RunJob creates and starts a new Job with the given program and args. It
// returns the Job's unique ID.
func (m *Manager) RunJob(
	program string,
	args []string,
	limits *cgroups.ResourceLimits,
) (string, error) {
	id := uuid.NewString()

	job, err := NewJob(id, program, args, m.cgroupRoot, limits)
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

// StopJob stops the Job with the given id or returns ErrJobNotFound if it
// doesn't exist.
func (m *Manager) StopJob(id string) error {
	job, err := m.GetJob(id)
	if err != nil {
		return err
	}

	return job.Stop()
}

// QueryJob returns the status of the Job with the given id or ErrJobNotFound
// if it doesn't exist.
func (m *Manager) QueryJob(id string) (*JobStatus, error) {
	job, err := m.GetJob(id)
	if err != nil {
		return nil, err
	}

	return job.Status(), nil
}

// StreamJobOutput returns an io.ReadCloser of output from the Job with the
// given id or ErrJobNotFound if it doesn't exist.
//
// Read will return all output since the Job started and block waiting for new
// output.
func (m *Manager) StreamJobOutput(id string) (io.ReadCloser, error) {
	job, err := m.GetJob(id)
	if err != nil {
		return nil, err
	}

	return job.StreamOutput(), nil
}

// Shutdown makes a 'best effort' attempt to stop any running Jobs managed by
// the Manager.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	jobs := slices.Collect(maps.Values(m.jobs))
	m.mu.Unlock()

	var wg sync.WaitGroup

	for _, job := range jobs {
		if job.Status().State == JobStateStarted {
			wg.Go(func() {
				if err := job.Stop(); err != nil {
					// NOTE: In the context of a Shutdown where we're not attempting
					// graceful shutdown (i.e. we're just going to SIGKILL), treating
					// Stop as a 'best effort' and ignoring any errors.

					// TODO: If observability was in scope, we could bubble these errors,
					// log them, and capture relevent metrics.
				}
			})
		}
	}

	wg.Wait()
}

// GetJob returns the Job with the given id or ErrJobNotFound if it doesn't
// exist.
func (m *Manager) GetJob(id string) (*Job, error) {
	m.mu.Lock()
	job, exists := m.jobs[id]
	m.mu.Unlock()

	if !exists {
		return nil, ErrJobNotFound
	}

	return job, nil
}
