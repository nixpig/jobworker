package jobmanager_test

import (
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/nixpig/jobworker/internal/jobmanager"
)

func newTestJob(t *testing.T, program string, args []string) *jobmanager.Job {
	t.Helper()

	id := uuid.NewString()

	job, err := jobmanager.NewJob(id, program, args)
	if err != nil {
		t.Fatalf("expected not to receive error: got '%v'", err)
	}

	gotID := job.ID()
	if gotID != id {
		t.Errorf("expected job id: got '%s', want '%s'", gotID, id)
	}

	return job
}

func runTestJob(t *testing.T, program string, args []string) *jobmanager.Job {
	t.Helper()

	job := newTestJob(t, program, args)

	if err := job.Start(); err != nil {
		t.Fatalf("expected not to receive error: got '%v'", err)
	}

	return job
}

func testJobState(
	t *testing.T,
	got jobmanager.JobStatus,
	want jobmanager.JobStatus,
) {
	t.Helper()

	if got.ExitCode != want.ExitCode {
		t.Errorf(
			"expected exit code: got '%d', want '%d'",
			got.ExitCode,
			want.ExitCode,
		)
	}

	if got.State != want.State {
		t.Errorf("expected state: got '%s', want '%s'", got.State, want.State)
	}

	if got.Interrupted != want.Interrupted {
		t.Errorf(
			"expected interrupted: got '%t', want '%t'",
			got.Interrupted,
			want.Interrupted,
		)
	}
}

func TestJob(t *testing.T) {
	t.Run("Test initial state", func(t *testing.T) {
		job := newTestJob(t, "echo", []string{"Hello, world!"})

		testJobState(
			t,
			jobmanager.JobStatus{
				ExitCode:    job.ExitCode(),
				State:       job.State(),
				Interrupted: job.Interrupted(),
			},
			jobmanager.JobStatus{
				ExitCode:    -1,
				State:       jobmanager.JobStateCreated,
				Interrupted: false,
			},
		)
	})

	t.Run("Test run to completion", func(t *testing.T) {
		job := runTestJob(t, "echo", []string{"Hello, world!"})

		<-job.Done()

		testJobState(
			t,
			jobmanager.JobStatus{
				ExitCode:    job.ExitCode(),
				State:       job.State(),
				Interrupted: job.Interrupted(),
			},
			jobmanager.JobStatus{
				ExitCode:    0,
				State:       jobmanager.JobStateStopped,
				Interrupted: false,
			},
		)
	})

	t.Run("Test stop long-running program", func(t *testing.T) {
		job := runTestJob(t, "sleep", []string{"30"})

		testJobState(
			t,
			jobmanager.JobStatus{
				ExitCode:    job.ExitCode(),
				State:       job.State(),
				Interrupted: job.Interrupted(),
			},
			jobmanager.JobStatus{
				ExitCode:    -1,
				State:       jobmanager.JobStateStarted,
				Interrupted: false,
			},
		)

		if err := job.Stop(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		<-job.Done()

		testJobState(
			t,
			jobmanager.JobStatus{
				ExitCode:    job.ExitCode(),
				State:       job.State(),
				Interrupted: job.Interrupted(),
			},
			jobmanager.JobStatus{
				ExitCode:    -1,
				State:       jobmanager.JobStateStopped,
				Interrupted: true,
			},
		)
	})

	t.Run("Test streaming output", func(t *testing.T) {
		job := runTestJob(
			t,
			"/bin/bash",
			[]string{"-c", "for i in {1..10}; do echo $i; done"},
		)

		<-job.Done()

		outputReader := job.StreamOutput()
		gotOutput, err := io.ReadAll(outputReader)
		outputReader.Close()

		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		wantOutput := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
		if string(gotOutput) != wantOutput {
			t.Errorf(
				"expected output: got '%s', want '%s'",
				gotOutput,
				wantOutput,
			)
		}
	})

	t.Run("Test non-existent program", func(t *testing.T) {
		job := newTestJob(t, "non-existent-program", []string{})

		if err := job.Start(); err == nil {
			t.Errorf("expected to receive error: got '%v'", err)
		}
	})

	t.Run("Test duplicate operations", func(t *testing.T) {
		job := newTestJob(t, "sleep", []string{"30"})

		if err := job.Start(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		if err := job.Start(); !errors.As(
			err,
			&jobmanager.InvalidStateError{},
		) {
			t.Errorf("expected to receive InvalidStateError: got '%v'", err)
		}

		if err := job.Stop(); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		if err := job.Stop(); !errors.As(
			err,
			&jobmanager.InvalidStateError{},
		) {
			t.Errorf("expected to receive InvalidStateError: got '%v'", err)
		}

		if err := job.Start(); !errors.As(
			err,
			&jobmanager.InvalidStateError{},
		) {
			t.Errorf("expected to receive InvalidStateError: got '%v'", err)
		}
	})
}
