package main

import (
	"context"
	"log/slog"
	"net"
	"testing"

	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/internal/jobmanager"
	"github.com/nixpig/jobworker/internal/tlsconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	// NOTE: Generate with: `make certs-ca certs-server certs-client`.
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

	tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
		CertPath:   clientCertPath,
		KeyPath:    clientKeyPath,
		CACertPath: caCertPath,
		Server:     false,
		ServerAddr: listener.Addr().String(),
	})
	if err != nil {
		t.Fatalf("failed to setup client TLS: '%v'", err)
	}

	creds := credentials.NewTLS(tlsConfig)

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(creds),
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

func TestRunJobIntegration(t *testing.T) {
	client, cleanup := setupTestClientAndServer(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Test empty program", func(t *testing.T) {
		req := &api.RunJobRequest{
			Program: "",
			Args:    []string{},
		}

		_, err := client.RunJob(ctx, req)

		st, ok := status.FromError(err)
		if !ok {
			t.Fatal("expected gRPC status error")
		}

		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument error: got '%v'", st.Code())
		}

		if st.Message() != "program is empty" {
			t.Errorf("expected message: got '%v'", st.Message())
		}
	})
}
