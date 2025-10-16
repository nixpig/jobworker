package jobmanager

import "sync/atomic"

type JobState int

const (
	// JobStateUnknown indicates the state of the job is unknown. It's used as
	// the zero value for functions that return a (possibly absent) JobState.
	JobStateUnknown JobState = iota

	// JobStateCreated indicates the job has been configured and resources
	// allocated successfully. The job can be started.
	JobStateCreated

	// JobStateStarting indicates the job is in the process of starting, e.g.
	// Start() called but the underlying process has not yet started.
	JobStateStarting

	// JobStateStarted indicates the program specified by the job has started.
	// The job can be stopped.
	JobStateStarted

	// JobStateStopping indicates the program specified by the job in in the
	// process of stopping, e.g. received SIGTERM but not yet exited.
	JobStateStopping

	// JobStateStopped indicates the program specified by the job has exited with
	// an exit code.
	JobStateStopped

	// JobStateFailed indicates the job has failed for some reason that is not
	// due to the underlying program being run, e.g. the server failed to
	// allocate resources.
	JobStateFailed
)

// NOTE: This slice needs to be kept in sync with any changes to the JobState
// values. Ideally, we'd only ever be 'adding' more states to maintain a
// consistent API.
var jobStates = []string{
	"Unknown",
	"Created",
	"Starting",
	"Started",
	"Stopping",
	"Stopped",
	"Failed",
}

// String implements the Stringer interface for JobState and returns a string
// representation of the JobState by using the int value to index into a slice.
func (s JobState) String() string {
	if int(s) < 0 || int(s) >= len(jobStates) {
		return jobStates[0]
	}

	return jobStates[s]
}

// AtomicJobState is a wrapper around an atomic.Int32 to provide atomic
// operations on a JobState.
//  1. Simplifies validating state transitions with CompareAndSwap.
//  2. Reduces (maybe removes?) the
//     need for mutexes and explicit handling of locking on a Job.
type AtomicJobState struct {
	v atomic.Int32
}

// Load atomically loads the JobState value.
func (a *AtomicJobState) Load() JobState {
	return JobState(a.v.Load())
}

// Store attomically stores the JobState value.
func (a *AtomicJobState) Store(s JobState) {
	a.v.Store(int32(s))
}

// CompareAndSwap performs an atomic compare-and-swap operation with an old and
// new JobState.
func (a *AtomicJobState) CompareAndSwap(o, n JobState) bool {
	return a.v.CompareAndSwap(int32(o), int32(n))
}
