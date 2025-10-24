package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/certs"
	"github.com/nixpig/jobworker/internal/jobmanager"
	"github.com/nixpig/jobworker/internal/tlsconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// setupTestServerAndClients starts a test server and returns an operator
// client and a viewer client.
func setupTestServerAndClients(
	t *testing.T,
) (api.JobServiceClient, api.JobServiceClient) {
	t.Helper()

	certDir := t.TempDir()

	certFiles := []string{
		"ca.crt",
		"server.crt",
		"server.key",
		"client-operator.crt",
		"client-operator.key",
		"client-viewer.crt",
		"client-viewer.key",
	}

	for _, filename := range certFiles {
		data, err := certs.FS.ReadFile(filename)
		if err != nil {
			t.Fatalf("read cert %s: %v", filename, err)
		}

		path := filepath.Join(certDir, filename)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("save cert %s: %v", filename, err)
		}
	}

	caCertPath := filepath.Join(certDir, "ca.crt")
	serverCertPath := filepath.Join(certDir, "server.crt")
	serverKeyPath := filepath.Join(certDir, "server.key")
	operatorCertPath := filepath.Join(certDir, "client-operator.crt")
	operatorKeyPath := filepath.Join(certDir, "client-operator.key")
	viewerCertPath := filepath.Join(certDir, "client-viewer.crt")
	viewerKeyPath := filepath.Join(certDir, "client-viewer.key")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to setup listener: '%v'", err)
	}

	t.Cleanup(func() {
		listener.Close()
	})

	manager := jobmanager.NewManager()

	t.Cleanup(func() {
		manager.Shutdown()
	})

	s := newServer(
		manager,
		slog.New(slog.DiscardHandler),
		&config{
			port:       "0",
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
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("failed to setup operator TLS: '%v'", err)
	}

	operatorConn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(operatorTLSConfig)),
	)
	if err != nil {
		t.Fatalf("failed to create operator client: '%v'", err)
	}

	t.Cleanup(func() {
		operatorConn.Close()
	})

	operatorClient := api.NewJobServiceClient(operatorConn)

	viewerTLSConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
		CertPath:   viewerCertPath,
		KeyPath:    viewerKeyPath,
		CACertPath: caCertPath,
		Server:     false,
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("failed to setup viewer TLS: '%v'", err)
	}

	viewerConn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(viewerTLSConfig)),
	)
	if err != nil {
		t.Fatalf("failed to create viewer client: '%v'", err)
	}

	t.Cleanup(func() {
		viewerConn.Close()
	})

	viewerClient := api.NewJobServiceClient(viewerConn)

	go func() {
		if err := s.start(listener); err != nil {
			t.Logf("failed to start server: '%v'", err)
		}
	}()

	t.Cleanup(func() {
		s.shutdown()
	})

	return operatorClient, viewerClient
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

func testGRPCStatus(t *testing.T, err error, want codes.Code) {
	t.Helper()

	st, ok := status.FromError(err)
	if !ok {
		t.Errorf("expected gRPC status error: got '%v'", err)
	}

	if st.Code() != want {
		t.Errorf(
			"expected gRPC status code: got '%v', want '%v'",
			st.Code(),
			want,
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

	deadline := time.Now().Add(timeout)

	var resp *api.QueryJobResponse
	var err error

	for time.Now().Before(deadline) {
		resp, err = client.QueryJob(t.Context(), &api.QueryJobRequest{Id: id})
		if err != nil {
			continue
		}

		if resp.State == wantState {
			return resp
		}

		time.Sleep(interval)
	}

	t.Fatalf(
		"timed out waiting for job state: got '%v', want '%v'",
		resp.State,
		wantState,
	)
	return nil
}

func TestJobServerIntegrationAsOperator(t *testing.T) {
	operatorClient, _ := setupTestServerAndClients(t)

	t.Run("Test job lifecycle", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "sleep",
			Args:    []string{"30"},
		}

		runResp, err := operatorClient.RunJob(t.Context(), runReq)
		if err != nil {
			t.Fatalf("expected run job not to return error: got '%v'", err)
		}

		if _, err := uuid.Parse(runResp.Id); err != nil {
			t.Errorf("expected run job to return UUID: got '%v'", runResp.Id)
		}

		queryReq := &api.QueryJobRequest{
			Id: runResp.Id,
		}

		queryResp, err := operatorClient.QueryJob(t.Context(), queryReq)
		if err != nil {
			t.Errorf("expected query job not to return error: got '%v'", err)
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

		_, err = operatorClient.StopJob(t.Context(), stopReq)
		if err != nil {
			t.Errorf("expected stop job not to return error: got '%v'", err)
		}

		// Try stopping an already stopped job
		_, err = operatorClient.StopJob(t.Context(), stopReq)
		testGRPCStatus(t, err, codes.FailedPrecondition)

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

		runResp, err := operatorClient.RunJob(t.Context(), runReq)
		if err != nil {
			t.Fatalf("expected run job not to return error: got '%v'", err)
		}

		streamReq := &api.StreamJobOutputRequest{
			Id: runResp.Id,
		}

		// Stream from same job multiple times
		for i := range 3 {
			stream, err := operatorClient.StreamJobOutput(
				t.Context(),
				streamReq,
			)
			if err != nil {
				t.Errorf(
					"expected stream job not to return error: got '%v'",
					err,
				)
			}

			var output []byte

			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf(
						"expected stream '%d' not to recieve error: got '%v'",
						i,
						err,
					)
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

	t.Run("Test job put in cgroup", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "/bin/bash",
			Args:    []string{"-c", "cat /proc/self/cgroup"},
		}

		runResp, err := operatorClient.RunJob(t.Context(), runReq)
		if err != nil {
			t.Fatalf("expected run job not to return error: got '%v'", err)
		}

		streamReq := &api.StreamJobOutputRequest{
			Id: runResp.Id,
		}

		stream, err := operatorClient.StreamJobOutput(t.Context(), streamReq)
		if err != nil {
			t.Errorf("expected stream job not to return error: got '%v'", err)
		}

		var output []byte

		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf(
					"expected stream not to receive error: got '%v'",
					err,
				)
			}

			output = append(output, resp.Output...)
		}

		if !strings.Contains(string(output), runResp.Id) {
			t.Errorf(
				"expected job to be in cgroup: got '%s', want '%s'",
				output,
				runResp.Id,
			)
		}
	})
}

