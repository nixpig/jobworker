package jobmanager_test

import (
	"testing"

	"github.com/nixpig/jobworker/internal/jobmanager"
)

func runTestJobInManager(
	t *testing.T,
	m *jobmanager.Manager,
	program string,
	args []string,
) string {
	t.Helper()

	id, err := m.RunJob(program, args)
	if err != nil {
		t.Fatalf("expected not to receive error: got '%v'", err)
	}

	return id
}

func testJobStatus(
	t *testing.T,
	got *jobmanager.JobStatus,
	want jobmanager.JobStatus,
) {
	t.Helper()

	if got.State != want.State {
		t.Errorf("expected state: got '%s', want '%s'", got.State, want.State)
	}

	if got.ExitCode != want.ExitCode {
		t.Errorf(
			"expected exit code: got '%d', want '%d'",
			got.ExitCode,
			want.ExitCode,
		)
	}

	if got.Interrupted != want.Interrupted {
		t.Errorf(
			"expected interrupted: got '%t', want '%t'",
			got.Interrupted,
			want.Interrupted,
		)
	}
}

func TestJobManagerWithMultipleJobs(t *testing.T) {
	// TODO:
	// RunJob
	//	- Run a job successfully / verify uuid / verify status
	//	- Empty program string returns error
	//
	// StopJob
	//	- Stop a running job
	//	- Stop non-existent job returns ErrJobNotFound
	//
	// QueryJob
	//	- Query existing job returns correct status
	//	- Query non-existent job returns ErrJobNotFound
	//
	// StreamJobOutput
	//	- Stream output from existing job
	//	- Stream from non-existent job returns ErrJobNotFound
	//
	// Shutdown
	//	- Shutdown stops running jobs gracefully
	//
	//	- Run 2-3 jobs concurrently to verify the Manager actually manages multiple jobs.

}
