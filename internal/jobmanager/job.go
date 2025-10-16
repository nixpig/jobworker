package jobmanager

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"

	"github.com/nixpig/jobworker/internal/jobmanager/output"
)

// Job is an abstraction around a process executed using exec.Cmd that enables
// safe concurrent streaming of the process' stdout/stderr (combined).
type Job struct {
	id          string
	state       AtomicJobState
	interrupted atomic.Bool

	cmd           *exec.Cmd
	processState  *os.ProcessState
	outputManager *output.Manager
	pipeWriter    io.WriteCloser

	done chan struct{}
}

type JobStatus struct {
	State       JobState
	ExitCode    int
	Interrupted bool
}

// NewJob creates a Job with the given id that will execute the program with
// the provided args. It configures an output.Manager for concurrent streaming
// of process output.
func NewJob(
	id string,
	program string,
	args []string,
) (*Job, error) {
	if program == "" {
		return nil, fmt.Errorf("program cannot be empty")
	}

	cmd := exec.Command(program, args...)

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create os pipe: %w", err)
	}

	cmd.Stdout = pw
	cmd.Stderr = pw

	j := &Job{
		id:            id,
		cmd:           cmd,
		outputManager: output.NewManager(pr),
		pipeWriter:    pw,
		done:          make(chan struct{}),
	}

	j.state.Store(JobStateCreated)

	return j, nil
}

// Start starts the Job. Trying to start a Job that is not in JobStateCreated
// returns an InvalidStateError.
func (j *Job) Start() error {
	if !j.state.CompareAndSwap(JobStateCreated, (JobStateStarting)) {
		return NewInvalidStateError(j.state.Load(), JobStateStarted)
	}

	// FIXME: Create a new cgroup and add the process to it.

	if err := j.cmd.Start(); err != nil {
		j.state.Store(JobStateFailed)

		j.pipeWriter.Close()

		return fmt.Errorf("failed to start process: %w", err)
	}

	j.pipeWriter.Close()

	j.state.Store(JobStateStarted)

	go func() {
		j.cmd.Wait()

		j.state.Store(JobStateStopped)
		j.processState = j.cmd.ProcessState

		close(j.done)

		j.cleanup()
	}()

	return nil
}

// Stop stops the Job. Trying to stop a Job that is not in JobStateStarted
// returns an InvalidStateError.
func (j *Job) Stop() error {
	if !j.state.CompareAndSwap(JobStateStarted, JobStateStopping) {
		return NewInvalidStateError(j.state.Load(), JobStateStopping)
	}

	j.interrupted.Store(true)

	// FIXME: When cgroups are implemented, we'll use those to kill the process.
	// In the meantime, just use cmd.Process.Kill() and accept the small risk.
	return j.cmd.Process.Kill()
}

// ID returns the ID of the Job.
func (j *Job) ID() string {
	return j.id
}

// State returns the state of the job.
func (j *Job) State() JobState {
	return j.state.Load()
}

// Interrupted returns whether the Job execution was prematurely stopped.
func (j *Job) Interrupted() bool {
	return j.interrupted.Load()
}

// ExitCode returns the exit code of the process. In the case no exit code is
// available, such as when the process hasn't yet exited, then -1 will be
// returned.
func (j *Job) ExitCode() int {
	if j.processState == nil {
		return -1
	}

	return j.processState.ExitCode()
}

// StreamOutput returns an io.ReadCloser that streams the combined
// stdout/stderr output from the process. Multiple clients can subscribe and
// each will receive the full output from the beginning.
func (j *Job) StreamOutput() io.ReadCloser {
	return j.outputManager.Subscribe()
}

// Done returns a channel that is closed when the job is closed and the
// process has exited.
func (j *Job) Done() <-chan struct{} {
	return j.done
}

// Status returns the status of the job.
func (j *Job) Status() *JobStatus {
	return &JobStatus{
		State:       j.state.Load(),
		ExitCode:    j.ExitCode(),
		Interrupted: j.interrupted.Load(),
	}
}

func (j *Job) cleanup() {
	// NOTE: Output manager cleanup happens automatically when source pipe closes.

	// FIXME: Remove cgroup when implemented.

}