func TestJobServerIntegrationAsViewer(t *testing.T) {
	operatorClient, viewerClient := setupTestServerAndClients(t)

	ctx := t.Context()

	t.Run("Test job lifecycle", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "sleep",
			Args:    []string{"30"},
		}

		_, err := viewerClient.RunJob(ctx, runReq)
		testGRPCStatus(t, err, codes.PermissionDenied)

		// Operator client used to start the job.
		runResp, err := operatorClient.RunJob(ctx, runReq)
		if err != nil {
			t.Fatalf("expected run job not to return error: got '%v'", err)
		}

		if _, err := uuid.Parse(runResp.Id); err != nil {
			t.Errorf("expected run job to return UUID: got '%v'", runResp.Id)
		}

		queryReq := &api.QueryJobRequest{
			Id: runResp.Id,
		}

		queryResp, err := viewerClient.QueryJob(ctx, queryReq)
		if err != nil {
			t.Errorf("expected query job not to return error: got '%v'", err)
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
		testGRPCStatus(t, err, codes.PermissionDenied)
	})

	t.Run("Test job output streaming", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "echo",
			Args:    []string{"Hello, world!"},
		}

		// Operator used to start the job.
		runResp, err := operatorClient.RunJob(ctx, runReq)
		if err != nil {
			t.Fatalf("expected run job not to return error: got '%v'", err)
		}

		streamReq := &api.StreamJobOutputRequest{
			Id: runResp.Id,
		}

		// Stream from same job multiple times
		for i := range 3 {
			stream, err := viewerClient.StreamJobOutput(ctx, streamReq)
			if err != nil {
				t.Errorf(
					"expected stream job not to return error: got '%v'",
					err,
				)
			}

			var output []byte

			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf(
						"expected stream '%d' not to receive error: got '%v'",
						i,
						err,
					)
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

func TestJobServerIntegrationErrorScenarios(t *testing.T) {
	operatorClient, _ := setupTestServerAndClients(t)

	ctx := t.Context()

	t.Run("Test RunJob with empty program", func(t *testing.T) {
		req := &api.RunJobRequest{
			Program: "",
			Args:    []string{},
		}

		_, err := operatorClient.RunJob(ctx, req)
		testGRPCStatus(t, err, codes.InvalidArgument)
	})

	t.Run("Test QueryJob with non-existent ID", func(t *testing.T) {
		req := &api.QueryJobRequest{
			Id: "some-non-existent-id",
		}

		_, err := operatorClient.QueryJob(ctx, req)
		testGRPCStatus(t, err, codes.NotFound)
	})

	t.Run("Test StopJob with non-existent ID", func(t *testing.T) {
		req := &api.StopJobRequest{
			Id: "some-non-existent-id",
		}

		_, err := operatorClient.StopJob(ctx, req)
		testGRPCStatus(t, err, codes.NotFound)
	})

	t.Run("Test StreamJobOutput with non-existent ID", func(t *testing.T) {
		req := &api.StreamJobOutputRequest{
			Id: "some-non-existent-id",
		}

		stream, err := operatorClient.StreamJobOutput(ctx, req)
		if err != nil {
			t.Errorf("expected stream job not to receive error: got '%v'", err)
		}

		_, err = stream.Recv()
		testGRPCStatus(t, err, codes.NotFound)
	})
}
