package jobmanager

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"

	"github.com/nixpig/jobworker/internal/jobmanager/output"
)

// Job represents a process executed using exec.Cmd. It provides management of
// the Job's lifecycle and safe concurrent streaming of the process' combined
// stdout/stderr.
type Job struct {
	id          string
	state       AtomicJobState
	interrupted atomic.Bool

	cmd            *exec.Cmd
	processState   atomic.Pointer[os.ProcessState]
	outputStreamer *output.Streamer
	pipeWriter     io.WriteCloser

	done chan struct{}
}

// JobStatus represents the status of a Job, including its state, exit code,
// and whether its execution was interrupted.
type JobStatus struct {
	State       JobState
	ExitCode    int
	Interrupted bool
}

// NewJob creates a new Job with the given id, program and args. It configures
// an output.Streamer for concurrent streaming of process output.
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
		id:             id,
		cmd:            cmd,
		outputStreamer: output.NewStreamer(pr),
		pipeWriter:     pw,
		done:           make(chan struct{}),
	}

	j.state.Store(JobStateCreated)

	return j, nil
}

// Start starts the Job. Trying to start a Job that is not in JobStateCreated
// returns an InvalidStateError.
func (j *Job) Start() error {
	if !j.state.CompareAndSwap(JobStateCreated, (JobStateStarting)) {
		return NewInvalidStateError(j.state.Load(), JobStateStarting)
	}

	// TODO: Create a new cgroup and add the process to it.

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
		j.processState.Store(j.cmd.ProcessState)

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

	// TODO: When cgroups are implemented, use those to kill the process.
	// In the meantime, just use cmd.Process.Kill() and accept the small risk.
	return j.cmd.Process.Kill()
}

// ID returns the ID of the Job.
func (j *Job) ID() string {
	return j.id
}

// State returns the state of the Job.
func (j *Job) State() JobState {
	return j.state.Load()
}

// Interrupted returns whether the Job interrupted execution of the process.
func (j *Job) Interrupted() bool {
	return j.interrupted.Load()
}

// ExitCode returns the exit code of the process or -1 if the process hasn't
// exited or was interrupted.
func (j *Job) ExitCode() int {
	ps := j.processState.Load()
	if ps == nil {
		return -1
	}

	return ps.ExitCode()
}

// StreamOutput returns an io.ReadCloser of output from the Job.
//
// Read returns all output since the Job started and block waiting for new
// output.
func (j *Job) StreamOutput() io.ReadCloser {
	return j.outputStreamer.Subscribe()
}

// Done returns a channel that is closed when the Job has completed and the
// process has exited.
func (j *Job) Done() <-chan struct{} {
	return j.done
}

// Status returns the status of the Job.
func (j *Job) Status() *JobStatus {
	return &JobStatus{
		State:       j.state.Load(),
		ExitCode:    j.ExitCode(),
		Interrupted: j.interrupted.Load(),
	}
}

func (j *Job) cleanup() {
	// TODO: Remove cgroup when implemented.
}
