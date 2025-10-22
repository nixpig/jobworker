package jobmanager

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"

	"github.com/nixpig/jobworker/internal/jobmanager/cgroups"
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
	cgroup         *cgroups.Cgroup
	limits         *cgroups.ResourceLimits
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
	Signal      os.Signal
	Interrupted bool
}

// NewJob creates a new Job with the given id, program and args. It configures
// an output.Streamer for concurrent streaming of process output.
func NewJob(
	id string,
	program string,
	args []string,
	limits *cgroups.ResourceLimits,
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
		limits:         limits,
		outputStreamer: output.NewStreamer(pr),
		pipeWriter:     pw,
		done:           make(chan struct{}),
	}

	j.state.Store(JobStateCreated)

	return j, nil
}

// Start starts the Job. Trying to start a Job that is not in JobStateCreated
// returns an InvalidStateError.
func (j *Job) Start() (err error) {
	if !j.state.CompareAndSwap(JobStateCreated, (JobStateStarting)) {
		return NewInvalidStateError(j.state.Load(), JobStateStarting)
	}

	defer func() {
		if err != nil {
			j.state.Store(JobStateFailed)
			j.pipeWriter.Close()

			if j.cgroup != nil {
				j.cgroup.Destroy()
			}

			close(j.done)
		}
	}()

	cgroup, err := cgroups.CreateCgroup("job-manager-"+j.id, j.limits)
	if err != nil {
		return fmt.Errorf("create cgroup: %w", err)
	}

	j.cgroup = cgroup

	fd, err := j.cgroup.FD()
	if err != nil {
		return fmt.Errorf("open cgroup fd: %w", err)
	}
	defer fd.Close()

	j.cmd.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    int(fd.Fd()),
	}

	if err = j.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	j.pipeWriter.Close()
	j.state.Store(JobStateStarted)

	go func() {
		j.cmd.Wait()

		j.processState.Store(j.cmd.ProcessState)
		j.state.Store(JobStateStopped)
		j.cgroup.Destroy()

		close(j.done)
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

	// TODO: Implement graceful shutdown for production:
	// (SIGTERM -> timeout -> SIGKILL)
	// For now, just let cgroup.kill SIGKILL all processes immediately.
	return j.cgroup.Kill()
}

// ID returns the ID of the Job.
func (j *Job) ID() string {
	return j.id
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
	ps := j.processState.Load()

	var sig os.Signal

	exitCode := -1

	if ps != nil {
		exitCode = ps.ExitCode()

		if ps.Sys() != nil {
			if status, ok := ps.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					sig = status.Signal()
				}
			}
		}
	}

	return &JobStatus{
		State:       j.state.Load(),
		ExitCode:    exitCode,
		Signal:      sig,
		Interrupted: j.interrupted.Load(),
	}
}
