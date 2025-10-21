package jobmanager

import "sync/atomic"

// JobState represents the state of a Job throughout its lifecycle.
type JobState int

const (
	// JobStateUnknown indicates the state of the Job is unknown.
	JobStateUnknown JobState = iota

	// JobStateCreated indicates the Job has been configured and resources
	// allocated successfully. The Job can be started.
	JobStateCreated

	// JobStateStarting indicates the Job is in the process of starting, e.g.
	// Start() called but the underlying process has not yet started.
	JobStateStarting

	// JobStateStarted indicates the process managed by the Job has started.
	// The Job can be stopped.
	JobStateStarted

	// JobStateStopping indicates the process managed by the Job is in the
	// process of stopping, e.g. received SIGTERM but not yet exited.
	JobStateStopping

	// JobStateStopped indicates the process managed by the Job has exited with
	// an exit code.
	JobStateStopped

	// JobStateFailed indicates the Job has failed for a reason that is not
	// due to the underlying process being run, e.g. the server failed to
	// allocate resources.
	JobStateFailed
)

// NOTE: This slice needs to be kept in sync with any changes to the JobState
// values. Ideally, we'd only ever be 'appending' states to maintain a
// consistent API, so wouldn't expect this to break for existing JobStates.
var jobStates = []string{
	"Unknown",
	"Created",
	"Starting",
	"Started",
	"Stopping",
	"Stopped",
	"Failed",
}

// String implements the Stringer interface and returns the name of the
// JobState.
func (s JobState) String() string {
	if int(s) < 0 || int(s) >= len(jobStates) {
		return jobStates[0]
	}

	return jobStates[s]
}

// AtomicJobState provides atomic operations on JobState.
//  1. Simplifies validating state transitions with CompareAndSwap.
//  2. Reduces the need for explicit lock management on a Job.
type AtomicJobState struct {
	v atomic.Int32
}

// Load atomically loads the JobState value.
func (a *AtomicJobState) Load() JobState {
	return JobState(a.v.Load())
}

// Store atomically stores the JobState value.
func (a *AtomicJobState) Store(s JobState) {
	a.v.Store(int32(s))
}

// CompareAndSwap performs an atomic compare-and-swap operation with an old and
// new JobState.
func (a *AtomicJobState) CompareAndSwap(o, n JobState) bool {
	return a.v.CompareAndSwap(int32(o), int32(n))
}
