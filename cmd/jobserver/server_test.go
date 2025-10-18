package main

import (
	"log/slog"
	"testing"

	"github.com/nixpig/jobworker/internal/jobmanager"
)

const (
	// NOTE: Generate with: `make certs-ca certs-server certs-client`.
	caCertPath     = "../../certs/ca.crt"
	serverCertPath = "../../certs/server.crt"
	serverKeyPath  = "../../certs/server.key"
	clientCertPath = "../../certs/client.crt"
	clientCertKey  = "../../certs/client.key"
)

func startTestServer(t *testing.T) (string, func()) {
	t.Helper()

	manager := jobmanager.NewManager()

	s := newServer(
		manager,
		slog.New(slog.DiscardHandler),
		&config{
			certPath:   serverCertPath,
			keyPath:    serverKeyPath,
			caCertPath: caCertPath,
		},
	)

	go func() {
		if err := s.start(); err != nil {
			t.Logf("failed to start server: '%s'", err)
		}
	}()

	cleanup := func() {
		s.shutdown()
		manager.Shutdown()
	}

	return s.addr.String(), cleanup
}
