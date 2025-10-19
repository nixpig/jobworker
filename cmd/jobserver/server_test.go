package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/internal/jobmanager"
	"github.com/nixpig/jobworker/internal/tlsconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	// NOTE: Generate all cert files with: `make certs`.
	caCertPath       = "../../certs/ca.crt"
	serverCertPath   = "../../certs/server.crt"
	serverKeyPath    = "../../certs/server.key"
	operatorCertPath = "../../certs/client-operator.crt"
	operatorKeyPath  = "../../certs/client-operator.key"
	viewerCertPath   = "../../certs/client-viewer.crt"
	viewerKeyPath    = "../../certs/client-viewer.key"
)

func setupTestServerAndClients(
	t *testing.T,
) (api.JobServiceClient, api.JobServiceClient, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to setup listener: '%v'", err)
	}

	manager := jobmanager.NewManager()

	s := newServer(
		manager,
		slog.New(slog.DiscardHandler),
		&config{
			port:       0,
			certPath:   serverCertPath,
			keyPath:    serverKeyPath,
			caCertPath: caCertPath,
		},
	)

	operatorTLSConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
		CertPath:   operatorCertPath,
		KeyPath:    operatorKeyPath,
		CACertPath: caCertPath,
		Server:     false,
		ServerAddr: listener.Addr().String(),
	})
	if err != nil {
		t.Fatalf("failed to setup operator TLS: '%v'", err)
	}

	operatorConn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(operatorTLSConfig)),
	)
	if err != nil {
		t.Fatalf("operator failed to connect: '%v'", err)
	}

	viewerTLSConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
		CertPath:   viewerCertPath,
		KeyPath:    viewerKeyPath,
		CACertPath: caCertPath,
		Server:     false,
		ServerAddr: listener.Addr().String(),
	})
	if err != nil {
		t.Fatalf("failed to setup viewer TLS: '%v'", err)
	}

	viewerConn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(viewerTLSConfig)),
	)
	if err != nil {
		t.Fatalf("viewer failed to connect: '%v'", err)
	}

	go func() {
		if err := s.start(listener); err != nil {
			t.Logf("failed to start server: '%v'", err)
		}
	}()

	cleanup := func() {
		s.shutdown()
		manager.Shutdown()
		operatorConn.Close()
		viewerConn.Close()
	}

	operatorClient := api.NewJobServiceClient(operatorConn)
	viewerClient := api.NewJobServiceClient(viewerConn)

	return operatorClient, viewerClient, cleanup
}

func testJobStatus(
	t *testing.T,
	got *api.QueryJobResponse,
	want *api.QueryJobResponse,
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

	if got.Signal != want.Signal {
		t.Errorf(
			"expected signal: got '%s', want '%s'",
			got.Signal,
			want.Signal,
		)
	}
}

func waitForJobState(
	t *testing.T,
	client api.JobServiceClient,
	id string,
	wantState api.JobState,
	timeout time.Duration,
	interval time.Duration,
) *api.QueryJobResponse {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.QueryJob(ctx, &api.QueryJobRequest{Id: id})
		if err != nil {
			continue
		}

		if resp.State == wantState {
			return resp
		}

		time.Sleep(interval)
	}

	t.Fatalf("timeout out waiting for job state")
	return nil
}

// TODO: Add more individual tests to cover edge cases, error scenarios, and
// full auth matrix.

