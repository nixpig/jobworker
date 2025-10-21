package jobmanager

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	cgroupRoot  string

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
	cgroupRoot string,
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

	cg, err := cgroups.CreateCgroup(j.cgroupRoot, j.id, j.limits)
	if err != nil {
		return fmt.Errorf("create cgroup: %w", err)
	}

	j.cgroup = cg

	// TODO: Create a new cgroup and add the process to it.
	fd := j.cgroup.FD()
	if fd != nil {
		j.cmd.SysProcAttr = &syscall.SysProcAttr{
			UseCgroupFD: true,
			CgroupFD:    int(fd.Fd()),
		}
	}

	if err := j.cmd.Start(); err != nil {
		j.state.Store(JobStateFailed)
		j.pipeWriter.Close()

		return fmt.Errorf("failed to start process: %w", err)
	}

	if fd == nil {
		if err := j.cgroup.Join(j.cmd.Process.Pid); err != nil {
			j.cmd.Process.Kill()
			j.state.Store(JobStateFailed)
			j.pipeWriter.Close()
			return fmt.Errorf("add process to cgroup: %w", err)
		}
	}

	j.pipeWriter.Close()
	j.state.Store(JobStateStarted)

	go func() {
		j.cmd.Wait()

		j.state.Store(JobStateStopped)
		j.processState.Store(j.cmd.ProcessState)
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

	if err := j.killCgroup(); err != nil {
		_ = err
	}

	return nil
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

func (j *Job) killCgroup() error {
	procsPath := filepath.Join(j.cgroup.Path(), "cgroup.procs")
	procsData, err := os.ReadFile(procsPath)
	if err != nil {
		return fmt.Errorf("read cgroup.procs: %w", err)
	}

	for line := range strings.SplitSeq(string(procsData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}

		// Kill the process (best effort)
		syscall.Kill(pid, syscall.SIGKILL)
	}

	return nil
}
