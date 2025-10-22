//go:build !e2e

package jobmanager_test

import (
	"io"
	"syscall"
	"testing"

	"github.com/google/uuid"
	"github.com/nixpig/jobworker/internal/jobmanager"
)

func runTestJobInManager(
	t *testing.T,
	m *jobmanager.Manager,
	program string,
	args []string,
) string {
	t.Helper()

	id, err := m.RunJob(program, args, nil)
	if err != nil {
		t.Fatalf("expected not to receive error: got '%v'", err)
	}

	return id
}

func TestJobManager(t *testing.T) {
	t.Parallel()

	t.Run("Test run job", func(t *testing.T) {
		t.Parallel()

		m := jobmanager.NewManager()

		id := runTestJobInManager(t, m, "echo", []string{"Hello, world!"})

		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("expected to have valid uuid: got '%v'", err)
		}
	})

	t.Run("Test stop long-running job", func(t *testing.T) {
		t.Parallel()

		m := jobmanager.NewManager()

		id := runTestJobInManager(t, m, "sleep", []string{"5"})

		status, err := m.QueryJob(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		testJobState(t, status, &jobmanager.JobStatus{
			ExitCode:    -1,
			State:       jobmanager.JobStateStarted,
			Interrupted: false,
		})

		if err := m.StopJob(id); err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		job, err := m.GetJob(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		<-job.Done()

		status, err = m.QueryJob(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		testJobState(t, status, &jobmanager.JobStatus{
			ExitCode:    -1,
			State:       jobmanager.JobStateStopped,
			Signal:      syscall.SIGKILL,
			Interrupted: true,
		})
	})

	t.Run("Test stream job output", func(t *testing.T) {
		t.Parallel()

		m := jobmanager.NewManager()

		id := runTestJobInManager(
			t,
			m,
			"/bin/bash",
			[]string{"-c", "for i in {1..10}; do echo $i; done"},
		)

		job, err := m.GetJob(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		<-job.Done()

		outputReader, err := m.StreamJobOutput(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		gotOutput, err := io.ReadAll(outputReader)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		outputReader.Close()

		wantOutput := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
		if string(gotOutput) != wantOutput {
			t.Errorf(
				"expected output: got '%s', want '%s'",
				gotOutput,
				wantOutput,
			)
		}
	})

	t.Run("Test shutdown job manager", func(t *testing.T) {
		t.Parallel()

		m := jobmanager.NewManager()

		id := runTestJobInManager(t, m, "sleep", []string{"10"})

		m.Shutdown()

		job, err := m.GetJob(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		<-job.Done()

		status, err := m.QueryJob(id)
		if err != nil {
			t.Fatalf("expected not to receive error: got '%v'", err)
		}

		testJobState(t, status, &jobmanager.JobStatus{
			ExitCode:    -1,
			State:       jobmanager.JobStateStopped,
			Signal:      syscall.SIGKILL,
			Interrupted: true,
		})
	})

	t.Run("Test operations on non-existent job", func(t *testing.T) {
		t.Parallel()

		m := jobmanager.NewManager()

		if _, err := m.QueryJob("non-existent-job-id"); err != jobmanager.ErrJobNotFound {
			t.Errorf("expected to receive ErrJobNotFound: got '%v'", err)
		}

		if err := m.StopJob("non-existent-job-id"); err != jobmanager.ErrJobNotFound {
			t.Errorf("expected to receive ErrJobNotFound: got '%v'", err)
		}

		if _, err := m.StreamJobOutput("non-existent-job-id"); err != jobmanager.ErrJobNotFound {
			t.Errorf("expected to receive ErrJobNotFound: got '%v'", err)
		}

		if _, err := m.GetJob("non-existent-job-id"); err != jobmanager.ErrJobNotFound {
			t.Errorf("expected to receive ErrJobNotFound: got '%v'", err)
		}
	})

	t.Run("Test multiple jobs", func(t *testing.T) {
		t.Parallel()

		m := jobmanager.NewManager()

		ids := make([]string, 3)
		for i := range len(ids) {
			ids[i] = runTestJobInManager(t, m, "sleep", []string{"5"})
		}

		for _, id := range ids {
			status, err := m.QueryJob(id)
			if err != nil {
				t.Errorf("expected not to receive error: got '%v'", err)
			}

			if status.State != jobmanager.JobStateStarted {
				t.Errorf("expected job to be started: got '%s'", status.State)
			}
		}
	})
}