func TestJobServerIntegrationAsOperator(t *testing.T) {
	operatorClient, _, cleanup := setupTestServerAndClients(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Test job lifecycle", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "sleep",
			Args:    []string{"30"},
		}

		runResp, err := operatorClient.RunJob(ctx, runReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		if _, err := uuid.Parse(runResp.Id); err != nil {
			t.Errorf("expected to get valid UUID: got '%v'", runResp.Id)
		}

		queryReq := &api.QueryJobRequest{
			Id: runResp.Id,
		}

		queryResp, err := operatorClient.QueryJob(ctx, queryReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		testJobStatus(t, queryResp, &api.QueryJobResponse{
			ExitCode:    -1,
			State:       api.JobState_JOB_STATE_STARTED,
			Signal:      "",
			Interrupted: false,
		})

		stopReq := &api.StopJobRequest{
			Id: runResp.Id,
		}

		_, err = operatorClient.StopJob(ctx, stopReq)
		if err != nil {
			t.Errorf("exptected not to get error: got '%v'", err)
		}

		// Try stopping an already stopped job
		_, err = operatorClient.StopJob(ctx, stopReq)
		st, ok := status.FromError(err)
		if !ok {
			t.Errorf("expected gRPC status error: got '%v'", err)
		}

		if st.Code() != codes.FailedPrecondition {
			t.Errorf("expected FailedPrecondition error: got '%v'", st.Code())
		}

		jobStatus := waitForJobState(
			t,
			operatorClient,
			runResp.Id,
			api.JobState_JOB_STATE_STOPPED,
			1*time.Second,
			50*time.Millisecond,
		)

		testJobStatus(t, jobStatus, &api.QueryJobResponse{
			ExitCode:    -1,
			State:       api.JobState_JOB_STATE_STOPPED,
			Signal:      "killed",
			Interrupted: true,
		})
	})

	t.Run("Test job output streaming", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "echo",
			Args:    []string{"Hello, world!"},
		}

		runResp, err := operatorClient.RunJob(ctx, runReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		streamReq := &api.StreamJobOutputRequest{
			Id: runResp.Id,
		}

		// Stream from same job multiple times
		for i := range 3 {
			stream, err := operatorClient.StreamJobOutput(ctx, streamReq)
			if err != nil {
				t.Errorf("exptected not to get error: got '%v'", err)
			}

			var output []byte

			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("exptected not to get error: got '%v'", err)
				}

				output = append(output, resp.Output...)
			}

			if string(output) != "Hello, world!\n" {
				t.Errorf(
					"stream '%d' expected output: got '%s', want '%s'",
					i,
					string(output),
					"Hello, world!",
				)
			}
		}

		jobStatus := waitForJobState(
			t,
			operatorClient,
			runResp.Id,
			api.JobState_JOB_STATE_STOPPED,
			1*time.Second,
			50*time.Millisecond,
		)

		testJobStatus(t, jobStatus, &api.QueryJobResponse{
			ExitCode:    0,
			State:       api.JobState_JOB_STATE_STOPPED,
			Signal:      "",
			Interrupted: false,
		})

	})
}

func TestJobServerIntegrationAsViewer(t *testing.T) {
	operatorClient, viewerClient, cleanup := setupTestServerAndClients(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Test job lifecycle", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "sleep",
			Args:    []string{"30"},
		}

		_, err := viewerClient.RunJob(ctx, runReq)
		st, ok := status.FromError(err)
		if !ok {
			t.Errorf("expected gRPC status error: got '%v'", err)
		}

		if st.Code() != codes.PermissionDenied {
			t.Errorf("expected PermissionDenied error: got '%v'", st.Code())
		}

		runResp, err := operatorClient.RunJob(ctx, runReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		if _, err := uuid.Parse(runResp.Id); err != nil {
			t.Errorf("expected to get valid UUID: got '%v'", runResp.Id)
		}

		queryReq := &api.QueryJobRequest{
			Id: runResp.Id,
		}

		queryResp, err := viewerClient.QueryJob(ctx, queryReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		testJobStatus(t, queryResp, &api.QueryJobResponse{
			ExitCode:    -1,
			State:       api.JobState_JOB_STATE_STARTED,
			Signal:      "",
			Interrupted: false,
		})

		stopReq := &api.StopJobRequest{
			Id: runResp.Id,
		}

		_, err = viewerClient.StopJob(ctx, stopReq)
		st, ok = status.FromError(err)
		if !ok {
			t.Errorf("expected gRPC status error: got '%v'", err)
		}

		if st.Code() != codes.PermissionDenied {
			t.Errorf("expected PermissionDenied error: got '%v'", st.Code())
		}
	})

	t.Run("Test job output streaming", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "echo",
			Args:    []string{"Hello, world!"},
		}

		runResp, err := operatorClient.RunJob(ctx, runReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		streamReq := &api.StreamJobOutputRequest{
			Id: runResp.Id,
		}

		// Stream from same job multiple times
		for i := range 3 {
			stream, err := viewerClient.StreamJobOutput(ctx, streamReq)
			if err != nil {
				t.Errorf("exptected not to get error: got '%v'", err)
			}

			var output []byte

			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("exptected not to get error: got '%v'", err)
				}

				output = append(output, resp.Output...)
			}

			if string(output) != "Hello, world!\n" {
				t.Errorf(
					"stream '%d' expected output: got '%s', want '%s'",
					i,
					string(output),
					"Hello, world!",
				)
			}
		}

		jobStatus := waitForJobState(
			t,
			viewerClient,
			runResp.Id,
			api.JobState_JOB_STATE_STOPPED,
			1*time.Second,
			50*time.Millisecond,
		)

		testJobStatus(t, jobStatus, &api.QueryJobResponse{
			ExitCode:    0,
			State:       api.JobState_JOB_STATE_STOPPED,
			Signal:      "",
			Interrupted: false,
		})
	})
}
