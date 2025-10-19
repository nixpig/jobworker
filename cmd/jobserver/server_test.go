package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

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
	caCertPath     = "../../certs/ca.crt"
	serverCertPath = "../../certs/server.crt"
	serverKeyPath  = "../../certs/server.key"
	clientCertPath = "../../certs/client.crt"
	clientKeyPath  = "../../certs/client.key"
)

func setupTestClientAndServer(t *testing.T) (api.JobServiceClient, func()) {
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

	clientTLSConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
		CertPath:   clientCertPath,
		KeyPath:    clientKeyPath,
		CACertPath: caCertPath,
		Server:     false,
		ServerAddr: listener.Addr().String(),
	})
	if err != nil {
		t.Fatalf("failed to setup client TLS: '%v'", err)
	}

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLSConfig)),
	)
	if err != nil {
		t.Fatalf("failed to connect: '%v'", err)
	}

	go func() {
		if err := s.start(listener); err != nil {
			t.Logf("failed to start server: '%v'", err)
		}
	}()

	cleanup := func() {
		s.shutdown()
		manager.Shutdown()
		conn.Close()
	}

	return api.NewJobServiceClient(conn), cleanup
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

func TestJobServerIntegration(t *testing.T) {
	client, cleanup := setupTestClientAndServer(t)
	defer cleanup()

	ctx := context.Background()

	// TODO: Add more individual tests to cover edge cases and error scenarios.
	t.Run("Test job lifecycle", func(t *testing.T) {
		runReq := &api.RunJobRequest{
			Program: "sleep",
			Args:    []string{"30"},
		}

		runResp, err := client.RunJob(ctx, runReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		if _, err := uuid.Parse(runResp.Id); err != nil {
			t.Errorf("expected to get valid UUID: got '%v'", runResp.Id)
		}

		queryReq := &api.QueryJobRequest{
			Id: runResp.Id,
		}

		queryResp, err := client.QueryJob(ctx, queryReq)
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

		_, err = client.StopJob(ctx, stopReq)
		if err != nil {
			t.Errorf("exptected not to get error: got '%v'", err)
		}

		_, err = client.StopJob(ctx, stopReq)
		st, ok := status.FromError(err)
		if !ok {
			t.Fatal("expected gRPC status error")
		}

		if st.Code() != codes.FailedPrecondition {
			t.Errorf("expected FailedPrecondition error: got '%v'", st.Code())
		}

		queryResp, err = client.QueryJob(ctx, queryReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		testJobStatus(t, queryResp, &api.QueryJobResponse{
			ExitCode:    -1,
			State:       api.JobState_JOB_STATE_STOPPED,
			Signal:      "killed",
			Interrupted: true,
		})
	})

	t.Run("Test job output streaming", func(t *testing.T) {
		// RunJob
		runReq := &api.RunJobRequest{
			Program: "echo",
			Args:    []string{"Hello, world!"},
		}

		runResp, err := client.RunJob(ctx, runReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		// StreamJobOutput
		streamReq := &api.StreamJobOutputRequest{
			Id: runResp.Id,
		}

		stream, err := client.StreamJobOutput(ctx, streamReq)
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
				"expected output: got '%s', want '%s'",
				string(output),
				"Hello, world!",
			)
		}
		// QueryJob
		queryReq := &api.QueryJobRequest{
			Id: runResp.Id,
		}

		queryResp, err := client.QueryJob(ctx, queryReq)
		if err != nil {
			t.Errorf("expected not to get error: got '%v'", err)
		}

		testJobStatus(t, queryResp, &api.QueryJobResponse{
			ExitCode:    0,
			State:       api.JobState_JOB_STATE_STOPPED,
			Signal:      "",
			Interrupted: false,
		})

	})
}
