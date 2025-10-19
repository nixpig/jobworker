// Command jobserver implements a gRPC server for handling requests to execute
// arbitrary Linux processes and stream their output using the jobmanager
// library.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/nixpig/jobworker/internal/jobmanager"

	// NOTE: Strictly speaking, the std lib flag package would be fine, but
	// wanted consistency between the client and server CLI without the overhead
	// of cobra, so using pflag.
	"github.com/spf13/pflag"
)

// TODO: Inject version at build time.
const version = "0.0.1"

type config struct {
	port       uint16
	debug      bool
	certPath   string
	keyPath    string
	caCertPath string
}

func main() {
	cfg := parseFlags()

	logger := newLogger(cfg.debug)

	manager := jobmanager.NewManager()

	server := newServer(manager, logger, cfg)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.port))
	if err != nil {
		logger.Error("failed to create listener", "port", cfg.port, "err", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "port", cfg.port, "version", version)
		errCh <- server.start(listener)
	}()

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGTERM,
		os.Interrupt,
	)
	defer cancel()

	select {
	case err := <-errCh:
		if err != nil {
			logger.Error("server stopped with error", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutting down server")
		server.shutdown()
		manager.Shutdown()
		cancel()
	}

	logger.Info("server shutdown cleanly")
}

func parseFlags() *config {
	cfg := &config{}

	pflag.Uint16Var(&cfg.port, "port", 8443, "gRPC server port")
	pflag.BoolVar(&cfg.debug, "debug", false, "Enable debug logs")

	pflag.StringVar(
		&cfg.certPath,
		"cert-path",
		"certs/server.crt",
		"Path to server TLS certificate",
	)

	pflag.StringVar(
		&cfg.keyPath,
		"key-path",
		"certs/server.key",
		"Path to server TLS private key",
	)

	pflag.StringVar(
		&cfg.caCertPath,
		"ca-cert-path",
		"certs/ca.crt",
		"Path to CA certificate for mTLS",
	)

	pflag.Parse()

	return cfg
}

func newLogger(debug bool) *slog.Logger {
	var level slog.Level

	if debug {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}

	// NOTE: Using a text handler, since this is running locally. In production,
	// would use JSON so it can be easily hooked up to existing solution.
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}
